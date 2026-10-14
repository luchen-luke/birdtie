import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/workspace/human_causal_control.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const humanOwner = '11111111-1111-4111-8111-111111111111',
    humanTarget = '22222222-2222-4222-8222-222222222222',
    humanOrg = '33333333-3333-4333-8333-333333333333',
    humanResult = '44444444-4444-4444-8444-444444444444',
    humanOp = '55555555-5555-4555-8555-555555555555',
    humanOtherOp = '66666666-6666-4666-8666-666666666666',
    humanBase = 'https://human.fixture';
http.Response humanWire(Object? data, {int status = 200}) =>
    http.Response.bytes(
      utf8.encode(jsonEncode({'data': data})),
      status,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );
String fixtureTextSHA(String text) =>
    sha256.convert(utf8.encode(text.trim())).toString();
String fixtureDigest(
  HumanCausalFamily f,
  Map<String, dynamic> b, {
  String org = humanOrg,
}) => sha256
    .convert(
      utf8.encode(
        jsonEncode(switch (f) {
          HumanCausalFamily.faq => [
            'human-faq-v1',
            org,
            b['operationId'],
            b['action'],
            b['expectedVersion'],
            b['faqId'],
            fixtureTextSHA(b['question'] ?? ''),
            fixtureTextSHA(b['answer'] ?? ''),
            b['published'] ?? false,
          ],
          HumanCausalFamily.context => [
            'human-context-v1',
            b['operationId'],
            b['action'],
            b['expectedVersion'],
            b['contextId'] ?? '',
            b['contextType'] ?? '',
            fixtureTextSHA(b['sourceKey'] ?? ''),
            b['relation'],
          ],
          HumanCausalFamily.report => [
            'human-report-v1',
            b['operationId'],
            'SUBMIT',
            b['targetType'],
            b['targetId'],
            b['reason'],
            fixtureTextSHA(b['details']),
          ],
          HumanCausalFamily.grantRevoke => [
            'human-grant-revoke-v1',
            b['operationId'],
            'REVOKE',
            b['grantId'],
            b['expectedVersion'],
          ],
          HumanCausalFamily.block => [
            'human-block-v1',
            b['operationId'],
            b['action'],
            b['accountId'],
            b['expectedVersion'],
          ],
        }),
      ),
    )
    .toString();
Map<String, dynamic> humanPreviewWire(
  HumanCausalInput input, {
  int seconds = 20,
}) => {
  ...input.fields,
  if (input is HumanFAQInput) 'organizationId': input.organizationID,
  'payloadDigest': fixtureDigest(
    input.family,
    Map<String, dynamic>.from(input.fields),
    org: input is HumanFAQInput ? input.organizationID : humanOrg,
  ),
  'observedAt': DateTime.now().toUtc().toIso8601String(),
  'expiresAt': DateTime.now()
      .toUtc()
      .add(Duration(seconds: seconds))
      .toIso8601String(),
  'approval': 'opaque-original-approval',
};
Map<String, dynamic> humanReceiptWire(
  HumanCausalInput input, {
  bool stopped = false,
  int? version,
}) => stopped
    ? {
        'operationId': input.operationID,
        'family': input.family.wire,
        'status': 'STOPPED',
        'appliedVersion': 0,
      }
    : {
        'operationId': input.operationID,
        'family': input.family.wire,
        'status': 'SUCCEEDED',
        'action': input.action,
        'payloadDigest': fixtureDigest(
          input.family,
          Map<String, dynamic>.from(input.fields),
          org: input is HumanFAQInput ? input.organizationID : humanOrg,
        ),
        'committedAt': DateTime.now().toUtc().toIso8601String(),
        'appliedVersion':
            version ??
            switch (input) {
              HumanReportInput _ => 0,
              HumanFAQInput i =>
                i.action == 'CREATE'
                    ? 1
                    : (i.expectedVersion + 1).clamp(0, humanControlMaxVersion),
              HumanContextInput i => (i.expectedVersion + 1).clamp(
                0,
                humanControlMaxVersion,
              ),
              HumanGrantInput i => (i.expectedVersion + 1).clamp(
                0,
                humanControlMaxVersion,
              ),
              HumanBlockInput i => (i.expectedVersion + 1).clamp(
                0,
                humanControlMaxVersion,
              ),
            },
        if (input is! HumanReportInput || input.targetType != 'general')
          'targetId': switch (input) {
            HumanFAQInput i => i.faqID.isEmpty ? humanResult : i.faqID,
            HumanContextInput i =>
              i.contextID.isEmpty ? humanResult : i.contextID,
            HumanReportInput i => i.targetID,
            HumanGrantInput i => i.grantID,
            HumanBlockInput i => i.accountID,
          },
        if (input is HumanFAQInput ||
            input is HumanContextInput ||
            input is HumanReportInput)
          'resultId': switch (input) {
            HumanFAQInput i => i.faqID.isEmpty ? humanResult : i.faqID,
            HumanContextInput i =>
              i.contextID.isEmpty ? humanResult : i.contextID,
            _ => humanResult,
          },
      };
HumanCausalInput humanInput(
  HumanCausalFamily family,
  Map<String, dynamic> b, {
  String org = humanOrg,
}) => switch (family) {
  HumanCausalFamily.faq => HumanFAQInput(
    organizationID: org,
    operationID: b['operationId'],
    action: b['action'],
    expectedVersion: b['expectedVersion'],
    faqID: b['faqId'],
    question: b['question'] ?? '',
    answer: b['answer'] ?? '',
    published: b['published'],
  ),
  HumanCausalFamily.context => HumanContextInput(
    operationID: b['operationId'],
    action: b['action'],
    expectedVersion: b['expectedVersion'],
    relation: b['relation'],
    contextID: b['contextId'] ?? '',
    contextType: b['contextType'] ?? '',
    sourceKey: b['sourceKey'] ?? '',
  ),
  HumanCausalFamily.report => HumanReportInput(
    operationID: b['operationId'],
    targetType: b['targetType'],
    targetID: b['targetId'],
    reason: b['reason'],
    details: b['details'],
  ),
  HumanCausalFamily.grantRevoke => HumanGrantInput(
    operationID: b['operationId'],
    grantID: b['grantId'],
    expectedVersion: b['expectedVersion'],
  ),
  HumanCausalFamily.block => HumanBlockInput(
    operationID: b['operationId'],
    action: b['action'],
    accountID: b['accountId'],
    expectedVersion: b['expectedVersion'],
  ),
};

class HumanControlFixture {
  HumanControlFixture() {
    client = MockClient(handle);
  }
  late final MockClient client;
  final calls = <http.Request>[],
      faq = <Map<String, dynamic>>[],
      contexts = <Map<String, dynamic>>[],
      reports = <Map<String, dynamic>>[];
  final receipts = <String, Map<String, dynamic>>{};
  int version = 0,
      grantRevision = 4,
      remaining = 5,
      putStatus = 200,
      previewSeconds = 20;
  bool blocked = false,
      revoked = false,
      stateDenied = false,
      receipt404 = false,
      denyAfterWrite = false;
  Future<http.Response?> Function(http.Request)? intercept;
  List<http.Request> get writes =>
      calls.where((r) => r.method == 'PUT').toList();
  Map<String, dynamic> lease() => {
    'observedAt': DateTime.now().toUtc().toIso8601String(),
    'expiresAt': DateTime.now()
        .toUtc()
        .add(const Duration(seconds: 25))
        .toIso8601String(),
  };
  Map<String, dynamic> grant() => {
    'id': humanTarget,
    'recipientAccountId': humanOwner,
    'resourceType': 'account_profile',
    'resourceId': humanOwner,
    'purpose': 'profile_view',
    'actions': ['read'],
    'revision': grantRevision,
    'createdAt': '2026-10-01T00:00:00Z',
    'expiresAt': '2026-10-02T00:00:00Z',
    if (revoked) 'revokedAt': '2026-10-14T00:00:00Z',
  };
  Future<http.Response> handle(http.Request r) async {
    calls.add(r);
    expect(r.headers.containsKey('X-Birdtie-Organization-Workspace'), false);
    final custom = await intercept?.call(r);
    if (custom != null) return custom;
    final p = r.url.path;
    if (p == '/v1/me')
      return humanWire({'id': humanOwner, 'accountType': 'PERSON'});
    if (p.contains('/profile')) return humanWire({'displayName': '合成测试用户'});
    if (p == '/v1/auth/dev-phone/status') return humanWire({'enabled': false});
    if (p == '/v1/cities')
      return humanWire([
        {'id': 'aberdeen-gb', 'name': '阿伯丁'},
        {'id': 'london-gb', 'name': '伦敦'},
      ]);
    if (p == '/v1/me/reports') return humanWire(reports);
    if (p == '/v1/me/blocks')
      return humanWire(
        blocked
            ? [
                {
                  'accountId': humanTarget,
                  'displayName': '合成屏蔽账号',
                  'version': version,
                },
              ]
            : [],
      );
    if (p == '/v1/me/consents')
      return humanWire(
        revoked
            ? []
            : [
                {...grant(), 'displayName': '合成授权账号'},
              ],
      );
    if (p == '/v1/me/organization-invitations') return humanWire([]);
    final family = p.contains('/faq-control')
        ? HumanCausalFamily.faq
        : p.contains('/context-control')
        ? HumanCausalFamily.context
        : p.contains('/report-control')
        ? HumanCausalFamily.report
        : p.contains('/consent-revoke-control')
        ? HumanCausalFamily.grantRevoke
        : p.contains('/block-control')
        ? HumanCausalFamily.block
        : null;
    if (family == null) return humanWire(null, status: 404);
    if (p.contains('/operations/')) {
      final op = p.split('/operations/').last.split('/').first;
      if (r.method == 'GET')
        return humanWire(
          receipts[op],
          status: receipt404 || !receipts.containsKey(op) ? 404 : 200,
        );
      if (p.endsWith('/stop')) {
        expect(r.bodyBytes, isEmpty);
        final data = receipts.putIfAbsent(
          op,
          () => {
            'operationId': op,
            'family': family.wire,
            'status': 'STOPPED',
            'appliedVersion': 0,
          },
        );
        return humanWire(data);
      }
    }
    if (r.method == 'GET') {
      if (stateDenied) return humanWire(null, status: 403);
      return humanWire(switch (family) {
        HumanCausalFamily.faq => {
          'organizationId': humanOrg,
          'role': 'admin',
          'items': faq,
          ...lease(),
        },
        HumanCausalFamily.context => {
          'version': version,
          'items': contexts,
          ...lease(),
        },
        HumanCausalFamily.report => {
          'remainingQuota': remaining,
          'canSubmit': remaining > 0,
          ...lease(),
        },
        HumanCausalFamily.grantRevoke => {
          'grant': grant(),
          'canRevoke': !revoked,
          ...lease(),
        },
        HumanCausalFamily.block => {
          'accountId': humanTarget,
          'version': version,
          'blocked': blocked,
          'canBlock': !blocked,
          'canUnblock': blocked,
          ...lease(),
        },
      });
    }
    final body = jsonDecode(r.body) as Map<String, dynamic>,
        input = humanInput(family, body);
    if (r.method == 'POST' && p.endsWith('/previews'))
      return humanWire(humanPreviewWire(input, seconds: previewSeconds));
    if (r.method == 'PUT') {
      expect(body['approval'], 'opaque-original-approval');
      final rec = humanReceiptWire(
        input,
        version: input is HumanContextInput ? version + 2 : null,
      );
      receipts[input.operationID] = rec;
      switch (input) {
        case HumanBlockInput i:
          blocked = i.action == 'BLOCK';
          version = rec['appliedVersion'];
        case HumanGrantInput _:
          revoked = true;
          grantRevision = rec['appliedVersion'];
        case HumanFAQInput i:
          faq.removeWhere((x) => x['id'] == i.faqID);
          if (i.action != 'DELETE')
            faq.add({
              'id': i.faqID.isEmpty ? humanResult : i.faqID,
              'organizationId': humanOrg,
              'question': i.question,
              'answer': i.answer,
              'published': i.published,
              'revision': rec['appliedVersion'],
              'createdAt': '2026-10-01T00:00:00Z',
              'updatedAt': '2026-10-14T00:00:00Z',
            });
        case HumanContextInput i:
          version += 2;
          if (i.action == 'REMOVE')
            contexts.removeWhere(
              (x) =>
                  x['contextId'] == i.contextID && x['relation'] == i.relation,
            );
          else {
            if (i.relation == 'current')
              contexts.removeWhere(
                (x) => x['contextType'] == 'CITY' && x['relation'] == 'current',
              );
            contexts.add({
              'contextId': humanResult,
              'contextType': i.contextType,
              'sourceKey': i.sourceKey,
              'label': i.sourceKey == 'aberdeen-gb' ? '阿伯丁' : i.sourceKey,
              'relation': i.relation,
              'visibility': 'private',
            });
          }
        case HumanReportInput i:
          remaining--;
          reports.add({
            'id': humanResult,
            'reason': i.reason,
            'status': 'open',
          });
      }
      if (denyAfterWrite) stateDenied = true;
      return humanWire(putStatus == 200 ? rec : null, status: putStatus);
    }
    return humanWire(null, status: 404);
  }
}

Future<BirdtieAuthController> humanAuth(HumanControlFixture f) async {
  final auth = BirdtieAuthController(
    client: f.client,
    apiBaseUrl: humanBase,
    sessionVault: MemorySessionVault()
      ..session = const StoredSession(token: 'human-original', method: 'oidc'),
  );
  await auth.initialize();
  addTearDown(auth.dispose);
  return auth;
}

Future<void> humanSettle(WidgetTester t) async {
  for (var i = 0; i < 3; i++) {
    await t.pump();
    await t.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 30)),
    );
  }
  await t.pumpAndSettle();
}

Future<void> humanTap(WidgetTester t, Finder f) async {
  await t.ensureVisible(f);
  await t.pumpAndSettle();
  await t.tap(f);
  await humanSettle(t);
}

void main() {
  final inputs = <HumanCausalInput>[
    const HumanFAQInput(
      organizationID: humanOrg,
      operationID: humanOp,
      action: 'CREATE',
      expectedVersion: 0,
      question: '如何报名？',
      answer: '请在原活动详情报名。',
      published: false,
    ),
    const HumanContextInput(
      operationID: humanOp,
      action: 'DECLARE',
      expectedVersion: 2,
      relation: 'current',
      contextType: 'CITY',
      sourceKey: 'aberdeen-gb',
    ),
    const HumanReportInput(
      operationID: humanOp,
      targetType: 'general',
      targetID: '',
      reason: 'safety',
      details: '这是足够长的真实问题说明。',
    ),
    const HumanGrantInput(
      operationID: humanOp,
      grantID: humanTarget,
      expectedVersion: 4,
    ),
    const HumanBlockInput(
      operationID: humanOp,
      action: 'BLOCK',
      accountID: humanTarget,
      expectedVersion: 0,
    ),
  ];
  for (final input in inputs) {
    test(
      '${input.family.wire} closed preview/receipt selectors and no body in journal',
      () {
        expect(input.valid, true);
        expect(
          input.digest,
          fixtureDigest(input.family, Map.from(input.fields)),
        );
        final src = DateTime.now().toUtc().add(const Duration(seconds: 10));
        final p = HumanCausalPreview.fromJson(
          humanPreviewWire(input),
          input,
          src,
        );
        expect(p.expiresAt, src);
        expect(
          HumanCausalReceipt.fromJson(
            humanReceiptWire(input),
            input.selector,
          ).status,
          'SUCCEEDED',
        );
        expect(
          HumanCausalReceipt.fromJson(
            humanReceiptWire(input, stopped: true),
            input.selector,
          ).status,
          'STOPPED',
        );
        expect(
          () => HumanCausalPreview.fromJson(
            {...humanPreviewWire(input), 'operationId': humanOtherOp},
            input,
            src,
          ),
          throwsFormatException,
        );
        expect(
          () => HumanCausalReceipt.fromJson({
            ...humanReceiptWire(input, stopped: true),
            'targetId': humanTarget,
          }, input.selector),
          throwsFormatException,
        );
        expect(
          jsonEncode(input.selector.toJson()),
          isNot(contains('question')),
        );
        expect(
          jsonEncode(input.selector.toJson()),
          isNot(contains('sourceKey')),
        );
        expect(jsonEncode(input.selector.toJson()), isNot(contains('details')));
      },
    );
  }
  test(
    'Context actual aggregate may advance by two, wrong returned id rejected',
    () {
      final i = inputs[1];
      expect(
        HumanCausalReceipt.fromJson(
          humanReceiptWire(i, version: 4),
          i.selector,
        ).appliedVersion,
        4,
      );
      expect(
        () => HumanCausalReceipt.fromJson({
          ...humanReceiptWire(i),
          'targetId': humanTarget,
        }, i.selector),
        throwsFormatException,
      );
    },
  );
  test(
    'terminal cap is saturated; nonterminal MAX and invented report revision rejected',
    () {
      const i = HumanGrantInput(
        operationID: humanOp,
        grantID: humanTarget,
        expectedVersion: humanControlMaxVersion,
      );
      expect(i.valid, true);
      expect(
        HumanCausalReceipt.fromJson(
          humanReceiptWire(i),
          i.selector,
        ).appliedVersion,
        humanControlMaxVersion,
      );
      expect(
        const HumanBlockInput(
          operationID: humanOp,
          action: 'BLOCK',
          accountID: humanTarget,
          expectedVersion: humanControlMaxVersion,
        ).valid,
        false,
      );
      expect(
        () => HumanCausalSelector.fromJson({
          ...inputs[2].selector.toJson(),
          'expectedVersion': 0,
        }),
        throwsFormatException,
      );
    },
  );
  test(
    'city slug accepted; private label never enters context input or digest',
    () {
      const i = HumanContextInput(
        operationID: humanOp,
        action: 'DECLARE',
        expectedVersion: 0,
        relation: 'current',
        contextType: 'CITY',
        sourceKey: 'aberdeen-gb',
        localLabel: '阿伯丁',
      );
      expect(i.valid, true);
      expect(i.fields.containsKey('localLabel'), false);
      expect(i.digest, fixtureDigest(i.family, Map.from(i.fields)));
    },
  );
  test('lease extension and unknown fields cannot create approval', () {
    final i = inputs.last,
        s = DateTime.now().toUtc().add(const Duration(seconds: 10));
    expect(
      () => HumanCausalPreview.fromJson(
        {
          ...humanPreviewWire(i),
          'expiresAt': DateTime.now()
              .toUtc()
              .add(const Duration(seconds: 70))
              .toIso8601String(),
        },
        i,
        s,
      ),
      throwsFormatException,
    );
    expect(
      () => HumanCausalPreview.fromJson(
        {...humanPreviewWire(i), 'body': 'private'},
        i,
        s,
      ),
      throwsFormatException,
    );
  });
}
