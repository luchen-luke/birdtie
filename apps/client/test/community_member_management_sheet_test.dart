import 'dart:io';
import 'dart:ui' as ui;

import 'package:birdtie_client/src/workspace/community_member_management_sheet.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'community_members_controller_test.dart';

void main() {
  testWidgets('小屏大字中文管理可滚动，预览前零写入', (t) async {
    final h = CommunityHarness();
    t.view.physicalSize = const Size(360, 740);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    addTearDown(h.dispose);
    final capture = Platform.environment['BIRDTIE_COMM_WIDGET_CAPTURE'] == '1';
    if (capture) {
      await t.runAsync(() async {
        // Local widget-only evidence. This is a Windows CJK test font, not
        // an Android screenshot, production data or production identity.
        final loader = FontLoader('CommCaptureCJK');
        loader.addFont(
          File(
            'C:/Windows/Fonts/msyh.ttc',
          ).readAsBytes().then(ByteData.sublistView),
        );
        await loader.load();
      });
    }
    await t.pumpWidget(
      MaterialApp(
        theme: capture ? ThemeData(fontFamily: 'CommCaptureCJK') : null,
        home: MediaQuery(
          data: const MediaQueryData(
            size: Size(360, 740),
            textScaler: TextScaler.linear(1.8),
          ),
          child: RepaintBoundary(
            key: const Key('comm-widget-capture'),
            child: Scaffold(
              body: CommunityMemberManagementSheet(controller: h.controller),
            ),
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.text('成员管理'), findsOneWidget);
    expect(h.writes, 0);
    expect(t.takeException(), isNull);
    await captureCommWidget(t, 'manager-360x740-text1_8.png');
    await t.scrollUntilVisible(
      find.text('检查通过'),
      160,
      scrollable: find.byType(Scrollable).first,
    );
    await t.tap(find.text('检查通过'));
    await t.pumpAndSettle();
    expect(h.previews, 1);
    expect(h.writes, 0);
    await t.scrollUntilVisible(
      find.text('检查操作'),
      -180,
      scrollable: find.byType(Scrollable).first,
    );
    expect(find.text('检查操作'), findsOneWidget);
    expect(t.takeException(), isNull);
    await captureCommWidget(t, 'confirmation-360x740-text1_8.png');
    await t.scrollUntilVisible(
      find.text('返回修改'),
      80,
      scrollable: find.byType(Scrollable).first,
    );
    await t.tap(find.text('返回修改'));
    await t.pumpAndSettle();
    expect(h.writes, 0);
    expect(h.controller.proposal, isNull);
  });
  testWidgets('管理员不提供管理员调整和所有权转让', (t) async {
    final h = CommunityHarness()..role = 'admin';
    addTearDown(h.dispose);
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: CommunityMemberManagementSheet(controller: h.controller),
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.scrollUntilVisible(
      find.byTooltip('管理 测试成员名字很长但应完整可阅读'),
      180,
      scrollable: find.byType(Scrollable).first,
    );
    await t.tap(find.byTooltip('管理 测试成员名字很长但应完整可阅读'));
    await t.pumpAndSettle();
    expect(find.text('移除成员'), findsOneWidget);
    expect(find.text('转让所有权'), findsNothing);
    expect(find.text('设为管理员'), findsNothing);
  });
  testWidgets('中文语义按钮具有48像素触达和可读标签', (t) async {
    final h = CommunityHarness();
    addTearDown(h.dispose);
    final semantics = t.ensureSemantics();
    try {
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: CommunityMemberManagementSheet(controller: h.controller),
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.byTooltip('关闭成员管理'), findsOneWidget);
      expect(
        t.getSize(find.byTooltip('关闭成员管理')).height,
        greaterThanOrEqualTo(48),
      );
      expect(t.takeException(), isNull);
    } finally {
      semantics.dispose();
    }
  });
}

Future<void> captureCommWidget(WidgetTester tester, String name) async {
  if (Platform.environment['BIRDTIE_COMM_WIDGET_CAPTURE'] != '1') return;
  await tester.runAsync(() async {
    final render = tester.renderObject<RenderRepaintBoundary>(
      find.byKey(const Key('comm-widget-capture')),
    );
    final image = await render.toImage(pixelRatio: 1);
    try {
      final data = await image.toByteData(format: ui.ImageByteFormat.png);
      if (data == null) throw StateError('widget image encoding unavailable');
      final directory = Directory(
        'D:/Project/birdtie/work/v4-comm002/widget-captures',
      );
      await directory.create(recursive: true);
      await File(
        '${directory.path}/$name',
      ).writeAsBytes(data.buffer.asUint8List());
    } finally {
      image.dispose();
    }
  });
}
