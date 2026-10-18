import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:flutter/foundation.dart';
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

  testWidgets(
    'mapObserve60 observes a full fixed window without UI or domain actions',
    (tester) async {
      var shown = 0,
          hidden = 0,
          refreshReads = 0,
          outgoing = 0,
          httpRequests = 0;
      const channel = MethodChannel('app.birdtie/qa/observe-test');
      final lines = <String>[];
      final previousDebugPrint = debugPrint;
      debugPrint = (message, {wrapWidth}) {
        if (message != null) lines.add(message);
      };
      binding.defaultBinaryMessenger.setMockMethodCallHandler(channel, (
        _,
      ) async {
        outgoing++;
        return null;
      });
      final bridge = NowQaBridge(
        enabled: true,
        channel: channel,
        current: () => true,
        refreshRate: () {
          refreshReads++;
          return 60;
        },
        showKeyboard: () => shown++,
        hideKeyboard: () => hidden++,
      );
      try {
        await HttpOverrides.runZoned(
          () async {
            final done = Completer<Object?>();
            binding.defaultBinaryMessenger.handlePlatformMessage(
              channel.name,
              const StandardMethodCodec().encodeMethodCall(
                const MethodCall('mapObserve60'),
              ),
              (data) => done.complete(
                const StandardMethodCodec().decodeEnvelope(data!),
              ),
            );
            expect(await done.future, true);
            await tester.pump(const Duration(seconds: 8));
            expect(lines, isEmpty);
            await tester.pump(const Duration(milliseconds: 51999));
            expect(lines, isEmpty);
            await tester.pump(const Duration(milliseconds: 1));
            expect(lines, isEmpty); // The existing bounded drain is unchanged.
            await tester.pump(const Duration(milliseconds: 1100));
          },
          createHttpClient: (_) {
            httpRequests++;
            throw StateError('Observation attempted a domain/network request');
          },
        );
        final payload = lines
            .map(
              (line) => RegExp(
                r'^BIRDTIE_NOW_PERF_V2 CHUNK [0-9a-f]{64} [0-9]+/[0-9]+ (.+)$',
              ).firstMatch(line)?.group(1),
            )
            .whereType<String>()
            .join();
        final report = jsonDecode(payload) as Map<String, dynamic>;
        expect(report['scene'], 'mapPan');
        expect(report['endReason'], 'elapsed');
        expect(report['nativeMapPresentTimingsIncluded'], false);
        expect(lines.where((line) => line.contains(' BEGIN ')), hasLength(1));
        expect(refreshReads, 1);
        expect(shown, 0);
        expect(hidden, 0);
        expect(outgoing, 0);
        expect(httpRequests, 0);
      } finally {
        bridge.dispose();
        debugPrint = previousDebugPrint;
        binding.defaultBinaryMessenger.setMockMethodCallHandler(channel, null);
      }
    },
  );

  testWidgets('mapObserve60 rejects noncurrent routes and all parameters', (
    tester,
  ) async {
    var current = false, refreshReads = 0, keyboardCalls = 0;
    const channel = MethodChannel('app.birdtie/qa/observe-guard-test');
    final bridge = NowQaBridge(
      enabled: true,
      channel: channel,
      current: () => current,
      refreshRate: () {
        refreshReads++;
        return 60;
      },
      showKeyboard: () => keyboardCalls++,
      hideKeyboard: () => keyboardCalls++,
    );
    Future<Object?> call(String method, [Object? arguments]) {
      final done = Completer<Object?>();
      binding.defaultBinaryMessenger.handlePlatformMessage(
        channel.name,
        const StandardMethodCodec().encodeMethodCall(
          MethodCall(method, arguments),
        ),
        (data) =>
            done.complete(const StandardMethodCodec().decodeEnvelope(data!)),
      );
      return done.future;
    }

    try {
      expect(await call('mapObserve60'), false);
      current = true;
      expect(await call('mapObserve60', {'cityID': 'not-accepted'}), false);
      expect(await call('mapObserve60', 'not-accepted'), false);
      expect(await call('mapObserve61'), false);
      expect(refreshReads, 0);
      expect(keyboardCalls, 0);
    } finally {
      bridge.dispose();
    }
  });

  testWidgets('disabled diagnostic bridge registers no observation action', (
    tester,
  ) async {
    var callbacks = 0;
    const channel = MethodChannel('app.birdtie/qa/observe-disabled-test');
    final bridge = NowQaBridge(
      enabled: false,
      channel: channel,
      current: () {
        callbacks++;
        return true;
      },
      refreshRate: () {
        callbacks++;
        return 60;
      },
      showKeyboard: () => callbacks++,
      hideKeyboard: () => callbacks++,
    );
    final done = Completer<Object?>();
    binding.defaultBinaryMessenger.handlePlatformMessage(
      channel.name,
      const StandardMethodCodec().encodeMethodCall(
        const MethodCall('mapObserve60'),
      ),
      done.complete,
    );
    expect(await done.future, isNull);
    expect(callbacks, 0);
    bridge.dispose();
  });
}
