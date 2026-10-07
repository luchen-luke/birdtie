import 'package:flutter/foundation.dart' show debugPrintSynchronously;
import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:birdtie_client/src/workspace/agent_conversation.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:birdtie_client/src/workspace/entity_peek_card.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'now_scope_recovery_test.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/workspace/top_controls.dart';
import 'package:birdtie_client/src/workspace/map_layer_controls.dart';
import 'package:birdtie_client/src/workspace/active_social_intent_card.dart';
import 'package:birdtie_client/src/workspace/social_intent_drafts.dart';

void main() {
  for (final sample in [
    (const Size(2656, 1220), 400.0, '横屏空间足够'),
    (const Size(1220, 2656), 1000.0, '纵屏空间足够'),
  ]) {
    testWidgets('无任务字号2.0与IME${sample.$3}保持四导航而不假设紧凑', (t) async {
      t.view.physicalSize = sample.$1;
      t.view.devicePixelRatio = 3.25;
      t.view.padding = const FakeViewPadding(top: 78);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      addTearDown(t.view.resetPadding);
      addTearDown(t.view.resetViewInsets);
      final f = NowFixture();
      addTearDown(f.dispose);
      await _mountInputHitFixture(t, f, scale: 2);
      final canvas = find.byType(MapCanvas).evaluate().single,
          publicMap = find.byType(PublicCityMapView).evaluate().single;
      t.view.viewInsets = FakeViewPadding(bottom: sample.$2);
      await t.enterText(nowField(), '空间足够的未发送草稿');
      await t.pumpAndSettle();
      final header = t.getRect(find.byType(TopControls)),
          composer = t.getRect(find.byType(AgentComposer));
      debugPrintSynchronously((
        jsonEncode({
          'tasklessShortEditorControl': sample.$3,
          'header': [header.top, header.bottom],
          'composer': [composer.top, composer.bottom],
          'overlap': header.overlaps(composer),
        })).toString());
      expect(header.overlaps(composer), false);
      expect(find.byKey(const Key('now-sheet-keyboard-restore')), findsNothing);
      for (final entry in [
        find.byTooltip('打开侧边栏'),
        find.byKey(const Key('now-city-picker')),
        find.byTooltip('打开更多工具'),
        find.byTooltip('打开收件箱'),
      ]) {
        expect(entry.hitTestable(), findsOneWidget);
      }
      await nowTap(t, find.byTooltip('打开更多工具'));
      expect(find.byKey(const Key('now-tools-menu')), findsOneWidget);
      await nowTap(t, find.byTooltip('关闭更多工具'));
      expect(t.widget<TextField>(nowField()).controller!.text, '空间足够的未发送草稿');
      expect(t.testTextInput.isVisible, false);
      expect(f.workspace(t).task, isNull);
      expect(f.source.queries, isEmpty);
      expect(identical(canvas, find.byType(MapCanvas).evaluate().single), true);
      expect(
        identical(publicMap, find.byType(PublicCityMapView).evaluate().single),
        true,
      );
      expect(t.takeException(), isNull);
      await f.unmount(t);
    });
  }
  for (final lines in [1, 2, 3, 4]) {
    testWidgets('无任务横屏字号2.0与IME的$lines行草稿有明确导航恢复', (t) async {
      t.view.physicalSize = const Size(2656, 1220);
      t.view.devicePixelRatio = 3.25;
      t.view.padding = const FakeViewPadding(top: 78);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      addTearDown(t.view.resetPadding);
      addTearDown(t.view.resetViewInsets);
      final f = NowFixture();
      addTearDown(f.dispose);
      await _mountInputHitFixture(t, f, scale: 2);
      final canvas = find.byType(MapCanvas),
          publicMap = find.byType(PublicCityMapView);
      final canvasElement = canvas.evaluate().single,
          canvasWidget = t.widget<MapCanvas>(canvas),
          publicElement = publicMap.evaluate().single,
          publicState = t.state(publicMap);
      final ws = f.workspace(t), extent = ws.sheetExtent, mode = ws.contentMode;
      final headerBefore = t.getRect(find.byType(TopControls));
      const bounds = MapBounds(
        west: -2.2,
        south: 57.1,
        east: -2.0,
        north: 57.2,
      );
      final view = t.widget<PublicCityMapView>(publicMap);
      view.onCameraMotion!();
      view.onViewportSettled!(bounds);
      await t.pumpAndSettle();
      final mapBounds = canvasWidget.mapState.viewportBounds,
          searchBounds = canvasWidget.mapState.searchAreaBounds;
      expect(ws.task, isNull);
      expect(ws.result, isNull);
      expect(f.source.queries, isEmpty);
      for (final entry in [
        find.byTooltip('打开侧边栏'),
        find.byKey(const Key('now-city-picker')),
        find.byTooltip('打开更多工具'),
        find.byTooltip('打开收件箱'),
      ]) {
        expect(entry.hitTestable(), findsOneWidget);
      }
      final draft = List.generate(
        lines,
        (i) => '第${i + 1}行尚未发送的安全草稿',
      ).join('\n');
      t.view.viewInsets = const FakeViewPadding(bottom: 844);
      await t.enterText(nowField(), draft);
      await t.pumpAndSettle();
      final composerRect = t.getRect(find.byType(AgentComposer));
      final currentHeader = find.byType(TopControls);
      final restore = find.byKey(const Key('now-sheet-keyboard-restore'));
      debugPrintSynchronously((
        jsonEncode({
          'tasklessShortEditor': 'real-mounted-MapWorkspace',
          'lines': lines,
          'physical': [2656, 1220],
          'dpr': 3.25,
          'fontScale': 2,
          'imePhysical': 844,
          'headerBefore': [headerBefore.top, headerBefore.bottom],
          'composer': [composerRect.top, composerRect.bottom],
          'headerStillMounted': currentHeader.evaluate().isNotEmpty,
          'headerOverlap':
              currentHeader.evaluate().isNotEmpty &&
              t.getRect(currentHeader).overlaps(composerRect),
          'restoreMounted': restore.evaluate().isNotEmpty,
          'queries': f.source.queries.length,
        })).toString());
      expect(
        currentHeader.evaluate().isNotEmpty &&
            t.getRect(currentHeader).overlaps(composerRect),
        false,
        reason: '短屏编辑不能把仍挂载的顶部导航藏在输入表面后面',
      );
      expect(currentHeader, findsNothing);
      expect(restore.hitTestable(), findsOneWidget);
      expect(find.byTooltip('收起键盘返回地图'), findsOneWidget);
      final restoreRect = t.getRect(restore),
          sendRect = t.getRect(find.byTooltip('发送需求')),
          plusRect = t.getRect(find.byTooltip('打开快捷操作'));
      expect(restoreRect.width, greaterThanOrEqualTo(48));
      expect(restoreRect.height, greaterThanOrEqualTo(48));
      expect(restoreRect.overlaps(sendRect), false);
      expect(restoreRect.overlaps(plusRect), false);
      expect(composerRect.top, greaterThanOrEqualTo(78 / 3.25));
      expect(composerRect.bottom, closeTo((1220 - 844) / 3.25 - 16, .5));
      expect(find.byTooltip('发送需求').hitTestable(), findsOneWidget);
      expect(find.byTooltip('打开快捷操作').hitTestable(), findsOneWidget);
      expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, true);
      expect(t.testTextInput.isVisible, true);
      await nowTap(t, restore);
      expect(t.widget<TextField>(nowField()).controller!.text, draft);
      expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, false);
      expect(t.testTextInput.isVisible, false);
      t.view.viewInsets = const FakeViewPadding();
      await t.pumpAndSettle();
      expect(find.byType(TopControls), findsOneWidget);
      for (final entry in [
        find.byTooltip('打开侧边栏'),
        find.byKey(const Key('now-city-picker')),
        find.byTooltip('打开更多工具'),
        find.byTooltip('打开收件箱'),
      ]) {
        expect(entry.hitTestable(), findsOneWidget);
        expect(t.getSize(entry).width, greaterThanOrEqualTo(48));
        expect(t.getSize(entry).height, greaterThanOrEqualTo(48));
      }
      await nowTap(t, find.byTooltip('打开更多工具'));
      expect(find.byKey(const Key('now-tools-menu')), findsOneWidget);
      await nowTap(t, find.byTooltip('关闭更多工具'));
      await nowTap(t, find.byKey(const Key('now-city-picker')));
      expect(find.byTooltip('取消选择城市').hitTestable(), findsOneWidget);
      await nowTap(t, find.byTooltip('取消选择城市'));
      await nowTap(t, find.byTooltip('打开收件箱'));
      expect(find.byType(BottomSheet), findsOneWidget);
      await nowBack(t);
      await nowTap(t, find.byTooltip('打开侧边栏'));
      expect(find.byType(Drawer).hitTestable(), findsOneWidget);
      await nowBack(t);
      expect(t.widget<TextField>(nowField()).controller!.text, draft);
      expect(t.testTextInput.isVisible, false);
      expect(ws.task, isNull);
      expect(ws.result, isNull);
      expect(ws.contentMode, mode);
      expect(ws.sheetExtent, extent);
      expect(f.source.queries, isEmpty);
      expect(f.city.selections, 0);
      expect(identical(mapBounds, canvasWidget.mapState.viewportBounds), true);
      expect(
        identical(searchBounds, canvasWidget.mapState.searchAreaBounds),
        true,
      );
      expect(identical(canvasElement, canvas.evaluate().single), true);
      expect(identical(canvasWidget, t.widget<MapCanvas>(canvas)), true);
      expect(identical(publicElement, publicMap.evaluate().single), true);
      expect(identical(publicState, t.state(publicMap)), true);
      expect(t.takeException(), isNull);
      await f.unmount(t);
    });
  }
  for (final scale in [1.0, 2.0]) {
    testWidgets('空闲地图区域搜索字号$scale位于实测顶部署名之后且一次提交', (t) async {
      t.view.physicalSize = const Size(320, 844);
      t.view.devicePixelRatio = 1;
      t.view.padding = const FakeViewPadding(top: 24, bottom: 20);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      addTearDown(t.view.resetPadding);
      final f = NowFixture();
      addTearDown(f.dispose);
      await _mountInputHitFixture(t, f, scale: scale);
      final publicMap = find.byType(PublicCityMapView);
      final element = publicMap.evaluate().single;
      final canvas = t.widget<MapCanvas>(find.byType(MapCanvas));
      final view = t.widget<PublicCityMapView>(publicMap);
      view.onCameraMotion!();
      view.onViewportSettled!(
        const MapBounds(west: -2.2, south: 57.1, east: -2.0, north: 57.2),
      );
      await t.pumpAndSettle();
      final search = find.widgetWithText(FilledButton, '搜索此区域');
      expect(search.hitTestable(), findsOneWidget);
      final rect = t.getRect(search),
          header = t.getRect(find.byType(TopControls));
      final ornamentTop = t.widget<PublicCityMapView>(publicMap).ornamentTop!;
      debugPrintSynchronously((
        jsonEncode({
          'idleSearchGeometry': 'actual-registered-camera-callback',
          'scale': scale,
          'searchRect': [rect.left, rect.top, rect.right, rect.bottom],
          'headerBottom': header.bottom,
          'ornamentTop': ornamentTop,
          'ornamentReservedHeight': 48,
          'mapElementRetained': identical(element, publicMap.evaluate().single),
          'queries': f.source.queries.length,
        })).toString());
      expect(rect.top, greaterThanOrEqualTo(ornamentTop + 48 + 8 - .5));
      expect(rect.overlaps(header), false);
      expect(
        find.descendant(
          of: find.byKey(const Key('now-native-context-scroll')),
          matching: search,
        ),
        findsOneWidget,
      );
      expect(f.source.queries, isEmpty);
      await nowTap(t, search);
      expect(f.source.queries, ['搜索此区域']);
      expect(find.widgetWithText(FilledButton, '搜索此区域'), findsNothing);
      expect(identical(element, publicMap.evaluate().single), true);
      expect(
        identical(canvas, t.widget<MapCanvas>(find.byType(MapCanvas))),
        true,
      );
      expect(t.takeException(), isNull);
      await f.unmount(t);
    });
  }
  for (final sample in [
    (390.0, 1.0, '甲验收城市'),
    (320.0, 2.0, '这是一座用于窄屏与大字号核验的长城市名称'),
  ]) {
    testWidgets('顶部主入口与更多工具${sample.$1}字号${sample.$2}避开结果和署名', (t) async {
      t.view.physicalSize = Size(sample.$1, 844);
      t.view.devicePixelRatio = 1;
      t.view.padding = const FakeViewPadding(top: 24, bottom: 20);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      addTearDown(t.view.resetPadding);
      addTearDown(t.view.resetViewInsets);
      final f = NowFixture();
      addTearDown(f.dispose);
      f.city.selection = PublicCity(
        id: 'alpha',
        name: sample.$3,
        region: '合成测试',
        contentStatus: 'test',
        map: null,
        source: NowFixtureCity.catalog.first.source,
      );
      await _mountInputHitFixture(t, f, scale: sample.$2);
      final canvas = find.byType(MapCanvas),
          publicMap = find.byType(PublicCityMapView);
      final canvasElement = canvas.evaluate().single,
          canvasWidget = t.widget<MapCanvas>(canvas);
      final publicElement = publicMap.evaluate().single,
          publicState = t.state(publicMap);
      final header = t.getRect(find.byType(TopControls));
      final secondary = <String, Rect>{};
      for (final label in ['我的社交意图', '地图图层', '选择查询情境']) {
        final finder = find.byTooltip(label);
        if (finder.evaluate().isNotEmpty) secondary[label] = t.getRect(finder);
      }
      debugPrintSynchronously((
        jsonEncode({
          'topToolsGeometry': 'idle',
          'width': sample.$1,
          'scale': sample.$2,
          'header': [header.left, header.top, header.right, header.bottom],
          'secondary': {
            for (final e in secondary.entries)
              e.key: [e.value.left, e.value.top, e.value.right, e.value.bottom],
          },
          'secondaryOverlapsPrimary': secondary.values.any(
            (r) => r.overlaps(header),
          ),
          'ornamentTop': t.widget<PublicCityMapView>(publicMap).ornamentTop,
        })).toString());
      for (final entry in secondary.entries) {
        expect(
          entry.value.overlaps(header),
          false,
          reason: '${entry.key}不应压在第一排主入口上',
        );
      }
      expect(secondary, isEmpty, reason: '地图工具按原提案收敛到可达的更多工具，不常驻第二排');
      final more = find.byTooltip('打开更多工具');
      expect(more.hitTestable(), findsOneWidget);
      final primaries = [
        find.byTooltip('打开侧边栏'),
        find.byKey(const Key('now-city-picker')),
        more,
        find.byTooltip('打开收件箱'),
      ];
      for (var i = 0; i < primaries.length; i++) {
        final rect = t.getRect(primaries[i]);
        expect(rect.width, greaterThanOrEqualTo(48));
        expect(rect.height, greaterThanOrEqualTo(48));
        expect(rect.left, greaterThanOrEqualTo(0));
        expect(rect.right, lessThanOrEqualTo(sample.$1));
        for (var j = i + 1; j < primaries.length; j++) {
          expect(rect.overlaps(t.getRect(primaries[j])), false);
        }
      }
      final top = t.widget<PublicCityMapView>(publicMap).ornamentTop!;
      expect(top, closeTo(header.bottom + 8, .5));
      await nowSend(t, '找地点');
      final ws = f.workspace(t),
          task = f.workspace(t).task,
          result = f.workspace(t).result;
      ws.selectEntity('place:retained-tool-selection');
      for (final extent in [
        AgentSheetExtent.peek,
        AgentSheetExtent.medium,
        AgentSheetExtent.expanded,
      ]) {
        ws.setSheetExtent(extent);
        await t.pumpAndSettle();
        for (final inset in [0.0, 220.0]) {
          t.view.viewInsets = FakeViewPadding(bottom: inset);
          await t.enterText(nowField(), '尚未发送的安全草稿');
          await t.pumpAndSettle();
          final sheet = t.getRect(find.byType(AgentResultsSheet));
          final currentHeader = t.getRect(find.byType(TopControls));
          expect(
            sheet.top,
            greaterThanOrEqualTo(currentHeader.bottom + 8 + 48 + 8 - .5),
          );
          for (final entry in primaries) {
            expect(sheet.overlaps(t.getRect(entry)), false);
          }
          await nowTap(t, more);
          final modal = find.byKey(const Key('now-tools-menu'));
          expect(modal, findsOneWidget);
          final modalRect = t.getRect(find.byType(BottomSheet));
          expect(modalRect.top, greaterThanOrEqualTo(24));
          final close = find.byTooltip('关闭更多工具');
          expect(close.hitTestable(), findsOneWidget);
          expect(t.getSize(close).height, greaterThanOrEqualTo(48));
          for (final label in ['地图图层', '选择查询情境', '我的社交意图', '打开意图草稿']) {
            final action = find.byTooltip(label);
            await t.ensureVisible(action);
            await t.pumpAndSettle();
            expect(action.hitTestable(), findsOneWidget);
            expect(t.getSize(action).height, greaterThanOrEqualTo(48));
            expect(t.getSize(action).width, greaterThanOrEqualTo(48));
          }
          await nowTap(t, close);
          expect(find.byKey(const Key('now-tools-menu')), findsNothing);
          expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, false);
          expect(t.widget<TextField>(nowField()).controller!.text, '尚未发送的安全草稿');
          expect(ws.sheetExtent, extent);
          expect(ws.contentMode, AgentContentMode.results);
          expect(ws.selectedEntityId, 'place:retained-tool-selection');
          expect(identical(ws.task, task), true);
          expect(identical(ws.result, result), true);
          expect(identical(canvasElement, canvas.evaluate().single), true);
          expect(identical(canvasWidget, t.widget<MapCanvas>(canvas)), true);
          expect(identical(publicElement, publicMap.evaluate().single), true);
          expect(identical(publicState, t.state(publicMap)), true);
          expect(t.widget<PublicCityMapView>(publicMap).ornamentTop, top);
        }
      }
      expect(f.source.queries, ['找地点']);
      expect(t.takeException(), isNull);
      await f.unmount(t);
    });
  }
  testWidgets('更多工具复用四个真实旧入口且关闭后保留草稿与地图', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await _mountInputHitFixture(t, f);
    final map = find.byType(MapCanvas).evaluate().single;
    await t.enterText(nowField(), '不替我提交的草稿');
    await t.pumpAndSettle();
    for (final label in ['地图图层', '选择查询情境', '我的社交意图', '打开意图草稿']) {
      await nowTap(t, find.byTooltip('打开更多工具'));
      await nowTap(t, find.byTooltip(label));
      expect(find.byKey(const Key('now-tools-menu')), findsNothing);
      switch (label) {
        case '地图图层':
          expect(find.byType(MapLayerControls), findsOneWidget);
        case '选择查询情境':
          expect(find.text('请先登录本人账号，再选择线上情境。'), findsOneWidget);
        case '我的社交意图':
          expect(find.byType(ActiveSocialIntentCard), findsOneWidget);
        case '打开意图草稿':
          expect(find.byType(SocialIntentDraftPage), findsOneWidget);
      }
      await nowBack(t);
      expect(find.byTooltip('打开更多工具').hitTestable(), findsOneWidget);
      expect(identical(map, find.byType(MapCanvas).evaluate().single), true);
      expect(t.widget<TextField>(nowField()).controller!.text, '不替我提交的草稿');
      expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, false);
      expect(f.source.queries, isEmpty);
    }
    expect(t.takeException(), isNull);
    await f.unmount(t);
  });
  for (final change in ['账号ABA', '接口ABA', '城市ABA']) {
    testWidgets('更多工具$change旧菜单动作不复活，当前新入口仍可用', (t) async {
      final f = NowFixture();
      addTearDown(f.dispose);
      await _mountInputHitFixture(t, f);
      final map = find.byType(MapCanvas).evaluate().single;
      await nowTap(t, find.byTooltip('打开更多工具'));
      final action = find.byTooltip('打开意图草稿');
      await t.ensureVisible(action);
      await t.pumpAndSettle();
      expect(action.hitTestable(), findsOneWidget);
      final oldAction = t
          .widget<ListTile>(
            find.descendant(of: action, matching: find.byType(ListTile)),
          )
          .onTap!;
      switch (change) {
        case '账号ABA':
          f.auth.changeIdentity('Bearer next', nextOwner: 'next');
          f.auth.changeIdentity(null);
        case '接口ABA':
          f.base = 'http://next-fixture.test';
          await _mountInputHitFixture(t, f);
          f.base = 'http://now-fixture.test';
          await _mountInputHitFixture(t, f);
        case '城市ABA':
          f.city.selectCity('beta');
          f.city.selectCity('alpha');
      }
      await t.pumpAndSettle();
      expect(find.text('请重新打开内容'), findsOneWidget);
      expect(find.byKey(const Key('now-tools-menu')), findsNothing);
      oldAction();
      await t.pumpAndSettle();
      expect(find.byType(SocialIntentDraftPage), findsNothing);
      expect(f.source.queries, isEmpty);
      await nowBack(t);
      expect(find.byTooltip('打开更多工具').hitTestable(), findsOneWidget);
      await nowTap(t, find.byTooltip('打开更多工具'));
      await nowTap(t, find.byTooltip('打开意图草稿'));
      expect(find.byType(SocialIntentDraftPage), findsOneWidget);
      await nowBack(t);
      expect(identical(map, find.byType(MapCanvas).evaluate().single), true);
      expect(f.source.queries, isEmpty);
      expect(t.takeException(), isNull);
      await f.unmount(t);
    });
  }
  for (final feedback in [false, true]) {
    testWidgets('横屏字号2.0与IME长稿受限且素材提示$feedback不挤没输入', (t) async {
      t.view.physicalSize = const Size(2656, 1220);
      t.view.devicePixelRatio = 3.25;
      t.view.padding = const FakeViewPadding(top: 78);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      addTearDown(t.view.resetPadding);
      addTearDown(t.view.resetViewInsets);
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(
            SystemChannels.platform,
            (call) async => null,
          );
      addTearDown(() {
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
            .setMockMethodCallHandler(SystemChannels.platform, null);
      });
      final f = NowFixture();
      addTearDown(f.dispose);
      await _mountInputHitFixture(t, f, scale: 2);
      await nowSend(t, '找地点');
      final ws = f.workspace(t);
      ws.showContent(AgentContentMode.conversation);
      ws.setSheetExtent(AgentSheetExtent.peek);
      await t.pumpAndSettle();
      final task = ws.task, result = ws.result;
      final map = t.widget<MapCanvas>(find.byType(MapCanvas));
      if (feedback) {
        await nowTap(t, find.byTooltip('打开快捷操作'));
        await nowTap(t, find.text('粘贴文字/链接'));
        expect(find.text('剪贴板没有可粘贴的文字或链接。'), findsOneWidget);
      }
      const draft =
          '第一行保留尚未发送的长文字\n第二行保留尚未发送的长文字\n第三行保留尚未发送的长文字\n第四行保留尚未发送的长文字';
      t.view.viewInsets = const FakeViewPadding(bottom: 844);
      await t.enterText(nowField(), draft);
      await t.pumpAndSettle();
      final composer = find.byType(AgentComposer);
      final composerRect = t.getRect(composer);
      final field = t.widget<TextField>(nowField());
      final fieldRect = t.getRect(nowField());
      final plus = find.byTooltip('打开快捷操作');
      final send = find.byTooltip('发送需求');
      final safeTop = 78 / 3.25;
      final visibleBottom = (1220 - 844) / 3.25;
      debugPrintSynchronously((
        jsonEncode({
          'composerHeight': 'actual-landscape-font2-component',
          'feedback': feedback,
          'composer': [composerRect.top, composerRect.bottom],
          'field': [fieldRect.top, fieldRect.bottom],
          'safeTop': safeTop,
          'visibleBottom': visibleBottom,
        })).toString());
      expect(composerRect.top, greaterThanOrEqualTo(safeTop));
      expect(composerRect.bottom, closeTo(visibleBottom - 16, .5));
      expect(fieldRect.top, greaterThanOrEqualTo(safeTop));
      expect(fieldRect.bottom, lessThanOrEqualTo(visibleBottom));
      expect(fieldRect.height, greaterThanOrEqualTo(48));
      expect(plus.hitTestable(), findsOneWidget);
      expect(send.hitTestable(), findsOneWidget);
      expect(t.getSize(plus).height, greaterThanOrEqualTo(48));
      expect(t.getSize(send).height, greaterThanOrEqualTo(48));
      expect(field.maxLines, 4);
      expect(MediaQuery.textScalerOf(t.element(nowField())).scale(16), 32);
      expect(field.focusNode!.hasFocus, true);
      expect(t.testTextInput.isVisible, true);
      final internalScroll = find.descendant(
        of: find.byType(EditableText),
        matching: find.byType(Scrollable),
      );
      final position = t.state<ScrollableState>(internalScroll).position;
      expect(position.maxScrollExtent, greaterThan(0));
      final atCaret = position.pixels;
      await t.drag(nowField(), const Offset(0, 60));
      await t.pumpAndSettle();
      expect(position.pixels, lessThan(atCaret));
      expect(field.controller!.text, draft);
      expect(ws.sheetExtent, AgentSheetExtent.peek);
      expect(ws.contentMode, AgentContentMode.conversation);
      expect(identical(ws.task, task), true);
      expect(identical(ws.result, result), true);
      expect(identical(t.widget<MapCanvas>(find.byType(MapCanvas)), map), true);
      expect(f.source.queries, ['找地点']);
      if (feedback) {
        await nowTap(t, plus);
        expect(find.text('剪贴板没有可粘贴的文字或链接。').hitTestable(), findsOneWidget);
        expect(field.controller!.text, draft);
        await nowBack(t);
      }
      final restore = find.byKey(const Key('now-sheet-keyboard-restore'));
      await nowTap(t, restore);
      expect(field.focusNode!.hasFocus, false);
      expect(t.testTextInput.isVisible, false);
      expect(field.controller!.text, draft);
      expect(ws.sheetExtent, AgentSheetExtent.peek);
      expect(ws.contentMode, AgentContentMode.conversation);
      t.view.viewInsets = const FakeViewPadding();
      await t.pumpAndSettle();
      expect(find.byType(AgentResultsSheet), findsOneWidget);
      expect(t.takeException(), isNull);
      await f.unmount(t);
    });
  }
  for (final mode in AgentContentMode.values) {
    for (final extent in [
      AgentSheetExtent.peek,
      AgentSheetExtent.medium,
      AgentSheetExtent.expanded,
    ]) {
      testWidgets('短屏IME回退收键盘保留$mode与$extent及选中草稿', (t) async {
        t.view.physicalSize = const Size(360, 640);
        t.view.devicePixelRatio = 1;
        t.view.padding = const FakeViewPadding(top: 24, bottom: 24);
        addTearDown(t.view.resetPhysicalSize);
        addTearDown(t.view.resetDevicePixelRatio);
        addTearDown(t.view.resetPadding);
        addTearDown(t.view.resetViewInsets);
        final f = NowFixture();
        addTearDown(f.dispose);
        f.source.pending = Completer<AgentResult>();
        await _mountInputHitFixture(t, f, scale: 1.7);
        await t.enterText(nowField(), '查找公开地点');
        await t.testTextInput.receiveAction(TextInputAction.send);
        await t.pump();
        const entity = MapEntity(
          id: 'place:keyboard-fallback-synthetic',
          kind: MapEntityKind.place,
          title: '合成已选公开地点',
          subtitle: '仅回退组件验收',
          latitude: 57.15,
          longitude: -2.1,
        );
        f.source.pending!.complete(
          AgentResult(
            entities: const [entity],
            activities: const [],
            places: const [],
            note: '合成当前结果：键盘动作保持当前任务。',
            task: f.source.response('查找公开地点').task,
            resultSet: AgentResultSet(
              id: 'keyboard-fallback-synthetic-result',
              status: 'ready',
              generatedAt: DateTime.utc(2026, 10, 6),
              schema: typedAgentResultSchema,
              entities: const [
                AgentResultRef(
                  type: 'place',
                  id: 'keyboard-fallback-synthetic',
                ),
              ],
              items: const [
                AgentResultItem(
                  entity: AgentResultRef(
                    type: 'place',
                    id: 'keyboard-fallback-synthetic',
                  ),
                  title: '合成已选公开地点',
                  summary: '仅回退组件验收',
                  scope: 'AUTHORIZED_VIEW',
                  anchor: AgentResultAnchor(
                    precision: 'point',
                    latitude: 57.15,
                    longitude: -2.1,
                  ),
                ),
              ],
            ),
          ),
        );
        await t.pumpAndSettle();
        final ws = f.workspace(t);
        ws.selectEntity(entity.id);
        ws.showContent(mode);
        ws.setSheetExtent(extent);
        await t.pumpAndSettle();
        final task = ws.task, result = ws.result;
        final originalMap = t.widget<MapCanvas>(find.byType(MapCanvas));
        t.view.padding = const FakeViewPadding(top: 24);
        t.view.viewInsets = const FakeViewPadding(bottom: 380);
        const draft = '保留的第一行\n第二行尚未发送\n第三行原地点条件\n第四行原时间条件';
        await t.enterText(nowField(), draft);
        await t.pumpAndSettle();
        final field = t.widget<TextField>(nowField());
        final editingValue = field.controller!.value;
        expect(field.focusNode!.hasFocus, true);
        expect(t.testTextInput.isVisible, true);
        expect(ws.sheetExtent, extent);
        expect(ws.contentMode, mode);
        final recover = find.byKey(const Key('now-sheet-keyboard-restore'));
        expect(recover, findsOneWidget);
        expect(recover.hitTestable(), findsOneWidget);
        final recoverRect = t.getRect(recover);
        expect(recoverRect.width, greaterThanOrEqualTo(48));
        expect(recoverRect.height, greaterThanOrEqualTo(48));
        expect(recoverRect.top, greaterThanOrEqualTo(24));
        expect(recoverRect.bottom, lessThanOrEqualTo(640 - 380));
        expect(
          find.byKey(const Key('now-context-keyboard-restore')),
          findsNothing,
        );
        await nowTap(t, recover);
        debugPrintSynchronously((
          jsonEncode({
            'keyboardFallback': 'actual-mounted-hit',
            'expectedMode': mode.name,
            'actualMode': ws.contentMode.name,
            'expectedExtent': extent.name,
            'actualExtent': ws.sheetExtent.name,
            'ownFocus': field.focusNode!.hasFocus,
            'imeVisible': t.testTextInput.isVisible,
            'controlRect': [
              recoverRect.left,
              recoverRect.top,
              recoverRect.right,
              recoverRect.bottom,
            ],
          })).toString());
        expect(field.focusNode!.hasFocus, false);
        expect(t.testTextInput.isVisible, false);
        expect(field.controller!.value, editingValue);
        expect(ws.sheetExtent, extent);
        expect(ws.contentMode, mode);
        expect(ws.selectedEntityId, entity.id);
        expect(identical(ws.task, task), true);
        expect(identical(ws.result, result), true);
        expect(f.source.queries, ['查找公开地点']);
        t.view.viewInsets = const FakeViewPadding();
        t.view.padding = const FakeViewPadding(top: 24, bottom: 24);
        await t.pumpAndSettle();
        expect(find.byType(AgentResultsSheet), findsOneWidget);
        expect(
          find.byType(EntityPeekCard),
          extent == AgentSheetExtent.expanded ? findsNothing : findsOneWidget,
        );
        expect(ws.sheetExtent, extent);
        expect(ws.contentMode, mode);
        expect(ws.selectedEntityId, entity.id);
        expect(identical(ws.task, task), true);
        expect(identical(ws.result, result), true);
        expect(field.controller!.value, editingValue);
        expect(
          identical(t.widget<MapCanvas>(find.byType(MapCanvas)), originalMap),
          true,
        );
        expect(find.byTooltip('打开侧边栏').hitTestable(), findsOneWidget);
        expect(t.takeException(), isNull);
        await f.unmount(t);
      });
    }
  }
  testWidgets('轻量焦点事件默认关闭且显式观察不改变tap取消与编辑暂停', (t) async {
    const enabled = bool.fromEnvironment('BIRDTIE_COMPOSER_FOCUS_EVENTS');
    const prefix = 'BIRDTIE_COMPOSER_FOCUS_EVENTS ';
    const privateDraft = 'SAFE_PRIVATE_EVENTS_DRAFT_MUST_NOT_BE_LOGGED';
    final lines = <String>[];
    final previousPrint = debugPrint;
    debugPrint = (message, {wrapWidth}) {
      if (message?.startsWith(prefix) ?? false) {
        lines.add(message!);
      } else {
        previousPrint(message, wrapWidth: wrapWidth);
      }
    };
    try {
      final f = NowFixture(selected: false);
      addTearDown(f.dispose);
      await _mountInputHitFixture(t, f);
      final state = t.state<AgentComposerState>(find.byType(AgentComposer));
      final field = t.widget<TextField>(nowField());
      final central = find.ancestor(
        of: nowField(),
        matching: find.byType(GestureDetector),
      );
      final rect = t.getRect(central);
      final blank = Offset(rect.center.dx, rect.top + 2);
      final drag = await t.startGesture(blank);
      await t.pump(const Duration(milliseconds: 120));
      await drag.moveBy(const Offset(0, -100));
      await t.pump();
      await drag.up();
      await t.pumpAndSettle();
      expect(field.focusNode!.hasFocus, false);
      expect(t.testTextInput.isVisible, false);
      expect(f.source.queries, isEmpty);
      await t.tapAt(blank);
      await t.pumpAndSettle();
      expect(field.focusNode!.hasFocus, true);
      expect(t.testTextInput.isVisible, true);
      const value = TextEditingValue(
        text: privateDraft,
        selection: TextSelection(baseOffset: 2, extentOffset: 5),
      );
      t.testTextInput.updateEditingValue(value);
      await t.pumpAndSettle();
      state.pauseEditing();
      await t.pumpAndSettle();
      expect(field.focusNode!.hasFocus, false);
      expect(t.testTextInput.isVisible, false);
      expect(field.controller!.value, value);
      state.resumeEditing();
      await t.pumpAndSettle();
      expect(field.focusNode!.hasFocus, true);
      expect(t.testTextInput.isVisible, true);
      expect(field.controller!.value, value);
      expect(f.source.queries, isEmpty);
      if (enabled) {
        expect(lines, isNotEmpty);
        final records = lines
            .map((line) => jsonDecode(line.substring(prefix.length)))
            .cast<Map<String, dynamic>>()
            .toList();
        const safeFields = {
          'event',
          'sequence',
          'elapsedMicros',
          'mounted',
          'hasFocus',
          'hasPrimaryFocus',
          'canRequestFocus',
          'ownNodeHash',
          'primaryType',
          'primaryHash',
          'parentAttached',
          'ancestorCount',
          'blockedAncestorCount',
          'focusContextType',
          'keyboardPath',
          'editableFound',
          'editableMounted',
          'editableSameNode',
        };
        expect(records.every((r) => r.keys.every(safeFields.contains)), true);
        expect(lines.join(), isNot(contains(privateDraft)));
        final events = records.map((r) => r['event']).toList();
        for (final event in [
          'init',
          'central_tap_down',
          'central_tap_cancel',
          'central_tap',
          'resume_before',
          'resume_keyboard_path',
          'resume_after',
          'own_focus_change',
          'manager_focus_change',
          'pause_before',
          'pause_after',
        ]) {
          expect(events, contains(event));
        }
        final path = records.firstWhere(
          (r) => r['event'] == 'resume_keyboard_path',
        );
        expect(path['keyboardPath'], 'requestKeyboard');
        expect(path['editableFound'], true);
        expect(path['editableMounted'], true);
        expect(path['editableSameNode'], true);
        expect(path['focusContextType'], 'Focus');
        expect(path['parentAttached'], true);
        expect(path['blockedAncestorCount'], 0);
        for (var i = 1; i < records.length; i++) {
          expect(
            records[i]['sequence'],
            greaterThan(records[i - 1]['sequence'] as int),
          );
          expect(
            records[i]['elapsedMicros'],
            greaterThanOrEqualTo(records[i - 1]['elapsedMicros'] as int),
          );
        }
      } else {
        expect(lines, isEmpty);
      }
      await f.unmount(t);
      final count = lines.length;
      await t.pumpWidget(
        const MaterialApp(
          home: Scaffold(body: Focus(autofocus: true, child: SizedBox())),
        ),
      );
      await t.pumpAndSettle();
      expect(lines.length, count);
      expect(t.takeException(), isNull);
    } finally {
      debugPrint = previousPrint;
    }
  });
  for (final scale in [1.0, 1.7]) {
    testWidgets('已选实体与区域搜索$scale字号在一至四行IME下不覆盖结果正文', (t) async {
      t.view.physicalSize = const Size(1220, 2656);
      t.view.devicePixelRatio = 3.25;
      t.view.padding = const FakeViewPadding(top: 150, bottom: 52);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      addTearDown(t.view.resetPadding);
      addTearDown(t.view.resetViewInsets);
      final f = NowFixture();
      addTearDown(f.dispose);
      f.source.pending = Completer<AgentResult>();
      await _mountInputHitFixture(t, f, scale: scale);
      await t.enterText(nowField(), '查找公开地点');
      await t.testTextInput.receiveAction(TextInputAction.send);
      await t.pump();
      const entity = MapEntity(
        id: 'place:overlap-synthetic',
        kind: MapEntityKind.place,
        title: '合成公开地点选中卡片与当前任务应当保持',
        subtitle: '仅组件几何验收',
        latitude: 57.15,
        longitude: -2.1,
      );
      f.source.pending!.complete(
        AgentResult(
          entities: const [entity],
          activities: const [],
          places: const [],
          note: '合成结果正文：保留当前地点与未发送草稿。',
          task: f.source.response('查找公开地点').task,
          resultSet: AgentResultSet(
            id: 'geometry-synthetic-result',
            status: 'ready',
            generatedAt: DateTime.utc(2026, 10, 6),
            schema: typedAgentResultSchema,
            entities: const [
              AgentResultRef(type: 'place', id: 'overlap-synthetic'),
            ],
            items: const [
              AgentResultItem(
                entity: AgentResultRef(type: 'place', id: 'overlap-synthetic'),
                title: '合成公开地点选中卡片与当前任务应当保持',
                summary: '仅组件几何验收',
                scope: 'AUTHORIZED_VIEW',
                anchor: AgentResultAnchor(
                  precision: 'point',
                  latitude: 57.15,
                  longitude: -2.1,
                ),
              ),
            ],
          ),
        ),
      );
      await t.pumpAndSettle();
      final ws = f.workspace(t);
      ws.selectEntity(entity.id);
      ws.setSheetExtent(AgentSheetExtent.medium);
      final map = t.widget<MapCanvas>(find.byType(MapCanvas));
      const originalBounds = MapBounds(
        west: -2.2,
        south: 57.1,
        east: -2.0,
        north: 57.2,
      );
      const movedBounds = MapBounds(
        west: -2.1,
        south: 57.1,
        east: -1.9,
        north: 57.2,
      );
      map.mapState.initializeViewport(originalBounds);
      map.mapState.cameraStarted();
      map.mapState.cameraSettled(movedBounds);
      await t.pumpAndSettle();
      final task = ws.task, result = ws.result;
      t.view.padding = const FakeViewPadding(top: 150);
      t.view.viewInsets = const FakeViewPadding(bottom: 1058);
      for (var count = 1; count <= 4; count++) {
        final draft = List.generate(count, (i) => '安全未发送第${i + 1}行').join('\n');
        await t.enterText(nowField(), draft);
        await t.pumpAndSettle();
        final peek = find.byType(EntityPeekCard);
        final search = find.widgetWithText(FilledButton, '搜索此区域');
        final sheet = find.byType(AgentResultsSheet);
        final composerRect = t.getRect(find.byType(AgentComposer));
        final peekRect = t.getRect(peek), searchRect = t.getRect(search);
        final sheetRect = t.getRect(sheet);
        final lane = find.byKey(const Key('now-selected-context-scroll'));
        final laneRect = lane.evaluate().isEmpty ? null : t.getRect(lane);
        final paintedPeek = laneRect == null
            ? peekRect
            : peekRect.intersect(laneRect);
        final paintedSearch = laneRect == null
            ? searchRect
            : searchRect.intersect(laneRect);
        debugPrintSynchronously((
          jsonEncode({
            'geometryCase': 'selected-area-multiline',
            'scale': scale,
            'lines': count,
            'peek': [peekRect.top, peekRect.bottom],
            'search': [searchRect.top, searchRect.bottom],
            'laneViewport': laneRect == null
                ? null
                : [laneRect.top, laneRect.bottom],
            'paintedPeek': [paintedPeek.top, paintedPeek.bottom],
            'sheet': [sheetRect.top, sheetRect.bottom],
            'composer': [composerRect.top, composerRect.bottom],
          })).toString());
        expect(peek, findsOneWidget);
        expect(search, findsOneWidget);
        expect(sheetRect.top, greaterThanOrEqualTo(paintedPeek.bottom + 7.5));
        expect(sheetRect.top, greaterThanOrEqualTo(paintedSearch.bottom + 7.5));
        expect(peekRect.top, greaterThanOrEqualTo(searchRect.bottom + 7.5));
        expect(sheetRect.bottom, lessThanOrEqualTo(composerRect.top - 11.5));
        expect(composerRect.bottom, closeTo((2656 - 1058) / 3.25 - 16, .5));
        if (laneRect != null) {
          expect(sheetRect.top, greaterThanOrEqualTo(laneRect.bottom + 11.5));
        }
        final view = find.descendant(of: peek, matching: find.text('查看'));
        await t.ensureVisible(view);
        await t.pumpAndSettle();
        expect(view.hitTestable(), findsOneWidget);
        expect(
          t.getRect(view).intersect(t.getRect(lane)).bottom,
          lessThan(sheetRect.top),
        );
        await t.ensureVisible(search);
        await t.pumpAndSettle();
        expect(search.hitTestable(), findsOneWidget);
        expect(
          find.descendant(of: sheet, matching: find.text('继续对话')).hitTestable(),
          findsOneWidget,
        );
        expect(t.widget<TextField>(nowField()).controller!.text, draft);
        expect(ws.selectedEntityId, entity.id);
        expect(identical(ws.task, task), true);
        expect(identical(ws.result, result), true);
        expect(ws.contentMode, AgentContentMode.results);
        expect(ws.sheetExtent, AgentSheetExtent.medium);
        expect(f.source.queries, ['查找公开地点']);
        expect(
          identical(t.widget<MapCanvas>(find.byType(MapCanvas)), map),
          true,
        );
        expect(map.mapState.searchAreaBounds, movedBounds);
        expect(t.takeException(), isNull);
      }
      final recover = find.byKey(const Key('now-context-keyboard-restore'));
      if (scale == 1.7) {
        ws.setSheetExtent(AgentSheetExtent.peek);
        await t.pumpAndSettle();
        await t.ensureVisible(recover);
        await t.pumpAndSettle();
        expect(recover.hitTestable(), findsOneWidget);
        final mode = find.descendant(
          of: find.byType(AgentResultsSheet),
          matching: find.text('继续对话'),
        );
        expect(t.getRect(recover).overlaps(t.getRect(mode)), false);
        expect(t.getSize(recover).height, greaterThanOrEqualTo(48));
        await nowTap(t, recover);
        expect(t.testTextInput.isVisible, false);
        expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, false);
        t.view.viewInsets = const FakeViewPadding();
        t.view.padding = const FakeViewPadding(top: 150, bottom: 52);
        await t.pumpAndSettle();
        expect(find.byType(EntityPeekCard), findsOneWidget);
        expect(ws.selectedEntityId, entity.id);
        expect(ws.contentMode, AgentContentMode.results);
        expect(ws.sheetExtent, AgentSheetExtent.peek);
        expect(identical(ws.task, task), true);
        expect(
          t.widget<TextField>(nowField()).controller!.text,
          '安全未发送第1行\n安全未发送第2行\n安全未发送第3行\n安全未发送第4行',
        );
        expect(f.source.queries, ['查找公开地点']);
        expect(t.takeException(), isNull);
        await nowTap(t, find.byKey(const Key('agent-sheet-expand-summary')));
        expect(ws.sheetExtent, AgentSheetExtent.medium);
        expect(find.text('合成结果正文：保留当前地点与未发送草稿。').hitTestable(), findsOneWidget);
      }
      t.view.viewInsets = const FakeViewPadding();
      t.view.padding = const FakeViewPadding(top: 150, bottom: 52);
      await t.pumpAndSettle();
      for (final mode in AgentContentMode.values) {
        ws.showContent(mode);
        for (final extent in [
          AgentSheetExtent.peek,
          AgentSheetExtent.medium,
          AgentSheetExtent.expanded,
        ]) {
          ws.setSheetExtent(extent);
          t.view.viewInsets = const FakeViewPadding(bottom: 1058);
          t.view.padding = const FakeViewPadding(top: 150);
          await t.enterText(
            nowField(),
            '安全未发送第1行\n安全未发送第2行\n安全未发送第3行\n安全未发送第4行',
          );
          await t.pumpAndSettle();
          await t.ensureVisible(recover);
          await t.pumpAndSettle();
          expect(recover.hitTestable(), findsOneWidget);
          expect(t.getSize(recover).height, greaterThanOrEqualTo(48));
          final modeControl = find.descendant(
            of: find.byType(AgentResultsSheet),
            matching: find.text(
              mode == AgentContentMode.results ? '继续对话' : '查看结果',
            ),
          );
          expect(t.getRect(recover).overlaps(t.getRect(modeControl)), false);
          await nowTap(t, recover);
          expect(t.testTextInput.isVisible, false);
          expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, false);
          expect(ws.contentMode, mode);
          expect(ws.sheetExtent, extent);
          expect(ws.selectedEntityId, entity.id);
          expect(identical(ws.task, task), true);
          expect(identical(ws.result, result), true);
          expect(
            t.widget<TextField>(nowField()).controller!.text,
            '安全未发送第1行\n安全未发送第2行\n安全未发送第3行\n安全未发送第4行',
          );
          t.view.viewInsets = const FakeViewPadding();
          t.view.padding = const FakeViewPadding(top: 150, bottom: 52);
          await t.pumpAndSettle();
          final laneRect = t.getRect(
            find.byKey(const Key('now-selected-context-scroll')),
          );
          final sheetRect = t.getRect(find.byType(AgentResultsSheet));
          expect(sheetRect.top, greaterThanOrEqualTo(laneRect.bottom + 11.5));
          expect(
            find.byType(EntityPeekCard),
            extent == AgentSheetExtent.expanded ? findsNothing : findsOneWidget,
          );
          expect(find.byTooltip('打开侧边栏').hitTestable(), findsOneWidget);
          expect(
            find.byKey(const Key('now-city-picker')).hitTestable(),
            findsOneWidget,
          );
          expect(ws.contentMode, mode);
          expect(ws.sheetExtent, extent);
          expect(ws.selectedEntityId, entity.id);
          expect(identical(ws.task, task), true);
          expect(identical(ws.result, result), true);
          expect(
            identical(t.widget<MapCanvas>(find.byType(MapCanvas)), map),
            true,
          );
          expect(f.source.queries, ['查找公开地点']);
          expect(t.takeException(), isNull);
        }
      }
      ws.showContent(AgentContentMode.results);
      ws.setSheetExtent(AgentSheetExtent.medium);
      await t.pumpAndSettle();
      await t.drag(
        find.byKey(const Key('agent-sheet-handle')),
        const Offset(0, -150),
      );
      await t.pumpAndSettle();
      expect(ws.sheetExtent, AgentSheetExtent.expanded);
      expect(ws.contentMode, AgentContentMode.results);
      expect(ws.selectedEntityId, entity.id);
      await t.drag(
        find.byKey(const Key('agent-sheet-handle')),
        const Offset(0, 150),
      );
      await t.pumpAndSettle();
      expect(ws.sheetExtent, AgentSheetExtent.medium);
      expect(ws.selectedEntityId, entity.id);
      await nowTap(t, find.byTooltip('打开侧边栏'));
      await nowBack(t);
      expect(ws.selectedEntityId, entity.id);
      expect(ws.contentMode, AgentContentMode.results);
      expect(
        t.widget<TextField>(nowField()).controller!.text,
        '安全未发送第1行\n安全未发送第2行\n安全未发送第3行\n安全未发送第4行',
      );
      expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, false);
      await nowTap(t, find.byKey(const Key('now-city-picker')));
      await nowBack(t);
      expect(identical(ws.task, task), true);
      expect(ws.selectedEntityId, entity.id);
      expect(map.mapState.searchAreaBounds, movedBounds);
      expect(f.source.queries, ['查找公开地点']);
      expect(t.takeException(), isNull);
      await f.unmount(t);
    });
  }
  testWidgets('焦点诊断默认关闭，显式启用只记录表面几何和无内容的焦点时序', (t) async {
    const traceEnabled = bool.fromEnvironment(
      'BIRDTIE_COMPOSER_FOCUS_TRACE',
      defaultValue: false,
    );
    const prefix = 'BIRDTIE_COMPOSER_FOCUS_TRACE ';
    const privateDraft = '私人草稿 TRACE_PRIVATE_DRAFT_NOT_A_LOG_FIELD';
    final lines = <String>[];
    final previousPrint = debugPrint;
    debugPrint = (message, {wrapWidth}) {
      if (message?.startsWith(prefix) ?? false) {
        lines.add(message!);
      } else {
        previousPrint(message, wrapWidth: wrapWidth);
      }
    };
    try {
      t.view.physicalSize = const Size(1220, 2656);
      t.view.devicePixelRatio = 3.25;
      t.view.padding = const FakeViewPadding(top: 150, bottom: 52);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      addTearDown(t.view.resetPadding);
      final f = NowFixture(selected: false);
      addTearDown(f.dispose);
      await _mountInputHitFixture(t, f);
      final state = t.state<AgentComposerState>(find.byType(AgentComposer));
      final field = t.widget<TextField>(nowField());
      final central = find.ancestor(
        of: nowField(),
        matching: find.byType(GestureDetector),
      );
      final initialRect = t.getRect(central);
      expect(field.focusNode!.hasFocus, false);
      await t.tapAt(Offset(initialRect.center.dx, initialRect.top + 2));
      await t.pumpAndSettle();
      expect(field.focusNode!.hasFocus, true);
      expect(t.testTextInput.isVisible, true);
      const value = TextEditingValue(
        text: privateDraft,
        selection: TextSelection(baseOffset: 2, extentOffset: 4),
        composing: TextRange.empty,
      );
      t.testTextInput.updateEditingValue(value);
      await t.pumpAndSettle();
      state.pauseEditing();
      await t.pumpAndSettle();
      expect(field.focusNode!.hasFocus, false);
      expect(t.testTextInput.isVisible, false);
      expect(field.controller!.value, value);
      state.resumeEditing();
      await t.pumpAndSettle();
      expect(field.focusNode!.hasFocus, true);
      expect(t.testTextInput.isVisible, true);
      expect(field.controller!.value, value);
      expect(f.source.queries, isEmpty);
      if (traceEnabled) {
        expect(lines, isNotEmpty);
        final records = lines
            .map((line) => jsonDecode(line.substring(prefix.length)))
            .cast<Map<String, dynamic>>()
            .toList();
        const safeFields = {
          'event',
          'sequence',
          'elapsedMicros',
          'mounted',
          'hasFocus',
          'canRequestFocus',
          'ownNodeHash',
          'primaryType',
          'primaryHash',
          'surfaceAttached',
          'logicalRect',
          'physicalRect',
          'dpr',
          'logicalTap',
          'physicalTap',
        };
        expect(records.every((r) => r.keys.every(safeFields.contains)), true);
        expect(lines.join(), isNot(contains(privateDraft)));
        expect(
          lines.join(),
          isNot(contains('TRACE_PRIVATE_DRAFT_NOT_A_LOG_FIELD')),
        );
        final events = records.map((r) => r['event']).toList();
        for (final event in [
          'central_tap_down',
          'central_tap',
          'resume_before',
          'resume_after',
          'own_focus_change',
          'manager_focus_change',
          'pause_before',
          'pause_after',
        ]) {
          expect(events, contains(event));
        }
        for (var index = 1; index < records.length; index++) {
          expect(
            records[index]['sequence'],
            greaterThan(records[index - 1]['sequence'] as int),
          );
          expect(
            records[index]['elapsedMicros'],
            greaterThanOrEqualTo(records[index - 1]['elapsedMicros'] as int),
          );
        }
        final down = records.firstWhere(
          (r) => r['event'] == 'central_tap_down',
        );
        expect(down['dpr'], 3.25);
        expect(down['surfaceAttached'], true);
        final logical = (down['logicalRect'] as List).cast<num>();
        final physical = (down['physicalRect'] as List).cast<num>();
        final expected = [
          initialRect.left,
          initialRect.top,
          initialRect.right,
          initialRect.bottom,
        ];
        for (var i = 0; i < 4; i++) {
          expect(logical[i], closeTo(expected[i], .01));
          expect(physical[i], closeTo(expected[i] * 3.25, .01));
        }
      } else {
        expect(lines, isEmpty);
      }
      await f.unmount(t);
      final afterDispose = lines.length;
      await t.pumpWidget(
        const MaterialApp(
          home: Scaffold(body: Focus(autofocus: true, child: SizedBox())),
        ),
      );
      await t.pumpAndSettle();
      expect(lines.length, afterDispose);
      expect(t.takeException(), isNull);
    } finally {
      debugPrint = previousPrint;
    }
  });

  testWidgets('焦点诊断只观察原手势取消，不添加焦点请求或提交', (t) async {
    const enabled = bool.fromEnvironment('BIRDTIE_COMPOSER_FOCUS_TRACE');
    const prefix = 'BIRDTIE_COMPOSER_FOCUS_TRACE ';
    final lines = <String>[];
    final previousPrint = debugPrint;
    debugPrint = (message, {wrapWidth}) {
      if (message?.startsWith(prefix) ?? false) {
        lines.add(message!);
      } else {
        previousPrint(message, wrapWidth: wrapWidth);
      }
    };
    try {
      final f = NowFixture(selected: false);
      addTearDown(f.dispose);
      await _mountInputHitFixture(t, f);
      final field = t.widget<TextField>(nowField());
      final central = find.ancestor(
        of: nowField(),
        matching: find.byType(GestureDetector),
      );
      final rect = t.getRect(central);
      final gesture = await t.startGesture(
        Offset(rect.center.dx, rect.top + 2),
      );
      await t.pump(const Duration(milliseconds: 120));
      await gesture.moveBy(const Offset(0, -100));
      await t.pump();
      await gesture.up();
      await t.pumpAndSettle();
      expect(field.focusNode!.hasFocus, false);
      expect(t.testTextInput.isVisible, false);
      expect(f.source.queries, isEmpty);
      if (enabled) {
        final events = lines
            .map((line) => jsonDecode(line.substring(prefix.length))['event'])
            .toList();
        expect(events, contains('central_tap_down'));
        expect(events, contains('central_tap_cancel'));
        expect(events, isNot(contains('central_tap')));
        expect(events, isNot(contains('resume_before')));
      } else {
        expect(lines, isEmpty);
      }
      await f.unmount(t);
    } finally {
      debugPrint = previousPrint;
    }
  });

  for (final sample in ['4102450', '5202400', '5202480', 'fieldCenter']) {
    testWidgets('实际统一输入可视表面 $sample 点击应编辑', (t) async {
      const density = 3.25;
      t.view.physicalSize = const Size(1220, 2656);
      t.view.devicePixelRatio = density;
      t.view.padding = const FakeViewPadding(top: 150, bottom: 52);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      addTearDown(t.view.resetPadding);
      final f = NowFixture(selected: false);
      addTearDown(f.dispose);
      await _mountInputHitFixture(t, f);
      final composer = find.byType(AgentComposer), field = nowField();
      final surface = find.descendant(
        of: composer,
        matching: find.byType(BirdtieSurface),
      );
      final editable = find.descendant(
        of: field,
        matching: find.byType(EditableText),
      );
      final decorator = find.descendant(
        of: field,
        matching: find.byType(InputDecorator),
      );
      final rect = t.getRect(field);
      final point = switch (sample) {
        '4102450' => const Offset(410, 2450) / density,
        '5202400' => const Offset(520, 2400) / density,
        '5202480' => const Offset(520, 2480) / density,
        _ => rect.center,
      };
      List<double> physical(Rect r) => [
        r.left * density,
        r.top * density,
        r.right * density,
        r.bottom * density,
      ];
      final hit = HitTestResult();
      t.binding.hitTestInView(hit, point, t.view.viewId);
      debugPrintSynchronously((
        jsonEncode({
          'sample': sample,
          'logicalTap': [point.dx, point.dy],
          'physicalTap': [point.dx * density, point.dy * density],
          'physicalRects': {
            'surface': physical(t.getRect(surface)),
            'textField': physical(rect),
            'editable': physical(t.getRect(editable)),
            'decorator': physical(t.getRect(decorator)),
          },
          'surfaceContainsTap': t.getRect(surface).contains(point),
          'fieldContainsTap': rect.contains(point),
          'hitPath': hit.path
              .map((e) => e.target.runtimeType.toString())
              .toList(),
          'initialFocus': t.widget<TextField>(field).focusNode!.hasFocus,
        })).toString());
      expect(t.getRect(surface).contains(point), true);
      expect(t.getRect(editable).center.dy, closeTo(rect.center.dy, .5));
      expect(t.widget<TextField>(field).focusNode!.hasFocus, false);
      await t.tapAt(point);
      await t.pumpAndSettle();
      debugPrintSynchronously((
        jsonEncode({
          'sample': sample,
          'focusAfter': t.widget<TextField>(field).focusNode!.hasFocus,
          'imeAfter': t.testTextInput.isVisible,
        })).toString());
      expect(t.widget<TextField>(field).focusNode!.hasFocus, true);
      expect(t.testTextInput.isVisible, true);
      expect(f.source.queries, isEmpty);
      await f.unmount(t);
    });
  }

  for (final sample in ['5202400', '5202480', 'fieldCenter']) {
    testWidgets(
      '已获焦但系统隐藏键盘后 $sample 明确点击应重新编辑',
      (t) async {
        const density = 3.25;
        t.view.physicalSize = const Size(1220, 2656);
        t.view.devicePixelRatio = density;
        t.view.padding = const FakeViewPadding(top: 150, bottom: 52);
        addTearDown(t.view.resetPhysicalSize);
        addTearDown(t.view.resetDevicePixelRatio);
        addTearDown(t.view.resetPadding);
        final f = NowFixture(selected: false);
        addTearDown(f.dispose);
        await _mountInputHitFixture(t, f);
        final field = t.widget<TextField>(nowField());
        await t.tapAt(const Offset(520, 2480) / density);
        await t.pumpAndSettle();
        expect(field.focusNode!.hasFocus, true);
        expect(t.testTextInput.isVisible, true);
        final ownEditable = field.focusNode!.context
            ?.findAncestorStateOfType<EditableTextState>();
        expect(ownEditable, isNotNull);
        expect(identical(ownEditable!.widget.focusNode, field.focusNode), true);
        const value = TextEditingValue(
          text: '本人尚未发送的地点',
          selection: TextSelection(baseOffset: 2, extentOffset: 4),
          composing: TextRange(start: 2, end: 4),
        );
        for (var attempt = 0; attempt < 3; attempt++) {
          t.testTextInput.updateEditingValue(value);
          await t.pumpAndSettle();
          // Simulates a platform-hidden IME that retains the native input focus.
          // Native Back/event routing still requires the recorded device check.
          t.testTextInput.hide();
          await t.pumpAndSettle();
          expect(field.focusNode!.hasFocus, true);
          expect(t.testTextInput.isVisible, false);
          final start = t.testTextInput.log.length;
          final point = switch (sample) {
            '5202400' => const Offset(520, 2400) / density,
            '5202480' => const Offset(520, 2480) / density,
            _ => t.getRect(nowField()).center,
          };
          await t.tapAt(point);
          await t.pumpAndSettle();
          final calls = t.testTextInput.log
              .skip(start)
              .map((call) => call.method)
              .toList();
          debugPrintSynchronously((
            jsonEncode({
              'hiddenIME': sample,
              'attempt': attempt,
              'focus': field.focusNode!.hasFocus,
              'ime': t.testTextInput.isVisible,
              'calls': calls,
            })).toString());
          expect(field.focusNode!.hasFocus, true);
          expect(t.testTextInput.isVisible, true);
          expect(calls, contains('TextInput.show'));
          expect(field.controller!.text, value.text);
          expect(field.controller!.selection.isValid, true);
          if (sample == '5202400') {
            // A blank-surface tap reopens this editor without moving its caret,
            // committing its composing range or replacing the safe draft.
            expect(field.controller!.value, value);
          }
          expect(f.source.queries, isEmpty);
        }
        t.state<AgentComposerState>(find.byType(AgentComposer)).pauseEditing();
        await t.pumpAndSettle();
        expect(field.focusNode!.hasFocus, false);
        expect(t.testTextInput.isVisible, false);
        expect(field.controller!.text, value.text);
        expect(t.takeException(), isNull);
        await f.unmount(t);
      },
      variant: TargetPlatformVariant.only(TargetPlatform.android),
    );
  }

  for (final brightness in Brightness.values) {
    for (final scale in [1.0, 1.7, 3.0]) {
      testWidgets('中央输入完整可点击 $brightness 字号$scale 保留无IME返回与安全稿', (t) async {
        t.view.physicalSize = const Size(320, 720);
        t.view.devicePixelRatio = 1;
        t.view.padding = const FakeViewPadding(top: 24, bottom: 24);
        addTearDown(t.view.resetPhysicalSize);
        addTearDown(t.view.resetDevicePixelRatio);
        addTearDown(t.view.resetPadding);
        final f = NowFixture(selected: false);
        addTearDown(f.dispose);
        await _mountInputHitFixture(t, f, brightness: brightness, scale: scale);
        final field = nowField();
        final surface = find.descendant(
          of: find.byType(AgentComposer),
          matching: find.byWidgetPredicate(
            (w) => w is BirdtieSurface && w.kind == BirdtieSurfaceKind.floating,
          ),
        );
        final central = find.ancestor(
          of: field,
          matching: find.byType(GestureDetector),
        );
        final area = t.getRect(surface), input = t.getRect(field);
        final editableArea = t.getRect(central);
        expect(editableArea.height, greaterThanOrEqualTo(48));
        expect(editableArea.top, closeTo(area.top, .5));
        expect(editableArea.bottom, closeTo(area.bottom, .5));
        expect(editableArea.left, closeTo(input.left, .5));
        expect(editableArea.right, closeTo(input.right, .5));
        final state = t.state<AgentComposerState>(find.byType(AgentComposer));
        expect(t.widget<TextField>(field).focusNode!.hasFocus, false);
        await t.tapAt(Offset(input.center.dx, area.top + 2));
        await t.pumpAndSettle();
        expect(t.testTextInput.isVisible, true);
        await t.enterText(field, '安全的原始草稿');
        state.pauseEditing();
        await t.pumpAndSettle();
        expect(t.widget<TextField>(field).controller!.text, '安全的原始草稿');
        expect(t.widget<TextField>(field).focusNode!.hasFocus, false);
        expect(t.testTextInput.isVisible, false);
        final lower = t.getRect(central);
        await t.tapAt(Offset(lower.center.dx, lower.bottom - 2));
        await t.pumpAndSettle();
        expect(t.widget<TextField>(field).focusNode!.hasFocus, true);
        expect(t.testTextInput.isVisible, true);
        expect(t.widget<TextField>(field).controller!.text, '安全的原始草稿');
        expect(t.widget<TextField>(field).controller!.selection.isValid, true);
        expect(f.source.queries, isEmpty);
        expect(t.takeException(), isNull);
        await f.unmount(t);
      });
    }
  }
  testWidgets('输入留白编辑与独立加号发送保留选区 composing 与导航暂停', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await _mountInputHitFixture(t, f);
    final field = nowField();
    final state = t.state<AgentComposerState>(find.byType(AgentComposer));
    await t.enterText(field, '本人检查的未发送草稿');
    final controller = t.widget<TextField>(field).controller!;
    controller.selection = const TextSelection(baseOffset: 0, extentOffset: 2);
    state.pauseEditing();
    await t.pumpAndSettle();
    final plus = find.byTooltip('打开快捷操作');
    await nowTap(t, plus);
    expect(find.text('添加素材'), findsOneWidget);
    expect(controller.text, '本人检查的未发送草稿');
    expect(t.testTextInput.isVisible, false);
    expect(f.source.queries, isEmpty);
    await nowBack(t);
    expect(t.widget<TextField>(field).focusNode!.hasFocus, false);
    expect(t.testTextInput.isVisible, false);
    final input = t.getRect(field);
    await t.tapAt(Offset(input.center.dx, input.top + 2));
    await t.pumpAndSettle();
    expect(controller.text, '本人检查的未发送草稿');
    expect(controller.selection.isValid, true);
    expect(t.testTextInput.isVisible, true);
    t.testTextInput.updateEditingValue(
      const TextEditingValue(
        text: '地点',
        selection: TextSelection.collapsed(offset: 2),
        composing: TextRange(start: 0, end: 2),
      ),
    );
    await t.pumpAndSettle();
    await nowTap(t, find.byTooltip('发送需求'));
    expect(f.source.queries, isEmpty);
    expect(controller.text, '地点');
    t.testTextInput.updateEditingValue(
      const TextEditingValue(
        text: '地点',
        selection: TextSelection.collapsed(offset: 2),
      ),
    );
    await t.pumpAndSettle();
    await nowTap(t, find.byTooltip('发送需求'));
    expect(f.source.queries, ['地点']);
    expect(controller.text, isEmpty);
    expect(t.widget<TextField>(field).focusNode!.hasFocus, false);
    expect(t.testTextInput.isVisible, false);
    expect(t.takeException(), isNull);
    await f.unmount(t);
  });

  for (final scale in [1.0, 1.7, 3.0]) {
    testWidgets('原peek $scale 可见模式与展开主动作，点击动作不改变原选择和task', (t) async {
      t.view.physicalSize = const Size(320, 720);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final f = NowFixture();
      addTearDown(f.dispose);
      await f.mount(t, scale: scale);
      await nowSend(t, '找地点');
      final ws = f.workspace(t);
      ws.setSheetExtent(AgentSheetExtent.peek);
      ws.selectEntity('place:synthetic-keep-id');
      await t.pumpAndSettle();
      final task = ws.task;
      final sheet = find.byType(AgentResultsSheet);
      final mode = find.descendant(of: sheet, matching: find.text('继续对话'));
      final expand = find.descendant(of: sheet, matching: find.text('展开结果'));
      await nowTap(t, expand);
      expect(ws.contentMode, AgentContentMode.results);
      expect(ws.sheetExtent, AgentSheetExtent.medium);
      expect(identical(ws.task, task), true);
      expect(ws.selectedEntityId, 'place:synthetic-keep-id');
      await nowTap(t, mode);
      expect(ws.contentMode, AgentContentMode.conversation);
      final backMode = find.descendant(of: sheet, matching: find.text('查看结果'));
      final action = find.ancestor(
        of: backMode,
        matching: find.byType(TextButton),
      );
      expect(t.getSize(action).height, greaterThanOrEqualTo(48));
      await nowTap(t, backMode);
      expect(ws.contentMode, AgentContentMode.results);
      expect(identical(ws.task, task), true);
      expect(ws.selectedEntityId, 'place:synthetic-keep-id');
      expect(t.takeException(), isNull);
      await f.unmount(t);
    });
  }
  test('改变面板高度不改正在执行的查询状态', () async {
    final city = NowFixtureCity(), source = NowFixtureSource(NowFixtureCity());
    final pending = Completer<AgentResult>();
    source.pending = pending;
    final w = AgentWorkspaceController(source: source);
    final turn = w.submit('找地点', [], []);
    w.setSheetExtent(AgentSheetExtent.expanded);
    final observed = w.state;
    pending.complete(source.response('找地点'));
    await turn;
    w.dispose();
    city.dispose();
    source.city.dispose();
    expect(observed, AgentViewState.searching);
  });
  testWidgets('明确打开对话后拖低面板仍保留对话内容', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await nowSend(t, '找地点');
    await nowTap(t, find.byTooltip('打开对话'));
    expect(find.byType(AgentConversation), findsOneWidget);
    await t.drag(
      find.byKey(const Key('agent-sheet-handle')),
      const Offset(0, 150),
    );
    await t.pumpAndSettle();
    expect(find.byType(AgentConversation), findsOneWidget);
    await f.unmount(t);
  });
  testWidgets('实际统一输入框支持四行草稿', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    final field = t.widget<TextField>(nowField());
    expect(field.maxLines, greaterThanOrEqualTo(4));
    await f.unmount(t);
  });
  for (final scale in [1.0, 1.7]) {
    testWidgets('实际布局$scale字号四行IME只扣一次且peek恢复不被输入框遮挡', (t) async {
      t.view.devicePixelRatio = 1;
      t.view.physicalSize = const Size(390, 844);
      addTearDown(t.view.resetDevicePixelRatio);
      addTearDown(t.view.resetPhysicalSize);
      final f = NowFixture(selected: false);
      addTearDown(f.dispose);
      await f.mount(t, scale: scale, keyboardInset: 300, safeBottom: 24);
      await nowSend(t, '找组织');
      final oneLineHeight = t.getSize(find.byType(AgentComposer)).height;
      await t.enterText(nowField(), '第一行尚未提交\n第二行私人草稿\n第三行地点条件\n第四行时间条件');
      await t.pumpAndSettle();
      final composer = t.getRect(find.byType(AgentComposer));
      final sheet = t.getRect(find.byType(AgentResultsSheet));
      expect(composer.height, greaterThan(oneLineHeight));
      expect(composer.bottom, closeTo(844 - 300 - 24 - 16, .5));
      expect(sheet.bottom, lessThanOrEqualTo(composer.top - 11.5));
      expect(sheet.top, greaterThanOrEqualTo(24 + 63.5));
      final recover = find.text('选择城市继续');
      await t.ensureVisible(recover);
      await t.pumpAndSettle();
      expect(recover.hitTestable(), findsOneWidget);
      expect(t.getRect(recover).bottom, lessThan(composer.top));
      expect(t.takeException(), isNull);
      await f.unmount(t);
    });
  }
  testWidgets('紧凑屏幕保留键盘收起路径且安全草稿不丢', (t) async {
    t.view.devicePixelRatio = 1;
    t.view.physicalSize = const Size(360, 640);
    addTearDown(t.view.resetDevicePixelRatio);
    addTearDown(t.view.resetPhysicalSize);
    final f = NowFixture(selected: false);
    addTearDown(f.dispose);
    await f.mount(t, scale: 1.7, keyboardInset: 380, safeBottom: 24);
    await nowSend(t, '找地点');
    await t.enterText(nowField(), '保留的第一行\n第二行\n第三行\n第四行');
    await t.pumpAndSettle();
    await nowTap(t, find.byKey(const Key('now-sheet-keyboard-restore')));
    expect(
      t.widget<TextField>(nowField()).controller!.text,
      '保留的第一行\n第二行\n第三行\n第四行',
    );
    expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, false);
    expect(t.takeException(), isNull);
    await f.unmount(t);
  });
}

Future<void> _mountInputHitFixture(
  WidgetTester t,
  NowFixture f, {
  Brightness brightness = Brightness.light,
  double scale = 1,
}) async {
  // Same real MapWorkspace and NowFixture dependencies; production theme and
  // locale make input geometry comparable to the recorded native screen.
  await t.pumpWidget(
    MaterialApp(
      theme: birdtieTheme(brightness),
      locale: const Locale('zh', 'CN'),
      supportedLocales: const [Locale('zh', 'CN')],
      localizationsDelegates: const [
        GlobalMaterialLocalizations.delegate,
        GlobalWidgetsLocalizations.delegate,
        GlobalCupertinoLocalizations.delegate,
      ],
      builder: (context, child) => MediaQuery(
        data: MediaQuery.of(
          context,
        ).copyWith(textScaler: TextScaler.linear(scale)),
        child: child!,
      ),
      home: MapWorkspace(
        city: f.city,
        auth: f.auth,
        moments: f.moments,
        agentTaskSource: f.source,
        seedClient: f.client,
        seedApiBaseUrl: f.base,
      ),
    ),
  );
  await t.pumpAndSettle();
}
