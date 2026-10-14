import 'dart:io';
import 'dart:typed_data';
import 'package:birdtie_client/src/workspace/local_draft_context_page.dart';
import 'package:birdtie_client/src/workspace/now_composer_draft_state.dart';
import 'package:birdtie_client/src/workspace/typed_local_draft.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'typed_local_draft_test.dart' show DraftMemoryStore;
import 'batch_a_visual_test.dart' show visualTheme, capture, captureKey;

void main() {
  final capturing =
      Platform.environment['BIRDTIE_TYPED_CONTEXT_CAPTURE'] == '1';
  setUpAll(() async {
    if (!capturing) return;
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
  });
  Future<NowComposerDraftState> mount(
    WidgetTester t,
    List<LocalDraftReferenceCandidate> Function() read,
  ) async {
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final d = NowComposerDraftState(store: DraftMemoryStore())
      ..observeOwner('owner');
    d.configurePersistence('https://typed-reference-fixture.test');
    d.select(
      NowComposerTarget.human(
        const HumanConversation(
          id: 'actual-conversation',
          otherAccountId: 'actual-peer',
          otherName: '实际收件人',
        ),
      ),
    );
    d.update(const TextEditingValue(text: '本人未发送文字'));
    await t.pumpWidget(
      RepaintBoundary(
        key: captureKey,
        child: MaterialApp(
          debugShowCheckedModeBanner: false,
          theme: capturing ? visualTheme() : null,
          builder: (context, child) => Stack(
            children: [
              child!,
              if (capturing)
                const Positioned(
                  left: 0,
                  right: 0,
                  bottom: 0,
                  child: Material(
                    color: Colors.black,
                    child: Padding(
                      padding: EdgeInsets.all(5),
                      child: Text(
                        '隔离合成草稿 · 非真实人物/来源授权验收',
                        textAlign: TextAlign.center,
                        style: TextStyle(color: Colors.white, fontSize: 10),
                      ),
                    ),
                  ),
                ),
            ],
          ),
          home: LocalDraftContextPage(drafts: d, candidates: read),
        ),
      ),
    );
    return d;
  }

  testWidgets(
    'typed reference selection never rebinds actual recipient or stores source title',
    (t) async {
      final candidate = LocalDraftReferenceCandidate(
        kind: 'person',
        id: 'referenced-person',
        title: '当前获准姓名',
        version: 'current-v1',
        expiresAt: DateTime.now().toUtc().add(const Duration(seconds: 30)),
        sourceCurrent: () => true,
      );
      final d = await mount(t, () => [candidate]);
      final id = d.envelope!.id, recipient = d.target.key;
      await t.tap(find.text('添加当前引用'));
      await t.pumpAndSettle();
      await t.tap(find.text('当前获准姓名'));
      await t.pumpAndSettle();
      expect(d.envelope!.id, id);
      expect(d.target.key, recipient);
      expect(d.envelope!.recipientID, 'actual-peer');
      expect(d.envelope!.references.single.id, 'referenced-person');
      expect(
        d.envelope!.references.single.toJson().values,
        isNot(contains('当前获准姓名')),
      );
      expect(d.value.text, '本人未发送文字');
      expect(find.text('实际发送目标：实际收件人'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      d.dispose();
    },
  );
  testWidgets(
    'typed expired original reader callback cannot add reference and hides stale label',
    (t) async {
      var current = true;
      final candidate = LocalDraftReferenceCandidate(
        kind: 'place',
        id: 'place-1',
        title: '旧借用名称',
        version: 'v1',
        expiresAt: DateTime.now().toUtc().add(const Duration(seconds: 30)),
        sourceCurrent: () => current,
      );
      final d = await mount(t, () => [candidate]);
      await t.tap(find.text('添加当前引用'));
      await t.pumpAndSettle();
      final tap = t
          .widget<ListTile>(
            find.ancestor(
              of: find.text('旧借用名称'),
              matching: find.byType(ListTile),
            ),
          )
          .onTap!;
      if (capturing)
        await capture(t, 'typed-reference-before-source-retirement');
      current = false;
      await t.pump(const Duration(seconds: 1));
      expect(find.text('旧借用名称'), findsNothing);
      if (capturing)
        await capture(t, 'typed-reference-after-source-retirement');
      tap();
      await t.pump();
      expect(d.envelope!.references, isEmpty);
      await t.pumpWidget(const SizedBox());
      d.dispose();
    },
  );
  testWidgets(
    'typed annotation is own snapshot; owner ABA retires modal and old callback',
    (t) async {
      final d = await mount(t, () => []);
      d.setContext(
        references: const [
          LocalDraftReference(
            kind: 'activity',
            id: 'activity-1',
            version: 'v1',
          ),
        ],
      );
      await t.pump();
      await t.tap(find.text('编辑本人注释'));
      await t.pumpAndSettle();
      await t.enterText(find.byType(TextField), '本人计划，非作者正文');
      await t.tap(find.text('保留注释'));
      await t.pumpAndSettle();
      expect(d.envelope!.references.single.mode, 'snapshot');
      expect(d.envelope!.references.single.snapshotText, '本人计划，非作者正文');
      expect(find.text('活动 · 当前来源未核验'), findsOneWidget);
      await t.tap(find.text('编辑本人注释'));
      await t.pumpAndSettle();
      final confirm = t
          .widget<FilledButton>(find.widgetWithText(FilledButton, '保留注释'))
          .onPressed!;
      d.observeOwner('other');
      d.observeOwner('owner');
      await t.pump();
      expect(find.text('本人计划，非作者正文'), findsNothing);
      confirm();
      await t.pump();
      expect(d.envelope!.references, isEmpty);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      d.dispose();
    },
  );
}
