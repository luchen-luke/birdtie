import 'dart:convert';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'map_entities.dart';
import 'now_context_query_api.dart';

const mapLayerKinds = [
  'PLACE',
  'ACTIVITY',
  'MOMENT',
  'ORGANIZATION',
  'BUSINESS',
  'OPPORTUNITY',
];
String mapLayerLabel(String kind) => switch (kind) {
  'PLACE' => '地点',
  'ACTIVITY' => '活动',
  'MOMENT' => '公开记录',
  'ORGANIZATION' => '组织',
  'BUSINESS' => '商家经营场地',
  'OPPORTUNITY' => '我的活动机会',
  _ => '内容',
};
Map<String, dynamic> _mapObject(dynamic x, Set<String> keys) {
  if (x is! Map<String, dynamic> ||
      x.keys.toSet().difference(keys).isNotEmpty ||
      keys.difference(x.keys.toSet()).isNotEmpty) {
    throw const FormatException('地图数据结构无效');
  }
  return x;
}

String _mapText(dynamic x, int limit, {bool empty = false}) {
  if (x is! String ||
      x.trim() != x ||
      (!empty && x.isEmpty) ||
      x.runes.length > limit ||
      x.contains('\u0000')) {
    throw const FormatException('地图内容无效');
  }
  return x;
}

class TypedMapItem {
  const TypedMapItem({
    required this.kind,
    required this.id,
    required this.title,
    required this.detailType,
    required this.detailID,
    required this.placeID,
    required this.anchorLabel,
    required this.latitude,
    required this.longitude,
    required this.sourceVersion,
  });
  final String kind,
      id,
      title,
      detailType,
      detailID,
      placeID,
      anchorLabel,
      sourceVersion;
  final double latitude, longitude;
  String get mapID => '${kind.toLowerCase()}:$id';
  String get explanation => switch (kind) {
    'MOMENT' => '本人明确公开的地点情境，不表示当前位置',
    'BUSINESS' => '已核验经营关系的公开场地，不代表交易背书',
    'OPPORTUNITY' => '仅你可见 · 当前公开活动的地点',
    'ORGANIZATION' => '组织明确提交并审核公开的地点',
    _ => anchorLabel,
  };
  MapEntity get entity => MapEntity(
    id: mapID,
    kind: switch (kind) {
      'PLACE' => MapEntityKind.place,
      'ACTIVITY' => MapEntityKind.activity,
      'MOMENT' => MapEntityKind.moment,
      'ORGANIZATION' => MapEntityKind.organization,
      'BUSINESS' => MapEntityKind.business,
      _ => MapEntityKind.opportunity,
    },
    title: title,
    subtitle: explanation,
    latitude: latitude,
    longitude: longitude,
  );
  factory TypedMapItem.decode(dynamic raw, String scope, MapBounds bounds) {
    final x = _mapObject(raw, {
      'kind',
      'id',
      'title',
      'entityRef',
      'detailRef',
      'anchor',
      'sourceVersion',
    });
    final kind = _mapText(x['kind'], 20), id = _mapText(x['id'], 80);
    final er = _mapObject(x['entityRef'], {'type', 'id'}),
        dr = _mapObject(x['detailRef'], {'type', 'id'});
    if (!mapLayerKinds.contains(kind) || er['type'] != kind || er['id'] != id) {
      throw const FormatException('地图身份无效');
    }
    if (scope == 'SELF_PRIVATE') {
      final ids = id.split(':');
      if (kind != 'OPPORTUNITY' ||
          ids.length != 2 ||
          !ids.every(validOnlineID) ||
          dr['type'] != 'ACTIVITY' ||
          dr['id'] != ids[1]) {
        throw const FormatException('私人机会身份无效');
      }
    } else if (kind == 'OPPORTUNITY' ||
        !validOnlineID(id) ||
        dr['type'] != kind ||
        dr['id'] != id) {
      throw const FormatException('公开地图身份无效');
    }
    final a = _mapObject(x['anchor'], {
      'placeId',
      'label',
      'coordinateSystem',
      'precision',
      'latitude',
      'longitude',
    });
    final place = _mapText(a['placeId'], 36, empty: kind == 'ORGANIZATION');
    if ((kind == 'ORGANIZATION' ? place.isNotEmpty : !validOnlineID(place)) ||
        a['coordinateSystem'] != 'wgs84' ||
        a['precision'] != 'point' ||
        a['latitude'] is! num ||
        a['longitude'] is! num) {
      throw const FormatException('没有明确公开点位');
    }
    final lat = (a['latitude'] as num).toDouble(),
        lon = (a['longitude'] as num).toDouble();
    if (!lat.isFinite ||
        !lon.isFinite ||
        lat < bounds.south ||
        lat > bounds.north ||
        lon < bounds.west ||
        lon > bounds.east) {
      throw const FormatException('点位范围无效');
    }
    return TypedMapItem(
      kind: kind,
      id: id,
      title: _mapText(x['title'], 160),
      detailType: dr['type'],
      detailID: dr['id'],
      placeID: place,
      anchorLabel: _mapText(a['label'], 160),
      latitude: lat,
      longitude: lon,
      sourceVersion: _mapText(x['sourceVersion'], 180),
    );
  }
}

class TypedMapView {
  const TypedMapView({
    required this.cityID,
    required this.scope,
    required this.observedAt,
    required this.validUntil,
    required this.truncated,
    required this.items,
  });
  final String cityID, scope;
  final DateTime observedAt, validUntil;
  final bool truncated;
  final List<TypedMapItem> items;
  factory TypedMapView.decode(
    dynamic raw,
    String city,
    MapBounds bounds,
    bool private,
  ) {
    final x = _mapObject(raw, {
      'schemaVersion',
      'cityId',
      'scope',
      'observedAt',
      'validUntil',
      'truncated',
      'items',
    });
    final expected = private ? 'SELF_PRIVATE' : 'PUBLIC';
    final observed = onlineStamp(x['observedAt']),
        until = onlineStamp(x['validUntil']);
    if (x['schemaVersion'] != 'typed-map-layers-v1' ||
        x['cityId'] != city ||
        x['scope'] != expected ||
        x['truncated'] is! bool ||
        x['items'] is! List ||
        (x['items'] as List).length > 150 ||
        !until.isAfter(observed) ||
        until.difference(observed) > const Duration(minutes: 2)) {
      throw const FormatException('地图来源已失效');
    }
    final items = (x['items'] as List)
        .map((e) => TypedMapItem.decode(e, expected, bounds))
        .toList(growable: false);
    if (items.map((e) => e.mapID).toSet().length != items.length) {
      throw const FormatException('重复地图身份');
    }
    return TypedMapView(
      cityID: city,
      scope: expected,
      observedAt: observed,
      validUntil: until,
      truncated: x['truncated'],
      items: List.unmodifiable(items),
    );
  }
}

class MapLayersApi {
  MapLayersApi({http.Client? client, String? apiBaseUrl})
    : _client = client ?? http.Client(),
      _owns = client == null,
      base = (apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl).replaceFirst(
        RegExp(r'/$'),
        '',
      );
  final http.Client _client;
  final bool _owns;
  final String base;
  bool _closed = false;
  Future<TypedMapView> read(
    String city,
    MapBounds bounds, {
    String? authorization,
    bool private = false,
  }) async {
    if (_closed ||
        !bounds.isValid ||
        city.isEmpty ||
        city.contains('/') ||
        (private && authorization == null)) {
      throw const FormatException('地图读取条件无效');
    }
    final route = private ? '/v1/me/cities/' : '/v1/cities/';
    final uri =
        Uri.parse(
          '$base$route${Uri.encodeComponent(city)}/${private ? 'map-opportunities' : 'map-layers'}',
        ).replace(
          queryParameters: bounds.toJson().map(
            (k, v) => MapEntry(k, v.toString()),
          ),
        );
    final response = await _client
        .get(uri, headers: {'Authorization': ?authorization})
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) {
      throw const FormatException('当前地图内容暂不可用，请重新读取');
    }
    final envelope = _mapObject(jsonDecode(response.body), {'data'});
    return TypedMapView.decode(envelope['data'], city, bounds, private);
  }

  void dispose() {
    if (_closed) return;
    _closed = true;
    if (_owns) _client.close();
  }
}
