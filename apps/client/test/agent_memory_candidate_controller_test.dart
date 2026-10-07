import 'package:birdtie_client/src/workspace/agent_candidate_pending_store.dart';
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:birdtie_client/src/workspace/agent_memory_candidate_api.dart';
import 'package:birdtie_client/src/workspace/agent_memory_candidate_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_memory_candidate_api_test.dart';
import 'agent_candidate_pending_store_test.dart' show pendingAcceptance;

class CandidateOwnedHttpClient implements HttpClient {
  int closes = 0;
  @override
  void close({bool force = false}) {
    closes++;
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class CandidateBorrowedHttpClient extends MockClient {
  CandidateBorrowedHttpClient(super.handler);
  int closes = 0;
  @override
  void close() {
    closes++;
    super.close();
  }
}

class FailingCandidateStore extends MemoryAgentCandidatePendingStore {
  bool failRead = false, failWrite = false, hideReadback = false;
  Completer<void>? pauseDelete;
  final deleteEntered = Completer<void>();
  Completer<void>? pauseWrite;
  final writeEntered = Completer<void>();
  @override
  Future<List<PendingHumanAcceptance>> read(
    String environment,
    String owner,
  ) async {
    if (failRead) {
      throw StateError('storage unavailable');
    }
    if (hideReadback && values.isNotEmpty) {
      return [];
    }
    return super.read(environment, owner);
  }

  @override
  Future<void> write(
    String environment,
    String owner,
    PendingHumanAcceptance value,
  ) async {
    if (failWrite) {
      throw StateError('storage unavailable');
    }
    if (!writeEntered.isCompleted) {
      writeEntered.complete();
    }
    await pauseWrite?.future;
    return super.write(environment, owner, value);
  }

  @override
  Future<void> delete(
    String environment,
    String owner,
    PendingHumanAcceptance captured,
  ) async {
    if (!deleteEntered.isCompleted) {
      deleteEntered.complete();
    }
    await pauseDelete?.future;
    return super.delete(environment, owner, captured);
  }
}

void main() {
  AgentMemoryCandidateController recoveryController(
    http.Client client,
    AgentCandidatePendingStore store, {
    String? Function()? auth,
  }) => AgentMemoryCandidateController(
    api: AgentMemoryCandidateAPI(client: client, apiBaseUrl: 'http://fixture'),
    pendingStore: store,
    authorizationHeader: auth ?? () => 'Bearer t',
    accountID: () => candidateOwner,
    organizationWorkspaceID: () => null,
  );

  test(
    'production default Secure store survives close reopen without restoring approval',
    () async {
      TestWidgetsFlutterBinding.ensureInitialized();
      FlutterSecureStorage.setMockInitialValues({});
      var accepts = 0;
      final c = MockClient((r) async {
        if (r.method == 'GET') {
          return candidateResponse(
            r.url.path == AgentMemoryCandidateAPI.path
                ? [candidateWire()]
                : candidateWire(),
          );
        }
        if (r.url.path.endsWith('/preview')) {
          return candidateResponse(
            previewWire(
              (jsonDecode(r.body) as Map)['previewId'],
              candidateWire(),
            ),
          );
        }
        accepts++;
        throw StateError('unknown');
      });
      AgentMemoryCandidateController make() => AgentMemoryCandidateController(
        api: AgentMemoryCandidateAPI(client: c, apiBaseUrl: 'http://fixture'),
        authorizationHeader: () => 'Bearer t',
        accountID: () => candidateOwner,
        organizationWorkspaceID: () => null,
      );
      final a = make();
      expect(a.pendingStore, isA<SecureAgentCandidatePendingStore>());
      await a.load();
      await a.prepare(a.records.single);
      await a.accept();
      a.dispose();
      final b = make();
      await b.load();
      expect(b.unknownAccept, true);
      expect(b.preview, isNull);
      await b.prepare(b.records.single);
      await b.accept();
      expect(accepts, 1);
      b.dispose();
      final raw = await const FlutterSecureStorage().readAll();
      expect(raw.length, 1);
      expect(raw.values.single, isNot(contains('Bearer t')));
      expect(raw.values.single, isNot(contains('我偏好')));
    },
  );
  test(
    'write success without immutable readback blocks dispatch and keeps unknown metadata',
    () async {
      final store = FailingCandidateStore();
      var accepts = 0;
      final c = MockClient((r) async {
        if (r.method == 'GET') {
          return candidateResponse([candidateWire()]);
        }
        if (r.url.path.endsWith('/preview')) {
          return candidateResponse(
            previewWire(
              (jsonDecode(r.body) as Map)['previewId'],
              candidateWire(),
            ),
          );
        }
        accepts++;
        return candidateResponse(candidateWire(status: 'ACTIVE'));
      });
      final d = recoveryController(c, store);
      await d.load();
      await d.prepare(d.records.single);
      store.hideReadback = true;
      await d.accept();
      expect(accepts, 0);
      expect(d.unknownAccept, true);
      expect(store.values.length, 1);
      await d.save();
      await d.accept();
      expect(accepts, 0);
      d.dispose();
    },
  );
  test(
    'late captured delete after identity ABA never deletes another pending entry or restores UI',
    () async {
      final store = FailingCandidateStore()..pauseDelete = Completer<void>();
      final a = pendingAcceptance();
      await store.write('http://fixture', candidateOwner, a);
      String? auth = 'Bearer t';
      final c = MockClient(
        (r) async => candidateResponse(
          r.url.path == AgentMemoryCandidateAPI.path
              ? [candidateWire(status: 'ACTIVE')]
              : candidateWire(status: 'ACTIVE'),
        ),
      );
      final d = recoveryController(c, store, auth: () => auth);
      final load = d.load();
      await store.deleteEntered.future;
      store.values.clear();
      final b = pendingAcceptance(key: 'c');
      await store.write('http://fixture', candidateOwner, b);
      auth = 'Bearer other';
      d.synchronizeIdentity();
      auth = 'Bearer t';
      d.synchronizeIdentity();
      store.pauseDelete!.complete();
      await load;
      expect(
        (await store.read('http://fixture', candidateOwner)).single.previewID,
        b.previewID,
      );
      expect(d.notice, isNull);
      expect(d.current, isNull);
      d.dispose();
    },
  );
  for (final failure in ['read', 'write', 'corrupt']) {
    test(
      'unsafe recovery storage $failure blocks all mutation with zero POST',
      () async {
        final store = FailingCandidateStore();
        var posts = 0;
        final c = MockClient((r) async {
          if (r.method != 'GET') {
            posts++;
          }
          if (r.url.path.endsWith('/preview')) {
            return candidateResponse(
              previewWire(
                (jsonDecode(r.body) as Map)['previewId'],
                candidateWire(),
              ),
            );
          }
          return candidateResponse([candidateWire()]);
        });
        final d = recoveryController(c, store);
        if (failure == 'read') {
          store.failRead = true;
        }
        if (failure == 'corrupt') {
          await store.write(
            'http://fixture',
            candidateOwner,
            pendingAcceptance(),
          );
          store.values[store.values.keys.single] = '{broken';
        }
        await d.load();
        if (failure == 'write') {
          await d.prepare(d.records.single);
          posts = 0;
          store.failWrite = true;
          await d.accept();
        }
        await d.save();
        await d.prepare(
          HumanMemoryCandidate.read(candidateWire(), candidateOwner),
        );
        await d.reject(
          HumanMemoryCandidate.read(candidateWire(), candidateOwner),
        );
        await d.accept();
        await d.loadSources();
        expect(posts, 0);
        expect(d.unknownAccept, true);
        expect(d.error, isNotNull);
        d.dispose();
      },
    );
  }
  test(
    'two old prepared States cannot submit another target past atomic pending reservation',
    () async {
      final store = MemoryAgentCandidatePendingStore();
      var posts = 0, previews = 0;
      final c = MockClient((r) async {
        if (r.method == 'GET') {
          return candidateResponse([candidateWire()]);
        }
        if (r.url.path.endsWith('/preview')) {
          previews++;
          final wire = previewWire(
            (jsonDecode(r.body) as Map)['previewId'],
            candidateWire(),
          );
          if (previews == 2) {
            (wire['review'] as Map)['targetMemoryId'] = candidateSaved;
          }
          return candidateResponse(wire);
        }
        posts++;
        throw StateError('unknown commit');
      });
      final a = recoveryController(c, store), b = recoveryController(c, store);
      await a.load();
      await b.load();
      await a.prepare(a.records.single);
      await b.prepare(b.records.single);
      await a.accept();
      await b.accept();
      expect(posts, 1);
      expect(b.unknownAccept, true);
      expect(
        (await store.read(
          'http://fixture',
          candidateOwner,
        )).single.data['targetMemoryId'],
        candidateMemory,
      );
      a.dispose();
      b.dispose();
    },
  );
  for (final mismatch in [
    'candidateVersion',
    'memoryVersion',
    'target',
    'agent',
  ]) {
    test(
      'reopened ACTIVE $mismatch mismatch never clears original pending',
      () async {
        final store = MemoryAgentCandidatePendingStore(),
            p = pendingAcceptance();
        await store.write('http://fixture', candidateOwner, p);
        final c = MockClient((r) async {
          final wire = candidateWire(status: 'ACTIVE');
          switch (mismatch) {
            case 'candidateVersion':
              wire['version'] = 3;
            case 'memoryVersion':
              wire['memoryVersion'] = 2;
            case 'target':
              wire['memoryId'] = candidateSaved;
            case 'agent':
              wire['agentId'] = candidateSaved;
          }
          return candidateResponse(
            r.url.path == AgentMemoryCandidateAPI.path ? [wire] : wire,
          );
        });
        final d = recoveryController(c, store);
        await d.load();
        expect(d.unknownAccept, true);
        expect(
          await store.read('http://fixture', candidateOwner),
          hasLength(1),
        );
        d.dispose();
      },
    );
  }
  test(
    'reopened exact ACTIVE acknowledges only original IDs and two versions',
    () async {
      final store = MemoryAgentCandidatePendingStore();
      await store.write('http://fixture', candidateOwner, pendingAcceptance());
      var posts = 0;
      final c = MockClient((r) async {
        if (r.method != 'GET') {
          posts++;
        }
        return candidateResponse(
          r.url.path == AgentMemoryCandidateAPI.path
              ? [candidateWire(status: 'ACTIVE')]
              : candidateWire(status: 'ACTIVE'),
        );
      });
      final d = recoveryController(c, store);
      await d.load();
      expect(d.unknownAccept, false);
      expect(d.notice, contains('无需重复提交'));
      expect(await store.read('http://fixture', candidateOwner), isEmpty);
      expect(posts, 0);
      d.dispose();
    },
  );
  test(
    'different Session discovers old metadata but cannot restore old review or submit',
    () async {
      final store = MemoryAgentCandidatePendingStore();
      await store.write('http://fixture', candidateOwner, pendingAcceptance());
      var posts = 0;
      final c = MockClient((r) async {
        if (r.method != 'GET') {
          posts++;
        }
        return candidateResponse(
          r.url.path == AgentMemoryCandidateAPI.path
              ? [candidateWire()]
              : candidateWire(),
        );
      });
      final d = recoveryController(c, store, auth: () => 'Bearer new');
      await d.load();
      await d.prepare(d.records.single);
      await d.accept();
      expect(posts, 0);
      expect(d.preview, isNull);
      expect(d.unknownAccept, true);
      expect(await store.read('http://fixture', candidateOwner), hasLength(1));
      d.dispose();
    },
  );
  test(
    'expired journal GET CANDIDATE permits only later explicit fresh review',
    () async {
      final store = MemoryAgentCandidatePendingStore();
      await store.write(
        'http://fixture',
        candidateOwner,
        pendingAcceptance(
          expires: DateTime.now().toUtc().subtract(const Duration(seconds: 1)),
        ),
      );
      var posts = 0;
      final c = MockClient((r) async {
        if (r.method != 'GET') {
          posts++;
        }
        if (r.url.path.endsWith('/preview')) {
          return candidateResponse(
            previewWire(
              (jsonDecode(r.body) as Map)['previewId'],
              candidateWire(),
            ),
          );
        }
        return candidateResponse(
          r.url.path == AgentMemoryCandidateAPI.path
              ? [candidateWire()]
              : candidateWire(),
        );
      });
      final d = recoveryController(c, store);
      await d.load();
      expect(posts, 0);
      expect(d.preview, isNull);
      expect(d.unknownAccept, false);
      await d.prepare(d.records.single);
      expect(posts, 1);
      d.dispose();
    },
  );
  for (final status in ['EXPIRED', '404']) {
    test(
      'recovery $status is current unavailability not proof that old save never committed',
      () async {
        final store = MemoryAgentCandidatePendingStore();
        await store.write(
          'http://fixture',
          candidateOwner,
          pendingAcceptance(),
        );
        final c = MockClient(
          (r) async =>
              status == '404' && r.url.path != AgentMemoryCandidateAPI.path
              ? http.Response(
                  '{"error":{"code":"not_found","message":"当前不可用"}}',
                  404,
                )
              : candidateResponse(
                  r.url.path == AgentMemoryCandidateAPI.path
                      ? []
                      : candidateWire(status: 'EXPIRED'),
                ),
        );
        final d = recoveryController(c, store);
        await d.load();
        expect(d.unknownAccept, true);
        expect(
          await store.read('http://fixture', candidateOwner),
          hasLength(1),
        );
        expect(d.notice, contains('未'));
        expect(d.notice, isNot(contains('从未')));
        d.dispose();
      },
    );
  }
  for (final change in ['identity', 'expiry']) {
    test('storage wait $change rechecks before first POST', () async {
      final store = FailingCandidateStore()..pauseWrite = Completer<void>();
      String? token = 'Bearer t';
      var posts = 0;
      final c = MockClient((r) async {
        if (r.method == 'GET') {
          return candidateResponse([candidateWire()]);
        }
        if (r.url.path.endsWith('/preview')) {
          return candidateResponse(
            previewWire(
              (jsonDecode(r.body) as Map)['previewId'],
              candidateWire(),
            ),
          );
        }
        posts++;
        return candidateResponse(candidateWire(status: 'ACTIVE'));
      });
      final d = recoveryController(c, store, auth: () => token);
      await d.load();
      await d.prepare(d.records.single);
      if (change == 'expiry') {
        final old = d.preview!;
        d.preview = HumanCandidatePreview(old.id, {
          ...old.review,
          'expiresAt': DateTime.now()
              .toUtc()
              .add(const Duration(seconds: 1))
              .toIso8601String(),
        }, old.candidate);
      }
      final pending = d.accept();
      await store.writeEntered.future;
      expect(d.preview!.expiresAt.isAfter(DateTime.now()), true);
      if (change == 'identity') {
        token = 'Bearer b';
        d.synchronizeIdentity();
        token = 'Bearer t';
        d.synchronizeIdentity();
      } else {
        await Future<void>.delayed(const Duration(milliseconds: 1100));
      }
      store.pauseWrite!.complete();
      await pending;
      expect(posts, 0);
      expect(await store.read('http://fixture', candidateOwner), hasLength(1));
      d.dispose();
    });
  }

  test(
    'verified same-page retry preserves original review but blocks every new target mutation',
    () async {
      final store = MemoryAgentCandidatePendingStore();
      var previews = 0, accepts = 0, otherPosts = 0;
      final c = MockClient((r) async {
        if (r.method == 'GET') {
          return candidateResponse(
            r.url.path == AgentMemoryCandidateAPI.path
                ? [candidateWire()]
                : candidateWire(),
          );
        }
        if (r.url.path.endsWith('/preview')) {
          previews++;
          return candidateResponse(
            previewWire(
              (jsonDecode(r.body) as Map)['previewId'],
              candidateWire(),
            ),
          );
        }
        if (r.url.path.endsWith('/accept')) {
          accepts++;
          throw StateError('unknown');
        }
        otherPosts++;
        return candidateResponse(candidateWire());
      });
      final d = recoveryController(c, store);
      await d.load();
      await d.prepare(d.records.single);
      final old = d.preview!;
      await d.accept();
      await d.verifyUnknown();
      expect(d.unknownAccept, false);
      expect(d.hasPendingAcceptance, true);
      d.edit(nextCategory: 'hiking');
      d.cancelPreview();
      await d.loadSources();
      await d.save();
      await d.prepare(d.current!);
      await d.reject(d.current!);
      expect(identical(d.preview, old), true);
      expect(previews, 1);
      expect(otherPosts, 0);
      expect(d.category, 'badminton');
      await d.accept();
      expect(accepts, 2);
      expect(
        (await store.read('http://fixture', candidateOwner)).single.previewID,
        old.id,
      );
      d.dispose();
    },
  );
  test('unknown accept survives controller close and reopen', () async {
    final store = MemoryAgentCandidatePendingStore();
    final client = MockClient((r) async {
      if (r.method == 'GET') {
        return candidateResponse(
          r.url.path == AgentMemoryCandidateAPI.path
              ? [candidateWire()]
              : candidateWire(),
        );
      }
      if (r.url.path.endsWith('/preview')) {
        return candidateResponse(
          previewWire(
            (jsonDecode(r.body) as Map)['previewId'],
            candidateWire(),
          ),
        );
      }
      throw StateError('unknown commit');
    });
    AgentMemoryCandidateController create() => AgentMemoryCandidateController(
      pendingStore: store,
      api: AgentMemoryCandidateAPI(
        client: client,
        apiBaseUrl: 'http://fixture',
      ),
      authorizationHeader: () => 'Bearer t',
      accountID: () => candidateOwner,
      organizationWorkspaceID: () => null,
    );
    final first = create();
    await first.load();
    await first.prepare(first.records.single);
    await first.accept();
    expect(first.unknownAccept, true);
    first.dispose();
    final reopened = create();
    await reopened.load();
    expect(reopened.unknownAccept, true);
    reopened.dispose();
  });

  test('controller disposal closes owned IO transport exactly once', () async {
    final owned = CandidateOwnedHttpClient();
    await HttpOverrides.runZoned(() async {
      final d = AgentMemoryCandidateController(
        pendingStore: MemoryAgentCandidatePendingStore(),
        api: AgentMemoryCandidateAPI(apiBaseUrl: 'http://fixture'),
        authorizationHeader: () => 'Bearer t',
        accountID: () => candidateOwner,
        organizationWorkspaceID: () => null,
      );
      d.dispose();
      d.dispose();
      expect(owned.closes, 1);
    }, createHttpClient: (_) => owned);
  });
  for (final outcome in ['confirmed', 'unknown']) {
    test(
      'disposed late $outcome accept clears private state and never resends',
      () async {
        final pending = Completer<http.Response>();
        var posts = 0, getters = 0, notifications = 0;
        final c = CandidateBorrowedHttpClient((r) async {
          if (r.method == 'GET') {
            return candidateResponse([candidateWire()]);
          }
          if (r.url.path.endsWith('/preview')) {
            return candidateResponse(
              previewWire(
                (jsonDecode(r.body) as Map)['previewId'] as String,
                candidateWire(),
              ),
            );
          }
          posts++;
          return pending.future;
        });
        final d = AgentMemoryCandidateController(
          pendingStore: MemoryAgentCandidatePendingStore(),
          api: AgentMemoryCandidateAPI(client: c, apiBaseUrl: 'http://fixture'),
          authorizationHeader: () {
            getters++;
            return 'Bearer t';
          },
          accountID: () => candidateOwner,
          organizationWorkspaceID: () => null,
        );
        await d.load();
        await d.prepare(d.records.single);
        expect(d.preview, isNotNull);
        d.addListener(() {
          notifications++;
        });
        final accept = d.accept();
        await Future<void>.delayed(Duration.zero);
        d.dispose();
        d.dispose();
        final n = notifications, g = getters;
        d.synchronizeIdentity();
        if (outcome == 'unknown') {
          pending.completeError(Exception('unknown commit'));
        } else {
          pending.complete(candidateResponse(candidateWire(status: 'ACTIVE')));
        }
        await accept;
        await d.accept();
        await d.load();
        expect(getters, g);
        expect(notifications, n);
        expect(posts, 1);
        expect(c.closes, 0);
        expect(d.records, isEmpty);
        expect(d.choices, isEmpty);
        expect(d.selected, isEmpty);
        expect(d.preview, isNull);
        expect(d.current, isNull);
        expect(d.notice, isNull);
        expect(d.error, isNull);
        expect(d.unknownAccept, isFalse);
        expect(d.busy, isFalse);
        c.close();
        expect(c.closes, 1);
      },
    );
  }
  test(
    'pending unknown receipt retains original token and target for explicit retry',
    () async {
      var previews = 0, posts = 0;
      String? key;
      final c = MockClient((r) async {
        if (r.url.path.endsWith('/preview')) {
          previews++;
          key = (jsonDecode(r.body) as Map)['previewId'] as String;
          return candidateResponse(previewWire(key!, candidateWire()));
        }
        if (r.method == 'POST') {
          posts++;
          expect(jsonDecode(r.body), {'previewId': key});
          if (posts == 1) {
            throw Exception('unknown before commit');
          }
          return candidateResponse(candidateWire(status: 'ACTIVE'));
        }
        return candidateResponse(
          r.url.path.endsWith(candidateTarget)
              ? candidateWire()
              : [candidateWire()],
        );
      });
      final d = AgentMemoryCandidateController(
        pendingStore: MemoryAgentCandidatePendingStore(),
        api: AgentMemoryCandidateAPI(client: c, apiBaseUrl: 'http://fixture'),
        authorizationHeader: () => 'Bearer t',
        accountID: () => candidateOwner,
        organizationWorkspaceID: () => null,
      );
      await d.load();
      await d.prepare(d.records.single);
      final p = d.preview;
      await d.accept();
      await d.verifyUnknown();
      expect(d.preview, same(p));
      expect(d.unknownAccept, isFalse);
      expect(previews, 1);
      await d.accept();
      expect(posts, 2);
      expect(previews, 1);
      expect(d.current!.memoryID, candidateMemory);
      d.dispose();
      c.close();
    },
  );
  test('expired concrete preview causes zero approval POST', () async {
    var approvals = 0;
    final c = MockClient((r) async {
      if (r.url.path.endsWith('/preview')) {
        final p = previewWire(
          (jsonDecode(r.body) as Map)['previewId'] as String,
          candidateWire(),
        );
        (p['review'] as Map)['expiresAt'] = DateTime.now()
            .toUtc()
            .add(const Duration(milliseconds: 100))
            .toIso8601String();
        return candidateResponse(p);
      }
      if (r.method == 'POST') {
        approvals++;
      }
      return candidateResponse([candidateWire()]);
    });
    final d = AgentMemoryCandidateController(
      pendingStore: MemoryAgentCandidatePendingStore(),
      api: AgentMemoryCandidateAPI(client: c, apiBaseUrl: 'http://fixture'),
      authorizationHeader: () => 'Bearer t',
      accountID: () => candidateOwner,
      organizationWorkspaceID: () => null,
    );
    await d.load();
    await d.prepare(d.records.single);
    expect(d.preview, isNotNull);
    await Future<void>.delayed(const Duration(milliseconds: 150));
    await d.accept();
    expect(approvals, 0);
    expect(d.preview, isNull);
    expect(d.error, contains('已失效'));
    d.dispose();
    c.close();
  });
  test(
    'manual preview cancel no accept and explicit confirm uses exact opaque token',
    () async {
      var accepts = 0;
      String? key;
      final client = MockClient((r) async {
        if (r.method == 'GET') {
          return candidateResponse([candidateWire()]);
        }
        final body = jsonDecode(r.body) as Map;
        if (r.url.path.endsWith('/preview')) {
          key = body['previewId'] as String;
          expect(body.keys.toSet(), {
            'previewId',
            'expectedVersion',
            'memoryValidUntil',
          });
          return candidateResponse(previewWire(key!, candidateWire()));
        }
        accepts++;
        expect(body, {'previewId': key});
        return candidateResponse(candidateWire(status: 'ACTIVE'));
      });
      final d = AgentMemoryCandidateController(
        pendingStore: MemoryAgentCandidatePendingStore(),
        api: AgentMemoryCandidateAPI(
          client: client,
          apiBaseUrl: 'http://fixture',
        ),
        authorizationHeader: () => 'Bearer t',
        accountID: () => candidateOwner,
        organizationWorkspaceID: () => null,
      );
      await d.load();
      await d.prepare(d.records.single);
      expect(d.preview, isNotNull);
      d.cancelPreview();
      expect(accepts, 0);
      await d.prepare(d.records.single);
      await d.accept();
      expect(accepts, 1);
      expect(d.current!.status, 'ACTIVE');
      d.dispose();
      client.close();
    },
  );
  for (final identity in ['token', 'account', 'workspace', 'dispose']) {
    test('late response rejected after $identity change', () async {
      var token = 'Bearer a', owner = candidateOwner;
      String? workspace;
      final pending = Completer<http.Response>();
      final c = MockClient((_) => pending.future);
      final d = AgentMemoryCandidateController(
        pendingStore: MemoryAgentCandidatePendingStore(),
        api: AgentMemoryCandidateAPI(client: c, apiBaseUrl: 'http://fixture'),
        authorizationHeader: () => token,
        accountID: () => owner,
        organizationWorkspaceID: () => workspace,
      );
      final load = d.load();
      await Future<void>.delayed(Duration.zero);
      switch (identity) {
        case 'token':
          token = 'Bearer b';
        case 'account':
          owner = candidateAgent;
        case 'workspace':
          workspace = 'org';
        case 'dispose':
          d.dispose();
      }
      if (identity != 'dispose') {
        d.synchronizeIdentity();
      }
      pending.complete(candidateResponse([candidateWire()]));
      await load;
      expect(d.records, isEmpty);
      expect(d.preview, isNull);
      if (identity != 'dispose') {
        d.dispose();
      }
      c.close();
    });
  }
  test(
    'unknown accept resolves original native candidate without second POST',
    () async {
      var posts = 0, reads = 0;
      final c = MockClient((r) async {
        if (r.url.path.endsWith('/preview')) {
          final b = jsonDecode(r.body) as Map;
          return candidateResponse(
            previewWire(b['previewId'] as String, candidateWire()),
          );
        }
        if (r.method == 'POST') {
          posts++;
          throw Exception('lost after native commit');
        }
        if (r.url.path.endsWith(candidateTarget)) {
          reads++;
          return candidateResponse(candidateWire(status: 'ACTIVE'));
        }
        return candidateResponse([candidateWire()]);
      });
      final d = AgentMemoryCandidateController(
        pendingStore: MemoryAgentCandidatePendingStore(),
        api: AgentMemoryCandidateAPI(client: c, apiBaseUrl: 'http://fixture'),
        authorizationHeader: () => 'Bearer t',
        accountID: () => candidateOwner,
        organizationWorkspaceID: () => null,
      );
      await d.load();
      await d.prepare(d.records.single);
      await d.accept();
      expect(d.unknownAccept, isTrue);
      await d.accept();
      expect(posts, 1);
      await d.verifyUnknown();
      expect(reads, 1);
      expect(posts, 1);
      expect(d.unknownAccept, isFalse);
      expect(d.current!.status, 'ACTIVE');
      d.dispose();
      c.close();
    },
  );
  test('organization workspace never invokes network', () async {
    var calls = 0;
    final c = MockClient((r) async {
      calls++;
      return candidateResponse([]);
    });
    final d = AgentMemoryCandidateController(
      pendingStore: MemoryAgentCandidatePendingStore(),
      api: AgentMemoryCandidateAPI(client: c, apiBaseUrl: 'http://fixture'),
      authorizationHeader: () => 'Bearer t',
      accountID: () => candidateOwner,
      organizationWorkspaceID: () => 'org',
    );
    await d.load();
    await d.loadSources();
    await d.save();
    expect(calls, 0);
    d.dispose();
    c.close();
  });
}
