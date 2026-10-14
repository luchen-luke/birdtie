import 'package:birdtie_client/src/workspace/friend_image.dart';
import 'friend_image_test.dart' show imageFixtureBytes, imageMetadata;
import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/workspace/chat_message_pending_store.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/friend_audio.dart';
import 'package:birdtie_client/src/workspace/friend_conversation_controller.dart';
import 'package:birdtie_client/src/workspace/friend_conversation_thread.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'friend_audio_test.dart' show audioFixture;

const _owner = '11111111-1111-4111-8111-111111111111';
const _peer = '22222222-2222-4222-8222-222222222222';
const _conversation = '33333333-3333-4333-8333-333333333333';
const _message = '55555555-5555-4555-8555-555555555555';
const _privateBody = '先前已获准读取的私密消息';

class _Auth extends BirdtieAuthController {
  _Auth() : super(client: MockClient((_) async => http.Response('{}', 404)));
  @override
  String get accountID => _owner;
  @override
  String get authorizationHeader => 'Bearer synthetic-a';
  @override
  bool get signedIn => true;
}

class _NoAudio extends FriendAudioPort {
  @override
  bool get supported => false;
  @override
  Future<void> start(String id, ValueChanged<FriendAudioPhase> phase) async =>
      throw StateError('Audio is outside this thread-state test');
  @override
  Future<FriendAudioDraft> stop(String id) async =>
      throw StateError('Audio is outside this thread-state test');
  @override
  Future<void> cancel(String id) async {}
  @override
  Future<void> play(
    String id,
    FriendAudioDraft draft,
    VoidCallback finished,
  ) async => throw StateError('Audio is outside this thread-state test');
  @override
  Future<void> stopPlayback(String id) async {}
  @override
  void dispose() {}
}

http.Response _history(List<Object> messages) => http.Response.bytes(
  utf8.encode(
    jsonEncode({
      'data': {'items': messages, 'nextCursor': null},
    }),
  ),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

Map<String, Object> _oldMessage() => {
  'id': _message,
  'conversationId': _conversation,
  'senderAccountId': _peer,
  'speakerKind': 'human',
  'body': _privateBody,
  'createdAt': '2026-10-11T01:02:03Z',
};

FriendConversationController _controller(
  http.Client client, {
  void Function(String)? onUnauthorized,
  bool imageEnabled = false,
}) {
  final auth = _Auth();
  final c = FriendConversationController(
    auth: auth,
    imageEnabled: imageEnabled, imagePendingStore: MemoryFriendImagePendingStore(),
    isVisible: imageEnabled ? () => true : null,
    conversation: const HumanConversation(
      id: _conversation,
      otherAccountId: _peer,
      otherName: '测试联系人',
    ),
    apiBaseUrl: 'https://unit.example',
    client: client,
    audioPort: _NoAudio(),
    audioPendingStore: MemoryFriendAudioPendingStore(),
    messagePendingStore: MemoryHumanMessagePendingStore(),
    onUnauthorized: onUnauthorized,
  );
  addTearDown(c.dispose);
  addTearDown(auth.dispose);
  addTearDown(client.close);
  return c;
}

Future<void> _paint(WidgetTester tester, FriendConversationController c) =>
    tester.pumpWidget(
      MaterialApp(
        home: Scaffold(body: FriendConversationThread(controller: c)),
      ),
    );

void _expectNoFalseEmptyOrLogin() {
  expect(find.text('还没有消息。'), findsNothing);
  expect(find.textContaining('登录'), findsNothing);
}

void main() {

  for (final scale in [1.0, 2.0]) {
    testWidgets('visible image row has bounded thumbnail only and natural content at font $scale', (tester) async {
      final bytes = (await tester.runAsync(imageFixtureBytes))!, meta = imageMetadata(bytes), variants = <String>[];
      final client = MockClient((r) async {
        if (!r.url.path.endsWith('/image')) return _history([
          {..._oldMessage(), 'body': '保留图片 caption', 'image': meta.toJson()},
        ]);
        variants.add(r.url.queryParameters['variant']!);
        return http.Response.bytes(bytes, 200, headers: {
          'content-type': 'image/png', 'content-length': bytes.length.toString(),
          'x-content-sha256': meta.full.sha256, 'x-image-width': '4', 'x-image-height': '4',
          'x-image-variant': r.url.queryParameters['variant']!,
          'cache-control': 'no-store', 'x-content-type-options': 'nosniff',
        });
      });
      addTearDown(client.close);
      final c = _controller(client, imageEnabled: true);
      await c.initialise();
      await tester.pumpWidget(MaterialApp(builder: (context, child) => MediaQuery(
        data: MediaQuery.of(context).copyWith(textScaler: TextScaler.linear(scale)), child: child!),
        home: Scaffold(body: SizedBox(width: 390, height: 420,
            child: FriendConversationThread(controller: c)))));
      await tester.pumpAndSettle();
      expect(variants, ['thumbnail']);
      expect(find.text('保留图片 caption'), findsOneWidget);
      expect(find.text('查看原图'), findsOneWidget);
      expect(find.byType(TextField), findsNothing);
      expect(tester.takeException(), isNull);
    });
  }


  testWidgets('image row preserves caption and has no extra input or automatic full-image read', (tester) async {
    final bytes = (await tester.runAsync(imageFixtureBytes))!, calls = <String>[];
    final client = MockClient((request) async {
      calls.add(request.url.toString());
      return _history([{..._oldMessage(), 'body': '图片消息', 'image': imageMetadata(bytes).toJson()}]);
    });
    addTearDown(client.close);
    final c = _controller(client);
    await c.initialise();
    await tester.pumpWidget(MaterialApp(home: Scaffold(body: FriendConversationThread(controller: c))));
    await tester.pump();
    expect(find.byType(TextField), findsNothing);
    expect(find.text('图片消息'), findsNothing);
    expect(find.text('当前版本暂不支持查看图片'), findsOneWidget);
    expect(calls.where((url) => url.contains('/image?')), isEmpty);
  });

  testWidgets('uninitialised history is not a verified empty conversation', (
    tester,
  ) async {
    var reads = 0;
    final c = _controller(
      MockClient((_) async {
        reads++;
        return _history([]);
      }),
    );
    await _paint(tester, c);
    expect(c.current, isTrue);
    expect(c.historyAvailable, isFalse);
    expect(c.loading, isFalse);
    expect(c.error, isNull);
    expect(reads, 0);
    _expectNoFalseEmptyOrLogin();
  });

  testWidgets(
    'cancelling a retained audio draft cannot clear HTTP404 permission failure',
    (tester) async {
      var reads = 0, rejected = 0, recovered = false;
      final c = _controller(
        MockClient((r) async {
          expect(r.method, 'GET');
          expect(
            r.url.path,
            '/v1/me/conversations/$_conversation/messages/page',
          );
          if (++reads == 1) return _history([_oldMessage()]);
          return recovered ? _history([]) : http.Response('{}', 404);
        }),
        onUnauthorized: (_) => rejected++,
      );
      await _paint(tester, c);
      await c.initialise();
      await tester.pump();
      expect(find.text(_privateBody), findsOneWidget);
      expect(
        c.restoreAudioDraft(
          audioFixture(),
          owner: _owner,
          conversationID: _conversation,
        ),
        isTrue,
      );
      await c.load();
      await tester.pump();
      expect(c.audioDraft, isNotNull);
      expect(c.messages, isEmpty);
      expect(c.historyAvailable, isFalse);
      expect(find.text(_privateBody), findsNothing);
      expect(find.text('这段对话已不可访问，请返回联系人核对。'), findsOneWidget);
      _expectNoFalseEmptyOrLogin();

      await tester.tap(find.byTooltip('取消这段语音'));
      await tester.pump();
      expect(reads, 2);
      expect(rejected, 0);
      expect(c.current, isTrue);
      expect(c.historyAvailable, isFalse);
      expect(c.audioDraft, isNull);
      expect(c.error, '这段对话已不可访问，请返回联系人核对。');
      expect(find.text('这段对话已不可访问，请返回联系人核对。'), findsOneWidget);
      expect(find.text(_privateBody), findsNothing);
      _expectNoFalseEmptyOrLogin();

      recovered = true;
      await c.load();
      await tester.pump();
      expect(reads, 3);
      expect(rejected, 0);
      expect(c.error, isNull);
      expect(c.historyAvailable, isTrue);
      expect(find.text('还没有消息。'), findsOneWidget);
    },
  );

  for (final status in [403, 404]) {
    testWidgets(
      'HTTP $status clears prior private history and shows only the permission error',
      (tester) async {
        var reads = 0, rejected = 0;
        final c = _controller(
          MockClient((r) async {
            expect(r.method, 'GET');
            expect(
              r.url.path,
              '/v1/me/conversations/$_conversation/messages/page',
            );
            expect(r.headers['authorization'], 'Bearer synthetic-a');
            return ++reads == 1
                ? _history([_oldMessage()])
                : http.Response('{}', status);
          }),
          onUnauthorized: (_) => rejected++,
        );
        await _paint(tester, c);
        await c.initialise();
        await tester.pump();
        expect(find.text(_privateBody), findsOneWidget);

        await c.load();
        await tester.pump();
        expect(reads, 2);
        expect(rejected, 0);
        expect(c.current, isTrue);
        expect(c.messages, isEmpty);
        expect(find.text(_privateBody), findsNothing);
        expect(
          find.text(status == 403 ? '当前身份没有对话访问权限。' : '这段对话已不可访问，请返回联系人核对。'),
          findsOneWidget,
        );
        _expectNoFalseEmptyOrLogin();
      },
    );
  }

  for (final offline in [false, true]) {
    testWidgets(
      '${offline ? 'offline' : 'HTTP 503'} is an error until an authorised empty history succeeds',
      (tester) async {
        var available = false, reads = 0, rejected = 0;
        final c = _controller(
          MockClient((r) async {
            expect(r.method, 'GET');
            reads++;
            if (available) return _history([]);
            if (offline) throw http.ClientException('synthetic offline');
            return http.Response('{}', 503);
          }),
          onUnauthorized: (_) => rejected++,
        );
        await _paint(tester, c);
        await c.initialise();
        await tester.pump();
        expect(c.current, isTrue);
        expect(
          find.text(offline ? '连接中断，请检查网络后重试。' : '消息服务暂不可用，请稍后重试。'),
          findsOneWidget,
        );
        _expectNoFalseEmptyOrLogin();

        available = true;
        await c.load();
        await tester.pump();
        expect(reads, 2);
        expect(rejected, 0);
        expect(c.error, isNull);
        expect(find.text('还没有消息。'), findsOneWidget);
        expect(find.textContaining('登录'), findsNothing);
      },
    );
  }

  testWidgets('offline refresh preserves previously authorised history', (
    tester,
  ) async {
    var reads = 0, rejected = 0;
    final c = _controller(
      MockClient((r) async {
        expect(r.method, 'GET');
        if (++reads == 1) return _history([_oldMessage()]);
        throw http.ClientException('synthetic offline');
      }),
      onUnauthorized: (_) => rejected++,
    );
    await _paint(tester, c);
    await c.initialise();
    await tester.pump();
    await c.load();
    await tester.pump();
    expect(reads, 2);
    expect(rejected, 0);
    expect(c.current, isTrue);
    expect(find.text(_privateBody), findsOneWidget);
    expect(find.text('连接中断，请检查网络后重试。'), findsOneWidget);
    _expectNoFalseEmptyOrLogin();
  });

  testWidgets('valid HTTP 200 empty history shows one empty state', (
    tester,
  ) async {
    final c = _controller(MockClient((_) async => _history([])));
    await _paint(tester, c);
    await c.initialise();
    await tester.pump();
    expect(c.error, isNull);
    expect(find.text('还没有消息。'), findsOneWidget);
    expect(find.textContaining('登录'), findsNothing);
  });

  testWidgets('pending history does not claim the conversation is empty', (
    tester,
  ) async {
    final reply = Completer<http.Response>();
    final c = _controller(MockClient((_) => reply.future));
    await _paint(tester, c);
    final loading = c.initialise();
    await tester.pump();
    expect(find.byType(LinearProgressIndicator), findsOneWidget);
    _expectNoFalseEmptyOrLogin();
    reply.complete(_history([]));
    await loading;
    await tester.pump();
    expect(find.byType(LinearProgressIndicator), findsNothing);
    expect(find.text('还没有消息。'), findsOneWidget);
  });
}
