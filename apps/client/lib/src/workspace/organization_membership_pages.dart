import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../auth/birdtie_auth_controller.dart';
import 'organization_membership_api.dart';

String memberRoleLabel(String role) => switch (role) {
  'owner' => '所有者',
  'admin' => '管理员',
  'moderator' => '协管员',
  'member' => '成员',
  _ => '未知角色',
};

class OrganizationMembersPage extends StatefulWidget {
  const OrganizationMembersPage({
    super.key,
    required this.organizationID,
    required this.organizationName,
    required this.currentRole,
    required this.authorizationHeader,
    this.api,
  });
  final String organizationID;
  final String organizationName;
  final String currentRole;
  final String? Function() authorizationHeader;
  final OrganizationMembershipApi? api;

  @override
  State<OrganizationMembersPage> createState() =>
      _OrganizationMembersPageState();
}

class _OrganizationMembersPageState extends State<OrganizationMembersPage> {
  late final OrganizationMembershipApi _api;
  List<OrganizationMember> _members = [];
  bool _loading = true;
  bool _busy = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _api =
        widget.api ??
        OrganizationMembershipApi(
          authorizationHeader: widget.authorizationHeader,
        );
    _load();
  }

  @override
  void dispose() {
    if (widget.api == null) _api.dispose();
    super.dispose();
  }

  String _message(Object error) => error is OrganizationMembershipException
      ? error.message
      : '网络暂不可用，请稍后重试。';

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final members = await _api.list(widget.organizationID);
      if (mounted) setState(() => _members = members);
    } catch (error) {
      if (mounted) setState(() => _error = _message(error));
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _run(Future<void> Function() action) async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await action();
      await _load();
    } catch (error) {
      if (mounted) setState(() => _error = _message(error));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _invite() async {
    final account = TextEditingController();
    var role = 'member';
    final result = await showDialog<(String, String)>(
      context: context,
      builder: (context) => StatefulBuilder(
        builder: (context, update) => AlertDialog(
          title: const Text('邀请组织成员'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Text('请成员在“我的组织邀请”中复制账号 ID，确认身份后填写。邀请会出现在该账号内。'),
              const SizedBox(height: 12),
              TextField(
                key: const Key('member_account_id'),
                controller: account,
                decoration: const InputDecoration(labelText: '成员账号 ID'),
              ),
              DropdownButton<String>(
                value: role,
                items: [
                  for (final value in [
                    'member',
                    'moderator',
                    if (widget.currentRole == 'owner') 'admin',
                  ])
                    DropdownMenuItem(
                      value: value,
                      child: Text(memberRoleLabel(value)),
                    ),
                ],
                onChanged: (value) => update(() => role = value ?? 'member'),
              ),
            ],
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () =>
                  Navigator.pop(context, (account.text.trim(), role)),
              child: const Text('发送站内邀请'),
            ),
          ],
        ),
      ),
    );
    // Wait for the dialog to release its field before disposing the controller.
    await Future<void>.delayed(const Duration(milliseconds: 350));
    account.dispose();
    if (result == null || !mounted) return;
    await _run(() async {
      await _api.invite(widget.organizationID, result.$1, result.$2);
    });
  }

  Future<void> _changeRole(OrganizationMember member, String role) async =>
      _run(() async {
        await _api.changeRole(widget.organizationID, member.id, role);
      });

  Future<void> _revoke(OrganizationMember member) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('移除组织成员'),
        content: Text('移除“${member.displayName}”后，该账号将立即失去此组织的工作区权限。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('返回'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('确认移除'),
          ),
        ],
      ),
    );
    if (confirmed == true && mounted) {
      await _run(() => _api.revoke(widget.organizationID, member.id));
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: Text('${widget.organizationName} · 成员')),
    body: RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          const Text('邀请、角色和移除均由服务器检查权限并记录审计。'),
          const SizedBox(height: 12),
          FilledButton.icon(
            key: const Key('organization_invite_member'),
            onPressed: _busy ? null : _invite,
            icon: const Icon(Icons.person_add_alt_1),
            label: const Text('邀请成员'),
          ),
          if (_loading) const Center(child: CircularProgressIndicator()),
          if (_error != null) ...[
            Text(
              _error!,
              style: TextStyle(color: Theme.of(context).colorScheme.error),
            ),
            TextButton(onPressed: _load, child: const Text('重试')),
          ],
          for (final member in _members)
            Card(
              child: ListTile(
                title: Text(member.displayName),
                subtitle: Text(
                  '${memberRoleLabel(member.role)} · ${member.status == 'invited' ? '待接受' : '已加入'}\n${member.userAccountID}',
                ),
                isThreeLine: true,
                trailing: PopupMenuButton<String>(
                  enabled: !_busy,
                  onSelected: (value) {
                    if (value == 'revoke') {
                      _revoke(member);
                    } else {
                      _changeRole(member, value);
                    }
                  },
                  itemBuilder: (context) => [
                    if (member.status == 'active')
                      for (final role in [
                        'member',
                        'moderator',
                        if (widget.currentRole == 'owner') 'admin',
                        if (widget.currentRole == 'owner') 'owner',
                      ])
                        if (role != member.role &&
                            (widget.currentRole == 'owner' ||
                                (member.role != 'owner' &&
                                    member.role != 'admin')))
                          PopupMenuItem(
                            value: role,
                            child: Text('设为${memberRoleLabel(role)}'),
                          ),
                    if (widget.currentRole == 'owner' ||
                        (member.role != 'owner' && member.role != 'admin'))
                      const PopupMenuItem(value: 'revoke', child: Text('移除成员')),
                  ],
                ),
              ),
            ),
        ],
      ),
    ),
  );
}

class OrganizationInvitationsPage extends StatefulWidget {
  const OrganizationInvitationsPage({
    super.key,
    required this.auth,
    required this.onAccepted,
    this.api,
  });
  final BirdtieAuthController auth;
  final Future<void> Function() onAccepted;
  final OrganizationMembershipApi? api;
  @override
  State<OrganizationInvitationsPage> createState() =>
      _OrganizationInvitationsPageState();
}

class _OrganizationInvitationsPageState
    extends State<OrganizationInvitationsPage> {
  late final OrganizationMembershipApi _api;
  List<OrganizationMember> _invitations = [];
  bool _loading = true;
  String? _error;
  @override
  void initState() {
    super.initState();
    _api =
        widget.api ??
        OrganizationMembershipApi(
          authorizationHeader: () => widget.auth.authorizationHeader,
        );
    _load();
  }

  @override
  void dispose() {
    if (widget.api == null) _api.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final items = await _api.invitations();
      if (mounted) setState(() => _invitations = items);
    } catch (error) {
      if (mounted) {
        setState(
          () => _error = error is OrganizationMembershipException
              ? error.message
              : '网络暂不可用，请稍后重试。',
        );
      }
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _accept(OrganizationMember invite) async {
    try {
      await _api.accept(invite.id);
      await widget.onAccepted();
      await _load();
    } catch (error) {
      if (mounted) {
        setState(
          () => _error = error is OrganizationMembershipException
              ? error.message
              : '接受邀请失败，请稍后重试。',
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('我的组织邀请')),
    body: RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          const Text('向组织管理员提供你的账号 ID，管理员可向此账号发送站内邀请。'),
          const SizedBox(height: 8),
          if (widget.auth.accountID case final accountID?)
            SelectableText(accountID, key: const Key('my_account_id')),
          if (widget.auth.accountID != null)
            TextButton.icon(
              onPressed: () async {
                await Clipboard.setData(
                  ClipboardData(text: widget.auth.accountID!),
                );
                if (context.mounted) {
                  ScaffoldMessenger.of(
                    context,
                  ).showSnackBar(const SnackBar(content: Text('账号 ID 已复制')));
                }
              },
              icon: const Icon(Icons.copy),
              label: const Text('复制我的账号 ID'),
            ),
          const SizedBox(height: 16),
          if (_loading) const Center(child: CircularProgressIndicator()),
          if (_error != null)
            Text(
              _error!,
              style: TextStyle(color: Theme.of(context).colorScheme.error),
            ),
          if (!_loading && _error == null && _invitations.isEmpty)
            const Text('目前没有待处理的组织邀请。'),
          for (final invite in _invitations)
            Card(
              child: ListTile(
                title: Text(invite.organizationName),
                subtitle: Text('邀请成为${memberRoleLabel(invite.role)}'),
                trailing: FilledButton(
                  onPressed: () => _accept(invite),
                  child: const Text('接受邀请'),
                ),
              ),
            ),
        ],
      ),
    ),
  );
}
