import 'package:flutter/foundation.dart';
import 'now_context_selection_api.dart';
import 'model_egress_api.dart' show egressIDValid;

class NowSelectionIdentity {
  const NowSelectionIdentity(this.auth, this.owner, this.workspace);
  final String? auth, owner, workspace;
  bool get personal =>
      auth != null && egressIDValid(owner) && workspace == null;
  bool same(NowSelectionIdentity b) =>
      auth == b.auth && owner == b.owner && workspace == b.workspace;
}

class NowContextSelectionController extends ChangeNotifier {
  NowContextSelectionController({
    required this.api,
    required this.identity,
    DateTime Function()? now,
  }) : now = now ?? DateTime.now,
       _bound = identity();
  final NowContextSelectionAPI api;
  final NowSelectionIdentity Function() identity;
  final DateTime Function() now;
  NowSelectionIdentity _bound;
  bool _closed = false, _retired = false, busy = false;
  int _serial = 0, generation = 0;
  NowContextOptions? options;
  NowContextOption? selected;
  String? message;
  bool get current =>
      !_closed && !_retired && _bound.personal && _bound.same(identity());
  bool get ready =>
      current && !busy && options?.expiresAt.isAfter(now()) == true;
  void _notify() {
    if (!_closed) notifyListeners();
  }

  void sync() {
    if (_bound.same(identity())) return;
    _retired = true;
    _bound = identity();
    _serial++;
    generation++;
    options = null;
    selected = null;
    busy = false;
    message = '账号或工作区已变化，请重新打开情境选择。';
    _notify();
  }

  void expire() {
    if (options?.expiresAt.isAfter(now()) == false) {
      options = null;
      selected = null;
      _serial++;
      generation++;
      busy = false;
      message = '本轮情境选项已到期，请刷新后重选。';
      _notify();
    }
  }

  bool _valid(int s, NowSelectionIdentity b) =>
      current && s == _serial && b.same(_bound);
  Future<void> load() async {
    sync();
    if (!current) return;
    final b = _bound, s = ++_serial;
    busy = true;
    options = null;
    selected = null;
    message = null;
    _notify();
    try {
      final v = await api.options(b.auth!, b.owner!);
      if (!_valid(s, b)) return;
      if (!v.expiresAt.isAfter(now())) throw const FormatException('情境选项到期');
      options = v;
    } catch (_) {
      if (_valid(s, b)) message = '无法读取当前情境，请检查登录或稍后刷新。';
    } finally {
      if (_valid(s, b)) {
        busy = false;
        _notify();
      }
    }
  }

  void select(NowContextOption v) {
    sync();
    expire();
    if (!ready || !options!.items.any((x) => x.same(v))) return;
    selected = v;
    message = null;
    _notify();
  }

  Future<NowContextChoice?> resolve() async {
    sync();
    expire();
    final o = options, v = selected;
    if (!ready || o == null || v == null) return null;
    final b = _bound, s = ++_serial;
    busy = true;
    message = null;
    _notify();
    try {
      final result = await api.resolve(b.auth!, b.owner!, o, v);
      if (!_valid(s, b) || !result.expiresAt.isAfter(now())) return null;
      return result;
    } catch (_) {
      if (_valid(s, b)) {
        options = null;
        selected = null;
        message = '选择尚未核验，请刷新后重选。未修改情境声明或发起查询。';
      }
      return null;
    } finally {
      if (_valid(s, b)) {
        busy = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    if (_closed) return;
    _closed = true;
    _serial++;
    api.dispose();
    super.dispose();
  }
}
