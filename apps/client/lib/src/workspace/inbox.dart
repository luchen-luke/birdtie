import 'dart:async';

import 'package:flutter/material.dart';

import '../auth/birdtie_auth_controller.dart';
import 'remote_inbox_source.dart';
import 'connections.dart';

class InboxPanel extends StatefulWidget {
  const InboxPanel({super.key, required this.auth, this.source});
  final BirdtieAuthController auth;
  final RemoteInboxSource? source;

  @override
  State<InboxPanel> createState() => _InboxPanelState();
}

class _InboxPanelState extends State<InboxPanel> {
  late final RemoteInboxSource _source;
  List<InboxItem> _items = const [];
  bool _loading = false;
  bool _failed = false;
  bool _signedIn = false;
  int _serial = 0;

  @override
  void initState() {
    super.initState();
    _source =
        widget.source ??
        RemoteInboxSource(
          authorizationHeader: () => widget.auth.authorizationHeader,
        );
    _signedIn = widget.auth.signedIn;
    widget.auth.addListener(_onAuthChanged);
    if (_signedIn && RemoteInboxSource.apiBase.isNotEmpty) unawaited(_load());
  }

  void _onAuthChanged() {
    if (_signedIn == widget.auth.signedIn) return;
    _signedIn = widget.auth.signedIn;
    if (!_signedIn) {
      ++_serial;
      setState(() {
        _items = const [];
        _loading = false;
        _failed = false;
      });
    } else if (RemoteInboxSource.apiBase.isNotEmpty) {
      unawaited(_load());
    }
  }

  Future<void> _load() async {
    final serial = ++_serial;
    setState(() {
      _loading = true;
      _failed = false;
    });
    try {
      final items = await _source.load();
      if (!mounted || serial != _serial) return;
      setState(() => _items = items);
    } catch (_) {
      if (!mounted || serial != _serial) return;
      setState(() => _failed = true);
    } finally {
      if (mounted && serial == _serial) setState(() => _loading = false);
    }
  }

  Future<void> _markRead(InboxItem item) async {
    if (item.readAt != null) return;
    final serial = _serial;
    try {
      final updated = await _source.markRead(item.id);
      if (!mounted || serial != _serial || !widget.auth.signedIn) return;
      setState(() {
        _items = [
          for (final current in _items)
            current.id == item.id ? updated : current,
        ];
      });
    } catch (_) {
      if (!mounted || serial != _serial || !widget.auth.signedIn) return;
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Could not mark this update as read.')),
      );
    }
  }

  @override
  void dispose() {
    ++_serial;
    widget.auth.removeListener(_onAuthChanged);
    if (widget.source == null) _source.dispose();
    super.dispose();
  }

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
          Text(
            RemoteInboxSource.apiBase.isEmpty
                ? 'Action center · local preview'
                : 'Action center · your account',
            style: const TextStyle(color: Color(0xFF747B73), fontSize: 12),
          ),
          const SizedBox(height: 16),
          Expanded(child: _body()),
        ],
      ),
    ),
  );

  Widget _body() {
    if (RemoteInboxSource.apiBase.isEmpty) return const _DemoInbox();
    if (!widget.auth.signedIn) {
      return const _InboxMessage('Sign in to see your Birdtie updates.');
    }
    if (_loading) return const Center(child: CircularProgressIndicator());
    if (_failed) {
      return Center(
        child: TextButton(
          onPressed: _load,
          child: const Text('Inbox unavailable. Tap to retry.'),
        ),
      );
    }
    if (_items.isEmpty) {
      return ListView(
        children: [
          SocialInboxSection(auth: widget.auth),
          const _InboxMessage(
            'Place and Activity review results will appear here.',
          ),
        ],
      );
    }
    const sections = [
      ('needs_attention', 'Needs attention'),
      ('messages', 'Messages'),
      ('requests', 'Requests'),
      ('agent_updates', 'Agent Updates'),
      ('updates', 'Updates'),
    ];
    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        children: [
          SocialInboxSection(auth: widget.auth),
          for (final (key, label) in sections)
            if (_items.any((item) => item.category == key)) ...[
              _SectionHeading(label),
              for (final item in _items.where((item) => item.category == key))
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  onTap: () => _markRead(item),
                  leading: CircleAvatar(
                    backgroundColor: const Color(0xFFE7EDE2),
                    child: Icon(
                      item.resourceType == 'connection_request'
                          ? Icons.person_add_alt_outlined
                          : item.resourceType == 'conversation_message'
                          ? Icons.chat_bubble_outline
                          : item.resourceType == 'community'
                          ? Icons.group_outlined
                          : item.resourceType == 'activity_candidate'
                          ? Icons.event_outlined
                          : Icons.place_outlined,
                      color: const Color(0xFF193B32),
                      size: 20,
                    ),
                  ),
                  title: Text(
                    item.title,
                    style: TextStyle(
                      fontWeight: item.readAt == null
                          ? FontWeight.w700
                          : FontWeight.w500,
                    ),
                  ),
                  subtitle: Text(item.detail),
                  trailing: item.readAt == null
                      ? const Icon(
                          Icons.circle,
                          size: 8,
                          color: Color(0xFF497966),
                        )
                      : null,
                ),
              const Divider(height: 20),
            ],
        ],
      ),
    );
  }
}

class _InboxMessage extends StatelessWidget {
  const _InboxMessage(this.message);
  final String message;
  @override
  Widget build(BuildContext context) => Center(
    child: Padding(
      padding: const EdgeInsets.all(24),
      child: Text(
        message,
        textAlign: TextAlign.center,
        style: const TextStyle(color: Color(0xFF747B73)),
      ),
    ),
  );
}

class _SectionHeading extends StatelessWidget {
  const _SectionHeading(this.label);
  final String label;
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.fromLTRB(0, 15, 0, 8),
    child: Text(
      label,
      style: const TextStyle(
        fontSize: 15,
        fontWeight: FontWeight.w700,
        color: Color(0xFF193B32),
      ),
    ),
  );
}

class _DemoInbox extends StatelessWidget {
  const _DemoInbox();
  @override
  Widget build(BuildContext context) => ListView(
    children: const [
      _SectionHeading('Needs attention'),
      ListTile(
        title: Text('Anna · local preview'),
        subtitle: Text('Wants to join your badminton activity'),
      ),
      ListTile(
        title: Text('Kevin · local preview'),
        subtitle: Text('Are you still going tonight?'),
      ),
      _SectionHeading('Agent Updates'),
      ListTile(
        title: Text('Birdtie · local preview'),
        subtitle: Text('3 new people match your badminton task'),
      ),
    ],
  );
}
