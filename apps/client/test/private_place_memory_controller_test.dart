import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/private_place_memory_api.dart';
import 'package:birdtie_client/src/workspace/private_place_memory_controller.dart';
import 'private_place_memory_api_test.dart';

class OwnedPlaceControllerClient implements HttpClient {
  int closes = 0;
  @override
  void close({bool force = false}) {
    closes++;
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class BorrowedPlaceControllerClient extends MockClient {
  BorrowedPlaceControllerClient(super.handler);
  int closes = 0;
  @override
  void close() {
    closes++;
    super.close();
  }
}

void main() {
  test(
    'closed controller disposes owned IO exactly once and no longer reads identity',
    () async {
      final owned = OwnedPlaceControllerClient();
      var getters = 0;
      await HttpOverrides.runZoned(() async {
        final c = PrivatePlaceMemoryController(
          placeID: placeTarget,
          authorizationHeader: () {
            getters++;
            return 'Bearer local';
          },
          accountID: () => placeOwner,
          pendingStore: MemoryPlacePendingStore(),
          api: PrivatePlaceMemoryApi(apiBaseUrl: 'http://fixture'),
        );
        c.dispose();
        c.dispose();
        c.synchronizeIdentity();
        await c.load();
        await c.retryOriginal();
        await c.stopRecovery();
        expect(owned.closes, 1);
        expect(getters, 0);
        expect(c.personal, isFalse);
      }, createHttpClient: (_) => owned);
    },
  );
  for (final result in ['confirmed', 'unknown']) {
    test(
      'dispose during $result native write preserves original pending without notifications or retry',
      () async {
        final store = MemoryPlacePendingStore(),
            pending = Completer<http.Response>();
        http.Request? write;
        var calls = 0, notifications = 0;
        final client = BorrowedPlaceControllerClient((r) async {
          calls++;
          if (r.method == 'GET') {
            return placeResponse(
              r.url.path.endsWith('/declarations')
                  ? placeControlsFixture()
                  : placeReadFixture(),
            );
          }
          write = r;
          return pending.future;
        });
        final c = PrivatePlaceMemoryController(
          placeID: placeTarget,
          authorizationHeader: () => 'Bearer local',
          accountID: () => placeOwner,
          pendingStore: store,
          api: PrivatePlaceMemoryApi(
            client: client,
            apiBaseUrl: 'http://old-fixture',
          ),
        );
        await c.load();
        c.edit(
          PlaceDeclarationDraft(
            kind: 'LIKED',
            visibility: 'PRIVATE',
            validUntil: DateTime.now().toUtc().add(const Duration(days: 7)),
          ),
        );
        final approved = c.preview()!;
        c.addListener(() {
          notifications++;
        });
        final submit = c.submit(approved);
        await Future<void>.delayed(Duration.zero);
        expect(write, isNotNull);
        expect(store.items.length, 1);
        final raw = Map.of(store.items), beforeCalls = calls;
        c.dispose();
        c.dispose();
        final beforeNotifications = notifications;
        if (result == 'unknown') {
          pending.completeError(StateError('lost receipt'));
        } else {
          pending.complete(placeResponse(placeReceiptFixture(write!)));
        }
        await submit;
        await c.submit(approved);
        await c.retryOriginal();
        await c.stopRecovery();
        await c.load();
        expect(store.items, raw);
        expect(calls, beforeCalls);
        expect(notifications, beforeNotifications);
        expect(client.closes, 0);
        expect(c.view, isNull);
        expect(c.controls, isNull);
        expect(c.pending, isNull);
        expect(c.draft, isNull);
        expect(c.message, isNull);
        expect(c.error, isNull);
        expect(c.saving, isFalse);
        expect(c.loading, isFalse);
        client.close();
        expect(client.closes, 1);
      },
    );
  }
  test(
    'read and concrete approval normal success, original receipt only',
    () async {
      var puts = 0;
      final store = MemoryPlacePendingStore();
      final c = PrivatePlaceMemoryController(
        placeID: placeTarget,
        authorizationHeader: () => 'Bearer local',
        accountID: () => placeOwner,
        pendingStore: store,
        api: PrivatePlaceMemoryApi(
          apiBaseUrl: 'http://127.0.0.1',
          client: placeFixtureClient((r) async {
            if (r.method == 'GET') return placeResponse(placeReadFixture());
            puts++;
            return placeResponse(placeReceiptFixture(r));
          }),
        ),
      );
      addTearDown(c.dispose);
      await c.load();
      c.edit(
        PlaceDeclarationDraft(
          kind: 'VISITED',
          visibility: 'PRIVATE',
          validUntil: DateTime.now().toUtc().add(const Duration(days: 7)),
        ),
      );
      final approved = c.preview()!;
      await c.submit(approved);
      expect(puts, 1);
      expect(c.uncertain, isFalse);
      expect(c.message, contains('不是到访核验'));
      expect(store.items, isEmpty);
      await c.submit(approved);
      expect(puts, 1);
    },
  );
  test('edited draft and reloaded source invalidate old approval', () async {
    var puts = 0;
    final c = PrivatePlaceMemoryController(
      placeID: placeTarget,
      authorizationHeader: () => 'Bearer local',
      accountID: () => placeOwner,
      pendingStore: MemoryPlacePendingStore(),
      api: PrivatePlaceMemoryApi(
        apiBaseUrl: 'http://127.0.0.1',
        client: placeFixtureClient((r) async {
          if (r.method == 'GET') return placeResponse(placeReadFixture());
          puts++;
          return placeResponse(placeReceiptFixture(r));
        }),
      ),
    );
    addTearDown(c.dispose);
    await c.load();
    final d = PlaceDeclarationDraft(
      kind: 'LIKED',
      visibility: 'PRIVATE',
      validUntil: DateTime.now().toUtc().add(const Duration(days: 7)),
    );
    c.edit(d);
    final a = c.preview()!;
    c.edit(
      PlaceDeclarationDraft(
        kind: 'VISITED',
        visibility: 'PRIVATE',
        validUntil: d.validUntil,
      ),
    );
    await c.submit(a);
    expect(puts, 0);
    final b = c.preview()!;
    await c.load();
    await c.submit(b);
    expect(puts, 0);
  });
  test('ABA and late response never show previous account source', () async {
    String token = 'Bearer A';
    final wait = Completer<http.Response>();
    final c = PrivatePlaceMemoryController(
      placeID: placeTarget,
      authorizationHeader: () => token,
      accountID: () => placeOwner,
      pendingStore: MemoryPlacePendingStore(),
      api: PrivatePlaceMemoryApi(
        apiBaseUrl: 'http://127.0.0.1',
        client: placeFixtureClient((r) => wait.future),
      ),
    );
    addTearDown(c.dispose);
    final load = c.load();
    await Future<void>.delayed(Duration.zero);
    token = 'Bearer B';
    c.synchronizeIdentity();
    token = 'Bearer A';
    c.synchronizeIdentity();
    wait.complete(
      placeResponse(placeReadFixture(signals: [placeSignalFixture()])),
    );
    await load;
    expect(c.view, isNull);
    expect(c.draft, isNull);
  });
  test('organization and wrong owner DTO cannot load', () async {
    String? workspace = 'organization';
    final c = PrivatePlaceMemoryController(
      placeID: placeTarget,
      authorizationHeader: () => 'Bearer local',
      accountID: () => placeOwner,
      organizationWorkspaceID: () => workspace,
      pendingStore: MemoryPlacePendingStore(),
      api: PrivatePlaceMemoryApi(
        apiBaseUrl: 'http://127.0.0.1',
        client: placeFixtureClient(
          (r) async => placeResponse(placeReadFixture(owner: placeAgent)),
        ),
      ),
    );
    addTearDown(c.dispose);
    await c.load();
    expect(c.view, isNull);
    workspace = null;
    await c.load();
    expect(c.view, isNull);
    expect(c.preview(), isNull);
  });
  test(
    'lost response persists across close and safely replays same PUT/CAS',
    () async {
      final store = MemoryPlacePendingStore();
      final sent = <http.Request>[];
      var fail = true;
      final client = placeFixtureClient((r) async {
        if (r.method == 'GET') return placeResponse(placeReadFixture());
        sent.add(r);
        if (fail) throw StateError('lost response after commit');
        return placeResponse(placeReceiptFixture(r));
      });
      PrivatePlaceMemoryController make() => PrivatePlaceMemoryController(
        placeID: placeTarget,
        authorizationHeader: () => 'Bearer local',
        accountID: () => placeOwner,
        pendingStore: store,
        api: PrivatePlaceMemoryApi(
          apiBaseUrl: 'http://127.0.0.1',
          client: client,
        ),
      );
      var c = make();
      await c.load();
      c.edit(
        PlaceDeclarationDraft(
          kind: 'LIKED',
          visibility: 'PRIVATE',
          validUntil: DateTime.now().toUtc().add(const Duration(days: 7)),
        ),
      );
      await c.submit(c.preview()!);
      expect(c.uncertain, isTrue);
      expect(c.preview(), isNull);
      c.dispose();
      c = make();
      addTearDown(c.dispose);
      await c.load();
      expect(c.uncertain, isTrue);
      expect(c.message, isNull);
      fail = false;
      await c.retryOriginal();
      expect(sent.length, 2);
      expect(sent[1].url, sent[0].url);
      expect(sent[1].body, sent[0].body);
      expect(c.uncertain, isFalse);
    },
  );
  test(
    'DELETE lost response remains UNKNOWN after absence and original CAS resolves',
    () async {
      var deleting = 0;
      final store = MemoryPlacePendingStore();
      final client = placeFixtureClient((r) async {
        if (r.method == 'GET') {
          return placeResponse(
            placeReadFixture(
              signals: deleting == 0 ? [placeSignalFixture()] : [],
            ),
          );
        }
        deleting++;
        if (deleting == 1) throw StateError('lost delete');
        return placeResponse(placeReceiptFixture(r));
      });
      final c = PrivatePlaceMemoryController(
        placeID: placeTarget,
        authorizationHeader: () => 'Bearer local',
        accountID: () => placeOwner,
        pendingStore: store,
        api: PrivatePlaceMemoryApi(
          apiBaseUrl: 'http://127.0.0.1',
          client: client,
        ),
      );
      addTearDown(c.dispose);
      await c.load();
      await c.submit(c.preview(deleting: c.controls!.declarations.single)!);
      expect(c.uncertain, isTrue);
      await c.load();
      expect(c.view!.signals, isEmpty);
      expect(c.uncertain, isTrue);
      expect(c.message, isNull);
      await c.retryOriginal();
      expect(c.uncertain, isFalse);
      expect(c.message, contains('撤回回执'));
    },
  );
  test(
    'late write source/auth rejection is unknown not false no-write',
    () async {
      for (final status in [401, 403, 404, 409, 503]) {
        final store = MemoryPlacePendingStore();
        final c = PrivatePlaceMemoryController(
          placeID: placeTarget,
          authorizationHeader: () => 'Bearer local',
          accountID: () => placeOwner,
          pendingStore: store,
          api: PrivatePlaceMemoryApi(
            apiBaseUrl: 'http://127.0.0.1',
            client: placeFixtureClient(
              (r) async => r.method == 'GET'
                  ? placeResponse(placeReadFixture())
                  : http.Response('{}', status),
            ),
          ),
        );
        await c.load();
        c.edit(
          PlaceDeclarationDraft(
            kind: 'LIKED',
            visibility: 'PRIVATE',
            validUntil: DateTime.now().toUtc().add(const Duration(days: 7)),
          ),
        );
        await c.submit(c.preview()!);
        expect(c.uncertain, isTrue, reason: '$status may follow commit');
        expect(store.items, isNotEmpty);
        expect(c.message, isNull);
        c.dispose();
      }
    },
  );
  test(
    'durable note failure blocks dispatch and duplicate click sends once',
    () async {
      final store = MemoryPlacePendingStore()..failWrite = true;
      var puts = 0;
      final wait = Completer<http.Response>();
      final c = PrivatePlaceMemoryController(
        placeID: placeTarget,
        authorizationHeader: () => 'Bearer local',
        accountID: () => placeOwner,
        pendingStore: store,
        api: PrivatePlaceMemoryApi(
          apiBaseUrl: 'http://127.0.0.1',
          client: placeFixtureClient((r) async {
            if (r.method == 'GET') return placeResponse(placeReadFixture());
            puts++;
            return wait.future;
          }),
        ),
      );
      addTearDown(c.dispose);
      await c.load();
      c.edit(
        PlaceDeclarationDraft(
          kind: 'LIKED',
          visibility: 'PRIVATE',
          validUntil: DateTime.now().toUtc().add(const Duration(days: 7)),
        ),
      );
      final a = c.preview()!;
      await c.submit(a);
      expect(puts, 0);
      store.failWrite = false;
      final save = c.submit(a);
      await Future<void>.delayed(Duration.zero);
      await c.submit(a);
      expect(puts, 1);
      final p = c.pending!;
      wait.complete(
        http.Response(
          jsonEncode({
            'data': {
              'schemaVersion': 'human-place-declaration-receipt-v1',
              'ownerId': placeOwner,
              'agentId': placeAgent,
              'memoryId': p.memoryID,
              'version': 1,
              'status': 'ACTIVE',
              'placeId': placeTarget,
              'kind': 'LIKED',
              'basis': 'SELF_DECLARATION',
              'visibility': 'PRIVATE',
              'validUntil': p.draft!.validUntil.toUtc().toIso8601String(),
            },
          }),
          200,
        ),
      );
      await save;
      expect(c.uncertain, isFalse);
    },
  );
  test(
    'new Session/Agent cannot reuse persisted approval, stop is not success',
    () async {
      final store = MemoryPlacePendingStore();
      await store.write(
        'http://127.0.0.1',
        placeOwner,
        placeTarget,
        PendingPlaceDeclaration(
          memoryID: placeMemory,
          agentID: placeAgent,
          expectedVersion: 1,
          sessionFingerprint: placeSessionFingerprint('Bearer old'),
          deleting: true,
        ),
      );
      var writes = 0;
      final c = PrivatePlaceMemoryController(
        placeID: placeTarget,
        authorizationHeader: () => 'Bearer new',
        accountID: () => placeOwner,
        pendingStore: store,
        api: PrivatePlaceMemoryApi(
          apiBaseUrl: 'http://127.0.0.1',
          client: placeFixtureClient((r) async {
            if (r.method != 'GET') writes++;
            return placeResponse(placeReadFixture());
          }),
        ),
      );
      addTearDown(c.dispose);
      await c.load();
      expect(c.canRetryOriginal, isFalse);
      await c.retryOriginal();
      expect(writes, 0);
      await c.stopRecovery();
      expect(c.view, isNull);
      expect(c.message, contains('结果仍未知'));
      expect(c.preview(), isNull);
    },
  );
  test('expired read lease requires current read before preview', () async {
    final n = DateTime.now();
    var clock = n;
    final c = PrivatePlaceMemoryController(
      placeID: placeTarget,
      authorizationHeader: () => 'Bearer local',
      accountID: () => placeOwner,
      now: () => clock,
      pendingStore: MemoryPlacePendingStore(),
      api: PrivatePlaceMemoryApi(
        apiBaseUrl: 'http://127.0.0.1',
        client: placeFixtureClient(
          (r) async => placeResponse(placeReadFixture(now: n)),
        ),
      ),
    );
    addTearDown(c.dispose);
    await c.load();
    c.edit(
      PlaceDeclarationDraft(
        kind: 'LIKED',
        visibility: 'PRIVATE',
        validUntil: n.add(const Duration(days: 7)),
      ),
    );
    clock = n.add(const Duration(minutes: 3));
    expect(c.preview(), isNull);
  });

  test(
    'expired declaration renews original ID/version, hidden target permits withdrawal only',
    () async {
      var hidden = false;
      final sent = <http.Request>[];
      final expired = placeDeclarationFixture(
        version: 4,
        status: 'EXPIRED',
        until: DateTime.now().toUtc().subtract(const Duration(days: 1)),
      );
      final c = PrivatePlaceMemoryController(
        placeID: placeTarget,
        authorizationHeader: () => 'Bearer local',
        accountID: () => placeOwner,
        pendingStore: MemoryPlacePendingStore(),
        api: PrivatePlaceMemoryApi(
          apiBaseUrl: 'http://127.0.0.1',
          client: placeFixtureClient((r) async {
            if (r.method == 'GET') {
              if (r.url.path.endsWith('/declarations')) {
                return placeResponse(
                  placeControlsFixture(declarations: [expired]),
                );
              }
              return hidden
                  ? http.Response('{}', 404)
                  : placeResponse(placeReadFixture());
            }
            sent.add(r);
            return placeResponse(placeReceiptFixture(r));
          }),
        ),
      );
      addTearDown(c.dispose);
      await c.load();
      c.edit(
        PlaceDeclarationDraft(
          kind: 'LIKED',
          visibility: 'PRIVATE',
          validUntil: DateTime.now().toUtc().add(const Duration(days: 7)),
        ),
      );
      final p = c.preview()!;
      expect(p.pending.memoryID, placeMemory);
      expect(p.pending.expectedVersion, 4);
      await c.submit(p);
      expect(sent.single.url.path.endsWith(placeMemory), isTrue);
      expect(jsonDecode(sent.single.body)['expectedVersion'], 4);
      hidden = true;
      await c.load();
      expect(c.view, isNull);
      expect(c.controls!.declarations.single.status, 'EXPIRED');
      c.edit(
        PlaceDeclarationDraft(
          kind: 'LIKED',
          visibility: 'PRIVATE',
          validUntil: DateTime.now().toUtc().add(const Duration(days: 7)),
        ),
      );
      expect(c.preview(), isNull);
      await c.submit(c.preview(deleting: c.controls!.declarations.single)!);
      expect(sent.last.method, 'DELETE');
      expect(c.message, contains('撤回回执'));
    },
  );
  test(
    'wrong control owner and Agent/source disagreement reject human draft',
    () async {
      for (final mismatch in ['owner', 'agent']) {
        final c = PrivatePlaceMemoryController(
          placeID: placeTarget,
          authorizationHeader: () => 'Bearer local',
          accountID: () => placeOwner,
          pendingStore: MemoryPlacePendingStore(),
          api: PrivatePlaceMemoryApi(
            apiBaseUrl: 'http://127.0.0.1',
            client: placeFixtureClient(
              (r) async => placeResponse(
                r.url.path.endsWith('/declarations')
                    ? placeControlsFixture(
                        owner: mismatch == 'owner' ? placeAgent : placeOwner,
                        agent: mismatch == 'agent' ? placeMemory : placeAgent,
                      )
                    : placeReadFixture(),
              ),
            ),
          ),
        );
        await c.load();
        expect(c.view, isNull);
        expect(c.controls, isNull);
        expect(c.preview(), isNull);
        c.dispose();
      }
    },
  );

  test(
    'Agent changed during concrete confirmation blocks unsent create',
    () async {
      var reads = 0, writes = 0;
      final c = PrivatePlaceMemoryController(
        placeID: placeTarget,
        authorizationHeader: () => 'Bearer local',
        accountID: () => placeOwner,
        pendingStore: MemoryPlacePendingStore(),
        api: PrivatePlaceMemoryApi(
          apiBaseUrl: 'http://127.0.0.1',
          client: placeFixtureClient((r) async {
            if (r.method == 'GET') {
              if (r.url.path.endsWith('/declarations')) {
                reads++;
                return placeResponse(
                  placeControlsFixture(
                    agent: reads > 1 ? placeMemory : placeAgent,
                  ),
                );
              }
              return placeResponse(placeReadFixture());
            }
            writes++;
            return placeResponse(placeReceiptFixture(r));
          }),
        ),
      );
      addTearDown(c.dispose);
      await c.load();
      c.edit(
        PlaceDeclarationDraft(
          kind: 'LIKED',
          visibility: 'PRIVATE',
          validUntil: DateTime.now().toUtc().add(const Duration(days: 7)),
        ),
      );
      await c.submit(c.preview()!);
      expect(writes, 0);
      expect(c.pending, isNull);
      expect(c.error, contains('尚未发送'));
    },
  );

  test(
    'unavailable preflight is unsent and does not misreport storage or create pending',
    () async {
      var gets = 0, writes = 0;
      final store = MemoryPlacePendingStore();
      final c = PrivatePlaceMemoryController(
        placeID: placeTarget,
        authorizationHeader: () => 'Bearer local',
        accountID: () => placeOwner,
        pendingStore: store,
        api: PrivatePlaceMemoryApi(
          apiBaseUrl: 'http://127.0.0.1',
          client: placeFixtureClient((r) async {
            if (r.method == 'GET') {
              if (r.url.path.endsWith('/declarations') && ++gets > 1) {
                return http.Response('{}', 503);
              }
              return placeResponse(placeReadFixture());
            }
            writes++;
            return placeResponse(placeReceiptFixture(r));
          }),
        ),
      );
      addTearDown(c.dispose);
      await c.load();
      c.edit(
        PlaceDeclarationDraft(
          kind: 'LIKED',
          visibility: 'PRIVATE',
          validUntil: DateTime.now().toUtc().add(const Duration(days: 7)),
        ),
      );
      await c.submit(c.preview()!);
      expect(writes, 0);
      expect(store.items, isEmpty);
      expect(c.error, contains('复核失败，尚未发送'));
      expect(c.error, isNot(contains('设备存储')));
    },
  );
}
