import 'dart:async';
import 'dart:ui';

/// Only map chrome/layout is coalesced. Entity, permission and camera sources
/// keep their own immediate notification paths.
class NowMapInsetGate {
  NowMapInsetGate(
    this.onCommit, {
    this.settle = const Duration(milliseconds: 140),
  });
  final void Function(Rect) onCommit;
  final Duration settle;
  Timer? _timer;
  Rect? _value, _pending;
  double _inset = 0;
  bool _disposed = false;
  Rect? get value => _value;

  void offer(Rect rect, double keyboardInset) {
    if (_disposed) return;
    final changedInset = keyboardInset != _inset;
    _inset = keyboardInset;
    _pending = rect;
    if (changedInset) {
      _timer?.cancel();
      _timer = Timer(settle, () {
        _timer = null;
        _commit();
      });
    } else if (_timer == null) {
      _commit();
    }
  }

  void _commit() {
    if (_disposed || _pending == null || _pending == _value) return;
    _value = _pending;
    onCommit(_value!);
  }

  void dispose() {
    _disposed = true;
    _timer?.cancel();
    _pending = null;
  }
}
