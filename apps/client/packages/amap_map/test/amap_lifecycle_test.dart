import 'dart:async';
import 'dart:typed_data';

import 'package:amap_map/amap_map.dart';
import 'package:amap_map/src/core/amap_flutter_platform.dart';
import 'package:amap_map/src/core/map_event.dart';
import 'package:amap_map/src/core/method_channel_amap_map.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:plugin_platform_interface/plugin_platform_interface.dart';

/// Replaces only platform-view construction; init, native-message dispatch,
/// controller subscriptions and the real AMapWidget lifecycle stay in use.
class _TrackedPlatform extends MethodChannelAMapFlutterMap
    with MockPlatformInterfaceMixin {
  late void Function(int) created;
  int listened = 0, cancelled = 0;

  @override
  Widget buildView(
    Map<String, dynamic> creationParams,
    Set<Factory<OneSequenceGestureRecognizer>> gestureRecognizers,
    void Function(int) onPlatformViewCreated,
  ) {
    created = onPlatformViewCreated;
    return const SizedBox.expand();
  }

  Stream<T> _track<T>(Stream<T> source) => Stream<T>.multi((output) {
        listened++;
        final subscription = source.listen(
          output.add,
          onError: output.addError,
          onDone: output.close,
        );
        output.onCancel = () {
          cancelled++;
          unawaited(subscription.cancel());
        };
      }, isBroadcast: true);

  @override
  Stream<LocationChangedEvent> onLocationChanged({required int mapId}) =>
      _track(super.onLocationChanged(mapId: mapId));
  @override
  Stream<CameraMoveEvent> onCameraMove({required int mapId}) =>
      _track(super.onCameraMove(mapId: mapId));
  @override
  Stream<CameraMoveEndEvent> onCameraMoveEnd({required int mapId}) =>
      _track(super.onCameraMoveEnd(mapId: mapId));
  @override
  Stream<MapTapEvent> onMapTap({required int mapId}) =>
      _track(super.onMapTap(mapId: mapId));
  @override
  Stream<MapLongPressEvent> onMapLongPress({required int mapId}) =>
      _track(super.onMapLongPress(mapId: mapId));
  @override
  Stream<MapPoiTouchEvent> onPoiTouched({required int mapId}) =>
      _track(super.onPoiTouched(mapId: mapId));
  @override
  Stream<MarkerTapEvent> onMarkerTap({required int mapId}) =>
      _track(super.onMarkerTap(mapId: mapId));
  @override
  Stream<MarkerDragEndEvent> onMarkerDragEnd({required int mapId}) =>
      _track(super.onMarkerDragEnd(mapId: mapId));
  @override
  Stream<PolylineTapEvent> onPolylineTap({required int mapId}) =>
      _track(super.onPolylineTap(mapId: mapId));
}

void main() {
  testWidgets(
    'AMap disposal cancels all streams and native handlers without late revival',
    (tester) async {
      final platform = _TrackedPlatform();
      AMapFlutterPlatform.instance = platform;
      final messenger =
          TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
      const readyChannel = MethodChannel('amap_map_41');
      const lateChannel = MethodChannel('amap_map_42');
      final pending = Completer<void>();
      messenger.setMockMethodCallHandler(readyChannel, (_) async => null);
      messenger.setMockMethodCallHandler(lateChannel, (call) async {
        if (call.method == 'map#waitForMap') await pending.future;
        return null;
      });
      addTearDown(() {
        messenger.setMockMethodCallHandler(readyChannel, null);
        messenger.setMockMethodCallHandler(lateChannel, null);
        platform.dispose(id: 41);
        platform.dispose(id: 42);
      });
      Future<ByteData?> nativeTap(int id) {
        final reply = Completer<ByteData?>();
        // Send through the actual registered incoming MethodChannel handler.
        // A null reply means no handler remains, not an empty success envelope.
        // ignore: deprecated_member_use
        unawaited(messenger.handlePlatformMessage(
          'amap_map_$id',
          const StandardMethodCodec().encodeMethodCall(
            const MethodCall('map#onTap', {
              'latLng': [30.0, 120.0],
            }),
          ),
          reply.complete,
        ));
        return reply.future;
      }

      final controllers = <AMapController>[];
      var taps = 0;
      Widget map(String key) => MaterialApp(
            home: AMapWidget(
              key: ValueKey(key),
              onMapCreated: controllers.add,
              onTap: (_) => taps++,
            ),
          );
      await tester.pumpWidget(map('ready'));
      platform.created(41);
      await tester.pump();
      expect(controllers, hasLength(1));
      expect(platform.listened, 9);
      expect(await nativeTap(41), isNotNull);
      await tester.pump();
      expect(taps, 1);

      await tester.pumpWidget(const SizedBox());
      controllers.single.disponse(); // Repeated disposal is harmless.
      await tester.pump();
      expect(platform.cancelled, 9);
      expect(await nativeTap(41), isNull);
      expect(taps, 1);

      await tester.pumpWidget(map('late'));
      final lateCreation = platform.created;
      lateCreation(42);
      await tester.pump();
      expect(controllers, hasLength(1));
      await tester.pumpWidget(const SizedBox());
      // The incoming handler is removed even while native init is pending.
      expect(await nativeTap(42), isNull);
      pending.complete();
      await tester.pump();
      expect(controllers, hasLength(1));
      expect(platform.listened, 9);
      expect(platform.cancelled, 9);
      expect(await nativeTap(42), isNull);
      lateCreation(
          42); // A platform callback arriving after unmount also retires.
      await tester.pump();
      expect(controllers, hasLength(1));
      expect(await nativeTap(42), isNull);
      expect(tester.takeException(), isNull);
    },
  );
}
