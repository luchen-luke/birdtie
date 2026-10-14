import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import 'package:http/http.dart' as http;

import '../config/birdtie_environment.dart';

const agentActivityJoinSchema = 'air.activity_join.v1';
const agentActivityJoinPending = 'AWAITING_APPROVAL';
const agentActivityJoinApproved = 'APPROVED_NOT_EXECUTED';

bool activityJoinID(Object? v) =>
    v is String &&
    RegExp(
      r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
    ).hasMatch(v) &&
    v != '00000000-0000-0000-0000-000000000000';
bool activityJoinDigest(Object? v) =>
    v is String && RegExp(r'^[0-9a-f]{64}$').hasMatch(v);

/// Check duplicate decoded keys before jsonDecode can discard a binding.
Object? activityJoinJSON(String text, {int maxBytes = 32768}) {
  if (utf8.encode(text).length > maxBytes)
    throw const FormatException('智能体报名记录过大。');
  var i = 0;
  void space() {
    while (i < text.length && ' \r\n\t'.contains(text[i])) {
      i++;
    }
  }

  String string() {
    final start = i++;
    while (i < text.length) {
      if (text[i] == '\\') {
        i += 2;
        continue;
      }
      if (text[i++] == '"')
        return jsonDecode(text.substring(start, i)) as String;
    }
    throw const FormatException('智能体报名JSON无效。');
  }

  void Function(int) value = (_) {};
  value = (int depth) {
    if (depth > 8) throw const FormatException('智能体报名JSON过深。');
    space();
    if (i >= text.length) throw const FormatException('智能体报名JSON无效。');
    if (text[i] == '{') {
      i++;
      space();
      final keys = <String>{};
      if (i < text.length && text[i] == '}') {
        i++;
        return;
      }
      while (true) {
        space();
        if (i >= text.length || text[i] != '"' || !keys.add(string()))
          throw const FormatException('智能体报名JSON键冲突。');
        space();
        if (i >= text.length || text[i++] != ':')
          throw const FormatException('智能体报名JSON无效。');
        value(depth + 1);
        space();
        if (i >= text.length) throw const FormatException('智能体报名JSON无效。');
        final end = text[i++];
        if (end == '}') return;
        if (end != ',') throw const FormatException('智能体报名JSON无效。');
      }
    }
    if (text[i] == '[') {
      i++;
      space();
      if (i < text.length && text[i] == ']') {
        i++;
        return;
      }
      while (true) {
        value(depth + 1);
        space();
        if (i >= text.length) throw const FormatException('智能体报名JSON无效。');
        final end = text[i++];
        if (end == ']') return;
        if (end != ',') throw const FormatException('智能体报名JSON无效。');
      }
    }
    if (text[i] == '"') {
      string();
      return;
    }
    final start = i;
    while (i < text.length && !' \r\n\t,}]'.contains(text[i])) {
      i++;
    }
    if (i == start) throw const FormatException('智能体报名JSON无效。');
    jsonDecode(text.substring(start, i));
  };
  value(0);
  space();
  if (i != text.length) throw const FormatException('智能体报名JSON尾部无效。');
  return jsonDecode(text);
}

String activityJoinUUID() {
  final random = Random.secure();
  final bytes = List.generate(16, (_) => random.nextInt(256));
  bytes[6] = (bytes[6] & 15) | 64;
  bytes[8] = (bytes[8] & 63) | 128;
  final s = bytes.map((v) => v.toRadixString(16).padLeft(2, '0')).join();
  return '${s.substring(0, 8)}-${s.substring(8, 12)}-${s.substring(12, 16)}-${s.substring(16, 20)}-${s.substring(20)}';
}

Map<String, dynamic> activityJoinObject(Object? raw, Set<String> keys) {
  if (raw is! Map<String, dynamic> ||
      raw.length != keys.length ||
      raw.keys.any((k) => !keys.contains(k))) {
    throw const FormatException('智能体报名回执不完整，请核实原操作。');
  }
  return raw;
}

DateTime activityJoinTime(Object? raw) {
  if (raw is! String ||
      !RegExp(
        r'^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?Z$',
      ).hasMatch(raw)) {
    throw const FormatException('智能体报名时间无效。');
  }
  final at = DateTime.tryParse(raw);
  if (at == null ||
      at.year < 2000 ||
      at.year > 2200 ||
      at.toUtc().toIso8601String().substring(0, 19) != raw.substring(0, 19)) {
    throw const FormatException('智能体报名时间无效。');
  }
  return at.toUtc();
}

bool _text(Object? v, int bytes, {bool nullable = false, bool empty = true}) =>
    nullable && v == null ||
    v is String &&
        (empty || v.trim().isNotEmpty) &&
        utf8.encode(v).length <= bytes &&
        !v.contains('\u0000');
void _check(bool valid) {
  if (!valid) throw const FormatException('智能体报名回执与原操作不符。');
}

String _canonical(Object? value) {
  Object? sorted(Object? v) => v is Map
      ? {
          for (final k in (v.keys.cast<String>().toList()..sort()))
            k: sorted(v[k]),
        }
      : v is List
      ? v.map(sorted).toList()
      : v;
  return jsonEncode(sorted(value));
}

/// An original Now source selector. It is not an approval or execution grant.
class AgentActivityJoinSelection {
  const AgentActivityJoinSelection({
    required this.taskID,
    required this.activityID,
    required this.expectedTaskUpdatedAt,
    required this.replyTurnDigest,
  });
  final String taskID, activityID, replyTurnDigest;
  final DateTime expectedTaskUpdatedAt;
  bool get valid =>
      activityJoinID(taskID) &&
      activityJoinID(activityID) &&
      activityJoinDigest(replyTurnDigest) &&
      expectedTaskUpdatedAt.year >= 2000 &&
      expectedTaskUpdatedAt.year <= 2200;
  @override
  bool operator ==(Object o) =>
      o is AgentActivityJoinSelection &&
      o.taskID == taskID &&
      o.activityID == activityID &&
      o.replyTurnDigest == replyTurnDigest &&
      o.expectedTaskUpdatedAt.isAtSameMomentAs(expectedTaskUpdatedAt);
  @override
  int get hashCode => Object.hash(
    taskID,
    activityID,
    replyTurnDigest,
    expectedTaskUpdatedAt.microsecondsSinceEpoch,
  );
}

class AgentActivityJoinReview {
  AgentActivityJoinReview._(this.raw);
  final Map<String, dynamic> raw;
  String get title => raw['title'];
  String get summary => raw['summary'];
  String get description => raw['description'];
  String get activityID => raw['activityId'];
  DateTime get startsAt => activityJoinTime(raw['startsAt']);
  DateTime get endsAt => activityJoinTime(raw['endsAt']);
  String get timeZone => raw['timeZone'];
  String get modality => raw['modality'];
  String? get locationLabel => raw['locationLabel'];
  String? get hostLabel => raw['hostLabel'];
  String? get organizerName => (raw['organizer'] as Map)['name'];
  String get organizerType => (raw['organizer'] as Map)['type'];
  int? get priceMinor => raw['priceMinor'];
  String? get currency => raw['currency'];
  String? get eligibility => raw['eligibility'];
  int? get capacity => raw['capacity'];
  int? get participantCount => raw['participantCount'];
  bool same(AgentActivityJoinReview other) =>
      _canonical(raw) == _canonical(other.raw);

  factory AgentActivityJoinReview.read(Object? value, String activityID) {
    final m = activityJoinObject(value, {
      'schemaVersion',
      'activityId',
      'targetVersion',
      'title',
      'summary',
      'description',
      'hostLabel',
      'organizer',
      'startsAt',
      'endsAt',
      'timeZone',
      'modality',
      'locationLabel',
      'physicalPlaceStatus',
      'priceMinor',
      'currency',
      'eligibility',
      'visibility',
      'capacity',
      'participantCount',
    });
    final organizer = activityJoinObject(m['organizer'], {
      'type',
      'id',
      'name',
    });
    _check(
      m['schemaVersion'] == 'activity-join-review-v1' &&
          m['activityId'] == activityID &&
          activityJoinID(activityID) &&
          activityJoinDigest(m['targetVersion']) &&
          _text(m['title'], 1024, empty: false) &&
          _text(m['summary'], 2048) &&
          _text(m['description'], 16384) &&
          _text(m['hostLabel'], 512, nullable: true, empty: false) &&
          const {
            'PERSON',
            'ORGANIZATION',
            'BUSINESS',
            'COMMUNITY',
          }.contains(organizer['type']) &&
          activityJoinID(organizer['id']) &&
          _text(organizer['name'], 512, nullable: true, empty: false) &&
          _text(m['timeZone'], 128, empty: false) &&
          const {
            'unspecified',
            'in_person',
            'online',
            'hybrid',
          }.contains(m['modality']) &&
          _text(m['locationLabel'], 1024, nullable: true, empty: false) &&
          (m['physicalPlaceStatus'] == null ||
              const {
                'unknown',
                'confirmed',
                'tbd',
                'not_applicable',
              }.contains(m['physicalPlaceStatus'])) &&
          _text(m['eligibility'], 2048, nullable: true, empty: false) &&
          m['visibility'] == 'public' &&
          (m['priceMinor'] == null && m['currency'] == null ||
              m['priceMinor'] is int &&
                  m['priceMinor'] >= 0 &&
                  m['priceMinor'] <= 2147483647 &&
                  m['currency'] is String &&
                  RegExp(r'^[A-Z]{3}$').hasMatch(m['currency'])) &&
          (m['capacity'] == null ||
              m['capacity'] is int &&
                  m['capacity'] > 0 &&
                  m['capacity'] <= 2147483647) &&
          (m['participantCount'] == null ||
              m['participantCount'] is int &&
                  m['participantCount'] >= 0 &&
                  m['participantCount'] <= 2147483647),
    );
    _check(
      activityJoinTime(m['endsAt']).isAfter(activityJoinTime(m['startsAt'])),
    );
    return AgentActivityJoinReview._(
      Map.unmodifiable({
        ...m,
        'organizer': Map<String, dynamic>.unmodifiable(organizer),
      }),
    );
  }
}

const _previewKeys = {
  'schemaVersion',
  'owner',
  'agentId',
  'taskId',
  'approvalId',
  'logicalOperationId',
  'actionId',
  'activityId',
  'bindingDigest',
  'state',
  'observedAt',
  'expiresAt',
  'dispatchCommitted',
  'review',
};

class AgentActivityJoinPreview {
  AgentActivityJoinPreview._(this.raw, this.review);
  final Map<String, dynamic> raw;
  final AgentActivityJoinReview? review;
  String get agentID => raw['agentId'];
  String get approvalID => raw['approvalId'];
  String get taskID => raw['taskId'];
  String get activityID => raw['activityId'];
  String get logicalOperationID => raw['logicalOperationId'];
  String get actionID => raw['actionId'];
  String get bindingDigest => raw['bindingDigest'];
  String get state => raw['state'];
  bool get dispatchCommitted => raw['dispatchCommitted'];
  DateTime get observedAt => activityJoinTime(raw['observedAt']);
  DateTime get expiresAt => activityJoinTime(raw['expiresAt']);
  bool get freshState => const {
    agentActivityJoinPending,
    agentActivityJoinApproved,
  }.contains(state);
  bool sameBinding(AgentActivityJoinPreview p) =>
      _canonical({
        for (final e in raw.entries)
          if (!const {'state', 'review', 'dispatchCommitted'}.contains(e.key))
            e.key: e.value,
      }) ==
      _canonical({
        for (final e in p.raw.entries)
          if (!const {'state', 'review', 'dispatchCommitted'}.contains(e.key))
            e.key: e.value,
      });

  factory AgentActivityJoinPreview.read(Object? value, String owner) {
    final m = activityJoinObject(value, _previewKeys);
    final o = activityJoinObject(m['owner'], {'type', 'id'});
    _check(
      m['schemaVersion'] == agentActivityJoinSchema &&
          o['type'] == 'PERSON' &&
          o['id'] == owner &&
          activityJoinID(owner),
    );
    _check(
      const [
            'agentId',
            'taskId',
            'approvalId',
            'logicalOperationId',
            'actionId',
            'activityId',
          ].every((k) => activityJoinID(m[k])) &&
          activityJoinDigest(m['bindingDigest']) &&
          m['dispatchCommitted'] is bool,
    );
    _check(
      const {
        agentActivityJoinPending,
        agentActivityJoinApproved,
        'DISPATCH_COMMITTED',
        'CANCELLED',
      }.contains(m['state']),
    );
    final observed = activityJoinTime(m['observedAt']),
        end = activityJoinTime(m['expiresAt']);
    _check(
      end.isAfter(observed) &&
          end.difference(observed) <= const Duration(seconds: 30),
    );
    final fresh = const {
      agentActivityJoinPending,
      agentActivityJoinApproved,
    }.contains(m['state']);
    _check(!fresh || m['dispatchCommitted'] == false && m['review'] != null);
    _check(
      m['state'] != 'DISPATCH_COMMITTED' || m['dispatchCommitted'] == true,
    );
    final review = m['review'] == null
        ? null
        : AgentActivityJoinReview.read(m['review'], m['activityId']);
    return AgentActivityJoinPreview._(
      Map.unmodifiable({
        ...m,
        'owner': Map<String, dynamic>.unmodifiable(o),
        'review': review?.raw,
      }),
      review,
    );
  }
}

class AgentActivityJoinReceipt {
  AgentActivityJoinReceipt._(this.raw);
  final Map<String, dynamic> raw;
  String get agentID => raw['agentId'];
  String get approvalID => raw['approvalId'];
  String get dispatchID => raw['dispatchId'];
  String get status => raw['status'];
  String? get participationID => raw['participationId'];
  bool get terminal => const {'SUCCEEDED', 'NO_EFFECT'}.contains(status);
  factory AgentActivityJoinReceipt.read(
    Object? value,
    String owner,
    String approvalID, {
    String? agentID,
  }) {
    if (value is! Map<String, dynamic>)
      throw const FormatException('智能体报名回执不完整。');
    final succeeded = value['status'] == 'SUCCEEDED';
    final m = activityJoinObject(value, {
      'schemaVersion',
      'owner',
      'agentId',
      'approvalId',
      'dispatchId',
      'status',
      'committedAt',
      if (succeeded) ...{'effectId', 'participationId', 'appliedAt'},
    });
    final o = activityJoinObject(m['owner'], {'type', 'id'});
    _check(
      m['schemaVersion'] == agentActivityJoinSchema &&
          o['type'] == 'PERSON' &&
          o['id'] == owner &&
          activityJoinID(owner) &&
          activityJoinID(approvalID) &&
          m['approvalId'] == approvalID &&
          activityJoinID(m['agentId']) &&
          (agentID == null || m['agentId'] == agentID) &&
          activityJoinID(m['dispatchId']) &&
          const {
            'DISPATCH_COMMITTED',
            'IN_FLIGHT',
            'UNKNOWN_OUTCOME',
            'NO_EFFECT',
            'SUCCEEDED',
          }.contains(m['status']),
    );
    final committed = activityJoinTime(m['committedAt']);
    if (succeeded)
      _check(
        activityJoinID(m['effectId']) &&
            activityJoinID(m['participationId']) &&
            !activityJoinTime(m['appliedAt']).isBefore(committed),
      );
    return AgentActivityJoinReceipt._(
      Map.unmodifiable({...m, 'owner': Map<String, dynamic>.unmodifiable(o)}),
    );
  }
}

class AgentActivityJoinFailure implements Exception {
  const AgentActivityJoinFailure(this.status, this.code);
  final int status;
  final String code;
}

class AgentActivityJoinAPI {
  AgentActivityJoinAPI({http.Client? client, String? apiBaseUrl})
    : _client = client ?? http.Client(),
      _owns = client == null,
      base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final http.Client _client;
  final bool _owns;
  final String base;
  String get environment {
    if (base.isEmpty) {
      final origin = Uri.base;
      if (const {'http', 'https'}.contains(origin.scheme) &&
          origin.hasAuthority)
        return origin.origin;
      throw const FormatException('智能体报名服务地址不可用。');
    }
    final u = Uri.tryParse(base);
    if (u == null ||
        !u.hasAuthority ||
        !const {'http', 'https'}.contains(u.scheme) ||
        u.host.isEmpty ||
        u.userInfo.isNotEmpty ||
        u.hasQuery ||
        u.hasFragment ||
        base.endsWith('//'))
      throw const FormatException('智能体报名服务地址无效。');
    return base.endsWith('/') ? base.substring(0, base.length - 1) : base;
  }

  Uri _uri(String suffix) =>
      Uri.parse('$environment/v1/me/agent-activity-join$suffix');
  Future<Object?> _request(
    String token,
    String suffix, {
    Object? body,
    required int status,
  }) async {
    if (token.isEmpty || token.trim() != token)
      throw const FormatException('请先以本人账号登录。');
    final request = http.Request('POST', _uri(suffix));
    request.headers['Authorization'] = token;
    if (body != null) {
      request.headers['Content-Type'] = 'application/json';
      request.body = jsonEncode(body);
    }
    Future<(http.StreamedResponse, Uint8List)> receive() async {
      final response = await _client.send(request);
      final bytes = BytesBuilder(copy: false);
      await for (final chunk in response.stream) {
        if (bytes.length + chunk.length > 32768)
          throw const FormatException('智能体报名回执过大，请核实原操作。');
        bytes.add(chunk);
      }
      return (response, bytes.takeBytes());
    }

    final (r, bytes) = await receive().timeout(const Duration(seconds: 12));
    if (r.statusCode != status) {
      var code = '';
      try {
        final error = activityJoinJSON(utf8.decode(bytes));
        if (error is Map && error['error'] is String) code = error['error'];
      } catch (_) {}
      throw AgentActivityJoinFailure(r.statusCode, code);
    }
    if (!(r.headers['content-type'] ?? '').toLowerCase().startsWith(
      'application/json',
    ))
      throw const FormatException('智能体报名回执格式无效。');
    return activityJoinObject(activityJoinJSON(utf8.decode(bytes)), {
      'data',
    })['data'];
  }

  Future<AgentActivityJoinPreview> preview(
    String token,
    String owner,
    AgentActivityJoinSelection selection,
    String logicalID,
    String actionID,
  ) async {
    _check(
      selection.valid &&
          activityJoinID(logicalID) &&
          activityJoinID(actionID) &&
          activityJoinID(owner),
    );
    final p = AgentActivityJoinPreview.read(
      await _request(
        token,
        '/previews',
        status: 201,
        body: {
          'taskId': selection.taskID,
          'logicalOperationId': logicalID,
          'actionId': actionID,
          'activityId': selection.activityID,
          'expectedTaskUpdatedAt': selection.expectedTaskUpdatedAt
              .toUtc()
              .toIso8601String(),
          'replyTurnDigest': selection.replyTurnDigest,
        },
      ),
      owner,
    );
    _check(
      p.taskID == selection.taskID &&
          p.activityID == selection.activityID &&
          p.logicalOperationID == logicalID &&
          p.actionID == actionID &&
          p.state == agentActivityJoinPending,
    );
    return p;
  }

  Future<AgentActivityJoinPreview> confirm(
    String token,
    String owner,
    AgentActivityJoinPreview original,
  ) async {
    final p = AgentActivityJoinPreview.read(
      await _request(
        token,
        '/approvals/${original.approvalID}/confirm',
        status: 200,
        body: {'bindingDigest': original.bindingDigest},
      ),
      owner,
    );
    _check(
      p.sameBinding(original) &&
          p.state == agentActivityJoinApproved &&
          p.review != null &&
          original.review != null &&
          p.review!.same(original.review!),
    );
    return p;
  }

  Future<AgentActivityJoinReceipt> execute(
    String token,
    String owner,
    String approvalID,
    String digest,
    String agentID,
  ) async {
    _check(
      activityJoinID(approvalID) &&
          activityJoinID(agentID) &&
          activityJoinDigest(digest),
    );
    return AgentActivityJoinReceipt.read(
      await _request(
        token,
        '/approvals/$approvalID/execute',
        status: 200,
        body: {'bindingDigest': digest},
      ),
      owner,
      approvalID,
      agentID: agentID,
    );
  }

  Future<AgentActivityJoinReceipt> reconcile(
    String token,
    String owner,
    String approvalID,
    String agentID,
  ) async {
    _check(activityJoinID(approvalID) && activityJoinID(agentID));
    return AgentActivityJoinReceipt.read(
      await _request(token, '/approvals/$approvalID/reconcile', status: 200),
      owner,
      approvalID,
      agentID: agentID,
    );
  }

  Future<AgentActivityJoinPreview> revoke(
    String token,
    String owner,
    String approvalID,
    String agentID,
  ) async {
    _check(activityJoinID(approvalID) && activityJoinID(agentID));
    final p = AgentActivityJoinPreview.read(
      await _request(token, '/approvals/$approvalID/revoke', status: 200),
      owner,
    );
    _check(
      p.approvalID == approvalID &&
          p.agentID == agentID &&
          p.state == 'CANCELLED',
    );
    return p;
  }

  void dispose() {
    if (_owns) _client.close();
  }
}
