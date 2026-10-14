import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;
import '../app/birdtie_app_bar.dart';
import '../auth/birdtie_auth_controller.dart';
import '../content/social_preference_seed_sheet.dart';
import 'agent_profile_api.dart';
import 'agent_profile_controller.dart';
import 'agent_memory_correction_page.dart';
import 'agent_memory_candidate_page.dart';
import 'agent_run_page.dart';
import 'agent_activity_join_page.dart';
import 'memory_content_fields.dart';
import 'agent_introduction_page.dart';
import 'person_community_interest_page.dart';
import 'activity_participation_disclosure_page.dart';
import 'notification_policy_page.dart';
import 'model_egress_page.dart';
import 'notification_destination_router.dart';
import 'now_ui_tokens.dart';

class AgentProfilePage extends StatefulWidget {
  const AgentProfilePage({
    super.key,
    required this.auth,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
  });
  final BirdtieAuthController auth;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  @override
  State<AgentProfilePage> createState() => _AgentProfilePageState();
}

class _AgentProfilePageState extends State<AgentProfilePage> {
  late final _auth = widget.auth;
  late final _client = widget.client;
  late final _base = widget.apiBaseUrl;
  late final _changes = widget.workspaceChanges;
  late final _workspace = widget.organizationWorkspaceID;
  late final AgentProfileController _data;
  late final Listenable _nestedChanges;
  late final bool Function() _nestedSourceCurrent = () => _current;
  late final String? Function() _nestedAuthorization = () =>
      _auth.authorizationHeader;
  late final String? Function() _nestedOwner = () => _auth.accountID;
  final _scroll = ScrollController();
  bool _queued = false;
  @override
  void initState() {
    super.initState();
    _data = AgentProfileController(
      api: AgentProfileAPI(client: _client, apiBaseUrl: _base),
      authorizationHeader: () => _auth.authorizationHeader,
      accountID: () => _auth.accountID,
      organizationWorkspaceID: _workspace,
    )..addListener(_changed);
    _nestedChanges = Listenable.merge([_data, _auth, _changes]);
    _auth.addListener(_identity);
    _changes?.addListener(_identity);
    unawaited(_data.load());
  }

  void _changed() {
    if (!mounted) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      if (_queued) return;
      _queued = true;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        _queued = false;
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  void _identity() {
    _data.identityChanged();
    _changed();
  }

  @override
  void didUpdateWidget(AgentProfilePage old) {
    super.didUpdateWidget(old);
    if (!identical(_auth, widget.auth) ||
        !identical(_client, widget.client) ||
        _base != widget.apiBaseUrl ||
        !identical(_changes, widget.workspaceChanges) ||
        !identical(_workspace, widget.organizationWorkspaceID)) {
      _data.retire();
    } else {
      _identity();
    }
  }

  @override
  void dispose() {
    _auth.removeListener(_identity);
    _changes?.removeListener(_identity);
    _data.removeListener(_changed);
    _data.dispose();
    _scroll.dispose();
    super.dispose();
  }

  bool get _current => mounted && _data.current;
  void _open(WidgetBuilder builder) {
    _data.identityChanged();
    if (!_current) return;
    Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => NotificationDestinationBoundary(
          identityChanges: _nestedChanges,
          current: () => _current,
          builder: builder,
        ),
      ),
    );
  }

  Widget _button(String label, VoidCallback action, {Key? key}) => Padding(
    padding: const EdgeInsets.only(top: 8),
    child: OutlinedButton(
      key: key,
      style: OutlinedButton.styleFrom(
        minimumSize: const Size(48, 48),
        padding: const EdgeInsets.all(12),
      ),
      onPressed: _current ? action : null,
      child: Text(label),
    ),
  );
  Widget _notice(String source, {String empty = '当前没有已声明的记录。'}) {
    if (_data.loading.contains(source)) return const Text('正在读取本人当前资料…');
    return Text(_data.errors[source] ?? empty);
  }

  Widget _section(String title, List<Widget> children) => SliverPadding(
    padding: const EdgeInsets.fromLTRB(20, 26, 20, 0),
    sliver: SliverList.list(
      children: [
        Semantics(
          header: true,
          child: Text(title, style: NowUiTokens.of(context).placeTitle),
        ),
        const SizedBox(height: 10),
        ...children,
        const Divider(height: 26),
      ],
    ),
  );
  List<Widget> _memoryRows(Set<String> types) {
    final all = _data.memories;
    if (all == null) return [_notice('memories')];
    final rows = all.where((m) => types.contains(m.raw['memoryType'])).toList();
    if (rows.isEmpty)
      return [
        Text(_data.hasMoreMemories ? '当前页没有这一类记忆，可在管理记忆中继续查看。' : '尚无这一类记忆。'),
      ];
    return [
      if (_data.hasMoreMemories) const Text('仅显示当前页记录，可在管理记忆中继续查看。'),
      for (final m in rows)
        ListTile(
          key: ValueKey('profile-memory-${m.id}'),
          contentPadding: EdgeInsets.zero,
          minTileHeight: 48,
          title: Text(switch (m.status) {
            'ACTIVE' || 'PENDING_REVIEW' => m.summary,
            'DELETED' => '已删除的记忆',
            _ => '已过期的记忆',
          }, style: NowUiTokens.of(context).body),
          subtitle: Padding(
            padding: const EdgeInsets.only(top: 4),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(switch (m.status) {
                  'ACTIVE' => '已确认',
                  'PENDING_REVIEW' => '待确认',
                  'DELETED' => '已删除',
                  _ => '已过期',
                }, style: NowUiTokens.of(context).metadata),
              ],
            ),
          ),
          trailing: const Icon(Icons.chevron_right),
          minVerticalPadding: 12,
          onTap: _current ? () => unawaited(_showDetail(m.id)) : null,
        ),
    ];
  }

  Future<void> _showDetail(String id) async {
    await _data.detail(id);
    if (!_current) return;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_current && _scroll.hasClients) _scroll.jumpTo(0);
    });
  }

  List<Widget> _fields(Iterable<String> keys) {
    final p = _data.profile;
    if (p == null) return [_notice('profile')];
    final result = <Widget>[];
    for (final k in keys) {
      final v = p.fields[k]!;
      final text = v is List ? (v.cast<String>().join('、')) : v as String;
      if (text.isNotEmpty) {
        result.add(
          Padding(
            padding: const EdgeInsets.only(bottom: 12),
            child: Text('${profileFieldLabels[k]}：$text'),
          ),
        );
      }
    }
    return result.isEmpty
        ? [Text(p.configured ? '这一组尚未填写。' : '尚未填写私密智能体资料。')]
        : result;
  }

  Widget _detail() {
    final d = _data.selectedDetail;
    if (d == null) {
      return _data.loading.contains('detail') ||
              _data.errors.containsKey('detail')
          ? _section('当前记忆详情', [_notice('detail')])
          : const SliverToBoxAdapter(child: SizedBox.shrink());
    }
    return _section('当前记忆详情', [
      if (d.memory == null)
        const Text('这条记忆已过期或删除。')
      else ...[
        Text(d.memory!.sourceType == 'EXPLICIT' ? '已确认' : '待确认'),
        const SizedBox(height: 8),
        SelectableText(d.memory!.summary),
        MemoryContentFields(values: d.memory!.raw['structuredValue'] as Map),
      ],
      const SizedBox(height: 8),
      const Text('仅自己可见'),
      _button('管理与纠正记忆', (_openMemories)),
    ]);
  }

  void _openMemories() => _open(
    (_) => AgentMemoryCorrectionPage(
      auth: _auth,
      client: _client,
      apiBaseUrl: _base,
      workspaceChanges: _changes,
      organizationWorkspaceID: _workspace,
    ),
  );
  @override
  Widget build(BuildContext context) {
    if (!_data.current) {
      final reason = _data.retired
          ? '身份或连接已变化，请返回设置重新打开。'
          : _workspace?.call() != null
          ? '我的智能体仅供个人身份查看，请返回个人工作区。'
          : '请先登录个人账号，再查看我的智能体。';
      return Scaffold(
        appBar: birdtieAppBar(context, title: const Text('我的智能体')),
        body: SingleChildScrollView(
          padding: const EdgeInsets.all(24),
          child: Text(reason),
        ),
      );
    }
    final interests = _data.interests,
        activities = _data.participations,
        policies = _data.policies;
    return Scaffold(
      appBar: birdtieAppBar(context, title: const Text('我的智能体')),
      body: SafeArea(
        child: RefreshIndicator(
          onRefresh: _data.load,
          child: CustomScrollView(
            controller: _scroll,
            slivers: [
              SliverPadding(
                padding: const EdgeInsets.fromLTRB(20, 12, 20, 0),
                sliver: SliverToBoxAdapter(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      Text(
                        '仅本人查看',
                        style: Theme.of(context).textTheme.bodySmall,
                      ),
                      _button(
                        '刷新当前资料',
                        () => unawaited(_data.load()),
                        key: const ValueKey('profile-refresh'),
                      ),
                    ],
                  ),
                ),
              ),
              _detail(),
              _section('智能体知道哪些本人资料', [
                ..._fields([
                  'availability',
                  'privateCityHistory',
                  'agentNotes',
                ]),
                ..._memoryRows({
                  'IDENTITY',
                  'CITY',
                  'HISTORY',
                  'INTENT',
                  'AVAILABILITY',
                  'ROUTINE',
                  'EXPERIENCE',
                  'RELATIONSHIP_CONTEXT',
                  'ORGANIZATION',
                }),
                _button(
                  '待确认的记忆',
                  () => _open(
                    (_) => AgentMemoryCandidatePage(
                      auth: _auth,
                      client: _client,
                      apiBaseUrl: _base,
                      workspaceChanges: _changes,
                      organizationWorkspaceID: _workspace,
                    ),
                  ),
                ),
                _button(
                  '智能体处理状态',
                  () => _open(
                    (_) => AgentRunPage(
                      auth: _auth,
                      client: _client,
                      apiBaseUrl: _base,
                      workspaceChanges: _nestedChanges,
                      organizationWorkspaceID: _workspace,
                      sourceCurrent: _nestedSourceCurrent,
                    ),
                  ),
                ),
              ]),
              _section('偏好', [
                ..._fields([
                  'personalPreferences',
                  'socialPreferences',
                  'travelPreferences',
                  'interactionPreferences',
                  'languagePreferences',
                ]),
                ..._memoryRows({'PREFERENCE'}),
                _button(
                  '编辑社交偏好',
                  () => _open(
                    (_) => SocialPreferenceSeedSheet(
                      auth: _auth,
                      client: _client,
                      apiBaseUrl: _base,
                      workspaceChanges: _changes,
                      organizationWorkspaceID: _workspace,
                    ),
                  ),
                ),
                _button('管理我的记忆', _openMemories),
              ]),
              _section('地点', [
                ..._memoryRows({'PLACE'}),
              ]),
              _section('活动', [
                _button(
                  '核实智能体报名',
                  () => _open(
                    (_) => AgentActivityJoinPage(
                      authorizationHeader: _nestedAuthorization,
                      ownerID: _nestedOwner,
                      client: _client,
                      apiBaseUrl: _base,
                      identityChanges: _nestedChanges,
                      organizationWorkspaceID: _workspace,
                      sourceCurrent: _nestedSourceCurrent,
                    ),
                  ),
                  key: const Key('profile-agent-join-recovery'),
                ),
                ..._fields(['preferredActivityTypes']),
                ..._memoryRows({'ACTIVITY'}),
                const Text('我的报名'),
                if (activities == null)
                  _notice('participations')
                else ...[
                  if (activities.records.isEmpty) const Text('当前没有报名记录。'),
                  for (final r in activities.records)
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 8),
                      child: Text(
                        '${r.title}\n${r.sourceAvailable ? '当前报名 · ${r.effectivePublic ? '已明确公开' : '非当前公开'}' : '来源不可公开展示'} · 到场情况未知',
                      ),
                    ),
                  if (activities.truncated)
                    const Text('仅展示前 100 条报名，暂不能查看后续记录。'),
                ],
                _button(
                  '管理报名的公开范围',
                  () => _open(
                    (_) => ActivityParticipationDisclosurePage(
                      auth: _auth,
                      client: _client,
                      apiBaseUrl: _base,
                      workspaceChanges: _changes,
                      organizationWorkspaceID: _workspace,
                    ),
                  ),
                ),
              ]),
              _section('社群', [
                ..._memoryRows({'COMMUNITY'}),
                const Text('感兴趣的社群'),
                if (interests == null)
                  _notice('interests')
                else ...[
                  if (interests.records
                      .where((r) => r.state != 'ABSENT')
                      .isEmpty)
                    const Text('当前没有社群兴趣声明。'),
                  for (final r in interests.records.where(
                    (r) => r.state != 'ABSENT',
                  ))
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 8),
                      child: Text(
                        '${r.sourceAvailable ? r.name : '当前不可展示的社群兴趣'}\n${r.sourceAvailable ? '本人兴趣 · ${r.state == 'PUBLIC' ? '公开' : '私密'}' : '当前来源不可展示'}',
                      ),
                    ),
                  if (interests.truncated)
                    const Text('仅展示前 100 条声明，暂不能查看后续记录。'),
                ],
                _button(
                  '管理社群兴趣',
                  () => _open(
                    (_) => PersonCommunityInterestPage(
                      auth: _auth,
                      client: _client,
                      apiBaseUrl: _base,
                      workspaceChanges: _changes,
                      organizationWorkspaceID: _workspace,
                    ),
                  ),
                ),
              ]),
              _section('智能体设置', [
                if (policies == null)
                  _notice('policies')
                else
                  for (final f in policies.records.keys)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 12),
                      child: Text(
                        '${const {'ATTENTION': '注意力设置', 'SOCIAL': '社交设置', 'AUTONOMY': '自主性设置'}[f]}：${policies.records[f]!.status == 'ACTIVE'
                            ? policies.records[f]!.caption
                            : policies.records[f]!.status == 'UNCONFIGURED'
                            ? '尚未配置'
                            : '已到期'}',
                      ),
                    ),
                _button(
                  '检查社交建议设置',
                  () => _open(
                    (_) => AgentIntroductionPage(
                      auth: _auth,
                      client: _client,
                      apiBaseUrl: _base,
                      workspaceChanges: _changes,
                      organizationWorkspaceID: _workspace,
                    ),
                  ),
                ),
                _button(
                  '通知偏好',
                  () => _open(
                    (_) => NotificationPolicyPage(
                      auth: _auth,
                      client: _client,
                      apiBaseUrl: _base,
                      workspaceChanges: _changes,
                      organizationWorkspaceID: _workspace,
                    ),
                  ),
                ),
                _button(
                  '模型出口与预算',
                  () => _open(
                    (_) => ModelEgressPage(
                      auth: _auth,
                      client: _client,
                      apiBaseUrl: _base,
                      workspaceChanges: _changes,
                      organizationWorkspaceID: _workspace,
                    ),
                  ),
                ),
              ]),
              const SliverToBoxAdapter(child: SizedBox(height: 36)),
            ],
          ),
        ),
      ),
    );
  }
}
