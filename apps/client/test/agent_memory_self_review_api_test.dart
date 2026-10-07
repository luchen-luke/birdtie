import 'dart:convert';
import 'dart:io';
import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:birdtie_client/src/workspace/agent_memory_correction_api.dart';
import 'package:birdtie_client/src/workspace/agent_memory_self_review.dart';

// Original registered Go HTTP output, never re-date or silently repair its DTO.
const reviewWirePath =
    '../../docs/testing/evidence/human-self-review-field-evidence-2026-10-07/freeze10/wire/';
String reviewOriginalWire([
  String name = 'SYNTHETIC_REGISTERED_HUMAN_SELF_REVIEW_WIRE',
]) {
  final bytes = File('$reviewWirePath$name.json').readAsBytesSync();
  final index =
      jsonDecode(File('${reviewWirePath}index.json').readAsStringSync())
          as List;
  final expected = index.singleWhere((v) => v['label'] == name)['sha256'];
  expect(sha256.convert(bytes).toString(), expected);
  return utf8.decode(bytes);
}

Map<String, dynamic> reviewData() => jsonDecode(reviewOriginalWire())['data'];
DateTime reviewClock() => correctionTime(
  reviewData()['observedAt'],
).add(const Duration(milliseconds: 1));
Map<String, dynamic> reviewMemoryRow() {
  final d = reviewData(), m = d['memories'].single;
  final source = (d['fieldEvidenceSet']['claims'] as List).firstWhere(
    (v) => v['itemKind'] == 'memories',
  )['source'];
  return {
    'schemaVersion': 'agent-memory-v1',
    'id': m['id'],
    'agentId': d['agentId'],
    'ownerType': 'PERSON',
    'ownerId': d['owner']['id'],
    'version': m['version'],
    'memoryType': 'PREFERENCE',
    'memoryKey': 'activity_category:badminton',
    'summary': m['summary'],
    'structuredValue': {
      'activityCategory': 'badminton',
      'nature': 'human-correction',
    },
    'confidence': 1,
    'sourceType': 'EXPLICIT',
    'visibility': 'PRIVATE',
    'status': 'ACTIVE',
    'validFrom': m['validFrom'],
    'validUntil': m['validUntil'],
    'lastReinforcedAt': null,
    'createdAt': m['createdAt'],
    'updatedAt': source['nativeTime'],
  };
}

String get reviewOwner => reviewData()['owner']['id'];
CorrectableMemory reviewSelected() =>
    CorrectableMemory.read(reviewMemoryRow(), reviewOwner);
Map<String, dynamic> reviewBody() => {
  'profileFields': ['preferredActivityTypes'],
  'memoryIds': [reviewSelected().id],
  'policyFamilies': [],
};

class ReviewSendClient extends http.BaseClient {
  ReviewSendClient(this.handler);
  Future<http.Response> Function(http.BaseRequest) handler;
  final requests = <http.BaseRequest>[];
  bool closed = false;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest r) {
    requests.add(
      r,
    ); // Actual synchronous transport entry, not async handler time.
    return handler(r).then(
      (v) => http.StreamedResponse(
        Stream.value(v.bodyBytes),
        v.statusCode,
        headers: v.headers,
      ),
    );
  }

  @override
  void close() {
    closed = true;
  }
}

http.Response reviewResponse(dynamic data, {int status = 200}) => http.Response(
  jsonEncode({'data': data}),
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);
http.Response reviewWireResponse({int status = 200}) => http.Response(
  reviewOriginalWire(),
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

void main() {
  test('同体协议：原registered wrapper原字节仅POST单只读suffix，并保留未知confidence', () async {
    final raw = reviewOriginalWire();
    final client = ReviewSendClient(
      (r) async => http.Response(
        raw,
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      ),
    );
    final api = AgentMemoryCorrectionAPI(
      client: client,
      apiBaseUrl: 'http://local',
    );
    final d = await api.request(
      'POST',
      'self-review',
      'Bearer synthetic',
      body: reviewBody(),
    );
    final view = MemorySelfReview.read(
      d,
      owner: reviewOwner,
      selected: reviewSelected(),
      now: reviewClock(),
    );
    expect(client.requests, hasLength(1));
    final request = client.requests.single as http.Request;
    expect(request.url.path, '/v1/me/agent-context/self-review');
    expect(request.headers['Authorization'], 'Bearer synthetic');
    expect(jsonDecode(request.body), reviewBody());
    expect(view.conflictCategories, ['badminton']);
    expect(view.memory['summary'], reviewSelected().summary);
    expect(view.confidenceProvided, isFalse);
    expect(view.raw['grantsAuthority'], isFalse);
    expect(() => view.raw['notice'] = 'changed', throwsUnsupportedError);
    api.dispose();
    expect(client.closed, isFalse);
  });
  for (final wrong in [
    'get',
    'suffix',
    'empty',
    'owner',
    'policy',
    'null',
    'twoMemory',
    'field',
  ]) {
    test('同体协议：$wrong请求拒绝在同步send前', () async {
      final client = ReviewSendClient((r) async => reviewWireResponse());
      final api = AgentMemoryCorrectionAPI(
        client: client,
        apiBaseUrl: 'http://local',
      );
      final body = reviewBody();
      switch (wrong) {
        case 'empty':
          body['memoryIds'] = [];
          break;
        case 'owner':
          body['ownerId'] = reviewOwner;
          break;
        case 'policy':
          body['policyFamilies'] = ['AUTONOMY'];
          break;
        case 'null':
          body['profileFields'] = null;
          break;
        case 'twoMemory':
          body['memoryIds'] = [reviewSelected().id, reviewSelected().id];
          break;
        case 'field':
          body['profileFields'] = ['privateCityHistory'];
          break;
      }
      await expectLater(
        api.request(
          wrong == 'get' ? 'GET' : 'POST',
          wrong == 'suffix' ? 'self-review/confirm' : 'self-review',
          'Bearer synthetic',
          body: body,
        ),
        throwsFormatException,
      );
      expect(client.requests, isEmpty);
    });
  }
  for (final wrong in [
    'owner',
    'agent',
    'selector',
    'version',
    'summary',
    'recordTime',
    'expired',
    'future',
    'lease',
    'unknownScore',
    'grant',
    'sourceOwner',
    'sourceRevision',
    'sourceTime',
    'captured',
    'wrongSubject',
    'missingClaim',
    'duplicateClaim',
    'wrongConflict',
    'autoWinner',
    'missingConflict',
    'scope',
    'count',
    'bytes',
  ]) {
    test('同体协议：$wrong不能混入当前声明或来源', () {
      final d = reviewData(), claims = d['fieldEvidenceSet']['claims'] as List;
      switch (wrong) {
        case 'owner':
          d['owner']['id'] = '40000000-0000-4000-8000-000000000099';
          break;
        case 'agent':
          d['agentId'] = '40000000-0000-4000-8000-000000000099';
          break;
        case 'selector':
          d['selection']['memoryIds'] = [];
          break;
        case 'version':
          d['memories'][0]['version']++;
          break;
        case 'summary':
          d['memories'][0]['summary'] = '假的正文';
          break;
        case 'recordTime':
          d['memories'][0]['createdAt'] = d['observedAt'];
          break;
        case 'expired':
          d['expiresAt'] = d['observedAt'];
          break;
        case 'future':
          d['observedAt'] = reviewClock()
              .add(const Duration(hours: 1))
              .toIso8601String();
          break;
        case 'lease':
          d['expiresAt'] = reviewClock()
              .add(const Duration(minutes: 6))
              .toIso8601String();
          break;
        case 'unknownScore':
          d['memories'][0]['confidence'] = {
            'semantics': 'CALIBRATED_PROBABILITY',
            'value': 1,
          };
          break;
        case 'grant':
          d['grantsAuthority'] = true;
          break;
        case 'sourceOwner':
          d['fieldEvidenceSet']['owner']['id'] =
              '40000000-0000-4000-8000-000000000099';
          break;
        case 'sourceRevision':
          claims[0]['source']['version']['revision']++;
          break;
        case 'sourceTime':
          claims[0]['source']['nativeTime'] = d['observedAt'];
          break;
        case 'captured':
          claims[0]['capturedAt'] = d['observedAt'];
          break;
        case 'wrongSubject':
          claims[0]['subjectId'] = '40000000-0000-4000-8000-000000000099';
          break;
        case 'missingClaim':
          claims.removeLast();
          break;
        case 'duplicateClaim':
          claims.add(claims.first);
          break;
        case 'wrongConflict':
          d['fieldEvidenceSet']['conflicts'][0]['claimIds'][0] = 'unselected';
          break;
        case 'autoWinner':
          d['fieldEvidenceSet']['conflicts'][0]['winner'] = 'profile';
          break;
        case 'missingConflict':
          d['fieldEvidenceSet']['conflicts'] = [];
          break;
        case 'scope':
          d['fieldEvidenceSet']['scope'] = 'FILTERED_CONTEXT';
          break;
        case 'count':
          while (claims.length <= 100) {
            claims.add(claims.first);
          }
          break;
        case 'bytes':
          d['notice'] = 'x' * 65536;
          break;
      }
      expect(
        () => MemorySelfReview.read(
          d,
          owner: reviewOwner,
          selected: reviewSelected(),
          now: reviewClock(),
        ),
        throwsFormatException,
      );
    });
  }
  for (final name in [
    'SYNTHETIC_REGISTERED_HUMAN_SELF_REVIEW_UNCONFIGURED_false_WIRE',
    'SYNTHETIC_REGISTERED_HUMAN_SELF_REVIEW_UNCONFIGURED_true_WIRE',
    'SYNTHETIC_REGISTERED_HUMAN_SELF_REVIEW_CONFIGURED_WIRE',
  ]) {
    test('同体协议：原$name的不同selector不得冒充固定1/1/0成功', () {
      final d = jsonDecode(reviewOriginalWire(name))['data'];
      expect(
        () => MemorySelfReview.read(
          d,
          owner: reviewOwner,
          selected: reviewSelected(),
          now: correctionTime(
            d['observedAt'],
          ).add(const Duration(milliseconds: 1)),
        ),
        throwsFormatException,
      );
    });
  }
}
