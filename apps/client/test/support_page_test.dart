import 'dart:convert';
import 'package:birdtie_client/src/workspace/support_page.dart';
import 'package:birdtie_client/src/workspace/human_causal_pending_store.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'human_causal_control_test.dart';
import 'privacy_control_page_test.dart'
    show humanApp, humanFonts, humanViewport, humanCapture;
import 'agent_seed_sheet_test.dart' show SeedTestAuth;

void main() {
  setUpAll(humanFonts);
  for (final (kind, label, name, details) in [
    (
      'activity',
      '举报此活动',
      'activity report submits and shows its real receipt',
      '活动现场存在安全问题，需要核查。',
    ),
    ('account', '举报此账号', '账号举报保留账号目标与中文说明', '此账号持续发送骚扰消息，请协助核查。'),
    ('message', '举报此消息', 'message 举报提交对应对象并返回收据', '该内容持续造成骚扰，请协助核查。'),
    ('community', '举报此社群', 'community 举报提交对应对象并返回收据', '该内容持续造成骚扰，请协助核查。'),
    ('business', '举报此商家', 'business 举报提交对应对象并返回收据', '该内容持续造成骚扰，请协助核查。'),
  ])
    testWidgets(name, (t) async {
      humanViewport(t);
      final f = HumanControlFixture();
      await t.pumpWidget(
        humanApp(
          SupportPage(
            authorizationHeader: () => 'Bearer original',
            ownerID: () => humanOwner,
            targetType: kind,
            targetID: humanTarget,
            apiBaseUrl: humanBase,
            client: f.client,
            pendingStore: MemoryHumanCausalPendingStore(),
          ),
        ),
      );
      await humanSettle(t);
      expect(find.text(label), findsOneWidget);
      await t.enterText(find.byKey(const Key('support-details')), details);
      await t.pump();
      await humanTap(t, find.byKey(const Key('support-submit')));
      expect(find.text(details), findsWidgets);
      expect(f.writes, isEmpty);
      if (kind == 'activity') await humanCapture(t, 'report-confirm-normal390');
      await humanTap(t, find.byKey(const Key('human-control-confirm')));
      final body = jsonDecode(f.writes.single.body);
      expect(body['targetType'], kind);
      expect(body['targetId'], humanTarget);
      expect(body['details'], details);
      expect(find.textContaining('原提交已确证，编号：$humanResult'), findsOneWidget);
      expect(find.text('待处理'), findsOneWidget);
      expect(
        t
            .widget<TextField>(find.byKey(const Key('support-details')))
            .controller!
            .text,
        isEmpty,
      );
      await t.pumpWidget(const SizedBox());
      f.client.close();
    });
  testWidgets(
    'Support same owner renewal keeps own body, actor ABA hides it and old approval',
    (t) async {
      final auth = SeedTestAuth()..owner = humanOwner,
          f = HumanControlFixture();
      await t.pumpWidget(
        humanApp(
          SupportPage(
            authorizationHeader: () => auth.authorizationHeader,
            ownerID: () => auth.accountID,
            identityChanges: auth,
            client: f.client,
            apiBaseUrl: humanBase,
            pendingStore: MemoryHumanCausalPendingStore(),
          ),
        ),
      );
      await humanSettle(t);
      await t.enterText(
        find.byKey(const Key('support-details')),
        '本人尚未发送的问题正文必须保留。',
      );
      auth.changeIdentity('Bearer renewed', nextOwner: humanOwner);
      await humanSettle(t);
      expect(
        t
            .widget<TextField>(find.byKey(const Key('support-details')))
            .controller!
            .text,
        '本人尚未发送的问题正文必须保留。',
      );
      await humanTap(t, find.byKey(const Key('human-control-read')));
      await humanTap(t, find.byKey(const Key('support-submit')));
      final old = t
          .widget<FilledButton>(find.byKey(const Key('human-control-confirm')))
          .onPressed!;
      auth.changeIdentity('Bearer B', nextOwner: humanTarget);
      auth.changeIdentity('Bearer renewed', nextOwner: humanOwner);
      await t.pumpAndSettle();
      expect(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.text('本人尚未发送的问题正文必须保留。'),
        ),
        findsNothing,
      );
      old();
      await humanSettle(t);
      expect(f.writes, isEmpty);
      await humanTap(t, find.widgetWithText(TextButton, '取消'));
      expect(find.byKey(const Key('support-details')), findsNothing);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      f.client.close();
    },
  );
}
