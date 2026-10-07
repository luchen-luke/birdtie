import 'package:birdtie_client/src/workspace/active_intent.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('active intent uses structured task fields and stays compact', () {
    const task = AgentTask(
      id: 'task-1',
      query: '帮我找一个周末可以参加的羽毛球活动，最好在一个离学校近的地方',
      status: 'COMPLETED',
      intent: 'FIND_ACTIVITY',
      filters: {'category': 'badminton', 'timePreference': 'weekend'},
    );
    final summary = ActiveIntentSummary.fromTask(task);
    expect(summary.action, '羽毛球活动');
    expect(summary.category, '羽毛球');
    expect(summary.time, '周末');
    expect(summary.label, '羽毛球活动 · 周末');
    expect(summary.label, isNot(contains('学校')));
  });

  test('unsupported task never renders the original sentence as heading', () {
    const task = AgentTask(
      id: 'task-2',
      query: '请给我制定一套完整的旅行计划并自动联系所有人',
      status: 'FAILED',
      intent: 'UNSUPPORTED',
    );
    expect(ActiveIntentSummary.fromTask(task).label, '当前任务');
  });
}
