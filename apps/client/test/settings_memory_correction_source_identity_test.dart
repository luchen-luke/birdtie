import 'package:flutter/foundation.dart' show debugPrintSynchronously;
import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_memory_correction_page.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'agent_memory_correction_api_test.dart';
import 'agent_memory_correction_page_test.dart' show correctionSettle;

class _MemoryWire extends http.BaseClient {
  _MemoryWire(this.at);
  final DateTime at;
  final sent = <({String method, String host, String path, String body})>[];
  bool closed = false;
  Map<String, dynamic>? input;
  String? delayedPath;
  Completer<http.StreamedResponse>? delayed;

  http.StreamedResponse response(Object data, [int status = 200]) =>
      http.StreamedResponse(
        Stream.value(utf8.encode(jsonEncode(data))), status,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );

  @override
  Future<http.StreamedResponse> send(http.BaseRequest r) {
    // Synchronous real dispatch boundary; not an async MockClient callback.
    final body = r is http.Request ? r.body : '';
    sent.add((method: r.method, host: r.url.host, path: r.url.path, body: body));
    if (r.method == 'POST' && r.url.path == delayedPath) return delayed!.future;
    if (r.url.path == '/v1/me/agent-memories') {
      return Future.value(response({'data': [correctionMemoryRaw(at: at)]}));
    }
    if (r.url.path == '/v1/me/agent-context/self-review') {
      // This slice tests dispatch admission. It does not re-date the earlier
      // immutable SelfReview wire or pretend to test native successful reads.
      return Future.value(response({'error': {'code': 'unavailable'}}, 503));
    }
    if (r.url.path == '/v1/me/agent-memory-corrections/previews') {
      input = Map<String, dynamic>.from(jsonDecode(body));
      return Future.value(response(correctionPreviewRaw(input: input, at: at)));
    }
    if (r.url.path.endsWith('/confirm')) {
      return Future.value(response(correctionReceiptRaw(input: input, at: at)));
    }
    return Future.value(response({'data': []}));
  }

  @override
  void close() { closed = true; super.close(); }
}

class _Scenario {
  _Scenario() : at = DateTime.now().toUtc();
  final DateTime at;
  final auth = SeedTestAuth()..owner = correctionOwner;
  final city = PublicCityController();
  late final wire = _MemoryWire(at), otherWire = _MemoryWire(at);
  late final source = ValueNotifier<(http.Client, String)>((wire, 'http://memory-a.test'));
  late final moments = PrivateMomentController(
    authorizationHeader: () => auth.authorizationHeader,
    client: wire, apiBaseUrl: 'http://memory-a.test',
  );
  Iterable<({String method, String host, String path, String body})> get posts =>
      [...wire.sent, ...otherWire.sent].where((r) => r.method == 'POST');
  void dispose() {
    moments.dispose(); city.dispose(); auth.dispose(); source.dispose();
    wire.close(); otherWire.close();
  }
}

Widget _home(_Scenario s) => MaterialApp(
  home: ValueListenableBuilder<(http.Client, String)>(
    valueListenable: s.source,
    builder: (_, source, _) => Scaffold(body: SettingsPage(
      key: const ValueKey('same-memory-settings'), auth: s.auth, city: s.city,
      moments: s.moments, client: source.$1, apiBaseUrl: source.$2,
    )),
  ),
);

Future<void> _show(WidgetTester t, Finder target) async {
  if (target.evaluate().isEmpty) {
    await t.scrollUntilVisible(target, 160, maxScrolls: 50,
      scrollable: find.byType(Scrollable).first);
  }
  await t.ensureVisible(target);
  await correctionSettle(t);
  expect(target.hitTestable(), findsOneWidget);
}

Future<void> _open(WidgetTester t, _Scenario s) async {
  FlutterSecureStorage.setMockInitialValues({});
  await t.pumpWidget(_home(s));
  await correctionSettle(t);
  final entry = find.widgetWithText(ListTile, '管理我的记忆');
  await _show(t, entry);
  await t.tap(entry);
  await correctionSettle(t);
  expect(find.byType(AgentMemoryCorrectionPage), findsOneWidget);
  expect(find.text('我偏好徒步活动'), findsOneWidget);
}

Future<void> _switchOneFrame(WidgetTester t, _Scenario s, String change) async {
  s.source.value = change == 'client'
      ? (s.otherWire, 'http://memory-a.test')
      : (s.wire, 'http://memory-b.test');
  await t.pump(); // Exactly one parent update. No settle/extra pump before tap.
  final settings = t.widget<SettingsPage>(find.byType(SettingsPage, skipOffstage: false));
  expect(settings.client, same(s.source.value.$1));
  expect(settings.apiBaseUrl, s.source.value.$2);
}

Finder _target(String stage) => stage == 'read'
    ? find.widgetWithText(TextButton, '核对活动偏好与这条记忆')
    : find.widgetWithText(FilledButton, stage == 'preview'
        ? '检查具体纠正版本' : '确认此版本的纠正');

Future<void> _prepare(WidgetTester t, String stage) async {
  if (stage != 'read') {
    final edit = find.widgetWithText(TextButton, '修改这条记忆');
    await _show(t, edit);
    await t.tap(edit);
    await correctionSettle(t);
    await t.enterText(find.byType(TextField), '本人当前修订');
    if (stage == 'confirm') {
      await _show(t, _target('preview'));
      await t.tap(_target('preview'));
      await correctionSettle(t);
      expect(find.text('这是具体版本的纠正草稿；尚未执行。请检查全部影响后确认。'), findsOneWidget);
    }
  }
  await _show(t, _target(stage));
}

void main() {
  final scenarios = <MapEntry<String, Future<void> Function(WidgetTester)>>[];
  void scenario(String name, Future<void> Function(WidgetTester) run) {
    scenarios.add(MapEntry(name, run));
  }
  scenario('设置记忆来源边界：client单帧退休后实际同体按钮不向旧来源POST', (t) async {
    final s = _Scenario();
    await _open(t, s);
    final button = find.widgetWithText(TextButton, '核对活动偏好与这条记忆');
    await _show(t, button);
    expect(s.posts, isEmpty);
    await _switchOneFrame(t, s, 'client');
    expect(find.byType(AgentMemoryCorrectionPage), findsOneWidget);
    expect(button.hitTestable(), findsOneWidget);
    await t.tap(button); // Standard visible gesture; no callback/controller hook.
    await t.idle(); // Allow async completion, without another rendering frame.
    final dispatched = s.posts.toList();
    debugPrintSynchronously(({'synchronousDispatchAfterParentRetirement': [
      for (final r in dispatched) {'method': r.method, 'host': r.host, 'path': r.path, 'body': r.body},
    ]}).toString());
    await t.pumpWidget(const SizedBox());
    s.dispose();
    expect(dispatched, isEmpty);
  });

  for (final stage in ['read', 'preview', 'confirm']) {
    for (final change in ['client', 'base']) {
      if (stage == 'read' && change == 'client') continue; // Actual RED above.
      scenario('设置记忆来源边界：$change单帧退休后实际$stage按钮无新POST', (t) async {
        final s = _Scenario();
        await _open(t, s);
        await _prepare(t, stage);
        final before = s.posts.length;
        expect(before, stage == 'confirm' ? 1 : 0);
        final references = await const FlutterSecureStorage().readAll();
        await _switchOneFrame(t, s, change);
        final button = _target(stage);
        expect(find.byType(AgentMemoryCorrectionPage), findsOneWidget);
        expect(button.hitTestable(), findsOneWidget);
        await t.tap(button);
        await t.idle(); // No additional frame before the actual dispatch oracle.
        final after = s.posts.length;
        expect(await const FlutterSecureStorage().readAll(), references);
        await t.pumpAndSettle();
        expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        s.dispose();
        expect(after, before);
      });
    }
  }

  scenario('设置记忆来源边界：当前来源同体只读503保留原记忆且不执行纠正', (t) async {
    final s = _Scenario();
    await _open(t, s);
    await _show(t, _target('read'));
    await t.tap(_target('read'));
    await correctionSettle(t);
    expect(s.posts.length, 1);
    expect(s.posts.single.path, '/v1/me/agent-context/self-review');
    expect(s.posts.single.host, 'memory-a.test');
    expect(jsonDecode(s.posts.single.body), {
      'profileFields': ['preferredActivityTypes'],
      'memoryIds': [correctionMemory], 'policyFamilies': [],
    });
    expect(find.text('暂时无法核对这些来源；当前记忆和更正批准未改变。'), findsOneWidget);
    expect(find.text('我偏好徒步活动'), findsOneWidget);
    expect(find.text('修改这条记忆'), findsOneWidget);
    expect(await const FlutterSecureStorage().readAll(), isEmpty);
    await t.pumpWidget(const SizedBox());
    s.dispose();
  });

  scenario('设置记忆来源边界：当前来源保留原具体预览确认与回执核实协议', (t) async {
    final s = _Scenario();
    await _open(t, s);
    await _prepare(t, 'confirm');
    final reference = await const FlutterSecureStorage().readAll();
    expect(reference.length, 1);
    expect(jsonDecode(reference.values.single)['phase'], 'PREVIEW');
    await t.tap(_target('confirm'));
    await correctionSettle(t);
    expect(s.posts.length, 2);
    expect(s.posts.first.path, '/v1/me/agent-memory-corrections/previews');
    expect(s.posts.last.path, '/v1/me/agent-memory-corrections/${s.wire.input!['id']}/confirm');
    expect(jsonDecode(s.posts.last.body), {'planDigest': correctionPlan});
    expect(find.text('原纠正已完成，当前对应记忆仍与该回执一致。请重新读取当前记忆。'), findsOneWidget);
    expect(await const FlutterSecureStorage().readAll(), isEmpty);
    await t.pumpWidget(const SizedBox());
    expect(s.wire.closed, isFalse);
    s.dispose();
  });

  scenario('设置记忆来源边界：已发确认晚回不清原核实引用或重发，父来源ABA不复活', (t) async {
    final s = _Scenario();
    await _open(t, s);
    await _prepare(t, 'confirm');
    s.wire.delayedPath = '/v1/me/agent-memory-corrections/${s.wire.input!['id']}/confirm';
    s.wire.delayed = Completer<http.StreamedResponse>();
    await t.tap(_target('confirm'));
    await t.idle();
    expect(s.posts.length, 2); // Already dispatched before retirement, not cancelled.
    final reference = await const FlutterSecureStorage().readAll();
    expect(reference.length, 1);
    expect(jsonDecode(reference.values.single)['phase'], 'CONFIRM');
    await _switchOneFrame(t, s, 'client');
    await t.pumpAndSettle();
    s.wire.delayed!.complete(s.wire.response(correctionReceiptRaw(input: s.wire.input, at: s.at)));
    await t.idle();
    await t.pumpAndSettle();
    s.source.value = (s.wire, 'http://memory-a.test');
    await t.pumpAndSettle();
    expect(find.byType(AgentMemoryCorrectionPage), findsNothing);
    expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
    expect(find.textContaining('原纠正已完成'), findsNothing);
    expect(s.posts.length, 2);
    expect(s.otherWire.sent.where((r) => r.method == 'POST'), isEmpty);
    expect(await const FlutterSecureStorage().readAll(), reference);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    s.dispose();
  });

  // The original SecurePendingStore serial tail survives widget test zones.
  // One zone keeps the real Settings/store fixture without a production reset.
  testWidgets('设置记忆来源边界：原九场景同一测试区顺序核验', (t) async {
    expect(scenarios, hasLength(9));
    for (final item in scenarios) {
      debugPrintSynchronously(({'scenarioStart': item.key}).toString());
      await item.value(t);
      debugPrintSynchronously(({'scenarioPass': item.key}).toString());
    }
  });
}
