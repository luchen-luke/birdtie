import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:flutter_test/flutter_test.dart';

const failure = AgentRequestFailure(
  'Synthetic unavailable facts',
  statusCode: 503,
  code: 'place_answer_unavailable',
);
const task = AgentTask(
  id: '77777777-7777-4777-8777-777777777777',
  query: '找地点 Glenhouse',
  status: 'COMPLETED',
  cityID: 'aberdeen-gb',
  messages: [
    AgentMessage(role: 'user', text: '找地点 Glenhouse'),
    AgentMessage(
      role: 'assistant',
      text: 'Synthetic reply, not provider evidence',
    ),
  ],
);

class Source extends AgentTaskSource {
  bool initialFailure = false;
  final submitted = <String>[];
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    submitted.add(query);
    if (initialFailure) throw failure;
    return const AgentResult(
      entities: [],
      activities: [],
      places: [],
      note: 'Synthetic reply',
      task: task,
    );
  }

  @override
  Future<AgentResult> followUp(
    AgentTask active,
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    submitted.add(query);
    expect(active.id, task.id);
    throw failure;
  }

  @override
  Future<List<AgentTask>> loadRecent() async => [];
}

void main() {
  test(
    'failed followup keeps expanded conversation and exact original Task/query',
    () async {
      final source = Source();
      final workspace = AgentWorkspaceController(source: source);
      await workspace.submit(task.query, [], []);
      final previous = workspace.result;
      await workspace.submit('地址', [], []);
      expect(workspace.sheetExtent, AgentSheetExtent.expanded);
      expect(workspace.task!.id, task.id);
      expect(workspace.result, same(previous));
      expect(workspace.conversation.last.text, '地址');
      expect(source.submitted, [task.query, '地址']);
      expect(workspace.requestFailure, same(failure));
      workspace.dispose();
    },
  );
  test(
    'new query failure retains existing medium failure presentation',
    () async {
      final workspace = AgentWorkspaceController(
        source: Source()..initialFailure = true,
      );
      await workspace.submit('地址', [], []);
      expect(workspace.sheetExtent, AgentSheetExtent.medium);
      workspace.dispose();
    },
  );
}
