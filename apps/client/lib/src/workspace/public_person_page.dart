import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../app/birdtie_app_bar.dart';
import '../config/birdtie_environment.dart';
import 'connections.dart';
import 'human_causal_operation_controller.dart';
import 'follow_button.dart';
import 'shared_social_context_panel.dart';
import '../auth/birdtie_auth_controller.dart';
import 'entity_action_contract.dart';
import 'entity_action_dispatcher.dart';
import 'now_ui_tokens.dart';
import 'human_visibility.dart';
import 'person_avatar.dart';

class PublicPersonPage extends StatefulWidget {
  const PublicPersonPage({
    super.key,
    required this.accountID,
    required this.authorizationHeader,
    this.apiBaseUrl,
    this.client,
    this.identityChanges,
    this.workspaceID,
    this.auth,
    this.embedded = false,
    this.onOpenOwnProfile,
    this.onOpenPublicMoments,
  });

  final String accountID;
  final String? Function() authorizationHeader;
  final String? apiBaseUrl;
  final http.Client? client;
  final Listenable? identityChanges;
  final String? Function()? workspaceID;
  final BirdtieAuthController? auth;
  final bool embedded;
  final Future<void> Function()? onOpenOwnProfile;
  final Future<void> Function()? onOpenPublicMoments;

  @override
  State<PublicPersonPage> createState() => _PublicPersonPageState();
}

class _PublicPersonPageState extends State<PublicPersonPage> {
  Map<String, dynamic>? _profile;
  String? _error;
  late http.Client _client = widget.client ?? http.Client();
  bool _ownsClient = false;
  String get _base => widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  int _epoch = 0;
  int? _profileEpoch;
  int? _openingProfileEpoch;
  int? _openingMomentsEpoch;
  int _publicNavigationEpoch = 0;
  bool _proposing = false;
  late (String?, String?, String?) _identity;
  late Listenable _actionChanges;
  final _shareSource = HumanCausalSourceSerial();
  void _observeShareSource() {
    _shareSource.observe((
      widget.accountID,
      widget.auth,
      widget.client,
      widget.apiBaseUrl,
      widget.auth?.accountID,
      widget.workspaceID?.call(),
    ));
    _shareSource.notifyReadBindingChanged();
  }

  (String?, String?, String?) get _current => (
    widget.authorizationHeader(),
    widget.workspaceID?.call(),
    widget.auth?.accountID,
  );
  bool get _profileCurrent =>
      _profile != null && _profileEpoch == _epoch && _error == null;
  bool get _personalSource =>
      _current.$1 != null &&
      _current.$2 == null &&
      (widget.auth == null ||
          (widget.auth!.signedIn &&
              widget.auth!.accountID != null &&
              widget.auth!.authorizationHeader == _current.$1));
  bool get _ownProfile =>
      _personalSource &&
      widget.auth != null &&
      widget.auth!.accountID == widget.accountID;
  bool get _publicPersonalSource =>
      _profileCurrent && _profile!['visibility'] == 'public' && _personalSource;
  bool get _otherPublicPerson => _publicPersonalSource && !_ownProfile;
  bool get _publicMomentsSource =>
      _profileCurrent &&
      _profile!['visibility'] == 'public' &&
      _current.$2 == null &&
      (widget.auth == null ||
          (widget.auth!.authorizationHeader == _current.$1 &&
              (_current.$1 == null ||
                  (widget.auth!.signedIn && widget.auth!.accountID != null))));

  @override
  void dispose() {
    _shareSource.dispose();
    ++_epoch;
    widget.identityChanges?.removeListener(_changed);
    widget.auth?.removeListener(_changed);
    if (_ownsClient) _client.close();
    super.dispose();
  }

  @override
  void initState() {
    super.initState();
    _observeShareSource();
    _ownsClient = widget.client == null;
    _identity = _current;
    _actionChanges = Listenable.merge([widget.identityChanges, widget.auth]);
    widget.identityChanges?.addListener(_changed);
    widget.auth?.addListener(_changed);
    _load();
  }

  void _changed() {
    _observeShareSource();
    if (_identity == _current) return;
    _identity = _current;
    ++_epoch;
    _profile = null;
    _load();
  }

  @override
  void didUpdateWidget(PublicPersonPage old) {
    super.didUpdateWidget(old);
    _observeShareSource();
    if (!identical(old.onOpenPublicMoments, widget.onOpenPublicMoments)) {
      ++_publicNavigationEpoch;
    }
    final transportChanged =
        !identical(old.client, widget.client) ||
        old.apiBaseUrl != widget.apiBaseUrl;
    if (!identical(old.client, widget.client)) {
      if (_ownsClient) _client.close();
      _client = widget.client ?? http.Client();
      _ownsClient = widget.client == null;
    }

    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges?.removeListener(_changed);
      widget.identityChanges?.addListener(_changed);
    }
    if (!identical(old.auth, widget.auth)) {
      old.auth?.removeListener(_changed);
      widget.auth?.addListener(_changed);
    }
    if (!identical(old.auth, widget.auth) ||
        !identical(old.identityChanges, widget.identityChanges)) {
      _actionChanges = Listenable.merge([widget.identityChanges, widget.auth]);
    }
    if (transportChanged ||
        !identical(old.authorizationHeader, widget.authorizationHeader) ||
        !identical(old.workspaceID, widget.workspaceID) ||
        !identical(old.identityChanges, widget.identityChanges) ||
        !identical(old.auth, widget.auth) ||
        old.accountID != widget.accountID ||
        _identity != _current) {
      _identity = _current;
      ++_epoch;
      _profile = null;
      _load();
    }
  }

  Future<void> _load() async {
    final epoch = ++_epoch, identity = _current, id = widget.accountID;
    setState(() => _error = null);
    try {
      final headers = <String, String>{};
      final auth = widget.authorizationHeader();
      if (auth != null) headers['Authorization'] = auth;
      final response = await _client
          .get(
            Uri.parse(
              '${_base.replaceFirst(RegExp(r'/$'), '')}/v1/accounts/${Uri.encodeComponent(widget.accountID)}/profile',
            ),
            headers: headers,
          )
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) {
        throw StateError('profile_unavailable');
      }
      if (mounted &&
          epoch == _epoch &&
          identity == _current &&
          id == widget.accountID) {
        final value =
            (jsonDecode(utf8.decode(response.bodyBytes))
                    as Map<String, dynamic>)['data']
                as Map<String, dynamic>;
        if (value['accountId'] != id) throw const FormatException('个人标识不符');
        humanVisibility(value['visibility']);
        PersonAvatarMetadata.optional(value['avatar'], id);
        setState(() {
          _profile = value;
          _profileEpoch = epoch;
        });
      }
    } catch (_) {
      if (mounted && epoch == _epoch && identity == _current) {
        setState(() => _error = '个人资料暂不可访问。');
      }
    }
  }

  Future<void> _personAction(EntityActionKind kind) async {
    if (_proposing ||
        !_publicPersonalSource ||
        (kind != EntityActionKind.share && _ownProfile)) {
      return;
    }
    _proposing = true;
    try {
      final identity = _current,
          epoch = _epoch,
          id = widget.accountID,
          auth = widget.auth;
      bool current() =>
          mounted &&
          epoch == _epoch &&
          identity == _current &&
          id == widget.accountID &&
          auth == widget.auth &&
          _publicPersonalSource &&
          (kind == EntityActionKind.share || !_ownProfile);
      String? note;
      if (kind == EntityActionKind.connect) {
        note = await showDialog<String>(
          context: context,
          builder: (_) =>
              _FriendNoteDialog(current: current, changes: _actionChanges),
        );
        if (note == null || !current()) return;
      }
      if (kind == EntityActionKind.message && auth == null) return;
      if (!mounted || !current()) return;
      await runEntityAction(
        context,
        ref: EntityActionRef('person', id),
        kind: kind,
        authorizationHeader: () => current() ? identity.$1 : null,
        workspaceID: widget.workspaceID,
        identityChanges: _actionChanges,
        accountID: () => auth?.accountID,
        client: _client,
        apiBaseUrl: _base,
        domainCurrent: current,
        reviewDetails: (_) => note == null ? '' : '申请内容：$note',
        handler: (approved) async {
          if (!current()) return;
          final source = ConnectionSource(
            authorizationHeader: () => current() ? identity.$1 : null,
            client: _client,
            apiBaseUrl: _base,
          );
          try {
            if (kind == EntityActionKind.connect) {
              await source.requestFriend(id, note!, approved: approved);
              return;
            }
            if (kind == EntityActionKind.share) {
              await shareEntityToChat(
                context,
                authorizationHeader: () => widget.authorizationHeader(),
                identityChanges: _shareSource,
                ownerID: widget.auth == null
                    ? null
                    : () => widget.auth?.accountID,
                workspaceID: () => widget.workspaceID?.call(),
                sourceKey: () => _shareSource.generation,
                sourceCurrent: () => mounted,
                type: 'person',
                id: id,
                apiBaseUrl: _base,
                client: _client,
              );
              return;
            }
            if (kind == EntityActionKind.message) {
              final ties = await source.ties();
              if (!current()) return;
              final matches = ties
                  .where((t) => t.otherAccountId == id)
                  .toList();
              if (matches.length != 1) throw StateError('当前好友关系不可用');
              final conversation = await source.startFriendChat(
                matches.single.id,
                approved: approved,
              );
              if (!mounted || !current()) return;
              await Navigator.of(context).push(
                MaterialPageRoute<void>(
                  builder: (_) => HumanConversationRoute(
                    auth: auth!,
                    conversation: conversation,
                    workspaceChanges: _actionChanges,
                    workspaceID: widget.workspaceID,
                    apiBaseUrl: _base,
                    client: _client,
                  ),
                ),
              );
            }
          } finally {
            source.dispose();
          }
        },
      );
    } finally {
      _proposing = false;
    }
  }

  Future<void> Function()? _ownProfileAction() {
    final open = widget.onOpenOwnProfile;
    if (open == null || !_profileCurrent || !_ownProfile) return null;
    final epoch = _epoch,
        identity = _current,
        id = widget.accountID,
        auth = widget.auth,
        client = _client,
        base = _base;
    bool current() =>
        mounted &&
        epoch == _epoch &&
        identity == _current &&
        id == widget.accountID &&
        identical(auth, widget.auth) &&
        identical(client, _client) &&
        base == _base &&
        identical(open, widget.onOpenOwnProfile) &&
        _profileCurrent &&
        _ownProfile;
    return () async {
      if (!current() || _openingProfileEpoch == epoch) return;
      setState(() => _openingProfileEpoch = epoch);
      try {
        await open();
        if (current()) await _load();
      } catch (_) {
        if (current()) setState(() => _error = '个人资料暂不可访问。');
      } finally {
        if (mounted && _openingProfileEpoch == epoch) {
          setState(() => _openingProfileEpoch = null);
        }
      }
    };
  }

  Future<void> Function()? _publicMomentsAction() {
    final open = widget.onOpenPublicMoments;
    if (open == null || !_publicMomentsSource) return null;
    final epoch = _epoch,
        navigationEpoch = _publicNavigationEpoch,
        identity = _current,
        id = widget.accountID,
        auth = widget.auth,
        client = _client,
        base = _base;
    bool current() =>
        mounted &&
        epoch == _epoch &&
        navigationEpoch == _publicNavigationEpoch &&
        identity == _current &&
        id == widget.accountID &&
        identical(auth, widget.auth) &&
        identical(client, _client) &&
        base == _base &&
        identical(open, widget.onOpenPublicMoments) &&
        _publicMomentsSource;
    return () async {
      if (!current() || _openingMomentsEpoch == epoch) return;
      setState(() => _openingMomentsEpoch = epoch);
      try {
        await open();
      } catch (_) {
        if (current()) {
          setState(() => _error = '当前范围的公开动态暂不可访问，请重新打开。');
        }
      } finally {
        if (mounted && _openingMomentsEpoch == epoch) {
          setState(() => _openingMomentsEpoch = null);
        }
      }
    };
  }

  @override
  Widget build(BuildContext context) {
    final body = _body(context);
    if (widget.embedded) return body;
    return Scaffold(
      appBar: birdtieAppBar(
        context,
        title: Text('个人主页', style: NowUiTokens.of(context).pageTitle),
      ),
      body: body,
    );
  }

  Widget _body(BuildContext context) {
    final tokens = NowUiTokens.of(context);
    final textActionStyle = TextButton.styleFrom(
      minimumSize: const Size(48, 48),
      textStyle: tokens.action,
    );
    final outlinedActionStyle = OutlinedButton.styleFrom(
      minimumSize: const Size(48, 48),
      textStyle: tokens.action,
    );
    return _error != null
        ? Center(
            child: TextButton(
              style: textActionStyle,
              onPressed: _load,
              child: Text('${_error!} 点击重试'),
            ),
          )
        : _profile == null
        ? const Center(child: CircularProgressIndicator())
        : ListView(
            key: PageStorageKey('public-person-scroll:${widget.accountID}'),
            primary: false,
            padding: const EdgeInsets.all(16),
            children: [
              Align(
                alignment: Alignment.centerLeft,
                child: PersonAvatar(
                  metadata: PersonAvatarMetadata.optional(
                    _profile!['avatar'],
                    widget.accountID,
                  ),
                  authorizationHeader: widget.authorizationHeader,
                  baseUrl: _base,
                  client: _client,
                  identityChanges: _actionChanges,
                  current: () => _profileCurrent && _current.$2 == null,
                  radius: 32,
                ),
              ),
              const SizedBox(height: 12),
              Text(
                _profile!['displayName'] as String? ?? 'Birdtie 用户',
                style: tokens.pageTitle,
              ),
              const SizedBox(height: 8),
              Text(
                '资料可见范围：${humanVisibilityLabels[_profile!['visibility']]}',
                style: tokens.metadata,
              ),
              if (_publicMomentsSource && widget.onOpenPublicMoments != null)
                TextButton.icon(
                  onPressed: _openingMomentsEpoch == _epoch
                      ? null
                      : _publicMomentsAction(),
                  style: textActionStyle,
                  icon: const Icon(Icons.map_outlined),
                  label: const Text('当前范围的公开动态'),
                ),
              if (_profileCurrent &&
                  _ownProfile &&
                  widget.onOpenOwnProfile != null)
                TextButton.icon(
                  onPressed: _openingProfileEpoch == _epoch
                      ? null
                      : _ownProfileAction(),
                  style: textActionStyle,
                  icon: const Icon(Icons.edit_outlined),
                  label: const Text('编辑我的资料与隐私'),
                ),
              if (_otherPublicPerson)
                FollowButton(
                  targetType: 'PERSON',
                  targetID: widget.accountID,
                  accountID: () => widget.auth?.accountID,
                  identityChanges: _actionChanges,
                  workspaceID: widget.workspaceID,
                  authorizationHeader: widget.authorizationHeader,
                  apiBaseUrl: _base,
                  client: _client,
                ),
              if (_publicPersonalSource)
                TextButton.icon(
                  style: textActionStyle,
                  onPressed: () => _personAction(EntityActionKind.share),
                  icon: const Icon(Icons.chat_bubble_outline),
                  label: const Text('发给好友'),
                ),
              if (_otherPublicPerson)
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    OutlinedButton.icon(
                      onPressed: () => _personAction(EntityActionKind.connect),
                      style: outlinedActionStyle,
                      icon: const Icon(Icons.person_add_alt_outlined),
                      label: const Text('添加好友'),
                    ),
                    if (widget.auth != null)
                      OutlinedButton.icon(
                        onPressed: () =>
                            _personAction(EntityActionKind.message),
                        style: outlinedActionStyle,
                        icon: const Icon(Icons.chat_bubble_outline),
                        label: const Text('打开私信'),
                      ),
                  ],
                ),
              const SizedBox(height: 16),
              if ((_profile!['bio'] as String? ?? '').isNotEmpty)
                Text(_profile!['bio'] as String, style: tokens.body),
              if (_otherPublicPerson)
                SharedSocialContextPanel(
                  identityChanges: _actionChanges,
                  workspaceID: widget.workspaceID,
                  accountID: widget.accountID,
                  authorizationHeader: widget.authorizationHeader,
                  apiBaseUrl: _base,
                  client: _client,
                ),
            ],
          );
  }
}

class _FriendNoteDialog extends StatefulWidget {
  const _FriendNoteDialog({required this.current, this.changes});
  final bool Function() current;
  final Listenable? changes;
  @override
  State<_FriendNoteDialog> createState() => _FriendNoteDialogState();
}

class _FriendNoteDialogState extends State<_FriendNoteDialog> {
  final _input = TextEditingController();
  @override
  void dispose() {
    _input.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: Listenable.merge([_input, widget.changes]),
    builder: (_, _) => AlertDialog(
      title: Text('好友申请', style: NowUiTokens.of(context).sheetTitle),
      content: SingleChildScrollView(
        child: widget.current()
            ? TextField(
                controller: _input,
                style: NowUiTokens.of(context).composer,
                maxLength: 280,
                maxLines: 3,
                decoration: InputDecoration(
                  labelText: '给对方的说明',
                  helperText: '需对方接受后成为好友，不自动发送私信。',
                  labelStyle: NowUiTokens.of(context).body,
                  helperStyle: NowUiTokens.of(context).metadata,
                  helperMaxLines: 4,
                ),
              )
            : Text('身份或页面来源已变化，请取消后重新打开。', style: NowUiTokens.of(context).body),
      ),
      actions: [
        TextButton(
          style: TextButton.styleFrom(
            minimumSize: const Size(48, 48),
            textStyle: NowUiTokens.of(context).action,
          ),
          onPressed: () => Navigator.pop(context),
          child: const Text('取消'),
        ),
        FilledButton(
          style: FilledButton.styleFrom(
            minimumSize: const Size(48, 48),
            textStyle: NowUiTokens.of(context).action,
          ),
          onPressed: widget.current() && _input.text.trim().isNotEmpty
              ? () => Navigator.pop(context, _input.text.trim())
              : null,
          child: const Text('检查申请'),
        ),
      ],
    ),
  );
}
