import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/agent_entity_result_card.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

const _item = AgentResultItem(
  entity: AgentResultRef(type: 'place', id: 'synthetic-source-role'),
  title: '来源角色测试地点',
  summary: '',
  scope: 'AUTHORIZED_VIEW',
);

void main() {
  testWidgets('地点来源保留原标签和真实域名，维护者独立展示', (t) async {
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentEntityResultCard(
            item: _item,
            source: const PublicSource(
              label: 'OpenStreetMap 社区地图数据',
              reference: 'https://www.openstreetmap.org/node/123456',
              maintainer: '城市维护者',
              freshness: 'current',
              updatedAt: null,
            ),
            sourceCurrent: () => true,
          ),
        ),
      ),
    );
    await t.tap(find.text('来源'));
    await t.pumpAndSettle();
    expect(find.text('OpenStreetMap 社区地图数据'), findsOneWidget);
    expect(find.text('来源原名：城市维护者'), findsNothing);
    expect(find.text('来源原名：www.openstreetmap.org'), findsOneWidget);
    expect(find.text('资料维护：城市维护者'), findsOneWidget);
    expect(
      find.text('https://www.openstreetmap.org/node/123456'),
      findsOneWidget,
    );
    expect(find.textContaining('官方'), findsNothing);
    expect(t.takeException(), isNull);
  });

  testWidgets('维护者缺失不猜测维护身份', (t) async {
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentEntityResultCard(
            item: _item,
            source: const PublicSource(
              label: '',
              reference: 'https://example.org/test-only',
              maintainer: '',
              freshness: 'current',
              updatedAt: null,
            ),
            sourceCurrent: () => true,
          ),
        ),
      ),
    );
    await t.tap(find.text('来源'));
    await t.pumpAndSettle();
    expect(find.text('地点来源'), findsOneWidget);
    expect(find.text('来源原名：example.org'), findsOneWidget);
    expect(find.textContaining('资料维护：'), findsNothing);
  });

  testWidgets('当前来源撤销后维护标签和原文动作同时失效', (t) async {
    var current = true;
    final changes = ValueNotifier(0);
    addTearDown(changes.dispose);
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentEntityResultCard(
            item: _item,
            source: const PublicSource(
              label: '测试来源',
              reference: 'https://example.org/test-only',
              maintainer: 'TEST_ONLY 编辑者',
              freshness: 'current',
              updatedAt: null,
            ),
            sourceCurrent: () => current,
            sourceChanges: changes,
          ),
        ),
      ),
    );
    await t.tap(find.text('来源'));
    await t.pumpAndSettle();
    expect(find.text('资料维护：TEST_ONLY 编辑者'), findsOneWidget);
    current = false;
    changes.value++;
    await t.pump();
    expect(find.textContaining('资料维护：'), findsNothing);
    expect(find.textContaining('来源已失效'), findsOneWidget);
    final open = t.widget<TextButton>(find.widgetWithText(TextButton, '打开原文'));
    expect(open.onPressed, isNull);
    current = true;
    changes.value++;
    await t.pump();
    expect(find.textContaining('资料维护：'), findsNothing);
    expect(t.takeException(), isNull);
  });
}
