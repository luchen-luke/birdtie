import 'dart:convert';

import 'package:birdtie_client/src/workspace/sponsored_opportunities.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

const sponsorActivityID = '22222222-2222-4222-8222-222222222222';
const sponsorPlaceID = '33333333-3333-4333-8333-333333333333';
const sponsorTitle = '合成公开羽毛球活动';

Map<String, dynamic> sponsorRow({String type = 'ACTIVITY'}) => {
  'id': '44444444-4444-4444-8444-444444444444',
  'revision': 2,
  'kind': 'SPONSORED',
  'label': '赞助',
  'sponsor': {
    'type': 'BUSINESS',
    'id': '55555555-5555-4555-8555-555555555555',
    'name': '本地合成验收商家',
  },
  'target': {
    'type': type,
    'id': type == 'ACTIVITY' ? sponsorActivityID : sponsorPlaceID,
    'title': type == 'ACTIVITY' ? sponsorTitle : '合成公开球馆',
  },
  'source': {
    'url': 'https://qa.example/sponsorship',
    'observedAt': '2026-10-03T01:00:00Z',
    'reviewedAt': '2026-10-03T01:05:00Z',
    'expiresAt': '2026-10-10T00:00:00Z',
  },
  'checkedAt': '2026-10-03T01:06:00Z',
};

Map<String, dynamic> sponsorEnvelope({String type = 'ACTIVITY'}) => {
  'commercialTrustVersion': sponsoredTrustVersion,
  'sponsoredStatus': 'available',
  'sponsoredOpportunities': [sponsorRow(type: type)],
};
const sponsorEligible = {
  'ACTIVITY:$sponsorActivityID': sponsorTitle,
  'PLACE:$sponsorPlaceID': '合成公开球馆',
};

void main() {
  test('legacy response has no invented sponsor or unavailable status', () {
    final result = decodeSponsoredDisclosure({'data': []}, eligibleTargets: {});
    expect(result.items, isEmpty);
    expect(result.unavailable, false);
  });
  test('unavailable disclosure is explicit and empty', () {
    final result = decodeSponsoredDisclosure({
      'commercialTrustVersion': sponsoredTrustVersion,
      'sponsoredStatus': 'unavailable',
      'sponsoredOpportunities': [],
    }, eligibleTargets: {});
    expect(result.unavailable, true);
    expect(result.items, isEmpty);
  });
  for (final type in ['ACTIVITY', 'PLACE']) {
    test('strict $type disclosure binds natural target and immutable list', () {
      final result = decodeSponsoredDisclosure(
        sponsorEnvelope(type: type),
        eligibleTargets: sponsorEligible,
      );
      expect(result.items.single.targetType, type);
      expect(() => result.items.clear(), throwsUnsupportedError);
      expect(
        jsonEncode(sponsorEnvelope(type: type)),
        isNot(contains('rightsNote')),
      );
    });
  }
  final invalids = <String, void Function(Map<String, dynamic>)>{
    'missing version': (e) => e.remove('commercialTrustVersion'),
    'unknown version': (e) => e['commercialTrustVersion'] = 'future',
    'unknown status': (e) => e['sponsoredStatus'] = 'published',
    'unavailable nonempty': (e) => e['sponsoredStatus'] = 'unavailable',
    'null items': (e) => e['sponsoredOpportunities'] = null,
    'seven items': (e) =>
        e['sponsoredOpportunities'] = List.generate(7, (_) => sponsorRow()),
    'duplicate statement': (e) =>
        (e['sponsoredOpportunities'] as List).add(sponsorRow()),
    'wrong label': (e) => e['sponsoredOpportunities'][0]['label'] = '推荐',
    'unknown kind': (e) => e['sponsoredOpportunities'][0]['kind'] = 'ORGANIC',
    'no revision': (e) => e['sponsoredOpportunities'][0]['revision'] = 0,
    'fractional revision': (e) =>
        e['sponsoredOpportunities'][0]['revision'] = 2.5,
    'too large revision': (e) =>
        e['sponsoredOpportunities'][0]['revision'] = 9007199254740992,
    'private reviewer': (e) =>
        e['sponsoredOpportunities'][0]['reviewer'] = 'private',
    'private rights': (e) =>
        e['sponsoredOpportunities'][0]['source']['rightsNote'] = 'private',
    'unknown target': (e) =>
        e['sponsoredOpportunities'][0]['target']['type'] = 'PERSON',
    'wrong target title': (e) =>
        e['sponsoredOpportunities'][0]['target']['title'] = '另一活动',
    'unrelated target': (e) => e['sponsoredOpportunities'][0]['target']['id'] =
        '66666666-6666-4666-8666-666666666666',
    'zero sponsor': (e) => e['sponsoredOpportunities'][0]['sponsor']['id'] =
        '00000000-0000-0000-0000-000000000000',
    'organization sponsor': (e) =>
        e['sponsoredOpportunities'][0]['sponsor']['type'] = 'ORGANIZATION',
    'control text': (e) =>
        e['sponsoredOpportunities'][0]['sponsor']['name'] = '秘密\n内容',
    'http source': (e) => e['sponsoredOpportunities'][0]['source']['url'] =
        'http://qa.example/sponsor',
    'credential source': (e) =>
        e['sponsoredOpportunities'][0]['source']['url'] =
            'https://a:b@qa.example/sponsor',
    'fragment source': (e) => e['sponsoredOpportunities'][0]['source']['url'] =
        'https://qa.example/sponsor#x',
    'future observed': (e) =>
        e['sponsoredOpportunities'][0]['source']['observedAt'] =
            '2026-10-04T01:00:00Z',
    'future review': (e) =>
        e['sponsoredOpportunities'][0]['source']['reviewedAt'] =
            '2026-10-04T01:00:00Z',
    'expired': (e) => e['sponsoredOpportunities'][0]['source']['expiresAt'] =
        '2026-10-03T01:06:00Z',
    'unbounded': (e) => e['sponsoredOpportunities'][0]['source']['expiresAt'] =
        '2027-01-03T01:06:00Z',
    'local timestamp': (e) =>
        e['sponsoredOpportunities'][0]['checkedAt'] = '2026-10-03T01:06:00',
  };
  for (final entry in invalids.entries) {
    test('reject ${entry.key} without treating it as organic', () {
      final input =
          jsonDecode(jsonEncode(sponsorEnvelope())) as Map<String, dynamic>;
      entry.value(input);
      expect(
        () =>
            decodeSponsoredDisclosure(input, eligibleTargets: sponsorEligible),
        throwsFormatException,
      );
    });
  }
  const timeFields = ['observedAt', 'reviewedAt', 'expiresAt', 'checkedAt'];
  const malformedTimes = [
    '2026-13-03T01:06:00Z',
    '2026-10-32T01:06:00Z',
    '2026-02-29T01:06:00Z',
    '1900-02-29T01:06:00Z',
    '2100-02-29T01:06:00Z',
    '2026-00-03T01:06:00Z',
    '2026-10-00T01:06:00Z',
    '2026-10-03T25:06:00Z',
    '2026-10-03T01:60:00Z',
    '2026-10-03T01:06:60Z',
    '2026-10-03T01:06:00+00:00',
    '2026-10-03 01:06:00Z',
    '20261003T010600Z',
    '2026-10-03T01:06Z',
    '2026-10-03T01:06:00,1Z',
    '2026-10-03T01:06:00.1234567890Z',
    '0000-10-03T01:06:00Z',
    '+010000-10-03T01:06:00Z',
    '2026-10-03T01:06:00Z\n',
  ];
  for (final field in timeFields) {
    for (final invalid in malformedTimes) {
      test('reject strict UTC $field value $invalid', () {
        final input = sponsorEnvelope();
        final row = input['sponsoredOpportunities'][0];
        if (field == 'checkedAt') {
          row[field] = invalid;
        } else {
          row['source'][field] = invalid;
        }
        expect(
          () => decodeSponsoredDisclosure(
            input,
            eligibleTargets: sponsorEligible,
          ),
          throwsFormatException,
        );
      });
    }
  }
  for (final year in [4, 2000, 2024]) {
    for (final fraction in [
      '',
      for (var length = 1; length <= 9; length++)
        '.${'123456789'.substring(0, length)}',
    ]) {
      test('valid leap year $year with UTC fraction $fraction', () {
        final paddedYear = year.toString().padLeft(4, '0');
        final input = sponsorEnvelope();
        final row = input['sponsoredOpportunities'][0];
        row['source']['observedAt'] = '$paddedYear-02-29T01:00:00${fraction}Z';
        row['source']['reviewedAt'] = '$paddedYear-02-29T01:05:00${fraction}Z';
        row['checkedAt'] = '$paddedYear-02-29T01:06:00${fraction}Z';
        row['source']['expiresAt'] = '$paddedYear-03-01T00:00:00${fraction}Z';
        final decoded = decodeSponsoredDisclosure(
          input,
          eligibleTargets: sponsorEligible,
        );
        expect(decoded.items.single.checkedAt.year, year);
        expect(decoded.items.single.checkedAt.month, 2);
        expect(decoded.items.single.checkedAt.day, 29);
        expect(decoded.items.single.checkedAt.isUtc, true);
        expect(
          decoded.items.single.checkedAt.microsecond,
          int.parse(
                fraction.replaceFirst('.', '').padRight(6, '0').substring(0, 6),
              ) %
              1000,
        );
      });
    }
  }
  test('duplicate target with different statement also rejected', () {
    final e = sponsorEnvelope();
    final row = sponsorRow()..['id'] = '77777777-7777-4777-8777-777777777777';
    (e['sponsoredOpportunities'] as List).add(row);
    expect(
      () => decodeSponsoredDisclosure(e, eligibleTargets: sponsorEligible),
      throwsFormatException,
    );
  });
  testWidgets(
    'Chinese disclosure wraps at 360 and 1.6 text, detail has same reference',
    (tester) async {
      tester.view.physicalSize = const Size(360, 800);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final data = decodeSponsoredDisclosure(
        sponsorEnvelope(),
        eligibleTargets: sponsorEligible,
      );
      SponsoredOpportunity? opened;
      await tester.pumpWidget(
        MaterialApp(
          home: MediaQuery(
            data: const MediaQueryData(textScaler: TextScaler.linear(1.6)),
            child: Scaffold(
              body: SingleChildScrollView(
                child: SponsoredOpportunitiesSection(
                  items: data.items,
                  onOpen: (v) => opened = v,
                ),
              ),
            ),
          ),
        ),
      );
      expect(find.text('赞助'), findsOneWidget);
      expect(find.textContaining('支持方：'), findsOneWidget);
      expect(find.text('https://qa.example/sponsorship'), findsOneWidget);
      await tester.ensureVisible(find.text('查看赞助活动详情'));
      await tester.tap(find.text('查看赞助活动详情'));
      expect(opened?.targetID, sponsorActivityID);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'unavailable natural fallback never offers a fake sponsor button',
    (tester) async {
      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(
            body: SponsoredOpportunitiesSection(items: [], unavailable: true),
          ),
        ),
      );
      expect(find.textContaining('自然匹配仍可查看'), findsOneWidget);
      expect(find.byType(OutlinedButton), findsNothing);
    },
  );
  testWidgets('missing current detail callback disables visible button', (
    tester,
  ) async {
    final data = decodeSponsoredDisclosure(
      sponsorEnvelope(),
      eligibleTargets: sponsorEligible,
    );
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(body: SponsoredOpportunitiesSection(items: data.items)),
      ),
    );
    expect(
      tester.widget<OutlinedButton>(find.byType(OutlinedButton)).onPressed,
      isNull,
    );
  });
}
