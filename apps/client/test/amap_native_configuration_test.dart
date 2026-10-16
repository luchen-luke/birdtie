import 'dart:async';

import 'package:amap_map/amap_map.dart' as amap;
import 'package:birdtie_client/src/city/amap_city_map_io.dart';
import 'package:birdtie_client/src/city/amap_native_configuration.dart';
import 'package:birdtie_client/src/city/amap_offline_style.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

const _city = PublicCity(
  id: 'synthetic-domestic-city',
  name: '测试城市',
  region: '',
  countryCode: 'CN',
  contentStatus: 'ready',
  source: PublicSource(
    label: 'test',
    maintainer: 'test',
    freshness: 'test',
    updatedAt: null,
  ),
  map: PublicCityMap(
    provider: 'amap',
    latitude: 31.2,
    longitude: 121.4,
    defaultZoom: 11,
    sourceRef: 'synthetic',
  ),
);

Widget _map() => MaterialApp(
  home: Scaffold(
    body: AMapCityMapView(
      city: _city,
      places: const [],
      entities: const [],
      selectedEntityId: null,
      onPlaceSelected: (_) {},
      onEntitySelected: (_) {},
      fullBleed: true,
    ),
  ),
);

void main() {
  final binding = TestWidgetsFlutterBinding.ensureInitialized();
  final messenger = binding.defaultBinaryMessenger;

  // These cases isolate key detection. Prepare the real Android style pair
  // outside each widget's fake clock; style loading/failure has its own tests.
  setUpAll(() async {
    await androidAMapOfflineStyle.load();
  });

  tearDown(() {
    messenger.setMockMethodCallHandler(AMapNativeConfiguration.channel, null);
    debugDefaultTargetPlatformOverride = null;
  });

  test(
    'manifest presence grants configuration without passing a key to SDK',
    () async {
      var calls = 0;
      messenger.setMockMethodCallHandler(AMapNativeConfiguration.channel, (
        call,
      ) async {
        calls++;
        expect(call.method, 'hasAndroidAMapKey');
        expect(call.arguments, isNull);
        return true;
      });
      final configuration = await AMapNativeConfiguration.inspect(
        platform: TargetPlatform.android,
        androidKey: '',
      );
      expect(configuration.source, AMapNativeKeySource.androidManifest);
      expect(configuration.configured, isTrue);
      expect(configuration.apiKey, isNull);
      expect(calls, 1);
    },
  );

  test(
    'explicit Android define and existing iOS behavior bypass native probe',
    () async {
      var calls = 0;
      messenger.setMockMethodCallHandler(AMapNativeConfiguration.channel, (
        _,
      ) async {
        calls++;
        return false;
      });
      final android = await AMapNativeConfiguration.inspect(
        platform: TargetPlatform.android,
        androidKey: 'synthetic-android-define',
        iosKey: 'unrelated-ios-define',
      );
      expect(android.source, AMapNativeKeySource.dartDefine);
      expect(android.apiKey!.androidKey, 'synthetic-android-define');
      expect(android.apiKey!.iosKey, isNull);
      final ios = await AMapNativeConfiguration.inspect(
        platform: TargetPlatform.iOS,
        androidKey: 'unrelated-android-define',
        iosKey: 'synthetic-ios-define',
      );
      expect(ios.configured, isTrue);
      expect(ios.apiKey!.iosKey, 'synthetic-ios-define');
      expect(ios.apiKey!.androidKey, isNull);
      final missingIOS = await AMapNativeConfiguration.inspect(
        platform: TargetPlatform.iOS,
        iosKey: '',
      );
      expect(missingIOS.source, AMapNativeKeySource.missing);
      expect(missingIOS.apiKey, isNull);
      expect(calls, 0);
    },
  );

  test(
    'missing metadata and channel errors are distinct and never configured',
    () async {
      for (final value in [false, null, 'unexpected-string']) {
        messenger.setMockMethodCallHandler(
          AMapNativeConfiguration.channel,
          (_) async => value,
        );
        final configuration = await AMapNativeConfiguration.inspect(
          platform: TargetPlatform.android,
          androidKey: '',
        );
        expect(configuration.configured, isFalse);
        expect(configuration.apiKey, isNull);
        expect(
          configuration.source,
          value == false
              ? AMapNativeKeySource.missing
              : AMapNativeKeySource.unavailable,
        );
      }
      messenger.setMockMethodCallHandler(AMapNativeConfiguration.channel, (
        _,
      ) async {
        throw PlatformException(
          code: 'synthetic-error',
          message: 'must-not-surface',
        );
      });
      final unavailable = await AMapNativeConfiguration.inspect(
        platform: TargetPlatform.android,
        androidKey: '',
      );
      expect(unavailable.source, AMapNativeKeySource.unavailable);
      expect(unavailable.apiKey, isNull);
    },
  );

  testWidgets(
    'loading and missing config retry reach consent without initializing SDK',
    (tester) async {
      final pending = <Completer<bool>>[];
      messenger.setMockMethodCallHandler(AMapNativeConfiguration.channel, (_) {
        final response = Completer<bool>();
        pending.add(response);
        return response.future;
      });
      await tester.pumpWidget(_map());
      await tester.runAsync(() async {
        await androidAMapOfflineStyle.load();
      });
      await tester.pump();
      expect(find.text('正在检查高德地图配置…'), findsOneWidget);
      expect(find.byType(amap.AMapWidget), findsNothing);
      pending.single.complete(false);
      await tester.pump();
      expect(find.text('该设备的高德地图尚未配置，请使用地点列表。'), findsOneWidget);
      expect(find.byType(amap.AMapWidget), findsNothing);
      await tester.tap(find.text('重试'));
      await tester.pump();
      expect(find.text('正在检查高德地图配置…'), findsOneWidget);
      pending.last.complete(true);
      await tester.pump();
      expect(find.text('同意并启用高德地图'), findsOneWidget);
      expect(find.byType(amap.AMapWidget), findsNothing);
      expect(pending.length, 2);
      await tester.pumpWidget(const SizedBox());
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );

  testWidgets(
    'timeout is retryable and its late response cannot replace current probe',
    (tester) async {
      final pending = <Completer<bool>>[];
      messenger.setMockMethodCallHandler(AMapNativeConfiguration.channel, (_) {
        final response = Completer<bool>();
        pending.add(response);
        return response.future;
      });
      await tester.pumpWidget(_map());
      await tester.runAsync(() async {
        await androidAMapOfflineStyle.load();
      });
      await tester.pump();
      await tester.pump(const Duration(seconds: 5));
      expect(find.text('暂时无法读取高德地图配置，请重试或使用地点列表。'), findsOneWidget);
      expect(find.byType(amap.AMapWidget), findsNothing);
      await tester.tap(find.text('重试'));
      await tester.pump();
      pending.first.complete(false);
      await tester.pump();
      expect(find.text('正在检查高德地图配置…'), findsOneWidget);
      pending.last.complete(true);
      await tester.pump();
      expect(find.text('同意并启用高德地图'), findsOneWidget);
      expect(find.byType(amap.AMapWidget), findsNothing);
      await tester.pumpWidget(const SizedBox());
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );

  testWidgets(
    'configuration reply after map disposal does not initialize or setState',
    (tester) async {
      final pending = Completer<bool>();
      messenger.setMockMethodCallHandler(
        AMapNativeConfiguration.channel,
        (_) => pending.future,
      );
      await tester.pumpWidget(_map());
      await tester.runAsync(() async {
        await androidAMapOfflineStyle.load();
      });
      await tester.pump();
      await tester.pumpWidget(const MaterialApp(home: Text('离开地图')));
      pending.complete(true);
      await tester.pump();
      expect(find.text('离开地图'), findsOneWidget);
      expect(find.byType(amap.AMapWidget), findsNothing);
      expect(tester.takeException(), isNull);
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );
}
