import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/system_push_api.dart';
import 'package:birdtie_client/src/workspace/system_push_gateway.dart';
import 'package:birdtie_client/src/workspace/system_push_pending_store.dart';
import 'package:firebase_core/firebase_core.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const pushOwner = '11111111-1111-4111-8111-111111111111';
const pushInstallID = '22222222-2222-4222-8222-222222222222';
const pushBindingID = '33333333-3333-4333-8333-333333333333';
const pushEventID = '44444444-4444-4444-8444-444444444444';
const pushInboxID = '55555555-5555-4555-8555-555555555555';
const pushOpID = '66666666-6666-4666-8666-666666666666';
PushInstallation fixtureInstallation() =>
    PushInstallation(pushInstallID, List.filled(43, 'a').join());
Map<String, dynamic> pushEventJSON({
  String event = pushEventID,
  String binding = pushBindingID,
  int version = 1,
}) => {
  'schema': 'birdtie-push-v1',
  'eventId': event,
  'bindingId': binding,
  'bindingVersion': '$version',
};
Map<String, dynamic> pushOptions({bool available = true}) => {
  'schema': 'system-push-options-v1',
  'available': available,
  'registrationKinds': available ? ['FCM_TOKEN'] : [],
};
Map<String, dynamic> pushViewJSON({
  bool enabled = false,
  int version = 0,
  String binding = pushBindingID,
}) => {
  'schema': 'system-push-installation-v1',
  'installationId': pushInstallID,
  'version': version,
  'bindingId': enabled ? binding : null,
  'enabled': enabled,
  'registrationKind': enabled ? 'FCM_TOKEN' : null,
  'registeredAt': enabled ? '2026-10-13T00:00:00Z' : null,
  'validUntil': enabled
      ? DateTime.now().toUtc().add(const Duration(hours: 1)).toIso8601String()
      : null,
};
Map<String, dynamic> pushOperationJSON(PushPending op) => {
  'schema': 'system-push-operation-v1',
  'operationId': op.id,
  'operation': op.operation,
  'expectedVersion': op.expectedVersion,
  'resultVersion': op.expectedVersion + 1,
  'bindingId': op.operation == 'REGISTER' ? pushBindingID : null,
  'state': 'COMMITTED',
  'committedAt': '2026-10-13T00:00:00Z',
};
Map<String, dynamic> pushInboxJSON({String id = pushInboxID}) => {
  'id': id,
  'category': 'updates',
  'title': '服务器当前通知',
  'detail': '服务端重新读取原Inbox内容',
  'resourceType': 'community',
  'resourceId': '77777777-7777-4777-8777-777777777777',
  'createdAt': '2026-10-13T00:00:00Z',
};
Map<String, dynamic> pushOpenJSON(
  PushEvent event, {
  String inbox = pushInboxID,
}) => {
  'schema': 'system-push-open-v1',
  'eventId': event.id,
  'bindingId': event.bindingID,
  'bindingVersion': event.version,
  'item': pushInboxJSON(id: inbox),
};
http.Response pushResponse(Object? data, {int code = 200}) =>
    http.Response.bytes(
      utf8.encode(jsonEncode({'data': data})),
      code,
      headers: {'content-type': 'application/json'},
    );
Map<String, dynamic> pushAddressMatchJSON(
  http.Request request, {
  bool matches = true,
  String binding = pushBindingID,
}) {
  final input = jsonDecode(request.body) as Map<String, dynamic>;
  return {
    'schema': 'system-push-address-match-v1',
    'installationId': pushInstallID,
    'bindingId': binding,
    'version': input['expectedVersion'],
    'matches': matches,
  };
}

const pushSDKConfig = SystemPushProviderConfig(
  enabled: true,
  apiKey: 'synthetic-key',
  appID: 'synthetic-app',
  sender: 'synthetic-sender',
  project: 'synthetic-project',
);

class FakeAndroidPushSDK implements AndroidSystemPushSDK {
  FirebaseOptions options = const FirebaseOptions(
    apiKey: 'synthetic-key',
    appId: 'synthetic-app',
    messagingSenderId: 'synthetic-sender',
    projectId: 'synthetic-project',
  );
  final calls = <String>[];
  final fg = StreamController<Map<String, dynamic>>.broadcast(),
      openedStream = StreamController<Map<String, dynamic>>.broadcast(),
      rotation = StreamController<String>.broadcast();
  Future<PushDeviceAddress?>? lateRegistration;
  @override
  Future<FirebaseOptions> initialize(SystemPushProviderConfig c) async {
    calls.add('initialize');
    return options;
  }

  @override
  Future<void> setAutoInitEnabled(bool value) async {
    calls.add('auto:$value');
  }

  @override
  Stream<Map<String, dynamic>> get foreground {
    calls.add('foreground-listener');
    return fg.stream;
  }

  @override
  Stream<Map<String, dynamic>> get opened => openedStream.stream;
  @override
  Stream<String> get addresses => rotation.stream;
  @override
  Future<Map<String, dynamic>?> initial() async {
    calls.add('initial');
    return null;
  }

  @override
  Future<PushDeviceAddress?> registration({
    required bool requestPermission,
  }) async {
    calls.add('permission:$requestPermission');
    return lateRegistration == null
        ? const PushDeviceAddress('synthetic-address', 'AUTHORIZED')
        : await lateRegistration;
  }

  @override
  Future<void> deleteAddress() async {
    calls.add('delete-address');
  }

  Future<void> close() async {
    await fg.close();
    await openedStream.close();
    await rotation.close();
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test('地址核对只向当前安装证明API读取匹配结果，不返回地址摘要', () async {
    final requests = <http.Request>[];
    var leak = false;
    final client = MockClient((r) async {
      requests.add(r);
      return pushResponse({
        ...pushAddressMatchJSON(r),
        if (leak) 'addressSha256': 'forbidden',
      });
    });
    final api = SystemPushAPI(
      authorizationHeader: () => 'Bearer owner',
      current: () => true,
      baseUrl: 'https://api.invalid',
      client: client,
    );
    try {
      final view = PushView.fromJson(
        pushViewJSON(enabled: true, version: 1),
        pushInstallID,
      );
      expect(
        await api.addressMatches(
          fixtureInstallation(),
          view,
          kind: 'FCM_TOKEN',
          address: 'synthetic-address',
        ),
        isTrue,
      );
      expect(requests.single.method, 'POST');
      expect(requests.single.url.path.endsWith('/address-check'), isTrue);
      expect(jsonDecode(requests.single.body), {
        'schema': 'system-push-address-check-v1',
        'expectedVersion': 1,
        'registrationKind': 'FCM_TOKEN',
        'address': 'synthetic-address',
      });
      leak = true;
      await expectLater(
        api.addressMatches(
          fixtureInstallation(),
          view,
          kind: 'FCM_TOKEN',
          address: 'synthetic-address',
        ),
        throwsFormatException,
      );
    } finally {
      api.dispose();
      client.close();
    }
  });
  test('推送点击只GET获准原Inbox，事件ID与Inbox ID分别保留', () async {
    final event = PushEvent.fromJson(pushEventJSON());
    final requests = <http.Request>[];
    final client = MockClient((r) async {
      requests.add(r);
      return pushResponse(pushOpenJSON(event));
    });
    final api = SystemPushAPI(
      authorizationHeader: () => 'Bearer synthetic-owner',
      current: () => true,
      baseUrl: 'https://api.invalid',
      client: client,
    );
    try {
      final item = await api.open(fixtureInstallation(), event);
      expect(item.id, pushInboxID);
      expect(item.id, isNot(event.id));
      expect(item.title, '服务器当前通知');
      expect(requests.single.method, 'GET');
      expect(
        requests.single.url.path,
        '/v1/me/system-push/events/$pushEventID',
      );
      expect(requests.single.url.queryParameters, {
        'bindingId': pushBindingID,
        'bindingVersion': '1',
      });
      expect(
        requests.single.headers['X-Birdtie-Push-Installation-Proof'],
        fixtureInstallation().proof,
      );
      expect(
        requests.single.headers['Authorization'],
        'Bearer synthetic-owner',
      );
    } finally {
      api.dispose();
      client.close();
    }
  });
  test('payload拒绝业务内容、URL和非规范绑定版本', () {
    for (final extra in [
      {'url': 'https://outside.invalid'},
      {'title': 'payload伪造事实'},
      {'resourceId': pushInboxID},
    ]) {
      expect(
        () => PushEvent.fromJson({...pushEventJSON(), ...extra}),
        throwsFormatException,
      );
    }
    for (final version in ['01', '-1', '9007199254740992', 'one']) {
      expect(
        () =>
            PushEvent.fromJson({...pushEventJSON(), 'bindingVersion': version}),
        throwsFormatException,
      );
    }
  });
  test('恢复API只查原operation且验证已提交版本，不重放PUT或DELETE', () async {
    for (final action in ['REGISTER', 'REVOKE']) {
      final op = PushPending(
        owner: pushOwner,
        id: pushOpID,
        operation: action,
        expectedVersion: 3,
      );
      final requests = <http.Request>[];
      final client = MockClient((r) async {
        requests.add(r);
        return pushResponse(pushOperationJSON(op));
      });
      final api = SystemPushAPI(
        authorizationHeader: () => 'Bearer owner',
        current: () => true,
        baseUrl: 'https://api.invalid',
        client: client,
      );
      try {
        await api.recover(fixtureInstallation(), op);
        expect(requests.single.method, 'GET');
        expect(
          requests.single.url.path.endsWith('/operations/$pushOpID'),
          isTrue,
        );
        expect(
          () => SystemPushAPI.validateOperation({
            ...pushOperationJSON(op),
            'resultVersion': 5,
          }, op),
          throwsFormatException,
        );
      } finally {
        api.dispose();
        client.close();
      }
    }
  });
  test('迟到会话响应和授权拒绝不能成为有效设置', () async {
    var token = 'Bearer original';
    final delayed = Completer<http.Response>();
    final client = MockClient((r) => delayed.future);
    final api = SystemPushAPI(
      authorizationHeader: () => token,
      current: () => true,
      baseUrl: 'https://api.invalid',
      client: client,
    );
    try {
      final expectFailure = expectLater(api.options(), throwsFormatException);
      token = 'Bearer changed';
      delayed.complete(pushResponse(pushOptions()));
      await expectFailure;
    } finally {
      api.dispose();
      client.close();
    }
    var denied = 0;
    final forbidden = MockClient((r) async => pushResponse(null, code: 403));
    final blocked = SystemPushAPI(
      authorizationHeader: () => 'Bearer owner',
      current: () => true,
      baseUrl: 'https://api.invalid',
      client: forbidden,
      onDenied: () => denied++,
    );
    try {
      await expectLater(blocked.options(), throwsFormatException);
      expect(denied, 1);
    } finally {
      blocked.dispose();
      forbidden.close();
    }
  });
  test('SDK默认OFF不初始化，可信四字段完整匹配先于Messaging调用', () async {
    final off = FakeAndroidPushSDK();
    final disabled = PlatformSystemPushGateway(
      config: const SystemPushProviderConfig(),
      sdk: off,
      targetPlatform: TargetPlatform.android,
      isWeb: false,
    );
    expect(disabled.supported, isFalse);
    await disabled.start();
    expect(off.calls, isEmpty);
    disabled.dispose();
    await off.close();
    for (final field in ['apiKey', 'appId', 'sender', 'project']) {
      final sdk = FakeAndroidPushSDK()
        ..options = FirebaseOptions(
          apiKey: field == 'apiKey' ? 'wrong' : 'synthetic-key',
          appId: field == 'appId' ? 'wrong' : 'synthetic-app',
          messagingSenderId: field == 'sender' ? 'wrong' : 'synthetic-sender',
          projectId: field == 'project' ? 'wrong' : 'synthetic-project',
        );
      final gateway = PlatformSystemPushGateway(
        config: pushSDKConfig,
        sdk: sdk,
        targetPlatform: TargetPlatform.android,
        isWeb: false,
      );
      try {
        await expectLater(gateway.start(), throwsFormatException);
        expect(sdk.calls, ['initialize']);
        expect(await gateway.register(requestPermission: true), isNull);
      } finally {
        gateway.dispose();
        await sdk.close();
      }
    }
  });
  test('可信SDK先关闭auto-init，停用期间迟到permission不返回地址', () async {
    final sdk = FakeAndroidPushSDK();
    final gateway = PlatformSystemPushGateway(
      config: pushSDKConfig,
      sdk: sdk,
      targetPlatform: TargetPlatform.android,
      isWeb: false,
    );
    try {
      await gateway.start();
      expect(sdk.calls.take(3), [
        'initialize',
        'auto:false',
        'foreground-listener',
      ]);
      final permission = Completer<PushDeviceAddress?>();
      sdk.lateRegistration = permission.future;
      final task = gateway.register(requestPermission: true);
      await gateway.stop(deleteAddress: false);
      permission.complete(
        const PushDeviceAddress('late-address', 'AUTHORIZED'),
      );
      expect(await task, isNull);
      expect(sdk.calls.where((c) => c == 'auto:true'), isEmpty);
    } finally {
      gateway.dispose();
      await sdk.close();
    }
  });
}
