import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

final videoMaterialUUID = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
final videoMaterialDigest = RegExp(r'^[0-9a-f]{64}$');

class VideoMaterialPending {
  VideoMaterialPending(this.ownerID, Map<String, dynamic> input)
    : input = Map.unmodifiable(input) {
    if (!videoMaterialUUID.hasMatch(ownerID) ||
        input.length != 8 ||
        !const {
          'operationId',
          'mimeType',
          'byteSize',
          'sha256',
          'durationMs',
          'purpose',
          'confirmedPrivateSave',
          'storagePolicyId',
        }.every(input.containsKey) ||
        !videoMaterialUUID.hasMatch(input['operationId'] as String) ||
        input['mimeType'] != 'video/mp4' ||
        input['byteSize'] is! int ||
        input['byteSize'] < 1 ||
        input['byteSize'] > 20 * 1024 * 1024 ||
        !videoMaterialDigest.hasMatch(input['sha256'] as String) ||
        input['durationMs'] is! int ||
        input['durationMs'] < 1 ||
        input['durationMs'] > 30000 ||
        input['purpose'] != 'HUMAN_PRIVATE_VIDEO_MATERIAL' ||
        input['confirmedPrivateSave'] != true ||
        !videoMaterialDigest.hasMatch(input['storagePolicyId'] as String))
      throw const FormatException('视频恢复记录无法核验');
  }
  final String ownerID;
  final Map<String, dynamic> input;
  String get operationID => input['operationId'] as String;
  Map<String, dynamic> toJson() => {'ownerId': ownerID, 'input': input};
  factory VideoMaterialPending.fromJson(Map<String, dynamic> p) {
    if (p.length != 2 ||
        p['ownerId'] is! String ||
        p['input'] is! Map<String, dynamic>)
      throw const FormatException('视频恢复记录无法核验');
    return VideoMaterialPending(
      p['ownerId'] as String,
      p['input'] as Map<String, dynamic>,
    );
  }
}

abstract class VideoMaterialPendingStore {
  Future<List<VideoMaterialPending>> read(String namespace, String owner);
  Future<void> write(String namespace, VideoMaterialPending operation);
  Future<void> remove(String namespace, String owner, String operationID);
}

String _prefix(String namespace, String owner) {
  if (namespace.isEmpty || !videoMaterialUUID.hasMatch(owner))
    throw const FormatException('视频恢复来源无法核验');
  return 'birdtie_video_v1_${sha256.convert(utf8.encode(namespace))}_${owner}_';
}

List<VideoMaterialPending> _decode(
  Map<String, String> all,
  String prefix,
  String owner,
) {
  final rows = <VideoMaterialPending>[];
  for (final e in all.entries.where((e) => e.key.startsWith(prefix))) {
    if (rows.length >= 12 || utf8.encode(e.value).length > 4096)
      throw const FormatException('视频恢复记录过多');
    final v = VideoMaterialPending.fromJson(
      jsonDecode(e.value) as Map<String, dynamic>,
    );
    if (v.ownerID != owner || e.key != '$prefix${v.operationID}')
      throw const FormatException('视频恢复来源无法核验');
    rows.add(v);
  }
  return List.unmodifiable(rows);
}

Future<void> _videoStorageTail = Future.value();
Future<T> _serialized<T>(Future<T> Function() action) {
  final result = _videoStorageTail.then((_) => action());
  _videoStorageTail = result.then<void>(
    (_) {},
    onError: (Object _, StackTrace __) {},
  );
  return result;
}

class SecureVideoMaterialPendingStore implements VideoMaterialPendingStore {
  const SecureVideoMaterialPendingStore({
    this.storage = const FlutterSecureStorage(),
  });
  final FlutterSecureStorage storage;
  @override
  Future<List<VideoMaterialPending>> read(String namespace, String owner) =>
      _serialized(
        () async =>
            _decode(await storage.readAll(), _prefix(namespace, owner), owner),
      );
  @override
  Future<void> write(String namespace, VideoMaterialPending operation) =>
      _serialized(() async {
        final prefix = _prefix(namespace, operation.ownerID),
            key = '$prefix${operation.operationID}',
            raw = jsonEncode(operation.toJson());
        if (utf8.encode(raw).length > 4096)
          throw const FormatException('视频恢复记录过大');
        final all = await storage.readAll();
        final existing = _decode(all, prefix, operation.ownerID);
        if (existing.length >= 12 && !all.containsKey(key))
          throw const FormatException('请先核实已有视频保存结果');
        if (all[key] != null && all[key] != raw)
          throw const FormatException('视频保存版本已变化');
        await storage.write(key: key, value: raw);
      });
  @override
  Future<void> remove(String namespace, String owner, String operationID) =>
      _serialized(() async {
        if (!videoMaterialUUID.hasMatch(operationID))
          throw const FormatException('视频恢复记录无效');
        await storage.delete(key: '${_prefix(namespace, owner)}$operationID');
      });
}

class MemoryVideoMaterialPendingStore implements VideoMaterialPendingStore {
  final Map<String, String> records = {};
  @override
  Future<List<VideoMaterialPending>> read(
    String namespace,
    String owner,
  ) async => _decode(records, _prefix(namespace, owner), owner);
  @override
  Future<void> write(String namespace, VideoMaterialPending operation) async {
    final key =
            '${_prefix(namespace, operation.ownerID)}${operation.operationID}',
        raw = jsonEncode(operation.toJson());
    if (records[key] != null && records[key] != raw)
      throw const FormatException('视频保存版本已变化');
    if ((await read(namespace, operation.ownerID)).length >= 12 &&
        !records.containsKey(key))
      throw const FormatException('请先核实已有视频保存结果');
    records[key] = raw;
  }

  @override
  Future<void> remove(
    String namespace,
    String owner,
    String operationID,
  ) async {
    records.remove('${_prefix(namespace, owner)}$operationID');
  }
}
