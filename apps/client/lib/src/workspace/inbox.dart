import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';

import '../auth/birdtie_auth_controller.dart';
import '../auth/birdtie_login_sheet.dart';
import '../auth/account_access_notice.dart';
import 'remote_inbox_source.dart';
import 'connections.dart';
import 'connection_request_review_page.dart';
import 'now_social_edge_source.dart';
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
import 'public_business_page.dart';
import 'person_avatar.dart';
import 'inbox_review_result_page.dart';
import 'moment_interaction.dart';
import 'public_moment_page.dart';
import '../content/moment_control.dart' show newMomentOperationID;

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
    this.onUsePersonal,
    this.onSignIn,
    this.embedded = false,
    this.notificationsOnly = false,
    this.previewLimit,
    this.onUnreadChanged,
    this.onUnreadInvalidated,
    this.initialItem,
    this.refreshChanges,
  });
  final BirdtieAuthController auth;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final Future<void> Function()? onOrganizationAccepted;
  final VoidCallback? onUsePersonal;
  final VoidCallback? onSignIn;
  final bool embedded, notificationsOnly;
  final int? previewLimit;
  final ValueChanged<int>? onUnreadChanged;
  final VoidCallback? onUnreadInvalidated;
  final InboxItem? initialItem;
  final Listenable? refreshChanges;
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
  String _failureMessage = '连接暂不可用，请重试。';
  bool _denied = false;
  bool _showAll = false;
  String? _nextCursor;
  int? _serverUnread;
  int _unreadSerial = 0;
  bool _unreadRefreshFailed = false;
  bool _loadingMore = false, _moreFailed = false;
  Object? _sourceIdentity;
  final Set<String> _consumedCursors = {};
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
  SocialOperationController? _management;
  String _category = '';
  bool _unreadOnly = false;

  void _configureManagement() {
    _management?.dispose();
    final source = _source, version = source.identityVersion;
    _management = SocialOperationController(
      api: SocialOperationAPI(
        client: source.followClient,
        baseUrl: source.followApiBaseUrl,
        authorizationHeader: () => widget.auth.authorizationHeader,
        current: () =>
            mounted &&
            _personal &&
            _sourceCurrent &&
            identical(source, _source) &&
            version == source.identityVersion,
      ),
      family: 'INBOX_MANAGEMENT',
      ownerID: () => widget.auth.accountID,
      identityChanges: Listenable.merge([
        widget.auth,
        widget.workspaceChanges,
        _destinationChanges,
      ]),
    );
    unawaited(_management!.restore());
  }

  Future<void> _manageNotifications(String action, {InboxItem? item}) async {
    final controller = _management, serial = _serial;
    if (controller == null || !controller.canStart || !_current(serial)) return;
    final title = switch (action) {
      'MARK_ALL_READ' => '全部标为已读',
      'CLEAR_READ' => '清理已读通知',
      _ => '删除这条通知',
    };
    final result = await controller.run(
      context,
      {
        'operationId': newMomentOperationID(),
        'action': action,
        'itemId': item?.id ?? '',
        'category': _category,
        'unreadOnly': action == 'CLEAR_READ' ? false : _unreadOnly,
      },
      title,
      action == 'DELETE_ITEM'
          ? '只删除这条通知记录，关联动态、申请和会话会保留。'
          : '只处理当前分类的通知快照，关联内容会保留。',
      contentCurrent: () => _current(serial),
    );
    if (result != null && _current(serial)) await _load();
  }

  Widget _notificationManagement() {
    final controller = _management;
    if (controller == null ||
        !_personal ||
        widget.previewLimit != null && !_showAll)
      return const SizedBox.shrink();
    return AnimatedBuilder(
      animation: controller,
      builder: (_, _) => Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Wrap(
            spacing: 8,
            children: [
              DropdownButton<String>(
                value: _category,
                items: const [
                  DropdownMenuItem(value: '', child: Text('全部分类')),
                  DropdownMenuItem(
                    value: 'needs_attention',
                    child: Text('需要处理'),
                  ),
                  DropdownMenuItem(value: 'requests', child: Text('申请')),
                  DropdownMenuItem(value: 'messages', child: Text('消息')),
                  DropdownMenuItem(value: 'agent_updates', child: Text('助理动态')),
                  DropdownMenuItem(value: 'updates', child: Text('其他动态')),
                ],
                onChanged: controller.busy
                    ? null
                    : (value) {
                        if (value == null) return;
                        controller.retirePreview();
                        setState(() => _category = value);
                        unawaited(_load());
                      },
              ),
              FilterChip(
                label: const Text('仅未读'),
                selected: _unreadOnly,
                onSelected: controller.busy
                    ? null
                    : (value) {
                        controller.retirePreview();
                        setState(() => _unreadOnly = value);
                        unawaited(_load());
                      },
              ),
            ],
          ),
          Wrap(
            children: [
              TextButton(
                onPressed: controller.canStart
                    ? () => _manageNotifications('MARK_ALL_READ')
                    : null,
                child: const Text('全部已读'),
              ),
              TextButton(
                onPressed: controller.canStart
                    ? () => _manageNotifications('CLEAR_READ')
                    : null,
                child: const Text('清理已读'),
              ),
            ],
          ),
          SocialOperationRecovery(
            controller: controller,
            onResolved: () => unawaited(_load()),
          ),
        ],
      ),
    );
  }

  void _notifyDestinationChanges({bool dispose = false}) {
    void notify() {
      if (dispose || mounted) _destinationChanges.value++;
      if (dispose) _destinationChanges.dispose();
    }

    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) => notify());
    } else {
      notify();
    }
  }

  @override
  void initState() {
    super.initState();
    _ownsSource = widget.source == null;
    _source =
        widget.source ??
        RemoteInboxSource(
          authorizationHeader: () => widget.auth.authorizationHeader,
          identityEpoch: () => _identityEpoch,
        );
    _sourceIdentity = _source.identityVersion;
    _identity = _currentIdentity;
    _configureManagement();
    widget.auth.addListener(_onAuthChanged);
    widget.workspaceChanges?.addListener(_onAuthChanged);
    widget.refreshChanges?.addListener(_refreshFromPush);
    if (widget.initialItem case final item?) {
      final initialEpoch = _identityEpoch;
      final initialSource = _source;
      final initialSourceIdentity = _source.identityVersion;
      bool initialCurrent() =>
          mounted &&
          _personal &&
          _sourceCurrent &&
          initialEpoch == _identityEpoch &&
          identical(initialSource, _source) &&
          initialSourceIdentity == _source.identityVersion;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (initialCurrent() && _configured)
          unawaited(() async {
            await _load();
            if (initialCurrent()) await _open(item);
          }());
      });
    } else if (_personal && _configured) {
      unawaited(_load());
    }
  }

  void _refreshFromPush() {
    if (mounted &&
        _personal &&
        _sourceCurrent &&
        !_loading &&
        !_loadingMore &&
        _openingID == null)
      unawaited(_load());
  }

  bool get _configured =>
      widget.source != null || RemoteInboxSource.apiBase.isNotEmpty;

  void _onAuthChanged() {
    _refreshIdentity();
  }

  void _refreshIdentity({bool force = false}) {
    if (!force &&
        _identity == _currentIdentity &&
        _sourceIdentity == _source.identityVersion) {
      // Phase-only notifications update copy, never retire data or reload.
      if (mounted) setState(() {});
      return;
    }
    _identity = _currentIdentity;
    ++_serial;
    ++_identityEpoch;
    ++_unreadSerial;
    _sourceIdentity = _source.identityVersion;
    _notifyDestinationChanges();
    final child = _child;
    _child = null;
    if (child?.isActive ?? false) {
      void close() {
        if (child?.navigator != null) child!.navigator!.removeRoute(child);
      }

      if (SchedulerBinding.instance.schedulerPhase ==
          SchedulerPhase.persistentCallbacks) {
        WidgetsBinding.instance.addPostFrameCallback((_) => close());
      } else {
        close();
      }
    }
    setState(() {
      _items = const [];
      _openingID = null;
      _loading = false;
      _failed = false;
      _showAll = false;
      _nextCursor = null;
      _serverUnread = null;
      _loadingMore = false;
      _moreFailed = false;
      _consumedCursors.clear();
    });
    _invalidateUnread();
    _configureManagement();
    if (_personal && _configured) {
      unawaited(_load());
    }
  }

  @override
  void didUpdateWidget(covariant InboxPanel oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.refreshChanges != widget.refreshChanges) {
      oldWidget.refreshChanges?.removeListener(_refreshFromPush);
      widget.refreshChanges?.addListener(_refreshFromPush);
    }
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
            identityEpoch: () => _identityEpoch,
          );
    }
    _refreshIdentity(
      force:
          sourceChanged ||
          oldWidget.auth != widget.auth ||
          oldWidget.workspaceChanges != widget.workspaceChanges ||
          oldWidget.notificationsOnly != widget.notificationsOnly ||
          oldWidget.previewLimit != widget.previewLimit,
    );
  }

  bool get _sourceCurrent =>
      _source.active &&
      _source.authorizationHeader() == widget.auth.authorizationHeader;

  bool _current(int serial) =>
      mounted &&
      serial == _serial &&
      _personal &&
      _sourceCurrent &&
      _sourceIdentity == _source.identityVersion &&
      _identity == _currentIdentity;
  int get _notificationLimit => !_showAll && widget.previewLimit != null
      ? widget.previewLimit!.clamp(1, 20)
      : 20;

  void _invalidateUnread() {
    if (!widget.notificationsOnly) return;
    final epoch = _identityEpoch;
    void notify() {
      if (mounted && epoch == _identityEpoch && _serverUnread == null) {
        widget.onUnreadInvalidated?.call();
      }
    }

    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) => notify());
    } else {
      notify();
    }
  }

  int? _readStatus(Object error) => error is InboxReadFailure
      ? error.statusCode
      : error is NowSocialReadFailure
      ? error.statusCode
      : null;

  void _readFailed(Object error) {
    final code = _readStatus(error);
    final retired = const {401, 403, 404, 409}.contains(code);
    setState(() {
      _failed = true;
      _denied = code == 403;
      _failureMessage = switch (code) {
        401 => '登录已失效，请重新登录。',
        403 => '暂无权限查看通知。',
        404 => '这些通知当前不可访问，请刷新核对。',
        409 => '通知内容已变化，请刷新查看。',
        final n? when n >= 500 => '通知服务暂不可用，请稍后重试。',
        _ => '连接暂不可用，请检查网络后重试。',
      };
      if (retired) {
        _items = const [];
        _nextCursor = null;
        _serverUnread = null;
        _consumedCursors.clear();
      }
    });
    if (retired) _invalidateUnread();
  }

  Future<void> _load() async {
    ++_unreadSerial;
    if (!_personal) {
      return;
    }
    if (!_sourceCurrent) {
      setState(() {
        _items = const [];
        _loading = false;
        _failed = true;
        _nextCursor = null;
        _serverUnread = null;
      });
      _invalidateUnread();
      return;
    }
    final serial = ++_serial;
    setState(() {
      _loading = true;
      _failed = false;
      _denied = false;
      _loadingMore = false;
      _moreFailed = false;
      _unreadRefreshFailed = false;
    });
    try {
      final page = widget.notificationsOnly
          ? await _source.loadNotificationsPage(
              limit: _notificationLimit,
              category: _category,
              unreadOnly: _unreadOnly,
            )
          : null;
      final items = page?.items ?? await _source.load();
      if (!_current(serial)) return;
      setState(() {
        _items = items;
        _nextCursor = page?.nextCursor;
        _serverUnread = page?.unreadCount;
        _consumedCursors.clear();
      });
      _reportUnread();
    } catch (error) {
      if (!_current(serial)) return;
      _readFailed(error);
    } finally {
      if (_current(serial)) setState(() => _loading = false);
    }
  }

  Future<void> _loadMore() async {
    final cursor = _nextCursor;
    if (!widget.notificationsOnly ||
        _loading ||
        _loadingMore ||
        _failed ||
        cursor == null ||
        !_current(_serial)) {
      return;
    }
    final serial = _serial;
    ++_unreadSerial;
    setState(() {
      _loadingMore = true;
      _moreFailed = false;
    });
    try {
      final page = await _source.loadNotificationsPage(
        limit: 20,
        cursor: cursor,
        category: _category,
        unreadOnly: _unreadOnly,
      );
      if (!_current(serial) || _nextCursor != cursor) return;
      final known = _items.map((item) => item.id).toSet();
      if (page.items.any((item) => known.contains(item.id)) ||
          page.nextCursor != null &&
              _consumedCursors.contains(page.nextCursor)) {
        throw const FormatException('Repeated notification page');
      }
      setState(() {
        _items = List.unmodifiable([..._items, ...page.items]);
        _consumedCursors.add(cursor);
        _nextCursor = page.nextCursor;
        _serverUnread = page.unreadCount;
      });
      _reportUnread();
    } catch (error) {
      if (!_current(serial)) return;
      if (const {401, 403, 404, 409}.contains(_readStatus(error))) {
        _readFailed(error);
      } else {
        setState(() => _moreFailed = true);
      }
    } finally {
      if (_current(serial)) setState(() => _loadingMore = false);
    }
  }

  void _showAllNotifications() {
    if (_loading || _loadingMore) return;
    setState(() => _showAll = true);
    if (widget.notificationsOnly) unawaited(_load());
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
      if (widget.notificationsOnly && item.readAt == null) {
        // This row receipt is not a global unread total. Re-read the existing
        // authoritative count without replacing rows, cursor or scroll state.
        await _refreshUnreadCount();
      } else {
        _reportUnread();
      }
      return current;
    } catch (error) {
      if (mounted && _current(serial)) {
        if (const {401, 403, 404, 409}.contains(_readStatus(error))) {
          _readFailed(error);
        }
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

  Future<void> _refreshUnreadCount() async {
    if (!widget.notificationsOnly || !_personal || !_sourceCurrent) return;
    final request = ++_unreadSerial,
        serial = _serial,
        source = _source,
        identity = _identityEpoch,
        sourceVersion = _source.identityVersion;
    bool current() =>
        mounted &&
        request == _unreadSerial &&
        _current(serial) &&
        identity == _identityEpoch &&
        identical(source, _source) &&
        sourceVersion == source.identityVersion;
    try {
      final page = await source.loadNotificationsPage(limit: 1);
      if (!current()) return;
      setState(() {
        _serverUnread = page.unreadCount;
        _unreadRefreshFailed = false;
      });
      _reportUnread();
    } catch (_) {
      if (current()) setState(() => _unreadRefreshFailed = true);
    }
  }

  Future<void> _open(InboxItem item) async {
    final serial = _serial, current = await _markRead(item);
    if (!mounted || current == null || !_current(serial)) {
      return;
    }
    if (const {
      'moment_like',
      'moment_comment',
    }.contains(current.resourceType)) {
      final source = _source,
          version = source.identityVersion,
          identity = _identityEpoch;
      bool valid() =>
          _current(serial) &&
          identical(source, _source) &&
          version == source.identityVersion &&
          identity == _identityEpoch;
      try {
        final target = await source.momentTarget(current.id);
        if (current.resourceType == 'moment_comment' &&
                target.commentID == null ||
            current.resourceType == 'moment_like' && target.commentID != null)
          throw const FormatException('动态通知类型与目标不符');
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
            builder: (_) => PublicMomentPage(
              momentID: target.momentID,
              commentID: target.commentID,
              authorizationHeader: () =>
                  valid() ? widget.auth.authorizationHeader : null,
              ownerID: () => valid() ? widget.auth.accountID : null,
              workspaceID: widget.organizationWorkspaceID,
              identityChanges: changes,
              client: source.followClient,
              apiBaseUrl: source.followApiBaseUrl,
            ),
          ),
        );
        _child = route;
        await Navigator.of(context).push(route);
        if (_child == route) _child = null;
      } catch (_) {
        if (mounted && valid())
          ScaffoldMessenger.of(
            context,
          ).showSnackBar(const SnackBar(content: Text('这条动态或评论已失效，请刷新通知。')));
      }
    } else if (const {
      'activity_candidate',
      'place_candidate',
    }.contains(current.resourceType)) {
      final source = _source,
          version = source.identityVersion,
          identity = _identityEpoch;
      bool valid() =>
          _current(serial) &&
          identical(source, _source) &&
          version == source.identityVersion &&
          identity == _identityEpoch;
      final changes = Listenable.merge([
        widget.auth,
        widget.workspaceChanges,
        _destinationChanges,
      ]);
      final route = MaterialPageRoute<void>(
        builder: (_) => NotificationDestinationBoundary(
          identityChanges: changes,
          current: valid,
          builder: (_) => InboxReviewResultPage(
            item: current,
            source: source,
            current: valid,
            identityChanges: changes,
            ownerID: () => widget.auth.accountID,
          ),
        ),
      );
      _child = route;
      await Navigator.of(context).push(route);
      if (_child == route) _child = null;
    } else if (current.resourceType == 'connection_request') {
      final source = ConnectionSource(
        authorizationHeader: () => widget.auth.authorizationHeader,
        client: _source.followClient,
        apiBaseUrl: _source.followApiBaseUrl,
      );
      try {
        await Navigator.of(context).push<void>(
          MaterialPageRoute(
            builder: (_) => ConnectionRequestReviewPage(
              auth: widget.auth,
              source: source,
              requestID: current.resourceID,
              workspaceChanges: widget.workspaceChanges,
              organizationWorkspaceID: widget.organizationWorkspaceID,
              current: () => _current(serial),
            ),
          ),
        );
        if (_current(serial)) await _load();
      } finally {
        source.dispose();
      }
    } else if (current.targetActivityID != null) {
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
    final changes = Listenable.merge([
      widget.auth,
      widget.workspaceChanges,
      _destinationChanges,
    ]);
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
        child = item.resourceType == 'business_update'
            ? PublicBusinessPage(
                businessID: id,
                authorizationHeader: token,
                workspaceID: widget.organizationWorkspaceID,
                identityChanges: changes,
                apiBaseUrl: _source.followApiBaseUrl,
                client: _source.followClient,
                onOpenActivity: (activity) {
                  if (valid()) widget.onOpenActivity?.call(activity.id);
                },
              )
            : BusinessClaimNotificationPage(
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
    _management?.dispose();
    ++_serial;
    ++_identityEpoch;
    _notifyDestinationChanges(dispose: true);
    widget.auth.removeListener(_onAuthChanged);
    widget.workspaceChanges?.removeListener(_onAuthChanged);
    widget.refreshChanges?.removeListener(_refreshFromPush);
    if (_ownsSource) _source.dispose();
    _scroll.dispose();
    super.dispose();
  }

  void _signIn() {
    final auth = widget.auth, source = _source, epoch = _identityEpoch;
    bool current() =>
        mounted &&
        identical(widget.auth, auth) &&
        identical(_source, source) &&
        epoch == _identityEpoch;
    unawaited(
      showBirdtieLoginSheet(
        context,
        auth,
        isCurrent: current,
        entryChanges: _destinationChanges,
      ),
    );
  }

  @override
  Widget build(BuildContext context) => widget.embedded
      ? _body()
      : SafeArea(
          top: false,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(22, 12, 22, 24),
            child: widget.auth.sessionRestoring || !_personal
                ? SingleChildScrollView(child: _contents(expand: false))
                : _contents(expand: true),
          ),
        );

  Widget _contents({required bool expand}) => Column(
    mainAxisSize: expand ? MainAxisSize.max : MainAxisSize.min,
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
      Text('收件箱', style: Theme.of(context).textTheme.headlineSmall),
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
      if (expand) Expanded(child: _body()) else _body(),
    ],
  );

  Iterable<InboxItem> get _visibleItems => widget.notificationsOnly
      ? _items.where((item) => item.resourceType != 'conversation_message')
      : _items.where(
          (item) =>
              (_category.isEmpty || item.category == _category) &&
              (!_unreadOnly || item.readAt == null),
        );

  void _reportUnread() {
    if (widget.notificationsOnly) {
      final total = _serverUnread;
      if (total != null) widget.onUnreadChanged?.call(total);
    } else {
      widget.onUnreadChanged?.call(
        _visibleItems.where((item) => item.readAt == null).length,
      );
    }
  }

  Iterable<InboxItem> get _presentedItems =>
      !_showAll && widget.previewLimit != null
      ? _visibleItems.take(widget.previewLimit!.clamp(1, 20))
      : _visibleItems;

  Iterable<InboxItem> _sectionItems(String key) => key == 'notifications'
      ? _presentedItems
      : _presentedItems.where(
          (item) => key == 'digest'
              ? item.notificationRoute == 'DIGEST'
              : item.notificationRoute != 'DIGEST' && item.category == key,
        );

  Widget _itemLeading(InboxItem item) {
    final avatar = item.actorAvatar;
    if (avatar != null && item.actorID == avatar.accountID) {
      final epoch = _identityEpoch, source = _source;
      final sourceIdentity = _source.identityVersion;
      final identity = _currentIdentity;
      return PersonAvatar(
        key: ValueKey((
          'inbox-avatar',
          item.id,
          avatar.accountID,
          avatar.version,
        )),
        metadata: avatar,
        authorizationHeader: () => widget.auth.authorizationHeader,
        baseUrl: source.followApiBaseUrl,
        client: source.followClient,
        identityChanges: Listenable.merge([
          widget.auth,
          _destinationChanges,
          if (widget.workspaceChanges != null) widget.workspaceChanges!,
        ]),
        current: () =>
            mounted &&
            _personal &&
            _sourceCurrent &&
            epoch == _identityEpoch &&
            identical(source, _source) &&
            sourceIdentity == _source.identityVersion &&
            identity == _currentIdentity,
        radius: 22,
      );
    }
    final color = Theme.of(context).colorScheme;
    return CircleAvatar(
      backgroundColor: color.surfaceContainerLow,
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
            : item.targetBusinessID != null
            ? Icons.storefront_outlined
            : item.resourceType == 'activity_candidate'
            ? Icons.event_outlined
            : Icons.place_outlined,
        color: color.primary,
        size: 20,
      ),
    );
  }

  Widget _body() {
    if (widget.auth.sessionRestoring) {
      return const AccountAccessNotice(
        title: '正在恢复账户',
        message: '账户核验完成后会加载本人的动态。',
        restoring: true,
      );
    }
    if (_personal && !_sourceCurrent) {
      return const _InboxMessage('消息连接与当前账号不符，请返回当前入口重新打开。');
    }
    if (!_configured) {
      return const _InboxMessage('通知暂不可用，请稍后重试。');
    }
    if (!_personal) {
      final signedIn = widget.auth.signedIn;
      return AccountAccessNotice(
        title: signedIn ? '切回个人身份查看动态' : '登录后查看动态',
        message: signedIn ? '收件箱展示本人的消息、申请和活动变更。' : '你的消息、申请和活动变更会显示在这里。',
        actionLabel: signedIn ? '切回个人身份' : '登录',
        onAction: signedIn ? widget.onUsePersonal : widget.onSignIn ?? _signIn,
      );
    }
    if (_loading && (!widget.notificationsOnly || _items.isEmpty)) {
      return Center(
        heightFactor: widget.embedded && widget.notificationsOnly ? 1 : null,
        child: CircularProgressIndicator(
          semanticsLabel: widget.embedded && widget.notificationsOnly
              ? '正在加载通知'
              : null,
        ),
      );
    }
    if (_failed && (!widget.notificationsOnly || _items.isEmpty)) {
      return ListView(
        shrinkWrap: widget.embedded && widget.notificationsOnly,
        controller: _scroll,
        children: [
          if (!widget.notificationsOnly)
            SocialInboxSection(
              auth: widget.auth,
              workspaceChanges: widget.workspaceChanges,
              workspaceID: widget.organizationWorkspaceID,
              client: _source.followClient,
              apiBaseUrl: _source.followApiBaseUrl,
            ),
          if (widget.embedded) ...[
            _InboxMessage(_failureMessage),
            if (!_denied) TextButton(onPressed: _load, child: const Text('重试')),
          ] else
            Center(
              child: TextButton(
                onPressed: _load,
                child: const Text('收件箱暂不可用，点击重试。'),
              ),
            ),
        ],
      );
    }
    if (_visibleItems.isEmpty) {
      return RefreshIndicator(
        onRefresh: _load,
        child: ListView(
          shrinkWrap: widget.embedded && widget.notificationsOnly,
          controller: _scroll,
          physics: const AlwaysScrollableScrollPhysics(),
          children: [
            _notificationManagement(),
            if (!widget.notificationsOnly)
              SocialInboxSection(
                auth: widget.auth,
                workspaceChanges: widget.workspaceChanges,
                workspaceID: widget.organizationWorkspaceID,
                client: _source.followClient,
                apiBaseUrl: _source.followApiBaseUrl,
              ),
            _InboxMessage('暂无通知'),
            if (widget.embedded)
              TextButton(onPressed: _load, child: const Text('刷新')),
          ],
        ),
      );
    }
    const sections = [
      ('digest', '定时汇总'),
      ('needs_attention', '需要处理'),
      ('messages', '消息'),
      ('requests', '申请'),
      ('agent_updates', '助理动态'),
      ('updates', '其他动态'),
    ];
    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        shrinkWrap: widget.embedded && widget.notificationsOnly,
        controller: _scroll,
        children: [
          _notificationManagement(),
          if (widget.notificationsOnly && _loading)
            const LinearProgressIndicator(),
          if (widget.notificationsOnly && _failed) ...[
            _InboxMessage(_failureMessage),
            if (!_denied)
              TextButton(
                onPressed: _loading ? null : _load,
                child: const Text('重试'),
              ),
          ],
          if (!widget.notificationsOnly)
            SocialInboxSection(
              auth: widget.auth,
              workspaceChanges: widget.workspaceChanges,
              workspaceID: widget.organizationWorkspaceID,
              client: _source.followClient,
              apiBaseUrl: _source.followApiBaseUrl,
            ),
          for (final (key, label)
              in widget.notificationsOnly
                  ? const [('notifications', '通知')]
                  : sections)
            if (_sectionItems(key).isNotEmpty) ...[
              if (!widget.notificationsOnly &&
                  (widget.previewLimit == null || _showAll))
                _SectionHeading(label),
              for (final item in _sectionItems(key))
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  onTap: _openingID != null ? null : () => _open(item),
                  leading: _itemLeading(item),
                  title: Text(
                    item.title,
                    style: Theme.of(context).textTheme.titleMedium,
                    maxLines: widget.embedded ? 1 : null,
                    overflow: widget.embedded ? TextOverflow.ellipsis : null,
                  ),
                  subtitle: Text(
                    item.detail,
                    maxLines: widget.embedded ? 1 : null,
                    overflow: widget.embedded ? TextOverflow.ellipsis : null,
                  ),
                  trailing: widget.previewLimit != null && !_showAll
                      ? item.readAt == null
                            ? const Icon(
                                Icons.circle,
                                size: 8,
                                color: Color(0xFF497966),
                              )
                            : const Icon(Icons.chevron_right)
                      : PopupMenuButton<String>(
                          tooltip: '通知操作',
                          onSelected: (value) {
                            if (value == 'delete')
                              unawaited(
                                _manageNotifications('DELETE_ITEM', item: item),
                              );
                          },
                          itemBuilder: (_) => [
                            PopupMenuItem(
                              value: 'delete',
                              enabled: _management?.canStart == true,
                              child: const Text('删除通知'),
                            ),
                          ],
                        ),
                ),
              const Divider(height: 20),
            ],
          if (_unreadRefreshFailed)
            TextButton(
              onPressed: _refreshUnreadCount,
              child: const Text('未读数量暂未更新，重试'),
            ),
          if (!_showAll &&
              widget.previewLimit != null &&
              (widget.notificationsOnly
                  ? _nextCursor != null
                  : _visibleItems.length > widget.previewLimit!))
            TextButton(
              key: const Key('notifications-show-all'),
              onPressed: _loading ? null : _showAllNotifications,
              child: const Text('查看全部通知'),
            ),
          if (widget.notificationsOnly &&
              (_showAll || widget.previewLimit == null) &&
              _nextCursor != null &&
              !_failed)
            TextButton(
              key: const Key('notifications-load-more'),
              onPressed: _loading || _loadingMore ? null : _loadMore,
              child: Text(
                _loadingMore
                    ? '正在加载…'
                    : _moreFailed
                    ? '重试加载更多'
                    : '加载更多通知',
              ),
            ),
          if (widget.notificationsOnly && _moreFailed)
            const _InboxMessage('更多通知暂不可读取，已保留当前内容。'),
        ],
      ),
    );
  }
}

/// A single request category in the same social viewport. Opening a request
/// uses the original explicit review/decision flow; reads never accept it.
class SocialRequestsPanel extends StatefulWidget {
  const SocialRequestsPanel({
    super.key,
    required this.auth,
    required this.source,
    required this.scope,
    required this.identityChanges,
    required this.current,
  });
  final BirdtieAuthController auth;
  final NowSocialEdgeSource source;
  final String scope;
  final Listenable identityChanges;
  final bool Function() current;
  @override
  State<SocialRequestsPanel> createState() => _SocialRequestsPanelState();
}

class _SocialRequestsPanelState extends State<SocialRequestsPanel> {
  List<ContactRequest> _rows = const [];
  int _serial = 0;
  bool _loading = false;
  String? _error;
  bool _denied = false, _loadingMore = false;
  String? _nextCursor, _moreError;
  bool _restartRequired = false;
  final _cursors = <String>{};
  late (String?, String?) _identity;
  bool get _valid =>
      mounted &&
      widget.current() &&
      widget.auth.signedIn &&
      _identity == (widget.auth.authorizationHeader, widget.auth.accountID);
  @override
  void initState() {
    super.initState();
    _identity = (widget.auth.authorizationHeader, widget.auth.accountID);
    widget.identityChanges.addListener(_changed);
    unawaited(_load());
  }

  void _retire() {
    ++_serial;
    _rows = const [];
    _loading = false;
    _loadingMore = false;
    _error = null;
    _moreError = null;
    _nextCursor = null;
    _denied = false;
    _restartRequired = false;
    _cursors.clear();
  }

  void _changed() {
    // Invalidate synchronously, including A-B-A within one frame.
    _retire();
    _identity = (widget.auth.authorizationHeader, widget.auth.accountID);
    void refresh() {
      if (!mounted) return;
      setState(() {});
      if (_valid) unawaited(_load());
    }

    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) => refresh());
    } else {
      refresh();
    }
  }

  @override
  void didUpdateWidget(SocialRequestsPanel old) {
    super.didUpdateWidget(old);
    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges.removeListener(_changed);
      widget.identityChanges.addListener(_changed);
    }
    if (old.source != widget.source ||
        old.scope != widget.scope ||
        old.auth != widget.auth ||
        old.identityChanges != widget.identityChanges ||
        _identity != (widget.auth.authorizationHeader, widget.auth.accountID)) {
      _retire();
      _identity = (widget.auth.authorizationHeader, widget.auth.accountID);
      unawaited(_load());
    }
  }

  Future<void> _load({bool more = false}) async {
    if (!_valid || _loading || _loadingMore || more && _nextCursor == null)
      return;
    final serial = ++_serial, source = widget.source, scope = widget.scope;
    final cursor = more ? _nextCursor : null;
    bool current() =>
        _valid &&
        serial == _serial &&
        source == widget.source &&
        scope == widget.scope;
    setState(() {
      if (more) {
        _loadingMore = true;
        _moreError = null;
      } else {
        _loading = true;
        _error = null;
        _moreError = null;
        _nextCursor = null;
        _cursors.clear();
      }
      _denied = false;
      _restartRequired = false;
    });
    try {
      final page = await source.requestsPage(cursor: cursor);
      if (!current()) return;
      if (page.nextCursor != null && _cursors.contains(page.nextCursor))
        throw const FormatException('申请分页无法继续');
      final rows = <String, ContactRequest>{
        if (more)
          for (final row in _rows) row.id: row,
        for (final row in page.items.where((r) => r.scope == scope))
          row.id: row,
      };
      setState(() {
        _rows = List.unmodifiable(rows.values);
        _nextCursor = page.nextCursor;
        if (cursor != null) _cursors.add(cursor);
      });
    } catch (e) {
      if (current())
        setState(() {
          final message = e is NowSocialReadFailure ? e.message : '连接中断，请重试。';
          if (e is NowSocialReadFailure &&
              (e.statusCode == 401 || e.statusCode == 403)) {
            _rows = const [];
            _nextCursor = null;
            _cursors.clear();
            _error = message;
            _moreError = null;
            _denied = e.statusCode == 403;
          } else if (more) {
            _moreError = message;
            _restartRequired = e is NowSocialReadFailure && e.statusCode == 409;
          } else {
            _error = message;
          }
        });
    } finally {
      if (current())
        setState(() {
          _loading = false;
          _loadingMore = false;
        });
    }
  }

  Future<void> _open(ContactRequest row) async {
    if (!_valid) return;
    final serial = _serial;
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => ConnectionRequestReviewPage(
          auth: widget.auth,
          source: widget.source.connections,
          requestID: row.id,
          workspaceChanges: widget.identityChanges,
          current: () => _valid && serial == _serial,
        ),
      ),
    );
    if (_valid && serial == _serial) await _load();
  }

  @override
  void dispose() {
    ++_serial;
    widget.identityChanges.removeListener(_changed);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!_valid) return const SizedBox.shrink();
    if (_loading && _rows.isEmpty) {
      return const Center(child: CircularProgressIndicator(strokeWidth: 2));
    }
    if (_error != null && _rows.isEmpty) {
      return Center(
        child: _denied
            ? Text(_error!)
            : TextButton(
                onPressed: _load,
                style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
                child: Text(_error!),
              ),
      );
    }
    if (_rows.isEmpty && _nextCursor == null) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(widget.scope == 'friend' ? '暂无好友申请' : '暂无消息请求'),
            TextButton(onPressed: _load, child: const Text('刷新')),
          ],
        ),
      );
    }
    return ListView(
      key: ValueKey('social-requests-list-${widget.scope}'),
      children: [
        if (_rows.isEmpty && _nextCursor != null)
          const Padding(
            padding: EdgeInsets.all(8),
            child: Text('本页没有此类申请，可继续查看。'),
          ),
        if (_loading) const LinearProgressIndicator(),
        if (_error != null)
          TextButton(
            onPressed: _load,
            style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
            child: Text(_error!),
          ),
        for (final row in _rows)
          ListTile(
            key: ValueKey('social-request-${row.id}'),
            leading: const Icon(Icons.person_outline, size: 20),
            title: Text(
              row.otherName,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
            subtitle: Text(
              '${row.direction == 'incoming' ? '收到' : '已发送'} · ${switch (row.state) {
                'pending' => '待处理',
                'accepted' => '已接受',
                'declined' => '已拒绝',
                'withdrawn' => '已撤回',
                _ => '已过期',
              }}',
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
            onTap: () => _open(row),
          ),
        if (_nextCursor != null)
          TextButton(
            key: const Key('social-requests-more'),
            style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
            onPressed: _loading || _loadingMore
                ? null
                : () => _load(more: !_restartRequired),
            child: Text(
              _loadingMore
                  ? '正在加载'
                  : _restartRequired
                  ? '列表已更新，重新加载'
                  : _moreError != null
                  ? '重试加载更多'
                  : '更多申请',
            ),
          ),
        if (_moreError != null) Text(_moreError!),
      ],
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
        style: Theme.of(context).textTheme.bodySmall,
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
    child: Text(label, style: Theme.of(context).textTheme.titleMedium),
  );
}
