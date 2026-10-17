import 'dart:convert';
import 'dart:ui' show SemanticsAction;
import 'package:birdtie_client/src/content/collection_control.dart';
import 'package:birdtie_client/src/content/collection_control_pending_store.dart';
import 'package:birdtie_client/src/content/collection_create_pending_store.dart';
import 'collection_control_test.dart';
import 'package:birdtie_client/src/content/personal_collection_page.dart';
import 'package:birdtie_client/src/content/collection_copy_pending_store.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/testing.dart';

import 'personal_collection_controller_test.dart';
import 'collection_copy_controller_test.dart'
    show
        copyBase,
        copyRef,
        copyOp,
        copyDestination,
        copyInput,
        copyPreviewWire,
        copyReceiptWire,
        copyPrivateWire,
        copyPublicWire;
import 'collection_copy_page_test.dart'
    show
        copyViewport,
        copyApp,
        copySettle,
        copyCapture,
        openCopyPreview,
        copyLoadFonts;
import 'collection_copy_pending_store_test.dart' show copyStored;

void main() {
  testWidgets(
    'create recovery normal390 cold secure page offers semantic original GET only then shows verified record',
    (t) async {
      copyViewport(t);
      final semantics = t.ensureSemantics();
      try {
        var posts = 0;
        final reads = <String>[];
        final client = MockClient((r) async {
          if (r.method == 'POST') {
            posts++;
            return collectionResponse(null, code: 503);
          }
          reads.add(r.url.path);
          return collectionResponse(
            r.url.path.contains('/operations/')
                ? collectionReceipt()
                : {'items': <Object>[]},
          );
        });
        final old = collectionController(
          client,
          createStore: const SecureCollectionCreatePendingStore(),
        );
        seedCollectionDraft(old);
        expect(await old.save(), false);
        old.dispose();
        final next = collectionController(
          client,
          createStore: const SecureCollectionCreatePendingStore(),
        );
        await t.pumpWidget(
          copyApp(
            PersonalCollectionPage(
              controller: next,
              cityName: '合成城市',
              copyPendingStore: MemoryCollectionCopyPendingStore(),
            ),
          ),
        );
        await copySettle(t);
        expect(find.textContaining('上次新建保存结果待核实'), findsOneWidget);
        expect(find.text('按原稿重试'), findsNothing);
        final recover = find.byKey(
          ValueKey(
            'collection-create-recover-${next.createPending!.operationID}',
          ),
        );
        expect(
          t
              .getSemantics(recover)
              .getSemanticsData()
              .hasAction(SemanticsAction.tap),
          true,
        );
        await t.tap(recover);
        await copySettle(t);
        expect(next.editing!.id, collectionID);
        expect(find.textContaining('上次新建保存结果待核实'), findsNothing);
        expect(posts, 1);
        expect(reads.where((p) => p.contains('/operations/')), hasLength(1));
        expect(await const FlutterSecureStorage().readAll(), isEmpty);
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        next.dispose();
        client.close();
      } finally {
        semantics.dispose();
      }
    },
  );
  collection147PageCases();
  setUpAll(copyLoadFonts);
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  testWidgets(
    'C3 withdrawn private copy saves own title and note with exact revision, metadata stays retired normal390',
    (t) async {
      copyViewport(t);
      final puts = <Map<String, dynamic>>[];
      Map<String, dynamic> record(
        int revision, {
        String title = '合成本人旧标题',
        String note = '',
      }) => {
        ...copyPrivateWire(
          revision: revision,
          title: title,
          ids: [collectionPlaceID],
        ),
        'entries': [
          {
            'placeId': collectionPlaceID,
            'note': note,
            'locationVisibility': 'hidden',
            'available': false,
          },
        ],
      };
      var actual = record(1);
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview'))
          return ccResponse(ccPreview(ccRequest(r)));
        if (r.method == 'PUT') {
          expectSync(
            r.url.path.startsWith(
              '/v1/me/personal-collection-controls/operations/',
            ),
            true,
          );
          final input = ccRequest(r), b = input.input!.toJson();
          expectSync(input.collectionID, copyDestination);
          expectSync(input.expectedRevision, 1);
          puts.add(b);
          actual = record(
            2,
            title: input.input!.title,
            note: input.input!.entries.single.note,
          );
          return ccResponse(ccReceipt(input));
        }
        return collectionResponse(
          r.url.path.endsWith(copyDestination)
              ? actual
              : {
                  'items': [actual],
                },
        );
      });
      final c = collectionController(client, apiBaseUrl: copyBase);
      expect(await c.openID(copyDestination), true, reason: c.error);
      await t.pumpWidget(
        copyApp(
          PersonalCollectionPage(
            controller: c,
            cityName: '合成城市',
            copyPendingStore: MemoryCollectionCopyPendingStore(),
          ),
        ),
      );
      await copySettle(t);
      await t.enterText(find.byKey(const Key('collection-title')), '合成本人新标题');
      c.editEntry(0, '本人私人备注');
      await t.pump();
      final save = find.byKey(const Key('collection-save'));
      await t.ensureVisible(save);
      await t.pump();
      await t.tap(save);
      await copySettle(t);
      expect(puts, isEmpty);
      expect(find.text('检查修改'), findsOneWidget);
      await t.tap(find.byKey(const Key('collection-control-confirm')));
      await collection147CommitReady(t, c);
      expect(puts, hasLength(1));
      expect(c.lastControlReceipt!.expectedRevision, 1);
      expect(puts.single['title'], '合成本人新标题');
      expect(puts.single['entries'], [
        {
          'placeId': collectionPlaceID,
          'note': '本人私人备注',
          'locationVisibility': 'hidden',
        },
      ]);
      expect(c.editing!.revision, 2);
      expect(c.editing!.entries.single.available, isFalse);
      expect(c.editing!.entries.single.latitude, isNull);
      expect(find.text('地点来源已变化，请重新读取'), findsOneWidget);
      expect(
        t
            .widget<OutlinedButton>(
              find.byKey(const Key('collection-publication')),
            )
            .onPressed,
        isNull,
      );
      expect(puts.single.containsKey('privatePlaceCopyRestricted'), isFalse);
      await t.pumpWidget(const SizedBox());
      c.dispose();
      client.close();
    },
  );
  testWidgets(
    'C3 cold private borrowed draft keeps edit but disables public preview normal390',
    (t) async {
      copyViewport(t);
      var writes = 0;
      final raw = copyPrivateWire(),
          client = MockClient((r) async {
            if (r.method != 'GET') writes++;
            return collectionResponse(
              r.url.path.endsWith(copyDestination)
                  ? raw
                  : {
                      'items': [raw],
                    },
            );
          });
      final c = collectionController(client, apiBaseUrl: copyBase);
      expect(await c.openID(copyDestination), true, reason: c.error);
      await t.pumpWidget(
        copyApp(
          PersonalCollectionPage(
            controller: c,
            cityName: '合成城市',
            copyPendingStore: MemoryCollectionCopyPendingStore(),
          ),
        ),
      );
      await copySettle(t);
      final publish = find.byKey(const Key('collection-publication'));
      await t.scrollUntilVisible(
        publish,
        180,
        scrollable: find.byType(Scrollable).first,
      );
      expect(t.widget<OutlinedButton>(publish).onPressed, isNull);
      expect(
        find.byKey(const Key('collection-private-copy-restriction')),
        findsOneWidget,
      );
      expect(
        t
            .widget<FilledButton>(find.byKey(const Key('collection-save')))
            .onPressed,
        isNotNull,
      );
      await t.tap(publish);
      await t.pump();
      expect(writes, 0);
      await copyCapture(t, 'copy-private-draft-publication-blocked');
      await t.pumpWidget(const SizedBox());
      c.dispose();
      client.close();
    },
  );
  testWidgets(
    'C3 original private collection entry restores lost public source operation without new grant',
    (t) async {
      copyViewport(t);
      final store = MemoryCollectionCopyPendingStore();
      await store.write(copyBase, collectionOwner, copyStored);
      final calls = <String>[];
      final client = MockClient((r) async {
        calls.add('${r.method} ${r.url.path}');
        if (r.url.path.contains('/copy-operations/'))
          return collectionResponse(null, code: 404);
        if (r.url.path.startsWith('/v1/public-collections/'))
          throw StateError('recovery must not need public source');
        return collectionResponse({'items': []});
      });
      final c = collectionController(client, apiBaseUrl: copyBase);
      await t.pumpWidget(
        copyApp(
          PersonalCollectionPage(
            controller: c,
            cityName: '合成城市',
            copyPendingStore: store,
          ),
        ),
      );
      await copySettle(t);
      await t.tap(find.byKey(const Key('collection-copy-open-recovery')));
      await copySettle(t);
      expect(find.byKey(const Key('collection-copy-preview')), findsNothing);
      await t.tap(find.byKey(const Key('collection-copy-recover')));
      await copySettle(t);
      expect(
        calls.where(
          (s) => s.startsWith(
            'GET /v1/me/personal-collections/copy-operations/$copyOp/receipt',
          ),
        ),
        hasLength(1),
      );
      expect(
        calls.where((s) => s.startsWith('PUT') || s.startsWith('POST')),
        isEmpty,
      );
      expect(store.values.values.single.same(copyStored), isTrue);
      expect(find.byKey(const Key('collection-copy-stop')), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      c.dispose();
      client.close();
    },
  );
  testWidgets(
    'C3 original share route copies actual selected places into new own editor without ordinary create',
    (t) async {
      copyViewport(t);
      final store = MemoryCollectionCopyPendingStore(), puts = <String>[];
      var savedTitle = '';
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/copy/preview'))
          return collectionResponse(
            copyPreviewWire(copyInput(jsonDecode(r.body))),
          );
        if (r.method == 'PUT') {
          puts.add(r.url.path);
          final input = copyInput(jsonDecode(r.body));
          savedTitle = input.title;
          return collectionResponse(copyReceiptWire(input.selector));
        }
        if (r.url.path.startsWith('/v1/public-collections/'))
          return collectionResponse(copyPublicWire());
        if (r.url.path.endsWith(copyDestination))
          return collectionResponse(copyPrivateWire(title: savedTitle));
        if (r.method != 'GET')
          throw StateError('COPY must not use ordinary create');
        return collectionResponse({'items': []});
      });
      final c = collectionController(client, apiBaseUrl: copyBase);
      await t.pumpWidget(
        copyApp(
          PersonalCollectionPage(
            controller: c,
            cityName: '合成城市',
            copyPendingStore: store,
          ),
        ),
      );
      await copySettle(t);
      await t.tap(find.byTooltip('打开分享'));
      await t.pumpAndSettle();
      await t.enterText(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.byType(TextField),
        ),
        copyRef.code,
      );
      await t.tap(find.widgetWithText(FilledButton, '读取公开内容'));
      await copySettle(t);
      final copy = find.byKey(const Key('public-collection-copy-places'));
      await t.ensureVisible(copy);
      await t.tap(copy);
      await copySettle(t);
      await t.enterText(
        find.byKey(const Key('collection-copy-title')),
        '属于本人的新副本',
      );
      await openCopyPreview(t);
      await t.tap(find.byKey(const Key('collection-copy-confirm')));
      await copySettle(t);
      await t.tap(find.byKey(const Key('collection-copy-open-destination')));
      await copySettle(t);
      expect(puts, ['/v1/public-collections/journey/$collectionID/copy']);
      expect(
        t
            .widget<TextField>(find.byKey(const Key('collection-title')))
            .controller!
            .text,
        '属于本人的新副本',
      );
      final publish = find.byKey(const Key('collection-publication'));
      await t.scrollUntilVisible(
        publish,
        180,
        scrollable: find.byType(Scrollable).first,
      );
      expect(t.widget<OutlinedButton>(publish).onPressed, isNull);
      expect(store.values, isEmpty);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      c.dispose();
      client.close();
    },
  );
  for (final kind in ['journey', 'theme_map']) {
    testWidgets('$kind小屏大字真实保存/列表/读取可达，无额外地图', (t) async {
      t.view.physicalSize = const Size(320, 720);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final writes = <String>[];
      final client = MockClient((r) async {
        if (r.method == 'POST') {
          writes.add(r.body);
          return collectionResponse(collectionReceipt(kind: kind), code: 201);
        }
        if (r.url.path.endsWith(collectionID))
          return collectionResponse(collectionReceipt(kind: kind));
        return collectionResponse({
          'items': [collectionReceipt(kind: kind)],
        });
      });
      final c = collectionController(client, kind: kind);
      try {
        seedCollectionDraft(c);
        await t.pumpWidget(
          MaterialApp(
            builder: (context, child) => MediaQuery(
              data: MediaQuery.of(
                context,
              ).copyWith(textScaler: const TextScaler.linear(2)),
              child: child!,
            ),
            home: PersonalCollectionPage(controller: c, cityName: 'Aberdeen'),
          ),
        );
        await t.pumpAndSettle();
        expect(find.byKey(const Key('collection-title')), findsOneWidget);
        expect(find.text('Aberdeen · 仅自己可见'), findsOneWidget);
        final save = find.byKey(const Key('collection-save'));
        await t.scrollUntilVisible(
          save,
          180,
          scrollable: find.byType(Scrollable).first,
        );
        await t.tap(save);
        await t.pumpAndSettle();
        expect(writes.length, 1);
        expect(c.editing!.id, collectionID);
        expect(t.takeException(), isNull);
        await t.tap(find.byTooltip('已保存${c.label}'));
        await t.pumpAndSettle();
        expect(find.text('周末路线'), findsOneWidget);
        await t.tap(find.text('周末路线'));
        await t.pumpAndSettle();
        expect(c.editing!.id, collectionID);
        expect(t.takeException(), isNull);
      } finally {
        // The caller owns this persistent draft controller. Dispose it inside
        // the widget test body, before binding verifies pending source timers.
        await t.pumpWidget(const SizedBox());
        c.dispose();
        client.close();
      }
    }, tags: const ['historical-enlarged-font']);
  }
  testWidgets('保存失败保留草稿，未知结果只核实原保存而不重发', (t) async {
    final client = MockClient(
      (r) async => r.method == 'POST'
          ? collectionResponse(null, code: 503)
          : collectionResponse({'items': []}),
    );
    final c = collectionController(client);
    try {
      seedCollectionDraft(c);
      await t.pumpWidget(
        MaterialApp(
          home: PersonalCollectionPage(controller: c, cityName: 'Aberdeen'),
        ),
      );
      await t.pumpAndSettle();
      final save = find.byKey(const Key('collection-save'));
      await t.scrollUntilVisible(
        save,
        180,
        scrollable: find.byType(Scrollable).first,
      );
      await t.tap(save);
      await t.pumpAndSettle();
      expect(c.title, '周末路线');
      await t.scrollUntilVisible(
        find.text('核实原保存'),
        -180,
        scrollable: find.byType(Scrollable).first,
      );
      expect(find.text('核实原保存'), findsOneWidget);
      expect(find.text('按原稿重试'), findsNothing);
      expect(t.takeException(), isNull);
    } finally {
      await t.pumpWidget(const SizedBox());
      c.dispose();
      client.close();
    }
  });
}

Future<void> collection147CommitReady(WidgetTester t, dynamic c) async {
  // Complete the actual reverse route first. Its completed Future starts the
  // journal/HTTP chain; then let real streamed I/O and the current read run.
  for (var i = 0; i < 3; i++) {
    await t.pump(const Duration(milliseconds: 300));
    await t.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 50)),
    );
  }
  await t.pump();
  expect(
    find.byType(AlertDialog),
    findsNothing,
    reason: 'The confirmation route must actually unmount',
  );
  expect(
    c.saving,
    false,
    reason: 'Original submit and current read must finish',
  );
  expect(
    c.controlPreview,
    isNull,
    reason: 'Submitted approval must be retired',
  );
  expect(
    c.lastControlReceipt != null || c.controlPending.isNotEmpty,
    true,
    reason: 'A definitive original receipt or real UNKNOWN selector must exist',
  );
}

void collection147PageCases() {
  testWidgets(
    '147 normal390 UPDATE explicitly confirms original ID revision and sends one PUT',
    (t) async {
      copyViewport(t);
      final calls = <(String, String)>[];
      var revision = 7, title = '周末路线', note = '';
      Map<String, dynamic> current() {
        final raw = collectionReceipt(revision: revision, title: title);
        final entries = raw['entries'] as List;
        (entries.single as Map<String, dynamic>)['note'] = note;
        return raw;
      }

      final client = MockClient((r) async {
        calls.add((r.method, r.url.path));
        if (r.url.path.endsWith('/preview'))
          return ccResponse(ccPreview(ccRequest(r)));
        if (r.method == 'PUT') {
          final input = ccRequest(r);
          expectSync(input.collectionID, collectionID);
          expectSync(input.expectedRevision, 7);
          revision = 8;
          title = input.input!.title;
          note = input.input!.entries.single.note;
          return ccResponse(ccReceipt(input));
        }
        return collectionResponse(
          r.url.path.endsWith(collectionID)
              ? current()
              : {
                  'items': [current()],
                },
        );
      });
      final c = collectionController(client);
      try {
        await c.openID(collectionID);
        await t.pumpWidget(
          copyApp(PersonalCollectionPage(controller: c, cityName: '隔离城市')),
        );
        await copySettle(t);
        await t.enterText(find.byKey(const Key('collection-title')), '本人新标题');
        c.editEntry(0, '本人私人备注');
        await t.pump();
        final save = find.byKey(const Key('collection-save'));
        await t.ensureVisible(save);
        await t.pump();
        await t.tap(save);
        await copySettle(t);
        expect(calls.where((r) => r.$1 == 'PUT'), isEmpty);
        expect(
          t
              .widget<Text>(
                find.byKey(const Key('collection-control-preview-title')),
              )
              .data,
          '本人新标题',
        );
        expect(find.text('地点 1 · 不显示位置 · 本人私人备注'), findsOneWidget);
        await t.tap(find.byKey(const Key('collection-control-confirm')));
        await collection147CommitReady(t, c);
        expect(calls.where((r) => r.$1 == 'PUT'), hasLength(1));
        expect(c.lastControlReceipt!.succeeded, true);
        expect(c.lastControlReceipt!.collectionID, collectionID);
        expect(c.lastControlReceipt!.expectedRevision, 7);
        expect(c.lastControlReceipt!.appliedRevision, 8);
        expect(c.editing!.id, collectionID);
        expect(c.editing!.revision, 8);
        expect(c.editing!.entries.single.note, '本人私人备注');
        expect(c.controlPending, isEmpty);
        expect(calls.any((r) => r.$1 == 'DELETE'), false);
        expect(t.takeException(), isNull);
      } finally {
        await t.pumpWidget(const SizedBox());
        c.dispose();
        client.close();
      }
    },
  );
  testWidgets(
    '147 normal390 UNKNOWN disables new producer and captured callbacks do not replay',
    (t) async {
      copyViewport(t);
      final calls = <(String, String)>[],
          store = MemoryCollectionControlPendingStore();
      final client = MockClient((r) async {
        calls.add((r.method, r.url.path));
        if (r.url.path.endsWith('/preview'))
          return ccResponse(ccPreview(ccRequest(r)));
        if (r.method == 'PUT') return ccResponse(null, status: 409);
        return collectionResponse(
          r.url.path.endsWith(collectionID)
              ? collectionReceipt(revision: 7)
              : {
                  'items': [collectionReceipt(revision: 7)],
                },
        );
      });
      final c = collectionController(client, controlStore: store);
      try {
        await c.openID(collectionID);
        await t.pumpWidget(
          copyApp(PersonalCollectionPage(controller: c, cityName: '隔离城市')),
        );
        await copySettle(t);
        await t.enterText(find.byKey(const Key('collection-title')), '本人未知修改稿');
        final save = find.byKey(const Key('collection-save'));
        await t.ensureVisible(save);
        await t.pump();
        final oldSave = t.widget<FilledButton>(save).onPressed!;
        await t.tap(save);
        await copySettle(t);
        final confirm = find.byKey(const Key('collection-control-confirm'));
        final oldConfirm = t.widget<FilledButton>(confirm).onPressed!;
        await t.tap(confirm);
        await collection147CommitReady(t, c);
        expect(c.uncertain, true);
        final p = c.controlPending.single;
        final recover = find.byKey(
          ValueKey('collection-control-recover-' + p.operationID),
        );
        await t.ensureVisible(recover);
        await t.pump();
        expect(find.text('上次修改结果待核实，不会再次提交。'), findsOneWidget);
        expect(find.text('按原稿重试'), findsNothing);
        expect(find.text('移除已保存内容'), findsNothing);
        await t.ensureVisible(save);
        await t.pump();
        expect(t.widget<FilledButton>(save).onPressed, isNull);
        oldSave();
        oldConfirm();
        await collection147CommitReady(t, c);
        expect(calls.where((r) => r.$1 == 'PUT'), hasLength(1));
        expect(calls.where((r) => r.$2.endsWith('/preview')), hasLength(1));
        expect(find.byType(PersonalCollectionPage), findsOneWidget);
        expect(c.title, '本人未知修改稿');
        expect(store.values.values.single.stage, 'UNKNOWN');
        expect(t.takeException(), isNull);
      } finally {
        await t.pumpWidget(const SizedBox());
        c.dispose();
        client.close();
      }
    },
  );
}
