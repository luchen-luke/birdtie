import 'dart:async';
import 'dart:io';
import 'dart:convert';
import 'dart:ui' show SemanticsAction;
import 'package:birdtie_client/src/workspace/chat_entity_router.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/entity_share_pending_store.dart';
import 'package:birdtie_client/src/workspace/chat_message_pending_store.dart';
import 'package:birdtie_client/src/workspace/chat_message_operation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'chat_entity_router_test.dart'
    show ChatTestAuth, chatReply, chatActivity;
import 'human_conversation_page_test.dart' show humanChat;
import 'entity_share_pending_store_test.dart';
import 'share_entity_to_chat_test.dart' show ShareFixture;

class BindingOwnedChatClient implements HttpClient {
  int closes = 0;
  @override
  void close({bool force = false}) {
    closes++;
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class BindingChatChanges extends ChangeNotifier {
  bool get listening => hasListeners;
}

class BindingChatClient extends MockClient {
  BindingChatClient(super.fn);
  int closes = 0;
  @override
  void close() {
    closes++;
    super.close();
  }
}

Map<String, dynamic> bindingMessage() => {
  'id': shareMessage,
  'senderAccountId': sharePeer,
  'body': '旧连接私信正文',
  'createdAt': '2026-10-03T09:00:00Z',
};

Map<String, dynamic> bindingPlainReceipt(http.Request request) {
  final body = jsonDecode(request.body) as Map<String, dynamic>;
  final operationID = body['operationId'] as String;
  return {
    'schemaVersion': humanMessageOperationSchema,
    'ownerId': shareOwner,
    'conversationId': shareConversation,
    'operationId': operationID,
    'effectKey': humanMessageEffectKey(shareOwner, operationID),
    'payloadDigest': humanMessagePayloadDigest(shareConversation, body['body']),
    'toolVersion': humanMessageToolVersion,
    'messageId': shareMessage,
    'createdAt': '2026-10-07T01:02:03Z',
  };
}

void main() {
  for (final height in [800.0, 820.0]) {
    testWidgets(
      'normal $height height allocates remaining body to messages and anchors composer',
      (t) async {
        final messageStore = MemoryHumanMessagePendingStore();
        final auth = ChatTestAuth();
        final client = BindingChatClient((_) async => chatReply([]));
        t.view.physicalSize = Size(360, height);
        t.view.devicePixelRatio = 1;
        addTearDown(t.view.resetPhysicalSize);
        addTearDown(t.view.resetDevicePixelRatio);
        await t.pumpWidget(
          MaterialApp(
            home: HumanConversationRoute(
              auth: auth,
              conversation: humanChat,
              client: client,
              apiBaseUrl: 'https://old.example',
              pendingStore: MemoryEntitySharePendingStore(),
              messagePendingStore: messageStore,
            ),
          ),
        );
        await t.pumpAndSettle();
        final body = t.getRect(find.byType(HumanConversationPage));
        final messages = t.getRect(find.byType(ListView));
        final input = t.getRect(find.byType(TextField));
        expect(messages.height, greaterThan(body.height * .7));
        expect(input.bottom, greaterThanOrEqualTo(body.bottom - 24));
        expect(input.bottom, lessThanOrEqualTo(body.bottom));
        expect(
          t.getSize(find.byTooltip('发送消息')).height,
          greaterThanOrEqualTo(48),
        );
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        expect(client.closes, 0);
        auth.dispose();
      },
    );
  }
  for (final fail in [false, true]) {
    testWidgets(
      'nested original-key approval late ${fail ? 'unknown' : '201'} retires all overlays without deleting pending',
      (t) async {
        final messageStore = MemoryHumanMessagePendingStore();
        final auth = ChatTestAuth(), f = ShareFixture();
        f.starts = 1;
        f.missing = true;
        f.pendingWrite = Completer<http.Response>();
        await f.store.write('https://api.example', shareOwner, pendingShare);
        final client = BindingChatClient((r) async {
          if (r.url.path.endsWith('/messages')) return chatReply([]);
          return r.method == 'GET'
              ? f.client.get(r.url, headers: r.headers)
              : f.client.post(r.url, headers: r.headers, body: r.body);
        });
        Widget page(String base) => MaterialApp(
          home: HumanConversationRoute(
            key: const ValueKey('same'),
            auth: auth,
            conversation: humanChat,
            apiBaseUrl: base,
            client: client,
            pendingStore: f.store,
            messagePendingStore: messageStore,
          ),
        );
        await t.pumpWidget(page('https://api.example'));
        await t.pumpAndSettle();
        await t.tap(find.text('核实这次卡片分享'));
        await t.pumpAndSettle();
        await t.ensureVisible(find.text('检查并重试原分享'));
        await t.tap(find.text('检查并重试原分享'));
        await t.pumpAndSettle();
        expect(find.text('重试原分享'), findsOneWidget);
        await t.tap(find.text('确认重试原分享'));
        await t.pump();
        expect(f.posted, [shareOperation]);
        await t.pumpWidget(page('https://new.example'));
        await t.pumpAndSettle();
        if (fail) {
          f.pendingWrite!.completeError(http.ClientException('合成本地丢响应'));
        } else {
          f.pendingWrite!.complete(
            chatReply({
              'operationId': shareOperation,
              'message': {
                ...bindingMessage(),
                'conversationId': shareConversation,
                'senderAccountId': shareOwner,
                'speakerKind': 'human',
                'body': '分享了一张卡片',
                'entity': {
                  'type': 'moment',
                  'id': shareTarget,
                  'title': '当前公开动态',
                  'available': true,
                },
              },
            }, 201),
          );
        }
        await t.pumpAndSettle();
        expect(f.posted, [shareOperation]);
        expect(find.byType(AlertDialog), findsNothing);
        expect(find.textContaining('已发送到私信'), findsNothing);
        expect(find.text('发给好友'), findsNothing);
        expect(
          (await f.store.read(
            'https://api.example',
            shareOwner,
          )).single.toJson(),
          pendingShare.toJson(),
        );
        expect(await f.store.read('https://new.example', shareOwner), isEmpty);
        await t.pumpWidget(const SizedBox());
        expect(client.closes, 0);
        auth.dispose();
        f.dispose();
      },
    );
  }
  testWidgets(
    'retired route large text narrow keyboard has scrollable semantic return action',
    (t) async {
      final messageStore = MemoryHumanMessagePendingStore();
      final auth = ChatTestAuth(),
          client = BindingChatClient((_) async => chatReply([]));
      t.view.physicalSize = const Size(320, 640);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final store = MemoryEntitySharePendingStore();
      Widget page(String base) => MaterialApp(
        builder: (c, child) => MediaQuery(
          data: MediaQuery.of(c).copyWith(
            textScaler: const TextScaler.linear(3),
            viewInsets: const EdgeInsets.only(bottom: 260),
          ),
          child: child!,
        ),
        home: HumanConversationRoute(
          key: const ValueKey('same'),
          auth: auth,
          conversation: humanChat,
          client: client,
          apiBaseUrl: base,
          pendingStore: store,
          messagePendingStore: messageStore,
        ),
      );
      await t.pumpWidget(page('https://old.example'));
      await t.pumpAndSettle();
      await t.pumpWidget(page('https://new.example'));
      await t.pumpAndSettle();
      await t.scrollUntilVisible(
        find.text('返回个人消息'),
        120,
        scrollable: find.byType(Scrollable).first,
      );
      expect(
        t.getSize(find.widgetWithText(TextButton, '返回个人消息')).height,
        greaterThanOrEqualTo(48),
      );
      final handle = t.ensureSemantics();
      expect(
        t
            .getSemantics(find.widgetWithText(TextButton, '返回个人消息'))
            .getSemanticsData()
            .hasAction(SemanticsAction.tap),
        true,
      );
      handle.dispose();
      await t.tap(find.text('返回个人消息'));
      await t.pumpAndSettle();
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      expect(client.closes, 0);
      auth.dispose();
    },
  );
  for (final route in [false, true]) {
    for (final changed in [
      'base',
      'client',
      'store',
      'getter',
      'listener',
      'auth',
      'peer',
    ]) {
      testWidgets(
        '${route ? 'route' : 'body'} $changed same key permanently retires and preserves exact pending partition',
        (t) async {
          final messageStore = MemoryHumanMessagePendingStore();
          final auth = ChatTestAuth(), otherAuth = ChatTestAuth();
          final firstChanges = BindingChatChanges(),
              secondChanges = BindingChatChanges();
          final store = MemoryEntitySharePendingStore(),
              otherStore = MemoryEntitySharePendingStore();
          await store.write('https://old.example', shareOwner, pendingShare);
          final requests = <http.Request>[];
          http.Response reply(http.Request r) {
            requests.add(r);
            return chatReply(r.method == 'GET' ? [bindingMessage()] : {});
          }

          final a = BindingChatClient((r) async => reply(r));
          final b = BindingChatClient((r) async => reply(r));
          String? originalGetter() => null;
          String? replacementGetter() => null;
          Widget page(bool replacement) {
            final currentAuth = replacement && changed == 'auth'
                ? otherAuth
                : auth;
            final client = replacement && changed == 'client' ? b : a;
            final base = replacement && changed == 'base'
                ? 'https://new.example'
                : 'https://old.example';
            final changes = replacement && changed == 'listener'
                ? secondChanges
                : firstChanges;
            final getter = replacement && changed == 'getter'
                ? replacementGetter
                : originalGetter;
            final pending = replacement && changed == 'store'
                ? otherStore
                : store;
            final conversation = replacement && changed == 'peer'
                ? const HumanConversation(
                    id: shareConversation,
                    otherAccountId: shareTarget,
                    otherName: '另一收件人',
                  )
                : humanChat;
            final child = route
                ? HumanConversationRoute(
                    key: const ValueKey('same'),
                    auth: currentAuth,
                    conversation: conversation,
                    client: client,
                    apiBaseUrl: base,
                    workspaceChanges: changes,
                    workspaceID: getter,
                    pendingStore: pending,
                    messagePendingStore: messageStore,
                  )
                : Scaffold(
                    body: HumanConversationPage(
                      key: const ValueKey('same'),
                      auth: currentAuth,
                      conversation: conversation,
                      client: client,
                      apiBaseUrl: base,
                      workspaceChanges: changes,
                      workspaceID: getter,
                      pendingStore: pending,
                      messagePendingStore: messageStore,
                    ),
                  );
            return MaterialApp(home: child);
          }

          await t.pumpWidget(page(false));
          await t.pumpAndSettle();
          expect(find.text('旧连接私信正文'), findsOneWidget);
          expect(find.text('核实这次卡片分享'), findsOneWidget);
          await t.enterText(find.byType(TextField), '原连接草稿');
          final count = requests.length;
          await t.pumpWidget(page(true));
          await t.pumpAndSettle();
          await t.pumpWidget(page(false));
          await t.pumpAndSettle();
          expect(requests.length, count);
          expect(find.text('旧连接私信正文'), findsNothing);
          expect(find.text('原连接草稿'), findsNothing);
          expect(find.byType(TextField), findsNothing);
          expect(firstChanges.listening, false);
          expect(secondChanges.listening, false);
          expect(
            (await store.read(
              'https://old.example',
              shareOwner,
            )).single.toJson(),
            pendingShare.toJson(),
          );
          expect(await store.read('https://new.example', shareOwner), isEmpty);
          expect(otherStore.values, isEmpty);
          await t.pumpWidget(const SizedBox());
          expect(a.closes, 0);
          expect(b.closes, 0);
          auth.dispose();
          otherAuth.dispose();
          firstChanges.dispose();
          secondChanges.dispose();
        },
      );
    }
  }

  testWidgets(
    'normal current conversation sends with captured transport and reloads',
    (t) async {
      final messageStore = MemoryHumanMessagePendingStore();
      final auth = ChatTestAuth(), requests = <http.Request>[];
      final client = BindingChatClient((r) async {
        requests.add(r);
        if (r.method == 'POST' && r.url.path.endsWith('/message-operations')) {
          return chatReply(bindingPlainReceipt(r), 201);
        }
        return chatReply(
          r.method == 'GET'
              ? [bindingMessage()]
              : {...bindingMessage(), 'senderAccountId': shareOwner},
        );
      });
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: HumanConversationPage(
              auth: auth,
              conversation: humanChat,
              client: client,
              apiBaseUrl: 'https://old.example',
              pendingStore: MemoryEntitySharePendingStore(),
              messagePendingStore: messageStore,
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      await t.enterText(find.byType(TextField), '本人明确消息');
      await t.tap(find.byTooltip('发送消息'));
      await t.pumpAndSettle();
      expect(
        requests
            .where(
              (r) => r.method == 'POST' && r.url.path.endsWith('/message-operations'),
            )
            .length,
        1,
      );
      expect(
        requests.every(
          (r) =>
              r.url.host == 'old.example' &&
              r.headers['Authorization'] == 'Bearer A',
        ),
        true,
      );
      expect(find.text('旧连接私信正文'), findsOneWidget);
      expect(
        t.widget<TextField>(find.byType(TextField)).controller!.text,
        isEmpty,
      );
      await t.pumpWidget(const SizedBox());
      expect(client.closes, 0);
      auth.dispose();
    },
  );

  testWidgets(
    'late message GET after transport replacement cannot mark read or display',
    (t) async {
      final messageStore = MemoryHumanMessagePendingStore();
      final auth = ChatTestAuth(), pending = Completer<http.Response>();
      final requests = <http.Request>[];
      final client = BindingChatClient((r) {
        requests.add(r);
        return pending.future;
      });
      Widget page(String base) => MaterialApp(
        home: Scaffold(
          body: HumanConversationPage(
            key: const ValueKey('body'),
            auth: auth,
            conversation: humanChat,
            client: client,
            apiBaseUrl: base,
            pendingStore: MemoryEntitySharePendingStore(),
            messagePendingStore: messageStore,
          ),
        ),
      );
      await t.pumpWidget(page('https://old.example'));
      await t.pumpWidget(page('https://new.example'));
      pending.complete(chatReply([bindingMessage()]));
      await t.pumpAndSettle();
      expect(requests.length, 1);
      expect(find.text('旧连接私信正文'), findsNothing);
      await t.pumpWidget(const SizedBox());
      expect(client.closes, 0);
      auth.dispose();
    },
  );

  for (final failure in [false, true]) {
    testWidgets(
      'late plain send ${failure ? 'unknown' : 'reply'} after source replacement cannot claim success or retry',
      (t) async {
        final messageStore = MemoryHumanMessagePendingStore();
        final auth = ChatTestAuth(), pending = Completer<http.Response>();
        var sends = 0, gets = 0;
        http.Request? submitted;
        final client = BindingChatClient((r) {
          if (r.method == 'POST' && r.url.path.endsWith('/message-operations')) {
            sends++;
            submitted = r;
            return pending.future;
          }
          if (r.method == 'GET') gets++;
          return Future.value(chatReply(r.method == 'GET' ? [] : {}));
        });
        final store = MemoryEntitySharePendingStore();
        Widget page(String base) => MaterialApp(
          home: Scaffold(
            body: HumanConversationPage(
              key: const ValueKey('body'),
              auth: auth,
              conversation: humanChat,
              client: client,
              apiBaseUrl: base,
              pendingStore: store,
              messagePendingStore: messageStore,
            ),
          ),
        );
        await t.pumpWidget(page('https://old.example'));
        await t.pumpAndSettle();
        await t.enterText(find.byType(TextField), '本人消息');
        await t.tap(find.byTooltip('发送消息'));
        await t.pump();
        await t.pumpWidget(page('https://new.example'));
        await t.pumpAndSettle();
        if (failure) {
          pending.completeError(http.ClientException('合成响应未知'));
        } else {
          pending.complete(chatReply(bindingPlainReceipt(submitted!), 201));
        }
        await t.pumpAndSettle();
        expect(sends, 1);
        expect(gets, 1);
        expect(find.byType(TextField), findsNothing);
        expect(find.text('发送结果暂未确认，请先查看会话。'), findsNothing);
        await t.pumpWidget(const SizedBox());
        expect(client.closes, 0);
        auth.dispose();
      },
    );
  }

  for (final owner in ['router', 'body', 'source']) {
    testWidgets(
      '$owner actual owned IO client remains owned across replacement and closes exactly once',
      (t) async {
        final messageStore = MemoryHumanMessagePendingStore();
        final auth = ChatTestAuth(), owned = BindingOwnedChatClient();
        final borrowed = BindingChatClient((_) async => chatReply([]));
        await HttpOverrides.runZoned(() async {
          if (owner == 'source') {
            final source = ConnectionSource(
              authorizationHeader: () => 'Bearer A',
            );
            source.dispose();
            source.dispose();
            expect(owned.closes, 1);
            await expectLater(
              source.messages(shareConversation),
              throwsStateError,
            );
          } else {
            Widget page(http.Client? client) => MaterialApp(
              home: owner == 'router'
                  ? ChatEntityDetail(
                      key: const ValueKey('owned'),
                      type: 'activity',
                      id: shareTarget,
                      auth: auth,
                      client: client,
                      apiBaseUrl: 'https://fixture.invalid',
                    )
                  : Scaffold(
                      body: HumanConversationPage(
                        key: const ValueKey('owned'),
                        auth: auth,
                        conversation: humanChat,
                        client: client,
                        apiBaseUrl: 'https://fixture.invalid',
                        pendingStore: MemoryEntitySharePendingStore(),
                        messagePendingStore: messageStore,
                      ),
                    ),
            );
            await t.pumpWidget(page(null));
            await t.pumpAndSettle();
            await t.pumpWidget(page(borrowed));
            await t.pumpAndSettle();
            await t.pumpWidget(const SizedBox());
            expect(owned.closes, 1);
          }
        }, createHttpClient: (_) => owned);
        expect(borrowed.closes, 0);
        auth.dispose();
      },
    );
  }

  testWidgets(
    'nested share approval retires with conversation source; original unknown reference is retained',
    (t) async {
      final messageStore = MemoryHumanMessagePendingStore();
      final auth = ChatTestAuth(), f = ShareFixture();
      await f.store.write('https://api.example', shareOwner, pendingShare);
      final messagesClient = BindingChatClient((r) async {
        if (r.url.path.endsWith('/messages')) return chatReply([]);
        return r.method == 'GET'
            ? f.client.get(r.url, headers: r.headers)
            : f.client.post(r.url, headers: r.headers, body: r.body);
      });
      Widget page(String base) => MaterialApp(
        home: HumanConversationRoute(
          key: const ValueKey('chat'),
          auth: auth,
          conversation: humanChat,
          client: messagesClient,
          apiBaseUrl: base,
          pendingStore: f.store,
          messagePendingStore: messageStore,
        ),
      );
      await t.pumpWidget(page('https://api.example'));
      await t.pumpAndSettle();
      await t.tap(find.text('核实这次卡片分享'));
      await t.pumpAndSettle();
      expect(find.text('确认发送卡片'), findsNothing);
      expect(find.textContaining('核实原'), findsWidgets);
      await t.pumpWidget(page('https://new.example'));
      await t.pumpAndSettle();
      expect(find.textContaining('本地合成好友'), findsNothing);
      expect(find.byType(AlertDialog), findsNothing);
      expect(f.posted, isEmpty);
      expect(
        (await f.store.read('https://api.example', shareOwner)).single.toJson(),
        pendingShare.toJson(),
      );
      expect(await f.store.read('https://new.example', shareOwner), isEmpty);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      f.dispose();
      expect(messagesClient.closes, 0);
    },
  );

  testWidgets(
    'RED same key endpoint replacement retires old conversation and cannot send to old host',
    (t) async {
      final messageStore = MemoryHumanMessagePendingStore();
      final auth = ChatTestAuth(), calls = <http.Request>[];
      final client = BindingChatClient((r) async {
        calls.add(r);
        return chatReply(r.method == 'GET' ? [bindingMessage()] : {});
      });
      final store = MemoryEntitySharePendingStore();
      Widget page(String base) => MaterialApp(
        home: HumanConversationRoute(
          key: const ValueKey('chat'),
          auth: auth,
          conversation: humanChat,
          client: client,
          apiBaseUrl: base,
          pendingStore: store,
          messagePendingStore: messageStore,
          actions: [TextButton(onPressed: () {}, child: const Text('旧收件人操作'))],
        ),
      );
      await t.pumpWidget(page('https://old.example'));
      await t.pumpAndSettle();
      expect(find.text('旧连接私信正文'), findsOneWidget);
      await t.enterText(find.byType(TextField), '旧批准草稿');
      final prior = calls.length;
      await t.pumpWidget(page('https://new.example'));
      await t.pumpAndSettle();
      if (find.byType(TextField).evaluate().isNotEmpty) {
        await t.tap(find.byTooltip('发送消息'));
        await t.pumpAndSettle();
      }
      expect(
        calls.length,
        prior,
        reason: 'replacement must not send draft to original transport',
      );
      expect(find.text('旧连接私信正文'), findsNothing);
      expect(find.text('旧收件人操作'), findsNothing);
      await t.pumpWidget(page('https://old.example'));
      await t.pumpAndSettle();
      expect(find.byType(TextField), findsNothing);
      await t.pumpWidget(const SizedBox());
      expect(client.closes, 0);
      auth.dispose();
    },
  );

  testWidgets('RED borrowed router client replaced with null is never closed', (
    t,
  ) async {
    final auth = ChatTestAuth(),
        client = BindingChatClient((_) async => chatReply([]));
    Widget page(http.Client? c) => MaterialApp(
      home: ChatEntityDetail(
        key: const ValueKey('detail'),
        type: 'activity',
        id: shareTarget,
        auth: auth,
        client: c,
        apiBaseUrl: 'https://old.example',
      ),
    );
    await t.pumpWidget(page(client));
    await t.pumpAndSettle();
    await t.pumpWidget(page(null));
    await t.pumpAndSettle();
    await t.pumpWidget(const SizedBox());
    expect(client.closes, 0);
    auth.dispose();
  });

  testWidgets(
    'RED late activity from old endpoint after same key replacement never projects into new frame',
    (t) async {
      final auth = ChatTestAuth(), pending = Completer<http.Response>();
      final calls = <http.Request>[];
      final client = BindingChatClient((r) {
        calls.add(r);
        return pending.future;
      });
      Widget page(String base) => MaterialApp(
        home: ChatEntityDetail(
          key: const ValueKey('detail'),
          type: 'activity',
          id: shareTarget,
          auth: auth,
          client: client,
          apiBaseUrl: base,
        ),
      );
      await t.pumpWidget(page('https://old.example'));
      await t.pumpWidget(page('https://new.example'));
      pending.complete(chatReply(chatActivity()..['title'] = '旧连接活动正文'));
      await t.pumpAndSettle();
      expect(find.text('旧连接活动正文'), findsNothing);
      expect(calls.length, 1);
      expect(find.textContaining('重新'), findsWidgets);
      await t.pumpWidget(const SizedBox());
      expect(client.closes, 0);
      auth.dispose();
    },
  );
}
