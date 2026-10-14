import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/content/moment_time_choice.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/legacy/legacy_shell.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'moment_control_controller_test.dart';
import 'moment_control_test.dart';

Map<String, dynamic> record({
  String precision = 'year',
  String? occurred = '2025-01-01T00:00:00Z',
  int revision = 1,
}) => {
  'id': '11111111-1111-4111-8111-111111111111',
  'cityId': 'aberdeen-gb',
  'title': '2025年的阿伯丁旅行',
  'body': '本人记录',
  'timePrecision': precision,
  'occurredAt': ?occurred,
  'createdAt': '2026-10-03T04:05:06Z',
  'updatedAt': '2026-10-03T04:05:06Z',
  'locationPrecision': 'city',
  'status': 'draft',
  'revision': revision,
};
http.Response response(Object data, [int status = 200]) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': data})), status);

class _HistoricalAuth extends BirdtieAuthController {
  String? token = 'Bearer synthetic-owner';
  @override
  bool get signedIn => token != null;
  @override
  String? get authorizationHeader => token;
  @override
  String? get displayName => '合成本人';
  @override
  String? get loginMethod => 'dev_phone';
  void switchAccount() {
    token = 'Bearer synthetic-other';
    notifyListeners();
  }
}

class _HistoricalCity extends PublicCityController {
  static const city = PublicCity(
    id: 'aberdeen-gb',
    name: '阿伯丁',
    region: '英国',
    contentStatus: 'published',
    source: PublicSource(
      label: '合成 UI fixture',
      maintainer: '本地测试',
      freshness: 'unverified',
      updatedAt: null,
    ),
    map: null,
  );
  @override
  List<PublicCity> get cities => const [city];
  @override
  PublicCity get selectedCity => city;
}

void main() {
  test('成功状态但损坏的创建响应仍属结果未知；关闭时迟到结果不恢复', () async {
    final f = MomentFixture()..heldPUT = Completer<http.Response>();
    addTearDown(f.dispose);
    final p = await f.prepare(), save = f.controller.commit(p);
    while (f.puts == 0) {
      await Future<void>.delayed(Duration.zero);
    }
    f.heldPUT!.complete(http.Response('{broken', 200));
    expect(await save, isFalse);
    expect(f.controller.creationUncertain, isTrue);
    f.controller.confirmCreationChecked();
    expect(
      await f.controller.previewCreate(
        cityID: 'aberdeen-gb',
        title: '不能猜列表成功',
        body: '',
      ),
      isNull,
    );
    expect(f.puts, 1);
    expect(f.store.values.length, 1);
    final pending = f.store.values.values.single;
    f.controller.dispose();
    expect(f.controller.operationPreview, isNull);
    expect(pending.operationID, p.input.operationID);
  });
  testWidgets('实际私人动态页面可编辑历史年份，小屏大字保留草稿并在账号切换清除表单', (tester) async {
    tester.view.physicalSize = const Size(360, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final auth = _HistoricalAuth(), city = _HistoricalCity();
    var row = record();
    final sent = <Map<String, dynamic>>[];
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      apiBaseUrl: 'https://birdtie.example',
      client: MockClient((request) async {
        if (request.method == 'GET') return response([row]);
        final input = jsonDecode(request.body) as Map<String, dynamic>;
        sent.add(input);
        row = {...row, ...input, 'revision': 2};
        return response(row);
      }),
    );
    await moments.refresh();
    await tester.pumpWidget(
      MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(
            context,
          ).copyWith(textScaler: const TextScaler.linear(1.6)),
          child: child!,
        ),
        home: Scaffold(
          body: LegacyProfilePage(auth: auth, city: city, moments: moments),
        ),
      ),
    );
    await tester.ensureVisible(find.text('编辑或撤回'));
    await tester.tap(find.text('编辑或撤回'));
    await tester.pumpAndSettle();
    expect(find.text('编辑动态'), findsOneWidget);
    final year = find.byKey(const ValueKey('moment-time-year'));
    await tester.ensureVisible(year);
    await tester.enterText(year, '2024');
    await tester.ensureVisible(find.text('保存私人草稿'));
    await tester.tap(find.text('保存私人草稿'));
    await tester.pumpAndSettle();
    expect(sent.single['occurredAt'], '2024-01-01T00:00:00.000Z');
    expect(sent.single['timePrecision'], 'year');
    expect(find.textContaining('2024年'), findsOneWidget);
    await tester.ensureVisible(find.text('编辑或撤回'));
    await tester.tap(find.text('编辑或撤回'));
    await tester.pumpAndSettle();
    auth.switchAccount();
    await tester.pumpAndSettle();
    expect(find.text('账号已切换，请重新打开私人草稿。'), findsOneWidget);
    expect(find.byType(TextFormField), findsNothing);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
    moments.dispose();
    auth.dispose();
    city.dispose();
  });
  test('历史发生时间和创建时间独立；未知不能用创建时间回填', () {
    final known = PrivateMoment.fromJson(record());
    expect(known.time.label, '2025年');
    expect(known.createdAt!.year, 2026);
    expect(known.isEditableDraft, isTrue);
    final unknown = PrivateMoment.fromJson(
      record(precision: 'unknown', occurred: null),
    );
    expect(unknown.time, isNotNull);
    expect(unknown.occurredAt, isNull);
    expect(unknown.time.label, '发生时间未知');
    expect(
      () => PrivateMoment.fromJson({
        ...record(),
        'occurredAt': '2025-01-01T00:00:00',
      }),
      throwsFormatException,
    );
  });
  test('创建、刷新、重建 controller、编辑保留历史时间与精度；显式清为未知', () async {
    final f = MomentFixture()
      ..record = {...mcRecord(), 'createdAt': '2026-10-03T04:05:06Z'};
    addTearDown(f.dispose);
    final created = await f.controller.previewCreate(
      cityID: 'aberdeen-gb',
      title: '历史旅行',
      body: '',
      time: MomentTimeValue.parse('year', '2025'),
    );
    expect(await f.controller.commit(created!), isTrue);
    expect(created.input.input!['occurredAt'], '2025-01-01T00:00:00Z');
    final restarted = PrivateMomentController(
      authorizationHeader: () => f.identity.value.$1,
      ownerID: () => f.identity.value.$2,
      identityChanges: f.identity,
      client: f.client,
      apiBaseUrl: 'https://isolated.invalid',
      pendingStore: f.store,
    );
    addTearDown(restarted.dispose);
    await restarted.refresh();
    final old = restarted.moments.single;
    expect(old.createdAt!.year, 2026);
    final edit = await restarted.previewUpdate(
      moment: old,
      title: '修改标题',
      body: '',
      placeID: '',
    );
    expect(edit!.input.input!['timePrecision'], 'year');
    expect(await restarted.commit(edit), isTrue);
    expect(
      await restarted.previewUpdate(
        moment: old,
        title: '旧版本',
        body: '',
        placeID: '',
      ),
      isNull,
    );
    expect(f.puts, 2);
    final clear = await restarted.previewUpdate(
      moment: restarted.moments.single,
      title: '时间未知',
      body: '',
      placeID: '',
      time: MomentTimeValue.unknown,
    );
    expect(clear!.input.input!.containsKey('occurredAt'), isFalse);
    expect(await restarted.commit(clear), isTrue);
    expect(restarted.moments.single.occurredAt, isNull);
  });
  test('切换账号、迟到成功与失败不复用旧数据、版本或错误', () async {
    final f = MomentFixture()..heldPUT = Completer<http.Response>();
    addTearDown(f.dispose);
    await f.controller.refresh();
    final old = f.controller.moments.single;
    final p = await f.controller.previewUpdate(
      moment: old,
      title: 'A秘密',
      body: '',
      placeID: '',
    );
    final saved = f.controller.commit(p!);
    while (f.puts == 0) {
      await Future<void>.delayed(Duration.zero);
    }
    f.identity.value = ('Bearer B', mcOp);
    expect(f.controller.moments, isEmpty);
    f.heldPUT!.complete(mcJSON(mcReceipt(p.input)));
    expect(await saved, isFalse);
    expect(f.controller.moments, isEmpty);
    expect(f.controller.saving, isFalse);
    expect(
      await f.controller.previewUpdate(
        moment: old,
        title: '旧账号',
        body: '',
        placeID: '',
      ),
      isNull,
    );
    expect(f.puts, 1);
  });
  test('旧 GET 不覆盖更新后的版本；409不重试；dispose阻止迟到恢复', () async {
    final f = MomentFixture();
    addTearDown(f.dispose);
    await f.controller.refresh();
    final old = f.controller.moments.single;
    final pending = Completer<http.Response>();
    f.heldList = pending;
    final read = f.controller.refresh();
    await Future<void>.delayed(Duration.zero);
    final p = await f.controller.previewUpdate(
      moment: old,
      title: '新',
      body: '',
      placeID: '',
    );
    expect(await f.controller.commit(p!), isTrue);
    pending.complete(mcJSON([mcRecord()]));
    await read;
    expect(f.controller.moments.single.revision, 2);
    f.putStatus = 409;
    final conflict = await f.controller.previewUpdate(
      moment: f.controller.moments.single,
      title: '冲突',
      body: '',
      placeID: '',
    );
    expect(await f.controller.commit(conflict!), isFalse);
    expect(f.puts, 2);
    expect(f.controller.pendingOperations.length, 1);
    f.controller.dispose();
    expect(
      await f.controller.create(cityID: 'aberdeen-gb', title: '关闭', body: ''),
      isFalse,
    );
    expect(f.puts, 2);
  });
  test('POST 响应丢失不能宣称未保存，不自动或盲目重试', () async {
    final f = MomentFixture()..lost = true;
    addTearDown(f.dispose);
    final p = await f.prepare();
    expect(await f.controller.commit(p), isFalse);
    expect(f.controller.creationUncertain, isTrue);
    expect(f.controller.operationError, contains('待核实'));
    expect(
      await f.controller.previewCreate(
        cityID: 'aberdeen-gb',
        title: '旅行',
        body: '',
      ),
      isNull,
    );
    expect(f.puts, 1);
    expect(f.store.values.length, 1);
  });
}
