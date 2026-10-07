import 'dart:async';
import 'dart:convert';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'agent_request_failure.dart';
import 'agent_workspace_controller.dart';

final _onlineID = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
bool validOnlineID(String value) =>
    _onlineID.hasMatch(value) &&
    value != '00000000-0000-0000-0000-000000000000';
DateTime onlineStamp(dynamic raw) {
  if (raw is! String) throw const FormatException('Invalid online timestamp');
  final m = RegExp(
    r'^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})$',
  ).firstMatch(raw);
  if (m == null) throw const FormatException('Invalid online timestamp');
  final v = [for (var i = 1; i <= 6; i++) int.parse(m[i]!)];
  final d = DateTime.utc(v[0], v[1], v[2], v[3], v[4], v[5]);
  if (v[0] < 1 ||
      d.year != v[0] ||
      d.month != v[1] ||
      d.day != v[2] ||
      d.hour != v[3] ||
      d.minute != v[4] ||
      d.second != v[5]) {
    throw const FormatException('Invalid online calendar');
  }
  final zone = m[8]!;
  if (zone != 'Z' &&
      (int.parse(zone.substring(1, 3)) > 23 ||
          int.parse(zone.substring(4, 6)) > 59)) {
    throw const FormatException('Invalid online offset');
  }
  return DateTime.parse(raw).toUtc();
}

Map<String, dynamic> _object(dynamic raw, Set<String> keys) {
  if (raw is! Map<String, dynamic> ||
      raw.keys.toSet().difference(keys).isNotEmpty ||
      keys.difference(raw.keys.toSet()).isNotEmpty) {
    throw const FormatException('Invalid online DTO');
  }
  return raw;
}

String _text(dynamic value, int limit) {
  if (value is! String ||
      value.trim() != value ||
      value.isEmpty ||
      utf8.encode(value).length > limit ||
      value.contains('\u0000')) {
    throw const FormatException('Invalid online text');
  }
  return value;
}

class NowOnlineContext {
  const NowOnlineContext({required this.id, required this.label});
  final String id, label;
  factory NowOnlineContext.decode(dynamic raw) {
    final x = _object(raw, {'id', 'label', 'type'});
    if (x['type'] != 'ONLINE' ||
        x['id'] is! String ||
        !validOnlineID(x['id'])) {
      throw const FormatException('Invalid online context');
    }
    return NowOnlineContext(id: x['id'], label: _text(x['label'], 640));
  }
}

class NowOnlineIntent {
  const NowOnlineIntent({
    required this.id,
    required this.title,
    required this.contextID,
    required this.updatedAt,
    required this.expiresAt,
  });
  final String id, title, contextID;
  final DateTime updatedAt, expiresAt;
  factory NowOnlineIntent.decode(
    dynamic raw,
    NowOnlineContext context,
    DateTime observed,
  ) {
    final x = _object(raw, {
      'id',
      'title',
      'modality',
      'contextId',
      'sourceUpdatedAt',
      'expiresAt',
    });
    final at = onlineStamp(x['sourceUpdatedAt']),
        until = onlineStamp(x['expiresAt']);
    if (x['modality'] != 'ONLINE' ||
        x['contextId'] != context.id ||
        x['id'] is! String ||
        !validOnlineID(x['id']) ||
        at.isAfter(observed) ||
        !until.isAfter(observed)) {
      throw const FormatException('Online source unavailable');
    }
    return NowOnlineIntent(
      id: x['id'],
      title: _text(x['title'], 640),
      contextID: context.id,
      updatedAt: at,
      expiresAt: until,
    );
  }
}

class NowOnlineResponse {
  const NowOnlineResponse({
    required this.context,
    required this.query,
    required this.answer,
    required this.items,
    required this.observedAt,
    this.task,
  });
  final NowOnlineContext context;
  final String query, answer;
  final List<NowOnlineIntent> items;
  final DateTime observedAt;
  final AgentTask? task;
  factory NowOnlineResponse.decode(
    dynamic raw, {
    String? expectedTaskID,
    String? expectedContextID,
    String? expectedIntentID,
  }) {
    final allowed = {
      'schema',
      'context',
      'query',
      'answer',
      'items',
      'observedAt',
      'modelAccess',
      'promotion',
      'truncated',
      'task',
    };
    if (raw is! Map<String, dynamic> ||
        raw.keys.toSet().difference(allowed).isNotEmpty ||
        allowed.difference({'task'}).difference(raw.keys.toSet()).isNotEmpty ||
        raw['schema'] != 'now-public-online-query-v1' ||
        raw['modelAccess'] != 'UNAVAILABLE' ||
        raw['promotion'] != false ||
        raw['truncated'] is! bool) {
      throw const FormatException('Invalid online response');
    }
    final context = NowOnlineContext.decode(raw['context']),
        at = onlineStamp(raw['observedAt']);
    if (expectedContextID != null && context.id != expectedContextID) {
      throw const FormatException('Context changed');
    }
    if (raw['query'] is! String ||
        utf8.encode(raw['query']).length > 240 ||
        raw['items'] is! List ||
        (raw['items'] as List).length > 20) {
      throw const FormatException('Invalid online response');
    }
    final items = [
      for (final value in raw['items'] as List)
        NowOnlineIntent.decode(value, context, at),
    ];
    if (items.map((e) => e.id).toSet().length != items.length ||
        (expectedIntentID != null &&
            (items.length != 1 || items.single.id != expectedIntentID))) {
      throw const FormatException('Online source mismatch');
    }
    AgentTask? task;
    if (raw.containsKey('task')) {
      final t = _object(raw['task'], {
        'id',
        'principalType',
        'principalId',
        'actingUserId',
        'cityContext',
        'contextType',
        'contextId',
        'query',
        'intent',
        'status',
        'filters',
        'conversation',
        'createdAt',
        'updatedAt',
      });
      onlineStamp(t['createdAt']);
      onlineStamp(t['updatedAt']);
      if (t['filters'] is! Map<String, dynamic> ||
          (t['filters'] as Map).keys.any((x) => x != 'currentQuery') ||
          t['conversation'] is! List ||
          (t['conversation'] as List).length > 48 ||
          t['status'] != 'COMPLETED') {
        throw const FormatException('Invalid task content');
      }
      for (final m in t['conversation'] as List) {
        final x = _object(m, {'role', 'text'});
        if (x['role'] != 'user' && x['role'] != 'assistant') {
          throw const FormatException('Invalid task role');
        }
        _text(x['text'], 1800);
      }
      task = AgentTask.fromJson(t);
    }
    if (task != null &&
        (!validOnlineID(task.id) ||
            task.contextType != 'ONLINE' ||
            task.contextID != context.id ||
            (task.cityID?.isNotEmpty ?? false) ||
            task.intent != 'FIND_PUBLIC_ONLINE_INTENT' ||
            task.principalType.toUpperCase() != 'PERSON' ||
            !validOnlineID(task.principalID) ||
            task.actingUserID != task.principalID ||
            task.updatedAt == null ||
            task.filters['currentQuery'] != raw['query'])) {
      throw const FormatException('Invalid online task');
    }
    if (expectedTaskID != null && task?.id != expectedTaskID) {
      throw const FormatException('Task changed');
    }
    return NowOnlineResponse(
      context: context,
      query: raw['query'],
      answer: _text(raw['answer'], 1800),
      items: List.unmodifiable(items),
      observedAt: at,
      task: task,
    );
  }
  AgentResult result() => AgentResult(
    entities: const [],
    activities: const [],
    places: const [],
    note: answer,
    message: answer,
    task: task,
    taskID: task?.id,
    onlineContext: context,
    onlineIntents: items,
  );
}

class NowContextQueryApi {
  NowContextQueryApi({
    required this.authorizationHeader,
    this.organizationWorkspaceID,
    this.ownerID,
    this.current,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _owns = client == null,
       _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final String? Function() authorizationHeader;
  final String? Function()? organizationWorkspaceID;
  final String? Function()? ownerID;
  final bool Function()? current;
  final http.Client _client;
  final bool _owns;
  final String _base;
  bool _disposed = false;
  Future<dynamic> _request(String path, {Map<String, dynamic>? body}) async {
    final token = authorizationHeader(), owner = ownerID?.call();
    if (_disposed ||
        token == null ||
        organizationWorkspaceID?.call() != null ||
        current?.call() == false) {
      throw const AgentRequestFailure('请使用本人账号选择线上情境。');
    }
    final uri = Uri.parse('${_base.replaceFirst(RegExp(r'/$'), '')}$path');
    http.Response response;
    try {
      response =
          await (body == null
                  ? _client.get(uri, headers: {'Authorization': token})
                  : _client.post(
                      uri,
                      headers: {
                        'Authorization': token,
                        'Content-Type': 'application/json',
                      },
                      body: jsonEncode(body),
                    ))
              .timeout(const Duration(seconds: 12));
    } on Object {
      throw AgentRequestFailure(
        body == null ? '暂时无法读取线上结果，请重试。' : '提交结果尚未确认，请查看最近对话；不会自动重复提交。',
        code: body == null ? 'online_read_failed' : 'online_result_unknown',
      );
    }
    if (_disposed ||
        token != authorizationHeader() ||
        owner != ownerID?.call() ||
        organizationWorkspaceID?.call() != null ||
        current?.call() == false) {
      throw const AgentRequestFailure('工作身份已变化，请重新打开线上查询。');
    }
    if (response.statusCode != 200 &&
        body != null &&
        (response.statusCode >= 500 || response.statusCode == 409)) {
      throw const AgentRequestFailure(
        '提交结果尚未确认，请从最近对话重新读取；不会自动重复提交。',
        code: 'online_result_unknown',
      );
    }
    if (response.statusCode != 200) {
      throw AgentRequestFailure(switch (response.statusCode) {
        401 => '登录已失效，请重新登录。',
        403 => '当前身份无法读取此情境。',
        404 => '情境或公开意图已不可用。',
        409 => '任务或来源已变化，请重新读取。',
        _ => '线上查询暂时不可用。',
      }, statusCode: response.statusCode);
    }
    final envelope = _object(jsonDecode(utf8.decode(response.bodyBytes)), {
      'data',
    });
    return envelope['data'];
  }

  Future<List<NowOnlineContext>> contexts() async {
    final raw = await _request('/v1/me/now/online-contexts');
    if (raw is! List || raw.length > 20) {
      throw const FormatException('Invalid context list');
    }
    final values = [for (final x in raw) NowOnlineContext.decode(x)];
    if (values.map((e) => e.id).toSet().length != values.length) {
      throw const FormatException('Duplicate context');
    }
    return List.unmodifiable(values);
  }

  Future<NowOnlineResponse> query(
    String contextID,
    String query, {
    AgentTask? task,
  }) async {
    if (!validOnlineID(contextID) ||
        query.trim().isEmpty ||
        utf8.encode(query).length > 240 ||
        (task != null &&
            (task.contextType != 'ONLINE' ||
                task.contextID != contextID ||
                task.updatedAt == null))) {
      throw const AgentRequestFailure('请检查线上情境与当前任务。');
    }
    try {
      final result = NowOnlineResponse.decode(
        await _request(
          '/v1/me/now/online/tasks',
          body: {
            'contextId': contextID,
            'query': query.trim(),
            'taskId': task?.id ?? '',
            'expectedTaskUpdatedAt':
                task?.updatedAt?.toUtc().toIso8601String() ?? '',
          },
        ),
        expectedContextID: contextID,
        expectedTaskID: task?.id,
      );
      if (result.task == null ||
          (ownerID != null && result.task!.principalID != ownerID!())) {
        throw const FormatException('Owner changed');
      }
      return result;
    } catch (error) {
      if (error is AgentRequestFailure) rethrow;
      throw const AgentRequestFailure(
        '提交结果尚未确认，请从最近对话核实；不会自动重复提交。',
        code: 'online_result_unknown',
      );
    }
  }

  Future<NowOnlineResponse> restore(String taskID) async {
    if (!validOnlineID(taskID)) throw const FormatException('Invalid task ID');
    final result = NowOnlineResponse.decode(
      await _request('/v1/me/now/online/tasks/$taskID'),
      expectedTaskID: taskID,
    );
    if (ownerID != null && result.task?.principalID != ownerID!()) {
      throw const AgentRequestFailure('当前任务不属于本人，请重新读取。');
    }
    return result;
  }

  Future<NowOnlineResponse> intent(String intentID) async {
    if (!validOnlineID(intentID)) {
      throw const FormatException('Invalid intent ID');
    }
    return NowOnlineResponse.decode(
      await _request('/v1/me/now/online/intents/$intentID'),
      expectedIntentID: intentID,
    );
  }

  void dispose() {
    if (_disposed) return;
    _disposed = true;
    if (_owns) _client.close();
  }
}
