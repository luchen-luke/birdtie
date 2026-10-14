import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';
import 'dart:ui' as ui;
import 'package:birdtie_client/src/content/collection_copy_controller.dart';
import 'package:birdtie_client/src/content/collection_copy_page.dart';
import 'package:birdtie_client/src/content/collection_copy_pending_store.dart';
import 'package:birdtie_client/src/content/collection_publication_controller.dart';
import 'package:birdtie_client/src/content/collection_publication_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'batch_a_visual_test.dart' show visualTheme;
import 'collection_copy_controller_test.dart';
import 'personal_collection_controller_test.dart'
    show collectionID, collectionPlaceID, collectionOwner, collectionResponse;

const copyCaptureKey = Key('C3-isolated-copy-render');
Future<void> copySettle(WidgetTester t) async {
  for (var i = 0; i < 3; i++) {
    await t.pump();
    await t.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 50)),
    );
  }
  await t.pumpAndSettle();
}

void copyViewport(WidgetTester t) {
  t.view.physicalSize = const Size(390, 844);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
}

Future<void> copyLoadFonts() async {
  if (Platform.environment['BIRDTIE_C3_CAPTURE'] == null) return;
  for (final font in [
    ('BatchACJK', r'C:\Windows\Fonts\msyh.ttc'),
    (
      'MaterialIcons',
      r'D:\DevTools\flutter\bin\cache\artifacts\material_fonts\MaterialIcons-Regular.otf',
    ),
  ]) {
    await (FontLoader(font.$1)..addFont(
          Future.value(ByteData.sublistView(await File(font.$2).readAsBytes())),
        ))
        .load();
  }
}

Widget copyApp(
  Widget child, {
  ThemeData? theme,
  GlobalKey<NavigatorState>? nav,
}) => MaterialApp(
  navigatorKey: nav,
  debugShowCheckedModeBanner: false,
  theme: theme ?? visualTheme(),
  builder: (context, child) => RepaintBoundary(
    key: copyCaptureKey,
    child: Column(
      children: [
        Expanded(child: child!),
        Material(
          color: Theme.of(context).colorScheme.surfaceContainerHigh,
          child: const Padding(
            padding: EdgeInsets.all(6),
            child: Text(
              '隔离测试渲染 · 合成旅程 · 非真实复制授权',
              style: TextStyle(fontFamily: 'BatchACJK', fontSize: 11),
            ),
          ),
        ),
      ],
    ),
  ),
  home: child,
);
Future<void> copyCapture(WidgetTester t, String name) async {
  final path = Platform.environment['BIRDTIE_C3_CAPTURE'];
  if (path == null) return;
  await t.pumpAndSettle();
  await t.runAsync(() async {
    final im = await t
        .renderObject<RenderRepaintBoundary>(find.byKey(copyCaptureKey))
        .toImage(pixelRatio: 1);
    final bytes = await im.toByteData(format: ui.ImageByteFormat.png);
    final f = File('$path/$name.png');
    await f.parent.create(recursive: true);
    if (await f.exists())
      throw StateError('Immutable copy capture already exists');
    await f.writeAsBytes(bytes!.buffer.asUint8List());
    im.dispose();
  });
}

Future<void> openCopyPreview(WidgetTester t) async {
  await t.pump();
  final button = find.byKey(const Key('collection-copy-preview'));
  await t.ensureVisible(button);
  await t.pump();
  await t.tap(button);
  await copySettle(t);
  expect(find.byKey(const Key('collection-copy-confirm')), findsOneWidget);
}

void main() {
  setUpAll(copyLoadFonts);
  for (final allowed in [false, true]) {
    testWidgets(
      'C3 public original share $allowed exposes only current grant CTA normal390',
      (t) async {
        copyViewport(t);
        var copies = 0, writes = 0;
        final client = MockClient((r) async {
          if (r.method != 'GET') writes++;
          return collectionResponse(copyPublicWire(allowed: allowed));
        });
        final c = PublicCollectionController(
          reference: const CollectionShareReference('journey', collectionID),
          authorizationHeader: () => 'Bearer receiver',
          client: client,
          apiBaseUrl: copyBase,
        );
        await t.pumpWidget(
          copyApp(
            PublicCollectionPage(
              controller: c,
              cityNameFor: (_) => '合成城市',
              onCopyPlaces: (v) async {
                expect(v.copyGrant!.sourceRef.same(copyRef), isTrue);
                copies++;
              },
            ),
          ),
        );
        await copySettle(t);
        final button = find.byKey(const Key('public-collection-copy-places'));
        if (allowed) {
          await t.ensureVisible(button);
          await t.tap(button);
          expect(copies, 1);
        } else {
          expect(button, findsNothing);
          expect(find.textContaining('作者未允许复制'), findsOneWidget);
          await copyCapture(t, 'public-no-copy-grant');
        }
        expect(writes, 0);
        await t.pumpWidget(const SizedBox());
        c.dispose();
        client.close();
      },
    );
  }
  testWidgets(
    'C3 explicit own title and selected places confirm one COPY then actual private target normal390',
    (t) async {
      copyViewport(t);
      final calls = <http.Request>[],
          store = MemoryCollectionCopyPendingStore(),
          g = copyGrant();
      var opened = 0;
      final client = MockClient((r) async {
        calls.add(r);
        if (r.url.path.endsWith('/preview'))
          return collectionResponse(
            copyPreviewWire(copyInput(jsonDecode(r.body))),
          );
        if (r.method == 'PUT')
          return collectionResponse(
            copyReceiptWire(copyInput(jsonDecode(r.body)).selector),
          );
        return collectionResponse(copyPrivateWire(ids: [collectionPlaceID]));
      });
      final c = copyController(client, store, grant: () => g);
      await t.pumpWidget(
        copyApp(
          CollectionCopyPage(
            controller: c,
            placeNameFor: (id) => id == collectionPlaceID ? '合成海边地点' : '合成阅读地点',
            onOpenPrivateCopy: (v) async {
              opened++;
              expect(v.id, copyDestination);
              expect(v.privatePlaceCopyRestricted, isTrue);
            },
          ),
        ),
      );
      await copySettle(t);
      expect(
        t
            .widget<TextField>(find.byKey(const Key('collection-copy-title')))
            .controller!
            .text,
        isEmpty,
      );
      await t.enterText(
        find.byKey(const Key('collection-copy-title')),
        '本人的周末副本',
      );
      await t.pump();
      await t.tap(find.byKey(const Key('collection-copy-place-$copySecond')));
      await t.pump();
      await copyCapture(t, 'copy-own-selection-normal');
      await openCopyPreview(t);
      expect(calls.where((r) => r.method == 'PUT'), isEmpty);
      expect(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.text('本人的周末副本'),
        ),
        findsOneWidget,
      );
      expect(find.textContaining('1 个公开地点'), findsOneWidget);
      await copyCapture(t, 'copy-explicit-confirm');
      await t.tap(find.byKey(const Key('collection-copy-confirm')));
      await copySettle(t);
      final put = calls.singleWhere((r) => r.method == 'PUT'),
          body = jsonDecode(put.body) as Map<String, dynamic>;
      expect(body['orderedPlaceIds'], [collectionPlaceID]);
      expect(body['title'], '本人的周末副本');
      for (final canary in [
        'AUTHOR_TITLE',
        'AUTHOR_BODY',
        'AUTHOR_NOTE',
        'latitude',
        'longitude',
        'privatePlaceCopyRestricted',
      ])
        expect(put.body.contains(canary), isFalse);
      expect(put.url.path, '/v1/public-collections/journey/$collectionID/copy');
      expect(
        calls.where((r) => r.url.path == '/v1/me/personal-collections'),
        isEmpty,
      );
      await t.tap(find.byKey(const Key('collection-copy-open-destination')));
      await t.pump();
      expect(opened, 1);
      expect(c.pending, isNull);
      expect(store.values, isEmpty);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      c.dispose();
      client.close();
    },
  );
  testWidgets(
    'C3 cancelled preview retains only own draft and never dispatches COPY',
    (t) async {
      copyViewport(t);
      var puts = 0;
      final g = copyGrant(), store = MemoryCollectionCopyPendingStore();
      final client = MockClient((r) async {
        if (r.method == 'PUT') puts++;
        return collectionResponse(
          copyPreviewWire(copyInput(jsonDecode(r.body))),
        );
      });
      final c = copyController(client, store, grant: () => g);
      await t.pumpWidget(copyApp(CollectionCopyPage(controller: c)));
      await copySettle(t);
      await t.enterText(
        find.byKey(const Key('collection-copy-title')),
        '本人取消后保留',
      );
      await openCopyPreview(t);
      await t.tap(find.widgetWithText(TextButton, '取消'));
      await t.pumpAndSettle();
      expect(c.title, '本人取消后保留');
      expect(puts, 0);
      expect(store.values, isEmpty);
      await t.pumpWidget(const SizedBox());
      c.dispose();
      client.close();
    },
  );
  testWidgets(
    'C3 expiry retires borrowed preview and captured confirm callback performs no PUT',
    (t) async {
      copyViewport(t);
      var puts = 0;
      final g = copyGrant();
      final client = MockClient((r) async {
        if (r.method == 'PUT') puts++;
        return collectionResponse(
          copyPreviewWire(copyInput(jsonDecode(r.body)), lease: 1),
        );
      });
      final c = copyController(
        client,
        MemoryCollectionCopyPendingStore(),
        grant: () => g,
      );
      await t.pumpWidget(
        copyApp(
          CollectionCopyPage(
            controller: c,
            placeNameFor: (_) => 'BORROWED_LABEL_MUST_RETIRE',
          ),
        ),
      );
      await copySettle(t);
      await t.enterText(
        find.byKey(const Key('collection-copy-title')),
        'PRIVATE_TITLE_MUST_RETIRE',
      );
      await openCopyPreview(t);
      final old = t
          .widget<FilledButton>(
            find.byKey(const Key('collection-copy-confirm')),
          )
          .onPressed!;
      await t.pump(const Duration(seconds: 2));
      await t.pumpAndSettle();
      expect(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.text('PRIVATE_TITLE_MUST_RETIRE'),
        ),
        findsNothing,
      );
      expect(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.textContaining('BORROWED_LABEL_MUST_RETIRE'),
        ),
        findsNothing,
      );
      expect(
        t
            .widget<FilledButton>(
              find.byKey(const Key('collection-copy-confirm')),
            )
            .onPressed,
        isNull,
      );
      old();
      await copySettle(t);
      expect(puts, 0);
      expect(c.currentGrant, isNotNull);
      expect(find.text('PRIVATE_TITLE_MUST_RETIRE'), findsOneWidget);
      expect(find.text('这次确认已失效，确认中的原资料已隐藏。关闭后重新检查，不会执行旧批准。'), findsOneWidget);
      await copyCapture(t, 'copy-confirm-expired-b5-wording');
      await t.tap(find.widgetWithText(TextButton, '取消'));
      await t.pumpAndSettle();
      await t.pumpWidget(const SizedBox());
      c.dispose();
      client.close();
    },
  );
  testWidgets(
    'C3 grant withdrawal retires borrowed labels but original UNKNOWN remains recoverable',
    (t) async {
      copyViewport(t);
      final changes = ChangeNotifier(),
          store = MemoryCollectionCopyPendingStore();
      CollectionCopyGrant? grant = copyGrant();
      var puts = 0, reads = 0;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview'))
          return collectionResponse(
            copyPreviewWire(copyInput(jsonDecode(r.body))),
          );
        if (r.method == 'PUT') {
          puts++;
          return collectionResponse(null, code: 503);
        }
        if (r.url.path.endsWith('/receipt')) {
          reads++;
          return collectionResponse(null, code: 404);
        }
        throw StateError('unexpected source mutation');
      });
      final c = copyController(
        client,
        store,
        grant: () => grant,
        changes: changes,
      );
      await t.pumpWidget(
        copyApp(
          CollectionCopyPage(controller: c, placeNameFor: (_) => '旧公开地点'),
        ),
      );
      await copySettle(t);
      await t.enterText(
        find.byKey(const Key('collection-copy-title')),
        '只保留本人的草稿',
      );
      await openCopyPreview(t);
      await t.tap(find.byKey(const Key('collection-copy-confirm')));
      await copySettle(t);
      grant = null;
      changes.notifyListeners();
      await t.pumpAndSettle();
      expect(find.textContaining('旧公开地点'), findsNothing);
      expect(c.pending, isNotNull);
      await copyCapture(t, 'copy-unknown-original-operation');
      await t.tap(find.byKey(const Key('collection-copy-recover')));
      await copySettle(t);
      expect(reads, 1);
      expect(puts, 1);
      expect(c.pending, isNotNull);
      expect(c.canPreview, isFalse);
      await t.pumpWidget(const SizedBox());
      c.dispose();
      changes.dispose();
      client.close();
    },
  );
  testWidgets(
    'C3 actor ABA captured confirmation cannot dispatch or revive old private body',
    (t) async {
      copyViewport(t);
      final changes = ChangeNotifier(), g = copyGrant();
      var owner = collectionOwner, puts = 0;
      final client = MockClient((r) async {
        if (r.method == 'PUT') puts++;
        return collectionResponse(
          copyPreviewWire(copyInput(jsonDecode(r.body))),
        );
      });
      final c = copyController(
        client,
        MemoryCollectionCopyPendingStore(),
        grant: () => g,
        owner: () => owner,
        changes: changes,
      );
      await t.pumpWidget(
        copyApp(
          CollectionCopyPage(
            controller: c,
            placeNameFor: (_) => 'ACTOR_OLD_SOURCE',
          ),
        ),
      );
      await copySettle(t);
      await t.enterText(
        find.byKey(const Key('collection-copy-title')),
        'ACTOR_OLD_TITLE',
      );
      await openCopyPreview(t);
      final old = t
          .widget<FilledButton>(
            find.byKey(const Key('collection-copy-confirm')),
          )
          .onPressed!;
      owner = copySecond;
      changes.notifyListeners();
      owner = collectionOwner;
      changes.notifyListeners();
      await t.pumpAndSettle();
      old();
      await copySettle(t);
      expect(puts, 0);
      expect(c.current, isFalse);
      expect(find.textContaining('ACTOR_OLD_SOURCE'), findsNothing);
      expect(find.text('ACTOR_OLD_TITLE'), findsNothing);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      c.dispose();
      changes.dispose();
      client.close();
    },
  );
  testWidgets(
    'C3 STOP requires current explicit confirmation and cancelled old callback cannot stop',
    (t) async {
      copyViewport(t);
      final store = MemoryCollectionCopyPendingStore();
      final input = CollectionCopyInput(
        sourceRef: copyRef,
        operationID: copyOp,
        publicationRevision: 2,
        grantVersion: 1,
        orderedPlaceIDs: [collectionPlaceID],
        title: '不会落盘的本人标题',
      );
      await store.write(
        copyBase,
        collectionOwner,
        CollectionCopyPending(input.selector),
      );
      var stops = 0;
      final client = MockClient((r) async {
        expectSync(r.method, 'POST');
        expectSync(r.url.path.endsWith('/$copyOp/stop'), isTrue);
        expectSync(r.body, isEmpty);
        stops++;
        return collectionResponse(
          copyReceiptWire(input.selector, stopped: true),
        );
      });
      final c = copyController(client, store);
      await t.pumpWidget(copyApp(CollectionCopyPage(controller: c)));
      await copySettle(t);
      await t.tap(find.byKey(const Key('collection-copy-stop')));
      await t.pumpAndSettle();
      final old = t
          .widget<FilledButton>(
            find.byKey(const Key('collection-copy-confirm-stop')),
          )
          .onPressed!;
      await t.tap(find.widgetWithText(TextButton, '保留待核实状态'));
      await t.pump();
      old();
      await t.pumpAndSettle();
      expect(stops, 0);
      expect(c.pending, isNotNull);
      await t.tap(find.byKey(const Key('collection-copy-stop')));
      await t.pumpAndSettle();
      await t.tap(find.byKey(const Key('collection-copy-confirm-stop')));
      await copySettle(t);
      expect(stops, 1);
      expect(c.receipt!.status, 'STOPPED');
      expect(store.values, isEmpty);
      expect(c.canPreview, isFalse);
      await t.pumpWidget(const SizedBox());
      c.dispose();
      client.close();
    },
  );
  testWidgets(
    'C3 own dialog reverse transition survives controller target ABA and Theme rebuild',
    (t) async {
      copyViewport(t);
      final nav = GlobalKey<NavigatorState>();
      var puts = 0;
      final g = copyGrant();
      final client = MockClient((r) async {
        if (r.method == 'PUT') puts++;
        return collectionResponse(
          copyPreviewWire(copyInput(jsonDecode(r.body))),
        );
      });
      final old = copyController(
            client,
            MemoryCollectionCopyPendingStore(),
            grant: () => g,
          ),
          next = copyController(
            client,
            MemoryCollectionCopyPendingStore(),
            grant: () => g,
          );
      Widget app(CollectionCopyController c, ThemeData theme) => copyApp(
        CollectionCopyPage(controller: c),
        theme: theme,
        nav: nav,
      );
      await t.pumpWidget(app(old, visualTheme()));
      await copySettle(t);
      await t.enterText(
        find.byKey(const Key('collection-copy-title')),
        '逆动画本人草稿',
      );
      await openCopyPreview(t);
      nav.currentState!.pop(false);
      await t.pump();
      // Navigator.push has completed while the old DialogRoute is still mounted.
      await t.pumpWidget(app(next, ThemeData.dark()));
      await t.pump(const Duration(milliseconds: 30));
      await t.pumpWidget(app(old, visualTheme()));
      await t.pumpAndSettle();
      expect(t.takeException(), isNull);
      expect(puts, 0);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      old.dispose();
      next.dispose();
      client.close();
    },
  );
}
