import 'dart:async';
import 'package:flutter/foundation.dart';
import 'package:flutter/scheduler.dart';
import 'entity_share_control.dart';
import 'entity_share_pending_store.dart';

/// Original receipt recovery is independent of current content visibility.
class EntityShareControlController extends ChangeNotifier {
  EntityShareControlController({
    required this.api,
    required this.store,
    this.ownerID,
    this.identityChanges,
    this.sourceKey,
    this.sourceCurrent,
    this.workspaceID,
  }) {
    _owner = ownerID?.call();
    _header = api.api.authorizationHeader();
    _key = sourceKey?.call();
    _workspace = workspaceID?.call();
    identityChanges?.addListener(observe);
  }
  final EntityShareControlAPI api;
  final EntitySharePendingStore store;
  final String? Function()? ownerID, workspaceID;
  final Object? Function()? sourceKey;
  final bool Function()? sourceCurrent;
  final Listenable? identityChanges;
  String? _owner, _header, _lastResolvedOwner, _workspace;
  Object? _key;
  bool _retired = false,
      _sourceRetired = false,
      _disposed = false,
      _notifierDisposed = false;
  int _epoch = 0, _dialogs = 0;
  Timer? _expiry;
  final _dialogTimers = <Timer>{};
  List<PendingEntityShare> pending = [];
  EntitySharePreview? preview;
  EntityShareReceipt? receipt;
  PendingEntityShare? _notSubmitted;
  String? error, notice;
  bool busy = false, storageReady = false;
  String? get owner => _owner;
  int get epoch => _epoch;
  bool get current {
    if (!_disposed &&
        !_retired &&
        (sourceKey?.call() != _key ||
            workspaceID?.call() != _workspace ||
            _workspace != null ||
            ownerID != null && (ownerID!() != _owner || _owner == null) ||
            api.api.authorizationHeader() == null)) {
      _retired = true;
      ++_epoch;
      _clearPreview();
      pending = [];
      receipt = null;
      storageReady = false;
      busy = false;
      error = '个人身份或来源已变化。旧资料和批准已移除，请返回当前入口。';
    }
    return !_disposed && !_retired;
  }

  bool get newSourceCurrent {
    if (sourceCurrent?.call() == false) _sourceRetired = true;
    return current && !_sourceRetired;
  }

  void _clearPreview() {
    _expiry?.cancel();
    preview = null;
  }

  void _notify() {
    if (_notifierDisposed) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      SchedulerBinding.instance.addPostFrameCallback((_) {
        if (!_notifierDisposed) notifyListeners();
      });
    } else {
      notifyListeners();
    }
  }

  void observe() {
    final wasRetired = _retired;
    if (!current) {
      if (!wasRetired) _notify();
      return;
    }
    var changed = false;
    if (_header != api.api.authorizationHeader()) {
      _header = api.api.authorizationHeader();
      ++_epoch;
      _clearPreview();
      busy = false;
      storageReady = false;
      if (ownerID == null) {
        _owner = null;
        pending = [];
      }
      error = '登录已更新。原恢复记录保留，旧批准已失效，请重新读取当前状态。';
      changed = true;
    }
    if (!newSourceCurrent && preview != null) {
      ++_epoch;
      _clearPreview();
      busy = false;
      changed = true;
    }
    if (changed) _notify();
  }

  bool Function() capture() {
    observe();
    final e = _epoch, h = _header;
    return () => current && e == _epoch && h == api.api.authorizationHeader();
  }

  Future<void> load() async {
    final valid = capture();
    if (!valid()) return;
    busy = true;
    storageReady = false;
    error = null;
    _clearPreview();
    _notify();
    try {
      final actual = await api.owner(valid);
      if (!valid()) return;
      if (_lastResolvedOwner != null && _lastResolvedOwner != actual ||
          ownerID != null && ownerID!() != actual) {
        _retired = true;
        ++_epoch;
        pending = [];
        _owner = null;
        throw StateError('个人账号已变化');
      }
      _owner = actual;
      _lastResolvedOwner = actual;
      final records = await store.read(api.origin, actual);
      if (!valid()) return;
      pending = records;
      storageReady = true;
    } catch (_) {
      if (valid()) error = '分享恢复记录或当前个人身份暂不可读取。请重试。';
    } finally {
      if (valid()) {
        busy = false;
        _notify();
      }
    }
  }

  Future<EntitySharePreview?> prepare(
    EntityShareInput input,
    String peer,
    Future<bool> Function() recipientCurrent,
  ) async {
    final valid = capture();
    if (!valid() ||
        !input.valid ||
        !newSourceCurrent ||
        !storageReady ||
        busy ||
        _owner == null)
      return null;
    if (pending.any(
      (p) => p.type == input.type && p.entityID == input.entityID,
    )) {
      error = '这张卡片有待核实的原分享。请先核实或停止原操作。';
      _notify();
      return null;
    }
    busy = true;
    notice = null;
    receipt = null;
    error = null;
    _clearPreview();
    _notify();
    try {
      if (!await recipientCurrent() || !valid() || !newSourceCurrent)
        throw StateError('收件人已变化');
      final p = await api.preview(input, peer, _owner!, valid);
      if (!valid() || !newSourceCurrent) return null;
      preview = p;
      _expiry = Timer(p.expiresAt.difference(DateTime.now().toUtc()), () {
        if (identical(preview, p)) {
          ++_epoch;
          _clearPreview();
          busy = false;
          error = '这次确认已到期，旧标题和批准已移除。请重新检查。';
          _notify();
        }
      });
      return p;
    } catch (_) {
      if (valid()) error = '当前收件人或内容已变化，暂不可取得具体分享预览。';
      return null;
    } finally {
      if (valid()) {
        busy = false;
        _notify();
      }
    }
  }

  bool approvedCurrent(EntitySharePreview p, int epoch) =>
      current &&
      newSourceCurrent &&
      epoch == _epoch &&
      identical(preview, p) &&
      p.live &&
      _header == api.api.authorizationHeader();
  Future<void> submit(
    EntitySharePreview p,
    int epoch,
    Future<bool> Function() recipientCurrent,
  ) async {
    if (busy || !approvedCurrent(p, epoch) || !storageReady || _owner == null)
      return;
    final valid = () => approvedCurrent(p, epoch);
    final owner = _owner!;
    var record = p.input.pending(p.sourceVersion, 'PREPARED');
    var sent = false;
    busy = true;
    notice = null;
    error = null;
    _notSubmitted = record;
    _notify();
    try {
      await store.write(api.origin, owner, record);
      if (!valid()) return;
      var found = await store.read(api.origin, owner);
      if (!valid()) return;
      if (!found.any((r) => r.sameSelector(record) && r.stage == 'PREPARED'))
        throw StateError('本机记录未核实');
      record = record.unknown;
      await store.write(api.origin, owner, record);
      if (!valid()) return;
      found = await store.read(api.origin, owner);
      if (!valid()) return;
      if (!found.any((r) => r.sameSelector(record) && r.stage == 'UNKNOWN'))
        throw StateError('本机记录未核实');
      pending = found;
      if (!await recipientCurrent() || !valid()) throw StateError('当前收件人已变化');
      sent = true;
      _notSubmitted = null;
      final result = await api.submit(p, record, valid);
      if (!valid()) return;
      await _finish(result, record, valid);
    } catch (_) {
      if (current) {
        try {
          final found = await store.read(api.origin, owner);
          if (current) pending = found;
        } catch (_) {
          storageReady = false;
        }
        if (current)
          error = sent
              ? '分享结果待核实，可能已发送。请核实原结果，不要重新发送。'
              : '尚未发起提交。本机准备记录需要检查，不能直接重新发送。';
      }
    } finally {
      if (current && _epoch == epoch) {
        busy = false;
        _clearPreview();
        _notify();
      }
    }
  }

  bool locallyPrepared(PendingEntityShare p) =>
      !p.legacy &&
      (p.stage == 'PREPARED' || _notSubmitted?.sameSelector(p) == true);
  Future<void> repair(PendingEntityShare p) async {
    final valid = capture();
    if (!valid() || busy || _owner == null || !locallyPrepared(p)) return;
    busy = true;
    _notify();
    try {
      final records = await store.read(api.origin, _owner!);
      if (!valid()) return;
      final found = records.where((r) => r.sameSelector(p)).firstOrNull;
      if (found != null &&
          (found.stage == 'PREPARED' ||
              _notSubmitted?.sameSelector(found) == true))
        await store.remove(api.origin, _owner!, found);
      if (!valid()) return;
      pending = await store.read(api.origin, _owner!);
      if (!valid()) return;
      storageReady = true;
      _notSubmitted = null;
      notice = '已检查尚未提交的本机准备记录。可重新取得当前预览。';
      error = null;
    } catch (_) {
      if (valid()) error = '本机记录仍不可核实，请稍后检查；没有再次提交。';
    } finally {
      if (valid()) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> recover(PendingEntityShare p, {bool stop = false}) async {
    final valid = capture();
    if (!valid() || busy || _owner == null) return;
    if (locallyPrepared(p)) {
      await repair(p);
      return;
    }
    busy = true;
    error = null;
    notice = null;
    _clearPreview();
    _notify();
    try {
      final result = await api.recover(p, valid, stop: stop);
      if (valid()) await _finish(result, p, valid);
    } catch (_) {
      if (valid()) error = '原操作仍待核实；未找到回执不等于未发送。只能核实或停止原操作，不会再次提交。';
    } finally {
      if (valid()) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> _finish(
    EntityShareReceipt result,
    PendingEntityShare p,
    bool Function() valid,
  ) async {
    // An immutable receipt remains true even if current conversation/content reads fail.
    receipt = result;
    notice = result.succeeded
        ? '已发送到私信。可在消息中找回。'
        : '原操作已停止，不会再发送；已经成功的分享不会被撤销。';
    error = null;
    try {
      await store.remove(api.origin, _owner!, p);
      if (!valid()) return;
      pending = await store.read(api.origin, _owner!);
      if (!valid()) return;
      storageReady = true;
    } catch (_) {
      if (valid()) error = '原结果已核实，本机恢复记录尚未清理。可再次核实原结果。';
    }
  }

  void cancelPreview(EntitySharePreview p) {
    if (identical(preview, p)) {
      ++_epoch;
      _clearPreview();
      _notify();
    }
  }

  void retainDialog(Timer timer) {
    _dialogs++;
    _dialogTimers.add(timer);
  }

  void releaseDialog(Timer timer) {
    timer.cancel();
    if (!_dialogTimers.remove(timer)) return;
    _dialogs--;
    if (_disposed && _dialogs == 0 && !_notifierDisposed) {
      _notifierDisposed = true;
      super.dispose();
    }
  }

  @override
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    ++_epoch;
    _clearPreview();
    identityChanges?.removeListener(observe);
    for (final timer in _dialogTimers) {
      timer.cancel();
    }
    _notify();
    if (_dialogs == 0) {
      _notifierDisposed = true;
      super.dispose();
    }
  }
}
