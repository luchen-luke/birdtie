import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/remote_inbox_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'inbox_panel_test.dart' as fixture;
import 'remote_inbox_source_test.dart' as rows;

const _actor = '22222222-2222-4222-8222-222222222222';
Map<String, dynamic> _notice({bool read = false}) => {
  ...rows.notificationRow(1),
  'title': '收到新的关注',
  'detail': '查看对方当前可见的资料',
  'resourceType': 'person_follow',
  'notificationRoute': 'NORMAL',
  'semanticCategory': 'SOCIAL',
  'notificationPriority': 50,
  if (read) 'readAt': '2026-10-16T00:00:00Z',
};
http.Response _reply(Object data, {int status = 200}) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': data})), status);

void main() {
  test(
    'follow notifications negotiate compatibility and resolve current target',
    () async {
      var status = 200;
      final source = RemoteInboxSource(
        authorizationHeader: () => 'Bearer synthetic',
        apiBaseUrl: 'https://fixture.invalid',
        client: MockClient((request) async {
          expect(request.method, 'GET');
          if (request.url.path.endsWith('/page')) {
            expect(
              request.headers['X-Birdtie-Inbox-Features'],
              'person-follow-v1',
            );
            return _reply(
              rows.notificationPageData(items: [_notice()], unread: 21),
            );
          }
          return _reply({'accountId': _actor}, status: status);
        }),
      );
      addTearDown(source.dispose);
      final page = await source.loadNotificationsPage();
      expect(page.items.single.supported, isTrue);
      expect(page.unreadCount, 21);
      expect(await source.personTarget(page.items.single.id), _actor);
      status = 403;
      await expectLater(
        source.personTarget(page.items.single.id),
        throwsA(isA<InboxReadFailure>()),
      );
      expect(
        () =>
            InboxItem.fromJson({..._notice(), 'targetConversationId': _actor}),
        throwsFormatException,
      );
      expect(
        () => InboxItem.fromJson({..._notice(), 'semanticCategory': 'MESSAGE'}),
        throwsFormatException,
      );
    },
  );

  test('late person target is retired after identity changes', () async {
    var token = 'Bearer before';
    final reply = Completer<http.Response>();
    final started = Completer<void>();
    final source = RemoteInboxSource(
      authorizationHeader: () => token,
      apiBaseUrl: 'https://fixture.invalid',
      client: MockClient((_) {
        started.complete();
        return reply.future;
      }),
    );
    addTearDown(source.dispose);
    final pending = source.personTarget(_notice()['id'] as String);
    await started.future;
    token = 'Bearer after';
    final rejected = expectLater(pending, throwsA(isA<Object>()));
    reply.complete(_reply({'accountId': _actor}));
    await rejected;
  });

  testWidgets(
    'notification opens currently resolved profile and denial keeps inbox',
    (tester) async {
      final auth = SeedTestAuth()
        ..owner = '11111111-1111-4111-8111-111111111111';
      addTearDown(auth.dispose);
      var denied = true;
      var profileReads = 0;
      final paths = <String>[];
      final client = MockClient((request) async {
        paths.add(request.url.path);
        if (request.url.path.endsWith('/page'))
          return _reply(
            rows.notificationPageData(items: [_notice()], unread: 1),
          );
        if (request.url.path.endsWith('/read'))
          return _reply(_notice(read: true));
        if (request.url.path.endsWith('/person-target'))
          return _reply({'accountId': _actor}, status: denied ? 404 : 200);
        if (request.url.path == '/v1/accounts/$_actor/profile') {
          profileReads++;
          return _reply({
            'accountId': _actor,
            'displayName': '当前可见关注者',
            'visibility': 'public',
          });
        }
        return _reply({}, status: 404);
      });
      final source = RemoteInboxSource(
        authorizationHeader: () => auth.authorizationHeader,
        apiBaseUrl: 'https://fixture.invalid',
        client: client,
      );
      addTearDown(source.dispose);
      await tester.pumpWidget(fixture.notificationView(auth, source));
      await tester.pumpAndSettle();
      await tester.tap(find.text('收到新的关注'));
      await tester.pumpAndSettle();
      expect(profileReads, 0);
      expect(find.text('这条关注通知已失效，请刷新通知。'), findsOneWidget);
      denied = false;
      await tester.tap(find.text('收到新的关注'));
      await tester.pumpAndSettle();
      expect(profileReads, 1);
      expect(find.text('当前可见关注者'), findsOneWidget);
      expect(paths.any((p) => p.endsWith('/person-target')), isTrue);
      expect(paths.any((p) => p.contains('conversations')), isFalse);
    },
  );
}
