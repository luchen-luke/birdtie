import 'dart:async';

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
  });
  final List<MapEntity> entities;
  final List<PublicActivity> activities;
  final List<PublicPlace> places;
  final String note;
}

/// A replaceable boundary for the future Agent API. No network or LLM calls here.
abstract class AgentTaskSource {
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  );
}

class LocalAgentTaskSource implements AgentTaskSource {
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
  AgentViewState state = AgentViewState.idle;
  AgentSheetExtent sheetExtent = AgentSheetExtent.compact;
  AgentTask? task;
  AgentResult? result;
  String? selectedEntityId;
  final List<AgentTask> recent = [];
  final List<String> conversation = [];
  final Map<String, AgentResult> _savedResults = {};
  final Map<String, List<String>> _savedConversation = {};
  int _serial = 0;

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
    conversation.add(query);
    notifyListeners();
    final resolved = await _source.resolve(query, activities, places);
    if (serial != _serial) return;
    result = resolved;
    _savedResults[task!.id] = resolved;
    _savedConversation[task!.id] = List.of(conversation);
    recent.removeWhere((item) => item.query == query);
    recent.insert(0, task!);
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

  void reopen(
    AgentTask previous,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) {
    newTask();
    final saved = _savedResults[previous.id];
    if (saved == null) {
      unawaited(
        submit(previous.query, activities, places, cityID: previous.cityID),
      );
      return;
    }
    task = previous;
    result = saved;
    conversation.addAll(_savedConversation[previous.id] ?? [previous.query]);
    state = AgentViewState.results;
    sheetExtent = AgentSheetExtent.half;
    notifyListeners();
  }
}
