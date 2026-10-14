import 'dart:convert';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/organization_human_control.dart';
import 'package:birdtie_client/src/workspace/organization_map_location_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'organization_human_control_test.dart';
import 'organization_invite_page_test.dart' show orgNormalView;
import 'organization_map_location_api_test.dart';

Widget orgMapApp(
  http.Client client,
  MemoryOrganizationControlPendingStore store, {
  String cityID = 'aberdeen-gb',
  PublicCity? city,
  ValueNotifier<int>? signal,
  String? Function()? owner,
  String? Function()? header,
}) => orgHarness(
  OrganizationMapLocationPage(
    organizationID: orgTestID,
    cityID: cityID,
    city: city,
    authorizationHeader: header ?? () => 'Bearer original',
    ownerID: owner ?? () => orgTestOwner,
    identityChanges: signal,
    client: client,
    apiBaseUrl: orgTestBase,
    pendingStore: store,
  ),
);
Future<void> orgCoordinates(
  WidgetTester t, {
  String lat = '57.2',
  String lon = '-2.2',
}) async {
  await t.enterText(find.byKey(const Key('organization_latitude')), lat);
  await t.enterText(find.byKey(const Key('organization_longitude')), lon);
  t.testTextInput.hide();
  await orgSettle(t);
}

void main() {
  setUpAll(orgFonts);
  testWidgets(
    '139 explicit position proposal preserves old canonical until independent approval',
    (t) async {
      orgNormalView(t);
      var puts = 0;
      Map<String, dynamic>? original;
      var submitted = false;
      final c = MockClient((r) async {
        if (r.url.path.endsWith('/map-location/control'))
          return orgWire(
            orgMapWire(
              proposals: submitted
                  ? [
                      orgProposalWire(
                        id: original!['operationId'],
                        version: 1,
                        withdraw: true,
                      ),
                    ]
                  : [],
            ),
          );
        if (r.url.path.endsWith('/preview'))
          return orgWire(orgPreviewWire(orgInput(jsonDecode(r.body))));
        if (r.method == 'PUT') {
          puts++;
          original = jsonDecode(r.body);
          submitted = true;
          return orgWire(
            orgReceiptWire(orgInput(original!).selector(orgTestID)),
          );
        }
        return orgWire(null, status: 404);
      });
      addTearDown(c.close);
      final store = MemoryOrganizationControlPendingStore();
      await t.pumpWidget(orgMapApp(c, store));
      await orgSettle(t);
      await orgCoordinates(t);
      await orgCapture(t, 'map-proposal-draft-normal390');
      await orgTap(t, find.byKey(const Key('organization_preview_point')));
      expect(find.textContaining('原公开点保持原状'), findsOneWidget);
      expect(puts, 0);
      await orgCapture(t, 'map-proposal-confirm-normal390');
      await orgTap(t, find.byKey(const Key('organization_confirm_control')));
      expect(puts, 1);
      expect(original!['expectedCanonicalRevision'], 3);
      expect(original!['expectedAuthorityVersion'], 4);
      expect(original!['cityId'], 'aberdeen-gb');
      expect(original!['latitude'], 57.2);
      expect(original!['longitude'], -2.2);
      expect(original!['action'], 'PROPOSE');
      expect(find.textContaining('原点 WGS84：57.1, -2.1 · 版本 3'), findsOneWidget);
      expect(find.textContaining('提案 1 · 待审核'), findsOneWidget);
      expect(store.values, isEmpty);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      expect(t.takeException(), isNull);
    },
  );
  testWidgets(
    '139 nonmember same-city reviewer can approve existing proposal without selected city',
    (t) async {
      orgNormalView(t);
      var puts = 0, approved = false;
      Map<String, dynamic>? original;
      final c = MockClient((r) async {
        if (r.url.path.endsWith('/map-location/control'))
          return orgWire(
            orgMapWire(
              role: 'reviewer',
              canonical: approved
                  ? {
                      ...orgCanonicalWire(revision: 4),
                      'latitude': 57.2,
                      'longitude': -2.2,
                    }
                  : null,
              proposals: [
                orgProposalWire(
                  version: approved ? 3 : 2,
                  status: approved ? 'APPROVED' : 'PENDING',
                  review: !approved,
                ),
              ],
            ),
          );
        if (r.url.path.endsWith('/preview'))
          return orgWire(orgPreviewWire(orgInput(jsonDecode(r.body))));
        if (r.method == 'PUT') {
          puts++;
          original = jsonDecode(r.body);
          approved = true;
          return orgWire(
            orgReceiptWire(orgInput(original!).selector(orgTestID)),
          );
        }
        return orgWire(null, status: 404);
      });
      addTearDown(c.close);
      await t.pumpWidget(
        orgMapApp(c, MemoryOrganizationControlPendingStore(), cityID: ''),
      );
      await orgSettle(t);
      expect(find.byKey(const Key('organization_latitude')), findsNothing);
      expect(find.byKey(const Key('organization_preview_point')), findsNothing);
      await orgCapture(t, 'map-nonmember-reviewer-normal390');
      await orgTap(
        t,
        find.byKey(const Key('organization_approve_$orgTestProposal')),
      );
      expect(find.textContaining('独立批准'), findsOneWidget);
      await orgTap(t, find.byKey(const Key('organization_confirm_control')));
      expect(puts, 1);
      expect(original!['proposalId'], orgTestProposal);
      expect(original!['expectedProposalVersion'], 2);
      expect(original!['expectedCanonicalRevision'], 3);
      expect(original!.containsKey('cityId'), isFalse);
      expect(find.textContaining('原点 WGS84：57.2, -2.2 · 版本 4'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
  testWidgets(
    '139 stale proposal callback cannot target reloaded same proposal ID with new version',
    (t) async {
      orgNormalView(t);
      var version = 2, previews = 0;
      final c = MockClient((r) async {
        if (r.url.path.endsWith('/map-location/control'))
          return orgWire(
            orgMapWire(
              role: 'reviewer',
              proposals: [orgProposalWire(version: version, review: true)],
            ),
          );
        if (r.url.path.endsWith('/preview')) previews++;
        return orgWire(null, status: 404);
      });
      addTearDown(c.close);
      await t.pumpWidget(
        orgMapApp(c, MemoryOrganizationControlPendingStore(), cityID: ''),
      );
      await orgSettle(t);
      final old = t
          .widget<TextButton>(
            find.byKey(const Key('organization_approve_$orgTestProposal')),
          )
          .onPressed!;
      version = 3;
      await orgTap(t, find.text('重新读取位置'));
      old();
      await orgSettle(t);
      expect(previews, 0);
      expect(find.textContaining('提案版本 3'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
  testWidgets(
    '139 UNKNOWN restart reads original operation despite current map403, stop preserves fact',
    (t) async {
      orgNormalView(t);
      const input = OrganizationControlInput(
        action: 'PROPOSE',
        operationID: orgTestOp,
        expectedAuthorityVersion: 4,
        cityID: 'aberdeen-gb',
        latitude: 57.2,
        longitude: -2.2,
        expectedCanonicalRevision: 3,
      );
      final s = input.selector(orgTestID),
          store = MemoryOrganizationControlPendingStore();
      await store.write(
        orgTestBase,
        orgTestOwner,
        orgTestID,
        OrganizationControlPending(s),
      );
      var puts = 0, operationReads = 0, stops = 0;
      final c = MockClient((r) async {
        if (r.method == 'PUT') puts++;
        if (r.url.path.endsWith('/stop')) {
          stops++;
          expectSync(jsonDecode(r.body), {'expectedAuthorityVersion': 4});
          return orgWire(orgReceiptWire(s, stopped: true));
        }
        if (r.url.path.contains('/operations/')) {
          operationReads++;
          return orgWire(null, status: 404);
        }
        return orgWire(null, status: 403);
      });
      addTearDown(c.close);
      await t.pumpWidget(orgMapApp(c, store, cityID: ''));
      await orgSettle(t);
      expect(
        find.byKey(const Key('organization_verify_operation')),
        findsOneWidget,
      );
      expect(find.byKey(const Key('organization_preview_point')), findsNothing);
      await orgTap(t, find.byKey(const Key('organization_verify_operation')));
      expect(operationReads, 1);
      expect(store.values, isNotEmpty);
      expect(puts, 0);
      await orgCapture(t, 'map-original-unknown403-normal390');
      await orgTap(t, find.byKey(const Key('organization_stop_operation')));
      expect(stops, 1);
      expect(puts, 0);
      expect(store.values, isEmpty);
      expect(find.text('原操作已停止。'), findsOneWidget);
      expect(find.byKey(const Key('organization_preview_point')), findsNothing);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
  testWidgets(
    '139 original success remains known when subsequent current map404',
    (t) async {
      orgNormalView(t);
      const input = OrganizationControlInput(
        action: 'HIDE_POINT',
        operationID: orgTestOp,
        expectedAuthorityVersion: 4,
        expectedCanonicalRevision: 3,
      );
      final s = input.selector(orgTestID),
          store = MemoryOrganizationControlPendingStore();
      await store.write(
        orgTestBase,
        orgTestOwner,
        orgTestID,
        OrganizationControlPending(s),
      );
      final c = MockClient(
        (r) async => r.url.path.contains('/operations/')
            ? orgWire(orgReceiptWire(s))
            : orgWire(null, status: 404),
      );
      addTearDown(c.close);
      await t.pumpWidget(orgMapApp(c, store, cityID: ''));
      await orgSettle(t);
      await orgTap(t, find.byKey(const Key('organization_verify_operation')));
      expect(store.values, isEmpty);
      expect(find.text('原操作已完成。'), findsOneWidget);
      expect(
        find.byKey(const Key('organization_canonical_status')),
        findsNothing,
      );
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
  testWidgets(
    '139 map expiry clears sourced proposal while retaining owner local draft for next read',
    (t) async {
      orgNormalView(t);
      var previews = 0;
      final c = MockClient((r) async {
        if (r.url.path.endsWith('/preview')) previews++;
        return orgWire(
          orgMapWire(lease: 3, proposals: [orgProposalWire(withdraw: true)]),
        );
      });
      addTearDown(c.close);
      await t.pumpWidget(orgMapApp(c, MemoryOrganizationControlPendingStore()));
      await orgSettle(t);
      await orgCoordinates(t);
      final old = t
          .widget<FilledButton>(
            find.byKey(const Key('organization_preview_point')),
          )
          .onPressed!;
      await t.pump(const Duration(seconds: 4));
      await t.pump();
      old();
      await orgSettle(t);
      expect(previews, 0);
      expect(
        find.byKey(const Key('organization_proposal_$orgTestProposal')),
        findsNothing,
      );
      await orgCapture(t, 'map-expired-source-normal390');
      await orgTap(t, find.text('重新读取位置'));
      expect(
        t
            .widget<TextField>(find.byKey(const Key('organization_latitude')))
            .controller!
            .text,
        '57.2',
      );
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
  testWidgets(
    '139 actor ABA removes source and coordinates, captured button never sends',
    (t) async {
      orgNormalView(t);
      final signal = ValueNotifier(0);
      addTearDown(signal.dispose);
      var owner = orgTestOwner, writes = 0;
      final c = MockClient((r) async {
        if (r.method != 'GET') writes++;
        return orgWire(orgMapWire());
      });
      addTearDown(c.close);
      await t.pumpWidget(
        orgMapApp(
          c,
          MemoryOrganizationControlPendingStore(),
          signal: signal,
          owner: () => owner,
        ),
      );
      await orgSettle(t);
      await orgCoordinates(t);
      final old = t
          .widget<FilledButton>(
            find.byKey(const Key('organization_preview_point')),
          )
          .onPressed!;
      owner = orgTestTarget;
      signal.value++;
      owner = orgTestOwner;
      signal.value++;
      await t.pump();
      old();
      await orgSettle(t);
      expect(writes, 0);
      expect(
        find.byKey(const Key('organization_canonical_status')),
        findsNothing,
      );
      await orgTap(t, find.text('重新读取位置'));
      expect(
        t
            .widget<TextField>(find.byKey(const Key('organization_latitude')))
            .controller!
            .text,
        '',
      );
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
  testWidgets(
    '139 AMap unavailable real cancel leaves manual coordinates unchanged',
    (t) async {
      orgNormalView(t);
      final c = MockClient((r) async => orgWire(orgMapWire()));
      addTearDown(c.close);
      final city = PublicCity.fromJson({
        'id': 'aberdeen-gb',
        'name': '合成城市',
        'region': '隔离测试',
        'contentStatus': 'ready',
        'source': {
          'label': '隔离样例',
          'reference': 'fixture',
          'maintainer': 'test',
          'freshness': 'fixture',
        },
        'map': {
          'provider': 'amap',
          'latitude': 57.1,
          'longitude': -2.1,
          'defaultZoom': 10.0,
          'sourceRef': 'fixture',
        },
      });
      await t.pumpWidget(
        orgMapApp(c, MemoryOrganizationControlPendingStore(), city: city),
      );
      await orgSettle(t);
      await orgCoordinates(t);
      await orgTap(t, find.byKey(const Key('organization_pick_coordinate')));
      expect(find.textContaining('尚不支持可核对的 WGS84'), findsOneWidget);
      await orgTap(t, find.text('取消选点'));
      expect(
        t
            .widget<TextField>(find.byKey(const Key('organization_latitude')))
            .controller!
            .text,
        '57.2',
      );
      expect(
        t
            .widget<TextField>(find.byKey(const Key('organization_longitude')))
            .controller!
            .text,
        '-2.2',
      );
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
}
