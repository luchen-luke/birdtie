import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

final _id = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
final _hash = RegExp(r'^[0-9a-f]{64}$');

/// Recovery references only. No image, session, text or approval is persisted.
class PendingPrivateImageOperation {
  PendingPrivateImageOperation({
    required this.ownerID,
    required this.momentID,
    required this.operationID,
    required this.phase,
    required this.momentRevision,
    required this.mime,
    required this.byteSize,
    required this.inputHash,
    required this.pixelRisk,
    required this.observedAt,
    required this.deadlineAt,
    this.assetID,
    this.assetRevision,
    this.derivativeHash,
  }) {
    _validate(toJson());
  }
  final String ownerID,
      momentID,
      operationID,
      phase,
      mime,
      inputHash,
      pixelRisk;
  final int momentRevision, byteSize;
  final String? assetID, derivativeHash;
  final int? assetRevision;
  final DateTime observedAt, deadlineAt;
  String get key => '$operationID.$phase';
  Map<String, dynamic> toJson() => {
    'schema': 'private-image-pending-v1',
    'ownerId': ownerID,
    'momentId': momentID,
    'operationId': operationID,
    'phase': phase,
    'momentRevision': momentRevision,
    'mimeType': mime,
    'byteSize': byteSize,
    'inputSha256': inputHash,
    'pixelRisk': pixelRisk,
    'assetId': assetID,
    'assetRevision': assetRevision,
    'derivativeSha256': derivativeHash,
    'observedAt': observedAt.toUtc().toIso8601String(),
    'deadlineAt': deadlineAt.toUtc().toIso8601String(),
  };
  static const _keys = {
    'schema',
    'ownerId',
    'momentId',
    'operationId',
    'phase',
    'momentRevision',
    'mimeType',
    'byteSize',
    'inputSha256',
    'pixelRisk',
    'assetId',
    'assetRevision',
    'derivativeSha256',
    'observedAt',
    'deadlineAt',
  };
  static DateTime _date(dynamic value) {
    final parts = value is String
        ? RegExp(
            r'^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,6})?Z$',
          ).firstMatch(value)
        : null;
    if (parts == null) throw const FormatException('图片恢复期限不符');
    final d = DateTime.parse(value).toUtc();
    final actual = [d.year, d.month, d.day, d.hour, d.minute, d.second];
    if (d.year < 1 ||
        d.year > 9999 ||
        List.generate(
          6,
          (i) => int.parse(parts.group(i + 1)!),
        ).asMap().entries.any((e) => actual[e.key] != e.value)) {
      throw const FormatException('图片恢复期限不符');
    }
    return d;
  }

  static void _validate(Map<String, dynamic> d) {
    if (d.length != _keys.length ||
        d.keys.toSet().difference(_keys).isNotEmpty ||
        d['schema'] != 'private-image-pending-v1' ||
        [
          'ownerId',
          'momentId',
          'operationId',
        ].any((k) => d[k] is! String || !_id.hasMatch(d[k])) ||
        !['preview', 'save', 'delete'].contains(d['phase']) ||
        d['momentRevision'] is! int ||
        d['momentRevision'] < 1 ||
        !['image/png', 'image/jpeg'].contains(d['mimeType']) ||
        d['byteSize'] is! int ||
        d['byteSize'] < 1 ||
        d['byteSize'] > 10 * 1024 * 1024 ||
        d['inputSha256'] is! String ||
        !_hash.hasMatch(d['inputSha256']) ||
        !['UNKNOWN', 'USER_MASKED'].contains(d['pixelRisk']) ||
        (d['phase'] == 'preview' &&
            (d['assetId'] != null ||
                d['assetRevision'] != null ||
                d['derivativeSha256'] != null)) ||
        (d['phase'] != 'preview' &&
            (d['assetId'] is! String ||
                !_id.hasMatch(d['assetId']) ||
                d['assetRevision'] is! int ||
                d['assetRevision'] < 1)) ||
        (d['phase'] == 'save' && d['derivativeSha256'] != null) ||
        (d['phase'] == 'delete' &&
            (d['derivativeSha256'] is! String ||
                !_hash.hasMatch(d['derivativeSha256'])))) {
      throw const FormatException('图片恢复记录不符');
    }
    final at = _date(d['observedAt']), end = _date(d['deadlineAt']);
    // Historical deadlines are references, not permission or a no-effect verdict.
    if (end.isAfter(at.add(const Duration(days: 30)))) {
      throw const FormatException('图片恢复期限不符');
    }
  }

  factory PendingPrivateImageOperation.fromJson(Map<String, dynamic> d) {
    _validate(d);
    return PendingPrivateImageOperation(
      ownerID: d['ownerId'],
      momentID: d['momentId'],
      operationID: d['operationId'],
      phase: d['phase'],
      momentRevision: d['momentRevision'],
      mime: d['mimeType'],
      byteSize: d['byteSize'],
      inputHash: d['inputSha256'],
      pixelRisk: d['pixelRisk'],
      assetID: d['assetId'],
      assetRevision: d['assetRevision'],
      derivativeHash: d['derivativeSha256'],
      observedAt: _date(d['observedAt']),
      deadlineAt: _date(d['deadlineAt']),
    );
  }
}

abstract interface class PrivateImagePendingStore {
  Future<List<PendingPrivateImageOperation>> read(
    String environment,
    String owner,
    String moment,
  );
  Future<void> write(String environment, PendingPrivateImageOperation value);
  Future<void> delete(String environment, PendingPrivateImageOperation value);
}

String _prefix(String environment, String owner, String moment) {
  final u = Uri.tryParse(environment);
  if (u == null ||
      !u.hasAuthority ||
      !['http', 'https'].contains(u.scheme) ||
      u.userInfo.isNotEmpty ||
      u.hasQuery ||
      u.hasFragment ||
      !_id.hasMatch(owner) ||
      !_id.hasMatch(moment)) {
    throw const FormatException('图片恢复归属不符');
  }
  final normalized = u
      .replace(path: u.path.replaceFirst(RegExp(r'/$'), ''))
      .toString();
  return 'birdtie.private-image.pending.v1.${sha256.convert(utf8.encode('$normalized\n$owner\n$moment'))}.';
}

class SecurePrivateImagePendingStore implements PrivateImagePendingStore {
  const SecurePrivateImagePendingStore({
    this.storage = const FlutterSecureStorage(),
  });
  final FlutterSecureStorage storage;
  @override
  Future<List<PendingPrivateImageOperation>> read(
    String environment,
    String owner,
    String moment,
  ) async {
    final prefix = _prefix(environment, owner, moment),
        all = await storage.readAll();
    final result = <PendingPrivateImageOperation>[];
    for (final entry in all.entries.where((e) => e.key.startsWith(prefix))) {
      if (entry.value.length > 4096) throw const FormatException('图片恢复记录过大');
      final v = PendingPrivateImageOperation.fromJson(
        jsonDecode(entry.value) as Map<String, dynamic>,
      );
      if (v.ownerID != owner ||
          v.momentID != moment ||
          entry.key != '$prefix${v.key}') {
        throw const FormatException('图片恢复归属不符');
      }
      result.add(v);
    }
    if (result.length > 16) throw const FormatException('待核实图片操作过多');
    result.sort((a, b) => a.observedAt.compareTo(b.observedAt));
    return List.unmodifiable(result);
  }

  @override
  Future<void> write(
    String environment,
    PendingPrivateImageOperation value,
  ) async {
    PendingPrivateImageOperation.fromJson(value.toJson());
    final key =
            '${_prefix(environment, value.ownerID, value.momentID)}${value.key}',
        raw = jsonEncode(value.toJson());
    final old = await storage.read(key: key);
    if (old != null && old != raw) throw StateError('原图片操作不能更换内容或版本');
    await storage.write(key: key, value: raw);
  }

  @override
  Future<void> delete(
    String environment,
    PendingPrivateImageOperation value,
  ) => storage.delete(
    key: '${_prefix(environment, value.ownerID, value.momentID)}${value.key}',
  );
}

class MemoryPrivateImagePendingStore implements PrivateImagePendingStore {
  final Map<String, PendingPrivateImageOperation> values = {};
  @override
  Future<List<PendingPrivateImageOperation>> read(
    String environment,
    String owner,
    String moment,
  ) async {
    final p = _prefix(environment, owner, moment);
    return values.entries
        .where((e) => e.key.startsWith(p))
        .map((e) => e.value)
        .toList();
  }

  @override
  Future<void> write(
    String environment,
    PendingPrivateImageOperation value,
  ) async {
    final key =
            '${_prefix(environment, value.ownerID, value.momentID)}${value.key}',
        old = values[key];
    if (old != null && jsonEncode(old.toJson()) != jsonEncode(value.toJson())) {
      throw StateError('原图片操作不能更换内容或版本');
    }
    values[key] = value;
  }

  @override
  Future<void> delete(
    String environment,
    PendingPrivateImageOperation value,
  ) async {
    values.remove(
      '${_prefix(environment, value.ownerID, value.momentID)}${value.key}',
    );
  }
}
