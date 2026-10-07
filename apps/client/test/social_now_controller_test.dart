import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/social_now_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const nowOwner = '00000000-0000-4000-8000-000000000001';
const nowFriend = '00000000-0000-4000-8000-000000000002';
String nowID(int n) =>
    '00000000-0000-4000-8000-${n.toString().padLeft(12, '0')}';
Map<String, dynamic> nowIntent(
  int n, {
  String owner = nowFriend,
  String audience = 'FRIENDS',
}) => {
  'id': nowID(n),
  'creatorAccountId': owner,
  'title': '好友想一起散步 $n',
  'type': 'FIND_COMPANION',
  'status': 'ACTIVE',
  'audience': audience,
  'modality': 'IN_PERSON',
  'expiresAt': '2099-01-01T00:00:00Z',
  'constraints': {'private': 'never project'},
};
Map<String, dynamic> nowActivity(int n, {String tier = 'EXISTING_TIE'}) => {
  'id': '${nowID(20)}:${nowID(n)}',
  'intentId': nowID(20),
  'entity': {'type': 'ACTIVITY', 'id': nowID(n)},
  'place': {'type': 'PLACE', 'id': nowID(21)},
  'action': {
    'type': 'OPEN_ACTIVITY',
    'target': {'type': 'ACTIVITY', 'id': nowID(n)},
  },
  'title': '活动 $n',
  'placeName': '公开地点',
  'startsAt': '2099-01-01T01:00:00Z',
  'ruleVersion': 'activity-place-v2',
  'routeTier': tier,
  'reasonCodes': [
    if (tier == 'EXISTING_TIE')
      'TIE_ORGANIZER'
    else if (tier == 'SHARED_COMMUNITY')
      'JOINED_COMMUNITY'
    else
      'UNKNOWN_REASON',
  ],
  'reason': 'raw secret should never show',
};
http.Response nowResponse(dynamic data, {bool opportunities = false}) =>
    http.Response(
      jsonEncode({
        'data': data,
        if (opportunities) 'source': 'RULE_BASED',
        if (opportunities) 'ruleVersion': 'activity-place-v2',
      }),
      200,
      headers: {'content-type': 'application/json'},
    );
http.Response nowEmpty(http.Request r) =>
    nowResponse([], opportunities: r.url.path == '/v1/me/opportunities');
Map<String, dynamic> nowTie() => {
  'id': nowID(3),
  'otherAccountId': nowFriend,
  'otherName': 'private label',
};

void main() {
  test('有限去重；好友公开当前意图与规则分组，不投影私密长期兴趣/原reason', () async {
    final calls = <String>[];
    final c = SocialNowController(
      authorizationHeader: () => 'Bearer a',
      accountID: () => nowOwner,
      apiBaseUrl: 'http://local',
      client: MockClient((r) async {
        calls.add(r.method);
        return switch (r.url.path) {
          '/v1/me/ties' => nowResponse([nowTie()]),
          '/v1/social-intents' => nowResponse([
            for (var n = 30; n < 42; n++) nowIntent(n),
            nowIntent(50, audience: 'PRIVATE'),
            nowIntent(51, owner: nowID(52)),
          ]),
          _ => nowResponse([
            for (var n = 60; n < 68; n++) nowActivity(n),
            nowActivity(60),
            nowActivity(70, tier: 'SHARED_COMMUNITY'),
            nowActivity(71, tier: 'PUBLIC'),
          ], opportunities: true),
        };
      }),
    );
    addTearDown(c.dispose);
    expect(await c.load(), isTrue);
    expect(c.intents.length, 10);
    expect(c.friendActivities.length, 5);
    expect(c.communityActivities.length, 1);
    expect(c.otherActivities.length, 1);
    expect(c.limited, isTrue);
    expect(c.otherActivities.single.reasons.join(), isNot(contains('secret')));
    expect(calls, everyElement('GET'));
  });
  test('闭集格式/来源/实体不一致、时区未知与过期不显示', () async {
    for (final malformed in [
      {...nowActivity(60), 'routeTier': 'SHARED_COMMUNITY'},
      {...nowActivity(60), 'ruleVersion': 'future'},
      {...nowActivity(60), 'startsAt': '2099-01-01T01:00:00'},
      {
        ...nowActivity(60),
        'action': {
          'type': 'OPEN_ACTIVITY',
          'target': {'type': 'ACTIVITY', 'id': nowID(61)},
        },
      },
    ]) {
      final c = SocialNowController(
        authorizationHeader: () => 'a',
        accountID: () => nowOwner,
        apiBaseUrl: 'http://local',
        client: MockClient(
          (r) async => r.url.path == '/v1/me/opportunities'
              ? nowResponse([malformed], opportunities: true)
              : nowEmpty(r),
        ),
      );
      expect(await c.load(), isFalse);
      expect(c.loaded, isFalse);
      expect(c.error, isNotNull);
      c.dispose();
    }
    expect(
      SocialNowIntent.read(
        {...nowIntent(30), 'expiresAt': '2026-01-01T00:00:00Z'},
        {nowFriend},
        DateTime.utc(2026, 1, 1),
      ),
      isNull,
    );
    expect(
      () => SocialNowIntent.read(
        {...nowIntent(30), 'modality': 'unknown'},
        {nowFriend},
        DateTime.utc(2026),
      ),
      throwsFormatException,
    );
  });
  test('账号/组织/ABA与迟到结果清空，不能复用旧读取', () async {
    String? token = 'a', workspace;
    final wait = Completer<http.Response>();
    final c = SocialNowController(
      authorizationHeader: () => token,
      accountID: () => nowOwner,
      organizationWorkspaceID: () => workspace,
      apiBaseUrl: 'http://local',
      client: MockClient(
        (r) async => r.url.path == '/v1/me/ties' ? wait.future : nowEmpty(r),
      ),
    );
    addTearDown(c.dispose);
    final pending = c.load();
    token = 'b';
    c.synchronizeIdentity();
    token = 'a';
    c.synchronizeIdentity();
    wait.complete(nowResponse([nowTie()]));
    expect(await pending, isFalse);
    expect(c.loaded, isFalse);
    workspace = 'organization';
    c.synchronizeIdentity();
    expect(await c.load(), isFalse);
    token = null;
    c.synchronizeIdentity();
    expect(c.intents, isEmpty);
  });
  test('好友意图点击重新验证关系/本人当前会话/具体source；撤回与失败清全部', () async {
    var ties = true;
    var calls = 0;
    final c = SocialNowController(
      authorizationHeader: () => 'a',
      accountID: () => nowOwner,
      apiBaseUrl: 'http://local',
      client: MockClient((r) async {
        calls++;
        if (r.url.path == '/v1/me/ties') {
          return nowResponse(ties ? [nowTie()] : []);
        }
        if (r.url.path == '/v1/social-intents') {
          return nowResponse([nowIntent(30)]);
        }
        if (r.url.path.endsWith(nowID(30))) return nowResponse(nowIntent(30));
        return nowEmpty(r);
      }),
    );
    addTearDown(c.dispose);
    await c.load();
    final signal = c.intents.single;
    expect((await c.readIntent(signal))?.id, signal.id);
    ties = false;
    expect(await c.readIntent(signal), isNull);
    expect(c.loaded, isFalse);
    expect(c.intents, isEmpty);
    final after = calls;
    expect(await c.readIntent(signal), isNull);
    expect(calls, after);
  });
  test('下游403整体失败关闭，无偷偷读取AgentProfile/关系授权', () async {
    final paths = <String>[];
    final c = SocialNowController(
      authorizationHeader: () => 'a',
      accountID: () => nowOwner,
      apiBaseUrl: 'http://local',
      client: MockClient((r) async {
        paths.add(r.url.path);
        return r.url.path == '/v1/me/ties'
            ? http.Response('', 403)
            : nowEmpty(r);
      }),
    );
    addTearDown(c.dispose);
    expect(await c.load(), isFalse);
    expect(paths.toSet(), {
      '/v1/me/ties',
      '/v1/social-intents',
      '/v1/me/opportunities',
    });
    expect(c.intents, isEmpty);
  });
}
