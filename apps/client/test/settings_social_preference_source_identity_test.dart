import 'package:flutter/foundation.dart' show debugPrintSynchronously;
import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/content/social_preference_seed_sheet.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'social_preference_seed_controller_test.dart' show socialJson;
import 'social_preference_seed_sheet_test.dart' show SocialTestAuth;

class _PreferenceWire extends http.BaseClient {
  final sent = <({String method, String host, String? token, String body})>[];
  Map<String, dynamic> record = socialJson();
  Completer<http.StreamedResponse>? pendingPut;
  bool closed = false;

  http.StreamedResponse response(Object data, [int status = 200]) =>
      http.StreamedResponse(
        Stream.value(utf8.encode(jsonEncode({'data': data}))),
        status,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) {
    // Real synchronous send boundary, before the returned response Future.
    final body = request is http.Request ? request.body : '';
    if (request.url.path == '/v1/me/agent-private-profile') {
      sent.add((
        method: request.method,
        host: request.url.host,
        token: request.headers['Authorization'],
        body: body,
      ));
      if (request.method == 'PUT') {
        if (pendingPut != null) return pendingPut!.future;
        final value = jsonDecode(body) as Map<String, dynamic>;
        record = socialJson(
          version: ((record['profile'] as Map)['profileVersion'] as int) + 1,
          fields: value['fields'] as Map<String, dynamic>,
        );
      }
      return Future.value(response(record));
    }
    return Future.value(response([]));
  }

  @override
  void close() {
    closed = true;
    super.close();
  }
}

class _Scenario {
  final auth = SocialTestAuth(), otherAuth = SocialTestAuth();
  final city = PublicCityController();
  final wire = _PreferenceWire(), otherWire = _PreferenceWire();
  late final source = ValueNotifier<(SocialTestAuth, http.Client, String)>((
    auth,
    wire,
    'http://preference-a.test',
  ));
  late final moments = PrivateMomentController(
    authorizationHeader: () => auth.authorizationHeader,
    client: wire,
    apiBaseUrl: 'http://preference-a.test',
  );
  Iterable<({String method, String host, String? token, String body})>
  get writes =>
      [...wire.sent, ...otherWire.sent].where((x) => x.method == 'PUT');

  void dispose() {
    moments.dispose();
    city.dispose();
    auth.dispose();
    otherAuth.dispose();
    source.dispose();
    wire.close();
    otherWire.close();
  }
}

Widget _home(_Scenario s) => MaterialApp(
  home: ValueListenableBuilder<(SocialTestAuth, http.Client, String)>(
    valueListenable: s.source,
    builder: (_, source, _) => Scaffold(
      body: SettingsPage(
        key: const ValueKey('same-settings'),
        auth: source.$1,
        city: s.city,
        moments: s.moments,
        client: source.$2,
        apiBaseUrl: source.$3,
      ),
    ),
  ),
);

Future<void> _tap(WidgetTester t, Finder target) async {
  await t.ensureVisible(target);
  await t.pump();
  expect(target.hitTestable(), findsOneWidget);
  await t.tap(target);
  await t.pumpAndSettle();
}

Future<void> _open(WidgetTester t) async {
  final target = find.widgetWithText(ListTile, '社交偏好');
  await t.scrollUntilVisible(
    target,
    180,
    maxScrolls: 50,
    scrollable: find.byType(Scrollable).last,
  );
  await _tap(t, target);
  expect(find.byType(SocialPreferenceSeedSheet), findsOneWidget);
  expect(find.text('原有明确描述'), findsOneWidget);
}

Future<void> _review(WidgetTester t) async {
  await _tap(t, find.widgetWithText(FilterChip, '小群体'));
  await _tap(t, find.widgetWithText(FilledButton, '检查选择'));
  expect(find.text('私密偏好：原有明确描述、小群体'), findsOneWidget);
  final save = find.widgetWithText(FilledButton, '保存私密偏好');
  expect(save.hitTestable(), findsOneWidget);
  expect(t.widget<FilledButton>(save).onPressed, isNotNull);
}

Future<void> _close(WidgetTester t, _Scenario s) async {
  await t.pumpWidget(const SizedBox());
  s.dispose();
}

void main() {
  testWidgets('设置偏好来源边界：本人正常保存，新来源重新打开可保存新选择', (t) async {
    final s = _Scenario();
    await t.pumpWidget(_home(s));
    await t.pumpAndSettle();
    await _open(t);
    expect(s.wire.sent.where((x) => x.method == 'GET').length, 1);
    await _review(t);
    await _tap(t, find.widgetWithText(FilledButton, '保存私密偏好'));
    expect(find.byType(SocialPreferenceSeedSheet), findsNothing);
    expect(s.writes.length, 1);
    expect(s.writes.single.host, 'preference-a.test');
    expect(s.writes.single.token, 'Bearer owner');
    expect(jsonDecode(s.writes.single.body)['expectedVersion'], 2);
    expect(s.wire.closed, isFalse);
    s.source.value = (s.auth, s.otherWire, 'http://preference-b.test');
    await t.pumpAndSettle();
    await _open(t);
    expect(
      t.widget<FilterChip>(find.widgetWithText(FilterChip, '小群体')).selected,
      isFalse,
    );
    await _tap(t, find.widgetWithText(FilterChip, '共同兴趣'));
    await _tap(t, find.widgetWithText(FilledButton, '检查选择'));
    await _tap(t, find.widgetWithText(FilledButton, '保存私密偏好'));
    expect(s.writes.length, 2);
    expect(s.writes.last.host, 'preference-b.test');
    expect(jsonDecode(s.writes.last.body)['fields']['socialPreferences'], [
      '原有明确描述',
      '共同兴趣',
    ]);
    expect(s.otherWire.closed, isFalse);
    expect(t.takeException(), isNull);
    await _close(t, s);
  });

  for (final change in ['client', 'base', 'source-aba', 'auth-instance']) {
    testWidgets('设置偏好来源边界：$change 退休旧检查，不向旧来源PUT', (t) async {
      final s = _Scenario();
      await t.pumpWidget(_home(s));
      await t.pumpAndSettle();
      await _open(t);
      await _review(t);
      expect(s.wire.sent.where((x) => x.method == 'GET').length, 1);
      s.source.value = switch (change) {
        'client' => (s.auth, s.otherWire, 'http://preference-a.test'),
        'base' => (s.auth, s.wire, 'http://preference-b.test'),
        'auth-instance' => (s.otherAuth, s.wire, 'http://preference-a.test'),
        _ => (s.auth, s.otherWire, 'http://preference-b.test'),
      };
      await t.pumpAndSettle();
      final settings = t.widget<SettingsPage>(
        find.byType(SettingsPage, skipOffstage: false),
      );
      expect(settings.client, same(s.source.value.$2));
      expect(settings.apiBaseUrl, s.source.value.$3);
      expect(settings.auth, same(s.source.value.$1));
      if (change == 'source-aba') {
        s.source.value = (s.auth, s.wire, 'http://preference-a.test');
        await t.pumpAndSettle();
      }
      final oldSave = find.widgetWithText(FilledButton, '保存私密偏好');
      if (oldSave.evaluate().isNotEmpty) {
        // Actual old button only; no force tap or callback invocation.
        await _tap(t, oldSave);
      }
      debugPrintSynchronously(({
        'change': change,
        'synchronousPUTs': [
          for (final x in s.writes)
            {'host': x.host, 'header': x.token, 'body': x.body},
        ],
      }).toString());
      final writes = s.writes.toList();
      final retired = find.text('工作身份或来源已变化，请返回当前入口重新核实。').evaluate().length;
      final sheets = find.byType(SocialPreferenceSeedSheet).evaluate().length;
      expect(t.takeException(), isNull);
      await _close(t, s);
      expect(writes, isEmpty);
      expect(retired, 1);
      expect(sheets, 0);
    });
  }

  for (final status in [200, 503]) {
    testWidgets('设置偏好来源边界：已发PUT迟到$status 不弹回或污染当前入口', (t) async {
      final s = _Scenario();
      final pending = Completer<http.StreamedResponse>();
      s.wire.pendingPut = pending;
      await t.pumpWidget(_home(s));
      await t.pumpAndSettle();
      await _open(t);
      await _review(t);
      await _tap(t, find.widgetWithText(FilledButton, '保存私密偏好'));
      expect(s.writes.length, 1); // Already sent A; no claim of cancellation.
      final body = jsonDecode(s.writes.single.body) as Map<String, dynamic>;
      s.source.value = (s.auth, s.otherWire, 'http://preference-b.test');
      await t.pumpAndSettle();
      pending.complete(
        s.wire.response(
          socialJson(
            version: 3,
            fields: body['fields'] as Map<String, dynamic>,
          ),
          status,
        ),
      );
      await t.pumpAndSettle();
      final retired = find.text('工作身份或来源已变化，请返回当前入口重新核实。').evaluate().length;
      final sheets = find.byType(SocialPreferenceSeedSheet).evaluate().length;
      final oldSuccess = find.text('私密社交偏好已保存。').evaluate().length;
      final oldFailure = find.text('保存结果尚未确认，请先读取当前资料核实。').evaluate().length;
      expect(s.writes.length, 1);
      expect(t.takeException(), isNull);
      await _close(t, s);
      expect(retired, 1);
      expect(sheets, 0);
      expect(oldSuccess, 0);
      expect(oldFailure, 0);
    });
  }
}
