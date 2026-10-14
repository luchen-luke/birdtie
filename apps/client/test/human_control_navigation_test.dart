import 'package:birdtie_client/src/workspace/human_causal_pending_store.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:birdtie_client/src/workspace/support_page.dart';
import 'package:birdtie_client/src/workspace/activity_conversation_page.dart';
import 'package:birdtie_client/src/workspace/community_conversation_page.dart';
import 'package:birdtie_client/src/workspace/public_organization_page.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'human_causal_control_test.dart';
import 'privacy_control_page_test.dart'
    show humanApp, humanFonts, humanViewport;
import 'agent_seed_sheet_test.dart' show SeedTestAuth;

void main() {
  setUpAll(humanFonts);
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  testWidgets(
    'real human conversation renewal retires chat source but keeps separate owned report draft',
    (t) async {
      humanViewport(t);
      final auth = SeedTestAuth()..owner = humanOwner,
          f = HumanControlFixture();
      f.intercept = (r) async {
        if (r.url.path == '/v1/me/conversations/$humanOp/messages')
          return humanWire([
            {
              'id': humanResult,
              'senderAccountId': humanTarget,
              'body': '合成原消息',
              'createdAt': '2026-10-14T00:00:00Z',
            },
          ]);
        if (r.url.path.endsWith('/read')) return humanWire({});
        return null;
      };
      await t.pumpWidget(
        humanApp(
          HumanConversationRoute(
            auth: auth,
            conversation: const HumanConversation(
              id: humanOp,
              otherAccountId: humanTarget,
              otherName: '合成联系人',
            ),
            client: f.client,
            apiBaseUrl: humanBase,
          ),
        ),
      );
      await humanSettle(t);
      await humanTap(t, find.text('举报此消息'));
      await t.enterText(
        find.byKey(const Key('support-details')),
        '本人另行填写但尚未提交的问题正文。',
      );
      auth.changeIdentity('Bearer renewed', nextOwner: humanOwner);
      await humanSettle(t);
      expect(
        t
            .widget<TextField>(find.byKey(const Key('support-details')))
            .controller!
            .text,
        '本人另行填写但尚未提交的问题正文。',
      );
      expect(f.writes, isEmpty);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      f.client.close();
    },
  );
  for (final kind in [
    'activity',
    'community',
    'organization',
    'settings',
    'contacts',
  ])
    testWidgets(
      'B5 actual $kind parent target or transport ABA retires old report callback',
      (t) async {
        humanViewport(t);
        final auth = SeedTestAuth()..owner = humanOwner,
            f = HumanControlFixture(),
            target = ValueNotifier(humanTarget);
        final city = PublicCityController(),
            moments = PrivateMomentController(
              authorizationHeader: () => auth.authorizationHeader,
            );
        f.intercept = (r) async {
          if (r.url.path.startsWith('/v1/organizations/'))
            return humanWire({
              'id': r.url.path.split('/').last,
              'name': '合成组织',
              'verificationStatus': 'unverified',
              'agentAvailable': false,
              'officialLinks': [],
              'upcomingActivities': [],
            });
          if (r.url.path.endsWith('/ties/page'))
            return humanWire({
              'items': [
                {
                  'id': humanOp,
                  'otherAccountId': humanTarget,
                  'otherName': '合成好友',
                  'createdAt': '2026-10-14T00:00:00Z',
                },
              ],
              'nextCursor': null,
            });
          if (r.url.path.endsWith('/connection-requests/page'))
            return humanWire({'items': [], 'nextCursor': null});
          if (r.url.path.endsWith('/conversations/page'))
            return humanWire({
              'items': [],
              'nextCursor': null,
              'unreadCount': 0,
            });
          if (r.url.path.contains('/conversation')) {
            if (r.url.path.endsWith('/messages'))
              return humanWire({
                'messages': [
                  {
                    'id': humanResult,
                    'senderAccountId': humanTarget,
                    'senderName': '合成参与者',
                    'body': '原消息',
                    'createdAt': '2026-10-14T00:00:00Z',
                    'removed': false,
                  },
                ],
                'hasMore': false,
              });
            return humanWire({
              'id': humanOp,
              'viewerAccountId': humanOwner,
              'joined': true,
              'canJoin': false,
              'canSend': true,
              'moderator': false,
            });
          }
          return null;
        };
        Widget page(String id) {
          final base = id == humanTarget
              ? humanBase
              : 'https://other-human.fixture';
          return switch (kind) {
            'activity' => ActivityConversationPage(
              key: const Key('same-parent'),
              activityID: id,
              activityTitle: '合成活动',
              authorizationHeader: () => auth.authorizationHeader,
              ownerID: () => auth.accountID,
              identityChanges: auth,
              client: f.client,
              apiBaseUrl: humanBase,
            ),
            'community' => CommunityConversationPage(
              key: const Key('same-parent'),
              communityID: id,
              communityTitle: '合成社群',
              authorizationHeader: () => auth.authorizationHeader,
              ownerID: () => auth.accountID,
              identityChanges: auth,
              client: f.client,
              apiBaseUrl: humanBase,
            ),
            'organization' => PublicOrganizationPage(
              key: const Key('same-parent'),
              organizationID: id,
              authorizationHeader: () => auth.authorizationHeader,
              ownerID: () => auth.accountID,
              identityChanges: auth,
              onOpenActivity: (_) {},
              client: f.client,
              apiBaseUrl: humanBase,
            ),
            'settings' => Scaffold(
              body: SettingsPage(
                key: const Key('same-parent'),
                auth: auth,
                city: city,
                moments: moments,
                client: f.client,
                apiBaseUrl: base,
                humanPendingStore: MemoryHumanCausalPendingStore(),
              ),
            ),
            _ => Scaffold(
              body: SingleChildScrollView(
                child: SocialInboxSection(
                  key: const Key('same-parent'),
                  auth: auth,
                  client: f.client,
                  apiBaseUrl: base,
                ),
              ),
            ),
          };
        }

        await t.pumpWidget(
          humanApp(
            ValueListenableBuilder(
              valueListenable: target,
              builder: (_, String id, __) => page(id),
            ),
          ),
        );
        await humanSettle(t);
        if (kind == 'activity' || kind == 'community') {
          await humanTap(t, find.byTooltip('消息操作'));
          await humanTap(t, find.text('举报消息'));
        } else if (kind == 'organization') {
          await t.scrollUntilVisible(find.text('举报组织信息'), 200);
          await humanTap(t, find.text('举报组织信息'));
        } else if (kind == 'settings') {
          await t.scrollUntilVisible(find.text('帮助与举报'), 200);
          await humanTap(t, find.text('帮助与举报'));
        } else {
          await humanTap(t, find.byTooltip('好友操作'));
          await humanTap(t, find.text('举报此账号'));
        }
        await t.enterText(
          find.byKey(const Key('support-details')),
          '本人的旧来源确认正文必须立即退休。',
        );
        await t.pump();
        await humanTap(t, find.byKey(const Key('support-submit')));
        final old = t
            .widget<FilledButton>(
              find.byKey(const Key('human-control-confirm')),
            )
            .onPressed!;
        target.value = humanOtherOp;
        await t.pump();
        target.value = humanTarget;
        await t.pump();
        await humanSettle(t);
        expect(
          find.descendant(
            of: find.byType(AlertDialog),
            matching: find.text('本人的旧来源确认正文必须立即退休。'),
          ),
          findsNothing,
        );
        old();
        await humanSettle(t);
        expect(f.writes, isEmpty);
        expect(find.byKey(const Key('support-details')), findsNothing);
        expect(t.takeException(), isNull);
        await humanTap(t, find.widgetWithText(TextButton, '取消'));
        await t.pumpWidget(const SizedBox());
        auth.dispose();
        target.dispose();
        city.dispose();
        moments.dispose();
        f.client.close();
      },
    );
  testWidgets(
    'Settings original Help entry uses current personal transport and typed report only',
    (t) async {
      humanViewport(t);
      final auth = SeedTestAuth()..owner = humanOwner,
          f = HumanControlFixture(),
          city = PublicCityController(),
          moments = PrivateMomentController(
            authorizationHeader: () => auth.authorizationHeader,
          );
      await t.pumpWidget(
        humanApp(
          Scaffold(
            body: SettingsPage(
              auth: auth,
              city: city,
              moments: moments,
              client: f.client,
              apiBaseUrl: humanBase,
              humanPendingStore: MemoryHumanCausalPendingStore(),
            ),
          ),
        ),
      );
      await humanSettle(t);
      await t.scrollUntilVisible(find.text('帮助与举报'), 200);
      await humanTap(t, find.text('帮助与举报'));
      final page = t.widget<SupportPage>(find.byType(SupportPage));
      expect(page.ownerID!(), humanOwner);
      expect(page.client, same(f.client));
      expect(page.apiBaseUrl, humanBase);
      expect(page.targetType, 'general');
      await t.enterText(
        find.byKey(const Key('support-details')),
        '本人的一般功能故障说明，等待处理。',
      );
      await t.pump();
      await humanTap(t, find.byKey(const Key('support-submit')));
      await humanTap(t, find.byKey(const Key('human-control-confirm')));
      expect(
        f.writes.single.url.path,
        startsWith('/v1/me/report-control/operations/'),
      );
      expect(
        f.calls.where(
          (r) => r.url.path == '/v1/me/reports' && r.method != 'GET',
        ),
        isEmpty,
      );
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      city.dispose();
      moments.dispose();
      f.client.close();
    },
  );
  for (final kind in ['activity', 'community'])
    testWidgets(
      '$kind original message report passes personal owner and exact message ID',
      (t) async {
        humanViewport(t);
        final auth = SeedTestAuth()..owner = humanOwner,
            f = HumanControlFixture();
        f.intercept = (r) async {
          if (!r.url.path.contains('/conversation')) return null;
          if (r.url.path.endsWith('/messages'))
            return humanWire({
              'messages': [
                {
                  'id': humanResult,
                  'senderAccountId': humanTarget,
                  'senderName': '合成参与者',
                  'body': '原消息内容',
                  'createdAt': '2026-10-14T00:00:00Z',
                  'removed': false,
                },
              ],
              'hasMore': false,
            });
          return humanWire({
            'id': humanOp,
            'viewerAccountId': humanOwner,
            'joined': true,
            'canJoin': false,
            'canSend': true,
            'moderator': false,
          });
        };
        Widget page = kind == 'activity'
            ? ActivityConversationPage(
                activityID: humanTarget,
                activityTitle: '合成活动',
                authorizationHeader: () => auth.authorizationHeader,
                ownerID: () => auth.accountID,
                identityChanges: auth,
                client: f.client,
                apiBaseUrl: humanBase,
              )
            : CommunityConversationPage(
                communityID: humanTarget,
                communityTitle: '合成社群',
                authorizationHeader: () => auth.authorizationHeader,
                ownerID: () => auth.accountID,
                identityChanges: auth,
                client: f.client,
                apiBaseUrl: humanBase,
              );
        await t.pumpWidget(humanApp(page));
        await humanSettle(t);
        await humanTap(t, find.byTooltip('消息操作'));
        await humanTap(t, find.text('举报消息'));
        final support = t.widget<SupportPage>(find.byType(SupportPage));
        expect(support.targetType, '${kind}_message');
        expect(support.targetID, humanResult);
        expect(support.ownerID!(), humanOwner);
        expect(support.client, same(f.client));
        await t.enterText(
          find.byKey(const Key('support-details')),
          '此条原消息存在持续骚扰，请协助核查。',
        );
        await t.pump();
        await humanTap(t, find.byKey(const Key('support-submit')));
        await humanTap(t, find.byKey(const Key('human-control-confirm')));
        expect(f.writes.length, 1);
        expect(f.writes.single.body, contains('"targetId":"$humanResult"'));
        expect(
          f.writes.single.body,
          contains('"targetType":"${kind}_message"'),
        );
        await t.pumpWidget(const SizedBox());
        auth.dispose();
        f.client.close();
      },
    );
  testWidgets(
    'original contact menu uses pair State/preview, no unversioned block endpoint',
    (t) async {
      humanViewport(t);
      final auth = SeedTestAuth()..owner = humanOwner,
          f = HumanControlFixture();
      f.intercept = (r) async {
        if (r.url.path.endsWith('/ties/page'))
          return humanWire({
            'items': [
              {
                'id': humanOp,
                'otherAccountId': humanTarget,
                'otherName': '合成好友',
                'createdAt': '2026-10-14T00:00:00Z',
              },
            ],
            'nextCursor': null,
          });
        if (r.url.path.endsWith('/connection-requests/page'))
          return humanWire({'items': [], 'nextCursor': null});
        if (r.url.path.endsWith('/conversations/page'))
          return humanWire({'items': [], 'nextCursor': null, 'unreadCount': 0});
        return null;
      };
      await t.pumpWidget(
        humanApp(
          Scaffold(
            body: SingleChildScrollView(
              child: SocialInboxSection(
                auth: auth,
                client: f.client,
                apiBaseUrl: humanBase,
              ),
            ),
          ),
        ),
      );
      await humanSettle(t);
      await humanTap(t, find.byTooltip('好友操作'));
      await humanTap(t, find.text('屏蔽账号'));
      expect(find.text('屏蔽设置'), findsOneWidget);
      expect(f.writes, isEmpty);
      await humanTap(t, find.byKey(const Key('human-privacy-apply')));
      await humanTap(t, find.byKey(const Key('human-control-confirm')));
      expect(f.writes.length, 1);
      expect(
        f.calls.where(
          (r) => r.url.path == '/v1/me/blocks' && r.method != 'GET',
        ),
        isEmpty,
      );
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      f.client.close();
    },
  );
  testWidgets(
    'caller logical source ABA retires helper even when token is renewed normally',
    (t) async {
      final auth = SeedTestAuth()..owner = humanOwner,
          source = ValueNotifier('A'),
          f = HumanControlFixture();
      await t.pumpWidget(
        humanApp(
          humanSupportDestination(
            authorizationHeader: () => auth.authorizationHeader,
            ownerID: () => auth.accountID,
            identityChanges: Listenable.merge([auth, source]),
            sourceKey: () => source.value,
            targetType: 'account',
            targetID: humanTarget,
            client: f.client,
            apiBaseUrl: humanBase,
          ),
        ),
      );
      await humanSettle(t);
      await t.enterText(
        find.byKey(const Key('support-details')),
        '来源仍合法时保留本人未提交正文。',
      );
      auth.changeIdentity('Bearer renewed', nextOwner: humanOwner);
      await humanSettle(t);
      expect(
        t
            .widget<TextField>(find.byKey(const Key('support-details')))
            .controller!
            .text,
        '来源仍合法时保留本人未提交正文。',
      );
      source.value = 'B';
      source.value = 'A';
      await t.pumpAndSettle();
      expect(find.byKey(const Key('support-details')), findsNothing);
      expect(f.writes, isEmpty);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      source.dispose();
      f.client.close();
    },
  );
}
