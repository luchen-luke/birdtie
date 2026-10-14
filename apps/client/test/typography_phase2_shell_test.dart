import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/legacy/legacy_shell.dart';
import 'package:birdtie_client/src/workspace/activity_participations.dart';
import 'package:birdtie_client/src/workspace/activity_plans.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/entity_peek_card.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_layer_controls.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:birdtie_client/src/workspace/map_layers_controller.dart';
import 'package:birdtie_client/src/workspace/now_ui_tokens.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:birdtie_client/src/workspace/saved_items.dart';
import 'package:birdtie_client/src/workspace/sidebar.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'map_layers_api_test.dart' as map_fixture;
import 'now_scope_recovery_test.dart' show NowFixture;

const _mixed = '阿伯丁 Aberdeen 的周末中文与 English 城市探索、餐厅和朋友聚会详细安排';
const _captureKey = Key('typography-shell-widget-fixture');
String? _font;

// Screenshot display fonts are test-only; production keeps platform fonts.
ThemeData _theme(Brightness brightness) {
  final original = birdtieTheme(brightness);
  if (_font == null) return original;
  final base = original.copyWith(
    textTheme: original.textTheme.apply(fontFamily: _font),
  );
  final roles = NowUiTokens.fromTheme(base);
  ButtonStyle? fontStyle(ButtonStyle? style) =>
      style?.copyWith(textStyle: WidgetStatePropertyAll(roles.action));
  return base.copyWith(
    appBarTheme: base.appBarTheme.copyWith(titleTextStyle: roles.pageTitle),
    listTileTheme: base.listTileTheme.copyWith(
      titleTextStyle: roles.placeTitle,
      subtitleTextStyle: roles.metadata,
    ),
    textButtonTheme: TextButtonThemeData(
      style: fontStyle(base.textButtonTheme.style),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: fontStyle(base.filledButtonTheme.style),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: fontStyle(base.outlinedButtonTheme.style),
    ),
    extensions: [
      ...base.extensions.values.where((e) => e is! NowUiTokens),
      roles,
    ],
  );
}

Future<void> _mount(
  WidgetTester tester,
  Widget child,
  Brightness brightness,
  double scale,
) async {
  tester.view.physicalSize = const Size(320, 780);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(
    MaterialApp(
      theme: _theme(brightness),
      builder: (context, child) => MediaQuery(
        data: MediaQuery.of(
          context,
        ).copyWith(textScaler: TextScaler.linear(scale)),
        child: child!,
      ),
      home: RepaintBoundary(key: _captureKey, child: child),
    ),
  );
  await tester.pumpAndSettle();
  expect(tester.takeException(), isNull);
}

Future<void> _capture(WidgetTester tester, String name) async {
  final directory = Platform.environment['BIRDTIE_SHELL_CAPTURE'];
  if (directory == null) return;
  await tester.runAsync(() async {
    final boundary = tester.renderObject<RenderRepaintBoundary>(
      find.byKey(_captureKey),
    );
    final image = await boundary.toImage(pixelRatio: 2);
    final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
    final file = File('$directory/$name.png');
    await file.parent.create(recursive: true);
    await file.writeAsBytes(bytes!.buffer.asUint8List());
    image.dispose();
  });
}

void main() {
  setUpAll(() async {
    if (Platform.environment['BIRDTIE_SHELL_CAPTURE'] == null) return;
    final font = File(r'C:\Windows\Fonts\msyh.ttc');
    final loader = FontLoader('ShellFixtureCJK')
      ..addFont(Future.value(ByteData.sublistView(await font.readAsBytes())));
    await loader.load();
    _font = 'ShellFixtureCJK';
    final icons = File(
      r'D:\DevTools\flutter\bin\cache\artifacts\material_fonts\MaterialIcons-Regular.otf',
    );
    if (await icons.exists()) {
      await (FontLoader('MaterialIcons')..addFont(
            Future.value(ByteData.sublistView(await icons.readAsBytes())),
          ))
          .load();
    }
  });

  for (final brightness in Brightness.values) {
    for (final scale in [1.0, 1.3, 2.0]) {
      final label = '${brightness.name}-${(scale * 100).round()}';
      testWidgets('sidebar long history and 48px actions $label', (
        tester,
      ) async {
        final fixture = NowFixture();
        final workspace = AgentWorkspaceController();
        final organizations = OrganizationWorkspaceController(
          authorizationHeader: () => null,
        );
        addTearDown(fixture.dispose);
        addTearDown(workspace.dispose);
        addTearDown(organizations.dispose);
        workspace.recent.add(
          const AgentTask(id: 'fixture-history', query: _mixed, status: 'done'),
        );
        var selected = false, created = false;
        await _mount(
          tester,
          Scaffold(
            drawer: Sidebar(
              workspace: workspace,
              city: fixture.city,
              onCitySelected: (_) {},
              onNew: () => created = true,
              onDestination: (_) {},
              onRecent: (_) => selected = true,
              organizations: organizations,
              onCreateOrganization: () {},
              onViewInvitations: () {},
              signedIn: false,
            ),
          ),
          brightness,
          scale,
        );
        tester.state<ScaffoldState>(find.byType(Scaffold)).openDrawer();
        await tester.pumpAndSettle();
        final text = tester.widget<Text>(find.text(_mixed));
        expect(text.maxLines, isNull);
        expect(text.overflow, isNull);
        await _capture(tester, 'sidebar-$label');
        final button = find.byKey(const Key('sidebar-new-conversation'));
        expect(tester.getRect(button).height, greaterThanOrEqualTo(48 - 1e-7));
        expect(
          tester.getRect(find.byKey(const Key('sidebar-account'))).size,
          const Size(48, 48),
        );
        await tester.tap(find.text(_mixed));
        expect(selected, isTrue);
        await tester.tap(button);
        expect(created, isTrue);
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
      });

      testWidgets(
        'entity card wraps mixed title and action remains reachable $label',
        (tester) async {
          var opened = false;
          await _mount(
            tester,
            Scaffold(
              body: SingleChildScrollView(
                padding: const EdgeInsets.all(16),
                child: EntityPeekCard(
                  entity: const MapEntity(
                    id: 'fixture-card',
                    kind: MapEntityKind.place,
                    title: _mixed,
                    subtitle: '',
                    latitude: 57.15,
                    longitude: -2.1,
                  ),
                  onOpen: () => opened = true,
                ),
              ),
            ),
            brightness,
            scale,
          );
          expect(tester.widget<Text>(find.text(_mixed)).maxLines, isNull);
          await _capture(tester, 'entity-$label');
          final action = find.widgetWithText(TextButton, '查看');
          await tester.ensureVisible(action);
          await tester.pumpAndSettle();
          expect(
            tester.getRect(action).height,
            greaterThanOrEqualTo(48 - 1e-7),
          );
          await tester.tap(action);
          expect(opened, isTrue);
          expect(tester.takeException(), isNull);
        },
      );

      testWidgets('map layer scroll and long original card $label', (
        tester,
      ) async {
        final view = map_fixture.mapTestView();
        for (final row in view['items'] as List) {
          row['title'] = _mixed;
        }
        final client = MockClient(
          (_) async => http.Response(
            jsonEncode({'data': view}),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          ),
        );
        final controller = MapLayersController(
          api: MapLayersApi(client: client, apiBaseUrl: 'http://fixture.test'),
          authorizationHeader: () => null,
          organizationWorkspaceID: () => null,
        );
        var released = false;
        addTearDown(() {
          if (!released) controller.dispose();
        });
        addTearDown(client.close);
        await controller.load('owned-city', map_fixture.mapTestBounds);
        String? opened;
        await _mount(
          tester,
          Scaffold(
            body: MapLayerControls(
              controller: controller,
              current: () => true,
              onRefresh: () {},
              onOpen: (item) => opened = item.mapID,
            ),
          ),
          brightness,
          scale,
        );
        await _capture(tester, 'layers-$label');
        final card = find.byKey(
          const ValueKey('map-layer-card-business:${map_fixture.mapTestID}'),
        );
        await tester.scrollUntilVisible(
          card,
          240,
          scrollable: find.byType(Scrollable),
        );
        await tester.pumpAndSettle();
        await tester.tap(card);
        expect(opened, 'business:${map_fixture.mapTestID}');
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
        controller.dispose();
        released = true;
      });

      testWidgets('existing account page long Chinese natural scroll $label', (
        tester,
      ) async {
        final fixture = NowFixture();
        addTearDown(fixture.dispose);
        await _mount(
          tester,
          Scaffold(
            body: LegacyProfilePage(
              auth: fixture.auth,
              moments: fixture.moments,
              city: fixture.city,
              client: fixture.client,
              apiBaseUrl: fixture.base,
            ),
          ),
          brightness,
          scale,
        );
        final title = tester.widget<Text>(find.text('按自己的方式体验城市生活'));
        expect(title.maxLines, isNull);
        expect(title.style!.fontSize, 20);
        expect(title.style!.fontWeight, FontWeight.w600);
        await _capture(tester, 'profile-$label');
        await tester.drag(
          find.byType(SingleChildScrollView),
          const Offset(0, -300),
        );
        await tester.pumpAndSettle();
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
      });

      testWidgets(
        'Plans sections and last reminder scroll at narrow width $label',
        (tester) async {
          final auth = SeedTestAuth();
          final fixture = NowFixture();
          final client = MockClient(
            (_) async => http.Response(
              '{"data":[]}',
              200,
              headers: {'content-type': 'application/json'},
            ),
          );
          final plans = ActivityPlansController(
            authorizationHeader: () => auth.authorizationHeader,
            client: client,
            apiBaseUrl: 'http://fixture.test',
          );
          final attendance = ActivityParticipationController(
            authorizationHeader: () => auth.authorizationHeader,
            client: client,
            apiBaseUrl: 'http://fixture.test',
          );
          final saved = SavedController(
            authorizationHeader: () => auth.authorizationHeader,
            client: client,
            apiBaseUrl: 'http://fixture.test',
          );
          final organizations = OrganizationWorkspaceController(
            authorizationHeader: () => auth.authorizationHeader,
            client: client,
            apiBaseUrl: 'http://fixture.test',
          );
          addTearDown(auth.dispose);
          addTearDown(fixture.dispose);
          addTearDown(client.close);
          addTearDown(plans.dispose);
          addTearDown(attendance.dispose);
          addTearDown(saved.dispose);
          addTearDown(organizations.dispose);
          await _mount(
            tester,
            Scaffold(
              body: MyActivitiesPage(
                plans: plans,
                participations: attendance,
                saved: saved,
                auth: auth,
                city: fixture.city,
                organizations: organizations,
              ),
            ),
            brightness,
            scale,
          );
          expect(tester.widget<Text>(find.text('我发起的')).style!.fontSize, 16);
          await _capture(tester, 'plans-$label');
          await tester.scrollUntilVisible(
            find.text('个人提醒'),
            220,
            scrollable: find.byType(Scrollable),
          );
          await tester.pumpAndSettle();
          expect(find.text('个人提醒').hitTestable(), findsOneWidget);
          expect(tester.takeException(), isNull);
          await tester.pumpWidget(const SizedBox());
        },
      );
    }
  }
}
