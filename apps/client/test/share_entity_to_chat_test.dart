import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/entity_share_control.dart';
import 'package:birdtie_client/src/workspace/entity_share_pending_store.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'entity_share_pending_store_test.dart';
import 'entity_share_control_test.dart';

http.Response shareJSON(Object value, [int status = 200]) =>
    shareControlJSON(value, status);
// Old full-message payload is preserved only as an invalid history fixture.
Map<String, dynamic> shareReceipt(String op, {bool available = true}) => {
  'operationId': op,
  'message': {
    'id': shareMessage,
    'conversationId': shareConversation,
    'senderAccountId': shareOwner,
    'speakerKind': 'human',
    'body': '分享了一张卡片',
    'createdAt': '2026-10-03T09:00:00Z',
    'entity': {
      'type': 'moment',
      'available': available,
      if (available) 'id': shareTarget,
      if (available) 'title': '当前公开动态',
    },
  },
};

// Match the existing B5 HTTP streamed-response test boundary. Fake animation
// time alone must not substitute for completion of the actual async stream.
Future<void> shareSettle(WidgetTester t) async {
  for (var i = 0; i < 3; i++) {
    await t.pump();
    await t.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 30)),
    );
  }
  await t.pumpAndSettle();
}

class ShareFixture {
  ShareFixture({MemoryEntitySharePendingStore? pendingStore})
    : store = pendingStore ?? MemoryEntitySharePendingStore();
  final identity = ValueNotifier<String?>('Bearer A'),
      workspace = ValueNotifier<String?>(null),
      actor = ValueNotifier<String?>(shareOwner);
  final source = ValueNotifier<int>(0);
  final MemoryEntitySharePendingStore store;
  final posted = <String>[], prepared = <String, PendingEntityShare>{};
  int starts = 0, reads = 0, stops = 0, previewSeconds = 30;
  String peerName = '本地合成好友', title = '当前公开动态';
  bool lost = false, missing = false, withdrawn = false, peerChanged = false;
  Completer<http.Response>? pendingWrite;
  late final client = MockClient(handle);
  final requests = <String>[];
  Future<http.Response> handle(http.Request r) async {
    requests.add('${r.method} ${r.url.path}');
    if (r.url.path == '/v1/me')
      return shareJSON({'id': actor.value, 'accountType': 'person'});
    if (r.url.path == '/v1/me/conversations')
      return shareJSON(
        starts == 0
            ? []
            : [
                {
                  'id': shareConversation,
                  'otherAccountId': peerChanged ? shareTarget : sharePeer,
                  'otherName': peerName,
                },
              ],
      );
    if (r.url.path == '/v1/me/ties')
      return shareJSON([
        {
          'id': shareOperation,
          'otherAccountId': peerChanged ? shareTarget : sharePeer,
          'otherName': peerName,
        },
      ]);
    if (r.url.path.endsWith('/conversation')) {
      starts++;
      throw StateError('Preview cannot create conversation');
    }
    if (r.url.path.endsWith('/previews')) {
      expect(r.method, 'POST');
      final b = jsonDecode(r.body) as Map<String, dynamic>;
      expect(b.keys.toSet(), {'operationId', 'entity'});
      final seg = r.url.path.split('/');
      final input = EntityShareInput(
        operationID: b['operationId'],
        recipient: EntityShareRecipient(seg[4], seg[5]),
        type: b['entity']['type'],
        entityID: b['entity']['id'],
      );
      if (withdrawn) return http.Response('{}', 403);
      prepared[input.operationID] = input.pending('a' * 64, 'UNKNOWN');
      return shareJSON(
        sharePreviewWire(input, title: title, seconds: previewSeconds),
      );
    }
    if (r.url.path.contains('/operations/') ||
        r.url.path.contains('/entity-shares/')) {
      final stop = r.url.path.endsWith('/stop'),
          seg = r.url.path.split('/'),
          op = seg[seg.length - (stop ? 2 : 1)];
      final legacy = r.url.path.contains('/conversations/');
      final p = legacy
          ? PendingEntityShare(
              operationID: op,
              conversationID: shareConversation,
              type: 'moment',
              entityID: shareTarget,
            )
          : prepared[op] ??
                (await store.read(
                  'https://api.example',
                  shareOwner,
                )).where((p) => p.operationID == op).first;
      if (r.method == 'PUT') {
        final b = jsonDecode(r.body) as Map<String, dynamic>;
        expect(b.keys.toSet(), {
          'operationId',
          'entity',
          'sourceVersion',
          'approval',
        });
        expect(b['operationId'], op);
        expect(b['sourceVersion'], 'a' * 64);
        posted.add(op);
        if (p.recipientType == 'FRIEND_TIE') starts++;
        if (pendingWrite != null) return pendingWrite!.future;
        if (lost) throw http.ClientException('synthetic response lost');
        return shareJSON(tinyShareWire(p));
      }
      expect(r.body, isEmpty);
      if (stop) {
        stops++;
        return shareJSON(
          tinyShareWire(p, stopped: !posted.contains(op) && !legacy),
        );
      }
      reads++;
      return missing ? http.Response('{}', 404) : shareJSON(tinyShareWire(p));
    }
    throw StateError('Unexpected ${r.method} ${r.url}');
  }

  Widget harness({ThemeData? theme, TransitionBuilder? builder}) => MaterialApp(
    theme: theme,
    builder: builder,
    home: Scaffold(
      body: Builder(
        builder: (context) => FilledButton(
          onPressed: () => shareEntityToChat(
            context,
            authorizationHeader: () => identity.value,
            ownerID: () => actor.value,
            identityChanges: Listenable.merge([
              identity,
              workspace,
              actor,
              source,
            ]),
            sourceKey: () => source.value,
            sourceCurrent: () => true,
            workspaceID: () => workspace.value,
            type: 'moment',
            id: shareTarget,
            apiBaseUrl: 'https://api.example',
            client: client,
            pendingStore: store,
          ),
          child: const Text('分享入口'),
        ),
      ),
    ),
  );
  Future<void> open(WidgetTester t) async {
    await t.tap(find.text('分享入口'));
    try {
      await shareSettle(t);
    } catch (_) {
      print(
        'DIAGNOSTIC requests=$requests text=${t.widgetList<Text>(find.byType(Text)).map((w) => w.data).toList()}',
      );
      rethrow;
    }
  }

  Future<void> preview(WidgetTester t) async {
    await t.ensureVisible(find.text(peerName));
    await t.tap(find.text(peerName));
    await shareSettle(t);
  }

  void dispose() {
    identity.dispose();
    workspace.dispose();
    actor.dispose();
    source.dispose();
    client.close();
  }
}

class FailingShareStore extends MemoryEntitySharePendingStore {
  bool failRead = false;
  @override
  Future<List<PendingEntityShare>> read(String env, String owner) {
    if (failRead) throw const FormatException('损坏的合成恢复记录');
    return super.read(env, owner);
  }
}

void main() {
  testWidgets('选择好友及取消只预览不创建会话；批准后仅一次143 PUT', (t) async {
    final f = ShareFixture();
    await t.pumpWidget(f.harness());
    await f.open(t);
    await f.preview(t);
    expect(f.posted, isEmpty);
    expect(f.starts, 0);
    expect(find.text('收件人：本地合成好友'), findsOneWidget);
    await t.tap(find.text('取消'));
    await shareSettle(t);
    expect(f.starts, 0);
    await f.preview(t);
    await t.tap(find.text('确认发送'));
    await shareSettle(t);
    expect(f.starts, 1);
    expect(f.posted.length, 1);
    expect(f.store.values, isEmpty);
    expect(find.text('已发送到私信。可在消息中找回。'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  testWidgets('第二次分享响应丢失移除上次成功提示且保留原未知键', (t) async {
    final f = ShareFixture();
    await t.pumpWidget(f.harness());
    await f.open(t);
    await f.preview(t);
    await t.tap(find.text('确认发送'));
    await shareSettle(t);
    f.lost = true;
    await f.preview(t);
    await t.tap(find.text('确认发送'));
    await shareSettle(t);
    expect(f.posted.toSet().length, 2);
    expect(f.store.values.length, 1);
    expect(find.textContaining('分享结果待核实'), findsOneWidget);
    expect(find.text('已发送到私信。可在消息中找回。'), findsNothing);
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  testWidgets('UNKNOWN关闭重开只原GET，当前内容403不抹原成功事实', (t) async {
    final f = ShareFixture()..lost = true;
    await t.pumpWidget(f.harness());
    await f.open(t);
    await f.preview(t);
    await t.tap(find.text('确认发送'));
    await shareSettle(t);
    await t.tap(find.text('关闭'));
    await shareSettle(t);
    f.withdrawn = true;
    await f.open(t);
    await t.tap(find.text('核实原结果'));
    await shareSettle(t);
    expect(f.posted.length, 1);
    expect(f.reads, 1);
    expect(f.store.values, isEmpty);
    expect(find.text('已发送到私信。可在消息中找回。'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  testWidgets('UNKNOWN404不宣称未发送且只有原GET或bodylessSTOP没有重发', (t) async {
    final f = ShareFixture()..lost = true;
    await t.pumpWidget(f.harness());
    await f.open(t);
    await f.preview(t);
    await t.tap(find.text('确认发送'));
    await shareSettle(t);
    f.missing = true;
    await t.tap(find.text('核实原结果'));
    await shareSettle(t);
    expect(find.textContaining('不等于未发送'), findsOneWidget);
    expect(find.text('检查并重试原分享'), findsNothing);
    expect(f.posted.length, 1);
    await t.tap(find.text('停止原分享'));
    await shareSettle(t);
    await t.tap(find.text('停止原操作'));
    await shareSettle(t);
    expect(f.stops, 1);
    expect(f.posted.length, 1);
    expect(f.store.values, isEmpty);
    expect(find.text('已发送到私信。可在消息中找回。'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  testWidgets('旧074记录恢复沿原会话命名空间且有未知记录禁止新分享', (t) async {
    final f = ShareFixture();
    await f.store.write('https://api.example', shareOwner, pendingShare);
    await t.pumpWidget(f.harness());
    await f.open(t);
    await f.preview(t);
    expect(f.starts, 0);
    expect(f.posted, isEmpty);
    expect(find.textContaining('待核实的原分享。请先'), findsOneWidget);
    await t.tap(find.text('核实原结果'));
    await shareSettle(t);
    expect(f.reads, 1);
    expect(f.store.values, isEmpty);
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  for (final mode in ['owner', 'workspace', 'source']) {
    testWidgets('分享具体预览 $mode A→B→A 原确认退休并去旧名字', (t) async {
      final f = ShareFixture();
      await t.pumpWidget(f.harness());
      await f.open(t);
      await f.preview(t);
      if (mode == 'owner') {
        f.actor.value = sharePeer;
        f.actor.value = shareOwner;
      }
      if (mode == 'workspace') {
        f.workspace.value = 'org-a';
        f.workspace.value = null;
      }
      if (mode == 'source') {
        f.source.value++;
        f.source.value++;
      }
      await shareSettle(t);
      expect(find.text('收件人：本地合成好友'), findsNothing);
      expect(
        t
            .widget<FilledButton>(find.widgetWithText(FilledButton, '确认发送'))
            .onPressed,
        isNull,
      );
      expect(f.starts, 0);
      expect(f.posted, isEmpty);
      await t.tap(find.text('取消'));
      await shareSettle(t);
      await t.pumpWidget(const SizedBox());
      f.dispose();
    });
  }
  testWidgets('普通续期退休旧批准但保留本人未知metadata供核实', (t) async {
    final f = ShareFixture()..lost = true;
    await t.pumpWidget(f.harness());
    await f.open(t);
    await f.preview(t);
    await t.tap(find.text('确认发送'));
    await shareSettle(t);
    final op = f.posted.single;
    f.identity.value = 'Bearer renewed';
    await shareSettle(t);
    expect(f.store.values.values.single.operationID, op);
    await t.tap(find.text('刷新当前状态'));
    await shareSettle(t);
    await t.tap(find.text('核实原结果'));
    await shareSettle(t);
    expect(f.posted.length, 1);
    expect(f.store.values, isEmpty);
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  testWidgets('迟到成功不能在新owner报成功且durable原记录不丢', (t) async {
    final f = ShareFixture()..pendingWrite = Completer<http.Response>();
    await t.pumpWidget(f.harness());
    await f.open(t);
    await f.preview(t);
    await t.tap(find.text('确认发送'));
    await t.pump();
    await t.pump();
    expect(f.posted.length, 1);
    f.actor.value = sharePeer;
    f.actor.value = shareOwner;
    f.pendingWrite!.complete(
      shareJSON(tinyShareWire(f.prepared[f.posted.single]!)),
    );
    await shareSettle(t);
    expect(f.store.values.length, 1);
    expect(find.text('已发送到私信。可在消息中找回。'), findsNothing);
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  testWidgets('刷新恢复存储损坏禁止旧缓存好友继续分享', (t) async {
    final store = FailingShareStore(),
        f = ShareFixture(pendingStore: FailingShareStore());
    final attached = f.store as FailingShareStore;
    await t.pumpWidget(f.harness());
    await f.open(t);
    attached.failRead = true;
    await t.tap(find.text('刷新当前状态'));
    await shareSettle(t);
    expect(find.text(f.peerName), findsNothing);
    expect(f.posted, isEmpty);
    expect(find.textContaining('分享恢复记录或当前个人身份暂不可读取'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    f.dispose();
    expect(store.values, isEmpty);
  });
}
