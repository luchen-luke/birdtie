import 'package:birdtie_client/src/content/agent_profile_completion_pending_store.dart';
import 'package:birdtie_client/src/content/agent_profile_completion_api.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'agent_profile_completion_api_test.dart';

PendingProfileCompletion completionPending({
  String phase = 'PREVIEW',
  String id = completionPreviewID,
}) => PendingProfileCompletion({
  'environment': completionFingerprint('http://local'),
  'ownerId': completionOwner,
  'agentId': completionAgent,
  'sessionFingerprint': completionFingerprint('Bearer synthetic'),
  'previewId': id,
  'memoryId': completionMemory,
  'memoryVersion': 1,
  'expectedProfileVersion': 2,
  'phase': phase,
  'planDigest': phase == 'ACCEPT' ? completionPlan : null,
  'expiresAt': completionAt
      .add(Duration(minutes: phase == 'ACCEPT' ? 5 : 60))
      .toIso8601String(),
});
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  for (final secure in [false, true]) {
    test(
      'journal ${secure ? 'Secure' : 'Memory'} reserve/readback 不可替换与 exact delete',
      () async {
        FlutterSecureStorage.setMockInitialValues({});
        final AgentProfileCompletionPendingStore s = secure
            ? const SecureAgentProfileCompletionPendingStore()
            : MemoryAgentProfileCompletionPendingStore();
        final p = completionPending(), a = completionPending(phase: 'ACCEPT');
        await s.write('http://local', completionOwner, p);
        expect((await s.read('http://local', completionOwner))!.same(p), true);
        await expectLater(
          s.write(
            'http://local',
            completionOwner,
            completionPending(id: completionMemory),
          ),
          throwsStateError,
        );
        await s.write('http://local', completionOwner, a);
        await expectLater(
          s.delete('http://local', completionOwner, p),
          throwsStateError,
        );
        expect((await s.read('http://local', completionOwner))!.same(a), true);
        await s.delete('http://local', completionOwner, a);
        expect(await s.read('http://local', completionOwner), isNull);
      },
    );
  }
  test('journal 仅闭集引用，无 token 正文或批准', () {
    final p = completionPending();
    expect(p.encoded, isNot(contains('Bearer synthetic')));
    expect(p.encoded, isNot(contains('我偏好')));
    expect(
      () => PendingProfileCompletion({...p.data, 'body': 'private'}),
      throwsFormatException,
    );
    expect(
      () => PendingProfileCompletion({...p.data, 'phase': 'AUTO_ACCEPT'}),
      throwsFormatException,
    );
    expect(
      p.matches(
        ProfileCompletionReceipt.read(completionReceipt(), completionOwner),
      ),
      true,
    );
  });
}
