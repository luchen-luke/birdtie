import 'dart:async';

import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:flutter_test/flutter_test.dart';

const place = MapEntity(
  id: 'place:unit-same-place',
  kind: MapEntityKind.place,
  title: 'UNIT place',
  subtitle: 'Synthetic',
  latitude: 57.15,
  longitude: -2.09,
);

AgentTask placeTask(
  String query, {
  String category = 'restaurant',
  String term = '',
  String scope = 'city',
}) => AgentTask(
  id: 'unit-original-place-task',
  query: '找地点',
  status: 'COMPLETED',
  intent: 'FIND_PLACE',
  cityID: 'unit-city',
  filters: {
    'targetIntent': 'FIND_PLACE',
    'category': category,
    'searchTerm': term,
    'locationPreference': scope,
    'currentQuery': query,
  },
  messages: [
    AgentMessage(role: 'user', text: query),
    const AgentMessage(role: 'assistant', text: 'UNIT reply'),
  ],
);
AgentResult result(AgentTask task) => AgentResult(
  entities: const [place],
  activities: const [],
  places: const [],
  note: 'UNIT results',
  task: task,
);

class PlaceRefinementSource extends AgentTaskSource {
  final turns = <String>[];
  AgentTask? initial;
  AgentResult? next;
  Completer<AgentResult>? pending;
  bool fail = false;
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async => result(initial ?? placeTask(query));
  @override
  Future<AgentResult> followUp(
    AgentTask task,
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    turns.add(query);
    expect(task.id, 'unit-original-place-task');
    if (fail)
      throw const AgentRequestFailure(
        'UNIT denied',
        statusCode: 403,
        code: 'agent_context_denied',
      );
    return pending == null
        ? next ?? result(placeTask(query, category: 'cafe'))
        : pending!.future;
  }

  @override
  Future<List<AgentTask>> loadRecent() async => [];
}

void main() {
  test(
    'accepted category refinement clears prior selection even for same canonical entity',
    () async {
      final source = PlaceRefinementSource();
      final workspace = AgentWorkspaceController(source: source);
      await workspace.submit('找地点', [], []);
      workspace.selectEntity(place.id);
      await workspace.submit('只看咖啡馆', [], []);
      expect(workspace.selectedEntityId, isNull);
      expect(workspace.task!.id, 'unit-original-place-task');
      expect(workspace.result!.entities.single.id, place.id);
      expect(source.turns, ['只看咖啡馆']);
      workspace.dispose();
    },
  );
  test(
    'accepted scope refinement clears selection while unchanged fact question keeps selection',
    () async {
      final source = PlaceRefinementSource();
      final workspace = AgentWorkspaceController(source: source);
      await workspace.submit('找地点', [], []);
      workspace.selectEntity(place.id);
      source.next = result(placeTask('地址'));
      await workspace.submit('地址', [], []);
      expect(workspace.selectedEntityId, place.id);
      source.next = result(placeTask('只看当前地图范围', scope: 'viewport'));
      await workspace.submit('只看当前地图范围', [], []);
      expect(workspace.selectedEntityId, isNull);
      workspace.dispose();
    },
  );
  test(
    'fact question can bind public name without clearing current place selection',
    () async {
      final source = PlaceRefinementSource();
      final workspace = AgentWorkspaceController(source: source);
      await workspace.submit('找地点', [], []);
      workspace.selectEntity(place.id);
      source.next = result(placeTask('地址', term: 'UNIT place'));
      await workspace.submit('地址', [], []);
      expect(workspace.selectedEntityId, place.id);
      workspace.dispose();
    },
  );
  test('failed refinement preserves prior result and selection', () async {
    final source = PlaceRefinementSource();
    final workspace = AgentWorkspaceController(source: source);
    await workspace.submit('找地点', [], []);
    workspace.selectEntity(place.id);
    final previous = workspace.result;
    source.fail = true;
    await workspace.submit('只看咖啡馆', [], []);
    expect(workspace.result, same(previous));
    expect(workspace.selectedEntityId, place.id);
    expect(workspace.requestFailure!.code, 'agent_context_denied');
    workspace.dispose();
  });
  test('same category refinement retires named result selection', () async {
    final source = PlaceRefinementSource()
      ..initial = placeTask('找地点 UNIT place', term: 'UNIT place');
    final workspace = AgentWorkspaceController(source: source);
    await workspace.submit('找地点 UNIT place', [], []);
    workspace.selectEntity(place.id);
    source.next = result(placeTask('改成餐馆'));
    await workspace.submit('改成餐馆', [], []);
    expect(workspace.selectedEntityId, isNull);
    expect(workspace.result!.entities.single.id, place.id);
    workspace.dispose();
  });
  test(
    'stale refinement response cannot clear newer successful selection',
    () async {
      final source = PlaceRefinementSource();
      final workspace = AgentWorkspaceController(source: source);
      await workspace.submit('找地点', [], []);
      workspace.selectEntity(place.id);
      final stale = Completer<AgentResult>();
      source.pending = stale;
      final first = workspace.submit('只看咖啡馆', [], []);
      source.pending = null;
      source.next = result(placeTask('地址'));
      await workspace.submit('地址', [], []);
      workspace.selectEntity(place.id);
      stale.complete(result(placeTask('只看咖啡馆', category: 'cafe')));
      await first;
      expect(workspace.selectedEntityId, place.id);
      expect(workspace.task!.filters['currentQuery'], '地址');
      workspace.dispose();
    },
  );
}
