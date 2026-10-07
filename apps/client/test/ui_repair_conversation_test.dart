import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/agent_conversation.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

// These fixtures exercise local UI contracts, not live retrieval or a device.
const _firstID = '11111111-1111-4111-8111-111111111111';
const _secondID = '22222222-2222-4222-8222-222222222222';
const _firstAnswer = '先看这处符合当前条件的地点，再决定是否继续缩小范围。';
const _secondAnswer = '按照你的追问，当前结果已经改为另一处地点。';

void main() {
  testWidgets('explanation, clickable card and source share one message flow', (
    tester,
  ) async {
    final reply = _placeReply(_firstID, '测试地点甲', _firstAnswer, '甲');
    final workspace = await _workspace([reply]);
    addTearDown(workspace.dispose);
    AgentResultItem? opened;
    await _pumpSheet(tester, workspace, onOpen: (item) => opened = item);

    final flow = find.byType(AgentConversation);
    final attached = find.byKey(const ValueKey('reply-1'));
    final card = find.byKey(const Key('agent-entity-place:$_firstID'));
    final source = find.byKey(const Key('agent-source-place:$_firstID'));
    expect(flow, findsOneWidget);
    expect(
      find.descendant(of: flow, matching: find.text(_firstAnswer)),
      findsOneWidget,
    );
    expect(find.descendant(of: flow, matching: card), findsOneWidget);
    expect(find.descendant(of: attached, matching: source), findsOneWidget);
    expect(
      find.descendant(of: flow, matching: find.byType(ListView)),
      findsOneWidget,
    );
    expect(
      find.descendant(of: attached, matching: find.byType(ListView)),
      findsNothing,
    );
    expect(
      tester.getTopLeft(find.text(_firstAnswer)).dy,
      lessThan(tester.getTopLeft(card).dy),
    );
    expect(tester.widget<TextButton>(source).onPressed, isNotNull);
    expect(find.textContaining('来源：本地测试来源甲'), findsOneWidget);

    await tester.tap(card);
    expect(opened, same(reply.projectionItems!.single));
    expect(opened!.detail, opened!.entity);
    expect(opened!.entity.mapID, reply.entities.single.id);
    expect(tester.takeException(), isNull);
  });

  testWidgets('one flow has no separate result or conversation mode switch', (
    tester,
  ) async {
    final workspace = await _workspace([
      _placeReply(_firstID, '测试地点甲', _firstAnswer, '甲'),
    ]);
    addTearDown(workspace.dispose);
    await _pumpSheet(tester, workspace);

    expect(find.byType(AgentConversation), findsOneWidget);
    expect(find.byType(SegmentedButton<AgentContentMode>), findsNothing);
    expect(find.byType(TabBar), findsNothing);
    for (final label in ['查看结果', '继续对话', '打开对话', '切换到结果']) {
      expect(find.text(label), findsNothing, reason: label);
    }
    final toggle = find.byKey(const Key('agent-sheet-map-toggle'));
    expect(toggle.hitTestable(), findsOneWidget);
    expect(find.byTooltip('查看地图'), findsOneWidget);
    expect(tester.widget<IconButton>(toggle).onPressed, isNotNull);
    await tester.tap(toggle);
    await tester.pumpAndSettle();
    expect(workspace.sheetExtent, AgentSheetExtent.peek);
    expect(find.byTooltip('展开对话'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  for (final reference in <String, bool>{
    'http://example.org/public-place': true,
    'https://example.org/public-place': true,
    'dev-seed://place/test': false,
    'javascript:alert(1)': false,
    'https://user:password@example.org/public-place': false,
    '/public-place': false,
  }.entries) {
    testWidgets('source protocol guard: ${reference.key}', (tester) async {
      final workspace = await _workspace([
        _placeReply(
          _firstID,
          '测试地点甲',
          _firstAnswer,
          '甲',
          sourceReference: reference.key,
        ),
      ]);
      addTearDown(workspace.dispose);
      await _pumpSheet(tester, workspace);

      final source = find.byKey(const Key('agent-source-place:$_firstID'));
      expect(source, findsOneWidget);
      expect(
        tester.widget<TextButton>(source).onPressed != null,
        reference.value,
      );
      expect(
        find.descendant(
          of: find.byKey(const ValueKey('reply-1')),
          matching: source,
        ),
        findsOneWidget,
      );
      // The platform launcher remains NOT_RUN: no button is pressed here.
      expect(tester.takeException(), isNull);
    });
  }

  testWidgets('each turn retains its own cards and historical map selection', (
    tester,
  ) async {
    final first = _placeReply(_firstID, '测试地点甲', _firstAnswer, '甲');
    final second = _placeReply(_secondID, '测试地点乙', _secondAnswer, '乙');
    final workspace = await _workspace([first, second]);
    addTearDown(workspace.dispose);
    await workspace.submit(
      '换一个更合适的地点',
      const [],
      const [],
      cityID: 'test-city',
    );
    final task = workspace.task;
    AgentResultItem? opened;
    await _pumpSheet(
      tester,
      workspace,
      availableHeight: 1400,
      surfaceHeight: 1440,
      onOpen: (item) => opened = item,
    );

    expect(workspace.replies.map((reply) => reply.messageIndex), [1, 3]);
    expect(workspace.replies.first.result, same(first));
    expect(workspace.replies.last.result, same(second));
    final oldReply = find.byKey(const ValueKey('reply-1'));
    final newReply = find.byKey(const ValueKey('reply-3'));
    final oldCard = find.byKey(const Key('agent-entity-place:$_firstID'));
    final newCard = find.byKey(const Key('agent-entity-place:$_secondID'));
    expect(find.descendant(of: oldReply, matching: oldCard), findsOneWidget);
    expect(find.descendant(of: oldReply, matching: newCard), findsNothing);
    expect(find.descendant(of: newReply, matching: newCard), findsOneWidget);
    expect(find.descendant(of: newReply, matching: oldCard), findsNothing);
    expect(
      tester.getTopLeft(find.text(_firstAnswer)).dy,
      lessThan(tester.getTopLeft(oldCard).dy),
    );
    expect(
      tester.getTopLeft(oldCard).dy,
      lessThan(tester.getTopLeft(find.text(_secondAnswer)).dy),
    );
    expect(
      tester.getTopLeft(find.text(_secondAnswer)).dy,
      lessThan(tester.getTopLeft(newCard).dy),
    );
    expect(tester.widget<ListTile>(oldCard).onTap, isNull);
    expect(tester.widget<ListTile>(newCard).onTap, isNotNull);
    await tester.tap(newCard);
    expect(opened, same(second.projectionItems!.single));

    await tester.tap(
      find.descendant(of: oldReply, matching: find.text('地图查看')),
    );
    await tester.pumpAndSettle();
    expect(workspace.presentedResult, same(first));
    expect(workspace.result, same(second));
    expect(workspace.task, same(task));
    expect(workspace.selectedEntityId, 'place:$_firstID');
    expect(workspace.sheetExtent, AgentSheetExtent.peek);
    expect(workspace.replies.length, 2);

    await tester.tap(find.byKey(const Key('agent-sheet-map-toggle')));
    await tester.pumpAndSettle();
    expect(workspace.sheetExtent, AgentSheetExtent.expanded);
    expect(workspace.result, same(second));
    expect(find.descendant(of: oldReply, matching: oldCard), findsOneWidget);
    expect(find.descendant(of: newReply, matching: newCard), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('map view toggle retains task, results, selection and messages', (
    tester,
  ) async {
    final reply = _placeReply(_firstID, '测试地点甲', _firstAnswer, '甲');
    final workspace = await _workspace([reply]);
    addTearDown(workspace.dispose);
    workspace.selectEntity('place:$_firstID');
    final task = workspace.task;
    final messages = List<AgentMessage>.of(workspace.conversation);
    await _pumpSheet(tester, workspace);

    await tester.tap(find.byKey(const Key('agent-sheet-map-toggle')));
    await tester.pumpAndSettle();
    expect(workspace.sheetExtent, AgentSheetExtent.peek);
    expect(workspace.task, same(task));
    expect(workspace.result, same(reply));
    expect(workspace.presentedResult, same(reply));
    expect(workspace.selectedEntityId, 'place:$_firstID');
    expect(workspace.conversation, orderedEquals(messages));
    expect(workspace.replies.single.result, same(reply));

    await tester.tap(find.byKey(const Key('agent-sheet-map-toggle')));
    await tester.pumpAndSettle();
    expect(workspace.sheetExtent, AgentSheetExtent.expanded);
    expect(find.text(_firstAnswer), findsOneWidget);
    expect(
      find.byKey(const Key('agent-entity-place:$_firstID')),
      findsOneWidget,
    );
    expect(workspace.selectedEntityId, 'place:$_firstID');
    expect(tester.takeException(), isNull);
  });

  for (final width in [360.0, 390.0, 430.0]) {
    testWidgets(
      'handle spans and centers on the full $width logical pixel sheet',
      (tester) async {
        final workspace = await _workspace([
          _placeReply(_firstID, '测试地点甲', _firstAnswer, '甲'),
        ]);
        addTearDown(workspace.dispose);
        await _pumpSheet(tester, workspace, width: width, textScale: 2);

        final handle = find.byKey(const Key('agent-sheet-handle'));
        final bar = find.descendant(
          of: handle,
          matching: find.byWidgetPredicate(
            (widget) =>
                widget is Container &&
                widget.constraints?.maxWidth == 36 &&
                widget.constraints?.maxHeight == 4,
          ),
        );
        expect(bar, findsOneWidget);
        final handleRect = tester.getRect(handle);
        final barRect = tester.getRect(bar);
        expect(handleRect.width, closeTo(width, 0.1));
        expect(barRect.center.dx, closeTo(handleRect.center.dx, 0.1));
        expect(barRect.center.dy, closeTo(handleRect.center.dy, 0.1));
        expect(handleRect.height, greaterThanOrEqualTo(48));
        final toggleRect = tester.getRect(
          find.byKey(const Key('agent-sheet-map-toggle')),
        );
        expect(toggleRect.width, greaterThanOrEqualTo(48));
        expect(toggleRect.height, greaterThanOrEqualTo(48));
        expect(toggleRect.contains(barRect.center), isFalse);
        expect(find.byTooltip('查看地图'), findsOneWidget);
        expect(
          find.byKey(const Key('agent-sheet-map-toggle')).hitTestable(),
          findsOneWidget,
        );
        await tester.tapAt(barRect.center);
        await tester.pumpAndSettle();
        expect(workspace.sheetExtent, AgentSheetExtent.medium);
        expect(tester.takeException(), isNull);
      },
    );
  }

  testWidgets(
    'input focus expands into the remaining height without double IME subtraction',
    (tester) async {
      final reply = _placeReply(_firstID, '测试地点甲', _firstAnswer, '甲');
      final workspace = await _workspace([reply]);
      addTearDown(workspace.dispose);
      workspace
        ..selectEntity('place:$_firstID')
        ..setSheetExtent(AgentSheetExtent.peek);
      await _pumpSheet(
        tester,
        workspace,
        availableHeight: 340,
        keyboardInset: 310,
      );

      workspace.beginTyping();
      await tester.pumpAndSettle();
      expect(workspace.inputFocused, isTrue);
      expect(workspace.sheetExtent, AgentSheetExtent.expanded);
      final surface = find.descendant(
        of: find.byType(AgentResultsSheet),
        matching: find.byType(AnimatedContainer),
      );
      expect(tester.getSize(surface).height, closeTo(340, 0.1));
      final flow = find.byType(AgentConversation);
      expect(tester.getSize(flow).height, greaterThanOrEqualTo(200));
      expect(
        find.descendant(of: flow, matching: find.byType(ListView)),
        findsOneWidget,
      );
      await tester.ensureVisible(find.text(_firstAnswer));
      await tester.pumpAndSettle();
      expect(find.text(_firstAnswer).hitTestable(), findsOneWidget);
      await tester.ensureVisible(
        find.byKey(const Key('agent-entity-place:$_firstID')),
      );
      await tester.pumpAndSettle();
      expect(
        find.byKey(const Key('agent-entity-place:$_firstID')).hitTestable(),
        findsOneWidget,
      );
      expect(workspace.result, same(reply));
      expect(workspace.selectedEntityId, 'place:$_firstID');
      expect(tester.takeException(), isNull);
    },
  );
}

Future<AgentWorkspaceController> _workspace(List<AgentResult> replies) async {
  final workspace = AgentWorkspaceController(source: _ReplySource(replies));
  await workspace.submit('找一个适合聊天的地点', const [], const [], cityID: 'test-city');
  return workspace;
}

Future<void> _pumpSheet(
  WidgetTester tester,
  AgentWorkspaceController workspace, {
  double width = 390,
  double surfaceHeight = 844,
  double availableHeight = 760,
  double textScale = 1,
  double keyboardInset = 0,
  ValueChanged<AgentResultItem>? onOpen,
}) async {
  tester.view.physicalSize = Size(width, surfaceHeight);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(
    MaterialApp(
      home: MediaQuery(
        data: MediaQueryData(
          size: Size(width, surfaceHeight),
          viewInsets: EdgeInsets.only(bottom: keyboardInset),
          textScaler: TextScaler.linear(textScale),
          disableAnimations: true,
        ),
        child: Scaffold(
          resizeToAvoidBottomInset: false,
          body: Align(
            alignment: Alignment.bottomCenter,
            child: SizedBox(
              width: width,
              height: availableHeight,
              child: AgentResultsSheet(
                workspace: workspace,
                availableHeight: availableHeight,
                onOpenEntity: onOpen,
                onSuggestion: (_) {},
                onRetry: () {},
              ),
            ),
          ),
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

AgentResult _placeReply(
  String id,
  String title,
  String answer,
  String marker, {
  String? sourceReference,
}) {
  final ref = AgentResultRef(type: 'place', id: id);
  final item = AgentResultItem(
    entity: ref,
    title: title,
    summary: '仅用于本地界面回归的地点夹具',
    scope: 'AUTHORIZED_VIEW',
    detail: ref,
    anchor: const AgentResultAnchor(
      precision: 'point',
      latitude: 57.15,
      longitude: -2.1,
    ),
  );
  return AgentResult(
    entities: const [],
    activities: const [],
    places: [
      PublicPlace(
        id: id,
        name: title,
        categoryCode: 'test',
        summary: item.summary,
        source: PublicSource(
          label: '本地测试来源$marker',
          reference: sourceReference ?? 'https://example.org/ui-fixture/$id',
          maintainer: 'test-only',
          freshness: 'unverified',
          updatedAt: DateTime.utc(2026, 10, 8),
        ),
        location: const PublicPlaceLocation(
          coordinateSystem: 'wgs84',
          precision: 'point',
          latitude: 57.15,
          longitude: -2.1,
        ),
      ),
    ],
    note: '本地 UI 测试夹具，不代表真实检索',
    message: answer,
    resultSet: AgentResultSet(
      id: 'ui-test-$id',
      status: 'ready',
      entities: [ref],
      generatedAt: DateTime.utc(2026, 10, 8),
      schema: typedAgentResultSchema,
      items: [item],
    ),
  );
}

class _ReplySource extends AgentTaskSource {
  _ReplySource(this.replies);
  final List<AgentResult> replies;
  int _next = 0;

  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) async => replies[_next++];
}
