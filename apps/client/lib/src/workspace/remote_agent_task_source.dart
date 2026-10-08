import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import '../city/public_city_controller.dart';
import 'agent_workspace_controller.dart';
import 'agent_answer_sources.dart';
import 'agent_result_projection.dart';
import 'agent_reply_membership.dart';
import 'public_query_field_evidence.dart';
import 'agent_request_failure.dart';
import 'agent_debug_diagnostics.dart';
import 'map_entities.dart';
import 'now_context_query_api.dart';
import 'sponsored_opportunities.dart';
import '../config/birdtie_environment.dart';

String _activityStatusLabel(String status) => switch (status) {
  'upcoming' => '即将开始',
  'ongoing' => '进行中',
  'completed' => '已结束',
  'cancelled' => '已取消',
  _ => status,
};

bool _publicMapZone(String? zone) => switch (zone) {
  'city_centre' || 'north' || 'south' || 'east' || 'west' => true,
  _ => false,
};

class RemoteAgentTaskSource extends AgentTaskSource {
  RemoteAgentTaskSource({
    required this.cityID,
    required this.authorizationHeader,
    this.organizationWorkspaceID,
    this.onlineContextID,
    this.onlineApi,
    this.onlineApiFactory,
    this.publicEvidenceOwnerID,
    this.publicEvidenceEpoch,
    this.publicEvidenceNow,
    AgentDebugDiagnostics? diagnostics,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _apiBaseUrl = apiBaseUrl ?? apiBase,
       _diagnostics = diagnostics ?? AgentDebugDiagnostics.instance;

  static const apiBase = BirdtieEnvironment.apiBaseUrl;
  // One bounded search-and-answer round; no transport retry is performed.
  static const currentQueryTimeout = Duration(seconds: 35);
  final String? Function() cityID;
  final String? Function() authorizationHeader;
  final String? Function()? organizationWorkspaceID;
  final String? Function()? onlineContextID;
  final NowContextQueryApi? onlineApi;
  final NowContextQueryApi Function()? onlineApiFactory;
  // These reuse the existing caller lifecycle solely for optional descriptions.
  // They do not replace domain request identity or its transport/ACL checks.
  final String? Function()? publicEvidenceOwnerID;
  final Object? Function()? publicEvidenceEpoch;
  final DateTime Function()? publicEvidenceNow;
  bool _publicEvidenceDisposed = false;
  final bool _ownsClient;
  final http.Client _client;
  final String _apiBaseUrl;
  final AgentDebugDiagnostics _diagnostics;

  String _newRequestID() {
    final random = Random.secure();
    return List<int>.generate(
      16,
      (_) => random.nextInt(256),
    ).map((byte) => byte.toRadixString(16).padLeft(2, '0')).join();
  }

  Uri _endpoint(String path) =>
      Uri.parse('${_apiBaseUrl.replaceFirst(RegExp(r'/$'), '')}$path');

  Map<String, String> _headers({bool json = false}) {
    final headers = <String, String>{};
    if (json) headers['Content-Type'] = 'application/json';
    final bearer = authorizationHeader();
    if (bearer != null) headers['Authorization'] = bearer;
    final organizationID = organizationWorkspaceID?.call();
    if (organizationID != null) {
      headers['X-Birdtie-Organization-Workspace'] = organizationID;
    }
    return headers;
  }

  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    return _submit(query, null);
  }

  @override
  Future<AgentResult> followUp(
    AgentTask task,
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) => task.id.startsWith('local-') && task.contextType == 'CITY'
      ? _localPublicTurn(task, query)
      : task.contextType == 'ONLINE'
      ? _online(task.contextID!, query, task: task)
      : _submit(query, task.id);

  @override
  Future<AgentResult> searchArea(
    AgentTask? task,
    String query,
    MapBounds bounds,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) =>
      task != null && task.id.startsWith('local-') && task.contextType == 'CITY'
      ? _localPublicTurn(task, query, bounds: bounds)
      : _submit(query, task?.id, bounds: bounds);

  Future<AgentResult> _localPublicTurn(
    AgentTask task,
    String query, {
    MapBounds? bounds,
    bool restoring = false,
  }) {
    if (_publicEvidenceDisposed) {
      throw const AgentRequestFailure(
        '本轮查询来源已变化，请重新发起查询。',
        code: 'PUBLIC_CONTEXT_EXPIRED',
      );
    }
    final context = task.lastSuccessfulPublicQuery;
    if (context == null) {
      if (restoring && task.localOriginalQuerySuperseded) {
        throw const AgentRequestFailure(
          '上次成功查询条件已变化，请重新输入当前需求；未重发原查询。',
          code: 'PUBLIC_CONTEXT_EXPIRED',
        );
      }
      // Legacy servers provide no normalized successful conditions. Preserve
      // their original two-turn compatibility without claiming richer context.
      // An explicit place request replaces unknown legacy query text.
      // It must not acquire a search term from a previous question's period.
      if (!restoring && _explicitPublicTarget(query) == 'FIND_PLACE') {
        return _submit(query, null, bounds: bounds);
      }
      return _submit(
        restoring
            ? task.query
            : task.localOriginalQuerySuperseded
            ? query
            : '${task.query}. $query',
        null,
        bounds: bounds,
      );
    }
    if (!context.current ||
        !identical(context.sourceIdentity, this) ||
        context.cityID != cityID() ||
        authorizationHeader() != null ||
        organizationWorkspaceID?.call() != null ||
        onlineContextID?.call() != null) {
      context.retire();
      if (restoring) {
        throw const AgentRequestFailure(
          '上次查询来源或范围已变化，请重新输入当前需求；未重发原查询。',
          code: 'PUBLIC_CONTEXT_EXPIRED',
        );
      }
      return _submit(query, null, bounds: bounds);
    }
    final slots = Map<String, String>.of(context.slots);
    final text = query.toLowerCase();
    bool mentions(List<String> words) => words.any(text.contains);
    final categorySpecified = mentions([
      '羽毛球',
      'badminton',
      '篮球',
      'basketball',
      '足球',
      'football',
      '运动',
      'sports',
      '文化',
      'culture',
    ]);
    if (categorySpecified) {
      slots.remove('category');
    }
    if (mentions([
      '今晚',
      'tonight',
      '明天',
      'tomorrow',
      '今天',
      'today',
      '周末',
      'weekend',
      'saturday',
      'sunday',
      '不限时间',
      '任何时间',
      'anytime',
    ])) {
      slots.remove('timePreference');
    }
    final cityScope = mentions(['全城', '全市', '整个城市', 'whole city', 'citywide']);
    final viewportScope =
        bounds != null ||
        mentions([
          '附近',
          '周边',
          'nearby',
          'near me',
          'around me',
          '搜索此区域',
          'search this area',
        ]);
    if (cityScope || viewportScope) slots.remove('locationPreference');
    if (mentions(['近一点', '更近', '靠近市中心', '按市中心距离排序', 'closer'])) {
      slots.remove('distancePreference');
    }
    final target = _explicitPublicTarget(query);
    if (target != null && target != slots['targetIntent']) {
      slots.remove('targetIntent');
      slots.remove('category');
      slots.remove('timePreference');
      slots.remove('distancePreference');
    }
    if (target != null) slots.remove('targetIntent');
    const categoryLabels = {
      'badminton': '羽毛球',
      'basketball': '篮球',
      'football': '足球',
      'sports': '运动',
      'culture': '文化',
    };
    const timeLabels = {
      'today': '今天',
      'tonight': '今晚',
      'tomorrow': '明天',
      'weekend': '周末',
    };
    final conditions = [
      if (target == null)
        switch (slots['targetIntent']) {
          'FIND_ACTIVITY' || 'AREA_DISCOVERY' => '找活动',
          'FIND_PLACE' => '找地点 ${jsonEncode(context.placeSearchTerm ?? '')}',
          _ => '',
        },
      categoryLabels[slots['category']] ?? '',
      timeLabels[slots['timePreference']] ?? '',
      if (slots['locationPreference'] == 'viewport') '附近',
      if (slots['locationPreference'] == 'city') '全城',
      if (slots['distancePreference'] == 'closer') '按市中心距离排序',
    ].where((word) => word.isNotEmpty).join(' ');
    final separator =
        target == 'FIND_PLACE' ||
            (target == null && context.slots['targetIntent'] == 'FIND_PLACE')
        ? ' '
        : '. ';
    return _submit(
      restoring
          ? conditions
          : conditions.isEmpty
          ? query
          : query.trimLeft().startsWith('找地点 "')
          ? '$query $conditions'
          : '$conditions$separator$query',
      null,
      bounds: bounds ?? (cityScope ? null : context.bounds),
    );
  }

  String? _explicitPublicTarget(String query) {
    final text = query.toLowerCase();
    if (text.trimLeft().startsWith('找地点 "')) return 'FIND_PLACE';
    bool mentions(List<String> words) => words.any(text.contains);
    return mentions([
          '社团',
          '组织',
          '协会',
          'cssa',
          'society',
          'club',
          'organization',
        ])
        ? 'FIND_ORGANIZATION'
        : mentions([
            '地点',
            '场馆',
            '体育馆',
            '图书馆',
            '咖啡馆',
            '餐厅',
            'place',
            'venue',
            'library',
            'restaurant',
            'cafe',
          ])
        ? 'FIND_PLACE'
        : mentions([
            '羽毛球',
            'badminton',
            '篮球',
            'basketball',
            '足球',
            'football',
            '运动',
            'sports',
            '文化',
            'culture',
            '活动',
            '比赛',
            'event',
            'activity',
            'activities',
            'tournament',
            'play',
          ])
        ? 'FIND_ACTIVITY'
        : null;
  }

  Future<AgentResult> _submit(
    String query,
    String? taskID, {
    MapBounds? bounds,
  }) async {
    final online = onlineContextID?.call();
    if (online != null) {
      if (bounds != null || taskID != null) {
        throw const AgentRequestFailure('请重新读取当前线上任务。');
      }
      return _online(online, query);
    }
    if (_apiBaseUrl.isEmpty) {
      throw const AgentRequestFailure('尚未连接 Birdtie 服务，暂时无法搜索内容。');
    }
    final selected = cityID();
    if (selected == null) {
      throw const AgentRequestFailure('请先选择城市，再搜索公开内容。', code: 'NEEDS_CITY');
    }
    if (utf8.encode(query).length > 240) {
      throw const AgentRequestFailure(
        '描述加上当前任务条件过长，请缩短内容或开始新任务；尚未发出查询。',
        code: 'QUERY_TOO_LONG',
      );
    }
    final anonymousCity =
        authorizationHeader() == null && organizationWorkspaceID?.call() == null
        ? selected
        : null;
    final anonymousEpoch = publicEvidenceEpoch?.call();
    bool anonymousCurrent() =>
        anonymousCity != null &&
        anonymousCity == cityID() &&
        authorizationHeader() == null &&
        organizationWorkspaceID?.call() == null &&
        onlineContextID?.call() == null &&
        !_publicEvidenceDisposed &&
        publicEvidenceEpoch?.call() == anonymousEpoch;
    final requestID = _newRequestID();
    final readPublicEvidence = _capturePublicEvidence();
    final readAnswerSources = _captureAnswerSources(
      expectedRequestID: requestID,
    );
    final messageSourcesCurrent = _captureMessageSourcesCurrent();
    _diagnostics.started(
      requestId: requestID,
      apiBase: _apiBaseUrl,
      authenticated: authorizationHeader() != null,
    );
    late final http.Response response;
    try {
      response = await _client
          .post(
            _endpoint(
              '/v1/cities/${Uri.encodeComponent(selected)}/agent/tasks',
            ),
            headers: {..._headers(json: true), 'X-Request-ID': requestID},
            body: jsonEncode({
              'query': query,
              'taskId': ?taskID,
              if (bounds != null) 'mapBounds': bounds.toJson(),
            }),
          )
          .timeout(currentQueryTimeout);
    } on TimeoutException {
      _diagnostics.finished(
        requestId: requestID,
        status: '超时',
        resultCount: null,
        error: '请求超时',
      );
      debugPrint('Birdtie Agent request timed out.');
      throw const AgentRequestFailure('请求超时，请检查网络后重试。');
    } on http.ClientException {
      _diagnostics.finished(
        requestId: requestID,
        status: '网络错误',
        resultCount: null,
        error: '无法连接 API',
      );
      debugPrint('Birdtie Agent could not connect to the API.');
      throw const AgentRequestFailure('无法连接 Birdtie 服务，请检查网络与服务状态。');
    }
    if (response.statusCode != 200) {
      final code = _errorCode(response.body);
      _diagnostics.finished(
        requestId: response.headers['x-request-id'] ?? requestID,
        status: 'HTTP ${response.statusCode}',
        resultCount: null,
        error: code ?? 'unknown',
      );
      debugPrint(
        'Birdtie Agent API returned HTTP ${response.statusCode} (${code ?? 'unknown'}).',
      );
      throw AgentRequestFailure(
        _httpFailureMessage(response.statusCode),
        statusCode: response.statusCode,
        code: code,
      );
    }
    try {
      final decoded =
          jsonDecode(utf8.decode(response.bodyBytes)) as Map<String, dynamic>;
      final data = decoded['data'] as Map<String, dynamic>;
      final result = _parseResult(
        data,
        readPublicEvidence: readPublicEvidence,
        anonymousCity: anonymousCurrent() ? anonymousCity : null,
        anonymousCurrent: anonymousCurrent,
        readAnswerSources: readAnswerSources,
        messageSourcesCurrent: messageSourcesCurrent,
      );
      _diagnostics.finished(
        requestId: response.headers['x-request-id'] ?? requestID,
        status: 'HTTP 200',
        resultCount:
            result.activities.length +
            result.places.length +
            result.people.length +
            result.groups.length +
            result.organizations.length,
      );
      return result;
    } on FormatException {
      _diagnostics.finished(
        requestId: requestID,
        status: 'HTTP 200',
        resultCount: null,
        error: '响应 JSON 无效',
      );
      debugPrint('Birdtie Agent API returned invalid JSON.');
      throw const AgentRequestFailure('服务返回的数据暂时无法读取，请稍后重试。');
    } on TypeError catch (error, stack) {
      _diagnostics.finished(
        requestId: requestID,
        status: 'HTTP 200',
        resultCount: null,
        error: '响应结构无效',
      );
      if (kDebugMode) {
        debugPrint('Birdtie Agent API response shape: $error');
        debugPrintStack(stackTrace: stack);
      }
      throw const AgentRequestFailure('服务返回的数据暂时无法读取，请稍后重试。');
    }
  }

  String? _errorCode(String body) {
    try {
      final decoded = jsonDecode(body) as Map<String, dynamic>;
      final error = decoded['error'];
      return error is Map<String, dynamic> ? error['code'] as String? : null;
    } on Object {
      return null;
    }
  }

  String _httpFailureMessage(int statusCode) => switch (statusCode) {
    401 => '登录状态已失效，请重新登录后重试。',
    403 => '当前账号没有执行此操作的权限。',
    404 => '没有找到这个城市或对话，请返回后重试。',
    429 => '请求过于频繁，请稍后重试。',
    >= 500 => 'Birdtie 服务暂时不可用，请稍后重试。',
    _ => '请求未完成（HTTP $statusCode），请检查内容后重试。',
  };

  Future<http.Response> _readTaskResponse(
    Future<http.Response> request, {
    Duration timeout = const Duration(seconds: 12),
    bool Function()? sourceCurrent,
  }) async {
    try {
      final response = await request.timeout(timeout);
      if (sourceCurrent != null) _requireTaskReadCurrent(sourceCurrent);
      return response;
    } catch (error) {
      if (sourceCurrent != null) _requireTaskReadCurrent(sourceCurrent);
      if (error is TimeoutException) {
        throw const AgentRequestFailure('读取任务超时，请检查网络后重新核实。');
      }
      if (error is http.ClientException) {
        throw const AgentRequestFailure('无法读取 Birdtie 任务，请检查网络与服务状态。');
      }
      rethrow;
    }
  }

  void _requireTaskReadCurrent(bool Function() current) {
    if (!current()) {
      throw const AgentRequestFailure(
        '任务读取范围或账号已变化，请在当前范围重新读取。',
        code: 'PUBLIC_CONTEXT_EXPIRED',
      );
    }
  }

  AgentRequestFailure _taskReadFailure(http.Response response) =>
      AgentRequestFailure(
        _httpFailureMessage(response.statusCode),
        statusCode: response.statusCode,
        code: _errorCode(response.body),
      );

  @override
  Future<List<AgentTask>> loadRecent() async {
    final sourceCurrent = _captureSourceLifecycle();
    _requireTaskReadCurrent(sourceCurrent);
    if (authorizationHeader() == null) return [];
    if (_apiBaseUrl.isEmpty) {
      throw const AgentRequestFailure('尚未连接 Birdtie 服务，暂时无法读取对话记录。');
    }
    final messageSourcesCurrent = _captureMessageSourcesCurrent();
    final response = await _readTaskResponse(
      _client.get(_endpoint('/v1/me/agent-tasks'), headers: _headers()),
      timeout: const Duration(seconds: 10),
      sourceCurrent: sourceCurrent,
    );
    if (response.statusCode != 200) {
      throw _taskReadFailure(response);
    }
    final records =
        (jsonDecode(utf8.decode(response.bodyBytes))
                as Map<String, dynamic>)['data']
            as List<dynamic>;
    return [
      for (final raw in records)
        AgentTask.fromJson(
          raw as Map<String, dynamic>,
          sourceCurrent: messageSourcesCurrent,
        ),
    ];
  }

  @override
  Future<AgentResult> restore(
    AgentTask task,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    if (task.contextType == 'ONLINE') {
      final sourceCurrent = _captureSourceLifecycle();
      _requireTaskReadCurrent(sourceCurrent);
      final response = await _onlineApi().restore(task.id);
      _requireTaskReadCurrent(sourceCurrent);
      return response.result();
    }
    if (_apiBaseUrl.isEmpty) {
      throw const AgentRequestFailure('尚未连接 Birdtie 服务，暂时无法恢复对话。');
    }
    if (task.id.startsWith('local-')) {
      if (task.contextType == 'CITY') {
        return _localPublicTurn(task, '', restoring: true);
      }
      return resolve(task.query, activities, places);
    }
    if (authorizationHeader() == null) {
      throw const AgentRequestFailure(
        '请先登录本人账号，再恢复任务。',
        statusCode: 401,
        code: 'unauthorized',
      );
    }
    final sourceCurrent = _captureSourceLifecycle();
    _requireTaskReadCurrent(sourceCurrent);
    final readPublicEvidence = _capturePublicEvidence();
    final readAnswerSources = _captureAnswerSources();
    final messageSourcesCurrent = _captureMessageSourcesCurrent();
    final response = await _readTaskResponse(
      _client.get(
        _endpoint('/v1/me/agent-tasks/${Uri.encodeComponent(task.id)}'),
        headers: _headers(),
      ),
      sourceCurrent: sourceCurrent,
    );
    if (response.statusCode != 200) throw _taskReadFailure(response);
    return _parseResult(
      (jsonDecode(utf8.decode(response.bodyBytes))
              as Map<String, dynamic>)['data']
          as Map<String, dynamic>,
      readPublicEvidence: readPublicEvidence,
      readAnswerSources: readAnswerSources,
      messageSourcesCurrent: messageSourcesCurrent,
    );
  }

  @override
  bool canRereadReplyProjections(AgentTask task) =>
      !_publicEvidenceDisposed &&
      _apiBaseUrl.isNotEmpty &&
      RegExp(
        r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
      ).hasMatch(task.id) &&
      task.contextType == 'CITY' &&
      task.cityID == cityID() &&
      task.status == 'COMPLETED' &&
      task.principalType.toUpperCase() == 'PERSON' &&
      task.principalID.isNotEmpty &&
      task.actingUserID == task.principalID &&
      publicEvidenceOwnerID?.call() == task.principalID &&
      authorizationHeader() != null &&
      organizationWorkspaceID?.call() == null;

  @override
  Future<AgentResult?> rereadReplyProjections(AgentTask task) async {
    if (!canRereadReplyProjections(task)) return null;
    final current = _captureSourceLifecycle();
    _requireTaskReadCurrent(current);
    final fresh = await readByID(task.id, ownerID: task.principalID);
    _requireTaskReadCurrent(current);
    return fresh;
  }

  /// Read an existing task by its native ID. Opening a notification never
  /// submits a new intent or fabricates a placeholder task.
  Future<AgentResult> readByID(String id, {required String ownerID}) async {
    final uuid = RegExp(
      r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
    );
    if (!uuid.hasMatch(id) ||
        !uuid.hasMatch(ownerID) ||
        id == '00000000-0000-0000-0000-000000000000' ||
        ownerID == '00000000-0000-0000-0000-000000000000') {
      throw const FormatException('Invalid task destination');
    }
    final token = authorizationHeader();
    if (token == null || organizationWorkspaceID?.call() != null) {
      throw const AgentRequestFailure('请先登录本人账号。');
    }
    if (_apiBaseUrl.isEmpty) {
      throw const AgentRequestFailure('尚未连接 Birdtie 服务，暂时无法读取对话。');
    }
    final sourceCurrent = _captureSourceLifecycle();
    _requireTaskReadCurrent(sourceCurrent);
    final readPublicEvidence = _capturePublicEvidence(ownerOverride: ownerID);
    final readAnswerSources = _captureAnswerSources();
    final messageSourcesCurrent = _captureMessageSourcesCurrent();
    final response = await _readTaskResponse(
      _client.get(
        _endpoint('/v1/me/agent-tasks/$id'),
        headers: {'Authorization': token},
      ),
      sourceCurrent: sourceCurrent,
    );
    if (token != authorizationHeader() ||
        organizationWorkspaceID?.call() != null) {
      throw const AgentRequestFailure('工作身份已变化，请重新打开通知。');
    }
    if (response.statusCode != 200) {
      throw _taskReadFailure(response);
    }
    final result = _parseResult(
      (jsonDecode(utf8.decode(response.bodyBytes))
              as Map<String, dynamic>)['data']
          as Map<String, dynamic>,
      readPublicEvidence: readPublicEvidence,
      readAnswerSources: readAnswerSources,
      messageSourcesCurrent: messageSourcesCurrent,
    );
    final task = result.task;
    if (result.taskID != id ||
        task == null ||
        task.id != id ||
        task.principalType.toUpperCase() != 'PERSON' ||
        task.principalID != ownerID ||
        task.actingUserID != ownerID) {
      throw const FormatException('Task owner or identity mismatch');
    }
    return result;
  }

  NowContextQueryApi _onlineApi() =>
      onlineApiFactory?.call() ??
      onlineApi ??
      NowContextQueryApi(
        authorizationHeader: authorizationHeader,
        organizationWorkspaceID: organizationWorkspaceID,
        client: _client,
        apiBaseUrl: _apiBaseUrl,
      );
  Future<AgentResult> _online(
    String id,
    String query, {
    AgentTask? task,
  }) async {
    final frame = onlineContextID?.call(), token = authorizationHeader();
    final response = await _onlineApi().query(id, query, task: task);
    if (frame != onlineContextID?.call() || token != authorizationHeader()) {
      throw const AgentRequestFailure('查询情境已变化。');
    }
    return response.result();
  }

  PublicQueryFieldEvidence? Function(Map<String, dynamic>)
  _capturePublicEvidence({String? ownerOverride}) {
    final owner = ownerOverride ?? publicEvidenceOwnerID?.call();
    final epoch = publicEvidenceEpoch?.call();
    final token = authorizationHeader(), org = organizationWorkspaceID?.call();
    final waited = Stopwatch()..start();
    bool current() =>
        !_publicEvidenceDisposed &&
        owner != null &&
        token != null &&
        org == null &&
        publicEvidenceEpoch != null &&
        publicEvidenceOwnerID?.call() == owner &&
        publicEvidenceEpoch?.call() == epoch &&
        authorizationHeader() == token &&
        organizationWorkspaceID?.call() == org;
    return (data) {
      final rawSet = data['resultSet'];
      if (rawSet is! Map<String, dynamic> ||
          !rawSet.containsKey('publicFieldEvidence') ||
          owner == null ||
          token == null ||
          org != null ||
          publicEvidenceEpoch == null ||
          publicEvidenceOwnerID == null) {
        return null;
      }
      try {
        return PublicQueryFieldEvidence.read(
          data,
          expectedOwner: owner,
          now: publicEvidenceNow ?? () => DateTime.now().toUtc(),
          current: current,
          waited: waited.elapsed,
        );
      } on Object {
        final items = decodeAgentResultItems(rawSet);
        final kind = items.isEmpty ? '' : items.first.entity.type;
        return PublicQueryFieldEvidence.unavailable(
          taskID: data['taskId'] as String? ?? '',
          resultSetID: rawSet['id'] as String? ?? '',
          queryKind: kind,
          selectedRefs: {for (final i in items) i.entity.mapID},
          current: current,
        );
      }
    };
  }

  bool Function() _captureSourceLifecycle() {
    final token = authorizationHeader();
    final org = organizationWorkspaceID?.call();
    final city = cityID();
    final online = onlineContextID?.call();
    final owner = publicEvidenceOwnerID?.call();
    final epoch = publicEvidenceEpoch?.call();
    var retired = false;
    bool current() {
      if (_publicEvidenceDisposed ||
          token != authorizationHeader() ||
          org != organizationWorkspaceID?.call() ||
          city != cityID() ||
          online != onlineContextID?.call() ||
          owner != publicEvidenceOwnerID?.call() ||
          epoch != publicEvidenceEpoch?.call()) {
        retired = true;
      }
      return !retired;
    }

    return current;
  }

  bool Function() _captureMessageSourcesCurrent() {
    final token = authorizationHeader();
    final org = organizationWorkspaceID?.call();
    final current = _captureSourceLifecycle();
    return () => token != null && org == null && current();
  }

  AgentAnswerSources? Function(Map<String, dynamic>) _captureAnswerSources({
    String? expectedRequestID,
  }) {
    final current = _captureSourceLifecycle();
    return (data) => AgentAnswerSources.read(
      data,
      current: current,
      now: publicEvidenceNow ?? () => DateTime.now().toUtc(),
      expectedRequestID: expectedRequestID,
    );
  }

  AgentResult _parseResult(
    Map<String, dynamic> data, {
    PublicQueryFieldEvidence? Function(Map<String, dynamic>)?
    readPublicEvidence,
    String? anonymousCity,
    bool Function()? anonymousCurrent,
    AgentAnswerSources? Function(Map<String, dynamic>)? readAnswerSources,
    bool Function()? messageSourcesCurrent,
    bool readMessageResults = true,
    AgentReplyProjectionLifetime? replyProjection,
    AgentMessageSources? persistedMessageSources,
  }) {
    if (data['schema'] == 'now-public-online-query-v1') {
      return NowOnlineResponse.decode(data).result();
    }
    if (!data.containsKey('activities') || !data.containsKey('places')) {
      throw const FormatException('Missing domain result collection');
    }
    final activities = [
      for (final raw in data['activities'] as List<dynamic>? ?? const [])
        PublicActivity.fromJson(raw as Map<String, dynamic>),
    ];
    final places = [
      for (final raw in data['places'] as List<dynamic>? ?? const [])
        PublicPlace.fromJson(raw as Map<String, dynamic>),
    ];
    final people = [
      for (final raw in data['people'] as List<dynamic>)
        AgentPerson(
          accountID: (raw as Map<String, dynamic>)['accountId'] as String,
          displayName: raw['displayName'] as String,
          topic: raw['topic'] as String,
          areaLabel: raw['areaLabel'] as String,
          publicMapZone: raw['publicMapZone'] as String? ?? '',
          mapLatitude: _publicMapZone(raw['publicMapZone'] as String?)
              ? (raw['mapLatitude'] as num?)?.toDouble()
              : null,
          mapLongitude: _publicMapZone(raw['publicMapZone'] as String?)
              ? (raw['mapLongitude'] as num?)?.toDouble()
              : null,
        ),
    ];
    final groups = [
      for (final raw in data['groups'] as List<dynamic>)
        AgentGroup(
          id: (raw as Map<String, dynamic>)['id'] as String,
          name: raw['name'] as String,
          summary: raw['summary'] as String? ?? '',
        ),
    ];
    final organizations = [
      for (final raw in data['organizations'] as List<dynamic>? ?? const [])
        AgentOrganization(
          id: (raw as Map<String, dynamic>)['id'] as String,
          name: raw['name'] as String,
          description: raw['description'] as String? ?? '',
          verificationStatus:
              raw['verificationStatus'] as String? ?? 'unverified',
        ),
    ];
    final entities = <MapEntity>[
      for (final place in places)
        if (place.location.hasPublicPoint)
          MapEntity(
            id: 'place:${place.id}',
            kind: MapEntityKind.place,
            title: place.name,
            subtitle: '已发布地点',
            latitude: place.location.latitude!,
            longitude: place.location.longitude!,
          ),
      for (final person in people)
        if (person.mapLatitude != null && person.mapLongitude != null)
          MapEntity(
            id: 'person:${person.accountID}',
            kind: MapEntityKind.person,
            title: person.displayName,
            subtitle: '大致位置 · ${person.areaLabel}',
            latitude: person.mapLatitude!,
            longitude: person.mapLongitude!,
          ),
      for (final activity in activities)
        if (activity.location case final location?)
          if (location.hasPublicPoint)
            MapEntity(
              id: 'activity:${activity.id}',
              kind: MapEntityKind.activity,
              title: activity.title,
              subtitle: _activityStatusLabel(activity.status),
              latitude: location.latitude!,
              longitude: location.longitude!,
            ),
      for (final raw in data['groups'] as List<dynamic>)
        if ((raw as Map<String, dynamic>)['location']
            case final Map<String, dynamic> location)
          if (location['precision'] == 'point' &&
              location['coordinateSystem'] == 'wgs84')
            MapEntity(
              id: 'group:${raw['id']}',
              kind: MapEntityKind.group,
              title: raw['name'] as String,
              subtitle: '已发布社群',
              latitude: (location['latitude'] as num).toDouble(),
              longitude: (location['longitude'] as num).toDouble(),
            ),
    ];
    final count =
        activities.length +
        people.length +
        groups.length +
        organizations.length +
        places.length;
    final task = data['task'] is Map<String, dynamic>
        ? AgentTask.fromJson(
            data['task'] as Map<String, dynamic>,
            sourceCurrent: messageSourcesCurrent,
          )
        : null;
    final assistantMessages =
        task?.messages
            .where((message) => message.role == 'assistant')
            .toList() ??
        const <AgentMessage>[];
    final assistantMessage = assistantMessages.isEmpty
        ? null
        : assistantMessages.last.text;
    final rawSet = data['resultSet'] as Map<String, dynamic>?;
    final resultSet = rawSet == null
        ? null
        : AgentResultSet(
            id: rawSet['id'] as String,
            status: rawSet['status'] as String,
            generatedAt: DateTime.parse(rawSet['generatedAt'] as String),
            schema: rawSet['schema'] as String?,
            items: rawSet['schema'] == null
                ? null
                : decodeAgentResultItems(rawSet),
            publicFieldEvidence: readPublicEvidence?.call(data),
            answerSources: readAnswerSources?.call(data),
            publicQueryContext:
                anonymousCity != null &&
                    (data['cityId'] == null || data['cityId'] == anonymousCity)
                ? LocalPublicQueryContext.decode(
                    rawSet['filters'],
                    anonymousCity,
                    this,
                    sourceCurrent: anonymousCurrent,
                  )
                : null,
            entities: [
              for (final raw in rawSet['entities'] as List<dynamic>)
                AgentEntityRef(
                  type: (raw as Map<String, dynamic>)['type'] as String,
                  id: raw['id'] as String,
                ),
            ],
          );
    final rawMapEffects = data['mapEffects'] as Map<String, dynamic>?;
    if (resultSet?.items != null) {
      final pins = [
        for (final i in resultSet!.items!)
          if (i.anchor != null) i.entity.mapID,
      ];
      if (rawMapEffects == null ||
          rawMapEffects['camera'] != 'preserve' ||
          rawMapEffects['pinEntityIds'] is! List ||
          !listEquals(pins, rawMapEffects['pinEntityIds'] as List)) {
        throw const FormatException('地图效果与当前实体来源不一致');
      }
    }
    final disclosure = decodeSponsoredDisclosure(
      data,
      eligibleTargets: {
        for (final a in activities) 'ACTIVITY:${a.id}': a.title,
        for (final p in places) 'PLACE:${p.id}': p.name,
      },
    );
    final history = readMessageResults
        ? _parseMessageResults(data, task, messageSourcesCurrent)
        : null;
    var currentReplyLifetime = replyProjection;
    if (history != null &&
        task != null &&
        task.messages.isNotEmpty &&
        task.messages.last.role == 'assistant' &&
        task.messages.last.resultMembership != null) {
      final latest = history[task.messages.length - 1];
      if (latest == null) {
        // An authoritative empty history never grafts current search results
        // onto a persisted reply whose membership was not freshly read.
        currentReplyLifetime = AgentReplyProjectionLifetime(
          validUntil: (publicEvidenceNow ?? () => DateTime.now().toUtc())(),
          current: () => false,
        );
      } else {
        if (resultSet?.id != latest.resultSet?.id ||
            !_equalWire(
              rawSet?['items'],
              _historySet(data, task.messages.length - 1)?['items'],
            ) ||
            !_equalWire(
              rawMapEffects,
              _historyEntry(data, task.messages.length - 1)?['mapEffects'],
            )) {
          throw const FormatException('最新回答与历史结果集合不一致');
        }
        currentReplyLifetime = latest.replyProjection;
      }
    }
    return AgentResult(
      entities: entities,
      sponsoredOpportunities: disclosure.items,
      sponsoredUnavailable: disclosure.unavailable,
      activities: activities,
      people: people,
      groups: groups,
      organizations: organizations,
      places: places,
      taskID: data['taskId'] as String?,
      task: task,
      messageResults: history,
      replyProjection: currentReplyLifetime,
      messageSources:
          persistedMessageSources ??
          (task != null &&
                  task.messages.isNotEmpty &&
                  task.messages.last.role == 'assistant'
              ? task.messages.last.sourceReferences
              : null),
      requestID: data['requestId'] as String?,
      conversationID: data['conversationId'] as String?,
      resultSet: resultSet,
      actions: [
        for (final raw in data['actions'] as List<dynamic>? ?? const [])
          AgentAction(
            type: (raw as Map<String, dynamic>)['type'] as String,
            label: raw['label'] as String,
            targetType: raw['targetType'] as String?,
            targetID: raw['targetId'] as String?,
          ),
      ],
      mapEffects: rawMapEffects == null
          ? null
          : AgentMapEffects(
              camera: rawMapEffects['camera'] as String,
              pinEntityIDs: [
                for (final id in rawMapEffects['pinEntityIds'] as List<dynamic>)
                  id as String,
              ],
            ),
      message: data['message'] as String? ?? assistantMessage,
      note:
          data['note'] as String? ??
          (count == 0
              ? '暂未找到符合条件的内容。Birdtie 根据已发布信息进行基础文本匹配。'
              : 'Birdtie 已发布信息 · 基础文本匹配'),
      followUps: [
        for (final item in data['followUps'] as List<dynamic>? ?? const [])
          item as String,
      ],
    );
  }

  Map<String, dynamic>? _historyEntry(Map<String, dynamic> data, int index) {
    for (final raw in data['messageResults'] as List<dynamic>? ?? const []) {
      if (raw is Map<String, dynamic> && raw['messageIndex'] == index) {
        return raw;
      }
    }
    return null;
  }

  Map<String, dynamic>? _historySet(Map<String, dynamic> data, int index) =>
      _historyEntry(data, index)?['resultSet'] as Map<String, dynamic>?;

  bool _equalWire(Object? left, Object? right) {
    if (left is Map && right is Map) {
      return left.length == right.length &&
          left.keys.every(
            (key) =>
                right.containsKey(key) && _equalWire(left[key], right[key]),
          );
    }
    if (left is List && right is List) {
      return left.length == right.length &&
          List.generate(
            left.length,
            (i) => i,
          ).every((i) => _equalWire(left[i], right[i]));
    }
    return left == right;
  }

  Map<int, AgentResult>? _parseMessageResults(
    Map<String, dynamic> data,
    AgentTask? task,
    bool Function()? current,
  ) {
    if (!data.containsKey('messageResults')) return null;
    void require(bool condition) {
      if (!condition) throw const FormatException('历史回答的实体结果无法读取');
    }

    require(
      data['messageResults'] is List &&
          (data['messageResults'] as List).length <= 30 &&
          task != null &&
          task.contextType == 'CITY' &&
          task.principalType.toUpperCase() == 'PERSON' &&
          data['taskId'] == task.id &&
          data['conversationId'] == task.id &&
          current?.call() == true,
    );
    final original = task!;
    final owner = publicEvidenceOwnerID?.call();
    require(
      original.principalID.isNotEmpty &&
          original.actingUserID == original.principalID &&
          (owner == null || owner == original.principalID),
    );
    final now = publicEvidenceNow ?? () => DateTime.now().toUtc();
    final results = <int, AgentResult>{};
    for (final value in data['messageResults'] as List) {
      require(value is Map<String, dynamic>);
      final raw = value as Map<String, dynamic>;
      require(
        raw.keys.every(
              (key) => const {
                'messageIndex',
                'turnDigest',
                'validUntil',
                'resultSet',
                'activities',
                'places',
                'organizations',
                'mapEffects',
              }.contains(key),
            ) &&
            raw['messageIndex'] is int &&
            raw['turnDigest'] is String &&
            raw['resultSet'] is Map<String, dynamic>,
      );
      final index = raw['messageIndex'] as int;
      require(
        index >= 0 &&
            index < original.messages.length &&
            !results.containsKey(index),
      );
      final message = original.messages[index];
      final membership = message.resultMembership;
      require(message.role == 'assistant' && membership != null);
      final member = membership!;
      final set = raw['resultSet'] as Map<String, dynamic>;
      final generated = DateTime.tryParse(set['generatedAt'] as String? ?? '');
      final until = DateTime.tryParse(raw['validUntil'] as String? ?? '');
      require(
        member.taskID == original.id &&
            member.cityID == original.cityID &&
            raw['turnDigest'] == member.turnDigest &&
            member.turnDigest ==
                agentReplyTurnDigest(
                  original.messages
                      .take(index + 1)
                      .map((m) => (role: m.role, text: m.text)),
                ) &&
            set['id'] == member.resultSetID &&
            set['taskId'] == original.id &&
            set['cityId'] == original.cityID &&
            set['schema'] == typedAgentResultSchema &&
            const {'ready', 'empty'}.contains(set['status']) &&
            generated != null &&
            until != null &&
            !generated.isAfter(now().toUtc().add(const Duration(seconds: 2))) &&
            until.isAfter(generated) &&
            until.isAfter(now().toUtc()) &&
            (set['sources'] is List && (set['sources'] as List).isEmpty) &&
            !set.containsKey('answerBinding') &&
            !set.containsKey('publicFieldEvidence') &&
            !raw.containsKey('messageResults'),
      );
      require(
        original.messages
            .take(index + 1)
            .every((m) => const {'user', 'assistant'}.contains(m.role)),
      );
      final lifetime = AgentReplyProjectionLifetime(
        validUntil: until!,
        current: current!,
        now: now,
      );
      final reply = _parseResult(
        {
          ...raw,
          'taskId': original.id,
          'conversationId': original.id,
          'message': message.text,
          'people': <dynamic>[],
          'groups': <dynamic>[],
          'organizations': raw['organizations'] ?? <dynamic>[],
        },
        readMessageResults: false,
        replyProjection: lifetime,
        persistedMessageSources: message.sourceReferences,
      );
      final items = reply.projectionItems;
      require(
        items != null &&
            items.length <= 30 &&
            (items.isEmpty
                ? set['status'] == 'empty'
                : set['status'] == 'ready'),
      );
      var previous = -1;
      for (final item in items!) {
        final position = member.refs.indexOf(item.entity);
        require(
          item.scope == 'AUTHORIZED_VIEW' &&
              position > previous &&
              item.entity.type == member.kind,
        );
        previous = position;
      }
      require(
        reply.activities.every(
              (a) => items.any(
                (item) =>
                    item.entity == AgentResultRef(type: 'activity', id: a.id),
              ),
            ) &&
            reply.places.every(
              (p) => items.any(
                (item) =>
                    item.entity == AgentResultRef(type: 'place', id: p.id),
              ),
            ),
      );
      results[index] = reply;
    }
    return Map.unmodifiable(results);
  }

  @override
  void dispose() {
    _publicEvidenceDisposed = true;
    if (_ownsClient) _client.close();
  }
}
