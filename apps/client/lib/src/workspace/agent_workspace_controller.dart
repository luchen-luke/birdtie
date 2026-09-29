import 'package:flutter/foundation.dart';

import '../city/public_city_controller.dart';
import 'map_entities.dart';

enum AgentViewState { idle, typing, searching, results, conversation }

enum AgentSheetExtent { compact, half, full }

class AgentTask {
  const AgentTask({
    required this.id,
    required this.query,
    required this.status,
    this.intent,
    this.cityID,
    this.principalType = 'PERSON',
    this.principalID = '',
    this.actingUserID = '',
    this.filters = const {},
    this.messages = const [],
    this.createdAt,
    this.updatedAt,
  });
  final String id;
  final String query;
  final String status;
  final String? intent;
  final String? cityID;
  final String principalType;
  final String principalID;
  final String actingUserID;
  final Map<String, String> filters;
  final List<AgentMessage> messages;
  final DateTime? createdAt;
  final DateTime? updatedAt;

  factory AgentTask.fromJson(Map<String, dynamic> json) => AgentTask(
    id: json['id'] as String,
    query: json['query'] as String,
    status: json['status'] as String,
    intent: json['intent'] as String?,
    cityID: json['cityContext'] as String? ?? json['cityId'] as String?,
    principalType: json['principalType'] as String? ?? 'PERSON',
    principalID: json['principalId'] as String? ?? '',
    actingUserID: json['actingUserId'] as String? ?? '',
    filters: (json['filters'] as Map<String, dynamic>? ?? const {}).map(
      (key, value) => MapEntry(key, value as String),
    ),
    messages: [
      for (final item in json['conversation'] as List<dynamic>? ?? const [])
        AgentMessage.fromJson(item as Map<String, dynamic>),
    ],
    createdAt: DateTime.tryParse(json['createdAt'] as String? ?? ''),
    updatedAt: DateTime.tryParse(json['updatedAt'] as String? ?? ''),
  );
}

class AgentMessage {
  const AgentMessage({required this.role, required this.text});
  final String role;
  final String text;
  factory AgentMessage.fromJson(Map<String, dynamic> json) =>
      AgentMessage(role: json['role'] as String, text: json['text'] as String);
}

class AgentResult {
  const AgentResult({
    required this.entities,
    required this.activities,
    required this.places,
    required this.note,
    this.people = const [],
    this.groups = const [],
    this.taskID,
    this.task,
  });
  final List<MapEntity> entities;
  final List<PublicActivity> activities;
  final List<PublicPlace> places;
  final String note;
  final List<AgentPerson> people;
  final List<AgentGroup> groups;
  final String? taskID;
  final AgentTask? task;
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
  Future<AgentResult> restore(
    AgentTask task,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) => resolve(task.query, activities, places);
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
        note:
            'I can currently help you find nearby activities. Try: Find badminton this weekend.',
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
          ? 'No matching published activities are available. Connect the local API and development seed to try the full Agent loop.'
          : 'Published Birdtie activities · local rule-based preview',
    );
  }
}

class AgentWorkspaceController extends ChangeNotifier {
  AgentWorkspaceController({AgentTaskSource? source})
    : _source = source ?? const LocalAgentTaskSource();
  final AgentTaskSource _source;
  AgentViewState state = AgentViewState.idle;
  AgentSheetExtent sheetExtent = AgentSheetExtent.compact;
  AgentTask? task;
  AgentResult? result;
  String? selectedEntityId;
  final List<AgentTask> recent = [];
  final List<String> conversation = [];
  final Map<String, List<String>> _savedConversation = {};
  int _serial = 0;
  int _historySerial = 0;

  Future<void> loadRecent() async {
    final serial = ++_historySerial;
    try {
      final stored = await _source.loadRecent();
      if (serial != _historySerial) return;
      final local = recent
          .where((item) => item.id.startsWith('local-'))
          .toList();
      recent
        ..clear()
        ..addAll([...local, ...stored]);
      notifyListeners();
    } catch (_) {
      // Keep current-session tasks if history is unavailable.
    }
  }

  void clearAccountContext() {
    ++_historySerial;
    newTask();
    recent.clear();
    _savedConversation.clear();
    notifyListeners();
  }

  void beginTyping() {
    if (state == AgentViewState.idle || state == AgentViewState.results) {
      state = AgentViewState.typing;
      notifyListeners();
    }
  }

  void stopTyping() {
    if (state == AgentViewState.typing) {
      state = result == null ? AgentViewState.idle : AgentViewState.results;
      notifyListeners();
    }
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
    result = null;
    selectedEntityId = null;
    sheetExtent = AgentSheetExtent.compact;
    state = AgentViewState.searching;
    conversation.add(query);
    notifyListeners();
    AgentResult resolved;
    try {
      resolved = continuing
          ? await _source.followUp(previousTask!, query, activities, places)
          : await _source.resolve(query, activities, places);
    } catch (_) {
      if (serial != _serial) return;
      task = AgentTask(
        id: task!.id,
        query: task!.query,
        status: 'FAILED',
        cityID: task!.cityID,
        intent: task!.intent,
      );
      result = const AgentResult(
        entities: [],
        activities: [],
        places: [],
        note: 'Results are unavailable right now. Please try again.',
      );
      state = AgentViewState.results;
      sheetExtent = AgentSheetExtent.half;
      notifyListeners();
      return;
    }
    if (serial != _serial) return;
    if (resolved.task != null) {
      task = resolved.task;
      conversation
        ..clear()
        ..addAll(resolved.task!.messages.map((message) => message.text));
    } else if (resolved.taskID != null) {
      task = AgentTask(
        id: resolved.taskID!,
        query: query,
        status: 'COMPLETED',
        cityID: cityID,
      );
    }
    result = resolved;
    if (task != null && resolved.task == null) {
      task = AgentTask(
        id: task!.id,
        query: task!.query,
        status: 'COMPLETED',
        cityID: task!.cityID,
        intent: task!.intent,
      );
    }
    if (resolved.taskID == null && resolved.task == null) {
      _savedConversation[task!.id] = List.of(conversation);
    }
    recent.removeWhere((item) => item.id == task!.id);
    recent.insert(0, task!);
    if (recent.length > 50) {
      for (final removed in recent.skip(50)) {
        _savedConversation.remove(removed.id);
      }
      recent.removeRange(50, recent.length);
    }
    state = AgentViewState.results;
    sheetExtent = AgentSheetExtent.half;
    notifyListeners();
  }

  void selectEntity(String id) {
    selectedEntityId = id;
    sheetExtent = AgentSheetExtent.half;
    notifyListeners();
  }

  void setSheetExtent(AgentSheetExtent extent) {
    sheetExtent = extent;
    state = extent == AgentSheetExtent.full
        ? AgentViewState.conversation
        : AgentViewState.results;
    notifyListeners();
  }

  void newTask() {
    ++_serial;
    task = null;
    result = null;
    selectedEntityId = null;
    conversation.clear();
    sheetExtent = AgentSheetExtent.compact;
    state = AgentViewState.idle;
    notifyListeners();
  }

  Future<void> reopen(
    AgentTask previous,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    newTask();
    final serial = _serial;
    task = previous;
    conversation.addAll(
      previous.messages.isNotEmpty
          ? previous.messages.map((message) => message.text)
          : _savedConversation[previous.id] ?? [previous.query],
    );
    state = AgentViewState.searching;
    notifyListeners();
    AgentResult restored;
    try {
      restored = await _source.restore(previous, activities, places);
    } catch (_) {
      restored = const AgentResult(
        entities: [],
        activities: [],
        places: [],
        note: 'This task could not be restored. Please try again.',
      );
    }
    if (serial != _serial || task?.id != previous.id) return;
    result = restored;
    if (restored.task != null) task = restored.task;
    state = AgentViewState.results;
    sheetExtent = AgentSheetExtent.half;
    notifyListeners();
  }

  @override
  void dispose() {
    _source.dispose();
    super.dispose();
  }
}
