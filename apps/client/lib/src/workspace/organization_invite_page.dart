import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:uuid/uuid.dart';
import '../app/birdtie_app_bar.dart';
import 'organization_human_control.dart';
import 'organization_membership_api.dart';

String organizationMemberRoleLabel(String role) => switch (role) {
  'owner' => '所有者',
  'admin' => '管理员',
  'moderator' => '协管员',
  'member' => '成员',
  _ => '未知角色',
};

class OrganizationInvitePage extends StatefulWidget {
  const OrganizationInvitePage({
    super.key,
    required this.organizationID,
    required this.organizationName,
    required this.authorizationHeader,
    this.ownerID,
    this.identityChanges,
    this.sourceCurrent,
    this.api,
    this.client,
    this.apiBaseUrl,
    this.pendingStore,
  });
  final String organizationID, organizationName;
  final String? Function() authorizationHeader;
  final String? Function()? ownerID;
  final Listenable? identityChanges;
  final bool Function()? sourceCurrent;
  final OrganizationMembershipApi? api;
  final http.Client? client;
  final String? apiBaseUrl;
  final OrganizationControlPendingStore? pendingStore;
  @override
  State<OrganizationInvitePage> createState() => _OrganizationInvitePageState();
}

class _OrganizationInvitePageState extends State<OrganizationInvitePage> {
  final _query = TextEditingController(), _account = TextEditingController();
  late OrganizationMembershipApi _api;
  late OrganizationHumanPageBinding _binding;
  OrganizationMembersControl? _snapshot;
  List<OrganizationInvitePerson> _people = [];
  String _role = 'member', _selectedName = '', _selectedQuery = '';
  int? _selectedVersion, _selectedAuthority;
  String _selectedMembership = '';
  int _inputEpoch = 0, _searchEpoch = 0, _readEpoch = 0;
  bool _loading = true, _searching = false;
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
      onRetired: (scopeChanged) {
        _lease?.cancel();
        _snapshot = null;
        _people = [];
        _selectedName = '';
        _selectedQuery = '';
        ++_readEpoch;
        ++_searchEpoch;
        ++_inputEpoch;
        if (scopeChanged) {
          _query.clear();
          _account.clear();
          _role = 'member';
          _selectedVersion = null;
          _selectedAuthority = null;
          _selectedMembership = '';
        }
        organizationPageUpdate(this, () {
          if (mounted) {
            setState(() {
              _error = '当前身份或来源已变化，请重新读取。';
              _loading = false;
              _searching = false;
            });
          }
        });
      },
    );
    _op.addListener(_changed);
  }

  void _changed() => organizationPageUpdate(this, () {
    if (mounted) setState(() {});
  });
  @override
  void didUpdateWidget(OrganizationInvitePage old) {
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
      _people = [];
      ++_searchEpoch;
      _selectedName = '';
      _selectedQuery = '';
      _account.clear();
      _query.clear();
      _selectedVersion = null;
      _selectedAuthority = null;
      _selectedMembership = '';
      ++_inputEpoch;
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
    setState(() {
      _loading = true;
      _error = null;
      _snapshot = null;
      _people = [];
      _selectedName = '';
      ++_searchEpoch;
    });
    _op.invalidatePreview();
    _lease?.cancel();
    try {
      await _binding.resolveOwner();
      if (!guard() || read != _readEpoch) return;
      await _binding.restore(widget.organizationID);
      if (!guard() || read != _readEpoch) return;
      final s = await _api.membersControl(widget.organizationID, guard: guard);
      if (!guard() || read != _readEpoch) return;
      setState(() => _snapshot = s);
      _lease = Timer(s.expiresAt.difference(DateTime.now().toUtc()), () {
        if (guard() && identical(_snapshot, s)) {
          _op.invalidatePreview();
          setState(() {
            _snapshot = null;
            _people = [];
            _selectedName = '';
            _error = '成员资料已到期，请重新读取后检查。';
          });
        }
      });
    } catch (e) {
      if (guard() && read == _readEpoch)
        setState(() {
          _snapshot = null;
          _people = [];
          _selectedName = '';
          _error = organizationControlMessage(e);
        });
    } finally {
      if (guard() && read == _readEpoch) setState(() => _loading = false);
    }
  }

  void _editAccount(String text) {
    ++_inputEpoch;
    _selectedName = '';
    _selectedQuery = '';
    _selectedVersion = null;
    _selectedAuthority = null;
    _selectedMembership = '';
    _op.invalidatePreview();
    setState(() {});
  }

  void _choose(
    OrganizationInvitePerson p,
    String query,
    OrganizationMembersControl? source,
    int searchGeneration,
  ) {
    if (!_source ||
        !identical(_snapshot, source) ||
        searchGeneration != _searchEpoch ||
        !_snapshot!.canManage)
      return;
    final matches = _snapshot!.members
        .where((m) => m.userAccountID == p.accountID)
        .toList();
    if (matches.isNotEmpty && matches.single.status != 'removed') {
      setState(() => _error = '该账号已是成员或已有待接受邀请。');
      return;
    }
    final old = matches.isEmpty ? null : matches.single;
    ++_inputEpoch;
    _account.text = p.accountID;
    _selectedName = p.displayName;
    _selectedQuery = query;
    _selectedVersion = old?.version ?? 0;
    _selectedMembership = old?.id ?? '';
    _selectedAuthority = _snapshot!.authorityVersion;
    _op.invalidatePreview();
    setState(() => _error = null);
  }

  Future<void> _search() async {
    if (!_source || !_snapshot!.canManage || _op.busy) return;
    final guard = _binding.capture(),
        search = ++_searchEpoch,
        q = _query.text.trim();
    setState(() {
      _searching = true;
      _people = [];
      _error = null;
    });
    try {
      final people = await _api.searchPeople(
        q,
        guard: () =>
            guard() &&
            search == _searchEpoch &&
            _query.text.trim() == q &&
            _source,
      );
      if (guard() &&
          search == _searchEpoch &&
          _query.text.trim() == q &&
          _source)
        setState(() => _people = people);
    } catch (e) {
      if (guard() && search == _searchEpoch)
        setState(() => _error = organizationControlMessage(e));
    } finally {
      if (guard() && search == _searchEpoch) setState(() => _searching = false);
    }
  }

  Future<void> _prepare() async {
    final s = _snapshot, id = _account.text.trim(), generation = _inputEpoch;
    final guard = _binding.capture();
    if (s == null || !_source || !s.canManage || !_op.canWrite) return;
    if (_role == 'admin' && s.role != 'owner') {
      setState(() => _error = '当前不能授予管理员角色，请重新选择角色。');
      return;
    }
    if (!organizationControlID(id)) {
      setState(() => _error = '请输入已核对的个人账号 ID，或选择获准的搜索结果。');
      return;
    }
    final matches = s.members.where((m) => m.userAccountID == id).toList();
    final old = matches.isEmpty ? null : matches.single;
    if (old != null && old.status != 'removed') {
      setState(() => _error = '该账号已是成员或已有待接受邀请。');
      return;
    }
    if (_selectedVersion != null &&
        (_selectedVersion != (old?.version ?? 0) ||
            _selectedAuthority != s.authorityVersion ||
            _selectedMembership != (old?.id ?? ''))) {
      setState(() => _error = '选择时的成员版本已变化，请重新选择或核对账号。');
      return;
    }
    if (_selectedQuery.isNotEmpty) {
      final q = _selectedQuery;
      try {
        final currentPeople = await _api.searchPeople(
          q,
          guard: () => guard() && generation == _inputEpoch && _source,
        );
        if (!guard() || generation != _inputEpoch || !_source) return;
        final found = currentPeople.where((p) => p.accountID == id).toList();
        setState(
          () => _selectedName = found.isEmpty ? '' : found.single.displayName,
        );
      } catch (e) {
        if (guard() && generation == _inputEpoch)
          setState(() {
            _selectedName = '';
            _error = '当前人员资料不可读。可核对已知账号 ID 后继续。';
          });
        return;
      }
    }
    bool current() =>
        guard() &&
        generation == _inputEpoch &&
        identical(_snapshot, s) &&
        s.canManage &&
        _account.text.trim() == id;
    final input = OrganizationControlInput(
      action: 'INVITE',
      operationID: const Uuid().v4(),
      expectedAuthorityVersion: s.authorityVersion,
      userAccountID: id,
      role: _role,
      expectedMemberVersion: old?.version ?? 0,
    );
    final p = await _op.preview(input);
    if (p == null || !current() || !mounted) return;
    final accepted = await confirmOrganizationControl(
      context,
      _op,
      p,
      title: '确认组织邀请',
      target:
          '${widget.organizationName}\n${_selectedName.isEmpty ? '账号资料暂不可用' : _selectedName}\n个人账号：$id\n邀请为${organizationMemberRoleLabel(_role)}',
      consequence: '邀请会出现在该个人账号内。对方接受后获得此组织相应工作区权限；不会发送邮件或自动加入。',
      sourceCurrent: current,
    );
    if (!accepted || !current()) return;
    final r = await _op.submit(
      p,
      input.selector(widget.organizationID, invitedMembershipID: old?.id ?? ''),
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
    _query.dispose();
    _account.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final people = _people,
        query = _query.text.trim(),
        source = _snapshot,
        searchGeneration = _searchEpoch;
    return Scaffold(
      appBar: birdtieAppBar(context, title: const Text('邀请组织成员')),
      body: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          if (_loading) const LinearProgressIndicator(),
          if (_error != null)
            Text(_error!, key: const Key('organization_invite_error')),
          organizationOperationRecovery(_op, _recover),
          TextButton(
            onPressed: _op.busy ? null : _load,
            child: const Text('重新读取成员'),
          ),
          if (_source && _snapshot!.canManage) ...[
            const Text('搜索只显示当前获准的个人公开资料，最多20人。找不到时，可核对对方提供的账号 ID。'),
            TextField(
              key: const Key('organization_people_query'),
              controller: _query,
              decoration: const InputDecoration(labelText: '搜索个人姓名'),
              onChanged: (_) {
                ++_searchEpoch;
                setState(() => _people = []);
              },
            ),
            TextButton(
              key: const Key('organization_people_search'),
              onPressed: _searching || _op.busy ? null : _search,
              child: const Text('搜索'),
            ),
            if (_searching) const LinearProgressIndicator(),
            for (var i = 0; i < people.length; i++)
              ListTile(
                key: Key('organization_person_${people[i].accountID}'),
                title: Text(people[i].displayName),
                subtitle: Text('搜索结果 ${i + 1} · 公开个人账号'),
                onTap: () =>
                    _choose(people[i], query, source, searchGeneration),
              ),
            TextField(
              key: const Key('member_account_id'),
              controller: _account,
              decoration: const InputDecoration(labelText: '已知个人账号 ID'),
              onChanged: _editAccount,
            ),
            if (_selectedName.isNotEmpty) Text('已选择：$_selectedName'),
            DropdownButton<String>(
              key: const Key('organization_invite_role'),
              value: _role,
              items: [
                for (final role in [
                  'member',
                  'moderator',
                  if (_snapshot!.role == 'owner') 'admin',
                ])
                  DropdownMenuItem(
                    value: role,
                    child: Text(organizationMemberRoleLabel(role)),
                  ),
                if (_role == 'admin' && _snapshot!.role != 'owner')
                  const DropdownMenuItem(
                    value: 'admin',
                    enabled: false,
                    child: Text('管理员（当前不能授予）'),
                  ),
              ],
              onChanged: _op.busy
                  ? null
                  : (role) {
                      ++_inputEpoch;
                      _op.invalidatePreview();
                      setState(() => _role = role ?? 'member');
                    },
            ),
            FilledButton(
              key: const Key('organization_preview_invite'),
              onPressed:
                  _op.canWrite &&
                      (_role != 'admin' || _snapshot!.role == 'owner')
                  ? _prepare
                  : null,
              child: const Text('检查站内邀请'),
            ),
          ] else if (!_loading)
            const Text('当前不能邀请成员。原操作核实不授予新的管理权限。'),
        ],
      ),
    );
  }
}
