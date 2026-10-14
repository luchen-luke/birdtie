import 'dart:convert';

import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  test('地点事实不足显示未回答，保留原503和业务码且不自动重发', () async {
    final requests = <http.Request>[];
    final source = RemoteAgentTaskSource(
      cityID: () => 'aberdeen-gb',
      authorizationHeader: () => 'Bearer TEST_ONLY',
      apiBaseUrl: 'https://test-only.invalid',
      client: MockClient((r) async {
        requests.add(r);
        return http.Response(
          jsonEncode({
            'error': {'code': 'place_answer_unavailable'},
          }),
          503,
        );
      }),
    );
    addTearDown(source.dispose);
    await expectLater(
      source.resolve('推荐晚餐地点', [], []),
      throwsA(
        isA<AgentRequestFailure>()
            .having((e) => e.statusCode, '原HTTP状态', 503)
            .having((e) => e.code, '原业务码', 'place_answer_unavailable')
            .having(
              (e) => e.userMessage,
              '缺失事实',
              '当前地点资料尚不足以回答这个问题。请明确地点名称，或调整条件后再试。',
            ),
      ),
    );
    expect(requests, hasLength(1));
    expect(jsonDecode(requests.single.body), {'query': '推荐晚餐地点'});
  });
}
