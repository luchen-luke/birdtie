import 'package:birdtie_client/src/city/destination_artwork.dart';
import 'package:birdtie_client/src/city/destination_shortlist.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:flutter_test/flutter_test.dart';

const _shanghai = 'civu-geo-4e2b4c4d-f186-42ca-b6f3-fc7e560dd00b';
const _aberdeen = 'aberdeen-gb';
const _beijing = 'civu-geo-7800f687-1404-4bb4-aed3-ddd54a6376af';
const _edinburgh = 'civu-geo-66920ecb-9717-42bb-8320-a43a1d2851d2';
const _qingdao = 'civu-geo-7a536573-67c6-42f0-9ede-c423fb5dba00';
const _jinan = 'civu-geo-bbffd2af-68b4-49ee-bb01-5e5a7673aa9b';

PublicCity _city(String id, {String? country, String? source}) => PublicCity(
  id: id,
  name: '合成测试名称 $id',
  region: '合成测试地区',
  countryCode: country ?? (id == _aberdeen || id == _edinburgh ? 'GB' : 'CN'),
  contentStatus: 'empty',
  source: PublicSource(
    label: '隔离测试目录',
    reference:
        source ??
        (id == _aberdeen
            ? 'docs/product/01-Birdtie-产品文档-V3.0.md'
            : id.startsWith('civu-geo-')
            ? 'civu:geo_area:${id.substring(9)}'
            : 'synthetic-test:$id'),
    maintainer: '测试',
    freshness: 'unverified',
    updatedAt: null,
  ),
  map: null,
);

List<PublicCity> _catalog() => [
  _city('synthetic-unselected'),
  _city(_beijing),
  _city(_jinan),
  _city(_shanghai),
  _city(_aberdeen),
  _city(_edinburgh),
  _city(_qingdao),
];

void main() {
  test(
    'reviewed common and featured identities replace full catalog browse',
    () {
      final catalog = _catalog(), before = [...catalog];
      final result = destinationShortlist(catalog: catalog);
      expect(result.map((c) => c.id), [
        _shanghai,
        _aberdeen,
        _qingdao,
        _jinan,
        _edinburgh,
        _beijing,
      ]);
      expect(catalog, before);
      expect(identical(result.first, catalog[3]), isTrue);
      expect(result.any((c) => c.id == 'synthetic-unselected'), isFalse);
    },
  );

  test(
    'only actual current and recent catalog identities enter bounded list',
    () {
      final catalog = [
        ..._catalog(),
        for (var i = 0; i < 12; i++) _city('synthetic-recent-$i'),
      ];
      final result = destinationShortlist(
        catalog: catalog,
        currentCityID: 'synthetic-recent-0',
        recentCityIDs: [
          'not-in-catalog',
          _shanghai,
          for (var i = 0; i < 12; i++) 'synthetic-recent-$i',
        ],
        limit: 100,
      );
      expect(result.length, 8);
      expect(result.map((c) => c.id).take(3), [
        _shanghai,
        _aberdeen,
        'synthetic-recent-0',
      ]);
      expect(result.map((c) => c.id).toSet().length, result.length);
      expect(result.any((c) => c.id == 'not-in-catalog'), isFalse);
    },
  );

  test(
    'country or provenance mismatch never binds a reviewed identity by name',
    () {
      final result = destinationShortlist(
        catalog: [
          _city(_shanghai, country: 'IS'),
          _city(_aberdeen, source: 'unrelated-source'),
          _city('same-name-different-id'),
          _city(_beijing),
        ],
      );
      expect(result.map((c) => c.id), [_beijing]);
    },
  );

  test(
    'classification filters only shortlist and does not mutate full search catalog',
    () {
      final catalog = _catalog();
      final domestic = destinationShortlist(catalog: catalog, filter: 'CN');
      final abroad = destinationShortlist(catalog: catalog, filter: 'abroad');
      expect(domestic.map((c) => c.id), [
        _shanghai,
        _qingdao,
        _jinan,
        _beijing,
      ]);
      expect(abroad.map((c) => c.id), [_aberdeen, _edinburgh]);
      expect(catalog.any((c) => c.id == 'synthetic-unselected'), isTrue);
      expect(destinationShortlist(catalog: const []), isEmpty);
    },
  );

  test('selected original souvenirs never replace a remote revision', () {
    for (final item in {
      _shanghai: 'shanghai',
      _aberdeen: 'aberdeen',
      _beijing: 'beijing',
    }.entries) {
      expect(
        destinationLocalArtwork(item.key),
        'assets/destinations/${item.value}.png',
      );
      expect(
        destinationLocalArtwork(item.key, remoteVersion: 'a' * 64),
        isNull,
      );
    }
    expect(destinationLocalArtwork('same-name-different-id'), isNull);
  });
}
