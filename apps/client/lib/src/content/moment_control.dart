import 'dart:convert';
import 'dart:math';
import 'package:crypto/crypto.dart';
import 'package:http/http.dart' as http;
import '../workspace/human_causal_control.dart';

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
  }) : input = input == null ? null : Map.unmodifiable(input) {
    if (!valid) throw const FormatException('动态操作输入不符');
  }
  final String operationID, action, momentID;
  final int expectedRevision;
  final Map<String, Object?>? input;
  bool get valid {
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
    if (const {'PUBLISH', 'UPDATE_PUBLIC'}.contains(action) &&
        (!((d['placeId'] == '' && d['locationPrecision'] == 'city') ||
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
        };
    if (!required.every(d.containsKey) ||
        d.keys.any((k) => !required.contains(k) && !optional.contains(k)))
      return false;
    for (final entry in {
      'cityId': (1, 80),
      'title': (1, 160),
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
    ]);
  }

  String get digest => momentHash([
    'human-moment-control-v1',
    operationID,
    action,
    momentID,
    expectedRevision,
    inputHash,
  ]);
  Map<String, Object> toJson() => {
    'operationId': operationID,
    'action': action,
    'expectedRevision': expectedRevision,
    if (action != 'CREATE') 'momentId': momentID,
    if (input != null) 'input': input!,
  };
}

class MomentControlPreview {
  const MomentControlPreview({
    required this.input,
    required this.sourceVersion,
    required this.approval,
    required this.observedAt,
    required this.expiresAt,
    required this.preservedPlaceUnavailable,
  });
  final MomentControlInput input;
  final String sourceVersion, approval;
  final DateTime observedAt, expiresAt;
  final bool preservedPlaceUnavailable;
  bool get live => DateTime.now().toUtc().isBefore(expiresAt);
  Map<String, Object> get body => {
    ...input.toJson(),
    'sourceVersion': sourceVersion,
    'approval': approval,
  };
  factory MomentControlPreview.fromJson(
    Object? raw,
    MomentControlInput expected,
  ) {
    final m = humanControlObject(raw, {
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
    });
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
        optional: {'activityId', 'communityId', 'organizationId'},
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
      );
      if (echoed.inputHash != expected.inputHash)
        throw const FormatException('动态规范正文不符');
    }
    final preserved = m['preservedPlaceUnavailable'] as bool;
    if (preserved &&
        (expected.action != 'UPDATE' ||
            expected.input?['locationPrecision'] == 'place'))
      throw const FormatException('此前关联地点不符');
    final at = humanControlTime(m['observedAt']),
        until = humanControlTime(m['expiresAt']);
    humanControlLease(at, until);
    if (until.isAfter(at.add(const Duration(seconds: 30))))
      throw const FormatException('批准期限不符');
    return MomentControlPreview(
      input: expected,
      sourceVersion: m['sourceVersion'],
      approval: m['approval'],
      observedAt: at,
      expiresAt: until,
      preservedPlaceUnavailable: preserved,
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
