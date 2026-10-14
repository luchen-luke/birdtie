import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart'
    show RenderAbstractViewport, ScrollDirection;

import '../city/public_city_controller.dart';
import 'agent_conversation.dart';
import 'agent_workspace_controller.dart';
import 'active_intent.dart';
import 'activity_plans.dart';
import 'saved_items.dart';
import 'entity_action_contract.dart';
import 'entity_action_dispatcher.dart';
import 'sponsored_opportunities.dart';
import 'agent_result_projection.dart';
import 'external_place_candidates.dart';
import 'agent_entity_result_card.dart';
import 'agent_activity_join_entry.dart';
import 'agent_answer_sources.dart';
import 'map_layers_api.dart';
import 'place_fact_presentation.dart';
import 'place_answer_card.dart';
import 'now_ui_tokens.dart';
import 'natural_content_height.dart';

String _activityStatusLabel(String status) => switch (status) {
  'upcoming' => '即将开始',
  'ongoing' => '进行中',
  'completed' => '已结束',
  'cancelled' => '已取消',
  _ => status,
};

String _typedResultCount(AgentResult result) {
  if (!result.replyProjectionCurrent) return '结果需要重新读取';
  const labels = {
    'person': '位伙伴',
    'activity': '个活动',
    'place': '个地点',
    'community': '个社群',
    'organization': '个组织',
    'business': '个商家',
    'opportunity': '个本人机会',
    'group': '个待核实来源',
  };
  final items = result.projectionItems!;
  if (items.isEmpty) return '暂无符合条件的结果';
  return labels.entries
      .map((entry) {
        final count = items
            .where((item) => item.entity.type == entry.key)
            .length;
        return count == 0 ? '' : '$count ${entry.value}';
      })
      .where((label) => label.isNotEmpty)
      .join(' · ');
}

bool _queryUnfinished(AgentWorkspaceController workspace) {
  final task = workspace.task;
  final resultTaskID = workspace.result?.taskID;
  return workspace.queryState == AgentQueryState.empty &&
      task?.status == 'ACTIVE' &&
      task!.messages.isNotEmpty &&
      task.messages.last.role == 'user' &&
      (resultTaskID == null || resultTaskID == task.id);
}

String _queryCaption(AgentWorkspaceController workspace) {
  final search = workspace.result?.onlinePlaceSearch;
  if (workspace.state != AgentViewState.searching &&
      workspace.requestError == null &&
      search != null) {
    if (!search.current) return '联网结果已失效 · 请重新搜索';
    if (search.status == 'UNAVAILABLE') return '联网查询暂不可用 · 可重试';
    if (search.status == 'EMPTY') return '本次联网查询暂无结果';
    if (search.places.isNotEmpty) {
      return '${search.places.length} 家联网餐厅 · ${search.stale ? '资料需更新' : '信息待核实'}';
    }
  }
  switch (workspace.queryState) {
    case AgentQueryState.loading:
      return '正在查找符合条件的内容…';
    case AgentQueryState.needsScope:
      return '尚未搜索 · 需要选择城市';
    case AgentQueryState.error:
      return '查询未完成';
    case AgentQueryState.idle:
      return '尚未搜索';
    case AgentQueryState.empty:
      return _queryUnfinished(workspace) ? '查询尚未完成 · 暂无结果' : '未找到符合条件的公开内容';
    case AgentQueryState.unsupported:
      return '当前请求暂不可处理';
    case AgentQueryState.success:
      break;
  }
  final result = workspace.result!;
  if (result.hasSourcedAnswer && result.entities.isEmpty) {
    return '已找到回答 · 可查看来源';
  }
  if (workspace.task?.intent == 'PERSONAL_RELATIONSHIP_CONTEXT') {
    return '本人授权 · 最近 30 天的互动记录';
  }
  if (workspace.task?.intent == 'FIND_NEW_PEOPLE') {
    return '双方选择参与 · 发送前由你确认';
  }
  if (workspace.task?.contextType == 'ONLINE') {
    return '线上 · ${result.onlineIntents.length} 个公开意图 · 无地图点位';
  }
  if (result.projectionItems != null) return _typedResultCount(result);
  return '${result.activities.length} 个活动 · ${result.organizations.length} 个组织 · ${result.places.length} 个地点';
}

class AgentResultsSheet extends StatelessWidget {
  const AgentResultsSheet({
    super.key,
    required this.workspace,
    this.saved,
    this.plans,
    this.onContact,
    this.onOpenActivity,
    this.onOpenPlace,
    this.onOpenOnlineIntent,
    this.onOpenEntity,
    this.onReviewAgentJoin,
    this.onOpenReplyEntity,
    this.onFocusReplyPlace,
    this.onNavigateReplyPlace,
    this.onShareReplyEntity,
    this.onSaveReplyEntity,
    this.onShareEntity,
    this.onSaveEntity,
    this.onActivityImpression,
    this.onAction,
    this.onSaveSocialIntent,
    required this.onSuggestion,
    required this.onRetry,
    this.onChooseCity,
    this.availableHeight,
    this.publicEvidenceCurrent,
    this.publicEvidenceChanges,
    this.attachmentBuilder,
    this.embedded = false,
    this.onOpenMapAreaItem,
    this.selectionPreview,
    this.onContentHeightChanged,
  });
  final AgentWorkspaceController workspace;
  final SavedController? saved;
  final ActivityPlansController? plans;
  final ValueChanged<AgentPerson>? onContact;
  final ValueChanged<PublicActivity>? onOpenActivity;
  final ValueChanged<String>? onOpenPlace;
  final ValueChanged<String>? onOpenOnlineIntent;
  final ValueChanged<AgentResultItem>? onOpenEntity;
  final void Function(AgentResult, AgentResultItem)? onReviewAgentJoin;
  final void Function(AgentResult, AgentResultItem)? onOpenReplyEntity;
  final void Function(AgentResult, AgentResultItem)? onFocusReplyPlace;
  final void Function(AgentResult, AgentResultItem)? onNavigateReplyPlace;
  final void Function(AgentResult, AgentResultItem)? onShareReplyEntity;
  final void Function(AgentResult, AgentResultItem)? onSaveReplyEntity;
  final ValueChanged<AgentResultItem>? onShareEntity;
  final ValueChanged<AgentResultItem>? onSaveEntity;
  final ValueChanged<PublicActivity>? onActivityImpression;
  final ValueChanged<AgentAction>? onAction;
  final ValueChanged<AgentTask>? onSaveSocialIntent;
  final ValueChanged<String> onSuggestion;
  final VoidCallback onRetry;
  final VoidCallback? onChooseCity;
  final double? availableHeight;
  final bool Function(AgentResultItem)? publicEvidenceCurrent;
  final Listenable? publicEvidenceChanges;
  final Widget Function(int messageIndex)? attachmentBuilder;

  /// The Now layout owns height and the sole external surface in this mode.
  final bool embedded;
  final ValueChanged<TypedMapItem>? onOpenMapAreaItem;

  /// The original selected-map-object card shares this content region.
  final Widget? selectionPreview;

  /// Actual short embedded content height, including its existing handle and
  /// header, excluding the outer Composer. Never an estimated scroll extent.
  final ValueChanged<double>? onContentHeightChanged;

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: Listenable.merge([workspace, ?saved, ?plans]),
    builder: (context, _) {
      final tokens = NowUiTokens.reviewedOf(context);
      if (workspace.task == null && workspace.areaOperation == null) {
        return const SizedBox.shrink();
      }
      final extent = workspace.sheetExtent;
      final panelName = workspace.contentMode == AgentContentMode.conversation
          ? '对话面板'
          : '结果面板';
      return LayoutBuilder(
        builder: (context, constraints) {
          final requested =
              availableHeight ?? MediaQuery.sizeOf(context).height;
          final available = requested.clamp(0.0, constraints.maxHeight);
          final medium = (requested * 0.43)
              .clamp(250.0, 420.0)
              .clamp(0.0, available);
          final expanded = available;
          final label = workspace.task == null
              ? '地图区域搜索'
              : ActiveIntentSummary.fromTask(workspace.task!).label;
          final caption = _queryCaption(workspace);
          final needsScope = workspace.queryState == AgentQueryState.needsScope;
          final failed = workspace.queryState == AgentQueryState.error;
          final unsupported =
              workspace.queryState == AgentQueryState.unsupported;
          final peekExplanation = unsupported && extent == AgentSheetExtent.peek
              ? workspace.result!.responseMessage
              : null;
          final hasAnswer =
              workspace.queryState == AgentQueryState.success ||
              workspace.queryState == AgentQueryState.empty ||
              unsupported;
          final canRetry = failed && workspace.permitsUnchangedRetry;
          final recoveryMessage = failed
              ? workspace.requestFailure?.recoveryMessage
              : null;
          double textHeight(String text, TextStyle style, {int? maxLines}) {
            final painter = TextPainter(
              text: TextSpan(text: text, style: style),
              textDirection: Directionality.of(context),
              textScaler: MediaQuery.textScalerOf(context),
              maxLines: maxLines,
            );
            painter.layout(
              maxWidth: (constraints.maxWidth - 40).clamp(1, double.infinity),
            );
            return painter.height;
          }

          const modeLabel = '查看地图';
          final buttonStyle = tokens.action;
          const toolbarHeight = 48.0;
          const peekAction = '展开对话';
          final peekActionHeight = (textHeight(peekAction, buttonStyle) + 8)
              .clamp(48.0, double.infinity);
          // Peek is its own readable summary. Its controls cannot be pushed
          // below the composer by a clipped copy of the expanded header.
          final peekHeight =
              (toolbarHeight +
                      9 +
                      textHeight(label, tokens.sheetTitle) +
                      4 +
                      textHeight(caption, tokens.metadata) +
                      (peekExplanation == null
                          ? 0
                          : 8 + textHeight(peekExplanation, tokens.body)) +
                      (workspace.requestError == null
                          ? 0
                          : 8 +
                                textHeight(
                                  workspace.requestError!,
                                  tokens.body,
                                )) +
                      (recoveryMessage == null
                          ? 0
                          : 8 + textHeight(recoveryMessage, tokens.body)) +
                      (needsScope || canRetry ? 48 : 0) +
                      (hasAnswer ? peekActionHeight : 0))
                  .clamp(104.0, double.infinity);
          final height = switch (extent) {
            AgentSheetExtent.hidden => 0.0,
            AgentSheetExtent.peek => peekHeight.clamp(0.0, available),
            AgentSheetExtent.medium => medium,
            AgentSheetExtent.expanded => expanded,
          };
          var dragDelta = 0.0;
          final header = Padding(
            padding: const EdgeInsets.fromLTRB(20, 0, 20, 9),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                ?selectionPreview,
                if (!embedded) ...[
                  Text(label, style: tokens.sheetTitle),
                  const SizedBox(height: 4),
                  Text(caption, style: tokens.metadata),
                ],
                if (!embedded && peekExplanation != null) ...[
                  const SizedBox(height: 8),
                  Text(peekExplanation, style: tokens.body),
                ],
                if (workspace.areaOperation == null &&
                    workspace.requestError != null) ...[
                  const SizedBox(height: 8),
                  Text(
                    workspace.requestError!,
                    style: tokens.body.copyWith(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                  if (recoveryMessage != null) ...[
                    const SizedBox(height: 8),
                    Text(recoveryMessage, style: tokens.body),
                  ],
                  if (needsScope)
                    TextButton.icon(
                      key: const Key('agent-sheet-choose-city'),
                      style: TextButton.styleFrom(
                        minimumSize: const Size(48, 48),
                        textStyle: tokens.action,
                      ),
                      onPressed: onChooseCity,
                      icon: const Icon(Icons.location_city_outlined),
                      label: const Text('选择城市继续'),
                    )
                  else if (canRetry)
                    TextButton.icon(
                      key: const Key('agent-sheet-retry'),
                      style: TextButton.styleFrom(
                        minimumSize: const Size(48, 48),
                        textStyle: tokens.action,
                      ),
                      onPressed: onRetry,
                      icon: const Icon(Icons.refresh),
                      label: const Text('重试'),
                    ),
                ],
                if (!embedded && hasAnswer && extent == AgentSheetExtent.peek)
                  TextButton.icon(
                    key: const Key('agent-sheet-expand-summary'),
                    style: TextButton.styleFrom(
                      minimumSize: const Size(0, 48),
                      textStyle: tokens.action,
                      padding: const EdgeInsets.symmetric(horizontal: 8),
                    ),
                    onPressed: () =>
                        workspace.setSheetExtent(AgentSheetExtent.medium),
                    icon: const Icon(Icons.expand_less),
                    label: Text(peekAction),
                  ),
              ],
            ),
          );
          final measurement =
              embedded &&
                  workspace.conversation.length <= 4 &&
                  onContentHeightChanged != null
              ? _SheetNaturalHeights(onContentHeightChanged!)
              : null;
          return AnimatedContainer(
            duration: MediaQuery.disableAnimationsOf(context)
                ? Duration.zero
                : const Duration(milliseconds: 260),
            curve: Curves.easeOutCubic,
            height: embedded ? null : height,
            decoration: embedded
                ? null
                : BoxDecoration(
                    color: Theme.of(context).colorScheme.surface,
                    borderRadius: const BorderRadius.vertical(
                      top: Radius.circular(26),
                    ),
                    boxShadow: [
                      BoxShadow(
                        color: Theme.of(
                          context,
                        ).colorScheme.shadow.withValues(alpha: 0.12),
                        blurRadius: 18,
                        offset: const Offset(0, -3),
                      ),
                    ],
                  ),
            child: LayoutBuilder(
              builder: (context, animatedConstraints) {
                // No invisible controls while a hidden/zero-space sheet animates.
                // The composer and keyboard dismiss path remain outside the sheet.
                if ((!embedded && extent == AgentSheetExtent.hidden) ||
                    animatedConstraints.maxHeight < 48) {
                  return const SizedBox.shrink();
                }
                return Column(
                  children: [
                    SizedBox(
                      height: toolbarHeight.clamp(
                        48.0,
                        animatedConstraints.maxHeight,
                      ),
                      child: Stack(
                        children: [
                          Positioned.fill(
                            child: Semantics(
                              label: switch (extent) {
                                AgentSheetExtent.hidden => '$panelName已隐藏',
                                AgentSheetExtent.peek => '$panelName已收起',
                                AgentSheetExtent.medium => '$panelName处于中间高度',
                                AgentSheetExtent.expanded => '$panelName已展开',
                              },
                              hint: '点击切换面板高度，上滑展开，下滑收起',
                              button: true,
                              child: GestureDetector(
                                key: const Key('agent-sheet-handle'),
                                behavior: HitTestBehavior.opaque,
                                onVerticalDragStart: (_) => dragDelta = 0,
                                onVerticalDragUpdate: (d) =>
                                    dragDelta += d.primaryDelta ?? 0,
                                onVerticalDragEnd: (d) {
                                  final velocity = d.primaryVelocity ?? 0;
                                  final up = velocity.abs() > 50
                                      ? velocity < 0
                                      : dragDelta < 0;
                                  workspace.setSheetExtent(
                                    up
                                        ? extent == AgentSheetExtent.peek
                                              ? AgentSheetExtent.medium
                                              : AgentSheetExtent.expanded
                                        : extent == AgentSheetExtent.expanded
                                        ? AgentSheetExtent.medium
                                        : AgentSheetExtent.peek,
                                  );
                                },
                                onTap: () => workspace.setSheetExtent(
                                  extent == AgentSheetExtent.peek
                                      ? AgentSheetExtent.medium
                                      : extent == AgentSheetExtent.expanded
                                      ? AgentSheetExtent.medium
                                      : AgentSheetExtent.peek,
                                ),
                                child: Center(
                                  child: Container(
                                    width: 36,
                                    height: 4,
                                    decoration: BoxDecoration(
                                      color: Theme.of(
                                        context,
                                      ).colorScheme.outline,
                                      borderRadius: BorderRadius.circular(4),
                                    ),
                                  ),
                                ),
                              ),
                            ),
                          ),
                          Positioned(
                            right: 8,
                            top: 0,
                            bottom: 0,
                            child: IconButton(
                              key: const Key('agent-sheet-map-toggle'),
                              tooltip: extent == AgentSheetExtent.peek
                                  ? '展开对话'
                                  : modeLabel,
                              onPressed: () {
                                if (extent != AgentSheetExtent.peek) {
                                  FocusManager.instance.primaryFocus?.unfocus();
                                }
                                workspace.setSheetExtent(
                                  extent == AgentSheetExtent.peek
                                      ? AgentSheetExtent.expanded
                                      : AgentSheetExtent.peek,
                                );
                              },
                              icon: Icon(
                                extent == AgentSheetExtent.peek
                                    ? Icons.chat_bubble_outline
                                    : Icons.map_outlined,
                                size: 20,
                              ),
                            ),
                          ),
                        ],
                      ),
                    ),
                    Expanded(
                      // Retain both coordinated scroll positions while the
                      // map peeks through; a new task/principal starts fresh.
                      child: _RetainedConversationViewport(
                        key: ValueKey((
                          workspace,
                          workspace.task?.id,
                          workspace.task?.principalType,
                          workspace.task?.principalID,
                          workspace.task?.actingUserID,
                        )),
                        peek: !embedded && extent == AgentSheetExtent.peek,
                        header: measurement == null
                            ? header
                            : NaturalContentHeight(
                                onHeight: measurement.header,
                                child: header,
                              ),
                        conversation: _LatestConversation(
                          attachmentBuilder: attachmentBuilder,
                          workspace: workspace,
                          onSuggestion: onSuggestion,
                          onAction: onAction,
                          onRetry: onRetry,
                          onOpenMapAreaItem: onOpenMapAreaItem,
                          onContentHeightChanged: measurement?.conversation,
                          answerBuilder: (source, current) {
                            // The persisted message remains a general history
                            // notice. Render the live observation separately.
                            if (source.onlinePlaceSearch != null &&
                                (current ||
                                    source.onlinePlaceSearch!.durable)) {
                              return Column(
                                crossAxisAlignment: CrossAxisAlignment.stretch,
                                children: [
                                  Text(
                                    source.onlinePlaceSearch!.current
                                        ? source.responseMessage
                                        : '这次联网结果已失效，请重新搜索查看最新信息。',
                                    style: NowUiTokens.reviewedOf(context).body,
                                  ),
                                  _ResultList(
                                    workspace: workspace,
                                    resultOverride: source,
                                    saved: saved,
                                    plans: plans,
                                    onContact: onContact,
                                    onOpenActivity: onOpenActivity,
                                    onOpenPlace: onOpenPlace,
                                    onOpenOnlineIntent: onOpenOnlineIntent,
                                    onOpenEntity: onOpenEntity,
                                    onReviewAgentJoin: current
                                        ? onReviewAgentJoin
                                        : null,
                                    onShareEntity: onShareEntity,
                                    onSaveEntity: onSaveEntity,
                                    onActivityImpression: onActivityImpression,
                                    onAction: onAction,
                                    onSaveSocialIntent: onSaveSocialIntent,
                                    onSuggestion: onSuggestion,
                                    onRetry: onRetry,
                                    historical: !current,
                                    publicEvidenceCurrent:
                                        publicEvidenceCurrent,
                                    publicEvidenceChanges:
                                        publicEvidenceChanges,
                                  ),
                                ],
                              );
                            }
                            if (PlaceFactPresentation.read(workspace, source) ==
                                null) {
                              return null;
                            }
                            return _LeaseBoundPlaceAnswer(
                              key: ValueKey((
                                'place-fact',
                                source.resultSet?.id,
                              )),
                              workspace: workspace,
                              result: source,
                              saved: saved,
                              onFocus: onFocusReplyPlace,
                              onNavigate: onNavigateReplyPlace,
                              onShare:
                                  onShareReplyEntity ??
                                  (current && onShareEntity != null
                                      ? (_, item) => onShareEntity!(item)
                                      : null),
                              onSave:
                                  onSaveReplyEntity ??
                                  (current && onSaveEntity != null
                                      ? (_, item) => onSaveEntity!(item)
                                      : null),
                              fallback: () => Column(
                                crossAxisAlignment: CrossAxisAlignment.stretch,
                                children: [
                                  Text(
                                    source.responseMessage,
                                    style: NowUiTokens.reviewedOf(context).body,
                                  ),
                                  _ResultList(
                                    workspace: workspace,
                                    resultOverride: source,
                                    historical: !current,
                                    saved: current ? saved : null,
                                    plans: current ? plans : null,
                                    onContact: current ? onContact : null,
                                    onOpenActivity: current
                                        ? onOpenActivity
                                        : null,
                                    onOpenPlace: current ? onOpenPlace : null,
                                    onOpenOnlineIntent: current
                                        ? onOpenOnlineIntent
                                        : null,
                                    onOpenEntity: null,
                                    onReviewAgentJoin: current
                                        ? onReviewAgentJoin
                                        : null,
                                    onShareEntity: null,
                                    onSaveEntity: null,
                                    onActivityImpression: current
                                        ? onActivityImpression
                                        : null,
                                    onAction: current ? onAction : null,
                                    onSaveSocialIntent: current
                                        ? onSaveSocialIntent
                                        : null,
                                    onSuggestion: onSuggestion,
                                    onRetry: onRetry,
                                  ),
                                ],
                              ),
                            );
                          },
                          resultBuilder: (source, current) => _ResultList(
                            workspace: workspace,
                            resultOverride: source,
                            publicEvidenceCurrent: current
                                ? publicEvidenceCurrent
                                : null,
                            publicEvidenceChanges: current
                                ? publicEvidenceChanges
                                : null,
                            saved: current ? saved : null,
                            plans: current ? plans : null,
                            onContact: current ? onContact : null,
                            onOpenActivity: current ? onOpenActivity : null,
                            onOpenPlace: current ? onOpenPlace : null,
                            onOpenOnlineIntent: current
                                ? onOpenOnlineIntent
                                : null,
                            onOpenEntity: onOpenReplyEntity != null
                                ? (item) => onOpenReplyEntity!(source, item)
                                : current
                                ? onOpenEntity
                                : null,
                            onReviewAgentJoin: current
                                ? onReviewAgentJoin
                                : null,
                            onShareEntity: current ? onShareEntity : null,
                            onSaveEntity: current ? onSaveEntity : null,
                            onActivityImpression: current
                                ? onActivityImpression
                                : null,
                            onAction: current ? onAction : null,
                            onSaveSocialIntent: current
                                ? onSaveSocialIntent
                                : null,
                            onSuggestion: onSuggestion,
                            onRetry: onRetry,
                            historical: !current,
                          ),
                        ),
                      ),
                    ),
                  ],
                );
              },
            ),
          );
        },
      );
    },
  );
}

// A peek is a visibility change, not a new conversation. Keep the hidden
// viewport's last visible geometry so shrinking the map sheet cannot clamp its
// coordinated header/list offsets. Offstage removes hit testing and semantics;
// only the compact header remains interactive while the conversation is hidden.
class _RetainedConversationViewport extends StatefulWidget {
  const _RetainedConversationViewport({
    super.key,
    required this.peek,
    required this.header,
    required this.conversation,
  });

  final bool peek;
  final Widget header;
  final Widget conversation;

  @override
  State<_RetainedConversationViewport> createState() =>
      _RetainedConversationViewportState();
}

class _RetainedConversationViewportState
    extends State<_RetainedConversationViewport> {
  Widget? _visibleHeader;
  double? _visibleHeight;

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) {
      if (!widget.peek || _visibleHeader == null) {
        _visibleHeader = widget.header;
        _visibleHeight = constraints.maxHeight;
      }
      final height = _visibleHeight!;
      return Stack(
        fit: StackFit.expand,
        children: [
          Offstage(
            offstage: widget.peek,
            child: OverflowBox(
              alignment: Alignment.topCenter,
              minHeight: height,
              maxHeight: height,
              child: SizedBox(
                height: height,
                child: NestedScrollView(
                  key: const Key('agent-sheet-content'),
                  headerSliverBuilder: (context, innerBoxIsScrolled) => [
                    SliverToBoxAdapter(child: _visibleHeader!),
                  ],
                  body: widget.conversation,
                ),
              ),
            ),
          ),
          if (widget.peek)
            SingleChildScrollView(
              key: const Key('agent-sheet-content'),
              child: widget.header,
            ),
        ],
      );
    },
  );
}

// Reuse the NestedScrollView's inner controller rather than giving conversation
// and header competing scroll positions. Only presentation follows the current
// turn; request, source and principal retirement remain owned by the workspace.
class _LatestConversation extends StatefulWidget {
  const _LatestConversation({
    required this.workspace,
    required this.onSuggestion,
    required this.onRetry,
    this.onAction,
    this.resultBuilder,
    this.answerBuilder,
    this.onContentHeightChanged,
    this.attachmentBuilder,
    this.onOpenMapAreaItem,
  });

  final AgentWorkspaceController workspace;
  final ValueChanged<String> onSuggestion;
  final VoidCallback onRetry;
  final ValueChanged<AgentAction>? onAction;
  final Widget Function(AgentResult, bool)? resultBuilder;
  final Widget? Function(AgentResult, bool)? answerBuilder;
  final ValueChanged<double>? onContentHeightChanged;
  final Widget Function(int messageIndex)? attachmentBuilder;
  final ValueChanged<TypedMapItem>? onOpenMapAreaItem;

  @override
  State<_LatestConversation> createState() => _LatestConversationState();
}

class _LatestConversationState extends State<_LatestConversation> {
  late List<AgentMessage> _messages;
  late int _epoch;
  late AgentQueryState _queryState;
  bool _followingLatest = true;
  int _scrollGeneration = 0;
  bool _followingProgrammatically = false;
  final _currentMessageAnchor = GlobalKey();
  final _currentResultAnchor = GlobalKey();
  final _currentAreaAnchor = GlobalKey();
  String? _areaID;

  @override
  void initState() {
    super.initState();
    _messages = List.of(widget.workspace.conversation);
    _epoch = widget.workspace.taskEpoch;
    _queryState = widget.workspace.queryState;
    _areaID = widget.workspace.areaOperation?.id;
    widget.workspace.addListener(_observeRetirement);
    _followCurrentTurn();
  }

  // A controller can retire a Task and restore the same ID before the next
  // paint. Observe that real notification rather than treating an equal final
  // key as permission to retain the retired reading/following state.
  void _observeRetirement() {
    if (widget.workspace.task == null &&
        widget.workspace.areaOperation == null) {
      _followingLatest = true;
      ++_scrollGeneration;
    }
  }

  @override
  void dispose() {
    widget.workspace.removeListener(_observeRetirement);
    ++_scrollGeneration;
    super.dispose();
  }

  @override
  void didUpdateWidget(_LatestConversation oldWidget) {
    super.didUpdateWidget(oldWidget);
    final workspace = widget.workspace;
    if (!identical(oldWidget.workspace, workspace)) {
      oldWidget.workspace.removeListener(_observeRetirement);
      workspace.addListener(_observeRetirement);
      ++_scrollGeneration;
    }
    final current = workspace.conversation;
    final changed = !_sameMessages(_messages, current);
    final recoveryChanged =
        workspace.areaOperation == null &&
        _epoch == workspace.taskEpoch &&
        _queryState != workspace.queryState &&
        _prefersRecovery(workspace.queryState);
    final newSubmission =
        workspace.taskEpoch != _epoch &&
        workspace.state == AgentViewState.searching &&
        changed &&
        current.isNotEmpty &&
        current.last.role == 'user';
    final newAreaCommand =
        workspace.areaOperation?.id != _areaID &&
        workspace.areaOperation?.status == AgentAreaStatus.loading;
    if (!identical(oldWidget.workspace, workspace) ||
        newSubmission ||
        newAreaCommand) {
      _followingLatest = true;
    }
    _messages = List.of(current);
    _epoch = workspace.taskEpoch;
    _queryState = workspace.queryState;
    _areaID = workspace.areaOperation?.id;
    if (_followingLatest || recoveryChanged) _followCurrentTurn();
  }

  bool _prefersRecovery(AgentQueryState state) =>
      state == AgentQueryState.error ||
      state == AgentQueryState.needsScope ||
      state == AgentQueryState.unsupported;

  bool _sameMessages(List<AgentMessage> a, List<AgentMessage> b) {
    if (a.length != b.length) return false;
    for (var i = 0; i < a.length; i++) {
      if (a[i].role != b[i].role || a[i].text != b[i].text) return false;
    }
    return true;
  }

  void _followCurrentTurn() {
    final workspace = widget.workspace;
    final epoch = workspace.taskEpoch;
    final task = workspace.task;
    final queryState = workspace.queryState;
    // A map command owns the current area anchor while its retained Task may
    // still have an error. Only the current Task view restores that header.
    final recovery =
        workspace.areaOperation == null && _prefersRecovery(queryState);
    final messages = List<AgentMessage>.of(workspace.conversation);
    final generation = ++_scrollGeneration;

    void scrollAfterLayout(int remaining) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted ||
            !identical(workspace, widget.workspace) ||
            generation != _scrollGeneration ||
            (!_followingLatest && !recovery) ||
            workspace.taskEpoch != epoch ||
            workspace.queryState != queryState ||
            !identical(workspace.task, task) ||
            !_sameMessages(messages, workspace.conversation)) {
          return;
        }
        final controller = recovery
            ? context
                  .findAncestorStateOfType<NestedScrollViewState>()
                  ?.outerController
            : PrimaryScrollController.maybeOf(context);
        if (controller == null ||
            !controller.hasClients ||
            controller.positions.length != 1) {
          return;
        }
        final position = controller.position;
        if (!position.hasContentDimensions) {
          return;
        }
        // A failed/unmet current turn must first expose the existing unique
        // recovery header. It must not appear successful via old conversation.
        // Inner offset zero still means a collapsed nested header. Recovery
        // therefore uses the existing outer controller to restore that header.
        var target = recovery
            ? position.minScrollExtent
            : position.maxScrollExtent;
        if (!recovery) {
          final anchor =
              (workspace.areaOperation == null
                      ? _currentMessageAnchor
                      : _currentAreaAnchor)
                  .currentContext
                  ?.findRenderObject();
          // On restoring a lazy list with many cards, the message itself may
          // not yet be laid out. Its result block provides the nearby position
          // for the next bounded frame; no text/entity lookup is needed.
          final resultAnchor = workspace.state == AgentViewState.searching
              ? null
              : _currentResultAnchor.currentContext?.findRenderObject();
          final render = anchor ?? resultAnchor;
          if (render != null && render.attached) {
            target = RenderAbstractViewport.of(
              render,
            ).getOffsetToReveal(render, 0).offset;
            if (anchor == null) target -= 24;
          }
          target = target
              .clamp(position.minScrollExtent, position.maxScrollExtent)
              .toDouble();
        }
        if ((target - position.pixels).abs() >= 0.5) {
          _followingProgrammatically = true;
          try {
            controller.jumpTo(target);
          } finally {
            _followingProgrammatically = false;
          }
        }
        // Locate the current explanation before its cards. Correction is
        // bounded and every frame still checks the original live-turn fence.
        if (remaining > 0) scrollAfterLayout(remaining - 1);
      });
    }

    scrollAfterLayout(recovery ? 0 : 3);
  }

  @override
  Widget build(BuildContext context) =>
      NotificationListener<ScrollNotification>(
        onNotification: (notification) {
          // Reading is a user choice, including a vertical material list
          // nested inside a message. A later idle/end notification or a lazy
          // extent estimate is not permission to resume automatic following.
          // Only the existing new-submission/retirement paths resume it.
          if (!_followingProgrammatically &&
              notification.metrics.axis == Axis.vertical &&
              ((notification is ScrollStartNotification &&
                      notification.dragDetails != null) ||
                  (notification is UserScrollNotification &&
                      notification.direction != ScrollDirection.idle) ||
                  notification is ScrollUpdateNotification)) {
            _followingLatest = false;
            ++_scrollGeneration;
          }
          return false;
        },
        child: NotificationListener<ScrollMetricsNotification>(
          onNotification: (notification) {
            if (notification.depth == 0 &&
                _followingLatest &&
                !_followingProgrammatically) {
              _followCurrentTurn();
            }
            return false;
          },
          child: AgentConversation(
            workspace: widget.workspace,
            onSuggestion: widget.onSuggestion,
            onAction: widget.onAction,
            onRetry: widget.onRetry,
            resultBuilder: widget.resultBuilder == null
                ? null
                : (result, current) => KeyedSubtree(
                    key: current ? _currentResultAnchor : null,
                    child: widget.resultBuilder!(result, current),
                  ),
            answerBuilder: widget.answerBuilder,
            onContentHeightChanged: widget.onContentHeightChanged,
            currentMessageAnchor: _currentMessageAnchor,
            currentAreaAnchor: _currentAreaAnchor,
            onOpenMapAreaItem: widget.onOpenMapAreaItem,
            attachmentBuilder: widget.attachmentBuilder,
          ),
        ),
      );
}

class _SheetNaturalHeights {
  _SheetNaturalHeights(this.onChanged);
  final ValueChanged<double> onChanged;
  double? _header, _conversation;
  void header(double value) {
    _header = value;
    _report();
  }

  void conversation(double value) {
    _conversation = value;
    _report();
  }

  void _report() {
    if (_header != null && _conversation != null) {
      onChanged(48 + _header! + _conversation!);
    }
  }
}

class _LeaseBoundPlaceAnswer extends StatefulWidget {
  const _LeaseBoundPlaceAnswer({
    super.key,
    required this.workspace,
    required this.result,
    required this.fallback,
    this.saved,
    this.onFocus,
    this.onNavigate,
    this.onShare,
    this.onSave,
  });
  final AgentWorkspaceController workspace;
  final AgentResult result;
  final SavedController? saved;
  final Widget Function() fallback;
  final void Function(AgentResult, AgentResultItem)? onFocus,
      onNavigate,
      onShare,
      onSave;

  @override
  State<_LeaseBoundPlaceAnswer> createState() => _LeaseBoundPlaceAnswerState();
}

class _LeaseBoundPlaceAnswerState extends State<_LeaseBoundPlaceAnswer> {
  Timer? _expiry;

  AgentPlaceSourceBinding? Function() _sourceResolver(
    PlaceFactPresentation original,
  ) {
    final workspace = widget.workspace;
    final taskID = workspace.task?.id;
    final epoch = workspace.taskIdentityEpoch;
    final indices = workspace.replies
        .where((reply) => identical(reply.result, original.result))
        .map((reply) => reply.messageIndex)
        .toList();
    final index = indices.length == 1 ? indices.single : null;
    AgentPlaceSourceBinding? resolve() {
      if (taskID == null ||
          index == null ||
          workspace.task?.id != taskID ||
          workspace.taskIdentityEpoch != epoch)
        return null;
      final replies = workspace.replies
          .where((reply) => reply.messageIndex == index)
          .toList();
      if (replies.length != 1) return null;
      final result = replies.single.result;
      final fresh = PlaceFactPresentation.read(workspace, result);
      if (fresh == null || fresh.entityID != original.entityID) return null;
      final place = fresh.place;
      return AgentPlaceSourceBinding(
        taskID: taskID,
        messageIndex: index,
        entityID: fresh.entityID,
        contentIdentity: jsonEncode([
          place.id,
          place.name,
          place.addressLabel,
          place.location.latitude,
          place.location.longitude,
          place.source.label,
          place.source.reference,
          place.source.maintainer,
          place.source.freshness,
          place.source.updatedAt?.toIso8601String(),
        ]),
        sources: fresh.sources,
        placeSource: place.source,
        current: () =>
            workspace.task?.id == taskID &&
            workspace.taskIdentityEpoch == epoch &&
            PlaceFactPresentation.read(workspace, result) != null,
      );
    }

    return resolve;
  }

  void _schedule() {
    _expiry?.cancel();
    final remaining = widget.workspace
        .messageSourcesForReply(widget.result)
        ?.remaining;
    if (remaining != null && remaining > Duration.zero) {
      _expiry = Timer(remaining, () {
        if (mounted) setState(() {});
      });
    }
  }

  @override
  void initState() {
    super.initState();
    _schedule();
  }

  @override
  void didUpdateWidget(_LeaseBoundPlaceAnswer oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!identical(oldWidget.result, widget.result)) _schedule();
  }

  @override
  void dispose() {
    _expiry?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final presentation = PlaceFactPresentation.read(
      widget.workspace,
      widget.result,
    );
    if (presentation == null) return widget.fallback();
    bool current() =>
        PlaceFactPresentation.read(widget.workspace, widget.result) != null;
    void act(void Function(AgentResult, AgentResultItem)? callback) {
      if (current()) callback?.call(widget.result, presentation.item);
    }

    return PlaceAnswerCard(
      presentation: presentation,
      current: current,
      changes: widget.workspace,
      resolveSources: _sourceResolver(presentation),
      onFocus: widget.onFocus == null ? null : () => act(widget.onFocus),
      onNavigate: widget.onNavigate == null
          ? null
          : () => act(widget.onNavigate),
      onShare: widget.onShare == null ? null : () => act(widget.onShare),
      onSave: widget.onSave == null ? null : () => act(widget.onSave),
      saved: widget.saved?.contains('place', presentation.place.id) ?? false,
      saving: widget.saved?.isBusy('place', presentation.place.id) ?? false,
    );
  }
}

class _ResultList extends StatelessWidget {
  const _ResultList({
    required this.workspace,
    this.resultOverride,
    this.historical = false,
    this.publicEvidenceCurrent,
    this.publicEvidenceChanges,
    required this.saved,
    required this.plans,
    required this.onContact,
    required this.onOpenActivity,
    required this.onOpenPlace,
    required this.onOpenOnlineIntent,
    required this.onOpenEntity,
    this.onReviewAgentJoin,
    required this.onShareEntity,
    required this.onSaveEntity,
    required this.onActivityImpression,
    required this.onAction,
    required this.onSaveSocialIntent,
    required this.onSuggestion,
    required this.onRetry,
  });
  final AgentWorkspaceController workspace;
  final AgentResult? resultOverride;
  final bool historical;
  final SavedController? saved;
  final ActivityPlansController? plans;
  final ValueChanged<AgentPerson>? onContact;
  final ValueChanged<PublicActivity>? onOpenActivity;
  final ValueChanged<String>? onOpenPlace;
  final ValueChanged<String>? onOpenOnlineIntent;
  final ValueChanged<AgentResultItem>? onOpenEntity;
  final void Function(AgentResult, AgentResultItem)? onReviewAgentJoin;
  final ValueChanged<AgentResultItem>? onShareEntity;
  final ValueChanged<AgentResultItem>? onSaveEntity;
  final ValueChanged<PublicActivity>? onActivityImpression;
  final ValueChanged<AgentAction>? onAction;
  final ValueChanged<AgentTask>? onSaveSocialIntent;
  final ValueChanged<String> onSuggestion;
  final VoidCallback onRetry;

  final bool Function(AgentResultItem)? publicEvidenceCurrent;
  final Listenable? publicEvidenceChanges;

  PublicSource? _sourceFor(AgentResult result, AgentResultItem item) {
    if (item.entity.type == 'place') {
      for (final place in result.places) {
        if (place.id == item.entity.id) return place.source;
      }
    }
    if (item.entity.type == 'activity') {
      for (final activity in result.activities) {
        if (activity.id == item.entity.id) return activity.source;
      }
    }
    return null;
  }

  PublicActivity? _activityFor(AgentResult result, AgentResultItem item) {
    final id = item.entity.type == 'opportunity'
        ? item.detail?.id
        : item.entity.type == 'activity'
        ? item.entity.id
        : null;
    if (id == null) return null;
    for (final activity in result.activities) {
      if (activity.id == id) return activity;
    }
    return null;
  }

  Future<void> _toggleSaved(
    BuildContext context,
    String kind,
    String targetId,
  ) async {
    try {
      final source = saved!;
      final result = workspace.result;
      final token = source.authorizationHeader();
      final ref = EntityActionRef(
        kind == 'group' ? 'community' : kind,
        targetId,
      );
      final expectedOperation = source.contains(kind, targetId)
          ? 'UNSAVE'
          : 'SAVE';
      bool current() =>
          identical(workspace.result, result) &&
          source.authorizationHeader() == token;
      await runEntityAction(
        context,
        ref: ref,
        kind: EntityActionKind.save,
        operation: expectedOperation,
        authorizationHeader: () => current() ? token : null,
        identityChanges: Listenable.merge([workspace, source]),
        client: source.followClient,
        apiBaseUrl: source.followApiBaseUrl,
        domainCurrent: current,
        handler: (approved) async {
          if (!current()) return;
          await source.toggle(
            kind,
            targetId,
            entrySource: 'agent',
            approved: approved,
          );
        },
      );
    } catch (_) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              saved!.authorizationHeader() == null
                  ? '请先登录再收藏内容。'
                  : '收藏更新失败，请重试。',
            ),
          ),
        );
      }
    }
  }

  Future<void> _togglePlanned(BuildContext context, String activityId) async {
    try {
      await plans!.toggle(activityId);
    } catch (_) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              plans!.authorizationHeader() == null
                  ? '请先登录再添加活动计划。'
                  : '活动计划更新失败，请重试。',
            ),
          ),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final tokens = NowUiTokens.reviewedOf(context);
    final result = resultOverride ?? workspace.result;
    if (resultOverride == null && workspace.requestError != null) {
      return const SizedBox.shrink();
    }
    if (result == null) {
      if (workspace.state == AgentViewState.searching) {
        return const Center(child: CircularProgressIndicator());
      }
      return const SizedBox.shrink();
    }
    if (result.isUnsupported) return const SizedBox.shrink();
    final messageSources = workspace.messageSourcesForReply(result);
    if (result.hasOnlyMessageSources) {
      return messageSources == null
          ? const SizedBox.shrink()
          : AgentAnswerSourcesPanel.persisted(
              sources: messageSources,
              changes: workspace,
              replyCurrent: () =>
                  workspace.retainsMessageSources(result, messageSources),
            );
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (messageSources != null)
          AgentAnswerSourcesPanel.persisted(
            sources: messageSources,
            changes: workspace,
            replyCurrent: () =>
                workspace.retainsMessageSources(result, messageSources),
          )
        else if (result.resultSet?.answerSources case final answer?)
          AgentAnswerSourcesPanel(
            answer: answer,
            changes: workspace,
            replyCurrent: () => workspace.retainsAnswerSources(result),
          ),
        for (final item in result.onlineIntents)
          Material(
            color: Colors.transparent,
            child: ListTile(
              key: Key('online-intent-${item.id}'),
              minTileHeight: 48,
              leading: const Icon(Icons.language),
              title: Text(item.title, style: tokens.placeTitle),
              subtitle: Text('公开线上意图 · 查看原始详情', style: tokens.listSubtitle),
              onTap: onOpenOnlineIntent == null
                  ? null
                  : () => onOpenOnlineIntent!(item.id),
            ),
          ),
        if (onSaveSocialIntent != null &&
            workspace.task?.principalType.toUpperCase() == 'PERSON' &&
            workspace.task?.intent == 'FIND_ACTIVITY' &&
            workspace.task?.status == 'COMPLETED') ...[
          const SizedBox(height: 8),
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton.icon(
              style: TextButton.styleFrom(
                minimumSize: const Size(48, 48),
                textStyle: tokens.action,
              ),
              onPressed: () => onSaveSocialIntent!(workspace.task!),
              icon: const Icon(Icons.edit_note),
              label: const Text('保存为社交意图草稿'),
            ),
          ),
        ],
        if (onAction != null && result.actions.isNotEmpty) ...[
          const SizedBox(height: 10),
          Wrap(
            spacing: 8,
            children: [
              for (final action in result.actions)
                ActionChip(
                  labelStyle: tokens.action,
                  label: Text(action.label),
                  onPressed: () => onAction!(action),
                ),
            ],
          ),
        ],
        if (onAction != null &&
            workspace.task?.principalType.toUpperCase() == 'PERSON' &&
            (workspace.task?.principalID.isNotEmpty ?? false) &&
            workspace.task?.intent == 'FIND_ACTIVITY' &&
            workspace.task?.status == 'COMPLETED')
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton.icon(
              style: TextButton.styleFrom(
                minimumSize: const Size(48, 48),
                textStyle: tokens.action,
              ),
              onPressed: () => onAction!(
                const AgentAction(
                  type: 'OPEN_OPPORTUNITIES',
                  label: '查看我的活动机会',
                ),
              ),
              icon: const Icon(Icons.event_available_outlined),
              label: const Text('查看我的活动机会'),
            ),
          ),
        if (!historical && result.followUps.isNotEmpty) ...[
          const SizedBox(height: 10),
          Wrap(
            spacing: 8,
            runSpacing: 4,
            children: [
              for (final followUp in result.followUps)
                ActionChip(
                  labelStyle: tokens.action,
                  label: Text(followUp),
                  onPressed: () => onSuggestion(followUp),
                ),
            ],
          ),
        ],
        if (!result.replyProjectionCurrent &&
            !workspace.canUseExternalPlaces(result))
          Padding(
            padding: const EdgeInsets.symmetric(vertical: 12),
            child: Text('这条回答的结果已失效，请从最近对话重新打开。', style: tokens.body),
          ),
        if (result.onlinePlaceSearch case final search?)
          ExternalPlaceCandidatesPanel(
            search: search,
            current: () => workspace.canUseExternalPlaces(result),
            changes: workspace,
            onRefresh:
                !historical && workspace.state != AgentViewState.searching
                ? () => onSuggestion('更新餐厅信息')
                : null,
            onMap: (place) {
              FocusManager.instance.primaryFocus?.unfocus();
              workspace.showExternalPlaceOnMap(result, place);
            },
          ),
        if (workspace.publicHistoryNotice case final notice?)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: 8),
            child: Text(notice, style: tokens.body),
          ),
        if (result.projectionItems != null)
          for (final item in result.projectionItems!)
            AgentEntityResultCard(
              key: ObjectKey(item),
              item: item,
              activity: _activityFor(result, item),
              authorizationHeader: saved?.authorizationHeader,
              mediaBaseUrl: saved?.followApiBaseUrl ?? '',
              mediaClient: saved?.followClient,
              mediaIdentityChanges: Listenable.merge([
                workspace,
                if (saved != null) saved!,
              ]),
              mediaCurrent: () =>
                  workspace.canUseReplyProjection(result) &&
                  saved?.authorizationHeader() != null,
              onReviewAgentJoin:
                  onReviewAgentJoin != null &&
                      canReviewAgentActivityJoin(workspace, result, item)
                  ? () => onReviewAgentJoin!(result, item)
                  : null,
              source: _sourceFor(result, item),
              sourceCurrent: () => workspace.canUseReplyProjection(result),
              sourceChanges: workspace,
              onMap: () {
                FocusManager.instance.primaryFocus?.unfocus();
                workspace.showReplyOnMap(result, item.entity.mapID);
              },
              publicFieldEvidence: result.resultSet?.publicFieldEvidence,
              publicEvidenceCurrent: publicEvidenceCurrent,
              publicEvidenceChanges: publicEvidenceChanges,
              onOpen: onOpenEntity,
              onShare: onShareEntity,
              onSave: item.actions != null
                  ? (onSaveEntity == null || item.bookmarkKind == null
                        ? null
                        : () => onSaveEntity!(item))
                  : saved == null || item.bookmarkKind == null
                  ? null
                  : () => _toggleSaved(
                      context,
                      item.bookmarkKind!,
                      item.entity.id,
                    ),
              saved:
                  item.bookmarkKind != null &&
                  (saved?.contains(item.bookmarkKind!, item.entity.id) ??
                      false),
              saving:
                  item.bookmarkKind != null &&
                  (saved?.isBusy(item.bookmarkKind!, item.entity.id) ?? false),
              onReminder:
                  item.actions == null ||
                      plans == null ||
                      item.entity.type != 'activity' ||
                      !result.activities.any(
                        (a) =>
                            a.id == item.entity.id &&
                            const {'upcoming', 'ongoing'}.contains(a.status),
                      )
                  ? null
                  : () => _togglePlanned(context, item.entity.id),
              onPlan: item.actions != null
                  ? (item.detail?.type != 'activity' &&
                                item.detail?.type != 'community' ||
                            onOpenEntity == null
                        ? null
                        : () => onOpenEntity!(item))
                  : plans == null ||
                        item.entity.type != 'activity' ||
                        !result.activities.any(
                          (a) =>
                              a.id == item.entity.id &&
                              const {'upcoming', 'ongoing'}.contains(a.status),
                        )
                  ? null
                  : () => _togglePlanned(context, item.entity.id),
              planned: plans?.contains(item.entity.id) ?? false,
              planning: plans?.busy.contains(item.entity.id) ?? false,
              onContact: item.actions != null
                  ? (item.entity.type != 'person' || onOpenEntity == null
                        ? null
                        : () => onOpenEntity!(item))
                  : item.entity.type != 'person' ||
                        onContact == null ||
                        !result.people.any((p) => p.accountID == item.entity.id)
                  ? null
                  : () => onContact!(
                      result.people.firstWhere(
                        (p) => p.accountID == item.entity.id,
                      ),
                    ),
              onMessage:
                  item.actions != null &&
                      item.entity.type == 'person' &&
                      onOpenEntity != null
                  ? () => onOpenEntity!(item)
                  : null,
              // The current domain callback opens details. It must not be
              // presented as a separate navigation or route action.
            ),
        if (result.projectionItems == null && result.activities.isNotEmpty) ...[
          const _Heading('活动'),
          for (final activity in result.activities)
            Builder(
              builder: (context) {
                if (onActivityImpression != null) {
                  WidgetsBinding.instance.addPostFrameCallback(
                    (_) => onActivityImpression!(activity),
                  );
                }
                return _Row(
                  icon: Icons.event_outlined,
                  title: activity.title,
                  subtitle: [
                    if (activity.organizer?.name.isNotEmpty == true)
                      '主办方：${activity.organizer!.name}',
                    if (activity.schedule.isNotEmpty) activity.schedule,
                    if (activity.placeName.isNotEmpty) activity.placeName,
                    _activityStatusLabel(activity.status),
                    activity.source.label,
                  ].join(' · '),
                  onTap: onOpenActivity == null
                      ? (activity.location?.hasPublicPoint == true
                            ? () => workspace.selectEntity(
                                'activity:${activity.id}',
                              )
                            : null)
                      : () => onOpenActivity!(activity),
                  saved: saved?.contains('activity', activity.id) ?? false,
                  saving: saved?.isBusy('activity', activity.id) ?? false,
                  onSave: saved == null
                      ? null
                      : () => _toggleSaved(context, 'activity', activity.id),
                  planned: plans?.contains(activity.id) ?? false,
                  planning: plans?.busy.contains(activity.id) ?? false,
                  onPlan:
                      plans == null ||
                          (activity.status != 'upcoming' &&
                              activity.status != 'ongoing')
                      ? null
                      : () => _togglePlanned(context, activity.id),
                );
              },
            ),
        ],
        if (result.projectionItems == null && result.people.isNotEmpty) ...[
          const _Heading('用户'),
          for (final person in result.people)
            _Row(
              icon: Icons.person_outline,
              title: person.displayName,
              subtitle:
                  '${person.topic} · ${person.areaLabel}（大致位置）${person.mapLatitude == null || person.mapLongitude == null ? ' · 未显示地图标记' : ' · 公开区域标记'}',
              onTap: person.mapLatitude == null || person.mapLongitude == null
                  ? null
                  : () => workspace.selectEntity('person:${person.accountID}'),
              onContact: onContact == null ? null : () => onContact!(person),
            ),
        ],
        if (result.projectionItems == null && result.groups.isNotEmpty) ...[
          const _Heading('社群'),
          for (final group in result.groups)
            _Row(
              icon: Icons.group_outlined,
              title: group.name,
              subtitle: '${group.summary} · 已发布社群',
              onTap:
                  result.entities.any(
                    (entity) => entity.id == 'group:${group.id}',
                  )
                  ? () => workspace.selectEntity('group:${group.id}')
                  : null,
              saved: saved?.contains('group', group.id) ?? false,
              saving: saved?.isBusy('group', group.id) ?? false,
              onSave: saved == null
                  ? null
                  : () => _toggleSaved(context, 'group', group.id),
            ),
        ],
        if (result.projectionItems == null &&
            result.organizations.isNotEmpty) ...[
          const _Heading('组织'),
          for (final organization in result.organizations)
            _Row(
              icon: Icons.groups_outlined,
              title: organization.name,
              subtitle: [
                if (organization.description.isNotEmpty)
                  organization.description,
                organization.verificationStatus == 'verified' ? '已认证' : '未认证',
              ].join(' · '),
            ),
        ],
        if (result.projectionItems == null && result.places.isNotEmpty) ...[
          const _Heading('地点'),
          for (final place in result.places)
            _Row(
              icon: Icons.place_outlined,
              title: place.name,
              subtitle: '城市公开数据 · ${place.source.label}',
              onTap: onOpenPlace == null ? null : () => onOpenPlace!(place.id),
              saved: saved?.contains('place', place.id) ?? false,
              saving: saved?.isBusy('place', place.id) ?? false,
              onSave: saved == null
                  ? null
                  : () => _toggleSaved(context, 'place', place.id),
            ),
        ],
        SponsoredOpportunitiesSection(
          items: result.sponsoredOpportunities,
          unavailable: result.sponsoredUnavailable,
          canOpen: (item) => item.targetType == 'ACTIVITY'
              ? onOpenActivity != null &&
                    result.activities.any((a) => a.id == item.targetID)
              : onOpenPlace != null &&
                    result.places.any((p) => p.id == item.targetID),
          onOpen: (item) {
            if (item.targetType == 'ACTIVITY') {
              final matches = result.activities.where(
                (a) => a.id == item.targetID,
              );
              if (matches.isNotEmpty) onOpenActivity?.call(matches.first);
            } else if (result.places.any((p) => p.id == item.targetID)) {
              onOpenPlace?.call(item.targetID);
            }
          },
        ),
      ],
    );
  }
}

class _Heading extends StatelessWidget {
  const _Heading(this.label);
  final String label;
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.fromLTRB(0, 23, 0, 5),
    child: Text(label, style: NowUiTokens.reviewedOf(context).sheetTitle),
  );
}

class _Row extends StatelessWidget {
  const _Row({
    required this.icon,
    required this.title,
    required this.subtitle,
    this.onTap,
    this.onSave,
    this.onPlan,
    this.onContact,
    this.saved = false,
    this.saving = false,
    this.planned = false,
    this.planning = false,
  });
  final IconData icon;
  final String title;
  final String subtitle;
  final VoidCallback? onTap;
  final VoidCallback? onSave;
  final VoidCallback? onPlan;
  final VoidCallback? onContact;
  final bool saved;
  final bool saving;
  final bool planned;
  final bool planning;
  @override
  Widget build(BuildContext context) => Material(
    color: Colors.transparent,
    child: InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(14),
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 10, horizontal: 2),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Padding(
                  padding: const EdgeInsets.only(top: 2, right: 10),
                  child: Icon(
                    icon,
                    color: Theme.of(context).colorScheme.onSurface,
                  ),
                ),
                Expanded(
                  child: Text(
                    title,
                    softWrap: true,
                    style: NowUiTokens.reviewedOf(context).placeTitle,
                  ),
                ),
                if (onTap != null)
                  const Padding(
                    padding: EdgeInsets.only(left: 8),
                    child: Icon(Icons.arrow_outward, size: 16),
                  ),
              ],
            ),
            Padding(
              padding: const EdgeInsets.only(left: 34, top: 4),
              child: Text(
                subtitle,
                softWrap: true,
                style: NowUiTokens.reviewedOf(context).listSubtitle,
              ),
            ),
            if (onSave != null || onPlan != null || onContact != null)
              Padding(
                padding: const EdgeInsets.only(left: 28, top: 2),
                child: Wrap(
                  spacing: 4,
                  children: [
                    if (onContact != null)
                      IconButton(
                        tooltip: '申请联系',
                        onPressed: onContact,
                        icon: const Icon(Icons.person_add_alt_outlined),
                      ),
                    if (onPlan != null)
                      IconButton(
                        tooltip: planned ? '移除活动计划' : '计划参加',
                        onPressed: planning ? null : onPlan,
                        icon: Icon(
                          planned
                              ? Icons.event_available
                              : Icons.event_available_outlined,
                        ),
                      ),
                    if (onSave != null)
                      IconButton(
                        tooltip: saved ? '取消收藏' : '收藏',
                        onPressed: saving ? null : onSave,
                        icon: Icon(
                          saved ? Icons.bookmark : Icons.bookmark_outline,
                        ),
                      ),
                  ],
                ),
              ),
          ],
        ),
      ),
    ),
  );
}
