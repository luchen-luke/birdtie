import 'dart:convert';

import 'package:birdtie_client/src/workspace/agent_debug_diagnostics.dart';
import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _pendingCode = 'search_retention_permission_pending';
const _pendingMessage = '联网搜索的结果保存许可尚未确认，本次未发出联网或模型请求。';
const _query = 'find place maritime museum';

void main() {
  test('服务器明确503许可门禁显示真实原因并保留诊断与请求，不自动重试', () async {
    final requests = <http.Request>[];
    final diagnostics = AgentDebugDiagnostics();
    final source = RemoteAgentTaskSource(
      cityID: () => 'aberdeen-gb',
      authorizationHeader: () => 'Bearer synthetic-owner',
      apiBaseUrl: 'https://retention-gate-fixture.test',
      diagnostics: diagnostics,
      client: MockClient((request) async {
        requests.add(request);
        return http.Response(
          jsonEncode({
            'error': {'code': _pendingCode, 'message': 'not displayed'},
          }),
          503,
          headers: {'X-Request-ID': request.headers['X-Request-ID']!},
        );
      }),
    );
    addTearDown(source.dispose);
    addTearDown(diagnostics.dispose);
    await expectLater(
      source.resolve(_query, [], []),
      throwsA(
        isA<AgentRequestFailure>()
            .having((e) => e.userMessage, '中文真实门禁原因', _pendingMessage)
            .having((e) => e.statusCode, '原HTTP状态', 503)
            .having((e) => e.code, '原业务码', _pendingCode),
      ),
    );
    await Future<void>.delayed(Duration.zero);
    expect(requests, hasLength(1));
    expect(requests.single.method, 'POST');
    expect(requests.single.url.path, '/v1/cities/aberdeen-gb/agent/tasks');
    expect(jsonDecode(requests.single.body), {'query': _query});
    expect(diagnostics.requestId, requests.single.headers['X-Request-ID']);
    expect(diagnostics.requestId, matches(RegExp(r'^[0-9a-f]{32}$')));
    expect(diagnostics.status, 'HTTP 503');
    expect(diagnostics.error, _pendingCode);
    expect(diagnostics.resultCount, isNull);
    expect(diagnostics.authenticated, isTrue);
  });

  final unrelatedResponses = <String, String>{
    '另一业务码': jsonEncode({
      'error': {'code': 'service_unavailable'},
    }),
    '大小写不同': jsonEncode({
      'error': {'code': _pendingCode.toUpperCase()},
    }),
    '填充空格': jsonEncode({
      'error': {'code': ' $_pendingCode '},
    }),
    '只在message出现': jsonEncode({
      'error': {'message': _pendingCode},
    }),
    '顶层伪同名字段': jsonEncode({'code': _pendingCode}),
    '非字符串code': jsonEncode({
      'error': {
        'code': [_pendingCode],
      },
    }),
    '无error结构': '{}',
    '无效JSON': 'not-json $_pendingCode',
  };
  for (final entry in unrelatedResponses.entries) {
    test('其他503不推断联网与模型未请求：${entry.key}', () async {
      var calls = 0;
      final source = RemoteAgentTaskSource(
        cityID: () => 'aberdeen-gb',
        authorizationHeader: () => null,
        apiBaseUrl: 'https://retention-gate-fixture.test',
        client: MockClient((_) async {
          calls++;
          return http.Response(entry.value, 503);
        }),
      );
      addTearDown(source.dispose);
      await expectLater(
        source.resolve(_query, [], []),
        throwsA(
          isA<AgentRequestFailure>()
              .having((e) => e.statusCode, '原HTTP状态', 503)
              .having(
                (e) => e.userMessage,
                '保留原一般服务提示',
                'Birdtie 服务暂时不可用，请稍后重试。',
              ),
        ),
      );
      expect(calls, 1);
    });
  }

  for (final status in [401, 403, 500]) {
    test('同名业务码不覆盖HTTP$status的原错误语义', () async {
      final source = RemoteAgentTaskSource(
        cityID: () => 'aberdeen-gb',
        authorizationHeader: () => 'Bearer synthetic-owner',
        apiBaseUrl: 'https://retention-gate-fixture.test',
        client: MockClient(
          (_) async => http.Response(
            jsonEncode({
              'error': {'code': _pendingCode},
            }),
            status,
          ),
        ),
      );
      addTearDown(source.dispose);
      await expectLater(
        source.resolve(_query, [], []),
        throwsA(
          isA<AgentRequestFailure>()
              .having((e) => e.statusCode, '原HTTP状态', status)
              .having((e) => e.code, '原业务码', _pendingCode)
              .having(
                (e) => e.userMessage,
                '不推断供应商未调用',
                isNot(_pendingMessage),
              ),
        ),
      );
    });
  }

  test('controller遇到真实门禁保留原结果、选择、Task及本次请求文本', () async {
    final requests = <http.Request>[];
    final source = RemoteAgentTaskSource(
      cityID: () => 'aberdeen-gb',
      authorizationHeader: () => 'Bearer synthetic-owner',
      apiBaseUrl: 'https://retention-gate-fixture.test',
      client: MockClient((request) async {
        requests.add(request);
        if (request.method == 'GET') {
          return http.Response('{"data":[]}', 200);
        }
        return http.Response(
          jsonEncode({
            'error': {'code': _pendingCode},
          }),
          503,
          headers: {'X-Request-ID': request.headers['X-Request-ID']!},
        );
      }),
    );
    final controller = AgentWorkspaceController(source: source);
    addTearDown(controller.dispose);
    addTearDown(source.dispose);
    const previous = AgentResult(
      entities: [],
      activities: [],
      places: [],
      note: '保留的原结果',
    );
    const originalTask = AgentTask(
      id: '013796c7-4af0-4bfc-90db-c26fd1383a59',
      query: '原查询',
      status: 'COMPLETED',
      cityID: 'aberdeen-gb',
    );
    controller.task = originalTask;
    controller.result = previous;
    controller.selectedEntityId = 'place:synthetic-original';
    controller.conversation.add(
      const AgentMessage(role: 'assistant', text: '原回答'),
    );
    await controller.submit(_query, [], [], cityID: 'aberdeen-gb');
    expect(controller.requestError, _pendingMessage);
    expect(controller.requestFailure?.statusCode, 503);
    expect(controller.requestFailure?.code, _pendingCode);
    expect(controller.result, same(previous));
    expect(controller.task, same(originalTask));
    expect(controller.selectedEntityId, 'place:synthetic-original');
    expect(controller.conversation.map((m) => m.text), ['原回答', _query]);
    expect(requests.where((r) => r.method == 'POST'), hasLength(1));
    final body = jsonDecode(requests.first.body) as Map<String, dynamic>;
    expect(body, {'query': _query, 'taskId': originalTask.id});
    expect(requests.where((r) => r.method == 'GET'), hasLength(1));
    expect(requests.last.url.path, '/v1/me/agent-tasks');
    expect(controller.state, AgentViewState.results);
  });
}
