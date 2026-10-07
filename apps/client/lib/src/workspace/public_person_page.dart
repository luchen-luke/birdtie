import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../config/birdtie_environment.dart';
import 'connections.dart';
import 'follow_button.dart';
import 'shared_social_context_panel.dart';
import '../auth/birdtie_auth_controller.dart';
import 'entity_action_contract.dart';
import 'entity_action_dispatcher.dart';

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
  });

  final String accountID;
  final String? Function() authorizationHeader;
  final String? apiBaseUrl;
  final http.Client? client;
  final Listenable? identityChanges;
  final String? Function()? workspaceID;
  final BirdtieAuthController? auth;

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
  bool _proposing = false;
  late (String?, String?) _identity;
  (String?, String?) get _current =>
      (widget.authorizationHeader(), widget.workspaceID?.call());

  @override
  void dispose() {
    ++_epoch;
    widget.identityChanges?.removeListener(_changed);
    if (_ownsClient) _client.close();
    super.dispose();
  }

  @override
  void initState() {
    super.initState();
    _ownsClient = widget.client == null;
    _identity = _current;
    widget.identityChanges?.addListener(_changed);
    _load();
  }

  void _changed() {
    if (_identity == _current) return;
    _identity = _current;
    ++_epoch;
    _profile = null;
    _load();
  }

  @override
  void didUpdateWidget(PublicPersonPage old) {
    super.didUpdateWidget(old);
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
        setState(() => _profile = value);
      }
    } catch (_) {
      if (mounted && epoch == _epoch && identity == _current) {
        setState(() => _error = '个人资料暂不可访问。');
      }
    }
  }

  Future<void> _personAction(EntityActionKind kind) async {
    if (_proposing) return;
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
          auth == widget.auth;
      String? note;
      if (kind == EntityActionKind.connect) {
        note = await showDialog<String>(
          context: context,
          builder: (_) => _FriendNoteDialog(
            current: current,
            changes: widget.identityChanges,
          ),
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
        identityChanges: widget.identityChanges,
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
                authorizationHeader: () => current() ? identity.$1 : null,
                identityChanges: widget.identityChanges,
                workspaceID: widget.workspaceID,
                apiBaseUrl: _base,
                client: _client,
                type: 'person',
                id: id,
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
                    workspaceChanges: widget.identityChanges,
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

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('主办人')),
    body: _error != null
        ? Center(
            child: TextButton(onPressed: _load, child: Text('${_error!} 点击重试')),
          )
        : _profile == null
        ? const Center(child: CircularProgressIndicator())
        : ListView(
            padding: const EdgeInsets.all(24),
            children: [
              Text(
                _profile!['displayName'] as String? ?? 'Birdtie 用户',
                style: Theme.of(context).textTheme.headlineSmall,
              ),
              if (_profile!['visibility'] == 'public')
                FollowButton(
                  targetType: 'PERSON',
                  targetID: widget.accountID,
                  authorizationHeader: widget.authorizationHeader,
                  apiBaseUrl: _base,
                  client: _client,
                ),
              if (_profile!['visibility'] == 'public')
                TextButton.icon(
                  onPressed: () => _personAction(EntityActionKind.share),
                  icon: const Icon(Icons.chat_bubble_outline),
                  label: const Text('发给好友'),
                ),
              if (_profile!['visibility'] == 'public' &&
                  widget.authorizationHeader() != null &&
                  widget.workspaceID?.call() == null)
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    OutlinedButton.icon(
                      onPressed: () => _personAction(EntityActionKind.connect),
                      style: OutlinedButton.styleFrom(
                        minimumSize: const Size(48, 48),
                      ),
                      icon: const Icon(Icons.person_add_alt_outlined),
                      label: const Text('申请连接'),
                    ),
                    if (widget.auth != null)
                      OutlinedButton.icon(
                        onPressed: () =>
                            _personAction(EntityActionKind.message),
                        style: OutlinedButton.styleFrom(
                          minimumSize: const Size(48, 48),
                        ),
                        icon: const Icon(Icons.chat_bubble_outline),
                        label: const Text('打开私信'),
                      ),
                  ],
                ),
              const SizedBox(height: 16),
              if ((_profile!['bio'] as String? ?? '').isNotEmpty)
                Text(_profile!['bio'] as String),
              if (_profile!['visibility'] == 'public')
                SharedSocialContextPanel(
                  identityChanges: widget.identityChanges,
                  workspaceID: widget.workspaceID,
                  accountID: widget.accountID,
                  authorizationHeader: widget.authorizationHeader,
                  apiBaseUrl: _base,
                  client: _client,
                ),
            ],
          ),
  );
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
      title: const Text('好友申请'),
      content: SingleChildScrollView(
        child: widget.current()
            ? TextField(
                controller: _input,
                maxLength: 280,
                maxLines: 3,
                decoration: const InputDecoration(
                  labelText: '给对方的说明',
                  helperText: '需对方接受后成为好友，不自动发送私信。',
                ),
              )
            : const Text('身份或页面来源已变化，请取消后重新打开。'),
      ),
      actions: [
        TextButton(
          style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
          onPressed: () => Navigator.pop(context),
          child: const Text('取消'),
        ),
        FilledButton(
          style: FilledButton.styleFrom(minimumSize: const Size(48, 48)),
          onPressed: widget.current() && _input.text.trim().isNotEmpty
              ? () => Navigator.pop(context, _input.text.trim())
              : null,
          child: const Text('检查申请'),
        ),
      ],
    ),
  );
}
