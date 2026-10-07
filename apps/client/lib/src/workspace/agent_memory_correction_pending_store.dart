import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'agent_memory_candidate_api.dart' show candidateIDValid;
import '../content/agent_profile_completion_api.dart'
    show completionMap, completionDigest, completionVersion;
import 'agent_memory_correction_api.dart';

String completionFingerprint(String v) =>
    sha256.convert(utf8.encode(v)).toString();

class PendingMemoryCorrection {
  PendingMemoryCorrection(Map<String, dynamic> value)
    : data = Map.unmodifiable(value) {
    completionMap(data, {
      'environment',
      'ownerId',
      'agentId',
      'sessionFingerprint',
      'operationId',
      'targetId',
      'expectedVersion',
      'targetKind',
      'action',
      'phase',
      'planDigest',
      'expiresAt',
    });
    if (!completionDigest(data['environment']) ||
        !completionDigest(data['sessionFingerprint']) ||
        [
          'ownerId',
          'agentId',
          'operationId',
          'targetId',
        ].any((k) => !candidateIDValid(data[k])) ||
        !completionVersion(data['expectedVersion']) ||
        !{'MEMORY', 'CANDIDATE'}.contains(data['targetKind']) ||
        !correctionActions.containsKey(data['action']) ||
        !{'PREVIEW', 'CONFIRM'}.contains(data['phase']) ||
        (data['phase'] == 'PREVIEW'
            ? data['planDigest'] != null
            : !completionDigest(data['planDigest']))) {
      throw const FormatException('本机核实记录不符');
    }
    correctionTime(data['expiresAt']);
  }
  final Map<String, dynamic> data;
  String get id => data['operationId'];
  String get phase => data['phase'];
  String get encoded => jsonEncode(data);
  bool same(PendingMemoryCorrection other) => encoded == other.encoded;
  bool mayReplace(PendingMemoryCorrection next) =>
      same(next) ||
      (phase == 'PREVIEW' &&
          next.phase == 'CONFIRM' &&
          data.keys
              .where((k) => !{'phase', 'planDigest', 'expiresAt'}.contains(k))
              .every((k) => data[k] == next.data[k]) &&
          !correctionTime(
            next.data['expiresAt'],
          ).isAfter(correctionTime(data['expiresAt'])));
  bool matches(CorrectionReceipt r) =>
      r.id == id &&
      r.agentID == data['agentId'] &&
      r.raw['target']['id'] == data['targetId'] &&
      r.raw['target']['kind'] == data['targetKind'] &&
      r.raw['target']['version'] == data['expectedVersion'] &&
      r.raw['action'] == data['action'] &&
      (phase == 'PREVIEW' || r.digest == data['planDigest']) &&
      (phase == 'CONFIRM'
          ? r.expiresAt.isAtSameMomentAs(correctionTime(data['expiresAt']))
          : !r.expiresAt.isAfter(correctionTime(data['expiresAt'])));
}

abstract interface class AgentMemoryCorrectionPendingStore {
  Future<PendingMemoryCorrection?> read(String environment, String owner);
  Future<void> write(
    String environment,
    String owner,
    PendingMemoryCorrection value,
  );
  Future<void> delete(
    String environment,
    String owner,
    PendingMemoryCorrection captured,
  );
}

String _key(String environment, String owner) {
  if (environment.isEmpty || !candidateIDValid(owner)) {
    throw const FormatException('本机核实归属不符');
  }
  return 'birdtie.memory.correction.v1.${completionFingerprint('$environment\n$owner')}';
}

void _bound(String environment, String owner, PendingMemoryCorrection p) {
  if (p.data['environment'] != completionFingerprint(environment) ||
      p.data['ownerId'] != owner) {
    throw const FormatException('本机核实归属不符');
  }
}

class SecureAgentMemoryCorrectionPendingStore
    implements AgentMemoryCorrectionPendingStore {
  const SecureAgentMemoryCorrectionPendingStore({
    this.storage = const FlutterSecureStorage(),
  });
  final FlutterSecureStorage storage;
  static Future<void> _tail = Future.value();
  Future<T> _serial<T>(Future<T> Function() action) {
    final r = _tail.then((_) => action());
    _tail = r.then<void>((_) {}, onError: (Object e, StackTrace s) {});
    return r;
  }

  @override
  Future<PendingMemoryCorrection?> read(String environment, String owner) =>
      _serial(() async {
        final raw = await storage.read(key: _key(environment, owner));
        if (raw == null) return null;
        final p = PendingMemoryCorrection(
          Map<String, dynamic>.from(jsonDecode(raw)),
        );
        _bound(environment, owner, p);
        return p;
      });
  @override
  Future<void> write(
    String environment,
    String owner,
    PendingMemoryCorrection value,
  ) => _serial(() async {
    _bound(environment, owner, value);
    final k = _key(environment, owner), raw = await storage.read(key: k);
    if (raw != null) {
      final old = PendingMemoryCorrection(
        Map<String, dynamic>.from(jsonDecode(raw)),
      );
      _bound(environment, owner, old);
      if (!old.mayReplace(value)) throw StateError('请先核实原操作');
    }
    await storage.write(key: k, value: value.encoded);
    if (await storage.read(key: k) != value.encoded) {
      throw StateError('本机核实记录未安全保存');
    }
  });
  @override
  Future<void> delete(
    String environment,
    String owner,
    PendingMemoryCorrection captured,
  ) => _serial(() async {
    _bound(environment, owner, captured);
    final k = _key(environment, owner), raw = await storage.read(key: k);
    if (raw == null) return;
    if (raw != captured.encoded) throw StateError('原核实记录已改变');
    await storage.delete(key: k);
    if (await storage.read(key: k) != null) throw StateError('本机核实记录未删除');
  });
}

class MemoryAgentMemoryCorrectionPendingStore
    implements AgentMemoryCorrectionPendingStore {
  final Map<String, String> values = {};
  @override
  Future<PendingMemoryCorrection?> read(String e, String o) async {
    final raw = values[_key(e, o)];
    if (raw == null) return null;
    final p = PendingMemoryCorrection(
      Map<String, dynamic>.from(jsonDecode(raw)),
    );
    _bound(e, o, p);
    return p;
  }

  @override
  Future<void> write(String e, String o, PendingMemoryCorrection p) async {
    _bound(e, o, p);
    final k = _key(e, o), raw = values[k];
    if (raw != null) {
      final old = PendingMemoryCorrection(
        Map<String, dynamic>.from(jsonDecode(raw)),
      );
      _bound(e, o, old);
      if (!old.mayReplace(p)) throw StateError('请先核实原操作');
    }
    values[k] = p.encoded;
  }

  @override
  Future<void> delete(String e, String o, PendingMemoryCorrection p) async {
    _bound(e, o, p);
    final k = _key(e, o);
    if (values[k] != null && values[k] != p.encoded) {
      throw StateError('原核实记录已改变');
    }
    values.remove(k);
  }
}
