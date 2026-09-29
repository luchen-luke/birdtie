import 'package:flutter/material.dart';

import 'agent_conversation.dart';
import 'agent_workspace_controller.dart';
import 'activity_plans.dart';
import 'saved_items.dart';

class AgentResultSheet extends StatelessWidget {
  const AgentResultSheet({
    super.key,
    required this.workspace,
    this.saved,
    this.plans,
    this.onContact,
  });
  final AgentWorkspaceController workspace;
  final SavedController? saved;
  final ActivityPlansController? plans;
  final ValueChanged<AgentPerson>? onContact;

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: Listenable.merge([workspace, ?saved, ?plans]),
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
                            : '${workspace.result?.activities.length ?? 0} activities · ${workspace.result?.people.length ?? 0} people · ${workspace.result?.groups.length ?? 0} groups',
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
                    : _ResultList(
                        workspace: workspace,
                        saved: saved,
                        plans: plans,
                        onContact: onContact,
                      ),
              ),
          ],
        ),
      );
    },
  );
}

class _ResultList extends StatelessWidget {
  const _ResultList({
    required this.workspace,
    required this.saved,
    required this.plans,
    required this.onContact,
  });
  final AgentWorkspaceController workspace;
  final SavedController? saved;
  final ActivityPlansController? plans;
  final ValueChanged<AgentPerson>? onContact;

  Future<void> _toggleSaved(
    BuildContext context,
    String kind,
    String targetId,
  ) async {
    try {
      await saved!.toggle(kind, targetId);
    } catch (_) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              saved!.authorizationHeader() == null
                  ? 'Sign in to save Birdtie items.'
                  : 'Could not update Saved. Please try again.',
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
                  ? 'Sign in to plan an Activity.'
                  : 'Could not update My Activities. Please try again.',
            ),
          ),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final result = workspace.result;
    if (result == null) return const Center(child: CircularProgressIndicator());
    return ListView(
      padding: const EdgeInsets.fromLTRB(20, 0, 20, 20),
      children: [
        Text(
          result.note,
          style: const TextStyle(color: Color(0xFF747B73), fontSize: 12),
        ),
        if (result.activities.isNotEmpty) ...[
          const _Heading('Activities'),
          for (final activity in result.activities)
            _Row(
              icon: Icons.event_outlined,
              title: activity.title,
              subtitle: [
                if (activity.schedule.isNotEmpty) activity.schedule,
                if (activity.placeName.isNotEmpty) activity.placeName,
                activity.status,
                activity.source.label,
              ].join(' · '),
              onTap: activity.location?.hasPublicPoint == true
                  ? () => workspace.selectEntity('activity:${activity.id}')
                  : null,
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
            ),
        ],
        if (result.people.isNotEmpty) ...[
          const _Heading('People'),
          for (final person in result.people)
            _Row(
              icon: Icons.person_outline,
              title: person.displayName,
              subtitle:
                  '${person.topic} · ${person.areaLabel} (approximate area)${person.mapLatitude == null || person.mapLongitude == null ? ' · no map marker' : ' · public area marker'}',
              onTap: person.mapLatitude == null || person.mapLongitude == null
                  ? null
                  : () => workspace.selectEntity('person:${person.accountID}'),
              onContact: onContact == null ? null : () => onContact!(person),
            ),
        ],
        if (result.groups.isNotEmpty) ...[
          const _Heading('Groups'),
          for (final group in result.groups)
            _Row(
              icon: Icons.group_outlined,
              title: group.name,
              subtitle: '${group.summary} · published group',
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
        if (result.places.isNotEmpty) ...[
          const _Heading('Places'),
          for (final place in result.places)
            _Row(
              icon: Icons.place_outlined,
              title: place.name,
              subtitle: 'Published City API · ${place.source.label}',
              onTap: place.location.hasPublicPoint
                  ? () => workspace.selectEntity('place:${place.id}')
                  : null,
              saved: saved?.contains('place', place.id) ?? false,
              saving: saved?.isBusy('place', place.id) ?? false,
              onSave: saved == null
                  ? null
                  : () => _toggleSaved(context, 'place', place.id),
            ),
        ],
        if (result.entities.isEmpty &&
            result.activities.isEmpty &&
            result.people.isEmpty &&
            result.groups.isEmpty &&
            result.places.isEmpty)
          Padding(
            padding: EdgeInsets.only(top: 24),
            child: Text(
              'No matching public activities were found. Try another time or check back as more city data is added.',
              style: const TextStyle(color: Color(0xFF747B73)),
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
    child: ListTile(
      dense: true,
      contentPadding: EdgeInsets.zero,
      onTap: onTap,
      leading: Icon(icon, color: const Color(0xFF193B32)),
      title: Text(title, style: const TextStyle(fontWeight: FontWeight.w600)),
      subtitle: Text(subtitle, maxLines: 2, overflow: TextOverflow.ellipsis),
      trailing: onSave == null && onPlan == null && onContact == null
          ? (onTap == null ? null : const Icon(Icons.arrow_outward, size: 16))
          : Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                if (onContact != null)
                  IconButton(
                    tooltip: 'Request contact',
                    onPressed: onContact,
                    icon: const Icon(Icons.person_add_alt_outlined),
                  ),
                if (onPlan != null)
                  IconButton(
                    tooltip: planned
                        ? 'Remove activity plan'
                        : 'Plan to attend',
                    onPressed: planning ? null : onPlan,
                    icon: Icon(
                      planned
                          ? Icons.event_available
                          : Icons.event_available_outlined,
                    ),
                  ),
                if (onSave != null)
                  IconButton(
                    tooltip: saved ? 'Remove from Saved' : 'Save item',
                    onPressed: saving ? null : onSave,
                    icon: Icon(saved ? Icons.bookmark : Icons.bookmark_outline),
                  ),
              ],
            ),
    ),
  );
}
