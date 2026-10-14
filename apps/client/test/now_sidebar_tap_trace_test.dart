import 'dart:convert';

import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:flutter/foundation.dart' show DebugPrintCallback;
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'now_scope_recovery_test.dart';

const _enabled = bool.fromEnvironment('BIRDTIE_SIDEBAR_TAP_TRACE');
const _prefix = 'BIRDTIE_SIDEBAR_TAP_TRACE ';
const _privateDraft = 'PRIVATE_SIDEBAR_DRAFT_DO_NOT_LOG';
const _privateAuth = 'Bearer PRIVATE_SIDEBAR_AUTH_DO_NOT_LOG';
const _deliveryKeys = {
  'event',
  'eventType',
  'count',
  'sequence',
  'elapsedMicros',
  'viewId',
  'expectedViewId',
  'headerAttached',
  'insideHeader',
};

class _Trace {
  _Trace() {
    previous = debugPrint;
    debugPrint = (message, {wrapWidth}) {
      if (message?.startsWith(_prefix) ?? false) {
        lines.add(message!);
      } else {
        previous(message, wrapWidth: wrapWidth);
      }
    };
  }
  late final DebugPrintCallback previous;
  final lines = <String>[];
  List<Map<String, dynamic>> get records => [
    for (final line in lines)
      (jsonDecode(line.substring(_prefix.length)) as Map)
          .cast<String, dynamic>(),
  ];
  List<String> get events => records.map((r) => r['event'] as String).toList();
  void dispose() => debugPrint = previous;
}

void _phone(WidgetTester t) {
  t.view.physicalSize = const Size(1220, 2656);
  t.view.devicePixelRatio = 3.25;
  t.view.padding = const FakeViewPadding(top: 150, bottom: 52);
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
  addTearDown(t.view.resetPadding);
}

Future<void> _firstFrame(WidgetTester t, NowFixture f) => t.pumpWidget(
  MaterialApp(
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

ScaffoldState _scaffold(WidgetTester t) => t.state<ScaffoldState>(
  find
      .descendant(
        of: find.byType(MapWorkspace),
        matching: find.byType(Scaffold),
      )
      .first,
);

void _traceWidget(
  String name,
  Future<void> Function(WidgetTester, _Trace) body,
) => testWidgets(name, (t) async {
  final trace = _Trace();
  try {
    await body(t, trace);
  } finally {
    trace.dispose();
  }
});

void main() {
  _traceWidget('默认无诊断订阅；开启只观察首帧实际中心点击和抽屉状态', (t, trace) async {
    _phone(t);
    final routes = t.binding.pointerRouter.debugGlobalRouteCount;
    final f = NowFixture();
    addTearDown(f.dispose);
    await _firstFrame(t, f);
    final scaffold = _scaffold(t);
    final target = t.getRect(find.byKey(const Key('now-sidebar-button')));
    expect(target.width, closeTo(48, 1e-8));
    expect(target.height, closeTo(48, 1e-8));
    final glyph = t.getRect(
      find.descendant(
        of: find.byKey(const Key('now-sidebar-button')),
        matching: find.byIcon(Icons.menu_rounded),
      ),
    );
    expect(glyph.width, closeTo(20, 1e-8));
    expect(glyph.height, closeTo(20, 1e-8));
    final point = target.center;
    expect(target.contains(point), true);
    expect(glyph.contains(point), true);
    expect(scaffold.isDrawerOpen, false);
    await t.tapAt(point);
    await t.pumpAndSettle();
    expect(scaffold.isDrawerOpen, true);
    expect(f.source.queries, isEmpty);
    if (_enabled) {
      expect(
        trace.events,
        containsAll([
          'global_pointer_down',
          'header_pointer_down',
          'header_pointer_up',
          'callback_before_pause',
          'callback_after_pause',
          'drawer_changed',
          'callback_after_open',
          'callback_post_frame',
          'global_pointer_up',
        ]),
      );
      final after = trace.records.singleWhere(
        (r) => r['event'] == 'callback_after_open',
      );
      final changed = trace.records.singleWhere(
        (r) => r['event'] == 'drawer_changed',
      );
      expect(changed['reportedOpened'], true);
      expect(after['isDrawerOpen'], true);
      expect(after['scaffoldPresent'], true);
      expect(after['viewId'], t.view.viewId);
      expect(after['dpr'], 3.25);
      final event = trace.records.singleWhere(
        (r) => r['event'] == 'global_pointer_down',
      );
      expect(event['physicalPoint'], [
        point.dx * t.view.devicePixelRatio,
        point.dy * t.view.devicePixelRatio,
      ]);
      final delivery = trace.records
          .where((r) => r['event'] == 'global_delivery')
          .toList();
      expect(delivery.map((r) => r['eventType']), ['down', 'up']);
      expect(delivery.map((r) => r['count']), [1, 2]);
      expect(delivery.every((r) => r['insideHeader'] == true), true);
      expect(
        delivery.every((r) => r.keys.toSet().difference(_deliveryKeys).isEmpty),
        true,
      );
      final callbacks = trace.events;
      expect(
        callbacks.indexOf('callback_before_pause'),
        lessThan(callbacks.indexOf('callback_after_pause')),
      );
      expect(
        callbacks.indexOf('callback_after_pause'),
        lessThan(callbacks.indexOf('callback_after_open')),
      );
    } else {
      expect(trace.lines, isEmpty);
    }
    expect(t.takeException(), isNull);
    await f.unmount(t);
    expect(t.binding.pointerRouter.debugGlobalRouteCount, routes);
  });

  _traceWidget('20dp图标外的48dp命中区可点击，区外不打开且保留原Task草稿地图', (t, trace) async {
    _phone(t);
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await nowSend(t, '合成原任务');
    final w = f.workspace(t),
        task = w.task,
        result = w.result,
        epoch = w.taskEpoch;
    final selected = w.selectedEntityId;
    final map = find.byType(MapCanvas).evaluate().single;
    final publicMap = find.byType(PublicCityMapView).evaluate().single;
    await t.enterText(nowField(), _privateDraft);
    t.testTextInput.hide();
    await t.pumpAndSettle();
    final target = t.getRect(find.byKey(const Key('now-sidebar-button')));
    expect(target.width, closeTo(48, 1e-8));
    expect(target.height, closeTo(48, 1e-8));
    final glyph = t.getRect(
      find.descendant(
        of: find.byKey(const Key('now-sidebar-button')),
        matching: find.byIcon(Icons.menu_rounded),
      ),
    );
    expect(glyph.width, closeTo(20, 1e-8));
    expect(glyph.height, closeTo(20, 1e-8));
    final outside = Offset(target.center.dx, target.bottom + 1);
    expect(target.contains(outside), false);
    await t.tapAt(outside);
    await t.pumpAndSettle();
    expect(_scaffold(t).isDrawerOpen, false);
    final point = Offset(target.center.dx, target.bottom - 1);
    expect(target.contains(point), true);
    expect(glyph.contains(point), false);
    await t.tapAt(point);
    await t.pumpAndSettle();
    expect(_scaffold(t).isDrawerOpen, true);
    await nowBack(t);
    expect(_scaffold(t).isDrawerOpen, false);
    expect(w.task, same(task));
    expect(w.result, same(result));
    expect(w.taskEpoch, epoch);
    expect(w.selectedEntityId, selected);
    expect(t.widget<TextField>(nowField()).controller!.text, _privateDraft);
    expect(find.byType(MapCanvas).evaluate().single, same(map));
    expect(find.byType(PublicCityMapView).evaluate().single, same(publicMap));
    expect(f.source.queries, ['合成原任务']);
    if (!_enabled) expect(trace.lines, isEmpty);
    expect(t.takeException(), isNull);
    await f.unmount(t);
  });

  _traceWidget('顶栏外仅记过滤前计数；开关抽屉不自动重开', (t, trace) async {
    _phone(t);
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await t.tapAt(const Offset(10, 200));
    await t.pumpAndSettle();
    if (_enabled) {
      expect(trace.events, ['global_delivery', 'global_delivery']);
      expect(trace.records.map((r) => r['eventType']), ['down', 'up']);
      expect(trace.records.every((r) => r['insideHeader'] == false), true);
      expect(trace.records.every((r) => r['headerAttached'] == true), true);
    } else {
      expect(trace.lines, isEmpty);
    }
    expect(_scaffold(t).isDrawerOpen, false);
    await t.tap(find.byKey(const Key('now-sidebar-button')));
    await t.pumpAndSettle();
    await nowBack(t);
    await t.pump(const Duration(seconds: 1));
    expect(_scaffold(t).isDrawerOpen, false);
    if (_enabled) {
      expect(
        trace.records
            .where((r) => r['event'] == 'drawer_changed')
            .map((r) => r['reportedOpened']),
        [true, false],
      );
      expect(trace.events.where((e) => e == 'callback_before_pause').length, 1);
    } else {
      expect(trace.lines, isEmpty);
    }
    expect(f.source.queries, isEmpty);
    await f.unmount(t);
  });

  _traceWidget('过滤前记录跨view与未配对取消，移动事件不计数或推断命中', (t, trace) async {
    _phone(t);
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    final view = t.view.viewId, otherView = view + 1;
    final inside = t
        .getRect(find.byKey(const Key('now-sidebar-button')))
        .center;
    const outside = Offset(10, 200);
    final router = t.binding.pointerRouter;
    // Exercise the global observer without delivering gestures to a widget.
    // This proves diagnostic filtering, not native Android input delivery.
    for (final event in <PointerEvent>[
      PointerDownEvent(viewId: view, pointer: 9001, position: outside),
      PointerMoveEvent(viewId: view, pointer: 9001, position: outside),
      PointerUpEvent(viewId: view, pointer: 9001, position: outside),
      PointerCancelEvent(viewId: view, pointer: 9002, position: outside),
      PointerAddedEvent(viewId: view, pointer: 9002, position: outside),
      PointerDownEvent(viewId: otherView, pointer: 9003, position: inside),
      PointerUpEvent(viewId: otherView, pointer: 9003, position: inside),
      PointerCancelEvent(viewId: otherView, pointer: 9004, position: inside),
    ]) {
      router.route(event);
    }
    if (_enabled) {
      final records = trace.records;
      expect(records.length, 6);
      expect(trace.events, everyElement('global_delivery'));
      expect(records.map((r) => r['eventType']), [
        'down',
        'up',
        'cancel',
        'down',
        'up',
        'cancel',
      ]);
      expect(records.map((r) => r['count']), [1, 2, 3, 4, 5, 6]);
      expect(records.take(3).map((r) => r['viewId']), everyElement(view));
      expect(records.skip(3).map((r) => r['viewId']), everyElement(otherView));
      expect(records.map((r) => r['expectedViewId']), everyElement(view));
      expect(records.map((r) => r['headerAttached']), everyElement(true));
      expect(
        records.take(3).map((r) => r['insideHeader']),
        everyElement(false),
      );
      expect(
        records.skip(3).map((r) => r['insideHeader']),
        everyElement(isNull),
      );
      for (final r in records) {
        expect(r.keys.toSet(), _deliveryKeys);
      }
    } else {
      expect(trace.lines, isEmpty);
    }
    expect(_scaffold(t).isDrawerOpen, false);
    expect(f.source.queries, isEmpty);
    await f.unmount(t);
    expect(t.takeException(), isNull);
  });

  _traceWidget('dispose移除全局观察，迟到post-frame不借新workspace', (t, trace) async {
    _phone(t);
    final routes = t.binding.pointerRouter.debugGlobalRouteCount;
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await t.tap(find.byKey(const Key('now-sidebar-button')));
    // Flush the framework's pending drawer focus without running a frame.
    // This fixture intentionally removes the route before the next frame.
    FocusManager.instance.applyFocusChangesIfNeeded();
    FocusManager.instance.primaryFocus?.unfocus();
    FocusManager.instance.applyFocusChangesIfNeeded();
    // Replace the route before the diagnostic post-frame callback runs.
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
    expect(t.binding.pointerRouter.debugGlobalRouteCount, routes);
    expect(trace.events, isNot(contains('callback_post_frame')));
    final previous = trace.lines.length;
    await t.tapAt(const Offset(48, 80));
    await t.pump();
    expect(trace.lines.length, previous);
    final second = NowFixture();
    addTearDown(second.dispose);
    await second.mount(t);
    expect(_scaffold(t).isDrawerOpen, false);
    await t.tap(find.byKey(const Key('now-sidebar-button')));
    await t.pumpAndSettle();
    expect(_scaffold(t).isDrawerOpen, true);
    if (_enabled) {
      final calls = trace.records
          .where((r) => r['event'] == 'callback_before_pause')
          .toList();
      expect(calls.length, 2);
      expect(calls[0]['scaffoldHash'], isNot(calls[1]['scaffoldHash']));
      expect(trace.events.where((e) => e == 'callback_post_frame').length, 1);
    } else {
      expect(trace.lines, isEmpty);
    }
    await second.unmount(t);
    expect(t.binding.pointerRouter.debugGlobalRouteCount, routes);
    expect(t.takeException(), isNull);
  });

  _traceWidget('诊断字段白名单不含Task查询身份草稿或凭据', (t, trace) async {
    _phone(t);
    final f = NowFixture()..auth.token = _privateAuth;
    addTearDown(f.dispose);
    await f.mount(t);
    await nowSend(t, 'PRIVATE_SIDEBAR_QUERY_DO_NOT_LOG');
    await t.enterText(nowField(), _privateDraft);
    t.testTextInput.hide();
    await t.pumpAndSettle();
    await t.tap(find.byKey(const Key('now-sidebar-button')));
    await t.pumpAndSettle();
    if (_enabled) {
      expect(trace.records, isNotEmpty);
      const allowed = {
        'event',
        'sequence',
        'elapsedMicros',
        'mounted',
        'viewId',
        'dpr',
        'headerAttached',
        'logicalHeaderRect',
        'physicalHeaderRect',
        'scaffoldPresent',
        'scaffoldHash',
        'isDrawerOpen',
        'routeCurrent',
        'lifecycle',
        'schedulerPhase',
        'pointer',
        'logicalPoint',
        'physicalPoint',
        'reportedOpened',
      };
      expect(
        trace.records
            .where((r) => r['event'] != 'global_delivery')
            .every((r) => r.keys.every(allowed.contains)),
        true,
      );
      for (final r in trace.records.where(
        (r) => r['event'] == 'global_delivery',
      )) {
        expect(r.keys.toSet(), _deliveryKeys);
      }
      final sequences = trace.records.map((r) => r['sequence'] as int).toList();
      expect(
        sequences,
        orderedEquals(List.generate(sequences.length, (i) => i + 1)),
      );
    } else {
      expect(trace.lines, isEmpty);
    }
    for (final secret in [
      _privateDraft,
      _privateAuth,
      'PRIVATE_SIDEBAR_QUERY_DO_NOT_LOG',
      f.city.selectedCity!.name,
    ]) {
      expect(trace.lines.join(), isNot(contains(secret)));
    }
    await f.unmount(t);
    expect(t.takeException(), isNull);
  });
}
