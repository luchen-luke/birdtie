import 'dart:convert';

import 'package:birdtie_client/src/workspace/agent_activity_join_pending_store.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';

import 'agent_activity_join_api_test.dart';

PendingAgentActivityJoin joinPending({
  String phase = 'EXECUTE',
  String approval = joinApproval,
}) => PendingAgentActivityJoin(
  environmentDigest: activityJoinFingerprint('http://local'),
  ownerID: joinOwner,
  agentID: joinAgent,
  taskID: joinTask,
  activityID: joinActivity,
  approvalID: approval,
  logicalOperationID: joinLogical,
  actionID: joinAction,
  bindingDigest: joinDigest,
  expectedTaskUpdatedAt: joinSelection().expectedTaskUpdatedAt
      .toIso8601String(),
  replyTurnDigest: joinTurn,
  phase: phase,
);
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  test(
    'secure competing reservations retain one original operation and same selector cannot acquire fresh execution',
    () async {
      const a = SecureAgentActivityJoinPendingStore(),
          b = SecureAgentActivityJoinPendingStore();
      final first = joinPending(), next = joinPending(approval: joinOther);
      final wins = await Future.wait([
        a.reserve('http://local', joinOwner, first),
        b.reserve('http://local', joinOwner, next),
      ]);
      expect(wins.where((v) => v).length, 1);
      final actual = (await a.read('http://local', joinOwner))!;
      expect(actual.same(first), true);
      expect(
        (await b.read('http://local', joinOwner))!.encoded,
        actual.encoded,
      );
      expect(await b.reserve('http://local', joinOwner, first), false);
      expect(await b.read('http://other', joinOwner), null);
      expect(await b.read('http://local', joinOther), null);
    },
  );
  test(
    'phase CAS is monotonic and late clear cannot delete changed or next operation',
    () async {
      const store = SecureAgentActivityJoinPendingStore();
      final p = joinPending(phase: 'CONFIRM'),
          executing = p.withPhase('EXECUTE'),
          stopping = p.withPhase('STOP');
      expect(await store.reserve('http://local', joinOwner, p), true);
      await store.replace('http://local', joinOwner, p, executing);
      expect(store.delete('http://local', joinOwner, p), throwsStateError);
      await store.replace('http://local', joinOwner, executing, stopping);
      expect(
        store.replace('http://local', joinOwner, stopping, executing),
        throwsStateError,
      );
      await store.delete('http://local', joinOwner, stopping);
      final next = joinPending(approval: joinOther);
      expect(await store.reserve('http://local', joinOwner, next), true);
      expect(
        store.delete('http://local', joinOwner, stopping),
        throwsStateError,
      );
      expect((await store.read('http://local', joinOwner))!.same(next), true);
    },
  );
  test(
    'journal contains only owner/environment/selectors/digests/phase and rejects forged stored capability',
    () async {
      final p = joinPending();
      expect(jsonDecode(p.encoded).keys.toSet(), {
        'schema',
        'environmentDigest',
        'ownerId',
        'agentId',
        'taskId',
        'activityId',
        'approvalId',
        'logicalOperationId',
        'actionId',
        'bindingDigest',
        'expectedTaskUpdatedAt',
        'replyTurnDigest',
        'phase',
      });
      for (final forbidden in [
        'Bearer',
        'title',
        'description',
        'review',
        'expiresAt',
        'grantId',
        'latitude',
        'approved',
      ]) {
        expect(p.encoded.contains('"$forbidden"'), false);
        final malformed = Map<String, dynamic>.from(jsonDecode(p.encoded))
          ..[forbidden] = true;
        expect(
          () => PendingAgentActivityJoin.decode(jsonEncode(malformed)),
          throwsFormatException,
        );
      }
      expect(
        () => PendingAgentActivityJoin.decode(
          p.encoded.replaceFirst('"phase":', '"phase":"STOP","phase":'),
        ),
        throwsFormatException,
      );
      const store = SecureAgentActivityJoinPendingStore();
      expect(
        store.reserve('http://other', joinOwner, p),
        throwsFormatException,
      );
      expect(
        store.reserve('http://local', joinOther, p),
        throwsFormatException,
      );
    },
  );
  test(
    'malformed secure bytes are retained and cannot be overwritten by fresh approval',
    () async {
      const storage = FlutterSecureStorage(),
          store = SecureAgentActivityJoinPendingStore();
      final p = joinPending();
      final key =
          'birdtie.agent-activity-join.pending.v1.${activityJoinFingerprint('http://local\n$joinOwner')}';
      await storage.write(key: key, value: '{"approved":true}');
      expect(store.read('http://local', joinOwner), throwsFormatException);
      expect(
        store.reserve('http://local', joinOwner, p),
        throwsFormatException,
      );
      expect(await storage.read(key: key), '{"approved":true}');
    },
  );
}
