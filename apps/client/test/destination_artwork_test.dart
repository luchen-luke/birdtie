import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:birdtie_client/src/city/destination_artwork.dart';
import 'package:birdtie_client/src/city/destination_artwork_view.dart';
import 'package:birdtie_client/src/city/destination_picker_grid.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

// Isolated one-pixel PNG and synthetic catalog identities; no provider payload
// or product destination is persisted by these tests.
final _png = base64Decode(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a1X8AAAAASUVORK5CYII=',
);
final _version = List.filled(64, 'a').join();

CityArtworkMetadata _metadata([String cityID = 'unit-city']) =>
    CityArtworkMetadata(
      cityID: cityID,
      version: _version,
      descriptorPath: '/v1/cities/$cityID/artwork/$_version/descriptor',
    );

Map<String, Object?> _descriptor(CityArtworkMetadata metadata) => {
  'cityId': metadata.cityID,
  'version': metadata.version,
  'contentPath': '${metadata.basePath}/content',
  'contentType': 'image/png',
  'width': 1,
  'height': 1,
  'byteCount': _png.length,
  'sha256': sha256.convert(_png).toString(),
};

Map<String, Object?> _cityJSON(String id, {Object? artwork}) => {
  'id': id,
  'name': '合成城市 $id',
  'region': '合成地区',
  'countryCode': 'GB',
  'contentStatus': 'unit',
  'source': {'label': 'unit', 'maintainer': 'unit'},
  'map': null,
  'artwork': artwork,
};

Future<void> _drainUntil(bool Function() ready) async {
  for (var i = 0; i < 30 && !ready(); i++) {
    await Future<void>.delayed(Duration.zero);
  }
  expect(ready(), isTrue, reason: 'bounded fixture request settlement');
}

class _VisibleArtworkLoader extends DestinationArtworkLoader {
  _VisibleArtworkLoader() : super(apiBaseUrl: 'https://unit.invalid');
  final requestedCities = <String>{};
  @override
  Future<Uint8List> load(CityArtworkMetadata metadata) {
    requestedCities.add(metadata.cityID);
    return SynchronousFuture(_png);
  }
}

class _VersionArtworkLoader extends DestinationArtworkLoader {
  _VersionArtworkLoader(this.pendingVersion)
    : super(apiBaseUrl: 'https://unit.invalid');
  final String pendingVersion;
  final pending = Completer<Uint8List>();
  @override
  Future<Uint8List> load(CityArtworkMetadata metadata) =>
      metadata.version == pendingVersion
      ? pending.future
      : SynchronousFuture(_png);
}

void main() {
  test(
    'all newly selected Civu originals are bundled under exact catalog IDs',
    () async {
      const selected = <String, (String, String)>{
        '3545e9c0-951b-43c4-b08e-6e251f06961a': (
          'chengdu',
          'cf8eba71100cb74682c3e342eb92cf202943321ce9a38d6ff977e3cf7863c4eb',
        ),
        'ce95fde6-9f53-4e08-9395-603b29d93695': (
          'chongqing',
          '912fda3d7e3a4ab9751b3225dacc411846f5fce5f622af7fb822bb3f368336c8',
        ),
        'bf470294-58bf-4c40-8ae9-7bdb133aa79a': (
          'guangzhou',
          'f2f3f5fc35a5926f4711c0fa88869ed8bd2565157b73d162e23299641c272af7',
        ),
        'a3e6e0fe-6e2e-4789-8591-0b1a8c2b4c21': (
          'hangzhou',
          '51a553bc7d8e931a784fbad4ccf9810e695104de6b2020d6876aaee2d8284e36',
        ),
        '62580aa5-27db-449c-85c2-fb1d18f6e27a': (
          'sanya',
          '24003933d483f33f7e122f4d084a21b6ab52607ddfdb9cb6926878d4e57d0708',
        ),
        'e608a913-b824-4102-827b-4f2f6340cc58': (
          'xiamen',
          'c41b8d2ef713a4bcaeac103af2deec7cac433da7cc5f8a8b612649ee17423ee0',
        ),
        'f27e9a14-6461-429a-8c8c-51a23825ac00': (
          'xian',
          '0e32c246a9510ebf1a789065cb07b531937f737fb6466ec1af5b92cf56d50bfd',
        ),
        '3d036847-c12d-464b-b2d4-28e2018a2c45': (
          'glasgow',
          '29106527a824983853792ebd365dd2f6307e2049c7611f05ef79d0a8d607a9c9',
        ),
        'bd8be026-9f57-4fd9-81a8-2ae331ebe3ca': (
          'inverness',
          'c7b5a389dc150508b20c97927506a708a00526f1d5a8cf1722d62ee96525b1ad',
        ),
        '53e52b17-4c9a-4bdc-bd69-c74c8932f3c6': (
          'london',
          '56b56d79698b42dd1ee81670741bcdb9c8944dd7ac666c52ae0057bdad9f8303',
        ),
        'ea7251b0-0f1e-47ff-b2ab-48ee344fcaa4': (
          'oban',
          '3113b95fe329561924ee7a7f80b8b5c116fe19ea533be427dad1cb5191483358',
        ),
        'cd00608f-6497-4912-ac88-2c55ccd0f57e': (
          'york',
          'c71a4e0e79ce1a858dbb45eef935231b5d49a95c9c6646aaad6307bc45f680f4',
        ),
      };
      for (final entry in selected.entries) {
        final id = 'civu-geo-${entry.key}',
            asset = 'assets/destinations/${entry.value.$1}.png';
        expect(destinationLocalArtwork(id), asset);
        expect(destinationLocalArtwork(id, remoteVersion: _version), isNull);
        final data = await rootBundle.load(asset);
        expect(
          sha256
              .convert(
                data.buffer.asUint8List(data.offsetInBytes, data.lengthInBytes),
              )
              .toString(),
          entry.value.$2,
        );
        expect(data.getUint32(16), entry.value.$1 == 'york' ? 768 : 512);
        expect(data.getUint32(20), data.getUint32(16));
      }
      for (final id in [
        'civu-geo-419c8051-a273-4e0a-81b4-98ec13103b1f',
        'civu-geo-a1bfe797-7607-4992-8088-fc14d9dd42da',
        'civu-geo-cb9a67eb-fa97-4ef5-8849-114cc1199717',
        'civu-geo-1e2773a8-1638-45d4-81bc-b332db729c61',
        'manual-city:lisbon',
      ]) {
        expect(destinationLocalArtwork(id), isNull);
      }
    },
  );

  testWidgets(
    'reviewed grid exposes Chinese label and selects the unchanged catalog object',
    (t) async {
      final city = PublicCity(
        id: 'aberdeen-gb',
        name: 'Aberdeen',
        region: 'Scotland',
        countryCode: 'GB',
        contentStatus: 'empty',
        map: null,
        source: const PublicSource(
          label: 'unit',
          reference: 'docs/product/01-Birdtie-产品文档-V3.0.md',
          maintainer: 'unit',
          freshness: 'unverified',
          updatedAt: null,
        ),
      );
      final loader = _VisibleArtworkLoader();
      final semantics = t.ensureSemantics();
      PublicCity? selected;
      try {
        await t.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: CustomScrollView(
                slivers: [
                  DestinationPickerGrid(
                    cities: [city],
                    loader: loader,
                    onSelect: (value) => selected = value,
                  ),
                ],
              ),
            ),
          ),
        );
        await t.pumpAndSettle();
        expect(find.text('阿伯丁'), findsOneWidget);
        expect(find.text('英国 · 苏格兰'), findsOneWidget);
        expect(find.text('Aberdeen'), findsNothing);
        expect(find.bySemanticsLabel('阿伯丁，英国 · 苏格兰'), findsOneWidget);
        await t.tap(find.byKey(const Key('now-scope-city-aberdeen-gb')));
        expect(selected, same(city));
        expect(selected!.name, 'Aberdeen');
        expect(t.takeException(), isNull);
      } finally {
        await t.pumpWidget(const SizedBox.shrink());
        semantics.dispose();
        loader.dispose();
      }
    },
  );

  test('optional artwork failures preserve selectable catalog identity', () {
    final metadata = _metadata();
    for (final raw in [
      null,
      'malformed',
      <String, Object?>{},
      {'version': 'invalid', 'descriptorPath': metadata.descriptorPath},
      {'version': _version, 'descriptorPath': 'https://other.invalid/image'},
      {'version': _version, 'descriptorPath': '/v1/cities/other/descriptor'},
    ]) {
      final city = PublicCity.fromJson(
        _cityJSON(metadata.cityID, artwork: raw),
      );
      expect(city.id, metadata.cityID);
      expect(city.artwork, isNull);
      expect(city.artworkDeclared, raw != null);
    }
    final city = PublicCity.fromJson(
      _cityJSON(
        metadata.cityID,
        artwork: {
          'version': _version,
          'descriptorPath': metadata.descriptorPath,
        },
      ),
    );
    expect(city.artwork?.cityID, metadata.cityID);
    expect(city.artwork?.key, metadata.key);
  });

  test('local souvenir fallback never replaces an unknown server version', () {
    const qingdao = 'civu-geo-7a536573-67c6-42f0-9ede-c423fb5dba00';
    const reviewedVersion =
        '9a62fcce9f889e12fa192cb58ed0bb010089e7b59b6595070949be9bfa0182f0';
    expect(
      destinationLocalArtwork(qingdao),
      'assets/destinations/qingdao-d2988c88.png',
    );
    expect(
      destinationLocalArtwork(qingdao, remoteVersion: reviewedVersion),
      'assets/destinations/qingdao-d2988c88.png',
    );
    expect(destinationLocalArtwork(qingdao, remoteVersion: _version), isNull);
    expect(destinationLocalArtwork('unknown-city'), isNull);
  });

  test(
    'descriptor and bytes use configured origin, verified version and cache',
    () async {
      final metadata = _metadata();
      final requests = <http.Request>[];
      final client = MockClient((request) async {
        requests.add(request);
        return request.url.path.endsWith('/descriptor')
            ? http.Response(jsonEncode({'data': _descriptor(metadata)}), 200)
            : http.Response.bytes(
                _png,
                200,
                headers: {'content-type': 'image/png'},
              );
      });
      final loader = DestinationArtworkLoader(
        client: client,
        apiBaseUrl: 'https://unit.invalid:8443/old-path?ignored=1#fragment',
      );
      addTearDown(loader.dispose);
      addTearDown(client.close);
      expect(await loader.load(metadata), orderedEquals(_png));
      expect(await loader.load(metadata), orderedEquals(_png));
      expect(requests.map((r) => r.url.path), [
        metadata.descriptorPath,
        '${metadata.basePath}/content',
      ]);
      for (final request in requests) {
        expect(request.url.origin, 'https://unit.invalid:8443');
        expect(request.url.query, isEmpty);
        expect(request.url.fragment, isEmpty);
        expect(request.followRedirects, isFalse);
        expect(request.headers.containsKey('authorization'), isFalse);
      }
    },
  );

  test(
    'foreign identity, version, URL, over-324 dimensions and bad bytes fail closed',
    () async {
      final metadata = _metadata();
      final cases = <Map<String, Object?>>[
        {'cityId': 'other-city'},
        {'version': List.filled(64, 'b').join()},
        {'contentPath': 'https://other.invalid/image.png'},
        {'contentPath': '${metadata.basePath}/other-content'},
        {'width': 325},
        {'height': 325},
        {'byteCount': 512 * 1024 + 1},
        {'sha256': List.filled(64, '0').join()},
        {'width': 2},
        {'byteCount': _png.length + 1},
        {'contentType': 'image/jpeg'},
      ];
      for (final change in cases) {
        final paths = <String>[];
        final client = MockClient((request) async {
          paths.add(request.url.path);
          return request.url.path.endsWith('/descriptor')
              ? http.Response(
                  jsonEncode({..._descriptor(metadata), ...change}),
                  200,
                )
              : http.Response.bytes(
                  _png,
                  200,
                  headers: {'content-type': 'image/png'},
                );
        });
        final loader = DestinationArtworkLoader(
          client: client,
          apiBaseUrl: 'https://unit.invalid',
        );
        try {
          await expectLater(
            loader.load(metadata),
            throwsFormatException,
            reason: '$change',
          );
          expect(
            paths.every((path) => path.startsWith(metadata.basePath)),
            true,
          );
          if (change.containsKey('cityId') ||
              change.containsKey('version') ||
              change.containsKey('contentPath') ||
              change['width'] == 325 ||
              change['height'] == 325 ||
              change['contentType'] == 'image/jpeg' ||
              change['byteCount'] == 512 * 1024 + 1) {
            expect(paths, [
              metadata.descriptorPath,
            ], reason: 'reject before content read');
          }
        } finally {
          loader.dispose();
          client.close();
        }
      }
    },
  );

  test(
    'two reads at most and identical artwork shares one in-flight request',
    () async {
      final metadata = List.generate(3, (i) => _metadata('unit-city-$i'));
      final gates = List.generate(3, (_) => Completer<void>());
      var active = 0, peak = 0;
      final requests = <String>[];
      final client = MockClient((request) async {
        requests.add(request.url.path);
        active++;
        if (active > peak) peak = active;
        try {
          final item = metadata.firstWhere(
            (item) => request.url.path.startsWith(item.basePath),
          );
          if (request.url.path.endsWith('/descriptor')) {
            await gates[metadata.indexOf(item)].future;
            return http.Response(jsonEncode(_descriptor(item)), 200);
          }
          return http.Response.bytes(
            _png,
            200,
            headers: {'content-type': 'image/png'},
          );
        } finally {
          active--;
        }
      });
      final loader = DestinationArtworkLoader(
        client: client,
        apiBaseUrl: 'https://unit.invalid',
      );
      addTearDown(loader.dispose);
      addTearDown(client.close);
      final first = loader.load(metadata[0]);
      final same = loader.load(metadata[0]);
      final second = loader.load(metadata[1]);
      final third = loader.load(metadata[2]);
      expect(identical(first, same), true);
      await _drainUntil(() => requests.length == 2);
      expect(requests, [
        metadata[0].descriptorPath,
        metadata[1].descriptorPath,
      ]);
      gates[0].complete();
      await first;
      await _drainUntil(() => requests.contains(metadata[2].descriptorPath));
      gates[1].complete();
      gates[2].complete();
      final images = await Future.wait([same, second, third]);
      expect(images.every((image) => image.length == _png.length), true);
      expect(peak, 2);
      expect(
        requests.where((path) => path == metadata[0].descriptorPath),
        hasLength(1),
      );
      expect(requests, hasLength(6));
    },
  );

  testWidgets(
    'new artwork loading clears old pixels and invalid remote metadata cannot use old local souvenir',
    (t) async {
      final old = _metadata();
      final newVersion = List.filled(64, 'b').join();
      final next = CityArtworkMetadata(
        cityID: old.cityID,
        version: newVersion,
        descriptorPath:
            '/v1/cities/${old.cityID}/artwork/$newVersion/descriptor',
      );
      final loader = _VersionArtworkLoader(newVersion);
      Widget host(
        String cityID,
        CityArtworkMetadata? artwork, {
        bool declared = true,
      }) => MaterialApp(
        home: DestinationArtworkView(
          key: const ValueKey('stable-artwork-view'),
          cityID: cityID,
          artwork: artwork,
          hasRemoteArtwork: declared,
          loader: loader,
        ),
      );
      try {
        await t.pumpWidget(host(old.cityID, old));
        await t.pump();
        expect(find.byType(Image), findsOneWidget);
        await t.pumpWidget(host(next.cityID, next));
        expect(loader.pending.isCompleted, false);
        expect(
          find.byType(Image),
          findsNothing,
          reason: 'new version must not retain the old FutureBuilder snapshot',
        );
        expect(find.byIcon(Icons.place_outlined), findsOneWidget);
        loader.pending.complete(_png);
        await t.pumpAndSettle();
        expect(find.byType(Image), findsOneWidget);

        const reviewedCityID = 'civu-geo-7a536573-67c6-42f0-9ede-c423fb5dba00';
        final invalid = PublicCity.fromJson(
          _cityJSON(
            reviewedCityID,
            artwork: {
              'version': 'invalid',
              'descriptorPath': 'https://other.invalid/untrusted.png',
            },
          ),
        );
        expect(destinationLocalArtwork(reviewedCityID), isNotNull);
        expect(invalid.artwork, isNull);
        expect(invalid.artworkDeclared, true);
        await t.pumpWidget(
          host(invalid.id, invalid.artwork, declared: invalid.artworkDeclared),
        );
        expect(
          find.byType(Image),
          findsNothing,
          reason:
              'declared but invalid remote artwork cannot downgrade to local',
        );
        expect(find.byIcon(Icons.place_outlined), findsOneWidget);
        expect(t.takeException(), isNull);
      } finally {
        await t.pumpWidget(const SizedBox.shrink());
        loader.dispose();
      }
    },
  );

  testWidgets(
    '1067-city grid builds only visible thumbnails and selects original city',
    (t) async {
      final cities = List.generate(1067, (i) {
        final metadata = _metadata('unit-city-$i');
        return PublicCity.fromJson(
          _cityJSON(
            metadata.cityID,
            artwork: {
              'version': metadata.version,
              'descriptorPath': metadata.descriptorPath,
            },
          ),
        );
      });
      final loader = _VisibleArtworkLoader();
      PublicCity? selected;
      t.view.physicalSize = const Size(390, 600);
      t.view.devicePixelRatio = 1;
      try {
        await t.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: CustomScrollView(
                cacheExtent: 0,
                slivers: [
                  DestinationPickerGrid(
                    cities: cities,
                    loader: loader,
                    onSelect: (city) => selected = city,
                  ),
                ],
              ),
            ),
          ),
        );
        await t.pumpAndSettle();
        expect(
          find.byType(DestinationArtworkView).evaluate().length,
          inInclusiveRange(2, 10),
        );
        expect(loader.requestedCities.length, inInclusiveRange(2, 10));
        expect(
          find.byKey(const Key('now-scope-city-unit-city-1066')),
          findsNothing,
        );
        await t.tap(find.byKey(const Key('now-scope-city-unit-city-0')));
        expect(selected, same(cities.first));
        final images = t.widgetList<Image>(find.byType(Image));
        expect(images, isNotEmpty);
        expect(images.every((image) => image.fit == BoxFit.contain), true);
        expect(t.takeException(), isNull);
      } finally {
        await t.pumpWidget(const SizedBox.shrink());
        loader.dispose();
        t.view.resetPhysicalSize();
        t.view.resetDevicePixelRatio();
      }
    },
  );
}
