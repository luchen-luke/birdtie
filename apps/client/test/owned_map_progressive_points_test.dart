import 'dart:async';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/owned_map_controller.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'content_map_controller_test.dart' as isolated;

// Synthetic transport only. Never connected to any real Birdtie environment.
class PagedFixture extends isolated.Fixture {
  int total = 120;
  bool duplicateSecond = false;
  int cursorRevision = 0;
  bool paddingPoint = false;
  bool shortMiddleLease = false;
  bool emptyContinuation = false,
      repeatCursor = false,
      changedContinuation = false;
  int? failPage;
  @override
  Map<String, Object?> page(Uri uri) {
    final result = super.page(uri);
    final q = uri.queryParameters;
    final page = int.parse(q['cursor']?.split('-')[1] ?? '0');
    final b = MapBounds(
      west: double.parse(q['west']!),
      south: double.parse(q['south']!),
      east: double.parse(q['east']!),
      north: double.parse(q['north']!),
    );
    final start = duplicateSecond && page == 1 ? 0 : page * 40;
    final pins = [
      for (var n = start + 1; n <= total && n <= start + 40; n++) pin(n, b),
    ];
    if (paddingPoint && start == 0 && pins.isNotEmpty) {
      pins.first['anchor'] = {
        'coordinateSystem': 'wgs84',
        'precision': 'point',
        'latitude': b.north - .000001,
        'longitude': (b.west + b.east) / 2,
      };
    }
    final more = start + 40 < total;
    final pageSource = changedContinuation && page > 0
        ? isolated.hash('changed-source')
        : source;
    return {
      ...result,
      if (shortMiddleLease && page == 1)
        'validUntil': now.add(const Duration(seconds: 5)).toIso8601String(),
      'pins': emptyContinuation && page > 0 ? [] : pins,
      'sourceVersion': pageSource,
      'totalContentCount': total,
      'totalAnchorCount': total,
      'totalPointCount': total,
      'truncated': more,
      'nextCursor': more
          ? 'page-${repeatCursor && page > 0 ? page : page + 1}-$cursorRevision'
          : null,
      'viewportPath': Uri(
        path: '/v1/me/map-content',
        queryParameters: {
          ...Map<String, String>.of(q)..remove('cursor'),
          'sourceVersion': pageSource,
        },
      ).toString(),
    };
  }

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) {
    if (request.url.queryParameters['cursor']?.startsWith('page-$failPage-') ==
        true) {
      requests.add(request);
      return Future.value(
        http.StreamedResponse(
          Stream.value([123, 125]),
          503,
          headers: {'content-type': 'application/json'},
        ),
      );
    }
    return super.send(request);
  }
}

class Scenario {
  Scenario({
    int total = 120,
    int batch = 4,
    int budget = 2048,
    int cachedViews = 1,
  }) {
    f.total = total;
    c = OwnedMapController(
      client: f,
      apiBaseUrl: () => isolated.base,
      ownerID: () => f.owner,
      authorizationHeader: () => 'Bearer fixture',
      organizationWorkspaceID: () => f.organization,
      identityChanges: identity,
      refreshAllowed: () => true,
      now: () => f.now,
      progressiveBatchPages: batch,
      maxCachedViews: cachedViews,
      maxCachedPins: 40 * cachedViews,
      maxActivePins: budget,
    );
  }
  final f = PagedFixture();
  final identity = ValueNotifier(0);
  late final OwnedMapController c;
  Future<void> settle() async {
    for (var n = 0; n < 200 && c.loading; n++) {
      await Future<void>.delayed(Duration.zero);
    }
    expect(c.loading, false);
  }

  Future<void> open() async {
    c.observeViewport(
      cityID: 'fixture-city',
      bounds: isolated.bounds,
      zoom: 12,
      navigationGeneration: 1,
    );
    await settle();
    expect(c.current, true, reason: c.error);
  }

  void close() {
    c.dispose();
    identity.dispose();
    f.close();
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'independent coincident points accumulate beyond page40 and view LRU',
    () async {
      final s = Scenario();
      addTearDown(s.close);
      await s.open();
      expect(s.f.requests.length, 3);
      expect(s.c.items.length, 120);
      expect(s.c.entities.map((e) => e.id).toSet().length, 120);
      expect(s.c.items.every((p) => !p.isCluster), true);
      expect(s.c.items.map((p) => (p.latitude, p.longitude)).toSet().length, 1);
      expect(s.c.cachedViews, 1);
      expect(s.c.cachedPins, 40);
      expect(s.c.hasNextMapPage, false);
      expect(s.c.mapContentIncomplete, false);
    },
  );
  test(
    'four-page batch stops visibly, continuation adds without replacing',
    () async {
      final s = Scenario(total: 200);
      addTearDown(s.close);
      await s.open();
      final first = s.c.items.first;
      expect(s.c.items.length, 160);
      expect(s.f.requests.length, 4);
      expect(s.c.mapContentIncomplete, true);
      expect(s.c.hasNextMapPage, true);
      await s.c.nextMapPage();
      expect(s.c.items.length, 200);
      expect(s.f.requests.length, 5);
      expect(s.c.sourceCurrent(first), true);
      expect(s.c.find(first.mapID), same(first));
      expect(s.c.mapContentIncomplete, false);
      await s.c.nextMapPage();
      expect(s.f.requests.length, 5);
    },
  );
  test(
    'failed continuation preserves loaded grants and explicit retry resumes',
    () async {
      final s = Scenario();
      addTearDown(s.close);
      s.f.failPage = 1;
      await s.open();
      final first = s.c.items.first;
      expect(s.c.items.length, 40);
      expect(s.c.mapContentIncomplete, true);
      expect(s.c.error, isNotNull);
      expect(s.c.sourceCurrent(first), true);
      s.f.failPage = null;
      await s.c.nextMapPage();
      expect(s.c.items.length, 120);
      expect(s.c.error, isNull);
      expect(s.c.mapContentIncomplete, false);
    },
  );
  test(
    'empty continuation with next cursor stops without erasing earlier pages',
    () async {
      final s = Scenario();
      addTearDown(s.close);
      s.f.emptyContinuation = true;
      await s.open();
      expect(s.c.items.length, 40);
      expect(s.f.requests.length, 2);
      expect(s.c.error, isNotNull);
      expect(s.c.mapContentIncomplete, true);
      s.f.emptyContinuation = false;
      await s.c.nextMapPage();
      expect(s.c.items.length, 120);
      expect(s.c.mapContentIncomplete, false);
    },
  );
  test('duplicate cursor never loops or claims full coverage', () async {
    final s = Scenario(total: 160);
    addTearDown(s.close);
    s.f.repeatCursor = true;
    await s.open();
    expect(s.c.items.length, 80);
    expect(s.f.requests.length, 2);
    expect(s.c.error, isNotNull);
    expect(s.c.mapContentIncomplete, true);
    await s.c.nextMapPage();
    expect(s.f.requests.length, 2);
    s.f.repeatCursor = false;
    await s.c.refresh();
    expect(s.c.items.length, 160);
    expect(s.c.mapContentIncomplete, false);
  });
  test(
    'changed source on continuation cannot append a mixed snapshot',
    () async {
      final s = Scenario();
      addTearDown(s.close);
      s.f.changedContinuation = true;
      await s.open();
      expect(s.c.items.length, 40);
      expect(s.c.error, isNotNull);
      expect(s.c.mapContentIncomplete, true);
      expect(s.f.requests.length, 2);
    },
  );
  test(
    'finite active budget stops additional pages without silently sampling loaded points',
    () async {
      final s = Scenario(total: 200, budget: 80);
      addTearDown(s.close);
      await s.open();
      expect(s.c.items.length, 80);
      expect(s.f.requests.length, 3);
      expect(s.c.mapContentIncomplete, true);
      expect(s.c.hasNextMapPage, false);
      expect(s.c.error, contains('缩小范围'));
      await s.c.nextMapPage();
      expect(s.f.requests.length, 3);
    },
  );
  test(
    'camera-only pans and zooms do not change committed content search',
    () async {
      final s = Scenario(total: 40);
      addTearDown(s.close);
      await s.open();
      final initial = s.c.items.singleWhere(
        (p) => p.momentID == isolated.id(1),
      );
      final calls = s.f.requests.length;
      const moved = MapBounds(west: -3, south: 56, east: -2.4, north: 56.4);
      s.c.observeViewport(
        cityID: 'fixture-city',
        bounds: moved,
        zoom: 8,
        navigationGeneration: 1,
      );
      await s.settle();
      expect(s.f.requests.length, calls);
      expect(s.c.items.length, 40);
      expect(s.c.sourceCurrent(initial), true);
      s.c.sync(cityID: 'fixture-city', bounds: moved, zoom: 8);
      await s.settle();
      expect(s.f.requests.length, greaterThan(calls));
      expect(s.c.view!.zoom, 8);
    },
  );
  test(
    'identity retirement cancels late continuation and all accumulated authority',
    () async {
      final s = Scenario(total: 80, batch: 1);
      addTearDown(s.close);
      await s.open();
      final first = s.c.items.first;
      s.f.delay = Completer<void>();
      final wait = s.f.delay!;
      final reading = s.c.nextMapPage();
      await Future<void>.delayed(Duration.zero);
      s.f.organization = isolated.id(7000);
      s.identity.value++;
      wait.complete();
      await reading;
      expect(s.c.items, isEmpty);
      expect(s.c.sourceCurrent(first), false);
    },
  );
  test(
    'new authoritative complete snapshot withdraws old IDs; original leases still expire',
    () async {
      final s = Scenario();
      addTearDown(s.close);
      await s.open();
      final removed = s.c.items.last;
      s.f.total = 40;
      s.f.source = isolated.hash('new-complete');
      s.f.now = s.f.now.add(const Duration(seconds: 1));
      await s.c.refresh();
      expect(s.c.items.length, 40);
      expect(s.c.sourceCurrent(removed), false);
      final kept = s.c.items.first;
      s.f.now = s.f.now.add(const Duration(seconds: 31));
      expect(s.c.entities, isEmpty);
      expect(s.c.sourceCurrent(kept), false);
    },
  );
  test(
    'duplicate page entities cannot pretend the point set is complete',
    () async {
      final s = Scenario();
      addTearDown(s.close);
      s.f.duplicateSecond = true;
      await s.open();
      expect(s.c.items.length, 80);
      expect(s.f.requests.length, 3);
      expect(s.c.mapContentIncomplete, true);
      expect(s.c.error, isNotNull);
    },
  );
  test(
    'complete multi-page new source retires removed IDs from older pages',
    () async {
      final s = Scenario(total: 160);
      addTearDown(s.close);
      await s.open();
      final removed = s.c.items.last;
      s.f.total = 80;
      s.f.source = isolated.hash('new-multi-page');
      s.f.now = s.f.now.add(const Duration(seconds: 1));
      await s.c.refresh();
      expect(s.c.items.length, 80);
      expect(s.c.sourceCurrent(removed), false);
      expect(s.c.mapContentIncomplete, false);
    },
  );
  test('expired middle page is not covered by live root and tail', () async {
    final s = Scenario(batch: 1);
    addTearDown(s.close);
    s.f.shortMiddleLease = true;
    await s.open();
    await s.c.nextMapPage();
    await s.c.nextMapPage();
    expect(s.c.items.length, 120);
    s.f.now = s.f.now.add(const Duration(seconds: 6));
    s.c.sync(cityID: 'fixture-city', bounds: isolated.bounds, zoom: 12);
    expect(s.c.items.length, 80);
    expect(s.c.mapContentIncomplete, true);
    expect(s.c.hasNextMapPage, true);
    await s.c.nextMapPage();
    expect(s.f.requests.last.url.queryParameters['cursor'], 'page-1-0');
    expect(s.c.items.length, 120);
    expect(s.c.mapContentIncomplete, false);
  });

  test(
    'same-source renewed cursor chain keeps all loaded IDs without extending old pixel leases',
    () async {
      final s = Scenario();
      addTearDown(s.close);
      await s.open();
      final oldEntity = s.c.entities.first;
      s.f.cursorRevision = 1;
      s.f.now = s.f.now.add(const Duration(seconds: 1));
      await s.c.refresh();
      expect(s.c.items.length, 120);
      expect(s.c.mapContentIncomplete, false);
      expect(s.f.requests.last.url.queryParameters['cursor'], 'page-2-1');
      s.f.now = s.f.now.add(const Duration(seconds: 30));
      expect(oldEntity.readGrant!.authorizes(oldEntity, s.f.now), false);
    },
  );

  test(
    'new root owns continuation despite an older source tail in the LRU',
    () async {
      final s = Scenario(total: 560, cachedViews: 8);
      addTearDown(s.close);
      await s.open();
      await s.c.nextMapPage();
      await s.c.nextMapPage();
      expect(s.c.items.length, 480);
      s.f.total = 520;
      s.f.source = isolated.hash('latest-root-snapshot');
      s.f.now = s.f.now.add(const Duration(seconds: 1));
      await s.c.refresh();
      expect(s.c.items.length, 480);
      final before = s.f.requests.length;
      await s.c.nextMapPage();
      expect(
        s.f.requests[before].url.queryParameters['sourceVersion'],
        s.f.source,
      );
      expect(s.f.requests[before].url.queryParameters['cursor'], 'page-4-0');
      expect(s.c.error, isNull);
      await s.c.nextMapPage();
      await s.c.nextMapPage();
      expect(s.c.items.length, 520);
      expect(s.c.mapContentIncomplete, false);
    },
  );

  test(
    'already authorized padded points stay in the base set during passive pan',
    () async {
      final s = Scenario(total: 40);
      addTearDown(s.close);
      s.f.paddingPoint = true;
      await s.open();
      final padded = s.c.items.singleWhere((p) => p.momentID == isolated.id(1));
      expect(
        isolated.bounds.contains(padded.latitude, padded.longitude),
        false,
      );
      expect(s.c.items.length, 40);
      final requests = s.f.requests.length;
      s.c.observeViewport(
        cityID: 'fixture-city',
        bounds: const MapBounds(
          west: -2.3,
          south: 57.3,
          east: -1.9,
          north: 57.6,
        ),
        zoom: 13,
        navigationGeneration: 1,
      );
      await s.settle();
      expect(s.f.requests.length, requests);
      expect(s.c.entities.map((e) => e.id), contains(padded.mapID));
    },
  );
}
