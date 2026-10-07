import 'dart:convert';

import 'package:birdtie_client/src/workspace/support_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  testWidgets('activity report submits and shows its real receipt', (
    tester,
  ) async {
    var submitted = false;
    final client = MockClient((request) async {
      if (request.method == 'POST') {
        final body = jsonDecode(request.body) as Map<String, dynamic>;
        expect(body['targetType'], 'activity');
        expect(body['targetId'], 'activity-123');
        expect(body['reason'], 'safety');
        expect(body['details'], '活动现场存在安全问题，需要核查。');
        submitted = true;
        return http.Response(
          jsonEncode({
            'data': {
              'id': 'report-123',
              'targetType': 'activity',
              'reason': 'safety',
              'status': 'open',
            },
          }),
          201,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }
      return http.Response(
        jsonEncode({
          'data': submitted
              ? [
                  {'id': 'report-123', 'reason': 'safety', 'status': 'open'},
                ]
              : [],
        }),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    });
    await tester.pumpWidget(
      MaterialApp(
        home: SupportPage(
          authorizationHeader: () => 'Bearer reporter',
          targetType: 'activity',
          targetID: 'activity-123',
          apiBaseUrl: 'http://api.test',
          client: client,
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), '活动现场存在安全问题，需要核查。');
    await tester.ensureVisible(find.text('提交问题'));
    await tester.tap(find.text('提交问题'));
    await tester.pumpAndSettle();
    expect(submitted, isTrue);
    expect(find.textContaining('已提交，编号：report-123'), findsOneWidget);
    expect(find.textContaining('待处理'), findsOneWidget);
  });

  testWidgets('账号举报保留账号目标与中文说明', (tester) async {
    const accountId = '22222222-2222-4222-8222-222222222222';
    final client = MockClient((request) async {
      if (request.method == 'GET') {
        return http.Response(jsonEncode({'data': []}), 200);
      }
      final body = jsonDecode(request.body) as Map<String, dynamic>;
      expect(body['targetType'], 'account');
      expect(body['targetId'], accountId);
      return http.Response(
        jsonEncode({
          'data': {'id': 'receipt-1'},
        }),
        201,
      );
    });
    await tester.pumpWidget(
      MaterialApp(
        home: SupportPage(
          authorizationHeader: () => 'Bearer reporter',
          targetType: 'account',
          targetID: accountId,
          apiBaseUrl: 'http://api.test',
          client: client,
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('举报此账号'), findsOneWidget);
    expect(find.text('举报此组织'), findsNothing);
    await tester.enterText(find.byType(TextField), '此账号持续发送骚扰消息，请协助核查。');
    await tester.ensureVisible(find.text('提交问题'));
    await tester.tap(find.text('提交问题'));
    await tester.pumpAndSettle();
    expect(find.textContaining('receipt-1'), findsOneWidget);
  });

  for (final (kind, label) in [
    ('message', '举报此消息'),
    ('community', '举报此社群'),
    ('business', '举报此商家'),
  ]) {
    testWidgets('$kind 举报提交对应对象并返回收据', (tester) async {
      const targetId = '22222222-2222-4222-8222-222222222222';
      var posted = false;
      final client = MockClient((request) async {
        if (request.method == 'GET') {
          return http.Response(jsonEncode({'data': []}), 200);
        }
        final body = jsonDecode(request.body) as Map<String, dynamic>;
        expect(body['targetType'], kind);
        expect(body['targetId'], targetId);
        expect(body['details'], '该内容持续造成骚扰，请协助核查。');
        posted = true;
        return http.Response(
          jsonEncode({
            'data': {'id': 'receipt-$kind'},
          }),
          201,
        );
      });
      await tester.pumpWidget(
        MaterialApp(
          home: SupportPage(
            authorizationHeader: () => 'Bearer reporter',
            targetType: kind,
            targetID: targetId,
            apiBaseUrl: 'https://api.test',
            client: client,
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text(label), findsOneWidget);
      await tester.enterText(find.byType(TextField), '该内容持续造成骚扰，请协助核查。');
      await tester.ensureVisible(find.text('提交问题'));
      await tester.tap(find.text('提交问题'));
      await tester.pumpAndSettle();
      expect(posted, isTrue);
      expect(find.textContaining('receipt-$kind'), findsOneWidget);
      client.close();
    });
  }
}
