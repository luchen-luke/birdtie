import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/moment_interaction.dart';
import 'package:birdtie_client/src/workspace/remote_inbox_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const owner = '11111111-1111-4111-8111-111111111111';
const moment = '22222222-2222-4222-8222-222222222222';
const operation = '33333333-3333-4333-8333-333333333333';
const comment = '44444444-4444-4444-8444-444444444444';
const item = '55555555-5555-4555-8555-555555555555';

http.Response jsonResponse(Object data) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': data})),
  200,
  headers: {'Content-Type': 'application/json'},
);

class MemoryPendingStore implements SocialPendingStore {
  SocialOperationPending? value;
  @override
  Future<SocialOperationPending?> read(String key) async => value;
  @override
  Future<void> write(String key, SocialOperationPending pending) async {
    value = pending;
  }

  @override
  Future<void> remove(String key, SocialOperationPending pending) async {
    if (value?.same(pending) == true) value = null;
  }
}

Map<String, dynamic> commentInput() => {
  'operationId': operation,
  'action': 'COMMENT',
  'momentId': moment,
  'expectedMomentRevision': 2,
  'expectedLikeVersion': 0,
  'commentId': '',
  'body': '本次明确公开评论<保持原 ID>',
};
Future<void> drain(WidgetTester tester) async {
  await tester.runAsync(() async {
    await Future<void>.delayed(const Duration(milliseconds: 25));
  });
  await tester.pump();
}

void main() {
  test(
    'notification query and stable comment destination remain explicit',
    () async {
      final requests = <http.Request>[];
      final source = RemoteInboxSource(
        authorizationHeader: () => 'Bearer synthetic',
        apiBaseUrl: 'https://isolated.invalid',
        client: MockClient((request) async {
          requests.add(request);
          if (request.url.path.endsWith('/moment-target'))
            return jsonResponse({'momentId': moment, 'commentId': comment});
          return jsonResponse({
            'items': [
              {
                'id': item,
                'category': 'updates',
                'title': '动态收到评论',
                'detail': '打开当前动态查看评论。',
                'resourceType': 'moment_comment',
                'resourceId': operation,
                'createdAt': DateTime.now().toUtc().toIso8601String(),
                'semanticCategory': 'SOCIAL',
                'notificationRoute': 'NORMAL',
                'notificationPriority': 50,
              },
            ],
            'nextCursor': null,
            'unreadCount': 3,
          });
        }),
      );
      final page = await source.loadNotificationsPage(
        category: 'updates',
        unreadOnly: true,
      );
      expect(page.unreadCount, 3);
      expect(requests.single.url.queryParameters['category'], 'updates');
      expect(requests.single.url.queryParameters['unreadOnly'], 'true');
      final target = await source.momentTarget(item);
      expect(target.momentID, moment);
      expect(target.commentID, comment);
      source.dispose();
      final pending = SocialOperationPending(
        'MOMENT_INTERACTION',
        operation,
        'COMMENT',
        moment,
        'a' * 64,
        'UNKNOWN',
      );
      expect(
        () =>
            SocialOperationPending.decode({...pending.json, 'body': '禁止持久化正文'}),
        throwsFormatException,
      );
    },
  );

  testWidgets(
    'cancel writes nothing and lost comment response recovers original ID without PUT replay',
    (tester) async {
      final store = MemoryPendingStore();
      int puts = 0, gets = 0;
      final client = MockClient((request) async {
        if (request.url.path.endsWith('/previews')) {
          final input = jsonDecode(request.body) as Map<String, dynamic>;
          final now = DateTime.now().toUtc();
          return jsonResponse({
            'operationId': operation,
            'action': 'COMMENT',
            'payloadDigest': socialOperationDigest('MOMENT_INTERACTION', input),
            'observedAt': now.toIso8601String(),
            'expiresAt': now.add(const Duration(seconds: 29)).toIso8601String(),
            'approval': 'synthetic exact approval',
            'input': input,
            'sourceVersion': 'a' * 64,
          });
        }
        if (request.method == 'PUT') {
          puts++;
          throw TimeoutException('synthetic lost original response');
        }
        gets++;
        return jsonResponse({
          'operationId': operation,
          'status': 'SUCCEEDED',
          'action': 'COMMENT',
          'momentId': moment,
          'commentId': comment,
          'appliedVersion': 1,
          'payloadDigest': store.value!.digest,
          'committedAt': DateTime.now().toUtc().toIso8601String(),
        });
      });
      final api = SocialOperationAPI(
        client: client,
        baseUrl: 'https://isolated.invalid',
        authorizationHeader: () => 'Bearer synthetic',
        current: () => true,
      );
      final control = SocialOperationController(
        api: api,
        family: 'MOMENT_INTERACTION',
        ownerID: () => owner,
        targetID: moment,
        pendingStore: store,
      );
      await control.restore();
      await tester.pumpWidget(
        MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: FilledButton(
                onPressed: () => unawaited(
                  control.run(context, commentInput(), '确认公开评论', '这条具体评论将公开。'),
                ),
                child: const Text('发评论'),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('发评论'));
      await drain(tester);
      await tester.pumpAndSettle();
      expect(puts, 0);
      expect(store.value, isNull);
      await tester.tap(find.text('取消'));
      await tester.pumpAndSettle();
      expect(puts, 0);
      await tester.tap(find.text('发评论'));
      await drain(tester);
      await tester.pumpAndSettle();
      await tester.tap(find.text('确认'));
      await drain(tester);
      await tester.pumpAndSettle();
      expect(puts, 1);
      expect(store.value?.stage, 'UNKNOWN');
      expect(
        store.value!.json.keys,
        containsAll({'operationId', 'payloadDigest', 'stage'}),
      );
      expect(store.value!.json.containsKey('body'), isFalse);
      expect(store.value!.json.containsKey('approval'), isFalse);
      await tester.pumpWidget(const SizedBox());
      control.dispose();
      final resumed = SocialOperationController(
        api: api,
        family: 'MOMENT_INTERACTION',
        ownerID: () => owner,
        targetID: moment,
        pendingStore: store,
      );
      await resumed.restore();
      final fact = await tester.runAsync(() => resumed.recover());
      expect(fact?.commentID, comment);
      expect(puts, 1);
      expect(gets, 1);
      expect(store.value, isNull);
      resumed.dispose();
      client.close();
    },
  );

  testWidgets('captured confirmation cannot write after account changes', (
    tester,
  ) async {
    final who = ValueNotifier<String>(owner), store = MemoryPendingStore();
    int puts = 0;
    final client = MockClient((request) async {
      if (request.method == 'PUT') {
        puts++;
        return http.Response('', 500);
      }
      final input = jsonDecode(request.body) as Map<String, dynamic>;
      final now = DateTime.now().toUtc();
      return jsonResponse({
        'operationId': operation,
        'action': 'COMMENT',
        'payloadDigest': socialOperationDigest('MOMENT_INTERACTION', input),
        'observedAt': now.toIso8601String(),
        'expiresAt': now.add(const Duration(seconds: 29)).toIso8601String(),
        'approval': 'synthetic exact approval',
        'input': input,
        'sourceVersion': 'a' * 64,
      });
    });
    final control = SocialOperationController(
      api: SocialOperationAPI(
        client: client,
        baseUrl: 'https://isolated.invalid',
        authorizationHeader: () => 'Bearer synthetic',
        current: () => true,
      ),
      family: 'MOMENT_INTERACTION',
      ownerID: () => who.value,
      targetID: moment,
      pendingStore: store,
      identityChanges: who,
    );
    await control.restore();
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => Scaffold(
            body: TextButton(
              onPressed: () => unawaited(
                control.run(context, commentInput(), '确认评论', '原账号的具体内容。'),
              ),
              child: const Text('预览评论'),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('预览评论'));
    await drain(tester);
    await tester.pumpAndSettle();
    final confirm = tester
        .widget<FilledButton>(find.byType(FilledButton))
        .onPressed!;
    who.value = comment;
    confirm();
    await drain(tester);
    await tester.pumpAndSettle();
    expect(puts, 0);
    expect(store.value, isNull);
    await tester.pumpWidget(const SizedBox());
    control.dispose();
    who.dispose();
    client.close();
  });
}
