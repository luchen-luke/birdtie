import 'dart:async';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'system_push_api.dart';

class PushDeviceAddress {
  const PushDeviceAddress(this.address, this.permission);
  final String address, permission;
}

class PushDelivery {
  const PushDelivery(this.event, {required this.opened});
  final PushEvent event;
  final bool opened;
}

abstract interface class SystemPushGateway {
  bool get supported;
  String get kind;
  String get platform;
  Stream<PushDelivery> get events;
  Stream<String> get addressChanges;
  Future<void> start();
  Future<PushDeviceAddress?> register({required bool requestPermission});
  Future<void> stop({required bool deleteAddress});
  void dispose();
}

class SystemPushProviderConfig {
  const SystemPushProviderConfig({
    this.enabled = const bool.fromEnvironment('BIRDTIE_SYSTEM_PUSH_ENABLED'),
    this.apiKey = const String.fromEnvironment('BIRDTIE_FIREBASE_API_KEY'),
    this.appID = const String.fromEnvironment('BIRDTIE_FIREBASE_APP_ID'),
    this.sender = const String.fromEnvironment('BIRDTIE_FIREBASE_SENDER_ID'),
    this.project = const String.fromEnvironment('BIRDTIE_FIREBASE_PROJECT_ID'),
    this.apnsConfigured = const bool.fromEnvironment('BIRDTIE_APNS_CONFIGURED'),
  });
  final bool enabled, apnsConfigured;
  final String apiKey, appID, sender, project;
  bool matches(FirebaseOptions o) =>
      o.apiKey == apiKey &&
      o.appId == appID &&
      o.messagingSenderId == sender &&
      o.projectId == project;
}

// The injectable port uses the exact native SDK calls in production. It holds
// no account, installation proof, registration address or navigation target.
abstract interface class AndroidSystemPushSDK {
  Future<FirebaseOptions> initialize(SystemPushProviderConfig config);
  Future<void> setAutoInitEnabled(bool value);
  Stream<Map<String, dynamic>> get foreground;
  Stream<Map<String, dynamic>> get opened;
  Stream<String> get addresses;
  Future<Map<String, dynamic>?> initial();
  Future<PushDeviceAddress?> registration({required bool requestPermission});
  Future<void> deleteAddress();
}

class FirebaseSystemPushSDK implements AndroidSystemPushSDK {
  @override
  Future<FirebaseOptions> initialize(SystemPushProviderConfig c) async {
    FirebaseApp app;
    try {
      app = Firebase.app();
    } on FirebaseException catch (e) {
      if (e.code != 'no-app') rethrow;
      app = await Firebase.initializeApp(
        options: FirebaseOptions(
          apiKey: c.apiKey,
          appId: c.appID,
          messagingSenderId: c.sender,
          projectId: c.project,
        ),
      );
    }
    return app.options;
  }

  @override
  Future<void> setAutoInitEnabled(bool value) =>
      FirebaseMessaging.instance.setAutoInitEnabled(value);
  @override
  Stream<Map<String, dynamic>> get foreground =>
      FirebaseMessaging.onMessage.map((m) => m.data);
  @override
  Stream<Map<String, dynamic>> get opened =>
      FirebaseMessaging.onMessageOpenedApp.map((m) => m.data);
  @override
  Stream<String> get addresses => FirebaseMessaging.instance.onTokenRefresh;
  @override
  Future<Map<String, dynamic>?> initial() async =>
      (await FirebaseMessaging.instance.getInitialMessage())?.data;
  @override
  Future<PushDeviceAddress?> registration({
    required bool requestPermission,
  }) async {
    final settings = requestPermission
        ? await FirebaseMessaging.instance.requestPermission(
            alert: true,
            badge: false,
            sound: true,
          )
        : await FirebaseMessaging.instance.getNotificationSettings();
    if (settings.authorizationStatus != AuthorizationStatus.authorized &&
        settings.authorizationStatus != AuthorizationStatus.provisional)
      return null;
    final token = await FirebaseMessaging.instance.getToken();
    if (token == null || token.isEmpty) return null;
    return PushDeviceAddress(
      token,
      settings.authorizationStatus == AuthorizationStatus.provisional
          ? 'PROVISIONAL'
          : 'AUTHORIZED',
    );
  }

  @override
  Future<void> deleteAddress() => FirebaseMessaging.instance.deleteToken();
}

class PlatformSystemPushGateway implements SystemPushGateway {
  PlatformSystemPushGateway({
    SystemPushProviderConfig config = const SystemPushProviderConfig(),
    AndroidSystemPushSDK? sdk,
    TargetPlatform? targetPlatform,
    bool? isWeb,
  }) : _config = config,
       _sdk = sdk ?? FirebaseSystemPushSDK(),
       _target = targetPlatform ?? defaultTargetPlatform,
       _web = isWeb ?? kIsWeb;
  final SystemPushProviderConfig _config;
  final AndroidSystemPushSDK _sdk;
  final TargetPlatform _target;
  final bool _web;
  final _events = StreamController<PushDelivery>.broadcast(),
      _addresses = StreamController<String>.broadcast();
  static const _apns = MethodChannel('birdtie/system-push');
  StreamSubscription<Map<String, dynamic>>? _foreground, _opened;
  StreamSubscription<String>? _rotation;
  StreamSubscription<dynamic>? _apnsEvents;
  bool _started = false, _closed = false, _trustedAndroid = false;
  int _epoch = 0;
  bool _live(int e) => !_closed && _started && e == _epoch;
  @override
  bool get supported =>
      _config.enabled &&
      !_web &&
      (_target == TargetPlatform.android
          ? _config.apiKey.isNotEmpty &&
                _config.appID.isNotEmpty &&
                _config.sender.isNotEmpty &&
                _config.project.isNotEmpty
          : _target == TargetPlatform.iOS && _config.apnsConfigured);
  @override
  String get kind => _target == TargetPlatform.iOS ? 'APNS_TOKEN' : 'FCM_TOKEN';
  @override
  String get platform => _target == TargetPlatform.iOS ? 'IOS' : 'ANDROID';
  @override
  Stream<PushDelivery> get events => _events.stream;
  @override
  Stream<String> get addressChanges => _addresses.stream;
  void _emit(Map<String, dynamic> data, bool opened, int e) {
    if (!_live(e)) return;
    try {
      _events.add(PushDelivery(PushEvent.fromJson(data), opened: opened));
    } catch (_) {}
  }

  @override
  Future<void> start() async {
    if (!supported || _started || _closed) return;
    _started = true;
    final e = ++_epoch;
    try {
      if (platform == 'ANDROID') {
        final identity = await _sdk.initialize(_config);
        if (!_live(e)) return;
        if (!_config.matches(identity))
          throw const FormatException('系统通知服务配置与当前版本不符。');
        _trustedAndroid = true;
        // Native auto-init is also disabled by the manifest. The first
        // Messaging call disables it explicitly, before listeners or tokens.
        await _sdk.setAutoInitEnabled(false);
        if (!_live(e)) return;
        _foreground = _sdk.foreground.listen((data) => _emit(data, false, e));
        _opened = _sdk.opened.listen((data) => _emit(data, true, e));
        _rotation = _sdk.addresses.listen((token) {
          if (_live(e) && token.isNotEmpty) _addresses.add(token);
        });
        final initial = await _sdk.initial();
        if (initial != null) _emit(initial, true, e);
      } else {
        _apnsEvents = const EventChannel('birdtie/system-push/events')
            .receiveBroadcastStream()
            .listen((value) {
              if (!_live(e) || value is! Map) return;
              final data = Map<String, dynamic>.from(value);
              if (data['type'] == 'ADDRESS') {
                if (data.length == 2 &&
                    data.keys.every({'type', 'address'}.contains) &&
                    data['address'] is String &&
                    (data['address'] as String).isNotEmpty)
                  _addresses.add(data['address']);
                return;
              }
              final opened = data.remove('opened') == true;
              _emit(data, opened, e);
            }, onError: (_) {});
        final initial = await _apns.invokeMapMethod<String, dynamic>('initial');
        if (initial != null) _emit(initial, true, e);
      }
    } catch (_) {
      if (e == _epoch) {
        _started = false;
        await _cancelListeners();
      }
      rethrow;
    }
  }

  @override
  Future<PushDeviceAddress?> register({required bool requestPermission}) async {
    if (!supported || !_started || _closed) return null;
    final e = _epoch;
    if (platform == 'IOS') {
      final value = await _apns.invokeMapMethod<String, dynamic>(
        requestPermission ? 'register' : 'status',
      );
      if (!_live(e) ||
          value == null ||
          !['AUTHORIZED', 'PROVISIONAL'].contains(value['permission']) ||
          value['address'] is! String ||
          (value['address'] as String).isEmpty)
        return null;
      return PushDeviceAddress(value['address'], value['permission']);
    }
    if (!_trustedAndroid) return null;
    final address = await _sdk.registration(
      requestPermission: requestPermission,
    );
    return _live(e) ? address : null;
  }

  Future<void> _cancelListeners() async {
    final foreground = _foreground,
        opened = _opened,
        rotation = _rotation,
        apns = _apnsEvents;
    _foreground = null;
    _opened = null;
    _rotation = null;
    _apnsEvents = null;
    await foreground?.cancel();
    await opened?.cancel();
    await rotation?.cancel();
    await apns?.cancel();
  }

  @override
  Future<void> stop({required bool deleteAddress}) async {
    _started = false;
    ++_epoch;
    final nativeCancel = supported && platform == 'IOS'
        ? _apns.invokeMethod<void>('cancelRegistration')
        : null;
    await _cancelListeners();
    await nativeCancel;
    if (!supported) return;
    if (platform == 'ANDROID' && _trustedAndroid) {
      await _sdk.setAutoInitEnabled(false);
      if (deleteAddress) await _sdk.deleteAddress();
    }
    if (platform == 'IOS') {
      if (deleteAddress) await _apns.invokeMethod<void>('unregister');
    }
  }

  @override
  void dispose() {
    if (_closed) return;
    _closed = true;
    unawaited(stop(deleteAddress: false).catchError((_) {}));
    unawaited(_events.close());
    unawaited(_addresses.close());
  }
}
