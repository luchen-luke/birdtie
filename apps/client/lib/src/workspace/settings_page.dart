import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import '../app/birdtie_app_bar.dart';
import '../auth/birdtie_auth_controller.dart';
import '../city/public_city_controller.dart';
import '../content/private_moment_controller.dart';
import '../content/legacy_archive_page.dart';
import '../content/agent_seed_sheet.dart';
import '../content/social_preference_seed_sheet.dart';
import '../legacy/legacy_shell.dart';
import 'agent_debug_panel.dart';
import 'support_page.dart';
import 'privacy_control_page.dart';
import 'human_causal_control.dart';
import 'human_causal_pending_store.dart';
import 'human_causal_operation_controller.dart';
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
import 'voice_input_consent.dart';
import 'notification_destination_router.dart';
import 'system_push_controller.dart';
import 'system_push_settings_page.dart';
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
    this.systemPush,
    this.humanPendingStore,
  });

  final BirdtieAuthController auth;
  final PublicCityController city;
  final PrivateMomentController moments;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final SystemPushController? systemPush;
  final HumanCausalPendingStore? humanPendingStore;

  @override
  State<SettingsPage> createState() => _SettingsPageState();
}

class _SettingsPageState extends State<SettingsPage> {
  static const _defaultApiBase = BirdtieEnvironment.apiBaseUrl;
  late http.Client _client;
  late bool _ownsClient;
  final _entryChanges = ValueNotifier<int>(0);
  final _reportSource = HumanCausalSourceSerial();
  final _archiveSource = HumanCausalSourceSerial();
  void _observeArchiveSource() => _archiveSource.observe((
    widget.auth,
    widget.auth.signedIn,
    widget.auth.accountID,
    widget.organizationWorkspaceID?.call(),
    widget.organizationWorkspaceID,
    widget.workspaceChanges,
    _client,
    _apiBase,
  ));
  void _observeReportSource() => _reportSource.observe((
    widget.auth,
    widget.auth.accountID,
    widget.organizationWorkspaceID?.call(),
    _client,
    _apiBase,
  ));
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
  List<HumanCausalPending> _humanPending = const [];
  String? _humanRecoveryError;
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
    _observeReportSource();
    _observeArchiveSource();
    widget.auth.addListener(_onAuthChanged);
    widget.workspaceChanges?.addListener(_onAuthChanged);
    if (widget.auth.signedIn &&
        widget.organizationWorkspaceID?.call() == null) {
      unawaited(_load());
    }
  }

  void _onAuthChanged() {
    _observeReportSource();
    _observeArchiveSource();
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
    _observeReportSource();
    _observeArchiveSource();
    ++_serial;
    ++_entryEpoch;
    _settingsIdentity = _currentIdentity;
    _entryChanges.value++;
    setState(() {
      _blocks = const [];
      _grants = const [];
      _humanPending = const [];
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
      try {
        final owner = widget.auth.accountID;
        if (humanControlID(owner)) {
          final pending =
              await (widget.humanPendingStore ??
                      SecureHumanCausalPendingStore())
                  .read(_apiBase.replaceFirst(RegExp(r'/$'), ''), owner!);
          if (!current() || serial != _serial) return;
          _humanRecoveryError = null;
          _humanPending = pending
              .where(
                (p) => const {
                  HumanCausalFamily.grantRevoke,
                  HumanCausalFamily.block,
                }.contains(p.selector.family),
              )
              .toList();
        }
      } catch (_) {
        if (current() && serial == _serial) {
          _humanRecoveryError = '本机原操作列表暂不可读，请重新读取。';
        }
      }
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
    final auth = widget.auth, client = _client, base = _apiBase;
    final owner = auth.accountID;
    bool current() =>
        mounted &&
        auth == widget.auth &&
        client == _client &&
        base == _apiBase &&
        owner == auth.accountID &&
        auth.signedIn &&
        widget.organizationWorkspaceID?.call() == null;
    setState(() => _workingID = id);
    try {
      await Navigator.of(context).push<void>(
        MaterialPageRoute(
          builder: (_) => HumanCausalEntryBoundary(
            ownerID: () => auth.accountID,
            authorizationHeader: () => auth.authorizationHeader,
            sourceKey: () => _reportSource.generation,
            sourceCurrent: current,
            identityChanges: Listenable.merge([
              auth,
              widget.workspaceChanges,
              _reportSource,
            ]),
            builder: (_, live) => PrivacyControlPage(
              family: path.contains('/consents/')
                  ? HumanCausalFamily.grantRevoke
                  : HumanCausalFamily.block,
              targetID: id,
              authorizationHeader: () => auth.authorizationHeader,
              ownerID: () => auth.accountID,
              sourceCurrent: live,
              identityChanges: Listenable.merge([
                auth,
                widget.workspaceChanges,
                _reportSource,
              ]),
              client: client,
              apiBaseUrl: base,
              pendingStore: widget.humanPendingStore,
            ),
          ),
        ),
      );
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
        appBar: birdtieAppBar(context, title: const Text('个人资料')),
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

  void _openLegacyArchive() {
    _observeArchiveSource();
    final auth = widget.auth, client = _client, base = _apiBase;
    final owner = auth.accountID, generation = _archiveSource.generation;
    final workspace = widget.organizationWorkspaceID;
    bool current() =>
        mounted &&
        generation == _archiveSource.generation &&
        auth == widget.auth &&
        client == _client &&
        base == _apiBase &&
        auth.signedIn &&
        owner == auth.accountID &&
        widget.organizationWorkspaceID?.call() == null;
    if (!current()) return;
    final changes = Listenable.merge([
      auth,
      widget.workspaceChanges,
      _archiveSource,
    ]);
    String? header() => auth.authorizationHeader;
    String? ownerID() => auth.accountID;
    Object source() => _archiveSource.generation;
    Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => LegacyArchivePage(
          authorizationHeader: header,
          ownerID: ownerID,
          organizationWorkspaceID: workspace,
          identityChanges: changes,
          sourceCurrent: current,
          sourceKey: source,
          client: client,
          apiBaseUrl: base,
        ),
      ),
    );
  }

  void _openProfileVisibility() {
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
      (_) => AgentProfileVisibilityPage(
        auth: auth,
        client: client,
        apiBaseUrl: base,
        workspaceChanges: changes,
        organizationWorkspaceID: workspace,
        current: current,
      ),
    );
  }

  void _openTaskContextPrivacy() {
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
      (_) => TaskContextPrivacyPage(
        auth: auth,
        client: client,
        apiBaseUrl: base,
        workspaceChanges: changes,
        organizationWorkspaceID: workspace,
        current: current,
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
    final auth = widget.auth,
        client = _client,
        base = _apiBase,
        owner = widget.auth.accountID;
    bool current() =>
        mounted &&
        auth == widget.auth &&
        client == _client &&
        base == _apiBase &&
        owner == auth.accountID &&
        auth.signedIn &&
        widget.organizationWorkspaceID?.call() == null;
    Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => HumanCausalEntryBoundary(
          ownerID: () => auth.accountID,
          authorizationHeader: () => auth.authorizationHeader,
          sourceKey: () => _reportSource.generation,
          identityChanges: Listenable.merge([
            auth,
            widget.workspaceChanges,
            _reportSource,
          ]),
          sourceCurrent: current,
          builder: (_, live) => PersonContextsPage(
            auth: auth,
            client: client,
            apiBaseUrl: base,
            current: live,
            identityChanges: Listenable.merge([
              widget.workspaceChanges,
              _reportSource,
            ]),
            pendingStore: widget.humanPendingStore,
          ),
        ),
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
    _archiveSource.dispose();
    _reportSource.dispose();
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

  Widget _voiceInputEntry() => ListTile(
    leading: const Icon(Icons.mic_none_rounded),
    title: const Text('语音输入'),
    onTap: () => unawaited(showVoiceInputInformation(context)),
  );

  Widget _heading(String label) => Padding(
    padding: const EdgeInsets.fromLTRB(16, 24, 16, 8),
    child: Text(label, style: Theme.of(context).textTheme.titleMedium),
  );

  Widget _entry(
    IconData icon,
    String label,
    VoidCallback onTap, {
    String? value,
  }) => ListTile(
    leading: Icon(icon),
    title: Text(label),
    subtitle: value == null ? null : Text(value),
    onTap: widget.organizationWorkspaceID?.call() == null ? onTap : null,
  );

  String _recipientName(Map<String, dynamic> row, String fallback) {
    final label = row['displayName'];
    return label is String && label.trim().isNotEmpty ? label : fallback;
  }

  Widget _helpEntry() => ListTile(
    leading: const Icon(Icons.help_outline),
    title: const Text('帮助与举报'),
    onTap: () {
      final auth = widget.auth, client = _client, base = _apiBase;
      final owner = auth.accountID;
      bool current() =>
          mounted &&
          auth == widget.auth &&
          client == _client &&
          base == _apiBase &&
          owner == auth.accountID &&
          widget.organizationWorkspaceID?.call() == null;
      Navigator.of(context).push<void>(
        MaterialPageRoute(
          builder: (_) => HumanCausalEntryBoundary(
            ownerID: () => auth.accountID,
            authorizationHeader: () => auth.authorizationHeader,
            sourceKey: () => _reportSource.generation,
            sourceCurrent: current,
            identityChanges: Listenable.merge([
              auth,
              widget.workspaceChanges,
              _reportSource,
            ]),
            builder: (_, live) => SupportPage(
              authorizationHeader: () => auth.authorizationHeader,
              ownerID: () => auth.accountID,
              sourceCurrent: live,
              identityChanges: Listenable.merge([
                auth,
                widget.workspaceChanges,
                _reportSource,
              ]),
              client: client,
              apiBaseUrl: base,
              pendingStore: widget.humanPendingStore,
            ),
          ),
        ),
      );
    },
  );

  @override
  Widget build(BuildContext context) {
    if (_apiBase.isEmpty || !widget.auth.signedIn) {
      return ListView(
        padding: const EdgeInsets.symmetric(vertical: 16),
        children: [
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: Text(
              _apiBase.isEmpty ? '设置暂不可用' : '登录后管理设置',
              style: Theme.of(context).textTheme.titleMedium,
            ),
          ),
          if (_apiBase.isNotEmpty)
            ListTile(
              leading: const Icon(Icons.login),
              title: const Text('登录 Birdtie'),
              subtitle: widget.auth.available ? null : const Text('登录服务暂不可用'),
              onTap: widget.auth.available && !widget.auth.busy
                  ? widget.auth.signIn
                  : null,
            ),
          _voiceInputEntry(),
          _helpEntry(),
          if (kDebugMode) _debugEntry(),
        ],
      );
    }
    final now = DateTime.now();
    final activeGrants = _grants.where((grant) {
      if (grant['revokedAt'] != null) return false;
      final expiry = DateTime.tryParse(grant['expiresAt'] as String? ?? '');
      return expiry == null || expiry.isAfter(now);
    }).toList();
    final revocableGrants = _grants
        .where((grant) => grant['revokedAt'] == null)
        .toList();
    final organization = widget.organizationWorkspaceID?.call() != null;
    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(2, 16, 2, 28),
        children: [
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  widget.auth.displayName ?? 'Birdtie 账号',
                  style: Theme.of(context).textTheme.titleLarge,
                ),
                if (widget.auth.loginMethod == 'dev_phone')
                  const Text('本地开发账号 · 手机号尚未验证'),
                if (organization) const Text('组织工作身份 · 切换到个人身份管理个人设置'),
              ],
            ),
          ),
          _heading('账号'),
          _entry(Icons.person_outline, '个人资料', _openProfile),
          _entry(Icons.tune, '昵称、城市与语言', _openSeed),
          if (!organization) ...[
            _heading('本人内容'),
            _entry(Icons.inventory_2_outlined, '旧内容档案', _openLegacyArchive),
          ],
          _heading('通知'),
          _entry(Icons.notifications_outlined, '通知偏好', _openNotificationPolicy),
          if (widget.systemPush != null)
            AnimatedBuilder(
              animation: widget.systemPush!,
              builder: (_, _) => _entry(
                Icons.notifications_active_outlined,
                '系统通知',
                () => _openPersonalRoute(
                  (_) => SystemPushSettingsPage(controller: widget.systemPush!),
                ),
                value: !widget.systemPush!.initialized
                    ? '尚未读取'
                    : widget.systemPush!.uncertain
                    ? '状态待核实'
                    : widget.systemPush!.view == null
                    ? '暂不可用'
                    : widget.systemPush!.enabled
                    ? '已启用'
                    : '未启用',
              ),
            ),
          _entry(Icons.schedule_outlined, '定时汇总', _openNotificationSchedule),
          _heading('隐私'),
          _entry(Icons.visibility_outlined, '资料可见范围', _openProfileVisibility),
          _entry(
            Icons.mark_email_unread_outlined,
            '消息请求',
            () => _openPersonalRoute(
              (_) => MessageRequestPolicyPage(
                auth: widget.auth,
                client: widget.client,
                apiBaseUrl: _apiBase,
                workspaceChanges: widget.workspaceChanges,
                organizationWorkspaceID: widget.organizationWorkspaceID,
              ),
            ),
          ),
          _entry(
            Icons.event_available_outlined,
            '报名可见范围',
            _openParticipationDisclosures,
          ),
          _entry(
            Icons.people_outline,
            '共同信息展示',
            () => _openSocialVisibility(relationship: false),
          ),
          _entry(
            Icons.manage_search_outlined,
            'AI 使用与预算',
            () => _openPersonalRoute(
              (_) => ModelEgressPage(
                auth: widget.auth,
                client: widget.client,
                apiBaseUrl: _apiBase,
                workspaceChanges: widget.workspaceChanges,
                organizationWorkspaceID: widget.organizationWorkspaceID,
              ),
            ),
          ),
          _entry(Icons.privacy_tip_outlined, '本地分析授权', _openEnrichmentPrivacy),
          _entry(Icons.rule_folder_outlined, '任务资料授权', _openTaskContextPrivacy),
          if (!organization)
            ExpansionTile(
              title: const Text('屏蔽与资料访问授权'),
              subtitle: Text(
                _loading
                    ? '正在加载'
                    : _failed
                    ? '暂不可用'
                    : '${_blocks.length} 个屏蔽账号 · ${activeGrants.length} 项有效授权',
              ),
              leading: const Icon(Icons.shield_outlined),
              children: [
                if (_loading) const LinearProgressIndicator(),
                if (_humanRecoveryError != null)
                  TextButton(
                    onPressed: _loading ? null : _load,
                    child: Text(_humanRecoveryError!),
                  ),
                if (_failed)
                  TextButton(
                    onPressed: _load,
                    child: const Text('隐私设置暂不可用，点击重试。'),
                  ),
                if (!_loading &&
                    !_failed &&
                    _blocks.isEmpty &&
                    revocableGrants.isEmpty)
                  const Padding(
                    padding: EdgeInsets.all(16),
                    child: Text('暂无屏蔽账号或访问授权'),
                  ),
                if (_blocks.isNotEmpty) _heading('已屏蔽账号'),
                for (final block in _blocks)
                  ListTile(
                    title: Text(_recipientName(block, '账号资料暂不可用')),
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
                if (revocableGrants.isNotEmpty) _heading('资料访问授权'),
                for (final grant in revocableGrants)
                  ListTile(
                    title: Text(_recipientName(grant, '账号资料暂不可用')),
                    subtitle: Text('有效至 ${grant['expiresAt'] ?? '期限未提供'}'),
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
                for (final p in _humanPending)
                  ListTile(
                    key: Key('privacy-recovery-${p.selector.family.wire}'),
                    title: const Text('核实本人原隐私操作'),
                    subtitle: Text(
                      '${p.selector.family.label} · 只核实原回执，不授予新权限',
                    ),
                    onTap: () => _remove(
                      p.selector.family == HumanCausalFamily.grantRevoke
                          ? '/v1/me/consents/${p.selector.targetID}'
                          : '/v1/me/blocks/${p.selector.targetID}',
                      p.selector.targetID,
                    ),
                  ),
                if (_blocks.isNotEmpty)
                  const Padding(
                    padding: EdgeInsets.fromLTRB(16, 8, 16, 16),
                    child: Text('取消屏蔽不会恢复之前已撤销的授权。'),
                  ),
              ],
            ),
          _heading('智能体与偏好'),
          _entry(
            Icons.person_search_outlined,
            '我的智能体',
            () => _openPersonalRoute(
              (_) => AgentProfilePage(
                auth: widget.auth,
                client: widget.client,
                apiBaseUrl: _apiBase,
                workspaceChanges: widget.workspaceChanges,
                organizationWorkspaceID: widget.organizationWorkspaceID,
              ),
            ),
          ),
          _entry(Icons.edit_note, '管理我的记忆', _openMemoryCorrection),
          _entry(Icons.fact_check_outlined, '待确认的记忆', _openMemoryCandidates),
          _entry(Icons.explore_outlined, '城市与生活资料', _openPersonContexts),
          _entry(Icons.people_outline, '社交偏好', _openSocialPreferences),
          _entry(
            Icons.psychology_outlined,
            '互动记录使用范围',
            () => _openSocialVisibility(relationship: true),
          ),
          _entry(
            Icons.person_add_alt_1_outlined,
            '引荐与社交授权',
            _openIntroductions,
          ),
          _entry(Icons.groups_outlined, '社群兴趣', _openCommunityInterests),
          ExpansionTile(
            title: const Text('发现与近况'),
            leading: const Icon(Icons.travel_explore),
            children: [
              _entry(Icons.people_outline, '好友近况', _openSocialNow),
              _entry(
                Icons.event_available_outlined,
                '为你找到的活动',
                () => _openPersonalRoute(
                  (_) => OpportunityPage(
                    auth: widget.auth,
                    apiBaseUrl: _apiBase,
                    client: widget.client,
                  ),
                ),
              ),
              _entry(
                Icons.person_add_alt_1_outlined,
                '找新朋友',
                () => _openPersonalRoute(
                  (_) => NewPeoplePage(
                    auth: widget.auth,
                    apiBaseUrl: _apiBase,
                    client: widget.client,
                    workspaceChanges: widget.workspaceChanges,
                    organizationWorkspaceID: widget.organizationWorkspaceID,
                  ),
                ),
              ),
              _entry(
                Icons.edit_note_outlined,
                '完善我的选择',
                () => _openSeed(progressive: true),
              ),
            ],
          ),
          _heading('帮助'),
          _voiceInputEntry(),
          _helpEntry(),
          if (kDebugMode) _debugEntry(),
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 20, 16, 0),
            child: OutlinedButton.icon(
              onPressed: widget.auth.busy ? null : widget.auth.signOut,
              icon: const Icon(Icons.logout),
              label: const Text('退出登录'),
            ),
          ),
        ],
      ),
    );
  }
}
