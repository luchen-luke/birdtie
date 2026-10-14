import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:http/http.dart' as http;
import 'package:uuid/uuid.dart';
import '../app/birdtie_app_bar.dart';
import '../auth/birdtie_auth_controller.dart';
import 'organization_human_control.dart';
import 'organization_invite_page.dart';
import 'organization_map_location_page.dart';
import 'organization_membership_api.dart';
import 'organization_faq_page.dart';
import 'human_causal_control.dart';
import 'human_causal_pending_store.dart';
import 'human_causal_operation_controller.dart';

String memberRoleLabel(String role) => organizationMemberRoleLabel(role);
String memberStatusLabel(String status) => switch (status) {
  'active' => '已加入',
  'invited' => '待接受',
  'removed' => '已移除',
  _ => '资料不可用',
};

class OrganizationMembersPage extends StatefulWidget {
  const OrganizationMembersPage({
    super.key,
    required this.organizationID,
    required this.organizationName,
    required this.currentRole,
    required this.authorizationHeader,
    this.api,
    this.ownerID,
    this.identityChanges,
    this.sourceCurrent,
    this.client,
    this.apiBaseUrl,
    this.pendingStore,
  });
  final String organizationID, organizationName, currentRole;
  final String? Function() authorizationHeader;
  final String? Function()? ownerID;
  final Listenable? identityChanges;
  final bool Function()? sourceCurrent;
  final OrganizationMembershipApi? api;
  final http.Client? client;
  final String? apiBaseUrl;
  final OrganizationControlPendingStore? pendingStore;
  @override
  State<OrganizationMembersPage> createState() =>
      _OrganizationMembersPageState();
}

class _OrganizationMembersPageState extends State<OrganizationMembersPage> {
  late OrganizationMembershipApi _api;
  late OrganizationHumanPageBinding _binding;
  OrganizationMembersControl? _snapshot;
  bool _loading = true;
  int _readEpoch = 0;
  String? _error;
  Timer? _lease;
  OrganizationControlOperation get _op =>
      _binding.operation(widget.organizationID);
  bool get _source => _binding.current && _snapshot?.current == true;
  @override
  void initState() {
    super.initState();
    _configure();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) unawaited(_load());
    });
  }

  void _configure() {
    _api =
        widget.api ??
        OrganizationMembershipApi(
          authorizationHeader: () => widget.authorizationHeader(),
          client: widget.client,
          apiBaseUrl: widget.apiBaseUrl,
        );
    _binding = OrganizationHumanPageBinding(
      api: _api.controlApi,
      target: () =>
          (widget.organizationID, widget.api, widget.client, widget.apiBaseUrl),
      header: () => widget.authorizationHeader(),
      ownerID: widget.ownerID == null ? null : () => widget.ownerID!(),
      identityChanges: widget.identityChanges,
      sourceCurrent: () => mounted && widget.sourceCurrent?.call() != false,
      store: widget.pendingStore,
      onRetired: (_) {
        _lease?.cancel();
        _snapshot = null;
        ++_readEpoch;
        organizationPageUpdate(this, () {
          if (mounted)
            setState(() {
              _loading = false;
              _error = '当前身份或来源已变化，请重新读取。';
            });
        });
      },
    );
    _op.addListener(_changed);
  }

  void _changed() => organizationPageUpdate(this, () {
    if (mounted) setState(() {});
  });
  @override
  void didUpdateWidget(OrganizationMembersPage old) {
    super.didUpdateWidget(old);
    if (old.organizationID != widget.organizationID ||
        old.api != widget.api ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.identityChanges != widget.identityChanges ||
        (old.ownerID == null) != (widget.ownerID == null)) {
      _op.removeListener(_changed);
      _binding.dispose();
      if (old.api == null) _api.dispose();
      _snapshot = null;
      _configure();
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) unawaited(_load());
      });
    } else {
      _binding.observe();
    }
  }

  Future<void> _load() async {
    final guard = _binding.capture(), read = ++_readEpoch;
    if (!guard()) return;
    _lease?.cancel();
    _op.invalidatePreview();
    setState(() {
      _loading = true;
      _error = null;
      _snapshot = null;
    });
    try {
      await _binding.resolveOwner();
      if (!guard() || read != _readEpoch) return;
      await _binding.restore(widget.organizationID);
      if (!guard() || read != _readEpoch) return;
      final snapshot = await _api.membersControl(
        widget.organizationID,
        guard: guard,
      );
      if (!guard() || read != _readEpoch) return;
      setState(() => _snapshot = snapshot);
      _lease = Timer(snapshot.expiresAt.difference(DateTime.now().toUtc()), () {
        if (guard() && identical(_snapshot, snapshot)) {
          _op.invalidatePreview();
          setState(() {
            _snapshot = null;
            _error = '成员资料已到期，请重新读取。';
          });
        }
      });
    } catch (e) {
      if (guard() && read == _readEpoch)
        setState(() {
          _snapshot = null;
          _error = organizationControlMessage(e);
        });
    } finally {
      if (guard() && read == _readEpoch) setState(() => _loading = false);
    }
  }

  Future<void> _invite() async {
    final s = _snapshot, guard = _binding.capture();
    if (s == null || !_source || !s.canManage || !_op.canWrite) return;
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => OrganizationInvitePage(
          organizationID: widget.organizationID,
          organizationName: widget.organizationName,
          authorizationHeader: () => widget.authorizationHeader(),
          ownerID: () => _binding.owner,
          identityChanges: widget.identityChanges,
          sourceCurrent: () => guard(),
          api: _api,
          pendingStore: _binding.store,
        ),
      ),
    );
    if (guard() && mounted) await _load();
  }

  Future<void> _act(
    OrganizationMember m,
    String action,
    String role,
    OrganizationMembersControl source,
  ) async {
    final guard = _binding.capture();
    bool current() =>
        guard() &&
        identical(_snapshot, source) &&
        _source &&
        source.canChange(m) &&
        (action != 'ROLE' || m.status == 'active') &&
        (source.role == 'owner' || !const {'owner', 'admin'}.contains(role));
    if (!current() || !_op.canWrite) return;
    final input = OrganizationControlInput(
      action: action,
      operationID: const Uuid().v4(),
      expectedAuthorityVersion: source.authorityVersion,
      membershipID: m.id,
      expectedMemberVersion: m.version,
      role: action == 'ROLE' ? role : '',
    );
    final p = await _op.preview(input);
    if (p == null || !current() || !mounted) return;
    final accepted = await confirmOrganizationControl(
      context,
      _op,
      p,
      title: action == 'REVOKE' ? '确认移除成员' : '确认成员角色',
      target:
          '${widget.organizationName}\n${m.displayName.isEmpty ? '账号资料暂不可用' : m.displayName}\n个人账号：${m.userAccountID}\n当前${memberRoleLabel(m.role)} · ${memberStatusLabel(m.status)} · 成员版本 ${m.version}',
      consequence: action == 'REVOKE'
          ? '该账号将失去此组织工作区权限；待接受邀请也会失效。不能移除最后一位有效所有者。'
          : '将改为${memberRoleLabel(role)}，相应管理权限立即变化。设为所有者会授予最高管理权限；不能移除最后一位有效所有者。',
      sourceCurrent: current,
    );
    if (!accepted || !current()) return;
    final r = await _op.submit(
      p,
      input.selector(widget.organizationID),
      sourceCurrent: current,
    );
    if (r != null && guard() && mounted) await _load();
  }

  Future<void> _recover({required bool stop}) async {
    final guard = _binding.capture(), r = await _op.recover(stop: stop);
    if (r != null && guard() && mounted) await _load();
  }

  @override
  void dispose() {
    _lease?.cancel();
    _op.removeListener(_changed);
    _binding.dispose();
    if (widget.api == null) _api.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final s = _source ? _snapshot : null;
    return Scaffold(
      appBar: birdtieAppBar(
        context,
        title: Text(
          _binding.current ? '${widget.organizationName} · 成员' : '组织成员',
        ),
      ),
      body: RefreshIndicator(
        onRefresh: _load,
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            if (_loading) const LinearProgressIndicator(),
            if (_error != null)
              Text(_error!, key: const Key('organization_members_error')),
            organizationOperationRecovery(_op, _recover),
            TextButton(
              onPressed: _op.busy ? null : _load,
              child: const Text('重新读取成员'),
            ),
            if (s?.canManage == true)
              FilledButton.icon(
                key: const Key('organization_invite_member'),
                onPressed: _op.canWrite ? _invite : null,
                icon: const Icon(Icons.person_add_alt_1),
                label: const Text('邀请成员'),
              ),
            if (s != null)
              for (var i = 0; i < s.members.length; i++)
                ListTile(
                  key: Key('organization_member_${s.members[i].id}'),
                  title: Text(
                    s.members[i].displayName.isEmpty
                        ? '账号资料暂不可用 · 成员 ${i + 1}'
                        : s.members[i].displayName,
                  ),
                  subtitle: Text(
                    '${memberRoleLabel(s.members[i].role)} · ${memberStatusLabel(s.members[i].status)}',
                  ),
                  trailing: s.canChange(s.members[i])
                      ? PopupMenuButton<String>(
                          key: Key(
                            'organization_member_menu_${s.members[i].id}',
                          ),
                          enabled: _op.canWrite,
                          onSelected: (v) => _act(
                            s.members[i],
                            v == 'revoke' ? 'REVOKE' : 'ROLE',
                            v == 'revoke' ? '' : v,
                            s,
                          ),
                          itemBuilder: (_) => [
                            if (s.members[i].status == 'active')
                              for (final role in [
                                'member',
                                'moderator',
                                if (s.role == 'owner') 'admin',
                                if (s.role == 'owner') 'owner',
                              ])
                                if (role != s.members[i].role)
                                  PopupMenuItem(
                                    value: role,
                                    child: Text('设为${memberRoleLabel(role)}'),
                                  ),
                            const PopupMenuItem(
                              value: 'revoke',
                              child: Text('移除成员'),
                            ),
                          ],
                        )
                      : null,
                ),
            if (s != null && s.members.isEmpty) const Text('当前名单没有成员。'),
          ],
        ),
      ),
    );
  }
}

class OrganizationInvitationsPage extends StatefulWidget {
  const OrganizationInvitationsPage({
    super.key,
    required this.auth,
    required this.onAccepted,
    this.api,
    this.client,
    this.apiBaseUrl,
    this.pendingStore,
    this.sourceCurrent,
    this.humanPendingStore,
  });
  final BirdtieAuthController auth;
  final Future<void> Function() onAccepted;
  final OrganizationMembershipApi? api;
  final http.Client? client;
  final String? apiBaseUrl;
  final OrganizationControlPendingStore? pendingStore;
  final bool Function()? sourceCurrent;
  final HumanCausalPendingStore? humanPendingStore;
  @override
  State<OrganizationInvitationsPage> createState() =>
      _OrganizationInvitationsPageState();
}

class _OrganizationInvitationsPageState
    extends State<OrganizationInvitationsPage> {
  late OrganizationMembershipApi _api;
  late OrganizationHumanPageBinding _binding;
  List<OrganizationMember> _items = [];
  List<String> _pendingOrgs = [];
  List<HumanCausalPending> _faqPending = [];
  String? _faqRecoveryError;
  bool _loading = true;
  int _readEpoch = 0;
  String? _error;
  DateTime? _readUntil;
  Timer? _lease;
  @override
  void initState() {
    super.initState();
    _configure();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) unawaited(_load());
    });
  }

  void _configure() {
    _api =
        widget.api ??
        OrganizationMembershipApi(
          authorizationHeader: () => widget.auth.authorizationHeader,
          client: widget.client,
          apiBaseUrl: widget.apiBaseUrl,
        );
    _binding = OrganizationHumanPageBinding(
      api: _api.controlApi,
      target: () => (widget.auth, widget.api, widget.client, widget.apiBaseUrl),
      header: () => widget.auth.authorizationHeader,
      ownerID: () => widget.auth.accountID,
      identityChanges: widget.auth,
      sourceCurrent: () =>
          mounted &&
          widget.auth.signedIn &&
          widget.sourceCurrent?.call() != false,
      store: widget.pendingStore,
      onRetired: (_) {
        _lease?.cancel();
        ++_readEpoch;
        _items = [];
        _pendingOrgs = [];
        _faqPending = [];
        _readUntil = null;
        organizationPageUpdate(this, () {
          if (mounted)
            setState(() {
              _loading = false;
              _error = '当前个人身份已变化，请重新读取。';
            });
        });
      },
    );
  }

  void _changed() => organizationPageUpdate(this, () {
    if (mounted) setState(() {});
  });
  @override
  void didUpdateWidget(OrganizationInvitationsPage old) {
    super.didUpdateWidget(old);
    if (old.auth != widget.auth ||
        old.api != widget.api ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl) {
      _binding.dispose();
      if (old.api == null) _api.dispose();
      _items = [];
      _pendingOrgs = [];
      _faqPending = [];
      _configure();
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) unawaited(_load());
      });
    } else {
      _binding.observe();
    }
  }

  Future<void> _load() async {
    final guard = _binding.capture(), read = ++_readEpoch;
    if (!guard()) return;
    _lease?.cancel();
    _readUntil = null;
    _items = [];
    for (final op in _binding.operations.values) {
      op.invalidatePreview();
    }
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      await _binding.resolveOwner();
      if (!guard() || read != _readEpoch) return;
      try {
        final pending =
            await (widget.humanPendingStore ?? SecureHumanCausalPendingStore())
                .read(
                  _api.baseUrl.replaceFirst(RegExp(r'/$'), ''),
                  _binding.owner!,
                );
        if (!guard() || read != _readEpoch) return;
        _faqPending = pending
            .where((p) => p.selector.family == HumanCausalFamily.faq)
            .toList();
        _faqRecoveryError = null;
      } catch (_) {
        if (!guard() || read != _readEpoch) return;
        _faqPending = [];
        _faqRecoveryError = '本机原问答操作列表暂不可读，请重新读取。';
      }
      // Recovery is independent of the current invitation list and its authority.
      final indexed = await _binding.store.organizations(
        _api.baseUrl.replaceFirst(RegExp(r'/$'), ''),
        _binding.owner!,
      );
      if (!guard() || read != _readEpoch) return;
      for (final org in indexed) {
        await _binding.restore(org);
        if (!guard() || read != _readEpoch) return;
        final op = _binding.operation(org);
        op.removeListener(_changed);
        op.addListener(_changed);
      }
      setState(
        () => _pendingOrgs = indexed
            .where((org) => _binding.operation(org).pending != null)
            .toList(),
      );
      final items = await _api.invitations(guard: guard);
      if (!guard() || read != _readEpoch) return;
      if (items.any((m) => m.userAccountID != _binding.owner))
        throw const FormatException('邀请不是发给当前个人账号');
      for (final org in items.map((m) => m.organizationID).toSet()) {
        await _binding.restore(org);
        if (!guard() || read != _readEpoch) return;
        final op = _binding.operation(org);
        op.removeListener(_changed);
        op.addListener(_changed);
      }
      _readUntil = DateTime.now().toUtc().add(const Duration(seconds: 30));
      setState(() => _items = items);
      _lease = Timer(const Duration(seconds: 30), () {
        if (guard() && read == _readEpoch) {
          _items = [];
          _readUntil = null;
          for (final op in _binding.operations.values) {
            op.invalidatePreview();
          }
          setState(() => _error = '邀请资料已到期，请重新读取。');
        }
      });
    } catch (e) {
      if (guard() && read == _readEpoch)
        setState(() {
          _items = [];
          _readUntil = null;
          _error = organizationControlMessage(e);
        });
    } finally {
      if (guard() && read == _readEpoch) setState(() => _loading = false);
    }
  }

  bool get _source =>
      _binding.current && _readUntil?.isAfter(DateTime.now().toUtc()) == true;
  Future<void> _accept(
    OrganizationMember invite,
    List<OrganizationMember> source,
  ) async {
    final guard = _binding.capture(),
        op = _binding.operation(invite.organizationID);
    bool current() =>
        guard() &&
        _source &&
        identical(_items, source) &&
        invite.userAccountID == _binding.owner;
    if (!current() || !op.canWrite) return;
    final input = OrganizationControlInput(
      action: 'ACCEPT',
      operationID: const Uuid().v4(),
      expectedAuthorityVersion: invite.authorityVersion,
      membershipID: invite.id,
      expectedMemberVersion: invite.version,
    );
    final p = await op.preview(input);
    if (p == null || !current() || !mounted) return;
    final yes = await confirmOrganizationControl(
      context,
      op,
      p,
      title: '确认接受组织邀请',
      target:
          '${invite.organizationName.isEmpty ? '组织名称暂不可用' : invite.organizationName}\n组织：${invite.organizationID}\n本人的邀请版本 ${invite.version}',
      consequence: '接受后以${memberRoleLabel(invite.role)}身份加入该组织，并获得相应工作区权限。',
      sourceCurrent: current,
    );
    if (!yes || !current()) return;
    final r = await op.submit(
      p,
      input.selector(invite.organizationID),
      sourceCurrent: current,
    );
    if (r != null && guard() && mounted) {
      if (r.status == 'SUCCEEDED') await widget.onAccepted();
      if (guard() && mounted) await _load();
    }
  }

  Future<void> _recover(
    OrganizationControlOperation op, {
    required bool stop,
  }) async {
    final guard = _binding.capture(), r = await op.recover(stop: stop);
    if (r != null && guard() && mounted) {
      if (r.status == 'SUCCEEDED' && r.selector.action == 'ACCEPT')
        await widget.onAccepted();
      if (guard() && mounted) await _load();
    }
  }

  Future<void> _openMapRecovery(String org) async {
    final guard = _binding.capture(), op = _binding.operation(org);
    if (!guard() || op.pending == null || op.pending!.selector.memberAction)
      return;
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => OrganizationMapLocationPage(
          organizationID: org,
          cityID: '',
          authorizationHeader: () => widget.auth.authorizationHeader,
          ownerID: () => guard() ? widget.auth.accountID : null,
          identityChanges: widget.auth,
          sourceCurrent: guard,
          client: _api.client,
          apiBaseUrl: _api.baseUrl,
          pendingStore: _binding.store,
        ),
      ),
    );
    if (guard() && mounted) await _load();
  }

  Future<void> _openFAQRecovery(HumanCausalPending pending) async {
    final auth = widget.auth, api = _api, owner = auth.accountID;
    bool current() =>
        mounted &&
        auth == widget.auth &&
        identical(api, _api) &&
        owner == auth.accountID &&
        auth.signedIn &&
        widget.sourceCurrent?.call() != false;
    if (!current() ||
        !_faqPending.any((p) => p.selector.same(pending.selector)))
      return;
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => HumanCausalEntryBoundary(
          ownerID: () => auth.accountID,
          authorizationHeader: () => auth.authorizationHeader,
          sourceKey: () =>
              (widget.auth, widget.api, widget.client, widget.apiBaseUrl),
          identityChanges: auth,
          sourceCurrent: current,
          builder: (_, live) => OrganizationFAQPage(
            organizationID: pending.selector.organizationID,
            authorizationHeader: () => auth.authorizationHeader,
            ownerID: () => auth.accountID,
            sourceCurrent: live,
            identityChanges: auth,
            client: api.client,
            apiBaseUrl: api.baseUrl,
            pendingStore: widget.humanPendingStore,
          ),
        ),
      ),
    );
    if (current()) await _load();
  }

  @override
  void dispose() {
    _lease?.cancel();
    _binding.dispose();
    if (widget.api == null) _api.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final items = _source ? _items : const <OrganizationMember>[];
    return Scaffold(
      appBar: birdtieAppBar(context, title: const Text('我的组织邀请')),
      body: RefreshIndicator(
        onRefresh: _load,
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            const Text('管理员可使用获准的个人公开搜索选择你；找不到时，可核对你提供的账号 ID。邀请只出现在本账号内。'),
            if (_binding.current && widget.auth.accountID != null) ...[
              SelectableText(
                widget.auth.accountID!,
                key: const Key('my_account_id'),
              ),
              TextButton.icon(
                onPressed: () async {
                  final guard = _binding.capture(), id = widget.auth.accountID;
                  if (!guard() || id == null) return;
                  await Clipboard.setData(ClipboardData(text: id));
                  if (guard() && mounted)
                    ScaffoldMessenger.of(
                      context,
                    ).showSnackBar(const SnackBar(content: Text('账号 ID 已复制')));
                },
                icon: const Icon(Icons.copy),
                label: const Text('复制我的账号 ID'),
              ),
            ],
            if (_loading) const LinearProgressIndicator(),
            if (_error != null)
              Text(_error!, key: const Key('organization_invitations_error')),
            if (_faqRecoveryError != null) Text(_faqRecoveryError!),
            TextButton(
              onPressed: _binding.operations.values.any((o) => o.busy)
                  ? null
                  : _load,
              child: const Text('重新读取邀请'),
            ),
            if (!_loading && _error == null && items.isEmpty)
              const Text('目前没有待处理的组织邀请。'),
            for (final org in _pendingOrgs)
              if (_binding.operation(org).pending != null) ...[
                Text(
                  '本人待核实的组织操作',
                  style: Theme.of(context).textTheme.titleSmall,
                ),
                if (!_binding.operation(org).pending!.selector.memberAction)
                  ListTile(
                    key: Key('organization_map_recovery_$org'),
                    title: const Text('核实原位置操作'),
                    subtitle: Text('组织：$org\n只核实原操作，不授予新的位置权限。'),
                    onTap: () => _openMapRecovery(org),
                  ),
                if (_binding.operation(org).pending!.selector.memberAction)
                  organizationOperationRecovery(
                    _binding.operation(org),
                    ({required bool stop}) =>
                        _recover(_binding.operation(org), stop: stop),
                  ),
              ],
            if (_binding.current)
              for (final p in _faqPending)
                ListTile(
                  key: Key(
                    'organization_faq_recovery_${p.selector.organizationID}',
                  ),
                  title: const Text('核实本人原问答操作'),
                  subtitle: Text(
                    '组织：${p.selector.organizationID}\n只核实原回执，不授予新的管理权限。',
                  ),
                  onTap: () => _openFAQRecovery(p),
                ),
            for (final invite in items) ...[
              ListTile(
                key: Key('organization_invitation_${invite.id}'),
                title: Text(
                  invite.organizationName.isEmpty
                      ? '组织名称暂不可用'
                      : invite.organizationName,
                ),
                subtitle: Text('邀请成为${memberRoleLabel(invite.role)}'),
                trailing: FilledButton(
                  onPressed: _binding.operation(invite.organizationID).canWrite
                      ? () => _accept(invite, items)
                      : null,
                  child: const Text('接受邀请'),
                ),
              ),
              if (!_pendingOrgs.contains(invite.organizationID))
                organizationOperationRecovery(
                  _binding.operation(invite.organizationID),
                  ({required bool stop}) => _recover(
                    _binding.operation(invite.organizationID),
                    stop: stop,
                  ),
                ),
            ],
          ],
        ),
      ),
    );
  }
}
