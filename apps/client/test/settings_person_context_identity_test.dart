import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/person_contexts_page.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'agent_seed_sheet_test.dart' show SeedTestAuth;

const _a = '91000000-0000-4000-8000-000000000001';
const _b = '91000000-0000-4000-8000-000000000002';
const _oldDraft = '甲账号未发送的合成线上情境';

class _ContextWireClient extends http.BaseClient {
  final requests =
      <
        ({String method, String host, String path, String? token, String body})
      >[];
  final rows = <String, List<Map<String, dynamic>>>{};
  Completer<http.StreamedResponse>? pendingPost;
  bool closed = false;
  http.StreamedResponse response(Object value, [int status = 200]) =>
      http.StreamedResponse(
        Stream.value(utf8.encode(jsonEncode({'data': value}))),
        status,
        headers: {'content-type': 'application/json'},
      );

  @override
  Future<http.StreamedResponse> send(http.BaseRequest r) {
    // Synchronous real dispatch boundary; no MockClient async handler oracle.
    final body = r is http.Request ? r.body : '';
    final token = r.headers['Authorization'];
    requests.add((
      method: r.method,
      host: r.url.host,
      path: r.url.path,
      token: token,
      body: body,
    ));
    if (r.url.path == '/v1/cities') {
      return Future.value(
        response([
          {'id': 'aberdeen-gb', 'name': '阿伯丁'},
        ]),
      );
    }
    if (r.url.path == '/v1/me/contexts') {
      if (r.method == 'GET') return Future.value(response(rows[token] ?? []));
      if (r.method == 'POST') {
        if (pendingPost != null) return pendingPost!.future;
        final data = jsonDecode(body) as Map<String, dynamic>;
        final row = <String, dynamic>{
          'contextId': _a,
          'contextType': data['contextType'],
          'sourceKey': data['sourceKey'],
          'label': data['sourceKey'],
          'relation': data['relation'],
          'visibility': 'private',
        };
        rows.putIfAbsent(token!, () => []).add(row);
        return Future.value(response(row, 201));
      }
    }
    return Future.value(response([]));
  }

  @override
  void close() {
    closed = true;
    super.close();
  }
}

class _ContextScenario {
  final auth = SeedTestAuth()
    ..owner = _a
    ..token = 'Bearer synthetic-A';
  final city = PublicCityController();
  final client = _ContextWireClient(), replacement = _ContextWireClient();
  final org = ValueNotifier<String?>(null);
  late final source = ValueNotifier<(http.Client, String)>((
    client,
    'http://context-a.test',
  ));
  String? workspace() => org.value;
  late final moments = PrivateMomentController(
    authorizationHeader: () => auth.authorizationHeader,
    client: client,
    apiBaseUrl: 'http://context-a.test',
  );
  void toB() => auth.changeIdentity('Bearer synthetic-B', nextOwner: _b);
  void toA() => auth.changeIdentity('Bearer synthetic-A', nextOwner: _a);
  void dispose() {
    moments.dispose();
    city.dispose();
    auth.dispose();
    org.dispose();
    source.dispose();
    client.close();
    replacement.close();
  }
}

Widget _settings(_ContextScenario s) => MaterialApp(
  home: ValueListenableBuilder<(http.Client, String)>(
    valueListenable: s.source,
    builder: (_, source, _) => Scaffold(
      body: SettingsPage(
        key: const ValueKey('same-settings'),
        auth: s.auth,
        city: s.city,
        moments: s.moments,
        client: source.$1,
        apiBaseUrl: source.$2,
        workspaceChanges: s.org,
        organizationWorkspaceID: s.workspace,
      ),
    ),
  ),
);

Future<void> _tapText(WidgetTester t, String text) async {
  final target = find.text(text);
  await t.scrollUntilVisible(
    target,
    180,
    maxScrolls: 50,
    scrollable: find.byType(Scrollable).last,
  );
  await t.ensureVisible(target);
  await t.pump();
  expect(target.hitTestable(), findsOneWidget);
  await t.tap(target);
  await t.pumpAndSettle();
}

Future<void> _open(WidgetTester t, _ContextScenario s) async {
  await t.pumpWidget(_settings(s));
  await t.pumpAndSettle();
  await _tapText(t, '我的生活情境');
  expect(find.byType(PersonContextsPage), findsOneWidget);
}

Future<void> _onlineDraft(WidgetTester t, String text) async {
  await _tapText(t, '当前城市');
  await t.tap(find.text('线上兴趣或社群').last);
  await t.pumpAndSettle();
  expect(find.byType(TextField), findsOneWidget);
  await t.enterText(find.byType(TextField), text);
}

void main() {
  testWidgets('生活情境设置边界：本人正常保存，返回以B重开只保存新草稿', (t) async {
    final s = _ContextScenario();
    addTearDown(s.dispose);
    await _open(t, s);
    await _onlineDraft(t, '甲本人的合成记录');
    await _tapText(t, '保存情境');
    final first = s.client.requests.where((r) => r.method == 'POST').single;
    expect(first.token, 'Bearer synthetic-A');
    expect(first.host, 'context-a.test');
    expect(jsonDecode(first.body), {
      'contextType': 'ONLINE',
      'sourceKey': '甲本人的合成记录',
      'relation': 'interest',
    });
    expect(find.text('甲本人的合成记录'), findsOneWidget);
    await t.pageBack();
    await t.pumpAndSettle();
    s.toB();
    await t.pumpAndSettle();
    await _tapText(t, '我的生活情境');
    await _onlineDraft(t, '乙本人的新合成记录');
    await _tapText(t, '保存情境');
    final posts = s.client.requests.where((r) => r.method == 'POST').toList();
    expect(posts.length, 2);
    expect(posts.last.token, 'Bearer synthetic-B');
    expect(jsonDecode(posts.last.body)['sourceKey'], '乙本人的新合成记录');
    expect(find.text('甲本人的合成记录'), findsNothing);
    expect(find.text('乙本人的新合成记录'), findsOneWidget);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox.shrink());
    expect(s.client.closed, isFalse);
  });

  for (final change in [
    'account',
    'account-aba',
    'organization',
    'source-aba',
  ]) {
    testWidgets('生活情境设置边界：$change退休旧稿，不发旧POST', (t) async {
      final s = _ContextScenario();
      addTearDown(s.dispose);
      await _open(t, s);
      await _onlineDraft(t, _oldDraft);
      if (change == 'organization') {
        s.org.value = '91000000-0000-4000-8000-000000000003';
      } else if (change == 'source-aba') {
        s.source.value = (s.replacement, 'http://context-b.test');
      } else {
        s.toB();
      }
      await t.pumpAndSettle();
      if (change == 'account-aba') {
        s.toA();
        await t.pumpAndSettle();
      }
      if (change == 'source-aba') {
        s.source.value = (s.client, 'http://context-a.test');
        await t.pumpAndSettle();
      }
      // If the real old editor is still live, try its visible actual save.
      // This proves the old body/header dispatch, rather than only missing text.
      if (find.widgetWithText(FilledButton, '保存情境').evaluate().isNotEmpty) {
        await _tapText(t, '保存情境');
      }
      expect(
        [
          ...s.client.requests,
          ...s.replacement.requests,
        ].where((r) => r.method == 'POST'),
        isEmpty,
      );
      expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
      expect(find.byType(PersonContextsPage), findsNothing);
      expect(find.widgetWithText(TextField, _oldDraft), findsNothing);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox.shrink());
      expect(s.client.closed, isFalse);
      expect(s.replacement.closed, isFalse);
    });
  }

  testWidgets('生活情境设置边界：账号退休后下一帧前真实旧按钮不发新token旧稿', (t) async {
    final s = _ContextScenario();
    addTearDown(s.dispose);
    await _open(t, s);
    await _onlineDraft(t, _oldDraft);
    final save = find.widgetWithText(FilledButton, '保存情境');
    await t.ensureVisible(save);
    await t.pump();
    expect(save.hitTestable(), findsOneWidget);
    expect(t.widget<FilledButton>(save).onPressed, isNotNull);
    s.toB();
    // No pump after retirement: the old rendered control is still hit-testable.
    // Real pointer dispatch, not a shadow writer or async handler callback.
    expect(save.hitTestable(), findsOneWidget);
    await t.tap(save);
    final posts = s.client.requests.where((r) => r.method == 'POST').toList();
    await t.pumpWidget(const SizedBox.shrink());
    expect(posts, isEmpty, reason: '退休后的旧按钮不得携乙token发送甲未发送字段：$posts');
    expect(t.takeException(), isNull);
  });

  testWidgets('生活情境设置边界：已发旧POST晚失败不污染新B页面', (t) async {
    final s = _ContextScenario();
    addTearDown(s.dispose);
    s.client.pendingPost = Completer<http.StreamedResponse>();
    await _open(t, s);
    await _onlineDraft(t, _oldDraft);
    final save = find.text('保存情境');
    await t.ensureVisible(save);
    await t.pump();
    expect(save.hitTestable(), findsOneWidget);
    await t.tap(save);
    await t.pump();
    final post = s.client.requests.where((r) => r.method == 'POST').single;
    expect(post.token, 'Bearer synthetic-A');
    expect(jsonDecode(post.body)['sourceKey'], _oldDraft);
    s.toB();
    await t.pumpAndSettle();
    final retiredBeforeResponse = find
        .text('工作身份或来源已变化，请返回当前入口重新核实。')
        .evaluate()
        .isNotEmpty;
    if (retiredBeforeResponse) {
      await t.pageBack();
      await t.pumpAndSettle();
      await _tapText(t, '我的生活情境');
    }
    expect(find.byType(PersonContextsPage), findsOneWidget);
    s.client.pendingPost!.complete(s.client.response({}, 503));
    await t.pumpAndSettle();
    expect(find.text('保存失败，请检查内容后重试。'), findsNothing);
    expect(retiredBeforeResponse, isTrue);
    await _onlineDraft(t, '乙未发送的新合成草稿');
    expect(
      t.widget<TextField>(find.byType(TextField)).controller!.text,
      '乙未发送的新合成草稿',
    );
    expect(s.client.requests.where((r) => r.method == 'POST').length, 1);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox.shrink());
  });
}
