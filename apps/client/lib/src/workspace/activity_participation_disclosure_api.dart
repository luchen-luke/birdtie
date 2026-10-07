import 'dart:convert';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'model_egress_api.dart' show egressTime, egressIDValid;

Map<String, dynamic> _map(dynamic v) {
  if (v is! Map<String, dynamic>) throw const FormatException('报名结构无效');
  return v;
}

void _keys(
  Map<String, dynamic> m,
  Set<String> required, [
  Set<String> optional = const {},
]) {
  if (!m.keys.toSet().containsAll(required) ||
      !required.union(optional).containsAll(m.keys)) {
    throw const FormatException('报名字段无效');
  }
}

String _id(dynamic v) {
  if (!egressIDValid(v)) throw const FormatException('报名标识无效');
  return v as String;
}

String _text(dynamic v, [int max = 4096]) {
  if (v is! String || v.trim().isEmpty || v.length > max) {
    throw const FormatException('报名文本无效');
  }
  return v;
}

const _recordKeys = {
  'participationId',
  'activityId',
  'title',
  'status',
  'sourceAvailable',
  'visibility',
  'effectivePublic',
  'attendance',
};
const _recordOptional = {
  'startsAt',
  'endsAt',
  'sourceExpiresAt',
  'disclosureExpiresAt',
};
const _identityKeys = {
  'schemaVersion',
  'ownerId',
  'agentId',
  'observedAt',
  'modelAccess',
  'sendAllowed',
  'membershipGranted',
};
void _identity(Map<String, dynamic> m, String owner) {
  if (m['schemaVersion'] != 'human-activity-participation-disclosure-v1' ||
      _id(m['ownerId']) != owner ||
      m['modelAccess'] != false ||
      m['sendAllowed'] != false ||
      m['membershipGranted'] != false) {
    throw const FormatException('报名主体或权限不符');
  }
  _id(m['agentId']);
}

class ParticipationDisclosureRecord {
  ParticipationDisclosureRecord(
    Map<String, dynamic> m,
    DateTime observedAt, {
    bool preview = false,
  }) : participationID = _id(m['participationId']),
       activityID = _id(m['activityId']),
       title = _text(m['title'], 320),
       status = _text(m['status']),
       visibility = _text(m['visibility']) {
    if (!preview) _keys(m, _recordKeys, _recordOptional);
    if (!['going', 'pending', 'cancelled'].contains(status) ||
        !['PUBLIC', 'PRIVATE'].contains(visibility) ||
        m['attendance'] != 'UNKNOWN' ||
        m['sourceAvailable'] is! bool ||
        m['effectivePublic'] is! bool) {
      throw const FormatException('报名状态无效');
    }
    sourceAvailable = m['sourceAvailable'] as bool;
    effectivePublic = m['effectivePublic'] as bool;
    startsAt = m.containsKey('startsAt') ? egressTime(m['startsAt']) : null;
    endsAt = m.containsKey('endsAt') ? egressTime(m['endsAt']) : null;
    sourceExpiresAt = m.containsKey('sourceExpiresAt')
        ? egressTime(m['sourceExpiresAt'])
        : null;
    disclosureExpiresAt = m.containsKey('disclosureExpiresAt')
        ? egressTime(m['disclosureExpiresAt'])
        : null;
    if (!sourceAvailable &&
        (title != '已不可公开展示的活动报名' ||
            startsAt != null ||
            endsAt != null ||
            sourceExpiresAt != null)) {
      throw const FormatException('隐藏报名泄露来源');
    }
    if (sourceAvailable &&
        (status != 'going' ||
            startsAt == null ||
            endsAt == null ||
            sourceExpiresAt == null ||
            !startsAt!.isBefore(endsAt!) ||
            !sourceExpiresAt!.isAfter(observedAt) ||
            sourceExpiresAt!.isAfter(endsAt!))) {
      throw const FormatException('报名来源无效');
    }
    if (visibility == 'PRIVATE' &&
            (disclosureExpiresAt != null || effectivePublic) ||
        visibility == 'PUBLIC' && disclosureExpiresAt == null) {
      throw const FormatException('公开期限无效');
    }
    if (effectivePublic &&
        (!sourceAvailable ||
            visibility != 'PUBLIC' ||
            !disclosureExpiresAt!.isAfter(observedAt) ||
            disclosureExpiresAt!.isAfter(sourceExpiresAt!))) {
      throw const FormatException('公开状态无效');
    }
  }
  final String participationID, activityID, title, status, visibility;
  late final bool sourceAvailable, effectivePublic;
  late final DateTime? startsAt, endsAt, sourceExpiresAt, disclosureExpiresAt;
}

class ParticipationDisclosureView {
  ParticipationDisclosureView(Map<String, dynamic> m, String owner)
    : ownerID = _id(m['ownerId']),
      agentID = _id(m['agentId']),
      observedAt = egressTime(m['observedAt']) {
    _keys(m, {..._identityKeys, 'records', 'limit', 'truncated'});
    _identity(m, owner);
    if (m['records'] is! List || m['limit'] != 100 || m['truncated'] is! bool) {
      throw const FormatException('报名列表无效');
    }
    records = List.unmodifiable(
      (m['records'] as List).map(
        (v) => ParticipationDisclosureRecord(_map(v), observedAt),
      ),
    );
    truncated = m['truncated'] as bool;
    if (records.length > 100 ||
        records.map((v) => v.participationID).toSet().length !=
            records.length) {
      throw const FormatException('报名重复或超限');
    }
  }
  final String ownerID, agentID;
  final DateTime observedAt;
  late final List<ParticipationDisclosureRecord> records;
  late final bool truncated;
}

class ParticipationDisclosurePreview {
  ParticipationDisclosurePreview(
    Map<String, dynamic> m,
    String owner,
    String id,
    String op,
    DateTime? expiry,
  ) : ownerID = _id(m['ownerId']),
      agentID = _id(m['agentId']),
      observedAt = egressTime(m['observedAt']),
      expiresAt = egressTime(m['expiresAt']),
      token = _text(m['preview'], 24000),
      operation = _text(m['operation']),
      consequence = _text(m['consequence']) {
    _keys(
      m,
      {
        ..._identityKeys,
        ..._recordKeys,
        'operation',
        'targetVisibility',
        'preview',
        'expiresAt',
        'consequence',
      },
      {..._recordOptional, 'targetExpiresAt'},
    );
    _identity(m, owner);
    record = ParticipationDisclosureRecord(m, observedAt, preview: true);
    targetExpiresAt = m.containsKey('targetExpiresAt')
        ? egressTime(m['targetExpiresAt'])
        : null;
    if (record.participationID != id ||
        operation != op ||
        m['targetVisibility'] != op ||
        !['PUBLIC', 'PRIVATE'].contains(op) ||
        targetExpiresAt != expiry ||
        token.length < 24 ||
        !expiresAt.isAfter(observedAt) ||
        expiresAt.isAfter(observedAt.add(const Duration(seconds: 90)))) {
      throw const FormatException('具体预览不符');
    }
    if (op == 'PUBLIC' &&
        (!record.sourceAvailable ||
            expiry == null ||
            !expiry.isAfter(observedAt) ||
            expiry.isAfter(observedAt.add(const Duration(hours: 24))) ||
            expiry.isAfter(record.sourceExpiresAt!) ||
            expiresAt.isAfter(expiry))) {
      throw const FormatException('具体公开期限不符');
    }
    if (op == 'PRIVATE' && targetExpiresAt != null) {
      throw const FormatException('私密预览期限不符');
    }
  }
  final String ownerID, agentID, token, operation, consequence;
  final DateTime observedAt, expiresAt;
  late final DateTime? targetExpiresAt;
  late final ParticipationDisclosureRecord record;
}

class ParticipationDisclosureHTTPError implements Exception {
  const ParticipationDisclosureHTTPError(this.status);
  final int status;
}

class ActivityParticipationDisclosureAPI {
  ActivityParticipationDisclosureAPI({http.Client? client, String? apiBaseUrl})
    : _client = client ?? http.Client(),
      _owned = client == null,
      _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final http.Client _client;
  final bool _owned;
  final String _base;
  bool _closed = false;
  Future<Map<String, dynamic>> _request(
    String method,
    String path,
    String auth, [
    Map<String, dynamic>? body,
  ]) async {
    if (_closed) throw StateError('报名页面已关闭');
    final uri = Uri.parse(
      '${_base.replaceFirst(RegExp(r'/$'), '')}/v1/me/activity-participation-disclosures$path',
    );
    final headers = {'Authorization': auth, 'Content-Type': 'application/json'};
    final response =
        await (method == 'GET'
                ? _client.get(uri, headers: headers)
                : _client.post(uri, headers: headers, body: jsonEncode(body)))
            .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) {
      throw ParticipationDisclosureHTTPError(response.statusCode);
    }
    final m = _map(jsonDecode(utf8.decode(response.bodyBytes)));
    _keys(m, {'data'});
    return _map(m['data']);
  }

  Future<ParticipationDisclosureView> read(
    String auth,
    String owner, {
    bool options = false,
  }) async => ParticipationDisclosureView(
    await _request('GET', options ? '/options' : '', auth),
    owner,
  );
  Future<ParticipationDisclosurePreview> preview(
    String auth,
    String owner,
    String id,
    String operation,
    DateTime? expiry,
  ) async {
    _id(id);
    if (!['PUBLIC', 'PRIVATE'].contains(operation) ||
        (operation == 'PUBLIC') != (expiry != null)) {
      throw const FormatException('请选择具体公开期限');
    }
    return ParticipationDisclosurePreview(
      await _request('POST', '/preview', auth, {
        'participationId': id,
        'operation': operation,
        if (expiry != null)
          'disclosureExpiresAt': expiry.toUtc().toIso8601String(),
      }),
      owner,
      id,
      operation,
      expiry?.toUtc(),
    );
  }

  Future<ParticipationDisclosureView> approve(
    String auth,
    ParticipationDisclosurePreview p,
  ) async {
    final v = ParticipationDisclosureView(
      await _request('POST', '/approve', auth, {'preview': p.token}),
      p.ownerID,
    );
    if (v.agentID != p.agentID ||
        v.records.length != 1 ||
        v.truncated ||
        v.observedAt.isBefore(p.observedAt) ||
        !v.observedAt.isBefore(p.expiresAt)) {
      throw const FormatException('权威结果与预览不符');
    }
    final r = v.records.single;
    if (r.participationID != p.record.participationID ||
        r.activityID != p.record.activityID ||
        r.visibility != p.operation ||
        r.disclosureExpiresAt != p.targetExpiresAt ||
        p.operation == 'PUBLIC' && !r.effectivePublic) {
      throw const FormatException('权威报名与预览不符');
    }
    return v;
  }

  void dispose() {
    if (_closed) return;
    _closed = true;
    if (_owned) _client.close();
  }
}
