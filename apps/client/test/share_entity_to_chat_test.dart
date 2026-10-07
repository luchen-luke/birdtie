import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/entity_share_pending_store.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'entity_share_pending_store_test.dart';
import 'public_moment_api_test.dart' show publicMomentData;

http.Response shareJSON(Object value, [int status = 200]) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': value})), status);
Map<String, dynamic> shareReceipt(String operation, {bool available = true}) =>
    {
      'operationId': operation,
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

class ShareFixture {
  ShareFixture({MemoryEntitySharePendingStore? pendingStore})
    : store = pendingStore ?? MemoryEntitySharePendingStore();
  final identity = ValueNotifier<String?>('Bearer A');
  final workspace = ValueNotifier<String?>(null);
  final MemoryEntitySharePendingStore store;
  final posted = <String>[];
  int starts = 0, reads = 0;
  bool lost = false, missing = false, withdrawn = false;
  Completer<http.Response>? pendingWrite;
  late final client = MockClient((r) async {
    switch (r.url.path) {
      case '/v1/me':
        return shareJSON({'id': shareOwner, 'accountType': 'person'});
      case '/v1/me/conversations':
        return shareJSON(
          starts == 0
              ? []
              : [
                  {
                    'id': shareConversation,
                    'otherAccountId': sharePeer,
                    'otherName': '本地合成好友',
                  },
                ],
        );
      case '/v1/me/ties':
        return shareJSON([
          {
            'id': shareOperation,
            'otherAccountId': sharePeer,
            'otherName': '本地合成好友',
          },
        ]);
      case '/v1/me/ties/$shareOperation/conversation':
        starts++;
        return shareJSON({
          'id': shareConversation,
          'otherAccountId': sharePeer,
          'otherName': '本地合成好友',
        });
      case '/v1/moments/$shareTarget':
        return withdrawn
            ? http.Response('{}', 404)
            : shareJSON(publicMomentData());
      case '/v1/me/conversations/$shareConversation/entity-shares':
        final value = jsonDecode(r.body) as Map<String, dynamic>;
        posted.add(value['operationId'] as String);
        expect(value.keys.toSet(), {'operationId', 'entity'});
        expect(value['entity'], {'type': 'moment', 'id': shareTarget});
        if (pendingWrite != null) return pendingWrite!.future;
        if (lost) throw http.ClientException('synthetic response lost');
        return shareJSON(shareReceipt(posted.last), 201);
      default:
        if (r.url.path.startsWith(
          '/v1/me/conversations/$shareConversation/entity-shares/',
        )) {
          reads++;
          return missing
              ? http.Response('{}', 404)
              : shareJSON(
                  shareReceipt(
                    r.url.path.split('/').last,
                    available: !withdrawn,
                  ),
                );
        }
        throw StateError('Unexpected ${r.method} ${r.url}');
    }
  });
  Widget harness() => MaterialApp(
    home: Scaffold(
      body: Builder(
        builder: (context) => FilledButton(
          onPressed: () => shareEntityToChat(
            context,
            authorizationHeader: () => identity.value,
            identityChanges: Listenable.merge([identity, workspace]),
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
    await t.pumpAndSettle();
  }

  Future<void> preview(WidgetTester t) async {
    await t.ensureVisible(find.text('本地合成好友'));
    await t.tap(find.text('本地合成好友'));
    await t.pumpAndSettle();
  }

  void dispose() {
    identity.dispose();
    workspace.dispose();
    client.close();
  }
}

class FailingShareStore extends MemoryEntitySharePendingStore {
  bool failRead = false;
  @override
  Future<List<PendingEntityShare>> read(String environment, String ownerID) {
    if (failRead) throw const FormatException('损坏的合成恢复记录');
    return super.read(environment, ownerID);
  }
}

void main() {
  testWidgets('再次具体批准后的未知结果不能沿用上次发送成功提示', (t) async {
    final f = ShareFixture();
    await t.pumpWidget(f.harness());
    await f.open(t);
    await f.preview(t);
    await t.tap(find.text('确认发送'));
    await t.pumpAndSettle();
    expect(find.text('已发送到私信。可在消息中找回。'), findsOneWidget);
    f.lost = true;
    await f.preview(t);
    await t.tap(find.text('确认发送'));
    await t.pumpAndSettle();
    expect(f.posted.length, 2);
    expect(f.posted.toSet().length, 2);
    expect(f.store.values.length, 1);
    expect(find.text('分享结果待核实，可能已发送。请核实原结果，不要重新发送。'), findsOneWidget);
    expect(find.text('已发送到私信。可在消息中找回。'), findsNothing);
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  testWidgets('没有核验到原收件人时只可核实，禁止按泛称重试POST', (t) async {
    final f = ShareFixture()..missing = true;
    await f.store.write('https://api.example', shareOwner, pendingShare);
    await t.pumpWidget(f.harness());
    await f.open(t);
    await t.tap(find.text('检查并重试原分享'));
    await t.pumpAndSettle();
    expect(f.reads, 1);
    expect(f.posted, isEmpty);
    expect(find.text('确认重试原分享'), findsNothing);
    expect(f.store.values.length, 1);
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  testWidgets('刷新发现恢复记录损坏后旧缓存好友不能继续发新卡片', (t) async {
    final store = FailingShareStore();
    final f = ShareFixture(pendingStore: store)..lost = true;
    await t.pumpWidget(f.harness());
    await f.open(t);
    await f.preview(t);
    await t.tap(find.text('确认发送'));
    await t.pumpAndSettle();
    expect(f.posted.length, 1);
    store.failRead = true;
    await t.ensureVisible(find.text('刷新当前状态'));
    await t.tap(find.text('刷新当前状态'));
    await t.pumpAndSettle();
    expect(find.text('本地合成好友'), findsNothing);
    expect(find.text('好友、分享恢复记录或当前内容暂不可读取。请重试。'), findsOneWidget);
    expect(f.posted.length, 1);
    expect(store.values.length, 1);
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  testWidgets('旧会话不在最新列表时，好友路径解析回原ID仍先核实原操作', (t) async {
    final f = ShareFixture();
    await f.store.write('https://api.example', shareOwner, pendingShare);
    await t.pumpWidget(f.harness());
    await f.open(t);
    await f.preview(t);
    await t.tap(find.text('确认发送'));
    await t.pumpAndSettle();
    expect(f.starts, 1);
    expect(f.posted, isEmpty);
    expect(f.store.values.length, 1);
    expect(find.text('这张卡片有待核实的原分享。请先核实原结果。'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  testWidgets('选择好友和取消不创建对话或发送；具体中文预览后才提交', (t) async {
    final f = ShareFixture();
    await t.pumpWidget(f.harness());
    await f.open(t);
    await f.preview(t);
    expect(f.posted, isEmpty);
    expect(f.starts, 0);
    expect(find.text('收件人：本地合成好友'), findsOneWidget);
    expect(find.textContaining('本人明确公开'), findsWidgets);
    await t.tap(find.text('取消'));
    await t.pumpAndSettle();
    expect(f.posted, isEmpty);
    await f.preview(t);
    await t.tap(find.text('确认发送'));
    await t.pumpAndSettle();
    expect(f.starts, 1);
    expect(f.posted.length, 1);
    expect(f.store.values, isEmpty);
    expect(find.text('已发送到私信。可在消息中找回。'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  testWidgets('响应丢失关闭重开按原键核实，撤回卡片仍是已提交事实', (t) async {
    final f = ShareFixture()..lost = true;
    await t.pumpWidget(f.harness());
    await f.open(t);
    await f.preview(t);
    await t.tap(find.text('确认发送'));
    await t.pumpAndSettle();
    expect(f.posted.length, 1);
    expect(f.store.values.length, 1);
    await t.ensureVisible(find.text('关闭'));
    await t.tap(find.text('关闭'));
    await t.pumpAndSettle();
    f.withdrawn = true;
    await f.open(t);
    await t.tap(find.text('核实原结果'));
    await t.pumpAndSettle();
    expect(f.posted.length, 1);
    expect(f.reads, 1);
    expect(f.store.values, isEmpty);
    expect(find.text('已发送到私信；内容当前已不可查看。'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  testWidgets('未知结果404不能声称未发送；fresh批准仍使用原操作键', (t) async {
    final f = ShareFixture()..lost = true;
    await t.pumpWidget(f.harness());
    await f.open(t);
    await f.preview(t);
    await t.tap(find.text('确认发送'));
    await t.pumpAndSettle();
    f.missing = true;
    f.lost = false;
    await t.tap(find.text('核实原结果'));
    await t.pumpAndSettle();
    expect(find.textContaining('不等于未发送'), findsOneWidget);
    await t.tap(find.text('检查并重试原分享'));
    await t.pumpAndSettle();
    expect(f.posted.length, 1);
    await t.tap(find.text('确认重试原分享'));
    await t.pumpAndSettle();
    expect(f.posted.length, 2);
    expect(f.posted.toSet().length, 1);
    expect(f.store.values, isEmpty);
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  for (final mode in ['account', 'workspace']) {
    testWidgets('分享预览$mode ABA旧确认无发送', (t) async {
      final f = ShareFixture();
      await t.pumpWidget(f.harness());
      await f.open(t);
      await f.preview(t);
      if (mode == 'account') {
        f.identity.value = 'Bearer B';
        f.identity.value = 'Bearer A';
      } else {
        f.workspace.value = 'org-a';
        f.workspace.value = null;
      }
      await t.tap(find.text('确认发送'));
      await t.pumpAndSettle();
      expect(f.starts, 0);
      expect(f.posted, isEmpty);
      expect(find.textContaining('工作身份已变化'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      f.dispose();
    });
  }
  testWidgets('迟到201不在新身份报成功，安全恢复引用仍保留', (t) async {
    final f = ShareFixture()..pendingWrite = Completer<http.Response>();
    await t.pumpWidget(f.harness());
    await f.open(t);
    await f.preview(t);
    await t.tap(find.text('确认发送'));
    await t.pump();
    await t.pump();
    expect(f.posted.length, 1);
    f.identity.value = 'Bearer B';
    f.identity.value = 'Bearer A';
    f.pendingWrite!.complete(shareJSON(shareReceipt(f.posted.single), 201));
    await t.pumpAndSettle();
    expect(f.store.values.length, 1);
    expect(find.text('已发送到私信。可在消息中找回。'), findsNothing);
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
}
