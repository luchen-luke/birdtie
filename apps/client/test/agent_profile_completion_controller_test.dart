import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/content/agent_profile_completion_api.dart';
import 'package:birdtie_client/src/content/agent_profile_completion_controller.dart';
import 'package:birdtie_client/src/content/agent_profile_completion_pending_store.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_profile_completion_api_test.dart';
import 'agent_profile_completion_pending_store_test.dart'
    show completionPending;

class CompletionGateStore extends MemoryAgentProfileCompletionPendingStore {
  Completer<void>? gate;
  bool fail = false;
  @override
  Future<void> write(String e, String o, PendingProfileCompletion p) async {
    if (gate != null) await gate!.future;
    if (fail) throw StateError('synthetic write failed');
    await super.write(e, o, p);
  }
}

void main() {
  test('同源同版本响应延长期限不能形成具体批准', () async {
    final store = MemoryAgentProfileCompletionPendingStore();
    final c = AgentProfileCompletionController(
      authorizationHeader: () => 'Bearer synthetic',
      accountID: () => completionOwner,
      api: AgentProfileCompletionAPI(
        apiBaseUrl: 'http://local',
        client: MockClient((r) async {
          if (r.method == 'POST') {
            final p = completionPreview(id: jsonDecode(r.body)['previewId']);
            p['source'] = {
              ...completionSource(),
              'memoryValidUntil': completionAt
                  .add(const Duration(hours: 2))
                  .toIso8601String(),
            };
            return completionResponse(p);
          }
          return completionResponse(completionSuggestions());
        }),
      ),
      pendingStore: store,
      now: () => completionAt,
    );
    await c.load();
    await c.review(c.suggestions!.sources.single);
    expect(c.preview, isNull);
    expect(c.canAccept, false);
    expect(c.unknown, true);
    c.dispose();
  });
  test('根复核重复匿名或组织 load 0GET 0POST', () async {
    for (final mode in ['anonymous', 'organization']) {
      var calls = 0;
      final c = AgentProfileCompletionController(
        authorizationHeader: () =>
            mode == 'anonymous' ? null : 'Bearer synthetic',
        accountID: () => completionOwner,
        organizationWorkspaceID: () =>
            mode == 'organization' ? completionMemory : null,
        api: AgentProfileCompletionAPI(
          apiBaseUrl: 'http://local',
          client: MockClient((_) async {
            calls++;
            return completionResponse(completionSuggestions());
          }),
        ),
        pendingStore: MemoryAgentProfileCompletionPendingStore(),
      );
      await c.load();
      await c.load();
      await c.load();
      expect(calls, 0);
      expect(c.denied, true);
      expect(c.suggestions, isNull);
      c.dispose();
    }
  });
  test('根复核404不能证明迟到原提交没有生效 保留UNKNOWN与原引用', () async {
    final s = MemoryAgentProfileCompletionPendingStore(),
        p = completionPending(phase: 'ACCEPT');
    await s.write('http://local', completionOwner, p);
    var posts = 0;
    final c = AgentProfileCompletionController(
      authorizationHeader: () => 'Bearer synthetic',
      accountID: () => completionOwner,
      api: AgentProfileCompletionAPI(
        apiBaseUrl: 'http://local',
        client: MockClient((r) async {
          if (r.method == 'POST') posts++;
          return completionResponse({}, status: 404);
        }),
      ),
      pendingStore: s,
    );
    await c.load();
    await c.verify();
    expect((await s.read('http://local', completionOwner))?.same(p), true);
    expect(c.unknown, true);
    expect(c.canReview, false);
    expect(posts, 0);
    c.dispose();
  });
  test('具体 review Accept 保存，关闭重开仅 GET 历史/current 分别表达', () async {
    final store = MemoryAgentProfileCompletionPendingStore();
    var id = '', posts = 0, lost = false;
    final client = MockClient((r) async {
      if (r.method == 'POST') {
        posts++;
        if (r.url.path.endsWith('/accept')) {
          if (lost) throw http.ClientException('synthetic unknown');
          return completionResponse(completionReceipt(id: id));
        }
        id = jsonDecode(r.body)['previewId'];
        return completionResponse(completionPreview(id: id));
      }
      return completionResponse(
        r.url.path.endsWith('/suggestions')
            ? completionSuggestions()
            : completionReceipt(id: id, current: false),
      );
    });
    AgentProfileCompletionController create() =>
        AgentProfileCompletionController(
          authorizationHeader: () => 'Bearer synthetic',
          accountID: () => completionOwner,
          api: AgentProfileCompletionAPI(
            client: client,
            apiBaseUrl: 'http://local',
          ),
          pendingStore: store,
          now: () => completionAt,
        );
    final a = create();
    await a.load();
    await a.review(a.suggestions!.sources.single);
    expect(a.canAccept, true);
    lost = true;
    await a.accept();
    expect(a.unknown, true);
    expect(posts, 2);
    a.dispose();
    final b = create();
    await b.load();
    expect(posts, 2);
    expect(b.preview, isNull);
    expect(b.message, contains('原操作曾保存'));
    expect(b.message, contains('当前资料已变化'));
    expect(await store.read('http://local', completionOwner), isNull);
    b.dispose();
  });
  test('POST 前持久成功 + await 身份 ABA 永久退休', () async {
    final store = CompletionGateStore()..gate = Completer<void>();
    var token = 'Bearer synthetic', posts = 0;
    final client = MockClient((r) async {
      if (r.method == 'POST') posts++;
      return completionResponse(completionSuggestions());
    });
    final c = AgentProfileCompletionController(
      authorizationHeader: () => token,
      accountID: () => completionOwner,
      api: AgentProfileCompletionAPI(
        client: client,
        apiBaseUrl: 'http://local',
      ),
      pendingStore: store,
      now: () => completionAt,
    );
    await c.load();
    final call = c.review(c.suggestions!.sources.single);
    await Future<void>.delayed(Duration.zero);
    expect(posts, 0);
    token = 'Bearer replacement';
    c.identityChanged();
    token = 'Bearer synthetic';
    c.identityChanged();
    store.gate!.complete();
    await call;
    expect(posts, 0);
    expect(c.retired, true);
    expect(c.preview, isNull);
    c.dispose();
  });
  test('持久失败、损坏、跨 owner/workspace 0POST', () async {
    for (final mode in ['write', 'corrupt', 'workspace', 'owner']) {
      final store = CompletionGateStore()..fail = mode == 'write';
      if (mode == 'corrupt') {
        store.values['birdtie.profile.completion.v1.${completionFingerprint('http://local\n$completionOwner')}'] =
            '{"body":"private"}';
      }
      var posts = 0;
      final c = AgentProfileCompletionController(
        authorizationHeader: () => 'Bearer synthetic',
        accountID: () => mode == 'owner' ? 'not-owner' : completionOwner,
        organizationWorkspaceID: () =>
            mode == 'workspace' ? completionMemory : null,
        api: AgentProfileCompletionAPI(
          client: MockClient((r) async {
            if (r.method == 'POST') posts++;
            return completionResponse(completionSuggestions());
          }),
          apiBaseUrl: 'http://local',
        ),
        pendingStore: store,
        now: () => completionAt,
      );
      await c.load();
      if (c.suggestions != null) await c.review(c.suggestions!.sources.single);
      expect(posts, 0);
      expect(c.preview, isNull);
      c.dispose();
    }
  });
  test('新 Session 不恢复批准，pending 等原期限，仅 GET', () async {
    final store = MemoryAgentProfileCompletionPendingStore();
    await store.write(
      'http://local',
      completionOwner,
      completionPending(phase: 'ACCEPT'),
    );
    var posts = 0, state = 'PENDING';
    final c = AgentProfileCompletionController(
      authorizationHeader: () => 'Bearer new-session',
      accountID: () => completionOwner,
      api: AgentProfileCompletionAPI(
        client: MockClient((r) async {
          if (r.method == 'POST') posts++;
          return completionResponse(completionReceipt(state: state));
        }),
        apiBaseUrl: 'http://local',
      ),
      pendingStore: store,
      now: () => completionAt,
    );
    await c.load();
    expect(c.unknown, true);
    expect(c.preview, isNull);
    await c.accept();
    expect(posts, 0);
    expect(c.message, contains('旧审阅内容无法恢复'));
    state = 'EXPIRED';
    await c.verify();
    expect(c.unknown, false);
    expect(c.message, contains('未保存'));
    expect(posts, 0);
    c.dispose();
  });
  test('错误 target/version/权限不清除原 journal', () async {
    for (final mode in ['target', 'version', 'permission', 'format']) {
      final store = MemoryAgentProfileCompletionPendingStore(),
          p = completionPending(phase: 'ACCEPT');
      await store.write('http://local', completionOwner, p);
      var posts = 0;
      final c = AgentProfileCompletionController(
        authorizationHeader: () => 'Bearer synthetic',
        accountID: () => completionOwner,
        api: AgentProfileCompletionAPI(
          client: MockClient((r) async {
            if (r.method == 'POST') posts++;
            final raw = completionReceipt();
            if (mode == 'target') raw['memoryId'] = completionAgent;
            if (mode == 'version') raw['memoryVersion'] = 2;
            if (mode == 'format') raw['body'] = 'private';
            return completionResponse(
              raw,
              status: mode == 'permission' ? 403 : 200,
            );
          }),
          apiBaseUrl: 'http://local',
        ),
        pendingStore: store,
        now: () => completionAt,
      );
      await c.load();
      expect(c.unknown, true);
      expect(
        (await store.read('http://local', completionOwner))!.same(p),
        true,
      );
      expect(posts, 0);
      c.dispose();
    }
  });
  test('双 State 原操作未核实不能准备另一 key', () async {
    final store = MemoryAgentProfileCompletionPendingStore();
    var posts = 0;
    AgentProfileCompletionController create() =>
        AgentProfileCompletionController(
          authorizationHeader: () => 'Bearer synthetic',
          accountID: () => completionOwner,
          api: AgentProfileCompletionAPI(
            client: MockClient((r) async {
              if (r.method == 'POST') {
                posts++;
                throw http.ClientException('synthetic unknown');
              }
              return completionResponse(completionSuggestions());
            }),
            apiBaseUrl: 'http://local',
          ),
          pendingStore: store,
          now: () => completionAt,
        );
    final a = create(), b = create();
    await a.load();
    await b.load();
    await a.review(a.suggestions!.sources.single);
    await b.review(b.suggestions!.sources.single);
    expect(posts, 1);
    expect(b.storageBlocked, true);
    a.dispose();
    b.dispose();
  });
  test('Accept reserve 等待越过原期限 0保存POST', () async {
    var now = completionAt, posts = 0;
    final store = CompletionGateStore();
    var id = '';
    final c = AgentProfileCompletionController(
      authorizationHeader: () => 'Bearer synthetic',
      accountID: () => completionOwner,
      api: AgentProfileCompletionAPI(
        client: MockClient((r) async {
          if (r.method == 'POST') {
            posts++;
            id = jsonDecode(r.body)['previewId'];
            return completionResponse(completionPreview(id: id));
          }
          return completionResponse(completionSuggestions());
        }),
        apiBaseUrl: 'http://local',
      ),
      pendingStore: store,
      now: () => now,
    );
    await c.load();
    await c.review(c.suggestions!.sources.single);
    store.gate = Completer<void>();
    final accept = c.accept();
    await Future<void>.delayed(Duration.zero);
    now = completionAt.add(const Duration(minutes: 6));
    store.gate!.complete();
    await accept;
    expect(posts, 1);
    expect(c.unknown, true);
    expect(c.preview, isNull);
    expect(c.message, contains('未发送保存'));
    c.dispose();
  });
}
