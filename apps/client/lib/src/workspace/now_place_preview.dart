import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import 'now_ui_tokens.dart';
import 'place_history_controller.dart';
import 'place_history_pagination.dart';
import 'public_moment_api.dart';
import '../config/birdtie_environment.dart';
import 'entity_cover.dart';
import 'place_photo.dart';

/// A small, read-only preview of one leased public map place.
///
/// Mount this above the existing map, outside its composer/keyboard owner. The
/// parent supplies the original snapshot's deadline and stable provider
/// instances. A fresh map refresh cannot renew this preview's original lease.
/// The detailed reader remains NowPublicPlacePanel, with its own read fences.
class NowPlacePreview extends StatefulWidget {
  const NowPlacePreview({
    super.key,
    required this.placeID,
    required this.placeTitle,
    required this.validUntil,
    required this.authorizationHeader,
    required this.workspaceID,
    required this.sourceChanges,
    required this.current,
    required this.revalidate,
    required this.onClose,
    required this.onOpenPlace,
    required this.onOpenMoment,
    this.apiBaseUrl,
    this.client,
    this.onExpandedChanged,
  });

  final String placeID, placeTitle;
  final DateTime validUntil;
  final String? Function() authorizationHeader, workspaceID;
  final Listenable sourceChanges;
  final bool Function() current;
  final Future<bool> Function() revalidate;
  final VoidCallback onClose, onOpenPlace;
  final ValueChanged<String> onOpenMoment;
  final String? apiBaseUrl;
  final http.Client? client;
  final ValueChanged<bool>? onExpandedChanged;

  @override
  State<NowPlacePreview> createState() => _NowPlacePreviewState();
}

class _NowPlacePreviewState extends State<NowPlacePreview> {
  late final _identity = (widget.authorizationHeader(), widget.workspaceID());
  late final PlaceHistoryPaginationController _history;
  late final PublicMomentApi _moments;
  late final http.Client _photoClient;
  late final bool _ownsPhotoClient;
  late final PlacePhotoAPI _photoAPI;
  EntityCoverMetadata? _photo;
  int _photoEpoch = 0;
  Timer? _expiry;
  bool _retired = false, _checking = false, _expanded = false;
  String? _notice;

  bool get _current =>
      !_retired &&
      DateTime.now().toUtc().isBefore(widget.validUntil.toUtc()) &&
      _identity == (widget.authorizationHeader(), widget.workspaceID()) &&
      widget.current();

  @override
  void initState() {
    super.initState();
    _history = PlaceHistoryPaginationController(
      placeID: widget.placeID,
      authorizationHeader: widget.authorizationHeader,
      identityChanges: widget.sourceChanges,
      sourceCurrent: () => _current,
      includePublicAuthor: true,
      apiBaseUrl: widget.apiBaseUrl,
      client: widget.client,
    )..addListener(_historyChanged);
    _moments = PublicMomentApi(
      authorizationHeader: widget.authorizationHeader,
      apiBaseUrl: widget.apiBaseUrl,
      client: widget.client,
    );
    _ownsPhotoClient = widget.client == null;
    _photoClient = widget.client ?? http.Client();
    _photoAPI = PlacePhotoAPI(
      client: _photoClient,
      baseUrl: widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl,
      authorizationHeader: widget.authorizationHeader,
      current: () => _current,
    );
    widget.sourceChanges.addListener(_sourceChanged);
    if (!_current) {
      _retire(notify: false);
      return;
    }
    _expiry = Timer(
      widget.validUntil.toUtc().difference(DateTime.now().toUtc()),
      _retire,
    );
    unawaited(_history.refresh());
    unawaited(_loadPhoto());
  }

  Future<void> _loadPhoto() async {
    final epoch = ++_photoEpoch;
    try {
      final state = await _photoAPI.state(PlacePhotoRef(widget.placeID));
      if (mounted && epoch == _photoEpoch && _current) {
        setState(() => _photo = state.placePhoto);
      }
    } catch (_) {
      /* No image region is created without current public pixels. */
    }
  }

  @override
  void didUpdateWidget(NowPlacePreview old) {
    super.didUpdateWidget(old);
    if (!identical(old.sourceChanges, widget.sourceChanges)) {
      old.sourceChanges.removeListener(_sourceChanged);
      widget.sourceChanges.addListener(_sourceChanged);
    }
    if (old.placeID != widget.placeID ||
        old.placeTitle != widget.placeTitle ||
        old.validUntil != widget.validUntil ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        !identical(old.client, widget.client) ||
        !identical(old.authorizationHeader, widget.authorizationHeader) ||
        !identical(old.workspaceID, widget.workspaceID) ||
        !identical(old.sourceChanges, widget.sourceChanges) ||
        !identical(old.current, widget.current) ||
        !identical(old.revalidate, widget.revalidate) ||
        !_current) {
      _retire(notify: false);
    }
  }

  void _sourceChanged() {
    if (!_current) _retire();
  }

  void _historyChanged() {
    if (!mounted) return;
    if (!_current) {
      _retire();
    } else {
      setState(() {});
    }
  }

  void _retire({bool notify = true}) {
    if (_retired) return;
    _retired = true;
    ++_photoEpoch;
    _photo = null;
    _expiry?.cancel();
    _history.invalidate(notify: false);
    final wasExpanded = _expanded;
    _expanded = false;
    _notice = null;
    if (notify && mounted) setState(() {});
    if (wasExpanded) {
      // Retirement can originate in build/didUpdateWidget. Notify the
      // parent after this frame; changing presentation cannot renew data.
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && _retired) widget.onExpandedChanged?.call(false);
      });
    }
  }

  Future<bool> _verify() async {
    if (_checking || !_current) {
      if (!_current) _retire();
      return false;
    }
    setState(() {
      _checking = true;
      _notice = null;
    });
    bool valid;
    try {
      valid = await widget.revalidate();
    } catch (_) {
      valid = false;
    }
    if (!mounted) return false;
    if (!valid || !_current) {
      _retire();
      return false;
    }
    setState(() => _checking = false);
    return true;
  }

  Future<void> _refresh() async {
    if (!await _verify() || !mounted) return;
    await _history.refresh();
  }

  Future<void> _showStories() async {
    if (!await _verify() || !mounted) return;
    if (_history.page == null) await _history.refresh();
    if (!mounted || !_current) return;
    setState(() => _expanded = _history.page != null);
    widget.onExpandedChanged?.call(_expanded);
  }

  Future<void> _openPlace() async {
    if (await _verify() && mounted && _current) widget.onOpenPlace();
  }

  Future<void> _loadMore() async {
    if (!await _verify() || !mounted) return;
    await _history.loadMore();
  }

  Future<void> _openMoment(PlaceHistoryMoment selected) async {
    if (selected.id == null || !await _verify() || !mounted) return;
    final generation = _history.page;
    final listed = _history.items.any(
      (m) => m.id == selected.id && m.revision == selected.revision,
    );
    if (generation == null || !listed) return;
    setState(() => _checking = true);
    try {
      // Read the actual selected object. Refreshing the first page here would
      // reject every valid story loaded on a later page and discard its cursor.
      final live = await _moments.read(selected.id!, includePublicAuthor: true);
      if (!mounted || !_current || !identical(generation, _history.page))
        return;
      final sameAuthor =
          live.authorID == selected.authorID &&
          live.authorPublicProfile?.displayName ==
              selected.authorPublicProfile?.displayName;
      if (live.placeID != widget.placeID ||
          live.revision != selected.revision ||
          !sameAuthor) {
        _history.invalidate(notify: false);
        setState(() => _notice = '这条动态已变化，请重新查看。');
        return;
      }
      widget.onOpenMoment(selected.id!);
    } catch (_) {
      if (mounted && _current && identical(generation, _history.page)) {
        // Unknown current authority is not a zero-row result or a permission.
        _history.invalidate(notify: false);
        setState(() => _notice = '这条动态暂时无法查看，请重新读取。');
      }
    } finally {
      if (mounted && _current) setState(() => _checking = false);
    }
  }

  @override
  void dispose() {
    _expiry?.cancel();
    widget.sourceChanges.removeListener(_sourceChanged);
    _history.removeListener(_historyChanged);
    _history.dispose();
    _moments.dispose();
    ++_photoEpoch;
    if (_ownsPhotoClient) _photoClient.close();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!_current) _retire(notify: false);
    final tokens = NowUiTokens.of(context);
    final summary = _retired ? null : _history.page;
    final rows = _retired ? const <PlaceHistoryMoment>[] : _history.items;
    final busy = !_retired && (_checking || _history.loading);
    final action = TextButton.styleFrom(
      minimumSize: const Size(48, 48),
      textStyle: tokens.action,
    );
    final close = IconButton(
      tooltip: '收起地点预览',
      constraints: const BoxConstraints(minWidth: 48, minHeight: 48),
      onPressed: widget.onClose,
      icon: const Icon(Icons.close, size: 20),
    );
    return Align(
      alignment: Alignment.centerLeft,
      child: ConstrainedBox(
        constraints: BoxConstraints(maxWidth: _expanded ? 390 : 250),
        child: Material(
          color: Theme.of(context).colorScheme.surface,
          elevation: _expanded ? 0 : 3,
          shadowColor: Theme.of(
            context,
          ).colorScheme.shadow.withValues(alpha: .12),
          borderRadius: BorderRadius.circular(16),
          child: Padding(
            padding: const EdgeInsets.all(8),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Expanded(
                      child: Padding(
                        padding: const EdgeInsets.fromLTRB(6, 10, 0, 6),
                        child: Text(
                          _retired ? '请重新打开地点' : widget.placeTitle,
                          style: tokens.placeTitle,
                        ),
                      ),
                    ),
                    close,
                  ],
                ),
                if (_retired)
                  Padding(
                    padding: const EdgeInsets.all(6),
                    child: Text('请重新打开这个地点，查看最新内容。', style: tokens.body),
                  )
                else ...[
                  if (_photo != null)
                    EntityCover(
                      metadata: _photo,
                      api: _photoAPI.pixels,
                      current: () => _current,
                      identityChanges: widget.sourceChanges,
                      width: 224,
                      height: 126,
                      label: '${widget.placeTitle}的地点照片',
                    ),
                  if (summary != null ||
                      (_history.error == null && _notice == null))
                    Padding(
                      padding: const EdgeInsets.symmetric(horizontal: 6),
                      child: Text(
                        summary == null &&
                                _history.error == null &&
                                _notice == null
                            ? _history.loading
                                  ? '正在读取公开动态…'
                                  : '公开动态数量尚未读取'
                            : summary != null && summary.count > 0
                            ? '最近 30 天 · ${summary.count} 条公开分享'
                            : summary?.count == 0
                            ? '暂无仍公开的近期分享。'
                            : '',
                        style: tokens.metadata,
                      ),
                    ),
                  if (busy) const LinearProgressIndicator(),
                  if (_history.error case final error?)
                    Padding(
                      padding: const EdgeInsets.all(6),
                      child: Semantics(
                        liveRegion: true,
                        child: Text(error, style: tokens.body),
                      ),
                    ),
                  if (_notice case final notice?)
                    Padding(
                      padding: const EdgeInsets.all(6),
                      child: Semantics(
                        liveRegion: true,
                        child: Text(notice, style: tokens.body),
                      ),
                    ),
                  if (_history.error != null || _notice != null)
                    TextButton(
                      style: action,
                      onPressed: busy ? null : _refresh,
                      child: const Text('重新读取公开动态'),
                    ),
                  if (_expanded && summary != null)
                    NowPlaceStoryStrip(
                      key: ValueKey('place-stories:${widget.placeID}'),
                      moments: rows,
                      onOpen: busy ? null : _openMoment,
                    ),
                  if (_expanded && summary != null && _history.hasMore)
                    TextButton(
                      key: const ValueKey('place-stories-load-more'),
                      style: action,
                      onPressed: busy ? null : _loadMore,
                      child: const Text('继续查看动态'),
                    ),
                  Wrap(
                    spacing: 4,
                    children: [
                      if (!_expanded && (summary?.count ?? 0) > 0)
                        TextButton(
                          style: action,
                          onPressed: busy ? null : _showStories,
                          child: const Text('看看公开动态'),
                        ),
                      if (_expanded)
                        TextButton(
                          style: action,
                          onPressed: () {
                            setState(() => _expanded = false);
                            widget.onExpandedChanged?.call(false);
                          },
                          child: const Text('收起动态'),
                        ),
                      TextButton(
                        style: action,
                        // The Place already has its own current source lease.
                        // Reading related stories is an independent request.
                        onPressed: _checking ? null : _openPlace,
                        child: const Text('查看地点'),
                      ),
                    ],
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// A horizontal, gesture-paged strip. Only the current group is mounted, so
/// there are at most three visible stories, including while swiping. Its height
/// follows real text; at large system text sizes it uses fewer, wider cards.
/// This strip browses accumulated authorised rows; the parent owns server paging.
class NowPlaceStoryStrip extends StatefulWidget {
  const NowPlaceStoryStrip({
    super.key,
    required this.moments,
    required this.onOpen,
  });
  final List<PlaceHistoryMoment> moments;
  final ValueChanged<PlaceHistoryMoment>? onOpen;

  @override
  State<NowPlaceStoryStrip> createState() => _NowPlaceStoryStripState();
}

class _NowPlaceStoryStripState extends State<NowPlaceStoryStrip> {
  int _start = 0;
  double _drag = 0;

  @override
  void didUpdateWidget(NowPlaceStoryStrip old) {
    super.didUpdateWidget(old);
    final oldIDs = old.moments.map((m) => (m.id, m.revision)).toList();
    final newIDs = widget.moments.map((m) => (m.id, m.revision)).toList();
    // Appending an authorised page keeps the reader's group. Replacing or
    // shrinking the corpus resets it; three mounted cards are not a data cap.
    final preservesPrefix =
        oldIDs.length <= newIDs.length &&
        List.generate(
          oldIDs.length,
          (i) => oldIDs[i] == newIDs[i],
        ).every((v) => v);
    if (!preservesPrefix) _start = 0;
  }

  void _move(int step, int count) {
    final last = math.max(0, ((widget.moments.length - 1) ~/ count) * count);
    setState(() => _start = (_start + step * count).clamp(0, last).toInt());
  }

  @override
  Widget build(BuildContext context) {
    if (widget.moments.isEmpty) return const SizedBox.shrink();
    final tokens = NowUiTokens.of(context);
    return LayoutBuilder(
      builder: (context, constraints) {
        final width = constraints.maxWidth.isFinite
            ? constraints.maxWidth
            : 340.0;
        final minimum = math.max(
          104.0,
          MediaQuery.textScalerOf(context).scale(16) * 6,
        );
        final count = ((width + 8) / (minimum + 8)).floor().clamp(1, 3).toInt();
        final start = math.min(
          (_start ~/ count) * count,
          ((widget.moments.length - 1) ~/ count) * count,
        );
        final rows = widget.moments.skip(start).take(count).toList();
        final hasPrevious = start > 0;
        final hasNext = start + rows.length < widget.moments.length;
        return Semantics(
          container: true,
          label: '公开动态，左右滑动查看其他已读取动态',
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              GestureDetector(
                behavior: HitTestBehavior.opaque,
                onHorizontalDragStart: (_) => _drag = 0,
                onHorizontalDragUpdate: (details) => _drag += details.delta.dx,
                onHorizontalDragEnd: (_) {
                  if (_drag < -32 && hasNext) _move(1, count);
                  if (_drag > 32 && hasPrevious) _move(-1, count);
                },
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    for (var i = 0; i < rows.length; i++) ...[
                      if (i > 0) const SizedBox(width: 8),
                      Expanded(
                        child: _StoryCard(
                          key: ValueKey('place-story:${rows[i].id}'),
                          moment: rows[i],
                          onOpen: widget.onOpen == null
                              ? null
                              : () => widget.onOpen!(rows[i]),
                        ),
                      ),
                    ],
                    for (var i = rows.length; i < count; i++) ...[
                      const SizedBox(width: 8),
                      const Expanded(child: SizedBox()),
                    ],
                  ],
                ),
              ),
              if (hasPrevious || hasNext)
                Row(
                  children: [
                    IconButton(
                      tooltip: '上一组公开动态',
                      constraints: const BoxConstraints(
                        minWidth: 48,
                        minHeight: 48,
                      ),
                      onPressed: hasPrevious ? () => _move(-1, count) : null,
                      icon: const Icon(Icons.chevron_left, size: 20),
                    ),
                    Expanded(
                      child: Text(
                        '${start + 1}–${start + rows.length} / ${widget.moments.length} 条已读取动态',
                        textAlign: TextAlign.center,
                        style: tokens.metadata,
                      ),
                    ),
                    IconButton(
                      tooltip: '下一组公开动态',
                      constraints: const BoxConstraints(
                        minWidth: 48,
                        minHeight: 48,
                      ),
                      onPressed: hasNext ? () => _move(1, count) : null,
                      icon: const Icon(Icons.chevron_right, size: 20),
                    ),
                  ],
                ),
            ],
          ),
        );
      },
    );
  }
}

class _StoryCard extends StatelessWidget {
  const _StoryCard({super.key, required this.moment, required this.onOpen});
  final PlaceHistoryMoment moment;
  final VoidCallback? onOpen;

  @override
  Widget build(BuildContext context) {
    final tokens = NowUiTokens.of(context);
    final at = moment.publishedAt.toLocal();
    return Material(
      color: Theme.of(context).colorScheme.surfaceContainerLow,
      borderRadius: BorderRadius.circular(14),
      child: InkWell(
        borderRadius: BorderRadius.circular(14),
        onTap: moment.id == null ? null : onOpen,
        child: ConstrainedBox(
          constraints: const BoxConstraints(minHeight: 48),
          child: Padding(
            padding: const EdgeInsets.all(8),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(moment.title, style: tokens.placeTitle),
                const SizedBox(height: 4),
                Text(
                  moment.authorPublicProfile?.displayName ?? '作者资料未公开',
                  style: tokens.metadata,
                ),
                Text('公开于 ${at.month}月${at.day}日', style: tokens.metadata),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

// Only the independent current public Place photo supplies these pixels.
// Never borrow private attachments, visitor moments or design art for a POI.
