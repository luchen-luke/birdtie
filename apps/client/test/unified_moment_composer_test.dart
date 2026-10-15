import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:birdtie_client/src/content/moment_control.dart';
import 'package:birdtie_client/src/content/moment_locations.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/entity_cover.dart';
import 'package:birdtie_client/src/workspace/human_causal_control.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/moment_thread_api.dart';
import 'package:birdtie_client/src/workspace/moment_thread_controller.dart';
import 'package:birdtie_client/src/workspace/moment_thread_panel.dart';
import 'package:birdtie_client/src/workspace/moment_thread_pending_store.dart';
import 'package:birdtie_client/src/workspace/public_moment_media.dart';
import 'package:birdtie_client/src/workspace/public_moment_media_editor.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'now_scope_recovery_test.dart' show NowFixture;
import 'video_material_picker_test.dart' show syntheticVideoBytes;

const unifiedOwner = '00000000-0000-4000-8000-000000000951';
const unifiedFirst = '00000000-0000-4000-8000-000000000952';
const unifiedRemoved = '00000000-0000-4000-8000-000000000953';
const unifiedThread = '00000000-0000-4000-8000-000000000954';
const unifiedImage = '00000000-0000-4000-8000-000000000955';
const unifiedVideo = '00000000-0000-4000-8000-000000000956';
const unifiedImageMoment = '00000000-0000-4000-8000-000000000957';
const unifiedVideoMoment = '00000000-0000-4000-8000-000000000958';
const unifiedImageOperation = '00000000-0000-4000-8000-000000000959';
const unifiedVideoOperation = '00000000-0000-4000-8000-000000000960';
const unifiedActivity = '00000000-0000-4000-8000-000000000961';
const unifiedCommunity = '00000000-0000-4000-8000-000000000962';
const unifiedOrganization = '00000000-0000-4000-8000-000000000963';

MomentThreadJournal unifiedJournal(Map<String, String> values) =>
    MomentThreadJournal(
      readValue: (key) async => values[key],
      writeValue: (key, value) async {
        values[key] = value;
      },
    );

http.Response unifiedResponse(Object data) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': data})),
  200,
  headers: {'content-type': 'application/json'},
);

MomentThreadAPI unifiedAPI(http.Client client) => MomentThreadAPI(
  client: client,
  baseUrl: 'https://unified.test',
  authorizationHeader: () => 'Bearer synthetic-unified-only',
  current: () => true,
);

VoidCallback unifiedDisposeOnce(MomentThreadController controller) {
  var disposed = false;
  return () {
    if (disposed) return;
    disposed = true;
    controller.dispose();
  };
}

MomentLocationInput unifiedCity(String id, String city) =>
    MomentLocationInput(id: id, kind: 'CITY', cityID: city, precision: 'city');

Widget unifiedPanel(
  MomentThreadController controller, {
  bool materialsReady = true,
  ValueChanged<String>? onActiveText,
  ValueChanged<MomentThreadReceipt>? onPublished,
}) => MaterialApp(
  home: Scaffold(
    body: SizedBox(
      height: 560,
      child: MomentThreadPanel(
        controller: controller,
        materialsReady: materialsReady,
        onActiveText: onActiveText ?? (_) {},
        onPublished: onPublished,
      ),
    ),
  ),
);

void main() {
  testWidgets(
    'actual Now replaces a STOPPED video operation reference without clearing another entry missing material and restores the replacement journal',
    (tester) async {
      FlutterSecureStorage.setMockInitialValues({});
      tester.view.physicalSize = const Size(390, 844);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final poster = (await tester.runAsync(
        () => File('test/fixtures/map-media/isolation-a.jpg').readAsBytes(),
      ))!;
      final rawVideo = syntheticVideoBytes();
      final posterSHA = sha256.convert(poster).toString();
      const genuinelyMissing = unifiedActivity;
      final f = NowFixture();
      f.auth.changeIdentity(
        'Bearer synthetic-now-stop',
        nextOwner: unifiedOwner,
      );
      String? stoppedID, replacementID;
      var stopped = false;
      final calls = <String>[];
      Map<String, Object?>? successfulStage;
      Map<String, Object?> stoppedFact(String id) => {
        'operationId': id,
        'status': 'STOPPED',
        'payloadDigest': '',
        'material': null,
        'committedAt': momentUTC(DateTime.now().toUtc()),
      };
      final client = MockClient((request) async {
        final path = request.url.path;
        calls.add('${request.method} $path');
        if (request.method == 'PUT' &&
            path.startsWith(
              '/v1/me/moment-media-video-materials/operations/',
            )) {
          final input = jsonDecode(request.body) as Map<String, dynamic>;
          final op = input['operationId'] as String;
          stoppedID ??= op;
          if (op == stoppedID && !stopped) {
            return http.Response(
              '{}',
              503,
              headers: {'content-type': 'application/json'},
            );
          }
          if (op == stoppedID) return unifiedResponse(stoppedFact(op));
          replacementID = op;
          final at = DateTime.now().toUtc();
          successfulStage = {
            'operationId': op,
            'status': 'SUCCEEDED',
            'payloadDigest': humanControlTupleDigest([
              'moment-video-stage-v2',
              op,
              input['localScope'],
              publicMomentVideoPurpose,
              'video/mp4',
              sha256.convert(base64Decode(input['contentBase64'])).toString(),
            ]),
            'committedAt': momentUTC(at),
            'material': {
              // Typed stage/poster fixture only; no actual MP4 transcoding.
              'schemaVersion': 'public-moment-media-stage-v2', 'kind': 'VIDEO',
              'purpose': publicMomentVideoPurpose, 'mediaId': unifiedVideo,
              'mediaVersion': 1, 'localScope': input['localScope'],
              'displaySha256': posterSHA, 'displayBytes': poster.length,
              'width': 320, 'height': 180, 'thumbnailSha256': posterSHA,
              'thumbnailBytes': poster.length,
              'validationKind': publicMomentVideoValidation,
              'validationStatus': 'VALIDATED',
              'antivirusScan': 'NOT_RUN', 'privacyRisk': 'UNKNOWN',
              'preparedAt': momentUTC(at),
              'retainUntil': momentUTC(at.add(const Duration(minutes: 10))),
              'video': {
                'mimeType': 'video/mp4',
                'durationMs': 1000,
                'codecVideo': 'h264',
                'codecAudio': 'none',
                'pixelFormat': 'yuv420p',
                'videoSha256': sha256.convert(rawVideo).toString(),
                'videoBytes': rawVideo.length,
                'videoPath':
                    '/v1/me/moment-media-materials/$unifiedVideo/1/video',
              },
            },
          };
          return unifiedResponse(successfulStage!);
        }
        if (path.startsWith('/v1/me/moment-media-materials/operations/')) {
          if (request.method == 'POST' && path.endsWith('/stop')) {
            expect(
              path,
              '/v1/me/moment-media-materials/operations/$stoppedID/stop',
            );
            stopped = true;
            return unifiedResponse(stoppedFact(stoppedID!));
          }
          if (request.method == 'GET' && path.endsWith('/$stoppedID')) {
            return stopped
                ? unifiedResponse(stoppedFact(stoppedID!))
                : http.Response(
                    '{}',
                    404,
                    headers: {'content-type': 'application/json'},
                  );
          }
          if (request.method == 'GET' && path.endsWith('/$replacementID')) {
            return unifiedResponse(successfulStage!);
          }
          throw StateError('unexpected synthetic stage selector');
        }
        if (path == '/v1/me/moment-media-materials/$unifiedVideo/1/thumbnail') {
          expect(request.method, 'GET');
          return http.Response.bytes(
            poster,
            200,
            headers: {'content-type': 'image/jpeg'},
          );
        }
        // Existing Now catalog/readers may independently read their empty
        // synthetic fixtures; this test creates no public operation.
        expect(request.method, 'GET');
        return unifiedResponse([]);
      });
      final moments = PrivateMomentController(
        client: client,
        apiBaseUrl: f.base,
        authorizationHeader: () => f.auth.authorizationHeader,
        ownerID: () => f.auth.accountID,
      );
      Future<void> mount() async {
        await tester.pumpWidget(
          MaterialApp(
            home: MapWorkspace(
              city: f.city,
              auth: f.auth,
              moments: moments,
              agentTaskSource: f.source,
              seedClient: client,
              seedApiBaseUrl: f.base,
            ),
          ),
        );
        await tester.pumpAndSettle();
      }

      Future<void> unmount() async {
        await tester.pumpWidget(const SizedBox());
        await tester.pumpAndSettle();
      }

      addTearDown(() async {
        await unmount();
        moments.dispose();
        client.close();
        f.dispose();
      });
      final journal = MomentThreadJournal();
      debugPrint('STOP recovery fixture: save cold journal');
      await journal.saveDraft(
        unifiedOwner,
        f.base,
        MomentThreadJournalState(
          unifiedThread,
          [
            MomentThreadDraftEntry(
              localID: unifiedFirst,
              momentID: unifiedFirst,
              expectedRevision: 3,
              cityID: '',
              originalStatus: 'published',
              preserveMedia: true,
              locations: const [],
            ),
            MomentThreadDraftEntry(
              localID: unifiedRemoved,
              cityID: '',
              text: '第二条文字和真正未找回的图片',
              title: '第二条独立标题',
              locations: [unifiedCity(unifiedImage, 'london-gb')],
              materialReferences: const [
                MomentDraftMaterialReference(genuinelyMissing, 'IMAGE'),
              ],
            ),
            MomentThreadDraftEntry(
              localID: unifiedVideoMoment,
              cityID: '',
              locations: const [],
              materialReferences: const [
                MomentDraftMaterialReference(unifiedVideoOperation, 'VIDEO'),
                MomentDraftMaterialReference(unifiedImageOperation, 'VIDEO'),
              ],
            ),
          ],
          2,
          null,
          threadID: unifiedThread,
          threadVersion: 2,
        ),
      );
      Future<void> waitFor(bool Function() done) async {
        for (var i = 0; i < 80 && !done(); i++) {
          await tester.runAsync(
            () => Future<void>.delayed(const Duration(milliseconds: 10)),
          );
          await tester.pump();
        }
        expect(done(), isTrue);
      }

      Future<T> pumpExternal<T>(Future<T> Function() action) async {
        var completed = false;
        final result = action();
        unawaited(
          result.then<void>(
            (_) => completed = true,
            onError: (Object _, StackTrace __) => completed = true,
          ),
        );
        await waitFor(() => completed);
        return await result;
      }

      MomentThreadPanel panel() =>
          tester.widget<MomentThreadPanel>(find.byType(MomentThreadPanel));
      PublicMomentMediaEditor editor() =>
          tester.widget<PublicMomentMediaEditor>(
            find.byType(PublicMomentMediaEditor),
          );
      Future<void> openDraft() async {
        await tester.runAsync(() async {
          tester
              .widget<AgentComposer>(find.byType(AgentComposer))
              .onCreateMoment!();
        });
        await waitFor(
          () =>
              find.byType(MomentThreadPanel).evaluate().isNotEmpty &&
              find.byType(PublicMomentMediaEditor).evaluate().isNotEmpty &&
              editor().controller.journalReady &&
              !editor().controller.busy,
        );
      }

      Future<void> prepareViaEditor() async {
        final button = tester.widget<FilledButton>(
          find.widgetWithText(FilledButton, '准备这条材料，随动态一起确认'),
        );
        expect(button.onPressed, isNotNull);
        // Streaming HTTP completion belongs to the real async zone; Flutter's
        // fake clock is advanced separately while the actual host listens.
        await tester.runAsync(() async => button.onPressed!());
        await waitFor(() => !editor().controller.busy);
        await tester.pump();
      }

      EntityCoverRights ownRights() => EntityCoverRights(
        basis: 'OWNED',
        attribution: '',
        licenseReference: '',
        canRetain: true,
        canPublish: true,
        retentionUntil: DateTime.now().toUtc().add(const Duration(days: 30)),
      );
      debugPrint('STOP recovery fixture: mount Now');
      await mount();
      debugPrint('STOP recovery fixture: open draft');
      await openDraft();
      debugPrint('STOP recovery fixture: draft restored');
      final thread = panel().controller, media = editor().controller;
      expect(thread.entries, hasLength(3));
      expect(thread.entries.first.preserveMedia, isTrue);
      expect(thread.entries.first.text, '');
      expect(thread.entries.first.momentID, unifiedFirst);
      expect(thread.entries.first.expectedRevision, 3);
      expect(thread.entries[1].text, '第二条文字和真正未找回的图片');
      expect(thread.entries[1].title, '第二条独立标题');
      expect(thread.entries[1].locations!.single.cityID, 'london-gb');
      expect(thread.entries.last.hasConflictingMaterialReferences, isTrue);
      expect(thread.entries.last.materialReferences.map((v) => v.operationID), [
        unifiedVideoOperation,
        unifiedImageOperation,
      ]);
      expect(
        () => thread.entries.last.toEditItem(unifiedThread),
        throwsFormatException,
      );
      await pumpExternal(() => thread.preview());
      expect(thread.previewValue, isNull);
      expect(thread.notice, contains('未完成材料不能忽略'));
      expect(panel().materialsReady, isFalse);
      expect(
        calls.where((v) => v.contains('/moment-threads/previews')),
        isEmpty,
      );
      // The failed legacy journal is retained until its two placeholders are
      // explicitly removed; recovery never silently drops either selector.
      expect(await pumpExternal(() => thread.flushDraft()), isTrue);
      final coldSaved = (await pumpExternal(
        () => journal.load(unifiedOwner, f.base),
      ))!;
      expect(
        coldSaved.entries.last.materialReferences.map((v) => v.operationID),
        [unifiedVideoOperation, unifiedImageOperation],
      );
      thread.removeMissingMaterial(unifiedVideoMoment, unifiedVideoOperation);
      expect(
        thread.entries.last.materialReferences.single.operationID,
        unifiedImageOperation,
      );
      thread.removeMissingMaterial(unifiedVideoMoment, unifiedImageOperation);
      expect(thread.entries.last.materialReferences, isEmpty);
      media.addSelectedVideo(
        PublicMomentVideoSelection(
          bytes: rawVideo,
          durationMS: 1000,
          width: 320,
          height: 180,
        ),
      );
      media.setRights(0, ownRights());
      await tester.pump();
      final originalSelection = media.selectionRevision;
      debugPrint('STOP recovery fixture: prepare unknown');
      await prepareViaEditor();
      expect(media.uncertain, isTrue);
      final pending = media.pending.single;
      expect(
        thread.entries.last.materialReferences.single.operationID,
        pending.operationID,
      );
      await pumpExternal(() => media.recover(pending));
      debugPrint('STOP recovery fixture: original GET unresolved');
      expect(media.uncertain, isTrue);
      expect(media.pending.single.operationID, stoppedID);
      await pumpExternal(() => media.recover(pending, stop: true));
      debugPrint('STOP recovery fixture: stopped original');
      await tester.pump();
      expect(media.pending, isEmpty);
      expect(media.uncertain, isFalse);
      // First preparation observes the old STOPPED fact and rotates the ID;
      // selectionRevision intentionally remains unchanged for the same bytes.
      await prepareViaEditor();
      debugPrint('STOP recovery fixture: operation rotated');
      final rotatedID = media.images.single.operationID;
      expect(rotatedID, isNot(stoppedID));
      expect(media.selectionRevision, originalSelection);
      expect(
        thread.entries.last.materialReferences.single.operationID,
        rotatedID,
      );
      await prepareViaEditor();
      debugPrint('STOP recovery fixture: replacement ready');
      expect(replacementID, rotatedID);
      expect(media.images.single.stage!.current, isTrue);
      expect(media.images.single.poster, isNotNull);
      expect(thread.entries.last.media.single.mediaID, unifiedVideo);
      expect(
        thread.entries.last.materialReferences.single.operationID,
        rotatedID,
      );
      expect(
        thread.entries
            .expand((v) => v.materialReferences)
            .any((v) => v.operationID == stoppedID),
        isFalse,
      );
      expect(
        thread.entries[1].materialReferences.single.operationID,
        genuinelyMissing,
      );
      expect(panel().materialsReady, isFalse);
      expect(
        tester
            .widget<FilledButton>(find.widgetWithText(FilledButton, '检查全部动态'))
            .onPressed,
        isNull,
      );
      debugPrint('STOP recovery fixture: flush replacement journal');
      expect(await pumpExternal(() => thread.flushDraft()), isTrue);
      debugPrint('STOP recovery fixture: close draft');
      await tester.runAsync(() async => panel().onClose!());
      await tester.pump();
      debugPrint('STOP recovery fixture: unmount Now');
      await unmount();
      debugPrint('STOP recovery fixture: remount Now');
      await mount();
      debugPrint('STOP recovery fixture: reopen draft');
      await openDraft();
      debugPrint('STOP recovery fixture: replacement journal restored');
      final reopened = panel().controller, reopenedMedia = editor().controller;
      expect(
        reopened.entries.last.materialReferences.single.operationID,
        rotatedID,
      );
      expect(
        reopened.entries[1].materialReferences.single.operationID,
        genuinelyMissing,
      );
      expect(reopenedMedia.images.single.stage!.mediaID, unifiedVideo);
      expect(reopenedMedia.images.single.rights, isNull);
      expect(panel().materialsReady, isFalse);
      reopenedMedia.setRights(0, ownRights());
      await tester.pump();
      await prepareViaEditor();
      expect(panel().materialsReady, isFalse);
      reopened.removeMissingMaterial(unifiedRemoved, genuinelyMissing);
      await tester.pump();
      expect(panel().materialsReady, isTrue);
      expect(
        tester
            .widget<FilledButton>(find.widgetWithText(FilledButton, '检查全部动态'))
            .onPressed,
        isNotNull,
      );
      expect(await pumpExternal(() => reopened.flushDraft()), isTrue);
      final saved = (await pumpExternal(
        () => journal.load(unifiedOwner, f.base),
      ))!;
      expect(
        saved.entries.last.materialReferences.single.operationID,
        rotatedID,
      );
      expect(saved.entries[1].materialReferences, isEmpty);
      expect(saved.entries.every((v) => v.media.isEmpty), isTrue);
      expect(
        calls.where(
          (v) => v.startsWith(
            'PUT /v1/me/moment-media-video-materials/operations/',
          ),
        ),
        hasLength(3),
      );
      expect(
        calls.where((v) => v.contains('/moment-threads/previews')),
        isEmpty,
      );
      expect(f.source.queries, isEmpty);
      expect(tester.takeException(), isNull);
    },
  );
  testWidgets(
    'one composer starts with one body, adds explicitly and keeps optional titles, locations and display identity independent',
    (tester) async {
      final values = <String, String>{};
      var calls = 0, selectedText = 'not selected';
      final client = MockClient((_) async {
        calls++;
        throw StateError('local editing must not publish');
      });
      final c = MomentThreadController(
        api: unifiedAPI(client),
        ownerID: unifiedOwner,
        journal: unifiedJournal(values),
      );
      final close = unifiedDisposeOnce(c);
      addTearDown(close);
      addTearDown(client.close);
      await c.begin(initialText: '第一行是正文\n第二行仍是正文');
      final first = c.entries.single.localID;
      expect(c.entries.single.title, isEmpty);
      expect(c.entries.single.input['title'], '');
      expect(c.entries.single.input['body'], c.activeText);
      expect(c.entries.single.locations, isEmpty);
      await tester.pumpWidget(
        unifiedPanel(c, onActiveText: (text) => selectedText = text),
      );
      expect(find.byType(TextField), findsNothing);
      expect(find.text('第1条 · 正在编辑'), findsOneWidget);
      expect(find.text('第2条'), findsNothing);
      await tester.ensureVisible(find.text('再接一条'));
      await tester.tap(find.text('再接一条'));
      await tester.pump();
      expect(c.entries, hasLength(2));
      expect(selectedText, isEmpty);
      expect(c.activeField, 'body');
      expect(c.entries.last.cityID, isEmpty);
      expect(c.entries.last.locations, isEmpty);
      c.updateActiveText('第二条只使用自己的位置');
      c.setActiveLocations([unifiedCity(unifiedImage, 'reykjavik-is')]);
      final second = c.entries.last.localID;
      c.selectEntry(0);
      c.setActiveLocations([
        unifiedCity(unifiedFirst, 'london-gb'),
        unifiedCity(unifiedRemoved, 'edinburgh-gb'),
      ]);
      c.selectField('title');
      c.updateActiveText('独立的可选标题');
      expect(c.activeText, '独立的可选标题');
      c.selectField('body');
      expect(c.activeText, '第一行是正文\n第二行仍是正文');
      c.updateActiveTitle('');
      expect(c.entries.first.input['title'], '');
      expect(c.entries.first.input['body'], '第一行是正文\n第二行仍是正文');
      expect(c.entries.first.locations!.map((v) => v.cityID), [
        'london-gb',
        'edinburgh-gb',
      ]);
      expect(c.entries.last.locations!.single.cityID, 'reykjavik-is');
      c.moveEntry(1, 0);
      expect(c.entries.map((v) => v.localID), [second, first]);
      expect(c.entries[c.activeIndex].localID, first);
      expect(c.activeText, '第一行是正文\n第二行仍是正文');
      await tester.pump();
      await tester.ensureVisible(find.byTooltip('删除第1条草稿'));
      await tester.tap(find.byTooltip('删除第1条草稿'));
      await tester.pump();
      expect(c.entries.single.localID, first);
      c.removeEntry(0);
      expect(c.entries, hasLength(1));
      expect(c.removedItems, isEmpty);
      expect(await c.flushDraft(), isTrue);
      expect(calls, 0);
      await tester.pumpWidget(const SizedBox());
      close();
      client.close();
    },
  );

  testWidgets(
    'six realistic UTF8 bodies restore beyond 16KiB while unresolved image and video operations remain blockers without saved rights or bytes',
    (tester) async {
      final values = <String, String>{}, store = unifiedJournal(values);
      var calls = 0;
      final client = MockClient((_) async {
        calls++;
        throw StateError('unprepared media must not send preview');
      });
      final c = MomentThreadController(
        api: unifiedAPI(client),
        ownerID: unifiedOwner,
        journal: store,
      );
      final close = unifiedDisposeOnce(c);
      addTearDown(close);
      addTearDown(client.close);
      final body = '汉' * 1000;
      expect(utf8.encode(body), hasLength(3000));
      await c.begin(initialText: body);
      for (var i = 1; i < 6; i++) {
        c.addEntry();
        c.updateActiveText(body);
      }
      c.rememberEntryMaterials(c.entries.first.localID, const [
        MomentDraftMaterialReference(unifiedImageOperation, 'IMAGE'),
      ]);
      c.rememberEntryMaterials(c.entries.last.localID, const [
        MomentDraftMaterialReference(unifiedVideoOperation, 'VIDEO'),
      ]);
      c.selectEntry(2);
      c.updateActiveTitle('恢复后仍独立的标题');
      expect(await c.flushDraft(), isTrue);
      expect(utf8.encode(values.values.single).length, greaterThan(16 * 1024));
      expect(
        utf8.encode(values.values.single).length,
        lessThan(momentThreadJournalMaxBytes),
      );
      final originalIDs = c.entries.map((v) => v.localID).toList();
      for (final forbidden in [
        'rights',
        'approval',
        'Bearer',
        'contentBase64',
        'thumbnailBase64',
      ]) {
        expect(values.values.single, isNot(contains(forbidden)));
      }
      close();
      final restored = MomentThreadController(
        api: unifiedAPI(client),
        ownerID: unifiedOwner,
        journal: store,
      );
      final closeRestored = unifiedDisposeOnce(restored);
      addTearDown(closeRestored);
      expect(await restored.restore(), isTrue);
      expect(restored.entries.map((v) => v.localID), originalIDs);
      expect(restored.entries.every((v) => v.text == body), isTrue);
      expect(restored.entries[2].title, '恢复后仍独立的标题');
      expect(restored.activeIndex, 2);
      expect(restored.entries.every((v) => v.media.isEmpty), isTrue);
      expect(restored.entries.first.materialReferences.single.kind, 'IMAGE');
      expect(
        restored.entries.last.materialReferences.single.operationID,
        unifiedVideoOperation,
      );
      await restored.preview();
      expect(restored.previewValue, isNull);
      expect(restored.pending, isNull);
      expect(restored.notice, contains('未完成材料不能忽略'));
      expect(calls, 0);
      await tester.pumpWidget(unifiedPanel(restored, materialsReady: false));
      expect(find.text('请先准备好各条媒体，或明确移除未完成的材料后再检查。'), findsOneWidget);
      final check = tester.widget<FilledButton>(
        find.widgetWithText(FilledButton, '检查全部动态'),
      );
      expect(check.onPressed, isNull);
      await tester.pumpWidget(const SizedBox());
      closeRestored();
      client.close();
    },
  );

  testWidgets(
    'owned EDIT binds original IDs, removal summary and mixed staged media then restores unknown using the original GET without another PUT',
    (tester) async {
      final pixels = (await tester.runAsync(
        () => File('test/fixtures/map-media/isolation-a.jpg').readAsBytes(),
      ))!;
      final pixelSHA = sha256.convert(pixels).toString();
      final values = <String, String>{}, store = unifiedJournal(values);
      final at = DateTime.now().toUtc(),
          expires = at.add(const Duration(seconds: 30));
      final rights = EntityCoverRights(
        basis: 'OWNED',
        attribution: 'Synthetic test fixture',
        licenseReference: '',
        retentionUntil: at.add(const Duration(days: 1)),
        canRetain: true,
        canPublish: true,
      );
      final calls = <String>[];
      final delivered = <MomentThreadReceipt>[];
      late MomentThreadController c;
      late MomentThreadInput requested;
      late MomentThreadInput withdrawal;
      Map<String, dynamic>? previewWire, originalFact;
      Map<String, dynamic> stage(String media, bool video) => {
        if (video) ...{
          'schemaVersion': 'public-moment-media-stage-v2', 'kind': 'VIDEO',
          'purpose': publicMomentVideoPurpose,
          // This is a typed server response fixture, not a claim that this
          // widget test ran FFmpeg or validated an actual MP4 payload.
          'video': {
            'mimeType': 'video/mp4',
            'durationMs': 4000,
            'codecVideo': 'h264',
            'codecAudio': 'aac',
            'pixelFormat': 'yuv420p',
            'videoSha256': 'f' * 64,
            'videoBytes': 1234,
            'videoPath': '/v1/me/moment-media-materials/$media/1/video',
          },
        },
        'mediaId': media,
        'mediaVersion': 1,
        'localScope': c.localScope,
        'displaySha256': pixelSHA,
        'displayBytes': pixels.length,
        'width': 320,
        'height': 180,
        'thumbnailSha256': pixelSHA,
        'thumbnailBytes': pixels.length,
        'validationKind': video
            ? publicMomentVideoValidation
            : publicMomentMediaValidation,
        'validationStatus': 'VALIDATED',
        'antivirusScan': 'NOT_RUN',
        'privacyRisk': 'UNKNOWN',
        'preparedAt': momentUTC(at),
        'retainUntil': momentUTC(at.add(const Duration(minutes: 10))),
      };
      Map<String, Object?> resolved(MomentLocationInput input, int index) => {
        ...input.toJson(),
        'position': index,
        'cityName': input.cityID,
        'placeName': '',
        'region': '',
        'countryCode': 'GB',
        'available': true,
        'sourceVersion': 'a' * 64,
        'observedAt': momentUTC(at),
        'validUntil': momentUTC(expires),
      };
      final client = MockClient((request) async {
        calls.add('${request.method} ${request.url.path}');
        if (request.method == 'POST') {
          expect(request.url.path, '/v1/me/moment-threads/previews');
          final raw = jsonDecode(request.body) as Map<String, dynamic>;
          if (raw['action'] == 'WITHDRAW') {
            withdrawal = MomentThreadInput(
              operationID: raw['operationId'],
              action: 'WITHDRAW',
              threadID: unifiedThread,
              expectedVersion: 5,
            );
            expect(raw, withdrawal.toJson());
            return unifiedResponse({
              'operationId': withdrawal.operationID,
              'action': 'WITHDRAW',
              'payloadDigest': withdrawal.digest,
              'observedAt': momentUTC(at),
              'expiresAt': momentUTC(expires),
              'approval': 'hw1.synthetic-withdraw-only',
              'input': raw,
              'sourceVersion': 'b' * 64,
              'members': originalFact!['members'],
              'materials': [],
              'contexts': [],
              'locations': [],
            });
          }
          requested = MomentThreadInput(
            operationID: raw['operationId'],
            action: 'EDIT',
            threadID: unifiedThread,
            expectedVersion: 4,
            localScope: raw['localScope'],
            entries: c.entries,
            removedItems: c.removedItems,
          );
          expect(raw, requested.toJson());
          // Go's struct field ordering differs from local draft ordering.
          final echoed = {
            ...raw,
            'items': [
              for (final rawItem in raw['items'])
                {
                  'input': {
                    'locations': rawItem['input']['locations'],
                    ...Map<String, dynamic>.from(rawItem['input']),
                  },
                  'media': rawItem['media'],
                  'restore': rawItem['restore'],
                  'expectedRevision': rawItem['expectedRevision'],
                  'momentId': rawItem['momentId'],
                },
            ],
          };
          previewWire = {
            'operationId': requested.operationID,
            'action': 'EDIT',
            'payloadDigest': requested.digest,
            'observedAt': momentUTC(at),
            'expiresAt': momentUTC(expires),
            'approval': 'hw1.synthetic-edit-only',
            'input': echoed,
            'sourceVersion': 'a' * 64,
            'members': [],
            'preservedMedia': [for (final _ in c.entries) null],
            'contexts': [
              for (final _ in c.entries)
                {'cityId': '', 'cityName': '', 'placeId': '', 'placeName': ''},
            ],
            'locations': [
              for (final entry in c.entries)
                [
                  for (var i = 0; i < entry.locations!.length; i++)
                    resolved(entry.locations![i], i),
                ],
            ],
            'materials': [
              for (final entry in c.entries)
                [
                  for (final media in entry.media)
                    {
                      'material': stage(
                        media.mediaID,
                        media.mediaID == unifiedVideo,
                      ),
                      'thumbnailBase64': base64Encode(pixels),
                    },
                ],
            ],
          };
          return unifiedResponse(previewWire!);
        }
        if (request.method == 'PUT') {
          final raw = jsonDecode(request.body) as Map<String, dynamic>;
          if (raw['action'] == 'WITHDRAW') {
            expect(
              request.url.path,
              '/v1/me/moment-threads/operations/${withdrawal.operationID}',
            );
            expect(raw['sourceVersion'], 'b' * 64);
            expect(raw['approval'], 'hw1.synthetic-withdraw-only');
            return unifiedResponse({
              'schemaVersion': 'moment-thread-operation-v2',
              'operationId': withdrawal.operationID,
              'status': 'SUCCEEDED',
              'action': 'WITHDRAW',
              'threadId': unifiedThread,
              'rootMomentId': unifiedFirst,
              'audience': 'PUBLIC',
              'expectedVersion': 5,
              'appliedVersion': 6,
              'payloadDigest': withdrawal.digest,
              'removedMembers': [],
              'members': [
                for (final member in originalFact!['members'])
                  {
                    ...Map<String, dynamic>.from(member),
                    'status': 'withdrawn',
                    'appliedRevision': member['appliedRevision'] + 1,
                    'media': [],
                  },
              ],
              'committedAt': momentUTC(at),
            });
          }
          expect(raw['sourceVersion'], 'a' * 64);
          expect(raw['approval'], 'hw1.synthetic-edit-only');
          expect(
            request.url.path,
            '/v1/me/moment-threads/operations/${requested.operationID}',
          );
          originalFact = {
            'schemaVersion': 'moment-thread-operation-v2',
            'operationId': requested.operationID,
            'status': 'SUCCEEDED',
            'action': 'EDIT',
            'threadId': unifiedThread,
            'rootMomentId': unifiedFirst,
            'audience': 'PUBLIC',
            'expectedVersion': 4,
            'appliedVersion': 5,
            'payloadDigest': requested.digest,
            'members': [
              for (var i = 0; i < requested.entries.length; i++)
                {
                  'momentId': requested.entries[i].momentID.isNotEmpty
                      ? requested.entries[i].momentID
                      : requested.entries[i].media.single.mediaID ==
                            unifiedVideo
                      ? unifiedVideoMoment
                      : unifiedImageMoment,
                  'status': 'published',
                  'position': i,
                  'appliedRevision': requested.entries[i].momentID.isNotEmpty
                      ? requested.entries[i].expectedRevision + 1
                      : 2,
                  'mediaSetVersion': requested.entries[i].media.isEmpty ? 0 : 1,
                  'media': [
                    for (final media in requested.entries[i].media)
                      {'mediaId': media.mediaID, 'mediaVersion': media.version},
                  ],
                },
            ],
            'removedMembers': [
              {
                'momentId': unifiedRemoved,
                'status': 'withdrawn',
                'position': 1,
                'appliedRevision': 6,
                'mediaSetVersion': 0,
                'media': [],
              },
            ],
            'committedAt': momentUTC(at),
          };
          // The original server fact exists but its successful response was lost.
          return http.Response(
            '{}',
            503,
            headers: {'content-type': 'application/json'},
          );
        }
        expect(request.method, 'GET');
        if (request.url.path == '/v1/moment-threads/$unifiedThread') {
          return unifiedResponse({
            'schemaVersion': 'moment-thread-view-v2',
            'threadId': unifiedThread,
            'rootMomentId': unifiedFirst,
            'audience': 'PUBLIC',
            'version': 5,
            'canWithdraw': true,
            'sourceVersion': 'b' * 64,
            'observedAt': momentUTC(at),
            'validUntil': momentUTC(expires),
            'members': [
              for (final member in originalFact!['members'])
                {
                  'position': member['position'],
                  'momentId': member['momentId'],
                  'revision': member['appliedRevision'],
                  'status': 'AVAILABLE',
                },
            ],
          });
        }
        expect(
          request.url.path,
          '/v1/me/moment-threads/operations/${requested.operationID}',
        );
        return unifiedResponse(originalFact!);
      });
      c = MomentThreadController(
        api: unifiedAPI(client),
        ownerID: unifiedOwner,
        journal: store,
      );
      final close = unifiedDisposeOnce(c);
      addTearDown(close);
      addTearDown(client.close);
      MomentThreadDraftEntry original(String id, int revision, String body) =>
          MomentThreadDraftEntry(
            localID: id,
            momentID: id,
            expectedRevision: revision,
            cityID: '',
            text: body,
            title: '原可选标题',
            locations: const [],
            originalStatus: 'published',
            preserveMedia: true,
            originalInput: {
              'cityId': '',
              'placeId': '',
              'title': '原可选标题',
              'body': body,
              'occurredAt': null,
              'timePrecision': 'unknown',
              'locationPrecision': 'none',
              if (id == unifiedFirst) ...{
                'activityId': unifiedActivity,
                'communityId': unifiedCommunity,
                'organizationId': unifiedOrganization,
              },
            },
          );
      final owned = OwnedMomentThread(
        unifiedThread,
        unifiedFirst,
        'PUBLIC',
        'published',
        4,
        [
          {
            'position': 0,
            'momentId': unifiedFirst,
            'revision': 7,
            'status': 'AVAILABLE',
          },
          {
            'position': 1,
            'momentId': unifiedRemoved,
            'revision': 5,
            'status': 'AVAILABLE',
          },
        ],
        const [],
        const [],
        'a' * 64,
        expires,
      );
      expect(
        await c.beginOwned(owned, [
          original(unifiedFirst, 7, '原第一条正文'),
          original(unifiedRemoved, 5, '待移除的原正文'),
        ]),
        isTrue,
      );
      c.updateActiveText('修改后的原第一条正文');
      c.updateActiveTitle('');
      expect(c.entries.first.input['title'], '');
      c.removeEntry(1);
      expect(c.removedItems, [
        {'momentId': unifiedRemoved, 'expectedRevision': 5},
      ]);
      expect(c.removedEntries.single.text, '待移除的原正文');
      expect(c.removedEntries.single.title, '原可选标题');
      c.addEntry();
      c.setActiveMedia([PublicMomentMediaSelection(unifiedImage, 1, rights)]);
      c.rememberEntryMaterials(c.entries.last.localID, const [
        MomentDraftMaterialReference(unifiedImageOperation, 'IMAGE'),
      ]);
      c.setActiveLocations([unifiedCity(unifiedImage, 'london-gb')]);
      c.addEntry();
      c.setActiveMedia([PublicMomentMediaSelection(unifiedVideo, 1, rights)]);
      c.rememberEntryMaterials(c.entries.last.localID, const [
        MomentDraftMaterialReference(unifiedVideoOperation, 'VIDEO'),
      ]);
      c.setActiveLocations([unifiedCity(unifiedVideo, 'edinburgh-gb')]);
      c.moveEntry(2, 0);
      expect(c.entries[1].momentID, unifiedFirst);
      await c.preview();
      final approved = c.previewValue;
      expect(approved, isNotNull, reason: c.notice);
      expect(approved!.input.entries.first.text, isEmpty);
      expect(approved.materials.first.single['material']['kind'], 'VIDEO');
      expect(
        approved.materials.last.single['material']['validationKind'],
        publicMomentMediaValidation,
      );
      expect(approved.locations.first.single.cityID, 'edinburgh-gb');
      expect(approved.locations.last.single.cityID, 'london-gb');
      expect(
        approved.input.entries[1].input,
        containsPair('activityId', unifiedActivity),
      );
      expect(
        approved.input.entries[1].input,
        containsPair('communityId', unifiedCommunity),
      );
      expect(
        approved.input.entries[1].input,
        containsPair('organizationId', unifiedOrganization),
      );
      expect(approved.input.entries[1].input['locations'], isEmpty);
      expect(previewWire!['payloadDigest'], approved.input.digest);
      expect(
        momentHash(previewWire!['input']),
        isNot(momentHash(approved.input.toJson())),
      );
      final changed =
          jsonDecode(jsonEncode(previewWire)) as Map<String, dynamic>;
      changed['input']['items'][1]['input']['body'] = '未经批准的新正文';
      expect(
        () => MomentThreadPreview.decode(changed, approved.input),
        throwsFormatException,
      );
      final changedContext =
          jsonDecode(jsonEncode(previewWire)) as Map<String, dynamic>;
      changedContext['input']['items'][1]['input']['activityId'] =
          unifiedOrganization;
      expect(
        () => MomentThreadPreview.decode(changedContext, approved.input),
        throwsFormatException,
      );
      final savedBeforeCommit = jsonDecode(values.values.single) as Map;
      final savedOriginal = (savedBeforeCommit['entries'] as List).singleWhere(
        (dynamic entry) => entry['momentId'] == unifiedFirst,
      );
      expect(
        (savedOriginal['originalInput'] as Map).containsKey('body'),
        isFalse,
      );
      expect(values.values.single, isNot(contains('hw1.synthetic-edit-only')));
      expect(values.values.single, isNot(contains('rights')));
      expect(values.values.single, isNot(contains('thumbnailBase64')));
      await tester.pumpWidget(unifiedPanel(c, onPublished: delivered.add));
      await tester.pump();
      expect(find.byType(Image), findsNWidgets(2));
      await c.confirm();
      await tester.pump();
      expect(c.uncertain, isTrue);
      expect(c.pending!.stage, 'UNKNOWN');
      expect(delivered, isEmpty);
      final operation = c.pending!.operationID;
      expect(calls.where((v) => v.startsWith('PUT ')), hasLength(1));
      await tester.pumpWidget(const SizedBox());
      close();
      final recovered = MomentThreadController(
        api: unifiedAPI(client),
        ownerID: unifiedOwner,
        journal: store,
      );
      final closeRecovered = unifiedDisposeOnce(recovered);
      addTearDown(closeRecovered);
      expect(await recovered.restore(), isTrue);
      expect(recovered.pending!.operationID, operation);
      expect(recovered.entries.every((v) => v.media.isEmpty), isTrue);
      expect(recovered.removedEntries.single.momentID, unifiedRemoved);
      await tester.pumpWidget(
        unifiedPanel(recovered, onPublished: delivered.add),
      );
      await recovered.recover();
      await tester.pump();
      expect(recovered.pending, isNull);
      expect(recovered.receipt!.rootMomentID, unifiedFirst);
      expect(recovered.entries.map((v) => v.momentID), [
        unifiedVideoMoment,
        unifiedFirst,
        unifiedImageMoment,
      ]);
      expect(recovered.entries[1].title, '');
      expect(recovered.removedItems, isEmpty);
      expect(recovered.removedEntries, isEmpty);
      expect(calls.where((v) => v.startsWith('PUT ')), hasLength(1));
      expect(calls.where((v) => v.startsWith('GET ')), [
        'GET /v1/me/moment-threads/operations/$operation',
      ]);
      expect(delivered, hasLength(1));
      expect(delivered.single.removedMembers.single.momentID, unifiedRemoved);
      // Whole withdrawal applies returned revisions in one journal write; an
      // unsubmitted next entry and its original stage selectors remain local.
      final retainedText = recovered.entries.map((v) => v.text).toList();
      final retainedTitle = recovered.entries.map((v) => v.title).toList();
      recovered.addEntry();
      recovered.updateActiveText('尚未发布的下一条正文');
      recovered.rememberEntryMaterials(recovered.entries.last.localID, const [
        MomentDraftMaterialReference(unifiedVideoOperation, 'VIDEO'),
      ]);
      await recovered.previewWithdraw(unifiedThread);
      expect(recovered.previewValue, isNotNull, reason: recovered.notice);
      await recovered.confirm();
      await tester.pump();
      final withdrawn = recovered.receipt!;
      expect(withdrawn.action, 'WITHDRAW');
      expect(recovered.pending, isNull);
      expect(recovered.ownedThreadVersion, withdrawn.appliedVersion);
      for (final member in withdrawn.members) {
        final entry = recovered.entries.singleWhere(
          (v) => v.momentID == member.momentID,
        );
        expect(entry.expectedRevision, member.revision);
        expect(entry.originalStatus, 'withdrawn');
        expect(entry.preserveMedia, isFalse);
        expect(entry.locations, isEmpty);
        expect(entry.media, isEmpty);
      }
      expect(recovered.entries.take(3).map((v) => v.text), retainedText);
      expect(recovered.entries.take(3).map((v) => v.title), retainedTitle);
      expect(recovered.entries.last.originalStatus, 'draft');
      expect(recovered.entries.last.text, '尚未发布的下一条正文');
      expect(
        recovered.entries.last.materialReferences.single.operationID,
        unifiedVideoOperation,
      );
      final savedWithdrawn = (await store.load(
        unifiedOwner,
        recovered.origin,
      ))!;
      expect(savedWithdrawn.pending, isNull);
      expect(savedWithdrawn.threadVersion, withdrawn.appliedVersion);
      expect(
        savedWithdrawn.entries.map(
          (v) => [v.momentID, v.expectedRevision, v.originalStatus],
        ),
        recovered.entries.map(
          (v) => [v.momentID, v.expectedRevision, v.originalStatus],
        ),
      );
      expect(
        savedWithdrawn.entries.last.materialReferences.single.operationID,
        unifiedVideoOperation,
      );
      expect(
        calls.where(
          (v) => v == 'PUT /v1/me/moment-threads/operations/$operation',
        ),
        hasLength(1),
      );
      expect(calls.where((v) => v.startsWith('PUT ')), hasLength(2));
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      closeRecovered();
      client.close();
    },
  );
}
