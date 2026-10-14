import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'agent_run_api.dart';
import 'agent_memory_candidate_api.dart' show candidateIDValid;

String agentRunFingerprint(String s) =>
    sha256.convert(utf8.encode(s)).toString();

class PendingAgentRunCancel {
  PendingAgentRunCancel(
    this.environment,
    this.owner,
    this.agentID,
    this.runID,
    this.version,
    this.nonce,
  ) {
    if (!RegExp(r'^[0-9a-f]{64}$').hasMatch(environment) ||
        [owner, agentID, runID, nonce].any((v) => !candidateIDValid(v)) ||
        !agentRunVersion(version))
      throw const FormatException('核实记录不符');
  }
  final String environment, owner, agentID, runID, nonce;
  final int version;
  String get encoded => jsonEncode({
    'environment': environment,
    'owner': owner,
    'agentId': agentID,
    'runId': runID,
    'expectedVersion': version,
    'nonce': nonce,
  });
  factory PendingAgentRunCancel.decode(String raw) {
    final m = Map<String, dynamic>.from(jsonDecode(raw) as Map);
    if (m.length != 6 ||
        !m.keys.toSet().containsAll({
          'environment',
          'owner',
          'agentId',
          'runId',
          'expectedVersion',
          'nonce',
        }))
      throw const FormatException('核实记录不符');
    return PendingAgentRunCancel(
      m['environment'],
      m['owner'],
      m['agentId'],
      m['runId'],
      m['expectedVersion'],
      m['nonce'],
    );
  }
}

abstract interface class AgentRunPendingStore {
  Future<PendingAgentRunCancel?> read(String environment, String owner);
  Future<void> reserve(
    String environment,
    String owner,
    PendingAgentRunCancel value,
  );
  Future<void> delete(
    String environment,
    String owner,
    PendingAgentRunCancel captured,
  );
}

class SecureAgentRunPendingStore implements AgentRunPendingStore {
  const SecureAgentRunPendingStore({
    this.storage = const FlutterSecureStorage(),
  });
  final FlutterSecureStorage storage;
  static Future<void> _tail = Future.value();
  Future<T> _serial<T>(Future<T> Function() f) {
    final p = _tail.then((_) => f());
    _tail = p.then<void>((_) {}, onError: (Object e, StackTrace s) {});
    return p;
  }

  String _key(String e, String o) =>
      'birdtie.run-cancel.v1.${agentRunFingerprint('$e\n$o')}';
  void _bound(String e, String o, PendingAgentRunCancel p) {
    if (p.environment != agentRunFingerprint(e) || p.owner != o)
      throw const FormatException('核实归属不符');
  }

  @override
  Future<PendingAgentRunCancel?> read(String e, String o) => _serial(() async {
    final raw = await storage.read(key: _key(e, o));
    if (raw == null) return null;
    final p = PendingAgentRunCancel.decode(raw);
    _bound(e, o, p);
    return p;
  });
  @override
  Future<void> reserve(String e, String o, PendingAgentRunCancel p) =>
      _serial(() async {
        _bound(e, o, p);
        final k = _key(e, o), old = await storage.read(key: k);
        if (old != null && old != p.encoded) throw StateError('请先核实原停止操作');
        await storage.write(key: k, value: p.encoded);
        if (await storage.read(key: k) != p.encoded)
          throw StateError('无法安全保留核实记录');
      });
  @override
  Future<void> delete(String e, String o, PendingAgentRunCancel p) =>
      _serial(() async {
        _bound(e, o, p);
        final k = _key(e, o), old = await storage.read(key: k);
        if (old == null) return;
        if (old != p.encoded) throw StateError('核实记录已变化');
        await storage.delete(key: k);
        if (await storage.read(key: k) != null) throw StateError('核实记录未清除');
      });
}
