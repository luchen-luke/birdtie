import 'dart:async';
import 'dart:math' as math;

import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'chat_entity_router_test.dart' show ChatTestAuth;
import 'reply_history_restore_test.dart' as history;

class _ShellCity extends PublicCityController {
  static const origin = PublicCity(
    id: history.cityID,
    name: 'Aberdeen',
    region: '受控目录夹具',
    contentStatus: 'building',
    source: PublicSource(
      label: 'fixture',
      maintainer: 'test',
      freshness: 'unverified',
      updatedAt: null,
    ),
    map: null,
  );
  static const destination = PublicCity(
    id: 'synthetic-v2-destination',
    name: '目的合成城市',
    region: '受控目录夹具',
    contentStatus: 'building',
    source: PublicSource(
      label: 'fixture',
      maintainer: 'test',
      freshness: 'unverified',
      updatedAt: null,
    ),
    map: null,
  );
  String selected = origin.id;
  @override
  List<PublicCity> get cities => const [origin, destination];
  @override
  PublicCity get selectedCity => selected == origin.id ? origin : destination;
  @override
  void selectCity(String id, {bool rememberExploration = true}) {
    if (!cities.any((city) => city.id == id) || selected == id) return;
    selected = id;
    notifyListeners();
  }
}

class _ShortSource extends AgentTaskSource {
  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async => AgentResult(
    entities: const [],
    activities: const [],
    places: const [],
    note: '',
    message: '短回答。',
    task: AgentTask(
      id: 'synthetic-shell-task',
      query: query,
      cityID: history.cityID,
      status: 'COMPLETED',
      principalID: history.ownerID,
      actingUserID: history.ownerID,
    ),
  );
}

Future<void> _frame(
  WidgetTester t, {
  required double scale,
  AgentTaskSource? source,
  _ShellCity? city,
  ChatTestAuth? auth,
}) async {
  final selectedCity = city ?? _ShellCity(),
      identity = auth ?? (ChatTestAuth()..person = history.ownerID);
  final client = MockClient((r) async => http.Response('{"data":[]}', 200));
  final moments = PrivateMomentController(
    client: client,
    apiBaseUrl: 'https://shell.test',
    authorizationHeader: () => identity.authorizationHeader,
  );
  await t.pumpWidget(
    MaterialApp(
      theme: birdtieTheme(Brightness.light),
      builder: (context, child) => MediaQuery(
        data: MediaQuery.of(
          context,
        ).copyWith(textScaler: TextScaler.linear(scale)),
        child: child!,
      ),
      home: MapWorkspace(
        city: selectedCity,
        auth: identity,
        moments: moments,
        agentTaskSource: source ?? _ShortSource(),
        seedClient: client,
        seedApiBaseUrl: 'https://shell.test',
      ),
    ),
  );
  await t.pumpAndSettle();
  addTearDown(() async {
    await t.pumpWidget(const SizedBox());
    moments.dispose();
    selectedCity.dispose();
    identity.dispose();
    client.close();
  });
}

void _view(WidgetTester t, Size logical, {double dpr = 1}) {
  t.view.physicalSize = logical * dpr;
  t.view.devicePixelRatio = dpr;
  t.view.viewPadding = FakeViewPadding(top: 48 * dpr, bottom: 24 * dpr);
  t.view.padding = FakeViewPadding(top: 48 * dpr, bottom: 24 * dpr);
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
  addTearDown(t.view.resetViewInsets);
  addTearDown(t.view.resetViewPadding);
  addTearDown(t.view.resetPadding);
}

Rect _surface(WidgetTester t) =>
    t.getRect(find.byKey(const Key('now-primary-surface')));
AgentWorkspaceController _workspace(WidgetTester t) =>
    t.widget<MapCanvas>(find.byType(MapCanvas)).workspace;
void _completeLine(WidgetTester t) {
  final editable = t
      .state<EditableTextState>(find.byType(EditableText))
      .renderEditable;
  final input = t.widget<TextField>(find.byType(TextField));
  final padding = input.decoration!.contentPadding!.resolve(TextDirection.ltr);
  expect(
    editable.size.height,
    greaterThanOrEqualTo(editable.preferredLineHeight - .01),
  );
  expect(
    t.getSize(find.byType(TextField)).height,
    greaterThanOrEqualTo(editable.preferredLineHeight + padding.vertical - .01),
  );
  expect(find.byType(TextField), findsOneWidget);
}

void main() {
  test(
    'material identity epoch only changes at conversation/account boundaries',
    () {
      final workspace = AgentWorkspaceController();
      expect(workspace.taskIdentityEpoch, 0);
      workspace.retirePendingQueryForViewChange();
      expect(workspace.taskEpoch, 1);
      expect(workspace.taskIdentityEpoch, 0);
      workspace.newTask();
      expect(workspace.taskIdentityEpoch, 1);
      workspace.retireAccountContext();
      expect(workspace.taskIdentityEpoch, 2);
      workspace.dispose();
    },
  );
  // Actual MapWorkspace widget tree, not an isolated shell replacement. Map
  // SDK/device/real source evidence remains separate from these fixtures.
  for (final size in [
    const Size(320, 568),
    const Size(390, 844),
    const Size(430, 932),
  ]) {
    for (final scale in [1.0, 1.3, 2.0]) {
      testWidgets(
        'root full bleed, single IME and natural short result $size × $scale',
        (t) async {
          _view(t, size);
          await _frame(t, scale: scale);
          final map = find.byType(MapCanvas).evaluate().single,
              mapView = find.byType(PublicCityMapView).evaluate().single;
          final composer = t.state<AgentComposerState>(
                find.byType(AgentComposer),
              ),
              w = _workspace(t);
          final empty = _surface(t),
              material = t.widget<Material>(
                find.byKey(const Key('now-primary-surface')),
              );
          expect(empty.left, 0);
          expect(empty.right, size.width);
          expect(empty.bottom, size.height);
          expect(
            material.borderRadius,
            const BorderRadius.vertical(top: Radius.circular(24)),
          );
          expect(t.getRect(find.byType(AgentComposer)).left, 16);
          expect(t.getRect(find.byType(AgentComposer)).right, size.width - 16);
          expect(find.byKey(const Key('now-explore-handle')), findsNothing);
          expect(find.byType(AgentResultsSheet), findsNothing);
          final minimum = AgentComposer.minimumInputHeightOf(
            t.element(find.byType(AgentComposer)),
          );
          expect(
            empty.height,
            lessThanOrEqualTo(
              t.getSize(find.byType(AgentComposer)).height + 24 + 24 + .1,
            ),
          );
          composer.fillDraft('保留原草稿', requestFocus: false);
          await t.pumpAndSettle();
          final originalFocus = t
              .widget<PublicCityMapView>(find.byType(PublicCityMapView))
              .focusRevision;
          for (var cycle = 0; cycle < 2; cycle++) {
            t.view.viewInsets = FakeViewPadding(bottom: size.height * .42);
            await t.pumpAndSettle();
            expect(_surface(t).bottom, closeTo(size.height * .58, .001));
            expect(
              t
                  .widget<Scaffold>(find.byType(Scaffold).first)
                  .resizeToAvoidBottomInset,
              isFalse,
            );
            _completeLine(t);
            expect(find.byTooltip('发送需求').hitTestable(), findsOneWidget);
            expect(composer.draftText, '保留原草稿');
            expect(
              t.state<AgentComposerState>(find.byType(AgentComposer)),
              same(composer),
            );
            expect(
              t
                  .widget<PublicCityMapView>(find.byType(PublicCityMapView))
                  .focusRevision,
              originalFocus,
            );
            final contentRect = t
                .widget<PublicCityMapView>(find.byType(PublicCityMapView))
                .contentRect!;
            expect(contentRect.bottom, lessThanOrEqualTo(_surface(t).top));
            t.view.viewInsets = const FakeViewPadding();
            await t.pumpAndSettle();
            expect(_surface(t).bottom, size.height);
            expect(t.takeException(), isNull);
          }
          t.widget<AgentComposer>(find.byType(AgentComposer)).onSubmit('原始短问题');
          await t.pumpAndSettle();
          expect(w.sheetExtent, AgentSheetExtent.peek);
          expect(w.conversation.first.text, '原始短问题');
          expect(w.conversation.last.text, '短回答。');
          final short = _surface(t);
          expect(short.left, 0);
          expect(short.right, size.width);
          expect(short.bottom, size.height);
          if (scale <= 1.3)
            expect(
              short.height,
              lessThanOrEqualTo((size.height - 48 - 24) * .45 + .1),
            );
          else
            expect(short.height, lessThanOrEqualTo(size.height - 48));
          expect(short.height, greaterThanOrEqualTo(minimum + 24));
          w.setSheetExtent(AgentSheetExtent.expanded);
          await t.pumpAndSettle();
          expect(w.sheetExtent, AgentSheetExtent.expanded);
          if (scale <= 1.3) {
            expect(_surface(t).height, greaterThan(short.height));
          } else {
            // Large text can already consume the available reading viewport.
            expect(_surface(t).height, greaterThanOrEqualTo(short.height));
          }
          w.setSheetExtent(AgentSheetExtent.peek);
          await t.pumpAndSettle();
          expect(_surface(t).height, closeTo(short.height, 1));
          expect(find.byType(MapCanvas).evaluate().single, same(map));
          expect(
            find.byType(PublicCityMapView).evaluate().single,
            same(mapView),
          );
          expect(t.takeException(), isNull);
        },
      );
    }
  }

  for (final scale in [1.0, 2.0]) {
    testWidgets(
      'actual root composer grows 1–4 lines without replacing map at $scale',
      (t) async {
        _view(t, const Size(390, 844));
        await _frame(t, scale: scale);
        final composer = t.state<AgentComposerState>(
              find.byType(AgentComposer),
            ),
            map = find.byType(MapCanvas).evaluate().single;
        var previous = 0.0;
        for (final lines in [1, 2, 3, 4]) {
          composer.fillDraft(List.filled(lines, '原草稿').join('\n'));
          await t.pumpAndSettle();
          final editable = t
              .state<EditableTextState>(find.byType(EditableText))
              .renderEditable;
          final natural = math.max(
            56.0,
            lines * editable.preferredLineHeight + 16,
          );
          final height = t.getSize(find.byType(AgentComposer)).height;
          expect(height, closeTo(natural, 1));
          expect(height, greaterThanOrEqualTo(previous));
          previous = height;
          _completeLine(t);
          expect(
            (editable.offset as ScrollPosition).maxScrollExtent,
            lessThanOrEqualTo(1),
          );
          expect(find.byType(MapCanvas).evaluate().single, same(map));
          expect(find.byKey(const Key('now-explore-handle')), findsNothing);
          expect(t.takeException(), isNull);
        }
      },
    );
  }

  testWidgets(
    'actual extreme landscape IME prioritizes original scaled editor',
    (t) async {
      const dpr = 3.25;
      _view(t, const Size(2656 / dpr, 1220 / dpr), dpr: dpr);
      t.view.viewPadding = const FakeViewPadding(top: 150, bottom: 78);
      t.view.padding = const FakeViewPadding(top: 150, bottom: 78);
      await _frame(t, scale: 2);
      final composer = t.state<AgentComposerState>(find.byType(AgentComposer));
      final map = find.byType(MapCanvas).evaluate().single;
      composer.fillDraft('原第一行\n第二行\n第三行\n第四行');
      await t.pumpAndSettle();
      t.view.viewInsets = const FakeViewPadding(bottom: 1220 - 379);
      await t.pumpAndSettle();
      _completeLine(t);
      expect(_surface(t).bottom, closeTo(379 / dpr, .001));
      final editable = t
          .state<EditableTextState>(find.byType(EditableText))
          .renderEditable;
      expect(
        (editable.offset as ScrollPosition).maxScrollExtent,
        greaterThan(0),
      );
      final send = find.byTooltip('发送需求');
      expect(send.hitTestable(), findsOneWidget);
      expect(t.getSize(send).height, greaterThanOrEqualTo(48));
      expect(t.getBottomRight(send).dy, lessThanOrEqualTo(379 / dpr));
      expect(composer.draftText, '原第一行\n第二行\n第三行\n第四行');
      expect(find.byType(MapCanvas).evaluate().single, same(map));
      expect(t.takeException(), isNull);
    },
  );

  testWidgets(
    'late clipboard remains fenced by retired query serial, owned draft survives',
    (t) async {
      _view(t, const Size(390, 844));
      final city = _ShellCity();
      await _frame(t, scale: 1, city: city);
      final pending = Completer<Object?>();
      t.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        (call) async =>
            call.method == 'Clipboard.getData' ? pending.future : null,
      );
      addTearDown(
        () => t.binding.defaultBinaryMessenger.setMockMethodCallHandler(
          SystemChannels.platform,
          null,
        ),
      );
      final composer = t.state<AgentComposerState>(find.byType(AgentComposer)),
          w = _workspace(t);
      composer.fillDraft('原用户草稿', requestFocus: false);
      await t.pumpAndSettle();
      final paste = composer.pasteTextFromClipboard();
      await t.pump();
      await t.tap(find.byKey(const Key('now-city-picker')));
      await t.pumpAndSettle();
      await t.tap(
        find.byKey(const Key('now-scope-city-synthetic-v2-destination')),
      );
      await t.pumpAndSettle();
      pending.complete({'text': '迟到剪贴板不得加入'});
      await paste;
      await t.pumpAndSettle();
      expect(composer.draftText, '原用户草稿');
      expect(city.selectedCity.id, _ShellCity.destination.id);
      expect(w.taskIdentityEpoch, 0);
      expect(w.taskEpoch, greaterThan(0));
      expect(t.takeException(), isNull);
    },
  );

  testWidgets(
    'owned pasted material is cleared by account identity retirement',
    (t) async {
      _view(t, const Size(390, 844));
      final auth = ChatTestAuth()..person = history.ownerID;
      await _frame(t, scale: 1, auth: auth);
      t.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        (call) async =>
            call.method == 'Clipboard.getData' ? {'text': '只属原账号的材料'} : null,
      );
      addTearDown(
        () => t.binding.defaultBinaryMessenger.setMockMethodCallHandler(
          SystemChannels.platform,
          null,
        ),
      );
      final composer = t.state<AgentComposerState>(find.byType(AgentComposer));
      await composer.pasteTextFromClipboard();
      await t.pumpAndSettle();
      expect(composer.draftText, '只属原账号的材料');
      final before = _workspace(t).taskIdentityEpoch;
      auth.use('Bearer B');
      auth.use('Bearer A');
      await t.pumpAndSettle();
      expect(composer.draftText, isEmpty);
      expect(_workspace(t).taskIdentityEpoch, greaterThan(before));
      expect(t.takeException(), isNull);
    },
  );

  testWidgets(
    'real Remote DTO lease survives city browsing; focus revision is explicit and ABA revokes old callback',
    (t) async {
      _view(t, const Size(390, 844));
      final now = DateTime.now().toUtc(), methods = <String>[];
      final wire = history.historyWire(now),
          city = _ShellCity(),
          auth = ChatTestAuth()..person = history.ownerID;
      final source = history.historyRemote(() => wire, methods, () => now);
      await _frame(t, scale: 1, source: source, city: city, auth: auth);
      final w = _workspace(t);
      await w.reopen(
        AgentTask.fromJson(wire['task'] as Map<String, dynamic>),
        [],
        [],
      );
      w.setSheetExtent(AgentSheetExtent.peek);
      await t.pumpAndSettle();
      final reply = w.replies.first.result,
          item = w.replies.first.result.projectionItems!.single;
      expect(w.canUseReplyProjection(reply), isTrue);
      final task = w.task!,
          result = w.result!,
          native = find.byType(PublicCityMapView).evaluate().single;
      final callback = t
          .widget<AgentResultsSheet>(find.byType(AgentResultsSheet))
          .onFocusReplyPlace!;
      final revision = t
          .widget<PublicCityMapView>(find.byType(PublicCityMapView))
          .focusRevision;
      await t.tap(find.byKey(const Key('now-city-picker')));
      await t.pumpAndSettle();
      await t.tap(
        find.byKey(const Key('now-scope-city-synthetic-v2-destination')),
      );
      await t.pumpAndSettle();
      expect(w.task, same(task));
      expect(w.result, same(result));
      expect(w.canUseReplyProjection(reply), isTrue);
      callback(reply, item);
      await t.pumpAndSettle();
      expect(city.selectedCity.id, history.cityID);
      expect(w.selectedEntityId, item.entity.mapID);
      expect(
        t
            .widget<PublicCityMapView>(find.byType(PublicCityMapView))
            .focusRevision,
        revision + 1,
      );
      callback(reply, item);
      await t.pumpAndSettle();
      expect(
        t
            .widget<PublicCityMapView>(find.byType(PublicCityMapView))
            .focusRevision,
        revision + 2,
      );
      t.view.viewInsets = const FakeViewPadding(bottom: 300);
      await t.pumpAndSettle();
      t.view.viewInsets = const FakeViewPadding();
      await t.pumpAndSettle();
      expect(
        t
            .widget<PublicCityMapView>(find.byType(PublicCityMapView))
            .focusRevision,
        revision + 2,
      );
      auth.use('Bearer B');
      auth.use('Bearer A');
      await t.pumpAndSettle();
      callback(reply, item);
      await t.pumpAndSettle();
      expect(w.selectedEntityId, isNull);
      expect(w.result, isNull);
      expect(w.canUseReplyProjection(reply), isFalse);
      expect(
        t
            .widget<PublicCityMapView>(find.byType(PublicCityMapView))
            .focusRevision,
        revision + 2,
      );
      expect(find.byType(PublicCityMapView).evaluate().single, same(native));
      expect(methods, contains('GET'));
      expect(t.takeException(), isNull);
    },
  );
}
