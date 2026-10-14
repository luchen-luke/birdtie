import 'dart:io';
import 'package:birdtie_client/src/workspace/activity_participation_operation.dart';
import 'package:birdtie_client/src/workspace/entity_action_contract.dart';
import 'package:flutter_test/flutter_test.dart';

const participationOwner = 'a1000000-0000-4000-8000-000000000001';
const participationActivity = 'a1000000-0000-4000-8000-000000000002';
const participationOp = 'a1000000-0000-4000-8000-000000000003';
const participationDigest =
    '4834866d2dca009519508f6cc425db420c9ed529aab7d866b3adc78ce95c1f37';
const participationKey =
    '162bbc84f81c0aa6a17b333801b53f3bfe46ca83fb7f3e17333674a3b482962c';
const testParticipationPending = PendingParticipationOperation(
  operationID: participationOp,
  activityID: participationActivity,
  operation: 'JOIN',
  requestDigest: participationDigest,
);
EntityActionDescriptor participationApproval() => EntityActionDescriptor(
  kind: EntityActionKind.join,
  target: const EntityActionRef('activity', participationActivity),
  available: true,
  label: '报名参加',
  reason: '',
  requiresConfirmation: true,
  operation: 'JOIN',
  reviewedVersion: 'a' * 64,
  reviewedUntil: DateTime.utc(2026, 10, 20, 10),
);
Map<String, dynamic> participationWire({
  bool closure = false,
  bool committed = true,
}) => {
  'schemaVersion': closure
      ? participationClosureSchema
      : participationOperationSchema,
  'ownerId': participationOwner,
  'activityId': participationActivity,
  'operationId': participationOp,
  'operation': 'JOIN',
  'effectKey': participationKey,
  'requestDigest': participationDigest,
  'toolVersion': participationToolVersion,
  'recordedAt': '2026-10-10T08:00:00.123456Z',
  if (closure)
    'closed': true
  else ...{
    'status': committed ? 'COMMITTED' : 'NO_EFFECT',
    if (committed) ...{
      'effectId': 'a1000000-0000-4000-8000-000000000004',
      'state': 'going',
      'effectAt': '2026-10-10T08:00:00.123Z',
    } else
      'reason': 'USER_CLOSED',
  },
};

void main() {
  test(
    'actual native registered HTTP bytes preserve COMMITTED/NO_EFFECT/opaque closure contract',
    () {
      final directory =
          Platform.environment['BIRDTIE_RSVP_NATIVE_WIRE_DIR'] ??
          '../../docs/testing/evidence/unfinished-completion-2026-10-10/rsvp/backend/native-wire/native-final01';
      Object? read(String name) => participationWireJSON(
        File('$directory/$name.json').readAsStringSync(),
      );
      final request = read('committed-request') as Map<String, dynamic>;
      final headers = request['reviewedHeaders'] as Map<String, dynamic>;
      final approved = EntityActionDescriptor(
        kind: EntityActionKind.join,
        target: EntityActionRef('activity', request['activityId']),
        available: true,
        label: '报名',
        reason: '',
        requiresConfirmation: true,
        operation: headers['X-Birdtie-Action-Operation'],
        reviewedVersion: headers['X-Birdtie-Action-Version'],
        reviewedUntil: DateTime.parse(headers['X-Birdtie-Action-Until']),
      );
      final pending = PendingParticipationOperation(
        operationID: request['operationId'],
        activityID: request['activityId'],
        operation: approved.operation,
        requestDigest: participationRequestDigest(
          request['ownerId'],
          request['activityId'],
          request['operationId'],
          approved,
        ),
      );
      final committed =
          (read('committed-response') as Map<String, dynamic>)['data'];
      expect(
        ParticipationOperationReceipt.decode(
          committed,
          request['ownerId'],
          pending,
        ).committed,
        isTrue,
      );
      final closingRequest = read('close-request') as Map<String, dynamic>;
      final closeBody = closingRequest['body'] as Map<String, dynamic>;
      final closeHeaders =
          closingRequest['reviewedHeaders'] as Map<String, dynamic>;
      final closingApproval = EntityActionDescriptor(
        kind: EntityActionKind.join,
        target: EntityActionRef('activity', closingRequest['activityId']),
        available: true,
        label: '报名',
        reason: '',
        requiresConfirmation: true,
        operation: closeBody['operation'],
        reviewedVersion: closeHeaders['X-Birdtie-Action-Version'],
        reviewedUntil: DateTime.parse(closeHeaders['X-Birdtie-Action-Until']),
      );
      final closeDigest = participationRequestDigest(
        closingRequest['ownerId'],
        closingRequest['activityId'],
        closingRequest['operationId'],
        closingApproval,
      );
      expect(closeBody['requestDigest'], closeDigest);
      final closed = PendingParticipationOperation(
        operationID: closingRequest['operationId'],
        activityID: closingRequest['activityId'],
        operation: closeBody['operation'],
        requestDigest: closeDigest,
        stopRequested: true,
      );
      for (final name in ['no-effect-response', 'closure-response']) {
        final raw =
            (read(name) as Map<String, dynamic>)['data']
                as Map<String, dynamic>;
        expect(raw['ownerId'], request['ownerId']);
        if (name == 'no-effect-response') {
          expect(
            ParticipationOperationReceipt.decode(
              raw,
              request['ownerId'],
              closed,
            ).committed,
            isFalse,
          );
        } else {
          ParticipationOperationClosure.decode(raw, request['ownerId'], closed);
          expect(raw.containsKey('status'), isFalse);
          expect(raw.containsKey('effectId'), isFalse);
        }
      }
    },
  );
  test(
    'Go contract vectors use independent Python SHA and fixed UTC microseconds',
    () {
      expect(
        participationEffectKey(participationOwner, participationOp),
        participationKey,
      );
      expect(
        participationRequestDigest(
          participationOwner,
          participationActivity,
          participationOp,
          participationApproval(),
        ),
        participationDigest,
      );
      expect(
        participationDeadline(DateTime.parse('2026-10-20T12:00:00+02:00')),
        '2026-10-20T10:00:00.000000Z',
      );
      expect(
        participationDeadline(DateTime.utc(2026, 10, 20, 10, 0, 0, 123, 456)),
        '2026-10-20T10:00:00.123456Z',
      );
    },
  );
  test('historical committed and closed receipts stay distinct', () {
    expect(
      ParticipationOperationReceipt.decode(
        participationWire(),
        participationOwner,
        testParticipationPending,
      ).committed,
      isTrue,
    );
    expect(
      ParticipationOperationReceipt.decode(
        participationWire(committed: false),
        participationOwner,
        testParticipationPending,
      ).committed,
      isFalse,
    );
    ParticipationOperationClosure.decode(
      participationWire(closure: true),
      participationOwner,
      testParticipationPending.stopping(),
    );
    expect(
      () => ParticipationOperationReceipt.decode(
        participationWire(closure: true),
        participationOwner,
        testParticipationPending,
      ),
      throwsFormatException,
    );
  });
  for (final field in [
    'ownerId',
    'activityId',
    'operationId',
    'operation',
    'effectKey',
    'requestDigest',
    'toolVersion',
    'effectId',
    'state',
    'schemaVersion',
  ]) {
    test('receipt rejects changed $field and keeps the original pending', () {
      final raw = participationWire();
      raw[field] = 'untrusted';
      expect(
        () => ParticipationOperationReceipt.decode(
          raw,
          participationOwner,
          testParticipationPending,
        ),
        throwsFormatException,
      );
    });
  }
  for (final stamp in [
    '2026-02-30T08:00:00Z',
    '2026-10-10T25:00:00Z',
    '2026-10-10T08:00:00.1234567Z',
    '2026-10-10T08:00:00+00:00',
    '2201-01-01T00:00:00Z',
  ]) {
    test('receipt rejects invalid timestamp $stamp', () {
      final raw = participationWire();
      raw['recordedAt'] = stamp;
      expect(
        () => ParticipationOperationReceipt.decode(
          raw,
          participationOwner,
          testParticipationPending,
        ),
        throwsFormatException,
      );
    });
  }
  test(
    'future effect, extra authority, closure status and empty IDs are rejected',
    () {
      final raw = participationWire();
      raw['effectAt'] = '2026-10-10T08:00:01Z';
      expect(
        () => ParticipationOperationReceipt.decode(
          raw,
          participationOwner,
          testParticipationPending,
        ),
        throwsFormatException,
      );
      final extra = participationWire();
      extra['confirmed'] = true;
      expect(
        () => ParticipationOperationReceipt.decode(
          extra,
          participationOwner,
          testParticipationPending,
        ),
        throwsFormatException,
      );
      final closure = participationWire(closure: true);
      closure['status'] = 'COMMITTED';
      expect(
        () => ParticipationOperationClosure.decode(
          closure,
          participationOwner,
          testParticipationPending,
        ),
        throwsFormatException,
      );
      expect(participationID('00000000-0000-0000-0000-000000000000'), isFalse);
    },
  );
  test('duplicate escaped JSON keys cannot overwrite owner binding', () {
    expect(
      () => participationWireJSON(
        r'{"data":{"ownerId":"old","owner\u0049d":"new"}}',
      ),
      throwsFormatException,
    );
    expect(
      () => participationWireJSON('{"data":{},"data":{}}'),
      throwsFormatException,
    );
    expect(
      () => participationEnvelope('{"data":{},"approval":true}'),
      throwsFormatException,
    );
  });
  test('journal is closed metadata, no action approval or private content', () {
    final raw = testParticipationPending.toJson();
    raw['token'] = 'private';
    expect(
      () => PendingParticipationOperation.decode(raw),
      throwsFormatException,
    );
    expect(
      testParticipationPending.toJson().keys,
      unorderedEquals([
        'operationId',
        'activityId',
        'operation',
        'requestDigest',
        'toolVersion',
        'stopRequested',
      ]),
    );
  });
}
