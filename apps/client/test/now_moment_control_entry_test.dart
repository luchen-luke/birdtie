import 'dart:convert';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'now_scope_recovery_test.dart'
    show NowFixtureCity, NowFixtureSource, nowTap, nowField;

const _owner = '82000000-0000-4000-8000-000000000001';

class _Fixture {
  final auth = SeedTestAuth()
    ..token = 'Bearer SYNTHETIC-NOW-MOMENT-A'
    ..owner = _owner;
  final city = NowFixtureCity();
  late final source = NowFixtureSource(city);
  final calls = <http.Request>[];
  late final client = MockClient((r) async {
    calls.add(r);
    return http.Response(
      jsonEncode({'data': []}),
      200,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );
  });
  late final moments = PrivateMomentController(
    authorizationHeader: () => auth.authorizationHeader,
    ownerID: () => auth.accountID,
    identityChanges: auth,
    client: client,
    apiBaseUrl: 'http://now-moment-control.test',
  );
  http.Client? replacement;
  Future<void> mount(WidgetTester t) async {
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    await t.pumpWidget(
      MaterialApp(
        home: MapWorkspace(
          auth: auth,
          city: city,
          moments: moments,
          agentTaskSource: source,
          seedClient: replacement ?? client,
          seedApiBaseUrl: 'http://now-moment-control.test',
        ),
      ),
    );
    await t.pumpAndSettle();
  }

  Future<void> open(WidgetTester t) async {
    await t.enterText(nowField(), '原Now未发送草稿');
    await nowTap(t, find.byTooltip('添加材料'));
    await nowTap(t, find.text('动态'));
    expect(find.text('新建动态'), findsOneWidget);
    await t.enterText(find.byType(TextFormField).first, '本人未保存标题');
    await t.enterText(find.byType(TextFormField).last, '本人未保存正文');
  }

  void dispose() {
    moments.dispose();
    city.dispose();
    auth.dispose();
    client.close();
    replacement?.close();
  }
}

void main() {
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  testWidgets(
    'actual Now Moment bearer renewal retains own editor and mounted map',
    (t) async {
      final f = _Fixture();
      addTearDown(f.dispose);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      await f.mount(t);
      final map = find.byType(MapCanvas).evaluate().single;
      await f.open(t);
      f.auth.changeIdentity(
        'Bearer SYNTHETIC-NOW-MOMENT-RENEWED',
        nextOwner: _owner,
      );
      await t.pumpAndSettle();
      expect(find.text('新建动态'), findsOneWidget);
      expect(
        t
            .widget<TextFormField>(find.byType(TextFormField).first)
            .controller!
            .text,
        '本人未保存标题',
      );
      expect(
        t
            .widget<TextFormField>(find.byType(TextFormField).last)
            .controller!
            .text,
        '本人未保存正文',
      );
      expect(identical(map, find.byType(MapCanvas).evaluate().single), true);
      expect(f.calls.where((r) => r.method != 'GET'), isEmpty);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
  for (final boundary in ['ownerABA', 'cityABA', 'transportABA']) {
    testWidgets(
      'actual Now Moment $boundary retires original captured editor without write',
      (t) async {
        final f = _Fixture();
        addTearDown(f.dispose);
        addTearDown(t.view.resetPhysicalSize);
        addTearDown(t.view.resetDevicePixelRatio);
        await f.mount(t);
        await f.open(t);
        final save = t
            .widget<FilledButton>(find.widgetWithText(FilledButton, '保存私人草稿'))
            .onPressed!;
        switch (boundary) {
          case 'ownerABA':
            f.auth.changeIdentity(
              'Bearer SYNTHETIC-B',
              nextOwner: '82000000-0000-4000-8000-000000000002',
            );
            f.auth.changeIdentity(
              'Bearer SYNTHETIC-NOW-MOMENT-A',
              nextOwner: _owner,
            );
          case 'cityABA':
            f.city.selectCity('beta');
            f.city.selectCity('alpha');
          case 'transportABA':
            f.replacement = MockClient(
              (r) async => http.Response('{"data":[]}', 200),
            );
            await f.mount(t);
            final old = f.replacement;
            f.replacement = null;
            await f.mount(t);
            old!.close();
        }
        save();
        await t.pumpAndSettle();
        expect(find.text('新建动态'), findsNothing);
        expect(
          t.widget<TextField>(nowField()).controller!.text,
          boundary == 'ownerABA' ? '' : '原Now未发送草稿',
        );
        expect(f.calls.where((r) => r.method != 'GET'), isEmpty);
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        await t.pumpAndSettle();
      },
    );
  }
}
