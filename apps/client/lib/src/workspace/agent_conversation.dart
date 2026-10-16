import 'package:flutter/material.dart';

import 'agent_workspace_controller.dart';
import 'agent_source_labels.dart';
import 'map_layers_api.dart';
import 'natural_content_height.dart';
import 'now_ui_tokens.dart';

class AgentConversation extends StatelessWidget {
  const AgentConversation({
    super.key,
    required this.workspace,
    required this.onSuggestion,
    this.onAction,
    required this.onRetry,
    this.resultBuilder,
    this.answerBuilder,
    this.onContentHeightChanged,
    this.attachmentBuilder,
    this.currentMessageAnchor,
    this.currentAreaAnchor,
    this.scrollController,
    this.onOpenMapAreaItem,
  });
  final AgentWorkspaceController workspace;
  final ValueChanged<String> onSuggestion;
  final ValueChanged<AgentAction>? onAction;
  final VoidCallback onRetry;
  final Widget Function(AgentResult result, bool current)? resultBuilder;

  /// A structured turn may supply its whole answer body. Null keeps prose and
  /// the existing result block; underlying conversation text is never changed.
  final Widget? Function(AgentResult result, bool current)? answerBuilder;
  final ValueChanged<double>? onContentHeightChanged;
  final Widget Function(int messageIndex)? attachmentBuilder;
  final GlobalKey? currentMessageAnchor;
  final GlobalKey? currentAreaAnchor;
  final ScrollController? scrollController;
  final ValueChanged<TypedMapItem>? onOpenMapAreaItem;

  @override
  Widget build(BuildContext context) {
    final tokens = NowUiTokens.reviewedOf(context);
    final replacements = <int, Widget>{};
    for (final reply in workspace.replies) {
      if (reply.messageIndex < 0 ||
          reply.messageIndex >= workspace.conversation.length ||
          workspace.conversation[reply.messageIndex].role != 'assistant') {
        continue;
      }
      final current =
          reply.messageIndex ==
              workspace.conversation.lastIndexWhere(
                (m) => m.role == 'assistant',
              ) &&
          identical(reply.result, workspace.result);
      final body = answerBuilder?.call(reply.result, current);
      if (body != null) replacements[reply.messageIndex] = body;
    }
    final children = <Widget>[
      for (final (index, message) in workspace.conversation.indexed) ...[
        Align(
          key: index == workspace.conversation.length - 1
              ? currentMessageAnchor
              : null,
          alignment: message.role == 'user'
              ? Alignment.centerRight
              : Alignment.centerLeft,
          child: message.role == 'user'
              ? Container(
                  constraints: BoxConstraints(
                    maxWidth: MediaQuery.sizeOf(context).width * 0.82,
                  ),
                  padding: const EdgeInsets.symmetric(
                    horizontal: 15,
                    vertical: 11,
                  ),
                  decoration: BoxDecoration(
                    color: Theme.of(context).colorScheme.surfaceContainerHigh,
                    borderRadius: BorderRadius.circular(19),
                  ),
                  child: _messageText(context, message, index),
                )
              : replacements[index] ?? _messageText(context, message, index),
        ),
        if (message.role == 'user' && attachmentBuilder != null)
          attachmentBuilder!(index),
        const SizedBox(height: 18),
        if (resultBuilder != null && !replacements.containsKey(index))
          for (final reply in workspace.replies.where(
            (r) => r.messageIndex == index,
          ))
            KeyedSubtree(
              key: ValueKey('reply-$index'),
              child: resultBuilder!(
                reply.result,
                index ==
                        workspace.conversation.lastIndexWhere(
                          (message) => message.role == 'assistant',
                        ) &&
                    identical(reply.result, workspace.result),
              ),
            ),
      ],
      if (resultBuilder != null &&
          workspace.result != null &&
          workspace.replies.isEmpty) ...[
        if (!workspace.conversation.any((m) => m.role == 'assistant'))
          _resultText(context, workspace.result!),
        resultBuilder!(workspace.result!, true),
      ],
      if (workspace.areaOperation case final operation?)
        _mapAreaContent(context, operation),
      if (workspace.state == AgentViewState.searching)
        Padding(
          padding: const EdgeInsets.symmetric(vertical: 12),
          child: Row(
            children: [
              const SizedBox.square(
                dimension: 16,
                child: CircularProgressIndicator(strokeWidth: 2),
              ),
              const SizedBox(width: 10),
              Expanded(child: Text('正在查找当前公开信息…', style: tokens.body)),
            ],
          ),
        ),
      // History and a retained map result are not a completed current turn.
      if (resultBuilder == null &&
          workspace.queryState == AgentQueryState.success) ...[
        const SizedBox(height: 8),
        Text(
          workspace.task?.contextType == 'ONLINE'
              ? '${workspace.result!.onlineIntents.length} 个公开线上意图 · 无地图点位'
              : _resultCount(workspace.result!),
          style: tokens.metadata,
        ),
        if (onAction != null && workspace.result!.actions.isNotEmpty) ...[
          const SizedBox(height: 14),
          Wrap(
            spacing: 8,
            children: [
              for (final action in workspace.result!.actions)
                ActionChip(
                  labelStyle: tokens.action,
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
                  labelStyle: tokens.action,
                  label: Text(followUp),
                  onPressed: () => onSuggestion(followUp),
                ),
            ],
          ),
        ],
      ],
    ];
    if (onContentHeightChanged != null && workspace.conversation.length <= 4) {
      return SingleChildScrollView(
        controller: scrollController,
        child: NaturalContentHeight(
          onHeight: onContentHeightChanged!,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              mainAxisSize: MainAxisSize.min,
              children: children,
            ),
          ),
        ),
      );
    }
    return ListView(
      controller: scrollController,
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 16),
      children: children,
    );
  }

  Widget _mapAreaContent(BuildContext context, AgentAreaOperation operation) {
    final scheme = Theme.of(context).colorScheme;
    final tokens = NowUiTokens.reviewedOf(context);
    return Padding(
      key: currentAreaAnchor,
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (operation.status == AgentAreaStatus.loading)
            Row(
              children: [
                const SizedBox.square(
                  dimension: 16,
                  child: CircularProgressIndicator(strokeWidth: 2),
                ),
                const SizedBox(width: 10),
                Expanded(child: Text(operation.summary, style: tokens.body)),
              ],
            )
          else
            Text(
              operation.summary,
              style: tokens.body.copyWith(
                color: operation.status == AgentAreaStatus.error
                    ? scheme.error
                    : tokens.body.color,
              ),
            ),
          if (operation.status != AgentAreaStatus.error &&
              operation.status != AgentAreaStatus.loading &&
              operation.current) ...[
            const SizedBox(height: 6),
            Text(operation.coverage, style: tokens.metadata),
          ],
          if (mapLayerScopeNotices(operation.items).isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 8, bottom: 4),
              child: Semantics(
                container: true,
                label: '位置与公开范围说明',
                child: Text(
                  mapLayerScopeNotices(operation.items).join('\n'),
                  style: tokens.metadata,
                ),
              ),
            ),
          for (final item in operation.items)
            ListTile(
              key: ValueKey('area-${item.mapID}'),
              contentPadding: EdgeInsets.zero,
              minTileHeight: 48,
              title: Text(item.title, style: tokens.placeTitle),
              subtitle: Text(item.rowSubtitle, style: tokens.listSubtitle),
              onTap: onOpenMapAreaItem == null
                  ? null
                  : () {
                      if (workspace.retainsMapAreaItem(item)) {
                        onOpenMapAreaItem!(item);
                      }
                    },
            ),
          if (operation.current &&
              operation.read?.content?.available == false &&
              operation.read?.content?.onRecover != null)
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton.icon(
                key: const Key('now-area-content-recovery'),
                style: TextButton.styleFrom(
                  minimumSize: const Size(48, 48),
                  textStyle: tokens.action,
                ),
                onPressed: () {
                  if (operation.current) {
                    operation.read!.content!.onRecover!();
                  }
                },
                icon: const Icon(Icons.refresh),
                label: Text(operation.read!.content!.recoveryLabel ?? '重试'),
              ),
            )
          else if ((operation.status == AgentAreaStatus.error ||
                  operation.status == AgentAreaStatus.partial ||
                  !operation.current) &&
              operation.status != AgentAreaStatus.loading &&
              operation.failure?.permitsUnchangedRetry != false)
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton.icon(
                key: const Key('now-area-retry'),
                style: TextButton.styleFrom(
                  minimumSize: const Size(48, 48),
                  textStyle: tokens.action,
                ),
                onPressed: workspace.retryMapArea,
                icon: const Icon(Icons.refresh),
                label: const Text('重试'),
              ),
            ),
        ],
      ),
    );
  }

  Widget _resultText(BuildContext context, AgentResult result) {
    final answer = result.resultSet?.answerSources;
    return AgentCitationText(
      text: result.responseMessage,
      sources: answer?.sources ?? const [],
      current: () =>
          workspace.retainsAnswerSources(result) && answer?.current == true,
      remaining: answer == null ? null : () => answer.remaining,
      changes: workspace,
      style: NowUiTokens.reviewedOf(context).body,
    );
  }

  Widget _messageText(BuildContext context, AgentMessage message, int index) {
    final style = message.role == 'user'
        ? NowUiTokens.reviewedOf(context).user
        : NowUiTokens.reviewedOf(context).body;
    if (message.role != 'assistant') return Text(message.text, style: style);
    final reply = workspace.replies
        .where((reply) => reply.messageIndex == index)
        .firstOrNull;
    final persisted = message.sourceReferences;
    final answer = reply?.result.resultSet?.answerSources;
    return AgentCitationText(
      text: message.text,
      sources: persisted?.sources ?? answer?.sources ?? const [],
      current: () =>
          reply != null &&
          (persisted != null
              ? workspace.retainsMessageSources(reply.result, persisted) &&
                    persisted.current
              : answer != null &&
                    workspace.retainsAnswerSources(reply.result) &&
                    answer.current),
      remaining: persisted != null
          ? () => persisted.remaining
          : answer != null
          ? () => answer.remaining
          : null,
      changes: workspace,
      style: style,
    );
  }

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
