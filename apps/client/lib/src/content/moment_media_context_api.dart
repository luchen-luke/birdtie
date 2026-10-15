import 'dart:convert';
import 'package:http/http.dart' as http;
import '../workspace/human_causal_control.dart';
import '../workspace/legacy_place_reference_page.dart';

class MomentMediaContextPlace {
  const MomentMediaContextPlace(this.id, this.placeID, this.reference);
  final String id, placeID;
  final LegacyPlaceReference? reference;
}

class MomentMediaContext {
  const MomentMediaContext({
    required this.id,
    required this.assetKind,
    required this.assetID,
    required this.revision,
    required this.sourcePosition,
    required this.originalMapEnabled,
    required this.coordinateStatus,
    required this.locations,
    this.capturedDate,
    this.latitude,
    this.longitude,
  });
  final String id, assetKind, assetID, coordinateStatus;
  final int revision, sourcePosition;
  final bool originalMapEnabled;
  final String? capturedDate;
  final double? latitude, longitude;
  final List<MomentMediaContextPlace> locations;

  factory MomentMediaContext.decode(Object? raw, String momentID) {
    final v = humanControlObject(
      raw,
      {
        'id',
        'assetKind',
        'assetId',
        'revision',
        'sourcePosition',
        'capturedDate',
        'originalMapEnabled',
        'coordinateStatus',
        'locations',
      },
      optional: {'sourceLatitude', 'sourceLongitude'},
    );
    final date = v['capturedDate'],
        lat = v['sourceLatitude'],
        lon = v['sourceLongitude'];
    if (!humanControlID(v['id']) ||
        !humanControlID(v['assetId']) ||
        !{'IMAGE', 'VIDEO'}.contains(v['assetKind']) ||
        !humanControlVersion(v['revision'], zero: false) ||
        v['sourcePosition'] is! int ||
        v['sourcePosition'] < 0 ||
        v['originalMapEnabled'] is! bool ||
        !{'ABSENT', 'UNSPECIFIED'}.contains(v['coordinateStatus']) ||
        date != null &&
            (date is! String ||
                !RegExp(r'^\d{4}-\d{2}-\d{2}$').hasMatch(date) ||
                DateTime.tryParse(date) == null) ||
        v['locations'] is! List ||
        (v['locations'] as List).length > 50 ||
        (v['coordinateStatus'] == 'ABSENT' && (lat != null || lon != null)) ||
        (v['coordinateStatus'] == 'UNSPECIFIED' &&
            (lat is! num ||
                lon is! num ||
                !lat.isFinite ||
                !lon.isFinite ||
                lat.abs() > 90 ||
                lon.abs() > 180))) {
      throw const FormatException('原材料信息格式不符');
    }
    final places = <MomentMediaContextPlace>[], ids = <String>{};
    for (final raw in v['locations'] as List) {
      final p = humanControlObject(
        raw,
        {'id', 'position', 'placeId'},
        optional: {'legacyReference'},
      );
      if (!humanControlID(p['id']) ||
          !ids.add(p['id']) ||
          p['position'] != places.length ||
          !humanControlID(p['placeId']))
        throw const FormatException('原材料地点顺序不符');
      final reference = p['legacyReference'] == null
          ? null
          : LegacyPlaceReference.decode(p['legacyReference']);
      if (reference != null &&
          (reference.kind != 'moment' ||
              reference.recordID != momentID ||
              reference.placeID != p['placeId']))
        throw const FormatException('原材料地点归属不符');
      places.add(MomentMediaContextPlace(p['id'], p['placeId'], reference));
    }
    return MomentMediaContext(
      id: v['id'],
      assetKind: v['assetKind'],
      assetID: v['assetId'],
      revision: v['revision'],
      sourcePosition: v['sourcePosition'],
      capturedDate: date,
      originalMapEnabled: v['originalMapEnabled'],
      coordinateStatus: v['coordinateStatus'],
      latitude: (lat as num?)?.toDouble(),
      longitude: (lon as num?)?.toDouble(),
      locations: List.unmodifiable(places),
    );
  }
}

class MomentMediaContexts {
  const MomentMediaContexts(
    this.momentID,
    this.revision,
    this.items,
    this.sourceVersion,
    this.observedAt,
    this.validUntil,
  );
  final String momentID, sourceVersion;
  final int revision;
  final List<MomentMediaContext> items;
  final DateTime observedAt, validUntil;
  bool get current => DateTime.now().toUtc().isBefore(validUntil);
  factory MomentMediaContexts.decode(
    Object? raw,
    String momentID, {
    int? expectedRevision,
  }) {
    final v = humanControlObject(raw, {
      'schemaVersion',
      'momentId',
      'momentRevision',
      'items',
      'sourceVersion',
      'observedAt',
      'validUntil',
    });
    if (v['schemaVersion'] != 'owned-moment-media-context-v1' ||
        v['momentId'] != momentID ||
        !humanControlID(momentID) ||
        !humanControlVersion(v['momentRevision'], zero: false) ||
        expectedRevision != null && v['momentRevision'] != expectedRevision ||
        !humanControlHash(v['sourceVersion']) ||
        v['items'] is! List ||
        (v['items'] as List).length > 200)
      throw const FormatException('原材料信息来源不符');
    final at = humanControlTime(v['observedAt']),
        until = humanControlTime(v['validUntil']);
    humanControlLease(at, until);
    if (until.difference(at) > const Duration(seconds: 30))
      throw const FormatException('原材料信息已变化');
    final items = (v['items'] as List)
        .map((r) => MomentMediaContext.decode(r, momentID))
        .toList();
    if (items.map((v) => v.id).toSet().length != items.length)
      throw const FormatException('原材料信息重复');
    return MomentMediaContexts(
      momentID,
      v['momentRevision'],
      List.unmodifiable(items),
      v['sourceVersion'],
      at,
      until,
    );
  }
}

class MomentMediaContextAPI {
  const MomentMediaContextAPI({
    required this.client,
    required this.baseUrl,
    required this.authorizationHeader,
    required this.current,
  });
  final http.Client client;
  final String baseUrl;
  final String? Function() authorizationHeader;
  final bool Function() current;
  Future<MomentMediaContexts> read(
    String momentID, {
    int? expectedRevision,
  }) async {
    final token = authorizationHeader();
    if (!current() || token == null || !humanControlID(momentID))
      throw StateError('请重新打开动态');
    final response = await client
        .get(
          Uri.parse(
            '${baseUrl.replaceFirst(RegExp(r'/$'), '')}/v1/me/moments/$momentID/media-contexts',
          ),
          headers: {'Authorization': token},
        )
        .timeout(const Duration(seconds: 12));
    if (!current() || token != authorizationHeader())
      throw StateError('登录状态已变化');
    if (response.statusCode != 200 || response.bodyBytes.length > 256 * 1024)
      throw StateError('原材料信息暂不可读');
    final envelope = humanControlObject(
      jsonDecode(utf8.decode(response.bodyBytes)),
      {'data'},
    );
    return MomentMediaContexts.decode(
      envelope['data'],
      momentID,
      expectedRevision: expectedRevision,
    );
  }
}
