import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

final visualUUID = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
final visualDigest = RegExp(r'^[0-9a-f]{64}$');
final _sourceSelector = RegExp(r'^[A-Za-z0-9][A-Za-z0-9._:-]{0,125}$');

/// Recovery selectors only; never pixels, text, credentials or an approval.
abstract interface class AgentVisualPendingStore {
  Future<String?> read(String origin, String owner, String source);
  Future<void> write(
    String origin,
    String owner,
    String source,
    String operation,
  );
  Future<void> delete(
    String origin,
    String owner,
    String source,
    String operation,
  );
}

class SecureAgentVisualPendingStore implements AgentVisualPendingStore {
  SecureAgentVisualPendingStore({FlutterSecureStorage? storage})
    : _storage = storage ?? const FlutterSecureStorage();
  final FlutterSecureStorage _storage;
  static String key(String origin, String owner, String source) {
    final uri = Uri.tryParse(origin);
    if (uri == null ||
        !uri.hasAuthority ||
        uri.userInfo.isNotEmpty ||
        uri.hasQuery ||
        uri.hasFragment ||
        !['http', 'https'].contains(uri.scheme) ||
        !visualUUID.hasMatch(owner) ||
        !_sourceSelector.hasMatch(source)) {
      throw const FormatException('图片分析恢复来源无效');
    }
    return 'birdtie.visual.pending.v1.${sha256.convert(utf8.encode('${uri.origin}\n$owner\n$source'))}';
  }

  @override
  Future<String?> read(String origin, String owner, String source) async {
    final value = await _storage.read(key: key(origin, owner, source));
    if (value != null && !visualUUID.hasMatch(value)) {
      throw const FormatException('图片分析恢复记录无法核验');
    }
    return value;
  }

  @override
  Future<void> write(
    String origin,
    String owner,
    String source,
    String operation,
  ) async {
    if (!visualUUID.hasMatch(operation))
      throw const FormatException('图片分析操作无效');
    final prior = await read(origin, owner, source);
    if (prior != null && prior != operation)
      throw const FormatException('请先核实原图片分析操作');
    await _storage.write(key: key(origin, owner, source), value: operation);
  }

  @override
  Future<void> delete(
    String origin,
    String owner,
    String source,
    String operation,
  ) async {
    if (await read(origin, owner, source) == operation) {
      await _storage.delete(key: key(origin, owner, source));
    }
  }
}
