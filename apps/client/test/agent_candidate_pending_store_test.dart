import 'dart:convert';
import 'package:birdtie_client/src/workspace/agent_candidate_pending_store.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'agent_memory_candidate_api_test.dart';

PendingHumanAcceptance pendingAcceptance({
  String key = 'a',
  DateTime? expires,
  String session = 'Bearer t',
  String environment = 'http://fixture',
}) => PendingHumanAcceptance({
  'environment': candidateRecoveryFingerprint(environment),
  'ownerId': candidateOwner,
  'agentId': candidateAgent,
  'sessionFingerprint': candidateRecoveryFingerprint(session),
  'previewId': key * 64,
  'candidateId': candidateTarget,
  'candidateVersion': 1,
  'targetMemoryId': candidateMemory,
  'expectedMemoryVersion': 0,
  'planDigest': 'b' * 64,
  'expiresAt':
      (expires ?? DateTime.now().toUtc().add(const Duration(minutes: 4)))
          .toIso8601String(),
});

void main() {
  test(
    'legitimate offset metadata parses while invalid calendar and offsets block',
    () {
      final p = pendingAcceptance();
      expect(
        PendingHumanAcceptance({
          ...p.data,
          'expiresAt': '2026-10-05T23:10:01+08:00',
        }).expiresAt,
        DateTime.utc(2026, 10, 5, 15, 10, 1),
      );
      for (final stamp in [
        '2026-02-30T00:00:00Z',
        '2026-10-05T25:00:00Z',
        '2026-10-05T00:00:00+24:00',
        '2026-10-05T00:00:00+08:60',
        '2026-10-05T00:00:00Z ',
      ]) {
        expect(
          () => PendingHumanAcceptance({...p.data, 'expiresAt': stamp}),
          throwsFormatException,
        );
      }
    },
  );
  test(
    'write cannot add a ninth or another unknown operation and corrupt prefix blocks write',
    () async {
      final s = MemoryAgentCandidatePendingStore();
      final p = pendingAcceptance();
      await s.write('http://fixture', candidateOwner, p);
      final prefix = s.values.keys.single.substring(
        0,
        s.values.keys.single.length - 64,
      );
      for (final key in ['c', 'd', 'e', 'f', '0', '1', '2']) {
        final v = pendingAcceptance(key: key);
        s.values['$prefix${v.previewID}'] = v.encoded;
      }
      expect(await s.read('http://fixture', candidateOwner), hasLength(8));
      await expectLater(
        s.write('http://fixture', candidateOwner, pendingAcceptance(key: '3')),
        throwsStateError,
      );
      expect(s.values.length, 8);
      s.values[s.values.keys.first] = 'broken';
      await expectLater(
        s.write('http://fixture', candidateOwner, pendingAcceptance(key: '3')),
        throwsFormatException,
      );
      expect(s.values.length, 8);
    },
  );

  TestWidgetsFlutterBinding.ensureInitialized();
  for (final secure in [false, true]) {
    test(
      'closed metadata persists exact operation and session stays metadata secure=$secure',
      () async {
        FlutterSecureStorage.setMockInitialValues({});
        final AgentCandidatePendingStore store = secure
            ? const SecureAgentCandidatePendingStore()
            : MemoryAgentCandidatePendingStore();
        final p = pendingAcceptance();
        await store.write('http://fixture', candidateOwner, p);
        final encoded = (await store.read(
          'http://fixture',
          candidateOwner,
        )).single.encoded;
        expect(encoded, p.encoded);
        expect(encoded, isNot(contains('Bearer t')));
        expect(encoded, isNot(contains('http://fixture')));
        expect(encoded, isNot(contains('statement')));
        expect((jsonDecode(encoded) as Map).containsKey('review'), false);
        expect(await store.read('http://other', candidateOwner), isEmpty);
        expect(await store.read('http://fixture', candidateSaved), isEmpty);
        final changed = PendingHumanAcceptance({
          ...p.data,
          'candidateVersion': 2,
        });
        await expectLater(
          store.write('http://fixture', candidateOwner, changed),
          throwsStateError,
        );
        await expectLater(
          store.delete('http://fixture', candidateOwner, changed),
          throwsStateError,
        );
        final second = pendingAcceptance(key: 'c');
        await expectLater(
          store.write('http://fixture', candidateOwner, second),
          throwsStateError,
        );
        await store.delete('http://fixture', candidateOwner, p);
        await store.write('http://fixture', candidateOwner, second);
        await store.delete('http://fixture', candidateOwner, p);
        expect(
          (await store.read('http://fixture', candidateOwner)).single.previewID,
          second.previewID,
        );
      },
    );
  }
  test(
    'strict closed fields and bounds never deserialize approval or private content',
    () {
      final p = pendingAcceptance();
      for (final bad in [
        {
          ...p.data,
          'review': {'statement': 'private canary'},
        },
        {
          ...p.data,
          'environment': 'http://userinfo:secret@fixture?token=canary',
        },
        {...p.data, 'candidateVersion': 0},
        {...p.data, 'expectedMemoryVersion': -1},
        {...p.data, 'previewId': 'x'},
        {...p.data, 'sessionFingerprint': null},
        {...p.data, 'expiresAt': 'not a time'},
      ]) {
        expect(() => PendingHumanAcceptance(bad), throwsFormatException);
      }
    },
  );
  test(
    'corrupt owner metadata blocks instead of silently dropping unknown operation',
    () async {
      final s = MemoryAgentCandidatePendingStore(), p = pendingAcceptance();
      await s.write('http://fixture', candidateOwner, p);
      s.values[s.values.keys.single] = jsonEncode({
        ...p.data,
        'answers': 'canary',
      });
      await expectLater(
        s.read('http://fixture', candidateOwner),
        throwsFormatException,
      );
    },
  );
}
