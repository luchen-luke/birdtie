import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';

import '../auth/birdtie_auth_controller.dart';
import '../auth/dev_phone_login_sheet.dart';
import '../city/public_city_controller.dart';
import '../city/public_city_map.dart';
import '../content/private_moment_controller.dart';
import '../content/public_intent_section.dart';

// Existing City and private Moment surfaces remain available from the V2 sidebar.
class LegacyProfilePage extends StatelessWidget {
  const LegacyProfilePage({
    super.key,
    required this.auth,
    required this.moments,
    required this.city,
  });

  final BirdtieAuthController auth;
  final PrivateMomentController moments;
  final PublicCityController city;

  @override
  Widget build(BuildContext context) =>
      _MyBirdtiePage(auth: auth, moments: moments, city: city);
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
        title: 'Today in the city',
        subtitle: '近期活动、城市故事与正在寻找同行者的 Intent。',
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            _CityState(city: city),
            const SizedBox(height: 34),
            const _SectionLabel('CITY PULSE'),
            const SizedBox(height: 12),
            _ActivitySection(city: city),
            const SizedBox(height: 30),
            const _SectionLabel('EXPLORE THE CITY'),
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
        detail: 'City Seed 活动完成来源审核后才会显示。Moment 和 Intent 尚未接入。',
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
                ? '尚未配置 City API 地址。'
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
      eyebrow: 'EXPLORE ANYWHERE',
      title: 'Explore ${widget.city.selectedCity?.name ?? 'a city'}',
      subtitle: '无需分享设备定位。已发布地点来自当前选定城市。',
      trailing: SegmentedButton<bool>(
        showSelectedIcon: false,
        segments: const [
          ButtonSegment(
            value: true,
            icon: Icon(Icons.map_outlined),
            label: Text('Map'),
          ),
          ButtonSegment(
            value: false,
            icon: Icon(Icons.view_list_outlined),
            label: Text('List'),
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
          const _SectionLabel('CITY CONTENT'),
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
            '公开 Moment 与 Journey 尚未接入；Map 与 List 共用当前城市和搜索条件。',
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
    this.existing,
  });

  final String cityID;
  final String cityName;
  final PrivateMomentController moments;
  final PrivateMoment? existing;

  @override
  State<_MomentDraftSheet> createState() => _MomentDraftSheetState();
}

class _MomentDraftSheetState extends State<_MomentDraftSheet> {
  final _formKey = GlobalKey<FormState>();
  late final TextEditingController _title;
  late final TextEditingController _body;

  @override
  void initState() {
    super.initState();
    _title = TextEditingController(text: widget.existing?.title);
    _body = TextEditingController(text: widget.existing?.body);
  }

  @override
  void dispose() {
    _title.dispose();
    _body.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    if (!_formKey.currentState!.validate()) return;
    final existing = widget.existing;
    final saved = existing == null
        ? await widget.moments.create(
            cityID: widget.cityID,
            title: _title.text.trim(),
            body: _body.text.trim(),
          )
        : await widget.moments.update(
            moment: existing,
            title: _title.text.trim(),
            body: _body.text.trim(),
          );
    if (mounted && saved) Navigator.pop(context, true);
  }

  Future<void> _withdraw() async {
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
    if (confirmed != true) return;
    final withdrawn = await widget.moments.withdraw(existing);
    if (mounted && withdrawn) Navigator.pop(context, true);
  }

  @override
  Widget build(BuildContext context) => SafeArea(
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
                widget.existing == null ? 'New Moment' : 'Edit Moment',
                style: Theme.of(context).textTheme.headlineSmall,
              ),
              const SizedBox(height: 6),
              Text(
                '${widget.cityName} · 仅自己可见 · 不会发布',
                style: const TextStyle(color: _muted),
              ),
              const SizedBox(height: 20),
              TextFormField(
                controller: _title,
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
                maxLines: 5,
                maxLength: 5000,
                decoration: const InputDecoration(labelText: '记录'),
                validator: (value) =>
                    utf8.encode(value?.trim() ?? '').length > 5000
                    ? '记录超过 5000 字节，请缩短内容'
                    : null,
              ),
              const SizedBox(height: 8),
              AnimatedBuilder(
                animation: widget.moments,
                builder: (context, _) => Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    if (widget.moments.error != null) ...[
                      Text(
                        widget.moments.error!,
                        style: const TextStyle(color: _copper),
                      ),
                      const SizedBox(height: 8),
                    ],
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
  });

  final BirdtieAuthController auth;
  final PrivateMomentController moments;
  final PublicCityController city;

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
    if (!auth.signedIn || selected == null) return;
    await showModalBottomSheet<bool>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      backgroundColor: _surface,
      builder: (context) => _MomentDraftSheet(
        cityID: selected.id,
        cityName: selected.name,
        moments: moments,
      ),
    );
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
        existing: moment,
      ),
    );
  }

  @override
  Widget build(BuildContext context) => _PageFrame(
    eyebrow: 'MY BIRDTIE',
    title: 'Your city life, on your terms',
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
            title: auth.signedIn
                ? (auth.displayName ?? 'Birdtie account')
                : 'Account not connected',
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
              child: const Text('Sign out'),
            ),
          if (!auth.signedIn && auth.available)
            FilledButton(
              onPressed: auth.busy ? null : auth.signIn,
              child: Text(auth.busy ? 'Connecting…' : 'Sign in'),
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
            PublicIntentSection(auth: auth, city: city),
            const SizedBox(height: 28),
            Row(
              children: [
                const Expanded(child: _SectionLabel('PRIVATE MOMENT DRAFTS')),
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
              const Text('正在读取私人草稿…', style: TextStyle(color: _muted))
            else if (moments.error != null)
              Text(moments.error!, style: const TextStyle(color: _copper))
            else if (moments.moments.isEmpty)
              const Text('还没有私人 Moment 草稿。', style: TextStyle(color: _muted))
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
                  '仅自己可见 · ${moment.status}',
                  style: const TextStyle(color: _muted, fontSize: 12),
                ),
                if (moment.isTextOnlyDraft)
                  TextButton(
                    onPressed: moments.saving
                        ? null
                        : () => _editMoment(context, moment),
                    child: const Text('编辑或撤回'),
                  ),
              ],
            const SizedBox(height: 28),
          ],
          const _SectionLabel('PRIVATE WORKSPACE'),
          const SizedBox(height: 8),
          const _WorkspaceRow(
            icon: Icons.auto_awesome_outlined,
            title: 'Personal Agent',
            detail: '私人资料整理、授权和可审阅草稿',
          ),
          const Divider(height: 1),
          const _WorkspaceRow(
            icon: Icons.apartment_outlined,
            title: 'Organization / City workspace',
            detail: '仅对有维护权限的组织成员开放',
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

class _WorkspaceRow extends StatelessWidget {
  const _WorkspaceRow({
    required this.icon,
    required this.title,
    required this.detail,
  });

  final IconData icon;
  final String title;
  final String detail;

  @override
  Widget build(BuildContext context) => ListTile(
    contentPadding: EdgeInsets.zero,
    leading: Icon(icon, color: _forest),
    title: Text(title, style: const TextStyle(color: _forest)),
    subtitle: Text(detail, style: const TextStyle(color: _muted)),
    trailing: const Text('规划中', style: TextStyle(color: _muted, fontSize: 12)),
  );
}
