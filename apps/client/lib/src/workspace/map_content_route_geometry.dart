import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'human_causal_control.dart';

@immutable
class MapContentRouteEndpoint {
  const MapContentRouteEndpoint({
    required this.viewportPath,
    required this.markerID,
    required this.sourceVersion,
  });
  final String viewportPath, markerID, sourceVersion;
  bool get valid {
    final uri = Uri.tryParse(viewportPath);
    return markerID.startsWith('anchor:') &&
        humanControlID(markerID.substring(7)) &&
        humanControlHash(sourceVersion) &&
        uri != null &&
        !uri.hasScheme &&
        !uri.hasAuthority &&
        !uri.hasFragment &&
        uri.path == '/v1/me/map-content' &&
        viewportPath.length <= 8192 &&
        uri.queryParametersAll.values.every((v) => v.length == 1);
  }

  Map<String, Object?> toJson() => {
    'viewportPath': viewportPath,
    'markerId': markerID,
    'sourceVersion': sourceVersion,
  };
}

@immutable
class MapContentRouteQuery {
  const MapContentRouteQuery({
    required this.threadID,
    required this.expectedProvider,
    required this.from,
    required this.to,
    required this.validUntil,
  });
  final String threadID, expectedProvider;
  final MapContentRouteEndpoint from, to;

  /// Original client pin deadlines: a new route response cannot extend these.
  final DateTime validUntil;
  bool get valid =>
      humanControlID(threadID) &&
      const {'amap', 'mapbox'}.contains(expectedProvider) &&
      from.valid &&
      to.valid &&
      from.markerID != to.markerID;
  Map<String, Object?> toJson() => {
    'threadId': threadID,
    'expectedProvider': expectedProvider,
    'from': from.toJson(),
    'to': to.toJson(),
  };
  String get key => jsonEncode(toJson());
}

@immutable
class MapContentRoutePoint {
  const MapContentRoutePoint(this.latitude, this.longitude);
  final double latitude, longitude;
  @override
  bool operator ==(Object other) =>
      other is MapContentRoutePoint &&
      other.latitude == latitude &&
      other.longitude == longitude;
  @override
  int get hashCode => Object.hash(latitude, longitude);
}

@immutable
class MapContentRouteGeometry {
  const MapContentRouteGeometry({
    required this.query,
    required this.status,
    required this.mode,
    required this.provider,
    required this.geometryKind,
    required this.segments,
    required this.distanceMeters,
    required this.durationSeconds,
    required this.sourceVersion,
    required this.observedAt,
    required this.validUntil,
    this.current,
  });
  final MapContentRouteQuery query;
  final String status, mode, provider, geometryKind, sourceVersion;
  final List<List<MapContentRoutePoint>> segments;
  final int distanceMeters, durationSeconds;
  final DateTime observedAt, validUntil;
  final bool Function()? current;
  bool availableAt(DateTime at) =>
      status == 'AVAILABLE' &&
      at.isBefore(validUntil) &&
      (current?.call() ?? true);
  String get fingerprint =>
      '$sourceVersion:${validUntil.microsecondsSinceEpoch}:$status:$geometryKind';
  MapContentRouteGeometry bind({
    required DateTime until,
    required bool Function() current,
  }) => MapContentRouteGeometry(
    query: query,
    status: status,
    mode: mode,
    provider: provider,
    geometryKind: geometryKind,
    segments: segments,
    distanceMeters: distanceMeters,
    durationSeconds: durationSeconds,
    sourceVersion: sourceVersion,
    observedAt: observedAt,
    validUntil: until.isBefore(validUntil) ? until : validUntil,
    current: current,
  );

  factory MapContentRouteGeometry.decode(
    Object? raw, {
    required String ownerID,
    required MapContentRouteQuery query,
    required DateTime now,
  }) {
    final v = humanControlObject(raw, {
      'schemaVersion',
      'viewerAccountId',
      'threadId',
      'fromMarkerId',
      'toMarkerId',
      'fromSourceVersion',
      'toSourceVersion',
      'status',
      'mode',
      'provider',
      'coordinateSystem',
      'geometryKind',
      'segments',
      'distanceMeters',
      'durationSeconds',
      'sourceVersion',
      'observedAt',
      'validUntil',
    });
    final observed = v['observedAt'] is String
        ? DateTime.tryParse(v['observedAt'])
        : null;
    final until = v['validUntil'] is String
        ? DateTime.tryParse(v['validUntil'])
        : null;
    final status = v['status'],
        kind = v['geometryKind'],
        mode = v['mode'],
        provider = v['provider'];
    if (!query.valid ||
        !humanControlID(ownerID) ||
        v['schemaVersion'] != 'map-content-thread-route-v1' ||
        v['viewerAccountId'] != ownerID ||
        v['threadId'] != query.threadID ||
        v['fromMarkerId'] != query.from.markerID ||
        v['toMarkerId'] != query.to.markerID ||
        v['fromSourceVersion'] != query.from.sourceVersion ||
        v['toSourceVersion'] != query.to.sourceVersion ||
        !const {'AVAILABLE', 'BLOCKED', 'NOT_AVAILABLE'}.contains(status) ||
        v['coordinateSystem'] != 'wgs84' ||
        !const {'', 'ROAD', 'TRANSFER'}.contains(kind) ||
        mode is! String ||
        provider is! String ||
        mode.length > 40 ||
        provider.length > 80 ||
        !humanControlHash(v['sourceVersion']) ||
        observed == null ||
        until == null ||
        !observed.isUtc ||
        !until.isUtc ||
        observed.isAfter(now.add(const Duration(seconds: 5))) ||
        !until.isAfter(now) ||
        !until.isAfter(observed) ||
        until.difference(observed) > const Duration(seconds: 30) ||
        v['segments'] is! List ||
        (v['segments'] as List).length > 32 ||
        v['distanceMeters'] is! int ||
        v['durationSeconds'] is! int ||
        (v['distanceMeters'] as int) < 0 ||
        (v['distanceMeters'] as int) > 100000000 ||
        (v['durationSeconds'] as int) < 0 ||
        (v['durationSeconds'] as int) > 31536000) {
      throw const FormatException('串文路线来源不符');
    }
    var count = 0;
    final segments = <List<MapContentRoutePoint>>[];
    for (final row in v['segments'] as List) {
      if (row is! List ||
          row.length < 2 ||
          row.length > 12000 ||
          (count += row.length) > 24000)
        throw const FormatException('串文路线分段无效');
      final points = <MapContentRoutePoint>[];
      for (final raw in row) {
        final p = humanControlObject(raw, {'latitude', 'longitude'});
        final lat = p['latitude'], lon = p['longitude'];
        if (lat is! num ||
            lon is! num ||
            !lat.isFinite ||
            !lon.isFinite ||
            lat.abs() > 90 ||
            lon.abs() > 180)
          throw const FormatException('串文路线坐标无效');
        points.add(MapContentRoutePoint(lat.toDouble(), lon.toDouble()));
      }
      segments.add(List.unmodifiable(points));
    }
    if (status == 'AVAILABLE') {
      if (segments.isEmpty ||
          kind == '' ||
          (kind == 'ROAD' &&
              (!const {
                    'walking',
                    'cycling',
                    'bicycling',
                    'driving',
                    'transit',
                  }.contains(mode) ||
                  provider != query.expectedProvider ||
                  v['distanceMeters'] <= 0 ||
                  v['durationSeconds'] <= 0)) ||
          (kind == 'TRANSFER' &&
              (!const {'train', 'flight'}.contains(mode) ||
                  provider != 'saved-transfer' ||
                  v['distanceMeters'] != 0 ||
                  v['durationSeconds'] != 0)))
        throw const FormatException('串文路线类型无效');
    } else if (segments.isNotEmpty ||
        v['distanceMeters'] != 0 ||
        v['durationSeconds'] != 0) {
      throw const FormatException('不可用路线不能携带几何');
    }
    return MapContentRouteGeometry(
      query: query,
      status: status,
      mode: mode,
      provider: provider,
      geometryKind: kind,
      segments: List.unmodifiable(segments),
      distanceMeters: v['distanceMeters'],
      durationSeconds: v['durationSeconds'],
      sourceVersion: v['sourceVersion'],
      observedAt: observed,
      validUntil: until,
    );
  }
}
