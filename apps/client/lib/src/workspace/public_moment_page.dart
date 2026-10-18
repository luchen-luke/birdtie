import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'connections.dart';
import 'human_causal_operation_controller.dart';
import 'public_moment_api.dart';
import 'public_moment_photo.dart';
import 'human_causal_control.dart';
import 'supplier_profile_api.dart';
import 'now_ui_tokens.dart';
import '../app/birdtie_app_bar.dart';
import 'moment_interaction.dart';
import 'place_detail_sheet.dart';
import 'public_moment_media.dart';
import 'moment_thread_api.dart';
import 'moment_thread_panel.dart';
import 'moment_locations_api.dart';
import 'now_moment_editor_host.dart';
import '../city/map_navigation_target.dart';
import '../city/public_city_controller.dart';

/// One current, explicitly public human publication. It is not attendance evidence.
class PublicMomentPage extends StatefulWidget {
  const PublicMomentPage({
    super.key,
    required this.momentID,
    required this.authorizationHeader,
    this.identityChanges,
    this.ownerID,
    this.workspaceID,
    this.onOpenPlace,
    this.onNavigateMap,
    this.onOpenAuthor,
    this.onOpenActivity,
    this.embedded = false,
    this.apiBaseUrl,
    this.client,
    this.commentID,
    this.showManagement = true,
    this.sourceCurrent,
    this.useNowComposer = false,
    this.onCommentTarget,
    this.onCommentIntent,
    this.nowDepth = 0,
    this.composerCurrent,
  });
  final String momentID;
  final String? Function() authorizationHeader;
  final Listenable? identityChanges;
  final String? Function()? ownerID;
  final String? Function()? workspaceID;
  final ValueChanged<String>? onOpenPlace;
  final ValueChanged<MapNavigationTarget>? onNavigateMap;
  final ValueChanged<String>? onOpenAuthor;
  final ValueChanged<PublicActivity>? onOpenActivity;
  final bool embedded;
  final String? apiBaseUrl;
  final http.Client? client;
  final String? commentID;
  final bool showManagement;
  final bool Function()? sourceCurrent;
  final bool useNowComposer;
  final int nowDepth;
  final bool Function()? composerCurrent;
  final void Function(
    MomentInteractionController? controller,
    String? momentID,
    String? title,
    MomentComment? reply,
  )?
  onCommentTarget;
  final VoidCallback? onCommentIntent;
  @override
  State<PublicMomentPage> createState() => _PublicMomentPageState();
}

class _PublicMomentPageState extends State<PublicMomentPage> {
  final _shareSource = HumanCausalSourceSerial();
  MomentInteractionController? _nowInteraction;
  MomentComment? _nowReply;
  String? _nowTitle;
  bool _nowSourceRetired = false, _nowRevisionBlocked = false;
  int _nowNotice = 0;
  late ({String? id, String? title, MomentComment? reply}) _nowTarget;
  final _nowFrames = <({Object key, Widget content, bool place})>[];
  bool get _composerActive =>
      _nowFrames.isEmpty && widget.composerCurrent?.call() != false;
  bool get _externalCurrent =>
      !_nowSourceRetired && widget.sourceCurrent?.call() != false;
  bool get _readable =>
      mounted &&
      _externalCurrent &&
      _identity == _current &&
      _moment?.current == true;

  void _emitNowTarget({bool place = false, bool preserveActive = false}) {
    if (!widget.useNowComposer) return;
    final notice = ++_nowNotice;
    if (place) {
      _nowTarget = (id: null, title: null, reply: null);
    } else if (!preserveActive) {
      _nowTarget = (
        id: widget.momentID,
        title: _nowTitle ?? '当前动态',
        reply: _nowReply,
      );
    }
    final target = _nowTarget;
    final interaction = _nowInteraction;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || notice != _nowNotice) return;
      final live =
          interaction != null &&
          identical(interaction, _nowInteraction) &&
          interaction.momentID == target.id &&
          interaction.current();
      widget.onCommentTarget?.call(
        live ? interaction : null,
        target.id,
        target.title,
        target.reply,
      );
    });
  }

  void _retireNowInteraction({bool emit = true}) {
    final previous = _nowInteraction;
    _nowInteraction = null;
    previous?.removeListener(_nowInteractionChanged);
    previous?.dispose();
    if (emit) _emitNowTarget(preserveActive: true);
  }

  void _nowInteractionChanged() {
    final c = _nowInteraction;
    if (c == null || !mounted) return;
    if (c.page != null && c.page!.revision != _moment?.revision) {
      _nowRevisionBlocked = true;
      _emitNowTarget();
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted || !identical(c, _nowInteraction)) return;
        setState(() {
          _retireNowInteraction();
          _error = '动态版本已变化，请重新核实后评论。';
        });
      });
      return;
    }
    _emitNowTarget();
  }

  void _startNowInteraction() {
    if (!widget.useNowComposer ||
        !_readable ||
        _nowFrames.isNotEmpty ||
        widget.workspaceID?.call() != null ||
        widget.ownerID?.call() == null)
      return;
    _retireNowInteraction(emit: false);
    final value = _moment!, epoch = _epoch, identity = _current;
    _nowTitle = value.title.isEmpty ? value.body : value.title;
    _nowRevisionBlocked = false;
    final c = MomentInteractionController(
      momentID: value.id,
      authorizationHeader: widget.authorizationHeader,
      ownerID: () => widget.ownerID?.call(),
      apiBaseUrl: _api.baseUrl,
      client: _api.borrowedClient,
      identityChanges: _shareSource,
      commentID: widget.commentID,
      current: () =>
          _readable &&
          epoch == _epoch &&
          identity == _current &&
          identical(_moment, value) &&
          _composerActive &&
          !_nowRevisionBlocked &&
          widget.workspaceID?.call() == null,
    );
    _nowInteraction = c;
    c.addListener(_nowInteractionChanged);
    _emitNowTarget();
    unawaited(c.load());
  }

  bool _canPushNow() {
    if (!_readable) return false;
    if (widget.nowDepth + _nowFrames.length >= 8) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('请先返回上一层，再继续打开内容。')));
      return false;
    }
    return true;
  }

  void _pushNowMoment(String id) {
    if (!_canPushNow()) return;
    _retireNowInteraction(emit: false);
    ++_nowNotice;
    final key = Object(), value = _moment;
    bool current() =>
        _readable &&
        identical(_moment, value) &&
        _nowFrames.any((f) => identical(f.key, key));
    final child = PublicMomentPage(
      key: ValueKey(key),
      momentID: id,
      authorizationHeader: widget.authorizationHeader,
      ownerID: widget.ownerID,
      workspaceID: widget.workspaceID,
      identityChanges: _shareSource,
      apiBaseUrl: _api.baseUrl,
      client: _api.borrowedClient,
      embedded: true,
      useNowComposer: true,
      showManagement: false,
      sourceCurrent: current,
      onOpenActivity: widget.onOpenActivity,
      onCommentIntent: widget.onCommentIntent,
      composerCurrent: () =>
          _readable &&
          identical(_nowFrames.lastOrNull?.key, key) &&
          widget.composerCurrent?.call() != false,
      nowDepth: widget.nowDepth + _nowFrames.length + 1,
      onCommentTarget: (c, momentID, title, reply) {
        if (current() && identical(_nowFrames.last.key, key)) {
          ++_nowNotice;
          _nowTarget = (id: momentID, title: title, reply: reply);
          widget.onCommentTarget?.call(c, momentID, title, reply);
        }
      },
    );
    setState(() => _nowFrames.add((key: key, content: child, place: false)));
    _shareSource.notifyReadBindingChanged();
    // The user explicitly selected this original Moment; loading never borrows
    // the parent Moment's reply or draft slot.
    _nowTarget = (id: id, title: '正在读取动态', reply: null);
    _emitNowTarget(preserveActive: true);
  }

  void _pushNowPlace(String id) {
    if (!_canPushNow()) return;
    _retireNowInteraction(emit: false);
    final key = Object(), value = _moment;
    bool current() =>
        _readable &&
        identical(_moment, value) &&
        _nowFrames.any((f) => identical(f.key, key));
    final child = PlaceDetailSheet(
      key: ValueKey(key),
      placeID: id,
      publicContentFirst: true,
      authorizationHeader: () =>
          current() ? widget.authorizationHeader() : null,
      ownerID: () => current() ? widget.ownerID?.call() : null,
      organizationWorkspaceID: widget.workspaceID,
      workspaceChanges: _shareSource,
      apiBaseUrl: _api.baseUrl,
      client: _api.borrowedClient,
      onOpenActivity: (activity) {
        if (current()) widget.onOpenActivity?.call(activity);
      },
      onOpenMoment: (mid) {
        if (current()) _pushNowMoment(mid);
      },
    );
    setState(() => _nowFrames.add((key: key, content: child, place: true)));
    _shareSource.notifyReadBindingChanged();
    _emitNowTarget(place: true);
  }

  void _popNowFrame() {
    if (_nowFrames.isEmpty) return;
    ++_nowNotice;
    setState(() => _nowFrames.removeLast());
    _shareSource.notifyReadBindingChanged();
    if (_nowFrames.isEmpty)
      _startNowInteraction();
    else if (_nowFrames.last.place)
      _emitNowTarget(place: true);
  }

  void _observeShareSource() {
    _shareSource.observe((
      widget.momentID,
      widget.client,
      widget.apiBaseUrl,
      widget.ownerID?.call(),
      widget.workspaceID?.call(),
    ));
    _shareSource.notifyReadBindingChanged();
  }

  late PublicMomentApi _api = PublicMomentApi(
    authorizationHeader: () => widget.authorizationHeader(),
    apiBaseUrl: widget.apiBaseUrl,
    client: widget.client,
  );
  PublicMoment? _moment;
  MomentThreadAPI? _threadAPI;
  PublicMomentMediaSetAPI? _mediaAPI;
  bool _wholeThreadWithdrawn = false;
  String? _error;
  int _epoch = 0;
  Timer? _leaseTimer, _photoCapabilityTimer;
  bool _canManagePhoto = false;
  bool _canManageMoment = false;
  late (String?, String?, String?) _identity;
  (String?, String?, String?) get _current => (
    widget.authorizationHeader(),
    widget.workspaceID?.call(),
    widget.ownerID?.call(),
  );
  @override
  void initState() {
    super.initState();
    _nowTarget = (id: widget.momentID, title: '当前动态', reply: null);
    _observeShareSource();
    _identity = _current;
    widget.identityChanges?.addListener(_changed);
    unawaited(_load());
  }

  void _changed() {
    _observeShareSource();
    if (!_externalCurrent) {
      _nowSourceRetired = true;
      ++_epoch;
      _retireNowInteraction();
      _shareSource.notifyReadBindingChanged();
      setState(() {
        _moment = null;
        _nowFrames.clear();
        _error = '原内容来源已变化，请返回当前旅程重新打开。';
      });
      return;
    }
    if (_identity == _current) {
      if (widget.useNowComposer &&
          _composerActive &&
          _readable &&
          !_nowRevisionBlocked &&
          (_nowInteraction == null || !_nowInteraction!.current()))
        _startNowInteraction();
      return;
    }
    _identity = _current;
    _retireNowInteraction();
    _nowFrames.clear();
    ++_epoch;
    setState(() {
      _moment = null;
      _error = null;
      _canManagePhoto = false;
      _canManageMoment = false;
    });
    unawaited(_load());
  }

  @override
  void didUpdateWidget(PublicMomentPage old) {
    super.didUpdateWidget(old);
    _observeShareSource();
    final transportChanged =
        old.client != widget.client || old.apiBaseUrl != widget.apiBaseUrl;
    if (transportChanged) {
      _api.dispose();
      _api = PublicMomentApi(
        authorizationHeader: () => widget.authorizationHeader(),
        apiBaseUrl: widget.apiBaseUrl,
        client: widget.client,
      );
    }
    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges?.removeListener(_changed);
      widget.identityChanges?.addListener(_changed);
    }
    if (transportChanged ||
        old.momentID != widget.momentID ||
        (old.onOpenAuthor != null) != (widget.onOpenAuthor != null) ||
        _identity != _current) {
      _identity = _current;
      ++_epoch;
      _moment = null;
      unawaited(_load());
    }
  }

  Future<void> _load() async {
    _retireNowInteraction();
    _nowFrames.clear();
    if (!_externalCurrent) {
      _nowSourceRetired = true;
      setState(() => _error = '原内容来源已变化，请返回当前旅程重新打开。');
      return;
    }
    final epoch = ++_epoch, identity = _current, id = widget.momentID;
    _leaseTimer?.cancel();
    _photoCapabilityTimer?.cancel();
    _threadAPI = null;
    _mediaAPI?.dispose();
    _mediaAPI = null;
    _wholeThreadWithdrawn = false;
    setState(() {
      _moment = null;
      _error = null;
      _canManagePhoto = false;
      _canManageMoment = false;
    });
    try {
      final value = await _api.read(
        id,
        includePublicAuthor: widget.onOpenAuthor != null,
      );
      if (mounted &&
          epoch == _epoch &&
          _externalCurrent &&
          identity == _current &&
          id == widget.momentID) {
        // Retain the exact transport and identity binding for this read. Normal
        // capability/ancestor rebuilds must not retire a prepared thread action.
        // A new load, identity, target or transport invalidates this API epoch.
        _threadAPI = MomentThreadAPI(
          client: _api.borrowedClient,
          baseUrl: _api.baseUrl,
          authorizationHeader: () => widget.authorizationHeader(),
          current: () =>
              mounted &&
              _externalCurrent &&
              epoch == _epoch &&
              identity == _current &&
              id == widget.momentID &&
              _moment?.current == true,
        );
        // Media playback belongs to the same captured read, not to a build.
        // Recreating this API on a capability/ancestor rebuild would retire the
        // active video handle and restart its poster request.
        _mediaAPI = PublicMomentMediaSetAPI(
          client: _api.borrowedClient,
          apiBaseUrl: _api.baseUrl,
          authorizationHeader: () => widget.authorizationHeader(),
          current: () =>
              mounted &&
              _externalCurrent &&
              epoch == _epoch &&
              identity == _current &&
              id == widget.momentID &&
              _moment?.revision == value.revision &&
              _moment?.current == true,
        );
        setState(() => _moment = value);
        _nowReply = null;
        _startNowInteraction();
        if (value.validUntil case final expiry?) {
          if (!value.current) {
            setState(() {
              _moment = null;
              _error = '当前动态资料已到期，请重新核实。';
            });
            return;
          }
          _leaseTimer = Timer(expiry.difference(DateTime.now().toUtc()), () {
            if (mounted && epoch == _epoch)
              setState(() {
                _retireNowInteraction();
                _moment = null;
                _shareSource.notifyReadBindingChanged();
                _canManagePhoto = false;
                _canManageMoment = false;
                _error = '当前动态资料已到期，请重新核实。';
              });
          });
        }
        if (widget.showManagement)
          unawaited(_readPhotoCapability(epoch, identity, id));
      }
    } catch (_) {
      if (mounted && epoch == _epoch && identity == _current) {
        setState(() => _error = '这条动态已撤回、不可访问，或暂时无法读取。');
      }
    }
  }

  @override
  void dispose() {
    ++_nowNotice;
    _retireNowInteraction(emit: false);
    _leaseTimer?.cancel();
    _photoCapabilityTimer?.cancel();
    _mediaAPI?.dispose();
    _shareSource.dispose();
    ++_epoch;
    widget.identityChanges?.removeListener(_changed);
    _api.dispose();
    super.dispose();
  }

  PublicMomentPhotoAPI _photoAPI({bool Function()? current}) =>
      PublicMomentPhotoAPI(
        client: _api.borrowedClient,
        baseUrl: _api.baseUrl,
        authorizationHeader: widget.authorizationHeader,
        current: current,
      );
  Future<void> _readPhotoCapability(
    int epoch,
    (String?, String?, String?) identity,
    String id,
  ) async {
    if (!humanControlID(widget.ownerID?.call()) ||
        identity.$1 == null ||
        identity.$2 != null)
      return;
    bool current() =>
        mounted &&
        epoch == _epoch &&
        identity == _current &&
        id == widget.momentID;
    try {
      final state = await _photoAPI(current: current).state(id);
      if (!current()) return;
      setState(() {
        // A successful owner-only state read proves ordinary Moment ownership
        // even where the Place-only photograph contract has no save capability.
        _canManageMoment = true;
        _canManagePhoto = state.canSave || state.canDelete;
      });
      _photoCapabilityTimer = Timer(
        state.expiresAt.difference(DateTime.now().toUtc()),
        () {
          if (current())
            setState(() {
              _canManagePhoto = false;
              _canManageMoment = false;
            });
        },
      );
    } catch (_) {
      if (current())
        setState(() {
          _canManagePhoto = false;
          _canManageMoment = false;
        });
    }
  }

  Future<void> _manageMoment(int generation) async {
    if (!mounted ||
        generation != _shareSource.generation ||
        !_canManageMoment ||
        !widget.showManagement ||
        widget.workspaceID?.call() != null ||
        _moment?.current != true)
      return;
    final capability = NowMomentEditorScope.maybeOf(context);
    final owner = widget.ownerID?.call(), token = widget.authorizationHeader();
    if (capability == null || owner == null || token == null) return;
    final id = widget.momentID,
        client = _api.borrowedClient,
        base = _api.baseUrl,
        epoch = _epoch,
        identity = _current,
        revision = _moment!.revision;
    bool current() =>
        mounted &&
        generation == _shareSource.generation &&
        epoch == _epoch &&
        identity == _current &&
        _canManageMoment &&
        _moment?.current == true &&
        _moment?.revision == revision &&
        widget.momentID == id &&
        client == _api.borrowedClient &&
        base == _api.baseUrl &&
        widget.workspaceID?.call() == null;
    final opened = await capability.open(
      NowMomentEditorRequest(
        momentID: id,
        ownerID: owner,
        authorizationHeader: token,
        apiBaseUrl: base,
        sourceCurrent: current,
      ),
    );
    if (!mounted) return;
    if (!opened && current()) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('当前身份、来源或草稿状态已变化，请回到 Now 重新打开。')),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final root = _body(context);
    final frames = _readable
        ? _nowFrames
        : const <({Object key, Widget content, bool place})>[];
    final body = widget.useNowComposer
        ? Column(
            children: [
              if (frames.isNotEmpty)
                Align(
                  alignment: Alignment.centerLeft,
                  child: TextButton.icon(
                    onPressed: _popNowFrame,
                    icon: const Icon(Icons.arrow_back),
                    label: Text(frames.last.place ? '返回原动态' : '返回内容列表'),
                  ),
                )
              else
                const SizedBox.shrink(),
              Expanded(
                child: IndexedStack(
                  index: frames.length,
                  children: [root, for (final frame in frames) frame.content],
                ),
              ),
            ],
          )
        : root;
    if (widget.embedded) return body;
    return Scaffold(
      appBar: birdtieAppBar(
        context,
        title: Text('公开动态', style: NowUiTokens.of(context).pageTitle),
      ),
      body: body,
    );
  }

  void _threadWithdrawn(MomentThreadAPI source) {
    if (!mounted || !identical(source, _threadAPI) || !source.current()) return;
    ++_epoch;
    _retireNowInteraction();
    _nowFrames.clear();
    _leaseTimer?.cancel();
    _photoCapabilityTimer?.cancel();
    _threadAPI = null;
    _mediaAPI?.dispose();
    _mediaAPI = null;
    setState(() {
      _moment = null;
      _error = null;
      _canManagePhoto = false;
      _canManageMoment = false;
      _wholeThreadWithdrawn = true;
    });
    _shareSource.observe((widget.momentID, _epoch, 'withdrawn'));
  }

  void _openAuthor() {
    if (_identity != _current) {
      _changed();
      return;
    }
    final author = _moment?.authorPublicProfile;
    if (author != null && widget.onOpenAuthor != null) {
      widget.onOpenAuthor!(author.accountID);
    }
  }

  Widget _body(BuildContext context) {
    final tokens = NowUiTokens.of(context);
    if (!_externalCurrent)
      return const Center(child: Text('原内容来源已变化，请返回当前旅程重新打开。'));
    final generation = _shareSource.generation;
    final threadAPI = _threadAPI;
    final editor = NowMomentEditorScope.maybeOf(context);
    if (_wholeThreadWithdrawn) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Semantics(
            liveRegion: true,
            child: Text('整串已撤回，不再公开显示。', style: tokens.body),
          ),
        ),
      );
    }
    final actionStyle = TextButton.styleFrom(
      minimumSize: const Size(48, 48),
      textStyle: tokens.action,
    );
    return _error != null
        ? Center(
            child: Padding(
              padding: const EdgeInsets.all(24),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(_error!, style: tokens.body),
                  TextButton(
                    style: actionStyle,
                    onPressed: _load,
                    child: const Text('重新核实'),
                  ),
                  if (widget.workspaceID?.call() == null &&
                      widget.authorizationHeader() != null)
                    MomentInteractionPanel(
                      key: ValueKey((
                        'moment-interaction-recovery',
                        widget.momentID,
                        generation,
                      )),
                      momentID: widget.momentID,
                      authorizationHeader: () => widget.authorizationHeader(),
                      ownerID: () => widget.ownerID?.call(),
                      apiBaseUrl: _api.baseUrl,
                      client: _api.borrowedClient,
                      identityChanges: _shareSource,
                      recoveryOnly: true,
                      showComposer: !widget.useNowComposer,
                      current: () =>
                          mounted &&
                          generation == _shareSource.generation &&
                          _identity == _current &&
                          widget.workspaceID?.call() == null,
                    ),
                ],
              ),
            ),
          )
        : _moment == null
        ? const Center(child: CircularProgressIndicator())
        : RefreshIndicator(
            onRefresh: _load,
            child: ListView(
              key: PageStorageKey('public-moment-scroll:${widget.momentID}'),
              primary: false,
              padding: const EdgeInsets.all(16),
              children: [
                PublicMomentPhoto(
                  metadata: _moment!.photo,
                  api: _photoAPI(),
                  current: () =>
                      mounted &&
                      _externalCurrent &&
                      _moment?.current == true &&
                      _identity == _current,
                  identityChanges: _shareSource,
                ),
                _PublicMomentMediaSidecar(
                  key: ValueKey((widget.momentID, _moment!.revision, _epoch)),
                  momentID: widget.momentID,
                  revision: _moment!.revision,
                  api: _mediaAPI!,
                ),
                MomentThreadSidecar(
                  key: ValueKey(('moment-thread', widget.momentID, _epoch)),
                  momentID: widget.momentID,
                  api: threadAPI!,
                  onWithdrawn: () => _threadWithdrawn(threadAPI),
                  ownerID: widget.showManagement
                      ? widget.ownerID?.call()
                      : null,
                  onOpenMoment: (id) async {
                    if (id == widget.momentID || _moment?.current != true)
                      return;
                    if (widget.useNowComposer) {
                      _pushNowMoment(id);
                      return;
                    }
                    await Navigator.of(context).push(
                      MaterialPageRoute<void>(
                        builder: (_) => PublicMomentPage(
                          momentID: id,
                          authorizationHeader: widget.authorizationHeader,
                          ownerID: widget.ownerID,
                          workspaceID: widget.workspaceID,
                          identityChanges: widget.identityChanges,
                          apiBaseUrl: widget.apiBaseUrl,
                          client: widget.client,
                          onOpenPlace: widget.onOpenPlace,
                          onNavigateMap: widget.onNavigateMap,
                          onOpenAuthor: widget.onOpenAuthor,
                        ),
                      ),
                    );
                    // Another member's page may have withdrawn the whole thread.
                    // Retire this page's old publication before reading it again.
                    if (mounted && identical(_threadAPI, threadAPI)) {
                      await _load();
                    }
                  },
                ),
                if (_moment!.title.isNotEmpty) ...[
                  Text(_moment!.title, style: tokens.pageTitle),
                  const SizedBox(height: 12),
                ],
                if (_moment!.authorPublicProfile case final author?)
                  ListTile(
                    contentPadding: EdgeInsets.zero,
                    minTileHeight: 48,
                    leading: const CircleAvatar(
                      child: Icon(Icons.person_outline),
                    ),
                    title: Text(author.displayName, style: tokens.placeTitle),
                    subtitle: Text('查看作者主页', style: tokens.listSubtitle),
                    onTap: widget.onOpenAuthor != null ? _openAuthor : null,
                  ),
                Text(
                  '本人明确公开 · ${supplierDate(_moment!.publishedAt)}',
                  style: tokens.metadata,
                ),
                if (_moment!.body.isNotEmpty) ...[
                  const SizedBox(height: 16),
                  Text(_moment!.body, style: tokens.body),
                ],
                MomentLocationsSidecar(
                  key: ValueKey((
                    'moment-locations',
                    widget.momentID,
                    _moment!.revision,
                    _epoch,
                  )),
                  momentID: widget.momentID,
                  expectedRevision: _moment!.revision,
                  authorizationHeader: widget.authorizationHeader,
                  sourceCurrent: () =>
                      mounted &&
                      _externalCurrent &&
                      _moment?.current == true &&
                      _identity == _current,
                  identityChanges: _shareSource,
                  apiBaseUrl: _api.baseUrl,
                  client: _api.borrowedClient,
                  onOpenPlace: widget.useNowComposer
                      ? _pushNowPlace
                      : widget.onOpenPlace,
                  onNavigateMap: widget.onNavigateMap,
                ),
                const SizedBox(height: 12),
                Text('这是用户的公开分享，不代表已核验到访或出席。', style: tokens.metadata),
                if (_canManageMoment && widget.showManagement)
                  TextButton.icon(
                    style: actionStyle,
                    onPressed: editor == null
                        ? null
                        : () => _manageMoment(generation),
                    icon: const Icon(Icons.edit_outlined),
                    label: const Text('修改或删除这条动态'),
                  ),
                if (_canManageMoment && widget.showManagement)
                  TextButton.icon(
                    style: actionStyle,
                    onPressed: editor == null
                        ? null
                        : () => _manageMoment(generation),
                    icon: const Icon(Icons.photo_library_outlined),
                    label: const Text('管理本人图片或视频'),
                  ),
                if (_canManagePhoto &&
                    widget.showManagement &&
                    _moment!.placeID.isNotEmpty)
                  TextButton.icon(
                    onPressed: editor == null
                        ? null
                        : () => _manageMoment(generation),
                    icon: const Icon(Icons.photo_outlined),
                    label: const Text('管理本人公开图片'),
                  ),
                if (_canManageMoment && widget.showManagement && editor == null)
                  Text('请回到 Now 首页编辑这条动态。', style: tokens.metadata),
                if (widget.workspaceID?.call() == null)
                  TextButton.icon(
                    style: actionStyle,
                    onPressed: () => shareEntityToChat(
                      context,
                      authorizationHeader: () => widget.authorizationHeader(),
                      identityChanges: _shareSource,
                      ownerID: widget.ownerID == null
                          ? null
                          : () => widget.ownerID?.call(),
                      workspaceID: () => widget.workspaceID?.call(),
                      sourceKey: () => _shareSource.generation,
                      sourceCurrent: () => _readable,
                      type: 'moment',
                      id: widget.momentID,
                      apiBaseUrl: widget.apiBaseUrl,
                      client: widget.client,
                    ),
                    icon: const Icon(Icons.chat_bubble_outline),
                    label: const Text('发给好友'),
                  ),
                if (!widget.useNowComposer || _nowInteraction != null)
                  MomentInteractionPanel(
                    key: ValueKey((
                      'moment-interactions',
                      widget.momentID,
                      widget.commentID,
                      generation,
                      _epoch,
                    )),
                    controller: widget.useNowComposer ? _nowInteraction : null,
                    showComposer: !widget.useNowComposer,
                    onReply: widget.useNowComposer
                        ? (reply) {
                            if (_nowInteraction?.replyCurrent(reply) != true)
                              return;
                            widget.onCommentIntent?.call();
                            _nowReply = reply;
                            _emitNowTarget();
                          }
                        : null,
                    onComment: widget.useNowComposer
                        ? () {
                            if (_nowInteraction?.canComment != true) return;
                            widget.onCommentIntent?.call();
                            _nowReply = null;
                            _emitNowTarget();
                          }
                        : null,
                    momentID: widget.momentID,
                    commentID: widget.commentID,
                    authorizationHeader: () => widget.authorizationHeader(),
                    ownerID: () => widget.ownerID?.call(),
                    apiBaseUrl: _api.baseUrl,
                    client: _api.borrowedClient,
                    identityChanges: _shareSource,
                    current: () =>
                        mounted &&
                        generation == _shareSource.generation &&
                        _identity == _current &&
                        widget.workspaceID?.call() == null,
                  ),
              ],
            ),
          );
  }
}

/// One optional media projection. Its expiry removes the whole displayed set;
/// failure never hides the independently authorized text publication.
class _PublicMomentMediaSidecar extends StatefulWidget {
  const _PublicMomentMediaSidecar({
    super.key,
    required this.api,
    required this.momentID,
    required this.revision,
  });
  final PublicMomentMediaSetAPI api;
  final String momentID;
  final int revision;
  @override
  State<_PublicMomentMediaSidecar> createState() =>
      _PublicMomentMediaSidecarState();
}

class _PublicMomentMediaSidecarState extends State<_PublicMomentMediaSidecar> {
  PublicMomentMediaSet? _set;
  Timer? _expiry;
  int _serial = 0;
  bool _failed = false;
  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load() async {
    final serial = ++_serial;
    _expiry?.cancel();
    try {
      final value = await widget.api.publicSet(
        widget.momentID,
        expectedRevision: widget.revision,
      );
      if (!mounted || serial != _serial || !widget.api.current()) return;
      setState(() {
        _set = value;
        _failed = false;
      });
      _expiry = Timer(value.validUntil.difference(DateTime.now().toUtc()), () {
        if (mounted && serial == _serial)
          setState(() {
            _set = null;
            _failed = true;
          });
      });
    } catch (_) {
      if (mounted && serial == _serial)
        setState(() {
          _set = null;
          _failed = true;
        });
    }
  }

  @override
  Widget build(BuildContext context) {
    final set = _set;
    if (set != null &&
        set.current &&
        widget.api.current() &&
        set.status == 'READY')
      return PublicMomentMediaGallery(set: set, api: widget.api);
    if (_failed)
      return Align(
        alignment: Alignment.centerLeft,
        child: TextButton(
          onPressed: () => unawaited(_load()),
          child: const Text('重读当前图片'),
        ),
      );
    return const SizedBox.shrink();
  }

  @override
  void dispose() {
    ++_serial;
    _expiry?.cancel();
    widget.api.dispose();
    super.dispose();
  }
}
