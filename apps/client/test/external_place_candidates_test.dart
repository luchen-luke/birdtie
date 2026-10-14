import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/agent_answer_sources.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/workspace/agent_reply_membership.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/external_place_candidates.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_answer_sources_test.dart' show sourcedWire;
import 'now_scope_recovery_test.dart' show NowFixture, nowSend, nowTap;

const _query = '有没有好的餐厅';
const _taskID = '00000000-0000-4000-8000-000000000901';

// Entirely synthetic OSM observation; these tests do not search live services.
Map<String, dynamic> externalSearchWire(
  DateTime now, {
  String requestID = 'request-one',
  String cityID = 'alpha',
  String query = _query,
  String status = 'SUCCESS',
}) {
  final until = now.add(const Duration(minutes: 10));
  return {
    'schema': onlinePlaceSearchSchema,
    'requestId': requestID,
    'cityId': cityID,
    'query': query,
    'status': status,
    'provider': 'openstreetmap',
    'attribution': openStreetMapAttribution,
    'scope': 'city',
    'observedAt': now.toIso8601String(),
    'expiresAt': until.toIso8601String(),
    'places': [
      if (status == 'SUCCESS')
        {
          'id': 'osm:node:123456789',
          'cityId': cityID,
          'name': '合成餐厅 · 非真实推荐',
          'categoryCode': 'restaurant',
          'addressLabel': '合成街道 1 号',
          'location': {
            'coordinateSystem': 'wgs84',
            'precision': 'point',
            'latitude': 57.15,
            'longitude': -2.1,
          },
          'sourceUrl': 'https://www.openstreetmap.org/node/123456789',
          'attribution': openStreetMapAttribution,
          'observedAt': now.toIso8601String(),
          'expiresAt': until.toIso8601String(),
          'reviewStatus': 'UNREVIEWED',
        },
    ],
  };
}

Map<String, dynamic> externalQueryWire(
  DateTime now,
  String requestID, {
  bool signed = false,
  String status = 'SUCCESS',
  String query = _query,
}) {
  const persisted = '外部结果仅本轮有效，请重新搜索查看最新信息。';
  final digest = agentReplyTurnDigest([
    (role: 'user', text: query),
    (role: 'assistant', text: persisted),
  ]);
  final setID = signed
      ? 'reply:$_taskID:${digest.substring(0, 24)}'
      : requestID;
  return {
    'cityId': 'alpha',
    'query': query,
    'mode': 'rules',
    'principalType': 'PERSON',
    'principalId': signed ? 'owner' : '',
    'workspace': 'PERSONAL',
    'requestId': requestID,
    'activities': [],
    'places': [],
    'people': [],
    'groups': [],
    'organizations': [],
    'actions': [],
    'followUps': [],
    'message': status == 'SUCCESS'
        ? '已联网查到 1 家餐厅，尚未核实评分。'
        : status == 'UNAVAILABLE'
        ? '联网查询暂不可用，请稍后重试。'
        : '本次联网查询暂无餐厅结果。',
    'taskId': signed ? _taskID : null,
    'conversationId': signed ? _taskID : null,
    'task': !signed
        ? null
        : {
            'id': _taskID,
            'query': query,
            'status': 'COMPLETED',
            'intent': 'FIND_PLACE',
            'cityContext': 'alpha',
            'principalType': 'PERSON',
            'principalId': 'owner',
            'actingUserId': 'owner',
            'filters': {'currentQuery': query},
            'conversation': [
              {'role': 'user', 'text': query},
              {
                'role': 'assistant',
                'text': persisted,
                'resultMembership': {
                  'schema': agentReplyMembershipSchema,
                  'taskId': _taskID,
                  'cityId': 'alpha',
                  'kind': 'place',
                  'turnDigest': digest,
                  'resultSetId': setID,
                  'refs': [],
                },
              },
            ],
            'updatedAt': now.toIso8601String(),
          },
    'messageResults': signed ? <dynamic>[] : null,
    'resultSet': {
      'id': setID,
      'schema': typedAgentResultSchema,
      'status': 'empty',
      'generatedAt': now.toIso8601String(),
      'entities': [],
      'items': [],
      'filters': {},
    },
    'mapEffects': {'camera': 'preserve', 'pinEntityIds': []},
    'onlinePlaceSearch': externalSearchWire(
      now,
      requestID: requestID,
      status: status,
      query: query,
    ),
  };
}

http.Response _reply(Map<String, dynamic> data) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': data})),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

void main() {
  final originalNow = DateTime.utc(2026, 10, 10, 5);

  ExternalPlaceSearch read(
    Map<String, dynamic> wire, {
    bool Function()? current,
    DateTime Function()? now,
    MapBounds? bounds,
  }) => ExternalPlaceSearch.read(
    wire,
    requestID: 'request-one',
    cityID: 'alpha',
    query: _query,
    current: current ?? () => true,
    now: now ?? () => originalNow,
    bounds: bounds,
  )!;

  test(
    'verified external observation uses distinct map ID and immutable candidates',
    () {
      final search = read(externalSearchWire(originalNow));
      expect(search.places.single.mapID, 'external-place:osm:node:123456789');
      expect(search.places.single.mapEntity.kind, MapEntityKind.place);
      expect(search.places.single.mapEntity.hasMapAnchor, true);
      expect(search.places.single.mapEntity.placeID, isEmpty);
      expect(() => search.places.clear(), throwsUnsupportedError);
    },
  );

  for (final status in ['EMPTY', 'UNAVAILABLE']) {
    test('$status carries no invented restaurant or pin', () {
      final search = read(externalSearchWire(originalNow, status: status));
      expect(search.status, status);
      expect(search.places, isEmpty);
    });
  }

  for (final invalid in [
    'request',
    'city',
    'query',
    'provider',
    'url',
    'id',
    'duplicate',
    'coordinate',
    'precision',
    'expired',
    'future',
    'duration',
    'review',
    'attribution',
    'candidate-time',
    'scope',
    'extra',
  ]) {
    test('rejects unbound or invalid $invalid before cards/pins', () {
      final wire = externalSearchWire(originalNow);
      final place = (wire['places'] as List).single as Map<String, dynamic>;
      switch (invalid) {
        case 'request':
          wire['requestId'] = 'other';
        case 'city':
          wire['cityId'] = 'other';
        case 'query':
          wire['query'] = '其他问题';
        case 'provider':
          wire['provider'] = 'unbound';
        case 'url':
          place['sourceUrl'] = 'https://evil.example/node/123456789';
        case 'id':
          place['id'] = 'native-place-id';
        case 'duplicate':
          (wire['places'] as List).add(place);
        case 'coordinate':
          place['location']['latitude'] = 91;
        case 'precision':
          place['location']['precision'] = 'area';
        case 'expired':
          wire['expiresAt'] = originalNow.toIso8601String();
        case 'future':
          wire['observedAt'] = originalNow
              .add(const Duration(minutes: 1))
              .toIso8601String();
        case 'duration':
          wire['expiresAt'] = originalNow
              .add(const Duration(hours: 1))
              .toIso8601String();
        case 'review':
          place['reviewStatus'] = 'PUBLISHED';
        case 'attribution':
          place['attribution'] = 'none';
        case 'candidate-time':
          place['expiresAt'] = originalNow
              .add(const Duration(minutes: 11))
              .toIso8601String();
        case 'scope':
          wire['scope'] = 'world';
        case 'extra':
          wire['places'][0]['rating'] = 5;
      }
      expect(() => read(wire), throwsFormatException);
    });
  }

  test('viewport observation must stay inside submitted bounds', () {
    final wire = externalSearchWire(originalNow)..['scope'] = 'viewport';
    const bounds = MapBounds(
      west: -2.11,
      south: 57.14,
      east: -2.09,
      north: 57.16,
    );
    expect(read(wire, bounds: bounds).places, hasLength(1));
    wire['places'][0]['location']['longitude'] = -2.2;
    expect(() => read(wire, bounds: bounds), throwsFormatException);
  });

  test('expiry and observed context loss cannot revive after returning', () {
    var now = originalNow;
    var active = true;
    final search = read(
      externalSearchWire(originalNow),
      now: () => now,
      current: () => active,
    );
    active = false;
    expect(search.places, isEmpty);
    active = true;
    expect(search.current, false);
    final expiring = read(externalSearchWire(originalNow), now: () => now);
    now = now.add(const Duration(minutes: 10));
    expect(expiring.places, isEmpty);
    now = originalNow;
    expect(expiring.current, false);
  });

  for (final signed in [false, true]) {
    test(
      'Remote ${signed ? 'signed empty history' : 'guest'} gets current candidates without native items',
      () async {
        final client = MockClient((r) async {
          expect(r.url.path, '/v1/cities/alpha/agent/tasks');
          expect(jsonDecode(r.body), {'query': _query});
          expect(r.headers['Authorization'], signed ? 'Bearer owner' : isNull);
          return _reply(
            externalQueryWire(
              originalNow,
              r.headers['X-Request-ID']!,
              signed: signed,
            ),
          );
        });
        final source = RemoteAgentTaskSource(
          cityID: () => 'alpha',
          authorizationHeader: () => signed ? 'Bearer owner' : null,
          publicEvidenceOwnerID: () => signed ? 'owner' : null,
          publicEvidenceNow: () => originalNow,
          client: client,
          apiBaseUrl: 'https://fixture.test',
        );
        final workspace = AgentWorkspaceController(source: source);
        await workspace.submit(_query, [], [], cityID: 'alpha');
        expect(workspace.requestError, isNull);
        expect(workspace.result!.places, isEmpty);
        expect(workspace.result!.projectionItems, isEmpty);
        expect(
          workspace.result!.entities.single.id,
          'external-place:osm:node:123456789',
        );
        expect(workspace.queryState, AgentQueryState.success);
        expect(workspace.replies.single.result, same(workspace.result));
        expect(workspace.conversation.last.text, externalPlaceHistoryNotice);
        final place = workspace.result!.onlinePlaceSearch!.places.single;
        workspace.showExternalPlaceOnMap(workspace.result!, place);
        expect(workspace.selectedEntityId, place.mapID);
        workspace.newTask(preserveMapSelection: true);
        expect(workspace.mapBackgroundResult?.entities, isEmpty);
        workspace.dispose();
        client.close();
      },
    );
  }

  test('missing external field keeps existing native empty behavior', () async {
    final source = RemoteAgentTaskSource(
      cityID: () => 'alpha',
      authorizationHeader: () => null,
      publicEvidenceNow: () => originalNow,
      apiBaseUrl: 'https://fixture.test',
      client: MockClient((r) async {
        final wire = externalQueryWire(originalNow, r.headers['X-Request-ID']!);
        wire.remove('onlinePlaceSearch');
        return _reply(wire);
      }),
    );
    final workspace = AgentWorkspaceController(source: source);
    await workspace.submit(_query, [], [], cityID: 'alpha');
    expect(workspace.queryState, AgentQueryState.empty);
    expect(workspace.result!.onlinePlaceSearch, isNull);
    expect(workspace.result!.entities, isEmpty);
    workspace.dispose();
  });

  test('native history read never reconstructs external source facts', () async {
    final client = MockClient((r) async {
      expect(r.method, 'GET');
      final wire = externalQueryWire(
        originalNow,
        'history-request',
        signed: true,
      );
      // Even an accidental old field from history is never a live observation.
      wire['onlinePlaceSearch']['places'][0]['name'] = '历史外部地点';
      return _reply(wire);
    });
    final source = RemoteAgentTaskSource(
      cityID: () => 'alpha',
      authorizationHeader: () => 'Bearer owner',
      publicEvidenceOwnerID: () => 'owner',
      publicEvidenceNow: () => originalNow,
      apiBaseUrl: 'https://fixture.test',
      client: client,
    );
    final restored = await source.restore(
      const AgentTask(
        id: _taskID,
        query: _query,
        status: 'COMPLETED',
        cityID: 'alpha',
      ),
      [],
      [],
    );
    expect(restored.onlinePlaceSearch, isNull);
    expect(restored.entities, isEmpty);
    expect(restored.places, isEmpty);
    source.dispose();
    client.close();
  });

  test('explicit city response may discard a prior submitted viewport', () {
    const oldViewport = MapBounds(west: -4, south: 55, east: -3, north: 56);
    expect(
      read(externalSearchWire(originalNow), bounds: oldViewport).places,
      hasLength(1),
    );
  });

  for (final changed in ['city', 'account', 'epoch']) {
    test(
      'Remote $changed change retires external cards/pins permanently',
      () async {
        String city = 'alpha';
        String? token;
        Object epoch = Object();
        final source = RemoteAgentTaskSource(
          cityID: () => city,
          authorizationHeader: () => token,
          publicEvidenceEpoch: () => epoch,
          publicEvidenceNow: () => originalNow,
          apiBaseUrl: 'https://fixture.test',
          client: MockClient(
            (r) async => _reply(
              externalQueryWire(originalNow, r.headers['X-Request-ID']!),
            ),
          ),
        );
        final result = await source.resolve(_query, [], []);
        expect(result.entities, hasLength(1));
        if (changed == 'city') city = 'beta';
        if (changed == 'account') token = 'Bearer another';
        if (changed == 'epoch') epoch = Object();
        expect(result.entities, isEmpty);
        city = 'alpha';
        token = null;
        expect(result.onlinePlaceSearch!.current, false);
        source.dispose();
      },
    );
  }

  test(
    'late old response cannot replace the current candidate observation',
    () async {
      final held = Completer<http.Response>();
      String? oldRequest;
      var calls = 0;
      final source = RemoteAgentTaskSource(
        cityID: () => 'alpha',
        authorizationHeader: () => null,
        publicEvidenceNow: () => originalNow,
        apiBaseUrl: 'https://fixture.test',
        client: MockClient((r) async {
          if (++calls == 1) {
            oldRequest = r.headers['X-Request-ID'];
            return held.future;
          }
          return _reply(
            externalQueryWire(
              originalNow,
              r.headers['X-Request-ID']!,
              query: '附近餐厅',
            ),
          );
        }),
      );
      final workspace = AgentWorkspaceController(source: source);
      final first = workspace.submit(_query, [], [], cityID: 'alpha');
      await Future<void>.delayed(Duration.zero);
      await workspace.submit('附近餐厅', [], [], cityID: 'alpha');
      final current = workspace.result;
      held.complete(_reply(externalQueryWire(originalNow, oldRequest!)));
      await first;
      expect(workspace.result, same(current));
      expect(workspace.result!.onlinePlaceSearch!.query, '附近餐厅');
      expect(workspace.result!.entities, hasLength(1));
      workspace.dispose();
    },
  );

  test(
    'a source-grounded text answer is successful with zero native entities',
    () {
      final wire = sourcedWire(originalNow);
      final answer = AgentAnswerSources.read(
        wire,
        current: () => true,
        now: () => originalNow,
      )!;
      final result = AgentResult(
        entities: [],
        activities: [],
        places: [],
        note: '',
        message: wire['message'] as String,
        resultSet: AgentResultSet(
          id: 'sourced',
          status: 'empty',
          generatedAt: originalNow,
          entities: [],
          items: [],
          answerSources: answer,
        ),
      );
      final workspace = AgentWorkspaceController()..result = result;
      expect(workspace.queryState, AgentQueryState.success);
      workspace.dispose();
    },
  );

  testWidgets(
    'signed empty native history shows real current restaurant cards, map action and external detail',
    (t) async {
      final now = DateTime.now().toUtc();
      final source = RemoteAgentTaskSource(
        cityID: () => 'alpha',
        authorizationHeader: () => 'Bearer owner',
        publicEvidenceOwnerID: () => 'owner',
        publicEvidenceNow: () => now,
        apiBaseUrl: 'https://fixture.test',
        client: MockClient(
          (r) async => _reply(
            externalQueryWire(now, r.headers['X-Request-ID']!, signed: true),
          ),
        ),
      );
      final workspace = AgentWorkspaceController(source: source);
      await workspace.submit(_query, [], [], cityID: 'alpha');
      workspace.setSheetExtent(AgentSheetExtent.expanded);
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: AgentResultsSheet(
              workspace: workspace,
              availableHeight: 650,
              onSuggestion: (_) {},
              onRetry: () {},
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('已联网查到 1 家餐厅，尚未核实评分。'), findsOneWidget);
      expect(find.text('合成餐厅 · 非真实推荐'), findsOneWidget);
      expect(find.text('这条回答的结果已失效，请从最近对话重新打开。'), findsNothing);
      expect(find.byTooltip('收藏'), findsNothing);
      expect(find.text('分享给好友'), findsNothing);
      expect(find.text('OpenStreetMap 数据许可'), findsOneWidget);
      await t.tap(find.text('地图查看'));
      await t.pumpAndSettle();
      expect(workspace.selectedEntityId, 'external-place:osm:node:123456789');
      workspace.setSheetExtent(AgentSheetExtent.expanded);
      await t.pumpAndSettle();
      await t.tap(find.text('合成餐厅 · 非真实推荐'));
      await t.pumpAndSettle();
      expect(find.text('OpenStreetMap 外部来源快照 · 尚未发布到 Birdtie'), findsOneWidget);
      expect(find.text('打开原始来源').hitTestable(), findsOneWidget);
      final liveSearch = workspace.result!.onlinePlaceSearch!;
      workspace.clearAccountContext();
      expect(liveSearch.current, false);
      expect(workspace.result, isNull);
      await t.pumpAndSettle();
      expect(find.text('打开原始来源'), findsNothing);
      expect(find.text('这次联网结果已失效，请重新搜索查看最新信息。'), findsOneWidget);
      await t.pumpWidget(const SizedBox.shrink());
      workspace.dispose();
    },
  );

  testWidgets(
    'Now exact user phrase projects external pins and opens external source detail',
    (t) async {
      final fixture = NowFixture();
      addTearDown(fixture.dispose);
      final now = DateTime.now().toUtc();
      final source = RemoteAgentTaskSource(
        cityID: () => fixture.city.selectedCity?.id,
        authorizationHeader: () => null,
        publicEvidenceNow: () => now,
        apiBaseUrl: 'https://fixture.test',
        client: MockClient(
          (r) async =>
              _reply(externalQueryWire(now, r.headers['X-Request-ID']!)),
        ),
      );
      await t.pumpWidget(
        MaterialApp(
          home: MapWorkspace(
            city: fixture.city,
            auth: fixture.auth,
            moments: fixture.moments,
            agentTaskSource: source,
            seedClient: fixture.client,
            seedApiBaseUrl: fixture.base,
          ),
        ),
      );
      await t.pumpAndSettle();
      await nowSend(t, _query);
      final canvas = t.widget<MapCanvas>(find.byType(MapCanvas));
      expect(
        canvas.workspace.result!.entities.single.id,
        'external-place:osm:node:123456789',
      );
      final mapView = t.widget<PublicCityMapView>(
        find.byType(PublicCityMapView),
      );
      expect(mapView.entities.single.id, 'external-place:osm:node:123456789');
      mapView.onEntitySelected!(mapView.entities.single);
      await t.pumpAndSettle();
      expect(canvas.workspace.selectedEntityId, mapView.entities.single.id);
      expect(find.text('找到 0 个已发布地点。'), findsNothing);
      canvas.workspace.setSheetExtent(AgentSheetExtent.expanded);
      await t.pumpAndSettle();
      expect(find.text('合成餐厅 · 非真实推荐').hitTestable(), findsOneWidget);
      await nowTap(t, find.text('合成餐厅 · 非真实推荐'));
      expect(find.text('打开原始来源').hitTestable(), findsOneWidget);
      await fixture.unmount(t);
    },
  );
}
