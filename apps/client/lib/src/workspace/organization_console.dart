import 'dart:async';

import 'package:flutter/material.dart';

import '../app/birdtie_app_bar.dart';
import '../auth/birdtie_auth_controller.dart';
import '../city/public_city_controller.dart';
import 'aberdeen_schedule.dart';
import 'community_api.dart';
import 'organization_activity_api.dart';
import 'organization_faq_page.dart';
import 'organization_analytics_page.dart';
import 'organization_membership_pages.dart';
import 'organization_map_location_page.dart';
import 'organization_workspaces.dart';
import 'entity_cover.dart';
import 'entity_cover_editor.dart';
import 'notification_destination_router.dart';

String _visibilityLabel(String value) => switch (value) {
  'public' => '公开',
  'organizer_members' => '仅主办方成员',
  'invite_only' => '仅受邀者',
  'private' => '私密草稿',
  _ => '仅链接访问草稿',
};

String activityLocationChoice(String modality, String status) =>
    switch ((modality, status)) {
      ('online', _) => 'online',
      ('hybrid', 'confirmed') => 'hybrid_confirmed',
      ('hybrid', 'tbd') => 'hybrid_tbd',
      (_, 'tbd') => 'tbd',
      _ => 'confirmed',
    };

Map<String, dynamic> activityLocationFields(String choice, String? placeID) => {
  'placeId': choice == 'confirmed' || choice == 'hybrid_confirmed'
      ? placeID
      : null,
  'modality': choice == 'online'
      ? 'online'
      : choice.startsWith('hybrid_')
      ? 'hybrid'
      : 'in_person',
  'physicalPlaceStatus': choice == 'online'
      ? 'not_applicable'
      : choice == 'tbd' || choice == 'hybrid_tbd'
      ? 'tbd'
      : 'confirmed',
};

List<ActivityOrganizer> authorizedActivityOrganizers(
  String accountID,
  List<CommunityItem> communities,
  List<OrganizationWorkspace> organizations, {
  List<ActivityOrganizer> businesses = const [],
}) => [
  ActivityOrganizer(type: 'PERSON', id: accountID, name: '我自己'),
  for (final community in communities)
    if (community.managed && community.status == 'active')
      ActivityOrganizer(
        type: 'COMMUNITY',
        id: community.id,
        name: community.name,
      ),
  for (final org in organizations)
    if (org.role == 'owner' || org.role == 'admin')
      ActivityOrganizer(type: 'ORGANIZATION', id: org.id, name: org.name),
  for (final business in businesses)
    if (business.type == 'BUSINESS') business,
];

class OrganizationConsolePage extends StatefulWidget {
  const OrganizationConsolePage({
    super.key,
    required this.auth,
    required this.city,
    required this.organizations,
    this.api,
  });

  final BirdtieAuthController auth;
  final PublicCityController city;
  final OrganizationWorkspaceController organizations;
  final OrganizationActivityApi? api;

  @override
  State<OrganizationConsolePage> createState() =>
      _OrganizationConsolePageState();
}

class _OrganizationConsolePageState extends State<OrganizationConsolePage> {
  late OrganizationActivityApi _api;
  List<OrganizationActivity> _activities = const [];
  bool _loading = true;
  String? _error;
  String? _workingId;
  int _coverEpoch = 0;
  int _humanEpoch = 0;
  Object? _humanSeen;
  Object get _humanIdentity => (
    widget.auth,
    widget.organizations,
    widget.auth.accountID,
    widget.organizations.active?.id,
    widget.organizations.active?.role,
    widget.api,
  );
  Object? _coverSeen;
  Object get _coverIdentity => (
    widget.auth,
    widget.organizations,
    widget.auth.accountID,
    widget.auth.authorizationHeader,
    widget.organizations.active?.id,
    widget.organizations.active?.role,
    widget.api,
  );
  void _observeCover() {
    var humanChanged = false;
    if (_humanSeen != _humanIdentity) {
      _humanSeen = _humanIdentity;
      ++_humanEpoch;
      humanChanged = true;
      _activities = const [];
      _error = null;
      _workingId = null;
    }
    if (_coverSeen == _coverIdentity) return;
    _coverSeen = _coverIdentity;
    _humanSeen = _humanIdentity;
    ++_coverEpoch;
    if (mounted) setState(() {});
    if (humanChanged && mounted) unawaited(_load());
  }

  @override
  void didUpdateWidget(OrganizationConsolePage old) {
    super.didUpdateWidget(old);
    if (old.api != widget.api || old.auth != widget.auth) {
      if (old.api == null) _api.dispose();
      _api =
          widget.api ??
          OrganizationActivityApi(
            authorizationHeader: () => widget.auth.authorizationHeader,
          );
    }
    if (old.auth != widget.auth) {
      old.auth.removeListener(_observeCover);
      widget.auth.addListener(_observeCover);
    }
    if (old.organizations != widget.organizations) {
      old.organizations.removeListener(_observeCover);
      widget.organizations.addListener(_observeCover);
    }
    _observeCover();
  }

  OrganizationWorkspace? get _organization => widget.organizations.active;
  bool get _canManage =>
      widget.auth.signedIn &&
      _api.authorizationHeader() == widget.auth.authorizationHeader &&
      _organization != null &&
      (_organization!.role == 'owner' || _organization!.role == 'admin');

  @override
  void initState() {
    super.initState();
    _api =
        widget.api ??
        OrganizationActivityApi(
          authorizationHeader: () => widget.auth.authorizationHeader,
        );
    _coverSeen = _coverIdentity;
    _humanSeen = _humanIdentity;
    widget.auth.addListener(_observeCover);
    widget.organizations.addListener(_observeCover);
    unawaited(_load());
  }

  @override
  void dispose() {
    ++_humanEpoch;
    ++_coverEpoch;
    widget.auth.removeListener(_observeCover);
    widget.organizations.removeListener(_observeCover);
    if (widget.api == null) _api.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    final org = _organization, api = _api;
    final epoch = _humanEpoch, identity = _humanIdentity;
    bool current() =>
        mounted &&
        epoch == _humanEpoch &&
        identity == _humanIdentity &&
        identical(api, _api);
    if (org == null || !_canManage) {
      setState(() => _loading = false);
      return;
    }
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final items = await api.list(org.id);
      if (current()) setState(() => _activities = items);
    } catch (error) {
      if (current()) setState(() => _error = _message(error));
    } finally {
      if (current()) setState(() => _loading = false);
    }
  }

  String _message(Object error) => error is OrganizationActivityException
      ? error.chineseMessage
      : '连接失败，请检查网络后重试。';

  Future<void> _edit([OrganizationActivity? activity]) async {
    final org = _organization, api = _api;
    final epoch = _humanEpoch, identity = _humanIdentity;
    bool current() =>
        mounted &&
        _canManage &&
        epoch == _humanEpoch &&
        identity == _humanIdentity &&
        identical(api, _api);
    if (org == null || !current()) return;
    final changed = await Navigator.push<bool>(
      context,
      MaterialPageRoute(
        builder: (_) => NotificationDestinationBoundary(
          identityChanges: Listenable.merge([
            widget.auth,
            widget.organizations,
          ]),
          current: current,
          builder: (_) => ActivityEditorPage(
            api: api,
            auth: widget.auth,
            organizations: widget.organizations,
            initialOrganizer: ActivityOrganizer(
              type: 'ORGANIZATION',
              id: org.id,
              name: org.name,
            ),
            city: widget.city,
            existing: activity,
          ),
        ),
      ),
    );
    if (changed == true && current()) {
      await _load();
      unawaited(widget.city.loadActivities());
    }
  }

  Future<void> _editOrganizationCover() async {
    final org = _organization, owner = widget.auth.accountID;
    if (!_canManage || org == null || owner == null) return;
    final epoch = _coverEpoch, binding = _coverIdentity;
    bool current() =>
        mounted &&
        epoch == _coverEpoch &&
        binding == _coverIdentity &&
        _canManage;
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => EntityCoverEditor(
          ref: EntityCoverRef('organization', org.id),
          name: org.name,
          ownerID: owner,
          api: EntityCoverAPI(
            client: _api.coverClient,
            baseUrl: _api.coverBaseUrl,
            authorizationHeader: () => widget.auth.authorizationHeader,
            current: current,
          ),
          identityChanges: Listenable.merge([
            widget.auth,
            widget.organizations,
          ]),
          current: current,
        ),
      ),
    );
  }

  Future<void> _stateAction(
    OrganizationActivity activity,
    String action,
  ) async {
    final org = _organization, api = _api;
    final epoch = _humanEpoch, identity = _humanIdentity;
    bool current() =>
        mounted &&
        _canManage &&
        epoch == _humanEpoch &&
        identity == _humanIdentity &&
        identical(api, _api);
    if (org == null || _workingId != null || !current()) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AnimatedBuilder(
        animation: Listenable.merge([widget.auth, widget.organizations]),
        builder: (context, _) => !current()
            ? AlertDialog(
                title: const Text('请重新打开操作'),
                content: const Text('身份或组织已变化，请返回当前入口重新核实。'),
                actions: [
                  TextButton(
                    onPressed: () => Navigator.pop(context, false),
                    child: const Text('返回'),
                  ),
                ],
              )
            : AlertDialog(
                title: Text(action == 'publish' ? '确认发布活动' : '确认取消活动'),
                content: Text(
                  action == 'publish'
                      ? activity.visibility == 'public'
                            ? '“${activity.title}”发布后会出现在公开活动和智能体搜索中。'
                            : '“${activity.title}”发布后可见范围：${_visibilityLabel(activity.visibility)}。'
                      : '取消后活动仍可查看，但不能再报名。',
                ),
                actions: [
                  TextButton(
                    onPressed: () => Navigator.pop(context, false),
                    child: const Text('返回'),
                  ),
                  FilledButton(
                    onPressed: () {
                      if (current() &&
                          context.mounted &&
                          ModalRoute.of(context)?.isActive == true) {
                        Navigator.pop(context, true);
                      }
                    },
                    child: Text(action == 'publish' ? '确认发布' : '确认取消'),
                  ),
                ],
              ),
      ),
    );
    if (confirmed != true || !current()) return;
    setState(() => _workingId = activity.id);
    try {
      if (action == 'publish') {
        await api.publish(org.id, activity.id);
      } else {
        await api.cancel(org.id, activity.id);
      }
      if (current()) {
        await _load();
        if (current()) unawaited(widget.city.loadActivities());
      }
    } catch (error) {
      if (current()) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(_message(error))));
      }
    } finally {
      if (current()) setState(() => _workingId = null);
    }
  }

  @override
  Widget build(BuildContext context) {
    final org = _organization;
    if (org == null) return const Center(child: Text('请先在侧边栏选择组织工作区。'));
    if (!_canManage) return const Center(child: Text('只有组织所有者或管理员可以管理活动。'));
    final coverEpoch = _coverEpoch, coverIdentity = _coverIdentity;
    final humanEpoch = _humanEpoch, humanIdentity = _humanIdentity;
    bool humanCurrent() =>
        mounted &&
        _canManage &&
        humanEpoch == _humanEpoch &&
        humanIdentity == _humanIdentity;
    bool coverCurrent() =>
        mounted &&
        _canManage &&
        coverEpoch == _coverEpoch &&
        coverIdentity == _coverIdentity;
    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(20, 18, 20, 40),
        children: [
          Text(org.name, style: Theme.of(context).textTheme.headlineSmall),
          const SizedBox(height: 6),
          const Text('组织活动管理 · 活动按所选可见范围发布'),
          const SizedBox(height: 18),
          FilledButton.icon(
            key: const Key('organization_create_activity'),
            onPressed: widget.city.selectedCity?.id == 'aberdeen-gb'
                ? () => _edit()
                : null,
            icon: const Icon(Icons.add),
            label: const Text('发布新活动'),
          ),
          if (widget.city.selectedCity?.id != 'aberdeen-gb')
            const Padding(
              padding: EdgeInsets.only(top: 8),
              child: Text('目前仅开放阿伯丁组织活动发布。'),
            ),
          const SizedBox(height: 8),
          OutlinedButton.icon(
            key: const Key('organization_cover_editor'),
            onPressed: _editOrganizationCover,
            icon: const Icon(Icons.image_outlined),
            label: const Text('管理组织封面'),
          ),
          const SizedBox(height: 8),
          OutlinedButton.icon(
            key: const Key('organization_map_location'),
            onPressed: () async {
              if (!humanCurrent()) return;
              await Navigator.of(context).push(
                MaterialPageRoute<void>(
                  builder: (_) => OrganizationMapLocationPage(
                    organizationID: org.id,
                    cityID: widget.city.selectedCity?.id ?? '',
                    authorizationHeader: () => widget.auth.authorizationHeader,
                    ownerID: () =>
                        humanCurrent() ? widget.auth.accountID : null,
                    identityChanges: Listenable.merge([
                      widget.auth,
                      widget.organizations,
                      widget.city,
                    ]),
                    sourceCurrent: humanCurrent,
                    citySource: () => widget.city.selectedCity,
                    client: _api.coverClient,
                    apiBaseUrl: _api.coverBaseUrl,
                  ),
                ),
              );
              if (humanCurrent()) await widget.city.loadOrganizationPins();
            },
            icon: const Icon(Icons.location_on_outlined),
            label: const Text('管理组织地图位置'),
          ),
          const SizedBox(height: 8),
          OutlinedButton.icon(
            key: const Key('organization_manage_members'),
            onPressed: () async {
              if (!humanCurrent()) return;
              await Navigator.of(context).push(
                MaterialPageRoute<void>(
                  builder: (context) => OrganizationMembersPage(
                    organizationID: org.id,
                    organizationName: org.name,
                    currentRole: org.role,
                    authorizationHeader: () => widget.auth.authorizationHeader,
                    ownerID: () =>
                        humanCurrent() ? widget.auth.accountID : null,
                    identityChanges: Listenable.merge([
                      widget.auth,
                      widget.organizations,
                    ]),
                    sourceCurrent: humanCurrent,
                    client: _api.coverClient,
                    apiBaseUrl: _api.coverBaseUrl,
                  ),
                ),
              );
              if (humanCurrent()) await widget.organizations.load();
              if (mounted) setState(() {});
            },
            icon: const Icon(Icons.people_outline),
            label: const Text('管理组织成员'),
          ),
          const SizedBox(height: 8),
          OutlinedButton.icon(
            onPressed: () {
              if (!humanCurrent()) return;
              Navigator.of(context).push<void>(
                MaterialPageRoute<void>(
                  builder: (_) => NotificationDestinationBoundary(
                    identityChanges: Listenable.merge([
                      widget.auth,
                      widget.organizations,
                    ]),
                    current: humanCurrent,
                    builder: (_) => OrganizationFAQPage(
                      organizationID: org.id,
                      authorizationHeader: () =>
                          widget.auth.authorizationHeader,
                      ownerID: () =>
                          humanCurrent() ? widget.auth.accountID : null,
                      sourceCurrent: humanCurrent,
                      identityChanges: Listenable.merge([
                        widget.auth,
                        widget.organizations,
                      ]),
                      client: _api.coverClient,
                      apiBaseUrl: _api.coverBaseUrl,
                    ),
                  ),
                ),
              );
            },
            icon: const Icon(Icons.question_answer_outlined),
            label: const Text('管理组织常见问答'),
          ),
          const SizedBox(height: 8),
          OutlinedButton.icon(
            onPressed: () => Navigator.of(context).push(
              MaterialPageRoute<void>(
                builder: (context) => OrganizationAnalyticsPage(
                  organizationID: org.id,
                  authorizationHeader: () => widget.auth.authorizationHeader,
                ),
              ),
            ),
            icon: const Icon(Icons.bar_chart_outlined),
            label: const Text('查看活动数据'),
          ),
          const SizedBox(height: 20),
          if (_loading) const Center(child: CircularProgressIndicator()),
          if (_error != null) ...[
            Text(
              _error!,
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                color: Theme.of(context).colorScheme.error,
              ),
            ),
            TextButton(onPressed: _load, child: const Text('重试')),
          ],
          if (!_loading && _error == null && _activities.isEmpty)
            const Text('还没有组织活动。创建草稿后可预览并发布。'),
          for (final activity in _activities)
            Card(
              margin: const EdgeInsets.only(bottom: 12),
              child: Padding(
                padding: const EdgeInsets.all(16),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    if (activity.cover != null &&
                        activity.cover!.ref ==
                            EntityCoverRef('activity', activity.id))
                      EntityCover(
                        key: ObjectKey(activity),
                        metadata: activity.cover,
                        api: EntityCoverAPI(
                          client: _api.coverClient,
                          baseUrl: _api.coverBaseUrl,
                          authorizationHeader: () =>
                              widget.auth.authorizationHeader,
                          current: coverCurrent,
                        ),
                        identityChanges: Listenable.merge([
                          widget.auth,
                          widget.organizations,
                        ]),
                        current: coverCurrent,
                        width: 320,
                        height: 180,
                        label: '${activity.title}的活动封面',
                      ),
                    Text(
                      activity.title,
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    const SizedBox(height: 6),
                    Text(_status(activity)),
                    const SizedBox(height: 10),
                    Wrap(
                      spacing: 8,
                      children: [
                        if (activity.cancelledAt == null)
                          OutlinedButton(
                            onPressed: _workingId == null
                                ? () => _edit(activity)
                                : null,
                            child: const Text('编辑'),
                          ),
                        if (activity.publicationStatus == 'draft')
                          FilledButton(
                            onPressed: _workingId == null
                                ? () => _stateAction(activity, 'publish')
                                : null,
                            child: const Text('发布'),
                          ),
                        if (activity.publicationStatus == 'published' &&
                            activity.cancelledAt == null)
                          TextButton(
                            onPressed: _workingId == null
                                ? () => _stateAction(activity, 'cancel')
                                : null,
                            child: const Text('取消活动'),
                          ),
                      ],
                    ),
                  ],
                ),
              ),
            ),
        ],
      ),
    );
  }

  String _status(OrganizationActivity activity) {
    if (activity.cancelledAt != null) return '已取消';
    if (activity.publicationStatus == 'draft') return '草稿 · 仅管理员可见';
    if (activity.endsAt.isBefore(DateTime.now())) return '已结束';
    return '已发布 · ${_visibilityLabel(activity.visibility)}';
  }
}

class ActivityEditorPage extends StatefulWidget {
  const ActivityEditorPage({
    super.key,
    required this.api,
    required this.auth,
    required this.organizations,
    required this.city,
    this.initialOrganizer,
    this.existing,
  });
  final OrganizationActivityApi api;
  final BirdtieAuthController auth;
  final OrganizationWorkspaceController organizations;
  final PublicCityController city;
  final ActivityOrganizer? initialOrganizer;
  final OrganizationActivity? existing;

  @override
  State<ActivityEditorPage> createState() => _ActivityEditorPageState();
}

class _ActivityEditorPageState extends State<ActivityEditorPage> {
  final _formKey = GlobalKey<FormState>();
  late final TextEditingController _title;
  late final TextEditingController _description;
  late final TextEditingController _capacity;
  late DateTime _startDate;
  late DateTime _endDate;
  late TimeOfDay _startTime;
  late TimeOfDay _endTime;
  String? _placeId;
  String _locationChoice = 'confirmed';
  String _category = 'badminton';
  String _visibility = 'public';
  String? _error;
  bool _busy = false;
  bool _organizersLoading = true;
  List<ActivityOrganizer> _organizers = const [];
  ActivityOrganizer? _organizer;
  int _coverEpoch = 0;
  Object? _coverSeen;
  Object get _coverIdentity => (
    widget.auth,
    widget.organizations,
    widget.auth.accountID,
    widget.auth.authorizationHeader,
    widget.organizations.active?.id,
    widget.organizations.active?.role,
    widget.api,
    widget.existing?.id,
    widget.existing?.organizer?.type,
    widget.existing?.organizer?.id,
  );
  void _observeCover() {
    if (_coverSeen == _coverIdentity) return;
    _coverSeen = _coverIdentity;
    ++_coverEpoch;
    if (mounted) setState(() {});
  }

  @override
  void didUpdateWidget(ActivityEditorPage old) {
    super.didUpdateWidget(old);
    if (old.auth != widget.auth) {
      old.auth.removeListener(_observeCover);
      widget.auth.addListener(_observeCover);
    }
    if (old.organizations != widget.organizations) {
      old.organizations.removeListener(_observeCover);
      widget.organizations.addListener(_observeCover);
    }
    _observeCover();
  }

  bool get _canEditCover {
    final organizer = widget.existing?.organizer;
    return !_busy &&
        !_organizersLoading &&
        organizer != null &&
        _organizers.any(
          (o) => o.type == organizer.type && o.id == organizer.id,
        );
  }

  @override
  void initState() {
    super.initState();
    _coverSeen = _coverIdentity;
    widget.auth.addListener(_observeCover);
    widget.organizations.addListener(_observeCover);
    final existing = widget.existing;
    final tomorrow = DateTime.now().add(const Duration(days: 1));
    final start = existing == null
        ? null
        : utcToAberdeenLocal(existing.startsAt);
    final end = existing == null ? null : utcToAberdeenLocal(existing.endsAt);
    _startDate = start ?? tomorrow;
    _endDate = end ?? tomorrow;
    _startTime = start == null
        ? const TimeOfDay(hour: 18, minute: 0)
        : TimeOfDay(hour: start.hour, minute: start.minute);
    _endTime = end == null
        ? const TimeOfDay(hour: 20, minute: 0)
        : TimeOfDay(hour: end.hour, minute: end.minute);
    _title = TextEditingController(text: existing?.title ?? '');
    _description = TextEditingController(
      text: existing?.description.isNotEmpty == true
          ? existing!.description
          : existing?.summary ?? '',
    );
    _capacity = TextEditingController(
      text: existing?.capacity?.toString() ?? '',
    );
    _placeId = existing?.placeId;
    _locationChoice = existing == null
        ? 'confirmed'
        : activityLocationChoice(
            existing.modality,
            existing.physicalPlaceStatus,
          );
    _category = existing?.categoryCode ?? 'badminton';
    _visibility = existing?.visibility ?? 'public';
    _organizer = existing?.organizer ?? widget.initialOrganizer;
    _loadOrganizers();
  }

  Future<void> _loadOrganizers() async {
    final accountID = widget.auth.accountID;
    final coverEpoch = _coverEpoch, coverBinding = _coverIdentity;
    if (accountID == null) {
      if (mounted) {
        setState(() {
          _organizersLoading = false;
          _error = '请先登录，再发起活动。';
        });
      }
      return;
    }
    final api = CommunityApi(
      authorizationHeader: () => widget.auth.authorizationHeader,
      client: widget.api.coverClient,
      apiBaseUrl: widget.api.coverBaseUrl,
    );
    try {
      await widget.organizations.load();
      final communities = await api.mine();
      final businesses = await widget.api.managedBusinessOrganizers();
      if (!mounted ||
          coverEpoch != _coverEpoch ||
          coverBinding != _coverIdentity)
        return;
      final options = authorizedActivityOrganizers(
        accountID,
        communities,
        widget.organizations.organizations,
        businesses: businesses,
      );
      setState(() {
        _organizers = options;
        final intended = _organizer;
        _organizer = options.firstWhere(
          (option) =>
              option.type == intended?.type && option.id == intended?.id,
          orElse: () => options.first,
        );
        _organizersLoading = false;
      });
    } catch (_) {
      if (mounted &&
          coverEpoch == _coverEpoch &&
          coverBinding == _coverIdentity) {
        setState(() {
          _organizersLoading = false;
          _error = '主办方权限暂不可核对，请重试。';
        });
      }
    } finally {
      api.dispose();
    }
  }

  @override
  void dispose() {
    ++_coverEpoch;
    widget.auth.removeListener(_observeCover);
    widget.organizations.removeListener(_observeCover);
    _title.dispose();
    _description.dispose();
    _capacity.dispose();
    super.dispose();
  }

  String _dateLabel(DateTime date) =>
      MaterialLocalizations.of(context).formatMediumDate(date);
  Future<void> _editCover() async {
    final activity = widget.existing, owner = widget.auth.accountID;
    final organizer = activity?.organizer;
    if (activity == null ||
        owner == null ||
        organizer == null ||
        _busy ||
        _organizersLoading ||
        !_organizers.any(
          (o) => o.type == organizer.type && o.id == organizer.id,
        ))
      return;
    final epoch = _coverEpoch, binding = _coverIdentity;
    bool current() =>
        mounted &&
        epoch == _coverEpoch &&
        binding == _coverIdentity &&
        widget.auth.signedIn &&
        _organizers.any(
          (o) => o.type == organizer.type && o.id == organizer.id,
        );
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => EntityCoverEditor(
          ref: EntityCoverRef('activity', activity.id),
          name: activity.title,
          ownerID: owner,
          api: EntityCoverAPI(
            client: widget.api.coverClient,
            baseUrl: widget.api.coverBaseUrl,
            authorizationHeader: () => widget.auth.authorizationHeader,
            current: current,
          ),
          identityChanges: Listenable.merge([
            widget.auth,
            widget.organizations,
          ]),
          current: current,
        ),
      ),
    );
  }

  Future<void> _pickDate(bool end) async {
    final picked = await showDatePicker(
      context: context,
      initialDate: end ? _endDate : _startDate,
      firstDate: DateTime.now().subtract(const Duration(days: 1)),
      lastDate: DateTime.now().add(const Duration(days: 365)),
    );
    if (picked != null) {
      setState(() {
        if (end) {
          _endDate = picked;
        } else {
          _startDate = picked;
        }
      });
    }
  }

  Future<void> _pickTime(bool end) async {
    final picked = await showTimePicker(
      context: context,
      initialTime: end ? _endTime : _startTime,
    );
    if (picked != null) {
      setState(() {
        if (end) {
          _endTime = picked;
        } else {
          _startTime = picked;
        }
      });
    }
  }

  Future<void> _save(bool publish) async {
    if (_busy ||
        _organizersLoading ||
        _organizer == null ||
        !_formKey.currentState!.validate()) {
      return;
    }
    final city = widget.city.selectedCity;
    final start = aberdeenLocalToUtc(
      _startDate,
      _startTime.hour,
      _startTime.minute,
    );
    final end = aberdeenLocalToUtc(_endDate, _endTime.hour, _endTime.minute);
    if (city == null ||
        city.id != 'aberdeen-gb' ||
        start == null ||
        end == null) {
      setState(() => _error = '请检查日期和英国夏令时时间；切换日的 01:00 不可选。');
      return;
    }
    if (!end.isAfter(start) ||
        end.difference(start) > const Duration(days: 7)) {
      setState(() => _error = '结束时间须晚于开始时间，且活动不能超过 7 天。');
      return;
    }
    if (publish &&
        (!start.isAfter(DateTime.now()) ||
            _visibility == 'private' ||
            _visibility == 'unlisted')) {
      setState(() => _error = '只有未来开始的公开、成员或邀请活动可以发布。');
      return;
    }
    if (_organizer!.type == 'PERSON' && _visibility == 'organizer_members') {
      setState(() => _error = '个人活动不能设置为仅成员可见。');
      return;
    }
    if ((_locationChoice == 'confirmed' ||
            _locationChoice == 'hybrid_confirmed') &&
        !widget.city.places.any((place) => place.id == _placeId)) {
      setState(() => _error = '请先选择已公开地点，或将地点设为待定。');
      return;
    }
    if (publish) {
      final confirmed = await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          title: const Text('预览并确认发布'),
          content: Text(
            '${_title.text.trim()}\n'
            '${_dateLabel(_startDate)} ${_startTime.format(context)}（阿伯丁时间）\n'
            '${_locationChoice == 'online'
                ? '线上活动'
                : _locationChoice == 'tbd' || _locationChoice == 'hybrid_tbd'
                ? '${_locationChoice == 'hybrid_tbd' ? '线上＋线下 · ' : ''}地点待定'
                : '${_locationChoice == 'hybrid_confirmed' ? '线上＋线下 · ' : ''}${widget.city.places.firstWhere((p) => p.id == _placeId).name}'}\n\n'
            '主办方：${_organizer!.label}\n'
            '可见范围：${_visibilityLabel(_visibility)}',
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: const Text('继续编辑'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('确认发布'),
            ),
          ],
        ),
      );
      if (confirmed != true || !mounted) return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final details = _description.text.trim();
      final payload = <String, dynamic>{
        'cityId': city.id,
        ...activityLocationFields(_locationChoice, _placeId),
        'title': _title.text.trim(),
        'summary': details.length > 300 ? details.substring(0, 300) : details,
        'description': details,
        'startsAt': start.toIso8601String(),
        'endsAt': end.toIso8601String(),
        'timeZone': city.timeZone,
        'categoryCode': _category,
        'capacity': _capacity.text.trim().isEmpty
            ? null
            : int.parse(_capacity.text.trim()),
        'priceMinor': widget.existing?.priceMinor ?? 0,
        'currency': widget.existing?.currency ?? '',
        'eligibility': widget.existing?.eligibility ?? '',
        'languageCode': widget.existing?.languageCode ?? '',
        'visibility': _visibility,
      };
      final saved = await widget.api.saveAs(
        _organizer!,
        payload,
        activityId: widget.existing?.id,
      );
      if (publish && saved.publicationStatus == 'draft') {
        await widget.api.publishMine(saved.id);
      }
      if (mounted) Navigator.pop(context, true);
    } catch (error) {
      if (mounted) {
        setState(
          () => _error = error is OrganizationActivityException
              ? error.chineseMessage
              : '保存失败，请检查网络后重试。',
        );
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final places = widget.city.places;
    final selectedPlace = places.any((p) => p.id == _placeId) ? _placeId : null;
    return Scaffold(
      appBar: birdtieAppBar(
        context,
        title: Text(widget.existing == null ? '创建活动' : '编辑活动'),
      ),
      body: Form(
        key: _formKey,
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            if (widget.existing != null) ...[
              OutlinedButton.icon(
                key: const Key('activity_cover_editor'),
                onPressed: _canEditCover ? _editCover : null,
                icon: const Icon(Icons.image_outlined),
                label: const Text('管理活动封面'),
              ),
              if (!_organizersLoading && !_canEditCover)
                const Text('当前主办方权限尚不可核对，暂不能编辑封面。'),
              const SizedBox(height: 16),
            ],
            const Text('活动时间均按阿伯丁当地时间填写。'),
            const SizedBox(height: 16),
            if (_organizersLoading) const LinearProgressIndicator(),
            if (!_organizersLoading)
              DropdownButtonFormField<String>(
                key: const Key('activity_organizer'),
                isExpanded: true,
                initialValue: _organizer == null
                    ? null
                    : '${_organizer!.type}:${_organizer!.id}',
                decoration: const InputDecoration(
                  labelText: '以谁的名义主办 *',
                  border: OutlineInputBorder(),
                ),
                items: [
                  for (final option in _organizers)
                    DropdownMenuItem(
                      value: '${option.type}:${option.id}',
                      child: Text(
                        option.label,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                ],
                onChanged: widget.existing != null
                    ? null
                    : (value) {
                        if (value == null) return;
                        setState(() {
                          _organizer = _organizers.firstWhere(
                            (item) => '${item.type}:${item.id}' == value,
                          );
                          if (_organizer!.type == 'PERSON' &&
                              _visibility == 'organizer_members') {
                            _visibility = 'public';
                          }
                        });
                      },
              ),
            const SizedBox(height: 16),
            TextFormField(
              key: const Key('activity_title'),
              controller: _title,
              maxLength: 160,
              decoration: const InputDecoration(
                labelText: '活动标题 *',
                border: OutlineInputBorder(),
              ),
              validator: (value) => (value?.trim().runes.length ?? 0) < 2
                  ? '请填写至少 2 个字的标题'
                  : null,
            ),
            const SizedBox(height: 12),
            DropdownButtonFormField<String>(
              key: const Key('activity_location_choice'),
              initialValue: _locationChoice,
              decoration: const InputDecoration(
                labelText: '活动形式 *',
                border: OutlineInputBorder(),
              ),
              items: const [
                DropdownMenuItem(value: 'confirmed', child: Text('线下 · 地点已确认')),
                DropdownMenuItem(value: 'tbd', child: Text('线下 · 地点待定')),
                DropdownMenuItem(value: 'online', child: Text('线上活动')),
                DropdownMenuItem(
                  value: 'hybrid_confirmed',
                  child: Text('线上＋线下 · 地点已确认'),
                ),
                DropdownMenuItem(
                  value: 'hybrid_tbd',
                  child: Text('线上＋线下 · 地点待定'),
                ),
              ],
              onChanged: (value) => setState(() {
                _locationChoice = value ?? 'confirmed';
                if (_locationChoice != 'confirmed' &&
                    _locationChoice != 'hybrid_confirmed') {
                  _placeId = null;
                }
              }),
            ),
            const SizedBox(height: 12),
            if (_locationChoice == 'confirmed' ||
                _locationChoice == 'hybrid_confirmed')
              DropdownButtonFormField<String>(
                key: const Key('activity_place'),
                isExpanded: true,
                initialValue: selectedPlace,
                decoration: const InputDecoration(
                  labelText: '活动地点 *',
                  border: OutlineInputBorder(),
                ),
                items: [
                  for (final p in places)
                    DropdownMenuItem(
                      value: p.id,
                      child: Text(
                        p.name,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                ],
                onChanged: (value) => setState(() => _placeId = value),
                validator: (value) => value == null ? '请选择已有公开地点' : null,
              ),
            if (places.isEmpty &&
                (_locationChoice == 'confirmed' ||
                    _locationChoice == 'hybrid_confirmed'))
              const Padding(
                padding: EdgeInsets.only(top: 6),
                child: Text('当前城市没有可选地点，请刷新城市地点后再试。'),
              ),
            const SizedBox(height: 12),
            DropdownButtonFormField<String>(
              initialValue: _category,
              decoration: const InputDecoration(
                labelText: '活动分类 *',
                border: OutlineInputBorder(),
              ),
              items: [
                const DropdownMenuItem(value: 'badminton', child: Text('羽毛球')),
                const DropdownMenuItem(value: 'sports', child: Text('运动')),
                const DropdownMenuItem(value: 'student', child: Text('学生社交')),
                const DropdownMenuItem(value: 'culture', child: Text('文化')),
                const DropdownMenuItem(value: 'other', child: Text('其他')),
                if (!const [
                  'badminton',
                  'sports',
                  'student',
                  'culture',
                  'other',
                ].contains(_category))
                  DropdownMenuItem(
                    value: _category,
                    child: Text('其他（$_category）'),
                  ),
              ],
              onChanged: (value) =>
                  setState(() => _category = value ?? 'other'),
            ),
            const SizedBox(height: 12),
            TextFormField(
              key: const Key('activity_description'),
              controller: _description,
              minLines: 3,
              maxLines: 6,
              maxLength: 3000,
              decoration: const InputDecoration(
                labelText: '活动介绍 *',
                border: OutlineInputBorder(),
              ),
              validator: (value) =>
                  (value?.trim().isEmpty ?? true) ? '请填写活动介绍' : null,
            ),
            const SizedBox(height: 8),
            _scheduleRow('开始', _startDate, _startTime, false),
            _scheduleRow('结束', _endDate, _endTime, true),
            const SizedBox(height: 12),
            TextFormField(
              controller: _capacity,
              keyboardType: TextInputType.number,
              decoration: const InputDecoration(
                labelText: '人数上限（可选）',
                border: OutlineInputBorder(),
              ),
              validator: (value) {
                if (value == null || value.trim().isEmpty) return null;
                final count = int.tryParse(value.trim());
                return count == null || count < 1 || count > 100000
                    ? '请输入 1 至 100000 的人数'
                    : null;
              },
            ),
            const SizedBox(height: 12),
            DropdownButtonFormField<String>(
              initialValue: _visibility,
              decoration: const InputDecoration(
                labelText: '可见范围 *',
                border: OutlineInputBorder(),
              ),
              items: [
                const DropdownMenuItem(value: 'public', child: Text('公开')),
                if (_organizer?.type != 'PERSON')
                  const DropdownMenuItem(
                    value: 'organizer_members',
                    child: Text('仅主办方成员'),
                  ),
                const DropdownMenuItem(
                  value: 'invite_only',
                  child: Text('仅受邀者'),
                ),
                const DropdownMenuItem(value: 'private', child: Text('仅草稿保存')),
                const DropdownMenuItem(
                  value: 'unlisted',
                  child: Text('仅链接访问（草稿）'),
                ),
              ],
              onChanged: widget.existing?.publicationStatus == 'published'
                  ? null
                  : (value) => setState(() => _visibility = value ?? 'public'),
            ),
            if (_error != null)
              Padding(
                padding: const EdgeInsets.only(top: 12),
                child: Text(
                  _error!,
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
              ),
            const SizedBox(height: 22),
            OutlinedButton(
              onPressed: _busy || _organizersLoading || _organizer == null
                  ? null
                  : () => _save(false),
              child: Text(
                widget.existing?.publicationStatus == 'published'
                    ? '保存修改'
                    : '保存草稿',
              ),
            ),
            if (widget.existing?.publicationStatus != 'published') ...[
              const SizedBox(height: 8),
              FilledButton(
                key: const Key('activity_publish'),
                onPressed: _busy || _organizersLoading || _organizer == null
                    ? null
                    : () => _save(true),
                child: const Text('预览并发布'),
              ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _scheduleRow(String label, DateTime date, TimeOfDay time, bool end) =>
      Padding(
        padding: const EdgeInsets.only(bottom: 6),
        child: Row(
          children: [
            SizedBox(width: 42, child: Text(label)),
            Expanded(
              child: OutlinedButton(
                onPressed: () => _pickDate(end),
                child: Text(_dateLabel(date)),
              ),
            ),
            const SizedBox(width: 8),
            Expanded(
              child: OutlinedButton(
                onPressed: () => _pickTime(end),
                child: Text(time.format(context)),
              ),
            ),
          ],
        ),
      );
}
