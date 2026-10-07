import 'dart:convert';

import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/public_organization_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  test(
    'public organization rejects private and foreign organizer activities',
    () {
      final data = <String, dynamic>{
        'id': 'org-1',
        'name': '合成组织',
        'verificationStatus': 'verified',
        'upcomingActivities': [
          {
            'id': 'private',
            'visibility': 'private',
            'organizer': {'type': 'ORGANIZATION', 'id': 'org-1'},
          },
        ],
      };
      expect(() => PublicOrganization.fromJson(data), throwsFormatException);
      data['upcomingActivities'] = [
        {
          'id': 'foreign',
          'visibility': 'public',
          'organizer': {'type': 'BUSINESS', 'id': 'org-1'},
        },
      ];
      expect(() => PublicOrganization.fromJson(data), throwsFormatException);
    },
  );
  testWidgets(
    'public profile shows truthful verification and real activities',
    (tester) async {
      final data = {
        'data': {
          'id': 'org-1',
          'name': '阿伯丁学生社团',
          'organizationType': 'student_society',
          'description': '每周组织运动。',
          'verificationStatus': 'unverified',
          'officialLinks': ['https://example.org/society'],
          'upcomingActivities': [
            {
              'id': 'activity-1',
              'visibility': 'public',
              'organizer': {'type': 'ORGANIZATION', 'id': 'org-1'},
              'organizationId': 'org-1',
              'hostLabel': '阿伯丁学生社团',
              'placeName': '体育馆',
              'title': '周末羽毛球',
              'startsAt': '2026-10-03T10:00:00Z',
              'endsAt': '2026-10-03T12:00:00Z',
              'timeZone': 'Europe/London',
              'schedule': '10月3日（周六）11:00',
              'status': 'upcoming',
              'source': {'label': 'Birdtie Organization'},
            },
          ],
        },
      };
      final client = MockClient(
        (request) async => http.Response.bytes(
          utf8.encode(jsonEncode(data)),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        ),
      );
      PublicActivity? opened;
      Uri? linked;
      await tester.pumpWidget(
        MaterialApp(
          home: PublicOrganizationPage(
            organizationID: 'org-1',
            authorizationHeader: () => null,
            onOpenActivity: (activity) => opened = activity,
            apiBaseUrl: 'http://127.0.0.1:3694',
            client: client,
            openExternal: (uri) async {
              linked = uri;
              return true;
            },
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('身份未认证'), findsNothing);
      expect(find.textContaining('身份未认证'), findsWidgets);
      expect(find.textContaining('尚未通过 Birdtie 核验'), findsOneWidget);
      expect(find.text('组织提供的链接'), findsOneWidget);
      expect(find.text('周末羽毛球'), findsOneWidget);
      expect(find.text('关注'), findsNothing);
      expect(find.text('向组织提问'), findsNothing);
      await tester.tap(find.text('周末羽毛球'));
      expect(opened?.id, 'activity-1');
      await tester.tap(find.text('https://example.org/society'));
      await tester.pumpAndSettle();
      expect(linked?.host, 'example.org');
    },
  );

  testWidgets('verified Organization Agent shows sourced answer', (
    tester,
  ) async {
    final client = MockClient((request) async {
      final data = request.method == 'GET'
          ? {
              'data': {
                'id': 'org-1',
                'name': '已认证学生社团',
                'organizationType': 'student_society',
                'verificationStatus': 'verified',
                'agentAvailable': true,
                'description': '活动组织。',
                'officialLinks': <String>[],
                'upcomingActivities': <Object>[],
              },
            }
          : {
              'data': {
                'organizationId': 'org-1',
                'status': 'known',
                'answer': '请在活动详情报名。',
                'sources': [
                  {'type': 'faq', 'id': 'faq-1', 'label': '如何报名？'},
                ],
                'mode': 'verified_rules',
              },
            };
      if (request.method == 'POST') {
        expect(jsonDecode(request.body), {'query': '如何报名？'});
      }
      return http.Response.bytes(
        utf8.encode(jsonEncode(data)),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    });
    await tester.pumpWidget(
      MaterialApp(
        home: PublicOrganizationPage(
          organizationID: 'org-1',
          authorizationHeader: () => null,
          onOpenActivity: (_) {},
          apiBaseUrl: 'http://127.0.0.1:3694',
          client: client,
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.textContaining('已认证组织'), findsOneWidget);
    await tester.enterText(find.byType(TextField), '如何报名？');
    await tester.ensureVisible(find.text('提问'));
    await tester.tap(find.text('提问'));
    await tester.pumpAndSettle();
    expect(find.text('请在活动详情报名。'), findsOneWidget);
    expect(find.text('依据：如何报名？'), findsOneWidget);
  });
}
