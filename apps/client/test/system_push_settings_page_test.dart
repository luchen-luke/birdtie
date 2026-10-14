import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/system_push_controller.dart';
import 'package:birdtie_client/src/workspace/system_push_pending_store.dart';
import 'package:birdtie_client/src/workspace/system_push_settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'system_push_api_test.dart';
import 'system_push_controller_test.dart';

Future<void> tapPushUntilIdle(
  WidgetTester tester,
  SystemPushController controller,
  Finder control,
) async {
  await tester.runAsync(() async {
    final completed = Completer<void>();
    void settled() {
      if (!controller.busy && !completed.isCompleted) completed.complete();
    }

    controller.addListener(settled);
    try {
      await tester.tap(control);
      settled();
      await completed.future.timeout(const Duration(seconds: 5));
    } finally {
      controller.removeListener(settled);
    }
  });
}

void main() {
  testWidgets('小屏大字系统通知未配置，状态/边界/刷新可达不请求权限', (t) async {
    t.view.physicalSize = const Size(320, 720);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    WidgetsBinding.instance.handleAppLifecycleStateChanged(
      AppLifecycleState.resumed,
    );
    final port = FakePushGateway();
    final client = MockClient(
      (r) async => pushResponse(pushOptions(available: false)),
    );
    final c = fixturePushController(client, gateway: port);
    try {
      await t.runAsync(c.initialize);
      expect(c.busy, isFalse);
      await t.pumpWidget(
        MaterialApp(
          builder: (ctx, child) => MediaQuery(
            data: MediaQuery.of(
              ctx,
            ).copyWith(textScaler: const TextScaler.linear(2)),
            child: child!,
          ),
          home: SystemPushSettingsPage(controller: c),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('系统通知服务尚未配置。'), findsOneWidget);
      final enable = t.widget<FilledButton>(
        find.byKey(const Key('system-push-enable')),
      );
      expect(enable.onPressed, isNull);
      await t.scrollUntilVisible(
        find.byKey(const Key('system-push-refresh')),
        180,
        scrollable: find.byType(Scrollable).first,
      );
      expect(find.byKey(const Key('system-push-refresh')), findsOneWidget);
      expect(port.permissions, 0);
      expect(port.starts, 0);
      expect(t.takeException(), isNull);
    } finally {
      await t.pumpWidget(const SizedBox());
      c.dispose();
      client.close();
    }
  });
  testWidgets('未知注册只显示核实原操作，点击后GET恢复真实状态', (t) async {
    WidgetsBinding.instance.handleAppLifecycleStateChanged(
      AppLifecycleState.resumed,
    );
    PushPending? op;
    var puts = 0, recovered = false;
    final methods = <String>[];
    final client = MockClient((r) async {
      methods.add(r.method);
      if (r.url.path.endsWith('/address-check'))
        return pushResponse(pushAddressMatchJSON(r));
      if (r.url.path.endsWith('/options')) return pushResponse(pushOptions());
      if (r.method == 'PUT') {
        puts++;
        final j = jsonDecode(r.body) as Map<String, dynamic>;
        op = PushPending(
          owner: pushOwner,
          id: j['operationId'],
          operation: 'REGISTER',
          expectedVersion: j['expectedVersion'],
        );
        throw http.ClientException('unknown reply');
      }
      if (r.url.path.contains('/operations/')) {
        recovered = true;
        return pushResponse(pushOperationJSON(op!));
      }
      return pushResponse(
        pushViewJSON(enabled: recovered, version: recovered ? 1 : 0),
      );
    });
    final c = fixturePushController(client);
    try {
      await t.runAsync(c.initialize);
      expect(c.busy, isFalse);
      await t.pumpWidget(
        MaterialApp(home: SystemPushSettingsPage(controller: c)),
      );
      await t.pumpAndSettle();
      await tapPushUntilIdle(t, c, find.byKey(const Key('system-push-enable')));
      await t.pumpAndSettle();
      expect(c.uncertain, isTrue);
      expect(find.byKey(const Key('system-push-enable')), findsNothing);
      expect(find.byKey(const Key('system-push-recover')), findsOneWidget);
      await tapPushUntilIdle(
        t,
        c,
        find.byKey(const Key('system-push-recover')),
      );
      await t.pumpAndSettle();
      expect(find.text('本设备已启用系统通知'), findsOneWidget);
      expect(puts, 1);
      expect(methods.where((m) => m == 'PUT' || m == 'DELETE').length, 1);
      expect(t.takeException(), isNull);
    } finally {
      await t.pumpWidget(const SizedBox());
      c.dispose();
      client.close();
    }
  });
}
