import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import 'activity_participations.dart';
import 'saved_items.dart';
import '../config/birdtie_environment.dart';
import '../auth/birdtie_auth_controller.dart';
import '../auth/birdtie_login_sheet.dart';
import '../auth/account_access_notice.dart';
import '../city/public_city_controller.dart';
import 'organization_activity_api.dart';
import 'organization_console.dart';
import 'organization_workspaces.dart';
import 'chat_entity_router.dart';
import 'entity_share_pending_store.dart' show chatEntityUUID;
import 'package:flutter/scheduler.dart';

class ActivityPlan {
  const ActivityPlan({
    required this.id,
    required this.activityId,
    required this.title,
    required this.cityId,
    required this.status,
    required this.available,
    required this.startsAt,
    this.endsAt,
    this.modality = 'unspecified',
    this.physicalPlaceStatus = 'unknown',
    this.placeId = '',
    this.placeName = '',
    this.venuePlaceId = '',
    this.timeZone = '',
  });

  final String id;
  final String activityId;
  final String title;
  final String cityId;
  final String status;
  final bool available;
  final DateTime? startsAt;
  final DateTime? endsAt;
  final String modality,
      physicalPlaceStatus,
      placeId,
      placeName,
      venuePlaceId,
      timeZone;

  factory ActivityPlan.fromJson(Map<String, dynamic> json) => ActivityPlan(
    id: json['id'] as String,
    activityId: json['activityId'] as String,
    title: json['title'] as String? ?? '',
    cityId: json['cityId'] as String? ?? '',
    status: json['status'] as String? ?? 'unavailable',
    available: json['available'] as bool? ?? false,
    startsAt: plansNullableTime(json['startsAt']),
    endsAt: plansNullableTime(json['endsAt']),
    modality: json['modality'] as String? ?? 'unspecified',
    physicalPlaceStatus: json['physicalPlaceStatus'] as String? ?? 'unknown',
    placeId: json['placeId'] as String? ?? '',
    placeName: json['placeName'] as String? ?? '',
    venuePlaceId: json['venuePlaceId'] as String? ?? '',
    timeZone: json['timeZone'] as String? ?? '',
  );
}

class ActivityPlansController extends ChangeNotifier {
  ActivityPlansController({
    required this.authorizationHeader,
    http.Client? client,
    String? apiBaseUrl,
    this.identityChanges,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _apiBaseUrl = apiBaseUrl ?? apiBase {
    identityChanges?.addListener(clear);
  }

  static const apiBase = BirdtieEnvironment.apiBaseUrl;
  final String? Function() authorizationHeader;
  final http.Client _client;
  http.Client get detailClient => _client;
  String get apiBaseUrl => _apiBaseUrl;
  final Listenable? identityChanges;
  final bool _ownsClient;
  bool _disposed = false;
  int _generation = 0;
  final String _apiBaseUrl;
  bool get configured => _apiBaseUrl.isNotEmpty;
  List<ActivityPlan> plans = const [];
  bool loading = false;
  bool failed = false;
  final Set<String> busy = {};
  int _serial = 0;

  Uri _endpoint(String path) =>
      Uri.parse('${_apiBaseUrl.replaceFirst(RegExp(r'/$'), '')}$path');
  Map<String, String> _headers({bool json = false}) {
    final token = authorizationHeader();
    final headers = <String, String>{};
    if (json) headers['Content-Type'] = 'application/json';
    if (token != null) headers['Authorization'] = token;
    return headers;
  }

  bool contains(String activityId) =>
      plans.any((plan) => plan.activityId == activityId);

  void clear({bool notify = true}) {
    if (_disposed) return;
    ++_generation;
    ++_serial;
    plans = const [];
    loading = false;
    failed = false;
    busy.clear();
    if (notify) notifyListeners();
  }

  Future<void> load() async {
    if (_disposed) return;
    final token = authorizationHeader();
    if (!configured || authorizationHeader() == null) {
      clear();
      return;
    }
    final serial = ++_serial;
    loading = true;
    failed = false;
    notifyListeners();
    if (_disposed || serial != _serial || token != authorizationHeader()) {
      return;
    }
    try {
      final response = await _client
          .get(_endpoint('/v1/me/activity-plans'), headers: _headers())
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) throw StateError('Plans unavailable');
      final rows =
          (jsonDecode(response.body) as Map<String, dynamic>)['data']
              as List<dynamic>;
      if (_disposed || serial != _serial || token != authorizationHeader()) {
        return;
      }
      plans = [
        for (final row in rows)
          ActivityPlan.fromJson(row as Map<String, dynamic>),
      ];
    } catch (_) {
      if (_disposed || serial != _serial || token != authorizationHeader()) {
        return;
      }
      failed = true;
      plans = const [];
    } finally {
      if (!_disposed && serial == _serial && token == authorizationHeader()) {
        loading = false;
        notifyListeners();
      }
    }
  }

  Future<void> toggle(String activityId) async {
    if (_disposed) return;
    final token = authorizationHeader(), generation = _generation;
    if (!configured || authorizationHeader() == null) {
      throw StateError('Sign in to plan an activity');
    }
    if (busy.contains(activityId)) return;
    busy.add(activityId);
    notifyListeners();
    if (_disposed ||
        generation != _generation ||
        token != authorizationHeader()) {
      return;
    }
    try {
      final existing = plans.where((plan) => plan.activityId == activityId);
      if (existing.isEmpty) {
        final response = await _client
            .post(
              _endpoint('/v1/me/activity-plans'),
              headers: {
                'Authorization': token!,
                'Content-Type': 'application/json',
              },
              body: jsonEncode({'activityId': activityId}),
            )
            .timeout(const Duration(seconds: 12));
        if (response.statusCode != 201) throw StateError('Plan unavailable');
      } else {
        final response = await _client
            .delete(
              _endpoint(
                '/v1/me/activity-plans/${Uri.encodeComponent(existing.first.id)}',
              ),
              headers: {'Authorization': token!},
            )
            .timeout(const Duration(seconds: 12));
        if (response.statusCode != 204) {
          throw StateError('Could not remove plan');
        }
      }
      if (_disposed ||
          generation != _generation ||
          token != authorizationHeader()) {
        return;
      }
      await load();
    } finally {
      if (!_disposed && generation == _generation) {
        busy.remove(activityId);
        notifyListeners();
      }
    }
  }

  @override
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    identityChanges?.removeListener(clear);
    ++_generation;
    ++_serial;
    if (_ownsClient) _client.close();
    super.dispose();
  }
}

class MyActivitiesPage extends StatefulWidget {
  const MyActivitiesPage({
    super.key,
    required this.plans,
    required this.participations,
    required this.saved,
    required this.auth,
    required this.city,
    required this.organizations,
    this.onSignIn,
  });
  final ActivityPlansController plans;
  final ActivityParticipationController participations;
  final SavedController saved;
  final BirdtieAuthController auth;
  final PublicCityController city;
  final OrganizationWorkspaceController organizations;
  final VoidCallback? onSignIn;

  @override
  State<MyActivitiesPage> createState() => _MyActivitiesPageState();
}

class _MyActivitiesPageState extends State<MyActivitiesPage> {
  late OrganizationActivityApi _publishing;
  OrganizationActivityApi _makePublishing() {
    final epoch = _entryEpoch,
        plans = widget.plans,
        token = widget.auth.authorizationHeader;
    bool live() =>
        mounted &&
        epoch == _entryEpoch &&
        widget.plans == plans &&
        token == widget.auth.authorizationHeader &&
        _sourcesCurrent;
    return OrganizationActivityApi(
      authorizationHeader: () => live() ? token : null,
      apiBaseUrl: plans.apiBaseUrl,
      client: _PlansDetailClient(plans.detailClient, live),
    );
  }

  List<OrganizationActivity> _managed = const [];
  int _entryEpoch = 0;
  int? _scheduledPersonalLoadEpoch;
  Route<void>? _detailRoute;
  final _loginEntryChanges = ValueNotifier<int>(0);

  void _notifyLoginEntryChanges({bool dispose = false}) {
    void notify() {
      if (dispose || mounted) _loginEntryChanges.value++;
      if (dispose) _loginEntryChanges.dispose();
    }

    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) => notify());
    } else {
      notify();
    }
  }

  (String?, String?, String?)? _observedIdentity;
  (String?, String?, String?) get _currentIdentity => (
    widget.auth.authorizationHeader,
    widget.auth.accountID,
    widget.organizations.active?.id,
  );
  bool get _sourcesCurrent =>
      widget.plans.authorizationHeader() == widget.auth.authorizationHeader &&
      widget.participations.authorizationHeader() ==
          widget.auth.authorizationHeader &&
      widget.saved.authorizationHeader() == widget.auth.authorizationHeader;

  void _loadPersonal() {
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      final epoch = _entryEpoch;
      if (_scheduledPersonalLoadEpoch == epoch) return;
      _scheduledPersonalLoadEpoch = epoch;
      final identity = _currentIdentity;
      final plans = widget.plans,
          saved = widget.saved,
          participations = widget.participations;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (_scheduledPersonalLoadEpoch == epoch)
          _scheduledPersonalLoadEpoch = null;
        if (mounted &&
            epoch == _entryEpoch &&
            identity == _currentIdentity &&
            identical(plans, widget.plans) &&
            identical(saved, widget.saved) &&
            identical(participations, widget.participations)) {
          _loadPersonal();
        }
      });
      return;
    }
    if (!_sourcesCurrent ||
        widget.auth.sessionRestoring ||
        !widget.auth.signedIn)
      return;
    unawaited(widget.plans.load());
    if (!_sourcesCurrent) return;
    unawaited(widget.participations.load());
    if (!_sourcesCurrent) return;
    unawaited(widget.saved.load(paged: false));
  }

  void _identity() {
    if (_observedIdentity == _currentIdentity) return;
    _retireIdentity();
  }

  void _retireIdentity() {
    _observedIdentity = _currentIdentity;
    ++_entryEpoch;
    _notifyLoginEntryChanges();
    final building =
        SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks;
    widget.plans.clear(notify: !building);
    widget.participations.clear(notify: !building);
    widget.saved.clear(notify: !building);
    _managed = const [];
    _managedError = null;
    _managedLoading = false;
    _publishing.dispose();
    _publishing = _makePublishing();
    final route = _detailRoute;
    _detailRoute = null;
    void close() {
      if (route?.navigator != null) route!.navigator!.removeRoute(route);
    }

    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) => close());
    } else {
      close();
    }
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) {
        _loadPersonal();
        unawaited(_loadManaged());
      }
    });
  }

  @override
  void didUpdateWidget(MyActivitiesPage old) {
    super.didUpdateWidget(old);
    if (old.auth != widget.auth ||
        old.organizations != widget.organizations ||
        old.plans != widget.plans ||
        old.participations != widget.participations ||
        old.saved != widget.saved) {
      old.auth.removeListener(_identity);
      old.organizations.removeListener(_identity);
      _retireIdentity();
      widget.auth.addListener(_identity);
      widget.organizations.addListener(_identity);
    }
  }

  Future<void> _openActivity(String id) async {
    if (!chatEntityUUID.hasMatch(id) ||
        !_sourcesCurrent ||
        widget.plans.authorizationHeader() == null) {
      return;
    }
    final epoch = _entryEpoch,
        plans = widget.plans,
        token = widget.auth.authorizationHeader,
        owner = widget.auth.accountID,
        org = widget.organizations.active?.id;
    bool live() =>
        mounted &&
        _entryEpoch == epoch &&
        widget.plans == plans &&
        token == widget.auth.authorizationHeader &&
        owner == widget.auth.accountID &&
        org == widget.organizations.active?.id;
    String? currentWorkspaceID() => widget.organizations.active?.id;
    final workspaceID = currentWorkspaceID;
    final client = _PlansDetailClient(plans.detailClient, live);
    final route = MaterialPageRoute<void>(
      builder: (_) => ChatEntityDetail(
        type: 'activity',
        id: id,
        auth: widget.auth,
        workspaceChanges: widget.organizations,
        workspaceID: workspaceID,
        apiBaseUrl: plans.apiBaseUrl,
        client: client,
      ),
    );
    _detailRoute = route;
    await Navigator.of(context).push(route);
    if (_detailRoute == route) _detailRoute = null;
  }

  bool _managedLoading = true;
  String? _managedError;

  @override
  void initState() {
    super.initState();
    _observedIdentity = _currentIdentity;
    _publishing = _makePublishing();
    widget.auth.addListener(_identity);
    widget.organizations.addListener(_identity);
    _loadPersonal();
    unawaited(_loadManaged());
  }

  @override
  void dispose() {
    widget.auth.removeListener(_identity);
    widget.organizations.removeListener(_identity);
    ++_entryEpoch;
    _notifyLoginEntryChanges(dispose: true);
    _publishing.dispose();
    final route = _detailRoute;
    _detailRoute = null;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (route?.navigator != null) route!.navigator!.removeRoute(route);
    });
    super.dispose();
  }

  Future<void> _loadManaged() async {
    final epoch = _entryEpoch, api = _publishing;
    bool live() =>
        mounted &&
        epoch == _entryEpoch &&
        identical(api, _publishing) &&
        _sourcesCurrent;
    if (!widget.auth.signedIn ||
        widget.auth.sessionRestoring ||
        !_sourcesCurrent) {
      if (mounted) {
        setState(() {
          _managed = const [];
          _managedLoading = false;
        });
      }
      return;
    }
    setState(() {
      _managedLoading = true;
      _managedError = null;
    });
    try {
      final items = await api.listMine();
      if (live()) setState(() => _managed = items);
    } catch (_) {
      if (live()) setState(() => _managedError = '我发起的活动加载失败，请重试。');
    } finally {
      if (live()) setState(() => _managedLoading = false);
    }
  }

  Future<void> _editManaged([OrganizationActivity? activity]) async {
    if (!_sourcesCurrent || !widget.auth.signedIn) return;
    final changed = await Navigator.of(context).push<bool>(
      MaterialPageRoute(
        builder: (_) => ActivityEditorPage(
          api: _publishing,
          auth: widget.auth,
          city: widget.city,
          organizations: widget.organizations,
          existing: activity,
        ),
      ),
    );
    if (changed == true) {
      await _loadManaged();
      await widget.city.loadActivities();
    }
  }

  Future<void> _changeManaged(
    OrganizationActivity activity,
    String action,
  ) async {
    if (!_sourcesCurrent || !widget.auth.signedIn) return;
    final epoch = _entryEpoch, api = _publishing;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(action == 'publish' ? '确认发布活动' : '确认取消活动'),
        content: Text(
          action == 'publish'
              ? '发布“${activity.title}”？系统将按当前可见范围展示。'
              : '取消“${activity.title}”？已报名的人会收到状态变更。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('返回'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: Text(action == 'publish' ? '确认发布' : '确认取消'),
          ),
        ],
      ),
    );
    if (confirmed != true ||
        !mounted ||
        epoch != _entryEpoch ||
        !_sourcesCurrent ||
        !identical(api, _publishing)) {
      return;
    }
    try {
      if (action == 'publish') {
        await _publishing.publishMine(activity.id);
      } else {
        await _publishing.cancelMine(activity.id);
      }
      await _loadManaged();
      await widget.city.loadActivities();
    } catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              error is OrganizationActivityException
                  ? error.chineseMessage
                  : '操作失败，请稍后重试。',
            ),
          ),
        );
      }
    }
  }

  Future<void> _remove(ActivityPlan plan) async {
    if (!_sourcesCurrent || !widget.auth.signedIn) return;
    try {
      await widget.plans.toggle(plan.activityId);
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('活动计划更新失败，请重试。')));
      }
    }
  }

  void _signIn() {
    final auth = widget.auth, epoch = _entryEpoch, plans = widget.plans;
    bool current() =>
        mounted &&
        identical(widget.auth, auth) &&
        identical(widget.plans, plans) &&
        epoch == _entryEpoch;
    unawaited(
      showBirdtieLoginSheet(
        context,
        auth,
        isCurrent: current,
        entryChanges: _loginEntryChanges,
      ),
    );
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: Listenable.merge([
      widget.auth,
      widget.plans,
      widget.participations,
      widget.saved,
    ]),
    builder: (context, _) {
      final controller = widget.plans;
      final attendance = widget.participations;
      final saved = widget.saved;
      final upcoming =
          attendance.items.where((item) => item.joined && !item.past).toList()
            ..sort(
              (a, b) => (a.startsAt ?? DateTime(9999)).compareTo(
                b.startsAt ?? DateTime(9999),
              ),
            );
      final past = attendance.items
          .where((item) => item.joined && item.past)
          .toList();
      final savedActivities = saved.items
          .where((item) => item.kind == 'activity')
          .toList();
      if (widget.auth.sessionRestoring) {
        return const AccountAccessNotice(
          title: '正在恢复账户',
          message: '账户核验完成后会加载本人的活动。',
          restoring: true,
        );
      }
      if (!controller.configured) {
        return const Center(child: Text('活动暂不可用，请稍后重试。'));
      }
      if (!widget.auth.signedIn) {
        return AccountAccessNotice(
          title: '登录后查看我的活动',
          message: '你报名、发起、计划参加和收藏的活动会显示在这里。',
          actionLabel: '登录',
          onAction: widget.onSignIn ?? _signIn,
        );
      }
      if (!_sourcesCurrent) {
        return const AccountAccessNotice(
          title: '账户来源已变化',
          message: '请返回后重新打开我的活动；本页没有读取旧账户的数据。',
        );
      }
      if ((controller.loading || attendance.loading || saved.loading) &&
          controller.plans.isEmpty &&
          attendance.items.isEmpty &&
          saved.items.isEmpty) {
        return const Center(child: CircularProgressIndicator());
      }
      if (attendance.failed && attendance.items.isEmpty) {
        return Center(
          child: TextButton(
            onPressed: attendance.load,
            child: const Text('报名记录暂不可用，点击重试。'),
          ),
        );
      }
      return RefreshIndicator(
        onRefresh: () async {
          if (!_sourcesCurrent || !widget.auth.signedIn) return;
          await Future.wait([
            attendance.load(),
            saved.load(paged: false),
            controller.load(),
            _loadManaged(),
          ]);
        },
        child: ListView(
          padding: const EdgeInsets.fromLTRB(16, 22, 16, 32),
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    '我发起的',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                ),
                FilledButton.icon(
                  key: const Key('create_activity'),
                  onPressed: widget.city.selectedCity?.id == 'aberdeen-gb'
                      ? () => _editManaged()
                      : null,
                  icon: const Icon(Icons.add),
                  label: const Text('发起活动'),
                ),
              ],
            ),
            if (_managedLoading) const LinearProgressIndicator(),
            if (_managedError != null)
              TextButton(onPressed: _loadManaged, child: Text(_managedError!)),
            if (widget.city.selectedCity?.id != 'aberdeen-gb')
              const Padding(
                padding: EdgeInsets.only(top: 8),
                child: Text('目前仅支持在阿伯丁发起活动。'),
              ),
            if (!_managedLoading &&
                _managedError == null &&
                _managed.isEmpty &&
                upcoming.isEmpty &&
                past.isEmpty &&
                savedActivities.isEmpty &&
                controller.plans.isEmpty &&
                !saved.failed &&
                !controller.failed)
              const ListTile(title: Text('暂无活动')),
            for (final activity in _managed)
              Card(
                margin: const EdgeInsets.only(top: 10),
                child: Padding(
                  padding: const EdgeInsets.all(14),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        activity.title,
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      const SizedBox(height: 5),
                      Text(
                        '${activity.organizer?.label ?? '主办方待核对'} · '
                        '${activity.cancelledAt != null
                            ? '已取消'
                            : activity.publicationStatus == 'draft'
                            ? '草稿'
                            : '已发布'}',
                      ),
                      if (activity.cancelledAt == null)
                        Wrap(
                          spacing: 8,
                          children: [
                            TextButton(
                              onPressed: () => _editManaged(activity),
                              child: const Text('编辑'),
                            ),
                            if (activity.publicationStatus == 'draft')
                              TextButton(
                                onPressed: () =>
                                    _changeManaged(activity, 'publish'),
                                child: const Text('发布'),
                              )
                            else
                              TextButton(
                                onPressed: () =>
                                    _changeManaged(activity, 'cancel'),
                                child: const Text('取消活动'),
                              ),
                          ],
                        ),
                    ],
                  ),
                ),
              ),
            if (upcoming.isNotEmpty) ...[
              const SizedBox(height: 20),
              Text('即将参加', style: Theme.of(context).textTheme.titleMedium),
            ],
            for (final item in upcoming)
              ListTile(
                onTap: item.available
                    ? () => _openActivity(item.activityId)
                    : null,
                leading: const Icon(Icons.event_available_outlined),
                title: Text(item.available ? item.title : '活动暂不可用'),
                subtitle: Text(
                  item.available
                      ? '${item.status == 'pending' ? '待确认' : '已报名'} · ${_activityStatus(item.activityStatus)}\n${plansSchedule(item.startsAt, item.endsAt, item.timeZone)}\n${plansLocation(item.modality, item.physicalPlaceStatus, item.placeName)}'
                      : '活动暂不可见',
                ),
              ),
            if (savedActivities.isNotEmpty || saved.failed) ...[
              const SizedBox(height: 20),
              Text('收藏的活动', style: Theme.of(context).textTheme.titleMedium),
              if (saved.failed) const ListTile(title: Text('收藏加载失败，下拉重试。')),
            ],
            for (final item in savedActivities)
              ListTile(
                leading: const Icon(Icons.bookmark_outline),
                title: Text(item.available ? item.title : '活动暂不可用'),
                onTap: item.available
                    ? () => _openActivity(item.targetId)
                    : null,
                subtitle: const Text('已收藏 · 不代表已报名'),
              ),
            if (past.isNotEmpty) ...[
              const SizedBox(height: 20),
              Text('已结束', style: Theme.of(context).textTheme.titleMedium),
            ],
            for (final item in past)
              ListTile(
                leading: const Icon(Icons.history),
                title: Text(item.available ? item.title : '活动暂不可用'),
                onTap: item.available
                    ? () => _openActivity(item.activityId)
                    : null,
                subtitle: Text(
                  item.available
                      ? '${_activityStatus(item.activityStatus)}\n${plansSchedule(item.startsAt, item.endsAt, item.timeZone)}\n${plansLocation(item.modality, item.physicalPlaceStatus, item.placeName)}'
                      : '活动暂不可见',
                ),
              ),
            if (controller.plans.isNotEmpty || controller.failed) ...[
              const SizedBox(height: 20),
              Text('个人提醒', style: Theme.of(context).textTheme.titleMedium),
              if (controller.plans.isNotEmpty)
                Text(
                  '个人提醒不代表已报名或确认参加。',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              if (controller.failed)
                const ListTile(title: Text('个人提醒加载失败，下拉重试。')),
            ],
            for (final plan in controller.plans)
              ListTile(
                onTap: plan.available
                    ? () => _openActivity(plan.activityId)
                    : null,
                leading: const Icon(Icons.event_outlined),
                title: Text(plan.available ? plan.title : '活动暂不可用'),
                subtitle: Text(
                  plan.available
                      ? '${_activityStatus(plan.status)}\n${plansSchedule(plan.startsAt, plan.endsAt, plan.timeZone)}\n${plansLocation(plan.modality, plan.physicalPlaceStatus, plan.placeName)}'
                      : '该活动已不可见，你可以移除此计划。',
                ),
                trailing: IconButton(
                  tooltip: '移除活动计划',
                  onPressed: controller.busy.contains(plan.activityId)
                      ? null
                      : () => _remove(plan),
                  icon: const Icon(Icons.event_busy_outlined),
                ),
              ),
          ],
        ),
      );
    },
  );
}

String _activityStatus(String status) => switch (status) {
  'upcoming' => '即将开始',
  'ongoing' => '进行中',
  'completed' => '已结束',
  'past' => '已结束',
  'cancelled' => '已取消',
  'unavailable' => '暂不可用',
  _ => status,
};

class _PlansDetailClient extends http.BaseClient {
  _PlansDetailClient(this.borrowed, this.current);
  final http.Client borrowed;
  final bool Function() current;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    if (!current()) throw StateError('原活动详情工作身份已失效');
    final response = await http.Response.fromStream(
      await borrowed.send(request),
    );
    if (!current()) throw StateError('原活动详情响应已失效');
    return http.StreamedResponse(
      Stream.value(response.bodyBytes),
      response.statusCode,
      headers: response.headers,
      reasonPhrase: response.reasonPhrase,
      request: request,
    );
  }

  @override
  void close() {}
}
