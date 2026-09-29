import 'package:flutter/material.dart';

class TopControls extends StatelessWidget {
  const TopControls({
    super.key,
    required this.onSidebar,
    required this.onInbox,
  });
  final VoidCallback onSidebar;
  final VoidCallback onInbox;

  @override
  Widget build(BuildContext context) => SafeArea(
    bottom: false,
    child: Padding(
      padding: const EdgeInsets.fromLTRB(16, 10, 16, 0),
      child: Row(
        children: [
          _button(Icons.menu_rounded, 'Open sidebar', onSidebar),
          const Spacer(),
          _button(Icons.inbox_outlined, 'Open inbox', onInbox),
        ],
      ),
    ),
  );

  Widget _button(IconData icon, String tooltip, VoidCallback onPressed) =>
      Material(
        color: const Color(0xFFFCFBF8),
        elevation: 4,
        shadowColor: Colors.black.withValues(alpha: 0.12),
        shape: const CircleBorder(),
        child: IconButton(
          icon: Icon(icon, color: const Color(0xFF193B32)),
          tooltip: tooltip,
          onPressed: onPressed,
        ),
      );
}
