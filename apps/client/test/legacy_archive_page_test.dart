import 'dart:async';
import 'dart:io';
import 'package:birdtie_client/src/content/legacy_archive_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'legacy_archive_api_test.dart';
import 'batch_a_visual_test.dart' show visualTheme, capture, captureKey;

Future<void> archivePump(WidgetTester t) async {
  for (var i = 0; i < 6; i++) {
    await t.pump(const Duration(milliseconds: 25));
    await t.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 10)),
    );
  }
  await t.pump();
}

class ArchivePageFixture {
  ArchivePageFixture() {
    client = ArchiveBorrowedClient((r) async {
      calls.add(r);
      expectSync(r.method, 'GET');
      expectSync(r.bodyBytes, isEmpty);
      expectSync(r.url.origin, archiveOrigin);
      expectSync(
        r.headers.keys.any((k) => k.toLowerCase().contains('workspace')),
        false,
      );
      return reader(r);
    });
    page = LegacyArchivePage(
      authorizationHeader: () => header,
      ownerID: () => owner,
      organizationWorkspaceID: () => workspace,
      identityChanges: changes,
      sourceKey: () => logicalSource,
      sourceCurrent: () => current,
      client: client,
      apiBaseUrl: archiveOrigin,
    );
  }
  String? header = 'Bearer synthetic-A', owner = archiveOwner, workspace;
  int logicalSource = 1;
  bool current = true;
  final changes = ValueNotifier(0);
  final calls = <http.Request>[];
  late final ArchiveBorrowedClient client;
  late final LegacyArchivePage page;
  Future<http.Response> Function(http.Request) reader = (r) async =>
      r.url.path.endsWith('/migration-archive')
      ? archiveResponse({
          'items': [archiveSummary(1)],
        })
      : archiveResponse(archiveDetail(1));
  void notify() {
    changes.value++;
  }

  Widget harness({Widget? child}) => MaterialApp(
    debugShowCheckedModeBanner: false,
    theme: visualTheme(),
    builder: (context, content) => MediaQuery(
      data: MediaQuery.of(context).copyWith(textScaler: TextScaler.linear(1)),
      child: content!,
    ),
    home: RepaintBoundary(
      key: captureKey,
      child: Stack(
        children: [
          child ?? page,
          const Positioned(
            bottom: 0,
            left: 0,
            right: 0,
            child: ColoredBox(
              color: Colors.white,
              child: Text(
                '隔离样例 · 非真实旧用户 · NOT_DEVICE',
                textAlign: TextAlign.center,
                style: TextStyle(
                  fontFamily: 'BatchACJK',
                  fontSize: 10,
                  color: Colors.black87,
                  decoration: TextDecoration.none,
                ),
              ),
            ),
          ),
        ],
      ),
    ),
  );
  void dispose() {
    changes.dispose();
    client.close();
  }
}

Future<void> archiveSize(WidgetTester t) async {
  t.view.physicalSize = const Size(390, 844);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
}

Future<void> archiveMount(WidgetTester t, ArchivePageFixture f) async {
  await archiveSize(t);
  await t.pumpWidget(f.harness());
  await archivePump(t);
}

Future<void> archiveClose(WidgetTester t, ArchivePageFixture f) async {
  await t.pumpWidget(const SizedBox());
  f.dispose();
}

void main() {
  setUpAll(() async {
    if (Platform.environment['BIRDTIE_BATCH_A_CAPTURE'] == null) return;
    for (final font in [
      ('BatchACJK', r'C:\Windows\Fonts\msyh.ttc'),
      (
        'MaterialIcons',
        r'D:\DevTools\flutter\bin\cache\artifacts\material_fonts\MaterialIcons-Regular.otf',
      ),
    ]) {
      await (FontLoader(font.$1)..addFont(
            Future.value(
              ByteData.sublistView(await File(font.$2).readAsBytes()),
            ),
          ))
          .load();
    }
  });
  testWidgets(
    'archive normal390 raw full caption unknown date ordered relationships no pixels or effects',
    (t) async {
      final f = ArchivePageFixture();
      f.reader = (r) async => r.url.path.endsWith('/migration-archive')
          ? archiveResponse({
              'items': [
                {...archiveSummary(1, title: ''), 'recordedStartDate': null},
              ],
            })
          : archiveResponse({
              ...archiveDetail(1, title: ''),
              'recordedStartDate': null,
            });
      await archiveMount(t, f);
      expect(find.text('未命名的旧记录'), findsOneWidget);
      expect(find.text('原记录日期未提供'), findsOneWidget);
      await t.tap(find.byKey(ValueKey('legacy-archive-row-' + archiveID(1))));
      await archivePump(t);
      expect(
        t.widget<Text>(find.byKey(const Key('legacy-archive-body'))).data,
        archiveDetail(1)['body'],
      );
      expect(find.text('原记录日期未提供'), findsOneWidget);
      expect(find.byType(Image), findsNothing);
      await capture(t, 'archive-detail-normal390');
      await t.scrollUntilVisible(
        find.text('原关联地点'),
        180,
        scrollable: find.byType(Scrollable).first,
      );
      expect(find.text('原顺序 8 · 原地点乙'), findsOneWidget);
      expect(find.text('原顺序 2 · 原地点甲'), findsOneWidget);
      await t.scrollUntilVisible(
        find.text('原路线段'),
        180,
        scrollable: find.byType(Scrollable).first,
      );
      expect(find.textContaining('原顺序 4 → 5 · 步行'), findsOneWidget);
      await t.drag(
        find.byKey(const Key('legacy-archive-scroll')),
        const Offset(0, -420),
      );
      await archivePump(t);
      await capture(t, 'archive-history-normal390');
      expect(f.calls, hasLength(2));
      expect(
        f.calls.every((r) => r.url.path.startsWith('/v1/me/migration-archive')),
        true,
      );
      expect(t.takeException(), isNull);
      await archiveClose(t, f);
    },
  );
  testWidgets(
    'archive read 20 plus tail without truncation and end disables further pages',
    (t) async {
      final f = ArchivePageFixture();
      f.reader = (r) async => archiveResponse(
        r.url.queryParameters.containsKey('cursor')
            ? {
                'items': [archiveSummary(21)],
              }
            : {
                'items': [for (var i = 1; i <= 20; i++) archiveSummary(i)],
                'nextCursor': 'opaque20',
              },
      );
      await archiveMount(t, f);
      await t.scrollUntilVisible(
        find.byKey(const Key('legacy-archive-more')),
        300,
        scrollable: find.byType(Scrollable).first,
      );
      await t.tap(find.byKey(const Key('legacy-archive-more')));
      await archivePump(t);
      await t.scrollUntilVisible(
        find.text('隔离旧记录 21'),
        150,
        scrollable: find.byType(Scrollable).first,
      );
      expect(find.text('隔离旧记录 21'), findsOneWidget);
      expect(find.byKey(const Key('legacy-archive-more')), findsNothing);
      expect(f.calls, hasLength(2));
      expect(f.calls.last.url.queryParameters, {'cursor': 'opaque20'});
      await archiveClose(t, f);
    },
  );
  testWidgets(
    'archive failed tail retains prior rows retry reads same cursor and then recovers',
    (t) async {
      final f = ArchivePageFixture();
      var failures = 1;
      f.reader = (r) async {
        if (r.url.queryParameters.isEmpty)
          return archiveResponse({
            'items': [archiveSummary(1)],
            'nextCursor': 'opaque1',
          });
        if (failures-- > 0) return archiveResponse(null, status: 503);
        return archiveResponse({
          'items': [archiveSummary(2)],
        });
      };
      await archiveMount(t, f);
      await t.tap(find.byKey(const Key('legacy-archive-more')));
      await archivePump(t);
      expect(find.text('隔离旧记录 1'), findsOneWidget);
      expect(find.text('暂无旧内容档案。'), findsNothing);
      expect(find.byKey(const Key('legacy-archive-error')), findsOneWidget);
      await t.tap(find.byKey(const Key('legacy-archive-more')));
      await archivePump(t);
      expect(find.text('隔离旧记录 2'), findsOneWidget);
      expect(f.calls.last.url.queryParameters, {'cursor': 'opaque1'});
      await archiveClose(t, f);
    },
  );
  for (final invalid in ['duplicate', 'cursor-cycle', 'changed']) {
    testWidgets(
      'archive $invalid tail stops unsafe cursor and explicit first-page recovery',
      (t) async {
        final f = ArchivePageFixture();
        f.reader = (r) async {
          final cursor = r.url.queryParameters['cursor'];
          if (cursor == null)
            return archiveResponse({
              'items': [archiveSummary(1)],
              'nextCursor': 'opaque1',
            });
          if (invalid == 'changed') return archiveResponse(null, status: 409);
          return archiveResponse({
            'items': [archiveSummary(invalid == 'duplicate' ? 1 : 2)],
            if (invalid == 'cursor-cycle') 'nextCursor': cursor,
          });
        };
        await archiveMount(t, f);
        final old = t
            .widget<OutlinedButton>(
              find.byKey(const Key('legacy-archive-more')),
            )
            .onPressed!;
        old();
        await archivePump(t);
        expect(find.byKey(const Key('legacy-archive-error')), findsOneWidget);
        expect(find.byKey(const Key('legacy-archive-more')), findsNothing);
        expect(find.text('隔离旧记录 1'), findsOneWidget);
        final n = f.calls.length;
        old();
        await archivePump(t);
        expect(f.calls.length, n);
        await t.tap(find.byKey(const Key('legacy-archive-reload')));
        await archivePump(t);
        expect(f.calls.last.url.queryParameters, isEmpty);
        expect(find.byKey(const Key('legacy-archive-more')), findsOneWidget);
        await archiveClose(t, f);
      },
    );
  }
  for (final status in [200, 403, 503]) {
    testWidgets(
      'archive actual empty permission error $status has no false historical content',
      (t) async {
        final f = ArchivePageFixture();
        f.reader = (_) async => archiveResponse({'items': []}, status: status);
        await archiveMount(t, f);
        expect(
          find.text('暂无旧内容档案。'),
          status == 200 ? findsOneWidget : findsNothing,
        );
        expect(
          find.byKey(const Key('legacy-archive-error')),
          status == 200 ? findsNothing : findsOneWidget,
        );
        expect(find.byType(Image), findsNothing);
        if (status == 403) await capture(t, 'archive-permission-normal390');
        await archiveClose(t, f);
      },
    );
  }
  testWidgets(
    'archive deleted tombstone offers no original caption relationships media or restoration',
    (t) async {
      final f = ArchivePageFixture();
      f.reader = (r) async => r.url.path.endsWith('/migration-archive')
          ? archiveResponse({
              'items': [archiveSummary(1, deleted: true)],
            })
          : archiveResponse(archiveDetail(1, deleted: true));
      await archiveMount(t, f);
      await t.tap(find.byKey(ValueKey('legacy-archive-row-' + archiveID(1))));
      await archivePump(t);
      expect(find.text('原内容已删除。档案只保留删除记录，正文与媒体不会恢复。'), findsOneWidget);
      expect(find.byKey(const Key('legacy-archive-body')), findsNothing);
      expect(find.text('原关联地点'), findsNothing);
      expect(find.byType(Image), findsNothing);
      expect(f.calls.every((r) => r.method == 'GET'), true);
      await archiveClose(t, f);
    },
  );
  testWidgets(
    'archive same-owner renewal rejects old row callbacks then fresh current GET stays usable',
    (t) async {
      final f = ArchivePageFixture();
      await archiveMount(t, f);
      final old = t
          .widget<ListTile>(
            find.byKey(ValueKey('legacy-archive-row-' + archiveID(1))),
          )
          .onTap!;
      f.header = 'Bearer synthetic-B';
      f.notify();
      await archivePump(t);
      final count = f.calls.length;
      old();
      await archivePump(t);
      expect(f.calls.length, count);
      expect(f.calls.last.headers['authorization'], 'Bearer synthetic-B');
      await t.tap(find.byKey(ValueKey('legacy-archive-row-' + archiveID(1))));
      await archivePump(t);
      expect(find.byKey(const Key('legacy-archive-body')), findsOneWidget);
      expect(f.calls.last.headers['authorization'], 'Bearer synthetic-B');
      await archiveClose(t, f);
    },
  );
  for (final axis in ['owner', 'workspace', 'source']) {
    testWidgets(
      'archive $axis ABA removes body and captured row cannot GET after return',
      (t) async {
        final f = ArchivePageFixture();
        await archiveMount(t, f);
        final old = t
            .widget<ListTile>(
              find.byKey(ValueKey('legacy-archive-row-' + archiveID(1))),
            )
            .onTap!;
        old();
        await archivePump(t);
        expect(find.byKey(const Key('legacy-archive-body')), findsOneWidget);
        if (axis == 'owner') f.owner = archiveOtherOwner;
        if (axis == 'workspace') f.workspace = archiveID(700);
        if (axis == 'source') f.logicalSource++;
        f.notify();
        f.owner = archiveOwner;
        f.workspace = null;
        f.logicalSource = 1;
        f.notify();
        await archivePump(t);
        expect(find.byKey(const Key('legacy-archive-body')), findsNothing);
        expect(find.textContaining('查看来源已变化'), findsOneWidget);
        final count = f.calls.length;
        old();
        await archivePump(t);
        expect(f.calls.length, count);
        await archiveClose(t, f);
      },
    );
  }
  testWidgets(
    'archive late detail cannot leak after owner changed even owner returns',
    (t) async {
      final f = ArchivePageFixture();
      final pending = Completer<http.Response>();
      f.reader = (r) async => r.url.path.endsWith('/migration-archive')
          ? archiveResponse({
              'items': [archiveSummary(1)],
            })
          : pending.future;
      await archiveMount(t, f);
      await t.tap(find.byKey(ValueKey('legacy-archive-row-' + archiveID(1))));
      await t.pump();
      f.owner = archiveOtherOwner;
      f.notify();
      f.owner = archiveOwner;
      f.notify();
      pending.complete(
        archiveResponse(archiveDetail(1, body: 'LATE_BODY_MUST_RETIRE')),
      );
      await archivePump(t);
      expect(find.text('LATE_BODY_MUST_RETIRE'), findsNothing);
      expect(find.byKey(const Key('legacy-archive-body')), findsNothing);
      await archiveClose(t, f);
    },
  );
  for (final axis in ['origin', 'transport', 'listener']) {
    testWidgets('archive same widget $axis provider ABA remains retired', (
      t,
    ) async {
      final f = ArchivePageFixture();
      await archiveMount(t, f);
      final old = t
          .widget<ListTile>(
            find.byKey(ValueKey('legacy-archive-row-' + archiveID(1))),
          )
          .onTap!;
      final extra = ValueNotifier(0);
      final other = ArchiveBorrowedClient(
        (_) async => archiveResponse({'items': []}),
      );
      final rebound = LegacyArchivePage(
        authorizationHeader: f.page.authorizationHeader,
        ownerID: f.page.ownerID,
        organizationWorkspaceID: f.page.organizationWorkspaceID,
        identityChanges: axis == 'listener' ? extra : f.changes,
        sourceCurrent: f.page.sourceCurrent,
        sourceKey: f.page.sourceKey,
        client: axis == 'transport' ? other : f.client,
        apiBaseUrl: axis == 'origin'
            ? 'https://other-isolated.test'
            : archiveOrigin,
      );
      await t.pumpWidget(f.harness(child: rebound));
      await t.pumpWidget(f.harness());
      await archivePump(t);
      final count = f.calls.length;
      old();
      await archivePump(t);
      expect(f.calls.length, count);
      expect(find.textContaining('查看来源已变化'), findsOneWidget);
      expect(other.wasClosed, false);
      await archiveClose(t, f);
      extra.dispose();
      other.close();
    });
  }
}
