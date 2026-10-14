import 'dart:async';
import 'package:birdtie_client/src/workspace/system_push_gateway.dart';
import 'package:firebase_core/firebase_core.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'system_push_api_test.dart';

const _nativePush = MethodChannel('birdtie/system-push');
const _nativeEvents = MethodChannel('birdtie/system-push/events');
const _iosConfig = SystemPushProviderConfig(
  enabled: true,
  apnsConfigured: true,
);
Future<void> _flushGateway() async {
  for (var i = 0; i < 4; i++) {
    await Future<void>.delayed(Duration.zero);
  }
}

PlatformSystemPushGateway _iosGateway() => PlatformSystemPushGateway(
  config: _iosConfig,
  targetPlatform: TargetPlatform.iOS,
  isWeb: false,
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  final messenger =
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
  tearDown(() {
    messenger.setMockMethodCallHandler(_nativePush, null);
    messenger.setMockMethodCallHandler(_nativeEvents, null);
  });

  test('真实Android Gateway缺省或缺key不触碰SDK，错误default app拒绝注册', () async {
    for (final config in [
      const SystemPushProviderConfig(),
      const SystemPushProviderConfig(
        enabled: true,
        appID: 'synthetic-app',
        sender: 'synthetic-sender',
        project: 'synthetic-project',
      ),
    ]) {
      final sdk = FakeAndroidPushSDK();
      final gateway = PlatformSystemPushGateway(
        config: config,
        sdk: sdk,
        targetPlatform: TargetPlatform.android,
        isWeb: false,
      );
      try {
        expect(gateway.supported, isFalse);
        await gateway.start();
        expect(await gateway.register(requestPermission: true), isNull);
        expect(sdk.calls, isEmpty);
      } finally {
        gateway.dispose();
        await sdk.close();
      }
    }
    final sdk = FakeAndroidPushSDK()
      ..options = const FirebaseOptions(
        apiKey: 'synthetic-key',
        appId: 'synthetic-app',
        messagingSenderId: 'synthetic-sender',
        projectId: 'other-existing-project',
      );
    final gateway = PlatformSystemPushGateway(
      config: pushSDKConfig,
      sdk: sdk,
      targetPlatform: TargetPlatform.android,
      isWeb: false,
    );
    try {
      await expectLater(gateway.start(), throwsFormatException);
      expect(await gateway.register(requestPermission: true), isNull);
      expect(sdk.calls, ['initialize']);
    } finally {
      gateway.dispose();
      await sdk.close();
    }
  });

  test('真实Android Gateway关闭auto-init后只转发闭合选择器，stop退休旧stream', () async {
    final sdk = FakeAndroidPushSDK();
    final gateway = PlatformSystemPushGateway(
      config: pushSDKConfig,
      sdk: sdk,
      targetPlatform: TargetPlatform.android,
      isWeb: false,
    );
    final deliveries = <PushDelivery>[], addresses = <String>[];
    final events = gateway.events.listen(deliveries.add),
        rotation = gateway.addressChanges.listen(addresses.add);
    try {
      await gateway.start();
      expect(sdk.calls.take(3), [
        'initialize',
        'auto:false',
        'foreground-listener',
      ]);
      sdk.fg.add(pushEventJSON());
      sdk.openedStream.add(pushEventJSON());
      sdk.openedStream.add({
        ...pushEventJSON(),
        'url': 'https://untrusted.invalid',
        'title': '不可信内容',
      });
      sdk.rotation.add('new-native-address');
      sdk.rotation.add('');
      await _flushGateway();
      expect(deliveries.length, 2);
      expect(deliveries.map((d) => d.opened), [false, true]);
      expect(deliveries.map((d) => d.event.id), everyElement(pushEventID));
      expect(addresses, ['new-native-address']);
      await gateway.stop(deleteAddress: true);
      expect(sdk.calls.last, 'delete-address');
      expect(sdk.calls, isNot(contains('auto:true')));
      sdk.fg.add(pushEventJSON());
      sdk.openedStream.add(pushEventJSON());
      sdk.rotation.add('retired-native-address');
      await _flushGateway();
      expect(deliveries.length, 2);
      expect(addresses, ['new-native-address']);
    } finally {
      await events.cancel();
      await rotation.cancel();
      gateway.dispose();
      await sdk.close();
    }
  });

  for (final deleteAddress in [false, true]) {
    test(
      'iOS stop($deleteAddress)先发cancelRegistration，等待listener与取消回执再结束',
      () async {
        final calls = <String>[];
        final registration = Completer<Map<String, dynamic>?>(),
            nativeAck = Completer<void>(),
            listenerAck = Completer<void>(),
            listenerCancel = Completer<void>();
        messenger.setMockMethodCallHandler(_nativePush, (call) async {
          calls.add(call.method);
          if (call.method == 'initial') return null;
          if (call.method == 'register') return registration.future;
          if (call.method == 'cancelRegistration') {
            await nativeAck.future;
            return null;
          }
          if (call.method == 'unregister') return null;
          throw StateError(call.method);
        });
        messenger.setMockMethodCallHandler(_nativeEvents, (call) async {
          calls.add('listener.${call.method}');
          if (call.method == 'cancel') {
            listenerCancel.complete();
            await listenerAck.future;
          }
          return null;
        });
        final gateway = _iosGateway();
        try {
          await gateway.start();
          final late = gateway.register(requestPermission: true);
          await _flushGateway();
          expect(calls, contains('register'));
          var completed = false;
          final stop = gateway.stop(deleteAddress: deleteAddress).then((_) {
            completed = true;
          });
          await listenerCancel.future.timeout(const Duration(seconds: 3));
          expect(
            calls.indexOf('cancelRegistration'),
            lessThan(calls.indexOf('listener.cancel')),
          );
          expect(calls, isNot(contains('unregister')));
          expect(completed, isFalse);
          listenerAck.complete();
          await _flushGateway();
          expect(completed, isFalse);
          expect(calls, isNot(contains('unregister')));
          nativeAck.complete();
          await stop;
          expect(
            calls.where((c) => c == 'unregister').length,
            deleteAddress ? 1 : 0,
          );
          if (deleteAddress) expect(calls.last, 'unregister');
          registration.complete({
            'permission': 'AUTHORIZED',
            'address': 'retired-apns-address',
          });
          expect(await late, isNull);
        } finally {
          if (!listenerAck.isCompleted) listenerAck.complete();
          if (!nativeAck.isCompleted) nativeAck.complete();
          if (!registration.isCompleted) registration.complete(null);
          gateway.dispose();
          await _flushGateway();
        }
      },
    );
  }

  test('iOS EventChannel仅转发四字段事件与两字段ADDRESS，旧epoch没有地址或导航', () async {
    final methods = <String>[];
    messenger.setMockMethodCallHandler(_nativePush, (call) async {
      methods.add(call.method);
      return call.method == 'initial' ? pushEventJSON() : null;
    });
    messenger.setMockMethodCallHandler(_nativeEvents, (call) async => null);
    final gateway = _iosGateway();
    final deliveries = <PushDelivery>[], addresses = <String>[];
    final events = gateway.events.listen(deliveries.add),
        rotation = gateway.addressChanges.listen(addresses.add);
    Future<void> emit(Map<String, dynamic> payload) async {
      await messenger.handlePlatformMessage(
        _nativeEvents.name,
        const StandardMethodCodec().encodeSuccessEnvelope(payload),
        (_) {},
      );
      await _flushGateway();
    }

    try {
      await gateway.start();
      await _flushGateway();
      expect(deliveries.length, 1);
      expect(deliveries.single.opened, isTrue);
      await emit({...pushEventJSON(), 'opened': false});
      await emit({
        ...pushEventJSON(),
        'opened': true,
        'resourceId': pushInboxID,
      });
      await emit({'type': 'ADDRESS', 'address': 'current-apns-address'});
      await emit({
        'type': 'ADDRESS',
        'address': 'bad-extra-address',
        'owner': pushOwner,
      });
      await emit({'type': 'ADDRESS', 'address': ''});
      expect(deliveries.length, 2);
      expect(deliveries.last.opened, isFalse);
      expect(deliveries.last.event.bindingID, pushBindingID);
      expect(deliveries.last.event.id, pushEventID);
      expect(addresses, ['current-apns-address']);
      await gateway.stop(deleteAddress: false);
      await emit({...pushEventJSON(), 'opened': true});
      await emit({'type': 'ADDRESS', 'address': 'retired-apns-address'});
      expect(deliveries.length, 2);
      expect(addresses, ['current-apns-address']);
      expect(methods, isNot(contains('unregister')));
    } finally {
      await events.cancel();
      await rotation.cancel();
      gateway.dispose();
      await _flushGateway();
    }
  });

  test('iOS旧initial与status迟到回复不能复活已退休Gateway', () async {
    final initial = Completer<Map<String, dynamic>?>();
    messenger.setMockMethodCallHandler(
      _nativePush,
      (call) async => call.method == 'initial' ? initial.future : null,
    );
    messenger.setMockMethodCallHandler(_nativeEvents, (call) async => null);
    final first = _iosGateway();
    final deliveries = <PushDelivery>[];
    final events = first.events.listen(deliveries.add);
    try {
      final start = first.start();
      await _flushGateway();
      await first.stop(deleteAddress: false);
      initial.complete(pushEventJSON());
      await start;
      await _flushGateway();
      expect(deliveries, isEmpty);
    } finally {
      await events.cancel();
      first.dispose();
      await _flushGateway();
    }
    final status = Completer<Map<String, dynamic>?>();
    messenger.setMockMethodCallHandler(
      _nativePush,
      (call) async => call.method == 'status' ? status.future : null,
    );
    final second = _iosGateway();
    try {
      await second.start();
      final read = second.register(requestPermission: false);
      await _flushGateway();
      await second.stop(deleteAddress: false);
      status.complete({
        'permission': 'PROVISIONAL',
        'address': 'late-status-address',
      });
      expect(await read, isNull);
    } finally {
      second.dispose();
      await _flushGateway();
    }
  });
}
