import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:birdtie_client/src/workspace/agent_answer_sources.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'reply_history_restore_test.dart' as history;

// Synthetic protocol fixtures, not real provider or place evidence.
Map<String, dynamic> _message() {
  final message =
      jsonDecode(
            jsonEncode(
              (history.historyWire(
                    DateTime.utc(2026, 10, 9),
                  )['task']['conversation']
                  as List)[1],
            ),
          )
          as Map<String, dynamic>;
  message['text'] = '许可公共地点的回答 [source-1]。';
  message['sourceEvidenceDigest'] = 'a' * 64;
  message['sources'] = [
    {
      'id': 'source-1',
      'title': 'Synthetic source, NOT REAL',
      'url': 'https://example.org/place',
    },
  ];
  message.remove('sourceRunId');
  message['licensedPlaceModelEvidence'] = {
    'schema': 'birdtie.licensed-place-model-reply.v1',
    'operationId': '11000000-0000-4000-8000-000000000001',
    'previewId': '11000000-0000-4000-8000-000000000002',
    'bindingId': '11000000-0000-4000-8000-000000000003',
    'providerRequestId': 'chatcmpl-synthetic-not-real',
    'provider': 'tencent_tokenhub',
    'model': 'deepseek-v4-pro-0813',
    'requestDigest': 'b' * 64,
    'payloadDigest': 'c' * 64,
    'currentQueryDigest': 'd' * 64,
    'textDigest': sha256
        .convert(
          utf8.encode('birdtie.licensed-model-text.v1\u0000${message['text']}'),
        )
        .toString(),
    'sourceEvidenceDigest': 'a' * 64,
    'facts': [
      {
        'evidenceId': '11000000-0000-4000-8000-000000000004',
        'placeId': history.placeA,
        'permissionId': '11000000-0000-4000-8000-000000000005',
        'field': 'name',
        'version': 1,
      },
    ],
    'validUntil': '2026-10-09T01:00:00+08:00',
    'searchCalls': 0,
    'modelCalls': 1,
  };
  return message;
}

void main() {
  test(
    'stored licensed model proof is not source-reading or dispatch authority',
    () {
      final refs = AgentMessage.fromJson(_message()).sourceReferences!;
      expect(refs.current, isFalse);
      expect(refs.localReplyID, isNull);
      expect(refs.licensedModelOperationID, isNotNull);
      expect(
        refs.licensedModelObservedValidUntil,
        DateTime.utc(2026, 10, 8, 17),
      );
      expect(refs.runID, '11000000-0000-4000-8000-000000000003');
    },
  );
  test('same round current read enables links and retires permanently', () {
    var current = true;
    final refs = AgentMessageSources.read(
      _message(),
      expectedTaskID: history.taskID,
      expectedMessageIndex: 1,
      current: () => true,
      localRoundCurrent: () => current,
      localRoundRemaining: () => const Duration(seconds: 8),
    )!;
    expect(refs.current, isTrue);
    expect(refs.remaining, const Duration(seconds: 8));
    current = false;
    expect(refs.current, isFalse);
    current = true;
    expect(refs.current, isFalse);
  });
  final invalid = <String, Object?>{
    'schema': 'birdtie.local-place-reply.v1',
    'provider': 'other',
    'model': 'hy3',
    'modelCalls': 0,
    'searchCalls': 1,
    'operationId': '00000000-0000-0000-0000-000000000000',
    'bindingId': 'forged',
    'providerRequestId': 'bad\nrequest',
    'requestDigest': '0' * 64,
    'sourceEvidenceDigest': 'b' * 64,
    'textDigest': 'f' * 64,
    'validUntil': '2026-10-32T01:00:00Z',
    'facts': [],
    'extra': true,
  };
  for (final entry in invalid.entries) {
    test('rejects malformed licensed model ${entry.key}', () {
      final m = _message();
      m['licensedPlaceModelEvidence'][entry.key] = entry.value;
      expect(() => AgentMessage.fromJson(m), throwsFormatException);
    });
  }
  test('rejects WSA or local proof attached to standalone model reply', () {
    final m = _message();
    m['sourceRunId'] = 'fake-run';
    expect(() => AgentMessage.fromJson(m), throwsFormatException);
    m.remove('sourceRunId');
    m['localReplyEvidence'] = {};
    expect(() => AgentMessage.fromJson(m), throwsFormatException);
  });
  test('requires assistant place membership in the same Task', () {
    final m = _message();
    m.remove('resultMembership');
    expect(() => AgentMessage.fromJson(m), throwsFormatException);
    expect(
      () => AgentMessage.fromJson(
        _message(),
        expectedTaskID: 'other',
        expectedMessageIndex: 1,
      ),
      throwsFormatException,
    );
  });
}
