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
    final observe = call.method == 'mapObserve60';
    // This fixed observation action accepts no location, text or other input.
    if (observe && call.arguments != null) return false;
    if (!const {
      'keyboardShow',
      'mapPan',
      'mapObserve60',
    }.contains(call.method)) {
      return false;
    }
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
      scene: call.method == 'mapPan' || observe
          ? NowPerfScene.mapPan
          : NowPerfScene.keyboard,
      refreshRateHz: refreshRate(),
      duration: Duration(seconds: observe ? 60 : 8),
    );
    if (call.method == 'keyboardShow') showKeyboard();
    return true;
  }

  void dispose() {
    if (_enabled) channel.setMethodCallHandler(null);
    _probe?.dispose();
  }
}
