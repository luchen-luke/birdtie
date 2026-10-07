import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/agent_memory_correction_api.dart';
import 'package:birdtie_client/src/workspace/agent_memory_correction_controller.dart';
import 'package:birdtie_client/src/workspace/agent_memory_correction_pending_store.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_memory_correction_api_test.dart';
import 'agent_memory_correction_pending_store_test.dart';

class CorrectionGateStore extends MemoryAgentMemoryCorrectionPendingStore {
  Completer<void>? gate;
  bool fail = false;
  @override
  Future<void> write(String e, String o, PendingMemoryCorrection p) async {
    if (gate != null) await gate!.future;
    if (fail) throw StateError('synthetic blocked');
    await super.write(e, o, p);
  }
}

class CorrectionReadFailureStore
    extends MemoryAgentMemoryCorrectionPendingStore {
  bool fail = false;
  @override
  Future<PendingMemoryCorrection?> read(String e, String o) async {
    if (fail) throw PlatformException(code: 'synthetic');
    return super.read(e, o);
  }
}

class CorrectionFaultSecureStorage implements FlutterSecureStorage {
  final delegate = const FlutterSecureStorage();
  bool hideReadback = false;
  int writes = 0;
  @override
  dynamic noSuchMethod(Invocation i) {
    final key = i.namedArguments[#key] as String?;
    if (i.memberName == #read) {
      return hideReadback && writes > 0
          ? Future<String?>.value(null)
          : delegate.read(key: key!);
    }
    if (i.memberName == #write) {
      writes++;
      return delegate.write(
        key: key!,
        value: i.namedArguments[#value] as String?,
      );
    }
    if (i.memberName == #delete) return delegate.delete(key: key!);
    return super.noSuchMethod(i);
  }
}

class CorrectionDeleteStore extends MemoryAgentMemoryCorrectionPendingStore {
  bool fail = false;
  Completer<void>? gate;
  final entered = Completer<void>();
  @override
  Future<void> delete(String e, String o, PendingMemoryCorrection p) async {
    if (!entered.isCompleted) entered.complete();
    await gate?.future;
    if (fail) throw StateError('synthetic delete failed');
    return super.delete(e, o, p);
  }
}

void main() {
  for (final mode in [
    'future-response-wait',
    'initial-utc-shorter',
    'confirm-store-wait',
  ]) {
    test('注入单调时间 $mode 保守旧deadline不延长', () async {
      var ticks = Duration.zero, now = correctionAt, posts = 0;
      final store = CorrectionGateStore();
      if (mode == 'initial-utc-shorter') {
        now = correctionAt.add(const Duration(minutes: 2));
      }
      final c = AgentMemoryCorrectionController(
        authorizationHeader: () => 'Bearer synthetic',
        accountID: () => correctionOwner,
        pendingStore: store,
        now: () => now,
        elapsed: () => ticks,
        api: AgentMemoryCorrectionAPI(
          apiBaseUrl: 'http://local',
          client: MockClient((r) async {
            if (r.method == 'POST') {
              posts++;
              if (mode == 'future-response-wait') {
                ticks = const Duration(minutes: 6);
              }
              return correctionResponse(
                correctionPreviewRaw(
                  input: Map<String, dynamic>.from(jsonDecode(r.body)),
                  at: mode == 'future-response-wait'
                      ? correctionAt.add(const Duration(hours: 1))
                      : correctionAt,
                  memory: correctionMemoryRaw(),
                ),
              );
            }
            return correctionResponse({
              'data': r.url.path.endsWith('agent-memories')
                  ? [correctionMemoryRaw()]
                  : [],
            });
          }),
        ),
      );
      await c.load();
      await c.reviewMemory(c.memories.single, 'REJECT');
      if (mode == 'future-response-wait') {
        expect(c.preview, isNull);
      } else {
        expect(c.canConfirm, true);
        if (mode == 'confirm-store-wait') {
          store.gate = Completer<void>();
          final confirm = c.confirm();
          await Future<void>.delayed(Duration.zero);
          ticks = const Duration(minutes: 6);
          now = correctionAt.subtract(const Duration(days: 1));
          store.gate!.complete();
          await confirm;
        } else {
          ticks = const Duration(minutes: 3, seconds: 1);
          now = correctionAt.subtract(const Duration(days: 1));
          await c.confirm();
        }
        expect(c.preview, isNull);
      }
      expect(posts, 1);
      expect(c.unknown, true);
      expect(c.hasPending, true);
      c.dispose();
    });
  }

  test('future server observedAt与整段迟到response不得首次展示过期review', () async {
    var posts = 0;
    final c = AgentMemoryCorrectionController(
      authorizationHeader: () => 'Bearer synthetic',
      accountID: () => correctionOwner,
      pendingStore: MemoryAgentMemoryCorrectionPendingStore(),
      now: () => correctionAt,
      api: AgentMemoryCorrectionAPI(
        apiBaseUrl: 'http://local',
        client: MockClient((r) async {
          if (r.method == 'POST') {
            posts++;
            final server = correctionAt.add(const Duration(hours: 1));
            await Future<void>.delayed(const Duration(milliseconds: 80));
            return correctionResponse({
              ...correctionPreviewRaw(
                input: Map<String, dynamic>.from(jsonDecode(r.body)),
                at: server,
                memory: correctionMemoryRaw(),
              ),
              'expiresAt': server
                  .add(const Duration(milliseconds: 30))
                  .toIso8601String(),
            });
          }
          return correctionResponse({
            'data': r.url.path.endsWith('agent-memories')
                ? [correctionMemoryRaw()]
                : [],
          });
        }),
      ),
    );
    await c.load();
    await c.reviewMemory(c.memories.single, 'REJECT');
    expect(c.preview, isNull);
    expect(c.unknown, true);
    expect(c.hasPending, true);
    expect(posts, 1);
    c.dispose();
  });
  test('device clock回拨不能延长具体review原shortlease', () async {
    var now = correctionAt, posts = 0;
    final c = AgentMemoryCorrectionController(
      authorizationHeader: () => 'Bearer synthetic',
      accountID: () => correctionOwner,
      pendingStore: MemoryAgentMemoryCorrectionPendingStore(),
      now: () => now,
      api: AgentMemoryCorrectionAPI(
        apiBaseUrl: 'http://local',
        client: MockClient((r) async {
          if (r.method == 'POST') {
            posts++;
            return correctionResponse({
              ...correctionPreviewRaw(
                input: Map<String, dynamic>.from(jsonDecode(r.body)),
              ),
              'expiresAt': correctionAt
                  .add(const Duration(milliseconds: 300))
                  .toIso8601String(),
            });
          }
          return correctionResponse({
            'data': r.url.path.endsWith('agent-memories')
                ? [correctionMemoryRaw()]
                : [],
          });
        }),
      ),
    );
    await c.load();
    await c.reviewMemory(c.memories.single, 'REJECT');
    expect(c.canConfirm, true);
    await Future<void>.delayed(const Duration(milliseconds: 400));
    now = correctionAt.subtract(const Duration(days: 1));
    await c.confirm();
    expect(posts, 1);
    expect(c.preview, isNull);
    expect(c.unknown, true);
    c.dispose();
  });
  TestWidgetsFlutterBinding.ensureInitialized();
  test('真实Secure mocked readback失败0POST 保留metadata直到原GET', () async {
    FlutterSecureStorage.setMockInitialValues({});
    final native = CorrectionFaultSecureStorage()..hideReadback = true;
    final store = SecureAgentMemoryCorrectionPendingStore(storage: native);
    var posts = 0, gets = 0;
    final c = AgentMemoryCorrectionController(
      authorizationHeader: () => 'Bearer synthetic',
      accountID: () => correctionOwner,
      pendingStore: store,
      now: () => correctionAt,
      api: AgentMemoryCorrectionAPI(
        apiBaseUrl: 'http://local',
        client: MockClient((r) async {
          if (r.method == 'POST') posts++;
          if (r.url.path.contains('agent-memory-corrections')) {
            gets++;
            return correctionResponse({}, status: 404);
          }
          return correctionResponse({
            'data': r.url.path.endsWith('agent-memories')
                ? [correctionMemoryRaw()]
                : [],
          });
        }),
      ),
    );
    await c.load();
    await c.reviewMemory(c.memories.single, 'REJECT');
    expect(posts, 0);
    expect(c.storageBlocked, true);
    expect((await const FlutterSecureStorage().readAll()).length, 1);
    native.hideReadback = false;
    await c.load();
    expect(gets, 1);
    expect(posts, 0);
    expect(c.unknown, true);
    expect(await store.read('http://local', correctionOwner), isNotNull);
    c.dispose();
  });
  test('默认Secure lostConfirm关闭重开真实平台mock不还原批准', () async {
    FlutterSecureStorage.setMockInitialValues({});
    var posts = 0;
    Map<String, dynamic>? input;
    AgentMemoryCorrectionController build() => AgentMemoryCorrectionController(
      authorizationHeader: () => 'Bearer synthetic',
      accountID: () => correctionOwner,
      now: () => correctionAt,
      api: AgentMemoryCorrectionAPI(
        apiBaseUrl: 'http://local',
        client: MockClient((r) async {
          if (r.method == 'POST') {
            posts++;
            if (r.url.path.endsWith('previews')) {
              input = Map<String, dynamic>.from(jsonDecode(r.body));
              return correctionResponse(correctionPreviewRaw(input: input));
            }
            throw StateError('lost commit response');
          }
          if (r.url.path.contains('agent-memory-corrections')) {
            return correctionResponse(
              correctionReceiptRaw(input: input, current: false),
            );
          }
          return correctionResponse({
            'data': r.url.path.endsWith('agent-memories')
                ? [correctionMemoryRaw()]
                : [],
          });
        }),
      ),
    );
    final a = build();
    expect(a.store, isA<SecureAgentMemoryCorrectionPendingStore>());
    await a.load();
    await a.reviewMemory(a.memories.single, 'REJECT');
    await a.confirm();
    expect(a.unknown, true);
    a.dispose();
    final b = build();
    await b.load();
    await b.confirm();
    expect(posts, 2);
    expect(b.preview, isNull);
    expect(b.message, contains('曾完成'));
    expect(await const FlutterSecureStorage().readAll(), isEmpty);
    b.dispose();
  });
  test('delete失败保持原引用 后续只GET原ID方可清除', () async {
    final store = CorrectionDeleteStore()..fail = true;
    await store.write(
      'http://local',
      correctionOwner,
      correctionPending(phase: 'CONFIRM'),
    );
    var gets = 0, posts = 0;
    final c = AgentMemoryCorrectionController(
      authorizationHeader: () => 'Bearer synthetic',
      accountID: () => correctionOwner,
      pendingStore: store,
      now: () => correctionAt,
      api: AgentMemoryCorrectionAPI(
        apiBaseUrl: 'http://local',
        client: MockClient((r) async {
          if (r.method == 'POST') posts++;
          gets++;
          expect(r.url.path.endsWith(correctionID), true);
          return correctionResponse(correctionReceiptRaw());
        }),
      ),
    );
    await c.load();
    expect(c.unknown, true);
    expect(c.hasPending, true);
    expect(await store.read('http://local', correctionOwner), isNotNull);
    store.fail = false;
    await c.verify();
    expect(gets, 2);
    expect(posts, 0);
    expect(c.hasPending, false);
    c.dispose();
  });
  test('迟到exactdelete与身份ABA不显示结果或删他方pending', () async {
    final store = CorrectionDeleteStore()..gate = Completer<void>();
    final p = correctionPending(phase: 'CONFIRM');
    await store.write('http://local', correctionOwner, p);
    var token = 'Bearer synthetic';
    final c = AgentMemoryCorrectionController(
      authorizationHeader: () => token,
      accountID: () => correctionOwner,
      pendingStore: store,
      now: () => correctionAt,
      api: AgentMemoryCorrectionAPI(
        apiBaseUrl: 'http://local',
        client: MockClient(
          (r) async => correctionResponse(correctionReceiptRaw()),
        ),
      ),
    );
    final f = c.load();
    await store.entered.future;
    store.values.clear();
    final other = correctionPending(id: correctionAgent);
    await store.write('http://local', correctionOwner, other);
    token = 'Bearer next';
    c.identityChanged();
    token = 'Bearer synthetic';
    c.identityChanged();
    store.gate!.complete();
    await f;
    expect(c.retired, true);
    expect(c.message, isNull);
    expect(
      (await store.read('http://local', correctionOwner))!.same(other),
      true,
    );
    c.dispose();
  });
  for (final change in ['identity', 'deadline']) {
    test('CONFIRM reserve等待后$change无ConfirmPOST', () async {
      final store = CorrectionGateStore();
      var token = 'Bearer synthetic', now = correctionAt, posts = 0;
      final c = AgentMemoryCorrectionController(
        authorizationHeader: () => token,
        accountID: () => correctionOwner,
        pendingStore: store,
        now: () => now,
        api: AgentMemoryCorrectionAPI(
          apiBaseUrl: 'http://local',
          client: MockClient((r) async {
            if (r.method == 'POST') {
              posts++;
              return correctionResponse(
                correctionPreviewRaw(
                  input: Map<String, dynamic>.from(jsonDecode(r.body)),
                ),
              );
            }
            return correctionResponse({
              'data': r.url.path.endsWith('agent-memories')
                  ? [correctionMemoryRaw()]
                  : [],
            });
          }),
        ),
      );
      await c.load();
      await c.reviewMemory(c.memories.single, 'REJECT');
      store.gate = Completer<void>();
      final f = c.confirm();
      await Future<void>.delayed(Duration.zero);
      if (change == 'identity') {
        token = 'Bearer next';
        c.identityChanged();
        token = 'Bearer synthetic';
        c.identityChanged();
      } else {
        now = correctionAt.add(const Duration(minutes: 6));
      }
      store.gate!.complete();
      await f;
      expect(posts, 1);
      expect(c.canConfirm, false);
      expect(c.hasPending, true);
      c.dispose();
    });
  }

  test('具体预览过期清除原正文批准 保留journal 0新POST', () async {
    final store = MemoryAgentMemoryCorrectionPendingStore();
    var now = correctionAt, posts = 0;
    final c = AgentMemoryCorrectionController(
      authorizationHeader: () => 'Bearer synthetic',
      accountID: () => correctionOwner,
      pendingStore: store,
      now: () => now,
      api: AgentMemoryCorrectionAPI(
        apiBaseUrl: 'http://local',
        client: MockClient((r) async {
          if (r.method == 'POST') {
            posts++;
            return correctionResponse(
              correctionPreviewRaw(
                input: Map<String, dynamic>.from(jsonDecode(r.body)),
              ),
            );
          }
          return correctionResponse({
            'data': r.url.path.endsWith('agent-memories')
                ? [correctionMemoryRaw()]
                : [],
          });
        }),
      ),
    );
    await c.load();
    await c.reviewMemory(c.memories.single, 'REJECT');
    now = correctionAt.add(const Duration(minutes: 6));
    await c.confirm();
    expect(c.preview, isNull);
    expect(c.unknown, true);
    expect(c.hasPending, true);
    expect(posts, 1);
    expect(await store.read('http://local', correctionOwner), isNotNull);
    c.dispose();
  });
  test('已读列表后Secure read异常保持存储阻断 未核实前0POST', () async {
    final s = CorrectionReadFailureStore();
    var posts = 0;
    final c = AgentMemoryCorrectionController(
      authorizationHeader: () => 'Bearer synthetic',
      accountID: () => correctionOwner,
      pendingStore: s,
      now: () => correctionAt,
      api: AgentMemoryCorrectionAPI(
        apiBaseUrl: 'http://local',
        client: MockClient((r) async {
          if (r.method == 'POST') {
            posts++;
            return correctionResponse(
              correctionPreviewRaw(
                input: Map<String, dynamic>.from(jsonDecode(r.body)),
              ),
            );
          }
          return correctionResponse({
            'data': r.url.path.endsWith('agent-memories')
                ? [correctionMemoryRaw()]
                : [],
          });
        }),
      ),
    );
    await c.load();
    final selected = c.memories.single;
    s.fail = true;
    await c.load();
    await c.reviewMemory(selected, 'REJECT');
    expect(posts, 0);
    expect(c.storageBlocked, true);
    expect(c.canReview, false);
    s.fail = false;
    await c.load();
    expect(c.storageBlocked, false);
    expect(c.canReview, true);
    c.dispose();
  });
  for (final mode in ['anonymous', 'organization']) {
    test('$mode 重复load 0GET 0POST无crash', () async {
      var calls = 0;
      final c = AgentMemoryCorrectionController(
        authorizationHeader: () =>
            mode == 'anonymous' ? null : 'Bearer synthetic',
        accountID: () => correctionOwner,
        organizationWorkspaceID: () =>
            mode == 'organization' ? correctionAgent : null,
        api: AgentMemoryCorrectionAPI(
          apiBaseUrl: 'http://local',
          client: MockClient((r) async {
            calls++;
            return correctionResponse({});
          }),
        ),
        pendingStore: MemoryAgentMemoryCorrectionPendingStore(),
      );
      await c.load();
      await c.load();
      expect(calls, 0);
      expect(c.denied, true);
      expect(c.canReview, false);
      c.dispose();
    });
  }
  for (final action in ['EDIT', 'DELETE', 'REJECT', 'NEGATE']) {
    test('$action 持久读回先于POST、具体两阶段与once回执', () async {
      final s = MemoryAgentMemoryCorrectionPendingStore();
      var posts = 0;
      Map<String, dynamic>? input;
      final c = AgentMemoryCorrectionController(
        authorizationHeader: () => 'Bearer synthetic',
        accountID: () => correctionOwner,
        pendingStore: s,
        now: () => correctionAt,
        api: AgentMemoryCorrectionAPI(
          apiBaseUrl: 'http://local',
          client: MockClient((r) async {
            if (r.method == 'GET') {
              return correctionResponse({
                'data': r.url.path.endsWith('agent-memories')
                    ? [correctionMemoryRaw()]
                    : [],
              });
            }
            posts++;
            final p = await s.read('http://local', correctionOwner);
            expect(p, isNotNull);
            if (r.url.path.endsWith('previews')) {
              expect(p!.phase, 'PREVIEW');
              input = Map<String, dynamic>.from(jsonDecode(r.body));
              return correctionResponse(correctionPreviewRaw(input: input));
            }
            expect(p!.phase, 'CONFIRM');
            expect(jsonDecode(r.body), {'planDigest': correctionPlan});
            return correctionResponse(correctionReceiptRaw(input: input));
          }),
        ),
      );
      await c.load();
      await c.reviewMemory(
        c.memories.single,
        action,
        summary: '本人明确修订',
        category: action == 'NEGATE' ? 'hiking' : null,
      );
      expect(c.canConfirm, true);
      expect(posts, 1);
      await c.confirm();
      await c.confirm();
      expect(posts, 2);
      expect(c.hasPending, false);
      expect(c.message, contains('已完成'));
      expect(await s.read('http://local', correctionOwner), isNull);
      c.dispose();
    });
  }
  test('NEGATE必须本人选闭集类别，不从summary猜', () async {
    var posts = 0;
    final c = AgentMemoryCorrectionController(
      authorizationHeader: () => 'Bearer synthetic',
      accountID: () => correctionOwner,
      pendingStore: MemoryAgentMemoryCorrectionPendingStore(),
      now: () => correctionAt,
      api: AgentMemoryCorrectionAPI(
        apiBaseUrl: 'http://local',
        client: MockClient((r) async {
          if (r.method == 'POST') posts++;
          return correctionResponse({
            'data': r.url.path.endsWith('agent-memories')
                ? [correctionMemoryRaw()]
                : [],
          });
        }),
      ),
    );
    await c.load();
    await c.reviewMemory(c.memories.single, 'NEGATE');
    await c.reviewMemory(c.memories.single, 'NEGATE', category: 'sports');
    expect(posts, 0);
    c.dispose();
  });
  test('原preview返回目标正文或期限变化拒绝批准', () async {
    var posts = 0;
    final c = AgentMemoryCorrectionController(
      authorizationHeader: () => 'Bearer synthetic',
      accountID: () => correctionOwner,
      pendingStore: MemoryAgentMemoryCorrectionPendingStore(),
      now: () => correctionAt,
      api: AgentMemoryCorrectionAPI(
        apiBaseUrl: 'http://local',
        client: MockClient((r) async {
          if (r.method == 'POST') {
            posts++;
            return correctionResponse(
              correctionPreviewRaw(
                input: Map<String, dynamic>.from(jsonDecode(r.body)),
                memory: {
                  ...correctionMemoryRaw(),
                  'summary': '伪同version正文',
                  'validUntil': correctionAt
                      .add(const Duration(days: 2))
                      .toIso8601String(),
                },
              ),
            );
          }
          return correctionResponse({
            'data': r.url.path.endsWith('agent-memories')
                ? [correctionMemoryRaw()]
                : [],
          });
        }),
      ),
    );
    await c.load();
    await c.reviewMemory(c.memories.single, 'REJECT');
    expect(c.preview, isNull);
    expect(c.canConfirm, false);
    expect(c.unknown, true);
    await c.confirm();
    expect(posts, 1);
    c.dispose();
  });
  for (final state in [
    'PENDING',
    'COMMITTED',
    'EXPIRED',
    '404',
    'wrong-target',
  ]) {
    test('关闭重开新Session GET-only $state 不还原正文批准', () async {
      final s = MemoryAgentMemoryCorrectionPendingStore();
      final p = correctionPending(phase: 'CONFIRM');
      await s.write('http://local', correctionOwner, p);
      final calls = <http.Request>[];
      final c = AgentMemoryCorrectionController(
        authorizationHeader: () => 'Bearer next',
        accountID: () => correctionOwner,
        pendingStore: s,
        now: () => correctionAt,
        api: AgentMemoryCorrectionAPI(
          apiBaseUrl: 'http://local',
          client: MockClient((r) async {
            calls.add(r);
            return correctionResponse(
              state == 'wrong-target'
                  ? {
                      ...correctionReceiptRaw(),
                      'target': {
                        'kind': 'MEMORY',
                        'id': correctionAgent,
                        'version': 1,
                      },
                    }
                  : correctionReceiptRaw(
                      state: {'PENDING', 'EXPIRED'}.contains(state)
                          ? state
                          : 'COMMITTED',
                      current: false,
                    ),
              status: state == '404' ? 404 : 200,
            );
          }),
        ),
      );
      await c.load();
      await c.confirm();
      expect(c.preview, isNull);
      expect(c.canConfirm, false);
      expect(
        calls.every(
          (r) =>
              r.method == 'GET' &&
              r.url.path.endsWith(p.id) &&
              r.bodyBytes.isEmpty,
        ),
        true,
      );
      if ({'PENDING', '404', 'wrong-target'}.contains(state)) {
        expect(c.unknown, true);
        expect((await s.read('http://local', correctionOwner))!.same(p), true);
      } else {
        expect(c.hasPending, false);
        expect(await s.read('http://local', correctionOwner), isNull);
        expect(c.message, contains(state == 'COMMITTED' ? '曾完成' : '未执行'));
      }
      c.dispose();
    });
  }
  test('本机reserve失败0POST，成功重新读取后解除storageBlocked', () async {
    final s = CorrectionGateStore()..fail = true;
    var posts = 0;
    final c = AgentMemoryCorrectionController(
      authorizationHeader: () => 'Bearer synthetic',
      accountID: () => correctionOwner,
      pendingStore: s,
      now: () => correctionAt,
      api: AgentMemoryCorrectionAPI(
        apiBaseUrl: 'http://local',
        client: MockClient((r) async {
          if (r.method == 'POST') posts++;
          return correctionResponse({
            'data': r.url.path.endsWith('agent-memories')
                ? [correctionMemoryRaw()]
                : [],
          });
        }),
      ),
    );
    await c.load();
    await c.reviewMemory(c.memories.single, 'REJECT');
    expect(posts, 0);
    expect(c.storageBlocked, true);
    s.fail = false;
    await c.load();
    expect(c.storageBlocked, false);
    expect(c.canReview, true);
    c.dispose();
  });
  for (final event in ['token-ABA', 'deadline']) {
    test('reserve等待后的$event 0POST', () async {
      final s = CorrectionGateStore();
      var token = 'Bearer synthetic', now = correctionAt, posts = 0;
      final c = AgentMemoryCorrectionController(
        authorizationHeader: () => token,
        accountID: () => correctionOwner,
        pendingStore: s,
        now: () => now,
        api: AgentMemoryCorrectionAPI(
          apiBaseUrl: 'http://local',
          client: MockClient((r) async {
            if (r.method == 'POST') posts++;
            return correctionResponse({
              'data': r.url.path.endsWith('agent-memories')
                  ? [correctionMemoryRaw()]
                  : [],
            });
          }),
        ),
      );
      await c.load();
      s.gate = Completer<void>();
      final f = c.reviewMemory(c.memories.single, 'REJECT');
      await Future<void>.delayed(Duration.zero);
      if (event == 'token-ABA') {
        token = 'Bearer next';
        c.identityChanged();
        token = 'Bearer synthetic';
        c.identityChanged();
      } else {
        now = correctionAt.add(const Duration(days: 2));
      }
      s.gate!.complete();
      await f;
      expect(posts, 0);
      expect(c.canConfirm, false);
      expect(c.canReview, false);
      c.dispose();
    });
  }
  test('双controller竞争原操作不能覆盖未知或发第二POST', () async {
    final s = MemoryAgentMemoryCorrectionPendingStore();
    var posts = 0;
    AgentMemoryCorrectionController build() => AgentMemoryCorrectionController(
      authorizationHeader: () => 'Bearer synthetic',
      accountID: () => correctionOwner,
      pendingStore: s,
      now: () => correctionAt,
      api: AgentMemoryCorrectionAPI(
        apiBaseUrl: 'http://local',
        client: MockClient((r) async {
          if (r.method == 'POST') {
            posts++;
            throw StateError('lost');
          }
          return correctionResponse({
            'data': r.url.path.endsWith('agent-memories')
                ? [correctionMemoryRaw()]
                : [],
          });
        }),
      ),
    );
    final a = build(), b = build();
    await a.load();
    await b.load();
    await a.reviewMemory(a.memories.single, 'REJECT');
    await b.reviewMemory(b.memories.single, 'REJECT');
    expect(posts, 1);
    expect(a.unknown, true);
    expect(b.storageBlocked, true);
    a.dispose();
    b.dispose();
  });
}
