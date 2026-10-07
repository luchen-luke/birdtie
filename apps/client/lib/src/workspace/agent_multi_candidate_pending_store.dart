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
  final parsed = candidateTime(raw);
  if (parsed.year < 1 || parsed.year > 9999) {
    throw const FormatException('本机期限不符');
  }
  return parsed;
}

/// Bodyless reconciliation references; never an approval or executable grant.
class PendingMultiCandidateOperation {
  PendingMultiCandidateOperation(Map<String, dynamic> value)
    : data = candidateFreezeJson(value) as Map<String, dynamic> {
    const keys = {
      'environment',
      'ownerId',
      'agentId',
      'sessionFingerprint',
      'phase',
      'multi',
      'previewId',
      'grantId',
      'grantRevision',
      'selectionDigest',
      'reviewDigest',
      'taskId',
      'anchorEventId',
      'logicalOperationId',
      'expiresAt',
      'sourceVersions',
    };
    final hex = RegExp(r'^[0-9a-f]{64}$');
    if (data.length != keys.length ||
        data.keys.toSet().difference(keys).isNotEmpty ||
        [
          'environment',
          'sessionFingerprint',
          'selectionDigest',
          'reviewDigest',
        ].any((k) => data[k] is! String || !hex.hasMatch(data[k])) ||
        [
          'ownerId',
          'agentId',
          'previewId',
        ].any((k) => !candidateIDValid(data[k])) ||
        [
          'grantId',
          'taskId',
          'anchorEventId',
          'logicalOperationId',
        ].any((k) => data[k] != null && !candidateIDValid(data[k])) ||
        !const ['approval', 'stage', 'revocation'].contains(data['phase']) ||
        data['multi'] is! bool ||
        (data['grantRevision'] != null &&
            (data['grantRevision'] is! int ||
                data['grantRevision'] < 1 ||
                data['grantRevision'] >= 9007199254740991)) ||
        (data['phase'] == 'approval' &&
            (data['grantId'] != null || data['grantRevision'] != null)) ||
        data['taskId'] == null ||
        (data['phase'] != 'approval' &&
            (data['grantId'] == null || data['grantRevision'] == null)) ||
        data['sourceVersions'] is! List ||
        data['sourceVersions'].length > 5) {
      throw const FormatException('本机多来源核实记录不符');
    }
    final ids = <String>{};
    for (final v in data['sourceVersions'] as List) {
      if (v is! Map ||
          v.length != 2 ||
          !v.containsKey('momentId') ||
          !v.containsKey('revision') ||
          !candidateIDValid(v['momentId']) ||
          v['revision'] is! int ||
          v['revision'] < 1 ||
          v['revision'] >= 9007199254740991 ||
          !ids.add(v['momentId'])) {
        throw const FormatException('本机来源版本不符');
      }
    }
    if ((data['multi'] == true &&
            (ids.length < 2 ||
                ids.length > 5 ||
                data['anchorEventId'] == null ||
                data['logicalOperationId'] == null)) ||
        (data['multi'] == false &&
            (ids.length != 1 ||
                data['anchorEventId'] != null ||
                data['logicalOperationId'] != null))) {
      throw const FormatException('本机独立来源数量不符');
    }
    if (data['phase'] == 'stage' &&
        (data['multi'] != true ||
            data['taskId'] == null ||
            data['anchorEventId'] == null ||
            data['logicalOperationId'] == null ||
            ids.length < 2)) {
      throw const FormatException('本机提交引用不符');
    }
    _recoveryStamp(data['expiresAt']);
  }
  final Map<String, dynamic> data;
  String get previewID => data['previewId'];
  String get operationKey =>
      '${data['phase']}.$previewID.${data['grantId'] ?? 'preview'}';
  DateTime get expiresAt => _recoveryStamp(data['expiresAt']);
  String get encoded => jsonEncode(data);
  bool same(PendingMultiCandidateOperation other) => encoded == other.encoded;
}

abstract interface class AgentMultiCandidatePendingStore {
  Future<List<PendingMultiCandidateOperation>> read(
    String environment,
    String owner,
  );
  Future<void> write(
    String environment,
    String owner,
    PendingMultiCandidateOperation value,
  );
  Future<void> delete(
    String environment,
    String owner,
    PendingMultiCandidateOperation captured,
  );
}

String _prefix(String environment, String owner) {
  if (environment.isEmpty || !candidateIDValid(owner)) {
    throw const FormatException('本机核实归属不符');
  }
  return 'birdtie.candidate.multi.v1.${candidateRecoveryFingerprint('$environment\n$owner')}.';
}

void _bound(
  String environment,
  String owner,
  PendingMultiCandidateOperation value,
) {
  if (value.data['environment'] != candidateRecoveryFingerprint(environment) ||
      value.data['ownerId'] != owner) {
    throw const FormatException('本机核实归属不符');
  }
}

class SecureAgentMultiCandidatePendingStore
    implements AgentMultiCandidatePendingStore {
  const SecureAgentMultiCandidatePendingStore({
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
  Future<List<PendingMultiCandidateOperation>> read(
    String environment,
    String owner,
  ) => _serial(() async {
    final prefix = _prefix(environment, owner), all = await storage.readAll();
    final result = <PendingMultiCandidateOperation>[];
    for (final e in all.entries.where((e) => e.key.startsWith(prefix))) {
      final value = PendingMultiCandidateOperation(
        jsonDecode(e.value) as Map<String, dynamic>,
      );
      _bound(environment, owner, value);
      if (e.key != '$prefix${value.operationKey}') {
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
    PendingMultiCandidateOperation value,
  ) => _serial(() async {
    _bound(environment, owner, value);
    final prefix = _prefix(environment, owner);
    final all = await storage.readAll();
    for (final e in all.entries.where((e) => e.key.startsWith(prefix))) {
      final existing = PendingMultiCandidateOperation(
        jsonDecode(e.value) as Map<String, dynamic>,
      );
      _bound(environment, owner, existing);
      if (e.key != '$prefix${existing.operationKey}' || !existing.same(value)) {
        throw StateError('已有未核实操作，请先读取原候选');
      }
    }
    final key = '$prefix${value.operationKey}';
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
    PendingMultiCandidateOperation captured,
  ) => _serial(() async {
    _bound(environment, owner, captured);
    final key = '${_prefix(environment, owner)}${captured.operationKey}';
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

class MemoryAgentMultiCandidatePendingStore
    implements AgentMultiCandidatePendingStore {
  final Map<String, String> values = {};
  @override
  Future<List<PendingMultiCandidateOperation>> read(
    String environment,
    String owner,
  ) async {
    final prefix = _prefix(environment, owner);
    final result = <PendingMultiCandidateOperation>[];
    for (final e in values.entries.where((e) => e.key.startsWith(prefix))) {
      final value = PendingMultiCandidateOperation(
        jsonDecode(e.value) as Map<String, dynamic>,
      );
      _bound(environment, owner, value);
      if (e.key != '$prefix${value.operationKey}') {
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
    PendingMultiCandidateOperation value,
  ) async {
    _bound(environment, owner, value);
    final prefix = _prefix(environment, owner);
    for (final e in values.entries.where((e) => e.key.startsWith(prefix))) {
      final existing = PendingMultiCandidateOperation(
        jsonDecode(e.value) as Map<String, dynamic>,
      );
      _bound(environment, owner, existing);
      if (e.key != '$prefix${existing.operationKey}' || !existing.same(value)) {
        throw StateError('已有未核实操作，请先读取原候选');
      }
    }
    final key = '$prefix${value.operationKey}';
    if (values[key] != null && values[key] != value.encoded) {
      throw StateError('原核实操作不可替换');
    }
    values[key] = value.encoded;
  }

  @override
  Future<void> delete(
    String environment,
    String owner,
    PendingMultiCandidateOperation captured,
  ) async {
    _bound(environment, owner, captured);
    final key = '${_prefix(environment, owner)}${captured.operationKey}';
    if (values[key] != null && values[key] != captured.encoded) {
      throw StateError('原核实操作已改变');
    }
    values.remove(key);
  }
}
