import 'package:flutter/material.dart';

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
      const Text(
        'Birdtie preview',
        style: TextStyle(fontWeight: FontWeight.w700, color: Color(0xFF193B32)),
      ),
      const SizedBox(height: 8),
      Text(
        workspace.result?.note ?? 'Looking for local context…',
        style: const TextStyle(height: 1.5),
      ),
      const SizedBox(height: 14),
      const Text(
        'This conversation uses local task state. Live Agent replies are not connected.',
        style: TextStyle(color: Color(0xFF747B73), fontSize: 12),
      ),
    ],
  );
}
