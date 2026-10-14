import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_conversation.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/task_image_controller.dart';
import 'package:birdtie_client/src/workspace/task_image_page.dart';
import 'package:birdtie_client/src/workspace/agent_visual_page.dart';
import 'package:birdtie_client/src/workspace/task_image_pending_store.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'now_scope_recovery_test.dart' show NowFixtureCity;
import 'private_moment_media_controller_test.dart'
    show syntheticPrivatePNG, TestMomentImagePicker;

const owner = '22222222-2222-4222-8222-222222222222';
const taskID = '11111111-1111-4111-8111-111111111111';

class _Source extends AgentTaskSource {
  bool fail = false;
  Completer<AgentResult>? delayed;
  AgentTask? latest;
  final calls = <String>[];
  @override
  Future<AgentResult> resolve(String q, activities, places) => _turn(q, null);
  @override
  Future<AgentResult> followUp(AgentTask task, String q, activities, places) =>
      _turn(q, task);
  Future<AgentResult> _turn(String q, AgentTask? previous) async {
    calls.add(q);
    if (fail) throw const AgentRequestFailure('合成发送失败');
    if (delayed != null) return delayed!.future;
    latest = AgentTask(
      id: taskID,
      query: q,
      status: 'COMPLETED',
      cityID: 'alpha',
      contextType: 'CITY',
      contextID: '44444444-4444-4444-8444-444444444444',
      principalType: 'person',
      principalID: owner,
      actingUserID: owner,
      updatedAt: DateTime.now().toUtc(),
      messages: [
        ...?previous?.messages,
        AgentMessage(role: 'user', text: q),
        const AgentMessage(role: 'assistant', text: '合成文字回答，不读取图片'),
      ],
    );
    return AgentResult(
      entities: const [],
      activities: const [],
      places: const [],
      note: '合成文字回答，不读取图片',
      task: latest,
    );
  }
}

Future<(_Source, SeedTestAuth, List<http.Request>)> mount(
  WidgetTester t,
) async {
  t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
  FlutterSecureStorage.setMockInitialValues({});
  final png = await t.runAsync(syntheticPrivatePNG);
  final source = _Source();
  final auth = SeedTestAuth()..owner = owner;
  final city = NowFixtureCity();
  final calls = <http.Request>[];
  final client = MockClient((r) async {
    calls.add(r);
    return http.Response(
      jsonEncode({'data': []}),
      200,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );
  });
  final moments = PrivateMomentController(
    client: client,
    apiBaseUrl: 'https://synthetic.example',
    authorizationHeader: () => auth.authorizationHeader,
  );
  t.view.physicalSize = const Size(430, 1050);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
  await t.pumpWidget(
    MaterialApp(
      home: MapWorkspace(
        city: city,
        auth: auth,
        moments: moments,
        agentTaskSource: source,
        seedClient: client,
        seedApiBaseUrl: 'https://synthetic.example',
        taskImagePicker: TestMomentImagePicker(png!),
        taskImagePendingStore: MemoryTaskImagePendingStore(),
      ),
    ),
  );
  await t.pumpAndSettle();
  addTearDown(() async {
    await t.pumpWidget(const SizedBox());
    await t.pump();
    moments.dispose();
    auth.dispose();
    city.dispose();
    client.close();
  });
  return (source, auth, calls);
}

Future<TaskImageDraftController> selectLocal(WidgetTester t) async {
  await t.tap(find.byTooltip('添加材料'));
  await t.pumpAndSettle();
  await t.runAsync(() async {
    await t.tap(find.text('图片'));
    await t.pump(const Duration(milliseconds: 500));
    await Future<void>.delayed(const Duration(milliseconds: 150));
  });
  await t.pumpAndSettle();
  final draft = t.widget<AgentComposer>(find.byType(AgentComposer)).imageDraft!;
  expect(draft.localBytes, isNotNull);
  expect(draft.reviewed, isFalse);
  return draft;
}

void main() {
  testWidgets('真实Now上下文栏前插再移除不替换内部Task消息槽或地图输入实例', (t) async {
    final (source, _, requests) = await mount(t);
    final canvasElement = find.byType(MapCanvas).evaluate().single;
    final canvas = t.widget<MapCanvas>(find.byType(MapCanvas));
    final workspace = canvas.workspace;
    workspace.task = const AgentTask(
      id: taskID,
      query: '合成既有会话',
      status: 'COMPLETED',
      cityID: 'alpha',
    );
    workspace.result = const AgentResult(
      entities: [],
      activities: [],
      places: [],
      note: '合成组件结果，不是检索证据',
    );
    workspace.state = AgentViewState.results;
    workspace.conversation.addAll([
      for (var i = 0; i < 6; i++) ...[
        AgentMessage(role: 'user', text: '上下文栏测试问题 $i'),
        AgentMessage(
          role: 'assistant',
          text: '仅布局测试回答 $i：${'本文只有单元测试合成内容。' * 10}',
        ),
      ],
      const AgentMessage(role: 'assistant', text: '当前末尾测试解释。'),
    ]);
    workspace.showContent(AgentContentMode.conversation);
    await t.pumpAndSettle();
    final mainScroll = find
        .descendant(
          of: find.byType(AgentConversation),
          matching: find.byType(Scrollable),
        )
        .first;
    await t.fling(mainScroll, const Offset(0, 140), 450);
    await t.pumpAndSettle();
    final nested = t.state<NestedScrollViewState>(
      find.byType(NestedScrollView),
    );
    final inner = t.state<ScrollableState>(mainScroll).position;
    final pixels = inner.pixels;
    final composer = t.state<AgentComposerState>(find.byType(AgentComposer));
    final task = workspace.task, result = workspace.result;
    final epoch = workspace.taskEpoch;
    final messages = List<AgentMessage>.of(workspace.conversation);
    canvas.mapState.cameraSettled(
      const MapBounds(west: -2.2, south: 57.1, east: -2.0, north: 57.2),
    );
    await t.pumpAndSettle();
    expect(find.text('搜索此区域').hitTestable(), findsOneWidget);
    expect(
      t.state<NestedScrollViewState>(find.byType(NestedScrollView)),
      same(nested),
    );
    expect(t.state<ScrollableState>(mainScroll).position, same(inner));
    expect(inner.pixels, closeTo(pixels, 0.5));
    canvas.mapState.beginSearch();
    await t.pumpAndSettle();
    expect(find.text('搜索此区域'), findsNothing);
    expect(
      t.state<NestedScrollViewState>(find.byType(NestedScrollView)),
      same(nested),
    );
    expect(inner.pixels, closeTo(pixels, 0.5));
    expect(find.byType(MapCanvas).evaluate().single, same(canvasElement));
    expect(
      t.state<AgentComposerState>(find.byType(AgentComposer)),
      same(composer),
    );
    expect(workspace.task, same(task));
    expect(workspace.result, same(result));
    expect(workspace.taskEpoch, epoch);
    expect(workspace.conversation, orderedEquals(messages));
    expect(source.calls, isEmpty);
    expect(requests.where((r) => r.method != 'GET'), isEmpty);
    expect(t.takeException(), isNull);
  });

  for (final withContextLane in [false, true]) {
    testWidgets('真实Now父分支切换$withContextLane保留会话Element与手动历史位置，零新问答', (t) async {
      final (source, _, requests) = await mount(t);
      final canvas = t.widget<MapCanvas>(find.byType(MapCanvas));
      final workspace = canvas.workspace;
      workspace.task = const AgentTask(
        id: taskID,
        query: '仅合成的既有长会话',
        status: 'COMPLETED',
        cityID: 'alpha',
        principalType: 'person',
        principalID: owner,
        actingUserID: owner,
      );
      workspace.result = const AgentResult(
        entities: [],
        activities: [],
        places: [],
        note: '仅组件合成结果，不是联网回答',
      );
      workspace.state = AgentViewState.results;
      workspace.conversation.addAll([
        for (var i = 0; i < 7; i++) ...[
          AgentMessage(role: 'user', text: '测试历史问题 $i'),
          AgentMessage(
            role: 'assistant',
            text: '测试历史回答 $i：${'仅用于父组件布局与返回位置的合成文字。' * 8}',
          ),
        ],
        AgentMessage(
          role: 'assistant',
          text: '最新测试长回复：${'地点名称和地址均为单元合成，不作为真检索证据。' * 9}',
        ),
      ]);
      workspace.showContent(AgentContentMode.conversation);
      await t.pumpAndSettle();
      if (withContextLane) {
        canvas.mapState.cameraSettled(
          const MapBounds(west: -2.2, south: 57.1, east: -2.0, north: 57.2),
        );
        await t.pumpAndSettle();
      }
      final latest = workspace.conversation.last.text;
      final beforeTop = t.getTopLeft(find.text(latest)).dy;
      final scroll = find
          .descendant(
            of: find.byType(AgentConversation),
            matching: find.byType(Scrollable),
          )
          .first;
      await t.fling(scroll, const Offset(0, 140), 450);
      await t.pumpAndSettle();
      final nested = t.state<NestedScrollViewState>(
        find.byType(NestedScrollView),
      );
      final inner = t.state<ScrollableState>(scroll).position;
      final outer = nested.outerController.position;
      final pixels = inner.pixels, outerPixels = outer.pixels;
      final latestTop = t.getTopLeft(find.text(latest)).dy;
      expect(latestTop, greaterThan(beforeTop + 100));
      final mapElement = find.byType(MapCanvas).evaluate().single;
      final composerState = t.state<AgentComposerState>(
        find.byType(AgentComposer),
      );
      final draft = composerState.draftText;
      final task = workspace.task, result = workspace.result;
      final epoch = workspace.taskEpoch;
      final messages = List<AgentMessage>.of(workspace.conversation);
      final camera = canvas.mapState.viewportBounds,
          bounds = canvas.mapState.searchAreaBounds;
      await t.tap(find.byKey(const Key('agent-sheet-map-toggle')));
      await t.pumpAndSettle();
      expect(workspace.sheetExtent, AgentSheetExtent.peek);
      expect(
        nested.mounted,
        isTrue,
        reason:
            'Actual parent Stack must retain the existing conversation, not replace its sheet host.',
      );
      expect(
        t.state<NestedScrollViewState>(
          find.byType(NestedScrollView, skipOffstage: false),
        ),
        same(nested),
      );
      await t.tap(find.byKey(const Key('agent-sheet-map-toggle')));
      await t.pumpAndSettle();
      expect(
        t.state<NestedScrollViewState>(find.byType(NestedScrollView)),
        same(nested),
      );
      expect(t.state<ScrollableState>(scroll).position, same(inner));
      expect(inner.pixels, closeTo(pixels, 0.5));
      expect(outer.pixels, closeTo(outerPixels, 0.5));
      expect(t.getTopLeft(find.text(latest)).dy, closeTo(latestTop, 0.5));
      expect(find.byType(MapCanvas).evaluate().single, same(mapElement));
      expect(
        t.state<AgentComposerState>(find.byType(AgentComposer)),
        same(composerState),
      );
      expect(composerState.draftText, draft);
      expect(workspace.task, same(task));
      expect(workspace.result, same(result));
      expect(workspace.taskEpoch, epoch);
      expect(workspace.conversation, orderedEquals(messages));
      expect(canvas.mapState.viewportBounds, camera);
      expect(canvas.mapState.searchAreaBounds, bounds);
      expect(source.calls, isEmpty);
      expect(requests.where((r) => r.method != 'GET'), isEmpty);
      expect(t.takeException(), isNull);
    });
  }

  testWidgets('同城ID更换城市来源 A-B-A 不复活已检查图片', (t) async {
    await mount(t);
    final original = t.widget<MapWorkspace>(find.byType(MapWorkspace));
    final draft = await selectLocal(t);
    final alternate = NowFixtureCity();
    Future<void> replace(NowFixtureCity city) async {
      await t.pumpWidget(
        MaterialApp(
          home: MapWorkspace(
            city: city,
            auth: original.auth,
            moments: original.moments,
            agentTaskSource: original.agentTaskSource,
            seedClient: original.seedClient,
            seedApiBaseUrl: original.seedApiBaseUrl,
            taskImagePicker: original.taskImagePicker,
            taskImagePendingStore: original.taskImagePendingStore,
          ),
        ),
      );
      await t.pumpAndSettle();
    }

    await replace(alternate);
    expect(draft.current, isFalse);
    expect(draft.localBytes, isNull);
    await replace(original.city as NowFixtureCity);
    final composer = t.widget<AgentComposer>(find.byType(AgentComposer));
    expect(composer.imageDraft!.localBytes, isNull);
    expect(composer.imageDraft!.reviewed, isFalse);
    expect(draft.current, isFalse);
    await t.pumpWidget(const SizedBox());
    await t.pump();
    alternate.dispose();
  });
  testWidgets('图片分析预览往返保留像素与新文字，零文字问答', (t) async {
    final (source, _, calls) = await mount(t);
    await t.enterText(find.byType(TextField), '第一轮');
    await t.pump();
    await selectLocal(t);
    await t.tap(find.byTooltip('发送需求'));
    await t.pumpAndSettle();
    await t.runAsync(() async {
      await Future<void>.delayed(const Duration(milliseconds: 150));
    });
    await t.pumpAndSettle();
    final first = t
        .widget<AgentVisualPage>(find.byType(AgentVisualPage))
        .controller;
    expect(first.userText, '第一轮');
    final bytes = first.localPixels!.toList();
    await t.pageBack();
    await t.pumpAndSettle();
    await t.enterText(find.byType(TextField), '下一轮草稿');
    await t.pump();
    await t.tap(find.byTooltip('发送需求'));
    await t.pumpAndSettle();
    final next = t
        .widget<AgentVisualPage>(find.byType(AgentVisualPage))
        .controller;
    expect(next.userText, '下一轮草稿');
    expect(next.localPixels, bytes);
    expect(next.preview, isNull);
    expect(source.calls, isEmpty);
    await t.pageBack();
    await t.pumpAndSettle();
    expect(
      t.state<AgentComposerState>(find.byType(AgentComposer)).draftText,
      '下一轮草稿',
    );
    expect(calls.where((r) => r.method != 'GET'), isEmpty);
  });
  testWidgets('历史同文问句不充当新图片批准，不追加文字轮次', (t) async {
    final (source, _, calls) = await mount(t);
    await t.enterText(find.byType(TextField), '重复问题');
    await t.pump();
    await t.tap(find.byTooltip('发送需求'));
    await t.pumpAndSettle();
    await t.enterText(find.byType(TextField), '重复问题');
    await t.pump();
    await selectLocal(t);
    await t.tap(find.byTooltip('发送需求'));
    await t.pumpAndSettle();
    await t.runAsync(() async {
      await Future<void>.delayed(const Duration(milliseconds: 150));
    });
    await t.pumpAndSettle();
    final page = t.widget<AgentVisualPage>(find.byType(AgentVisualPage));
    expect(page.controller.userText, '重复问题');
    expect(page.controller.preview, isNull);
    expect(source.latest!.messages.length, 2);
    expect(source.calls, ['重复问题']);
    expect(calls.where((r) => r.method != 'GET'), isEmpty);
  });

  testWidgets('新图片先本机，发送只打开独立批准预览，零自动上传', (t) async {
    final (source, _, calls) = await mount(t);
    await t.enterText(find.byType(TextField), '同一条问题');
    await t.pump();
    final draft = await selectLocal(t);
    expect(source.calls, isEmpty);
    expect(calls.where((r) => r.method != 'GET'), isEmpty);
    await t.tap(find.byTooltip('发送需求'));
    await t.pumpAndSettle();
    await t.runAsync(() async {
      await Future<void>.delayed(const Duration(milliseconds: 150));
    });
    await t.pumpAndSettle();
    expect(
      find.byType(AgentVisualPage),
      findsOneWidget,
      reason:
          '${source.latest?.status} / ${draft.canUse} / '
          '${calls.map((r) => "${r.method} ${r.url.path}").toList()}',
    );
    final page = t.widget<AgentVisualPage>(find.byType(AgentVisualPage));
    expect(page.controller.userText, '同一条问题');
    expect(page.controller.localPixels, isNotNull);
    expect(page.controller.preview, isNull);
    expect(page.controller.pixelVisible, isFalse);
    expect(draft.localBytes, isNotNull);
    expect(source.calls, isEmpty);
    expect(source.latest, isNull);
    expect(calls.where((r) => r.method != 'GET'), isEmpty);
  });

  testWidgets('未配置图片分析保留材料和原文字，不回落文字请求', (t) async {
    final (source, _, calls) = await mount(t);
    source.fail = true;
    await t.enterText(find.byType(TextField), '仍需发送的问题');
    await t.pump();
    final draft = await selectLocal(t);
    await t.tap(find.byTooltip('发送需求'));
    await t.pumpAndSettle();
    expect(find.byType(TaskImagePage), findsNothing);
    final page = t.widget<AgentVisualPage>(find.byType(AgentVisualPage));
    await t.runAsync(page.controller.prepare);
    await t.pumpAndSettle();
    expect(page.controller.report, isNull);
    expect(draft.localBytes, isNotNull);
    expect(source.calls, isEmpty);
    await t.pageBack();
    await t.pumpAndSettle();
    expect(
      t.state<AgentComposerState>(find.byType(AgentComposer)).draftText,
      '仍需发送的问题',
    );
    expect(calls.where((r) => r.method != 'GET').map((r) => r.url.path), [
      '/v1/me/agent-visual/previews',
    ]);
  });

  testWidgets('账户变化清除本机像素与检查状态，迟到回复不能转交材料', (t) async {
    final (source, auth, calls) = await mount(t);
    await t.enterText(find.byType(TextField), '迟到问题');
    await t.pump();
    final draft = await selectLocal(t);
    source.delayed = Completer<AgentResult>();
    await t.tap(find.byTooltip('发送需求'));
    await t.pump();
    auth.changeIdentity(
      'Bearer synthetic-peer',
      nextOwner: '33333333-3333-4333-8333-333333333333',
    );
    await t.pumpAndSettle();
    expect(draft.current, isFalse);
    expect(draft.localBytes, isNull);
    source.delayed!.complete(
      AgentResult(
        entities: const [],
        activities: const [],
        places: const [],
        note: '迟到合成结果',
      ),
    );
    await t.pumpAndSettle();
    expect(find.byType(TaskImagePage), findsNothing);
    expect(calls.where((r) => r.method != 'GET'), isEmpty);
  });
}
