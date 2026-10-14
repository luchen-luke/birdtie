import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/content/moment_control.dart';
import 'package:birdtie_client/src/content/moment_control_pending_store.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/now_creation_draft.dart';
import 'package:birdtie_client/src/workspace/now_creation_panel.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'moment_control_test.dart' show mcPreview;
import 'now_scope_recovery_test.dart';

const _ownerA = '82000000-0000-4000-8000-000000000001';
const _ownerB = '82000000-0000-4000-8000-000000000002';

NowCreationPanel _panel(WidgetTester t) =>
    t.widget<NowCreationPanel>(find.byType(NowCreationPanel));

AgentComposer _composer(WidgetTester t) =>
    t.widget<AgentComposer>(find.byType(AgentComposer));

VoidCallback? _creationAction(WidgetTester t, String kind) => switch (kind) {
  'moment' => _composer(t).onCreateMoment,
  'activity' => _composer(t).onCreateActivity,
  'journey' => _composer(t).onCreateJourney,
  'theme_map' => _composer(t).onCreateThemeMap,
  _ => null,
};

Future<void> _mount(WidgetTester t, NowFixture f) async {
  t.view.physicalSize = const Size(390, 844);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
  await f.mount(t);
}

Future<void> _begin(
  WidgetTester t,
  String kind, {
  bool throughMenu = false,
}) async {
  if (throughMenu) {
    // Use the real material-menu route and its real creation action.
    final state = t.state<AgentComposerState>(find.byType(AgentComposer));
    unawaited(state.showNowMaterialMenu());
    await t.pumpAndSettle();
    expect(find.text('创建草稿'), findsOneWidget);
    await nowTap(t, find.text('动态'));
  } else {
    final action = _creationAction(t, kind);
    expect(action, isNotNull);
    action!();
    await t.pumpAndSettle();
  }
  expect(_panel(t).draft.kind, kind);
}

void _expectInline(
  WidgetTester t,
  Element originalMap, {
  required int appBars,
}) {
  expect(find.byType(MapCanvas, skipOffstage: false), findsOneWidget);
  expect(t.element(find.byType(MapCanvas)), same(originalMap));
  expect(find.byType(TextField), findsOneWidget);
  expect(find.byType(TextFormField), findsNothing);
  expect(find.byType(AppBar), findsNWidgets(appBars));
  expect(find.byType(BottomSheet), findsNothing);
  expect(Navigator.of(t.element(find.byType(AgentComposer))).canPop(), isFalse);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('创建草稿只取原文首行生成 UTF-8 安全标题，正文保持完整', () {
    final firstLine = List.filled(35, '中🌿').join();
    final original = '$firstLine\n  第二行保留空格与 English 原文。  \n';
    final draft = const NowCreationDraft(
      kind: 'moment',
      cityID: 'alpha',
      cityName: '甲验收城市',
    ).withText(original);

    expect(draft.body, original);
    expect(draft.initialText, original);
    expect(draft.title.runes.length, lessThanOrEqualTo(40));
    expect(utf8.encode(draft.title).length, lessThanOrEqualTo(80));
    expect(firstLine.startsWith(draft.title), isTrue);
    expect(draft.title.runes, isNot(contains(0xfffd)));
    expect(draft.title, isNot(contains('第二行')));

    final ascii = draft
        .copyWith(title: '')
        .withText('${List.filled(60, 'a').join()}\n另一行');
    expect(ascii.title, List.filled(40, 'a').join());
    final edited = draft.withText('新正文\n原样保留');
    expect(edited.title, draft.title);
    expect(edited.body, '新正文\n原样保留');
    final titleOnly = edited.withText('我指定的新标题', titleOnly: true);
    expect(titleOnly.title, '我指定的新标题');
    expect(titleOnly.body, edited.body);
    expect(
      const NowCreationDraft(
        kind: 'theme_map',
        cityID: 'alpha',
        cityName: '甲验收城市',
      ).initialText,
      isEmpty,
    );
  });

  for (final (kind, label) in const [
    ('moment', '动态'),
    ('activity', '活动'),
    ('journey', '旅程'),
    ('theme_map', '主题地图'),
  ]) {
    testWidgets('$label 在 Now 同一输入框创建，长中文不发送给 Agent', (t) async {
      final f = NowFixture();
      addTearDown(f.dispose);
      f.auth.changeIdentity('Bearer SYNTHETIC-CREATION-A', nextOwner: _ownerA);
      await _mount(t, f);
      final map = t.element(find.byType(MapCanvas));
      final appBars = find.byType(AppBar).evaluate().length;

      await _begin(t, kind, throughMenu: kind == 'moment');
      expect(
        find.descendant(
          of: find.byType(NowCreationPanel),
          matching: find.text('创建$label'),
        ),
        findsOneWidget,
      );
      expect(_panel(t).draft.cityID, 'alpha');
      expect(_panel(t).draft.cityName, '甲验收城市');
      expect(_composer(t).creationLabel, label);
      _expectInline(t, map, appBars: appBars);

      final text = List.filled(
        8,
        '这是本人正在整理的$label内容，包含真实的中文长句和沿途想法。',
      ).join('\n');
      expect(utf8.encode(text).length, greaterThan(240));
      expect(utf8.encode(text).length, lessThanOrEqualTo(5000));
      await nowSend(t, text);

      expect(_panel(t).draft.body, text);
      expect(_panel(t).draft.title, isNotEmpty);
      expect(find.text(text), findsOneWidget);
      expect(t.widget<TextField>(nowField()).controller!.text, isEmpty);
      expect(f.source.queries, isEmpty);
      _expectInline(t, map, appBars: appBars);
      expect(t.takeException(), isNull);
      await f.unmount(t);
    });
  }

  testWidgets('折叠与关闭保留创建草稿，退出后恢复原 Now 输入', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    f.auth.changeIdentity('Bearer SYNTHETIC-CREATION-A', nextOwner: _ownerA);
    await _mount(t, f);
    final map = t.element(find.byType(MapCanvas));
    final appBars = find.byType(AppBar).evaluate().length;
    await t.enterText(nowField(), '原 Now 尚未发送的内容');
    await _begin(t, 'moment');
    await nowSend(t, '折叠关闭后仍可继续的动态正文');
    final body = _panel(t).draft.body;

    _panel(t).onToggle();
    await t.pumpAndSettle();
    expect(_panel(t).collapsed, isTrue);
    expect(_panel(t).draft.body, body);
    expect(find.text(body), findsNothing);
    _panel(t).onToggle();
    await t.pumpAndSettle();
    expect(_panel(t).collapsed, isFalse);
    expect(find.text(body), findsWidgets);

    await nowTap(t, find.byTooltip('关闭动态草稿'));
    expect(find.byType(NowCreationPanel), findsNothing);
    expect(t.widget<TextField>(nowField()).controller!.text, '原 Now 尚未发送的内容');
    await _begin(t, 'moment');
    expect(_panel(t).draft.body, body);
    expect(_panel(t).collapsed, isFalse);
    expect(f.source.queries, isEmpty);
    _expectInline(t, map, appBars: appBars);
    expect(t.takeException(), isNull);
    await f.unmount(t);
  });

  testWidgets('账号切换隐藏原账号创建内容，新账号草稿不继承原文', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    f.auth.changeIdentity('Bearer SYNTHETIC-CREATION-A', nextOwner: _ownerA);
    await _mount(t, f);
    await _begin(t, 'moment');
    const privateBody = '账号甲独有的动态草稿，账号乙不能看到。';
    await nowSend(t, privateBody);
    expect(_panel(t).draft.body, privateBody);

    f.auth.changeIdentity('Bearer SYNTHETIC-CREATION-B', nextOwner: _ownerB);
    await t.pumpAndSettle();
    expect(find.byType(NowCreationPanel), findsNothing);
    expect(find.text(privateBody), findsNothing);
    expect(
      t.widget<TextField>(nowField()).controller!.text,
      isNot(privateBody),
    );
    await _begin(t, 'moment');
    expect(_panel(t).draft.body, isEmpty);
    expect(_panel(t).draft.title, isEmpty);
    expect(f.source.queries, isEmpty);
    expect(t.takeException(), isNull);
    await f.unmount(t);
  });

  testWidgets('具体文字预览后修改输入移除确认，旧确认回调不能提交', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    f.auth.changeIdentity('Bearer SYNTHETIC-CREATION-A', nextOwner: _ownerA);
    final calls = <http.Request>[];
    final client = MockClient((request) async {
      calls.add(request);
      Object data = <Object>[];
      if (request.url.path == '/v1/me/moment-controls/previews') {
        final raw = jsonDecode(request.body) as Map<String, dynamic>;
        final input = MomentControlInput(
          operationID: raw['operationId'] as String,
          action: raw['action'] as String,
          expectedRevision: raw['expectedRevision'] as int,
          input: Map<String, Object?>.from(raw['input'] as Map),
        );
        data = mcPreview(input);
      }
      return http.Response.bytes(
        utf8.encode(jsonEncode({'data': data})),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    });
    addTearDown(client.close);
    final moments = PrivateMomentController(
      authorizationHeader: () => f.auth.authorizationHeader,
      ownerID: () => f.auth.accountID,
      identityChanges: f.auth,
      client: client,
      apiBaseUrl: f.base,
      pendingStore: MemoryMomentControlPendingStore(),
    );
    addTearDown(moments.dispose);
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    await t.pumpWidget(
      MaterialApp(
        home: MapWorkspace(
          city: f.city,
          auth: f.auth,
          moments: moments,
          agentTaskSource: f.source,
          seedClient: client,
          seedApiBaseUrl: f.base,
        ),
      ),
    );
    await t.pumpAndSettle();
    await _begin(t, 'moment');
    await nowSend(t, '这是已经检查的第一版文字。');
    // Moment control crosses real asynchronous client/storage boundaries.
    // Let those complete outside the widget clock before settling its spinner.
    await t.runAsync(() async {
      _panel(t).onSave();
      await Future<void>.delayed(const Duration(milliseconds: 30));
    });
    for (var i = 0; i < 3; i++) {
      await t.pump();
      if (!_panel(t).busy) break;
      await t.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 30)),
      );
    }
    expect(
      _panel(t).busy,
      isFalse,
      reason:
          'Moment preview must finish, not leave a busy panel. '
          'Requests: ${calls.map((r) => '${r.method} ${r.url.path}').join(', ')}; '
          'error: ${_panel(t).error}; controller: ${moments.operationError}',
    );
    await t.pumpAndSettle();

    final confirmation = find.widgetWithText(FilledButton, '确认保存这版文字草稿');
    expect(confirmation, findsOneWidget);
    final oldConfirm = t.widget<FilledButton>(confirmation).onPressed!;
    expect(
      calls.where((r) => r.url.path == '/v1/me/moment-controls/previews'),
      hasLength(1),
    );
    _panel(t).onEditBody();
    await t.pumpAndSettle();
    expect(t.widget<TextField>(nowField()).controller!.text, '这是已经检查的第一版文字。');
    await t.enterText(nowField(), '这是还没有批准的第二版文字。');
    await t.pumpAndSettle();
    expect(confirmation, findsNothing);
    oldConfirm();
    await t.pumpAndSettle();
    expect(calls.where((r) => r.method == 'PUT'), isEmpty);
    await t.testTextInput.receiveAction(TextInputAction.send);
    await t.pumpAndSettle();
    expect(_panel(t).draft.body, '这是还没有批准的第二版文字。');
    expect(f.source.queries, isEmpty);
    expect(t.takeException(), isNull);
    await f.unmount(t);
  });
}
