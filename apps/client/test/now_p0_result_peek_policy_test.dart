import 'dart:async';

import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/now_workspace_layout.dart';
import 'package:flutter_test/flutter_test.dart';

// Controlled response fixtures validate presentation policy, not a native
// Guest request or the final device's visible map area.
class _PendingSource extends AgentTaskSource {
  final requests = <Completer<AgentResult>>[];
  final continued = <AgentTask>[];

  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) {
    final request = Completer<AgentResult>();
    requests.add(request);
    return request.future;
  }

  @override
  Future<AgentResult> followUp(
    AgentTask task,
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) {
    continued.add(task);
    return resolve(query, activities, places);
  }
}

const _empty = AgentResult(
  entities: [],
  activities: [],
  places: [],
  note: '本次未找到符合条件的结果。',
);
const _short = AgentResult(
  entities: [
    MapEntity(
      id: 'place:one',
      kind: MapEntityKind.place,
      title: '公开地点',
      subtitle: '',
      latitude: 57.1,
      longitude: -2.1,
    ),
  ],
  activities: [],
  places: [],
  note: '找到 1 个公开地点。',
);

void main() {
  for (final result in [_empty, _short]) {
    test(
      '新文字任务默认peek，${identical(result, _empty) ? '空结果' : '短成功'}不扩大终态',
      () async {
        final source = _PendingSource();
        final w = AgentWorkspaceController(source: source);
        final request = w.submit('原始 Guest 查询', [], [], cityID: 'aberdeen');
        expect(w.queryState, AgentQueryState.loading);
        expect(w.sheetExtent, AgentSheetExtent.peek);
        source.requests.single.complete(result);
        await request;
        expect(w.state, AgentViewState.results);
        expect(w.sheetExtent, AgentSheetExtent.peek);
        expect(w.result, same(result));
        expect(w.conversation.last.text, result.responseMessage);
        final layout = NowWorkspaceLayout.resolve(
          rootHeight: 800,
          safeTop: 24,
          safeBottom: 24,
          keyboard: 0,
          chromeBottom: 80,
          composerHeight: 72,
          hasContent: true,
          expanded: w.sheetExtent == AgentSheetExtent.expanded,
          editing: w.inputFocused,
        );
        expect(layout.surfaceHeight, lessThanOrEqualTo((800 - 24 - 24) * .4));
        w.dispose();
      },
    );
  }

  test('长回答内容与领域动作保留，默认peek并由用户主动展开', () async {
    final source = _PendingSource();
    final workspace = AgentWorkspaceController(source: source);
    final long = AgentResult(
      entities: const [],
      activities: const [],
      places: const [],
      note: List.filled(200, '完整长回答与逐段原始依据').join('\n'),
      actions: const [AgentAction(type: 'REVIEW_ACTIVITY', label: '检查活动草稿')],
    );
    final request = workspace.submit('检查完整活动方案', [], []);
    source.requests.single.complete(long);
    await request;
    expect(workspace.sheetExtent, AgentSheetExtent.peek);
    expect(workspace.conversation.last.text, long.responseMessage);
    expect(workspace.result!.actions, same(long.actions));
    workspace.setSheetExtent(AgentSheetExtent.expanded);
    expect(workspace.sheetExtent, AgentSheetExtent.expanded);
    expect(workspace.result, same(long));
    workspace.dispose();
  });

  for (final extent in [AgentSheetExtent.expanded, AgentSheetExtent.peek]) {
    for (final failed in [false, true]) {
      test(
        '请求期间用户选择$extent，${failed ? '失败' : '成功'}不覆盖presentationRevision',
        () async {
          final source = _PendingSource();
          final w = AgentWorkspaceController(source: source);
          final request = w.submit('待返回查询', [], []);
          w.beginTyping();
          expect(w.sheetExtent, AgentSheetExtent.expanded);
          w.setSheetExtent(extent);
          if (failed) {
            source.requests.single.completeError(
              const AgentRequestFailure('直接相关失败夹具'),
            );
          } else {
            source.requests.single.complete(_empty);
          }
          await request;
          expect(w.sheetExtent, extent);
          w.stopTyping();
          expect(
            w.sheetExtent,
            extent,
            reason: '显式视图 revision 已变，旧编辑快照不得覆盖该选择',
          );
          expect(w.inputFocused, isFalse);
          w.dispose();
        },
      );
    }
  }

  test('Explore输入仍focused时提交，新Task终态及随后unfocus保持peek', () async {
    final source = _PendingSource();
    final w = AgentWorkspaceController(source: source);
    w.beginTyping();
    expect(w.inputFocused, isTrue);
    final request = w.submit('输入中首次提交', [], []);
    expect(w.sheetExtent, AgentSheetExtent.peek);
    source.requests.single.complete(_empty);
    await request;
    expect(w.inputFocused, isTrue);
    expect(w.sheetExtent, AgentSheetExtent.peek);
    w.stopTyping();
    expect(w.sheetExtent, AgentSheetExtent.peek);
    expect(
      w.contentMode,
      AgentContentMode.conversation,
      reason: '提交改变task epoch，不能恢复提交前Explore的旧内容模式',
    );
    w.dispose();
  });

  test('新请求中临时键盘expanded，结果不改输入态，unfocus恢复peek', () async {
    final source = _PendingSource();
    final w = AgentWorkspaceController(source: source);
    final request = w.submit('正在等待', [], []);
    w.beginTyping();
    expect(w.sheetExtent, AgentSheetExtent.expanded);
    source.requests.single.complete(_empty);
    await request;
    expect(w.inputFocused, isTrue);
    expect(w.sheetExtent, AgentSheetExtent.expanded);
    w.stopTyping();
    expect(w.sheetExtent, AgentSheetExtent.peek);
    w.dispose();
  });

  for (final original in [AgentSheetExtent.peek, AgentSheetExtent.expanded]) {
    test('continuation继承输入前$original，仍focused提交后unfocus不改原选择', () async {
      final source = _PendingSource();
      final w = AgentWorkspaceController(source: source);
      final first = w.submit('原任务', [], [], cityID: 'aberdeen');
      source.requests.single.complete(_short);
      await first;
      final taskID = w.task!.id;
      w.setSheetExtent(original);
      w.beginTyping();
      expect(w.sheetExtent, AgentSheetExtent.expanded);
      final next = w.submit('同任务继续', [], [], cityID: 'aberdeen');
      expect(w.sheetExtent, original);
      expect(w.inputFocused, isTrue);
      expect(source.continued.single.id, taskID);
      // Exercise unfocus before completion as well as after it.
      w.stopTyping();
      expect(w.sheetExtent, original);
      source.requests.last.complete(_empty);
      await next;
      expect(w.sheetExtent, original);
      expect(w.task!.id, taskID);
      expect(w.recent, hasLength(1));
      expect(w.conversation.map((message) => message.role), [
        'user',
        'assistant',
        'user',
        'assistant',
      ]);
      w.dispose();
    });
  }

  test('旧任务恢复历史expanded，后续continued空结果仍expanded', () async {
    final source = _PendingSource();
    final w = AgentWorkspaceController(source: source);
    const task = AgentTask(
      id: 'original-history',
      query: '旧查询',
      status: 'COMPLETED',
      messages: [AgentMessage(role: 'assistant', text: '旧回答')],
    );
    final restore = w.reopen(task, [], []);
    source.requests.single.complete(_short);
    await restore;
    expect(w.sheetExtent, AgentSheetExtent.expanded);
    expect(w.contentMode, AgentContentMode.conversation);
    final continuation = w.submit('继续旧对话', [], []);
    source.requests.last.complete(_empty);
    await continuation;
    expect(w.sheetExtent, AgentSheetExtent.expanded);
    expect(w.task!.id, task.id);
    w.dispose();
  });

  for (final failed in [false, true]) {
    for (final typing in [false, true]) {
      test(
        'reopen pending主动peek保持，${failed ? '失败' : '成功'}，typing=$typing',
        () async {
          final source = _PendingSource();
          final w = AgentWorkspaceController(source: source);
          const task = AgentTask(
            id: 'original-restore',
            query: '旧查询',
            status: 'COMPLETED',
            messages: [AgentMessage(role: 'assistant', text: '原历史回答')],
          );
          final request = w.reopen(task, [], []);
          if (typing) w.beginTyping();
          w.setSheetExtent(AgentSheetExtent.peek);
          final mode = w.contentMode;
          if (failed) {
            source.requests.single.completeError(
              const AgentRequestFailure('恢复只读失败夹具'),
            );
          } else {
            source.requests.single.complete(_short);
          }
          await request;
          expect(w.sheetExtent, AgentSheetExtent.peek);
          expect(w.contentMode, mode);
          expect(w.task!.id, task.id);
          expect(w.result, failed ? isNull : same(_short));
          if (typing) {
            w.stopTyping();
            expect(
              w.sheetExtent,
              AgentSheetExtent.peek,
              reason: '用户 revision 已更新，旧输入快照不覆盖主动收起',
            );
            expect(w.contentMode, mode);
          }
          w.dispose();
        },
      );
    }

    test(
      'reopen pending只临时输入，${failed ? '失败medium' : '成功expanded'}恢复正确默认',
      () async {
        final source = _PendingSource();
        final w = AgentWorkspaceController(source: source);
        const task = AgentTask(
          id: 'typing-restore',
          query: '旧查询',
          status: 'COMPLETED',
          messages: [AgentMessage(role: 'assistant', text: '原历史回答')],
        );
        final request = w.reopen(task, [], []);
        w.beginTyping();
        expect(w.sheetExtent, AgentSheetExtent.expanded);
        if (failed) {
          source.requests.single.completeError(
            const AgentRequestFailure('恢复只读失败夹具'),
          );
        } else {
          source.requests.single.complete(_short);
        }
        await request;
        expect(w.inputFocused, isTrue);
        expect(
          w.sheetExtent,
          AgentSheetExtent.expanded,
          reason: '只读响应不缩小临时输入空间',
        );
        w.stopTyping();
        expect(
          w.sheetExtent,
          failed ? AgentSheetExtent.medium : AgentSheetExtent.expanded,
        );
        expect(
          w.contentMode,
          failed ? AgentContentMode.results : AgentContentMode.conversation,
        );
        w.dispose();
      },
    );

    test(
      'reopen pending主动content mode保留，${failed ? '失败' : '成功'}不覆盖选择',
      () async {
        final source = _PendingSource();
        final w = AgentWorkspaceController(source: source);
        const task = AgentTask(
          id: 'mode-restore',
          query: '旧查询',
          status: 'COMPLETED',
          messages: [AgentMessage(role: 'assistant', text: '原历史回答')],
        );
        final request = w.reopen(task, [], []);
        w.showContent(AgentContentMode.results);
        final extent = w.sheetExtent;
        if (failed) {
          source.requests.single.completeError(
            const AgentRequestFailure('恢复只读失败夹具'),
          );
        } else {
          source.requests.single.complete(_short);
        }
        await request;
        expect(w.contentMode, AgentContentMode.results);
        expect(w.sheetExtent, extent);
        w.dispose();
      },
    );
  }

  test('新Task失败保持peek，重试成功仍peek并保留原查询', () async {
    final source = _PendingSource();
    final w = AgentWorkspaceController(source: source);
    final first = w.submit('失败后保留的原查询', [], []);
    source.requests.single.completeError(const AgentRequestFailure('可重试的夹具失败'));
    await first;
    expect(w.queryState, AgentQueryState.error);
    expect(w.sheetExtent, AgentSheetExtent.peek);
    expect(w.conversation.single.text, '失败后保留的原查询');
    final retry = w.retry();
    source.requests.last.complete(_empty);
    await retry;
    expect(w.sheetExtent, AgentSheetExtent.peek);
    expect(w.task!.query, '失败后保留的原查询');
    expect(w.conversation.last.text, _empty.responseMessage);
    w.dispose();
  });
}
