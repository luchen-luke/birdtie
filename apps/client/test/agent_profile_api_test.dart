import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/agent_profile_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_memory_correction_api_test.dart'
    show
        correctionMemoryRaw,
        correctionOwner,
        correctionAgent,
        correctionMemory;

const profileOwner = correctionOwner,
    profileAgent = correctionAgent,
    profileMemory = correctionMemory;
Map<String, dynamic> profileRaw({DateTime? at, bool configured = true}) {
  final t = at ?? DateTime.now().toUtc();
  return {
    'schemaVersion': 'private-agent-profile-v1',
    'profile': {
      'ownerType': 'PERSON',
      'ownerId': profileOwner,
      'agentId': profileAgent,
      'profileVersion': 1,
      'createdAt': t.toIso8601String(),
      'updatedAt': t.toIso8601String(),
    },
    'configured': configured,
    'fields': {
      for (final k in profileListFields)
        k: configured && k == 'socialPreferences' ? ['共同兴趣'] : [],
      for (final k in profileTextFields)
        k: configured && k == 'agentNotes' ? '本人填写的合成补充' : '',
    },
  };
}

Map<String, dynamic> policiesRaw({DateTime? at}) {
  final t = at ?? DateTime.now().toUtc();
  Map<String, dynamic> record(String f, Map<String, dynamic> settings) => {
    'family': f,
    'configured': false,
    'nativeRevision': 0,
    'status': 'UNCONFIGURED',
    'settings': settings,
  };
  return {
    'schemaVersion': 'agent-policy-settings-v1',
    'ownerType': 'PERSON',
    'ownerId': profileOwner,
    'agentId': profileAgent,
    'observedAt': t.toIso8601String(),
    'attention': record('ATTENTION', {'defaultRoute': 'BLOCK', 'rules': []}),
    'social': record('SOCIAL', {
      'rules': [
        for (final k in [
          'SAME_UNIVERSITY',
          'SHARED_COMMUNITY',
          'SHARED_ACTIVITY',
          'EXISTING_CONNECTION',
          'UNKNOWN_PERSON',
          'BUSINESS',
          'ORGANIZATION',
        ])
          {'category': k, 'preference': 'DISABLED'},
      ],
    }),
    'autonomy': record('AUTONOMY', {'level': 'LEVEL_0_OBSERVE'}),
  };
}

Map<String, dynamic> interestRaw({DateTime? at, bool record = true}) => {
  'schemaVersion': 'human-community-interest-v1',
  'ownerId': profileOwner,
  'agentId': profileAgent,
  'observedAt': (at ?? DateTime.now().toUtc()).toIso8601String(),
  'records': record
      ? [
          {
            'communityId': '82000000-0000-4000-8000-000000000010',
            'contextId': '82000000-0000-4000-8000-000000000011',
            'name': '合成社群兴趣',
            'sourceAvailable': true,
            'relation': 'interest',
            'state': 'PRIVATE',
          },
        ]
      : [],
  'options': [],
  'limit': 100,
  'truncated': false,
  'modelAccess': false,
  'sendAllowed': false,
  'membershipGranted': false,
};
Map<String, dynamic> registrationRaw({DateTime? at, bool record = true}) {
  final t = at ?? DateTime.now().toUtc();
  return {
    'schemaVersion': 'human-activity-participation-disclosure-v1',
    'ownerId': profileOwner,
    'agentId': profileAgent,
    'observedAt': t.toIso8601String(),
    'records': record
        ? [
            {
              'participationId': '82000000-0000-4000-8000-000000000012',
              'activityId': '82000000-0000-4000-8000-000000000013',
              'title': '合成徒步报名',
              'status': 'going',
              'sourceAvailable': true,
              'visibility': 'PRIVATE',
              'effectivePublic': false,
              'attendance': 'UNKNOWN',
              'startsAt': t.add(const Duration(hours: 1)).toIso8601String(),
              'endsAt': t.add(const Duration(hours: 2)).toIso8601String(),
              'sourceExpiresAt': t
                  .add(const Duration(hours: 2))
                  .toIso8601String(),
            },
          ]
        : [],
    'limit': 100,
    'truncated': false,
    'modelAccess': false,
    'sendAllowed': false,
    'membershipGranted': false,
  };
}

Map<String, dynamic> detailRaw({
  DateTime? at,
  Duration lease = const Duration(seconds: 30),
  String status = 'ACTIVE',
}) {
  final t = at ?? DateTime.now().toUtc();
  return {
    'schemaVersion': 'agent-memory-detail-v1',
    'owner': {'type': 'PERSON', 'id': profileOwner},
    'agentId': profileAgent,
    'target': {'id': profileMemory, 'version': 1, 'status': status},
    'memory': {'ACTIVE', 'PENDING_REVIEW'}.contains(status)
        ? correctionMemoryRaw(at: t)
        : null,
    'observedAt': t.toIso8601String(),
    'expiresAt': t.add(lease).toIso8601String(),
    'explanation': memoryDetailExplanation,
    'modelAccess': false,
  };
}

dynamic profileData(String path) {
  if (path == '/v1/me/agent-private-profile') return profileRaw();
  if (path == '/v1/me/agent-memories') {
    return [correctionMemoryRaw(at: DateTime.now().toUtc())];
  }
  if (path == '/v1/me/agent-policies') return policiesRaw();
  if (path == '/v1/me/community-interests') return interestRaw();
  if (path == '/v1/me/activity-participation-disclosures') {
    return registrationRaw();
  }
  if (path == '/v1/me/agent-memories/$profileMemory') return detailRaw();
  return [];
}

http.Response profileResponse(dynamic v) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': v})),
  200,
  headers: {'Content-Type': 'application/json; charset=utf-8'},
);

class ProfileTrackedClient extends MockClient {
  ProfileTrackedClient(super.fn);
  int closeCount = 0;
  @override
  void close() {
    closeCount++;
    super.close();
  }
}

class SlowProfileClient extends http.BaseClient {
  bool cancelled = false;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest r) async {
    await Future<void>.delayed(const Duration(seconds: 2));
    late StreamController<List<int>> stream;
    Timer? ticker;
    stream = StreamController(
      onListen: () {
        ticker = Timer.periodic(
          const Duration(seconds: 2),
          (_) => stream.add([32]),
        );
      },
      onCancel: () {
        cancelled = true;
        ticker?.cancel();
      },
    );
    return http.StreamedResponse(stream.stream, 200);
  }
}

void main() {
  test('发送和持续慢流共用12秒总期限并取消流', () async {
    final client = SlowProfileClient();
    final api = AgentProfileAPI(client: client, apiBaseUrl: 'http://local');
    final watch = Stopwatch()..start();
    await expectLater(
      api.profile('Bearer synthetic', profileOwner),
      throwsA(isA<TimeoutException>()),
    );
    expect(watch.elapsed, lessThan(const Duration(seconds: 15)));
    await Future<void>.delayed(Duration.zero);
    expect(client.cancelled, true);
    api.close();
    client.close();
  });
  test('所有资料读取仅GET原端口且借用client不关闭', () async {
    final requests = <http.Request>[];
    final client = ProfileTrackedClient((r) async {
      requests.add(r);
      return profileResponse(profileData(r.url.path));
    });
    final api = AgentProfileAPI(client: client, apiBaseUrl: 'http://local');
    expect(
      (await api.profile(
        'Bearer synthetic',
        profileOwner,
      )).fields['socialPreferences'],
      ['共同兴趣'],
    );
    expect(
      (await api.memories('Bearer synthetic', profileOwner)).single.sourceType,
      'EXPLICIT',
    );
    expect(
      (await api.policies('Bearer synthetic', profileOwner)).records.length,
      3,
    );
    expect(
      (await api.interests(
        'Bearer synthetic',
        profileOwner,
      )).records.single.state,
      'PRIVATE',
    );
    expect(
      (await api.participations(
        'Bearer synthetic',
        profileOwner,
      )).records.single.status,
      'going',
    );
    expect(
      (await api.detail(
        'Bearer synthetic',
        profileOwner,
        profileMemory,
      )).memory!.summary,
      '我偏好徒步活动',
    );
    expect(
      requests.every(
        (r) =>
            r.method == 'GET' &&
            r.body.isEmpty &&
            r.url.query.isEmpty &&
            r.headers['Authorization'] == 'Bearer synthetic',
      ),
      true,
    );
    api.close();
    api.close();
    expect(client.closeCount, 0);
    client.close();
  });
  test('真实closed Profile形状与未配置无隐藏内容', () {
    expect(
      AgentPrivateProfileView(
        profileRaw(configured: false),
        profileOwner,
      ).configured,
      false,
    );
    for (final mode in [
      'owner',
      'organization',
      'extra',
      'field',
      'unconfigured',
      'duplicate',
      'id',
      'time',
    ]) {
      final m = profileRaw();
      switch (mode) {
        case 'owner':
          m['profile']['ownerId'] = '82000000-0000-4000-8000-000000000099';
        case 'organization':
          m['profile']['ownerType'] = 'ORGANIZATION';
        case 'extra':
          m['grant'] = true;
        case 'field':
          m['fields']['secret'] = [];
        case 'unconfigured':
          m['configured'] = false;
        case 'duplicate':
          m['fields']['socialPreferences'] = ['共同兴趣', '共同兴趣'];
        case 'id':
          m['profile']['agentId'] = 'bad';
        case 'time':
          m['profile']['createdAt'] = '2026-02-30T01:00:00Z';
      }
      expect(
        () => AgentPrivateProfileView(m, profileOwner),
        throwsA(isA<FormatException>()),
        reason: mode,
      );
    }
  });
  test('070三family闭集不将UNCONFIGURED猜成已批准', () {
    final p = AgentPoliciesView(policiesRaw(), profileOwner);
    expect(p.records.values.every((r) => !r.configured), true);
    for (final mode in [
      'extra',
      'attention',
      'social',
      'autonomy',
      'status',
      'owner',
    ]) {
      final m = policiesRaw();
      switch (mode) {
        case 'extra':
          m['allowed'] = true;
        case 'attention':
          m['attention']['settings']['defaultRoute'] = 'SEND';
        case 'social':
          m['social']['settings']['rules'][0]['preference'] = 'AUTO';
        case 'autonomy':
          m['autonomy']['settings']['level'] = 'OPEN';
        case 'status':
          m['attention']['status'] = 'ACTIVE';
        case 'owner':
          m['ownerType'] = 'person';
      }
      expect(
        () => AgentPoliciesView(m, profileOwner),
        throwsA(isA<FormatException>()),
        reason: mode,
      );
    }
  });
  test('070真实有效期与暂停边界，不接受null或超出原窗口', () {
    final now = DateTime.now().toUtc(),
        from = DateTime.now().toUtc().subtract(const Duration(hours: 1));
    final expiry = now.add(const Duration(days: 1));
    Map<String, dynamic> configured() {
      final m = policiesRaw(at: now);
      m['attention'].addAll({
        'configured': true,
        'nativeRevision': 2,
        'status': 'ACTIVE',
        'validFrom': from.toIso8601String(),
        'updatedAt': from.toIso8601String(),
        'expiresAt': expiry.toIso8601String(),
      });
      return m;
    }

    final valid = configured();
    valid['attention']['settings']['pauseUntil'] = from.toIso8601String();
    expect(
      AgentPoliciesView(valid, profileOwner).records['ATTENTION']!.configured,
      true,
    );
    for (final v in [
      null,
      from.subtract(const Duration(seconds: 1)).toIso8601String(),
      expiry.add(const Duration(seconds: 1)).toIso8601String(),
    ]) {
      final m = configured();
      m['attention']['settings']['pauseUntil'] = v;
      expect(
        () => AgentPoliciesView(m, profileOwner),
        throwsA(isA<FormatException>()),
      );
    }
  });
  test('069详情严格身份ID版本租期与EXPLICIT独立声明', () {
    for (final mode in [
      'owner',
      'target',
      'agent',
      'version',
      'permission',
      'lease',
      'unknown',
      'inferred',
      'expiredBody',
    ]) {
      final m = detailRaw();
      switch (mode) {
        case 'owner':
          m['owner']['id'] = '82000000-0000-4000-8000-000000000099';
        case 'target':
          m['target']['id'] = '82000000-0000-4000-8000-000000000099';
        case 'agent':
          m['agentId'] = '82000000-0000-4000-8000-000000000099';
        case 'version':
          m['target']['version'] = 2;
        case 'permission':
          m['modelAccess'] = true;
        case 'lease':
          m['expiresAt'] = DateTime.parse(
            m['observedAt'],
          ).add(const Duration(minutes: 3)).toIso8601String();
        case 'unknown':
          m['target']['status'] = 'UNKNOWN';
        case 'inferred':
          m['memory']['sourceType'] = 'INFERRED';
        case 'expiredBody':
          m['target']['status'] = 'EXPIRED';
      }
      expect(
        () => AgentMemoryDetailView(m, profileOwner, profileMemory),
        throwsA(isA<FormatException>()),
        reason: mode,
      );
    }
    expect(
      AgentMemoryDetailView(
        detailRaw(status: 'EXPIRED'),
        profileOwner,
        profileMemory,
      ).memory,
      isNull,
    );
  });
  test('合法RFC3339偏移时间与有限UTC年', () {
    final m = detailRaw();
    final t = DateTime.now().toUtc();
    m['observedAt'] = t.toIso8601String().replaceFirst('Z', '+00:00');
    m['expiresAt'] = t
        .add(const Duration(seconds: 30))
        .toIso8601String()
        .replaceFirst('Z', '+00:00');
    expect(
      AgentMemoryDetailView(
        m,
        profileOwner,
        profileMemory,
      ).expiresAt.isAfter(t),
      true,
    );
    final p = profileRaw();
    p['profile']['createdAt'] = '2026-10-06T10:00:00+08:00';
    p['profile']['updatedAt'] = '2026-10-06T02:00:00Z';
    expect(AgentPrivateProfileView(p, profileOwner).agentID, profileAgent);
    p['profile']['createdAt'] = '0001-01-01T00:00:00+08:00';
    expect(
      () => AgentPrivateProfileView(p, profileOwner),
      throwsA(isA<FormatException>()),
    );
  });
  test('HTTP权限失败及配置URI不能静默匿名或写入', () async {
    var calls = 0;
    final client = MockClient((r) async {
      calls++;
      return http.Response('{}', 403);
    });
    final api = AgentProfileAPI(client: client, apiBaseUrl: 'http://local');
    await expectLater(
      api.profile('Bearer synthetic', profileOwner),
      throwsA(isA<AgentProfileHTTPError>()),
    );
    final bad = AgentProfileAPI(
      client: client,
      apiBaseUrl: 'http://secret:token@local?x=1',
    );
    await expectLater(
      bad.profile('Bearer synthetic', profileOwner),
      throwsA(isA<FormatException>()),
    );
    expect(calls, 1);
    api.close();
    bad.close();
    client.close();
  });
}
