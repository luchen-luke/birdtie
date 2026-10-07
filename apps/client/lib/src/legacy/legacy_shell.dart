import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../auth/birdtie_auth_controller.dart';
import '../auth/dev_phone_login_sheet.dart';
import '../city/public_city_controller.dart';
import '../city/public_city_map.dart';
import '../content/private_moment_controller.dart';
import '../content/private_moment_media_controller.dart';
import '../content/private_moment_media_sheet.dart';
import '../content/moment_time_choice.dart';
import '../content/moment_context_choice.dart';
import '../content/public_intent_section.dart';
import '../config/birdtie_environment.dart';
import '../workspace/community_api.dart';

// Existing City and private Moment surfaces remain available from the V2 sidebar.
class LegacyProfilePage extends StatelessWidget {
  const LegacyProfilePage({
    super.key,
    required this.auth,
    required this.moments,
    required this.city,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
  });

  final BirdtieAuthController auth;
  final PrivateMomentController moments;
  final PublicCityController city;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;

  @override
  Widget build(BuildContext context) => _MyBirdtiePage(
    auth: auth,
    moments: moments,
    city: city,
    client: client,
    apiBaseUrl: apiBaseUrl,
    workspaceChanges: workspaceChanges,
    organizationWorkspaceID: organizationWorkspaceID,
  );
}

class LegacyActivityPage extends StatelessWidget {
  const LegacyActivityPage({super.key, required this.city});
  final PublicCityController city;

  @override
  Widget build(BuildContext context) => _NowPage(city: city);
}

const _forest = Color(0xFF193B32);
const _surface = Color(0xFFFCFBF8);
const _muted = Color(0xFF747B73);
const _rule = Color(0xFFDFE1D8);
const _copper = Color(0xFFB96743);

class _NowPage extends StatelessWidget {
  const _NowPage({required this.city});

  final PublicCityController city;

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: city,
    builder: (context, _) {
      final selected = city.selectedCity;
      return _PageFrame(
        eyebrow: selected == null
            ? 'NOW'
            : 'NOW · ${selected.name.toUpperCase()}',
        title: '今日城市动态',
        subtitle: '近期活动、城市故事与正在寻找同行者的意图。',
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            _CityState(city: city),
            const SizedBox(height: 34),
            const _SectionLabel('城市速览'),
            const SizedBox(height: 12),
            _ActivitySection(city: city),
            const SizedBox(height: 30),
            const _SectionLabel('探索城市'),
            const SizedBox(height: 10),
            Text(
              selected == null
                  ? '选择城市后可从地点、活动和真实经历开始探索。'
                  : '从地点、活动和真实经历开始了解 ${selected.name}。',
              style: const TextStyle(color: _muted, height: 1.5),
            ),
          ],
        ),
      );
    },
  );
}

class _ActivitySection extends StatelessWidget {
  const _ActivitySection({required this.city});

  final PublicCityController city;

  @override
  Widget build(BuildContext context) {
    if (city.selectedCity == null) {
      return const _EmptySection(
        icon: Icons.event_outlined,
        title: '先选择城市',
        detail: '公开活动会按所选城市读取。',
      );
    }
    if (city.activitiesLoading && city.activities.isEmpty) {
      return const _EmptySection(
        icon: Icons.event_outlined,
        title: '正在读取活动',
        detail: '活动状态会依据时间和取消记录实时计算。',
      );
    }
    if (city.activityError != null) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _EmptySection(
            icon: Icons.event_busy_outlined,
            title: '活动暂不可用',
            detail: city.activityError!,
          ),
          TextButton(onPressed: city.loadActivities, child: const Text('重试')),
        ],
      );
    }
    if (city.activities.isEmpty) {
      return const _EmptySection(
        icon: Icons.event_outlined,
        title: '目前没有已发布活动',
        detail: '城市种子活动通过来源审核后才会显示。公开动态尚未接入；个人意图可在个人资料中管理。',
      );
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (final activity in city.activities) ...[
          const Divider(height: 24),
          Text(
            activity.title,
            style: const TextStyle(
              color: _forest,
              fontSize: 17,
              fontWeight: FontWeight.w600,
            ),
          ),
          const SizedBox(height: 5),
          Text(
            '${_activityStatusLabel(activity.status)} · ${_utcLabel(activity.startsAt)} · 原时区 ${activity.timeZone}',
            style: TextStyle(
              color: activity.status == 'cancelled' ? _copper : _forest,
              fontSize: 12,
            ),
          ),
          if (activity.hostLabel.isNotEmpty) ...[
            const SizedBox(height: 4),
            Text(
              '主办信息（来源标注）：${activity.hostLabel}',
              style: const TextStyle(color: _muted, fontSize: 12),
            ),
          ],
          if (activity.summary.isNotEmpty) ...[
            const SizedBox(height: 6),
            Text(activity.summary),
          ],
          const SizedBox(height: 5),
          Text(
            _sourceDescription(activity.source),
            style: const TextStyle(color: _muted, fontSize: 12),
          ),
        ],
      ],
    );
  }
}

String _activityStatusLabel(String status) => switch (status) {
  'upcoming' => '即将开始',
  'ongoing' => '进行中',
  'past' => '已结束',
  'cancelled' => '已取消',
  _ => '状态未知',
};

String _utcLabel(DateTime instant) {
  final utc = instant.toUtc();
  return '${utc.year}-${utc.month.toString().padLeft(2, '0')}-${utc.day.toString().padLeft(2, '0')} '
      '${utc.hour.toString().padLeft(2, '0')}:${utc.minute.toString().padLeft(2, '0')} UTC';
}

class _CityState extends StatelessWidget {
  const _CityState({required this.city});

  final PublicCityController city;

  @override
  Widget build(BuildContext context) {
    final selected = city.selectedCity;
    if (selected == null) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _StatusLine(
            icon: Icons.location_city_outlined,
            title: city.citiesLoading ? '正在读取城市' : '城市暂不可用',
            detail: !city.configured
                ? '尚未配置城市 API 地址。'
                : city.cityError ?? '当前没有已发布的城市。',
            accent: _copper,
          ),
          if (city.cityError != null) ...[
            const SizedBox(height: 8),
            TextButton(onPressed: city.loadCities, child: const Text('重试')),
          ],
        ],
      );
    }
    return _StatusLine(
      icon: Icons.info_outline,
      title: selected.contentStatus == 'building'
          ? '${selected.name} 内容建设中'
          : '${selected.name} 城市内容状态：${selected.contentStatus}',
      detail: _sourceDescription(selected.source),
      accent: _copper,
    );
  }
}

String _freshnessLabel(String freshness) => switch (freshness) {
  'current' => '已核验',
  'expired' => '已过期',
  'review_needed' => '待复核',
  _ => '尚未核验',
};

String _sourceDescription(PublicSource source) {
  final updated = source.updatedAt;
  final updatedLabel = updated == null
      ? '更新时间未知'
      : '更新于 ${updated.year}-${updated.month.toString().padLeft(2, '0')}-${updated.day.toString().padLeft(2, '0')}';
  final label = source.label.isEmpty ? '未标注' : source.label;
  final maintainer = source.maintainer.isEmpty ? '未标注' : source.maintainer;
  return '来源：$label · 维护：$maintainer · ${_freshnessLabel(source.freshness)} · $updatedLabel';
}

class _ExplorePage extends StatefulWidget {
  const _ExplorePage({required this.city});

  final PublicCityController city;

  @override
  State<_ExplorePage> createState() => _ExplorePageState();
}

class _ExplorePageState extends State<_ExplorePage> {
  bool _mapView = false;
  final _search = TextEditingController();
  Timer? _searchDebounce;

  @override
  void dispose() {
    _searchDebounce?.cancel();
    _search.dispose();
    super.dispose();
  }

  void _scheduleSearch(String query) {
    _searchDebounce?.cancel();
    _searchDebounce = Timer(
      const Duration(milliseconds: 300),
      () => widget.city.searchPlaces(query),
    );
    setState(() {});
  }

  void _applySearch(String query) {
    _searchDebounce?.cancel();
    widget.city.searchPlaces(query);
  }

  void _showPlace(PublicPlace place) {
    showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      backgroundColor: _surface,
      builder: (context) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(24, 4, 24, 28),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                place.name,
                style: Theme.of(context).textTheme.headlineSmall,
              ),
              const SizedBox(height: 8),
              Text(place.categoryCode, style: const TextStyle(color: _muted)),
              if (place.summary.isNotEmpty) ...[
                const SizedBox(height: 12),
                Text(place.summary),
              ],
              const SizedBox(height: 14),
              Text(
                _sourceDescription(place.source),
                style: const TextStyle(color: _muted, fontSize: 12),
              ),
            ],
          ),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.city,
    builder: (context, _) => _PageFrame(
      eyebrow: '探索城市各处',
      title: '探索${widget.city.selectedCity?.name ?? '城市'}',
      subtitle: '无需分享设备定位。已发布地点来自当前选定城市。',
      trailing: SegmentedButton<bool>(
        showSelectedIcon: false,
        segments: const [
          ButtonSegment(
            value: true,
            icon: Icon(Icons.map_outlined),
            label: Text('地图'),
          ),
          ButtonSegment(
            value: false,
            icon: Icon(Icons.view_list_outlined),
            label: Text('列表'),
          ),
        ],
        selected: {_mapView},
        onSelectionChanged: (selection) =>
            setState(() => _mapView = selection.first),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _CityState(city: widget.city),
          const SizedBox(height: 26),
          const _SectionLabel('城市内容'),
          const SizedBox(height: 14),
          TextField(
            controller: _search,
            maxLength: 60,
            textInputAction: TextInputAction.search,
            onChanged: _scheduleSearch,
            onSubmitted: _applySearch,
            decoration: InputDecoration(
              labelText: '搜索已发布地点',
              hintText: '名称或简介',
              prefixIcon: const Icon(Icons.search),
              suffixIcon: _search.text.isEmpty
                  ? null
                  : IconButton(
                      tooltip: '清除搜索',
                      onPressed: () {
                        _search.clear();
                        _applySearch('');
                        setState(() {});
                      },
                      icon: const Icon(Icons.close),
                    ),
              border: const OutlineInputBorder(),
            ),
          ),
          const SizedBox(height: 10),
          _ExploreCanvas(
            mapView: _mapView,
            city: widget.city,
            onPlaceSelected: _showPlace,
          ),
          const SizedBox(height: 16),
          const Text(
            '公开动态与行程功能尚未接入；地图与列表共用当前城市和搜索条件。',
            style: TextStyle(color: _muted, fontSize: 12),
          ),
        ],
      ),
    ),
  );
}

class _ExploreCanvas extends StatelessWidget {
  const _ExploreCanvas({
    required this.mapView,
    required this.city,
    required this.onPlaceSelected,
  });

  final bool mapView;
  final PublicCityController city;
  final ValueChanged<PublicPlace> onPlaceSelected;

  @override
  Widget build(BuildContext context) {
    return AnimatedContainer(
      duration: const Duration(milliseconds: 220),
      curve: Curves.easeOut,
      width: double.infinity,
      constraints: const BoxConstraints(minHeight: 330),
      decoration: BoxDecoration(
        color: mapView ? const Color(0xFFE9ECE4) : _surface,
        borderRadius: BorderRadius.circular(18),
      ),
      child: mapView
          ? city.selectedCity == null
                ? const Center(child: Text('先选择城市以查看地图。'))
                : PublicCityMapView(
                    city: city.selectedCity!,
                    places: city.places,
                    onPlaceSelected: onPlaceSelected,
                    placeStateMessage:
                        city.placeError ??
                        (city.placesLoading ? '正在读取地点…' : null),
                  )
          : _ExplorePlaces(city: city, onPlaceSelected: onPlaceSelected),
    );
  }
}

class _ExplorePlaces extends StatelessWidget {
  const _ExplorePlaces({required this.city, required this.onPlaceSelected});

  final PublicCityController city;
  final ValueChanged<PublicPlace> onPlaceSelected;

  @override
  Widget build(BuildContext context) {
    if (city.selectedCity == null) {
      return const Center(
        child: Text('连接城市后可查看已发布地点。', style: TextStyle(color: _muted)),
      );
    }
    if (city.placesLoading && city.places.isEmpty) {
      return const Center(child: Text('正在读取已发布地点…'));
    }
    if (city.placeError != null) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(city.placeError!, style: const TextStyle(color: _muted)),
            TextButton(onPressed: city.loadPlaces, child: const Text('重试')),
          ],
        ),
      );
    }
    if (city.places.isEmpty) {
      return Center(
        child: Text(
          city.placeQuery.isEmpty ? '当前城市还没有已发布地点。' : '没有匹配的已发布地点。',
          style: const TextStyle(color: _muted),
        ),
      );
    }
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 22, vertical: 12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            '${city.places.length} 个已发布地点',
            style: const TextStyle(color: _muted, fontSize: 12),
          ),
          for (final place in city.places) ...[
            const Divider(height: 20),
            InkWell(
              onTap: () => onPlaceSelected(place),
              child: Text(
                place.name,
                style: const TextStyle(
                  color: _forest,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
            if (place.summary.isNotEmpty) ...[
              const SizedBox(height: 4),
              Text(place.summary),
            ],
            const SizedBox(height: 5),
            Text(
              '${place.categoryCode} · ${_sourceDescription(place.source)}',
              style: const TextStyle(color: _muted, fontSize: 12),
            ),
          ],
        ],
      ),
    );
  }
}

class _MomentDraftSheet extends StatefulWidget {
  const _MomentDraftSheet({
    required this.cityID,
    required this.cityName,
    required this.moments,
    required this.places,
    required this.activities,
    required this.auth,
    this.existing,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
  });

  final String cityID;
  final String cityName;
  final PrivateMomentController moments;
  final List<PublicPlace> places;
  final List<PublicActivity> activities;
  final PrivateMoment? existing;
  final BirdtieAuthController auth;

  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;

  @override
  State<_MomentDraftSheet> createState() => _MomentDraftSheetState();
}

class _MomentDraftSheetState extends State<_MomentDraftSheet> {
  final _formKey = GlobalKey<FormState>();
  late final TextEditingController _title;
  late final TextEditingController _body;
  late final CommunityApi _communityApi;
  final http.Client _lookupClient = http.Client();
  List<CommunityItem> _communities = const [];
  List<(String, String)> _organizations = const [];
  bool _loadingContexts = false;
  String? _contextError;
  late String _placeID;
  late String _activityID;
  late String _communityID;
  late String _organizationID;
  late final String? _draftToken;
  late final String? _draftOwner;
  late final BirdtieAuthController _draftAuth;
  late final PrivateMomentController _draftMoments;
  bool _retired = false;
  late MomentTimeValue? _time;
  String? _draftError;

  @override
  void initState() {
    super.initState();
    _title = TextEditingController(text: widget.existing?.title);
    _body = TextEditingController(text: widget.existing?.body);
    _placeID = widget.existing?.placeID ?? '';
    _activityID = widget.existing?.activityIDs.firstOrNull ?? '';
    _communityID = widget.existing?.communityID ?? '';
    _organizationID = widget.existing?.organizationID ?? '';
    _draftToken = widget.moments.authorizationHeader();
    _draftOwner = widget.auth.accountID;
    _draftAuth = widget.auth;
    _draftMoments = widget.moments;
    _draftAuth.addListener(_onActorChanged);
    _time = widget.existing?.time ?? MomentTimeValue.unknown;
    _communityApi = CommunityApi(
      authorizationHeader: widget.moments.authorizationHeader,
    );
  }

  bool get _matchesBinding =>
      identical(widget.auth, _draftAuth) &&
      identical(widget.moments, _draftMoments) &&
      widget.auth.signedIn &&
      widget.auth.accountID == _draftOwner &&
      widget.auth.authorizationHeader == _draftToken &&
      widget.moments.authorizationHeader() == _draftToken;

  bool get _current => mounted && !_retired && _matchesBinding;

  void _onActorChanged() {
    if (_retired || _matchesBinding) return;
    // Observe each identity event, including A -> B -> A before the next frame.
    setState(() => _retired = true);
  }

  Future<void> _loadContexts() async {
    final token = widget.moments.authorizationHeader();
    if (!_current || token == null || BirdtieEnvironment.apiBaseUrl.isEmpty) {
      return;
    }
    setState(() {
      _loadingContexts = true;
      _contextError = null;
    });
    try {
      final communities = await _communityApi.mine();
      if (!_current) return;
      final response = await _lookupClient
          .get(
            Uri.parse(
              '${BirdtieEnvironment.apiBaseUrl.replaceFirst(RegExp(r'/$'), '')}/v1/me/organizations',
            ),
            headers: {'Authorization': token},
          )
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) {
        throw StateError('organizations unavailable');
      }
      final rows =
          (jsonDecode(response.body) as Map<String, dynamic>)['data']
              as List<dynamic>;
      if (!_current) return;
      setState(() {
        _communities = communities
            .where(
              (item) =>
                  item.joined &&
                  (item.cityId == null || item.cityId == widget.cityID),
            )
            .toList();
        _organizations = rows.map((raw) {
          final row = raw as Map<String, dynamic>;
          return (row['id'] as String, row['name'] as String);
        }).toList();
      });
    } catch (_) {
      if (_current) {
        setState(() => _contextError = '社群或组织列表暂不可用，可只保存未关联的草稿。');
      }
    } finally {
      if (_current) {
        setState(() => _loadingContexts = false);
      }
    }
  }

  @override
  void dispose() {
    _draftAuth.removeListener(_onActorChanged);
    _title.dispose();
    _body.dispose();
    _communityApi.dispose();
    _lookupClient.close();
    super.dispose();
  }

  Future<void> _save() async {
    if (!_current) {
      setState(() => _draftError = '账号已切换，请关闭草稿后重新打开。');
      return;
    }
    if (!_formKey.currentState!.validate()) return;
    if (_time == null) return;
    final existing = widget.existing;
    if (existing != null &&
        !widget.moments.moments.any((item) => identical(item, existing))) {
      setState(() => _draftError = '草稿版本已变化，请刷新列表后重新打开。');
      return;
    }
    final saved = existing == null
        ? await widget.moments.create(
            cityID: widget.cityID,
            title: _title.text.trim(),
            body: _body.text.trim(),
            placeID: _placeID,
            activityID: _activityID,
            communityID: _communityID,
            organizationID: _organizationID,
            time: _time!,
          )
        : await widget.moments.update(
            moment: existing,
            title: _title.text.trim(),
            body: _body.text.trim(),
            placeID: _placeID,
            time: _time!,
            activityID: _activityID == existing.activityIDs.firstOrNull
                ? null
                : _activityID,
            communityID: _communityID == existing.communityID
                ? null
                : _communityID,
            organizationID: _organizationID == existing.organizationID
                ? null
                : _organizationID,
          );
    if (mounted && _current && saved) Navigator.pop(context, true);
  }

  Future<void> _withdraw() async {
    if (!_current) return;
    final existing = widget.existing;
    if (existing == null) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('撤回私人草稿？'),
        content: const Text('撤回后，这份草稿会从个人草稿列表移除。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('保留'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('撤回'),
          ),
        ],
      ),
    );
    if (confirmed != true || !_current) {
      return;
    }
    final withdrawn = await widget.moments.withdraw(existing);
    if (mounted && _current && withdrawn) Navigator.pop(context, true);
  }

  bool get _imageSourceCurrent {
    final m=widget.existing;
    if(!_current||m==null||m.authorAccountID!=_draftOwner||m.visibility!='private'||m.status!='draft')return false;
    return widget.moments.moments.any((row)=>row.id==m.id&&row.authorAccountID==_draftOwner&&
      row.revision==m.revision&&row.visibility=='private'&&row.status=='draft');
  }
  Future<void> _managePrivateImage() async {
    if(!_imageSourceCurrent)return;
    final m=widget.existing!;
    final c=PrivateMomentMediaController(momentID:m.id,momentRevision:m.revision,
      authorizationHeader:()=>widget.auth.authorizationHeader,ownerID:()=>widget.auth.accountID,
      identityChanges:Listenable.merge([widget.auth,widget.moments]),
      workspaceChanges:widget.workspaceChanges,organizationWorkspaceID:widget.organizationWorkspaceID,
      sourceCurrent:()=>_imageSourceCurrent,client:widget.client,
      apiBaseUrl:widget.apiBaseUrl??BirdtieEnvironment.apiBaseUrl);
    try {await Navigator.push(context,MaterialPageRoute<void>(builder:(_)=>
      PrivateMomentMediaSheet(controller:c,momentTitle:m.title)));}finally{c.dispose();}
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: Listenable.merge([widget.moments, widget.auth]),
    builder: (context, _) => !_current
        ? SafeArea(
            child: Padding(
              padding: const EdgeInsets.all(24),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text('账号已切换，请重新打开私人草稿。'),
                  TextButton(
                    onPressed: () => Navigator.pop(context),
                    child: const Text('关闭'),
                  ),
                ],
              ),
            ),
          )
        : _buildForm(context),
  );

  Widget _buildForm(BuildContext context) => SafeArea(
    child: Padding(
      padding: EdgeInsets.fromLTRB(
        24,
        4,
        24,
        24 + MediaQuery.viewInsetsOf(context).bottom,
      ),
      child: SingleChildScrollView(
        child: Form(
          key: _formKey,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                widget.existing == null ? '新建动态' : '编辑动态',
                style: Theme.of(context).textTheme.headlineSmall,
              ),
              const SizedBox(height: 6),
              Text(
                '${widget.cityName} · ${widget.existing?.visibilityLabel ?? '仅自己可见'} · 本页不会发布',
                style: const TextStyle(color: _muted),
              ),
              const SizedBox(height: 20),
              if(_imageSourceCurrent) ...[
                OutlinedButton.icon(onPressed:widget.moments.saving?null:_managePrivateImage,
                  icon:const Icon(Icons.photo_outlined),label:const Text('管理私人图片')),
                const Text('图片只用于这条已保存的私人记录；公开或撤回记录会移除图片。'),
                const SizedBox(height:12),
              ],
              TextFormField(
                controller: _title,
                enabled: !widget.moments.saving,
                autofocus: true,
                maxLength: 160,
                decoration: const InputDecoration(labelText: '标题'),
                validator: (value) {
                  final title = value?.trim() ?? '';
                  if (title.isEmpty) return '请输入标题';
                  if (utf8.encode(title).length > 160) return '标题太长';
                  return null;
                },
              ),
              const SizedBox(height: 10),
              TextFormField(
                controller: _body,
                enabled: !widget.moments.saving,
                maxLines: 5,
                maxLength: 5000,
                decoration: const InputDecoration(labelText: '记录'),
                validator: (value) =>
                    utf8.encode(value?.trim() ?? '').length > 5000
                    ? '记录超过 5000 字节，请缩短内容'
                    : null,
              ),
              const SizedBox(height: 8),
              MomentTimeChoice(
                initialValue: widget.existing?.time ?? MomentTimeValue.unknown,
                enabled: !widget.moments.saving,
                onChanged: (value) => _time = value,
              ),
              if (widget.existing?.createdAt != null) ...[
                const SizedBox(height: 8),
                Text(
                  '创建于 ${widget.existing!.createdAt!.toIso8601String().replaceFirst('T', ' ')}（UTC），与发生时间分别保存。',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ],
              const SizedBox(height: 20),
              const Text(
                '关联情境（可选）',
                style: TextStyle(fontWeight: FontWeight.w600),
              ),
              const SizedBox(height: 6),
              const Text(
                '只关联你选择的真实地点、活动和已加入的社群或组织；草稿仍只对自己可见。',
                style: TextStyle(color: _muted),
              ),
              const SizedBox(height: 12),
              MomentContextChoice(
                label: '地点',
                value: _placeID,
                available: {
                  for (final place in widget.places) place.id: place.name,
                },
                onChanged: (value) => setState(() => _placeID = value),
              ),
              MomentContextChoice(
                label: '活动',
                value: _activityID,
                available: {
                  for (final activity in widget.activities)
                    activity.id: activity.title,
                },
                onChanged: (value) => setState(() => _activityID = value),
              ),
              TextButton(
                onPressed: _loadingContexts ? null : _loadContexts,
                child: Text(_loadingContexts ? '正在读取我的社群与组织…' : '选择我加入的社群或组织'),
              ),
              if (_contextError != null)
                Text(_contextError!, style: const TextStyle(color: _copper)),
              MomentContextChoice(
                label: '社群',
                value: _communityID,
                available: {
                  for (final item in _communities) item.id: item.name,
                },
                onChanged: (value) => setState(() => _communityID = value),
              ),
              MomentContextChoice(
                label: '组织',
                value: _organizationID,
                available: {
                  for (final item in _organizations) item.$1: item.$2,
                },
                onChanged: (value) => setState(() => _organizationID = value),
              ),
              const SizedBox(height: 8),
              AnimatedBuilder(
                animation: widget.moments,
                builder: (context, _) => Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    if (_draftError != null)
                      Text(
                        _draftError!,
                        style: const TextStyle(color: _copper),
                      ),
                    if (widget.moments.error != null) ...[
                      Text(
                        widget.moments.error!,
                        style: const TextStyle(color: _copper),
                      ),
                      const SizedBox(height: 8),
                    ],
                    if (widget.existing == null &&
                        widget.moments.creationUncertain)
                      TextButton(
                        onPressed: () => Navigator.pop(context, false),
                        child: const Text('返回列表检查保存结果'),
                      )
                    else
                      FilledButton(
                        onPressed: widget.moments.saving ? null : _save,
                        child: Text(widget.moments.saving ? '正在保存…' : '保存私人草稿'),
                      ),
                    if (widget.existing != null)
                      TextButton(
                        onPressed: widget.moments.saving ? null : _withdraw,
                        child: const Text('撤回草稿'),
                      ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    ),
  );
}

class _MyBirdtiePage extends StatelessWidget {
  const _MyBirdtiePage({
    required this.auth,
    required this.moments,
    required this.city,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
  });

  final BirdtieAuthController auth;
  final PrivateMomentController moments;
  final PublicCityController city;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;

  Future<void> _openDevPhoneLogin(BuildContext context) async {
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      backgroundColor: _surface,
      builder: (context) => DevPhoneLoginSheet(auth: auth),
    );
  }

  Future<void> _createMoment(BuildContext context) async {
    final selected = city.selectedCity;
    final draftToken = auth.authorizationHeader;
    final draftOwner = auth.accountID;
    if (!auth.signedIn || selected == null) return;
    var retired = false;
    bool current() =>
        !retired &&
        context.mounted &&
        auth.signedIn &&
        auth.accountID == draftOwner &&
        auth.authorizationHeader == draftToken;
    void observeActor() {
      if (!current()) retired = true;
    }
    auth.addListener(observeActor);
    try {
      if (moments.creationUncertain) {
        await moments.refresh();
        if (!context.mounted || !current()) return;
        final checked = await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            title: const Text('先检查上次保存结果'),
            content: const Text('上次保存的结果尚未确认。请检查私人草稿列表；再次保存同一内容可能产生重复草稿。'),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(context, false),
                child: const Text('返回检查'),
              ),
              TextButton(
                onPressed: () => Navigator.pop(context, true),
                child: const Text('已检查，仍要新建'),
              ),
            ],
          ),
        );
        if (checked != true || !current()) {
          return;
        }
        moments.confirmCreationChecked();
      }
      if (!context.mounted || !current()) return;
      await showModalBottomSheet<bool>(
        context: context,
        isScrollControlled: true,
        showDragHandle: true,
        backgroundColor: _surface,
        builder: (context) => _MomentDraftSheet(
          cityID: selected.id,
          cityName: selected.name,
          moments: moments,
          places: city.places,
          activities: city.activities,
          auth: auth,
          client:client,apiBaseUrl:apiBaseUrl,
          workspaceChanges:workspaceChanges,organizationWorkspaceID:organizationWorkspaceID,
        ),
      );
    } finally {
      auth.removeListener(observeActor);
    }
  }

  Future<void> _editMoment(BuildContext context, PrivateMoment moment) async {
    final cityNames = city.cities
        .where((item) => item.id == moment.cityID)
        .map((item) => item.name);
    await showModalBottomSheet<bool>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      backgroundColor: _surface,
      builder: (context) => _MomentDraftSheet(
        cityID: moment.cityID,
        cityName: cityNames.isEmpty ? moment.cityID : cityNames.first,
        moments: moments,
        places: city.places,
        activities: city.activities,
        auth: auth,
        existing: moment,
        client:client,apiBaseUrl:apiBaseUrl,
        workspaceChanges:workspaceChanges,organizationWorkspaceID:organizationWorkspaceID,
      ),
    );
  }

  @override
  Widget build(BuildContext context) => _PageFrame(
    eyebrow: '我的 Birdtie',
    title: '按自己的方式体验城市生活',
    subtitle: '个人资料、公开经历、收藏和私人素材会在同一处管理。',
    child: AnimatedBuilder(
      animation: Listenable.merge([auth, moments]),
      builder: (context, _) => Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _StatusLine(
            icon: auth.signedIn
                ? Icons.verified_user_outlined
                : Icons.person_outline,
            title: auth.signedIn ? (auth.displayName ?? 'Birdtie 账号') : '账号未连接',
            detail: auth.signedIn
                ? auth.loginMethod == 'dev_phone'
                      ? '本地测试会话；手机号所有权尚未验证。'
                      : '已通过 OIDC 登录。个人内容和编辑工作区会按权限逐步开放。'
                : '可使用下方可用的登录方式；浏览城市不需要登录。',
            accent: _forest,
          ),
          if (auth.error != null) ...[
            const SizedBox(height: 12),
            Text(auth.error!, style: const TextStyle(color: _copper)),
          ],
          const SizedBox(height: 16),
          if (auth.signedIn)
            OutlinedButton(
              onPressed: auth.busy ? null : auth.signOut,
              child: const Text('退出登录'),
            ),
          if (!auth.signedIn && auth.available)
            FilledButton(
              onPressed: auth.busy ? null : auth.signIn,
              child: Text(auth.busy ? '正在连接…' : '登录'),
            ),
          if (!auth.signedIn && auth.devPhoneAvailable)
            FilledButton(
              onPressed: auth.busy ? null : () => _openDevPhoneLogin(context),
              child: const Text('手机号测试登录'),
            ),
          if (!auth.signedIn && !auth.available && !auth.devPhoneAvailable)
            Text(
              auth.configurationChecked
                  ? '登录服务尚未配置或暂不可用。当前仍可浏览公开城市内容。'
                  : '正在检查登录服务…',
              style: const TextStyle(color: _muted),
            ),
          const SizedBox(height: 28),
          if (auth.signedIn) ...[
            PublicIntentSection(
              auth: auth,
              city: city,
              client: client,
              apiBaseUrl: apiBaseUrl,
              workspaceChanges: workspaceChanges,
              organizationWorkspaceID: organizationWorkspaceID,
            ),
            const SizedBox(height: 28),
            Row(
              children: [
                const Expanded(child: _SectionLabel('我的动态与草稿')),
                TextButton(
                  onPressed: city.selectedCity == null
                      ? null
                      : () => _createMoment(context),
                  child: const Text('新建草稿'),
                ),
                TextButton(
                  onPressed: moments.loading ? null : moments.refresh,
                  child: const Text('刷新'),
                ),
              ],
            ),
            const SizedBox(height: 8),
            if (moments.loading && moments.moments.isEmpty)
              const Text('正在读取我的动态与草稿…', style: TextStyle(color: _muted))
            else if (moments.error != null)
              Text(moments.error!, style: const TextStyle(color: _copper))
            else if (moments.moments.isEmpty)
              const Text('还没有动态或草稿。', style: TextStyle(color: _muted))
            else
              for (final moment in moments.moments) ...[
                const Divider(height: 22),
                Text(
                  moment.title,
                  style: const TextStyle(
                    color: _forest,
                    fontWeight: FontWeight.w600,
                  ),
                ),
                if (moment.body.isNotEmpty) ...[
                  const SizedBox(height: 4),
                  Text(
                    moment.body,
                    maxLines: 3,
                    overflow: TextOverflow.ellipsis,
                  ),
                ],
                const SizedBox(height: 4),
                Text(
                  '${moment.visibilityLabel} · ${moment.statusLabel} · ${moment.time.label}',
                  style: const TextStyle(color: _muted, fontSize: 12),
                ),
                if (moment.createdAt != null)
                  Text(
                    '创建于 ${moment.createdAt!.toIso8601String().replaceFirst('T', ' ')}（UTC）',
                    style: const TextStyle(color: _muted, fontSize: 12),
                  ),
                if (moment.placeID.isNotEmpty ||
                    moment.activityIDs.isNotEmpty ||
                    moment.communityID.isNotEmpty ||
                    moment.organizationID.isNotEmpty)
                  Text(
                    '已关联${[if (moment.placeID.isNotEmpty) '地点', if (moment.activityIDs.isNotEmpty) '活动', if (moment.communityID.isNotEmpty) '社群', if (moment.organizationID.isNotEmpty) '组织'].join('、')}',
                    style: const TextStyle(color: _muted, fontSize: 12),
                  ),
                if (moment.isEditableDraft)
                  TextButton(
                    onPressed: moments.saving
                        ? null
                        : () => _editMoment(context, moment),
                    child: const Text('编辑或撤回'),
                  ),
              ],
            const SizedBox(height: 28),
          ],
          const _SectionLabel('资料与身份'),
          const SizedBox(height: 8),
          const Text(
            '个人资料由本人管理。组织工作区请从侧栏切换到已授权的组织；城市只用于公开内容浏览。',
            style: TextStyle(color: _muted, height: 1.45),
          ),
        ],
      ),
    ),
  );
}

class _PageFrame extends StatelessWidget {
  const _PageFrame({
    required this.eyebrow,
    required this.title,
    required this.subtitle,
    required this.child,
    this.trailing,
  });

  final String eyebrow;
  final String title;
  final String subtitle;
  final Widget child;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final horizontal = constraints.maxWidth < 600 ? 22.0 : 48.0;
        final compact = constraints.maxWidth < 680;
        return SingleChildScrollView(
          padding: EdgeInsets.fromLTRB(horizontal, 22, horizontal, 38),
          child: Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 1000),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    eyebrow,
                    style: const TextStyle(
                      color: _copper,
                      fontSize: 10,
                      fontWeight: FontWeight.w700,
                      letterSpacing: 1.6,
                    ),
                  ),
                  const SizedBox(height: 9),
                  Row(
                    crossAxisAlignment: CrossAxisAlignment.end,
                    children: [
                      Expanded(
                        child: Text(
                          title,
                          style: Theme.of(context).textTheme.headlineMedium
                              ?.copyWith(
                                color: _forest,
                                fontWeight: FontWeight.w600,
                                letterSpacing: -0.8,
                              ),
                        ),
                      ),
                      if (trailing != null && !compact) ...[
                        const SizedBox(width: 12),
                        trailing!,
                      ],
                    ],
                  ),
                  const SizedBox(height: 7),
                  Text(
                    subtitle,
                    style: const TextStyle(color: _muted, height: 1.5),
                  ),
                  if (trailing != null && compact) ...[
                    const SizedBox(height: 14),
                    Align(alignment: Alignment.centerLeft, child: trailing!),
                  ],
                  const SizedBox(height: 28),
                  child,
                ],
              ),
            ),
          ),
        );
      },
    );
  }
}

class _SectionLabel extends StatelessWidget {
  const _SectionLabel(this.label);
  final String label;

  @override
  Widget build(BuildContext context) => Text(
    label,
    style: const TextStyle(
      color: _muted,
      fontSize: 10,
      fontWeight: FontWeight.w700,
      letterSpacing: 1.3,
    ),
  );
}

class _StatusLine extends StatelessWidget {
  const _StatusLine({
    required this.icon,
    required this.title,
    required this.detail,
    required this.accent,
  });

  final IconData icon;
  final String title;
  final String detail;
  final Color accent;

  @override
  Widget build(BuildContext context) => Container(
    width: double.infinity,
    padding: const EdgeInsets.symmetric(vertical: 18),
    decoration: const BoxDecoration(
      border: Border.symmetric(horizontal: BorderSide(color: _rule)),
    ),
    child: Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(icon, color: accent, size: 20),
        const SizedBox(width: 13),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                title,
                style: const TextStyle(
                  color: _forest,
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(height: 4),
              Text(detail, style: const TextStyle(color: _muted, height: 1.45)),
            ],
          ),
        ),
      ],
    ),
  );
}

class _EmptySection extends StatelessWidget {
  const _EmptySection({
    required this.icon,
    required this.title,
    required this.detail,
  });

  final IconData icon;
  final String title;
  final String detail;

  @override
  Widget build(BuildContext context) => Container(
    width: double.infinity,
    padding: const EdgeInsets.symmetric(vertical: 24),
    decoration: const BoxDecoration(
      border: Border(bottom: BorderSide(color: _rule)),
    ),
    child: Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(icon, color: _forest, size: 22),
        const SizedBox(width: 13),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                title,
                style: const TextStyle(
                  color: _forest,
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(height: 5),
              Text(detail, style: const TextStyle(color: _muted, height: 1.45)),
            ],
          ),
        ),
      ],
    ),
  );
}
