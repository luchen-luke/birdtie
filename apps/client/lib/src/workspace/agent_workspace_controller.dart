import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';

import '../city/public_city_controller.dart';
import 'agent_request_failure.dart';
import 'agent_answer_sources.dart';
import 'agent_result_projection.dart';
import 'agent_reply_membership.dart';
import 'public_query_field_evidence.dart';
import 'map_entities.dart';
import 'now_context_query_api.dart';
import 'sponsored_opportunities.dart';

enum AgentViewState { idle, typing, searching, results, conversation }

enum AgentSheetExtent { hidden, peek, medium, expanded }

enum AgentQueryState {
  idle,
  needsScope,
  loading,
  empty,
  error,
  unsupported,
  success,
}

enum AgentContentMode { results, conversation }

class AgentTask {
  const AgentTask({
    required this.id,
    required this.query,
    required this.status,
    this.intent,
    this.cityID,
    this.contextType = 'CITY',
    this.contextID,
    this.principalType = 'PERSON',
    this.principalID = '',
    this.actingUserID = '',
    this.filters = const {},
    this.lastSuccessfulPublicQuery,
    this.localOriginalQuerySuperseded = false,
    this.messages = const [],
    this.createdAt,
    this.updatedAt,
  });
  final String id;
  final String query;
  final String status;
  final String? intent;
  final String? cityID;
  final String contextType;
  final String? contextID;
  final String principalType;
  final String principalID;
  final String actingUserID;
  final Map<String, String> filters;
  // In-memory public read conditions, never a persisted Task identity,
  // permission, approval or a copy of user/assistant conversation text.
  final LocalPublicQueryContext? lastSuccessfulPublicQuery;
  // Once normalized activity context was used, an unsupported later target
  // must not revive the original activity title as a new search condition.
  final bool localOriginalQuerySuperseded;
  final List<AgentMessage> messages;
  final DateTime? createdAt;
  final DateTime? updatedAt;

  factory AgentTask.fromJson(
    Map<String, dynamic> json, {
    bool Function()? sourceCurrent,
  }) => AgentTask(
    id: json['id'] as String,
    query: json['query'] as String,
    status: json['status'] as String,
    intent: json['intent'] as String?,
    cityID: json['cityContext'] as String? ?? json['cityId'] as String?,
    contextType: json['contextType'] as String? ?? 'CITY',
    contextID: json['contextId'] as String?,
    principalType: json['principalType'] as String? ?? 'PERSON',
    principalID: json['principalId'] as String? ?? '',
    actingUserID: json['actingUserId'] as String? ?? '',
    filters: (json['filters'] as Map<String, dynamic>? ?? const {}).map(
      (key, value) => MapEntry(key, value as String),
    ),
    messages: [
      for (final item in json['conversation'] as List<dynamic>? ?? const [])
        AgentMessage.fromJson(
          item as Map<String, dynamic>,
          sourceCurrent: sourceCurrent,
        ),
    ],
    createdAt: DateTime.tryParse(json['createdAt'] as String? ?? ''),
    updatedAt: DateTime.tryParse(json['updatedAt'] as String? ?? ''),
  );
}

class AgentMessage {
  const AgentMessage({
    required this.role,
    required this.text,
    this.sourceReferences,
    this.resultMembership,
  });
  final String role;
  final String text;
  final AgentMessageSources? sourceReferences;
  final AgentReplyMembership? resultMembership;
  List<AgentAnswerSource> get sources => sourceReferences?.sources ?? const [];
  AgentMessage withText(String text) => AgentMessage(
    role: role,
    text: text,
    sourceReferences: sourceReferences,
    resultMembership: resultMembership,
  );
  factory AgentMessage.fromJson(
    Map<String, dynamic> json, {
    bool Function()? sourceCurrent,
  }) {
    final role = json['role'] as String;
    final membership = AgentReplyMembership.read(json['resultMembership']);
    if (role != 'assistant' && membership != null) {
      throw const FormatException('用户消息不能携带回答结果');
    }
    return AgentMessage(
      role: role,
      text: json['text'] as String,
      sourceReferences: AgentMessageSources.read(json, current: sourceCurrent),
      resultMembership: membership,
    );
  }
}

class AgentResult {
  const AgentResult({
    required List<MapEntity> entities,
    required this.activities,
    required this.places,
    required this.note,
    this.message,
    this.people = const [],
    this.groups = const [],
    this.organizations = const [],
    this.taskID,
    this.task,
    this.followUps = const [],
    this.requestID,
    this.conversationID,
    this.resultSet,
    this.actions = const [],
    this.mapEffects,
    this.sponsoredOpportunities = const [],
    this.sponsoredUnavailable = false,
    this.onlineContext,
    this.onlineIntents = const [],
    this.messageSources,
    this.messageResults,
    this.replyProjection,
  }) : _legacyEntities = entities;
  final List<MapEntity> _legacyEntities;
  List<AgentResultItem>? get projectionItems =>
      replyProjectionCurrent ? resultSet?.items : const <AgentResultItem>[];
  bool get replyProjectionCurrent => replyProjection?.current ?? true;
  bool get isUnsupported => resultSet?.status == 'unsupported';
  List<MapEntity> get entities => !replyProjectionCurrent
      ? const <MapEntity>[]
      : projectionItems == null
      ? _legacyEntities
      : [for (final item in projectionItems!) ?item.mapEntity];
  final List<PublicActivity> activities;
  final List<PublicPlace> places;
  final String note;
  final String? message;
  String get responseMessage => message ?? note;
  final List<AgentPerson> people;
  final List<AgentGroup> groups;
  final List<AgentOrganization> organizations;
  final String? taskID;
  final AgentTask? task;
  final List<String> followUps;
  final String? requestID;
  final String? conversationID;
  final AgentResultSet? resultSet;
  final List<AgentAction> actions;
  final AgentMapEffects? mapEffects;
  final List<SponsoredOpportunity> sponsoredOpportunities;
  final bool sponsoredUnavailable;
  final NowOnlineContext? onlineContext;
  final List<NowOnlineIntent> onlineIntents;
  final AgentMessageSources? messageSources;
  // Null preserves legacy replies. An authenticated empty map is authoritative
  // and must not revive cached or latest-turn entities for an older message.
  final Map<int, AgentResult>? messageResults;
  final AgentReplyProjectionLifetime? replyProjection;
  bool get hasOnlyMessageSources =>
      messageSources != null &&
      resultSet == null &&
      _legacyEntities.isEmpty &&
      activities.isEmpty &&
      places.isEmpty &&
      people.isEmpty &&
      groups.isEmpty &&
      organizations.isEmpty &&
      onlineIntents.isEmpty;
}

typedef AgentEntityRef = AgentResultRef;

class AgentResultSet {
  const AgentResultSet({
    required this.id,
    required this.status,
    required this.entities,
    required this.generatedAt,
    this.schema,
    this.items,
    this.publicFieldEvidence,
    this.publicQueryContext,
    this.answerSources,
  });
  final String id;
  final String status;
  final List<AgentEntityRef> entities;
  final DateTime generatedAt;
  final String? schema;
  final List<AgentResultItem>? items;
  final PublicQueryFieldEvidence? publicFieldEvidence;
  final LocalPublicQueryContext? publicQueryContext;
  final AgentAnswerSources? answerSources;
  List<AgentAnswerSource> get sources => answerSources?.sources ?? const [];
}

/// A reply keeps the entity set returned for that turn. It never grants an
/// action or changes the current query's authority.
class AgentReply {
  const AgentReply({
    required this.messageIndex,
    required this.result,
    this.userMessages = const [],
  });
  final int messageIndex;
  final AgentResult result;
  final List<String> userMessages;
}

/// Only normalized public operation/filter slots become local context.
/// Place names are bounded search values, never authority or action text.
/// Missing legacy filters remain unknown; arbitrary query/evidence text is not
/// carried into later requests and these fields confer no access permission.
class LocalPublicQueryContext {
  LocalPublicQueryContext._(
    this.cityID,
    this.sourceIdentity,
    Map<String, String> slots,
    this.bounds,
    this.placeSearchTerm,
    this._sourceCurrent,
  ) : slots = Map.unmodifiable(slots);

  final String cityID;
  final Object sourceIdentity;
  final Map<String, String> slots;
  final MapBounds? bounds;
  final String? placeSearchTerm;
  final bool Function()? _sourceCurrent;
  bool _retired = false;

  bool get current {
    if (_retired || _sourceCurrent?.call() == false) {
      _retired = true;
      return false;
    }
    return true;
  }

  void retire() => _retired = true;

  static LocalPublicQueryContext? decode(
    Object? raw,
    String cityID,
    Object sourceIdentity, {
    bool Function()? sourceCurrent,
  }) {
    if (raw is! Map ||
        (raw['targetIntent'] != 'FIND_ACTIVITY' &&
            raw['targetIntent'] != 'AREA_DISCOVERY' &&
            raw['targetIntent'] != 'FIND_PLACE')) {
      return null;
    }
    final place = raw['targetIntent'] == 'FIND_PLACE';
    final term = place ? raw['searchTerm'] : null;
    if (place &&
        (term is! String ||
            term.trim() != term ||
            utf8.encode(term).length > 240 ||
            RegExp(r'[\x00-\x1f\x7f]').hasMatch(term))) {
      return null;
    }
    const allowed = {
      'category': {
        '',
        'badminton',
        'basketball',
        'football',
        'sports',
        'culture',
      },
      'timePreference': {
        '',
        'anytime',
        'today',
        'tonight',
        'tomorrow',
        'weekend',
      },
      'distancePreference': {'', 'closer'},
      'locationPreference': {'', 'city', 'viewport'},
      'targetIntent': {'FIND_ACTIVITY', 'AREA_DISCOVERY', 'FIND_PLACE'},
    };
    final slots = <String, String>{};
    for (final entry in allowed.entries) {
      if (place &&
          entry.key != 'targetIntent' &&
          entry.key != 'locationPreference') {
        continue;
      }
      final value = raw[entry.key];
      if (value is String && entry.value.contains(value)) {
        slots[entry.key] = value;
      }
    }
    final coordinates = <String, String>{};
    for (final key in ['mapWest', 'mapSouth', 'mapEast', 'mapNorth']) {
      final value = raw[key];
      if (value is String && double.tryParse(value)?.isFinite == true) {
        coordinates[key] = value;
      }
    }
    final bounds = MapBounds.fromFilters(coordinates);
    return LocalPublicQueryContext._(
      cityID,
      sourceIdentity,
      slots,
      bounds.isValid ? bounds : null,
      term as String?,
      sourceCurrent,
    );
  }
}

class AgentAction {
  const AgentAction({
    required this.type,
    required this.label,
    this.targetType,
    this.targetID,
  });
  final String type;
  final String label;
  final String? targetType;
  final String? targetID;
}

class AgentMapEffects {
  const AgentMapEffects({required this.camera, required this.pinEntityIDs});
  final String camera;
  final List<String> pinEntityIDs;
}

class AgentPerson {
  const AgentPerson({
    required this.accountID,
    required this.displayName,
    required this.topic,
    required this.areaLabel,
    this.publicMapZone = '',
    this.mapLatitude,
    this.mapLongitude,
  });
  final String accountID;
  final String displayName;
  final String topic;
  final String areaLabel;
  final String publicMapZone;
  final double? mapLatitude;
  final double? mapLongitude;
}

class AgentGroup {
  const AgentGroup({
    required this.id,
    required this.name,
    required this.summary,
  });
  final String id;
  final String name;
  final String summary;
}

class AgentOrganization {
  const AgentOrganization({
    required this.id,
    required this.name,
    required this.description,
    required this.verificationStatus,
  });
  final String id;
  final String name;
  final String description;
  final String verificationStatus;
}

/// A replaceable task source. The remote implementation uses the Birdtie API.
abstract class AgentTaskSource {
  const AgentTaskSource();
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  );
  Future<List<AgentTask>> loadRecent() async => [];
  Future<AgentResult> followUp(
    AgentTask task,
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) => resolve(query, activities, places);
  Future<AgentResult> searchArea(
    AgentTask? task,
    String query,
    MapBounds bounds,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) => task == null
      ? resolve(query, activities, places)
      : followUp(task, query, activities, places);
  Future<AgentResult> restore(
    AgentTask task,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) => resolve(task.query, activities, places);
  // A separate read-only port: the default must never fall back to resolve,
  // followUp or restore, since those may submit a new native turn.
  bool canRereadReplyProjections(AgentTask task) => false;
  Future<AgentResult?> rereadReplyProjections(AgentTask task) async => null;
  void dispose() {}
}

class LocalAgentTaskSource extends AgentTaskSource {
  const LocalAgentTaskSource();

  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    await Future<void>.delayed(const Duration(milliseconds: 650));
    if (!query.toLowerCase().contains('badminton')) {
      return const AgentResult(
        entities: [],
        activities: [],
        places: [],
        note: '本地预览目前只能搜索已加载的城市活动。',
      );
    }
    final matching = activities.where((activity) {
      final text = '${activity.title} ${activity.summary}'.toLowerCase();
      return text.contains('badminton') &&
          (activity.status == 'upcoming' || activity.status == 'ongoing');
    }).toList();
    final entities = [
      for (final activity in matching)
        if (activity.location?.hasPublicPoint == true)
          MapEntity(
            id: 'activity:${activity.id}',
            kind: MapEntityKind.activity,
            title: activity.title,
            subtitle: activity.status,
            latitude: activity.location!.latitude!,
            longitude: activity.location!.longitude!,
          ),
    ];
    return AgentResult(
      entities: entities,
      activities: matching,
      places: const <PublicPlace>[],
      note: matching.isEmpty
          ? '本地预览中没有符合条件的已发布活动。'
          : '在本地预览中找到 ${matching.length} 个公开活动。',
    );
  }
}

class AgentWorkspaceController extends ChangeNotifier {
  AgentWorkspaceController({
    AgentTaskSource? source,
    bool Function()? replyRefreshAllowed,
  }) : _source = source ?? const LocalAgentTaskSource(),
       _replyRefreshAllowed = replyRefreshAllowed ?? (() => true);
  final bool Function() _replyRefreshAllowed;
  final AgentTaskSource _source;
  AgentViewState state = AgentViewState.idle;
  AgentSheetExtent sheetExtent = AgentSheetExtent.peek;
  AgentTask? task;
  AgentResult? result;
  AgentResult? _mapReply;
  int? _mapReplyMessageIndex;
  // An expired historical map stays empty until its own authorized re-read;
  // never substitute the latest turn's entities for the selected older reply.
  AgentResult? get presentedResult => _mapReply ?? result;
  bool preserveReplyCamera = false;
  Timer? _replyExpiryTimer;
  Timer? _replyRefreshTimer;
  int _replyRefreshSerial = 0;
  bool _replyRefreshRetired = false;
  bool _replyRefreshFailed = false;
  bool _replyRefreshVisible = true;
  bool _replyRefreshInFlight = false;
  bool _replyResumePending = false;
  bool _disposed = false;
  Future<bool>? _replyRefreshFuture;
  final Map<String, List<AgentReply>> _repliesByTask = {};
  List<AgentReply> get replies =>
      (_repliesByTask[task?.id] ?? const <AgentReply>[])
          .where((reply) {
            if (reply.messageIndex >= conversation.length ||
                conversation[reply.messageIndex].role != 'assistant') {
              return false;
            }
            final users = conversation
                .take(reply.messageIndex)
                .where((m) => m.role == 'user')
                .map((m) => m.text)
                .toList();
            return listEquals(users, reply.userMessages);
          })
          .toList(growable: false);
  String queryContextType = 'CITY';
  AgentResult? mapBackgroundResult;
  String? mapBackgroundCityID;
  String? selectedEntityId;
  String? requestError;
  AgentRequestFailure? requestFailure;
  AgentContentMode contentMode = AgentContentMode.results;
  bool get permitsUnchangedRetry =>
      requestFailure?.permitsUnchangedRetry != false &&
      // Preserve old restored view state that has no typed failure. Current
      // HTTP/session/permission and unknown-outcome decisions use metadata.
      !(requestFailure == null && requestError?.contains('提交结果尚未确认') == true);
  AgentQueryState get queryState {
    if (state == AgentViewState.searching) return AgentQueryState.loading;
    if (requestFailure?.needsCity == true) return AgentQueryState.needsScope;
    if (requestError != null) return AgentQueryState.error;
    final answer = result;
    if (answer == null) return AgentQueryState.idle;
    if (answer.isUnsupported) return AgentQueryState.unsupported;
    if (task?.intent == 'PERSONAL_RELATIONSHIP_CONTEXT' ||
        answer.actions.isNotEmpty) {
      return AgentQueryState.success;
    }
    final empty = answer.projectionItems != null
        ? answer.projectionItems!.isEmpty
        : answer.activities.isEmpty &&
              answer.places.isEmpty &&
              answer.organizations.isEmpty &&
              answer.people.isEmpty &&
              answer.groups.isEmpty &&
              answer.onlineIntents.isEmpty &&
              answer.entities.isEmpty &&
              answer.sponsoredOpportunities.isEmpty;
    return empty ? AgentQueryState.empty : AgentQueryState.success;
  }

  String? get pendingScopeQuery =>
      queryState == AgentQueryState.needsScope ? _lastQuery : null;

  /// An unmet public-city precondition is a draft turn, not a remote result.
  void requireCity(String raw) {
    final query = raw.trim();
    if (query.isEmpty) return;
    final serial = ++_serial;
    task = AgentTask(id: 'local-$serial', query: query, status: 'NEEDS_SCOPE');
    result = null;
    conversation
      ..clear()
      ..add(AgentMessage(role: 'user', text: query));
    _lastQuery = query;
    _lastBounds = null;
    _lastActiveTask = null;
    _lastRestoreTask = null;
    requestFailure = const AgentRequestFailure(
      '选择城市后继续原查询；尚未搜索公开内容。',
      code: 'NEEDS_CITY',
    );
    requestError = requestFailure!.userMessage;
    contentMode = AgentContentMode.results;
    state = AgentViewState.results;
    sheetExtent = AgentSheetExtent.peek;
    notifyListeners();
  }

  Future<void> resumeCity(
    String cityID,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    final query = pendingScopeQuery;
    if (query == null) return;
    final serial = ++_serial;
    _lastCityID = cityID;
    _lastActivities = activities;
    _lastPlaces = places;
    requestError = null;
    requestFailure = null;
    state = AgentViewState.searching;
    notifyListeners();
    await _resolveTurn(
      serial: serial,
      query: query,
      activeTask: null,
      activities: activities,
      places: places,
      cityID: cityID,
    );
  }

  bool inputFocused = false;
  final List<AgentTask> recent = [];
  bool recentLoading = false;
  String? recentError;
  AgentRequestFailure? recentFailure;
  bool get permitsRecentRetry => recentFailure?.permitsUnchangedRetry != false;
  final List<AgentMessage> conversation = [];
  final Map<String, List<AgentMessage>> _savedConversation = {};
  String? _lastQuery;
  MapBounds? _lastBounds;
  AgentTask? _lastActiveTask;
  AgentTask? _lastRestoreTask;
  List<PublicActivity> _lastActivities = const [];
  List<PublicPlace> _lastPlaces = const [];
  String? _lastCityID;
  int _serial = 0;
  int _historySerial = 0;

  /// Existing request generation, also used to bind local editable material.
  /// Layout, focus and result arrival do not manufacture a new task epoch.
  int get taskEpoch => _serial;

  Future<void> loadRecent() => _loadRecent();

  Future<void> _loadRecent({int? failedTurnSerial}) async {
    if (failedTurnSerial != null && failedTurnSerial != _serial) return;
    final serial = ++_historySerial;
    recentLoading = true;
    recentError = null;
    recentFailure = null;
    notifyListeners();
    try {
      final stored = await _source.loadRecent();
      if (serial != _historySerial) return;
      if (failedTurnSerial != null && failedTurnSerial != _serial) {
        recentLoading = false;
        notifyListeners();
        return;
      }
      final local = recent
          .where((item) => item.id.startsWith('local-'))
          .toList();
      recent
        ..clear()
        ..addAll([...local, ...stored]);
      recentLoading = false;
      notifyListeners();
    } catch (error) {
      if (serial != _historySerial) return;
      if (failedTurnSerial != null && failedTurnSerial != _serial) {
        recentLoading = false;
        notifyListeners();
        return;
      }
      // Keep known current-session tasks; a failed read is not empty history.
      recentLoading = false;
      recentError = _failureMessage(error);
      recentFailure = error is AgentRequestFailure ? error : null;
      notifyListeners();
    }
  }

  Future<void> retryRecent() async {
    if (recentLoading || !permitsRecentRetry) return;
    await loadRecent();
  }

  void retireAccountContext() {
    _repliesByTask.clear();
    _savedConversation.clear();
    _mapReply = null;
    _mapReplyMessageIndex = null;
    queryContextType = 'CITY';
    mapBackgroundResult = null;
    mapBackgroundCityID = null;
    ++_serial;
    ++_historySerial;
    task = null;
    result = null;
    selectedEntityId = null;
    recent.clear();
    recentLoading = false;
    recentError = null;
    recentFailure = null;
    conversation.clear();
    requestError = null;
    requestFailure = null;
    contentMode = AgentContentMode.results;
    _lastQuery = null;
    _lastBounds = null;
    _lastActiveTask = null;
    _lastRestoreTask = null;
    sheetExtent = AgentSheetExtent.peek;
    state = AgentViewState.idle;
  }

  void clearAccountContext() {
    _repliesByTask.clear();
    queryContextType = 'CITY';
    mapBackgroundResult = null;
    mapBackgroundCityID = null;
    ++_historySerial;
    newTask();
    recent.clear();
    recentLoading = false;
    recentError = null;
    recentFailure = null;
    _savedConversation.clear();
    notifyListeners();
  }

  void beginTyping() {
    inputFocused = true;
    if (task != null) {
      sheetExtent = AgentSheetExtent.expanded;
      contentMode = AgentContentMode.conversation;
    }
    if (state == AgentViewState.idle) state = AgentViewState.typing;
    notifyListeners();
  }

  /// An explicit view choice retires late queries without destroying the
  /// current task, result, conversation, selected Pin or camera.
  void retirePendingQueryForViewChange() {
    retireReplyRefresh();
    ++_serial;
    if (state == AgentViewState.searching ||
        queryState == AgentQueryState.needsScope) {
      requestFailure = null;
      state = result == null ? AgentViewState.idle : AgentViewState.results;
      requestError = '查看范围已变化，请在新范围再次提交查询。';
    }
    notifyListeners();
  }

  void stopTyping() {
    inputFocused = false;
    if (state == AgentViewState.typing) state = AgentViewState.idle;
    notifyListeners();
  }

  Future<void> searchThisArea(
    List<PublicActivity> activities,
    List<PublicPlace> places, {
    required MapBounds bounds,
    String? cityID,
    String query = '搜索此区域',
  }) async {
    if (!bounds.isValid) return;
    final activeTask = task;
    final effectiveQuery = query;
    final serial = ++_serial;
    if (activeTask == null) {
      task = AgentTask(
        id: 'local-$serial',
        query: effectiveQuery,
        status: 'ACTIVE',
        cityID: cityID,
      );
      conversation.clear();
    }
    conversation.add(AgentMessage(role: 'user', text: query));
    requestError = null;
    state = AgentViewState.searching;
    _lastQuery = effectiveQuery;
    _lastBounds = bounds;
    _lastActiveTask = activeTask;
    _lastActivities = activities;
    _lastPlaces = places;
    _lastCityID = cityID;
    notifyListeners();
    await _resolveTurn(
      serial: serial,
      query: effectiveQuery,
      activeTask: activeTask,
      activities: activities,
      places: places,
      cityID: cityID,
      bounds: bounds,
    );
  }

  Future<void> submit(
    String raw,
    List<PublicActivity> activities,
    List<PublicPlace> places, {
    String? cityID,
  }) async {
    final query = raw.trim();
    if (query.isEmpty) return;
    final serial = ++_serial;
    final continuing = task != null && result != null;
    final previousTask = task;
    if (!continuing) {
      task = AgentTask(
        id: 'local-$serial',
        query: query,
        status: 'ACTIVE',
        cityID: cityID,
      );
      conversation.clear();
    }
    requestError = null;
    requestFailure = null;
    contentMode = AgentContentMode.conversation;
    sheetExtent = AgentSheetExtent.expanded;
    state = AgentViewState.searching;
    conversation.add(AgentMessage(role: 'user', text: query));
    _lastQuery = query;
    _lastBounds = null;
    _lastActiveTask = continuing ? previousTask : null;
    _lastActivities = activities;
    _lastPlaces = places;
    _lastCityID = cityID;
    notifyListeners();
    await _resolveTurn(
      serial: serial,
      query: query,
      activeTask: continuing ? previousTask : null,
      activities: activities,
      places: places,
      cityID: cityID,
    );
  }

  Future<void> retry() async {
    if (queryState == AgentQueryState.needsScope) return;
    if (!permitsUnchangedRetry) return;
    final query = _lastQuery;
    if (state == AgentViewState.searching) return;
    if (query == null && _lastRestoreTask != null) {
      final previous = _lastRestoreTask!;
      final serial = ++_serial;
      requestError = null;
      requestFailure = null;
      state = AgentViewState.searching;
      notifyListeners();
      try {
        final restored = await _source.restore(
          previous,
          _lastActivities,
          _lastPlaces,
        );
        if (serial != _serial || task?.id != previous.id) return;
        preserveReplyCamera = false;
        result = restored;
        task = _withSuccessfulPublicQuery(restored.task ?? previous, restored);
        _refreshRestoredLocalRecent(previous);
        conversation
          ..clear()
          ..addAll(
            task!.messages.isNotEmpty
                ? task!.messages
                : [
                    AgentMessage(role: 'user', text: previous.query),
                    AgentMessage(
                      role: 'assistant',
                      text: restored.responseMessage,
                    ),
                  ],
          );
        _presentCurrentTypedAnswer(restored);
        _rememberReply(restored);
        requestError = null;
        requestFailure = null;
        _lastRestoreTask = null;
        state = AgentViewState.results;
        notifyListeners();
      } catch (error) {
        if (serial != _serial || task?.id != previous.id) return;
        requestError = _failureMessage(error);
        requestFailure = error is AgentRequestFailure ? error : null;
        state = AgentViewState.results;
        notifyListeners();
      }
      return;
    }
    if (query == null) return;
    final serial = ++_serial;
    requestError = null;
    state = AgentViewState.searching;
    notifyListeners();
    await _resolveTurn(
      serial: serial,
      query: query,
      activeTask: _lastActiveTask,
      activities: _lastActivities,
      places: _lastPlaces,
      cityID: _lastCityID,
      bounds: _lastBounds,
    );
  }

  Future<void> _resolveTurn({
    required int serial,
    required String query,
    required AgentTask? activeTask,
    required List<PublicActivity> activities,
    required List<PublicPlace> places,
    String? cityID,
    MapBounds? bounds,
  }) async {
    retireReplyRefresh();
    preserveReplyCamera = false;
    final continuing = activeTask != null;
    final previousResult = result;
    AgentResult resolved;
    try {
      resolved = bounds != null
          ? await _source.searchArea(
              activeTask,
              query,
              bounds,
              activities,
              places,
            )
          : continuing
          ? await _source.followUp(activeTask, query, activities, places)
          : await _source.resolve(query, activities, places);
    } catch (error) {
      if (serial != _serial) return;
      requestError = _failureMessage(error);
      requestFailure = error is AgentRequestFailure ? error : null;
      result = previousResult;
      state = AgentViewState.results;
      sheetExtent = AgentSheetExtent.medium;
      notifyListeners();
      // A failed POST may already have retained a native Task. Read the
      // existing history once; this does not retry, restore or select it.
      await _loadRecent(failedTurnSerial: serial);
      return;
    }
    if (serial != _serial) return;
    if (resolved.task != null) {
      task = resolved.task;
      if (resolved.task!.messages.isNotEmpty) {
        conversation
          ..clear()
          ..addAll(resolved.task!.messages);
      }
    } else if (resolved.taskID != null) {
      task = AgentTask(
        id: resolved.taskID!,
        query: query,
        status: resolved.isUnsupported ? 'FAILED' : 'COMPLETED',
        cityID: cityID,
        messages: List.of(conversation),
      );
    }
    preserveReplyCamera = false;
    result = resolved;
    _mapReply = null;
    _mapReplyMessageIndex = null;
    requestError = null;
    requestFailure = null;
    if (resolved.task == null || resolved.task!.messages.isEmpty) {
      final message = resolved.responseMessage.trim();
      if (message.isNotEmpty) {
        conversation.add(AgentMessage(role: 'assistant', text: message));
      }
    }
    _presentCurrentTypedAnswer(resolved);
    _rememberReply(resolved);
    if (resolved.onlineContext == null &&
        selectedEntityId != null &&
        !_containsEntity(resolved, selectedEntityId!)) {
      selectedEntityId = null;
    }
    if (task != null && resolved.task == null) {
      task = AgentTask(
        id: task!.id,
        query: task!.query,
        status: resolved.isUnsupported ? 'FAILED' : 'COMPLETED',
        // A public anonymous response may have no persisted remote task.
        // Bind its local CITY task to the scope captured for this live turn;
        // never replace a remote task's authority or an ONLINE scope.
        cityID:
            resolved.taskID == null &&
                resolved.onlineContext == null &&
                task!.contextType == 'CITY' &&
                task!.id.startsWith('local-')
            ? cityID ?? task!.cityID
            : task!.cityID,
        intent: task!.intent,
        principalType: task!.principalType,
        principalID: task!.principalID,
        actingUserID: task!.actingUserID,
        lastSuccessfulPublicQuery: task!.lastSuccessfulPublicQuery,
        localOriginalQuerySuperseded: task!.localOriginalQuerySuperseded,
        filters: {
          ...task!.filters,
          if (bounds != null) ...{
            'mapWest': '${bounds.west}',
            'mapSouth': '${bounds.south}',
            'mapEast': '${bounds.east}',
            'mapNorth': '${bounds.north}',
          },
        },
        messages: List.of(conversation),
      );
      task = _withSuccessfulPublicQuery(task!, resolved);
    }
    if (task != null && resolved.task == null && resolved.taskID == null) {
      _savedConversation[task!.id] = List.of(conversation);
    }
    recent.removeWhere((item) => item.id == task!.id);
    recent.insert(0, task!);
    if (recent.length > 50) {
      for (final removed in recent.skip(50)) {
        _savedConversation.remove(removed.id);
        _repliesByTask.remove(removed.id);
      }
      recent.removeRange(50, recent.length);
    }
    state = AgentViewState.results;
    sheetExtent = AgentSheetExtent.expanded;
    notifyListeners();
  }

  bool _containsEntity(AgentResult result, String id) =>
      result.entities.any((entity) => entity.id == id) ||
      result.places.any((place) => 'place:${place.id}' == id);

  // Callers already checked the current request generation and restore ID.
  // Only an authoritative successful local public read replaces these slots.
  AgentTask _withSuccessfulPublicQuery(
    AgentTask original,
    AgentResult current,
  ) {
    if (!original.id.startsWith('local-') ||
        original.contextType != 'CITY' ||
        current.taskID != null ||
        current.onlineContext != null ||
        (current.resultSet?.status != 'ready' &&
            current.resultSet?.status != 'empty')) {
      return original;
    }
    final context = current.resultSet?.publicQueryContext;
    return AgentTask(
      id: original.id,
      query: original.query,
      status: original.status,
      intent: original.intent,
      cityID: original.cityID,
      contextType: original.contextType,
      contextID: original.contextID,
      principalType: original.principalType,
      principalID: original.principalID,
      actingUserID: original.actingUserID,
      filters: original.filters,
      lastSuccessfulPublicQuery: context,
      localOriginalQuerySuperseded:
          original.localOriginalQuerySuperseded || context != null,
      messages: original.messages,
      createdAt: original.createdAt,
      updatedAt: original.updatedAt,
    );
  }

  void _refreshRestoredLocalRecent(AgentTask previous) {
    if (!previous.id.startsWith('local-') || previous.contextType != 'CITY') {
      return;
    }
    final index = recent.indexWhere((item) => item.id == previous.id);
    if (index >= 0) {
      recent[index] = task!;
    }
  }

  // Map-origin presentation is separate from list/detail selection. It never
  // changes task authority, request generation, conversation or approval.
  bool selectMapEntity(String id) {
    final currentTask = task;
    final currentResult = presentedResult;
    if (queryContextType != 'CITY' ||
        currentTask?.contextType != 'CITY' ||
        currentTask!.cityID?.isNotEmpty != true ||
        state == AgentViewState.searching ||
        requestError != null ||
        currentResult?.onlineContext != null ||
        currentResult?.resultSet?.status != 'ready') {
      return false;
    }
    const publicReadIntents = {
      'FIND_ACTIVITY',
      'FIND_ORGANIZATION',
      'FIND_PLACE',
      'AREA_DISCOVERY',
      'FIND_PUBLIC_PERSON',
      'FIND_COMMUNITY',
      'FIND_BUSINESS',
    };
    var intent = currentTask.intent;
    if (intent == 'REFINE_RESULTS' || intent == 'COMPARE_RESULTS') {
      intent = currentTask.filters['targetIntent'];
    }
    final localContext = identical(currentResult, result)
        ? currentTask.lastSuccessfulPublicQuery
        : retainsReply(currentResult!)
        ? currentResult.resultSet?.publicQueryContext
        : null;
    final localPublicRead =
        intent == null &&
        currentTask.id.startsWith('local-') &&
        currentResult?.taskID == null &&
        localContext != null &&
        localContext.cityID == currentTask.cityID &&
        identical(localContext.sourceIdentity, _source) &&
        identical(localContext, currentResult?.resultSet?.publicQueryContext);
    if (!publicReadIntents.contains(intent) && !localPublicRead) return false;
    final items = currentResult?.projectionItems;
    if (items == null ||
        !items.any(
          (item) =>
              item.entity.mapID == id &&
              item.scope == 'AUTHORIZED_VIEW' &&
              item.valid &&
              item.mapEntity != null,
        )) {
      return false;
    }
    preserveReplyCamera = false;
    selectedEntityId = id;
    sheetExtent = AgentSheetExtent.peek;
    notifyListeners();
    return true;
  }

  void selectEntity(String id) {
    preserveReplyCamera = false;
    selectedEntityId = id;
    notifyListeners();
  }

  String _failureMessage(Object error) =>
      error is AgentRequestFailure ? error.userMessage : '暂时无法获取结果，请重试。';

  void setSheetExtent(AgentSheetExtent extent) {
    sheetExtent = extent;
    notifyListeners();
  }

  void showContent(AgentContentMode mode) {
    contentMode = mode;
    if (mode == AgentContentMode.conversation) {
      sheetExtent = AgentSheetExtent.expanded;
    } else if (sheetExtent == AgentSheetExtent.peek ||
        sheetExtent == AgentSheetExtent.hidden) {
      sheetExtent = AgentSheetExtent.medium;
    }
    notifyListeners();
  }

  void newTask({bool preserveMapSelection = false}) {
    retireReplyRefresh();
    preserveReplyCamera = false;
    _mapReply = null;
    _mapReplyMessageIndex = null;
    if (preserveMapSelection &&
        result != null &&
        result!.onlineContext == null) {
      mapBackgroundResult = result;
      mapBackgroundCityID = task?.cityID;
    }
    if (!preserveMapSelection && queryContextType != 'ONLINE') {
      mapBackgroundResult = null;
      mapBackgroundCityID = null;
    }
    ++_serial;
    task = null;
    result = null;
    if (!preserveMapSelection) selectedEntityId = null;
    conversation.clear();
    requestError = null;
    requestFailure = null;
    contentMode = AgentContentMode.results;
    _lastQuery = null;
    _lastBounds = null;
    _lastActiveTask = null;
    _lastRestoreTask = null;
    sheetExtent = AgentSheetExtent.peek;
    state = AgentViewState.idle;
    notifyListeners();
  }

  Future<void> reopen(
    AgentTask previous,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    queryContextType = previous.contextType;
    newTask(preserveMapSelection: previous.contextType == 'ONLINE');
    _lastRestoreTask = previous;
    _lastActivities = activities;
    _lastPlaces = places;
    final serial = _serial;
    task = previous;
    conversation.addAll(
      previous.messages.isNotEmpty
          ? previous.messages
          : _savedConversation[previous.id] ??
                [AgentMessage(role: 'user', text: previous.query)],
    );
    state = AgentViewState.searching;
    notifyListeners();
    AgentResult restored;
    try {
      restored = await _source.restore(previous, activities, places);
    } catch (error) {
      if (serial != _serial || task?.id != previous.id) return;
      requestError = _failureMessage(error);
      requestFailure = error is AgentRequestFailure ? error : null;
      // A failed read is not an authoritative empty answer. Keep the original
      // task/history available without inventing a ResultSet or replaying it.
      result = null;
      state = AgentViewState.results;
      sheetExtent = AgentSheetExtent.medium;
      notifyListeners();
      return;
    }
    if (serial != _serial || task?.id != previous.id) return;
    requestError = null;
    requestFailure = null;
    _lastRestoreTask = null;
    preserveReplyCamera = false;
    result = restored;
    task = _withSuccessfulPublicQuery(restored.task ?? previous, restored);
    _refreshRestoredLocalRecent(previous);
    if (restored.task?.messages.isNotEmpty == true) {
      conversation
        ..clear()
        ..addAll(restored.task!.messages);
    }
    _presentCurrentTypedAnswer(restored);
    _rememberReply(restored);
    state = AgentViewState.results;
    contentMode = AgentContentMode.conversation;
    sheetExtent = AgentSheetExtent.expanded;
    notifyListeners();
  }

  // The current human-read result is the authority for this turn's visible
  // answer. This projection neither rewrites stored Task history nor approves
  // an action. Earlier turns keep their original text.
  void _presentCurrentTypedAnswer(AgentResult current) {
    for (final reply in List<AgentReply>.of(replies)) {
      final index = reply.messageIndex;
      if (index >= conversation.length ||
          conversation[index].role != 'assistant') {
        continue;
      }
      final users = conversation
          .take(index)
          .where((m) => m.role == 'user')
          .map((m) => m.text)
          .toList();
      if (!listEquals(users, reply.userMessages)) continue;
      // Authenticated persisted messages retain their own text and citations;
      // an older in-memory result cannot replace them with another turn.
      if (conversation[index].sourceReferences == null &&
          conversation[index].resultMembership == null) {
        conversation[index] = conversation[index].withText(
          reply.result.responseMessage,
        );
      }
    }
    if (current.projectionItems == null) return;
    if (conversation.isNotEmpty &&
        conversation.last.role == 'assistant' &&
        (conversation.last.sourceReferences != null ||
            conversation.last.resultMembership != null)) {
      return;
    }
    final message = current.responseMessage.trim();
    if (message.isEmpty) return;
    final answer = AgentMessage(
      role: 'assistant',
      text: message,
      sourceReferences:
          current.messageSources ??
          (conversation.isNotEmpty && conversation.last.role == 'assistant'
              ? conversation.last.sourceReferences
              : null),
    );
    if (conversation.isNotEmpty && conversation.last.role == 'assistant') {
      conversation[conversation.length - 1] = answer;
    } else {
      conversation.add(answer);
    }
  }

  void _rememberReply(AgentResult source) {
    final id = task?.id;
    final index = conversation.lastIndexWhere((m) => m.role == 'assistant');
    if (id == null || index < 0) return;
    final stored = _repliesByTask.putIfAbsent(id, () => []);
    final history = source.messageResults;
    if (history != null) {
      stored.removeWhere(
        (reply) =>
            reply.messageIndex < conversation.length &&
            conversation[reply.messageIndex].resultMembership != null,
      );
      for (final entry in history.entries) {
        final messageIndex = entry.key;
        if (messageIndex < 0 || messageIndex >= conversation.length) continue;
        final membership = conversation[messageIndex].resultMembership;
        if (conversation[messageIndex].role != 'assistant' ||
            membership == null ||
            membership.taskID != id ||
            entry.value.taskID != id ||
            entry.value.resultSet?.id != membership.resultSetID) {
          continue;
        }
        stored.removeWhere((reply) => reply.messageIndex == messageIndex);
        stored.add(
          AgentReply(
            messageIndex: messageIndex,
            // The decoder has verified that the latest membership has exactly
            // the current result's entities and map effects. Keep the current
            // object so its original actions and follow-ups remain available.
            result: messageIndex == index ? source : entry.value,
            userMessages: conversation
                .take(messageIndex)
                .where((m) => m.role == 'user')
                .map((m) => m.text)
                .toList(growable: false),
          ),
        );
      }
    }
    if (history == null || conversation[index].resultMembership == null) {
      stored.removeWhere((reply) => reply.messageIndex == index);
      stored.add(
        AgentReply(
          messageIndex: index,
          result: source,
          userMessages: conversation
              .take(index)
              .where((m) => m.role == 'user')
              .map((m) => m.text)
              .toList(growable: false),
        ),
      );
    }
    stored.sort((a, b) => a.messageIndex.compareTo(b.messageIndex));
    // Reuse the existing per-message resultBuilder for citations recovered
    // from earlier turns. These replies have no entity/map result projection.
    for (final (messageIndex, message) in conversation.indexed) {
      final sources = message.sourceReferences;
      if (message.role != 'assistant' ||
          sources == null ||
          stored.any((reply) => reply.messageIndex == messageIndex)) {
        continue;
      }
      stored.add(
        AgentReply(
          messageIndex: messageIndex,
          result: AgentResult(
            entities: const [],
            activities: const [],
            places: const [],
            note: '',
            message: message.text,
            taskID: id,
            messageSources: sources,
          ),
          userMessages: conversation
              .take(messageIndex)
              .where((m) => m.role == 'user')
              .map((m) => m.text)
              .toList(growable: false),
        ),
      );
    }
    stored.sort((a, b) => a.messageIndex.compareTo(b.messageIndex));
    _replyRefreshRetired = false;
    _replyRefreshFailed = false;
    _scheduleReplyExpiry();
  }

  /// Retire a pending GET synchronously on an observed identity/scope change.
  /// A -> B -> A does not revive its response. No task or query is mutated.
  void retireReplyRefresh() {
    ++_replyRefreshSerial;
    _replyRefreshRetired = true;
    _replyResumePending = false;
    _replyRefreshTimer?.cancel();
  }

  /// Called by the original route/app lifecycle. Visibility is not authority.
  void updateReplyRefreshVisibility() {
    final visible = !_disposed && _replyRefreshAllowed();
    if (visible == _replyRefreshVisible) return;
    _replyRefreshVisible = visible;
    ++_replyRefreshSerial;
    _replyRefreshTimer?.cancel();
    if (visible && !_replyRefreshFailed && !_replyRefreshRetired) {
      if (_replyRefreshInFlight) {
        _replyResumePending = true;
      } else {
        unawaited(refreshReplyProjections());
      }
    }
  }

  bool get _canRefreshReplies =>
      !_disposed &&
      !_replyRefreshRetired &&
      _replyRefreshVisible &&
      _replyRefreshAllowed() &&
      task != null &&
      result != null &&
      queryContextType == 'CITY' &&
      state != AgentViewState.searching &&
      requestError == null &&
      _source.canRereadReplyProjections(task!) &&
      conversation.isNotEmpty &&
      conversation.last.role == 'assistant' &&
      conversation.last.resultMembership != null;

  bool _sameReplyTask(AgentTask a, AgentTask b) =>
      a.id == b.id &&
      a.query == b.query &&
      a.status == b.status &&
      a.intent == b.intent &&
      a.cityID == b.cityID &&
      a.contextType == b.contextType &&
      a.contextID == b.contextID &&
      a.principalType == b.principalType &&
      a.principalID == b.principalID &&
      a.actingUserID == b.actingUserID &&
      mapEquals(a.filters, b.filters) &&
      a.createdAt == b.createdAt &&
      a.updatedAt == b.updatedAt &&
      _sameReplyMessages(a.messages, b.messages);

  bool _sameReplyMessages(List<AgentMessage> a, List<AgentMessage> b) {
    if (a.length != b.length) return false;
    for (var i = 0; i < a.length; i++) {
      if (a[i].role != b[i].role || a[i].text != b[i].text) return false;
      final x = a[i].resultMembership, y = b[i].resultMembership;
      if (x == null || y == null) {
        if (x != y) return false;
      } else if (x.taskID != y.taskID ||
          x.cityID != y.cityID ||
          x.kind != y.kind ||
          x.turnDigest != y.turnDigest ||
          x.resultSetID != y.resultSetID ||
          !listEquals(x.refs, y.refs)) {
        return false;
      }
    }
    return true;
  }

  /// Refresh only the authorized entity projections of the exact open reply.
  /// No POST, restore, newTask, conversation append or model request is used.
  Future<bool> refreshReplyProjections() {
    if (_replyRefreshInFlight) return _replyRefreshFuture!;
    if (!_canRefreshReplies) return Future.value(false);
    _replyRefreshInFlight = true;
    final original = task!,
        serial = _serial,
        refreshSerial = _replyRefreshSerial;
    final messages = List<AgentMessage>.of(conversation);
    final filters = Map<String, String>.of(original.filters);
    _replyRefreshTimer?.cancel();
    bool current() =>
        _canRefreshReplies &&
        serial == _serial &&
        refreshSerial == _replyRefreshSerial &&
        identical(task, original) &&
        mapEquals(filters, original.filters) &&
        _sameReplyMessages(messages, conversation);
    Future<bool> read() async {
      try {
        final fresh = await _source.rereadReplyProjections(original);
        if (!current() || fresh == null) return false;
        if (fresh.taskID != original.id ||
            fresh.task == null ||
            fresh.messageResults == null ||
            !_sameReplyTask(original, fresh.task!) ||
            !_sameReplyMessages(messages, fresh.task!.messages)) {
          _replyRefreshFailed = true;
          return false;
        }
        final oldMap = _mapReply;
        final mapIndex = oldMap == null
            ? null
            : _mapReplyMessageIndex ??
                  replies
                      .where((reply) => identical(reply.result, oldMap))
                      .map((reply) => reply.messageIndex)
                      .firstOrNull;
        result = fresh;
        preserveReplyCamera = true;
        _rememberReply(fresh);
        if (oldMap != null) {
          _mapReply =
              (mapIndex == null
                  ? null
                  : replies
                        .where((reply) => reply.messageIndex == mapIndex)
                        .map((reply) => reply.result)
                        .firstOrNull) ??
              _retiredReplyMap(original.id);
        }
        final visible = presentedResult;
        if (selectedEntityId != null &&
            (visible == null || !_containsEntity(visible, selectedEntityId!))) {
          selectedEntityId = null;
        }
        notifyListeners();
        return true;
      } catch (_) {
        // The original query error/state remain untouched. Do not turn a
        // failed read into a paid retry or a repeated automatic GET loop.
        if (current()) _replyRefreshFailed = true;
        return false;
      } finally {
        _replyRefreshInFlight = false;
        _replyRefreshFuture = null;
        if (!_disposed && !_replyRefreshFailed) _scheduleReplyExpiry();
        if (_replyResumePending && !_disposed) {
          _replyResumePending = false;
          if (!_replyRefreshFailed) unawaited(refreshReplyProjections());
        }
      }
    }

    return _replyRefreshFuture = read();
  }

  // Empty presentation only: no old coordinates, cards, source links, answer
  // text or grants survive a missing historical projection. Keeping the
  // closed lifetime also prevents the map from using city catalog fallbacks.
  AgentResult _retiredReplyMap(String id) => AgentResult(
    entities: const [],
    activities: const [],
    places: const [],
    note: '',
    taskID: id,
    replyProjection: AgentReplyProjectionLifetime(
      validUntil: DateTime.utc(1970),
      current: () => false,
    ),
  );

  void _scheduleReplyExpiry() {
    _replyExpiryTimer?.cancel();
    _replyRefreshTimer?.cancel();
    Duration? next;
    for (final source in [result, ...replies.map((reply) => reply.result)]) {
      final remaining = source?.replyProjection?.remaining;
      if (remaining != null &&
          remaining > Duration.zero &&
          (next == null || remaining < next)) {
        next = remaining;
      }
    }
    if (next != null) {
      _replyExpiryTimer = Timer(next, () {
        // The temporary empty set must not move the camera while a new query
        // waits. Accepted explicit query/restore results reset this policy.
        preserveReplyCamera = true;
        _scheduleReplyExpiry();
        notifyListeners();
      });
      if (!_disposed &&
          !_replyRefreshRetired &&
          _replyRefreshVisible &&
          _replyRefreshAllowed() &&
          task != null &&
          _source.canRereadReplyProjections(task!) &&
          !_replyRefreshFailed &&
          !_replyRefreshInFlight) {
        const lead = Duration(seconds: 5);
        // A near-expired response must not cause a zero-delay GET loop.
        final delay = next > lead ? next - lead : lead;
        _replyRefreshTimer = Timer(delay, () {
          if (!_replyRefreshFailed) unawaited(refreshReplyProjections());
        });
      }
    }
  }

  bool canUseReplyProjection(AgentResult source) =>
      retainsReply(source) && source.replyProjectionCurrent;

  bool retainsReply(AgentResult source) =>
      identical(result, source) ||
      replies.any((reply) => identical(reply.result, source));

  bool retainsAnswerSources(AgentResult source) =>
      retainsReply(source) &&
      task?.id == source.resultSet?.answerSources?.taskID &&
      source.taskID == source.resultSet?.answerSources?.taskID;

  AgentMessageSources? messageSourcesForReply(AgentResult source) {
    for (final reply in replies) {
      if (identical(reply.result, source)) {
        return conversation[reply.messageIndex].sourceReferences;
      }
    }
    return null;
  }

  bool retainsMessageSources(AgentResult source, AgentMessageSources sources) =>
      retainsReply(source) &&
      source.taskID == task?.id &&
      identical(messageSourcesForReply(source), sources);

  void showReplyOnMap(AgentResult source, String id) {
    if (queryContextType != 'CITY' ||
        source.onlineContext != null ||
        !canUseReplyProjection(source) ||
        !source.entities.any((entity) => entity.id == id)) {
      return;
    }
    preserveReplyCamera = false;
    _mapReply = source;
    _mapReplyMessageIndex = replies
        .where((reply) => identical(reply.result, source))
        .map((reply) => reply.messageIndex)
        .firstOrNull;
    selectedEntityId = id;
    sheetExtent = AgentSheetExtent.peek;
    notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    retireReplyRefresh();
    _replyExpiryTimer?.cancel();
    ++_serial;
    ++_historySerial;
    _source.dispose();
    super.dispose();
  }
}
