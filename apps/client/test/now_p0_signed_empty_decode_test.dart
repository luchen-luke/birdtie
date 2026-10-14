import 'dart:convert';
import 'dart:io';

import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  test(
    'actual signed empty envelope retains owned history and one text turn',
    () async {
      // Unmodified real API34 body from one signed rules-only POST. This replay
      // verifies the decoder, not native UI; the clock is pinned to that capture.
      final bytes = await File(
        'test/fixtures/now_p0_signed_empty_response.json',
      ).readAsBytes();
      final data = (jsonDecode(utf8.decode(bytes)) as Map)['data'] as Map;
      final owner = data['principalId'] as String;
      final source = RemoteAgentTaskSource(
        cityID: () => 'aberdeen-gb',
        authorizationHeader: () => 'Bearer decoder-fixture-no-token',
        publicEvidenceOwnerID: () => owner,
        publicEvidenceEpoch: () => 1,
        publicEvidenceNow: () =>
            DateTime.parse((data['resultSet'] as Map)['generatedAt'] as String),
        apiBaseUrl: 'https://captured-response.test',
        client: MockClient((request) async {
          expect(request.method, 'POST');
          expect(jsonDecode(request.body), {
            'query': 'find badminton activities',
          });
          return http.Response.bytes(
            bytes,
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        }),
      );
      final workspace = AgentWorkspaceController(source: source);
      addTearDown(workspace.dispose);
      addTearDown(source.dispose);
      await workspace.submit(
        'find badminton activities',
        [],
        [],
        cityID: 'aberdeen-gb',
      );

      expect(workspace.requestError, isNull);
      expect(workspace.requestFailure, isNull);
      expect(workspace.queryState, AgentQueryState.empty);
      expect(workspace.task!.id, data['taskId']);
      expect(workspace.task!.principalID, owner);
      expect(workspace.task!.actingUserID, owner);
      expect(workspace.conversation.map((message) => message.role), [
        'user',
        'assistant',
      ]);
      expect(workspace.conversation.last.text, '当前没有符合条件的可见活动。');
      expect(workspace.result!.messageResults!.keys, [1]);
      expect(workspace.result!.messageResults![1]!.projectionItems, isEmpty);
      expect(workspace.result!.replyProjectionCurrent, isTrue);
      expect(workspace.areaOperation, isNull);
    },
  );
}
