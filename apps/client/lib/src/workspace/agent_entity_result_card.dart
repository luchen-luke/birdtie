import 'package:flutter/material.dart';
import '../city/public_city_controller.dart' show PublicSource, PublicActivity;
import 'package:http/http.dart' as http;
import 'agent_answer_sources.dart' show AgentAnswerSource;
import 'agent_source_labels.dart';
import 'agent_result_projection.dart';
import 'entity_action_contract.dart';
import 'public_query_field_evidence.dart';
import 'public_query_field_evidence_panel.dart';
import 'now_ui_tokens.dart';
import 'person_avatar.dart';
import 'community_avatar.dart';
import 'entity_cover.dart';

String agentResultKindLabel(String kind) => switch (kind) {
  'person' => '用户',
  'activity' => '活动',
  'place' => '地点',
  'community' => '社区',
  'organization' => '组织',
  'business' => '商家',
  'opportunity' => '我的活动机会',
  'group' => '旧版社群',
  _ => '内容',
};

/// One original reference supplies the title, optional Pin and explicit actions.
class AgentEntityResultCard extends StatelessWidget {
  const AgentEntityResultCard({
    super.key,
    required this.item,
    this.onOpen,
    this.onShare,
    this.onSave,
    this.onPlan,
    this.onReviewAgentJoin,
    this.onReminder,
    this.onContact,
    this.onMessage,
    this.onNavigate,
    this.onMap,
    this.source,
    this.sourceCurrent,
    this.sourceChanges,
    this.saved = false,
    this.saving = false,
    this.planned = false,
    this.planning = false,
    this.publicFieldEvidence,
    this.publicEvidenceCurrent,
    this.publicEvidenceChanges,
    this.activity,
    this.authorizationHeader,
    this.mediaBaseUrl = '',
    this.mediaClient,
    this.mediaIdentityChanges,
    this.mediaCurrent,
  });
  final AgentResultItem item;
  final ValueChanged<AgentResultItem>? onOpen, onShare;
  final VoidCallback? onSave,
      onPlan,
      onReviewAgentJoin,
      onContact,
      onMessage,
      onNavigate,
      onReminder;
  final bool saved, saving, planned, planning;
  final PublicQueryFieldEvidence? publicFieldEvidence;
  final bool Function(AgentResultItem)? publicEvidenceCurrent;
  final VoidCallback? onMap;
  final PublicSource? source;
  final bool Function()? sourceCurrent;
  final Listenable? sourceChanges;
  final Listenable? publicEvidenceChanges;
  final PublicActivity? activity;
  final String? Function()? authorizationHeader;
  final String mediaBaseUrl;
  final http.Client? mediaClient;
  final Listenable? mediaIdentityChanges;
  final bool Function()? mediaCurrent;
  @override
  Widget build(BuildContext context) {
    if (!item.valid) return const SizedBox.shrink();
    // The sole production caller is the Now result sheet. Keep these typed
    // cards on the same semantic roles as native and external place cards.
    final tokens = NowUiTokens.reviewedOf(context);
    final color = Theme.of(context).colorScheme;
    final actionStyle = TextButton.styleFrom(
      minimumSize: const Size(48, 48),
      textStyle: tokens.action,
    );
    final type = agentResultKindLabel(item.entity.type);
    final sourceUri = Uri.tryParse(source?.reference ?? '');
    final canOpenSource =
        sourceUri != null &&
        const {'http', 'https'}.contains(sourceUri.scheme) &&
        sourceUri.host.isNotEmpty &&
        sourceUri.userInfo.isEmpty;
    final icon = switch (item.entity.type) {
      'person' => Icons.person_outline,
      'activity' => Icons.event_outlined,
      'place' => Icons.place_outlined,
      'community' || 'organization' || 'group' => Icons.groups_outlined,
      'business' => Icons.storefront_outlined,
      _ => Icons.lightbulb_outline,
    };
    final detail = item.detail != null && onOpen != null
        ? () => onOpen!(item)
        : null;
    final activityID = item.entity.type == 'opportunity'
        ? item.detail?.id
        : item.entity.type == 'activity'
        ? item.entity.id
        : null;
    final originalActivity = activity?.id == activityID ? activity : null;
    final locale = MaterialLocalizations.of(context);
    final start = originalActivity?.startsAt.toLocal();
    final activityDetails = originalActivity == null
        ? const <String>[]
        : [
            originalActivity.schedule.isNotEmpty
                ? originalActivity.schedule
                : '${locale.formatMediumDate(start!)} ${locale.formatTimeOfDay(TimeOfDay.fromDateTime(start), alwaysUse24HourFormat: true)}',
            if (originalActivity.modality == 'online')
              '线上活动'
            else if (originalActivity.placeName.isNotEmpty)
              originalActivity.placeName,
          ];
    final shared = item.share != null && onShare != null
        ? () => onShare!(item)
        : null;
    bool allowed(EntityActionKind kind) =>
        item.actions == null || item.action(kind)?.available == true;
    return Material(
      color: Colors.transparent,
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (item.cover != null &&
                mediaClient != null &&
                authorizationHeader != null)
              EntityCover(
                key: ObjectKey(item),
                metadata: item.cover,
                api: EntityCoverAPI(
                  client: mediaClient!,
                  baseUrl: mediaBaseUrl,
                  authorizationHeader: authorizationHeader!,
                ),
                identityChanges: mediaIdentityChanges ?? sourceChanges,
                current: () =>
                    sourceCurrent?.call() != false &&
                    mediaCurrent?.call() != false,
                width: 320,
                height: 180,
                label: '${item.title}的封面',
              ),
            if (item.entity.type == 'place' &&
                item.placePhoto != null &&
                mediaClient != null &&
                authorizationHeader != null)
              EntityCover(
                key: ObjectKey(item),
                metadata: item.placePhoto,
                api: EntityCoverAPI(
                  client: mediaClient!,
                  baseUrl: mediaBaseUrl,
                  authorizationHeader: authorizationHeader!,
                ),
                identityChanges: mediaIdentityChanges ?? sourceChanges,
                current: () =>
                    sourceCurrent?.call() != false &&
                    mediaCurrent?.call() != false,
                width: 320,
                height: 180,
                label: '${item.title}的地点照片',
              ),
            Semantics(
              label: '$type：${item.title}${detail == null ? '' : '，查看详情'}',
              child: ListTile(
                key: Key('agent-entity-${item.entity.mapID}'),
                contentPadding: EdgeInsets.zero,
                minTileHeight: 48,
                leading:
                    item.entity.type == 'person' && authorizationHeader != null
                    ? PersonAvatar(
                        metadata: item.avatar,
                        authorizationHeader: authorizationHeader!,
                        baseUrl: mediaBaseUrl,
                        client: mediaClient,
                        identityChanges: mediaIdentityChanges ?? sourceChanges,
                        current: () =>
                            sourceCurrent?.call() != false &&
                            mediaCurrent?.call() != false,
                        radius: 22,
                      )
                    : item.entity.type == 'community' &&
                          authorizationHeader != null &&
                          mediaClient != null
                    ? CommunityAvatar(
                        key: Key('agent-community-avatar-${item.entity.id}'),
                        metadata: item.communityAvatar,
                        fallbackLabel: item.title,
                        api: CommunityAvatarAPI(
                          client: mediaClient!,
                          baseUrl: mediaBaseUrl,
                          authorizationHeader: authorizationHeader!,
                        ),
                        identityChanges:
                            mediaIdentityChanges ??
                            sourceChanges ??
                            Listenable.merge(const []),
                        current: () =>
                            sourceCurrent?.call() != false &&
                            mediaCurrent?.call() != false,
                        radius: 22,
                      )
                    : Icon(icon, color: color.primary),
                title: Text(
                  item.title,
                  softWrap: true,
                  style: tokens.placeTitle,
                ),
                subtitle: Text(
                  [
                    type,
                    if (item.summary.isNotEmpty) item.summary,
                    ...activityDetails,
                    if (item.entity.type == 'person' && item.anchor != null)
                      '大致区域',
                    if (item.scope == 'SELF_PRIVATE') '仅自己可见',
                    if (item.entity.type == 'group') '待核实',
                  ].join(' · '),
                  style: tokens.listSubtitle,
                ),
                onTap: detail,
              ),
            ),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                if (item.entity.type == 'activity' && onReviewAgentJoin != null)
                  TextButton.icon(
                    key: Key('agent-join-review-${item.entity.mapID}'),
                    style: actionStyle,
                    onPressed: onReviewAgentJoin,
                    icon: const Icon(Icons.fact_check_outlined),
                    label: const Text('检查智能体报名提案'),
                  ),
                if (source != null)
                  TextButton.icon(
                    key: Key('agent-source-${item.entity.mapID}'),
                    style: actionStyle,
                    onPressed: !canOpenSource || sourceCurrent?.call() == false
                        ? null
                        : () => showAgentSourceDetails(
                            context,
                            source: AgentAnswerSource(
                              id: 'entity-${item.entity.mapID}',
                              title: source!.label.isEmpty
                                  ? '地点来源'
                                  : source!.label,
                              uri: sourceUri,
                              site: sourceUri.host,
                            ),
                            current: () => sourceCurrent?.call() ?? true,
                            changes: sourceChanges,
                            maintainer: source!.maintainer,
                            updatedAt:
                                source!.updatedAt?.toIso8601String() ?? '未提供',
                          ),
                    icon: const Icon(Icons.link, size: 18),
                    label: const Text('来源'),
                  ),
                if (item.anchor != null && onMap != null)
                  TextButton.icon(
                    style: actionStyle,
                    onPressed: onMap,
                    icon: const Icon(Icons.map_outlined),
                    label: const Text('地图查看'),
                  ),
                if (shared != null)
                  TextButton.icon(
                    style: actionStyle,
                    onPressed: allowed(EntityActionKind.share) ? shared : null,
                    icon: const Icon(Icons.ios_share_outlined),
                    label: Text(
                      item.scope == 'SELF_PRIVATE' ? '分享活动' : '分享给好友',
                    ),
                  ),
                if (onSave != null)
                  IconButton(
                    constraints: const BoxConstraints(
                      minWidth: 48,
                      minHeight: 48,
                    ),
                    tooltip: saved ? '取消收藏' : '收藏',
                    onPressed: saving || !allowed(EntityActionKind.save)
                        ? null
                        : onSave,
                    icon: Icon(saved ? Icons.bookmark : Icons.bookmark_outline),
                  ),
                if (onReminder != null)
                  IconButton(
                    tooltip: planned ? '移除个人提醒' : '添加个人提醒',
                    constraints: const BoxConstraints(
                      minWidth: 48,
                      minHeight: 48,
                    ),
                    onPressed: planning ? null : onReminder,
                    icon: const Icon(Icons.notifications_none_outlined),
                  ),
                if (onPlan != null)
                  IconButton(
                    tooltip: item.actions != null
                        ? '查看参加操作'
                        : planned
                        ? '移出计划'
                        : '加入计划',
                    constraints: const BoxConstraints(
                      minWidth: 48,
                      minHeight: 48,
                    ),
                    onPressed: planning || !allowed(EntityActionKind.join)
                        ? null
                        : onPlan,
                    icon: Icon(
                      planned
                          ? Icons.event_available
                          : Icons.event_note_outlined,
                    ),
                  ),
                if (onContact != null)
                  IconButton(
                    tooltip: item.actions != null ? '查看联系操作' : '申请联系',
                    constraints: const BoxConstraints(
                      minWidth: 48,
                      minHeight: 48,
                    ),
                    onPressed: allowed(EntityActionKind.connect)
                        ? onContact
                        : null,
                    icon: const Icon(Icons.person_add_alt_outlined),
                  ),
                if (onMessage != null)
                  IconButton(
                    tooltip: '查看好友会话',
                    constraints: const BoxConstraints(
                      minWidth: 48,
                      minHeight: 48,
                    ),
                    onPressed: allowed(EntityActionKind.message)
                        ? onMessage
                        : null,
                    icon: const Icon(Icons.chat_bubble_outline),
                  ),
                if (onNavigate != null)
                  IconButton(
                    tooltip: '查看导航',
                    constraints: const BoxConstraints(
                      minWidth: 48,
                      minHeight: 48,
                    ),
                    onPressed: allowed(EntityActionKind.navigate)
                        ? onNavigate
                        : null,
                    icon: const Icon(Icons.directions_outlined),
                  ),
              ],
            ),
            if (publicFieldEvidence?.hasItem(item) == true &&
                publicEvidenceCurrent != null &&
                publicEvidenceChanges != null)
              DefaultTextStyle(
                style: tokens.metadata,
                child: TextButtonTheme(
                  data: TextButtonThemeData(style: actionStyle),
                  child: PublicQueryFieldEvidencePanel(
                    evidence: publicFieldEvidence!,
                    item: item,
                    current: publicEvidenceCurrent!,
                    changes: publicEvidenceChanges!,
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}
