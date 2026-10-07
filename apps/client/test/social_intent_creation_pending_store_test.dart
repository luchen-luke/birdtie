import 'dart:convert';
import 'dart:async';
import 'package:birdtie_client/src/workspace/social_intent_creation_pending_store.dart';
import 'package:birdtie_client/src/workspace/social_intent_drafts.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'social_intent_creation_api_test.dart'
    show
        creationDraft,
        creationOwner,
        creationID,
        creationWire,
        nativeCreationWireSnapshots;

class _UnavailablePendingStore extends MemorySocialIntentCreationPendingStore {
  @override
  Future<void> write(String e, String o, PendingSocialIntentCreation p) async {
    throw StateError('synthetic platform unavailable');
  }
}

Future<void> _tap(WidgetTester tester, String text) async {
  await tester.ensureVisible(find.text(text));
  await tester.tap(find.text(text));
  await tester.pumpAndSettle();
}

Map<String, dynamic> _native(String label) =>
    (jsonDecode(nativeCreationWireSnapshots) as List)
            .cast<Map<String, dynamic>>()
            .singleWhere((v) => v['label'] == label)['wire']
        as Map<String, dynamic>;

PendingSocialIntentCreation _nativePending(Map<String, dynamic> wire) {
  final response =
      jsonDecode(wire['responseBody'] as String) as Map<String, dynamic>;
  final data = response['data'] as Map<String, dynamic>;
  final body =
      jsonDecode(wire['requestBody'] as String) as Map<String, dynamic>;
  final draft = Map<String, dynamic>.from((body['draft'] ?? body) as Map);
  final operation = draft.remove('operationId') as String;
  return PendingSocialIntentCreation.capture(
    environment: 'https://api.test',
    owner: data['ownerAccountId'] as String,
    source: data['sourceTaskId'] as String? ?? '',
    draft: draft,
    operation: operation,
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test('待核实输入深层不可修改，不包含会话token且UUID独立', () {
    final p = PendingSocialIntentCreation.capture(
      environment: 'https://api.test',
      owner: creationOwner,
      source: '',
      draft: creationDraft(),
    );
    expect(
      () => (p.data['draft'] as Map)['title'] = 'changed',
      throwsUnsupportedError,
    );
    expect(p.encoded, isNot(contains('Bearer')));
    expect(newIntentCreationOperation(), isNot(p.operation));
  });
  test('账号和环境隔离，迟到compare-delete不能删除新操作', () async {
    final store = MemorySocialIntentCreationPendingStore(),
        p = PendingSocialIntentCreation.capture(
          environment: 'https://api.test',
          owner: creationOwner,
          source: '',
          draft: creationDraft(),
        );
    await store.write('https://api.test', creationOwner, p);
    expect(await store.read('https://other.test', creationOwner), isNull);
    expect(await store.read('https://api.test', creationID), isNull);
    final newer = PendingSocialIntentCreation.capture(
      environment: 'https://api.test',
      owner: creationOwner,
      source: '',
      draft: creationDraft(title: '另一件事'),
    );
    expect(
      store.write('https://api.test', creationOwner, newer),
      throwsStateError,
    );
    await store.delete('https://api.test', creationOwner, p);
    await store.write('https://api.test', creationOwner, newer);
    expect(
      store.delete('https://api.test', creationOwner, p),
      throwsStateError,
    );
    expect(
      (await store.read('https://api.test', creationOwner))!.operation,
      newer.operation,
    );
  });
  test('安全平台存储写入并回读确认，损坏或未落盘不能静默降级', () async {
    const channel = MethodChannel(
      'plugins.it_nomads.com/flutter_secure_storage',
    );
    final values = <String, String>{};
    bool lose = false;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
          final args = Map<String, dynamic>.from(call.arguments as Map);
          final key = args['key'] as String;
          switch (call.method) {
            case 'read':
              return values[key];
            case 'write':
              if (!lose) values[key] = args['value'] as String;
              return null;
            case 'delete':
              values.remove(key);
              return null;
            default:
              throw StateError('unexpected method');
          }
        });
    addTearDown(
      () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, null),
    );
    const store = SecureSocialIntentCreationPendingStore();
    final p = PendingSocialIntentCreation.capture(
      environment: 'https://api.test',
      owner: creationOwner,
      source: '',
      draft: creationDraft(),
    );
    lose = true;
    await expectLater(
      store.write('https://api.test', creationOwner, p),
      throwsStateError,
    );
    lose = false;
    await store.write('https://api.test', creationOwner, p);
    expect(
      (await store.read('https://api.test', creationOwner))!.operation,
      p.operation,
    );
    values[values.keys.single] = '{"broken":true}';
    expect(store.read('https://api.test', creationOwner), throwsA(anything));
  });
  testWidgets('重启页面先恢复原输入并核查同操作，不自动POST', (tester) async {
    final store = MemorySocialIntentCreationPendingStore(),
        p = PendingSocialIntentCreation.capture(
          environment: 'https://api.test',
          owner: creationOwner,
          source: '',
          draft: creationDraft(title: '重启前待核实的私人想法'),
        );
    await store.write('https://api.test', creationOwner, p);
    final calls = <http.Request>[];
    final client = MockClient((r) async {
      calls.add(r);
      return r.url.path.contains('/social-intent-creations/')
          ? http.Response('{"error":{"code":"not_found"}}', 404)
          : http.Response('{"data":[]}', 200);
    });
    await tester.pumpWidget(
      MaterialApp(
        home: SocialIntentDraftPage(
          authorizationHeader: () => 'Bearer A',
          ownerID: () => creationOwner,
          apiBaseUrl: 'https://api.test',
          client: client,
          pendingStore: store,
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(calls.where((r) => r.method == 'POST'), isEmpty);
    expect(calls.any((r) => r.url.path.endsWith('/${p.operation}')), isTrue);
    expect(
      tester
          .widget<TextField>(find.byKey(const Key('intent-draft-title')))
          .controller!
          .text,
      '重启前待核实的私人想法',
    );
    expect(
      (await store.read('https://api.test', creationOwner))!.operation,
      p.operation,
    );
    await tester.pumpWidget(const SizedBox());
    client.close();
  });

  for (final label in [
    'CREATE',
    'REPLAY',
    'CURRENT_CANCELLED',
    'CURRENT_EXPIRED',
    'SOURCE_NO_EFFECT',
  ]) {
    testWidgets('页面直接读取实际native wire $label，同ID管理而非标题匹配', (tester) async {
      final wire = _native(label), pending = _nativePending(_native(label));
      final data =
          (jsonDecode(wire['responseBody'] as String)
                  as Map<String, dynamic>)['data']
              as Map<String, dynamic>;
      final store = MemorySocialIntentCreationPendingStore();
      await store.write('https://api.test', pending.owner, pending);
      final calls = <http.Request>[];
      String? managed;
      final client = MockClient((r) async {
        calls.add(r);
        return r.url.path.contains('/social-intent-creations/')
            ? http.Response.bytes(
                utf8.encode(wire['responseBody'] as String),
                200,
              )
            : http.Response('{"data":[]}', 200);
      });
      await tester.pumpWidget(
        MaterialApp(
          home: SocialIntentDraftPage(
            authorizationHeader: () => 'Bearer synthetic-current',
            ownerID: () => pending.owner,
            apiBaseUrl: 'https://api.test',
            client: client,
            pendingStore: store,
            onManageIntent: (id) => managed = id,
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(calls.where((r) => r.method == 'POST'), isEmpty);
      expect(
        calls
            .singleWhere(
              (r) => r.url.path.contains('/social-intent-creations/'),
            )
            .url
            .path
            .endsWith('/${pending.operation}'),
        isTrue,
      );
      expect(await store.read('https://api.test', pending.owner), isNull);
      if (label == 'SOURCE_NO_EFFECT') {
        expect(find.textContaining('本次输入没有再次保存'), findsOneWidget);
        expect(find.text('私人草稿已保存'), findsNothing);
        await _tap(tester, '查看此前保存的意图');
        expect(managed, data['priorIntentId']);
      } else {
        final current = data['intent'] as Map<String, dynamic>;
        expect(find.text('想做的事：${current['title']}'), findsOneWidget);
        expect(
          find.text('私人草稿已保存'),
          label.startsWith('CURRENT_') ? findsNothing : findsOneWidget,
        );
        if (label == 'CURRENT_CANCELLED') {
          expect(find.textContaining('当前状态：已取消'), findsOneWidget);
        }
        if (label == 'CURRENT_EXPIRED') {
          expect(find.textContaining('当前状态：已过期'), findsOneWidget);
        }
        await _tap(tester, '查看已保存的意图');
        expect(managed, data['intentId']);
      }
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      client.close();
    });
  }

  testWidgets('安全存储未落盘时零POST，保留当前输入', (tester) async {
    final store = _UnavailablePendingStore(), calls = <http.Request>[];
    final client = MockClient((r) async {
      calls.add(r);
      return http.Response('{"data":[]}', 200);
    });
    await tester.pumpWidget(
      MaterialApp(
        home: SocialIntentDraftPage(
          authorizationHeader: () => 'Bearer synthetic-A',
          ownerID: () => creationOwner,
          apiBaseUrl: 'https://api.test',
          client: client,
          pendingStore: store,
          suggestedTitle: '尚未提交的输入',
          initialModality: 'ONLINE',
        ),
      ),
    );
    await tester.pumpAndSettle();
    await _tap(tester, '保存私人草稿');
    expect(calls.where((r) => r.method == 'POST'), isEmpty);
    expect(find.textContaining('尚未提交'), findsWidgets);
    expect(
      tester
          .widget<TextField>(find.byKey(const Key('intent-draft-title')))
          .controller!
          .text,
      '尚未提交的输入',
    );
    await tester.pumpWidget(const SizedBox());
    client.close();
  });

  for (final code in [400, 500]) {
    testWidgets('无原回执的HTTP$code保持未知；重开GET-only后明确重试只用原key/body', (
      tester,
    ) async {
      final store = MemorySocialIntentCreationPendingStore();
      final writes = <http.Request>[];
      var retry = false;
      final client = MockClient((r) async {
        if (r.method == 'POST') {
          writes.add(r);
          return retry
              ? creationWire(r)
              : http.Response('{"error":{"code":"unconfirmed"}}', code);
        }
        return r.url.path.contains('/social-intent-creations/')
            ? http.Response('{"error":{"code":"not_found"}}', 404)
            : http.Response('{"data":[]}', 200);
      });
      Widget page() => MaterialApp(
        home: SocialIntentDraftPage(
          authorizationHeader: () => 'Bearer synthetic-A',
          ownerID: () => creationOwner,
          apiBaseUrl: 'https://api.test',
          client: client,
          pendingStore: store,
          suggestedTitle: '服务错误后保留原内容',
          initialModality: 'ONLINE',
        ),
      );
      await tester.pumpWidget(page());
      await tester.pumpAndSettle();
      await _tap(tester, '保存私人草稿');
      expect(writes, hasLength(1));
      final captured = (await store.read('https://api.test', creationOwner))!;
      expect(find.textContaining('结果未知'), findsOneWidget);
      await _tap(tester, '保存私人草稿');
      expect(writes, hasLength(1));
      await tester.pumpWidget(const SizedBox());
      await tester.pumpAndSettle();
      await tester.pumpWidget(page());
      await tester.pumpAndSettle();
      expect(writes, hasLength(1));
      expect(find.textContaining('这不代表保存失败'), findsOneWidget);
      expect(
        (await store.read('https://api.test', creationOwner))!.operation,
        captured.operation,
      );
      retry = true;
      await _tap(tester, '重试原保存（内容不变）');
      expect(writes, hasLength(2));
      expect(writes[1].body, writes[0].body);
      expect(writes[1].headers['Authorization'], 'Bearer synthetic-A');
      expect(await store.read('https://api.test', creationOwner), isNull);
      expect(find.text('私人草稿已保存'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
      client.close();
    });
  }

  testWidgets('迟到GET不能覆盖切换后输入；回到本人后原结果与当前输入分开', (tester) async {
    final native = _native('CREATE'),
        pending = _nativePending(_native('CREATE'));
    final store = MemorySocialIntentCreationPendingStore();
    await store.write('https://api.test', pending.owner, pending);
    final notifier = ValueNotifier<String>(pending.owner),
        lateResponse = Completer<http.Response>();
    var gets = 0;
    final calls = <http.Request>[];
    final client = MockClient((r) async {
      calls.add(r);
      if (r.url.path.contains('/social-intent-creations/')) {
        gets++;
        if (gets == 1) return lateResponse.future;
        return http.Response.bytes(
          utf8.encode(native['responseBody'] as String),
          200,
        );
      }
      return http.Response('{"data":[]}', 200);
    });
    await tester.pumpWidget(
      MaterialApp(
        home: AnimatedBuilder(
          animation: notifier,
          builder: (_, _) => SocialIntentDraftPage(
            authorizationHeader: () => 'Bearer synthetic-${notifier.value}',
            authorizationChanges: notifier,
            ownerID: () => notifier.value,
            apiBaseUrl: 'https://api.test',
            client: client,
            pendingStore: store,
          ),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();
    expect(gets, 1);
    notifier.value = creationOwner;
    await tester.pumpAndSettle();
    await tester.enterText(
      find.byKey(const Key('intent-draft-title')),
      '换账号后的未保存输入',
    );
    lateResponse.complete(
      http.Response.bytes(utf8.encode(native['responseBody'] as String), 200),
    );
    await tester.pumpAndSettle();
    expect(
      tester
          .widget<TextField>(find.byKey(const Key('intent-draft-title')))
          .controller!
          .text,
      '换账号后的未保存输入',
    );
    expect(find.text('私人草稿已保存'), findsNothing);
    expect(await store.read('https://api.test', pending.owner), isNotNull);
    notifier.value = pending.owner;
    await tester.pumpAndSettle();
    expect(
      tester
          .widget<TextField>(find.byKey(const Key('intent-draft-title')))
          .controller!
          .text,
      '',
    );
    expect(find.text('此前的保存已核实；当前输入仍未保存。'), findsOneWidget);
    expect(find.text('私人草稿已保存'), findsNothing);
    expect(await store.read('https://api.test', pending.owner), isNull);
    expect(calls.where((r) => r.method == 'POST'), isEmpty);
    await tester.pumpWidget(const SizedBox());
    client.close();
    notifier.dispose();
  });
}
