import 'dart:async';
import 'dart:math' as math;
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:video_player/video_player.dart';

import '../content/video_material_player_stub.dart'
    if (dart.library.io) '../content/video_material_player_io.dart'
    if (dart.library.html) '../content/video_material_player_web.dart'
    as local_video;
import 'owned_map_api.dart';
import 'owned_map_controller.dart';
import 'map_media_read_metrics.dart';
import 'moment_interaction.dart';

/// Read-only content stays over the original Now map. It never grants public
/// access or changes the source of a legacy record.
class NowMapReadingPosition {
  double offset = 0;
  double collectionOffset = 0;
  String? threadSeedMomentID;
  String? currentMomentID;
  bool expanded = false;
  final mediaPages = <String, int>{};
}

class _ReaderFrame {
  const _ReaderFrame({
    required this.items,
    required this.thread,
    required this.nextCursor,
    required this.collectionItems,
    required this.collectionCursor,
    required this.collectionOffset,
    required this.seedID,
    required this.selectedID,
    required this.offset,
    required this.placeID,
    required this.placeName,
    required this.relation,
    required this.placeOriginMomentID,
    required this.desired,
    required this.expanded,
    required this.reply,
  });
  final List<OwnedMapMomentDetail> items;
  final OwnedMapThread? thread;
  final String? nextCursor,
      collectionCursor,
      seedID,
      selectedID,
      placeID,
      placeName,
      placeOriginMomentID;
  final List<OwnedMapMomentDetail>? collectionItems;
  final OwnedMapMomentDetail? relation;
  final double offset, collectionOffset;
  final int desired;
  final bool expanded;
  final MomentComment? reply;
}

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
    this.readingPosition,
    this.interactionClient,
    this.onCommentTarget,
    this.onCommentIntent,
  });
  final bool initialExpanded;
  final String? closeLabel;
  final NowMapReadingPosition? readingPosition;
  final OwnedMapController controller;
  final http.Client? interactionClient;
  final void Function(
    MomentInteractionController? controller,
    OwnedMapMomentDetail? moment,
    MomentComment? reply,
  )?
  onCommentTarget;
  final VoidCallback? onCommentIntent;
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
  OwnedMapThread? _thread;
  List<OwnedMapMomentDetail>? _collectionItems;
  String? _collectionCursor, _activeSeedID;
  double _collectionOffset = 0;
  Listenable? _changes;
  Timer? _renew, _expiry;
  String? _nextCursor, _error;
  late int _epoch;
  int _request = 0, _desiredCount = 1;
  String? _viewerMoment;
  final _mediaPages = <String, int>{};
  final _entryKeys = <String, GlobalKey>{};
  final _backStack = <_ReaderFrame>[];
  String? _placeID, _placeName, _placeOriginMomentID, _selectedMomentID;
  OwnedMapMomentDetail? _placeRelation;
  MomentInteractionController? _interaction;
  String? _interactionBinding;
  String? _blockedInteractionBinding;
  bool _interactionRetiring = false;
  MomentComment? _commentReply;
  OwnedMapMomentDetail? _lastCommentDetail;
  MomentComment? _lastCommentReply;
  int _targetNotice = 0;
  bool _pickScheduled = false;
  late final _images = _MapReaderImages(() => c, () => _current);
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

  bool get _contentMode =>
      _collectionItems != null ||
      _placeID == null &&
          (_livePin?.isMomentAnchor == true ||
              _items.length == 1 && _nextCursor == null);

  OwnedMapMomentDetail? get _selectedDetail => _contentMode
      ? _items
            .where((d) => d.id == _selectedMomentID && c.detailCurrent(d))
            .firstOrNull
      : null;

  void _disposeInteraction() {
    final old = _interaction;
    _interaction = null;
    _interactionBinding = null;
    old?.removeListener(_interactionChanged);
    old?.dispose();
  }

  bool _publicCommentSource(OwnedMapMomentDetail detail) =>
      detail.visibility == 'public' && detail.status == 'published';

  void _notifyCommentTarget() {
    final notice = ++_targetNotice;
    final selected = _current ? _selectedDetail : null;
    final publicSelected = selected != null && _publicCommentSource(selected);
    final explicitReadOnly =
        selected != null &&
        !publicSelected &&
        selected.id != _lastCommentDetail?.id;
    final explicitList =
        _current && !_contentMode && (_placeID != null || _hasRead) ||
        explicitReadOnly;
    if (publicSelected) {
      _lastCommentDetail = selected;
      _lastCommentReply = _commentReply;
    } else if (explicitList) {
      _lastCommentDetail = null;
      _lastCommentReply = null;
    }
    // The same public target becoming unavailable keeps its original draft
    // identity. Reading a different private Moment is an explicit new target,
    // so it must not inherit the previous Moment's public comment mode.
    final detail = explicitList
        ? null
        : publicSelected
        ? selected
        : _lastCommentDetail;
    final reply = explicitList
        ? null
        : selected == null
        ? _lastCommentReply
        : _commentReply;
    final interaction = publicSelected ? _interaction : null;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || notice != _targetNotice) return;
      final valid =
          _current &&
          publicSelected &&
          detail != null &&
          _selectedDetail?.id == detail.id &&
          c.detailCurrent(detail) &&
          identical(interaction, _interaction) &&
          (interaction == null || interaction.current());
      widget.onCommentTarget?.call(valid ? interaction : null, detail, reply);
    });
    WidgetsBinding.instance.ensureVisualUpdate();
  }

  void _interactionChanged() {
    final interaction = _interaction, detail = _selectedDetail;
    if (interaction != null &&
        detail != null &&
        interaction.page != null &&
        interaction.page!.revision != detail.revision) {
      _blockedInteractionBinding = _interactionBinding;
      // A newer comment page is not approval to comment on an older body.
      // Retire after notifyListeners unwinds; current() rejects immediately.
      if (!_interactionRetiring) {
        _interactionRetiring = true;
        WidgetsBinding.instance.addPostFrameCallback((_) {
          _interactionRetiring = false;
          if (!mounted || !identical(interaction, _interaction)) return;
          setState(_disposeInteraction);
          _notifyCommentTarget();
        });
      }
    }
    _notifyCommentTarget();
  }

  void _syncInteraction({String? preferred}) {
    final ids = _items.map((d) => d.id).toSet();
    _entryKeys.removeWhere((id, _) => !ids.contains(id));
    if (!_current || !_contentMode || _items.isEmpty) {
      _disposeInteraction();
      _notifyCommentTarget();
      return;
    }
    final previous = _selectedMomentID;
    final live = _items.where(c.detailCurrent).toList();
    final detail = previous == null
        ? live.where((d) => d.id == preferred).firstOrNull ?? live.firstOrNull
        : live.where((d) => d.id == previous).firstOrNull;
    if (detail == null) {
      _disposeInteraction();
      _notifyCommentTarget();
      return;
    }
    _selectedMomentID = detail.id;
    if (previous != detail.id) _commentReply = null;
    final client = widget.interactionClient;
    if (client == null ||
        !_publicCommentSource(detail) ||
        c.organizationWorkspaceID() != null) {
      _disposeInteraction();
      _notifyCommentTarget();
      return;
    }
    final id = detail.id,
        revision = detail.revision,
        source = detail.sourceVersion,
        base = c.apiBaseUrl(),
        owner = c.ownerID(),
        header = c.authorizationHeader();
    final binding = '$id:$revision:$source:$base:$owner:$header';
    if (_blockedInteractionBinding == binding) {
      _disposeInteraction();
      _notifyCommentTarget();
      return;
    }
    if (_interaction == null || _interactionBinding != binding) {
      _disposeInteraction();
      _interactionBinding = binding;
      final controller = MomentInteractionController(
        momentID: id,
        authorizationHeader: c.authorizationHeader,
        ownerID: c.ownerID,
        apiBaseUrl: base,
        client: client,
        identityChanges: c.identityChanges,
        current: () {
          final fresh = _selectedDetail;
          return _current &&
              _blockedInteractionBinding != binding &&
              identical(widget.interactionClient, client) &&
              c.apiBaseUrl() == base &&
              c.ownerID() == owner &&
              c.authorizationHeader() == header &&
              fresh?.id == id &&
              fresh?.revision == revision &&
              fresh?.sourceVersion == source;
        },
      );
      _interaction = controller;
      controller.addListener(_interactionChanged);
      unawaited(controller.load());
    }
    _notifyCommentTarget();
  }

  void _selectMoment(OwnedMapMomentDetail detail, {MomentComment? reply}) {
    if (!_current || !_contentMode || !c.detailCurrent(detail)) return;
    if (reply != null &&
        (_interaction?.momentID != detail.id ||
            _interaction?.replyCurrent(reply) != true))
      return;
    setState(() {
      _selectedMomentID = detail.id;
      _commentReply =
          reply; // Only an explicit top-level action clears a reply.
      _syncInteraction(preferred: detail.id);
    });
    _rememberPosition();
  }

  void _pickVisibleMoment() {
    if (_pickScheduled ||
        !_expanded ||
        !_contentMode ||
        !_current ||
        !_scroll.hasClients ||
        !_scroll.position.isScrollingNotifier.value)
      return;
    _pickScheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _pickScheduled = false;
      if (!_current || !_contentMode) return;
      final box = context.findRenderObject();
      if (box is! RenderBox || !box.hasSize) return;
      final origin = box.localToGlobal(Offset.zero);
      OwnedMapMomentDetail? best;
      var visible = 0.0;
      for (final detail in _items) {
        final tile = _entryKeys[detail.id]?.currentContext?.findRenderObject();
        if (tile is! RenderBox || !tile.hasSize || !c.detailCurrent(detail))
          continue;
        final top = tile.localToGlobal(Offset.zero).dy - origin.dy;
        final extent =
            math.min(top + tile.size.height, box.size.height) -
            math.max(52.0, top);
        if (extent > visible) {
          visible = extent;
          best = detail;
        }
      }
      if (best != null && best.id != _selectedMomentID) _selectMoment(best);
    });
  }

  Widget _commentsSection() {
    final detail = _selectedDetail;
    if (!_expanded ||
        !_current ||
        detail == null ||
        widget.interactionClient == null)
      return const SizedBox.shrink();
    final interaction = _interaction;
    final changed =
        _blockedInteractionBinding?.startsWith(
              '${detail.id}:${detail.revision}:${detail.sourceVersion}:',
            ) ==
            true ||
        interaction?.page != null &&
            interaction!.page!.revision != detail.revision;
    final position = _thread?.entries
        .where((e) => e.detail?.id == detail.id)
        .firstOrNull
        ?.position;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          position == null ? '当前动态的评论' : '第${position + 1}段的评论',
          key: ValueKey('reading-comments:${detail.id}'),
          style: Theme.of(context).textTheme.titleSmall,
        ),
        if (changed) ...[
          const Text('动态版本已变化，请重新读取正文后再评论'),
          TextButton(
            onPressed: _loading
                ? null
                : () {
                    _blockedInteractionBinding = null;
                    unawaited(_read());
                  },
            child: const Text('重新读取动态'),
          ),
        ] else if (interaction == null)
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 12),
            child: Text('这条动态暂不支持公开评论'),
          )
        else
          MomentInteractionPanel(
            key: ObjectKey(interaction),
            controller: interaction,
            momentID: detail.id,
            authorizationHeader: c.authorizationHeader,
            ownerID: c.ownerID,
            apiBaseUrl: c.apiBaseUrl(),
            client: widget.interactionClient!,
            identityChanges: c.identityChanges,
            current: interaction.current,
            showComposer: false,
            onReply: (reply) {
              if (!interaction.replyCurrent(reply)) return;
              widget.onCommentIntent?.call();
              _selectMoment(detail, reply: reply);
            },
            onComment: () {
              if (!interaction.canComment) return;
              widget.onCommentIntent?.call();
              _selectMoment(detail);
            },
          ),
      ],
    );
  }

  _ReaderFrame _captureReader() => _ReaderFrame(
    items: _items,
    thread: _thread,
    nextCursor: _nextCursor,
    collectionItems: _collectionItems,
    collectionCursor: _collectionCursor,
    collectionOffset: _collectionOffset,
    seedID: _activeSeedID,
    selectedID: _selectedMomentID,
    offset: _scroll.hasClients ? _scroll.offset : 0,
    placeID: _placeID,
    placeName: _placeName,
    relation: _placeRelation,
    placeOriginMomentID: _placeOriginMomentID,
    desired: _desiredCount,
    expanded: _expanded,
    reply: _commentReply,
  );

  void _openPlace(OwnedMapMomentDetail source, OwnedMapLocation location) {
    if (!_current ||
        !c.detailCurrent(source) ||
        !source.locations.any((p) => p.placeID == location.placeID))
      return;
    if (_backStack.length >= 8) {
      setState(() => _error = '请先返回上一层内容');
      return;
    }
    _backStack.add(_captureReader());
    ++_request;
    setState(() {
      _placeID = location.placeID;
      _placeName = location.name;
      _placeRelation = source;
      _placeOriginMomentID = source.id;
      _items = const [];
      _thread = null;
      _collectionItems = null;
      _collectionCursor = _nextCursor = _activeSeedID = _selectedMomentID =
          null;
      _collectionOffset = _restoreOffset = 0;
      _desiredCount = 20;
      _expanded = true;
      _loading = _hasRead = false;
      _viewerMoment = null;
      _error = null;
      _syncInteraction();
    });
    widget.onExpand();
    unawaited(_read());
  }

  void _returnToContent() {
    if (!_current || _backStack.isEmpty) return;
    final frame = _backStack.removeLast();
    ++_request;
    setState(() {
      _items = List.unmodifiable(frame.items.where(c.detailCurrent));
      _thread = frame.thread;
      _nextCursor = frame.nextCursor;
      _collectionItems = frame.collectionItems;
      _collectionCursor = frame.collectionCursor;
      _collectionOffset = frame.collectionOffset;
      _activeSeedID = frame.seedID;
      _selectedMomentID = frame.selectedID;
      _commentReply = frame.reply;
      _restoreOffset = frame.offset;
      _placeID = frame.placeID;
      _placeName = frame.placeName;
      _placeRelation = frame.relation;
      _placeOriginMomentID = frame.placeOriginMomentID;
      _desiredCount = frame.desired;
      _expanded = frame.expanded;
      _loading = false;
      _hasRead = _items.isNotEmpty;
      _error = null;
      _syncInteraction();
      _armLease();
    });
    _rememberPosition();
    unawaited(_read());
    final request = _request;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_current && request == _request && _scroll.hasClients)
        _scroll.jumpTo(
          frame.offset.clamp(
            _scroll.position.minScrollExtent,
            _scroll.position.maxScrollExtent,
          ),
        );
    });
  }

  @override
  void initState() {
    super.initState();
    _epoch = c.permissionEpoch;
    _expanded =
        widget.initialExpanded || widget.readingPosition?.expanded == true;
    _restoreOffset = widget.readingPosition?.offset ?? 0;
    _collectionOffset = widget.readingPosition?.collectionOffset ?? 0;
    _activeSeedID = widget.readingPosition?.threadSeedMomentID;
    _selectedMomentID = widget.readingPosition?.currentMomentID;
    _mediaPages.addAll(widget.readingPosition?.mediaPages ?? const {});
    _scroll.addListener(_rememberPosition);
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
      _activeSeedID = null;
      _collectionItems = null;
      _collectionCursor = null;
      _placeID = _placeName = _placeOriginMomentID = null;
      _placeRelation = null;
      _selectedMomentID = null;
      _backStack.clear();
      _mediaPages.clear();
      if (_scroll.hasClients) _scroll.jumpTo(0);
      unawaited(_read());
    } else if (!_current) {
      _retire();
    } else if (oldWidget.interactionClient != widget.interactionClient) {
      _disposeInteraction();
      _syncInteraction();
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
    _disposeInteraction();
    _notifyCommentTarget();
    _images.clear();
    _rememberPosition();
    if (_items.isNotEmpty && _scroll.hasClients)
      _restoreOffset = _scroll.offset;
    ++_request;
    _renew?.cancel();
    _expiry?.cancel();
    _items = const [];
    _thread = null;
    _nextCursor = null;
    _loading = false;
    _hasRead = false;
    _backStack.clear();
    _collectionItems = null;
    _placeRelation = null;
    _entryKeys.clear();
    _placeID = _placeName = _placeOriginMomentID = _activeSeedID =
        _selectedMomentID = null;
    _commentReply = null;
    _blockedInteractionBinding = null;
  }

  void _sourceChanged() {
    if (!mounted) return;
    if (!_current) {
      _retire();
    } else if (!_hasRead && !_loading) {
      unawaited(_read());
    }
    _images.prune();
    _syncInteraction();
    setState(() {});
  }

  void _armLease() {
    _renew?.cancel();
    _expiry?.cancel();
    if (_items.isEmpty) return;
    final now = DateTime.now().toUtc();
    var until = _items
        .map((item) => item.validUntil)
        .reduce((a, b) => a.isBefore(b) ? a : b);
    if (_thread != null && _thread!.validUntil.isBefore(until))
      until = _thread!.validUntil;
    if (_placeRelation != null && _placeRelation!.validUntil.isBefore(until))
      until = _placeRelation!.validUntil;
    final remaining = until.difference(now);
    if (remaining <= Duration.zero) {
      _items = const [];
      _syncInteraction();
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
        if (_placeRelation != null && !c.detailCurrent(_placeRelation!))
          _items = const [];
        _nextCursor = null;
        _hasRead = _items.isNotEmpty;
        if (_items.isNotEmpty) _armLease();
        _syncInteraction();
      });
    });
    if (remaining > const Duration(seconds: 2)) {
      _renew = Timer(remaining ~/ 2, () => unawaited(_read()));
    }
  }

  Future<void> _read({bool more = false}) async {
    if (!_current || _loading) return;
    final trace = MapMediaReadMetrics.begin(MapMediaReadKind.detail);
    final pin = _livePin!, request = ++_request;
    final desired = more
        ? math.max(_desiredCount, _items.length) + 20
        : _desiredCount;
    bool pending() => _current && request == _request;
    _loading = true;
    _error = null;
    var enteringThread = false;
    if (mounted) setState(() {});
    try {
      // Renew the already loaded pages with fresh cursors. Keep list identity,
      // expansion, media selection and scroll position when the pin is equal.
      final collected = <OwnedMapMomentDetail>[];
      final seen = <String>{}, cursors = <String>{};
      String? cursor;
      if (_placeID != null && _placeRelation != null && !more) {
        final original = _placeRelation!;
        final fresh = await c.readMoment(original.selector);
        if (!pending()) return;
        if (fresh.id != original.id ||
            fresh.revision != original.revision ||
            fresh.sourceVersion != original.sourceVersion ||
            !fresh.locations.any((p) => p.placeID == _placeID))
          throw const FormatException('原地点关联已变化');
        _placeRelation = fresh;
      }
      if (pin.isMomentAnchor && _placeID == null) {
        final target = pin.moments
            .where((item) => item.id == pin.entryID)
            .single;
        trace.requests++;
        collected.add(await c.readMoment(target));
      } else
        do {
          trace.requests++;
          final page = _placeID == null
              ? await c.readPlaceMoments(pin, cursor: cursor)
              : await c.readPlaceMoments(
                  pin,
                  cursor: cursor,
                  placeID: _placeID,
                  relationSource: _placeRelation,
                );
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
      OwnedMapThread? thread;
      final seedID =
          _activeSeedID ??
          (_placeID != null
              ? null
              : pin.isMomentAnchor
              ? pin.entryID
              : collected.length == 1 && cursor == null
              ? collected.first.id
              : null);
      OwnedMapMomentDetail? seed;
      if (_expanded && seedID != null) {
        seed = collected.where((item) => item.id == seedID).firstOrNull;
        if (seed == null) {
          _thread = null;
          _items = const [];
          throw const FormatException('原动态已不可读取');
        }
        trace.requests++;
        thread = await c.readThread(pin, seed);
        if (!pending()) return;
        if ((!pin.isMomentAnchor || _placeID != null) &&
            (_collectionItems != null ||
                collected.length > 1 ||
                cursor != null)) {
          _collectionItems = List.unmodifiable(collected);
          _collectionCursor = cursor;
        }
        if (thread != null || _collectionItems != null) _activeSeedID = seed.id;
      }
      enteringThread = _thread == null && thread != null;
      _thread = thread;
      _items = List.unmodifiable(
        thread == null
            ? (_collectionItems != null && seed != null ? [seed] : collected)
            : thread.entries
                  .map((entry) => entry.detail)
                  .whereType<OwnedMapMomentDetail>(),
      );
      _hasRead = true;
      _nextCursor = _collectionItems != null ? null : cursor;
      _desiredCount = math.max(1, collected.length);
      _syncInteraction(preferred: seedID);
      _armLease();
      trace.mark(MapMediaReadStage.detailReady);
    } catch (_) {
      trace.mark(MapMediaReadStage.failed);
      if (pending()) _error = '动态暂不可读，请重新读取。';
    } finally {
      if (pending()) {
        _loading = false;
        _syncInteraction();
        setState(() {});
        if ((_restoreOffset != 0 || enteringThread) && _items.isNotEmpty) {
          WidgetsBinding.instance.addPostFrameCallback((_) {
            if (!pending() || !_scroll.hasClients) return;
            _scroll.jumpTo(
              _restoreOffset.clamp(
                _scroll.position.minScrollExtent,
                _scroll.position.maxScrollExtent,
              ),
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
      final thread = c.threadForDetail(fresh);
      if (thread != null) {
        _thread = thread;
        _items = List.unmodifiable(
          thread.entries
              .map((entry) => entry.detail)
              .whereType<OwnedMapMomentDetail>()
              .where(c.detailCurrent),
        );
      } else {
        _items = List.unmodifiable(next.where(c.detailCurrent));
      }
      _armLease();
      _syncInteraction();
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

  void _rememberPosition() {
    _pickVisibleMoment();
    final position = widget.readingPosition;
    if (position == null) return;
    if (_backStack.isNotEmpty) {
      final original = _backStack.first;
      position.offset = original.offset;
      position.collectionOffset = original.collectionOffset;
      position.expanded = original.expanded;
      position.threadSeedMomentID = original.seedID;
      position.currentMomentID = original.selectedID;
      position.mediaPages.addAll(_mediaPages);
      return;
    }
    if (_scroll.hasClients) position.offset = _scroll.offset;
    position.expanded = _expanded;
    position.threadSeedMomentID = _activeSeedID;
    position.currentMomentID = _selectedMomentID;
    position.collectionOffset = _collectionOffset;
    position.mediaPages.addAll(_mediaPages);
  }

  void _expand() {
    if (!_current) return;
    setState(() => _expanded = true);
    widget.onExpand();
    _rememberPosition();
    unawaited(_readCurrentThread());
  }

  Future<void> _readCurrentThread({OwnedMapMomentDetail? selected}) async {
    if (!_current ||
        _loading ||
        _items.isEmpty ||
        selected == null &&
            !(_livePin!.isMomentAnchor ||
                _items.length == 1 && _nextCursor == null))
      return;
    final request = ++_request, pin = _livePin!;
    final seed = selected ?? _items.first;
    final originItems = _items, originCursor = _nextCursor;
    final originOffset = _scroll.hasClients ? _scroll.offset : 0.0;
    bool pending() => _current && request == _request;
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final thread = await c.readThread(pin, seed);
      if (!pending() || !c.detailCurrent(seed)) return;
      if (thread == null && selected == null) return;
      if (selected != null && (!pin.isMomentAnchor || _placeID != null)) {
        _collectionItems = originItems;
        _collectionCursor = originCursor;
        _collectionOffset = originOffset;
      }
      _activeSeedID = seed.id;
      _selectedMomentID = seed.id;
      _commentReply = null;
      _thread = thread;
      _items = List.unmodifiable(
        thread == null
            ? [seed]
            : thread.entries
                  .map((entry) => entry.detail)
                  .whereType<OwnedMapMomentDetail>(),
      );
      _nextCursor = null;
      _restoreOffset = 0;
      _armLease();
      _syncInteraction(preferred: seed.id);
      _rememberPosition();
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (pending() && _scroll.hasClients) _scroll.jumpTo(0);
      });
    } catch (_) {
      if (pending()) _error = '串文暂时无法加载';
    } finally {
      if (pending()) setState(() => _loading = false);
    }
  }

  void _returnToCollection() {
    if (!_current || _collectionItems == null) return;
    ++_request;
    setState(() {
      _thread = null;
      _activeSeedID = null;
      _items = List.unmodifiable(_collectionItems!.where(c.detailCurrent));
      _nextCursor = _collectionCursor;
      _restoreOffset = _collectionOffset;
      _collectionItems = null;
      _collectionCursor = null;
      _loading = false;
      _error = null;
      _syncInteraction();
    });
    _rememberPosition();
    _armLease();
    unawaited(_read());
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
          _restoreOffset.clamp(
            _scroll.position.minScrollExtent,
            _scroll.position.maxScrollExtent,
          ),
        );
      }
    });
  }

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) {
      final current = _current, pin = current ? _livePin : null;
      final thread = _expanded && _thread != null && c.threadCurrent(_thread!)
          ? _thread
          : null;
      final entryCount = thread?.entries.length ?? _items.length;
      final availableHeight = constraints.hasBoundedHeight
          ? constraints.maxHeight
          : 560.0;
      final height = _expanded
          ? availableHeight
          : math.min(288.0, availableHeight);
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
                    images: _images,
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
      // The Now host owns the interruptible expansion transition. A second
      // height tween here changes the scroll viewport after a reader has
      // already scrolled, clamping the retained position during renewal.
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
                      if (_collectionItems != null)
                        IconButton(
                          tooltip: '返回地点动态',
                          icon: const Icon(Icons.arrow_back),
                          onPressed: _returnToCollection,
                        )
                      else if (_backStack.isNotEmpty)
                        IconButton(
                          tooltip: '返回原动态',
                          icon: const Icon(Icons.arrow_back),
                          onPressed: _returnToContent,
                        )
                      else
                        Icon(
                          pin?.isMomentAnchor == true
                              ? Icons.photo_outlined
                              : Icons.place_outlined,
                          size: 20,
                          semanticLabel: pin?.isMomentAnchor == true
                              ? '动态'
                              : '地点',
                        ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: Text(
                          thread != null
                              ? '串文 · ${thread.entries.length}段'
                              : _collectionItems != null
                              ? '动态'
                              : _placeID != null
                              ? '这里的动态 · ${_placeName ?? '地点'}'
                              : pin == null
                              ? '动态'
                              : pin.isMomentAnchor
                              ? (pin.threadID.isEmpty ? '动态' : '串文中的动态')
                              : pin.name,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: Theme.of(context).textTheme.titleMedium,
                        ),
                      ),
                      if (thread == null &&
                          _collectionItems == null &&
                          pin != null &&
                          _placeID == null &&
                          !pin.isMomentAnchor &&
                          pin.momentCount > 0)
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
                    : _MapReadingList(
                        key: const PageStorageKey('now-map-content-scroll'),
                        controller: _scroll,
                        thread: thread,
                        seedMomentID: _activeSeedID,
                        padding: EdgeInsets.only(bottom: _expanded ? 16 : 0),
                        itemCount:
                            (_expanded
                                ? entryCount
                                : math.min(1, _items.length)) +
                            1,
                        findChildIndexCallback: (key) {
                          final count = _expanded
                              ? entryCount
                              : math.min(1, _items.length);
                          for (var index = 0; index < count; index++) {
                            final item =
                                thread?.entries[index].detail ??
                                (thread == null ? _items[index] : null);
                            if (item != null && key == _entryKey(item))
                              return index;
                          }
                          return null;
                        },
                        itemBuilder: (context, index) {
                          final visible = _expanded
                              ? entryCount
                              : math.min(1, _items.length);
                          if (index == visible) {
                            return Padding(
                              padding: const EdgeInsets.symmetric(
                                horizontal: 16,
                              ),
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  if (_expanded) _commentsSection(),
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
                          final entry = thread?.entries[index];
                          final item =
                              entry?.detail ??
                              (thread == null ? _items[index] : null);
                          if (item == null)
                            return Padding(
                              key: ValueKey('thread-unavailable:$index'),
                              padding: const EdgeInsets.all(16),
                              child: Text('第${entry!.position + 1}段暂不可查看'),
                            );
                          if (_placeID != null && _collectionItems == null) {
                            return ListTile(
                              key: _entryKey(item),
                              title: Text(
                                item.title.trim().isNotEmpty
                                    ? item.title
                                    : item.body,
                                maxLines: 2,
                                overflow: TextOverflow.ellipsis,
                              ),
                              subtitle: Text(
                                '${_momentTime(item)}${item.id == _placeOriginMomentID ? ' · 正在阅读' : ''}',
                              ),
                              trailing: const Icon(Icons.chevron_right),
                              onTap: () =>
                                  unawaited(_readCurrentThread(selected: item)),
                            );
                          }
                          return Align(
                            key: _entryKey(item),
                            alignment: Alignment.topCenter,
                            child: ConstrainedBox(
                              constraints: const BoxConstraints(maxWidth: 720),
                              child: Column(
                                key: _entryKeys.putIfAbsent(
                                  item.id,
                                  () => GlobalKey(),
                                ),
                                crossAxisAlignment: CrossAxisAlignment.stretch,
                                children: [
                                  if (entry != null)
                                    Padding(
                                      padding: const EdgeInsets.fromLTRB(
                                        16,
                                        12,
                                        16,
                                        4,
                                      ),
                                      child: Row(
                                        children: [
                                          Text(
                                            '第${entry.position + 1}段',
                                            style: Theme.of(
                                              context,
                                            ).textTheme.labelLarge,
                                          ),
                                          if (_selectedMomentID == item.id) ...[
                                            const SizedBox(width: 8),
                                            Text(
                                              '正在阅读',
                                              key: ValueKey(
                                                'current-thread-entry:${item.id}',
                                              ),
                                              style: Theme.of(
                                                context,
                                              ).textTheme.bodySmall,
                                            ),
                                          ],
                                        ],
                                      ),
                                    ),
                                  _MapMomentTile(
                                    key: ValueKey(
                                      '${item.id}:${item.revision}:${item.sourceVersion}',
                                    ),
                                    controller: c,
                                    images: _images,
                                    detail: item,
                                    onFreshDetail: _acceptFreshDetail,
                                    compact: !_expanded,
                                    current: () =>
                                        _current && c.detailCurrent(item),
                                    page: _mediaPages[item.id] ?? 0,
                                    onPageChanged: (page) =>
                                        _mediaPages[item.id] = page,
                                    onOpenMedia: (page) =>
                                        _openMedia(item, page),
                                    onExpand: _expand,
                                    onOpenPlace: (location) =>
                                        _openPlace(item, location),
                                    onReadThread:
                                        _expanded &&
                                            thread == null &&
                                            _collectionItems == null
                                        ? () => unawaited(
                                            _readCurrentThread(selected: item),
                                          )
                                        : null,
                                    onEdit: () => widget.onEdit(
                                      item.id,
                                      () =>
                                          _current &&
                                          c.detailCurrent(item) &&
                                          item.visibilitySource ==
                                              'SELF_PRIVATE' &&
                                          item.canEdit,
                                    ),
                                  ),
                                  if (_expanded &&
                                      _contentMode &&
                                      _publicCommentSource(item) &&
                                      c.detailCurrent(item))
                                    Padding(
                                      padding: const EdgeInsets.symmetric(
                                        horizontal: 16,
                                      ),
                                      child: TextButton.icon(
                                        key: ValueKey(
                                          'comment-target:${item.id}',
                                        ),
                                        icon: Icon(
                                          _selectedMomentID == item.id
                                              ? Icons.chat_bubble
                                              : Icons.chat_bubble_outline,
                                        ),
                                        label: Text(
                                          entry == null
                                              ? '评论这条动态'
                                              : '评论第${entry.position + 1}段',
                                        ),
                                        onPressed: () => _selectMoment(item),
                                      ),
                                    ),
                                ],
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
                    child: Text(
                      pin.isMomentAnchor
                          ? '查看动态'
                          : '查看此地点的全部${pin.momentCount}条动态',
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

/// Two bounded slivers keep the selected authoritative position at zero.
/// Earlier positions remain above it, without building every media tile just
/// to discover an estimated scroll offset.
class _MapReadingList extends StatelessWidget {
  const _MapReadingList({
    super.key,
    required this.controller,
    required this.thread,
    required this.seedMomentID,
    required this.padding,
    required this.itemCount,
    required this.itemBuilder,
    required this.findChildIndexCallback,
  });
  final ScrollController controller;
  final OwnedMapThread? thread;
  final String? seedMomentID;
  final EdgeInsets padding;
  final int itemCount;
  final IndexedWidgetBuilder itemBuilder;
  final int? Function(Key) findChildIndexCallback;

  @override
  Widget build(BuildContext context) {
    final value = thread;
    if (value == null) {
      return ListView.builder(
        key: const PageStorageKey('now-map-place-entries'),
        controller: controller,
        padding: padding,
        itemCount: itemCount,
        itemBuilder: itemBuilder,
        findChildIndexCallback: findChildIndexCallback,
      );
    }
    final requested = value.entries.indexWhere(
      (entry) => entry.detail?.id == seedMomentID,
    );
    final start = math.max(0, requested);
    final center = ValueKey('thread-center:${value.threadID}:$seedMomentID');
    return CustomScrollView(
      key: PageStorageKey('thread-scroll:${value.threadID}:$seedMomentID'),
      controller: controller,
      center: center,
      slivers: [
        SliverList(
          delegate: SliverChildBuilderDelegate(
            (context, index) => itemBuilder(context, start - index - 1),
            childCount: start,
            findChildIndexCallback: (key) {
              final index = findChildIndexCallback(key);
              return index != null && index < start ? start - index - 1 : null;
            },
          ),
        ),
        SliverPadding(
          key: center,
          padding: padding,
          sliver: SliverList(
            delegate: SliverChildBuilderDelegate(
              (context, index) => itemBuilder(context, start + index),
              childCount: itemCount - start,
              findChildIndexCallback: (key) {
                final index = findChildIndexCallback(key);
                return index != null && index >= start ? index - start : null;
              },
            ),
          ),
        ),
      ],
    );
  }
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
    required this.images,
    required this.detail,
    required this.onFreshDetail,
    required this.current,
    required this.compact,
    required this.onEdit,
    required this.page,
    required this.onPageChanged,
    required this.onOpenMedia,
    required this.onExpand,
    required this.onOpenPlace,
    this.onReadThread,
  });
  final OwnedMapController controller;
  final _MapReaderImages images;
  final OwnedMapMomentDetail detail;
  final bool Function(OwnedMapMomentDetail) onFreshDetail;
  final bool Function() current;
  final bool compact;
  final VoidCallback onEdit, onExpand;
  final VoidCallback? onReadThread;
  final int page;
  final ValueChanged<int> onPageChanged, onOpenMedia;
  final ValueChanged<OwnedMapLocation> onOpenPlace;

  @override
  Widget build(BuildContext context) {
    if (!current()) return const SizedBox.shrink();
    final colors = Theme.of(context).colorScheme;
    final title = detail.title.trim(), body = detail.body.trim();
    final author = detail.author, avatar = author?.avatar;
    final name = author?.displayName.trim();
    if (compact) {
      // Header and primary action use 100dp of the 288dp preview. The media
      // stays first; a single representative line never pushes it off screen.
      return Padding(
        padding: const EdgeInsets.symmetric(horizontal: 12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            if (detail.media.isNotEmpty)
              ClipRRect(
                borderRadius: BorderRadius.circular(12),
                child: _MapMediaPager(
                  controller: controller,
                  images: images,
                  detail: detail,
                  onFreshDetail: onFreshDetail,
                  current: current,
                  compact: true,
                  initialPage: page,
                  onPageChanged: onPageChanged,
                  onOpen: onOpenMedia,
                ),
              ),
            const SizedBox(height: 4),
            Text(
              '${name == null || name.isEmpty ? '作者未提供' : name} · ${_momentTime(detail)}',
              key: ValueKey('moment-author:${detail.id}'),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                fontSize: 12,
                height: 1.2,
                color: colors.onSurfaceVariant,
              ),
            ),
            if (body.isNotEmpty || title.isNotEmpty) ...[
              const SizedBox(height: 4),
              Text(
                body.isEmpty ? title : body,
                key: ValueKey('moment-preview-body:${detail.id}'),
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(
                  fontSize: 13,
                  height: 1.35,
                  color: colors.onSurface,
                ),
              ),
            ],
          ],
        ),
      );
    }
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
                maxLines: compact ? 1 : null,
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
                maxLines: compact ? 2 : null,
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
              images: images,
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
              child: Wrap(
                spacing: 8,
                runSpacing: 4,
                children: [
                  for (final location in detail.locations)
                    ActionChip(
                      key: ValueKey(
                        'moment-place:${detail.id}:${location.placeID}',
                      ),
                      materialTapTargetSize: MaterialTapTargetSize.padded,
                      avatar: const Icon(Icons.place_outlined, size: 18),
                      label: Text(location.name),
                      tooltip: '查看${location.name}的动态',
                      onPressed: () {
                        if (current()) onOpenPlace(location);
                      },
                    ),
                ],
              ),
            ),
          if (onReadThread != null)
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
              child: TextButton.icon(
                key: ValueKey('read-thread:${detail.id}'),
                onPressed: () {
                  if (current()) onReadThread!();
                },
                icon: const Icon(Icons.view_stream_outlined, size: 18),
                label: const Text('连续阅读'),
              ),
            ),
        ],
      ),
    );
  }
}

class _ReaderImage {
  _ReaderImage(this.bytes, this.until, this.detail, this.trace);
  final Uint8List bytes;
  final DateTime until;
  final OwnedMapMomentDetail detail;
  final MapMediaReadTrace? trace;
  void clear() => bytes.fillRange(0, bytes.length, 0);
}

class _ImageFlight {
  final consumers = <bool Function()>[];
  late Future<_ReaderImage> future;
  int waiting = 0;
  _ReaderImage? result;
}

/// Reader-owned, shared by preview, expanded list and fullscreen. Only selected
/// images are read; each consumer gets separate bytes to zero on retirement.
class _MapReaderImages {
  _MapReaderImages(this.controller, this.current);
  final OwnedMapController Function() controller;
  final bool Function() current;
  final _entries = <String, _ReaderImage>{};
  final _flights = <String, _ImageFlight>{};
  final _deadlines = Expando<DateTime>();
  final _traces = Expando<MapMediaReadTrace>();
  Timer? _expiry;
  int _epoch = 0, _bytes = 0;
  DateTime? deadline(Uint8List bytes) => _deadlines[bytes];
  MapMediaReadTrace? trace(Uint8List bytes) => _traces[bytes];
  String _key(OwnedMapMomentDetail detail, OwnedMapMomentMedia media) =>
      '${detail.viewerAccountID}:${detail.id}:${media.identity}';
  void evict(OwnedMapMomentDetail detail, OwnedMapMomentMedia media) {
    final retired = _entries.remove(_key(detail, media));
    if (retired != null) {
      _bytes -= retired.bytes.length;
      retired.clear();
    }
  }

  bool _valid(_ReaderImage value) =>
      current() &&
      value.until.isAfter(DateTime.now().toUtc()) &&
      controller().detailCurrent(value.detail);
  void clear() {
    ++_epoch;
    _expiry?.cancel();
    for (final value in _entries.values) {
      value.clear();
    }
    _entries.clear();
    _flights.clear();
    _bytes = 0;
  }

  void prune() {
    for (final key in _entries.keys.toList()) {
      if (!_valid(_entries[key]!)) {
        final retired = _entries.remove(key)!;
        _bytes -= retired.bytes.length;
        retired.clear();
      }
    }
    _expiry?.cancel();
    if (_entries.isNotEmpty) {
      final until = _entries.values
          .map((v) => v.until)
          .reduce((a, b) => a.isBefore(b) ? a : b);
      final remaining = until.difference(DateTime.now().toUtc());
      _expiry = Timer(remaining.isNegative ? Duration.zero : remaining, prune);
    }
  }

  Uint8List _copy(_ReaderImage value, {bool hit = false}) {
    final result = Uint8List.fromList(value.bytes);
    _deadlines[result] = value.until;
    final measured = hit
        ? MapMediaReadMetrics.begin(MapMediaReadKind.image)
        : value.trace;
    if (hit) {
      measured!.cacheHit = true;
      measured.mark(MapMediaReadStage.verified);
    }
    if (measured != null) _traces[result] = measured;
    return result;
  }

  Future<Uint8List> read(
    OwnedMapMomentDetail detail,
    OwnedMapMomentMedia media,
    bool Function() consumer,
  ) async {
    final c = controller(), epoch = _epoch;
    bool authorized() =>
        epoch == _epoch &&
        current() &&
        identical(controller(), c) &&
        c.detailCurrent(detail) &&
        detail.media.any((m) => identical(m, media));
    if (!authorized() || !consumer()) throw const FormatException('图片读取已失效');
    prune();
    // Detail snapshots differ between a single entry and its authoritative
    // thread. Reuse only this exact asset, under both current detail grants.
    final key = _key(detail, media);
    final cached = _entries.remove(key);
    if (cached != null) {
      _entries[key] = cached;
      return _copy(cached, hit: true);
    }
    var flight = _flights[key];
    if (flight == null) {
      flight = _ImageFlight();
      _flights[key] = flight;
      final job = flight;
      // Transfer authority needs a current reader and at least one visible
      // consumer. Mode handoff can join the same pending descriptor/request.
      bool pending() => authorized() && job.consumers.any((f) => f());
      job.future = Future.microtask(() async {
        final bytes = await c.momentContent(detail, media, current: pending);
        final until = c.contentValidUntil(bytes, detail);
        if (!pending() || !until.isAfter(DateTime.now().toUtc())) {
          bytes.fillRange(0, bytes.length, 0);
          throw const FormatException('图片读取已失效');
        }
        final value = job.result = _ReaderImage(
          bytes,
          until,
          detail,
          c.contentTrace(bytes),
        );
        if (bytes.length <= 1024 * 1024) {
          _entries[key] = value;
          _bytes += bytes.length;
          while (_entries.length > 4 || _bytes > 1024 * 1024) {
            final retired = _entries.remove(_entries.keys.first)!;
            _bytes -= retired.bytes.length;
            retired.clear();
          }
          prune();
        }
        return value;
      });
    }
    final job = flight;
    job.consumers.add(consumer);
    job.waiting++;
    try {
      final value = await job.future;
      if (!authorized() || !consumer() || !_valid(value))
        throw const FormatException('图片读取已失效');
      return _copy(value);
    } finally {
      job.waiting--;
      job.consumers.remove(consumer);
      if (job.waiting == 0) {
        if (identical(_flights[key], job)) _flights.remove(key);
        if (job.result != null && !_entries.values.contains(job.result))
          job.result!.clear();
      }
    }
  }
}

class _MapMediaPager extends StatefulWidget {
  const _MapMediaPager({
    super.key,
    required this.controller,
    required this.images,
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
  final _MapReaderImages images;
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
      // One reading frame for the whole gallery: a portrait/landscape/video
      // transition changes only the fitted content, never the surrounding list.
      final height = widget.full
          ? box.maxHeight
          : math.min(box.maxWidth * .75, widget.compact ? 144.0 : 360.0);
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
                    read: () => widget.images.read(detail, media, current),
                    contentUntil: widget.images.deadline,
                    contentTrace: widget.images.trace,
                    onRetry: () => widget.images.evict(detail, media),
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
                      '媒体 ${_index + 1} / ${widget.detail.media.length}',
                      style: const TextStyle(fontSize: 12, color: Colors.white),
                    ),
                  ),
                ),
              ),
            // Touch galleries swipe directly. Desktop/full-view navigation
            // retains buttons without covering the compact media preview.
            if (widget.detail.media.length > 1 && !widget.compact) ...[
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
    this.contentUntil,
    this.contentTrace,
    this.onRetry,
  });
  final String identity;
  final int width, height;
  final int decodeLimit;
  final Widget? placeholder;
  final DateTime validUntil;
  final bool Function() current;
  final Future<Uint8List> Function() read;
  final DateTime? Function(Uint8List bytes)? contentUntil;
  final MapMediaReadTrace? Function(Uint8List bytes)? contentTrace;
  final VoidCallback? onRetry;
  @override
  State<_LeasePicture> createState() => _LeasePictureState();
}

class _LeasePictureState extends State<_LeasePicture> {
  Uint8List? _data;
  ImageProvider? _image;
  Timer? _expiry;
  DateTime? _contentUntil;
  MapMediaReadTrace? _trace;
  int _request = 0;
  bool _loading = false, _failed = false;
  bool get _current =>
      mounted &&
      widget.current() &&
      DateTime.now().toUtc().isBefore(widget.validUntil) &&
      (_contentUntil == null ||
          DateTime.now().toUtc().isBefore(_contentUntil!));
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
    final until =
        _contentUntil != null && _contentUntil!.isBefore(widget.validUntil)
        ? _contentUntil!
        : widget.validUntil;
    final remaining = until.difference(DateTime.now().toUtc());
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
    _contentUntil = null;
    _trace = null;
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
      _contentUntil = widget.contentUntil?.call(data);
      _trace = widget.contentTrace?.call(data);
      if (!_current) return;
      _arm();
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

  void _retry() {
    if (!_current) return;
    widget.onRetry?.call();
    _clear();
    setState(() => _failed = false);
    unawaited(_load());
  }

  Widget _failure() => Center(
    child: Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        const Text('图片加载失败'),
        TextButton(onPressed: _retry, child: const Text('重试')),
      ],
    ),
  );

  @override
  Widget build(BuildContext context) => !_current
      ? (widget.placeholder ?? const Center(child: Text('媒体暂不可用')))
      : _image != null
      ? Image(
          image: _image!,
          fit: BoxFit.contain,
          frameBuilder: (_, child, frame, synchronous) {
            if (frame != null && _current)
              _trace?.mark(MapMediaReadStage.imageVisible);
            return child;
          },
          errorBuilder: (_, _, _) => widget.placeholder ?? _failure(),
        )
      : Center(
          child:
              widget.placeholder ??
              (_failed
                  ? _failure()
                  : const SizedBox(
                      width: 20,
                      height: 20,
                      child: CircularProgressIndicator(
                        strokeWidth: 2,
                        semanticsLabel: '图片加载中',
                      ),
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
  Completer<void>? _abort;
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
    final abort = _abort;
    _abort = null;
    if (abort != null && !abort.isCompleted) abort.complete();
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
    final abort = _abort = Completer<void>();
    _loading = true;
    _error = null;
    setState(() {});
    final trace = MapMediaReadMetrics.begin(MapMediaReadKind.video);
    VideoPlayerController? player;
    String? path;
    try {
      // Playback is an explicit action. Obtain a fresh, current grant before
      // starting a bounded video transfer instead of spending a stale list lease.
      trace.requests++;
      final fresh = await widget.controller.readMoment(
        widget.detail.selector,
        current: () => _current && request == _request && !abort.isCompleted,
        abortTrigger: abort.future,
      );
      trace.mark(MapMediaReadStage.detailReady);
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
      path = await widget.controller.prepareVideo(
        fresh,
        media,
        current: () => _current && request == _request,
        trace: trace,
        abortTrigger: abort.future,
        onFreshDetail: (renewed) async {
          if (!_current || request != _request || abort.isCompleted) {
            return false;
          }
          if (renewed.sourceVersion != widget.detail.sourceVersion ||
              !renewed.media.any((item) => item.identity == media.identity) ||
              !widget.onFreshDetail(renewed)) {
            return false;
          }
          // The parent must adopt the checked grant and refresh this widget's
          // expiry/current closures before the transfer uses the new lease.
          await WidgetsBinding.instance.endOfFrame;
          return _current && request == _request && !abort.isCompleted;
        },
      );
      if (!_current || request != _request) return;
      player = local_video.createVideoMaterialPlayer(path);
      if (player == null) throw const FormatException('此平台暂不支持视频播放');
      await player.initialize();
      trace.mark(MapMediaReadStage.initialized);
      if (!_current || request != _request) return;
      _player = player;
      player = null;
      _path = path;
      path = null;
      await _player!.play();
      trace.mark(MapMediaReadStage.playRequested);
    } catch (_) {
      trace.mark(MapMediaReadStage.failed);
      if (_current && request == _request) _error = '视频暂不可播放，请重试。';
    } finally {
      if (identical(_abort, abort)) _abort = null;
      if (!abort.isCompleted) abort.complete();
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
    this.interactionClient,
    this.onCommentTarget,
    this.onCommentIntent,
  });
  final OwnedMapController controller;
  final OwnedMapPin? cluster;
  final List<OwnedMapPin> pins;
  final bool Function() current;
  final Listenable changes;
  final VoidCallback onClose;
  final void Function(String, bool Function()) onEdit;
  final http.Client? interactionClient;
  final void Function(
    MomentInteractionController? controller,
    OwnedMapMomentDetail? moment,
    MomentComment? reply,
  )?
  onCommentTarget;
  final VoidCallback? onCommentIntent;
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
  OwnedMapMomentDetail? _lastCommentDetail;
  MomentComment? _lastCommentReply;
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
    _notifyGroupTarget(explicitList: true);
    _desiredCount = math.max(1, _pins.length);
    widget.changes.addListener(_changed);
    if (widget.cluster != null) {
      unawaited(_load());
    } else {
      _armLease();
    }
  }

  void _forwardCommentTarget(
    MomentInteractionController? controller,
    OwnedMapMomentDetail? moment,
    MomentComment? reply,
  ) {
    _lastCommentDetail = moment;
    _lastCommentReply = reply;
    widget.onCommentTarget?.call(controller, moment, reply);
  }

  void _notifyGroupTarget({bool explicitList = false}) {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || explicitList && _selected != null) return;
      if (explicitList && _current) {
        _forwardCommentTarget(null, null, null);
      } else {
        widget.onCommentTarget?.call(
          null,
          _lastCommentDetail,
          _lastCommentReply,
        );
      }
    });
    WidgetsBinding.instance.ensureVisualUpdate();
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
      _notifyGroupTarget();
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
        _notifyGroupTarget(explicitList: true);
      });
      if (selected.isCluster) {
        return NowMapContentGroupPanel(
          controller: widget.controller,
          cluster: selected,
          current: () => _current && widget.controller.sourceCurrent(selected),
          changes: widget.changes,
          onClose: back,
          onEdit: widget.onEdit,
          interactionClient: widget.interactionClient,
          onCommentTarget: _forwardCommentTarget,
          onCommentIntent: widget.onCommentIntent,
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
        interactionClient: widget.interactionClient,
        onCommentTarget: _forwardCommentTarget,
        onCommentIntent: widget.onCommentIntent,
      );
    }
    // A Moment may have several body/media location projections. The chooser
    // selects original content, so list that Moment once, retaining one exact
    // current projection as its read source. Never merge distinct Moments by
    // text, thread, location, or author, and never borrow an expired grant.
    final choices = <String, OwnedMapPin>{};
    if (_current) {
      for (final pin in _pins.where(widget.controller.sourceCurrent)) {
        final key = pin.isMomentAnchor && pin.momentID.isNotEmpty
            ? 'moment:${pin.momentID}'
            : pin.mapID;
        choices.putIfAbsent(key, () => pin);
      }
    }
    final visible = choices.values.toList(growable: false);
    final list = Material(
      color: Theme.of(context).colorScheme.surface,
      child: Column(
        children: [
          ListTile(
            title: Text(
              !_current
                  ? '地图内容'
                  : widget.cluster == null
                  ? '选择查看'
                  : '${widget.cluster!.momentCount}条动态',
            ),
            subtitle: !_current || widget.cluster == null
                ? null
                : Text('${widget.cluster!.anchorCount}个内容位置'),
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
                    // The retained Now host already applies system insets.
                    padding: EdgeInsets.zero,
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
                          pin.name.isEmpty ? '动态' : pin.name,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                        ),
                        subtitle: Text(
                          pin.isCluster
                              ? '${pin.anchorCount}个内容位置'
                              : pin.isMomentAnchor
                              ? [
                                  pin.threadID.isEmpty
                                      ? '动态'
                                      : '串文 · 第${pin.threadPosition + 1}段',
                                  if (pin.contentTime != null)
                                    pin.contentTimeLabel,
                                ].join(' · ')
                              : '地点 · ${pin.momentCount}条动态',
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
    // Two overlapping objects need a short chooser, not a mostly empty reader.
    // Selecting an object above still uses the full retained detail viewport.
    return LayoutBuilder(
      builder: (context, limits) {
        final available = limits.hasBoundedHeight ? limits.maxHeight : 560.0;
        final height = math.min(
          available,
          64.0 +
              visible.length * 88.0 +
              (_loading ? 4.0 : 0.0) +
              (_error != null || _cursor != null ? 48.0 : 0.0),
        );
        return Align(
          alignment: Alignment.bottomCenter,
          child: SizedBox(height: height, child: list),
        );
      },
    );
  }
}
