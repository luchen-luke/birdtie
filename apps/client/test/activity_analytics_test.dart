import 'dart:convert';

import 'package:birdtie_client/src/workspace/activity_analytics.dart';
import 'package:birdtie_client/src/workspace/organization_analytics_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  test('view event sends only activity and a fixed source', () async {
    final client = MockClient((request) async {
      expect(request.url.path, '/v1/activities/activity-123/analytics/events');
      expect(jsonDecode(request.body), {
        'eventType': 'impression',
        'entrySource': 'now',
      });
      expect(request.headers.containsKey('Authorization'), isFalse);
      return http.Response('', 204);
    });
    expect(
      await recordActivityView(
        'activity-123',
        'impression',
        'now',
        apiBaseUrl: 'http://api.test',
        client: client,
      ),
      isTrue,
    );
  });

  testWidgets(
    'organization sees aggregate metrics with Chinese source labels',
    (tester) async {
      final client = MockClient((request) async {
        expect(
          request.headers['authorization'] ?? request.headers['Authorization'],
          'Bearer admin',
        );
        return http.Response(
          jsonEncode({
            'data': [
              {
                'activityId': 'one',
                'title': '周末羽毛球',
                'impressions': 4,
                'detailOpens': 2,
                'rsvps': 1,
                'saves': 1,
                'sources': [
                  {
                    'source': 'now',
                    'impressions': 4,
                    'detailOpens': 2,
                    'rsvps': 1,
                    'saves': 1,
                  },
                ],
              },
            ],
          }),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      });
      await tester.pumpWidget(
        MaterialApp(
          home: OrganizationAnalyticsPage(
            organizationID: 'org',
            authorizationHeader: () => 'Bearer admin',
            apiBaseUrl: 'http://api.test',
            client: client,
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('活动数据'), findsOneWidget);
      await tester.drag(find.byType(ListView), const Offset(0, -350));
      await tester.pumpAndSettle();
      expect(find.text('周末羽毛球'), findsOneWidget);
      expect(find.text('展示 4 · 详情 2 · 报名 1 · 收藏 1'), findsOneWidget);
      expect(find.textContaining('附近：展示 4'), findsOneWidget);
    },
  );
}
