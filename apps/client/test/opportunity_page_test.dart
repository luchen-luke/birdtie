import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/workspace/activity_detail_sheet.dart';
import 'package:birdtie_client/src/workspace/opportunity_page.dart';
import 'package:birdtie_client/src/workspace/opportunity_reasons.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _person = '00000000-0000-4000-8000-000000000001';
const _intentId = '11111111-1111-4111-8111-111111111111';
const _activityId = '22222222-2222-4222-8222-222222222222';
const _placeId = '33333333-3333-4333-8333-333333333333';

http.Response _data(Object? value, [int status = 200]) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': value})), status);

Future<BirdtieAuthController> _auth({
  String account = _person,
  String token = 'test-session',
}) async {
  final auth = BirdtieAuthController(
    client: MockClient((r) async {
      if (r.method == 'GET' && r.url.path == '/v1/accounts/$account/profile') {
        return _data({'displayName': '合成测试用户'});
      }
      return switch ('${r.method} ${r.url.path}') {
        'GET /v1/auth/dev-phone/status' => _data({'enabled': true}),
        'POST /v1/auth/dev-phone/code' => _data({'expiresInSeconds': 300}),
        'POST /v1/auth/dev-phone/verify' => _data({'accessToken': token}),
        'GET /v1/me' => _data({'id': account}),
        'POST /v1/session/logout' => http.Response('', 204),
        _ => http.Response('{}', 404),
      };
    }),
    apiBaseUrl: 'https://api.test',
    sessionVault: MemorySessionVault(),
  );
  await auth.initialize();
  await auth.requestDevPhoneCode('13800138000');
  await auth.verifyDevPhoneCode('13800138000', '123456');
  return auth;
}

Map<String, dynamic> _intent({String status = 'ACTIVE'}) => {
  'id': _intentId,
  'creatorAccountId': _person,
  'title': '合成私人找活动意图',
  'type': 'FIND_ACTIVITY',
  'audience': 'PRIVATE',
  'modality': 'IN_PERSON',
  'status': status,
  'constraints': {'category': 'badminton', 'areaLabel': '合成市中心'},
  'expiresAt': DateTime.now()
      .toUtc()
      .add(const Duration(days: 7))
      .toIso8601String(),
};

Map<String, dynamic> _candidate() => {
  'id': '$_intentId:$_activityId',
  'intentId': _intentId,
  'entity': {'type': 'ACTIVITY', 'id': _activityId},
  'place': {'type': 'PLACE', 'id': _placeId},
  'title': '合成公开羽毛球活动',
  'placeName': '合成公开球馆',
  'startsAt': DateTime.now()
      .toUtc()
      .add(const Duration(days: 1))
      .toIso8601String(),
  'reasonCodes': [
    'INTENT_CATEGORY',
    'INTENT_PLACE',
    'INTENT_CITY_CONTEXT',
    'TIE_ORGANIZER',
  ],
  'reason': '禁止显示：私密学校、聊天正文和好友兴趣',
  'ruleVersion': 'activity-place-v2',
  'routeTier': 'EXISTING_TIE',
  'action': {
    'type': 'OPEN_ACTIVITY',
    'target': {'type': 'ACTIVITY', 'id': _activityId},
  },
};

Map<String, dynamic> _activity() => {
  'id': _activityId,
  'title': '合成公开羽毛球活动',
  'placeName': '合成公开球馆',
  'startsAt': DateTime.now()
      .toUtc()
      .add(const Duration(days: 1))
      .toIso8601String(),
  'endsAt': DateTime.now()
      .toUtc()
      .add(const Duration(days: 1, hours: 2))
      .toIso8601String(),
  'timeZone': 'Europe/London',
  'status': 'upcoming',
  'source': {},
};

class _Api {
  final acceptedHeaders = <String>{'Bearer test-session'};
  String source = 'RULE_BASED', version = 'activity-place-v2';
  List<Map<String, dynamic>> candidates = [_candidate()];
  List<Map<String, dynamic>> intents = [_intent()];
  final requests = <http.Request>[];
  Future<http.Response> Function(http.Request)? override;
  late final client = MockClient((r) async {
    requests.add(r);
    expect(acceptedHeaders, contains(r.headers['Authorization']));
    return override == null ? handle(r) : override!(r);
  });
  Future<http.Response> handle(http.Request r) async {
    switch ('${r.method} ${r.url.path}') {
      case 'GET /v1/me/opportunities':
        return http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': candidates,
              'source': source,
              'ruleVersion': version,
            }),
          ),
          200,
        );
      case 'GET /v1/me/social-intents':
        return _data(intents);
      case 'POST /v1/me/social-intents/$_intentId/activate':
        expect(jsonDecode(r.body), {'confirmed': true});
        intents[0]['status'] = 'ACTIVE';
        candidates = [_candidate()];
        return _data(intents[0]);
      case 'POST /v1/me/social-intents/$_intentId/cancel':
        expect(jsonDecode(r.body), {'confirmed': true});
        intents[0]['status'] = 'CANCELLED';
        candidates = [];
        return _data(intents[0]);
      case 'GET /v1/activities/$_activityId':
        return _data(_activity());
      case 'GET /v1/activities/$_activityId/participations/me':
        return _data(null);
      case 'GET /v1/places/$_placeId':
        return _data({'id': _placeId, 'name': '合成公开球馆'});
      default:
        return http.Response('{}', 404);
    }
  }

  int count(String method, String suffix) => requests
      .where((r) => r.method == method && r.url.path.endsWith(suffix))
      .length;
}

Future<void> _show(
  WidgetTester tester,
  BirdtieAuthController auth,
  _Api api, {
  ValueChanged<String>? open,
}) async {
  await tester.pumpWidget(
    MaterialApp(
      home: OpportunityPage(
        auth: auth,
        client: api.client,
        apiBaseUrl: 'https://api.test',
        onOpenActivity: open,
      ),
    ),
  );
  await tester.pumpAndSettle();
}

Future<void> _visible(WidgetTester tester, Finder finder) async {
  if (finder.evaluate().isEmpty) {
    tester
        .state<ScrollableState>(find.byType(Scrollable).first)
        .position
        .jumpTo(0);
    await tester.pump();
    await tester.scrollUntilVisible(
      finder,
      300,
      scrollable: find.byType(Scrollable).first,
    );
  }
  await tester.ensureVisible(finder);
  await tester.pump(const Duration(milliseconds: 300));
}

Future<void> _dispose(
  WidgetTester tester,
  BirdtieAuthController auth,
  _Api api,
) async {
  await tester.pumpWidget(const SizedBox());
  auth.dispose();
  api.client.close();
}

void main() {
  testWidgets('只读机会显示当前名称和三条事实理由；明确点击才调用详情', (tester) async {
    final auth = await _auth();
    final api = _Api();
    final opened = <String>[];
    await _show(tester, auth, api, open: opened.add);
    expect(find.text('为你找到的活动'), findsOneWidget);
    expect(find.text('合成公开羽毛球活动'), findsOneWidget);
    expect(find.textContaining('合成公开球馆'), findsOneWidget);
    expect(find.text('主办者与你已有好友关系'), findsOneWidget);
    expect(find.text('活动类别与你保存的意图一致'), findsOneWidget);
    expect(find.text('地点与你明确指定的地点一致'), findsOneWidget);
    expect(find.text('活动在你为意图明确选择的城市范围内'), findsNothing);
    expect(find.textContaining('禁止显示'), findsNothing);
    expect(find.textContaining('EXISTING_TIE'), findsNothing);
    expect(find.textContaining('activity-place-v2'), findsNothing);
    expect(find.textContaining(_activityId), findsNothing);
    expect(opened, isEmpty);
    expect(api.requests.where((r) => r.method != 'GET'), isEmpty);
    await _visible(tester, find.text('查看活动详情'));
    await tester.tap(find.text('查看活动详情'));
    await tester.pumpAndSettle();
    expect(opened, [_activityId]);
    expect(api.count('GET', '/activities/$_activityId'), 0);
    await _dispose(tester, auth, api);
  });

  testWidgets('未知和跨来源代码仅中性缺省，忽略原始理由和路由关系枚举', (tester) async {
    final auth = await _auth();
    final candidate = _candidate()
      ..['reasonCodes'] = ['CATEGORY_EQUAL', 'SECRET_CHAT'];
    final api = _Api()..candidates = [candidate];
    await _show(tester, auth, api);
    expect(find.text(opportunityReasonFallback), findsOneWidget);
    expect(find.text('主办者与你已有好友关系'), findsNothing);
    expect(find.textContaining('禁止显示'), findsNothing);
    await _dispose(tester, auth, api);
  });

  testWidgets('未知来源、整体版本或候选版本不显示成功和详情操作', (tester) async {
    for (final kind in ['source', 'version', 'candidateVersion']) {
      final auth = await _auth();
      final api = _Api();
      if (kind == 'source') api.source = 'UNVERIFIED';
      if (kind == 'version') api.version = 'future';
      if (kind == 'candidateVersion') {
        api.candidates[0]['ruleVersion'] = 'future';
      }
      final opened = <String>[];
      await _show(tester, auth, api, open: opened.add);
      expect(find.textContaining('活动机会暂不可用'), findsOneWidget);
      expect(find.text('合成公开羽毛球活动'), findsNothing);
      expect(find.text('查看活动详情'), findsNothing);
      expect(opened, isEmpty);
      await _dispose(tester, auth, api);
    }
  });

  testWidgets('非法实体、目标或动作不能显示为可操作候选', (tester) async {
    for (final kind in ['entity', 'target', 'action', 'id']) {
      final auth = await _auth();
      final candidate = _candidate();
      if (kind == 'entity') {
        candidate['entity'] = {'type': 'PERSON', 'id': _activityId};
      }
      if (kind == 'target') {
        candidate['action'] = {
          'type': 'OPEN_ACTIVITY',
          'target': {'type': 'ACTIVITY', 'id': _placeId},
        };
      }
      if (kind == 'action') {
        candidate['action'] = {
          'type': 'RSVP',
          'target': {'type': 'ACTIVITY', 'id': _activityId},
        };
      }
      if (kind == 'id') candidate['id'] = 'foreign:$_activityId';
      final api = _Api()..candidates = [candidate];
      await _show(tester, auth, api);
      expect(find.textContaining('活动机会暂不可用'), findsOneWidget);
      expect(find.text('查看活动详情'), findsNothing);
      expect(api.requests.where((r) => r.method != 'GET'), isEmpty);
      await _dispose(tester, auth, api);
    }
  });

  testWidgets('私人草稿独立预览；返回无写入，确认启用保持PRIVATE并回读机会', (tester) async {
    final auth = await _auth();
    final api = _Api()
      ..candidates = []
      ..intents = [_intent(status: 'DRAFT')];
    await _show(tester, auth, api);
    await _visible(tester, find.text('预览并启用'));
    await tester.tap(find.text('预览并启用'));
    await tester.pumpAndSettle();
    expect(find.text('预览并启用找活动意图'), findsOneWidget);
    expect(find.textContaining('仅供你找活动，不会公开。'), findsOneWidget);
    expect(find.text('类别：羽毛球'), findsOneWidget);
    expect(find.text('确认公开'), findsNothing);
    await tester.tap(find.text('返回'));
    await tester.pumpAndSettle();
    expect(api.count('POST', '/activate'), 0);
    await tester.tap(find.text('预览并启用'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('确认启用'));
    await tester.pumpAndSettle();
    expect(api.count('POST', '/activate'), 1);
    expect(api.intents[0]['audience'], 'PRIVATE');
    await _visible(tester, find.text('合成公开羽毛球活动'));
    expect(find.text('合成公开羽毛球活动'), findsOneWidget);
    expect(
      api.requests
          .where((r) => r.method == 'POST')
          .every((r) => r.url.path.endsWith('/activate')),
      true,
    );
    await _dispose(tester, auth, api);
  });

  testWidgets('取消来源须确认；待确认或取消请求期间旧候选立即清空', (tester) async {
    final auth = await _auth();
    final api = _Api();
    final late = Completer<http.Response>();
    api.override = (r) async =>
        r.method == 'POST' && r.url.path.endsWith('/cancel')
        ? late.future
        : api.handle(r);
    await _show(tester, auth, api);
    await _visible(tester, find.text('取消找活动意图'));
    await tester.tap(find.text('取消找活动意图'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('返回'));
    await tester.pumpAndSettle();
    expect(api.count('POST', '/cancel'), 0);
    expect(api.count('GET', '/opportunities'), 2);
    await _visible(tester, find.text('合成公开羽毛球活动'));
    expect(find.text('合成公开羽毛球活动'), findsOneWidget);
    await _visible(tester, find.text('取消找活动意图'));
    await tester.tap(find.text('取消找活动意图'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('确认取消'));
    await tester.pump();
    expect(find.text('合成公开羽毛球活动'), findsNothing);
    api.intents = [];
    api.candidates = [];
    late.complete(_data({'status': 'CANCELLED'}));
    await tester.pumpAndSettle();
    expect(api.count('POST', '/cancel'), 1);
    await _visible(tester, find.textContaining('暂无符合当前条件且有权查看的活动'));
    await _dispose(tester, auth, api);
  });

  testWidgets('过期、非私人、非本人、找伙伴、线上和终态来源不准启用', (tester) async {
    final auth = await _auth();
    final invalid = <Map<String, dynamic>>[];
    for (final change in [
      {'expiresAt': '2000-01-01T00:00:00Z'},
      {'audience': 'PUBLIC'},
      {'creatorAccountId': _activityId},
      {'type': 'FIND_COMPANION'},
      {'modality': 'ONLINE'},
      {'modality': 'HYBRID'},
      {'status': 'CANCELLED'},
    ]) {
      invalid.add({
        ..._intent(status: 'DRAFT'),
        ...change,
        'title': '不应可操作的来源',
      });
    }
    final api = _Api()
      ..candidates = []
      ..intents = invalid;
    await _show(tester, auth, api);
    expect(find.text('不应可操作的来源'), findsNothing);
    expect(find.text('预览并启用'), findsNothing);
    expect(api.count('POST', '/activate'), 0);
    await _visible(tester, find.textContaining('还没有可用来源。返回首页'));
    expect(find.textContaining('保存为社交意图草稿'), findsOneWidget);
    await _dispose(tester, auth, api);
  });

  testWidgets('读取失败可刷新重试，错误正文不显示', (tester) async {
    final auth = await _auth();
    final api = _Api();
    var failed = true;
    api.override = (r) async => failed && r.url.path.endsWith('/opportunities')
        ? http.Response('SECRET_AUTH_RESPONSE', 403)
        : api.handle(r);
    await _show(tester, auth, api);
    expect(find.textContaining('活动机会暂不可用'), findsOneWidget);
    expect(find.textContaining('SECRET_AUTH_RESPONSE'), findsNothing);
    failed = false;
    await tester.tap(find.byTooltip('刷新活动机会'));
    await tester.pumpAndSettle();
    expect(find.text('合成公开羽毛球活动'), findsOneWidget);
    await _dispose(tester, auth, api);
  });

  testWidgets('刷新开始立即清空，注销后旧请求迟到不能恢复', (tester) async {
    final auth = await _auth();
    final api = _Api();
    await _show(tester, auth, api);
    final late = Completer<http.Response>();
    api.override = (r) async =>
        r.url.path.endsWith('/opportunities') ? late.future : api.handle(r);
    await tester.tap(find.byTooltip('刷新活动机会'));
    await tester.pump();
    expect(find.text('合成公开羽毛球活动'), findsNothing);
    await auth.signOut();
    await tester.pump();
    expect(find.text('登录后查看本人有权访问的活动机会。'), findsOneWidget);
    late.complete(
      await api.handle(
        http.Request('GET', Uri.parse('https://api.test/v1/me/opportunities')),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('合成公开羽毛球活动'), findsNothing);
    await _dispose(tester, auth, api);
  });

  testWidgets('没有回调时实时重读详情，复用既有详情页且注销关闭旧内容', (tester) async {
    final auth = await _auth();
    final api = _Api();
    await _show(tester, auth, api);
    await _visible(tester, find.text('查看活动详情'));
    await tester.tap(find.text('查看活动详情'));
    await tester.pumpAndSettle();
    expect(find.byType(ActivityDetailSheet), findsOneWidget);
    expect(
      api.count('GET', '/activities/$_activityId'),
      greaterThanOrEqualTo(2),
    );
    expect(api.count('POST', '/participations'), 0);
    expect(api.count('POST', '/invitations'), 0);
    await auth.signOut();
    await tester.pumpAndSettle();
    expect(find.byType(ActivityDetailSheet), findsNothing);
    expect(find.text('合成公开羽毛球活动'), findsNothing);
    expect(find.text('登录后查看本人有权访问的活动机会。'), findsOneWidget);
    await _dispose(tester, auth, api);
  });

  testWidgets('详情失效或读取途中注销不能打开过期权限内容', (tester) async {
    for (final pending in [false, true]) {
      final auth = await _auth();
      final api = _Api();
      final late = Completer<http.Response>();
      api.override = (r) async =>
          r.url.path.endsWith('/activities/$_activityId')
          ? (pending ? late.future : http.Response('{}', 404))
          : api.handle(r);
      await _show(tester, auth, api);
      await _visible(tester, find.text('查看活动详情'));
      await tester.tap(find.text('查看活动详情'));
      if (pending) {
        await tester.pump();
        await auth.signOut();
        late.complete(_data(_activity()));
      }
      await tester.pumpAndSettle();
      expect(find.byType(ActivityDetailSheet), findsNothing);
      if (!pending) expect(find.text('活动详情当前不可访问，请刷新后重试。'), findsOneWidget);
      expect(api.count('POST', '/participations'), 0);
      await _dispose(tester, auth, api);
    }
  });

  testWidgets('来源预览只展示公开地点名称；查询失败不能确认启用', (tester) async {
    for (final failed in [false, true]) {
      final auth = await _auth();
      final row = _intent(status: 'DRAFT')
        ..['constraints'] = {'category': 'badminton', 'placeId': _placeId};
      final api = _Api()
        ..candidates = []
        ..intents = [row];
      if (failed) {
        api.override = (r) async => r.url.path.endsWith('/places/$_placeId')
            ? http.Response('{}', 404)
            : api.handle(r);
      }
      await _show(tester, auth, api);
      await _visible(tester, find.text('预览并启用'));
      await tester.tap(find.text('预览并启用'));
      await tester.pumpAndSettle();
      expect(find.textContaining(_placeId), findsNothing);
      if (failed) {
        expect(find.text('确认启用'), findsNothing);
        await _visible(tester, find.textContaining('指定地点暂不可核对'));
      } else {
        expect(find.text('公开地点：合成公开球馆'), findsOneWidget);
        await tester.tap(find.text('返回'));
        await tester.pumpAndSettle();
      }
      expect(api.count('POST', '/activate'), 0);
      await _dispose(tester, auth, api);
    }
  });

  testWidgets('首次空来源提供实际回首页路径，不停在设置页', (tester) async {
    final auth = await _auth();
    final api = _Api()
      ..candidates = []
      ..intents = [];
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (home) => Scaffold(
            body: TextButton(
              onPressed: () => Navigator.push(
                home,
                MaterialPageRoute<void>(
                  builder: (_) => Builder(
                    builder: (settings) => Scaffold(
                      body: TextButton(
                        onPressed: () => Navigator.push(
                          settings,
                          MaterialPageRoute<void>(
                            builder: (_) => OpportunityPage(
                              auth: auth,
                              client: api.client,
                              apiBaseUrl: 'https://api.test',
                            ),
                          ),
                        ),
                        child: const Text('设置里打开机会'),
                      ),
                    ),
                  ),
                ),
              ),
              child: const Text('测试首页打开设置'),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('测试首页打开设置'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('设置里打开机会'));
    await tester.pumpAndSettle();
    await _visible(tester, find.text('返回首页，找活动'));
    await tester.tap(find.text('返回首页，找活动'));
    await tester.pumpAndSettle();
    expect(find.text('测试首页打开设置'), findsOneWidget);
    expect(find.text('设置里打开机会'), findsNothing);
    expect(find.text('为你找到的活动'), findsNothing);
    await _dispose(tester, auth, api);
  });

  testWidgets('切换账号清空旧来源与候选，旧会话迟到响应不能覆盖新身份', (tester) async {
    final auth = await _auth();
    final api = _Api();
    await _show(tester, auth, api);
    final late = Completer<http.Response>();
    final oldResponse = await api.handle(
      http.Request('GET', Uri.parse('https://api.test/v1/me/opportunities')),
    );
    api.override = (r) async =>
        r.url.path.endsWith('/opportunities') &&
            r.headers['Authorization'] == 'Bearer test-session'
        ? late.future
        : api.handle(r);
    await tester.tap(find.byTooltip('刷新活动机会'));
    await tester.pump();
    const replacementId = '44444444-4444-4444-8444-444444444444';
    const replacementIntent = '55555555-5555-4555-8555-555555555555';
    final replacement = await _auth(
      account: replacementId,
      token: 'replacement-session',
    );
    api.acceptedHeaders.add('Bearer replacement-session');
    api.candidates = [
      {
        ..._candidate(),
        'title': '新身份可见的合成活动',
        'intentId': replacementIntent,
        'id': '$replacementIntent:$_activityId',
      },
    ];
    api.intents = [
      {
        ..._intent(),
        'id': replacementIntent,
        'creatorAccountId': replacementId,
        'title': '新身份的私人意图',
      },
    ];
    await _show(tester, replacement, api);
    expect(find.text('新身份可见的合成活动'), findsOneWidget);
    expect(find.text('合成公开羽毛球活动'), findsNothing);
    late.complete(oldResponse);
    await tester.pumpAndSettle();
    expect(find.text('新身份可见的合成活动'), findsOneWidget);
    expect(find.text('合成公开羽毛球活动'), findsNothing);
    expect(find.text('合成私人找活动意图'), findsNothing);
    await _dispose(tester, replacement, api);
    auth.dispose();
  });
}
