import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import '../auth/birdtie_auth_controller.dart';
import '../city/public_city_controller.dart';
import '../content/private_moment_controller.dart';
import '../content/agent_seed_sheet.dart';
import '../content/social_preference_seed_sheet.dart';
import '../legacy/legacy_shell.dart';
import 'agent_debug_panel.dart';
import 'support_page.dart';
import 'person_contexts_page.dart';
import 'person_community_interest_page.dart';
import 'activity_participation_disclosure_page.dart';
import 'social_disclosure_page.dart';
import 'agent_relationship_page.dart';
import 'new_people_page.dart';
import 'opportunity_page.dart';
import 'social_now_page.dart';
import 'notification_policy_page.dart';
import 'notification_schedule_page.dart';
import 'message_request_policy_page.dart';
import 'agent_memory_candidate_page.dart';
import 'agent_memory_correction_page.dart';
import 'agent_profile_page.dart';
import 'model_egress_page.dart';
import 'enrichment_privacy_page.dart';
import 'task_context_privacy_page.dart';
import 'agent_profile_visibility_page.dart';
import 'agent_introduction_page.dart';
import 'chat_entity_router.dart';
import 'notification_destination_router.dart';
import '../config/birdtie_environment.dart';

class SettingsPage extends StatefulWidget {
  const SettingsPage({
    super.key,
    required this.auth,
    required this.city,
    required this.moments,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
  });

  final BirdtieAuthController auth;
  final PublicCityController city;
  final PrivateMomentController moments;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;

  @override
  State<SettingsPage> createState() => _SettingsPageState();
}

class _SettingsPageState extends State<SettingsPage> {
  static const _defaultApiBase = BirdtieEnvironment.apiBaseUrl;
  late http.Client _client;
  late bool _ownsClient;
  final _entryChanges = ValueNotifier<int>(0);
  int _entryEpoch = 0;
  late (String?, String?, String?) _settingsIdentity;
  (String?, String?, String?) get _currentIdentity => (
    widget.auth.authorizationHeader,
    widget.auth.accountID,
    widget.organizationWorkspaceID?.call(),
  );
  String get _apiBase => widget.apiBaseUrl ?? _defaultApiBase;
  List<Map<String, dynamic>> _blocks = const [];
  List<Map<String, dynamic>> _grants = const [];
  bool _loading = false;
  bool _failed = false;
  String? _workingID;
  int _serial = 0;

  Uri _endpoint(String path) =>
      Uri.parse('${_apiBase.replaceFirst(RegExp(r'/$'), '')}$path');

  @override
  void initState() {
    super.initState();
    _client = widget.client ?? http.Client();
    _ownsClient = widget.client == null;
    _settingsIdentity = _currentIdentity;
    widget.auth.addListener(_onAuthChanged);
    widget.workspaceChanges?.addListener(_onAuthChanged);
    if (widget.auth.signedIn &&
        widget.organizationWorkspaceID?.call() == null) {
      unawaited(_load());
    }
  }

  void _onAuthChanged() {
    if (_settingsIdentity != _currentIdentity) {
      _resetBinding();
      return;
    }
    if (!widget.auth.signedIn) {
      ++_serial;
      setState(() {
        _blocks = const [];
        _grants = const [];
        _loading = false;
        _workingID = null;
      });
    } else if (_blocks.isEmpty && _grants.isEmpty && !_loading) {
      unawaited(_load());
    } else {
      setState(() {});
    }
  }

  void _resetBinding() {
    ++_serial;
    ++_entryEpoch;
    _settingsIdentity = _currentIdentity;
    _entryChanges.value++;
    setState(() {
      _blocks = const [];
      _grants = const [];
      _loading = _failed = false;
      _workingID = null;
    });
    if (widget.auth.signedIn) unawaited(_load());
  }

  @override
  void didUpdateWidget(SettingsPage old) {
    super.didUpdateWidget(old);
    if (old.auth != widget.auth) {
      old.auth.removeListener(_onAuthChanged);
      widget.auth.addListener(_onAuthChanged);
    }
    if (old.workspaceChanges != widget.workspaceChanges) {
      old.workspaceChanges?.removeListener(_onAuthChanged);
      widget.workspaceChanges?.addListener(_onAuthChanged);
    }
    if (old.client != widget.client) {
      if (_ownsClient) _client.close();
      _client = widget.client ?? http.Client();
      _ownsClient = widget.client == null;
    }
    if (old.auth != widget.auth ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.workspaceChanges != widget.workspaceChanges ||
        old.organizationWorkspaceID != widget.organizationWorkspaceID) {
      _resetBinding();
    } else {
      _onAuthChanged();
    }
  }

  Future<void> _load() async {
    final token = widget.auth.authorizationHeader;
    if (token == null ||
        _apiBase.isEmpty ||
        widget.organizationWorkspaceID?.call() != null) {
      return;
    }
    final epoch = _entryEpoch;
    bool current() =>
        mounted &&
        epoch == _entryEpoch &&
        widget.auth.authorizationHeader == token &&
        widget.organizationWorkspaceID?.call() == null;
    final serial = ++_serial;
    setState(() {
      _loading = true;
      _failed = false;
    });
    try {
      final responses = await Future.wait([
        _client.get(
          _endpoint('/v1/me/blocks'),
          headers: {'Authorization': token},
        ),
        _client.get(
          _endpoint('/v1/me/consents'),
          headers: {'Authorization': token},
        ),
      ]).timeout(const Duration(seconds: 12));
      if (responses.any((response) => response.statusCode != 200)) {
        throw StateError('Settings unavailable');
      }
      if (!current() || serial != _serial || !widget.auth.signedIn) return;
      setState(() {
        _blocks = [
          for (final raw
              in (jsonDecode(responses[0].body) as Map<String, dynamic>)['data']
                  as List<dynamic>)
            raw as Map<String, dynamic>,
        ];
        _grants = [
          for (final raw
              in (jsonDecode(responses[1].body) as Map<String, dynamic>)['data']
                  as List<dynamic>)
            raw as Map<String, dynamic>,
        ];
      });
    } catch (_) {
      if (current() && serial == _serial) setState(() => _failed = true);
    } finally {
      if (current() && serial == _serial) setState(() => _loading = false);
    }
  }

  Future<void> _remove(String path, String id) async {
    final token = widget.auth.authorizationHeader;
    if (token == null ||
        _workingID != null ||
        widget.organizationWorkspaceID?.call() != null) {
      return;
    }
    final epoch = _entryEpoch;
    bool current() =>
        mounted &&
        epoch == _entryEpoch &&
        widget.auth.authorizationHeader == token &&
        widget.organizationWorkspaceID?.call() == null;
    setState(() => _workingID = id);
    try {
      final response = await _client
          .delete(_endpoint(path), headers: {'Authorization': token})
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 204) {
        throw StateError('Settings update failed');
      }
      if (current()) await _load();
    } catch (_) {
      if (mounted && current()) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('隐私设置更新失败，请重试。')));
      }
    } finally {
      if (current()) setState(() => _workingID = null);
    }
  }

  void _openProfile() {
    if (widget.organizationWorkspaceID?.call() != null) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('请切换到个人身份后管理个人资料。')));
      return;
    }
    final auth = widget.auth, client = _client, city = widget.city;
    final moments = widget.moments, base = _apiBase;
    final changes = widget.workspaceChanges;
    final workspace = widget.organizationWorkspaceID;
    _openPersonalRoute(
      (context) => Scaffold(
        appBar: AppBar(title: const Text('个人资料')),
        body: LegacyProfilePage(
          auth: auth,
          city: city,
          moments: moments,
          client: client,
          apiBaseUrl: base,
          workspaceChanges: changes,
          organizationWorkspaceID: workspace,
        ),
      ),
    );
  }

  void _openIntroductions() {
    final auth = widget.auth, client = widget.client;
    final base = _apiBase, changes = widget.workspaceChanges;
    final workspace = widget.organizationWorkspaceID;
    final identity = (
      auth.authorizationHeader,
      auth.accountID,
      workspace?.call(),
    );
    final epoch = _entryEpoch;
    final entryChanges = Listenable.merge([_entryChanges, auth, changes]);
    bool current() =>
        mounted &&
        epoch == _entryEpoch &&
        auth == widget.auth &&
        client == widget.client &&
        base == _apiBase &&
        identity ==
            (auth.authorizationHeader, auth.accountID, workspace?.call());
    void findPeople(BuildContext inner, [String? source]) {
      if (!current()) return;
      Navigator.of(inner).push<void>(
        MaterialPageRoute(
          builder: (_) => NewPeoplePage(
            auth: auth,
            client: client,
            apiBaseUrl: base,
            workspaceChanges: changes,
            organizationWorkspaceID: workspace,
            initialSourceIntentID: source,
          ),
        ),
      );
    }

    Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => NotificationDestinationBoundary(
          identityChanges: entryChanges,
          current: current,
          builder: (inner) => AgentIntroductionPage(
            auth: auth,
            client: client,
            apiBaseUrl: base,
            workspaceChanges: changes,
            organizationWorkspaceID: workspace,
            onManageFindPeople: () => findPeople(inner),
            onFindPeople: (source) => findPeople(inner, source),
            onOpenPerson: (id) {
              if (!current()) return;
              Navigator.of(inner).push<void>(
                MaterialPageRoute(
                  builder: (_) => ChatEntityDetail(
                    type: 'person',
                    id: id,
                    auth: auth,
                    client: client,
                    apiBaseUrl: base,
                    workspaceChanges: changes,
                    workspaceID: workspace,
                  ),
                ),
              );
            },
          ),
        ),
      ),
    );
  }

  void _openPersonalRoute(WidgetBuilder builder) {
    final epoch = _entryEpoch, auth = widget.auth, client = widget.client;
    final ownedClient = _client, base = _apiBase, identity = _currentIdentity;

    final changes = Listenable.merge([
      _entryChanges,
      auth,
      widget.workspaceChanges,
    ]);
    Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => NotificationDestinationBoundary(
          identityChanges: changes,
          current: () =>
              mounted &&
              epoch == _entryEpoch &&
              auth == widget.auth &&
              client == widget.client &&
              ownedClient == _client &&
              base == _apiBase &&
              identity == _currentIdentity,
          builder: builder,
        ),
      ),
    );
  }

  void _openProfileVisibility() {
    final epoch = _entryEpoch, auth = widget.auth, client = widget.client;
    final ownedClient = _client, base = _apiBase, identity = _currentIdentity;
    final changes = Listenable.merge([_entryChanges, auth, widget.workspaceChanges]);
    final workspace = widget.organizationWorkspaceID;
    bool current() =>
        mounted && epoch == _entryEpoch && auth == widget.auth &&
        client == widget.client && ownedClient == _client &&
        base == _apiBase && identity == _currentIdentity;
    _openPersonalRoute(
      (_) => AgentProfileVisibilityPage(
        auth: auth, client: client, apiBaseUrl: base,
        workspaceChanges: changes, organizationWorkspaceID: workspace, current: current,
      ),
    );
  }

  void _openTaskContextPrivacy() {
    final epoch = _entryEpoch, auth = widget.auth, client = widget.client;
    final ownedClient = _client, base = _apiBase, identity = _currentIdentity;
    final changes = Listenable.merge([_entryChanges, auth, widget.workspaceChanges]);
    final workspace = widget.organizationWorkspaceID;
    bool current() => mounted && epoch == _entryEpoch && auth == widget.auth &&
        client == widget.client && ownedClient == _client && base == _apiBase &&
        identity == _currentIdentity;
    _openPersonalRoute(
      (_) => TaskContextPrivacyPage(
        auth: auth, client: client, apiBaseUrl: base,
        workspaceChanges: changes, organizationWorkspaceID: workspace, current: current,
      ),
    );
  }

  void _openNotificationSchedule() {
    final epoch = _entryEpoch, auth = widget.auth, client = widget.client;
    final ownedClient = _client, base = _apiBase, identity = _currentIdentity;
    final changes = Listenable.merge([
      _entryChanges,
      auth,
      widget.workspaceChanges,
    ]);
    final workspace = widget.organizationWorkspaceID;
    bool current() =>
        mounted &&
        epoch == _entryEpoch &&
        auth == widget.auth &&
        client == widget.client &&
        ownedClient == _client &&
        base == _apiBase &&
        identity == _currentIdentity;
    _openPersonalRoute(
      (_) => NotificationSchedulePage(
        auth: auth,
        client: client,
        apiBaseUrl: base,
        workspaceChanges: changes,
        organizationWorkspaceID: workspace,
        current: current,
      ),
    );
  }

  void _openEnrichmentPrivacy() {
    final epoch = _entryEpoch, auth = widget.auth, client = widget.client;
    final ownedClient = _client, base = _apiBase, identity = _currentIdentity;
    final changes = widget.workspaceChanges;
    final workspace = widget.organizationWorkspaceID;
    bool current() =>
        mounted &&
        epoch == _entryEpoch &&
        auth == widget.auth &&
        client == widget.client &&
        ownedClient == _client &&
        base == _apiBase &&
        identity == _currentIdentity;
    _openPersonalRoute(
      (_) => EnrichmentPrivacyPage(
        auth: auth,
        client: client,
        apiBaseUrl: base,
        workspaceChanges: changes,
        organizationWorkspaceID: workspace,
        current: current,
        onReviewSources: () {
          if (current()) _openMemoryCandidates();
        },
      ),
    );
  }

  void _openSocialNow() {
    final epoch = _entryEpoch, auth = widget.auth, client = widget.client;
    final ownedClient = _client, base = _apiBase, identity = _currentIdentity;
    final changes = widget.workspaceChanges;
    final workspace = widget.organizationWorkspaceID;
    bool current() =>
        mounted &&
        epoch == _entryEpoch &&
        auth == widget.auth &&
        client == widget.client &&
        ownedClient == _client &&
        base == _apiBase &&
        identity == _currentIdentity;
    _openPersonalRoute(
      (_) => SocialNowPage(
        auth: auth,
        apiBaseUrl: base,
        client: client,
        workspaceChanges: changes,
        organizationWorkspaceID: workspace,
        current: current,
      ),
    );
  }

  void _openNotificationPolicy() {
    final epoch = _entryEpoch, auth = widget.auth, client = widget.client;
    final ownedClient = _client, base = _apiBase, identity = _currentIdentity;
    final changes = widget.workspaceChanges;
    final workspace = widget.organizationWorkspaceID;
    bool current() =>
        mounted &&
        epoch == _entryEpoch &&
        auth == widget.auth &&
        client == widget.client &&
        ownedClient == _client &&
        base == _apiBase &&
        identity == _currentIdentity;
    _openPersonalRoute(
      (_) => NotificationPolicyPage(
        auth: auth,
        client: client,
        apiBaseUrl: base,
        workspaceChanges: changes,
        organizationWorkspaceID: workspace,
        current: current,
      ),
    );
  }

  void _openMemoryCorrection() {
    final epoch = _entryEpoch, auth = widget.auth, client = widget.client;
    final ownedClient = _client, base = _apiBase, identity = _currentIdentity;
    final changes = widget.workspaceChanges;
    final workspace = widget.organizationWorkspaceID;
    bool current() =>
        mounted &&
        epoch == _entryEpoch &&
        auth == widget.auth &&
        client == widget.client &&
        ownedClient == _client &&
        base == _apiBase &&
        identity == _currentIdentity;
    _openPersonalRoute(
      (_) => AgentMemoryCorrectionPage(
        auth: auth,
        client: client,
        apiBaseUrl: base,
        workspaceChanges: changes,
        organizationWorkspaceID: workspace,
        current: current,
      ),
    );
  }

  void _openMemoryCandidates() {
    final epoch = _entryEpoch, auth = widget.auth, client = widget.client;
    final ownedClient = _client, base = _apiBase, identity = _currentIdentity;
    final changes = widget.workspaceChanges;
    final workspace = widget.organizationWorkspaceID;
    bool current() =>
        mounted &&
        epoch == _entryEpoch &&
        auth == widget.auth &&
        client == widget.client &&
        ownedClient == _client &&
        base == _apiBase &&
        identity == _currentIdentity;
    _openPersonalRoute(
      (_) => AgentMemoryCandidatePage(
        auth: auth,
        client: client,
        apiBaseUrl: base,
        workspaceChanges: changes,
        organizationWorkspaceID: workspace,
        current: current,
      ),
    );
  }

  void _openSeed({bool progressive = false}) {
    final auth = widget.auth, client = widget.client;
    final base = _apiBase, changes = widget.workspaceChanges;
    final workspace = widget.organizationWorkspaceID;
    _openPersonalRoute(
      (_) => AgentSeedSheet(
        auth: auth,
        client: client,
        apiBaseUrl: base,
        workspaceChanges: changes,
        organizationWorkspaceID: workspace,
        progressive: progressive,
      ),
    );
  }

  void _openPersonContexts() {
    final epoch = _entryEpoch, auth = widget.auth, client = widget.client;
    final ownedClient = _client, base = _apiBase, identity = _currentIdentity;
    bool current() =>
        mounted &&
        epoch == _entryEpoch &&
        auth == widget.auth &&
        client == widget.client &&
        ownedClient == _client &&
        base == _apiBase &&
        identity == _currentIdentity;
    _openPersonalRoute(
      (_) => PersonContextsPage(
        auth: auth,
        client: client,
        apiBaseUrl: base,
        current: current,
      ),
    );
  }

  void _openSocialVisibility({required bool relationship}) {
    final epoch = _entryEpoch, auth = widget.auth, client = widget.client;
    final ownedClient = _client, base = _apiBase, identity = _currentIdentity;
    bool current() =>
        mounted &&
        epoch == _entryEpoch &&
        auth == widget.auth &&
        client == widget.client &&
        ownedClient == _client &&
        base == _apiBase &&
        identity == _currentIdentity;
    _openPersonalRoute(
      (_) => relationship
          ? AgentRelationshipPage(
              auth: auth,
              client: client,
              apiBaseUrl: base,
              current: current,
            )
          : SocialDisclosurePage(
              auth: auth,
              client: client,
              apiBaseUrl: base,
              current: current,
            ),
    );
  }

  void _openSocialPreferences() {
    final epoch = _entryEpoch, auth = widget.auth, client = widget.client;
    final ownedClient = _client, base = _apiBase, identity = _currentIdentity;
    final changes = widget.workspaceChanges;
    final workspace = widget.organizationWorkspaceID;
    bool current() =>
        mounted &&
        epoch == _entryEpoch &&
        auth == widget.auth &&
        client == widget.client &&
        ownedClient == _client &&
        base == _apiBase &&
        identity == _currentIdentity;
    _openPersonalRoute(
      (_) => SocialPreferenceSeedSheet(
        auth: auth,
        client: client,
        apiBaseUrl: base,
        workspaceChanges: changes,
        organizationWorkspaceID: workspace,
        current: current,
      ),
    );
  }

  void _openCommunityInterests() {
    final auth = widget.auth, client = widget.client;
    final base = _apiBase, changes = widget.workspaceChanges;
    final workspace = widget.organizationWorkspaceID;
    _openPersonalRoute(
      (_) => PersonCommunityInterestPage(
        auth: auth,
        client: client,
        apiBaseUrl: base,
        workspaceChanges: changes,
        organizationWorkspaceID: workspace,
      ),
    );
  }

  void _openParticipationDisclosures() {
    final auth = widget.auth, client = widget.client;
    final base = _apiBase, changes = widget.workspaceChanges;
    final workspace = widget.organizationWorkspaceID;
    _openPersonalRoute(
      (_) => ActivityParticipationDisclosurePage(
        auth: auth,
        client: client,
        apiBaseUrl: base,
        workspaceChanges: changes,
        organizationWorkspaceID: workspace,
      ),
    );
  }

  @override
  void dispose() {
    ++_serial;
    ++_entryEpoch;
    _entryChanges.value++;
    widget.auth.removeListener(_onAuthChanged);
    widget.workspaceChanges?.removeListener(_onAuthChanged);
    _entryChanges.dispose();
    if (_ownsClient) _client.close();
    super.dispose();
  }

  Widget _debugEntry() => ExpansionTile(
    key: const PageStorageKey('settings-debug-diagnostics'),
    leading: const Icon(Icons.bug_report_outlined),
    title: const Text('运行诊断（开发版）'),
    children: [
      AgentDebugPanel(apiBase: _apiBase, signedIn: widget.auth.signedIn),
    ],
  );

  @override
  Widget build(BuildContext context) {
    if (_apiBase.isEmpty) {
      return ListView(
        children: [
          if (kDebugMode) _debugEntry(),
          const Center(child: Text('请连接 Birdtie API 后管理设置。')),
        ],
      );
    }
    if (!widget.auth.signedIn) {
      return ListView(
        children: [
          if (kDebugMode) _debugEntry(),
          const Center(child: Text('登录后即可管理设置。')),
          ListTile(
            leading: const Icon(Icons.login),
            title: const Text('登录 Birdtie'),
            subtitle: Text(
              widget.auth.available ? '通过当前可用的身份服务登录。' : '登录服务暂不可用，仍可浏览公开城市内容。',
            ),
            onTap: widget.auth.available && !widget.auth.busy
                ? widget.auth.signIn
                : null,
          ),
          ListTile(
            leading: const Icon(Icons.help_outline),
            title: const Text('帮助与举报'),
            onTap: () => Navigator.push(
              context,
              MaterialPageRoute<void>(
                builder: (_) => SupportPage(
                  authorizationHeader: () => widget.auth.authorizationHeader,
                  apiBaseUrl: _apiBase,
                ),
              ),
            ),
          ),
        ],
      );
    }
    final now = DateTime.now();
    final activeGrants = _grants.where((grant) {
      if (grant['revokedAt'] != null) return false;
      final expiry = DateTime.tryParse(grant['expiresAt'] as String? ?? '');
      return expiry == null || expiry.isAfter(now);
    });
    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(18, 20, 18, 36),
        children: [
          if (kDebugMode) _debugEntry(),
          Text(
            widget.auth.displayName ?? 'Birdtie 账号',
            style: Theme.of(context).textTheme.titleLarge,
          ),
          Text(
            widget.auth.loginMethod == 'dev_phone'
                ? '本地开发账号 · 尚未验证手机号归属'
                : '已登录 Birdtie',
            style: const TextStyle(color: Color(0xFF747B73)),
          ),
          const SizedBox(height: 16),
          ListTile(
            leading: const Icon(Icons.person_search_outlined),
            title: const Text('我的智能体'),
            subtitle: const Text('查看本人资料、记忆、兴趣、报名与当前设置。'),
            onTap: widget.organizationWorkspaceID?.call() == null
                ? () {
                    final auth = widget.auth, client = widget.client;
                    final base = _apiBase, changes = widget.workspaceChanges;
                    final workspace = widget.organizationWorkspaceID;
                    _openPersonalRoute(
                      (_) => AgentProfilePage(
                        auth: auth,
                        client: client,
                        apiBaseUrl: base,
                        workspaceChanges: changes,
                        organizationWorkspaceID: workspace,
                      ),
                    );
                  }
                : null,
          ),
          ListTile(
            leading: const Icon(Icons.edit_note),
            title: const Text('管理我的记忆'),
            subtitle: const Text('修改、纠正、拒绝或删除本人记忆'),
            onTap: widget.organizationWorkspaceID?.call() == null
                ? _openMemoryCorrection
                : null,
          ),
          ListTile(
            leading: const Icon(Icons.tune),
            title: const Text('我的初始设置'),
            subtitle: const Text('昵称、城市声明、交流语言、私密意图与可选兴趣。'),
            onTap: _openSeed,
          ),
          ListTile(
            leading: const Icon(Icons.edit_note),
            title: const Text('完善我的选择'),
            subtitle: const Text('一次选一组，不重复填写已有设置。'),
            onTap: () => _openSeed(progressive: true),
          ),
          ListTile(
            leading: const Icon(Icons.person_outline),
            title: const Text('个人资料与可见范围'),
            subtitle: const Text('编辑公开名称、简介和可见范围。'),
            onTap: _openProfile,
          ),
          ListTile(
            leading: const Icon(Icons.visibility_outlined),
            title: const Text('各项资料的可见范围'),
            subtitle: const Text('逐项选择谁可以读取；不会开启模型或自动学习。'),
            onTap: _openProfileVisibility,
          ),
          ListTile(
            leading: const Icon(Icons.explore_outlined),
            title: const Text('我的生活情境'),
            subtitle: const Text('记录当前城市、过往学校、目的地和线上兴趣。'),
            onTap: _openPersonContexts,
          ),
          ListTile(
            leading: const Icon(Icons.people_outline),
            title: const Text('社交偏好'),
            subtitle: const Text('选择喜欢的社交方式，私密保存，也可以跳过。'),
            onTap: _openSocialPreferences,
          ),
          const Divider(height: 32),
          ListTile(
            leading: const Icon(Icons.people_outline),
            title: const Text('我的社交近况'),
            subtitle: const Text('查看好友分享的想法与相关活动，打开前核对当前来源。'),
            onTap: _openSocialNow,
          ),
          ListTile(
            leading: const Icon(Icons.notifications_outlined),
            title: const Text('通知设置'),
            subtitle: const Text('按类型选择提醒方式，关闭自定义会恢复普通提醒。'),
            onTap: _openNotificationPolicy,
          ),
          ListTile(
            leading: const Icon(Icons.schedule_outlined),
            title: const Text('定时汇总计划'),
            subtitle: const Text('明确时区、汇总时间与额度；只保存计划，不保证已投递。'),
            onTap: _openNotificationSchedule,
          ),
          Text('隐私与安全', style: Theme.of(context).textTheme.titleMedium),
          ListTile(
            leading: const Icon(Icons.mark_email_unread_outlined),
            title: const Text('消息请求设置'),
            subtitle: const Text('由本人决定请求处理方式；人工审阅不会自动接受或聊天。'),
            onTap: widget.organizationWorkspaceID?.call() == null
                ? () {
                    final auth = widget.auth, client = widget.client;
                    final base = _apiBase, changes = widget.workspaceChanges;
                    final workspace = widget.organizationWorkspaceID;
                    _openPersonalRoute(
                      (_) => MessageRequestPolicyPage(
                        auth: auth, client: client, apiBaseUrl: base,
                        workspaceChanges: changes,
                        organizationWorkspaceID: workspace,
                      ),
                    );
                  }
                : null,
          ),
          ListTile(
            leading: const Icon(Icons.manage_search_outlined),
            title: const Text('模型请求与预算'),
            subtitle: const Text('核对请求内容、授权与预算；批准不会直接运行模型。'),
            onTap: () => _openPersonalRoute(
              (_) => ModelEgressPage(
                auth: widget.auth,
                client: widget.client,
                apiBaseUrl: _apiBase,
                workspaceChanges: widget.workspaceChanges,
                organizationWorkspaceID: widget.organizationWorkspaceID,
              ),
            ),
          ),
          ListTile(
            leading: const Icon(Icons.privacy_tip_outlined),
            title: const Text('本地分析许可'),
            subtitle: const Text('查看并撤回已允许的本地分析；不会开启模型或自动学习。'),
            onTap: _openEnrichmentPrivacy,
          ),
          ListTile(
            leading: const Icon(Icons.rule_folder_outlined),
            title: const Text('本次会话的任务资料许可'),
            subtitle: const Text('查看当前任务允许使用的资料范围，检查具体许可后撤回。'),
            onTap: _openTaskContextPrivacy,
          ),
          ListTile(
            leading: const Icon(Icons.fact_check_outlined),
            title: const Text('待确认的记忆'),
            subtitle: const Text('选择自己的记录，核对来源后确认保存；不会自动分析或公开。'),
            onTap: _openMemoryCandidates,
          ),
          ListTile(
            leading: const Icon(Icons.event_available_outlined),
            title: const Text('为你找到的活动'),
            subtitle: const Text('查看与你的意图相符的活动及推荐理由。'),
            onTap: () => Navigator.push(
              context,
              MaterialPageRoute<void>(
                builder: (_) => OpportunityPage(
                  auth: widget.auth,
                  apiBaseUrl: _apiBase,
                  client: widget.client,
                ),
              ),
            ),
          ),
          ListTile(
            leading: const Icon(Icons.person_add_alt_1_outlined),
            title: const Text('引荐与社交许可'),
            subtitle: const Text('检查有效公开意图的建议；联系对方仍需单独确认。'),
            onTap: _openIntroductions,
          ),
          ListTile(
            leading: const Icon(Icons.groups_outlined),
            title: const Text('我的社群兴趣'),
            subtitle: const Text('管理本人兴趣声明，公开前检查；不代表加入社群。'),
            onTap: _openCommunityInterests,
          ),
          ListTile(
            leading: const Icon(Icons.event_available_outlined),
            title: const Text('我的报名可见范围'),
            subtitle: const Text('默认私密；公开前检查期限。隐藏声明不会取消报名。'),
            onTap: _openParticipationDisclosures,
          ),
          ListTile(
            leading: const Icon(Icons.person_add_alt_1_outlined),
            title: const Text('找新朋友'),
            subtitle: const Text('公开找伙伴意图后，自主查看候选与发送申请。'),
            onTap: () => _openPersonalRoute(
              (_) => NewPeoplePage(
                auth: widget.auth,
                apiBaseUrl: _apiBase,
                client: widget.client,
                workspaceChanges: widget.workspaceChanges,
                organizationWorkspaceID: widget.organizationWorkspaceID,
              ),
            ),
          ),
          ListTile(
            leading: const Icon(Icons.psychology_outlined),
            title: const Text('Agent 关系信号'),
            subtitle: const Text('默认关闭，审阅个人 Agent 可使用的互动记录。'),
            onTap: () => _openSocialVisibility(relationship: true),
          ),
          ListTile(
            leading: const Icon(Icons.people_outline),
            title: const Text('共同信息展示'),
            subtitle: const Text('决定是否展示共同好友、公开社群和活动。'),
            onTap: () => _openSocialVisibility(relationship: false),
          ),
          const SizedBox(height: 8),
          if (_loading) const LinearProgressIndicator(),
          if (_failed)
            TextButton(onPressed: _load, child: const Text('隐私设置暂不可用，点击重试。')),
          const SizedBox(height: 16),
          const Text('已屏蔽账号'),
          if (!_loading && !_failed && _blocks.isEmpty) const Text('没有已屏蔽的账号。'),
          for (final block in _blocks)
            ListTile(
              title: Text(block['accountId'] as String? ?? '账号'),
              subtitle: const Text('双方将无法查看彼此的个人资料和公开活动。'),
              trailing: TextButton(
                onPressed: _workingID != null
                    ? null
                    : () => _remove(
                        '/v1/me/blocks/${Uri.encodeComponent(block['accountId'] as String)}',
                        block['accountId'] as String,
                      ),
                child: const Text('取消屏蔽'),
              ),
            ),
          const SizedBox(height: 20),
          const Text('个人资料访问授权'),
          if (!_loading && !_failed && activeGrants.isEmpty)
            const Text('当前没有有效的个人资料访问授权。'),
          for (final grant in activeGrants)
            ListTile(
              title: Text(grant['recipientAccountId'] as String? ?? '账号'),
              subtitle: Text('有效期至 ${grant['expiresAt'] ?? '已撤销'}'),
              trailing: TextButton(
                onPressed: _workingID != null
                    ? null
                    : () => _remove(
                        '/v1/me/consents/${Uri.encodeComponent(grant['id'] as String)}',
                        grant['id'] as String,
                      ),
                child: const Text('撤销授权'),
              ),
            ),
          const SizedBox(height: 14),
          const Text(
            '取消屏蔽不会恢复之前已撤销的授权。',
            style: TextStyle(color: Color(0xFF747B73), fontSize: 12),
          ),
          const Divider(height: 40),
          ListTile(
            leading: const Icon(Icons.help_outline),
            title: const Text('帮助与举报'),
            subtitle: const Text('提交问题并查看编号与处理状态。'),
            onTap: () => Navigator.push(
              context,
              MaterialPageRoute<void>(
                builder: (_) => SupportPage(
                  authorizationHeader: () => widget.auth.authorizationHeader,
                  apiBaseUrl: _apiBase,
                ),
              ),
            ),
          ),
          const SizedBox(height: 8),
          OutlinedButton.icon(
            onPressed: widget.auth.busy ? null : widget.auth.signOut,
            icon: const Icon(Icons.logout),
            label: const Text('退出登录'),
          ),
        ],
      ),
    );
  }
}
