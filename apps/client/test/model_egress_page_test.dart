import 'dart:convert';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/workspace/model_egress_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'model_egress_api_test.dart';

Future<BirdtieAuthController> egressAuth() async {
  final a = BirdtieAuthController(
    apiBaseUrl: 'http://fixture',
    client: MockClient(
      (r) async => egressResponse(switch (r.url.path) {
        '/v1/me' => {'id': egressOwner},
        '/v1/auth/dev-phone/status' => {'enabled': true},
        _ => {'displayName': '本人合成验收'},
      }),
    ),
    sessionVault: MemorySessionVault()
      ..session = const StoredSession(token: 'synthetic', method: 'dev_phone'),
  );
  await a.initialize();
  return a;
}

Future<void> egressTap(WidgetTester t, String label) async {
  final f = find.text(label);
  await t.scrollUntilVisible(f, 180, scrollable: find.byType(Scrollable).first);
  await t.ensureVisible(f);
  await t.pumpAndSettle();
  await t.tap(f);
  await t.pumpAndSettle();
}

void main() {
  testWidgets(
    'same-key transport replacement retires old API and concrete dialog',
    (t) async {
      final auth = await egressAuth();
      final hosts = <String>[];
      final c = MockClient((r) async {
        hosts.add(r.url.host);
        if (r.url.path.endsWith('/options')) {
          return egressResponse(egressOptionsWire());
        }
        if (r.url.path.endsWith('/budget')) {
          return egressResponse(egressBudgetsWire());
        }
        if (r.method == 'POST') {
          return egressResponse(
            egressPreviewWire(
              deadline: (jsonDecode(r.body) as Map)['deadlineAt'],
            ),
          );
        }
        return egressResponse([]);
      });
      Widget page(String base) => MaterialApp(
        home: ModelEgressPage(
          key: const ValueKey('same'),
          auth: auth,
          client: c,
          apiBaseUrl: base,
        ),
      );
      await t.pumpWidget(page('http://transport-a'));
      await t.pumpAndSettle();
      await egressTap(t, '帮我找周末羽毛球');
      await egressTap(t, '检查具体请求版本');
      await egressTap(t, '确认此版本的本地许可');
      expect(find.byType(AlertDialog), findsOneWidget);
      await t.pumpWidget(page('http://transport-b'));
      await t.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      expect(hosts.where((v) => v == 'transport-b').length, 2);
      expect(find.text('具体请求预览'), findsNothing);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets(
    'unconfigured Chinese direct page never fabricates IDs or writes',
    (t) async {
      final auth = await egressAuth();
      var writes = 0;
      final c = MockClient((r) async {
        if (r.method != 'GET') writes++;
        return egressResponse(
          r.url.path.endsWith('/options') ? egressOptionsWire([]) : [],
        );
      });
      await t.pumpWidget(
        MaterialApp(
          home: ModelEgressPage(
            auth: auth,
            client: c,
            apiBaseUrl: 'http://fixture',
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('模型请求与预算'), findsOneWidget);
      expect(find.textContaining('尚无可用的本人任务配置'), findsOneWidget);
      expect(find.byType(TextField), findsNothing);
      expect(writes, 0);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets(
    'mobile large text concrete preview cancel performs no approval',
    (t) async {
      await t.binding.setSurfaceSize(const Size(360, 720));
      addTearDown(() => t.binding.setSurfaceSize(null));
      final auth = await egressAuth();
      var approvals = 0;
      final c = MockClient((r) async {
        if (r.url.path.endsWith('/options')) {
          return egressResponse(egressOptionsWire());
        }
        if (r.url.path.endsWith('/budget')) {
          return egressResponse(egressBudgetsWire());
        }
        if (r.method == 'POST' && r.url.path.endsWith('/previews')) {
          return egressResponse(
            egressPreviewWire(
              deadline: (jsonDecode(r.body) as Map)['deadlineAt'],
            ),
          );
        }
        if (r.url.path.endsWith('/approvals')) approvals++;
        return egressResponse([]);
      });
      await t.pumpWidget(
        MaterialApp(
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: const TextScaler.linear(1.6)),
            child: child!,
          ),
          home: ModelEgressPage(
            auth: auth,
            client: c,
            apiBaseUrl: 'http://fixture',
          ),
        ),
      );
      await t.pumpAndSettle();
      await egressTap(t, '帮我找周末羽毛球');
      await egressTap(t, '检查具体请求版本');
      expect(find.text('具体请求预览'), findsOneWidget);
      await egressTap(t, '确认此版本的本地许可');
      expect(find.widgetWithText(FilledButton, '确认本地许可'), findsOneWidget);
      await t.tap(find.widgetWithText(TextButton, '取消'));
      await t.pumpAndSettle();
      expect(approvals, 0);
      expect(t.takeException(), isNull);
      await egressTap(t, '退出检查');
      await t.scrollUntilVisible(
        find.textContaining('已退出检查'),
        -180,
        scrollable: find.byType(Scrollable).first,
      );
      expect(find.textContaining('已退出检查'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets('org switch retires nested concrete approval and old callback', (
    t,
  ) async {
    final auth = await egressAuth(), workspace = ValueNotifier<String?>(null);
    var approvals = 0;
    final c = MockClient((r) async {
      if (r.url.path.endsWith('/options')) {
        return egressResponse(egressOptionsWire());
      }
      if (r.url.path.endsWith('/budget')) {
        return egressResponse(egressBudgetsWire());
      }
      if (r.method == 'POST' && r.url.path.endsWith('/previews')) {
        return egressResponse(
          egressPreviewWire(
            deadline: (jsonDecode(r.body) as Map)['deadlineAt'],
          ),
        );
      }
      if (r.url.path.endsWith('/approvals')) approvals++;
      return egressResponse([]);
    });
    await t.pumpWidget(
      MaterialApp(
        home: ModelEgressPage(
          auth: auth,
          client: c,
          apiBaseUrl: 'http://fixture',
          workspaceChanges: workspace,
          organizationWorkspaceID: () => workspace.value,
        ),
      ),
    );
    await t.pumpAndSettle();
    await egressTap(t, '帮我找周末羽毛球');
    await egressTap(t, '检查具体请求版本');
    await egressTap(t, '确认此版本的本地许可');
    expect(find.byType(AlertDialog), findsOneWidget);
    workspace.value = egressRoot;
    await t.pumpAndSettle();
    expect(find.byType(AlertDialog), findsNothing);
    expect(find.textContaining('请使用已登录的个人身份'), findsOneWidget);
    workspace.value = null;
    await t.pumpAndSettle();
    expect(find.text('具体请求预览'), findsNothing);
    expect(approvals, 0);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    workspace.dispose();
  });
}
