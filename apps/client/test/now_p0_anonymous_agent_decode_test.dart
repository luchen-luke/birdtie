import 'dart:convert';
import 'dart:io';

import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

// Unmodified API34 anonymous POST response captured on 2026-10-09 17:39 UTC.
// This is an offline decoder regression fixture, not native UI evidence.
// Raw response/HTTP receipt: work/now-p0-repair-2026-10-09/anonymous-decoder-01.
const _capturedResponse =
    r'''{"data":{"commercialTrustVersion":"sponsored-opportunity-v1","sponsoredStatus":"available","sponsoredOpportunities":[],"cityId":"aberdeen-gb","query":"find badminton activities","mode":"rules","note":"这个时间段暂无符合条件的已发布活动。","message":"暂时没有找到符合条件的公开活动。","principalType":"PERSON","principalId":"","workspace":"PERSONAL","permissions":["city_context.read","relationship_context.read"],"activities":[],"people":[],"groups":[],"organizations":null,"places":[],"followUps":[],"requestId":"3894ea470327414fac9e1fafb094bfd1","resultSet":{"schema":"typed-agent-results-v1","id":"3894ea470327414fac9e1fafb094bfd1","query":"find badminton activities","cityId":"aberdeen-gb","entities":[],"items":[],"filters":{"category":"badminton","currentQuery":"find badminton activities","locationPreference":"city","targetIntent":"FIND_ACTIVITY","timePreference":"anytime"},"generatedAt":"2026-10-09T17:39:14.2937442Z","status":"empty","sources":[]},"actions":[],"mapEffects":{"camera":"preserve","pinEntityIds":[]},"messageResults":null}}''';
const _query = 'find badminton activities';
const _owner = '8da3554c-5ace-4136-b90a-b86be84ad797';
const _taskID = '84073842-fedf-4741-a263-863b4a4e0065';

Map<String, dynamic> _body() =>
    jsonDecode(_capturedResponse) as Map<String, dynamic>;

http.Response _reply(String body) => http.Response.bytes(
  utf8.encode(body),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

RemoteAgentTaskSource _source(
  http.Client client, {
  String? Function()? authorization,
}) => RemoteAgentTaskSource(
  cityID: () => 'aberdeen-gb',
  authorizationHeader: authorization ?? () => null,
  apiBaseUrl: 'https://captured-response.test',
  client: client,
);

Matcher get _invalidReply => throwsA(
  isA<AgentRequestFailure>().having(
    (error) => error.userMessage,
    'decode failure',
    '服务返回的数据暂时无法读取，请稍后重试。',
  ),
);

class _RecordingRealClient extends http.BaseClient {
  final http.Client _inner = http.Client();
  String? method, path, query, requestID;
  int? status;

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    method = request.method;
    path = request.url.path;
    expect(request.headers.containsKey('Authorization'), isFalse);
    expect(request, isA<http.Request>());
    query =
        (jsonDecode((request as http.Request).body) as Map)['query'] as String;
    final response = await _inner.send(request);
    status = response.statusCode;
    requestID = response.headers['x-request-id'];
    return response;
  }

  @override
  void close() => _inner.close();
}

void main() {
  test(
    'actual anonymous wire null history decodes as public empty result',
    () async {
      final source = _source(
        MockClient((request) async {
          expect(request.method, 'POST');
          expect(request.headers.containsKey('Authorization'), isFalse);
          expect(jsonDecode(request.body), {'query': _query});
          return _reply(_capturedResponse);
        }),
      );
      addTearDown(source.dispose);
      final result = await source.resolve(_query, [], []);
      expect(result.resultSet?.status, 'empty');
      expect(result.projectionItems, isEmpty);
      expect(result.task, isNull);
      expect(result.taskID, isNull);
      expect(result.conversationID, isNull);
      expect(result.messageResults, isNull);
      expect(result.responseMessage, '暂时没有找到符合条件的公开活动。');
      expect(result.resultSet?.publicQueryContext?.current, isTrue);
      expect(result.mapEffects?.camera, 'preserve');
      expect(result.mapEffects?.pinEntityIDs, isEmpty);
    },
  );

  test('captured public response finishes the actual text turn once', () async {
    final source = _source(
      MockClient((request) async => _reply(_capturedResponse)),
    );
    final workspace = AgentWorkspaceController(source: source);
    addTearDown(workspace.dispose);
    await workspace.submit(_query, [], [], cityID: 'aberdeen-gb');
    expect(workspace.requestError, isNull);
    expect(workspace.requestFailure, isNull);
    expect(workspace.queryState, AgentQueryState.empty);
    expect(workspace.conversation.map((message) => message.role), [
      'user',
      'assistant',
    ]);
    expect(workspace.conversation.first.text, _query);
    expect(workspace.conversation.last.text, '暂时没有找到符合条件的公开活动。');
    expect(workspace.areaOperation, isNull);
  });

  for (final field in ['principalId', 'taskId', 'conversationId']) {
    test('anonymous null history cannot assert an owned $field', () async {
      final body = _body();
      (body['data'] as Map<String, dynamic>)[field] = _owner;
      final source = _source(
        MockClient((request) async => _reply(jsonEncode(body))),
      );
      addTearDown(source.dispose);
      await expectLater(source.resolve(_query, [], []), _invalidReply);
    });
  }

  test(
    'anonymous non-null history retains the personal history guard',
    () async {
      final body = _body();
      (body['data'] as Map<String, dynamic>)['messageResults'] = <dynamic>[];
      final source = _source(
        MockClient((request) async => _reply(jsonEncode(body))),
      );
      addTearDown(source.dispose);
      await expectLater(source.resolve(_query, [], []), _invalidReply);
    },
  );

  test(
    'authenticated null history remains invalid for a persisted task',
    () async {
      final body = _body();
      (body['data'] as Map<String, dynamic>)
        ..['principalId'] = _owner
        ..['taskId'] = _taskID
        ..['conversationId'] = _taskID
        ..['task'] = {
          'id': _taskID,
          'query': _query,
          'status': 'COMPLETED',
          'cityContext': 'aberdeen-gb',
          'contextType': 'CITY',
          'principalType': 'person',
          'principalId': _owner,
          'actingUserId': _owner,
          'conversation': <dynamic>[],
        };
      final source = _source(
        MockClient((request) async => _reply(jsonEncode(body))),
        authorization: () => 'Bearer unit-owner',
      );
      addTearDown(source.dispose);
      await expectLater(source.resolve(_query, [], []), _invalidReply);
    },
  );

  test(
    'late anonymous null history is rejected after identity changes',
    () async {
      String? authorization;
      final source = _source(
        MockClient((request) async {
          authorization = 'Bearer changed-unit-identity';
          return _reply(_capturedResponse);
        }),
        authorization: () => authorization,
      );
      addTearDown(source.dispose);
      await expectLater(source.resolve(_query, [], []), _invalidReply);
    },
  );

  final realAPI = Platform.environment['BIRDTIE_NOW_P0_REAL_API'];
  test(
    'actual API34 anonymous POST decodes through RemoteAgentTaskSource',
    () async {
      expect(realAPI, 'http://127.0.0.1:18109');
      final client = _RecordingRealClient();
      final source = RemoteAgentTaskSource(
        cityID: () => 'aberdeen-gb',
        authorizationHeader: () => null,
        apiBaseUrl: realAPI,
        client: client,
      );
      addTearDown(() {
        source.dispose();
        client.close();
      });
      final result = await source.resolve(_query, [], []);
      expect(client.status, 200);
      expect(client.method, 'POST');
      expect(client.path, '/v1/cities/aberdeen-gb/agent/tasks');
      expect(client.query, _query);
      expect(result.requestID, client.requestID);
      expect(result.projectionItems, isEmpty);
      expect(result.resultSet?.status, 'empty');
      expect(result.messageResults, isNull);
      expect(result.responseMessage, '暂时没有找到符合条件的公开活动。');
      debugPrint(
        jsonEncode({
          'evidence': 'ACTUAL_API34_REMOTE_DECODER_NOT_NATIVE_UI',
          'method': client.method,
          'path': client.path,
          'status': client.status,
          'requestID': result.requestID,
          'resultStatus': result.resultSet?.status,
          'items': result.projectionItems?.length,
        }),
      );
    },
    skip: realAPI == null ? 'opt-in local API34 response verification' : false,
  );
}
