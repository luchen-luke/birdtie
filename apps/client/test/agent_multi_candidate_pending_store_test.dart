import 'dart:convert';
import 'package:birdtie_client/src/workspace/agent_multi_candidate_pending_store.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'agent_memory_candidate_api_test.dart';
import 'agent_multi_candidate_api_test.dart';

PendingMultiCandidateOperation pendingMulti({
  String phase = 'stage',
  String? preview,
}) => PendingMultiCandidateOperation({
  'environment': candidateRecoveryFingerprint('http://fixture'),
  'ownerId': candidateOwner,
  'agentId': candidateAgent,
  'sessionFingerprint': candidateRecoveryFingerprint('Bearer synthetic'),
  'phase': phase,
  'multi': true,
  'previewId': preview ?? multiPreviewID,
  'grantId': phase == 'approval' ? null : multiGrantID,
  'grantRevision': phase == 'approval' ? null : 1,
  'selectionDigest': 'a' * 64,
  'reviewDigest': 'b' * 64,
  'taskId': multiTask,
  'anchorEventId': multiEvent,
  'logicalOperationId': multiLogical,
  'expiresAt': DateTime.now()
      .toUtc()
      .add(const Duration(minutes: 1))
      .toIso8601String(),
  'sourceVersions': [
    {'momentId': candidateMoment, 'revision': 1},
    {'momentId': candidateSaved, 'revision': 2},
  ],
});
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  for (final phase in ['approval', 'revocation']) {
    for (final count in [0, 1, 6]) {
      test('multi $phase rejects source version count $count', () {
        final p = pendingMulti(phase: phase);
        final sources = [
          for (var i = 0; i < count; i++)
            {
              'momentId':
                  '00000000-0000-4000-8000-${i.toString().padLeft(12, '0')}',
              'revision': 1,
            },
        ];
        expect(
          () => PendingMultiCandidateOperation({
            ...p.data,
            'sourceVersions': sources,
          }),
          throwsFormatException,
        );
      });
    }
  }
  for (final count in [0, 2]) {
    test('single journal requires exactly one current source not $count', () {
      final p = pendingMulti(phase: 'approval');
      expect(
        () => PendingMultiCandidateOperation({
          ...p.data,
          'multi': false,
          'sourceVersions': (p.data['sourceVersions'] as List)
              .take(count)
              .toList(),
        }),
        throwsFormatException,
      );
    });
  }
  for (final phase in ['stage', 'revocation']) {
    test('$phase cannot omit original grant revision', () {
      expect(
        () => PendingMultiCandidateOperation({
          ...pendingMulti(phase: phase).data,
          'grantRevision': null,
        }),
        throwsFormatException,
      );
      expect(
        () => PendingMultiCandidateOperation({
          ...pendingMulti(phase: phase).data,
          'grantRevision': double.nan,
        }),
        throwsFormatException,
      );
    });
  }
  for (final field in ['taskId', 'anchorEventId', 'logicalOperationId']) {
    test('stage requires exact original $field', () {
      expect(
        () => PendingMultiCandidateOperation({
          ...pendingMulti().data,
          field: null,
        }),
        throwsFormatException,
      );
    });
  }
  for (final time in [
    '0001-01-01T00:00:00+23:59',
    '9999-12-31T23:59:59-23:59',
  ]) {
    test(
      'offset must not move native recovery time beyond supported UTC years $time',
      () {
        expect(
          () => PendingMultiCandidateOperation({
            ...pendingMulti().data,
            'expiresAt': time,
          }),
          throwsFormatException,
        );
      },
    );
  }
  test(
    'ninth malformed namespace entry blocks reserve without a replacement or deletion',
    () async {
      final store = MemoryAgentMultiCandidatePendingStore(), p = pendingMulti();
      await store.write('http://fixture', candidateOwner, p);
      final key = store.values.keys.single;
      for (var i = 1; i <= 8; i++) {
        store.values['$key.$i'] = p.encoded;
      }
      final before = Map<String, String>.from(store.values);
      await expectLater(
        store.write('http://fixture', candidateOwner, p),
        throwsA(anything),
      );
      expect(store.values, before);
    },
  );
  test('valid RFC3339 offset accepted while impossible date rejected', () {
    final p = pendingMulti();
    expect(
      PendingMultiCandidateOperation({
        ...p.data,
        'expiresAt': '2026-10-05T21:00:00+08:00',
      }).expiresAt.hour,
      13,
    );
    for (final time in [
      '2026-02-30T12:00:00Z',
      '2026-10-05T12:00:00+24:00',
      '2026-10-05T25:00:00Z',
    ]) {
      expect(
        () => PendingMultiCandidateOperation({...p.data, 'expiresAt': time}),
        throwsFormatException,
      );
    }
  });
  for (final secure in [false, true]) {
    test(
      'atomic owner reservation immutable key and exact delete secure=$secure',
      () async {
        FlutterSecureStorage.setMockInitialValues({});
        final AgentMultiCandidatePendingStore store = secure
            ? const SecureAgentMultiCandidatePendingStore()
            : MemoryAgentMultiCandidatePendingStore();
        final p = pendingMulti();
        await store.write('http://fixture', candidateOwner, p);
        await store.write('http://fixture', candidateOwner, p);
        final other = pendingMulti(preview: sourcePreviewID(1));
        await expectLater(
          store.write('http://fixture', candidateOwner, other),
          throwsStateError,
        );
        expect(
          (await store.read('http://fixture', candidateOwner)).single.same(p),
          true,
        );
        await expectLater(
          store.delete('http://fixture', candidateOwner, other),
          completes,
        );
        expect((await store.read('http://fixture', candidateOwner)).length, 1);
        await store.delete('http://fixture', candidateOwner, p);
        await store.write('http://fixture', candidateOwner, other);
        await store.delete('http://fixture', candidateOwner, p);
        expect(
          (await store.read(
            'http://fixture',
            candidateOwner,
          )).single.same(other),
          true,
        );
      },
    );
  }
  test(
    'secure journal survives new store instance and contains references only',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      final p = pendingMulti();
      await const SecureAgentMultiCandidatePendingStore().write(
        'http://fixture',
        candidateOwner,
        p,
      );
      final read = await const SecureAgentMultiCandidatePendingStore().read(
        'http://fixture',
        candidateOwner,
      );
      expect(read.single.same(p), true);
      final raw = await const FlutterSecureStorage().readAll();
      expect(raw.length, 1);
      for (final forbidden in [
        'http://fixture',
        'Bearer synthetic',
        'sourceSelections',
        'query',
        'review',
        'selection',
        'content',
        '羽毛球',
      ]) {
        // Digest keys are association metadata, not the corresponding body.
        expect(raw.values.single.contains('"$forbidden"'), false);
      }
    },
  );
  for (final field in [
    'query',
    'content',
    'selection',
    'review',
    'token',
    'permission',
  ]) {
    test('closed journal rejects extra $field', () {
      expect(
        () => PendingMultiCandidateOperation({
          ...pendingMulti().data,
          field: 'private-canary',
        }),
        throwsFormatException,
      );
    });
  }
  for (final field in [
    'ownerId',
    'agentId',
    'previewId',
    'grantId',
    'taskId',
    'anchorEventId',
    'logicalOperationId',
    'expiresAt',
    'sourceVersions',
    'phase',
    'multi',
    'grantRevision',
  ]) {
    test('malformed reference $field rejected', () {
      expect(
        () => PendingMultiCandidateOperation({
          ...pendingMulti().data,
          field: 'malformed',
        }),
        throwsFormatException,
      );
    });
  }
  test(
    'corrupt stored prefix blocks reads and atomic writes without deletion',
    () async {
      final store = MemoryAgentMultiCandidatePendingStore(), p = pendingMulti();
      await store.write('http://fixture', candidateOwner, p);
      store.values[store.values.keys.single] = '{"query":"private-canary"}';
      await expectLater(
        store.read('http://fixture', candidateOwner),
        throwsFormatException,
      );
      await expectLater(
        store.write('http://fixture', candidateOwner, p),
        throwsFormatException,
      );
      expect(
        store.values.values.single,
        jsonEncode({'query': 'private-canary'}),
      );
    },
  );
  test(
    'different environment and owner cannot access or replace pending',
    () async {
      final store = MemoryAgentMultiCandidatePendingStore(), p = pendingMulti();
      await store.write('http://fixture', candidateOwner, p);
      expect(await store.read('http://other', candidateOwner), isEmpty);
      expect(await store.read('http://fixture', candidateAgent), isEmpty);
      await expectLater(
        store.write('http://other', candidateOwner, p),
        throwsFormatException,
      );
      expect((await store.read('http://fixture', candidateOwner)).length, 1);
    },
  );
}
