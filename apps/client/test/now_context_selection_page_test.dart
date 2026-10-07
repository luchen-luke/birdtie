import 'dart:async';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/workspace/now_context_selection_api.dart';
import 'package:birdtie_client/src/workspace/now_context_selection_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'model_egress_page_test.dart' show egressAuth;
import 'now_context_selection_api_test.dart';

Future<void> selectionTap(WidgetTester t, String label) async {
  final f = find.text(label);
  await t.scrollUntilVisible(f, 180, scrollable: find.byType(Scrollable).first);
  await t.ensureVisible(f);
  await t.pumpAndSettle();
  await t.tap(f);
  await t.pumpAndSettle();
}

void main() {
  testWidgets(
    'Chinese own current destination history online selection does not auto-query and online callback is explicit',
    (t) async {
      final auth = await egressAuth();
      final raw = selectionOptions();
      var selected = 0, online = 0, posts = 0;
      final client = MockClient((r) async {
        if (r.method == 'POST') posts++;
        return selectionResponse(
          r.method == 'GET'
              ? raw
              : selectionReceipt(
                  raw,
                  (raw['items'] as List)[1] as Map<String, dynamic>,
                ),
        );
      });
      await t.pumpWidget(
        MaterialApp(
          home: NowContextSelectionPage(
            auth: auth,
            client: client,
            onSelect: (v) {
              expect(v.contextType, 'ONLINE');
              selected++;
            },
            onOpenOnlineOpportunities: () => online++,
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('选择查看情境'), findsOneWidget);
      expect(posts, 0);
      await selectionTap(t, '查找线上活动与伙伴');
      expect(online, 1);
      expect(selected, 0);
      await selectionTap(t, '本人合成线上情境');
      expect(posts, 0);
      await selectionTap(t, '核验并切换查看范围');
      expect(selected, 1);
      expect(posts, 1);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets(
    'anonymous and organization identity show neutral Chinese state with no domain requests',
    (t) async {
      final auth = BirdtieAuthController(sessionVault: MemorySessionVault());
      var calls = 0;
      final c = MockClient((r) async {
        calls++;
        return selectionResponse(selectionOptions());
      });
      await t.pumpWidget(
        MaterialApp(
          home: NowContextSelectionPage(
            auth: auth,
            client: c,
            onSelect: (_) => fail('anonymous callback'),
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('登录后可选择本人声明或公开目的地。'), findsOneWidget);
      expect(calls, 0);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      final person = await egressAuth();
      await t.pumpWidget(
        MaterialApp(
          home: NowContextSelectionPage(
            auth: person,
            client: c,
            organizationWorkspaceID: () => selectionContext,
            onSelect: (_) => fail('organization callback'),
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('请切回个人身份选择情境。'), findsOneWidget);
      expect(calls, 0);
      await t.pumpWidget(const SizedBox());
      person.dispose();
    },
  );
  testWidgets(
    'same-key transport replacement retires pending original choice without using new API for old request',
    (t) async {
      final auth = await egressAuth();
      final raw = selectionOptions();
      final pending = Completer<http.Response>();
      var callbacks = 0, oldPosts = 0, newPosts = 0;
      void select(NowContextChoice _) {
        callbacks++;
      }

      final first = MockClient((r) async {
        if (r.method == 'POST') {
          oldPosts++;
          return pending.future;
        }
        return selectionResponse(raw);
      });
      final next = MockClient((r) async {
        if (r.method == 'POST') newPosts++;
        return selectionResponse(raw);
      });
      Widget frame(http.Client c, String base) => MaterialApp(
        home: NowContextSelectionPage(
          key: const ValueKey('same'),
          auth: auth,
          client: c,
          apiBaseUrl: base,
          onSelect: select,
        ),
      );
      await t.pumpWidget(frame(first, 'http://first'));
      await t.pumpAndSettle();
      await selectionTap(t, '本人合成线上情境');
      await t.tap(find.text('核验并切换查看范围'));
      await t.pump();
      expect(oldPosts, 1);
      await t.pumpWidget(frame(next, 'http://next'));
      await t.pumpAndSettle();
      pending.complete(
        selectionResponse(
          selectionReceipt(
            raw,
            (raw['items'] as List)[1] as Map<String, dynamic>,
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(callbacks, 0);
      expect(newPosts, 0);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets(
    'parent-build workspace ABA retires selected view before late response and keeps private labels absent',
    (t) async {
      final auth = await egressAuth();
      final workspace = ValueNotifier<String?>(null),
          trigger = ValueNotifier(false);
      final raw = selectionOptions();
      var calls = 0, callbacks = 0;
      String? getter() => workspace.value;
      final c = MockClient((r) async {
        calls++;
        return selectionResponse(raw);
      });
      final page = NowContextSelectionPage(
        auth: auth,
        client: c,
        workspaceChanges: workspace,
        organizationWorkspaceID: getter,
        onSelect: (_) => callbacks++,
      );
      await t.pumpWidget(
        MaterialApp(
          home: ValueListenableBuilder<bool>(
            valueListenable: trigger,
            builder: (_, v, child) {
              if (v) {
                workspace.value = selectionContext;
                workspace.value = null;
              }
              return child!;
            },
            child: page,
          ),
        ),
      );
      await t.pumpAndSettle();
      final before = calls;
      trigger.value = true;
      await t.pumpAndSettle();
      expect(find.text('账号或工作区已变化，请关闭后重新打开情境选择。'), findsOneWidget);
      expect(find.text('本人合成线上情境'), findsNothing);
      expect(calls, before);
      expect(callbacks, 0);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      workspace.dispose();
      trigger.dispose();
      auth.dispose();
    },
  );
  testWidgets(
    'actual 320 physical view large font and IME inset retain scrollable 48dp selection controls',
    (t) async {
      t.view.physicalSize = const Size(320, 640);
      t.view.devicePixelRatio = 1;
      t.view.viewInsets = const FakeViewPadding(bottom: 220);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      addTearDown(t.view.resetViewInsets);
      final auth = await egressAuth();
      final raw = selectionOptions();
      var callbacks = 0;
      final c = MockClient((r) async => selectionResponse(raw));
      final semantics = t.ensureSemantics();
      await t.pumpWidget(
        MaterialApp(
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: const TextScaler.linear(3)),
            child: child!,
          ),
          home: NowContextSelectionPage(
            auth: auth,
            client: c,
            onSelect: (_) => callbacks++,
          ),
        ),
      );
      await t.pumpAndSettle();
      await selectionTap(t, '本人合成线上情境');
      await t.scrollUntilVisible(
        find.text('核验并切换查看范围'),
        150,
        scrollable: find.byType(Scrollable).first,
      );
      await t.ensureVisible(find.text('核验并切换查看范围'));
      await t.pumpAndSettle();
      final f = find.widgetWithText(FilledButton, '核验并切换查看范围');
      expect(t.getSize(f).height, greaterThanOrEqualTo(48));
      expect(t.getBottomRight(f).dy, lessThanOrEqualTo(420));
      expect(t.getSemantics(f).getSemanticsData().label, contains('核验并切换查看范围'));
      expect(callbacks, 0);
      expect(t.takeException(), isNull);
      semantics.dispose();
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
}
