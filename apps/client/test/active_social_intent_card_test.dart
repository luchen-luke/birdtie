import 'package:birdtie_client/src/workspace/active_social_intent_card.dart';
import 'package:birdtie_client/src/workspace/active_social_intent_page.dart';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'active_social_intent_api_test.dart';
import 'model_egress_page_test.dart' show egressAuth;

void main() {
  testWidgets(
    'anonymous modal has Chinese recovery and close with zero requests',
    (t) async {
      final auth = BirdtieAuthController(sessionVault: MemorySessionVault());
      var requests = 0;
      final c = MockClient((r) async {
        requests++;
        return nowResponse(nowList());
      });
      await t.pumpWidget(
        MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: TextButton(
                onPressed: () => showModalBottomSheet<void>(
                  context: context,
                  isScrollControlled: true,
                  builder: (sheetContext) => Scaffold(
                    body: SingleChildScrollView(
                      child: ActiveSocialIntentCard(
                        auth: auth,
                        client: c,
                        onClose: () => Navigator.of(sheetContext).pop(),
                      ),
                    ),
                  ),
                ),
                child: const Text('我的社交意图'),
              ),
            ),
          ),
        ),
      );
      await t.tap(find.text('我的社交意图'));
      await t.pumpAndSettle();
      expect(find.text('登录后可查看和管理自己的社交意图。'), findsOneWidget);
      expect(find.text('请先在个人资料中登录，再重新打开此入口。'), findsOneWidget);
      expect(find.text('请返回设置登录后，重新打开此入口。'), findsNothing);
      expect(requests, 0);
      expect(
        t.getSize(find.widgetWithText(TextButton, '关闭')).height,
        greaterThanOrEqualTo(48),
      );
      await t.tap(find.text('关闭'));
      await t.pumpAndSettle();
      expect(find.byType(ActiveSocialIntentCard), findsNothing);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets('organization identity has neutral guidance and zero requests', (
    t,
  ) async {
    final auth = await egressAuth();
    var requests = 0;
    final c = MockClient((r) async {
      requests++;
      return nowResponse(nowList());
    });
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ActiveSocialIntentCard(
            auth: auth,
            client: c,
            organizationWorkspaceID: () => nowIntent,
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.text('请切回个人身份查看社交意图。'), findsOneWidget);
    expect(find.byTooltip('管理我的意图'), findsNothing);
    expect(requests, 0);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
  });
  testWidgets(
    'native selector card opens same identity direct page without writes',
    (t) async {
      final auth = await egressAuth();
      final methods = <String>[];
      final c = MockClient((r) async {
        methods.add(r.method);
        return nowResponse(
          r.url.path.endsWith('/options') ? nowOptions() : nowList(),
        );
      });
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: ActiveSocialIntentCard(auth: auth, client: c),
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('当前社交意图'), findsOneWidget);
      await t.tap(find.byTooltip('管理我的意图'));
      await t.pumpAndSettle();
      expect(find.text('我的社交意图'), findsOneWidget);
      expect(methods.every((m) => m == 'GET'), isTrue);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets('workspace switch removes nested direct page before old writes', (
    t,
  ) async {
    final auth = await egressAuth(), workspace = ValueNotifier<String?>(null);
    var requests = 0;
    final c = MockClient((r) async {
      requests++;
      return nowResponse(
        r.url.path.endsWith('/options') ? nowOptions() : nowList(),
      );
    });
    String? getter() => workspace.value;
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ActiveSocialIntentCard(
            auth: auth,
            client: c,
            workspaceChanges: workspace,
            organizationWorkspaceID: getter,
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.tap(find.byTooltip('管理我的意图'));
    await t.pumpAndSettle();
    final before = requests;
    workspace.value = nowIntent;
    workspace.value = null;
    await t.pumpAndSettle();
    expect(find.byType(ActiveSocialIntentPage), findsNothing);
    expect(find.text('我的社交意图'), findsOneWidget);
    expect(find.text('当前社交意图'), findsNothing);
    expect(find.text('账号或工作区已变化，请关闭后重新打开意图入口。'), findsOneWidget);
    expect(requests, before);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    workspace.dispose();
    auth.dispose();
  });
}
