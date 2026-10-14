import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import '../app/birdtie_app_bar.dart';
import '../content/moment_image_picker.dart';
import 'entity_cover.dart';
import 'entity_share_pending_store.dart' show newEntityShareOperationID;

class EntityCoverPending {
  const EntityCoverPending(
    this.operationID,
    this.expectedVersion,
    this.action, {
    this.stage = 'UNKNOWN',
  });
  final String operationID, action, stage;
  final int expectedVersion;
  Map<String, Object> toJson() => {
    'operationId': operationID,
    'expectedVersion': expectedVersion,
    'action': action,
    'stage': stage,
  };
  bool same(EntityCoverPending o) =>
      operationID == o.operationID &&
      expectedVersion == o.expectedVersion &&
      action == o.action;
  static EntityCoverPending fromJson(Object? raw) {
    if (raw is! Map<String, dynamic> ||
        raw.length != 4 ||
        !entityCoverID(
          raw['operationId'] is String ? raw['operationId'] : '',
        ) ||
        raw['expectedVersion'] is! int ||
        raw['expectedVersion'] < 0 ||
        raw['expectedVersion'] >= entityCoverMaxVersion ||
        !const {'SAVE', 'DELETE'}.contains(raw['action']) ||
        !const {'PREPARED', 'UNKNOWN'}.contains(raw['stage']))
      throw const FormatException('原封面操作记录不符');
    return EntityCoverPending(
      raw['operationId'],
      raw['expectedVersion'],
      raw['action'],
      stage: raw['stage'],
    );
  }
}

abstract interface class EntityCoverPendingStore {
  Future<EntityCoverPending?> read(
    String origin,
    String owner,
    EntityCoverRef ref,
  );
  Future<void> write(
    String origin,
    String owner,
    EntityCoverRef ref,
    EntityCoverPending pending,
  );
  Future<void> remove(
    String origin,
    String owner,
    EntityCoverRef ref,
    EntityCoverPending pending,
  );
}

class SecureEntityCoverPendingStore implements EntityCoverPendingStore {
  SecureEntityCoverPendingStore({FlutterSecureStorage? storage})
    : _storage = storage ?? const FlutterSecureStorage();
  final FlutterSecureStorage _storage;
  static final _tails = <String, Future<void>>{};
  String _key(String origin, String owner, EntityCoverRef ref) =>
      'birdtie.cover.pending.v1.${sha256.convert(utf8.encode('$origin\n$owner\n${ref.type}\n${ref.id}'))}';
  Future<T> _serial<T>(String key, Future<T> Function() work) async {
    final old = _tails[key], done = Completer<void>();
    _tails[key] = done.future;
    try {
      if (old != null) await old;
      return await work();
    } finally {
      if (identical(_tails[key], done.future)) _tails.remove(key);
      done.complete();
    }
  }

  Future<EntityCoverPending?> _read(String key) async {
    final raw = await _storage.read(key: key);
    return raw == null ? null : EntityCoverPending.fromJson(jsonDecode(raw));
  }

  @override
  Future<EntityCoverPending?> read(
    String origin,
    String owner,
    EntityCoverRef ref,
  ) => _serial(_key(origin, owner, ref), () => _read(_key(origin, owner, ref)));
  @override
  Future<void> write(
    String origin,
    String owner,
    EntityCoverRef ref,
    EntityCoverPending p,
  ) => _serial(_key(origin, owner, ref), () async {
    EntityCoverPending.fromJson(p.toJson());
    final key = _key(origin, owner, ref), old = await _read(key);
    if (old != null &&
        (!old.same(p) || (old.stage == 'UNKNOWN' && p.stage == 'PREPARED')))
      throw const FormatException('上次封面结果尚未核实');
    await _storage.write(key: key, value: jsonEncode(p.toJson()));
  });
  @override
  Future<void> remove(
    String origin,
    String owner,
    EntityCoverRef ref,
    EntityCoverPending p,
  ) => _serial(_key(origin, owner, ref), () async {
    final key = _key(origin, owner, ref), old = await _read(key);
    if (old != null && (!old.same(p) || old.stage != p.stage))
      throw const FormatException('原封面记录已变化');
    await _storage.delete(key: key);
  });
}

class MemoryEntityCoverPendingStore implements EntityCoverPendingStore {
  final values = <(String, String, EntityCoverRef), EntityCoverPending>{};
  @override
  Future<EntityCoverPending?> read(
    String o,
    String owner,
    EntityCoverRef r,
  ) async => values[(o, owner, r)];
  @override
  Future<void> write(
    String o,
    String owner,
    EntityCoverRef r,
    EntityCoverPending p,
  ) async {
    final old = values[(o, owner, r)];
    if (old != null &&
        (!old.same(p) || (old.stage == 'UNKNOWN' && p.stage == 'PREPARED')))
      throw const FormatException('上次封面结果尚未核实');
    values[(o, owner, r)] = p;
  }

  @override
  Future<void> remove(
    String o,
    String owner,
    EntityCoverRef r,
    EntityCoverPending p,
  ) async {
    final old = values[(o, owner, r)];
    if (old != null && (!old.same(p) || old.stage != p.stage))
      throw const FormatException('原封面记录已变化');
    values.remove((o, owner, r));
  }
}

/// Independent cover operation: it never creates/publishes an activity.
class EntityCoverEditor extends StatefulWidget {
  const EntityCoverEditor({
    super.key,
    required this.ref,
    required this.name,
    required this.ownerID,
    required this.api,
    required this.identityChanges,
    required this.current,
    this.pendingStore,
    this.picker,
    this.initialImage,
    this.onChanged,
  });
  final EntityCoverRef ref;
  final String name, ownerID;
  final EntityCoverAPI api;
  final Listenable identityChanges;
  final bool Function() current;
  final EntityCoverPendingStore? pendingStore;
  final Future<Uint8List?> Function()? picker;
  final Uint8List? initialImage;
  final VoidCallback? onChanged;
  @override
  State<EntityCoverEditor> createState() => _EntityCoverEditorState();
}

class _EntityCoverEditorState extends State<EntityCoverEditor> {
  final _attribution = TextEditingController(),
      _license = TextEditingController(),
      _until = TextEditingController();
  late EntityCoverPendingStore _store;
  EntityCoverState? _state;
  EntityCoverPending? _pending, _notSubmitted;
  Uint8List? _draft;
  String? _basis, _message, _lastReceiptMessage;
  bool _retain = false,
      _publish = false,
      _busy = false,
      _confirming = false,
      _storageFailed = false,
      _sourceUnavailable = false,
      _retired = false;
  int _epoch = 0;
  Timer? _approvalTimer;
  Future<bool?>? _dialogCompletion;
  bool _retirementDisposed = false;
  final _retirement = ValueNotifier(0);
  Object? _seen;
  Object get _binding => (
    widget.api.client,
    widget.api.baseUrl,
    widget.api.authorizationHeader(),
    widget.ownerID,
    widget.ref,
    widget.current(),
  );
  bool _current(int epoch, Object binding) =>
      mounted &&
      !_retired &&
      epoch == _epoch &&
      binding == _binding &&
      widget.current();
  @override
  void initState() {
    super.initState();
    _store = widget.pendingStore ?? SecureEntityCoverPendingStore();
    _seen = _binding;
    _draft = widget.initialImage == null
        ? null
        : Uint8List.fromList(widget.initialImage!);
    widget.identityChanges.addListener(_observe);
    unawaited(_load());
  }

  void _observe() {
    if (_seen == _binding) return;
    _retired = true;
    _approvalTimer?.cancel();
    ++_epoch;
    _draft = null;
    _pending = null;
    _state = null;
    _lastReceiptMessage = null;
    _attribution.clear();
    _license.clear();
    _until.clear();
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) _retirement.value++;
      });
    } else {
      _retirement.value++;
    }
    if (mounted)
      setState(() {
        _busy = false;
        _confirming = false;
        _message = '当前身份、管理权限或对象已变化，请重新打开封面编辑。';
      });
  }

  @override
  void didUpdateWidget(EntityCoverEditor old) {
    super.didUpdateWidget(old);
    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges.removeListener(_observe);
      widget.identityChanges.addListener(_observe);
    }
    if (old.pendingStore != widget.pendingStore) _retired = true;
    _observe();
  }

  @override
  void dispose() {
    ++_epoch;
    _approvalTimer?.cancel();
    widget.identityChanges.removeListener(_observe);
    _attribution.dispose();
    _license.dispose();
    _until.dispose();
    final completion = _dialogCompletion;
    void disposeRetirement() {
      if (_retirementDisposed) return;
      _retirementDisposed = true;
      _retirement.dispose();
    }

    if (completion == null) {
      disposeRetirement();
    } else {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!_retirementDisposed) _retirement.value++;
      });
      unawaited(completion.then((_) => disposeRetirement()));
    }
    _draft = null;
    super.dispose();
  }

  String _error(Object e) => e is EntityCoverFailure
      ? e.message
      : e is FormatException
      ? '图片、授权或原操作记录未通过检查，请重新读取。'
      : '连接中断，请重试读取。';
  Future<void> _load() async {
    if (_retired || _busy || !widget.current()) return;
    final epoch = _epoch, binding = _binding;
    bool storageReady = false;
    setState(() => _busy = true);
    try {
      final pending = await _store.read(
        widget.api.baseUrl,
        widget.ownerID,
        widget.ref,
      );
      if (!_current(epoch, binding)) return;
      _pending = pending;
      _storageFailed = false;
      storageReady = true;
      if (pending != null) _draft = null;
      final state = await widget.api.state(widget.ref);
      if (!_current(epoch, binding)) return;
      setState(() {
        _state = state;
        _sourceUnavailable = false;
        _message = pending == null
            ? _lastReceiptMessage
            : pending.stage == 'PREPARED'
            ? '上次仅准备了本机记录，尚未提交。'
            : '上次封面结果待核实，请先读取原操作。';
      });
    } catch (e) {
      if (_current(epoch, binding))
        setState(() {
          _state = null;
          _sourceUnavailable = true;
          _draft = null;
          _clearRights();
          _storageFailed = !storageReady && _pending == null;
          _message = _lastReceiptMessage == null
              ? _error(e)
              : '${_lastReceiptMessage!}\n当前封面资料暂不可读取，请重新读取；原操作不会再次提交。';
        });
    } finally {
      if (_current(epoch, binding)) setState(() => _busy = false);
    }
  }

  Future<void> _select() async {
    if (!_enabled) return;
    final epoch = _epoch, binding = _binding;
    setState(() => _busy = true);
    try {
      final bytes =
          await (widget.picker ??
              () async => (await MomentImagePicker().select())?.bytes)();
      if (bytes == null || !_current(epoch, binding)) return;
      // Validate bounded static pixels; retain only an in-memory local draft.
      final rights = EntityCoverRights(
        basis: 'OWNED',
        attribution: '',
        licenseReference: '',
        retentionUntil: DateTime.now().add(const Duration(days: 1)),
        canRetain: true,
        canPublish: true,
      );
      widget.api.input(
        newEntityShareOperationID(),
        _state!.version,
        'SAVE',
        bytes,
        rights,
      );
      setState(() {
        _draft = Uint8List.fromList(bytes);
        _message = null;
      });
    } catch (e) {
      if (_current(epoch, binding)) setState(() => _message = _error(e));
    } finally {
      if (_current(epoch, binding)) setState(() => _busy = false);
    }
  }

  EntityCoverRights? get _rights {
    final date = DateTime.tryParse(_until.text.trim());
    if (date == null ||
        !RegExp(r'^\d{4}-\d\d-\d\d$').hasMatch(_until.text.trim()) ||
        date.toIso8601String().split('T').first != _until.text.trim() ||
        _basis == null)
      return null;
    final r = EntityCoverRights(
      basis: _basis!,
      attribution: _attribution.text.trim(),
      licenseReference: _license.text.trim(),
      retentionUntil: date
          .add(const Duration(days: 1))
          .subtract(const Duration(seconds: 1))
          .toUtc(),
      canRetain: _retain,
      canPublish: _publish,
    );
    return r.valid ? r : null;
  }

  bool get _enabled =>
      !_retired &&
      !_busy &&
      !_storageFailed &&
      _pending == null &&
      _state != null &&
      widget.current();
  Future<void> _checkStorage() async {
    if (_busy || _retired || !widget.current()) return;
    final epoch = _epoch, binding = _binding;
    setState(() => _busy = true);
    try {
      final p = await _store.read(
        widget.api.baseUrl,
        widget.ownerID,
        widget.ref,
      );
      if (!_current(epoch, binding)) return;
      if (p != null &&
          (p.stage == 'PREPARED' || (_notSubmitted?.same(p) == true))) {
        await _store.remove(widget.api.baseUrl, widget.ownerID, widget.ref, p);
        if (!_current(epoch, binding)) return;
        _pending = null;
        _notSubmitted = null;
        _message = '本机准备记录已清除，尚未提交。';
      } else {
        _pending = p;
        _message = p == null ? '本机记录已核对，请重新读取。' : '原操作仍待核实，不会重新提交。';
      }
      _storageFailed = false;
    } catch (e) {
      if (_current(epoch, binding)) setState(() => _message = _error(e));
    } finally {
      if (_current(epoch, binding)) setState(() => _busy = false);
    }
    if (_current(epoch, binding) && _pending == null) await _load();
  }

  Future<void> _save({bool delete = false}) async {
    if (!_enabled || (!delete && (_draft == null || _rights == null))) return;
    final epoch = _epoch,
        binding = _binding,
        ref = widget.ref,
        owner = widget.ownerID,
        api = widget.api;
    final bytes = delete ? null : Uint8List.fromList(_draft!),
        rights = delete ? null : _rights;
    final p = EntityCoverPending(
      newEntityShareOperationID(),
      _state!.version,
      delete ? 'DELETE' : 'SAVE',
      stage: 'PREPARED',
    );
    bool sent = false;
    setState(() => _busy = true);
    try {
      final approval = await api.preview(
        ref,
        p.operationID,
        p.expectedVersion,
        p.action,
        bytes,
        rights,
      );
      if (!_current(epoch, binding)) return;
      setState(() => _confirming = true);
      final live = ValueNotifier(true);
      final painted = ValueNotifier(delete);
      bool dialogOpen = true;
      final timer = Timer(
        approval.expiresAt.difference(DateTime.now().toUtc()),
        () => live.value = false,
      );
      _approvalTimer = timer;
      bool? yes;
      DialogRoute<bool>? route;
      try {
        route = DialogRoute<bool>(
          context: context,
          builder: (context) => AnimatedBuilder(
            animation: Listenable.merge([
              widget.identityChanges,
              _retirement,
              live,
              painted,
            ]),
            builder: (context, _) => AlertDialog(
              title: Text(
                !_current(epoch, binding)
                    ? '原封面确认已退出'
                    : !live.value
                    ? '封面确认已到期'
                    : delete
                    ? '删除封面？'
                    : '确认封面与授权',
              ),
              content: SingleChildScrollView(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    if (!_current(epoch, binding))
                      const Text('当前身份或对象已变化，请返回重新打开。')
                    else if (!live.value)
                      const Text('封面确认已到期，旧预览与授权已移除。请返回重新查看。')
                    else ...[
                      Text(widget.name),
                      if (approval.bytes != null) ...[
                        const SizedBox(height: 12),
                        Image.memory(
                          approval.bytes!,
                          width: 320,
                          height: 180,
                          fit: BoxFit.contain,
                          semanticLabel: '服务端实际裁剪封面预览',
                          frameBuilder: (context, child, frame, synchronous) {
                            if (frame != null && !painted.value) {
                              WidgetsBinding.instance.addPostFrameCallback((_) {
                                if (dialogOpen &&
                                    _current(epoch, binding) &&
                                    live.value &&
                                    !painted.value)
                                  painted.value = true;
                              });
                            }
                            return frame == null
                                ? const Text('正在显示实际裁剪预览…')
                                : child;
                          },
                          errorBuilder: (_, __, ___) =>
                              const Text('封面预览无法显示，请返回编辑。'),
                        ),
                      ],
                      const SizedBox(height: 12),
                      Text(
                        delete
                            ? '仅删除此对象的封面，原活动或组织内容保留。'
                            : '按此对象原可见范围展示。你确认有权保留并公开这张图片；不会交给智能体分析。',
                      ),
                      if (rights != null) ...[
                        Text(
                          rights.basis == 'OWNED'
                              ? '授权依据：本人拥有图片权利'
                              : '授权依据：已获许可',
                        ),
                        if (rights.attribution.isNotEmpty)
                          Text('署名：${rights.attribution}'),
                        if (rights.licenseReference.isNotEmpty)
                          Text('许可依据：${rights.licenseReference}'),
                        Text('保留截止：${_until.text.trim()}'),
                      ],
                    ],
                  ],
                ),
              ),
              actions: [
                TextButton(
                  onPressed: () => Navigator.pop(context, false),
                  child: const Text('返回编辑'),
                ),
                FilledButton(
                  onPressed:
                      live.value &&
                          painted.value &&
                          _current(epoch, binding) &&
                          approval.expiresAt.isAfter(DateTime.now().toUtc())
                      ? () {
                          if (!dialogOpen ||
                              !_current(epoch, binding) ||
                              !live.value ||
                              !painted.value ||
                              !approval.expiresAt.isAfter(
                                DateTime.now().toUtc(),
                              ))
                            return;
                          Navigator.pop(context, true);
                        }
                      : null,
                  child: Text(delete ? '确认删除' : '确认保存封面'),
                ),
              ],
            ),
          ),
        );
        final completion = route.completed;
        _dialogCompletion = completion;
        unawaited(
          completion.then((_) {
            if (identical(_dialogCompletion, completion))
              _dialogCompletion = null;
          }),
        );
        yes = await Navigator.of(context).push(route);
      } finally {
        dialogOpen = false;
        timer.cancel();
        if (identical(_approvalTimer, timer)) _approvalTimer = null;
        void disposeDialogState() {
          live.dispose();
          painted.dispose();
        }

        if (route == null) {
          disposeDialogState();
        } else {
          unawaited(route.completed.then((_) => disposeDialogState()));
        }
      }
      if (_current(epoch, binding)) setState(() => _confirming = false);
      if (yes != true || !_current(epoch, binding)) return;
      if (!approval.expiresAt.isAfter(DateTime.now().toUtc()))
        throw const FormatException('封面确认已过期');
      _notSubmitted = p;
      await _store.write(api.baseUrl, owner, ref, p);
      if (!_current(epoch, binding)) return;
      final prepared = await _store.read(api.baseUrl, owner, ref);
      if (prepared == null || !prepared.same(p) || prepared.stage != 'PREPARED')
        throw const FormatException('本机准备记录未确认');
      if (!_current(epoch, binding)) return;
      final unknown = EntityCoverPending(
        p.operationID,
        p.expectedVersion,
        p.action,
      );
      await _store.write(api.baseUrl, owner, ref, unknown);
      if (!_current(epoch, binding)) return;
      final durable = await _store.read(api.baseUrl, owner, ref);
      if (durable == null ||
          !durable.same(unknown) ||
          durable.stage != 'UNKNOWN')
        throw const FormatException('本机原操作记录未确认');
      if (!_current(epoch, binding)) return;
      _pending = unknown;
      if (!approval.expiresAt.isAfter(DateTime.now().toUtc()))
        throw const FormatException('封面确认已过期');
      sent = true;
      _notSubmitted = null;
      final result = await api.change(
        ref,
        p.operationID,
        p.expectedVersion,
        p.action,
        bytes,
        rights,
        approval,
      );
      if (!_current(epoch, binding)) return;
      await _finish(result, unknown, epoch, binding);
    } catch (e) {
      if (_current(epoch, binding))
        setState(() {
          if (sent) {
            _message = '保存结果待核实，请先核实原操作。';
          } else if (_notSubmitted != null) {
            _storageFailed = true;
            _message = '尚未提交，无法确认本机记录。请检查本机记录。';
          } else {
            _message = _error(e);
          }
        });
    } finally {
      if (_current(epoch, binding)) setState(() => _busy = false);
    }
  }

  Future<void> _finish(
    EntityCoverReceipt result,
    EntityCoverPending p,
    int epoch,
    Object binding,
  ) async {
    EntityCoverState? state;
    Object? readFailure;
    try {
      state = await widget.api.state(widget.ref);
    } catch (e) {
      readFailure = e;
    }
    if (!_current(epoch, binding)) return;
    if (state != null &&
        result.status == 'SUCCEEDED' &&
        state.version < result.appliedVersion) {
      state = null;
      readFailure = const FormatException('当前封面读取未通过版本检查');
    }
    // The strictly matched immutable original receipt is definitive. Today's
    // denied/unavailable source cannot erase that fact or authorize new writes.
    await _store.remove(widget.api.baseUrl, widget.ownerID, widget.ref, p);
    if (!_current(epoch, binding)) return;
    setState(() {
      _pending = null;
      _notSubmitted = null;
      _storageFailed = false;
      _state = state;
      _sourceUnavailable = state == null;
      _draft = null;
      _clearRights();
      _lastReceiptMessage = result.status == 'STOPPED'
          ? '这次提交已停止；当前封面以重新读取的结果为准。'
          : p.action == 'DELETE'
          ? '原封面删除已完成。'
          : '原封面保存已完成。';
      _message = _lastReceiptMessage;
      if (readFailure != null) {
        _message = '${_message!}\n当前封面资料暂不可读取，请重新读取；原操作不会再次提交。';
      }
    });
    widget.onChanged?.call();
  }

  void _clearRights() {
    _attribution.clear();
    _license.clear();
    _until.clear();
    _basis = null;
    _retain = false;
    _publish = false;
  }

  Future<void> _recover({bool stop = false}) async {
    final p = _pending;
    if (p == null || _busy || _retired || !widget.current()) return;
    if (p.stage == 'PREPARED' || _notSubmitted?.same(p) == true) {
      await _checkStorage();
      return;
    }
    final epoch = _epoch, binding = _binding;
    if (stop) {
      final yes = await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          title: const Text('停止这次提交？'),
          content: const Text('只停止此原操作，可能已经完成。不会撤销已保存封面；停止后会重新读取当前结果。'),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: const Text('返回'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('确认停止'),
            ),
          ],
        ),
      );
      if (yes != true || !_current(epoch, binding)) return;
    }
    setState(() => _busy = true);
    try {
      final result = stop
          ? await widget.api.stop(
              widget.ref,
              p.operationID,
              p.expectedVersion,
              p.action,
            )
          : await widget.api.operation(
              widget.ref,
              p.operationID,
              p.expectedVersion,
              p.action,
            );
      if (!_current(epoch, binding)) return;
      await _finish(result, p, epoch, binding);
    } catch (e) {
      if (_current(epoch, binding))
        setState(
          () => _message = e is EntityCoverFailure && e.status == 404
              ? '尚未找到原操作结果，仍待核实；不会再次保存。'
              : _error(e),
        );
    } finally {
      if (_current(epoch, binding)) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: birdtieAppBar(
      context,
      title: Text(widget.ref.type == 'activity' ? '活动封面' : '组织封面'),
    ),
    body: SingleChildScrollView(
      padding: EdgeInsets.fromLTRB(
        20,
        16,
        20,
        MediaQuery.viewInsetsOf(context).bottom +
            MediaQuery.paddingOf(context).bottom +
            24,
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            _retired
                ? '原对象编辑已退出'
                : _sourceUnavailable
                ? '原对象当前资料不可读取'
                : widget.name,
            style: Theme.of(context).textTheme.titleLarge,
          ),
          if (!_retired) ...[
            EntityCover(
              metadata: _state?.cover,
              api: widget.api,
              current: () => !_retired && widget.current(),
              identityChanges: widget.identityChanges,
              width: 320,
              height: 180,
            ),
            if (_draft != null) ...[
              const SizedBox(height: 16),
              const Text('本机草稿，尚未保存'),
              Image.memory(
                _draft!,
                width: 320,
                height: 180,
                fit: BoxFit.contain,
              ),
            ],
            const SizedBox(height: 16),
            OutlinedButton.icon(
              key: const Key('cover-select'),
              onPressed: _enabled ? _select : null,
              icon: const Icon(Icons.photo_library_outlined),
              label: const Text('选择图片'),
            ),
            if (_draft != null) ...[
              const SizedBox(height: 16),
              DropdownButtonFormField<String>(
                key: const Key('cover-rights-basis'),
                initialValue: _basis,
                isExpanded: true,
                decoration: const InputDecoration(labelText: '图片权利依据'),
                items: const [
                  DropdownMenuItem(value: 'OWNED', child: Text('本人拥有图片权利')),
                  DropdownMenuItem(value: 'LICENSED', child: Text('已获许可')),
                ],
                onChanged: _enabled ? (v) => setState(() => _basis = v) : null,
              ),
              TextField(
                key: const Key('cover-attribution'),
                controller: _attribution,
                enabled: _enabled,
                maxLength: 320,
                decoration: const InputDecoration(labelText: '公开署名（如需）'),
                onChanged: (_) => setState(() {}),
              ),
              if (_basis == 'LICENSED')
                TextField(
                  key: const Key('cover-license'),
                  controller: _license,
                  enabled: _enabled,
                  maxLength: 1024,
                  decoration: const InputDecoration(labelText: '许可依据（必填）'),
                  onChanged: (_) => setState(() {}),
                ),
              TextField(
                key: const Key('cover-retention'),
                controller: _until,
                enabled: _enabled,
                keyboardType: TextInputType.datetime,
                decoration: const InputDecoration(
                  labelText: '保留截止日期（YYYY-MM-DD）',
                ),
                onChanged: (_) => setState(() {}),
              ),
              CheckboxListTile(
                key: const Key('cover-retain'),
                contentPadding: EdgeInsets.zero,
                value: _retain,
                onChanged: _enabled
                    ? (v) => setState(() => _retain = v == true)
                    : null,
                title: const Text('我有权让 Birdtie 保留至上述日期'),
              ),
              CheckboxListTile(
                key: const Key('cover-publish'),
                contentPadding: EdgeInsets.zero,
                value: _publish,
                onChanged: _enabled
                    ? (v) => setState(() => _publish = v == true)
                    : null,
                title: const Text('我有权按此对象可见范围展示图片'),
              ),
              FilledButton(
                key: const Key('cover-preview'),
                onPressed: _enabled && _rights != null ? () => _save() : null,
                child: const Text('查看实际封面预览'),
              ),
              if (_enabled && _rights == null)
                const Padding(
                  padding: EdgeInsets.only(top: 8),
                  child: Text('请选择权利依据和有效的未来截止日期，并确认保留与展示权限。'),
                ),
            ],
            if (_state?.cover != null)
              TextButton(
                onPressed: _enabled ? () => _save(delete: true) : null,
                child: const Text('删除封面'),
              ),
          ],
          if (_busy && !_confirming) const LinearProgressIndicator(),
          if (_message != null)
            Padding(
              padding: const EdgeInsets.only(top: 12),
              child: Text(_message!),
            ),
          if (!_retired && _storageFailed)
            TextButton(
              onPressed: _busy ? null : _checkStorage,
              child: const Text('检查本机记录'),
            ),
          if (!_retired && _pending != null && !_storageFailed)
            Wrap(
              spacing: 8,
              children: [
                TextButton(
                  onPressed: _busy ? null : () => _recover(),
                  child: Text(
                    _pending!.stage == 'PREPARED' ? '清除未提交准备记录' : '核实上次封面操作',
                  ),
                ),
                if (_pending!.stage == 'UNKNOWN')
                  TextButton(
                    onPressed: _busy ? null : () => _recover(stop: true),
                    child: const Text('停止这次提交'),
                  ),
              ],
            ),
          if (!_retired && _pending == null && !_storageFailed)
            TextButton(
              onPressed: _busy ? null : _load,
              child: const Text('重新读取封面'),
            ),
        ],
      ),
    ),
  );
}
