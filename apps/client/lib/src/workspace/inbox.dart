import 'dart:async';

import 'package:flutter/material.dart';

import '../auth/birdtie_auth_controller.dart';
import 'remote_inbox_source.dart';
import 'connections.dart';
import 'notification_policy_page.dart';
import 'organization_membership_pages.dart';
import 'community_api.dart';
import 'community_page.dart';
import 'community_conversation_page.dart';
import 'remote_agent_task_source.dart';
import 'agent_task_detail_page.dart';
import 'notification_destination_router.dart';
import 'business_api.dart';
import 'business_claim_notification_page.dart';

class InboxPanel extends StatefulWidget {
  const InboxPanel({
    super.key,
    required this.auth,
    this.source,
    this.onOpenActivity,
    this.onOpenConversation,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.onOrganizationAccepted,
  });
  final BirdtieAuthController auth;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final Future<void> Function()? onOrganizationAccepted;
  final RemoteInboxSource? source;
  final ValueChanged<String>? onOpenActivity;
  final ValueChanged<String>? onOpenConversation;

  @override
  State<InboxPanel> createState() => _InboxPanelState();
}

class _InboxPanelState extends State<InboxPanel> {
  late RemoteInboxSource _source;
  late bool _ownsSource;
  final ScrollController _scroll = ScrollController();
  List<InboxItem> _items = const [];
  bool _loading = false;
  bool _failed = false;
  (String?, String?, String?)? _identity;
  String? _openingID;
  Route<void>? _child;
  bool get _personal =>
      widget.auth.signedIn && widget.organizationWorkspaceID?.call() == null;
  (String?, String?, String?) get _currentIdentity => (
    widget.auth.authorizationHeader,
    widget.auth.accountID,
    widget.organizationWorkspaceID?.call(),
  );
  int _serial = 0;
  int _identityEpoch = 0;
  final _destinationChanges = ValueNotifier<int>(0);

  @override
  void initState() {
    super.initState();
    _ownsSource = widget.source == null;
    _source =
        widget.source ??
        RemoteInboxSource(
          authorizationHeader: () => widget.auth.authorizationHeader,
        );
    _identity = _currentIdentity;
    widget.auth.addListener(_onAuthChanged);
    widget.workspaceChanges?.addListener(_onAuthChanged);
    if (_personal && _configured) unawaited(_load());
  }

  bool get _configured =>
      widget.source != null || RemoteInboxSource.apiBase.isNotEmpty;

  void _onAuthChanged() {
    _refreshIdentity();
  }

  void _refreshIdentity({bool force = false}) {
    if (!force && _identity == _currentIdentity) {
      return;
    }
    _identity = _currentIdentity;
    ++_serial;
    ++_identityEpoch;
    _destinationChanges.value++;
    final child = _child;
    _child = null;
    if (child?.isActive ?? false) {
      Navigator.of(context).removeRoute(child!);
    }
    setState(() {
      _items = const [];
      _openingID = null;
      _loading = false;
      _failed = false;
    });
    if (_personal && _configured) {
      unawaited(_load());
    }
  }

  @override
  void didUpdateWidget(covariant InboxPanel oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.auth != widget.auth) {
      oldWidget.auth.removeListener(_onAuthChanged);
      widget.auth.addListener(_onAuthChanged);
    }
    if (oldWidget.workspaceChanges != widget.workspaceChanges) {
      oldWidget.workspaceChanges?.removeListener(_onAuthChanged);
      widget.workspaceChanges?.addListener(_onAuthChanged);
    }
    final sourceChanged = oldWidget.source != widget.source;
    if (sourceChanged) {
      if (_ownsSource) _source.dispose();
      _ownsSource = widget.source == null;
      _source =
          widget.source ??
          RemoteInboxSource(
            authorizationHeader: () => widget.auth.authorizationHeader,
          );
    }
    _refreshIdentity(force: sourceChanged);
  }

  bool get _sourceCurrent =>
      _source.authorizationHeader() == widget.auth.authorizationHeader;

  bool _current(int serial) =>
      mounted &&
      serial == _serial &&
      _personal &&
      _sourceCurrent &&
      _identity == _currentIdentity;
  Future<void> _load() async {
    if (!_personal) {
      return;
    }
    if (!_sourceCurrent) {
      setState(() {
        _items = const [];
        _loading = false;
        _failed = true;
      });
      return;
    }
    final serial = ++_serial;
    setState(() {
      _loading = true;
      _failed = false;
    });
    try {
      final items = await _source.load();
      if (!_current(serial)) return;
      setState(() => _items = items);
    } catch (_) {
      if (!_current(serial)) return;
      setState(() => _failed = true);
    } finally {
      if (_current(serial)) setState(() => _loading = false);
    }
  }

  Future<InboxItem?> _markRead(InboxItem item) async {
    if (_openingID != null || !_personal || !_sourceCurrent) {
      return null;
    }
    final serial = _serial;
    setState(() => _openingID = item.id);
    try {
      // Even already-read items require a fresh authoritative permission check.
      final current = await _source.markRead(item.id);
      if (!_current(serial) || current.id != item.id) {
        return null;
      }
      setState(
        () => _items = [
          for (final old in _items) old.id == item.id ? current : old,
        ],
      );
      return current;
    } catch (_) {
      if (mounted && _current(serial)) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('当前通知暂不可读取，请刷新后重试。')));
      }
      return null;
    } finally {
      if (_current(serial)) {
        setState(() => _openingID = null);
      }
    }
  }

  Future<void> _open(InboxItem item) async {
    final serial = _serial, current = await _markRead(item);
    if (!mounted || current == null || !_current(serial)) {
      return;
    }
    if (current.targetActivityID != null) {
      widget.onOpenActivity?.call(current.targetActivityID!);
    } else if (current.targetConversationID != null) {
      widget.onOpenConversation?.call(current.targetConversationID!);
    } else if (current.targetCommunityID != null ||
        current.targetTaskID != null ||
        current.targetBusinessID != null) {
      setState(() => _openingID = current.id);
      try {
        await _openDestination(current);
      } finally {
        if (_current(serial)) setState(() => _openingID = null);
      }
    } else if (current.resourceType == 'connection_request') {
      await _scroll.animateTo(
        0,
        duration: const Duration(milliseconds: 250),
        curve: Curves.easeOut,
      );
    } else if (current.resourceType == 'organization_membership') {
      final route = MaterialPageRoute<void>(
        builder: (_) => OrganizationInvitationsPage(
          auth: widget.auth,
          onAccepted: widget.onOrganizationAccepted ?? () async {},
        ),
      );
      _child = route;
      await Navigator.of(context).push(route);
      if (_child == route) {
        _child = null;
      }
      if (_current(serial)) {
        await _load();
      }
    }
  }

  Future<void> _openDestination(InboxItem item) async {
    final identity = _currentIdentity, epoch = _identityEpoch;
    bool valid() =>
        mounted &&
        _personal &&
        _sourceCurrent &&
        _currentIdentity == identity &&
        epoch == _identityEpoch;
    String? token() => valid() ? identity.$1 : null;
    if (!valid() || identity.$2 == null) return;
    CommunityApi? community;
    RemoteAgentTaskSource? task;
    try {
      Widget child;
      if (item.targetCommunityID case final id?) {
        community = CommunityApi(
          authorizationHeader: token,
          apiBaseUrl: _source.followApiBaseUrl,
          client: _source.followClient,
        );
        // A message decision ID is never used as a Community ID. Current
        // detail and conversation handlers independently recheck membership.
        final detail = await community.detail(id);
        if (!valid()) return;
        child = item.resourceType == 'community_message'
            ? CommunityConversationPage(
                communityID: id,
                communityTitle: detail.name,
                authorizationHeader: token,
                apiBaseUrl: _source.followApiBaseUrl,
                client: _source.followClient,
              )
            : CommunityDetailPage(id: id, api: community);
      } else if (item.targetTaskID case final id?) {
        task = RemoteAgentTaskSource(
          cityID: () => null,
          authorizationHeader: token,
          apiBaseUrl: _source.followApiBaseUrl,
          client: _source.followClient,
        );
        child = AgentTaskDetailPage(
          taskID: id,
          ownerID: identity.$2!,
          source: task,
        );
      } else if (item.targetBusinessID case final id?) {
        child = BusinessClaimNotificationPage(
          businessID: id,
          ownerID: identity.$2!,
          authorizationHeader: token,
          isCurrent: valid,
          api: BusinessApi(
            authorizationHeader: token,
            apiBaseUrl: _source.followApiBaseUrl,
            client: _source.followClient,
          ),
        );
      } else {
        return;
      }
      if (!mounted || !valid()) return;
      final changes = Listenable.merge([
        widget.auth,
        widget.workspaceChanges,
        _destinationChanges,
      ]);
      final route = MaterialPageRoute<void>(
        builder: (_) => NotificationDestinationBoundary(
          identityChanges: changes,
          current: valid,
          builder: (_) => child,
        ),
      );
      _child = route;
      await Navigator.of(context).push(route);
      if (_child == route) _child = null;
    } catch (_) {
      if (mounted && valid()) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('通知指向的内容已失效、不可访问或暂时无法读取。')),
        );
      }
    }
    // Borrowed sources use the Inbox transport; only the Inbox owner closes it.
  }

  Future<void> _preferences() async {
    final route = MaterialPageRoute<void>(
      builder: (_) => NotificationPolicyPage(
        auth: widget.auth,
        workspaceChanges: widget.workspaceChanges,
        organizationWorkspaceID: widget.organizationWorkspaceID,
      ),
    );
    _child = route;
    await Navigator.of(context).push(route);
    if (_child == route) {
      _child = null;
    }
    if (mounted && _personal) {
      await _load();
    }
  }

  @override
  void dispose() {
    ++_serial;
    ++_identityEpoch;
    _destinationChanges.value++;
    _destinationChanges.dispose();
    widget.auth.removeListener(_onAuthChanged);
    widget.workspaceChanges?.removeListener(_onAuthChanged);
    if (_ownsSource) _source.dispose();
    _scroll.dispose();
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
            '收件箱',
            style: TextStyle(
              fontSize: 28,
              fontWeight: FontWeight.w700,
              color: Color(0xFF193B32),
            ),
          ),
          Text(
            '动态中心 · 你的账号',
            style: const TextStyle(color: Color(0xFF747B73), fontSize: 12),
          ),
          if (_personal)
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton.icon(
                onPressed: _preferences,
                icon: const Icon(Icons.notifications_outlined),
                label: const Text('通知设置'),
              ),
            ),
          const SizedBox(height: 16),
          Expanded(child: _body()),
        ],
      ),
    ),
  );

  Iterable<InboxItem> _sectionItems(String key) => _items.where(
    (item) => key == 'digest'
        ? item.notificationRoute == 'DIGEST'
        : item.notificationRoute != 'DIGEST' && item.category == key,
  );

  Widget _body() {
    if (_personal && !_sourceCurrent) {
      return const _InboxMessage('消息连接与当前账号不符，请返回当前入口重新打开。');
    }
    if (!_configured) {
      return const _InboxMessage('收件箱暂不可用，请连接 Birdtie 服务。');
    }
    if (!_personal) {
      return const _InboxMessage('请使用本人账号查看 Birdtie 动态。');
    }
    if (_loading) return const Center(child: CircularProgressIndicator());
    if (_failed) {
      return ListView(
        controller: _scroll,
        children: [
          SocialInboxSection(
            auth: widget.auth,
            workspaceChanges: widget.workspaceChanges,
            workspaceID: widget.organizationWorkspaceID,
            client: _source.followClient,
            apiBaseUrl: _source.followApiBaseUrl,
          ),
          Center(
            child: TextButton(
              onPressed: _load,
              child: const Text('收件箱暂不可用，点击重试。'),
            ),
          ),
        ],
      );
    }
    if (_items.isEmpty) {
      return RefreshIndicator(
        onRefresh: _load,
        child: ListView(
          controller: _scroll,
          physics: const AlwaysScrollableScrollPhysics(),
          children: [
            SocialInboxSection(
              auth: widget.auth,
              workspaceChanges: widget.workspaceChanges,
              workspaceID: widget.organizationWorkspaceID,
            client: _source.followClient,
            apiBaseUrl: _source.followApiBaseUrl,
            ),
            const _InboxMessage('暂时没有新动态。活动变更和重要消息会显示在这里。'),
          ],
        ),
      );
    }
    const sections = [
      ('digest', '定时汇总'),
      ('needs_attention', '需要处理'),
      ('messages', '消息'),
      ('requests', '申请'),
      ('agent_updates', 'Agent 动态'),
      ('updates', '其他动态'),
    ];
    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        controller: _scroll,
        children: [
          SocialInboxSection(
            auth: widget.auth,
            workspaceChanges: widget.workspaceChanges,
            workspaceID: widget.organizationWorkspaceID,
            client: _source.followClient,
            apiBaseUrl: _source.followApiBaseUrl,
          ),
          for (final (key, label) in sections)
            if (_sectionItems(key).isNotEmpty) ...[
              _SectionHeading(label),
              if (key == 'digest')
                const Text('以下是本次读取的已投递条目，打开时会核对当前内容。'),
              for (final item in _sectionItems(key))
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  onTap: _openingID != null ? null : () => _open(item),
                  leading: CircleAvatar(
                    backgroundColor: const Color(0xFFE7EDE2),
                    child: Icon(
                      item.resourceType == 'connection_request'
                          ? Icons.person_add_alt_outlined
                          : item.resourceType == 'conversation_message'
                          ? Icons.chat_bubble_outline
                          : item.targetActivityID != null
                          ? Icons.event_outlined
                          : item.targetCommunityID != null
                          ? Icons.group_outlined
                          : item.targetTaskID != null
                          ? Icons.task_alt
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
                  trailing:
                      item.targetActivityID != null ||
                          item.targetConversationID != null ||
                          item.targetCommunityID != null ||
                          item.targetTaskID != null
                      ? const Icon(Icons.chevron_right)
                      : item.readAt == null
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
