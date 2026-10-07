import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/social_intent_creation_pending_store.dart';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:birdtie_client/src/workspace/social_intent_drafts.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'social_intent_creation_api_test.dart' show creationWire;

late MemorySocialIntentCreationPendingStore _intentPendingStore;

const _ownerA = '11111111-1111-4111-8111-111111111111';
const _ownerB = '22222222-2222-4222-8222-222222222222';
const _createdID = '33333333-3333-4333-8333-333333333333';

http.Request? _lastCreationRequest;
MockClient _capturingClient(
  Future<http.Response> Function(http.Request) handler,
) => MockClient((request) {
  if (request.method == 'POST' &&
      (request.url.path.endsWith('/social-intents') ||
          request.url.path.endsWith('/social-intent-drafts'))) {
    _lastCreationRequest = request;
  }
  return handler(request);
});
http.Response _receipt({Map<String, dynamic>? bad, String title = '本人的私人想法'}) =>
    creationWire(_lastCreationRequest!, bad: {'title': title, ...?bad});

Future<void> _tap(WidgetTester tester, String text) async {
  await tester.ensureVisible(find.text(text));
  await tester.tap(find.text(text));
  await tester.pumpAndSettle();
}

Future<void> _mode(WidgetTester tester, String text) async {
  await tester.ensureVisible(find.byType(DropdownButtonFormField<String>));
  await tester.tap(find.byType(DropdownButtonFormField<String>));
  await tester.pumpAndSettle();
  await tester.tap(find.text(text).last);
  await tester.pumpAndSettle();
}

Future<void> _show(
  WidgetTester tester,
  http.Client client, {
  String title = '本人的私人想法',
  String? task,
  ValueChanged<String>? onManage,
}) async {
  await tester.pumpWidget(
    MaterialApp(
      home: SocialIntentDraftPage(
        pendingStore: _intentPendingStore,
        authorizationHeader: () => 'Bearer fixture-A',
        ownerID: () => _ownerA,
        apiBaseUrl: 'https://api.test',
        client: client,
        initialModality: 'ONLINE',
        suggestedTitle: title,
        agentTaskId: task,
        onManageIntent: onManage,
      ),
    ),
  );
  await tester.pumpAndSettle();
}

String _value(WidgetTester tester, String key) =>
    tester.widget<TextField>(find.byKey(Key(key))).controller!.text;

class _Identity extends ChangeNotifier {
  _Identity(this.client);
  String? token = 'Bearer fixture-A', owner = _ownerA, workspace;
  bool current = true;
  String base = 'https://api.test';
  http.Client client;
  void changed() => notifyListeners();
}

Future<void> _showIdentity(WidgetTester tester, _Identity identity) async {
  await tester.pumpWidget(
    MaterialApp(
      home: AnimatedBuilder(
        animation: identity,
        builder: (_, _) => SocialIntentDraftPage(
          pendingStore: _intentPendingStore,
          authorizationHeader: () => identity.token,
          authorizationChanges: identity,
          ownerID: () => identity.owner,
          workspaceID: () => identity.workspace,
          workspaceChanges: identity,
          current: () => identity.current,
          apiBaseUrl: identity.base,
          client: identity.client,
          initialModality: 'ONLINE',
          suggestedTitle: '本人的私人想法',
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  setUp(() {
    _intentPendingStore = MemorySocialIntentCreationPendingStore();
    _lastCreationRequest = null;
  });
  testWidgets('未知参与方式不能被预设为线上或线下', (tester) async {
    final client = _capturingClient(
      (_) async => http.Response('{"data":[]}', 200),
    );
    await tester.pumpWidget(
      MaterialApp(
        home: SocialIntentDraftPage(
          pendingStore: _intentPendingStore,
          authorizationHeader: () => 'Bearer fixture',
          ownerID: () => '11111111-1111-4111-8111-111111111111',
          apiBaseUrl: 'https://api.test',
          client: client,
        ),
      ),
    );
    await tester.pumpAndSettle();
    final mode = tester.widget<DropdownButtonFormField<String>>(
      find.byType(DropdownButtonFormField<String>),
    );
    expect(mode.initialValue, isNull);
    await tester.pumpWidget(const SizedBox());
    client.close();
  });

  testWidgets('首屏先整理一句话而不是展开所有可选字段', (tester) async {
    final client = _capturingClient(
      (_) async => http.Response('{"data":[]}', 200),
    );
    await tester.pumpWidget(
      MaterialApp(
        home: SocialIntentDraftPage(
          pendingStore: _intentPendingStore,
          authorizationHeader: () => 'Bearer fixture',
          apiBaseUrl: 'https://api.test',
          client: client,
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.byType(TextField), findsOneWidget);
    expect(find.text('整理草稿'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    client.close();
  });

  testWidgets('服务端5xx是结果未知，不能解除状态后盲目再次POST', (tester) async {
    var writes = 0;
    final client = _capturingClient((request) async {
      if (request.method == 'GET') return http.Response('{"data":[]}', 200);
      writes++;
      return http.Response(jsonEncode({'error': 'server_error'}), 500);
    });
    await tester.pumpWidget(
      MaterialApp(
        home: SocialIntentDraftPage(
          pendingStore: _intentPendingStore,
          authorizationHeader: () => 'Bearer fixture',
          apiBaseUrl: 'https://api.test',
          client: client,
          initialModality: 'ONLINE',
          ownerID: () => '11111111-1111-4111-8111-111111111111',
          suggestedTitle: '先记下线上讨论',
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('保存私人草稿'));
    await tester.tap(find.text('保存私人草稿'));
    await tester.pumpAndSettle();
    expect(writes, 1);
    final button = tester.widget<FilledButton>(
      find.widgetWithText(FilledButton, '保存私人草稿'),
    );
    expect(button.onPressed, isNull);
    expect(find.textContaining('结果未知'), findsWidgets);
    await tester.pumpWidget(const SizedBox());
    client.close();
  });

  testWidgets('真实201保留ID及成功状态，刷新失败不会诱发重新创建', (tester) async {
    var reads = 0, writes = 0;
    final managed = <String>[];
    final client = _capturingClient((r) async {
      if (r.method == 'GET') {
        return http.Response(
          reads++ == 0 ? '{"data":[]}' : '{"error":"fixture"}',
          reads == 1 ? 200 : 500,
        );
      }
      writes++;
      final body = jsonDecode(r.body) as Map;
      expect(body['audience'], 'PRIVATE');
      expect(r.headers['Authorization'], 'Bearer fixture-A');
      return _receipt();
    });
    await _show(tester, client, onManage: managed.add);
    await _tap(tester, '保存私人草稿');
    expect(find.text('私人草稿已保存'), findsOneWidget);
    expect(find.textContaining('草稿已保存；列表刷新失败'), findsOneWidget);
    expect(find.text('保存私人草稿'), findsNothing);
    await _tap(tester, '查看已保存的意图');
    expect(managed, [_createdID]);
    await _tap(tester, '重新读取记录');
    expect(writes, 1);
    expect(find.text('私人草稿已保存'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    client.close();
  });

  for (final bad in <Map<String, dynamic>>[
    {'id': 'draft'},
    {'creatorAccountId': _ownerB},
    {'audience': 'PUBLIC'},
    {'status': 'ACTIVE'},
    {'title': '另一条内容'},
  ]) {
    testWidgets('不可信201 ${bad.keys.single}保留输入并禁止盲目重发', (tester) async {
      var writes = 0;
      final managed = <String>[];
      final client = _capturingClient((r) async {
        if (r.method == 'GET') return http.Response('{"data":[]}', 200);
        writes++;
        return _receipt(bad: bad);
      });
      await _show(tester, client, onManage: managed.add);
      await _tap(tester, '完整编辑');
      await _tap(tester, '保存私人草稿');
      expect(writes, 1);
      expect(find.text('私人草稿已保存'), findsNothing);
      expect(find.text('查看已保存的意图'), findsNothing);
      expect(_value(tester, 'intent-draft-title'), '本人的私人想法');
      expect(
        tester
            .widget<TextField>(find.byKey(const Key('intent-draft-title')))
            .enabled,
        isFalse,
      );
      for (final label in [
        '整理草稿',
        '修改寻找有效期',
        '选择活动开始时间',
        '选择活动结束时间',
        '清除活动时间',
      ]) {
        final action = tester.widget<TextButton>(
          find.widgetWithText(TextButton, label),
        );
        expect(action.onPressed, isNull, reason: label);
      }
      await _tap(tester, '核实我的意图记录');
      expect(
        tester
            .widget<FilledButton>(find.widgetWithText(FilledButton, '保存私人草稿'))
            .onPressed,
        isNull,
      );
      expect(find.textContaining('结果未知'), findsWidgets);
      expect(writes, 1);
      expect(managed, isEmpty);
      await tester.pumpWidget(const SizedBox());
      client.close();
    });
  }

  testWidgets('来源409及同标题列表不能替代原提交回执', (tester) async {
    var reads = 0, writes = 0;
    final client = _capturingClient((r) async {
      if (r.method == 'GET') {
        return http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': reads++ == 0
                  ? []
                  : [
                      {
                        'id': _createdID,
                        'creatorAccountId': _ownerA,
                        'title': '本人的私人想法',
                        'audience': 'PRIVATE',
                        'status': 'DRAFT',
                        'modality': 'ONLINE',
                      },
                    ],
            }),
          ),
          200,
        );
      }
      writes++;
      expect(
        r.url.path,
        '/v1/me/agent-tasks/44444444-4444-4444-8444-444444444444/social-intent-drafts',
      );
      expect((jsonDecode(r.body) as Map)['confirmed'], isTrue);
      return http.Response('{"error":"conflict"}', 409);
    });
    await _show(tester, client, task: '44444444-4444-4444-8444-444444444444');
    await _tap(tester, '保存私人草稿');
    await _tap(tester, '核实我的意图记录');
    expect(find.widgetWithText(ListTile, '本人的私人想法'), findsOneWidget);
    expect(find.text('私人草稿已保存'), findsNothing);
    expect(find.textContaining('无法核实它的来源'), findsOneWidget);
    expect(
      tester
          .widget<FilledButton>(find.widgetWithText(FilledButton, '保存私人草稿'))
          .onPressed,
      isNull,
    );
    expect(writes, 1);
    await tester.pumpWidget(const SizedBox());
    client.close();
  });

  testWidgets('提交超时保存原输入，迟到201不能解除核实保护', (tester) async {
    final pending = Completer<http.Response>();
    var writes = 0;
    final client = _capturingClient((r) async {
      if (r.method == 'GET') return http.Response('{"data":[]}', 200);
      writes++;
      return pending.future;
    });
    await _show(tester, client);
    await tester.ensureVisible(find.text('保存私人草稿'));
    await tester.tap(find.text('保存私人草稿'));
    await tester.pump();
    await tester.pump(const Duration(seconds: 13));
    await tester.pumpAndSettle();
    expect(find.textContaining('结果未知'), findsWidgets);
    expect(_value(tester, 'intent-draft-title'), '本人的私人想法');
    pending.complete(_receipt());
    await tester.pumpAndSettle();
    expect(find.text('私人草稿已保存'), findsNothing);
    await _tap(tester, '核实我的意图记录');
    expect(
      tester
          .widget<FilledButton>(find.widgetWithText(FilledButton, '保存私人草稿'))
          .onPressed,
      isNull,
    );
    expect(writes, 1);
    await tester.pumpWidget(const SizedBox());
    client.close();
  });

  testWidgets('一句话整理与完整编辑往返使用同一草稿，模式分支不丢用户输入', (tester) async {
    const original = '这周末想在阿伯丁打羽毛球，先帮我记下来';
    Map<String, dynamic>? sent;
    final client = _capturingClient((r) async {
      if (r.method == 'GET') return http.Response('{"data":[]}', 200);
      sent = jsonDecode(r.body) as Map<String, dynamic>;
      return _receipt(title: original);
    });
    await tester.pumpWidget(
      MaterialApp(
        home: SocialIntentDraftPage(
          pendingStore: _intentPendingStore,
          authorizationHeader: () => 'Bearer fixture-A',
          ownerID: () => _ownerA,
          apiBaseUrl: 'https://api.test',
          client: client,
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.enterText(
      find.byKey(const Key('intent-draft-title')),
      original,
    );
    await _tap(tester, '整理草稿');
    expect(
      tester
          .widget<DropdownButtonFormField<String>>(
            find.byType(DropdownButtonFormField<String>),
          )
          .initialValue,
      isNull,
    );
    expect(find.textContaining('具体活动时间未确定'), findsOneWidget);
    expect(find.byKey(const Key('intent-draft-category')), findsNothing);
    await _mode(tester, '线下');
    expect(_value(tester, 'intent-draft-area'), '阿伯丁');
    await _tap(tester, '完整编辑');
    await tester.enterText(
      find.byKey(const Key('intent-draft-category')),
      '球类运动',
    );
    await _mode(tester, '线下＋线上');
    await tester.enterText(
      find.byKey(const Key('intent-draft-platform')),
      '视频讨论',
    );
    await _tap(tester, '返回草稿摘要');
    await _mode(tester, '线上');
    expect(find.byKey(const Key('intent-draft-area')), findsNothing);
    await _mode(tester, '线下＋线上');
    expect(_value(tester, 'intent-draft-area'), '阿伯丁');
    expect(_value(tester, 'intent-draft-platform'), '视频讨论');
    await _tap(tester, '完整编辑');
    expect(_value(tester, 'intent-draft-category'), '球类运动');
    await _tap(tester, '返回草稿摘要');
    await _tap(tester, '保存私人草稿');
    expect(sent!['title'], original);
    expect(sent!['audience'], 'PRIVATE');
    expect(sent!['modality'], 'HYBRID');
    expect(sent!['constraints'], {
      'category': '球类运动',
      'areaLabel': '阿伯丁',
      'onlinePlatform': '视频讨论',
    });
    expect(sent!.containsKey('cityId'), isFalse);
    expect((sent!['constraints'] as Map).containsKey('startsAt'), isFalse);
    expect(
      DateTime.parse(
        sent!['expiresAt'] as String,
      ).isAfter(DateTime.now().add(const Duration(days: 6))),
      isTrue,
    );
    expect(find.text('私人草稿已保存'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    client.close();
  });

  for (final axis in [
    'token',
    'owner',
    'workspace',
    'current',
    'base',
    'client',
  ]) {
    testWidgets('$axis 的ABA切换清除旧草稿，迟到POST不能污染新编辑', (tester) async {
      final pending = Completer<http.Response>();
      http.Request? captured;
      var writes = 0;
      final first = _capturingClient((r) async {
        if (r.method == 'GET') return http.Response('{"data":[]}', 200);
        writes++;
        captured = r;
        return pending.future;
      });
      final second = _capturingClient(
        (_) async => http.Response('{"data":[]}', 200),
      );
      final identity = _Identity(first);
      await _showIdentity(tester, identity);
      await tester.ensureVisible(find.text('保存私人草稿'));
      await tester.tap(find.text('保存私人草稿'));
      await tester.pump();
      expect(writes, 1);
      void change(bool other) {
        switch (axis) {
          case 'token':
            identity.token = other ? 'Bearer fixture-B' : 'Bearer fixture-A';
          case 'owner':
            identity.owner = other ? _ownerB : _ownerA;
          case 'workspace':
            identity.workspace = other ? 'fixture-org' : null;
          case 'current':
            identity.current = !other;
          case 'base':
            identity.base = other ? 'https://other.test' : 'https://api.test';
          case 'client':
            identity.client = other ? second : first;
        }
        identity.changed();
      }

      change(true);
      await tester.pumpAndSettle();
      expect(_value(tester, 'intent-draft-title'), isEmpty);
      change(false);
      await tester.pumpAndSettle();
      await tester.ensureVisible(find.byKey(const Key('intent-draft-title')));
      await tester.enterText(
        find.byKey(const Key('intent-draft-title')),
        '当前账号的新输入',
      );
      pending.complete(_receipt());
      await tester.pumpAndSettle();
      expect(_value(tester, 'intent-draft-title'), '当前账号的新输入');
      expect(find.text('私人草稿已保存'), findsNothing);
      expect(captured!.headers['Authorization'], 'Bearer fixture-A');
      expect(captured!.url.origin, 'https://api.test');
      expect((jsonDecode(captured!.body) as Map)['title'], '本人的私人想法');
      expect((jsonDecode(captured!.body) as Map)['audience'], 'PRIVATE');
      expect(writes, 1);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      identity.dispose();
      first.close();
      second.close();
    });
  }

  testWidgets('匿名输入明确未持久保存，不能触发POST', (tester) async {
    var writes = 0;
    final client = _capturingClient((r) async {
      if (r.method != 'GET') writes++;
      return http.Response('{"data":[]}', 200);
    });
    await tester.pumpWidget(
      MaterialApp(
        home: SocialIntentDraftPage(
          pendingStore: _intentPendingStore,
          authorizationHeader: () => null,
          apiBaseUrl: 'https://api.test',
          client: client,
          initialModality: 'ONLINE',
          suggestedTitle: '匿名本页草稿',
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.textContaining('不支持匿名本机持久保存'), findsOneWidget);
    await _tap(tester, '保存私人草稿');
    expect(find.textContaining('请先登录并核实本人账号'), findsOneWidget);
    expect(writes, 0);
    expect(_value(tester, 'intent-draft-title'), '匿名本页草稿');
    await tester.pumpWidget(const SizedBox());
    client.close();
  });

  testWidgets('快速连点仅一次POST，确定拒绝允许修改后重新提交', (tester) async {
    final pending = Completer<http.Response>();
    var writes = 0;
    final client = _capturingClient((r) async {
      if (r.method == 'GET') return http.Response('{"data":[]}', 200);
      return ++writes == 1 ? pending.future : _receipt(title: '修改后的私人想法');
    });
    await _show(tester, client);
    await tester.ensureVisible(find.text('保存私人草稿'));
    final button = tester.widget<FilledButton>(
      find.widgetWithText(FilledButton, '保存私人草稿'),
    );
    button.onPressed!();
    button.onPressed!();
    await tester.pump();
    expect(writes, 1);
    pending.complete(
      creationWire(_lastCreationRequest!, reason: 'SOURCE_CHANGED'),
    );
    await tester.pumpAndSettle();
    expect(find.text('保存被拒绝，请检查内容后再试。'), findsOneWidget);
    expect(
      tester
          .widget<TextField>(find.byKey(const Key('intent-draft-title')))
          .enabled,
      isTrue,
    );
    await tester.ensureVisible(find.byKey(const Key('intent-draft-title')));
    await tester.enterText(
      find.byKey(const Key('intent-draft-title')),
      '修改后的私人想法',
    );
    await _tap(tester, '保存私人草稿');
    expect(writes, 2);
    expect(find.text('私人草稿已保存'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    client.close();
  });

  testWidgets('组织身份不能提交个人草稿，切回个人清除原输入', (tester) async {
    var writes = 0;
    final client = _capturingClient((r) async {
      if (r.method != 'GET') writes++;
      return http.Response('{"data":[]}', 200);
    });
    final identity = _Identity(client)..workspace = 'fixture-org';
    await _showIdentity(tester, identity);
    await _tap(tester, '保存私人草稿');
    expect(find.text('请切回个人工作区保存本人的私人草稿。'), findsOneWidget);
    expect(writes, 0);
    identity.workspace = null;
    identity.changed();
    await tester.pumpAndSettle();
    expect(_value(tester, 'intent-draft-title'), isEmpty);
    expect(find.text('请切回个人工作区保存本人的私人草稿。'), findsNothing);
    await tester.pumpWidget(const SizedBox());
    identity.dispose();
    client.close();
  });

  for (final brightness in [Brightness.light, Brightness.dark]) {
    testWidgets('360宽${brightness.name}主题大字号与键盘下关键说明及保存可达（非真机）', (
      tester,
    ) async {
      await tester.binding.setSurfaceSize(const Size(360, 900));
      addTearDown(() => tester.binding.setSurfaceSize(null));
      final fixtureFont =
          Platform.environment['BIRDTIE_INTENT_DRAFT_FIXTURE_FONT'];
      if (fixtureFont != null && File(fixtureFont).existsSync()) {
        final font = FontLoader('BirdtieFixtureCJK')
          ..addFont(
            Future.value(
              ByteData.sublistView(File(fixtureFont).readAsBytesSync()),
            ),
          );
        await tester.runAsync(font.load);
        final icons = FontLoader('MaterialIcons')
          ..addFont(rootBundle.load('fonts/MaterialIcons-Regular.otf'));
        await tester.runAsync(icons.load);
      }
      final client = _capturingClient(
        (_) async => http.Response('{"data":[]}', 200),
      );
      const boundaryKey = Key('draft-widget-mock-image');
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData(
            brightness: brightness,
            fontFamily: fixtureFont == null ? null : 'BirdtieFixtureCJK',
          ),
          home: RepaintBoundary(
            key: boundaryKey,
            child: Column(
              children: [
                const Material(
                  child: SafeArea(
                    bottom: false,
                    child: Text(
                      'Widget 隔离样例 · 不是真机',
                      style: TextStyle(fontSize: 12),
                    ),
                  ),
                ),
                Expanded(
                  child: MediaQuery(
                    data: const MediaQueryData(
                      size: Size(360, 900),
                      textScaler: TextScaler.linear(2),
                      viewInsets: EdgeInsets.only(bottom: 280),
                    ),
                    child: SocialIntentDraftPage(
                      pendingStore: _intentPendingStore,
                      authorizationHeader: () => 'Bearer fixture-A',
                      ownerID: () => _ownerA,
                      apiBaseUrl: 'https://api.test',
                      client: client,
                      suggestedTitle: '周末想打羽毛球，地点与时间还要确认',
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      final initialPath =
          Platform.environment['BIRDTIE_INTENT_DRAFT_SCREENSHOT_DIR'];
      Future<void> capture(String name) async {
        if (initialPath == null) return;
        final boundary = tester.renderObject<RenderRepaintBoundary>(
          find.byKey(boundaryKey),
        );
        await tester.runAsync(() async {
          final pixels = await boundary.toImage(pixelRatio: 1.5);
          final bytes = await pixels.toByteData(format: ui.ImageByteFormat.png);
          expect(bytes, isNotNull);
          final output = File(
            '$initialPath/WIDGET_MOCK_NOT_DEVICE_${brightness.name}_$name.png',
          );
          await output.parent.create(recursive: true);
          await output.writeAsBytes(bytes!.buffer.asUint8List());
          pixels.dispose();
        });
      }

      await capture('initial');
      await _tap(tester, '保存私人草稿');
      expect(find.text('请选择参与方式，不会根据城市推断线上或线下。'), findsOneWidget);
      await tester.ensureVisible(find.text('保存私人草稿'));
      final rect = tester.getRect(find.widgetWithText(FilledButton, '保存私人草稿'));
      expect(rect.top, greaterThanOrEqualTo(0));
      expect(rect.bottom, lessThanOrEqualTo(620));
      await capture('save_reachable');
      final semantics = tester.ensureSemantics();
      expect(find.bySemanticsLabel(RegExp('参与方式')), findsWidgets);
      semantics.dispose();
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      client.close();
    });
  }
}
