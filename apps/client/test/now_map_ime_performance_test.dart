import 'dart:async';
import 'dart:ui';
import 'package:amap_map/amap_map.dart' as amap;
import 'package:amap_map/src/core/amap_flutter_platform.dart';
import 'package:amap_map/src/core/method_channel_amap_map.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:plugin_platform_interface/plugin_platform_interface.dart';
import 'package:birdtie_client/src/city/amap_city_map_io.dart';
import 'package:birdtie_client/src/city/amap_native_configuration.dart';
import 'package:birdtie_client/src/city/amap_offline_style.dart';
import 'package:birdtie_client/src/city/amap_session_consent.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/now_map_inset_gate.dart';

class _ReadyMap extends MethodChannelAMapFlutterMap
    with MockPlatformInterfaceMixin {
  bool started = false;
  @override
  Widget buildView(
    Map<String, dynamic> options,
    Set<Factory<OneSequenceGestureRecognizer>> gestures,
    void Function(int) created,
  ) {
    if (!started) {
      started = true;
      WidgetsBinding.instance.addPostFrameCallback((_) => created(753));
    }
    return const SizedBox.expand();
  }
}

// The vendored controller captures its first platform in a library-level final.
// Keep one surface for this isolate and reset only the per-mount callback flag.
final _surface = _ReadyMap();

const _city = PublicCity(
  id: 'isolated-ime',
  name: '隔离单测',
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

Widget _view(Rect rect, String scope) => MaterialApp(
  home: Scaffold(
    body: AMapCityMapView(
      city: _city,
      contextKey: scope,
      contentRect: rect,
      places: const [],
      entities: const [],
      selectedEntityId: null,
      onPlaceSelected: (_) {},
      onEntitySelected: (_) {},
      fullBleed: true,
    ),
  ),
);

Future<void> _settleReady(WidgetTester tester) async {
  // The production widget starts its own asset/configuration reads at mount.
  // Let those real asset futures finish after mount, outside the fake clock.
  await tester.runAsync(() => androidAMapOfflineStyle.load());
  await tester.pump();
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 200));
  await tester.pump();
  await tester.pump();
}

void main() {
  final binding = TestWidgetsFlutterBinding.ensureInitialized();
  setUpAll(() async {
    // Cache the real asset future outside any per-test FakeAsync zone.
    await androidAMapOfflineStyle.load();
  });
  testWidgets(
    'ready AMap republishes settled viewport after initial callback changes scope',
    (tester) async {
      final consent = AMapSessionConsent.accepted;
      final surface = _surface..started = false;
      AMapFlutterPlatform.instance = surface;
      AMapSessionConsent.accepted = true;
      var scope = Object();
      var initialized = 0, contentQueries = 0;
      const native = MethodChannel('amap_map_753');
      binding.defaultBinaryMessenger.setMockMethodCallHandler(
        AMapNativeConfiguration.channel,
        (_) async => true,
      );
      binding.defaultBinaryMessenger.setMockMethodCallHandler(native, (
        call,
      ) async {
        if (call.method == 'map#toScreenCoordinate') {
          return {'x': 100, 'y': 100};
        }
        if (call.method == 'map#fromScreenCoordinate') {
          final point = call.arguments as Map;
          return [
            31.25 - (point['y'] as num) / 10000,
            121.4 + (point['x'] as num) / 10000,
          ];
        }
        return null;
      });
      addTearDown(() {
        AMapFlutterPlatform.instance = _surface;
        AMapSessionConsent.accepted = consent;
        binding.defaultBinaryMessenger.setMockMethodCallHandler(
          AMapNativeConfiguration.channel,
          null,
        );
        binding.defaultBinaryMessenger.setMockMethodCallHandler(native, null);
      });
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: AMapCityMapView(
              city: _city,
              places: const [],
              entities: const [],
              selectedEntityId: null,
              onPlaceSelected: (_) {},
              onEntitySelected: (_) {},
              fullBleed: true,
              currentReaderScope: () => scope,
              onViewportInitialized: (_) {
                initialized++;
                scope = Object();
              },
              onContentViewportSettled: (_, _) {
                contentQueries++;
              },
            ),
          ),
        ),
      );
      await _settleReady(tester);
      await tester.pump(const Duration(milliseconds: 400));
      await tester.pump();
      expect(surface.started, true);
      expect(initialized, 1);
      expect(
        contentQueries,
        1,
        reason: 'the current settled query must not require the first user pan',
      );
      await tester.pumpWidget(const SizedBox.shrink());
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );
  testWidgets(
    'ready AMap rect-only update skips projection barrier; source change does not',
    (tester) async {
      final consent = AMapSessionConsent.accepted;
      final surface = _surface..started = false;
      AMapFlutterPlatform.instance = surface;
      AMapSessionConsent.accepted = true;
      var projections = 0;
      const native = MethodChannel('amap_map_753');
      binding.defaultBinaryMessenger.setMockMethodCallHandler(
        AMapNativeConfiguration.channel,
        (_) async => true,
      );
      binding.defaultBinaryMessenger.setMockMethodCallHandler(native, (
        call,
      ) async {
        if (call.method == 'map#toScreenCoordinate') {
          projections++;
          return {'x': 100, 'y': 100};
        }
        if (call.method == 'map#fromScreenCoordinate') return [31.2, 121.4];
        return null;
      });
      addTearDown(() {
        AMapFlutterPlatform.instance = _surface;
        AMapSessionConsent.accepted = consent;
        binding.defaultBinaryMessenger.setMockMethodCallHandler(
          AMapNativeConfiguration.channel,
          null,
        );
        binding.defaultBinaryMessenger.setMockMethodCallHandler(native, null);
      });
      await tester.pumpWidget(
        _view(const Rect.fromLTRB(16, 100, 374, 700), 'a'),
      );
      await _settleReady(tester);
      expect(surface.started, true);
      expect(
        projections,
        greaterThan(0),
        reason: 'actual map-ready ACL barrier ran',
      );
      final first = projections;
      final element = tester.element(find.byType(amap.AMapWidget));
      await tester.pumpWidget(
        _view(const Rect.fromLTRB(16, 100, 374, 400), 'a'),
      );
      await tester.pump();
      await tester.pump();
      expect(projections, first, reason: 'no native diff or ACL retirement');
      await tester.pumpWidget(
        _view(const Rect.fromLTRB(16, 100, 374, 400), 'b'),
      );
      await tester.pump();
      await tester.pump();
      expect(
        projections,
        greaterThan(first),
        reason: 'same marker keys cannot bypass source retirement barrier',
      );
      expect(tester.element(find.byType(amap.AMapWidget)), same(element));
      await tester.pumpWidget(const SizedBox.shrink());
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );
  testWidgets(
    'ready AMap retirement timeout keeps pixels covered and retry releases the queue',
    (tester) async {
      final consent = AMapSessionConsent.accepted;
      final surface = _surface..started = false;
      AMapFlutterPlatform.instance = surface;
      AMapSessionConsent.accepted = true;
      const native = MethodChannel('amap_map_753');
      Completer<Object?>? blocked;
      var projections = 0;
      binding.defaultBinaryMessenger.setMockMethodCallHandler(
        AMapNativeConfiguration.channel,
        (_) async => true,
      );
      binding.defaultBinaryMessenger.setMockMethodCallHandler(native, (
        call,
      ) async {
        if (call.method == 'map#toScreenCoordinate') {
          projections++;
          if (blocked != null) return blocked!.future;
          return {'x': 100, 'y': 100};
        }
        if (call.method == 'map#fromScreenCoordinate') return [31.2, 121.4];
        return null;
      });
      addTearDown(() {
        if (blocked?.isCompleted == false)
          blocked!.complete({'x': 100, 'y': 100});
        AMapFlutterPlatform.instance = _surface;
        AMapSessionConsent.accepted = consent;
        binding.defaultBinaryMessenger.setMockMethodCallHandler(
          AMapNativeConfiguration.channel,
          null,
        );
        binding.defaultBinaryMessenger.setMockMethodCallHandler(native, null);
      });
      const rect = Rect.fromLTRB(16, 100, 374, 700);
      await tester.pumpWidget(_view(rect, 'a'));
      await _settleReady(tester);
      expect(surface.started, true);
      expect(projections, greaterThan(0));
      final element = tester.element(find.byType(amap.AMapWidget));
      final cover = find.byWidgetPredicate(
        (widget) =>
            widget is ColoredBox && widget.color == const Color(0xFFF3F4EF),
      );
      expect(cover, findsNothing);
      final before = projections;
      blocked = Completer<Object?>();
      await tester.pumpWidget(_view(rect, 'b'));
      await tester.pump();
      await tester.pump();
      expect(projections, greaterThan(before));
      expect(cover, findsOneWidget);
      await tester.pump(const Duration(seconds: 3));
      await tester.pump();
      expect(find.text('地图内容暂时无法显示，请重试。'), findsOneWidget);
      expect(cover, findsOneWidget);
      final late = blocked!;
      blocked = null;
      await tester.tap(find.text('重试地图'));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));
      await tester.pump();
      await tester.pump();
      expect(cover, findsNothing);
      expect(find.text('地图内容暂时无法显示，请重试。'), findsNothing);
      expect(tester.element(find.byType(amap.AMapWidget)), same(element));
      // A response from the timed-out scope is not a new grant or a rebuild.
      late.complete({'x': 100, 'y': 100});
      await tester.pump();
      expect(cover, findsNothing);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );
  testWidgets(
    'map chrome commits once after continuous IME, overlays independent',
    (tester) async {
      final commits = <Rect>[];
      final gate = NowMapInsetGate(commits.add);
      const initial = Rect.fromLTRB(16, 100, 374, 700);
      gate.offer(initial, 0);
      expect(commits, [initial]);
      for (var inset = 30; inset <= 300; inset += 30) {
        gate.offer(
          Rect.fromLTRB(16, 100, 374, 700 - inset.toDouble()),
          inset.toDouble(),
        );
        await tester.pump(const Duration(milliseconds: 16));
      }
      expect(commits.length, 1);
      await tester.pump(const Duration(milliseconds: 140));
      expect(commits, [initial, const Rect.fromLTRB(16, 100, 374, 400)]);
      // A stationary keyboard does not delay ordinary overlay geometry updates.
      gate.offer(const Rect.fromLTRB(16, 100, 374, 390), 300);
      expect(commits.length, 3);
      gate.dispose();
    },
  );
  testWidgets(
    'last close wins; duplicate and disposed deliveries do not fire',
    (tester) async {
      final commits = <Rect>[];
      final gate = NowMapInsetGate(commits.add);
      const original = Rect.fromLTRB(0, 0, 400, 800);
      gate.offer(original, 0);
      gate.offer(const Rect.fromLTRB(0, 0, 400, 500), 300);
      await tester.pump(const Duration(milliseconds: 30));
      gate.offer(original, 0);
      await tester.pump(const Duration(milliseconds: 200));
      expect(commits, [original]);
      gate.offer(const Rect.fromLTRB(0, 0, 400, 600), 200);
      gate.dispose();
      await tester.pump(const Duration(milliseconds: 200));
      expect(commits, [original]);
    },
  );
}
