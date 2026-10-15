import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:birdtie_client/src/content/moment_control.dart';
import 'package:birdtie_client/src/workspace/moment_thread_api.dart';
import 'package:birdtie_client/src/workspace/now_creation_draft.dart';
import 'package:birdtie_client/src/workspace/now_creation_panel.dart';
import 'package:birdtie_client/src/workspace/public_moment_api.dart';
import 'package:birdtie_client/src/workspace/public_moment_media.dart';
import 'entity_cover_test.dart' show coverJPEG, coverRights;
import 'moment_control_test.dart';

Map<String, Object?> optionalInput({String body = ''}) => {
  'cityId': '',
  'placeId': '',
  'title': '',
  'body': body,
  'timePrecision': 'unknown',
  'locationPrecision': 'none',
};

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'body and history survive without inferred title or exploration city',
    () {
      const text = '第一行是正文\n第二行仍是正文';
      final draft = const NowCreationDraft(
        kind: 'moment',
        cityID: '',
        cityName: '',
      ).withText(text);
      final restored = NowCreationDraft.fromJson(draft.toJson());
      expect(restored.title, isEmpty);
      expect(restored.body, text);
      expect(restored.cityID, isEmpty);
      final historical = draft.copyWith(title: '作者原有标题').withText('只改正文');
      expect(historical.title, '作者原有标题');
      expect(historical.body, '只改正文');
      final entry = MomentThreadDraftEntry(
        localID: mcOwner,
        cityID: '',
        text: text,
      );
      expect(entry.toItem()['input'], {
        ...optionalInput(body: text),
        'occurredAt': null,
      });
      final now = DateTime.now().toUtc();
      final public = PublicMoment.fromJson({
        'schemaVersion': 'public-moment-v4',
        'id': mcTarget,
        'title': '',
        'body': text,
        'cityId': '',
        'cityName': '',
        'placeId': '',
        'placeName': '',
        'locationPrecision': 'none',
        'revision': 2,
        'publishedAt': now.toIso8601String(),
        'authorId': null,
        'authorPublicProfile': null,
        'photo': null,
        'observedAt': now.toIso8601String(),
        'validUntil': now.add(const Duration(seconds: 30)).toIso8601String(),
      }, mcTarget);
      expect(public.title, isEmpty);
      expect(public.body, text);
      expect(public.locationPrecision, 'none');
      expect(public.cityID, isEmpty);
    },
  );

  test(
    'one atomic media approval seals actual material and keeps old digest compatible',
    () {
      final legacy = MomentControlInput(
        operationID: mcOp,
        action: 'CREATE',
        expectedRevision: 0,
        input: mcInput(),
      );
      expect(
        legacy.digest,
        '653411493f706ea41ea76b54362a954d601dcec6dbe4cd86a2f088660dded516',
      );
      final bytes = coverJPEG(), rights = coverRights();
      final selection = PublicMomentMediaSelection(mcOwner, 1, rights);
      final input = MomentControlInput(
        operationID: mcOp,
        action: 'PUBLISH',
        momentID: mcTarget,
        expectedRevision: 1,
        input: optionalInput(),
        media: [selection],
        localScope: mcOp,
      );
      final now = DateTime.now().toUtc();
      final raw = mcPreview(input);
      raw['materials'] = [
        {
          'material': {
            'mediaId': mcOwner,
            'mediaVersion': 1,
            'localScope': mcOp,
            'displaySha256': sha256.convert(bytes).toString(),
            'displayBytes': bytes.length,
            'width': 320,
            'height': 180,
            'thumbnailSha256': sha256.convert(bytes).toString(),
            'thumbnailBytes': bytes.length,
            'validationKind': publicMomentMediaValidation,
            'validationStatus': 'VALIDATED',
            'antivirusScan': 'NOT_RUN',
            'privacyRisk': 'UNKNOWN',
            'preparedAt': now.toIso8601String(),
            'retainUntil': now
                .add(const Duration(minutes: 10))
                .toIso8601String(),
          },
          'thumbnailBase64': base64Encode(bytes),
        },
      ];
      final preview = MomentControlPreview.fromJson(raw, input);
      expect(preview.materials.single.thumbnail, bytes);
      expect(preview.materials.single.stage.localScope, mcOp);
      expect(preview.body['media'], [selection.toJson()]);
      expect(preview.live, isTrue);
      (raw['materials'] as List).single['material']['mediaVersion'] = 2;
      expect(
        () => MomentControlPreview.fromJson(raw, input),
        throwsFormatException,
      );
      expect(
        () => MomentControlInput(
          operationID: mcOp,
          action: 'PUBLISH',
          momentID: mcTarget,
          expectedRevision: 1,
          input: optionalInput(),
          media: const [],
        ),
        throwsFormatException,
      );
      final keep = MomentControlInput(
        operationID: mcOp,
        action: 'UPDATE_PUBLIC',
        momentID: mcTarget,
        expectedRevision: 2,
        input: optionalInput(body: '继续公开的正文'),
      );
      final remove = MomentControlInput(
        operationID: mcOp,
        action: 'UPDATE_PUBLIC',
        momentID: mcTarget,
        expectedRevision: 2,
        input: optionalInput(body: '继续公开的正文'),
        media: const [],
      );
      expect(keep.digest, isNot(remove.digest));
    },
  );

  testWidgets(
    'Now material preview has no title field, fake body or required location',
    (tester) async {
      var titleEdits = 0;
      void nothing() {}
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: NowCreationPanel(
              draft: const NowCreationDraft(
                kind: 'moment',
                cityID: '',
                cityName: '',
              ),
              onToggle: nothing,
              onClose: nothing,
              onEditTitle: () => titleEdits++,
              onEditBody: nothing,
              onChoosePlace: nothing,
              onChooseTime: nothing,
              onSave: nothing,
              mediaPreview: const SizedBox(
                key: ValueKey('selected-real-material-slot'),
                height: 40,
              ),
            ),
          ),
        ),
      );
      expect(
        find.byKey(const ValueKey('selected-real-material-slot')),
        findsOneWidget,
      );
      expect(find.byType(TextField), findsNothing);
      expect(find.byTooltip('修改标题'), findsNothing);
      expect(find.text('未命名动态'), findsNothing);
      expect(find.textContaining('未记录'), findsNothing);
      expect(find.text('添加位置'), findsOneWidget);
      expect(find.text('在下方输入框写下动态内容。'), findsNothing);
      expect(titleEdits, 0);
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );
}
