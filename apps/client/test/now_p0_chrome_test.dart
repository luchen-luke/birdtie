import 'package:birdtie_client/src/city/now_map_compass.dart';
import 'package:birdtie_client/src/workspace/top_controls.dart';
import 'package:birdtie_client/src/workspace/now_glass_surface.dart';
import 'package:flutter/material.dart';
import 'package:flutter/semantics.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  for (final tooltipsVisible in [true, false]) {
    testWidgets('社交按钮展开收起有稳定点击语义 tooltip=$tooltipsVisible', (t) async {
      final semantics = t.ensureSemantics();
      try {
        var open = false;
        var actions = 0;
        await t.pumpWidget(
          MaterialApp(
            home: StatefulBuilder(
              builder: (context, update) => TooltipVisibility(
                visible: tooltipsVisible,
                child: Align(
                  alignment: Alignment.topCenter,
                  child: TopControls(
                    contextLabel: 'Aberdeen',
                    socialExpanded: open,
                    onSidebar: () {},
                    onTools: () {},
                    onNotifications: () {},
                    onInbox: () => update(() {
                      open = !open;
                      actions++;
                    }),
                  ),
                ),
              ),
            ),
          ),
        );
        for (final expected in ['联系', '收起联系', '联系']) {
          await t.pumpAndSettle();
          final nodes = t.semantics
              .simulatedAccessibilityTraversal()
              .where((node) => node.label == expected)
              .toList();
          expect(nodes, hasLength(1), reason: '恰好一个可达按钮语义节点');
          final node = nodes.single;
          expect(node.label, expected);
          expect(node.hasFlag(SemanticsFlag.isButton), true);
          expect(node.getSemanticsData().hasAction(SemanticsAction.tap), true);
          if (actions < 2) {
            t.binding.pipelineOwner.semanticsOwner!.performAction(
              node.id,
              SemanticsAction.tap,
            );
            await t.pumpAndSettle();
          }
        }
        expect(actions, 2, reason: '语义操作调用原入口，展开收起各一次');
        expect(
          t.getSize(find.byKey(const Key('now-social-button'))),
          const Size(48, 48),
        );
        expect(t.takeException(), isNull);
      } finally {
        semantics.dispose();
      }
    });
  }
  for (final width in [320.0, 390.0, 430.0]) {
    for (final scale in [1.0, 1.3, 2.0]) {
      testWidgets('V4平面顶部、安全区与48触控 $width 字号$scale', (t) async {
        await t.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: MediaQuery(
                data: MediaQueryData(
                  size: Size(width, 844),
                  padding: const EdgeInsets.only(top: 32),
                  textScaler: TextScaler.linear(scale),
                ),
                child: Align(
                  alignment: Alignment.topLeft,
                  child: SizedBox(
                    width: width,
                    child: TopControls(
                      onSidebar: () {},
                      onInbox: () {},
                      onTools: () {},
                      onNotifications: () {},
                      contextLabel: 'Aberdeen',
                      onContext: () {},
                    ),
                  ),
                ),
              ),
            ),
          ),
        );
        await t.pumpAndSettle();
        final left = find.byKey(const Key('now-sidebar-button'));
        final right = find.byKey(const Key('now-social-button'));
        final notifications = find.byKey(const Key('now-notifications-button'));
        final center = find.byKey(const Key('now-city-picker'));
        expect(t.getCenter(left).dy, closeTo(t.getCenter(right).dy, .5));
        expect(t.getCenter(center).dy, closeTo(t.getCenter(left).dy, .5));
        expect(
          t.getCenter(notifications).dy,
          closeTo(t.getCenter(right).dy, .5),
        );
        expect(
          t.getTopLeft(notifications).dx,
          closeTo(t.getBottomRight(right).dx + 4, .5),
        );
        expect(
          t.getTopLeft(center).dx,
          closeTo(t.getBottomRight(left).dx + 4, .5),
        );
        expect(
          t.getBottomRight(center).dx,
          lessThanOrEqualTo(t.getTopLeft(right).dx),
        );
        final row = find
            .descendant(
              of: find.byType(TopControls),
              matching: find.byType(Row),
            )
            .first;
        expect(t.getTopLeft(row).dy, 32);
        expect(t.getSize(left), const Size(48, 48));
        expect(t.getSize(right), const Size(48, 48));
        expect(t.getSize(notifications), const Size(48, 48));
        expect(find.byKey(const Key('now-sidebar-capsule')), findsNothing);
        expect(find.byKey(const Key('now-city-capsule')), findsNothing);
        expect(t.widget<Icon>(find.byIcon(Icons.menu_rounded)).size, 20);
        expect(find.text('当前：Aberdeen'), findsNothing);
        expect(find.text('Aberdeen'), findsOneWidget);
        expect(t.takeException(), isNull);
      });
    }
  }
  testWidgets('顶部操作紧接安全区且清晰局部衬底仅包围自己的控件', (t) async {
    double? toolbarBottom;
    await t.pumpWidget(
      MaterialApp(
        home: MediaQuery(
          data: const MediaQueryData(
            size: Size(390, 844),
            padding: EdgeInsets.only(top: 32),
          ),
          child: Align(
            alignment: Alignment.topLeft,
            child: SizedBox(
              width: 390,
              child: TopControls(
                onSidebar: () {},
                onInbox: () {},
                onTools: () {},
                onNotifications: () {},
                onContext: () {},
                contextLabel: 'Aberdeen',
                onHeightChanged: (h) => toolbarBottom = h,
              ),
            ),
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    final toolbar = t.getRect(find.byKey(const Key('now-social-button')));
    expect(toolbar.top, 32);
    expect(toolbarBottom, closeTo(toolbar.bottom, .01));
    expect(find.byType(NowGlassSurface), findsNWidgets(4));
    expect(find.byType(BackdropFilter), findsNothing);
    final headerClips = find.descendant(
      of: find.byType(TopControls),
      matching: find.byType(ClipRRect),
    );
    expect(headerClips, findsNWidgets(4));
    for (final clip in headerClips.evaluate()) {
      final rect = t.getRect(
        find.byElementPredicate((e) => identical(e, clip)),
      );
      expect(rect.top, toolbar.top);
      expect(rect.bottom, toolbar.bottom);
      expect(rect.width, lessThan(390));
    }
    expect(
      find.byWidgetPredicate(
        (w) =>
            w is DecoratedBox &&
            w.decoration is BoxDecoration &&
            (w.decoration as BoxDecoration).gradient != null,
      ),
      findsNothing,
    );
    expect(t.takeException(), isNull);
  });
  testWidgets('联系与通知的未读语义和点击独立', (t) async {
    final handle = t.ensureSemantics();
    try {
      var contactTaps = 0, notificationTaps = 0;
      await t.pumpWidget(
        MaterialApp(
          home: TopControls(
            onSidebar: () {},
            onTools: () {},
            contextLabel: 'Aberdeen',
            onInbox: () => contactTaps++,
            onNotifications: () => notificationTaps++,
            hasUnreadMessages: true,
            hasUnreadNotifications: false,
          ),
        ),
      );
      final nodes = t.semantics.simulatedAccessibilityTraversal();
      expect(nodes.where((n) => n.label == '联系，有未读'), hasLength(1));
      expect(nodes.where((n) => n.label == '通知'), hasLength(1));
      expect(nodes.where((n) => n.label == '通知，有未读'), isEmpty);
      await t.tap(find.byKey(const Key('now-notifications-button')));
      expect((contactTaps, notificationTaps), (0, 1));
      await t.tap(find.byKey(const Key('now-social-button')));
      expect((contactTaps, notificationTaps), (1, 1));
    } finally {
      handle.dispose();
    }
  });
  testWidgets('指南针小表面、48触控与朝北动作', (t) async {
    var north = 0;
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: NowMapCompass(bearing: 90, onNorth: () => north++),
        ),
      ),
    );
    expect(
      t.getSize(find.byKey(const Key('now-map-compass'))),
      const Size(48, 48),
    );
    await t.tap(find.byTooltip('地图朝北'));
    expect(north, 1);
    expect(t.takeException(), isNull);
  });
}
