import 'package:birdtie_client/src/content/moment_control.dart';
import 'package:flutter_test/flutter_test.dart';

const mcOwner = '11111111-1111-4111-8111-111111111111',
    mcTarget = '22222222-2222-4222-8222-222222222222',
    mcOp = '33333333-3333-4333-8333-333333333333';
Map<String, Object?> mcInput({String? activity}) => {
  'cityId': 'aberdeen-gb',
  'placeId': '',
  'title': '中文<自己的稿>',
  'body': '本人记录',
  'timePrecision': 'unknown',
  'locationPrecision': 'city',
  if (activity != null) 'activityId': activity,
};
Map<String, Object?> mcPreview(
  MomentControlInput input, {
  int seconds = 30,
  bool preserved = false,
}) {
  final now = DateTime.now().toUtc();
  return {
    'operationId': input.operationID,
    'action': input.action,
    'payloadDigest': input.digest,
    'observedAt': now.toIso8601String(),
    'expiresAt': now.add(Duration(seconds: seconds)).toIso8601String(),
    'approval': 'opaque-actual-synthetic-token',
    'expectedRevision': input.expectedRevision,
    'inputSha256': input.inputHash,
    'sourceVersion': List.filled(64, 'a').join(),
    'preservedPlaceUnavailable': preserved,
    if (input.action != 'CREATE') 'momentId': input.momentID,
    if (input.input != null)
      'input': {...input.input!, 'occurredAt': input.input!['occurredAt']},
  };
}

Map<String, Object> mcReceipt(MomentControlInput input, {bool stop = false}) =>
    stop
    ? {
        'operationId': input.operationID,
        'status': 'STOPPED',
        'appliedRevision': 0,
      }
    : {
        'operationId': input.operationID,
        'status': 'SUCCEEDED',
        'appliedRevision': input.expectedRevision == momentControlMaxRevision
            ? momentControlMaxRevision
            : input.expectedRevision + 1,
        'action': input.action,
        'momentId': input.momentID.isEmpty ? mcTarget : input.momentID,
        'expectedRevision': input.expectedRevision,
        'payloadDigest': input.digest,
        'committedAt': DateTime.now().toUtc().toIso8601String(),
      };
void main() {
  test('ASCII base64 tuple matches the canonical Go144 contract vector', () {
    final input = MomentControlInput(
      operationID: mcOp,
      action: 'CREATE',
      expectedRevision: 0,
      input: mcInput(),
    );
    expect(
      input.inputHash,
      'a518420ce2504f047a53bc1e4b7e34a24a4cf772d267b9dfa4b011e1c098f26a',
    );
    expect(
      input.digest,
      '653411493f706ea41ea76b54362a954d601dcec6dbe4cd86a2f088660dded516',
    );
  });
  test('UTC digest trims fractional zeroes and preserves microseconds', () {
    expect(momentUTC(DateTime.utc(2025)), '2025-01-01T00:00:00Z');
    expect(
      momentUTC(DateTime.utc(2025, 1, 1, 0, 0, 0, 123, 400)),
      '2025-01-01T00:00:00.1234Z',
    );
  });
  test(
    'context omitted and explicit clear have different canonical digests',
    () {
      final a = MomentControlInput(
        operationID: mcOp,
        action: 'UPDATE',
        momentID: mcTarget,
        expectedRevision: 1,
        input: mcInput(),
      );
      final b = MomentControlInput(
        operationID: mcOp,
        action: 'UPDATE',
        momentID: mcTarget,
        expectedRevision: 1,
        input: mcInput(activity: ''),
      );
      expect(a.inputHash, isNot(b.inputHash));
      expect(a.digest, isNot(b.digest));
      expect(
        MomentControlPreview.fromJson(mcPreview(a), a).input.digest,
        a.digest,
      );
    },
  );
  for (final bad in ['extra', 'body', 'peer', 'ttl', 'sha'])
    test('strict concrete Moment preview rejects $bad', () {
      final input = MomentControlInput(
            operationID: mcOp,
            action: 'CREATE',
            expectedRevision: 0,
            input: mcInput(),
          ),
          raw = mcPreview(input);
      switch (bad) {
        case 'extra':
          raw['imageBase64'] = 'not allowed';
        case 'body':
          (raw['input'] as Map)['body'] = 'changed';
        case 'peer':
          raw['operationId'] = mcOwner;
        case 'ttl':
          raw['expiresAt'] = DateTime.now()
              .toUtc()
              .add(const Duration(seconds: 90))
              .toIso8601String();
        case 'sha':
          raw['inputSha256'] = List.filled(64, 'b').join();
      }
      expect(
        () => MomentControlPreview.fromJson(raw, input),
        throwsFormatException,
      );
    });
  test('terminal withdraw saturates max and stopped rejects current body', () {
    final input = MomentControlInput(
      operationID: mcOp,
      action: 'WITHDRAW',
      momentID: mcTarget,
      expectedRevision: momentControlMaxRevision,
    );
    expect(
      MomentControlReceipt.fromJson(
        mcReceipt(input),
        operationID: mcOp,
        digest: input.digest,
      ).appliedRevision,
      momentControlMaxRevision,
    );
    expect(
      () => MomentControlInput(
        operationID: mcOp,
        action: 'UPDATE',
        momentID: mcTarget,
        expectedRevision: momentControlMaxRevision,
        input: mcInput(),
      ),
      throwsFormatException,
    );
    expect(
      () => MomentControlReceipt.fromJson({
        ...mcReceipt(input, stop: true),
        'body': 'private',
      }, operationID: mcOp),
      throwsFormatException,
    );
  });
}
