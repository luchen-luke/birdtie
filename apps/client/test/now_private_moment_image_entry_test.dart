import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/content/private_moment_media_sheet.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/now_private_moment_image_page.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:birdtie_client/src/workspace/sidebar.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'now_scope_recovery_test.dart';

const entryOwner = '22222222-2222-4222-8222-222222222222';
const entryMoment = '11111111-1111-4111-8111-111111111111';
Map<String, Object?> entryRow({
  String id = entryMoment,
  String visibility = 'private',
  String status = 'draft',
  int revision = 1,
  String owner = entryOwner,
}) => {
  'id': id,
  'authorAccountId': owner,
  'cityId': 'alpha',
  'title': '本人已保存的私人记录',
  'body': '不复制Now草稿',
  'timePrecision': 'unknown',
  'locationPrecision': 'none',
  'visibility': visibility,
  'status': status,
  'revision': revision,
};

class EntryClient extends MockClient {
  EntryClient(super.fn);
  int closes = 0;
  @override
  void close() {
    closes++;
    super.close();
  }
}

class EntryFixture {
  EntryFixture({bool signedIn = true}) {
    auth.owner = entryOwner;
    auth.token = signedIn ? 'Bearer synthetic-entry' : null;
    client = EntryClient((r) async {
      requests.add(r);
      if (r.url.path == '/v1/me/moments') {
        onMomentRequest?.call();
        return pending?.future ?? responseStatus(status);
      }
      if (r.url.path.endsWith('/private-images')) {
        return imagePending?.future ?? jsonResponse([]);
      }
      return http.Response('', 503);
    });
    moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      ownerID: () => auth.accountID,
      identityChanges: auth,
      apiBaseUrl: base,
      client: client,
    );
  }
  final auth = SeedTestAuth();
  final city = NowFixtureCity();
  late final NowFixtureSource source = NowFixtureSource(city);
  late EntryClient client;
  late PrivateMomentController moments;
  final requests = <http.Request>[];
  List<Map<String, Object?>> rows = [entryRow()];
  int status = 200;
  String base = 'https://now-entry.test';
  Completer<http.Response>? pending, imagePending;
  VoidCallback? onMomentRequest;
  http.Response jsonResponse(Object data) => http.Response.bytes(
    utf8.encode(jsonEncode({'data': data})),
    200,
    headers: {'content-type': 'application/json'},
  );
  http.Response responseStatus(int code) =>
      code == 200 ? jsonResponse(rows) : http.Response('', code);
  Future<void> mount(
    WidgetTester t, {
    double scale = 1,
    double width = 390,
  }) async {
    t.view.devicePixelRatio = 1;
    t.view.physicalSize = Size(width, 844);
    await t.pumpWidget(
      MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(
            context,
          ).copyWith(textScaler: TextScaler.linear(scale)),
          child: child!,
        ),
        home: MapWorkspace(
          city: city,
          auth: auth,
          moments: moments,
          agentTaskSource: source,
          seedClient: client,
          seedApiBaseUrl: base,
        ),
      ),
    );
    await t.pumpAndSettle();
  }

  List<http.Request> get momentRequests =>
      requests.where((r) => r.url.path.startsWith('/v1/me/moments')).toList();
  Future<void> open(WidgetTester t, {bool settle = true}) async {
    await nowTap(t, find.byTooltip('打开快捷操作'));
    await t.ensureVisible(find.text('为私人记录添加图片'));
    expect(find.text('为私人记录添加图片').hitTestable(), findsOneWidget);
    await t.tap(find.text('为私人记录添加图片'));
    if (settle) {
      await t.pumpAndSettle();
    } else {
      await t.pump();
      await t.pump(const Duration(milliseconds: 400));
    }
  }

  Future<void> unmount(WidgetTester t) async {
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
  }

  void dispose() {
    moments.dispose();
    city.dispose();
    auth.dispose();
  }
}

Future<OrganizationWorkspaceController> entryOrganizations(
  WidgetTester t,
) async {
  await nowTap(t, find.byTooltip('打开侧边栏'));
  final org = t.widget<Sidebar>(find.byType(Sidebar)).organizations;
  await nowBack(t);
  return org;
}

const entryOrg = OrganizationWorkspace(
  id: 'synthetic-org',
  name: '合成组织',
  role: 'ADMIN',
  organizationType: 'CLUB',
);

void main() {
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));

  testWidgets('原Now加号添加素材提供明确私人记录图片入口且保留未发送草稿', (t) async {
    final f = NowFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await t.enterText(nowField(), '已有未发送草稿');
    await nowTap(t, find.byTooltip('打开快捷操作'));
    expect(find.text('添加素材'), findsOneWidget);
    expect(find.text('为私人记录添加图片'), findsOneWidget);
    expect(f.source.queries, isEmpty);
    await nowBack(t);
    expect(t.widget<TextField>(nowField()).controller!.text, '已有未发送草稿');
    await f.unmount(t);
  });
  testWidgets('Now图片入口选择原本人private draft同transport且取消返回保留Now草稿和地图', (t) async {
    final f = EntryFixture();
    addTearDown(f.dispose);
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    f.rows = [
      entryRow(),
      entryRow(
        id: '44444444-4444-4444-8444-444444444444',
        visibility: 'public',
      ),
      entryRow(id: '55555555-5555-4555-8555-555555555555', status: 'withdrawn'),
      entryRow(id: '66666666-6666-4666-8666-666666666666', status: 'published'),
      entryRow(id: '77777777-7777-4777-8777-777777777777', revision: 0),
    ];
    await f.mount(t);
    await t.enterText(nowField(), '已有未发送草稿');
    final map = t.element(find.byType(MapCanvas));
    final ws = t.widget<MapCanvas>(find.byType(MapCanvas)).workspace;
    await f.open(t);
    expect(find.text('选择私人记录'), findsOneWidget);
    expect(
      find.byKey(const ValueKey('private-image-moment-$entryMoment')),
      findsOneWidget,
    );
    expect(find.byType(ListTile), findsOneWidget);
    expect(f.momentRequests.single.method, 'GET');
    await nowTap(
      t,
      find.byKey(const ValueKey('private-image-moment-$entryMoment')),
    );
    final sheet = t.widget<PrivateMomentMediaSheet>(
      find.byType(PrivateMomentMediaSheet),
    );
    expect(sheet.controller.momentID, entryMoment);
    expect(sheet.controller.momentRevision, 1);
    expect(find.text('选择一张图片'), findsOneWidget);
    expect(f.momentRequests.length, 2);
    expect(f.momentRequests.last.url.origin, 'https://now-entry.test');
    expect(
      f.momentRequests.last.headers['Authorization'],
      'Bearer synthetic-entry',
    );
    expect(f.momentRequests.every((r) => r.method == 'GET'), isTrue);
    await nowTap(t, find.byTooltip('关闭私人图片'));
    expect(
      f.client.closes,
      0,
      reason: 'image page borrows original Moment transport',
    );
    await nowTap(t, find.byTooltip('关闭选择私人记录'));
    expect(t.widget<TextField>(nowField()).controller!.text, '已有未发送草稿');
    expect(t.testTextInput.isVisible, isFalse);
    expect(t.element(find.byType(MapCanvas)), same(map));
    expect(t.widget<MapCanvas>(find.byType(MapCanvas)).workspace, same(ws));
    expect(f.source.queries, isEmpty);
    await f.unmount(t);
  });
  for (final reason in ['guest', 'org', 'environment', 'owner', 'token']) {
    testWidgets('Now私人图片入口拒绝$reason来源且零私人HTTP不假创建', (t) async {
      final f = EntryFixture(signedIn: reason != 'guest');
      addTearDown(f.dispose);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      if (reason == 'environment') f.base = 'https://different-entry.test';
      if (reason == 'owner') {
        f.moments.dispose();
        f.moments = PrivateMomentController(
          authorizationHeader: () => f.auth.authorizationHeader,
          ownerID: () => '44444444-4444-4444-8444-444444444444',
          client: f.client,
          apiBaseUrl: f.base,
        );
      }
      if (reason == 'token') {
        f.moments.dispose();
        f.moments = PrivateMomentController(
          authorizationHeader: () => 'Bearer synthetic-different',
          ownerID: () => f.auth.accountID,
          client: f.client,
          apiBaseUrl: f.base,
        );
      }
      await f.mount(t);
      if (reason == 'org') {
        final org = await entryOrganizations(t);
        org.select(entryOrg);
        await t.pumpAndSettle();
      }
      f.requests.clear();
      await t.enterText(nowField(), '当前草稿');
      await f.open(t);
      expect(f.momentRequests, isEmpty);
      expect(find.byType(PrivateMomentMediaSheet), findsNothing);
      expect(find.text('选择私人记录'), findsNothing);
      expect(
        find.textContaining(switch (reason) {
          'guest' => '请先登录',
          'org' => '切回个人身份',
          _ => '来源不一致',
        }),
        findsOneWidget,
      );
      expect(t.widget<TextField>(nowField()).controller!.text, '当前草稿');
      expect(t.testTextInput.isVisible, isFalse);
      expect(f.source.queries, isEmpty);
      await f.unmount(t);
    });
  }
  for (final mode in ['empty', 'error', 'foreign']) {
    testWidgets('私人记录$mode不假选目标或创建空Moment且取消不写', (t) async {
      final f = EntryFixture();
      addTearDown(f.dispose);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      if (mode == 'empty') f.rows = [];
      if (mode == 'error') f.status = 503;
      if (mode == 'foreign') {
        f.rows = [entryRow(owner: '44444444-4444-4444-8444-444444444444')];
      }
      await f.mount(t);
      await f.open(t);
      expect(find.byType(PrivateMomentMediaSheet), findsNothing);
      expect(
        find.byKey(const ValueKey('private-image-moment-$entryMoment')),
        findsNothing,
      );
      expect(
        find.textContaining(mode == 'empty' ? '暂无可添加图片' : '读取失败不表示没有记录'),
        findsOneWidget,
      );
      if (mode == 'error') {
        f.status = 200;
        await nowTap(t, find.text('重新读取私人记录'));
        expect(
          find.byKey(const ValueKey('private-image-moment-$entryMoment')),
          findsOneWidget,
        );
      }
      expect(f.momentRequests.every((r) => r.method == 'GET'), isTrue);
      await nowTap(t, find.byTooltip('关闭选择私人记录'));
      await f.unmount(t);
    });
  }
  testWidgets('读取私人记录同步loading通知中工作范围退役时零旧GET且共享loading恢复', (t) async {
    final f = EntryFixture();
    addTearDown(f.dispose);
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    await f.mount(t);
    final org = await entryOrganizations(t);
    var switched = false;
    f.moments.addListener(() {
      if (f.moments.loading && !switched) {
        switched = true;
        org.select(entryOrg);
      }
    });
    f.requests.clear();
    await f.open(t);
    expect(switched, isTrue);
    expect(
      f.momentRequests,
      isEmpty,
      reason: 'notify retirement must precede old GET',
    );
    expect(
      f.moments.loading,
      isFalse,
      reason: 'aborted own request must release loading',
    );
    expect(find.byType(PrivateMomentMediaSheet), findsNothing);
    await f.unmount(t);
  });
  testWidgets('私人图片页真实记录版本ABA退休旧controller且拒绝旧gallery和write', (t) async {
    final f = EntryFixture();
    addTearDown(f.dispose);
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    await f.mount(t);
    await f.open(t);
    await nowTap(
      t,
      find.byKey(const ValueKey('private-image-moment-$entryMoment')),
    );
    final c = t
        .widget<PrivateMomentMediaSheet>(find.byType(PrivateMomentMediaSheet))
        .controller;
    f.rows = [entryRow(revision: 2)];
    await f.moments.refresh();
    f.rows = [entryRow()];
    await f.moments.refresh();
    await t.pumpAndSettle();
    expect(c.retired, isTrue);
    expect(c.localBytes, isNull);
    expect(find.byType(PrivateMomentMediaSheet), findsNothing);
    expect(f.momentRequests.every((r) => r.method == 'GET'), isTrue);
    expect(f.source.queries, isEmpty);
    await f.unmount(t);
  });
  testWidgets('私人记录读取迟到身份ABA不复活旧行或旧图片页', (t) async {
    final f = EntryFixture();
    addTearDown(f.dispose);
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    f.pending = Completer<http.Response>();
    await f.mount(t);
    await f.open(t, settle: false);
    expect(find.byType(CircularProgressIndicator), findsWidgets);
    f.auth.changeIdentity(
      'Bearer synthetic-b',
      nextOwner: '44444444-4444-4444-8444-444444444444',
    );
    f.auth.changeIdentity('Bearer synthetic-entry', nextOwner: entryOwner);
    f.pending!.complete(f.jsonResponse(f.rows));
    await t.pumpAndSettle();
    expect(find.text('本人已保存的私人记录'), findsNothing);
    expect(find.byType(PrivateMomentMediaSheet), findsNothing);
    expect(f.momentRequests.every((r) => r.method == 'GET'), isTrue);
    await f.unmount(t);
  });
  testWidgets('图片入口320大字原选择和关闭可达且没有提前gallery或写', (t) async {
    final f = EntryFixture();
    addTearDown(f.dispose);
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    f.rows = [entryRow()..['title'] = '本人已保存的非常长的私人记录标题用于检查大字换行与选择入口'];
    await f.mount(t, width: 320, scale: 2);
    await f.open(t);
    await nowTap(
      t,
      find.byKey(const ValueKey('private-image-moment-$entryMoment')),
    );
    expect(find.text('选择一张图片'), findsOneWidget);
    expect(f.momentRequests.every((r) => r.method == 'GET'), isTrue);
    expect(t.takeException(), isNull);
    await nowTap(t, find.byTooltip('关闭私人图片'));
    await nowTap(t, find.byTooltip('关闭选择私人记录'));
    await f.unmount(t);
  });
  for (final code in [401, 403]) {
    testWidgets('私人记录读取$code真实恢复登录权限不出现无效GET循环', (t) async {
      final f = EntryFixture()..status = code;
      addTearDown(f.dispose);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      await f.mount(t);
      await f.open(t);
      expect(
        find.textContaining(code == 401 ? '登录已失效' : '没有读取权限'),
        findsOneWidget,
      );
      expect(find.text('重新读取私人记录'), findsNothing);
      expect(find.textContaining('暂无可添加图片'), findsNothing);
      expect(f.momentRequests.single.method, 'GET');
      await nowTap(t, find.byTooltip('关闭选择私人记录'));
      await f.unmount(t);
    });
  }
  test('optional reader失效不能清同本人较新共享请求loading或回填旧结果', () async {
    final f = EntryFixture();
    addTearDown(f.dispose);
    final first = Completer<http.Response>(),
        second = Completer<http.Response>();
    final firstEntered = Completer<void>(), secondEntered = Completer<void>();
    f.onMomentRequest = () {
      if (f.momentRequests.length == 1) firstEntered.complete();
      if (f.momentRequests.length == 2) secondEntered.complete();
    };
    var valid = true;
    f.pending = first;
    final old = f.moments.refresh(current: () => valid);
    expect(f.moments.loading, isTrue);
    await firstEntered.future;
    valid = false;
    f.pending = second;
    final fresh = f.moments.refresh();
    await secondEntered.future;
    first.complete(f.jsonResponse([entryRow()..['title'] = '旧来源迟到']));
    await old;
    expect(f.moments.loading, isTrue);
    expect(f.moments.moments, isEmpty);
    second.complete(f.jsonResponse([entryRow()..['title'] = '当前本人新请求']));
    await fresh;
    expect(f.moments.loading, isFalse);
    expect(f.moments.moments.single.title, '当前本人新请求');
    expect(f.momentRequests.length, 2);
    expect(f.momentRequests.every((r) => r.method == 'GET'), isTrue);
  });
  for (final change in ['auth', 'moments', 'changes', 'current', 'org']) {
    testWidgets('私人图片选择页自身同key更换$change值相同仍永久退休旧route', (t) async {
      final f = EntryFixture();
      addTearDown(f.dispose);
      var auth = f.auth;
      var moments = f.moments;
      final scope = ValueNotifier<String?>(null);
      addTearDown(scope.dispose);
      Listenable changes = Listenable.merge([auth, moments, scope]);
      bool Function() current = () => true;
      String? Function() org = () => scope.value;
      Future<void> mount() async {
        await t.pumpWidget(
          MaterialApp(
            home: NowPrivateMomentImagePage(
              key: const Key('same-entry'),
              auth: auth,
              moments: moments,
              identityChanges: changes,
              current: current,
              organizationWorkspaceID: org,
            ),
          ),
        );
        await t.pumpAndSettle();
      }

      await mount();
      expect(
        find.byKey(const ValueKey('private-image-moment-$entryMoment')),
        findsOneWidget,
      );
      final before = f.momentRequests.length;
      if (change == 'auth') {
        auth = SeedTestAuth()
          ..owner = entryOwner
          ..token = 'Bearer synthetic-entry';
        addTearDown(auth.dispose);
      }
      if (change == 'moments') {
        moments = PrivateMomentController(
          authorizationHeader: () => f.auth.authorizationHeader,
          ownerID: () => f.auth.accountID,
          identityChanges: f.auth,
          client: f.client,
          apiBaseUrl: f.base,
        );
        addTearDown(moments.dispose);
      }
      if (change == 'changes') {
        changes = Listenable.merge([f.auth, f.moments, scope]);
      }
      if (change == 'current') current = () => true;
      if (change == 'org') org = () => null;
      await mount();
      expect(find.textContaining('身份或记录来源已变化'), findsOneWidget);
      expect(
        find.byKey(const ValueKey('private-image-moment-$entryMoment')),
        findsNothing,
      );
      expect(f.momentRequests.length, before);
      await t.pumpWidget(const SizedBox());
    });
  }
  testWidgets('选择页自身current与Org同步事件ABA不复活旧本人列表', (t) async {
    final f = EntryFixture();
    addTearDown(f.dispose);
    final changes = ValueNotifier<int>(0);
    addTearDown(changes.dispose);
    final merged = Listenable.merge([f.auth, f.moments, changes]);
    var allowed = true;
    String? org;
    await t.pumpWidget(
      MaterialApp(
        home: NowPrivateMomentImagePage(
          auth: f.auth,
          moments: f.moments,
          identityChanges: merged,
          current: () => allowed,
          organizationWorkspaceID: () => org,
        ),
      ),
    );
    await t.pumpAndSettle();
    final before = f.momentRequests.length;
    allowed = false;
    org = 'synthetic-org';
    changes.value++;
    allowed = true;
    org = null;
    changes.value++;
    await t.pumpAndSettle();
    expect(find.textContaining('身份或记录来源已变化'), findsOneWidget);
    expect(
      find.byKey(const ValueKey('private-image-moment-$entryMoment')),
      findsNothing,
    );
    expect(f.momentRequests.length, before);
    await t.pumpWidget(const SizedBox());
  });
  for (final change in [
    'moments',
    'transport',
    'environment',
    'scope',
    'task',
  ]) {
    testWidgets('完整Now图片读取迟到且$change替换永久退休旧页不复用新source', (t) async {
      final f = EntryFixture();
      addTearDown(f.dispose);
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      await f.mount(t);
      await f.open(t);
      f.imagePending = Completer<http.Response>();
      final choice = find.byKey(
        const ValueKey('private-image-moment-$entryMoment'),
      );
      await t.tap(choice);
      await t.pump();
      await t.pump(const Duration(milliseconds: 400));
      final c = t
          .widget<PrivateMomentMediaSheet>(find.byType(PrivateMomentMediaSheet))
          .controller;
      if (change == 'moments') {
        final old = f.moments;
        f.moments = PrivateMomentController(
          authorizationHeader: () => f.auth.authorizationHeader,
          ownerID: () => f.auth.accountID,
          identityChanges: f.auth,
          apiBaseUrl: f.base,
          client: f.client,
        );
        addTearDown(old.dispose);
        await f.mount(t);
      }
      if (change == 'transport') {
        final old = f.client;
        f.client = EntryClient((r) async => http.Response('', 503));
        addTearDown(f.client.close);
        await f.mount(t);
        expect(old.closes, 0);
      }
      if (change == 'environment') {
        f.base = 'https://new-environment.test';
        await f.mount(t);
      }
      if (change == 'scope') {
        f.city.selectCity('beta');
        await t.pump();
      }
      if (change == 'task') {
        t
            .widget<MapCanvas>(find.byType(MapCanvas, skipOffstage: false))
            .workspace
            .newTask();
        await t.pump();
      }
      f.imagePending!.complete(f.jsonResponse([]));
      await t.pumpAndSettle();
      expect(c.retired, isTrue);
      expect(c.images, isEmpty);
      expect(c.localBytes, isNull);
      expect(find.byType(PrivateMomentMediaSheet), findsNothing);
      expect(f.momentRequests.every((r) => r.method == 'GET'), isTrue);
      await f.unmount(t);
    });
  }
  testWidgets('旧加号菜单Moment source更换不打开新私人目标也不清Now现稿', (t) async {
    final f = EntryFixture();
    addTearDown(f.dispose);
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    await f.mount(t);
    await t.enterText(nowField(), '独立Now安全草稿');
    await nowTap(t, find.byTooltip('打开快捷操作'));
    final old = f.moments;
    f.moments = PrivateMomentController(
      authorizationHeader: () => f.auth.authorizationHeader,
      ownerID: () => f.auth.accountID,
      identityChanges: f.auth,
      client: f.client,
      apiBaseUrl: f.base,
    );
    addTearDown(old.dispose);
    await f.mount(t);
    f.requests.clear();
    await nowTap(t, find.text('为私人记录添加图片'));
    expect(find.textContaining('私人记录来源已变化'), findsOneWidget);
    expect(find.text('选择私人记录'), findsNothing);
    expect(f.momentRequests, isEmpty);
    expect(t.widget<TextField>(nowField()).controller!.text, '独立Now安全草稿');
    expect(t.testTextInput.isVisible, isFalse);
    await f.unmount(t);
  });
  testWidgets('旧加号菜单真实Moment来源A到B再A不能复活原图片入口', (t) async {
    final f = EntryFixture();
    addTearDown(f.dispose);
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    await f.mount(t);
    await t.enterText(nowField(), 'Now独立草稿不随个人来源替换清除');
    await nowTap(t, find.byTooltip('打开快捷操作'));
    final original = f.moments;
    final other = PrivateMomentController(
      authorizationHeader: () => f.auth.authorizationHeader,
      ownerID: () => f.auth.accountID,
      identityChanges: f.auth,
      client: f.client,
      apiBaseUrl: f.base,
    );
    addTearDown(other.dispose);
    f.moments = other;
    await f.mount(t);
    f.moments = original;
    await f.mount(t);
    f.requests.clear();
    await nowTap(t, find.text('为私人记录添加图片'));
    expect(find.textContaining('私人记录来源已变化'), findsOneWidget);
    expect(find.text('选择私人记录'), findsNothing);
    expect(f.momentRequests, isEmpty);
    expect(
      t.widget<TextField>(nowField()).controller!.text,
      'Now独立草稿不随个人来源替换清除',
    );
    expect(t.testTextInput.isVisible, isFalse);
    await f.unmount(t);
  });
  testWidgets('旧资料真实侧栏入口Moment来源替换安全退休且Now草稿保留', (t) async {
    final f = EntryFixture();
    addTearDown(f.dispose);
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    await f.mount(t);
    await t.enterText(nowField(), '个人页面以外的安全Now草稿');
    await nowTap(t, find.byTooltip('打开侧边栏'));
    await nowTap(t, find.text('个人资料'));
    final original = f.moments;
    f.moments = PrivateMomentController(
      authorizationHeader: () => f.auth.authorizationHeader,
      ownerID: () => f.auth.accountID,
      identityChanges: f.auth,
      client: f.client,
      apiBaseUrl: f.base,
    );
    addTearDown(original.dispose);
    await f.mount(t);
    expect(find.textContaining('工作身份或来源已变化'), findsOneWidget);
    expect(t.takeException(), isNull);
    await nowBack(t);
    expect(t.widget<TextField>(nowField()).controller!.text, '个人页面以外的安全Now草稿');
    expect(t.testTextInput.isVisible, isFalse);
    await f.unmount(t);
  });
  testWidgets('已粘贴文字材料Moment-only替换不清独立Now稿且新图片入口仍可用', (t) async {
    final f = EntryFixture();
    addTearDown(f.dispose);
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    var clipboardReads = 0;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, (call) async {
          if (call.method == 'Clipboard.getData') {
            clipboardReads++;
            return {'text': '明确粘贴的私人短文字'};
          }
          return null;
        });
    addTearDown(() {
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(SystemChannels.platform, null);
    });
    await f.mount(t);
    await t.enterText(nowField(), '原Now草稿');
    await nowTap(t, find.byTooltip('打开快捷操作'));
    await nowTap(t, find.text('粘贴文字/链接'));
    expect(clipboardReads, 1);
    expect(
      t.widget<TextField>(nowField()).controller!.text,
      '原Now草稿\n明确粘贴的私人短文字',
    );
    final original = f.moments;
    f.moments = PrivateMomentController(
      authorizationHeader: () => f.auth.authorizationHeader,
      ownerID: () => f.auth.accountID,
      identityChanges: f.auth,
      client: f.client,
      apiBaseUrl: f.base,
    );
    addTearDown(original.dispose);
    await f.mount(t);
    expect(
      t.widget<TextField>(nowField()).controller!.text,
      '原Now草稿\n明确粘贴的私人短文字',
    );
    expect(clipboardReads, 1);
    expect(f.source.queries, isEmpty);
    await f.open(t);
    expect(
      find.byKey(const ValueKey('private-image-moment-$entryMoment')),
      findsOneWidget,
    );
    expect(f.momentRequests.single.method, 'GET');
    await nowTap(t, find.byTooltip('关闭选择私人记录'));
    expect(
      t.widget<TextField>(nowField()).controller!.text,
      '原Now草稿\n明确粘贴的私人短文字',
    );
    expect(t.testTextInput.isVisible, isFalse);
    await f.unmount(t);
  });
}
