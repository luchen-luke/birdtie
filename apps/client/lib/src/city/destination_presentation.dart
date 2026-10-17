import 'public_city_controller.dart';

/// Display labels for existing identities, checked against Birdtie's public
/// catalog. Chinese names follow Civu's user-approved destination artwork.
const _reviewed = <String, (String, String, String)>{
  'civu-geo-4e2b4c4d-f186-42ca-b6f3-fc7e560dd00b': ('CN', '上海', '中国'),
  'aberdeen-gb': ('GB', '阿伯丁', '英国 · 苏格兰'),
  'civu-geo-7a536573-67c6-42f0-9ede-c423fb5dba00': ('CN', '青岛', '中国 · 山东'),
  'civu-geo-bbffd2af-68b4-49ee-bb01-5e5a7673aa9b': ('CN', '济南', '中国 · 山东'),
  'civu-geo-66920ecb-9717-42bb-8320-a43a1d2851d2': ('GB', '爱丁堡', '英国 · 苏格兰'),
  'civu-geo-7800f687-1404-4bb4-aed3-ddd54a6376af': ('CN', '北京', '中国'),
  'civu-geo-a3e6e0fe-6e2e-4789-8591-0b1a8c2b4c21': ('CN', '杭州', '中国 · 浙江'),
  'civu-geo-3545e9c0-951b-43c4-b08e-6e251f06961a': ('CN', '成都', '中国 · 四川'),
  'civu-geo-f27e9a14-6461-429a-8c8c-51a23825ac00': ('CN', '西安', '中国 · 陕西'),
  'civu-geo-e608a913-b824-4102-827b-4f2f6340cc58': ('CN', '厦门', '中国 · 福建'),
  'civu-geo-62580aa5-27db-449c-85c2-fb1d18f6e27a': ('CN', '三亚', '中国 · 海南'),
  'civu-geo-bf470294-58bf-4c40-8ae9-7bdb133aa79a': ('CN', '广州', '中国 · 广东'),
  'civu-geo-ce95fde6-9f53-4e08-9395-603b29d93695': ('CN', '重庆', '中国'),
  'civu-geo-53e52b17-4c9a-4bdc-bd69-c74c8932f3c6': ('GB', '伦敦', '英国'),
  'civu-geo-cd00608f-6497-4912-ac88-2c55ccd0f57e': ('GB', '约克', '英国'),
  'civu-geo-3d036847-c12d-464b-b2d4-28e2018a2c45': ('GB', '格拉斯哥', '英国 · 苏格兰'),
  'civu-geo-bd8be026-9f57-4fd9-81a8-2ae331ebe3ca': ('GB', '因弗内斯', '英国 · 苏格兰'),
  'civu-geo-ea7251b0-0f1e-47ff-b2ab-48ee344fcaa4': ('GB', '奥本', '英国 · 苏格兰'),
};

Iterable<String> get destinationReviewedCityIDs => _reviewed.keys;

bool destinationHasReviewedIdentity(PublicCity city) {
  final labels = _reviewed[city.id];
  if (labels == null || labels.$1 != city.countryCode) return false;
  final reference = city.id == 'aberdeen-gb'
      ? 'docs/product/01-Birdtie-产品文档-V3.0.md'
      : 'civu:geo_area:${city.id.substring(9)}';
  return city.source.reference == reference;
}

String destinationDisplayName(PublicCity city) =>
    destinationHasReviewedIdentity(city) ? _reviewed[city.id]!.$2 : city.name;

String destinationDisplayRegion(PublicCity city) {
  if (destinationHasReviewedIdentity(city)) return _reviewed[city.id]!.$3;
  // Preserve unknown source text. Never infer a province or canonical identity.
  final country = switch (city.countryCode) {
    'CN' => '中国',
    'GB' => '英国',
    'IS' => '冰岛',
    _ => city.countryCode,
  };
  final region = city.region.trim();
  if (region.isEmpty || region == city.countryCode || region == country) {
    return country;
  }
  return country.isEmpty ? region : '$country · $region';
}

/// Search retains raw source labels as well as checked Chinese display names.
/// Classification is deliberately not an input to global/local query matching.
bool destinationMatchesQuery(PublicCity city, String query) =>
    '${city.name} ${city.region} ${city.countryCode} '
            '${destinationDisplayName(city)} ${destinationDisplayRegion(city)}'
        .toLowerCase()
        .contains(query.trim().toLowerCase());
