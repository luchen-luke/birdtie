import 'package:birdtie_client/src/city/public_city_map.dart';
import 'dart:async';
import 'package:birdtie_client/src/city/native_city_map_io.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/now_discovery_controller.dart';
import 'package:birdtie_client/src/workspace/active_social_intent_card.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_conversation.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/top_controls.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mapbox_maps_flutter/mapbox_maps_flutter.dart'
    show MapWidget, CameraViewportState;
import 'now_scope_recovery_test.dart';

/// Real Now render boxes and the exact SDK argument carrier. The fixture has
/// no basemap: these checks do not stand in for native logo/link/device proof.
void main() {
  for (final scale in [1.0, 1.7]) {
    testWidgets('署名几何$scale字号跟随实测顶部控件且公开首屏不被私人错误卡占据', (t) async {
      t.view.devicePixelRatio = 1;
      t.view.physicalSize = const Size(390, 844);
      addTearDown(t.view.resetDevicePixelRatio);
      addTearDown(t.view.resetPhysicalSize);
      final f = NowFixture();
      addTearDown(f.dispose);
      await f.mount(t, scale: scale, safeBottom: 24);
      final map = find.byType(PublicCityMapView),
          element = map.evaluate().single;
      final header = t.getRect(find.byType(TopControls));
      final top = t.widget<PublicCityMapView>(map).ornamentTop!;
      expect(top, closeTo(header.bottom + 8, .5));
      expect(find.byType(ActiveSocialIntentCard), findsNothing);
      expect(
        t.getRect(find.byKey(const Key('now-native-context-scroll'))).top,
        greaterThanOrEqualTo(top + 48 + 7.5),
      );
      expect(find.text('该城市尚未配置地图视图。'), findsOneWidget);
      await t.enterText(nowField(), '保留的四行\n第二行\n第三行\n第四行');
      await t.pumpAndSettle();
      expect(identical(element, map.evaluate().single), true);
      expect(t.widget<PublicCityMapView>(map).ornamentTop, top);
      expect(t.takeException(), isNull);
      await f.unmount(t);
    });
    testWidgets('最高结果面板与IME$scale字号始终避开署名slot并保留原Map元素', (t) async {
      t.view.devicePixelRatio = 1;
      t.view.physicalSize = const Size(390, 844);
      addTearDown(t.view.resetDevicePixelRatio);
      addTearDown(t.view.resetPhysicalSize);
      final f = NowFixture();
      addTearDown(f.dispose);
      await f.mount(t, scale: scale, keyboardInset: 280, safeBottom: 24);
      final map = find.byType(PublicCityMapView),
          element = map.evaluate().single;
      await nowSend(t, '找地点');
      await t.enterText(nowField(), '尚未发送的第一行\n第二行地点\n第三行时间\n第四行范围');
      await t.pumpAndSettle();
      f.workspace(t).setSheetExtent(AgentSheetExtent.expanded);
      await t.pumpAndSettle();
      final top = t.widget<PublicCityMapView>(map).ornamentTop!;
      final sheet = t.getRect(find.byType(AgentResultsSheet));
      expect(sheet.top, greaterThanOrEqualTo(top + 48 + 7.5));
      expect(
        sheet.bottom,
        lessThanOrEqualTo(t.getRect(find.byType(AgentComposer)).top - 11.5),
      );
      expect(identical(element, map.evaluate().single), true);
      // Typing expands the unified message stream. The current fixture has
      // no entity/source cards; assert its real synthetic reply is in that
      // stream rather than reintroducing the retired results-only mode.
      expect(f.workspace(t).contentMode, AgentContentMode.conversation);
      expect(find.byType(AgentConversation), findsOneWidget);
      expect(
        find.descendant(
          of: find.byType(AgentConversation),
          matching: find.text('合成权威响应：找地点'),
        ),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('now-city-picker')).hitTestable(),
        findsOneWidget,
      );
      await nowTap(t, find.byKey(const Key('now-city-picker')));
      expect(
        find.descendant(
          of: find.byType(BottomSheet),
          matching: find.text('选择城市与范围'),
        ),
        findsOneWidget,
      );
      await nowBack(t);
      await nowTap(t, find.byTooltip('打开侧边栏'));
      expect(find.byType(Drawer), findsOneWidget);
      await nowBack(t);
      expect(f.workspace(t).contentMode, AgentContentMode.conversation);
      expect(f.source.queries, ['找地点']);
      expect(identical(element, map.evaluate().single), true);
      expect(
        t.widget<TextField>(nowField()).controller!.text,
        '尚未发送的第一行\n第二行地点\n第三行时间\n第四行范围',
      );
      expect(t.takeException(), isNull);
      await f.unmount(t);
    });
  }
  for (final selected in [true, false]) {
    testWidgets(
      '实际SDK构造：${selected ? "已有真实范围控制" : "无目录世界底图与恢复"}',
      (t) async {
        const tokenChannel =
            'dev.flutter.pigeon.mapbox_maps_flutter._MapboxOptions.setAccessToken';
        t.binding.defaultBinaryMessenger.setMockMessageHandler(
          tokenChannel,
          (message) async => const StandardMessageCodec().encodeMessage([null]),
        );
        addTearDown(
          () => t.binding.defaultBinaryMessenger.setMockMessageHandler(
            tokenChannel,
            null,
          ),
        );
        // Keep the native platform view creation pending. This inspects the real
        // SDK widget/arguments, not SDK paint, tiles, GPS or native event success.
        final pending = Completer<Object?>();
        t.binding.defaultBinaryMessenger.setMockMethodCallHandler(
          SystemChannels.platform_views,
          (call) async {
            if (call.method == 'create') return pending.future;
            return null;
          },
        );
        addTearDown(
          () => t.binding.defaultBinaryMessenger.setMockMethodCallHandler(
            SystemChannels.platform_views,
            null,
          ),
        );
        final city = _NoDirectoryCity(selected: selected),
            ws = AgentWorkspaceController(),
            viewport = MapViewportState(),
            discovery = NowDiscoveryController(authorizationHeader: () => null);
        var boundsEvents = 0;
        final ornamentTop = ValueNotifier<double?>(112);
        await t.pumpWidget(
          MaterialApp(
            home: MapCanvas(
              city: city,
              workspace: ws,
              mapState: viewport,
              discovery: discovery,
              onInitialViewport: (_) => boundsEvents++,
              onMapUnavailable: (_) {},
              onChooseCity: () => city.choose(),
              ornamentTop: ornamentTop,
            ),
          ),
        );
        await t.pump();
        final publicView = t.widget<PublicCityMapView>(
          find.byType(PublicCityMapView),
        );
        expect(publicView.city?.id, selected ? 'explicit-test-city' : null);
        expect(publicView.entities, isEmpty);
        expect(publicView.places, isEmpty);
        expect(publicView.onViewportInitialized == null, !selected);
        const configuredToken = String.fromEnvironment(
          'BIRDTIE_MAPBOX_MOBILE_PUBLIC_TOKEN',
        );
        if (!configuredToken.startsWith('pk.')) {
          // Default whole-suite builds intentionally have no map credential.
          // Exercise the real native widget directly; a second logged run with
          // a synthetic pk tests the unchanged MapCanvas -> PublicCityMap branch.
          expect(find.byType(NativeCityMapView), findsNothing);
          await t.pumpWidget(
            MaterialApp(
              home: ListenableBuilder(
                listenable: city,
                builder: (context, child) => NativeCityMapView(
                  city: city.selectedCity,
                  places: const [],
                  accessToken: 'pk.synthetic_widget_arguments_only',
                  onPlaceSelected: (_) {},
                  onViewportInitialized: city.selectedCity == null
                      ? null
                      : (_) => boundsEvents++,
                  onViewportSettled: city.selectedCity == null ? null : (_) {},
                  ornamentTop: 112,
                ),
              ),
            ),
          );
          await t.pump();
        }
        expect(find.byType(NativeCityMapView), findsOneWidget);
        expect(find.byType(MapWidget), findsOneWidget);
        final nativeElement = find.byType(NativeCityMapView).evaluate().single;
        final sdkElement = find.byType(MapWidget).evaluate().single;
        var native = t.widget<NativeCityMapView>(
          find.byType(NativeCityMapView),
        );
        expect(native.entities, isEmpty);
        expect(native.places, isEmpty);
        expect(native.ornamentTop, 112);
        expect(city.requests, 0);
        expect(boundsEvents, 0);
        final camera =
            t.widget<MapWidget>(find.byType(MapWidget)).viewport
                as CameraViewportState;
        expect(camera.center!.coordinates.lng, selected ? -2 : 0);
        expect(camera.center!.coordinates.lat, selected ? 57 : 0);
        expect(camera.zoom, selected ? 11 : 1);
        if (!selected) {
          expect(native.city, isNull);
          expect(native.onViewportInitialized, isNull);
          expect(native.onViewportSettled, isNull);
          city.choose();
          await t.pump();
          native = t.widget<NativeCityMapView>(find.byType(NativeCityMapView));
          expect(native.city?.id, 'explicit-test-city');
          expect(native.onViewportInitialized, isNotNull);
          expect(
            identical(
              nativeElement,
              find.byType(NativeCityMapView).evaluate().single,
            ),
            true,
          );
          expect(
            identical(sdkElement, find.byType(MapWidget).evaluate().single),
            true,
          );
        }
        expect(native.entities, isEmpty);
        expect(city.requests, 0);
        expect(boundsEvents, 0);
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        city.dispose();
        ws.dispose();
        viewport.dispose();
        discovery.dispose();
        ornamentTop.dispose();
      },
      variant: TargetPlatformVariant.only(TargetPlatform.android),
    );
  }
}

class _NoDirectoryCity extends PublicCityController {
  _NoDirectoryCity({required bool selected})
    : choice = selected ? catalog : null;
  static const catalog = PublicCity(
    id: 'explicit-test-city',
    name: '合成已选范围',
    region: 'synthetic',
    contentStatus: 'test',
    source: PublicSource(
      label: 'test',
      maintainer: 'test',
      freshness: 'test',
      updatedAt: null,
    ),
    map: PublicCityMap(
      provider: 'mapbox',
      latitude: 57,
      longitude: -2,
      defaultZoom: 11,
      sourceRef: 'synthetic',
    ),
  );
  PublicCity? choice;
  int requests = 0;
  @override
  PublicCity? get selectedCity => choice;
  @override
  List<PublicCity> get cities => const [];
  @override
  Future<void> loadCities() async {
    requests++;
  }

  void choose() {
    choice = catalog;
    notifyListeners();
  }
}
