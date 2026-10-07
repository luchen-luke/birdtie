import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/entity_share_pending_store.dart';
import 'package:birdtie_client/src/workspace/chat_message_pending_store.dart';
import 'package:birdtie_client/src/workspace/chat_message_operation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'chat_entity_router_test.dart' show ChatTestAuth, chatReply;
import 'human_conversation_page_test.dart' show humanChat;
import 'entity_share_pending_store_test.dart';

Map<String, dynamic> receipt(PendingHumanMessage p) => {
  'schemaVersion': humanMessageOperationSchema,
  'ownerId': shareOwner,
  'conversationId': p.conversationID,
  'operationId': p.operationID,
  'effectKey': humanMessageEffectKey(shareOwner, p.operationID),
  'payloadDigest': p.payloadDigest,
  'toolVersion': humanMessageToolVersion,
  'messageId': shareMessage,
  'createdAt': '2026-10-07T01:02:03Z',
};

class OperationWireClient extends http.BaseClient {
  int posts = 0, operationReads = 0, historyGets = 0;
  List<Object> history = [];
  bool lost = true;
  int readStatus = 200;
  PendingHumanMessage? captured;
  Completer<http.Response>? postGate;
  final methods = <String>[];
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) {
    methods.add('${request.method} ${request.url.path}');
    Future<http.Response> response;
    if (request.method == 'POST' &&
        (request.url.path.endsWith('/messages') ||
            request.url.path.endsWith('/message-operations'))) {
      posts++;
      final body =
          jsonDecode((request as http.Request).body) as Map<String, dynamic>;
      if (body['operationId'] is String) {
        captured = PendingHumanMessage(
          operationID: body['operationId'],
          conversationID: shareConversation,
          payloadDigest: humanMessagePayloadDigest(
            shareConversation,
            body['body'],
          ),
        );
      }
      response =
          postGate?.future ??
          (lost
              ? Future.error(http.ClientException('synthetic lost receipt'))
              : Future.value(chatReply(receipt(captured!), 201)));
    } else if (request.method == 'GET' &&
        request.url.path.contains('/message-operations/')) {
      operationReads++;
      response = Future.value(
        readStatus == 200
            ? chatReply(receipt(captured!))
            : chatReply({}, readStatus),
      );
    } else {
      if (request.method == 'GET' && request.url.path.endsWith('/messages')) {
        historyGets++;
      }
      response = Future.value(chatReply(history));
    }
    return response.then(
      (r) => http.StreamedResponse(Stream.value(r.bodyBytes), r.statusCode),
    );
  }
}

class ControlledMessageStore implements HumanMessagePendingStore {
  final memory = MemoryHumanMessagePendingStore();
  bool fail = false, readFail = false;
  Completer<void>? gate;
  Completer<void>? deleteGate;
  int deletes = 0;
  Completer<List<PendingHumanMessage>>? readGate;
  int writes = 0;
  @override
  Future<List<PendingHumanMessage>> read(String e, String o) => readFail
      ? Future.error(StateError('synthetic journal read failure'))
      : (readGate?.future ?? memory.read(e, o));
  @override
  Future<void> write(String e, String o, PendingHumanMessage p) async {
    writes++;
    if (fail) throw StateError('synthetic disk failure');
    await memory.write(e, o, p);
    if (gate != null) await gate!.future;
  }

  @override
  Future<void> delete(String e, String o, PendingHumanMessage p) async {
    deletes++;
    if (deleteGate != null) await deleteGate!.future;
    await memory.delete(e, o, p);
  }
}

Future<void> mount(
  WidgetTester t,
  ChatTestAuth auth,
  OperationWireClient wire,
  HumanMessagePendingStore store, {
  String? Function()? workspaceID,
  Listenable? changes,
}) async {
  await t.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: HumanConversationPage(
          auth: auth,
          conversation: humanChat,
          client: wire,
          apiBaseUrl: 'https://api.example',
          pendingStore: MemoryEntitySharePendingStore(),
          messagePendingStore: store,
          workspaceID: workspaceID,
          workspaceChanges: changes,
        ),
      ),
    ),
  );
  await t.pumpAndSettle();
}

Future<void> send(WidgetTester t) async {
  await t.enterText(find.byType(TextField), '本地合成消息');
  await t.tap(find.byTooltip('发送消息').hitTestable());
  await t.pumpAndSettle();
}

Future<void> close(
  WidgetTester t,
  ChatTestAuth auth,
  OperationWireClient wire,
) async {
  await t.pumpWidget(const SizedBox());
  auth.dispose();
  wire.close();
}

void main() {
  testWidgets('same human send with lost reply cannot issue a second POST', (
    t,
  ) async {
    final auth = ChatTestAuth(),
        wire = OperationWireClient(),
        store = MemoryHumanMessagePendingStore();
    await mount(t, auth, wire, store);
    await send(t);
    expect(wire.posts, 1);
    expect(
      t
          .widget<IconButton>(
            find.byWidgetPredicate(
              (w) => w is IconButton && w.tooltip == '发送消息',
            ),
          )
          .onPressed,
      isNull,
    );
    await t.testTextInput.receiveAction(TextInputAction.done);
    await t.pumpAndSettle();
    expect(
      wire.posts,
      1,
      reason: 'unknown response is not a new send approval',
    );
    expect(find.text('核实这次发送'), findsOneWidget);
    expect(store.values.length, 1);
    await close(t, auth, wire);
  });
  testWidgets(
    'current human success records same effect and clears only journal',
    (t) async {
      final auth = ChatTestAuth(),
          wire = OperationWireClient()..lost = false,
          store = MemoryHumanMessagePendingStore();
      await mount(t, auth, wire, store);
      await send(t);
      expect(wire.posts, 1);
      expect(store.values, isEmpty);
      expect(
        t.widget<TextField>(find.byType(TextField)).controller!.text,
        isEmpty,
      );
      expect(find.text('已核实消息已保存到会话，不代表对方已阅读。'), findsOneWidget);
      await close(t, auth, wire);
    },
  );
  testWidgets(
    'close and reopen restores metadata and only GET verifies original effect',
    (t) async {
      final auth = ChatTestAuth(),
          wire = OperationWireClient(),
          store = MemoryHumanMessagePendingStore();
      await mount(t, auth, wire, store);
      await send(t);
      final original = wire.captured!;
      await t.pumpWidget(const SizedBox());
      await mount(t, auth, wire, store);
      expect(
        t.widget<TextField>(find.byType(TextField)).controller!.text,
        isEmpty,
      );
      expect(wire.posts, 1);
      await t.tap(find.text('核实这次发送').hitTestable());
      await t.pumpAndSettle();
      expect(wire.posts, 1);
      expect(wire.operationReads, 1);
      expect(store.values, isEmpty);
      expect(
        wire.methods.lastWhere((s) => s.contains('/message-operations/')),
        endsWith(original.operationID),
      );
      await close(t, auth, wire);
    },
  );
  testWidgets('absent operation is unknown and cannot unlock a new send', (
    t,
  ) async {
    final auth = ChatTestAuth(),
        wire = OperationWireClient()..readStatus = 404,
        store = MemoryHumanMessagePendingStore();
    await mount(t, auth, wire, store);
    await send(t);
    await t.tap(find.text('核实这次发送').hitTestable());
    await t.pumpAndSettle();
    expect(wire.operationReads, 1);
    expect(wire.posts, 1);
    expect(store.values.length, 1);
    expect(
      t
          .widget<IconButton>(
            find.byWidgetPredicate(
              (w) => w is IconButton && w.tooltip == '发送消息',
            ),
          )
          .onPressed,
      isNull,
    );
    expect(find.textContaining('原记录已保留'), findsOneWidget);
    await close(t, auth, wire);
  });
  testWidgets(
    'failed durable journal means zero POST and refresh is required',
    (t) async {
      final auth = ChatTestAuth(),
          wire = OperationWireClient(),
          store = ControlledMessageStore()..fail = true;
      await mount(t, auth, wire, store);
      await send(t);
      expect(store.writes, 1);
      expect(wire.posts, 0);
      expect(
        t
            .widget<IconButton>(
              find.byWidgetPredicate(
                (w) => w is IconButton && w.tooltip == '发送消息',
              ),
            )
            .onPressed,
        isNull,
      );
      expect(find.textContaining('消息没有继续提交'), findsOneWidget);
      await close(t, auth, wire);
    },
  );
  testWidgets(
    'account ABA while journal waits sends nothing and keeps reference',
    (t) async {
      final auth = ChatTestAuth(),
          wire = OperationWireClient(),
          store = ControlledMessageStore()..gate = Completer<void>();
      await mount(t, auth, wire, store);
      await t.enterText(find.byType(TextField), '本地合成消息');
      await t.tap(find.byTooltip('发送消息').hitTestable());
      await t.pump();
      expect(store.writes, 1);
      expect(wire.posts, 0);
      auth.use('Bearer B');
      auth.use('Bearer A');
      store.gate!.complete();
      await t.pumpAndSettle();
      expect(wire.posts, 0);
      expect(store.memory.values.length, 1);
      expect(find.textContaining('工作身份已变化'), findsOneWidget);
      await close(t, auth, wire);
    },
  );
  testWidgets(
    'late POST receipt after organization retirement cannot clear journal',
    (t) async {
      final auth = ChatTestAuth(),
          workspace = ValueNotifier<String?>(null),
          wire = OperationWireClient()..postGate = Completer<http.Response>(),
          store = MemoryHumanMessagePendingStore();
      String? getter() => workspace.value;
      await mount(
        t,
        auth,
        wire,
        store,
        workspaceID: getter,
        changes: workspace,
      );
      await t.enterText(find.byType(TextField), '本地合成消息');
      await t.tap(find.byTooltip('发送消息').hitTestable());
      await t.pump();
      expect(wire.posts, 1);
      workspace.value = sharePeer;
      workspace.value = null;
      wire.postGate!.complete(chatReply(receipt(wire.captured!), 201));
      await t.pumpAndSettle();
      expect(store.values.length, 1);
      expect(find.textContaining('工作身份已变化'), findsOneWidget);
      expect(find.textContaining('已核实消息'), findsNothing);
      await close(t, auth, wire);
      workspace.dispose();
    },
  );
  testWidgets(
    'journal read failure preserves authorized message history and blocks sending',
    (t) async {
      final auth = ChatTestAuth(),
          wire = OperationWireClient()
            ..history = [
              {
                'id': shareMessage,
                'senderAccountId': sharePeer,
                'body': '原本人可读历史仍应显示',
                'createdAt': '2026-10-07T01:02:03Z',
              },
            ],
          store = ControlledMessageStore()..readFail = true;
      await mount(t, auth, wire, store);
      expect(
        wire.historyGets,
        1,
        reason:
            'journal is a send prerequisite, not a history read prerequisite',
      );
      expect(find.text('原本人可读历史仍应显示'), findsOneWidget);
      expect(
        t
            .widget<IconButton>(
              find.byWidgetPredicate(
                (w) => w is IconButton && w.tooltip == '发送消息',
              ),
            )
            .onPressed,
        isNull,
      );
      await t.enterText(find.byType(TextField), '新的安全草稿');
      await t.testTextInput.receiveAction(TextInputAction.done);
      await t.pumpAndSettle();
      expect(wire.posts, 0);
      expect(store.writes, 0);
      expect(store.memory.values, isEmpty);
      expect(find.textContaining('恢复记录'), findsWidgets);
      await close(t, auth, wire);
    },
  );
  testWidgets(
    'pending journal read does not wait before authorized history GET',
    (t) async {
      final auth = ChatTestAuth(),
          wire = OperationWireClient()
            ..history = [
              {
                'id': shareMessage,
                'senderAccountId': sharePeer,
                'body': '存储等待不应隐藏原消息',
                'createdAt': '2026-10-07T01:02:03Z',
              },
            ],
          store = ControlledMessageStore()
            ..readGate = Completer<List<PendingHumanMessage>>();
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: HumanConversationPage(
              auth: auth,
              conversation: humanChat,
              client: wire,
              apiBaseUrl: 'https://api.example',
              pendingStore: MemoryEntitySharePendingStore(),
              messagePendingStore: store,
            ),
          ),
        ),
      );
      await t.pump();
      await t.pump(const Duration(milliseconds: 20));
      try {
        expect(
          wire.historyGets,
          1,
          reason:
              'unfinished recovery storage cannot block existing authorized read',
        );
        expect(find.text('存储等待不应隐藏原消息'), findsOneWidget);
        expect(
          t
              .widget<IconButton>(
                find.byWidgetPredicate(
                  (w) => w is IconButton && w.tooltip == '发送消息',
                ),
              )
              .onPressed,
          isNull,
        );
        expect(wire.posts, 0);
        expect(store.writes, 0);
      } finally {
        await t.pumpWidget(const SizedBox());
        store.readGate!.complete([]);
        await t.pumpAndSettle();
        auth.dispose();
        wire.close();
      }
    },
  );

  for (final waitForDelete in [false, true]) {
    testWidgets(
      'draft edited during ${waitForDelete ? 'journal delete' : 'POST'} is retained after successful send',
      (t) async {
        final auth = ChatTestAuth();
        final wire = OperationWireClient()..lost = false;
        final store = ControlledMessageStore();
        if (waitForDelete) {
          store.deleteGate = Completer<void>();
        } else {
          wire.postGate = Completer<http.Response>();
        }
        await mount(t, auth, wire, store);
        try {
          await t.enterText(find.byType(TextField), '本地合成消息');
          await t.tap(find.byTooltip('发送消息').hitTestable());
          await t.pump();
          expect(wire.posts, 1);
          expect(store.deletes, waitForDelete ? 1 : 0);
          expect(store.memory.values.length, 1);
          expect(
            t.widget<TextField>(find.byType(TextField)).enabled,
            isNot(false),
          );
          const nextDraft = '下一条仍未发送的草稿';
          await t.enterText(find.byType(TextField), nextDraft);
          if (waitForDelete) {
            store.deleteGate!.complete();
          } else {
            wire.postGate!.complete(chatReply(receipt(wire.captured!), 201));
          }
          await t.pumpAndSettle();
          expect(
            t.widget<TextField>(find.byType(TextField)).controller!.text,
            nextDraft,
            reason: 'the old successful operation cannot erase a newer draft',
          );
          expect(wire.posts, 1);
          expect(wire.operationReads, 0);
          expect(store.deletes, 1);
          expect(store.memory.values, isEmpty);
          expect(find.text('已核实消息已保存到会话，不代表对方已阅读。'), findsOneWidget);
        } finally {
          if (wire.postGate != null && !wire.postGate!.isCompleted) {
            wire.postGate!.complete(chatReply(receipt(wire.captured!), 201));
          }
          if (store.deleteGate != null && !store.deleteGate!.isCompleted) {
            store.deleteGate!.complete();
          }
          await t.pumpAndSettle();
          await close(t, auth, wire);
        }
      },
    );
  }
}
