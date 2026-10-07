import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/content/social_preference_seed_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

// Offline wire fixtures only, not native identity/session authorization.
const socialOwner = '11111111-1111-4111-8111-111111111111';
const socialAgent = '22222222-2222-4222-8222-222222222222';
Map<String, dynamic> socialFields({
  List<String> preferences = const ['原有明确描述'],
}) => {
  'personalPreferences': ['原有兴趣'],
  'socialPreferences': preferences,
  'availability': '本人明确空闲描述',
  'preferredActivityTypes': ['原有活动'],
  'travelPreferences': ['原有出行'],
  'interactionPreferences': ['原有互动'],
  'privateCityHistory': '私密历史声明',
  'languagePreferences': ['zh-CN'],
  'agentNotes': '私密笔记不能当意图或许可',
};
Map<String, dynamic> socialJson({
  String owner = socialOwner,
  int version = 2,
  Map<String, dynamic>? fields,
}) => {
  'schemaVersion': 'private-agent-profile-v1',
  'configured': true,
  'profile': {
    'agentId': socialAgent,
    'ownerType': 'PERSON',
    'ownerId': owner,
    'profileVersion': version,
    'createdAt': '2026-10-01T00:00:00Z',
    'updatedAt': '2026-10-02T00:00:00Z',
  },
  'fields': fields ?? socialFields(),
};
http.Response socialResponse(Map<String, dynamic> record) => http.Response(
  jsonEncode({'data': record}),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);
void main() {
  test('原九字段冻结，只明确修改社交偏好；具体version随真实当前源提交', () async {
    Map<String, dynamic>? sent;
    final c = SocialPreferenceSeedController(
      authorizationHeader: () => 'Bearer owner',
      accountID: () => socialOwner,
      apiBaseUrl: 'http://local',
      client: MockClient((r) async {
        expect(r.url.path, '/v1/me/agent-private-profile');
        if (r.method == 'GET') return socialResponse(socialJson());
        sent = jsonDecode(r.body) as Map<String, dynamic>;
        return socialResponse(
          socialJson(
            version: 3,
            fields: sent!['fields'] as Map<String, dynamic>,
          ),
        );
      }),
    );
    addTearDown(c.dispose);
    expect(await c.load(), true);
    final r = c.record!;
    final choice = ['同一大学', '原有明确描述'];
    expect(() => r.preferences.add('隐含选择'), throwsUnsupportedError);
    expect(await c.save(r, choice), true);
    expect(sent!.keys.toSet(), {'expectedVersion', 'fields'});
    expect(sent!['expectedVersion'], 2);
    final fields = sent!['fields'] as Map<String, dynamic>;
    for (final key in socialFields().keys.where(
      (k) => k != 'socialPreferences',
    )) {
      expect(fields[key], socialFields()[key]);
    }
    expect(fields['socialPreferences'], choice);
    expect(fields.keys.length, 9);
    expect(await c.save(r, choice), false);
  });
  test('无改变不PUT，任意新增描述/重复选择不能冒充明确选项', () async {
    var writes = 0;
    final c = SocialPreferenceSeedController(
      authorizationHeader: () => 'Bearer owner',
      accountID: () => socialOwner,
      apiBaseUrl: 'http://local',
      client: MockClient((r) async {
        if (r.method == 'PUT') writes++;
        return socialResponse(socialJson());
      }),
    );
    addTearDown(c.dispose);
    await c.load();
    final r = c.record!;
    expect(await c.save(r, ['原有明确描述']), true);
    expect(writes, 0);
    expect(await c.save(r, ['verified university']), false);
    expect(await c.save(r, ['小群体', '小群体']), false);
    expect(writes, 0);
  });
  test('409重读保留草稿但旧源对象不可批准；对方的新其它字段不能覆盖', () async {
    var reads = 0, writes = 0;
    final c = SocialPreferenceSeedController(
      authorizationHeader: () => 'Bearer owner',
      accountID: () => socialOwner,
      apiBaseUrl: 'http://local',
      client: MockClient((r) async {
        if (r.method == 'PUT') {
          writes++;
          return http.Response('{}', 409);
        }
        reads++;
        return socialResponse(
          socialJson(
            version: reads == 1 ? 2 : 4,
            fields: {
              ...socialFields(),
              'agentNotes': reads == 1 ? '原笔记' : '另一窗口新笔记',
            },
          ),
        );
      }),
    );
    addTearDown(c.dispose);
    await c.load();
    final old = c.record!;
    expect(await c.save(old, ['小群体']), false);
    expect(c.conflict, true);
    expect(await c.save(old, ['小群体']), false);
    expect(writes, 1);
    expect(await c.load(), true);
    expect(c.record!.fields['agentNotes'], '另一窗口新笔记');
    expect(await c.save(old, ['小群体']), false);
    expect(writes, 1);
  });
  test('未知回复阻止盲重发，输入list晚变更不能改原审核，GET只报告当前一致', () async {
    final pending = Completer<http.Response>();
    var reads = 0, writes = 0;
    final wanted = socialFields(preferences: ['小群体']);
    final c = SocialPreferenceSeedController(
      authorizationHeader: () => 'Bearer owner',
      accountID: () => socialOwner,
      apiBaseUrl: 'http://local',
      client: MockClient((r) async {
        if (r.method == 'PUT') {
          writes++;
          return pending.future;
        }
        reads++;
        return socialResponse(
          socialJson(
            version: reads == 1 ? 2 : 3,
            fields: reads == 1 ? socialFields() : wanted,
          ),
        );
      }),
    );
    addTearDown(c.dispose);
    await c.load();
    final old = c.record!;
    final choice = ['小群体'];
    final saving = c.save(old, choice);
    choice.add('国际社群');
    pending.complete(http.Response('{}', 503));
    expect(await saving, false);
    expect(c.resultUnknown, true);
    expect(await c.save(old, ['小群体']), false);
    expect(writes, 1);
    expect(await c.load(), true);
    expect(c.message, contains('已核实'));
    expect(c.resultUnknown, false);
    expect(c.record!.preferences, ['小群体']);
    expect(writes, 1);
  });
  test('账户/组织ABA失效，迟到响应与dispose不能重建私密源', () async {
    String? token = 'Bearer owner', owner = socialOwner, workspace;
    final pending = Completer<http.Response>();
    final c = SocialPreferenceSeedController(
      authorizationHeader: () => token,
      accountID: () => owner,
      organizationWorkspaceID: () => workspace,
      apiBaseUrl: 'http://local',
      client: MockClient((_) => pending.future),
    );
    final loading = c.load();
    workspace = 'organization-entity';
    c.synchronizeIdentity();
    workspace = null;
    c.synchronizeIdentity();
    token = 'Bearer peer';
    owner = '33333333-3333-4333-8333-333333333333';
    c.synchronizeIdentity();
    token = 'Bearer owner';
    owner = socialOwner;
    c.synchronizeIdentity();
    pending.complete(socialResponse(socialJson()));
    expect(await loading, false);
    expect(c.record, isNull);
    c.dispose();
    expect(await c.load(), false);
  });
  test('403/foreign owner/未知shape不释放私密源，异常200保存仍为未知', () async {
    for (final mode in [
      '403',
      'foreign',
      'shape',
      'hiddenUnconfigured',
      'badSave',
    ]) {
      var count = 0;
      final c = SocialPreferenceSeedController(
        authorizationHeader: () => 'Bearer owner',
        accountID: () => socialOwner,
        apiBaseUrl: 'http://local',
        client: MockClient((r) async {
          count++;
          if (mode == '403') return http.Response('{}', 403);
          if (mode == 'foreign') {
            return socialResponse(socialJson(owner: 'peer'));
          }
          if (mode == 'shape') {
            return socialResponse({
              ...socialJson(),
              'fields': {'socialPreferences': []},
            });
          }
          if (mode == 'hiddenUnconfigured') {
            return socialResponse({...socialJson(), 'configured': false});
          }
          return socialResponse(socialJson(version: count == 1 ? 2 : 3));
        }),
      );
      if (mode == 'badSave') {
        expect(await c.load(), true);
        expect(await c.save(c.record!, ['小群体']), false);
        expect(c.resultUnknown, true);
      } else {
        expect(await c.load(), false);
        expect(c.record, isNull);
      }
      c.dispose();
    }
  });
}
