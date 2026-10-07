import 'package:flutter/foundation.dart' show debugPrintSynchronously;
import 'dart:convert';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'now_scope_recovery_test.dart';

class _CountWire extends http.BaseClient {
  _CountWire(this.original);
  final http.Client original;
  final methods = <String>[];
  @override
  Future<http.StreamedResponse> send(http.BaseRequest r) {
    methods.add(r.method);
    return original.send(r);
  }
}

List<double> _rect(Rect v) => [v.left, v.top, v.right, v.bottom];
Finder _editor() => find.descendant(
  of: find.byType(AgentComposer),
  matching: find.byType(EditableText),
);
Finder _privateEntry(String text) => find.widgetWithText(TextButton, text);

void main() {
  _shortSignedInCase();
  for (final lines in [1, 4]) {
    testWidgets('本人无任务纵屏$lines行输入与私人入口互不遮挡', (t) async {
      t.view.physicalSize = const Size(390, 844);
      t.view.devicePixelRatio = 1;
      t.view.padding = const FakeViewPadding(top: 24, bottom: 20);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      addTearDown(t.view.resetPadding);
      addTearDown(t.view.resetViewInsets);
      final f = NowFixture();
      // Explicit synthetic person; this is not a verified production session.
      f.auth.token = 'Bearer synthetic-idle-unit';
      f.auth.owner = '11111111-1111-4111-8111-111111111111';
      final wire = _CountWire(f.client);
      try {
        await t.pumpWidget(
          MaterialApp(
            home: MapWorkspace(
              city: f.city,
              auth: f.auth,
              moments: f.moments,
              agentTaskSource: f.source,
              seedClient: wire,
              seedApiBaseUrl: f.base,
            ),
          ),
        );
        await t.pumpAndSettle();
        expect(f.auth.signedIn, true);
        final canvas = find.byType(MapCanvas),
            map = find.byType(PublicCityMapView);
        final canvasElement = canvas.evaluate().single,
            mapElement = map.evaluate().single;
        final mapState = t.state(map);
        final ws = f.workspace(t);
        expect(ws.task, isNull);
        expect(ws.result, isNull);
        expect(ws.selectedEntityId, isNull);
        for (final label in ['我的社交近况', '完善我的选择']) {
          expect(_privateEntry(label).hitTestable(), findsOneWidget);
        }
        await nowTap(t, nowField());
        expect(t.testTextInput.isVisible, true);
        final draft = List.generate(
          lines,
          (i) => '第${i + 1}行本人的未发送草稿',
        ).join('\n');
        t.testTextInput.enterText(draft);
        t.view.viewInsets = const FakeViewPadding(bottom: 300);
        await t.pumpAndSettle();
        final composer = t.getRect(find.byType(AgentComposer)),
            editable = t.getRect(_editor());
        final editableRender = t.renderObject<RenderBox>(_editor());
        final plus = find.byTooltip('打开快捷操作'), send = find.byTooltip('发送需求');
        final entries = <Map<String, Object?>>[];
        for (final label in ['我的社交近况', '完善我的选择']) {
          final entry = _privateEntry(label);
          expect(entry, findsOneWidget);
          expect(entry.hitTestable(), findsOneWidget);
          final r = t.getRect(entry), overlap = r.intersect(editable);
          final point = overlap.isEmpty ? editable.center : overlap.center;
          final path = t.hitTestOnBinding(point).path;
          entries.add({
            'label': label,
            'rect': _rect(r),
            'insideVisibleViewport': r.top >= 24 && r.bottom <= 544,
            'overlapsEditable': r.overlaps(editable),
            'overlapsComposer': r.overlaps(composer),
            'editableReceivesOverlapPoint': path.any(
              (hit) => identical(hit.target, editableRender),
            ),
            'point': [point.dx, point.dy],
            'path': path
                .map((hit) => hit.target.runtimeType.toString())
                .toList(),
          });
        }
        debugPrintSynchronously((
          jsonEncode({
            'scenario': 'signed-in-idle-$lines-line',
            'source':
                'original NowFixture generic GET data[]; synthetic person only',
            'keyboard': t.testTextInput.isVisible,
            'inputFocused': ws.inputFocused,
            'composerRect': _rect(composer),
            'editableRect': _rect(editable),
            'plusRect': _rect(t.getRect(plus)),
            'sendRect': _rect(t.getRect(send)),
            'privateEntries': entries,
            'task': ws.task?.id,
            'selected': ws.selectedEntityId,
            'queries': f.source.queries.length,
            'methods': wire.methods,
            'indeterminateSpinners':
                find.byType(CircularProgressIndicator).evaluate().length +
                find.byType(LinearProgressIndicator).evaluate().length,
          })).toString());
        expect(t.widget<TextField>(nowField()).controller!.text, draft);
        expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, true);
        expect(plus.hitTestable(), findsOneWidget);
        expect(send.hitTestable(), findsOneWidget);
        expect(f.source.queries, isEmpty);
        expect(wire.methods.every((v) => v == 'GET'), true);
        expect(ws.task, isNull);
        expect(ws.result, isNull);
        expect(ws.selectedEntityId, isNull);
        expect(identical(canvasElement, canvas.evaluate().single), true);
        expect(identical(mapElement, map.evaluate().single), true);
        expect(identical(mapState, t.state(map)), true);
        for (final entry in entries) {
          expect(entry['insideVisibleViewport'], true);
          expect(
            entry['overlapsEditable'],
            false,
            reason: '实际绘制的私人入口不能遮挡本人四行输入',
          );
          expect(entry['overlapsComposer'], false);
        }
        expect(t.takeException(), isNull);
      } finally {
        await f.unmount(t);
        f.dispose();
      }
    });
  }
}

// This is only the compact branch affected by the changed private lane.
void _shortSignedInCase() {
  testWidgets('本人无任务短横屏大字编辑收敛私人入口，收键盘后入口与设置可达', (t) async {
    t.view.physicalSize = const Size(800, 400);
    t.view.devicePixelRatio = 1;
    t.view.padding = const FakeViewPadding(top: 24, bottom: 20);
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    addTearDown(t.view.resetPadding);
    addTearDown(t.view.resetViewInsets);
    final f = NowFixture();
    f.auth.token = 'Bearer synthetic-idle-unit';
    f.auth.owner = '11111111-1111-4111-8111-111111111111';
    final wire = _CountWire(f.client);
    try {
      await t.pumpWidget(
        MaterialApp(
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: const TextScaler.linear(2)),
            child: child!,
          ),
          home: MapWorkspace(
            city: f.city,
            auth: f.auth,
            moments: f.moments,
            agentTaskSource: f.source,
            seedClient: wire,
            seedApiBaseUrl: f.base,
          ),
        ),
      );
      await t.pumpAndSettle();
      final canvas = find.byType(MapCanvas),
          map = find.byType(PublicCityMapView);
      final ce = canvas.evaluate().single,
          me = map.evaluate().single,
          ms = t.state(map);
      for (final label in ['我的社交近况', '完善我的选择']) {
        expect(_privateEntry(label).hitTestable(), findsOneWidget);
      }
      await nowTap(t, nowField());
      const draft = '第1行未发送草稿\n第2行未发送草稿\n第3行未发送草稿\n第4行未发送草稿';
      t.testTextInput.enterText(draft);
      t.view.viewInsets = const FakeViewPadding(bottom: 240);
      await t.pumpAndSettle();
      expect(_privateEntry('我的社交近况'), findsNothing);
      expect(_privateEntry('完善我的选择'), findsNothing);
      final restore = find.byKey(const Key('now-sheet-keyboard-restore'));
      expect(restore.hitTestable(), findsOneWidget);
      expect(t.getSize(restore).width, greaterThanOrEqualTo(48));
      expect(t.getSize(restore).height, greaterThanOrEqualTo(48));
      expect(find.byTooltip('打开快捷操作').hitTestable(), findsOneWidget);
      expect(find.byTooltip('发送需求').hitTestable(), findsOneWidget);
      final editable = t.getRect(_editor()), viewHeight = 160.0;
      debugPrintSynchronously((
        jsonEncode({
          'scenario': 'signed-in-idle-short-font2',
          'privateEntries': 0,
          'keyboard': t.testTextInput.isVisible,
          'editableRect': _rect(editable),
          'restoreRect': _rect(t.getRect(restore)),
          'queries': f.source.queries.length,
          'methods': wire.methods,
        })).toString());
      expect(editable.top, greaterThanOrEqualTo(24));
      expect(editable.bottom, lessThanOrEqualTo(viewHeight));
      await nowTap(t, restore);
      expect(t.testTextInput.isVisible, false);
      expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, false);
      t.view.viewInsets = const FakeViewPadding();
      await t.pumpAndSettle();
      debugPrintSynchronously((
        jsonEncode({
          'scenario': 'short-after-restore',
          'composerRect': _rect(t.getRect(find.byType(AgentComposer))),
          'privateCount': _privateEntry('我的社交近况').evaluate().length,
          'privateViewportCount': find
              .byKey(const Key('now-idle-private-actions'))
              .evaluate()
              .length,
          'keyboard': t.testTextInput.isVisible,
        })).toString());
      // Four large lines still consume this short viewport after the IME closes.
      // Keep the draft, core navigation and direct Settings paths rather than
      // demand a floating lane with less than one usable 48dp target.
      expect(find.byKey(const Key('now-idle-private-actions')), findsNothing);
      expect(find.byTooltip('打开侧边栏').hitTestable(), findsOneWidget);
      expect(t.widget<TextField>(nowField()).controller!.text, draft);
      expect(f.workspace(t).task, isNull);
      expect(f.workspace(t).result, isNull);
      expect(f.workspace(t).selectedEntityId, isNull);
      expect(identical(ce, canvas.evaluate().single), true);
      expect(identical(me, map.evaluate().single), true);
      expect(identical(ms, t.state(map)), true);
      await nowTap(t, find.byTooltip('打开侧边栏'));
      await nowTap(t, find.text('设置'));
      debugPrintSynchronously((
        jsonEncode({
          'scenario': 'actual-sidebar-settings',
          'scrollables': find.byType(Scrollable).evaluate().length,
          'visibleText': t.allWidgets
              .whereType<Text>()
              .map((w) => w.data)
              .whereType<String>()
              .take(40)
              .toList(),
        })).toString());
      for (final label in ['完善我的选择', '我的社交近况']) {
        final target = find.widgetWithText(ListTile, label);
        await t.scrollUntilVisible(
          target,
          120,
          maxScrolls: 30,
          scrollable: find.byType(Scrollable).last,
        );
        await t.ensureVisible(target);
        await t.pumpAndSettle();
        expect(target.hitTestable(), findsOneWidget);
      }
      await nowBack(t);
      expect(t.widget<TextField>(nowField()).controller!.text, draft);
      expect(t.testTextInput.isVisible, false);
      t.view.physicalSize = const Size(390, 844);
      await t.pumpAndSettle();
      for (final label in ['我的社交近况', '完善我的选择']) {
        final entry = _privateEntry(label);
        expect(entry.hitTestable(), findsOneWidget);
        expect(t.getSize(entry).height, greaterThanOrEqualTo(48));
        expect(t.getRect(entry).overlaps(t.getRect(_editor())), false);
      }
      expect(t.widget<TextField>(nowField()).controller!.text, draft);
      expect(identical(ce, canvas.evaluate().single), true);
      expect(identical(me, map.evaluate().single), true);
      expect(identical(ms, t.state(map)), true);
      expect(f.source.queries, isEmpty);
      expect(wire.methods.every((m) => m == 'GET'), true);
      expect(t.takeException(), isNull);
    } finally {
      await f.unmount(t);
      f.dispose();
    }
  });
}
