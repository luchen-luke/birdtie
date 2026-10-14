import 'dart:convert';
import 'dart:async';
import 'package:birdtie_client/src/workspace/entity_cover.dart';
import 'package:birdtie_client/src/workspace/entity_cover_editor.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:crypto/crypto.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'entity_cover_test.dart';
import 'person_avatar_fixture.dart'
    show avatarJPEG, avatarPeer, avatarOperation;

Future<void> coverTap(WidgetTester t, Finder f) async {
  await t.ensureVisible(f);
  await t.pumpAndSettle();
  await t.tap(f.hitTestable());
  await coverSettle(t);
}

Future<void> approveCoverRights(WidgetTester t) async {
  await coverTap(t, find.byKey(const Key('cover-rights-basis')));
  await t.tap(find.text('本人拥有图片权利').last);
  await t.pumpAndSettle();
  await t.enterText(find.byKey(const Key('cover-retention')), '2030-01-02');
  t.testTextInput.hide();
  await t.pump();
  await coverTap(t, find.byKey(const Key('cover-retain')));
  await coverTap(t, find.byKey(const Key('cover-publish')));
}

Widget coverEditorApp(
  http.Client client,
  ValueNotifier<String> identity,
  EntityCoverPendingStore store, {
  EntityCoverRef ref = coverRef,
  ThemeData? theme,
}) => MaterialApp(
  theme: theme,
  home: EntityCoverEditor(
    key: const Key('original-cover-editor'),
    ref: ref,
    name: '隔离合成活动',
    ownerID: avatarPeer,
    api: EntityCoverAPI(
      client: client,
      baseUrl: coverBase,
      authorizationHeader: () => identity.value,
    ),
    identityChanges: identity,
    current: () => true,
    initialImage: avatarJPEG(),
    pendingStore: store,
  ),
);

Future<void> showPaintedCoverApproval(WidgetTester t) async {
  await approveCoverRights(t);
  await coverTap(t, find.byKey(const Key('cover-preview')));
  final image = find.descendant(
    of: find.byType(AlertDialog),
    matching: find.byType(Image),
  );
  expect(image, findsOneWidget);
  await t.runAsync(
    () => precacheImage(
      t.widget<Image>(image).image,
      t.element(image),
    ).timeout(const Duration(seconds: 2)),
  );
  await t.pump();
  await t.pumpAndSettle();
  final raw = t.widget<RawImage>(
    find.descendant(of: image, matching: find.byType(RawImage)),
  );
  expect(raw.image, isNotNull);
  expect(raw.image!.width, 320);
  expect(raw.image!.height, 180);
}

void coverDialogNormalView(WidgetTester t) {
  t.view.physicalSize = const Size(390, 844);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
}

class _FailPreparedRead extends MemoryEntityCoverPendingStore {
  bool fail = false;
  @override
  Future<void> write(
    String o,
    String owner,
    EntityCoverRef ref,
    EntityCoverPending p,
  ) async {
    await super.write(o, owner, ref, p);
    if (p.stage == 'PREPARED') fail = true;
  }

  @override
  Future<EntityCoverPending?> read(
    String o,
    String owner,
    EntityCoverRef ref,
  ) async {
    if (fail) {
      fail = false;
      throw StateError('synthetic readback failure');
    }
    return super.read(o, owner, ref);
  }
}

void main() {
  for (final outcome in ['success403', 'stop404']) {
    testWidgets(
      'cover definitive original receipt survives unavailable current state $outcome',
      (t) async {
        coverDialogNormalView(t);
        final identity = ValueNotifier('Bearer A');
        addTearDown(identity.dispose);
        final store = MemoryEntityCoverPendingStore();
        var writes = 0, reads = 0, receipts = 0, stops = 0, definitive = false;
        String? operation;
        final client = MockClient((r) async {
          if (r.url.path.endsWith('/preview'))
            return coverWire(coverApproval(jsonDecode(r.body)));
          if (r.method == 'PUT') {
            writes++;
            operation = (jsonDecode(r.body) as Map)['operationId'];
            if (outcome == 'stop404') return coverWire(null, status: 500);
            definitive = true;
            return coverWire(coverReceipt(operation!));
          }
          if (r.url.path.endsWith('/stop')) {
            stops++;
            definitive = true;
            expectSync(
              r.url.path,
              '${coverRef.path}/operations/$operation/stop',
            );
            return coverWire(coverReceipt(operation!, status: 'STOPPED'));
          }
          if (r.url.path.contains('/operations/')) {
            receipts++;
            expectSync(r.url.path, '${coverRef.path}/operations/$operation');
            return coverWire(null, status: 404);
          }
          reads++;
          return definitive
              ? coverWire(null, status: outcome == 'success403' ? 403 : 404)
              : coverWire({'version': 0, 'cover': null});
        });
        addTearDown(client.close);
        await t.pumpWidget(coverEditorApp(client, identity, store));
        await coverSettle(t);
        await approveCoverRights(t);
        await t.enterText(
          find.byKey(const Key('cover-attribution')),
          '原权利资料 QA',
        );
        final attribution = t
            .widget<TextField>(find.byKey(const Key('cover-attribution')))
            .controller!;
        final until = t
            .widget<TextField>(find.byKey(const Key('cover-retention')))
            .controller!;
        await coverTap(t, find.byKey(const Key('cover-preview')));
        final image = find.descendant(
          of: find.byType(AlertDialog),
          matching: find.byType(Image),
        );
        await t.runAsync(
          () => precacheImage(
            t.widget<Image>(image).image,
            t.element(image),
          ).timeout(const Duration(seconds: 2)),
        );
        await t.pump();
        await t.pumpAndSettle();
        final raw = t.widget<RawImage>(
          find.descendant(of: image, matching: find.byType(RawImage)),
        );
        expect(raw.image, isNotNull);
        expect(raw.image!.width, 320);
        expect(raw.image!.height, 180);
        await coverTap(t, find.widgetWithText(FilledButton, '确认保存封面'));
        if (outcome == 'stop404') {
          await coverTap(t, find.text('核实上次封面操作'));
          expect(receipts, 1);
          expect(writes, 1);
          expect(
            (await store.read(coverBase, avatarPeer, coverRef))!.operationID,
            operation,
          );
          await coverTap(t, find.text('停止这次提交'));
          await coverTap(t, find.text('确认停止'));
        }
        expect(writes, 1);
        expect(stops, outcome == 'stop404' ? 1 : 0);
        expect(await store.read(coverBase, avatarPeer, coverRef), isNull);
        expect(
          find.textContaining(outcome == 'stop404' ? '这次提交已停止' : '原封面保存已完成'),
          findsOneWidget,
        );
        expect(find.textContaining('当前封面资料暂不可读取'), findsOneWidget);
        expect(find.text('原对象当前资料不可读取'), findsOneWidget);
        expect(find.text('隔离合成活动'), findsNothing);
        expect(find.byType(Image), findsNothing);
        expect(find.byKey(const Key('cover-attribution')), findsNothing);
        expect(attribution.text, isEmpty);
        expect(until.text, isEmpty);
        expect(
          t
              .widget<OutlinedButton>(find.byKey(const Key('cover-select')))
              .onPressed,
          isNull,
        );
        expect(find.text('核实上次封面操作'), findsNothing);
        expect(find.text('检查本机记录'), findsNothing);
        expect(reads, 2);
        await coverTap(t, find.text('重新读取封面'));
        expect(reads, 3);
        expect(writes, 1);
        expect(
          find.textContaining(outcome == 'stop404' ? '这次提交已停止' : '原封面保存已完成'),
          findsOneWidget,
        );
        expect(
          t
              .widget<OutlinedButton>(find.byKey(const Key('cover-select')))
              .onPressed,
          isNull,
        );
        await t.pumpWidget(const SizedBox());
        await t.pumpAndSettle();
        expect(t.takeException(), isNull);
      },
    );
  }
  testWidgets(
    'cover dialog reverse animation survives Theme rebuild and target ABA without disposed notifier',
    (t) async {
      coverDialogNormalView(t);
      final identity = ValueNotifier('Bearer A');
      addTearDown(identity.dispose);
      final store = MemoryEntityCoverPendingStore(),
          pending = Completer<http.Response>();
      var writes = 0;
      Map<String, dynamic>? original;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview'))
          return coverWire(coverApproval(jsonDecode(r.body)));
        if (r.method == 'PUT') {
          writes++;
          original = jsonDecode(r.body);
          return pending.future;
        }
        return coverWire({'version': 0, 'cover': null});
      });
      addTearDown(client.close);
      await t.pumpWidget(coverEditorApp(client, identity, store));
      await coverSettle(t);
      await showPaintedCoverApproval(t);
      await t.tap(find.widgetWithText(FilledButton, '确认保存封面'));
      await t.pump();
      await t.runAsync(() async {
        final end = DateTime.now().add(const Duration(seconds: 2));
        while (writes == 0 && DateTime.now().isBefore(end)) {
          await Future<void>.delayed(const Duration(milliseconds: 20));
        }
      });
      expect(writes, 1);
      // Real I/O drained, but fake animation time has not advanced. The popped
      // confirmation is still mounted while the enclosing Theme rebuilds.
      expect(find.byType(AlertDialog), findsOneWidget);
      await t.pumpWidget(
        coverEditorApp(client, identity, store, theme: ThemeData.dark()),
      );
      expect(t.takeException(), isNull);
      await t.pumpWidget(
        coverEditorApp(
          client,
          identity,
          store,
          theme: ThemeData.light(),
          ref: EntityCoverRef('organization', coverRef.id),
        ),
      );
      await t.pumpWidget(
        coverEditorApp(client, identity, store, theme: ThemeData.dark()),
      );
      expect(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.byType(Image),
        ),
        findsNothing,
      );
      expect(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.text('隔离合成活动'),
        ),
        findsNothing,
      );
      pending.complete(coverWire(coverReceipt(original!['operationId'])));
      await coverSettle(t);
      expect(writes, 1);
      expect(find.text('原封面保存已完成。'), findsNothing);
      expect(
        (await store.read(coverBase, avatarPeer, coverRef))!.operationID,
        original!['operationId'],
      );
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      expect(t.takeException(), isNull);
    },
  );
  testWidgets(
    'cover dialog expiry captured confirmation callback cannot prepare or PUT',
    (t) async {
      coverDialogNormalView(t);
      final identity = ValueNotifier('Bearer A');
      addTearDown(identity.dispose);
      final store = MemoryEntityCoverPendingStore();
      var writes = 0;
      final client = MockClient((r) async {
        if (r.method == 'PUT') {
          writes++;
          return coverWire(null, status: 500);
        }
        if (r.url.path.endsWith('/preview'))
          return coverWire(coverApproval(jsonDecode(r.body)));
        return coverWire({'version': 0, 'cover': null});
      });
      addTearDown(client.close);
      await t.pumpWidget(coverEditorApp(client, identity, store));
      await coverSettle(t);
      await showPaintedCoverApproval(t);
      final confirm = find.widgetWithText(FilledButton, '确认保存封面');
      final old = t.widget<FilledButton>(confirm).onPressed!;
      await t.pump(const Duration(seconds: 26));
      await t.pump();
      expect(t.widget<FilledButton>(confirm).onPressed, isNull);
      expect(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.byType(Image),
        ),
        findsNothing,
      );
      expect(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.text('隔离合成活动'),
        ),
        findsNothing,
      );
      old();
      await coverSettle(t);
      expect(writes, 0);
      expect(store.values, isEmpty);
      await coverTap(t, find.text('返回编辑'));
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      expect(t.takeException(), isNull);
    },
  );
  testWidgets(
    'cover dialog parent removal retires private preview and keeps notifier until dialog unmounts',
    (t) async {
      coverDialogNormalView(t);
      final identity = ValueNotifier('Bearer A');
      addTearDown(identity.dispose);
      final store = MemoryEntityCoverPendingStore();
      var writes = 0;
      final client = MockClient((r) async {
        if (r.method == 'PUT') {
          writes++;
          return coverWire(null, status: 500);
        }
        if (r.url.path.endsWith('/preview'))
          return coverWire(coverApproval(jsonDecode(r.body)));
        return coverWire({'version': 0, 'cover': null});
      });
      addTearDown(client.close);
      await t.pumpWidget(coverEditorApp(client, identity, store));
      await coverSettle(t);
      await showPaintedCoverApproval(t);
      final old = t
          .widget<FilledButton>(find.widgetWithText(FilledButton, '确认保存封面'))
          .onPressed!;
      await t.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: const Scaffold(body: Text('原编辑页面已移除')),
        ),
      );
      await t.pump();
      expect(find.byType(AlertDialog), findsOneWidget);
      expect(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.byType(Image),
        ),
        findsNothing,
      );
      expect(find.text('隔离合成活动'), findsNothing);
      expect(t.takeException(), isNull);
      old();
      await coverSettle(t);
      expect(writes, 0);
      expect(store.values, isEmpty);
      await coverTap(t, find.text('返回编辑'));
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      expect(t.takeException(), isNull);
    },
  );
  test(
    'cover pending has no pixels credentials or rights and refuses foreign operation overwrite',
    () async {
      final store = MemoryEntityCoverPendingStore();
      const p = EntityCoverPending(avatarOperation, 0, 'SAVE');
      await store.write(coverBase, avatarPeer, coverRef, p);
      expect(
        await store.read('https://other.test', avatarPeer, coverRef),
        isNull,
      );
      expect(
        await store.read(
          coverBase,
          '11111111-1111-4111-8111-111111111111',
          coverRef,
        ),
        isNull,
      );
      expect(
        p.toJson().keys,
        unorderedEquals(['operationId', 'expectedVersion', 'action', 'stage']),
      );
      await expectLater(
        store.write(
          coverBase,
          avatarPeer,
          coverRef,
          const EntityCoverPending(
            '44444444-4444-4444-8444-444444444444',
            0,
            'SAVE',
          ),
        ),
        throwsFormatException,
      );
      await expectLater(
        store.remove(
          coverBase,
          avatarPeer,
          coverRef,
          const EntityCoverPending(
            avatarOperation,
            0,
            'SAVE',
            stage: 'PREPARED',
          ),
        ),
        throwsFormatException,
      );
      expect(
        (await store.read(coverBase, avatarPeer, coverRef))!.same(p),
        isTrue,
      );
    },
  );
  for (final mode in [
    'cancel',
    'success',
    'actorABA',
    'targetABA',
    'unknownStop',
    'unknownRestart',
    'lateCommitABA',
    'preparedReadFailure',
  ]) {
    testWidgets(
      'cover editor exact normalized preview rights journal and original operation $mode',
      (t) async {
        t.view.physicalSize = const Size(390, 844);
        t.view.devicePixelRatio = 1;
        addTearDown(t.view.resetPhysicalSize);
        addTearDown(t.view.resetDevicePixelRatio);
        final identity = ValueNotifier('Bearer A');
        addTearDown(identity.dispose);
        final EntityCoverPendingStore store = mode == 'preparedReadFailure'
            ? _FailPreparedRead()
            : MemoryEntityCoverPendingStore();
        var writes = 0, previews = 0, receipts = 0, stops = 0;
        String? operation;
        Map<String, dynamic>? original;
        var committed = false, stopped = false;
        final lateCommit = Completer<http.Response>();
        final client = MockClient((r) async {
          if (r.url.path.endsWith('/content'))
            return http.Response.bytes(coverJPEG(), 200);
          if (r.url.path.endsWith('/preview')) {
            previews++;
            final body = jsonDecode(r.body) as Map<String, dynamic>;
            operation = body['operationId'];
            original = body;
            expectSync(body['expectedVersion'], 0);
            expectSync(body['rights']['canRetain'], true);
            expectSync(body['rights']['canPublish'], true);
            expectSync(
              base64Decode(body['contentBase64']),
              orderedEquals(avatarJPEG()),
            );
            return coverWire(coverApproval(body));
          }
          if (r.url.path.endsWith('/stop')) {
            stops++;
            expectSync(
              r.url.path,
              '${coverRef.path}/operations/$operation/stop',
            );
            expectSync(jsonDecode(r.body), {'expectedVersion': 0});
            stopped = true;
            return coverWire(coverReceipt(operation!, status: 'STOPPED'));
          }
          if (r.url.path.contains('/operations/')) {
            receipts++;
            expectSync(r.url.path, '${coverRef.path}/operations/$operation');
            return stopped
                ? coverWire(coverReceipt(operation!, status: 'STOPPED'))
                : coverWire(null, status: 404);
          }
          if (r.method == 'PUT') {
            writes++;
            final body = jsonDecode(r.body) as Map<String, dynamic>;
            expectSync({...body}..remove('approval'), original);
            expectSync(body['approval'], 'ec1.1.2.${'a' * 64}');
            final durable = await store.read(coverBase, avatarPeer, coverRef);
            expectSync(durable!.stage, 'UNKNOWN');
            expectSync(durable.operationID, operation);
            if (mode == 'unknownStop' || mode == 'unknownRestart')
              throw StateError('synthetic result lost');
            if (mode == 'lateCommitABA') return lateCommit.future;
            committed = true;
            return coverWire(coverReceipt(operation!));
          }
          return coverWire({
            'version': committed ? 1 : 0,
            'cover': committed ? coverMetadata() : null,
          });
        });
        addTearDown(client.close);
        await t.pumpWidget(coverEditorApp(client, identity, store));
        await coverSettle(t);
        expect(
          t
              .widget<FilledButton>(find.byKey(const Key('cover-preview')))
              .onPressed,
          isNull,
        );
        await approveCoverRights(t);
        await coverTap(t, find.byKey(const Key('cover-preview')));
        expect(previews, 1);
        expect(writes, 0);
        expect(find.text('确认封面与授权'), findsOneWidget);
        final preview = find.widgetWithText(FilledButton, '确认保存封面');
        final image = find.descendant(
          of: find.byType(AlertDialog),
          matching: find.byType(Image),
        );
        await t.runAsync(
          () => precacheImage(
            t.widget<Image>(image).image,
            t.element(image),
          ).timeout(const Duration(seconds: 2)),
        );
        await t.pump();
        await t.pumpAndSettle();
        final raw = t.widget<RawImage>(
          find.descendant(of: image, matching: find.byType(RawImage)),
        );
        expect(raw.image, isNotNull);
        expect(raw.image!.width, 320);
        expect(raw.image!.height, 180);
        if (mode == 'cancel')
          await coverTap(t, find.text('返回编辑'));
        else if (mode == 'actorABA') {
          final old = t.widget<FilledButton>(preview).onPressed!;
          identity.value = 'Bearer B';
          identity.value = 'Bearer A';
          await t.pump();
          expect(find.byType(Image), findsNothing);
          expect(find.text('隔离合成活动'), findsNothing);
          expect(t.widget<FilledButton>(preview).onPressed, isNull);
          old();
          await coverSettle(t);
          expect(writes, 0);
        } else if (mode == 'targetABA') {
          final other = EntityCoverRef('organization', coverRef.id);
          await t.pumpWidget(
            coverEditorApp(client, identity, store, ref: other),
          );
          await t.pumpWidget(coverEditorApp(client, identity, store));
          await t.pump();
          expect(find.byType(Image), findsNothing);
          expect(find.text('查看实际封面预览'), findsNothing);
          expect(writes, 0);
        } else {
          if (mode == 'lateCommitABA') {
            await t.tap(preview);
            await t.pump();
            await t.runAsync(() async {
              await Future<void>.delayed(const Duration(milliseconds: 80));
            });
            expect(writes, 1);
            identity.value = 'Bearer B';
            identity.value = 'Bearer A';
            lateCommit.complete(coverWire(coverReceipt(operation!)));
            await coverSettle(t);
            expect(find.text('原封面保存已完成。'), findsNothing);
            expect(
              (await store.read(coverBase, avatarPeer, coverRef))!.operationID,
              operation,
            );
          } else {
            await coverTap(t, preview);
            if (mode == 'preparedReadFailure') {
              expect(writes, 0);
              expect(receipts, 0);
              expect(find.textContaining('尚未提交'), findsOneWidget);
              await coverTap(t, find.text('检查本机记录'));
              expect(await store.read(coverBase, avatarPeer, coverRef), isNull);
              expect(writes, 0);
              expect(receipts, 0);
            } else if (mode == 'unknownStop') {
              expect(writes, 1);
              await coverTap(t, find.text('核实上次封面操作'));
              expect(receipts, 1);
              expect(find.textContaining('仍待核实'), findsOneWidget);
              expect(
                (await store.read(
                  coverBase,
                  avatarPeer,
                  coverRef,
                ))!.operationID,
                operation,
              );
              await coverTap(t, find.text('停止这次提交'));
              await coverTap(t, find.text('确认停止'));
              expect(stops, 1);
              expect(writes, 1);
              expect(await store.read(coverBase, avatarPeer, coverRef), isNull);
              expect(find.textContaining('这次提交已停止'), findsOneWidget);
            } else if (mode == 'unknownRestart') {
              expect(writes, 1);
              await t.pumpWidget(const SizedBox());
              await t.pumpAndSettle();
              await t.pumpWidget(coverEditorApp(client, identity, store));
              await coverSettle(t);
              expect(find.text('本机草稿，尚未保存'), findsNothing);
              await coverTap(t, find.text('核实上次封面操作'));
              expect(receipts, 1);
              expect(writes, 1);
              expect(previews, 1);
              expect(
                (await store.read(
                  coverBase,
                  avatarPeer,
                  coverRef,
                ))!.operationID,
                operation,
              );
            } else {
              expect(writes, 1);
              expect(await store.read(coverBase, avatarPeer, coverRef), isNull);
              expect(find.text('原封面保存已完成。'), findsOneWidget);
            }
          }
        }
        if (const {'cancel', 'actorABA', 'targetABA'}.contains(mode))
          expect(writes, 0);
        await t.pumpWidget(const SizedBox());
        await t.pumpAndSettle();
        expect(t.takeException(), isNull);
      },
    );
  }
  for (final round in [1, 2]) {
    testWidgets(
      'cover secure store same-key serial tail retires across widget zones $round',
      (t) async {
        FlutterSecureStorage.setMockInitialValues({});
        final identity = ValueNotifier('Bearer A');
        addTearDown(identity.dispose);
        final client = MockClient(
          (r) async => coverWire({'version': 0, 'cover': null}),
        );
        addTearDown(client.close);
        await t.pumpWidget(
          coverEditorApp(client, identity, SecureEntityCoverPendingStore()),
        );
        await coverSettle(t);
        expect(find.byType(LinearProgressIndicator), findsNothing);
        expect(find.text('检查本机记录'), findsNothing);
        await t.pumpWidget(const SizedBox());
        await t.pumpAndSettle();
      },
    );
  }
  testWidgets(
    'cover undecodable normalized JPEG cannot approve or persist a write',
    (t) async {
      final identity = ValueNotifier('Bearer A');
      addTearDown(identity.dispose);
      final store = MemoryEntityCoverPendingStore();
      var writes = 0;
      final bad = coverJPEG();
      for (var at = 0; at + 21 < bad.length; at++) {
        if (bad[at] == 255 && bad[at + 1] == 196) {
          for (var n = at + 5; n < at + 21; n++) bad[n] = 255;
          break;
        }
      }
      final client = MockClient((r) async {
        if (r.method == 'PUT') {
          writes++;
          return coverWire(null, status: 500);
        }
        if (r.url.path.endsWith('/preview')) {
          final approval = coverApproval(
            jsonDecode(r.body) as Map<String, dynamic>,
          );
          final hash = sha256.convert(bad).toString();
          approval['inputSha256'] = hash;
          approval['previewDerivative'] = {
            'contentType': 'image/jpeg',
            'width': 320,
            'height': 180,
            'byteCount': bad.length,
            'sha256': hash,
            'contentBase64': base64Encode(bad),
          };
          return coverWire(approval);
        }
        return coverWire({'version': 0, 'cover': null});
      });
      addTearDown(client.close);
      await t.pumpWidget(coverEditorApp(client, identity, store));
      await coverSettle(t);
      await approveCoverRights(t);
      await coverTap(t, find.byKey(const Key('cover-preview')));
      expect(find.text('封面预览无法显示，请返回编辑。'), findsOneWidget);
      expect(
        t
            .widget<FilledButton>(find.widgetWithText(FilledButton, '确认保存封面'))
            .onPressed,
        isNull,
      );
      expect(writes, 0);
      expect(store.values, isEmpty);
      await coverTap(t, find.text('返回编辑'));
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
}
