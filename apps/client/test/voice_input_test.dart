import 'dart:async';

import 'package:birdtie_client/src/workspace/voice_input.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';

const _system = VoiceSystemService(id: 'com.example/.Speech', name: '测试系统识别服务');

class _Session {
  _Session(this.id, this.onDevice, this.approved, this.phase);
  final String id;
  final bool onDevice, approved;
  final ValueChanged<VoiceInputPhase> phase;
  final result = Completer<String>();
}

class _Port extends VoiceInputPort {
  VoiceAvailability found = const VoiceAvailability(
    onDevice: true,
    systemService: _system,
  );
  bool ready = true, granted = true;
  int permissions = 0, probes = 0, localChecks = 0;
  Completer<bool>? pendingPermission, pendingLocalReady;
  Completer<VoiceAvailability>? pendingAvailability;
  final sessions = <_Session>[];
  final stops = <String>[], cancellations = <String>[];
  @override
  Future<VoiceAvailability> availability() async {
    probes++;
    return pendingAvailability?.future ?? found;
  }

  @override
  Future<bool> requestPermission(String requestId) {
    permissions++;
    return pendingPermission?.future ?? Future.value(granted);
  }

  @override
  Future<bool> localLanguageReady(String requestId, String language) async {
    localChecks++;
    return pendingLocalReady?.future ?? ready;
  }

  @override
  Future<String> recognize({
    required String requestId,
    required String language,
    required bool onDevice,
    required VoiceSystemService? systemService,
    required bool systemApproved,
    required ValueChanged<VoiceInputPhase> onPhase,
  }) {
    final session = _Session(requestId, onDevice, systemApproved, onPhase);
    sessions.add(session);
    onPhase(VoiceInputPhase.listening);
    return session.result.future;
  }

  @override
  Future<void> stop(String requestId) async {
    stops.add(requestId);
  }

  @override
  Future<void> cancel(String requestId) async {
    cancellations.add(requestId);
  }

  @override
  void dispose() {
    for (final s in sessions) {
      if (!s.result.isCompleted) s.result.complete('late');
    }
  }
}

Future<void> _tick() async {
  for (var n = 0; n < 12; n++) {
    await Future<void>.value();
  }
}

Future<Object?> _nativeLease(MethodChannel channel, String requestId) async {
  final reply = Completer<Object?>();
  TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
      .handlePlatformMessage(
        channel.name,
        const StandardMethodCodec().encodeMethodCall(
          MethodCall('externalLease', {'requestId': requestId}),
        ),
        (data) => reply.complete(
          data == null
              ? null
              : const StandardMethodCodec().decodeEnvelope(data),
        ),
      );
  return reply.future;
}

Map<String, Object?> _diagnostic({
  String requestId = 'voice-1-1',
  String stage = 'recognition',
  String route = 'system',
  String reason = 'audio',
  int? androidError = 3,
}) => {
  'schemaVersion': VoiceInputDiagnostic.schema,
  'requestId': requestId,
  'stage': stage,
  'route': route,
  'reasonClass': reason,
  'androidError': androidError,
};

Future<VoiceInputFailure> _recognitionFailure(NativeVoiceInputPort port) async {
  try {
    await port.recognize(
      requestId: 'voice-1-1',
      language: 'auto',
      onDevice: false,
      systemService: _system,
      systemApproved: true,
      onPhase: (_) {},
    );
  } on VoiceInputFailure catch (failure) {
    return failure;
  }
  fail('The mocked native failure must reject recognition.');
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'bundled capability has a fixed notice identity, separate from network selection',
    () {
      const found = VoiceAvailability(
        bundledOffline: true,
        systemService: _system,
      );
      expect(found.onDevice, isTrue);
      expect(found.bundledEngine, isA<VoiceBundledEngine>());
      expect(found.bundledEngine!.id, 'birdtie.bundled.whisper.base-q5_1.v1');
      expect(found.preferredSystem, same(_system));
      expect(
        const VoiceAvailability(
          systemService: VoiceBundledEngine(),
        ).preferredSystem,
        isNull,
      );
    },
  );
  test(
    'native bundled marker is optional but strictly typed and requires local capability',
    () async {
      const channel = MethodChannel('voice-bundled-capability');
      Object? payload = {
        'onDevice': true,
        'systemService': null,
        'bundledOffline': true,
      };
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (_) async => payload);
      final port = NativeVoiceInputPort(
        channel: channel,
        supported: () => true,
      );
      addTearDown(port.dispose);
      var found = await port.availability();
      expect(found.bundledOffline, isTrue);
      expect(found.bundledEngine!.name, '本机 Whisper 语音识别');
      for (final invalid in [
        {'onDevice': true, 'systemService': null, 'bundledOffline': 'true'},
        {'onDevice': false, 'systemService': null, 'bundledOffline': true},
        {
          'onDevice': true,
          'systemService': null,
          'bundledOffline': true,
          'modelPath': '/private',
        },
      ]) {
        payload = invalid;
        found = await port.availability();
        expect(found.available, isFalse);
        expect(found.bundledOffline, isFalse);
      }
      payload = {'onDevice': true, 'systemService': null};
      found = await port.availability();
      expect(found.onDevice, isTrue);
      expect(found.bundledOffline, isFalse);
      expect(found.bundledEngine, isNull);
    },
  );
  testWidgets(
    'bundled native and controller support wait covers real preparation and recognition passes old 45s',
    (t) async {
      const channel = MethodChannel('voice-bundled-budget');
      final support = Completer<Object?>(), recognition = Completer<Object?>();
      final calls = <MethodCall>[];
      String? id;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (call) async {
            calls.add(call);
            if (call.method == 'availability')
              return {
                'onDevice': true,
                'systemService': null,
                'bundledOffline': true,
              };
            if (call.method == 'permission') {
              id = (call.arguments as Map)['requestId'];
              return {'requestId': id, 'granted': true};
            }
            if (call.method == 'localSupport') return support.future;
            if (call.method == 'recognize') return recognition.future;
            if (call.method == 'cancel') return null;
            fail('Unexpected bundled method ${call.method}');
          });
      final port = NativeVoiceInputPort(
        channel: channel,
        supported: () => true,
      );
      final owner = VoiceInputController(port: port);
      var systemPrompts = 0;
      final result = owner.capture(
        current: () => true,
        choose: (_) async => const VoiceInputChoice(),
        confirmSystem: (_) async {
          systemPrompts++;
          return true;
        },
      );
      await t.pump();
      expect(owner.phase, VoiceInputPhase.preparing);
      await t.pump(const Duration(seconds: 10));
      expect(owner.active, isTrue);
      expect(calls.where((c) => c.method == 'recognize'), isEmpty);
      support.complete({'requestId': id, 'ready': true});
      await t.pump();
      final request =
          calls.singleWhere((c) => c.method == 'recognize').arguments as Map;
      expect(request.keys.toSet(), {
        'requestId',
        'language',
        'onDevice',
        'serviceId',
        'serviceName',
        'systemApproved',
      });
      expect(request['onDevice'], isTrue);
      expect(request['language'], 'auto');
      expect(request['serviceId'], isNull);
      expect(request['systemApproved'], isFalse);
      await t.pump(const Duration(seconds: 46));
      expect(owner.active, isTrue);
      expect(owner.message, isNull);
      recognition.complete({'requestId': id, 'text': '完整的本机合成转写'});
      await t.pump();
      expect(await result, '完整的本机合成转写');
      expect(systemPrompts, 0);
      expect(calls.where((c) => c.method == 'recognize'), hasLength(1));
      owner.dispose();
      await t.pump();
    },
  );
  testWidgets(
    'bundled explicit short watchdog overrides default 105s without retry',
    (t) async {
      final port = _Port()
        ..found = const VoiceAvailability(
          bundledOffline: true,
          systemService: _system,
        );
      final owner = VoiceInputController(
        port: port,
        sessionTimeout: const Duration(seconds: 2),
      );
      final result = owner.capture(
        current: () => true,
        choose: (_) async => const VoiceInputChoice(),
        confirmSystem: (_) async => true,
      );
      await t.pump();
      expect(port.sessions, hasLength(1));
      await t.pump(const Duration(seconds: 3));
      expect(await result, isNull);
      expect(owner.message, contains('超时'));
      expect(port.sessions, hasLength(1));
      expect(port.cancellations, contains(port.sessions.single.id));
      owner.dispose();
      await t.pump();
    },
  );
  testWidgets(
    'bundled default recognition remains bounded at 105s and retires late text',
    (t) async {
      final port = _Port()
        ..found = const VoiceAvailability(bundledOffline: true);
      final owner = VoiceInputController(port: port);
      final result = owner.capture(
        current: () => true,
        choose: (_) async => const VoiceInputChoice(),
        confirmSystem: (_) async => false,
      );
      await t.pump();
      await t.pump(const Duration(seconds: 104));
      expect(owner.active, isTrue);
      await t.pump(const Duration(seconds: 2));
      expect(await result, isNull);
      expect(owner.message, contains('超时'));
      expect(port.sessions, hasLength(1));
      expect(port.cancellations, contains(port.sessions.single.id));
      port.sessions.single.result.complete('超过期限的文字');
      await t.pump();
      expect(owner.phase, VoiceInputPhase.idle);
      owner.dispose();
      await t.pump();
    },
  );
  testWidgets(
    'invalid capability retires cached bundled budget and native legacy still times out at 45s',
    (t) async {
      const channel = MethodChannel('voice-bundled-cache-retirement');
      var valid = true;
      final pending = Completer<Object?>();
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (call) async {
            if (call.method == 'availability')
              return valid
                  ? {
                      'onDevice': true,
                      'systemService': null,
                      'bundledOffline': true,
                    }
                  : {
                      'onDevice': true,
                      'systemService': null,
                      'bundledOffline': 'bad',
                    };
            if (call.method == 'recognize') return pending.future;
            return null;
          });
      final port = NativeVoiceInputPort(
        channel: channel,
        supported: () => true,
      );
      expect((await port.availability()).bundledOffline, isTrue);
      valid = false;
      expect((await port.availability()).available, isFalse);
      final rejected = expectLater(
        port.recognize(
          requestId: 'voice-cache',
          language: 'auto',
          onDevice: true,
          systemService: null,
          systemApproved: false,
          onPhase: (_) {},
        ),
        throwsA(isA<TimeoutException>()),
      );
      await t.pump();
      await t.pump(const Duration(seconds: 46));
      await rejected;
      pending.complete({'requestId': 'voice-cache', 'text': '不能回填的晚结果'});
      port.dispose();
      await t.pump();
    },
  );
  testWidgets(
    'native bundled explicit short watchdog also wins over cached marker',
    (t) async {
      const channel = MethodChannel('voice-bundled-native-short');
      final pending = Completer<Object?>();
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(
            channel,
            (call) async => call.method == 'availability'
                ? {
                    'onDevice': true,
                    'systemService': null,
                    'bundledOffline': true,
                  }
                : call.method == 'recognize'
                ? pending.future
                : null,
          );
      final port = NativeVoiceInputPort(
        channel: channel,
        supported: () => true,
        sessionTimeout: const Duration(seconds: 2),
      );
      expect((await port.availability()).bundledOffline, isTrue);
      final rejected = expectLater(
        port.recognize(
          requestId: 'voice-short',
          language: 'auto',
          onDevice: true,
          systemService: null,
          systemApproved: false,
          onPhase: (_) {},
        ),
        throwsA(isA<TimeoutException>()),
      );
      await t.pump();
      await t.pump(const Duration(seconds: 3));
      await rejected;
      pending.complete({'requestId': 'voice-short', 'text': '晚结果'});
      port.dispose();
      await t.pump();
    },
  );
  test(
    'missing bundled model never asks network consent or opens another recognizer',
    () async {
      final port = _Port()
        ..found = const VoiceAvailability(
          bundledOffline: true,
          systemService: _system,
          systemActivity: VoiceSystemActivity(
            id: 'com.example/.SpeechActivity',
            name: '系统界面',
          ),
        )
        ..ready = false;
      final owner = VoiceInputController(port: port);
      var systemPrompts = 0;
      expect(
        await owner.capture(
          current: () => true,
          choose: (_) async => const VoiceInputChoice(),
          confirmSystem: (_) async {
            systemPrompts++;
            return true;
          },
        ),
        isNull,
      );
      expect(systemPrompts, 0);
      expect(port.sessions, isEmpty);
      expect(owner.message, '本机语音模型未准备好，原草稿已保留，请使用键盘。');
      owner.dispose();
    },
  );
  test(
    'bundled recognition failure keeps original route and never automatically invokes a system fallback',
    () async {
      final port = _Port()
        ..found = const VoiceAvailability(
          bundledOffline: true,
          systemService: _system,
        );
      final owner = VoiceInputController(port: port);
      var systemPrompts = 0;
      final result = owner.capture(
        current: () => true,
        choose: (_) async => const VoiceInputChoice(),
        confirmSystem: (_) async {
          systemPrompts++;
          return true;
        },
      );
      await _tick();
      expect(port.sessions.single.onDevice, isTrue);
      expect(port.sessions.single.approved, isFalse);
      port.sessions.single.result.completeError(
        const VoiceInputFailure('本机处理未完成，请使用键盘。'),
      );
      expect(await result, isNull);
      expect(owner.message, '本机处理未完成，请使用键盘。');
      expect(systemPrompts, 0);
      expect(port.sessions, hasLength(1));
      owner.dispose();
    },
  );
  test(
    'system Activity capability is a separate controlled public component',
    () async {
      const channel = MethodChannel('voice-activity-capability');
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(
            channel,
            (_) async => {
              'onDevice': false,
              'systemService': {'id': _system.id, 'name': _system.name},
              'systemActivity': {
                'id': 'com.example/.SpeechActivity',
                'name': '测试系统语音界面',
              },
            },
          );
      final port = NativeVoiceInputPort(
        channel: channel,
        supported: () => true,
      );
      addTearDown(port.dispose);
      final found = await port.availability();
      expect(found.available, isTrue);
      final dynamic capability = found;
      expect(capability.systemActivity.id, 'com.example/.SpeechActivity');
      expect(capability.systemActivity.name, '测试系统语音界面');
      expect(found.onDevice, isFalse);
    },
  );
  test(
    'system Activity result number has its own closed diagnostic domain',
    () {
      final diagnostic = VoiceInputDiagnostic.tryDecode(
        {
          'schemaVersion': 'voice.activity_failure.v1',
          'requestId': 'voice-1-1',
          'stage': 'recognition',
          'route': 'system_activity',
          'reasonClass': 'network',
          'activityResultCode': 4,
        },
        requestId: 'voice-1-1',
        method: 'recognizeActivity',
      );
      expect(diagnostic, isNotNull);
      expect(diagnostic!.reason, VoiceFailureReason.network);
      expect(diagnostic.androidError, isNull);
      final dynamic typed = diagnostic;
      expect(typed.activityResultCode, 4);
      expect(diagnostic.toSafeMap().containsKey('androidError'), isFalse);
    },
  );
  testWidgets('system Activity owns expected pause and fills only after resume', (
    t,
  ) async {
    const channel = MethodChannel('voice-activity-owned-lifecycle');
    final calls = <MethodCall>[];
    final pending = Completer<Object?>();
    String? id;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
          calls.add(call);
          if (call.method == 'availability') {
            return {
              'onDevice': false,
              'systemService': null,
              'systemActivity': {
                'id': 'com.example/.SpeechActivity',
                'name': '测试系统语音界面',
              },
            };
          }
          if (call.method == 'recognizeActivity') {
            id = (call.arguments as Map)['requestId'];
            return pending.future;
          }
          if (call.method == 'cancel') return null;
          fail(
            'Activity must not request app recording permission: ${call.method}',
          );
        });
    final owner = VoiceInputController(
      port: NativeVoiceInputPort(channel: channel, supported: () => true),
    );
    addTearDown(owner.dispose);
    addTearDown(
      () => t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed),
    );
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    var completed = false;
    final result = owner.capture(
      current: () => true,
      choose: (_) async => const VoiceInputChoice(systemApproved: true),
      confirmSystem: (_) async => false,
    )..then((_) => completed = true);
    await t.pump();
    expect(owner.phase.name, 'awaitingSystemUi');
    expect(owner.recording, isFalse);
    expect(await _nativeLease(channel, id!), {
      'requestId': id,
      'accepted': true,
    });
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
    await t.pump();
    expect(owner.active, isTrue);
    expect(calls.where((c) => c.method == 'cancel'), isEmpty);
    pending.complete({'requestId': id, 'text': '附近 cafe 的可编辑合成结果'});
    await t.pump();
    expect(completed, isFalse);
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    await t.pump();
    expect(await result, '附近 cafe 的可编辑合成结果');
    expect(owner.phase, VoiceInputPhase.idle);
    expect(calls.where((c) => c.method == 'recognizeActivity'), hasLength(1));
  });
  testWidgets(
    'system Activity awaiting without a native launch lease still cancels on background',
    (t) async {
      const channel = MethodChannel('voice-activity-no-lease');
      final pending = Completer<Object?>();
      String? id;
      final calls = <String>[];
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (call) async {
            calls.add(call.method);
            if (call.method == 'availability') {
              return {
                'onDevice': false,
                'systemService': null,
                'systemActivity': {
                  'id': 'com.example/.SpeechActivity',
                  'name': '系统语音界面',
                },
              };
            }
            if (call.method == 'recognizeActivity') {
              id = (call.arguments as Map)['requestId'];
              return pending.future;
            }
            if (call.method == 'cancel') return null;
            fail('Unrequested native operation ${call.method}');
          });
      final owner = VoiceInputController(
        port: NativeVoiceInputPort(channel: channel, supported: () => true),
      );
      addTearDown(owner.dispose);
      addTearDown(
        () =>
            t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed),
      );
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      final result = owner.capture(
        current: () => true,
        choose: (_) async => const VoiceInputChoice(systemApproved: true),
        confirmSystem: (_) async => false,
      );
      await t.pump();
      expect(owner.phase, VoiceInputPhase.awaitingSystemUi);
      expect(await _nativeLease(channel, 'voice-999-999'), isNull);
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
      await t.pump();
      expect(await result, isNull);
      expect(calls, contains('cancel'));
      pending.complete({'requestId': id, 'text': '后台旧结果'});
      await t.pump();
      expect(owner.active, isFalse);
    },
  );
  testWidgets(
    'system Activity account ABA cancels its lease and never adopts late text',
    (t) async {
      const channel = MethodChannel('voice-activity-aba');
      final pending = Completer<Object?>();
      String? id;
      final cancellations = <String>[];
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (call) async {
            if (call.method == 'availability') {
              return {
                'onDevice': false,
                'systemService': null,
                'systemActivity': {
                  'id': 'com.example/.SpeechActivity',
                  'name': '系统语音界面',
                },
              };
            }
            if (call.method == 'recognizeActivity') {
              id = (call.arguments as Map)['requestId'];
              return pending.future;
            }
            if (call.method == 'cancel') {
              cancellations.add((call.arguments as Map)['requestId']);
              return null;
            }
            fail('Unexpected operation ${call.method}');
          });
      final owner = VoiceInputController(
        port: NativeVoiceInputPort(channel: channel, supported: () => true),
      );
      addTearDown(owner.dispose);
      var epoch = 1;
      final original = epoch;
      final result = owner.capture(
        current: () => epoch == original,
        choose: (_) async => const VoiceInputChoice(systemApproved: true),
        confirmSystem: (_) async => false,
      );
      await t.pump();
      expect(await _nativeLease(channel, id!), {
        'requestId': id,
        'accepted': true,
      });
      epoch++;
      owner.cancel();
      epoch++;
      await t.pump();
      expect(await result, isNull);
      expect(await _nativeLease(channel, id!), isNull);
      pending.complete({'requestId': id, 'text': '前一账号私密合成文字'});
      await t.pump();
      expect(cancellations, contains(id));
      expect(owner.lastDiagnostic, isNull);
    },
  );
  for (final invalid in <String, Object?>{
    'cross-domain android number': {
      'schemaVersion': 'voice.activity_failure.v1',
      'requestId': 'voice-1-1',
      'stage': 'recognition',
      'route': 'system_activity',
      'reasonClass': 'network',
      'androidError': 4,
    },
    'wrong result class': {
      'schemaVersion': 'voice.activity_failure.v1',
      'requestId': 'voice-1-1',
      'stage': 'recognition',
      'route': 'system_activity',
      'reasonClass': 'service',
      'activityResultCode': 4,
    },
    'retired request': {
      'schemaVersion': 'voice.activity_failure.v1',
      'requestId': 'voice-1-2',
      'stage': 'recognition',
      'route': 'system_activity',
      'reasonClass': 'network',
      'activityResultCode': 4,
    },
    'unknown number': {
      'schemaVersion': 'voice.activity_failure.v1',
      'requestId': 'voice-1-1',
      'stage': 'recognition',
      'route': 'system_activity',
      'reasonClass': 'network',
      'activityResultCode': 999,
    },
    'unknown details': {
      'schemaVersion': 'voice.activity_failure.v1',
      'requestId': 'voice-1-1',
      'stage': 'recognition',
      'route': 'system_activity',
      'reasonClass': 'network',
      'activityResultCode': 4,
      'text': 'PRIVATE_SENTINEL',
    },
  }.entries) {
    test('system Activity rejects ${invalid.key} diagnostic', () {
      expect(
        VoiceInputDiagnostic.tryDecode(
          invalid.value,
          requestId: 'voice-1-1',
          method: 'recognizeActivity',
        ),
        isNull,
      );
    });
  }
  for (final result in <String, Object?>{
    'wrong request': {'requestId': 'voice-1-2', 'text': '合成结果'},
    'empty': {'requestId': 'voice-1-1', 'text': '  '},
    'too long': {
      'requestId': 'voice-1-1',
      'text': List.filled(4097, 'a').join(),
    },
    'private extra': {
      'requestId': 'voice-1-1',
      'text': '合成结果',
      'audio': 'PRIVATE_SENTINEL',
    },
  }.entries) {
    test('system Activity rejects ${result.key} returned text', () async {
      final channel = MethodChannel('voice-activity-result-${result.key}');
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (_) async => result.value);
      final port = NativeVoiceInputPort(
        channel: channel,
        supported: () => true,
      );
      addTearDown(port.dispose);
      await expectLater(
        port.recognizeActivity(
          requestId: 'voice-1-1',
          language: 'auto',
          activity: const VoiceSystemActivity(
            id: 'a/.SpeechActivity',
            name: '系统语音界面',
          ),
          systemApproved: true,
          onLaunch: () => true,
        ),
        throwsA(isA<VoiceInputFailure>()),
      );
    });
  }
  test(
    'system Activity cancellation uses a closed result and performs no fallback',
    () async {
      const channel = MethodChannel('voice-activity-user-cancel');
      final calls = <String>[];
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (call) async {
            calls.add(call.method);
            if (call.method == 'availability') {
              return {
                'onDevice': false,
                'systemService': {'id': _system.id, 'name': _system.name},
                'systemActivity': {'id': 'a/.SpeechActivity', 'name': '系统语音界面'},
              };
            }
            if (call.method == 'recognizeActivity') {
              final id = (call.arguments as Map)['requestId'];
              throw PlatformException(
                code: 'CANCELLED',
                message: 'PRIVATE_VENDOR_SENTINEL',
                details: {
                  'schemaVersion': 'voice.activity_failure.v1',
                  'requestId': id,
                  'stage': 'recognition',
                  'route': 'system_activity',
                  'reasonClass': 'cancelled',
                  'activityResultCode': 0,
                },
              );
            }
            if (call.method == 'cancel') return null;
            fail('Must not retry through another provider ${call.method}');
          });
      final owner = VoiceInputController(
        port: NativeVoiceInputPort(channel: channel, supported: () => true),
      );
      addTearDown(owner.dispose);
      expect(
        await owner.capture(
          current: () => true,
          choose: (_) async => const VoiceInputChoice(systemApproved: true),
          confirmSystem: (_) async => false,
        ),
        isNull,
      );
      expect(owner.message, '语音输入已取消，原草稿已保留。');
      expect(owner.lastDiagnostic!.toSafeMap()['activityResultCode'], 0);
      expect(owner.lastDiagnostic!.androidError, isNull);
      expect(calls.where((c) => c == 'recognizeActivity'), hasLength(1));
      expect(calls, isNot(contains('recognize')));
      expect(calls, isNot(contains('permission')));
    },
  );
  for (final stage in ['create', 'start']) {
    test(
      'system Activity display $stage failure retains typed boundary with no fallback',
      () async {
        final channel = MethodChannel('voice-activity-display-$stage');
        final calls = <String>[];
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
            .setMockMethodCallHandler(channel, (call) async {
              calls.add(call.method);
              if (call.method == 'availability') {
                return {
                  'onDevice': false,
                  'systemService': {'id': _system.id, 'name': _system.name},
                  'systemActivity': {
                    'id': 'a/.SpeechActivity',
                    'name': '系统语音界面',
                  },
                };
              }
              if (call.method == 'recognizeActivity') {
                throw PlatformException(
                  code: 'UNAVAILABLE',
                  message: 'PRIVATE_DISPLAY_SENTINEL',
                  details: {
                    'schemaVersion': 'voice.activity_failure.v1',
                    'requestId': (call.arguments as Map)['requestId'],
                    'stage': stage,
                    'route': 'system_activity',
                    'reasonClass': 'invalid_state',
                    'activityResultCode': null,
                  },
                );
              }
              if (call.method == 'cancel') return null;
              fail(
                'Display failure must not auto retry or acquire audio: ${call.method}',
              );
            });
        final owner = VoiceInputController(
          port: NativeVoiceInputPort(channel: channel, supported: () => true),
        );
        addTearDown(owner.dispose);
        expect(
          await owner.capture(
            current: () => true,
            choose: (_) async => const VoiceInputChoice(systemApproved: true),
            confirmSystem: (_) async => false,
          ),
          isNull,
        );
        expect(owner.active, false);
        expect(owner.recording, false);
        expect(owner.lastDiagnostic!.reason, VoiceFailureReason.invalidState);
        expect(owner.lastDiagnostic!.stage.name, stage);
        expect(owner.lastDiagnostic!.activityResultCode, isNull);
        expect(owner.message, '当前语音服务不可用，请稍后重试或使用键盘。');
        expect(owner.message, isNot(contains('PRIVATE_DISPLAY_SENTINEL')));
        expect(calls.where((c) => c == 'recognizeActivity'), hasLength(1));
        expect(calls, isNot(contains('recognize')));
        expect(calls, isNot(contains('permission')));
      },
    );
  }
  test(
    'checking precedes consent and permission without preparing or recording',
    () async {
      final port = _Port()
        ..pendingAvailability = Completer<VoiceAvailability>()
        ..pendingPermission = Completer<bool>()
        ..pendingLocalReady = Completer<bool>();
      final owner = VoiceInputController(port: port);
      addTearDown(owner.dispose);
      final choice = Completer<VoiceInputChoice?>();
      final phases = <String>[];
      owner.addListener(() => phases.add(owner.phase.name));
      final result = owner.capture(
        current: () => true,
        choose: (_) => choice.future,
        confirmSystem: (_) async => false,
      );
      expect(owner.phase.name, 'checking');
      expect(owner.recording, false);
      expect(owner.elapsed, Duration.zero);
      expect(port.permissions, 0);
      expect(port.localChecks, 0);
      expect(port.sessions, isEmpty);
      port.pendingAvailability!.complete(port.found);
      await _tick();
      expect(owner.phase, VoiceInputPhase.consent);
      expect(phases, isNot(contains('preparing')));
      choice.complete(const VoiceInputChoice());
      await _tick();
      expect(owner.phase, VoiceInputPhase.permission);
      expect(phases, isNot(contains('preparing')));
      expect(port.sessions, isEmpty);
      port.pendingPermission!.complete(true);
      await _tick();
      expect(owner.phase, VoiceInputPhase.preparing);
      expect(port.localChecks, 1);
      expect(owner.recording, false);
      port.pendingLocalReady!.complete(true);
      await _tick();
      expect(owner.phase, VoiceInputPhase.listening);
      port.sessions.single.result.complete('可编辑草稿');
      expect(await result, '可编辑草稿');
      expect(phases, [
        'checking',
        'consent',
        'permission',
        'preparing',
        'listening',
        'idle',
      ]);
      owner.dispose();
    },
  );
  test(
    'denied permission never reports preparation or starts native recognition',
    () async {
      final port = _Port()..granted = false;
      final owner = VoiceInputController(port: port);
      addTearDown(owner.dispose);
      final phases = <String>[];
      owner.addListener(() => phases.add(owner.phase.name));
      expect(
        await owner.capture(
          current: () => true,
          choose: (_) async => const VoiceInputChoice(),
          confirmSystem: (_) async => false,
        ),
        isNull,
      );
      expect(phases, ['checking', 'consent', 'permission', 'idle']);
      expect(port.localChecks, 0);
      expect(port.sessions, isEmpty);
      expect(owner.message, contains('麦克风权限'));
      owner.dispose();
    },
  );
  test(
    'cancelling an availability check retires late result without consent or grant',
    () async {
      final pending = Completer<VoiceAvailability>();
      final port = _Port()..pendingAvailability = pending;
      final owner = VoiceInputController(port: port);
      addTearDown(owner.dispose);
      var choices = 0;
      final first = owner.capture(
        current: () => true,
        choose: (_) async {
          choices++;
          return const VoiceInputChoice();
        },
        confirmSystem: (_) async => false,
      );
      expect(owner.phase.name, 'checking');
      owner.cancel();
      expect(await first, isNull);
      expect(owner.phase, VoiceInputPhase.idle);
      port.pendingAvailability = null;
      final second = owner.capture(
        current: () => true,
        choose: (_) async {
          choices++;
          return const VoiceInputChoice();
        },
        confirmSystem: (_) async => false,
      );
      await _tick();
      expect(port.sessions, hasLength(1));
      pending.complete(const VoiceAvailability());
      await _tick();
      expect(owner.recording, true);
      expect(choices, 1);
      port.sessions.single.result.complete('新的有效草稿');
      expect(await second, '新的有效草稿');
      owner.dispose();
    },
  );
  test(
    'rapid repeated taps while checking start only one permission and recording',
    () async {
      final port = _Port()
        ..pendingAvailability = Completer<VoiceAvailability>();
      final owner = VoiceInputController(port: port);
      addTearDown(owner.dispose);
      final first = owner.capture(
        current: () => true,
        choose: (_) async => const VoiceInputChoice(),
        confirmSystem: (_) async => false,
      );
      expect(owner.phase.name, 'checking');
      expect(
        await owner.capture(
          current: () => true,
          choose: (_) async => const VoiceInputChoice(),
          confirmSystem: (_) async => false,
        ),
        isNull,
      );
      expect(port.probes, 1);
      expect(port.permissions, 0);
      port.pendingAvailability!.complete(port.found);
      await _tick();
      expect(port.permissions, 1);
      expect(port.sessions, hasLength(1));
      port.sessions.single.result.complete('一份草稿');
      expect(await first, '一份草稿');
      owner.dispose();
    },
  );
  test(
    'generic native language error offers actions present in automatic mode',
    () async {
      const channel = MethodChannel('voice-language-auto-recovery');
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (_) async {
            throw PlatformException(code: 'LANGUAGE');
          });
      final port = NativeVoiceInputPort(
        channel: channel,
        supported: () => true,
      );
      final failure = await _recognitionFailure(port);
      expect(failure.message, '当前服务无法识别这段语音，请重试或使用键盘。');
      expect(failure.message, isNot(contains('换语言')));
      port.dispose();
    },
  );
  test('unsupported platform never probes native or asks permission', () async {
    final channel = MethodChannel('voice-test-unsupported');
    var calls = 0;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (_) async {
          calls++;
          return null;
        });
    final port = NativeVoiceInputPort(channel: channel, supported: () => false);
    expect((await port.availability()).available, isFalse);
    expect(calls, 0);
    port.dispose();
  });
  for (final malformed in [
    null,
    <String, Object?>{'onDevice': true},
    {
      'onDevice': false,
      'systemService': {'id': 'a', 'name': ''},
    },
    {'onDevice': true, 'systemService': null, 'audio': 'forbidden'},
  ]) {
    test(
      'native capability rejects malformed closed payload $malformed',
      () async {
        const channel = MethodChannel('voice-test-malformed');
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
            .setMockMethodCallHandler(channel, (_) async => malformed);
        final port = NativeVoiceInputPort(
          channel: channel,
          supported: () => true,
        );
        expect((await port.availability()).available, isFalse);
        port.dispose();
      },
    );
  }
  test(
    'native port binds disclosed service and rejects mismatched result id',
    () async {
      const channel = MethodChannel('voice-test-contract');
      final calls = <MethodCall>[];
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (call) async {
            calls.add(call);
            if (call.method == 'availability') {
              return {
                'onDevice': false,
                'systemService': {'id': _system.id, 'name': _system.name},
              };
            }
            if (call.method == 'permission') {
              return {'requestId': 'voice-1-1', 'granted': true};
            }
            if (call.method == 'recognize') {
              return {'requestId': 'voice-other', 'text': '旧私密文字'};
            }
            return null;
          });
      final port = NativeVoiceInputPort(
        channel: channel,
        supported: () => true,
      );
      expect((await port.availability()).systemService!.name, _system.name);
      expect(calls.map((e) => e.method), ['availability']);
      await expectLater(
        port.recognize(
          requestId: 'voice-1-1',
          language: 'zh-CN',
          onDevice: false,
          systemService: _system,
          systemApproved: false,
          onPhase: (_) {},
        ),
        throwsA(isA<VoiceInputFailure>()),
      );
      expect(calls, hasLength(1));
      expect(await port.requestPermission('voice-1-1'), isTrue);
      await expectLater(
        port.recognize(
          requestId: 'voice-1-1',
          language: 'zh-CN',
          onDevice: false,
          systemService: _system,
          systemApproved: true,
          onPhase: (_) {},
        ),
        throwsA(isA<VoiceInputFailure>()),
      );
      final sent = calls.last.arguments as Map;
      expect(sent['serviceId'], _system.id);
      expect(sent['serviceName'], _system.name);
      expect(sent['systemApproved'], isTrue);
      expect(sent.containsKey('audio'), isFalse);
      expect(sent.containsKey('accountId'), isFalse);
      port.dispose();
    },
  );
  test(
    'probing and declining disclosure never start or request microphone',
    () async {
      final port = _Port(), owner = VoiceInputController(port: _Port());
      owner.dispose(); // A closed controller remains inert.
      final c = VoiceInputController(port: port);
      await c.probe();
      expect(c.canUse, isTrue);
      expect(port.permissions, 0);
      expect(
        await c.capture(
          current: () => true,
          choose: (_) async => null,
          confirmSystem: (_) async => false,
        ),
        isNull,
      );
      expect(port.permissions, 0);
      expect(port.sessions, isEmpty);
      c.dispose();
    },
  );
  test(
    'local support is preferred; stop waits for final text and never resends',
    () async {
      final p = _Port(), c = VoiceInputController(port: _Port());
      c.dispose();
      final owner = VoiceInputController(port: p);
      var fallback = 0;
      final result = owner.capture(
        current: () => true,
        choose: (_) async => const VoiceInputChoice(),
        confirmSystem: (_) async {
          fallback++;
          return true;
        },
      );
      await _tick();
      expect(p.sessions, hasLength(1));
      expect(p.sessions.single.onDevice, isTrue);
      expect(fallback, 0);
      expect(owner.recording, isTrue);
      await owner.stop();
      await owner.stop();
      expect(p.stops, [p.sessions.single.id]);
      expect(owner.phase, VoiceInputPhase.processing);
      p.sessions.single.result.complete('Aberdeen 今天想吃饭');
      expect(await result, 'Aberdeen 今天想吃饭');
      expect(p.sessions, hasLength(1));
      owner.dispose();
    },
  );
  test(
    'missing local model requires a separate fresh system consent',
    () async {
      final p = _Port()..ready = false,
          owner = VoiceInputController(port: _Port());
      owner.dispose();
      final c = VoiceInputController(port: p);
      var asks = 0;
      final result = c.capture(
        current: () => true,
        choose: (_) async => const VoiceInputChoice(),
        confirmSystem: (service) async {
          asks++;
          expect(service.name, _system.name);
          return false;
        },
      );
      expect(await result, isNull);
      expect(asks, 1);
      expect(p.permissions, 1);
      expect(p.sessions, isEmpty);
      c.dispose();
    },
  );
  test(
    'system recognition asks on each session and never defaults permanent consent',
    () async {
      final p = _Port()
        ..found = const VoiceAvailability(systemService: _system);
      final c = VoiceInputController(port: p);
      var consents = 0;
      for (var n = 0; n < 2; n++) {
        final result = c.capture(
          current: () => true,
          choose: (_) async {
            consents++;
            return const VoiceInputChoice(systemApproved: true);
          },
          confirmSystem: (_) async => false,
        );
        await _tick();
        final s = p.sessions.last;
        expect(s.onDevice, isFalse);
        expect(s.approved, isTrue);
        s.result.complete('本人文字');
        expect(await result, '本人文字');
      }
      expect(consents, 2);
      expect(p.permissions, 2);
      c.dispose();
    },
  );
  test('permission denial preserves retry and starts zero sessions', () async {
    final p = _Port()..granted = false, c = VoiceInputController(port: _Port());
    c.dispose();
    final owner = VoiceInputController(port: p);
    expect(
      await owner.capture(
        current: () => true,
        choose: (_) async => const VoiceInputChoice(),
        confirmSystem: (_) async => false,
      ),
      isNull,
    );
    expect(owner.message, contains('麦克风权限'));
    expect(p.sessions, isEmpty);
    expect(owner.active, isFalse);
    p.granted = true;
    final retry = owner.capture(
      current: () => true,
      choose: (_) async => const VoiceInputChoice(),
      confirmSystem: (_) async => false,
    );
    await _tick();
    p.sessions.single.result.complete('重试');
    expect(await retry, '重试');
    owner.dispose();
  });
  test(
    'cancelled pending permission cannot start after a late grant',
    () async {
      final p = _Port()..pendingPermission = Completer<bool>();
      final c = VoiceInputController(port: p);
      final result = c.capture(
        current: () => true,
        choose: (_) async => const VoiceInputChoice(),
        confirmSystem: (_) async => false,
      );
      await _tick();
      expect(c.phase, VoiceInputPhase.permission);
      c.cancel();
      p.pendingPermission!.complete(true);
      expect(await result, isNull);
      expect(p.sessions, isEmpty);
      expect(c.active, isFalse);
      c.dispose();
    },
  );
  test(
    'identity ABA current predicate and cancellation retire late transcript',
    () async {
      final p = _Port(), c = VoiceInputController(port: _Port());
      c.dispose();
      final owner = VoiceInputController(port: p);
      var generation = 1;
      final captured = generation;
      final result = owner.capture(
        current: () => generation == captured,
        choose: (_) async => const VoiceInputChoice(),
        confirmSystem: (_) async => false,
      );
      await _tick();
      generation++;
      owner.cancel();
      generation++;
      p.sessions.single.result.complete('旧账号文字');
      expect(await result, isNull);
      owner.dispose();
    },
  );
  test(
    'active background cancellation and resume never restart recognition',
    () async {
      final p = _Port(), c = VoiceInputController(port: _Port());
      c.dispose();
      final owner = VoiceInputController(port: p);
      final result = owner.capture(
        current: () => true,
        choose: (_) async => const VoiceInputChoice(),
        confirmSystem: (_) async => false,
      );
      await _tick();
      owner.didChangeAppLifecycleState(AppLifecycleState.inactive);
      owner.didChangeAppLifecycleState(AppLifecycleState.resumed);
      p.sessions.single.result.complete('后台旧文字');
      expect(await result, isNull);
      expect(p.sessions, hasLength(1));
      owner.dispose();
    },
  );
  test(
    'own permission inactive is tolerated but true hidden retires grant',
    () async {
      final p = _Port()..pendingPermission = Completer<bool>();
      final c = VoiceInputController(port: p);
      final result = c.capture(
        current: () => true,
        choose: (_) async => const VoiceInputChoice(),
        confirmSystem: (_) async => false,
      );
      await _tick();
      c.didChangeAppLifecycleState(AppLifecycleState.inactive);
      expect(c.active, isTrue);
      c.didChangeAppLifecycleState(AppLifecycleState.hidden);
      c.didChangeAppLifecycleState(AppLifecycleState.resumed);
      p.pendingPermission!.complete(true);
      expect(await result, isNull);
      expect(p.sessions, isEmpty);
      c.dispose();
    },
  );
  testWidgets(
    'recognition deadline cancels original native job without retry',
    (tester) async {
      final p = _Port(),
          c = VoiceInputController(
            port: p,
            sessionTimeout: const Duration(seconds: 2),
          );
      final result = c.capture(
        current: () => true,
        choose: (_) async => const VoiceInputChoice(),
        confirmSystem: (_) async => false,
      );
      await tester.pump();
      expect(p.sessions, hasLength(1));
      await tester.pump(const Duration(seconds: 3));
      expect(await result, isNull);
      expect(c.message, contains('超时'));
      expect(p.sessions, hasLength(1));
      expect(p.cancellations, contains(p.sessions.single.id));
      c.dispose();
      await tester.pump();
    },
  );
  for (final failure in const [
    (1, 'network_timeout', '语音服务连接超时，请检查网络后重试，或使用键盘。'),
    (2, 'network', '语音服务连接失败，请检查网络后重试，或使用键盘。'),
    (3, 'audio', '语音采集失败，请重新点击或使用键盘。'),
    (4, 'service', '设备语音服务暂时出错，请稍后重试或使用键盘。'),
    (5, 'client', '语音识别未能完成，请重新点击或使用键盘。'),
    (6, 'no_speech', '没有识别到语音，请重试或使用键盘。'),
    (7, 'no_speech', '没有识别到语音，请重试或使用键盘。'),
    (8, 'busy', '上一次语音尚未结束，请稍后重试。'),
    (9, 'permission', '未获麦克风权限，请允许后重试，或使用键盘。'),
    (10, 'rate_limit', '语音请求过于频繁，请稍后重试或使用键盘。'),
    (11, 'service', '设备语音服务暂时出错，请稍后重试或使用键盘。'),
    (12, 'language', '当前服务无法识别这段语音，请重试或使用键盘。'),
    (13, 'language', '当前服务无法识别这段语音，请重试或使用键盘。'),
    (14, 'unsupported', '当前语音服务不可用，请稍后重试或使用键盘。'),
    (15, 'unsupported', '当前语音服务不可用，请稍后重试或使用键盘。'),
  ]) {
    test(
      'native numeric diagnostics classify Android reason ${failure.$1}',
      () async {
        final channel = MethodChannel('voice-diagnostic-${failure.$1}');
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
            .setMockMethodCallHandler(channel, (call) async {
              throw PlatformException(
                code: 'UNAVAILABLE',
                message: 'PRIVATE_NATIVE_EXCEPTION_SENTINEL',
                details: {
                  'schemaVersion': 'voice.input_failure.v1',
                  'requestId': 'voice-1-1',
                  'stage': 'recognition',
                  'route': 'system',
                  'reasonClass': failure.$2,
                  'androidError': failure.$1,
                },
              );
            });
        final port = NativeVoiceInputPort(
          channel: channel,
          supported: () => true,
        );
        VoiceInputFailure? rejected;
        try {
          await port.recognize(
            requestId: 'voice-1-1',
            language: 'auto',
            onDevice: false,
            systemService: _system,
            systemApproved: true,
            onPhase: (_) {},
          );
        } on VoiceInputFailure catch (failure) {
          rejected = failure;
        }
        expect(rejected, isNotNull);
        expect(rejected!.message, failure.$3);
        expect(
          rejected.message,
          isNot(contains('PRIVATE_NATIVE_EXCEPTION_SENTINEL')),
        );
        final diagnostic = rejected.diagnostic!;
        expect(diagnostic.toSafeMap(), {
          'schemaVersion': 'voice.input_failure.v1',
          'requestId': 'voice-1-1',
          'stage': 'recognition',
          'route': 'system',
          'reasonClass': failure.$2,
          'androidError': failure.$1,
        });
        expect(port.lastDiagnostic, same(diagnostic));
        port.dispose();
        expect(port.lastDiagnostic, isNull);
      },
    );
  }
  for (final invalid in <String, Object?>{
    'legacy null': null,
    'future schema': {
      ..._diagnostic(),
      'schemaVersion': 'voice.input_failure.v2',
    },
    'retired request': {..._diagnostic(), 'requestId': 'voice-1-2'},
    'extra private payload': {
      ..._diagnostic(),
      'transcript': 'PRIVATE_SENTINEL',
    },
    'unknown stage': {..._diagnostic(), 'stage': 'raw_exception'},
    'support stage on recognition call': {..._diagnostic(), 'stage': 'support'},
    'opposite recognition route': {..._diagnostic(), 'route': 'on_device'},
    'unknown route': {..._diagnostic(), 'route': 'PRIVATE_SERVICE_SENTINEL'},
    'unknown reason': {
      ..._diagnostic(),
      'reasonClass': 'PRIVATE_REASON_SENTINEL',
    },
    'wrong numeric type': {..._diagnostic(), 'androidError': '3'},
    'future numeric code': {..._diagnostic(), 'androidError': 999},
    'negative numeric code': {..._diagnostic(), 'androidError': -3},
    'numeric category mismatch': {..._diagnostic(), 'reasonClass': 'network'},
    'numeric from create exception': {..._diagnostic(), 'stage': 'create'},
  }.entries) {
    test('native adapter drops ${invalid.key} diagnostic safely', () async {
      final channel = MethodChannel('voice-diagnostic-invalid-${invalid.key}');
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (_) async {
            throw PlatformException(
              code: 'UNAVAILABLE',
              message: 'PRIVATE_NATIVE_EXCEPTION_SENTINEL',
              details: invalid.value,
            );
          });
      final port = NativeVoiceInputPort(
        channel: channel,
        supported: () => true,
      );
      final failure = await _recognitionFailure(port);
      expect(failure.message, '当前语音服务不可用，请稍后重试或使用键盘。');
      expect(failure.diagnostic, isNull);
      expect(port.lastDiagnostic, isNull);
      port.dispose();
    });
  }
  test('known exception classes have no arbitrary numeric or raw details', () {
    final diagnostic = VoiceInputDiagnostic.tryDecode(
      _diagnostic(stage: 'create', reason: 'invalid_state', androidError: null),
      requestId: 'voice-1-1',
      method: 'recognize',
      onDevice: false,
    );
    expect(diagnostic!.stage, VoiceFailureStage.create);
    expect(diagnostic.reason, VoiceFailureReason.invalidState);
    expect(diagnostic.androidError, isNull);
    expect(
      diagnostic.toSafeMap().values,
      everyElement(anyOf(isNull, isA<String>())),
    );
    expect(
      VoiceInputDiagnostic.tryDecode(
        _diagnostic(route: 'system', stage: 'support'),
        requestId: 'voice-1-1',
        method: 'localSupport',
      ),
      isNull,
    );
  });
  test(
    'controller keeps one failure diagnostic and requires another gesture',
    () async {
      const channel = MethodChannel('voice-diagnostic-controller');
      final calls = <MethodCall>[];
      var failRecognition = true, fallback = 0;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (call) async {
            calls.add(call);
            final args = call.arguments as Map?;
            switch (call.method) {
              case 'availability':
                return {
                  'onDevice': false,
                  'systemService': {'id': _system.id, 'name': _system.name},
                };
              case 'permission':
                return {'requestId': args!['requestId'], 'granted': true};
              case 'recognize':
                if (failRecognition) {
                  throw PlatformException(
                    code: 'UNAVAILABLE',
                    message: 'PRIVATE_NATIVE_EXCEPTION_SENTINEL',
                    details: _diagnostic(requestId: args!['requestId']),
                  );
                }
                return {
                  'requestId': args!['requestId'],
                  'text': 'confirmed synthetic text',
                };
              case 'cancel':
                return null;
            }
            fail('Unexpected native operation ${call.method}.');
          });
      final port = NativeVoiceInputPort(
        channel: channel,
        supported: () => true,
      );
      final controller = VoiceInputController(port: port);
      Future<String?> capture() => controller.capture(
        current: () => true,
        choose: (_) async => const VoiceInputChoice(systemApproved: true),
        confirmSystem: (_) async {
          fallback++;
          return true;
        },
      );
      expect(await capture(), isNull);
      await _tick();
      expect(controller.message, '语音采集失败，请重新点击或使用键盘。');
      expect(controller.lastDiagnostic!.reason, VoiceFailureReason.audio);
      expect(controller.lastDiagnostic!.androidError, 3);
      expect(controller.lastDiagnostic, same(port.lastDiagnostic));
      expect(controller.active, isFalse);
      expect(controller.phase, VoiceInputPhase.idle);
      expect(calls.map((call) => call.method), [
        'availability',
        'permission',
        'recognize',
        'cancel',
      ]);
      expect(fallback, 0);
      failRecognition = false;
      expect(await capture(), 'confirmed synthetic text');
      await _tick();
      expect(controller.message, isNull);
      expect(controller.lastDiagnostic, isNull);
      expect(port.lastDiagnostic, isNull);
      expect(calls.where((call) => call.method == 'recognize'), hasLength(2));
      expect(fallback, 0);
      controller.dispose();
    },
  );
  test(
    'cancelled native error cannot populate a later owner diagnostic',
    () async {
      const channel = MethodChannel('voice-diagnostic-late-native');
      final pending = Completer<Object?>();
      var recognitions = 0;
      String? oldRequest;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (call) async {
            final args = call.arguments as Map?;
            switch (call.method) {
              case 'availability':
                return {
                  'onDevice': false,
                  'systemService': {'id': _system.id, 'name': _system.name},
                };
              case 'permission':
                return {'requestId': args!['requestId'], 'granted': true};
              case 'recognize':
                if (++recognitions == 1) {
                  oldRequest = args!['requestId'];
                  return pending.future;
                }
                return {
                  'requestId': args!['requestId'],
                  'text': 'new synthetic draft',
                };
              case 'cancel':
                return null;
            }
            fail('Unexpected native operation ${call.method}.');
          });
      final port = NativeVoiceInputPort(
        channel: channel,
        supported: () => true,
      );
      final controller = VoiceInputController(port: port);
      var generation = 1;
      Future<String?> capture() {
        final captured = generation;
        return controller.capture(
          current: () => generation == captured,
          choose: (_) async => const VoiceInputChoice(systemApproved: true),
          confirmSystem: (_) async => false,
        );
      }

      final oldCapture = capture();
      await _tick();
      expect(oldRequest, isNotNull);
      ++generation;
      controller.cancel();
      expect(await oldCapture, isNull);
      await _tick();
      ++generation;
      expect(await capture(), 'new synthetic draft');
      pending.completeError(
        PlatformException(
          code: 'UNAVAILABLE',
          message: 'PRIVATE_LATE_EXCEPTION_SENTINEL',
          details: _diagnostic(requestId: oldRequest!),
        ),
      );
      await _tick();
      expect(recognitions, 2);
      expect(controller.message, isNull);
      expect(controller.lastDiagnostic, isNull);
      expect(port.lastDiagnostic, isNull);
      controller.dispose();
    },
  );
}
