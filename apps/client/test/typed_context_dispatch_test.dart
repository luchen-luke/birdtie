import 'package:birdtie_client/src/workspace/now_composer_draft_state.dart';
import 'package:birdtie_client/src/workspace/typed_local_draft.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'dart:typed_data';
import 'package:birdtie_client/src/content/moment_image_picker.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/agent_visual_page.dart';
import 'package:birdtie_client/src/workspace/local_draft_context_page.dart';
import 'friend_image_test.dart' show imageFixtureBytes;
import 'package:http/http.dart' as http;
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'dart:io';
import 'package:flutter/services.dart';
import 'batch_a_visual_test.dart' show visualTheme, capture, captureKey;
import 'now_scope_recovery_test.dart';
import 'now_composer_material_test.dart' show openMaterialMenu;

const localOrigin = 'http://now-fixture.test';
const imageOwner = 'c1000000-0000-4000-8000-000000000001';

class _LocalPicker extends MomentImagePicker {
  _LocalPicker(this.bytes);
  final Uint8List bytes;
  @override
  Future<bool> discardLostSelection() async => false;
  @override
  Future<MomentImageSelection?> select() async => MomentImageSelection(bytes);
}

class _VisualRequestSpy extends http.BaseClient {
  _VisualRequestSpy(this.inner);
  final http.Client inner;
  final visualRequests = <http.BaseRequest>[];
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) {
    if (request.url.path.contains('/agent-visual')) visualRequests.add(request);
    return inner.send(request);
  }

  @override
  void close() {}
}

Future<void> mountImageNow(
  WidgetTester t,
  NowFixture f,
  Uint8List bytes, {
  http.Client? client,
  bool select = true,
}) async {
  t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
  await t.pumpWidget(
    MaterialApp(
      theme: visualTheme(),
      builder: (context, child) =>
          RepaintBoundary(key: captureKey, child: child!),
      home: MapWorkspace(
        city: f.city,
        auth: f.auth,
        moments: f.moments,
        agentTaskSource: f.source,
        seedClient: client ?? f.client,
        seedApiBaseUrl: f.base,
        taskImagePicker: _LocalPicker(bytes),
      ),
    ),
  );
  await t.pumpAndSettle();
  if (select) {
    await openMaterialMenu(t);
    await t.runAsync(() async {
      await t.tap(find.text('图片'));
      for (var n = 0; n < 100; n++) {
        await Future<void>.delayed(const Duration(milliseconds: 10));
        final draft = t
            .widget<AgentComposer>(find.byType(AgentComposer))
            .imageDraft;
        if (draft?.busy != true) break;
      }
    });
    await t.pumpAndSettle();
    final selected = t
        .widget<AgentComposer>(find.byType(AgentComposer))
        .imageDraft;
    expect(selected?.error, isNull);
    expect(selected?.localBytes, isNotNull);
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUpAll(() async {
    final bytes = await File(
      Platform.environment['BIRDTIE_MAP_MEDIA_FIXTURE_FONT'] ??
          r'C:\Windows\Fonts\msyh.ttc',
    ).readAsBytes();
    final loader = FontLoader('BatchACJK')
      ..addFont(Future.value(ByteData.sublistView(bytes)));
    await loader.load();
  });
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  for (final renew in [false, true]) {
    testWidgets(
      'actual Now unanalysed image question saves edited local text zero visual or Task request renew=$renew',
      (t) async {
        final bytes = (await t.runAsync(imageFixtureBytes))!;
        final f = NowFixture();
        f.auth.token = 'Bearer local';
        f.auth.owner = imageOwner;
        final spy = _VisualRequestSpy(f.client);
        addTearDown(f.dispose);
        await mountImageNow(t, f, bytes, client: spy);
        final map = t.state(find.byType(MapCanvas));
        await t.enterText(nowField(), '原本人的图片问题');
        await t.tap(find.byTooltip('发送需求'));
        await t.pumpAndSettle();
        expect(find.byType(AgentVisualPage), findsOneWidget);
        expect(spy.visualRequests, isEmpty);
        if (renew) {
          f.auth.changeIdentity('Bearer renewed', nextOwner: imageOwner);
          await t.pumpAndSettle();
          expect(find.byType(AgentVisualPage), findsOneWidget);
        }
        final question = find.byKey(const Key('local-visual-question'));
        await t.ensureVisible(question);
        await t.enterText(question, '本人检查后的未分析问题');
        await t.ensureVisible(
          find.byKey(const Key('local-visual-save-question')),
        );
        await t.tap(find.byKey(const Key('local-visual-save-question')));
        await t.pumpAndSettle();
        expect(find.byKey(const Key('local-visual-preview')), findsOneWidget);
        await t.tap(find.byKey(const Key('local-visual-confirm')));
        await t.runAsync(
          () => Future<void>.delayed(const Duration(milliseconds: 50)),
        );
        await t.pumpAndSettle();
        expect(find.byType(AgentVisualPage), findsNothing);
        expect(t.widget<TextField>(nowField()).controller!.text, '本人检查后的未分析问题');
        expect(t.state(find.byType(MapCanvas)), same(map));
        expect(spy.visualRequests, isEmpty);
        expect(f.source.queries, isEmpty);
        final rows = await t.runAsync(
          () => SecureTypedLocalDraftStore().read(localOrigin, imageOwner),
        );
        expect(rows!.single.text, '本人检查后的未分析问题');
        expect(rows.single.attachments.single.kind, 'image');
        await f.unmount(t);
      },
    );
  }
  testWidgets(
    'actual Now selected image projects opaque local attachment and restores without pixels or upload authority',
    (t) async {
      t.view.physicalSize = const Size(390, 844);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final bytes = (await t.runAsync(imageFixtureBytes))!;
      final f = NowFixture();
      f.auth.token = 'Bearer local';
      f.auth.owner = imageOwner;
      addTearDown(f.dispose);
      await mountImageNow(t, f, bytes);
      await t.enterText(nowField(), '本人图片问题');
      await openMaterialMenu(t);
      await t.tap(find.text('草稿引用与材料'));
      await t.pumpAndSettle();
      final page = t.widget<LocalDraftContextPage>(
        find.byType(LocalDraftContextPage),
      );
      final selected = page.drafts.envelope!;
      expect(selected.attachments.single.kind, 'image');
      expect(selected.attachments.single.state, 'local');
      expect(selected.attachments.single.toJson().keys.toSet(), {
        'id',
        'kind',
        'state',
      });
      expect(page.attachmentCurrent!(selected.attachments.single), isTrue);
      expect(f.source.queries, isEmpty);
      await capture(
        t,
        'WIDGET_FIXTURE_NOT_DEVICE_390x844_local-attachment-selected',
      );
      await t.runAsync(page.drafts.flush);
      await nowBack(t);
      await f.unmount(t);
      await mountImageNow(t, f, bytes, select: false);
      await t.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 40)),
      );
      await t.pump();
      await openMaterialMenu(t);
      await t.tap(find.text('草稿引用与材料'));
      await t.pumpAndSettle();
      final restored = t.widget<LocalDraftContextPage>(
        find.byType(LocalDraftContextPage),
      );
      expect(restored.drafts.envelope!.id, selected.id);
      expect(
        restored.drafts.envelope!.attachments.single.id,
        selected.attachments.single.id,
      );
      expect(
        restored.attachmentCurrent!(
          restored.drafts.envelope!.attachments.single,
        ),
        isFalse,
      );
      expect(find.text('仅有选择记录，请重新选择'), findsOneWidget);
      expect(f.source.queries, isEmpty);
      await capture(
        t,
        'WIDGET_FIXTURE_NOT_DEVICE_390x844_local-attachment-restored',
      );
      await f.unmount(t);
    },
  );
  test(
    'typed text success preserves unsent refs and attachment history on real secure restore',
    () async {
      final d = NowComposerDraftState()..observeOwner('owner');
      d.configurePersistence(localOrigin);
      d.update(const TextEditingValue(text: '已发送文字'));
      d.setContext(
        references: const [
          LocalDraftReference(kind: 'person', id: 'referenced-peer'),
        ],
        attachments: const [
          LocalDraftAttachment(
            id: 'old-image',
            kind: 'image',
            state: 'uploaded',
          ),
        ],
      );
      final identity = d.identity,
          version = d.contentVersion,
          id = d.envelope!.id;
      d.clearSent(
        identity,
        expectedText: '已发送文字',
        expectedVersion: version,
        preserveMaterials: true,
      );
      await d.flush();
      d.dispose();
      final restored = NowComposerDraftState()..observeOwner('owner');
      restored.configurePersistence(localOrigin);
      await restored.restore();
      expect(restored.envelope!.id, id);
      expect(restored.value.text, isEmpty);
      expect(restored.envelope!.references.single.id, 'referenced-peer');
      expect(restored.envelope!.attachments.single.state, 'uploaded');
      final oldVersion = restored.contentVersion;
      restored.setContext(
        references: const [
          LocalDraftReference(kind: 'person', id: 'new-reference'),
        ],
      );
      restored.update(const TextEditingValue(text: '下一句'));
      restored.clearSent(
        restored.identity,
        expectedVersion: oldVersion,
        preserveMaterials: true,
      );
      expect(restored.value.text, '下一句');
      expect(restored.envelope!.references.single.id, 'new-reference');
      restored.dispose();
    },
  );
  testWidgets(
    'typed actual Now material entry opens references without query or target write',
    (t) async {
      final f = NowFixture();
      f.auth.token = 'Bearer test';
      f.auth.owner = 'test-owner';
      addTearDown(f.dispose);
      await f.mount(t);
      await t.enterText(nowField(), '本人的草稿');
      await openMaterialMenu(t);
      await t.ensureVisible(find.text('草稿引用与材料'));
      await t.tap(find.text('草稿引用与材料'));
      await t.pumpAndSettle();
      expect(find.text('实际发送目标：本人 Agent'), findsOneWidget);
      expect(f.source.queries, isEmpty);
      expect(find.text('@ 引用保留在本机，不会改变收件人，也不会自动随文字发送。'), findsOneWidget);
      await nowBack(t);
      expect(t.widget<TextField>(nowField()).controller!.text, '本人的草稿');
      await f.unmount(t);
    },
  );
  testWidgets(
    'typed actual restored reference requires explicit text-scope confirmation and keeps metadata',
    (t) async {
      final f = NowFixture();
      f.auth.token = 'Bearer test';
      f.auth.owner = 'test-owner';
      addTearDown(f.dispose);
      final store = SecureTypedLocalDraftStore();
      final original = TypedLocalDraft(
        owner: 'test-owner',
        origin: localOrigin,
        kind: 'intent',
        text: '只发送本人的文字',
        references: const [
          LocalDraftReference(
            kind: 'place',
            id: 'not-model-place-id',
            version: 'v1',
          ),
        ],
      );
      await store.save(localOrigin, 'test-owner', [original]);
      await f.mount(t);
      await t.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 30)),
      );
      await t.pump();
      await t.tap(find.byTooltip('发送需求'));
      await t.pumpAndSettle();
      expect(f.source.queries, isEmpty);
      expect(find.text('确认本次发送内容'), findsOneWidget);
      await t.tap(find.text('返回检查'));
      await t.pumpAndSettle();
      expect(f.source.queries, isEmpty);
      await t.tap(find.byTooltip('发送需求'));
      await t.pumpAndSettle();
      await t.tap(find.text('继续发送'));
      await t.pumpAndSettle();
      expect(f.source.queries, ['只发送本人的文字']);
      // Secure storage debounce runs in simulated time, followed by real channel I/O.
      await t.pump(const Duration(milliseconds: 400));
      await t.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 30)),
      );
      await t.pump();
      final rows = await t.runAsync(
        () => store.read(localOrigin, 'test-owner'),
      );
      expect(rows!.single.id, original.id);
      expect(rows.single.text, isEmpty);
      expect(rows.single.references.single.id, 'not-model-place-id');
      expect(rows.single.taskID, f.workspace(t).task!.id);
      await f.unmount(t);
    },
  );
}
