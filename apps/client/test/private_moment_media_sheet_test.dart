import 'dart:convert';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/legacy/legacy_shell.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'private_moment_media_controller_test.dart'
    show MediaUnitFixture, syntheticPrivatePNG, reopenForRecovery;
import 'package:birdtie_client/src/content/private_moment_media_sheet.dart';
import 'package:birdtie_client/src/content/private_moment_media_pending_store.dart';

const mediaMomentID = '11111111-1111-4111-8111-111111111111';
const mediaOwnerID = '22222222-2222-4222-8222-222222222222';
Map<String, Object?> mediaMoment() => {
  'id': mediaMomentID,
  'authorAccountId': mediaOwnerID,
  'cityId': 'aberdeen-gb',
  'title': '本人已保存私人记录',
  'body': '文字草稿',
  'timePrecision': 'unknown',
  'locationPrecision': 'none',
  'status': 'draft',
  'visibility': 'private',
  'revision': 1,
};

void main() {
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  testWidgets('关闭原图片页重开真实待核实入口只GET原operation不复活保存批准', (t) async {
    final bytes = await t.runAsync(syntheticPrivatePNG);
    const store = SecurePrivateImagePendingStore();
    final x = MediaUnitFixture(bytes!, pendingStore: store);
    await t.runAsync(() async {
      await x.preview();
      x.custom = (r) async {
        if (r.method == 'PUT') {
          x.route(r);
          return http.Response('', 503);
        }
        return x.route(r);
      };
      await x.controller.saveReviewed(confirmed: true);
    });
    final op = x.metadata!['operationId'];
    await t.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => Scaffold(
            body: TextButton(
              onPressed: () => Navigator.of(context).push(
                MaterialPageRoute<void>(
                  builder: (route) => PrivateMomentMediaSheet(
                    controller: x.controller,
                    momentTitle: '当前私人记录',
                  ),
                ),
              ),
              child: const Text('打开私人图片'),
            ),
          ),
        ),
      ),
    );
    await t.tap(find.text('打开私人图片'));
    await t.pumpAndSettle();
    expect(find.text('图片保存结果待核实'), findsOneWidget);
    await t.tap(find.byTooltip('关闭私人图片'));
    await t.pumpAndSettle();
    reopenForRecovery(x, store);
    x.requests.clear();
    await t.tap(find.text('打开私人图片'));
    await t.pumpAndSettle();
    expect(find.text('图片保存结果待核实'), findsOneWidget);
    expect(find.text('当前核实的操作'), findsOneWidget);
    expect(x.requests, isEmpty);
    expect(find.byType(Image), findsNothing);
    await t.ensureVisible(find.text('核实当前操作结果'));
    await t.tap(find.text('核实当前操作结果'));
    await t.pumpAndSettle();
    expect(x.requests.single.method, 'GET');
    expect(
      x.requests.single.url.path,
      endsWith('/private-image-operations/$op'),
    );
    expect(x.controller.images.length, 1);
    expect(x.controller.localBytes, isNull);
    expect(x.controller.canSave, isFalse);
    expect(find.text('检查并保存私人图片'), findsNothing);
    expect(find.text('图片保存结果待核实'), findsNothing);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    x.close();
  });
  testWidgets('已保存本人私人Moment原编辑器提供实际图片附件入口', (t) async {
    final auth = SeedTestAuth()..owner = mediaOwnerID;
    final city = PublicCityController();
    final client = MockClient(
      (r) async => http.Response.bytes(
        utf8.encode(
          jsonEncode({
            'data': [mediaMoment()],
          }),
        ),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      ),
    );
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      ownerID: () => auth.accountID,
      identityChanges: auth,
      client: client,
      apiBaseUrl: 'https://birdtie.example',
    );
    await moments.refresh();
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: LegacyProfilePage(
            auth: auth,
            city: city,
            moments: moments,
            client: client,
            apiBaseUrl: 'https://birdtie.example',
          ),
        ),
      ),
    );
    await t.ensureVisible(find.text('编辑或撤回'));
    await t.tap(find.text('编辑或撤回'));
    await t.pumpAndSettle();
    expect(find.text('编辑动态'), findsOneWidget);
    expect(find.text('管理私人图片'), findsOneWidget);
    await t.ensureVisible(find.text('管理私人图片'));
    await t.tap(find.text('管理私人图片'));
    await t.pumpAndSettle();
    expect(find.byType(PrivateMomentMediaSheet), findsOneWidget);
    expect(find.text('选择一张图片'), findsOneWidget);
    await t.tap(find.byTooltip('关闭私人图片'));
    await t.pumpAndSettle();
    expect(find.text('编辑动态'), findsOneWidget);
    final body = t
        .widgetList<TextFormField>(find.byType(TextFormField))
        .map((w) => w.controller?.text);
    expect(body, contains('文字草稿'));
    await t.pumpWidget(const SizedBox());
    moments.dispose();
    auth.dispose();
    city.dispose();
    client.close();
  });
  testWidgets('真实图片表单小屏大字仅显式选择处理确认保存且操作可滚动到达', (t) async {
    t.view.physicalSize = const Size(320, 720);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final bytes = await t.runAsync(syntheticPrivatePNG);
    final x = MediaUnitFixture(bytes!);
    await t.pumpWidget(
      MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(
            context,
          ).copyWith(textScaler: const TextScaler.linear(2)),
          child: child!,
        ),
        home: PrivateMomentMediaSheet(
          controller: x.controller,
          momentTitle: '本人已保存私人记录的较长名称',
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(x.requests.where((r) => r.method == 'POST'), isEmpty);
    await t.tap(find.text('选择一张图片'));
    // Widget frames and engine-image futures both need to advance; awaiting
    // only a real timer leaves this fixture's fake frame queue unpumped.
    for (var i = 0; i < 50 && x.controller.busy; i++) {
      await t.pump(const Duration(milliseconds: 16));
      await t.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 1)),
      );
    }
    expect(x.controller.busy, isFalse);
    await t.pumpAndSettle();
    expect(x.controller.hasLocalImage, isTrue);
    expect(x.requests.where((r) => r.method == 'POST'), isEmpty);
    final review = find.byKey(const ValueKey('private-image-local-review'));
    final reviewControl = find.descendant(
      of: review,
      matching: find.byType(Checkbox),
    );
    await t.scrollUntilVisible(
      reviewControl,
      200,
      scrollable: find.byType(Scrollable).first,
    );
    await t.ensureVisible(reviewControl);
    await t.pumpAndSettle();
    expect(reviewControl.hitTestable(), findsOneWidget);
    await t.tap(reviewControl);
    await t.pump();
    expect(x.controller.localReviewed, isTrue);
    await t.scrollUntilVisible(
      find.text('生成私人保存预览'),
      150,
      scrollable: find.byType(Scrollable).first,
    );
    await t.tap(find.text('生成私人保存预览'));
    await t.pumpAndSettle();
    expect(x.controller.preview?.status, 'preview');
    expect(x.requests.where((r) => r.method == 'PUT'), isEmpty);
    await t.scrollUntilVisible(
      find.text('检查并保存私人图片'),
      150,
      scrollable: find.byType(Scrollable).first,
    );
    await t.tap(find.text('检查并保存私人图片'));
    await t.pumpAndSettle();
    expect(find.text('保存这张私人图片？'), findsOneWidget);
    expect(find.textContaining('不公开，不用于 AI 分析'), findsOneWidget);
    await t.tap(find.text('返回检查'));
    await t.pumpAndSettle();
    expect(x.requests.where((r) => r.method == 'PUT'), isEmpty);
    await t.tap(find.text('检查并保存私人图片'));
    await t.pumpAndSettle();
    await t.tap(find.text('确认保存私人图片'));
    await t.pumpAndSettle();
    expect(x.controller.images.length, 1);
    expect(x.requests.where((r) => r.method == 'PUT').length, 1);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    x.close();
  });
  testWidgets('晚保存确认在身份ABA后零PUT，失效表单不显示旧图', (t) async {
    final bytes = await t.runAsync(syntheticPrivatePNG);
    final x = MediaUnitFixture(bytes!);
    await t.runAsync(x.preview);
    await t.pumpWidget(
      MaterialApp(
        home: PrivateMomentMediaSheet(
          controller: x.controller,
          momentTitle: '当前私人记录',
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.scrollUntilVisible(
      find.text('检查并保存私人图片'),
      180,
      scrollable: find.byType(Scrollable).first,
    );
    await t.tap(find.text('检查并保存私人图片'));
    await t.pumpAndSettle();
    x.credentials.actor('Bearer synthetic-b');
    x.credentials.actor('Bearer synthetic-a');
    await t.pump();
    await t.tap(find.text('确认保存私人图片'));
    await t.pumpAndSettle();
    expect(x.requests.where((r) => r.method == 'PUT'), isEmpty);
    expect(find.byKey(const ValueKey('private-image-retired')), findsOneWidget);
    expect(find.byType(Image), findsNothing);
    await t.pumpWidget(const SizedBox());
    x.close();
  });
  testWidgets('实际移除确认晚回拒绝旧列表版本，正常关闭未启动模型', (t) async {
    final bytes = await t.runAsync(syntheticPrivatePNG);
    final x = MediaUnitFixture(bytes!);
    await t.runAsync(() async {
      await x.preview();
      await x.controller.saveReviewed(confirmed: true);
    });
    await t.pumpWidget(
      MaterialApp(
        home: PrivateMomentMediaSheet(
          controller: x.controller,
          momentTitle: '当前私人记录',
        ),
      ),
    );
    await t.pumpAndSettle();
    final remove = find.byTooltip('移除私人图片');
    await t.scrollUntilVisible(
      remove,
      180,
      scrollable: find.byType(Scrollable).first,
    );
    await t.tap(remove);
    await t.pumpAndSettle();
    await x.controller.load();
    await t.pump();
    await t.tap(find.text('确认移除'));
    await t.pumpAndSettle();
    expect(x.requests.where((r) => r.method == 'DELETE'), isEmpty);
    expect(x.controller.images.length, 1);
    await t.pumpWidget(const SizedBox());
    x.close();
  });
}
