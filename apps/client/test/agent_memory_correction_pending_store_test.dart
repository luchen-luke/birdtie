import 'package:birdtie_client/src/workspace/agent_memory_correction_pending_store.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'agent_memory_correction_api_test.dart';

PendingMemoryCorrection correctionPending({
  String phase = 'PREVIEW',
  String id = correctionID,
  String token = 'Bearer synthetic',
}) => PendingMemoryCorrection({
  'environment': completionFingerprint('http://local'),
  'ownerId': correctionOwner,
  'agentId': correctionAgent,
  'sessionFingerprint': completionFingerprint(token),
  'operationId': id,
  'targetId': correctionMemory,
  'expectedVersion': 1,
  'targetKind': 'MEMORY',
  'action': 'REJECT',
  'phase': phase,
  'planDigest': phase == 'CONFIRM' ? correctionPlan : null,
  'expiresAt': correctionAt
      .add(Duration(minutes: phase == 'CONFIRM' ? 5 : 1440))
      .toIso8601String(),
});
void main() {
  test('journal UTC归一后的年0期限拒绝', () {
    expect(
      () => PendingMemoryCorrection({
        ...correctionPending().data,
        'expiresAt': '0001-01-01T00:00:00+23:00',
      }),
      throwsFormatException,
    );
  });
  TestWidgetsFlutterBinding.ensureInitialized();
  for (final secure in [false, true]) {
    test(
      '${secure ? 'Secure mocked platform' : 'Memory'} 原引用原子reserve/readback/exact delete',
      () async {
        FlutterSecureStorage.setMockInitialValues({});
        final AgentMemoryCorrectionPendingStore s = secure
            ? const SecureAgentMemoryCorrectionPendingStore()
            : MemoryAgentMemoryCorrectionPendingStore();
        final p = correctionPending(), c = correctionPending(phase: 'CONFIRM');
        await s.write('http://local', correctionOwner, p);
        expect((await s.read('http://local', correctionOwner))!.same(p), true);
        await expectLater(
          s.write(
            'http://local',
            correctionOwner,
            correctionPending(id: correctionMemory),
          ),
          throwsStateError,
        );
        await s.write('http://local', correctionOwner, c);
        await expectLater(
          s.delete('http://local', correctionOwner, p),
          throwsStateError,
        );
        expect((await s.read('http://local', correctionOwner))!.same(c), true);
        await s.delete('http://local', correctionOwner, c);
        expect(await s.read('http://local', correctionOwner), isNull);
      },
    );
  }
  test('闭集metadata不含正文token/query/选择类别或可还原批准', () {
    final p = correctionPending();
    expect(p.encoded, isNot(contains('Bearer synthetic')));
    expect(p.encoded, isNot(contains('我偏好')));
    for (final k in ['body', 'category', 'replacement', 'token', 'query']) {
      expect(
        () => PendingMemoryCorrection({...p.data, k: 'private'}),
        throwsFormatException,
      );
    }
    expect(
      () => PendingMemoryCorrection({...p.data, 'phase': 'AUTO_CONFIRM'}),
      throwsFormatException,
    );
  });
  test('owner/env/target/session不可替换；阶段不能倒退/延长', () async {
    final s = MemoryAgentMemoryCorrectionPendingStore();
    final p = correctionPending();
    await s.write('http://local', correctionOwner, p);
    for (final next in [
      {...p.data, 'sessionFingerprint': completionFingerprint('Bearer next')},
      {...p.data, 'targetId': correctionAgent},
      {...p.data, 'ownerId': correctionMemory},
      {
        ...p.data,
        'phase': 'CONFIRM',
        'planDigest': correctionPlan,
        'expiresAt': correctionAt
            .add(const Duration(days: 2))
            .toIso8601String(),
      },
    ]) {
      await expectLater(
        s.write('http://local', correctionOwner, PendingMemoryCorrection(next)),
        throwsA(anyOf(isA<StateError>(), isA<FormatException>())),
      );
    }
    await s.write(
      'http://local',
      correctionOwner,
      correctionPending(phase: 'CONFIRM'),
    );
    await expectLater(
      s.write('http://local', correctionOwner, p),
      throwsStateError,
    );
  });
}
