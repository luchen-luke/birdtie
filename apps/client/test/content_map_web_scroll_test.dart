import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:latlong2/latlong.dart';

void main() {
  testWidgets(
    'real FlutterMap wheel burst publishes newest zoom exactly once',
    (t) async {
      final controller = MapController();
      final snapshots = <MapCamera>[];
      final events = <MapEvent>[];
      final scheduler = WebMapViewportScheduler(
        onMotion: () {},
        onSettled: snapshots.add,
      );
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: FlutterMap(
              mapController: controller,
              options: MapOptions(
                initialCenter: const LatLng(57.145, -2.1),
                initialZoom: 12,
                onMapEvent: (event) {
                  events.add(event);
                  scheduler.handle(event);
                },
              ),
              children: const [],
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      snapshots.clear();
      final center = t.getCenter(find.byType(FlutterMap));
      for (var n = 0; n < 3; n++) {
        await t.sendEventToBinding(
          PointerScrollEvent(
            position: center,
            scrollDelta: const Offset(0, -50),
          ),
        );
        await t.pump(const Duration(milliseconds: 80));
      }
      expect(events.whereType<MapEventScrollWheelZoom>(), hasLength(3));
      expect(snapshots, isEmpty);
      final expectedZoom = controller.camera.zoom;
      expect(expectedZoom, greaterThan(12));
      await t.pump(const Duration(milliseconds: 251));
      expect(snapshots, hasLength(1));
      expect(snapshots.single.zoom, expectedZoom);
      expect(snapshots.single.visibleBounds, controller.camera.visibleBounds);
      await t.pump(const Duration(seconds: 1));
      expect(snapshots, hasLength(1));
      scheduler.dispose();
      await t.pumpWidget(const SizedBox.shrink());
      controller.dispose();
    },
  );

  testWidgets(
    'real double tap ends once and rotation ends coalesce with move end',
    (t) async {
      final controller = MapController();
      final snapshots = <MapCamera>[];
      final events = <MapEvent>[];
      final scheduler = WebMapViewportScheduler(
        onMotion: () {},
        onSettled: snapshots.add,
      );
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: FlutterMap(
              mapController: controller,
              options: MapOptions(
                initialCenter: const LatLng(57.145, -2.1),
                initialZoom: 12,
                onMapEvent: (event) {
                  events.add(event);
                  scheduler.handle(event);
                },
              ),
              children: const [],
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      snapshots.clear();
      final center = t.getCenter(find.byType(FlutterMap));
      await t.tapAt(center);
      await t.pump(const Duration(milliseconds: 50));
      await t.tapAt(center);
      await t.pumpAndSettle();
      await t.pump(const Duration(milliseconds: 1));
      expect(events.whereType<MapEventDoubleTapZoomEnd>(), hasLength(1));
      expect(snapshots, hasLength(1));
      expect(snapshots.single.zoom, 13);
      snapshots.clear();
      final rotated = controller.camera.withRotation(35);
      scheduler.handle(
        MapEventRotate(
          source: MapEventSource.onMultiFinger,
          oldCamera: controller.camera,
          camera: rotated,
          id: null,
        ),
      );
      scheduler.handle(
        MapEventMoveEnd(source: MapEventSource.multiFingerEnd, camera: rotated),
      );
      scheduler.handle(
        MapEventRotateEnd(
          source: MapEventSource.multiFingerEnd,
          camera: rotated,
        ),
      );
      await t.pump(const Duration(milliseconds: 1));
      expect(snapshots, hasLength(1));
      expect(snapshots.single.rotation, 35);
      expect(snapshots.single.zoom, 13);
      scheduler.dispose();
      await t.pumpWidget(const SizedBox.shrink());
      controller.dispose();
    },
  );

  testWidgets(
    'new gesture, scope change and dispose retire pending wheel snapshots',
    (t) async {
      final controller = MapController();
      final snapshots = <MapCamera>[];
      final scheduler = WebMapViewportScheduler(
        onMotion: () {},
        onSettled: snapshots.add,
      );
      await t.pumpWidget(
        MaterialApp(
          home: FlutterMap(
            mapController: controller,
            options: const MapOptions(
              initialCenter: LatLng(57.145, -2.1),
              initialZoom: 12,
            ),
            children: const [],
          ),
        ),
      );
      await t.pumpAndSettle();
      final camera = controller.camera;
      void wheel() => scheduler.handle(
        MapEventScrollWheelZoom(
          source: MapEventSource.scrollWheel,
          oldCamera: camera,
          camera: camera.withPosition(zoom: 14),
        ),
      );
      wheel();
      await t.pump(const Duration(milliseconds: 200));
      scheduler.handle(
        MapEventMoveStart(source: MapEventSource.dragStart, camera: camera),
      );
      await t.pump(const Duration(milliseconds: 100));
      expect(snapshots, isEmpty);
      scheduler.handle(
        MapEventMoveEnd(
          source: MapEventSource.dragEnd,
          camera: camera.withPosition(zoom: 15),
        ),
      );
      scheduler.handle(
        MapEventFlingAnimationStart(
          source: MapEventSource.flingAnimationController,
          camera: camera,
        ),
      );
      await t.pump(const Duration(milliseconds: 1));
      expect(snapshots, isEmpty);
      scheduler.handle(
        MapEventFlingAnimationEnd(
          source: MapEventSource.flingAnimationController,
          camera: camera.withPosition(zoom: 16),
        ),
      );
      await t.pump(const Duration(milliseconds: 1));
      expect(snapshots.single.zoom, 16);
      snapshots.clear();
      wheel();
      scheduler.cancel();
      await t.pump(const Duration(milliseconds: 300));
      expect(snapshots, isEmpty);
      wheel();
      scheduler.dispose();
      await t.pump(const Duration(milliseconds: 300));
      expect(snapshots, isEmpty);
      await t.pumpWidget(const SizedBox.shrink());
      controller.dispose();
    },
  );
}
