import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/social_intent_creation_pending_store.dart';

import 'package:birdtie_client/src/workspace/social_intent_drafts.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'social_intent_creation_api_test.dart' show creationWire;

late MemorySocialIntentCreationPendingStore _intentPendingStore;

const _ownerA = '11111111-1111-4111-8111-111111111111';
const _ownerB = '22222222-2222-4222-8222-222222222222';

Future<void> _chooseMode(WidgetTester tester, String label) async {
  await tester.tap(find.byType(DropdownButtonFormField<String>));
  await tester.pumpAndSettle();
  await tester.tap(find.text(label).last);
  await tester.pumpAndSettle();
}

class _ChangingAuthorization extends ChangeNotifier {
  String? token = 'Bearer fixture-A';
  void use(String? value) {
    token = value;
    notifyListeners();
  }
}

Map<String, dynamic> _draft(String id, String title) => {
  'id': id,
  'title': title,
  'modality': 'ONLINE',
  'audience': 'PRIVATE',
  'status': 'DRAFT',
};

http.Response _rows(List<Map<String, dynamic>> rows) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': rows})), 200);

Future<void> _showDrafts(
  WidgetTester tester,
  _ChangingAuthorization auth,
  http.Client client, {
  String? task,
  String? title,
}) async {
  await tester.pumpWidget(
    MaterialApp(
      home: SocialIntentDraftPage(
        pendingStore: _intentPendingStore,
        authorizationHeader: () => auth.token,
        authorizationChanges: auth,
        ownerID: () => auth.token == 'Bearer fixture-B' ? _ownerB : _ownerA,
        apiBaseUrl: 'https://api.test',
        client: client,
        initialModality: 'ONLINE',
        agentTaskId: task,
        suggestedTitle: title,
      ),
    ),
  );
}

Future<void> _disposeDrafts(
  WidgetTester tester,
  _ChangingAuthorization auth,
  http.Client client,
) async {
  await tester.pumpWidget(const SizedBox());
  auth.dispose();
  client.close();
}

void main() {
  setUp(() => _intentPendingStore = MemorySocialIntentCreationPendingStore());
  testWidgets('切换账号清空私人草稿及输入，旧GET迟到不能覆盖新账号', (tester) async {
    final auth = _ChangingAuthorization();
    final pending = Completer<http.Response>();
    final client = MockClient((request) async {
      if (request.headers['Authorization'] == 'Bearer fixture-A') {
        return pending.future;
      }
      return _rows([_draft('B-draft', 'B自己的私人草稿')]);
    });
    await _showDrafts(tester, auth, client, title: 'A正在编辑的私人想法');
    await tester.pump();
    auth.use('Bearer fixture-B');
    await tester.pumpAndSettle();
    expect(find.text('B自己的私人草稿'), findsOneWidget);
    expect(
      tester.widget<TextField>(find.byType(TextField).first).controller!.text,
      isEmpty,
    );
    pending.complete(_rows([_draft('A-draft', 'A的迟到私人草稿')]));
    await tester.pumpAndSettle();
    expect(find.text('A的迟到私人草稿'), findsNothing);
    expect(find.text('B自己的私人草稿'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await _disposeDrafts(tester, auth, client);
  });

  testWidgets('草稿取消预览返回不写入，切换身份关闭预览且旧确认不能POST', (tester) async {
    final auth = _ChangingAuthorization();
    final writes = <http.Request>[];
    final client = MockClient((request) async {
      if (request.method == 'POST') {
        writes.add(request);
        return http.Response('{}', 200);
      }
      return auth.token == 'Bearer fixture-A'
          ? _rows([_draft('A-draft', 'A自己的草稿')])
          : _rows([_draft('B-draft', 'B自己的草稿')]);
    });
    await _showDrafts(tester, auth, client);
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.byTooltip('取消这条意图'));
    await tester.tap(find.byTooltip('取消这条意图'));
    await tester.pumpAndSettle();
    expect(writes, isEmpty);
    await tester.tap(find.text('返回'));
    await tester.pumpAndSettle();
    expect(writes, isEmpty);
    await tester.tap(find.byTooltip('取消这条意图'));
    await tester.pumpAndSettle();
    final confirm = tester
        .widget<FilledButton>(find.widgetWithText(FilledButton, '确认取消'))
        .onPressed!;
    auth.use('Bearer fixture-B');
    confirm();
    await tester.pumpAndSettle();
    expect(find.text('取消这条意图？'), findsNothing);
    expect(find.text('A自己的草稿'), findsNothing);
    expect(find.text('B自己的草稿'), findsOneWidget);
    expect(writes, isEmpty);
    expect(tester.takeException(), isNull);
    await _disposeDrafts(tester, auth, client);
  });

  testWidgets('确认取消绑定当前身份，注销后迟到响应不回读或恢复私有记录', (tester) async {
    final auth = _ChangingAuthorization();
    final requests = <http.Request>[];
    final pending = Completer<http.Response>();
    final client = MockClient((request) async {
      requests.add(request);
      if (request.method == 'POST') return pending.future;
      return _rows([_draft('A-draft', 'A自己的草稿')]);
    });
    await _showDrafts(tester, auth, client);
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.byTooltip('取消这条意图'));
    await tester.tap(find.byTooltip('取消这条意图'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('确认取消'));
    await tester.pump();
    final write = requests.singleWhere((request) => request.method == 'POST');
    expect(write.url.path, '/v1/me/social-intents/A-draft/cancel');
    expect(write.headers['Authorization'], 'Bearer fixture-A');
    expect(jsonDecode(write.body), {'confirmed': true});
    auth.use(null);
    pending.complete(http.Response('{}', 200));
    await tester.pumpAndSettle();
    expect(requests, hasLength(2));
    expect(find.text('A自己的草稿'), findsNothing);
    expect(find.text('取消失败，请重试。'), findsNothing);
    expect(tester.takeException(), isNull);
    await _disposeDrafts(tester, auth, client);
  });

  testWidgets('保存A草稿的迟到成功不能清除B正在编辑的内容或重新读取A数据', (tester) async {
    final auth = _ChangingAuthorization();
    final requests = <http.Request>[];
    final pending = Completer<http.Response>();
    final client = MockClient((request) async {
      requests.add(request);
      return request.method == 'POST' ? pending.future : _rows([]);
    });
    await _showDrafts(tester, auth, client);
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField).first, 'A明确保存的私人草稿');
    await tester.ensureVisible(find.text('保存私人草稿'));
    await tester.tap(find.text('保存私人草稿'));
    await tester.pump();
    auth.use('Bearer fixture-B');
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField).first, 'B正在编辑的私人内容');
    pending.complete(
      http.Response(
        jsonEncode({
          'data': {'id': 'A-created'},
        }),
        201,
      ),
    );
    await tester.pumpAndSettle();
    expect(
      tester.widget<TextField>(find.byType(TextField).first).controller!.text,
      'B正在编辑的私人内容',
    );
    expect(requests.where((request) => request.method == 'POST'), hasLength(1));
    expect(requests.where((request) => request.method == 'GET'), hasLength(2));
    expect(
      requests
          .where((request) => request.method == 'POST')
          .single
          .headers['Authorization'],
      'Bearer fixture-A',
    );
    expect(tester.takeException(), isNull);
    await _disposeDrafts(tester, auth, client);
  });

  testWidgets('旧Agent查询跨身份后失效，清除建议且不能以新身份保存旧来源', (tester) async {
    final auth = _ChangingAuthorization();
    final writes = <http.Request>[];
    final client = MockClient((request) async {
      if (request.method == 'POST') writes.add(request);
      return _rows([]);
    });
    await _showDrafts(
      tester,
      auth,
      client,
      task: '11111111-1111-4111-8111-111111111111',
      title: 'A私人的Agent查询',
    );
    await tester.pumpAndSettle();
    auth.use('Bearer fixture-B');
    await tester.pumpAndSettle();
    expect(
      tester.widget<TextField>(find.byType(TextField).first).controller!.text,
      isEmpty,
    );
    await tester.ensureVisible(find.text('保存私人草稿'));
    expect(
      tester
          .widget<FilledButton>(find.widgetWithText(FilledButton, '保存私人草稿'))
          .onPressed,
      isNull,
    );
    expect(find.text('登录状态已更新，请返回 Agent 重新选择本人的活动查询。'), findsOneWidget);
    expect(writes, isEmpty);
    await _disposeDrafts(tester, auth, client);
  });

  testWidgets('Agent 找活动查询只在编辑并确认后保存私人意图', (tester) async {
    const taskID = '11111111-1111-4111-8111-111111111111';
    Map<String, dynamic>? submitted;
    final client = MockClient((request) async {
      if (request.method == 'GET') {
        return http.Response(jsonEncode({'data': []}), 200);
      }
      expect(
        request.url.path,
        '/v1/me/agent-tasks/$taskID/social-intent-drafts',
      );
      submitted = jsonDecode(request.body) as Map<String, dynamic>;
      return creationWire(request);
    });
    await tester.pumpWidget(
      MaterialApp(
        home: SocialIntentDraftPage(
          pendingStore: _intentPendingStore,
          authorizationHeader: () => 'Bearer person',
          ownerID: () => _ownerA,
          apiBaseUrl: 'https://api.test',
          client: client,
          agentTaskId: taskID,
          suggestedTitle: '帮我找周末的羽毛球',
          suggestedCategory: 'badminton',
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(submitted, isNull);
    expect(
      find.text('这条草稿来自你的 Agent 找活动查询。请先修改并确认内容；搜索本身不会创建社交意图。'),
      findsOneWidget,
    );
    await tester.enterText(find.byType(TextField).first, '周末一起打羽毛球');
    await _chooseMode(tester, '线下');
    await tester.enterText(
      find.byKey(const Key('intent-draft-area')),
      '阿伯丁市中心',
    );
    await tester.ensureVisible(find.text('保存私人草稿'));
    await tester.tap(find.text('保存私人草稿'));
    await tester.pumpAndSettle();
    expect(submitted?['confirmed'], isTrue);
    final draft = submitted?['draft'] as Map<String, dynamic>;
    expect(draft['type'], 'FIND_ACTIVITY');
    expect(draft['title'], '周末一起打羽毛球');
    expect(draft['audience'], 'PRIVATE');
    expect(
      (draft['constraints'] as Map<String, dynamic>)['areaLabel'],
      '阿伯丁市中心',
    );
  });

  testWidgets('线上社交意图不要求位置，并只保存私人草稿', (tester) async {
    Map<String, dynamic>? submitted;
    final client = MockClient((request) async {
      expect(request.headers['Authorization'], 'Bearer person');
      if (request.method == 'GET') {
        return http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': submitted == null
                  ? []
                  : [
                      {
                        'title': submitted!['title'],
                        'modality': submitted!['modality'],
                      },
                    ],
            }),
          ),
          200,
        );
      }
      submitted = jsonDecode(request.body) as Map<String, dynamic>;
      return creationWire(request);
    });
    await tester.pumpWidget(
      MaterialApp(
        home: SocialIntentDraftPage(
          pendingStore: _intentPendingStore,
          authorizationHeader: () => 'Bearer person',
          ownerID: () => _ownerA,
          apiBaseUrl: 'https://api.test',
          client: client,
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('草稿仅自己可见。现在保存不会公开，也不会自动邀请其他人。'), findsOneWidget);
    await _chooseMode(tester, '线上');
    expect(find.text('大致区域'), findsNothing);
    await tester.enterText(find.byType(TextField).first, '线上讨论周末羽毛球');
    await tester.ensureVisible(find.text('保存私人草稿'));
    await tester.tap(find.text('保存私人草稿'));
    await tester.pumpAndSettle();
    expect(submitted?['modality'], 'ONLINE');
    expect(submitted?['audience'], 'PRIVATE');
    expect(submitted?['constraints'], isEmpty);
    expect(find.text('线上讨论周末羽毛球'), findsOneWidget);
  });

  testWidgets('混合意图要求大致区域和线上参与方式', (tester) async {
    final client = MockClient(
      (request) async => http.Response(jsonEncode({'data': []}), 200),
    );
    await tester.pumpWidget(
      MaterialApp(
        home: SocialIntentDraftPage(
          pendingStore: _intentPendingStore,
          authorizationHeader: () => 'Bearer person',
          ownerID: () => _ownerA,
          apiBaseUrl: 'https://api.test',
          client: client,
        ),
      ),
    );
    await tester.pumpAndSettle();
    await _chooseMode(tester, '线下＋线上');
    await tester.tap(find.text('整理草稿'));
    await tester.pumpAndSettle();
    expect(find.text('大致区域'), findsOneWidget);
    expect(find.text('线上参与方式'), findsOneWidget);
    await tester.enterText(find.byType(TextField).first, '混合羽毛球交流');
    await tester.ensureVisible(find.text('保存私人草稿'));
    await tester.tap(find.text('保存私人草稿'));
    await tester.pumpAndSettle();
    expect(find.text('请填写标题及当前方式所需的区域或线上方式。'), findsOneWidget);
  });

  testWidgets('已激活的公开意图如实标记受众，取消后更新服务端状态', (tester) async {
    const id = '11111111-1111-4111-8111-111111111111';
    var cancelled = false;
    final client = MockClient((request) async {
      expect(request.headers['Authorization'], 'Bearer person');
      if (request.method == 'POST') {
        expect(request.url.path, '/v1/me/social-intents/$id/cancel');
        cancelled = true;
        return http.Response(
          jsonEncode({
            'data': {'id': id, 'status': 'CANCELLED'},
          }),
          200,
        );
      }
      return http.Response.bytes(
        utf8.encode(
          jsonEncode({
            'data': [
              {
                'id': id,
                'title': '公开羽毛球意图',
                'modality': 'IN_PERSON',
                'audience': 'PUBLIC',
                'status': cancelled ? 'CANCELLED' : 'ACTIVE',
              },
            ],
          }),
        ),
        200,
      );
    });
    await tester.pumpWidget(
      MaterialApp(
        home: SocialIntentDraftPage(
          pendingStore: _intentPendingStore,
          authorizationHeader: () => 'Bearer person',
          ownerID: () => _ownerA,
          apiBaseUrl: 'https://api.test',
          client: client,
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('线下 · 已激活 · 公开'), findsOneWidget);
    await tester.ensureVisible(find.byTooltip('取消这条意图'));
    await tester.tap(find.byTooltip('取消这条意图'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('确认取消'));
    await tester.pumpAndSettle();
    expect(cancelled, isTrue);
    expect(find.text('线下 · 已取消'), findsOneWidget);
    expect(find.byTooltip('取消这条意图'), findsNothing);
  });
}
