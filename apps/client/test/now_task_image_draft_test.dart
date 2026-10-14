import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:birdtie_client/src/content/moment_image_header.dart';
import 'package:birdtie_client/src/content/moment_image_picker.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/task_image_controller.dart';
import 'package:birdtie_client/src/workspace/task_image_page.dart';
import 'package:birdtie_client/src/workspace/task_image_pending_store.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'private_moment_media_controller_test.dart'
    show syntheticPrivatePNG, TestMomentImagePicker;

const _owner = '22222222-2222-4222-8222-222222222222';
const _city = 'unit-aberdeen';
const _submitted = '请查找这个城市的公开地点';
final _now = DateTime.utc(2026, 10, 9);

// Synthetic red 4x4 JPEG generated locally by FFmpeg; no real person or media.
const _jpeg =
    '/9j/4AAQSkZJRgABAgAAAQABAAD//gAPTGF2YzYzLjEuMTAxAP/bAEMACAQEBAQEBQUFBQUFBgYGBgYGBgYGBgYGBgcHBwgICAcHBwYGBwcICAgICQkJCAgICAkJCgoKDAwLCw4ODhERFP/EAEwAAQEAAAAAAAAAAAAAAAAAAAAGAQEBAAAAAAAAAAAAAAAAAAAGBxABAAAAAAAAAAAAAAAAAAAAABEBAAAAAAAAAAAAAAAAAAAAAP/AABEIAAQABAMBIgACEQADEQD/2gAMAwEAAhEDEQA/AIsATX9//9k=';

class _DraftFixture {
  _DraftFixture(Uint8List bytes, {bool preserveRenewal = false}) {
    picker = TestMomentImagePicker(bytes);
    draft = TaskImageDraftController(
      authorizationHeader: () => token,
      ownerID: () => owner,
      cityID: () => city,
      sourceCurrent: () => source,
      sourceChanges: changes,
      preserveLocalOnSessionRenewal: preserveRenewal,
      picker: picker,
    );
  }
  final changes = ChangeNotifier();
  late final TestMomentImagePicker picker;
  late final TaskImageDraftController draft;
  String? token = 'Bearer synthetic-session-a', owner = _owner, city = _city;
  bool source = true;
  Future<TaskImageDraftSelection> checked() async {
    await draft.select();
    draft.setReviewed(true);
    return draft.snapshot()!;
  }

  void close() {
    draft.dispose();
    changes.dispose();
  }
}

AgentTask _nativeTask({
  String owner = _owner,
  String city = _city,
  String status = 'COMPLETED',
}) => AgentTask(
  id: '11111111-1111-4111-8111-111111111111',
  query: '原问题',
  status: status,
  principalType: 'person',
  principalID: owner,
  actingUserID: owner,
  contextType: 'CITY',
  contextID: '44444444-4444-4444-8444-444444444444',
  cityID: city,
  updatedAt: _now,
  messages: const [
    AgentMessage(role: 'user', text: '原问题'),
    AgentMessage(role: 'assistant', text: '合成回答'),
    AgentMessage(role: 'user', text: _submitted),
    AgentMessage(role: 'assistant', text: '合成公开检索回答'),
    AgentMessage(role: 'user', text: '后来另一问'),
    AgentMessage(role: 'assistant', text: '另一回答'),
  ],
);

class _NativeFixture {
  _NativeFixture({
    AgentTask? task,
    String token = 'Bearer synthetic-session-a',
    int loadStatus = 200,
  }) {
    this.task = task ?? _nativeTask();
    client = MockClient((request) async {
      calls.add(request);
      if (request.method == 'GET') return _reply([], loadStatus);
      if (request.method == 'POST') {
        input = jsonDecode(request.body) as Map<String, dynamic>;
        return _reply(_receipt('preview'), 201);
      }
      if (request.method == 'PUT') {
        stored = Uint8List.fromList(request.bodyBytes);
        return _reply(_receipt('ready_private'), 200);
      }
      throw StateError('Unexpected mock request');
    });
    controller = TaskImageController(
      task: this.task,
      authorizationHeader: () => token,
      ownerID: () => _owner,
      sourceCurrent: () => source,
      sourceChanges: changes,
      apiBaseUrl: 'https://unit.example',
      client: client,
      clock: () => _now,
      pendingStore: MemoryTaskImagePendingStore(),
    );
  }
  final changes = ChangeNotifier(), calls = <http.Request>[];
  late final AgentTask task;
  late final MockClient client;
  late final TaskImageController controller;
  bool source = true;
  Map<String, dynamic>? input;
  Uint8List? stored;
  http.Response _reply(Object value, int status) => http.Response.bytes(
    utf8.encode(jsonEncode({'data': value})),
    status,
    headers: {'content-type': 'application/json; charset=utf-8'},
  );
  Map<String, dynamic> _receipt(String status) => {
    'id': '33333333-3333-4333-8333-333333333333',
    'operationId': input!['operationId'],
    'taskId': task.id,
    'ownerAccountId': _owner,
    'messageIndex': input!['messageIndex'],
    'prefixDigest': input!['prefixDigest'],
    'expectedTaskUpdatedAt': _now.toIso8601String(),
    'mimeType': 'image/png',
    'inputSize': input!['byteSize'],
    'byteSize': status == 'ready_private' ? stored!.length : 0,
    'inputSha256': input!['sha256'],
    'sha256': status == 'ready_private'
        ? sha256.convert(stored!).toString()
        : '',
    'pixelRisk': input!['pixelRisk'],
    'purpose': taskImagePurpose,
    'status': status,
    'revision': status == 'preview' ? 1 : 2,
    'previewExpiresAt': _now.add(const Duration(minutes: 5)).toIso8601String(),
    'retainUntil': _now.add(const Duration(days: 30)).toIso8601String(),
  };
  void close() {
    controller.dispose();
    changes.dispose();
    client.close();
  }
}

void main() {
  test(
    'opt-in same-owner renewal preserves RAM pixels but retires every old approval',
    () async {
      final f = _DraftFixture(
        await syntheticPrivatePNG(),
        preserveRenewal: true,
      );
      addTearDown(f.close);
      final old = await f.checked();
      final bytes = Uint8List.fromList(f.draft.localBytes!);
      f.token = 'Bearer synthetic-session-b';
      f.changes.notifyListeners();
      expect(f.draft.current, isTrue);
      expect(f.draft.localBytes, bytes);
      expect(f.draft.reviewed, isFalse);
      expect(old.isCurrent, isFalse);
      expect(f.draft.snapshot(), isNull);
      f.draft.setReviewed(true);
      final renewed = f.draft.snapshot()!;
      expect(renewed.isCurrent, isTrue);
      f.token = 'Bearer synthetic-session-a';
      f.changes.notifyListeners();
      expect(old.isCurrent, isFalse);
      expect(renewed.isCurrent, isFalse);
      expect(f.draft.localBytes, bytes);
    },
  );

  test(
    'opt-in renewal cannot revive owner ABA or accept a late pending picker',
    () async {
      final f = _DraftFixture(
        await syntheticPrivatePNG(),
        preserveRenewal: true,
      );
      addTearDown(f.close);
      final old = await f.checked();
      f.owner = '55555555-5555-4555-8555-555555555555';
      f.changes.notifyListeners();
      f.owner = _owner;
      f.changes.notifyListeners();
      expect(f.draft.current, isFalse);
      expect(f.draft.localBytes, isNull);
      expect(old.isCurrent, isFalse);
      final pending = _DraftFixture(
        await syntheticPrivatePNG(),
        preserveRenewal: true,
      );
      addTearDown(pending.close);
      final picker = Completer<MomentImageSelection?>();
      pending.picker.pending = picker;
      final operation = pending.draft.select();
      await Future<void>.delayed(Duration.zero);
      pending.token = 'Bearer synthetic-session-b';
      pending.changes.notifyListeners();
      picker.complete(MomentImageSelection(pending.picker.bytes));
      await operation;
      expect(pending.draft.current, isFalse);
      expect(pending.draft.localBytes, isNull);
    },
  );

  test(
    'pure pixel extraction preserves city/owner-specific Agent approval fencing',
    () async {
      final f = _DraftFixture(await syntheticPrivatePNG());
      addTearDown(f.close);
      await f.draft.select();
      expect(f.draft.localBytes, isNotNull);
      expect(f.draft.snapshot(), isNull);
      final approved = await f.checked();
      f.city = 'other-city';
      f.changes.notifyListeners();
      expect(approved.isCurrent, isFalse);
      expect(f.draft.canUse, isFalse);
      expect(f.draft.snapshot(), isNull);
    },
  );

  TestWidgetsFlutterBinding.ensureInitialized();

  test(
    'first-conversation PNG material is local, pixel-only and unconfirmed',
    () async {
      final f = _DraftFixture(await syntheticPrivatePNG());
      addTearDown(f.close);
      await f.draft.select();
      expect(f.draft.current, isTrue);
      expect(f.draft.localBytes, isNotNull);
      expect(MomentImageHeader.inspect(f.draft.localBytes!).mime, 'image/png');
      expect(f.draft.localAspectRatio, 1);
      expect(f.draft.reviewed, isFalse);
      expect(f.draft.snapshot(), isNull);
      expect(f.picker.calls, 1);
    },
  );

  test(
    'real synthetic JPEG pixels normalize into the same PNG material',
    () async {
      final f = _DraftFixture(base64Decode(_jpeg));
      addTearDown(f.close);
      expect(MomentImageHeader.inspect(f.picker.bytes).mime, 'image/jpeg');
      await f.draft.select();
      expect(f.draft.error, isNull);
      expect(MomentImageHeader.inspect(f.draft.localBytes!).mime, 'image/png');
      expect(f.draft.localAspectRatio, 1);
    },
  );

  test(
    'local masking changes pixels and invalidates the checked material revision',
    () async {
      final f = _DraftFixture(await syntheticPrivatePNG());
      addTearDown(f.close);
      final snapshot = await f.checked();
      final before = sha256.convert(f.draft.localBytes!).toString();
      await f.draft.mask(const ui.Rect.fromLTRB(0, 0, .5, .5));
      expect(sha256.convert(f.draft.localBytes!).toString(), isNot(before));
      expect(f.draft.reviewed, isFalse);
      expect(snapshot.isCurrent, isFalse);
      expect(f.draft.snapshot(), isNull);
    },
  );

  test(
    'canceling a replacement preserves original checked pixels and text-independent state',
    () async {
      final f = _DraftFixture(await syntheticPrivatePNG());
      addTearDown(f.close);
      final snapshot = await f.checked();
      final before = sha256.convert(f.draft.localBytes!).toString();
      f.picker.canceled = true;
      await f.draft.select();
      expect(sha256.convert(f.draft.localBytes!).toString(), before);
      expect(snapshot.isCurrent, isTrue);
      expect(f.draft.busy, isFalse);
    },
  );

  test('canceling first gallery selection creates no material', () async {
    final f = _DraftFixture(await syntheticPrivatePNG())
      ..picker.canceled = true;
    addTearDown(f.close);
    await f.draft.select();
    expect(f.draft.localBytes, isNull);
    expect(f.draft.snapshot(), isNull);
    expect(f.draft.error, isNull);
  });

  test(
    'malformed replacement fails visibly and retains the checked original',
    () async {
      final f = _DraftFixture(await syntheticPrivatePNG());
      addTearDown(f.close);
      final snapshot = await f.checked();
      f.picker.bytes = Uint8List.fromList([1, 2, 3]);
      await f.draft.select();
      expect(f.draft.error, isNotNull);
      expect(snapshot.isCurrent, isTrue);
    },
  );

  for (final boundary in ['owner', 'session', 'city', 'source']) {
    test(
      '$boundary boundary permanently retires checked pixels, including ABA return',
      () async {
        final f = _DraftFixture(await syntheticPrivatePNG());
        addTearDown(f.close);
        final snapshot = await f.checked();
        switch (boundary) {
          case 'owner':
            f.owner = '55555555-5555-4555-8555-555555555555';
          case 'session':
            f.token = 'Bearer synthetic-session-b';
          case 'city':
            f.city = 'other-unit-city';
          case 'source':
            f.source = false;
        }
        f.changes.notifyListeners();
        f.owner = _owner;
        f.token = 'Bearer synthetic-session-a';
        f.city = _city;
        f.source = true;
        f.changes.notifyListeners();
        expect(f.draft.current, isFalse);
        expect(f.draft.localBytes, isNull);
        expect(snapshot.isCurrent, isFalse);
      },
    );
  }

  test('late gallery result cannot resurrect retired owner material', () async {
    final bytes = await syntheticPrivatePNG();
    final f = _DraftFixture(bytes);
    addTearDown(f.close);
    final pending = Completer<MomentImageSelection?>();
    f.picker.pending = pending;
    final operation = f.draft.select();
    await Future<void>.delayed(Duration.zero);
    f.token = 'Bearer synthetic-session-b';
    f.changes.notifyListeners();
    pending.complete(MomentImageSelection(bytes));
    await operation;
    expect(f.draft.localBytes, isNull);
    expect(f.draft.current, isFalse);
  });

  test('removing or unchecking material invalidates old snapshots', () async {
    final f = _DraftFixture(await syntheticPrivatePNG());
    addTearDown(f.close);
    final checked = await f.checked();
    f.draft.setReviewed(false);
    f.draft.setReviewed(true);
    expect(checked.isCurrent, isFalse);
    final newer = f.draft.snapshot()!;
    f.draft.removeLocal();
    expect(newer.isCurrent, isFalse);
    expect(f.draft.localBytes, isNull);
  });

  test(
    'mutating checked local bytes rejects the frozen revision before staging',
    () async {
      final f = _DraftFixture(await syntheticPrivatePNG());
      final native = _NativeFixture();
      addTearDown(f.close);
      addTearDown(native.close);
      final checked = await f.checked();
      await native.controller.load();
      f.draft.localBytes![f.draft.localBytes!.length - 1] ^= 1;
      expect(checked.isCurrent, isFalse);
      expect(
        native.controller.stageReviewedDraft(
          checked,
          messageIndex: 2,
          submittedText: _submitted,
        ),
        isFalse,
      );
      expect(native.calls.where((r) => r.method != 'GET'), isEmpty);
    },
  );

  test(
    'signed-out or invalid owner does not read gallery or create a material',
    () async {
      final f = _DraftFixture(await syntheticPrivatePNG())..owner = null;
      addTearDown(f.close);
      await f.draft.select();
      expect(f.picker.calls, 0);
      expect(f.draft.localBytes, isNull);
      expect(f.draft.current, isFalse);
    },
  );

  test(
    'staging binds exact submitted user turn rather than the last user message',
    () async {
      final f = _DraftFixture(await syntheticPrivatePNG());
      final native = _NativeFixture();
      addTearDown(f.close);
      addTearDown(native.close);
      await f.draft.select();
      await f.draft.mask(const ui.Rect.fromLTRB(0, 0, .5, .5));
      f.draft.setReviewed(true);
      final checked = f.draft.snapshot()!;
      final bytes = Uint8List.fromList(f.draft.localBytes!);
      await native.controller.load();
      expect(
        native.controller.stageReviewedDraft(
          checked,
          messageIndex: 2,
          submittedText: _submitted,
        ),
        isTrue,
      );
      expect(native.calls, hasLength(1)); // Only original authorized list read.
      expect(native.controller.targetUserIndex, 2);
      expect(native.controller.lastUserIndex, 4);
      expect(native.controller.localBytes, bytes);
      expect(f.draft.localBytes, isNull);
      expect(checked.isCurrent, isFalse);
      expect(native.controller.canPreview, isFalse);
      native.controller.setReviewed(true);
      await native.controller.prepare();
      expect(native.input!['messageIndex'], 2);
      expect(native.input!['prefixDigest'], taskImagePrefix(native.task, 2));
      expect(native.input!['pixelRisk'], 'USER_MASKED');
      expect(native.input!['purpose'], taskImagePurpose);
      expect(
        native.controller.canSave,
        isFalse,
      ); // Original final confirmation remains.
      native.controller.setReviewed(true);
      await native.controller.save();
      expect(native.controller.images.single.index, 2);
      expect(native.stored, bytes);
      expect(native.calls.where((r) => r.method == 'PUT'), hasLength(1));
      expect(
        native.controller.stageReviewedDraft(
          checked,
          messageIndex: 2,
          submittedText: _submitted,
        ),
        isFalse,
      );
    },
  );

  test(
    'pre-Task material stages only after the new native first user message exists',
    () async {
      final f = _DraftFixture(await syntheticPrivatePNG());
      addTearDown(f.close);
      final checked = await f.checked(); // No Task or HTTP client exists yet.
      final task = AgentTask(
        id: '66666666-6666-4666-8666-666666666666',
        query: _submitted,
        status: 'COMPLETED',
        principalType: 'person',
        principalID: _owner,
        actingUserID: _owner,
        contextType: 'CITY',
        contextID: '44444444-4444-4444-8444-444444444444',
        cityID: _city,
        updatedAt: _now,
        messages: const [
          AgentMessage(role: 'user', text: _submitted),
          AgentMessage(role: 'assistant', text: '合成检索回答，不读取图片'),
        ],
      );
      final native = _NativeFixture(task: task);
      addTearDown(native.close);
      expect(
        native.controller.stageReviewedDraft(
          checked,
          messageIndex: 0,
          submittedText: _submitted,
        ),
        isFalse,
      );
      expect(checked.isCurrent, isTrue);
      expect(native.calls, isEmpty);
      await native.controller.load();
      expect(
        native.controller.stageReviewedDraft(
          checked,
          messageIndex: 0,
          submittedText: _submitted,
        ),
        isTrue,
      );
      expect(native.controller.targetUserIndex, 0);
      expect(native.controller.localBytes, isNotNull);
      expect(native.controller.preview, isNull);
      expect(native.calls.where((r) => r.method != 'GET'), isEmpty);
      native.controller.setReviewed(true);
      await native.controller.prepare();
      expect(native.input!['messageIndex'], 0);
      expect(native.input!['prefixDigest'], taskImagePrefix(task, 0));
      expect(native.controller.canSave, isFalse);
    },
  );

  for (final wrong in [-1, 0, 1, 6, 256]) {
    test(
      'wrong/old/assistant message index $wrong rejects without consuming local draft',
      () async {
        final f = _DraftFixture(await syntheticPrivatePNG());
        final native = _NativeFixture();
        addTearDown(f.close);
        addTearDown(native.close);
        final checked = await f.checked();
        await native.controller.load();
        expect(
          native.controller.stageReviewedDraft(
            checked,
            messageIndex: wrong,
            submittedText: _submitted,
          ),
          isFalse,
        );
        expect(checked.isCurrent, isTrue);
        expect(native.controller.localBytes, isNull);
        expect(native.calls.where((r) => r.method != 'GET'), isEmpty);
      },
    );
  }

  for (final problem in [
    'owner',
    'session',
    'city',
    'not-completed',
    'list-failed',
    'source-retired',
  ]) {
    test(
      'native $problem failure retains local material and never starts an upload',
      () async {
        final f = _DraftFixture(await syntheticPrivatePNG());
        final native = _NativeFixture(
          task: _nativeTask(
            owner: problem == 'owner'
                ? '55555555-5555-4555-8555-555555555555'
                : _owner,
            city: problem == 'city' ? 'other-unit-city' : _city,
            status: problem == 'not-completed' ? 'ACTIVE' : 'COMPLETED',
          ),
          token: problem == 'session'
              ? 'Bearer synthetic-session-b'
              : 'Bearer synthetic-session-a',
          loadStatus: problem == 'list-failed' ? 503 : 200,
        );
        addTearDown(f.close);
        addTearDown(native.close);
        final checked = await f.checked();
        await native.controller.load();
        if (problem == 'source-retired') {
          native.source = false;
          native.changes.notifyListeners();
        }
        expect(
          native.controller.stageReviewedDraft(
            checked,
            messageIndex: 2,
            submittedText: _submitted,
          ),
          isFalse,
        );
        expect(checked.isCurrent, isTrue);
        expect(native.controller.localBytes, isNull);
        expect(native.calls.where((r) => r.method != 'GET'), isEmpty);
      },
    );
  }

  testWidgets(
    'real plus opens only connected material choices; cancel preserves same input',
    (t) async {
      final workspace = AgentWorkspaceController();
      addTearDown(workspace.dispose);
      var images = 0, paste = 0;
      final sent = <String>[];
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: AgentComposer(
              workspace: workspace,
              onSubmit: sent.add,
              onSearchArea: () {},
              hasSearchArea: false,
              onDraftImage: () => images++,
              onPasteText: () => paste++,
            ),
          ),
        ),
      );
      await t.enterText(find.byType(TextField), '同一份未发送草稿');
      await t.tap(find.byTooltip('添加材料'));
      await t.pumpAndSettle();
      expect(find.text('添加材料'), findsOneWidget);
      expect(find.text('图片'), findsOneWidget);
      expect(find.text('粘贴文字/链接'), findsNothing);
      expect(find.textContaining('不用于模型识图'), findsNothing);
      expect(images, 0);
      expect(paste, 0);
      expect(sent, isEmpty);
      Navigator.of(t.element(find.text('添加材料'))).pop();
      await t.pumpAndSettle();
      expect(
        t.widget<TextField>(find.byType(TextField)).controller!.text,
        '同一份未发送草稿',
      );
      await t.tap(find.byTooltip('添加材料'));
      await t.pumpAndSettle();
      await t.tap(find.text('图片'));
      await t.pumpAndSettle();
      expect(images, 1);
      expect(sent, isEmpty);
      await t.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'image thumbnail is visible and removing it preserves editable text without sending',
    (t) async {
      final f = _DraftFixture(
        await t.runAsync(syntheticPrivatePNG) ?? Uint8List(0),
      );
      final workspace = AgentWorkspaceController();
      addTearDown(f.close);
      addTearDown(workspace.dispose);
      await t.runAsync(f.draft.select);
      final sent = <String>[];
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: AgentComposer(
              workspace: workspace,
              onSubmit: sent.add,
              onSearchArea: () {},
              hasSearchArea: false,
              imageDraft: f.draft,
              onDraftImage: () {},
            ),
          ),
        ),
      );
      await t.enterText(find.byType(TextField), _submitted);
      await t.pump();
      expect(
        find.byKey(const Key('now-composer-image-thumbnail')),
        findsOneWidget,
      );
      expect(find.text('图片 · 助手暂不读图'), findsOneWidget);
      expect(f.draft.reviewed, isFalse);
      expect(sent, isEmpty);
      expect(
        t.widget<TextField>(find.byType(TextField)).controller!.text,
        _submitted,
      );
      await t.tap(find.byTooltip('移除本机图片'));
      await t.pump();
      expect(f.draft.localBytes, isNull);
      expect(
        t.widget<TextField>(find.byType(TextField)).controller!.text,
        _submitted,
      );
      await t.tap(find.byTooltip('发送需求'));
      await t.pump();
      expect(sent, [_submitted]);
      await t.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'explicit send approves the exact visible local image and submits once',
    (t) async {
      final f = _DraftFixture(
        await t.runAsync(syntheticPrivatePNG) ?? Uint8List(0),
      );
      final workspace = AgentWorkspaceController();
      addTearDown(f.close);
      addTearDown(workspace.dispose);
      await t.runAsync(f.draft.select);
      final sent = <String>[];
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: AgentComposer(
              workspace: workspace,
              onSubmit: sent.add,
              onSearchArea: () {},
              hasSearchArea: false,
              imageDraft: f.draft,
              onDraftImage: () {},
            ),
          ),
        ),
      );
      await t.enterText(find.byType(TextField), _submitted);
      await t.pump();
      final bytes = f.draft.localBytes;
      final visible = t.widget<Image>(
        find.byKey(const Key('now-composer-image-thumbnail')),
      );
      expect((visible.image as MemoryImage).bytes, same(bytes));
      expect(f.draft.reviewed, isFalse);
      expect(sent, isEmpty);
      await t.tap(find.byTooltip('发送需求'));
      await t.pump();
      expect(sent, [_submitted]);
      expect(f.draft.reviewed, isTrue);
      expect(f.draft.localBytes, same(bytes));
      expect(f.draft.snapshot(), isNotNull);
      await t.tap(find.byTooltip('发送需求'));
      await t.pump();
      expect(sent, [_submitted]);
      await t.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'local preview checks pixels and returns without any Task or upload',
    (t) async {
      final f = _DraftFixture(
        await t.runAsync(syntheticPrivatePNG) ?? Uint8List(0),
      );
      addTearDown(f.close);
      await t.runAsync(f.draft.select);
      bool? returned;
      await t.pumpWidget(
        MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: FilledButton(
                onPressed: () async => returned = await Navigator.push<bool>(
                  context,
                  MaterialPageRoute(
                    builder: (_) => TaskImageDraftPage(
                      controller: f.draft,
                      selectOnOpen: false,
                    ),
                  ),
                ),
                child: const Text('打开草稿'),
              ),
            ),
          ),
        ),
      );
      await t.tap(find.text('打开草稿'));
      await t.pumpAndSettle();
      expect(find.textContaining('助手暂不读取图片'), findsOneWidget);
      expect(find.byType(Image), findsOneWidget);
      await t.drag(find.byType(ListView), const Offset(0, -250));
      await t.pumpAndSettle();
      await t.ensureVisible(find.byType(Checkbox));
      await t.pumpAndSettle();
      await t.tap(find.byType(Checkbox));
      await t.pump();
      await t.ensureVisible(find.text('使用这张图片'));
      await t.tap(find.text('使用这张图片'));
      await t.pumpAndSettle();
      expect(returned, isTrue);
      expect(f.draft.canUse, isTrue);
      expect(f.draft.localBytes, isNotNull);
      await t.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'low composer height still offers image selection and removal in plus menu',
    (t) async {
      final f = _DraftFixture(
        await t.runAsync(syntheticPrivatePNG) ?? Uint8List(0),
      );
      final workspace = AgentWorkspaceController();
      addTearDown(f.close);
      addTearDown(workspace.dispose);
      await t.runAsync(f.draft.select);
      var imageChecks = 0;
      final sent = <String>[];
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Align(
              alignment: Alignment.bottomCenter,
              child: SizedBox(
                height: 80,
                child: AgentComposer(
                  workspace: workspace,
                  onSubmit: sent.add,
                  onSearchArea: () {},
                  hasSearchArea: false,
                  imageDraft: f.draft,
                  onDraftImage: () => imageChecks++,
                ),
              ),
            ),
          ),
        ),
      );
      await t.enterText(find.byType(TextField), _submitted);
      await t.pump();
      expect(sent, isEmpty);
      await t.tap(find.byTooltip('添加材料'));
      await t.pumpAndSettle();
      expect(find.text('移除本机图片'), findsOneWidget);
      expect(find.text('保留原输入框中的文字'), findsOneWidget);
      await t.tap(find.text('移除本机图片'));
      await t.pumpAndSettle();
      expect(f.draft.localBytes, isNull);
      expect(
        t.widget<TextField>(find.byType(TextField)).controller!.text,
        _submitted,
      );
      expect(imageChecks, 0);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
    },
  );

  testWidgets('open material menu cannot act after task boundary changes', (
    t,
  ) async {
    final workspace = AgentWorkspaceController();
    addTearDown(workspace.dispose);
    var selections = 0;
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentComposer(
            workspace: workspace,
            onSubmit: (_) {},
            onSearchArea: () {},
            hasSearchArea: false,
            onDraftImage: () => selections++,
          ),
        ),
      ),
    );
    await t.tap(find.byTooltip('添加材料'));
    await t.pumpAndSettle();
    workspace.newTask();
    await t.pump();
    await t.tap(find.text('图片'));
    await t.pumpAndSettle();
    expect(selections, 0);
    await t.pumpWidget(const SizedBox());
  });
}
