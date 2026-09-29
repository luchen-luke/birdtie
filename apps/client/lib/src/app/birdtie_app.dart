import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';

import '../auth/birdtie_auth_controller.dart';
import '../city/public_city_controller.dart';
import '../city/public_city_map.dart';
import '../content/private_moment_controller.dart';

const _forest = Color(0xFF193B32);
const _paper = Color(0xFFF5F4EF);
const _surface = Color(0xFFFCFBF8);
const _muted = Color(0xFF747B73);
const _rule = Color(0xFFDFE1D8);
const _moss = Color(0xFFE7EDE2);
const _copper = Color(0xFFB96743);

enum _Destination { now, explore, network, inbox }

class BirdtieApp extends StatelessWidget {
  const BirdtieApp({super.key});

  @override
  Widget build(BuildContext context) {
    final scheme = ColorScheme.fromSeed(
      seedColor: _forest,
      surface: _paper,
      brightness: Brightness.light,
    );
    return MaterialApp(
      title: 'Birdtie',
      debugShowCheckedModeBanner: false,
      theme: ThemeData(
        useMaterial3: true,
        colorScheme: scheme,
        scaffoldBackgroundColor: _paper,
        fontFamily: 'Aptos',
        dividerColor: _rule,
        splashFactory: InkSparkle.splashFactory,
        appBarTheme: const AppBarTheme(
          backgroundColor: _paper,
          foregroundColor: _forest,
          elevation: 0,
          scrolledUnderElevation: 0,
        ),
      ),
      home: const BirdtieShell(),
    );
  }
}

class BirdtieShell extends StatefulWidget {
  const BirdtieShell({super.key});

  @override
  State<BirdtieShell> createState() => _BirdtieShellState();
}

class _BirdtieShellState extends State<BirdtieShell> {
  _Destination _selected = _Destination.now;
  bool _showProfile = false;
  final BirdtieAuthController _auth = BirdtieAuthController();
  late final PublicCityController _city;
  late final PrivateMomentController _moments;
  bool _wasSignedIn = false;

  static const _labels = ['Now', 'Explore', 'Network', 'Inbox'];
  static const _icons = [
    Icons.wb_sunny_outlined,
    Icons.explore_outlined,
    Icons.hub_outlined,
    Icons.inbox_outlined,
  ];

  @override
  void initState() {
    super.initState();
    _city = PublicCityController(
      authorizationHeader: () => _auth.authorizationHeader,
    );
    _moments = PrivateMomentController(
      authorizationHeader: () => _auth.authorizationHeader,
    );
    _auth.addListener(_onAuthChange);
    if (Uri.base.queryParameters.keys.any(
      (key) => key == 'code' || key == 'challenge' || key == 'error',
    )) {
      _showProfile = true;
    }
    unawaited(_auth.initialize());
    unawaited(_city.loadCities());
  }

  @override
  void dispose() {
    _auth.removeListener(_onAuthChange);
    _auth.dispose();
    _city.dispose();
    _moments.dispose();
    super.dispose();
  }

  void _onAuthChange() {
    if (_wasSignedIn == _auth.signedIn) return;
    _wasSignedIn = _auth.signedIn;
    unawaited(_city.loadActivities());
    unawaited(_moments.refresh());
  }

  void _select(_Destination destination) {
    setState(() {
      _showProfile = false;
      _selected = destination;
    });
  }

  void _openProfile() => setState(() => _showProfile = true);

  Future<void> _showCreateOptions() async {
    final canDraft = _auth.signedIn && _city.selectedCity != null;
    final createMoment = await showModalBottomSheet<bool>(
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
              Text('Create', style: Theme.of(context).textTheme.headlineSmall),
              const SizedBox(height: 6),
              Text(
                canDraft ? '先保存仅自己可见的文字草稿。' : '创建私人草稿需要登录并选择城市。',
                style: const TextStyle(color: _muted),
              ),
              const SizedBox(height: 18),
              ListTile(
                contentPadding: EdgeInsets.zero,
                leading: const Icon(Icons.edit_note_outlined, color: _forest),
                title: const Text('Moment'),
                subtitle: const Text('保存一段私人文字草稿'),
                trailing: Text(
                  canDraft ? '创建草稿' : '需登录',
                  style: const TextStyle(color: _muted),
                ),
                enabled: canDraft,
                onTap: () => Navigator.pop(context, true),
              ),
              for (final item in const [
                ('Intent', '表达一件想做的事', Icons.near_me_outlined),
                ('Activity', '发布一场城市活动', Icons.event_outlined),
              ])
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  leading: Icon(item.$3, color: _forest),
                  title: Text(item.$1),
                  subtitle: Text(item.$2),
                  trailing: const Text('即将开放', style: TextStyle(color: _muted)),
                  enabled: false,
                ),
            ],
          ),
        ),
      ),
    );
    if (!mounted || createMoment != true) return;
    final selected = _city.selectedCity;
    if (!_auth.signedIn || selected == null) return;
    final saved = await showModalBottomSheet<bool>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      backgroundColor: _surface,
      builder: (context) => _MomentDraftSheet(
        cityID: selected.id,
        cityName: selected.name,
        moments: _moments,
      ),
    );
    if (!mounted || saved != true) return;
    setState(() => _showProfile = true);
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(const SnackBar(content: Text('Moment 已保存为私人草稿。')));
  }

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final wide = constraints.maxWidth >= 760;
        final pageIndex = _showProfile ? 4 : _selected.index;
        return Scaffold(
          body: SafeArea(
            bottom: false,
            child: Row(
              children: [
                if (wide)
                  _WideNavigation(
                    selected: _selected,
                    profileSelected: _showProfile,
                    onSelect: _select,
                    onProfile: _openProfile,
                    onCreate: _showCreateOptions,
                  ),
                Expanded(
                  child: Column(
                    children: [
                      _TopBar(
                        city: _city,
                        onProfile: _openProfile,
                        onCreate: _showCreateOptions,
                      ),
                      Expanded(
                        child: IndexedStack(
                          index: pageIndex,
                          children: [
                            _NowPage(city: _city),
                            _ExplorePage(city: _city),
                            const _NetworkPage(),
                            const _InboxPage(),
                            _MyBirdtiePage(
                              auth: _auth,
                              moments: _moments,
                              city: _city,
                            ),
                          ],
                        ),
                      ),
                      if (!wide)
                        _MobileNavigation(
                          selected: _selected,
                          onSelect: _select,
                        ),
                    ],
                  ),
                ),
              ],
            ),
          ),
        );
      },
    );
  }
}

class _TopBar extends StatelessWidget {
  const _TopBar({
    required this.city,
    required this.onProfile,
    required this.onCreate,
  });

  final PublicCityController city;
  final VoidCallback onProfile;
  final VoidCallback onCreate;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) => Padding(
        padding: const EdgeInsets.fromLTRB(22, 12, 22, 10),
        child: Row(
          children: [
            Container(
              width: 34,
              height: 34,
              decoration: const BoxDecoration(
                color: _forest,
                shape: BoxShape.circle,
              ),
              child: const Icon(Icons.near_me_rounded, color: _paper, size: 19),
            ),
            if (constraints.maxWidth > 390) ...[
              const SizedBox(width: 10),
              const Text(
                'birdtie',
                style: TextStyle(
                  color: _forest,
                  fontSize: 21,
                  fontWeight: FontWeight.w700,
                  letterSpacing: -0.6,
                ),
              ),
            ],
            const Spacer(),
            _CitySelector(city: city),
            const SizedBox(width: 10),
            IconButton.filledTonal(
              tooltip: 'Create',
              onPressed: onCreate,
              style: IconButton.styleFrom(
                backgroundColor: _moss,
                foregroundColor: _forest,
              ),
              icon: const Icon(Icons.add_rounded),
            ),
            const SizedBox(width: 4),
            IconButton(
              tooltip: 'My Birdtie',
              onPressed: onProfile,
              icon: const Icon(Icons.account_circle_outlined, color: _forest),
            ),
          ],
        ),
      ),
    );
  }
}

class _CitySelector extends StatelessWidget {
  const _CitySelector({required this.city});

  final PublicCityController city;

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: city,
    builder: (context, _) => PopupMenuButton<String>(
      tooltip: 'Choose a city',
      enabled: city.cities.length > 1,
      onSelected: city.selectCity,
      itemBuilder: (context) => [
        for (final option in city.cities)
          PopupMenuItem<String>(value: option.id, child: Text(option.name)),
      ],
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
        decoration: BoxDecoration(
          border: Border.all(color: _rule),
          borderRadius: BorderRadius.circular(24),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.location_city_outlined, color: _forest, size: 17),
            const SizedBox(width: 7),
            ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 140),
              child: Text(
                city.selectedCity?.name ??
                    (city.citiesLoading ? 'Loading city' : 'No city'),
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: const TextStyle(color: _forest, fontSize: 13),
              ),
            ),
            if (city.cities.length > 1) ...const [
              SizedBox(width: 3),
              Icon(Icons.keyboard_arrow_down_rounded, color: _muted, size: 17),
            ],
          ],
        ),
      ),
    ),
  );
}

class _MobileNavigation extends StatelessWidget {
  const _MobileNavigation({required this.selected, required this.onSelect});

  final _Destination selected;
  final ValueChanged<_Destination> onSelect;

  @override
  Widget build(BuildContext context) {
    final index = _Destination.values.indexOf(selected);
    return SafeArea(
      top: false,
      child: NavigationBar(
        selectedIndex: index,
        backgroundColor: _surface,
        indicatorColor: _moss,
        onDestinationSelected: (i) => onSelect(_Destination.values[i]),
        destinations: [
          for (var i = 0; i < _Destination.values.length; i++)
            NavigationDestination(
              icon: Icon(_BirdtieShellState._icons[i]),
              selectedIcon: Icon(_BirdtieShellState._icons[i], color: _forest),
              label: _BirdtieShellState._labels[i],
            ),
        ],
      ),
    );
  }
}

class _WideNavigation extends StatelessWidget {
  const _WideNavigation({
    required this.selected,
    required this.profileSelected,
    required this.onSelect,
    required this.onProfile,
    required this.onCreate,
  });

  final _Destination selected;
  final bool profileSelected;
  final ValueChanged<_Destination> onSelect;
  final VoidCallback onProfile;
  final VoidCallback onCreate;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 212,
      decoration: const BoxDecoration(
        color: _surface,
        border: Border(right: BorderSide(color: _rule)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const SizedBox(height: 24),
          const Padding(
            padding: EdgeInsets.symmetric(horizontal: 22),
            child: Text(
              'CITY LIFE,\nIN CONTEXT.',
              style: TextStyle(
                color: _muted,
                fontSize: 10,
                height: 1.5,
                fontWeight: FontWeight.w700,
                letterSpacing: 1.4,
              ),
            ),
          ),
          const SizedBox(height: 22),
          for (var i = 0; i < _Destination.values.length; i++)
            _RailItem(
              label: _BirdtieShellState._labels[i],
              icon: _BirdtieShellState._icons[i],
              selected: !profileSelected && selected.index == i,
              onTap: () => onSelect(_Destination.values[i]),
            ),
          const Spacer(),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 14),
            child: OutlinedButton.icon(
              onPressed: onCreate,
              icon: const Icon(Icons.add_rounded, size: 18),
              label: const Text('Create'),
              style: OutlinedButton.styleFrom(
                foregroundColor: _forest,
                side: const BorderSide(color: _rule),
                padding: const EdgeInsets.symmetric(vertical: 14),
              ),
            ),
          ),
          _RailItem(
            label: 'My Birdtie',
            icon: Icons.account_circle_outlined,
            selected: profileSelected,
            onTap: onProfile,
          ),
          const SizedBox(height: 18),
        ],
      ),
    );
  }
}

class _RailItem extends StatelessWidget {
  const _RailItem({
    required this.label,
    required this.icon,
    required this.selected,
    required this.onTap,
  });

  final String label;
  final IconData icon;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(12, 3, 12, 3),
      child: ListTile(
        selected: selected,
        selectedTileColor: _moss,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
        leading: Icon(icon, color: selected ? _forest : _muted, size: 21),
        title: Text(
          label,
          style: TextStyle(
            color: selected ? _forest : _muted,
            fontWeight: selected ? FontWeight.w600 : FontWeight.w400,
          ),
        ),
        onTap: onTap,
      ),
    );
  }
}

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

class _NetworkPage extends StatelessWidget {
  const _NetworkPage();

  @override
  Widget build(BuildContext context) => _PageFrame(
    eyebrow: 'NETWORK',
    title: 'Find your people',
    subtitle: '从公开的兴趣和 Intent 开始；建立连接前双方都要同意。',
    child: const _EmptySection(
      icon: Icons.hub_outlined,
      title: 'Network 尚未开放',
      detail: 'People、社区和限时 Intent 会在连接请求与隐私规则就绪后开放。',
    ),
  );
}

class _InboxPage extends StatelessWidget {
  const _InboxPage();

  @override
  Widget build(BuildContext context) => _PageFrame(
    eyebrow: 'INBOX',
    title: 'Your conversations',
    subtitle: '联系请求、已接受的会话，以及需要真人接手的 Agent 对话。',
    child: const _EmptySection(
      icon: Icons.inbox_outlined,
      title: 'Inbox 尚未连接',
      detail: '联系请求与会话会在账号和双向同意流程完成后启用。',
    ),
  );
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
                ? '已通过 OIDC 登录。个人内容和编辑工作区会按权限逐步开放。'
                : '可使用已配置的身份提供方登录；浏览城市不需要登录。',
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
            )
          else if (auth.available)
            FilledButton(
              onPressed: auth.busy ? null : auth.signIn,
              child: Text(auth.busy ? 'Connecting…' : 'Sign in'),
            )
          else
            Text(
              auth.configurationChecked
                  ? '登录服务尚未配置或暂不可用。当前仍可浏览公开城市内容。'
                  : '正在检查登录服务…',
              style: const TextStyle(color: _muted),
            ),
          const SizedBox(height: 28),
          if (auth.signedIn) ...[
            Row(
              children: [
                const Expanded(child: _SectionLabel('PRIVATE MOMENT DRAFTS')),
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
