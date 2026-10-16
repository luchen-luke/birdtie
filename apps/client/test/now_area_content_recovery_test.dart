import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_login_sheet.dart';
import 'package:birdtie_client/src/auth/dev_phone_login_sheet.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'content_map_controller_test.dart' as owned;
import 'now_scope_recovery_test.dart' show NowFixtureCity;

// Real host/listeners/decoders with isolated HTTP and deterministic auth state.
// No provider, production login, device storage or business writes are used.
class _RecoveryAuth extends SeedTestAuth {
  bool restoring = false, failed = false, required = false;
  bool authenticationBusy = false;
  String? rejectedOwner;
  Completer<bool>? recovery;
  Future<bool>? flight;
  int recoveries = 0;
  final expectedTokens = <String?>[];

  void setLoginBusy(bool value) {
    authenticationBusy = value;
    notifyListeners();
  }

  void publishColdRecovery({
    required bool pending,
    bool networkFailed = false,
  }) {
    restoring = pending;
    failed = networkFailed;
    notifyListeners();
  }

  @override
  bool get sessionRestoring => restoring;
  @override
  bool get sessionRecoveryFailed => failed;
  @override
  bool get sessionRecoveryRequired => required;
  @override
  bool get devPhoneAvailable => true;
  @override
  bool get busy => authenticationBusy;
  @override
  String? get rejectedSessionOwnerID => rejectedOwner;

  @override
  Future<bool> recoverSession({String? expectedAuthorization}) {
    if (flight != null) return flight!;
    if (expectedAuthorization != null && token != expectedAuthorization) {
      return Future.value(false);
    }
    recoveries++;
    expectedTokens.add(expectedAuthorization);
    rejectedOwner = owner;
    token = null;
    restoring = true;
    failed = required = false;
    final gate = recovery = Completer<bool>();
    final result = gate.future.then((success) {
      if (success) {
        token = 'Bearer recovered';
        rejectedOwner = null;
      } else {
        failed = true;
      }
      restoring = false;
      flight = null;
      notifyListeners();
      return success;
    });
    flight = result;
    notifyListeners();
    return result;
  }
}

class _Host {
  _Host(this.auth) {
    client = MockClient((request) async {
      final now = DateTime.now().toUtc();
      if (request.url.path.endsWith('/map-layers')) {
        typedReads++;
        return http.Response(
          jsonEncode({
            'data': {
              'schemaVersion': 'typed-map-layers-v1',
              'cityId': 'alpha',
              'scope': 'PUBLIC',
              'observedAt': now.toIso8601String(),
              'validUntil': now
                  .add(const Duration(minutes: 2))
                  .toIso8601String(),
              'truncated': false,
              'items': [],
            },
          }),
          200,
          headers: jsonHeaders,
        );
      }
      if (request.url.path == '/v1/me/map-content') {
        ownedReads++;
        await ownedGate?.future;
        if (ownedStatus != 200) {
          return http.Response(
            '{"error":{"code":"unauthorized"}}',
            ownedStatus,
            headers: jsonHeaders,
          );
        }
        fixture.now = DateTime.now().toUtc();
        fixture.owner = auth.owner;
        return http.Response(
          jsonEncode({'data': fixture.page(request.url)}),
          200,
          headers: {
            ...jsonHeaders,
            'cache-control': 'no-store',
            'x-content-type-options': 'nosniff',
          },
        );
      }
      return http.Response('{"data":[]}', 200, headers: jsonHeaders);
    });
    moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      client: client,
      apiBaseUrl: owned.base,
    );
  }

  static const jsonHeaders = {
    'content-type': 'application/json; charset=utf-8',
  };
  final _RecoveryAuth auth;
  final city = NowFixtureCity();
  final fixture = owned.Fixture();
  late final MockClient client;
  late final PrivateMomentController moments;
  late MapCanvas canvas;
  late Element mapElement;
  int typedReads = 0, ownedReads = 0, ownedStatus = 200;
  Completer<void>? ownedGate;

  Future<void> mount(WidgetTester t, {bool loadOwned = false}) async {
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    await t.pumpWidget(
      MaterialApp(
        home: MapWorkspace(
          city: city,
          auth: auth,
          moments: moments,
          seedClient: client,
          seedApiBaseUrl: owned.base,
        ),
      ),
    );
    await t.pumpAndSettle();
    canvas = t.widget<MapCanvas>(find.byType(MapCanvas));
    mapElement = find.byType(PublicCityMapView).evaluate().single;
    final map = t.widget<PublicCityMapView>(find.byType(PublicCityMapView));
    map.onViewportInitialized!(owned.bounds);
    map.onViewportSettled!(owned.bounds);
    if (loadOwned) map.onContentViewportSettled!(owned.bounds, 13);
    await t.pumpAndSettle();
    if (loadOwned) await owned.drain(t, () => !canvas.mapContent!.loading);
  }

  Future<void> search(WidgetTester t) async {
    await t.tap(find.byKey(const Key('now-map-area-command')));
    await t.pump();
  }

  void stableMap(WidgetTester t) {
    expect(
      identical(canvas, t.widget<MapCanvas>(find.byType(MapCanvas))),
      isTrue,
    );
    expect(
      identical(mapElement, find.byType(PublicCityMapView).evaluate().single),
      isTrue,
    );
    expect(canvas.mapState.viewportBounds, owned.bounds);
    expect(t.takeException(), isNull);
  }

  Future<void> dispose(WidgetTester t) async {
    if (ownedGate != null && !ownedGate!.isCompleted) ownedGate!.complete();
    await t.pumpWidget(const SizedBox.shrink());
    moments.dispose();
    auth.dispose();
    city.dispose();
    client.close();
    fixture.close();
    t.view.resetPhysicalSize();
    t.view.resetDevicePixelRatio();
    await t.pump();
  }
}

void main() {
  testWidgets(
    'map login tap reaches existing fixed-code sheet and can reopen after return',
    (t) async {
      final f = _Host(_RecoveryAuth()..token = null);
      try {
        await f.mount(t);
        t.view.physicalSize = const Size(1220, 2656);
        t.view.devicePixelRatio = 3.25;
        t.view.padding = const FakeViewPadding(top: 150);
        t.view.viewPadding = t.view.padding;
        addTearDown(t.view.resetPadding);
        addTearDown(t.view.resetViewPadding);
        await t.pumpAndSettle();
        final button = find.byKey(const Key('now-map-content-recovery'));
        f.auth.setLoginBusy(true);
        await t.pumpAndSettle();
        expect(button, findsNothing);
        expect(find.text('正在处理登录，请稍候…'), findsOneWidget);
        expect(find.byType(BirdtieLoginSheet), findsNothing);
        f.auth.setLoginBusy(false);
        await t.pumpAndSettle();
        expect(button.hitTestable(), findsOneWidget);
        expect(
          find.descendant(of: find.byType(MapCanvas), matching: button),
          findsNothing,
        );
        expect(
          find.descendant(
            of: find.byKey(const Key('now-map-access-overlay')),
            matching: button,
          ),
          findsOneWidget,
        );
        await t.tap(button);
        await t.pumpAndSettle();
        expect(find.byType(BirdtieLoginSheet), findsOneWidget);
        await t.tap(find.text('手机号测试登录'));
        await t.pumpAndSettle();
        expect(find.byType(DevPhoneLoginSheet), findsOneWidget);
        expect(find.text('手机号'), findsOneWidget);
        // Close without requesting a code or submitting any login.
        Navigator.of(t.element(find.byType(DevPhoneLoginSheet))).pop();
        await t.pumpAndSettle();
        expect(find.byType(DevPhoneLoginSheet), findsNothing);
        f.stableMap(t);
        f.auth.required = true;
        f.auth.publishColdRecovery(pending: false);
        await t.pumpAndSettle();
        await t.tap(button);
        await t.pumpAndSettle();
        expect(find.byType(BirdtieLoginSheet), findsOneWidget);
        await t.tap(find.text('返回'));
        await t.pumpAndSettle();
        f.stableMap(t);
        expect(f.auth.recoveries, 0);
      } finally {
        await f.dispose(t);
      }
    },
  );

  testWidgets(
    'guest area is incomplete rather than empty and offers explicit login',
    (t) async {
      final f = _Host(_RecoveryAuth()..token = null);
      try {
        await f.mount(t);
        expect(find.text('登录后可查看地图动态。'), findsOneWidget);
        expect(
          find.byKey(const Key('now-map-content-recovery')),
          findsOneWidget,
        );
        expect(find.byType(BottomSheet), findsNothing);
        await f.search(t);
        await t.pumpAndSettle();
        final area = f.canvas.workspace.areaOperation!;
        expect(area.status, AgentAreaStatus.partial);
        expect(area.read!.content!.available, isFalse);
        expect(area.summary, '登录后可查看地图动态。');
        expect(find.text('本次未找到符合条件的结果。'), findsNothing);
        expect(
          find.byKey(const Key('now-area-content-recovery')),
          findsOneWidget,
        );
        expect(f.ownedReads, 0);
        expect(f.auth.recoveries, 0);
        f.auth.publishColdRecovery(pending: true);
        await t.pump();
        expect(find.text('正在恢复登录，随后读取地图动态…'), findsOneWidget);
        f.auth.publishColdRecovery(pending: false, networkFailed: true);
        await t.pumpAndSettle();
        expect(
          f.canvas.workspace.areaOperation!.status,
          AgentAreaStatus.partial,
        );
        expect(f.canvas.workspace.areaOperation!.summary, '暂时无法恢复登录，地图动态尚未读取。');
        expect(find.byType(BottomSheet), findsNothing);
        f.stableMap(t);
      } finally {
        await f.dispose(t);
      }
    },
  );

  testWidgets(
    'area waits for owned content and retains its current grant and shorter lease',
    (t) async {
      final f = _Host(_RecoveryAuth()..owner = owned.id(9000));
      try {
        await f.mount(t);
        f.ownedGate = Completer<void>();
        await f.search(t);
        await owned.drain(t, () => f.ownedReads > 0);
        expect(f.typedReads, greaterThan(0));
        expect(f.canvas.layers!.publicView!.items, isEmpty);
        expect(
          f.canvas.workspace.areaOperation!.status,
          AgentAreaStatus.loading,
        );
        f.ownedGate!.complete();
        await owned.drain(
          t,
          () =>
              f.canvas.workspace.areaOperation!.status !=
              AgentAreaStatus.loading,
        );
        await t.pumpAndSettle();
        final area = f.canvas.workspace.areaOperation!;
        expect(area.status, AgentAreaStatus.success);
        expect(area.read!.content!.locationCount, 1);
        expect(area.summary, '动态分布在 1 处地图位置。');
        expect(
          area.read!.remaining!,
          lessThanOrEqualTo(const Duration(seconds: 30)),
        );
        expect(f.canvas.mapContent!.items, hasLength(1));
        f.canvas.mapContent!.retire();
        await t.pump();
        expect(area.current, isFalse);
        expect(area.summary, '本次地图来源已失效，请重新搜索。');
        expect(f.canvas.mapContent!.items, isEmpty);
        f.stableMap(t);
      } finally {
        await f.dispose(t);
      }
    },
  );

  testWidgets(
    'rejected access recovers through retry without automatic login or camera recreation',
    (t) async {
      final f = _Host(_RecoveryAuth()..owner = owned.id(9000));
      try {
        await f.mount(t, loadOwned: true);
        expect(f.canvas.mapContent!.current, isTrue);
        final oldPin = f.canvas.mapContent!.items.single;
        f.ownedStatus = 401;
        unawaited(f.canvas.mapContent!.refresh());
        await owned.drain(t, () => f.auth.restoring);
        await t.pump();
        expect(f.auth.expectedTokens, ['Bearer owner']);
        expect(find.text('正在恢复登录，随后读取地图动态…'), findsOneWidget);
        expect(find.byType(BottomSheet), findsNothing);
        expect(f.canvas.mapContent!.sourceCurrent(oldPin), isFalse);
        f.auth.recovery!.complete(false);
        await owned.drain(t, () => !f.auth.restoring);
        await t.pumpAndSettle();
        expect(find.text('暂时无法恢复登录，地图动态尚未读取。'), findsOneWidget);
        expect(find.byType(BottomSheet), findsNothing);
        f.ownedStatus = 200;
        await t.tap(find.byKey(const Key('now-map-content-recovery')));
        await t.pump();
        expect(f.auth.recoveries, 2);
        expect(f.auth.expectedTokens.last, isNull);
        f.auth.recovery!.complete(true);
        await owned.drain(t, () => f.canvas.mapContent!.current);
        await t.pumpAndSettle();
        expect(find.byKey(const Key('now-map-content-recovery')), findsNothing);
        expect(f.canvas.mapContent!.items, hasLength(1));
        expect(f.canvas.mapContent!.sourceCurrent(oldPin), isFalse);
        expect(find.byType(BottomSheet), findsNothing);
        f.stableMap(t);
      } finally {
        await f.dispose(t);
      }
    },
  );
}
