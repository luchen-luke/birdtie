import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/notification_schedule_page.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart' show debugPrintSynchronously;
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'notification_schedule_controller_test.dart' show scheduleData;

// Actual Settings navigation and widgets; synthetic original schedule transport.
// Dispatch is recorded synchronously before any response completion.
class _ScheduleWire extends http.BaseClient {
  final sent = <({String method, String host, String path, String body, String? auth})>[];
  bool closed = false;
  Completer<http.StreamedResponse>? delayedPut;
  final current = scheduleData(version: 1)..addAll({
    'validFrom': DateTime.now().toUtc().toIso8601String(),
    'expiresAt': DateTime.now().toUtc().add(const Duration(days: 7)).toIso8601String(),
  });
  _ScheduleWire() { current['updatedAt'] = current['validFrom']; }
  http.StreamedResponse response(Object body, [int status = 200]) =>
      http.StreamedResponse(Stream.value(utf8.encode(jsonEncode(body))), status,
        headers: {'content-type': 'application/json; charset=utf-8'});
  @override
  Future<http.StreamedResponse> send(http.BaseRequest r) {
    final body = r is http.Request ? r.body : '';
    sent.add((method: r.method, host: r.url.host, path: r.url.path,
      body: body, auth: r.headers['Authorization']));
    if (r.url.path == '/v1/me/notification-schedule') {
      if (r.method == 'PUT') {
        if (delayedPut != null) return delayedPut!.future;
        final input = jsonDecode(body) as Map<String,dynamic>;
        current.addAll(input);
        current.remove('expectedVersion');
        current['version'] = (input['expectedVersion'] as int) + 1;
        current['status'] = input['enabled'] == true ? 'ACTIVE' : 'DISABLED';
        current['validFrom'] = current['updatedAt'] = DateTime.now().toUtc().toIso8601String();
        return Future.value(response({'data': current}));
      }
      return Future.value(response({'data': current}));
    }
    return Future.value(response({'data': []}));
  }
  @override
  void close() { closed = true; super.close(); }
}

class _Scenario {
  final auth = SeedTestAuth(), city = PublicCityController();
  final wire = _ScheduleWire(), nextWire = _ScheduleWire();
  late final source = ValueNotifier<(http.Client,String)>((wire,'http://schedule-a.test'));
  late final moments = PrivateMomentController(authorizationHeader: () => auth.authorizationHeader,
    client: wire, apiBaseUrl: 'http://schedule-a.test');
  Iterable<({String method, String host, String path, String body, String? auth})> get puts =>
      [...wire.sent, ...nextWire.sent].where((r) => r.method == 'PUT');
  void dispose() {
    auth.dispose(); city.dispose(); moments.dispose(); source.dispose();
    wire.close(); nextWire.close();
  }
}
Widget _home(_Scenario s) => MaterialApp(home:
  ValueListenableBuilder<(http.Client,String)>(valueListenable: s.source,
    builder: (_, source, _) => Scaffold(body: SettingsPage(
      key: const ValueKey('same-schedule-settings'), auth: s.auth, city: s.city,
      moments: s.moments, client: source.$1, apiBaseUrl: source.$2))));
Future<void> _show(WidgetTester t, Finder target) async {
  if (target.evaluate().isEmpty) {
    await t.scrollUntilVisible(target,160,maxScrolls:50,
      scrollable:find.byType(Scrollable).first);
  }
  await t.ensureVisible(target);
  await t.pumpAndSettle();
  expect(target.hitTestable(),findsOneWidget);
}
Future<void> _open(WidgetTester t, _Scenario s) async {
  await t.pumpWidget(_home(s)); await t.pumpAndSettle();
  final entry=find.widgetWithText(ListTile,'定时汇总计划');
  await _show(t,entry); await t.tap(entry); await t.pumpAndSettle();
  expect(find.byType(NotificationSchedulePage),findsOneWidget);
  expect(find.text('当前状态：已启用 · 版本 1'), findsOneWidget);
}
Future<void> _approveDialog(WidgetTester t) async {
  final edit=find.widgetWithText(SwitchListTile,'启用定时汇总');
  await _show(t,edit); await t.tap(edit); await t.pumpAndSettle();
  final check=find.widgetWithText(FilledButton,'检查并保存计划');
  await _show(t,check); await t.tap(check); await t.pumpAndSettle();
  expect(find.byType(AlertDialog),findsOneWidget);
  expect(find.text('本人计划 · 当前版本 1'),findsOneWidget);
  expect(find.text('明确时区：Europe/London'),findsOneWidget);
  expect(find.text('关闭未来汇总，不回放旧记录'),findsOneWidget);
  expect(find.widgetWithText(FilledButton,'确认保存').hitTestable(),findsOneWidget);
}
void main() {
  for (final branch in ['client','base']) {
    testWidgets('AIR019父来源$branch：一帧更新后真实可点确认不向旧来源PUT',(t) async {
      final s=_Scenario(); await _open(t,s); await _approveDialog(t);
      expect(s.puts,isEmpty);
      s.source.value=branch=='client' ? (s.nextWire,'http://schedule-a.test') : (s.wire,'http://schedule-b.test');
      await t.pump(); // Exactly one actual Settings parent-update frame before tap.
      final parent=t.widget<SettingsPage>(find.byType(SettingsPage,skipOffstage:false));
      expect(branch=='client' ? parent.client==s.nextWire : parent.apiBaseUrl=='http://schedule-b.test',isTrue);
      final confirm=find.widgetWithText(FilledButton,'确认保存');
      final visible=confirm.hitTestable().evaluate().length==1;
      debugPrintSynchronously({'branch':branch,'confirmHitTestableAfterOneFrame':visible}.toString());
      if (visible) {
        await t.tap(confirm); // Standard tester gesture, never direct callback invocation.
        await t.idle(); // Complete dialog Future without a second rendering frame.
      }
      final sent=s.puts.toList();
      debugPrintSynchronously({'synchronousOldDispatchAfterRetirement':[for(final r in sent)
        {'method':r.method,'host':r.host,'path':r.path,'body':r.body} ]}.toString());
      await t.pumpAndSettle();
      expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'),findsOneWidget);
      expect(s.wire.closed,isFalse);
      await t.pumpWidget(const SizedBox()); s.dispose();
      expect(sent,isEmpty);
    });
  }
  testWidgets('AIR019当前父来源：取消0PUT、明确当前版本保存仅1PUT',(t) async {
    final s=_Scenario(); await _open(t,s); await _approveDialog(t);
    await t.tap(find.widgetWithText(TextButton,'返回修改')); await t.pumpAndSettle();
    expect(s.puts,isEmpty);
    final check=find.widgetWithText(FilledButton,'检查并保存计划');
    await _show(t,check); await t.tap(check); await t.pumpAndSettle();
    expect(find.text('本人计划 · 当前版本 1'),findsOneWidget);
    await t.tap(find.widgetWithText(FilledButton,'确认保存')); await t.pumpAndSettle();
    expect(s.puts.length,1);
    final saved=s.puts.single,input=jsonDecode(saved.body) as Map<String,dynamic>;
    expect(saved.host,'schedule-a.test'); expect(saved.auth,s.auth.authorizationHeader);
    expect(input['expectedVersion'],1); expect(input['enabled'],isFalse);
    expect(input['timeZone'],'Europe/London'); expect(input['localMinute'],1080);
    expect(input['categories'],['ACTIVITY','SOCIAL']);
    expect(find.text('计划设置已保存，不代表消息已投递；请在收件箱查看实际汇总。'),findsOneWidget);
    expect(s.wire.closed,isFalse); expect(t.takeException(),isNull);
    await t.pumpWidget(const SizedBox()); s.dispose();
  });
  testWidgets('AIR019已发保存：父来源退休后晚503不自动对旧来源GET或重PUT',(t) async {
    final s=_Scenario(); s.wire.delayedPut=Completer<http.StreamedResponse>();
    await _open(t,s); await _approveDialog(t);
    await t.tap(find.widgetWithText(FilledButton,'确认保存')); await t.idle();
    expect(s.puts.length,1); // Already sent cannot be undone by retirement.
    final before=s.wire.sent.where((r)=>r.path=='/v1/me/notification-schedule').length;
    s.source.value=(s.nextWire,'http://schedule-a.test'); await t.pump();
    s.wire.delayedPut!.complete(s.wire.response({'error':{'code':'unknown'}},503));
    await t.idle();
    final after=s.wire.sent.where((r)=>r.path=='/v1/me/notification-schedule').length;
    debugPrintSynchronously({'beforeRetirementScheduleDispatches':before,'afterLateResponseScheduleDispatches':after}.toString());
    await t.pumpAndSettle();
    expect(s.puts.length,1);
    expect(find.text('计划设置已保存，不代表消息已投递；请在收件箱查看实际汇总。'),findsNothing);
    expect(find.text('已核实当前计划与确认内容一致；不代表已投递。'),findsNothing);
    expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'),findsOneWidget);
    expect(s.wire.closed,isFalse); expect(t.takeException(),isNull);
    await t.pumpWidget(const SizedBox()); s.dispose();
    expect(after,before);
  });
}
