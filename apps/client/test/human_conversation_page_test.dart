import 'dart:async';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/entity_share_pending_store.dart';
import 'package:birdtie_client/src/workspace/chat_message_pending_store.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'entity_share_pending_store_test.dart';
import 'chat_entity_router_test.dart' show ChatTestAuth, chatReply;

const humanChat = HumanConversation(
  id: shareConversation,
  otherAccountId: sharePeer,
  otherName: '本地合成好友',
);
void main() {
  testWidgets('整个私信路由在账号ABA后删除旧收件人标题与安全动作', (t) async {
    final messageStore = MemoryHumanMessagePendingStore();
    final auth = ChatTestAuth();
    final client = MockClient((_) async => chatReply([]));
    await t.pumpWidget(
      MaterialApp(
        home: HumanConversationRoute(
          auth: auth,
          conversation: humanChat,
          client: client,
          apiBaseUrl: 'https://api.example',
          pendingStore: MemoryEntitySharePendingStore(),
          messagePendingStore: messageStore,
          actions: [TextButton(onPressed: () {}, child: const Text('旧对象安全操作'))],
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.text('本地合成好友'), findsOneWidget);
    auth.use('Bearer B');
    auth.use('Bearer A');
    await t.pumpAndSettle();
    expect(find.text('本地合成好友'), findsNothing);
    expect(find.text('旧对象安全操作'), findsNothing);
    expect(find.text('个人消息'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });
  testWidgets('账号ABA迟到消息不显示也不按新身份markRead', (t) async {
    final messageStore = MemoryHumanMessagePendingStore();
    final auth = ChatTestAuth(), pending = Completer<http.Response>();
    var reads = 0;
    final client = MockClient((r) async {
      if (r.method == 'GET') return pending.future;
      reads++;
      return chatReply({});
    });
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: HumanConversationPage(
            auth: auth,
            conversation: humanChat,
            client: client,
            apiBaseUrl: 'https://api.example',
            pendingStore: MemoryEntitySharePendingStore(),
            messagePendingStore: messageStore,
          ),
        ),
      ),
    );
    auth.use('Bearer B');
    auth.use('Bearer A');
    pending.complete(
      chatReply([
        {
          'id': shareMessage,
          'senderAccountId': sharePeer,
          'body': '旧账号私信正文',
          'createdAt': '2026-10-03T09:00:00Z',
        },
      ]),
    );
    await t.pumpAndSettle();
    expect(reads, 0);
    expect(find.text('旧账号私信正文'), findsNothing);
    expect(find.textContaining('工作身份已变化'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });
  testWidgets('组织ABA即时清掉既有私信与输入', (t) async {
    final messageStore = MemoryHumanMessagePendingStore();
    final auth = ChatTestAuth(), workspace = ValueNotifier<String?>(null);
    final client = MockClient(
      (r) async => r.method == 'GET'
          ? chatReply([
              {
                'id': shareMessage,
                'senderAccountId': sharePeer,
                'body': '本人私信',
                'createdAt': '2026-10-03T09:00:00Z',
              },
            ])
          : chatReply({}),
    );
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: HumanConversationPage(
            auth: auth,
            conversation: humanChat,
            workspaceChanges: workspace,
            workspaceID: () => workspace.value,
            client: client,
            apiBaseUrl: 'https://api.example',
            pendingStore: MemoryEntitySharePendingStore(),
            messagePendingStore: messageStore,
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.enterText(find.byType(TextField), '旧草稿');
    workspace.value = 'organization';
    workspace.value = null;
    await t.pumpAndSettle();
    expect(find.text('本人私信'), findsNothing);
    expect(find.text('旧草稿'), findsNothing);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    workspace.dispose();
    client.close();
  });
  testWidgets('320px大字体键盘与8条待恢复操作可滚动，无Column溢出', (t) async {
    final messageStore = MemoryHumanMessagePendingStore();
    final auth = ChatTestAuth(), store = MemoryEntitySharePendingStore();
    for (var i = 0; i < 8; i++) {
      await store.write(
        'https://api.example',
        shareOwner,
        PendingEntityShare(
          operationID: newEntityShareOperationID(),
          conversationID: shareConversation,
          type: 'moment',
          entityID: shareTarget,
        ),
      );
    }
    final client = MockClient((_) async => chatReply([]));
    t.view.physicalSize = const Size(320, 640);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    await t.pumpWidget(
      MaterialApp(
        home: MediaQuery(
          data: const MediaQueryData(
            size: Size(320, 640),
            textScaler: TextScaler.linear(2),
            viewInsets: EdgeInsets.only(bottom: 260),
          ),
          child: Scaffold(
            body: HumanConversationPage(
              auth: auth,
              conversation: humanChat,
              client: client,
              apiBaseUrl: 'https://api.example',
              pendingStore: store,
              messagePendingStore: messageStore,
            ),
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.text('核实这次卡片分享'), findsWidgets);
    expect(find.byType(TextField), findsOneWidget);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });
}
