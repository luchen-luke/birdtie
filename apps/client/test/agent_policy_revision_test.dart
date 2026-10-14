import 'dart:convert';

import 'package:birdtie_client/src/workspace/agent_introduction_api.dart';
import 'package:birdtie_client/src/workspace/agent_policy_revision.dart';
import 'package:birdtie_client/src/workspace/agent_profile_api.dart';
import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter_test/flutter_test.dart';

const owner = '11111111-1111-4111-8111-111111111111';
const otherOwner = '22222222-2222-4222-8222-222222222222';
const agent = '33333333-3333-4333-8333-333333333333';

// Exercise the production policy envelope and both real parsers; no test DTO.
Map<String, dynamic> policy({Object? revision = 1, bool configured = true}) {
  Map<String, dynamic> record(String family, Map<String, dynamic> settings) => {
    'family': family,
    'configured': configured,
    'nativeRevision': revision,
    'status': configured ? 'ACTIVE' : 'UNCONFIGURED',
    'settings': settings,
    if (configured) ...{
      'validFrom': '2026-10-10T10:00:00Z',
      'updatedAt': '2026-10-10T10:00:00Z',
      'expiresAt': '2026-10-11T10:00:00Z',
    },
  };
  return {
    'schemaVersion': 'agent-policy-settings-v1',
    'ownerType': 'PERSON',
    'ownerId': owner,
    'agentId': agent,
    'observedAt': '2026-10-10T10:01:00Z',
    'attention': record('ATTENTION', {'defaultRoute': 'BLOCK', 'rules': []}),
    'social': record('SOCIAL', {
      'rules': [
        for (final category in introductionCategories)
          {'category': category, 'preference': 'DISABLED'},
      ],
    }),
    'autonomy': record('AUTONOMY', {'level': 'LEVEL_0_OBSERVE'}),
  };
}

void expectBothReject(Map<String, dynamic> raw, {required bool isWeb}) {
  expect(
    () => IntroductionPolicy(raw, owner, isWeb: isWeb),
    throwsFormatException,
  );
  expect(
    () => AgentPoliciesView(raw, owner, isWeb: isWeb),
    throwsFormatException,
  );
}

void main() {
  test('production defaults follow the compiled platform', () {
    final revision = int.parse('9007199254740991');
    expect(agentPolicyRevisionValid(revision), !kIsWeb);
    if (kIsWeb) {
      expect(
        () => IntroductionPolicy(policy(revision: revision), owner),
        throwsFormatException,
      );
      expect(
        () => AgentPoliciesView(policy(revision: revision), owner),
        throwsFormatException,
      );
    } else {
      expect(
        IntroductionPolicy(policy(revision: revision), owner).revision,
        revision,
      );
      expect(
        AgentPoliciesView(policy(revision: revision), owner).records.length,
        3,
      );
    }
  });

  test('Web current revision and reserved next revision are exact', () {
    final maximum = int.parse('9007199254740990');
    final next = maximum + 1;
    expect(next.toString(), '9007199254740991');
    expect(next - 1, maximum);
    final raw = policy(revision: maximum);
    expect(IntroductionPolicy(raw, owner, isWeb: true).revision, maximum);
    expect(AgentPoliciesView(raw, owner, isWeb: true).records.length, 3);
    // The last safe integer has no safe-integer increment reserve.
    expectBothReject(policy(revision: next), isWeb: true);
  });

  test(
    'Web rejects unsafe JSON integers even after JS decoding rounds them',
    () {
      for (final decimal in [
        '9007199254740992',
        '9007199254740993',
        '9223372036854775806',
        '9223372036854775807',
      ]) {
        final raw = policy();
        raw['social']['nativeRevision'] = jsonDecode(decimal);
        expectBothReject(raw, isWeb: true);
      }
    },
  );

  if (!kIsWeb) {
    test('native keeps int64 maximum-minus-one with exact +1 arithmetic', () {
      final maximum = int.parse('9223372036854775806');
      final next = maximum + 1;
      expect(next.toString(), '9223372036854775807');
      expect(next - 1, maximum);
      final raw = policy(revision: maximum);
      expect(IntroductionPolicy(raw, owner, isWeb: false).revision, maximum);
      expect(AgentPoliciesView(raw, owner, isWeb: false).records.length, 3);
      expectBothReject(policy(revision: next), isWeb: false);
      // A value above the Web bound remains legitimate on native.
      expect(
        IntroductionPolicy(
          policy(revision: int.parse('9007199254740991')),
          owner,
          isWeb: false,
        ).revision,
        int.parse('9007199254740991'),
      );
    });
  }

  for (final isWeb in [true, if (!kIsWeb) false]) {
    test('type and nonnegative guards remain strict (Web=$isWeb)', () {
      for (final invalid in <Object?>[
        null,
        true,
        false,
        '1',
        1.5,
        -1,
        double.infinity,
        double.nan,
        if (!kIsWeb) 1.0,
      ]) {
        expectBothReject(policy(revision: invalid), isWeb: isWeb);
      }
    });

    test('zero is only a genuinely unconfigured policy (Web=$isWeb)', () {
      final raw = policy(revision: 0, configured: false);
      expect(IntroductionPolicy(raw, owner, isWeb: isWeb).configured, false);
      expect(AgentPoliciesView(raw, owner, isWeb: isWeb).records.length, 3);
      expectBothReject(policy(revision: 0), isWeb: isWeb);
      expectBothReject(policy(revision: 1, configured: false), isWeb: isWeb);
    });

    test(
      'owner, family, configured and rule guards stay intact (Web=$isWeb)',
      () {
        for (final mutate in <void Function(Map<String, dynamic>)>[
          (raw) => raw['ownerId'] = otherOwner,
          (raw) => raw['ownerType'] = 'ORGANIZATION',
          (raw) => raw['agentId'] = 'invalid-agent',
          (raw) => raw['schemaVersion'] = 'wrong-version',
          (raw) => raw['social']['family'] = 'ATTENTION',
          (raw) => raw['social']['configured'] = 'true',
          (raw) =>
              raw['social']['settings']['rules'][0]['category'] = 'UNKNOWN',
          (raw) =>
              raw['social']['settings']['rules'][0]['preference'] = 'AUTO_SEND',
          (raw) => raw['social']['updatedAt'] = '2026-10-10T10:00:01Z',
          (raw) => raw['social']['status'] = 'UNCONFIGURED',
          (raw) => raw['social']['isWeb'] = false,
        ]) {
          final raw = policy();
          mutate(raw);
          expectBothReject(raw, isWeb: isWeb);
        }
      },
    );
  }

  test('profile applies Web bound to each family, not only nested social', () {
    final maximum = int.parse('9007199254740990');
    for (final family in ['attention', 'social', 'autonomy']) {
      final raw = policy();
      raw[family]['nativeRevision'] = maximum;
      expect(AgentPoliciesView(raw, owner, isWeb: true).records.length, 3);
      raw[family]['nativeRevision'] = maximum + 1;
      expect(
        () => AgentPoliciesView(raw, owner, isWeb: true),
        throwsFormatException,
      );
      expect(
        () => AgentPolicySummary(
          raw[family],
          family.toUpperCase(),
          DateTime.parse(raw['observedAt'] as String),
          isWeb: true,
        ),
        throwsFormatException,
      );
    }
  });
}
