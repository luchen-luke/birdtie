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
import 'public_moment_media.dart';
import 'moment_thread_api.dart';
import 'moment_thread_panel.dart';
import 'moment_locations_api.dart';
import 'now_moment_editor_host.dart';

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
    this.onOpenAuthor,
    this.embedded = false,
    this.apiBaseUrl,
    this.client,
    this.commentID,
    this.showManagement = true,
  });
  final String momentID;
  final String? Function() authorizationHeader;
  final Listenable? identityChanges;
  final String? Function()? ownerID;
  final String? Function()? workspaceID;
  final ValueChanged<String>? onOpenPlace;
  final ValueChanged<String>? onOpenAuthor;
  final bool embedded;
  final String? apiBaseUrl;
  final http.Client? client;
  final String? commentID;
  final bool showManagement;
  @override
  State<PublicMomentPage> createState() => _PublicMomentPageState();
}

class _PublicMomentPageState extends State<PublicMomentPage> {
  final _shareSource = HumanCausalSourceSerial();
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
    _observeShareSource();
    _identity = _current;
    widget.identityChanges?.addListener(_changed);
    unawaited(_load());
  }

  void _changed() {
    _observeShareSource();
    if (_identity == _current) return;
    _identity = _current;
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
              epoch == _epoch &&
              identity == _current &&
              id == widget.momentID &&
              _moment?.revision == value.revision &&
              _moment?.current == true,
        );
        setState(() => _moment = value);
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
                _moment = null;
                _canManagePhoto = false;
                _canManageMoment = false;
                _error = '当前动态资料已到期，请重新核实。';
              });
          });
        }
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
    final body = _body(context);
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
                      _moment?.current == true &&
                      _identity == _current,
                  identityChanges: _shareSource,
                  apiBaseUrl: _api.baseUrl,
                  client: _api.borrowedClient,
                  onOpenPlace: widget.onOpenPlace,
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
                      sourceCurrent: () => mounted,
                      type: 'moment',
                      id: widget.momentID,
                      apiBaseUrl: widget.apiBaseUrl,
                      client: widget.client,
                    ),
                    icon: const Icon(Icons.chat_bubble_outline),
                    label: const Text('发给好友'),
                  ),
                MomentInteractionPanel(
                  key: ValueKey((
                    'moment-interactions',
                    widget.momentID,
                    widget.commentID,
                    generation,
                    _epoch,
                  )),
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
