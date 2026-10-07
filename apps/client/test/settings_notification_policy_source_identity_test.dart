import 'package:flutter/foundation.dart' show debugPrintSynchronously;
import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/notification_policy_page.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'notification_policy_controller_test.dart' show currentPolicy;

// Related widget units with synthetic transport, not native session/CAS proof.
class _PolicyWire extends http.BaseClient {
  final sent = <({String method, String host, String path, String body, String? auth})>[];
  bool closed = false;
  Completer<http.StreamedResponse>? delayedPut;
  http.StreamedResponse response(Object body, [int status = 200]) =>
      http.StreamedResponse(Stream.value(utf8.encode(jsonEncode(body))), status,
        headers: {'content-type': 'application/json; charset=utf-8'});
  @override
  Future<http.StreamedResponse> send(http.BaseRequest r) {
    // Synchronous HTTP dispatch oracle, not the async response handler.
    final body = r is http.Request ? r.body : '';
    sent.add((method: r.method, host: r.url.host, path: r.url.path,
      body: body, auth: r.headers['Authorization']));
    if (r.url.path == '/v1/me/notification-policy') {
      if (r.method == 'PUT') {
        if (delayedPut != null) return delayedPut!.future;
        final input = jsonDecode(body) as Map<String, dynamic>;
        return Future.value(response({'data': currentPolicy(version: 1,
          enabled: input['enabled'] as bool, route: input['defaultRoute'] as String,
          expires: DateTime.parse(input['expiresAt'] as String))}));
      }
      return Future.value(response({'data': currentPolicy()}));
    }
    return Future.value(response({'data': []}));
  }
  @override
  void close() { closed = true; super.close(); }
}

class _Scenario {
  final auth = SeedTestAuth();
  final city = PublicCityController();
  final wire = _PolicyWire(), otherWire = _PolicyWire();
  late final source = ValueNotifier<(http.Client, String)>((wire, 'http://policy-a.test'));
  late final moments = PrivateMomentController(authorizationHeader: () => auth.authorizationHeader,
    client: wire, apiBaseUrl: 'http://policy-a.test');
  Iterable<({String method, String host, String path, String body, String? auth})> get puts =>
      [...wire.sent, ...otherWire.sent].where((r) => r.method == 'PUT');
  void dispose() {
    moments.dispose(); city.dispose(); auth.dispose(); source.dispose();
    wire.close(); otherWire.close();
  }
}

Widget _home(_Scenario s) => MaterialApp(home:
  ValueListenableBuilder<(http.Client, String)>(valueListenable: s.source,
    builder: (_, source, _) => Scaffold(body: SettingsPage(
      key: const ValueKey('same-notification-settings'), auth: s.auth, city: s.city,
      moments: s.moments, client: source.$1, apiBaseUrl: source.$2))));

Future<void> _show(WidgetTester t, Finder target) async {
  if (target.evaluate().isEmpty) {
    await t.scrollUntilVisible(target, 160, maxScrolls: 50,
      scrollable: find.byType(Scrollable).first);
  }
  await t.ensureVisible(target);
  await t.pumpAndSettle();
  expect(target.hitTestable(), findsOneWidget);
}
Future<void> _open(WidgetTester t, _Scenario s) async {
  await t.pumpWidget(_home(s));
  await t.pumpAndSettle();
  final entry = find.widgetWithText(ListTile, '通知设置');
  await _show(t, entry);
  await t.tap(entry);
  await t.pumpAndSettle();
  expect(find.byType(NotificationPolicyPage), findsOneWidget);
}
Future<void> _approveDialog(WidgetTester t) async {
  final edit = find.widgetWithText(SwitchListTile, '使用自定义通知设置');
  await _show(t, edit);
  await t.tap(edit);
  await t.pumpAndSettle();
  final check = find.widgetWithText(FilledButton, '检查并保存');
  await _show(t, check);
  await t.tap(check);
  await t.pumpAndSettle();
  expect(find.byType(AlertDialog), findsOneWidget);
  expect(find.text('本人设置 · 当前版本 0'), findsOneWidget);
  expect(find.widgetWithText(FilledButton, '确认保存').hitTestable(), findsOneWidget);
}

void main() {
  testWidgets('通知父来源：client一帧退休后真实确认弹窗不向旧来源PUT', (t) async {
    final s = _Scenario();
    await _open(t, s);
    await _approveDialog(t);
    expect(s.puts, isEmpty);
    s.source.value = (s.otherWire, 'http://policy-a.test');
    await t.pump(); // Exactly one parent update; no extra frame before tap.
    final settings = t.widget<SettingsPage>(find.byType(SettingsPage, skipOffstage: false));
    expect(settings.client, same(s.otherWire));
    final confirm = find.widgetWithText(FilledButton, '确认保存');
    final stillVisible = confirm.hitTestable().evaluate().length == 1;
    debugPrintSynchronously(({'candidateDialogStillVisibleAfterOneFrame': stillVisible}).toString());
    if (stillVisible) {
      expect(find.byType(NotificationPolicyPage), findsOneWidget);
      await t.tap(confirm); // Actual visible button, never invoke callback.
      await t.idle(); // Async completion without another rendering frame.
    }
    final dispatched = s.puts.toList();
    debugPrintSynchronously(({'synchronousDispatchAfterParentRetirement': [for (final r in dispatched)
      {'method': r.method, 'host': r.host, 'path': r.path, 'body': r.body}]}).toString());
    await t.pumpWidget(const SizedBox());
    s.dispose();
    expect(dispatched, isEmpty);
  });

  testWidgets('通知子页来源：同key连接地址及auth对象替换不携新凭据访问旧连接', (t) async {
    final oldAuth = SeedTestAuth(), nextAuth = SeedTestAuth()..token = 'Bearer fixture-next';
    final oldWire = _PolicyWire(), nextWire = _PolicyWire();
    final source = ValueNotifier<(http.Client, String, SeedTestAuth)>(
      (oldWire, 'http://policy-a.test', oldAuth));
    await t.pumpWidget(MaterialApp(home:
      ValueListenableBuilder<(http.Client, String, SeedTestAuth)>(
        valueListenable: source, builder: (_, value, _) => NotificationPolicyPage(
          key: const ValueKey('same-direct-policy'), auth: value.$3,
          client: value.$1, apiBaseUrl: value.$2))));
    await t.pumpAndSettle();
    expect(oldWire.sent.where((r) => r.path == '/v1/me/notification-policy').length, 1);
    source.value = (nextWire, 'http://policy-b.test', nextAuth);
    await t.pump();
    await t.idle();
    final mismatched = oldWire.sent.where((r) => r.auth == 'Bearer fixture-next').toList();
    debugPrintSynchronously(({'directSameKeyOldTransportWithReplacementCredential': [for (final r in mismatched)
      {'method': r.method, 'host': r.host, 'path': r.path}]}).toString());
    expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
    source.value = (oldWire, 'http://policy-a.test', oldAuth);
    await t.pumpAndSettle();
    expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
    expect(oldWire.sent.length, 1); // Source ABA never revives the original route.
    await t.pumpWidget(const SizedBox());
    oldAuth.dispose(); nextAuth.dispose(); source.dispose(); oldWire.close(); nextWire.close();
    expect(mismatched, isEmpty);
    expect(nextWire.sent, isEmpty); // Retire this route; never rebind silently.
  });

  testWidgets('通知父来源：base一帧退休后真实确认弹窗不向旧地址PUT', (t) async {
    final s = _Scenario();
    await _open(t, s);
    await _approveDialog(t);
    s.source.value = (s.wire, 'http://policy-b.test');
    await t.pump();
    expect(t.widget<SettingsPage>(find.byType(SettingsPage, skipOffstage: false)).apiBaseUrl,
      'http://policy-b.test');
    final confirm = find.widgetWithText(FilledButton, '确认保存');
    expect(confirm.hitTestable(), findsOneWidget);
    await t.tap(confirm);
    await t.idle();
    final dispatched = s.puts.toList();
    await t.pumpAndSettle();
    expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    s.dispose();
    expect(dispatched, isEmpty);
  });

  testWidgets('通知当前来源：真实修改返回和具体版本确认只保存一次', (t) async {
    final s = _Scenario();
    await _open(t, s);
    await _approveDialog(t);
    await t.tap(find.widgetWithText(TextButton, '返回修改'));
    await t.pumpAndSettle();
    expect(s.puts, isEmpty);
    await _show(t, find.widgetWithText(FilledButton, '检查并保存'));
    await t.tap(find.widgetWithText(FilledButton, '检查并保存'));
    await t.pumpAndSettle();
    expect(find.text('本人设置 · 当前版本 0'), findsOneWidget);
    await t.tap(find.widgetWithText(FilledButton, '确认保存'));
    await t.pumpAndSettle();
    expect(s.puts.length, 1);
    final saved = s.puts.single;
    expect(saved.host, 'policy-a.test');
    expect(saved.auth, s.auth.authorizationHeader);
    final input = jsonDecode(saved.body) as Map<String, dynamic>;
    expect(input['expectedVersion'], 0);
    expect(input['enabled'], true);
    expect(find.text('通知设置已保存。'), findsOneWidget);
    expect(s.wire.closed, false); // The child never closes borrowed transport.
    await t.pumpWidget(const SizedBox());
    s.dispose();
  });

  testWidgets('通知已发保存：父来源退役后迟到503不确认不对旧来源自动GET或重PUT', (t) async {
    final s = _Scenario();
    s.wire.delayedPut = Completer<http.StreamedResponse>();
    await _open(t, s);
    await _approveDialog(t);
    await t.tap(find.widgetWithText(FilledButton, '确认保存'));
    await t.idle();
    expect(s.puts.length, 1); // Already dispatched; source change cannot cancel it.
    final before = s.wire.sent.where((r) => r.path == '/v1/me/notification-policy').length;
    s.source.value = (s.otherWire, 'http://policy-a.test');
    await t.pump();
    s.wire.delayedPut!.complete(s.wire.response({'error': {'code': 'unknown'}}, 503));
    await t.idle();
    expect(s.wire.sent.where((r) => r.path == '/v1/me/notification-policy').length, before);
    expect(s.puts.length, 1);
    await t.pumpAndSettle();
    expect(find.text('通知设置已保存。'), findsNothing);
    expect(find.text('已核实当前设置与确认内容一致。'), findsNothing);
    expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
    expect(s.wire.closed, false);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    s.dispose();
  });

  testWidgets('通知子页弹窗来源：同key连接地址一帧替换安全移除旧确认且ABA不复活', (t) async {
    final auth = SeedTestAuth();
    final oldWire = _PolicyWire(), nextWire = _PolicyWire();
    final source = ValueNotifier<(http.Client, String)>((oldWire, 'http://policy-a.test'));
    await t.pumpWidget(MaterialApp(home:
      ValueListenableBuilder<(http.Client, String)>(valueListenable: source,
        builder: (_, value, _) => NotificationPolicyPage(
          key: const ValueKey('same-active-dialog-policy'), auth: auth,
          client: value.$1, apiBaseUrl: value.$2))));
    await t.pumpAndSettle();
    await _approveDialog(t);
    expect(find.widgetWithText(FilledButton, '确认保存').hitTestable(), findsOneWidget);
    source.value = (nextWire, 'http://policy-b.test');
    await t.pump(); // Actual same-key parent rebuild while the original dialog is active.
    final frameworkError = t.takeException();
    debugPrintSynchronously(({'activeDialogSourceReplacementFrameworkError': frameworkError?.toString()}).toString());
    expect(frameworkError, isNull);
    await t.pumpAndSettle();
    expect(find.widgetWithText(FilledButton, '确认保存'), findsNothing);
    expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
    expect(oldWire.sent.where((r) => r.method == 'PUT'), isEmpty);
    expect(nextWire.sent, isEmpty);
    source.value = (oldWire, 'http://policy-a.test');
    await t.pumpAndSettle();
    expect(find.widgetWithText(FilledButton, '确认保存'), findsNothing);
    expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
    expect(oldWire.sent.length, 1);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    auth.dispose(); source.dispose(); oldWire.close(); nextWire.close();
  });
}
