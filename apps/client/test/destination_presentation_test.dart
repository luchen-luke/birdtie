import 'package:birdtie_client/src/city/destination_presentation.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:flutter_test/flutter_test.dart';

PublicCity city(
  String id,
  String name,
  String region,
  String country, {
  String? source,
}) => PublicCity(
  id: id,
  name: name,
  region: region,
  countryCode: country,
  contentStatus: 'empty',
  source: PublicSource(
    label: 'isolated fixture',
    reference:
        source ??
        (id == 'aberdeen-gb'
            ? 'docs/product/01-Birdtie-产品文档-V3.0.md'
            : id.startsWith('civu-geo-')
            ? 'civu:geo_area:${id.substring(9)}'
            : 'fixture:$id'),
    maintainer: 'unit',
    freshness: 'unverified',
    updatedAt: null,
  ),
  map: null,
);

void main() {
  test(
    'checked Chinese names and country-region hierarchy preserve raw metadata',
    () {
      final cases = [
        (
          city(
            'civu-geo-4e2b4c4d-f186-42ca-b6f3-fc7e560dd00b',
            '上海市',
            'CN',
            'CN',
          ),
          '上海',
          '中国',
        ),
        (
          city(
            'civu-geo-7a536573-67c6-42f0-9ede-c423fb5dba00',
            '青岛市',
            '山东省',
            'CN',
          ),
          '青岛',
          '中国 · 山东',
        ),
        (city('aberdeen-gb', 'Aberdeen', 'Scotland', 'GB'), '阿伯丁', '英国 · 苏格兰'),
        (
          city(
            'civu-geo-66920ecb-9717-42bb-8320-a43a1d2851d2',
            'Edinburgh',
            '苏格兰',
            'GB',
          ),
          '爱丁堡',
          '英国 · 苏格兰',
        ),
        (
          city(
            'civu-geo-53e52b17-4c9a-4bdc-bd69-c74c8932f3c6',
            'London',
            'GB',
            'GB',
          ),
          '伦敦',
          '英国',
        ),
      ];
      for (final (value, name, region) in cases) {
        final before = (
          value.id,
          value.name,
          value.region,
          value.countryCode,
          value.source.reference,
        );
        expect(destinationHasReviewedIdentity(value), isTrue);
        expect(destinationDisplayName(value), name);
        expect(destinationDisplayRegion(value), region);
        expect((
          value.id,
          value.name,
          value.region,
          value.countryCode,
          value.source.reference,
        ), before);
      }
    },
  );

  test(
    'Chinese aliases augment original English and provenance mismatches cannot borrow them',
    () {
      final value = city(
        'civu-geo-66920ecb-9717-42bb-8320-a43a1d2851d2',
        'Edinburgh',
        '苏格兰',
        'GB',
      );
      for (final query in ['爱丁堡', 'EDINBURGH', '英国', '苏格兰', 'GB']) {
        expect(destinationMatchesQuery(value, query), isTrue);
      }
      for (final wrong in [
        city(value.id, 'Edinburgh', 'source region', 'GB', source: 'unrelated'),
        city(value.id, 'Edinburgh', 'source region', 'CN'),
        city('same-name-other-identity', 'Edinburgh', 'source region', 'GB'),
      ]) {
        expect(destinationHasReviewedIdentity(wrong), isFalse);
        expect(destinationDisplayName(wrong), 'Edinburgh');
        expect(destinationMatchesQuery(wrong, '爱丁堡'), isFalse);
      }
    },
  );

  test(
    'unknown destinations retain source labels without inferred subdivisions',
    () {
      expect(destinationDisplayRegion(city('unknown', '原名', 'CN', 'CN')), '中国');
      expect(
        destinationDisplayRegion(city('unknown', '原名', '原始地区', 'CN')),
        '中国 · 原始地区',
      );
      expect(destinationDisplayRegion(city('unknown', '原名', '', '')), isEmpty);
    },
  );
}
