import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/content/agent_seed_sheet.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_controller_test.dart'
    show seedJson, seedResponse, SeedBorrowedClient;

class SeedTestAuth extends BirdtieAuthController {
  String? token = 'Bearer owner';
  String owner = 'owner';
  String label = '原昵称';
  @override
  String? get authorizationHeader => token;
  @override
  String? get accountID => token == null ? null : owner;
  @override
  bool get signedIn => token != null;
  @override
  String? get displayName => label;
  @override
  void updateProfileDisplayName(String value) {
    label = value;
    notifyListeners();
  }

  void changeIdentity(String? value, {String nextOwner = 'peer'}) {
    token = value;
    owner = nextOwner;
    notifyListeners();
  }
}

class SeedWorkspaceNotifier extends ValueNotifier<String?> {
  SeedWorkspaceNotifier() : super(null);
  bool get listening => hasListeners;
}

Widget seedHarness(
  SeedTestAuth auth,
  MockClient client, {
  double scale = 1,
  bool onlyMissing = false,
  ValueNotifier<String?>? workspace,
}) => MaterialApp(
  builder: (context, child) => MediaQuery(
    data: MediaQuery.of(context).copyWith(textScaler: TextScaler.linear(scale)),
    child: child!,
  ),
  home: Builder(
    builder: (context) => Scaffold(
      body: TextButton(
        onPressed: () => Navigator.push(
          context,
          MaterialPageRoute<bool>(
            builder: (_) => AgentSeedSheet(
              auth: auth,
              client: client,
              apiBaseUrl: 'http://local',
              onlyMissing: onlyMissing,
              workspaceChanges: workspace,
              organizationWorkspaceID: workspace == null
                  ? null
                  : () => workspace.value,
            ),
          ),
        ),
        child: const Text('打开初始设置'),
      ),
    ),
  ),
);

void main() {
  for (final changed in [
    'client',
    'base',
    'listener',
    'workspaceGetter',
    'onlyMissing',
    'progressive',
  ]) {
    testWidgets('同key单项$changed重绑永久退役并解除真正旧监听', (tester) async {
      final auth = SeedTestAuth();
      final first = SeedWorkspaceNotifier(), second = SeedWorkspaceNotifier();
      addTearDown(auth.dispose);
      addTearDown(first.dispose);
      addTearDown(second.dispose);
      var requests = 0;
      final a = SeedBorrowedClient((_) async {
        requests++;
        return seedResponse(seedJson());
      });
      final b = SeedBorrowedClient((_) async {
        requests++;
        return seedResponse(seedJson());
      });
      String? originalWorkspace() => first.value;
      String? replacementWorkspace() => first.value;
      Widget frame(bool replacement) => MaterialApp(
        home: AgentSeedSheet(
          key: const ValueKey('seed-binding'),
          auth: auth,
          client: replacement && changed == 'client' ? b : a,
          apiBaseUrl: replacement && changed == 'base'
              ? 'http://peer'
              : 'http://local',
          workspaceChanges: replacement && changed == 'listener'
              ? second
              : first,
          organizationWorkspaceID: replacement && changed == 'workspaceGetter'
              ? replacementWorkspace
              : originalWorkspace,
          onlyMissing: replacement && changed == 'onlyMissing',
          progressive: replacement && changed == 'progressive',
        ),
      );
      await tester.pumpWidget(frame(false));
      await tester.pumpAndSettle();
      expect(first.listening, true);
      await tester.pumpWidget(frame(true));
      await tester.pumpAndSettle();
      expect(find.text('设置入口已变化，请返回后重新打开。'), findsOneWidget);
      expect(first.listening, false);
      expect(second.listening, false);
      auth.changeIdentity('Bearer peer');
      first.value = 'org';
      second.value = 'org';
      await tester.pumpAndSettle();
      await tester.pumpWidget(frame(false));
      await tester.pumpAndSettle();
      expect(find.text('设置入口已变化，请返回后重新打开。'), findsOneWidget);
      expect(requests, 1);
      expect(a.closes, 0);
      expect(b.closes, 0);
      await tester.pumpWidget(const SizedBox());
      expect(a.closes, 0);
      expect(b.closes, 0);
      expect(tester.takeException(), isNull);
      a.close();
      b.close();
    });
  }

  testWidgets('同key重绑后晚GET不恢复旧正文，真正重新进入读取新endpoint', (tester) async {
    final auth = SeedTestAuth();
    addTearDown(auth.dispose);
    final pending = Completer<http.Response>();
    var oldReads = 0, newReads = 0;
    final a = MockClient((_) {
      oldReads++;
      return pending.future;
    });
    final b = MockClient((r) async {
      newReads++;
      expect(r.url.host, 'new-endpoint');
      return seedResponse(seedJson(displayName: '新当前资料'));
    });
    Widget frame(MockClient client, String base, String key) => MaterialApp(
      home: AgentSeedSheet(
        key: ValueKey(key),
        auth: auth,
        client: client,
        apiBaseUrl: base,
      ),
    );
    await tester.pumpWidget(frame(a, 'http://old-endpoint', 'same'));
    await tester.pump();
    await tester.pumpWidget(frame(b, 'http://new-endpoint', 'same'));
    pending.complete(seedResponse(seedJson(displayName: '旧晚私密资料')));
    await tester.pumpAndSettle();
    expect(find.text('旧晚私密资料'), findsNothing);
    expect(newReads, 0);
    await tester.pumpWidget(frame(b, 'http://new-endpoint', 'fresh'));
    await tester.pumpAndSettle();
    expect(find.text('新当前资料'), findsOneWidget);
    expect(oldReads, 1);
    expect(newReads, 1);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  for (final progressive in [false, true]) {
    testWidgets('同key重绑不把新身份凭据发到旧endpoint：progressive=$progressive', (
      tester,
    ) async {
      final a = SeedTestAuth();
      final b = SeedTestAuth()
        ..changeIdentity('Bearer peer', nextOwner: 'peer');
      addTearDown(a.dispose);
      addTearDown(b.dispose);
      final oldRequests = <http.Request>[];
      var newRequests = 0;
      final oldClient = MockClient((r) async {
        oldRequests.add(r);
        return http.Response('{}', 503);
      });
      final newClient = MockClient((r) async {
        newRequests++;
        return seedResponse(seedJson(owner: 'peer'));
      });
      Widget frame(SeedTestAuth auth, MockClient client, String base) =>
          MaterialApp(
            home: AgentSeedSheet(
              key: const ValueKey('same-seed'),
              auth: auth,
              client: client,
              apiBaseUrl: base,
              progressive: progressive,
            ),
          );
      await tester.pumpWidget(frame(a, oldClient, 'http://old-endpoint'));
      await tester.pumpAndSettle();
      expect(oldRequests.single.headers['Authorization'], 'Bearer owner');
      await tester.pumpWidget(frame(b, newClient, 'http://new-endpoint'));
      await tester.pumpAndSettle();
      // On the old implementation this real UI action sends B's credential
      // with A's fixed client/base. A retired page must have no retry action.
      final retry = find.text('重新读取（保留草稿）');
      if (retry.evaluate().isNotEmpty) {
        await tester.ensureVisible(retry);
        await tester.tap(retry);
        await tester.pumpAndSettle();
      }
      expect(
        oldRequests.where((r) => r.headers['Authorization'] == 'Bearer peer'),
        isEmpty,
      );
      expect(newRequests, 0);
      expect(find.text('设置入口已变化，请返回后重新打开。'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    });
  }

  testWidgets('同key重绑清除旧私密源，A-B-A不复活旧页', (tester) async {
    final a = SeedTestAuth(), b = SeedTestAuth();
    addTearDown(a.dispose);
    addTearDown(b.dispose);
    var requests = 0;
    final client = MockClient((_) async {
      requests++;
      return seedResponse(seedJson(displayName: 'A私密昵称'));
    });
    Widget frame(SeedTestAuth auth) => MaterialApp(
      home: AgentSeedSheet(
        key: const ValueKey('same-seed'),
        auth: auth,
        client: client,
        apiBaseUrl: 'http://local',
      ),
    );
    await tester.pumpWidget(frame(a));
    await tester.pumpAndSettle();
    expect(find.text('A私密昵称'), findsOneWidget);
    await tester.pumpWidget(frame(b));
    await tester.pumpAndSettle();
    expect(find.text('A私密昵称'), findsNothing);
    expect(find.text('设置入口已变化，请返回后重新打开。'), findsOneWidget);
    await tester.pumpWidget(frame(a));
    await tester.pumpAndSettle();
    expect(find.text('A私密昵称'), findsNothing);
    expect(requests, 1);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets('已获准的昵称城市不再重复询问；组织切换清草稿，切回必须重新读取', (tester) async {
    final auth = SeedTestAuth();
    final workspace = ValueNotifier<String?>(null);
    var reads = 0;
    final client = MockClient((_) async {
      reads++;
      return seedResponse(seedJson(city: 'aberdeen-gb'));
    });
    await tester.pumpWidget(
      seedHarness(auth, client, onlyMissing: true, workspace: workspace),
    );
    await tester.tap(find.text('打开初始设置'));
    await tester.pumpAndSettle();
    expect(find.byType(TextFormField), findsNothing);
    expect(find.text('交流语言（不会改变界面语言）'), findsOneWidget);
    await tester.tap(find.text('简体中文'));
    workspace.value = 'organization-resource-id';
    await tester.pump();
    expect(find.text('请切回个人身份后再完善初始设置。'), findsOneWidget);
    expect(find.text('交流语言（不会改变界面语言）'), findsNothing);
    workspace.value = null;
    await tester.pumpAndSettle();
    expect(reads, 2);
    final chip = tester.widget<FilterChip>(
      find.widgetWithText(FilterChip, '简体中文'),
    );
    expect(chip.selected, false);
    await tester.pumpWidget(const SizedBox());
    workspace.dispose();
    auth.dispose();
  });
  testWidgets('分步每轮最多两项，中文草稿可保存，兴趣可跳过且不以首城预填', (tester) async {
    final auth = SeedTestAuth();
    addTearDown(auth.dispose);
    Map<String, dynamic>? sent;
    final client = MockClient((r) async {
      if (r.method == 'PUT') {
        sent = jsonDecode(r.body) as Map<String, dynamic>;
        return seedResponse(
          seedJson(
            snapshot: 'c',
            progress: 'COMPLETED',
            intent: 'JUST_EXPLORE',
            city: 'aberdeen-gb',
            languages: ['zh-CN'],
          ),
        );
      }
      return seedResponse(seedJson());
    });
    await tester.pumpWidget(seedHarness(auth, client));
    await tester.tap(find.text('打开初始设置'));
    await tester.pumpAndSettle();
    expect(find.text('交流语言（不会改变界面语言）'), findsNothing);
    expect(find.text('本人声明的当前城市'), findsOneWidget);
    expect(find.text('阿伯丁'), findsNothing);
    await tester.tap(find.text('下一步'));
    await tester.pumpAndSettle();
    expect(find.text('请选择当前仍可用的城市，也可以稍后再说。'), findsOneWidget);
    await tester.tap(find.byType(DropdownButtonFormField<String>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('阿伯丁').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('下一步'));
    await tester.pumpAndSettle();
    expect(find.byType(TextFormField), findsNothing);
    await tester.tap(find.text('简体中文'));
    await tester.tap(find.byType(DropdownButtonFormField<String>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('先随便看看').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('下一步'));
    await tester.pumpAndSettle();
    expect(find.text('跳过初始兴趣'), findsOneWidget);
    expect(find.text('羽毛球'), findsNothing);
    await tester.tap(find.text('下一步'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('保存这些设置'));
    await tester.pumpAndSettle();
    expect(sent!['basicIntent'], 'JUST_EXPLORE');
    expect(sent!['interestChoice'], 'SKIP');
    expect(sent!['interests'], isEmpty);
    expect(sent!['currentCitySnapshot'], List.filled(64, 'b').join());
    expect(find.text('打开初始设置'), findsOneWidget);
  });
  testWidgets('稍后只保存进度；取消编辑不写；账号切换清除IME草稿', (tester) async {
    final auth = SeedTestAuth();
    addTearDown(auth.dispose);
    final writes = <Map<String, dynamic>>[];
    final client = MockClient((r) async {
      if (r.method == 'PUT') {
        writes.add(jsonDecode(r.body) as Map<String, dynamic>);
        return seedResponse(seedJson(progress: 'DEFERRED', owner: auth.owner));
      }
      return seedResponse(seedJson(owner: auth.owner));
    });
    await tester.pumpWidget(seedHarness(auth, client));
    await tester.tap(find.text('打开初始设置'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextFormField), '尚未批准的名字');
    auth.changeIdentity('Bearer peer');
    await tester.pumpAndSettle();
    expect(find.text('尚未批准的名字'), findsNothing);
    expect(writes, isEmpty);
    await tester.tap(find.text('稍后再说'));
    await tester.pumpAndSettle();
    expect(writes.single.keys.toSet(), {'expectedSnapshot', 'action'});
    expect(writes.single['action'], 'DEFER');
    expect(writes.single.containsKey('basicIntent'), false);
    await tester.tap(find.text('打开初始设置'));
    await tester.pumpAndSettle();
    await tester.pageBack();
    await tester.pumpAndSettle();
    expect(writes.length, 1);
  });
  testWidgets('小屏大字键盘可滚动，中文组合态不自动推进，语义标签保留', (tester) async {
    tester.view.physicalSize = const Size(320, 740);
    tester.view.devicePixelRatio = 1;
    tester.view.viewInsets = const FakeViewPadding(bottom: 240);
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetViewInsets);
    final auth = SeedTestAuth();
    addTearDown(auth.dispose);
    final semantics = tester.ensureSemantics();
    final client = MockClient((_) async => seedResponse(seedJson()));
    await tester.pumpWidget(seedHarness(auth, client, scale: 2));
    await tester.tap(find.text('打开初始设置'));
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.byType(TextFormField));
    await tester.tap(find.byType(TextFormField));
    await tester.pump();
    tester.testTextInput.updateEditingValue(
      const TextEditingValue(
        text: 'zhongwen',
        composing: TextRange(start: 0, end: 8),
      ),
    );
    await tester.pump();
    await tester.ensureVisible(find.text('下一步'));
    await tester.tap(find.text('下一步'));
    await tester.pump();
    expect(find.text('交流语言（不会改变界面语言）'), findsNothing);
    expect(find.text('zhongwen'), findsOneWidget);
    expect(tester.takeException(), isNull);
    expect(find.bySemanticsLabel(RegExp('昵称')), findsWidgets);
    tester.testTextInput.updateEditingValue(
      const TextEditingValue(text: '中文昵称'),
    );
    await tester.pump();
    await tester.ensureVisible(find.text('稍后再说'));
    expect(find.text('稍后再说'), findsOneWidget);
    expect(tester.takeException(), isNull);
    semantics.dispose();
  });
}
