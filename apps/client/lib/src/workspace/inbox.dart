import 'package:flutter/material.dart';

class InboxPanel extends StatelessWidget {
  const InboxPanel({super.key});

  @override
  Widget build(BuildContext context) => SafeArea(
    top: false,
    child: Padding(
      padding: const EdgeInsets.fromLTRB(22, 12, 22, 24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Center(
            child: Container(
              width: 38,
              height: 4,
              decoration: BoxDecoration(
                color: const Color(0xFFC5CCC3),
                borderRadius: BorderRadius.circular(3),
              ),
            ),
          ),
          const SizedBox(height: 22),
          const Text(
            'Inbox',
            style: TextStyle(
              fontSize: 28,
              fontWeight: FontWeight.w700,
              color: Color(0xFF193B32),
            ),
          ),
          const Text(
            'Action center · local preview',
            style: TextStyle(color: Color(0xFF747B73), fontSize: 12),
          ),
          const SizedBox(height: 16),
          Expanded(
            child: ListView(
              children: const [
                _Group('Needs attention', [
                  _InboxItem(
                    'Anna',
                    'Wants to join your badminton activity',
                    Icons.person_add_alt_1_outlined,
                  ),
                  _InboxItem(
                    'Kevin',
                    'Are you still going tonight?',
                    Icons.chat_bubble_outline,
                  ),
                ]),
                _Group('Messages', [
                  _InboxItem(
                    'Conversation preview',
                    'Messages will appear here when connected.',
                    Icons.mail_outline,
                  ),
                ]),
                _Group('Requests', [
                  _InboxItem(
                    'Connection preview',
                    'Requests will appear here when connected.',
                    Icons.handshake_outlined,
                  ),
                ]),
                _Group('Agent Updates', [
                  _InboxItem(
                    'Birdtie',
                    '3 new people match your badminton task',
                    Icons.auto_awesome_outlined,
                  ),
                ]),
                _Group('Updates', [
                  _InboxItem(
                    'Saturday Badminton',
                    'Changed to 16:00',
                    Icons.event_outlined,
                  ),
                ]),
              ],
            ),
          ),
        ],
      ),
    ),
  );
}

class _Group extends StatelessWidget {
  const _Group(this.title, this.items);
  final String title;
  final List<_InboxItem> items;
  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Padding(
        padding: const EdgeInsets.fromLTRB(0, 15, 0, 8),
        child: Text(
          title,
          style: const TextStyle(
            fontSize: 15,
            fontWeight: FontWeight.w700,
            color: Color(0xFF193B32),
          ),
        ),
      ),
      for (final item in items) item,
      const Divider(height: 20),
    ],
  );
}

class _InboxItem extends StatelessWidget {
  const _InboxItem(this.title, this.detail, this.icon);
  final String title;
  final String detail;
  final IconData icon;
  @override
  Widget build(BuildContext context) => ListTile(
    contentPadding: EdgeInsets.zero,
    leading: CircleAvatar(
      backgroundColor: const Color(0xFFE7EDE2),
      child: Icon(icon, color: const Color(0xFF193B32), size: 20),
    ),
    title: Text(title, style: const TextStyle(fontWeight: FontWeight.w600)),
    subtitle: Text(detail),
  );
}
