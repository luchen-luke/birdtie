import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/workspace/organization_membership_api.dart';
import 'package:birdtie_client/src/workspace/organization_membership_pages.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const orgID = '11111111-1111-4111-8111-111111111111';
const ownerID = '22222222-2222-4222-8222-222222222222';
const targetID = '33333333-3333-4333-8333-333333333333';

Map<String, dynamic> member(
  String id,
  String account,
  String name,
  String role,
  String status,
) => {
  'id': id,
  'organizationId': orgID,
  'userAccountId': account,
  'displayName': name,
  'role': role,
  'status': status,
};

void main() {
  testWidgets('owner sees Chinese roster and sends account-bound invitation', (
    tester,
  ) async {
    final roster = <Map<String, dynamic>>[
      member(
        'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
        ownerID,
        '组织负责人',
        'owner',
        'active',
      ),
    ];
    var invited = false;
    final client = MockClient((request) async {
      expect(request.headers['Authorization'], 'Bearer test');
      if (request.method == 'GET') {
        return http.Response.bytes(
          utf8.encode(jsonEncode({'data': roster})),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }
      if (request.method == 'POST') {
        final body = jsonDecode(request.body) as Map<String, dynamic>;
        expect(body, {'userAccountId': targetID, 'role': 'member'});
        roster.add(
          member(
            'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
            targetID,
            '受邀成员',
            'member',
            'invited',
          ),
        );
        invited = true;
        return http.Response.bytes(
          utf8.encode(jsonEncode({'data': roster.last})),
          201,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }
      return http.Response('', 404);
    });
    final api = OrganizationMembershipApi(
      authorizationHeader: () => 'Bearer test',
      client: client,
      apiBaseUrl: 'http://127.0.0.1:8080',
    );
    await tester.pumpWidget(
      MaterialApp(
        home: OrganizationMembersPage(
          organizationID: orgID,
          organizationName: '本地测试组织',
          currentRole: 'owner',
          authorizationHeader: () => 'Bearer test',
          api: api,
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('组织负责人'), findsOneWidget);
    await tester.tap(find.byKey(const Key('organization_invite_member')));
    await tester.pumpAndSettle();
    await tester.enterText(
      find.byKey(const Key('member_account_id')),
      targetID,
    );
    await tester.tap(find.text('发送站内邀请'));
    await tester.pumpAndSettle();
    expect(invited, isTrue);
    expect(find.text('受邀成员'), findsOneWidget);
    expect(find.textContaining('待接受'), findsOneWidget);
    api.dispose();
  });

  testWidgets('invitee accepts a targeted invitation in Chinese UI', (
    tester,
  ) async {
    final authClient = MockClient((request) async {
      if (request.url.path == '/v1/me') {
        return http.Response('{"data":{"id":"$targetID"}}', 200);
      }
      if (request.url.path == '/v1/auth/dev-phone/status') {
        return http.Response('{"data":{"enabled":false}}', 200);
      }
      return http.Response('', 404);
    });
    final auth = BirdtieAuthController(
      client: authClient,
      apiBaseUrl: 'http://127.0.0.1:8080',
      sessionVault: MemorySessionVault()
        ..session = const StoredSession(token: 'test', method: 'oidc'),
    );
    await auth.initialize();
    var accepted = false;
    var refreshed = false;
    final api = OrganizationMembershipApi(
      authorizationHeader: () => auth.authorizationHeader,
      client: MockClient((request) async {
        if (request.method == 'GET') {
          return http.Response.bytes(
            utf8.encode(
              jsonEncode({
                'data': accepted
                    ? []
                    : [
                        {
                          ...member(
                            'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
                            targetID,
                            '受邀成员',
                            'member',
                            'invited',
                          ),
                          'organizationName': '本地测试组织',
                        },
                      ],
              }),
            ),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        }
        if (request.method == 'POST') {
          accepted = true;
          return http.Response.bytes(
            utf8.encode(
              jsonEncode({
                'data': member(
                  'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
                  targetID,
                  '受邀成员',
                  'member',
                  'active',
                ),
              }),
            ),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        }
        return http.Response('', 404);
      }),
      apiBaseUrl: 'http://127.0.0.1:8080',
    );
    await tester.pumpWidget(
      MaterialApp(
        home: OrganizationInvitationsPage(
          auth: auth,
          api: api,
          onAccepted: () async {
            refreshed = true;
          },
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('本地测试组织'), findsOneWidget);
    expect(find.byKey(const Key('my_account_id')), findsOneWidget);
    await tester.tap(find.text('接受邀请'));
    await tester.pumpAndSettle();
    expect(accepted, isTrue);
    expect(refreshed, isTrue);
    expect(find.text('目前没有待处理的组织邀请。'), findsOneWidget);
    api.dispose();
    auth.dispose();
  });
}
