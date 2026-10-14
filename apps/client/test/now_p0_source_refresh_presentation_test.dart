import 'dart:convert';

import 'package:birdtie_client/src/workspace/agent_answer_sources.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'agent_answer_sources_test.dart' show sourcedWire;

// Synthetic parse/authorization fixtures only. No native GET, lease grant,
// provider call or real external URL launch is claimed by these widget tests.
Map<String, dynamic> _localMessage({
  String task = 'task-original',
  int index = 1,
}) => {
  'role': 'assistant',
  'text': '原始许可回答[source-1]。',
  'sourceEvidenceDigest': 'a' * 64,
  'sources': [
    {
      'id': 'source-1',
      'title': '原始标题',
      'url': 'https://example.org/original',
      'site': '原始站点',
      'publishedAt': '2026-10-08',
    },
  ],
  'localReplyEvidence': {
    'schema': 'birdtie.local-place-reply.v1',
    'id': 'local-native:$task:$index',
    'sourceEvidenceDigest': 'a' * 64,
    'validUntil': '2026-10-09T01:00:00+08:00',
    'facts': [
      {
        'evidenceId': '11111111-1111-1111-1111-111111111111',
        'placeId': '22222222-2222-2222-2222-222222222222',
        'field': 'address',
        'version': 1,
        'permissionId': '33333333-3333-3333-3333-333333333333',
      },
    ],
    'memoryUses': [],
    'reason': 'LOCAL_FIELDS_SUFFICIENT',
    'searchCalls': 0,
    'modelCalls': 0,
  },
};

Map<String, dynamic> _copy(Map<String, dynamic> raw) =>
    jsonDecode(jsonEncode(raw)) as Map<String, dynamic>;

AgentMessageSources _read(
  Map<String, dynamic> raw, {
  String task = 'task-original',
  int index = 1,
  bool Function()? current,
  Duration Function()? remaining,
}) => AgentMessageSources.read(
  raw,
  expectedTaskID: task,
  expectedMessageIndex: index,
  current: current ?? () => true,
  localRoundCurrent: current ?? () => true,
  localRoundRemaining: remaining ?? () => const Duration(seconds: 30),
)!;

Widget _persisted(
  ValueNotifier<AgentMessageSources> active,
  List<Uri> opened,
) => MaterialApp(
  home: Scaffold(
    body: SingleChildScrollView(
      child: ValueListenableBuilder<AgentMessageSources>(
        valueListenable: active,
        builder: (context, refs, _) => AgentAnswerSourcesPanel.persisted(
          sources: refs,
          changes: active,
          // Matches the real workspace's object ownership handoff on fresh GET.
          replyCurrent: () => identical(active.value, refs),
          openSource: (uri) async {
            opened.add(uri);
            return true;
          },
        ),
      ),
    ),
  ),
);

Future<void> _expand(WidgetTester tester) async {
  await tester.tap(find.byKey(const Key('agent-answer-sources-toggle')));
  await tester.pump();
  expect(find.text('收起来源'), findsOneWidget);
}

void main() {
  testWidgets('26s同轮fresh对象只保留expanded，点击走新对象且不延长旧lease', (tester) async {
    var seconds = 0;
    final raw = _localMessage();
    final old = _read(
      raw,
      current: () => seconds < 30,
      remaining: () => Duration(seconds: 30 - seconds),
    );
    final active = ValueNotifier(old), opened = <Uri>[];
    await tester.pumpWidget(_persisted(active, opened));
    await _expand(tester);
    seconds = 26;
    await tester.pump(const Duration(seconds: 26));
    final fresh = _read(
      _copy(raw),
      current: () => seconds < 56,
      remaining: () => Duration(seconds: 56 - seconds),
    );
    active.value = fresh;
    await tester.pump();
    expect(active.value, same(fresh));
    expect(find.text('收起来源'), findsOneWidget);
    expect(find.text('原始标题'), findsOneWidget);
    seconds = 31;
    await tester.pump(const Duration(seconds: 5));
    expect(old.current, isFalse);
    expect(fresh.current, isTrue);
    expect(find.text('收起来源'), findsOneWidget);
    final source = find.byKey(const ValueKey('agent-answer-source-source-1'));
    await tester.tap(source);
    await tester.pump();
    expect(opened, [Uri.parse('https://example.org/original')]);
    await tester.tap(find.byKey(const Key('agent-answer-sources-toggle')));
    await tester.pump();
    expect(find.text('全部来源 1'), findsOneWidget);
    await _expand(tester);
    seconds = 57;
    await tester.pump(const Duration(seconds: 26));
    expect(find.text('来源已失效，请重新查询。'), findsOneWidget);
    expect(find.text('收起来源'), findsNothing);
    expect(
      tester
          .widget<TextButton>(
            find.byKey(const Key('agent-answer-sources-toggle')),
          )
          .onPressed,
      isNull,
    );
    expect(
      find.byKey(const ValueKey('agent-answer-source-source-1')),
      findsNothing,
    );
    expect(old.current, isFalse);
    await tester.pumpWidget(const SizedBox());
    active.dispose();
  });

  for (final change in [
    'url',
    'title',
    'site',
    'publishedAt',
    'body',
    'evidence',
    'task',
    'reply',
  ]) {
    testWidgets('$change变化收起旧来源，不保留展开授权状态', (tester) async {
      final raw = _localMessage(),
          active = ValueNotifier(_read(_localMessage()));
      final opened = <Uri>[];
      await tester.pumpWidget(_persisted(active, opened));
      await _expand(tester);
      final source = (raw['sources'] as List).single as Map;
      switch (change) {
        case 'url':
          source['url'] = 'https://example.org/new';
        case 'title':
          source['title'] = '新标题';
        case 'site':
          source['site'] = '新站点';
        case 'publishedAt':
          source['publishedAt'] = '2026-10-09';
        case 'body':
          raw['text'] = '不同正文[source-1]。';
        case 'evidence':
          raw['sourceEvidenceDigest'] = 'b' * 64;
          (raw['localReplyEvidence'] as Map)['sourceEvidenceDigest'] = 'b' * 64;
        case 'task':
          (raw['localReplyEvidence'] as Map)['id'] = 'local-native:task-new:1';
        case 'reply':
          (raw['localReplyEvidence'] as Map)['id'] =
              'local-native:task-original:3';
      }
      active.value = _read(
        raw,
        task: change == 'task' ? 'task-new' : 'task-original',
        index: change == 'reply' ? 3 : 1,
      );
      await tester.pump();
      expect(find.text('收起来源'), findsNothing);
      expect(find.text('全部来源 1'), findsOneWidget);
      expect(
        find.byKey(const ValueKey('agent-answer-source-source-1')),
        findsNothing,
      );
      expect(opened, isEmpty);
      // Only the fresh authorized object can be expanded by another real tap.
      await _expand(tester);
      await tester.tap(
        find.byKey(const ValueKey('agent-answer-source-source-1')),
      );
      await tester.pump();
      expect(opened, [Uri.parse(source['url'] as String)]);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      active.dispose();
    });
  }

  testWidgets('撤权与ABA不得保留或复活旧展开链接', (tester) async {
    var allowed = true;
    final old = _read(_localMessage(), current: () => allowed);
    final active = ValueNotifier(old), opened = <Uri>[];
    await tester.pumpWidget(_persisted(active, opened));
    await _expand(tester);
    allowed = false;
    active.notifyListeners();
    // Retire the old authorization immediately, even before a painted frame.
    allowed = true;
    active.notifyListeners();
    await tester.pump();
    expect(old.current, isFalse);
    expect(find.text('收起来源'), findsNothing);
    expect(
      tester
          .widget<TextButton>(
            find.byKey(const Key('agent-answer-sources-toggle')),
          )
          .onPressed,
      isNull,
    );
    active.value = _read(_localMessage(), current: () => true);
    await tester.pump();
    expect(find.text('收起来源'), findsNothing);
    expect(
      find.byKey(const ValueKey('agent-answer-source-source-1')),
      findsNothing,
    );
    expect(opened, isEmpty);
    await tester.pumpWidget(const SizedBox());
    active.dispose();
  });

  testWidgets('fresh对象权限无效时收起且新控件disabled', (tester) async {
    final active = ValueNotifier(_read(_localMessage())), opened = <Uri>[];
    await tester.pumpWidget(_persisted(active, opened));
    await _expand(tester);
    active.value = _read(_localMessage(), current: () => false);
    await tester.pump();
    expect(find.text('来源已失效，请重新查询。'), findsOneWidget);
    expect(find.text('收起来源'), findsNothing);
    expect(
      tester
          .widget<TextButton>(
            find.byKey(const Key('agent-answer-sources-toggle')),
          )
          .onPressed,
      isNull,
    );
    expect(opened, isEmpty);
    await tester.pumpWidget(const SizedBox());
    active.dispose();
  });

  for (final changed in [false, true]) {
    testWidgets('live fresh保持仅相同原正文/run ${!changed}', (tester) async {
      var now = DateTime.utc(2026, 10, 10);
      final active = ValueNotifier(
        AgentAnswerSources.read(
          sourcedWire(now),
          current: () => true,
          now: () => now,
        )!,
      );
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: SingleChildScrollView(
              child: ValueListenableBuilder<AgentAnswerSources>(
                valueListenable: active,
                builder: (context, answer, _) => AgentAnswerSourcesPanel(
                  answer: answer,
                  changes: active,
                  replyCurrent: () => identical(active.value, answer),
                  openSource: (_) async => true,
                ),
              ),
            ),
          ),
        ),
      );
      await _expand(tester);
      now = now.add(const Duration(seconds: 26));
      final wire = sourcedWire(now);
      if (changed) wire['message'] = '改变正文但没有修改原Task snapshot digest';
      active.value = AgentAnswerSources.read(
        wire,
        current: () => true,
        now: () => now,
      )!;
      await tester.pump();
      expect(find.text('收起来源'), changed ? findsNothing : findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      active.dispose();
    });
  }
}
