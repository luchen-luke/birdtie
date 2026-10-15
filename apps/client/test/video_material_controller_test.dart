import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:birdtie_client/src/content/video_material_controller.dart';
import 'package:birdtie_client/src/content/video_material_picker.dart';
import 'package:birdtie_client/src/content/video_material_pending_store.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'video_material_picker_test.dart';

const videoOwner = '11111111-1111-4111-8111-111111111111';
const videoID = '22222222-2222-4222-8222-222222222222';
const videoOperation = '33333333-3333-4333-8333-333333333333';
final videoPolicy = List.filled(64, 'b').join();
Map<String, dynamic> videoCapabilities({bool available = true}) => {
  'available': available,
  'providerLabel': available ? '合成服务' : '',
  'region': available ? '合成区域' : '',
  'uploadOrigin': available ? 'https://storage.invalid' : '',
  'storagePolicyId': available ? videoPolicy : '',
  'maxBytes': videoMaterialMaxBytes,
  'maxDurationMs': videoMaterialMaxDurationMS,
  'mimeType': 'video/mp4',
  'purpose': videoMaterialPurpose,
};
Map<String, dynamic> videoReceipt({
  String status = 'quarantine',
  int revision = 1,
  String op = videoOperation,
  String owner = videoOwner,
}) {
  final selected = VideoMaterialSelection(
    bytes: syntheticVideoBytes(),
    preview: FakeVideoPreview(),
  );
  return {
    'id': videoID,
    'ownerAccountId': owner,
    'operationId': op,
    'status': status,
    'revision': revision,
    'byteSize': selected.bytes.length,
    'sha256': selected.sha256Hex,
    'durationMs': 1000,
    'mimeType': 'video/mp4',
    'purpose': videoMaterialPurpose,
    'confirmedPrivateSave': true,
    'storagePolicyId': videoPolicy,
    'width': status == 'ready_private' ? 640 : 0,
    'height': status == 'ready_private' ? 360 : 0,
    'uploadExpiresAt': DateTime.now()
        .toUtc()
        .add(const Duration(minutes: 1))
        .toIso8601String(),
    'retainUntil': DateTime.now()
        .toUtc()
        .add(const Duration(days: 1))
        .toIso8601String(),
  };
}

http.Response videoResponse(dynamic data, {int code = 200}) =>
    http.Response.bytes(
      utf8.encode(jsonEncode({'data': data})),
      code,
      headers: {'content-type': 'application/json'},
    );
VideoMaterialController videoController(
  http.Client client, {
  FakeVideoPicker? picker,
  http.Client? upload,
  VideoMaterialPendingStore? store,
  Listenable? changes,
  String? Function()? owner,
  String? Function()? token,
  bool Function()? source,
}) => VideoMaterialController(
  authorizationHeader: token ?? () => 'Bearer synthetic-person',
  ownerID: owner ?? () => videoOwner,
  sourceCurrent: source ?? () => true,
  identityChanges: changes ?? ChangeNotifier(),
  client: client,
  apiBaseUrl: 'https://api.invalid',
  picker: picker ?? FakeVideoPicker(),
  pendingStore: store ?? MemoryVideoMaterialPendingStore(),
  uploadClient: upload,
);
void main() {
  test('Now本机视频在同账号续期保留，换账号清空且不调用私有接口', () async {
    var owner = videoOwner, token = 'Bearer first';
    var requests = 0;
    final changes = ChangeNotifier();
    final preview = FakeVideoPreview();
    final picker = FakeVideoPicker()
      ..next = VideoMaterialSelection(
        bytes: syntheticVideoBytes(),
        preview: preview,
      );
    final client = MockClient((request) async {
      ++requests;
      throw StateError('本机选材不应请求私有视频服务');
    });
    final c = VideoMaterialController(
      authorizationHeader: () => token,
      ownerID: () => owner,
      sourceCurrent: () => true,
      identityChanges: changes,
      localSelectionOnly: true,
      client: client,
      picker: picker,
      apiBaseUrl: 'https://api.invalid',
      pendingStore: MemoryVideoMaterialPendingStore(),
    );
    try {
      await c.initialize();
      await c.select();
      final selected = c.selection!;
      final original = Uint8List.fromList(selected.bytes);
      token = 'Bearer renewed';
      changes.notifyListeners();
      expect(c.current, isTrue);
      expect(c.selection, same(selected));
      expect(c.selection!.bytes, original);
      expect(preview.closed, isFalse);
      expect(c.canSave, isFalse);
      expect(await c.save(), isFalse);
      await c.load();
      expect(requests, 0);
      owner = videoID;
      changes.notifyListeners();
      await Future<void>.value();
      expect(c.current, isFalse);
      expect(c.selection, isNull);
      expect(selected.bytes.every((byte) => byte == 0), isTrue);
      expect(preview.closed, isTrue);
      expect(requests, 0);
    } finally {
      c.dispose();
      client.close();
      changes.dispose();
    }
  });
  test('拒绝私有读取后退休原批准并清空旧账号内容', () async {
    var revoked = false;
    final client = MockClient(
      (r) async => revoked
          ? videoResponse(null, code: 401)
          : videoResponse({
              'items': [videoReceipt(status: 'ready_private')],
            }),
    );
    final c = videoController(client);
    try {
      await c.load();
      expect(c.items.length, 1);
      revoked = true;
      await c.load();
      expect(c.current, isFalse);
      expect(c.items, isEmpty);
      expect(c.active, isNull);
    } finally {
      c.dispose();
      client.close();
    }
  });
  test('来源失效释放本机预览，重新变有效也不恢复旧批准', () async {
    var valid = true;
    final signal = ChangeNotifier();
    final preview = FakeVideoPreview();
    final picker = FakeVideoPicker()
      ..next = VideoMaterialSelection(
        bytes: syntheticVideoBytes(),
        preview: preview,
      );
    final client = MockClient((r) async => videoResponse(videoCapabilities()));
    final c = videoController(
      client,
      picker: picker,
      changes: signal,
      source: () => valid,
    );
    try {
      await c.select();
      valid = false;
      signal.notifyListeners();
      await Future<void>.value();
      expect(c.current, isFalse);
      expect(preview.closed, isTrue);
      expect(c.selection, isNull);
      valid = true;
      signal.notifyListeners();
      expect(c.current, isFalse);
    } finally {
      c.dispose();
      client.close();
      signal.dispose();
    }
  });
  test('服务关闭仍能选取预览移除，绝不自动上传', () async {
    var writes = 0;
    final picker = FakeVideoPicker()
      ..next = VideoMaterialSelection(
        bytes: syntheticVideoBytes(),
        preview: FakeVideoPreview(),
      );
    final client = MockClient((r) async {
      if (r.method != 'GET') writes++;
      return videoResponse(videoCapabilities(available: false));
    });
    final c = videoController(client, picker: picker);
    try {
      await c.initialize();
      await c.select();
      expect(c.selection, isNotNull);
      expect(c.canSave, isFalse);
      expect(await c.save(), isFalse);
      await c.togglePreview();
      expect(c.selection!.preview.playing, isTrue);
      final old = c.selection!.preview as FakeVideoPreview;
      await c.removeLocal();
      expect(old.closed, isTrue);
      expect(writes, 0);
    } finally {
      c.dispose();
      client.close();
    }
  });
  test('私有保存真实PUT与安全检查受理分开，存储不接收账号会话', () async {
    String? op;
    var puts = 0, completes = 0;
    final picker = FakeVideoPicker()
      ..next = VideoMaterialSelection(
        bytes: syntheticVideoBytes(),
        preview: FakeVideoPreview(),
      );
    final store = MemoryVideoMaterialPendingStore();
    final upload = MockClient((r) async {
      puts++;
      expect(r.method, 'PUT');
      expect(r.headers['Authorization'], isNull);
      expect(r.headers['X-Upload-Proof'], 'synthetic-proof');
      expect(r.bodyBytes, syntheticVideoBytes());
      return http.Response('', 204);
    });
    final client = MockClient((r) async {
      if (r.url.path.endsWith('capabilities'))
        return videoResponse(videoCapabilities());
      if (r.method == 'POST' && !r.url.path.endsWith('/complete')) {
        final p = jsonDecode(r.body) as Map<String, dynamic>;
        op = p['operationId'] as String;
        expect(p['confirmedPrivateSave'], isTrue);
        expect(p['storagePolicyId'], videoPolicy);
        expect(
          (await store.read(
            'https://api.invalid',
            videoOwner,
          )).single.operationID,
          op,
        );
        final v = videoReceipt(op: op!);
        return videoResponse({
          'receipt': v,
          'uploadUrl': 'https://storage.invalid/put',
          'uploadHeaders': {'X-Upload-Proof': 'synthetic-proof'},
          'expiresAt': v['uploadExpiresAt'],
        }, code: 201);
      }
      if (r.method == 'POST') {
        completes++;
        return videoResponse(
          videoReceipt(op: op!, status: 'queued', revision: 2),
          code: 202,
        );
      }
      return videoResponse(
        videoReceipt(op: op!, status: 'ready_private', revision: 4),
      );
    });
    final c = videoController(
      client,
      picker: picker,
      upload: upload,
      store: store,
    );
    try {
      await c.initialize();
      await c.select();
      expect(await c.save(), isFalse);
      expect(c.active!.status, 'queued');
      expect(puts, 1);
      expect(completes, 1);
      expect(c.canSave, isFalse);
      expect((await store.read('https://api.invalid', videoOwner)).length, 1);
      await c.recover();
      expect(c.active!.status, 'ready_private');
      expect(await store.read('https://api.invalid', videoOwner), isEmpty);
    } finally {
      c.dispose();
      client.close();
      upload.close();
    }
  });
  test('未知回执保留原operation和本机字节，重试不另建记录', () async {
    final bodies = <String>[];
    final picker = FakeVideoPicker()
      ..next = VideoMaterialSelection(
        bytes: syntheticVideoBytes(),
        preview: FakeVideoPreview(),
      );
    final store = MemoryVideoMaterialPendingStore();
    final client = MockClient((r) async {
      if (r.method == 'GET') return videoResponse(videoCapabilities());
      bodies.add(r.body);
      throw http.ClientException('unknown reply');
    });
    final c = videoController(client, picker: picker, store: store);
    try {
      await c.initialize();
      await c.select();
      await c.save();
      expect(c.uncertain, isTrue);
      expect(c.selection, isNotNull);
      await c.save();
      expect(bodies.length, 2);
      expect(bodies[1], bodies[0]);
      expect((await store.read('https://api.invalid', videoOwner)).length, 1);
    } finally {
      c.dispose();
      client.close();
    }
  });
  test('账号或来源切换清空视频，迟到本机选取被释放', () async {
    final signal = ChangeNotifier();
    var owner = videoOwner;
    final delayed = Completer<VideoMaterialSelection?>();
    final picker = FakeVideoPicker()..delayed = delayed.future;
    final client = MockClient((r) async => videoResponse(videoCapabilities()));
    final c = videoController(
      client,
      picker: picker,
      changes: signal,
      owner: () => owner,
    );
    try {
      final task = c.select();
      owner = videoID;
      signal.notifyListeners();
      final preview = FakeVideoPreview();
      delayed.complete(
        VideoMaterialSelection(bytes: syntheticVideoBytes(), preview: preview),
      );
      await task;
      expect(c.current, isFalse);
      expect(c.selection, isNull);
      expect(preview.closed, isTrue);
      owner = videoOwner;
      signal.notifyListeners();
      expect(c.current, isFalse);
    } finally {
      c.dispose();
      client.close();
      signal.dispose();
    }
  });
  test('超时长或未知编码无法进入草稿', () async {
    final picker = FakeVideoPicker()
      ..next = VideoMaterialSelection(
        bytes: syntheticVideoBytes(),
        preview: FakeVideoPreview(ms: 30001),
      );
    final client = MockClient((r) async => videoResponse(videoCapabilities()));
    final c = videoController(client, picker: picker);
    try {
      await c.select();
      expect(c.selection, isNull);
      expect(c.error, isNotNull);
      expect((picker.next!.preview as FakeVideoPreview).closed, isTrue);
      picker.next = VideoMaterialSelection(
        bytes: syntheticVideoBytes(codec: 'hvc1'),
        preview: FakeVideoPreview(),
      );
      await c.select();
      expect(c.selection, isNull);
    } finally {
      c.dispose();
      client.close();
    }
  });
  test('本人已保存视频逐条打开、校验私有字节且删除释放播放器', () async {
    final client = MockClient((r) async {
      if (r.url.path.endsWith('capabilities'))
        return videoResponse(videoCapabilities());
      if (r.url.path.endsWith('/content'))
        return http.Response.bytes(
          syntheticVideoBytes(),
          200,
          headers: {
            'content-type': 'video/mp4',
            'content-length': '${syntheticVideoBytes().length}',
          },
        );
      if (r.method == 'DELETE')
        return videoResponse(
          videoReceipt(status: 'deleting', revision: 5),
          code: 202,
        );
      return videoResponse(videoReceipt(status: 'ready_private', revision: 4));
    });
    final c = videoController(client);
    try {
      await c.initialize();
      final v = VideoMaterialReceipt.fromJson(
        videoReceipt(status: 'ready_private', revision: 4),
      );
      expect(await c.openStoredPreview(v), isTrue);
      expect(c.active!.id, v.id);
      expect(c.canSave, isFalse);
      final preview = c.selection!.preview as FakeVideoPreview;
      await c.delete(v);
      expect(c.active!.status, 'deleting');
      expect(c.selection, isNull);
      expect(preview.closed, isTrue);
    } finally {
      c.dispose();
      client.close();
    }
  });
  test('读取失败保留本人列表，不能接纳其他owner回执', () async {
    var fail = false;
    final client = MockClient(
      (r) async => fail
          ? videoResponse(null, code: 503)
          : videoResponse({
              'items': [videoReceipt(status: 'ready_private')],
            }),
    );
    final c = videoController(client);
    try {
      await c.load();
      expect(c.items.length, 1);
      fail = true;
      await c.load();
      expect(c.items.length, 1);
      expect(c.error, isNotNull);
    } finally {
      c.dispose();
      client.close();
    }
  });
}
