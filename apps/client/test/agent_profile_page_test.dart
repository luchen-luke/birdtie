import 'dart:async';
import 'dart:io';
import 'dart:ui' as ui;
import 'package:birdtie_client/src/workspace/agent_profile_page.dart';
import 'package:birdtie_client/src/workspace/agent_memory_correction_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'agent_profile_api_test.dart';
import 'agent_memory_correction_api_test.dart' show correctionMemoryRaw;

Widget profileHarness(
  SeedTestAuth auth,
  http.Client client, {
  ValueNotifier<String?>? workspace,
  String base = 'http://local',
  Listenable? changes,
  String? Function()? getter,
  double scale = 1,
  double ime = 0,
  bool dark = false,
}) => MaterialApp(
  theme: ThemeData(
    brightness: dark ? Brightness.dark : Brightness.light,
    fontFamily: Platform.environment['BIRDTIE_AGENT_PROFILE_CAPTURE'] == '1'
        ? 'AgentProfileQA'
        : null,
  ),
  builder: (_, child) => MediaQuery(
    data: MediaQueryData(
      size: const Size(320, 640),
      textScaler: TextScaler.linear(scale),
      viewInsets: EdgeInsets.only(bottom: ime),
    ),
    child: child!,
  ),
  home: RepaintBoundary(
    key: const ValueKey('profile-render'),
    child: AgentProfilePage(
      key: const ValueKey('same-profile'),
      auth: auth,
      client: client,
      apiBaseUrl: base,
      workspaceChanges: changes ?? workspace,
      organizationWorkspaceID:
          getter ?? (workspace == null ? null : () => workspace.value),
    ),
  ),
);
Future<void> profileFont(WidgetTester t) async {
  if (Platform.environment['BIRDTIE_AGENT_PROFILE_CAPTURE'] != '1') return;
  await t.runAsync(() async {
    final loader = FontLoader('AgentProfileQA')
      ..addFont(
        File(
          'C:/Windows/Fonts/msyh.ttc',
        ).readAsBytes().then(ByteData.sublistView),
      );
    await loader.load();
  });
}

Future<void> profileCapture(WidgetTester t, String name) async {
  final path = Platform.environment['BIRDTIE_AGENT_PROFILE_CAPTURE_DIR'];
  if (path == null) return;
  final boundary = t.renderObject<RenderRepaintBoundary>(
    find.byKey(const ValueKey('profile-render')),
  );
  await t.runAsync(() async {
    final img = await boundary.toImage(pixelRatio: 2);
    try {
      final data = await img.toByteData(format: ui.ImageByteFormat.png);
      if (data == null) throw StateError('no render bytes');
      await Directory(path).create(recursive: true);
      await File('$path/$name.png').writeAsBytes(data.buffer.asUint8List());
    } finally {
      img.dispose();
    }
  });
}

Future<void> profileTap(WidgetTester t, String text) async {
  final f = find.text(text);
  await t.scrollUntilVisible(
    f,
    180,
    maxScrolls: 100,
    scrollable: find.byType(Scrollable).first,
  );
  await t.ensureVisible(f);
  await t.pump();
  await t.tap(f);
  await t.pumpAndSettle();
}

void main() {
  testWidgets('1000条原Memory只挂载视口行，末行仍可滚动读取', (t) async {
    final auth = SeedTestAuth()..owner = profileOwner;
    addTearDown(auth.dispose);
    String id(int n) =>
        '82000000-0000-4000-8000-${n.toString().padLeft(12, '0')}';
    final client = MockClient((r) async {
      if (r.url.path.endsWith('agent-memories')) {
        return profileResponse([
          for (var i = 1; i <= 1000; ++i)
            correctionMemoryRaw(at: DateTime.now().toUtc())..['id'] = id(i),
        ]);
      }
      return profileResponse(profileData(r.url.path));
    });
    addTearDown(client.close);
    await t.pumpWidget(profileHarness(auth, client));
    await t.pumpAndSettle();
    await t.scrollUntilVisible(
      find.byKey(ValueKey('profile-memory-${id(1)}')),
      200,
      maxScrolls: 100,
      scrollable: find.byType(Scrollable).first,
    );
    expect(find.byType(ListTile).evaluate().length, lessThan(30));
    await t.scrollUntilVisible(
      find.byKey(ValueKey('profile-memory-${id(1000)}')),
      500,
      maxScrolls: 400,
      scrollable: find.byType(Scrollable).first,
    );
    expect(find.byKey(ValueKey('profile-memory-${id(1000)}')), findsOneWidget);
    expect(find.byType(ListTile).evaluate().length, lessThan(30));
    expect(t.takeException(), isNull);
  });
  testWidgets('键盘激活原报名管理入口且借用transport零关闭', (t) async {
    final auth = SeedTestAuth()..owner = profileOwner;
    addTearDown(auth.dispose);
    final client = ProfileTrackedClient(
      (r) async => profileResponse(profileData(r.url.path)),
    );
    addTearDown(client.close);
    await t.pumpWidget(profileHarness(auth, client));
    await t.pumpAndSettle();
    final button = find.widgetWithText(OutlinedButton, '管理报名的公开范围');
    await t.scrollUntilVisible(
      button,
      160,
      maxScrolls: 100,
      scrollable: find.byType(Scrollable).first,
    );
    Focus.of(t.element(find.text('管理报名的公开范围'))).requestFocus();
    await t.pump();
    await t.sendKeyEvent(LogicalKeyboardKey.enter);
    await t.pumpAndSettle();
    expect(find.text('我的公开报名'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    expect(client.closeCount, 0);
  });
  for (final delayed in [false, true]) {
    testWidgets('短detail server未来时间 ${delayed ? '迟到不展示' : '自然期限清正文'}', (
      t,
    ) async {
      final auth = SeedTestAuth()..owner = profileOwner;
      addTearDown(auth.dispose);
      final entered = Completer<void>(), response = Completer<http.Response>();
      final server = DateTime.now().toUtc().add(const Duration(hours: 1));
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/$profileMemory')) {
          entered.complete();
          return response.future;
        }
        return profileResponse(profileData(r.url.path));
      });
      addTearDown(client.close);
      await t.pumpWidget(profileHarness(auth, client));
      await t.pumpAndSettle();
      await t.scrollUntilVisible(
        find.text('本人明确声明'),
        180,
        maxScrolls: 100,
        scrollable: find.byType(Scrollable).first,
      );
      await t.tap(find.text('本人明确声明'));
      await t.pump();
      await t.runAsync(() async {
        await entered.future;
        if (delayed) {
          await Future<void>.delayed(const Duration(milliseconds: 150));
        }
        response.complete(
          profileResponse(
            detailRaw(at: server, lease: const Duration(milliseconds: 100)),
          ),
        );
      });
      await t.pump();
      await t.pump();
      if (!delayed) {
        expect(find.text('我偏好徒步活动'), findsOneWidget);
        await t.pump(const Duration(milliseconds: 150));
      }
      expect(find.text('我偏好徒步活动'), findsNothing);
      expect(find.textContaining('读取已到期'), findsOneWidget);
    });
  }
  testWidgets('六组原资料与当前detail，不显示概率或虚构到访资格', (t) async {
    await profileFont(t);
    t.view.physicalSize = const Size(320, 640);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final auth = SeedTestAuth()..owner = profileOwner;
    addTearDown(auth.dispose);
    final req = <http.Request>[];
    final client = ProfileTrackedClient((r) async {
      req.add(r);
      return profileResponse(profileData(r.url.path));
    });
    addTearDown(client.close);
    await t.pumpWidget(profileHarness(auth, client));
    await t.pumpAndSettle();
    expect(find.text('我的智能体'), findsOneWidget);
    expect(find.text('本人填写的补充说明：本人填写的合成补充'), findsOneWidget);
    expect(find.text('我偏好徒步活动'), findsNothing);
    await profileCapture(t, 'profile-first');
    for (final title in ['智能体知道哪些本人资料', '偏好', '地点', '活动', '社群', '智能体设置']) {
      await t.scrollUntilVisible(
        find.text(title),
        160,
        maxScrolls: 100,
        scrollable: find.byType(Scrollable).first,
      );
      expect(find.text(title), findsOneWidget);
    }
    expect(find.textContaining('可在设置中分别管理'), findsOneWidget);
    await t.drag(find.byType(Scrollable).first, const Offset(0, 10000));
    await t.pumpAndSettle();
    await profileTap(t, '本人明确声明');
    expect(find.text('我偏好徒步活动'), findsOneWidget);
    await profileCapture(t, 'profile-current-detail');
    expect(req.every((r) => r.method == 'GET'), true);
    expect(find.textContaining('82%'), findsNothing);
    await t.pumpWidget(const SizedBox());
    expect(client.closeCount, 0);
  });
  for (final mode in ['client', 'base', 'getter', 'listener', 'auth']) {
    testWidgets('同key $mode A-B-A永久退役', (t) async {
      final auth = SeedTestAuth()..owner = profileOwner,
          other = SeedTestAuth()..owner = profileOwner;
      addTearDown(auth.dispose);
      addTearDown(other.dispose);
      final client = MockClient(
            (r) async => profileResponse(profileData(r.url.path)),
          ),
          next = MockClient(
            (r) async => profileResponse(profileData(r.url.path)),
          );
      addTearDown(client.close);
      addTearDown(next.close);
      final a = ValueNotifier<String?>(null), b = ValueNotifier<String?>(null);
      addTearDown(a.dispose);
      addTearDown(b.dispose);
      String? getA() => null;
      String? getB() => null;
      Widget frame(bool changed) => profileHarness(
        mode == 'auth' && changed ? other : auth,
        mode == 'client' && changed ? next : client,
        base: mode == 'base' && changed ? 'http://next' : 'http://local',
        changes: mode == 'listener' && changed ? b : a,
        getter: mode == 'getter' && changed ? getB : getA,
      );
      await t.pumpWidget(frame(false));
      await t.pumpAndSettle();
      expect(find.textContaining('本人填写的合成补充'), findsOneWidget);
      await t.pumpWidget(frame(true));
      await t.pumpAndSettle();
      await t.pumpWidget(frame(false));
      await t.pumpAndSettle();
      expect(find.textContaining('本人填写的合成补充'), findsNothing);
      expect(find.textContaining('身份或连接已变化'), findsOneWidget);
    });
  }
  testWidgets('通知身份ABA与迟到响应不会回显旧正文', (t) async {
    final auth = SeedTestAuth()..owner = profileOwner;
    addTearDown(auth.dispose);
    final response = Completer<http.Response>();
    final client = MockClient(
      (r) async => r.url.path.endsWith('agent-private-profile')
          ? response.future
          : profileResponse(profileData(r.url.path)),
    );
    addTearDown(client.close);
    await t.pumpWidget(profileHarness(auth, client));
    await t.pump();
    auth.changeIdentity('Bearer b', nextOwner: profileAgent);
    auth.changeIdentity('Bearer owner', nextOwner: profileOwner);
    response.complete(profileResponse(profileRaw()));
    await t.pumpAndSettle();
    expect(find.textContaining('本人填写的合成补充'), findsNothing);
    expect(find.textContaining('身份或连接已变化'), findsOneWidget);
  });
  testWidgets('during-build workspace变更同步失效不触Navigator', (t) async {
    final auth = SeedTestAuth()..owner = profileOwner;
    addTearDown(auth.dispose);
    final client = MockClient(
      (r) async => profileResponse(profileData(r.url.path)),
    );
    addTearDown(client.close);
    final workspace = ValueNotifier<String?>(null),
        trigger = ValueNotifier<bool>(false);
    addTearDown(workspace.dispose);
    addTearDown(trigger.dispose);
    String? org() => workspace.value;
    await t.pumpWidget(
      MaterialApp(
        home: ValueListenableBuilder<bool>(
          valueListenable: trigger,
          builder: (_, v, _) {
            if (v) workspace.value = profileAgent;
            return AgentProfilePage(
              auth: auth,
              client: client,
              apiBaseUrl: 'http://local',
              workspaceChanges: workspace,
              organizationWorkspaceID: org,
            );
          },
        ),
      ),
    );
    await t.pumpAndSettle();
    trigger.value = true;
    await t.pumpAndSettle();
    expect(t.takeException(), isNull);
    expect(find.textContaining('本人填写的合成补充'), findsNothing);
  });
  testWidgets('组织/匿名中文空态且0请求', (t) async {
    final auth = SeedTestAuth()..owner = profileOwner;
    addTearDown(auth.dispose);
    var calls = 0;
    final client = MockClient((r) async {
      calls++;
      return profileResponse([]);
    });
    addTearDown(client.close);
    final org = ValueNotifier<String?>(profileAgent);
    addTearDown(org.dispose);
    await t.pumpWidget(profileHarness(auth, client, workspace: org));
    await t.pumpAndSettle();
    expect(find.textContaining('个人身份查看'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    auth.token = null;
    await t.pumpWidget(profileHarness(auth, client));
    await t.pumpAndSettle();
    expect(find.textContaining('请先登录'), findsOneWidget);
    expect(calls, 0);
  });
  for (final dark in [false, true]) {
    testWidgets('320 font3 IME260 ${dark ? 'dark' : 'light'}所有组和48dp按钮可读', (
      t,
    ) async {
      await profileFont(t);
      t.view.physicalSize = const Size(320, 640);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final auth = SeedTestAuth()..owner = profileOwner;
      addTearDown(auth.dispose);
      final client = MockClient((r) async {
        final v = profileData(r.url.path);
        if (r.url.path.endsWith('agent-private-profile')) {
          v['fields']['agentNotes'] = '本人填写的长中文补充、不同语言和可检查的说明。' * 30;
        }
        return profileResponse(v);
      });
      addTearDown(client.close);
      final semantics = t.ensureSemantics();
      await t.pumpWidget(
        profileHarness(auth, client, scale: 3, ime: 260, dark: dark),
      );
      await t.pumpAndSettle();
      for (final title in ['偏好', '地点', '活动', '社群', '智能体设置']) {
        await t.scrollUntilVisible(
          find.text(title),
          160,
          maxScrolls: 160,
          scrollable: find.byType(Scrollable).first,
        );
        await t.pumpAndSettle();
        expect(t.takeException(), isNull);
      }
      await t.scrollUntilVisible(
        find.text('模型出口与预算'),
        160,
        maxScrolls: 100,
        scrollable: find.byType(Scrollable).first,
      );
      final button = find.widgetWithText(OutlinedButton, '模型出口与预算');
      await t.ensureVisible(button);
      await t.pumpAndSettle();
      expect(t.getSize(button).height, greaterThanOrEqualTo(48));
      expect(
        t
            .getSemantics(button)
            .getSemanticsData()
            .hasAction(SemanticsAction.tap),
        true,
      );
      expect(t.takeException(), isNull);
      await profileCapture(
        t,
        'profile-320-font3-ime260-${dark ? 'dark' : 'light'}',
      );
      semantics.dispose();
    });
  }
  testWidgets('隐藏source不显示旧名和截断不声称完整', (t) async {
    final auth = SeedTestAuth()..owner = profileOwner;
    addTearDown(auth.dispose);
    final client = MockClient((r) async {
      final v = profileData(r.url.path);
      if (r.url.path.endsWith('community-interests')) {
        v['records'][0]['sourceAvailable'] = false;
        v['records'][0]['name'] = '绝不可显示私密CANARY';
        v['truncated'] = true;
      }
      return profileResponse(v);
    });
    addTearDown(client.close);
    await t.pumpWidget(profileHarness(auth, client));
    await t.pumpAndSettle();
    await t.scrollUntilVisible(
      find.text('社群'),
      180,
      maxScrolls: 100,
      scrollable: find.byType(Scrollable).first,
    );
    expect(find.textContaining('绝不可显示'), findsNothing);
    await t.scrollUntilVisible(
      find.textContaining('暂不能查看后续'),
      100,
      maxScrolls: 100,
      scrollable: find.byType(Scrollable).first,
    );
    expect(find.textContaining('暂不能查看后续'), findsOneWidget);
  });
  testWidgets('原管理页入口和身份切换清nested正文', (t) async {
    final auth = SeedTestAuth()..owner = profileOwner;
    addTearDown(auth.dispose);
    final client = MockClient(
      (r) async => profileResponse(profileData(r.url.path)),
    );
    addTearDown(client.close);
    await t.pumpWidget(profileHarness(auth, client));
    await t.pumpAndSettle();
    // Use a read-only existing page without SecureStore mutations.
    await profileTap(t, '管理报名的公开范围');
    expect(find.text('我的公开报名'), findsOneWidget);
    auth.changeIdentity('Bearer next', nextOwner: profileAgent);
    await t.pumpAndSettle();
    expect(find.textContaining('合成徒步报名'), findsNothing);
    expect(find.textContaining('工作身份或来源已变化'), findsOneWidget);
    expect(find.byType(AgentMemoryCorrectionPage), findsNothing);
  });
}
