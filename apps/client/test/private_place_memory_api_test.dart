import 'dart:convert';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/private_place_memory_api.dart';

const placeOwner = '27000000-0000-4000-8000-000000000001';
const placeAgent = '27000000-0000-4000-8000-000000000002';
const placeTarget = '27000000-0000-4000-8000-000000000003';
const placeMemory = '27000000-0000-4000-8000-000000000004';
Map<String, dynamic> placeReadFixture({
  List<Map<String, dynamic>>? signals,
  DateTime? now,
  String owner = placeOwner,
  String agent = placeAgent,
  String place = placeTarget,
}) {
  final n = (now ?? DateTime.now()).toUtc().subtract(
    const Duration(seconds: 1),
  );
  return {
    'schemaVersion': 'human-place-memory-v1',
    'ownerId': owner,
    'agentId': agent,
    'placeId': place,
    'cityId': 'local-city',
    'signals': signals ?? [],
    'observedAt': n.toIso8601String(),
    'expiresAt': n.add(const Duration(minutes: 1)).toIso8601String(),
    'verifiedVisit': 'UNAVAILABLE',
    'attendance': 'UNAVAILABLE',
    'modelAccess': 'UNAVAILABLE',
  };
}

Map<String, dynamic> placeSignalFixture({
  String kind = 'LIKED',
  DateTime? now,
  int revision = 1,
  String visibility = 'PRIVATE',
  DateTime? until,
}) {
  final n = (now ?? DateTime.now()).toUtc().subtract(
    const Duration(minutes: 1),
  );
  return {
    'kind': kind,
    'basis': kind == 'SAVED'
        ? 'CURRENT_NATIVE_BOOKMARK'
        : kind == 'CREATED_MOMENT_AT'
        ? 'CURRENT_NATIVE_MOMENT_LINK'
        : 'SELF_DECLARATION',
    'sourceKind': kind == 'SAVED'
        ? 'SAVED_PLACE'
        : kind == 'CREATED_MOMENT_AT'
        ? 'MOMENT'
        : 'EXPLICIT_PLACE_MEMORY',
    'sourceId': placeMemory,
    'revision': kind == 'SAVED' ? 0 : revision,
    'recordCreatedAt': n.toIso8601String(),
    'sourceUpdatedAt': n.toIso8601String(),
    'visibility': visibility,
    if (kind == 'LIKED' || kind == 'VISITED')
      'validUntil': (until ?? n.add(const Duration(days: 7)))
          .toUtc()
          .toIso8601String(),
  };
}

Map<String, dynamic> placeDeclarationFixture({
  String kind = 'LIKED',
  int version = 1,
  String status = 'ACTIVE',
  DateTime? until,
}) {
  final n = DateTime.now().toUtc().subtract(const Duration(seconds: 1));
  return {
    'memoryId': placeMemory,
    'version': version,
    'kind': kind,
    'basis': 'SELF_DECLARATION',
    'visibility': 'PRIVATE',
    'status': status,
    'validUntil': (until ?? n.add(const Duration(days: 7))).toIso8601String(),
    'updatedAt': n.toIso8601String(),
  };
}

Map<String, dynamic> placeControlsFixture({
  List<Map<String, dynamic>>? declarations,
  DateTime? now,
  String owner = placeOwner,
  String agent = placeAgent,
  String place = placeTarget,
}) {
  final n = (now ?? DateTime.now()).toUtc().subtract(
    const Duration(seconds: 1),
  );
  return {
    'schemaVersion': 'human-place-declaration-controls-v1',
    'ownerId': owner,
    'agentId': agent,
    'placeId': place,
    'declarations': declarations ?? [],
    'observedAt': n.toIso8601String(),
    'expiresAt': n.add(const Duration(minutes: 1)).toIso8601String(),
    'modelAccess': 'UNAVAILABLE',
  };
}

// Separate registered route fixtures: source observations are not control receipts.
MockClient placeFixtureClient(
  Future<http.Response> Function(http.Request) reply,
) => MockClient((r) async {
  final response = await reply(r);
  if (r.method != 'GET' ||
      !r.url.path.endsWith('/declarations') ||
      response.statusCode != 200) {
    return response;
  }
  final envelope = jsonDecode(response.body) as Map<String, dynamic>;
  final v = envelope['data'];
  if (v is! Map<String, dynamic> ||
      v['schemaVersion'] != 'human-place-memory-v1') {
    return response;
  }
  return placeResponse({
    'schemaVersion': 'human-place-declaration-controls-v1',
    'ownerId': v['ownerId'],
    'agentId': v['agentId'],
    'placeId': v['placeId'],
    'observedAt': v['observedAt'],
    'expiresAt': v['expiresAt'],
    'modelAccess': 'UNAVAILABLE',
    'declarations': [
      for (final s in v['signals'] as List)
        if (s['sourceKind'] == 'EXPLICIT_PLACE_MEMORY')
          {
            'memoryId': s['sourceId'],
            'version': s['revision'],
            'kind': s['kind'],
            'basis': 'SELF_DECLARATION',
            'visibility': s['visibility'],
            'status': 'ACTIVE',
            'validUntil': s['validUntil'],
            'updatedAt': s['sourceUpdatedAt'],
          },
    ],
  });
});

http.Response placeResponse(Object value) =>
    http.Response(jsonEncode({'data': value}), 200);
Map<String, dynamic> placeReceiptFixture(
  http.Request req, {
  String owner = placeOwner,
  String agent = placeAgent,
}) {
  final body = jsonDecode(req.body) as Map<String, dynamic>;
  return {
    'schemaVersion': 'human-place-declaration-receipt-v1',
    'ownerId': owner,
    'agentId': agent,
    'memoryId': req.url.pathSegments.last,
    'version': (body['expectedVersion'] as int) + 1,
    'status': req.method == 'DELETE' ? 'DELETED' : 'ACTIVE',
    if (req.method != 'DELETE') ...{
      'placeId': body['placeId'],
      'kind': body['kind'],
      'basis': 'SELF_DECLARATION',
      'visibility': body['visibility'],
      'validUntil': body['validUntil'],
    },
  };
}

class MemoryPlacePendingStore implements PlaceDeclarationPendingStore {
  final Map<String, PendingPlaceDeclaration> items = {};
  bool failWrite = false, failRead = false;
  String key(String e, String o, String p) => '$e|$o|$p';
  @override
  Future<PendingPlaceDeclaration?> read(String e, String o, String p) async {
    if (failRead) throw StateError('storage');
    return items[key(e, o, p)];
  }

  @override
  Future<void> write(
    String e,
    String o,
    String p,
    PendingPlaceDeclaration v,
  ) async {
    if (failWrite) throw StateError('storage');
    items[key(e, o, p)] = v;
  }

  @override
  Future<void> delete(String e, String o, String p) async {
    items.remove(key(e, o, p));
  }
}

void main() {
  test(
    'closed human sources never certify visits and preserve declaration revision',
    () {
      final n = DateTime.now();
      for (final kind in placeMemoryKindLabels.keys) {
        final p = PrivatePlaceMemory.read(
          placeReadFixture(
            now: n,
            signals: [placeSignalFixture(kind: kind, now: n)],
          ),
        );
        expect(p.signals.single.kind, kind);
        expect(
          p.signals.single.declaration,
          kind == 'LIKED' || kind == 'VISITED',
        );
      }
    },
  );
  test(
    'closed response rejects private bodies, coordinate, false attendance and duplicate sources',
    () {
      for (final key in [
        'body',
        'summary',
        'snapshotId',
        'authorityDigest',
        'targetDigest',
        'latitude',
        'purpose',
      ]) {
        final data = placeReadFixture()..[key] = 'private';
        expect(() => PrivatePlaceMemory.read(data), throwsFormatException);
      }
      for (final key in ['verifiedVisit', 'attendance', 'modelAccess']) {
        final data = placeReadFixture()..[key] = 'VERIFIED';
        expect(() => PrivatePlaceMemory.read(data), throwsFormatException);
      }
      final s = placeSignalFixture();
      expect(
        () => PrivatePlaceMemory.read(placeReadFixture(signals: [s, s])),
        throwsFormatException,
      );
    },
  );
  test(
    'UTC date parser rejects normalized invalid calendars and accepts leap day nanos',
    () {
      for (final value in [
        '2026-13-01T00:00:00Z',
        '2026-02-30T00:00:00Z',
        '2026-01-32T00:00:00Z',
        '2026-01-01T25:00:00Z',
        '2026-01-01T00:60:00Z',
        '2026-01-01T00:00:60Z',
        '0000-01-01T00:00:00Z',
        '2026-01-01T00:00:00',
        '2026-01-01T00:00:00Z\n',
      ]) {
        expect(
          () => placeMemoryStamp(value),
          throwsFormatException,
          reason: value,
        );
      }
      expect(
        placeMemoryStamp('2024-02-29T23:59:59.123456789Z'),
        DateTime.utc(2024, 2, 29, 23, 59, 59, 0, 123456),
      );
    },
  );
  test('invalid signals and unsupported kinds fail closed', () {
    for (final change in [
      {'kind': 'ATTENDED_ACTIVITY_AT'},
      {'basis': 'VERIFIED'},
      {'visibility': 'PUBLIC'},
      {'revision': 0},
      {'sourceId': 'not-id'},
      {'body': 'private'},
    ]) {
      final s = placeSignalFixture()..addAll(change);
      expect(
        () => PrivatePlaceMemory.read(placeReadFixture(signals: [s])),
        throwsFormatException,
      );
    }
    final rows = List.generate(101, (_) => placeSignalFixture());
    expect(
      () => PrivatePlaceMemory.read(placeReadFixture(signals: rows)),
      throwsFormatException,
    );
  });
  test(
    'typed API uses original stable ID exact CAS and no context/owner query',
    () async {
      final requests = <http.Request>[];
      final api = PrivatePlaceMemoryApi(
        apiBaseUrl: 'http://127.0.0.1:3700',
        client: MockClient((r) async {
          requests.add(r);
          return r.method == 'GET'
              ? placeResponse(placeReadFixture())
              : placeResponse(placeReceiptFixture(r));
        }),
      );
      await api.read(placeTarget, 'Bearer synthetic');
      final d = PlaceDeclarationDraft(
        kind: 'VISITED',
        visibility: 'AGENT_ONLY',
        validUntil: DateTime.now().toUtc().add(const Duration(days: 7)),
      );
      await api.put(
        placeMemory,
        placeTarget,
        placeAgent,
        0,
        d,
        'Bearer synthetic',
      );
      await api.delete(placeMemory, 1, 'Bearer synthetic');
      expect(requests.map((r) => r.method), ['GET', 'PUT', 'DELETE']);
      expect(
        requests.every(
          (r) =>
              r.url.query.isEmpty &&
              r.headers['Authorization'] == 'Bearer synthetic',
        ),
        isTrue,
      );
      expect((jsonDecode(requests[1].body) as Map).keys.toSet(), {
        'agentId',
        'expectedVersion',
        'placeId',
        'kind',
        'visibility',
        'validUntil',
      });
      expect(jsonDecode(requests[2].body), {'expectedVersion': 1});
      expect(requests[2].url.path.endsWith(placeMemory), isTrue);
    },
  );
  test('deleted receipt contains no erased source facts', () {
    final r = {
      'schemaVersion': 'human-place-declaration-receipt-v1',
      'ownerId': placeOwner,
      'agentId': placeAgent,
      'memoryId': placeMemory,
      'version': 2,
      'status': 'DELETED',
    };
    expect(PlaceDeclarationReceipt.read(r).placeID, isNull);
    expect(
      () => PlaceDeclarationReceipt.read({...r, 'kind': 'VISITED'}),
      throwsFormatException,
    );
  });
  test(
    'recovery note stores session fingerprint and original CAS not authority',
    () {
      final p = PendingPlaceDeclaration(
        memoryID: placeMemory,
        agentID: placeAgent,
        expectedVersion: 1,
        sessionFingerprint: placeSessionFingerprint('Bearer synthetic'),
        deleting: true,
      );
      expect(jsonEncode(p.wire()), isNot(contains('Bearer synthetic')));
      final got = PendingPlaceDeclaration.read(p.wire());
      expect(got.memoryID, placeMemory);
      expect(got.expectedVersion, 1);
      expect(
        () => PendingPlaceDeclaration.read({...p.wire(), 'confirmed': true}),
        throwsFormatException,
      );
      expect(validPlaceMemoryID(newPlaceMemoryID()), isTrue);
    },
  );

  test(
    'human controls preserve expired ID/CAS and reject public body or revived expiry',
    () {
      final expired = placeDeclarationFixture(
        status: 'EXPIRED',
        until: DateTime.now().toUtc().subtract(const Duration(days: 1)),
      );
      final c = PrivatePlaceDeclarationControls.read(
        placeControlsFixture(declarations: [expired]),
      );
      expect(c.declarations.single.memoryID, placeMemory);
      expect(c.declarations.single.version, 1);
      expect(c.declarations.single.status, 'EXPIRED');
      for (final key in ['summary', 'latitude', 'authorityStamp', 'cityId']) {
        expect(
          () => PrivatePlaceDeclarationControls.read({
            ...placeControlsFixture(),
            key: 'private',
          }),
          throwsFormatException,
        );
      }
      expect(
        () => PrivatePlaceDeclarationControls.read(
          placeControlsFixture(
            declarations: [placeDeclarationFixture(status: 'EXPIRED')],
          ),
        ),
        throwsFormatException,
      );
      expect(
        () => PrivatePlaceDeclarationControls.read(
          placeControlsFixture(declarations: [expired, expired]),
        ),
        throwsFormatException,
      );
      expect(
        () => PrivatePlaceDeclarationControls.read(
          placeControlsFixture(
            declarations: [
              {...expired, 'status': 'VERIFIED'},
            ],
          ),
        ),
        throwsFormatException,
      );
    },
  );
  test(
    'control transport has stable Place path and never supplies owner purpose',
    () async {
      late http.Request request;
      final api = PrivatePlaceMemoryApi(
        apiBaseUrl: 'http://127.0.0.1',
        client: MockClient((r) async {
          request = r;
          return placeResponse(placeControlsFixture());
        }),
      );
      await api.readControls(placeTarget, 'Bearer synthetic');
      expect(request.url.path, '/v1/me/places/$placeTarget/declarations');
      expect(request.url.hasQuery, isFalse);
      expect(request.body, isEmpty);
    },
  );

  test(
    'actual Go RFC3339 positive offset controls wire normalizes to same UTC instant',
    () {
      final raw = {
        'schemaVersion': 'human-place-declaration-controls-v1',
        'ownerId': '22e9cd18-babb-4359-9d1e-53593bedff47',
        'agentId': '177c9022-4081-4ab7-aa79-22662e0a895c',
        'placeId': 'b1700000-0000-4000-8000-000000000004',
        'declarations': [],
        'observedAt': '2026-10-04T04:40:55.442838+08:00',
        'expiresAt': '2026-10-04T04:42:55.442838+08:00',
        'modelAccess': 'UNAVAILABLE',
      };
      final control = PrivatePlaceDeclarationControls.read(raw);
      expect(
        control.observedAt,
        DateTime.utc(2026, 10, 3, 20, 40, 55, 442, 838),
      );
      expect(
        control.expiresAt,
        DateTime.utc(2026, 10, 3, 20, 42, 55, 442, 838),
      );
      expect(
        control.expiresAt.difference(control.observedAt),
        const Duration(minutes: 2),
      );
      expect(control.observedAt.isUtc, isTrue);
    },
  );
  test(
    'strict RFC3339 signed offsets including date boundaries preserve nanos and UTC',
    () {
      expect(
        placeMemoryStamp('2026-10-03T15:10:55.442838-05:30'),
        DateTime.utc(2026, 10, 3, 20, 40, 55, 442, 838),
      );
      expect(
        placeMemoryStamp('2024-03-01T00:01:00.123456789+00:30'),
        DateTime.utc(2024, 2, 29, 23, 31, 0, 0, 123456),
      );
      expect(
        placeMemoryStamp('2026-12-31T23:59:59-00:01'),
        DateTime.utc(2027, 1, 1, 0, 0, 59),
      );
      expect(
        placeMemoryStamp('2026-01-01T00:00:00+23:59'),
        DateTime.utc(2025, 12, 31, 0, 1),
      );
      expect(
        placeMemoryStamp('2026-01-01T00:00:00-23:59'),
        DateTime.utc(2026, 1, 1, 23, 59),
      );
      for (final zone in ['Z', '+00:00', '-00:00']) {
        expect(
          placeMemoryStamp('2026-01-01T00:00:00$zone'),
          DateTime.utc(2026),
        );
      }
    },
  );
  test(
    'invalid offsets and local or normalized bad calendars cannot gain UTC validity',
    () {
      for (final value in [
        '2026-10-04T04:40:55+24:00',
        '2026-10-04T04:40:55-24:00',
        '2026-10-04T04:40:55+08:60',
        '2026-10-04T04:40:55-00:60',
        '2026-10-04T04:40:55+8:00',
        '2026-10-04T04:40:55+0800',
        '2026-10-04T04:40:55+08',
        '2026-10-04T04:40:55+08:00\n',
        '2026-10-04T04:40:55+08:00:00',
        '2026-10-04T04:40:55+08:00Z',
        '2026-13-01T00:00:00+08:00',
        '2026-02-30T00:00:00-05:00',
        '2026-01-32T00:00:00+00:00',
        '2026-01-01T25:00:00+08:00',
        '2026-01-01T00:60:00-05:00',
        '2026-01-01T00:00:60+08:00',
        '0000-01-01T00:00:00+08:00',
        '0001-01-01T00:00:00+00:01',
        '9999-12-31T23:59:59-00:01',
        '2026-01-01T00:00:00',
      ]) {
        expect(
          () => placeMemoryStamp(value),
          throwsFormatException,
          reason: value,
        );
      }
    },
  );
}
