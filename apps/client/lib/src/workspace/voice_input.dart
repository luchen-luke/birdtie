import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

enum VoiceInputPhase {
  idle,
  checking,
  preparing,
  consent,
  permission,
  listening,
  processing,
  awaitingSystemUi,
}

class VoiceSystemService {
  const VoiceSystemService({required this.id, required this.name});
  final String id, name;
}

/// The component owns its capture UI and may use its provider's network.
class VoiceSystemActivity extends VoiceSystemService {
  const VoiceSystemActivity({required super.id, required super.name});
}

/// Fixed local processing notice identity, never a selectable network provider.
class VoiceBundledEngine extends VoiceSystemService {
  const VoiceBundledEngine()
    : super(
        id: 'birdtie.bundled.whisper.base-q5_1.v1',
        name: '本机 Whisper 语音识别',
      );
}

class VoiceAvailability {
  const VoiceAvailability({
    bool onDevice = false,
    this.bundledOffline = false,
    this.systemService,
    this.systemActivity,
  }) : onDevice = onDevice || bundledOffline;
  final bool onDevice;
  // Capability declaration only; localLanguageReady verifies the actual model.
  final bool bundledOffline;
  final VoiceSystemService? systemService;
  final VoiceSystemActivity? systemActivity;
  VoiceBundledEngine? get bundledEngine =>
      bundledOffline ? const VoiceBundledEngine() : null;
  VoiceSystemService? get preferredSystem =>
      systemActivity ??
      (systemService is VoiceBundledEngine ? null : systemService);
  bool get available => onDevice || preferredSystem != null;
}

class VoiceInputChoice {
  const VoiceInputChoice({this.language = 'auto', this.systemApproved = false});
  final String language;
  final bool systemApproved;
}

class VoiceInputFailure implements Exception {
  const VoiceInputFailure(this.message, {this.diagnostic});
  final String message;
  final VoiceInputDiagnostic? diagnostic;
}

enum VoiceFailureStage {
  permission,
  support,
  create,
  start,
  recognition,
  stop,
  timeout,
  lifecycle,
}

enum VoiceFailureRoute { onDevice, system, systemActivity, unknown }

enum VoiceFailureReason {
  networkTimeout,
  network,
  audio,
  service,
  client,
  noSpeech,
  busy,
  permission,
  rateLimit,
  language,
  unsupported,
  invalidState,
  invalidRequest,
  cancelled,
  timeout,
  serviceChanged,
  unknown,
}

const _voiceReasonNames = {
  VoiceFailureReason.networkTimeout: 'network_timeout',
  VoiceFailureReason.network: 'network',
  VoiceFailureReason.audio: 'audio',
  VoiceFailureReason.service: 'service',
  VoiceFailureReason.client: 'client',
  VoiceFailureReason.noSpeech: 'no_speech',
  VoiceFailureReason.busy: 'busy',
  VoiceFailureReason.permission: 'permission',
  VoiceFailureReason.rateLimit: 'rate_limit',
  VoiceFailureReason.language: 'language',
  VoiceFailureReason.unsupported: 'unsupported',
  VoiceFailureReason.invalidState: 'invalid_state',
  VoiceFailureReason.invalidRequest: 'invalid_request',
  VoiceFailureReason.cancelled: 'cancelled',
  VoiceFailureReason.timeout: 'timeout',
  VoiceFailureReason.serviceChanged: 'service_changed',
  VoiceFailureReason.unknown: 'unknown',
};
const _voiceAndroidReasons = {
  1: VoiceFailureReason.networkTimeout,
  2: VoiceFailureReason.network,
  3: VoiceFailureReason.audio,
  4: VoiceFailureReason.service,
  5: VoiceFailureReason.client,
  6: VoiceFailureReason.noSpeech,
  7: VoiceFailureReason.noSpeech,
  8: VoiceFailureReason.busy,
  9: VoiceFailureReason.permission,
  10: VoiceFailureReason.rateLimit,
  11: VoiceFailureReason.service,
  12: VoiceFailureReason.language,
  13: VoiceFailureReason.language,
  14: VoiceFailureReason.unsupported,
  15: VoiceFailureReason.unsupported,
};

/// Closed metadata only: never a transcript, audio, service name or exception.
class VoiceInputDiagnostic {
  const VoiceInputDiagnostic._({
    required this.requestId,
    required this.stage,
    required this.route,
    required this.reason,
    required this.androidError,
    this.activityResultCode,
  });
  static const schema = 'voice.input_failure.v1';
  static const activitySchema = 'voice.activity_failure.v1';
  final String requestId;
  final VoiceFailureStage stage;
  final VoiceFailureRoute route;
  final VoiceFailureReason reason;
  final int? androidError;
  final int? activityResultCode;

  static VoiceInputDiagnostic? tryDecode(
    Object? raw, {
    required String requestId,
    required String method,
    bool? onDevice,
  }) {
    if (method == 'recognizeActivity') {
      const keys = {
        'schemaVersion',
        'requestId',
        'stage',
        'route',
        'reasonClass',
        'activityResultCode',
      };
      if (raw is! Map ||
          raw.length != keys.length ||
          !keys.every(raw.containsKey) ||
          raw['schemaVersion'] != activitySchema ||
          raw['requestId'] != requestId ||
          requestId.length > 80 ||
          !RegExp(r'^voice-[0-9]+-[0-9]+$').hasMatch(requestId) ||
          raw['route'] != 'system_activity') {
        return null;
      }
      final stage = VoiceFailureStage.values
          .where((v) => v.name == raw['stage'])
          .firstOrNull;
      final reason = _voiceReasonNames.entries
          .where((v) => v.value == raw['reasonClass'])
          .firstOrNull
          ?.key;
      const resultReasons = {
        0: VoiceFailureReason.cancelled,
        1: VoiceFailureReason.noSpeech,
        2: VoiceFailureReason.client,
        3: VoiceFailureReason.service,
        4: VoiceFailureReason.network,
        5: VoiceFailureReason.audio,
      };
      final numeric = raw['activityResultCode'];
      if (!const {
            VoiceFailureStage.create,
            VoiceFailureStage.start,
            VoiceFailureStage.recognition,
            VoiceFailureStage.timeout,
            VoiceFailureStage.lifecycle,
          }.contains(stage) ||
          reason == null ||
          numeric != null &&
              (numeric is! int ||
                  resultReasons[numeric] != reason ||
                  stage != VoiceFailureStage.recognition)) {
        return null;
      }
      return VoiceInputDiagnostic._(
        requestId: requestId,
        stage: stage!,
        route: VoiceFailureRoute.systemActivity,
        reason: reason,
        androidError: null,
        activityResultCode: numeric as int?,
      );
    }
    const keys = {
      'schemaVersion',
      'requestId',
      'stage',
      'route',
      'reasonClass',
      'androidError',
    };
    if (raw is! Map ||
        raw.length != keys.length ||
        !keys.every(raw.containsKey) ||
        raw['schemaVersion'] != schema ||
        raw['requestId'] != requestId ||
        requestId.length > 80 ||
        !RegExp(r'^voice-[0-9]+-[0-9]+$').hasMatch(requestId)) {
      return null;
    }
    final stage = VoiceFailureStage.values
        .where((v) => v.name == raw['stage'])
        .firstOrNull;
    final route = switch (raw['route']) {
      'on_device' => VoiceFailureRoute.onDevice,
      'system' => VoiceFailureRoute.system,
      'unknown' => VoiceFailureRoute.unknown,
      _ => null,
    };
    final reason = _voiceReasonNames.entries
        .where((v) => v.value == raw['reasonClass'])
        .firstOrNull
        ?.key;
    final numeric = raw['androidError'];
    if (stage == null ||
        route == null ||
        reason == null ||
        numeric != null &&
            (numeric is! int || _voiceAndroidReasons[numeric] != reason)) {
      return null;
    }
    if (numeric != null &&
        stage != VoiceFailureStage.recognition &&
        stage != VoiceFailureStage.support) {
      return null;
    }
    final allowed = switch (method) {
      'permission' => {VoiceFailureStage.permission},
      'localSupport' => {VoiceFailureStage.support, VoiceFailureStage.create},
      'recognize' => {
        VoiceFailureStage.permission,
        VoiceFailureStage.create,
        VoiceFailureStage.start,
        VoiceFailureStage.recognition,
        VoiceFailureStage.stop,
        VoiceFailureStage.timeout,
        VoiceFailureStage.lifecycle,
      },
      'stop' => {VoiceFailureStage.stop},
      _ => <VoiceFailureStage>{},
    };
    if (!allowed.contains(stage) ||
        method == 'localSupport' && route != VoiceFailureRoute.onDevice ||
        method == 'permission' && route != VoiceFailureRoute.unknown ||
        onDevice != null &&
            route !=
                (onDevice
                    ? VoiceFailureRoute.onDevice
                    : VoiceFailureRoute.system)) {
      return null;
    }
    return VoiceInputDiagnostic._(
      requestId: requestId,
      stage: stage,
      route: route,
      reason: reason,
      androidError: numeric as int?,
    );
  }

  Map<String, Object?> toSafeMap() => {
    'schemaVersion': route == VoiceFailureRoute.systemActivity
        ? activitySchema
        : schema,
    'requestId': requestId,
    'stage': stage.name,
    'route': switch (route) {
      VoiceFailureRoute.onDevice => 'on_device',
      VoiceFailureRoute.system => 'system',
      VoiceFailureRoute.systemActivity => 'system_activity',
      VoiceFailureRoute.unknown => 'unknown',
    },
    'reasonClass': _voiceReasonNames[reason],
    if (route == VoiceFailureRoute.systemActivity)
      'activityResultCode': activityResultCode
    else
      'androidError': androidError,
  };

  String get recoveryMessage => switch (reason) {
    VoiceFailureReason.networkTimeout => '语音服务连接超时，请检查网络后重试，或使用键盘。',
    VoiceFailureReason.network => '语音服务连接失败，请检查网络后重试，或使用键盘。',
    VoiceFailureReason.audio => '语音采集失败，请重新点击或使用键盘。',
    VoiceFailureReason.service => '设备语音服务暂时出错，请稍后重试或使用键盘。',
    VoiceFailureReason.client => '语音识别未能完成，请重新点击或使用键盘。',
    VoiceFailureReason.noSpeech => '没有识别到语音，请重试或使用键盘。',
    VoiceFailureReason.busy => '上一次语音尚未结束，请稍后重试。',
    VoiceFailureReason.permission => '未获麦克风权限，请允许后重试，或使用键盘。',
    VoiceFailureReason.rateLimit => '语音请求过于频繁，请稍后重试或使用键盘。',
    VoiceFailureReason.language => '当前服务无法识别这段语音，请重试或使用键盘。',
    VoiceFailureReason.cancelled => '语音输入已取消，原草稿已保留。',
    VoiceFailureReason.timeout => '语音识别等待超时，原草稿已保留，请重试。',
    VoiceFailureReason.serviceChanged => '语音服务已变化，请重新点击并检查服务名称。',
    _ => '当前语音服务不可用，请稍后重试或使用键盘。',
  };
}

/// Future.timeout cannot retire its timer while an unresponsive platform future
/// is still pending. Every watchdog and Dart continuation belongs to this owner.
class _VoiceWaits {
  final _cancel = <VoidCallback>{};
  Future<T> wait<T>(Future<T> source, {Duration? timeout}) {
    final result = Completer<T>();
    Timer? timer;
    void cancel() {
      timer?.cancel();
      if (!result.isCompleted) {
        result.completeError(const VoiceInputFailure('语音输入已取消，原草稿已保留。'));
      }
    }

    _cancel.add(cancel);
    if (timeout != null) {
      timer = Timer(timeout, () {
        if (!result.isCompleted) {
          result.completeError(TimeoutException('Voice deadline'));
        }
      });
    }
    source.then(
      (value) {
        if (!result.isCompleted) result.complete(value);
      },
      onError: (Object error, StackTrace stack) {
        // Consume even a late native failure after the owner has retired.
        if (!result.isCompleted) result.completeError(error, stack);
      },
    );
    return result.future.whenComplete(() {
      timer?.cancel();
      _cancel.remove(cancel);
    });
  }

  void cancelAll() {
    for (final cancel in _cancel.toList()) {
      cancel();
    }
  }
}

/// No audio, account context or HTTP passes through this port. System recognition
/// may use its provider's network; each session starts with an explicit gesture.
abstract class VoiceInputPort {
  Future<VoiceAvailability> availability();
  Future<bool> requestPermission(String requestId);
  Future<bool> localLanguageReady(String requestId, String language);
  Future<String> recognize({
    required String requestId,
    required String language,
    required bool onDevice,
    required VoiceSystemService? systemService,
    required bool systemApproved,
    required ValueChanged<VoiceInputPhase> onPhase,
  });
  Future<String> recognizeActivity({
    required String requestId,
    required String language,
    required VoiceSystemActivity activity,
    required bool systemApproved,
    required bool Function() onLaunch,
  }) async => throw const VoiceInputFailure('当前设备暂不支持系统语音界面，请使用键盘。');
  Future<void> stop(String requestId);
  Future<void> cancel(String requestId);
  void dispose();
}

class NativeVoiceInputPort extends VoiceInputPort {
  NativeVoiceInputPort({
    MethodChannel? channel,
    bool Function()? supported,
    Duration? sessionTimeout,
  }) : _channel = channel ?? const MethodChannel('birdtie/voice_input'),
       _sessionTimeoutOverride = sessionTimeout,
       _supported =
           supported ??
           (() => !kIsWeb && defaultTargetPlatform == TargetPlatform.android);

  final MethodChannel _channel;
  final bool Function() _supported;
  final Duration? _sessionTimeoutOverride;
  bool _bundledOffline = false;
  static NativeVoiceInputPort? _eventOwner;
  String? _request;
  ValueChanged<VoiceInputPhase>? _onPhase;
  bool Function()? _onExternalLaunch;
  bool _closed = false;
  final _waits = _VoiceWaits();
  VoiceInputDiagnostic? _lastDiagnostic;
  VoiceInputDiagnostic? get lastDiagnostic => _lastDiagnostic;

  @override
  Future<VoiceAvailability> availability() async {
    _bundledOffline = false;
    if (_closed || !_supported()) return const VoiceAvailability();
    try {
      final raw = await _waits.wait(
        _channel.invokeMethod<Object?>('availability'),
        timeout: const Duration(seconds: 4),
      );
      if (raw is! Map ||
          raw['onDevice'] is! bool ||
          !raw.containsKey('systemService') ||
          raw.keys.any(
            (key) => !const {
              'onDevice',
              'systemService',
              'systemActivity',
              'bundledOffline',
            }.contains(key),
          ) ||
          raw.containsKey('bundledOffline') && raw['bundledOffline'] is! bool ||
          raw['bundledOffline'] == true && raw['onDevice'] != true) {
        return const VoiceAvailability();
      }
      VoiceSystemService? service;
      final s = raw['systemService'];
      if (s != null) {
        if (s is! Map ||
            s.length != 2 ||
            s['id'] is! String ||
            s['name'] is! String ||
            (s['id'] as String).isEmpty ||
            (s['id'] as String).length > 256 ||
            (s['name'] as String).trim().isEmpty ||
            (s['name'] as String).length > 200) {
          return const VoiceAvailability();
        }
        service = VoiceSystemService(id: s['id'], name: s['name']);
      }
      VoiceSystemActivity? activity;
      final a = raw['systemActivity'];
      if (a != null) {
        if (a is! Map ||
            a.length != 2 ||
            a['id'] is! String ||
            a['name'] is! String ||
            !RegExp(
              r'^[A-Za-z0-9_.]+/[A-Za-z0-9_.$]+$',
            ).hasMatch(a['id'] as String) ||
            (a['id'] as String).length > 256 ||
            (a['name'] as String).trim().isEmpty ||
            (a['name'] as String).length > 200) {
          return const VoiceAvailability();
        }
        activity = VoiceSystemActivity(id: a['id'], name: a['name']);
      }
      _bundledOffline = raw['bundledOffline'] == true;
      return VoiceAvailability(
        onDevice: raw['onDevice'],
        bundledOffline: _bundledOffline,
        systemService: service,
        systemActivity: activity,
      );
    } catch (_) {
      return const VoiceAvailability();
    }
  }

  Future<Object?> _call(String method, Map<String, Object?> args) async {
    _lastDiagnostic = null;
    if (_closed || !_supported()) {
      throw const VoiceInputFailure('当前设备暂不支持语音输入，请使用键盘。');
    }
    try {
      return await _waits.wait(
        _channel.invokeMethod<Object?>(method, args),
        timeout: switch (method) {
          'recognize' =>
            _sessionTimeoutOverride ??
                Duration(
                  seconds: args['onDevice'] == true && _bundledOffline
                      ? 105
                      : 45,
                ),
          'recognizeActivity' =>
            _sessionTimeoutOverride ?? const Duration(seconds: 45),
          'permission' => const Duration(seconds: 90),
          'localSupport' => Duration(seconds: _bundledOffline ? 12 : 6),
          _ => const Duration(seconds: 4),
        },
      );
    } on PlatformException catch (e) {
      final request = args['requestId'];
      final diagnostic = request is String
          ? VoiceInputDiagnostic.tryDecode(
              e.details,
              requestId: request,
              method: method,
              onDevice: args['onDevice'] is bool
                  ? args['onDevice'] as bool
                  : null,
            )
          : null;
      _lastDiagnostic = diagnostic;
      throw VoiceInputFailure(
        diagnostic?.recoveryMessage ??
            switch (e.code) {
              'PERMISSION' => '未获麦克风权限，请允许后重试，或使用键盘。',
              'CANCELLED' => '语音输入已取消，原草稿已保留。',
              'SERVICE_CHANGED' => '语音服务已变化，请重新点击并检查服务名称。',
              'UNAVAILABLE' => '当前语音服务不可用，请稍后重试或使用键盘。',
              'BUSY' => '上一次语音尚未结束，请稍后重试。',
              'TIMEOUT' => '语音识别等待超时，原草稿已保留，请重试。',
              'NO_SPEECH' => '没有识别到语音，请重试或使用键盘。',
              'LANGUAGE' => '当前服务无法识别这段语音，请重试或使用键盘。',
              _ => '暂时无法识别语音，原草稿已保留，请重试或使用键盘。',
            },
        diagnostic: diagnostic,
      );
    } on MissingPluginException {
      throw const VoiceInputFailure('当前设备暂不支持语音输入，请使用键盘。');
    }
  }

  @override
  Future<bool> requestPermission(String requestId) async {
    final raw = await _call('permission', {'requestId': requestId});
    if (raw is! Map ||
        raw.length != 2 ||
        raw['requestId'] != requestId ||
        raw['granted'] is! bool) {
      throw const VoiceInputFailure('麦克风授权结果无法核实，请重新点击。');
    }
    return raw['granted'];
  }

  @override
  Future<bool> localLanguageReady(String requestId, String language) async {
    final raw = await _call('localSupport', {
      'requestId': requestId,
      'language': language,
    });
    if (raw is! Map ||
        raw.length != 2 ||
        raw['requestId'] != requestId ||
        raw['ready'] is! bool) {
      throw const VoiceInputFailure('无法核实本地语言模型，请稍后重试。');
    }
    return raw['ready'];
  }

  Future<Object?> _event(MethodCall call) async {
    if (_closed) return null;
    if (call.method == 'externalLease') {
      final raw = call.arguments;
      if (raw is! Map ||
          raw.length != 1 ||
          raw['requestId'] != _request ||
          _onExternalLaunch == null) {
        return null;
      }
      final accepted = _onExternalLaunch!();
      return {'requestId': _request, 'accepted': accepted};
    }
    if (call.method != 'phase') return null;
    final raw = call.arguments;
    if (raw is! Map || raw.length != 2 || raw['requestId'] != _request) {
      return null;
    }
    final phase = switch (raw['state']) {
      'listening' => VoiceInputPhase.listening,
      'processing' => VoiceInputPhase.processing,
      _ => null,
    };
    if (phase != null) _onPhase?.call(phase);
    return null;
  }

  @override
  Future<String> recognize({
    required String requestId,
    required String language,
    required bool onDevice,
    required VoiceSystemService? systemService,
    required bool systemApproved,
    required ValueChanged<VoiceInputPhase> onPhase,
  }) async {
    if (_request != null ||
        (_eventOwner != null &&
            _eventOwner != this &&
            _eventOwner!._request != null)) {
      throw const VoiceInputFailure('上一次语音尚未结束，请稍后重试。');
    }
    if (!onDevice && (!systemApproved || systemService == null)) {
      throw const VoiceInputFailure('请先检查并同意本次系统语音服务。');
    }
    _request = requestId;
    _onPhase = onPhase;
    _eventOwner = this;
    _channel.setMethodCallHandler(_event);
    try {
      final raw = await _call('recognize', {
        'requestId': requestId,
        'language': language,
        'onDevice': onDevice,
        'serviceId': onDevice ? null : systemService!.id,
        'serviceName': onDevice ? null : systemService!.name,
        'systemApproved': systemApproved,
      });
      if (raw is! Map ||
          raw.length != 2 ||
          raw['requestId'] != requestId ||
          raw['text'] is! String ||
          (raw['text'] as String).length > 4096) {
        throw const VoiceInputFailure('语音结果无法核实，原草稿已保留。');
      }
      return raw['text'];
    } finally {
      if (_request == requestId) {
        _request = null;
        _onPhase = null;
      }
    }
  }

  @override
  Future<void> stop(String requestId) async {
    await _call('stop', {'requestId': requestId});
  }

  @override
  Future<String> recognizeActivity({
    required String requestId,
    required String language,
    required VoiceSystemActivity activity,
    required bool systemApproved,
    required bool Function() onLaunch,
  }) async {
    if (_request != null ||
        (_eventOwner != null &&
            _eventOwner != this &&
            _eventOwner!._request != null)) {
      throw const VoiceInputFailure('上一次语音尚未结束，请稍后重试。');
    }
    if (!systemApproved) throw const VoiceInputFailure('请先检查并同意本次系统语音界面。');
    _request = requestId;
    _eventOwner = this;
    _onPhase = null; // The external UI owns recording; never invent mic phases.
    _onExternalLaunch = onLaunch;
    _channel.setMethodCallHandler(_event);
    try {
      final raw = await _call('recognizeActivity', {
        'requestId': requestId,
        'language': language,
        'activityId': activity.id,
        'activityName': activity.name,
        'systemApproved': systemApproved,
      });
      if (_closed ||
          _request != requestId ||
          raw is! Map ||
          raw.length != 2 ||
          raw['requestId'] != requestId ||
          raw['text'] is! String ||
          (raw['text'] as String).trim().isEmpty ||
          (raw['text'] as String).length > 4096) {
        throw const VoiceInputFailure('语音结果无法核实，原草稿已保留。');
      }
      return raw['text'];
    } finally {
      if (_request == requestId) {
        _request = null;
        _onExternalLaunch = null;
      }
    }
  }

  @override
  Future<void> cancel(String requestId) async {
    if (_request == requestId) _lastDiagnostic = null;
    if (_request == requestId) _onExternalLaunch = null;
    _waits.cancelAll();
    // A native cancel has no transcript/authority to release. Do not hold a new
    // timer or completion on a dead engine merely to wait for its acknowledgement.
    unawaited(
      _channel
          .invokeMethod<Object?>('cancel', {'requestId': requestId})
          .then<void>((_) {}, onError: (Object _) {}),
    );
  }

  @override
  void dispose() {
    if (_closed) return;
    _closed = true;
    _lastDiagnostic = null;
    _waits.cancelAll();
    final id = _request;
    _request = null;
    _onPhase = null;
    _onExternalLaunch = null;
    if (id != null) unawaited(cancel(id));
    if (_eventOwner == this) {
      _eventOwner = null;
      _channel.setMethodCallHandler(null);
    }
  }
}

/// One stable session owner. Business authority remains in the composer's
/// current() callback; this controller cannot submit or publish anything.
class VoiceInputController extends ChangeNotifier with WidgetsBindingObserver {
  VoiceInputController({VoiceInputPort? port, Duration? sessionTimeout})
    : _sessionTimeoutOverride = sessionTimeout,
      sessionTimeout = sessionTimeout ?? const Duration(seconds: 45),
      _port = port ?? NativeVoiceInputPort(sessionTimeout: sessionTimeout) {
    WidgetsBinding.instance.addObserver(this);
  }

  final VoiceInputPort _port;
  final Duration sessionTimeout;
  final Duration? _sessionTimeoutOverride;
  static int _sequence = 0;
  VoiceAvailability availability = const VoiceAvailability();
  VoiceInputDiagnostic? _lastDiagnostic;
  VoiceInputDiagnostic? get lastDiagnostic => _lastDiagnostic;
  VoiceInputPhase phase = VoiceInputPhase.idle;
  String? message;
  String? _request;
  String? _externalLease;
  int _serial = 0, _probeSerial = 0;
  bool _closed = false, _choosing = false;
  Timer? _elapsedTimer;
  Stopwatch? _recordingClock;
  Duration get elapsed => _recordingClock?.elapsed ?? Duration.zero;
  Completer<void>? _resume;
  final _waits = _VoiceWaits(), _probeWaits = _VoiceWaits();
  bool get active => _request != null;
  bool get canUse => availability.available && !_closed;
  bool get recording => phase == VoiceInputPhase.listening;
  bool get resumed =>
      WidgetsBinding.instance.lifecycleState == null ||
      WidgetsBinding.instance.lifecycleState == AppLifecycleState.resumed;

  Future<void> probe() async {
    final serial = ++_probeSerial;
    VoiceAvailability value;
    try {
      value = await _probeWaits.wait(
        _port.availability(),
        timeout: const Duration(seconds: 5),
      );
    } catch (_) {
      value = const VoiceAvailability();
    }
    if (_closed || serial != _probeSerial) return;
    availability = value;
    notifyListeners();
  }

  void _setPhase(VoiceInputPhase value) {
    if (_closed) return;
    if (value == VoiceInputPhase.listening &&
        phase != VoiceInputPhase.listening) {
      _recordingClock = Stopwatch()..start();
      _elapsedTimer?.cancel();
      _elapsedTimer = Timer.periodic(const Duration(seconds: 1), (_) {
        if (!_closed && recording) notifyListeners();
      });
    } else if (value != VoiceInputPhase.listening) {
      _recordingClock?.stop();
      _elapsedTimer?.cancel();
      _elapsedTimer = null;
    }
    phase = value;
    notifyListeners();
  }

  Future<String?> capture({
    required bool Function() current,
    required Future<VoiceInputChoice?> Function(VoiceAvailability) choose,
    required Future<bool> Function(VoiceSystemService) confirmSystem,
  }) async {
    if (_closed || active || !current() || !resumed) return null;
    final serial = ++_serial;
    final id = 'voice-${DateTime.now().microsecondsSinceEpoch}-${++_sequence}';
    _request = id;
    message = null;
    _lastDiagnostic = null;
    bool valid() =>
        !_closed && serial == _serial && _request == id && current();
    try {
      // Capability inspection has not obtained consent or microphone access.
      _setPhase(VoiceInputPhase.checking);
      final found = await _waits.wait(
        _port.availability(),
        timeout: const Duration(seconds: 5),
      );
      if (!valid()) return null;
      availability = found;
      if (!found.available) {
        throw const VoiceInputFailure('当前设备暂不支持语音输入，请使用键盘。');
      }
      _choosing = true;
      _setPhase(VoiceInputPhase.consent);
      final choice = await _waits.wait(choose(found));
      _choosing = false;
      if (!valid() || choice == null || !resumed) return null;
      if (!const ['auto', 'zh-CN', 'en-GB'].contains(choice.language)) {
        throw const VoiceInputFailure('识别语言无效，请重新选择。');
      }
      var onDevice = found.onDevice;
      var approved = choice.systemApproved;
      if (!onDevice && !approved) return null;
      if (onDevice || found.systemActivity == null) {
        _setPhase(VoiceInputPhase.permission);
        final granted = await _waits.wait(
          _port.requestPermission(id),
          timeout: const Duration(seconds: 90),
        );
        if (!valid()) return null;
        if (!granted) {
          throw const VoiceInputFailure('未获麦克风权限，可继续使用键盘，或再次点击重试。');
        }
      }
      // The native permission result is settled on Activity resume. Flutter's
      // lifecycle notification may follow it; wait only for this live gesture.
      if (!resumed) {
        _resume = Completer<void>();
        await _waits.wait(_resume!.future, timeout: const Duration(seconds: 2));
        _resume = null;
      }
      if (!valid() || !resumed) return null;
      _setPhase(VoiceInputPhase.preparing);
      if (onDevice) {
        onDevice = await _waits.wait(
          _port.localLanguageReady(id, choice.language),
          timeout: Duration(seconds: found.bundledOffline ? 12 : 6),
        );
        if (!valid() || !resumed) return null;
        if (!onDevice) {
          if (found.bundledOffline) {
            throw const VoiceInputFailure('本机语音模型未准备好，原草稿已保留，请使用键盘。');
          }
          final system = found.preferredSystem;
          if (system == null) {
            throw const VoiceInputFailure('所选语言的本地模型未准备好，请使用键盘。');
          }
          _choosing = true;
          _setPhase(VoiceInputPhase.consent);
          approved = await _waits.wait(confirmSystem(system));
          _choosing = false;
          if (!valid() || !approved || !resumed) return null;
        }
      }
      if (!valid() || !resumed) return null;
      if (!onDevice && found.systemActivity != null) {
        _setPhase(VoiceInputPhase.awaitingSystemUi);
        final text = await _waits.wait(
          _port.recognizeActivity(
            requestId: id,
            language: choice.language,
            activity: found.systemActivity!,
            systemApproved: approved,
            onLaunch: () {
              if (!valid() || !resumed || _externalLease != null) return false;
              _externalLease = id;
              return true;
            },
          ),
          timeout: sessionTimeout,
        );
        if (!valid() || _externalLease != id) return null;
        if (!resumed) {
          _resume = Completer<void>();
          await _waits.wait(
            _resume!.future,
            timeout: const Duration(seconds: 2),
          );
          _resume = null;
        }
        return valid() && resumed ? text : null;
      }
      final text = await _waits.wait(
        _port.recognize(
          requestId: id,
          language: choice.language,
          onDevice: onDevice,
          systemService: found.systemService,
          systemApproved: approved,
          onPhase: (value) {
            if (valid()) _setPhase(value);
          },
        ),
        timeout:
            _sessionTimeoutOverride ??
            (found.bundledOffline
                ? const Duration(seconds: 105)
                : sessionTimeout),
      );
      return valid() && resumed ? text : null;
    } on VoiceInputFailure catch (e) {
      if (valid()) {
        message = e.message;
        _lastDiagnostic = e.diagnostic;
      }
    } on TimeoutException {
      if (valid()) message = '语音识别等待超时，原草稿已保留，请重试。';
    } catch (_) {
      if (valid()) message = '暂时无法识别语音，原草稿已保留，请重试。';
    } finally {
      if (!_closed && serial == _serial) {
        _request = null;
        _externalLease = null;
        _choosing = false;
        _resume = null;
        unawaited(_port.cancel(id));
        _setPhase(VoiceInputPhase.idle);
      }
    }
    return null;
  }

  Future<void> stop() async {
    final id = _request, serial = _serial;
    if (id == null || phase != VoiceInputPhase.listening) return;
    _setPhase(VoiceInputPhase.processing);
    try {
      await _port.stop(id);
    } catch (_) {
      if (!_closed && _request == id && _serial == serial) {
        cancel();
        message = '暂时无法停止识别，请重新点击或使用键盘。';
        notifyListeners();
      }
    }
  }

  void cancel() {
    final id = _request;
    ++_serial;
    _waits.cancelAll();
    _request = null;
    _externalLease = null;
    _lastDiagnostic = null;
    _choosing = false;
    if (_resume?.isCompleted == false) _resume!.complete();
    _resume = null;
    if (id != null) unawaited(_port.cancel(id));
    if (!_closed) _setPhase(VoiceInputPhase.idle);
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      if (_resume?.isCompleted == false) _resume!.complete();
    } else if (state != AppLifecycleState.detached &&
        _request != null &&
        _externalLease == _request &&
        phase == VoiceInputPhase.awaitingSystemUi) {
      // The native launch handshake granted this request's external UI lease.
      // No such exemption exists while probing, choosing, or recording here.
      return;
    } else if (state == AppLifecycleState.hidden ||
        state == AppLifecycleState.detached ||
        (state == AppLifecycleState.paused &&
            phase != VoiceInputPhase.permission) ||
        (state == AppLifecycleState.inactive &&
            phase != VoiceInputPhase.permission &&
            !_choosing)) {
      cancel();
    }
  }

  @override
  void dispose() {
    if (_closed) return;
    cancel();
    _closed = true;
    _probeWaits.cancelAll();
    ++_probeSerial;
    WidgetsBinding.instance.removeObserver(this);
    _port.dispose();
    super.dispose();
  }
}
