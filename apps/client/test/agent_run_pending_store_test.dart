import 'package:birdtie_client/src/workspace/agent_run_pending_store.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'agent_run_api_test.dart' show runOwner, runAgent, runID;

class TestRunPendingStore implements AgentRunPendingStore {
  PendingAgentRunCancel? value;
  @override
  Future<PendingAgentRunCancel?> read(String e, String o) async =>
      value?.owner == o && value?.environment == agentRunFingerprint(e)
      ? value
      : null;
  @override
  Future<void> reserve(String e, String o, PendingAgentRunCancel p) async {
    if (value != null && value!.encoded != p.encoded)
      throw StateError('pending');
    value = p;
  }

  @override
  Future<void> delete(String e, String o, PendingAgentRunCancel p) async {
    if (value != null && value!.encoded != p.encoded)
      throw StateError('changed');
    value = null;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  test('安全selector按本人环境隔离，迟到删除不能清除下一操作', () async {
    const store = SecureAgentRunPendingStore();
    final p = PendingAgentRunCancel(
      agentRunFingerprint('http://local'),
      runOwner,
      runAgent,
      runID,
      1,
      runID,
    );
    await store.reserve('http://local', runOwner, p);
    expect((await store.read('http://local', runOwner))!.encoded, p.encoded);
    expect(await store.read('http://other', runOwner), null);
    final next = PendingAgentRunCancel(
      p.environment,
      runOwner,
      runAgent,
      runID,
      2,
      runAgent,
    );
    expect(store.reserve('http://local', runOwner, next), throwsStateError);
    expect(store.delete('http://local', runOwner, next), throwsStateError);
    await store.delete('http://local', runOwner, p);
    expect(await store.read('http://local', runOwner), null);
    expect(p.encoded.contains('Bearer'), false);
    expect(
      () => PendingAgentRunCancel.decode(
        p.encoded.replaceFirst('"nonce":', '"confirmed":true,"nonce":'),
      ),
      throwsFormatException,
    );
  });
}
