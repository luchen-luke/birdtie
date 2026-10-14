import 'dart:convert';
import 'dart:math';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

final pushUUID = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
String newPushID() {
  final r = Random.secure(), b = List<int>.generate(16, (_) => r.nextInt(256));
  b[6] = (b[6] & 15) | 64;
  b[8] = (b[8] & 63) | 128;
  final s = b.map((v) => v.toRadixString(16).padLeft(2, '0')).join();
  return '${s.substring(0, 8)}-${s.substring(8, 12)}-${s.substring(12, 16)}-${s.substring(16, 20)}-${s.substring(20)}';
}

class PushInstallation {
  PushInstallation(this.id, this.proof) {
    if (!pushUUID.hasMatch(id) ||
        !RegExp(r'^[A-Za-z0-9_-]{43}$').hasMatch(proof))
      throw const FormatException('设备通知凭证无法核验');
  }
  final String id, proof;
  @override
  String toString() => 'PushInstallation{redacted}';
}

class PushPending {
  PushPending({
    required this.owner,
    required this.id,
    required this.operation,
    required this.expectedVersion,
  }) {
    if (!pushUUID.hasMatch(owner) ||
        !pushUUID.hasMatch(id) ||
        !['REGISTER', 'REVOKE'].contains(operation) ||
        expectedVersion < 0 ||
        expectedVersion >= 9007199254740991)
      throw const FormatException('设备通知恢复记录无效');
  }
  final String owner, id, operation;
  final int expectedVersion;
  Map<String, dynamic> toJson() => {
    'owner': owner,
    'operationId': id,
    'operation': operation,
    'expectedVersion': expectedVersion,
  };
  factory PushPending.fromJson(Map<String, dynamic> j) {
    if (j.keys.toSet().difference({
          'owner',
          'operationId',
          'operation',
          'expectedVersion',
        }).isNotEmpty ||
        j.length != 4 ||
        j['owner'] is! String ||
        j['operationId'] is! String ||
        j['operation'] is! String ||
        j['expectedVersion'] is! int)
      throw const FormatException('设备通知恢复记录无效');
    return PushPending(
      owner: j['owner'],
      id: j['operationId'],
      operation: j['operation'],
      expectedVersion: j['expectedVersion'],
    );
  }
}

abstract interface class SystemPushPendingStore {
  Future<PushInstallation> installation();
  Future<PushPending?> read(String origin, String owner);
  Future<void> write(String origin, PushPending value);
  Future<void> delete(String origin, PushPending value);
}

Future<void> _pushStorageTail = Future.value();
Future<T> _pushSerialized<T>(Future<T> Function() action) {
  final result = _pushStorageTail.then((_) => action());
  _pushStorageTail = result.then<void>(
    (_) {},
    onError: (Object _, StackTrace __) {},
  );
  return result;
}

class SecureSystemPushPendingStore implements SystemPushPendingStore {
  SecureSystemPushPendingStore({FlutterSecureStorage? storage})
    : _storage = storage ?? const FlutterSecureStorage();
  final FlutterSecureStorage _storage;
  static const _installationKey = 'birdtie.push.installation.v1';
  static String pendingKey(String origin, String owner) {
    final u = Uri.tryParse(origin);
    if (u == null ||
        !u.hasAuthority ||
        !['http', 'https'].contains(u.scheme) ||
        u.userInfo.isNotEmpty ||
        u.hasQuery ||
        u.hasFragment ||
        !pushUUID.hasMatch(owner))
      throw const FormatException('设备通知来源无效');
    return 'birdtie.push.pending.v1.${sha256.convert(utf8.encode('${u.origin}\n$owner'))}';
  }

  @override
  Future<PushInstallation> installation() => _pushSerialized(() async {
    final saved = await _storage.read(key: _installationKey);
    if (saved != null) {
      if (saved.length > 256) throw const FormatException('设备通知凭证无法核验');
      final j = jsonDecode(saved);
      if (j is! Map<String, dynamic> ||
          j.length != 2 ||
          !j.keys.every({'id', 'proof'}.contains) ||
          j['id'] is! String ||
          j['proof'] is! String)
        throw const FormatException('设备通知凭证无法核验');
      return PushInstallation(j['id'], j['proof']);
    }
    final r = Random.secure();
    final value = PushInstallation(
      newPushID(),
      base64UrlEncode(
        List<int>.generate(32, (_) => r.nextInt(256)),
      ).replaceAll('=', ''),
    );
    await _storage.write(
      key: _installationKey,
      value: jsonEncode({'id': value.id, 'proof': value.proof}),
    );
    return value;
  });
  Future<PushPending?> _read(String origin, String owner) async {
    final saved = await _storage.read(key: pendingKey(origin, owner));
    if (saved == null) return null;
    if (saved.length > 512) throw const FormatException('设备通知恢复记录超限');
    final value = PushPending.fromJson(jsonDecode(saved));
    if (value.owner != owner) throw const FormatException('设备通知恢复账号不符');
    return value;
  }

  @override
  Future<PushPending?> read(String origin, String owner) =>
      _pushSerialized(() => _read(origin, owner));
  @override
  Future<void> write(String origin, PushPending value) =>
      _pushSerialized(() async {
        final prior = await _read(origin, value.owner);
        if (prior != null &&
            (prior.id != value.id ||
                prior.operation != value.operation ||
                prior.expectedVersion != value.expectedVersion))
          throw const FormatException('请先核实原设备通知操作');
        await _storage.write(
          key: pendingKey(origin, value.owner),
          value: jsonEncode(value.toJson()),
        );
      });
  @override
  Future<void> delete(String origin, PushPending value) =>
      _pushSerialized(() async {
        final prior = await _read(origin, value.owner);
        if (prior?.id == value.id &&
            prior?.operation == value.operation &&
            prior?.expectedVersion == value.expectedVersion)
          await _storage.delete(key: pendingKey(origin, value.owner));
      });
}
