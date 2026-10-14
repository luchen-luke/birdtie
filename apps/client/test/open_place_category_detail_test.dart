import 'dart:convert';

import 'package:birdtie_client/src/workspace/place_detail_sheet.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  for (final entry in {
    'restaurant': '餐厅',
    'cafe': '咖啡馆',
    'museum': '博物馆',
    'gallery': '美术馆',
    'park': '公园',
    'library': '图书馆',
    'unknown-category': '其他',
  }.entries) {
    testWidgets('开放地点详情沿用原类别 ${entry.key}', (tester) async {
      final calls = <http.Request>[];
      final client = MockClient((request) async {
        calls.add(request);
        final Object? data;
        if (request.url.path.endsWith('/venue')) {
          return http.Response('{}', 404);
        } else if (request.url.path.endsWith('/activities')) {
          data = [];
        } else if (request.url.path.endsWith('/social-history')) {
          return http.Response('{}', 404);
        } else {
          data = {
            'id': '22222222-2222-4222-8222-222222222222',
            'name': '类别组件夹具',
            'categoryCode': entry.key,
            'source': {'label': '合成单元资料，不是真实地点'},
            'location': {'precision': 'none'},
          };
        }
        return http.Response.bytes(
          utf8.encode(jsonEncode({'data': data})),
          200,
        );
      });
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: PlaceDetailSheet(
              placeID: '22222222-2222-4222-8222-222222222222',
              authorizationHeader: () => null,
              onOpenActivity: (_) {},
              apiBaseUrl: 'https://component-fixture.test',
              client: client,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('地点类别：${entry.value}'), findsOneWidget);
      expect(calls.every((request) => request.method == 'GET'), isTrue);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
      client.close();
    });
  }
}
