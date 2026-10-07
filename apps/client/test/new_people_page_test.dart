import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/workspace/new_people_page.dart';
import 'package:birdtie_client/src/workspace/opportunity_reasons.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _source = '11111111-1111-4111-8111-111111111111';
const _otherSource = '22222222-2222-4222-8222-222222222222';
const _candidateIntent = '33333333-3333-4333-8333-333333333333';
const _account = '44444444-4444-4444-8444-444444444444';

http.Response _response(Object value, [int status = 200]) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': value})), status);

Future<BirdtieAuthController> _auth() async {
  final auth = BirdtieAuthController(
    client: MockClient(
      (r) async => switch ('${r.method} ${r.url.path}') {
        'GET /v1/auth/dev-phone/status' => _response({'enabled': true}),
        'POST /v1/auth/dev-phone/code' => _response({'expiresInSeconds': 300}),
        'POST /v1/auth/dev-phone/verify' => _response({
          'accessToken': 'test-session',
        }),
        'GET /v1/me' => _response({'id': 'person'}),
        'GET /v1/accounts/person/profile' => _response({'displayName': '合成测试'}),
        'POST /v1/session/logout' => http.Response('', 204),
        _ => http.Response('', 404),
      },
    ),
    apiBaseUrl: 'https://api.test',
    sessionVault: MemorySessionVault(),
  );
  await auth.initialize();
  await auth.requestDevPhoneCode('13800138000');
  await auth.verifyDevPhoneCode('13800138000', '123456');
  return auth;
}

Map<String, dynamic> _intent({
  String id = _source,
  String status = 'ACTIVE',
  String title = '合成线上羽毛球交流',
}) => {
  'id': id,
  'creatorAccountId': 'person',
  'type': 'FIND_COMPANION',
  'title': title,
  'audience': 'PUBLIC',
  'modality': 'ONLINE',
  'status': status,
  'constraints': {'category': '羽毛球', 'onlinePlatform': '视频通话'},
  'expiresAt': DateTime.now()
      .toUtc()
      .add(const Duration(days: 7))
      .toIso8601String(),
};

Map<String, dynamic> _candidates({String source = _source}) => {
  'source': 'RULE_BASED',
  'ruleVersion': 'v1',
  'sourceIntentId': source,
  'truncated': false,
  'candidates': [
    {
      'sourceIntentId': source,
      'candidateIntentId': _candidateIntent,
      'accountId': _account,
      'displayName': '合成同行者',
      'category': '羽毛球',
      'modality': 'ONLINE',
      'reasonCodes': ['CATEGORY_EQUAL'],
      'reasons': ['双方主动填写的类别相同'],
    },
  ],
};

class _Api {
  bool enabled = true;
  List<Map<String, dynamic>> intents = [_intent()];
  final requests = <http.Request>[];
  Future<http.Response> Function(http.Request)? override;
  late final client = MockClient((r) async {
    requests.add(r);
    expectSync(r.headers['Authorization'], 'Bearer test-session');
    if (override != null) return override!(r);
    return handle(r);
  });
  Future<http.Response> handle(http.Request r) async {
    switch ('${r.method} ${r.url.path}') {
      case 'GET /v1/me/new-people/consent':
        return _response({'enabled': enabled});
      case 'PUT /v1/me/new-people/consent':
        enabled =
            (jsonDecode(r.body) as Map<String, dynamic>)['enabled'] as bool;
        return _response({'enabled': enabled});
      case 'GET /v1/me/new-people/intents':
        return _response(intents);
      case 'POST /v1/me/new-people/intents':
        final body = jsonDecode(r.body) as Map<String, dynamic>;
        final row = _intent(status: 'DRAFT', title: body['title'] as String);
        row['modality'] = body['modality'];
        row['constraints'] = {
          for (final key in [
            'category',
            'placeId',
            'areaLabel',
            'onlinePlatform',
            'minParticipants',
            'maxParticipants',
          ])
            if (body.containsKey(key)) key: body[key],
        };
        intents = [row];
        return _response(row, 201);
      case 'GET /v1/me/new-people/candidates':
        return _response(
          _candidates(source: r.url.queryParameters['sourceIntentId']!),
        );
      case 'POST /v1/me/new-people/invitations':
        return _response({
          'id': 'synthetic-request',
          'scope': 'friend',
          'state': 'pending',
        }, 201);
      case 'GET /v1/cities':
        return _response([
          {'id': 'aberdeen-gb', 'name': '阿伯丁'},
        ]);
      case 'GET /v1/cities/aberdeen-gb':
        return _response({'id': 'aberdeen-gb', 'name': '阿伯丁'});
      case 'GET /v1/places/55555555-5555-4555-8555-555555555555':
        return _response({
          'id': '55555555-5555-4555-8555-555555555555',
          'name': '合成公开球馆',
        });
      case 'GET /v1/cities/aberdeen-gb/places':
        return _response([
          {'id': '55555555-5555-4555-8555-555555555555', 'name': '合成公开球馆'},
        ]);
      case 'POST /v1/me/social-intents/$_source/activate':
        expect(jsonDecode(r.body), {'confirmed': true});
        intents[0]['status'] = 'ACTIVE';
        return _response(intents[0]);
      case 'POST /v1/me/social-intents/$_source/cancel':
        expect(jsonDecode(r.body), {'confirmed': true});
        intents[0]['status'] = 'CANCELLED';
        return _response(intents[0]);
      default:
        return http.Response('{}', 404);
    }
  }

  int count(String suffix) =>
      requests.where((r) => r.url.path.endsWith(suffix)).length;
}

Future<void> _show(
  WidgetTester tester,
  BirdtieAuthController auth,
  _Api api, {
  String? initialSource,
  ValueNotifier<String?>? workspace,
}) async {
  await tester.pumpWidget(
    MaterialApp(
      home: NewPeoplePage(
        auth: auth,
        client: api.client,
        apiBaseUrl: 'https://api.test',
        initialSourceIntentID: initialSource,
        workspaceChanges: workspace,
        organizationWorkspaceID: workspace == null
            ? null
            : () => workspace.value,
      ),
    ),
  );
  await tester.pumpAndSettle();
}

Future<void> _visible(WidgetTester tester, Finder finder) async {
  FocusManager.instance.primaryFocus?.unfocus();
  await tester.pump(const Duration(milliseconds: 300));
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

Future<void> _selectSource(
  WidgetTester tester, {
  String? current,
  String title = '合成线上羽毛球交流',
}) async {
  final field = find.byKey(ValueKey('source_$current'));
  await _visible(tester, field);
  await tester.tap(field);
  await tester.pumpAndSettle();
  await tester.tap(find.text(title).last);
  await tester.pumpAndSettle();
}

Future<void> _search(WidgetTester tester) async {
  final button = find.text('查看合适的同行者');
  await _visible(tester, button);
  await tester.tap(button);
  await tester.pumpAndSettle();
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
  testWidgets('引荐来源只在原接口重核公开有效本人意图后初选，不自动查询或邀请', (tester) async {
    final auth = await _auth();
    final api = _Api();
    await _show(tester, auth, api, initialSource: _source);
    await _visible(tester, find.byKey(const ValueKey('source_$_source')));
    expect(find.byKey(const ValueKey('source_$_source')), findsOneWidget);
    expect(api.count('/candidates'), 0);
    expect(api.count('/invitations'), 0);
    await _dispose(tester, auth, api);
  });

  for (final bad in [
    'PRIVATE',
    'EXPIRED',
    'ABSENT',
    'FOREIGN',
    'FRIENDS',
    'MATCHED',
  ]) {
    testWidgets('引荐来源$bad不得初选，不能把带入ID当权限', (tester) async {
      final auth = await _auth();
      final api = _Api();
      if (bad == 'PRIVATE') api.intents[0]['audience'] = 'PRIVATE';
      if (bad == 'FRIENDS') api.intents[0]['audience'] = 'FRIENDS';
      if (bad == 'MATCHED') api.intents[0]['status'] = 'MATCHED';
      if (bad == 'FOREIGN') api.intents[0]['creatorAccountId'] = _account;
      if (bad == 'EXPIRED') {
        api.intents[0]['expiresAt'] = DateTime.now()
            .toUtc()
            .subtract(const Duration(seconds: 1))
            .toIso8601String();
      }
      if (bad == 'ABSENT') api.intents = [];
      await _show(tester, auth, api, initialSource: _source);
      await _visible(tester, find.byKey(const ValueKey('source_null')));
      expect(find.byKey(const ValueKey('source_$_source')), findsNothing);
      expect(api.count('/candidates'), 0);
      expect(api.count('/invitations'), 0);
      await _dispose(tester, auth, api);
    });
  }

  testWidgets('组织切换退休邀请批准；回到个人须重选，不恢复带入来源', (tester) async {
    final auth = await _auth();
    final api = _Api();
    final workspace = ValueNotifier<String?>(null);
    await _show(
      tester,
      auth,
      api,
      initialSource: _source,
      workspace: workspace,
    );
    await _search(tester);
    await _visible(tester, find.text('发送好友申请'));
    await tester.tap(find.text('发送好友申请'));
    await tester.pumpAndSettle();
    expect(find.text('取消'), findsOneWidget);
    final count = api.requests.length;
    workspace.value = _otherSource;
    await tester.pumpAndSettle();
    expect(find.text('取消'), findsNothing);
    expect(find.textContaining('请切回个人身份'), findsOneWidget);
    expect(api.requests.length, count);
    workspace.value = null;
    await tester.pumpAndSettle();
    await _visible(tester, find.byKey(const ValueKey('source_null')));
    expect(find.text('合成同行者'), findsNothing);
    expect(api.count('/invitations'), 0);
    await _dispose(tester, auth, api);
    workspace.dispose();
  });

  testWidgets('同key更换transport读取新来源，不沿用旧候选和批准', (tester) async {
    final auth = await _auth();
    final first = _Api();
    final second = _Api()
      ..intents = [_intent(id: _otherSource, title: '新接口的本人意图')];
    await _show(tester, auth, first);
    await _selectSource(tester);
    await _search(tester);
    final oldCount = first.requests.length;
    await _show(tester, auth, second);
    expect(second.count('/intents'), 1);
    expect(first.requests.length, oldCount);
    expect(find.text('合成同行者'), findsNothing);
    await _visible(tester, find.text('新接口的本人意图'));
    expect(find.text('新接口的本人意图'), findsOneWidget);
    expect(second.count('/candidates'), 0);
    expect(second.count('/invitations'), 0);
    await _dispose(tester, auth, second);
    first.client.close();
  });

  testWidgets('默认关闭；开启及选择来源不自动查候选或邀请', (tester) async {
    final auth = await _auth();
    final api = _Api()..enabled = false;
    await _show(tester, auth, api);
    expect(api.count('/candidates'), 0);
    expect(find.text('尚未开启，不查询候选或发送新朋友邀请。'), findsOneWidget);
    expect(find.byKey(const Key('new_people_title')), findsNothing);
    await tester.tap(find.byType(SwitchListTile));
    await tester.pumpAndSettle();
    expect(api.enabled, true);
    await _selectSource(tester);
    expect(api.count('/candidates'), 0);
    expect(api.count('/invitations'), 0);
    await _search(tester);
    await _visible(tester, find.text('合成同行者'));
    expect(find.text('双方填写的类别相同'), findsOneWidget);
    expect(api.requests.last.url.queryParameters, {'sourceIntentId': _source});
    expect(api.count('/invitations'), 0);
    await _dispose(tester, auth, api);
  });

  testWidgets('线上不读取城市；从线下切换线上彻底省略物理字段', (tester) async {
    final auth = await _auth();
    final api = _Api()..intents = [];
    await _show(tester, auth, api);
    expect(api.count('/cities'), 0);
    expect(find.text('活动城市'), findsNothing);
    await tester.enterText(find.byKey(const Key('new_people_title')), '合成线上交流');
    await tester.enterText(find.byKey(const Key('new_people_category')), '羽毛球');
    await _visible(tester, find.byKey(const ValueKey('modality_ONLINE')));
    await tester.tap(find.byKey(const ValueKey('modality_ONLINE')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('线下').last);
    await tester.pumpAndSettle();
    final cityField = find.byKey(const ValueKey('city_null'));
    await _visible(tester, cityField);
    await tester.tap(cityField);
    await tester.pumpAndSettle();
    await tester.tap(find.text('阿伯丁').last);
    await tester.pumpAndSettle();
    await _visible(tester, find.byKey(const Key('new_people_area')));
    await tester.enterText(find.byKey(const Key('new_people_area')), '合成粗区域');
    await _visible(tester, find.byKey(const ValueKey('modality_IN_PERSON')));
    await tester.tap(find.byKey(const ValueKey('modality_IN_PERSON')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('线上').last);
    await tester.pumpAndSettle();
    expect(find.text('活动城市'), findsNothing);
    await _visible(tester, find.text('保存找伙伴草稿'));
    await tester.tap(find.text('保存找伙伴草稿'));
    await tester.pumpAndSettle();
    final saved = api.requests.singleWhere(
      (r) => r.method == 'POST' && r.url.path.endsWith('/new-people/intents'),
    );
    final body = jsonDecode(saved.body) as Map<String, dynamic>;
    expect(body['modality'], 'ONLINE');
    for (final field in [
      'cityId',
      'placeId',
      'areaLabel',
      'contextId',
      'audience',
      'type',
    ]) {
      expect(body.containsKey(field), false);
    }
    expect(api.count('/activate'), 0);
    await _dispose(tester, auth, api);
  });

  testWidgets('发布需要独立预览确认；返回不激活，确认后回读状态', (tester) async {
    final auth = await _auth();
    final api = _Api()..intents = [_intent(status: 'DRAFT')];
    await _show(tester, auth, api);
    final preview = find.text('预览并公开');
    await _visible(tester, preview);
    await tester.tap(preview);
    await tester.pumpAndSettle();
    expect(find.text('预览并确认公开'), findsOneWidget);
    expect(find.textContaining('不代表身份已经核验'), findsOneWidget);
    await tester.tap(find.text('返回'));
    await tester.pumpAndSettle();
    expect(api.count('/activate'), 0);
    await tester.tap(preview);
    await tester.pumpAndSettle();
    await tester.tap(find.text('确认公开'));
    await tester.pumpAndSettle();
    expect(api.count('/activate'), 1);
    await _visible(tester, find.text('线上 · 已激活 · 按受众参与匹配'));
    expect(api.count('/candidates'), 0);
    expect(api.count('/invitations'), 0);
    await _dispose(tester, auth, api);
  });

  testWidgets('邀请须留言和信息分享确认；取消不发送，提交双来源不指定 recipient', (tester) async {
    final auth = await _auth();
    final api = _Api();
    await _show(tester, auth, api);
    await _selectSource(tester);
    await _search(tester);
    final invite = find.text('发送好友申请');
    await _visible(tester, invite);
    await tester.tap(invite);
    await tester.pumpAndSettle();
    expect(find.textContaining('显示名称和这段留言会分享给对方'), findsOneWidget);
    await tester.tap(find.text('取消'));
    await tester.pumpAndSettle();
    expect(api.count('/invitations'), 0);
    await tester.tap(invite);
    await tester.pumpAndSettle();
    await tester.tap(find.text('确认发送好友申请'));
    await tester.pumpAndSettle();
    expect(find.text('请先填写留言。'), findsOneWidget);
    expect(api.count('/invitations'), 0);
    await tester.enterText(
      find.byKey(const Key('new_people_note')),
      '合成测试：一起交流吗？',
    );
    await tester.tap(find.text('确认发送好友申请'));
    await tester.pumpAndSettle();
    final request = api.requests.singleWhere(
      (r) => r.url.path.endsWith('/invitations'),
    );
    expect(jsonDecode(request.body), {
      'sourceIntentId': _source,
      'candidateIntentId': _candidateIntent,
      'note': '合成测试：一起交流吗？',
      'confirmed': true,
    });
    expect(find.text('合成同行者'), findsNothing);
    expect(find.textContaining('好友申请已发送。对方接受后才建立关系'), findsOneWidget);
    expect(
      api.requests.any((r) => r.url.path.endsWith('/conversation')),
      false,
    );
    await _dispose(tester, auth, api);
  });

  testWidgets('候选失败可重试、真实空状态；失效来源邀请不显示成功', (tester) async {
    final auth = await _auth();
    final api = _Api();
    var candidateStatus = 503;
    var empty = false;
    api.override = (r) async {
      if (r.url.path.endsWith('/candidates')) {
        if (candidateStatus != 200) return http.Response('{}', candidateStatus);
        final result = _candidates();
        if (empty) result['candidates'] = [];
        return _response(result);
      }
      if (r.url.path.endsWith('/invitations')) return http.Response('{}', 409);
      return api.handle(r);
    };
    await _show(tester, auth, api);
    await _selectSource(tester);
    await _search(tester);
    await _visible(tester, find.text('同行者暂不可用，请重试。'));
    candidateStatus = 200;
    empty = true;
    await _search(tester);
    await _visible(tester, find.textContaining('暂无符合当前条件的同行者'));
    empty = false;
    await _search(tester);
    await _visible(tester, find.text('发送好友申请'));
    await tester.tap(find.text('发送好友申请'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byKey(const Key('new_people_note')), '合成测试');
    await tester.tap(find.text('确认发送好友申请'));
    await tester.pumpAndSettle();
    await _visible(tester, find.textContaining('没有确认发送成功'));
    expect(find.text('合成同行者'), findsNothing);
    expect(find.textContaining('好友申请已发送。对方接受后才建立关系'), findsNothing);
    await _dispose(tester, auth, api);
  });

  testWidgets('换来源立即清空；旧来源迟到响应不得恢复', (tester) async {
    final auth = await _auth();
    final api = _Api()
      ..intents = [_intent(), _intent(id: _otherSource, title: '另一个合成意图')];
    final late = Completer<http.Response>();
    api.override = (r) async =>
        r.url.path.endsWith('/candidates') ? late.future : api.handle(r);
    await _show(tester, auth, api);
    await _selectSource(tester);
    await _visible(tester, find.text('查看合适的同行者'));
    await tester.tap(find.text('查看合适的同行者'));
    await tester.pump();
    await _selectSource(tester, current: _source, title: '另一个合成意图');
    late.complete(_response(_candidates()));
    await tester.pumpAndSettle();
    expect(find.text('合成同行者'), findsNothing);
    expect(api.count('/invitations'), 0);
    expect(find.byKey(const ValueKey('source_$_otherSource')), findsOneWidget);
    await _dispose(tester, auth, api);
  });

  testWidgets('候选查询途中关闭立即清空并拒绝迟到响应', (tester) async {
    final auth = await _auth();
    final api = _Api();
    final late = Completer<http.Response>();
    api.override = (r) async =>
        r.url.path.endsWith('/candidates') ? late.future : api.handle(r);
    await _show(tester, auth, api);
    await _selectSource(tester);
    await _visible(tester, find.text('查看合适的同行者'));
    await tester.tap(find.text('查看合适的同行者'));
    await tester.pump();
    await _visible(tester, find.byType(SwitchListTile));
    await tester.tap(find.byType(SwitchListTile));
    await tester.pumpAndSettle();
    expect(api.enabled, false);
    late.complete(_response(_candidates()));
    await tester.pumpAndSettle();
    expect(find.text('合成同行者'), findsNothing);
    expect(find.text('尚未开启，不查询候选或发送新朋友邀请。'), findsOneWidget);
    await _dispose(tester, auth, api);
  });

  testWidgets('注销关闭邀请弹窗、清空内容，候选迟到响应不恢复', (tester) async {
    final auth = await _auth();
    final api = _Api();
    await _show(tester, auth, api);
    await _selectSource(tester);
    await _search(tester);
    await _visible(tester, find.text('发送好友申请'));
    await tester.tap(find.text('发送好友申请'));
    await tester.pumpAndSettle();
    await tester.enterText(
      find.byKey(const Key('new_people_note')),
      '未发送的合成留言',
    );
    await auth.signOut();
    await tester.pumpAndSettle();
    expect(find.text('登录后管理本人的找伙伴意图。'), findsOneWidget);
    expect(find.text('合成同行者'), findsNothing);
    expect(find.byKey(const Key('new_people_note')), findsNothing);
    expect(api.count('/invitations'), 0);
    await _dispose(tester, auth, api);
  });

  testWidgets('取消来源需确认，回读取消状态后无候选可选', (tester) async {
    final auth = await _auth();
    final api = _Api();
    await _show(tester, auth, api);
    await _selectSource(tester);
    await _search(tester);
    await _visible(tester, find.text('取消意图'));
    await tester.tap(find.text('取消意图'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('返回'));
    await tester.pumpAndSettle();
    expect(api.count('/cancel'), 0);
    await tester.tap(find.text('取消意图'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('确认取消'));
    await tester.pumpAndSettle();
    await _visible(tester, find.text('线上 · 已取消'));
    expect(find.text('合成同行者'), findsNothing);
    expect(api.count('/cancel'), 1);
    await _dispose(tester, auth, api);
  });

  testWidgets('旧线下草稿解析公开城市和地点名称，预览不显示实现编号', (tester) async {
    final auth = await _auth();
    final record = _intent(status: 'DRAFT');
    record['modality'] = 'IN_PERSON';
    record['cityId'] = 'aberdeen-gb';
    record['constraints'] = {
      'category': '羽毛球',
      'placeId': '55555555-5555-4555-8555-555555555555',
    };
    final api = _Api()..intents = [record];
    await _show(tester, auth, api);
    await _visible(tester, find.text('预览并公开'));
    await tester.tap(find.text('预览并公开'));
    await tester.pumpAndSettle();
    expect(find.text('活动城市：阿伯丁'), findsOneWidget);
    expect(find.text('公开地点：合成公开球馆'), findsOneWidget);
    expect(find.textContaining('55555555-5555'), findsNothing);
    expect(api.count('/cities/aberdeen-gb'), 1);
    expect(api.count('/places/55555555-5555-4555-8555-555555555555'), 1);
    await tester.tap(find.text('返回'));
    await tester.pumpAndSettle();
    expect(api.count('/activate'), 0);
    await _dispose(tester, auth, api);
  });

  testWidgets('地点详情无法核对时不得出现公开确认或激活', (tester) async {
    final auth = await _auth();
    final record = _intent(status: 'DRAFT');
    record['modality'] = 'IN_PERSON';
    record['cityId'] = 'aberdeen-gb';
    record['constraints'] = {
      'category': '羽毛球',
      'placeId': '55555555-5555-4555-8555-555555555555',
    };
    final api = _Api()..intents = [record];
    api.override = (r) async => r.url.path.startsWith('/v1/places/')
        ? http.Response('{}', 404)
        : api.handle(r);
    await _show(tester, auth, api);
    await _visible(tester, find.text('预览并公开'));
    await tester.tap(find.text('预览并公开'));
    await tester.pumpAndSettle();
    await _visible(tester, find.text('城市或地点暂不可核对，尚未公开。请刷新后重试。'));
    expect(find.text('确认公开'), findsNothing);
    expect(api.count('/activate'), 0);
    await _dispose(tester, auth, api);
  });

  testWidgets('注销后旧候选请求的迟到响应不得恢复内容', (tester) async {
    final auth = await _auth();
    final api = _Api();
    final late = Completer<http.Response>();
    api.override = (r) async =>
        r.url.path.endsWith('/candidates') ? late.future : api.handle(r);
    await _show(tester, auth, api);
    await _selectSource(tester);
    await _visible(tester, find.text('查看合适的同行者'));
    await tester.tap(find.text('查看合适的同行者'));
    await tester.pump();
    await auth.signOut();
    await tester.pump();
    expect(find.text('登录后管理本人的找伙伴意图。'), findsOneWidget);
    late.complete(_response(_candidates()));
    await tester.pumpAndSettle();
    expect(find.text('合成同行者'), findsNothing);
    expect(api.count('/invitations'), 0);
    await _dispose(tester, auth, api);
  });

  testWidgets('新朋友忽略原始理由及活动关系代码，未知代码采用中性说明', (tester) async {
    final auth = await _auth();
    final api = _Api();
    api.override = (r) async {
      if (r.url.path.endsWith('/candidates')) {
        final result = _candidates();
        final candidate =
            (result['candidates'] as List<dynamic>)[0] as Map<String, dynamic>;
        candidate['reasonCodes'] = [
          'TIE_ORGANIZER',
          'ORGANIZATION_ACTIVITY',
          'PRIVATE_SCHOOL',
        ];
        candidate['reasons'] = ['禁止显示：私聊、好友名单与学校'];
        return _response(result);
      }
      return api.handle(r);
    };
    await _show(tester, auth, api);
    await _selectSource(tester);
    await _search(tester);
    await _visible(tester, find.text(opportunityReasonFallback));
    expect(find.textContaining('禁止显示'), findsNothing);
    expect(find.text('主办者与你已有好友关系'), findsNothing);
    expect(find.text('这是组织主办的活动，不代表组织身份已经核验'), findsNothing);
    expect(api.count('/invitations'), 0);
    await _dispose(tester, auth, api);
  });

  testWidgets('未知新朋友来源或版本拒绝展示候选和邀请入口', (tester) async {
    for (final field in ['source', 'ruleVersion']) {
      final auth = await _auth();
      final api = _Api();
      api.override = (r) async {
        if (r.url.path.endsWith('/candidates')) {
          return _response(_candidates()..[field] = 'UNSUPPORTED');
        }
        return api.handle(r);
      };
      await _show(tester, auth, api);
      await _selectSource(tester);
      await _search(tester);
      expect(find.text('合成同行者'), findsNothing);
      expect(find.text('发送好友申请'), findsNothing);
      expect(api.count('/invitations'), 0);
      await _dispose(tester, auth, api);
    }
  });
}
