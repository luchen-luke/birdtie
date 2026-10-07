import 'dart:convert';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/content/social_preference_seed_sheet.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'social_preference_seed_controller_test.dart'
    show socialOwner, socialJson, socialFields, socialResponse;

class SocialTestAuth extends BirdtieAuthController {
  String? token = 'Bearer owner';
  String owner = socialOwner;
  @override
  String? get authorizationHeader => token;
  @override
  String? get accountID => token == null ? null : owner;
  @override
  bool get signedIn => token != null;
  @override
  String? get displayName => '本人测试昵称';
  void switchTo(
    String? next, {
    String nextOwner = '33333333-3333-4333-8333-333333333333',
  }) {
    token = next;
    owner = nextOwner;
    notifyListeners();
  }
}

Widget socialHarness(
  SocialTestAuth auth,
  http.Client client, {
  ValueNotifier<String?>? workspace,
  double scale = 1,
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
            builder: (_) => SocialPreferenceSeedSheet(
              auth: auth,
              client: client,
              apiBaseUrl: 'http://local',
              workspaceChanges: workspace,
              organizationWorkspaceID: workspace == null
                  ? null
                  : () => workspace.value,
            ),
          ),
        ),
        child: const Text('打开偏好'),
      ),
    ),
  ),
);
void main() {
  testWidgets('并发更新重读保留可见草稿，已删除旧描述必须纠正，不能覆盖新笔记', (tester) async {
    final auth = SocialTestAuth();
    var reads = 0, writes = 0;
    Map<String, dynamic>? sent;
    final client = MockClient((r) async {
      if (r.method == 'GET') {
        reads++;
        return socialResponse(
          socialJson(
            version: reads == 1 ? 2 : 4,
            fields: reads == 1
                ? socialFields()
                : {...socialFields(preferences: []), 'agentNotes': '新笔记'},
          ),
        );
      }
      writes++;
      if (writes == 1) return http.Response('{}', 409);
      sent = jsonDecode(r.body) as Map<String, dynamic>;
      return socialResponse(
        socialJson(version: 5, fields: sent!['fields'] as Map<String, dynamic>),
      );
    });
    await tester.pumpWidget(socialHarness(auth, client));
    await tester.tap(find.text('打开偏好'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('小群体'));
    await tester.tap(find.text('检查选择'));
    await tester.pump();
    await tester.tap(find.text('保存私密偏好'));
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('重新读取（保留草稿）'));
    await tester.tap(find.text('重新读取（保留草稿）'));
    await tester.pumpAndSettle();
    expect(find.text('部分原有描述已在别处删除。请取消这些项，再检查当前选择。'), findsOneWidget);
    expect(
      tester
          .widget<FilterChip>(find.widgetWithText(FilterChip, '原有明确描述'))
          .selected,
      true,
    );
    expect(
      tester
          .widget<FilledButton>(find.widgetWithText(FilledButton, '检查选择'))
          .onPressed,
      isNull,
    );
    await tester.tap(find.text('原有明确描述'));
    await tester.pump();
    await tester.tap(find.text('检查选择'));
    await tester.pump();
    await tester.tap(find.text('保存私密偏好'));
    await tester.pumpAndSettle();
    expect(sent!['expectedVersion'], 4);
    expect((sent!['fields'] as Map)['agentNotes'], '新笔记');
    expect((sent!['fields'] as Map)['socialPreferences'], ['小群体']);
    await tester.pumpWidget(const SizedBox());
    auth.dispose();
  });
  testWidgets('单个多选问题，默认保留真实旧描述，检查后才保存；其它八字段保持', (tester) async {
    final auth = SocialTestAuth();
    Map<String, dynamic>? sent;
    final client = MockClient((r) async {
      if (r.method == 'PUT') {
        sent = jsonDecode(r.body) as Map<String, dynamic>;
        return socialResponse(
          socialJson(
            version: 3,
            fields: sent!['fields'] as Map<String, dynamic>,
          ),
        );
      }
      return socialResponse(socialJson());
    });
    await tester.pumpWidget(socialHarness(auth, client));
    await tester.tap(find.text('打开偏好'));
    await tester.pumpAndSettle();
    expect(find.text('你更喜欢怎样认识人？'), findsOneWidget);
    expect(
      tester
          .widget<FilterChip>(find.widgetWithText(FilterChip, '原有明确描述'))
          .selected,
      true,
    );
    expect(
      tester
          .widget<FilterChip>(find.widgetWithText(FilterChip, '同一大学'))
          .selected,
      false,
    );
    await tester.tap(find.text('同一大学'));
    expect(sent, isNull);
    await tester.tap(find.text('检查选择'));
    await tester.pumpAndSettle();
    expect(find.text('检查你的选择'), findsOneWidget);
    expect(find.textContaining('不会核验学校身份'), findsOneWidget);
    expect(sent, isNull);
    await tester.tap(find.text('保存私密偏好'));
    await tester.pumpAndSettle();
    expect(sent!['expectedVersion'], 2);
    expect(
      (sent!['fields'] as Map)['agentNotes'],
      socialFields()['agentNotes'],
    );
    expect((sent!['fields'] as Map)['socialPreferences'], ['原有明确描述', '同一大学']);
    expect(find.text('打开偏好'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    auth.dispose();
  });
  testWidgets('跳过/取消不PUT，组织与账号切换清草稿和旧审核', (tester) async {
    final auth = SocialTestAuth();
    final workspace = ValueNotifier<String?>(null);
    var writes = 0;
    final client = MockClient((r) async {
      if (r.method == 'PUT') writes++;
      return socialResponse(socialJson(owner: auth.owner));
    });
    await tester.pumpWidget(socialHarness(auth, client, workspace: workspace));
    await tester.tap(find.text('打开偏好'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('小群体'));
    await tester.tap(find.text('检查选择'));
    await tester.pump();
    workspace.value = 'org-entity';
    await tester.pump();
    expect(find.text('请切回个人身份后再编辑社交偏好。'), findsOneWidget);
    workspace.value = null;
    await tester.pumpAndSettle();
    expect(
      tester
          .widget<FilterChip>(find.widgetWithText(FilterChip, '小群体'))
          .selected,
      false,
    );
    auth.switchTo('Bearer peer');
    await tester.pumpAndSettle();
    expect(
      tester
          .widget<FilterChip>(find.widgetWithText(FilterChip, '小群体'))
          .selected,
      false,
    );
    await tester.tap(find.text('跳过，保留已有偏好'));
    await tester.pumpAndSettle();
    expect(writes, 0);
    await tester.tap(find.text('打开偏好'));
    await tester.pumpAndSettle();
    await tester.pageBack();
    await tester.pumpAndSettle();
    expect(writes, 0);
    await tester.pumpWidget(const SizedBox());
    workspace.dispose();
    auth.dispose();
  });
  testWidgets('小屏大字键盘可滚动完成；语义可读，不依赖颜色表达选择', (tester) async {
    tester.view.physicalSize = const Size(320, 740);
    tester.view.devicePixelRatio = 1;
    tester.view.viewInsets = const FakeViewPadding(bottom: 240);
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetViewInsets);
    final semantics = tester.ensureSemantics();
    final auth = SocialTestAuth();
    await tester.pumpWidget(
      socialHarness(
        auth,
        MockClient((_) async => socialResponse(socialJson())),
        scale: 2,
      ),
    );
    await tester.tap(find.text('打开偏好'));
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('国际社群'));
    expect(find.bySemanticsLabel(RegExp('国际社群')), findsWidgets);
    await tester.tap(find.text('国际社群'));
    await tester.ensureVisible(find.text('检查选择'));
    await tester.tap(find.text('检查选择'));
    await tester.pump();
    await tester.ensureVisible(find.text('取消'));
    await tester.tap(find.text('取消'));
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    semantics.dispose();
    await tester.pumpWidget(const SizedBox());
    auth.dispose();
  });
}
