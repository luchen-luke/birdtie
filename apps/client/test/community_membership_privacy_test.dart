import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/community_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'community_members_controller_test.dart';

void main() {
  test('成员错误社群绑定拒绝且不返回另一社群名单', () async {
    final api = CommunityApi(
      authorizationHeader: () => 'Bearer SYNTHETIC_ONLY',
      apiBaseUrl: 'https://local.synthetic.invalid',
      client: MockClient(
        (_) async => http.Response(
          jsonEncode({
            'data': [
              {
                'id': commMember,
                'communityId': commPeer,
                'userAccountId': commPeer,
                'displayName': '不得显示私人姓名',
                'role': 'member',
                'status': 'active',
              },
            ],
          }),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        ),
      ),
    );
    addTearDown(api.dispose);
    await expectLater(
      api.members(commGroup),
      throwsA(
        isA<CommunityApiException>().having((e) => e.status, 'status', 503),
      ),
    );
  });
  test('详情换账号后的迟到响应被丢弃', () async {
    String? token = 'Bearer A';
    final gate = Completer<void>();
    final api = CommunityApi(
      authorizationHeader: () => token,
      apiBaseUrl: 'https://local.synthetic.invalid',
      client: MockClient((_) async {
        await gate.future;
        return http.Response(
          jsonEncode({
            'data': {
              'id': commGroup,
              'name': '旧主体私密社群',
              'description': '不得显示',
              'visibility': 'private',
              'joinPolicy': 'request',
              'status': 'active',
            },
          }),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }),
    );
    addTearDown(api.dispose);
    final pending = api.detail(commGroup);
    token = 'Bearer B';
    gate.complete();
    await expectLater(
      pending,
      throwsA(
        isA<CommunityApiException>().having((e) => e.status, 'status', 401),
      ),
    );
  });
  test('管理请求无具体批准不自动获得新批准或发送', () async {
    var sent = 0;
    final api = CommunityApi(
      authorizationHeader: () => 'Bearer A',
      apiBaseUrl: 'https://local.synthetic.invalid',
      client: MockClient((_) async {
        sent++;
        return http.Response('', 204);
      }),
    );
    addTearDown(api.dispose);
    await expectLater(
      api.archive(commGroup),
      throwsA(
        isA<CommunityApiException>().having((e) => e.status, 'status', 428),
      ),
    );
    await expectLater(
      api.decide(commGroup, commMember, approve: true),
      throwsA(
        isA<CommunityApiException>().having((e) => e.status, 'status', 428),
      ),
    );
    expect(sent, 0);
  });
  test('403角色撤回后不保留管理员名单或旧预览', () async {
    final h = CommunityHarness();
    addTearDown(h.dispose);
    await h.controller.reload();
    await h.controller.prepare(
      CommunityAction.remove(commGroup, commMember),
      target: '成员',
      consequence: '移除',
    );
    h.controller.invalidate();
    expect(h.controller.item, isNull);
    expect(h.controller.members, isEmpty);
    expect(h.controller.requests, isEmpty);
    expect(h.controller.proposal, isNull);
  });
}
