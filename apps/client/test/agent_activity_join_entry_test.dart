import 'package:birdtie_client/src/workspace/agent_activity_join_entry.dart';
import 'package:birdtie_client/src/workspace/agent_entity_result_card.dart';
import 'package:birdtie_client/src/workspace/agent_reply_membership.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

const owner = '946c8a94-3125-4d2b-a3be-02d1b2cb4c10';
const taskID = 'a7ee707b-afc8-4d96-a259-164989f33dda';
const activityID = '662e707b-afc8-4d96-a259-164989f33dda';
const contextID = '4812975b-9130-40c8-93ad-2b0543cf9231';
const activityRef = AgentResultRef(type: 'activity', id: activityID);
const item = AgentResultItem(
  entity: activityRef,
  title: '合成公开活动，检查后才可以报名',
  summary: '本机入口夹具，不代表已批准或已报名',
  scope: 'AUTHORIZED_VIEW',
  detail: activityRef,
);
AgentTask originalTask({
  String status = 'COMPLETED',
  String principalType = 'PERSON',
  String context = 'CITY',
  bool member = true,
  bool target = true,
  bool validDigest = true,
  bool unfinished = false,
  String intent = 'FIND_ACTIVITY',
  bool hasVersion = true,
  String cityContextID = contextID,
}) {
  final digest = agentReplyTurnDigest([
    (role: 'user', text: '找活动'),
    (role: 'assistant', text: '找到一个公开活动。'),
  ]);
  final membership = member
      ? AgentReplyMembership.read({
          'schema': agentReplyMembershipSchema,
          'taskId': taskID,
          'cityId': 'aberdeen-gb',
          'kind': 'activity',
          'turnDigest': validDigest ? digest : 'a' * 64,
          'resultSetId':
              'reply:$taskID:${(validDigest ? digest : 'a' * 64).substring(0, 24)}',
          'refs': target
              ? [
                  {'type': 'activity', 'id': activityID},
                ]
              : [],
        })
      : null;
  return AgentTask(
    id: taskID,
    query: '找活动',
    status: status,
    intent: intent,
    principalType: principalType,
    principalID: owner,
    actingUserID: owner,
    cityID: 'aberdeen-gb',
    contextType: context,
    contextID: cityContextID,
    updatedAt: hasVersion
        ? DateTime.utc(2026, 10, 13, 4, 0, 0, 123, 456)
        : null,
    messages: [
      const AgentMessage(role: 'user', text: '找活动'),
      AgentMessage(
        role: 'assistant',
        text: '找到一个公开活动。',
        resultMembership: membership,
      ),
      if (unfinished) const AgentMessage(role: 'user', text: '改成明天'),
    ],
  );
}

AgentResult sourceFor(AgentTask task) => AgentResult(
  entities: const [],
  activities: const [],
  places: const [],
  note: '',
  taskID: task.id,
  task: task,
  resultSet: AgentResultSet(
    id: '$taskID:20261013',
    status: 'ready',
    entities: const [activityRef],
    items: const [item],
    generatedAt: DateTime.utc(2026, 10, 13),
  ),
);
AgentWorkspaceController workspaceFor(AgentTask task) {
  final source = sourceFor(task);
  return AgentWorkspaceController()
    ..task = task
    ..result = source
    ..state = AgentViewState.results
    ..conversation.addAll(task.messages);
}

void main() {
  test('当前真实UUID任务末轮活动引用可检查，原结果ID无需冒充history ID', () {
    final w = workspaceFor(originalTask());
    addTearDown(w.dispose);
    expect(
      canReviewAgentActivityJoin(w, w.result!, item, ownerID: owner),
      true,
    );
    final entry = AgentActivityJoinEntry.capture(w, w.result!, item, owner)!;
    addTearDown(entry.dispose);
    expect(entry.selection.taskID, taskID);
    expect(entry.selection.activityID, activityID);
    expect(entry.current, true);
  });
  test('ACTIVE、组织、ONLINE、无来源、伪轮摘要、非本轮目标和未完成新轮均不提供批准入口', () {
    for (final task in [
      originalTask(status: 'ACTIVE'),
      originalTask(principalType: 'ORGANIZATION'),
      originalTask(context: 'ONLINE'),
      originalTask(member: false),
      originalTask(validDigest: false),
      originalTask(target: false),
      originalTask(unfinished: true),
      originalTask(intent: 'AREA_DISCOVERY'),
      originalTask(intent: 'COMPARE_RESULTS'),
      originalTask(intent: 'REFINE_RESULTS'),
      originalTask(hasVersion: false),
      originalTask(cityContextID: 'aberdeen-gb'),
    ]) {
      final w = workspaceFor(task);
      expect(
        canReviewAgentActivityJoin(w, w.result!, item, ownerID: owner),
        false,
      );
      expect(AgentActivityJoinEntry.capture(w, w.result!, item, owner), isNull);
      w.dispose();
    }
  });
  test('任意旧source或另一owner不能借当前task显示提案', () {
    final w = workspaceFor(originalTask());
    addTearDown(w.dispose);
    final old = sourceFor(w.task!);
    expect(canReviewAgentActivityJoin(w, old, item, ownerID: owner), false);
    expect(
      canReviewAgentActivityJoin(w, w.result!, item, ownerID: activityID),
      false,
    );
  });
  test('来源新轮与返回旧task的ABA永久退休已打开入口', () {
    final w = workspaceFor(originalTask());
    addTearDown(w.dispose);
    final original = w.task!;
    final entry = AgentActivityJoinEntry.capture(w, w.result!, item, owner)!;
    addTearDown(entry.dispose);
    w.task = originalTask(unfinished: true);
    w.notifyListeners();
    expect(entry.current, false);
    w.task = original;
    w.notifyListeners();
    expect(entry.current, false);
  });
  testWidgets('正常窄屏Now当前结果提供明确检查入口，点击只给原source和ID不报名', (t) async {
    final w = workspaceFor(originalTask());
    addTearDown(w.dispose);
    w.sheetExtent = AgentSheetExtent.expanded;
    AgentResult? capturedSource;
    AgentResultItem? capturedItem;
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            width: 320,
            height: 650,
            child: AgentResultsSheet(
              workspace: w,
              onSuggestion: (_) {},
              onRetry: () {},
              onReviewAgentJoin: (source, target) {
                capturedSource = source;
                capturedItem = target;
              },
            ),
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    final button = find.text('检查智能体报名提案');
    await t.ensureVisible(button);
    await t.pumpAndSettle();
    final control = find.widgetWithText(TextButton, '检查智能体报名提案');
    expect(t.getSize(control).height, greaterThanOrEqualTo(48));
    await t.tap(button);
    expect(capturedSource, same(w.result));
    expect(capturedItem, same(item));
    expect(t.takeException(), isNull);
  });
  testWidgets('其他实体即使误传callback也没有智能体报名按钮', (t) async {
    const place = AgentResultItem(
      entity: AgentResultRef(type: 'place', id: activityID),
      title: '地点',
      summary: '',
      scope: 'AUTHORIZED_VIEW',
    );
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentEntityResultCard(
            item: place,
            onReviewAgentJoin: () => fail('非活动触发报名'),
          ),
        ),
      ),
    );
    expect(find.text('检查智能体报名提案'), findsNothing);
  });
}
