import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/activity_participation_operation.dart';
import 'package:birdtie_client/src/workspace/activity_participation_operation_controller.dart';
import 'package:birdtie_client/src/workspace/activity_participation_pending_store.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'activity_participation_operation_contract_test.dart';

class ParticipationSignals extends ChangeNotifier {
  String? owner = participationOwner, token = 'Bearer original', workspace;
  void change({String? newOwner, String? newToken, String? newWorkspace}) {
    owner = newOwner;
    token = newToken;
    workspace = newWorkspace;
    notifyListeners();
  }
}

class FailingParticipationStore extends MemoryParticipationPendingStore {
  @override
  Future<void> write(
    String e,
    String o,
    PendingParticipationOperation p,
  ) async {
    throw StateError('secure storage failed');
  }
}

class ObservingParticipationStore extends MemoryParticipationPendingStore {
  final environments = <String>[];
  @override
  Future<List<PendingParticipationOperation>> read(String e, String o) {
    environments.add(e);
    return super.read(e, o);
  }

  @override
  Future<void> write(String e, String o, PendingParticipationOperation p) {
    environments.add(e);
    return super.write(e, o, p);
  }

  @override
  Future<void> delete(String e, String o, PendingParticipationOperation p) {
    environments.add(e);
    return super.delete(e, o, p);
  }
}

ParticipationOperationController recoveryController(
  http.Client client,
  ParticipationPendingStore store, {
  ParticipationSignals? signals,
  bool Function()? current,
  Duration? timeout,
  String base = 'http://api.test',
  String operationID = participationOp,
}) => ParticipationOperationController(
  activityID: participationActivity,
  base: base,
  client: client,
  store: store,
  ownerID: () => signals == null ? participationOwner : signals.owner,
  authorizationHeader: () =>
      signals == null ? 'Bearer original' : signals.token,
  workspaceID: () => signals?.workspace,
  identityChanges: signals,
  domainCurrent: current,
  newOperationID: () => operationID,
  timeout: timeout ?? const Duration(seconds: 12),
);
http.Response receiptResponse({bool closure = false, bool committed = true}) =>
    http.Response(
      jsonEncode({
        'data': participationWire(closure: closure, committed: committed),
      }),
      200,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );

void main() {
  test(
    'cross-entry controller gives custom adapters the same canonical API base',
    () async {
      final store = ObservingParticipationStore();
      final client = MockClient((request) async {
        expect(
          request.url.toString(),
          'http://api.test/v1/activities/$participationActivity/participation-operations',
        );
        return receiptResponse();
      });
      final c = recoveryController(client, store, base: 'http://api.test/');
      await c.initialize();
      await c.submit(participationApproval());
      expect(c.pending, isNull);
      expect(store.environments, isNotEmpty);
      expect(store.environments.every((e) => e == 'http://api.test'), isTrue);
      c.dispose();
      client.close();
    },
  );
  for (final fromSlash in [true, false]) {
    test(
      'cross-entry trailing slash $fromSlash restores original key using GET only',
      () async {
        final store = MemoryParticipationPendingStore();
        final requests = <http.Request>[];
        final client = MockClient((request) async {
          requests.add(request);
          if (request.method == 'POST') {
            throw TimeoutException('committed result unknown');
          }
          return http.Response('{}', 404);
        });
        final firstBase = fromSlash ? 'http://api.test/' : 'http://api.test';
        final otherBase = fromSlash ? 'http://api.test' : 'http://api.test/';
        var c = recoveryController(client, store, base: firstBase);
        await c.initialize();
        await c.submit(participationApproval());
        expect(c.pending!.operationID, participationOp);
        c.dispose();
        c = recoveryController(
          client,
          store,
          base: otherBase,
          operationID: 'a1000000-0000-4000-8000-000000000088',
        );
        await c.initialize();
        await c.submit(participationApproval());
        expect(c.pending!.operationID, participationOp);
        expect(requests.where((r) => r.method == 'POST'), hasLength(1));
        expect(
          requests.where((r) => r.method == 'GET').single.url.toString(),
          'http://api.test/v1/activities/$participationActivity/participation-operations/$participationOp',
        );
        expect(
          requests.singleWhere((r) => r.method == 'POST').url.toString(),
          'http://api.test/v1/activities/$participationActivity/participation-operations',
        );
        expect(c.blocked, isTrue);
        c.dispose();
        client.close();
      },
    );
  }
  test(
    'only one POST follows durable binding; timeouts and all later restores only GET',
    () async {
      final store = MemoryParticipationPendingStore(),
          requests = <http.Request>[];
      final client = MockClient((r) async {
        requests.add(r);
        if (r.method == 'POST') {
          expect(
            (await store.read(
              'http://api.test',
              participationOwner,
            )).single.requestDigest,
            participationDigest,
          );
          expect(
            r.headers['X-Birdtie-Action-Until'],
            '2026-10-20T10:00:00.000000Z',
          );
          throw TimeoutException('response lost');
        }
        return http.Response('{}', 404);
      });
      var c = recoveryController(client, store);
      await c.initialize();
      await c.submit(participationApproval());
      await c.submit(participationApproval());
      expect(c.blocked, isTrue);
      expect(c.pending, isNotNull);
      c.dispose();
      c = recoveryController(client, store);
      await c.initialize();
      await c.verify();
      expect(requests.where((r) => r.method == 'POST'), hasLength(1));
      expect(requests.where((r) => r.method == 'GET'), hasLength(2));
      expect(c.pending, isNotNull);
      expect(c.message, contains('不能确定没有发生'));
      c.dispose();
    },
  );
  test('failed durable save prevents all execute requests', () async {
    var writes = 0;
    final c = recoveryController(
      MockClient((r) async {
        writes++;
        return receiptResponse();
      }),
      FailingParticipationStore(),
    );
    await c.initialize();
    await c.submit(participationApproval());
    expect(writes, 0);
    expect(c.storageBlocked, isTrue);
    c.dispose();
  });
  for (final status in [401, 403, 404, 409, 500, 503]) {
    test(
      'HTTP $status cannot turn unknown into no effect or erase journal',
      () async {
        final store = MemoryParticipationPendingStore();
        await store.write(
          'http://api.test',
          participationOwner,
          testParticipationPending,
        );
        final requests = <http.Request>[];
        final c = recoveryController(
          MockClient((r) async {
            requests.add(r);
            return http.Response('{}', status);
          }),
          store,
        );
        await c.initialize();
        await c.submit(participationApproval());
        expect(c.pending, isNotNull);
        expect(
          await store.read('http://api.test', participationOwner),
          hasLength(1),
        );
        expect(requests.every((r) => r.method == 'GET'), isTrue);
        c.dispose();
      },
    );
  }
  test(
    'COMMITTED old receipt resolves journal without supplying current mutable RSVP state',
    () async {
      final store = MemoryParticipationPendingStore();
      await store.write(
        'http://api.test',
        participationOwner,
        testParticipationPending,
      );
      final c = recoveryController(
        MockClient((r) async => receiptResponse()),
        store,
      );
      await c.initialize();
      expect(c.pending, isNull);
      expect(c.message, contains('查看当前报名状态'));
      expect(await store.read('http://api.test', participationOwner), isEmpty);
      c.dispose();
    },
  );
  test(
    'explicit close is separate one POST, lost close response reopens GET closure only',
    () async {
      final store = MemoryParticipationPendingStore(),
          requests = <http.Request>[];
      await store.write(
        'http://api.test',
        participationOwner,
        testParticipationPending,
      );
      var closed = false;
      final client = MockClient((r) async {
        requests.add(r);
        if (r.method == 'POST') {
          expect(r.url.path.endsWith('/close'), isTrue);
          expect(
            r.headers.keys.any(
              (k) => k.toLowerCase().startsWith('x-birdtie-action-'),
            ),
            isFalse,
          );
          expect(
            (await store.read(
              'http://api.test',
              participationOwner,
            )).single.stopRequested,
            isTrue,
          );
          expect(jsonDecode(r.body), {
            'operation': 'JOIN',
            'requestDigest': participationDigest,
          });
          closed = true;
          throw TimeoutException('close response lost');
        }
        return closed
            ? receiptResponse(closure: true)
            : http.Response('{}', 404);
      });
      var c = recoveryController(client, store);
      await c.initialize();
      final originalConfirmation = c.pending!;
      await c.stopAfterConfirmation(confirmedPending: originalConfirmation);
      expect(c.pending!.stopRequested, isTrue);
      await c.stopAfterConfirmation(confirmedPending: originalConfirmation);
      expect(
        requests.where((r) => r.method == 'POST'),
        hasLength(1),
        reason: 'the first dialog cannot confirm a newer pending object',
      );
      c.dispose();
      c = recoveryController(client, store);
      await c.initialize();
      expect(c.pending, isNull);
      expect(c.message, contains('原报名结果未核实'));
      expect(requests.where((r) => r.method == 'POST'), hasLength(1));
      expect(requests.last.url.path, endsWith('/closure'));
      c.dispose();
    },
  );
  test(
    'close that never reached API can be reconfirmed with the same key after GET-only restore',
    () async {
      final store = MemoryParticipationPendingStore(),
          requests = <http.Request>[];
      await store.write(
        'http://api.test',
        participationOwner,
        testParticipationPending,
      );
      var closes = 0;
      final client = MockClient((r) async {
        requests.add(r);
        if (r.method == 'POST') {
          expect(r.url.path, endsWith('/$participationOp/close'));
          closes++;
          if (closes == 1) {
            throw TimeoutException('request never reached server');
          }
          return receiptResponse(closure: true);
        }
        return http.Response('{}', 404);
      });
      var c = recoveryController(client, store);
      await c.initialize();
      await c.stopAfterConfirmation(confirmedPending: c.pending!);
      expect(c.pending!.stopRequested, isTrue);
      c.dispose();
      c = recoveryController(client, store);
      await c.initialize();
      expect(closes, 1);
      expect(c.pending, isNotNull);
      expect(requests.last.url.path, endsWith('/closure'));
      await c.stopAfterConfirmation(confirmedPending: c.pending!);
      expect(closes, 2);
      final posts = requests.where((r) => r.method == 'POST').toList();
      expect(posts[0].url, posts[1].url);
      expect(posts[0].body, posts[1].body);
      expect(posts.every((r) => r.url.path.endsWith('/close')), isTrue);
      expect(c.pending, isNull);
      expect(await store.read('http://api.test', participationOwner), isEmpty);
      c.dispose();
      client.close();
    },
  );
  for (final mutation in ['token', 'owner', 'workspace', 'domain', 'dispose']) {
    test(
      '$mutation ABA retires late successful receipt and keeps original journal',
      () async {
        final store = MemoryParticipationPendingStore(),
            signals = ParticipationSignals();
        final response = Completer<http.Response>();
        var sourceCurrent = true;
        final c = recoveryController(
          MockClient((r) => response.future),
          store,
          signals: signals,
          current: () => sourceCurrent,
        );
        await c.initialize();
        final sending = c.submit(participationApproval());
        for (var i = 0; i < 5 && c.pending == null; i++) {
          await Future<void>.delayed(Duration.zero);
        }
        expect(c.pending, isNotNull);
        if (mutation == 'domain') {
          sourceCurrent = false;
          expect(c.current, isFalse);
          sourceCurrent = true;
        } else if (mutation == 'dispose') {
          c.dispose();
        } else {
          signals.change(
            newOwner: mutation == 'owner'
                ? 'a1000000-0000-4000-8000-000000000099'
                : participationOwner,
            newToken: mutation == 'token' ? 'Bearer other' : 'Bearer original',
            newWorkspace: mutation == 'workspace' ? 'org' : null,
          );
          signals.change(
            newOwner: participationOwner,
            newToken: 'Bearer original',
          );
        }
        response.complete(receiptResponse());
        await sending;
        expect(
          await store.read('http://api.test', participationOwner),
          hasLength(1),
        );
        expect(c.current, isFalse);
        if (mutation != 'dispose') {
          c.dispose();
        }
      },
    );
  }
  test('bad wire binding cannot consume journal', () async {
    final store = MemoryParticipationPendingStore();
    await store.write(
      'http://api.test',
      participationOwner,
      testParticipationPending,
    );
    final raw = participationWire();
    raw['effectKey'] = '0' * 64;
    final c = recoveryController(
      MockClient((r) async => http.Response(jsonEncode({'data': raw}), 200)),
      store,
    );
    await c.initialize();
    expect(c.pending, isNotNull);
    expect(c.blocked, isTrue);
    c.dispose();
  });
}
