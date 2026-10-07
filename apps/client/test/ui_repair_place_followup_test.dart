import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _bounds = MapBounds(west: -2.2, south: 57.1, east: -2.0, north: 57.3);

Map<String, String> _filters({String term = '', bool viewport = false}) => {
  'targetIntent': 'FIND_PLACE',
  'searchTerm': term,
  'locationPreference': viewport ? 'viewport' : 'city',
  if (viewport) ...{
    'mapWest': '-2.2',
    'mapSouth': '57.1',
    'mapEast': '-2.0',
    'mapNorth': '57.3',
  },
};

// Transport contract fixtures only; these are not real search/provider evidence.
Map<String, dynamic> _wire(
  String city,
  Map<String, String> filters, {
  List<String> ids = const ['one', 'two', 'three'],
}) => {
  'data': {
    'cityId': city,
    'mode': 'rules',
    'message': 'Contract fixture: ${ids.length} places',
    'activities': [],
    'people': [],
    'groups': [],
    'organizations': [],
    'places': [],
    'resultSet': {
      'schema': 'typed-agent-results-v1',
      'id': 'contract-result-${filters['searchTerm'] ?? 'activity'}',
      'status': ids.isEmpty ? 'empty' : 'ready',
      'generatedAt': '2026-10-08T00:00:00Z',
      'filters': filters,
      'entities': [
        for (final id in ids) {'type': 'place', 'id': id},
      ],
      'items': [
        for (final id in ids)
          {
            'entityRef': {'type': 'place', 'id': id},
            'title': 'Contract place $id',
            'summary': 'Unit fixture only',
            'scope': 'AUTHORIZED_VIEW',
            'detailRef': {'type': 'place', 'id': id},
            'anchor': {
              'coordinateSystem': 'wgs84',
              'precision': 'point',
              'latitude': 57.15,
              'longitude': -2.1,
            },
          },
      ],
    },
    'mapEffects': {
      'camera': 'preserve',
      'pinEntityIds': [for (final id in ids) 'place:$id'],
    },
  },
};

class _Fixture {
  String city = 'aberdeen';
  String? token;
  String? organization;
  String? online;
  int epoch = 0;
  Map<String, String> filters = _filters();
  List<String> ids = ['one', 'two', 'three'];
  final requests = <http.Request>[];
  Completer<http.Response>? pending;
  late final source = RemoteAgentTaskSource(
    cityID: () => city,
    authorizationHeader: () => token,
    organizationWorkspaceID: () => organization,
    onlineContextID: () => online,
    publicEvidenceEpoch: () => epoch,
    apiBaseUrl: 'http://contract.invalid',
    client: MockClient((request) async {
      requests.add(request);
      if (pending case final waiting?) return waiting.future;
      return http.Response.bytes(
        utf8.encode(jsonEncode(_wire(city, filters, ids: ids))),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    }),
  );
  late final workspace = AgentWorkspaceController(source: source);
  Map<String, dynamic> get body => jsonDecode(requests.last.body);
  String get query => body['query'] as String;
  Future<void> submit(String query) =>
      workspace.submit(query, [], [], cityID: city);
  void dispose() => workspace.dispose();
}

void main() {
  for (final firstQuery in ['找地点', '好地点']) {
    test(
      '$firstQuery followed by explicit 地点 replaces the old search term',
      () async {
        final f = _Fixture();
        addTearDown(f.dispose);
        if (firstQuery == '好地点') {
          f.filters = _filters(term: '好');
          f.ids = [];
        }
        await f.submit(firstQuery);
        final taskID = f.workspace.task!.id;
        expect(f.workspace.task!.lastSuccessfulPublicQuery, isNotNull);
        expect(f.query, firstQuery);
        f.filters = _filters();
        f.ids = ['one', 'two', 'three'];
        await f.submit('地点');
        expect(f.query, '全城 地点');
        expect(f.query, isNot(contains('好')));
        expect(f.query, isNot(contains('.')));
        expect(f.body.containsKey('taskId'), isFalse);
        expect(f.body.containsKey('mapBounds'), isFalse);
        expect(f.workspace.task!.id, taskID);
        expect(f.workspace.result!.entities.map((e) => e.id), [
          'place:one',
          'place:two',
          'place:three',
        ]);
        expect(f.workspace.result!.mapEffects!.pinEntityIDs, [
          'place:one',
          'place:two',
          'place:three',
        ]);
        expect(
          f.workspace.replies.last.result,
          same(f.workspace.presentedResult),
        );
        expect(
          f.workspace.conversation
              .where((m) => m.role == 'user')
              .map((m) => m.text),
          [firstQuery, '地点'],
        );
      },
    );
  }

  test(
    'legacy place response also uses the explicit current place query',
    () async {
      final f = _Fixture()..filters = {};
      addTearDown(f.dispose);
      await f.submit('好地点');
      expect(f.workspace.task!.lastSuccessfulPublicQuery, isNull);
      await f.submit('地点');
      expect(f.query, '地点');
      expect(f.body.containsKey('taskId'), isFalse);
    },
  );

  test(
    'activity to place consumes only scope and retires old activity slots',
    () async {
      final f = _Fixture()
        ..filters = {
          'targetIntent': 'FIND_ACTIVITY',
          'category': 'badminton',
          'timePreference': 'weekend',
          'locationPreference': 'city',
        };
      addTearDown(f.dispose);
      await f.submit('找周末羽毛球活动');
      f.filters = _filters();
      await f.submit('地点');
      expect(f.query, '全城 地点');
      expect(f.query, isNot(contains('羽毛球')));
      expect(f.query, isNot(contains('周末')));
      expect(f.query, isNot(contains('.')));
    },
  );

  test(
    'place viewport is inherited until an explicit city scope replaces it',
    () async {
      final f = _Fixture()..filters = _filters(viewport: true);
      addTearDown(f.dispose);
      await f.workspace.searchThisArea(
        [],
        [],
        bounds: _bounds,
        cityID: f.city,
        query: '附近地点',
      );
      expect(f.body['mapBounds'], _bounds.toJson());
      await f.submit('地点');
      expect(f.query, '附近 地点');
      expect(f.body['mapBounds'], _bounds.toJson());
      f.filters = _filters();
      await f.submit('全城地点');
      expect(f.query, '全城地点');
      expect(f.body.containsKey('mapBounds'), isFalse);
      expect(f.workspace.task!.lastSuccessfulPublicQuery!.bounds, isNull);
    },
  );

  test('a new explicit map bounds replaces old inherited bounds', () async {
    final f = _Fixture()..filters = _filters(viewport: true);
    addTearDown(f.dispose);
    await f.submit('附近地点');
    const changed = MapBounds(west: -3, south: 56, east: -2.5, north: 56.5);
    await f.workspace.searchThisArea(
      [],
      [],
      bounds: changed,
      cityID: f.city,
      query: '地点',
    );
    expect(f.body['mapBounds'], changed.toJson());
    expect(f.query, '地点');
  });

  test(
    'name restoration preserves St. Mary apostrophe and internal period',
    () async {
      final f = _Fixture()..filters = _filters(term: "st. mary's library");
      addTearDown(f.dispose);
      await f.submit("找地点 St. Mary's library");
      final task = f.workspace.task!;
      await f.source.restore(task, [], []);
      expect(f.query, '找地点 "st. mary\'s library" 全城');
      expect(f.body.containsKey('taskId'), isFalse);
      await f.source.followUp(task, '附近的呢', [], []);
      expect(f.query, '找地点 "st. mary\'s library" 附近的呢');
    },
  );

  test(
    'normalized names with operation words are serialized as quoted values',
    () async {
      final f = _Fixture()..filters = _filters(term: '发布活动');
      addTearDown(f.dispose);
      await f.submit('地点');
      await f.source.restore(f.workspace.task!, [], []);
      expect(f.query, '找地点 "发布活动" 全城');
    },
  );

  test(
    'explicit quoted name stays first and cannot inherit an activity intent',
    () async {
      final f = _Fixture()
        ..filters = {
          'targetIntent': 'FIND_ACTIVITY',
          'category': 'badminton',
          'timePreference': 'weekend',
          'locationPreference': 'city',
        };
      addTearDown(f.dispose);
      await f.submit('找周末羽毛球活动');
      f.filters = _filters(term: 'cssa clubhouse');
      await f.submit('找地点 "CSSA clubhouse"');
      expect(f.query, '找地点 "CSSA clubhouse" 全城');
      expect(f.query, isNot(contains('羽毛球')));
      expect(f.query, isNot(contains('周末')));
    },
  );

  test(
    'place context never copies stale activity or arbitrary evidence fields',
    () {
      final context = LocalPublicQueryContext.decode(
        {
          ..._filters(term: 'library'),
          'category': 'badminton',
          'timePreference': 'weekend',
          'distancePreference': 'closer',
          'action': 'SEND_MESSAGE',
          'evidenceText': 'untrusted text',
        },
        'aberdeen',
        Object(),
      );
      expect(context!.slots, {
        'targetIntent': 'FIND_PLACE',
        'locationPreference': 'city',
      });
      expect(context.placeSearchTerm, 'library');
    },
  );

  test(
    'malformed or excessive place search values cannot become local context',
    () {
      for (final value in [
        null,
        42,
        ' padded ',
        'line\nname',
        'nul\u0000name',
        '名' * 81,
      ]) {
        expect(
          LocalPublicQueryContext.decode(
            {..._filters(), 'searchTerm': value},
            'aberdeen',
            Object(),
          ),
          isNull,
        );
      }
    },
  );

  for (final change in ['auth', 'city', 'organization', 'online', 'epoch']) {
    test(
      '$change ABA cannot revive an anonymous successful place context',
      () async {
        final f = _Fixture()..filters = _filters(viewport: true);
        addTearDown(f.dispose);
        await f.submit('附近地点');
        final task = f.workspace.task!;
        final requestCount = f.requests.length;
        if (change == 'auth') f.token = 'Bearer own';
        if (change == 'city') f.city = 'different-city';
        if (change == 'organization') f.organization = 'organization';
        if (change == 'online') f.online = 'online';
        f.epoch++;
        f.token = f.organization = f.online = null;
        f.city = 'aberdeen';
        f.epoch++;
        await expectLater(
          f.source.restore(task, [], []),
          throwsA(isA<AgentRequestFailure>()),
        );
        expect(f.requests, hasLength(requestCount));
        await f.source.followUp(task, '地点', [], []);
        expect(f.query, '地点');
        expect(f.body.containsKey('mapBounds'), isFalse);
        expect(task.lastSuccessfulPublicQuery!.current, isFalse);
      },
    );
  }

  test(
    'different transport source cannot inherit the prior name or bounds',
    () async {
      final f = _Fixture()..filters = _filters(term: 'library', viewport: true);
      final other = _Fixture();
      addTearDown(f.dispose);
      addTearDown(other.dispose);
      await f.submit('附近图书馆地点');
      final task = f.workspace.task!;
      await other.source.followUp(task, '地点', [], []);
      expect(other.query, '地点');
      expect(other.body.containsKey('mapBounds'), isFalse);
      await expectLater(
        f.source.restore(task, [], []),
        throwsA(isA<AgentRequestFailure>()),
      );
    },
  );

  test(
    'late anonymous response after epoch ABA cannot retain query context',
    () async {
      final f = _Fixture()..pending = Completer<http.Response>();
      addTearDown(f.dispose);
      final read = f.source.resolve('地点', [], []);
      await Future<void>.delayed(Duration.zero);
      f.epoch += 2;
      f.pending!.complete(
        http.Response.bytes(
          utf8.encode(jsonEncode(_wire(f.city, _filters(viewport: true)))),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        ),
      );
      final result = await read;
      expect(result.resultSet!.publicQueryContext, isNull);
    },
  );

  test(
    'historical map projection does not replace latest place followup context',
    () async {
      final f = _Fixture();
      addTearDown(f.dispose);
      await f.submit('地点');
      final first = f.workspace.result!;
      f.filters = _filters(term: 'library');
      await f.submit('图书馆地点');
      final latest = f.workspace.task!.lastSuccessfulPublicQuery;
      f.workspace.showReplyOnMap(first, 'place:one');
      expect(f.workspace.presentedResult, same(first));
      expect(f.workspace.task!.lastSuccessfulPublicQuery, same(latest));
      await f.source.restore(f.workspace.task!, [], []);
      expect(f.query, '找地点 "library" 全城');
    },
  );
}
