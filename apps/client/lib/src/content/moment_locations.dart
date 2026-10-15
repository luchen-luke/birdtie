import 'dart:convert';
import '../workspace/human_causal_control.dart';
import '../workspace/legacy_place_reference_page.dart';

const momentLocationMaxLinks = 50;
const momentLocationsSchema = 'moment-locations-v1';

class MomentLocationInput {
  const MomentLocationInput({
    required this.id,
    required this.kind,
    this.cityID = '',
    this.placeID = '',
    this.referenceID = '',
    required this.precision,
  });
  final String id, kind, cityID, placeID, referenceID, precision;
  bool get valid =>
      humanControlID(id) &&
      cityID == cityID.trim() &&
      !cityID.contains('\u0000') &&
      !RegExp(r'[/\\?#]').hasMatch(cityID) &&
      utf8.encode(cityID).length <= 80 &&
      (kind == 'REFERENCE'
          ? cityID.isEmpty &&
                placeID.isEmpty &&
                humanControlID(referenceID) &&
                {'none', 'place'}.contains(precision)
          : referenceID.isEmpty &&
                (kind == 'CITY'
                    ? cityID.isNotEmpty &&
                          placeID.isEmpty &&
                          {'none', 'city'}.contains(precision)
                    : kind == 'PLACE' &&
                          cityID.isEmpty &&
                          humanControlID(placeID) &&
                          {'none', 'city', 'place'}.contains(precision)));
  MomentLocationInput withPrecision(String value) => MomentLocationInput(
    id: id,
    kind: kind,
    cityID: cityID,
    placeID: placeID,
    referenceID: referenceID,
    precision: value,
  );
  Map<String, Object?> toJson() {
    if (!valid) throw const FormatException('动态位置选择不符');
    return {
      'id': id,
      'kind': kind,
      'cityId': cityID,
      'placeId': placeID,
      'precision': precision,
      if (kind == 'REFERENCE') 'referenceId': referenceID,
    };
  }

  List<Object?> get digestTuple {
    toJson();
    return [
      id,
      kind,
      base64Encode(utf8.encode(cityID)),
      placeID,
      precision,
      if (kind == 'REFERENCE') referenceID,
    ];
  }

  factory MomentLocationInput.decode(Object? raw) {
    final v = humanControlObject(raw, {
      'id',
      'kind',
      'cityId',
      'placeId',
      'precision',
      if (raw is Map && raw['kind'] == 'REFERENCE') 'referenceId',
    });
    if (v.values.any((value) => value is! String))
      throw const FormatException('动态位置选择不符');
    final result = MomentLocationInput(
      id: v['id'],
      kind: v['kind'],
      cityID: v['cityId'],
      placeID: v['placeId'],
      referenceID: v['referenceId'] ?? '',
      precision: v['precision'],
    );
    if (!result.valid) throw const FormatException('动态位置选择不符');
    return result;
  }
}

List<MomentLocationInput> decodeMomentLocationInputs(Object? raw) {
  if (raw is! List || raw.length > momentLocationMaxLinks)
    throw const FormatException('请保留最多50处位置');
  final values = raw.map(MomentLocationInput.decode).toList(growable: false);
  final ids = <String>{}, targets = <String>{};
  for (final value in values) {
    if (!ids.add(value.id) ||
        !targets.add(
          '${value.kind}:${value.cityID}:${value.placeID}:${value.referenceID}',
        )) {
      throw const FormatException('同一位置请修改原选择，不要重复添加');
    }
  }
  return List.unmodifiable(values);
}

String momentLocationPrecisionLabel(String precision) => switch (precision) {
  'none' => '仅自己可见',
  'city' => '仅公开城市',
  'place' => '公开地点',
  _ => '位置范围待核实',
};

class MomentLocationAnchor {
  const MomentLocationAnchor(
    this.placeID,
    this.label,
    this.latitude,
    this.longitude,
  );
  final String placeID, label;
  final double latitude, longitude;
  factory MomentLocationAnchor.decode(
    Object? raw,
    String placeID, {
    String referenceID = '',
  }) {
    final v = humanControlObject(raw, {
      'placeId',
      'label',
      'coordinateSystem',
      'precision',
      'latitude',
      'longitude',
      if (referenceID.isNotEmpty) ...{'targetKind', 'referenceId'},
    });
    if (v['placeId'] != placeID ||
        (referenceID.isEmpty
            ? !humanControlID(placeID)
            : placeID.isNotEmpty ||
                  !humanControlID(referenceID) ||
                  v['referenceId'] != referenceID ||
                  v['targetKind'] != 'REFERENCE') ||
        v['label'] is! String ||
        (v['label'] as String).isEmpty ||
        utf8.encode(v['label']).length > 640 ||
        v['coordinateSystem'] != 'wgs84' ||
        v['precision'] != 'point' ||
        v['latitude'] is! num ||
        v['longitude'] is! num) {
      throw const FormatException('动态地图位置不符');
    }
    final lat = (v['latitude'] as num).toDouble(),
        lon = (v['longitude'] as num).toDouble();
    if (!lat.isFinite || !lon.isFinite || lat.abs() > 90 || lon.abs() > 180)
      throw const FormatException('动态地图位置不符');
    return MomentLocationAnchor(placeID, v['label'], lat, lon);
  }
}

class MomentLocationResolved {
  const MomentLocationResolved({
    required this.id,
    required this.position,
    required this.kind,
    required this.precision,
    required this.cityID,
    required this.cityName,
    required this.placeID,
    required this.placeName,
    required this.region,
    required this.countryCode,
    required this.available,
    required this.sourceVersion,
    required this.observedAt,
    required this.validUntil,
    this.anchor,
    this.legacyReference,
    this.referenceID = '',
    this.referenceName = '',
    this.attribution = '',
    this.sourceURL = '',
  });
  final String id,
      kind,
      precision,
      cityID,
      cityName,
      placeID,
      placeName,
      region,
      countryCode,
      sourceVersion;
  final String referenceID, referenceName, attribution, sourceURL;
  final int position;
  final bool available;
  final DateTime observedAt, validUntil;
  final MomentLocationAnchor? anchor;
  final LegacyPlaceReference? legacyReference;
  bool get current => available && DateTime.now().toUtc().isBefore(validUntil);
  bool get readable =>
      DateTime.now().toUtc().isBefore(validUntil) &&
      (available || legacyReference?.current == true);
  MomentLocationInput get input => MomentLocationInput(
    id: id,
    kind: kind,
    cityID: kind == 'CITY' ? cityID : '',
    placeID: placeID,
    referenceID: referenceID,
    precision: precision,
  );
  String get label => !readable
      ? '位置暂不可用'
      : !available && legacyReference != null
      ? '原关联地点 · 仅自己可见'
      : [
          referenceName.isNotEmpty
              ? referenceName
              : placeName.isNotEmpty
              ? placeName
              : cityName,
          region,
          countryCode,
        ].where((v) => v.isNotEmpty).toSet().join(' · ');
  factory MomentLocationResolved.decode(
    Object? raw,
    int index, {
    bool own = true,
    DateTime? observedAt,
    DateTime? validUntil,
  }) {
    final v = humanControlObject(
      raw,
      {
        'id',
        own ? 'position' : 'displayOrder',
        'kind',
        'precision',
        'cityId',
        'cityName',
        'placeId',
        'placeName',
        'region',
        'countryCode',
        'sourceVersion',
        if (own) ...{'available', 'observedAt', 'validUntil'},
      },
      optional: {
        'anchor',
        if (own) 'legacyReference',
        'referenceId',
        'referenceName',
        'attribution',
        'sourceUrl',
      },
    );
    for (final key in [
      'id',
      'kind',
      'precision',
      'cityId',
      'cityName',
      'placeId',
      'placeName',
      'region',
      'countryCode',
      'sourceVersion',
    ]) {
      if (v[key] is! String ||
          (v[key] as String).contains('\u0000') ||
          (v[key] as String).trim() != v[key] ||
          utf8.encode(v[key]).length > 640)
        throw const FormatException('位置资料不完整');
    }
    final at = own ? humanControlTime(v['observedAt']) : observedAt!,
        until = own ? humanControlTime(v['validUntil']) : validUntil!;
    if (!humanControlID(v['id']) ||
        v[own ? 'position' : 'displayOrder'] != index ||
        !humanControlHash(v['sourceVersion']) ||
        utf8.encode(v['cityId']).length > 80 ||
        (own && v['available'] is! bool) ||
        !until.isAfter(at) ||
        until.difference(at) > const Duration(seconds: 60) ||
        !{'CITY', 'PLACE', 'REFERENCE'}.contains(v['kind']) ||
        !{'none', 'city', 'place'}.contains(v['precision']) ||
        v['kind'] == 'CITY' &&
            (v['cityId'] == '' ||
                v['placeId'] != '' ||
                v['precision'] == 'place') ||
        v['kind'] == 'PLACE' && !humanControlID(v['placeId']) ||
        !own && v['precision'] == 'none') {
      throw const FormatException('位置资料来源或期限不符');
    }
    final available = own ? v['available'] as bool : true;
    if (v['kind'] == 'REFERENCE') {
      if (!humanControlID(v['referenceId']) ||
          v['cityId'] != '' ||
          v['placeId'] != '' ||
          v['cityName'] != '' ||
          v['placeName'] != '' ||
          v['precision'] == 'city')
        throw const FormatException('外部位置身份不符');
      for (final key in ['referenceName', 'attribution', 'sourceUrl']) {
        if (v[key] != null &&
            (v[key] is! String || utf8.encode(v[key]).length > 640))
          throw const FormatException('外部位置资料不符');
      }
      if (available &&
          ((v['referenceName'] ?? '').isEmpty ||
              v['attribution'] != '© OpenStreetMap contributors · ODbL 1.0' ||
              !RegExp(
                r'^https://www\.openstreetmap\.org/(node|way|relation)/[1-9][0-9]*$',
              ).hasMatch(v['sourceUrl'] ?? '')))
        throw const FormatException('外部位置来源不符');
      if (!available &&
          [
            'referenceName',
            'attribution',
            'sourceUrl',
          ].any((key) => (v[key] ?? '') != ''))
        throw const FormatException('外部位置已失效');
    } else if ([
      'referenceId',
      'referenceName',
      'attribution',
      'sourceUrl',
    ].any(v.containsKey)) {
      throw const FormatException('位置类型不符');
    }
    final reference = v['legacyReference'] == null
        ? null
        : LegacyPlaceReference.decode(v['legacyReference']);
    if (reference != null &&
        (reference.kind != 'moment' ||
            reference.placeID != v['placeId'] ||
            v['kind'] != 'PLACE' ||
            reference.validUntil.isBefore(until))) {
      throw const FormatException('私人地点引用不符');
    }
    if (!available &&
            reference == null &&
            [
              'cityName',
              'placeName',
              'region',
              'countryCode',
            ].any((key) => v[key] != '') ||
        v['anchor'] != null &&
            (!available ||
                v['precision'] != 'place' ||
                !{'PLACE', 'REFERENCE'}.contains(v['kind'])) ||
        !own && v['kind'] == 'CITY' && v['placeName'] != '')
      throw const FormatException('位置公开粒度不符');
    return MomentLocationResolved(
      id: v['id'],
      position: index,
      kind: v['kind'],
      precision: v['precision'],
      cityID: v['cityId'],
      cityName: v['cityName'],
      placeID: v['placeId'],
      placeName: v['placeName'],
      region: v['region'],
      countryCode: v['countryCode'],
      available: available,
      legacyReference: reference,
      referenceID: v['referenceId'] ?? '',
      referenceName: v['referenceName'] ?? '',
      attribution: v['attribution'] ?? '',
      sourceURL: v['sourceUrl'] ?? '',
      sourceVersion: v['sourceVersion'],
      observedAt: at,
      validUntil: until,
      anchor: v['anchor'] == null
          ? null
          : MomentLocationAnchor.decode(
              v['anchor'],
              v['placeId'],
              referenceID: v['referenceId'] ?? '',
            ),
    );
  }
}

class MomentLocationsView {
  const MomentLocationsView({
    required this.momentID,
    required this.momentRevision,
    required this.items,
    required this.sourceVersion,
    required this.observedAt,
    required this.validUntil,
    required this.own,
  });
  final String momentID, sourceVersion;
  final int momentRevision;
  final List<MomentLocationResolved> items;
  final DateTime observedAt, validUntil;
  final bool own;
  bool get current => DateTime.now().toUtc().isBefore(validUntil);
  List<MomentLocationInput> get inputs {
    if (!own || !current) throw const FormatException('本人位置需要重新读取');
    return List.unmodifiable(items.map((item) => item.input));
  }

  factory MomentLocationsView.decode(
    Object? raw,
    String momentID, {
    required bool own,
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
    final at = humanControlTime(v['observedAt']),
        until = humanControlTime(v['validUntil']);
    if (!{
          momentLocationsSchema,
          'moment-locations-v2',
        }.contains(v['schemaVersion']) ||
        v['momentId'] != momentID ||
        !humanControlID(momentID) ||
        !humanControlVersion(v['momentRevision'], zero: false) ||
        expectedRevision != null && v['momentRevision'] != expectedRevision ||
        !humanControlHash(v['sourceVersion']) ||
        v['items'] is! List ||
        (v['items'] as List).length > momentLocationMaxLinks ||
        !until.isAfter(at) ||
        until.difference(at) > const Duration(seconds: 60) ||
        !DateTime.now().toUtc().isBefore(until))
      throw const FormatException('动态位置来源已变化');
    final values = <MomentLocationResolved>[];
    for (var i = 0; i < (v['items'] as List).length; i++)
      values.add(
        MomentLocationResolved.decode(
          v['items'][i],
          i,
          own: own,
          observedAt: at,
          validUntil: until,
        ),
      );
    if (values.map((v) => v.id).toSet().length != values.length)
      throw const FormatException('动态位置集合不符');
    return MomentLocationsView(
      momentID: momentID,
      momentRevision: v['momentRevision'],
      items: List.unmodifiable(values),
      sourceVersion: v['sourceVersion'],
      observedAt: at,
      validUntil: until,
      own: own,
    );
  }
}
