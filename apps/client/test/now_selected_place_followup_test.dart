import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/agent_reply_membership.dart';
import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:birdtie_client/src/workspace/selected_place_followup.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'external_place_candidates_test.dart' show externalQueryWire;

const place = '33333333-3333-4333-8333-333333333333';
const taskID = '00000000-0000-4000-8000-000000000901';

Map<String, dynamic> selectedReply(http.Request request) {
  final query = (jsonDecode(request.body) as Map)['query'] as String;
  const answer = 'UNIT 原生地点已作为第2天的待确认草稿，尚未保存到计划。';
  final wire = externalQueryWire(
    DateTime.now().toUtc(),
    request.headers['X-Request-ID']!,
    signed: true,
    query: query,
  );
  wire.remove('onlinePlaceSearch');
  wire['message'] = answer;
  final task = wire['task'] as Map<String, dynamic>;
  final messages = task['conversation'] as List;
  (messages.last as Map)['text'] = answer;
  final digest = agentReplyTurnDigest([
    (role: 'user', text: query),
    (role: 'assistant', text: answer),
  ]);
  final resultSetID = 'reply:$taskID:${digest.substring(0, 24)}';
  (messages.last as Map)['resultMembership']['turnDigest'] = digest;
  (messages.last as Map)['resultMembership']['resultSetId'] = resultSetID;
  (wire['resultSet'] as Map)['id'] = resultSetID;
  return wire;
}

void main() {
  test(
    'map host compiles and selector contains only canonical native identity',
    () {
      expect(MapWorkspace, isNotNull);
      var current = true;
      final selection = SelectedPlaceFollowup(
        placeID: place,
        cityID: 'alpha',
        taskID: null,
        current: () => current,
      );
      expect(selection.current, isTrue);
      expect(selection.selector, {'placeId': place});
      current = false;
      expect(selection.current, isFalse);
      expect(
        SelectedPlaceFollowup(
          placeID: 'osm:node:1',
          cityID: 'alpha',
          taskID: null,
          current: () => true,
        ).current,
        isFalse,
      );
      for (final q in [
        '地址',
        '它周末几点开门？',
        '这家的营业时间',
        '把这个加入第二天',
        'add this place to day 2',
      ]) {
        expect(isSelectedPlaceFollowupQuery(q), isTrue, reason: q);
      }
      for (final q in ['加入明天', 'Gallery的地址', '帮我预订它', '把这个加入第二天并发消息']) {
        expect(isSelectedPlaceFollowupQuery(q), isFalse, reason: q);
      }
    },
  );

  test(
    'controller and actual transport send native selector for a new and existing Task',
    () async {
      final requests = <http.Request>[];
      final source = RemoteAgentTaskSource(
        cityID: () => 'alpha',
        authorizationHeader: () => 'Bearer UNIT',
        apiBaseUrl: 'https://selected-followup.test',
        client: MockClient((r) async {
          requests.add(r);
          return http.Response.bytes(
            utf8.encode(jsonEncode({'data': selectedReply(r)})),
            200,
          );
        }),
      );
      final workspace = AgentWorkspaceController(source: source);
      for (final q in ['地址', '把这个加入第二天']) {
        final selected = SelectedPlaceFollowup(
          placeID: place,
          cityID: 'alpha',
          taskID: workspace.result == null ? null : workspace.task?.id,
          current: () => true,
        );
        await workspace.submit(
          q,
          [],
          [],
          cityID: 'alpha',
          selectedPlace: selected,
        );
        expect(workspace.requestError, isNull);
        expect(workspace.task?.id, taskID);
        expect(workspace.conversation.last.text, contains('第2天'));
      }
      final first = jsonDecode(requests.first.body) as Map,
          second = jsonDecode(requests.last.body) as Map;
      expect(first.keys.toSet(), {'query', 'selectedPlace'});
      expect(second.keys.toSet(), {'query', 'taskId', 'selectedPlace'});
      expect(second['taskId'], taskID);
      expect(first['selectedPlace'], {'placeId': place});
      expect(second['selectedPlace'], {'placeId': place});
      expect(
        requests.every((r) => r.url.path == '/v1/cities/alpha/agent/tasks'),
        isTrue,
      );
      workspace.dispose();
      source.dispose();
    },
  );

  for (final change in [
    'account',
    'city',
    'organization',
    'selection',
    'selection-return',
    'task',
  ]) {
    test(
      'late selected-place response cannot cross $change lifecycle',
      () async {
        var city = 'alpha', token = 'Bearer UNIT-A';
        String? organization;
        var selected = true;
        var selectionEpoch = 0;
        final capturedSelectionEpoch = selectionEpoch;
        final pending = Completer<void>(), started = Completer<void>();
        var posts = 0;
        final source = RemoteAgentTaskSource(
          cityID: () => city,
          authorizationHeader: () => token,
          organizationWorkspaceID: () => organization,
          apiBaseUrl: 'https://selected-followup.test',
          client: MockClient((r) async {
            if (r.method == 'GET') return http.Response('{"data":[]}', 200);
            posts++;
            started.complete();
            await pending.future;
            return http.Response.bytes(
              utf8.encode(jsonEncode({'data': selectedReply(r)})),
              200,
            );
          }),
        );
        final workspace = AgentWorkspaceController(source: source);
        final selection = SelectedPlaceFollowup(
          placeID: place,
          cityID: 'alpha',
          taskID: null,
          current: () => selected && selectionEpoch == capturedSelectionEpoch,
        );
        final sending = workspace.submit(
          '把这个加入第二天',
          [],
          [],
          cityID: 'alpha',
          selectedPlace: selection,
        );
        await started.future;
        switch (change) {
          case 'account':
            token = 'Bearer UNIT-B';
          case 'city':
            city = 'beta';
          case 'organization':
            organization = 'UNIT-ORG';
          case 'selection':
            selected = false;
          case 'selection-return':
            selected = false;
            selectionEpoch++;
            selected = true;
          case 'task':
            workspace.newTask();
        }
        pending.complete();
        await sending;
        expect(posts, 1);
        expect(workspace.result, isNull);
        expect(
          workspace.conversation.where((m) => m.role == 'assistant'),
          isEmpty,
        );
        if (change != 'task') expect(workspace.requestError, isNotNull);
        workspace.dispose();
        source.dispose();
      },
    );
  }

  test(
    'retired or foreign-task selector is rejected before transport',
    () async {
      var sends = 0;
      final source = RemoteAgentTaskSource(
        cityID: () => 'alpha',
        authorizationHeader: () => 'Bearer UNIT',
        apiBaseUrl: 'https://selected-followup.test',
        client: MockClient((r) async {
          sends++;
          throw StateError('unexpected transport');
        }),
      );
      for (final selected in [
        SelectedPlaceFollowup(
          placeID: place,
          cityID: 'alpha',
          taskID: null,
          current: () => false,
        ),
        SelectedPlaceFollowup(
          placeID: place,
          cityID: 'alpha',
          taskID: 'foreign-task',
          current: () => true,
        ),
      ]) {
        await expectLater(
          source.continueSelectedPlace(null, '地址', selected, [], []),
          throwsA(isA<AgentRequestFailure>()),
        );
      }
      expect(sends, 0);
      source.dispose();
    },
  );
}
