import 'package:flutter/material.dart';

import 'agent_conversation.dart';
import 'agent_workspace_controller.dart';
import 'map_entities.dart';

class AgentResultSheet extends StatelessWidget {
  const AgentResultSheet({super.key, required this.workspace});
  final AgentWorkspaceController workspace;

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: workspace,
    builder: (context, _) {
      if (workspace.task == null) return const SizedBox.shrink();
      final extent = workspace.sheetExtent;
      final available = MediaQuery.sizeOf(context).height;
      final height = switch (extent) {
        AgentSheetExtent.compact => 104.0,
        AgentSheetExtent.half => (available * 0.43).clamp(250.0, 420.0),
        AgentSheetExtent.full => available * 0.78,
      };
      return AnimatedContainer(
        duration: const Duration(milliseconds: 260),
        curve: Curves.easeOutCubic,
        height: height,
        decoration: const BoxDecoration(
          color: Color(0xFFFCFBF8),
          borderRadius: BorderRadius.vertical(top: Radius.circular(26)),
          boxShadow: [
            BoxShadow(
              color: Color(0x20000000),
              blurRadius: 18,
              offset: Offset(0, -3),
            ),
          ],
        ),
        child: Column(
          children: [
            GestureDetector(
              behavior: HitTestBehavior.opaque,
              onVerticalDragEnd: (detail) {
                final up = (detail.primaryVelocity ?? 0) < 0;
                workspace.setSheetExtent(
                  up
                      ? (extent == AgentSheetExtent.compact
                            ? AgentSheetExtent.half
                            : AgentSheetExtent.full)
                      : (extent == AgentSheetExtent.full
                            ? AgentSheetExtent.half
                            : AgentSheetExtent.compact),
                );
              },
              onTap: () => workspace.setSheetExtent(
                extent == AgentSheetExtent.compact
                    ? AgentSheetExtent.half
                    : AgentSheetExtent.compact,
              ),
              child: Padding(
                padding: const EdgeInsets.fromLTRB(20, 8, 20, 9),
                child: Column(
                  children: [
                    Container(
                      width: 36,
                      height: 4,
                      decoration: BoxDecoration(
                        color: const Color(0xFFC5CCC3),
                        borderRadius: BorderRadius.circular(4),
                      ),
                    ),
                    const SizedBox(height: 12),
                    Row(
                      children: [
                        Expanded(
                          child: Text(
                            workspace.task!.query,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(
                              fontSize: 16,
                              fontWeight: FontWeight.w700,
                              color: Color(0xFF193B32),
                            ),
                          ),
                        ),
                        IconButton(
                          tooltip: extent == AgentSheetExtent.full
                              ? 'Show results'
                              : 'Open conversation',
                          onPressed: () => workspace.setSheetExtent(
                            extent == AgentSheetExtent.full
                                ? AgentSheetExtent.half
                                : AgentSheetExtent.full,
                          ),
                          icon: Icon(
                            extent == AgentSheetExtent.full
                                ? Icons.expand_more
                                : Icons.chat_bubble_outline,
                            size: 21,
                          ),
                        ),
                      ],
                    ),
                    Align(
                      alignment: Alignment.centerLeft,
                      child: Text(
                        workspace.state == AgentViewState.searching
                            ? 'Looking around…'
                            : '${workspace.result?.activities.length ?? 0} published activities · ${workspace.result?.entities.length ?? 0} demo map entities',
                        style: const TextStyle(
                          color: Color(0xFF747B73),
                          fontSize: 12,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ),
            if (extent != AgentSheetExtent.compact)
              Expanded(
                child: extent == AgentSheetExtent.full
                    ? AgentConversation(workspace: workspace)
                    : _ResultList(workspace: workspace),
              ),
          ],
        ),
      );
    },
  );
}

class _ResultList extends StatelessWidget {
  const _ResultList({required this.workspace});
  final AgentWorkspaceController workspace;

  @override
  Widget build(BuildContext context) {
    final result = workspace.result;
    if (result == null) return const Center(child: CircularProgressIndicator());
    final activities = result.entities.where(
      (entity) => entity.kind == MapEntityKind.activity,
    );
    final people = result.entities.where(
      (entity) =>
          entity.kind == MapEntityKind.person ||
          entity.kind == MapEntityKind.peopleCluster,
    );
    final groups = result.entities.where(
      (entity) => entity.kind == MapEntityKind.group,
    );
    return ListView(
      padding: const EdgeInsets.fromLTRB(20, 0, 20, 20),
      children: [
        Text(
          result.note,
          style: const TextStyle(color: Color(0xFF747B73), fontSize: 12),
        ),
        if (result.activities.isNotEmpty || activities.isNotEmpty) ...[
          const _Heading('Activities'),
          for (final activity in result.activities)
            _Row(
              icon: Icons.event_outlined,
              title: activity.title,
              subtitle:
                  '${activity.status} · Published City API · ${activity.source.label}',
            ),
          for (final entity in activities)
            _Row(
              icon: Icons.sports_tennis,
              title: entity.title,
              subtitle: 'Local demo · map preview',
              onTap: () => workspace.selectEntity(entity.id),
            ),
        ],
        if (people.isNotEmpty) ...[
          const _Heading('People'),
          for (final entity in people)
            _Row(
              icon: Icons.person_outline,
              title: entity.title,
              subtitle: 'Local demo · no live profile',
              onTap: () => workspace.selectEntity(entity.id),
            ),
        ],
        if (groups.isNotEmpty) ...[
          const _Heading('Groups'),
          for (final entity in groups)
            _Row(
              icon: Icons.group_outlined,
              title: entity.title,
              subtitle: 'Local demo · no live group',
              onTap: () => workspace.selectEntity(entity.id),
            ),
        ],
        if (result.places.isNotEmpty) ...[
          const _Heading('Places'),
          for (final place in result.places)
            _Row(
              icon: Icons.place_outlined,
              title: place.name,
              subtitle: 'Published City API · ${place.source.label}',
            ),
        ],
        if (result.entities.isEmpty &&
            result.activities.isEmpty &&
            result.places.isEmpty)
          const Padding(
            padding: EdgeInsets.only(top: 24),
            child: Text(
              'Try “Find someone to play badminton this weekend” to see the local demo.',
              style: TextStyle(color: Color(0xFF747B73)),
            ),
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
    child: Text(
      label,
      style: const TextStyle(
        fontSize: 14,
        fontWeight: FontWeight.w700,
        color: Color(0xFF193B32),
      ),
    ),
  );
}

class _Row extends StatelessWidget {
  const _Row({
    required this.icon,
    required this.title,
    required this.subtitle,
    this.onTap,
  });
  final IconData icon;
  final String title;
  final String subtitle;
  final VoidCallback? onTap;
  @override
  Widget build(BuildContext context) => Material(
    color: Colors.transparent,
    child: ListTile(
      dense: true,
      contentPadding: EdgeInsets.zero,
      onTap: onTap,
      leading: Icon(icon, color: const Color(0xFF193B32)),
      title: Text(title, style: const TextStyle(fontWeight: FontWeight.w600)),
      subtitle: Text(subtitle, maxLines: 2, overflow: TextOverflow.ellipsis),
      trailing: onTap == null
          ? null
          : const Icon(Icons.arrow_outward, size: 16),
    ),
  );
}
