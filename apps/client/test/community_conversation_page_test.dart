import 'dart:convert';

import 'package:birdtie_client/src/workspace/community_conversation_page.dart';
import 'package:birdtie_client/src/workspace/support_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  testWidgets('社群交流使用独立路径和成员提示，加入与退出不改变社群关系', (tester) async {
    var joined = false;
    var sent = false;
    final paths = <String>[];
    final client = MockClient((request) async {
      paths.add(request.url.path);
      expect(
        request.url.path.startsWith('/v1/communities/community/conversation'),
        isTrue,
      );
      if (request.method == 'POST') {
        if (request.url.path.endsWith('/messages')) {
          expect(
            (jsonDecode(request.body) as Map<String, dynamic>)['body'],
            '社群近况测试',
          );
          sent = true;
          return http.Response('{"data":{}}', 201);
        }
        expect(jsonDecode(request.body), {'confirmed': true});
        joined = true;
      }
      if (request.method == 'DELETE') {
        joined = false;
        return http.Response('', 204);
      }
      final data = request.url.path.endsWith('/messages')
          ? {
              'messages': sent
                  ? [
                      {
                        'id': 'message',
                        'senderAccountId': 'me',
                        'senderName': '我',
                        'body': '社群近况测试',
                        'removed': false,
                      },
                    ]
                  : [],
              'hasMore': false,
            }
          : {
              'communityId': 'community',
              'viewerAccountId': 'me',
              'joined': joined,
              'canJoin': true,
              'canSend': joined,
              'moderator': false,
            };
      return http.Response.bytes(utf8.encode(jsonEncode({'data': data})), 200);
    });
    await tester.pumpWidget(
      MaterialApp(
        home: CommunityConversationPage(
          communityID: 'community',
          communityTitle: '合成社群',
          authorizationHeader: () => 'Bearer test',
          apiBaseUrl: 'https://api.test',
          client: client,
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('社群交流'), findsOneWidget);
    expect(find.textContaining('有效成员看见'), findsOneWidget);
    expect(find.textContaining('报名'), findsNothing);
    expect(joined, isFalse);
    await tester.tap(find.text('同意并加入交流'));
    await tester.pumpAndSettle();
    expect(find.text('还没有消息，聊聊社群近况吧。'), findsOneWidget);
    expect(
      tester.widget<TextField>(find.byType(TextField)).decoration!.hintText,
      '聊聊社群近况…',
    );
    await tester.enterText(find.byType(TextField), '社群近况测试');
    await tester.tap(find.text('发送'));
    await tester.pumpAndSettle();
    expect(find.text('社群近况测试'), findsOneWidget);
    await tester.tap(find.text('退出交流'));
    await tester.pumpAndSettle();
    expect(find.textContaining('社群成员关系不会改变'), findsOneWidget);
    await tester.tap(find.text('确认'));
    await tester.pumpAndSettle();
    expect(find.text('社群近况测试'), findsNothing);
    expect(find.text('同意并加入交流'), findsOneWidget);
    expect(
      paths.every((p) => !p.endsWith('/leave') && !p.endsWith('/join')),
      isTrue,
    );
    await tester.pumpWidget(const SizedBox());
    client.close();
  });

  testWidgets('待审批权限错误隐藏消息；举报选择社群消息目标', (tester) async {
    var inaccessible = false;
    final client = MockClient((request) async {
      if (inaccessible) return http.Response('{}', 404);
      final data = request.url.path.endsWith('/messages')
          ? {
              'messages': [
                {
                  'id': 'message',
                  'senderAccountId': 'other',
                  'senderName': '合成成员',
                  'body': '可读消息',
                  'removed': false,
                },
              ],
              'hasMore': false,
            }
          : {
              'viewerAccountId': 'me',
              'joined': true,
              'canJoin': true,
              'canSend': true,
              'moderator': false,
            };
      return http.Response.bytes(utf8.encode(jsonEncode({'data': data})), 200);
    });
    Widget page() => MaterialApp(
      home: CommunityConversationPage(
        communityID: 'community',
        communityTitle: '合成社群',
        authorizationHeader: () => 'Bearer test',
        apiBaseUrl: 'https://api.test',
        client: client,
      ),
    );
    await tester.pumpWidget(page());
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('消息操作'));
    await tester.pumpAndSettle();
    expect(find.text('移除消息'), findsNothing);
    await tester.tap(find.text('举报消息'));
    await tester.pumpAndSettle();
    final support = tester.widget<SupportPage>(find.byType(SupportPage));
    expect(support.targetType, 'community_message');
    expect(support.targetID, 'message');
    await tester.pageBack();
    await tester.pumpAndSettle();
    inaccessible = true;
    await tester.tap(find.byTooltip('刷新消息'));
    await tester.pumpAndSettle();
    expect(find.text('可读消息'), findsNothing);
    expect(find.textContaining('待审批、受邀或已退出'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    client.close();
  });
}
