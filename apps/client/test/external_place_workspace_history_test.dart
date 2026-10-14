import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/external_place_candidates.dart';
import 'package:birdtie_client/src/workspace/external_place_history_store.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'external_place_candidates_test.dart' show externalQueryWire;
import 'now_scope_recovery_test.dart' show NowFixture, nowSend, nowTap;

const _query = '有没有好的餐厅';
const _api = 'https://workspace-history.test';

class _TrackedHistoryStore extends MemoryExternalPlaceHistoryStore {
  int reads = 0;
  Completer<void>? delayNextRead;
  @override
  Future<List<Map<String, dynamic>>> read(String environment) async {
    ++reads;
    final delay = delayNextRead;
    delayNextRead = null;
    final rows = await super.read(environment);
    if (delay != null) await delay.future;
    return rows;
  }
}

class _WorkspaceHistoryFixture {
  final now = NowFixture();
  final store = _TrackedHistoryStore();
  final agentRequests = <http.Request>[];
  int sourceEpoch = 0;
  late RemoteAgentTaskSource source;

  Map<String, dynamic> _guestSearch() =>
      (jsonDecode(
                File(
                  '../../docs/testing/evidence/restaurant-history-cache-2026-10-10/api-01/guest-query.json',
                ).readAsStringSync(),
              )
              as Map<String, dynamic>)['onlinePlaceSearch']
          as Map<String, dynamic>;
  DateTime get clock => DateTime.parse(
    _guestSearch()['observedAt'] as String,
  ).add(const Duration(minutes: 5));

  Future<void> mount(WidgetTester tester) async {
    source = RemoteAgentTaskSource(
      cityID: () => now.city.selectedCity?.id,
      authorizationHeader: () => now.auth.authorizationHeader,
      publicEvidenceOwnerID: () => now.auth.accountID,
      publicEvidenceEpoch: () => sourceEpoch,
      publicEvidenceNow: () => clock,
      externalPlaceHistoryStore: store,
      apiBaseUrl: _api,
      client: MockClient((request) async {
        agentRequests.add(request);
        if (request.method == 'GET' &&
            request.url.path == '/v1/me/agent-tasks') {
          return http.Response(jsonEncode({'data': []}), 200);
        }
        if (request.method != 'POST') {
          throw StateError('No HTTP guest restoration');
        }
        final snapshot = _guestSearch();
        snapshot['requestId'] = request.headers['X-Request-ID'];
        snapshot['cityId'] = 'alpha';
        for (final item in snapshot['places'] as List) {
          (item as Map<String, dynamic>)['cityId'] = 'alpha';
        }
        final wire = externalQueryWire(clock, request.headers['X-Request-ID']!);
        wire['onlinePlaceSearch'] = snapshot;
        wire['message'] = '已保存本轮 15 家餐厅资料。';
        return http.Response.bytes(
          utf8.encode(jsonEncode({'data': wire})),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }),
    );
    await tester.pumpWidget(
      MaterialApp(
        home: MapWorkspace(
          city: now.city,
          auth: now.auth,
          moments: now.moments,
          agentTaskSource: source,
          seedClient: now.client,
          seedApiBaseUrl: now.base,
        ),
      ),
    );
    await tester.pumpAndSettle();
  }

  AgentWorkspaceController workspace(WidgetTester tester) =>
      tester.widget<MapCanvas>(find.byType(MapCanvas)).workspace;

  Future<void> unmount(WidgetTester tester) async {
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pumpAndSettle();
  }
}

void main() {
  testWidgets(
    'guest startup city initialization during the store await keeps the sidebar list and restores 15 cards',
    (tester) async {
      final fixture = _WorkspaceHistoryFixture();
      addTearDown(fixture.now.dispose);
      await fixture.mount(tester);
      await nowSend(tester, _query);
      final taskID = fixture.workspace(tester).task!.id;
      await fixture.unmount(tester);

      fixture.now.city.selection = null;
      final storedRead = Completer<void>();
      fixture.store.delayNextRead = storedRead;
      await fixture.mount(tester);
      final restarted = fixture.workspace(tester);
      expect(restarted.recentLoading, true);
      // This notifies the real MapWorkspace _onCityChange while loadRecent has
      // already captured its source identity and is awaiting device storage.
      fixture.now.city.selectCity('alpha');
      storedRead.complete();
      await tester.pumpAndSettle();
      expect(restarted.recentLoading, false);
      expect(restarted.recentError, isNull);
      expect(restarted.recent.single.id, taskID);
      await nowTap(tester, find.byTooltip('打开侧边栏'));
      await nowTap(tester, find.text(_query));
      expect(restarted.requestError, isNull);
      expect(restarted.task!.cityID, 'alpha');
      expect(restarted.result!.entities, hasLength(15));
      expect(find.byType(ExternalPlaceCandidateCard), findsNWidgets(15));
      expect(fixture.agentRequests, hasLength(1));
      await fixture.unmount(tester);
    },
  );

  testWidgets(
    'actual guest MapWorkspace boot loads device history and sidebar restores 15 cards without HTTP',
    (tester) async {
      final fixture = _WorkspaceHistoryFixture();
      addTearDown(fixture.now.dispose);
      await fixture.mount(tester);
      expect(
        fixture.store.reads,
        1,
      ); // guest initialization now reads local history
      await nowSend(tester, _query);
      expect(fixture.workspace(tester).publicHistoryNotice, isNull);
      expect(
        fixture.workspace(tester).result!.onlinePlaceSearch!.places,
        hasLength(15),
      );
      final originalTask = fixture.workspace(tester).task!.id;
      await fixture.unmount(tester);

      await fixture.mount(
        tester,
      ); // new actual MapWorkspace and new remote source
      final recovered = fixture.workspace(tester);
      expect(recovered.recentError, isNull);
      expect(recovered.recent.single.id, originalTask);
      expect(
        fixture.agentRequests.where((request) => request.method == 'POST'),
        hasLength(1),
      );
      expect(
        fixture.agentRequests.where((request) => request.method == 'GET'),
        isEmpty,
      );
      await nowTap(tester, find.byTooltip('打开侧边栏'));
      expect(find.text('最近对话'), findsOneWidget);
      expect(find.text('你开始的对话会显示在这里。'), findsNothing);
      await nowTap(tester, find.text(_query));
      expect(recovered.task!.id, originalTask);
      expect(recovered.requestError, isNull);
      expect(find.byType(ExternalPlaceCandidateCard), findsNWidgets(15));
      expect(find.text("Frankie & Benny's"), findsOneWidget);
      expect(find.text('已保存本轮 15 家餐厅资料。'), findsOneWidget);
      expect(recovered.result!.entities, hasLength(15));
      expect(
        fixture.agentRequests,
        hasLength(1),
      ); // the initial deliberate query only
      await fixture.unmount(tester);
    },
  );

  testWidgets(
    'actual sign-out MapWorkspace reloads guest sidebar after later epoch listeners finish',
    (tester) async {
      final fixture = _WorkspaceHistoryFixture();
      addTearDown(fixture.now.dispose);
      await fixture.mount(tester);
      await nowSend(tester, _query);
      final guestTask = fixture.workspace(tester).task!.id;
      final guestReads = fixture.store.reads;
      // Model the real later _onSeedIdentityChange listener: the source epoch
      // changes after _onAuthChange, within the same synchronous notification.
      void retireLaterEpoch() => ++fixture.sourceEpoch;
      fixture.now.auth.addListener(retireLaterEpoch);
      fixture.now.auth.changeIdentity('Bearer signed-in');
      await tester.pumpAndSettle();
      expect(fixture.workspace(tester).recent, isEmpty);
      expect(fixture.workspace(tester).recentError, isNull);
      expect(
        fixture.store.reads,
        guestReads,
      ); // owned history does not read guest storage

      fixture.now.auth.changeIdentity(null);
      await tester.pumpAndSettle();
      final recovered = fixture.workspace(tester);
      expect(recovered.recentError, isNull);
      expect(recovered.recent.single.id, guestTask);
      expect(fixture.store.reads, guestReads + 1);
      await nowTap(tester, find.byTooltip('打开侧边栏'));
      expect(find.text(_query), findsOneWidget);
      expect(find.text('最近对话读取失败'), findsNothing);
      expect(
        fixture.agentRequests.where((request) => request.method == 'POST'),
        hasLength(1),
      );
      expect(
        fixture.agentRequests.where((request) => request.method == 'GET'),
        hasLength(1),
      );
      fixture.now.auth.removeListener(retireLaterEpoch);
      await fixture.unmount(tester);
    },
  );
}
