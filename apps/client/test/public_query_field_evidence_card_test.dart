import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/agent_entity_result_card.dart';
import 'package:birdtie_client/src/workspace/public_query_field_evidence.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'remote_public_field_evidence_test.dart' show publicEvidenceData;
import 'public_query_field_evidence_test.dart'
    show evidenceData, evidenceMetadata, evidenceClock, readEvidence, rebudget;

void main() {
  testWidgets('公开查询原结果面板能展开来源与时间', (t) async {
    final data = publicEvidenceData();
    final rawSet = data['resultSet'] as Map<String, dynamic>;
    final items = decodeAgentResultItems(rawSet);
    final metadata = readEvidence(data);
    final changes = ChangeNotifier();
    AgentResultItem? opened;
    final workspace = AgentWorkspaceController()
      ..task = AgentTask.fromJson(data['task'] as Map<String, dynamic>)
      ..result = AgentResult(
        entities: const [],
        activities: [
          for (final a in data['activities'] as List)
            PublicActivity.fromJson(a as Map<String, dynamic>),
        ],
        places: const [],
        note: data['note'] as String,
        resultSet: AgentResultSet(
          id: rawSet['id'] as String,
          status: 'ready',
          generatedAt: DateTime.parse(rawSet['generatedAt'] as String),
          schema: rawSet['schema'] as String,
          items: items,
          publicFieldEvidence: metadata,
          entities: [for (final item in items) item.entity],
        ),
      )
      ..state = AgentViewState.results
      ..setSheetExtent(AgentSheetExtent.expanded);
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentResultsSheet(
            workspace: workspace,
            onSuggestion: (_) {},
            onRetry: () {},
            availableHeight: 700,
            publicEvidenceCurrent: (item) => identical(item, items.single),
            publicEvidenceChanges: changes,
            onOpenEntity: (item) => opened = item,
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.text('同一公开活动'), findsOneWidget);
    expect(find.text('查看来源与时间'), findsOneWidget);
    final selection = items.single.entity.mapID;
    workspace.selectEntity(selection);
    await t.pump();
    await t.tap(find.text('查看来源与时间'));
    await t.pump();
    expect(find.textContaining('来源更新时间：'), findsOneWidget);
    expect(find.textContaining('来源到期时间：'), findsOneWidget);
    expect(find.textContaining('本次读取时间：'), findsOneWidget);
    expect(find.textContaining('本次说明读取期限：'), findsOneWidget);
    expect(find.textContaining('拍摄时间：未采集'), findsOneWidget);
    expect(find.textContaining('不代表所有声明'), findsOneWidget);
    expect(workspace.selectedEntityId, selection);
    expect(workspace.result!.projectionItems!.single, same(items.single));
    final open = find.widgetWithText(TextButton, '查看详情');
    await t.ensureVisible(open);
    await t.tap(open);
    expect(opened, same(items.single));
    expect(metadata.current, isTrue);
    await t.pumpWidget(const SizedBox());
    workspace.dispose();
    changes.dispose();
  });
  testWidgets('说明20秒到期且时钟回退不续期，原卡片操作保留', (t) async {
    final data = publicEvidenceData();
    var now = evidenceClock(data);
    final item = decodeAgentResultItems(data['resultSet']).single;
    final metadata = readEvidence(data, now: () => now),
        changes = ChangeNotifier();
    AgentResultItem? opened;
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SingleChildScrollView(
            child: AgentEntityResultCard(
              item: item,
              publicFieldEvidence: metadata,
              publicEvidenceCurrent: (_) => true,
              publicEvidenceChanges: changes,
              onOpen: (i) => opened = i,
            ),
          ),
        ),
      ),
    );
    await t.tap(find.text('查看来源与时间'));
    await t.pump();
    expect(find.textContaining('来源更新时间：'), findsOneWidget);
    now = now.subtract(const Duration(days: 1));
    await t.pump(const Duration(seconds: 21));
    expect(find.textContaining('来源说明已失效'), findsOneWidget);
    expect(find.textContaining('来源更新时间：'), findsNothing);
    await t.tap(find.text('查看详情'));
    expect(opened, same(item));
    expect(find.text(item.title), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    changes.dispose();
  });
  testWidgets('原来源通知身份ABA永久退役只说明，旧动作不被元数据改写', (t) async {
    final data = publicEvidenceData(),
        item = decodeAgentResultItems(data['resultSet']).single;
    var current = true;
    final metadata = readEvidence(data), changes = ChangeNotifier();
    var opens = 0;
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SingleChildScrollView(
            child: AgentEntityResultCard(
              item: item,
              publicFieldEvidence: metadata,
              publicEvidenceCurrent: (_) => current,
              publicEvidenceChanges: changes,
              onOpen: (_) => opens++,
            ),
          ),
        ),
      ),
    );
    await t.tap(find.text('查看来源与时间'));
    await t.pump();
    current = false;
    changes.notifyListeners();
    await t.pump();
    current = true;
    changes.notifyListeners();
    await t.pump();
    expect(find.textContaining('来源说明已失效'), findsOneWidget);
    expect(metadata.current, isFalse);
    await t.tap(find.text('查看详情'));
    expect(opens, 1);
    expect(find.textContaining('source version'), findsNothing);
    await t.pumpWidget(const SizedBox());
    changes.dispose();
  });
  testWidgets('同说明替换Item不得挪用旧来源，新的说明须来自新carrier', (t) async {
    final data = publicEvidenceData();
    final item = decodeAgentResultItems(data['resultSet']).single;
    final replacement = decodeAgentResultItems(data['resultSet']).single;
    final metadata = readEvidence(data), changes = ChangeNotifier();
    Widget view(AgentResultItem i, PublicQueryFieldEvidence m) => MaterialApp(
      home: Scaffold(
        body: SingleChildScrollView(
          child: AgentEntityResultCard(
            key: const Key('original-card'),
            item: i,
            publicFieldEvidence: m,
            publicEvidenceCurrent: (_) => true,
            publicEvidenceChanges: changes,
            onOpen: (_) {},
          ),
        ),
      ),
    );
    await t.pumpWidget(view(item, metadata));
    await t.tap(find.text('查看来源与时间'));
    await t.pump();
    await t.pumpWidget(view(replacement, metadata));
    expect(find.textContaining('来源说明已失效'), findsOneWidget);
    expect(metadata.current, isFalse);
    final fresh = readEvidence(data);
    await t.pumpWidget(view(replacement, fresh));
    await t.tap(find.text('查看来源与时间'));
    await t.pump();
    expect(find.textContaining('来源更新时间：'), findsOneWidget);
    expect(fresh.current, isTrue);
    await t.pumpWidget(const SizedBox());
    changes.dispose();
  });
  testWidgets('无metadata旧卡片不新增入口，私密合法结果不标PUBLIC', (t) async {
    final data = evidenceData(
      'SYNTHETIC_REGISTERED_PUBLIC_METADATA_invited_WIRE',
    );
    final item = decodeAgentResultItems(data['resultSet']).single,
        changes = ChangeNotifier();
    final metadata = readEvidence(data);
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SingleChildScrollView(
            child: AgentEntityResultCard(item: item, onOpen: (_) {}),
          ),
        ),
      ),
    );
    expect(find.text('查看来源与时间'), findsNothing);
    expect(find.text('查看详情'), findsOneWidget);
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SingleChildScrollView(
            child: AgentEntityResultCard(
              item: item,
              onOpen: (_) {},
              publicFieldEvidence: metadata,
              publicEvidenceCurrent: (_) => true,
              publicEvidenceChanges: changes,
            ),
          ),
        ),
      ),
    );
    await t.tap(find.text('查看来源与时间'));
    await t.pump();
    expect(find.textContaining('非公开来源不会自动标为公开'), findsOneWidget);
    expect(find.textContaining('来源更新时间：'), findsNothing);
    await t.pumpWidget(const SizedBox());
    changes.dispose();
  });
  testWidgets('320宽浅深主题大字号键盘下省略说明与48dp操作可读', (t) async {
    t.view.physicalSize = const Size(320, 800);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    for (final dark in [false, true]) {
      final data = publicEvidenceData(), p = evidenceMetadata(data);
      (p['fieldEvidenceSet'] as Map)['claims'] = [];
      (p['fieldEvidenceSet'] as Map)['scope'] = 'BUDGETED_CONTEXT';
      (p['budget'] as Map)['omitted']['OMITTED_BUDGET'] = 1;
      p['status'] = 'UNAVAILABLE';
      rebudget(data);
      final metadata = readEvidence(data),
          changes = ChangeNotifier(),
          item = decodeAgentResultItems(data['resultSet']).single;
      var opens = 0;
      await t.pumpWidget(
        MaterialApp(
          theme: dark ? ThemeData.dark() : ThemeData.light(),
          home: MediaQuery(
            data: const MediaQueryData(
              size: Size(320, 800),
              textScaler: TextScaler.linear(3),
              viewInsets: EdgeInsets.only(bottom: 220),
            ),
            child: Scaffold(
              body: SingleChildScrollView(
                child: AgentEntityResultCard(
                  item: item,
                  onOpen: (_) => opens++,
                  publicFieldEvidence: metadata,
                  publicEvidenceCurrent: (_) => true,
                  publicEvidenceChanges: changes,
                ),
              ),
            ),
          ),
        ),
      );
      final expand = find.widgetWithText(TextButton, '查看来源与时间');
      await t.ensureVisible(expand);
      await t.pumpAndSettle();
      expect(t.getSize(expand).height, greaterThanOrEqualTo(48));
      await t.tap(expand);
      await t.pump();
      expect(find.textContaining('省略了 1 个对象'), findsOneWidget);
      final open = find.widgetWithText(TextButton, '查看详情');
      await t.ensureVisible(open);
      await t.pumpAndSettle();
      expect(t.getSize(open).height, greaterThanOrEqualTo(48));
      await t.tap(open);
      expect(opens, 1);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      changes.dispose();
    }
  });
}
