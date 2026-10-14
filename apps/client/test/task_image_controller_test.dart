import 'package:birdtie_client/src/workspace/local_image_draft.dart';
import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:birdtie_client/src/content/moment_image_picker.dart';
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
const _id = '33333333-3333-4333-8333-333333333333';
final _now = DateTime.utc(2026, 10, 8, 3);
final _task = AgentTask(
  id: '11111111-1111-4111-8111-111111111111',
  query: '<当前&问题>',
  status: 'COMPLETED',
  principalType: 'person',
  principalID: _owner,
  actingUserID: _owner,
  cityID: 'unit-city',
  contextID: '44444444-4444-4444-8444-444444444444',
  updatedAt: _now,
  messages: const [
    AgentMessage(role: 'user', text: '<当前&问题>'),
    AgentMessage(role: 'assistant', text: '合成原回答'),
  ],
);

class _Fixture {
  _Fixture(Uint8List bytes, {bool pageReader = false}) {
    picker = TestMomentImagePicker(bytes);
    client = MockClient((request) async {
      calls.add('${request.method} ${request.url.path}');
      if (request.method == 'GET' && request.url.path.endsWith('/images')) {
        return reply(ready == null ? [] : [ready!], listStatus);
      }
      if (request.method == 'POST') {
        input = jsonDecode(request.body) as Map<String, dynamic>;
        if (failPreview) return reply({}, 503);
        return reply(receipt('preview'), 201);
      }
      if (request.method == 'PUT') {
        stored = Uint8List.fromList(request.bodyBytes);
        ready = receipt('ready_private');
        return failSave ? reply({}, 503) : reply(ready, 200);
      }
      if (request.method == 'GET' &&
          request.url.path.contains('/image-operations/')) {
        return reply(
          ready ?? receipt(invalidated ? 'invalidated_preview' : 'preview'),
          200,
        );
      }
      if (request.method == 'GET' && request.url.path.endsWith('/content')) {
        if (waitingBinary != null) return waitingBinary!.future;
        return http.Response.bytes(
          tamper
              ? Uint8List.fromList(stored!..[stored!.length - 1] ^= 1)
              : stored!,
          200,
          headers: {'content-type': 'image/png'},
        );
      }
      if (request.method == 'DELETE') {
        ready = null;
        return http.Response('', 204);
      }
      return reply({}, 404);
    });
    controller = pageReader
        ? _PageReadController(this)
        : TaskImageController(
            task: _task,
            authorizationHeader: () => token,
            ownerID: () => owner,
            sourceCurrent: () => source,
            sourceChanges: changes,
            apiBaseUrl: 'https://unit.example',
            picker: picker,
            client: client,
            clock: () => now,
            pendingStore: pending,
          );
  }
  final changes = ChangeNotifier(), calls = <String>[];
  final pending = MemoryTaskImagePendingStore();
  late final TestMomentImagePicker picker;
  late final MockClient client;
  late final TaskImageController controller;
  String? token = 'Bearer unit-session', owner = _owner;
  bool source = true,
      failPreview = false,
      failSave = false,
      tamper = false,
      invalidated = false;
  int listStatus = 200;
  Map<String, dynamic>? input, ready;
  Completer<http.Response>? waitingBinary;
  DateTime now = _now;
  Uint8List? stored;
  http.Response reply(Object? value, int code) => http.Response.bytes(
    utf8.encode(jsonEncode({'data': value})),
    code,
    headers: {'content-type': 'application/json; charset=utf-8'},
  );
  Map<String, dynamic> receipt(String status) => {
    'id': _id,
    'operationId': input!['operationId'],
    'taskId': _task.id,
    'ownerAccountId': _owner,
    'messageIndex': 0,
    'prefixDigest': taskImagePrefix(_task, 0),
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
  Future<void> select() async {
    await controller.load();
    await controller.select();
  }

  Future<void> prepare() async {
    await select();
    controller.setReviewed(true);
    await controller.prepare();
  }

  void close() {
    controller.dispose();
    changes.dispose();
    client.close();
  }
}

// Page retention uses Flutter's fake clock; transport/stream validation has
// separate controller tests. Complete page reads in that clock's microtasks.
class _PageReadController extends TaskImageController {
  _PageReadController(this.fixture)
    : super(
        task: _task,
        authorizationHeader: () => fixture.token,
        ownerID: () => fixture.owner,
        sourceCurrent: () => fixture.source,
        sourceChanges: fixture.changes,
        apiBaseUrl: 'https://unit.example',
        picker: fixture.picker,
        client: fixture.client,
        clock: () => fixture.now,
        pendingStore: fixture.pending,
      );
  final _Fixture fixture;

  @override
  Future<Uint8List?> readBytes(TaskImageReceipt receipt) async {
    if (!isReadable(receipt)) return null;
    if (fixture.tamper) {
      error = '图片暂不可用，请重试读取。';
      notifyListeners();
      return null;
    }
    return Uint8List.fromList(fixture.stored!);
  }
}

class _DraftRetentionFixture {
  _DraftRetentionFixture(this.bytes);
  final Uint8List bytes;
  final changes = ChangeNotifier();
  final controllers = <TaskImageDraftController>[];
  String? token = 'Bearer original-local-session',
      owner = _owner,
      city = 'unit-city';
  bool source = true;
  TaskImageDraftController create() {
    final draft = TaskImageDraftController(
      authorizationHeader: () => token,
      ownerID: () => owner,
      cityID: () => city,
      sourceCurrent: () => source,
      sourceChanges: changes,
      picker: TestMomentImagePicker(bytes),
    );
    controllers.add(draft);
    return draft;
  }

  void close() {
    for (final draft in controllers) {
      draft.dispose();
    }
    changes.dispose();
  }
}

void main() {

  test('shared codec leaves the Agent draft unapproved until its explicit review', () async {
    final fixture = _DraftRetentionFixture(await syntheticPrivatePNG());
    addTearDown(fixture.close);
    final draft = fixture.create();
    await draft.select();
    final ComposerImageDraftView view = draft;
    expect(view.localBytes, isNotNull);
    expect(draft.reviewed, isFalse);
    expect(draft.snapshot(), isNull);
    draft.setReviewed(true);
    expect(draft.snapshot(), isNotNull);
    await draft.mask(const ui.Rect.fromLTRB(0, 0, .5, .5));
    expect(draft.reviewed, isFalse);
    expect(draft.snapshot(), isNull);
  });

  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'shared 401 retains only editable original-owner local image across credential removal',
    () async {
      final f = _DraftRetentionFixture(await syntheticPrivatePNG());
      addTearDown(f.close);
      final original = f.create();
      await original.select();
      await original.mask(const ui.Rect.fromLTRB(0, 0, .5, .5));
      original.setReviewed(true);
      final approval = original.snapshot()!;
      final visibleBytes = original.localBytes!;
      final expectedBytes = Uint8List.fromList(visibleBytes);
      f.token = null;
      f.owner = null;
      f.source = false;
      final retained = original.retainLocal(rejectedOwner: _owner);
      expect(retained, isNotNull);
      addTearDown(retained!.dispose);
      expect(original.reviewed, isFalse);
      expect(approval.isCurrent, isFalse);
      f.changes.notifyListeners();
      expect(original.localBytes, isNull);
      f.owner = _owner;
      f.token = 'Bearer replacement-local-session';
      f.source = true;
      final replacement = f.create();
      expect(replacement.restoreLocal(retained), isTrue);
      expect(replacement.localBytes, orderedEquals(expectedBytes));
      expect(replacement.localBytes, isNot(same(visibleBytes)));
      expect(replacement.reviewed, isFalse);
      expect(replacement.snapshot(), isNull);
      expect(replacement.canSelect, isTrue);
      await replacement.mask(const ui.Rect.fromLTRB(.5, .5, 1, 1));
      expect(replacement.localBytes, isNot(orderedEquals(expectedBytes)));
      expect(replacement.reviewed, isFalse);
    },
  );
  for (final reason in [
    'logout',
    'differentRejectedOwner',
    'replacementAccount',
    'changedCity',
    'retired',
    'busy',
    'stillSignedIn',
    'disposed',
  ]) {
    test(
      'local image retention excludes $reason during signed-out transition',
      () async {
        final f = _DraftRetentionFixture(await syntheticPrivatePNG());
        addTearDown(f.close);
        final draft = f.create();
        await draft.select();
        draft.setReviewed(true);
        f.token = null;
        f.owner = null;
        f.source = false;
        String? rejectedOwner = _owner;
        switch (reason) {
          case 'logout':
            rejectedOwner = null;
          case 'differentRejectedOwner':
            rejectedOwner = _id;
          case 'replacementAccount':
            f.token = 'Bearer another-account';
            f.owner = _id;
          case 'changedCity':
            f.city = 'other-city';
          case 'retired':
            expect(draft.current, isFalse);
          case 'busy':
            draft.busy = true;
          case 'stillSignedIn':
            f.token = 'Bearer original-local-session';
            f.owner = _owner;
            f.source = true;
          case 'disposed':
            draft.dispose();
            f.controllers.remove(draft);
        }
        expect(draft.retainLocal(rejectedOwner: rejectedOwner), isNull);
      },
    );
  }
  test(
    'retained image cannot be restored into another owner or city',
    () async {
      final f = _DraftRetentionFixture(await syntheticPrivatePNG());
      addTearDown(f.close);
      final draft = f.create();
      await draft.select();
      draft.setReviewed(true);
      final retained = draft.retainLocal()!;
      addTearDown(retained.dispose);
      expect(draft.reviewed, isFalse);
      f.owner = _id;
      f.token = 'Bearer another-account';
      expect(f.create().restoreLocal(retained), isFalse);
      f.owner = _owner;
      f.city = 'other-city';
      expect(f.create().restoreLocal(retained), isFalse);
    },
  );
  test('only real current person native Task is eligible', () {
    expect(TaskImageController.eligible(_task, _owner), isTrue);
    expect(TaskImageController.eligible(_task, null), isFalse);
    expect(
      TaskImageController.eligible(
        const AgentTask(id: 'local-unit', query: 'test', status: 'COMPLETED'),
        _owner,
      ),
      isFalse,
    );
    expect(taskImagePrefix(_task, 1), isNull);
  });
  test('old API without image port never enables gallery attachment', () async {
    final f = _Fixture(await syntheticPrivatePNG())..listStatus = 404;
    await f.select();
    expect(f.controller.connected, isFalse);
    expect(f.picker.calls, 0);
    expect(f.calls.where((c) => c.startsWith('POST')), isEmpty);
    f.close();
  });
  test(
    'local preview mask and removal send no private pixels or queries',
    () async {
      final f = _Fixture(await syntheticPrivatePNG());
      await f.select();
      final before = sha256.convert(f.controller.localBytes!).toString();
      await f.controller.mask(const ui.Rect.fromLTRB(0, 0, .5, .5));
      expect(
        sha256.convert(f.controller.localBytes!).toString(),
        isNot(before),
      );
      expect(f.controller.reviewed, isFalse);
      expect(f.calls.where((c) => !c.startsWith('GET')), isEmpty);
      f.controller.removeLocal();
      expect(f.controller.localBytes, isNull);
      expect(_task.messages.last.text, '合成原回答');
      f.close();
    },
  );
  test(
    'explicit prepare and save attach exact native user prefix and restore bytes',
    () async {
      final f = _Fixture(await syntheticPrivatePNG());
      await f.prepare();
      expect(f.input!.keys.toSet(), {
        'operationId',
        'messageIndex',
        'prefixDigest',
        'expectedTaskUpdatedAt',
        'mimeType',
        'byteSize',
        'sha256',
        'pixelRisk',
        'purpose',
      });
      expect(f.input!['prefixDigest'], taskImagePrefix(_task, 0));
      expect(f.controller.canSave, isFalse);
      f.controller.setReviewed(true);
      await f.controller.save();
      expect(f.controller.images.single.index, 0);
      expect(f.controller.localBytes, isNull);
      final saved = f.controller.images.single;
      expect(await f.controller.readBytes(saved), f.stored);
      await f.controller.load();
      expect(f.controller.images.single.prefix, taskImagePrefix(_task, 0));
      expect(f.calls.where((c) => c.contains('/agent/tasks')), isEmpty);
      expect(f.calls.where((c) => c.startsWith('POST')), hasLength(1));
      expect(f.calls.where((c) => c.startsWith('PUT')), hasLength(1));
      await f.controller.remove(saved);
      expect(f.controller.images, isEmpty);
      f.close();
    },
  );
  test(
    'uncertain preview retains local image and only GET recovers without resend',
    () async {
      final f = _Fixture(await syntheticPrivatePNG())..failPreview = true;
      await f.prepare();
      final retained = f.controller.localBytes;
      expect(f.controller.uncertain, isTrue);
      await f.controller.prepare();
      await f.controller.save();
      expect(f.calls.where((c) => c.startsWith('POST')), hasLength(1));
      expect(f.calls.where((c) => c.startsWith('PUT')), isEmpty);
      await f.controller.readOperation();
      expect(identical(retained, f.controller.localBytes), isTrue);
      expect(f.controller.preview?.status, 'preview');
      expect(f.controller.canSave, isFalse);
      f.close();
    },
  );
  test(
    'uncertain committed save resolves via canonical GET exactly once',
    () async {
      final f = _Fixture(await syntheticPrivatePNG())..failSave = true;
      await f.prepare();
      f.controller.setReviewed(true);
      await f.controller.save();
      expect(f.controller.uncertain, isTrue);
      expect(f.controller.localBytes, isNotNull);
      await f.controller.save();
      await f.controller.readOperation();
      expect(f.controller.uncertain, isFalse);
      expect(f.controller.images, hasLength(1));
      expect(f.controller.localBytes, isNull);
      expect(f.calls.where((c) => c.startsWith('PUT')), hasLength(1));
      f.close();
    },
  );
  test(
    'display association follows original user messages and allows assistant presentation changes',
    () {
      expect(
        taskImageDisplayMatches(_task, [
          _task.messages.first,
          const AgentMessage(role: 'assistant', text: '同一回答的结果短句'),
        ], 0),
        isTrue,
      );
      expect(
        taskImageDisplayMatches(_task, const [
          AgentMessage(role: 'user', text: '另一条问题'),
        ], 0),
        isFalse,
      );
    },
  );
  test(
    'invalidated preview recovery keeps original operation and never uploads again',
    () async {
      final f = _Fixture(await syntheticPrivatePNG())..failPreview = true;
      await f.prepare();
      f.invalidated = true;
      await f.controller.readOperation();
      expect(f.controller.preview?.status, 'invalidated_preview');
      f.controller.setReviewed(true);
      expect(f.controller.canSave, isFalse);
      await f.controller.save();
      expect(f.calls.where((c) => c.startsWith('POST')), hasLength(1));
      expect(f.calls.where((c) => c.startsWith('PUT')), isEmpty);
      await f.controller.remove(f.controller.preview!);
      expect(f.controller.preview, isNull);
      expect(f.controller.pendingOperations, isEmpty);
      f.close();
    },
  );
  test(
    'cold controller resolves original uncertain save with metadata and GET only',
    () async {
      final f = _Fixture(await syntheticPrivatePNG())..failSave = true;
      await f.prepare();
      f.controller.setReviewed(true);
      await f.controller.save();
      expect(f.controller.uncertain, isTrue);
      f.controller.dispose();
      final restored = TaskImageController(
        task: _task,
        authorizationHeader: () => f.token,
        ownerID: () => f.owner,
        sourceCurrent: () => f.source,
        sourceChanges: f.changes,
        apiBaseUrl: 'https://unit.example',
        client: f.client,
        pendingStore: f.pending,
        clock: () => _now,
      );
      final before = f.calls.length;
      await restored.load();
      expect(restored.pendingOperations, hasLength(1));
      expect(restored.localBytes, isNull);
      expect(restored.canSelect, isFalse);
      restored.choosePending(restored.pendingOperations.single);
      await restored.readOperation();
      expect(restored.images, hasLength(1));
      expect(restored.pendingOperations, isEmpty);
      expect(f.calls.skip(before).every((c) => c.startsWith('GET')), isTrue);
      restored.dispose();
      f.changes.dispose();
      f.client.close();
    },
  );
  test(
    'gallery reply after source ABA never attaches to later context',
    () async {
      final f = _Fixture(await syntheticPrivatePNG());
      await f.controller.load();
      f.picker.pending = Completer<MomentImageSelection?>();
      final selecting = f.controller.select();
      await Future<void>.delayed(Duration.zero);
      f.source = false;
      f.changes.notifyListeners();
      f.source = true;
      f.changes.notifyListeners();
      f.picker.pending!.complete(MomentImageSelection(f.picker.bytes));
      await selecting;
      expect(f.controller.current, isFalse);
      expect(f.controller.localBytes, isNull);
      expect(f.calls.where((c) => !c.startsWith('GET')), isEmpty);
      f.close();
    },
  );
  test(
    'binary reply that arrives after retention deadline is discarded',
    () async {
      final f = _Fixture(await syntheticPrivatePNG());
      await f.prepare();
      f.controller.setReviewed(true);
      await f.controller.save();
      final image = f.controller.images.single;
      f.waitingBinary = Completer<http.Response>();
      final reading = f.controller.readBytes(image);
      await Future<void>.delayed(Duration.zero);
      f.now = image.retainUntil;
      f.waitingBinary!.complete(
        http.Response.bytes(
          f.stored!,
          200,
          headers: {'content-type': 'image/png'},
        ),
      );
      expect(await reading, isNull);
      expect(f.controller.isReadable(image), isFalse);
      f.close();
    },
  );
  testWidgets(
    'same-key image page replacement never displays the old controller binary reply',
    (tester) async {
      final bytes = await tester.runAsync(syntheticPrivatePNG) ?? Uint8List(0);
      final a = _Fixture(bytes), b = _Fixture(bytes);
      await tester.runAsync(a.prepare);
      a.controller.setReviewed(true);
      await tester.runAsync(a.controller.save);
      await tester.runAsync(b.controller.load);
      a.waitingBinary = Completer<http.Response>();
      await tester.pumpWidget(
        MaterialApp(
          home: TaskImagePage(
            key: const ValueKey('same-image-page'),
            controller: a.controller,
            initialImage: a.controller.images.single,
          ),
        ),
      );
      await tester.pump();
      await tester.pumpWidget(
        MaterialApp(
          home: TaskImagePage(
            key: const ValueKey('same-image-page'),
            controller: b.controller,
          ),
        ),
      );
      a.waitingBinary!.complete(
        http.Response.bytes(
          a.stored!,
          200,
          headers: {'content-type': 'image/png'},
        ),
      );
      await tester.pumpAndSettle();
      expect(find.byType(Image), findsNothing);
      expect(find.text('尚无已保存图片。'), findsOneWidget);
      expect(b.calls.where((c) => c.endsWith('/content')), isEmpty);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      a.close();
      b.close();
    },
  );
  test(
    'binary content tamper rejects image and never becomes a model payload',
    () async {
      final f = _Fixture(await syntheticPrivatePNG());
      await f.prepare();
      f.controller.setReviewed(true);
      await f.controller.save();
      f.tamper = true;
      expect(await f.controller.readBytes(f.controller.images.single), isNull);
      expect(f.controller.error, isNotNull);
      expect(f.calls.every((c) => c.contains('/v1/me/agent-tasks/')), isTrue);
      f.close();
    },
  );
  testWidgets(
    'cancelled removal still clears an opened image at its retention deadline',
    (tester) async {
      tester.view.physicalSize = const Size(800, 1600);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final f = _Fixture(
        await tester.runAsync(syntheticPrivatePNG) ?? Uint8List(0),
        pageReader: true,
      );
      await tester.runAsync(f.prepare);
      f.controller.setReviewed(true);
      await tester.runAsync(f.controller.save);
      final image = f.controller.images.single;
      await tester.pumpWidget(
        MaterialApp(
          home: TaskImagePage(controller: f.controller, initialImage: image),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.byType(Image), findsOneWidget);
      await tester.ensureVisible(find.byTooltip('移除图片'));
      await tester.tap(find.byTooltip('移除图片'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('返回'));
      await tester.pumpAndSettle();
      expect(find.byType(Image), findsOneWidget);
      f.now = image.retainUntil;
      await tester.pump(const Duration(days: 1));
      expect(find.byType(Image), findsNothing);
      expect(f.calls.where((c) => c.startsWith('DELETE')), isEmpty);
      await tester.pumpWidget(const SizedBox());
      f.close();
    },
  );
  testWidgets(
    'failed reread still clears the previous pixels at their retention deadline',
    (tester) async {
      tester.view.physicalSize = const Size(800, 1600);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final f = _Fixture(
        await tester.runAsync(syntheticPrivatePNG) ?? Uint8List(0),
        pageReader: true,
      );
      await tester.runAsync(f.prepare);
      f.controller.setReviewed(true);
      await tester.runAsync(f.controller.save);
      final image = f.controller.images.single;
      await tester.pumpWidget(
        MaterialApp(
          home: TaskImagePage(controller: f.controller, initialImage: image),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.byType(Image), findsOneWidget);
      f.tamper = true;
      final tile = find.text('第 1 条消息的图片');
      await tester.ensureVisible(tile);
      await tester.tap(tile);
      await tester.pumpAndSettle();
      expect(f.controller.error, isNotNull);
      expect(find.byType(Image), findsOneWidget);
      f.now = image.retainUntil;
      await tester.pump(const Duration(days: 1));
      expect(find.byType(Image), findsNothing);
      await tester.pumpWidget(const SizedBox());
      f.close();
    },
  );
  testWidgets(
    'image page marks human-only capability and retires pixels on source change',
    (tester) async {
      final f = _Fixture(
        await tester.runAsync(syntheticPrivatePNG) ?? Uint8List(0),
      );
      await tester.runAsync(f.prepare);
      await tester.pumpWidget(
        MaterialApp(home: TaskImagePage(controller: f.controller)),
      );
      expect(find.text('仅本人可见'), findsOneWidget);
      expect(find.byType(Image), findsOneWidget);
      f.source = false;
      f.changes.notifyListeners();
      await tester.pump();
      expect(find.byType(Image), findsNothing);
      expect(find.text('会话或账户已变化，请返回后重新打开。'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      f.close();
    },
  );
}
