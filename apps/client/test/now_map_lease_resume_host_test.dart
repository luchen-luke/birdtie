import 'dart:async';

import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/city/map_cluster_members_sheet.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'map_layers_api_test.dart' show mapTestBounds, mapTestItem;
import 'now_scope_recovery_test.dart'
    show NowFixtureCity, nowOpenAccountTools, nowTap;
import 'place_detail_sheet_test.dart' show jsonReply;

class _NoAgentSource extends AgentTaskSource {
  int resolves = 0;
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async {
    resolves++;
    throw StateError(
      'Native map lease recovery must not create an Agent task.',
    );
  }
}

/// Actual Now route, map controller/API decoder, city modal and composer.
/// Only the HTTP transport and native city catalogue are synthetic. No manual
/// assignment to controller publicView/error and no Agent result/task fixtures.
/// FakeAsync timer progression proves scheduling and source epochs; it does
/// not substitute for native wall-clock expiry or real Mapbox device evidence.
class _LeaseHost {
  _LeaseHost({this.lease = const Duration(minutes: 2)});
  final auth = SeedTestAuth()..token = null;
  final city = NowFixtureCity();
  final source = _NoAgentSource();
  final requests = <http.Request>[];
  Duration lease;
  bool failMap = false;
  int mapReads = 0;
  final mapReadClocks = <DateTime>[];
  int? holdRead;
  final held = Completer<http.Response>();
  late final client = MockClient((r) async {
    requests.add(r);
    if (r.method != 'GET') return http.Response('{}', 503);
    if (r.url.path == '/v1/cities/alpha/map-layers') {
      final read = ++mapReads;
      mapReadClocks.add(TestWidgetsFlutterBinding.instance.clock.now());
      if (read == holdRead) return held.future;
      if (failMap) return http.Response('{}', 503);
      return mapResponse(read);
    }
    if (r.url.path.endsWith('/activities') || r.url.path == '/v1/me/moments') {
      return jsonReply({'data': []});
    }
    return http.Response('{}', 404);
  });
  late final moments = PrivateMomentController(
    authorizationHeader: () => auth.authorizationHeader,
    client: client,
    apiBaseUrl: 'https://map-lease-host.test',
  );
  final keyboard = ValueNotifier(0.0);
  final scale = ValueNotifier(1.0);
  late final presentation = Listenable.merge([keyboard, scale]);

  http.Response mapResponse(int read, {String? title}) {
    final now = DateTime.now().toUtc();
    final row = mapTestItem('PLACE');
    row['title'] = title ?? '当前合法地图来源 $read，Aberdeen 中文混排';
    row['sourceVersion'] = 'map-lease-response-$read';
    return jsonReply({
      'data': {
        'schemaVersion': 'typed-map-layers-v1',
        'cityId': 'alpha',
        'scope': 'PUBLIC',
        'observedAt': now.toIso8601String(),
        'validUntil': now.add(lease).toIso8601String(),
        'truncated': false,
        'items': [row],
      },
    });
  }

  MapCanvas canvas(WidgetTester t) =>
      t.widget<MapCanvas>(find.byType(MapCanvas, skipOffstage: false));
  AgentWorkspaceController workspace(WidgetTester t) => canvas(t).workspace;
  AgentComposerState composer(WidgetTester t) => t.state<AgentComposerState>(
    find.byType(AgentComposer, skipOffstage: false),
  );

  Future<void> mount(WidgetTester t) async {
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    await t.pumpWidget(
      MaterialApp(
        theme: birdtieTheme(Brightness.light),
        builder: (context, child) => AnimatedBuilder(
          animation: presentation,
          child: child,
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(context).copyWith(
              textScaler: TextScaler.linear(scale.value),
              viewInsets: EdgeInsets.only(bottom: keyboard.value),
            ),
            child: child!,
          ),
        ),
        home: MapWorkspace(
          city: city,
          auth: auth,
          moments: moments,
          agentTaskSource: source,
          seedClient: client,
          seedApiBaseUrl: 'https://map-lease-host.test',
          placeDetailClient: client,
          placeDetailApiBaseUrl: 'https://map-lease-host.test',
        ),
      ),
    );
    await t.pumpAndSettle();
    await canvas(t).layers!.load('alpha', mapTestBounds);
    canvas(t).mapState.initializeViewport(mapTestBounds);
    await t.pumpAndSettle();
    expect(mapReads, 1);
    expect(canvas(t).layers!.publicView, isNotNull);
    expect(canvas(t).layers!.error, isNull);
  }

  Future<void> openCity(WidgetTester t) async {
    await t.tap(find.byKey(const Key('now-city-picker')));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('now-scope-picker')), findsOneWidget);
    expect(
      ModalRoute.of(t.element(find.byType(MapWorkspace)))?.isCurrent,
      isFalse,
      reason: 'A real city modal covers the Now route.',
    );
  }

  Future<void> closeCity(WidgetTester t) async {
    await t.tap(find.byTooltip('关闭城市选择'));
    await t.pumpAndSettle();
    expect(find.byKey(const Key('now-scope-picker')), findsNothing);
  }

  void assertNativeOnly(WidgetTester t) {
    expect(source.resolves, 0);
    expect(workspace(t).task, isNull);
    expect(requests.every((r) => r.method == 'GET'), isTrue);
    expect(requests.any((r) => r.url.path.contains('/agent/')), isFalse);
    expect(t.takeException(), isNull);
  }

  Future<void> dispose(WidgetTester t) async {
    await t.pumpWidget(const SizedBox.shrink());
    if (!held.isCompleted) held.complete(http.Response('{}', 503));
    await t.pump();
    moments.dispose();
    auth.dispose();
    city.dispose();
    client.close();
    keyboard.dispose();
    scale.dispose();
    t.view.resetPhysicalSize();
    t.view.resetDevicePixelRatio();
    t.view.resetViewInsets();
  }
}

void main() {
  for (final reader in ['cluster', 'city', 'layers']) {
    testWidgets(
      'owned $reader reader stops reads under foreign route and resumes missed read on return',
      (t) async {
        final f = _LeaseHost(
          lease: Duration(seconds: reader == 'layers' ? 20 : 8),
        );
        try {
          await f.mount(t);
          final map = t.widget<PublicCityMapView>(
            find.byType(PublicCityMapView),
          );
          Future<MapEntity?>? selection;
          if (reader == 'city') {
            await f.openCity(t);
          } else if (reader == 'layers') {
            await nowOpenAccountTools(t);
            await nowTap(t, find.text('地图图层'));
          } else {
            selection = showCurrentMapClusterMembers(
              context: t.element(find.byType(MapWorkspace)),
              cluster: MapEntity(
                id: 'cluster:covered',
                kind: MapEntityKind.cluster,
                title: '聚合',
                subtitle: '',
                latitude: 57.2,
                longitude: -2.1,
                memberIDs: [map.currentReaderEntities!().single.id],
              ),
              changes: map.readerChanges!,
              currentMembers: map.currentReaderEntities!,
              currentScope: map.currentReaderScope!,
              onVisibilityChanged: map.onMapReaderVisibilityChanged,
            );
            await t.pumpAndSettle();
          }
          final navigator = Navigator.of(t.element(find.byType(MapWorkspace)));
          navigator.push(
            MaterialPageRoute<void>(
              builder: (_) => const Scaffold(body: Text('非地图覆盖页面')),
            ),
          );
          await t.pumpAndSettle();
          await t.pump(
            Duration(milliseconds: reader == 'layers' ? 14500 : 5700),
          );
          await t.pump();
          expect(
            f.mapReads,
            1,
            reason:
                'The mounted owned reader is covered and cannot authorize HTTP.',
          );
          navigator.pop();
          await t.pumpAndSettle();
          expect(
            f.mapReads,
            2,
            reason:
                'Returning to the current owned reader wakes its missed due once.',
          );
          expect(
            f.canvas(t).layers!.items.single.sourceVersion,
            'map-lease-response-2',
          );
          if (reader == 'city') {
            await f.closeCity(t);
          } else if (reader == 'layers') {
            navigator.pop();
            await t.pumpAndSettle();
          } else {
            await t.tap(find.text('当前合法地图来源 2，Aberdeen 中文混排'));
            await t.pumpAndSettle();
            expect((await selection)?.title, '当前合法地图来源 2，Aberdeen 中文混排');
          }
          f.assertNativeOnly(t);
        } finally {
          await f.dispose(t);
        }
      },
    );
  }

  testWidgets('原地图聚合读者跨租期读取当前成员，保留地图输入草稿和原选择', (t) async {
    final f = _LeaseHost(lease: const Duration(seconds: 8));
    try {
      await f.mount(t);
      final canvasElement = find.byType(MapCanvas).evaluate().single;
      final editor = f.composer(t);
      editor.fillDraft('聚合阅读保留 café 中文草稿', requestFocus: false);
      await t.pumpAndSettle();
      final map = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
      final original = map.currentReaderEntities!().single;
      final before = f.canvas(t).layers!.publicView!;
      final until = before.validUntil;
      final shownScope = map.currentReaderScope!();
      final result = showCurrentMapClusterMembers(
        context: t.element(find.byType(MapWorkspace)),
        cluster: MapEntity(
          id: 'cluster:host',
          kind: MapEntityKind.cluster,
          title: '聚合',
          subtitle: '',
          latitude: 57.2,
          longitude: -2.1,
          memberIDs: [original.id],
        ),
        changes: map.readerChanges!,
        currentMembers: map.currentReaderEntities!,
        currentScope: map.currentReaderScope!,
        onVisibilityChanged: map.onMapReaderVisibilityChanged,
      );
      await t.pumpAndSettle();
      expect(
        ModalRoute.of(t.element(find.byType(MapWorkspace)))!.isCurrent,
        false,
      );
      await t.pump(const Duration(seconds: 7));
      await t.pumpAndSettle();
      expect(f.mapReads, 2);
      expect(
        find.byKey(const Key('current-map-cluster-members')),
        findsOneWidget,
        reason:
            'beforeScope=$shownScope afterScope=${map.currentReaderScope!()} items=${map.currentReaderEntities!().map((e) => e.title).toList()} lease=${f.canvas(t).layers!.publicView?.validUntil} error=${f.canvas(t).layers!.error}',
      );
      expect(find.text('当前合法地图来源 1，Aberdeen 中文混排'), findsNothing);
      expect(
        find.text('当前合法地图来源 2，Aberdeen 中文混排'),
        findsOneWidget,
        reason:
            'members=${map.currentReaderEntities!().map((e) => e.title).toList()} visibleTexts=${t.widgetList<Text>(find.byType(Text)).map((e) => e.data).toList()}',
      );
      expect(before.validUntil, until);
      await t.tap(find.text('当前合法地图来源 2，Aberdeen 中文混排'));
      await t.pumpAndSettle();
      final selected = await result;
      expect(selected?.id, original.id);
      expect(selected?.title, '当前合法地图来源 2，Aberdeen 中文混排');
      expect(f.mapReads, 2);
      expect(identical(editor, f.composer(t)), true);
      expect(
        identical(canvasElement, find.byType(MapCanvas).evaluate().single),
        true,
      );
      expect(editor.draftText, '聚合阅读保留 café 中文草稿');
      f.assertNativeOnly(t);
    } finally {
      await f.dispose(t);
    }
  });

  testWidgets('后台到期返回前台只补读原范围并保留编辑器草稿', (t) async {
    final f = _LeaseHost(lease: const Duration(seconds: 8));
    try {
      await f.mount(t);
      final layers = f.canvas(t).layers!;
      final editor = f.composer(t);
      final element = find.byType(MapCanvas).evaluate().single;
      editor.fillDraft('后台返回仍保留 café 中文草稿', requestFocus: false);
      await t.pumpAndSettle();
      for (final state in [
        AppLifecycleState.inactive,
        AppLifecycleState.hidden,
        AppLifecycleState.paused,
      ]) {
        t.binding.handleAppLifecycleStateChanged(state);
      }
      await t.pump(const Duration(seconds: 9));
      expect(f.mapReads, 1);
      expect(layers.items, isEmpty);
      for (final state in [
        AppLifecycleState.hidden,
        AppLifecycleState.inactive,
        AppLifecycleState.resumed,
      ]) {
        t.binding.handleAppLifecycleStateChanged(state);
      }
      await t.pumpAndSettle();
      expect(f.mapReads, 2);
      expect(layers.items, isNotEmpty);
      expect(layers.bounds, mapTestBounds);
      expect(identical(editor, f.composer(t)), true);
      expect(
        identical(element, find.byType(MapCanvas).evaluate().single),
        true,
      );
      expect(editor.draftText, '后台返回仍保留 café 中文草稿');
      f.assertNativeOnly(t);
    } finally {
      await f.dispose(t);
    }
  });

  testWidgets('城市弹窗跨短租期关闭原范围自动重读，提前续读仅地图且保持编辑器地图任务', (t) async {
    // One leaf, two current timer scenarios: short and normal leases both
    // renew under this owned modal, without extending the original DTO.
    for (final seconds in [3, 8]) {
      final f = _LeaseHost(lease: Duration(seconds: seconds));
      try {
        await f.mount(t);
        final canvasElement = find.byType(MapCanvas).evaluate().single;
        final mapState = t.state(find.byType(PublicCityMapView));
        final composer = f.composer(t);
        final ws = f.workspace(t), taskEpoch = ws.taskEpoch;
        final before = f.canvas(t).layers!.publicView!;
        final originalValidUntil = before.validUntil;
        final originalDeadline = f.mapReadClocks.single.add(f.lease);
        composer.fillDraft('弹窗期间保留的中文 Aberdeen café 草稿', requestFocus: false);
        await t.pumpAndSettle();
        await f.openCity(t);
        // UI settling already consumes part of the short lease. Advance to
        // the first actual renewal, bounded by the original source deadline;
        // a fixed post-modal delay can legitimately reach two renewal cycles.
        while (f.mapReads == 1 &&
            t.binding.clock.now().isBefore(originalDeadline)) {
          await t.pump(const Duration(milliseconds: 50));
        }
        expect(f.mapReadClocks, hasLength(2));
        expect(
          f.mapReadClocks[1].isBefore(originalDeadline),
          isTrue,
          reason:
              'The first renewal must begin before the original lease expires.',
        );
        await t.pumpAndSettle();
        // Still cross that original lease boundary: only the new response,
        // never an extension of the original DTO, may remain on the map.
        final crossOriginal = originalDeadline
            .add(const Duration(milliseconds: 50))
            .difference(t.binding.clock.now());
        if (crossOriginal > Duration.zero) await t.pump(crossOriginal);
        expect(
          f.mapReads,
          2,
          reason: 'Local map GET runs while the owned city modal is open.',
        );
        expect(f.canvas(t).layers!.publicView, isNotNull);
        expect(
          f.canvas(t).layers!.items.single.sourceVersion,
          'map-lease-response-2',
        );
        await f.closeCity(t);
        final layers = f.canvas(t).layers!;
        expect(f.mapReads, 2);
        expect(layers.publicView, isNotNull);
        expect(identical(layers.publicView, before), isFalse);
        expect(before.validUntil, originalValidUntil);
        expect(t.binding.clock.now().isAfter(originalDeadline), isTrue);
        expect(layers.items.single.sourceVersion, 'map-lease-response-2');
        expect(layers.error, isNull);
        expect(f.city.selection?.id, 'alpha');
        expect(f.city.selections, 0);
        expect(ws.taskEpoch, taskEpoch);
        expect(ws.conversation, isEmpty);
        expect(f.canvas(t).mapState.viewportBounds, mapTestBounds);
        expect(
          identical(canvasElement, find.byType(MapCanvas).evaluate().single),
          isTrue,
        );
        expect(
          identical(mapState, t.state(find.byType(PublicCityMapView))),
          isTrue,
        );
        expect(identical(composer, f.composer(t)), isTrue);
        expect(composer.draftText, '弹窗期间保留的中文 Aberdeen café 草稿');
        f.assertNativeOnly(t);
      } finally {
        await f.dispose(t);
      }
    }
  });

  testWidgets('无Agent地图读取失败时200%键盘仅小输入面板，退出仍保留真实错误重试', (t) async {
    final f = _LeaseHost();
    try {
      await f.mount(t);
      final canvasElement = find.byType(MapCanvas).evaluate().single;
      final mapState = t.state(find.byType(PublicCityMapView));
      final composer = f.composer(t);
      f.failMap = true;
      await f.canvas(t).layers!.load('alpha', mapTestBounds);
      await t.pumpAndSettle();
      final failure = f.canvas(t).layers!.error;
      expect(failure, contains('暂不可用'));
      expect(find.byKey(const Key('now-map-source-recovery')), findsOneWidget);
      f.scale.value = 2;
      f.keyboard.value = 300;
      composer.fillDraft('输入时不显示巨大地图错误面板 café 中文长句');
      await t.pumpAndSettle();
      expect(f.workspace(t).inputFocused, isTrue);
      expect(find.byKey(const Key('now-map-source-recovery')), findsNothing);
      expect(f.canvas(t).layers!.error, failure);
      final surface = t.getRect(find.byKey(const Key('now-primary-surface')));
      final input = t.getRect(find.byType(AgentComposer));
      expect(surface.height, lessThanOrEqualTo(input.height + 20));
      expect(surface.bottom, lessThanOrEqualTo(844 - 300 + .1));
      expect(input.height, greaterThanOrEqualTo(48));
      expect(find.byType(TextField), findsOneWidget);
      composer.pauseEditing();
      f.keyboard.value = 0;
      await t.pumpAndSettle();
      expect(find.byKey(const Key('now-map-source-recovery')), findsOneWidget);
      final retry = find.widgetWithText(TextButton, '更新地图点位');
      await t.ensureVisible(retry);
      await t.pumpAndSettle();
      expect(retry.hitTestable(), findsOneWidget);
      expect(t.getSize(retry).height, greaterThanOrEqualTo(48));
      expect(
        f.mapReads,
        2,
        reason: 'Presentation changes must not submit or auto-retry.',
      );
      f.failMap = false;
      await t.tap(retry);
      await t.pumpAndSettle();
      expect(f.mapReads, 3);
      expect(f.canvas(t).layers!.publicView, isNotNull);
      expect(f.canvas(t).layers!.error, isNull);
      expect(find.byKey(const Key('now-map-source-recovery')), findsNothing);
      expect(
        identical(canvasElement, find.byType(MapCanvas).evaluate().single),
        isTrue,
      );
      expect(
        identical(mapState, t.state(find.byType(PublicCityMapView))),
        isTrue,
      );
      expect(identical(composer, f.composer(t)), isTrue);
      expect(composer.draftText, '输入时不显示巨大地图错误面板 café 中文长句');
      expect(f.canvas(t).mapState.viewportBounds, mapTestBounds);
      f.assertNativeOnly(t);
    } finally {
      await f.dispose(t);
    }
  });

  testWidgets('城市弹窗提前原读取跨账号ABA迟到不能恢复或触发原目录关闭续读', (t) async {
    final f = _LeaseHost(lease: const Duration(seconds: 8));
    try {
      await f.mount(t);
      final layers = f.canvas(t).layers!;
      final originalEpoch = layers.epoch;
      f.holdRead = 2;
      await f.openCity(t);
      await t.pump(const Duration(seconds: 7));
      await t.pump();
      expect(f.mapReads, 2);
      expect(layers.loading, isTrue);
      // A -> B -> A before the next Flutter frame, exercising actual auth
      // listeners/retirement. Equal final auth values cannot revive epoch A.
      f.auth.changeIdentity(
        'Bearer peer',
        nextOwner: '91000000-0000-4000-8000-000000000002',
      );
      f.auth.changeIdentity(null);
      await t.pumpAndSettle();
      expect(layers.epoch, greaterThan(originalEpoch));
      expect(find.text('城市目录已变化，请关闭后重新选择。'), findsOneWidget);
      // Positive control: a fresh, current native map request is accepted.
      // Auth listeners may legitimately issue new current reads; their count
      // is not asserted as zero and never reclassified as stale restoration.
      await layers.load('alpha', mapTestBounds);
      await t.pumpAndSettle();
      final current = layers.publicView!;
      final currentSource = layers.items.single.sourceVersion;
      f.held.complete(f.mapResponse(2, title: '已撤销旧城市弹窗迟到来源'));
      await t.pumpAndSettle();
      expect(identical(layers.publicView, current), isTrue);
      expect(layers.items.single.sourceVersion, currentSource);
      expect(layers.items.any((i) => i.title == '已撤销旧城市弹窗迟到来源'), isFalse);
      expect(find.text('已撤销旧城市弹窗迟到来源'), findsNothing);
      final beforeCloseReads = f.mapReads;
      await f.closeCity(t);
      expect(f.mapReads, beforeCloseReads);
      expect(identical(layers.publicView, current), isTrue);
      expect(f.city.selection?.id, 'alpha');
      expect(f.city.selections, 0);
      expect(f.canvas(t).mapState.viewportBounds, mapTestBounds);
      f.assertNativeOnly(t);
    } finally {
      await f.dispose(t);
    }
  });
}
