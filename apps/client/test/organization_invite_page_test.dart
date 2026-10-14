import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/organization_human_control.dart';
import 'package:birdtie_client/src/workspace/organization_invite_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'organization_human_control_test.dart';

void orgNormalView(WidgetTester t) {
  t.view.physicalSize = const Size(390, 844);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
}

Widget inviteApp(
  http.Client c,
  MemoryOrganizationControlPendingStore store, {
  ValueNotifier<int>? signal,
  String? Function()? owner,
  String? Function()? header,
}) => orgHarness(
  OrganizationInvitePage(
    organizationID: orgTestID,
    organizationName: '合成组织',
    authorizationHeader: header ?? () => 'Bearer original',
    ownerID: owner ?? () => orgTestOwner,
    identityChanges: signal,
    client: c,
    apiBaseUrl: orgTestBase,
    pendingStore: store,
  ),
);
Future<void> enterOrgKnownAccount(WidgetTester t) async {
  await t.enterText(find.byKey(const Key('member_account_id')), orgTestTarget);
  await orgTap(t, find.byKey(const Key('organization_preview_invite')));
}

void main() {
  setUpAll(orgFonts);
  testWidgets(
    '139 old people row after source reread cannot restore retired name or selection',
    (t) async {
      orgNormalView(t);
      final c = MockClient(
        (r) async => r.url.path == '/v1/people/search'
            ? orgWire([
                {'accountId': orgTestTarget, 'displayName': '旧获准合成人员'},
              ])
            : orgWire(orgRosterWire(members: [])),
      );
      addTearDown(c.close);
      await t.pumpWidget(inviteApp(c, MemoryOrganizationControlPendingStore()));
      await orgSettle(t);
      await t.enterText(
        find.byKey(const Key('organization_people_query')),
        '合成人',
      );
      await orgTap(t, find.byKey(const Key('organization_people_search')));
      final old = t
          .widget<ListTile>(
            find.byKey(const Key('organization_person_$orgTestTarget')),
          )
          .onTap!;
      await orgTap(t, find.text('重新读取成员'));
      expect(find.text('旧获准合成人员'), findsNothing);
      old();
      await orgSettle(t);
      expect(
        t
            .widget<TextField>(find.byKey(const Key('member_account_id')))
            .controller!
            .text,
        '',
      );
      expect(find.textContaining('已选择：'), findsNothing);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
  testWidgets(
    '139 owner to admin source change keeps original draft role but disables granting it',
    (t) async {
      orgNormalView(t);
      var role = 'owner', previews = 0;
      final c = MockClient((r) async {
        if (r.url.path.endsWith('/members/control'))
          return orgWire(orgRosterWire(role: role, members: []));
        if (r.url.path.endsWith('/preview')) previews++;
        return orgWire(null, status: 404);
      });
      addTearDown(c.close);
      await t.pumpWidget(inviteApp(c, MemoryOrganizationControlPendingStore()));
      await orgSettle(t);
      await t.enterText(
        find.byKey(const Key('member_account_id')),
        orgTestTarget,
      );
      await orgTap(t, find.byKey(const Key('organization_invite_role')));
      await orgTap(t, find.text('管理员').last);
      role = 'admin';
      await orgTap(t, find.text('重新读取成员'));
      expect(t.takeException(), isNull);
      expect(
        t
            .widget<DropdownButton<String>>(
              find.byKey(const Key('organization_invite_role')),
            )
            .value,
        'admin',
      );
      expect(
        t
            .widget<FilledButton>(
              find.byKey(const Key('organization_preview_invite')),
            )
            .onPressed,
        isNull,
      );
      expect(
        t
            .widget<TextField>(find.byKey(const Key('member_account_id')))
            .controller!
            .text,
        orgTestTarget,
      );
      expect(previews, 0);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
  testWidgets(
    '139 invite authorized people search binds original account and concrete removed version',
    (t) async {
      orgNormalView(t);
      var puts = 0;
      Map<String, dynamic>? body;
      final store = MemoryOrganizationControlPendingStore();
      final c = MockClient((r) async {
        expectSync(r.headers['Authorization'], 'Bearer original');
        expectSync(r.headers.containsKey('X-Birdtie-Organization'), isFalse);
        expectSync(r.url.path.contains('contacts'), isFalse);
        if (r.url.path == '/v1/people/search') {
          expectSync(r.url.queryParameters, {'q': '合成人'});
          return orgWire([
            {'accountId': orgTestTarget, 'displayName': '合成人员'},
          ]);
        }
        if (r.url.path.endsWith('/members/control'))
          return orgWire(
            orgRosterWire(
              members: [orgMemberWire(status: 'removed', version: 7)],
            ),
          );
        if (r.url.path.endsWith('/preview'))
          return orgWire(
            orgPreviewWire(orgInput(jsonDecode(r.body)), canonical: 3),
          );
        if (r.method == 'PUT') {
          puts++;
          body = jsonDecode(r.body);
          final input = orgInput(body!);
          return orgWire(
            orgReceiptWire(
              input.selector(orgTestID, invitedMembershipID: orgTestMember),
            ),
          );
        }
        return orgWire(null, status: 404);
      });
      addTearDown(c.close);
      await t.pumpWidget(inviteApp(c, store));
      await orgSettle(t);
      await t.enterText(
        find.byKey(const Key('organization_people_query')),
        '合成人',
      );
      await orgTap(t, find.byKey(const Key('organization_people_search')));
      await orgTap(
        t,
        find.byKey(const Key('organization_person_$orgTestTarget')),
      );
      expect(
        t
            .widget<TextField>(find.byKey(const Key('member_account_id')))
            .controller!
            .text,
        orgTestTarget,
      );
      await orgCapture(t, 'invite-authorized-person-normal390');
      await orgTap(t, find.byKey(const Key('organization_preview_invite')));
      expect(find.textContaining('个人账号：$orgTestTarget'), findsOneWidget);
      expect(find.textContaining('相应工作区权限'), findsOneWidget);
      expect(puts, 0);
      await orgCapture(t, 'invite-specific-version-confirm-normal390');
      await orgTap(t, find.byKey(const Key('organization_confirm_control')));
      expect(puts, 1);
      expect(body!['userAccountId'], orgTestTarget);
      expect(body!['expectedMemberVersion'], 7);
      expect(body!['expectedAuthorityVersion'], 4);
      expect(body!['approval'], startsWith('oh1.'));
      expect(body!.containsKey('displayName'), isFalse);
      expect(body!.containsKey('membershipId'), isFalse);
      expect(store.values, isEmpty);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      expect(t.takeException(), isNull);
    },
  );
  testWidgets(
    '139 known account fallback and cancel retain draft without domain write',
    (t) async {
      orgNormalView(t);
      var puts = 0;
      final c = MockClient((r) async {
        if (r.url.path.endsWith('/members/control'))
          return orgWire(orgRosterWire(members: []));
        if (r.url.path.endsWith('/preview'))
          return orgWire(orgPreviewWire(orgInput(jsonDecode(r.body))));
        if (r.method == 'PUT') puts++;
        return orgWire(null, status: 404);
      });
      addTearDown(c.close);
      final store = MemoryOrganizationControlPendingStore();
      await t.pumpWidget(inviteApp(c, store));
      await orgSettle(t);
      await enterOrgKnownAccount(t);
      expect(find.textContaining('账号资料暂不可用'), findsOneWidget);
      await orgTap(t, find.widgetWithText(TextButton, '取消'));
      expect(
        t
            .widget<TextField>(find.byKey(const Key('member_account_id')))
            .controller!
            .text,
        orgTestTarget,
      );
      expect(puts, 0);
      expect(store.values, isEmpty);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
  testWidgets(
    '139 captured search row and late query retire across actor ABA',
    (t) async {
      orgNormalView(t);
      final signal = ValueNotifier(0);
      addTearDown(signal.dispose);
      var owner = orgTestOwner, searches = 0;
      final late = Completer<http.Response>();
      final c = MockClient((r) async {
        if (r.url.path.endsWith('/members/control'))
          return orgWire(orgRosterWire(members: []));
        if (r.url.path == '/v1/people/search') {
          searches++;
          if (searches == 1)
            return orgWire([
              {'accountId': orgTestTarget, 'displayName': '旧合成人员'},
            ]);
          return late.future;
        }
        return orgWire(null, status: 404);
      });
      addTearDown(c.close);
      await t.pumpWidget(
        inviteApp(
          c,
          MemoryOrganizationControlPendingStore(),
          signal: signal,
          owner: () => owner,
        ),
      );
      await orgSettle(t);
      await t.enterText(
        find.byKey(const Key('organization_people_query')),
        '合成人',
      );
      await orgTap(t, find.byKey(const Key('organization_people_search')));
      final old = t
          .widget<ListTile>(
            find.byKey(const Key('organization_person_$orgTestTarget')),
          )
          .onTap!;
      await t.tap(find.byKey(const Key('organization_people_search')));
      await t.pump();
      owner = orgTestTarget;
      signal.value++;
      owner = orgTestOwner;
      signal.value++;
      await t.pump();
      old();
      late.complete(
        orgWire([
          {'accountId': orgTestTarget, 'displayName': '迟到合成人员'},
        ]),
      );
      await orgSettle(t);
      expect(find.text('旧合成人员'), findsNothing);
      expect(find.text('迟到合成人员'), findsNothing);
      expect(
        find.byKey(const Key('organization_preview_invite')),
        findsNothing,
      );
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      expect(t.takeException(), isNull);
    },
  );
  testWidgets('139 selected roster version is not rebased by a fresh read', (
    t,
  ) async {
    orgNormalView(t);
    var authority = 4, version = 7, previews = 0;
    final c = MockClient((r) async {
      if (r.url.path.endsWith('/members/control'))
        return orgWire(
          orgRosterWire(
            authority: authority,
            members: [
              orgMemberWire(
                status: 'removed',
                version: version,
                authority: authority,
              ),
            ],
          ),
        );
      if (r.url.path == '/v1/people/search')
        return orgWire([
          {'accountId': orgTestTarget, 'displayName': '合成人员'},
        ]);
      if (r.url.path.endsWith('/preview')) previews++;
      return orgWire(null, status: 404);
    });
    addTearDown(c.close);
    await t.pumpWidget(inviteApp(c, MemoryOrganizationControlPendingStore()));
    await orgSettle(t);
    await t.enterText(
      find.byKey(const Key('organization_people_query')),
      '合成人',
    );
    await orgTap(t, find.byKey(const Key('organization_people_search')));
    await orgTap(
      t,
      find.byKey(const Key('organization_person_$orgTestTarget')),
    );
    authority = 5;
    version = 8;
    await orgTap(t, find.text('重新读取成员'));
    await orgTap(t, find.byKey(const Key('organization_preview_invite')));
    expect(previews, 0);
    expect(find.textContaining('选择时的成员版本已变化'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
  });
  testWidgets(
    '139 expired approval captured callback cannot prepare journal or PUT',
    (t) async {
      orgNormalView(t);
      var puts = 0;
      final store = MemoryOrganizationControlPendingStore();
      final c = MockClient((r) async {
        if (r.url.path.endsWith('/members/control'))
          return orgWire(orgRosterWire(members: []));
        if (r.url.path.endsWith('/preview'))
          return orgWire(
            orgPreviewWire(orgInput(jsonDecode(r.body)), lease: 3),
          );
        if (r.method == 'PUT') puts++;
        return orgWire(null, status: 404);
      });
      addTearDown(c.close);
      await t.pumpWidget(inviteApp(c, store));
      await orgSettle(t);
      await enterOrgKnownAccount(t);
      final old = t
          .widget<FilledButton>(
            find.byKey(const Key('organization_confirm_control')),
          )
          .onPressed!;
      await t.pump(const Duration(seconds: 4));
      await t.pump();
      expect(
        t
            .widget<FilledButton>(
              find.byKey(const Key('organization_confirm_control')),
            )
            .onPressed,
        isNull,
      );
      expect(find.textContaining('个人账号：$orgTestTarget'), findsNothing);
      old();
      await orgSettle(t);
      expect(puts, 0);
      expect(store.values, isEmpty);
      await orgCapture(t, 'invite-expired-confirm-normal390');
      await orgTap(t, find.widgetWithText(TextButton, '取消'));
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      expect(t.takeException(), isNull);
    },
  );
  testWidgets(
    '139 target ABA while confirmation reverse animation retains notifier through unmount',
    (t) async {
      orgNormalView(t);
      final signal = ValueNotifier(0);
      addTearDown(signal.dispose);
      var owner = orgTestOwner, writes = 0;
      final delayed = Completer<http.Response>();
      Map<String, dynamic>? original;
      final c = MockClient((r) async {
        if (r.url.path.endsWith('/members/control'))
          return orgWire(orgRosterWire(members: []));
        if (r.url.path.endsWith('/preview'))
          return orgWire(orgPreviewWire(orgInput(jsonDecode(r.body))));
        if (r.method == 'PUT') {
          writes++;
          original = jsonDecode(r.body);
          return delayed.future;
        }
        return orgWire(null, status: 404);
      });
      addTearDown(c.close);
      final store = MemoryOrganizationControlPendingStore();
      Widget app() => inviteApp(c, store, signal: signal, owner: () => owner);
      await t.pumpWidget(app());
      await orgSettle(t);
      await enterOrgKnownAccount(t);
      await t.tap(find.byKey(const Key('organization_confirm_control')));
      await t.pump();
      await t.runAsync(() async {
        for (var i = 0; i < 60 && writes == 0; i++) {
          await Future<void>.delayed(const Duration(milliseconds: 20));
        }
      });
      expect(writes, 1);
      expect(find.byType(AlertDialog), findsOneWidget);
      owner = orgTestTarget;
      signal.value++;
      owner = orgTestOwner;
      signal.value++;
      await t.pumpWidget(app());
      expect(find.textContaining('个人账号：$orgTestTarget'), findsNothing);
      expect(t.takeException(), isNull);
      delayed.complete(
        orgWire(orgReceiptWire(orgInput(original!).selector(orgTestID))),
      );
      await orgSettle(t);
      expect(writes, 1);
      expect(
        store.values.values.single.selector.operationID,
        original!['operationId'],
      );
      expect(find.text('原操作已完成。'), findsNothing);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      expect(t.takeException(), isNull);
    },
  );
  testWidgets('139 ordinary credential renewal preserves local account draft', (
    t,
  ) async {
    orgNormalView(t);
    final signal = ValueNotifier(0);
    addTearDown(signal.dispose);
    var token = 'Bearer first';
    final c = MockClient((r) async => orgWire(orgRosterWire(members: [])));
    addTearDown(c.close);
    await t.pumpWidget(
      inviteApp(
        c,
        MemoryOrganizationControlPendingStore(),
        signal: signal,
        header: () => token,
      ),
    );
    await orgSettle(t);
    await t.enterText(
      find.byKey(const Key('member_account_id')),
      orgTestTarget,
    );
    token = 'Bearer renewed';
    signal.value++;
    await t.pump();
    await orgTap(t, find.text('重新读取成员'));
    expect(
      t
          .widget<TextField>(find.byKey(const Key('member_account_id')))
          .controller!
          .text,
      orgTestTarget,
    );
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
  });
}
