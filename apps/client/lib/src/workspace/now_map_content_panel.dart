import 'dart:async';
import 'dart:math' as math;
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:video_player/video_player.dart';

import '../content/video_material_player_stub.dart'
    if (dart.library.io) '../content/video_material_player_io.dart'
    if (dart.library.html) '../content/video_material_player_web.dart'
    as local_video;
import 'map_marker_visual.dart';
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
  });
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
      _expanded = false;
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
        _items = const [];
        _nextCursor = null;
        _hasRead = false;
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

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) {
      final current = _current, pin = current ? _livePin : null;
      final cover = pin?.cover;
      final coverUntil = pin?.validUntil ?? c.view?.validUntil;
      final availableHeight = constraints.hasBoundedHeight
          ? constraints.maxHeight
          : 560.0;
      final height = _expanded
          ? availableHeight
          : math.min(360.0, availableHeight);
      return SizedBox(
        height: height,
        child: Material(
          color: Theme.of(context).colorScheme.surface,
          child: Column(
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 8, 4, 8),
                child: Row(
                  children: [
                    if (pin != null) ...[
                      if (cover != null && coverUntil != null)
                        SizedBox(
                          width: 56,
                          height: 56,
                          child: _LeasePicture(
                            key: ValueKey('place:${_epoch}:${cover.identity}'),
                            identity: cover.identity,
                            width: cover.width,
                            height: cover.height,
                            validUntil: coverUntil,
                            current: () => _current,
                            read: () =>
                                c.coverContent(cover, current: () => _current),
                          ),
                        )
                      else
                        Icon(
                          MapMarkerVisual.placeIcon(pin.categoryCode),
                          size: 20,
                        ),
                      const SizedBox(width: 8),
                    ],
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            pin?.name ?? '地图内容',
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                            style: Theme.of(context).textTheme.titleMedium,
                          ),
                          if (pin != null)
                            Text(
                              '${pin.isMomentAnchor ? '内容位置' : _category(pin.categoryCode)} · ${pin.momentCount}条动态',
                              style: Theme.of(context).textTheme.bodySmall,
                            ),
                          if (pin?.hasPrivateContent == true)
                            const Text('含仅本人可见内容'),
                        ],
                      ),
                    ),
                    IconButton(
                      tooltip: _expanded ? '返回地图' : '关闭内容预览',
                      onPressed: widget.onClose,
                      icon: const Icon(Icons.close),
                    ),
                  ],
                ),
              ),
              if (_loading) const LinearProgressIndicator(minHeight: 2),
              Expanded(
                child: !current
                    ? Center(
                        child: Column(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            const Text('当前内容已不可读。'),
                            TextButton(
                              onPressed: () async {
                                await c.refresh();
                                if (mounted && _current) await _read();
                              },
                              child: const Text('重新读取地点'),
                            ),
                          ],
                        ),
                      )
                    : ListView.builder(
                        key: const PageStorageKey('now-map-content-scroll'),
                        controller: _scroll,
                        padding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
                        itemCount:
                            (_expanded
                                ? _items.length
                                : math.min(1, _items.length)) +
                            1,
                        itemBuilder: (context, index) {
                          final visible = _expanded
                              ? _items.length
                              : math.min(1, _items.length);
                          if (index == visible)
                            return Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                if (_error != null) Text(_error!),
                                if (!_loading && _items.isEmpty)
                                  Text(
                                    _hasRead ? '这里暂时没有可读动态。' : '读取已到期，请重新读取。',
                                  ),
                                if (_error != null || !_hasRead && !_loading)
                                  TextButton(
                                    onPressed: _loading
                                        ? null
                                        : () => unawaited(_read()),
                                    child: const Text('重新读取动态'),
                                  ),
                                if (_expanded && _nextCursor != null)
                                  TextButton(
                                    onPressed: _loading
                                        ? null
                                        : () => unawaited(_read(more: true)),
                                    child: const Text('加载更多动态'),
                                  ),
                              ],
                            );
                          final item = _items[index];
                          return _MapMomentTile(
                            key: ValueKey(
                              '${item.id}:${item.revision}:${item.sourceVersion}',
                            ),
                            controller: c,
                            detail: item,
                            compact: !_expanded,
                            current: () => _current && c.detailCurrent(item),
                            onEdit: () => widget.onEdit(
                              item.id,
                              () =>
                                  _current &&
                                  c.detailCurrent(item) &&
                                  item.visibilitySource == 'SELF_PRIVATE' &&
                                  item.canEdit,
                            ),
                          );
                        },
                      ),
              ),
              if (current && !_expanded && pin!.momentCount > 0)
                Align(
                  alignment: Alignment.centerLeft,
                  child: Padding(
                    padding: const EdgeInsets.fromLTRB(8, 0, 8, 4),
                    child: TextButton.icon(
                      onPressed: _expand,
                      icon: const Icon(Icons.expand_more),
                      label: const Text('查看动态'),
                    ),
                  ),
                ),
            ],
          ),
        ),
      );
    },
  );
}

String _category(String code) => switch (code) {
  'cafe' || 'coffee_shop' => '咖啡馆',
  'restaurant' || 'food' || 'food_drink' => '餐饮',
  'bar' || 'pub' => '酒吧',
  'museum' => '博物馆',
  'art_gallery' => '美术馆',
  'park' || 'nature' || 'nature_outdoor' => '自然与公园',
  'hotel' || 'hostel' || 'lodging' => '住宿',
  'shopping' || 'shop' || 'shopping_mall' => '商店',
  'distillery' || 'brewery' || 'winery' => '酒厂',
  _ => '地点',
};

class _MapMomentTile extends StatelessWidget {
  const _MapMomentTile({
    super.key,
    required this.controller,
    required this.detail,
    required this.current,
    required this.compact,
    required this.onEdit,
  });
  final OwnedMapController controller;
  final OwnedMapMomentDetail detail;
  final bool Function() current;
  final bool compact;
  final VoidCallback onEdit;

  @override
  Widget build(BuildContext context) {
    if (!current()) return const SizedBox.shrink();
    final text = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (detail.title.isNotEmpty)
          Padding(
            padding: const EdgeInsets.only(bottom: 6),
            child: Text(
              detail.title,
              maxLines: compact ? 2 : null,
              overflow: compact ? TextOverflow.ellipsis : null,
              style: Theme.of(context).textTheme.titleSmall,
            ),
          ),
        if (detail.body.isNotEmpty)
          Text(
            detail.body,
            maxLines: compact ? 3 : null,
            overflow: compact ? TextOverflow.ellipsis : null,
          ),
      ],
    );
    return Padding(
      padding: EdgeInsets.only(bottom: compact ? 12 : 20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(switch (detail.visibilitySource) {
                  'SELF_PRIVATE' => '仅本人可见',
                  'CIVU_PUBLIC_MOMENT' => '原公开记录',
                  _ => '公开动态',
                }, style: Theme.of(context).textTheme.bodySmall),
              ),
              if (detail.visibilitySource == 'SELF_PRIVATE' && detail.canEdit)
                TextButton(
                  onPressed: () {
                    if (current()) onEdit();
                  },
                  child: const Text('编辑动态'),
                ),
            ],
          ),
          if (compact && detail.media.isNotEmpty)
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                SizedBox(
                  width: 96,
                  child: _MapMediaPager(
                    controller: controller,
                    detail: detail,
                    current: current,
                    compact: true,
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(child: text),
              ],
            )
          else
            text,
          if (!compact && detail.media.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 10),
              child: _MapMediaPager(
                controller: controller,
                detail: detail,
                current: current,
                compact: compact,
              ),
            ),
          if (!compact && detail.locations.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Wrap(
                spacing: 8,
                runSpacing: 4,
                children: [
                  for (final location in detail.locations) Text(location.name),
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
    required this.controller,
    required this.detail,
    required this.current,
    required this.compact,
  });
  final OwnedMapController controller;
  final OwnedMapMomentDetail detail;
  final bool Function() current;
  final bool compact;
  @override
  State<_MapMediaPager> createState() => _MapMediaPagerState();
}

class _MapMediaPagerState extends State<_MapMediaPager> {
  int _index = 0;
  @override
  Widget build(BuildContext context) => Column(
    children: [
      SizedBox(
        height: widget.compact ? 96 : 240,
        child: PageView.builder(
          key: PageStorageKey(
            'map-media:${widget.detail.id}:${widget.detail.sourceVersion}',
          ),
          itemCount: widget.detail.media.length,
          onPageChanged: (index) => setState(() => _index = index),
          itemBuilder: (context, index) {
            final media = widget.detail.media[index];
            return media.isVideo
                ? _MapVideo(
                    key: ValueKey(media.identity),
                    controller: widget.controller,
                    detail: widget.detail,
                    media: media,
                    current: widget.current,
                  )
                : _LeasePicture(
                    key: ValueKey(media.identity),
                    identity: media.identity,
                    width: media.width,
                    height: media.height,
                    validUntil: widget.detail.validUntil,
                    current: widget.current,
                    read: () =>
                        widget.controller.momentContent(widget.detail, media),
                  );
          },
        ),
      ),
      if (widget.detail.media.length > 1)
        Text('${_index + 1} / ${widget.detail.media.length}'),
    ],
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
  });
  final String identity;
  final int width, height;
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
        math.min(960 / widget.width, 720 / widget.height),
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
          errorBuilder: (_, _, _) => const Text('图片暂不可读'),
        )
      : Center(
          child: _failed
              ? TextButton(
                  onPressed: () => unawaited(_load()),
                  child: const Text('重新读取图片'),
                )
              : const SizedBox(
                  width: 20,
                  height: 20,
                  child: CircularProgressIndicator(strokeWidth: 2),
                ),
        );
}

class _MapVideo extends StatefulWidget {
  const _MapVideo({
    super.key,
    required this.controller,
    required this.detail,
    required this.media,
    required this.current,
  });
  final OwnedMapController controller;
  final OwnedMapMomentDetail detail;
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
      data = await widget.controller.momentContent(widget.detail, widget.media);
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

  @override
  Widget build(BuildContext context) {
    if (!_current) return const SizedBox.shrink();
    final poster = widget.media.poster;
    return Column(
      children: [
        Expanded(
          child: _player != null
              ? AspectRatio(
                  aspectRatio: _player!.value.aspectRatio,
                  child: VideoPlayer(_player!),
                )
              : poster != null
              ? _LeasePicture(
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
              : const Center(child: Icon(Icons.videocam_outlined)),
        ),
        if (_error != null) Text(_error!),
        if (_player != null)
          ValueListenableBuilder(
            valueListenable: _player!,
            builder: (context, value, _) => IconButton(
              tooltip: value.isPlaying ? '暂停' : '播放',
              onPressed: () {
                if (_current)
                  unawaited(
                    value.isPlaying ? _player!.pause() : _player!.play(),
                  );
              },
              icon: Icon(value.isPlaying ? Icons.pause : Icons.play_arrow),
            ),
          )
        else
          TextButton.icon(
            onPressed: _loading ? null : () => unawaited(_play()),
            icon: _loading
                ? const SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.play_circle_outline),
            label: Text(
              _loading
                  ? '正在读取视频…'
                  : '播放视频 · ${((widget.media.durationMs ?? 0) / 1000).toStringAsFixed(1)}秒',
            ),
          ),
      ],
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
