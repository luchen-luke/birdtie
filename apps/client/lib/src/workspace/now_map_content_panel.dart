import 'dart:async';
import 'dart:math' as math;
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:video_player/video_player.dart';

import '../content/video_material_player_stub.dart'
    if (dart.library.io) '../content/video_material_player_io.dart'
    if (dart.library.html) '../content/video_material_player_web.dart'
    as local_video;
import 'owned_map_api.dart';
import 'owned_map_controller.dart';

/// Read-only content stays over the original Now map. It never grants public
/// access or changes the source of a legacy record.
class NowMapContentPanel extends StatefulWidget {
  const NowMapContentPanel({
    super.key,
    required this.controller,
    required this.pin,
    required this.current,
    required this.changes,
    required this.onClose,
    required this.onExpand,
    required this.onEdit,
    this.initialExpanded = false,
    this.closeLabel,
  });
  final bool initialExpanded;
  final String? closeLabel;
  final OwnedMapController controller;
  final OwnedMapPin pin;
  final bool Function() current;
  final Listenable changes;
  final VoidCallback onClose, onExpand;
  final void Function(String momentID, bool Function() current) onEdit;

  @override
  State<NowMapContentPanel> createState() => _NowMapContentPanelState();
}

class _NowMapContentPanelState extends State<NowMapContentPanel>
    with WidgetsBindingObserver {
  final _scroll = ScrollController();
  List<OwnedMapMomentDetail> _items = const [];
  Listenable? _changes;
  Timer? _renew, _expiry;
  String? _nextCursor, _error;
  late int _epoch;
  int _request = 0, _desiredCount = 1;
  String? _viewerMoment;
  final _mediaPages = <String, int>{};
  double _restoreOffset = 0;
  bool _expanded = false,
      _loading = false,
      _foreground = true,
      _hasRead = false;

  OwnedMapController get c => widget.controller;
  OwnedMapPin? get _livePin {
    final value = c.find(widget.pin.mapID);
    return value?.sourceVersion == widget.pin.sourceVersion ? value : null;
  }

  bool get _current =>
      mounted &&
      _foreground &&
      c.permissionEpoch == _epoch &&
      widget.current() &&
      (_livePin != null && c.sourceCurrent(_livePin!)) &&
      _livePin != null;

  @override
  void initState() {
    super.initState();
    _epoch = c.permissionEpoch;
    _expanded = widget.initialExpanded;
    _listen();
    WidgetsBinding.instance.addObserver(this);
    unawaited(_read());
  }

  void _listen() {
    _changes = Listenable.merge([c, widget.changes]);
    _changes!.addListener(_sourceChanged);
  }

  @override
  void didUpdateWidget(NowMapContentPanel oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller != c || oldWidget.changes != widget.changes) {
      _changes?.removeListener(_sourceChanged);
      _listen();
    }
    if (oldWidget.controller != c ||
        oldWidget.pin.mapID != widget.pin.mapID ||
        oldWidget.pin.sourceVersion != widget.pin.sourceVersion) {
      _retire();
      _epoch = c.permissionEpoch;
      _desiredCount = 1;
      _restoreOffset = 0;
      _expanded = widget.initialExpanded;
      _viewerMoment = null;
      _mediaPages.clear();
      if (_scroll.hasClients) _scroll.jumpTo(0);
      unawaited(_read());
    } else if (!_current) {
      _retire();
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _foreground = state == AppLifecycleState.resumed;
    if (!_foreground) {
      _retire();
      if (mounted) setState(() {});
    } else {
      _sourceChanged();
    }
  }

  void _retire() {
    if (_items.isNotEmpty && _scroll.hasClients)
      _restoreOffset = _scroll.offset;
    ++_request;
    _renew?.cancel();
    _expiry?.cancel();
    _items = const [];
    _nextCursor = null;
    _loading = false;
    _hasRead = false;
  }

  void _sourceChanged() {
    if (!mounted) return;
    if (!_current) {
      _retire();
    } else if (!_hasRead && !_loading) {
      unawaited(_read());
    }
    setState(() {});
  }

  void _armLease() {
    _renew?.cancel();
    _expiry?.cancel();
    if (_items.isEmpty) return;
    final now = DateTime.now().toUtc();
    final until = _items
        .map((item) => item.validUntil)
        .reduce((a, b) => a.isBefore(b) ? a : b);
    final remaining = until.difference(now);
    if (remaining <= Duration.zero) {
      _items = const [];
      return;
    }
    _expiry = Timer(remaining, () {
      if (!mounted) return;
      // The in-flight renewed read may still finish under a fresh grant. Old
      // text and pixels leave the widget tree at their own original deadline.
      setState(() {
        if (_items.isNotEmpty && _scroll.hasClients)
          _restoreOffset = _scroll.offset;
        // A user-confirmed playback read may have renewed just one entry.
        // Expiring neighbours must not retire that independently current grant.
        _items = List.unmodifiable(_items.where(c.detailCurrent));
        _nextCursor = null;
        _hasRead = _items.isNotEmpty;
        if (_items.isNotEmpty) _armLease();
      });
    });
    if (remaining > const Duration(seconds: 2)) {
      _renew = Timer(remaining ~/ 2, () => unawaited(_read()));
    }
  }

  Future<void> _read({bool more = false}) async {
    if (!_current || _loading) return;
    final pin = _livePin!, request = ++_request;
    final desired = more
        ? math.max(_desiredCount, _items.length) + 20
        : _desiredCount;
    bool pending() => _current && request == _request;
    _loading = true;
    _error = null;
    if (mounted) setState(() {});
    try {
      // Renew the already loaded pages with fresh cursors. Keep list identity,
      // expansion, media selection and scroll position when the pin is equal.
      final collected = <OwnedMapMomentDetail>[];
      final seen = <String>{}, cursors = <String>{};
      String? cursor;
      if (pin.isMomentAnchor) {
        final target = pin.moments
            .where((item) => item.id == pin.entryID)
            .single;
        collected.add(await c.readMoment(target));
      } else
        do {
          final page = await c.readPlaceMoments(pin, cursor: cursor);
          if (!pending()) return;
          for (final item in page.items) {
            if (!seen.add(item.id) || !c.detailCurrent(item)) {
              throw const FormatException('地图动态分页已变化');
            }
            collected.add(item);
          }
          cursor = page.nextCursor;
          if (cursor != null && !cursors.add(cursor))
            throw const FormatException('地图分页重复');
        } while (cursor != null && collected.length < desired);
      if (!pending()) return;
      _items = List.unmodifiable(collected);
      _hasRead = true;
      _nextCursor = cursor;
      _desiredCount = math.max(1, collected.length);
      _armLease();
    } catch (_) {
      if (pending()) _error = '动态暂不可读，请重新读取。';
    } finally {
      if (pending()) {
        _loading = false;
        setState(() {});
        if (_restoreOffset > 0 && _items.isNotEmpty) {
          WidgetsBinding.instance.addPostFrameCallback((_) {
            if (!pending() || !_scroll.hasClients) return;
            _scroll.jumpTo(
              math.min(_restoreOffset, _scroll.position.maxScrollExtent),
            );
            _restoreOffset = 0;
          });
        }
      }
    }
  }

  bool _acceptFreshDetail(OwnedMapMomentDetail fresh) {
    if (!_current || !c.detailCurrent(fresh)) return false;
    final index = _items.indexWhere((item) => item.id == fresh.id);
    if (index < 0 || _items[index].sourceVersion != fresh.sourceVersion) {
      return false;
    }
    setState(() {
      // A background page response started earlier must not replace this
      // explicit read with a shorter, older lease when it arrives later.
      ++_request;
      _loading = false;
      final next = [..._items];
      next[index] = fresh;
      _items = List.unmodifiable(next.where(c.detailCurrent));
      _armLease();
    });
    return true;
  }

  Key _entryKey(OwnedMapMomentDetail item) => ValueKey(
    'moment-entry:${item.id}:${item.revision}:${item.sourceVersion}',
  );

  @override
  void dispose() {
    _retire();
    _changes?.removeListener(_sourceChanged);
    WidgetsBinding.instance.removeObserver(this);
    _scroll.dispose();
    super.dispose();
  }

  void _expand() {
    if (!_current) return;
    setState(() => _expanded = true);
    widget.onExpand();
  }

  void _openMedia(OwnedMapMomentDetail item, int page) {
    if (!_current || !c.detailCurrent(item)) return;
    if (_scroll.hasClients) _restoreOffset = _scroll.offset;
    _mediaPages[item.id] = page;
    setState(() {
      _viewerMoment = item.id;
      _expanded = true;
    });
    widget.onExpand();
  }

  void _closeMedia() {
    setState(() => _viewerMoment = null);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && _scroll.hasClients) {
        _scroll.jumpTo(
          _restoreOffset.clamp(0, _scroll.position.maxScrollExtent),
        );
      }
    });
  }

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) {
      final current = _current, pin = current ? _livePin : null;
      final availableHeight = constraints.hasBoundedHeight
          ? constraints.maxHeight
          : 560.0;
      final height = _expanded
          ? availableHeight
          : math.min(440.0, availableHeight);
      final viewed = _items
          .where(
            (item) =>
                item.id == _viewerMoment && current && c.detailCurrent(item),
          )
          .firstOrNull;
      if (viewed != null) {
        return SizedBox(
          height: height,
          child: Material(
            color: const Color(0xFF101713),
            child: Column(
              children: [
                SizedBox(
                  height: 52,
                  child: Row(
                    children: [
                      IconButton(
                        tooltip: '返回动态',
                        color: Colors.white,
                        onPressed: _closeMedia,
                        icon: const Icon(Icons.arrow_back),
                      ),
                      const Expanded(
                        child: Text(
                          '照片与视频',
                          style: TextStyle(color: Colors.white, fontSize: 16),
                        ),
                      ),
                    ],
                  ),
                ),
                Expanded(
                  child: _MapMediaPager(
                    key: ValueKey('viewer:${viewed.id}'),
                    controller: c,
                    detail: viewed,
                    onFreshDetail: _acceptFreshDetail,
                    current: () => _current && c.detailCurrent(viewed),
                    compact: false,
                    full: true,
                    initialPage: _mediaPages[viewed.id] ?? 0,
                    onPageChanged: (index) => _mediaPages[viewed.id] = index,
                  ),
                ),
              ],
            ),
          ),
        );
      }
      return SizedBox(
        height: height,
        child: Material(
          color: Theme.of(context).colorScheme.surface,
          child: Column(
            children: [
              Padding(
                padding: const EdgeInsets.only(left: 16, right: 4),
                child: SizedBox(
                  height: 52,
                  child: Row(
                    children: [
                      Expanded(
                        child: Text(
                          pin == null || pin.isMomentAnchor ? '动态' : pin.name,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: Theme.of(context).textTheme.titleMedium,
                        ),
                      ),
                      if (pin != null &&
                          !pin.isMomentAnchor &&
                          pin.momentCount > 1)
                        Padding(
                          padding: const EdgeInsets.only(left: 8),
                          child: Text(
                            '${pin.momentCount}条动态',
                            style: Theme.of(context).textTheme.bodySmall,
                          ),
                        ),
                      IconButton(
                        tooltip:
                            widget.closeLabel ??
                            (_expanded ? '返回地图' : '关闭内容预览'),
                        onPressed: widget.onClose,
                        icon: const Icon(Icons.close),
                      ),
                    ],
                  ),
                ),
              ),
              if (_loading && _items.isEmpty)
                const LinearProgressIndicator(minHeight: 2),
              Expanded(
                child: !current
                    ? Center(
                        child: Column(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            const Text('这条动态暂时无法查看'),
                            TextButton(
                              onPressed: () async {
                                await c.refresh();
                                if (mounted && _current) await _read();
                              },
                              child: const Text('重试'),
                            ),
                          ],
                        ),
                      )
                    : ListView.builder(
                        key: const PageStorageKey('now-map-content-scroll'),
                        controller: _scroll,
                        padding: const EdgeInsets.only(bottom: 16),
                        itemCount:
                            (_expanded
                                ? _items.length
                                : math.min(1, _items.length)) +
                            1,
                        findChildIndexCallback: (key) {
                          final count = _expanded
                              ? _items.length
                              : math.min(1, _items.length);
                          for (var index = 0; index < count; index++) {
                            if (key == _entryKey(_items[index])) return index;
                          }
                          return null;
                        },
                        itemBuilder: (context, index) {
                          final visible = _expanded
                              ? _items.length
                              : math.min(1, _items.length);
                          if (index == visible) {
                            return Padding(
                              padding: const EdgeInsets.symmetric(
                                horizontal: 16,
                              ),
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  if (_error != null) Text('动态暂时无法加载'),
                                  if (!_loading && _items.isEmpty && _hasRead)
                                    const Text('这里还没有可查看的动态'),
                                  if (_error != null || !_hasRead && !_loading)
                                    TextButton(
                                      onPressed: _loading
                                          ? null
                                          : () => unawaited(_read()),
                                      child: const Text('重试'),
                                    ),
                                  if (_expanded && _nextCursor != null)
                                    TextButton(
                                      onPressed: _loading
                                          ? null
                                          : () => unawaited(_read(more: true)),
                                      child: const Text('加载更多动态'),
                                    ),
                                ],
                              ),
                            );
                          }
                          final item = _items[index];
                          return Align(
                            key: _entryKey(item),
                            alignment: Alignment.topCenter,
                            child: ConstrainedBox(
                              constraints: const BoxConstraints(maxWidth: 720),
                              child: _MapMomentTile(
                                key: ValueKey(
                                  '${item.id}:${item.revision}:${item.sourceVersion}',
                                ),
                                controller: c,
                                detail: item,
                                onFreshDetail: _acceptFreshDetail,
                                compact: !_expanded,
                                current: () =>
                                    _current && c.detailCurrent(item),
                                page: _mediaPages[item.id] ?? 0,
                                onPageChanged: (page) =>
                                    _mediaPages[item.id] = page,
                                onOpenMedia: (page) => _openMedia(item, page),
                                onExpand: _expand,
                                onEdit: () => widget.onEdit(
                                  item.id,
                                  () =>
                                      _current &&
                                      c.detailCurrent(item) &&
                                      item.visibilitySource == 'SELF_PRIVATE' &&
                                      item.canEdit,
                                ),
                              ),
                            ),
                          );
                        },
                      ),
              ),
              if (current && !_expanded && pin!.momentCount > 0)
                SizedBox(
                  width: double.infinity,
                  height: 48,
                  child: TextButton(
                    onPressed: _expand,
                    child: const Text('展开动态'),
                  ),
                ),
            ],
          ),
        ),
      );
    },
  );
}

String _momentTime(OwnedMapMomentDetail item) {
  final time = item.displayPublishedAt?.toLocal();
  if (time == null) return '时间未提供';
  String two(int n) => n.toString().padLeft(2, '0');
  return '${time.year}.${two(time.month)}.${two(time.day)}  ${two(time.hour)}:${two(time.minute)}';
}

class _MapMomentTile extends StatelessWidget {
  const _MapMomentTile({
    super.key,
    required this.controller,
    required this.detail,
    required this.onFreshDetail,
    required this.current,
    required this.compact,
    required this.onEdit,
    required this.page,
    required this.onPageChanged,
    required this.onOpenMedia,
    required this.onExpand,
  });
  final OwnedMapController controller;
  final OwnedMapMomentDetail detail;
  final bool Function(OwnedMapMomentDetail) onFreshDetail;
  final bool Function() current;
  final bool compact;
  final VoidCallback onEdit, onExpand;
  final int page;
  final ValueChanged<int> onPageChanged, onOpenMedia;

  @override
  Widget build(BuildContext context) {
    if (!current()) return const SizedBox.shrink();
    final colors = Theme.of(context).colorScheme;
    final title = detail.title.trim(), body = detail.body.trim();
    final author = detail.author, avatar = author?.avatar;
    final name = author?.displayName.trim();
    return Padding(
      padding: EdgeInsets.only(bottom: compact ? 0 : 28),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 4, 8, 12),
            child: Row(
              children: [
                Semantics(
                  label: '作者头像',
                  image: true,
                  child: ClipOval(
                    child: SizedBox(
                      width: 36,
                      height: 36,
                      child: avatar == null
                          ? ColoredBox(
                              color: colors.surfaceContainerHighest,
                              child: name != null && name.isNotEmpty
                                  ? Center(
                                      child: Text(
                                        name.characters.first,
                                        style: TextStyle(
                                          fontSize: 16,
                                          fontWeight: FontWeight.w600,
                                          color: colors.onSurfaceVariant,
                                        ),
                                      ),
                                    )
                                  : Icon(
                                      Icons.person_outline,
                                      size: 20,
                                      color: colors.onSurfaceVariant,
                                    ),
                            )
                          : _LeasePicture(
                              key: ValueKey(
                                'author:${avatar.accountID}:${avatar.version}:${avatar.digest}',
                              ),
                              identity:
                                  '${avatar.accountID}:${avatar.version}:${avatar.digest}',
                              width: 128,
                              height: 128,
                              validUntil: detail.validUntil,
                              current: current,
                              placeholder: const Icon(
                                Icons.person_outline,
                                size: 20,
                              ),
                              read: () =>
                                  controller.authorAvatarContent(detail),
                            ),
                    ),
                  ),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        name == null || name.isEmpty ? '作者未提供' : name,
                        key: ValueKey('moment-author:${detail.id}'),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          fontSize: 14,
                          height: 1.3,
                          fontWeight: FontWeight.w600,
                          color: colors.onSurface,
                        ),
                      ),
                      const SizedBox(height: 2),
                      Tooltip(
                        message: detail.publishedAt != null
                            ? '发布时间'
                            : detail.createdAtSource == 'CIVU_ORIGINAL'
                            ? '原创建时间'
                            : detail.createdAtSource == 'NATIVE'
                            ? '创建时间'
                            : '未提供原时间',
                        child: Text(
                          _momentTime(detail),
                          key: ValueKey('moment-time:${detail.id}'),
                          style: TextStyle(
                            fontSize: 12,
                            height: 1.35,
                            color: colors.onSurfaceVariant,
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
                if (detail.visibility == 'private')
                  Tooltip(
                    message: '仅自己可见',
                    child: Padding(
                      padding: const EdgeInsets.all(8),
                      child: Icon(
                        Icons.lock_outline,
                        size: 16,
                        color: colors.onSurfaceVariant,
                      ),
                    ),
                  ),
                if (detail.visibilitySource == 'SELF_PRIVATE' && detail.canEdit)
                  IconButton(
                    tooltip: '编辑动态',
                    icon: const Icon(Icons.edit_outlined, size: 20),
                    onPressed: () {
                      if (current()) onEdit();
                    },
                  ),
              ],
            ),
          ),
          if (title.isNotEmpty && title != body)
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
              child: Text(
                title,
                maxLines: compact ? 2 : null,
                overflow: compact ? TextOverflow.ellipsis : null,
                style: TextStyle(
                  fontSize: 18,
                  height: 1.4,
                  fontWeight: FontWeight.w600,
                  color: colors.onSurface,
                ),
              ),
            ),
          if (body.isNotEmpty || title.isNotEmpty && title == body)
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
              child: Text(
                body.isEmpty ? title : body,
                maxLines: compact ? 3 : null,
                overflow: compact ? TextOverflow.ellipsis : null,
                style: TextStyle(
                  fontSize: 16,
                  height: 1.55,
                  color: colors.onSurface,
                ),
              ),
            ),
          if (detail.media.isNotEmpty)
            _MapMediaPager(
              controller: controller,
              detail: detail,
              onFreshDetail: onFreshDetail,
              current: current,
              compact: compact,
              initialPage: page,
              onPageChanged: onPageChanged,
              onOpen: onOpenMedia,
            ),
          if (!compact && detail.locations.isNotEmpty)
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 0),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Icon(
                    Icons.location_on_outlined,
                    size: 16,
                    color: colors.onSurfaceVariant,
                  ),
                  const SizedBox(width: 4),
                  Expanded(
                    child: Text(
                      detail.locations.map((p) => p.name).join(' · '),
                      style: TextStyle(
                        fontSize: 13,
                        height: 1.5,
                        color: colors.onSurfaceVariant,
                      ),
                    ),
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

class _MapMediaPager extends StatefulWidget {
  const _MapMediaPager({
    super.key,
    required this.controller,
    required this.detail,
    required this.onFreshDetail,
    required this.current,
    required this.compact,
    this.full = false,
    this.initialPage = 0,
    required this.onPageChanged,
    this.onOpen,
  });
  final OwnedMapController controller;
  final OwnedMapMomentDetail detail;
  final bool Function(OwnedMapMomentDetail) onFreshDetail;
  final bool Function() current;
  final bool compact, full;
  final int initialPage;
  final ValueChanged<int> onPageChanged;
  final ValueChanged<int>? onOpen;
  @override
  State<_MapMediaPager> createState() => _MapMediaPagerState();
}

class _MapMediaPagerState extends State<_MapMediaPager> {
  late int _index;
  late final PageController _pages;
  @override
  void initState() {
    super.initState();
    _index = widget.initialPage.clamp(0, widget.detail.media.length - 1);
    _pages = PageController(initialPage: _index);
  }

  @override
  void dispose() {
    _pages.dispose();
    super.dispose();
  }

  void _move(int delta) {
    final target = _index + delta;
    if (target >= 0 && target < widget.detail.media.length) {
      _pages.animateToPage(
        target,
        duration: const Duration(milliseconds: 180),
        curve: Curves.easeOut,
      );
    }
  }

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, box) {
      final selected = widget.detail.media[_index];
      final ratio = selected.width / selected.height;
      final height = widget.full
          ? box.maxHeight
          : math.min(box.maxWidth / ratio, widget.compact ? 300.0 : 560.0);
      return SizedBox(
        key: ValueKey('moment-media-frame:${widget.detail.id}:${widget.full}'),
        height: height,
        child: Stack(
          children: [
            Positioned.fill(
              child: PageView.builder(
                controller: _pages,
                key: PageStorageKey(
                  'map-media:${widget.detail.id}:${widget.full}',
                ),
                physics: widget.full
                    ? const NeverScrollableScrollPhysics()
                    : null,
                itemCount: widget.detail.media.length,
                onPageChanged: (index) {
                  setState(() => _index = index);
                  widget.onPageChanged(index);
                },
                itemBuilder: (context, index) {
                  // Only the current page may own decoded pixels or a video player.
                  if (index != _index) return const SizedBox.expand();
                  final detail = widget.detail;
                  final media = detail.media[index];
                  bool current() =>
                      mounted &&
                      widget.current() &&
                      index == _index &&
                      widget.detail.id == detail.id &&
                      widget.detail.sourceVersion == detail.sourceVersion &&
                      index < widget.detail.media.length &&
                      widget.detail.media[index].identity == media.identity;
                  if (media.isVideo)
                    return _MapVideo(
                      key: ValueKey(media.identity),
                      controller: widget.controller,
                      detail: widget.detail,
                      onFreshDetail: widget.onFreshDetail,
                      media: media,
                      current: current,
                    );
                  final picture = _LeasePicture(
                    key: ValueKey(media.identity),
                    identity: media.identity,
                    width: media.width,
                    height: media.height,
                    decodeLimit: widget.full ? 2048 : 1280,
                    validUntil: widget.detail.validUntil,
                    current: current,
                    read: () => widget.controller.momentContent(
                      detail,
                      media,
                      current: current,
                    ),
                  );
                  return widget.full
                      ? InteractiveViewer(
                          minScale: 1,
                          maxScale: 4,
                          child: SizedBox.expand(child: picture),
                        )
                      : Semantics(
                          button: true,
                          label: '查看第${index + 1}张图片',
                          child: InkWell(
                            onTap: () => widget.onOpen?.call(index),
                            child: picture,
                          ),
                        );
                },
              ),
            ),
            if (widget.detail.media.length > 1)
              Positioned(
                right: 12,
                top: 12,
                child: DecoratedBox(
                  decoration: BoxDecoration(
                    color: Colors.black.withValues(alpha: .65),
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 8,
                      vertical: 4,
                    ),
                    child: Text(
                      '${_index + 1} / ${widget.detail.media.length}',
                      style: const TextStyle(fontSize: 12, color: Colors.white),
                    ),
                  ),
                ),
              ),
            if (widget.detail.media.length > 1) ...[
              if (_index > 0)
                Positioned(
                  left: 4,
                  top: height / 2 - 24,
                  child: IconButton.filledTonal(
                    tooltip: '上一张',
                    onPressed: () => _move(-1),
                    icon: const Icon(Icons.chevron_left),
                  ),
                ),
              if (_index + 1 < widget.detail.media.length)
                Positioned(
                  right: 4,
                  top: height / 2 - 24,
                  child: IconButton.filledTonal(
                    tooltip: '下一张',
                    onPressed: () => _move(1),
                    icon: const Icon(Icons.chevron_right),
                  ),
                ),
            ],
            if (!widget.full)
              Positioned(
                right: 4,
                bottom: 4,
                child: IconButton.filledTonal(
                  tooltip: '查看完整媒体',
                  onPressed: () => widget.onOpen?.call(_index),
                  icon: const Icon(Icons.open_in_full, size: 20),
                ),
              ),
          ],
        ),
      );
    },
  );
}

/// One visible attachment per pager is decoded in a bounded box; source bytes
/// remain unmodified and are zeroed together with the decoded cache on retirement.
class _LeasePicture extends StatefulWidget {
  const _LeasePicture({
    super.key,
    required this.identity,
    required this.width,
    required this.height,
    required this.validUntil,
    required this.current,
    required this.read,
    this.decodeLimit = 1280,
    this.placeholder,
  });
  final String identity;
  final int width, height;
  final int decodeLimit;
  final Widget? placeholder;
  final DateTime validUntil;
  final bool Function() current;
  final Future<Uint8List> Function() read;
  @override
  State<_LeasePicture> createState() => _LeasePictureState();
}

class _LeasePictureState extends State<_LeasePicture> {
  Uint8List? _data;
  ImageProvider? _image;
  Timer? _expiry;
  int _request = 0;
  bool _loading = false, _failed = false;
  bool get _current =>
      mounted &&
      widget.current() &&
      DateTime.now().toUtc().isBefore(widget.validUntil);
  @override
  void initState() {
    super.initState();
    _arm();
    unawaited(_load());
  }

  @override
  void didUpdateWidget(_LeasePicture oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.identity != widget.identity || !_current) _clear();
    _arm();
    if (_current && _data == null && !_loading) unawaited(_load());
  }

  void _arm() {
    _expiry?.cancel();
    final remaining = widget.validUntil.difference(DateTime.now().toUtc());
    _expiry = Timer(remaining.isNegative ? Duration.zero : remaining, () {
      _clear();
      if (mounted) setState(() {});
    });
  }

  void _clear() {
    ++_request;
    final image = _image;
    _image = null;
    if (image != null) unawaited(image.evict());
    _data?.fillRange(0, _data!.length, 0);
    _data = null;
    _loading = false;
  }

  Future<void> _load() async {
    if (!_current || _loading) return;
    final request = ++_request;
    _loading = true;
    _failed = false;
    Uint8List? data;
    try {
      data = await widget.read();
      if (!_current || request != _request) return;
      final scale = math.min(
        1.0,
        widget.decodeLimit / math.max(widget.width, widget.height),
      );
      _image = ResizeImage(
        MemoryImage(data),
        width: math.max(1, (widget.width * scale).round()),
        height: math.max(1, (widget.height * scale).round()),
      );
      _data = data;
      data = null;
    } catch (_) {
      if (_current && request == _request) _failed = true;
    } finally {
      data?.fillRange(0, data.length, 0);
      if (mounted && request == _request) setState(() => _loading = false);
    }
  }

  @override
  void dispose() {
    _expiry?.cancel();
    _clear();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => !_current
      ? const SizedBox.shrink()
      : _image != null
      ? Image(
          image: _image!,
          fit: BoxFit.contain,
          errorBuilder: (_, _, _) => widget.placeholder ?? const Text('图片加载失败'),
        )
      : Center(
          child:
              widget.placeholder ??
              (_failed
                  ? TextButton(
                      onPressed: () => unawaited(_load()),
                      child: const Text('重试'),
                    )
                  : const SizedBox(
                      width: 20,
                      height: 20,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )),
        );
}

class _MapVideo extends StatefulWidget {
  const _MapVideo({
    super.key,
    required this.controller,
    required this.detail,
    required this.onFreshDetail,
    required this.media,
    required this.current,
  });
  final OwnedMapController controller;
  final OwnedMapMomentDetail detail;
  final bool Function(OwnedMapMomentDetail) onFreshDetail;
  final OwnedMapMomentMedia media;
  final bool Function() current;
  @override
  State<_MapVideo> createState() => _MapVideoState();
}

class _MapVideoState extends State<_MapVideo> {
  VideoPlayerController? _player;
  String? _path, _error;
  Timer? _expiry;
  int _request = 0;
  bool _loading = false;
  bool get _current =>
      mounted &&
      widget.current() &&
      DateTime.now().toUtc().isBefore(widget.detail.validUntil);
  @override
  void initState() {
    super.initState();
    _arm();
  }

  @override
  void didUpdateWidget(_MapVideo oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.media.identity != widget.media.identity || !_current)
      unawaited(_clear());
    _arm();
  }

  void _arm() {
    _expiry?.cancel();
    final remaining = widget.detail.validUntil.difference(
      DateTime.now().toUtc(),
    );
    _expiry = Timer(remaining.isNegative ? Duration.zero : remaining, () {
      unawaited(_clear());
      if (mounted) setState(() {});
    });
  }

  Future<void> _clear() async {
    ++_request;
    final player = _player, path = _path;
    _player = null;
    _path = null;
    _loading = false;
    try {
      await player?.dispose();
    } finally {
      if (path != null) await local_video.releaseVideoMaterialPath(path);
    }
  }

  Future<void> _play() async {
    if (!_current || _loading) return;
    final request = ++_request;
    _loading = true;
    _error = null;
    setState(() {});
    Uint8List? data;
    VideoPlayerController? player;
    String? path;
    try {
      // Playback is an explicit action. Obtain a fresh, current grant before
      // starting a bounded video transfer instead of spending a stale list lease.
      final fresh = await widget.controller.readMoment(widget.detail.selector);
      if (!_current || request != _request) return;
      final media = fresh.media
          .where((item) => item.identity == widget.media.identity)
          .singleOrNull;
      if (fresh.sourceVersion != widget.detail.sourceVersion || media == null) {
        throw const FormatException('视频来源已变化，请重新读取动态');
      }
      if (!widget.onFreshDetail(fresh)) {
        throw const FormatException('视频读取已失效');
      }
      // The visible entry and its expiry timer must adopt the checked grant too.
      await WidgetsBinding.instance.endOfFrame;
      if (!_current || request != _request) return;
      data = await widget.controller.momentContent(
        fresh,
        media,
        current: () => _current && request == _request,
      );
      if (!_current || request != _request) return;
      path = await local_video.createVideoMaterialMemoryPath(data);
      data.fillRange(0, data.length, 0);
      data = null;
      if (!_current || request != _request) return;
      player = local_video.createVideoMaterialPlayer(path);
      if (player == null) throw const FormatException('此平台暂不支持视频播放');
      await player.initialize();
      if (!_current || request != _request) return;
      _player = player;
      player = null;
      _path = path;
      path = null;
      await _player!.play();
    } catch (_) {
      if (_current && request == _request) _error = '视频暂不可播放，请重试。';
    } finally {
      data?.fillRange(0, data.length, 0);
      await player?.dispose();
      if (path != null) await local_video.releaseVideoMaterialPath(path);
      if (mounted && request == _request) setState(() => _loading = false);
    }
  }

  @override
  void dispose() {
    _expiry?.cancel();
    unawaited(_clear());
    super.dispose();
  }

  String _duration(Duration value) {
    final seconds = value.inSeconds;
    return '${seconds ~/ 60}:${(seconds % 60).toString().padLeft(2, '0')}';
  }

  @override
  Widget build(BuildContext context) {
    if (!_current) return const SizedBox.shrink();
    final poster = widget.media.poster;
    return ColoredBox(
      color: const Color(0xFF101713),
      child: Stack(
        fit: StackFit.expand,
        children: [
          if (_player != null)
            Center(
              child: AspectRatio(
                aspectRatio: _player!.value.aspectRatio,
                child: VideoPlayer(_player!),
              ),
            )
          else if (poster != null)
            _LeasePicture(
              identity: poster.identity,
              width: poster.width,
              height: poster.height,
              validUntil: widget.detail.validUntil,
              current: () => _current,
              read: () => widget.controller.coverContent(
                poster,
                current: () => _current,
              ),
            )
          else
            const Center(
              child: Icon(
                Icons.videocam_outlined,
                size: 36,
                color: Colors.white70,
              ),
            ),
          if (_player == null)
            Center(
              child: _loading
                  ? const SizedBox(
                      width: 28,
                      height: 28,
                      child: CircularProgressIndicator(
                        color: Colors.white,
                        strokeWidth: 2,
                      ),
                    )
                  : IconButton.filled(
                      tooltip: _error == null ? '播放视频' : '重试播放',
                      style: IconButton.styleFrom(
                        backgroundColor: Colors.black54,
                        foregroundColor: Colors.white,
                        minimumSize: const Size(56, 56),
                      ),
                      onPressed: () => unawaited(_play()),
                      icon: Icon(
                        _error == null ? Icons.play_arrow : Icons.refresh,
                        size: 32,
                      ),
                    ),
            ),
          if (_error != null)
            Positioned(
              left: 16,
              right: 16,
              bottom: 40,
              child: Text(
                '视频加载失败，点击重试',
                textAlign: TextAlign.center,
                style: const TextStyle(color: Colors.white, fontSize: 13),
              ),
            ),
          if (_player == null && widget.media.durationMs != null)
            Positioned(
              left: 12,
              bottom: 12,
              child: DecoratedBox(
                decoration: BoxDecoration(
                  color: Colors.black54,
                  borderRadius: BorderRadius.circular(4),
                ),
                child: Padding(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 6,
                    vertical: 3,
                  ),
                  child: Text(
                    _duration(Duration(milliseconds: widget.media.durationMs!)),
                    style: const TextStyle(color: Colors.white, fontSize: 12),
                  ),
                ),
              ),
            ),
          if (_player != null)
            Positioned(
              left: 0,
              right: 0,
              bottom: 0,
              child: ColoredBox(
                color: Colors.black54,
                child: ValueListenableBuilder(
                  valueListenable: _player!,
                  builder: (context, value, _) => Row(
                    children: [
                      IconButton(
                        tooltip: value.isPlaying ? '暂停' : '播放',
                        color: Colors.white,
                        onPressed: () {
                          if (_current)
                            unawaited(
                              value.isPlaying
                                  ? _player!.pause()
                                  : _player!.play(),
                            );
                        },
                        icon: Icon(
                          value.isPlaying ? Icons.pause : Icons.play_arrow,
                        ),
                      ),
                      Text(
                        _duration(value.position),
                        style: const TextStyle(
                          color: Colors.white,
                          fontSize: 12,
                        ),
                      ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: VideoProgressIndicator(
                          _player!,
                          allowScrubbing: true,
                          padding: const EdgeInsets.symmetric(vertical: 20),
                          colors: const VideoProgressColors(
                            playedColor: Colors.white,
                            bufferedColor: Colors.white54,
                            backgroundColor: Colors.white24,
                          ),
                        ),
                      ),
                      const SizedBox(width: 8),
                      Padding(
                        padding: const EdgeInsets.only(right: 52),
                        child: Text(
                          _duration(value.duration),
                          style: const TextStyle(
                            color: Colors.white,
                            fontSize: 12,
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ),
        ],
      ),
    );
  }
}

/// Server clusters and screen overlaps stay in the same Now panel/map instance.
/// Each leaf retains its own current grant; display proximity grants no access.
class NowMapContentGroupPanel extends StatefulWidget {
  const NowMapContentGroupPanel({
    super.key,
    required this.controller,
    this.cluster,
    this.pins = const [],
    required this.current,
    required this.changes,
    required this.onClose,
    required this.onEdit,
  });
  final OwnedMapController controller;
  final OwnedMapPin? cluster;
  final List<OwnedMapPin> pins;
  final bool Function() current;
  final Listenable changes;
  final VoidCallback onClose;
  final void Function(String, bool Function()) onEdit;
  @override
  State<NowMapContentGroupPanel> createState() =>
      _NowMapContentGroupPanelState();
}

class _NowMapContentGroupPanelState extends State<NowMapContentGroupPanel> {
  final _scroll = ScrollController();
  List<OwnedMapPin> _pins = const [];
  OwnedMapPin? _selected;
  (String, String)? _restoreSelection;
  Timer? _expiry;
  String? _cursor, _error;
  bool _loading = false;
  int _request = 0, _desiredCount = 1;
  late final int _permissionEpoch;
  String? _windowStart;
  final _pageStarts = <String?>[];
  double _restoreOffset = 0;
  bool get _current =>
      mounted &&
      _permissionEpoch == widget.controller.permissionEpoch &&
      widget.current();
  @override
  void initState() {
    super.initState();
    _permissionEpoch = widget.controller.permissionEpoch;
    _pins = widget.pins;
    _desiredCount = math.max(1, _pins.length);
    widget.changes.addListener(_changed);
    if (widget.cluster != null) {
      unawaited(_load());
    } else {
      _armLease();
    }
  }

  void _rememberPosition() {
    if (_scroll.hasClients) _restoreOffset = _scroll.offset;
    final selected = _selected;
    if (selected != null)
      _restoreSelection = (selected.mapID, selected.sourceVersion);
  }

  void _clearMembers() {
    _rememberPosition();
    _expiry?.cancel();
    _pins = const [];
    _selected = null;
    _cursor = null;
  }

  void _changed() {
    if (!mounted) return;
    if (!_current) {
      ++_request;
      _clearMembers();
      if (_permissionEpoch != widget.controller.permissionEpoch) {
        _restoreSelection = null;
        _windowStart = null;
        _pageStarts.clear();
      }
      _loading = false;
    } else if (!_loading &&
        (_pins.isEmpty ||
            _pins.any((pin) => !widget.controller.sourceCurrent(pin)))) {
      // A changed/expired member may not borrow its parent's renewed grant.
      ++_request;
      _clearMembers();
      _loading = false;
      unawaited(_load());
    }
    setState(() {});
  }

  void _armLease() {
    _expiry?.cancel();
    final deadlines = _pins.map((p) => p.validUntil).whereType<DateTime>();
    if (deadlines.isEmpty) return;
    final until = deadlines.reduce((a, b) => a.isBefore(b) ? a : b);
    final remaining = until.difference(DateTime.now().toUtc());
    _expiry = Timer(remaining > Duration.zero ? remaining : Duration.zero, () {
      if (!mounted) return;
      ++_request;
      setState(() {
        _clearMembers();
        _loading = false;
      });
      if (_current) unawaited(_load());
    });
  }

  void _restoreValidSelection() {
    final target = _restoreSelection;
    if (target != null) {
      _selected = _pins
          .where(
            (p) =>
                p.mapID == target.$1 &&
                p.sourceVersion == target.$2 &&
                widget.controller.sourceCurrent(p),
          )
          .firstOrNull;
      _restoreSelection = null;
    }
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && _scroll.hasClients && _current) {
        _scroll.jumpTo(
          _restoreOffset.clamp(0, _scroll.position.maxScrollExtent),
        );
      }
    });
  }

  Future<void> _load({bool more = false}) async {
    if (!_current || _loading) return;
    final serial = ++_request;
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      List<OwnedMapPin> next;
      String? cursor;
      final cluster = widget.cluster == null
          ? null
          : widget.controller.find(widget.cluster!.mapID);
      if (cluster == null) {
        next = [
          for (final pin in widget.pins)
            if (widget.controller.find(pin.mapID) case final fresh?)
              if (fresh.sourceVersion == pin.sourceVersion &&
                  widget.controller.sourceCurrent(fresh))
                fresh,
        ];
      } else {
        next = more ? [..._pins.where(widget.controller.sourceCurrent)] : [];
        cursor = more ? _cursor : _windowStart;
        final starts = more ? [..._pageStarts] : <String?>[];
        do {
          final start = cursor;
          final page = await widget.controller.readClusterMembers(
            cluster,
            cursor: cursor,
          );
          if (!_current || serial != _request) return;
          // The controller retains a bounded current member window. Drop
          // retired earlier pages before appending, so pagination can proceed
          // beyond that window without exposing expired text or false totals.
          next = next.where(widget.controller.sourceCurrent).toList();
          next.addAll(page.items);
          starts.add(start);
          // Keep only the current eight-page window. Its signed starting
          // position survives content expiry, but no old text/cover grant does.
          while (starts.length > 8) starts.removeAt(0);
          cursor = page.nextCursor;
          if (next.map((p) => p.mapID).toSet().length != next.length)
            throw const FormatException('重复地图成员');
        } while (!more && next.length < _desiredCount && cursor != null);
        _pageStarts
          ..clear()
          ..addAll(starts);
        _windowStart = starts.isEmpty ? null : starts.first;
      }
      if (!_current || serial != _request) return;
      if (next.any((pin) => !widget.controller.sourceCurrent(pin)))
        throw const FormatException('地图成员读取已失效');
      _pins = List.unmodifiable(next);
      _cursor = cursor;
      _desiredCount = math.min(320, math.max(_desiredCount, next.length));
      _restoreValidSelection();
      _armLease();
    } catch (_) {
      if (_current && serial == _request) _error = '内容列表暂不可读，请重试。';
    } finally {
      if (mounted && serial == _request) setState(() => _loading = false);
    }
  }

  @override
  void dispose() {
    ++_request;
    _expiry?.cancel();
    widget.changes.removeListener(_changed);
    _scroll.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final selected = _selected;
    if (_current &&
        selected != null &&
        widget.controller.sourceCurrent(selected)) {
      void back() => setState(() {
        _selected = null;
        _restoreSelection = null;
      });
      if (selected.isCluster) {
        return NowMapContentGroupPanel(
          controller: widget.controller,
          cluster: selected,
          current: () => _current && widget.controller.sourceCurrent(selected),
          changes: widget.changes,
          onClose: back,
          onEdit: widget.onEdit,
        );
      }
      return NowMapContentPanel(
        key: ValueKey(selected.mapID),
        controller: widget.controller,
        pin: selected,
        current: () => _current && widget.controller.sourceCurrent(selected),
        changes: widget.changes,
        onClose: back,
        onExpand: () {},
        initialExpanded: true,
        closeLabel: '返回内容列表',
        onEdit: widget.onEdit,
      );
    }
    final visible = _current
        ? _pins.where(widget.controller.sourceCurrent).toList()
        : const <OwnedMapPin>[];
    return Material(
      color: Theme.of(context).colorScheme.surface,
      child: Column(
        children: [
          ListTile(
            title: Text(
              !_current
                  ? '地图内容'
                  : widget.cluster == null
                  ? '重叠位置的内容'
                  : '${widget.cluster!.momentCount}条动态',
            ),
            subtitle: !_current
                ? null
                : Text(
                    widget.cluster == null
                        ? '选择要查看的内容'
                        : '${widget.cluster!.anchorCount}个内容位置',
                  ),
            trailing: IconButton(
              tooltip: '返回地图',
              icon: const Icon(Icons.close),
              onPressed: widget.onClose,
            ),
          ),
          if (_loading) const LinearProgressIndicator(minHeight: 2),
          Expanded(
            child: !_current
                ? const Center(child: Text('当前内容已不可读，请返回地图重新选择。'))
                : ListView.builder(
                    controller: _scroll,
                    itemCount: visible.length + 1,
                    itemBuilder: (context, index) {
                      if (index == visible.length)
                        return Column(
                          children: [
                            if (_error != null) Text(_error!),
                            if (_error != null || _cursor != null)
                              TextButton(
                                onPressed: _loading
                                    ? null
                                    : () => _load(more: _error == null),
                                child: Text(_error == null ? '加载更多位置' : '重试'),
                              ),
                          ],
                        );
                      final pin = visible[index];
                      return ListTile(
                        key: ValueKey(pin.mapID),
                        leading: Icon(
                          pin.isCluster
                              ? Icons.layers_outlined
                              : pin.isMomentAnchor
                              ? Icons.photo_camera_outlined
                              : Icons.place_outlined,
                        ),
                        title: Text(
                          pin.name.isEmpty ? '无标题动态' : pin.name,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                        ),
                        subtitle: Text(
                          '${pin.momentCount}条动态 · ${pin.visibilityLabel}',
                        ),
                        onTap: () => setState(() {
                          _rememberPosition();
                          _selected = pin;
                        }),
                      );
                    },
                  ),
          ),
        ],
      ),
    );
  }
}
