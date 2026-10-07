import 'package:flutter/material.dart';

import 'agent_workspace_controller.dart';

class AgentConversation extends StatelessWidget {
  const AgentConversation({
    super.key,
    required this.workspace,
    required this.onSuggestion,
    this.onAction,
    required this.onRetry,
    this.resultBuilder,
  });
  final AgentWorkspaceController workspace;
  final ValueChanged<String> onSuggestion;
  final ValueChanged<AgentAction>? onAction;
  final VoidCallback onRetry;
  final Widget Function(AgentResult result, bool current)? resultBuilder;

  @override
  Widget build(BuildContext context) => ListView(
    padding: const EdgeInsets.fromLTRB(20, 8, 20, 24),
    children: [
      for (final (index, message) in workspace.conversation.indexed) ...[
        Align(
          alignment: message.role == 'user'
              ? Alignment.centerRight
              : Alignment.centerLeft,
          child: Container(
            constraints: BoxConstraints(
              maxWidth: MediaQuery.sizeOf(context).width * 0.82,
            ),
            padding: const EdgeInsets.symmetric(horizontal: 15, vertical: 11),
            decoration: BoxDecoration(
              color: message.role == 'user'
                  ? Theme.of(context).colorScheme.surfaceContainerHigh
                  : Theme.of(context).colorScheme.surfaceContainerLow,
              borderRadius: BorderRadius.circular(19),
            ),
            child: Text(
              message.text,
              style: TextStyle(
                height: 1.45,
                color: Theme.of(context).colorScheme.onSurface,
              ),
            ),
          ),
        ),
        const SizedBox(height: 18),
        if (resultBuilder != null)
          for (final reply in workspace.replies.where(
            (r) => r.messageIndex == index,
          ))
            KeyedSubtree(
              key: ValueKey('reply-$index'),
              child: resultBuilder!(
                reply.result,
                identical(reply.result, workspace.result),
              ),
            ),
      ],
      if (resultBuilder != null &&
          workspace.result != null &&
          workspace.replies.isEmpty) ...[
        if (!workspace.conversation.any((m) => m.role == 'assistant'))
          Text(
            workspace.result!.responseMessage,
            style: TextStyle(
              color: Theme.of(context).colorScheme.onSurface,
              height: 1.45,
            ),
          ),
        resultBuilder!(workspace.result!, true),
      ],
      if (workspace.state == AgentViewState.searching)
        const Padding(
          padding: EdgeInsets.symmetric(vertical: 12),
          child: Row(
            children: [
              SizedBox.square(
                dimension: 16,
                child: CircularProgressIndicator(strokeWidth: 2),
              ),
              SizedBox(width: 10),
              Expanded(child: Text('正在查找当前公开信息…')),
            ],
          ),
        ),
      // History and a retained map result are not a completed current turn.
      if (resultBuilder == null &&
          (workspace.queryState == AgentQueryState.success ||
              workspace.queryState == AgentQueryState.empty)) ...[
        const SizedBox(height: 8),
        Text(
          workspace.task?.contextType == 'ONLINE'
              ? '${workspace.result!.onlineIntents.length} 个公开线上意图 · 无地图点位'
              : _resultCount(workspace.result!),
          style: TextStyle(
            color: Theme.of(context).colorScheme.onSurfaceVariant,
            fontSize: 12,
            fontWeight: FontWeight.w600,
          ),
        ),
        if (onAction != null && workspace.result!.actions.isNotEmpty) ...[
          const SizedBox(height: 14),
          Wrap(
            spacing: 8,
            children: [
              for (final action in workspace.result!.actions)
                ActionChip(
                  label: Text(action.label),
                  onPressed: () => onAction!(action),
                ),
            ],
          ),
        ],
        if (workspace.result!.followUps.isNotEmpty) ...[
          const SizedBox(height: 14),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              for (final followUp in workspace.result!.followUps)
                ActionChip(
                  label: Text(followUp),
                  onPressed: () => onSuggestion(followUp),
                ),
            ],
          ),
        ],
      ],
    ],
  );

  String _resultCount(AgentResult result) {
    final items = result.projectionItems;
    if (items == null) {
      return '${result.activities.length} 个活动 · ${result.organizations.length} 个组织 · ${result.places.length} 个地点';
    }
    const labels = {
      'person': '成员',
      'activity': '活动',
      'place': '地点',
      'community': '社区',
      'organization': '组织',
      'business': '商家',
      'opportunity': '社交机会',
    };
    final counts = <String, int>{};
    for (final item in items) {
      final kind = item.entity.type;
      counts[kind] = (counts[kind] ?? 0) + 1;
    }
    final text = [
      for (final entry in labels.entries)
        if ((counts[entry.key] ?? 0) > 0)
          '${counts[entry.key]} 个${entry.value}',
      if ((counts['group'] ?? 0) > 0) '${counts['group']} 个兼容结果',
    ];
    return text.isEmpty ? '当前没有可见结果' : text.join(' · ');
  }
}
