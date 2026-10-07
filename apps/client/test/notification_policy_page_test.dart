import 'dart:convert';
import 'package:birdtie_client/src/workspace/notification_policy_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'inbox_panel_test.dart' show signedInAuth;
import 'notification_policy_controller_test.dart' show currentPolicy, wire;

// Offline transport contracts. Native actor, source and CAS proof is separate.
void main() {
  testWidgets('Chinese preview cancel is no-write, explicit confirm uses CAS', (
    tester,
  ) async {
    final auth = await signedInAuth();
    var puts = 0;
    final client = MockClient((r) async {
      if (r.method == 'GET') return wire(currentPolicy());
      puts++;
      final draft = jsonDecode(r.body) as Map<String, dynamic>;
      expect(draft['expectedVersion'], 0);
      return wire(
        currentPolicy(
          version: 1,
          expires: DateTime.parse(draft['expiresAt'] as String),
        ),
      );
    });
    await tester.pumpWidget(
      MaterialApp(
        home: NotificationPolicyPage(
          auth: auth,
          client: client,
          apiBaseUrl: 'http://fixture',
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.textContaining('关闭会恢复'), findsOneWidget);
    await tester.scrollUntilVisible(find.text('检查并保存'), 200);
    await tester.tap(find.text('检查并保存'));
    await tester.pumpAndSettle();
    expect(find.text('本人设置 · 当前版本 0'), findsOneWidget);
    expect(puts, 0);
    await tester.tap(find.text('返回修改'));
    await tester.pumpAndSettle();
    expect(puts, 0);
    await tester.tap(find.text('检查并保存'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('确认保存'));
    await tester.pumpAndSettle();
    expect(puts, 1);
    expect(find.text('通知设置已保存。'), findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
    auth.dispose();
    client.close();
  });

  testWidgets(
    'large type small screen scroll exposes actions without overflow',
    (tester) async {
      tester.view.physicalSize = const Size(360, 640);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final auth = await signedInAuth();
      final client = MockClient(
        (r) async =>
            wire(currentPolicy(version: 1, enabled: true, route: 'DIGEST')),
      );
      final semantics = tester.ensureSemantics();
      await tester.pumpWidget(
        MaterialApp(
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: const TextScaler.linear(1.6)),
            child: child!,
          ),
          home: NotificationPolicyPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://fixture',
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.scrollUntilVisible(find.text('检查并保存'), 200);
      expect(tester.takeException(), isNull);
      expect(find.text('检查并保存'), findsOneWidget);
      await tester.tap(find.text('检查并保存'));
      await tester.pumpAndSettle();
      expect(find.text('确认保存'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.tap(find.text('返回修改'));
      await tester.pumpAndSettle();
      semantics.dispose();
      await tester.pumpWidget(const SizedBox.shrink());
      auth.dispose();
      client.close();
    },
  );

  testWidgets('workspace switch closes specific-version approval without PUT', (
    tester,
  ) async {
    final auth = await signedInAuth();
    final workspace = ValueNotifier<String?>(null);
    var puts = 0;
    final client = MockClient((r) async {
      if (r.method == 'PUT') puts++;
      return wire(currentPolicy());
    });
    await tester.pumpWidget(
      MaterialApp(
        home: NotificationPolicyPage(
          auth: auth,
          client: client,
          apiBaseUrl: 'http://fixture',
          workspaceChanges: workspace,
          organizationWorkspaceID: () => workspace.value,
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(find.text('检查并保存'), 200);
    await tester.tap(find.text('检查并保存'));
    await tester.pumpAndSettle();
    workspace.value = 'organization';
    await tester.pumpAndSettle();
    expect(find.text('确认保存'), findsNothing);
    expect(find.text('请切回本人账号管理通知设置。'), findsOneWidget);
    expect(puts, 0);
    await tester.pumpWidget(const SizedBox.shrink());
    auth.dispose();
    workspace.dispose();
    client.close();
  });
}
