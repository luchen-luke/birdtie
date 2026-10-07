import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/notification_schedule_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'notification_schedule_controller_test.dart'
    show scheduleData, scheduleResponse, scheduleClock;

class _SchedulePageFixture {
  final auth = SeedTestAuth(), workspace = ValueNotifier<String?>(null);
  final requests = <http.Request>[];
  late MockClient client;
  late final secondClient = MockClient(
    (r) async => scheduleResponse(scheduleData(version: 4)),
  );
  int version = 1;
  bool network = false;
  bool useSecond = false;
  String base = 'http://schedule-page.test';
  Completer<http.Response>? gate;
  _SchedulePageFixture({this.version = 1}) {
    client = MockClient((r) async {
      requests.add(r);
      if (r.method == 'GET') {
        return scheduleResponse(scheduleData(version: version));
      }
      if (gate != null) return gate!.future;
      if (network) throw http.ClientException('unknown');
      final b = jsonDecode(r.body) as Map<String, dynamic>;
      version++;
      final p = scheduleData(version: version)..addAll(b);
      p.remove('expectedVersion');
      p['status'] = b['enabled'] == true ? 'ACTIVE' : 'DISABLED';
      return scheduleResponse(p);
    });
  }
  String? workspaceID() => workspace.value;
  Widget page({double scale = 1}) => MaterialApp(
    builder: (context, child) => MediaQuery(
      data: MediaQuery.of(
        context,
      ).copyWith(textScaler: TextScaler.linear(scale)),
      child: child!,
    ),
    home: NotificationSchedulePage(
      auth: auth,
      client: useSecond ? secondClient : client,
      apiBaseUrl: base,
      workspaceChanges: workspace,
      organizationWorkspaceID: workspaceID,
      now: scheduleClock,
    ),
  );
  Future<void> mount(WidgetTester t, {double scale = 1}) async {
    await t.pumpWidget(page(scale: scale));
    await t.pumpAndSettle();
  }

  Future<void> tap(WidgetTester t, String text) async {
    await t.scrollUntilVisible(
      find.text(text),
      200,
      scrollable: find
          .byWidgetPredicate(
            (w) => w is Scrollable && w.axisDirection == AxisDirection.down,
          )
          .first,
      maxScrolls: 30,
    );
    await t.pumpAndSettle();
    expect(find.text(text).hitTestable(), findsOneWidget);
    await t.tap(find.text(text));
    await t.pumpAndSettle();
  }

  Future<void> check(WidgetTester t) async {
    await tap(t, '检查并保存计划');
    expect(find.text('确认定时汇总计划'), findsOneWidget);
  }

  Future<void> dropdown(WidgetTester t, String label) async {
    final field = find.byWidgetPredicate(
      (w) =>
          w is DropdownButtonFormField<int> && w.decoration.labelText == label,
    );
    await t.scrollUntilVisible(
      field,
      200,
      scrollable: find
          .byWidgetPredicate(
            (w) => w is Scrollable && w.axisDirection == AxisDirection.down,
          )
          .first,
      maxScrolls: 30,
    );
    await t.pumpAndSettle();
    expect(field.hitTestable(), findsOneWidget);
    await t.tap(field);
    await t.pumpAndSettle();
  }

  Future<void> close(WidgetTester t) async {
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
    auth.dispose();
    workspace.dispose();
    client.close();
    secondClient.close();
  }
}

void main() {
  testWidgets('AIR019页面未配置显式草稿、缺项不能保存且不猜字段', (t) async {
    final x = _SchedulePageFixture(version: 0);
    await x.mount(t);
    expect(find.text('尚未设置定时汇总：不会自动创建每日计划。'), findsOneWidget);
    expect(find.byType(TextFormField), findsNothing);
    await x.tap(t, '创建汇总草稿');
    expect(
      t.widget<TextFormField>(find.byType(TextFormField)).controller!.text,
      '',
    );
    expect(find.text('汇总时间：请选择'), findsOneWidget);
    await x.tap(t, '检查并保存计划');
    expect(find.text('请明确时区、汇总时间、类别、滚动额度和未来有效期限。'), findsOneWidget);
    expect(x.requests.where((r) => r.method != 'GET'), isEmpty);
    await x.close(t);
  });
  testWidgets('AIR019页面真实中文预览取消0PUT、明确同版本保存1PUT', (t) async {
    final x = _SchedulePageFixture();
    await x.mount(t);
    await x.check(t);
    expect(find.text('本人计划 · 当前版本 1'), findsOneWidget);
    expect(find.text('明确时区：Europe/London'), findsOneWidget);
    expect(find.textContaining('不代表已投递'), findsNothing);
    expect(find.textContaining('滚动24小时最多 3'), findsOneWidget);
    expect(find.textContaining('不停止原活动提醒'), findsOneWidget);
    await t.tap(find.text('返回修改'));
    await t.pumpAndSettle();
    expect(x.requests.where((r) => r.method == 'PUT'), isEmpty);
    await x.check(t);
    await t.tap(find.text('确认保存'));
    await t.pumpAndSettle();
    expect(x.requests.where((r) => r.method == 'PUT').length, 1);
    expect(find.text('计划设置已保存，不代表消息已投递；请在收件箱查看实际汇总。'), findsOneWidget);
    expect(t.takeException(), isNull);
    await x.close(t);
  });
  testWidgets('AIR019页面时区预设同步实际编辑文字与草稿预览', (t) async {
    final x = _SchedulePageFixture();
    await x.mount(t);
    await x.tap(t, '中国大陆 · Asia/Shanghai');
    // The vertical list can lazily remove the text field after bringing its
    // preset into view. Scroll back through the real list before inspecting it.
    await t.drag(find.byType(ListView), const Offset(0, 400));
    await t.pumpAndSettle();
    expect(
      t.widget<TextFormField>(find.byType(TextFormField)).controller!.text,
      'Asia/Shanghai',
    );
    await x.check(t);
    expect(find.text('明确时区：Asia/Shanghai'), findsOneWidget);
    await t.tap(find.text('返回修改'));
    await t.pumpAndSettle();
    expect(x.requests.where((r) => r.method != 'GET'), isEmpty);
    await x.close(t);
  });
  testWidgets('AIR019页面未配置真实填写全部必需字段后一次CAS保存', (t) async {
    final x = _SchedulePageFixture(version: 0);
    await x.mount(t);
    await x.tap(t, '创建汇总草稿');
    await x.tap(t, '启用定时汇总');
    await x.tap(t, '中国大陆 · Asia/Shanghai');
    await x.tap(t, '汇总时间：请选择');
    expect(find.byType(TimePickerDialog), findsOneWidget);
    await t.tap(find.text('使用此时间'));
    await t.pumpAndSettle();
    await x.tap(t, '活动与匹配机会');
    await x.dropdown(t, '滚动24小时触达上限');
    await t.tap(find.text('最多 2 条收件箱记录').last);
    await t.pumpAndSettle();
    await x.dropdown(t, '明确选择新的有效期限');
    await t.tap(find.text('从此刻起 1 天').last);
    await t.pumpAndSettle();
    await x.check(t);
    expect(find.text('本人计划 · 当前版本 0'), findsOneWidget);
    expect(find.text('明确时区：Asia/Shanghai'), findsOneWidget);
    expect(find.text('当地汇总时间：00:00'), findsOneWidget);
    expect(find.text('静默：不设置'), findsOneWidget);
    expect(find.textContaining('滚动24小时最多 2'), findsOneWidget);
    await t.tap(find.text('确认保存'));
    await t.pumpAndSettle();
    final b = jsonDecode(x.requests.singleWhere((r) => r.method == 'PUT').body);
    expect(b['expectedVersion'], 0);
    expect(b['enabled'], isTrue);
    expect(b['quiet'], isNull);
    expect(b['localMinute'], 0);
    expect(b['categories'], ['ACTIVITY']);
    expect(
      b['expiresAt'],
      scheduleClock().add(const Duration(days: 1)).toIso8601String(),
    );
    expect(
      find.text('计划设置已保存，不代表消息已投递；请在收件箱查看实际汇总。').hitTestable(),
      findsOneWidget,
    );
    expect(t.takeException(), isNull);
    await x.close(t);
  });
  for (final actor in ['account', 'workspace']) {
    testWidgets('AIR019页面$actor实际ABA退休已打开的时间选择器', (t) async {
      final x = _SchedulePageFixture();
      await x.mount(t);
      await x.tap(t, '汇总时间：18:00');
      expect(find.byType(TimePickerDialog), findsOneWidget);
      if (actor == 'account') {
        x.auth.changeIdentity('Bearer peer', nextOwner: 'peer');
        x.auth.changeIdentity('Bearer owner', nextOwner: 'owner');
      } else {
        x.workspace.value = 'org';
        x.workspace.value = null;
      }
      await t.pumpAndSettle();
      expect(find.byType(TimePickerDialog), findsNothing);
      expect(x.requests.where((r) => r.method == 'PUT'), isEmpty);
      await x.check(t);
      expect(find.text('当地汇总时间：18:00'), findsOneWidget);
      await t.tap(find.text('返回修改'));
      await t.pumpAndSettle();
      await x.close(t);
    });
  }
  for (final status in [401, 403, 503]) {
    testWidgets('AIR019页面读取$status明确错误不是未配置，人工重读只GET', (t) async {
      final x = _SchedulePageFixture();
      var failure = true;
      x.client = MockClient((r) async {
        x.requests.add(r);
        return failure
            ? http.Response('{}', status)
            : scheduleResponse(scheduleData());
      });
      await x.mount(t);
      final message = status == 401
          ? '登录已失效，请重新登录后读取本人计划。'
          : status == 403
          ? '请切回本人账号，恢复访问权限后重新读取。'
          : '定时汇总计划暂不可读，请稍后重新读取。';
      expect(find.text(message).hitTestable(), findsOneWidget);
      expect(find.textContaining('尚未设置定时汇总'), findsNothing);
      expect(find.text('检查并保存计划'), findsNothing);
      failure = false;
      await x.tap(t, '重新读取当前计划');
      expect(find.text('尚未设置定时汇总：不会自动创建每日计划。'), findsOneWidget);
      expect(x.requests.length, 2);
      expect(x.requests.every((r) => r.method == 'GET'), isTrue);
      await x.close(t);
    });
  }
  for (final change in ['accountABA', 'workspaceABA', 'client', 'base']) {
    testWidgets('AIR019页面$change退休具体批准，晚确认不写旧来源', (t) async {
      final x = _SchedulePageFixture();
      await x.mount(t);
      await x.check(t);
      if (change == 'accountABA') {
        x.auth.changeIdentity('Bearer peer', nextOwner: 'peer');
        x.auth.changeIdentity('Bearer owner', nextOwner: 'owner');
      } else if (change == 'workspaceABA') {
        x.workspace.value = 'org';
        x.workspace.value = null;
      } else {
        if (change == 'client') {
          x.useSecond = true;
        } else {
          x.base = 'http://new-schedule.test';
        }
        await t.pumpWidget(x.page());
      }
      await t.pumpAndSettle();
      expect(find.text('确认保存'), findsNothing);
      expect(x.requests.where((r) => r.method == 'PUT'), isEmpty);
      expect(t.takeException(), isNull);
      await x.close(t);
    });
  }
  testWidgets('AIR019页面本人首次登录、昵称变更保正常GET而非永久退休', (t) async {
    final x = _SchedulePageFixture();
    x.auth.changeIdentity(null, nextOwner: 'owner');
    await x.mount(t);
    expect(find.text('请登录并切换到本人身份管理定时汇总。'), findsOneWidget);
    expect(x.requests, isEmpty);
    x.auth.changeIdentity('Bearer owner', nextOwner: 'owner');
    await t.pumpAndSettle();
    expect(x.requests.length, 1);
    x.auth.updateProfileDisplayName('合法新昵称');
    await t.pumpAndSettle();
    expect(x.requests.length, 1);
    await x.check(t);
    expect(find.text('确认保存'), findsOneWidget);
    await t.tap(find.text('返回修改'));
    await t.pumpAndSettle();
    await x.close(t);
  });
  testWidgets('AIR019页面组织身份不读取也不保留个人草稿', (t) async {
    final x = _SchedulePageFixture();
    x.workspace.value = 'org';
    await x.mount(t);
    expect(find.text('请登录并切换到本人身份管理定时汇总。'), findsOneWidget);
    expect(x.requests, isEmpty);
    x.workspace.value = null;
    await t.pumpAndSettle();
    await x.check(t);
    await t.tap(find.text('返回修改'));
    await t.pumpAndSettle();
    await x.close(t);
  });
  testWidgets('AIR019页面未知保存仅GET且明确采用当前版本前不重发', (t) async {
    final x = _SchedulePageFixture()..network = true;
    await x.mount(t);
    await x.check(t);
    await t.tap(find.text('确认保存'));
    await t.pumpAndSettle();
    expect(x.requests.where((r) => r.method == 'PUT').length, 1);
    expect(find.textContaining('原提交结果仍无法确定'), findsOneWidget);
    await x.tap(t, '重新读取当前计划');
    expect(x.requests.where((r) => r.method == 'PUT').length, 1);
    await x.tap(t, '已检查，采用读回的当前设置');
    await x.check(t);
    await t.tap(find.text('返回修改'));
    await t.pumpAndSettle();
    expect(x.requests.where((r) => r.method == 'PUT').length, 1);
    await x.close(t);
  });
  testWidgets('AIR019页面320字号2滚动到确认动作48dp，取消不写', (t) async {
    t.view.physicalSize = const Size(320, 640);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final x = _SchedulePageFixture();
    final semantics = t.ensureSemantics();
    await x.mount(t, scale: 2);
    await x.tap(t, '检查并保存计划');
    final button = find.widgetWithText(FilledButton, '确认保存');
    expect(t.getSize(button).height, greaterThanOrEqualTo(48));
    expect(button.hitTestable(), findsOneWidget);
    expect(t.takeException(), isNull);
    await t.tap(find.text('返回修改'));
    await t.pumpAndSettle();
    expect(x.requests.where((r) => r.method != 'GET'), isEmpty);
    semantics.dispose();
    await x.close(t);
  });
  for (final failure in [false, true]) {
    testWidgets('AIR019页面late保存failure$failure不污染来源替换当前UI', (t) async {
      final x = _SchedulePageFixture()..gate = Completer<http.Response>();
      await x.mount(t);
      await x.check(t);
      await t.tap(find.text('确认保存'));
      await t.pump();
      x.useSecond = true;
      await t.pumpWidget(x.page());
      await t.pumpAndSettle();
      expect(find.textContaining('版本 4'), findsOneWidget);
      if (failure) {
        x.gate!.completeError(http.ClientException('old outcome'));
      } else {
        x.gate!.complete(scheduleResponse(scheduleData(version: 2)));
      }
      await t.pumpAndSettle();
      expect(find.textContaining('版本 4'), findsOneWidget);
      expect(find.textContaining('已保存'), findsNothing);
      expect(find.textContaining('提交结果未知'), findsNothing);
      expect(t.takeException(), isNull);
      await x.close(t);
    });
  }
}
