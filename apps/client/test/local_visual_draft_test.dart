import 'package:birdtie_client/src/workspace/local_visual_draft.dart';
import 'package:flutter_test/flutter_test.dart';

LocalVisualSourceSelection localVisualSelection({
  int revision = 2,
  String owner = '85000000-0000-4000-8000-000000000001',
  String origin = 'https://synthetic.test',
}) => LocalVisualSourceSelection(
  ownerID: owner,
  origin: origin,
  sourceContextType: 'CITY',
  sourceContextID: 'aberdeen-gb',
  clientDraftID: '85000000-0000-4000-8000-000000000002',
  clientDraftRevision: revision,
  selectedImageSHA256: 'a' * 64,
);
LocalVisualReportProvenance localVisualProvenance(
  LocalVisualSourceSelection s,
) => LocalVisualReportProvenance(
  selectionBindingKey: s.bindingKey,
  operationID: '85000000-0000-4000-8000-000000000003',
  previewID: '85000000-0000-4000-8000-000000000004',
  previewVersion: 1,
  pixelID: '85000000-0000-4000-8000-000000000005',
  pixelRevision: 1,
  pixelSHA256: 'b' * 64,
  reportSHA256: 'c' * 64,
  unknowns: ['拍摄地点未知', '拍摄时间未知'],
);
void main() {
  test(
    'local own question carries exact personal selection without manufacturing an observation',
    () {
      final s = localVisualSelection(),
          draft = LocalVisualDraft.ownQuestion(
            text: ' 请问图上文字是什么？ ',
            sourceSelection: s,
          );
      expect(draft.text, '请问图上文字是什么？');
      expect(draft.status, LocalVisualDraftStatus.ownQuestion);
      expect(draft.includesUnverifiedObservation, isFalse);
      expect(draft.reportProvenance, isNull);
      expect(
        s.matches(
          '85000000-0000-4000-8000-000000000001',
          'https://synthetic.test',
        ),
        isTrue,
      );
      expect(
        s.matches(
          '85000000-0000-4000-8000-000000000009',
          'https://synthetic.test',
        ),
        isFalse,
      );
      expect(
        LocalVisualDraft.ownQuestion(
          text: '我修改后的问题',
          sourceSelection: s,
        ).contentVersion,
        isNot(draft.contentVersion),
      );
      expect(
        () => LocalVisualDraft.ownQuestion(text: ' ', sourceSelection: s),
        throwsFormatException,
      );
    },
  );
  test(
    'report excerpt always remains unverified with true original unknowns separate from own question',
    () {
      final s = localVisualSelection(),
          p = localVisualProvenance(localVisualSelection());
      final draft = LocalVisualDraft.reportNote(
        text: '本人改写的观察摘录',
        sourceSelection: s,
        reportProvenance: p,
      );
      expect(draft.status, LocalVisualDraftStatus.unverifiedReportNote);
      expect(draft.text, startsWith('未核验的图片观察摘录'));
      expect(draft.text, contains('不是地点事实、到访证据或记忆权威'));
      expect(draft.text, contains('拍摄地点未知'));
      expect(draft.text, contains('拍摄时间未知'));
      expect(draft.text, isNot(contains('请问图上文字是什么')));
      expect(draft.text, isNot(contains(p.operationID)));
      expect(draft.text, isNot(contains(p.pixelSHA256)));
      expect(
        () => LocalVisualDraft.reportNote(
          text: '摘录',
          sourceSelection: localVisualSelection(revision: 3),
          reportProvenance: p,
        ),
        throwsFormatException,
      );
    },
  );
  test(
    'local metadata rejects foreign origin missing real owner or altered source revision',
    () {
      for (final origin in [
        '',
        'https://synthetic.test/path',
        'https://user@synthetic.test',
      ]) {
        expect(
          () => localVisualSelection(origin: origin),
          throwsFormatException,
        );
      }
      expect(
        () => localVisualSelection(owner: 'Bearer fake'),
        throwsFormatException,
      );
      expect(() => localVisualSelection(revision: 0), throwsFormatException);
      expect(
        localVisualSelection(revision: 3).bindingKey,
        isNot(localVisualSelection().bindingKey),
      );
      expect(
        () => LocalVisualDraft.ownQuestion(
          text: '中' * 3000,
          sourceSelection: localVisualSelection(),
        ),
        throwsFormatException,
      );
    },
  );
}
