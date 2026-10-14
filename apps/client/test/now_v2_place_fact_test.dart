import 'dart:convert';

import 'package:birdtie_client/src/workspace/agent_answer_sources.dart';
import 'package:birdtie_client/src/workspace/agent_reply_membership.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/place_answer_card.dart';
import 'package:birdtie_client/src/workspace/place_fact_presentation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

import 'reply_history_restore_test.dart' as history;

// Synthetic native DTOs exercise existing strict decoding and presentation.
// They are not live source grants, native map/IME evidence or real POI facts.
Map<String, dynamic> _wire(DateTime now) {
  final wire = history.historyWire(now);
  final messages = wire['task']['conversation'] as List;
  final rounds = wire['messageResults'] as List;
  for (final (turn, id) in [history.placeA, history.placeB].indexed) {
    final index = turn * 2 + 1;
    final message = messages[index] as Map<String, dynamic>;
    final fields = ['name', 'address', 'coordinates'];
    message['sourceEvidenceDigest'] = 'a' * 64;
    message['sources'] = [
      {
        'id': 'source-1',
        'title': 'Synthetic attribution',
        'url': 'https://example.org/doc?a=$turn',
      },
      {
        'id': 'source-2',
        'title': 'Other document same domain',
        'url': 'https://example.org/doc?b=$turn',
      },
    ];
    message['localReplyEvidence'] = {
      'schema': 'birdtie.local-place-reply.v1',
      'id': 'local-native:${history.taskID}:$index',
      'sourceEvidenceDigest': 'a' * 64,
      'validUntil': now.add(const Duration(minutes: 1)).toIso8601String(),
      'facts': [
        for (final (n, field) in fields.indexed)
          {
            'evidenceId': '11111111-1111-1111-1111-11111111111${n + 1}',
            'placeId': id,
            'field': field,
            'version': 1,
            'permissionId': '22222222-2222-2222-2222-222222222222',
          },
      ],
      'memoryUses': [],
      'reason': 'LOCAL_FIELDS_SUFFICIENT',
      'searchCalls': 0,
      'modelCalls': 0,
    };
    final digest = agentReplyTurnDigest(
      messages
          .take(index + 1)
          .map((m) => (role: m['role'] as String, text: m['text'] as String)),
    );
    final setID = 'reply:${history.taskID}:${digest.substring(0, 24)}';
    message['resultMembership']['turnDigest'] = digest;
    message['resultMembership']['resultSetId'] = setID;
    rounds[turn]['turnDigest'] = digest;
    final set = rounds[turn]['resultSet'] as Map<String, dynamic>;
    set['id'] = setID;
    final item = (set['items'] as List).single as Map<String, dynamic>;
    item['summary'] = '';
    item['shareRef'] = {'type': 'place', 'id': id};
    item['anchor']['placeId'] = id;
    item['actionsSourceVersion'] = 'c' * 64;
    item['actionsValidUntil'] = now
        .add(const Duration(minutes: 1))
        .toIso8601String();
    item['actions'] = [
      for (final kind in [
        'CONNECT',
        'MESSAGE',
        'SHARE',
        'JOIN',
        'SAVE',
        'NAVIGATE',
      ])
        {
          'kind': kind,
          'targetRef': {'type': 'place', 'id': id},
          'state': ['SHARE', 'SAVE', 'NAVIGATE'].contains(kind)
              ? 'AVAILABLE'
              : 'UNAVAILABLE',
          'label': 'Synthetic action',
          'reason': 'Synthetic contract only',
          'requiresConfirmation': true,
          'operation': {
            'CONNECT': 'REQUEST_FRIEND',
            'MESSAGE': 'OPEN_CHAT',
            'SHARE': 'CHOOSE_RECIPIENT',
            'JOIN': 'JOIN',
            'SAVE': 'SAVE',
            'NAVIGATE': 'NAVIGATE',
          }[kind],
        },
    ];
    rounds[turn]['places'] = [
      {
        'id': id,
        'name': item['title'],
        'categoryCode': 'restaurant',
        'summary': '',
        'addressLabel': 'Synthetic full address $turn AB11 6BB',
        'location': {
          'coordinateSystem': 'wgs84',
          'precision': 'point',
          'latitude': item['anchor']['latitude'],
          'longitude': item['anchor']['longitude'],
        },
        'source': {
          'label': 'Synthetic attribution',
          'reference': 'https://example.org/doc?a=$turn',
          'maintainer': 'Synthetic QA',
          'updatedAt': now.toIso8601String(),
          'freshness': 'current',
        },
      },
    ];
  }
  wire['places'] = rounds.last['places'];
  return jsonDecode(jsonEncode(wire)) as Map<String, dynamic>;
}

Future<AgentWorkspaceController> _workspace(
  Map<String, dynamic> wire,
  DateTime now, {
  DateTime Function()? clock,
  bool refreshAllowed = false,
}) async {
  final w = AgentWorkspaceController(
    source: history.historyRemote(() => wire, [], clock ?? () => now),
    replyRefreshAllowed: () => refreshAllowed,
  );
  await w.reopen(AgentTask.fromJson(wire['task']), [], []);
  return w;
}

Widget _sheet(
  AgentWorkspaceController w, {
  void Function(AgentResult, dynamic)? focus,
  ValueChanged<double>? measured,
}) => MaterialApp(
  home: Scaffold(
    body: SizedBox(
      height: 700,
      child: AgentResultsSheet(
        workspace: w,
        embedded: true,
        onContentHeightChanged: measured,
        onFocusReplyPlace: focus,
        onNavigateReplyPlace: (_, _) {},
        onShareReplyEntity: (_, _) {},
        onSaveReplyEntity: (_, _) {},
        onSuggestion: (_) {},
        onRetry: () {},
      ),
    ),
  ),
);

void _refreshWireLease(Map<String, dynamic> wire, DateTime now) {
  for (final round in wire['messageResults'] as List) {
    round['validUntil'] = now.add(const Duration(minutes: 1)).toIso8601String();
    round['resultSet']['generatedAt'] = now.toIso8601String();
  }
}

Future<void> _openPlaceSources(
  WidgetTester tester,
  AgentWorkspaceController workspace,
) async {
  await tester.pumpWidget(_sheet(workspace, measured: (_) {}));
  await tester.pump();
  final entry = find.byKey(const Key('place-answer-sources')).last;
  await tester.ensureVisible(entry);
  await tester.tap(entry);
  await tester.pumpAndSettle();
  expect(find.text('https://example.org/doc?a=1'), findsOneWidget);
}

Finder _modalText(String text) => find.descendant(
  of: find.byKey(const Key('place-provenance-modal')),
  matching: find.text(text),
);

void main() {
  testWidgets(
    'open provenance transfers same authoritative turn to fresh GET lease',
    (tester) async {
      var clock = DateTime.now().toUtc();
      final wire = _wire(clock);
      final w = await _workspace(
        wire,
        clock,
        clock: () => clock,
        refreshAllowed: true,
      );
      await _openPlaceSources(tester, w);
      final original = w.replies.last.result;
      final originalSources = w.messageSourcesForReply(original)!;
      clock = clock.add(const Duration(seconds: 20));
      _refreshWireLease(wire, clock);
      expect(await w.refreshReplyProjections(), isTrue);
      await tester.pump(const Duration(seconds: 20));
      expect(w.canUseReplyProjection(original), isFalse);
      expect(
        w.messageSourcesForReply(w.replies.last.result),
        isNot(same(originalSources)),
      );
      expect(_modalText('来源已失效，请重新查询。'), findsNothing);
      expect(find.text('https://example.org/doc?b=1'), findsOneWidget);
      clock = clock.add(const Duration(seconds: 45));
      await tester.pump(const Duration(seconds: 45));
      expect(originalSources.remaining, Duration.zero);
      expect(_modalText('来源已失效，请重新查询。'), findsNothing);
      expect(find.text('https://example.org/doc?a=1'), findsOneWidget);
      clock = clock.add(const Duration(seconds: 20));
      await tester.pump(const Duration(seconds: 20));
      expect(_modalText('来源已失效，请重新查询。'), findsOneWidget);
      await tester.pumpWidget(const SizedBox.shrink());
      w.dispose();
    },
  );

  for (final change in ['document', 'factVersion', 'address', 'revoke']) {
    testWidgets(
      'open provenance retires on $change and never restores via same-turn ABA',
      (tester) async {
        final now = DateTime.now().toUtc();
        final wire = _wire(now);
        final w = await _workspace(wire, now, refreshAllowed: true);
        await _openPlaceSources(tester, w);
        final saved = jsonDecode(jsonEncode(wire)) as Map<String, dynamic>;
        switch (change) {
          case 'document':
            wire['task']['conversation'][3]['sources'][0]['url'] =
                'https://example.org/changed';
          case 'factVersion':
            wire['task']['conversation'][3]['localReplyEvidence']['facts'][1]['version'] =
                2;
          case 'address':
            wire['messageResults'][1]['places'][0]['addressLabel'] =
                'Different address';
            wire['places'] = wire['messageResults'][1]['places'];
          case 'revoke':
            wire['messageResults'][1]['resultSet']['status'] = 'empty';
            wire['messageResults'][1]['resultSet']['items'] = [];
            wire['messageResults'][1]['resultSet']['entities'] = [];
            wire['messageResults'][1]['places'] = [];
            wire['messageResults'][1]['mapEffects']['pinEntityIds'] = [];
            wire['places'] = [];
            wire['resultSet'] = wire['messageResults'][1]['resultSet'];
            wire['mapEffects'] = wire['messageResults'][1]['mapEffects'];
        }
        expect(await w.refreshReplyProjections(), isTrue);
        await tester.pump();
        expect(_modalText('来源已失效，请重新查询。'), findsOneWidget);
        expect(find.text('https://example.org/doc?a=1'), findsNothing);
        wire
          ..clear()
          ..addAll(saved);
        expect(await w.refreshReplyProjections(), isTrue);
        await tester.pump();
        expect(_modalText('来源已失效，请重新查询。'), findsOneWidget);
        await tester.pumpWidget(const SizedBox.shrink());
        w.dispose();
      },
    );
  }

  testWidgets(
    'open provenance cannot bind another task epoch even when task ID is restored',
    (tester) async {
      final now = DateTime.now().toUtc();
      final wire = _wire(now);
      final w = await _workspace(wire, now, refreshAllowed: true);
      await _openPlaceSources(tester, w);
      final task = w.task!;
      w.newTask();
      await tester.pump();
      expect(_modalText('来源已失效，请重新查询。'), findsOneWidget);
      await w.reopen(task, [], []);
      await tester.pump();
      expect(_modalText('来源已失效，请重新查询。'), findsOneWidget);
      expect(find.text('https://example.org/doc?a=1'), findsNothing);
      await tester.pumpWidget(const SizedBox.shrink());
      w.dispose();
    },
  );

  testWidgets(
    'expired open provenance cannot revive from a late fresh GET or clock rollback',
    (tester) async {
      var clock = DateTime.now().toUtc();
      final wire = _wire(clock);
      final w = await _workspace(
        wire,
        clock,
        clock: () => clock,
        refreshAllowed: true,
      );
      await _openPlaceSources(tester, w);
      clock = clock.add(const Duration(minutes: 2));
      await tester.pump(const Duration(minutes: 2));
      expect(_modalText('来源已失效，请重新查询。'), findsOneWidget);
      _refreshWireLease(wire, clock);
      expect(await w.refreshReplyProjections(), isTrue);
      await tester.pump();
      expect(_modalText('来源已失效，请重新查询。'), findsOneWidget);
      clock = clock.subtract(const Duration(minutes: 2));
      await tester.pump();
      expect(_modalText('来源已失效，请重新查询。'), findsOneWidget);
      await tester.pumpWidget(const SizedBox.shrink());
      w.dispose();
    },
  );

  test(
    'native exact address fact closes one same-ID place; no prose regex used',
    () async {
      final now = DateTime.now().toUtc();
      final w = await _workspace(_wire(now), now);
      addTearDown(w.dispose);
      final p = PlaceFactPresentation.read(w, w.replies.last.result)!;
      expect(p.entityID, 'place:${history.placeB}');
      expect(p.formattedAddress, 'Synthetic full address 1 AB11 6BB');
      expect(p.sources.localFacts.map((f) => f.field).toSet(), {
        'name',
        'address',
        'coordinates',
      });
      expect(w.conversation.last.text, '第 2 轮真实站内结果。');
    },
  );

  for (final scenario in [
    'hours',
    'conflict',
    'missingPoint',
    'mismatch',
    'multiple',
    'explanation',
  ]) {
    test(
      '$scenario retains prose instead of claiming one certain address',
      () async {
        final now = DateTime.now().toUtc();
        final wire = _wire(now);
        final proof = wire['task']['conversation'][3]['localReplyEvidence'];
        final round = wire['messageResults'][1];
        switch (scenario) {
          case 'hours':
            proof['facts'][0]['field'] = 'opening_hours';
          case 'conflict':
            final duplicate = Map<String, dynamic>.from(proof['facts'][1]);
            duplicate['evidenceId'] = '11111111-1111-1111-1111-111111111119';
            duplicate['permissionId'] = '22222222-2222-2222-2222-222222222229';
            proof['facts'].add(duplicate);
          case 'missingPoint':
            round['resultSet']['items'][0].remove('anchor');
          case 'mismatch':
            round['places'][0]['location']['latitude'] = 0;
          case 'multiple':
            round['places'].add(Map<String, dynamic>.from(round['places'][0]));
          case 'explanation':
            round['resultSet']['items'][0]['summary'] =
                'Nonredundant explanation';
        }
        wire['places'] = round['places'];
        wire['resultSet'] = round['resultSet'];
        final w = await _workspace(wire, now);
        addTearDown(w.dispose);
        if (w.requestError == null) {
          expect(PlaceFactPresentation.read(w, w.replies.last.result), isNull);
        } else {
          // The existing decoder rejects incoherent anchor/entity DTOs even
          // before presentation. A rejected response is never a trusted card.
          expect(w.queryState, AgentQueryState.error);
          expect(w.result, isNull);
        }
        expect(w.conversation.last.text, '第 2 轮真实站内结果。');
      },
    );
  }

  test(
    'expired native reply never becomes a current card or revives on clock rollback',
    () async {
      var clock = DateTime.now().toUtc();
      final w = await _workspace(_wire(clock), clock, clock: () => clock);
      addTearDown(w.dispose);
      final original = w.replies.last.result;
      expect(PlaceFactPresentation.read(w, original), isNotNull);
      clock = clock.add(const Duration(minutes: 2));
      expect(PlaceFactPresentation.read(w, original), isNull);
      clock = clock.subtract(const Duration(minutes: 2));
      expect(PlaceFactPresentation.read(w, original), isNull);
      expect(w.conversation.last.text, '第 2 轮真实站内结果。');
    },
  );

  testWidgets(
    'one card per turn replaces assistant visual only; user/history semantics retained',
    (tester) async {
      final now = DateTime.now().toUtc();
      final w = await _workspace(_wire(now), now);
      String? selected;
      final heights = <double>[];
      await tester.pumpWidget(
        _sheet(
          w,
          focus: (_, item) => selected = item.entity.mapID,
          measured: heights.add,
        ),
      );
      await tester.pump();
      expect(find.byType(PlaceAnswerCard), findsNWidgets(2));
      expect(find.text('找地点 A'), findsOneWidget);
      expect(find.text('找地点 B'), findsOneWidget);
      expect(find.text('第 1 轮真实站内结果。'), findsNothing);
      expect(find.byType(AgentAnswerSourcesPanel), findsNothing);
      expect(w.conversation.last.text, '第 2 轮真实站内结果。');
      expect(heights.last, greaterThan(48));
      await tester.ensureVisible(
        find.byKey(const Key('place-answer-map-focus')).last,
      );
      await tester.tap(find.byKey(const Key('place-answer-map-focus')).last);
      expect(selected, 'place:${history.placeB}');
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
      w.dispose();
    },
  );

  testWidgets(
    'single provenance entry preserves different documents on same domain and blocks withdrawal',
    (tester) async {
      final now = DateTime.now().toUtc();
      final w = await _workspace(_wire(now), now);
      final p = PlaceFactPresentation.read(w, w.replies.last.result)!;
      var live = true;
      final changed = ValueNotifier(0), opened = <Uri>[];
      addTearDown(changed.dispose);
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: PlaceAnswerCard(
              presentation: p,
              current: () => live,
              changes: changed,
              openSource: (uri) async {
                opened.add(uri);
                return true;
              },
            ),
          ),
        ),
      );
      expect(find.byKey(const Key('place-answer-sources')), findsOneWidget);
      await tester.tap(find.byKey(const Key('place-answer-sources')));
      await tester.pumpAndSettle();
      expect(find.text('https://example.org/doc?a=1'), findsOneWidget);
      expect(find.text('https://example.org/doc?b=1'), findsOneWidget);
      await tester.tap(
        find.byKey(const ValueKey('place-answer-source-source-2')),
      );
      expect(opened.single.query, 'b=1');
      live = false;
      changed.value++;
      await tester.pump();
      expect(find.text('来源已失效，请重新查询。'), findsOneWidget);
      expect(find.text('https://example.org/doc?a=1'), findsNothing);
      live = true;
      changed.value++;
      await tester.pump();
      expect(find.text('来源已失效，请重新查询。'), findsOneWidget);
      await tester.pumpWidget(const SizedBox.shrink());
      w.dispose();
    },
  );

  testWidgets(
    'same reply refresh keeps card identity and uses fresh callback source',
    (tester) async {
      final now = DateTime.now().toUtc();
      final w = await _workspace(_wire(now), now, refreshAllowed: true);
      AgentResult? focused;
      await tester.pumpWidget(
        _sheet(w, focus: (source, _) => focused = source, measured: (_) {}),
      );
      await tester.pump();
      final original = w.replies.last.result;
      final originalKey = tester
          .widget<Column>(
            find.byKey(ValueKey('place-answer-place:${history.placeB}')),
          )
          .key;
      w.setSheetExtent(AgentSheetExtent.expanded);
      expect(await w.refreshReplyProjections(), isTrue);
      await tester.pump();
      expect(w.sheetExtent, AgentSheetExtent.expanded);
      expect(w.replies.last.result, isNot(same(original)));
      expect(
        tester
            .widget<Column>(
              find.byKey(ValueKey('place-answer-place:${history.placeB}')),
            )
            .key,
        originalKey,
      );
      await tester.ensureVisible(
        find.byKey(const Key('place-answer-map-focus')).last,
      );
      await tester.tap(find.byKey(const Key('place-answer-map-focus')).last);
      expect(focused, same(w.replies.last.result));
      expect(w.canUseReplyProjection(original), isFalse);
      expect(w.conversation.last.text, '第 2 轮真实站内结果。');
      await tester.pumpWidget(const SizedBox.shrink());
      w.dispose();
    },
  );

  testWidgets(
    'lease expiry restores original prose and removes current card without renewing sources',
    (tester) async {
      var now = DateTime.now().toUtc();
      final w = await _workspace(_wire(now), now, clock: () => now);
      await tester.pumpWidget(_sheet(w, measured: (_) {}));
      await tester.pump();
      expect(find.byType(PlaceAnswerCard), findsNWidgets(2));
      now = now.add(const Duration(minutes: 2));
      await tester.pump(const Duration(minutes: 2));
      expect(find.byType(PlaceAnswerCard), findsNothing);
      expect(find.text('第 2 轮真实站内结果。'), findsOneWidget);
      expect(find.text('找地点 B'), findsOneWidget);
      await tester.pumpWidget(const SizedBox.shrink());
      w.dispose();
    },
  );

  testWidgets(
    'more contains real callbacks and copy; 2x text keeps full address and 48dp source',
    (tester) async {
      final now = DateTime.now().toUtc();
      final w = await _workspace(_wire(now), now);
      final p = PlaceFactPresentation.read(w, w.replies.last.result)!;
      var shares = 0, saves = 0, navigations = 0;
      String? copied;
      tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        (call) async {
          if (call.method == 'Clipboard.setData') {
            copied = (call.arguments as Map)['text'];
          }
          return null;
        },
      );
      addTearDown(
        () => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
          SystemChannels.platform,
          null,
        ),
      );
      await tester.pumpWidget(
        MaterialApp(
          home: MediaQuery(
            data: const MediaQueryData(
              size: Size(320, 650),
              textScaler: TextScaler.linear(2),
            ),
            child: Scaffold(
              body: SingleChildScrollView(
                child: PlaceAnswerCard(
                  presentation: p,
                  current: () => true,
                  changes: w,
                  onShare: () => shares++,
                  onSave: () => saves++,
                  onNavigate: () => navigations++,
                ),
              ),
            ),
          ),
        ),
      );
      expect(find.text(p.formattedAddress), findsOneWidget);
      expect(
        tester.getSize(find.byKey(const Key('place-answer-sources'))).height,
        greaterThanOrEqualTo(48),
      );
      await tester.tap(find.byKey(const Key('place-answer-navigate')));
      expect(navigations, 1);
      for (final action in ['分享给好友', '收藏', '复制地址']) {
        await tester.tap(find.byKey(const Key('place-answer-more')));
        await tester.pumpAndSettle();
        await tester.tap(find.text(action));
        await tester.pumpAndSettle();
      }
      expect(shares, 1);
      expect(saves, 1);
      expect(copied, p.formattedAddress);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
      w.dispose();
    },
  );
}
