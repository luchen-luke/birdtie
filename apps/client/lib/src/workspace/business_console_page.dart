import 'dart:async';
import 'package:flutter/material.dart';
import '../auth/birdtie_auth_controller.dart';
import '../city/public_city_controller.dart';
import 'business_api.dart';
import 'business_knowledge_page.dart';
import 'business_agent_identity_page.dart';
import 'business_console_controller.dart';
import 'organization_workspaces.dart';
import 'business_public_permission_page.dart';

String businessStateLabel(dynamic state) => switch (state) {
  'pending' => '待审核',
  'verified' => '已核验',
  'rejected' => '未通过',
  'revoked' => '已撤销',
  'expired' => '资料已过期',
  'active' => '有效',
  'removed' => '已移除',
  _ => '状态待确认',
};
String businessRoleLabel(dynamic role) => switch (role) {
  'owner' => '所有者',
  'admin' => '管理员',
  'member' => '成员',
  'reviewer' => '独立审核员',
  _ => '无管理权限',
};
const _labels = {
  'expectedVersion': '当前资料版本',
  'name': '商家名称',
  'description': '商家介绍',
  'sourceUrl': '证明来源',
  'rightsNote': '资料及经营权声明',
  'validUntil': '资料有效期',
  'facts': '填写资料',
  'timeZone': '营业时间时区',
  'openingHours': '营业时间',
  'officialLinks': '官方链接',
  'suitability': '适合的活动',
  'bookingUrl': '预约入口',
  'note': '说明',
  'targetPersonId': '对方账号标识',
  'action': '操作',
  'role': '权限',
  'decision': '审核决定',
  'day': '星期',
  'closed': '全天休息',
  'opensAt': '开始时间',
  'closesAt': '结束时间',
  'nextDay': '次日结束',
};
String _humanValue(dynamic value) {
  if (value is Map) {
    return value.entries
        .map((e) => '${_labels[e.key] ?? '资料'}：${_humanValue(e.value)}')
        .join('\n');
  }
  if (value is List) {
    return value.isEmpty ? '未填写' : value.map(_humanValue).join('\n');
  }
  if (value is bool) return value ? '是' : '否';
  return switch (value) {
    'grant' => '添加或修改成员权限',
    'remove' => '撤销成员权限',
    'transfer_owner' => '转交所有权',
    'approve' => '审核通过',
    'reject' => '审核不通过',
    'revoke' => '撤销核验',
    'owner' => '所有者',
    'admin' => '管理员',
    'member' => '成员',
    null || '' => '未填写',
    _ => value.toString(),
  };
}

class BusinessConsolePage extends StatefulWidget {
  const BusinessConsolePage({
    super.key,
    required this.auth,
    required this.city,
    required this.organizations,
    this.api,
    this.controller,
  });
  final BirdtieAuthController auth;
  final PublicCityController city;
  final OrganizationWorkspaceController organizations;
  final BusinessApi? api;
  final BusinessConsoleController? controller;
  @override
  State<BusinessConsolePage> createState() => _BusinessConsolePageState();
}

class _BusinessConsolePageState extends State<BusinessConsolePage> {
  late BusinessApi _api;
  late BusinessConsoleController c;
  final ValueNotifier<int> _bindingChanges = ValueNotifier(0);
  bool _ownsApi = false, _ownsController = false;
  bool _disposed = false;
  int _bindingEpoch = 0;
  @override
  void initState() {
    super.initState();
    _bind();
  }

  void _bind() {
    final auth = widget.auth, organizations = widget.organizations;
    _ownsApi = widget.api == null;
    _ownsController = widget.controller == null;
    _api =
        widget.api ??
        BusinessApi(authorizationHeader: () => auth.authorizationHeader);
    c =
        widget.controller ??
        BusinessConsoleController(
          api: _api,
          accountID: () => auth.accountID,
          authorizationHeader: () => auth.authorizationHeader,
          organizationWorkspaceID: () => organizations.active?.id,
        );
    widget.auth.addListener(_identity);
    widget.organizations.addListener(_identity);
    c.synchronizeIdentity();
    unawaited(c.load());
  }

  @override
  void didUpdateWidget(covariant BusinessConsolePage old) {
    super.didUpdateWidget(old);
    if (!identical(old.api, widget.api) ||
        !identical(old.controller, widget.controller) ||
        !identical(old.auth, widget.auth) ||
        !identical(old.organizations, widget.organizations)) {
      old.auth.removeListener(_identity);
      old.organizations.removeListener(_identity);
      final previousApi = _api, previousController = c;
      final disposeApi = _ownsApi, disposeController = _ownsController;
      _bindingEpoch++;
      _bind();
      // The old frame is already retired by the changed object identities.
      // Notify sibling Navigator routes after this build, never setState them
      // while the Console's parent is building.
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && !_disposed) _bindingChanges.value++;
      });
      if (disposeController) previousController.dispose();
      if (disposeApi) previousApi.dispose();
    }
  }

  void _identity() {
    c.synchronizeIdentity();
  }

  @override
  void dispose() {
    _disposed = true;
    widget.auth.removeListener(_identity);
    widget.organizations.removeListener(_identity);
    // Child routes also check mounted/disposed synchronously before actions.
    if (_ownsController) c.dispose();
    if (_ownsApi) _api.dispose();
    _bindingChanges.dispose();
    super.dispose();
  }

  Future<void> _edit(
    String kind, {
    String? resource,
    Map<String, dynamic>? facts,
    BusinessDraft? initialDraft,
    int? draftGeneration,
  }) async {
    final frameController = c, frameApi = _api;
    final bindingEpoch = _bindingEpoch;
    bool frameCurrent() =>
        mounted &&
        !_disposed &&
        bindingEpoch == _bindingEpoch &&
        identical(c, frameController) &&
        identical(_api, frameApi);
    if (initialDraft != null &&
        (draftGeneration != c.generation || !c.personal)) {
      return;
    }
    final editorGeneration = c.generation;
    final draft = await Navigator.of(context).push<BusinessDraft>(
      MaterialPageRoute(
        builder: (_) => _BusinessEditor(
          controller: c,
          kind: kind,
          resource: resource,
          existing: facts,
          initialDraft: initialDraft,
          expectedGeneration: editorGeneration,
          places: widget.city.places,
          frameCurrent: frameCurrent,
          bindingChanges: _bindingChanges,
        ),
      ),
    );
    if (!mounted || !frameCurrent() || draft == null) return;
    c.edit(draft);
    final approval = c.preview();
    if (approval == null) return;
    bool checked = false;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => StatefulBuilder(
        builder: (context, set) => AnimatedBuilder(
          animation: Listenable.merge([frameController, _bindingChanges]),
          builder: (context, _) {
            if (!frameCurrent() || !c.isApprovalCurrent(approval)) {
              return AlertDialog(
                title: Text(
                  approval.generation != c.generation || !c.personal
                      ? '工作身份已变化'
                      : '资料或权限已变化',
                ),
                content: const Text('旧预览已失效，请返回并重新读取资料。'),
                actions: [
                  TextButton(
                    onPressed: () => Navigator.pop(context, false),
                    child: const Text('返回'),
                  ),
                ],
              );
            }
            return AlertDialog(
              title: const Text('核对后再提交'),
              content: SingleChildScrollView(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      '本人账号：${widget.auth.displayName ?? '当前登录账号'}\n商家：${c.snapshot?.business.name ?? draft.body['name'] ?? '新商家'}',
                    ),
                    const SizedBox(height: 16),
                    Text(_humanValue(approval.draft.body)),
                    if (draft.kind == 'review' && facts != null) ...[
                      const SizedBox(height: 12),
                      const Text('本次核验的具体材料'),
                      Text(
                        _humanValue({
                          for (final key in [
                            'name',
                            'description',
                            'facts',
                            'sourceUrl',
                            'rightsNote',
                            'validUntil',
                          ])
                            if (facts.containsKey(key)) key: facts[key],
                        }),
                      ),
                    ],
                    if (draft.placeID != null)
                      Text(
                        '场地：${widget.city.places.where((p) => p.id == draft.placeID).firstOrNull?.name ?? draft.placeID}',
                      ),
                    const SizedBox(height: 12),
                    Text(
                      draft.kind == 'member'
                          ? '提交后直接更改成员权限；转交后你将不再是所有者。'
                          : draft.kind == 'review'
                          ? '仅核验这个具体版本，不会批准后续编辑。'
                          : '提交后进入待审核；填写资料不等于已核验经营权。',
                    ),
                    CheckboxListTile(
                      contentPadding: EdgeInsets.zero,
                      value: checked,
                      onChanged: (v) => set(() => checked = v == true),
                      title: const Text('我已核对当前主体、资料和后果'),
                    ),
                  ],
                ),
              ),
              actions: [
                TextButton(
                  onPressed: () => Navigator.pop(context, false),
                  child: const Text('返回修改'),
                ),
                FilledButton(
                  onPressed: checked
                      ? () => Navigator.pop(context, true)
                      : null,
                  child: const Text('确认提交'),
                ),
              ],
            );
          },
        ),
      ),
    );
    if (!frameCurrent()) return;
    if (confirmed == true) {
      await c.submit(approval);
    } else {
      final returnToEditor =
          confirmed == false && c.isApprovalCurrent(approval);
      c.cancelPreview();
      if (returnToEditor) {
        await _edit(
          kind,
          resource: resource,
          facts: facts,
          initialDraft: draft,
          draftGeneration: approval.generation,
        );
      }
    }
  }

  void _agentIdentity(BusinessConsoleSnapshot snapshot) {
    final api = _api, controller = c, auth = widget.auth, org = widget.organizations;
    final bindingEpoch = _bindingEpoch;
    final identity = (auth.accountID, auth.authorizationHeader, org.active?.id);
    var retired = false;
    bool current() {
      if (retired || !mounted || _disposed || bindingEpoch != _bindingEpoch ||
          !identical(_api, api) || !identical(c, controller) ||
          !identical(widget.auth, auth) || !identical(widget.organizations, org) ||
          identity != (auth.accountID, auth.authorizationHeader, org.active?.id) ||
          controller.selectedID != snapshot.business.id ||
          !identical(controller.snapshot, snapshot) || !snapshot.canManage ||
          !const {'owner', 'admin'}.contains(snapshot.business.role)) {
        retired = true;
        return false;
      }
      return true;
    }
    Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => BusinessAgentIdentityPage(
      api: api, businessID: snapshot.business.id,
      accountID: () => auth.accountID, authorizationHeader: () => auth.authorizationHeader,
      workspaceID: () => org.active?.id, currentBusinessID: () => controller.selectedID,
      bindingCurrent: current, sourceFrame: () => controller.snapshot,
      identityChanges: Listenable.merge([auth, org, controller, _bindingChanges]),
    )));
  }

  void _knowledge(BusinessConsoleSnapshot snapshot) {
    final api = _api,
        controller = c,
        auth = widget.auth,
        org = widget.organizations;
    final businessID = snapshot.business.id;
    final bindingEpoch = _bindingEpoch;
    Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => BusinessKnowledgePage(
          api: api,
          businessID: businessID,
          accountID: () => auth.accountID,
          authorizationHeader: () => auth.authorizationHeader,
          workspaceID: () => org.active?.id,
          currentBusinessID: () => controller.selectedID,
          bindingCurrent: () =>
              mounted &&
              !_disposed &&
              bindingEpoch == _bindingEpoch &&
              identical(_api, api) &&
              identical(c, controller) &&
              identical(widget.auth, auth) &&
              identical(widget.organizations, org) &&
              controller.snapshot?.canManage == true &&
              const {
                'owner',
                'admin',
              }.contains(controller.snapshot?.business.role),
          sourceFrame: () => controller.snapshot,
          identityChanges: Listenable.merge([
            auth,
            org,
            controller,
            _bindingChanges,
          ]),
        ),
      ),
    );
  }

  void _publicPermission(BusinessConsoleSnapshot snapshot) {
    final api = _api,
        controller = c,
        auth = widget.auth,
        org = widget.organizations;
    final identity = (auth.accountID, auth.authorizationHeader, org.active?.id);
    final bindingEpoch = _bindingEpoch;
    var retired = false;
    bool current() {
      if (retired ||
          !mounted ||
          _disposed ||
          bindingEpoch != _bindingEpoch ||
          !identical(_api, api) ||
          !identical(c, controller) ||
          !identical(widget.auth, auth) ||
          !identical(widget.organizations, org) ||
          identity !=
              (auth.accountID, auth.authorizationHeader, org.active?.id) ||
          controller.selectedID != snapshot.business.id ||
          !identical(controller.snapshot, snapshot) ||
          !snapshot.canManageMembers) {
        retired = true;
        return false;
      }
      return true;
    }

    Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => BusinessPublicPermissionPage(
          businessID: snapshot.business.id,
          businessName: snapshot.business.name,
          authorizationHeader: () =>
              current() ? auth.authorizationHeader : null,
          workspaceID: () => current() ? org.active?.id : null,
          identityChanges: Listenable.merge([
            auth,
            org,
            controller,
            _bindingChanges,
          ]),
        ),
      ),
    );
  }

  Widget _fact(String title, String resource, Map<String, dynamic> value) =>
      Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Divider(height: 32),
          Text(title, style: Theme.of(context).textTheme.titleMedium),
          Text(
            '${businessStateLabel(value['state'])} · 版本 ${value['version']}',
          ),
          if (value['validUntil'] != null) Text('有效至 ${value['validUntil']}'),
          if (value['facts'] is Map) Text(_humanValue(value['facts'])),
          if (resource == 'claim')
            Text(
              _humanValue({
                for (final key in ['name', 'description', 'rightsNote'])
                  if (value.containsKey(key)) key: value[key],
              }),
            ),
          if (value['sourceUrl'] != null)
            SelectableText('证明来源：${value['sourceUrl']}'),
          if (c.snapshot?.reviewPermissions.contains(resource) == true &&
              const {'pending', 'verified', 'expired'}.contains(value['state']))
            OutlinedButton(
              onPressed: c.loading || c.saving || c.uncertain
                  ? null
                  : () => _edit('review', resource: resource, facts: value),
              child: Text('审核$title'),
            ),
        ],
      );
  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: c,
    builder: (context, _) {
      if (!c.personal) {
        return Center(
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Text(
              widget.organizations.active != null
                  ? '请切换到个人工作区，再管理获准商家。'
                  : '请先登录本人账号，再申领或管理商家。',
            ),
          ),
        );
      }
      final s = c.snapshot;
      final blocked = c.loading || c.saving || c.uncertain;
      return ListView(
        padding: const EdgeInsets.all(24),
        children: [
          Text('商家工作台', style: Theme.of(context).textTheme.headlineSmall),
          const SizedBox(height: 8),
          const Text('以本人账号管理已获授权的商家。申领、资料核验与场地经营权分别审核。'),
          const SizedBox(height: 16),
          Wrap(
            spacing: 12,
            runSpacing: 8,
            children: [
              OutlinedButton.icon(
                onPressed: blocked ? null : c.load,
                icon: const Icon(Icons.refresh),
                label: const Text('刷新商家列表'),
              ),
              FilledButton.icon(
                onPressed: blocked
                    ? null
                    : () async {
                        c.newClaim();
                        await _edit('claim');
                      },
                icon: const Icon(Icons.add_business_outlined),
                label: const Text('申领商家'),
              ),
            ],
          ),
          if (c.loading || c.saving)
            const Padding(
              padding: EdgeInsets.symmetric(vertical: 16),
              child: LinearProgressIndicator(),
            ),
          if (c.error != null)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 12),
              child: Text(
                c.error!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ),
          if (c.message != null)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 12),
              child: Text(c.message!),
            ),
          if (c.uncertain)
            OutlinedButton(
              onPressed: c.saving || c.loading ? null : c.refresh,
              child: const Text('核实当前商家状态'),
            ),
          if (c.businesses.isEmpty && !c.loading)
            const Padding(
              padding: EdgeInsets.symmetric(vertical: 16),
              child: Text('暂无可管理或审核的商家。已有经营权时可提交申领材料。'),
            ),
          for (final item in c.businesses)
            ListTile(
              contentPadding: EdgeInsets.zero,
              title: Text(item.name),
              subtitle: Text(
                '${businessRoleLabel(item.role)} · ${businessStateLabel(item.claimStatus)}',
              ),
              trailing: const Icon(Icons.chevron_right),
              onTap: blocked ? null : () => c.select(item.id),
            ),
          if (s != null) ...[
            const Divider(height: 32),
            Text(
              s.business.name,
              style: Theme.of(context).textTheme.titleLarge,
            ),
            Text(
              '${businessRoleLabel(s.business.role)} · 经营权${businessStateLabel(s.business.claimStatus)}',
            ),
            OutlinedButton(
              onPressed: blocked ? null : c.refresh,
              child: const Text('重新读取当前资料'),
            ),
            if (s.claim != null) _fact('经营权声明', 'claim', s.claim!),
            if (s.profile != null) _fact('商家资料', 'profile', s.profile!),
            if (s.canManageMembers)
              OutlinedButton(
                onPressed: blocked ? null : () => _publicPermission(s),
                child: const Text('管理资料公开范围'),
              ),
            if (s.canManage) ...[
              OutlinedButton(
                style: OutlinedButton.styleFrom(minimumSize: const Size(48, 48)),
                onPressed: blocked ? null : () => _agentIdentity(s),
                child: const Text('商家智能体身份'),
              ),
              OutlinedButton(
                onPressed: blocked ? null : () => _knowledge(s),
                child: const Text('查看商家资料回答'),
              ),
              OutlinedButton(
                onPressed: blocked
                    ? null
                    : s.business.claimStatus == 'verified'
                    ? null
                    : () => _edit('claim', facts: s.claim),
                child: const Text('更新经营权声明'),
              ),
              OutlinedButton(
                onPressed: blocked
                    ? null
                    : () => _edit('profile', facts: s.profile),
                child: const Text('编辑介绍与营业时间'),
              ),
              OutlinedButton(
                onPressed: blocked || widget.city.places.isEmpty
                    ? null
                    : () => _edit('venue'),
                child: const Text('添加场地经营资料'),
              ),
              if (widget.city.places.isEmpty)
                const Text('当前城市暂无公开场地；可返回首页选择城市后重试。'),
            ],
            for (final v in s.venues) ...[
              _fact('场地 ${v['placeName'] ?? '资料'}', 'venue', v),
              Text('当前经营关系：${businessStateLabel(v['operationStatus'])}'),
              if (s.canManage)
                OutlinedButton(
                  onPressed: blocked ? null : () => _edit('venue', facts: v),
                  child: const Text('编辑这项场地资料'),
                ),
            ],
            if (s.canManageMembers) ...[
              const Divider(height: 32),
              Text('成员与所有权', style: Theme.of(context).textTheme.titleMedium),
              const Text('只有当前所有者能修改成员权限。'),
              for (final m in s.members)
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  title: SelectableText(m['personId'].toString()),
                  subtitle: Text(
                    '${businessRoleLabel(m['role'])} · ${businessStateLabel(m['status'])}',
                  ),
                ),
              OutlinedButton(
                onPressed: blocked ? null : () => _edit('member'),
                child: const Text('管理成员权限'),
              ),
            ],
          ],
        ],
      );
    },
  );
}

class _BusinessEditor extends StatefulWidget {
  const _BusinessEditor({
    required this.controller,
    required this.kind,
    required this.places,
    required this.expectedGeneration,
    required this.frameCurrent,
    required this.bindingChanges,
    this.resource,
    this.existing,
    this.initialDraft,
  });
  final BusinessConsoleController controller;
  final String kind;
  final String? resource;
  final Map<String, dynamic>? existing;
  final BusinessDraft? initialDraft;
  final int expectedGeneration;
  final bool Function() frameCurrent;
  final Listenable bindingChanges;
  final List<PublicPlace> places;
  @override
  State<_BusinessEditor> createState() => _BusinessEditorState();
}

class _BusinessEditorState extends State<_BusinessEditor> {
  final form = GlobalKey<FormState>();
  final values = <String, TextEditingController>{};
  late final int generation;
  String? place;
  String action = 'grant', role = 'admin', decision = 'reject';
  DateTime? expires;
  final hours = <int, Map<String, dynamic>>{};
  @override
  void initState() {
    super.initState();
    generation = widget.expectedGeneration;
    final old = widget.initialDraft == null
        ? widget.existing ?? {}
        : <String, dynamic>{
            ...?widget.existing,
            ...widget.initialDraft!.body,
            'version': widget.initialDraft!.body['expectedVersion'],
            if (widget.initialDraft!.placeID != null)
              'placeId': widget.initialDraft!.placeID,
          };
    if (widget.kind == 'review' &&
        const {'verified', 'expired'}.contains(old['state'])) {
      decision = 'revoke';
    }
    if (widget.initialDraft != null) {
      action = old['action'] as String? ?? action;
      final draftRole = old['role'] as String?;
      if (draftRole != null && draftRole.isNotEmpty) role = draftRole;
      decision = old['decision'] as String? ?? decision;
    }
    final facts = old['facts'] as Map? ?? {};
    for (final key in [
      'name',
      'description',
      'sourceUrl',
      'rightsNote',
      'timeZone',
      'officialLinks',
      'suitability',
      'bookingUrl',
      'note',
      'targetPersonId',
    ]) {
      final initial = facts[key] ?? old[key];
      values[key] = TextEditingController(
        text: initial is List ? initial.join('\n') : initial?.toString() ?? '',
      );
    }
    expires = DateTime.tryParse(old['validUntil']?.toString() ?? '')?.toLocal();
    place = old['placeId'] as String?;
    for (final day in (facts['openingHours'] as List? ?? [])) {
      hours[day['day'] as int] = Map<String, dynamic>.from(day as Map);
    }
    if (widget.kind == 'profile' && values['name']!.text.isEmpty) {
      values['name']!.text = widget.controller.snapshot?.business.name ?? '';
    }
  }

  @override
  void dispose() {
    for (final c in values.values) {
      c.dispose();
    }
    super.dispose();
  }

  Widget field(
    String key, {
    bool required = false,
    bool url = false,
    bool multi = false,
  }) => Padding(
    padding: const EdgeInsets.only(bottom: 16),
    child: TextFormField(
      controller: values[key],
      maxLines: multi ? 3 : 1,
      decoration: InputDecoration(
        labelText: _labels[key],
        border: const OutlineInputBorder(),
      ),
      validator: (v) {
        final s = v?.trim() ?? '';
        if (required && s.isEmpty) return '请填写${_labels[key]}';
        if (url && s.isNotEmpty) {
          final u = Uri.tryParse(s);
          if (u == null ||
              u.scheme != 'https' ||
              u.host.isEmpty ||
              u.userInfo.isNotEmpty ||
              u.fragment.isNotEmpty) {
            return '请填写有效的 HTTPS 来源链接';
          }
        }
        return null;
      },
    ),
  );
  Future<void> _expiry() async {
    final now = DateTime.now();
    final last = now.add(const Duration(days: 364));
    final candidate = expires?.isAfter(now) == true
        ? expires!.toLocal()
        : now.add(const Duration(days: 1));
    final day = await showDatePicker(
      context: context,
      initialDate: candidate.isAfter(last) ? last : candidate,
      firstDate: now,
      lastDate: last,
    );
    if (!mounted || day == null) return;
    final time = await showTimePicker(
      context: context,
      initialTime: expires == null
          ? const TimeOfDay(hour: 12, minute: 0)
          : TimeOfDay.fromDateTime(expires!.toLocal()),
    );
    if (!mounted || time == null) return;
    setState(
      () => expires = DateTime(
        day.year,
        day.month,
        day.day,
        time.hour,
        time.minute,
      ),
    );
  }

  void _finish() {
    final c = widget.controller;
    if (!widget.frameCurrent() ||
        c.generation != generation ||
        !c.personal ||
        !form.currentState!.validate()) {
      return;
    }
    final kind = widget.kind, old = widget.existing;
    String text(String k) => values[k]!.text.trim();
    List<String> lines(String k) => text(
      k,
    ).split('\n').map((s) => s.trim()).where((s) => s.isNotEmpty).toList();
    final body = <String, dynamic>{
      'expectedVersion': kind == 'member'
          ? c.snapshot!.membershipVersion
          : old?['version'] ?? 0,
    };
    if (kind == 'claim') {
      body.addAll({
        'name': text('name'),
        'description': text('description'),
        'sourceUrl': text('sourceUrl'),
        'rightsNote': text('rightsNote'),
      });
    }
    if (kind == 'profile' || kind == 'venue') {
      if (expires == null || !expires!.isAfter(DateTime.now())) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('请选择未来的资料有效期。')));
        return;
      }
      body.addAll({
        'sourceUrl': text('sourceUrl'),
        'rightsNote': text('rightsNote'),
        'validUntil': expires!.toUtc().toIso8601String(),
      });
      if (kind == 'profile') {
        body['facts'] = {
          'name': text('name'),
          'description': text('description'),
          'timeZone': text('timeZone'),
          'openingHours': [
            for (final day in hours.keys.toList()..sort()) hours[day],
          ],
          'officialLinks': lines('officialLinks'),
        };
      } else {
        if (place == null) return;
        body['facts'] = {
          'suitability': lines('suitability'),
          'bookingUrl': text('bookingUrl'),
          'note': text('note'),
        };
      }
    }
    if (kind == 'review') {
      body.addAll({'decision': decision, 'note': text('note')});
    }
    if (kind == 'member') {
      if (!BusinessApi.validID(text('targetPersonId'))) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('请填写对方有效的账号标识。')));
        return;
      }
      body.addAll({
        'targetPersonId': text('targetPersonId'),
        'action': action,
        'role': action == 'remove'
            ? ''
            : action == 'transfer_owner'
            ? 'owner'
            : role,
      });
    }
    Navigator.pop(
      context,
      BusinessDraft(
        kind: kind,
        body: body,
        resource: widget.resource,
        placeID: kind == 'venue' || widget.resource == 'venue' ? place : null,
      ),
    );
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: Text(switch (widget.kind) {
        'claim' => '申领商家',
        'profile' => '编辑商家资料',
        'venue' => '场地经营资料',
        'member' => '管理成员权限',
        _ => '审核具体版本',
      }),
    ),
    body: AnimatedBuilder(
      animation: Listenable.merge([widget.controller, widget.bindingChanges]),
      builder: (context, _) {
        if (!widget.frameCurrent() ||
            generation != widget.controller.generation ||
            !widget.controller.personal) {
          return const Center(
            child: Padding(
              padding: EdgeInsets.all(24),
              child: Text('工作身份已变化，请返回后重新读取资料。'),
            ),
          );
        }
        return Form(
          key: form,
          child: ListView(
            padding: const EdgeInsets.all(24),
            children: [
              Text('商家：${widget.controller.snapshot?.business.name ?? '新商家'}'),
              const SizedBox(height: 16),
              if (widget.kind == 'claim' || widget.kind == 'profile') ...[
                field('name', required: true),
                field('description', multi: true),
              ],
              if (widget.kind == 'profile') ...[
                field('timeZone'),
                const Text('填写营业时间时须指定实际时区，如 Europe/London。未填写的日期表示未知，不代表休息。'),
                for (var day = 1; day <= 7; day++)
                  ExpansionTile(
                    title: Text(
                      '星期${const ['一', '二', '三', '四', '五', '六', '日'][day - 1]}${hours.containsKey(day) ? ' · 已填写' : ' · 未知'}',
                    ),
                    children: [
                      CheckboxListTile(
                        title: const Text('填写这一天'),
                        value: hours.containsKey(day),
                        onChanged: (v) => setState(() {
                          if (v == true) {
                            hours[day] = {
                              'day': day,
                              'closed': false,
                              'opensAt': '',
                              'closesAt': '',
                              'nextDay': false,
                            };
                          } else {
                            hours.remove(day);
                          }
                        }),
                      ),
                      if (hours.containsKey(day)) ...[
                        CheckboxListTile(
                          title: const Text('全天休息'),
                          value: hours[day]!['closed'] as bool,
                          onChanged: (v) => setState(() {
                            hours[day]!['closed'] = v == true;
                            if (v == true) {
                              hours[day]!['opensAt'] = '';
                              hours[day]!['closesAt'] = '';
                              hours[day]!['nextDay'] = false;
                            }
                          }),
                        ),
                        if (hours[day]!['closed'] != true) ...[
                          for (final key in ['opensAt', 'closesAt'])
                            TextFormField(
                              key: ValueKey('$day-$key'),
                              initialValue: hours[day]![key] as String,
                              decoration: InputDecoration(
                                labelText: '${_labels[key]}（时:分）',
                              ),
                              onChanged: (v) => hours[day]![key] = v,
                              validator: (v) =>
                                  RegExp(
                                    r'^(0[0-9]|1[0-9]|2[0-3]):[0-5][0-9]$',
                                  ).hasMatch(v ?? '')
                                  ? null
                                  : '请填写有效的24小时制时间',
                            ),
                          CheckboxListTile(
                            title: const Text('结束时间属于次日'),
                            value: hours[day]!['nextDay'] as bool,
                            onChanged: (v) => setState(
                              () => hours[day]!['nextDay'] = v == true,
                            ),
                          ),
                        ],
                      ],
                    ],
                  ),
                field('officialLinks', multi: true),
                const Text('官方 HTTPS 链接，每行一条；没有时可以留空。'),
              ],
              if (widget.kind == 'venue') ...[
                if (widget.existing != null)
                  ListTile(
                    contentPadding: EdgeInsets.zero,
                    title: const Text('已绑定场地'),
                    subtitle: Text(
                      widget.existing?['placeName']?.toString() ??
                          place ??
                          '场地资料待确认',
                    ),
                  )
                else
                  DropdownButtonFormField<String>(
                    initialValue: widget.places.any((p) => p.id == place)
                        ? place
                        : null,
                    isExpanded: true,
                    decoration: const InputDecoration(labelText: '公开场地'),
                    items: [
                      for (final p in widget.places)
                        DropdownMenuItem(
                          value: p.id,
                          child: Text(p.name, overflow: TextOverflow.ellipsis),
                        ),
                    ],
                    onChanged: widget.existing == null
                        ? (v) => setState(() => place = v)
                        : null,
                    validator: (v) => v == null ? '请选择当前公开场地' : null,
                  ),
                const Text('公开地点不等于已获准场地经营权，服务器会重新核验资格。'),
                const SizedBox(height: 16),
                field('suitability', multi: true),
                field('bookingUrl', url: true),
                field('note', multi: true),
              ],
              if (widget.kind == 'claim' ||
                  widget.kind == 'profile' ||
                  widget.kind == 'venue') ...[
                field('sourceUrl', required: true, url: true),
                field('rightsNote', required: true, multi: true),
              ],
              if (widget.kind == 'profile' || widget.kind == 'venue')
                OutlinedButton(
                  onPressed: _expiry,
                  child: Text(
                    expires == null ? '选择资料有效期' : '有效至 ${expires!.toLocal()}',
                  ),
                ),
              if (widget.kind == 'review') ...[
                Text(
                  '审核版本：${widget.existing?['version']} · ${businessStateLabel(widget.existing?['state'])}',
                ),
                DropdownButtonFormField<String>(
                  initialValue: decision,
                  decoration: const InputDecoration(labelText: '审核决定'),
                  items: [
                    if (widget.existing?['state'] == 'pending') ...const [
                      DropdownMenuItem(value: 'reject', child: Text('不通过')),
                      DropdownMenuItem(value: 'approve', child: Text('审核通过')),
                    ] else
                      const DropdownMenuItem(
                        value: 'revoke',
                        child: Text('撤销核验'),
                      ),
                  ],
                  onChanged: (v) => setState(() => decision = v!),
                ),
                field('note', required: true, multi: true),
              ],
              if (widget.kind == 'member') ...[
                field('targetPersonId', required: true),
                DropdownButtonFormField<String>(
                  initialValue: action,
                  decoration: const InputDecoration(labelText: '成员操作'),
                  items: const [
                    DropdownMenuItem(value: 'grant', child: Text('添加或修改成员权限')),
                    DropdownMenuItem(value: 'remove', child: Text('撤销成员权限')),
                    DropdownMenuItem(
                      value: 'transfer_owner',
                      child: Text('转交所有权'),
                    ),
                  ],
                  onChanged: (v) => setState(() => action = v!),
                ),
                if (action == 'grant')
                  DropdownButtonFormField<String>(
                    initialValue: role,
                    decoration: const InputDecoration(labelText: '授予权限'),
                    items: const [
                      DropdownMenuItem(value: 'admin', child: Text('管理员')),
                      DropdownMenuItem(value: 'member', child: Text('普通成员')),
                    ],
                    onChanged: (v) => setState(() => role = v!),
                  ),
                const Text('转交只允许当前有效的管理员或成员。提交会直接生效，请核对账号标识。'),
              ],
              const SizedBox(height: 24),
              FilledButton(onPressed: _finish, child: const Text('检查并预览')),
              const SizedBox(height: 24),
            ],
          ),
        );
      },
    ),
  );
}
