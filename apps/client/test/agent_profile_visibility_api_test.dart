import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/agent_profile_visibility_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

const audienceOwner = '80000000-0000-4000-8000-000000000001';
const audienceAgent = '80000000-0000-4000-8000-000000000002';
const audienceCommunity = '80000000-0000-4000-8000-000000000003';
const audienceOtherCommunity = '80000000-0000-4000-8000-000000000004';
Map<String, dynamic> audienceWire({
  int version = 1,
  Map<String, Object>? rules,
  bool configured = false,
}) => {
  'schemaVersion': 'agent-profile-visibility-v1',
  'profile': {
    'agentId': audienceAgent,
    'ownerType': 'PERSON',
    'ownerId': audienceOwner,
    'profileVersion': version,
    'createdAt': '2026-10-01T00:00:00Z',
    'updatedAt': '2026-10-01T00:00:00Z',
  },
  'configured': configured,
  'rules':
      rules ??
      {
        for (final key in profileVisibilityLabels.keys)
          key: {
            'visibility': const {'displayName', 'bio'}.contains(key)
                ? 'PUBLIC'
                : 'PRIVATE',
            'communityIds': <String>[],
          },
      },
};
Map<String, Object?> audienceCommunityRow(
  String id,
  String name,
  String status,
) => {
  'id': id,
  'name': name,
  'description': '',
  'visibility': 'hidden',
  'joinPolicy': 'request',
  'status': 'active',
  'memberCount': 2,
  'myRole': status == 'active' ? 'member' : null,
  'myStatus': status,
  'createdByUserId': audienceOwner,
  'createdAt': '2026-10-01T00:00:00Z',
  'updatedAt': '2026-10-01T00:00:00Z',
};
http.StreamedResponse audienceResponse(Object data, {int status = 200}) =>
    http.StreamedResponse(
      Stream.value(utf8.encode(jsonEncode({'data': data}))),
      status,
    );

class AudienceWireClient extends http.BaseClient {
  AudienceWireClient(this.reply);
  final FutureOr<http.StreamedResponse> Function(http.BaseRequest) reply;
  final sent = <http.BaseRequest>[];
  bool closed = false;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest r) {
    sent.add(r);
    return Future.value(reply(r));
  }

  @override
  void close() {
    closed = true;
    super.close();
  }
}

void main() {
  test('真实十一字段默认形状只读，不复制私人内容或授予模型', () async {
    final c = AudienceWireClient((_) => audienceResponse(audienceWire()));
    final api = AgentProfileVisibilityAPI(
      client: c,
      apiBaseUrl: 'https://audience.test',
    );
    final value = await api.read('Bearer synthetic', audienceOwner);
    expect(value.rules.length, 11);
    expect(value.rules['displayName']!.visibility, 'PUBLIC');
    expect(value.rules['agentNotes']!.visibility, 'PRIVATE');
    expect(value.configured, false);
    expect(c.sent.single.method, 'GET');
    expect(c.sent.single.url.path, '/v1/me/agent-profile-visibility');
    expect(c.sent.single.url.hasQuery, false);
    expect(
      c.sent.single.headers.containsKey('X-Birdtie-Organization-Workspace'),
      false,
    );
    api.dispose();
    expect(c.closed, false);
  });
  test('五枚举与完整规则闭集拒绝未知字段错本人错误目标', () {
    for (final audience in profileAudienceLabels.keys) {
      final rule = ProfileFieldAudience(
        audience,
        audience == 'COMMUNITY' ? [audienceCommunity] : [],
      );
      expect(rule.visibility, audience);
    }
    expect(
      () => ProfileVisibilityRecord.read({
        ...audienceWire(),
        'modelAccess': true,
      }, audienceOwner),
      throwsFormatException,
    );
    final foreign = audienceWire();
    (foreign['profile'] as Map)['ownerId'] = audienceAgent;
    expect(
      () => ProfileVisibilityRecord.read(foreign, audienceOwner),
      throwsFormatException,
    );
    final missing = audienceWire();
    (missing['rules'] as Map).remove('agentNotes');
    expect(
      () => ProfileVisibilityRecord.read(missing, audienceOwner),
      throwsFormatException,
    );
    expect(() => ProfileFieldAudience('FRIENDS'), throwsFormatException);
    expect(
      () => ProfileFieldAudience('PRIVATE', [audienceCommunity]),
      throwsFormatException,
    );
    expect(() => ProfileFieldAudience('COMMUNITY', []), throwsFormatException);
    expect(
      () => ProfileFieldAudience('COMMUNITY', [
        audienceCommunity,
        audienceCommunity,
      ]),
      throwsFormatException,
    );
    final mismatchedDefault = audienceWire();
    (mismatchedDefault['rules'] as Map)['agentNotes'] = {
      'visibility': 'PUBLIC',
      'communityIds': [],
    };
    expect(
      () => ProfileVisibilityRecord.read(mismatchedDefault, audienceOwner),
      throwsFormatException,
    );
  });
  test('完整PUT仅原expectedVersion与十一规则，真实返回版本及规则必须一致', () async {
    final initial = ProfileVisibilityRecord.read(audienceWire(), audienceOwner);
    final rules = {
      ...initial.rules,
      'agentNotes': ProfileFieldAudience('AGENT_ONLY'),
    };
    final c = AudienceWireClient((r) {
      final body = jsonDecode((r as http.Request).body) as Map;
      expect(body.keys.toSet(), {'expectedVersion', 'rules'});
      expect(body['expectedVersion'], 1);
      expect((body['rules'] as Map).length, 11);
      expect(r.headers['Authorization'], 'Bearer synthetic');
      return audienceResponse(
        audienceWire(
          version: 2,
          rules: profileRulesWire(rules),
          configured: true,
        ),
      );
    });
    final api = AgentProfileVisibilityAPI(
      client: c,
      apiBaseUrl: 'https://audience.test',
    );
    final result = await api.replace('Bearer synthetic', initial, rules);
    expect(result.version, 2);
    expect(c.sent.length, 1);
    expect(c.sent.single.method, 'PUT');
    api.dispose();
    final bad = AgentProfileVisibilityAPI(
      client: AudienceWireClient((_) => audienceResponse(audienceWire())),
      apiBaseUrl: 'https://audience.test',
    );
    await expectLater(
      bad.replace('Bearer synthetic', initial, rules),
      throwsFormatException,
    );
    bad.dispose();
  });
  test('本人社群真实mine只筛active成员，不把邀请申请当资格', () async {
    final c = AudienceWireClient((r) {
      expect(r.url.path, '/v1/me/social-communities');
      return audienceResponse([
        audienceCommunityRow(audienceCommunity, '已加入社群', 'active'),
        audienceCommunityRow(audienceOtherCommunity, '待邀请社群', 'invited'),
        audienceCommunityRow(audienceAgent, '申请中社群', 'pending'),
      ]);
    });
    final api = AgentProfileVisibilityAPI(
      client: c,
      apiBaseUrl: 'https://audience.test',
    );
    expect(
      (await api.communities('Bearer synthetic', () => true)).map((r) => r.id),
      [audienceCommunity],
    );
    api.dispose();
    expect(c.closed, false);
  });
  test('HTTP状态与有界解析准确失败，失效入口零社群GET', () async {
    final c = AudienceWireClient((_) => audienceResponse({}, status: 409));
    final api = AgentProfileVisibilityAPI(
      client: c,
      apiBaseUrl: 'https://audience.test',
    );
    await expectLater(
      api.read('Bearer synthetic', audienceOwner),
      throwsA(
        isA<ProfileVisibilityHTTPError>().having(
          (e) => e.status,
          'status',
          409,
        ),
      ),
    );
    await expectLater(
      api.communities('Bearer synthetic', () => false),
      throwsFormatException,
    );
    expect(c.sent.length, 1);
    api.dispose();
    final large = AgentProfileVisibilityAPI(
      client: AudienceWireClient(
        (_) => http.StreamedResponse(Stream.value(List.filled(65537, 32)), 200),
      ),
      apiBaseUrl: 'https://audience.test',
    );
    await expectLater(
      large.read('Bearer synthetic', audienceOwner),
      throwsFormatException,
    );
    large.dispose();
  });
}
