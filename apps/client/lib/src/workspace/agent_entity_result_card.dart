import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';
import '../city/public_city_controller.dart' show PublicSource;
import 'agent_result_projection.dart';
import 'entity_action_contract.dart';
import 'public_query_field_evidence.dart';
import 'public_query_field_evidence_panel.dart';

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
    this.onReminder,
    this.onContact,
    this.onMessage,
    this.onNavigate,
    this.onMap,
    this.source,
    this.sourceCurrent,
    this.saved = false,
    this.saving = false,
    this.planned = false,
    this.planning = false,
    this.publicFieldEvidence,
    this.publicEvidenceCurrent,
    this.publicEvidenceChanges,
  });
  final AgentResultItem item;
  final ValueChanged<AgentResultItem>? onOpen, onShare;
  final VoidCallback? onSave,
      onPlan,
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
  final Listenable? publicEvidenceChanges;
  @override
  Widget build(BuildContext context) {
    if (!item.valid) return const SizedBox.shrink();
    final color = Theme.of(context).colorScheme;
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
            Semantics(
              label: '$type：${item.title}',
              child: ListTile(
                key: Key('agent-entity-${item.entity.mapID}'),
                contentPadding: EdgeInsets.zero,
                minTileHeight: 48,
                leading: Icon(icon, color: color.primary),
                title: Text(
                  item.title,
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                subtitle: Text(
                  [
                    type,
                    if (item.summary.isNotEmpty) item.summary,
                    if (item.entity.type == 'person')
                      item.anchor == null ? '未显示地图标记' : '公开的大致区域',
                    if (item.scope == 'SELF_PRIVATE') '仅你可见；分享只包含原活动',
                    if (item.entity.type == 'group') '来源类型未核实，暂无详情或分享',
                  ].join(' · '),
                ),
                onTap: detail,
              ),
            ),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                if (source != null)
                  TextButton.icon(
                    key: Key('agent-source-${item.entity.mapID}'),
                    onPressed: !canOpenSource
                        ? null
                        : () async {
                            if (sourceCurrent?.call() == false) return;
                            try {
                              final opened = await launchUrl(
                                sourceUri,
                                mode: LaunchMode.externalApplication,
                              );
                              if (!opened && context.mounted) {
                                ScaffoldMessenger.of(context).showSnackBar(
                                  const SnackBar(content: Text('暂时无法打开来源链接。')),
                                );
                              }
                            } catch (_) {
                              if (context.mounted) {
                                ScaffoldMessenger.of(context).showSnackBar(
                                  const SnackBar(content: Text('暂时无法打开来源链接。')),
                                );
                              }
                            }
                          },
                    icon: const Icon(Icons.link, size: 18),
                    label: Text(
                      '来源：${source!.label}${canOpenSource ? ' · ${sourceUri.host}' : ''}',
                    ),
                  ),
                if (item.anchor != null && onMap != null)
                  TextButton.icon(
                    onPressed: onMap,
                    icon: const Icon(Icons.map_outlined),
                    label: const Text('地图查看'),
                  ),
                if (detail != null)
                  TextButton.icon(
                    style: TextButton.styleFrom(
                      minimumSize: const Size(48, 48),
                    ),
                    onPressed: detail,
                    icon: const Icon(Icons.open_in_new),
                    label: const Text('查看详情'),
                  ),
                if (shared != null)
                  TextButton.icon(
                    style: TextButton.styleFrom(
                      minimumSize: const Size(48, 48),
                    ),
                    onPressed: allowed(EntityActionKind.share) ? shared : null,
                    icon: const Icon(Icons.ios_share_outlined),
                    label: Text(
                      item.scope == 'SELF_PRIVATE' ? '分享活动' : '分享给好友',
                    ),
                  ),
                if (onSave != null)
                  IconButton(
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
                    onPressed: allowed(EntityActionKind.connect)
                        ? onContact
                        : null,
                    icon: const Icon(Icons.person_add_alt_outlined),
                  ),
                if (onMessage != null)
                  IconButton(
                    tooltip: '查看好友会话',
                    onPressed: allowed(EntityActionKind.message)
                        ? onMessage
                        : null,
                    icon: const Icon(Icons.chat_bubble_outline),
                  ),
                if (onNavigate != null)
                  IconButton(
                    tooltip: '查看导航',
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
              PublicQueryFieldEvidencePanel(
                evidence: publicFieldEvidence!,
                item: item,
                current: publicEvidenceCurrent!,
                changes: publicEvidenceChanges!,
              ),
          ],
        ),
      ),
    );
  }
}
