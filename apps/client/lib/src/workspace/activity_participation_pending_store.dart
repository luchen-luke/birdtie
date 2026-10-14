import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'activity_participation_operation.dart';

/// Match the HTTP endpoint rule without changing hosts, ports or path aliases.
String participationEnvironment(String environment) {
  final u = Uri.tryParse(environment);
  if (u == null ||
      !u.hasAuthority ||
      u.host.isEmpty ||
      !const {'http', 'https'}.contains(u.scheme) ||
      u.userInfo.isNotEmpty ||
      u.hasQuery ||
      u.hasFragment ||
      environment.endsWith('//')) {
    throw const FormatException('报名恢复记录环境无效。');
  }
  return environment.endsWith('/')
      ? environment.substring(0, environment.length - 1)
      : environment;
}

List<String> _environments(String environment, String owner) {
  if (!participationID(owner)) {
    throw const FormatException('报名恢复记录归属无效。');
  }
  final canonical = participationEnvironment(environment);
  return [canonical, '$canonical/'];
}

List<PendingParticipationOperation> _merge(
  Iterable<PendingParticipationOperation> entries,
) {
  final rows = entries.toList();
  if (rows.length > 32) {
    throw const FormatException('待核实报名过多。');
  }
  final operations = <String, PendingParticipationOperation>{};
  for (final p in rows) {
    PendingParticipationOperation.decode(p.toJson());
    final old = operations[p.operationID];
    if (old != null && !old.sameBinding(p)) {
      throw const FormatException('原报名记录绑定冲突，请保留并核实。');
    }
    operations[p.operationID] = old?.stopRequested == true ? old! : p;
  }
  if (operations.length > 16 ||
      operations.values.map((p) => p.activityID).toSet().length !=
          operations.length) {
    throw const FormatException('原报名记录不唯一，请保留并核实。');
  }
  return operations.values.toList();
}

abstract interface class ParticipationPendingStore {
  Future<List<PendingParticipationOperation>> read(
    String environment,
    String owner,
  );
  Future<void> write(
    String environment,
    String owner,
    PendingParticipationOperation pending,
  );
  Future<void> delete(
    String environment,
    String owner,
    PendingParticipationOperation pending,
  );
}

void _writeAllowed(
  List<PendingParticipationOperation> all,
  PendingParticipationOperation p,
) {
  PendingParticipationOperation.decode(p.toJson());
  final same = all.where((x) => x.operationID == p.operationID).toList();
  if (same.isNotEmpty &&
      (!same.single.sameBinding(p) ||
          (same.single.stopRequested && !p.stopRequested))) {
    throw StateError('原报名记录不能更换绑定或恢复提交。');
  }
  if (same.isEmpty &&
      (all.length >= 16 || all.any((x) => x.activityID == p.activityID))) {
    throw StateError('请先核实这个活动的原报名操作。');
  }
}

void _deleteAllowed(
  List<PendingParticipationOperation> all,
  PendingParticipationOperation p,
) {
  PendingParticipationOperation.decode(p.toJson());
  final same = all.where((x) => x.operationID == p.operationID).toList();
  if (same.isNotEmpty &&
      (!same.single.sameBinding(p) ||
          same.single.stopRequested != p.stopRequested)) {
    throw StateError('原报名记录已变化，不能清除。');
  }
}

class SecureParticipationPendingStore implements ParticipationPendingStore {
  SecureParticipationPendingStore({
    this.storage = const FlutterSecureStorage(),
  });
  final FlutterSecureStorage storage;
  static Future<void> _tail = Future.value();
  Future<T> _exclusive<T>(Future<T> Function() f) {
    final next = _tail.then((_) => f());
    _tail = next.then<void>((_) {}, onError: (Object _, StackTrace _) {});
    return next;
  }

  String _prefix(String environment, String owner) {
    return 'birdtie.human-participation.pending.v1.${sha256.convert(utf8.encode('$environment\n$owner'))}.';
  }

  Future<List<(String, PendingParticipationOperation)>> _rows(
    String e,
    String o,
  ) async {
    final prefixes = [
      for (final alias in _environments(e, o)) _prefix(alias, o),
    ];
    final all = await storage.readAll();
    return [
      for (final row in all.entries)
        for (final prefix in prefixes)
          if (row.key.startsWith(prefix)) (row.key, _decode(row, prefix)),
    ];
  }

  Future<List<PendingParticipationOperation>> _read(String e, String o) async {
    return _merge((await _rows(e, o)).map((row) => row.$2));
  }

  PendingParticipationOperation _decode(
    MapEntry<String, String> row,
    String prefix,
  ) {
    if (row.value.length > 1024) throw const FormatException('报名恢复记录过大。');
    final p = PendingParticipationOperation.decode(
      participationWireJSON(row.value),
    );
    if (row.key != '$prefix${p.operationID}') {
      throw const FormatException('报名恢复记录地址不符。');
    }
    return p;
  }

  @override
  Future<List<PendingParticipationOperation>> read(String e, String o) =>
      _exclusive(() => _read(e, o));
  @override
  Future<void> write(String e, String o, PendingParticipationOperation p) =>
      _exclusive(() async {
        _writeAllowed(await _read(e, o), p);
        await storage.write(
          key: '${_prefix(participationEnvironment(e), o)}${p.operationID}',
          value: jsonEncode(p.toJson()),
        );
      });
  @override
  Future<void> delete(String e, String o, PendingParticipationOperation p) =>
      _exclusive(() async {
        final rows = await _rows(e, o);
        _deleteAllowed(_merge(rows.map((row) => row.$2)), p);
        final key =
            '${_prefix(participationEnvironment(e), o)}${p.operationID}';
        final matching = rows
            .where((row) => row.$2.operationID == p.operationID)
            .toList();
        matching.sort((a, b) {
          if (a.$1 == b.$1) return 0;
          if (a.$2.stopRequested != b.$2.stopRequested) {
            return a.$2.stopRequested ? 1 : -1;
          }
          return a.$1 == key
              ? 1
              : b.$1 == key
              ? -1
              : 0;
        });
        // Leave a stopping copy until all earlier copies are removed. A failed
        // clear retains UNKNOWN and cannot turn a close back into an execute.
        for (final row in matching) {
          await storage.delete(key: row.$1);
        }
      });
}

class MemoryParticipationPendingStore implements ParticipationPendingStore {
  final values = <(String, String, String), PendingParticipationOperation>{};

  List<PendingParticipationOperation> _read(String e, String o) {
    final aliases = _environments(e, o);
    final entries = values.entries.where(
      (x) => aliases.contains(x.key.$1) && x.key.$2 == o,
    );
    if (entries.any((x) => x.key.$3 != x.value.operationID)) {
      throw const FormatException('报名恢复记录地址不符。');
    }
    return _merge(entries.map((x) => x.value));
  }

  @override
  Future<List<PendingParticipationOperation>> read(String e, String o) async =>
      _read(e, o);
  @override
  Future<void> write(
    String e,
    String o,
    PendingParticipationOperation p,
  ) async {
    _writeAllowed(_read(e, o), p);
    values[(participationEnvironment(e), o, p.operationID)] = p;
  }

  @override
  Future<void> delete(
    String e,
    String o,
    PendingParticipationOperation p,
  ) async {
    _deleteAllowed(_read(e, o), p);
    final aliases = _environments(e, o);
    values.removeWhere(
      (key, value) =>
          aliases.contains(key.$1) && key.$2 == o && key.$3 == p.operationID,
    );
  }
}
