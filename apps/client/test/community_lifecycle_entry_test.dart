import 'package:birdtie_client/src/workspace/community_api.dart';
import 'package:birdtie_client/src/workspace/community_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'community_members_controller_test.dart';
import 'entity_action_contract_test.dart' show actionWire;
import 'package:birdtie_client/src/workspace/entity_action_contract.dart';

void main() {
  for (final visibility in ['private', 'public']) {
    for (final operation in [
      'LEAVE',
      'DECLINE_INVITATION',
      'ACCEPT_INVITATION',
    ]) {
      for (final confirm in [true, false]) {
        final invited = operation != 'LEAVE';
        final entry = invited
            ? (operation == 'ACCEPT_INVITATION' ? '接受邀请' : '拒绝邀请')
            : '撤回申请';
        final approval = switch (operation) {
          'ACCEPT_INVITATION' => '确认接受邀请',
          'DECLINE_INVITATION' => '确认拒绝邀请',
          _ => '确认撤回申请',
        };
        testWidgets('原详情$visibility本人$entry具体批准$confirm复用领域动作', (t) async {
          final h = CommunityHarness();
          var status = invited ? 'invited' : 'pending';
          final calls = <http.Request>[];
          final now = DateTime.now().toUtc();
          final until = now.add(const Duration(seconds: 25));
          final api = CommunityApi(
            authorizationHeader: () => h.token,
            apiBaseUrl: 'https://local.synthetic.invalid',
            client: MockClient((r) async {
              calls.add(r);
              if (r.url.path.contains('/entity-actions/')) {
                expect(r.method, 'GET');
                expect(
                  r.url.path,
                  '/v1/me/entity-actions/community/$commGroup',
                );
                expect(r.headers['Authorization'], h.token);
                final view = actionWire(
                  ref: const EntityActionRef('community', commGroup),
                  now: now,
                );
                view['title'] = h.group['name'];
                for (final dynamic action in view['actions'] as List) {
                  action['state'] = action['kind'] == 'JOIN'
                      ? 'AVAILABLE'
                      : 'UNAVAILABLE';
                }
                final join = view['actions'][3];
                join['operation'] = invited ? 'ACCEPT_INVITATION' : 'LEAVE';
                join['allowedOperations'] = invited
                    ? ['ACCEPT_INVITATION', 'DECLINE_INVITATION']
                    : ['LEAVE'];
                join['label'] = invited ? '接受邀请' : '撤回申请';
                return h.json(view);
              }
              if (r.method == 'POST') {
                expect(r.headers['Authorization'], h.token);
                expect(r.headers['X-Birdtie-Action-Version'], 'a' * 64);
                expect(
                  r.headers['X-Birdtie-Action-Until'],
                  until.toIso8601String(),
                );
                expect(r.headers['X-Birdtie-Action-Operation'], operation);
                // Existing writers take the exact condition in headers, not a new body contract.
                expect(r.body, isEmpty);
                if (operation == 'ACCEPT_INVITATION') {
                  expect(r.url.path, '/v1/communities/$commGroup/join');
                  status = 'active';
                  return h.json({'communityId': commGroup, 'status': status});
                }
                expect(r.url.path, '/v1/communities/$commGroup/leave');
                status = 'left';
                return http.Response('', 204);
              }
              if (r.url.path.endsWith('/members')) {
                expect(status, 'active');
                return h.json(<Object>[]);
              }
              expect(r.url.path, '/v1/communities/$commGroup');
              return h.json({
                ...h.group,
                'visibility': visibility,
                'myStatus': status,
                'myRole': null,
              });
            }),
          );
          addTearDown(h.dispose);
          addTearDown(api.dispose);
          await t.pumpWidget(
            MaterialApp(
              home: CommunityDetailPage(id: commGroup, api: api),
            ),
          );
          await t.pumpAndSettle();
          expect(find.text(entry), findsOneWidget);
          if (invited) {
            expect(find.text('接受邀请'), findsOneWidget);
            expect(find.text('拒绝邀请'), findsOneWidget);
          }
          await t.tap(find.text(entry));
          await t.pumpAndSettle();
          expect(find.text(approval), findsOneWidget);
          expect(calls.where((r) => r.method != 'GET'), isEmpty);
          await t.tap(find.text(confirm ? approval : '取消'));
          await t.pumpAndSettle();
          final writes = calls.where((r) => r.method != 'GET').toList();
          expect(writes, hasLength(confirm ? 1 : 0));
          if (confirm) {
            expect(
              writes.single.url.path,
              '/v1/communities/$commGroup/${operation == 'ACCEPT_INVITATION' ? 'join' : 'leave'}',
            );
            expect(
              status,
              operation == 'ACCEPT_INVITATION' ? 'active' : 'left',
            );
            expect(
              calls.where((r) => r.url.path.contains('/entity-actions/')),
              hasLength(2),
            );
          } else {
            expect(status, invited ? 'invited' : 'pending');
          }
          expect(calls.where((r) => r.url.path.endsWith('/requests')), isEmpty);
          expect(
            calls.where((r) => r.url.path.endsWith('/members')),
            hasLength(confirm && operation == 'ACCEPT_INVITATION' ? 1 : 0),
          );
          expect(t.takeException(), isNull);
          await t.pumpWidget(const SizedBox());
        });
      }
    }
  }
  testWidgets('原详情无真人账号时不显示管理批准入口', (t) async {
    final h = CommunityHarness();
    addTearDown(h.dispose);
    await t.pumpWidget(
      MaterialApp(
        home: CommunityDetailPage(id: commGroup, api: h.api),
      ),
    );
    await t.pumpAndSettle();
    expect(find.byKey(const Key('community_member_management')), findsNothing);
    expect(find.text('归档社群'), findsNothing);
    expect(h.writes, 0);
  });
}
