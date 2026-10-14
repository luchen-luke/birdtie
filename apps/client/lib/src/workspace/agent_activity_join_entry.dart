import 'package:flutter/foundation.dart';
import 'agent_activity_join_api.dart';
import 'agent_memory_candidate_api.dart' show candidateIDValid;
import 'agent_reply_membership.dart';
import 'agent_result_projection.dart';
import 'agent_workspace_controller.dart';

/// A visible entry for the latest real reply. This is presentation context,
/// never a Task, an approval, a permission or an executable capability.
bool canReviewAgentActivityJoin(
  AgentWorkspaceController workspace,
  AgentResult source,
  AgentResultItem item, {
  String? ownerID,
}) {
  final task = workspace.task;
  if (task == null ||
      !candidateIDValid(task.id) ||
      !candidateIDValid(task.principalID) ||
      task.principalType.toUpperCase() != 'PERSON' ||
      task.actingUserID != task.principalID ||
      (ownerID != null && ownerID != task.principalID) ||
      task.status != 'COMPLETED' ||
      task.updatedAt == null ||
      task.contextType != 'CITY' ||
      task.cityID?.isNotEmpty != true ||
      !candidateIDValid(task.contextID) ||
      workspace.queryContextType != 'CITY' ||
      workspace.queryState != AgentQueryState.success ||
      workspace.requestError != null ||
      source.taskID != task.id ||
      !workspace.canUseReplyProjection(source) ||
      !item.valid ||
      item.entity.type != 'activity' ||
      !candidateIDValid(item.entity.id) ||
      source.projectionItems?.contains(item) != true ||
      !(task.intent == 'FIND_ACTIVITY' ||
          (task.intent == 'REFINE_RESULTS' &&
              task.filters['targetIntent'] == 'FIND_ACTIVITY')) ||
      !const {
        '',
        'FIND_ACTIVITY',
      }.contains(task.filters['targetIntent'] ?? '') ||
      task.messages.length < 2) {
    return false;
  }
  final index = task.messages.length - 1;
  if (!identical(workspace.result, source) &&
      !workspace.replies.any(
        (reply) =>
            identical(reply.result, source) && reply.messageIndex == index,
      )) {
    return false;
  }
  final last = task.messages[index], previous = task.messages[index - 1];
  final member = last.resultMembership;
  final query = task.filters['currentQuery'] ?? task.query;
  return last.role == 'assistant' &&
      previous.role == 'user' &&
      query.isNotEmpty &&
      previous.text == query &&
      member != null &&
      member.taskID == task.id &&
      member.cityID == task.cityID &&
      member.kind == 'activity' &&
      member.turnDigest ==
          agentReplyTurnDigest(
            task.messages.map((m) => (role: m.role, text: m.text)),
          ) &&
      member.refs.contains(item.entity);
}

/// Permanently retires a route's proposal on a new turn or a Task/context ABA.
/// Returning to an old visible result cannot revive this captured entry.
class AgentActivityJoinEntry extends ChangeNotifier {
  AgentActivityJoinEntry._(
    this.workspace,
    this.source,
    this.item,
    this.ownerID,
    this._task,
    this._epoch,
  ) {
    workspace.addListener(_changed);
  }
  final AgentWorkspaceController workspace;
  final AgentResult source;
  final AgentResultItem item;
  final String ownerID;
  final AgentTask _task;
  final int _epoch;
  bool _retired = false, _closed = false;

  static AgentActivityJoinEntry? capture(
    AgentWorkspaceController workspace,
    AgentResult source,
    AgentResultItem item,
    String ownerID,
  ) => canReviewAgentActivityJoin(workspace, source, item, ownerID: ownerID)
      ? AgentActivityJoinEntry._(
          workspace,
          source,
          item,
          ownerID,
          workspace.task!,
          workspace.taskIdentityEpoch,
        )
      : null;

  AgentActivityJoinSelection get selection => AgentActivityJoinSelection(
    taskID: _task.id,
    activityID: item.entity.id,
    expectedTaskUpdatedAt: _task.updatedAt!,
    replyTurnDigest: _task.messages.last.resultMembership!.turnDigest,
  );
  bool get current {
    if (!_closed &&
        !_retired &&
        (!identical(workspace.task, _task) ||
            workspace.taskIdentityEpoch != _epoch ||
            !canReviewAgentActivityJoin(
              workspace,
              source,
              item,
              ownerID: ownerID,
            ))) {
      _retired = true;
    }
    return !_closed && !_retired;
  }

  void _changed() {
    current;
    if (!_closed) notifyListeners();
  }

  @override
  void dispose() {
    _closed = true;
    workspace.removeListener(_changed);
    super.dispose();
  }
}
