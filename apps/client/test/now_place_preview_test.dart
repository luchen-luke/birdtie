import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/now_place_preview.dart';
import 'package:birdtie_client/src/workspace/place_history_controller.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _place = '22000000-0000-4000-8000-000000000001';
String _momentID(int n) =>
    '22000000-0000-4000-8000-${n.toString().padLeft(12, '0')}';

// Synthetic closed native-page wire. All pages retain one original window,
// source and deadline; five initial rows plus two continuation rows total seven.
Map<String, dynamic> _history({
  required DateTime end,
  int start = 1,
  int rows = 5,
  int total = 7,
  String? title,
}) => {
  'schemaVersion': 'place-social-history-page-v1',
  'placeId': _place,
  'cityId': 'synthetic-owned-city',
  'authorMode': 'PUBLIC_PROFILE',
  'windowDays': 30,
  'windowStart': end.subtract(const Duration(days: 30)).toIso8601String(),
  'windowEnd': end.toIso8601String(),
  'observedAt': end.toIso8601String(),
  'validUntil': end.add(const Duration(minutes: 2)).toIso8601String(),
  'sourceVersion': List.filled(64, total == 0 ? 'b' : 'a').join(),
  'count': total,
  'items': [
    for (var n = start; n < start + rows; n++)
      {
        'id': _momentID(n),
        'title': title ?? '公开动态 $n',
        'excerpt': '本人明确公开的合成记录 $n',
        'revision': 2,
        'publishedAt': end.subtract(Duration(days: n)).toIso8601String(),
        'authorId': null,
        'authorPublicProfile': null,
      },
  ],
  'nextCursor': start + rows - 1 < total ? 'synthetic_preview_cursor' : null,
};

http.Response _reply(Map<String, dynamic> data) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': data})),
  200,
  headers: {'content-type': 'application/json'},
);

class _Fixture {
  _Fixture() {
    client = MockClient((request) async {
      requests.add(request);
      expect(request.method, 'GET');
      expect(request.url.host, 'unit.test');
      if (request.url.path == '/v1/places/$_place/social-history/moments') {
        final next = request.url.queryParameters.containsKey('cursor');
        expect(request.url.queryParameters, {
          'include': 'public-author',
          if (next) 'cursor': 'synthetic_preview_cursor',
        });
        if (pending case final value?) {
          pending = null;
          return value.future;
        }
        return unavailable
            ? http.Response('{}', 503)
            : _reply(data(next: next));
      }
      final n = List.generate(
        7,
        (i) => i + 1,
      ).singleWhere((i) => request.url.path == '/v1/moments/${_momentID(i)}');
      expect(request.url.queryParameters, {'include': 'public-author'});
      // Withdrawal is an authoritative native object read failure, never an
      // invented zero-row response or permission to open the cached ID.
      if (withdrawn) return http.Response('{}', 404);
      return _reply({
        'schemaVersion': 'public-moment-v2',
        'id': _momentID(n),
        'title': longTitle ?? '公开动态 $n',
        'body': '本人明确公开的合成完整正文 $n',
        'placeId': _place,
        'placeName': '真实来源中的地点标题',
        'cityId': 'synthetic-owned-city',
        'revision': 2,
        'publishedAt': historyEnd.subtract(Duration(days: n)).toIso8601String(),
        'authorId': null,
        'authorPublicProfile': null,
      });
    });
  }

  final source = ValueNotifier(true);
  final token = ValueNotifier<String?>(null);
  final workspace = ValueNotifier<String?>(null);
  late final changes = Listenable.merge([source, token, workspace]);
  late final http.Client client;
  late final historyEnd = DateTime.now().toUtc();
  late final deadline = historyEnd.add(const Duration(minutes: 2));
  final requests = <http.Request>[];
  final opened = <String>[];
  int placeOpens = 0, closes = 0, revalidations = 0;
  bool unavailable = false, withdrawn = false;
  String? longTitle;
  Completer<http.Response>? pending;
  Completer<bool>? verifyPending;

  String? _authorization() => token.value;
  String? _workspace() => workspace.value;
  bool _current() => source.value;
  Future<bool> _verify() async {
    revalidations++;
    if (verifyPending case final value?) {
      verifyPending = null;
      return value.future;
    }
    return source.value;
  }

  late final authorization = _authorization;
  late final workspaceID = _workspace;
  late final current = _current;
  late final verify = _verify;

  Map<String, dynamic> data({bool next = false}) => _history(
    end: historyEnd,
    start: next ? 6 : 1,
    rows: withdrawn
        ? 0
        : next
        ? 2
        : 5,
    total: withdrawn ? 0 : 7,
    title: longTitle,
  );

  Future<void> mount(
    WidgetTester t, {
    double scale = 1,
    bool dark = false,
    double width = 390,
    DateTime? validUntil,
    http.Client? transport,
  }) async {
    final waiting = pending != null || verifyPending != null;
    t.view.physicalSize = Size(width, 844);
    t.view.devicePixelRatio = 1;
    await t.pumpWidget(
      MaterialApp(
        theme: ThemeData(brightness: dark ? Brightness.dark : Brightness.light),
        home: MediaQuery(
          data: MediaQueryData(
            size: Size(width, 844),
            textScaler: TextScaler.linear(scale),
          ),
          child: Scaffold(
            body: SingleChildScrollView(
              padding: const EdgeInsets.all(16),
              child: NowPlacePreview(
                key: const Key('original-preview-binding'),
                placeID: _place,
                placeTitle: '真实来源中的地点标题',
                validUntil: validUntil ?? deadline,
                authorizationHeader: authorization,
                workspaceID: workspaceID,
                sourceChanges: changes,
                current: current,
                revalidate: verify,
                onClose: () => closes++,
                onOpenPlace: () => placeOpens++,
                onOpenMoment: opened.add,
                apiBaseUrl: 'https://unit.test',
                client: transport ?? client,
              ),
            ),
          ),
        ),
      ),
    );
    if (waiting) {
      await t.pump();
      await t.pump();
    } else {
      await t.pumpAndSettle();
    }
  }

  Future<void> expand(WidgetTester t) async {
    await t.tap(find.text('看看公开动态'));
    await t.pumpAndSettle();
  }

  Future<void> finish(WidgetTester t) async {
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox.shrink());
    source.dispose();
    token.dispose();
    workspace.dispose();
    client.close();
    t.view.resetPhysicalSize();
    t.view.resetDevicePixelRatio();
  }
}

void main() {
  testWidgets(
    'recording available Place opens independently while stories are loading',
    (t) async {
      final f = _Fixture();
      final response = Completer<http.Response>();
      f.pending = response;
      await f.mount(t);
      expect(find.text('正在读取公开动态…'), findsOneWidget);
      expect(
        t
            .widget<TextButton>(
              find.ancestor(
                of: find.text('查看地点'),
                matching: find.byType(TextButton),
              ),
            )
            .onPressed,
        isNotNull,
      );
      await t.tap(find.text('查看地点'));
      await t.pump();
      await t.pump();
      expect(f.placeOpens, 1);
      expect(f.revalidations, 1);
      expect(f.requests, hasLength(1));
      response.complete(_reply(f.data()));
      await t.pumpAndSettle();
      expect(find.text('最近 30 天 · 7 条公开分享'), findsOneWidget);
      await f.finish(t);
    },
  );
  testWidgets(
    'recording empty Place preview has one compact empty message and real action',
    (t) async {
      final f = _Fixture()..withdrawn = true;
      await f.mount(t);
      expect(find.text('真实来源中的地点标题'), findsOneWidget);
      expect(find.text('暂无仍公开的近期分享。'), findsOneWidget);
      expect(find.textContaining('0 条公开分享'), findsNothing);
      expect(find.text('暂未提供公开图片'), findsNothing);
      expect(find.byIcon(Icons.image_not_supported_outlined), findsNothing);
      await t.tap(find.text('查看地点'));
      await t.pumpAndSettle();
      expect(f.placeOpens, 1);
      expect(f.opened, isEmpty);
      await f.finish(t);
    },
  );
  testWidgets('原授权回包五条可横向翻页，最多三张，计数不冒充作者或分页', (t) async {
    final f = _Fixture();
    await f.mount(t);
    expect(find.text('最近 30 天 · 7 条公开分享'), findsOneWidget);
    expect(find.textContaining('位作者'), findsNothing);
    await f.expand(t);
    expect(find.text('公开动态 1'), findsOneWidget);
    expect(find.text('公开动态 3'), findsOneWidget);
    expect(find.text('公开动态 4'), findsNothing);
    expect(find.text('作者资料未公开'), findsNWidgets(3));
    expect(
      find.byKey(const ValueKey('place-stories-load-more')),
      findsOneWidget,
    );
    expect(find.textContaining('摘要未提供下一页'), findsNothing);
    await t.drag(find.byType(NowPlaceStoryStrip), const Offset(-120, 0));
    await t.pumpAndSettle();
    expect(find.text('公开动态 1'), findsNothing);
    expect(find.text('公开动态 4'), findsOneWidget);
    expect(find.text('公开动态 5'), findsOneWidget);
    await t.tap(find.text('公开动态 4'));
    await t.pumpAndSettle();
    expect(f.opened, [_momentID(4)]);
    // Initial native page plus exact selected Moment read. Expanding already
    // loaded stories and opening row four do not refresh the first page.
    expect(f.requests, hasLength(2));
    expect(
      f.requests.first.url.path,
      '/v1/places/$_place/social-history/moments',
    );
    expect(f.requests.last.url.path, '/v1/moments/${_momentID(4)}');
    expect(
      f.requests.every(
        (r) => r.url.queryParameters['include'] == 'public-author',
      ),
      isTrue,
    );
    expect(f.requests.every((r) => r.method == 'GET'), isTrue);
    expect(find.byType(Image), findsNothing);
    await f.finish(t);
  });

  testWidgets('读取失败数量未知，与真实零条严格区分，重试不匿名降权', (t) async {
    final f = _Fixture()..unavailable = true;
    f.token.value = 'Bearer synthetic-owner';
    await f.mount(t);
    // A failed read now has one error with recovery, without an additional
    // unknown-count line. Authoritative zero remains a distinct later state.
    expect(find.text('公开动态数量尚未读取'), findsNothing);
    expect(find.text('重新读取公开动态'), findsOneWidget);
    expect(find.text('最近 30 天 · 0 条公开分享'), findsNothing);
    expect(find.text('暂无仍公开的近期分享。'), findsNothing);
    expect(
      f.requests.single.headers['Authorization'],
      'Bearer synthetic-owner',
    );
    f.unavailable = false;
    f.withdrawn = true;
    await t.tap(find.text('重新读取公开动态'));
    await t.pumpAndSettle();
    expect(find.text('最近 30 天 · 0 条公开分享'), findsNothing);
    expect(find.text('暂无仍公开的近期分享。'), findsOneWidget);
    expect(f.requests.last.headers['Authorization'], 'Bearer synthetic-owner');
    await f.finish(t);
  });

  for (final mode in ['source', 'account', 'workspace']) {
    testWidgets('$mode ABA永久退役，不回生旧标题或公开记录', (t) async {
      final f = _Fixture();
      await f.mount(t);
      await f.expand(t);
      final reads = f.requests.length;
      switch (mode) {
        case 'source':
          f.source.value = false;
          f.source.value = true;
        case 'account':
          f.token.value = 'Bearer different-account';
          f.token.value = null;
        case 'workspace':
          f.workspace.value = 'different-workspace';
          f.workspace.value = null;
      }
      await t.pumpAndSettle();
      expect(find.text('请重新打开地点'), findsOneWidget);
      expect(find.text('真实来源中的地点标题'), findsNothing);
      expect(find.text('公开动态 1'), findsNothing);
      expect(f.requests.length, reads);
      expect(f.opened, isEmpty);
      expect(find.text('查看地点'), findsNothing);
      await f.finish(t);
    });
  }

  testWidgets('来源失效后迟到摘要无法恢复旧正文，期限不会被刷新续租', (t) async {
    final f = _Fixture();
    final response = Completer<http.Response>();
    f.pending = response;
    await f.mount(
      t,
      validUntil: DateTime.now().toUtc().add(const Duration(seconds: 2)),
    );
    await t.pump(const Duration(seconds: 3));
    response.complete(_reply(f.data()));
    await t.pumpAndSettle();
    expect(find.text('请重新打开地点'), findsOneWidget);
    expect(find.text('真实来源中的地点标题'), findsNothing);
    expect(find.textContaining('7 条公开分享'), findsNothing);
    await f.finish(t);
  });

  testWidgets('重核晚到true不能在source ABA后打开旧故事', (t) async {
    final f = _Fixture();
    await f.mount(t);
    await f.expand(t);
    final verification = Completer<bool>();
    f.verifyPending = verification;
    await t.tap(find.text('公开动态 1'));
    await t.pump();
    f.source.value = false;
    f.source.value = true;
    verification.complete(true);
    await t.pumpAndSettle();
    expect(f.opened, isEmpty);
    expect(find.text('请重新打开地点'), findsOneWidget);
    await f.finish(t);
  });

  testWidgets('公开动态撤回或版本变化需重读，不能只用旧ID导航', (t) async {
    final f = _Fixture();
    await f.mount(t);
    await f.expand(t);
    f.withdrawn = true;
    await t.tap(find.text('公开动态 1'));
    await t.pumpAndSettle();
    expect(f.opened, isEmpty);
    expect(find.text('这条动态暂时无法查看，请重新读取。'), findsOneWidget);
    expect(f.requests.last.url.path, '/v1/moments/${_momentID(1)}');
    expect(find.text('公开动态 1'), findsNothing);
    await f.finish(t);
  });

  testWidgets('普通Theme字号变化保留稳定provider，200%长中文自然滚动操作可达', (t) async {
    final f = _Fixture();
    f.longTitle = '这是一条很长的中文地点故事标题带有 Aberdeen 与 café 混排内容';
    await f.mount(t);
    await f.expand(t);
    final state = t.state(find.byType(NowPlacePreview));
    final reads = f.requests.length;
    for (final scale in [1.3, 2.0]) {
      await f.mount(t, scale: scale, dark: true, width: scale == 2 ? 320 : 390);
      expect(t.state(find.byType(NowPlacePreview)), same(state));
      expect(find.text('请重新打开地点'), findsNothing);
      expect(f.requests.length, reads);
      expect(find.text(f.longTitle!), findsNWidgets(scale == 2 ? 1 : 2));
      expect(t.takeException(), isNull);
      final close = find.byTooltip('收起地点预览');
      expect(t.getSize(close).width, greaterThanOrEqualTo(48));
      expect(t.getSize(close).height, greaterThanOrEqualTo(48));
      await t.ensureVisible(find.text('查看地点'));
      await t.tap(find.text('查看地点'));
      await t.pumpAndSettle();
      expect(f.placeOpens, scale == 1.3 ? 1 : 2);
    }
    await f.finish(t);
  });

  testWidgets('真实transport替换永久退役，展示rebuild不能绕过原连接边界', (t) async {
    final f = _Fixture();
    await f.mount(t);
    final replacement = MockClient((_) async => _reply(f.data()));
    await f.mount(t, transport: replacement);
    expect(find.text('请重新打开地点'), findsOneWidget);
    expect(find.text('真实来源中的地点标题'), findsNothing);
    expect(f.requests, hasLength(1));
    replacement.close();
    await f.finish(t);
  });

  testWidgets('横向组件不以三条限制数据，八条分页全部可达', (t) async {
    final moments = [
      for (var i = 1; i <= 8; i++)
        PlaceHistoryMoment(
          '合成故事 $i',
          '',
          DateTime.utc(2026, 10, 1),
          id: _momentID(i),
          revision: 2,
        ),
    ];
    final opened = <String?>[];
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            width: 350,
            child: NowPlaceStoryStrip(
              moments: moments,
              onOpen: (m) => opened.add(m.id),
            ),
          ),
        ),
      ),
    );
    expect(find.text('合成故事 3'), findsOneWidget);
    expect(find.text('合成故事 4'), findsNothing);
    await t.tap(find.byTooltip('下一组公开动态'));
    await t.pumpAndSettle();
    expect(find.text('合成故事 6'), findsOneWidget);
    await t.tap(find.byTooltip('下一组公开动态'));
    await t.pumpAndSettle();
    expect(find.text('合成故事 7'), findsOneWidget);
    expect(find.text('合成故事 8'), findsOneWidget);
    expect(find.text('合成故事 6'), findsNothing);
    await t.tap(find.text('合成故事 8'));
    expect(opened, [_momentID(8)]);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox.shrink());
  });
}
