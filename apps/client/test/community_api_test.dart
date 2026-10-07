import 'package:birdtie_client/src/workspace/entity_action_contract.dart';
import 'entity_action_contract_test.dart' show actionWire;
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/community_api.dart';
import 'community_members_controller_test.dart';

void main() {
  test('邀请两个具体操作条件传原join/leave，同原版本期限不续签', () async {
    const id = '11111111-1111-4111-8111-111111111111';
    final raw = actionWire(ref: const EntityActionRef('community', id));
    raw['actions'][3]['operation'] = 'ACCEPT_INVITATION';
    raw['actions'][3]['allowedOperations'] = [
      'ACCEPT_INVITATION',
      'DECLINE_INVITATION',
    ];
    final view = EntityActionView.decode(
      raw,
      const EntityActionRef('community', id),
    );
    final seen = <http.Request>[];
    final client = MockClient((r) async {
      seen.add(r);
      return r.url.path.endsWith('/leave')
          ? http.Response('', 204)
          : http.Response(
              jsonEncode({
                'data': {'communityId': id, 'status': 'active'},
              }),
              200,
            );
    });
    final api = CommunityApi(
      authorizationHeader: () => 'Bearer a',
      client: client,
      apiBaseUrl: 'https://api.test',
    );
    await api.join(
      id,
      approved: view.action(EntityActionKind.join).reviewed(view),
    );
    await api.leave(
      id,
      approved: view
          .action(EntityActionKind.join)
          .selectOperation('DECLINE_INVITATION')
          .reviewed(view),
    );
    expect(seen.map((r) => r.headers['X-Birdtie-Action-Operation']), [
      'ACCEPT_INVITATION',
      'DECLINE_INVITATION',
    ]);
    for (final r in seen) {
      expect(r.headers['X-Birdtie-Action-Version'], view.sourceVersion);
      expect(
        r.headers['X-Birdtie-Action-Until'],
        view.validUntil.toIso8601String(),
      );
    }
    api.dispose();
    client.close();
  });
  for (final op in [
    'invite',
    'approve',
    'reject',
    'role',
    'remove',
    'transfer',
    'archive',
  ]) {
    test('接口fixture $op 使用现有路径具体预览后确认，未造真实许可', () async {
      final h = CommunityHarness();
      addTearDown(h.dispose);
      final action = switch (op) {
        'invite' => CommunityAction.invite(commGroup, commPeer),
        'approve' => CommunityAction.decide(
          commGroup,
          commMember,
          approve: true,
        ),
        'reject' => CommunityAction.decide(
          commGroup,
          commMember,
          approve: false,
        ),
        'role' => CommunityAction.role(commGroup, commMember, 'admin'),
        'remove' => CommunityAction.remove(commGroup, commMember),
        'transfer' => CommunityAction.transfer(commGroup, commPeer),
        _ => CommunityAction.archive(commGroup),
      };
      final preview = await h.api.preview(action, actorId: commOwner);
      expect(h.writes, 0);
      expect(h.previews, 1);
      await h.api.submit(preview);
      expect(h.writes, 1);
      expect(
        h.seen.last.headers['X-Birdtie-Community-Snapshot'],
        'SYNTHETIC_CONTRACT_ONLY',
      );
      expect(h.seen.last.method, action.method);
      expect(h.seen.last.url.path, action.path);
    });
  }
  const community = {
    'id': '11111111-1111-4111-8111-111111111111',
    'name': '阿伯丁羽毛球',
    'description': '周末一起打球',
    'visibility': 'private',
    'joinPolicy': 'request',
    'status': 'active',
    'memberCount': 12,
    'myRole': 'admin',
    'myStatus': 'active',
  };

  test('我的社群、公开发现和权限字段来自 API', () async {
    final paths = <String>[];
    final api = CommunityApi(
      authorizationHeader: () => 'Bearer test-token',
      apiBaseUrl: 'https://birdtie.example',
      client: MockClient((request) async {
        paths.add(request.url.path);
        expect(request.headers['Authorization'], 'Bearer test-token');
        return http.Response(
          jsonEncode({
            'data': [community],
          }),
          200,
          headers: {'content-type': 'application/json'},
        );
      }),
    );
    final mine = await api.mine();
    final discovered = await api.discover();
    expect(mine.single.managed, isTrue);
    expect(mine.single.name, '阿伯丁羽毛球');
    expect(discovered.single.joinPolicy, 'request');
    expect(paths, ['/v1/me/social-communities', '/v1/communities']);
    api.dispose();
  });

  test('申请加入和审批使用服务端接口，拒绝权限错误', () async {
    final paths = <String>[];
    final api = CommunityApi(
      authorizationHeader: () => 'Bearer test-token',
      apiBaseUrl: 'https://birdtie.example',
      client: MockClient((request) async {
        paths.add(request.url.path);
        if (request.url.path.endsWith('/requests')) {
          return http.Response('', 403);
        }
        return http.Response(
          jsonEncode({
            'data': {'communityId': community['id'], 'status': 'pending'},
          }),
          200,
        );
      }),
    );
    await api.join(community['id']! as String);
    expect(paths.single, endsWith('/join'));
    await expectLater(
      api.members(community['id']! as String, requests: true),
      throwsA(
        isA<CommunityApiException>().having((e) => e.status, 'status', 403),
      ),
    );
    api.dispose();
  });

  test('创建社群提交中文资料和独立的可见性、加入方式', () async {
    final api = CommunityApi(
      authorizationHeader: () => 'Bearer test-token',
      apiBaseUrl: 'https://birdtie.example',
      client: MockClient((request) async {
        expect(request.method, 'POST');
        expect(request.url.path, '/v1/communities');
        final body = jsonDecode(request.body) as Map<String, dynamic>;
        expect(body['name'], '阿伯丁羽毛球');
        expect(body['visibility'], 'private');
        expect(body['joinPolicy'], 'request');
        expect(body['cityId'], '');
        return http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': {...community, 'myRole': 'owner'},
            }),
          ),
          201,
        );
      }),
    );
    final created = await api.create(
      name: '阿伯丁羽毛球',
      description: '周末一起打球',
      visibility: 'private',
      joinPolicy: 'request',
    );
    expect(created.myRole, 'owner');
    api.dispose();
  });
}
