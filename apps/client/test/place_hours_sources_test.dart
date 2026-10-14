import 'dart:convert';
import 'package:birdtie_client/src/workspace/agent_answer_sources.dart';
import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  test('已核验营业时间仅扩已知字段，原source/task/expiry合同保留', () {
    final message = <String, dynamic>{
      'role': 'assistant',
      'sourceEvidenceDigest': 'a' * 64,
      'sources': [
        {
          'id': 'source-1',
          'title': '合成社区来源',
          'url': 'https://example.org/place',
        },
      ],
      'localReplyEvidence': {
        'schema': 'birdtie.local-place-reply.v1',
        'id': 'local-native:synthetic-task:1',
        'sourceEvidenceDigest': 'a' * 64,
        'validUntil': DateTime.now()
            .toUtc()
            .add(const Duration(minutes: 1))
            .toIso8601String(),
        'facts': [
          {
            'evidenceId': '11111111-1111-4111-8111-111111111111',
            'placeId': '22222222-2222-4222-8222-222222222222',
            'permissionId': '33333333-3333-4333-8333-333333333333',
            'field': 'opening_hours',
            'version': 1,
          },
        ],
        'memoryUses': [],
        'reason': 'LOCAL_FIELDS_SUFFICIENT',
        'searchCalls': 0,
        'modelCalls': 0,
      },
    };
    expect(AgentMessageSources.read(message), isNotNull);
    message['localReplyEvidence']['facts'][0]['field'] = 'unverified_hours';
    expect(() => AgentMessageSources.read(message), throwsFormatException);
  });
  test('营业时间缺资料422准确传达不确定，不变成联网或已完成', () async {
    final source = RemoteAgentTaskSource(
      authorizationHeader: () => 'Bearer synthetic',
      cityID: () => 'synthetic-city',
      apiBaseUrl: 'https://synthetic.example',
      client: MockClient(
        (r) async => http.Response(
          jsonEncode({
            'error': {'code': 'place_hours_unavailable'},
          }),
          422,
        ),
      ),
    );
    addTearDown(source.dispose);
    await expectLater(
      source.resolve('地点的营业时间', const [], const []),
      throwsA(
        isA<AgentRequestFailure>().having(
          (e) => e.userMessage,
          'message',
          contains('尚缺许可有效、未过期的营业时间资料'),
        ),
      ),
    );
  });
}
