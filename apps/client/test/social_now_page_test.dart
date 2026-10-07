import 'dart:async';
import 'package:birdtie_client/src/workspace/social_now_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'social_now_controller_test.dart';

Widget nowHarness(
  SeedTestAuth auth,
  MockClient client, {
  ValueNotifier<String?>? workspace,
  double scale = 1,
}) => MaterialApp(
  builder: (context, child) => MediaQuery(
    data: MediaQuery.of(context).copyWith(textScaler: TextScaler.linear(scale)),
    child: child!,
  ),
  home: SocialNowPage(
    auth: auth,
    client: client,
    apiBaseUrl: 'http://local',
    workspaceChanges: workspace,
    organizationWorkspaceID: () => workspace?.value,
  ),
);
void main() {
  testWidgets('活动打开重读机会与当前详情，撤来源不进入；换工作身份关闭真实详情', (tester) async {
    final auth = SeedTestAuth()..owner = nowOwner;
    final workspace = ValueNotifier<String?>(null);
    var available = true, opportunities = 0, details = 0;
    final calls = <String>[];
    final client = MockClient((r) async {
      calls.add(r.method);
      if (r.url.path == '/v1/me/opportunities') {
        opportunities++;
        return nowResponse(
          available ? [nowActivity(60)] : [],
          opportunities: true,
        );
      }
      if (r.url.path == '/v1/activities/${nowID(60)}') {
        details++;
        return nowResponse({
          'id': nowID(60),
          'title': '已核对活动详情',
          'placeName': '公开地点',
          'startsAt': '2099-01-01T01:00:00Z',
          'endsAt': '2099-01-01T03:00:00Z',
          'timeZone': 'UTC',
          'status': 'upcoming',
          'source': {},
        });
      }
      if (r.url.path.endsWith('/participations/me')) return nowResponse(null);
      return nowEmpty(r);
    });
    await tester.pumpWidget(nowHarness(auth, client, workspace: workspace));
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(find.text('活动 60'), 200);
    available = false;
    await tester.tap(find.text('活动 60'));
    await tester.pumpAndSettle();
    expect(details, 0);
    await tester.drag(find.byType(ListView).first, const Offset(0, 1000));
    await tester.pumpAndSettle();
    expect(find.textContaining('推荐来源已变化'), findsOneWidget);
    available = true;
    await tester.tap(find.byTooltip('刷新社交近况'));
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(find.text('活动 60'), -200);
    await tester.tap(find.text('活动 60'));
    await tester.pumpAndSettle();
    // Existing ActivityDetailSheet also resolves the live entity itself.
    expect(details, 2);
    expect(opportunities, 4);
    expect(find.text('已核对活动详情'), findsOneWidget);
    workspace.value = 'org';
    await tester.pumpAndSettle();
    expect(find.text('已核对活动详情'), findsNothing);
    expect(find.textContaining('个人身份'), findsOneWidget);
    expect(calls, everyElement('GET'));
    await tester.pumpWidget(const SizedBox());
    workspace.dispose();
    auth.dispose();
  });
  testWidgets('中文有限分组与具体意图详情，当前source GET，零隐式写/私密信息', (tester) async {
    final auth = SeedTestAuth()..owner = nowOwner;
    final calls = <String>[];
    final client = MockClient((r) async {
      calls.add('${r.method} ${r.url.path}');
      if (r.url.path == '/v1/me/ties') return nowResponse([nowTie()]);
      if (r.url.path == '/v1/social-intents') {
        return nowResponse([nowIntent(30)]);
      }
      if (r.url.path.endsWith(nowID(30))) return nowResponse(nowIntent(30));
      return nowEmpty(r);
    });
    await tester.pumpWidget(nowHarness(auth, client));
    await tester.pumpAndSettle();
    expect(find.text('好友明确分享的意图'), findsOneWidget);
    expect(find.textContaining('private'), findsNothing);
    await tester.tap(find.text('好友想一起散步 30'));
    await tester.pumpAndSettle();
    expect(find.text('好友分享的意图'), findsOneWidget);
    expect(calls, contains('GET /v1/social-intents/${nowID(30)}'));
    expect(calls, everyElement(startsWith('GET ')));
    await tester.pageBack();
    await tester.pumpAndSettle();
    expect(find.text('我的社交近况'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    auth.dispose();
  });
  testWidgets('当前source拒绝清空；账号与组织切换关闭详情/ABA不能恢复', (tester) async {
    final auth = SeedTestAuth()..owner = nowOwner;
    final workspace = ValueNotifier<String?>(null);
    var reject = false;
    final client = MockClient((r) async {
      if (r.url.path == '/v1/me/ties') return nowResponse([nowTie()]);
      if (r.url.path == '/v1/social-intents') {
        return nowResponse([nowIntent(30)]);
      }
      if (r.url.path.endsWith(nowID(30))) {
        return reject ? http.Response('', 403) : nowResponse(nowIntent(30));
      }
      return nowEmpty(r);
    });
    await tester.pumpWidget(nowHarness(auth, client, workspace: workspace));
    await tester.pumpAndSettle();
    await tester.tap(find.text('好友想一起散步 30'));
    await tester.pumpAndSettle();
    workspace.value = 'org';
    await tester.pumpAndSettle();
    expect(find.text('好友分享的意图'), findsNothing);
    expect(find.textContaining('个人身份'), findsOneWidget);
    workspace.value = null;
    await tester.pumpAndSettle();
    reject = true;
    await tester.tap(find.text('好友想一起散步 30'));
    await tester.pumpAndSettle();
    expect(find.textContaining('当前不可查看'), findsOneWidget);
    expect(find.text('好友想一起散步 30'), findsNothing);
    auth.changeIdentity(null);
    await tester.pumpAndSettle();
    expect(find.textContaining('个人身份'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    workspace.dispose();
    auth.dispose();
  });
  testWidgets('320px/2倍大字/键盘/语义，有限列表底部管理入口可达', (tester) async {
    final auth = SeedTestAuth()..owner = nowOwner;
    tester.view.physicalSize = const Size(320, 640);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final semantics = tester.ensureSemantics();
    final client = MockClient(
      (r) async => r.url.path == '/v1/me/opportunities'
          ? nowResponse([
              nowActivity(60),
              nowActivity(70, tier: 'SHARED_COMMUNITY'),
              nowActivity(71, tier: 'PUBLIC'),
            ], opportunities: true)
          : nowEmpty(r),
    );
    await tester.pumpWidget(nowHarness(auth, client, scale: 2));
    await tester.pumpAndSettle();
    expect(find.byTooltip('刷新社交近况'), findsOneWidget);
    await tester.sendKeyEvent(LogicalKeyboardKey.tab);
    await tester.pump();
    expect(tester.takeException(), isNull);
    await tester.scrollUntilVisible(find.text('管理我的活动意图'), 300);
    await tester.pumpAndSettle();
    expect(find.text('管理我的活动意图').hitTestable(), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
    semantics.dispose();
    auth.dispose();
  });
  testWidgets('读取迟到在dispose后不显示且没有异常', (tester) async {
    final auth = SeedTestAuth()..owner = nowOwner;
    final late = Completer<http.Response>();
    final client = MockClient(
      (r) async => r.url.path == '/v1/me/ties' ? late.future : nowEmpty(r),
    );
    await tester.pumpWidget(nowHarness(auth, client));
    await tester.pump();
    await tester.pumpWidget(const SizedBox());
    late.complete(nowResponse([nowTie()]));
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    auth.dispose();
  });
}
