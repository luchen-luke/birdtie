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
import 'place_photo.dart';

class PlacePhotoPending {
  const PlacePhotoPending(this.selector, {this.stage = 'UNKNOWN'});
  final PlacePhotoSelector selector;
  final String stage;
  Map<String, Object> toJson() => {...selector.toJson(), 'stage': stage};
  bool same(PlacePhotoPending p) => selector.same(p.selector);
  static PlacePhotoPending fromJson(Object? raw) {
    if (raw is! Map<String, dynamic> ||
        raw.keys.any(
          (k) => !const {
            'operationId',
            'expectedVersion',
            'action',
            'candidateId',
            'candidateVersion',
            'stage',
          }.contains(k),
        ) ||
        raw.length != (raw['action'] == 'SUBMIT' ? 4 : 6) ||
        raw['operationId'] is! String ||
        raw['expectedVersion'] is! int ||
        raw['action'] is! String ||
        !const {'PREPARED', 'UNKNOWN'}.contains(raw['stage']) ||
        (raw['action'] != 'SUBMIT' &&
            (raw['candidateId'] is! String ||
                raw['candidateVersion'] is! int))) {
      throw const FormatException('原地点照片记录不符');
    }
    final s = PlacePhotoSelector(
      operationID: raw['operationId'],
      expectedVersion: raw['expectedVersion'],
      action: raw['action'],
      candidateID: raw['candidateId'] ?? '',
      candidateVersion: raw['candidateVersion'] ?? 0,
    );
    if (!s.valid) throw const FormatException('原地点照片操作不符');
    return PlacePhotoPending(s, stage: raw['stage']);
  }
}

abstract interface class PlacePhotoPendingStore {
  Future<PlacePhotoPending?> read(
    String origin,
    String owner,
    PlacePhotoRef ref,
  );
  Future<void> write(
    String origin,
    String owner,
    PlacePhotoRef ref,
    PlacePhotoPending p,
  );
  Future<void> remove(
    String origin,
    String owner,
    PlacePhotoRef ref,
    PlacePhotoPending p,
  );
}

/// Per-key serialization also spans instances; completed tails never retain a zone.
class SecurePlacePhotoPendingStore implements PlacePhotoPendingStore {
  SecurePlacePhotoPendingStore({FlutterSecureStorage? storage})
    : _storage = storage ?? const FlutterSecureStorage();
  final FlutterSecureStorage _storage;
  static final _tails = <String, Future<void>>{};
  String _key(String origin, String owner, PlacePhotoRef ref) =>
      'birdtie.place.photo.pending.v1.${sha256.convert(utf8.encode('$origin\n$owner\n${ref.id}'))}';
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

  Future<PlacePhotoPending?> _read(String key) async {
    final raw = await _storage.read(key: key);
    return raw == null ? null : PlacePhotoPending.fromJson(jsonDecode(raw));
  }

  @override
  Future<PlacePhotoPending?> read(String o, String owner, PlacePhotoRef ref) =>
      _serial(_key(o, owner, ref), () => _read(_key(o, owner, ref)));
  @override
  Future<void> write(
    String o,
    String owner,
    PlacePhotoRef ref,
    PlacePhotoPending p,
  ) => _serial(_key(o, owner, ref), () async {
    PlacePhotoPending.fromJson(p.toJson());
    final key = _key(o, owner, ref), old = await _read(key);
    if (old != null &&
        (!old.same(p) || (old.stage == 'UNKNOWN' && p.stage == 'PREPARED'))) {
      throw const FormatException('上次地点照片结果尚未核实');
    }
    await _storage.write(key: key, value: jsonEncode(p.toJson()));
  });
  @override
  Future<void> remove(
    String o,
    String owner,
    PlacePhotoRef ref,
    PlacePhotoPending p,
  ) => _serial(_key(o, owner, ref), () async {
    final key = _key(o, owner, ref), old = await _read(key);
    if (old != null && (!old.same(p) || old.stage != p.stage)) {
      throw const FormatException('原地点照片记录已变化');
    }
    await _storage.delete(key: key);
  });
}

class MemoryPlacePhotoPendingStore implements PlacePhotoPendingStore {
  final values = <(String, String, PlacePhotoRef), PlacePhotoPending>{};
  @override
  Future<PlacePhotoPending?> read(
    String o,
    String owner,
    PlacePhotoRef ref,
  ) async => values[(o, owner, ref)];
  @override
  Future<void> write(
    String o,
    String owner,
    PlacePhotoRef ref,
    PlacePhotoPending p,
  ) async {
    PlacePhotoPending.fromJson(p.toJson());
    final old = values[(o, owner, ref)];
    if (old != null &&
        (!old.same(p) || (old.stage == 'UNKNOWN' && p.stage == 'PREPARED'))) {
      throw const FormatException('上次地点照片结果尚未核实');
    }
    values[(o, owner, ref)] = p;
  }

  @override
  Future<void> remove(
    String o,
    String owner,
    PlacePhotoRef ref,
    PlacePhotoPending p,
  ) async {
    final old = values[(o, owner, ref)];
    if (old != null && (!old.same(p) || old.stage != p.stage)) {
      throw const FormatException('原地点照片记录已变化');
    }
    values.remove((o, owner, ref));
  }
}

/// Contribution and independent review; submission never claims publication.
class PlacePhotoEditor extends StatefulWidget {
  const PlacePhotoEditor({
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
  final PlacePhotoRef ref;
  final String name, ownerID;
  final PlacePhotoAPI api;
  final Listenable identityChanges;
  final bool Function() current;
  final PlacePhotoPendingStore? pendingStore;
  final Future<Uint8List?> Function()? picker;
  final Uint8List? initialImage;
  final VoidCallback? onChanged;
  @override
  State<PlacePhotoEditor> createState() => _PlacePhotoEditorState();
}

class _PlacePhotoEditorState extends State<PlacePhotoEditor> {
  final _attribution = TextEditingController(),
      _license = TextEditingController(),
      _until = TextEditingController();
  final _retirement = ValueNotifier(0);
  late PlacePhotoPendingStore _store;
  PlacePhotoEditorial? _editorial;
  PlacePhotoState? _state;
  PlacePhotoPending? _pending, _notSubmitted;
  Uint8List? _draft;
  String? _basis, _message;
  bool _retain = false,
      _publish = false,
      _busy = false,
      _confirming = false,
      _storageFailed = false,
      _sourceUnavailable = false,
      _retired = false;
  int _epoch = 0;
  Timer? _approvalTimer, _editorialTimer;
  Future<bool?>? _dialogCompletion;
  bool _retirementDisposed = false;
  Object? _seen;
  Object get _binding => (
    widget.api.client,
    widget.api.baseUrl,
    widget.api.authorizationHeader(),
    widget.ownerID,
    widget.ref,
    widget.current(),
    widget.api.current?.call() != false,
  );
  String get _origin => Uri.parse(widget.api.baseUrl).origin;
  bool _current(int epoch, Object binding) =>
      mounted &&
      !_retired &&
      epoch == _epoch &&
      binding == _binding &&
      widget.current() &&
      widget.api.current?.call() != false;
  @override
  void initState() {
    super.initState();
    _store = widget.pendingStore ?? SecurePlacePhotoPendingStore();
    _seen = _binding;
    _draft = widget.initialImage == null
        ? null
        : Uint8List.fromList(widget.initialImage!);
    widget.identityChanges.addListener(_observe);
    unawaited(_load());
  }

  void _observe() {
    if (!_retired &&
        _seen == _binding &&
        widget.current() &&
        widget.api.current?.call() != false)
      return;
    _retired = true;
    _approvalTimer?.cancel();
    _editorialTimer?.cancel();
    ++_epoch;
    _draft = null;
    _pending = null;
    _editorial = null;
    _state = null;
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
        _message = '当前身份、地点或权限已变化，请重新打开照片管理。';
      });
  }

  @override
  void didUpdateWidget(PlacePhotoEditor old) {
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
    _editorialTimer?.cancel();
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

  String _error(Object e) => e is PlacePhotoFailure
      ? e.message
      : e is FormatException
      ? '图片、权利或原操作记录未通过检查，请重新读取。'
      : '连接中断，请重试读取。';
  void _setEditorial(PlacePhotoEditorial e) {
    _editorialTimer?.cancel();
    _editorial = e;
    _editorialTimer = Timer(e.expiresAt.difference(DateTime.now().toUtc()), () {
      if (mounted && !_retired && identical(_editorial, e)) {
        setState(() {
          _editorial = null;
          _message = '照片权限读取已到期，请重新读取。';
        });
      }
    });
  }

  Future<void> _load() async {
    if (_retired ||
        _busy ||
        !widget.current() ||
        !entityCoverID(widget.ownerID))
      return;
    final epoch = _epoch, binding = _binding;
    bool storageReady = false;
    setState(() => _busy = true);
    try {
      final p = await _store.read(_origin, widget.ownerID, widget.ref);
      if (!_current(epoch, binding)) return;
      _pending = p;
      _storageFailed = false;
      storageReady = true;
      if (p != null) _draft = null;
      final editorial = await widget.api.editorial(widget.ref);
      if (!_current(epoch, binding)) return;
      final state = await widget.api.state(widget.ref);
      if (!_current(epoch, binding)) return;
      if (state.version != editorial.version) {
        throw const FormatException('地点照片两次读取版本不同');
      }
      setState(() {
        _setEditorial(editorial);
        _state = state;
        _sourceUnavailable = false;
        _message = p == null
            ? null
            : p.stage == 'PREPARED'
            ? '上次仅准备了本机记录，尚未提交。'
            : '上次照片结果待核实，请先读取原操作。';
      });
    } catch (e) {
      if (_current(epoch, binding))
        setState(() {
          _editorial = null;
          _state = null;
          _sourceUnavailable = true;
          _storageFailed = !storageReady && _pending == null;
          _message = _error(e);
        });
    } finally {
      if (_current(epoch, binding)) setState(() => _busy = false);
    }
  }

  bool get _enabled =>
      !_retired &&
      !_busy &&
      !_storageFailed &&
      _pending == null &&
      _editorial?.current == true &&
      _state != null &&
      widget.current() &&
      widget.api.current?.call() != false;
  EntityCoverRights? get _rights {
    final raw = _until.text.trim(),
        date = DateTime.tryParse(_until.text.trim());
    if (date == null ||
        !RegExp(r'^\d{4}-\d\d-\d\d$').hasMatch(raw) ||
        date.toIso8601String().split('T').first != raw ||
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

  Future<void> _select() async {
    if (!_enabled || _editorial?.canContribute != true) return;
    final epoch = _epoch, binding = _binding;
    setState(() => _busy = true);
    try {
      final bytes =
          await (widget.picker ??
              () async => (await MomentImagePicker().select())?.bytes)();
      if (bytes == null || !_current(epoch, binding)) return;
      widget.api.input(
        PlacePhotoSelector(
          operationID: newEntityShareOperationID(),
          expectedVersion: _state!.version,
          action: 'SUBMIT',
        ),
        bytes: bytes,
        rights: EntityCoverRights(
          basis: 'OWNED',
          attribution: '',
          licenseReference: '',
          retentionUntil: DateTime.now().add(const Duration(days: 1)),
          canRetain: true,
          canPublish: true,
        ),
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

  Future<void> _checkStorage() async {
    if (_busy || _retired || !widget.current()) return;
    final epoch = _epoch, binding = _binding;
    setState(() => _busy = true);
    try {
      final p = await _store.read(_origin, widget.ownerID, widget.ref);
      if (!_current(epoch, binding)) return;
      if (p != null &&
          (p.stage == 'PREPARED' || _notSubmitted?.same(p) == true)) {
        await _store.remove(_origin, widget.ownerID, widget.ref, p);
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

  bool _candidateAllowed(PlacePhotoCandidate candidate, String action) =>
      _enabled &&
      _editorial!.candidates.any((c) => identical(c, candidate)) &&
      switch (action) {
        'APPROVE' =>
          candidate.canApprove &&
              candidate.rights.valid &&
              candidate.version < entityCoverMaxVersion &&
              _editorial!.version < entityCoverMaxVersion,
        'REJECT' => candidate.canReject,
        'WITHDRAW' => candidate.canWithdraw,
        _ => false,
      };
  Future<void> _act(String action, {PlacePhotoCandidate? candidate}) async {
    if (!_enabled ||
        (action == 'SUBMIT'
            ? _editorial?.canContribute != true ||
                  _draft == null ||
                  _rights == null
            : candidate == null || !_candidateAllowed(candidate, action)))
      return;
    final epoch = _epoch,
        binding = _binding,
        api = widget.api,
        ref = widget.ref,
        owner = widget.ownerID,
        origin = _origin;
    final bytes = action == 'SUBMIT' ? Uint8List.fromList(_draft!) : null,
        rights = action == 'SUBMIT' ? _rights! : candidate!.rights;
    final selector = PlacePhotoSelector(
      operationID: newEntityShareOperationID(),
      expectedVersion: _editorial!.version,
      action: action,
      candidateID: candidate?.id ?? '',
      candidateVersion: candidate?.version ?? 0,
    );
    final p = PlacePhotoPending(selector, stage: 'PREPARED');
    bool sent = false;
    setState(() => _busy = true);
    try {
      final approval = await api.preview(
        ref,
        selector,
        bytes: bytes,
        rights: rights,
        thumbnail: candidate?.thumbnail,
      );
      if (!_current(epoch, binding)) return;
      setState(() => _confirming = true);
      final live = ValueNotifier(true),
          painted = ValueNotifier(approval.bytes == null);
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
          builder: (dialogContext) => AnimatedBuilder(
            animation: Listenable.merge([live, painted, _retirement]),
            builder: (_, _) {
              final sourceCurrent = _current(epoch, binding);
              final valid =
                  sourceCurrent &&
                  live.value &&
                  approval.expiresAt.isAfter(DateTime.now().toUtc());
              return AlertDialog(
                title: Text(
                  valid
                      ? _previewTitle(action)
                      : sourceCurrent
                      ? '这次照片确认已到期'
                      : '原照片确认已退出',
                ),
                content: SingleChildScrollView(
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: valid
                        ? [
                            Text(widget.name),
                            if (approval.bytes != null)
                              Image.memory(
                                approval.bytes!,
                                key: const Key(
                                  'place-photo-normalized-preview',
                                ),
                                width: 320,
                                height: 180,
                                fit: BoxFit.contain,
                                semanticLabel: '服务端规范化的地点照片',
                                frameBuilder:
                                    (context, child, frame, synchronous) {
                                      if ((frame != null || synchronous) &&
                                          !painted.value) {
                                        WidgetsBinding.instance
                                            .addPostFrameCallback((_) {
                                              if (dialogOpen &&
                                                  _current(epoch, binding) &&
                                                  live.value) {
                                                painted.value = true;
                                              }
                                            });
                                      }
                                      return child;
                                    },
                                errorBuilder: (_, _, _) =>
                                    const Text('照片未能显示，不能确认。'),
                              ),
                            Text(_consequence(action)),
                            Text(
                              '权利依据：${rights.basis == 'OWNED' ? '本人拥有图片权利' : '已获许可'}',
                            ),
                            if (rights.attribution.isNotEmpty)
                              Text('公开署名：${rights.attribution}'),
                            if (rights.licenseReference.isNotEmpty)
                              Text('许可依据：${rights.licenseReference}'),
                            Text(
                              '保留截止：${rights.retentionUntil.toLocal().toIso8601String().split('T').first}',
                            ),
                            if (!live.value) const Text('这次确认已到期，请返回重新查看。'),
                          ]
                        : [
                            Text(
                              sourceCurrent
                                  ? '确认已到期，旧照片与权利资料已移除。请返回重新查看。'
                                  : '身份、地点或权限已变化。旧照片与权利资料已移除。',
                            ),
                          ],
                  ),
                ),
                actions: [
                  TextButton(
                    onPressed: () => Navigator.pop(dialogContext, false),
                    child: const Text('返回'),
                  ),
                  FilledButton(
                    key: const Key('place-photo-confirm'),
                    onPressed:
                        valid &&
                            live.value &&
                            painted.value &&
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
                            Navigator.pop(dialogContext, true);
                          }
                        : null,
                    child: Text(_confirmLabel(action)),
                  ),
                ],
              );
            },
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
        yes = await Navigator.of(context, rootNavigator: true).push(route);
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
      if (yes != true ||
          !_current(epoch, binding) ||
          !approval.expiresAt.isAfter(DateTime.now().toUtc()))
        return;
      setState(() => _confirming = false);
      _notSubmitted = p;
      await _store.write(origin, owner, ref, p);
      if (!_current(epoch, binding)) return;
      _pending = p;
      final readPrepared = await _store.read(origin, owner, ref);
      if (readPrepared == null ||
          !readPrepared.same(p) ||
          readPrepared.stage != 'PREPARED') {
        throw const FormatException('本机准备记录未读回');
      }
      if (!_current(epoch, binding)) return;
      final unknown = PlacePhotoPending(selector);
      await _store.write(origin, owner, ref, unknown);
      if (!_current(epoch, binding)) return;
      _pending = unknown;
      final readUnknown = await _store.read(origin, owner, ref);
      if (readUnknown == null ||
          !readUnknown.same(unknown) ||
          readUnknown.stage != 'UNKNOWN') {
        throw const FormatException('原操作记录未读回');
      }
      if (!_current(epoch, binding)) return;
      if (!approval.expiresAt.isAfter(DateTime.now().toUtc())) {
        throw const FormatException('照片确认已到期，尚未提交');
      }
      _notSubmitted = null;
      sent = true;
      final receipt = await api.change(
        ref,
        selector,
        approval,
        bytes: bytes,
        rights: action == 'SUBMIT' ? rights : null,
      );
      if (!_current(epoch, binding)) return;
      await _finish(receipt, unknown, epoch, binding);
    } catch (e) {
      if (_current(epoch, binding))
        setState(() {
          if (sent) {
            _message = '照片操作结果待核实，请先核实原操作。';
          } else if (_notSubmitted != null) {
            _storageFailed = true;
            _message = '尚未提交，无法确认本机记录。请检查本机记录。';
          } else {
            _message = _error(e);
          }
        });
    } finally {
      if (_current(epoch, binding))
        setState(() {
          _busy = false;
          _confirming = false;
        });
    }
  }

  Future<void> _finish(
    PlacePhotoReceipt receipt,
    PlacePhotoPending p,
    int epoch,
    Object binding,
  ) async {
    PlacePhotoEditorial? editorial;
    PlacePhotoState? state;
    Object? readFailure;
    try {
      editorial = await widget.api.editorial(widget.ref);
    } catch (e) {
      readFailure = e;
    }
    if (!_current(epoch, binding)) return;
    try {
      state = await widget.api.state(widget.ref);
    } catch (e) {
      readFailure = e;
    }
    if (!_current(epoch, binding)) return;
    if (state != null &&
        ((editorial != null && state.version != editorial.version) ||
            state.version < receipt.appliedVersion)) {
      readFailure = const FormatException('当前照片读取未通过版本检查');
      state = null;
    }
    if (state == null) editorial = null;
    // The exact original receipt is definitive even if today's source cannot
    // be read. A failed current read neither erases its effect nor grants a write.
    await _store.remove(_origin, widget.ownerID, widget.ref, p);
    if (!_current(epoch, binding)) return;
    setState(() {
      _pending = null;
      _notSubmitted = null;
      _storageFailed = false;
      _state = state;
      _sourceUnavailable = state == null;
      if (editorial == null) {
        _editorialTimer?.cancel();
        _editorial = null;
      } else {
        _setEditorial(editorial);
      }
      _draft = null;
      _message = receipt.status == 'STOPPED'
          ? '这次提交已停止；照片当前状态以重新读取结果为准。'
          : switch (p.selector.action) {
              'SUBMIT' => '照片已提交，等待其他审核者检查，尚未公开。',
              'APPROVE' => '原照片审核已完成；公开展示以当前读取结果为准。',
              'REJECT' => '原照片已拒绝，候选图片已移除。',
              _ => '原照片已撤回，展示以当前读取结果为准。',
            };
      if (readFailure != null) {
        _message = '${_message!}\n当前照片或管理资料暂不可读取，请重新读取；原操作不会再次提交。';
      }
    });
    widget.onChanged?.call();
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
          title: const Text('停止这次照片提交？'),
          content: const Text('只停止此原操作，操作也可能已经完成。不会自动撤回已提交或公开的照片；停止后重新读取当前结果。'),
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
      final receipt = stop
          ? await widget.api.stop(widget.ref, p.selector)
          : await widget.api.operation(widget.ref, p.selector);
      if (!_current(epoch, binding)) return;
      await _finish(receipt, p, epoch, binding);
    } catch (e) {
      if (_current(epoch, binding))
        setState(() {
          _message = e is PlacePhotoFailure && e.status == 404
              ? '尚未找到原操作结果，仍待核实；不会再次提交。'
              : _error(e);
        });
    } finally {
      if (_current(epoch, binding)) setState(() => _busy = false);
    }
  }

  String _previewTitle(String action) => switch (action) {
    'SUBMIT' => '检查待审核照片',
    'APPROVE' => '检查照片与公开权利',
    'REJECT' => '拒绝这张候选照片？',
    _ => '撤回这张照片？',
  };
  String _confirmLabel(String action) => switch (action) {
    'SUBMIT' => '确认提交审核',
    'APPROVE' => '确认权利并批准展示',
    'REJECT' => '确认拒绝',
    _ => '确认撤回',
  };
  String _consequence(String action) => switch (action) {
    'SUBMIT' => '保留照片供独立审核；提交不会立即公开。审核批准后会作为此地点的公开照片展示。照片不自动交给智能体分析。',
    'APPROVE' => '确认当前照片与权利资料后，照片将作为此地点的公开照片展示，并替换当前照片。贡献者不能审核自己的照片。',
    'REJECT' => '拒绝此候选并移除其图片。不会删除地点或重新提交照片。',
    _ => '移除此候选图片；若它是当前公开照片，也会停止公开展示。不会删除地点。',
  };
  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: birdtieAppBar(context, title: const Text('地点照片')),
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
                ? '原地点编辑已退出'
                : _sourceUnavailable
                ? '原地点当前资料不可读取'
                : widget.name,
            style: Theme.of(context).textTheme.titleLarge,
          ),
          if (!_retired) ...[
            EntityCover(
              metadata: _state?.placePhoto,
              api: widget.api.pixels,
              current: () => !_retired && widget.current(),
              identityChanges: widget.identityChanges,
              width: 320,
              height: 180,
              label: '当前公开地点照片',
            ),
            if (_editorial != null) ...[
              if (_editorial!.canContribute) ...[
                const SizedBox(height: 16),
                OutlinedButton.icon(
                  key: const Key('place-photo-select'),
                  onPressed:
                      _enabled && _editorial!.version < entityCoverMaxVersion
                      ? _select
                      : null,
                  icon: const Icon(Icons.photo_library_outlined),
                  label: const Text('选择地点照片'),
                ),
                if (_draft != null) ...[
                  const Text('本机草稿，尚未提交审核'),
                  Image.memory(
                    _draft!,
                    width: 320,
                    height: 180,
                    fit: BoxFit.contain,
                    semanticLabel: '本机地点照片草稿',
                  ),
                  DropdownButtonFormField<String>(
                    key: const Key('place-photo-rights-basis'),
                    initialValue: _basis,
                    isExpanded: true,
                    decoration: const InputDecoration(labelText: '图片权利依据'),
                    items: const [
                      DropdownMenuItem(value: 'OWNED', child: Text('本人拥有图片权利')),
                      DropdownMenuItem(value: 'LICENSED', child: Text('已获许可')),
                    ],
                    onChanged: _enabled
                        ? (v) => setState(() => _basis = v)
                        : null,
                  ),
                  TextField(
                    key: const Key('place-photo-attribution'),
                    controller: _attribution,
                    enabled: _enabled,
                    maxLength: 320,
                    decoration: const InputDecoration(labelText: '公开署名（如需）'),
                    onChanged: (_) => setState(() {}),
                  ),
                  if (_basis == 'LICENSED')
                    TextField(
                      key: const Key('place-photo-license'),
                      controller: _license,
                      enabled: _enabled,
                      maxLength: 1024,
                      decoration: const InputDecoration(labelText: '许可依据（必填）'),
                      onChanged: (_) => setState(() {}),
                    ),
                  TextField(
                    key: const Key('place-photo-retention'),
                    controller: _until,
                    enabled: _enabled,
                    keyboardType: TextInputType.datetime,
                    decoration: const InputDecoration(
                      labelText: '保留截止日期（YYYY-MM-DD）',
                    ),
                    onChanged: (_) => setState(() {}),
                  ),
                  CheckboxListTile(
                    key: const Key('place-photo-retain'),
                    contentPadding: EdgeInsets.zero,
                    value: _retain,
                    onChanged: _enabled
                        ? (v) => setState(() => _retain = v == true)
                        : null,
                    title: const Text('我有权让 Birdtie 保留至上述日期'),
                  ),
                  CheckboxListTile(
                    key: const Key('place-photo-publish'),
                    contentPadding: EdgeInsets.zero,
                    value: _publish,
                    onChanged: _enabled
                        ? (v) => setState(() => _publish = v == true)
                        : null,
                    title: const Text('我有权让审核批准后的照片公开展示'),
                  ),
                  FilledButton(
                    key: const Key('place-photo-preview'),
                    onPressed:
                        _enabled &&
                            _rights != null &&
                            _editorial!.version < entityCoverMaxVersion
                        ? () => _act('SUBMIT')
                        : null,
                    child: const Text('查看实际照片预览'),
                  ),
                  if (_enabled && _rights == null)
                    const Text('请选择权利依据和未来截止日期，并确认保留与公开展示权限。'),
                ],
              ],
              if (_editorial!.candidates.isEmpty)
                const Padding(
                  padding: EdgeInsets.only(top: 16),
                  child: Text('暂无可见的照片候选。'),
                ),
              for (final candidate in _editorial!.candidates)
                Padding(
                  key: ValueKey('place-photo-candidate-${candidate.id}'),
                  padding: const EdgeInsets.only(top: 16),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        candidate.statusLabel,
                        style: Theme.of(context).textTheme.titleSmall,
                      ),
                      if (candidate.rights.attribution.isNotEmpty)
                        Text(candidate.rights.attribution),
                      Wrap(
                        spacing: 8,
                        children: [
                          if (candidate.canApprove)
                            TextButton(
                              onPressed: _candidateAllowed(candidate, 'APPROVE')
                                  ? () => _act('APPROVE', candidate: candidate)
                                  : null,
                              child: const Text('检查并批准'),
                            ),
                          if (candidate.canReject)
                            TextButton(
                              onPressed: _candidateAllowed(candidate, 'REJECT')
                                  ? () => _act('REJECT', candidate: candidate)
                                  : null,
                              child: const Text('检查并拒绝'),
                            ),
                          if (candidate.canWithdraw)
                            TextButton(
                              onPressed:
                                  _candidateAllowed(candidate, 'WITHDRAW')
                                  ? () => _act('WITHDRAW', candidate: candidate)
                                  : null,
                              child: const Text('撤回照片'),
                            ),
                        ],
                      ),
                    ],
                  ),
                ),
            ],
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
                    _pending!.stage == 'PREPARED' ? '清除未提交准备记录' : '核实上次照片操作',
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
              child: const Text('重新读取照片与权限'),
            ),
        ],
      ),
    ),
  );
}
