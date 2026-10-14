import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/now_place_preview.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _place = '22000000-0000-4000-8000-000000000001';
String _momentID(int n) =>
    '22000000-0000-4000-8000-${(100 - n).toString().padLeft(12, '0')}';

http.Response _reply(Map<String, dynamic> value) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': value})),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

/// Real preview and original pagination/public-Moment adapters. Closed native
/// wire is explicitly synthetic. No history/page/ACL field is assigned by this
/// test and no production model, data service or database is contacted.
class _RecoveryFixture {
  _RecoveryFixture(
    this.failure, {
    this.previewLease = const Duration(minutes: 2),
  });
  final String failure;
  final Duration previewLease;
  final source = ValueNotifier(true);
  final token = ValueNotifier<String?>('Bearer synthetic-owner');
  final workspace = ValueNotifier<String?>(null);
  late final changes = Listenable.merge([source, token, workspace]);
  late final initialEnd = DateTime.now().toUtc();
  late final previewDeadline = initialEnd.add(previewLease);
  late DateTime windowEnd = initialEnd;
  final requests = <http.Request>[];
  final opened = <String>[];
  int pageReads = 0, momentReads = 0, generation = 0, revalidations = 0;
  bool recovered = false;
  Completer<bool>? verifyPending;

  String? _authorization() => token.value;
  String? _workspace() => workspace.value;
  bool _current() => source.value;
  Future<bool> _verify() async {
    revalidations++;
    if (verifyPending case final wait?) {
      verifyPending = null;
      return wait.future;
    }
    return source.value;
  }

  late final authorization = _authorization;
  late final workspaceID = _workspace;
  late final current = _current;
  late final verify = _verify;

  List<int> get visibleRows => [
    for (var n = 1; n <= 7; n++)
      if (!(recovered && failure == 'withdrawal' && n == 6)) n,
  ];
  int revisionOf(int n) => recovered && failure == 'revision' && n == 6 ? 3 : 2;
  Map<String, dynamic> page({required bool next}) => {
    'schemaVersion': 'place-social-history-page-v1',
    'placeId': _place,
    'cityId': 'synthetic-owned-city',
    'authorMode': 'PUBLIC_PROFILE',
    'windowDays': 30,
    'windowStart': windowEnd
        .subtract(const Duration(days: 30))
        .toIso8601String(),
    'windowEnd': windowEnd.toIso8601String(),
    'observedAt': windowEnd.toIso8601String(),
    'validUntil': windowEnd.add(const Duration(minutes: 2)).toIso8601String(),
    'sourceVersion': List.filled(64, generation == 1 ? 'a' : 'b').join(),
    'count': visibleRows.length,
    'nextCursor': next ? null : 'synthetic_cursor_$generation',
    'items': [
      for (final n in next ? visibleRows.skip(5) : visibleRows.take(5))
        {
          'id': _momentID(n),
          'title': '公开动态 $n',
          'excerpt': '本人明确公开的合成节选 $n',
          'revision': revisionOf(n),
          'publishedAt': windowEnd
              .subtract(Duration(days: n))
              .toIso8601String(),
          'authorId': null,
          'authorPublicProfile': null,
        },
    ],
  };
  Map<String, dynamic> moment(int n, {int? revision}) => {
    'schemaVersion': 'public-moment-v2',
    'id': _momentID(n),
    'title': '公开动态 $n',
    'body': '本人明确公开的完整合成正文 $n',
    'placeId': _place,
    'placeName': '真实来源中的合成地点',
    'cityId': 'synthetic-owned-city',
    'revision': revision ?? revisionOf(n),
    'publishedAt': windowEnd.subtract(Duration(days: n)).toIso8601String(),
    'authorId': null,
    'authorPublicProfile': null,
  };

  late final client = MockClient((r) async {
    requests.add(r);
    expect(r.method, 'GET');
    expect(r.headers['Authorization'], token.value);
    if (r.url.path == '/v1/places/$_place/social-history/moments') {
      pageReads++;
      final next = r.url.queryParameters.containsKey('cursor');
      if (!next) {
        generation++;
        windowEnd = DateTime.now().toUtc();
        recovered = generation > 1;
      }
      expect(r.url.queryParameters, {
        'include': 'public-author',
        if (next) 'cursor': 'synthetic_cursor_$generation',
      });
      return _reply(page(next: next));
    }
    final n = visibleRows.singleWhere(
      (id) => r.url.path == '/v1/moments/${_momentID(id)}',
    );
    momentReads++;
    expect(r.url.queryParameters, {'include': 'public-author'});
    if (!recovered) {
      expect(n, 6, reason: 'The failure concerns an actual later-page object.');
      switch (failure) {
        case 'network':
          return http.Response('{}', 503);
        case 'withdrawal':
          return http.Response('{}', 404);
        case 'revision':
          return _reply(moment(n, revision: 3));
      }
    }
    return _reply(moment(n));
  });

  Future<void> mount(WidgetTester t) async {
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SingleChildScrollView(
            padding: const EdgeInsets.all(16),
            child: NowPlacePreview(
              key: const Key('original-recovery-preview'),
              placeID: _place,
              placeTitle: '真实来源中的合成地点',
              validUntil: previewDeadline,
              authorizationHeader: authorization,
              workspaceID: workspaceID,
              sourceChanges: changes,
              current: current,
              revalidate: verify,
              onClose: () {},
              onOpenPlace: () {},
              onOpenMoment: opened.add,
              apiBaseUrl: 'https://recovery-unit.test',
              client: client,
            ),
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.text('最近 30 天 · 7 条公开分享'), findsOneWidget);
    expect(pageReads, 1);
  }

  Future<void> tap(WidgetTester t, Finder control) async {
    await t.ensureVisible(control);
    await t.pumpAndSettle();
    expect(control.hitTestable(), findsOneWidget);
    await t.tap(control);
    await t.pumpAndSettle();
  }

  Future<void> laterPage(WidgetTester t) async {
    await tap(t, find.text('看看公开动态'));
    await tap(t, find.byTooltip('下一组公开动态'));
    await tap(t, find.byKey(const ValueKey('place-stories-load-more')));
    expect(find.text('公开动态 6'), findsOneWidget);
    expect(pageReads, recovered ? 4 : 2);
  }

  Future<void> failLaterMoment(WidgetTester t) async {
    await laterPage(t);
    await tap(t, find.text('公开动态 6'));
    expect(opened, isEmpty);
    expect(pageReads, 2);
    expect(momentReads, 1);
    expect(find.text('公开动态 6'), findsNothing);
    expect(find.byType(NowPlaceStoryStrip), findsNothing);
    expect(
      find.text(failure == 'revision' ? '这条动态已变化，请重新查看。' : '这条动态暂时无法查看，请重新读取。'),
      findsOneWidget,
    );
  }

  Finder get retry => find.widgetWithText(TextButton, '重新读取公开动态');
  void assertRetry(WidgetTester t) {
    expect(
      retry,
      findsOneWidget,
      reason: 'One recovery control survives object failure.',
    );
    expect(t.getSize(retry).height, greaterThanOrEqualTo(48));
  }

  void assertNativeOnly(WidgetTester t) {
    expect(requests.every((r) => r.method == 'GET'), isTrue);
    expect(requests.any((r) => r.url.path.contains('/agent/')), isFalse);
    expect(t.takeException(), isNull);
  }

  Future<void> finish(WidgetTester t) async {
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
  for (final failure in ['network', 'withdrawal', 'revision']) {
    testWidgets('后页Moment $failure清旧数据后单一重试真实新page GET恢复同一预览', (t) async {
      final f = _RecoveryFixture(failure);
      try {
        await f.mount(t);
        final state = t.state(find.byType(NowPlacePreview));
        final originalDeadline = t
            .widget<NowPlacePreview>(find.byType(NowPlacePreview))
            .validUntil;
        await f.failLaterMoment(t);
        f.assertRetry(t);
        expect(find.text('看看公开动态'), findsNothing);
        expect(find.textContaining('0 条公开分享'), findsNothing);
        await f.tap(t, f.retry);
        expect(f.pageReads, 3);
        expect(
          f.momentReads,
          1,
          reason:
              'Retry must refresh the page, never replay the object action.',
        );
        expect(
          f.requests.last.url.path,
          '/v1/places/$_place/social-history/moments',
        );
        expect(f.requests.last.url.queryParameters, {
          'include': 'public-author',
        });
        expect(t.state(find.byType(NowPlacePreview)), same(state));
        expect(
          t.widget<NowPlacePreview>(find.byType(NowPlacePreview)).validUntil,
          originalDeadline,
        );
        expect(f.retry, findsNothing);
        expect(find.text('这条动态暂时无法查看，请重新读取。'), findsNothing);
        expect(find.text('这条动态已变化，请重新查看。'), findsNothing);
        expect(find.text('公开动态 1'), findsOneWidget);
        expect(
          find.text('最近 30 天 · ${failure == 'withdrawal' ? 6 : 7} 条公开分享'),
          findsOneWidget,
        );
        // Continue the NEW page generation. Withdrawal excludes old row six;
        // a changed revision is accepted only after this new native page lists it.
        await f.tap(t, find.byTooltip('下一组公开动态'));
        await f.tap(t, find.byKey(const ValueKey('place-stories-load-more')));
        final selected = failure == 'withdrawal' ? 7 : 6;
        if (failure == 'withdrawal') expect(find.text('公开动态 6'), findsNothing);
        await f.tap(t, find.text('公开动态 $selected'));
        expect(f.opened, [_momentID(selected)]);
        expect(f.pageReads, 4);
        expect(f.momentReads, 2);
        f.assertNativeOnly(t);
      } finally {
        await f.finish(t);
      }
    });
  }

  testWidgets('重试重核跨source ABA迟到true不发新page GET且旧回调不能恢复', (t) async {
    final f = _RecoveryFixture('network');
    try {
      await f.mount(t);
      await f.failLaterMoment(t);
      f.assertRetry(t);
      final originalRetry = t.widget<TextButton>(f.retry).onPressed!;
      final verify = Completer<bool>();
      f.verifyPending = verify;
      await t.ensureVisible(f.retry);
      await t.tap(f.retry);
      await t.pump();
      f.source.value = false;
      f.source.value = true;
      verify.complete(true);
      await t.pumpAndSettle();
      expect(find.text('请重新打开地点'), findsOneWidget);
      expect(f.retry, findsNothing);
      expect(find.text('公开动态 1'), findsNothing);
      expect(find.text('公开动态 6'), findsNothing);
      originalRetry();
      await t.pumpAndSettle();
      expect(f.pageReads, 2);
      expect(f.momentReads, 1);
      expect(f.opened, isEmpty);
      f.assertNativeOnly(t);
    } finally {
      await f.finish(t);
    }
  });

  testWidgets('失败恢复的新page不能延长原预览期限，到期旧重试不操作', (t) async {
    final f = _RecoveryFixture(
      'network',
      previewLease: const Duration(seconds: 10),
    );
    try {
      await f.mount(t);
      final originalDeadline = t
          .widget<NowPlacePreview>(find.byType(NowPlacePreview))
          .validUntil;
      await f.failLaterMoment(t);
      f.assertRetry(t);
      final originalRetry = t.widget<TextButton>(f.retry).onPressed!;
      await f.tap(t, f.retry);
      expect(f.pageReads, 3);
      expect(find.text('公开动态 1'), findsOneWidget);
      expect(
        t.widget<NowPlacePreview>(find.byType(NowPlacePreview)).validUntil,
        originalDeadline,
      );
      await t.pump(const Duration(seconds: 11));
      await t.pumpAndSettle();
      expect(find.text('请重新打开地点'), findsOneWidget);
      expect(find.text('公开动态 1'), findsNothing);
      expect(f.retry, findsNothing);
      originalRetry();
      await t.pumpAndSettle();
      expect(f.pageReads, 3);
      expect(f.momentReads, 1);
      expect(f.opened, isEmpty);
      f.assertNativeOnly(t);
    } finally {
      await f.finish(t);
    }
  });
}
