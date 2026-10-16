import 'dart:async';
import 'dart:io';
import 'dart:typed_data';

import 'package:amap_map/amap_map.dart' as amap;
import 'package:amap_map/src/core/amap_flutter_platform.dart';
import 'package:amap_map/src/core/method_channel_amap_map.dart';
import 'package:birdtie_client/src/city/amap_city_map_io.dart';
import 'package:birdtie_client/src/city/amap_native_configuration.dart';
import 'package:birdtie_client/src/city/amap_offline_style.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:plugin_platform_interface/plugin_platform_interface.dart';

final _files = {
  for (final name in ['style.data', 'style_extra.data'])
    '${AMapOfflineStyleLoader.assetDirectory}/$name': File(
      '${AMapOfflineStyleLoader.assetDirectory}/$name',
    ).readAsBytesSync(),
};

ByteData _offsetData(Uint8List bytes) {
  final storage = Uint8List(bytes.length + 8)
    ..setRange(4, bytes.length + 4, bytes);
  return ByteData.view(storage.buffer, 4, bytes.length);
}

class _Bundle extends CachingAssetBundle {
  _Bundle(this.read);
  final Future<ByteData> Function(String) read;
  int calls = 0;
  @override
  Future<ByteData> load(String key) {
    calls++;
    return read(key);
  }
}

/// Keeps the real widget/option serialization, without constructing an SDK view.
class _Platform extends MethodChannelAMapFlutterMap
    with MockPlatformInterfaceMixin {
  final parameters = <Map<String, dynamic>>[];
  @override
  Widget buildView(
    Map<String, dynamic> creationParams,
    Set<Factory<OneSequenceGestureRecognizer>> gestureRecognizers,
    void Function(int) onPlatformViewCreated,
  ) {
    parameters.add(creationParams);
    return const SizedBox.expand();
  }
}

const _city = PublicCity(
  id: 'synthetic-style-city',
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

Widget _map({double inset = 0}) => MaterialApp(
  home: Scaffold(
    body: Padding(
      padding: EdgeInsets.all(inset),
      child: AMapCityMapView(
        city: _city,
        places: const [],
        entities: const [],
        selectedEntityId: null,
        onPlaceSelected: (_) {},
        onEntitySelected: (_) {},
        fullBleed: true,
      ),
    ),
  ),
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('bundled Android export retains the exact user files', () async {
    final style = await AMapOfflineStyleLoader().load();
    expect(style.enabled, isTrue);
    expect(
      sha256.convert(style.styleData!).toString(),
      '2e58328b9a676e23705f8a11ded22f9eb5d5482d1c2d1d00b9d67494ec25c8d1',
    );
    expect(
      sha256.convert(style.styleExtraData!).toString(),
      'a2055f0e095badfbbb402eb5e61b49ec5a7ff71795be6600a6a55a92bb9e034b',
    );
  });

  test(
    'one shared pair load honors byte offsets and reuses successful result',
    () async {
      final first = Completer<ByteData>();
      final bundle = _Bundle((key) async {
        if (key.endsWith('/style.data')) return first.future;
        return _offsetData(_files[key]!);
      });
      final loader = AMapOfflineStyleLoader(bundle: bundle);
      final a = loader.load(), b = loader.load();
      expect(identical(a, b), isTrue);
      expect(bundle.calls, 1);
      first.complete(_offsetData(_files.values.first));
      final loaded = await a;
      expect(identical(loaded, await b), isTrue);
      expect(identical(loaded, await loader.load()), isTrue);
      expect(bundle.calls, 2);
      expect(loaded.styleData, orderedEquals(_files.values.first));
      expect(loaded.styleExtraData, orderedEquals(_files.values.last));
    },
  );

  test(
    'a damaged pair is rejected and a subsequent load retries both files',
    () async {
      var damaged = true;
      final bundle = _Bundle((key) async {
        if (damaged && key.endsWith('/style_extra.data')) return ByteData(0);
        return _offsetData(_files[key]!);
      });
      final loader = AMapOfflineStyleLoader(bundle: bundle);
      await expectLater(loader.load(), throwsStateError);
      expect(bundle.calls, 2);
      damaged = false;
      expect((await loader.load()).enabled, isTrue);
      expect(bundle.calls, 4);
    },
  );

  testWidgets(
    'style failure retries; both files and consent precede first map, rebuild retains it',
    (tester) async {
      final messenger = tester.binding.defaultBinaryMessenger;
      final platform = _Platform();
      AMapFlutterPlatform.instance = platform;
      var firstAttempt = true, reads = 0;
      final extra = Completer<ByteData>();
      messenger.setMockMessageHandler('flutter/assets', (message) async {
        final key = const StringCodec().decodeMessage(message)!;
        if (!_files.containsKey(key)) return null;
        reads++;
        if (firstAttempt) {
          firstAttempt = false;
          return ByteData(0);
        }
        if (key.endsWith('/style_extra.data')) return extra.future;
        return _offsetData(_files[key]!);
      });
      messenger.setMockMethodCallHandler(
        AMapNativeConfiguration.channel,
        (_) async => true,
      );
      addTearDown(() {
        messenger.setMockMessageHandler('flutter/assets', null);
        messenger.setMockMethodCallHandler(
          AMapNativeConfiguration.channel,
          null,
        );
      });
      await tester.pumpWidget(_map());
      await tester.pump();
      expect(find.text('地图样式暂时无法加载，请重试或使用地点列表。'), findsOneWidget);
      expect(platform.parameters, isEmpty);
      await tester.tap(find.text('重试地图样式'));
      await tester.pump();
      expect(find.text('正在准备地图样式…'), findsOneWidget);
      expect(platform.parameters, isEmpty);
      extra.complete(_offsetData(_files.values.last));
      await tester.pump();
      expect(find.text('同意并启用高德地图'), findsOneWidget);
      expect(platform.parameters, isEmpty);
      await tester.tap(find.text('同意并启用高德地图'));
      await tester.pump();
      final mapState = tester.state(find.byType(amap.AMapWidget));
      final mapWidget = tester.widget<amap.AMapWidget>(
        find.byType(amap.AMapWidget),
      );
      expect(mapWidget.labelsEnabled, isTrue);
      expect(mapWidget.touchPoiEnabled, isFalse);
      final options = platform.parameters.first['options'] as Map;
      final style = options['customStyleOptions'] as Map;
      expect(style['enabled'], isTrue);
      expect(style['styleData'], orderedEquals(_files.values.first));
      expect(style['styleExtraData'], orderedEquals(_files.values.last));
      expect(platform.parameters.first['apiKey'], isNull);
      final privacy = platform.parameters.first['privacyStatement'] as Map;
      expect(privacy['hasAgree'], isTrue);
      await tester.pumpWidget(_map(inset: 4));
      expect(tester.state(find.byType(amap.AMapWidget)), same(mapState));
      expect(reads, 3);
      expect(
        tester
            .widget<amap.AMapWidget>(find.byType(amap.AMapWidget))
            .customStyleOptions,
        same(mapWidget.customStyleOptions),
      );
      await tester.pumpWidget(const SizedBox());
      expect(tester.takeException(), isNull);
    },
    variant: TargetPlatformVariant.only(TargetPlatform.android),
  );
}
