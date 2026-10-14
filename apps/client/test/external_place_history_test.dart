import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/external_place_candidates.dart';
import 'package:birdtie_client/src/workspace/external_place_history_store.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'external_place_candidates_test.dart' show externalQueryWire;

// Implements the injected storage API in memory; no plugin channel is invoked.
class _FakeSecureStorage implements FlutterSecureStorage {
  final values = <String, String>{};
  var writes = 0;
  @override
  dynamic noSuchMethod(Invocation invocation) {
    final key = invocation.namedArguments[#key] as String;
    if (invocation.memberName == #read) {
      return Future<String?>.value(values[key]);
    }
    if (invocation.memberName == #write) {
      values[key] = invocation.namedArguments[#value] as String;
      ++writes;
      return Future<void>.value();
    }
    return super.noSuchMethod(invocation);
  }
}

const _query = '有没有好的餐厅';
const _api = 'https://public-history.test';
const _owner = '40000000-0000-4000-8000-000000000001';
const _taskID = '40000000-0000-4000-8000-000000000002';

Map<String, dynamic> _copy(Map<String, dynamic> value) =>
    jsonDecode(jsonEncode(value)) as Map<String, dynamic>;

// Captured by root from the actual Go API. Only private owner/task IDs were
// replaced by offline UUIDs; protocol casing, source facts and times are exact.
Map<String, dynamic> _actualHistory() =>
    jsonDecode(
          File(
            'test/fixtures/online_restaurant_v2_actual_history_response.json',
          ).readAsStringSync(),
        )
        as Map<String, dynamic>;

Map<String, dynamic> _actualGuestSearch() =>
    (jsonDecode(
              File(
                '../../docs/testing/evidence/restaurant-history-cache-2026-10-10/api-01/guest-query.json',
              ).readAsStringSync(),
            )
            as Map<String, dynamic>)['onlinePlaceSearch']
        as Map<String, dynamic>;

Map<String, dynamic> _search(
  DateTime now,
  String requestID,
  String query, {
  int turn = 1,
  String status = 'SUCCESS',
}) {
  final result = _copy(_actualGuestSearch());
  result.addAll({
    'requestId': requestID, 'cityId': 'alpha', 'query': query,
    'turnDigest': sha256.convert(utf8.encode(query)).toString(),
    'snapshotId': '00000000-0000-4000-8000-${turn.toString().padLeft(12, '0')}',
    'messageIndex': 1,
    'status': status,
    'origin': 'CACHE',
    'freshness': 'FRESH',
    'observedAt': now.toIso8601String(),
    'expiresAt': now.add(const Duration(hours: 24)).toIso8601String(),
    // Anonymous Go response omits the empty taskId via omitempty.
  });
  result.remove('taskId');
  result['places'] = status != 'SUCCESS'
      ? []
      : [
          for (final raw in (result['places'] as List).take(3))
            (raw as Map<String, dynamic>)..addAll({
              'cityId': 'alpha',
              'observedAt': result['observedAt'],
              'expiresAt': result['expiresAt'],
            }),
        ];
  return result;
}

Map<String, dynamic> _queryWire(
  DateTime now,
  String requestID,
  String query, {
  int turn = 1,
  String status = 'SUCCESS',
}) => externalQueryWire(now, requestID, query: query, status: status)
  ..addAll({
    'message': '本轮餐厅说明：$query',
    'onlinePlaceSearch': _search(
      now,
      requestID,
      query,
      turn: turn,
      status: status,
    ),
  });

http.Response _reply(Map<String, dynamic> value) => http.Response.bytes(
  utf8.encode(jsonEncode(value.containsKey('data') ? value : {'data': value})),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

void main() {
  final base = DateTime.utc(2026, 10, 10, 7);

  test(
    'actual anonymous Go v2 envelope decodes ODbL URL and omitted empty taskId',
    () {
      final raw = _actualGuestSearch();
      final now = DateTime.parse(
        raw['observedAt'] as String,
      ).add(const Duration(minutes: 5));
      final search = ExternalPlaceSearch.read(
        raw,
        requestID: raw['requestId'] as String,
        cityID: raw['cityId'] as String,
        query: raw['query'] as String,
        expectedTaskID: '',
        expectedMessageIndex: 1,
        current: () => true,
        now: () => now,
      )!;
      expect(search.places, hasLength(15));
      expect(
        search.licenseUri.toString(),
        'https://opendatacommons.org/licenses/odbl/1-0/',
      );
      expect(search.taskID, isEmpty);
      expect(search.places.first.name, "Frankie & Benny's");
    },
  );

  test(
    'v2 freshness expiry keeps original facts; source revocation remains permanent',
    () {
      var now = base;
      var allowed = true;
      final search = ExternalPlaceSearch.read(
        _search(base, 'one', _query),
        requestID: 'one',
        cityID: 'alpha',
        query: _query,
        expectedTaskID: '',
        expectedMessageIndex: 1,
        current: () => allowed,
        now: () => now,
      )!;
      final original = search.places;
      now = base.add(const Duration(days: 100));
      expect(search.current, true);
      expect(search.stale, true);
      expect(search.places, same(original));
      allowed = false;
      expect(search.places, isEmpty);
      allowed = true;
      expect(search.current, false);
    },
  );

  for (final invalid in ['digest', 'snapshot', 'index', 'license', 'ttl']) {
    test('v2 rejects invalid $invalid before durable history can display', () {
      final raw = _search(base, 'one', _query);
      switch (invalid) {
        case 'digest':
          raw['turnDigest'] = '0' * 64;
        case 'snapshot':
          raw['snapshotId'] = 'osm:node:1';
        case 'index':
          raw['messageIndex'] = 3;
        case 'license':
          raw['licenseUrl'] = 'https://untrusted.test/license';
        case 'ttl':
          raw['expiresAt'] = base
              .add(const Duration(hours: 25))
              .toIso8601String();
      }
      expect(
        () => ExternalPlaceSearch.read(
          raw,
          requestID: 'one',
          cityID: 'alpha',
          query: _query,
          expectedTaskID: '',
          expectedMessageIndex: 1,
          current: () => true,
          now: () => base,
        ),
        throwsFormatException,
      );
    });
  }

  testWidgets(
    'all three restaurants are equal cards with detail/map and explicit refresh',
    (t) async {
      var now = base;
      final changes = ChangeNotifier();
      final search = ExternalPlaceSearch.read(
        _search(base, 'one', _query),
        requestID: 'one',
        cityID: 'alpha',
        query: _query,
        current: () => true,
        now: () => now,
      )!;
      var refreshes = 0;
      String? mapped;
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: SingleChildScrollView(
              child: ListenableBuilder(
                listenable: changes,
                builder: (context, _) => ExternalPlaceCandidatesPanel(
                  search: search,
                  current: () => true,
                  changes: changes,
                  onMap: (place) => mapped = place.mapID,
                  onRefresh: () => ++refreshes,
                ),
              ),
            ),
          ),
        ),
      );
      expect(find.byType(ExternalPlaceCandidateCard), findsNWidgets(3));
      expect(find.byType(ListTile), findsNothing);
      for (final place in search.places) {
        final card = find.byKey(ValueKey('external-place-${place.id}'));
        expect(
          find.descendant(of: card, matching: find.byType(Material)),
          findsWidgets,
        );
        expect(
          find.descendant(of: card, matching: find.text('查看详情')),
          findsOneWidget,
        );
        expect(
          find.descendant(of: card, matching: find.text('地图查看')),
          findsOneWidget,
        );
      }
      await t.tap(find.text('更新餐厅信息'));
      expect(refreshes, 1);
      await t.tap(find.text('地图查看').first);
      expect(mapped, search.places.first.mapID);
      now = base.add(const Duration(days: 2));
      changes.notifyListeners();
      await t.pump();
      expect(find.byType(ExternalPlaceCandidateCard), findsNWidgets(3));
      expect(find.text('OpenStreetMap · 资料需更新'), findsNWidgets(3));
      await t.tap(find.text(search.places.first.name));
      await t.pumpAndSettle();
      expect(find.text('打开原始来源'), findsOneWidget);
      expect(find.text('这些资料可能已变化，可回到对话更新餐厅信息。'), findsOneWidget);
      expect(find.text('收藏'), findsNothing);
      expect(find.text('分享给好友'), findsNothing);
      await t.pumpWidget(const SizedBox.shrink());
      changes.dispose();
    },
  );

  for (final status in ['EMPTY', 'UNAVAILABLE']) {
    testWidgets(
      'saved $status reply still provides a refresh button and timestamp',
      (t) async {
        final changes = ChangeNotifier();
        final search = ExternalPlaceSearch.read(
          _search(base, 'one', _query, status: status),
          requestID: 'one',
          cityID: 'alpha',
          query: _query,
          current: () => true,
          now: () => base,
        )!;
        var refreshed = false;
        await t.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: ExternalPlaceCandidatesPanel(
                search: search,
                current: () => true,
                changes: changes,
                onMap: (_) {},
                onRefresh: () => refreshed = true,
              ),
            ),
          ),
        );
        expect(find.textContaining('资料采集：'), findsOneWidget);
        expect(find.byType(ExternalPlaceCandidateCard), findsNothing);
        await t.tap(find.text('更新餐厅信息'));
        expect(refreshed, true);
        await t.pumpWidget(const SizedBox.shrink());
        changes.dispose();
      },
    );
  }

  test(
    'guest multi-turn facts and text survive expiry and a fresh source/controller without HTTP',
    () async {
      var now = base;
      var calls = 0;
      final store = MemoryExternalPlaceHistoryStore();
      final source = RemoteAgentTaskSource(
        cityID: () => 'alpha',
        authorizationHeader: () => null,
        publicEvidenceNow: () => now,
        apiBaseUrl: _api,
        externalPlaceHistoryStore: store,
        client: MockClient((request) async {
          expect(request.method, 'POST');
          final query = (jsonDecode(request.body) as Map)['query'] as String;
          return _reply(
            _queryWire(
              now,
              request.headers['X-Request-ID']!,
              query,
              turn: ++calls,
            ),
          );
        }),
      );
      final workspace = AgentWorkspaceController(source: source);
      await workspace.submit(_query, [], [], cityID: 'alpha');
      final first = workspace.replies.single.result;
      now = base.add(const Duration(hours: 25));
      await workspace.submit('附近餐厅', [], [], cityID: 'alpha');
      expect(workspace.replies, hasLength(2));
      expect(workspace.canUseExternalPlaces(first), true);
      expect(first.onlinePlaceSearch!.stale, true);
      expect(first.entities, hasLength(3));
      final historical = workspace.replies.first.result;
      workspace.showExternalPlaceOnMap(
        historical,
        historical.onlinePlaceSearch!.places.first,
      );
      expect(workspace.presentedResult, same(historical));
      // A selected older card only changes map presentation, never task text.
      expect(workspace.conversation.last.text, '本轮餐厅说明：附近餐厅');
      expect(workspace.task!.query, _query);
      final id = workspace.task!.id;
      final originalTexts = workspace.conversation
          .map((message) => message.text)
          .toList();
      workspace.dispose();
      var network = 0;
      final restoredSource = RemoteAgentTaskSource(
        cityID: () => 'alpha',
        authorizationHeader: () => null,
        publicEvidenceNow: () => now,
        apiBaseUrl: _api,
        externalPlaceHistoryStore: store,
        client: MockClient((request) async {
          ++network;
          throw StateError('restore must not submit/read API');
        }),
      );
      final reopened = AgentWorkspaceController(source: restoredSource);
      await reopened.loadRecent();
      expect(reopened.recent.single.id, id);
      await reopened.reopen(reopened.recent.single, [], []);
      expect(reopened.requestError, isNull);
      expect(network, 0);
      expect(
        reopened.conversation.map((message) => message.text),
        originalTexts,
      );
      expect(reopened.replies, hasLength(2));
      expect(
        reopened.replies.map(
          (reply) => reply.result.onlinePlaceSearch!.places.length,
        ),
        [3, 3],
      );
      expect(reopened.replies.first.result.onlinePlaceSearch!.stale, true);
      final old = reopened.replies.first.result;
      expect(
        old.projectionItems,
        isEmpty,
      ); // explicit empty map shell, no native refs
      reopened.showExternalPlaceOnMap(old, old.onlinePlaceSearch!.places.last);
      expect(reopened.presentedResult!.entities, hasLength(3));
      expect(reopened.replies.last.result.onlinePlaceSearch!.taskID, isEmpty);
      expect(reopened.replies.last.result.onlinePlaceSearch!.messageIndex, 1);
      reopened.dispose();
    },
  );

  test(
    'secure store retains more than 50 conversations and rejects a full write without evicting old content',
    () async {
      final storage = _FakeSecureStorage();
      final store = SecureExternalPlaceHistoryStore(storage: storage);
      for (var index = 0; index < 51; index++) {
        await store.write(_api, {
          'id': 'local-$index',
          'text': '原文$index',
        }, current: () => true);
      }
      final previous = Map<String, String>.of(storage.values);
      expect(await store.read(_api), hasLength(51));
      await expectLater(
        store.write(_api, {
          'id': 'local-overflow',
          'text': 'x' * SecureExternalPlaceHistoryStore.maxBytes,
        }, current: () => true),
        throwsFormatException,
      );
      expect(storage.values, previous);
      expect(await store.read(_api), hasLength(51));
      expect(await store.read('https://another-public-history.test'), isEmpty);
    },
  );

  test(
    'guest history cannot be restored into another city, account or API and never falls back to POST',
    () async {
      final store = MemoryExternalPlaceHistoryStore();
      final liveSource = RemoteAgentTaskSource(
        cityID: () => 'alpha',
        authorizationHeader: () => null,
        publicEvidenceNow: () => base,
        apiBaseUrl: _api,
        externalPlaceHistoryStore: store,
        client: MockClient(
          (request) async => _reply(
            _queryWire(base, request.headers['X-Request-ID']!, _query),
          ),
        ),
      );
      final workspace = AgentWorkspaceController(source: liveSource);
      await workspace.submit(_query, [], [], cityID: 'alpha');
      final guest = workspace.task!;
      workspace.dispose();
      for (final changed in ['city', 'account', 'api']) {
        var httpCalls = 0;
        final source = RemoteAgentTaskSource(
          cityID: () => changed == 'city' ? 'beta' : 'alpha',
          authorizationHeader: () =>
              changed == 'account' ? 'Bearer different-account' : null,
          publicEvidenceNow: () => base,
          apiBaseUrl: changed == 'api' ? 'https://different-api.test' : _api,
          externalPlaceHistoryStore: store,
          client: MockClient((request) async {
            ++httpCalls;
            throw StateError('no restore fallback');
          }),
        );
        await expectLater(
          source.restore(guest, [], []),
          throwsA(isA<Exception>()),
          reason: changed,
        );
        expect(httpCalls, 0, reason: changed);
        source.dispose();
      }
    },
  );

  test(
    'late v2 response cannot overwrite the accepted task or its persisted guest snapshot',
    () async {
      final held = Completer<http.Response>();
      final store = MemoryExternalPlaceHistoryStore();
      String? oldRequest;
      var calls = 0;
      final source = RemoteAgentTaskSource(
        cityID: () => 'alpha',
        authorizationHeader: () => null,
        publicEvidenceNow: () => base,
        apiBaseUrl: _api,
        externalPlaceHistoryStore: store,
        client: MockClient((request) async {
          if (++calls == 1) {
            oldRequest = request.headers['X-Request-ID'];
            return held.future;
          }
          return _reply(
            _queryWire(base, request.headers['X-Request-ID']!, '附近餐厅', turn: 2),
          );
        }),
      );
      final workspace = AgentWorkspaceController(source: source);
      final first = workspace.submit(_query, [], [], cityID: 'alpha');
      await Future<void>.delayed(Duration.zero);
      await workspace.submit('附近餐厅', [], [], cityID: 'alpha');
      final accepted = workspace.result;
      held.complete(_reply(_queryWire(base, oldRequest!, _query)));
      await first;
      expect(workspace.result, same(accepted));
      final rows = await store.read(_api);
      expect(rows, hasLength(1));
      expect(rows.single['query'], '附近餐厅');
      expect(
        ((rows.single['conversation'] as List).last as Map)['text'],
        '本轮餐厅说明：附近餐厅',
      );
      workspace.dispose();
    },
  );

  test(
    'actual owned Go GET accepts person/PERSON, HISTORY presentation and old per-message snapshots',
    () async {
      final fixture = _actualHistory();
      final data = fixture['data'] as Map<String, dynamic>;
      final top = data['onlinePlaceSearch'] as Map<String, dynamic>;
      final original =
          ((data['task'] as Map)['conversation'] as List).last as Map;
      expect((data['task'] as Map)['principalType'], 'person');
      expect(data['principalType'], 'PERSON');
      expect(top['origin'], 'HISTORY');
      expect(
        (original['externalPlaceReply'] as Map)['origin'],
        isNot('HISTORY'),
      );
      var now = DateTime.parse(
        top['observedAt'] as String,
      ).add(const Duration(minutes: 5));
      var posts = 0;
      final source = RemoteAgentTaskSource(
        cityID: () => top['cityId'] as String,
        authorizationHeader: () => 'Bearer offline-owner',
        publicEvidenceOwnerID: () => _owner,
        publicEvidenceNow: () => now,
        apiBaseUrl: _api,
        client: MockClient((request) async {
          if (request.method == 'POST') ++posts;
          return _reply(fixture);
        }),
      );
      final workspace = AgentWorkspaceController(source: source);
      await workspace.reopen(
        AgentTask.fromJson(data['task'] as Map<String, dynamic>),
        [],
        [],
      );
      expect(workspace.requestError, isNull);
      expect(posts, 0);
      expect(workspace.replies, hasLength(2));
      expect(workspace.replies.map((reply) => reply.messageIndex), [1, 3]);
      expect(
        workspace.replies.map(
          (reply) => reply.result.onlinePlaceSearch!.places.length,
        ),
        [15, 15],
      );
      expect(workspace.result!.onlinePlaceSearch!.origin, 'HISTORY');
      now = DateTime.parse(
        top['expiresAt'] as String,
      ).add(const Duration(days: 7));
      expect(workspace.replies.first.result.entities, hasLength(15));
      expect(workspace.replies.first.result.onlinePlaceSearch!.stale, true);
      final old = workspace.replies.first.result;
      workspace.showExternalPlaceOnMap(
        old,
        old.onlinePlaceSearch!.places.first,
      );
      expect(
        workspace.selectedEntityId,
        old.onlinePlaceSearch!.places.first.mapID,
      );
      expect(workspace.presentedResult!.projectionItems, isEmpty);
      workspace.dispose();
    },
  );

  test(
    'a fresh authorized GET replaces retired same-ID historical objects after city A-B-A reopen',
    () async {
      final fixture = _actualHistory();
      final data = fixture['data'] as Map<String, dynamic>;
      final top = data['onlinePlaceSearch'] as Map<String, dynamic>;
      final originalCity = top['cityId'] as String;
      var city = originalCity;
      final now = DateTime.parse(
        top['observedAt'] as String,
      ).add(const Duration(minutes: 5));
      var gets = 0;
      final source = RemoteAgentTaskSource(
        cityID: () => city,
        authorizationHeader: () => 'Bearer offline-owner',
        publicEvidenceOwnerID: () => _owner,
        publicEvidenceNow: () => now,
        apiBaseUrl: _api,
        client: MockClient((request) async {
          expect(request.method, 'GET');
          ++gets;
          return _reply(fixture);
        }),
      );
      final workspace = AgentWorkspaceController(source: source);
      await workspace.reopen(
        AgentTask.fromJson(data['task'] as Map<String, dynamic>),
        [],
        [],
      );
      final previous = workspace.task!;
      final old = workspace.replies.first.result;
      city = 'another-city';
      workspace.retirePendingQueryForViewChange();
      expect(old.onlinePlaceSearch!.current, false);
      city = originalCity;
      expect(old.onlinePlaceSearch!.current, false);
      await workspace.reopen(previous, [], []);
      expect(workspace.requestError, isNull);
      expect(gets, 2);
      final recovered = workspace.replies.first.result;
      expect(
        recovered.onlinePlaceSearch!.snapshotID,
        old.onlinePlaceSearch!.snapshotID,
      );
      expect(recovered, isNot(same(old)));
      expect(workspace.canUseExternalPlaces(recovered), true);
      expect(recovered.entities, hasLength(15));
      expect(workspace.result!.onlinePlaceSearch!.origin, 'HISTORY');
      workspace.showExternalPlaceOnMap(
        recovered,
        recovered.onlinePlaceSearch!.places.last,
      );
      expect(workspace.presentedResult, same(recovered));
      workspace.dispose();
    },
  );

  test(
    'actual GET allows freshness downgrade but rejects any fact/query/message binding change',
    () async {
      for (final changed in [
        'stale',
        'name',
        'request',
        'index',
        'query',
        'owner',
      ]) {
        final fixture = _actualHistory();
        final data = fixture['data'] as Map<String, dynamic>;
        final top = data['onlinePlaceSearch'] as Map<String, dynamic>;
        switch (changed) {
          case 'stale':
            top['freshness'] = 'STALE';
          case 'name':
            ((top['places'] as List).first as Map)['name'] = '另一家';
          case 'request':
            top['requestId'] = 'unrelated-request';
          case 'index':
            top['messageIndex'] = 1;
          case 'query':
            top['query'] = '另一轮餐厅';
          case 'owner':
            (data['task'] as Map)['principalId'] = 'another-owner';
        }
        final now = DateTime.parse(
          top['observedAt'] as String,
        ).add(const Duration(minutes: 5));
        final source = RemoteAgentTaskSource(
          cityID: () => top['cityId'] as String,
          authorizationHeader: () => 'Bearer offline-owner',
          publicEvidenceOwnerID: () => _owner,
          publicEvidenceNow: () => now,
          apiBaseUrl: _api,
          client: MockClient((_) async => _reply(fixture)),
        );
        final read = source.restore(
          const AgentTask(id: _taskID, query: _query, status: 'COMPLETED'),
          [],
          [],
        );
        if (changed == 'stale') {
          expect((await read).onlinePlaceSearch!.stale, true);
        } else {
          await expectLater(read, throwsFormatException, reason: changed);
        }
        source.dispose();
      }
    },
  );

  testWidgets(
    'conversation shows both original rounds with three equal cards each',
    (t) async {
      var turn = 0;
      final source = RemoteAgentTaskSource(
        cityID: () => 'alpha',
        authorizationHeader: () => null,
        publicEvidenceNow: () => base,
        apiBaseUrl: _api,
        externalPlaceHistoryStore: MemoryExternalPlaceHistoryStore(),
        client: MockClient(
          (request) async => _reply(
            _queryWire(
              base,
              request.headers['X-Request-ID']!,
              (jsonDecode(request.body) as Map)['query'] as String,
              turn: ++turn,
            ),
          ),
        ),
      );
      final workspace = AgentWorkspaceController(source: source);
      await workspace.submit(_query, [], [], cityID: 'alpha');
      await workspace.submit('附近餐厅', [], [], cityID: 'alpha');
      workspace.setSheetExtent(AgentSheetExtent.expanded);
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: AgentResultsSheet(
              workspace: workspace,
              embedded: true,
              onContentHeightChanged: (_) {},
              availableHeight: 650,
              onSuggestion: (_) {},
              onRetry: () {},
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.byType(ExternalPlaceCandidateCard), findsNWidgets(6));
      expect(find.text('本轮餐厅说明：有没有好的餐厅'), findsOneWidget);
      expect(find.text('本轮餐厅说明：附近餐厅'), findsOneWidget);
      expect(
        find.text('更新餐厅信息'),
        findsOneWidget,
      ); // only the current query refreshes
      expect(find.text('这条回答的结果已失效，请从最近对话重新打开。'), findsNothing);
      await t.pumpWidget(const SizedBox.shrink());
      workspace.dispose();
    },
  );
}
