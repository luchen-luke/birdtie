import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/authenticated_client.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/content/moment_control.dart';
import 'package:birdtie_client/src/content/moment_control_pending_store.dart';
import 'moment_control_test.dart' show mcPreview, mcReceipt;
import 'moment_control_navigation_test.dart' show mcSettle;
import 'package:birdtie_client/src/legacy/legacy_shell.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'now_scope_recovery_test.dart' show NowFixtureCity;

class _BorrowedClient extends MockClient {
  _BorrowedClient(super.handler);
  bool closed = false;
  @override
  void close() {
    closed = true;
    super.close();
  }
}

// Uses the real existing private Moment writer over synthetic HTTP. Opening a
// draft is not a publication and never creates a social intent or Agent task.
class _CreationFixture {
  _CreationFixture() {
    client = _BorrowedClient((r) async {
      calls.add(r);
      if (r.url.path == '/v1/me/moment-controls/previews') {
        final m = jsonDecode(r.body) as Map<String, dynamic>;
        final input = MomentControlInput(
          operationID: m['operationId'],
          action: m['action'],
          expectedRevision: m['expectedRevision'],
          input: Map<String, Object?>.from(m['input']),
        );
        inputs[input.operationID] = input;
        return _json(mcPreview(input));
      }
      if (r.method == 'PUT') {
        final pending = pendingWrite;
        pendingWrite = null;
        if (pending != null) return pending.future;
        if (writeStatus != 201) return _json({}, status: writeStatus);
        return created(r);
      }
      if (r.url.path.startsWith('/v1/me/moment-controls/operations/')) {
        final op = r.url.path.split('/')[5], input = inputs[op]!;
        return _json(mcReceipt(input));
      }
      if (r.url.path == '/v1/me/moments') return _json(rows);
      if (r.url.path == '/v1/me/social-communities') {
        if (pendingContexts case final pending?) {
          pendingContexts = null;
          return pending.future;
        }
        return _json([], status: contextStatus);
      }
      return _json([]);
    });
    moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      ownerID: () => auth.accountID,
      identityChanges: auth,
      client: client,
      apiBaseUrl: 'http://moment-entry.test',
      pendingStore: MemoryMomentControlPendingStore(),
    );
  }

  final auth = SeedTestAuth()
    ..token = 'Bearer SYNTHETIC-MOMENT-A'
    ..owner = '82000000-0000-4000-8000-000000000001';
  final city = NowFixtureCity();
  final workspace = ValueNotifier<String?>(null);
  final source = ValueNotifier<int>(0);
  final navigator = GlobalKey<NavigatorState>();
  final calls = <http.Request>[];
  final results = <bool?>[];
  final inputs = <String, MomentControlInput>{};
  final rows = <Map<String, Object?>>[];
  late final _BorrowedClient client;
  http.Client? transport;
  late final PrivateMomentController moments;
  Completer<http.Response>? pendingWrite;
  Completer<http.Response>? pendingContexts;
  int writeStatus = 201;
  int contextStatus = 200;
  bool current = true;
  List<http.Request> get writes =>
      calls.where((r) => r.method == 'PUT').toList();

  http.Response _json(Object data, {int status = 200}) => http.Response(
    jsonEncode({'data': data}),
    status,
    headers: {'content-type': 'application/json; charset=utf-8'},
  );

  http.Response created(http.Request r) {
    final m = jsonDecode(r.body) as Map<String, dynamic>,
        input = inputs[m['operationId']]!;
    rows.add({
      ...input.input!,
      'id': '82000000-0000-4000-8000-000000000003',
      'authorAccountId': auth.accountID,
      'visibility': 'private',
      'status': 'draft',
      'revision': 1,
    });
    final receipt = mcReceipt(input)
      ..['momentId'] = '82000000-0000-4000-8000-000000000003';
    return _json(receipt);
  }

  Future<void> mount(WidgetTester t) async {
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    await t.pumpWidget(
      MaterialApp(
        navigatorKey: navigator,
        home: Builder(
          builder: (context) => Scaffold(
            body: Column(
              children: [
                const Text('原 Now 未发送草稿'),
                TextButton(
                  key: const Key('open-current-moment'),
                  onPressed: () => unawaited(
                    showPrivateMomentDraft(
                      context,
                      auth: auth,
                      city: city,
                      moments: moments,
                      client: transport ?? client,
                      apiBaseUrl: 'http://moment-entry.test',
                      workspaceChanges: Listenable.merge([workspace, source]),
                      organizationWorkspaceID: () => workspace.value,
                      current: () => current,
                    ).then(results.add),
                  ),
                  child: const Text('动态'),
                ),
              ],
            ),
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
  }

  Future<void> open(WidgetTester t) async {
    await t.tap(find.byKey(const Key('open-current-moment')));
    await mcSettle(t);
  }

  Future<void> fill(WidgetTester t) async {
    await t.enterText(find.byType(TextFormField).first, '可检查的私人动态');
    await t.enterText(find.byType(TextFormField).last, '保留真实正文');
  }

  Future<void> save(WidgetTester t) async {
    await tapSheet(t, '保存私人草稿');
    await mcSettle(t);
    expect(find.text('检查私人草稿'), findsOneWidget);
    await t.tap(find.text('确认保存'));
    await t.pump();
    await t.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 30)),
    );
    await t.pump();
  }

  Future<void> tapSheet(WidgetTester t, String label) async {
    final target = find.text(label);
    final scrollable = find
        .descendant(
          of: find.byType(BottomSheet),
          matching: find.byType(Scrollable),
        )
        .first;
    for (var i = 0; i < 8 && target.hitTestable().evaluate().isEmpty; i++) {
      await t.drag(scrollable, const Offset(0, -220));
      await t.pumpAndSettle();
    }
    await t.ensureVisible(target);
    await t.pumpAndSettle();
    expect(target.hitTestable(), findsOneWidget);
    await t.tap(target);
    await t.pump();
  }

  void pushUnrelated() => unawaited(
    navigator.currentState!.push<void>(
      MaterialPageRoute(builder: (_) => const Scaffold(body: Text('另一个真实页面'))),
    ),
  );

  void dispose() {
    moments.dispose();
    transport?.close();
    client.close();
    workspace.dispose();
    source.dispose();
    city.dispose();
    auth.dispose();
  }
}

void main() {
  testWidgets('动态入口打开原私人编辑器；仅明确保存写 Moment，不发布或查询', (t) async {
    final f = _CreationFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await f.open(t);
    expect(find.text('新建动态'), findsOneWidget);
    expect(find.textContaining('仅自己可见 · 本页不会发布'), findsOneWidget);
    expect(f.calls, isEmpty);
    await f.fill(t);
    await f.save(t);
    await t.pumpAndSettle();
    expect(f.writes, hasLength(1));
    final request = f.writes.single;
    expect(request.method, 'PUT');
    expect(request.url.path, startsWith('/v1/me/moment-controls/operations/'));
    expect(
      f.calls.where((r) => r.url.path.endsWith('/previews')),
      hasLength(1),
    );
    expect(jsonDecode(request.body)['input'], containsPair('cityId', 'alpha'));
    expect(
      jsonDecode(request.body)['input'],
      containsPair('title', '可检查的私人动态'),
    );
    expect(jsonDecode(request.body)['input'], containsPair('body', '保留真实正文'));
    expect(
      jsonDecode(request.body)['input'],
      containsPair('timePrecision', 'unknown'),
    );
    expect(f.results, [true]);
    expect(find.text('原 Now 未发送草稿'), findsOneWidget);
  });

  testWidgets('访客或未绑定当前来源不打开私人创建、不请求身份数据', (t) async {
    final f = _CreationFixture();
    addTearDown(f.dispose);
    f.auth.token = null;
    await f.mount(t);
    await f.open(t);
    expect(find.text('新建动态'), findsNothing);
    expect(f.calls, isEmpty);
    f.auth.changeIdentity(
      'Bearer SYNTHETIC-MOMENT-A',
      nextOwner: '82000000-0000-4000-8000-000000000001',
    );
    f.current = false;
    await f.open(t);
    expect(find.text('新建动态'), findsNothing);
    expect(f.writes, isEmpty);
  });

  for (final boundary in ['ownerABA', 'workspaceABA', 'cityABA', 'source']) {
    testWidgets('$boundary 同步退休私人草稿，不能因返回原值复活保存', (t) async {
      final f = _CreationFixture();
      addTearDown(f.dispose);
      await f.mount(t);
      await f.open(t);
      await f.fill(t);
      final oldSave = t
          .widget<FilledButton>(
            find.ancestor(
              of: find.text('保存私人草稿'),
              matching: find.byType(FilledButton),
            ),
          )
          .onPressed!;
      switch (boundary) {
        case 'ownerABA':
          f.auth.changeIdentity('Bearer SYNTHETIC-MOMENT-B');
          f.auth.changeIdentity(
            'Bearer SYNTHETIC-MOMENT-A',
            nextOwner: '82000000-0000-4000-8000-000000000001',
          );
        case 'workspaceABA':
          f.workspace.value = '82000000-0000-4000-8000-000000000002';
          f.workspace.value = null;
        case 'cityABA':
          f.city.selectCity('beta');
          f.city.selectCity('alpha');
        case 'source':
          f.current = false;
          f.source.value++;
          f.current = true;
      }
      // Exercise an already captured UI callback before the closing frame.
      oldSave();
      await t.pumpAndSettle();
      expect(find.text('新建动态'), findsNothing);
      expect(f.writes, isEmpty);
      expect(find.text('原 Now 未发送草稿'), findsOneWidget);
      expect(t.takeException(), isNull);
    });
  }

  testWidgets('来源退休只移除自有草稿 route，不弹出其上另一个页面', (t) async {
    final f = _CreationFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await f.open(t);
    f.pushUnrelated();
    await t.pumpAndSettle();
    f.current = false;
    f.source.value++;
    await t.pumpAndSettle();
    expect(find.text('另一个真实页面'), findsOneWidget);
    f.navigator.currentState!.pop();
    await t.pumpAndSettle();
    expect(find.text('新建动态'), findsNothing);
    expect(find.text('原 Now 未发送草稿'), findsOneWidget);
    expect(f.writes, isEmpty);
  });

  testWidgets('已提交保存晚返回不能关闭另一个页面或恢复旧草稿', (t) async {
    final f = _CreationFixture();
    addTearDown(f.dispose);
    final pending = Completer<http.Response>();
    f.pendingWrite = pending;
    await f.mount(t);
    await f.open(t);
    await f.fill(t);
    await f.save(t);
    expect(f.writes, hasLength(1));
    f.pushUnrelated();
    await t.pumpAndSettle();
    f.current = false;
    f.source.value++;
    await t.pumpAndSettle();
    pending.complete(f.created(f.writes.single));
    await t.pumpAndSettle();
    expect(find.text('另一个真实页面'), findsOneWidget);
    expect(f.writes, hasLength(1));
    f.navigator.currentState!.pop();
    await t.pumpAndSettle();
    expect(find.text('新建动态'), findsNothing);
    expect(f.results, [null]);
  });

  testWidgets('不确定旧保存须读核查并明确再次确认，返回检查不新增草稿', (t) async {
    final f = _CreationFixture();
    addTearDown(f.dispose);
    f.writeStatus = 503;
    await f.mount(t);
    await f.open(t);
    await f.fill(t);
    await f.save(t);
    await t.pumpAndSettle();
    expect(f.writes, hasLength(1));
    expect(f.moments.creationUncertain, isTrue);
    expect(find.text('待核实的原动态操作'), findsOneWidget);
    await f.tapSheet(t, '核实原结果');
    await mcSettle(t);
    expect(f.moments.operationReceipt!.succeeded, isTrue);
    expect(f.writes, hasLength(1));
    expect(f.moments.pendingOperations, isEmpty);
    f.writeStatus = 201;
    await f.fill(t);
    await f.save(t);
    await t.pumpAndSettle();
    expect(f.writes, hasLength(2));
    expect(f.results, [true]);
  });

  testWidgets('社群组织只按需读取，复用指定源和借用client且关闭草稿不关闭连接', (t) async {
    final f = _CreationFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await f.open(t);
    expect(f.calls, isEmpty);
    await f.tapSheet(t, '选择我加入的社群或组织');
    await t.pumpAndSettle();
    expect(f.calls.map((r) => r.url.path), [
      '/v1/me/social-communities',
      '/v1/me/organizations',
    ]);
    for (final request in f.calls) {
      expect(request.method, 'GET');
      expect(request.url.host, 'moment-entry.test');
      expect(request.headers['Authorization'], f.auth.authorizationHeader);
    }
    expect(f.writes, isEmpty);
    f.navigator.currentState!.pop();
    await t.pumpAndSettle();
    expect(f.client.closed, false);
    await f.open(t);
    expect(find.text('新建动态'), findsOneWidget);
    expect(f.calls, hasLength(2));
  });

  testWidgets('按需上下文401走共用认证边界一次，不继续读取组织或自动保存', (t) async {
    final f = _CreationFixture();
    addTearDown(f.dispose);
    f.contextStatus = 401;
    var recoveries = 0;
    f.transport = AuthenticatedClient(
      auth: f.auth,
      client: f.client,
      onUnauthorized: (_) async {
        recoveries++;
        f.auth.changeIdentity(null);
      },
    );
    await f.mount(t);
    await f.open(t);
    await f.tapSheet(t, '选择我加入的社群或组织');
    await t.pumpAndSettle();
    expect(recoveries, 1);
    expect(
      f.calls.where((r) => r.url.path == '/v1/me/social-communities'),
      hasLength(1),
    );
    expect(f.calls.where((r) => r.url.path == '/v1/me/organizations'), isEmpty);
    expect(f.writes, isEmpty);
    expect(find.text('新建动态'), findsNothing);
    expect(find.text('原 Now 未发送草稿'), findsOneWidget);
    expect(f.client.closed, false);
  });
}
