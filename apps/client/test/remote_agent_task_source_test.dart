import 'dart:async';
import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'agent_task_detail_page_test.dart' show taskDestinationID;
import 'dart:convert';
import 'now_context_query_api_test.dart'
    show onlineWire, onlineContextID, onlineTaskID;
import 'agent_result_projection_test.dart' show typedItem, typedSet;

import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  for (final status in [401, 403, 503]) {
    test(
      'Recent list HTTP$status carries typed history read failure',
      () async {
        final requests = <http.Request>[];
        final source = RemoteAgentTaskSource(
          cityID: () => 'alpha',
          authorizationHeader: () => 'Bearer synthetic-owner',
          apiBaseUrl: 'https://recent-list-fixture.test',
          client: MockClient((r) async {
            requests.add(r);
            return http.Response(
              jsonEncode({
                'error': {'code': 'fixture_$status'},
              }),
              status,
            );
          }),
        );
        Object? caught;
        try {
          await source.loadRecent();
        } catch (e) {
          caught = e;
        }
        expect(requests.single.method, 'GET');
        expect(requests.single.url.path, '/v1/me/agent-tasks');
        expect(caught, isA<AgentRequestFailure>());
        expect((caught as AgentRequestFailure).statusCode, status);
        expect(caught.code, 'fixture_$status');
        source.dispose();
      },
    );
  }
  test(
    'Recent list network failure remains typed and cannot be an empty list',
    () async {
      final source = RemoteAgentTaskSource(
        cityID: () => null,
        authorizationHeader: () => 'Bearer synthetic-owner',
        apiBaseUrl: 'https://recent-list-fixture.test',
        client: MockClient((r) async {
          expect(r.method, 'GET');
          throw http.ClientException('synthetic network');
        }),
      );
      await expectLater(
        source.loadRecent(),
        throwsA(
          isA<AgentRequestFailure>().having(
            (e) => e.userMessage,
            'network feedback',
            contains('网络'),
          ),
        ),
      );
      source.dispose();
    },
  );

  test(
    'GET recovery missing personal login cannot issue historical native read',
    () async {
      var requests = 0;
      final source = RemoteAgentTaskSource(
        cityID: () => 'alpha',
        authorizationHeader: () => null,
        apiBaseUrl: 'https://get-recovery-fixture.test',
        client: MockClient((r) async {
          requests++;
          return http.Response('{}', 200);
        }),
      );
      await expectLater(
        source.restore(
          const AgentTask(
            id: taskDestinationID,
            query: '原合成查询',
            status: 'COMPLETED',
          ),
          [],
          [],
        ),
        throwsA(
          isA<AgentRequestFailure>().having((e) => e.statusCode, 'status', 401),
        ),
      );
      expect(requests, 0);
      source.dispose();
    },
  );

  for (final context in ['CITY', 'ONLINE']) {
    for (final status in [401, 403, 503]) {
      test(
        'GET recovery $context HTTP$status retains typed failure without POST',
        () async {
          final requests = <http.Request>[];
          final source = RemoteAgentTaskSource(
            cityID: () => 'alpha',
            authorizationHeader: () => 'Bearer synthetic-owner',
            apiBaseUrl: 'https://get-recovery-fixture.test',
            client: MockClient((r) async {
              requests.add(r);
              return http.Response(
                jsonEncode({
                  'error': {'code': 'fixture_$status'},
                }),
                status,
              );
            }),
          );
          Object? caught;
          try {
            await source.restore(
              AgentTask(
                id: taskDestinationID,
                query: '原合成查询',
                status: 'COMPLETED',
                contextType: context,
              ),
              [],
              [],
            );
          } catch (e) {
            caught = e;
          }
          expect(requests.single.method, 'GET');
          expect(
            requests.single.url.path,
            context == 'CITY'
                ? '/v1/me/agent-tasks/$taskDestinationID'
                : '/v1/me/now/online/tasks/$taskDestinationID',
          );
          expect(caught, isA<AgentRequestFailure>());
          expect((caught as AgentRequestFailure).statusCode, status);
          if (context == 'CITY') expect(caught.code, 'fixture_$status');
          source.dispose();
        },
      );
    }
  }
  for (final failure in ['network', 'timeout']) {
    test(
      'GET recovery CITY $failure is typed recoverable read failure',
      () async {
        final requests = <http.Request>[];
        final source = RemoteAgentTaskSource(
          cityID: () => 'alpha',
          authorizationHeader: () => 'Bearer synthetic-owner',
          apiBaseUrl: 'https://get-recovery-fixture.test',
          client: MockClient((r) async {
            requests.add(r);
            if (failure == 'network') {
              throw http.ClientException('synthetic network');
            }
            throw TimeoutException('synthetic timeout');
          }),
        );
        Object? caught;
        try {
          await source.restore(
            const AgentTask(
              id: taskDestinationID,
              query: '原合成查询',
              status: 'COMPLETED',
            ),
            [],
            [],
          );
        } catch (e) {
          caught = e;
        }
        expect(requests.single.method, 'GET');
        expect(caught, isA<AgentRequestFailure>());
        expect((caught as AgentRequestFailure).permitsUnchangedRetry, true);
        expect(
          caught.userMessage,
          contains(failure == 'network' ? '网络' : '超时'),
        );
        source.dispose();
      },
    );
  }

  test('原生typed ResultSet统一七类卡片引用，无MapEffects来源冲突', () async {
    final items = [
      for (final k in [
        'person',
        'activity',
        'place',
        'community',
        'organization',
        'business',
      ])
        typedItem(k, 'original-$k'),
      typedItem(
        'opportunity',
        'own-intent:original-activity',
        scope: 'SELF_PRIVATE',
      ),
    ];
    var corrupt = false;
    final source = RemoteAgentTaskSource(
      cityID: () => 'fixture-city',
      authorizationHeader: () => null,
      apiBaseUrl: 'http://fixture',
      client: MockClient(
        (_) async => http.Response(
          jsonEncode({
            'data': {
              'cityId': 'fixture-city',
              'activities': [],
              'people': [],
              'places': [],
              'groups': [],
              'organizations': [],
              'message': '先回复，再给真实引用的卡片',
              'resultSet': {
                ...typedSet(items),
                'id': 'set-native',
                'status': 'ready',
                'generatedAt': '2026-10-04T00:00:00Z',
              },
              'mapEffects': {
                'camera': 'preserve',
                'pinEntityIds': corrupt ? ['person:inferred'] : [],
              },
            },
          }),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        ),
      ),
    );
    final result = await source.resolve('找内容', [], []);
    expect(result.projectionItems!.length, 7);
    expect(result.entities, isEmpty);
    for (var n = 0; n < 7; n++) {
      expect(result.resultSet!.entities[n], result.projectionItems![n].entity);
    }
    expect(
      result.projectionItems!.last.share!.mapID,
      'activity:original-activity',
    );
    corrupt = true;
    await expectLater(source.resolve('找内容', [], []), throwsA(anything));
    source.dispose();
  });
  test(
    'explicit ONLINE resolves follows up restores same native Task without city or map body',
    () async {
      final calls = <http.Request>[];
      final source = RemoteAgentTaskSource(
        cityID: () => null,
        onlineContextID: () => onlineContextID,
        authorizationHeader: () => 'Bearer own',
        apiBaseUrl: 'http://localhost',
        client: MockClient((r) async {
          calls.add(r);
          return http.Response(
            jsonEncode({'data': onlineWire()}),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        }),
      );
      final first = await source.resolve('阅读', [], []);
      expect(first.task!.contextType, 'ONLINE');
      expect(first.entities, isEmpty);
      final second = await source.followUp(first.task!, '阅读', [], []);
      expect(second.taskID, onlineTaskID);
      final restored = await source.restore(second.task!, [], []);
      expect(restored.taskID, onlineTaskID);
      expect(calls.last.url.path, '/v1/me/now/online/tasks/$onlineTaskID');
      for (final req in calls.where((r) => r.method == 'POST')) {
        final body = jsonDecode(req.body) as Map;
        expect(body.containsKey('mapBounds'), false);
        expect(body.containsKey('cityId'), false);
      }
      expect(
        (jsonDecode(calls[1].body) as Map)['expectedTaskUpdatedAt'],
        '2026-10-04T04:00:00.123456Z',
      );
      source.dispose();
    },
  );
  test(
    'anonymous area follow-up keeps query slots without sending local ID',
    () async {
      final requests = <http.Request>[];
      final source = RemoteAgentTaskSource(
        cityID: () => 'aberdeen-gb',
        authorizationHeader: () => null,
        apiBaseUrl: 'http://localhost:8080',
        client: MockClient((request) async {
          requests.add(request);
          return http.Response(
            jsonEncode({
              'data': {
                'cityId': 'aberdeen-gb',
                'mode': 'rules',
                'activities': [],
                'people': [],
                'groups': [],
                'places': [],
              },
            }),
            200,
          );
        }),
      );
      await source.searchArea(
        const AgentTask(
          id: 'local-1',
          query: 'Find badminton this weekend',
          status: 'COMPLETED',
        ),
        '搜索此区域',
        const MapBounds(west: -2.2, south: 57.1, east: -2.0, north: 57.3),
        [],
        [],
      );
      final body = jsonDecode(requests.single.body) as Map<String, dynamic>;
      expect(body['taskId'], isNull);
      expect(body['query'], 'Find badminton this weekend. 搜索此区域');
      expect((body['mapBounds'] as Map<String, dynamic>)['west'], -2.2);
      source.dispose();
    },
  );
  test('place result and map pin use the same public entity ID', () async {
    final source = RemoteAgentTaskSource(
      cityID: () => 'aberdeen-gb',
      authorizationHeader: () => null,
      apiBaseUrl: 'http://localhost:8080',
      client: MockClient(
        (request) async => http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': {
                'cityId': 'aberdeen-gb',
                'mode': 'rules',
                'message': '找到 1 个地点。',
                'activities': [],
                'people': [],
                'groups': [],
                'organizations': [],
                'places': [
                  {
                    'id': 'place-1',
                    'name': '测试体育馆',
                    'categoryCode': 'sports_venue',
                    'summary': '',
                    'source': {'label': '测试数据'},
                    'location': {
                      'coordinateSystem': 'wgs84',
                      'precision': 'point',
                      'latitude': 57.14,
                      'longitude': -2.1,
                    },
                  },
                ],
                'resultSet': {
                  'id': 'set-1',
                  'status': 'ready',
                  'generatedAt': '2026-10-01T02:00:00Z',
                  'entities': [
                    {'type': 'place', 'id': 'place-1'},
                  ],
                },
                'mapEffects': {
                  'camera': 'preserve',
                  'pinEntityIds': ['place:place-1'],
                },
              },
            }),
          ),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        ),
      ),
    );
    final result = await source.resolve('找体育馆', [], []);
    expect(result.entities.single.id, 'place:place-1');
    expect(result.resultSet?.entities.single.mapID, result.entities.single.id);
    expect(result.mapEffects?.pinEntityIDs.single, result.entities.single.id);
    source.dispose();
  });
  test(
    'person coordinates without broad-zone opt in never reach map pins',
    () async {
      final source = RemoteAgentTaskSource(
        cityID: () => 'aberdeen-gb',
        authorizationHeader: () => null,
        apiBaseUrl: 'http://localhost:8080',
        client: MockClient(
          (request) async => http.Response.bytes(
            utf8.encode(
              jsonEncode({
                'data': {
                  'cityId': 'aberdeen-gb',
                  'mode': 'rules',
                  'activities': [],
                  'places': [],
                  'groups': [],
                  'people': [
                    {
                      'accountId': 'private-area',
                      'displayName': '匿名用户',
                      'topic': '羽毛球',
                      'areaLabel': '阿伯丁',
                      'mapLatitude': 57.14,
                      'mapLongitude': -2.1,
                    },
                    {
                      'accountId': 'public-area',
                      'displayName': '已公开区域用户',
                      'topic': '羽毛球',
                      'areaLabel': '阿伯丁北区',
                      'publicMapZone': 'north',
                      'mapLatitude': 57.18,
                      'mapLongitude': -2.1,
                    },
                  ],
                },
              }),
            ),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          ),
        ),
      );
      final result = await source.resolve('找羽毛球伙伴', [], []);
      expect(result.people.first.mapLatitude, isNull);
      expect(result.people.first.mapLongitude, isNull);
      expect(result.entities.map((entity) => entity.id), [
        'person:public-area',
      ]);
      source.dispose();
    },
  );
  test('real organization results remain visible as organizations', () async {
    final source = RemoteAgentTaskSource(
      cityID: () => 'aberdeen-gb',
      authorizationHeader: () => null,
      apiBaseUrl: 'http://localhost:8080',
      client: MockClient(
        (request) async => http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': {
                'cityId': 'aberdeen-gb',
                'mode': 'rules',
                'activities': [],
                'people': [],
                'groups': [],
                'places': [],
                'organizations': [
                  {
                    'id': 'org-1',
                    'name': '华人学生会',
                    'description': '学生社团',
                    'verificationStatus': 'unverified',
                  },
                ],
                'message': '找到 1 个公开组织。',
                'requestId': 'request-1',
                'resultSet': {
                  'id': 'result-1',
                  'status': 'ready',
                  'generatedAt': '2026-10-01T02:00:00Z',
                  'entities': [
                    {'type': 'organization', 'id': 'org-1'},
                  ],
                },
                'actions': [],
                'followUps': [],
                'mapEffects': {'camera': 'preserve', 'pinEntityIds': []},
              },
            }),
          ),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        ),
      ),
    );
    final result = await source.resolve('找学生社团', [], []);
    expect(result.organizations.single.name, '华人学生会');
    expect(result.organizations.single.verificationStatus, 'unverified');
    expect(result.responseMessage, '找到 1 个公开组织。');
    expect(result.requestID, 'request-1');
    expect(result.resultSet?.entities.single.mapID, 'organization:org-1');
    expect(result.mapEffects?.pinEntityIDs, isEmpty);
    source.dispose();
  });
  test('anonymous local Recent reruns the public City query', () async {
    final requests = <http.Request>[];
    final source = RemoteAgentTaskSource(
      cityID: () => 'aberdeen',
      authorizationHeader: () => null,
      apiBaseUrl: 'http://localhost:8080',
      client: MockClient((request) async {
        requests.add(request);
        return http.Response(
          jsonEncode({
            'data': {
              'cityId': 'aberdeen',
              'mode': 'rules',
              'activities': [],
              'people': [],
              'groups': [],
              'places': [],
            },
          }),
          200,
        );
      }),
    );
    await source.restore(
      const AgentTask(id: 'local-1', query: 'badminton', status: 'active'),
      [],
      [],
    );
    expect(requests, hasLength(1));
    expect(requests.single.method, 'POST');
    expect(requests.single.url.path, '/v1/cities/aberdeen/agent/tasks');
    expect(requests.single.headers.containsKey('Authorization'), isFalse);
    source.dispose();
  });

  test(
    'signed-in follow-up sends the same task and organization workspace',
    () async {
      final requests = <http.Request>[];
      final source = RemoteAgentTaskSource(
        cityID: () => 'aberdeen-gb',
        authorizationHeader: () => 'Bearer local-session',
        organizationWorkspaceID: () => 'org-1',
        apiBaseUrl: 'http://localhost:8080',
        client: MockClient((request) async {
          requests.add(request);
          return http.Response(
            jsonEncode({
              'data': {
                'cityId': 'aberdeen-gb',
                'mode': 'rules',
                'taskId': 'task-1',
                'activities': [],
                'people': [],
                'groups': [],
                'places': [],
                'note': 'Showing closer activities.',
              },
            }),
            200,
          );
        }),
      );
      await source.followUp(
        const AgentTask(
          id: 'task-1',
          query: 'Find badminton this weekend',
          status: 'COMPLETED',
        ),
        'Anything closer?',
        [],
        [],
      );
      expect(requests, hasLength(1));
      expect(requests.single.method, 'POST');
      expect(jsonDecode(requests.single.body), {
        'query': 'Anything closer?',
        'taskId': 'task-1',
      });
      expect(requests.single.headers['Authorization'], 'Bearer local-session');
      expect(
        requests.single.headers['X-Birdtie-Organization-Workspace'],
        'org-1',
      );
      source.dispose();
    },
  );

  test(
    'anonymous follow-up carries prior intent without a local fake task ID',
    () async {
      final requests = <http.Request>[];
      final source = RemoteAgentTaskSource(
        cityID: () => 'aberdeen-gb',
        authorizationHeader: () => null,
        apiBaseUrl: 'http://localhost:8080',
        client: MockClient((request) async {
          requests.add(request);
          return http.Response(
            jsonEncode({
              'data': {
                'cityId': 'aberdeen-gb',
                'mode': 'rules',
                'activities': [],
                'people': [],
                'groups': [],
                'places': [],
              },
            }),
            200,
          );
        }),
      );
      await source.followUp(
        const AgentTask(
          id: 'local-1',
          query: 'Find badminton this weekend',
          status: 'COMPLETED',
        ),
        'Anything closer?',
        [],
        [],
      );
      expect(jsonDecode(requests.single.body), {
        'query': 'Find badminton this weekend. Anything closer?',
      });
      source.dispose();
    },
  );
}
