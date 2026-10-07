import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/activity_conversation_page.dart';
import 'package:birdtie_client/src/workspace/support_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

http.Response envelope(Object data, [int code = 200]) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': data})), code);
Map<String, dynamic> state({
  bool joined = true,
  bool canSend = true,
  bool moderator = false,
}) => {
  'id': 'chat',
  'activityId': 'activity',
  'viewerAccountId': 'me',
  'joined': joined,
  'canJoin': canSend,
  'canSend': joined && canSend,
  'moderator': moderator,
};
Map<String, dynamic> message({String sender = 'me', bool removed = false}) => {
  'id': 'message',
  'senderAccountId': sender,
  'senderName': '合成参与者',
  'body': removed ? '' : '十点体育馆见',
  'createdAt': '2026-10-02T00:00:00Z',
  'removed': removed,
};
Widget screen(http.Client client, String? Function() token) => MaterialApp(
  home: ActivityConversationPage(
    activityID: 'activity',
    activityTitle: '合成羽毛球活动',
    authorizationHeader: token,
    apiBaseUrl: 'https://api.test',
    client: client,
  ),
);

void main() {
  testWidgets('先提示可见范围，明确加入；发送失败保留草稿并用同一标识重试', (tester) async {
    var joined = false;
    var sends = 0;
    final attempts = <Map<String, dynamic>>[];
    final messages = <Map<String, dynamic>>[];
    final client = MockClient((request) async {
      if (request.url.path.endsWith('/messages')) {
        if (request.method == 'POST') {
          attempts.add(jsonDecode(request.body) as Map<String, dynamic>);
          sends++;
          if (sends == 1) return http.Response('{}', 500);
          messages.add(message());
          return envelope(messages.last, 201);
        }
        return envelope({'messages': messages, 'hasMore': false});
      }
      if (request.method == 'POST') {
        expect(jsonDecode(request.body), {'confirmed': true});
        joined = true;
      }
      return envelope(state(joined: joined));
    });
    await tester.pumpWidget(screen(client, () => 'Bearer test'));
    await tester.pumpAndSettle();
    expect(joined, isFalse);
    expect(find.textContaining('你的名称和消息'), findsOneWidget);
    expect(find.byType(TextField), findsNothing);
    await tester.tap(find.text('同意并加入交流'));
    await tester.pumpAndSettle();
    expect(joined, isTrue);
    await tester.enterText(find.byType(TextField), '十点体育馆见');
    await tester.tap(find.text('发送'));
    await tester.pumpAndSettle();
    expect(
      tester.widget<TextField>(find.byType(TextField)).controller!.text,
      '十点体育馆见',
    );
    expect(find.textContaining('未确认发送'), findsOneWidget);
    await tester.tap(find.text('发送'));
    await tester.pumpAndSettle();
    expect(attempts.length, 2);
    expect(attempts[0], attempts[1]);
    expect(
      attempts.first['clientMessageId'],
      matches(
        RegExp(
          r'^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$',
        ),
      ),
    );
    expect(
      tester.widget<TextField>(find.byType(TextField)).controller!.text,
      '',
    );
    expect(find.text('十点体育馆见'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    client.close();
  });

  testWidgets('取消或结束只读；撤回内容不可见；无权限不显示历史', (tester) async {
    var inaccessible = false;
    final client = MockClient((request) async {
      if (inaccessible) return http.Response('{}', 404);
      if (request.url.path.endsWith('/messages')) {
        return envelope({
          'messages': [message(removed: true)],
          'hasMore': false,
        });
      }
      return envelope(state(canSend: false));
    });
    await tester.pumpWidget(screen(client, () => 'Bearer test'));
    await tester.pumpAndSettle();
    expect(find.textContaining('可查看历史'), findsOneWidget);
    expect(find.text('这条消息已被移除'), findsOneWidget);
    expect(find.byType(TextField), findsNothing);
    inaccessible = true;
    await tester.tap(find.byTooltip('刷新消息'));
    await tester.pumpAndSettle();
    expect(find.text('这条消息已被移除'), findsNothing);
    expect(find.textContaining('确认报名的参与者'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    client.close();
  });

  testWidgets('换账号立即隐藏历史和草稿，不接受旧身份响应', (tester) async {
    String? token = 'Bearer first';
    final pending = Completer<http.Response>();
    final client = MockClient((request) async {
      if (request.headers['Authorization'] != 'Bearer first') {
        return pending.future;
      }
      return request.url.path.endsWith('/messages')
          ? envelope({
              'messages': [message()],
              'hasMore': false,
            })
          : envelope(state());
    });
    await tester.pumpWidget(screen(client, () => token));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), '私密草稿');
    token = 'Bearer second';
    await tester.pump(const Duration(seconds: 1));
    expect(find.text('十点体育馆见'), findsNothing);
    expect(find.text('私密草稿'), findsNothing);
    pending.complete(envelope(state(joined: false)));
    await tester.pumpAndSettle();
    expect(find.text('同意并加入交流'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    client.close();
  });

  testWidgets('普通参与者不能移除他人消息；举报传活动消息目标', (tester) async {
    final client = MockClient(
      (request) async => request.url.path.endsWith('/messages')
          ? envelope({
              'messages': [message(sender: 'other')],
              'hasMore': false,
            })
          : envelope(state()),
    );
    await tester.pumpWidget(screen(client, () => 'Bearer test'));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('消息操作'));
    await tester.pumpAndSettle();
    expect(find.text('移除消息'), findsNothing);
    await tester.tap(find.text('举报消息'));
    await tester.pumpAndSettle();
    final page = tester.widget<SupportPage>(find.byType(SupportPage));
    expect(page.targetType, 'activity_message');
    expect(page.targetID, 'message');
    await tester.pumpWidget(const SizedBox());
    client.close();
  });

  testWidgets('主办方移除需确认，退出交流后需再次主动加入', (tester) async {
    var joined = true;
    var removed = false;
    var removals = 0;
    final client = MockClient((request) async {
      if (request.method == 'DELETE') {
        if (request.url.path.endsWith('/messages/message')) {
          removed = true;
          removals++;
        } else {
          joined = false;
        }
        return http.Response('', 204);
      }
      if (request.url.path.endsWith('/messages')) {
        return envelope({
          'messages': [message(sender: 'other', removed: removed)],
          'hasMore': false,
        });
      }
      return envelope(state(joined: joined, moderator: true));
    });
    await tester.pumpWidget(screen(client, () => 'Bearer test'));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('消息操作'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('移除消息'));
    await tester.pumpAndSettle();
    expect(removals, 0);
    await tester.tap(find.text('确认'));
    await tester.pumpAndSettle();
    expect(removals, 1);
    expect(find.text('这条消息已被移除'), findsOneWidget);
    await tester.tap(find.text('退出交流'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('确认'));
    await tester.pumpAndSettle();
    expect(find.text('同意并加入交流'), findsOneWidget);
    expect(find.text('这条消息已被移除'), findsNothing);
    await tester.pumpWidget(const SizedBox());
    client.close();
  });
}
