import 'package:birdtie_client/src/workspace/organization_faq_page.dart';
import 'package:birdtie_client/src/workspace/human_causal_pending_store.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'human_causal_control_test.dart';
import 'privacy_control_page_test.dart'
    show humanApp, humanFonts, humanViewport, humanCapture;

void main() {
  setUpAll(humanFonts);
  testWidgets('admin can save a real FAQ draft from the console page', (
    t,
  ) async {
    humanViewport(t);
    final f = HumanControlFixture();
    await t.pumpWidget(
      humanApp(
        OrganizationFAQPage(
          organizationID: humanOrg,
          authorizationHeader: () => 'Bearer original',
          ownerID: () => humanOwner,
          client: f.client,
          apiBaseUrl: humanBase,
          pendingStore: MemoryHumanCausalPendingStore(),
        ),
      ),
    );
    await humanSettle(t);
    expect(find.textContaining('还没有常见问答'), findsOneWidget);
    await humanTap(t, find.text('新增问答'));
    await t.enterText(find.byKey(const Key('faq-question')), '如何报名？');
    await t.enterText(find.byKey(const Key('faq-answer')), '请在活动详情报名。');
    await t.pump();
    await humanTap(t, find.byKey(const Key('faq-check')));
    expect(find.text('保存草稿问答'), findsOneWidget);
    expect(f.writes, isEmpty);
    await humanCapture(t, 'faq-confirm-normal390');
    await humanTap(t, find.byKey(const Key('human-control-confirm')));
    expect(f.faq.single['published'], false);
    expect(f.writes.single.body, contains('"expectedVersion":0'));
    expect(find.text('如何报名？'), findsOneWidget);
    expect(find.text('请在活动详情报名。'), findsOneWidget);
    expect(find.text('草稿 · 仅管理员可见'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    f.client.close();
  });
  testWidgets(
    'FAQ current403 hides existing private answers but original op actions remain',
    (t) async {
      final f = HumanControlFixture()..stateDenied = true;
      await t.pumpWidget(
        humanApp(
          OrganizationFAQPage(
            organizationID: humanOrg,
            authorizationHeader: () => 'Bearer original',
            ownerID: () => humanOwner,
            client: f.client,
            apiBaseUrl: humanBase,
            pendingStore: MemoryHumanCausalPendingStore(),
          ),
        ),
      );
      await humanSettle(t);
      expect(find.text('新增问答'), findsNothing);
      expect(find.textContaining('不能开始新的修改'), findsOneWidget);
      expect(f.calls.where((r) => r.method != 'GET'), isEmpty);
      await t.pumpWidget(const SizedBox());
      f.client.close();
    },
  );
  testWidgets(
    'cancel FAQ concrete preview preserves own editor and never PUT',
    (t) async {
      final f = HumanControlFixture();
      await t.pumpWidget(
        humanApp(
          OrganizationFAQPage(
            organizationID: humanOrg,
            authorizationHeader: () => 'Bearer original',
            ownerID: () => humanOwner,
            client: f.client,
            apiBaseUrl: humanBase,
            pendingStore: MemoryHumanCausalPendingStore(),
          ),
        ),
      );
      await humanSettle(t);
      await humanTap(t, find.text('新增问答'));
      await t.enterText(find.byKey(const Key('faq-question')), '取消的问题');
      await t.enterText(find.byKey(const Key('faq-answer')), '取消后保留自己的回答草稿。');
      await t.pump();
      await humanTap(t, find.byKey(const Key('faq-check')));
      await humanTap(
        t,
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.widgetWithText(TextButton, '取消'),
        ),
      );
      expect(f.writes, isEmpty);
      expect(
        t
            .widget<TextField>(find.byKey(const Key('faq-answer')))
            .controller!
            .text,
        '取消后保留自己的回答草稿。',
      );
      await t.pumpWidget(const SizedBox());
      f.client.close();
    },
  );
}
