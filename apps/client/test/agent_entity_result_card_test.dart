import 'package:birdtie_client/src/workspace/agent_entity_result_card.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('320宽大字和键盘下可检查本人机会，打开及分享原活动，各48dp', (tester) async {
    tester.view.physicalSize = const Size(320, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    const activity = AgentResultRef(
      type: 'activity',
      id: '6c12dfe8-18ef-49eb-8b73-4db5bda920ec',
    );
    const item = AgentResultItem(
      entity: AgentResultRef(
        type: 'opportunity',
        id: 'own-intent:6c12dfe8-18ef-49eb-8b73-4db5bda920ec',
      ),
      title: '这是明确原始活动的完整中文标题，请先检查时间与受众',
      summary: '原活动事实，私密匹配内容不进入分享',
      scope: 'SELF_PRIVATE',
      detail: activity,
      share: activity,
    );
    AgentResultItem? opened, shared;
    await tester.pumpWidget(
      MaterialApp(
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
                onOpen: (i) => opened = i,
                onShare: (i) => shared = i,
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    final share = find.widgetWithText(TextButton, '分享活动');
    await tester.ensureVisible(share);
    await tester.pumpAndSettle();
    expect(tester.getSize(share).height, greaterThanOrEqualTo(48));
    await tester.tap(share);
    expect(shared, same(item));
    final open = find.widgetWithText(TextButton, '查看详情');
    await tester.ensureVisible(open);
    await tester.pumpAndSettle();
    expect(tester.getSize(open).height, greaterThanOrEqualTo(48));
    await tester.tap(open);
    expect(opened, same(item));
    expect(find.textContaining('仅你可见'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
  testWidgets('旧Group与未知源无假详情和分享按钮', (tester) async {
    const group = AgentResultItem(
      entity: AgentResultRef(type: 'group', id: 'legacy'),
      title: '旧社群',
      summary: '',
      scope: 'AUTHORIZED_VIEW',
    );
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentEntityResultCard(
            item: group,
            onOpen: (_) => fail('opened'),
            onShare: (_) => fail('shared'),
          ),
        ),
      ),
    );
    expect(find.text('查看详情'), findsNothing);
    expect(find.text('分享给好友'), findsNothing);
    expect(find.textContaining('来源类型未核实'), findsOneWidget);
  });
}
