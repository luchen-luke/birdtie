import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/content/legacy_archive_api.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const archiveOwner = 'ad100000-0000-4000-8000-000000000001';
const archiveOtherOwner = 'ad100000-0000-4000-8000-000000000002';
const archiveOrigin = 'https://archive-isolated.test';
String archiveID(int n) =>
    'ad200000-0000-4000-8000-' + n.toString().padLeft(12, '0');
Map<String, Object?> archiveSummary(
  int n, {
  bool deleted = false,
  String? title,
}) => {
  'id': archiveID(n),
  'source': {
    'system': legacyArchiveSourceSystem,
    'kind': 'note',
    'id': archiveID(n + 100),
  },
  'sourceVersion': 1,
  'sourceSha256': 'a' * 64,
  'deleted': deleted,
  'title': deleted ? '' : title ?? '隔离旧记录 ' + n.toString(),
  'recordedStartDate': deleted ? null : '原始日期未规范化',
  'recordedEndDate': null,
  'sourceVisibility': deleted ? '' : 'private',
  'sourceModerationStatus': deleted ? '' : 'legacy-approved',
  'importedAt': '2026-10-15T01:02:03.123456789Z',
};
Map<String, Object?> archiveRelationships() => {
  'places': [
    {'sourcePlaceId': archiveID(201), 'rawName': '原地点乙', 'position': 8},
    {'sourcePlaceId': null, 'rawName': '原地点甲', 'position': 2},
  ],
  'media': [
    {'sourceMediaId': archiveID(301), 'position': 3, 'state': 'UNAVAILABLE'},
  ],
  'mediaLocations': [
    {
      'sourceMediaId': archiveID(301),
      'latitude': 999.125,
      'longitude': null,
      'mapEnabled': true,
      'sourcePlaceIds': [archiveID(201), archiveID(202)],
      'capturedDate': '原拍摄时间',
    },
  ],
  'thread': {
    'sourceThreadId': archiveID(401),
    'position': 4,
    'entryCount': 7,
    'routeRevision': 2,
    'createdAt': '原组日期',
  },
  'threadLegs': [
    {
      'sourceFromNoteId': archiveID(101),
      'sourceToNoteId': archiveID(102),
      'fromPosition': 4,
      'toPosition': 5,
      'mode': '步行',
      'sourceFromAnchorPlaceId': archiveID(201),
      'sourceToAnchorPlaceId': null,
      'createdAt': '原段创建日期',
      'updatedAt': '原段修改日期',
    },
  ],
};
Map<String, Object?> archiveDetail(
  int n, {
  bool deleted = false,
  String? title,
  String? body,
}) => {
  ...archiveSummary(n, deleted: deleted, title: title),
  'body': deleted
      ? ''
      : body ??
            '  原正文\n<script>literal()</script>\nhttps://old.invalid/private.jpg  ',
  if (!deleted) 'relationships': archiveRelationships(),
};
http.Response archiveResponse(Object? data, {int status = 200}) =>
    http.Response.bytes(
      utf8.encode(jsonEncode({'data': data})),
      status,
      headers: {
        'content-type': 'application/json; charset=utf-8',
        'cache-control': 'no-store',
      },
    );

class ArchiveBorrowedClient extends MockClient {
  ArchiveBorrowedClient(super.fn);
  bool wasClosed = false;
  @override
  void close() {
    wasClosed = true;
    super.close();
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'archive wire exact personal GET opaque cursor and kind preserves 20 plus tail',
    () async {
      final calls = <http.Request>[];
      final c = ArchiveBorrowedClient((r) async {
        calls.add(r);
        expectSync(r.method, 'GET');
        expectSync(r.bodyBytes, isEmpty);
        expectSync(r.headers['authorization'], 'Bearer isolated-A');
        expectSync(
          r.headers.keys.any((k) => k.toLowerCase().contains('workspace')),
          false,
        );
        if (!r.url.hasQuery)
          return archiveResponse({
            'items': [for (var i = 1; i <= 20; i++) archiveSummary(i)],
            'nextCursor': 'opaque_owner_source_last_id',
          });
        expectSync(r.url.queryParameters, {
          'cursor': 'opaque_owner_source_last_id',
          'kind': 'note',
        });
        return archiveResponse({
          'items': [archiveSummary(21)],
        });
      });
      final api = LegacyArchiveAPI(
        client: c,
        apiBaseUrl: archiveOrigin,
        authorizationHeader: () => 'Bearer isolated-A',
        ownerID: () => archiveOwner,
      );
      final first = await api.page(),
          tail = await api.page(cursor: first.nextCursor, kind: 'note');
      expect(first.items, hasLength(20));
      expect(tail.items.single.id, archiveID(21));
      expect(tail.nextCursor, isNull);
      api.dispose();
      api.dispose();
      expect(c.wasClosed, false);
      expect(calls, hasLength(2));
      c.close();
    },
  );
  test(
    'archive raw caption dates and original array order are not native Place or map permission',
    () async {
      final c = ArchiveBorrowedClient((r) async {
        expectSync(r.url.query, isEmpty);
        return archiveResponse(archiveDetail(1));
      });
      final api = LegacyArchiveAPI(
        client: c,
        apiBaseUrl: archiveOrigin,
        authorizationHeader: () => 'Bearer isolated',
        ownerID: () => archiveOwner,
      );
      final summary = LegacyArchiveSummary.decode(archiveSummary(1));
      final d = await api.detail(summary.id, expected: summary);
      expect(d.body, archiveDetail(1)['body']);
      expect(d.summary.recordedStartDate, '原始日期未规范化');
      expect(d.summary.importedAtRaw, '2026-10-15T01:02:03.123456789Z');
      expect(d.relationships!.places.map((p) => p.position), [8, 2]);
      expect(d.relationships!.mediaLocations.single.latitude, 999.125);
      expect(d.relationships!.mediaLocations.single.longitude, isNull);
      expect(d.relationships!.threadLegs.single.mode, '步行');
      api.dispose();
      c.close();
    },
  );
  test('archive tombstone never accepts old body relationships or date', () {
    final d = LegacyArchiveDetail.decode(archiveDetail(1, deleted: true));
    expect(d.body, isEmpty);
    expect(d.relationships, isNull);
    for (final bad in [
      {...archiveDetail(1, deleted: true), 'body': 'deleted secret'},
      {...archiveDetail(1, deleted: true), 'relationships': null},
      {...archiveSummary(1, deleted: true), 'recordedStartDate': 'old date'},
    ]) {
      expect(
        () => bad.containsKey('body')
            ? LegacyArchiveDetail.decode(bad)
            : LegacyArchiveSummary.decode(bad),
        throwsFormatException,
      );
    }
  });
  test(
    'archive closed selectors versions raw dates and media state reject malformed wire',
    () {
      for (final patch in <Map<String, Object?>>[
        {'unknown': 'raw URL'},
        {'sourceVersion': legacyArchiveMaxVersion + 1},
        {'sourceVersion': 1.5},
        {'id': archiveID(1).toUpperCase()},
        {'sourceSha256': 'A' * 64},
        {'recordedStartDate': 1700000000},
        {'title': '\u0000'},
        {'title': '\ud800'},
        {'importedAt': '2026-02-31T00:00:00Z'},
        {
          'source': {'system': 'foreign', 'kind': 'note', 'id': archiveID(101)},
        },
      ]) {
        expect(
          () => LegacyArchiveSummary.decode({...archiveSummary(1), ...patch}),
          throwsFormatException,
        );
      }
      expect(
        () => LegacyArchiveMedia.decode({
          'sourceMediaId': archiveID(1),
          'position': 0,
          'state': 'READY',
        }),
        throwsFormatException,
      );
      expect(
        () => LegacyArchivePageData.decode({'items': [], 'nextCursor': null}),
        throwsFormatException,
      );
      expect(
        () => LegacyArchivePageData.decode({
          'items': [archiveSummary(1), archiveSummary(1)],
        }),
        throwsFormatException,
      );
      expect(
        () =>
            LegacyArchivePageData.decode({'items': [], 'nextCursor': 'opaque'}),
        throwsFormatException,
      );
      expect(
        () => LegacyArchiveRelationships.decode({
          ...archiveRelationships(),
          'places': [
            {'sourcePlaceId': null, 'rawName': '', 'position': 0},
            {'sourcePlaceId': null, 'rawName': '', 'position': 0},
          ],
        }),
        throwsFormatException,
      );
    },
  );
  test(
    'archive duplicate escaped JSON keys and invalid UTF8 fail rather than discard',
    () async {
      expect(
        () => legacyArchiveJSON('{"data":1,"d\\u0061ta":2}'),
        throwsFormatException,
      );
      final c = ArchiveBorrowedClient(
        (_) async => http.Response.bytes(
          [0xff],
          200,
          headers: {'content-type': 'application/json'},
        ),
      );
      final api = LegacyArchiveAPI(
        client: c,
        apiBaseUrl: archiveOrigin,
        authorizationHeader: () => 'Bearer isolated',
        ownerID: () => archiveOwner,
      );
      await expectLater(api.page(), throwsFormatException);
      api.dispose();
      c.close();
    },
  );
  test(
    'archive foreign detail and mismatched original source are rejected without rebase',
    () async {
      for (final detail in [
        archiveDetail(2),
        {...archiveDetail(1), 'sourceVersion': 2},
        {...archiveDetail(1), 'sourceSha256': 'b' * 64},
        {...archiveDetail(1), 'title': '不符的来源标题'},
      ]) {
        final c = ArchiveBorrowedClient((_) async => archiveResponse(detail));
        final api = LegacyArchiveAPI(
          client: c,
          apiBaseUrl: archiveOrigin,
          authorizationHeader: () => 'Bearer isolated',
          ownerID: () => archiveOwner,
        );
        await expectLater(
          api.detail(
            archiveID(1),
            expected: LegacyArchiveSummary.decode(archiveSummary(1)),
          ),
          throwsFormatException,
        );
        api.dispose();
        c.close();
      }
    },
  );
  test(
    'archive permission and invalid selectors do not issue a writer or borrowed close',
    () async {
      var calls = 0;
      final c = ArchiveBorrowedClient((_) async {
        calls++;
        return archiveResponse(null, status: 403);
      });
      final api = LegacyArchiveAPI(
        client: c,
        apiBaseUrl: archiveOrigin,
        authorizationHeader: () => 'Bearer isolated',
        ownerID: () => archiveOwner,
      );
      await expectLater(
        api.page(),
        throwsA(
          isA<LegacyArchiveFailure>().having(
            (e) => e.permission,
            'permission',
            true,
          ),
        ),
      );
      await expectLater(api.page(kind: 'moment'), throwsFormatException);
      await expectLater(api.detail('../secret'), throwsFormatException);
      expect(calls, 1);
      api.dispose();
      expect(c.wasClosed, false);
      c.close();
    },
  );
  test(
    'archive same-owner renewal retires late GET and next read uses current header',
    () async {
      var header = 'Bearer synthetic-A';
      final changes = ValueNotifier(0), pending = Completer<http.Response>();
      final calls = <http.Request>[];
      final c = ArchiveBorrowedClient((r) async {
        calls.add(r);
        return calls.length == 1
            ? pending.future
            : archiveResponse({'items': []});
      });
      final api = LegacyArchiveAPI(
        client: c,
        apiBaseUrl: archiveOrigin,
        authorizationHeader: () => header,
        ownerID: () => archiveOwner,
        identityChanges: changes,
      );
      final old = api.page();
      final expectation = expectLater(
        old,
        throwsA(isA<LegacyArchiveSourceChanged>()),
      );
      await Future<void>.delayed(Duration.zero);
      header = 'Bearer synthetic-B';
      changes.value++;
      pending.complete(
        archiveResponse({
          'items': [archiveSummary(1)],
        }),
      );
      await expectation;
      expect((await api.page()).items, isEmpty);
      expect(calls.last.headers['authorization'], 'Bearer synthetic-B');
      api.dispose();
      changes.dispose();
      c.close();
    },
  );
  for (final axis in ['owner', 'workspace', 'source']) {
    test(
      'archive $axis ABA permanently retires API and original late detail',
      () async {
        var owner = archiveOwner;
        String? workspace;
        var source = 1;
        final changes = ValueNotifier(0), pending = Completer<http.Response>();
        var calls = 0;
        final c = ArchiveBorrowedClient((_) async {
          calls++;
          return pending.future;
        });
        final api = LegacyArchiveAPI(
          client: c,
          apiBaseUrl: archiveOrigin,
          authorizationHeader: () => 'Bearer isolated',
          ownerID: () => owner,
          organizationWorkspaceID: () => workspace,
          sourceKey: () => source,
          identityChanges: changes,
        );
        final old = api.detail(archiveID(1));
        final expectation = expectLater(
          old,
          throwsA(isA<LegacyArchiveSourceChanged>()),
        );
        await Future<void>.delayed(Duration.zero);
        if (axis == 'owner') owner = archiveOtherOwner;
        if (axis == 'workspace') workspace = archiveID(999);
        if (axis == 'source') source++;
        changes.value++;
        owner = archiveOwner;
        workspace = null;
        source = 1;
        changes.value++;
        pending.complete(archiveResponse(archiveDetail(1)));
        await expectation;
        await expectLater(
          api.page(),
          throwsA(isA<LegacyArchiveSourceChanged>()),
        );
        expect(calls, 1);
        api.dispose();
        changes.dispose();
        c.close();
      },
    );
  }
  test('archive same cursor response is explicit malformed page', () async {
    final c = ArchiveBorrowedClient(
      (_) async => archiveResponse({
        'items': [archiveSummary(1)],
        'nextCursor': 'opaque',
      }),
    );
    final api = LegacyArchiveAPI(
      client: c,
      apiBaseUrl: archiveOrigin,
      authorizationHeader: () => 'Bearer isolated',
      ownerID: () => archiveOwner,
    );
    await expectLater(api.page(cursor: 'opaque'), throwsFormatException);
    api.dispose();
    c.close();
  });
}
