import 'human_causal_pending_store_test.dart' show humanRemember;
import 'dart:convert';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/workspace/organization_human_control.dart';
import 'package:birdtie_client/src/workspace/organization_membership_pages.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'organization_human_control_test.dart';
import 'human_causal_control_test.dart';
import 'package:birdtie_client/src/workspace/human_causal_control.dart';
import 'package:birdtie_client/src/workspace/human_causal_pending_store.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'organization_invite_page_test.dart'
    show orgNormalView, enterOrgKnownAccount;

Future<BirdtieAuthController> invitationAuth() async {
  final c = MockClient(
    (r) async => r.url.path == '/v1/me'
        ? orgWire({'id': orgTestTarget})
        : orgWire({'enabled': false}),
  );
  final auth = BirdtieAuthController(
    client: c,
    apiBaseUrl: orgTestBase,
    sessionVault: MemorySessionVault()
      ..session = const StoredSession(token: 'original', method: 'oidc'),
  );
  await auth.initialize();
  addTearDown(c.close);
  addTearDown(auth.dispose);
  return auth;
}

Widget membersApp(
  http.Client c,
  MemoryOrganizationControlPendingStore store, {
  ValueNotifier<int>? signal,
  String? Function()? owner,
}) => orgHarness(
  OrganizationMembersPage(
    organizationID: orgTestID,
    organizationName: '合成组织',
    currentRole: 'owner',
    authorizationHeader: () => 'Bearer original',
    ownerID: owner ?? () => orgTestOwner,
    identityChanges: signal,
    client: c,
    apiBaseUrl: orgTestBase,
    pendingStore: store,
  ),
);
void main() {
  setUpAll(orgFonts);
  testWidgets(
    'FAQ original recovery remains at invitations entry after current organization role lost',
    (t) async {
      final auth = SeedTestAuth()..owner = humanOwner,
          f = HumanControlFixture()..stateDenied = true;
      final pending = MemoryHumanCausalPendingStore();
      const input = HumanFAQInput(
        organizationID: humanOrg,
        operationID: humanOp,
        action: 'CREATE',
        expectedVersion: 0,
        question: '原问题',
        answer: '原操作已经成功的组织回答。',
        published: false,
      );
      await humanRemember(
        pending,
        humanBase,
        humanOwner,
        HumanCausalPending(input.selector),
      );
      f.receipts[humanOp] = humanReceiptWire(input);
      var accepted = 0;
      await t.pumpWidget(
        orgHarness(
          OrganizationInvitationsPage(
            auth: auth,
            client: f.client,
            apiBaseUrl: humanBase,
            pendingStore: MemoryOrganizationControlPendingStore(),
            humanPendingStore: pending,
            onAccepted: () async {
              accepted++;
            },
          ),
        ),
      );
      await humanSettle(t);
      await humanTap(
        t,
        find.byKey(const Key('organization_faq_recovery_$humanOrg')),
      );
      expect(find.text('新增问答'), findsNothing);
      await humanTap(t, find.byKey(const Key('human-control-recover')));
      expect(find.textContaining('原操作已完成，当前资料暂不可读'), findsOneWidget);
      expect(f.writes, isEmpty);
      expect(accepted, 0);
      expect(await pending.read(humanBase, humanOwner), isEmpty);
      await t.pageBack();
      await humanSettle(t);
      expect(
        find.byKey(const Key('organization_faq_recovery_$humanOrg')),
        findsNothing,
      );
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      f.client.close();
    },
  );
  testWidgets('owner sees Chinese roster and sends account-bound invitation', (
    t,
  ) async {
    orgNormalView(t);
    var invited = false;
    Map<String, dynamic>? original;
    final c = MockClient((r) async {
      expectSync(r.headers['Authorization'], 'Bearer original');
      if (r.url.path.endsWith('/members/control'))
        return orgWire(
          orgRosterWire(
            authority: invited ? 5 : 4,
            members: [
              orgMemberWire(
                id: orgTestOwner,
                account: orgTestOwner,
                name: '合成负责人',
                role: 'owner',
                authority: invited ? 5 : 4,
              ),
              if (invited)
                orgMemberWire(
                  id: original!['operationId'],
                  name: '合成受邀成员',
                  status: 'invited',
                  version: 1,
                  authority: 5,
                ),
            ],
          ),
        );
      if (r.url.path.endsWith('/preview'))
        return orgWire(orgPreviewWire(orgInput(jsonDecode(r.body))));
      if (r.method == 'PUT') {
        original = jsonDecode(r.body);
        expectSync(original!['userAccountId'], orgTestTarget);
        expectSync(original!['expectedMemberVersion'], 0);
        expectSync(original!['expectedAuthorityVersion'], 4);
        invited = true;
        return orgWire(orgReceiptWire(orgInput(original!).selector(orgTestID)));
      }
      return orgWire(null, status: 404);
    });
    addTearDown(c.close);
    final store = MemoryOrganizationControlPendingStore();
    await t.pumpWidget(membersApp(c, store));
    await orgSettle(t);
    expect(find.text('合成负责人'), findsOneWidget);
    await orgTap(t, find.byKey(const Key('organization_invite_member')));
    await enterOrgKnownAccount(t);
    expect(invited, isFalse);
    await orgTap(t, find.byKey(const Key('organization_confirm_control')));
    expect(invited, isTrue);
    await t.pageBack();
    await orgSettle(t);
    expect(find.text('合成受邀成员'), findsOneWidget);
    expect(find.textContaining('待接受'), findsOneWidget);
    expect(store.values, isEmpty);
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
  });
  testWidgets('invitee accepts a targeted invitation in Chinese UI', (t) async {
    orgNormalView(t);
    final auth = await invitationAuth();
    var accepted = false, refreshed = false;
    Map<String, dynamic>? original;
    final c = MockClient((r) async {
      if (r.url.path == '/v1/me/organization-invitations')
        return orgWire(
          accepted
              ? []
              : [
                  {
                    ...orgMemberWire(status: 'invited'),
                    'organizationName': '合成组织',
                  },
                ],
        );
      if (r.url.path.endsWith('/preview'))
        return orgWire(
          orgPreviewWire(orgInput(jsonDecode(r.body)), canonical: 6),
        );
      if (r.method == 'PUT') {
        original = jsonDecode(r.body);
        expectSync(original!['membershipId'], orgTestMember);
        expectSync(original!['expectedMemberVersion'], 2);
        expectSync(original!['action'], 'ACCEPT');
        accepted = true;
        return orgWire(orgReceiptWire(orgInput(original!).selector(orgTestID)));
      }
      return orgWire(null, status: 404);
    });
    addTearDown(c.close);
    await t.pumpWidget(
      orgHarness(
        OrganizationInvitationsPage(
          auth: auth,
          client: c,
          apiBaseUrl: orgTestBase,
          pendingStore: MemoryOrganizationControlPendingStore(),
          onAccepted: () async {
            refreshed = true;
          },
        ),
      ),
    );
    await orgSettle(t);
    expect(find.text('合成组织'), findsOneWidget);
    expect(find.byKey(const Key('my_account_id')), findsOneWidget);
    await orgTap(t, find.text('接受邀请'));
    expect(find.textContaining('相应工作区权限'), findsOneWidget);
    expect(accepted, isFalse);
    await orgTap(t, find.byKey(const Key('organization_confirm_control')));
    expect(accepted, isTrue);
    expect(refreshed, isTrue);
    expect(find.text('目前没有待处理的组织邀请。'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
  });
  testWidgets(
    '139 missing permitted name still offers role management and original revocation',
    (t) async {
      orgNormalView(t);
      var revoked = false;
      Map<String, dynamic>? original;
      final c = MockClient((r) async {
        if (r.url.path.endsWith('/members/control'))
          return orgWire(
            orgRosterWire(
              authority: revoked ? 5 : 4,
              members: [
                orgMemberWire(
                  name: null,
                  status: revoked ? 'removed' : 'active',
                  version: revoked ? 3 : 2,
                  authority: revoked ? 5 : 4,
                ),
              ],
            ),
          );
        if (r.url.path.endsWith('/preview'))
          return orgWire(orgPreviewWire(orgInput(jsonDecode(r.body))));
        if (r.method == 'PUT') {
          original = jsonDecode(r.body);
          revoked = true;
          return orgWire(
            orgReceiptWire(orgInput(original!).selector(orgTestID)),
          );
        }
        return orgWire(null, status: 404);
      });
      addTearDown(c.close);
      await t.pumpWidget(
        membersApp(c, MemoryOrganizationControlPendingStore()),
      );
      await orgSettle(t);
      expect(find.textContaining('账号资料暂不可用 · 成员 1'), findsOneWidget);
      await orgCapture(t, 'members-missing-name-normal390');
      await orgTap(
        t,
        find.byKey(const Key('organization_member_menu_$orgTestMember')),
      );
      expect(find.text('设为协管员'), findsOneWidget);
      await orgTap(t, find.text('移除成员'));
      expect(find.textContaining('失去此组织工作区权限'), findsOneWidget);
      expect(find.textContaining('个人账号：$orgTestTarget'), findsOneWidget);
      await orgTap(t, find.byKey(const Key('organization_confirm_control')));
      expect(revoked, isTrue);
      expect(original!['membershipId'], orgTestMember);
      expect(original!['expectedMemberVersion'], 2);
      expect(original!['action'], 'REVOKE');
      expect(find.textContaining('已移除'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
  testWidgets(
    '139 actual current admin policy wins over stale constructor owner role',
    (t) async {
      orgNormalView(t);
      final c = MockClient(
        (r) async => orgWire(
          orgRosterWire(
            role: 'admin',
            members: [
              orgMemberWire(
                id: orgTestOwner,
                account: orgTestOwner,
                name: null,
                role: 'owner',
              ),
              orgMemberWire(name: null),
            ],
          ),
        ),
      );
      addTearDown(c.close);
      await t.pumpWidget(
        membersApp(c, MemoryOrganizationControlPendingStore()),
      );
      await orgSettle(t);
      expect(
        find.byKey(const Key('organization_member_menu_$orgTestOwner')),
        findsNothing,
      );
      await orgTap(
        t,
        find.byKey(const Key('organization_member_menu_$orgTestMember')),
      );
      expect(find.text('设为所有者'), findsNothing);
      expect(find.text('设为管理员'), findsNothing);
      expect(find.text('移除成员'), findsOneWidget);
      await t.tapAt(const Offset(5, 5));
      await orgSettle(t);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
  testWidgets(
    '139 invitation version ABA retires old accept callback after re-read',
    (t) async {
      orgNormalView(t);
      final auth = await invitationAuth();
      var version = 2, previews = 0;
      final c = MockClient((r) async {
        if (r.url.path == '/v1/me/organization-invitations')
          return orgWire([
            {
              ...orgMemberWire(status: 'invited', version: version),
              'organizationName': '合成组织',
            },
          ]);
        if (r.url.path.endsWith('/preview')) previews++;
        return orgWire(null, status: 404);
      });
      addTearDown(c.close);
      await t.pumpWidget(
        orgHarness(
          OrganizationInvitationsPage(
            auth: auth,
            client: c,
            apiBaseUrl: orgTestBase,
            pendingStore: MemoryOrganizationControlPendingStore(),
            onAccepted: () async {},
          ),
        ),
      );
      await orgSettle(t);
      final old = t
          .widget<FilledButton>(find.widgetWithText(FilledButton, '接受邀请'))
          .onPressed!;
      version = 4;
      await orgTap(t, find.text('重新读取邀请'));
      old();
      await orgSettle(t);
      expect(previews, 0);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
  testWidgets(
    '139 original map selector stays reachable from invitations after organization access loss',
    (t) async {
      orgNormalView(t);
      final auth = await invitationAuth();
      const input = OrganizationControlInput(
        action: 'HIDE_POINT',
        operationID: orgTestOp,
        expectedAuthorityVersion: 4,
        expectedCanonicalRevision: 3,
      );
      final store = MemoryOrganizationControlPendingStore(),
          s = input.selector(orgTestID);
      await store.write(
        orgTestBase,
        orgTestTarget,
        orgTestID,
        OrganizationControlPending(s),
      );
      var writes = 0, originalReads = 0;
      final c = MockClient((r) async {
        if (r.method != 'GET') writes++;
        if (r.url.path == '/v1/me/organization-invitations') return orgWire([]);
        if (r.url.path.contains('/operations/')) {
          originalReads++;
          return orgWire(orgReceiptWire(s));
        }
        return orgWire(null, status: 403);
      });
      addTearDown(c.close);
      await t.pumpWidget(
        orgHarness(
          OrganizationInvitationsPage(
            auth: auth,
            client: c,
            apiBaseUrl: orgTestBase,
            pendingStore: store,
            onAccepted: () async {},
          ),
        ),
      );
      await orgSettle(t);
      expect(
        find.byKey(const Key('organization_map_recovery_$orgTestID')),
        findsOneWidget,
      );
      await orgCapture(t, 'invitations-original-map-recovery-normal390');
      await orgTap(
        t,
        find.byKey(const Key('organization_map_recovery_$orgTestID')),
      );
      expect(find.byKey(const Key('organization_preview_point')), findsNothing);
      await orgTap(t, find.byKey(const Key('organization_verify_operation')));
      expect(originalReads, 1);
      expect(writes, 0);
      expect(store.values, isEmpty);
      expect(find.text('原操作已完成。'), findsOneWidget);
      await t.pageBack();
      await orgSettle(t);
      expect(
        find.byKey(const Key('organization_map_recovery_$orgTestID')),
        findsNothing,
      );
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
}
