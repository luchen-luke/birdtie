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
    final pending = Completer<http.Response>();
    var calls = 0;
    final moments = PrivateMomentController(
      authorizationHeader: () => 'Bearer A',
      apiBaseUrl: 'https://birdtie.example',
      client: MockClient((request) async {
        calls++;
        return calls == 1 ? http.Response('{broken', 201) : pending.future;
      }),
    );
    expect(
      await moments.create(cityID: 'aberdeen-gb', title: '旅行', body: ''),
      isFalse,
    );
    expect(moments.creationUncertain, isTrue);
    moments.confirmCreationChecked();
    final save = moments.create(
      cityID: 'aberdeen-gb',
      title: '重新检查后新建',
      body: '',
    );
    expect(
      await moments.create(cityID: 'aberdeen-gb', title: '重复点击', body: ''),
      isFalse,
    );
    moments.dispose();
    pending.complete(response(record(), 201));
    expect(await save, isFalse);
    expect(moments.moments, isEmpty);
    expect(calls, 2);
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
    var row = record();
    final sent = <Map<String, dynamic>>[];
    http.Client client() => MockClient((request) async {
      if (request.method == 'GET') return response([row]);
      final input = jsonDecode(request.body) as Map<String, dynamic>;
      sent.add(input);
      row = {
        ...row,
        ...input,
        'revision': request.method == 'POST' ? 1 : (row['revision'] as int) + 1,
      };
      if (input['timePrecision'] == 'unknown') row.remove('occurredAt');
      return response(row, request.method == 'POST' ? 201 : 200);
    });
    PrivateMomentController controller() => PrivateMomentController(
      authorizationHeader: () => 'Bearer owner',
      client: client(),
      apiBaseUrl: 'https://birdtie.example',
    );
    var moments = controller();
    expect(
      await moments.create(
        cityID: 'aberdeen-gb',
        title: row['title'] as String,
        body: '',
        time: MomentTimeValue.parse('year', '2025'),
      ),
      isTrue,
    );
    expect(sent.single['occurredAt'], '2025-01-01T00:00:00.000Z');
    moments.dispose();
    moments = controller();
    await moments.refresh();
    final old = moments.moments.single;
    expect(old.createdAt!.year, 2026);
    expect(
      await moments.update(moment: old, title: '修改标题', body: '', placeID: ''),
      isTrue,
    );
    expect(sent.last['timePrecision'], 'year');
    expect(sent.last['occurredAt'], '2025-01-01T00:00:00.000Z');
    expect(
      await moments.update(moment: old, title: '旧版本', body: '', placeID: ''),
      isFalse,
    );
    expect(sent, hasLength(2));
    expect(
      await moments.update(
        moment: moments.moments.single,
        title: '时间未知',
        body: '',
        placeID: '',
        time: MomentTimeValue.unknown,
      ),
      isTrue,
    );
    expect(sent.last.containsKey('occurredAt'), isFalse);
    expect(moments.moments.single.occurredAt, isNull);
    moments.dispose();
  });
  test('切换账号、迟到成功与失败不复用旧数据、版本或错误', () async {
    var token = 'Bearer A';
    final pending = Completer<http.Response>();
    var reads = 0, writes = 0;
    final moments = PrivateMomentController(
      authorizationHeader: () => token,
      apiBaseUrl: 'https://birdtie.example',
      client: MockClient((request) async {
        if (request.method == 'GET') {
          reads++;
          return response([record()]);
        }
        writes++;
        return pending.future;
      }),
    );
    await moments.refresh();
    final old = moments.moments.single;
    final save = moments.update(
      moment: old,
      title: 'A秘密',
      body: '',
      placeID: '',
    );
    token = 'Bearer B';
    expect(moments.moments, isEmpty);
    expect(moments.error, isNull);
    pending.complete(response(record(revision: 2)));
    expect(await save, isFalse);
    expect(moments.moments, isEmpty);
    expect(moments.saving, isFalse);
    expect(
      await moments.update(moment: old, title: '旧账号', body: '', placeID: ''),
      isFalse,
    );
    expect(writes, 1);
    expect(reads, 1);
    moments.dispose();
  });
  test('旧 GET 不覆盖更新后的版本；409不重试；dispose阻止迟到恢复', () async {
    final pending = Completer<http.Response>();
    var gets = 0, puts = 0;
    final moments = PrivateMomentController(
      authorizationHeader: () => 'Bearer A',
      apiBaseUrl: 'https://birdtie.example',
      client: MockClient((request) async {
        if (request.method == 'GET') {
          gets++;
          return gets == 1 ? response([record()]) : pending.future;
        }
        puts++;
        return puts == 1
            ? response(record(revision: 2))
            : http.Response('', 409);
      }),
    );
    await moments.refresh();
    final old = moments.moments.single;
    final refresh = moments.refresh();
    expect(
      await moments.update(moment: old, title: '新', body: '', placeID: ''),
      isTrue,
    );
    pending.complete(response([record()]));
    await refresh;
    expect(moments.moments.single.revision, 2);
    expect(
      await moments.update(
        moment: moments.moments.single,
        title: '冲突',
        body: '',
        placeID: '',
      ),
      isFalse,
    );
    expect(moments.error, contains('重新打开'));
    expect(puts, 2);
    moments.dispose();
    expect(
      await moments.create(cityID: 'aberdeen-gb', title: '关闭', body: ''),
      isFalse,
    );
  });
  test('POST 响应丢失不能宣称未保存，不自动或盲目重试', () async {
    var posts = 0;
    final moments = PrivateMomentController(
      authorizationHeader: () => 'Bearer A',
      apiBaseUrl: 'https://birdtie.example',
      client: MockClient((request) async {
        posts++;
        throw http.ClientException('response lost');
      }),
    );
    expect(
      await moments.create(cityID: 'aberdeen-gb', title: '旅行', body: ''),
      isFalse,
    );
    expect(moments.creationUncertain, isTrue);
    expect(moments.error, contains('尚未确认'));
    expect(
      await moments.create(cityID: 'aberdeen-gb', title: '旅行', body: ''),
      isFalse,
    );
    expect(posts, 1);
    moments.dispose();
  });
}
