import 'dart:async';
import 'dart:typed_data';
import 'package:flutter/foundation.dart';
import 'package:flutter/scheduler.dart';
import '../content/moment_control.dart' show newMomentOperationID;
import 'entity_cover.dart' show EntityCoverRights;
import 'human_causal_control.dart';
import 'public_moment_photo.dart';
import 'public_moment_photo_pending_store.dart';

class PublicMomentPhotoController extends ChangeNotifier {
  PublicMomentPhotoController({
    required this.momentID,
    required PublicMomentPhotoAPI api,
    this.ownerID,
    this.workspaceID,
    this.identityChanges,
    this.sourceKey,
    this.sourceCurrent,
    PublicMomentPhotoPendingStore? pendingStore,
  }) : transport = api,
       store = pendingStore ?? const SecurePublicMomentPhotoPendingStore() {
    _owner = ownerID?.call();
    _header = api.authorizationHeader();
    _key = sourceKey?.call();
    _workspace = workspaceID?.call();
    identityChanges?.addListener(observe);
  }
  final String momentID;
  final PublicMomentPhotoAPI transport;
  final PublicMomentPhotoPendingStore store;
  final String? Function()? ownerID, workspaceID;
  final Object? Function()? sourceKey;
  final bool Function()? sourceCurrent;
  final Listenable? identityChanges;
  String? _owner, _header, _lastOwner, _workspace;
  Object? _key;
  bool _closed = false, _retired = false, _notifierClosed = false;
  int _epoch = 0, _selection = 0, _dialogs = 0;
  Timer? _leaseTimer, _previewTimer;
  final _dialogTimers = <Timer>{};
  PublicMomentPhotoState? state;
  PublicMomentPhotoPreview? preview;
  PublicMomentPhotoReceipt? receipt;
  List<PublicMomentPhotoPending> pending = [];
  PublicMomentPhotoPending? _notSubmitted;
  Uint8List? localBytes;
  bool busy = false, journalReady = false, sourceUnavailable = false;
  String? error, notice;
  String? get owner => _owner;
  int get epoch => _epoch;
  PublicMomentPhotoAPI get api => PublicMomentPhotoAPI(
    client: transport.client,
    baseUrl: transport.baseUrl,
    authorizationHeader: transport.authorizationHeader,
    current: () => current,
  );
  bool get current {
    if (!_closed &&
        !_retired &&
        (ownerID != null && (_owner == null || ownerID!() != _owner) ||
            workspaceID?.call() != _workspace ||
            _workspace != null ||
            sourceKey?.call() != _key ||
            transport.authorizationHeader() == null)) {
      _retired = true;
      ++_epoch;
      _clearVisible(clearLocal: true);
      pending = [];
      receipt = null;
      busy = false;
      journalReady = false;
      error = '个人身份或原动态已变化。旧图片、权利与批准已移除，请返回当前入口。';
    }
    return !_closed && !_retired;
  }

  List<PublicMomentPhotoPending> get recoveryOperations => [
    ...pending,
    if (_notSubmitted != null && !pending.any((p) => p.same(_notSubmitted!)))
      _notSubmitted!,
  ];
  bool get canStart =>
      current &&
      sourceCurrent?.call() != false &&
      journalReady &&
      !busy &&
      recoveryOperations.isEmpty &&
      state?.current == true &&
      !sourceUnavailable;
  void _notify() {
    if (_notifierClosed) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      SchedulerBinding.instance.addPostFrameCallback((_) {
        if (!_notifierClosed) notifyListeners();
      });
    } else {
      notifyListeners();
    }
  }

  void _clearPreview() {
    _previewTimer?.cancel();
    preview = null;
  }

  void _clearVisible({bool clearLocal = false}) {
    _leaseTimer?.cancel();
    _clearPreview();
    state = null;
    if (clearLocal) {
      localBytes = null;
      ++_selection;
    }
  }

  void observe() {
    if (!current) {
      _notify();
      return;
    }
    if (_header != transport.authorizationHeader()) {
      _header = transport.authorizationHeader();
      ++_epoch;
      _clearVisible();
      busy = false;
      journalReady = false;
      if (ownerID == null) {
        _owner = null;
        pending = [];
      }
      notice = '登录已更新。本人选择和文字保留，旧批准已失效，请重读当前图片状态。';
      unawaited(load());
    }
    if (sourceCurrent?.call() == false) {
      ++_epoch;
      _clearVisible(clearLocal: true);
      sourceUnavailable = true;
    }
    _notify();
  }

  bool Function() capture() {
    observe();
    final e = _epoch, h = _header;
    return () => current && e == _epoch && transport.authorizationHeader() == h;
  }

  Future<bool> _resolve(bool Function() valid) async {
    final owner =
        ownerID?.call() ??
        await HumanCausalApi(
          client: transport.client,
          baseUrl: transport.baseUrl,
          authorizationHeader: transport.authorizationHeader,
        ).owner(valid);
    if (!valid() || !humanControlID(owner)) return false;
    if (_lastOwner != null && _lastOwner != owner) {
      _retired = true;
      ++_epoch;
      _clearVisible(clearLocal: true);
      pending = [];
      return false;
    }
    _owner = owner;
    _lastOwner = owner;
    return true;
  }

  Future<void> load() async {
    final valid = capture();
    if (!valid() || busy) return;
    busy = true;
    journalReady = false;
    error = null;
    _clearVisible();
    _notify();
    try {
      if (!await _resolve(valid)) return;
      final list = await store.read(api.origin, _owner!);
      if (!valid()) return;
      pending = list.where((p) => p.momentID == momentID).toList();
      journalReady = true;
      if (sourceCurrent?.call() == false) {
        sourceUnavailable = true;
        return;
      }
      final fresh = await api.state(momentID);
      if (!valid()) return;
      state = fresh;
      sourceUnavailable = false;
      if (!fresh.canSave) {
        localBytes = null;
        ++_selection;
      }
      _leaseTimer = Timer(
        fresh.expiresAt.difference(DateTime.now().toUtc()),
        () {
          if (identical(state, fresh)) {
            state = null;
            _clearPreview();
            sourceUnavailable = true;
            notice = '当前图片资料已到期，请重新读取；原操作仍可核实。';
            _notify();
          }
        },
      );
    } catch (_) {
      if (valid()) {
        _clearVisible(clearLocal: true);
        sourceUnavailable = true;
        error = journalReady
            ? '当前动态图片不可读取。旧图片和权利已移除，原操作仍可核实。'
            : '本机原图片操作记录暂不可读取，请先检查。';
      }
    } finally {
      if (valid()) {
        busy = false;
        _notify();
      }
    }
  }

  void select(Uint8List bytes) {
    if (!canStart || state?.canSave != true) return;
    try {
      publicMomentPhotoSelectedImage(bytes);
    } catch (_) {
      localBytes = null;
      _clearPreview();
      error = '请选择5MiB以内、单边不超过4096像素的静态JPEG或PNG。';
      _notify();
      return;
    }
    localBytes = Uint8List.fromList(bytes);
    ++_selection;
    _clearPreview();
    receipt = null;
    notice = null;
    error = null;
    _notify();
  }

  void draftChanged() {
    ++_selection;
    _clearPreview();
    _notify();
  }

  Future<PublicMomentPhotoPreview?> prepare({
    required String action,
    EntityCoverRights? rights,
  }) async {
    if (!canStart ||
        (action == 'SAVE'
            ? state?.canSave != true || localBytes == null
            : state?.canDelete != true))
      return null;
    final valid = capture(), snapshot = state!, selection = _selection;
    busy = true;
    error = null;
    notice = null;
    receipt = null;
    _clearPreview();
    _notify();
    try {
      final input = PublicMomentPhotoInput(
        momentID: momentID,
        operationID: newMomentOperationID(),
        action: action,
        expectedMomentRevision: snapshot.momentRevision,
        photoID: snapshot.photoID ?? newMomentOperationID(),
        expectedPhotoVersion: snapshot.photoVersion,
        bytes: action == 'SAVE' ? localBytes : null,
        rights: action == 'SAVE' ? rights : null,
      );
      final p = await api.preview(input, snapshot.expiresAt);
      if (action == 'SAVE' && p.sourceVersion != snapshot.sourceVersion)
        throw const FormatException('原公开动态来源已变化，请重新读取。');
      if (!valid() ||
          selection != _selection ||
          state != snapshot ||
          !snapshot.current ||
          sourceCurrent?.call() == false)
        return null;
      preview = p;
      _previewTimer = Timer(p.expiresAt.difference(DateTime.now().toUtc()), () {
        if (identical(preview, p)) {
          _clearPreview();
          notice = '这次图片确认已到期，旧批准已移除，请重新检查。';
          _notify();
        }
      });
      return p;
    } catch (_) {
      if (valid()) error = '当前动态、图片版本或权利已变化，未取得可提交的具体预览。';
      return null;
    } finally {
      if (valid()) {
        busy = false;
        _notify();
      }
    }
  }

  bool previewCurrent(PublicMomentPhotoPreview p, int epoch) =>
      current &&
      epoch == _epoch &&
      identical(preview, p) &&
      p.current &&
      state?.current == true &&
      sourceCurrent?.call() != false &&
      transport.authorizationHeader() == _header;
  void cancel(PublicMomentPhotoPreview p) {
    if (identical(preview, p)) {
      _clearPreview();
      _notify();
    }
  }

  Future<void> commit(
    PublicMomentPhotoPreview p,
    int epoch, {
    required bool painted,
  }) async {
    if (busy ||
        !canStart ||
        !previewCurrent(p, epoch) ||
        (p.input.action == 'SAVE' && !painted) ||
        _owner == null)
      return;
    bool valid() => previewCurrent(p, epoch);
    final owner = _owner!;
    var record = PublicMomentPhotoPending.preview(p), sent = false;
    busy = true;
    _notSubmitted = record;
    error = null;
    notice = null;
    _notify();
    try {
      await store.write(api.origin, owner, record);
      if (!valid()) return;
      var rows = await store.read(api.origin, owner);
      if (!valid()) return;
      if (!rows.any((v) => v.same(record) && v.stage == 'PREPARED'))
        throw StateError('原准备未读回');
      record = record.unknown;
      await store.write(api.origin, owner, record);
      if (!valid()) return;
      rows = await store.read(api.origin, owner);
      if (!valid()) return;
      if (!rows.any((v) => v.same(record) && v.stage == 'UNKNOWN'))
        throw StateError('原操作未读回');
      pending = rows.where((v) => v.momentID == momentID).toList();
      sent = true;
      _notSubmitted = null;
      final raw = await api.put(p);
      if (!valid()) return;
      final r = record.receipt(raw);
      await _finish(r, record, () => current && epoch == _epoch);
    } catch (_) {
      if (current) {
        error = sent
            ? '公开图片结果待核实，可能已完成。只能核实或停止原操作，不会重新上传。'
            : '尚未提交。本机准备记录需要检查，没有再次上传。';
      }
    } finally {
      if (current) {
        try {
          final rows = await store.read(api.origin, owner);
          if (current)
            pending = rows.where((p) => p.momentID == momentID).toList();
        } catch (_) {
          journalReady = false;
        }
        busy = false;
        _clearPreview();
        _notify();
      }
    }
  }

  bool locallyPrepared(PublicMomentPhotoPending p) =>
      p.stage == 'PREPARED' || _notSubmitted?.same(p) == true;
  Future<void> repair(PublicMomentPhotoPending p) async {
    final valid = capture();
    if (!valid() || busy || !locallyPrepared(p)) return;
    busy = true;
    _notify();
    try {
      if (!await _resolve(valid)) return;
      final rows = await store.read(api.origin, _owner!);
      if (!valid()) return;
      final old = rows.where((v) => v.same(p)).firstOrNull;
      if (old != null &&
          (old.stage == 'PREPARED' || _notSubmitted?.same(old) == true))
        await store.remove(api.origin, _owner!, old);
      if (!valid()) return;
      _notSubmitted = null;
      pending = (await store.read(
        api.origin,
        _owner!,
      )).where((v) => v.momentID == momentID).toList();
      if (!valid()) return;
      journalReady = true;
      error = null;
      notice = '未提交的本机准备已检查。请重读当前状态后重新选择或确认。';
    } catch (_) {
      if (valid()) error = '本机准备仍不可核实，没有再次上传。';
    } finally {
      if (valid()) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> recover(PublicMomentPhotoPending p, {bool stop = false}) async {
    if (locallyPrepared(p)) {
      await repair(p);
      return;
    }
    final valid = capture();
    if (!valid() || busy) return;
    busy = true;
    error = null;
    notice = null;
    _clearPreview();
    _notify();
    try {
      if (!await _resolve(valid)) return;
      final raw = await api.original(p.momentID, p.operationID, stop: stop);
      if (valid()) await _finish(p.receipt(raw), p, valid);
    } catch (_) {
      if (valid()) error = '原图片结果仍待核实；未找到回执不等于没有公开。仅核实或停止原操作，不会重放上传。';
    } finally {
      if (valid()) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> _finish(
    PublicMomentPhotoReceipt r,
    PublicMomentPhotoPending p,
    bool Function() valid,
  ) async {
    if (!valid()) return;
    receipt = r;
    if (r.succeeded && _owner != null)
      publicMomentPhotoChanges.value = PublicMomentPhotoLocalChange(
        ownerID: _owner!,
        origin: api.origin,
        momentID: momentID,
      );
    notice = r.succeeded
        ? '原公开图片操作已完成。当前图片需要重新读取。'
        : '原图片操作已停止，不会执行迟到上传；已完成的公开不会撤销。';
    error = null;
    try {
      await store.remove(api.origin, _owner!, p);
      if (!valid()) return;
      pending = (await store.read(
        api.origin,
        _owner!,
      )).where((v) => v.momentID == momentID).toList();
      if (!valid()) return;
      journalReady = true;
    } catch (_) {
      if (valid()) error = '原结果已核实，本机记录尚未清理，可再次核实。';
    }
    localBytes = null;
    ++_selection;
    _clearVisible();
    try {
      final fresh = await api.state(momentID);
      if (!valid()) return;
      state = fresh;
      sourceUnavailable = false;
      _leaseTimer = Timer(
        fresh.expiresAt.difference(DateTime.now().toUtc()),
        () {
          if (identical(state, fresh)) {
            state = null;
            sourceUnavailable = true;
            _notify();
          }
        },
      );
    } catch (_) {
      if (valid()) {
        _clearVisible(clearLocal: true);
        sourceUnavailable = true;
        error = '原结果已核实；当前图片不可读取，不能据此推断仍公开或再次上传。';
      }
    }
  }

  void retainDialog(Timer timer) {
    _dialogs++;
    _dialogTimers.add(timer);
  }

  void releaseDialog(Timer timer) {
    timer.cancel();
    _dialogTimers.remove(timer);
    _dialogs--;
    if (_closed && _dialogs == 0 && !_notifierClosed) {
      _notifierClosed = true;
      super.dispose();
    }
  }

  @override
  void dispose() {
    if (_closed) return;
    _closed = true;
    ++_epoch;
    _clearVisible(clearLocal: true);
    identityChanges?.removeListener(observe);
    for (final t in _dialogTimers) t.cancel();
    _notify();
    if (_dialogs == 0) {
      _notifierClosed = true;
      super.dispose();
    }
  }
}
