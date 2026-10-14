import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';

import 'connections.dart';
import 'now_social_edge_source.dart';
import 'now_ui_tokens.dart';
import 'now_glass_surface.dart';
import 'person_avatar.dart';
import 'community_avatar.dart';

enum NowSocialSection { messages, contacts, notifications }

enum _ContactsFilter { friends, follows }

typedef _EdgeBinding = ({
  NowSocialEdgeSource source,
  String? token,
  String? owner,
  String? workspace,
});

/// A bounded viewport over the map. Only the selected section owns a list;
/// identity, relationship writes and the shared composer belong to the host.
class NowSocialEdge extends StatefulWidget {
  const NowSocialEdge({
    super.key,
    required this.open,
    required this.source,
    required this.identityChanges,
    required this.current,
    required this.accountID,
    required this.workspaceID,
    required this.onOpenAuthor,
    required this.onOpenConversation,
    required this.onOpenManagement,
    required this.onSignIn,
    required this.onUsePersonal,
    this.onOpenFollowTarget,
    this.onFindPeople,
    this.onOpenFriend,
    this.notificationBody,
    this.notificationTitle,
    this.conversationBody,
    this.onCloseConversation,
    this.onScanCode,
    this.onShowOwnCode,
    this.onSectionChanging,
    this.onSectionChanged,
    this.initialSection = NowSocialSection.messages,
    this.initialFollowedContacts = false,
    this.onContactsFilterChanged,
    this.onOpenMessageRequests,
    this.messageRequestsBody,
    this.onCloseMessageRequests,
    this.initialAllConversations = false,
    this.onAllConversationsChanged,
    this.onUnreadMessagesChanged,
    this.onChooseConversation,
    this.onClose,
  });

  final bool open;
  final NowSocialEdgeSource source;
  final Listenable identityChanges;
  final bool Function() current;
  final String? Function() accountID, workspaceID;
  final ValueChanged<String> onOpenAuthor, onOpenConversation;
  final ValueChanged<FriendTie>? onOpenFriend;
  final ValueChanged<NowFollowEntry>? onOpenFollowTarget;
  final VoidCallback? onFindPeople, onScanCode, onShowOwnCode;
  // Constructor compatibility for older hosts; it is not a second entry.
  final VoidCallback onOpenManagement, onSignIn, onUsePersonal;
  final Widget? notificationBody, conversationBody, messageRequestsBody;
  final String? notificationTitle;
  final VoidCallback? onCloseConversation,
      onOpenMessageRequests,
      onCloseMessageRequests;
  final bool Function(NowSocialSection)? onSectionChanging;
  final ValueChanged<NowSocialSection>? onSectionChanged;
  // The host keeps these owner-bound choices when keyboard chrome removes
  // the viewport. They initialize a mount; later user changes stay local.
  final NowSocialSection initialSection;
  final bool initialFollowedContacts;
  final ValueChanged<bool>? onContactsFilterChanged;
  final bool initialAllConversations;
  final ValueChanged<bool>? onAllConversationsChanged;
  final ValueChanged<int>? onUnreadMessagesChanged;
  final ValueChanged<HumanConversation>? onChooseConversation;
  final VoidCallback? onClose;
  static const columnWidth = NowUiTokens.socialPanelWidth;

  @override
  State<NowSocialEdge> createState() => _NowSocialEdgeState();
}

class _ContactPage<T> {
  List<T>? items;
  String? next;
  bool loading = false, loadingMore = false;
  Object? error, moreError;
  bool get restartRequired =>
      moreError is NowSocialReadFailure &&
      (moreError as NowSocialReadFailure).statusCode == 409;
  final cursors = <String>{};
  void clear() {
    items = null;
    next = null;
    loading = false;
    loadingMore = false;
    error = null;
    moreError = null;
    cursors.clear();
  }
}

class _NowSocialEdgeState extends State<NowSocialEdge> {
  late _EdgeBinding _binding;
  String? _sectionOwner;
  NowSocialSection _section = NowSocialSection.messages;
  _ContactsFilter _contacts = _ContactsFilter.friends;
  int _generation = 0, _serial = 0;
  bool _loading = false, _notificationQueued = false;
  Object? _error;
  final _friendsPage = _ContactPage<FriendTie>();
  final _followsPage = _ContactPage<NowFollowEntry>();
  List<FriendTie>? get _friends => _friendsPage.items;
  List<NowFollowEntry>? get _follows => _followsPage.items;
  _ContactPage<Object> get _contactsPage =>
      _contacts == _ContactsFilter.friends ? _friendsPage : _followsPage;
  void _retireContactPages() {
    _friendsPage.clear();
    _followsPage.clear();
  }

  List<HumanConversation>? _conversations;
  bool _allConversations = false;
  String? _nextConversationsCursor;
  bool _loadingMore = false;
  Object? _moreError;

  _EdgeBinding _capture() => (
    source: widget.source,
    token: widget.source.authorization,
    owner: widget.accountID(),
    workspace: widget.workspaceID(),
  );
  bool get _personal =>
      widget.current() &&
      _binding.source.configured &&
      _binding.token != null &&
      validSocialEdgeID(_binding.owner ?? '') &&
      _binding.workspace == null;
  bool _current(int generation, int serial) =>
      mounted &&
      widget.open &&
      _personal &&
      _binding == _capture() &&
      generation == _generation &&
      serial == _serial;

  @override
  void initState() {
    super.initState();
    _binding = _capture();
    _sectionOwner = _binding.owner;
    _section = widget.initialSection;
    _allConversations = widget.initialAllConversations;
    _contacts = widget.initialFollowedContacts
        ? _ContactsFilter.follows
        : _ContactsFilter.friends;
    widget.identityChanges.addListener(_identityChanged);
    if (widget.open && _personal) unawaited(_load());
  }

  /// Retire reads and visible private data synchronously, including A-B-A
  /// changes in one frame. Scheduling paint must never defer invalidation.
  void _retire(_EdgeBinding next) {
    ++_generation;
    ++_serial;
    if (next.owner != null && next.owner != _sectionOwner) {
      if (_sectionOwner != null) {
        _section = NowSocialSection.messages;
        _contacts = _ContactsFilter.friends;
        _allConversations = false;
      }
      _sectionOwner = next.owner;
    }
    _binding = next;
    _retireContactPages();
    _conversations = null;
    _nextConversationsCursor = null;
    _loadingMore = false;
    _moreError = null;
    _loading = false;
    _error = null;
  }

  void _identityChanged() {
    final next = _capture();
    _retire(next);
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      if (_notificationQueued) return;
      _notificationQueued = true;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        _notificationQueued = false;
        if (!mounted) return;
        setState(() {});
        if (widget.open && _personal) unawaited(_load());
      });
    } else if (mounted) {
      setState(() {});
      if (widget.open && _personal) unawaited(_load());
    }
  }

  @override
  void didUpdateWidget(NowSocialEdge oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.identityChanges != widget.identityChanges) {
      oldWidget.identityChanges.removeListener(_identityChanged);
      widget.identityChanges.addListener(_identityChanged);
    }
    final next = _capture();
    final changed = next != _binding || oldWidget.open != widget.open;
    if (changed) {
      _retire(next);
      if (widget.open && _personal) unawaited(_load());
    }
    if (oldWidget.initialSection != widget.initialSection &&
        _section != widget.initialSection) {
      _retireContactPages();
      _loadingMore = false;
      _section = widget.initialSection;
      _error = null;
      _loading = false;
      ++_serial;
      if (widget.open && _personal) unawaited(_load());
    }
    if (oldWidget.initialAllConversations != widget.initialAllConversations) {
      _allConversations = widget.initialAllConversations;
    }
    if ((oldWidget.conversationBody == null &&
                widget.conversationBody != null ||
            oldWidget.messageRequestsBody == null &&
                widget.messageRequestsBody != null) &&
        _section != NowSocialSection.messages) {
      _section = NowSocialSection.messages;
      _error = null;
      _loading = false;
      ++_serial;
    }
    if (!changed &&
        oldWidget.conversationBody != null &&
        widget.conversationBody == null &&
        widget.messageRequestsBody == null &&
        _section == NowSocialSection.messages &&
        widget.open &&
        _personal) {
      // The thread owns message reads. Returning to its recent list refreshes
      // previews and message unread counts without reading notifications.
      unawaited(_load());
    }
  }

  Future<void> _load() async {
    if (_section == NowSocialSection.contacts) {
      await _loadContacts();
      return;
    }
    final generation = _generation,
        serial = ++_serial,
        source = _binding.source,
        section = _section;
    if (!_current(generation, serial) ||
        section == NowSocialSection.notifications)
      return;
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      if (section == NowSocialSection.messages) {
        final page = await source.conversationPage();
        if (_current(generation, serial)) {
          setState(() {
            _conversations = page.items;
            _nextConversationsCursor = page.nextCursor;
            _moreError = null;
          });
          widget.onUnreadMessagesChanged?.call(page.unreadCount);
        }
      }
    } catch (e) {
      if (_current(generation, serial)) setState(() => _error = e);
    } finally {
      if (_current(generation, serial)) setState(() => _loading = false);
    }
  }

  Future<void> _loadContacts({bool more = false}) async {
    final state = _contactsPage,
        filter = _contacts,
        cursor = more ? _contactsPage.next : null;
    if (_section != NowSocialSection.contacts ||
        state.loading ||
        state.loadingMore ||
        more && cursor == null)
      return;
    final generation = _generation,
        serial = ++_serial,
        source = _binding.source;
    if (!_current(generation, serial)) return;
    setState(() {
      if (more) {
        state.loadingMore = true;
        state.moreError = null;
      } else {
        state.loading = true;
        state.error = null;
        state.moreError = null;
        state.next = null;
        state.cursors.clear();
      }
    });
    try {
      if (filter == _ContactsFilter.friends) {
        final page = await source.friendsPage(cursor: cursor);
        if (!_current(generation, serial)) return;
        if (page.nextCursor != null &&
            _friendsPage.cursors.contains(page.nextCursor))
          throw const FormatException('好友分页无法继续');
        final rows = <String, FriendTie>{
          if (more)
            for (final v in _friends ?? <FriendTie>[]) v.otherAccountId: v,
          for (final v in page.items) v.otherAccountId: v,
        };
        setState(() {
          _friendsPage.items = List.unmodifiable(rows.values);
          _friendsPage.next = page.nextCursor;
          if (cursor != null) _friendsPage.cursors.add(cursor);
        });
      } else {
        final page = await source.followsPage(cursor: cursor);
        if (!_current(generation, serial)) return;
        if (page.nextCursor != null &&
            _followsPage.cursors.contains(page.nextCursor))
          throw const FormatException('关注分页无法继续');
        final rows = <String, NowFollowEntry>{
          if (more)
            for (final v in _follows ?? <NowFollowEntry>[])
              '${v.type}:${v.targetID}': v,
          for (final v in page.items) '${v.type}:${v.targetID}': v,
        };
        setState(() {
          _followsPage.items = List.unmodifiable(rows.values);
          _followsPage.next = page.nextCursor;
          if (cursor != null) _followsPage.cursors.add(cursor);
        });
      }
    } catch (e) {
      if (_current(generation, serial))
        setState(() {
          if (e is NowSocialReadFailure &&
              (e.statusCode == 401 || e.statusCode == 403)) {
            state.clear();
            state.error = e;
          } else if (more)
            state.moreError = e;
          else
            state.error = e;
        });
    } finally {
      if (_current(generation, serial))
        setState(() {
          state.loading = false;
          state.loadingMore = false;
        });
    }
  }

  Future<void> _loadMoreConversations() async {
    final cursor = _nextConversationsCursor;
    if (_loading ||
        _loadingMore ||
        cursor == null ||
        _section != NowSocialSection.messages ||
        !_allConversations)
      return;
    final generation = _generation, serial = ++_serial;
    if (!_current(generation, serial)) return;
    setState(() {
      _loadingMore = true;
      _moreError = null;
    });
    try {
      final page = await _binding.source.conversationPage(cursor: cursor);
      if (!_current(generation, serial)) return;
      final merged = <String, HumanConversation>{
        for (final chat in _conversations ?? const <HumanConversation>[])
          chat.id: chat,
        for (final chat in page.items) chat.id: chat,
      };
      setState(() {
        _conversations = List.unmodifiable(merged.values);
        _nextConversationsCursor = page.nextCursor;
      });
      widget.onUnreadMessagesChanged?.call(page.unreadCount);
    } catch (e) {
      if (_current(generation, serial)) setState(() => _moreError = e);
    } finally {
      if (_current(generation, serial)) setState(() => _loadingMore = false);
    }
  }

  void _choose(HumanConversation chat) {
    if (widget.onChooseConversation != null) {
      widget.onChooseConversation!(chat);
    } else {
      widget.onOpenConversation(chat.id);
    }
  }

  void _select(NowSocialSection value) {
    if (!_personal ||
        value == _section ||
        widget.onSectionChanging?.call(value) == false)
      return;
    setState(() {
      _retireContactPages();
      _loadingMore = false;
      _section = value;
      _error = null;
      _loading = false;
      ++_serial;
    });
    widget.onSectionChanged?.call(value);
    if (value != NowSocialSection.notifications) unawaited(_load());
  }

  VoidCallback _bound(VoidCallback action) {
    final binding = _binding, generation = _generation;
    return () {
      if (!mounted ||
          !widget.open ||
          !_personal ||
          binding != _capture() ||
          generation != _generation)
        return;
      action();
    };
  }

  bool _followCurrent(
    NowFollowEntry entry,
    _EdgeBinding binding,
    int generation,
  ) =>
      mounted &&
      widget.open &&
      _personal &&
      _binding == _capture() &&
      binding == _binding &&
      generation == _generation &&
      _section == NowSocialSection.contacts &&
      _contacts == _ContactsFilter.follows &&
      !_followsPage.loading &&
      (_follows?.any((row) => identical(row, entry)) ?? false);

  VoidCallback _followBound(NowFollowEntry entry, VoidCallback action) {
    final binding = _binding, generation = _generation;
    return () {
      if (_followCurrent(entry, binding, generation)) action();
    };
  }

  Widget? _followMedia(NowFollowEntry entry) {
    final metadata = entry.communityAvatar;
    if (entry.type != 'COMMUNITY' || metadata == null) return null;
    final binding = _binding, generation = _generation;
    bool current() => _followCurrent(entry, binding, generation);
    return CommunityAvatar(
      // A newly read row owns a new media lifecycle, even if its target or
      // version later returns to the same value. Valid append pages retain
      // the original rows and do not retire their visible pixels.
      key: ObjectKey(entry),
      metadata: metadata,
      fallbackLabel: entry.label,
      radius: 15,
      api: CommunityAvatarAPI(
        client: binding.source.connections.followClient,
        baseUrl: binding.source.connections.environment,
        authorizationHeader: () => current() ? binding.token : null,
      ),
      identityChanges: widget.identityChanges,
      current: current,
    );
  }

  @override
  void dispose() {
    ++_generation;
    widget.identityChanges.removeListener(_identityChanged);
    super.dispose();
  }

  TextStyle get _bodyStyle => NowUiTokens.of(context).body;
  TextStyle get _actionStyle => NowUiTokens.of(context).action;
  String _sectionName(NowSocialSection section) => switch (section) {
    NowSocialSection.messages => '消息',
    NowSocialSection.contacts => '联系人',
    NowSocialSection.notifications => '通知',
  };
  bool get _canAdd =>
      widget.onFindPeople != null ||
      widget.onScanCode != null ||
      widget.onShowOwnCode != null;
  bool get _hasDetail =>
      _section == NowSocialSection.messages &&
      (widget.conversationBody != null || widget.messageRequestsBody != null);
  bool get _compact =>
      _personal &&
      _section == NowSocialSection.messages &&
      !_hasDetail &&
      !_allConversations;

  void _showAll(bool value) {
    setState(() => _allConversations = value);
    widget.onAllConversationsChanged?.call(value);
  }

  int? get _rowCount => _section == NowSocialSection.messages
      ? _conversations?.length
      : _contacts == _ContactsFilter.friends
      ? _friends?.length
      : _follows?.length;

  Widget _row(
    String name,
    String preview,
    VoidCallback? action, {
    int unread = 0,
    PersonAvatarMetadata? avatar,
    Widget? mediaLeading,
    IconData? entityIcon,
    DateTime? lastMessageAt,
    Widget? trailingAction,
    Key? key,
  }) => Semantics(
    label: unread > 0 ? '$name，$unread 条未读消息' : name,
    button: action != null,
    child: ListTile(
      key: key,
      contentPadding: const EdgeInsets.symmetric(
        horizontal: NowUiTokens.space4,
      ),
      minLeadingWidth: 28,
      horizontalTitleGap: NowUiTokens.space8,
      leading:
          mediaLeading ??
          (entityIcon != null
              ? SizedBox(
                  width: 30,
                  height: 30,
                  child: Icon(entityIcon, size: 24),
                )
              : PersonAvatar(
                  metadata: avatar,
                  radius: 15,
                  authorizationHeader:
                      widget.source.connections.authorizationHeader,
                  baseUrl: widget.source.connections.environment,
                  client: widget.source.connections.followClient,
                  identityChanges: widget.identityChanges,
                  current: () => _personal && widget.current(),
                )),
      title: Text(
        name,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: _bodyStyle.copyWith(fontWeight: FontWeight.w500),
      ),
      subtitle: preview.isEmpty
          ? null
          : Text(
              preview,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: NowUiTokens.of(context).metadata,
            ),
      trailing:
          trailingAction ??
          (unread > 0 || lastMessageAt != null
              ? ExcludeSemantics(
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    crossAxisAlignment: CrossAxisAlignment.end,
                    children: [
                      if (lastMessageAt != null)
                        Text(
                          _messageTime(lastMessageAt),
                          style: NowUiTokens.of(context).metadata,
                        ),
                      if (unread > 0)
                        Text(
                          unread > 99 ? '99+' : '$unread',
                          style: NowUiTokens.of(context).metadata.copyWith(
                            color: NowUiTokens.ownerMapOverlay.title,
                          ),
                        ),
                    ],
                  ),
                )
              : null),
      onTap: action,
    ),
  );

  Widget _addButton() => PopupMenuButton<VoidCallback>(
    key: const Key('edge-add-contact'),
    tooltip: '添加联系人',
    icon: const Icon(Icons.person_add_outlined, size: 20),
    constraints: const BoxConstraints(minWidth: 168),
    onSelected: (action) => action(),
    itemBuilder: (_) => [
      if (widget.onFindPeople != null)
        PopupMenuItem(
          value: _bound(widget.onFindPeople!),
          child: const Text('搜索用户'),
        ),
      if (widget.onScanCode != null)
        PopupMenuItem(
          value: _bound(widget.onScanCode!),
          child: const Text('扫一扫'),
        ),
      if (widget.onShowOwnCode != null)
        PopupMenuItem(
          value: _bound(widget.onShowOwnCode!),
          child: const Text('我的二维码'),
        ),
    ],
  );

  Widget _header() => Row(
    children: [
      if (_section == NowSocialSection.contacts ||
          (_section == NowSocialSection.messages &&
              _allConversations &&
              !_hasDetail))
        IconButton(
          key: const Key('edge-back-recent'),
          tooltip: '最近联系',
          onPressed: _bound(() {
            _showAll(false);
            _select(NowSocialSection.messages);
          }),
          icon: const Icon(Icons.arrow_back, size: 20),
        ),
      Expanded(
        child: Semantics(
          header: true,
          selected: true,
          child: Text(
            _section == NowSocialSection.messages
                ? (_hasDetail
                      ? widget.messageRequestsBody != null
                            ? '消息请求'
                            : '会话'
                      : _allConversations
                      ? '全部会话'
                      : '联系人')
                : _section == NowSocialSection.notifications
                ? widget.notificationTitle ?? '通知'
                : _sectionName(_section),
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: NowUiTokens.of(context).placeTitle,
          ),
        ),
      ),
      if (_section == NowSocialSection.contacts && _canAdd) _addButton(),
      if (widget.onClose != null)
        IconButton(
          key: const Key('edge-close'),
          tooltip: '关闭',
          onPressed: _bound(widget.onClose!),
          icon: const Icon(Icons.close, size: 20),
        ),
      if (_section == NowSocialSection.messages &&
          _allConversations &&
          !_hasDetail)
        IconButton(
          key: const ValueKey('edge-tab-contacts'),
          tooltip: '联系人管理',
          constraints: const BoxConstraints(
            minWidth: NowUiTokens.actionHitSize,
            minHeight: NowUiTokens.actionHitSize,
          ),
          color: Theme.of(context).colorScheme.onSurface,
          onPressed: _bound(() => _select(NowSocialSection.contacts)),
          icon: const Icon(Icons.people_outline, size: 20),
        ),
    ],
  );

  Widget _status(List<Widget> children) => Center(
    child: SingleChildScrollView(
      padding: const EdgeInsets.all(NowUiTokens.space8),
      child: Column(mainAxisSize: MainAxisSize.min, children: children),
    ),
  );

  Widget _empty() {
    final isMessages = _section == NowSocialSection.messages;
    final action = widget.onFindPeople;
    return _status([
      Text(
        isMessages
            ? '还没有消息'
            : _contacts == _ContactsFilter.friends
            ? '还没有好友'
            : '还没有关注的人',
        style: _bodyStyle,
      ),
      if (isMessages)
        TextButton(
          onPressed: _bound(() => _select(NowSocialSection.contacts)),
          child: Text('查看联系人', style: _actionStyle),
        )
      else if (action != null)
        TextButton(
          onPressed: _bound(action),
          child: Text('搜索用户', style: _actionStyle),
        ),
    ]);
  }

  Widget _failure({bool scrollable = true, Object? error}) {
    final failure = (error ?? _error);
    final expired =
        failure is NowSocialReadFailure && failure.statusCode == 401;
    final forbidden =
        failure is NowSocialReadFailure && failure.statusCode == 403;
    final message = failure is NowSocialReadFailure
        ? failure.message
        : failure is TimeoutException
        ? '连接超时，请重试。'
        : failure is FormatException
        ? '内容暂不可用，请重试。'
        : '连接中断，请重试。';
    final children = <Widget>[
      Text(message, style: _bodyStyle),
      if (!forbidden)
        TextButton(
          key: const Key('edge-retry'),
          style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
          onPressed: expired
              ? widget.onSignIn
              : _bound(() => unawaited(_load())),
          child: Text(expired ? '登录' : '重试', style: _actionStyle),
        ),
    ];
    return scrollable
        ? _status(children)
        : Padding(
            padding: const EdgeInsets.all(NowUiTokens.space8),
            child: Column(mainAxisSize: MainAxisSize.min, children: children),
          );
  }

  Widget _list() {
    if (_section == NowSocialSection.notifications) {
      return widget.notificationBody ??
          Center(child: Text('通知服务暂不可用。', style: _bodyStyle));
    }
    if (_hasDetail) {
      final requests = widget.messageRequestsBody;
      return Column(
        children: [
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton.icon(
              key: const Key('edge-back-messages'),
              onPressed: _bound(
                () => requests != null
                    ? widget.onCloseMessageRequests?.call()
                    : widget.onCloseConversation?.call(),
              ),
              icon: const Icon(Icons.arrow_back, size: 18),
              label: Text('最近联系', style: _actionStyle),
            ),
          ),
          Expanded(child: requests ?? widget.conversationBody!),
        ],
      );
    }
    final contacts = _section == NowSocialSection.contacts;
    final loading = contacts ? _contactsPage.loading : _loading;
    final error = contacts ? _contactsPage.error : _error;
    if (loading && _rowCount == null)
      return const Center(
        child: SizedBox(
          width: 20,
          height: 20,
          child: CircularProgressIndicator(strokeWidth: 2),
        ),
      );
    if (error != null && (!contacts || (_rowCount ?? 0) == 0)) {
      return _failure(error: error);
    }
    final rows = <Widget>[];
    if (_section == NowSocialSection.messages) {
      for (final chat in _conversations ?? const <HumanConversation>[]) {
        rows.add(
          _row(
            chat.otherName,
            chat.preview,
            _bound(() => _choose(chat)),
            unread: chat.unreadCount,
            lastMessageAt: chat.lastMessageAt,
            avatar: chat.avatar,
            key: ValueKey('edge-conversation-${chat.id}'),
          ),
        );
      }
      if (_nextConversationsCursor != null)
        rows.add(
          TextButton(
            key: const Key('edge-more-conversations'),
            onPressed: _loadingMore
                ? null
                : _bound(() => unawaited(_loadMoreConversations())),
            child: Text(
              _loadingMore
                  ? '正在加载'
                  : _moreError == null
                  ? '更多会话'
                  : '重试加载更多',
              style: _actionStyle,
            ),
          ),
        );
    } else if (_contacts == _ContactsFilter.friends) {
      for (final friend in _friends ?? const <FriendTie>[]) {
        rows.add(
          _row(
            friend.otherName,
            '',
            _bound(
              () => widget.onOpenFriend == null
                  ? widget.onOpenAuthor(friend.otherAccountId)
                  : widget.onOpenFriend!(friend),
            ),
            key: ValueKey('edge-friend-${friend.otherAccountId}'),
            avatar: friend.avatar,
            trailingAction: PopupMenuButton<VoidCallback>(
              tooltip: '好友操作',
              constraints: const BoxConstraints(minWidth: 160),
              icon: const Icon(Icons.more_horiz),
              onSelected: (action) => action(),
              itemBuilder: (_) => [
                PopupMenuItem(
                  value: _bound(
                    () => widget.onOpenAuthor(friend.otherAccountId),
                  ),
                  child: const Text('查看资料'),
                ),
                PopupMenuItem(
                  value: _bound(widget.onOpenManagement),
                  child: const Text('好友管理'),
                ),
              ],
            ),
          ),
        );
      }
    } else {
      for (final follow in _follows ?? const <NowFollowEntry>[]) {
        rows.add(
          _row(
            follow.label,
            follow.typeLabel,
            follow.isPerson
                ? _followBound(
                    follow,
                    () => widget.onOpenAuthor(follow.targetID),
                  )
                : widget.onOpenFollowTarget == null
                ? null
                : _followBound(
                    follow,
                    () => widget.onOpenFollowTarget!(follow),
                  ),
            key: ValueKey('edge-follow-${follow.type}-${follow.targetID}'),
            avatar: follow.avatar,
            mediaLeading: _followMedia(follow),
            entityIcon: follow.isPerson
                ? null
                : switch (follow.type) {
                    'COMMUNITY' => Icons.groups_outlined,
                    'ORGANIZATION' => Icons.account_balance_outlined,
                    _ => Icons.storefront_outlined,
                  },
          ),
        );
      }
    }
    if (contacts && _contactsPage.error != null) {
      rows.add(
        TextButton(
          onPressed: _bound(() => unawaited(_loadContacts())),
          style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
          child: const Text('刷新失败，重试'),
        ),
      );
    }
    if (contacts && _contactsPage.next != null) {
      rows.add(
        TextButton(
          key: ValueKey('edge-more-${_contacts.name}'),
          style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
          onPressed: _contactsPage.loading || _contactsPage.loadingMore
              ? null
              : _bound(
                  () => unawaited(
                    _loadContacts(more: !_contactsPage.restartRequired),
                  ),
                ),
          child: Text(
            _contactsPage.loadingMore
                ? '正在加载'
                : _contactsPage.restartRequired
                ? '列表已更新，重新加载'
                : _contactsPage.moreError != null
                ? '重试加载更多'
                : _contacts == _ContactsFilter.friends
                ? '更多好友'
                : '更多关注',
            style: _actionStyle,
          ),
        ),
      );
    }
    return rows.isEmpty
        ? _empty()
        : ListView(
            key: ValueKey('edge-list-${_section.name}-${_contacts.name}'),
            padding: const EdgeInsets.only(bottom: 12),
            children: rows,
          );
  }

  Widget _body(double maxHeight, double scale) {
    if (_binding.token == null || _binding.owner == null) {
      return ConstrainedBox(
        constraints: BoxConstraints(maxHeight: maxHeight),
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(NowUiTokens.space8),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('登录，与朋友保持联系', style: _bodyStyle),
              TextButton(
                key: const Key('edge-sign-in'),
                onPressed: widget.onSignIn,
                style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
                child: Text('登录', style: _actionStyle),
              ),
            ],
          ),
        ),
      );
    }
    if (_binding.workspace != null)
      return TextButton(
        key: const Key('edge-use-personal'),
        onPressed: widget.onUsePersonal,
        child: const Text('切回个人身份'),
      );
    if (!_personal) return Text('当前连接已变化，请重新打开。', style: _bodyStyle);
    if (_compact) return _recentAvatars(maxHeight);
    if (_section == NowSocialSection.notifications) {
      return ConstrainedBox(
        constraints: BoxConstraints(maxHeight: maxHeight),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            _header(),
            Flexible(fit: FlexFit.loose, child: _list()),
          ],
        ),
      );
    }
    final controlsHeight =
        math.max(NowUiTokens.actionHitSize, 24 * scale) +
        (_section == NowSocialSection.contacts
            ? math.max(48, 21 * scale + 16)
            : 0) +
        (_section == NowSocialSection.messages &&
                !_hasDetail &&
                widget.onOpenMessageRequests != null
            ? math.max(48, 21 * scale + 16)
            : 0);
    final rowsHeight = _hasDetail
        ? maxHeight
        : _rowCount == null || _rowCount == 0 || _error != null
        ? math.max(72, 56 * scale)
        : math.min(_rowCount!, 4) * math.max(72, 28 + 34 * scale);
    final height = math.min(maxHeight, controlsHeight + rowsHeight);
    return SizedBox(
      height: height,
      child: Column(
        children: [
          _header(),
          if (_section == NowSocialSection.contacts)
            Row(
              children: [
                for (final filter in _ContactsFilter.values)
                  Semantics(
                    selected: filter == _contacts,
                    child: TextButton(
                      key: ValueKey('edge-filter-${filter.name}'),
                      style: TextButton.styleFrom(
                        minimumSize: const Size(48, 48),
                        foregroundColor: Theme.of(
                          context,
                        ).colorScheme.onSurface,
                        textStyle: _actionStyle.copyWith(
                          fontWeight: filter == _contacts
                              ? FontWeight.w600
                              : FontWeight.w400,
                        ),
                      ),
                      onPressed: _bound(() {
                        if (filter == _contacts) return;
                        setState(() {
                          ++_serial;
                          _retireContactPages();
                          _contacts = filter;
                          _error = null;
                        });
                        widget.onContactsFilterChanged?.call(
                          filter == _ContactsFilter.follows,
                        );
                        unawaited(_load());
                      }),
                      child: Text(
                        filter == _ContactsFilter.friends ? '好友' : '关注',
                      ),
                    ),
                  ),
              ],
            ),
          Expanded(child: _list()),
          if (_section == NowSocialSection.messages &&
              !_hasDetail &&
              widget.onOpenMessageRequests != null)
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton(
                key: const Key('edge-message-requests'),
                onPressed: _bound(widget.onOpenMessageRequests!),
                child: Text('消息请求', style: _actionStyle),
              ),
            ),
        ],
      ),
    );
  }

  double _lineHeight(TextStyle style) {
    final painter = TextPainter(
      text: TextSpan(text: '国Ag', style: style),
      textDirection: Directionality.of(context),
      textScaler: MediaQuery.textScalerOf(context),
      maxLines: 1,
    )..layout();
    final height = painter.height;
    painter.dispose();
    return height;
  }

  String _messageTime(DateTime value) {
    final local = value.toLocal(), now = DateTime.now();
    return local.year == now.year &&
            local.month == now.month &&
            local.day == now.day
        ? '${local.hour.toString().padLeft(2, '0')}:${local.minute.toString().padLeft(2, '0')}'
        : '${local.month}/${local.day}';
  }

  Widget _allConversationsButton() => TextButton(
    key: const Key('edge-all-conversations'),
    style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
    onPressed: _bound(() => _showAll(true)),
    child: Text(
      '全部会话',
      maxLines: 1,
      overflow: TextOverflow.ellipsis,
      style: _actionStyle,
    ),
  );

  Widget _recentAvatars(double maxHeight) {
    if (_error != null) {
      // Keep the message and recovery action together at their natural height.
      // Only the whole local surface scrolls when the available height is short.
      return ConstrainedBox(
        key: const Key('edge-recent-avatars'),
        constraints: BoxConstraints(maxHeight: maxHeight),
        child: SingleChildScrollView(
          key: const Key('edge-recent-status-scroll'),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [_failure(scrollable: false), _allConversationsButton()],
          ),
        ),
      );
    }
    final tokens = NowUiTokens.of(context);
    final nameStyle = tokens.body.copyWith(fontWeight: FontWeight.w500);
    final rowHeight = math.max(
      NowUiTokens.socialAvatarSize + 12,
      12 + _lineHeight(nameStyle) + 2 + _lineHeight(tokens.metadata),
    );
    final controlsHeight = math.max(
      NowUiTokens.actionHitSize,
      _lineHeight(_actionStyle) + 16,
    );
    final recent = (_conversations ?? const <HumanConversation>[])
        .take(NowUiTokens.socialRecentBaseline)
        .toList(growable: false);
    final showStatus = recent.isEmpty;
    final statusHeight = math.min(
      rowHeight,
      math.max(NowUiTokens.actionHitSize, maxHeight - controlsHeight),
    );
    final height = math.min(
      maxHeight,
      controlsHeight + (showStatus ? statusHeight : recent.length * rowHeight),
    );
    return SizedBox(
      key: const Key('edge-recent-avatars'),
      height: height,
      // All four recent people remain reachable at large text sizes or with
      // the keyboard open. Scrolling never adds another category or request.
      child: ListView(
        key: const Key('edge-recent-list'),
        padding: EdgeInsets.zero,
        children: [
          if (showStatus)
            SizedBox(
              height: statusHeight,
              child: _loading && _conversations == null
                  ? const Center(
                      child: SizedBox(
                        width: 20,
                        height: 20,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      ),
                    )
                  : Center(child: Text('暂无会话', style: tokens.metadata)),
            )
          else
            for (final chat in recent)
              SizedBox(
                height: rowHeight,
                child: Semantics(
                  key: ValueKey('edge-recent-semantics-${chat.id}'),
                  label: [
                    chat.otherName,
                    if (chat.unreadCount > 0) '${chat.unreadCount} 条未读消息',
                  ].join('，'),
                  button: true,
                  excludeSemantics: true,
                  onTap: _bound(() => _choose(chat)),
                  child: InkWell(
                    key: ValueKey('edge-conversation-${chat.id}'),
                    onTap: _bound(() => _choose(chat)),
                    child: Padding(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 8,
                        vertical: 6,
                      ),
                      child: Row(
                        children: [
                          Badge(
                            key: ValueKey('edge-recent-unread-${chat.id}'),
                            isLabelVisible: chat.unreadCount > 0,
                            child: PersonAvatar(
                              metadata: chat.avatar,
                              radius: NowUiTokens.socialAvatarSize / 2,
                              authorizationHeader:
                                  widget.source.connections.authorizationHeader,
                              baseUrl: widget.source.connections.environment,
                              client: widget.source.connections.followClient,
                              identityChanges: widget.identityChanges,
                              current: () => _personal && widget.current(),
                            ),
                          ),
                          const SizedBox(width: 8),
                          Expanded(
                            child: Column(
                              mainAxisAlignment: MainAxisAlignment.center,
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(
                                  chat.otherName,
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                  style: nameStyle,
                                ),
                                if (chat.preview.isNotEmpty)
                                  Text(
                                    chat.preview,
                                    maxLines: 1,
                                    overflow: TextOverflow.ellipsis,
                                    style: tokens.metadata,
                                  ),
                              ],
                            ),
                          ),
                        ],
                      ),
                    ),
                  ),
                ),
              ),
          Row(
            children: [
              Expanded(child: _allConversationsButton()),
              if (widget.onClose != null)
                IconButton(
                  key: const Key('edge-close'),
                  tooltip: '关闭',
                  onPressed: _bound(widget.onClose!),
                  icon: const Icon(Icons.close, size: 20),
                ),
            ],
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    if (!widget.open) return const SizedBox.shrink();
    return LayoutBuilder(
      builder: (context, constraints) {
        final media = MediaQuery.of(context);
        final scale = media.textScaler.scale(15) / 15;
        final availableWidth = constraints.maxWidth.isFinite
            ? math.min(
                constraints.maxWidth,
                media.size.width - NowUiTokens.actionHitSize,
              )
            : media.size.width - NowUiTokens.actionHitSize;
        final width = math.max(
          0.0,
          math.min(
            availableWidth,
            (_compact
                    ? NowUiTokens.socialRailWidth
                    : NowUiTokens.socialPanelWidth) *
                scale.clamp(1, 1.5),
          ),
        );
        final height = math.max(
          0.0,
          math.min(
            constraints.maxHeight.isFinite
                ? constraints.maxHeight
                : NowUiTokens.socialPanelMaxHeight,
            NowUiTokens.socialPanelMaxHeight,
          ),
        );
        return Align(
          alignment: Alignment.topRight,
          widthFactor: 1,
          heightFactor: 1,
          child: SizedBox(
            key: const Key('now-social-edge'),
            width: width,
            child: NowGlassSurface(
              key: const Key('now-social-local-surface'),
              child: Padding(
                key: const Key('now-social-edge-column'),
                padding: const EdgeInsets.symmetric(horizontal: 4),
                child: _body(height, scale),
              ),
            ),
          ),
        );
      },
    );
  }
}
