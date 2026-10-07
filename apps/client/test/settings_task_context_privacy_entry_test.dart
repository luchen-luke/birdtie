import 'dart:convert';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'task_context_privacy_api_test.dart';
import 'package:birdtie_client/src/workspace/task_context_privacy_api.dart';

class _Auth extends BirdtieAuthController {
  @override
  String? get authorizationHeader => 'Bearer synthetic-task-control';
  @override
  String? get accountID => '81000000-0000-4000-8000-000000000001';
  @override
  bool get signedIn => true;
}

class _Wire extends http.BaseClient {
  final sent = <http.BaseRequest>[];
  @override
  Future<http.StreamedResponse> send(http.BaseRequest r) {
    sent.add(r);
    return Future.value(
      http.StreamedResponse(Stream.value(utf8.encode('{"data":[]}')), 200),
    );
  }
}

Future<void> _visible(WidgetTester t, Finder f) async {
  await t.scrollUntilVisible(
    f,
    200,
    scrollable: find.byType(Scrollable).last,
    maxScrolls: 45,
  );
  await t.pumpAndSettle();
  expect(f.hitTestable(), findsOneWidget);
}

void main() {
  testWidgets('实际设置可发现本次会话任务资料许可入口且初始不写', (t) async {
    FlutterSecureStorage.setMockInitialValues({});
    final auth = _Auth(), wire = _Wire(), city = PublicCityController();
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      client: wire,
      apiBaseUrl: 'https://task-privacy.test',
    );
    addTearDown(() {
      moments.dispose();
      city.dispose();
      auth.dispose();
      wire.close();
    });
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SettingsPage(
            auth: auth,
            city: city,
            moments: moments,
            client: wire,
            apiBaseUrl: 'https://task-privacy.test',
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    await _visible(t, find.widgetWithText(ListTile, '本次会话的任务资料许可'));
    expect(wire.sent.where((r) => r.method != 'GET'), isEmpty);
  });
  testWidgets('实际设置读取具体许可版本，经检查确认原DELETE并刷新且不创建许可', (t) async {
    FlutterSecureStorage.setMockInitialValues({});
    final at = DateTime.now().toUtc();
    bool revoked = false;
    final wire = TaskPrivacyWire((r) async {
      if (r.url.path == taskContextPrivacyPath) {
        return taskResponse(
          taskInventoryWire(
            at,
            grants: [
              taskGrantWire(at, revision: revoked ? 2 : 1, revoked: revoked),
            ],
          ),
        );
      }
      if (r.method == 'DELETE') {
        revoked = true;
        return taskResponse(
          taskOriginalWire(taskGrantWire(at, revision: 2, revoked: true)),
        );
      }
      return taskResponse([], status: 404);
    });
    final auth = _Auth(), city = PublicCityController();
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      client: wire,
      apiBaseUrl: 'https://task-privacy.test',
    );
    addTearDown(() {
      moments.dispose();
      city.dispose();
      auth.dispose();
      wire.close();
    });
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SettingsPage(
            auth: auth,
            city: city,
            moments: moments,
            client: wire,
            apiBaseUrl: 'https://task-privacy.test',
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    final entry = find.widgetWithText(ListTile, '本次会话的任务资料许可');
    await _visible(t, entry);
    await t.tap(entry.hitTestable());
    await t.pumpAndSettle();
    expect(find.textContaining('仅显示本次登录会话'), findsOneWidget);
    final check = find.widgetWithText(OutlinedButton, '检查这项撤回');
    await _visible(t, check);
    expect(find.text('许可版本 1'), findsOneWidget);
    expect(find.text('资料范围：个人偏好'), findsOneWidget);
    expect(wire.sent.where((r) => r.method != 'GET'), isEmpty);
    await t.tap(check.hitTestable());
    await t.pumpAndSettle();
    final confirm = find.widgetWithText(FilledButton, '确认撤回这项许可');
    await _visible(t, confirm);
    expect(t.getSize(confirm).height, greaterThanOrEqualTo(48));
    await t.tap(confirm.hitTestable());
    await t.pumpAndSettle();
    final deletes = wire.sent.where((r) => r.method == 'DELETE').toList();
    expect(deletes.length, 1);
    expect(jsonDecode((deletes.single as http.Request).body), {
      'expectedRevision': 1,
    });
    expect(deletes.single.url.host, 'task-privacy.test');
    expect(
      wire.sent.where((r) => r.method == 'POST' || r.method == 'PUT'),
      isEmpty,
    );
    await _visible(t, find.text('已撤回'));
    expect(find.text('许可版本 2'), findsOneWidget);
    expect(
      wire.sent.where((r) => r.url.path == taskContextPrivacyPath).length,
      2,
    );
  });
  testWidgets('设置来源替换使原许可检查永久退休，不发送旧DELETE；新入口正常读取', (t) async {
    FlutterSecureStorage.setMockInitialValues({});
    final at = DateTime.now().toUtc();
    final wireA = TaskPrivacyWire(
          (r) async => taskResponse(taskInventoryWire(at)),
        ),
        wireB = TaskPrivacyWire(
          (r) async => taskResponse(taskInventoryWire(at)),
        );
    final auth = _Auth(),
        city = PublicCityController(),
        change = ValueNotifier(false);
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      client: wireA,
      apiBaseUrl: 'https://task-a.test',
    );
    addTearDown(() {
      moments.dispose();
      city.dispose();
      auth.dispose();
      wireA.close();
      wireB.close();
      change.dispose();
    });
    await t.pumpWidget(
      MaterialApp(
        home: ValueListenableBuilder<bool>(
          valueListenable: change,
          builder: (context, b, _) => Scaffold(
            body: SettingsPage(
              key: const ValueKey('settings-source'),
              auth: auth,
              city: city,
              moments: moments,
              client: b ? wireB : wireA,
              apiBaseUrl: b ? 'https://task-b.test' : 'https://task-a.test',
            ),
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    final entry = find.widgetWithText(ListTile, '本次会话的任务资料许可');
    await _visible(t, entry);
    await t.tap(entry.hitTestable());
    await t.pumpAndSettle();
    final check = find.widgetWithText(OutlinedButton, '检查这项撤回');
    await _visible(t, check);
    await t.tap(check.hitTestable());
    await t.pumpAndSettle();
    final confirm = find.widgetWithText(FilledButton, '确认撤回这项许可');
    await _visible(t, confirm);
    expect(confirm.hitTestable(), findsOneWidget);
    change.value = true;
    await t.pump();
    await t.pumpAndSettle();
    expect(wireA.sent.where((r) => r.method == 'DELETE'), isEmpty);
    expect(
      wireB.sent.where((r) => r.url.path.startsWith(taskContextPrivacyPath)),
      isEmpty,
    );
    expect(wireB.sent.where((r) => r.method != 'GET'), isEmpty);
    expect(find.text('确认撤回这项许可'), findsNothing);
    await t.pageBack();
    await t.pumpAndSettle();
    await _visible(t, entry);
    await t.tap(entry.hitTestable());
    await t.pumpAndSettle();
    await _visible(t, check);
    final currentReads = wireB.sent
        .where((r) => r.url.path == taskContextPrivacyPath)
        .toList();
    expect(currentReads.single.method, 'GET');
    expect(currentReads.single.url.host, 'task-b.test');
    expect(wireA.sent.where((r) => r.method == 'DELETE'), isEmpty);
  });
}
