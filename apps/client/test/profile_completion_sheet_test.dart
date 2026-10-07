import 'dart:async';
import 'dart:convert';
import 'dart:ui' show Tristate, SemanticsAction;
import 'package:birdtie_client/src/content/agent_seed_sheet.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_controller_test.dart' show seedJson, seedResponse;
import 'agent_seed_sheet_test.dart' show SeedTestAuth;

Widget completionHarness(
  SeedTestAuth auth,
  MockClient client, {
  double scale = 1,
  ValueNotifier<String?>? workspace,
}) => MaterialApp(
  builder: (context, child) => MediaQuery(
    data: MediaQuery.of(context).copyWith(textScaler: TextScaler.linear(scale)),
    child: child!,
  ),
  home: AgentSeedSheet(
    auth: auth,
    client: client,
    apiBaseUrl: 'http://local',
    progressive: true,
    workspaceChanges: workspace,
    organizationWorkspaceID: workspace == null ? null : () => workspace.value,
  ),
);

Map<String, dynamic> completeSeed({
  String snapshot = 'a',
  List<String> interests = const ['原兴趣'],
}) => seedJson(
  snapshot: snapshot,
  city: 'aberdeen-gb',
  languages: ['zh-CN'],
  intent: 'JUST_EXPLORE',
  progress: 'COMPLETED',
  interests: interests,
);

Future<void> completionTap(WidgetTester tester, String label) async {
  await tester.ensureVisible(find.text(label).last);
  await tester.tap(find.text(label).last);
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('渐进入口提供本人已确认记忆单项补齐', (tester) async {
    final auth = SeedTestAuth();
    addTearDown(auth.dispose);
    final client = MockClient((_) async => seedResponse(completeSeed()));
    await tester.pumpWidget(completionHarness(auth, client));
    await tester.pumpAndSettle();
    expect(find.text('用已确认记忆补齐活动偏好'), findsOneWidget);
  });
  for (final responseCode in [200, 503]) {
    testWidgets('渐进保存晚$responseCode回执在同key重绑后不得恢复或自动重发', (tester) async {
      final a = SeedTestAuth(), b = SeedTestAuth();
      addTearDown(a.dispose);
      addTearDown(b.dispose);
      final pending = Completer<http.Response>();
      var oldWrites = 0, newRequests = 0;
      final old = MockClient((r) async {
        if (r.method == 'PUT') {
          oldWrites++;
          return pending.future;
        }
        return seedResponse(completeSeed());
      });
      final replacement = MockClient((_) async {
        newRequests++;
        return seedResponse(completeSeed());
      });
      Widget frame(SeedTestAuth auth, MockClient client) => MaterialApp(
        home: AgentSeedSheet(
          key: const ValueKey('same-progressive'),
          auth: auth,
          client: client,
          apiBaseUrl: 'http://local',
          progressive: true,
        ),
      );
      await tester.pumpWidget(frame(a, old));
      await tester.pumpAndSettle();
      await completionTap(tester, '私密兴趣');
      await completionTap(tester, '羽毛球');
      await completionTap(tester, '下一步');
      await completionTap(tester, '保存这些设置');
      expect(oldWrites, 1);
      await tester.pumpWidget(frame(b, replacement));
      pending.complete(
        responseCode == 200
            ? seedResponse({
                ...completeSeed(interests: ['羽毛球']),
                'displayName': '迟到保存昵称',
              })
            : http.Response('{}', responseCode),
      );
      await tester.pumpAndSettle();
      await tester.pumpWidget(frame(a, old));
      await tester.pumpAndSettle();
      expect(find.text('设置入口已变化，请返回后重新打开。'), findsOneWidget);
      expect(find.text('读取当前设置核实'), findsNothing);
      expect(find.text('保存这些设置'), findsNothing);
      expect(a.label, '原昵称');
      expect(b.label, '原昵称');
      expect(oldWrites, 1);
      expect(newRequests, 0);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    });
  }

  testWidgets('已未知渐进保存重绑不复活批准或未知正文，重新进入仅GET当前结果', (tester) async {
    final auth = SeedTestAuth();
    addTearDown(auth.dispose);
    var writes = 0, reads = 0;
    final a = MockClient((r) async {
      if (r.method == 'PUT') {
        writes++;
        return http.Response('{}', 503);
      }
      return seedResponse(completeSeed());
    });
    final b = MockClient((r) async {
      expect(r.method, 'GET');
      reads++;
      return seedResponse(completeSeed(interests: ['已存在的当前兴趣']));
    });
    Widget frame(MockClient client, String key) => MaterialApp(
      home: AgentSeedSheet(
        key: ValueKey(key),
        auth: auth,
        client: client,
        apiBaseUrl: 'http://local',
        progressive: true,
      ),
    );
    await tester.pumpWidget(frame(a, 'same'));
    await tester.pumpAndSettle();
    await completionTap(tester, '私密兴趣');
    await completionTap(tester, '羽毛球');
    await completionTap(tester, '下一步');
    await completionTap(tester, '保存这些设置');
    expect(find.text('读取当前设置核实'), findsOneWidget);
    await tester.pumpWidget(frame(b, 'same'));
    await tester.pumpAndSettle();
    expect(find.text('读取当前设置核实'), findsNothing);
    expect(reads, 0);
    await tester.pumpWidget(frame(a, 'same'));
    await tester.pumpAndSettle();
    expect(find.text('保存这些设置'), findsNothing);
    expect(writes, 1);
    await tester.pumpWidget(frame(b, 'fresh'));
    await tester.pumpAndSettle();
    await completionTap(tester, '私密兴趣');
    expect(find.text('已存在的当前兴趣'), findsOneWidget);
    expect(find.text('已核实：当前设置与你提交的内容一致。'), findsNothing);
    expect(reads, 1);
    expect(writes, 1);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('退役中文页320px三倍字键盘可滚动返回，48dp真实语义操作', (tester) async {
    tester.view.physicalSize = const Size(320, 740);
    tester.view.devicePixelRatio = 1;
    tester.view.viewInsets = const FakeViewPadding(bottom: 240);
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetViewInsets);
    final auth = SeedTestAuth();
    addTearDown(auth.dispose);
    final semantics = tester.ensureSemantics();
    var writes = 0;
    final a = MockClient((r) async {
      if (r.method == 'PUT') writes++;
      return seedResponse(completeSeed());
    });
    final b = MockClient((_) async => seedResponse(completeSeed()));
    http.Client client = a;
    late StateSetter rebind;
    await tester.pumpWidget(
      MaterialApp(
        builder: (c, child) => MediaQuery(
          data: MediaQuery.of(c).copyWith(textScaler: TextScaler.linear(3)),
          child: child!,
        ),
        home: Builder(
          builder: (context) => Scaffold(
            body: TextButton(
              onPressed: () => Navigator.of(context).push(
                MaterialPageRoute<void>(
                  builder: (_) => StatefulBuilder(
                    builder: (c, update) {
                      rebind = update;
                      return AgentSeedSheet(
                        key: const ValueKey('retired'),
                        auth: auth,
                        client: client,
                        apiBaseUrl: 'http://local',
                        progressive: true,
                      );
                    },
                  ),
                ),
              ),
              child: const Text('打开完善'),
            ),
          ),
        ),
      ),
    );
    await completionTap(tester, '打开完善');
    rebind(() => client = b);
    await tester.pumpAndSettle();
    expect(find.text('设置入口已变化，请返回后重新打开。'), findsOneWidget);
    final back = find.widgetWithText(FilledButton, '返回设置');
    await tester.ensureVisible(back);
    await tester.pumpAndSettle();
    final node = tester.getSemantics(back);
    expect(node.getSemanticsData().hasAction(SemanticsAction.tap), true);
    expect(node.rect.height, greaterThanOrEqualTo(48));
    expect(tester.takeException(), isNull);
    await tester.tap(back);
    await tester.pumpAndSettle();
    expect(find.text('打开完善'), findsOneWidget);
    expect(find.byType(AgentSeedSheet), findsNothing);
    expect(writes, 0);
    expect(tester.takeException(), isNull);
    semantics.dispose();
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets('已完整当前源只改兴趣；review披露原CITY私密化和全量保存', (tester) async {
    final auth = SeedTestAuth();
    addTearDown(auth.dispose);
    Map<String, dynamic>? sent;
    final client = MockClient((request) async {
      if (request.method == 'GET') return seedResponse(completeSeed());
      sent = jsonDecode(request.body) as Map<String, dynamic>;
      return seedResponse(
        completeSeed(
          snapshot: 'c',
          interests: List<String>.from(sent!['interests'] as List),
        ),
      );
    });
    await tester.pumpWidget(completionHarness(auth, client));
    await tester.pumpAndSettle();
    expect(find.text('这次想完善哪一项？'), findsOneWidget);
    await completionTap(tester, '私密兴趣');
    expect(find.text('昵称'), findsNothing);
    expect(find.text('这次来到 Birdtie 想做什么？'), findsNothing);
    await completionTap(tester, '羽毛球');
    await completionTap(tester, '下一步');
    expect(find.textContaining('复用已有城市也会'), findsOneWidget);
    expect(sent, isNull);
    await completionTap(tester, '保存这些设置');
    expect(sent!['displayName'], '原昵称');
    expect(sent!['languagePreferences'], ['zh-CN']);
    expect(sent!['basicIntent'], 'JUST_EXPLORE');
    expect(sent!['interests'], containsAll(['原兴趣', '羽毛球']));
    expect(sent!['interestChoice'], 'SET');
  });

  testWidgets('缺项按一组一组补，不制造城市；取消不写DEFER或草稿', (tester) async {
    final auth = SeedTestAuth();
    addTearDown(auth.dispose);
    var writes = 0;
    final client = MockClient((request) async {
      if (request.method == 'PUT') writes++;
      return seedResponse(seedJson());
    });
    await tester.pumpWidget(completionHarness(auth, client));
    await tester.pumpAndSettle();
    await completionTap(tester, '私密兴趣');
    await completionTap(tester, '下一步');
    expect(find.text('本人声明的当前城市'), findsOneWidget);
    expect(find.text('阿伯丁'), findsNothing);
    await completionTap(tester, '下一步');
    expect(find.textContaining('请选择当前仍可用的城市'), findsOneWidget);
    expect(writes, 0);
    expect(find.text('稍后再说'), findsNothing);
    await completionTap(tester, '取消，保留当前设置');
    expect(writes, 0);
  });

  testWidgets('冲突重读更新非本组源，保留本人兴趣草稿并要求新review', (tester) async {
    final auth = SeedTestAuth();
    addTearDown(auth.dispose);
    var reads = 0;
    var writes = 0;
    Map<String, dynamic>? finalSent;
    final client = MockClient((request) async {
      if (request.method == 'GET') {
        reads++;
        final raw = completeSeed(snapshot: reads == 1 ? 'a' : 'b');
        if (reads > 1) {
          raw['displayName'] = '新昵称';
          raw['languagePreferences'] = ['en'];
        }
        return seedResponse(raw);
      }
      writes++;
      if (writes == 1) return http.Response('{}', 409);
      finalSent = jsonDecode(request.body) as Map<String, dynamic>;
      final raw = completeSeed(
        snapshot: 'c',
        interests: List<String>.from(finalSent!['interests'] as List),
      );
      raw['displayName'] = '新昵称';
      raw['languagePreferences'] = ['en'];
      return seedResponse(raw);
    });
    await tester.pumpWidget(completionHarness(auth, client));
    await tester.pumpAndSettle();
    await completionTap(tester, '私密兴趣');
    await completionTap(tester, '羽毛球');
    await completionTap(tester, '下一步');
    await completionTap(tester, '保存这些设置');
    await completionTap(tester, '重新读取（保留草稿）');
    expect(find.text('这次想完善哪一项？'), findsOneWidget);
    await completionTap(tester, '私密兴趣');
    await completionTap(tester, '下一步');
    expect(find.text('昵称：新昵称'), findsOneWidget);
    expect(writes, 1);
    await completionTap(tester, '保存这些设置');
    expect(finalSent!['expectedSnapshot'], List.filled(64, 'b').join());
    expect(finalSent!['languagePreferences'], ['en']);
    expect(finalSent!['interests'], contains('羽毛球'));
  });

  testWidgets('提交结果未知禁止盲重发；重新读取后仅报告当前设置一致', (tester) async {
    final auth = SeedTestAuth();
    addTearDown(auth.dispose);
    var writes = 0;
    var reads = 0;
    final client = MockClient((request) async {
      if (request.method == 'GET') {
        reads++;
        return seedResponse(
          completeSeed(
            snapshot: reads == 1 ? 'a' : 'b',
            interests: reads == 1 ? ['原兴趣'] : ['原兴趣', '羽毛球'],
          ),
        );
      }
      writes++;
      return http.Response('{}', 503);
    });
    await tester.pumpWidget(completionHarness(auth, client));
    await tester.pumpAndSettle();
    await completionTap(tester, '私密兴趣');
    await completionTap(tester, '羽毛球');
    await completionTap(tester, '下一步');
    await completionTap(tester, '保存这些设置');
    expect(writes, 1);
    expect(
      tester
          .widget<FilledButton>(find.widgetWithText(FilledButton, '保存这些设置'))
          .onPressed,
      isNull,
    );
    await completionTap(tester, '读取当前设置核实');
    expect(find.text('已核实：当前设置与你提交的内容一致。'), findsOneWidget);
    expect(find.text('这次想完善哪一项？'), findsOneWidget);
    expect(writes, 1);
  });

  testWidgets('换主体ABA与迟到GET清理；320px大字键盘和选择语义可操作', (tester) async {
    tester.view.physicalSize = const Size(320, 740);
    tester.view.devicePixelRatio = 1;
    tester.view.viewInsets = const FakeViewPadding(bottom: 180);
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetViewInsets);
    final auth = SeedTestAuth();
    final workspace = ValueNotifier<String?>(null);
    addTearDown(auth.dispose);
    addTearDown(workspace.dispose);
    final semantics = tester.ensureSemantics();
    final pending = Completer<http.Response>();
    var reads = 0;
    final client = MockClient((_) async {
      reads++;
      return reads == 1 ? pending.future : seedResponse(completeSeed());
    });
    await tester.pumpWidget(
      completionHarness(auth, client, scale: 2, workspace: workspace),
    );
    await tester.pump();
    workspace.value = 'organization';
    pending.complete(seedResponse(completeSeed()));
    await tester.pumpAndSettle();
    expect(find.text('私密兴趣'), findsNothing);
    workspace.value = null;
    await tester.pumpAndSettle();
    await completionTap(tester, '私密兴趣');
    expect(
      tester
          .getSemantics(find.widgetWithText(FilterChip, '原兴趣'))
          .getSemanticsData()
          .flagsCollection
          .isSelected,
      Tristate.isTrue,
    );
    auth.changeIdentity(null);
    auth.changeIdentity('Bearer owner', nextOwner: 'owner');
    await tester.pumpAndSettle();
    expect(find.text('这次想完善哪一项？'), findsOneWidget);
    await completionTap(tester, '取消，保留当前设置');
    expect(tester.takeException(), isNull);
    semantics.dispose();
  });
}
