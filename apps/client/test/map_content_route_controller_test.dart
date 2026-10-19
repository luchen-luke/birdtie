import 'dart:async';
import 'package:flutter_test/flutter_test.dart';
import 'package:birdtie_client/src/workspace/map_content_route_controller.dart';
import 'package:birdtie_client/src/workspace/map_content_route_geometry.dart';
import 'map_content_route_geometry_test.dart' show routeQuery, routeGeometry;

void main() {
  testWidgets(
    'singleflight cancels real route work on source change and ignores identity ABA late response',
    (t) async {
      var now = DateTime.utc(2026, 10, 19), live = true;
      var calls = 0, aborts = 0;
      final held = <Completer<MapContentRouteGeometry>>[];
      final c = MapContentRouteController(
        now: () => now,
        load: (q, current, abort) {
          calls++;
          abort.then((_) => aborts++);
          final v = Completer<MapContentRouteGeometry>();
          held.add(v);
          return v.future;
        },
      );
      final q = routeQuery(now);
      final candidate = MapContentRouteCandidate(q, () => live);
      c.sync([candidate]);
      c.sync([candidate]);
      expect(calls, 1);
      live = false;
      c.sync([]);
      await t.pump();
      expect(aborts, 1);
      expect(c.geometry(q), isNull);
      live = true;
      c.sync([candidate]);
      expect(calls, 2);
      held[0].complete(routeGeometry(q, now));
      await t.pump();
      expect(c.geometry(q), isNull);
      held[1].complete(routeGeometry(q, now));
      await t.pump();
      expect(c.geometry(q), isNotNull);
      c.retire();
      await t.pump();
      expect(c.geometry(q), isNull);
      c.dispose();
    },
  );
  testWidgets(
    'route original deadline cannot extend from later provider lease and negative results never draw',
    (t) async {
      var now = DateTime.utc(2026, 10, 19), live = true;
      var calls = 0;
      final q = routeQuery(now, seconds: 5);
      final c = MapContentRouteController(
        now: () => now,
        load: (q, current, abort) async {
          calls++;
          return routeGeometry(q, now);
        },
      );
      c.sync([MapContentRouteCandidate(q, () => live)]);
      await t.pump();
      final old = c.geometry(q)!;
      expect(old.validUntil, q.validUntil);
      now = now.add(const Duration(seconds: 5));
      await t.pump(const Duration(seconds: 5));
      expect(old.availableAt(now), isFalse);
      expect(c.geometry(q), isNull);
      live = false;
      c.dispose();
      expect(calls, 1);
      final q2 = routeQuery(now);
      var negative = 0;
      final blocked = MapContentRouteController(
        now: () => now,
        load: (q, current, abort) async {
          negative++;
          return routeGeometry(q, now, status: 'NOT_AVAILABLE');
        },
      );
      blocked.sync([MapContentRouteCandidate(q2, () => true)]);
      await t.pump();
      blocked.sync([MapContentRouteCandidate(q2, () => true)]);
      expect(negative, 1);
      expect(blocked.geometry(q2), isNull);
      blocked.dispose();
    },
  );
  testWidgets(
    'two concurrent operations and 22 second absolute cancellation are bounded',
    (t) async {
      var now = DateTime.utc(2026, 10, 19);
      var calls = 0, aborts = 0;
      final c = MapContentRouteController(
        now: () => now,
        load: (q, current, abort) {
          calls++;
          abort.then((_) => aborts++);
          return Completer<MapContentRouteGeometry>().future;
        },
      );
      c.sync([
        for (var i = 1; i <= 30; i++)
          MapContentRouteCandidate(routeQuery(now, n: i), () => true),
      ]);
      expect(calls, 2);
      now = now.add(const Duration(seconds: 22));
      await t.pump(const Duration(seconds: 22));
      expect(aborts, 2);
      expect(calls, 4);
      c.dispose();
      await t.pump();
      expect(aborts, 4);
      await t.pump(const Duration(seconds: 22));
    },
  );
}
