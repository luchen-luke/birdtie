import 'dart:convert';
import 'package:birdtie_client/src/workspace/business_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const identityBusiness = 'be000000-0000-4000-8000-000000000001';
const identityPerson = 'be000000-0000-4000-8000-000000000002';
const identityPrincipal = 'be000000-0000-4000-8000-000000000003';
const identityAgent = 'be000000-0000-4000-8000-000000000004';
Map<String, dynamic> identityWire({
  bool exists = false,
  int version = 2,
  DateTime? now,
  String status = 'suspended',
}) {
  final at = (now ?? DateTime.now()).toUtc();
  return {
    'schema': 'business-agent-identity-v1',
    'businessId': identityBusiness,
    'businessName': '本地合成商家',
    'principal': {'type': 'BUSINESS', 'id': identityPrincipal},
    'role': 'owner',
    'claimStatus': 'verified',
    'claimState': 'verified',
    'claimVersion': version,
    'agent': exists
        ? {
            'id': identityAgent,
            'type': 'BUSINESS',
            'status': status,
            'profile': {
              'agentId': identityAgent,
              'ownerType': 'BUSINESS',
              'ownerId': identityPrincipal,
              'profileVersion': 1,
              'createdAt': at.toIso8601String(),
              'updatedAt': at.toIso8601String(),
            },
          }
        : null,
    'observedAt': at.toIso8601String(),
    'validUntil': at.add(const Duration(seconds: 30)).toIso8601String(),
    'metadataOnly': true,
    'runtimeAvailable': false,
    'tools': [],
  };
}

http.Response identityReply(Map<String, dynamic> v) => http.Response(
  jsonEncode({'data': v}),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);
void main() {
  test('身份API复用本人借用来源并完整提交当前经营权版本', () async {
    final calls = <http.Request>[];
    final at = DateTime.now().toUtc();
    final probe = identityReply(identityWire(now: at));
    expect(jsonDecode(utf8.decode(probe.bodyBytes))['data'], isMap);
    final api = BusinessApi(
      authorizationHeader: () => 'Bearer contract-person',
      apiBaseUrl: 'https://business.test',
      client: MockClient((r) async {
        calls.add(r);
        return identityReply(identityWire(exists: r.method == 'POST', now: at));
      }),
    );
    final before = await api.readAgentIdentity(identityBusiness, now: at);
    expect(before.agentID, isNull);
    final saved = await api.establishAgentIdentity(
      identityBusiness,
      2,
      now: at,
    );
    expect(saved.agentID, identityAgent);
    expect(saved.agentStatus, 'suspended');
    expect(calls.map((r) => r.method), ['GET', 'POST']);
    expect(
      calls.last.url.path,
      '/v1/me/businesses/$identityBusiness/agent-identity',
    );
    expect(calls.last.headers['Authorization'], 'Bearer contract-person');
    expect(jsonDecode(calls.last.body), {'expectedClaimVersion': 2});
  });
  test('身份API不接受active或跨主体Profile，坏成功保持结果未知', () async {
    final at = DateTime.now().toUtc();
    var wire = identityWire(exists: true, now: at, status: 'active');
    final api = BusinessApi(
      authorizationHeader: () => 'Bearer contract-person',
      apiBaseUrl: 'https://business.test',
      client: MockClient((_) async => identityReply(wire)),
    );
    await expectLater(
      api.establishAgentIdentity(identityBusiness, 2, now: at),
      throwsA(
        isA<BusinessApiException>().having(
          (e) => e.outcomeUnknown,
          'unknown',
          true,
        ),
      ),
    );
    wire = identityWire(exists: true, now: at);
    (wire['agent']['profile'] as Map)['ownerId'] = identityPerson;
    await expectLater(
      api.readAgentIdentity(identityBusiness, now: at),
      throwsA(isA<BusinessApiException>()),
    );
    wire = identityWire(now: at)..['enabled'] = true;
    await expectLater(
      api.readAgentIdentity(identityBusiness, now: at),
      throwsA(isA<BusinessApiException>()),
    );
  });
}
