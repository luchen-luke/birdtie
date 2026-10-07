import 'package:flutter/material.dart';

import '../city/map_link.dart';
import 'agent_workspace_controller.dart';

class AgentConversation extends StatelessWidget {
  const AgentConversation({super.key, required this.workspace});
  final AgentWorkspaceController workspace;

  @override
  Widget build(BuildContext context) => ListView(
    padding: const EdgeInsets.fromLTRB(20, 8, 20, 20),
    children: [
      for (final query in workspace.conversation) ...[
        Align(
          alignment: Alignment.centerRight,
          child: Container(
            constraints: const BoxConstraints(maxWidth: 290),
            padding: const EdgeInsets.symmetric(horizontal: 15, vertical: 11),
            decoration: BoxDecoration(
              color: const Color(0xFFE7EDE2),
              borderRadius: BorderRadius.circular(19),
            ),
            child: Text(query),
          ),
        ),
        const SizedBox(height: 18),
      ],
      Text(
        workspace.result?.taskID == null
            ? 'Birdtie local context'
            : 'Birdtie search',
        style: TextStyle(fontWeight: FontWeight.w700, color: Color(0xFF193B32)),
      ),
      const SizedBox(height: 8),
      Text(
        workspace.result?.note ?? 'Looking for local context…',
        style: const TextStyle(height: 1.5),
      ),
      for (final activity in workspace.result?.activities ?? const []) ...[
        const SizedBox(height: 12),
        Text(
          activity.title,
          style: const TextStyle(fontWeight: FontWeight.w600),
        ),
        Text(
          '${activity.schedule} · ${activity.placeName} · ${activity.source.label}',
          style: const TextStyle(fontSize: 12, color: Color(0xFF747B73)),
        ),
        if (activity.source.reference.startsWith('http'))
          TextButton.icon(
            onPressed: () => openExternalSource(activity.source.reference),
            icon: const Icon(Icons.open_in_new, size: 16),
            label: Text('Open source · ${activity.source.label}'),
          ),
      ],
      for (final place in workspace.result?.places ?? const []) ...[
        const SizedBox(height: 12),
        Text(place.name, style: const TextStyle(fontWeight: FontWeight.w600)),
        Text(
          '${place.summary} · ${place.source.label} · ${place.source.freshness}',
          style: const TextStyle(fontSize: 12, color: Color(0xFF747B73)),
        ),
        if (place.source.reference.startsWith('http'))
          TextButton.icon(
            onPressed: () => openExternalSource(place.source.reference),
            icon: const Icon(Icons.open_in_new, size: 16),
            label: Text('Open source · ${place.source.label}'),
          ),
      ],
      const SizedBox(height: 14),
      Text(
        workspace.task?.intent == 'FIND_ACTIVITY'
            ? 'This conversation stays with your activity search and map context.'
            : 'Ask about published Birdtie activities to start a task.',
        style: const TextStyle(color: Color(0xFF747B73), fontSize: 12),
      ),
    ],
  );
}
