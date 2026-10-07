import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:birdtie_client/src/content/agent_seed_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

// Offline HTTP contract fixtures, not actual session/database authorization.
Map<String, dynamic> seedJson({
  String owner = 'owner',
  String snapshot = 'a',
  String progress = 'UNSET',
  String intent = '',
  String? city,
  List<String> languages = const [],
  List<String> interests = const [],
  String displayName = '原昵称',
}) => {
  'schemaVersion': 'personal-agent-seed-v1',
  'ownerId': owner,
  'agentId': 'agent',
  'snapshot': List.filled(64, snapshot).join(),
  'displayName': displayName,
  'profileVisibility': 'private',
  'currentCity': city == null
      ? null
      : {
          'id': city,
          'name': '阿伯丁',
          'sourceSnapshot': List.filled(64, 'b').join(),
        },
  'cities': [
    {
      'id': 'aberdeen-gb',
      'name': '阿伯丁',
      'sourceSnapshot': List.filled(64, 'b').join(),
    },
  ],
  'languagePreferences': languages,
  'interests': interests,
  'privateProfileVersion': 1,
  'userIntent': {
    'version': 0,
    'basicIntent': intent,
    'progress': progress,
    'interestChoice': 'SKIP',
    'createdAt': null,
    'updatedAt': null,
  },
  'needsPrompt': progress == 'UNSET',
};
http.Response seedResponse(Map<String, dynamic> value) => http.Response(
  jsonEncode({'data': value}),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);
Map<String, dynamic> seedChoices(AgentSeedRecord r) => {
  'action': 'SAVE',
  'displayName': '原昵称',
  'currentCityId': 'aberdeen-gb',
  'currentCitySnapshot': r.cities.first.snapshot,
  'languagePreferences': ['zh-CN'],
  'basicIntent': 'JUST_EXPLORE',
  'interestChoice': 'SKIP',
  'interests': <String>[],
};

class SeedOwnedHttpClient extends Fake implements HttpClient {
  int closes = 0;
  @override
  void close({bool force = false}) => closes++;
}

class SeedBorrowedClient extends MockClient {
  SeedBorrowedClient(super.handler);
  int closes = 0;
  @override
  void close() {
    closes++;
    super.close();
  }
}

void main() {
  test('owned transport只关闭一次，借用transport不关闭，退休controller不再发请求', () async {
    final owned = SeedOwnedHttpClient();
    HttpOverrides.runZoned(() {
      final c = AgentSeedController(
        authorizationHeader: () => 'Bearer owner',
        accountID: () => 'owner',
        apiBaseUrl: 'http://local',
      );
      c.dispose();
      c.dispose();
      c.synchronizeIdentity();
      expect(owned.closes, 1);
    }, createHttpClient: (_) => owned);
    var requests = 0;
    final borrowed = SeedBorrowedClient((_) async {
      requests++;
      return seedResponse(seedJson());
    });
    final c = AgentSeedController(
      authorizationHeader: () => 'Bearer owner',
      accountID: () => 'owner',
      apiBaseUrl: 'http://local',
      client: borrowed,
    );
    await c.load();
    final reviewed = c.record!;
    c.dispose();
    c.dispose();
    expect(await c.load(), false);
    expect(await c.save(reviewed, seedChoices(reviewed)), false);
    expect(requests, 1);
    expect(borrowed.closes, 0);
    borrowed.close();
  });

  test('晚PUT在账号ABA后不得恢复回执或批准，新GET不自动重发', () async {
    var token = 'Bearer owner', owner = 'owner';
    final pending = Completer<http.Response>();
    var writes = 0;
    final c = AgentSeedController(
      authorizationHeader: () => token,
      accountID: () => owner,
      apiBaseUrl: 'http://local',
      client: MockClient((r) async {
        if (r.method == 'PUT') {
          writes++;
          return pending.future;
        }
        return seedResponse(seedJson(owner: owner));
      }),
    );
    addTearDown(c.dispose);
    await c.load();
    final reviewed = c.record!;
    final saving = c.save(reviewed, seedChoices(reviewed));
    token = 'Bearer peer';
    owner = 'peer';
    c.synchronizeIdentity();
    token = 'Bearer owner';
    owner = 'owner';
    c.synchronizeIdentity();
    pending.complete(seedResponse(seedJson(progress: 'COMPLETED')));
    expect(await saving, false);
    expect(c.record, isNull);
    expect(c.message, isNull);
    expect(c.resultUnknown, false);
    await c.load();
    expect(await c.save(reviewed, seedChoices(reviewed)), false);
    expect(writes, 1);
  });

  test('未知保存按原身份隔离，切换和退休不自动PUT或恢复未知草稿', () async {
    var token = 'Bearer owner', owner = 'owner';
    var writes = 0;
    final c = AgentSeedController(
      authorizationHeader: () => token,
      accountID: () => owner,
      apiBaseUrl: 'http://local',
      client: MockClient((r) async {
        if (r.method == 'PUT') {
          writes++;
          return http.Response('{}', 503);
        }
        return seedResponse(seedJson(owner: owner));
      }),
    );
    await c.load();
    final reviewed = c.record!;
    await c.save(reviewed, seedChoices(reviewed));
    expect(c.resultUnknown, true);
    token = 'Bearer peer';
    owner = 'peer';
    c.synchronizeIdentity();
    expect(c.resultUnknown, false);
    await c.load();
    token = 'Bearer owner';
    owner = 'owner';
    c.synchronizeIdentity();
    await c.load();
    expect(await c.save(reviewed, seedChoices(reviewed)), false);
    expect(c.message, isNull);
    c.dispose();
    c.synchronizeIdentity();
    c.dispose();
    expect(writes, 1);
  });
  test('缺项保持未知，绑定当前记录与源版本，兴趣跳过不假造选择', () async {
    final requests = <Map<String, dynamic>>[];
    final c = AgentSeedController(
      authorizationHeader: () => 'Bearer owner',
      accountID: () => 'owner',
      apiBaseUrl: 'http://local',
      client: MockClient((r) async {
        if (r.method == 'PUT') {
          requests.add(jsonDecode(r.body) as Map<String, dynamic>);
          return seedResponse(
            seedJson(
              snapshot: 'c',
              progress: 'COMPLETED',
              intent: 'JUST_EXPLORE',
              city: 'aberdeen-gb',
              languages: ['zh-CN'],
            ),
          );
        }
        return seedResponse(seedJson());
      }),
    );
    addTearDown(c.dispose);
    expect(await c.load(), true);
    final first = c.record!;
    expect(first.currentCityID, isNull);
    expect(first.basicIntent, isEmpty);
    expect(first.languages, isEmpty);
    expect(await c.save(first, seedChoices(first)), true);
    expect(requests.single['expectedSnapshot'], first.snapshot);
    expect(
      requests.single['currentCitySnapshot'],
      first.cities.single.snapshot,
    );
    expect(requests.single.containsKey('confirmed'), false);
    expect(requests.single['interests'], isEmpty);
    expect(await c.save(first, seedChoices(first)), false);
    expect(requests.length, 1);
  });
  test('账号与组织切换同步失效，旧读取晚响应不能恢复，ABA也不能复用', () async {
    var token = 'Bearer owner';
    var owner = 'owner';
    String? workspace;
    final slow = Completer<http.Response>();
    var calls = 0;
    final c = AgentSeedController(
      authorizationHeader: () => token,
      accountID: () => owner,
      organizationWorkspaceID: () => workspace,
      apiBaseUrl: 'http://local',
      client: MockClient((_) async {
        calls++;
        return slow.future;
      }),
    );
    addTearDown(c.dispose);
    final pending = c.load();
    workspace = 'org-actor';
    c.synchronizeIdentity();
    workspace = null;
    c.synchronizeIdentity();
    slow.complete(seedResponse(seedJson()));
    expect(await pending, false);
    expect(c.record, isNull);
    expect(c.busy, false);
    token = 'Bearer peer';
    owner = 'peer';
    c.synchronizeIdentity();
    expect(c.record, isNull);
    expect(calls, 1);
  });
  test('5xx和丢失回复先核实，不盲重发；GET当前值一致才报告核实', () async {
    var writes = 0;
    var reads = 0;
    final wanted = seedJson(
      snapshot: 'c',
      progress: 'COMPLETED',
      intent: 'JUST_EXPLORE',
      city: 'aberdeen-gb',
      languages: ['zh-CN'],
      interests: ['原有兴趣'],
    );
    final c = AgentSeedController(
      authorizationHeader: () => 'Bearer owner',
      accountID: () => 'owner',
      apiBaseUrl: 'http://local',
      client: MockClient((r) async {
        if (r.method == 'PUT') {
          writes++;
          return http.Response('{}', 503);
        }
        reads++;
        return seedResponse(
          reads == 1 ? seedJson(interests: ['原有兴趣']) : wanted,
        );
      }),
    );
    addTearDown(c.dispose);
    await c.load();
    final r = c.record!;
    expect(await c.save(r, seedChoices(r)), false);
    expect(c.resultUnknown, true);
    expect(await c.save(r, seedChoices(r)), false);
    expect(writes, 1);
    expect(await c.load(), true);
    expect(c.resultUnknown, false);
    expect(c.message, contains('已核实'));
    expect(c.record!.interests, ['原有兴趣']);
    expect(writes, 1);
  });
  test('具体源409须重读并重新检查，禁止旧对象及客户端权限字段', () async {
    var reads = 0, writes = 0;
    final c = AgentSeedController(
      authorizationHeader: () => 'Bearer owner',
      accountID: () => 'owner',
      apiBaseUrl: 'http://local',
      client: MockClient((r) async {
        if (r.method == 'PUT') {
          writes++;
          return http.Response('{}', 409);
        }
        reads++;
        return seedResponse(seedJson(snapshot: reads == 1 ? 'a' : 'd'));
      }),
    );
    addTearDown(c.dispose);
    await c.load();
    final old = c.record!;
    expect(await c.save(old, {...seedChoices(old), 'verified': true}), false);
    expect(writes, 0);
    expect(await c.save(old, seedChoices(old)), false);
    expect(c.conflict, true);
    expect(await c.save(old, seedChoices(old)), false);
    expect(writes, 1);
    await c.load();
    expect(c.conflict, false);
    expect(await c.save(old, seedChoices(old)), false);
    expect(writes, 1);
  });
  test('403清除私密源，dispose后晚响应不通知或重建状态', () async {
    var calls = 0;
    final c = AgentSeedController(
      authorizationHeader: () => 'Bearer owner',
      accountID: () => 'owner',
      apiBaseUrl: 'http://local',
      client: MockClient((_) async {
        calls++;
        return calls == 1
            ? seedResponse(seedJson(interests: ['私密声明']))
            : http.Response('{}', 403);
      }),
    );
    await c.load();
    expect(c.record!.interests, isNotEmpty);
    expect(await c.load(), false);
    expect(c.denied, true);
    expect(c.record, isNull);
    c.dispose();
    final delayed = Completer<http.Response>();
    final d = AgentSeedController(
      authorizationHeader: () => 'Bearer owner',
      accountID: () => 'owner',
      apiBaseUrl: 'http://local',
      client: MockClient((_) => delayed.future),
    );
    var notifications = 0;
    d.addListener(() => notifications++);
    final pending = d.load();
    d.dispose();
    final before = notifications;
    delayed.complete(seedResponse(seedJson()));
    expect(await pending, false);
    expect(notifications, before);
    expect(d.record, isNull);
  });
}
