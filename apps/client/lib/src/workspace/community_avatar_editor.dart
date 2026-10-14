import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'community_avatar.dart';
import 'entity_share_pending_store.dart' show newEntityShareOperationID;
import 'person_avatar_editor.dart'
    show pickPersonAvatarImage, prepareLocalPersonAvatar;

abstract interface class CommunityAvatarPendingStore {
  Future<CommunityAvatarPending?> read(
    String origin,
    String owner,
    String community,
  );
  Future<void> write(
    String origin,
    String owner,
    String community,
    CommunityAvatarPending p,
  );
  Future<void> remove(
    String origin,
    String owner,
    String community,
    CommunityAvatarPending p,
  );
}

class SecureCommunityAvatarPendingStore implements CommunityAvatarPendingStore {
  final FlutterSecureStorage _storage = const FlutterSecureStorage();
  static final Map<String, Future<void>> _tails = {};
  String _key(String o, String a, String c) =>
      'birdtie.community-avatar.pending.v1.${sha256.convert(utf8.encode('$o\n$a\n$c'))}';
  Future<T> _serial<T>(String key, Future<T> Function() work) async {
    final previous = _tails[key], done = Completer<void>();
    _tails[key] = done.future;
    try {
      if (previous != null) await previous;
      return await work();
    } finally {
      if (identical(_tails[key], done.future)) _tails.remove(key);
      done.complete();
    }
  }

  Future<CommunityAvatarPending?> _read(String key) async {
    final raw = await _storage.read(key: key);
    return raw == null
        ? null
        : CommunityAvatarPending.fromJson(jsonDecode(raw));
  }

  @override
  Future<CommunityAvatarPending?> read(String o, String a, String c) =>
      _serial(_key(o, a, c), () => _read(_key(o, a, c)));
  @override
  Future<void> write(String o, String a, String c, CommunityAvatarPending p) =>
      _serial(_key(o, a, c), () async {
        final key = _key(o, a, c), old = await _read(key);
        if (old != null &&
            (!old.same(p) || (old.stage == 'UNKNOWN' && p.stage == 'PREPARED')))
          throw const FormatException('上次头像结果尚未核实');
        await _storage.write(key: key, value: jsonEncode(p.toJson()));
      });
  @override
  Future<void> remove(String o, String a, String c, CommunityAvatarPending p) =>
      _serial(_key(o, a, c), () async {
        final key = _key(o, a, c), old = await _read(key);
        if (old != null && (!old.same(p) || old.stage != p.stage))
          throw const FormatException('头像恢复记录已变化');
        await _storage.delete(key: key);
      });
}

class MemoryCommunityAvatarPendingStore implements CommunityAvatarPendingStore {
  final Map<(String, String, String), CommunityAvatarPending> values = {};
  @override
  Future<CommunityAvatarPending?> read(String o, String a, String c) async =>
      values[(o, a, c)];
  @override
  Future<void> write(
    String o,
    String a,
    String c,
    CommunityAvatarPending p,
  ) async {
    final old = values[(o, a, c)];
    if (old != null &&
        (!old.same(p) || (old.stage == 'UNKNOWN' && p.stage == 'PREPARED')))
      throw const FormatException('上次头像结果尚未核实');
    values[(o, a, c)] = p;
  }

  @override
  Future<void> remove(
    String o,
    String a,
    String c,
    CommunityAvatarPending p,
  ) async {
    final old = values[(o, a, c)];
    if (old != null && (!old.same(p) || old.stage != p.stage))
      throw const FormatException('头像恢复记录已变化');
    values.remove((o, a, c));
  }
}

class CommunityAvatarEditor extends StatefulWidget {
  const CommunityAvatarEditor({
    super.key,
    required this.communityID,
    required this.communityName,
    required this.ownerID,
    required this.api,
    required this.identityChanges,
    required this.current,
    this.initialImage,
    this.picker,
    this.pendingStore,
    this.onChanged,
  });
  final String communityID, communityName, ownerID;
  final CommunityAvatarAPI api;
  final Listenable identityChanges;
  final bool Function() current;
  final Uint8List? initialImage;
  final Future<Uint8List?> Function()? picker;
  final CommunityAvatarPendingStore? pendingStore;
  final VoidCallback? onChanged;
  @override
  State<CommunityAvatarEditor> createState() => _CommunityAvatarEditorState();
}

class _CommunityAvatarEditorState extends State<CommunityAvatarEditor> {
  late CommunityAvatarPendingStore _store;
  CommunityAvatarState? _state;
  CommunityAvatarPending? _pending;
  Uint8List? _draft;
  bool _busy = false, _confirming = false, _journalFailed = false;
  String? _message;
  int _epoch = 0;
  Object? _seen;
  Object get _binding => (
    widget.api.authorizationHeader(),
    widget.api.baseUrl,
    widget.api.client,
    widget.ownerID,
    widget.communityID,
    widget.current(),
  );
  bool _current(int epoch, Object binding) =>
      mounted && epoch == _epoch && binding == _binding && widget.current();
  @override
  void initState() {
    super.initState();
    _store = widget.pendingStore ?? SecureCommunityAvatarPendingStore();
    _draft = widget.initialImage == null
        ? null
        : Uint8List.fromList(widget.initialImage!);
    _seen = _binding;
    widget.identityChanges.addListener(_observe);
    unawaited(_load());
  }

  void _observe() {
    if (_seen == _binding) return;
    _seen = _binding;
    ++_epoch;
    setState(() {
      _state = null;
      _draft = null;
      _pending = null;
      _busy = false;
      _confirming = false;
      _message = '当前身份或社群已变化，请重新打开。';
    });
  }

  @override
  void didUpdateWidget(CommunityAvatarEditor old) {
    super.didUpdateWidget(old);
    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges.removeListener(_observe);
      widget.identityChanges.addListener(_observe);
    }
    if (_seen != _binding) _observe();
  }

  @override
  void dispose() {
    ++_epoch;
    widget.identityChanges.removeListener(_observe);
    _draft = null;
    super.dispose();
  }

  String _error(Object e) =>
      e is CommunityAvatarFailure ? e.message : '头像操作未完成，请重试读取。';
  Future<void> _load() async {
    if (!widget.current() || _busy) return;
    final epoch = _epoch, binding = _binding;
    setState(() => _busy = true);
    try {
      final pending = await _store.read(
        widget.api.baseUrl,
        widget.ownerID,
        widget.communityID,
      );
      if (!_current(epoch, binding)) return;
      _pending = pending;
      if (pending != null) _draft = null;
      _journalFailed = false;
      final state = await widget.api.state(widget.communityID);
      if (!_current(epoch, binding)) return;
      setState(() {
        _state = state;
        _message = pending == null
            ? null
            : pending.stage == 'PREPARED'
            ? '上次尚未提交，可清除准备记录。'
            : '上次头像保存结果待核实。';
      });
    } catch (e) {
      if (_current(epoch, binding))
        setState(() {
          _journalFailed = _pending == null;
          _message = _error(e);
        });
    } finally {
      if (_current(epoch, binding)) setState(() => _busy = false);
    }
  }

  Future<void> _select() async {
    if (_busy || _pending != null || _journalFailed || !widget.current())
      return;
    final epoch = _epoch, binding = _binding;
    setState(() => _busy = true);
    try {
      final raw = await (widget.picker ?? pickPersonAvatarImage)();
      if (raw == null || !_current(epoch, binding)) return;
      final bytes = await prepareLocalPersonAvatar(raw);
      if (_current(epoch, binding))
        setState(() {
          _draft = bytes;
          _message = null;
        });
    } catch (e) {
      if (_current(epoch, binding)) setState(() => _message = _error(e));
    } finally {
      if (_current(epoch, binding)) setState(() => _busy = false);
    }
  }

  Future<void> _save({bool delete = false}) async {
    final state = _state;
    if (state == null ||
        _busy ||
        _pending != null ||
        _journalFailed ||
        !widget.current() ||
        (!delete && _draft == null))
      return;
    final epoch = _epoch,
        binding = _binding,
        bytes = delete ? null : Uint8List.fromList(_draft!);
    final p = CommunityAvatarPending(
      newEntityShareOperationID(),
      state.version,
      delete ? 'DELETE' : 'SAVE',
      stage: 'PREPARED',
    );
    setState(() => _busy = true);
    try {
      final approval = await widget.api.preview(widget.communityID, p, bytes);
      if (!_current(epoch, binding)) return;
      setState(() => _confirming = true);
      final yes = await showDialog<bool>(
        context: context,
        useRootNavigator: false,
        builder: (context) => AnimatedBuilder(
          animation: widget.identityChanges,
          builder: (context, _) => AlertDialog(
            title: Text(delete ? '删除社群头像？' : '保存社群头像？'),
            content: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(widget.communityName),
                if (bytes != null)
                  Padding(
                    padding: const EdgeInsets.only(top: 12),
                    child: ClipOval(
                      child: Image.memory(
                        bytes,
                        width: 96,
                        height: 96,
                        fit: BoxFit.cover,
                      ),
                    ),
                  ),
                const SizedBox(height: 12),
                Text(delete ? '将恢复默认头像。' : '头像随社群可见范围展示。'),
              ],
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(context, false),
                child: const Text('取消'),
              ),
              FilledButton(
                onPressed:
                    _current(epoch, binding) &&
                        approval.expiresAt.isAfter(DateTime.now().toUtc())
                    ? () => Navigator.pop(context, true)
                    : null,
                child: Text(delete ? '确认删除' : '确认保存'),
              ),
            ],
          ),
        ),
      );
      if (_current(epoch, binding)) setState(() => _confirming = false);
      if (yes != true || !_current(epoch, binding)) return;
      if (!approval.expiresAt.isAfter(DateTime.now().toUtc())) {
        setState(() => _message = '确认已过期，请重新检查头像。');
        return;
      }
      await _store.write(
        widget.api.baseUrl,
        widget.ownerID,
        widget.communityID,
        p,
      );
      if (!_current(epoch, binding)) return;
      final unknown = CommunityAvatarPending(
        p.operationID,
        p.version,
        p.action,
      );
      await _store.write(
        widget.api.baseUrl,
        widget.ownerID,
        widget.communityID,
        unknown,
      );
      if (!_current(epoch, binding)) return;
      final durable = await _store.read(
        widget.api.baseUrl,
        widget.ownerID,
        widget.communityID,
      );
      if (durable == null ||
          !durable.same(unknown) ||
          durable.stage != 'UNKNOWN')
        throw const FormatException('头像恢复记录未确认');
      if (!_current(epoch, binding)) return;
      setState(() => _pending = unknown);
      if (!approval.expiresAt.isAfter(DateTime.now().toUtc())) {
        // No PUT was started. Retire this exact durable selector before
        // requiring a fresh explicit read/approval; never send an old token.
        await _store.remove(
          widget.api.baseUrl,
          widget.ownerID,
          widget.communityID,
          unknown,
        );
        if (!_current(epoch, binding)) return;
        setState(() {
          _pending = null;
          _state = null;
          _message = '确认已过期，请重新读取头像。';
        });
        return;
      }
      final result = await widget.api.change(
        widget.communityID,
        unknown,
        bytes,
        approval,
      );
      if (!_current(epoch, binding)) return;
      await _store.remove(
        widget.api.baseUrl,
        widget.ownerID,
        widget.communityID,
        unknown,
      );
      if (!_current(epoch, binding)) return;
      setState(() {
        _pending = null;
        _state = result.current;
        _draft = null;
        _message = delete ? '头像已删除。' : '社群头像已保存。';
      });
      widget.onChanged?.call();
    } catch (e) {
      if (_current(epoch, binding)) {
        final sent = _pending?.stage == 'UNKNOWN';
        // These statuses are explicit pre-commit rejections. Session loss, missing
        // result, transport/decoding failures remain UNKNOWN: never rebase or PUT.
        if (sent &&
            e is CommunityAvatarFailure &&
            const {400, 403, 409}.contains(e.status)) {
          try {
            await _store.remove(
              widget.api.baseUrl,
              widget.ownerID,
              widget.communityID,
              _pending!,
            );
            if (!_current(epoch, binding)) return;
            setState(() {
              _pending = null;
              _state = null;
              _message = e.message;
            });
          } catch (_) {
            if (_current(epoch, binding))
              setState(() => _message = '保存结果待核实，请先核实原操作。');
          }
        } else {
          setState(() {
            _message = sent ? '保存结果待核实，请先核实原操作。' : _error(e);
            if (!sent) _journalFailed = true;
          });
        }
      }
    } finally {
      if (_current(epoch, binding))
        setState(() {
          _busy = false;
          _confirming = false;
        });
    }
  }

  Future<void> _recover() async {
    final pending = _pending;
    if (pending == null || _busy || !widget.current()) return;
    final epoch = _epoch, binding = _binding;
    setState(() => _busy = true);
    try {
      if (pending.stage == 'PREPARED') {
        await _store.remove(
          widget.api.baseUrl,
          widget.ownerID,
          widget.communityID,
          pending,
        );
        if (!_current(epoch, binding)) return;
        setState(() {
          _pending = null;
          _message = '准备记录已清除，尚未提交头像。';
          _journalFailed = false;
        });
        return;
      }
      final result = await widget.api.operation(widget.communityID, pending);
      if (!_current(epoch, binding)) return;
      await _store.remove(
        widget.api.baseUrl,
        widget.ownerID,
        widget.communityID,
        pending,
      );
      if (!_current(epoch, binding)) return;
      setState(() {
        _pending = null;
        _state = result.current;
        _draft = null;
        _message = '原头像操作已完成。';
        _journalFailed = false;
      });
      widget.onChanged?.call();
    } catch (e) {
      if (_current(epoch, binding))
        setState(
          () => _message = e is CommunityAvatarFailure && e.status == 404
              ? '尚未找到原操作结果，请稍后再核实。'
              : _error(e),
        );
    } finally {
      if (_current(epoch, binding)) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final enabled =
        widget.current() &&
        !_busy &&
        _pending == null &&
        !_journalFailed &&
        _state != null;
    return Padding(
      padding: EdgeInsets.only(
        left: 20,
        right: 20,
        top: 12,
        bottom:
            MediaQuery.viewInsetsOf(context).bottom +
            MediaQuery.paddingOf(context).bottom +
            20,
      ),
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('社群头像', style: Theme.of(context).textTheme.titleLarge),
            Text(widget.communityName),
            const SizedBox(height: 16),
            Align(
              alignment: Alignment.center,
              child: _draft == null
                  ? CommunityAvatar(
                      metadata: _state?.avatar,
                      fallbackLabel: widget.communityName,
                      api: widget.api,
                      identityChanges: widget.identityChanges,
                      current: widget.current,
                      radius: 48,
                    )
                  : ClipOval(
                      child: Image.memory(
                        _draft!,
                        width: 96,
                        height: 96,
                        fit: BoxFit.cover,
                      ),
                    ),
            ),
            if (_busy && !_confirming) const LinearProgressIndicator(),
            const SizedBox(height: 16),
            Wrap(
              spacing: 12,
              runSpacing: 8,
              children: [
                OutlinedButton.icon(
                  key: const Key('community-avatar-select'),
                  onPressed: enabled ? _select : null,
                  icon: const Icon(Icons.photo_library_outlined),
                  label: const Text('选择头像'),
                ),
                if (_draft != null)
                  FilledButton(
                    key: const Key('community-avatar-save'),
                    onPressed: enabled ? _save : null,
                    child: const Text('保存头像'),
                  ),
                if (_state?.avatar != null && _draft == null)
                  TextButton(
                    onPressed: enabled ? () => _save(delete: true) : null,
                    child: const Text('删除头像'),
                  ),
              ],
            ),
            if (_pending != null)
              TextButton(
                key: const Key('community-avatar-recover'),
                onPressed: _busy ? null : _recover,
                child: Text(
                  _pending!.stage == 'PREPARED' ? '清除准备记录' : '核实原头像操作',
                ),
              ),
            if (_state == null || _journalFailed)
              TextButton(
                onPressed: _busy ? null : _load,
                child: const Text('重新读取'),
              ),
            if (_message != null)
              Text(_message!, key: const Key('community-avatar-status')),
          ],
        ),
      ),
    );
  }
}
