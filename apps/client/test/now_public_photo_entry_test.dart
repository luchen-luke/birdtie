import 'package:birdtie_client/src/workspace/public_moment_photo_editor.dart';
import 'package:birdtie_client/src/workspace/now_creation_panel.dart';
import 'package:birdtie_client/src/workspace/now_creation_draft.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'entity_cover_test.dart' show coverJPEG, coverSettle;
import 'public_moment_photo_controller_test.dart' show PhotoFixture;
import 'public_moment_photo_editor_test.dart'
    show photoTap, photoPaintedApproval;
import 'package:birdtie_client/src/workspace/public_moment_photo_pending_store.dart';

void main() {
  testWidgets(
    'Now inline image loads local material without upload or inherited approval',
    (t) async {
      final f = PhotoFixture()..unavailable = true;
      final controller = f.controller();
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: NowCreationPanel(
              draft: const NowCreationDraft(
                kind: 'moment',
                cityID: 'aberdeen-gb',
                cityName: '阿伯丁',
                title: '本人公开文字',
              ),
              onToggle: () {},
              onClose: () {},
              onEditTitle: () {},
              onEditBody: () {},
              onChoosePlace: () {},
              onChooseTime: () {},
              onSave: () {},
              extraContent: PublicMomentPhotoEditor(
                controller: controller,
                ownsController: false,
                embedded: true,
                allowImageSelection: false,
                momentTitleRevision: 2,
                initialImage: coverJPEG(),
              ),
            ),
          ),
        ),
      );
      await coverSettle(t);
      expect(controller.localBytes, isNull);
      f.unavailable = false;
      await photoTap(t, find.text('重新读取当前状态'));
      expect(controller.localBytes, isNotNull);
      expect(f.previews, 0);
      expect(f.puts, 0);
      expect(find.byKey(const Key('public-photo-select')), findsNothing);
      expect(
        t
            .widget<CheckboxListTile>(
              find.byKey(const Key('public-photo-publish')),
            )
            .value,
        isFalse,
      );
      expect(
        t
            .widget<CheckboxListTile>(
              find.byKey(const Key('public-photo-retain')),
            )
            .value,
        isFalse,
      );
      await t.pumpWidget(const SizedBox());
      expect(controller.current, isTrue);
      controller.dispose();
      f.changes.dispose();
    },
  );
  testWidgets(
    'inline image confirms the prepared derivative once and reports exact original receipt',
    (t) async {
      final f = PhotoFixture(), c = f.controller();
      PublicMomentPhotoPending? prepared;
      String? completed;
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: SingleChildScrollView(
              child: PublicMomentPhotoEditor(
                controller: c,
                embedded: true,
                ownsController: false,
                allowImageSelection: false,
                initialImage: coverJPEG(),
                momentTitleRevision: 2,
                onPrepared: (p) =>
                    prepared = PublicMomentPhotoPending.preview(p),
                onResolved: (p, r) {
                  expect(prepared?.same(p), isTrue);
                  completed = r.operationID;
                },
              ),
            ),
          ),
        ),
      );
      await coverSettle(t);
      await t.ensureVisible(find.byKey(const Key('public-photo-attribution')));
      await t.enterText(
        find.byKey(const Key('public-photo-attribution')),
        '合成测试素材',
      );
      await t.ensureVisible(find.byKey(const Key('public-photo-retention')));
      await t.enterText(
        find.byKey(const Key('public-photo-retention')),
        '2030-01-02',
      );
      t.testTextInput.hide();
      await t.pump();
      await photoTap(t, find.byKey(const Key('public-photo-retain')));
      await photoTap(t, find.byKey(const Key('public-photo-publish')));
      await photoPaintedApproval(t);
      expect(f.puts, 0);
      expect(prepared, isNotNull);
      await photoTap(t, find.widgetWithText(FilledButton, '确认公开图片'));
      expect(f.puts, 1);
      expect(completed, prepared!.operationID);
      await t.pumpWidget(const SizedBox());
      c.dispose();
      f.dispose();
    },
  );
}
