import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:flutter_test/flutter_test.dart';

class _Source extends AgentTaskSource {
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async => const AgentResult(
    entities: [],
    activities: [],
    places: [],
    note: 'preview',
  );
}

class _ChangingSource extends AgentTaskSource {
  int calls = 0;

  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async => AgentResult(
    entities: const [],
    activities: const [],
    places: const [],
    note: 'version ${++calls}',
  );
}

class _FollowUpSource extends AgentTaskSource {
  AgentTask? continuedTask;

  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async => const AgentResult(
    entities: [],
    activities: [],
    places: [],
    note: 'first result',
  );

  @override
  Future<AgentResult> followUp(
    AgentTask task,
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    continuedTask = task;
    return AgentResult(
      entities: const [],
      activities: const [],
      places: const [],
      note: 'follow-up result',
      task: AgentTask(
        id: task.id,
        query: task.query,
        status: 'COMPLETED',
        cityID: task.cityID,
        messages: const [
          AgentMessage(role: 'user', text: 'Find badminton this weekend'),
          AgentMessage(role: 'assistant', text: 'Found activities.'),
          AgentMessage(role: 'user', text: 'Anything closer?'),
          AgentMessage(role: 'assistant', text: 'Showing closer activities.'),
        ],
      ),
    );
  }
}

void main() {
  test(
    'intent moves through typing, results and conversation, then New clears context',
    () async {
      final workspace = AgentWorkspaceController(source: _Source());
      workspace.beginTyping();
      expect(workspace.state, AgentViewState.typing);
      await workspace.submit('  badminton this weekend  ', [], []);
      expect(workspace.state, AgentViewState.results);
      expect(workspace.task?.query, 'badminton this weekend');
      expect(workspace.sheetExtent, AgentSheetExtent.half);
      expect(workspace.recent.single.query, 'badminton this weekend');
      await workspace.submit('Anything closer?', [], []);
      expect(workspace.recent, hasLength(1));
      expect(workspace.task?.query, 'badminton this weekend');
      workspace.setSheetExtent(AgentSheetExtent.full);
      expect(workspace.state, AgentViewState.conversation);
      workspace.newTask();
      expect(workspace.state, AgentViewState.idle);
      expect(workspace.task, isNull);
      expect(workspace.result, isNull);
      expect(workspace.conversation, isEmpty);
      expect(workspace.recent, hasLength(1));
      workspace.dispose();
    },
  );

  test('Recent refreshes results instead of reusing an old snapshot', () async {
    final source = _ChangingSource();
    final workspace = AgentWorkspaceController(source: source);
    await workspace.submit('badminton', [], []);
    final previous = workspace.task!;
    expect(workspace.result?.note, 'version 1');
    await workspace.reopen(previous, [], []);
    expect(source.calls, 2);
    expect(workspace.result?.note, 'version 2');
    workspace.dispose();
  });

  test('follow-up keeps one task and New preserves it in Recent', () async {
    final source = _FollowUpSource();
    final workspace = AgentWorkspaceController(source: source);
    await workspace.submit(
      'Find badminton this weekend',
      [],
      [],
      cityID: 'aberdeen',
    );
    final id = workspace.task!.id;
    await workspace.submit('Anything closer?', [], [], cityID: 'aberdeen');
    expect(source.continuedTask?.id, id);
    expect(workspace.task?.id, id);
    expect(workspace.task?.query, 'Find badminton this weekend');
    expect(workspace.conversation, [
      'Find badminton this weekend',
      'Found activities.',
      'Anything closer?',
      'Showing closer activities.',
    ]);
    workspace.newTask();
    expect(workspace.task, isNull);
    expect(workspace.recent.single.id, id);
    workspace.dispose();
  });

  test('New starts a separate task and Recent keeps both contexts', () async {
    final workspace = AgentWorkspaceController(source: _Source());
    await workspace.submit('badminton', [], [], cityID: 'aberdeen');
    final first = workspace.task!;
    workspace.newTask();
    await workspace.submit('badminton', [], [], cityID: 'edinburgh');
    expect(workspace.conversation, ['badminton']);
    expect(workspace.recent, hasLength(2));
    expect(workspace.recent.first.cityID, 'edinburgh');
    expect(workspace.recent.last.cityID, 'aberdeen');
    expect(workspace.recent.last.id, first.id);
    await workspace.reopen(first, [], []);
    expect(workspace.conversation, ['badminton']);
    workspace.dispose();
  });
}
