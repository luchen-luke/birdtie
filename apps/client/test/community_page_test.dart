import 'package:birdtie_client/src/workspace/entity_action_contract.dart';
import 'entity_action_contract_test.dart' show actionWire;
import 'chat_entity_router_test.dart' show ChatTestAuth;
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/community_api.dart';
import 'package:birdtie_client/src/workspace/community_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  for (final decision in [
    'cancelDecline',
    'decline',
    'accept',
    'withdrawDecline',
    'identityABA',
  ]) {
    testWidgets('社群邀请具体选择 $decision 不隐式加入或重复提交', (t) async {
      const id = '11111111-1111-4111-8111-111111111111';
      final auth = ChatTestAuth();
      var joins = 0, leaves = 0, reads = 0;
      var status = 'invited';
      final now = DateTime.now().toUtc();
      final until = now.add(const Duration(seconds: 25));
      final client = MockClient((r) async {
        if (r.url.path.contains('/entity-actions/')) {
          reads++;
          final d = actionWire(
            ref: const EntityActionRef('community', id),
            now: now,
          );
          for (final dynamic a in d['actions'] as List) {
            a['state'] = a['kind'] == 'JOIN' ? 'AVAILABLE' : 'UNAVAILABLE';
          }
          final a = d['actions'][3];
          a['operation'] = 'ACCEPT_INVITATION';
          a['allowedOperations'] = decision == 'withdrawDecline' && reads > 1
              ? ['ACCEPT_INVITATION']
              : ['ACCEPT_INVITATION', 'DECLINE_INVITATION'];
          return http.Response.bytes(utf8.encode(jsonEncode({'data': d})), 200);
        }
        if (r.method == 'POST') {
          expect(r.headers['X-Birdtie-Action-Version'], 'a' * 64);
          expect(r.headers['X-Birdtie-Action-Until'], until.toIso8601String());
          if (r.url.path.endsWith('/join')) {
            joins++;
            status = 'active';
            expect(
              r.headers['X-Birdtie-Action-Operation'],
              'ACCEPT_INVITATION',
            );
            return http.Response(
              jsonEncode({
                'data': {'communityId': id, 'status': 'active'},
              }),
              200,
            );
          }
          leaves++;
          status = 'left';
          expect(r.url.path.endsWith('/leave'), isTrue);
          expect(r.headers['X-Birdtie-Action-Operation'], 'DECLINE_INVITATION');
          return http.Response('', 204);
        }
        if (r.url.path.endsWith('/members')) {
          return http.Response('{"data":[]}', 200);
        }
        return http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': {
                'id': id,
                'name': '合成邀请圈',
                'description': '已授权测试资料',
                'visibility': 'private',
                'joinPolicy': 'invite_only',
                'status': 'active',
                'memberCount': 2,
                'myRole': 'member',
                'myStatus': status,
              },
            }),
          ),
          200,
        );
      });
      final api = CommunityApi(
        authorizationHeader: () => auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'https://api.test',
      );
      await t.pumpWidget(
        MaterialApp(
          home: CommunityDetailPage(id: id, api: api, auth: auth),
        ),
      );
      await t.pumpAndSettle();
      final accepting = decision == 'accept';
      await t.ensureVisible(find.text(accepting ? '接受邀请' : '拒绝邀请'));
      await t.tap(find.text(accepting ? '接受邀请' : '拒绝邀请'));
      await t.pumpAndSettle();
      final confirm = accepting ? '确认接受邀请' : '确认拒绝邀请';
      expect(find.text(confirm), findsOneWidget);
      expect(joins + leaves, 0);
      if (decision == 'cancelDecline') {
        await t.tap(find.text('取消'));
      } else if (decision == 'identityABA') {
        auth.use('Bearer B');
        await t.pump();
        auth.use('Bearer A');
        await t.pump();
        final button = t.widget<FilledButton>(
          find.ancestor(
            of: find.text(confirm),
            matching: find.byType(FilledButton),
          ),
        );
        expect(button.onPressed, isNull);
        await t.tap(find.text('取消'));
      } else {
        await t.tap(find.text(confirm));
      }
      await t.pumpAndSettle();
      expect(joins, accepting ? 1 : 0);
      expect(leaves, decision == 'decline' ? 1 : 0);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      api.dispose();
      client.close();
      auth.dispose();
    });
  }
  testWidgets('中文社群页显示管理、加入和申请状态', (tester) async {
    final authClient = MockClient((request) async {
      switch ('${request.method} ${request.url.path}') {
        case 'GET /v1/auth/dev-phone/status':
          return http.Response('{"data":{"enabled":true}}', 200);
        case 'POST /v1/auth/dev-phone/code':
          return http.Response('{"data":{"expiresInSeconds":300}}', 200);
        case 'POST /v1/auth/dev-phone/verify':
          return http.Response('{"data":{"accessToken":"test-session"}}', 200);
        case 'GET /v1/me':
          return http.Response('{"data":{"id":"account-1"}}', 200);
        case 'GET /v1/accounts/account-1/profile':
          return http.Response('{"data":{"displayName":"测试用户"}}', 200);
        default:
          return http.Response('', 404);
      }
    });
    final auth = BirdtieAuthController(
      client: authClient,
      apiBaseUrl: 'http://localhost:8080',
      sessionVault: MemorySessionVault(),
    );
    await auth.initialize();
    await auth.requestDevPhoneCode('13800138000');
    await auth.verifyDevPhoneCode('13800138000', '123456');
    expect(auth.signedIn, isTrue);
    final communities = [
      {
        'id': '11111111-1111-4111-8111-111111111111',
        'name': '我管理的圈子',
        'description': '一起活动',
        'visibility': 'public',
        'joinPolicy': 'request',
        'status': 'active',
        'memberCount': 3,
        'myRole': 'owner',
        'myStatus': 'active',
      },
      {
        'id': '22222222-2222-4222-8222-222222222222',
        'name': '我加入的圈子',
        'description': '一起学习',
        'visibility': 'private',
        'joinPolicy': 'open',
        'status': 'active',
        'memberCount': 5,
        'myRole': 'member',
        'myStatus': 'active',
      },
      {
        'id': '33333333-3333-4333-8333-333333333333',
        'name': '等待审批的圈子',
        'description': '',
        'visibility': 'private',
        'joinPolicy': 'request',
        'status': 'active',
        'memberCount': 2,
        'myRole': null,
        'myStatus': 'pending',
      },
    ];
    final api = CommunityApi(
      authorizationHeader: () => auth.authorizationHeader,
      apiBaseUrl: 'https://birdtie.example',
      client: MockClient((request) async {
        if (request.url.path == '/v1/me/social-communities') {
          return http.Response.bytes(
            utf8.encode(jsonEncode({'data': communities})),
            200,
          );
        }
        if (request.url.path == '/v1/communities') {
          return http.Response.bytes(
            utf8.encode(jsonEncode({'data': communities.take(2).toList()})),
            200,
          );
        }
        return http.Response('', 404);
      }),
    );
    final city = PublicCityController();
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: CommunityPage(auth: auth, city: city, api: api),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('我的社群'), findsOneWidget);
    expect(find.text('我管理的'), findsOneWidget);
    expect(find.text('我加入的'), findsOneWidget);
    expect(find.text('待处理'), findsOneWidget);
    expect(find.text('发现社群'), findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
    auth.dispose();
    city.dispose();
    api.dispose();
    authClient.close();
  });
}
