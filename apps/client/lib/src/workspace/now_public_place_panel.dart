import 'dart:async';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../auth/birdtie_auth_controller.dart';
import '../city/public_city_controller.dart';
import 'map_layers_api.dart';
import 'map_layers_controller.dart';
import 'place_detail_sheet.dart';
import 'public_moment_page.dart';
import 'public_person_page.dart';
import 'public_author_moments.dart';

/// A read path over the original map projection and publication/profile APIs.
/// The parent keeps its map, composer, task and viewport mounted throughout.
class NowPublicPlacePanel extends StatefulWidget {
  const NowPublicPlacePanel({
    super.key,
    required this.item,
    required this.auth,
    required this.identityChanges,
    required this.workspaceID,
    required this.current,
    required this.revalidate,
    required this.onClose,
    required this.onOpenActivity,
    this.apiBaseUrl,
    this.client,
    this.onOpenOwnProfile,
    this.authorLayers,
    this.onOpenAuthorScope,
    this.onLeaveAuthorScope,
    this.onAuthorMapVisible,
    this.onSelectAuthorMoment,
    this.authorSelectionChanges,
    this.selectedAuthorMomentID,
    this.initialMomentID,
    this.initialObjectDetail = false,
  });
  final TypedMapItem item;
  final BirdtieAuthController auth;
  final Listenable identityChanges;
  final String? Function() workspaceID;
  final bool Function() current;
  final Future<bool> Function() revalidate;
  final VoidCallback onClose;
  final ValueChanged<PublicActivity> onOpenActivity;
  final String? apiBaseUrl;
  final http.Client? client;
  final Future<void> Function()? onOpenOwnProfile;
  final MapLayersController? authorLayers;
  final Future<bool> Function(String)? onOpenAuthorScope;
  final VoidCallback? onLeaveAuthorScope;
  final ValueChanged<bool>? onAuthorMapVisible;
  final ValueChanged<TypedMapItem>? onSelectAuthorMoment;
  final Listenable? authorSelectionChanges;
  final String? Function()? selectedAuthorMomentID;
  final String? initialMomentID;
  final bool initialObjectDetail;

  @override
  State<NowPublicPlacePanel> createState() => _NowPublicPlacePanelState();
}

typedef _PanelFrame = ({String kind, String id});

class _NowPublicPlacePanelState extends State<NowPublicPlacePanel> {
  late final List<_PanelFrame> _frames = [
    if (widget.initialObjectDetail)
      (kind: 'moment', id: widget.item.detailID)
    else ...[
      (kind: 'place', id: widget.item.placeID),
      if (widget.initialMomentID case final id?) (kind: 'moment', id: id),
    ],
  ];
  final _reads = <String, int>{};
  final _storage = PageStorageBucket();
  // Place's original connection fence treats provider replacement as retirement.
  // Overlay rebuilds therefore reuse this exact provider, including during back.
  late final String? Function() _authorization = () =>
      widget.auth.authorizationHeader;
  late final Future<void> Function() _ownProfileNavigation = _openOwnProfile;
  late final Future<void> Function() _publicMomentsNavigation =
      _openPublicMoments;
  ({
    String author,
    String city,
    int epoch,
    String? token,
    String? account,
    String? org,
  })?
  _authorBinding;
  final _authorDetails = <String, TypedMapItem>{};
  bool _authorRetired = false,
      _authorMapVisible = false,
      _authorAttempt = false;
  bool _openingAuthorMoment = false;
  String? _authorNotice;
  bool _retired = false, _checking = false;
  String _key(_PanelFrame f) => '${f.kind}:${f.id}';
  String _frameKey(int index) {
    final authorIndex = _frames.indexWhere((f) => f.kind == 'author-moments');
    final prefix = authorIndex >= 0 && index > authorIndex
        ? 'author-read:'
        : '';
    return '$prefix${_key(_frames[index])}';
  }

  @override
  void initState() {
    super.initState();
    _retired =
        !widget.current() ||
        (widget.initialObjectDetail &&
            (widget.item.kind != 'MOMENT' ||
                widget.item.detailType != 'MOMENT' ||
                widget.item.detailID != widget.item.id ||
                widget.initialMomentID != widget.item.detailID));
    widget.identityChanges.addListener(_changed);
    widget.authorLayers?.addListener(_authorChanged);
    widget.authorSelectionChanges?.addListener(_authorSelectionChanged);
  }

  @override
  void didUpdateWidget(NowPublicPlacePanel old) {
    super.didUpdateWidget(old);
    if (!identical(old.authorLayers, widget.authorLayers)) {
      old.authorLayers?.removeListener(_authorChanged);
      widget.authorLayers?.addListener(_authorChanged);
      _authorRetired = _authorBinding != null;
    }
    if (!identical(old.onOpenAuthorScope, widget.onOpenAuthorScope)) {
      _authorRetired = _authorBinding != null;
    }
    if (!identical(old.authorSelectionChanges, widget.authorSelectionChanges)) {
      old.authorSelectionChanges?.removeListener(_authorSelectionChanged);
      widget.authorSelectionChanges?.addListener(_authorSelectionChanged);
    }
  }

  void _changed() {
    if (_retired || widget.current()) return;
    // An identity/source ABA cannot revive these previously fetched children.
    _retired = true;
    _authorRetired = true;
    if (mounted) setState(() {});
  }

  @override
  void dispose() {
    widget.identityChanges.removeListener(_changed);
    widget.authorLayers?.removeListener(_authorChanged);
    widget.authorSelectionChanges?.removeListener(_authorSelectionChanged);
    super.dispose();
  }

  void _authorSelectionChanged() {
    if (!_authorMapVisible ||
        _checking ||
        _openingAuthorMoment ||
        _frames.last.kind != 'author-moments' ||
        !_authorScopeCurrent())
      return;
    final selected = widget.selectedAuthorMomentID?.call();
    final item = selected == null ? null : widget.authorLayers?.find(selected);
    if (item != null && item.kind == 'MOMENT')
      unawaited(_openAuthorMoment(item));
  }

  bool get _inAuthorScope => _frames.any((f) => f.kind == 'author-moments');
  bool _authorScopeCurrent() {
    final binding = _authorBinding, layers = widget.authorLayers;
    final view = layers?.authorView;
    return binding != null &&
        layers != null &&
        view != null &&
        !_retired &&
        !_authorRetired &&
        widget.current() &&
        binding.epoch == layers.epoch &&
        binding.city == layers.cityID &&
        binding.city == layers.authorCityID &&
        binding.city == view.cityID &&
        binding.author == layers.authorID &&
        binding.author == view.authorID &&
        view.scope == 'PUBLIC_AUTHOR' &&
        view.validUntil.isAfter(DateTime.now().toUtc()) &&
        layers.authorError == null &&
        !layers.authorLoading &&
        binding.token == widget.auth.authorizationHeader &&
        binding.account == widget.auth.accountID &&
        binding.org == widget.workspaceID() &&
        binding.org == null &&
        identical(layers.items, view.items) &&
        view.items.every(
          (i) =>
              i.kind == 'MOMENT' &&
              i.detailType == 'MOMENT' &&
              i.detailID == i.id,
        );
  }

  void _authorChanged() {
    final binding = _authorBinding, layers = widget.authorLayers;
    if (binding == null || layers == null || !mounted) return;
    if (binding.epoch != layers.epoch ||
        binding.city != layers.cityID ||
        binding.author != layers.authorID ||
        binding.token != widget.auth.authorizationHeader ||
        binding.account != widget.auth.accountID ||
        binding.org != widget.workspaceID() ||
        (!layers.authorLoading &&
            (layers.authorError != null || layers.authorView == null))) {
      _authorRetired = true;
    }
    if (_authorRetired && _authorMapVisible) {
      _authorMapVisible = false;
      widget.onAuthorMapVisible?.call(false);
    }
    setState(() {});
  }

  void _leaveAuthor({bool keepNotice = false}) {
    if (_authorBinding == null && !_authorMapVisible && !_authorAttempt) return;
    // Retire first: synchronous controller notifications cannot revive a frame.
    _authorBinding = null;
    _authorRetired = true;
    _authorDetails.clear();
    _authorMapVisible = false;
    _authorAttempt = false;
    if (!keepNotice) _authorNotice = null;
    widget.onLeaveAuthorScope?.call();
    widget.onAuthorMapVisible?.call(false);
  }

  void _close() {
    _leaveAuthor();
    widget.onClose();
  }

  Future<void> _openPublicMoments() async {
    final frame = _frames.last,
        layers = widget.authorLayers,
        open = widget.onOpenAuthorScope;
    if (frame.kind != 'author' ||
        layers == null ||
        open == null ||
        widget.workspaceID() != null ||
        !await _verify() ||
        !mounted ||
        _frames.last != frame)
      return;
    if (_inAuthorScope && !await _refreshAuthor()) return;
    if (!mounted || _frames.last != frame) return;
    final epoch = layers.epoch, city = layers.cityID;
    final token = widget.auth.authorizationHeader,
        account = widget.auth.accountID;
    setState(() {
      _checking = true;
      _authorAttempt = true;
    });
    var loaded = false;
    try {
      loaded = await open(frame.id);
    } catch (_) {
      loaded = false;
    }
    if (!mounted) return;
    final bound =
        !_retired &&
        widget.current() &&
        _frames.last == frame &&
        identical(layers, widget.authorLayers) &&
        identical(open, widget.onOpenAuthorScope) &&
        epoch == layers.epoch &&
        city != null &&
        city == layers.cityID &&
        token == widget.auth.authorizationHeader &&
        account == widget.auth.accountID &&
        widget.workspaceID() == null;
    setState(() {
      _checking = false;
      if (bound && loaded) {
        _authorBinding = (
          author: frame.id,
          city: city,
          epoch: epoch,
          token: token,
          account: account,
          org: null,
        );
        _authorRetired = false;
        _authorNotice = null;
        if (_authorScopeCurrent()) {
          _frames.add((kind: 'author-moments', id: frame.id));
        } else {
          _authorRetired = true;
        }
      }
      if (!bound || !loaded || !_inAuthorScope) {
        _authorNotice = '作者当前范围的公开动态暂不可用，请返回重新打开。';
      }
    });
    if (!bound || !loaded || !_inAuthorScope) {
      _leaveAuthor(keepNotice: true);
    }
  }

  Future<bool> _refreshAuthor() async {
    final binding = _authorBinding,
        layers = widget.authorLayers,
        open = widget.onOpenAuthorScope;
    if (binding == null ||
        layers == null ||
        open == null ||
        !_authorScopeCurrent())
      return false;
    var loaded = false;
    try {
      loaded = await open(binding.author);
    } catch (_) {
      loaded = false;
    }
    if (!mounted ||
        _authorBinding != binding ||
        !identical(layers, widget.authorLayers) ||
        !identical(open, widget.onOpenAuthorScope))
      return false;
    if (!loaded || !_authorScopeCurrent()) {
      setState(() => _authorRetired = true);
      return false;
    }
    return true;
  }

  bool _sameAuthorItem(TypedMapItem a, TypedMapItem? b) =>
      b != null &&
      a.kind == b.kind &&
      a.id == b.id &&
      a.detailType == b.detailType &&
      a.detailID == b.detailID &&
      a.sourceVersion == b.sourceVersion &&
      a.placeID == b.placeID &&
      a.latitude == b.latitude &&
      a.longitude == b.longitude;

  Future<void> _openAuthorMoment(TypedMapItem item) async {
    if (_openingAuthorMoment) return;
    _openingAuthorMoment = true;
    try {
      final frame = _frames.last;
      if (frame.kind != 'author-moments' ||
          !_authorScopeCurrent() ||
          !await _verify() ||
          !mounted ||
          _frames.last != frame ||
          !await _refreshAuthor() ||
          !mounted ||
          _frames.last != frame)
        return;
      final fresh = widget.authorLayers!.find(item.mapID);
      if (!_sameAuthorItem(item, fresh)) {
        setState(() => _authorNotice = '这条动态已变化或不可访问，请从当前列表重新选择。');
        return;
      }
      widget.onSelectAuthorMoment?.call(fresh!);
      if (!mounted || !_authorScopeCurrent() || _frames.last != frame) return;
      setState(() {
        _authorNotice = null;
        _authorDetails[item.id] = fresh!;
        _frames.add((kind: 'moment', id: item.detailID));
        _authorMapVisible = false;
      });
      widget.onAuthorMapVisible?.call(false);
    } finally {
      _openingAuthorMoment = false;
    }
  }

  Future<void> _toggleAuthorMap() async {
    if (widget.onAuthorMapVisible == null ||
        !_inAuthorScope ||
        !_authorScopeCurrent() ||
        !await _verify() ||
        !mounted ||
        !await _refreshAuthor() ||
        !mounted)
      return;
    setState(() => _authorMapVisible = !_authorMapVisible);
    widget.onAuthorMapVisible!(_authorMapVisible);
  }

  Future<bool> _verify() async {
    if (_checking || _retired || !widget.current()) {
      _changed();
      return false;
    }
    setState(() => _checking = true);
    bool valid = false;
    try {
      valid = await widget.revalidate();
    } catch (_) {
      valid = false;
    }
    if (!mounted) return false;
    valid = valid && !_retired && widget.current();
    setState(() {
      _checking = false;
      if (!valid) _retired = true;
    });
    return valid;
  }

  Future<void> _open(String kind, String id) async {
    if (!await _verify() || !mounted) return;
    if (_inAuthorScope && !await _refreshAuthor()) return;
    if (!mounted) return;
    setState(() {
      final prior = _frames.indexWhere((f) => f.kind == kind && f.id == id);
      if (prior >= 0) {
        _frames.removeRange(prior + 1, _frames.length);
        _reads.update(
          _frameKey(_frames.length - 1),
          (n) => n + 1,
          ifAbsent: () => 1,
        );
      } else {
        _frames.add((kind: kind, id: id));
      }
      if (!_inAuthorScope) _leaveAuthor();
    });
  }

  Future<void> _back() async {
    if (_frames.length == 1 || _retired) {
      _close();
      return;
    }
    if (!await _verify() || !mounted) return;
    // An unavailable author scope can be left; it cannot authorize old details.
    if (_inAuthorScope &&
        _frames.last.kind != 'author-moments' &&
        !_authorRetired &&
        !await _refreshAuthor())
      return;
    if (!mounted) return;
    setState(() {
      _frames.removeLast();
      if (!_inAuthorScope) _leaveAuthor();
      _reads.update(
        _frameKey(_frames.length - 1),
        (n) => n + 1,
        ifAbsent: () => 1,
      );
    });
  }

  Future<void> _openOwnProfile() async {
    final open = widget.onOpenOwnProfile;
    final frame = _frames.last;
    if (open == null ||
        frame.kind != 'author' ||
        frame.id != widget.auth.accountID ||
        !widget.auth.signedIn ||
        widget.workspaceID() != null ||
        !await _verify() ||
        !mounted) {
      return;
    }
    if (_frames.last != frame ||
        widget.onOpenOwnProfile != open ||
        frame.id != widget.auth.accountID ||
        widget.workspaceID() != null) {
      return;
    }
    // This callback opens the original owner editor. The public author's read
    // and any subsequent write still use their own current server contracts.
    if (_inAuthorScope && !await _refreshAuthor()) return;
    if (!mounted || _frames.last != frame) return;
    await open();
  }

  Widget _content(_PanelFrame frame, int index) {
    final frameKey = _frameKey(index);
    final read = _reads[frameKey] ?? 0;
    final authorIndex = _frames.indexWhere((f) => f.kind == 'author-moments');
    final inAuthorRead = authorIndex >= 0 && index > authorIndex;
    final selected = _authorDetails[frame.id];
    final allowed =
        !inAuthorRead ||
        (_authorScopeCurrent() &&
            (frame.kind != 'moment' ||
                (selected != null &&
                    _sameAuthorItem(
                      selected,
                      widget.authorLayers?.find(selected.mapID),
                    ))));
    final content = KeyedSubtree(
      key: ValueKey('$frameKey:$read'),
      child: switch (frame.kind) {
        'moment' => PublicMomentPage(
          key: PageStorageKey(frameKey),
          embedded: true,
          momentID: frame.id,
          authorizationHeader: _authorization,
          identityChanges: widget.identityChanges,
          workspaceID: widget.workspaceID,
          apiBaseUrl: widget.apiBaseUrl,
          client: widget.client,
          onOpenPlace: (id) => unawaited(_open('place', id)),
          onOpenAuthor: (id) => unawaited(_open('author', id)),
        ),
        'author' => PublicPersonPage(
          key: PageStorageKey(frameKey),
          embedded: true,
          accountID: frame.id,
          authorizationHeader: _authorization,
          identityChanges: widget.identityChanges,
          workspaceID: widget.workspaceID,
          auth: widget.auth,
          onOpenOwnProfile: widget.onOpenOwnProfile == null
              ? null
              : _ownProfileNavigation,
          onOpenPublicMoments:
              widget.authorLayers == null ||
                  widget.onOpenAuthorScope == null ||
                  _authorNotice != null
              ? null
              : _publicMomentsNavigation,
          apiBaseUrl: widget.apiBaseUrl,
          client: widget.client,
        ),
        'author-moments' => PublicAuthorMoments(
          layers: widget.authorLayers!,
          authorID: frame.id,
          authorScopeCurrent: _authorScopeCurrent,
          notice: _authorNotice,
          onOpenMoment: (item) => unawaited(_openAuthorMoment(item)),
        ),
        _ => PlaceDetailSheet(
          publicContentFirst: true,
          key: PageStorageKey(frameKey),
          placeID: frame.id,
          authorizationHeader: _authorization,
          organizationWorkspaceID: widget.workspaceID,
          ownerID: () =>
              !_retired && widget.current() ? widget.auth.accountID : null,
          workspaceChanges: widget.identityChanges,
          apiBaseUrl: widget.apiBaseUrl,
          client: widget.client,
          onOpenActivity: widget.onOpenActivity,
          onOpenMoment: (id) => unawaited(_open('moment', id)),
        ),
      },
    );
    if (!inAuthorRead) return content;
    // Keep the reader and scroll state mounted, but never show a retired body.
    return Stack(
      key: ValueKey('author-read-host:$frameKey'),
      children: [
        Positioned.fill(
          child: Offstage(offstage: !allowed, child: content),
        ),
        if (!allowed)
          const Center(
            child: Padding(
              padding: EdgeInsets.all(24),
              child: Text('该动态或作者范围已变化，请返回当前列表重新核实。'),
            ),
          ),
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final valid = !_retired && widget.current();
    return PopScope<void>(
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop && !_checking) unawaited(_back());
      },
      child: Material(
        key: const Key('now-public-place-panel'),
        color: Theme.of(context).colorScheme.surface,
        borderRadius: const BorderRadius.vertical(top: Radius.circular(26)),
        clipBehavior: Clip.antiAlias,
        child: SafeArea(
          top: false,
          child: Column(
            children: [
              const SizedBox(height: 10),
              Container(
                width: 36,
                height: 4,
                decoration: BoxDecoration(
                  color: Theme.of(context).colorScheme.outlineVariant,
                  borderRadius: BorderRadius.circular(2),
                ),
              ),
              Row(
                children: [
                  if (_frames.length > 1 && valid)
                    IconButton(
                      tooltip: '返回上一层',
                      onPressed: _checking ? null : () => unawaited(_back()),
                      constraints: const BoxConstraints(
                        minWidth: 48,
                        minHeight: 48,
                      ),
                      icon: const Icon(Icons.arrow_back),
                    )
                  else
                    const SizedBox(width: 16),
                  Expanded(
                    child: Text(
                      !valid
                          ? '请重新打开内容'
                          : switch (_frames.last.kind) {
                              'moment' => '公开动态',
                              'author' => '作者主页',
                              'author-moments' => '作者公开动态',
                              _ => '地点与公开动态',
                            },
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                  ),
                  IconButton(
                    tooltip: '关闭内容返回地图',
                    constraints: const BoxConstraints(
                      minWidth: 48,
                      minHeight: 48,
                    ),
                    onPressed: _close,
                    icon: const Icon(Icons.close),
                  ),
                ],
              ),
              if (valid &&
                  _frames.last.kind == 'author' &&
                  _authorNotice != null)
                Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 24),
                  child: Text(_authorNotice!),
                ),
              if (valid &&
                  _frames.last.kind == 'author-moments' &&
                  widget.onAuthorMapVisible != null &&
                  _authorScopeCurrent())
                TextButton.icon(
                  onPressed: _checking
                      ? null
                      : () => unawaited(_toggleAuthorMap()),
                  style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
                  icon: Icon(
                    _authorMapVisible ? Icons.list : Icons.map_outlined,
                  ),
                  label: Text(_authorMapVisible ? '展开作者动态' : '查看作者地图'),
                ),
              Expanded(
                child: !valid
                    ? const Center(
                        child: Padding(
                          padding: EdgeInsets.all(24),
                          child: Text('身份、地点或公开来源已变化，请返回地图重新核实。'),
                        ),
                      )
                    : Stack(
                        children: [
                          Positioned.fill(
                            child: Offstage(
                              offstage:
                                  _authorMapVisible &&
                                  _frames.last.kind == 'author-moments',
                              child: PageStorage(
                                bucket: _storage,
                                child: IndexedStack(
                                  index: _frames.length - 1,
                                  children: _frames
                                      .asMap()
                                      .entries
                                      .map((e) => _content(e.value, e.key))
                                      .toList(growable: false),
                                ),
                              ),
                            ),
                          ),
                          if (_checking)
                            Positioned.fill(
                              child: Material(
                                color: Theme.of(context).colorScheme.surface,
                                child: const Center(
                                  child: CircularProgressIndicator(),
                                ),
                              ),
                            ),
                        ],
                      ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// Counts are from this exact leased map snapshot, never a claim of all visits.
class NowPublicPlaceList extends StatelessWidget {
  const NowPublicPlaceList({
    super.key,
    required this.items,
    required this.truncated,
    required this.onOpen,
  });
  final List<TypedMapItem> items;
  final bool truncated;
  final ValueChanged<TypedMapItem> onOpen;

  @override
  Widget build(BuildContext context) {
    final grouped = <String, List<TypedMapItem>>{};
    for (final item in items) {
      if (item.kind == 'MOMENT') (grouped[item.placeID] ??= []).add(item);
    }
    return Material(
      color: Theme.of(context).colorScheme.surface,
      borderRadius: BorderRadius.circular(18),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text('公开分享的地点', style: Theme.of(context).textTheme.titleMedium),
            const Text('当前地图范围 · 不代表作者现在位于这里'),
            if (grouped.isEmpty) const Text('当前范围暂无可见的公开地点动态。'),
            for (final row in grouped.values)
              ListTile(
                contentPadding: EdgeInsets.zero,
                leading: const Icon(Icons.place_outlined),
                title: Text(row.first.anchorLabel),
                subtitle: Text('此地图结果中有 ${row.length} 条公开记录'),
                trailing: const Icon(Icons.chevron_right),
                onTap: () => onOpen(row.first),
              ),
            if (truncated) const Text('当前范围内容较多，仅展示已加载的地图结果；可缩小地图范围。'),
          ],
        ),
      ),
    );
  }
}
