import 'package:birdtie_client/src/content/video_material_page.dart';
import 'package:birdtie_client/src/content/video_material_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'video_material_controller_test.dart';
import 'video_material_picker_test.dart';

void main() {
  testWidgets(
    'recording video selection leads while provider diagnostics stay secondary',
    (t) async {
      final requests = <String>[];
      final client = MockClient((r) async {
        requests.add(r.method);
        return r.url.path.endsWith('capabilities')
            ? videoResponse(videoCapabilities())
            : videoResponse({'items': []});
      });
      final c = videoController(client);
      try {
        await t.pumpWidget(MaterialApp(home: VideoMaterialPage(controller: c)));
        await t.pumpAndSettle();
        expect(find.text('添加视频'), findsOneWidget);
        expect(find.byKey(const Key('video-select')), findsOneWidget);
        expect(find.text('先在本机检查视频'), findsNothing);
        expect(find.text('重新核实存储能力'), findsNothing);
        expect(find.textContaining('存储服务：合成服务'), findsNothing);
        expect(find.text('选择和预览只在本机进行，不会上传或发送。'), findsOneWidget);
        await t.ensureVisible(find.text('保存说明'));
        await t.pumpAndSettle();
        await t.tap(find.text('保存说明'));
        await t.pumpAndSettle();
        expect(find.textContaining('存储服务：合成服务 · 区域：合成区域'), findsOneWidget);
        expect(find.textContaining('此处不会发送给 Agent、好友或公开发布'), findsOneWidget);
        expect(requests.every((method) => method == 'GET'), isTrue);
        expect(c.selection, isNull);
        expect(c.active, isNull);
        expect(t.takeException(), isNull);
      } finally {
        await t.pumpWidget(const SizedBox());
        c.dispose();
        client.close();
      }
    },
  );
  testWidgets('小屏大字选择播放移除可达，服务未配置保持明确空态', (t) async {
    t.view.physicalSize = const Size(320, 720);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final client = MockClient(
      (r) async => r.url.path.endsWith('capabilities')
          ? videoResponse(videoCapabilities(available: false))
          : videoResponse({'items': []}),
    );
    final picker = FakeVideoPicker()
      ..next = VideoMaterialSelection(
        bytes: syntheticVideoBytes(),
        preview: FakeVideoPreview(),
      );
    final c = videoController(client, picker: picker);
    try {
      await t.pumpWidget(
        MaterialApp(
          builder: (ctx, child) => MediaQuery(
            data: MediaQuery.of(
              ctx,
            ).copyWith(textScaler: const TextScaler.linear(2)),
            child: child!,
          ),
          home: VideoMaterialPage(controller: c),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('暂时无法保存视频，你仍可在本机选择和预览。'), findsOneWidget);
      expect(find.text('重试保存服务'), findsOneWidget);
      await t.tap(find.byKey(const Key('video-select')));
      await t.pumpAndSettle();
      expect(find.byKey(const Key('synthetic-video-preview')), findsOneWidget);
      await t.scrollUntilVisible(
        find.text('播放预览'),
        160,
        scrollable: find.byType(Scrollable).first,
      );
      await t.tap(find.text('播放预览'));
      await t.pumpAndSettle();
      expect(find.text('暂停'), findsOneWidget);
      await t.tap(find.text('移除本机视频'));
      await t.pumpAndSettle();
      expect(c.selection, isNull);
      await t.tap(find.byTooltip('本人已保存视频'));
      await t.pumpAndSettle();
      expect(find.text('还没有已保存视频。'), findsOneWidget);
      expect(t.takeException(), isNull);
    } finally {
      await t.pumpWidget(const SizedBox());
      c.dispose();
      client.close();
    }
  });
  testWidgets('安全检查受理显示等待状态，不能宣告保存完成', (t) async {
    final client = MockClient(
      (r) async => r.url.path.endsWith('capabilities')
          ? videoResponse(videoCapabilities())
          : videoResponse({
              'items': [videoReceipt(status: 'queued')],
            }),
    );
    final c = videoController(client);
    try {
      await t.pumpWidget(MaterialApp(home: VideoMaterialPage(controller: c)));
      await t.pumpAndSettle();
      await t.tap(find.byTooltip('本人已保存视频'));
      await t.pumpAndSettle();
      expect(find.text('等待安全检查'), findsOneWidget);
      expect(find.text('已保存为本人私有材料'), findsNothing);
      expect(t.takeException(), isNull);
    } finally {
      await t.pumpWidget(const SizedBox());
      c.dispose();
      client.close();
    }
  });
}
