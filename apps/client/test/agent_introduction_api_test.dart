import 'dart:convert';
import 'dart:io';
import 'package:birdtie_client/src/workspace/agent_introduction_api.dart';
import 'package:birdtie_client/src/workspace/model_egress_api.dart'
    show egressTime;
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const introOwner = '11111111-1111-4111-8111-111111111111';
const introSource = '22222222-2222-4222-8222-222222222222';
const introPeer = '33333333-3333-4333-8333-333333333333';
const introPeerIntent = '44444444-4444-4444-8444-444444444444';
const introAgent = '55555555-5555-4555-8555-555555555555';
String introStamp([Duration d = const Duration(hours: 1)]) =>
    DateTime.now().toUtc().add(d).toIso8601String();
http.Response introResponse(dynamic d, {int status = 200}) =>
    http.Response.bytes(
      utf8.encode(jsonEncode({'data': d})),
      status,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );
Map<String, dynamic> introIntent({
  String audience = 'PUBLIC',
  String status = 'ACTIVE',
  String? expiry,
}) => {
  'id': introSource,
  'creatorAccountId': introOwner,
  'type': 'FIND_COMPANION',
  'title': '周末羽毛球',
  'audience': audience,
  'status': status,
  'expiresAt': expiry ?? introStamp(),
};
Map<String, dynamic> introPolicy({
  int revision = 1,
  bool enabled = true,
  bool configured = true,
  Map<String, String>? values,
  String? expiry,
}) {
  final from = DateTime.now()
      .toUtc()
      .subtract(const Duration(minutes: 1))
      .toIso8601String();
  final rules = {
    for (final k in introductionCategories)
      k:
          values?[k] ??
          (k == 'UNKNOWN_PERSON' && enabled && configured
              ? 'REVIEW_REQUIRED'
              : 'DISABLED'),
  };
  return {
    'schemaVersion': 'agent-policy-settings-v1',
    'ownerType': 'PERSON',
    'ownerId': introOwner,
    'agentId': introAgent,
    'observedAt': introStamp(Duration.zero),
    'social': {
      'family': 'SOCIAL',
      'configured': configured,
      'nativeRevision': configured ? revision : 0,
      'status': configured ? 'ACTIVE' : 'UNCONFIGURED',
      'settings': {
        'rules': [
          for (final k in introductionCategories)
            {'category': k, 'preference': rules[k]},
        ],
      },
      if (configured) ...{
        'validFrom': from,
        'updatedAt': from,
        'expiresAt': expiry ?? introStamp(),
      },
    },
  };
}

Map<String, dynamic> introResult({String? end, bool empty = false}) {
  final expiry = end ?? introStamp();
  return {
    'schemaVersion': 'human-introduction-suggestions-v1',
    'mode': 'HUMAN_REVIEW_ONLY',
    'sourceIntentId': introSource,
    'sourceStatus': {
      'SHARED_INTEREST': 'PUBLIC_DECLARATIONS_ONLY',
      'SHARED_CITY': 'PUBLIC_DECLARATIONS_ONLY',
      'SHARED_COMMUNITY': 'PUBLIC_DECLARATIONS_ONLY',
      'SHARED_ACTIVITY': 'UNAVAILABLE',
    },
    'candidates': empty
        ? []
        : [
            {
              'sourceIntentId': introSource,
              'candidateIntentId': introPeerIntent,
              'accountId': introPeer,
              'displayName': '公开搭子',
              'basis': [
                {
                  'kind': 'SHARED_INTEREST',
                  'explanation': '双方当前公开意图都明确填写了羽毛球。',
                },
              ],
              'sourceBinding': List.filled(64, 'a').join(),
              'expiresAt': expiry,
            },
          ],
    'truncated': false,
    'observedAt': introStamp(Duration.zero),
    'expiresAt': expiry,
    'explanation': '仅供本人查看当前公开声明；不会发出引荐。',
    'modelAccess': false,
    'sendAllowed': false,
    'memoryPromotionAllowed': false,
  };
}

dynamic introClone(dynamic v) => jsonDecode(jsonEncode(v));
void main() {
  test(
    'only explicit PUBLIC_REGISTRATIONS_ONLY allows fourth shared-registration basis',
    () {
      final m = introResult();
      m['sourceStatus']['SHARED_ACTIVITY'] = 'PUBLIC_REGISTRATIONS_ONLY';
      m['candidates'][0]['basis'].add({
        'kind': 'SHARED_ACTIVITY',
        'explanation': '双方明确公开当前报名；不代表到场。',
      });
      final v = IntroductionResult(m, introSource, introOwner);
      expect(v.activityAvailable, true);
      expect(v.candidates.single.basis['SHARED_ACTIVITY'], contains('不代表到场'));
      m['sourceStatus']['SHARED_ACTIVITY'] = 'UNAVAILABLE';
      expect(
        () => IntroductionResult(m, introSource, introOwner),
        throwsFormatException,
      );
      m['sourceStatus']['SHARED_ACTIVITY'] = 'RSVP_ONLY';
      expect(
        () => IntroductionResult(m, introSource, introOwner),
        throwsFormatException,
      );
      expect(
        IntroductionResult(
          introResult(),
          introSource,
          introOwner,
        ).activityAvailable,
        false,
      );
    },
  );

  test(
    'actual native registered HTTP wire parses without synthetic DTO substitution',
    () async {
      final root = Directory('../../work/v5-age043-ui/native-wire2');
      Map<String, dynamic> wire(String name) =>
          jsonDecode(File('${root.path}/$name.json').readAsStringSync())
              as Map<String, dynamic>;
      final receipt = wire('wire-receipts');
      final owner = receipt['ownerID'] as String,
          source = receipt['sourceID'] as String;
      final rawSocial =
          wire('unconfigured-policy')['data']['social'] as Map<String, dynamic>;
      expect(
        rawSocial.keys.toSet().intersection({
          'validFrom',
          'updatedAt',
          'expiresAt',
        }),
        isEmpty,
      );
      final unconfigured = IntroductionPolicy(
        wire('unconfigured-policy')['data'] as Map<String, dynamic>,
        owner,
      );
      expect(unconfigured.configured, false);
      expect(unconfigured.expiry, isNull);
      final draft = IntroductionIntent(
        wire('draft-0')['data'] as Map<String, dynamic>,
        owner,
      );
      expect(draft.eligible, false);
      final api = AgentIntroductionAPI(
        apiBaseUrl: 'http://captured-native-wire',
        client: MockClient((r) async {
          final name = switch (r.url.path) {
            '/v1/me/new-people/consent' => 'consent-0',
            '/v1/me/new-people/intents' => 'intents',
            '/v1/me/agent-policies' => 'current-policy',
            '/v1/me/agent-introductions' => 'suggestions',
            _ => throw StateError('unexpected request'),
          };
          expect(r.method, 'GET');
          return http.Response.bytes(
            File('${root.path}/$name.json').readAsBytesSync(),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        }),
      );
      expect(await api.consent('Bearer captured-in-test'), true);
      final intents = await api.intents('Bearer captured-in-test', owner);
      expect(intents.single.id, source);
      expect(intents.single.eligible, true);
      // Native pool emits this exact legitimate +08:00 timestamp; do not rewrite to Z.
      expect(
        wire('intents')['data'][0]['expiresAt'] as String,
        endsWith('+08:00'),
      );
      final policy = await api.policy('Bearer captured-in-test', owner);
      expect(policy.status, 'ACTIVE');
      expect(policy.rules.length, 7);
      expect(policy.revision, 1);
      final result = await api.suggestions(
        'Bearer captured-in-test',
        owner,
        source,
      );
      expect(result.candidates.length, 1);
      expect(result.candidates.single.accountID, receipt['peerID']);
      expect(result.candidates.single.intentID, receipt['peerIntentID']);
      expect(receipt['modelCalled'], false);
      expect(receipt['production'], false);
      api.dispose();
    },
  );

  test(
    'native lifecycle and audience closed sets load safely without becoming PUBLIC active sources',
    () {
      for (final status in [
        'DRAFT',
        'MATCHED',
        'CONVERTED',
        'EXPIRED',
        'CANCELLED',
      ]) {
        expect(
          IntroductionIntent(introIntent(status: status), introOwner).eligible,
          false,
        );
      }
      for (final audience in [
        'PRIVATE',
        'FRIENDS',
        'COMMUNITY',
        'LOCAL',
        'INVITE_ONLY',
      ]) {
        expect(
          IntroductionIntent(
            introIntent(audience: audience),
            introOwner,
          ).eligible,
          false,
        );
      }
    },
  );
  test(
    'strict current human introduction DTO retains real IDs and closes all effects',
    () {
      final r = IntroductionResult(introResult(), introSource, introOwner);
      expect(r.candidates.single.accountID, introPeer);
      expect(
        () => r.candidates.add(r.candidates.single),
        throwsUnsupportedError,
      );
      expect(
        () => r.candidates.single.basis['SHARED_ACTIVITY'] = 'invented',
        throwsUnsupportedError,
      );
      for (final field in [
        'modelAccess',
        'sendAllowed',
        'memoryPromotionAllowed',
      ]) {
        final raw = introClone(introResult()) as Map<String, dynamic>;
        raw[field] = true;
        expect(
          () => IntroductionResult(raw, introSource, introOwner),
          throwsFormatException,
        );
      }
    },
  );
  test(
    'candidate refuses private rules, wrong source, forged activity, owner, duplicate and invalid digest',
    () {
      for (final mutate in <void Function(Map<String, dynamic>)>[
        (r) => r['rules'] = {},
        (r) => (r['candidates'][0])['sourceIntentId'] = introPeerIntent,
        (r) => (r['candidates'][0])['accountId'] = introOwner,
        (r) => (r['candidates'][0])['sourceBinding'] = 'confirmed',
        (r) => (r['candidates'][0])['basis'][0]['kind'] = 'SHARED_ACTIVITY',
        (r) => r['candidates'].add(introClone(r['candidates'][0])),
        (r) => r['sourceStatus']['SHARED_ACTIVITY'] = 'AVAILABLE',
        (r) => r['expiresAt'] = '2026-02-30T00:00:00+08:00',
      ]) {
        final r = introClone(introResult()) as Map<String, dynamic>;
        mutate(r);
        expect(
          () => IntroductionResult(r, introSource, introOwner),
          throwsFormatException,
        );
      }
    },
  );
  test(
    'strict social policy preserves seven categories and real offset timestamps',
    () {
      final raw = introPolicy();
      final from = '2026-10-04T07:00:00+08:00';
      raw['observedAt'] = '2026-10-04T07:01:00+08:00';
      raw['social']['validFrom'] = from;
      raw['social']['updatedAt'] = from;
      raw['social']['expiresAt'] = '2026-10-04T08:00:00+08:00';
      final p = IntroductionPolicy(raw, introOwner);
      expect(p.rules.length, 7);
      expect(p.expiry, DateTime.utc(2026, 10, 4));
      expect(
        egressTime('2026-10-04T00:00:00-02:30'),
        DateTime.utc(2026, 10, 4, 2, 30),
      );
      for (final mutate in <void Function(Map<String, dynamic>)>[
        (r) => r['ownerId'] = introPeer,
        (r) => r['social']['nativeRevision'] = 1.5,
        (r) => r['social']['settings']['rules'].removeLast(),
        (r) => r['social']['settings']['rules'][0]['preference'] = 'ALLOW',
        (r) => r['social']['expiresAt'] = '2026-10-04T08:00:00+08:60',
      ]) {
        final r = introClone(raw) as Map<String, dynamic>;
        mutate(r);
        expect(() => IntroductionPolicy(r, introOwner), throwsFormatException);
      }
      final unconfigured = IntroductionPolicy(
        introPolicy(configured: false),
        introOwner,
      );
      expect(unconfigured.rules.values.every((v) => v == 'DISABLED'), true);
      expect(unconfigured.revision, 0);
    },
  );
  test(
    'owner intent rejects cross owner while PUBLIC eligibility remains exact',
    () {
      expect(
        IntroductionIntent(introIntent(), introOwner).current(DateTime.now()),
        true,
      );
      for (final a in [
        'PRIVATE',
        'FRIENDS',
        'COMMUNITY',
        'LOCAL',
        'INVITE_ONLY',
      ]) {
        expect(
          IntroductionIntent(introIntent(audience: a), introOwner).eligible,
          false,
        );
      }
      expect(
        IntroductionIntent(introIntent(status: 'DRAFT'), introOwner).eligible,
        false,
      );
      expect(
        () => IntroductionIntent(introIntent(), introPeer),
        throwsFormatException,
      );
    },
  );
  test(
    'GET suggestions sends only source selector and original social CAS preserves other rules',
    () async {
      final calls = <http.Request>[];
      final original = IntroductionPolicy(
        introPolicy(values: {'BUSINESS': 'REVIEW_REQUIRED'}),
        introOwner,
      );
      final rules = Map<String, String>.from(original.rules)
        ..['UNKNOWN_PERSON'] = 'REVIEW_REQUIRED'
        ..['SHARED_ACTIVITY'] = 'REVIEW_REQUIRED';
      final expiry = DateTime.now().toUtc().add(const Duration(days: 7));
      final api = AgentIntroductionAPI(
        apiBaseUrl: 'http://fixture',
        client: MockClient((r) async {
          calls.add(r);
          if (r.method == 'GET') return introResponse(introResult());
          return introResponse(
            introPolicy(
              revision: 2,
              values: rules,
              expiry: expiry.toIso8601String(),
            ),
          );
        }),
      );
      await api.suggestions('Bearer own', introOwner, introSource);
      await api.saveSocial('Bearer own', original, rules, expiry);
      expect(calls[0].url.queryParameters, {'sourceIntentId': introSource});
      expect(calls[0].body, '');
      expect(
        calls.every(
          (r) => !r.headers.containsKey('X-Birdtie-Organization-Workspace'),
        ),
        true,
      );
      final body = jsonDecode(calls[1].body) as Map<String, dynamic>;
      expect(body.keys.toSet(), {'expectedVersion', 'settings', 'expiresAt'});
      expect(body['expectedVersion'], 1);
      final sent = body['settings']['rules'] as List;
      expect(sent.length, 7);
      expect(
        sent.singleWhere((r) => r['category'] == 'BUSINESS')['preference'],
        'REVIEW_REQUIRED',
      );
      final forbidden = Map<String, String>.from(rules)
        ..['BUSINESS'] = 'DISABLED';
      await expectLater(
        api.saveSocial('Bearer own', original, forbidden, expiry),
        throwsFormatException,
      );
      expect(calls.length, 2);
      api.dispose();
    },
  );
}
