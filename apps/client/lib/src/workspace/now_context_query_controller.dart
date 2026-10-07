import 'package:flutter/foundation.dart';
import 'now_context_query_api.dart';

/// Selection is a declared node, never a processing permit or membership proof.
class NowContextQueryController extends ChangeNotifier {
  NowContextQueryController({required this.api});
  final NowContextQueryApi api;
  List<NowOnlineContext> contexts = const [];
  NowOnlineContext? selected;
  bool loading = false;
  String? error;
  int _serial = 0;
  bool _disposed = false;
  Future<void> load() async {
    if (_disposed) return;
    final serial = ++_serial;
    loading = true;
    error = null;
    notifyListeners();
    try {
      final values = await api.contexts();
      if (_disposed || serial != _serial) return;
      contexts = values;
      if (selected != null && !values.any((x) => x.id == selected!.id)) {
        selected = null;
      }
    } catch (_) {
      if (_disposed || serial != _serial) return;
      contexts = const [];
      selected = null;
      error = '无法读取本人线上情境，请检查登录或稍后重试。';
    }
    loading = false;
    notifyListeners();
  }

  void select(NowOnlineContext? value) {
    if (_disposed) return;
    if (value != null &&
        !contexts.any((x) => x.id == value.id && x.label == value.label)) {
      throw const FormatException('Context is not current');
    }
    selected = value;
    notifyListeners();
  }

  void restoreContext(NowOnlineContext value) {
    if (_disposed) return;
    selected = value;
    notifyListeners();
  }

  void retire() {
    ++_serial;
    contexts = const [];
    selected = null;
    loading = false;
    error = null;
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    ++_serial;
    api.dispose();
    super.dispose();
  }
}
