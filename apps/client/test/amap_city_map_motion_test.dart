import 'package:amap_map/amap_map.dart' as amap;
import 'package:amap_map/src/core/amap_flutter_platform.dart';
import 'package:amap_map/src/core/method_channel_amap_map.dart';
import 'package:birdtie_client/src/city/amap_city_map_io.dart';
import 'package:birdtie_client/src/city/amap_native_configuration.dart';
import 'package:birdtie_client/src/city/amap_offline_style.dart';
import 'package:birdtie_client/src/city/amap_session_consent.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:plugin_platform_interface/plugin_platform_interface.dart';
import 'package:x_amap_base/x_amap_base.dart' as base;

class _MapSurface extends MethodChannelAMapFlutterMap
    with MockPlatformInterfaceMixin {
  int builds = 0;
  @override
  Widget buildView(
    Map<String, dynamic> creationParams,
    Set<Factory<OneSequenceGestureRecognizer>> gestureRecognizers,
    void Function(int) created,
  ) {
    builds++;
    return const SizedBox.expand();
  }
}

void main() {
  final binding = TestWidgetsFlutterBinding.ensureInitialized();
  setUpAll(() async {
    await androidAMapOfflineStyle.load();
  });
  testWidgets(
    'one camera motion hides overlays once without rebuilding each tick',
    (tester) async {
      final surface = _MapSurface();
      AMapFlutterPlatform.instance = surface;
      final consent = AMapSessionConsent.accepted;
      AMapSessionConsent.accepted = true;
      binding.defaultBinaryMessenger.setMockMethodCallHandler(
        AMapNativeConfiguration.channel,
        (_) async => true,
      );
      addTearDown(() {
        AMapFlutterPlatform.instance = _MapSurface();
        AMapSessionConsent.accepted = consent;
        binding.defaultBinaryMessenger.setMockMethodCallHandler(
          AMapNativeConfiguration.channel,
          null,
        );
      });
      var starts = 0;
      const city = PublicCity(
        id: 'isolated-motion-city',
        name: '隔离单测城市',
        region: '',
        countryCode: 'CN',
        contentStatus: 'ready',
        source: PublicSource(
          label: 'isolated',
          maintainer: 'isolated',
          freshness: 'test',
          updatedAt: null,
        ),
        map: PublicCityMap(
          provider: 'amap',
          latitude: 31.2,
          longitude: 121.4,
          defaultZoom: 11,
          sourceRef: 'isolated',
        ),
      );
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: AMapCityMapView(
              city: city,
              places: const [],
              entities: const [],
              selectedEntityId: null,
              onPlaceSelected: (_) {},
              onEntitySelected: (_) {},
              fullBleed: true,
              onCameraMotion: () {
                starts++;
              },
            ),
          ),
        ),
      );
      await tester.runAsync(() async {
        await androidAMapOfflineStyle.load();
      });
      await tester.pump();
      await tester.pump();
      expect(find.byType(amap.AMapWidget), findsOneWidget);
      final element = tester.element(find.byType(amap.AMapWidget));
      amap.CameraPosition camera(int i) => amap.CameraPosition(
        target: base.LatLng(31.2 + i / 10000, 121.4),
        zoom: 11,
      );
      tester
          .widget<amap.AMapWidget>(find.byType(amap.AMapWidget))
          .onCameraMove!(camera(0));
      await tester.pump();
      final firstMotionBuilds = surface.builds;
      for (var i = 1; i <= 8; i++) {
        tester
            .widget<amap.AMapWidget>(find.byType(amap.AMapWidget))
            .onCameraMove!(camera(i));
        await tester.pump();
      }
      expect(starts, 1);
      expect(tester.element(find.byType(amap.AMapWidget)), same(element));
      expect(
        surface.builds,
        firstMotionBuilds,
        reason:
            'Camera ticks must not rebuild the platform view or whole overlay tree',
      );
      await tester.pumpWidget(const SizedBox.shrink());
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );
}
