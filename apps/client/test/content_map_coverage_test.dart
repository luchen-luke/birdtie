import 'dart:math' as math;
import 'package:birdtie_client/src/workspace/map_content_coverage.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/owned_map_controller.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'content_map_controller_test.dart' as f;

class CoverageFixture extends f.Fixture {
  bool dense = false;
  @override
  Map<String, Object?> page(Uri uri) {
    final result = super.page(uri);
    final q = uri.queryParameters;
    final b = MapBounds(
      west: double.parse(q['west']!),
      east: double.parse(q['east']!),
      south: double.parse(q['south']!),
      north: double.parse(q['north']!),
    );
    final points = <Map<String, Object?>>[];
    for (var n = 1; n <= (dense ? 81 : 3); n++) {
      final lat = dense ? 57.18 + n * .0001 : 57.2;
      final lon = dense ? -2.11 + n * .0001 : -2.1 + (n - 1) * .32;
      if (!b.contains(lat, lon)) continue;
      final p = pin(n, b);
      p['anchor'] = {
        'coordinateSystem': 'wgs84',
        'precision': 'point',
        'latitude': lat,
        'longitude': lon,
      };
      points.add(p);
    }
    final start = int.tryParse(q['cursor'] ?? '') ?? 0;
    final end = math.min(points.length, start + 40);
    return {
      ...result,
      'pins': points.sublist(math.min(start, points.length), end),
      'totalContentCount': points.length,
      'totalAnchorCount': points.length,
      'totalPointCount': points.length,
      'truncated': end < points.length,
      'nextCursor': end < points.length ? '$end' : null,
      'viewportPath': Uri(
        path: '/v1/me/map-content',
        queryParameters: {
          for (final e in q.entries)
            if (e.key != 'cursor') e.key: e.value,
          'sourceVersion': source,
        },
      ).toString(),
    };
  }
}

void main() {
  test('coverage subtracts only uncovered strips, including dateline', () {
    const a = MapBounds(west: 0, south: 0, east: 10, north: 10);
    const b = MapBounds(west: 8, south: 2, east: 12, north: 8);
    expect(MapContentCoverage.missing(b, [a]), [
      const MapBounds(west: 10, south: 2, east: 12, north: 8),
    ]);
    expect(MapContentCoverage.missing(a, [a]), isEmpty);
    const crossing = MapBounds(west: 179, south: -1, east: -179, north: 1);
    expect(
      MapContentCoverage.missing(crossing, MapContentCoverage.split(crossing)),
      isEmpty,
    );
    expect(MapContentCoverage.padded(crossing).isValid, isTrue);
  });
  testWidgets(
    'pan and zoom reuse loaded area; only new strips fetched; return keeps source and no map clear',
    (t) async {
      final fixture = CoverageFixture();
      final identity = ValueNotifier(0);
      final c = OwnedMapController(
        client: fixture,
        apiBaseUrl: () => f.base,
        ownerID: () => fixture.owner,
        authorizationHeader: () => 'Bearer fixture',
        organizationWorkspaceID: () => fixture.organization,
        identityChanges: identity,
        refreshAllowed: () => true,
        now: () => fixture.now,
      );
      c.updateQuery('', f.bounds);
      await f.drain(t, () => c.current && !c.loading);
      expect(c.requests, 1);
      final pin = c.items.single;
      final epoch = c.epoch;
      final first = fixture.requests.first.url.queryParameters;
      final loaded = MapBounds(
        west: double.parse(first['west']!),
        east: double.parse(first['east']!),
        south: double.parse(first['south']!),
        north: double.parse(first['north']!),
      );
      c.updateQuery(
        '',
        const MapBounds(west: -2.25, south: 57.05, east: -1.95, north: 57.35),
        zoom: 14,
      );
      expect(c.requests, 1);
      expect(c.epoch, epoch);
      expect(c.sourceCurrent(pin), isTrue);
      c.updateQuery(
        '',
        const MapBounds(west: -2.1, south: 57, east: -1.7, north: 57.4),
      );
      expect(c.current, isTrue);
      expect(c.sourceCurrent(pin), isTrue);
      await f.drain(t, () => !c.loading);
      expect(c.requests, greaterThan(1));
      for (final request in fixture.requests.skip(1)) {
        final q = request.url.queryParameters;
        final w = double.parse(q['west']!),
            e = double.parse(q['east']!),
            s = double.parse(q['south']!),
            n = double.parse(q['north']!);
        expect(
          math.min(e, loaded.east) - math.max(w, loaded.west) <= 1e-9 ||
              math.min(n, loaded.north) - math.max(s, loaded.south) <= 1e-9,
          isTrue,
          reason: 'New reads must not repeat the previously covered interior.',
        );
      }
      final requests = c.requests;
      c.updateQuery('', f.bounds);
      expect(c.requests, requests);
      expect(c.current, isTrue);
      expect(c.sourceCurrent(pin), isTrue);
      fixture.status = 403;
      await t.runAsync(c.refresh);
      expect(c.current, isFalse);
      expect(c.cachedViews, 0);
      expect(c.sourceCurrent(pin), isFalse);
      c.dispose();
      identity.dispose();
      await t.pump();
    },
  );
  testWidgets(
    'failed renewal expires only old coverage and immediately refills the gap',
    (t) async {
      final fixture = CoverageFixture();
      final identity = ValueNotifier(0);
      final c = OwnedMapController(
        client: fixture,
        apiBaseUrl: () => f.base,
        ownerID: () => fixture.owner,
        authorizationHeader: () => 'Bearer fixture',
        organizationWorkspaceID: () => null,
        identityChanges: identity,
        refreshAllowed: () => true,
        now: () => fixture.now,
      );
      c.updateQuery('', f.bounds);
      await f.drain(t, () => c.current && !c.loading);
      final oldPin = c.items.single;
      final epoch = c.epoch;
      fixture.now = fixture.now.add(const Duration(seconds: 20));
      c.updateQuery(
        '',
        const MapBounds(west: -2.1, south: 57, east: -1.7, north: 57.4),
      );
      await f.drain(t, () => !c.loading);
      final freshPin = c.items.firstWhere((p) => p.mapID != oldPin.mapID);
      fixture.status = 503;
      fixture.now = fixture.now.add(const Duration(seconds: 6));
      await t.pump(const Duration(seconds: 5));
      await f.drain(t, () => !c.loading);
      expect(c.sourceCurrent(oldPin), isTrue);
      final reads = c.requests;
      fixture.status = 200;
      fixture.now = fixture.now.add(const Duration(seconds: 5));
      await t.pump(const Duration(seconds: 5));
      expect(c.sourceCurrent(freshPin), isTrue);
      expect(c.epoch, epoch);
      await f.drain(t, () => !c.loading);
      expect(c.requests, greaterThan(reads));
      expect(c.items.any((p) => p.mapID == oldPin.mapID), isTrue);
      expect(c.sourceCurrent(freshPin), isTrue);
      c.dispose();
      identity.dispose();
      await t.pump();
    },
  );
  testWidgets(
    '81 distinct positions stay bounded to 40 and all pages remain reachable',
    (t) async {
      final fixture = CoverageFixture()..dense = true;
      final identity = ValueNotifier(0);
      final c = OwnedMapController(
        client: fixture,
        apiBaseUrl: () => f.base,
        ownerID: () => fixture.owner,
        authorizationHeader: () => 'Bearer fixture',
        organizationWorkspaceID: () => null,
        identityChanges: identity,
        refreshAllowed: () => true,
        now: () => fixture.now,
      );
      c.updateQuery('', f.bounds, zoom: 8);
      await f.drain(t, () => c.current && !c.loading);
      final seen = c.items.map((p) => p.mapID).toSet();
      expect(c.items.length, 40);
      expect(c.items.every((p) => !p.isCluster), isTrue);
      expect(c.hasNextMapPage, isTrue);
      await t.runAsync(c.nextMapPage);
      seen.addAll(c.items.map((p) => p.mapID));
      expect(c.items.length, 40);
      expect(c.mapPageNumber, 2);
      await t.runAsync(c.nextMapPage);
      seen.addAll(c.items.map((p) => p.mapID));
      expect(c.items.length, 1);
      expect(seen.length, 81);
      expect(c.hasNextMapPage, isFalse);
      final requests = c.requests;
      c.previousMapPage();
      expect(c.items.length, 40);
      expect(c.requests, requests);
      final middle = c.items.map((p) => p.mapID).toList();
      fixture.now = fixture.now.add(const Duration(seconds: 10));
      await t.runAsync(c.refresh);
      expect(c.mapPageNumber, 2);
      expect(c.items.map((p) => p.mapID).toList(), middle);
      expect(c.hasNextMapPage, isTrue);
      c.dispose();
      identity.dispose();
      await t.pump();
    },
  );
}
