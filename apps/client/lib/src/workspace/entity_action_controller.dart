import 'dart:async';
import 'package:flutter/widgets.dart';
import 'package:flutter/scheduler.dart';
import 'entity_action_contract.dart';
import 'entity_action_api.dart';

typedef EntityActionIdentity = (String?, String?, String?);

class EntityActionController extends ChangeNotifier {
  EntityActionController({
    required this.api,
    required this.ref,
    required this.identity,
    this.allowPublic = false,
  }) : captured = identity();
  final EntityActionApi api;
  final EntityActionRef ref;
  final EntityActionIdentity Function() identity;
  final EntityActionIdentity captured;
  final bool allowPublic;
  EntityActionView? view;
  String? error;
  bool busy = false, _retired = false, _disposed = false, _queued = false;
  int _epoch = 0, _serial = 0;
  Timer? _timer;
  bool get current =>
      !_disposed &&
      !_retired &&
      captured == identity() &&
      (captured.$1 != null ||
          (allowPublic &&
              captured.$2 == null &&
              const {'place', 'activity'}.contains(ref.type))) &&
      captured.$3 == null;
  bool _active(int s, int e) => current && s == _serial && e == _epoch;
  void _notify() {
    if (_disposed) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      if (_queued) return;
      _queued = true;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        _queued = false;
        if (!_disposed) notifyListeners();
      });
    } else {
      notifyListeners();
    }
  }

  void sync() {
    if (_disposed || _retired || captured == identity()) return;
    retire();
  }

  void retire() {
    if (_disposed || _retired) return;
    _retired = true;
    _epoch++;
    _serial++;
    _timer?.cancel();
    view = null;
    busy = false;
    error = '身份或连接已变化，请重新打开操作入口。';
    _notify();
  }

  void expire() {
    if (_disposed) return;
    if (view?.live(DateTime.now()) == false) {
      view = null;
      _serial++;
      busy = false;
      error = '操作来源已到期，请重新读取。';
      _notify();
    }
  }

  Future<EntityActionView?> load() async {
    sync();
    if (!current) return null;
    final s = ++_serial, e = _epoch;
    busy = true;
    error = null;
    _notify();
    try {
      final fresh = await api.read(captured.$1, ref);
      if (!_active(s, e)) return null;
      if (!fresh.live(DateTime.now())) throw const EntityActionFailure(409);
      view = fresh;
      _timer?.cancel();
      _timer = Timer(fresh.validUntil.difference(DateTime.now()), expire);
      return fresh;
    } catch (ex) {
      if (_active(s, e)) {
        view = null;
        error = ex is EntityActionFailure ? ex.message : '暂时无法核实可用操作，请重新读取。';
      }
      return null;
    } finally {
      if (_active(s, e)) {
        busy = false;
        _notify();
      }
    }
  }

  /// A read proposal cannot authorize a write. The original domain writer must
  /// still check the exact target/recipient/state after this source recheck.
  Future<EntityActionDescriptor?> recheck(
    EntityActionView expected,
    EntityActionKind kind,
  ) async {
    sync();
    expire();
    if (!current ||
        !identical(view, expected) ||
        !expected.live(DateTime.now())) {
      return null;
    }
    final fresh = await load();
    if (fresh == null ||
        !current ||
        fresh.sourceVersion != expected.sourceVersion ||
        fresh.title != expected.title ||
        !expected.live(DateTime.now())) {
      if (current) {
        error = '具体来源已变化，请检查当前内容后重新选择。';
        _notify();
      }
      return null;
    }
    final a = fresh.action(kind);
    return a.available ? a : null;
  }

  @override
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    _timer?.cancel();
    _serial++;
    super.dispose();
  }
}
