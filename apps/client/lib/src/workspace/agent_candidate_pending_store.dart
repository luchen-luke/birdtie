import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'agent_memory_candidate_api.dart';

String candidateRecoveryFingerprint(String value) =>
    sha256.convert(utf8.encode(value)).toString();

DateTime _recoveryStamp(dynamic raw) {
  if (raw is! String) {
    throw const FormatException('本机期限不符');
  }
  final m = RegExp(
    r'^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?(Z|([+-])(\d{2}):(\d{2}))$',
  ).firstMatch(raw);
  if (m == null || m.end != raw.length) {
    throw const FormatException('本机期限不符');
  }
  final v = [for (var i = 1; i <= 6; i++) int.parse(m.group(i)!)];
  final t = DateTime.utc(v[0], v[1], v[2], v[3], v[4], v[5]);
  if (v[0] < 1 ||
      v[0] > 9999 ||
      t.year != v[0] ||
      t.month != v[1] ||
      t.day != v[2] ||
      t.hour != v[3] ||
      t.minute != v[4] ||
      t.second != v[5] ||
      (m.group(8) != 'Z' &&
          (int.parse(m.group(10)!) > 23 || int.parse(m.group(11)!) > 59))) {
    throw const FormatException('本机期限不符');
  }
  return candidateTime(raw);
}

/// References for human reconciliation only. This cannot reconstruct approval.
class PendingHumanAcceptance {
  PendingHumanAcceptance(Map<String, dynamic> value)
    : data = Map.unmodifiable(value) {
    const keys = {
      'environment',
      'ownerId',
      'agentId',
      'sessionFingerprint',
      'previewId',
      'candidateId',
      'candidateVersion',
      'targetMemoryId',
      'expectedMemoryVersion',
      'planDigest',
      'expiresAt',
    };
    final hex = RegExp(r'^[0-9a-f]{64}$');
    if (data.length != keys.length ||
        data.keys.toSet().difference(keys).isNotEmpty ||
        [
          'environment',
          'sessionFingerprint',
          'previewId',
          'planDigest',
        ].any((k) => data[k] is! String || !hex.hasMatch(data[k])) ||
        [
          'ownerId',
          'agentId',
          'candidateId',
          'targetMemoryId',
        ].any((k) => !candidateIDValid(data[k])) ||
        data['candidateVersion'] is! int ||
        data['candidateVersion'] < 1 ||
        data['candidateVersion'] >= 9007199254740991 ||
        data['expectedMemoryVersion'] is! int ||
        data['expectedMemoryVersion'] < 0 ||
        data['expectedMemoryVersion'] >= 9007199254740991) {
      throw const FormatException('本机核实记录不符');
    }
    _recoveryStamp(data['expiresAt']);
  }
  final Map<String, dynamic> data;
  String get previewID => data['previewId'];
  String get candidateID => data['candidateId'];
  DateTime get expiresAt => _recoveryStamp(data['expiresAt']);
  String get encoded => jsonEncode(data);
  bool same(PendingHumanAcceptance other) => encoded == other.encoded;
  bool matches(HumanMemoryCandidate r) =>
      r.id == candidateID &&
      r.raw['owner']['id'] == data['ownerId'] &&
      r.raw['agentId'] == data['agentId'] &&
      r.status == 'ACTIVE' &&
      r.version == data['candidateVersion'] + 1 &&
      r.memoryID == data['targetMemoryId'] &&
      r.raw['memoryVersion'] == data['expectedMemoryVersion'] + 1;
}

abstract interface class AgentCandidatePendingStore {
  Future<List<PendingHumanAcceptance>> read(String environment, String owner);
  Future<void> write(
    String environment,
    String owner,
    PendingHumanAcceptance value,
  );
  Future<void> delete(
    String environment,
    String owner,
    PendingHumanAcceptance captured,
  );
}

String _prefix(String environment, String owner) {
  if (environment.isEmpty || !candidateIDValid(owner)) {
    throw const FormatException('本机核实归属不符');
  }
  return 'birdtie.candidate.accept.v1.${candidateRecoveryFingerprint('$environment\n$owner')}.';
}

void _bound(String environment, String owner, PendingHumanAcceptance value) {
  if (value.data['environment'] != candidateRecoveryFingerprint(environment) ||
      value.data['ownerId'] != owner) {
    throw const FormatException('本机核实归属不符');
  }
}

class SecureAgentCandidatePendingStore implements AgentCandidatePendingStore {
  const SecureAgentCandidatePendingStore({
    this.storage = const FlutterSecureStorage(),
  });
  final FlutterSecureStorage storage;
  static Future<void> _tail = Future.value();
  Future<T> _serial<T>(Future<T> Function() action) {
    final result = _tail.then((_) => action());
    _tail = result.then<void>(
      (_) {},
      onError: (Object error, StackTrace stack) {},
    );
    return result;
  }

  @override
  Future<List<PendingHumanAcceptance>> read(String environment, String owner) =>
      _serial(() async {
        final prefix = _prefix(environment, owner),
            all = await storage.readAll();
        final result = <PendingHumanAcceptance>[];
        for (final e in all.entries.where((e) => e.key.startsWith(prefix))) {
          final value = PendingHumanAcceptance(
            jsonDecode(e.value) as Map<String, dynamic>,
          );
          _bound(environment, owner, value);
          if (e.key != '$prefix${value.previewID}') {
            throw const FormatException('本机核实操作不符');
          }
          result.add(value);
        }
        if (result.length > 8) {
          throw const FormatException('本机核实记录超过上限');
        }
        return result;
      });
  @override
  Future<void> write(
    String environment,
    String owner,
    PendingHumanAcceptance value,
  ) => _serial(() async {
    _bound(environment, owner, value);
    final prefix = _prefix(environment, owner);
    final all = await storage.readAll();
    for (final e in all.entries.where((e) => e.key.startsWith(prefix))) {
      final existing = PendingHumanAcceptance(
        jsonDecode(e.value) as Map<String, dynamic>,
      );
      _bound(environment, owner, existing);
      if (e.key != '$prefix${existing.previewID}' || !existing.same(value)) {
        throw StateError('已有未核实操作，请先读取原候选');
      }
    }
    final key = '$prefix${value.previewID}';
    final old = await storage.read(key: key);
    if (old != null && old != value.encoded) {
      throw StateError('原核实操作不可替换');
    }
    await storage.write(key: key, value: value.encoded);
    if (await storage.read(key: key) != value.encoded) {
      throw StateError('本机核实记录未安全保存');
    }
  });
  @override
  Future<void> delete(
    String environment,
    String owner,
    PendingHumanAcceptance captured,
  ) => _serial(() async {
    _bound(environment, owner, captured);
    final key = '${_prefix(environment, owner)}${captured.previewID}';
    final old = await storage.read(key: key);
    if (old == null) {
      return;
    }
    if (old != captured.encoded) {
      throw StateError('原核实操作已改变');
    }
    await storage.delete(key: key);
  });
}

class MemoryAgentCandidatePendingStore implements AgentCandidatePendingStore {
  final Map<String, String> values = {};
  @override
  Future<List<PendingHumanAcceptance>> read(
    String environment,
    String owner,
  ) async {
    final prefix = _prefix(environment, owner);
    final result = <PendingHumanAcceptance>[];
    for (final e in values.entries.where((e) => e.key.startsWith(prefix))) {
      final value = PendingHumanAcceptance(
        jsonDecode(e.value) as Map<String, dynamic>,
      );
      _bound(environment, owner, value);
      if (e.key != '$prefix${value.previewID}') {
        throw StateError('本机核实操作不符');
      }
      result.add(value);
    }
    if (result.length > 8) {
      throw StateError('本机核实记录超过上限');
    }
    return result;
  }

  @override
  Future<void> write(
    String environment,
    String owner,
    PendingHumanAcceptance value,
  ) async {
    _bound(environment, owner, value);
    final prefix = _prefix(environment, owner);
    for (final e in values.entries.where((e) => e.key.startsWith(prefix))) {
      final existing = PendingHumanAcceptance(
        jsonDecode(e.value) as Map<String, dynamic>,
      );
      _bound(environment, owner, existing);
      if (e.key != '$prefix${existing.previewID}' || !existing.same(value)) {
        throw StateError('已有未核实操作，请先读取原候选');
      }
    }
    final key = '$prefix${value.previewID}';
    if (values[key] != null && values[key] != value.encoded) {
      throw StateError('原核实操作不可替换');
    }
    values[key] = value.encoded;
  }

  @override
  Future<void> delete(
    String environment,
    String owner,
    PendingHumanAcceptance captured,
  ) async {
    _bound(environment, owner, captured);
    final key = '${_prefix(environment, owner)}${captured.previewID}';
    if (values[key] != null && values[key] != captured.encoded) {
      throw StateError('原核实操作已改变');
    }
    values.remove(key);
  }
}
