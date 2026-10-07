import 'dart:convert';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';

final _egressID = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
bool egressIDValid(dynamic v) =>
    v is String &&
    _egressID.hasMatch(v) &&
    v != '00000000-0000-0000-0000-000000000000';
String _id(dynamic v) {
  if (!egressIDValid(v)) throw const FormatException('记录标识无效');
  return v as String;
}

String _text(dynamic v) {
  if (v is! String || v.isEmpty || v.length > 4096) {
    throw const FormatException('记录字段无效');
  }
  return v;
}

int _integer(dynamic v, {int max = 1000000000000, int min = 0}) {
  if (v is! int || v < min || v > max) throw const FormatException('预算值无效');
  return v;
}

Map<String, dynamic> _map(dynamic v) {
  if (v is! Map<String, dynamic>) throw const FormatException('记录结构无效');
  return v;
}

dynamic _freeze(dynamic v) => v is Map
    ? Map<String, dynamic>.unmodifiable(
        v.map((k, val) => MapEntry(k as String, _freeze(val))),
      )
    : v is List
    ? List<dynamic>.unmodifiable(v.map(_freeze))
    : v;

/// RFC3339 with real calendar/range validation; DateTime.parse alone normalizes
/// impossible dates. Both Z and signed offsets are legitimate native outputs.
DateTime egressTime(dynamic v) {
  if (v is! String) throw const FormatException('期限无效');
  final m = RegExp(
    r'^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})$',
  ).firstMatch(v);
  if (m == null) throw const FormatException('期限必须带有效时区');
  final y = int.parse(m[1]!),
      mo = int.parse(m[2]!),
      d = int.parse(m[3]!),
      h = int.parse(m[4]!),
      mi = int.parse(m[5]!),
      s = int.parse(m[6]!);
  if (y < 1 ||
      y > 9999 ||
      mo < 1 ||
      mo > 12 ||
      d < 1 ||
      d > DateTime.utc(y, mo + 1, 0).day ||
      h > 23 ||
      mi > 59 ||
      s > 59) {
    throw const FormatException('日期或时间无效');
  }
  final z = m[8]!;
  if (z != 'Z' &&
      (int.parse(z.substring(1, 3)) > 23 ||
          int.parse(z.substring(4, 6)) > 59)) {
    throw const FormatException('时区无效');
  }
  final result = DateTime.parse(v).toUtc();
  if (result.year < 1 || result.year > 9999) {
    throw const FormatException('期限超出范围');
  }
  return result;
}

void _unavailable(dynamic v) {
  if (v != 'UNAVAILABLE') throw const FormatException('模型状态不符合当前人审边界');
}

void _target(Map<String, dynamic> v) {
  final dest = _map(v['destination']);
  for (final k in ['Provider', 'Model', 'Version', 'WireContract']) {
    final s = _text(dest[k]);
    if (!RegExp(r'^[a-z][a-z0-9_.-]{1,79}$').hasMatch(s) ||
        ['auto', 'default', 'latest'].contains(s)) {
      throw const FormatException('模型目标不明确');
    }
  }
  if (!['UK', 'EU', 'US', 'APAC'].contains(v['region']) ||
      v['retention'] != 'NO_STATE_NO_STORAGE' ||
      v['evidence'] != 'LOCAL_SYNTHETIC' ||
      !RegExp(r'^[A-Z]{3}$').hasMatch(_text(v['currency']))) {
    throw const FormatException('目标或费用依据无效');
  }
}

class EgressOption {
  EgressOption(Map<String, dynamic> v)
    : data = _freeze(v) as Map<String, dynamic> {
    _id(v['rootTraceId']);
    _id(v['taskId']);
    _text(v['taskQuery']);
    _target(v);
    for (final k in [
      'priceVersion',
      'configurationVersion',
      'promptVersion',
      'inputSchemaVersion',
      'outputSchemaVersion',
    ]) {
      _text(v[k]);
    }
    _integer(v['maxOutputTokens'], min: 1, max: 4096);
    egressTime(v['maxDeadlineAt']);
  }
  final Map<String, dynamic> data;
  String get key =>
      '${data['rootTraceId']}:${data['taskId']}:${data['priceVersion']}';
  String get taskID => data['taskId'] as String;
  String get rootID => data['rootTraceId'] as String;
  String get query => data['taskQuery'] as String;
  DateTime get deadline => egressTime(data['maxDeadlineAt']);
  int get maxOutput => data['maxOutputTokens'] as int;
}

class EgressPreview {
  EgressPreview(Map<String, dynamic> v, String owner)
    : data = _freeze(v) as Map<String, dynamic> {
    if (v['schemaVersion'] != 'air.model_egress_budget.v1' ||
        !['DRAFT', 'APPROVED'].contains(v['status']) ||
        v['scope'] != 'SELF_TASK_QUERY' ||
        v['purpose'] != 'MODEL_CONTEXT_EGRESS') {
      throw const FormatException('预览边界无效');
    }
    _id(v['id']);
    if (!RegExp(r'^[0-9a-f]{64}$').hasMatch(_text(v['requestDigest']))) {
      throw const FormatException('具体版本摘要无效');
    }
    _target(v);
    _unavailable(v['modelAccess']);
    final expiry = egressTime(v['expiresAt']);
    if (expiry.isAfter(egressTime(v['priceExpiresAt']))) {
      throw const FormatException('价格期限短于请求期限');
    }
    final request = _map(v['request']),
        agent = _map(request['agent_ref']),
        principal = _map(agent['Principal']);
    if (request['schema_version'] != 'air.model_request.v1' ||
        principal['type'] != 'PERSON' ||
        _id(principal['id']) != owner ||
        request['task_kind'] != 'ACTIVITY_QUERY' ||
        agent['Role'] != 'PERSONAL' ||
        request['tool_allowlist'] is! List ||
        (request['tool_allowlist'] as List).isNotEmpty) {
      throw const FormatException('请求不属于当前本人只读任务');
    }
    _id(agent['AgentID']);
    _id(request['context_snapshot_ref']);
    _id(request['budget_ref']);
    _id(request['data_policy_ref']);
    _id(request['run_id']);
    if (request['data_policy_ref'] != request['budget_ref'] ||
        !['STRUCTURED', 'TEXT'].contains(request['output_mode'])) {
      throw const FormatException('请求目的或输出不匹配');
    }
    for (final k in [
      'prompt_version',
      'input_schema_version',
      'output_schema_version',
    ]) {
      _text(request[k]);
    }
    if (egressTime(request['deadline_at']) != expiry) {
      throw const FormatException('请求期限不一致');
    }
    final messages = request['messages'];
    if (messages is! List || messages.length != 2) {
      throw const FormatException('请求内容无效');
    }
    for (final m in messages) {
      final item = _map(m);
      if (!['system', 'user'].contains(item['role'])) {
        throw const FormatException('未获准的上下文');
      }
      _text(item['content']);
    }
    if (messages.map((m) => m['role']).toSet().length != 2) {
      throw const FormatException('本人查询或系统提示词缺失');
    }
    final upper = _map(v['upper']);
    _integer(upper['inputTokens'], min: 1, max: 1000000);
    _integer(upper['outputTokens'], min: 1, max: 4096);
    _integer(upper['costMicros'], min: 1);
    _integer(v['inputMicrosPerToken'], min: 1, max: 1000000);
    _integer(v['outputMicrosPerToken'], min: 1, max: 1000000);
    if (_integer(
              _map(request['budget'])['max_output_tokens'],
              min: 1,
              max: 4096,
            ) !=
            upper['outputTokens'] ||
        upper['costMicros'] !=
            upper['inputTokens'] * v['inputMicrosPerToken'] +
                upper['outputTokens'] * v['outputMicrosPerToken']) {
      throw const FormatException('请求预算与费用依据不匹配');
    }
  }
  final Map<String, dynamic> data;
  String get id => data['id'] as String;
  String get digest => data['requestDigest'] as String;
  DateTime get expiry => egressTime(data['expiresAt']);
  String get taskID =>
      (_map(data['request']))['context_snapshot_ref'] as String;
  String get rootID => (_map(data['request']))['budget_ref'] as String;
}

class EgressReceipt {
  EgressReceipt(Map<String, dynamic> v, String owner)
    : data = _freeze(v) as Map<String, dynamic> {
    if (v['schemaVersion'] != 'air.model_egress_human.v1' ||
        _id(v['ownerId']) != owner ||
        !['DRAFT', 'APPROVED', 'REVOKED'].contains(v['status'])) {
      throw const FormatException('回执不属于当前本人');
    }
    for (final k in ['previewId', 'rootTraceId', 'taskId']) {
      _id(v[k]);
    }
    _text(v['priceVersion']);
    _integer(v['maxOutputTokens'], min: 1, max: 4096);
    _integer(v['revision'], min: 1);
    egressTime(v['createdAt']);
    egressTime(v['expiresAt']);
    if (v['approvedAt'] != null) {
      egressTime(v['approvedAt']);
    }
    if (v['revokedAt'] != null) {
      egressTime(v['revokedAt']);
    }
    _unavailable(v['modelAccess']);
    for (final k in ['reviewable', 'approvable', 'revocable']) {
      if (v[k] is! bool) throw const FormatException('回执状态无效');
    }
    if (v['status'] == 'REVOKED' &&
        (v['reviewable'] == true ||
            v['approvable'] == true ||
            v['revocable'] == true)) {
      throw const FormatException('撤回记录被恢复');
    }
    if (v['approvable'] == true &&
        (v['reviewable'] != true || v['status'] != 'DRAFT')) {
      throw const FormatException('批准状态无效');
    }
    if (v['review'] != null) {
      review = EgressPreview(_map(v['review']), owner);
      if (v['reviewable'] != true ||
          review!.id != v['previewId'] ||
          review!.taskID != v['taskId'] ||
          review!.rootID != v['rootTraceId'] ||
          review!.expiry != egressTime(v['expiresAt']) ||
          review!.data['status'] != v['status']) {
        throw const FormatException('预览与原回执不一致');
      }
    } else if (v['reviewable'] == true) {
      throw const FormatException('缺少具体预览');
    }
  }
  final Map<String, dynamic> data;
  EgressPreview? review;
  String get id => data['previewId'] as String;
  String get status => data['status'] as String;
  bool get approvable => data['approvable'] == true;
  bool get revocable => data['revocable'] == true;
}

class EgressBudget {
  EgressBudget(Map<String, dynamic> v)
    : data = _freeze(v) as Map<String, dynamic> {
    if (![
          'TENANT_PERSON',
          'SUBJECT_PERSON',
          'ROOT',
          'TASK',
        ].contains(v['scope']) ||
        !RegExp(r'^[A-Z]{3}$').hasMatch(_text(v['currency']))) {
      throw const FormatException('预算范围无效');
    }
    final limit = _map(v['limits']), used = _map(v['allocated']);
    for (final k in ['requests', 'inputTokens', 'outputTokens', 'costMicros']) {
      if (_integer(used[k]) > _integer(limit[k], min: 1)) {
        throw const FormatException('预算账目无效');
      }
    }
  }
  final Map<String, dynamic> data;
}

class ModelEgressAPI {
  ModelEgressAPI({http.Client? client, String? apiBaseUrl})
    : _client = client ?? http.Client(),
      _owns = client == null,
      _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final http.Client _client;
  final bool _owns;
  final String _base;
  Future<dynamic> _call(
    String token,
    String method,
    String suffix, [
    Map<String, dynamic>? body,
  ]) async {
    final url = Uri.parse(
      '${_base.replaceFirst(RegExp(r'/$'), '')}/v1/me/model-egress$suffix',
    );
    final req = http.Request(method, url)..headers['Authorization'] = token;
    if (body != null) {
      req.headers['Content-Type'] = 'application/json';
      req.body = jsonEncode(body);
    }
    final response = await http.Response.fromStream(
      await _client.send(req).timeout(const Duration(seconds: 12)),
    ).timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) {
      throw StateError('模型请求记录无法读取或已失效（${response.statusCode}）');
    }
    return _map(jsonDecode(utf8.decode(response.bodyBytes)));
  }

  Future<List<EgressOption>> options(String token, String owner) async {
    final v = _map((await _call(token, 'GET', '/options'))['data']);
    if (v['schemaVersion'] != 'air.model_egress_human.v1' ||
        _id(v['ownerId']) != owner) {
      throw const FormatException('选择来源不属于本人');
    }
    _unavailable(v['modelAccess']);
    egressTime(v['observedAt']);
    final items = v['options'];
    if (items is! List || items.length > 20) {
      throw const FormatException('选择范围无效');
    }
    final result = [for (final i in items) EgressOption(_map(i))];
    if (result.map((i) => i.key).toSet().length != result.length) {
      throw const FormatException('选择重复');
    }
    return List.unmodifiable(result);
  }

  Future<List<EgressReceipt>> receipts(String token, String owner) async {
    final v = await _call(token, 'GET', '/previews');
    _unavailable(v['modelAccess']);
    final items = v['data'];
    if (items is! List || items.length > 50) {
      throw const FormatException('记录范围无效');
    }
    final result = [for (final i in items) EgressReceipt(_map(i), owner)];
    if (result.map((i) => i.id).toSet().length != result.length) {
      throw const FormatException('重复回执');
    }
    return List.unmodifiable(result);
  }

  Future<EgressReceipt> receipt(String token, String owner, String id) async {
    final r = EgressReceipt(
      _map((await _call(token, 'GET', '/previews/${_id(id)}'))['data']),
      owner,
    );
    if (r.id != id) throw const FormatException('原记录不匹配');
    return r;
  }

  Future<EgressPreview> preview(
    String token,
    String owner,
    EgressOption o,
    int maxOutput,
    DateTime deadline,
  ) async {
    final p = EgressPreview(
      _map(
        (await _call(token, 'POST', '/previews', {
          'rootTraceId': o.rootID,
          'taskId': o.taskID,
          'priceVersion': o.data['priceVersion'],
          'maxOutputTokens': maxOutput,
          'deadlineAt': deadline.toUtc().toIso8601String(),
        }))['data'],
      ),
      owner,
    );
    if (p.taskID != o.taskID ||
        p.rootID != o.rootID ||
        p.data['priceVersion'] != o.data['priceVersion'] ||
        p.expiry != deadline) {
      throw const FormatException('预览不匹配原选择');
    }
    return p;
  }

  Future<void> approve(String token, EgressPreview p) async {
    final d = _map(
      (await _call(token, 'POST', '/approvals', {
        'previewId': p.id,
        'requestDigest': p.digest,
      }))['data'],
    );
    if (d['previewId'] != p.id ||
        d['status'] != 'APPROVED' ||
        d['evidence'] != 'LOCAL_SYNTHETIC') {
      throw const FormatException('批准回执不匹配');
    }
    _unavailable(d['modelAccess']);
  }

  Future<void> revoke(String token, String id) async {
    final d = _map(
      (await _call(token, 'DELETE', '/previews/${_id(id)}'))['data'],
    );
    if (d['previewId'] != id || d['status'] != 'REVOKED') {
      throw const FormatException('撤回回执不匹配');
    }
    _unavailable(d['modelAccess']);
  }

  Future<List<EgressBudget>> budgets(
    String token,
    String root,
    String task,
  ) async {
    final v = await _call(
      token,
      'GET',
      '/roots/${_id(root)}/tasks/${_id(task)}/budget',
    );
    _unavailable(v['modelAccess']);
    final raw = v['data'];
    if (raw is! List || raw.length != 4) throw const FormatException('缺少四层原预算');
    final result = [for (final i in raw) EgressBudget(_map(i))];
    if (result.map((i) => i.data['scope']).toSet().length != 4) {
      throw const FormatException('预算范围重复');
    }
    return List.unmodifiable(result);
  }

  void dispose() {
    if (_owns) _client.close();
  }
}
