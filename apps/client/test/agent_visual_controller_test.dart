import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:crypto/crypto.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/agent_visual_controller.dart';
import 'package:birdtie_client/src/workspace/agent_visual_pending_store.dart';
import 'friend_image_test.dart' show imageFixtureBytes;

const visualOwner = '85000000-0000-4000-8000-000000000001';
const visualSource = '85000000-0000-4000-8000-000000000002';
const visualPreview = '85000000-0000-4000-8000-000000000003';
const visualPixel = '85000000-0000-4000-8000-000000000004';
const visualOldOperation = '85000000-0000-4000-8000-000000000005';

class VisualMemoryPending implements AgentVisualPendingStore {
  String? value;
  bool fail = false;
  @override
  Future<String?> read(String origin, String owner, String source) async =>
      value;
  @override
  Future<void> write(
    String origin,
    String owner,
    String source,
    String operation,
  ) async {
    if (fail) throw StateError('unavailable');
    value = operation;
  }

  @override
  Future<void> delete(
    String origin,
    String owner,
    String source,
    String operation,
  ) async {
    if (value == operation) value = null;
  }
}

http.Response visualWire(Object data, {int status = 200}) =>
    http.Response.bytes(
      utf8.encode(jsonEncode({'data': data})),
      status,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );
Map<String, dynamic> visualPreviewData(
  Map<String, dynamic> input,
  Uint8List bytes,
  DateTime now,
) => {
  'schemaVersion': 1,
  'previewId': visualPreview,
  'version': 1,
  'status': 'PREVIEW',
  'clientDraftId': input['clientDraftId'],
  'clientDraftRevision': input['clientDraftRevision'],
  'userText': input['userText'],
  'purpose': agentVisualPurpose,
  'egressPurpose': 'MODEL_VISUAL_PIXEL_EGRESS',
  'payloadDigest': 'a' * 64,
  'memoryWriteAllowed': false,
  'placeFactWriteAllowed': false,
  'pixel': {
    'id': visualPixel,
    'revision': 1,
    'sha256': sha256.convert(bytes).toString(),
    'mimeType': 'image/png',
    'width': 4,
    'height': 4,
    'byteSize': bytes.length,
    'previewContentPath': '/v1/me/agent-visual/previews/$visualPreview/pixels',
  },
  'destination': {
    'provider': 'Tencent',
    'model': 'hy-vision-2.0-instruct',
    'wireContract': 'VISION_V1',
    'region': 'CN',
    'retention': 'UNKNOWN',
  },
  'budget': {
    'currency': 'CNY',
    'inputTokenUpper': 24576,
    'outputTokenUpper': 768,
    'costMicrosUpper': 210432,
    'priceVersion': 'synthetic-v1',
    'priceExpiresAt': now.add(const Duration(hours: 1)).toIso8601String(),
  },
  'createdAt': now.toIso8601String(),
  'expiresAt': now.add(const Duration(minutes: 2)).toIso8601String(),
};

class VisualFixture {
  VisualFixture(this.bytes, {this.text = ''}) {
    client = MockClient((r) async {
      calls.add(r);
      if (r.method == 'POST' && r.url.path.endsWith('/previews')) {
        if (previewWait != null) return previewWait!.future;
        if (unavailable) return visualWire({}, status: 503);
        final p = visualPreviewData(jsonDecode(r.body), bytes, now);
        if (wrongPurpose) p['purpose'] = 'HUMAN_TASK_MESSAGE_ATTACHMENT';
        return visualWire(p);
      }
      if (r.url.path.endsWith('/pixels'))
        return http.Response.bytes(
          bytes,
          200,
          headers: {'content-type': 'image/png'},
        );
      if (r.method == 'POST') {
        dispatched = jsonDecode(r.body);
        if (loseDispatch) throw StateError('response lost');
        return visualWire(completed(dispatched!['operationId']));
      }
      return visualWire(completed(r.url.path.split('/').last));
    });
    controller = AgentVisualController(
      authorizationHeader: () => token,
      ownerID: () => owner,
      sourceContextID: visualSource,
      sourceContextType: 'CITY',
      sourceCurrent: () => sourceCurrent,
      draftCurrent: () => draftCurrent,
      identityChanges: changes,
      bytes: bytes,
      userText: text,
      clientDraftRevision: 2,
      masked: true,
      client: client,
      apiBaseUrl: 'https://synthetic.test',
      pendingStore: pending,
      clock: () => now,
    );
  }
  final Uint8List bytes;
  final String text;
  DateTime now = DateTime.utc(2026, 10, 13);
  final changes = ValueNotifier<int>(0), pending = VisualMemoryPending();
  final calls = <http.Request>[];
  late final MockClient client;
  late final AgentVisualController controller;
  String? token = 'Bearer synthetic', owner = visualOwner;
  bool sourceCurrent = true,
      draftCurrent = true,
      unavailable = false,
      loseDispatch = false,
      wrongPurpose = false;
  Completer<http.Response>? previewWait;
  Map<String, dynamic>? dispatched;
  Map<String, dynamic> completed(String op) => {
    'schemaVersion': 1,
    'operationId': op,
    'previewId': visualPreview,
    'version': 1,
    'status': 'COMPLETED',
    'report': {
      'summary': '可见内容，仅为观察',
      'observations': [
        {
          'kind': 'VISIBLE_OBJECT',
          'value': '合成物体',
          'confidence': {'kind': 'QUALITATIVE', 'level': 'UNKNOWN'},
          'source': {
            'type': 'APPROVED_PIXELS',
            'id': visualPixel,
            'revision': 1,
            'sha256': sha256.convert(bytes).toString(),
          },
          'observedAt': now.toIso8601String(),
        },
      ],
      'unknowns': ['拍摄地点未知', '拍摄时间未知'],
    },
  };
  void approvePixels() => controller.setPixelVisible(visualPreview, 1, true);
  void dispose() {
    controller.dispose();
    client.close();
    changes.dispose();
  }
}

void main() {
  final binding = TestWidgetsFlutterBinding.ensureInitialized();
  setUp(
    () => binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed),
  );
  for (final text in ['', '图片里的文字是什么？']) {
    test('pure/image-text single approved wire; no text Task; $text', () async {
      final f = VisualFixture(await imageFixtureBytes(), text: text);
      addTearDown(f.dispose);
      await f.controller.prepare();
      expect(f.controller.preview, isNotNull);
      expect(f.calls.where((r) => r.url.path.contains('/analyze')), isEmpty);
      expect(jsonDecode(f.calls.first.body)['userText'], text);
      expect(jsonDecode(f.calls.first.body).containsKey('taskId'), isFalse);
      await f.controller.analyze();
      expect(f.dispatched, isNull);
      f.approvePixels();
      await f.controller.analyze();
      expect(f.controller.report!['unknowns'], contains('拍摄地点未知'));
      expect(f.pending.value, isNull);
      expect(f.dispatched!['pixelSha256'], sha256.convert(f.bytes).toString());
      expect(f.dispatched!['purpose'], agentVisualPurpose);
    });
  }
  test(
    'missing provider retains exact local draft without fake report',
    () async {
      final f = VisualFixture(await imageFixtureBytes())..unavailable = true;
      addTearDown(f.dispose);
      await f.controller.prepare();
      expect(f.controller.preview, isNull);
      expect(f.controller.report, isNull);
      expect(f.controller.localPixels, orderedEquals(f.bytes));
      expect(f.controller.error, contains('尚未配置'));
    },
  );
  test(
    'human attachment purpose is never accepted as visual approval',
    () async {
      final f = VisualFixture(await imageFixtureBytes())..wrongPurpose = true;
      addTearDown(f.dispose);
      await f.controller.prepare();
      expect(f.controller.preview, isNull);
      expect(f.controller.canAnalyze, isFalse);
      expect(f.calls.any((r) => r.url.path.endsWith('/pixels')), isFalse);
    },
  );
  test(
    'lost dispatch is GET only, durable operation is not replayed',
    () async {
      final f = VisualFixture(await imageFixtureBytes())..loseDispatch = true;
      addTearDown(f.dispose);
      await f.controller.prepare();
      f.approvePixels();
      await f.controller.analyze();
      expect(f.controller.uncertain, isTrue);
      expect(f.pending.value, isNotNull);
      final count = f.calls.where((r) => r.method == 'POST').length;
      await f.controller.analyze();
      await f.controller.prepare();
      await f.controller.recover();
      expect(f.calls.where((r) => r.method == 'POST').length, count);
      expect(f.calls.last.method, 'GET');
      expect(f.controller.uncertain, isFalse);
      expect(f.controller.report, isNotNull);
    },
  );
  test(
    'reopened draft detects durable pending before sending any pixels',
    () async {
      final f = VisualFixture(await imageFixtureBytes());
      addTearDown(f.dispose);
      f.pending.value = visualOldOperation;
      await f.controller.prepare();
      expect(f.calls, isEmpty);
      expect(f.controller.uncertain, isTrue);
      await f.controller.recover();
      expect(f.calls.single.method, 'GET');
      expect(f.controller.report, isNotNull);
    },
  );
  test('pending-store failure never dispatches', () async {
    final f = VisualFixture(await imageFixtureBytes());
    addTearDown(f.dispose);
    await f.controller.prepare();
    f.approvePixels();
    f.pending.fail = true;
    await f.controller.analyze();
    expect(f.dispatched, isNull);
    expect(f.controller.uncertain, isFalse);
    expect(f.controller.error, contains('尚未发送'));
  });
  for (final boundary in [
    'owner ABA',
    'draft edit',
    'source ABA',
    'background',
  ]) {
    test('late preview retires on $boundary', () async {
      final f = VisualFixture(await imageFixtureBytes());
      addTearDown(f.dispose);
      f.previewWait = Completer<http.Response>();
      final running = f.controller.prepare();
      await Future<void>.delayed(Duration.zero);
      final input = jsonDecode(f.calls.single.body) as Map<String, dynamic>;
      switch (boundary) {
        case 'owner ABA':
          f.owner = null;
          f.changes.value++;
          f.owner = visualOwner;
          f.changes.value++;
        case 'draft edit':
          f.draftCurrent = false;
          f.changes.value++;
        case 'source ABA':
          f.sourceCurrent = false;
          f.changes.value++;
          f.sourceCurrent = true;
          f.changes.value++;
        case 'background':
          binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
          binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      }
      f.previewWait!.complete(
        visualWire(visualPreviewData(input, f.bytes, f.now)),
      );
      await running;
      expect(f.controller.preview, isNull);
      expect(f.controller.report, isNull);
      expect(f.controller.current, isFalse);
      expect(f.calls.length, 1);
    });
  }
  test('expired preview cannot dispatch even after visible approval', () async {
    final f = VisualFixture(await imageFixtureBytes());
    addTearDown(f.dispose);
    await f.controller.prepare();
    f.approvePixels();
    f.now = f.now.add(const Duration(minutes: 3));
    await f.controller.analyze();
    expect(f.dispatched, isNull);
  });
}
