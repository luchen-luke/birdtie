import 'package:birdtie_client/src/workspace/person_contexts_page.dart';
import 'package:birdtie_client/src/workspace/human_causal_pending_store.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'human_causal_control_test.dart';
import 'privacy_control_page_test.dart'
    show humanApp, humanFonts, humanViewport, humanCapture;

void main() {
  setUpAll(humanFonts);
  testWidgets('本人以中文管理所在城市和线上情境', (t) async {
    humanViewport(t);
    final f = HumanControlFixture(), auth = await humanAuth(f);
    await t.pumpWidget(
      humanApp(
        PersonContextsPage(
          auth: auth,
          apiBaseUrl: humanBase,
          client: f.client,
          pendingStore: MemoryHumanCausalPendingStore(),
        ),
      ),
    );
    await humanSettle(t);
    expect(find.text('我的城市与兴趣'), findsOneWidget);
    expect(find.textContaining('仅自己可见'), findsOneWidget);
    await humanTap(t, find.byKey(const Key('context-check')));
    expect(find.text('城市：阿伯丁'), findsOneWidget);
    expect(find.textContaining('替换本人原所在城市声明'), findsOneWidget);
    expect(f.writes, isEmpty);
    await humanCapture(t, 'context-confirm-normal390');
    await humanTap(t, find.byKey(const Key('human-control-confirm')));
    expect(f.contexts.single['sourceKey'], 'aberdeen-gb');
    expect(f.contexts.single['relation'], 'current');
    expect(f.version, 2);
    await humanTap(t, find.byTooltip('移除记录'));
    expect(find.textContaining('不是身份'), findsOneWidget);
    await humanTap(t, find.byKey(const Key('human-control-confirm')));
    expect(f.contexts, isEmpty);
    await humanTap(t, find.byKey(const Key('context-type')));
    await humanTap(t, find.text('线上兴趣').last);
    await t.enterText(find.byKey(const Key('context-source')), '线上羽毛球');
    await t.pump();
    await humanTap(t, find.byKey(const Key('context-check')));
    await humanTap(t, find.byKey(const Key('human-control-confirm')));
    expect(f.contexts.single['contextType'], 'ONLINE');
    expect(f.contexts.single['relation'], 'interest');
    expect(find.text('线上羽毛球'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    f.client.close();
  });
}
