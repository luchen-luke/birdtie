import 'dart:async';
import 'dart:convert';

import 'business_console_page.dart';

import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;

import '../auth/birdtie_auth_controller.dart';
import 'agent_relationship_page.dart';
import 'new_people_page.dart';
import 'opportunity_page.dart';
import 'social_now_page.dart';
import '../city/public_city_controller.dart';
import '../content/private_moment_controller.dart';
import '../content/agent_seed_controller.dart';
import '../content/agent_seed_sheet.dart';
import '../legacy/legacy_shell.dart';
import 'agent_composer.dart';
import 'now_private_moment_image_page.dart';
import 'activity_plans.dart';
import 'activity_participations.dart';
import 'activity_detail_sheet.dart';
import 'activity_analytics.dart';
import 'connections.dart';
import 'chat_entity_router.dart';
import 'agent_result_sheet.dart';
import 'agent_workspace_controller.dart';
import 'agent_request_failure.dart';
import 'entity_peek_card.dart';
import 'entity_action_contract.dart';
import 'entity_action_dispatcher.dart';
import 'inbox.dart';
import 'community_page.dart';
import 'community_api.dart';
import 'public_person_page.dart';
import 'map_canvas.dart';
import 'map_entities.dart';
import 'agent_entity_result_router.dart';
import 'map_layers_api.dart';
import 'map_layers_controller.dart';
import 'map_layer_controls.dart';
import 'active_social_intent_card.dart';
import 'active_social_intent_page.dart';
import 'place_detail_sheet.dart';
import 'now_discovery_controller.dart';
import 'remote_agent_task_source.dart';
import 'now_context_query_api.dart';
import 'now_context_query_controller.dart';
import 'now_context_selection_api.dart';
import 'now_context_selection_controller.dart';
import 'online_social_opportunity_page.dart';
import 'person_contexts_page.dart';
import 'saved_items.dart';
import 'settings_page.dart';
import 'social_intent_drafts.dart';
import 'organization_workspaces.dart';
import 'notification_destination_router.dart';
import 'organization_membership_pages.dart';
import 'organization_console.dart';
import 'public_organization_page.dart';
import 'public_business_page.dart';
import 'sidebar.dart';
import 'top_controls.dart';
import '../config/birdtie_environment.dart';

class MapWorkspace extends StatefulWidget {
  const MapWorkspace({
    super.key,
    required this.city,
    required this.auth,
    required this.moments,
    this.agentTaskSource,
    this.connectionSource,
    this.seedClient,
    this.seedApiBaseUrl,
    this.placeDetailClient,
    this.placeDetailApiBaseUrl,
  });
  final PublicCityController city;
  final BirdtieAuthController auth;
  final PrivateMomentController moments;
  final AgentTaskSource? agentTaskSource;
  final ConnectionSource? connectionSource;
  final http.Client? seedClient;
  final String? seedApiBaseUrl;
  final http.Client? placeDetailClient;
  final String? placeDetailApiBaseUrl;

  @override
  State<MapWorkspace> createState() => _MapWorkspaceState();
}

class _MapWorkspaceState extends State<MapWorkspace>
    with WidgetsBindingObserver {
  final _scaffold = GlobalKey<ScaffoldState>();
  final _composerKey = GlobalKey<AgentComposerState>();
  final MapViewportState _mapState = MapViewportState();
  late final AgentWorkspaceController _workspace;
  late NowContextQueryController _online;
  NowContextChoice? _viewChoice;
  NowContextChoice? _submittedChoice;
  int _viewSerial = 0;
  int _onlineIntentSerial = 0;
  String? get _queryCityID => _submittedChoice?.queryRoute == 'CITY'
      ? _submittedChoice!.cityID
      : widget.city.selectedCity?.id;
  String? get _queryOnlineID => _submittedChoice?.queryRoute == 'ONLINE'
      ? _submittedChoice!.contextID
      : _online.selected?.id;
  late final SavedController _saved;
  late final ActivityPlansController _plans;
  late final ActivityParticipationController _participations;
  late final ConnectionSource _connections;
  late final OrganizationWorkspaceController _organizations;
  late final NowDiscoveryController _discovery;
  late final MapCanvas _mapCanvas;
  late final MapLayersController _layers;
  late Listenable _activeIntentChanges;
  late final String? Function() _activeIntentWorkspace;
  String? _lastCityID;
  String? _lastAuthorization;
  int _authSerial = 0;
  int _workspaceSerial = 0;
  int _replyContextEpoch = 0;
  (String?, String?) _workspaceIdentity = (null, null);
  String? _seedCheckedAuthorization;
  final _seedEntryChanges = ValueNotifier<int>(0);
  final _placeEntryChanges = ValueNotifier<int>(0);
  late (String?, String?, String?) _seedIdentity;
  int _seedEntryEpoch = 0;
  int _personalSourceSerial = 0;
  bool _clearingAccount = false;
  BuildContext? _confirmationContext;
  double _composerHeight = 56;
  double? _topControlsHeight;
  double _ornamentHeight = 48;
  final _ornamentTop = ValueNotifier<double?>(null);
  bool _cityPickerOpen = false;
  bool _selectingCity = false;
  bool _loadingCityCatalog = false;
  bool Function()? _pendingCityCurrent;
  final Set<String> _recordedImpressions = {};

  void _recordImpression(PublicActivity activity, String source) {
    if (activity.organizationID == null) return;
    if (_recordedImpressions.add('$source:${activity.id}')) {
      unawaited(recordActivityView(activity.id, 'impression', source));
    }
  }

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _online = NowContextQueryController(
      api: NowContextQueryApi(
        authorizationHeader: () => widget.auth.authorizationHeader,
        ownerID: () => widget.auth.accountID,
        organizationWorkspaceID: () => _organizations.active?.id,
        client: widget.seedClient,
        apiBaseUrl: widget.seedApiBaseUrl,
      ),
    );
    _workspace = AgentWorkspaceController(
      replyRefreshAllowed: () =>
          mounted &&
          ModalRoute.of(context)?.isCurrent == true &&
          WidgetsBinding.instance.lifecycleState == AppLifecycleState.resumed,
      source:
          widget.agentTaskSource ??
          RemoteAgentTaskSource(
            cityID: () => _queryCityID,
            onlineContextID: () => _queryOnlineID,
            onlineApiFactory: () => _online.api,
            authorizationHeader: () => widget.auth.authorizationHeader,
            organizationWorkspaceID: () => _organizations.active?.id,
            publicEvidenceOwnerID: () => widget.auth.accountID,
            publicEvidenceEpoch: () =>
                (_seedEntryEpoch, _workspaceSerial, _replyContextEpoch),
          ),
    );
    _saved = SavedController(
      authorizationHeader: () => widget.auth.authorizationHeader,
    );
    _plans = ActivityPlansController(
      authorizationHeader: () => widget.auth.authorizationHeader,
    );
    _participations = ActivityParticipationController(
      authorizationHeader: () => widget.auth.authorizationHeader,
    );
    _connections =
        widget.connectionSource ??
        ConnectionSource(
          authorizationHeader: () => widget.auth.authorizationHeader,
        );
    _organizations = OrganizationWorkspaceController(
      authorizationHeader: () => widget.auth.authorizationHeader,
    );
    _activeIntentChanges = Listenable.merge([
      widget.auth,
      _organizations,
      _seedEntryChanges,
    ]);
    _activeIntentWorkspace = () => _organizations.active?.id;
    _discovery = NowDiscoveryController(
      authorizationHeader: () => widget.auth.authorizationHeader,
    );
    _layers = MapLayersController(
      api: MapLayersApi(
        client: widget.seedClient,
        apiBaseUrl: widget.seedApiBaseUrl,
      ),
      authorizationHeader: () => widget.auth.authorizationHeader,
      organizationWorkspaceID: () => _organizations.active?.id,
      refreshAllowed: () =>
          mounted &&
          ModalRoute.of(context)?.isCurrent == true &&
          WidgetsBinding.instance.lifecycleState == AppLifecycleState.resumed,
    );
    _mapCanvas = MapCanvas(
      city: widget.city,
      workspace: _workspace,
      mapState: _mapState,
      discovery: _discovery,
      layers: _layers,
      onInitialViewport: _initialViewport,
      onMapUnavailable: _mapUnavailable,
      onChooseCity: () => unawaited(_chooseCity()),
      ornamentTop: _ornamentTop,
      onOrnamentHeightChanged: (height) {
        final measured = height.clamp(48.0, double.infinity);
        if (mounted && (_ornamentHeight - measured).abs() > .5) {
          setState(() => _ornamentHeight = measured);
        }
      },
    );
    _lastCityID = widget.city.selectedCity?.id;
    widget.city.addListener(_onCityChange);
    _organizations.addListener(_refreshWorkspaceTasks);
    widget.auth.addListener(_onAuthChange);
    _seedIdentity = _currentSeedIdentity;
    widget.auth.addListener(_onSeedIdentityChange);
    _organizations.addListener(_onSeedIdentityChange);
    _lastAuthorization = widget.auth.authorizationHeader;
    if (widget.auth.signedIn) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) unawaited(_checkSeed());
      });
      unawaited(_workspace.loadRecent());
      unawaited(_saved.load());
      unawaited(_plans.load());
      unawaited(_participations.load());
      unawaited(_organizations.load());
    }
  }

  (String?, String?, String?) get _currentSeedIdentity => (
    widget.auth.authorizationHeader,
    widget.auth.accountID,
    _organizations.active?.id,
  );

  void _retireSeedEntry() {
    _workspace.retireReplyRefresh();
    ++_seedEntryEpoch;
    _pendingCityCurrent = null;
    _seedIdentity = _currentSeedIdentity;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) {
          _composerKey.currentState?.clearDraft();
          _seedEntryChanges.value++;
        }
      });
    } else {
      _composerKey.currentState?.clearDraft();
      _seedEntryChanges.value++;
    }
  }

  void _onSeedIdentityChange() {
    if (_seedIdentity != _currentSeedIdentity) {
      // Person and workspace changes retire the projection even if an opaque
      // session string happens to stay equal. Returning to A cannot revive it.
      _layers.retire();
      _retireSeedEntry();
    }
  }

  @override
  void didUpdateWidget(MapWorkspace old) {
    super.didUpdateWidget(old);
    if (old.moments != widget.moments) {
      // Only retire child readers of the replaced personal source. Preserve
      // the independent Now task, map selection and unsent composer draft.
      ++_personalSourceSerial;
      // Source validity changes immediately, including A -> B -> A. Publish
      // the visual update after this build so mounted child readers may retire
      // without notifying their AnimatedBuilders during a different route's build.
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) _seedEntryChanges.value++;
      });
    }
    final authChanged = old.auth != widget.auth;
    if (authChanged ||
        old.placeDetailClient != widget.placeDetailClient ||
        old.placeDetailApiBaseUrl != widget.placeDetailApiBaseUrl) {
      _placeEntryChanges.value++;
    }
    if (authChanged ||
        old.seedClient != widget.seedClient ||
        old.seedApiBaseUrl != widget.seedApiBaseUrl) {
      ++_viewSerial;
      _viewChoice = null;
      _submittedChoice = null;
      _online.dispose();
      _layers.retire(
        api: MapLayersApi(
          client: widget.seedClient,
          apiBaseUrl: widget.seedApiBaseUrl,
        ),
      );
      _online = NowContextQueryController(
        api: NowContextQueryApi(
          authorizationHeader: () => widget.auth.authorizationHeader,
          ownerID: () => widget.auth.accountID,
          organizationWorkspaceID: () => _organizations.active?.id,
          client: widget.seedClient,
          apiBaseUrl: widget.seedApiBaseUrl,
        ),
      );
      ++_workspaceSerial;
      _workspace.retireAccountContext();
    }
    if (authChanged) {
      _activeIntentChanges = Listenable.merge([
        widget.auth,
        _organizations,
        _seedEntryChanges,
      ]);
      old.auth.removeListener(_onAuthChange);
      old.auth.removeListener(_onSeedIdentityChange);
      widget.auth.addListener(_onAuthChange);
      widget.auth.addListener(_onSeedIdentityChange);
      ++_authSerial;
      _seedCheckedAuthorization = null;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted) return;
        _lastAuthorization = null;
        _onAuthChange();
      });
    }
    if (authChanged ||
        old.seedClient != widget.seedClient ||
        old.seedApiBaseUrl != widget.seedApiBaseUrl) {
      _seedCheckedAuthorization = null;
      _retireSeedEntry();
    }
  }

  Future<void> _openSeedRoute({
    bool onlyMissing = false,
    bool progressive = false,
  }) async {
    final auth = widget.auth, client = widget.seedClient;
    final base = widget.seedApiBaseUrl, identity = _currentSeedIdentity;
    final epoch = _seedEntryEpoch;
    final changes = Listenable.merge([_seedEntryChanges, auth, _organizations]);
    bool current() =>
        mounted &&
        epoch == _seedEntryEpoch &&
        auth == widget.auth &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl &&
        identity == _currentSeedIdentity;
    if (!current() || identity.$1 == null || identity.$3 != null) return;
    _pauseComposer();
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => NotificationDestinationBoundary(
          identityChanges: changes,
          current: current,
          builder: (_) => AgentSeedSheet(
            auth: auth,
            client: client,
            apiBaseUrl: base,
            workspaceChanges: _organizations,
            organizationWorkspaceID: () => _organizations.active?.id,
            onlyMissing: onlyMissing,
            progressive: progressive,
          ),
        ),
      ),
    );
  }

  Future<void> _openSocialNowRoute() async {
    final auth = widget.auth, client = widget.seedClient;
    final base = widget.seedApiBaseUrl, identity = _currentSeedIdentity;
    final epoch = _seedEntryEpoch;
    final changes = Listenable.merge([_seedEntryChanges, auth, _organizations]);
    bool current() =>
        mounted &&
        epoch == _seedEntryEpoch &&
        auth == widget.auth &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl &&
        identity == _currentSeedIdentity;
    if (!current() || identity.$1 == null || identity.$3 != null) return;
    _pauseComposer();
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => NotificationDestinationBoundary(
          identityChanges: changes,
          current: current,
          builder: (_) => SocialNowPage(
            auth: auth,
            client: client,
            apiBaseUrl: base,
            workspaceChanges: changes,
            organizationWorkspaceID: () => _organizations.active?.id,
            current: current,
          ),
        ),
      ),
    );
  }

  void _onAuthChange() {
    final token = widget.auth.authorizationHeader;
    if (_lastAuthorization == token) return;
    _lastAuthorization = token;
    _seedCheckedAuthorization = null;
    ++_authSerial;
    _layers.retire();
    _closeConfirmation();
    // This invalidates pending Agent turns synchronously. MapCanvas and its
    // viewport remain the same objects; only the previous account is cleared.
    _clearingAccount = true;
    _clearAccountWorkspace();
    _saved.clear();
    _plans.clear();
    _participations.clear();
    _organizations.clear();
    _clearingAccount = false;
    _discovery.clear();
    final cityID = widget.city.selectedCity?.id;
    final bounds = _mapState.viewportBounds;
    if (cityID != null && bounds != null) {
      unawaited(_discovery.load(cityID, bounds));
      unawaited(_layers.load(cityID, bounds));
    } else if (cityID != null && _mapState.mapError != null) {
      unawaited(_discovery.loadCityActivities(cityID));
    }
    if (widget.auth.signedIn) {
      unawaited(_checkSeed());
      unawaited(_workspace.loadRecent());
      unawaited(_saved.load());
      unawaited(_plans.load());
      unawaited(_participations.load());
      unawaited(_organizations.load());
    }
  }

  bool _currentAccount(int serial, String? token) =>
      mounted &&
      serial == _authSerial &&
      widget.auth.authorizationHeader == token;

  Future<void> _checkSeed() async {
    final auth = widget.auth, client = widget.seedClient;
    final base = widget.seedApiBaseUrl, identity = _currentSeedIdentity;
    final epoch = _seedEntryEpoch;
    final token = auth.authorizationHeader;
    if (token == null ||
        token == _seedCheckedAuthorization ||
        _organizations.active != null) {
      return;
    }
    _seedCheckedAuthorization = token;
    final serial = _authSerial;
    bool current() =>
        _currentAccount(serial, token) &&
        epoch == _seedEntryEpoch &&
        auth == widget.auth &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl &&
        identity == _currentSeedIdentity;
    final controller = AgentSeedController(
      authorizationHeader: () => current() ? auth.authorizationHeader : null,
      accountID: () => current() ? auth.accountID : null,
      organizationWorkspaceID: () => _organizations.active?.id,
      client: client,
      apiBaseUrl: base,
    );
    try {
      if (!await controller.load() ||
          !mounted ||
          !current() ||
          _organizations.active != null ||
          controller.record?.needsPrompt != true) {
        return;
      }
      await _openSeedRoute(onlyMissing: true);
    } finally {
      controller.dispose();
    }
  }

  void _closeConfirmation() {
    final dialog = _confirmationContext;
    _confirmationContext = null;
    if (dialog == null || !dialog.mounted) return;
    final route = ModalRoute.of(dialog);
    if (route?.isCurrent == true) {
      Navigator.of(dialog).pop();
    } else if (route != null) {
      Navigator.of(dialog).removeRoute(route);
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _ornamentTop.dispose();
    ++_authSerial;
    ++_seedEntryEpoch;
    _seedEntryChanges.value++;
    widget.auth.removeListener(_onSeedIdentityChange);
    _organizations.removeListener(_onSeedIdentityChange);
    _seedEntryChanges.dispose();
    _placeEntryChanges.value++;
    _placeEntryChanges.dispose();
    widget.city.removeListener(_onCityChange);
    widget.auth.removeListener(_onAuthChange);
    _organizations.removeListener(_refreshWorkspaceTasks);
    _online.dispose();
    _workspace.dispose();
    _saved.dispose();
    _plans.dispose();
    _participations.dispose();
    _connections.dispose();
    _organizations.dispose();
    _discovery.dispose();
    _layers.dispose();
    _mapState.dispose();
    super.dispose();
  }

  void _refreshWorkspaceTasks() {
    final current = (
      _organizations.active?.id,
      _organizations.active?.role.toLowerCase(),
    );
    if (current == _workspaceIdentity) return;
    _workspaceIdentity = current;
    ++_workspaceSerial;
    _layers.retire();
    if (_clearingAccount || !widget.auth.signedIn) return;
    _clearAccountWorkspace();
    unawaited(_workspace.loadRecent());
  }

  void _clearAccountWorkspace() {
    ++_viewSerial;
    _pendingCityCurrent = null;
    _viewChoice = null;
    _submittedChoice = null;
    final selection = _workspace.selectedEntityId;
    final publicSelection =
        selection != null &&
        widget.city.places.any(
          (place) =>
              selection == 'place:${place.id}' && place.location.hasPublicPoint,
        );
    _online.retire();
    _workspace.clearAccountContext();
    if (publicSelection) _workspace.selectEntity(selection);
  }

  void _onCityChange() {
    final cityID = widget.city.selectedCity?.id;
    if (cityID == _lastCityID) return;
    ++_replyContextEpoch;
    _workspace.retireReplyRefresh();
    if (!_selectingCity && !_loadingCityCatalog) {
      ++_viewSerial;
      _pendingCityCurrent = null;
      _workspace.retirePendingQueryForViewChange();
    }
    _lastCityID = cityID;
    _layers.retire();
    _mapState.reset();
    _discovery.clear();
  }

  void _pauseComposer() => _composerKey.currentState?.pauseEditing();

  Future<void> _openNowTools() async {
    _pauseComposer();
    final auth = widget.auth, client = widget.seedClient;
    final base = widget.seedApiBaseUrl, identity = _currentSeedIdentity;
    final epoch = _seedEntryEpoch, viewSerial = _viewSerial;
    final cityID = widget.city.selectedCity?.id,
        onlineID = _online.selected?.id;
    bool current() =>
        mounted &&
        auth == widget.auth &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl &&
        identity == _currentSeedIdentity &&
        epoch == _seedEntryEpoch &&
        viewSerial == _viewSerial &&
        cityID == widget.city.selectedCity?.id &&
        onlineID == _online.selected?.id;
    final selected = await showModalBottomSheet<Future<void> Function()>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      builder: (sheetContext) => ConstrainedBox(
        constraints: BoxConstraints(
          maxHeight: MediaQuery.sizeOf(sheetContext).height * .7,
        ),
        child: NotificationDestinationBoundary(
          identityChanges: Listenable.merge([
            _activeIntentChanges,
            widget.city,
            _online,
          ]),
          current: current,
          builder: (_) => SafeArea(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                key: const Key('now-tools-menu'),
                children: [
                  Row(
                    children: [
                      Expanded(
                        child: Text(
                          '更多工具',
                          style: Theme.of(sheetContext).textTheme.titleLarge,
                        ),
                      ),
                      IconButton(
                        tooltip: '关闭更多工具',
                        constraints: const BoxConstraints(
                          minWidth: 48,
                          minHeight: 48,
                        ),
                        onPressed: () => Navigator.pop(sheetContext),
                        icon: const Icon(Icons.close),
                      ),
                    ],
                  ),
                  Expanded(
                    child: ListView(
                      children: [
                        for (final tool in [
                          (
                            Icons.layers_outlined,
                            '地图图层',
                            '选择可查看的地图内容',
                            '地图图层',
                            _openMapLayers,
                          ),
                          (
                            Icons.flag_outlined,
                            '我的社交意图',
                            '查看和管理本人意图',
                            '我的社交意图',
                            _openActiveIntentCard,
                          ),
                          (
                            Icons.edit_note_outlined,
                            '意图草稿',
                            '私人草稿 · 保存不等于公开发布',
                            '打开意图草稿',
                            () => _openIntentDraft(),
                          ),
                        ])
                          Tooltip(
                            message: tool.$4,
                            child: ListTile(
                              minTileHeight: 48,
                              contentPadding: const EdgeInsets.symmetric(
                                horizontal: 8,
                              ),
                              leading: Icon(
                                tool.$1,
                                color: Theme.of(
                                  sheetContext,
                                ).colorScheme.onSurface,
                              ),
                              title: Text(tool.$2),
                              subtitle: Text(tool.$3),
                              onTap: () {
                                if (current()) {
                                  Navigator.pop(sheetContext, tool.$5);
                                }
                              },
                            ),
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
    );
    if (current() && selected != null) await selected();
  }

  Future<void> _openIntentDraft({AgentTask? task}) async {
    _pauseComposer();
    if (task != null && !identical(task, _workspace.task)) return;
    final auth = widget.auth, client = widget.seedClient;
    final base = widget.seedApiBaseUrl, identity = _currentSeedIdentity;
    final epoch = _seedEntryEpoch;
    bool current() =>
        mounted &&
        auth == widget.auth &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl &&
        identity == _currentSeedIdentity &&
        epoch == _seedEntryEpoch &&
        _organizations.active == null;
    if (_organizations.active != null) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('请切换到个人身份后管理私人意图草稿。')));
      return;
    }
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (routeContext) => NotificationDestinationBoundary(
          identityChanges: _activeIntentChanges,
          current: current,
          builder: (_) => SocialIntentDraftPage(
            authorizationHeader: () => auth.authorizationHeader,
            authorizationChanges: auth,
            ownerID: () => auth.accountID,
            workspaceID: _activeIntentWorkspace,
            workspaceChanges: _activeIntentChanges,
            current: current,
            client: client,
            apiBaseUrl: base,
            agentTaskId: task?.id,
            suggestedTitle: task?.query,
            suggestedCategory: task?.filters['category'],
            contextNote: task == null
                ? '先保存私人草稿。参与方式、时间和区域由你确认；保存不等于公开发布。'
                : '来自本次已完成查询。请检查建议内容；保存草稿不会发布或报名。',
            onClose: () => Navigator.pop(routeContext),
            onManageIntent: (id) {
              if (!current()) return;
              unawaited(
                Navigator.of(routeContext).push<void>(
                  MaterialPageRoute(
                    builder: (_) => NotificationDestinationBoundary(
                      identityChanges: _activeIntentChanges,
                      current: current,
                      builder: (_) => ActiveSocialIntentPage(
                        auth: auth,
                        client: client,
                        apiBaseUrl: base,
                        workspaceChanges: _activeIntentChanges,
                        organizationWorkspaceID: _activeIntentWorkspace,
                        initialIntentID: id,
                      ),
                    ),
                  ),
                ),
              );
            },
          ),
        ),
      ),
    );
  }

  void _selectPublicCity(String id) {
    final pending = _workspace.pendingScopeQuery;
    final resume = pending != null && _pendingCityCurrent?.call() == true;
    if (!resume && widget.city.selectedCity?.id == id) return;
    if (!resume) {
      ++_viewSerial;
      _pendingCityCurrent = null;
      _workspace.newTask();
    }
    _viewChoice = null;
    _submittedChoice = null;
    _online.select(null);
    _workspace.queryContextType = 'CITY';
    _selectingCity = true;
    try {
      widget.city.selectCity(id);
    } finally {
      _selectingCity = false;
    }
    // Consume before awaiting, so repeated taps and retired ABA cannot replay.
    if (resume && widget.city.selectedCity?.id == id) {
      _pendingCityCurrent = null;
      unawaited(
        _workspace.resumeCity(id, widget.city.activities, widget.city.places),
      );
    }
  }

  Future<void> _chooseCity() async {
    if (_cityPickerOpen) return;
    _pauseComposer();
    final identity = _currentSeedIdentity, epoch = _seedEntryEpoch;
    final viewSerial = _viewSerial, auth = widget.auth;
    final client = widget.seedClient, base = widget.seedApiBaseUrl;
    bool current() =>
        mounted &&
        auth == widget.auth &&
        identity == _currentSeedIdentity &&
        epoch == _seedEntryEpoch &&
        viewSerial == _viewSerial &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl;
    Future<void> reload() async {
      if (!current() || _loadingCityCatalog) return;
      _loadingCityCatalog = true;
      try {
        await widget.city.loadCities();
      } finally {
        _loadingCityCatalog = false;
      }
    }

    Object? selected;
    _cityPickerOpen = true;
    if (widget.city.cities.isEmpty) unawaited(reload());
    try {
      do {
        if (!mounted || !current()) return;
        selected = await showModalBottomSheet<Object>(
          context: context,
          showDragHandle: true,
          isScrollControlled: true,
          useSafeArea: true,
          builder: (sheetContext) => _NowScopePicker(
            city: widget.city,
            auth: auth,
            client: client,
            apiBaseUrl: base,
            identityChanges: _activeIntentChanges,
            organizationWorkspaceID: _activeIntentWorkspace,
            current: current,
            reloadCities: reload,
            onClose: (value) => Navigator.pop(sheetContext, value),
          ),
        );
        if (!mounted || !current()) return;
        if (selected != _NowScopeAction.manage) break;
        await Navigator.of(context).push<void>(
          MaterialPageRoute(
            builder: (_) => NotificationDestinationBoundary(
              identityChanges: _activeIntentChanges,
              current: current,
              builder: (_) => PersonContextsPage(
                auth: auth,
                client: client,
                apiBaseUrl: base,
                current: current,
              ),
            ),
          ),
        );
      } while (current());
    } finally {
      _cityPickerOpen = false;
    }
    if (!mounted || !current()) return;
    if (selected is String) {
      _selectPublicCity(selected);
    } else if (selected is NowContextChoice) {
      final choice = selected;
      ++_viewSerial;
      _workspace.retirePendingQueryForViewChange();
      setState(() => _viewChoice = choice);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            choice.queryRoute == 'UNAVAILABLE'
                ? '已选择${choice.label}；该范围暂不支持查询。原任务和地图已保留。'
                : '已选择${choice.label}；下次提交时查询此范围。原任务和地图已保留。',
          ),
        ),
      );
    } else if (selected == _NowScopeAction.online) {
      await _openOnlineOpportunities();
    }
  }

  void _initialViewport(MapBounds bounds) {
    _mapState.initializeViewport(bounds);
    final cityID = widget.city.selectedCity?.id;
    if (cityID != null) {
      unawaited(_discovery.load(cityID, bounds));
      unawaited(_layers.load(cityID, bounds));
    }
  }

  void _mapUnavailable(String message) {
    if (_mapState.mapError == message) return;
    _mapState.mapUnavailable(message);
    final cityID = widget.city.selectedCity?.id;
    if (cityID != null) unawaited(_discovery.loadCityActivities(cityID));
  }

  Future<void> _reopenTask(AgentTask task) async {
    final serial = ++_viewSerial;
    _viewChoice = null;
    _submittedChoice = null;
    final epoch = _authSerial,
        workspaceEpoch = _workspaceSerial,
        auth = widget.auth,
        token = widget.auth.authorizationHeader,
        owner = widget.auth.accountID,
        client = widget.seedClient,
        base = widget.seedApiBaseUrl;
    bool current() =>
        mounted &&
        serial == _viewSerial &&
        epoch == _authSerial &&
        workspaceEpoch == _workspaceSerial &&
        auth == widget.auth &&
        token == widget.auth.authorizationHeader &&
        owner == widget.auth.accountID &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl;
    if (task.contextType == 'ONLINE') {
      try {
        final response = await _online.api.restore(task.id);
        if (!current()) return;
        _online.restoreContext(response.context);
        if (!current()) return;
      } catch (error) {
        if (mounted && current()) {
          final message = error is AgentRequestFailure
              ? [error.userMessage, ?error.recoveryMessage].join('\n')
              : '此线上任务的情境或来源已不可用。';
          ScaffoldMessenger.of(
            context,
          ).showSnackBar(SnackBar(content: Text(message)));
        }
        return;
      }
    } else {
      _online.select(null);
      if (task.cityID != null) widget.city.selectCity(task.cityID!);
    }
    await _workspace.reopen(task, widget.city.activities, widget.city.places);
  }

  Future<void> _openOnlineIntent(String id) async {
    final token = widget.auth.authorizationHeader,
        epoch = _authSerial,
        workspaceEpoch = _workspaceSerial,
        viewSerial = _viewSerial,
        requestSerial = ++_onlineIntentSerial,
        owner = widget.auth.accountID,
        client = widget.seedClient,
        base = widget.seedApiBaseUrl;
    final auth = widget.auth;
    bool current() =>
        mounted &&
        auth == widget.auth &&
        token == widget.auth.authorizationHeader &&
        owner == widget.auth.accountID &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl &&
        viewSerial == _viewSerial &&
        requestSerial == _onlineIntentSerial &&
        epoch == _authSerial &&
        workspaceEpoch == _workspaceSerial &&
        _organizations.active == null;
    try {
      final response = await _online.api.intent(id);
      if (!mounted || !current()) return;
      await Navigator.of(context).push<void>(
        MaterialPageRoute(
          builder: (_) => NotificationDestinationBoundary(
            identityChanges: Listenable.merge([
              auth,
              _organizations,
              _seedEntryChanges,
            ]),
            current: current,
            builder: (_) => _NowOnlineDetail(response: response),
          ),
        ),
      );
    } catch (error) {
      if (mounted && current()) {
        final message = error is AgentRequestFailure
            ? [error.userMessage, ?error.recoveryMessage].join('\n')
            : '此意图已不可用或权限发生变化，请重新查询。';
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(message)));
      }
    }
  }

  void _submit(String query) {
    unawaited(_submitInSelectedView(query));
  }

  Future<void> _submitInSelectedView(String query) async {
    _pendingCityCurrent = null;
    final choice = _viewChoice,
        serial = ++_viewSerial,
        identity = _currentSeedIdentity,
        epoch = _seedEntryEpoch,
        token = widget.auth.authorizationHeader,
        owner = widget.auth.accountID;
    bool current() =>
        mounted &&
        serial == _viewSerial &&
        identity == _currentSeedIdentity &&
        epoch == _seedEntryEpoch &&
        token == widget.auth.authorizationHeader &&
        owner == widget.auth.accountID;
    if (choice != null) {
      if (choice.queryRoute == 'UNAVAILABLE' ||
          token == null ||
          owner == null ||
          _organizations.active != null) {
        if (mounted && current()) {
          _composerKey.currentState?.fillDraft(query, requestFocus: false);
          ScaffoldMessenger.of(context).showSnackBar(
            const SnackBar(content: Text('该查看范围当前不支持查询；请选择城市或本人线上情境。')),
          );
        }
        return;
      }
      final api = NowContextSelectionAPI(
        client: widget.seedClient,
        apiBaseUrl: widget.seedApiBaseUrl,
      );
      try {
        final options = await api.options(token, owner);
        if (!current()) return;
        final candidates = options.items.where((o) => o.same(choice.option));
        if (candidates.length != 1) throw const FormatException('来源已变化');
        final refreshed = await api.resolve(
          token,
          owner,
          options,
          candidates.single,
        );
        if (!current()) return;
        if (refreshed.queryRoute == 'CITY') {
          final cityID = refreshed.cityID;
          if (!widget.city.cities.any((city) => city.id == cityID)) {
            throw const FormatException('城市目录已变化');
          }
          // The receipt was revalidated for this submit. Keep the map on that
          // canonical city without retiring the query's existing authority.
          // Choosing a view in the picker alone never changes the map.
          _selectingCity = true;
          try {
            widget.city.selectCity(cityID);
          } finally {
            _selectingCity = false;
          }
          if (!current()) return;
          if (widget.city.selectedCity?.id != cityID) {
            throw const FormatException('城市范围无法应用');
          }
        }
        final task = _workspace.task;
        final sameScope =
            task != null &&
            (refreshed.queryRoute == 'CITY'
                ? task.contextType == 'CITY' && task.cityID == refreshed.cityID
                : task.contextType == 'ONLINE' &&
                      task.contextID == refreshed.contextID);
        _viewChoice = refreshed;
        _submittedChoice = refreshed;
        _workspace.queryContextType = refreshed.queryRoute;
        if (refreshed.queryRoute == 'ONLINE') {
          _online.restoreContext(
            NowOnlineContext(id: refreshed.contextID, label: refreshed.label),
          );
        } else {
          _online.select(null);
        }
        if (!sameScope) _workspace.newTask(preserveMapSelection: true);
      } catch (_) {
        if (mounted && current()) {
          _composerKey.currentState?.fillDraft(query, requestFocus: false);
          ScaffoldMessenger.of(context).showSnackBar(
            const SnackBar(content: Text('查看范围或会话已变化，请重新选择。原任务、结果和输入已保留。')),
          );
        }
        return;
      } finally {
        api.dispose();
      }
    }
    if (!current()) return;
    if (_queryCityID == null && _queryOnlineID == null) {
      _workspace.requireCity(query);
      _pendingCityCurrent = current;
      return;
    }
    unawaited(
      _workspace.submit(
        query,
        widget.city.activities,
        widget.city.places,
        cityID: _queryCityID,
      ),
    );
  }

  Future<void> _openOnlineOpportunities() async {
    final identity = _currentSeedIdentity,
        epoch = _seedEntryEpoch,
        auth = widget.auth;
    bool current() =>
        mounted &&
        auth == widget.auth &&
        identity == _currentSeedIdentity &&
        epoch == _seedEntryEpoch;
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => NotificationDestinationBoundary(
          identityChanges: _activeIntentChanges,
          current: current,
          builder: (_) => OnlineSocialOpportunityPage(
            auth: auth,
            client: widget.seedClient,
            apiBaseUrl: widget.seedApiBaseUrl,
            workspaceChanges: _activeIntentChanges,
            organizationWorkspaceID: _activeIntentWorkspace,
          ),
        ),
      ),
    );
  }

  void _searchThisArea({String query = '搜索此区域'}) {
    final bounds = _mapState.searchAreaBounds ?? _mapState.viewportBounds;
    if (bounds == null) return;
    _mapState.beginSearch();
    final cityID = widget.city.selectedCity?.id;
    if (cityID != null) unawaited(_discovery.load(cityID, bounds));
    unawaited(
      _workspace.searchThisArea(
        widget.city.activities,
        widget.city.places,
        bounds: bounds,
        cityID: widget.city.selectedCity?.id,
        query: query,
      ),
    );
  }

  AgentResult? get _mapProjectionResult =>
      _workspace.queryContextType == 'ONLINE'
      ? _workspace.mapBackgroundResult
      : _workspace.presentedResult;

  List<MapEntity> _visibleEntities() {
    final online = _workspace.queryContextType == 'ONLINE';
    final result = online
        ? _workspace.mapBackgroundResult
        : _workspace.presentedResult;
    final cityID = online
        ? _workspace.mapBackgroundCityID
        : _workspace.task?.cityID;
    if (result?.projectionItems != null &&
        cityID == widget.city.selectedCity?.id) {
      return result!.entities;
    }
    if (_layers.cityID != widget.city.selectedCity?.id) return const [];
    return _layers.items.map((i) => i.entity).toList(growable: false);
  }

  Future<void> _openMapLayers() async {
    final epoch = _layers.epoch,
        auth = widget.auth,
        client = widget.seedClient,
        base = widget.seedApiBaseUrl;
    final identity = _currentSeedIdentity;
    bool current() =>
        mounted &&
        epoch == _layers.epoch &&
        auth == widget.auth &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl &&
        identity == _currentSeedIdentity;
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      builder: (sheetContext) => SizedBox(
        height: MediaQuery.sizeOf(sheetContext).height * .8,
        child: MapLayerControls(
          controller: _layers,
          current: current,
          onRefresh:
              _mapState.viewportBounds == null ||
                  widget.city.selectedCity == null
              ? null
              : () {
                  final city = widget.city.selectedCity?.id,
                      bounds = _mapState.viewportBounds;
                  if (current() && city != null && bounds != null) {
                    unawaited(_layers.load(city, bounds));
                  }
                },
          onOpen: (item) {
            if (!current() || _layers.find(item.mapID) == null) return;
            Navigator.pop(sheetContext);
            _workspace.selectEntity(item.mapID);
            _openEntityDetails(item.entity);
          },
        ),
      ),
    );
  }

  void _openEntityDetails(MapEntity entity) {
    final result = _mapProjectionResult;
    final matching = result?.projectionItems?.where(
      (i) => i.entity.mapID == entity.id,
    );
    if (_visibleEntities().any((visible) => visible.id == entity.id) &&
        matching != null &&
        matching.length == 1) {
      unawaited(
        _agentResultRouter(mapBackground: true).open(context, matching.single),
      );
      return;
    }
    final item = _layers.find(entity.id);
    if (item != null) {
      unawaited(_openTypedMapDetail(item));
      return;
    }
  }

  AgentEntityResultRouter _agentResultRouter({
    bool mapBackground = false,
    AgentResult? reply,
  }) {
    final identity = _currentSeedIdentity,
        epoch = _seedEntryEpoch,
        result =
            reply ?? (mapBackground ? _mapProjectionResult : _workspace.result);
    return AgentEntityResultRouter(
      auth: widget.auth,
      workspaceChanges: Listenable.merge([
        widget.auth,
        _organizations,
        _seedEntryChanges,
        _workspace,
      ]),
      workspaceID: () => _organizations.active?.id,
      apiBaseUrl: widget.seedApiBaseUrl,
      client: widget.seedClient,
      current: (item) =>
          mounted &&
          identity == _currentSeedIdentity &&
          epoch == _seedEntryEpoch &&
          (result?.replyProjectionCurrent ?? false) &&
          (reply != null
              ? _workspace.retainsReply(reply)
              : identical(
                  result,
                  mapBackground ? _mapProjectionResult : _workspace.result,
                )) &&
          (result?.projectionItems?.any((i) => identical(i, item)) ?? false),
    );
  }

  Widget _activeIntentCard() => ActiveSocialIntentCard(
    auth: widget.auth,
    client: widget.seedClient,
    apiBaseUrl: widget.seedApiBaseUrl,
    workspaceChanges: _activeIntentChanges,
    organizationWorkspaceID: _activeIntentWorkspace,
  );

  Future<void> _openActiveIntentCard() async {
    final auth = widget.auth,
        client = widget.seedClient,
        base = widget.seedApiBaseUrl;
    final identity = _currentSeedIdentity, epoch = _seedEntryEpoch;
    bool current() =>
        mounted &&
        auth == widget.auth &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl &&
        identity == _currentSeedIdentity &&
        epoch == _seedEntryEpoch;
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      builder: (sheetContext) => SizedBox(
        height: MediaQuery.sizeOf(sheetContext).height * .8,
        child: NotificationDestinationBoundary(
          identityChanges: _activeIntentChanges,
          current: current,
          builder: (_) => Scaffold(
            body: SafeArea(
              child: SingleChildScrollView(
                padding: const EdgeInsets.all(16),
                child: ActiveSocialIntentCard(
                  auth: auth,
                  client: client,
                  apiBaseUrl: base,
                  workspaceChanges: _activeIntentChanges,
                  organizationWorkspaceID: _activeIntentWorkspace,
                  onClose: () => Navigator.of(sheetContext).pop(),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  Future<void> _openTypedMapDetail(TypedMapItem item) async {
    final auth = widget.auth,
        client = widget.seedClient,
        base = widget.seedApiBaseUrl;
    final identity = _currentSeedIdentity, epoch = _layers.epoch;
    final changes = Listenable.merge([
      auth,
      _organizations,
      _seedEntryChanges,
      _layers,
    ]);
    bool current() =>
        mounted &&
        auth == widget.auth &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl &&
        identity == _currentSeedIdentity &&
        epoch == _layers.epoch &&
        _layers.find(item.mapID)?.sourceVersion == item.sourceVersion;
    if (!current()) return;
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => NotificationDestinationBoundary(
          identityChanges: changes,
          current: current,
          builder: (_) => ChatEntityDetail(
            type: item.detailType.toLowerCase(),
            id: item.detailID,
            auth: auth,
            workspaceChanges: changes,
            workspaceID: () => _organizations.active?.id,
            apiBaseUrl: base,
            client: client,
          ),
        ),
      ),
    );
  }

  void _openPlaceDetails(String placeID) {
    // The route can rebuild for keyboard, theme and text size. Keep its actual
    // providers stable; a genuine connection/owner replacement retires it.
    final auth = widget.auth;
    final owner = auth.accountID;
    final client = widget.placeDetailClient;
    final base = widget.placeDetailApiBaseUrl;
    final changes = Listenable.merge([
      auth,
      _organizations,
      _placeEntryChanges,
    ]);
    bool current() =>
        mounted &&
        identical(auth, widget.auth) &&
        owner == auth.accountID &&
        identical(client, widget.placeDetailClient) &&
        base == widget.placeDetailApiBaseUrl;
    String? authorization() => auth.authorizationHeader;
    String? workspaceID() => _organizations.active?.id;
    if (!current()) return;
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Theme.of(context).scaffoldBackgroundColor,
      builder: (sheetContext) => FractionallySizedBox(
        heightFactor: 0.72,
        child: NotificationDestinationBoundary(
          identityChanges: changes,
          current: current,
          builder: (_) => PlaceDetailSheet(
            onOpenMoment: (id) => openChatEntity(
              context,
              ChatEntityCard(type: 'moment', id: id, available: true),
              auth: auth,
              workspaceChanges: changes,
              workspaceID: workspaceID,
            ),
            placeID: placeID,
            client: client,
            apiBaseUrl: base,
            authorizationHeader: authorization,
            organizationWorkspaceID: workspaceID,
            workspaceChanges: changes,
            onOpenOrganization: (organizationID) {
              if (!current()) return;
              Navigator.pop(sheetContext);
              unawaited(_openPublicOrganization(organizationID));
            },
            onOpenBusiness: (businessID) {
              if (!current()) return;
              Navigator.pop(sheetContext);
              unawaited(_openPublicBusiness(businessID));
            },
            onOpenActivity: (activity) {
              if (!current()) return;
              Navigator.pop(sheetContext);
              _openFallbackActivity(activity, source: 'place');
            },
          ),
        ),
      ),
    );
  }

  void _openFallbackActivity(PublicActivity activity, {String source = 'now'}) {
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: const Color(0xFFFCFBF8),
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(26)),
      ),
      builder: (context) => ActivityDetailSheet(
        apiBaseUrl: widget.seedApiBaseUrl,
        client: widget.seedClient,
        identityChanges: Listenable.merge([widget.auth, _organizations]),
        workspaceID: () => _organizations.active?.id,
        activity: activity,
        entrySource: source,
        authorizationHeader: () => widget.auth.authorizationHeader,
        onParticipationChanged: () => unawaited(_participations.load()),
        onOpenOrganization: (organizationID) {
          Navigator.of(context).pop();
          unawaited(_openPublicOrganization(organizationID));
        },
        onOpenOrganizer: (organizer) {
          Navigator.of(context).pop();
          unawaited(_openActivityOrganizer(organizer));
        },
        saved: _saved,
      ),
    );
  }

  Future<void> _openPublicOrganization(String organizationID) async {
    if (!mounted) return;
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (context) => PublicOrganizationPage(
          organizationID: organizationID,
          authorizationHeader: () => widget.auth.authorizationHeader,
          workspaceID: () => _organizations.active?.id,
          identityChanges: Listenable.merge([widget.auth, _organizations]),
          onOpenActivity: (activity) =>
              _openFallbackActivity(activity, source: 'organization'),
        ),
      ),
    );
  }

  Future<void> _openPublicBusiness(String businessID) async {
    if (!mounted) return;
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => PublicBusinessPage(
          businessID: businessID,
          authorizationHeader: () => widget.auth.authorizationHeader,
          workspaceID: () => _organizations.active?.id,
          identityChanges: Listenable.merge([widget.auth, _organizations]),
          apiBaseUrl: widget.placeDetailApiBaseUrl,
          client: widget.placeDetailClient,
          onOpenActivity: (activity) =>
              _openFallbackActivity(activity, source: 'business'),
        ),
      ),
    );
  }

  Future<void> _openActivityOrganizer(PublicActivityOrganizer organizer) async {
    if (!mounted) return;
    switch (organizer.type) {
      case 'BUSINESS':
        await _openPublicBusiness(organizer.id);
        return;
      case 'ORGANIZATION':
        await _openPublicOrganization(organizer.id);
        return;
      case 'COMMUNITY':
        final api = CommunityApi(
          authorizationHeader: () => widget.auth.authorizationHeader,
        );
        try {
          await Navigator.of(context).push(
            MaterialPageRoute<void>(
              builder: (_) => CommunityDetailPage(
                id: organizer.id,
                api: api,
                auth: widget.auth,
                city: widget.city,
                organizations: _organizations,
              ),
            ),
          );
        } finally {
          api.dispose();
        }
        return;
      case 'PERSON':
        await Navigator.of(context).push(
          MaterialPageRoute<void>(
            builder: (_) => PublicPersonPage(
              accountID: organizer.id,
              identityChanges: Listenable.merge([widget.auth, _organizations]),
              workspaceID: () => _organizations.active?.id,
              authorizationHeader: () => widget.auth.authorizationHeader,
            ),
          ),
        );
        return;
    }
  }

  void _inbox() {
    _pauseComposer();
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: const Color(0xFFFCFBF8),
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(26)),
      ),
      builder: (context) => SizedBox(
        height: MediaQuery.sizeOf(context).height * 0.82,
        child: InboxPanel(
          auth: widget.auth,
          workspaceChanges: _organizations,
          organizationWorkspaceID: () => _organizations.active?.id,
          onOrganizationAccepted: _organizations.load,
          onOpenActivity: (activityID) {
            Navigator.of(context).pop();
            unawaited(_openActivityByID(activityID, source: 'inbox'));
          },
          onOpenConversation: (conversationID) {
            Navigator.of(context).pop();
            unawaited(_openConversationByID(conversationID));
          },
        ),
      ),
    );
  }

  Future<void> _openActivityByID(
    String activityID, {
    String source = 'direct',
  }) async {
    const apiBase = BirdtieEnvironment.apiBaseUrl;
    if (apiBase.isEmpty) return;
    final token = widget.auth.authorizationHeader;
    final serial = _authSerial;
    final workspaceSerial = _workspaceSerial;
    final workspace = _organizations.active?.id;
    bool current() =>
        _currentAccount(serial, token) &&
        workspaceSerial == _workspaceSerial &&
        workspace == _organizations.active?.id;
    try {
      final response = await http
          .get(
            Uri.parse(
              '${apiBase.replaceFirst(RegExp(r'/$'), '')}/v1/activities/${Uri.encodeComponent(activityID)}',
            ),
            headers: {'Authorization': ?token},
          )
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) throw StateError('Activity unavailable');
      if (!current()) return;
      final activity = PublicActivity.fromJson(
        (jsonDecode(response.body) as Map<String, dynamic>)['data']
            as Map<String, dynamic>,
      );
      if (activity.id != activityID) throw const FormatException('活动标识不符');
      if (current()) {
        _openFallbackActivity(activity, source: source);
      }
    } catch (_) {
      if (mounted && current()) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('活动详情暂不可用，请稍后重试。')));
      }
    }
  }

  Future<void> _openConversationByID(String conversationID) async {
    final token = widget.auth.authorizationHeader,
        serial = _authSerial,
        workspaceSerial = _workspaceSerial,
        workspace = _organizations.active?.id;
    try {
      final conversations = await _connections.conversations();
      if (!mounted ||
          !_currentAccount(serial, token) ||
          workspaceSerial != _workspaceSerial ||
          workspace != _organizations.active?.id) {
        return;
      }
      HumanConversation? target;
      for (final conversation in conversations) {
        if (conversation.id == conversationID) {
          target = conversation;
          break;
        }
      }
      if (target == null) throw StateError('Conversation unavailable');
      final selected = target;
      await Navigator.of(context).push(
        MaterialPageRoute<void>(
          builder: (context) => HumanConversationRoute(
            auth: widget.auth,
            conversation: selected,
            workspaceChanges: _organizations,
            workspaceID: () => _organizations.active?.id,
          ),
        ),
      );
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('对话暂不可用，请稍后重试。')));
      }
    }
  }

  Future<void> _createOrganization() async {
    if (!widget.auth.signedIn) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('请先登录再创建组织工作区。')));
      return;
    }
    final token = widget.auth.authorizationHeader;
    final serial = _authSerial;
    final name = TextEditingController();
    var type = 'student_society';
    var saving = false;
    await showDialog<void>(
      context: context,
      builder: (dialogContext) {
        _confirmationContext = dialogContext;
        return StatefulBuilder(
          builder: (dialogContext, update) => AlertDialog(
            title: const Text('创建组织工作区'),
            content: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                TextField(
                  controller: name,
                  maxLength: 160,
                  decoration: const InputDecoration(labelText: '组织名称'),
                ),
                DropdownButtonFormField<String>(
                  initialValue: type,
                  decoration: const InputDecoration(labelText: '组织类型'),
                  items: const [
                    DropdownMenuItem(
                      value: 'student_society',
                      child: Text('学生社团'),
                    ),
                    DropdownMenuItem(value: 'club', child: Text('俱乐部')),
                    DropdownMenuItem(value: 'business', child: Text('商家')),
                    DropdownMenuItem(value: 'university', child: Text('高校')),
                    DropdownMenuItem(value: 'community', child: Text('社区组织')),
                    DropdownMenuItem(value: 'venue', child: Text('场所')),
                    DropdownMenuItem(value: 'nonprofit', child: Text('非营利组织')),
                    DropdownMenuItem(value: 'other', child: Text('其他')),
                  ],
                  onChanged: (value) {
                    if (value != null) update(() => type = value);
                  },
                ),
                const SizedBox(height: 8),
                const Text(
                  '你的账号将成为首位所有者。组织工作区的 Agent 权限与个人数据分开管理。',
                  style: TextStyle(fontSize: 12),
                ),
              ],
            ),
            actions: [
              TextButton(
                onPressed: saving ? null : () => Navigator.pop(dialogContext),
                child: const Text('取消'),
              ),
              FilledButton(
                onPressed: saving
                    ? null
                    : () async {
                        if (!_currentAccount(serial, token)) return;
                        update(() => saving = true);
                        var created = false;
                        try {
                          await _organizations.create(name.text.trim(), type);
                          created = true;
                          if (_currentAccount(serial, token) &&
                              dialogContext.mounted) {
                            Navigator.pop(dialogContext);
                          }
                        } catch (_) {
                          if (mounted && _currentAccount(serial, token)) {
                            ScaffoldMessenger.of(context).showSnackBar(
                              const SnackBar(content: Text('创建组织工作区失败，请重试。')),
                            );
                          }
                        } finally {
                          if (!created && dialogContext.mounted) {
                            update(() => saving = false);
                          }
                        }
                      },
                child: const Text('创建'),
              ),
            ],
          ),
        );
      },
    );
    if (_currentAccount(serial, token)) _confirmationContext = null;
    // showDialog completes before the reverse transition removes its TextField.
    await Future<void>.delayed(const Duration(milliseconds: 350));
    name.dispose();
  }

  Future<void> _viewOrganizationInvitations() async {
    if (!widget.auth.signedIn) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('请先登录再查看组织邀请。')));
      return;
    }
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (context) => OrganizationInvitationsPage(
          auth: widget.auth,
          onAccepted: _organizations.load,
        ),
      ),
    );
  }

  Future<void> _requestContact(AgentPerson person) async {
    if (!widget.auth.signedIn) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('请先登录再申请联系。')));
      return;
    }
    final cityID = widget.city.selectedCity?.id;
    if (cityID == null) return;
    final token = widget.auth.authorizationHeader;
    final serial = _authSerial;
    final account = widget.auth.accountID;
    final workspace = _organizations.active?.id;
    final source = _connections;
    bool current() =>
        _currentAccount(serial, token) &&
        widget.auth.accountID == account &&
        _organizations.active?.id == workspace &&
        widget.city.selectedCity?.id == cityID;
    final note = TextEditingController();
    var sending = false;
    var requestFriend = true;
    await showDialog<void>(
      context: context,
      builder: (dialogContext) {
        _confirmationContext = dialogContext;
        return StatefulBuilder(
          builder: (dialogContext, update) => AlertDialog(
            scrollable: true,
            title: Text('联系 ${person.displayName}'),
            content: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const Text('对方可以选择是否接受。你的留言和显示名称会分享给对方。'),
                SegmentedButton<bool>(
                  segments: const [
                    ButtonSegment(value: true, label: Text('加为好友')),
                    ButtonSegment(value: false, label: Text('申请私信')),
                  ],
                  selected: {requestFriend},
                  onSelectionChanged: sending
                      ? null
                      : (value) => update(() => requestFriend = value.first),
                ),
                Text(requestFriend ? '建立持续的好友关系，不会自动开启私信。' : '仅请求开启这次对话。'),
                TextField(
                  controller: note,
                  maxLength: 280,
                  maxLines: 3,
                  decoration: const InputDecoration(labelText: '留言'),
                ),
              ],
            ),
            actions: [
              TextButton(
                onPressed: sending ? null : () => Navigator.pop(dialogContext),
                child: const Text('取消'),
              ),
              FilledButton(
                onPressed: sending
                    ? null
                    : () async {
                        if (!_currentAccount(serial, token)) return;
                        final body = note.text.trim();
                        if (body.isEmpty) return;
                        update(() => sending = true);
                        try {
                          final isFriend = requestFriend;
                          var submitted = false;
                          await runEntityAction(
                            context,
                            ref: EntityActionRef('person', person.accountID),
                            kind: EntityActionKind.connect,
                            operation: isFriend
                                ? 'REQUEST_FRIEND'
                                : 'REQUEST_CONVERSATION',
                            authorizationHeader: () => current() ? token : null,
                            accountID: () => current() ? account : null,
                            workspaceID: () => _organizations.active?.id,
                            identityChanges: _activeIntentChanges,
                            client: source.followClient,
                            apiBaseUrl: source.followApiBaseUrl,
                            domainCurrent: current,
                            reviewDetails: (_) =>
                                '留言：$body\n${isFriend ? '申请持续好友关系' : '申请本次私信；当前城市：$cityID'}',
                            handler: (approved) async {
                              if (!current()) return;
                              if (isFriend) {
                                await source.requestFriend(
                                  person.accountID,
                                  body,
                                  approved: approved,
                                );
                              } else {
                                await source.request(
                                  person.accountID,
                                  cityID,
                                  body,
                                  approved: approved,
                                );
                              }
                              submitted = true;
                            },
                          );
                          if (!submitted) return;
                          if (current() && dialogContext.mounted) {
                            Navigator.pop(dialogContext);
                          }
                          if (mounted && _currentAccount(serial, token)) {
                            ScaffoldMessenger.of(context).showSnackBar(
                              SnackBar(
                                content: Text(
                                  '${requestFriend ? '好友' : '私信'}申请已发送，请在收件箱查看回复。',
                                ),
                              ),
                            );
                          }
                        } catch (_) {
                          if (mounted && _currentAccount(serial, token)) {
                            ScaffoldMessenger.of(context).showSnackBar(
                              const SnackBar(
                                content: Text('申请结果暂未确认，请先检查收件箱；不会自动重新发送。'),
                              ),
                            );
                          }
                        } finally {
                          if (dialogContext.mounted) {
                            update(() => sending = false);
                          }
                        }
                      },
                child: const Text('检查申请'),
              ),
            ],
          ),
        );
      },
    );
    if (_currentAccount(serial, token)) _confirmationContext = null;
    await Future<void>.delayed(const Duration(milliseconds: 350));
    note.dispose();
  }

  void _destination(SidebarDestination destination) {
    _pauseComposer();
    if (_scaffold.currentState?.isDrawerOpen == true) Navigator.pop(context);
    if (destination == SidebarDestination.home) return;
    final personalDestination =
        destination == SidebarDestination.profile ||
        destination == SidebarDestination.settings;
    final auth = widget.auth, moments = widget.moments;
    final client = widget.seedClient, base = widget.seedApiBaseUrl;
    final personalSourceSerial = _personalSourceSerial;
    bool currentPersonalSource() =>
        mounted &&
        personalSourceSerial == _personalSourceSerial &&
        auth == widget.auth &&
        moments == widget.moments &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl;
    final title = switch (destination) {
      SidebarDestination.activities => '我的活动',
      SidebarDestination.organization => '组织活动管理',
      SidebarDestination.business => '商家工作台',
      SidebarDestination.groups => '社群',
      SidebarDestination.saved => '收藏',
      SidebarDestination.profile => '个人资料',
      SidebarDestination.settings => '设置',
      SidebarDestination.home => '首页',
    };
    final content = switch (destination) {
      SidebarDestination.activities => MyActivitiesPage(
        plans: _plans,
        participations: _participations,
        saved: _saved,
        auth: widget.auth,
        city: widget.city,
        organizations: _organizations,
      ),
      SidebarDestination.organization => OrganizationConsolePage(
        auth: widget.auth,
        city: widget.city,
        organizations: _organizations,
      ),
      SidebarDestination.business => BusinessConsolePage(
        auth: widget.auth,
        city: widget.city,
        organizations: _organizations,
      ),
      SidebarDestination.profile => LegacyProfilePage(
        auth: widget.auth,
        moments: widget.moments,
        city: widget.city,
        client: widget.seedClient,
        apiBaseUrl: widget.seedApiBaseUrl,
        workspaceChanges: _organizations,
        organizationWorkspaceID: () => _organizations.active?.id,
      ),
      SidebarDestination.groups => CommunityPage(
        auth: widget.auth,
        city: widget.city,
        organizations: _organizations,
      ),
      SidebarDestination.saved => SavedPage(saved: _saved),
      SidebarDestination.settings => SettingsPage(
        auth: widget.auth,
        city: widget.city,
        moments: widget.moments,
        client: widget.seedClient,
        apiBaseUrl: widget.seedApiBaseUrl,
        workspaceChanges: _organizations,
        organizationWorkspaceID: () => _organizations.active?.id,
      ),
      _ => Center(
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Text(
            '$title 暂未开放。',
            textAlign: TextAlign.center,
            style: const TextStyle(color: Color(0xFF747B73)),
          ),
        ),
      ),
    };
    Navigator.push(
      context,
      MaterialPageRoute<void>(
        builder: (context) => Scaffold(
          appBar: AppBar(title: Text(title)),
          body: personalDestination
              ? NotificationDestinationBoundary(
                  identityChanges: _seedEntryChanges,
                  current: currentPersonalSource,
                  builder: (_) => content,
                )
              : content,
        ),
      ),
    );
  }

  Future<void> _openPrivateMomentImage() async {
    _pauseComposer();
    final auth = widget.auth, moments = widget.moments;
    final seedClient = widget.seedClient, seedBase = widget.seedApiBaseUrl;
    final identity = _currentSeedIdentity;
    final epoch = _seedEntryEpoch, momentEpoch = moments.identityEpoch;
    final personalSourceSerial = _personalSourceSerial;
    final taskEpoch = _workspace.taskEpoch, viewSerial = _viewSerial;
    final city = widget.city.selectedCity?.id;
    final online = _online.selected?.id;
    void explain(String message) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(message)));
      }
    }

    if (identity.$1 == null || identity.$2 == null) {
      explain('请先登录，再为本人的私人记录添加图片。');
      return;
    }
    if (identity.$3 != null) {
      explain('私人图片属于个人记录，请先切回个人身份。');
      return;
    }
    String? environment(String value) {
      final uri = Uri.tryParse(value);
      if (uri == null ||
          !uri.hasAuthority ||
          !['https', 'http'].contains(uri.scheme) ||
          uri.userInfo.isNotEmpty ||
          uri.hasQuery ||
          uri.hasFragment) {
        return null;
      }
      return uri
          .replace(path: uri.path.replaceFirst(RegExp(r'/$'), ''))
          .toString();
    }

    final momentBase = environment(moments.apiBaseUrl);
    final nowBase = environment(seedBase ?? BirdtieEnvironment.apiBaseUrl);
    if (momentBase == null || nowBase == null) {
      explain('私人图片服务尚未连接，请稍后重试。');
      return;
    }
    if (momentBase != nowBase ||
        moments.authorizationHeader() != identity.$1 ||
        moments.ownerID?.call() != identity.$2) {
      explain('私人记录与当前登录来源不一致，请重新打开应用后重试。');
      return;
    }
    bool current() =>
        mounted &&
        auth == widget.auth &&
        moments == widget.moments &&
        seedClient == widget.seedClient &&
        seedBase == widget.seedApiBaseUrl &&
        identity == _currentSeedIdentity &&
        epoch == _seedEntryEpoch &&
        personalSourceSerial == _personalSourceSerial &&
        momentEpoch == moments.identityEpoch &&
        taskEpoch == _workspace.taskEpoch &&
        viewSerial == _viewSerial &&
        city == widget.city.selectedCity?.id &&
        online == _online.selected?.id;
    final changes = Listenable.merge([
      auth,
      moments,
      _activeIntentChanges,
      _workspace,
      widget.city,
      _online,
    ]);
    if (!current()) return;
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => NotificationDestinationBoundary(
          identityChanges: changes,
          current: current,
          builder: (_) => NowPrivateMomentImagePage(
            auth: auth,
            moments: moments,
            identityChanges: changes,
            current: current,
            organizationWorkspaceID: () => _organizations.active?.id,
          ),
        ),
      ),
    );
  }

  Future<void> _openAgentAction(AgentAction action) async {
    if (action.type == 'OPEN_OPPORTUNITIES') {
      await Navigator.push(
        context,
        MaterialPageRoute<void>(
          builder: (_) => OpportunityPage(auth: widget.auth),
        ),
      );
      return;
    }
    if (action.type == 'OPEN_NEW_PEOPLE') {
      await Navigator.push(
        context,
        MaterialPageRoute<void>(
          builder: (_) => NewPeoplePage(auth: widget.auth),
        ),
      );
      return;
    }
    if (action.type == 'OPEN_RELATIONSHIP_CONTEXT') {
      await Navigator.push(
        context,
        MaterialPageRoute<void>(
          builder: (_) => AgentRelationshipPage(auth: widget.auth),
        ),
      );
      // Signal snapshots are never retained in the map or public result entities.
      return;
    }
    if (action.type == 'OPEN_PLACE' && action.targetID != null) {
      _openPlaceDetails(action.targetID!);
      return;
    }
    if (action.type != 'OPEN_ORGANIZATION_CONSOLE' || action.targetID == null) {
      return;
    }
    await _organizations.load();
    if (!mounted) return;
    OrganizationWorkspace? allowed;
    for (final item in _organizations.organizations) {
      if (item.id == action.targetID &&
          (item.role == 'owner' || item.role == 'admin')) {
        allowed = item;
        break;
      }
    }
    if (allowed == null) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('组织权限已变化，请重新登录或刷新后重试。')));
      return;
    }
    _organizations.select(allowed);
    await Navigator.push(
      context,
      MaterialPageRoute<void>(
        builder: (context) => Scaffold(
          appBar: AppBar(title: const Text('组织活动管理')),
          body: OrganizationConsolePage(
            auth: widget.auth,
            city: widget.city,
            organizations: _organizations,
          ),
        ),
      ),
    );
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _workspace.updateReplyRefreshVisibility();
  }

  @override
  Widget build(BuildContext context) {
    // Subscribe to the existing route; returning never reopens or submits.
    _workspace.updateReplyRefreshVisibility();
    return _buildWorkspace(context);
  }

  Widget _buildWorkspace(BuildContext context) => AnimatedBuilder(
    animation: Listenable.merge([
      widget.city,
      _workspace,
      _mapState,
      _organizations,
      _discovery,
      _online,
      _layers,
    ]),
    builder: (context, _) => Scaffold(
      key: _scaffold,
      resizeToAvoidBottomInset: false,
      drawer: Sidebar(
        workspace: _workspace,
        city: widget.city,
        onCitySelected: (id) {
          _selectPublicCity(id);
        },
        onNew: () {
          ++_viewSerial;
          _pendingCityCurrent = null;
          _workspace.newTask();
          Navigator.pop(context);
        },
        onDestination: _destination,
        onTools: () {
          _scaffold.currentState?.closeDrawer();
          unawaited(_openNowTools());
        },
        onWorkspaceSelected: () => _scaffold.currentState?.closeDrawer(),
        onRecent: (task) {
          Navigator.pop(context);
          unawaited(_reopenTask(task));
        },
        organizations: _organizations,
        onCreateOrganization: _createOrganization,
        onViewInvitations: _viewOrganizationInvitations,
        signedIn: widget.auth.signedIn,
        onChooseCity: () {
          Navigator.pop(context);
          unawaited(_chooseCity());
        },
      ),
      body: LayoutBuilder(
        builder: (context, constraints) {
          // Scaffold does not resize: consume IME once for these overlays.
          final keyboard = MediaQuery.viewInsetsOf(context).bottom;
          final visibleHeight = (constraints.maxHeight - keyboard).clamp(
            0.0,
            constraints.maxHeight,
          );
          final safeBottom = MediaQuery.paddingOf(context).bottom;
          final composerBottom = safeBottom + keyboard + 16;
          final composerMaxHeight =
              (visibleHeight -
                      MediaQuery.paddingOf(context).top -
                      safeBottom -
                      32)
                  .clamp(0.0, constraints.maxHeight);
          final continuousConversation =
              _workspace.inputFocused ||
              (_workspace.task != null &&
                  _workspace.sheetExtent == AgentSheetExtent.expanded);
          final sheetBottom = composerBottom + _composerHeight + 12;
          final topControlsHeight =
              _topControlsHeight ?? MediaQuery.paddingOf(context).top + 58;
          final ornamentBottom = topControlsHeight + 8 + _ornamentHeight;
          // The continuous conversation surface still starts below the map's
          // attribution lane. Its composer padding belongs to that same
          // surface, so focusing input never covers the persistent map chrome.
          final chromeBottom = ornamentBottom + 8;
          final sheetHeight =
              (constraints.maxHeight - sheetBottom - chromeBottom).clamp(
                0.0,
                constraints.maxHeight,
              );
          final hasSheet =
              _workspace.task != null &&
              _workspace.sheetExtent != AgentSheetExtent.hidden;
          final hasSearchArea =
              _online.selected == null && _mapState.searchAreaBounds != null;
          final currentSelection = _workspace.selectedEntityId != null
              ? _visibleEntities()
                    .where((entity) => entity.id == _workspace.selectedEntityId)
                    .take(1)
                    .toList(growable: false)
              : const <MapEntity>[];
          final selectedPeek =
              _workspace.sheetExtent != AgentSheetExtent.expanded
              ? currentSelection
              : const <MapEntity>[];
          final searchInLane =
              hasSearchArea && (hasSheet || selectedPeek.isNotEmpty);
          final hasContextLane = searchInLane || selectedPeek.isNotEmpty;
          final composerTop =
              constraints.maxHeight -
              composerBottom -
              _composerHeight.clamp(0.0, composerMaxHeight);
          final idlePrivateBottom = constraints.maxHeight - composerTop + 12;
          final idlePrivateHeight = (composerTop - chromeBottom - 12).clamp(
            0.0,
            constraints.maxHeight,
          );
          final tasklessHeaderOverlap =
              _workspace.task == null && composerTop < topControlsHeight;
          final needsKeyboardRestore =
              keyboard > 0 &&
              (_workspace.task != null
                  ? sheetHeight <= 48
                  : tasklessHeaderOverlap);
          final contextTop = chromeBottom;
          final contextHeight =
              (constraints.maxHeight - sheetBottom - contextTop).clamp(
                0.0,
                constraints.maxHeight * .45,
              );
          return Stack(
            children: [
              Positioned.fill(child: _mapCanvas),
              if (continuousConversation && sheetHeight >= 48)
                Positioned(
                  top: chromeBottom,
                  left: 0,
                  right: 0,
                  bottom: keyboard,
                  child: Material(
                    color: Theme.of(context).colorScheme.surface,
                    borderRadius: const BorderRadius.vertical(
                      top: Radius.circular(26),
                    ),
                  ),
                ),
              if (!needsKeyboardRestore)
                Positioned(
                  top: 0,
                  left: 0,
                  right: 0,
                  child: TopControls(
                    onHeightChanged: (height) {
                      if (!mounted || _topControlsHeight == height) return;
                      setState(() => _topControlsHeight = height);
                      _ornamentTop.value = height + 8;
                    },
                    onSidebar: () {
                      _pauseComposer();
                      _scaffold.currentState?.openDrawer();
                    },
                    onInbox: _inbox,
                    onTools: () => unawaited(_openNowTools()),
                    onContext: () => unawaited(_chooseCity()),
                    contextLabel:
                        _viewChoice?.label ??
                        (_online.selected == null
                            ? widget.city.selectedCity?.name ?? '未选城市'
                            : '线上 · ${_online.selected!.label}'),
                  ),
                ),
              if (hasSheet || selectedPeek.isNotEmpty)
                Positioned(
                  top: chromeBottom,
                  left: 0,
                  right: 0,
                  height: sheetHeight,
                  child: Column(
                    children: [
                      if (hasContextLane) ...[
                        ConstrainedBox(
                          constraints: BoxConstraints(
                            maxHeight: (sheetHeight - (hasSheet ? 60 : 0))
                                .clamp(0.0, sheetHeight),
                          ),
                          child: SingleChildScrollView(
                            key: const Key('now-selected-context-scroll'),
                            child: Column(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                if (searchInLane ||
                                    (keyboard > 0 && !needsKeyboardRestore))
                                  Row(
                                    children: [
                                      Expanded(
                                        child: searchInLane
                                            ? Center(
                                                child: FilledButton.icon(
                                                  onPressed:
                                                      _workspace.state ==
                                                          AgentViewState
                                                              .searching
                                                      ? null
                                                      : () => _searchThisArea(),
                                                  icon: const Icon(
                                                    Icons.search,
                                                  ),
                                                  label: const Text('搜索此区域'),
                                                ),
                                              )
                                            : const SizedBox.shrink(),
                                      ),
                                      if (keyboard > 0 && !needsKeyboardRestore)
                                        IconButton(
                                          key: const Key(
                                            'now-context-keyboard-restore',
                                          ),
                                          constraints: const BoxConstraints(
                                            minWidth: 48,
                                            minHeight: 48,
                                          ),
                                          tooltip: '收起键盘查看结果',
                                          icon: const Icon(
                                            Icons.keyboard_hide_outlined,
                                          ),
                                          onPressed: _pauseComposer,
                                        ),
                                    ],
                                  ),
                                if ((searchInLane ||
                                        (keyboard > 0 &&
                                            !needsKeyboardRestore)) &&
                                    selectedPeek.isNotEmpty)
                                  const SizedBox(height: 8),
                                for (final entity in selectedPeek)
                                  Padding(
                                    padding: const EdgeInsets.symmetric(
                                      horizontal: 16,
                                    ),
                                    child: EntityPeekCard(
                                      entity: entity,
                                      onOpen: () => _openEntityDetails(entity),
                                    ),
                                  ),
                              ],
                            ),
                          ),
                        ),
                        if (hasSheet && sheetHeight >= 60)
                          const SizedBox(height: 12),
                      ],
                      if (hasSheet)
                        Expanded(
                          child: LayoutBuilder(
                            builder: (context, remaining) {
                              final evidenceRouter = _agentResultRouter();
                              final sheet = AgentResultsSheet(
                                workspace: _workspace,
                                publicEvidenceCurrent: evidenceRouter.current,
                                publicEvidenceChanges:
                                    evidenceRouter.workspaceChanges,
                                saved: _saved,
                                plans: _plans,
                                onContact: _requestContact,
                                onOpenActivity: (activity) =>
                                    _openFallbackActivity(
                                      activity,
                                      source: 'agent',
                                    ),
                                onOpenPlace: _openPlaceDetails,
                                onOpenEntity: (item) => unawaited(
                                  _agentResultRouter().open(context, item),
                                ),
                                onOpenReplyEntity: (source, item) => unawaited(
                                  _agentResultRouter(
                                    reply: source,
                                  ).open(context, item),
                                ),
                                onShareEntity: (item) => unawaited(
                                  _agentResultRouter().share(context, item),
                                ),
                                onSaveEntity: (item) => unawaited(
                                  _agentResultRouter().save(
                                    context,
                                    item,
                                    _saved,
                                  ),
                                ),
                                onOpenOnlineIntent: (id) =>
                                    unawaited(_openOnlineIntent(id)),
                                onActivityImpression: (activity) =>
                                    _recordImpression(activity, 'agent'),
                                onAction: (action) =>
                                    unawaited(_openAgentAction(action)),
                                onSaveSocialIntent: (task) =>
                                    unawaited(_openIntentDraft(task: task)),
                                onSuggestion: (suggestion) => _composerKey
                                    .currentState
                                    ?.fillDraft(suggestion),
                                onRetry: () => unawaited(_workspace.retry()),
                                onChooseCity: () => unawaited(_chooseCity()),
                                availableHeight: remaining.maxHeight,
                              );
                              return Align(
                                alignment: Alignment.bottomCenter,
                                child: sheet,
                              );
                            },
                          ),
                        ),
                    ],
                  ),
                ),
              if (_online.selected == null &&
                  _workspace.task == null &&
                  currentSelection.isEmpty &&
                  !_mapState.cameraMoving &&
                  contextHeight >= 48)
                Positioned(
                  top: contextTop,
                  left: 16,
                  right: 16,
                  child: ConstrainedBox(
                    constraints: BoxConstraints(maxHeight: contextHeight),
                    child: SingleChildScrollView(
                      key: const Key('now-native-context-scroll'),
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          if (hasSearchArea && !searchInLane) ...[
                            Center(
                              child: FilledButton.icon(
                                onPressed:
                                    _workspace.state == AgentViewState.searching
                                    ? null
                                    : () => _searchThisArea(),
                                icon: const Icon(Icons.search),
                                label: const Text('搜索此区域'),
                              ),
                            ),
                            const SizedBox(height: 8),
                          ],
                          if (_layers.error != null &&
                              _layers.cityID == widget.city.selectedCity?.id &&
                              _layers.bounds != null) ...[
                            Material(
                              key: const Key('now-map-source-recovery'),
                              color: Theme.of(context).colorScheme.surface,
                              borderRadius: BorderRadius.circular(16),
                              child: Padding(
                                padding: const EdgeInsets.all(12),
                                child: Column(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: [
                                    Text(_layers.error!),
                                    TextButton.icon(
                                      onPressed: _layers.loading
                                          ? null
                                          : () => unawaited(
                                              _layers.load(
                                                _layers.cityID!,
                                                _layers.bounds!,
                                              ),
                                            ),
                                      icon: const Icon(Icons.refresh),
                                      label: Text(
                                        _layers.loading ? '正在更新点位…' : '更新地图点位',
                                      ),
                                    ),
                                  ],
                                ),
                              ),
                            ),
                            const SizedBox(height: 8),
                          ],
                          if (widget.auth.signedIn &&
                              _organizations.active == null) ...[
                            _activeIntentCard(),
                            const SizedBox(height: 8),
                          ],
                          if (widget.city.selectedCity != null &&
                              (_mapState.viewportBounds != null ||
                                  _mapState.mapError != null))
                            AreaPulseStack(
                              pulse: _discovery.pulse,
                              bounds: _mapState.viewportBounds,
                              loading: _discovery.loading,
                              errorMessage:
                                  _mapState.mapError ?? _discovery.error,
                              fallbackActivities: _mapState.mapError == null
                                  ? null
                                  : _discovery.cityActivities,
                              fallbackLoading: _discovery.cityLoading,
                              fallbackError: _discovery.cityError,
                              onOpenActivity: _openFallbackActivity,
                              onActivityImpression: (activity) =>
                                  _recordImpression(activity, 'now'),
                              onRetry:
                                  _mapState.mapError == null &&
                                      _mapState.viewportBounds != null &&
                                      widget.city.selectedCity != null
                                  ? () => unawaited(
                                      _discovery.load(
                                        widget.city.selectedCity!.id,
                                        _mapState.viewportBounds!,
                                      ),
                                    )
                                  : null,
                              onSearch: (query) =>
                                  _searchThisArea(query: query),
                            ),
                        ],
                      ),
                    ),
                  ),
                ),
              Positioned(
                left: 16,
                right: 16,
                bottom: composerBottom,
                child: ConstrainedBox(
                  constraints: BoxConstraints(maxHeight: composerMaxHeight),
                  child: AgentComposer(
                    key: _composerKey,
                    onHeightChanged: (height) {
                      if (mounted && (_composerHeight - height).abs() > .5) {
                        setState(() => _composerHeight = height);
                      }
                    },
                    workspace: _workspace,
                    onPrivateMomentImage: () =>
                        unawaited(_openPrivateMomentImage()),
                    privateMomentImageContext: () => (
                      widget.moments,
                      widget.moments.identityEpoch,
                      _personalSourceSerial,
                    ),
                    materialContext: () => (
                      widget.auth,
                      _currentSeedIdentity,
                      _seedEntryEpoch,
                      widget.seedClient,
                      widget.seedApiBaseUrl,
                      widget.city.selectedCity?.id,
                      _workspace.queryContextType,
                      _online.selected?.id,
                    ),
                    materialContextChanges: Listenable.merge([
                      _activeIntentChanges,
                      widget.city,
                      _online,
                    ]),
                    onSubmit: _submit,
                    onSearchArea: _searchThisArea,
                    hasSearchArea:
                        _online.selected == null &&
                        _mapState.searchAreaBounds != null,
                    onlineMode: _online.selected != null,
                    suggestions: const [],
                    reserveKeyboardRestoreSpace:
                        needsKeyboardRestore && visibleHeight >= 48,
                  ),
                ),
              ),
              if (needsKeyboardRestore && visibleHeight >= 48)
                Positioned(
                  top: (MediaQuery.paddingOf(context).top + 10).clamp(
                    0.0,
                    visibleHeight - 48,
                  ),
                  right: 16,
                  child: Material(
                    color: const Color(0xFFFCFBF8),
                    shape: const CircleBorder(),
                    child: IconButton(
                      key: const Key('now-sheet-keyboard-restore'),
                      constraints: const BoxConstraints(
                        minWidth: 48,
                        minHeight: 48,
                      ),
                      tooltip: _workspace.task == null
                          ? '收起键盘返回地图'
                          : '收起键盘查看结果',
                      icon: const Icon(Icons.keyboard_hide_outlined),
                      onPressed: _pauseComposer,
                    ),
                  ),
                ),
              if (widget.auth.signedIn &&
                  _organizations.active == null &&
                  _workspace.task == null &&
                  !needsKeyboardRestore &&
                  idlePrivateHeight >= 48)
                Positioned(
                  left: 16,
                  right: 16,
                  top: chromeBottom,
                  bottom: idlePrivateBottom,
                  child: Align(
                    alignment: Alignment.bottomRight,
                    child: SingleChildScrollView(
                      key: const Key('now-idle-private-actions'),
                      child: Material(
                        borderRadius: BorderRadius.circular(12),
                        color: Theme.of(context).colorScheme.surface,
                        child: Wrap(
                          alignment: WrapAlignment.end,
                          children: [
                            TextButton.icon(
                              icon: const Icon(Icons.people_outline),
                              label: const Text('我的社交近况'),
                              onPressed: _openSocialNowRoute,
                            ),
                            TextButton.icon(
                              icon: const Icon(Icons.edit_note),
                              label: const Text('完善我的选择'),
                              onPressed: () =>
                                  _openSeedRoute(progressive: true),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                ),
            ],
          );
        },
      ),
    ),
  );
}

enum _NowScopeAction { online, manage }

/// One temporary selector reuses the native read-only context controller.
/// Selecting a declared view never submits a query or replaces the map/task.
class _NowScopePicker extends StatefulWidget {
  const _NowScopePicker({
    required this.city,
    required this.auth,
    required this.identityChanges,
    required this.organizationWorkspaceID,
    required this.current,
    required this.reloadCities,
    required this.onClose,
    this.client,
    this.apiBaseUrl,
  });
  final PublicCityController city;
  final BirdtieAuthController auth;
  final Listenable identityChanges;
  final String? Function() organizationWorkspaceID;
  final bool Function() current;
  final Future<void> Function() reloadCities;
  final ValueChanged<Object?> onClose;
  final http.Client? client;
  final String? apiBaseUrl;

  @override
  State<_NowScopePicker> createState() => _NowScopePickerState();
}

class _NowScopePickerState extends State<_NowScopePicker> {
  late final NowContextSelectionController _data =
      NowContextSelectionController(
        api: NowContextSelectionAPI(
          client: widget.client,
          apiBaseUrl: widget.apiBaseUrl,
        ),
        identity: () => NowSelectionIdentity(
          widget.auth.authorizationHeader,
          widget.auth.accountID,
          widget.organizationWorkspaceID(),
        ),
      );
  Timer? _clock;
  bool _retired = false, _refreshQueued = false;
  bool get _valid => mounted && !_retired && widget.current();

  @override
  void initState() {
    super.initState();
    widget.city.addListener(_changed);
    widget.identityChanges.addListener(_identityChanged);
    _data.addListener(_changed);
    _retired = !widget.current();
    if (_valid) unawaited(_data.load());
    _clock = Timer.periodic(const Duration(seconds: 1), (_) {
      if (!_valid) _identityChanged();
      _data.expire();
    });
  }

  void _changed() {
    if (!mounted) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      if (_refreshQueued) return;
      _refreshQueued = true;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        _refreshQueued = false;
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  void _identityChanged() {
    _data.sync();
    if (!widget.current()) _retired = true;
    _changed();
  }

  @override
  void didUpdateWidget(_NowScopePicker old) {
    super.didUpdateWidget(old);
    if (old.city != widget.city) {
      old.city.removeListener(_changed);
      widget.city.addListener(_changed);
      _retired = true;
    }
    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges.removeListener(_identityChanged);
      widget.identityChanges.addListener(_identityChanged);
      _retired = true;
    }
    if (old.auth != widget.auth ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.organizationWorkspaceID != widget.organizationWorkspaceID) {
      _retired = true;
    }
    _data.sync();
    if (!widget.current()) _retired = true;
  }

  Future<void> _select() async {
    if (!_valid || !_data.ready) return;
    final generation = _data.generation;
    final result = await _data.resolve();
    if (!_valid ||
        generation != _data.generation ||
        !_data.current ||
        result == null ||
        !result.expiresAt.isAfter(DateTime.now())) {
      return;
    }
    widget.onClose(result);
  }

  @override
  void dispose() {
    _clock?.cancel();
    widget.city.removeListener(_changed);
    widget.identityChanges.removeListener(_identityChanged);
    _data.removeListener(_changed);
    _data.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final selected = _data.selected;
    // Public destinations already offered by the city directory have one row.
    // Declared current/home/past scopes remain distinct, with their real relation.
    final options =
        _data.options?.items.where(
          (option) =>
              option.declared ||
              option.contextType != 'CITY' ||
              !widget.city.cities.any((city) => city.id == option.cityID),
        ) ??
        const <NowContextOption>[];
    return ConstrainedBox(
      key: const Key('now-scope-picker'),
      constraints: BoxConstraints(
        maxHeight: MediaQuery.sizeOf(context).height * .7,
      ),
      child: SafeArea(
        top: false,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 0, 8, 0),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      '选择城市与范围',
                      style: Theme.of(context).textTheme.titleLarge,
                    ),
                  ),
                  IconButton(
                    tooltip: '取消选择城市与范围',
                    constraints: const BoxConstraints(
                      minWidth: 48,
                      minHeight: 48,
                    ),
                    onPressed: () => widget.onClose(null),
                    icon: const Icon(Icons.close),
                  ),
                ],
              ),
            ),
            Flexible(
              fit: FlexFit.loose,
              child: ListView(
                key: const Key('now-scope-options'),
                shrinkWrap: true,
                padding: const EdgeInsets.fromLTRB(16, 8, 16, 16),
                children: [
                  if (!_valid)
                    const Text('账号或工作身份已变化，请关闭后重新选择。')
                  else ...[
                    const Text('使用公开城市目录；选择目的地不代表你的实时位置。'),
                    if (widget.city.citiesLoading)
                      const LinearProgressIndicator(),
                    if (widget.city.cityError != null)
                      Text(widget.city.cityError!),
                    if (!widget.city.citiesLoading &&
                        widget.city.cities.isEmpty) ...[
                      const Text('暂时没有可选择的城市。'),
                      TextButton.icon(
                        style: TextButton.styleFrom(
                          minimumSize: const Size(48, 48),
                        ),
                        onPressed: () {
                          if (_valid) unawaited(widget.reloadCities());
                        },
                        icon: const Icon(Icons.refresh),
                        label: const Text('重新读取城市'),
                      ),
                    ],
                    for (final city in widget.city.cities)
                      ListTile(
                        key: Key('now-scope-city-${city.id}'),
                        minTileHeight: 48,
                        title: Text(city.name),
                        subtitle: city.region.isEmpty
                            ? null
                            : Text(city.region),
                        trailing: city.id == widget.city.selectedCity?.id
                            ? const Icon(Icons.check)
                            : null,
                        onTap: () {
                          if (_valid) widget.onClose(city.id);
                        },
                      ),
                    const Divider(),
                    Text(
                      '线上与其他范围',
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    if (widget.organizationWorkspaceID() != null)
                      const Text('切回个人身份后可选择本人声明的范围。')
                    else if (widget.auth.authorizationHeader == null)
                      const Text('登录后可选择本人声明的线上范围。')
                    else if (!_data.current)
                      const Text('请确认当前登录账号后重新选择。')
                    else ...[
                      const Text('只切换下次查询的范围，保留当前任务和地图；不会新增声明。'),
                      if (_data.busy) const LinearProgressIndicator(),
                      if (_data.options?.items.isEmpty == true)
                        const Text('尚无本人可用范围。可在下方管理自己的声明。'),
                      for (final option in options)
                        ListTile(
                          key: Key('now-scope-option-${option.optionID}'),
                          minTileHeight: 48,
                          selected: selected?.same(option) == true,
                          leading: Icon(
                            option.contextType == 'ONLINE'
                                ? Icons.language
                                : option.contextType == 'CITY'
                                ? Icons.location_city
                                : Icons.person_outline,
                            color: colors.onSurface,
                          ),
                          title: Text(option.label),
                          subtitle: Text(option.description),
                          trailing: selected?.same(option) == true
                              ? const Icon(Icons.check)
                              : null,
                          onTap: _data.ready
                              ? () {
                                  if (_valid) _data.select(option);
                                }
                              : null,
                        ),
                      if (_data.options?.truncated == true)
                        const Text('最多显示100个当前可用选项，不代表完整历史。'),
                      if (selected != null) ...[
                        Text('本次选择：${selected.label}'),
                        if (selected.contextType == 'INSTITUTION' ||
                            selected.contextType == 'COMMUNITY')
                          const Text('本人声明不代表机构或社群成员资格。'),
                        if (selected.queryRoute == 'UNAVAILABLE')
                          const Text('该范围暂不支持公开查询；选择不会读取私密资料。'),
                        FilledButton(
                          key: const Key('now-scope-confirm'),
                          style: FilledButton.styleFrom(
                            minimumSize: const Size(48, 48),
                          ),
                          onPressed: _data.ready ? _select : null,
                          child: const Text('使用此范围'),
                        ),
                      ],
                      if (_data.message != null) Text(_data.message!),
                      TextButton.icon(
                        style: TextButton.styleFrom(
                          minimumSize: const Size(48, 48),
                        ),
                        onPressed: _data.busy
                            ? null
                            : () {
                                if (_valid) unawaited(_data.load());
                              },
                        icon: const Icon(Icons.refresh),
                        label: const Text('重新读取本人范围'),
                      ),
                      TextButton.icon(
                        key: const Key('now-scope-manage'),
                        style: TextButton.styleFrom(
                          minimumSize: const Size(48, 48),
                        ),
                        onPressed: () {
                          if (_valid && _data.current) {
                            widget.onClose(_NowScopeAction.manage);
                          }
                        },
                        icon: const Icon(Icons.edit_outlined),
                        label: const Text('管理我的范围声明'),
                      ),
                    ],
                    TextButton.icon(
                      key: const Key('now-scope-online'),
                      style: TextButton.styleFrom(
                        minimumSize: const Size(48, 48),
                      ),
                      onPressed: () {
                        if (_valid) widget.onClose(_NowScopeAction.online);
                      },
                      icon: const Icon(Icons.language),
                      label: const Text('查找线上活动与伙伴'),
                    ),
                  ],
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _NowOnlineDetail extends StatefulWidget {
  const _NowOnlineDetail({required this.response});
  final NowOnlineResponse response;
  @override
  State<_NowOnlineDetail> createState() => _NowOnlineDetailState();
}

class _NowOnlineDetailState extends State<_NowOnlineDetail> {
  Timer? _expiry;
  bool _expired = false;
  @override
  void initState() {
    super.initState();
    final remaining = widget.response.items.single.expiresAt.difference(
      widget.response.observedAt,
    );
    _expiry = Timer(remaining.isNegative ? Duration.zero : remaining, () {
      if (mounted) setState(() => _expired = true);
    });
  }

  @override
  void dispose() {
    _expiry?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final item = widget.response.items.single;
    return Scaffold(
      appBar: AppBar(title: const Text('公开线上意图')),
      body: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          if (_expired)
            const Text('此意图已到期，请返回重新查询。')
          else ...[
            Text(item.title, style: Theme.of(context).textTheme.headlineSmall),
            const SizedBox(height: 16),
            Text('线上 · ${widget.response.context.label}'),
            const SizedBox(height: 12),
            const Text('这是刚读取的公开标题快照。未发送消息或邀请，也不推断关系或位置；来源变动后请重新打开核实。'),
            Text('有效至 ${item.expiresAt.toLocal()}'),
          ],
        ],
      ),
    );
  }
}
