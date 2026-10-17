import 'dart:async';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:birdtie_client/src/workspace/now_qa_bridge.dart';

void main() {
  final binding = TestWidgetsFlutterBinding.ensureInitialized();
  testWidgets('own-window port accepts only closed actions on current Now', (
    tester,
  ) async {
    var shown = 0, hidden = 0, current = true;
    const channel = MethodChannel('app.birdtie/qa/ui-test');
    final bridge = NowQaBridge(
      enabled: true,
      channel: channel,
      current: () => current,
      refreshRate: () => 60,
      showKeyboard: () => shown++,
      hideKeyboard: () => hidden++,
    );
    Future<Object?> call(String method) {
      final done = Completer<Object?>();
      binding.defaultBinaryMessenger.handlePlatformMessage(
        channel.name,
        const StandardMethodCodec().encodeMethodCall(MethodCall(method)),
        (data) =>
            done.complete(const StandardMethodCodec().decodeEnvelope(data!)),
      );
      return done.future;
    }

    expect(await call('keyboardShow'), true);
    expect(shown, 1);
    expect(await call('submit'), false);
    expect(await call('keyboardHide'), true);
    expect(hidden, 1);
    current = false;
    expect(await call('mapPanContinue'), false);
    expect(await call('keyboardShow'), false);
    expect(await call('mapPan'), false);
    expect(shown, 1);
    bridge.dispose();
  });
}
