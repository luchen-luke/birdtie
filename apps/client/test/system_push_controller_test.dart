import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/remote_inbox_source.dart';
import 'package:birdtie_client/src/workspace/system_push_api.dart';
import 'package:birdtie_client/src/workspace/system_push_controller.dart';
import 'package:birdtie_client/src/workspace/system_push_gateway.dart';
import 'package:birdtie_client/src/workspace/system_push_pending_store.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'system_push_api_test.dart';

class MemoryPushStore implements SystemPushPendingStore {
  final rows = <String, PushPending>{};
  int installations = 0;
  @override
  Future<PushInstallation> installation() async {
    installations++;
    return fixtureInstallation();
  }

  @override
  Future<PushPending?> read(String origin, String owner) async =>
      rows['$origin|$owner'];
  @override
  Future<void> write(String origin, PushPending p) async {
    final key = '$origin|${p.owner}';
    if (rows[key] != null && rows[key]!.id != p.id)
      throw const FormatException();
    rows[key] = p;
  }

  @override
  Future<void> delete(String origin, PushPending p) async {
    final key = '$origin|${p.owner}';
    if (rows[key]?.id == p.id) rows.remove(key);
  }
}

class FakePushGateway implements SystemPushGateway {
  bool configured = true;
  int starts = 0, permissions = 0;
  final stops = <bool>[];
  String address = 'synthetic-address';
  Future<PushDeviceAddress?>? latePermission;
  PushDelivery? initial;
  final deliveries = StreamController<PushDelivery>.broadcast(sync: true),
      rotations = StreamController<String>.broadcast(sync: true);
  @override
  bool get supported => configured;
  @override
  String get kind => 'FCM_TOKEN';
  @override
  String get platform => 'ANDROID';
  @override
  Stream<PushDelivery> get events => deliveries.stream;
  @override
  Stream<String> get addressChanges => rotations.stream;
  @override
  Future<void> start() async {
    starts++;
    if (initial != null) {
      deliveries.add(initial!);
      initial = null;
    }
  }

  @override
  Future<PushDeviceAddress?> register({required bool requestPermission}) async {
    if (requestPermission) permissions++;
    return latePermission == null
        ? PushDeviceAddress(address, 'AUTHORIZED')
        : await latePermission;
  }

  @override
  Future<void> stop({required bool deleteAddress}) async {
    stops.add(deleteAddress);
  }

  @override
  void dispose() {
    unawaited(deliveries.close());
    unawaited(rotations.close());
  }
}

SystemPushController fixturePushController(
  http.Client client, {
  FakePushGateway? gateway,
  SystemPushPendingStore? store,
  ChangeNotifier? changes,
  String? Function()? owner,
  String? Function()? token,
  bool Function()? source,
  Future<void> Function(InboxItem, PushEvent)? onOpen,
}) => SystemPushController(
  authorizationHeader: token ?? () => 'Bearer synthetic-owner',
  ownerID: owner ?? () => pushOwner,
  sourceCurrent: source ?? () => true,
  identityChanges: changes ?? ChangeNotifier(),
  apiBaseUrl: 'https://api.invalid',
  client: client,
  gateway: gateway ?? FakePushGateway(),
  pendingStore: store ?? MemoryPushStore(),
  onOpen: onOpen,
);
Future<void> flushPush() async {
  for (var i = 0; i < 5; i++) {
    await Future<void>.delayed(Duration.zero);
  }
}

void main() {
  final binding = TestWidgetsFlutterBinding.ensureInitialized();
  setUp(
    () => binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed),
  );
  test('冷启动opened加stream重复只GET和打开一次，foreground仍刷新', () async {
    final event = PushEvent.fromJson(pushEventJSON());
    final port = FakePushGateway()..initial = PushDelivery(event, opened: true);
    var reads = 0, opens = 0, foreground = 0;
    final client = MockClient((r) async {
      if (r.url.path.endsWith('/options')) return pushResponse(pushOptions());
      if (r.url.path.endsWith('/address-check'))
        return pushResponse(pushAddressMatchJSON(r));
      if (r.url.path.contains('/events/')) {
        reads++;
        return pushResponse(pushOpenJSON(event));
      }
      return pushResponse(pushViewJSON(enabled: true, version: 1));
    });
    final c = SystemPushController(
      authorizationHeader: () => 'Bearer synthetic-owner',
      ownerID: () => pushOwner,
      sourceCurrent: () => true,
      identityChanges: ChangeNotifier(),
      apiBaseUrl: 'https://api.invalid',
      client: client,
      gateway: port,
      pendingStore: MemoryPushStore(),
      onOpen: (item, event) async {
        expect(item.id, pushInboxID);
        opens++;
      },
      onForeground: () => foreground++,
    );
    try {
      await c.initialize();
      await flushPush();
      expect(reads, 1);
      expect(opens, 1);
      port.deliveries.add(PushDelivery(event, opened: true));
      port.deliveries.add(PushDelivery(event, opened: true));
      await flushPush();
      expect(reads, 1);
      expect(opens, 1);
      port.deliveries.add(PushDelivery(event, opened: false));
      await flushPush();
      expect(foreground, 1);
      expect(reads, 1);
      expect(opens, 1);
    } finally {
      c.dispose();
      client.close();
    }
  });
  test('恢复已开启设备先核对地址，同地址只读、变更才CAS续绑', () async {
    for (final changed in [false, true]) {
      var version = 1, puts = 0, checks = 0;
      final port = FakePushGateway()
        ..address = changed ? 'new-address' : 'same-address';
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/options')) return pushResponse(pushOptions());
        if (r.url.path.endsWith('/address-check')) {
          checks++;
          return pushResponse(pushAddressMatchJSON(r, matches: !changed));
        }
        if (r.method == 'PUT') {
          puts++;
          final j = jsonDecode(r.body) as Map<String, dynamic>;
          expect(j['expectedVersion'], 1);
          expect(j['address'], 'new-address');
          version = 2;
          return pushResponse(
            pushOperationJSON(
              PushPending(
                owner: pushOwner,
                id: j['operationId'],
                operation: 'REGISTER',
                expectedVersion: 1,
              ),
            ),
          );
        }
        return pushResponse(pushViewJSON(enabled: true, version: version));
      });
      final c = fixturePushController(client, gateway: port);
      try {
        await c.initialize();
        expect(c.enabled, isTrue);
        expect(checks, 1);
        expect(puts, changed ? 1 : 0);
        expect(port.permissions, 0);
        expect(c.view!.version, changed ? 2 : 1);
      } finally {
        c.dispose();
        client.close();
      }
    }
  });
  test('冷启动系统权限已撤销，停止listener且不做地址核对或续绑', () async {
    final port = FakePushGateway()
      ..latePermission = Future<PushDeviceAddress?>.value(null);
    var checks = 0, puts = 0;
    final client = MockClient((r) async {
      if (r.url.path.endsWith('/options')) return pushResponse(pushOptions());
      if (r.url.path.endsWith('/address-check')) checks++;
      if (r.method == 'PUT') puts++;
      return pushResponse(pushViewJSON(enabled: true, version: 1));
    });
    final c = fixturePushController(client, gateway: port);
    try {
      await c.initialize();
      expect(c.enabled, isFalse);
      expect(c.view!.enabled, isTrue);
      expect(checks, 0);
      expect(puts, 0);
      expect(port.permissions, 0);
      expect(port.stops, isNotEmpty);
      expect(c.error, contains('权限'));
    } finally {
      c.dispose();
      client.close();
    }
  });
  test('Provider缺失不创建安装凭证、不start SDK、不注册地址', () async {
    var calls = 0;
    final store = MemoryPushStore();
    final port = FakePushGateway();
    final client = MockClient((r) async {
      calls++;
      return pushResponse(pushOptions(available: false));
    });
    final c = fixturePushController(client, gateway: port, store: store);
    try {
      await c.initialize();
      await c.enable();
      expect(c.available, isFalse);
      expect(port.starts, 0);
      expect(port.permissions, 0);
      expect(store.installations, 0);
      expect(calls, 1);
    } finally {
      c.dispose();
      client.close();
    }
  });
  for (final operation in ['REGISTER', 'REVOKE']) {
    test('未知$operation只GET核实原操作，重复点击与重启不重放写入', () async {
      final port = FakePushGateway();
      final store = MemoryPushStore();
      PushPending? submitted;
      final methods = <String>[];
      var committed = false;
      final client = MockClient((r) async {
        methods.add(r.method);
        if (r.url.path.endsWith('/address-check'))
          return pushResponse(pushAddressMatchJSON(r));
        if (r.url.path.endsWith('/options')) return pushResponse(pushOptions());
        if (r.method == 'PUT' || r.method == 'DELETE') {
          final j = jsonDecode(r.body) as Map<String, dynamic>;
          submitted = PushPending(
            owner: pushOwner,
            id: j['operationId'],
            operation: operation,
            expectedVersion: j['expectedVersion'],
          );
          throw http.ClientException('unknown write receipt');
        }
        if (r.url.path.contains('/operations/')) {
          committed = true;
          return pushResponse(pushOperationJSON(submitted!));
        }
        return pushResponse(
          pushViewJSON(
            enabled: committed
                ? operation == 'REGISTER'
                : operation == 'REVOKE',
            version: committed
                ? submitted!.expectedVersion + 1
                : operation == 'REVOKE'
                ? 1
                : 0,
          ),
        );
      });
      var c = fixturePushController(client, gateway: port, store: store);
      try {
        await c.initialize();
        if (operation == 'REGISTER') {
          await c.enable();
        } else {
          await c.disable();
        }
        expect(c.uncertain, isTrue);
        expect(store.rows.values.single.id, submitted!.id);
        await c.enable();
        await c.disable();
        expect(methods.where((m) => m == 'PUT' || m == 'DELETE').length, 1);
        c.dispose();
        c = fixturePushController(
          client,
          gateway: FakePushGateway(),
          store: store,
        );
        await c.initialize();
        expect(c.uncertain, isTrue);
        expect(methods.where((m) => m == 'PUT' || m == 'DELETE').length, 1);
        await c.recover();
        expect(c.uncertain, isFalse);
        expect(store.rows, isEmpty);
        expect(c.enabled, operation == 'REGISTER');
        expect(methods.where((m) => m == 'PUT' || m == 'DELETE').length, 1);
      } finally {
        c.dispose();
        client.close();
      }
    });
  }
  test('冷启动已开启恢复最多一条匹配点击，收尾后重新读取原Inbox', () async {
    final event = PushEvent.fromJson(pushEventJSON());
    final port = FakePushGateway()..initial = PushDelivery(event, opened: true);
    final opened = <String>[];
    var eventsRead = 0;
    final client = MockClient((r) async {
      if (r.url.path.endsWith('/address-check'))
        return pushResponse(pushAddressMatchJSON(r));
      if (r.url.path.endsWith('/options')) return pushResponse(pushOptions());
      if (r.url.path.contains('/events/')) {
        eventsRead++;
        return pushResponse(pushOpenJSON(event));
      }
      return pushResponse(pushViewJSON(enabled: true, version: 1));
    });
    final c = fixturePushController(
      client,
      gateway: port,
      onOpen: (item, event) async {
        opened.add(item.id);
      },
    );
    try {
      await c.initialize();
      await flushPush();
      expect(c.enabled, isTrue);
      expect(port.starts, 1);
      expect(port.permissions, 0);
      expect(eventsRead, 1);
      expect(opened, [pushInboxID]);
      expect(opened.single, isNot(pushEventID));
    } finally {
      c.dispose();
      client.close();
    }
  });
  test('冷启动点击早于resumed也只暂存一条，恢复前后台后再核实', () async {
    binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
    final event = PushEvent.fromJson(pushEventJSON());
    final port = FakePushGateway()..initial = PushDelivery(event, opened: true);
    var reads = 0;
    final opened = <String>[];
    final client = MockClient((r) async {
      if (r.url.path.endsWith('/options')) return pushResponse(pushOptions());
      if (r.url.path.endsWith('/address-check'))
        return pushResponse(pushAddressMatchJSON(r));
      if (r.url.path.contains('/events/')) {
        reads++;
        return pushResponse(pushOpenJSON(event));
      }
      return pushResponse(pushViewJSON(enabled: true, version: 1));
    });
    final c = fixturePushController(
      client,
      gateway: port,
      onOpen: (item, event) async {
        opened.add(item.id);
      },
    );
    try {
      await c.initialize();
      await flushPush();
      expect(reads, 0);
      expect(opened, isEmpty);
      binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      await flushPush();
      expect(reads, 1);
      expect(opened, [pushInboxID]);
    } finally {
      c.dispose();
      client.close();
      binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    }
  });
  for (final field in ['owner', 'session', 'source']) {
    test('$field先变化再恢复也退休原批准，迟到permission不能PUT', () async {
      var owner = pushOwner;
      var token = 'Bearer synthetic-owner';
      var source = true;
      final signal = ChangeNotifier();
      final gate = Completer<PushDeviceAddress?>();
      final port = FakePushGateway()..latePermission = gate.future;
      var writes = 0;
      final client = MockClient((r) async {
        if (r.method != 'GET' && !r.url.path.endsWith('/address-check'))
          writes++;
        return pushResponse(
          r.url.path.endsWith('/options') ? pushOptions() : pushViewJSON(),
        );
      });
      final c = fixturePushController(
        client,
        gateway: port,
        changes: signal,
        owner: () => owner,
        token: () => token,
        source: () => source,
      );
      try {
        await c.initialize();
        final task = c.enable();
        await flushPush();
        if (field == 'owner') {
          owner = pushInstallID;
        } else if (field == 'session') {
          token = 'Bearer changed';
        } else {
          source = false;
        }
        signal.notifyListeners();
        expect(c.current, isFalse);
        owner = pushOwner;
        token = 'Bearer synthetic-owner';
        source = true;
        signal.notifyListeners();
        gate.complete(const PushDeviceAddress('late-address', 'AUTHORIZED'));
        await task;
        expect(writes, 0);
        expect(c.current, isFalse);
        expect(c.view, isNull);
        expect(c.installation, isNull);
        expect(c.pending, isNull);
        expect(c.uncertain, isFalse);
      } finally {
        c.dispose();
        client.close();
        signal.dispose();
      }
    });
  }
  test('系统permission交接到后台的迟到结果不能绑定，停止本地listener', () async {
    final gate = Completer<PushDeviceAddress?>();
    final port = FakePushGateway()..latePermission = gate.future;
    var writes = 0;
    final client = MockClient((r) async {
      if (r.method != 'GET' && !r.url.path.endsWith('/address-check')) writes++;
      return pushResponse(
        r.url.path.endsWith('/options') ? pushOptions() : pushViewJSON(),
      );
    });
    final c = fixturePushController(client, gateway: port);
    try {
      await c.initialize();
      final task = c.enable();
      await flushPush();
      binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
      gate.complete(const PushDeviceAddress('late-address', 'AUTHORIZED'));
      await task;
      expect(writes, 0);
      expect(port.stops, isNotEmpty);
      expect(c.uncertain, isFalse);
    } finally {
      c.dispose();
      client.close();
      binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    }
  });
  test('COMMITTED后的view读取失败清空旧view，只能刷新不能再次PUT', () async {
    var wrote = false, failView = true;
    var puts = 0;
    final client = MockClient((r) async {
      if (r.url.path.endsWith('/address-check'))
        return pushResponse(pushAddressMatchJSON(r));
      if (r.url.path.endsWith('/options')) return pushResponse(pushOptions());
      if (r.method == 'PUT') {
        puts++;
        wrote = true;
        final j = jsonDecode(r.body) as Map<String, dynamic>;
        return pushResponse(
          pushOperationJSON(
            PushPending(
              owner: pushOwner,
              id: j['operationId'],
              operation: 'REGISTER',
              expectedVersion: j['expectedVersion'],
            ),
          ),
        );
      }
      if (wrote && failView) return pushResponse(null, code: 503);
      return pushResponse(pushViewJSON(enabled: wrote, version: wrote ? 1 : 0));
    });
    final c = fixturePushController(client);
    try {
      await c.initialize();
      await c.enable();
      expect(c.uncertain, isFalse);
      expect(c.view, isNull);
      expect(c.canEnable, isFalse);
      await c.enable();
      expect(puts, 1);
      failView = false;
      await c.refresh();
      expect(c.enabled, isTrue);
      expect(puts, 1);
    } finally {
      c.dispose();
      client.close();
    }
  });
  test('地址换版本后丢弃旧点击和旧GET，使用新binding重新读当前Inbox', () async {
    const nextBinding = '88888888-8888-4888-8888-888888888888';
    final oldEvent = PushEvent.fromJson(pushEventJSON());
    final nextEvent = PushEvent.fromJson(
      pushEventJSON(binding: nextBinding, version: 2),
    );
    final stale = Completer<http.Response>();
    final port = FakePushGateway();
    final opened = <String>[];
    var version = 1, reads = 0, puts = 0;
    final client = MockClient((r) async {
      if (r.url.path.endsWith('/address-check'))
        return pushResponse(pushAddressMatchJSON(r));
      if (r.url.path.endsWith('/options')) return pushResponse(pushOptions());
      if (r.method == 'PUT') {
        puts++;
        final j = jsonDecode(r.body) as Map<String, dynamic>;
        expect(j['address'], 'new-address');
        version = 2;
        return pushResponse(
          pushOperationJSON(
            PushPending(
              owner: pushOwner,
              id: j['operationId'],
              operation: 'REGISTER',
              expectedVersion: j['expectedVersion'],
            ),
          ),
        );
      }
      if (r.url.path.contains('/events/')) {
        if (++reads == 1) return stale.future;
        return pushResponse(pushOpenJSON(nextEvent));
      }
      return pushResponse(
        pushViewJSON(
          enabled: true,
          version: version,
          binding: version == 1 ? pushBindingID : nextBinding,
        ),
      );
    });
    final c = fixturePushController(
      client,
      gateway: port,
      onOpen: (item, event) async {
        opened.add(item.id);
      },
    );
    try {
      await c.initialize();
      port.deliveries.add(PushDelivery(oldEvent, opened: true));
      await flushPush();
      port.address = 'new-address';
      port.rotations.add('new-address');
      await flushPush();
      expect(puts, 1);
      expect(port.permissions, 0);
      stale.complete(pushResponse(pushOpenJSON(oldEvent)));
      await flushPush();
      expect(opened, isEmpty);
      port.deliveries.add(PushDelivery(oldEvent, opened: true));
      await flushPush();
      expect(reads, 1);
      port.deliveries.add(PushDelivery(nextEvent, opened: true));
      await flushPush();
      expect(opened, [pushInboxID]);
      expect(reads, 2);
    } finally {
      c.dispose();
      client.close();
    }
  });
  test('两个同binding点击的旧GET不能覆盖较新的点击', () async {
    final old = PushEvent.fromJson(pushEventJSON());
    final recent = PushEvent.fromJson(pushEventJSON(event: pushOpID));
    final delayed = Completer<http.Response>();
    final opened = <String>[];
    final port = FakePushGateway();
    final client = MockClient((r) async {
      if (r.url.path.endsWith('/address-check'))
        return pushResponse(pushAddressMatchJSON(r));
      if (r.url.path.endsWith('/options')) return pushResponse(pushOptions());
      if (r.url.path.contains('/events/$pushEventID')) return delayed.future;
      if (r.url.path.contains('/events/'))
        return pushResponse(pushOpenJSON(recent, inbox: pushInstallID));
      return pushResponse(pushViewJSON(enabled: true, version: 1));
    });
    final c = fixturePushController(
      client,
      gateway: port,
      onOpen: (item, event) async {
        opened.add(item.id);
      },
    );
    try {
      await c.initialize();
      port.deliveries.add(PushDelivery(old, opened: true));
      await flushPush();
      port.deliveries.add(PushDelivery(recent, opened: true));
      await flushPush();
      delayed.complete(pushResponse(pushOpenJSON(old)));
      await flushPush();
      expect(opened, [pushInstallID]);
    } finally {
      c.dispose();
      client.close();
    }
  });
}
