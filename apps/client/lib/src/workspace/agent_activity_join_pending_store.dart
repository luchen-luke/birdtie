import 'dart:convert';

import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

import 'agent_activity_join_api.dart';

String activityJoinFingerprint(String value) =>
    sha256.convert(utf8.encode(value)).toString();

/// Recovery selectors only. This contains no activity text, approval DTO,
/// Session credential, source lease, grant or native execution capability.
class PendingAgentActivityJoin {
  PendingAgentActivityJoin({
    required this.environmentDigest,
    required this.ownerID,
    required this.agentID,
    required this.taskID,
    required this.activityID,
    required this.approvalID,
    required this.logicalOperationID,
    required this.actionID,
    required this.bindingDigest,
    required this.expectedTaskUpdatedAt,
    required this.replyTurnDigest,
    required this.phase,
  }) {
    if (!activityJoinDigest(environmentDigest) ||
        !activityJoinDigest(bindingDigest) ||
        !activityJoinDigest(replyTurnDigest) ||
        [
          ownerID,
          agentID,
          taskID,
          activityID,
          approvalID,
          logicalOperationID,
          actionID,
        ].any((id) => !activityJoinID(id)) ||
        !const {'CONFIRM', 'EXECUTE', 'STOP'}.contains(phase)) {
      throw const FormatException('原智能体报名核实记录无效。');
    }
    activityJoinTime(expectedTaskUpdatedAt);
  }
  final String environmentDigest,
      ownerID,
      agentID,
      taskID,
      activityID,
      approvalID,
      logicalOperationID,
      actionID,
      bindingDigest,
      expectedTaskUpdatedAt,
      replyTurnDigest,
      phase;
  Map<String, dynamic> toJson() => {
    'schema': 'agent-activity-join-pending-v1',
    'environmentDigest': environmentDigest,
    'ownerId': ownerID,
    'agentId': agentID,
    'taskId': taskID,
    'activityId': activityID,
    'approvalId': approvalID,
    'logicalOperationId': logicalOperationID,
    'actionId': actionID,
    'bindingDigest': bindingDigest,
    'expectedTaskUpdatedAt': expectedTaskUpdatedAt,
    'replyTurnDigest': replyTurnDigest,
    'phase': phase,
  };
  String get encoded => jsonEncode(toJson());
  PendingAgentActivityJoin withPhase(String next) => PendingAgentActivityJoin(
    environmentDigest: environmentDigest,
    ownerID: ownerID,
    agentID: agentID,
    taskID: taskID,
    activityID: activityID,
    approvalID: approvalID,
    logicalOperationID: logicalOperationID,
    actionID: actionID,
    bindingDigest: bindingDigest,
    expectedTaskUpdatedAt: expectedTaskUpdatedAt,
    replyTurnDigest: replyTurnDigest,
    phase: next,
  );
  bool sameBinding(PendingAgentActivityJoin other) =>
      environmentDigest == other.environmentDigest &&
      ownerID == other.ownerID &&
      agentID == other.agentID &&
      taskID == other.taskID &&
      activityID == other.activityID &&
      approvalID == other.approvalID &&
      logicalOperationID == other.logicalOperationID &&
      actionID == other.actionID &&
      bindingDigest == other.bindingDigest &&
      expectedTaskUpdatedAt == other.expectedTaskUpdatedAt &&
      replyTurnDigest == other.replyTurnDigest;
  bool same(PendingAgentActivityJoin other) =>
      sameBinding(other) && phase == other.phase;
  bool allowsNext(PendingAgentActivityJoin next) =>
      sameBinding(next) &&
      (phase == next.phase ||
          phase == 'CONFIRM' && next.phase == 'EXECUTE' ||
          phase != 'STOP' && next.phase == 'STOP');
  bool matches(AgentActivityJoinPreview p) =>
      p.raw['owner']['id'] == ownerID &&
      p.agentID == agentID &&
      p.taskID == taskID &&
      p.activityID == activityID &&
      p.approvalID == approvalID &&
      p.logicalOperationID == logicalOperationID &&
      p.actionID == actionID &&
      p.bindingDigest == bindingDigest;
  factory PendingAgentActivityJoin.decode(String raw) {
    final m = activityJoinObject(activityJoinJSON(raw, maxBytes: 2048), {
      'schema',
      'environmentDigest',
      'ownerId',
      'agentId',
      'taskId',
      'activityId',
      'approvalId',
      'logicalOperationId',
      'actionId',
      'bindingDigest',
      'expectedTaskUpdatedAt',
      'replyTurnDigest',
      'phase',
    });
    if (m['schema'] != 'agent-activity-join-pending-v1' ||
        m.values.any((v) => v is! String)) {
      throw const FormatException('原智能体报名核实记录无效。');
    }
    return PendingAgentActivityJoin(
      environmentDigest: m['environmentDigest'],
      ownerID: m['ownerId'],
      agentID: m['agentId'],
      taskID: m['taskId'],
      activityID: m['activityId'],
      approvalID: m['approvalId'],
      logicalOperationID: m['logicalOperationId'],
      actionID: m['actionId'],
      bindingDigest: m['bindingDigest'],
      expectedTaskUpdatedAt: m['expectedTaskUpdatedAt'],
      replyTurnDigest: m['replyTurnDigest'],
      phase: m['phase'],
    );
  }
}

abstract interface class AgentActivityJoinPendingStore {
  Future<PendingAgentActivityJoin?> read(String environment, String owner);

  /// false means a retained operation already owns the slot. Even an identical
  /// recovered selector is not a fresh reservation or permission to execute.
  Future<bool> reserve(
    String environment,
    String owner,
    PendingAgentActivityJoin value,
  );
  Future<void> replace(
    String environment,
    String owner,
    PendingAgentActivityJoin expected,
    PendingAgentActivityJoin next,
  );
  Future<void> delete(
    String environment,
    String owner,
    PendingAgentActivityJoin expected,
  );
}

String _key(String environment, String owner) {
  if (environment.isEmpty || !activityJoinID(owner)) {
    throw const FormatException('原智能体报名记录归属无效。');
  }
  return 'birdtie.agent-activity-join.pending.v1.${activityJoinFingerprint('$environment\n$owner')}';
}

void _bound(String environment, String owner, PendingAgentActivityJoin p) {
  _key(environment, owner);
  if (p.environmentDigest != activityJoinFingerprint(environment) ||
      p.ownerID != owner) {
    throw const FormatException('原智能体报名记录归属不符。');
  }
}

class SecureAgentActivityJoinPendingStore
    implements AgentActivityJoinPendingStore {
  const SecureAgentActivityJoinPendingStore({
    this.storage = const FlutterSecureStorage(),
  });
  final FlutterSecureStorage storage;
  static Future<void> _tail = Future.value();
  Future<T> _serial<T>(Future<T> Function() action) {
    final next = _tail.then((_) => action());
    _tail = next.then<void>((_) {}, onError: (Object _, StackTrace _) {});
    return next;
  }

  Future<PendingAgentActivityJoin?> _read(String e, String o) async {
    final raw = await storage.read(key: _key(e, o));
    if (raw == null) return null;
    final p = PendingAgentActivityJoin.decode(raw);
    _bound(e, o, p);
    return p;
  }

  Future<void> _readBack(String e, String o, PendingAgentActivityJoin p) async {
    final actual = await _read(e, o);
    if (actual == null || !actual.same(p)) throw StateError('原智能体报名核实记录未安全保存。');
  }

  @override
  Future<PendingAgentActivityJoin?> read(String e, String o) =>
      _serial(() => _read(e, o));
  @override
  Future<bool> reserve(String e, String o, PendingAgentActivityJoin p) =>
      _serial(() async {
        _bound(e, o, p);
        if (await _read(e, o) != null) return false;
        await storage.write(key: _key(e, o), value: p.encoded);
        await _readBack(e, o, p);
        return true;
      });
  @override
  Future<void> replace(
    String e,
    String o,
    PendingAgentActivityJoin expected,
    PendingAgentActivityJoin next,
  ) => _serial(() async {
    _bound(e, o, expected);
    _bound(e, o, next);
    final actual = await _read(e, o);
    if (actual == null || !actual.same(expected) || !expected.allowsNext(next))
      throw StateError('原智能体报名记录已变化，请重新核实。');
    await storage.write(key: _key(e, o), value: next.encoded);
    await _readBack(e, o, next);
  });
  @override
  Future<void> delete(String e, String o, PendingAgentActivityJoin expected) =>
      _serial(() async {
        _bound(e, o, expected);
        final actual = await _read(e, o);
        if (actual == null) return;
        if (!actual.same(expected)) throw StateError('原智能体报名记录已变化，不能清除。');
        await storage.delete(key: _key(e, o));
        if (await storage.read(key: _key(e, o)) != null)
          throw StateError('原智能体报名核实记录未清除。');
      });
}

class MemoryAgentActivityJoinPendingStore
    implements AgentActivityJoinPendingStore {
  final Map<String, String> values = {};
  PendingAgentActivityJoin? _read(String e, String o) {
    final raw = values[_key(e, o)];
    if (raw == null) return null;
    final p = PendingAgentActivityJoin.decode(raw);
    _bound(e, o, p);
    return p;
  }

  @override
  Future<PendingAgentActivityJoin?> read(String e, String o) async =>
      _read(e, o);
  @override
  Future<bool> reserve(String e, String o, PendingAgentActivityJoin p) async {
    _bound(e, o, p);
    if (_read(e, o) != null) return false;
    values[_key(e, o)] = p.encoded;
    return true;
  }

  @override
  Future<void> replace(
    String e,
    String o,
    PendingAgentActivityJoin expected,
    PendingAgentActivityJoin next,
  ) async {
    _bound(e, o, expected);
    _bound(e, o, next);
    final actual = _read(e, o);
    if (actual == null || !actual.same(expected) || !expected.allowsNext(next))
      throw StateError('原智能体报名记录已变化。');
    values[_key(e, o)] = next.encoded;
  }

  @override
  Future<void> delete(
    String e,
    String o,
    PendingAgentActivityJoin expected,
  ) async {
    _bound(e, o, expected);
    final actual = _read(e, o);
    if (actual != null && !actual.same(expected))
      throw StateError('原智能体报名记录已变化。');
    values.remove(_key(e, o));
  }
}
