import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:birdtie_client/src/workspace/sidebar.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:birdtie_client/src/workspace/public_query_field_evidence_panel.dart';
import 'package:birdtie_client/src/workspace/now_ui_tokens.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'public_query_field_evidence_test.dart' show evidenceData, readEvidence;
import 'copy29_baseline_settings.dart';

const _captureKey = Key('copy29-isolated-visual-fixture');
const _owner = '60000000-0000-4000-8000-000000000001';
String? _font;
ThemeData _theme() {
  final base = birdtieTheme(Brightness.light);
  if (_font == null) return base;
  final themed = base.copyWith(
    textTheme: base.textTheme.apply(fontFamily: _font),
    primaryTextTheme: base.primaryTextTheme.apply(fontFamily: _font),
  );
  final roles = NowUiTokens.fromTheme(themed);
  ButtonStyle? textStyle(ButtonStyle? style) =>
      style?.copyWith(textStyle: WidgetStatePropertyAll(roles.action));
  return themed.copyWith(
    appBarTheme: themed.appBarTheme.copyWith(titleTextStyle: roles.pageTitle),
    listTileTheme: themed.listTileTheme.copyWith(
      titleTextStyle: roles.placeTitle,
      subtitleTextStyle: roles.listSubtitle,
    ),
    textButtonTheme: TextButtonThemeData(
      style: textStyle(themed.textButtonTheme.style),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: textStyle(themed.filledButtonTheme.style),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: textStyle(themed.outlinedButtonTheme.style),
    ),
    extensions: [
      ...themed.extensions.values.where((e) => e is! NowUiTokens),
      roles,
    ],
  );
}

Future<void> _mount(WidgetTester tester, Widget page) async {
  tester.view.physicalSize = const Size(390, 844);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(
    RepaintBoundary(
      key: _captureKey,
      child: MaterialApp(
        theme: _theme(),
        debugShowCheckedModeBanner: false,
        builder: (context, child) => Column(
          children: [
            Material(
              color: Theme.of(context).colorScheme.surfaceContainerHigh,
              child: const SizedBox(
                height: 28,
                child: Center(
                  child: Text(
                    '隔离视觉样例 · 非真实账号',
                    textDirection: TextDirection.ltr,
                  ),
                ),
              ),
            ),
            Expanded(child: child!),
          ],
        ),
        home: page,
      ),
    ),
  );
  await tester.pumpAndSettle();
  expect(tester.takeException(), isNull);
}

Future<void> _capture(WidgetTester tester, String name) async {
  final directory = Platform.environment['BIRDTIE_COPY29_CAPTURE'];
  if (directory == null) return;
  final stage = Platform.environment['BIRDTIE_COPY29_STAGE'] ?? 'after';
  await tester.runAsync(() async {
    final render = tester.renderObject<RenderRepaintBoundary>(
      find.byKey(_captureKey),
    );
    final image = await render.toImage(pixelRatio: 1);
    final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
    final file = File('$directory/$stage-$name.png');
    await file.parent.create(recursive: true);
    if (await file.exists())
      throw StateError('Immutable capture already exists: ${file.path}');
    await file.writeAsBytes(bytes!.buffer.asUint8List());
    image.dispose();
  });
}

void main() {
  setUpAll(() async {
    if (Platform.environment['BIRDTIE_COPY29_CAPTURE'] == null) return;
    final loader = FontLoader('Copy29FixtureCJK')
      ..addFont(
        Future.value(
          ByteData.sublistView(
            await File(r'C:\Windows\Fonts\msyh.ttc').readAsBytes(),
          ),
        ),
      );
    await loader.load();
    _font = 'Copy29FixtureCJK';
    await (FontLoader('MaterialIcons')..addFont(
          Future.value(
            ByteData.sublistView(
              await File(
                r'D:\DevTools\flutter\bin\cache\artifacts\material_fonts\MaterialIcons-Regular.otf',
              ).readAsBytes(),
            ),
          ),
        ))
        .load();
  });
  testWidgets(
    'current settings person empty and failed state render without writes',
    (tester) async {
      final auth = SeedTestAuth()
        ..owner = _owner
        ..label = '视觉样例 · 林小夏 Lin';
      var writes = 0;
      var failed = false;
      final client = MockClient((request) async {
        if (request.method != 'GET') writes++;
        return http.Response(jsonEncode({'data': []}), failed ? 503 : 200);
      });
      final city = PublicCityController(
        client: client,
        apiBaseUrl: 'https://copy29.test',
      );
      final moments = PrivateMomentController(
        authorizationHeader: () => auth.authorizationHeader,
        ownerID: () => auth.accountID,
        client: client,
        apiBaseUrl: 'https://copy29.test',
      );
      addTearDown(auth.dispose);
      addTearDown(city.dispose);
      addTearDown(moments.dispose);
      addTearDown(client.close);
      Widget page(Key key) => Scaffold(
        appBar: AppBar(title: const Text('设置')),
        body:
            (Platform.environment['BIRDTIE_COPY29_STAGE'] ?? '').startsWith(
              'before',
            )
            ? Copy29BaselineSettingsPage(
                key: key,
                auth: auth,
                city: city,
                moments: moments,
                client: client,
                apiBaseUrl: 'https://copy29.test',
              )
            : SettingsPage(
                key: key,
                auth: auth,
                city: city,
                moments: moments,
                client: client,
                apiBaseUrl: 'https://copy29.test',
              ),
      );
      await _mount(tester, page(const ValueKey('normal')));
      await _capture(tester, 'settings-person-empty');
      await tester.scrollUntilVisible(find.text('退出登录'), 200, maxScrolls: 60);
      await _capture(tester, 'settings-person-bottom');
      failed = true;
      await _mount(tester, page(const ValueKey('failed')));
      if (!(Platform.environment['BIRDTIE_COPY29_STAGE'] ?? '').startsWith(
        'before',
      )) {
        await tester.scrollUntilVisible(
          find.text('屏蔽与资料访问授权'),
          160,
          maxScrolls: 60,
        );
        await Scrollable.ensureVisible(
          tester.element(find.text('屏蔽与资料访问授权')),
          alignment: .4,
        );
        await tester.pumpAndSettle();
        await tester.tap(find.text('屏蔽与资料访问授权'));
        await tester.pumpAndSettle();
      }
      await tester.scrollUntilVisible(
        find.textContaining('隐私设置暂不可用'),
        160,
        maxScrolls: 60,
      );
      await _capture(tester, 'settings-person-error');
      expect(writes, 0);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );
  testWidgets(
    'current empty sidebar is an actual render with the original navigation',
    (tester) async {
      final workspace = AgentWorkspaceController();
      final city = PublicCityController(apiBaseUrl: '');
      final org = OrganizationWorkspaceController(
        authorizationHeader: () => null,
      );
      addTearDown(workspace.dispose);
      addTearDown(city.dispose);
      addTearDown(org.dispose);
      final scaffold = GlobalKey<ScaffoldState>();
      await _mount(
        tester,
        Scaffold(
          key: scaffold,
          drawer: Sidebar(
            workspace: workspace,
            city: city,
            organizations: org,
            signedIn: false,
            onCitySelected: (_) {},
            onNew: () {},
            onDestination: (_) {},
            onRecent: (_) {},
            onCreateOrganization: () {},
            onViewInvitations: () {},
          ),
        ),
      );
      scaffold.currentState!.openDrawer();
      await tester.pumpAndSettle();
      await _capture(tester, 'sidebar-anonymous-empty');
      expect(find.text('Now'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );
  testWidgets(
    'current original public evidence expanded and expired retain no authority',
    (tester) async {
      final data = evidenceData();
      final evidence = readEvidence(data);
      final item = decodeAgentResultItems(data['resultSet']).single;
      final changes = ChangeNotifier();
      addTearDown(changes.dispose);
      await _mount(
        tester,
        Scaffold(
          appBar: AppBar(title: const Text('活动来源')),
          body: ListView(
            padding: const EdgeInsets.all(16),
            children: [
              Text(item.title),
              PublicQueryFieldEvidencePanel(
                evidence: evidence,
                item: item,
                current: (_) => true,
                changes: changes,
              ),
            ],
          ),
        ),
      );
      await tester.tap(find.text('查看来源与时间'));
      await tester.pump();
      await _capture(tester, 'public-source-expanded');
      expect(evidence.current, isTrue);
      await tester.pump(const Duration(seconds: 21));
      await _capture(tester, 'public-source-expired');
      expect(evidence.current, isFalse);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );
}
