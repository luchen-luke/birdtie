import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/now_composer_draft_state.dart';
import 'package:birdtie_client/src/workspace/typed_local_draft.dart';
import 'package:birdtie_client/src/workspace/voice_input.dart';
import 'package:birdtie_client/src/workspace/voice_input_consent.dart';
import 'typed_local_draft_test.dart' show DraftMemoryStore;

const owner = '81000000-0000-4000-8000-000000000001';
const moment = '81000000-0000-4000-8000-000000000002';
const parent = '81000000-0000-4000-8000-000000000003';
const field = '81000000-0000-4000-8000-000000000004';

class _NoVoice implements VoiceInputPort {
  @override
  Future<VoiceAvailability> availability() async => const VoiceAvailability();
  @override
  void dispose() {}
  @override
  dynamic noSuchMethod(Invocation invocation) =>
      throw StateError('Voice capture not part of this test');
}

void main() {
  test(
    'comment, reply and Journey fields persist independently without approval or routing authority',
    () async {
      final store = DraftMemoryStore();
      final d = NowComposerDraftState(store: store)..observeOwner(owner);
      d.configurePersistence('https://composer.synthetic.invalid');
      await d.restore();
      d.update(const TextEditingValue(text: '原 Now 草稿'));
      d.select(const NowComposerTarget.comment(contentID: moment));
      d.update(const TextEditingValue(text: '顶层评论'));
      final old = d.identity, version = d.contentVersion;
      d.select(
        const NowComposerTarget.comment(contentID: moment, replyID: parent),
      );
      d.update(const TextEditingValue(text: '明确回复'));
      d.clearSent(old, expectedVersion: version);
      expect(d.value.text, '明确回复');
      d.select(
        const NowComposerTarget.edit(
          mode: 'edit-journey',
          contentID: moment,
          replyID: field,
        ),
      );
      expect(d.value.text, isEmpty);
      d.update(const TextEditingValue(text: '我的旅程字段'));
      await d.flush();
      d.dispose();
      final reloaded = NowComposerDraftState(store: store)..observeOwner(owner);
      reloaded.configurePersistence('https://composer.synthetic.invalid');
      await reloaded.restore();
      expect(reloaded.target.agent, isTrue);
      expect(reloaded.value.text, '原 Now 草稿');
      reloaded.select(const NowComposerTarget.comment(contentID: moment));
      expect(reloaded.value.text, '顶层评论');
      reloaded.select(
        const NowComposerTarget.comment(contentID: moment, replyID: parent),
      );
      expect(reloaded.value.text, '明确回复');
      reloaded.select(
        const NowComposerTarget.edit(
          mode: 'edit-journey',
          contentID: moment,
          replyID: field,
        ),
      );
      expect(reloaded.value.text, '我的旅程字段');
      final wire = TypedLocalDraft.fromJson(reloaded.envelope!.toJson());
      expect(wire.slotID, endsWith(':$field'));
      reloaded.observeWorkspace('another-workspace');
      expect(reloaded.target.agent, isTrue);
      reloaded.select(const NowComposerTarget.comment(contentID: moment));
      expect(reloaded.target.agent, isTrue);
      reloaded.observeWorkspace(null);
      expect(reloaded.target.agent, isTrue);
      reloaded.observeOwner('another-owner');
      expect(reloaded.value.text, isEmpty);
      reloaded.dispose();
    },
  );
  testWidgets(
    'one composer shows explicit target, preserves drafts and blocks readonly IME send',
    (t) async {
      final workspace = AgentWorkspaceController();
      final drafts = NowComposerDraftState()..observeOwner(owner);
      final sent = <String>[];
      var readonly = false, allowEmpty = false;
      drafts.update(const TextEditingValue(text: '原 Now 草稿'));
      Future<void> paint() async {
        await t.pumpWidget(
          MaterialApp(
            theme: birdtieTheme(Brightness.light),
            home: Scaffold(
              body: AgentComposer(
                workspace: workspace,
                onSubmit: sent.add,
                onSearchArea: () {},
                hasSearchArea: false,
                draftIdentity: drafts.identity,
                draftValue: drafts.value,
                onDraftChanged: drafts.update,
                contentLabel: drafts.target.content
                    ? (allowEmpty ? '编辑我的旅程 · 备注 · 私人草稿' : '评论《当前原动态》 · 公开可见')
                    : null,
                contentInputHint: drafts.target.content ? '写评论…' : null,
                contentSubmitLabel: allowEmpty ? '更新私人草稿' : '发布评论',
                inputReadOnly: readonly,
                allowEmptyContent: allowEmpty,
                maxDraftBytes: 2000,
                voiceInputPort: _NoVoice(),
                voiceConsentStore: MemoryVoiceInputConsentStore(),
              ),
            ),
          ),
        );
        await t.pump();
      }

      await paint();
      drafts.select(const NowComposerTarget.comment(contentID: moment));
      await paint();
      expect(find.byType(TextField), findsOneWidget);
      expect(
        t.widget<TextField>(find.byType(TextField)).controller!.text,
        isEmpty,
      );
      expect(
        t.widget<TextField>(find.byType(TextField)).focusNode!.hasFocus,
        isFalse,
      );
      expect(find.text('评论《当前原动态》 · 公开可见'), findsOneWidget);
      await t.enterText(find.byType(TextField), '这条动态的评论');
      await t.pump();
      await t.tap(find.byTooltip('发布评论'));
      await t.pump();
      expect(sent, ['这条动态的评论']);
      expect(
        drafts.value.text,
        '这条动态的评论',
        reason: 'A tap is not a success receipt',
      );
      readonly = true;
      await paint();
      expect(
        t
            .widget<IconButton>(
              find.byKey(const Key('now-composer-primary-action')),
            )
            .onPressed,
        isNull,
      );
      t.widget<TextField>(find.byType(TextField)).onSubmitted?.call('旧回调');
      await t.pump();
      expect(sent, hasLength(1));
      readonly = false;
      drafts.select(const NowComposerTarget.agent());
      await paint();
      expect(
        t.widget<TextField>(find.byType(TextField)).controller!.text,
        '原 Now 草稿',
      );
      allowEmpty = true;
      drafts.select(
        const NowComposerTarget.edit(
          mode: 'edit-journey',
          contentID: moment,
          replyID: field,
        ),
      );
      await paint();
      await t.tap(find.byTooltip('更新私人草稿'));
      await t.pump();
      expect(sent.last, '');
      await t.pumpWidget(const SizedBox.shrink());
      drafts.dispose();
      workspace.dispose();
    },
  );
}
