import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';
import 'package:crypto/crypto.dart';
import 'package:http/http.dart' as http;
import '../workspace/human_causal_control.dart';
import '../workspace/public_moment_media.dart';
import 'moment_locations.dart';

Map<String, Object?>? _freezeMomentInput(Map<String, Object?>? input) {
  if (input == null) return null;
  return Map.unmodifiable({
    ...input,
    if (input.containsKey('locations'))
      'locations': List.unmodifiable(
        decodeMomentLocationInputs(
          input['locations'],
        ).map((value) => Map<String, Object?>.unmodifiable(value.toJson())),
      ),
  });
}

const momentControlMaxRevision = humanControlMaxVersion;
String newMomentOperationID() {
  final r = Random.secure(), b = List<int>.generate(16, (_) => r.nextInt(256));
  b[6] = (b[6] & 15) | 64;
  b[8] = (b[8] & 63) | 128;
  final h = b.map((v) => v.toRadixString(16).padLeft(2, '0')).join();
  return '${h.substring(0, 8)}-${h.substring(8, 12)}-${h.substring(12, 16)}-${h.substring(16, 20)}-${h.substring(20)}';
}

String momentHash(Object raw) =>
    sha256.convert(utf8.encode(jsonEncode(raw))).toString();
String momentUTC(DateTime time) => time
    .toUtc()
    .toIso8601String()
    .replaceFirstMapped(RegExp(r'\.(\d+)Z$'), (m) {
      final fraction = m[1]!.replaceFirst(RegExp(r'0+$'), '');
      return fraction.isEmpty
          ? 'Z'
          : '.$fraction'
                'Z';
    });

/// Omitted context means keep the original selector; empty means remove it.
class MomentControlInput {
  MomentControlInput({
    required this.operationID,
    required this.action,
    this.momentID = '',
    required this.expectedRevision,
    Map<String, Object?>? input,
    List<PublicMomentMediaSelection>? media,
    this.localScope = '',
  }) : input = _freezeMomentInput(input),
       media = media == null ? null : List.unmodifiable(media) {
    if (!valid) throw const FormatException('动态操作输入不符');
  }
  final String operationID, action, momentID;
  final int expectedRevision;
  final Map<String, Object?>? input;
  final List<PublicMomentMediaSelection>? media;
  final String localScope;
  bool get valid {
    final selections = media;
    if (selections == null
        ? localScope.isNotEmpty
        : !const {'PUBLISH', 'UPDATE_PUBLIC'}.contains(action) ||
              selections.length > 9 ||
              selections.isNotEmpty && !humanControlID(localScope) ||
              selections.isEmpty && localScope.isNotEmpty ||
              selections.map((selection) => selection.mediaID).toSet().length !=
                  selections.length ||
              selections.any(
                (selection) =>
                    !humanControlID(selection.mediaID) ||
                    !humanControlVersion(selection.version, zero: false) ||
                    !selection.rights.valid,
              )) {
      return false;
    }
    if (!humanControlID(operationID) || !humanControlVersion(expectedRevision))
      return false;
    if (action == 'CREATE'
        ? momentID.isNotEmpty || expectedRevision != 0 || input == null
        : !humanControlID(momentID) || expectedRevision < 1)
      return false;
    if (!const {
          'CREATE',
          'UPDATE',
          'WITHDRAW',
          'PUBLISH',
          'UPDATE_PUBLIC',
        }.contains(action) ||
        const {'UPDATE', 'PUBLISH', 'UPDATE_PUBLIC'}.contains(action) &&
            (input == null || expectedRevision >= momentControlMaxRevision) ||
        action == 'WITHDRAW' && input != null)
      return false;
    final d = input;
    if (d == null) return true;
    if ((action == 'PUBLISH' || action == 'UPDATE_PUBLIC' && media != null) &&
        d['body'] is String &&
        (d['body'] as String).trim().isEmpty &&
        (media == null || media!.isEmpty))
      return false;
    if (const {'PUBLISH', 'UPDATE_PUBLIC'}.contains(action) &&
        (!((d['cityId'] == '' &&
                    d['placeId'] == '' &&
                    d['locationPrecision'] == 'none') ||
                (d['cityId'] != '' &&
                    d['placeId'] == '' &&
                    d['locationPrecision'] == 'city') ||
                (d['placeId'] is String &&
                    (d['placeId'] as String).isNotEmpty &&
                    d['locationPrecision'] == 'place')) ||
            const [
              'activityId',
              'communityId',
              'organizationId',
            ].any(d.containsKey))) {
      return false;
    }
    const required = {
          'cityId',
          'placeId',
          'title',
          'body',
          'timePrecision',
          'locationPrecision',
        },
        optional = {
          'occurredAt',
          'activityId',
          'communityId',
          'organizationId',
          'locations',
        };
    if (!required.every(d.containsKey) ||
        d.keys.any((k) => !required.contains(k) && !optional.contains(k)))
      return false;
    if (d.containsKey('locations')) {
      try {
        decodeMomentLocationInputs(d['locations']);
      } catch (_) {
        return false;
      }
      if (d['cityId'] != '' ||
          d['placeId'] != '' ||
          d['locationPrecision'] != 'none')
        return false;
    }
    for (final entry in {
      'cityId': (0, 80),
      'title': (0, 160),
      'body': (0, 5000),
    }.entries) {
      final v = d[entry.key];
      if (v is! String ||
          v != v.trim() ||
          v.contains('\u0000') ||
          utf8.encode(v).length < entry.value.$1 ||
          utf8.encode(v).length > entry.value.$2)
        return false;
    }
    if (d['placeId'] is! String ||
        (d['placeId'] != '' && !humanControlID(d['placeId'])))
      return false;
    for (final key in ['activityId', 'communityId', 'organizationId']) {
      if (d.containsKey(key) &&
          (d[key] is! String || (d[key] != '' && !humanControlID(d[key]))))
        return false;
    }
    if (d['cityId'] == '' &&
        ((d['placeId'] == ''
                ? d['locationPrecision'] != 'none'
                : d['locationPrecision'] != 'place') ||
            const [
              'activityId',
              'communityId',
              'organizationId',
            ].any((key) => d.containsKey(key) && d[key] != ''))) {
      return false;
    }
    if (!const {
          'unknown',
          'year',
          'month',
          'day',
          'instant',
        }.contains(d['timePrecision']) ||
        !const {'none', 'city', 'place'}.contains(d['locationPrecision']) ||
        d['locationPrecision'] == 'place' && d['placeId'] == '')
      return false;
    if (d['timePrecision'] == 'unknown') return d['occurredAt'] == null;
    try {
      final t = humanControlTime(d['occurredAt']);
      return t.year >= 1 && t.year <= 9999;
    } catch (_) {
      return false;
    }
  }

  String get inputHash {
    final d = input;
    if (d == null) return '';
    String b(String key) => base64Encode(utf8.encode(d[key] as String));
    final at = d['occurredAt'] == null
        ? ''
        : momentUTC(humanControlTime(d['occurredAt']));
    return momentHash([
      'moment-input-v1',
      b('cityId'),
      d['placeId'],
      b('title'),
      b('body'),
      at,
      d['timePrecision'],
      d['locationPrecision'],
      d['activityId'],
      d['communityId'],
      d['organizationId'],
      if (d.containsKey('locations')) ...[
        decodeMomentLocationInputs(
              d['locations'],
            ).any((v) => v.kind == 'REFERENCE')
            ? 'moment-locations-v2'
            : 'moment-locations-v1',
        decodeMomentLocationInputs(
          d['locations'],
        ).map((value) => value.digestTuple).toList(),
      ],
    ]);
  }

  String get digest => momentHash([
    'human-moment-control-v1',
    operationID,
    action,
    momentID,
    expectedRevision,
    inputHash,
    if (media != null) ...[
      'moment-media-v1',
      localScope,
      media!
          .map(
            (selection) => [
              selection.mediaID,
              selection.version,
              selection.rights.digest,
            ],
          )
          .toList(),
    ],
  ]);
  Map<String, Object> toJson() => {
    'operationId': operationID,
    'action': action,
    'expectedRevision': expectedRevision,
    if (action != 'CREATE') 'momentId': momentID,
    if (input != null) 'input': input!,
    if (media != null)
      'media': media!.map((selection) => selection.toJson()).toList(),
    if (localScope.isNotEmpty) 'localScope': localScope,
  };
}

class MomentControlMaterial {
  const MomentControlMaterial(this.stage, this.thumbnail);
  final PublicMomentMediaStage stage;
  final Uint8List thumbnail;
}

class MomentControlPreview {
  const MomentControlPreview({
    required this.input,
    required this.sourceVersion,
    required this.approval,
    required this.observedAt,
    required this.expiresAt,
    required this.preservedPlaceUnavailable,
    this.materials = const [],
    this.preservedMedia,
    this.locations = const [],
    this.locationsSourceVersion,
  });
  final MomentControlInput input;
  final String sourceVersion, approval;
  final DateTime observedAt, expiresAt;
  final bool preservedPlaceUnavailable;
  final List<MomentControlMaterial> materials;
  final PublicMomentMediaSet? preservedMedia;
  final List<MomentLocationResolved> locations;
  final String? locationsSourceVersion;
  bool get live =>
      DateTime.now().toUtc().isBefore(expiresAt) &&
      materials.every((material) => material.stage.current) &&
      (input.media?.every((selection) => selection.rights.valid) ?? true) &&
      locations.every((location) => !location.available || location.current) &&
      (preservedMedia?.current ?? true);
  Map<String, Object> get body => {
    ...input.toJson(),
    'sourceVersion': sourceVersion,
    'approval': approval,
  };
  factory MomentControlPreview.fromJson(
    Object? raw,
    MomentControlInput expected,
  ) {
    final m = humanControlObject(
      raw,
      {
        'operationId',
        'action',
        'payloadDigest',
        'observedAt',
        'expiresAt',
        'approval',
        'expectedRevision',
        'inputSha256',
        'sourceVersion',
        'preservedPlaceUnavailable',
        if (expected.action != 'CREATE') 'momentId',
        if (expected.action != 'WITHDRAW') 'input',
      },
      optional: {
        'materials',
        'preservedMedia',
        'locations',
        'locationsSourceVersion',
      },
    );
    if (m['operationId'] != expected.operationID ||
        m['action'] != expected.action ||
        m['expectedRevision'] != expected.expectedRevision ||
        (m['momentId'] ?? '') != expected.momentID ||
        m['payloadDigest'] != expected.digest ||
        m['inputSha256'] != expected.inputHash ||
        !humanControlHash(m['sourceVersion']) ||
        m['preservedPlaceUnavailable'] is! bool ||
        m['approval'] is! String ||
        (m['approval'] as String).isEmpty ||
        utf8.encode(m['approval']).length > 8192 ||
        (m['approval'] as String).contains('\u0000'))
      throw const FormatException('动态具体批准不符');
    if (expected.input != null) {
      final echo = humanControlObject(
        m['input'],
        {
          'cityId',
          'placeId',
          'title',
          'body',
          'timePrecision',
          'locationPrecision',
          'occurredAt',
        },
        optional: {'activityId', 'communityId', 'organizationId', 'locations'},
      );
      if (echo.isEmpty) throw const FormatException('动态正文不符');
    }
    final echo = expected.input == null
        ? null
        : Map<String, Object?>.from(m['input'] as Map);
    if (echo != null) {
      // Go always serializes occurredAt:null. Normalize this harmless omission only.
      if (echo['occurredAt'] == null) echo.remove('occurredAt');
      final echoed = MomentControlInput(
        operationID: expected.operationID,
        action: expected.action,
        momentID: expected.momentID,
        expectedRevision: expected.expectedRevision,
        input: echo,
        media: expected.media,
        localScope: expected.localScope,
      );
      if (echoed.inputHash != expected.inputHash)
        throw const FormatException('动态规范正文不符');
    }
    final preserved = m['preservedPlaceUnavailable'] as bool;
    final at = humanControlTime(m['observedAt']),
        until = humanControlTime(m['expiresAt']);
    humanControlLease(at, until);
    if (until.isAfter(at.add(const Duration(seconds: 30))))
      throw const FormatException('批准期限不符');
    final rawMaterials = m['materials'] ?? const [];
    final selections = expected.media ?? const <PublicMomentMediaSelection>[];
    if (rawMaterials is! List ||
        rawMaterials.length != selections.length ||
        selections.isNotEmpty && !m.containsKey('materials')) {
      throw const FormatException('动态批准材料范围不符');
    }
    final materials = <MomentControlMaterial>[];
    for (var i = 0; i < selections.length; i++) {
      final value = humanControlObject(rawMaterials[i], {
        'material',
        'thumbnailBase64',
      });
      final stage = PublicMomentMediaStage.decode(value['material']);
      if (stage.mediaID != selections[i].mediaID ||
          stage.version != selections[i].version ||
          stage.localScope != expected.localScope ||
          !stage.current ||
          !selections[i].rights.valid ||
          value['thumbnailBase64'] is! String) {
        throw const FormatException('动态批准原材料已变化');
      }
      final thumbnail = base64Decode(value['thumbnailBase64'] as String);
      momentMediaJPEG(
        thumbnail,
        stage.thumbnailBytes,
        stage.thumbnailSHA,
        320,
        180,
      );
      materials.add(MomentControlMaterial(stage, thumbnail));
    }
    if (materials.any((material) => material.stage.isVideo) &&
        materials.length != 1) {
      throw const FormatException('一条动态使用图片或一段视频');
    }
    PublicMomentMediaSet? preservedMedia;
    if (m['preservedMedia'] != null) {
      if (expected.action != 'UPDATE_PUBLIC' || expected.media != null) {
        throw const FormatException('动态保留材料范围不符');
      }
      preservedMedia = PublicMomentMediaSet.decode(
        m['preservedMedia'],
        expected.momentID,
        expectedRevision: expected.expectedRevision,
        own: true,
      );
      if (preservedMedia.status != 'READY' ||
          preservedMedia.attachments.isEmpty) {
        throw const FormatException('动态原媒体已不可用');
      }
    }
    final locations = <MomentLocationResolved>[];
    if (m.containsKey('locations') != m.containsKey('locationsSourceVersion') ||
        expected.input?.containsKey('locations') == true &&
            !m.containsKey('locations')) {
      throw const FormatException('动态位置批准范围不符');
    }
    if (m.containsKey('locations')) {
      final raw = m['locations'];
      if (raw is! List ||
          raw.length > momentLocationMaxLinks ||
          !humanControlHash(m['locationsSourceVersion']))
        throw const FormatException('动态位置批准来源不符');
      for (var i = 0; i < raw.length; i++)
        locations.add(MomentLocationResolved.decode(raw[i], i));
      if (locations.map((v) => v.id).toSet().length != locations.length)
        throw const FormatException('动态位置批准重复');
      if (expected.input?.containsKey('locations') == true &&
          jsonEncode(locations.map((v) => v.input.toJson()).toList()) !=
              jsonEncode(
                decodeMomentLocationInputs(
                  expected.input!['locations'],
                ).map((v) => v.toJson()).toList(),
              ))
        throw const FormatException('动态实际位置与选择不符');
    }
    if (preserved) {
      final currentLocationsContract =
          m.containsKey('locationsSourceVersion') &&
          const {
            'UPDATE',
            'PUBLISH',
            'UPDATE_PUBLIC',
          }.contains(expected.action) &&
          locations.any((location) => !location.available);
      final legacyPreservation =
          !m.containsKey('locationsSourceVersion') &&
          expected.action == 'UPDATE' &&
          expected.input?['locationPrecision'] != 'place';
      if (!currentLocationsContract && !legacyPreservation) {
        throw const FormatException('此前关联地点不符');
      }
    }
    return MomentControlPreview(
      input: expected,
      sourceVersion: m['sourceVersion'],
      approval: m['approval'],
      observedAt: at,
      expiresAt: until,
      preservedPlaceUnavailable: preserved,
      materials: List.unmodifiable(materials),
      preservedMedia: preservedMedia,
      locations: List.unmodifiable(locations),
      locationsSourceVersion: m['locationsSourceVersion'],
    );
  }
}

class MomentControlReceipt {
  const MomentControlReceipt({
    required this.operationID,
    required this.status,
    required this.appliedRevision,
    this.action = '',
    this.momentID = '',
    this.expectedRevision,
    this.payloadDigest = '',
  });
  final String operationID, status, action, momentID, payloadDigest;
  final int appliedRevision;
  final int? expectedRevision;
  bool get succeeded => status == 'SUCCEEDED';
  factory MomentControlReceipt.fromJson(
    Object? raw, {
    required String operationID,
    String? action,
    String? momentID,
    int? expectedRevision,
    String? digest,
  }) {
    if (raw is! Map<String, dynamic>) throw const FormatException('原动态回执不符');
    final stop = raw['status'] == 'STOPPED';
    final m = humanControlObject(
      raw,
      stop
          ? {'operationId', 'status', 'appliedRevision'}
          : {
              'operationId',
              'status',
              'appliedRevision',
              'action',
              'momentId',
              'expectedRevision',
              'payloadDigest',
              'committedAt',
            },
    );
    if (m['operationId'] != operationID ||
        !humanControlID(operationID) ||
        !humanControlVersion(m['appliedRevision']))
      throw const FormatException('原动态回执不符');
    if (stop) {
      if (m['appliedRevision'] != 0) throw const FormatException('停止结果不符');
      return MomentControlReceipt(
        operationID: operationID,
        status: 'STOPPED',
        appliedRevision: 0,
      );
    }
    final a = m['action'], e = m['expectedRevision'];
    if (m['status'] != 'SUCCEEDED' ||
        !const {
          'CREATE',
          'UPDATE',
          'WITHDRAW',
          'PUBLISH',
          'UPDATE_PUBLIC',
        }.contains(a) ||
        !humanControlID(m['momentId']) ||
        !humanControlVersion(e) ||
        !humanControlHash(m['payloadDigest']) ||
        action != null && action != a ||
        momentID != null && momentID.isNotEmpty && momentID != m['momentId'] ||
        expectedRevision != null && expectedRevision != e ||
        digest != null && digest != m['payloadDigest'])
      throw const FormatException('原动态事实不符');
    if (a == 'CREATE'
        ? e != 0
        : e < 1 ||
              const {'UPDATE', 'PUBLISH', 'UPDATE_PUBLIC'}.contains(a) &&
                  e >= momentControlMaxRevision)
      throw const FormatException('动态版本不符');
    final want = a == 'WITHDRAW' && e == momentControlMaxRevision ? e : e + 1;
    if (m['appliedRevision'] != want) throw const FormatException('动态应用版本不符');
    humanControlTime(m['committedAt']);
    return MomentControlReceipt(
      operationID: operationID,
      status: 'SUCCEEDED',
      appliedRevision: want,
      action: a,
      momentID: m['momentId'],
      expectedRevision: e,
      payloadDigest: m['payloadDigest'],
    );
  }
}

class MomentControlAPI {
  MomentControlAPI({
    required http.Client client,
    required String apiBaseUrl,
    required String? Function() authorizationHeader,
  }) : api = HumanCausalApi(
         client: client,
         baseUrl: apiBaseUrl,
         authorizationHeader: authorizationHeader,
       );
  final HumanCausalApi api;
  Future<MomentControlPreview> preview(
    MomentControlInput input,
    bool Function() current,
  ) async => MomentControlPreview.fromJson(
    await api.json(
      'POST',
      '/v1/me/moment-controls/previews',
      body: input.toJson(),
      current: current,
    ),
    input,
  );
  Future<MomentControlReceipt> put(
    MomentControlPreview p,
    bool Function() current,
  ) async => MomentControlReceipt.fromJson(
    await api.json(
      'PUT',
      '/v1/me/moment-controls/operations/${p.input.operationID}',
      body: p.body,
      current: current,
    ),
    operationID: p.input.operationID,
    action: p.input.action,
    momentID: p.input.momentID,
    expectedRevision: p.input.expectedRevision,
    digest: p.input.digest,
  );
  Future<Object?> original(
    String op,
    bool Function() current, {
    bool stop = false,
  }) => api.json(
    stop ? 'POST' : 'GET',
    '/v1/me/moment-controls/operations/$op${stop ? '/stop' : ''}',
    current: current,
  );
}
