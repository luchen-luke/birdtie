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
