import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:http/http.dart' as http;

import '../auth/session_vault.dart';
import '../content/moment_image_header.dart';
import 'human_causal_control.dart';
import 'map_entities.dart';
import 'map_media_read_metrics.dart';
import 'map_marker_visual.dart';
import 'now_context_query_api.dart' show onlineStamp;
import 'person_avatar.dart';

const ownedMapVisibilitySources = {
  'CIVU_PUBLIC_CATALOG',
  'CIVU_PUBLIC_MOMENT',
  'SELF_PRIVATE',
  'BIRDTIE_PUBLIC_MOMENT',
};

String _text(Object? value, int max, {bool empty = false}) {
  if (value is! String || !humanControlText(value, empty ? 0 : 1, max)) {
    throw const FormatException('地图文字资料不符');
  }
  return value;
}

class OwnedMapMoment {
  const OwnedMapMoment(
    this.id,
    this.revision,
    this.visibilitySource,
    this.detailPath,
  );
  final String id, visibilitySource, detailPath;
  final int revision;
  bool get selfPrivate => visibilitySource == 'SELF_PRIVATE';
  factory OwnedMapMoment.decode(Object? raw) {
    final v = humanControlObject(raw, {
      'id',
      'revision',
      'visibilitySource',
      'detailPath',
    });
    if (!humanControlID(v['id']) ||
        !humanControlVersion(v['revision'], zero: false) ||
        !const {
          'CIVU_PUBLIC_MOMENT',
          'SELF_PRIVATE',
          'BIRDTIE_PUBLIC_MOMENT',
        }.contains(v['visibilitySource']) ||
        v['detailPath'] != '/v1/me/map-content/moments/${v['id']}') {
      throw const FormatException('地图动态读取来源不符');
    }
    return OwnedMapMoment(
      v['id'],
      v['revision'],
      v['visibilitySource'],
      v['detailPath'],
    );
  }
}

/// A selector for the dedicated current-grant byte reader, never a public URL
/// or permission to call another account's private attachment API.
class OwnedMapCover {
  const OwnedMapCover({
    required this.kind,
    required this.id,
    required this.revision,
    required this.visibilitySource,
    required this.mimeType,
    required this.width,
    required this.height,
    required this.byteCount,
    required this.sha256,
    required this.sourceVersion,
    required this.contentPath,
  });
  final String kind,
      id,
      visibilitySource,
      mimeType,
      sha256,
      sourceVersion,
      contentPath;
  final int revision, width, height, byteCount;
  String get identity => '$kind:$id:$revision:$sourceVersion:$sha256';
  factory OwnedMapCover.decode(Object? raw, String placeID) =>
      OwnedMapCover._decode(raw, placeID: placeID);
  factory OwnedMapCover.poster(Object? raw, String momentID, String mediaID) =>
      OwnedMapCover._decode(raw, momentID: momentID, mediaID: mediaID);
  factory OwnedMapCover.marker(
    Object? raw, {
    required String placeID,
    required String momentID,
  }) => OwnedMapCover._decode(
    raw,
    placeID: placeID.isEmpty ? null : placeID,
    momentID: momentID.isEmpty ? null : momentID,
    marker: true,
  );
  factory OwnedMapCover._decode(
    Object? raw, {
    String? placeID,
    String? momentID,
    String? mediaID,
    bool marker = false,
  }) {
    final v = humanControlObject(raw, {
      'kind',
      'id',
      'revision',
      'visibilitySource',
      'mimeType',
      'width',
      'height',
      'byteCount',
      'sha256',
      'sourceVersion',
      'contentPath',
    });
    if (!const {
          'REFERENCE_IMAGE',
          'MOMENT_IMAGE',
          'MOMENT_VIDEO_POSTER',
        }.contains(v['kind']) ||
        !humanControlID(v['id']) ||
        !humanControlVersion(v['revision'], zero: false) ||
        !ownedMapVisibilitySources.contains(v['visibilitySource']) ||
        !const {'image/jpeg', 'image/png'}.contains(v['mimeType']) ||
        v['width'] is! int ||
        v['height'] is! int ||
        v['width'] < 1 ||
        v['height'] < 1 ||
        v['width'] > 8192 ||
        v['height'] > 8192 ||
        v['width'] * v['height'] > 48 * 1024 * 1024 ||
        v['byteCount'] is! int ||
        v['byteCount'] < 1 ||
        v['byteCount'] > momentImageMaxDerivativeBytes ||
        !humanControlHash(v['sha256']) ||
        !humanControlHash(v['sourceVersion']) ||
        v['contentPath'] is! String) {
      throw const FormatException('地图图片来源不符');
    }
    final path = Uri.tryParse(v['contentPath']);
    if (marker) {
      if (v['mimeType'] != 'image/jpeg' ||
          v['width'] > 160 ||
          v['height'] > 160 ||
          v['byteCount'] > 512 * 1024 ||
          path == null ||
          path.hasScheme ||
          path.hasAuthority ||
          path.hasFragment ||
          path.path !=
              (placeID != null
                  ? '/v1/me/map-content/places/$placeID/images/${v['id']}/content'
                  : '/v1/me/map-content/moments/$momentID/media/${v['id']}/content') ||
          path.queryParametersAll.length != 2 ||
          path.queryParametersAll['sourceVersion']?.length != 1 ||
          path.queryParameters['sourceVersion'] != v['sourceVersion'] ||
          path.queryParametersAll['variant']?.length != 1 ||
          path.queryParameters['variant'] != 'marker' ||
          (placeID == null && !humanControlID(momentID))) {
        throw const FormatException('地图缩略图来源不符');
      }
    } else {
      if (path == null ||
          path.hasScheme ||
          path.hasAuthority ||
          path.hasFragment ||
          path.path !=
              (placeID != null
                  ? '/v1/me/map-content/places/$placeID/images/${v['id']}/content'
                  : '/v1/me/map-content/moments/$momentID/media/$mediaID/content') ||
          path.queryParametersAll.length != (placeID != null ? 1 : 2) ||
          path.queryParametersAll['sourceVersion']?.length != 1 ||
          path.queryParameters['sourceVersion'] != v['sourceVersion'] ||
          (placeID == null &&
              (v['kind'] != 'MOMENT_VIDEO_POSTER' ||
                  v['id'] != mediaID ||
                  path.queryParametersAll['variant']?.length != 1 ||
                  path.queryParameters['variant'] != 'poster'))) {
        throw const FormatException('地图图片路径不符');
      }
    }
    return OwnedMapCover(
      kind: v['kind'],
      id: v['id'],
      revision: v['revision'],
      visibilitySource: v['visibilitySource'],
      mimeType: v['mimeType'],
      width: v['width'],
      height: v['height'],
      byteCount: v['byteCount'],
      sha256: v['sha256'],
      sourceVersion: v['sourceVersion'],
      contentPath: v['contentPath'],
    );
  }
}

class OwnedMapPin {
  const OwnedMapPin({
    required this.placeID,
    required this.name,
    required this.categoryCode,
    required this.cityID,
    required this.latitude,
    required this.longitude,
    required this.visibilitySources,
    required this.sourceVersion,
    required this.momentCount,
    required this.moments,
    required this.momentsTruncated,
    required this.cover,
    this.observedAt,
    this.validUntil,
    this.kind = 'PLACE_CONTENT',
    this.markerID = '',
    this.anchorID = '',
    this.momentID = '',
    this.entryID = '',
    this.threadID = '',
    this.membersPath = '',
    this.bounds,
    this.anchorCount = 1,
    this.precision = 'point',
  });
  final String placeID, name, categoryCode, sourceVersion;
  final String kind,
      markerID,
      anchorID,
      momentID,
      entryID,
      threadID,
      membersPath,
      precision;
  final MapBounds? bounds;
  final String? cityID;
  final double latitude, longitude;
  final List<String> visibilitySources;
  final int momentCount, anchorCount;
  final List<OwnedMapMoment> moments;
  final bool momentsTruncated;
  final OwnedMapCover? cover;
  final DateTime? observedAt, validUntil;
  String get canonicalMarkerID =>
      markerID.isEmpty ? 'place:$placeID' : markerID;
  String get mapID => 'owned-map:$canonicalMarkerID';
  bool get isCluster => kind == 'CLUSTER';
  bool get isMomentAnchor => kind == 'MOMENT_ANCHOR';
  bool get isPlace => kind == 'PLACE_CONTENT';
  bool get hasPrivateContent => visibilitySources.contains('SELF_PRIVATE');
  String get visibilityLabel => hasPrivateContent
      ? (visibilitySources.length == 1 ? '仅本人可见' : '含本人私人内容')
      : '可访问动态';
  MapEntity get entity => MapEntity(
    id: mapID,
    kind: isCluster
        ? MapEntityKind.cluster
        : isMomentAnchor
        ? MapEntityKind.moment
        : MapEntityKind.place,
    title: name,
    subtitle: '$visibilityLabel · $momentCount条动态',
    latitude: latitude,
    longitude: longitude,
    placeID: placeID,
    precision: isCluster ? 'area' : precision,
    sourceVersion: sourceVersion,
    categoryCode: categoryCode,
    anchorID: anchorID,
    momentID: momentID,
    entryID: entryID,
    threadID: threadID,
    contentCount: momentCount,
    anchorCount: anchorCount,
    momentRevision: isMomentAnchor && moments.isNotEmpty
        ? moments.first.revision
        : 0,
    recentMoments: observedAt == null || validUntil == null
        ? null
        : MapRecentMoments.visible(
            count: momentCount,
            checkedAt: observedAt!,
            validUntil: validUntil!,
            sourceVersion: sourceVersion,
          ),
  );
  factory OwnedMapPin.decode(
    Object? raw,
    MapBounds bounds, {
    DateTime? observedAt,
    DateTime? validUntil,
    String cityID = '',
    double zoom = 12,
    String pageSourceVersion = '',
  }) {
    final v = humanControlObject(
      raw,
      {
        'markerId',
        'kind',
        'entityId',
        'anchorCount',
        'placeId',
        'name',
        'categoryCode',
        'cityId',
        'anchor',
        'visibilitySources',
        'sourceVersion',
        'momentCount',
        'moments',
        'momentsTruncated',
        'cover',
      },
      optional: {
        'anchorId',
        'momentId',
        'entryId',
        'threadId',
        'membersPath',
        'bounds',
        'representativeMarkerId',
        'representativePlaceId',
        'representativeMomentId',
      },
    );
    final kind = v['kind'], place = v['placeId'], marker = v['markerId'];
    final anchorID = v['anchorId'] ?? '',
        moment = v['momentId'] ?? '',
        entry = v['entryId'] ?? '',
        thread = v['threadId'] ?? '',
        path = v['membersPath'] ?? '';
    final anchor = humanControlObject(v['anchor'], {
      'coordinateSystem',
      'precision',
      'latitude',
      'longitude',
    });
    final identityValid = switch (kind) {
      'PLACE_CONTENT' =>
        humanControlID(place) &&
            marker == 'place:$place' &&
            v['entityId'] == place &&
            anchorID == '' &&
            moment == '' &&
            entry == '' &&
            thread == '' &&
            path == '' &&
            v['bounds'] == null &&
            v['anchorCount'] == 1,
      'MOMENT_ANCHOR' =>
        place == '' &&
            humanControlID(anchorID) &&
            humanControlID(moment) &&
            marker == 'anchor:$anchorID' &&
            v['entityId'] == moment &&
            entry == moment &&
            (thread == '' || humanControlID(thread)) &&
            path == '' &&
            v['bounds'] == null &&
            v['anchorCount'] == 1,
      'CLUSTER' =>
        place == '' &&
            marker is String &&
            marker.startsWith('cluster:') &&
            humanControlHash(marker.substring(8)) &&
            v['entityId'] == marker &&
            anchorID == '' &&
            moment == '' &&
            entry == '' &&
            thread == '' &&
            v['anchorCount'] is int &&
            v['anchorCount'] > 1,
      _ => false,
    };
    if (!identityValid ||
        !humanControlHash(v['sourceVersion']) ||
        v['visibilitySources'] is! List ||
        (v['visibilitySources'] as List).isEmpty ||
        (v['visibilitySources'] as List).length > 4 ||
        !(v['visibilitySources'] as List).every(
          ownedMapVisibilitySources.contains,
        ) ||
        (v['visibilitySources'] as List).toSet().length !=
            (v['visibilitySources'] as List).length ||
        v['moments'] is! List ||
        (v['moments'] as List).length > 20 ||
        !humanControlVersion(v['momentCount'], zero: false) ||
        v['momentsTruncated'] is! bool ||
        anchor['coordinateSystem'] != 'wgs84' ||
        !const {'point', 'approximate'}.contains(anchor['precision']) ||
        anchor['latitude'] is! num ||
        anchor['longitude'] is! num) {
      throw const FormatException('地图内容来源或权限不符');
    }
    final lat = (anchor['latitude'] as num).toDouble(),
        lon = (anchor['longitude'] as num).toDouble();
    if (!lat.isFinite ||
        !lon.isFinite ||
        lat < -90 ||
        lat > 90 ||
        lon < -180 ||
        lon > 180 ||
        !bounds.contains(lat, lon))
      throw const FormatException('地图位置超出当前范围');
    MapBounds? clusterBounds;
    if (kind == 'CLUSTER') {
      final b = humanControlObject(v['bounds'], {
        'west',
        'east',
        'south',
        'north',
      });
      if (b.values.any((v) => v is! num)) {
        throw const FormatException('地图聚合范围不符');
      }
      clusterBounds = MapBounds(
        west: (b['west'] as num).toDouble(),
        east: (b['east'] as num).toDouble(),
        south: (b['south'] as num).toDouble(),
        north: (b['north'] as num).toDouble(),
      );
      if (!clusterBounds.isValid ||
          !clusterBounds.contains(lat, lon) ||
          clusterBounds.longitudeSpan > bounds.longitudeSpan + 0.000003 ||
          clusterBounds.south < bounds.south - 0.000002 ||
          clusterBounds.north > bounds.north + 0.000002)
        throw const FormatException('地图聚合范围超出视口');
      final uri = path is String ? Uri.tryParse(path) : null;
      final query = uri?.queryParameters;
      if (uri == null ||
          uri.hasScheme ||
          uri.hasAuthority ||
          uri.hasFragment ||
          uri.path != '/v1/me/map-content' ||
          uri.queryParametersAll.values.any((v) => v.length != 1) ||
          !uri.queryParameters.keys.every(
            const {
              'cityId',
              'west',
              'south',
              'east',
              'north',
              'zoom',
              'filterScope',
              'clusterId',
              'sourceVersion',
              'selectedMarkerId',
            }.contains,
          ) ||
          (query?['cityId'] ?? '') != cityID ||
          query?['clusterId'] != marker ||
          query?['sourceVersion'] != pageSourceVersion ||
          (query?['filterScope'] ?? 'DEFAULT_CONTENT') != 'DEFAULT_CONTENT' ||
          double.tryParse(query?['zoom'] ?? '') != zoom ||
          bounds.toJson().entries.any(
            (e) => double.tryParse(query?[e.key] ?? '') != e.value,
          )) {
        throw const FormatException('地图聚合读取路径不符');
      }
    }
    final moments = (v['moments'] as List).map(OwnedMapMoment.decode).toList();
    final representativePlace = v['representativePlaceId'] ?? '';
    final representativeMoment = v['representativeMomentId'] ?? '';
    final representativeMarker = v['representativeMarkerId'] ?? '';
    if (kind == 'CLUSTER' &&
        v['cover'] != null &&
        !((humanControlID(representativePlace) &&
                representativeMoment == '' &&
                representativeMarker == 'place:$representativePlace') ||
            (humanControlID(representativeMoment) &&
                representativePlace == '' &&
                representativeMarker is String &&
                representativeMarker.startsWith('anchor:') &&
                humanControlID(representativeMarker.substring(7))))) {
      throw const FormatException('同位置封面来源不符');
    }
    final cover = v['cover'] == null
        ? null
        : OwnedMapCover.marker(
            v['cover'],
            placeID: kind == 'CLUSTER' ? representativePlace : place,
            momentID: kind == 'CLUSTER' ? representativeMoment : moment,
          );
    if (moments.map((m) => m.id).toSet().length != moments.length ||
        v['momentCount'] < moments.length ||
        v['momentsTruncated'] != (v['momentCount'] > moments.length) ||
        moments.any(
          (m) => !(v['visibilitySources'] as List).contains(m.visibilitySource),
        ) ||
        kind == 'MOMENT_ANCHOR' &&
            (v['momentCount'] != 1 ||
                moments.length != 1 ||
                moments.single.id != moment) ||
        cover != null &&
            !(v['visibilitySources'] as List).contains(
              cover.visibilitySource,
            )) {
      throw const FormatException('地图动态数量不符');
    }
    return OwnedMapPin(
      placeID: place,
      markerID: marker,
      kind: kind,
      anchorID: anchorID,
      momentID: moment,
      entryID: entry,
      threadID: thread,
      membersPath: path,
      bounds: clusterBounds,
      anchorCount: v['anchorCount'],
      precision: anchor['precision'],
      name: _text(v['name'], 16000, empty: kind == 'MOMENT_ANCHOR'),
      categoryCode: _text(v['categoryCode'], 160, empty: true),
      cityID: v['cityId'] == null ? null : _text(v['cityId'], 200, empty: true),
      latitude: lat,
      longitude: lon,
      visibilitySources: List<String>.unmodifiable(v['visibilitySources']),
      sourceVersion: v['sourceVersion'],
      momentCount: v['momentCount'],
      moments: List.unmodifiable(moments),
      momentsTruncated: v['momentsTruncated'],
      cover: cover,
      observedAt: observedAt,
      validUntil: validUntil,
    );
  }
}

class OwnedMapView {
  const OwnedMapView({
    required this.viewerAccountID,
    required this.cityID,
    required this.bounds,
    required this.observedAt,
    required this.validUntil,
    required this.sourceVersion,
    required this.items,
    required this.truncated,
    this.zoom = 12,
    this.filterScope = 'DEFAULT_CONTENT',
    this.totalContentCount = 0,
    this.totalAnchorCount = 0,
    this.nextCursor,
    this.viewportPath = '',
    this.totalPointCount = 0,
    this.byteCount = 0,
    this.rejectedItemCount = 0,
  });
  final String viewerAccountID, cityID, sourceVersion, filterScope;
  final MapBounds bounds;
  final double zoom;
  final int totalContentCount, totalAnchorCount, totalPointCount, byteCount;

  /// Local diagnostics only; no rejected payload or private field is retained.
  final int rejectedItemCount;
  final String viewportPath;
  final String? nextCursor;
  final DateTime observedAt, validUntil;
  final List<OwnedMapPin> items;
  final bool truncated;
  factory OwnedMapView.decode(
    Object? raw, {
    required String ownerID,
    required String cityID,
    required MapBounds bounds,
    required DateTime now,
    double zoom = 12,
    int byteCount = 0,
    bool memberPage = false,
  }) {
    final v = humanControlObject(
      raw,
      {
        'schemaVersion',
        'scope',
        'viewerAccountId',
        'cityId',
        'bounds',
        'observedAt',
        'validUntil',
        'sourceVersion',
        'pins',
        'truncated',
        'zoom',
        'filterScope',
        'totalContentCount',
        'totalAnchorCount',
        'nextCursor',
      },
      optional: {'viewportPath', 'totalPointCount'},
    );
    final b = humanControlObject(v['bounds'], {
      'west',
      'south',
      'east',
      'north',
    });
    if (v['schemaVersion'] != 'map-content-v2' ||
        v['scope'] != 'MAP_CONTENT' ||
        !humanControlID(ownerID) ||
        v['viewerAccountId'] != ownerID ||
        v['cityId'] != cityID ||
        b['west'] != bounds.west ||
        b['south'] != bounds.south ||
        b['east'] != bounds.east ||
        b['north'] != bounds.north ||
        v['zoom'] != zoom ||
        v['filterScope'] != 'DEFAULT_CONTENT' ||
        !humanControlHash(v['sourceVersion']) ||
        v['pins'] is! List ||
        (v['pins'] as List).length > 40 ||
        v['truncated'] is! bool ||
        !humanControlVersion(v['totalContentCount']) ||
        !humanControlVersion(v['totalAnchorCount']) ||
        v['totalAnchorCount'] < (v['pins'] as List).length ||
        v['nextCursor'] != null &&
            !humanControlText(v['nextCursor'], 1, 4096)) {
      throw const FormatException('地图读取身份或范围不符');
    }
    final lease = _lease(v, now);
    final viewportPath = v['viewportPath'] ?? '';
    if (viewportPath != '') {
      final uri = viewportPath is String ? Uri.tryParse(viewportPath) : null;
      if (uri == null ||
          uri.hasScheme ||
          uri.hasAuthority ||
          uri.hasFragment ||
          uri.path != '/v1/me/map-content' ||
          uri.queryParametersAll.values.any((v) => v.length != 1) ||
          !uri.queryParameters.keys.every(
            const {
              'cityId',
              'west',
              'east',
              'south',
              'north',
              'zoom',
              'filterScope',
              'sourceVersion',
              'selectedMarkerId',
            }.contains,
          ) ||
          (uri.queryParameters['cityId'] ?? '') != cityID ||
          uri.queryParameters['sourceVersion'] != v['sourceVersion'] ||
          uri.queryParameters['filterScope'] != 'DEFAULT_CONTENT' ||
          double.tryParse(uri.queryParameters['zoom'] ?? '') != zoom ||
          bounds.toJson().entries.any(
            (e) => double.tryParse(uri.queryParameters[e.key] ?? '') != e.value,
          )) {
        throw const FormatException('地图分组读取路径不符');
      }
    }
    if (v['totalPointCount'] != null &&
        (!humanControlVersion(v['totalPointCount']) ||
            !memberPage && v['totalPointCount'] < (v['pins'] as List).length)) {
      throw const FormatException('地图位置数量不符');
    }
    final items = <OwnedMapPin>[];
    var rejected = 0;
    for (final raw in v['pins'] as List) {
      try {
        items.add(
          OwnedMapPin.decode(
            raw,
            bounds,
            observedAt: lease.$1,
            validUntil: lease.$2,
            cityID: cityID,
            zoom: zoom,
            pageSourceVersion: v['sourceVersion'],
          ),
        );
      } on FormatException {
        // All envelope identity, permission, range and lease checks above stay
        // fail closed. A rejected row grants nothing and cannot hide valid rows.
        rejected++;
      }
    }
    if (items.isEmpty && rejected > 0) {
      throw const FormatException('地图内容全部无法解析，请重新读取');
    }
    if (items.map((v) => v.mapID).toSet().length != items.length ||
        items.any(
          (p) =>
              p.momentCount > v['totalContentCount'] ||
              p.anchorCount > v['totalAnchorCount'],
        ) ||
        items.isEmpty && v['nextCursor'] != null)
      throw const FormatException('地图内容重复或统计不符');
    return OwnedMapView(
      viewerAccountID: ownerID,
      cityID: cityID,
      bounds: bounds,
      observedAt: lease.$1,
      validUntil: lease.$2,
      sourceVersion: v['sourceVersion'],
      items: List.unmodifiable(items),
      truncated: v['truncated'],
      zoom: zoom,
      filterScope: v['filterScope'],
      totalContentCount: v['totalContentCount'],
      totalAnchorCount: v['totalAnchorCount'],
      nextCursor: v['nextCursor'],
      viewportPath: viewportPath,
      totalPointCount: v['totalPointCount'] ?? v['totalAnchorCount'],
      byteCount: byteCount,
      rejectedItemCount: rejected,
    );
  }
}

String _authoredText(Object? value, int maxBytes) {
  if (value is! String ||
      value.contains('\u0000') ||
      utf8.encode(value).length > maxBytes) {
    throw const FormatException('地图动态文字不符');
  }
  return value;
}

(DateTime, DateTime) _lease(Map<String, dynamic> v, DateTime now) {
  final at = onlineStamp(v['observedAt']), until = onlineStamp(v['validUntil']);
  if (at.isAfter(now.add(const Duration(seconds: 2))) ||
      !until.isAfter(now) ||
      !until.isAfter(at) ||
      until.difference(at) > const Duration(seconds: 30) ||
      until.isAfter(now.add(const Duration(seconds: 32)))) {
    throw const FormatException('地图读取期限不符');
  }
  return (at, until);
}

class OwnedMapLocation {
  const OwnedMapLocation(
    this.placeID,
    this.name,
    this.latitude,
    this.longitude,
  );
  final String placeID, name;
  final double? latitude, longitude;
  factory OwnedMapLocation.decode(Object? raw) {
    final v = humanControlObject(raw, {'placeId', 'name', 'anchor'});
    if (!humanControlID(v['placeId'])) throw const FormatException('动态地点不符');
    double? lat, lon;
    if (v['anchor'] != null) {
      final a = humanControlObject(v['anchor'], {
        'coordinateSystem',
        'precision',
        'latitude',
        'longitude',
      });
      if (a['coordinateSystem'] != 'wgs84' ||
          a['precision'] != 'point' ||
          a['latitude'] is! num ||
          a['longitude'] is! num)
        throw const FormatException('动态位置不符');
      lat = (a['latitude'] as num).toDouble();
      lon = (a['longitude'] as num).toDouble();
      if (!lat.isFinite || !lon.isFinite || lat.abs() > 90 || lon.abs() > 180) {
        throw const FormatException('动态位置不符');
      }
    }
    return OwnedMapLocation(v['placeId'], _text(v['name'], 1000), lat, lon);
  }
}

class OwnedMapMomentMedia {
  const OwnedMapMomentMedia({
    required this.kind,
    required this.id,
    required this.revision,
    required this.mimeType,
    required this.width,
    required this.height,
    required this.byteCount,
    required this.sha256,
    required this.sourceVersion,
    required this.contentPath,
    required this.durationMs,
    required this.videoCodec,
    required this.audioCodec,
    required this.poster,
    this.displayDescriptorPath,
  });
  final String kind, id, mimeType, sha256, sourceVersion, contentPath;
  final String? displayDescriptorPath;
  final int revision, width, height, byteCount;
  final int? durationMs;
  final String? videoCodec, audioCodec;
  final OwnedMapCover? poster;
  bool get isVideo => kind == 'VIDEO';
  String get identity => '$kind:$id:$revision:$sourceVersion:$sha256';
  factory OwnedMapMomentMedia.decode(
    Object? raw,
    String momentID,
    String visibilitySource,
  ) {
    final v = humanControlObject(
      raw,
      {
        'kind',
        'id',
        'revision',
        'mimeType',
        'width',
        'height',
        'byteCount',
        'sha256',
        'sourceVersion',
        'contentPath',
        'durationMs',
        'videoCodec',
        'audioCodec',
        'poster',
      },
      optional: {'displayDescriptorPath'},
    );
    final video = v['kind'] == 'VIDEO';
    if (!const {'IMAGE', 'VIDEO'}.contains(v['kind']) ||
        !humanControlID(v['id']) ||
        !humanControlVersion(v['revision'], zero: false) ||
        v['width'] is! int ||
        v['height'] is! int ||
        v['width'] < 1 ||
        v['height'] < 1 ||
        v['width'] > 8192 ||
        v['height'] > 8192 ||
        v['width'] * v['height'] > 48 * 1024 * 1024 ||
        v['byteCount'] is! int ||
        v['byteCount'] < 1 ||
        v['byteCount'] >
            (video ? 64 * 1024 * 1024 : momentImageMaxDerivativeBytes) ||
        !humanControlHash(v['sha256']) ||
        !humanControlHash(v['sourceVersion']) ||
        v['contentPath'] is! String ||
        (video
            ? v['mimeType'] != 'video/mp4' ||
                  v['durationMs'] is! int ||
                  v['durationMs'] < 1 ||
                  v['durationMs'] > 120000 ||
                  v['videoCodec'] != 'h264' ||
                  !const {'aac', 'none'}.contains(v['audioCodec'])
            : !const {'image/jpeg', 'image/png'}.contains(v['mimeType']) ||
                  v['durationMs'] != null ||
                  v['videoCodec'] != null ||
                  v['audioCodec'] != null ||
                  v['poster'] != null)) {
      throw const FormatException('地图动态媒体不符');
    }
    final path = Uri.tryParse(v['contentPath']);
    final display = path?.queryParameters['variant'] == 'display';
    final descriptor = v['displayDescriptorPath'];
    if (descriptor != null &&
        (video ||
            display ||
            descriptor !=
                '/v1/me/map-content/moments/$momentID/media/${v['id']}/display-descriptor?sourceVersion=${v['sourceVersion']}')) {
      throw const FormatException('地图图片阅读来源不符');
    }
    if (path == null ||
        path.hasScheme ||
        path.hasAuthority ||
        path.hasFragment ||
        path.path !=
            '/v1/me/map-content/moments/$momentID/media/${v['id']}/content' ||
        path.queryParametersAll.length != (display ? 2 : 1) ||
        display &&
            (video ||
                path.queryParametersAll['variant']?.length != 1 ||
                v['mimeType'] != 'image/jpeg' ||
                v['width'] > 1280 ||
                v['height'] > 1280 ||
                v['byteCount'] > 256 * 1024) ||
        path.queryParametersAll['sourceVersion']?.length != 1 ||
        path.queryParameters['sourceVersion'] != v['sourceVersion'])
      throw const FormatException('地图动态媒体路径不符');
    final poster = v['poster'] == null
        ? null
        : OwnedMapCover.poster(v['poster'], momentID, v['id']);
    if (poster != null && poster.visibilitySource != visibilitySource)
      throw const FormatException('视频封面权限来源不符');
    return OwnedMapMomentMedia(
      kind: v['kind'],
      id: v['id'],
      revision: v['revision'],
      mimeType: v['mimeType'],
      width: v['width'],
      height: v['height'],
      byteCount: v['byteCount'],
      sha256: v['sha256'],
      sourceVersion: v['sourceVersion'],
      contentPath: v['contentPath'],
      durationMs: v['durationMs'],
      videoCodec: v['videoCodec'],
      audioCodec: v['audioCodec'],
      poster: poster,
      displayDescriptorPath: descriptor,
    );
  }
}

/// Exact bytes for one selected image. A descriptor is not a renewed grant:
/// the caller must keep the original detail and identity current throughout.
class OwnedMapDisplayDescriptor {
  const OwnedMapDisplayDescriptor(this.media, this.validUntil);
  final OwnedMapMomentMedia media;
  final DateTime validUntil;

  factory OwnedMapDisplayDescriptor.decode(
    Object? raw, {
    required OwnedMapMomentDetail detail,
    required OwnedMapMomentMedia original,
    required DateTime now,
  }) {
    final v = humanControlObject(raw, {
      'schemaVersion',
      'viewerAccountId',
      'momentId',
      'originalSourceVersion',
      'observedAt',
      'validUntil',
      'media',
    });
    if (v['schemaVersion'] != 'map-content-media-display-v1' ||
        v['viewerAccountId'] != detail.viewerAccountID ||
        v['momentId'] != detail.id ||
        v['originalSourceVersion'] != original.sourceVersion ||
        original.isVideo ||
        original.displayDescriptorPath == null ||
        !detail.media.any((item) => identical(item, original)) ||
        !detail.validUntil.isAfter(now)) {
      throw const FormatException('图片阅读描述已失效');
    }
    final lease = _lease(v, now);
    final media = OwnedMapMomentMedia.decode(
      v['media'],
      detail.id,
      detail.visibilitySource,
    );
    final expectedSource = sha256
        .convert(
          utf8.encode(jsonEncode([original.sourceVersion, 'display1280-v1'])),
        )
        .toString();
    if (media.isVideo ||
        media.id != original.id ||
        media.revision != original.revision ||
        media.sourceVersion != expectedSource ||
        media.displayDescriptorPath != null ||
        media.contentPath !=
            '/v1/me/map-content/moments/${detail.id}/media/${original.id}/content?sourceVersion=$expectedSource&variant=display' ||
        media.width > original.width ||
        media.height > original.height ||
        // The byte-budget fallback can perform several integer resamplings.
        (media.width * original.height - media.height * original.width).abs() >
            4 * (original.width + original.height)) {
      throw const FormatException('图片阅读描述来源不符');
    }
    return OwnedMapDisplayDescriptor(
      media,
      lease.$2.isBefore(detail.validUntil) ? lease.$2 : detail.validUntil,
    );
  }
}

class OwnedMapMomentAuthor {
  const OwnedMapMomentAuthor({
    required this.accountID,
    required this.displayName,
    this.avatar,
  });
  final String accountID, displayName;
  final PersonAvatarMetadata? avatar;
  factory OwnedMapMomentAuthor.decode(Object? raw, {required String momentID}) {
    final value = humanControlObject(raw, {
      'accountId',
      'displayName',
      'avatar',
    });
    if (!humanControlID(value['accountId'])) {
      throw const FormatException('动态作者资料不符');
    }
    return OwnedMapMomentAuthor(
      accountID: value['accountId'],
      displayName: _text(value['displayName'], 160, empty: true),
      avatar: _momentAuthorAvatar(
        value['avatar'],
        value['accountId'],
        momentID,
      ),
    );
  }
}

PersonAvatarMetadata? _momentAuthorAvatar(
  Object? raw,
  String accountID,
  String momentID,
) {
  if (raw == null) return null;
  final value = humanControlObject(raw, {
    'accountId',
    'version',
    'thumbnailPath',
    'width',
    'height',
    'byteCount',
    'sha256',
  });
  final path = value['thumbnailPath'];
  if (path is! String || !path.startsWith('/v1/me/map-content/moments/')) {
    return PersonAvatarMetadata.fromJson(raw, accountID);
  }
  final match = RegExp(
    '^/v1/me/map-content/moments/${RegExp.escape(momentID)}'
    r'/author-avatar/([0-9a-f-]{36})/content\?sourceVersion=([0-9a-f]{64})$',
  ).firstMatch(path);
  if (momentID.length != 36 ||
      !personAvatarID(momentID) ||
      accountID.length != 36 ||
      !personAvatarID(accountID) ||
      value['accountId'] != accountID ||
      !humanControlVersion(value['version'], zero: false) ||
      value['width'] != 128 ||
      value['height'] != 128 ||
      value['byteCount'] is! int ||
      (value['byteCount'] as int) < 1 ||
      (value['byteCount'] as int) > personAvatarMaxThumbnailBytes ||
      !humanControlHash(value['sha256']) ||
      (value['sha256'] as String).length != 64 ||
      match == null ||
      match.end != path.length ||
      !personAvatarID(match.group(1)!)) {
    throw const FormatException('动态作者头像来源无法核实');
  }
  return PersonAvatarMetadata(
    accountID: accountID,
    version: value['version'],
    thumbnailPath: path,
    byteCount: value['byteCount'],
    digest: value['sha256'],
  );
}

class OwnedMapMomentDetail {
  OwnedMapMomentDetail({
    required this.viewerAccountID,
    required this.id,
    required this.revision,
    required this.visibilitySource,
    required this.canEdit,
    required this.status,
    required this.visibility,
    required this.title,
    required this.body,
    required this.createdAt,
    required this.locations,
    required this.media,
    required this.observedAt,
    required this.validUntil,
    required this.sourceVersion,
    this.author,
    this.publishedAt,
    this.createdAtSource = 'UNKNOWN',
  });
  final String viewerAccountID,
      id,
      visibilitySource,
      status,
      visibility,
      title,
      body,
      sourceVersion;
  final int revision;
  final bool canEdit;
  final DateTime createdAt, observedAt, validUntil;
  final List<OwnedMapLocation> locations;
  final List<OwnedMapMomentMedia> media;
  final OwnedMapMomentAuthor? author;
  final DateTime? publishedAt;
  final String createdAtSource;
  DateTime? get displayPublishedAt =>
      publishedAt ??
      (const {'NATIVE', 'CIVU_ORIGINAL'}.contains(createdAtSource)
          ? createdAt
          : null);
  late final OwnedMapMoment selector = OwnedMapMoment(
    id,
    revision,
    visibilitySource,
    '/v1/me/map-content/moments/$id',
  );
  factory OwnedMapMomentDetail.decode(
    Object? raw, {
    required String ownerID,
    required DateTime now,
  }) {
    final v = humanControlObject(
      raw,
      {
        'schemaVersion',
        'viewerAccountId',
        'id',
        'revision',
        'visibilitySource',
        'canEdit',
        'status',
        'visibility',
        'title',
        'body',
        'createdAt',
        'locations',
        'media',
        'observedAt',
        'validUntil',
        'sourceVersion',
      },
      optional: {'author', 'publishedAt', 'createdAtSource'},
    );
    if (v['schemaVersion'] != 'map-content-moment-v1' ||
        !humanControlID(ownerID) ||
        v['viewerAccountId'] != ownerID ||
        !humanControlID(v['id']) ||
        !humanControlVersion(v['revision'], zero: false) ||
        !const {
          'SELF_PRIVATE',
          'CIVU_PUBLIC_MOMENT',
          'BIRDTIE_PUBLIC_MOMENT',
        }.contains(v['visibilitySource']) ||
        v['canEdit'] is! bool ||
        !const {'draft', 'published'}.contains(v['status']) ||
        !const {'private', 'public'}.contains(v['visibility']) ||
        !humanControlHash(v['sourceVersion']) ||
        v['locations'] is! List ||
        (v['locations'] as List).length > 100 ||
        v['media'] is! List ||
        (v['media'] as List).length > 200) {
      throw const FormatException('地图动态读取身份或来源不符');
    }
    if (v['createdAtSource'] != null &&
        !const {
          'NATIVE',
          'CIVU_ORIGINAL',
          'UNKNOWN',
        }.contains(v['createdAtSource'])) {
      throw const FormatException('动态时间来源不符');
    }
    final lease = _lease(v, now);
    final locations = (v['locations'] as List)
        .map(OwnedMapLocation.decode)
        .toList();
    final media = (v['media'] as List)
        .map(
          (m) => OwnedMapMomentMedia.decode(m, v['id'], v['visibilitySource']),
        )
        .toList();
    if (locations.map((l) => l.placeID).toSet().length != locations.length ||
        media.map((m) => m.id).toSet().length != media.length)
      throw const FormatException('地图动态内容重复');
    return OwnedMapMomentDetail(
      viewerAccountID: ownerID,
      id: v['id'],
      revision: v['revision'],
      visibilitySource: v['visibilitySource'],
      canEdit: v['canEdit'],
      status: v['status'],
      visibility: v['visibility'],
      title: _authoredText(v['title'], 16384),
      body: _authoredText(v['body'], 1024 * 1024),
      createdAt: onlineStamp(v['createdAt']),
      locations: List.unmodifiable(locations),
      media: List.unmodifiable(media),
      observedAt: lease.$1,
      validUntil: lease.$2,
      sourceVersion: v['sourceVersion'],
      author: v['author'] == null
          ? null
          : OwnedMapMomentAuthor.decode(v['author'], momentID: v['id']),
      publishedAt: v['publishedAt'] == null
          ? null
          : onlineStamp(v['publishedAt']),
      createdAtSource: v['createdAtSource'] ?? 'UNKNOWN',
    );
  }
}

class OwnedMapPlacePage {
  const OwnedMapPlacePage({
    required this.placeID,
    required this.items,
    required this.nextCursor,
    required this.observedAt,
    required this.validUntil,
    required this.sourceVersion,
  });
  final String placeID, sourceVersion;
  final String? nextCursor;
  final List<OwnedMapMomentDetail> items;
  final DateTime observedAt, validUntil;
  factory OwnedMapPlacePage.decode(
    Object? raw, {
    required String ownerID,
    required String placeID,
    required DateTime now,
  }) {
    final v = humanControlObject(raw, {
      'schemaVersion',
      'viewerAccountId',
      'placeId',
      'items',
      'observedAt',
      'validUntil',
      'sourceVersion',
      'nextCursor',
    });
    if (v['schemaVersion'] != 'map-content-place-moments-v1' ||
        v['viewerAccountId'] != ownerID ||
        !humanControlID(ownerID) ||
        v['placeId'] != placeID ||
        !humanControlID(placeID) ||
        !humanControlHash(v['sourceVersion']) ||
        v['items'] is! List ||
        (v['items'] as List).length > 20 ||
        v['nextCursor'] != null && !humanControlText(v['nextCursor'], 1, 4096))
      throw const FormatException('地点动态分页来源不符');
    final lease = _lease(v, now);
    final items = (v['items'] as List)
        .map((m) => OwnedMapMomentDetail.decode(m, ownerID: ownerID, now: now))
        .toList();
    if (items.map((m) => m.id).toSet().length != items.length ||
        items.any((m) => m.validUntil.isAfter(lease.$2)) ||
        items.isEmpty && v['nextCursor'] != null) {
      throw const FormatException('地点动态分页期限或内容不符');
    }
    return OwnedMapPlacePage(
      placeID: placeID,
      items: List.unmodifiable(items),
      nextCursor: v['nextCursor'],
      observedAt: lease.$1,
      validUntil: lease.$2,
      sourceVersion: v['sourceVersion'],
    );
  }
}

class OwnedMapReadFailure implements Exception {
  const OwnedMapReadFailure(
    this.statusCode, {
    this.code = '',
    this.resourceKind,
    this.resourceID,
  });
  final int statusCode;
  final String code;
  final String? resourceKind, resourceID;
  bool get retiresGrant => const {401, 403, 404, 409, 410}.contains(statusCode);
  bool get resourceScoped =>
      resourceID != null &&
      (statusCode == 404 && code == 'not_found' ||
          statusCode == 409 && code == 'moment_changed');
  bool get retryable => statusCode >= 500 && statusCode <= 599;
}

class OwnedMapThreadEntry {
  const OwnedMapThreadEntry(this.position, this.detail);
  final int position;
  final OwnedMapMomentDetail? detail;
  bool get available => detail != null;
}

class OwnedMapThread {
  const OwnedMapThread({
    required this.threadID,
    required this.seedMomentID,
    required this.seedRevision,
    required this.seedVisibilitySource,
    required this.entries,
    required this.observedAt,
    required this.validUntil,
    required this.sourceVersion,
  });
  final String threadID, seedMomentID, seedVisibilitySource, sourceVersion;
  final int seedRevision;
  final List<OwnedMapThreadEntry> entries;
  final DateTime observedAt, validUntil;
  bool get current => validUntil.isAfter(DateTime.now().toUtc());
  bool get hasThread => threadID.isNotEmpty;

  factory OwnedMapThread.decode(
    Object? raw, {
    required String ownerID,
    required String seedMomentID,
    required DateTime now,
  }) {
    final v = humanControlObject(raw, {
      'schemaVersion',
      'viewerAccountId',
      'seedMomentId',
      'seedRevision',
      'seedVisibilitySource',
      'status',
      'threadId',
      'entries',
      'observedAt',
      'validUntil',
      'sourceVersion',
    });
    if (v['schemaVersion'] != 'map-content-thread-v1' ||
        v['viewerAccountId'] != ownerID ||
        !humanControlID(ownerID) ||
        v['seedMomentId'] != seedMomentID ||
        !humanControlID(seedMomentID) ||
        !humanControlVersion(v['seedRevision'], zero: false) ||
        !const {
          'SELF_PRIVATE',
          'CIVU_PUBLIC_MOMENT',
          'BIRDTIE_PUBLIC_MOMENT',
        }.contains(v['seedVisibilitySource']) ||
        v['entries'] is! List ||
        (v['status'] == 'NOT_A_THREAD'
            ? v['threadId'] != '' || (v['entries'] as List).isNotEmpty
            : v['status'] != 'AVAILABLE' ||
                  !humanControlID(v['threadId']) ||
                  (v['entries'] as List).isEmpty ||
                  (v['entries'] as List).length > 20)) {
      throw const FormatException('地图串文来源不符');
    }
    final lease = _lease(v, now), entries = <OwnedMapThreadEntry>[];
    final seen = <String>{};
    var seedFound = false;
    for (final rawEntry in v['entries'] as List) {
      final e = humanControlObject(rawEntry, {'position', 'status', 'detail'});
      if (e['position'] != entries.length ||
          !const {'AVAILABLE', 'UNAVAILABLE'}.contains(e['status']) ||
          (e['status'] == 'UNAVAILABLE') != (e['detail'] == null)) {
        throw const FormatException('地图串文段序不符');
      }
      final detail = e['detail'] == null
          ? null
          : OwnedMapMomentDetail.decode(
              e['detail'],
              ownerID: ownerID,
              now: now,
            );
      if (detail != null) {
        if (!seen.add(detail.id) || detail.validUntil.isBefore(lease.$2)) {
          throw const FormatException('地图串文内容已变化');
        }
        if (detail.id == seedMomentID) {
          seedFound =
              detail.revision == v['seedRevision'] &&
              detail.visibilitySource == v['seedVisibilitySource'];
        }
      }
      entries.add(OwnedMapThreadEntry(entries.length, detail));
    }
    if (v['status'] == 'AVAILABLE' && !seedFound) {
      throw const FormatException('地图串文入口已变化');
    }
    return OwnedMapThread(
      threadID: v['threadId'],
      seedMomentID: seedMomentID,
      seedRevision: v['seedRevision'],
      seedVisibilitySource: v['seedVisibilitySource'],
      entries: List.unmodifiable(entries),
      observedAt: lease.$1,
      validUntil: lease.$2,
      sourceVersion: v['sourceVersion'],
    );
  }
}

class OwnedMapAPI {
  OwnedMapAPI({
    required http.Client client,
    required this.apiBaseUrl,
    this.onDenied,
  }) : _client = client;
  http.Client _client;
  final String Function() apiBaseUrl;
  final void Function(OwnedMapReadFailure failure)? onDenied;
  bool _closed = false;
  int _transport = 0;
  final _contentDeadlines = Expando<DateTime>();
  final _contentTraces = Expando<MapMediaReadTrace>();
  DateTime? contentValidUntil(Uint8List bytes) => _contentDeadlines[bytes];
  MapMediaReadTrace? contentTrace(Uint8List bytes) => _contentTraces[bytes];
  void replaceTransport({required http.Client client}) {
    ++_transport;
    _client = client;
  }

  Future<Uint8List> _read(
    Uri uri, {
    required String authorization,
    required bool Function() current,
    required int limit,
    String? mime,
    int? exact,
    Future<void>? abortTrigger,
    bool allowMissing = false,
    MapMediaReadTrace? trace,
    Future<void> Function(Uint8List bytes)? receive,
  }) async {
    final transport = _transport, client = _client;
    bool valid() =>
        !_closed &&
        transport == _transport &&
        current() &&
        canonicalAuthOrigin(apiBaseUrl()) == uri.origin;
    if (!valid()) throw const FormatException('地图读取已失效');
    final request = http.AbortableRequest(
      'GET',
      uri,
      abortTrigger: abortTrigger,
    )..followRedirects = false;
    request.headers['Authorization'] = authorization;
    if (trace != null) trace.requests++;
    final response = await client
        .send(request)
        .timeout(const Duration(seconds: 12));
    trace?.mark(MapMediaReadStage.headers);
    if (response.statusCode != 200) {
      final code = await _errorCode(response, allowMissingRoute: allowMissing);
      final target = RegExp(
        r'^/v1/me/map-content/(moments|places)/([0-9a-f-]{36})(?:/|$)',
      ).firstMatch(uri.path);
      final failure = OwnedMapReadFailure(
        response.statusCode,
        code: code,
        resourceKind: target?.group(1),
        resourceID: target?.group(2),
      );
      if (valid() &&
          failure.retiresGrant &&
          !(allowMissing && code == 'route_not_found')) {
        onDenied?.call(failure);
      }
      throw failure;
    }
    if (!valid() ||
        response.statusCode != 200 ||
        !response.headers['cache-control'].toString().toLowerCase().contains(
          'no-store',
        ) ||
        (mime != null &&
            response.headers['content-type']?.split(';').first != mime) ||
        (mime != null &&
            mime != 'application/json' &&
            (response.headers['x-content-type-options'] != 'nosniff' ||
                response.contentLength != exact)) ||
        (response.contentLength != null &&
            (response.contentLength! > limit ||
                exact != null && response.contentLength != exact))) {
      await response.stream.listen((_) {}).cancel();
      throw const FormatException('地图内容暂不可读');
    }
    final chunks = <Uint8List>[];
    final reader = StreamIterator(response.stream);
    var count = 0;
    final deadline = Stopwatch()..start();
    try {
      while (await reader.moveNext().timeout(const Duration(seconds: 10))) {
        if (!valid() ||
            deadline.elapsed > const Duration(seconds: 20) ||
            count + reader.current.length > limit) {
          throw const FormatException('地图读取已失效或超限');
        }
        final chunk = Uint8List.fromList(reader.current);
        count += chunk.length;
        if (chunk.isNotEmpty) trace?.mark(MapMediaReadStage.firstByte);
        if (trace != null) trace.bytes = count;
        if (receive == null) {
          chunks.add(chunk);
        } else {
          try {
            await receive(chunk);
          } finally {
            chunk.fillRange(0, chunk.length, 0);
          }
        }
      }
      if (!valid() || exact != null && count != exact)
        throw const FormatException('地图内容大小不符');
      trace?.mark(MapMediaReadStage.downloaded);
      if (receive != null) return Uint8List(0);
      final bytes = Uint8List(count);
      var offset = 0;
      for (final chunk in chunks) {
        bytes.setRange(offset, offset + chunk.length, chunk);
        offset += chunk.length;
      }
      return bytes;
    } finally {
      await reader.cancel();
      for (final chunk in chunks) {
        chunk.fillRange(0, chunk.length, 0);
      }
    }
  }

  /// Only the existing error code is inspected, never logged or retained as a
  /// response body. Unknown identity/permission failures remain global.
  Future<String> _errorCode(
    http.StreamedResponse response, {
    bool allowMissingRoute = false,
  }) async {
    final plainMissing =
        allowMissingRoute &&
        response.statusCode == 404 &&
        response.headers['content-type']?.split(';').first == 'text/plain';
    if (!const {404, 409}.contains(response.statusCode) ||
        !plainMissing &&
            response.headers['content-type']?.split(';').first !=
                'application/json') {
      await response.stream.listen((_) {}).cancel();
      return '';
    }
    final reader = StreamIterator(response.stream);
    final bytes = <int>[];
    try {
      final deadline = Stopwatch()..start();
      while (await reader.moveNext().timeout(const Duration(seconds: 1))) {
        if (deadline.elapsed > const Duration(seconds: 1) ||
            bytes.length + reader.current.length > (plainMissing ? 64 : 2048))
          return '';
        bytes.addAll(reader.current);
      }
      if (plainMissing) {
        return utf8.decode(bytes) == '404 page not found\n'
            ? 'route_not_found'
            : '';
      }
      final value = jsonDecode(utf8.decode(bytes));
      final code = value is Map && value['error'] is Map
          ? value['error']['code']
          : null;
      return const {
            'not_found',
            'moment_changed',
            'human_control_changed',
          }.contains(code)
          ? code as String
          : '';
    } catch (_) {
      return '';
    } finally {
      bytes.fillRange(0, bytes.length, 0);
      await reader.cancel();
    }
  }

  Future<OwnedMapView> read({
    required String cityID,
    required MapBounds bounds,
    required String ownerID,
    required String authorization,
    required String origin,
    required bool Function() current,
    required DateTime Function() now,
    double zoom = 12,
    String? selectedMarkerID,
    Future<void>? abortTrigger,
    OwnedMapPin? cluster,
    String? cursor,
    String? viewportPath,
  }) async {
    if (!bounds.isValid ||
        !humanControlID(ownerID) ||
        !humanControlText(cityID, 0, 180) ||
        !zoom.isFinite ||
        zoom < 0 ||
        zoom > 22 ||
        canonicalAuthOrigin(origin) != origin ||
        authorization.isEmpty)
      throw const FormatException('地图读取条件不符');
    final params = <String, String>{
      if (cityID.isNotEmpty) 'cityId': cityID,
      ...bounds.toJson().map((k, v) => MapEntry(k, v.toString())),
      'zoom': zoom.toString(),
      'filterScope': 'DEFAULT_CONTENT',
      if (selectedMarkerID != null && selectedMarkerID.isNotEmpty)
        'selectedMarkerId': selectedMarkerID,
    };
    if (cluster != null || viewportPath != null) {
      if (cluster != null && !cluster.isCluster ||
          cursor != null && !humanControlText(cursor, 1, 4096)) {
        throw const FormatException('地图聚合读取条件不符');
      }
      final path = Uri.parse(cluster?.membersPath ?? viewportPath!);
      if (path.hasScheme ||
          path.hasAuthority ||
          path.hasFragment ||
          path.path != '/v1/me/map-content') {
        throw const FormatException('地图读取路径不符');
      }
      params
        ..clear()
        ..addAll(path.queryParameters);
      if (cursor != null) params['cursor'] = cursor;
    }
    final uri = Uri.parse(
      origin,
    ).replace(path: '/v1/me/map-content', queryParameters: params);
    final bytes = await _read(
      uri,
      authorization: authorization,
      current: current,
      limit: 4 * 1024 * 1024,
      mime: 'application/json',
      abortTrigger: abortTrigger,
    );
    try {
      final envelope = humanControlObject(jsonDecode(utf8.decode(bytes)), {
        'data',
      });
      return OwnedMapView.decode(
        envelope['data'],
        ownerID: ownerID,
        cityID: params['cityId'] ?? '',
        bounds: MapBounds(
          west: double.parse(params['west']!),
          east: double.parse(params['east']!),
          south: double.parse(params['south']!),
          north: double.parse(params['north']!),
        ),
        now: now(),
        zoom: double.parse(params['zoom']!),
        byteCount: bytes.length,
        memberPage: cluster != null,
      );
    } finally {
      bytes.fillRange(0, bytes.length, 0);
    }
  }

  Future<Uint8List> content(
    OwnedMapCover cover, {
    required String authorization,
    required String origin,
    required bool Function() current,
    Future<void>? abortTrigger,
  }) async {
    final bytes = await _read(
      Uri.parse(origin).resolve(cover.contentPath),
      authorization: authorization,
      current: current,
      limit: cover.byteCount,
      exact: cover.byteCount,
      mime: cover.mimeType,
      abortTrigger: abortTrigger,
    );
    try {
      final header = MomentImageHeader.inspect(
        bytes,
        storedDerivative: true,
        importedLegacyPrivate: true,
      );
      if (!current() ||
          sha256.convert(bytes).toString() != cover.sha256 ||
          header.mime != cover.mimeType ||
          header.width != cover.width ||
          header.height != cover.height)
        throw const FormatException('地图图片内容不符');
      return bytes;
    } catch (_) {
      bytes.fillRange(0, bytes.length, 0);
      rethrow;
    }
  }

  Future<Object?> _json(
    String path, {
    Map<String, String>? query,
    required String authorization,
    required String origin,
    required bool Function() current,
    bool allowMissing = false,
  }) async {
    if (canonicalAuthOrigin(origin) != origin || authorization.isEmpty) {
      throw const FormatException('地图读取身份不符');
    }
    final bytes = await _read(
      Uri.parse(origin).replace(path: path, queryParameters: query),
      authorization: authorization,
      current: current,
      limit: 8 * 1024 * 1024,
      mime: 'application/json',
      allowMissing: allowMissing,
    );
    try {
      return humanControlObject(jsonDecode(utf8.decode(bytes)), {
        'data',
      })['data'];
    } finally {
      bytes.fillRange(0, bytes.length, 0);
    }
  }

  Future<OwnedMapMomentDetail> readMoment(
    OwnedMapMoment target, {
    required String ownerID,
    required String authorization,
    required String origin,
    required bool Function() current,
    required DateTime Function() now,
  }) async {
    if (!humanControlID(target.id) ||
        target.detailPath != '/v1/me/map-content/moments/${target.id}') {
      throw const FormatException('地图动态读取目标不符');
    }
    final value = OwnedMapMomentDetail.decode(
      await _json(
        target.detailPath,
        query: const {'mediaMode': 'lazy'},
        authorization: authorization,
        origin: origin,
        current: current,
      ),
      ownerID: ownerID,
      now: now(),
    );
    if (!current() ||
        value.id != target.id ||
        value.revision != target.revision ||
        value.visibilitySource != target.visibilitySource)
      throw const FormatException('地图动态来源已变化');
    return value;
  }

  Future<OwnedMapThread?> readThread(
    String seedMomentID, {
    required String ownerID,
    required String authorization,
    required String origin,
    required bool Function() current,
    required DateTime Function() now,
  }) async {
    if (!humanControlID(seedMomentID)) {
      throw const FormatException('地图串文入口不符');
    }
    try {
      final value = OwnedMapThread.decode(
        await _json(
          '/v1/me/map-content/moments/$seedMomentID/thread',
          authorization: authorization,
          origin: origin,
          current: current,
          allowMissing: true,
        ),
        ownerID: ownerID,
        seedMomentID: seedMomentID,
        now: now(),
      );
      if (!current()) throw const FormatException('地图串文读取已失效');
      return value.hasThread ? value : null;
    } on OwnedMapReadFailure catch (failure) {
      // An older API has no additive reader route. Keep its existing single
      // entry path; absence of this endpoint does not establish no thread.
      if (failure.statusCode == 404 &&
          failure.code == 'route_not_found' &&
          current())
        return null;
      rethrow;
    }
  }

  Future<OwnedMapPlacePage> readPlaceMoments(
    String placeID, {
    String? cursor,
    required String ownerID,
    required String authorization,
    required String origin,
    required bool Function() current,
    required DateTime Function() now,
  }) async {
    if (!humanControlID(placeID) ||
        cursor != null && !humanControlText(cursor, 1, 4096)) {
      throw const FormatException('地点动态分页目标不符');
    }
    final value = OwnedMapPlacePage.decode(
      await _json(
        '/v1/me/map-content/places/$placeID/moments',
        query: {'mediaMode': 'lazy', if (cursor != null) 'cursor': cursor},
        authorization: authorization,
        origin: origin,
        current: current,
      ),
      ownerID: ownerID,
      placeID: placeID,
      now: now(),
    );
    if (!current()) throw const FormatException('地点动态读取已失效');
    return value;
  }

  Future<Uint8List> avatarContent(
    PersonAvatarMetadata avatar, {
    required String authorization,
    required String origin,
    required bool Function() current,
  }) async {
    final bytes = await _read(
      Uri.parse(origin).resolve(avatar.thumbnailPath),
      authorization: authorization,
      current: current,
      limit: personAvatarMaxThumbnailBytes,
      exact: avatar.byteCount,
      mime: 'image/jpeg',
    );
    try {
      final header = MomentImageHeader.inspect(bytes);
      if (!current() ||
          sha256.convert(bytes).toString() != avatar.digest ||
          header.mime != 'image/jpeg' ||
          header.width != 128 ||
          header.height != 128) {
        throw const FormatException('动态作者头像无法核实');
      }
      return bytes;
    } catch (_) {
      bytes.fillRange(0, bytes.length, 0);
      rethrow;
    }
  }

  Future<Uint8List> momentContent(
    OwnedMapMomentMedia media, {
    required OwnedMapMomentDetail detail,
    required DateTime Function() now,
    required String authorization,
    required String origin,
    required bool Function() current,
  }) async {
    final trace = MapMediaReadMetrics.begin(
      media.isVideo ? MapMediaReadKind.video : MapMediaReadKind.image,
    );
    var selected = media;
    var until = detail.validUntil;
    bool pending() => current() && until.isAfter(now());
    if (media.displayDescriptorPath != null) {
      trace.requests++;
      final path = Uri.parse(media.displayDescriptorPath!);
      final descriptor = OwnedMapDisplayDescriptor.decode(
        await _json(
          path.path,
          query: path.queryParameters,
          authorization: authorization,
          origin: origin,
          current: pending,
        ),
        detail: detail,
        original: media,
        now: now(),
      );
      if (!pending()) throw const FormatException('图片阅读已失效');
      selected = descriptor.media;
      until = descriptor.validUntil;
    }
    trace.mark(MapMediaReadStage.descriptorReady);
    final bytes = await _read(
      Uri.parse(origin).resolve(selected.contentPath),
      authorization: authorization,
      current: pending,
      limit: selected.byteCount,
      exact: selected.byteCount,
      mime: selected.mimeType,
      trace: trace,
    );
    try {
      if (!pending() || sha256.convert(bytes).toString() != selected.sha256)
        throw const FormatException('地图动态媒体内容不符');
      if (media.isVideo) {
        if (bytes.length < 12 ||
            ascii.decode(bytes.sublist(4, 8), allowInvalid: true) != 'ftyp') {
          throw const FormatException('地图视频格式不符');
        }
      } else {
        final header = MomentImageHeader.inspect(
          bytes,
          storedDerivative: true,
          importedLegacyPrivate: true,
        );
        if (header.mime != selected.mimeType ||
            header.width != selected.width ||
            header.height != selected.height) {
          throw const FormatException('地图图片内容不符');
        }
      }
      trace.mark(MapMediaReadStage.verified);
      _contentDeadlines[bytes] = until;
      _contentTraces[bytes] = trace;
      return bytes;
    } catch (_) {
      trace.mark(MapMediaReadStage.failed);
      bytes.fillRange(0, bytes.length, 0);
      rethrow;
    }
  }

  /// Transfers to a private, unpublished sink. The caller may publish a player
  /// path only after this completes with exact bytes, full SHA, and live ACL.
  Future<void> transferVideo(
    OwnedMapMomentMedia media, {
    required OwnedMapMomentDetail detail,
    required DateTime Function() now,
    required String authorization,
    required String origin,
    required bool Function() current,
    required Future<void> Function(Uint8List bytes) receive,
    required MapMediaReadTrace trace,
  }) async {
    bool pending() => current() && detail.validUntil.isAfter(now());
    if (!media.isVideo ||
        media.displayDescriptorPath != null ||
        !detail.media.any((m) => identical(m, media)) ||
        !pending()) {
      throw const FormatException('地图视频读取已失效');
    }
    final digest = _MapMediaDigest();
    final hasher = sha256.startChunkedConversion(digest);
    final prefix = <int>[];
    try {
      await _read(
        Uri.parse(origin).resolve(media.contentPath),
        authorization: authorization,
        current: pending,
        limit: media.byteCount,
        exact: media.byteCount,
        mime: media.mimeType,
        trace: trace,
        receive: (chunk) async {
          hasher.add(chunk);
          if (prefix.length < 12) prefix.addAll(chunk.take(12 - prefix.length));
          await receive(chunk);
          if (!pending()) throw const FormatException('地图视频读取已失效');
        },
      );
      hasher.close();
      if (!pending() ||
          digest.value?.toString() != media.sha256 ||
          prefix.length < 12 ||
          ascii.decode(prefix.sublist(4, 8), allowInvalid: true) != 'ftyp') {
        throw const FormatException('地图视频内容不符');
      }
      trace.mark(MapMediaReadStage.verified);
    } catch (_) {
      trace.mark(MapMediaReadStage.failed);
      rethrow;
    } finally {
      prefix.fillRange(0, prefix.length, 0);
    }
  }

  void dispose() {
    _closed = true;
    ++_transport;
  }
}

class _MapMediaDigest implements Sink<Digest> {
  Digest? value;
  @override
  void add(Digest data) => value = data;
  @override
  void close() {}
}
