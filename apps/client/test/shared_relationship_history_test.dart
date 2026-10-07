import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/shared_social_context_panel.dart';
import 'package:birdtie_client/src/workspace/public_person_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const target = '22222222-2222-4222-8222-222222222222';
const activity = '33333333-3333-4333-8333-333333333333';
const place = '44444444-4444-4444-8444-444444444444';
Map<String, dynamic> currentWire({
  String title = '合成共同报名',
  bool populated = true,
}) {
  final now = DateTime.now().toUtc();
  return {
    'data': {
      'schema': 'shared-relationship-history-v1',
      'viewerId': '11111111-1111-4111-8111-111111111111',
      'targetId': target,
      'observedAt': now.toIso8601String(),
      'validUntil': now.add(const Duration(seconds: 29)).toIso8601String(),
      'attendance': 'UNKNOWN',
      'visit': 'UNKNOWN',
      'mutualCount': 0,
      'communities': [],
      'activities': populated
          ? [
              {
                'id': activity,
                'title': title,
                'startsAt': now.add(const Duration(hours: 1)).toIso8601String(),
                'endsAt': now.add(const Duration(hours: 2)).toIso8601String(),
                'timeZone': 'Europe/London',
                'modality': 'in_person',
              },
            ]
          : [],
      'places': populated
          ? [
              {
                'id': place,
                'title': '活动关联地点',
                'activityIds': [activity],
              },
            ]
          : [],
      'communitiesTruncated': false,
      'activitiesTruncated': false,
      'placesTruncated': false,
    },
  };
}

http.Response response([String title = '合成共同报名']) => http.Response.bytes(
  utf8.encode(jsonEncode(currentWire(title: title))),
  200,
);

class TrackedClient extends MockClient {
  TrackedClient(super.fn);
  int closes = 0;
  @override
  void close() {
    closes++;
    super.close();
  }
}

void main() {
  test('实际注册HTTP native3原样JSON兼容严格Dart DTO', () {
    // Captured native3 registered synthetic wire; bodySHA256 e6f119aeed4933bf52b9eec16674f0f828d703e40a7ce7afb559361def5431c0.
    const raw =
        r'''{"data":{"mutualCount":0,"communities":[],"activities":[{"id":"86abd5a6-5185-4f4b-9d4f-f6dea340c90d","title":"合成端到端周末羽毛球","startsAt":"2026-10-04T22:21:02Z","endsAt":"2026-10-04T23:21:02Z","timeZone":"UTC","modality":"in_person"}],"communitiesTruncated":false,"activitiesTruncated":false,"places":[{"id":"bbf545d6-9e6b-4d8b-9b74-5ab44cf625f3","title":"合成端到端羽毛球馆","activityIds":["86abd5a6-5185-4f4b-9d4f-f6dea340c90d"]}],"schema":"shared-relationship-history-v1","viewerId":"2ad5ef23-417e-4ef1-9b27-ba6283267087","targetId":"5b57526f-4f7c-4869-9752-9deaa784a125","observedAt":"2026-10-05T05:21:02.560424+08:00","validUntil":"2026-10-05T05:21:32.560424+08:00","attendance":"UNKNOWN","visit":"UNKNOWN"}}''';
    final targetId = (jsonDecode(raw)['data']['targetId']) as String;
    final v = decodeSharedHistory(raw, targetId);
    expect((v['activities'] as List).length, 1);
    expect((v['places'] as List).length, 1);
    expect(v['attendance'], 'UNKNOWN');
  });

  test('原生shape闭集及±offset，拒绝假到场/坐标/错误主体/无关联地点', () {
    final good = currentWire();
    expect(decodeSharedHistory(jsonEncode(good), target)['visit'], 'UNKNOWN');
    final offset = currentWire();
    final v = offset['data'] as Map;
    v['observedAt'] = '2026-10-05T08:00:00+08:00';
    v['validUntil'] = '2026-10-05T08:00:29+08:00';
    expect(
      decodeSharedHistory(jsonEncode(offset), target)['attendance'],
      'UNKNOWN',
    );
    for (final key in [
      'attendance',
      'visit',
      'targetId',
      'latitude',
      'validUntil',
    ]) {
      final b = currentWire();
      final d = b['data'] as Map;
      d[key] = switch (key) {
        'attendance' => 'CONFIRMED',
        'visit' => 'VISITED',
        'targetId' => 'wrong',
        'validUntil' => '2026-02-30T00:00:00Z',
        _ => 57.15,
      };
      expect(
        () => decodeSharedHistory(jsonEncode(b), target),
        throwsFormatException,
      );
    }
    final b = currentWire();
    ((b['data'] as Map)['places'] as List).first['activityIds'] = [target];
    expect(
      () => decodeSharedHistory(jsonEncode(b), target),
      throwsFormatException,
    );
  });
  testWidgets('320大字IME260共同报名与UTC时间及48dp刷新可读，无到场到访宣称', (tester) async {
    tester.view.physicalSize = const Size(320, 640);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final client = TrackedClient(
      (_) async => response('共同报名的很长活动标题${'羽毛球' * 12}'),
    );
    String get() => 'Bearer person';
    await tester.pumpWidget(
      MaterialApp(
        home: MediaQuery(
          data: const MediaQueryData(
            size: Size(320, 640),
            textScaler: TextScaler.linear(3),
            viewInsets: EdgeInsets.only(bottom: 260),
          ),
          child: Scaffold(
            resizeToAvoidBottomInset: false,
            body: SingleChildScrollView(
              child: SharedSocialContextPanel(
                accountID: target,
                authorizationHeader: get,
                client: client,
                apiBaseUrl: 'https://a.test',
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('共同报名的公开活动'), findsOneWidget);
    expect(find.textContaining('UTC 时间'), findsOneWidget);
    expect(find.text('共同参加的公开活动'), findsNothing);
    await tester.ensureVisible(find.text('刷新共同信息'));
    await tester.pumpAndSettle();
    expect(
      tester.getSize(find.widgetWithText(TextButton, '刷新共同信息')).height,
      greaterThanOrEqualTo(48),
    );
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
    expect(client.closes, 0);
  });
  testWidgets('同key transport A→B→A永久退休迟到A，不借用close', (tester) async {
    final old = Completer<http.Response>();
    var callsA = 0, callsB = 0;
    final a = TrackedClient((_) async {
      callsA++;
      if (callsA == 1) return old.future;
      return response('新A');
    });
    final b = TrackedClient((_) async {
      callsB++;
      return response('B');
    });
    String get() => 'Bearer person';
    Widget screen(http.Client c, String base) => MaterialApp(
      home: Scaffold(
        body: SharedSocialContextPanel(
          key: const ValueKey('same'),
          accountID: target,
          authorizationHeader: get,
          client: c,
          apiBaseUrl: base,
        ),
      ),
    );
    await tester.pumpWidget(screen(a, 'https://a.test'));
    await tester.pump();
    await tester.pumpWidget(screen(b, 'https://b.test'));
    await tester.pumpAndSettle();
    expect(find.text('B'), findsOneWidget);
    await tester.pumpWidget(screen(a, 'https://a.test'));
    await tester.pumpAndSettle();
    old.complete(response('旧A'));
    await tester.pumpAndSettle();
    expect(find.text('旧A'), findsNothing);
    expect(find.text('新A'), findsOneWidget);
    expect(callsA, 2);
    expect(callsB, 1);
    await tester.pumpWidget(const SizedBox());
    expect(a.closes, 0);
    expect(b.closes, 0);
  });
  testWidgets('listener工作区ABA/账号ABA同步隐藏，旧响应不能恢复私人信息', (tester) async {
    String? workspace;
    var token = 'Bearer A';
    final changed = ValueNotifier<int>(0);
    final pending = Completer<http.Response>();
    var calls = 0;
    final client = TrackedClient((_) async {
      calls++;
      if (calls == 1) return pending.future;
      return response('当前身份');
    });
    String get() => token;
    String? w() => workspace;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SharedSocialContextPanel(
            accountID: target,
            authorizationHeader: get,
            workspaceID: w,
            identityChanges: changed,
            client: client,
            apiBaseUrl: 'https://a.test',
          ),
        ),
      ),
    );
    await tester.pump();
    workspace = 'org';
    changed.value++;
    workspace = null;
    changed.value++;
    token = 'Bearer B';
    changed.value++;
    token = 'Bearer A';
    changed.value++;
    await tester.pumpAndSettle();
    pending.complete(response('旧身份私人信号'));
    await tester.pumpAndSettle();
    expect(find.text('旧身份私人信号'), findsNothing);
    expect(find.text('当前身份'), findsOneWidget);
    expect(calls, 2);
    await tester.pumpWidget(const SizedBox());
    changed.dispose();
    expect(client.closes, 0);
  });
  testWidgets('自然期限移除旧数据且组织态不请求', (tester) async {
    final changes = ValueNotifier<int>(0);
    String? workspace;
    var calls = 0;
    String get() => 'Bearer person';
    String? w() => workspace;
    final client = MockClient((_) async {
      calls++;
      final b = currentWire();
      final d = b['data'] as Map;
      d['validUntil'] = DateTime.now()
          .toUtc()
          .add(const Duration(milliseconds: 500))
          .toIso8601String();
      return http.Response.bytes(utf8.encode(jsonEncode(b)), 200);
    });
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SharedSocialContextPanel(
            accountID: target,
            authorizationHeader: get,
            workspaceID: w,
            identityChanges: changes,
            client: client,
            apiBaseUrl: 'https://a.test',
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('合成共同报名'), findsOneWidget);
    await tester.pump(const Duration(seconds: 1));
    expect(find.text('合成共同报名'), findsNothing);
    workspace = 'org';
    changes.value++;
    await tester.pumpAndSettle();
    expect(calls, 1);
    await tester.pumpWidget(const SizedBox());
    changes.dispose();
  });
  testWidgets('服务器领先手机时短租期不从接收时续为30秒', (tester) async {
    final body = currentWire(title: '短租期共同报名');
    final d = body['data'] as Map;
    final server = DateTime.now().toUtc().add(const Duration(minutes: 5));
    d['observedAt'] = server.toIso8601String();
    d['validUntil'] = server
        .add(const Duration(milliseconds: 300))
        .toIso8601String();
    final client = TrackedClient(
      (_) async => http.Response.bytes(utf8.encode(jsonEncode(body)), 200),
    );
    String get() => 'Bearer person';
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SharedSocialContextPanel(
            accountID: target,
            authorizationHeader: get,
            client: client,
            apiBaseUrl: 'https://a.test',
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('短租期共同报名'), findsOneWidget);
    await tester.pump(const Duration(milliseconds: 600));
    expect(find.text('短租期共同报名'), findsNothing);
    await tester.pumpWidget(const SizedBox());
    expect(client.closes, 0);
  });
  testWidgets('真实网络等待超短租期迟到响应从未展示，即使服务器领先手机', (tester) async {
    final body = currentWire(title: '迟到短租期共同报名');
    final d = body['data'] as Map;
    final server = DateTime.now().toUtc().add(const Duration(minutes: 5));
    d['observedAt'] = server.toIso8601String();
    d['validUntil'] = server
        .add(const Duration(milliseconds: 300))
        .toIso8601String();
    final pending = Completer<http.Response>();
    var calls = 0;
    final client = TrackedClient((_) {
      calls++;
      return pending.future;
    });
    String get() => 'Bearer person';
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SharedSocialContextPanel(
            accountID: target,
            authorizationHeader: get,
            client: client,
            apiBaseUrl: 'https://a.test',
          ),
        ),
      ),
    );
    await tester.pump();
    expect(calls, 1);
    // Real monotonic delay only: no device/PG/host clock mutation or fake authority.
    await tester.runAsync(() async {
      await Future<void>.delayed(const Duration(milliseconds: 600));
    });
    pending.complete(http.Response.bytes(utf8.encode(jsonEncode(body)), 200));
    await tester.pumpAndSettle();
    expect(find.text('迟到短租期共同报名'), findsNothing);
    expect(find.text('共同信息暂不可用，点击重试。'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    expect(client.closes, 0);
  });
  testWidgets('祖先build中workspace变化同步退休，不setState during build且组织态0请求', (
    tester,
  ) async {
    var calls = 0;
    String? workspace;
    var changeDuringBuild = false;
    late StateSetter rebuild;
    final changes = ValueNotifier<int>(0);
    String get() => 'Bearer person';
    String? w() => workspace;
    final client = TrackedClient((_) async {
      calls++;
      return response('原共同信息');
    });
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: StatefulBuilder(
            builder: (context, set) {
              rebuild = set;
              if (changeDuringBuild) {
                changeDuringBuild = false;
                workspace = 'org';
                changes.value++;
              }
              return SharedSocialContextPanel(
                accountID: target,
                authorizationHeader: get,
                workspaceID: w,
                identityChanges: changes,
                client: client,
                apiBaseUrl: 'https://a.test',
              );
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('原共同信息'), findsOneWidget);
    rebuild(() => changeDuringBuild = true);
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    expect(find.text('原共同信息'), findsNothing);
    expect(calls, 1);
    await tester.pumpWidget(const SizedBox());
    changes.dispose();
    expect(client.closes, 0);
  });
  testWidgets('原公开Person详情实际传入identity与workspace，共同信息无新写入口', (tester) async {
    String? workspace;
    final changes = ValueNotifier<int>(0);
    var historyGET = 0, writes = 0;
    String get() => 'Bearer person';
    String? w() => workspace;
    final client = TrackedClient((r) async {
      if (r.method != 'GET') {
        writes++;
        return http.Response('{}', 503);
      }
      if (r.url.path.endsWith('/shared-context')) {
        historyGET++;
        return response('当前共同报名');
      }
      if (r.url.path.endsWith('/profile')) {
        return http.Response(
          jsonEncode({
            'data': {
              'accountId': target,
              'displayName': '公开合成伙伴',
              'visibility': 'public',
            },
          }),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }
      return http.Response('{}', 404);
    });
    await tester.pumpWidget(
      MaterialApp(
        home: PublicPersonPage(
          accountID: target,
          authorizationHeader: get,
          identityChanges: changes,
          workspaceID: w,
          apiBaseUrl: 'https://a.test',
          client: client,
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(
      find.text('当前共同报名'),
      findsOneWidget,
      reason:
          'historyGET=$historyGET; widgets=${tester.widgetList<Text>(find.byType(Text)).map((t) => t.data).toList()}',
    );
    expect(historyGET, 1);
    workspace = 'org';
    changes.value++;
    await tester.pumpAndSettle();
    expect(find.text('当前共同报名'), findsNothing);
    expect(historyGET, 1);
    expect(writes, 0);
    await tester.pumpWidget(const SizedBox());
    changes.dispose();
    expect(client.closes, 0);
  });
}
