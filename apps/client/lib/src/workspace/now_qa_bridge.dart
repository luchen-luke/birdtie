import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'now_perf_probe.dart';

/// An explicit profile-only own-window test port. No text, entity, credentials
/// or domain write can be supplied. It never starts a measurement by itself.
class NowQaBridge {
  NowQaBridge({
    required this.showKeyboard,
    required this.hideKeyboard,
    required this.current,
    required this.refreshRate,
    bool enabled = kProfileMode,
    this.channel = const MethodChannel('app.birdtie/qa/ui'),
  }) : _enabled = enabled {
    if (_enabled) channel.setMethodCallHandler(_handle);
  }
  final VoidCallback showKeyboard, hideKeyboard;
  final bool Function() current;
  final double Function() refreshRate;
  final bool _enabled;
  final MethodChannel channel;
  NowPerfProbe? _probe;
  Future<bool> _handle(MethodCall call) async {
    if (!_enabled || !current()) return false;
    if (call.method == 'mapPanContinue') return true;
    if (call.method == 'keyboardHide') {
      hideKeyboard();
      return true;
    }
    if (!const {'keyboardShow', 'mapPan'}.contains(call.method)) return false;
    _probe?.dispose();
    _probe = NowPerfProbe(
      enabled: true,
      onReport: (report) {
        for (final line in report.toQaLogLines()) {
          debugPrint(line);
        }
      },
    );
    _probe!.beginWindow(
      scene: call.method == 'mapPan'
          ? NowPerfScene.mapPan
          : NowPerfScene.keyboard,
      refreshRateHz: refreshRate(),
      duration: const Duration(seconds: 8),
    );
    if (call.method == 'keyboardShow') showKeyboard();
    return true;
  }

  void dispose() {
    if (_enabled) channel.setMethodCallHandler(null);
    _probe?.dispose();
  }
}
