import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'connections.dart';
import 'human_causal_operation_controller.dart';
import 'public_moment_api.dart';
import 'public_moment_photo.dart';
import 'public_moment_photo_controller.dart';
import 'public_moment_photo_editor.dart';
import 'human_causal_control.dart';
import 'supplier_profile_api.dart';
import 'now_ui_tokens.dart';
import '../app/birdtie_app_bar.dart';
import '../content/private_moment_controller.dart';
import 'public_moment_control_page.dart';

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
  String? _error;
  int _epoch = 0;
  Timer? _leaseTimer, _photoCapabilityTimer;
  bool _canManagePhoto = false;
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
    setState(() {
      _moment = null;
      _error = null;
      _canManagePhoto = false;
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
      setState(() => _canManagePhoto = state.canSave || state.canDelete);
      _photoCapabilityTimer = Timer(
        state.expiresAt.difference(DateTime.now().toUtc()),
        () {
          if (current()) setState(() => _canManagePhoto = false);
        },
      );
    } catch (_) {
      if (current()) setState(() => _canManagePhoto = false);
    }
  }

  Future<void> _managePhoto(int expectedGeneration) async {
    if (!mounted ||
        expectedGeneration != _shareSource.generation ||
        !_canManagePhoto ||
        widget.workspaceID?.call() != null)
      return;
    final generation = _shareSource.generation,
        id = widget.momentID,
        client = _api.borrowedClient,
        base = _api.baseUrl;
    final c = PublicMomentPhotoController(
      momentID: id,
      api: _photoAPI(),
      ownerID: widget.ownerID,
      workspaceID: widget.workspaceID,
      identityChanges: _shareSource,
      sourceKey: () => _shareSource.generation,
      sourceCurrent: () =>
          mounted &&
          generation == _shareSource.generation &&
          widget.momentID == id &&
          client == _api.borrowedClient &&
          base == _api.baseUrl,
    );
    final route = MaterialPageRoute<void>(
      builder: (_) => PublicMomentPhotoEditor(
        controller: c,
        momentTitle: _moment?.title ?? '',
        momentTitleRevision: _moment?.revision,
      ),
    );
    unawaited(route.completed.whenComplete(c.dispose));
    try {
      await Navigator.of(context, rootNavigator: true).push(route);
    } finally {
      c.dispose();
      if (mounted && generation == _shareSource.generation) unawaited(_load());
    }
  }

  Future<void> _manageMoment(int generation) async {
    if (!mounted ||
        generation != _shareSource.generation ||
        !_canManagePhoto ||
        widget.workspaceID?.call() != null)
      return;
    final id = widget.momentID,
        client = _api.borrowedClient,
        base = _api.baseUrl;
    bool current() =>
        mounted &&
        generation == _shareSource.generation &&
        widget.momentID == id &&
        client == _api.borrowedClient &&
        base == _api.baseUrl &&
        widget.workspaceID?.call() == null;
    final c = PrivateMomentController(
      authorizationHeader: () =>
          current() ? widget.authorizationHeader() : null,
      ownerID: () => current() ? widget.ownerID?.call() : null,
      identityChanges: _shareSource,
      client: client,
      apiBaseUrl: base,
    );
    try {
      final result = await Navigator.of(context).push<String>(
        MaterialPageRoute(
          builder: (_) => PublicMomentControlPage(
            moments: c,
            momentID: id,
            current: current,
            identityChanges: _shareSource,
          ),
        ),
      );
      if (current() && result != null) await _load();
    } finally {
      c.dispose();
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
                Text(_moment!.title, style: tokens.pageTitle),
                const SizedBox(height: 12),
                PublicMomentPhoto(
                  metadata: _moment!.photo,
                  api: _photoAPI(),
                  current: () =>
                      mounted &&
                      _moment?.current == true &&
                      _identity == _current,
                  identityChanges: _shareSource,
                ),
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
                const SizedBox(height: 16),
                Text(_moment!.body, style: tokens.body),
                const SizedBox(height: 16),
                Text('公开地点：${_moment!.placeName}', style: tokens.body),
                if (widget.onOpenPlace != null)
                  TextButton.icon(
                    style: actionStyle,
                    onPressed: () => widget.onOpenPlace!(_moment!.placeID),
                    icon: const Icon(Icons.place_outlined),
                    label: const Text('查看地点'),
                  ),
                const SizedBox(height: 12),
                Text('这是用户的公开分享，不代表已核验到访或出席。', style: tokens.metadata),
                if (_canManagePhoto)
                  TextButton.icon(
                    style: actionStyle,
                    onPressed: () => _manageMoment(generation),
                    icon: const Icon(Icons.edit_outlined),
                    label: const Text('修改或删除这条动态'),
                  ),
                if (_canManagePhoto)
                  TextButton.icon(
                    onPressed: () => _managePhoto(generation),
                    icon: const Icon(Icons.photo_outlined),
                    label: const Text('管理本人公开图片'),
                  ),
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
              ],
            ),
          );
  }
}
