import 'dart:async';
import 'package:birdtie_client/src/workspace/agent_run_api.dart';
import 'package:birdtie_client/src/workspace/agent_run_controller.dart';
import 'package:birdtie_client/src/workspace/agent_run_pending_store.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_run_api_test.dart';
import 'agent_run_pending_store_test.dart' show TestRunPendingStore;

class DelayedRunStore extends TestRunPendingStore {
  final saved = Completer<void>();
  @override
  Future<void> reserve(String e, String o, PendingAgentRunCancel p) async {
    await super.reserve(e, o, p);
    await saved.future;
  }
}

void main() {
  test('两个控制器竞争核实槽只恢复真正的原selector', () async {
    final store = TestRunPendingStore(), gate = Completer<http.Response>();
    var posts = 0;
    final client = MockClient((r) async {
      if (r.method == 'POST') {
        posts++;
        return gate.future;
      }
      return runResponse(
        r.url.path.endsWith('/page') ? runPage() : runRecord(),
      );
    });
    AgentRunController make() => AgentRunController(
      api: AgentRunAPI(client: client, apiBaseUrl: 'http://local'),
      authorizationHeader: () => 'Bearer synthetic',
      ownerID: () => runOwner,
      pendingStore: store,
    );
    final a = make(), b = make();
    await a.initialize();
    await b.initialize();
    await a.select(runID);
    await b.select(runID);
    final ra = await a.prepareCancel(), rb = await b.prepareCancel();
    final first = a.confirmCancel(ra!);
    await Future<void>.delayed(Duration.zero);
    final original = store.value!;
    expect(await b.confirmCancel(rb!), false);
    expect(b.pending!.encoded, original.encoded);
    expect(posts, 1);
    gate.complete(runResponse(runRecord(state: 'CANCELLED', version: 2)));
    expect(await first, true);
    a.dispose();
    b.dispose();
    client.close();
  });
  test('存储完成前返回的未发送停止不留下永久UNKNOWN', () async {
    final store = DelayedRunStore();
    var posts = 0;
    final client = MockClient((r) async {
      if (r.method == 'POST') posts++;
      return runResponse(
        r.url.path.endsWith('/page') ? runPage() : runRecord(),
      );
    });
    final c = AgentRunController(
      api: AgentRunAPI(client: client, apiBaseUrl: 'http://local'),
      authorizationHeader: () => 'Bearer synthetic',
      ownerID: () => runOwner,
      pendingStore: store,
    );
    await c.initialize();
    await c.select(runID);
    final review = await c.prepareCancel();
    final writing = c.confirmCancel(review!);
    await Future<void>.delayed(Duration.zero);
    c.dispose();
    store.saved.complete();
    expect(await writing, false);
    expect(posts, 0);
    expect(store.value, null);
    client.close();
  });
  test('未知停止关闭重开只GET原对象，当前状态不伪造因果', () async {
    var posts = 0;
    var state = 'WAITING_CONFIRMATION';
    final store = TestRunPendingStore();
    final client = MockClient((r) async {
      if (r.method == 'POST') {
        posts++;
        throw http.ClientException('lost');
      }
      return runResponse(
        r.url.path.endsWith('/page')
            ? runPage()
            : runRecord(state: state, version: state == 'CANCELLED' ? 2 : 1),
      );
    });
    AgentRunController make() => AgentRunController(
      api: AgentRunAPI(client: client, apiBaseUrl: 'http://local'),
      authorizationHeader: () => 'Bearer synthetic',
      ownerID: () => runOwner,
      pendingStore: store,
    );
    final c = make();
    await c.initialize();
    await c.select(runID);
    final review = await c.prepareCancel();
    expect(await c.confirmCancel(review!), false);
    expect(store.value, isNotNull);
    c.dispose();
    state = 'CANCELLED';
    final next = make();
    try {
      await next.initialize();
      expect(next.pending, isNotNull);
      await next.recover();
      expect(posts, 1);
      expect(next.pending, null);
      expect(next.notice, contains('不证明'));
      expect(next.selected!.state, 'CANCELLED');
    } finally {
      next.dispose();
      client.close();
    }
  });
  test('source身份ABA退休迟到页且0POST，borrowed transport保留', () async {
    var token = 'Bearer A';
    final signal = ChangeNotifier(), gate = Completer<http.Response>();
    var calls = 0;
    final client = MockClient((r) async {
      calls++;
      return gate.future;
    });
    final c = AgentRunController(
      api: AgentRunAPI(client: client, apiBaseUrl: 'http://local'),
      authorizationHeader: () => token,
      ownerID: () => runOwner,
      identityChanges: signal,
      pendingStore: TestRunPendingStore(),
    );
    final first = c.initialize();
    await Future<void>.delayed(Duration.zero);
    token = 'Bearer B';
    signal.notifyListeners();
    token = 'Bearer A';
    signal.notifyListeners();
    gate.complete(runResponse(runPage()));
    await first;
    expect(c.current, false);
    expect(c.items, isEmpty);
    expect(calls, 1);
    c.dispose();
    signal.dispose();
    client.close();
  });
  test('分页追加原ID与失败保留已有内容，非法cursor只重读第一页', () async {
    var fail = false;
    final first = [
      for (var i = 1; i <= 20; i++)
        runRecord(
          id: '${i.toRadixString(16).padLeft(8, '0')}-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
        ),
    ];
    final client = MockClient(
      (r) async => r.url.query.isNotEmpty
          ? (fail
                ? runResponse({}, code: 503)
                : runResponse(runPage(items: [runRecord(id: runID)])))
          : runResponse(runPage(items: first, cursor: 'sealed_cursor')),
    );
    final c = AgentRunController(
      api: AgentRunAPI(client: client, apiBaseUrl: 'http://local'),
      authorizationHeader: () => 'Bearer synthetic',
      ownerID: () => runOwner,
      pendingStore: TestRunPendingStore(),
    );
    try {
      await c.initialize();
      expect(c.items.length, 20);
      fail = true;
      await c.load(more: true);
      expect(c.items.length, 20);
      fail = false;
      await c.load(more: true);
      expect(c.items.length, 21);
    } finally {
      c.dispose();
      client.close();
    }
  });
}
