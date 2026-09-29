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
  });
  final String id;
  final String query;
  final String status;
  final String? intent;
  final String? cityID;
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
  });
  final List<MapEntity> entities;
  final List<PublicActivity> activities;
  final List<PublicPlace> places;
  final String note;
  final List<AgentPerson> people;
  final List<AgentGroup> groups;
  final String? taskID;
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
    final words = query
        .toLowerCase()
        .split(RegExp(r'\s+'))
        .where((word) => word.length > 2);
    final matching = activities.where((activity) {
      final text = '${activity.title} ${activity.summary}'.toLowerCase();
      return words.any(text.contains);
    }).toList();
    final matchingPlaces = places.where((place) {
      final text = '${place.name} ${place.summary} ${place.categoryCode}'
          .toLowerCase();
      return words.any(text.contains);
    }).toList();
    if (query.toLowerCase().contains('badminton')) {
      return AgentResult(
        entities: demoBadmintonEntities,
        activities: matching,
        places: matchingPlaces,
        note: 'Local demo suggestions. People and groups are not live matches.',
      );
    }
    return AgentResult(
      entities: const [],
      activities: matching,
      places: matchingPlaces,
      note: matching.isEmpty && matchingPlaces.isEmpty
          ? 'No matching published activities yet. Agent search is a local preview.'
          : 'Published records from Birdtie City API. Agent search is a local preview.',
    );
  }
}

class AgentWorkspaceController extends ChangeNotifier {
  AgentWorkspaceController({AgentTaskSource? source})
    : _source = source ?? const LocalAgentTaskSource();
  final AgentTaskSource _source;
  bool get demoMode => _source is LocalAgentTaskSource;
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
    task = AgentTask(
      id: 'local-$serial',
      query: query,
      status: 'active',
      cityID: cityID,
    );
    result = null;
    selectedEntityId = null;
    sheetExtent = AgentSheetExtent.compact;
    state = AgentViewState.searching;
    conversation.clear();
    conversation.add(query);
    notifyListeners();
    AgentResult resolved;
    try {
      resolved = await _source.resolve(query, activities, places);
    } catch (_) {
      if (serial != _serial) return;
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
    if (resolved.taskID != null) {
      task = AgentTask(
        id: resolved.taskID!,
        query: query,
        status: 'active',
        cityID: cityID,
      );
    }
    result = resolved;
    if (demoMode || resolved.taskID == null) {
      _savedConversation[task!.id] = List.of(conversation);
    }
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
    conversation.addAll(_savedConversation[previous.id] ?? [previous.query]);
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
