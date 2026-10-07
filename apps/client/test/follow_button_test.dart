import 'dart:convert';

import 'package:birdtie_client/src/workspace/follow_button.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  testWidgets('关注与取消关注只写入明确的主体目标', (tester) async {
    const targetID = '11111111-1111-4111-8111-111111111111';
    var followed = false;
    final client = MockClient((request) async {
      expect(request.headers['Authorization'], 'Bearer person');
      if (request.method == 'GET') {
        return http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': followed
                  ? [
                      {
                        'target': {
                          'targetType': 'ORGANIZATION',
                          'targetId': targetID,
                        },
                      },
                    ]
                  : [],
            }),
          ),
          200,
        );
      }
      if (request.method == 'POST') {
        expect(jsonDecode(request.body), {
          'targetType': 'ORGANIZATION',
          'targetId': targetID,
        });
        followed = true;
        return http.Response('{"data":{"id":"follow-1"}}', 201);
      }
      expect(request.method, 'DELETE');
      expect(request.url.path, '/v1/me/follows/ORGANIZATION/$targetID');
      followed = false;
      return http.Response('', 204);
    });
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: FollowButton(
            targetType: 'ORGANIZATION',
            targetID: targetID,
            authorizationHeader: () => 'Bearer person',
            apiBaseUrl: 'https://api.test',
            client: client,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('关注'), findsOneWidget);
    await tester.tap(find.text('关注'));
    await tester.pumpAndSettle();
    expect(find.text('已关注'), findsOneWidget);
    await tester.tap(find.text('已关注'));
    await tester.pumpAndSettle();
    expect(find.text('关注'), findsOneWidget);
    expect(followed, isFalse);
    client.close();
  });

  testWidgets('未登录时不显示关注操作', (tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: FollowButton(
            targetType: 'PERSON',
            targetID: '11111111-1111-4111-8111-111111111111',
            authorizationHeader: _noSession,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('关注'), findsNothing);
  });

  testWidgets('切换账号时不会展示上一账号的关注状态', (tester) async {
    var token = 'Bearer first';
    final client = MockClient((request) async {
      final followed = request.headers['Authorization'] == 'Bearer first';
      return http.Response(
        jsonEncode({
          'data': followed
              ? [
                  {
                    'target': {
                      'targetType': 'PERSON',
                      'targetId': '11111111-1111-4111-8111-111111111111',
                    },
                  },
                ]
              : [],
        }),
        200,
      );
    });
    Widget screen() => MaterialApp(
      home: Scaffold(
        body: FollowButton(
          targetType: 'PERSON',
          targetID: '11111111-1111-4111-8111-111111111111',
          authorizationHeader: () => token,
          apiBaseUrl: 'https://api.test',
          client: client,
        ),
      ),
    );
    await tester.pumpWidget(screen());
    await tester.pumpAndSettle();
    expect(find.text('已关注'), findsOneWidget);
    token = 'Bearer second';
    await tester.pumpWidget(screen());
    await tester.pumpAndSettle();
    expect(find.text('已关注'), findsNothing);
    expect(find.text('关注'), findsOneWidget);
    client.close();
  });
}

String? _noSession() => null;
