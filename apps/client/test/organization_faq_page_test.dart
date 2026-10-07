import 'dart:convert';

import 'package:birdtie_client/src/workspace/organization_faq_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  testWidgets('admin can save a real FAQ draft from the console page', (
    tester,
  ) async {
    final items = <Map<String, dynamic>>[];
    final client = MockClient((request) async {
      if (request.method == 'GET') {
        return http.Response.bytes(
          utf8.encode(jsonEncode({'data': items})),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }
      if (request.method == 'POST') {
        final body = jsonDecode(request.body) as Map<String, dynamic>;
        expect(body['published'], false);
        items.add({
          'id': 'faq-1',
          'question': body['question'],
          'answer': body['answer'],
          'published': body['published'],
        });
        return http.Response.bytes(
          utf8.encode(jsonEncode({'data': items.single})),
          201,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }
      return http.Response('{}', 404);
    });
    await tester.pumpWidget(
      MaterialApp(
        home: OrganizationFAQPage(
          organizationID: 'org-1',
          authorizationHeader: () => 'Bearer test',
          apiBaseUrl: 'http://127.0.0.1:3694',
          client: client,
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.textContaining('还没有常见问答'), findsOneWidget);
    await tester.tap(find.text('新增问答'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField).at(0), '如何报名？');
    await tester.enterText(find.byType(TextField).at(1), '请在活动详情报名。');
    await tester.tap(find.text('保存'));
    await tester.pumpAndSettle();
    expect(find.text('如何报名？'), findsOneWidget);
    expect(find.text('请在活动详情报名。'), findsOneWidget);
    expect(find.text('草稿 · 仅管理员可见'), findsOneWidget);
  });
}
