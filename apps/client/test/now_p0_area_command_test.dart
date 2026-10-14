import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/agent_conversation.dart';
import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'map_layers_api_test.dart' show mapTestView, mapTestItem;

const _a = MapBounds(west: -2.2, south: 57.1, east: -2, north: 57.3);
const _b = MapBounds(west: -2.3, south: 57.0, east: -2.1, north: 57.2);

TypedMapView _view({
  List<TypedMapItem> items = const [],
  bool truncated = false,
}) => TypedMapView(
  cityID: 'owned-city',
  scope: 'PUBLIC',
  observedAt: DateTime.utc(2026),
  validUntil: DateTime.utc(2026).add(const Duration(minutes: 2)),
  truncated: truncated,
  items: items,
);

class _AreaSource extends AgentTaskSource {
  final requests = <MapBounds, Completer<AgentAreaRead>>{};
  int areaCalls = 0, turns = 0;
  @override
  Future<AgentAreaRead> readMapArea(
    String cityID,
    MapBounds bounds,
    Set<String> kinds, {
    String? authorID,
  }) {
    areaCalls++;
    return (requests[bounds] = Completer<AgentAreaRead>()).future;
  }

  @override
  Future<AgentResult> resolve(String query, activities, places) async {
    turns++;
    return const AgentResult(
      entities: [],
      activities: [],
      places: [],
      note: '这个区域暂未找到符合条件的公开活动。',
    );
  }
}

void main() {
  test(
    'editing restores original mode only while task and manual choice are unchanged',
    () async {
      final workspace = AgentWorkspaceController(source: _AreaSource());
      addTearDown(workspace.dispose);
      await workspace.submit('原对话', [], []);
      workspace.setSheetExtent(AgentSheetExtent.peek);
      workspace.contentMode = AgentContentMode.results;
      workspace.selectEntity('place:keep');
      workspace.beginTyping();
      expect(workspace.sheetExtent, AgentSheetExtent.expanded);
      workspace.stopTyping();
      expect(workspace.sheetExtent, AgentSheetExtent.peek);
      expect(workspace.contentMode, AgentContentMode.results);
      expect(workspace.selectedEntityId, 'place:keep');
      workspace.beginTyping();
      workspace.setSheetExtent(AgentSheetExtent.medium);
      workspace.showContent(AgentContentMode.conversation);
      workspace.stopTyping();
      expect(workspace.sheetExtent, AgentSheetExtent.expanded);
      expect(workspace.contentMode, AgentContentMode.conversation);
      workspace.setSheetExtent(AgentSheetExtent.hidden);
      workspace.beginTyping();
      workspace.newTask();
      await workspace.submit('本人新问题', [], []);
      workspace.stopTyping();
      expect(
        workspace.sheetExtent,
        AgentSheetExtent.peek,
        reason:
            'A new short answer uses its own peek; it cannot restore the old hidden extent',
      );
    },
  );
  test(
    'author command requires exact author scope and never substitutes public city',
    () async {
      const authorID = '12345678-1234-1234-1234-123456789abc';
      final workspace = AgentWorkspaceController();
      addTearDown(workspace.dispose);
      await workspace.executeMapArea(
        cityID: 'owned-city',
        bounds: _a,
        authorID: authorID,
        kinds: {'MOMENT'},
        reader: () async => AgentAreaRead(publicView: _view()),
      );
      expect(workspace.areaOperation!.status, AgentAreaStatus.error);
      final view = TypedMapView(
        cityID: 'owned-city',
        scope: 'PUBLIC_AUTHOR',
        authorID: authorID,
        observedAt: DateTime.utc(2026),
        validUntil: DateTime.utc(2026).add(const Duration(minutes: 2)),
        truncated: false,
        items: const [],
      );
      await workspace.executeMapArea(
        cityID: 'owned-city',
        bounds: _a,
        authorID: authorID,
        kinds: {'MOMENT'},
        reader: () async => AgentAreaRead(publicView: view),
      );
      expect(workspace.areaOperation!.status, AgentAreaStatus.empty);
      expect(workspace.areaOperation!.authorID, authorID);
    },
  );
  test(
    'map command owns one operation while task/history/selection survive',
    () async {
      final source = _AreaSource();
      final workspace = AgentWorkspaceController(source: source);
      addTearDown(workspace.dispose);
      await workspace.submit('保留原查询', [], [], cityID: 'owned-city');
      workspace.selectEntity('place:original');
      final task = workspace.task,
          result = workspace.result,
          messages = List<AgentMessage>.of(workspace.conversation),
          recent = List<AgentTask>.of(workspace.recent);
      final pending = workspace.executeMapArea(
        cityID: 'owned-city',
        bounds: _a,
      );
      final repeated = workspace.executeMapArea(
        cityID: 'owned-city',
        bounds: _a,
      );
      expect(repeated, same(pending));
      expect(source.areaCalls, 1);
      expect(workspace.areaOperation!.status, AgentAreaStatus.loading);
      source.requests[_a]!.complete(AgentAreaRead(publicView: _view()));
      await pending;
      expect(workspace.areaOperation!.status, AgentAreaStatus.empty);
      expect(workspace.areaOperation!.origin, 'map_action');
      expect(workspace.task, same(task));
      expect(workspace.result, same(result));
      expect(workspace.conversation, messages);
      expect(workspace.recent, recent);
      expect(workspace.selectedEntityId, 'place:original');
      expect(source.turns, 1);
      final again = workspace.executeMapArea(cityID: 'owned-city', bounds: _a);
      source.requests[_a]!.complete(AgentAreaRead(publicView: _view()));
      await again;
      expect(source.areaCalls, 2, reason: 'Explicit later refresh is allowed');
    },
  );

  test(
    'slow A cannot overwrite B; retired scope cannot revive late data',
    () async {
      final source = _AreaSource();
      final workspace = AgentWorkspaceController(source: source);
      addTearDown(workspace.dispose);
      final a = workspace.executeMapArea(cityID: 'owned-city', bounds: _a);
      final b = workspace.executeMapArea(cityID: 'owned-city', bounds: _b);
      source.requests[_b]!.complete(AgentAreaRead(publicView: _view()));
      await b;
      final current = workspace.areaOperation;
      source.requests[_a]!.complete(AgentAreaRead(publicView: _view()));
      await a;
      expect(workspace.areaOperation, same(current));
      final late = workspace.executeMapArea(cityID: 'owned-city', bounds: _a);
      workspace.retirePendingQueryForViewChange();
      source.requests[_a]!.complete(AgentAreaRead(publicView: _view()));
      await late;
      expect(workspace.areaOperation, isNull);
    },
  );

  test(
    'failure, partial source coverage, truncation and expiry differ from empty',
    () async {
      final workspace = AgentWorkspaceController();
      addTearDown(workspace.dispose);
      await workspace.executeMapArea(
        cityID: 'owned-city',
        bounds: _a,
        reader: () => Future.error(StateError('private raw payload')),
      );
      expect(workspace.areaOperation!.status, AgentAreaStatus.error);
      expect(workspace.areaOperation!.summary, isNot(contains('private raw')));
      await workspace.executeMapArea(
        cityID: 'owned-city',
        bounds: _a,
        kinds: {'PLACE', 'OPPORTUNITY'},
        reader: () async => AgentAreaRead(
          publicView: _view(),
          unavailableKinds: {'OPPORTUNITY'},
        ),
      );
      expect(workspace.areaOperation!.status, AgentAreaStatus.partial);
      expect(workspace.areaOperation!.searchedKinds, {'PLACE'});
      expect(workspace.areaOperation!.summary, isNot(contains('未找到')));
      await workspace.executeMapArea(
        cityID: 'owned-city',
        bounds: _a,
        reader: () async => AgentAreaRead(publicView: _view(truncated: true)),
      );
      expect(workspace.areaOperation!.status, AgentAreaStatus.partial);
      var current = true;
      final item = TypedMapItem.decode(mapTestItem('PLACE'), 'PUBLIC', _a);
      await workspace.executeMapArea(
        cityID: 'owned-city',
        bounds: _a,
        reader: () async => AgentAreaRead(
          publicView: _view(items: [item]),
          current: () => current,
        ),
      );
      expect(workspace.retainsMapAreaItem(item), isTrue);
      current = false;
      expect(workspace.areaOperation!.items, isEmpty);
      expect(workspace.areaOperation!.summary, contains('已失效'));
      expect(workspace.retainsMapAreaItem(item), isFalse);
    },
  );

  test(
    'explicit retry reuses authorized reader and never creates a chat turn',
    () async {
      final workspace = AgentWorkspaceController();
      addTearDown(workspace.dispose);
      var calls = 0;
      await workspace.executeMapArea(
        cityID: 'owned-city',
        bounds: _a,
        reader: () async {
          if (++calls == 1) throw const AgentRequestFailure('暂时没能完成搜索，请重试。');
          return AgentAreaRead(publicView: _view());
        },
      );
      await workspace.retryMapArea();
      expect(calls, 2);
      expect(workspace.areaOperation!.status, AgentAreaStatus.empty);
      expect(workspace.conversation, isEmpty);
      expect(workspace.task, isNull);
    },
  );

  test(
    'Remote map command reads all existing public types without Agent POST',
    () async {
      final paths = <String>[];
      final source = RemoteAgentTaskSource(
        cityID: () => 'owned-city',
        authorizationHeader: () => null,
        apiBaseUrl: 'https://unit.test',
        client: MockClient((request) async {
          paths.add('${request.method} ${request.url.path}');
          return http.Response(
            jsonEncode({'data': mapTestView()}),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        }),
      );
      addTearDown(source.dispose);
      final read = await source.readMapArea('owned-city', _a, {
        'PLACE',
        'ACTIVITY',
        'MOMENT',
        'ORGANIZATION',
        'BUSINESS',
      });
      expect(paths, ['GET /v1/cities/owned-city/map-layers']);
      expect(read.items.map((i) => i.kind).toSet(), {
        'PLACE',
        'ACTIVITY',
        'MOMENT',
        'ORGANIZATION',
        'BUSINESS',
      });
      expect(read.current, isTrue);
    },
  );

  test(
    'Remote private source failure retains public items and explicit coverage',
    () async {
      var token = 'Bearer test';
      final source = RemoteAgentTaskSource(
        cityID: () => 'owned-city',
        authorizationHeader: () => token,
        apiBaseUrl: 'https://unit.test',
        client: MockClient(
          (request) async => request.url.path.contains('/me/')
              ? http.Response('{}', 503)
              : http.Response(
                  jsonEncode({'data': mapTestView()}),
                  200,
                  headers: {'content-type': 'application/json; charset=utf-8'},
                ),
        ),
      );
      addTearDown(source.dispose);
      final read = await source.readMapArea('owned-city', _a, {
        'PLACE',
        'ACTIVITY',
        'MOMENT',
        'ORGANIZATION',
        'BUSINESS',
        'OPPORTUNITY',
      });
      expect(read.publicView!.items, hasLength(5));
      expect(read.privateView, isNull);
      expect(read.unavailableKinds, {'OPPORTUNITY'});
      token = 'Bearer changed';
      expect(read.current, isFalse);
    },
  );

  testWidgets(
    'embedded area empty renders once with no pseudo-user or second surface',
    (tester) async {
      final workspace = AgentWorkspaceController();
      addTearDown(workspace.dispose);
      await workspace.executeMapArea(
        cityID: 'owned-city',
        bounds: _a,
        reader: () async => AgentAreaRead(publicView: _view()),
      );
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: SizedBox(
              height: 230,
              child: AgentResultsSheet(
                workspace: workspace,
                embedded: true,
                onSuggestion: (_) {},
                onRetry: () {},
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('本次未找到符合条件的结果。'), findsOneWidget);
      expect(find.text('搜索此区域'), findsNothing);
      expect(find.text('当前没有可展示的结果。'), findsNothing);
      expect(find.byKey(const Key('agent-sheet-handle')), findsOneWidget);
      final surface = tester.widget<AnimatedContainer>(
        find.byType(AnimatedContainer).first,
      );
      expect(surface.decoration, isNull);
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets(
    'typed same text remains two user turns; assistant is plain text',
    (tester) async {
      final source = _AreaSource();
      final workspace = AgentWorkspaceController(source: source);
      addTearDown(workspace.dispose);
      await workspace.submit('搜索此区域', [], []);
      await workspace.submit('搜索此区域', [], []);
      expect(
        workspace.conversation.where((m) => m.role == 'user'),
        hasLength(2),
      );
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: AgentResultsSheet(
              workspace: workspace,
              embedded: true,
              onSuggestion: (_) {},
              onRetry: () {},
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('当前没有可展示的结果。'), findsNothing);
      final reply = find.text('这个区域暂未找到符合条件的公开活动。').first;
      final alignment = tester.widget<Align>(
        find.ancestor(of: reply, matching: find.byType(Align)).first,
      );
      expect(alignment.child, isNot(isA<Container>()));
      expect(find.byType(AgentConversation), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
  );
}
