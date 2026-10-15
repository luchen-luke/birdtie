import 'dart:convert';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'map_entities.dart';
import 'now_context_query_api.dart';
import 'entity_cover.dart';
import 'place_photo.dart';
import 'map_marker_visual.dart';
import 'public_moment_photo.dart';

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

EntityCoverMetadata? _mapPhoto(
  Object raw,
  String id,
  DateTime observed,
  DateTime baseUntil,
  DateTime now,
) {
  final m = _mapObject(raw, {
    'entityType',
    'entityId',
    'version',
    'thumbnailPath',
    'width',
    'height',
    'byteCount',
    'sha256',
    'expiresAt',
    'attribution',
  });
  final ref = PlacePhotoRef(id), end = onlineStamp(m['expiresAt']);
  if (!ref.valid ||
      m['entityType'] != 'place' ||
      m['entityId'] != id ||
      m['version'] is! int ||
      m['version'] < 1 ||
      m['version'] > entityCoverMaxVersion ||
      m['thumbnailPath'] != '${ref.path}/${m['version']}/content' ||
      m['width'] != 320 ||
      m['height'] != 180 ||
      m['byteCount'] is! int ||
      m['byteCount'] < 1 ||
      m['byteCount'] > entityCoverMaxBytes ||
      m['sha256'] is! String ||
      !RegExp(r'^[0-9a-f]{64}$').hasMatch(m['sha256']) ||
      !end.isAfter(observed) ||
      end.isAfter(observed.add(const Duration(seconds: 61))) ||
      m['attribution'] is! String ||
      utf8.encode(m['attribution']).length > 320 ||
      m['attribution'].trim() != m['attribution'] ||
      RegExp(r'[\x00-\x1f\x7f-\x9f]').hasMatch(m['attribution'])) {
    throw const FormatException('地点照片来源、版本或期限不符');
  }
  // Validate the original response, then retire only its annotation in transit.
  if (!now.isBefore(end)) return null;
  return EntityCoverMetadata(
    ref: ref,
    version: m['version'],
    path: m['thumbnailPath'],
    byteCount: m['byteCount'],
    digest: m['sha256'],
    expiresAt: end,
    attribution: m['attribution'],
  );
}

MapRecentMoments? _mapCount(
  Object raw,
  DateTime observed,
  DateTime baseUntil,
  DateTime now,
) {
  final m = _mapObject(raw, {
    'count',
    'checkedAt',
    'windowStart',
    'validUntil',
  });
  final checked = onlineStamp(m['checkedAt']),
      start = onlineStamp(m['windowStart']),
      until = onlineStamp(m['validUntil']);
  if (m['count'] is! int ||
      m['count'] < 0 ||
      m['count'] > 9007199254740991 ||
      start != checked.subtract(const Duration(days: 30)) ||
      checked.isBefore(observed) ||
      !until.isAfter(checked) ||
      until.isAfter(checked.add(const Duration(seconds: 60))) ||
      until.isAfter(baseUntil)) {
    throw const FormatException('地点动态计数或期限不符');
  }
  return now.isBefore(until)
      ? MapRecentMoments(
          count: m['count'],
          checkedAt: checked,
          windowStart: start,
          validUntil: until,
        )
      : null;
}

PublicMomentPhotoMetadata? _mapMomentPhoto(
  Object raw,
  String id,
  DateTime observed,
  DateTime until,
  DateTime now,
) {
  final photo = PublicMomentPhotoMetadata.decode(raw, id, observed, now: now);
  return photo.expiresAt.isAfter(now) ? photo : null;
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
    this.placePhoto,
    this.momentPhoto,
    this.recentMoments,
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
  final EntityCoverMetadata? placePhoto;
  final PublicMomentPhotoMetadata? momentPhoto;
  final MapRecentMoments? recentMoments;
  String get mapID => '${kind.toLowerCase()}:$id';
  String get explanation => switch (kind) {
    'MOMENT' => '本人明确公开的地点情境，不表示当前位置',
    'BUSINESS' => '已核验经营关系的公开场地，不代表交易背书',
    'OPPORTUNITY' => '仅你可见 · 当前公开活动的地点',
    'ORGANIZATION' => '组织明确提交并审核公开的地点',
    _ => anchorLabel,
  };

  /// Compact row identity. Shared publication/location qualifications belong
  /// to the whole result group; the native detail still keeps explanation.
  String get rowSubtitle {
    final label = kind == 'OPPORTUNITY' ? '仅本人可见的活动机会' : mapLayerLabel(kind);
    return anchorLabel == title || anchorLabel.isEmpty
        ? label
        : '$label · $anchorLabel';
  }

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
    placePhoto: placePhoto,
    momentPhoto: momentPhoto,
    recentMoments: recentMoments,
    sourceVersion: sourceVersion,
  );
  factory TypedMapItem.decode(
    dynamic raw,
    String scope,
    MapBounds bounds, {
    bool media = false,
    bool momentMedia = false,
    DateTime? observedAt,
    DateTime? validUntil,
    DateTime? now,
  }) {
    final keys = {
      'kind',
      'id',
      'title',
      'entityRef',
      'detailRef',
      'anchor',
      'sourceVersion',
    };
    if (media && raw is Map<String, dynamic>) {
      if (raw.containsKey('placePhoto')) keys.add('placePhoto');
      if (raw.containsKey('recentMoments')) keys.add('recentMoments');
    }
    if (momentMedia &&
        raw is Map<String, dynamic> &&
        raw.containsKey('momentPhoto'))
      keys.add('momentPhoto');
    final x = _mapObject(raw, keys);
    final kind = _mapText(x['kind'], 20), id = _mapText(x['id'], 80);
    if (x.containsKey('momentPhoto') &&
        (!momentMedia ||
            !{'PUBLIC', 'PUBLIC_AUTHOR'}.contains(scope) ||
            kind != 'MOMENT' ||
            observedAt == null ||
            validUntil == null))
      throw const FormatException('公开动态照片身份不符');
    if ((x.containsKey('placePhoto') || x.containsKey('recentMoments')) &&
        (!media ||
            scope != 'PUBLIC' ||
            kind != 'PLACE' ||
            observedAt == null ||
            validUntil == null)) {
      throw const FormatException('地图媒体身份不符');
    }
    final er = _mapObject(x['entityRef'], {'type', 'id'}),
        dr = _mapObject(x['detailRef'], {'type', 'id'});
    if (!mapLayerKinds.contains(kind) ||
        (scope == 'PUBLIC_AUTHOR' && kind != 'MOMENT') ||
        er['type'] != kind ||
        er['id'] != id) {
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
      title: _mapText(x['title'], media ? 300 : 160, empty: kind == 'MOMENT'),
      detailType: dr['type'],
      detailID: dr['id'],
      placeID: place,
      anchorLabel: _mapText(a['label'], media ? 300 : 160),
      latitude: lat,
      longitude: lon,
      sourceVersion: _mapText(x['sourceVersion'], 180),
      placePhoto: x['placePhoto'] == null
          ? null
          : _mapPhoto(
              x['placePhoto'],
              id,
              observedAt!,
              validUntil!,
              now ?? DateTime.now().toUtc(),
            ),
      momentPhoto: x['momentPhoto'] == null
          ? null
          : _mapMomentPhoto(
              x['momentPhoto'],
              id,
              observedAt!,
              validUntil!,
              now ?? DateTime.now().toUtc(),
            ),
      recentMoments: x['recentMoments'] == null
          ? null
          : _mapCount(
              x['recentMoments'],
              observedAt!,
              validUntil!,
              now ?? DateTime.now().toUtc(),
            ),
    );
  }
}

/// One notice per actual kind. Never relabel the private opportunity overlay
/// as public and never infer current whereabouts from a published anchor.
List<String> mapLayerScopeNotices(Iterable<TypedMapItem> items) {
  final kinds = items.map((item) => item.kind).toSet();
  return [
    if (kinds.contains('MOMENT')) '公开记录来自本人明确公开的地点情境，不表示作者当前位置。',
    if (kinds.contains('BUSINESS')) '商家点位为已核验经营关系的公开场地，不代表交易背书。',
    if (kinds.contains('ORGANIZATION')) '组织点位由组织明确提交并审核公开。',
    if (kinds.contains('OPPORTUNITY')) '活动机会仅本人可见，点位来自当前公开活动，不公开意图或关系。',
  ];
}

class TypedMapView {
  const TypedMapView({
    required this.cityID,
    required this.scope,
    required this.observedAt,
    required this.validUntil,
    required this.truncated,
    required this.items,
    this.authorID,
  });
  final String cityID, scope;
  final String? authorID;
  final DateTime observedAt, validUntil;
  final bool truncated;
  final List<TypedMapItem> items;
  factory TypedMapView.decode(
    dynamic raw,
    String city,
    MapBounds bounds,
    bool private, {
    String? authorID,
    DateTime? now,
  }) {
    if (authorID != null &&
        (private || !validOnlineID(authorID) || !bounds.isValid)) {
      throw const FormatException('公开作者地图读取条件无效');
    }
    final x = _mapObject(raw, {
      'schemaVersion',
      'cityId',
      'scope',
      'observedAt',
      'validUntil',
      'truncated',
      'items',
      if (authorID != null) 'authorId',
    });
    final expected = authorID != null
        ? 'PUBLIC_AUTHOR'
        : private
        ? 'SELF_PRIVATE'
        : 'PUBLIC';
    final observed = onlineStamp(x['observedAt']),
        until = onlineStamp(x['validUntil']);
    final momentMedia = !private && x['schemaVersion'] == 'typed-map-layers-v4';
    final media =
        momentMedia ||
        authorID == null &&
            !private &&
            x['schemaVersion'] == 'typed-map-layers-v3';
    if ((!media &&
            x['schemaVersion'] !=
                (authorID == null
                    ? 'typed-map-layers-v1'
                    : 'typed-map-layers-v2')) ||
        x['cityId'] != city ||
        x['scope'] != expected ||
        (authorID != null && x['authorId'] != authorID) ||
        x['truncated'] is! bool ||
        x['items'] is! List ||
        (x['items'] as List).length > (authorID == null ? 150 : 30) ||
        !until.isAfter(observed) ||
        until.difference(observed) > const Duration(minutes: 2)) {
      throw const FormatException('地图来源已失效');
    }
    final items = (x['items'] as List)
        .map(
          (e) => TypedMapItem.decode(
            e,
            expected,
            bounds,
            media: media,
            momentMedia: momentMedia,
            observedAt: observed,
            validUntil: until,
            now: now,
          ),
        )
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
      authorID: authorID,
    );
  }
}

/// Safe status only; response bodies never enter diagnostics or retry policy.
class MapLayersReadFailure implements Exception {
  const MapLayersReadFailure(this.statusCode);
  final int statusCode;
  bool get retryable =>
      statusCode == 409 || (statusCode >= 500 && statusCode <= 599);
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
    String? authorID,
  }) async {
    if (_closed ||
        !bounds.isValid ||
        city.isEmpty ||
        city.contains('/') ||
        (private && authorization == null) ||
        (authorID != null && (private || !validOnlineID(authorID)))) {
      throw const FormatException('地图读取条件无效');
    }
    final route = private ? '/v1/me/cities/' : '/v1/cities/';
    final uri =
        Uri.parse(
          '$base$route${Uri.encodeComponent(city)}/${private ? 'map-opportunities' : 'map-layers'}',
        ).replace(
          queryParameters: {
            ...bounds.toJson().map((k, v) => MapEntry(k, v.toString())),
            if (authorID != null) 'scope': 'public-author',
            if (authorID != null) 'authorId': authorID,
          },
        );
    final response = await _client
        .get(
          uri,
          headers: {
            'Authorization': ?authorization,
            if (!private) 'X-Birdtie-Map-Media': 'public-moment-photo-v1',
          },
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) {
      throw MapLayersReadFailure(response.statusCode);
    }
    final envelope = _mapObject(jsonDecode(utf8.decode(response.bodyBytes)), {
      'data',
    });
    return TypedMapView.decode(
      envelope['data'],
      city,
      bounds,
      private,
      authorID: authorID,
    );
  }

  void dispose() {
    if (_closed) return;
    _closed = true;
    if (_owns) _client.close();
  }
}
