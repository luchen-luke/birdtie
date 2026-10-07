import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter/foundation.dart' show debugPrintSynchronously;
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

import 'agent_result_projection_test.dart' show typedItem, typedSet;
import 'now_context_query_api_test.dart'
    show onlineWire, onlineContextID, onlineTaskID;

Map<String, Object> _filters({
  String category = 'badminton',
  String time = 'weekend',
  String location = 'city',
}) => {
  'category': category,
  'timePreference': time,
  'locationPreference': location,
  'targetIntent': 'FIND_ACTIVITY',
};

// Mirrors the anonymous Place receipt's normalized public filters. These
// fixtures are transport contracts, not live place/provider evidence.
Map<String, Object> _placeFilters({
  String term = 'sports_venue',
  String location = 'city',
}) => {
  'targetIntent': 'FIND_PLACE',
  'searchTerm': term,
  'locationPreference': location,
};

http.Response _reply({
  Map<String, Object>? filters,
  String status = 'empty',
  String city = 'alpha',
}) {
  final kind = filters?['targetIntent'] == 'FIND_PLACE' ? 'place' : 'activity';
  final items = status == 'ready'
      ? [typedItem(kind, '32000000-0000-4000-8000-000000000001')]
      : <Map<String, dynamic>>[];
  return http.Response.bytes(
    utf8.encode(
      jsonEncode({
        'data': {
          'cityId': city,
          'message': '合成公开查询回执，不是实际活动或位置。',
          'activities': [],
          'places': [],
          'people': [],
          'groups': [],
          'resultSet': {
            ...typedSet(items),
            'id': 'synthetic-read-set',
            'status': status,
            'generatedAt': '2026-10-07T08:00:00Z',
            'filters': ?filters,
          },
          'mapEffects': {'camera': 'preserve', 'pinEntityIds': []},
        },
      }),
    ),
    200,
    headers: {'content-type': 'application/json; charset=utf-8'},
  );
}

class _PublicWire extends http.BaseClient {
  final requests = <Map<String, dynamic>>[];
  final urls = <Uri>[];
  final headers = <Map<String, String>>[];
  late FutureOr<http.Response> Function(int, Map<String, dynamic>) answer;

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) {
    final body =
        jsonDecode((request as http.Request).body) as Map<String, dynamic>;
    requests.add(body);
    urls.add(request.url);
    headers.add(Map.of(request.headers));
    final value = answer(requests.length, body);
    return Future<http.Response>.value(value).then(
      (r) => http.StreamedResponse(
        Stream.value(r.bodyBytes),
        r.statusCode,
        headers: r.headers,
      ),
    );
  }
}

class _Fixture {
  String city = 'alpha';
  final wire = _PublicWire();
  late final source = RemoteAgentTaskSource(
    cityID: () => city,
    authorizationHeader: () => null,
    apiBaseUrl: 'https://anonymous-context-fixture.test',
    client: wire,
  );
  late final controller = AgentWorkspaceController(source: source);

  Future<void> submit(String query) =>
      controller.submit(query, [], [], cityID: city);

  void dispose() {
    controller.dispose();
    wire.close();
  }
}

String _query(_Fixture f) => f.wire.requests.last['query'] as String;

void main() {
  for (final status in ['ready', 'empty']) {
    test('匿名三轮$status成功：真实Remote第三wire保留周末而非首轮', () async {
      final f = _Fixture();
      addTearDown(f.dispose);
      f.wire.answer = (n, _) => _reply(
        status: status,
        filters: _filters(time: n == 1 ? 'anytime' : 'weekend'),
      );
      await f.submit('找羽毛球活动');
      final id = f.controller.task!.id;
      await f.submit('周末羽毛球');
      await f.submit('近一点的呢？');
      debugPrintSynchronously(jsonEncode(f.wire.requests));
      expect(_query(f), contains('周末'));
      expect(_query(f), contains('羽毛球'));
      expect(_query(f), endsWith('近一点的呢？'));
      expect(f.wire.requests.every((r) => !r.containsKey('taskId')), isTrue);
      expect(f.controller.task!.id, id);
      expect(f.controller.task!.query, '找羽毛球活动');
      expect(
        f.controller.conversation
            .where((m) => m.role == 'user')
            .map((m) => m.text),
        ['找羽毛球活动', '周末羽毛球', '近一点的呢？'],
      );
    });
  }

  for (final change in ['time', 'category']) {
    test('匿名明确换$change槽先除旧条件：today不压weekend且羽毛球不压篮球', () async {
      final f = _Fixture();
      addTearDown(f.dispose);
      f.wire.answer = (_, _) => _reply(filters: _filters(time: 'today'));
      await f.submit('找今天的羽毛球活动');
      await f.submit(change == 'time' ? '周末的呢？' : '换成篮球');
      expect(_query(f), isNot(contains(change == 'time' ? '今天' : '羽毛球')));
      expect(_query(f), contains(change == 'time' ? '周末' : '篮球'));
      expect(_query(f), contains(change == 'time' ? '羽毛球' : '今天'));
    });
  }

  for (final failure in ['HTTP503', 'unsupported']) {
    test('匿名$failure本轮不能替代上一成功周末条件', () async {
      final f = _Fixture();
      addTearDown(f.dispose);
      f.wire.answer = (n, _) {
        if (n == 3) {
          return failure == 'HTTP503'
              ? http.Response('{"error":{"code":"fixture_unavailable"}}', 503)
              : _reply(
                  status: 'unsupported',
                  filters: _filters(category: 'basketball', time: 'today'),
                );
        }
        return _reply(filters: _filters(time: n == 1 ? 'anytime' : 'weekend'));
      };
      await f.submit('找羽毛球活动');
      await f.submit('周末羽毛球');
      await f.submit('今天篮球');
      await f.submit('近一点的呢？');
      expect(_query(f), contains('周末'));
      expect(_query(f), contains('羽毛球'));
      expect(_query(f), isNot(contains('今天')));
      expect(_query(f), isNot(contains('篮球')));
    });
  }

  test('匿名区域搜索与失败后retry使用上一成功条件及实际新bounds', () async {
    final f = _Fixture();
    addTearDown(f.dispose);
    f.wire.answer = (n, _) => n == 3
        ? http.Response('{"error":{"code":"fixture_unavailable"}}', 503)
        : _reply(filters: _filters(time: n == 1 ? 'anytime' : 'weekend'));
    await f.submit('找羽毛球活动');
    await f.submit('周末羽毛球');
    const bounds = MapBounds(west: -2.2, south: 57.1, east: -2, north: 57.3);
    await f.controller.searchThisArea([], [], bounds: bounds, cityID: f.city);
    final attempted = Map.of(f.wire.requests.last);
    expect(attempted['query'] as String, contains('周末'));
    expect(attempted['mapBounds'], bounds.toJson());
    await f.controller.retry();
    expect(f.wire.requests.last, attempted);
    expect(f.controller.requestError, isNull);
  });

  test('匿名迟到回执和newTask不将旧条件塞入新的本地任务', () async {
    final f = _Fixture();
    addTearDown(f.dispose);
    final held = Completer<http.Response>();
    f.wire.answer = (n, _) => n == 2
        ? held.future
        : _reply(
            filters: _filters(
              category: n >= 3 ? 'football' : 'badminton',
              time: 'anytime',
            ),
          );
    await f.submit('找羽毛球活动');
    final pending = f.submit('周末羽毛球');
    await Future<void>.delayed(Duration.zero);
    f.controller.newTask();
    await f.submit('找足球活动');
    final task = f.controller.task, result = f.controller.result;
    held.complete(_reply(filters: _filters()));
    await pending;
    expect(f.controller.task, same(task));
    expect(f.controller.result, same(result));
    await f.submit('近一点的呢？');
    expect(_query(f), contains('足球'));
    expect(_query(f), isNot(contains('周末')));
    expect(_query(f), isNot(contains('羽毛球')));
  });

  test('匿名已成功条件加本轮超过240 UTF8字节拒绝且不截断或dispatch', () async {
    final f = _Fixture();
    addTearDown(f.dispose);
    f.wire.answer = (_, _) => _reply(filters: _filters());
    await f.submit('找周末羽毛球活动');
    final body = '内容' * 38;
    expect(utf8.encode(body).length, 228);
    await f.submit(body);
    expect(f.wire.requests, hasLength(1));
    expect(f.controller.requestFailure!.code, 'QUERY_TOO_LONG');
    expect(f.controller.requestError, contains('当前任务条件过长'));
    expect(f.controller.conversation.last.text, body);
  });

  test('匿名只复用闭集公开条件：私人文本、未知值及不合法bounds不进入wire', () async {
    final f = _Fixture();
    addTearDown(f.dispose);
    f.wire.answer = (_, _) => _reply(
      filters: {
        ..._filters(),
        'currentQuery': '不得复制的旧当前文本',
        'searchTerm': '不作为上下文的任意文本',
        'privateReason': '不得复制的私人理由',
        'resultIDs': '不得复制的引用',
        'distancePreference': 'SECRET_DISTANCE',
        'mapWest': '-2.2',
        'mapSouth': '57.1',
        'mapEast': '-2',
        'mapNorth': 'NaN',
      },
    );
    await f.submit('找羽毛球活动');
    f.controller.conversation.add(
      const AgentMessage(role: 'assistant', text: '不得复制的助手建议：今天篮球'),
    );
    await f.submit('近一点的呢？');
    expect(_query(f), contains('周末'));
    for (final text in ['不得复制', '任意文本', 'SECRET_DISTANCE', '今天', '篮球']) {
      expect(_query(f), isNot(contains(text)));
    }
    expect(f.wire.requests.last.containsKey('mapBounds'), isFalse);
    expect(f.wire.requests.last.keys.toSet(), {'query'});
  });

  test('匿名成功旧版无filters保留明确两轮兼容，不伪造已核验条件', () async {
    final f = _Fixture();
    addTearDown(f.dispose);
    f.wire.answer = (_, _) => _reply();
    await f.submit('Find badminton this weekend');
    await f.submit('Anything closer?');
    expect(_query(f), 'Find badminton this weekend. Anything closer?');
    expect(f.controller.task!.lastSuccessfulPublicQuery, isNull);
  });

  test('匿名城市变化、来源替换及身份退休不继承旧已成功条件', () async {
    final f = _Fixture(), other = _Fixture();
    addTearDown(f.dispose);
    addTearDown(other.dispose);
    f.wire.answer = (_, _) => _reply(filters: _filters());
    other.wire.answer = (_, _) =>
        _reply(filters: _filters(category: 'football'));
    await f.submit('找羽毛球活动');
    final old = f.controller.task!;
    await other.source.followUp(old, '近一点的呢？', [], []);
    expect(_query(other), '近一点的呢？');
    f.city = 'beta';
    f.wire.answer = (_, _) => _reply(
      city: 'beta',
      filters: _filters(category: 'football', time: 'anytime'),
    );
    await f.submit('找足球活动');
    expect(_query(f), '找足球活动');
    await f.submit('近一点的呢？');
    expect(_query(f), contains('足球'));
    expect(_query(f), isNot(contains('周末')));
    f.controller.retireAccountContext();
    await f.submit('找篮球活动');
    expect(_query(f), '找篮球活动');
  });

  test('匿名成功bounds只在原公开范围续用，显式全城与新区域分别替换', () async {
    final f = _Fixture();
    addTearDown(f.dispose);
    const oldBounds = MapBounds(west: -2.2, south: 57.1, east: -2, north: 57.3);
    const nextBounds = MapBounds(
      west: -2.3,
      south: 57,
      east: -2.1,
      north: 57.2,
    );
    f.wire.answer = (_, _) => _reply(
      filters: {
        ..._filters(location: 'viewport'),
        'mapWest': '-2.2',
        'mapSouth': '57.1',
        'mapEast': '-2',
        'mapNorth': '57.3',
      },
    );
    await f.submit('找周末附近的羽毛球');
    await f.submit('近一点的呢？');
    expect(f.wire.requests.last['mapBounds'], oldBounds.toJson());
    await f.submit('改找全城的呢？');
    expect(_query(f), isNot(contains('附近')));
    expect(f.wire.requests.last.containsKey('mapBounds'), isFalse);
    await f.controller.searchThisArea(
      [],
      [],
      bounds: nextBounds,
      cityID: f.city,
    );
    expect(f.wire.requests.last['mapBounds'], nextBounds.toJson());
    expect(_query(f), contains('周末'));
  });

  test('匿名未成功的新范围退休响应不污染当前成功条件', () async {
    final f = _Fixture();
    addTearDown(f.dispose);
    final held = Completer<http.Response>();
    f.wire.answer = (n, _) =>
        n == 2 ? held.future : _reply(filters: _filters());
    await f.submit('找周末羽毛球活动');
    final old = f.submit('今天篮球');
    await Future<void>.delayed(Duration.zero);
    f.controller.retirePendingQueryForViewChange();
    held.complete(
      _reply(
        filters: _filters(category: 'basketball', time: 'today'),
      ),
    );
    await old;
    await f.submit('近一点的呢？');
    expect(_query(f), contains('周末'));
    expect(_query(f), contains('羽毛球'));
    expect(_query(f), isNot(contains('篮球')));
  });

  test('已保存远程CITY和原ONLINE仍只提交本轮及原任务版本', () async {
    final remote = _Fixture();
    addTearDown(remote.dispose);
    remote.wire.answer = (_, _) => _reply(filters: _filters());
    await remote.submit('找周末羽毛球活动');
    final publicContext = remote.controller.task!.lastSuccessfulPublicQuery;
    final task = AgentTask(
      id: '32000000-0000-4000-8000-000000000002',
      query: '原远程标题',
      status: 'COMPLETED',
      cityID: 'alpha',
      lastSuccessfulPublicQuery: publicContext,
    );
    await remote.source.followUp(task, '本轮新查询', [], []);
    expect(remote.wire.requests.last, {'query': '本轮新查询', 'taskId': task.id});

    final wire = _PublicWire();
    addTearDown(wire.close);
    wire.answer = (_, _) => http.Response.bytes(
      utf8.encode(jsonEncode({'data': onlineWire()})),
      200,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );
    final online = RemoteAgentTaskSource(
      cityID: () => null,
      authorizationHeader: () => 'Bearer synthetic-own',
      onlineContextID: () => onlineContextID,
      client: wire,
      apiBaseUrl: 'https://online-context-fixture.test',
    );
    addTearDown(online.dispose);
    final first = await online.resolve('阅读', [], []);
    await online.followUp(first.task!, '继续阅读', [], []);
    expect(wire.urls.every((url) => !url.path.contains('/cities/')), isTrue);
    expect(wire.requests.last['query'], '继续阅读');
    expect(wire.requests.last['taskId'], onlineTaskID);
    expect(
      wire.requests.last['expectedTaskUpdatedAt'],
      '2026-10-04T04:00:00.123456Z',
    );
    expect(wire.requests.last.containsKey('mapBounds'), isFalse);
  });

  test('原公开UTF8恰240字节完整dispatch，241以上不发且不截断', () async {
    final f = _Fixture();
    addTearDown(f.dispose);
    f.wire.answer = (_, _) => _reply();
    final exact = '汉' * 80;
    await f.source.resolve(exact, [], []);
    expect(_query(f), exact);
    expect(utf8.encode(_query(f)).length, 240);
    await expectLater(
      f.source.resolve('${exact}A', [], []),
      throwsA(
        isA<AgentRequestFailure>().having(
          (e) => e.code,
          'specific byte guard',
          'QUERY_TOO_LONG',
        ),
      ),
    );
    expect(f.wire.requests, hasLength(1));
  });

  test('匿名来源已经dispose后续问不能发出旧来源请求', () async {
    final f = _Fixture();
    addTearDown(f.dispose);
    f.wire.answer = (_, _) => _reply(filters: _filters());
    await f.submit('找周末羽毛球活动');
    final old = f.controller.task!;
    f.source.dispose();
    await expectLater(
      Future(() => f.source.followUp(old, '近一点的呢？', [], [])),
      throwsA(
        isA<AgentRequestFailure>().having(
          (e) => e.userMessage,
          'retired source explanation',
          contains('来源已变化'),
        ),
      ),
    );
    expect(f.wire.requests, hasLength(1));
  });

  for (final target in ['FIND_ORGANIZATION', 'FIND_PLACE']) {
    test('匿名非活动$target名称检索保留原名称并使用对应公开合同', () async {
      final f = _Fixture();
      addTearDown(f.dispose);
      final original = target == 'FIND_ORGANIZATION' ? '找CSSA组织' : '找体育馆地点';
      f.wire.answer = (n, body) => target == 'FIND_PLACE'
          ? _reply(
              status: n == 2 ? 'unsupported' : 'empty',
              filters: n == 2
                  ? _placeFilters(term: '', location: '')
                  : _placeFilters(
                      location: (body['query'] as String).contains('附近')
                          ? 'viewport'
                          : 'city',
                    ),
            )
          : _reply(
              filters: {
                'targetIntent': target,
                'category': '',
                'timePreference': 'anytime',
                'locationPreference': 'city',
                'searchTerm': 'cssa',
              },
            );
      await f.submit(original);
      final successful = f.controller.task!.lastSuccessfulPublicQuery;
      await f.submit('还有别的吗？');
      debugPrintSynchronously(jsonEncode(f.wire.requests));
      if (target == 'FIND_ORGANIZATION') {
        expect(_query(f), '$original. 还有别的吗？');
        expect(f.controller.task!.lastSuccessfulPublicQuery, isNull);
      } else {
        expect(_query(f), '找地点 "sports_venue" 全城 还有别的吗？');
        expect(successful!.placeSearchTerm, 'sports_venue');
        expect(successful.slots, {
          'targetIntent': 'FIND_PLACE',
          'locationPreference': 'city',
        });
        expect(f.controller.result!.resultSet!.status, 'unsupported');
        expect(f.controller.result!.entities, isEmpty);
        expect(f.controller.task!.lastSuccessfulPublicQuery, same(successful));
        expect(f.wire.requests.every((r) => !r.containsKey('taskId')), isTrue);
        await f.submit('附近的呢？');
        expect(_query(f), '找地点 "sports_venue" 附近的呢？');
        expect(f.controller.result!.resultSet!.status, 'empty');
      }
      expect(f.controller.task!.query, original);
    });
  }

  test('匿名filters空map仍属于legacy未知，不清掉原明确查询词', () async {
    final f = _Fixture();
    addTearDown(f.dispose);
    f.wire.answer = (_, _) => _reply(filters: {});
    await f.submit('找周末羽毛球活动');
    await f.submit('还有别的吗？');
    expect(_query(f), '找周末羽毛球活动. 还有别的吗？');
    expect(f.controller.task!.lastSuccessfulPublicQuery, isNull);
  });

  test('匿名已成功活动切至已成功地点后下一轮不得复活原活动首句', () async {
    final f = _Fixture();
    addTearDown(f.dispose);
    f.wire.answer = (n, _) => _reply(
      status: n == 3 ? 'unsupported' : 'empty',
      filters: n == 1
          ? _filters()
          : n == 3
          ? _placeFilters(term: '', location: '')
          : _placeFilters(term: n == 4 ? '' : 'sports_venue'),
    );
    await f.submit('找周末羽毛球活动');
    final id = f.controller.task!.id;
    await f.submit('找体育馆地点');
    final successful = f.controller.task!.lastSuccessfulPublicQuery;
    expect(successful!.slots, {
      'targetIntent': 'FIND_PLACE',
      'locationPreference': 'city',
    });
    await f.submit('还有别的吗？');
    debugPrintSynchronously(jsonEncode(f.wire.requests));
    expect(_query(f), '找地点 "sports_venue" 全城 还有别的吗？');
    expect(_query(f), isNot(contains('羽毛球')));
    expect(_query(f), isNot(contains('周末')));
    expect(_query(f), isNot(contains('找活动')));
    expect(f.controller.result!.resultSet!.status, 'unsupported');
    expect(f.controller.result!.entities, isEmpty);
    expect(f.controller.task!.lastSuccessfulPublicQuery, same(successful));
    expect(f.controller.task!.query, '找周末羽毛球活动');
    expect(f.controller.task!.id, id);
    await f.submit('地点');
    expect(_query(f), '全城 地点');
    expect(_query(f), isNot(contains('羽毛球')));
    expect(_query(f), isNot(contains('周末')));
    expect(f.controller.task!.query, '找周末羽毛球活动');
    expect(f.controller.task!.id, id);
  });

  test('匿名本地recent真实Remote只读恢复上一成功周末条件而非首句anytime', () async {
    final f = _Fixture();
    addTearDown(f.dispose);
    f.wire.answer = (_, body) => _reply(
      filters: _filters(
        time: (body['query'] as String).contains('周末') ? 'weekend' : 'anytime',
      ),
    );
    await f.submit('找羽毛球活动');
    await f.submit('周末羽毛球');
    final saved = f.controller.task!;
    await f.controller.reopen(saved, [], []);
    debugPrintSynchronously(jsonEncode(f.wire.requests));
    expect(_query(f), contains('周末'));
    expect(_query(f), contains('羽毛球'));
    expect(f.wire.requests.last.containsKey('taskId'), isFalse);
    expect(f.controller.task!.id, saved.id);
    expect(f.controller.task!.query, saved.query);
    expect(
      f.controller.task!.lastSuccessfulPublicQuery!.slots['timePreference'],
      'weekend',
    );
    expect(f.controller.requestError, isNull);
  });

  test('匿名原legacy本地恢复仍读取原明确首句，不伪造规范条件', () async {
    final f = _Fixture();
    addTearDown(f.dispose);
    f.wire.answer = (_, _) => _reply();
    await f.submit('找CSSA组织');
    final saved = f.controller.task!;
    await f.controller.reopen(saved, [], []);
    expect(_query(f), saved.query);
    expect(f.controller.task!.lastSuccessfulPublicQuery, isNull);
    expect(f.controller.task!.localOriginalQuerySuperseded, isFalse);
    expect(f.controller.requestError, isNull);
  });

  test('匿名规范本地恢复城市变化、来源更换、dispose均拒绝且零HTTP', () async {
    final f = _Fixture(), other = _Fixture();
    addTearDown(f.dispose);
    addTearDown(other.dispose);
    f.wire.answer = (_, _) => _reply(filters: _filters());
    other.wire.answer = (_, _) => _reply(filters: _filters());
    await f.submit('找周末羽毛球活动');
    final saved = f.controller.task!;
    await expectLater(
      other.source.restore(saved, [], []),
      throwsA(
        isA<AgentRequestFailure>().having(
          (e) => e.code,
          'changed source',
          'PUBLIC_CONTEXT_EXPIRED',
        ),
      ),
    );
    expect(other.wire.requests, isEmpty);
    f.city = 'beta';
    await expectLater(
      f.source.restore(saved, [], []),
      throwsA(
        isA<AgentRequestFailure>().having(
          (e) => e.code,
          'changed city',
          'PUBLIC_CONTEXT_EXPIRED',
        ),
      ),
    );
    f.city = 'alpha';
    f.source.dispose();
    await expectLater(
      f.source.restore(saved, [], []),
      throwsA(
        isA<AgentRequestFailure>().having(
          (e) => e.userMessage,
          'retired source',
          contains('来源已变化'),
        ),
      ),
    );
    expect(f.wire.requests, hasLength(1));
  });

  test('匿名规范本地恢复当前身份或组织或ONLINE变化均不发送旧条件', () async {
    String? bearer, organization, online;
    final wire = _PublicWire();
    wire.answer = (_, _) => _reply(filters: _filters());
    final source = RemoteAgentTaskSource(
      cityID: () => 'alpha',
      authorizationHeader: () => bearer,
      organizationWorkspaceID: () => organization,
      onlineContextID: () => online,
      client: wire,
      apiBaseUrl: 'https://anonymous-retirement-fixture.test',
    );
    final controller = AgentWorkspaceController(source: source);
    addTearDown(controller.dispose);
    addTearDown(wire.close);
    await controller.submit('找周末羽毛球活动', [], [], cityID: 'alpha');
    final saved = controller.task!;
    for (final changed in ['PERSON', 'ORGANIZATION', 'ONLINE']) {
      bearer = changed == 'PERSON' ? 'Bearer synthetic-own' : null;
      organization = changed == 'ORGANIZATION' ? 'synthetic-workspace' : null;
      online = changed == 'ONLINE' ? onlineContextID : null;
      await expectLater(
        source.restore(saved, [], []),
        throwsA(
          isA<AgentRequestFailure>().having(
            (e) => e.code,
            'retired $changed',
            'PUBLIC_CONTEXT_EXPIRED',
          ),
        ),
      );
      expect(wire.requests, hasLength(1));
    }
  });

  for (final missing in [false, true]) {
    test('匿名已成功活动被${missing ? '缺少filters' : '地点'}替代后恢复不重发首句', () async {
      final f = _Fixture();
      addTearDown(f.dispose);
      f.wire.answer = (n, _) => _reply(
        filters: n == 1
            ? _filters()
            : missing
            ? null
            : _placeFilters(),
      );
      await f.submit('找周末羽毛球活动');
      await f.submit('找体育馆地点');
      final saved = f.controller.task!;
      if (missing) {
        expect(saved.lastSuccessfulPublicQuery, isNull);
      } else {
        expect(saved.lastSuccessfulPublicQuery!.slots, {
          'targetIntent': 'FIND_PLACE',
          'locationPreference': 'city',
        });
        expect(
          saved.lastSuccessfulPublicQuery!.placeSearchTerm,
          'sports_venue',
        );
      }
      expect(saved.localOriginalQuerySuperseded, isTrue);
      await f.controller.reopen(saved, [], []);
      if (missing) {
        expect(f.wire.requests, hasLength(2));
        expect(f.controller.result, isNull);
        expect(f.controller.requestFailure!.code, 'PUBLIC_CONTEXT_EXPIRED');
        expect(f.controller.requestError, contains('未重发原查询'));
      } else {
        expect(f.wire.requests, hasLength(3));
        expect(_query(f), '找地点 "sports_venue" 全城');
        expect(f.wire.requests.last.keys.toSet(), {'query'});
        expect(_query(f), isNot(contains('羽毛球')));
        expect(_query(f), isNot(contains('周末')));
        expect(_query(f), isNot(contains('找活动')));
        expect(f.controller.result, isNotNull);
        expect(f.controller.result!.resultSet!.status, 'empty');
        expect(f.controller.requestFailure, isNull);
        expect(f.controller.requestError, isNull);
        expect(
          f.controller.task!.lastSuccessfulPublicQuery!.placeSearchTerm,
          'sports_venue',
        );
        expect(f.controller.task!.messages, saved.messages);
      }
      expect(f.controller.task!.query, saved.query);
      expect(f.controller.task!.id, saved.id);
    });
  }

  test('匿名AREA_DISCOVERY公开规范条件恢复仅有限词及合法bounds', () async {
    final f = _Fixture();
    addTearDown(f.dispose);
    const bounds = MapBounds(west: -2.2, south: 57.1, east: -2, north: 57.3);
    f.wire.answer = (_, _) => _reply(
      filters: {
        ..._filters(location: 'viewport'),
        'targetIntent': 'AREA_DISCOVERY',
        'mapWest': '-2.2',
        'mapSouth': '57.1',
        'mapEast': '-2',
        'mapNorth': '57.3',
        'currentQuery': '不得复用旧query',
        'searchTerm': '不得复用任意搜索词',
      },
    );
    await f.submit('看看这里的公开活动');
    final saved = f.controller.task!;
    await f.controller.reopen(saved, [], []);
    expect(_query(f), '找活动 羽毛球 周末 附近');
    expect(f.wire.requests.last, {
      'query': '找活动 羽毛球 周末 附近',
      'mapBounds': bounds.toJson(),
    });
    expect(f.controller.task!.query, saved.query);
    expect(f.controller.requestError, isNull);
  });

  test('匿名恢复当前成功回执更新task与recent条件，保留原历史和标题', () async {
    final f = _Fixture();
    addTearDown(f.dispose);
    f.wire.answer = (n, _) =>
        _reply(filters: _filters(time: n == 1 ? 'weekend' : 'today'));
    await f.submit('找周末羽毛球活动');
    final saved = f.controller.task!;
    final history = saved.messages;
    await f.controller.reopen(saved, [], []);
    expect(_query(f), '找活动 羽毛球 周末 全城');
    expect(
      f.controller.task!.lastSuccessfulPublicQuery!.slots['timePreference'],
      'today',
    );
    expect(
      f
          .controller
          .recent
          .single
          .lastSuccessfulPublicQuery!
          .slots['timePreference'],
      'today',
    );
    expect(f.controller.task!.messages, history);
    expect(f.controller.task!.query, saved.query);
    expect(f.controller.task!.id, saved.id);
    await f.submit('近一点的呢？');
    expect(_query(f), contains('今天'));
    expect(_query(f), isNot(contains('周末')));
  });

  test('匿名规范恢复503后retry仍原有限条件，成功才更新，不重发标题', () async {
    final f = _Fixture();
    addTearDown(f.dispose);
    f.wire.answer = (n, _) => n == 2
        ? http.Response('{"error":{"code":"UNAVAILABLE"}}', 503)
        : _reply(filters: _filters(time: n == 1 ? 'weekend' : 'today'));
    await f.submit('找周末羽毛球活动');
    final saved = f.controller.task!;
    await f.controller.reopen(saved, [], []);
    expect(
      f.controller.task!.lastSuccessfulPublicQuery!.slots['timePreference'],
      'weekend',
    );
    expect(f.controller.result, isNull);
    expect(f.controller.requestFailure!.statusCode, 503);
    await f.controller.retry();
    expect(f.wire.requests[1], f.wire.requests[2]);
    expect(_query(f), '找活动 羽毛球 周末 全城');
    expect(
      f.controller.task!.lastSuccessfulPublicQuery!.slots['timePreference'],
      'today',
    );
    expect(
      f
          .controller
          .recent
          .single
          .lastSuccessfulPublicQuery!
          .slots['timePreference'],
      'today',
    );
    expect(f.controller.requestError, isNull);
  });

  test('匿名规范restore迟到成功不得越过newTask代际更新条件或结果', () async {
    final f = _Fixture();
    addTearDown(f.dispose);
    final held = Completer<http.Response>();
    f.wire.answer = (n, _) =>
        n == 2 ? held.future : _reply(filters: _filters());
    await f.submit('找周末羽毛球活动');
    final saved = f.controller.task!;
    final restoring = f.controller.reopen(saved, [], []);
    await Future<void>.delayed(Duration.zero);
    f.controller.newTask();
    held.complete(
      _reply(
        filters: _filters(category: 'basketball', time: 'today'),
      ),
    );
    await restoring;
    expect(f.controller.task, isNull);
    expect(f.controller.result, isNull);
    expect(f.controller.conversation, isEmpty);
    expect(
      f.controller.recent.single.lastSuccessfulPublicQuery!.slots['category'],
      'badminton',
    );
    expect(
      f
          .controller
          .recent
          .single
          .lastSuccessfulPublicQuery!
          .slots['timePreference'],
      'weekend',
    );
  });

  test('匿名超长描述不允许原样重试，保留草稿和已成功条件并提供恢复说明', () async {
    final f = _Fixture();
    addTearDown(f.dispose);
    f.wire.answer = (_, _) => _reply(filters: _filters());
    await f.submit('找周末羽毛球活动');
    final context = f.controller.task!.lastSuccessfulPublicQuery;
    final draft = '内容' * 38;
    await f.submit(draft);
    expect(f.wire.requests, hasLength(1));
    expect(f.controller.requestFailure!.code, 'QUERY_TOO_LONG');
    expect(f.controller.permitsUnchangedRetry, isFalse);
    expect(f.controller.requestFailure!.recoveryMessage, '请缩短内容或开始新任务。');
    expect(f.controller.requestError, contains('尚未发出查询'));
    expect(f.controller.requestError, isNot(contains('240')));
    expect(f.controller.requestError, isNot(contains('字节')));
    expect(f.controller.conversation.last.text, draft);
    expect(f.controller.task!.lastSuccessfulPublicQuery, same(context));
    await f.controller.retry();
    expect(f.wire.requests, hasLength(1));
  });

  for (final retirement in ['city', 'source']) {
    test('匿名规范restore$retirement不允许原样重试且零HTTP，要求当前新查询', () async {
      final f = _Fixture();
      addTearDown(f.dispose);
      f.wire.answer = (_, _) => _reply(filters: _filters());
      await f.submit('找周末羽毛球活动');
      final saved = f.controller.task!;
      if (retirement == 'city') {
        f.city = 'beta';
      } else {
        f.source.dispose();
      }
      await f.controller.reopen(saved, [], []);
      expect(f.wire.requests, hasLength(1));
      expect(f.controller.permitsUnchangedRetry, isFalse);
      expect(f.controller.requestFailure!.code, 'PUBLIC_CONTEXT_EXPIRED');
      expect(
        f.controller.requestFailure!.recoveryMessage,
        '本轮范围或来源已变化，请重新发起查询。',
      );
      expect(f.controller.task!.query, saved.query);
      expect(
        f.controller.task!.lastSuccessfulPublicQuery,
        same(saved.lastSuccessfulPublicQuery),
      );
      expect(f.controller.result, isNull);
      await f.controller.retry();
      expect(f.wire.requests, hasLength(1));
    });
  }

  test('本地确定失败门槛保留原缺城市、身份、线上未知、HTTP与网络重试规则', () {
    for (final failure in [
      const AgentRequestFailure('缺城市', code: 'NEEDS_CITY'),
      const AgentRequestFailure('登录失效', statusCode: 401),
      const AgentRequestFailure('无权限', statusCode: 403),
      const AgentRequestFailure('线上结果未知', code: 'online_result_unknown'),
    ]) {
      expect(failure.permitsUnchangedRetry, isFalse);
    }
    for (final failure in [
      const AgentRequestFailure('暂时不可用', statusCode: 503),
      const AgentRequestFailure('网络连接失败'),
      const AgentRequestFailure('请求超时'),
      const AgentRequestFailure('普通失败', code: 'unrelated_original_code'),
    ]) {
      expect(failure.permitsUnchangedRetry, isTrue);
      expect(failure.recoveryMessage, isNull);
    }
    expect(
      const AgentRequestFailure(
        '原线上未知',
        code: 'online_result_unknown',
      ).recoveryMessage,
      '请先从最近对话核对提交结果；不会重复发送原请求。',
    );
    expect(
      const AgentRequestFailure('原登录失效', statusCode: 401).recoveryMessage,
      '请先通过个人资料重新登录，再发起查询。',
    );
    expect(
      const AgentRequestFailure('原权限失效', statusCode: 403).recoveryMessage,
      '请先确认当前身份和访问权限；恢复权限后再发起查询。',
    );
  });
}
