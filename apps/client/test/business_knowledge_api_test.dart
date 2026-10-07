import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/business_api.dart';
import 'package:birdtie_client/src/workspace/business_knowledge_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'business_console_controller_test.dart' show merchant, reply;

const knowledgePlace = 'be000000-0000-4000-8000-000000000088';
Map<String, dynamic> knowledgeAnswer({
  String status = 'known',
  String? until,
  bool venue = false,
}) => {
  'businessId': merchant,
  'mode': 'HUMAN_VERIFIED_RULES',
  'status': status,
  'answer': status == 'known' ? '本地合成资料回答' : '没有当前可引用的已核验资料。',
  'sourceVersion': List.filled(64, 'a').join(),
  'sources': status == 'known'
      ? [
          {
            'type': venue ? 'business_venue' : 'business_profile',
            'id': venue ? knowledgePlace : merchant,
            'version': 2,
            'validUntil':
                until ??
                DateTime.now()
                    .toUtc()
                    .add(const Duration(hours: 1))
                    .toIso8601String(),
          },
        ]
      : <dynamic>[],
  'agentStatus': 'unavailable',
  'modelStatus': 'unavailable',
  'tools': <String>[],
};
void main() {
  test(
    'six closed human queries exact body; no permission or private transport exposed',
    () async {
      for (final q in businessKnowledgeQuestions) {
        final venue = businessKnowledgeNeedsPlace(q);
        final api = BusinessApi(
          authorizationHeader: () => 'Bearer local',
          apiBaseUrl: 'http://fixture',
          client: MockClient((r) async {
            expect(r.method, 'POST');
            expect(r.url.path, '/v1/me/businesses/$merchant/knowledge/ask');
            expect(r.url.query, isEmpty);
            expect(jsonDecode(r.body), {
              'query': q,
              'placeId': venue ? knowledgePlace : '',
            });
            expect(r.headers['Authorization'], 'Bearer local');
            expect(
              r.headers.keys.any(
                (k) => k.toLowerCase().contains('organization'),
              ),
              isFalse,
            );
            return reply(knowledgeAnswer(venue: venue));
          }),
        );
        final answer = await api.askKnowledge(
          merchant,
          q,
          placeID: venue ? knowledgePlace : '',
        );
        expect(answer.status, 'known');
        expect(answer.sources.single.version, 2);
        expect(() => answer.sources.clear(), throwsUnsupportedError);
        api.dispose();
      }
    },
  );
  test('unknown sources empty; source provenance closes known status', () {
    final now = DateTime.utc(2026, 10, 4);
    BusinessKnowledgeAnswer read(Map<String, dynamic> m) =>
        BusinessKnowledgeAnswer.read(
          m,
          businessID: merchant,
          query: '营业时间',
          placeID: '',
          now: now,
        );
    expect(read(knowledgeAnswer(status: 'unknown')).sources, isEmpty);
    for (final change in <void Function(Map<String, dynamic>)>[
      (m) => m['businessId'] = knowledgePlace,
      (m) => m['mode'] = 'AGENT',
      (m) => m['tools'] = ['book'],
      (m) => m['modelStatus'] = 'enabled',
      (m) => m['rightsNote'] = 'PRIVATE',
      (m) => m['sourceVersion'] = 'a',
      (m) => m['sources'] = [],
      (m) =>
          m['sources'] = [...(m['sources'] as List), ...(m['sources'] as List)],
      (m) => (m['sources'] as List).single['id'] = knowledgePlace,
      (m) => (m['sources'] as List).single['version'] = 0,
      (m) => (m['sources'] as List).single['sourceUrl'] = 'PRIVATE',
    ]) {
      final m = knowledgeAnswer(until: '2027-01-01T00:00:00Z');
      change(m);
      expect(() => read(m), throwsFormatException);
    }
    final contradictory = knowledgeAnswer(status: 'unknown');
    contradictory['sources'] = knowledgeAnswer()['sources'];
    expect(() => read(contradictory), throwsFormatException);
  });
  test('strict RFC3339 Go offsets calendar normalization finite future', () {
    expect(
      businessKnowledgeStamp('2026-10-04T12:30:22.123456789+08:00'),
      DateTime.utc(2026, 10, 4, 4, 30, 22, 123, 456),
    );
    expect(
      businessKnowledgeStamp('2026-10-04T00:00:00-03:30'),
      DateTime.utc(2026, 10, 4, 3, 30),
    );
    for (final raw in [
      '2026-13-01T00:00:00Z',
      '2026-02-29T00:00:00Z',
      '2026-10-32T00:00:00Z',
      '2026-10-04T25:00:00Z',
      '2026-10-04T00:60:00Z',
      '2026-10-04T00:00:60Z',
      '2026-10-04T00:00:00+24:00',
      '2026-10-04T00:00:00+08:60',
      '2026-10-04T00:00:00',
      '0000-01-01T00:00:00Z',
      '9999-12-31T23:59:59-01:00',
      '2026-10-04T00:00:00Z\n',
    ]) {
      expect(
        () => businessKnowledgeStamp(raw),
        throwsFormatException,
        reason: raw,
      );
    }
    expect(
      () => BusinessKnowledgeAnswer.read(
        knowledgeAnswer(until: '2026-10-04T08:00:00+08:00'),
        businessID: merchant,
        query: '营业时间',
        placeID: '',
        now: DateTime.utc(2026, 10, 4),
      ),
      throwsFormatException,
    );
  });
  test(
    'readonly query errors never classify as an uncertain business write',
    () async {
      for (final status in [401, 403, 409, 503]) {
        final api = BusinessApi(
          authorizationHeader: () => 'Bearer local',
          apiBaseUrl: 'http://fixture',
          client: MockClient((r) async => reply(null, status)),
        );
        await expectLater(
          api.askKnowledge(merchant, '营业时间'),
          throwsA(
            isA<BusinessApiException>()
                .having((e) => e.status, 'status', status)
                .having((e) => e.outcomeUnknown, 'write outcome', false),
          ),
        );
        api.dispose();
      }
    },
  );
  test(
    'late token and timeout fail closed without mutation uncertainty',
    () async {
      String token = 'Bearer old';
      final pending = Completer<http.Response>();
      final api = BusinessApi(
        authorizationHeader: () => token,
        apiBaseUrl: 'http://fixture',
        client: MockClient((r) => pending.future),
      );
      final future = api.askKnowledge(merchant, '营业时间');
      token = 'Bearer new';
      pending.complete(reply(knowledgeAnswer()));
      await expectLater(
        future,
        throwsA(
          isA<BusinessApiException>()
              .having((e) => e.status, 'status', 409)
              .having((e) => e.outcomeUnknown, 'readonly', false),
        ),
      );
      api.dispose();
      final timed = BusinessApi(
        authorizationHeader: () => token,
        apiBaseUrl: 'http://fixture',
        timeout: const Duration(milliseconds: 1),
        client: MockClient((r) => Completer<http.Response>().future),
      );
      await expectLater(
        timed.askKnowledge(merchant, '营业时间'),
        throwsA(
          isA<BusinessApiException>().having(
            (e) => e.outcomeUnknown,
            'readonly',
            false,
          ),
        ),
      );
      timed.dispose();
    },
  );
  test('unsupported query or implicit venue is rejected before HTTP', () async {
    var calls = 0;
    final api = BusinessApi(
      authorizationHeader: () => 'Bearer local',
      apiBaseUrl: 'http://fixture',
      client: MockClient((r) async {
        calls++;
        return reply(knowledgeAnswer());
      }),
    );
    for (final q in ['帮我预约', '场地适用场景']) {
      await expectLater(
        api.askKnowledge(merchant, q),
        throwsA(
          isA<BusinessApiException>().having((e) => e.status, 'status', 400),
        ),
      );
    }
    expect(calls, 0);
    api.dispose();
  });
}
