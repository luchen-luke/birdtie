import 'dart:convert';

import 'package:http/http.dart' as http;

import '../city/public_city_controller.dart';
import 'agent_workspace_controller.dart';
import 'map_entities.dart';

class RemoteAgentTaskSource extends AgentTaskSource {
  RemoteAgentTaskSource({
    required this.cityID,
    required this.authorizationHeader,
    this.organizationWorkspaceID,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _apiBaseUrl = apiBaseUrl ?? apiBase;

  static const apiBase = String.fromEnvironment('BIRDTIE_API_BASE_URL');
  final String? Function() cityID;
  final String? Function() authorizationHeader;
  final String? Function()? organizationWorkspaceID;
  final http.Client _client;
  final String _apiBaseUrl;

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
  ) => task.id.startsWith('local-')
      ? _submit('${task.query}. $query', null)
      : _submit(query, task.id);

  Future<AgentResult> _submit(String query, String? taskID) async {
    final selected = cityID();
    if (selected == null) throw StateError('No selected city');
    final response = await _client
        .post(
          _endpoint('/v1/cities/${Uri.encodeComponent(selected)}/agent/tasks'),
          headers: _headers(json: true),
          body: jsonEncode({'query': query, 'taskId': ?taskID}),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw StateError('Agent query unavailable');
    return _parseResult(
      (jsonDecode(response.body) as Map<String, dynamic>)['data']
          as Map<String, dynamic>,
    );
  }

  @override
  Future<List<AgentTask>> loadRecent() async {
    if (authorizationHeader() == null) return [];
    final response = await _client
        .get(_endpoint('/v1/me/agent-tasks'), headers: _headers())
        .timeout(const Duration(seconds: 10));
    if (response.statusCode != 200) {
      throw StateError('Task history unavailable');
    }
    final records =
        (jsonDecode(response.body) as Map<String, dynamic>)['data']
            as List<dynamic>;
    return [
      for (final raw in records)
        AgentTask.fromJson(raw as Map<String, dynamic>),
    ];
  }

  @override
  Future<AgentResult> restore(
    AgentTask task,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    if (task.id.startsWith('local-')) {
      return resolve(task.query, activities, places);
    }
    if (authorizationHeader() == null) {
      throw StateError('Sign in to restore task');
    }
    final response = await _client
        .get(
          _endpoint('/v1/me/agent-tasks/${Uri.encodeComponent(task.id)}'),
          headers: _headers(),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw StateError('Task unavailable');
    return _parseResult(
      (jsonDecode(response.body) as Map<String, dynamic>)['data']
          as Map<String, dynamic>,
    );
  }

  AgentResult _parseResult(Map<String, dynamic> data) {
    final activities = [
      for (final raw in data['activities'] as List<dynamic>)
        PublicActivity.fromJson(raw as Map<String, dynamic>),
    ];
    final places = [
      for (final raw in data['places'] as List<dynamic>)
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
          mapLatitude: (raw['mapLatitude'] as num?)?.toDouble(),
          mapLongitude: (raw['mapLongitude'] as num?)?.toDouble(),
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
    final entities = <MapEntity>[
      for (final person in people)
        if (person.mapLatitude != null && person.mapLongitude != null)
          MapEntity(
            id: 'person:${person.accountID}',
            kind: MapEntityKind.person,
            title: person.displayName,
            subtitle: 'Approximate area · ${person.areaLabel}',
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
              subtitle: activity.status,
              latitude: location.latitude!,
              longitude: location.longitude!,
            ),
      for (final place in places)
        if (place.location.hasPublicPoint)
          MapEntity(
            id: 'place:${place.id}',
            kind: MapEntityKind.place,
            title: place.name,
            subtitle: '${place.categoryCode} · ${place.source.label}',
            latitude: place.location.latitude!,
            longitude: place.location.longitude!,
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
              subtitle: 'Published group',
              latitude: (location['latitude'] as num).toDouble(),
              longitude: (location['longitude'] as num).toDouble(),
            ),
    ];
    final count =
        activities.length + people.length + groups.length + places.length;
    return AgentResult(
      entities: entities,
      activities: activities,
      people: people,
      groups: groups,
      places: places,
      taskID: data['taskId'] as String?,
      task: data['task'] is Map<String, dynamic>
          ? AgentTask.fromJson(data['task'] as Map<String, dynamic>)
          : null,
      note:
          data['note'] as String? ??
          (count == 0
              ? 'No visible matches yet. Birdtie used published records and simple text matching.'
              : 'Published Birdtie records · simple text matching'),
    );
  }

  @override
  void dispose() => _client.close();
}
