import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:birdtie_client/src/workspace/place_photo.dart';
import 'package:birdtie_client/src/workspace/place_photo_editor.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'batch_a_visual_test.dart' show visualTheme;
import 'entity_cover_test.dart'
    show coverJPEG, coverSettle, coverWire, decodeCover;
import 'person_avatar_fixture.dart' show avatarPeer, avatarOwner, avatarJPEG;
import 'place_photo_test.dart';

const photoCaptureKey = Key('place-photo-isolated-render');
Future<void> loadPhotoFont() async {
  if (Platform.environment['BIRDTIE_PLACE_PHOTO_CAPTURE'] == null) return;
  for (final f in [
    ('BatchACJK', r'C:\Windows\Fonts\msyh.ttc'),
    (
      'MaterialIcons',
      r'D:\DevTools\flutter\bin\cache\artifacts\material_fonts\MaterialIcons-Regular.otf',
    ),
  ]) {
    await (FontLoader(f.$1)..addFont(
          Future.value(ByteData.sublistView(await File(f.$2).readAsBytes())),
        ))
        .load();
  }
}

Future<void> photoCapture(WidgetTester t, String name) async {
  final folder = Platform.environment['BIRDTIE_PLACE_PHOTO_CAPTURE'];
  if (folder == null) return;
  await t.runAsync(() async {
    final image = await t
        .renderObject<RenderRepaintBoundary>(find.byKey(photoCaptureKey))
        .toImage(pixelRatio: 1);
    final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
    final file = File('$folder/$name.png');
    await file.parent.create(recursive: true);
    if (await file.exists()) throw StateError('Immutable screenshot exists');
    await file.writeAsBytes(bytes!.buffer.asUint8List());
    image.dispose();
  });
}

Widget photoEditorApp(
  http.Client client,
  ValueNotifier<String> identity,
  PlacePhotoPendingStore store, {
  PlacePhotoRef ref = photoRef,
  bool initial = true,
}) => RepaintBoundary(
  key: photoCaptureKey,
  child: MaterialApp(
    theme: Platform.environment['BIRDTIE_PLACE_PHOTO_CAPTURE'] == null
        ? null
        : visualTheme(),
    debugShowCheckedModeBanner: false,
    builder: (_, child) => Column(
      children: [
        const Material(
          child: SizedBox(
            height: 28,
            child: Center(child: Text('隔离合成样例 · 非真实身份或官方照片')),
          ),
        ),
        Expanded(child: child!),
      ],
    ),
    home: PlacePhotoEditor(
      key: const Key('original-photo-editor'),
      ref: ref,
      name: '隔离合成地点 Aberdeen · QA',
      ownerID: avatarPeer,
      api: PlacePhotoAPI(
        client: client,
        baseUrl: photoBase,
        authorizationHeader: () => identity.value,
      ),
      identityChanges: identity,
      current: () => true,
      initialImage: initial ? avatarJPEG() : null,
      pendingStore: store,
    ),
  ),
);
Future<void> photoTap(WidgetTester t, Finder f) async {
  await t.ensureVisible(f);
  await t.pumpAndSettle();
  await t.tap(f.hitTestable());
  await coverSettle(t);
}

Future<void> photoRights(WidgetTester t) async {
  await photoTap(t, find.byKey(const Key('place-photo-rights-basis')));
  await t.tap(find.text('本人拥有图片权利').last);
  await t.pumpAndSettle();
  await t.enterText(
    find.byKey(const Key('place-photo-attribution')),
    '隔离合成图片 · QA',
  );
  await t.enterText(
    find.byKey(const Key('place-photo-retention')),
    '2030-01-02',
  );
  t.testTextInput.hide();
  await t.pump();
  await photoTap(t, find.byKey(const Key('place-photo-retain')));
  await photoTap(t, find.byKey(const Key('place-photo-publish')));
}

Future<void> photoDecodeDialog(WidgetTester t) async {
  final f = find.descendant(
    of: find.byType(AlertDialog),
    matching: find.byType(Image),
  );
  expect(f, findsOneWidget);
  await t.runAsync(
    () => precacheImage(
      t.widget<Image>(f).image,
      t.element(f),
    ).timeout(const Duration(seconds: 2)),
  );
  await t.pump();
  await t.pumpAndSettle();
  final raw = t.widget<RawImage>(
    find.descendant(of: f, matching: find.byType(RawImage)),
  );
  expect(raw.image, isNotNull);
  expect(raw.image!.width, 320);
  expect(raw.image!.height, 180);
}

class _ReadbackFault extends MemoryPlacePhotoPendingStore {
  bool fault = false;
  @override
  Future<void> write(
    String o,
    String owner,
    PlacePhotoRef ref,
    PlacePhotoPending p,
  ) async {
    await super.write(o, owner, ref, p);
    if (p.stage == 'PREPARED') fault = true;
  }

  @override
  Future<PlacePhotoPending?> read(
    String o,
    String owner,
    PlacePhotoRef ref,
  ) async {
    if (fault) {
      fault = false;
      throw StateError('isolated storage readback failure');
    }
    return super.read(o, owner, ref);
  }
}

class _HeldPhotoStorage extends FlutterSecureStorage {
  final values = <String, String>{};
  Completer<void>? hold;
  int writes = 0;
  @override
  Future<String?> read({
    required String key,
    AppleOptions? iOptions,
    AndroidOptions? aOptions,
    LinuxOptions? lOptions,
    WebOptions? webOptions,
    AppleOptions? mOptions,
    WindowsOptions? wOptions,
  }) async {
    final pending = hold;
    hold = null;
    if (pending != null) await pending.future;
    return values[key];
  }

  @override
  Future<void> write({
    required String key,
    required String? value,
    AppleOptions? iOptions,
    AndroidOptions? aOptions,
    LinuxOptions? lOptions,
    WebOptions? webOptions,
    AppleOptions? mOptions,
    WindowsOptions? wOptions,
  }) async {
    writes++;
    if (value == null) {
      values.remove(key);
    } else {
      values[key] = value;
    }
  }

  @override
  Future<void> delete({
    required String key,
    AppleOptions? iOptions,
    AndroidOptions? aOptions,
    LinuxOptions? lOptions,
    WebOptions? webOptions,
    AppleOptions? mOptions,
    WindowsOptions? wOptions,
  }) async {
    values.remove(key);
  }
}

void normalPhotoView(WidgetTester t) {
  t.view.physicalSize = const Size(390, 844);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
}

void main() {
  setUpAll(loadPhotoFont);
  test(
    'Place pending isolates origin owner target and refuses old selector overwrite or delete',
    () async {
      final store = MemoryPlacePhotoPendingStore();
      final p = PlacePhotoPending(photoSelector('SUBMIT'));
      await store.write(photoBase, avatarPeer, photoRef, p);
      expect(
        p.toJson().keys,
        unorderedEquals(['operationId', 'expectedVersion', 'action', 'stage']),
      );
      expect(jsonEncode(p.toJson()), isNot(contains('rights')));
      expect(await store.read(photoBase, avatarOwner, photoRef), isNull);
      expect(
        await store.read('https://other.test', avatarPeer, photoRef),
        isNull,
      );
      expect(
        await store.read(
          photoBase,
          avatarPeer,
          const PlacePhotoRef(avatarPeer),
        ),
        isNull,
      );
      final different = PlacePhotoPending(
        PlacePhotoSelector(
          operationID: avatarOwner,
          expectedVersion: 0,
          action: 'SUBMIT',
        ),
      );
      await expectLater(
        store.write(photoBase, avatarPeer, photoRef, different),
        throwsFormatException,
      );
      await expectLater(
        store.remove(photoBase, avatarPeer, photoRef, different),
        throwsFormatException,
      );
      await expectLater(
        store.remove(
          photoBase,
          avatarPeer,
          photoRef,
          PlacePhotoPending(p.selector, stage: 'PREPARED'),
        ),
        throwsFormatException,
      );
      expect(
        (await store.read(photoBase, avatarPeer, photoRef))!.same(p),
        isTrue,
      );
    },
  );
  test(
    'Place secure journal hanging old storage cannot replace or delete another original selector',
    () async {
      final storage = _HeldPhotoStorage(),
          p = PlacePhotoPending(photoSelector('SUBMIT'));
      final first = SecurePlacePhotoPendingStore(storage: storage),
          second = SecurePlacePhotoPendingStore(storage: storage);
      await first.write(photoBase, avatarPeer, photoRef, p);
      final hold = Completer<void>();
      storage.hold = hold;
      final oldRemove = first.remove(
        photoBase,
        avatarPeer,
        photoRef,
        PlacePhotoPending(p.selector, stage: 'PREPARED'),
      );
      final oldResult = expectLater(oldRemove, throwsFormatException);
      final next = PlacePhotoPending(
        PlacePhotoSelector(
          operationID: avatarOwner,
          expectedVersion: 0,
          action: 'SUBMIT',
        ),
      );
      final newWrite = second.write(photoBase, avatarPeer, photoRef, next);
      final newResult = expectLater(newWrite, throwsFormatException);
      await Future<void>.delayed(Duration.zero);
      expect(storage.writes, 1);
      hold.complete();
      await oldResult;
      await newResult;
      expect(
        (await first.read(photoBase, avatarPeer, photoRef))!.same(p),
        isTrue,
      );
      expect(storage.writes, 1);
    },
  );
  for (final mode in [
    'submit',
    'actorABA',
    'targetABA',
    'expiry',
    'preparedFault',
    'unknown401RestartStop',
    'lateCommitABA',
    'confirmedReceiptCurrent404',
  ]) {
    testWidgets(
      'Place photo concrete JPEG approval and original recovery $mode',
      (t) async {
        normalPhotoView(t);
        final identity = ValueNotifier('Bearer A');
        addTearDown(identity.dispose);
        final PlacePhotoPendingStore store = mode == 'preparedFault'
            ? _ReadbackFault()
            : MemoryPlacePhotoPendingStore();
        final late = Completer<http.Response>();
        var writes = 0,
            previews = 0,
            receipts = 0,
            stops = 0,
            submitted = false;
        Map<String, dynamic>? original;
        final client = MockClient((r) async {
          expectSync(r.headers['Authorization'], 'Bearer A');
          if (mode == 'confirmedReceiptCurrent404' &&
              submitted &&
              r.url.path.endsWith('/editorial'))
            return coverWire(null, status: 403);
          if (r.url.path.endsWith('/editorial'))
            return coverWire(
              photoEditorial(
                candidates: submitted
                    ? [photoCandidate(id: original!['operationId'])]
                    : [],
              ),
            );
          if (r.url.path.endsWith('/preview')) {
            previews++;
            original = jsonDecode(r.body);
            expectSync(original!['action'], 'SUBMIT');
            return coverWire(photoApproval(original!));
          }
          if (r.url.path.endsWith('/stop')) {
            stops++;
            expectSync(jsonDecode(r.body), {'expectedVersion': 0});
            return coverWire(photoReceipt(original!, stopped: true));
          }
          if (r.url.path.contains('/operations/')) {
            receipts++;
            expectSync(
              r.url.path,
              '${photoRef.path}/operations/${original!['operationId']}',
            );
            return coverWire(null, status: 404);
          }
          if (r.method == 'PUT') {
            writes++;
            original = jsonDecode(r.body);
            expectSync(original!['approval'], startsWith('pp1.'));
            if (mode == 'unknown401RestartStop')
              return coverWire(null, status: 401);
            if (mode == 'lateCommitABA') return late.future;
            submitted = true;
            return coverWire(photoReceipt(original!));
          }
          expectSync(r.method, 'GET');
          if (mode == 'confirmedReceiptCurrent404' && submitted)
            return coverWire(null, status: 404);
          return coverWire({'version': 0, 'placePhoto': null});
        });
        addTearDown(client.close);
        await t.pumpWidget(photoEditorApp(client, identity, store));
        await coverSettle(t);
        expect(
          t
              .widget<FilledButton>(
                find.byKey(const Key('place-photo-preview')),
              )
              .onPressed,
          isNull,
        );
        await photoRights(t);
        await photoTap(t, find.byKey(const Key('place-photo-preview')));
        expect(previews, 1);
        expect(writes, 0);
        await photoDecodeDialog(t);
        if (mode == 'submit') await photoCapture(t, 'normalized-preview');
        final confirm = find.byKey(const Key('place-photo-confirm'));
        final oldConfirm = t.widget<FilledButton>(confirm).onPressed!;
        if (mode == 'actorABA') {
          identity.value = 'Bearer B';
          identity.value = 'Bearer A';
          await t.pump();
          expect(find.byType(Image), findsNothing);
          expect(find.text('隔离合成地点 Aberdeen · QA'), findsNothing);
          expect(find.textContaining('隔离合成图片'), findsNothing);
          expect(t.widget<FilledButton>(confirm).onPressed, isNull);
          oldConfirm();
          await coverSettle(t);
          expect(writes, 0);
        } else if (mode == 'targetABA') {
          await t.pumpWidget(
            photoEditorApp(
              client,
              identity,
              store,
              ref: const PlacePhotoRef(avatarPeer),
            ),
          );
          await t.pumpWidget(photoEditorApp(client, identity, store));
          await t.pump();
          expect(find.byType(Image), findsNothing);
          expect(t.widget<FilledButton>(confirm).onPressed, isNull);
          oldConfirm();
          await coverSettle(t);
          expect(writes, 0);
        } else if (mode == 'expiry') {
          await t.pump(const Duration(seconds: 26));
          await t.pump();
          expect(find.byType(Image), findsNothing);
          expect(t.widget<FilledButton>(confirm).onPressed, isNull);
          await photoCapture(t, 'expired');
          oldConfirm();
          await coverSettle(t);
          expect(writes, 0);
          expect(await store.read(photoBase, avatarPeer, photoRef), isNull);
          await photoTap(t, find.text('返回'));
        } else if (mode == 'lateCommitABA') {
          await t.tap(confirm);
          await t.pump();
          await t.runAsync(() async {
            await Future<void>.delayed(const Duration(milliseconds: 100));
          });
          expect(writes, 1);
          identity.value = 'Bearer B';
          identity.value = 'Bearer A';
          late.complete(coverWire(photoReceipt(original!)));
          await coverSettle(t);
          expect(find.textContaining('照片已提交'), findsNothing);
          expect(
            (await store.read(
              photoBase,
              avatarPeer,
              photoRef,
            ))!.selector.operationID,
            original!['operationId'],
          );
        } else {
          await photoTap(t, confirm);
          if (mode == 'preparedFault') {
            expect(writes, 0);
            expect(receipts, 0);
            expect(find.text('尚未提交，无法确认本机记录。请检查本机记录。'), findsOneWidget);
            await photoTap(t, find.text('检查本机记录'));
            expect(await store.read(photoBase, avatarPeer, photoRef), isNull);
            expect(receipts, 0);
            expect(writes, 0);
          } else if (mode == 'unknown401RestartStop') {
            expect(writes, 1);
            await t.pumpWidget(const SizedBox());
            await t.pumpAndSettle();
            await t.pumpWidget(photoEditorApp(client, identity, store));
            await coverSettle(t);
            expect(find.text('本机草稿，尚未提交审核'), findsNothing);
            await photoTap(t, find.text('核实上次照片操作'));
            expect(receipts, 1);
            expect(writes, 1);
            expect(previews, 1);
            expect(find.textContaining('仍待核实'), findsOneWidget);
            await photoCapture(t, 'unknown-original404');
            await photoTap(t, find.text('停止这次提交'));
            await photoTap(t, find.text('确认停止'));
            expect(stops, 1);
            expect(writes, 1);
            expect(await store.read(photoBase, avatarPeer, photoRef), isNull);
          } else if (mode == 'confirmedReceiptCurrent404') {
            expect(writes, 1);
            expect(receipts, 0);
            expect(await store.read(photoBase, avatarPeer, photoRef), isNull);
            expect(find.textContaining('照片已提交'), findsOneWidget);
            expect(find.textContaining('当前照片或管理资料暂不可读取'), findsOneWidget);
            expect(find.text('原地点当前资料不可读取'), findsOneWidget);
            expect(find.text('核实上次照片操作'), findsNothing);
            expect(find.text('选择地点照片'), findsNothing);
          } else {
            expect(writes, 1);
            expect(await store.read(photoBase, avatarPeer, photoRef), isNull);
            expect(find.text('照片已提交，等待其他审核者检查，尚未公开。'), findsOneWidget);
            expect(find.text('检查并批准'), findsNothing);
            expect(find.byType(Image), findsNothing);
            await photoCapture(t, 'submitted-awaiting-independent-review');
          }
        }
        await t.pumpWidget(const SizedBox());
        await t.pumpAndSettle();
        expect(t.takeException(), isNull);
      },
    );
  }
  for (final action in ['APPROVE', 'REJECT', 'WITHDRAW']) {
    testWidgets(
      'Place photo authoritative candidate $action consequences and receipts',
      (t) async {
        normalPhotoView(t);
        final identity = ValueNotifier('Bearer A');
        addTearDown(identity.dispose);
        final store = MemoryPlacePhotoPendingStore();
        var finished = false, writes = 0;
        final client = MockClient((r) async {
          if (r.url.path.endsWith('/content'))
            return http.Response.bytes(coverJPEG(), 200);
          if (r.url.path.endsWith('/editorial'))
            return coverWire(
              photoEditorial(
                version: finished && action == 'APPROVE' ? 1 : 0,
                contribute: false,
                review: action != 'WITHDRAW',
                candidates: finished
                    ? []
                    : [
                        photoCandidate(
                          approve: action == 'APPROVE',
                          reject: action == 'REJECT',
                          withdraw: action == 'WITHDRAW',
                        ),
                      ],
              ),
            );
          if (r.url.path.endsWith('/preview'))
            return coverWire(photoApproval(jsonDecode(r.body)));
          if (r.method == 'PUT') {
            writes++;
            finished = true;
            final body = jsonDecode(r.body) as Map<String, dynamic>;
            expectSync(body['candidateId'], photoCandidateID);
            expectSync(body['candidateVersion'], 1);
            expectSync(body.containsKey('contentBase64'), isFalse);
            expectSync(body.containsKey('rights'), isFalse);
            expectSync(
              body['rightsConfirmed'],
              action == 'APPROVE' ? true : null,
            );
            return coverWire(photoReceipt(body));
          }
          return coverWire({
            'version': finished && action == 'APPROVE' ? 1 : 0,
            'placePhoto': finished && action == 'APPROVE'
                ? photoMetadata()
                : null,
          });
        });
        addTearDown(client.close);
        await t.pumpWidget(
          photoEditorApp(client, identity, store, initial: false),
        );
        await coverSettle(t);
        expect(find.text('选择地点照片'), findsNothing);
        final text = action == 'APPROVE'
            ? '检查并批准'
            : action == 'REJECT'
            ? '检查并拒绝'
            : '撤回照片';
        await photoTap(t, find.text(text));
        if (action != 'WITHDRAW')
          await photoDecodeDialog(t);
        else
          expect(find.byType(Image), findsNothing);
        if (action == 'APPROVE')
          await photoCapture(t, 'normalized-approved-preview');
        await photoTap(t, find.byKey(const Key('place-photo-confirm')));
        expect(writes, 1);
        expect(await store.read(photoBase, avatarPeer, photoRef), isNull);
        if (action == 'APPROVE') {
          await decodeCover(t);
          await photoCapture(t, 'approved-current-public-photo');
        }
        await t.pumpWidget(const SizedBox());
        await t.pumpAndSettle();
        expect(t.takeException(), isNull);
      },
    );
  }
  for (final round in [1, 2]) {
    testWidgets(
      'Place secure journal per-key tail completes across widget zones $round',
      (t) async {
        FlutterSecureStorage.setMockInitialValues({});
        final identity = ValueNotifier('Bearer A');
        addTearDown(identity.dispose);
        final client = MockClient(
          (r) async => coverWire(
            r.url.path.endsWith('/editorial')
                ? photoEditorial()
                : {'version': 0, 'placePhoto': null},
          ),
        );
        addTearDown(client.close);
        await t.pumpWidget(
          photoEditorApp(
            client,
            identity,
            SecurePlacePhotoPendingStore(),
            initial: false,
          ),
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
    'Place undecodable normalized JPEG cannot approve or persist a write',
    (t) async {
      normalPhotoView(t);
      final identity = ValueNotifier('Bearer A');
      addTearDown(identity.dispose);
      final store = MemoryPlacePhotoPendingStore();
      var writes = 0;
      final bad = coverJPEG();
      for (var at = 0; at + 21 < bad.length; at++) {
        if (bad[at] == 255 && bad[at + 1] == 196) {
          for (var n = at + 5; n < at + 21; n++) {
            bad[n] = 255;
          }
          break;
        }
      }
      final client = MockClient((r) async {
        if (r.method == 'PUT') {
          writes++;
          return coverWire(null, status: 500);
        }
        if (r.url.path.endsWith('/editorial'))
          return coverWire(photoEditorial());
        if (r.url.path.endsWith('/preview'))
          return coverWire(photoApproval(jsonDecode(r.body), bytes: bad));
        return coverWire({'version': 0, 'placePhoto': null});
      });
      addTearDown(client.close);
      await t.pumpWidget(photoEditorApp(client, identity, store));
      await coverSettle(t);
      await photoRights(t);
      await photoTap(t, find.byKey(const Key('place-photo-preview')));
      expect(find.text('照片未能显示，不能确认。'), findsOneWidget);
      expect(
        t
            .widget<FilledButton>(find.byKey(const Key('place-photo-confirm')))
            .onPressed,
        isNull,
      );
      expect(writes, 0);
      expect(store.values, isEmpty);
      await photoTap(t, find.text('返回'));
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
}
