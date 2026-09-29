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
}
