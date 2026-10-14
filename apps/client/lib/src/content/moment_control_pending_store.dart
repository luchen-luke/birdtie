import 'dart:async';
import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import '../workspace/human_causal_control.dart';
import 'moment_control.dart';

class MomentControlPending {
  const MomentControlPending({
    required this.operationID,
    required this.action,
    required this.momentID,
    required this.expectedRevision,
    required this.inputHash,
    required this.payloadDigest,
    required this.sourceVersion,
    required this.stage,
  });
  final String operationID,
      action,
      momentID,
      inputHash,
      payloadDigest,
      sourceVersion,
      stage;
  final int expectedRevision;
  factory MomentControlPending.preview(MomentControlPreview p) =>
      MomentControlPending(
        operationID: p.input.operationID,
        action: p.input.action,
        momentID: p.input.momentID,
        expectedRevision: p.input.expectedRevision,
        inputHash: p.input.inputHash,
        payloadDigest: p.input.digest,
        sourceVersion: p.sourceVersion,
        stage: 'PREPARED',
      );
  MomentControlPending get unknown => MomentControlPending(
    operationID: operationID,
    action: action,
    momentID: momentID,
    expectedRevision: expectedRevision,
    inputHash: inputHash,
    payloadDigest: payloadDigest,
    sourceVersion: sourceVersion,
    stage: 'UNKNOWN',
  );
  bool same(MomentControlPending p) =>
      operationID == p.operationID &&
      action == p.action &&
      momentID == p.momentID &&
      expectedRevision == p.expectedRevision &&
      inputHash == p.inputHash &&
      payloadDigest == p.payloadDigest &&
      sourceVersion == p.sourceVersion;
  Map<String, Object> toJson() => {
    'schema': 'moment-control-v1',
    'operationId': operationID,
    'action': action,
    'momentId': momentID,
    'expectedRevision': expectedRevision,
    'inputSha256': inputHash,
    'payloadDigest': payloadDigest,
    'sourceVersion': sourceVersion,
    'stage': stage,
  };
  factory MomentControlPending.fromJson(Object? raw) {
    final m = humanControlObject(raw, {
      'schema',
      'operationId',
      'action',
      'momentId',
      'expectedRevision',
      'inputSha256',
      'payloadDigest',
      'sourceVersion',
      'stage',
    });
    final action = m['action'];
    if (m['schema'] != 'moment-control-v1' ||
        !humanControlID(m['operationId']) ||
        !const {
          'CREATE',
          'UPDATE',
          'WITHDRAW',
          'PUBLISH',
          'UPDATE_PUBLIC',
        }.contains(action) ||
        !const {'PREPARED', 'UNKNOWN'}.contains(m['stage']) ||
        !humanControlVersion(m['expectedRevision']) ||
        !humanControlHash(m['sourceVersion']) ||
        !humanControlHash(m['payloadDigest']) ||
        (action == 'CREATE'
            ? m['momentId'] != '' || m['expectedRevision'] != 0
            : !humanControlID(m['momentId']) || m['expectedRevision'] < 1) ||
        (action == 'WITHDRAW'
            ? m['inputSha256'] != ''
            : !humanControlHash(m['inputSha256'])) ||
        const {'UPDATE', 'PUBLISH', 'UPDATE_PUBLIC'}.contains(action) &&
            m['expectedRevision'] >= momentControlMaxRevision)
      throw const FormatException('动态恢复选择器不符');
    final digest = momentHash([
      'human-moment-control-v1',
      m['operationId'],
      action,
      m['momentId'],
      m['expectedRevision'],
      m['inputSha256'],
    ]);
    if (digest != m['payloadDigest']) throw const FormatException('动态恢复摘要不符');
    return MomentControlPending(
      operationID: m['operationId'],
      action: action,
      momentID: m['momentId'],
      expectedRevision: m['expectedRevision'],
      inputHash: m['inputSha256'],
      payloadDigest: m['payloadDigest'],
      sourceVersion: m['sourceVersion'],
      stage: m['stage'],
    );
  }
}

abstract interface class MomentControlPendingStore {
  Future<List<MomentControlPending>> read(String origin, String owner);
  Future<void> write(String origin, String owner, MomentControlPending p);
  Future<void> remove(String origin, String owner, MomentControlPending p);
}

void _transition(MomentControlPending? old, MomentControlPending next) {
  MomentControlPending.fromJson(next.toJson());
  if (old != null && !old.same(next)) throw StateError('原动态操作不能换输入');
  if (next.stage == 'UNKNOWN' && old == null) throw StateError('原准备记录已经退休');
  if (old?.stage == 'UNKNOWN' && next.stage != 'UNKNOWN')
    throw StateError('未知操作不能重放');
}

class SecureMomentControlPendingStore implements MomentControlPendingStore {
  const SecureMomentControlPendingStore({
    this.storage = const FlutterSecureStorage(),
  });
  final FlutterSecureStorage storage;
  static final _tails = <String, Future<void>>{};
  String _prefix(String origin, String owner) {
    if (origin.isEmpty || !humanControlID(owner))
      throw const FormatException('本人动态归属不符');
    return 'birdtie.moment-control.v1.${sha256.convert(utf8.encode('$origin\n$owner'))}.';
  }

  Future<T> _lock<T>(String key, Future<T> Function() run) async {
    final prior = _tails[key], done = Completer<void>();
    _tails[key] = done.future;
    try {
      if (prior != null) await prior;
      return await run();
    } finally {
      if (identical(_tails[key], done.future)) _tails.remove(key);
      done.complete();
    }
  }

  @override
  Future<List<MomentControlPending>> read(String origin, String owner) =>
      _lock(_prefix(origin, owner), () async {
        final prefix = _prefix(origin, owner),
            all = await storage.readAll(),
            values = <MomentControlPending>[];
        for (final e in all.entries.where((e) => e.key.startsWith(prefix))) {
          final p = MomentControlPending.fromJson(jsonDecode(e.value));
          if (e.key != '$prefix${p.operationID}')
            throw const FormatException('原动态键不符');
          values.add(p);
        }
        return values;
      });
  @override
  Future<void> write(String origin, String owner, MomentControlPending p) =>
      _lock(_prefix(origin, owner), () async {
        final key = '${_prefix(origin, owner)}${p.operationID}',
            old = await storage.read(key: key);
        _transition(
          old == null ? null : MomentControlPending.fromJson(jsonDecode(old)),
          p,
        );
        await storage.write(key: key, value: jsonEncode(p.toJson()));
      });
  @override
  Future<void> remove(String origin, String owner, MomentControlPending p) =>
      _lock(_prefix(origin, owner), () async {
        final key = '${_prefix(origin, owner)}${p.operationID}',
            raw = await storage.read(key: key);
        if (raw == null) return;
        final old = MomentControlPending.fromJson(jsonDecode(raw));
        if (!old.same(p) || old.stage != p.stage) throw StateError('本机原操作已变化');
        await storage.delete(key: key);
      });
}

class MemoryMomentControlPendingStore implements MomentControlPendingStore {
  final values = <(String, String, String), MomentControlPending>{};
  @override
  Future<List<MomentControlPending>> read(String origin, String owner) async =>
      values.entries
          .where((e) => e.key.$1 == origin && e.key.$2 == owner)
          .map((e) => e.value)
          .toList();
  @override
  Future<void> write(
    String origin,
    String owner,
    MomentControlPending p,
  ) async {
    final key = (origin, owner, p.operationID);
    _transition(values[key], p);
    values[key] = p;
  }

  @override
  Future<void> remove(
    String origin,
    String owner,
    MomentControlPending p,
  ) async {
    final key = (origin, owner, p.operationID), old = values[key];
    if (old == null) return;
    if (!old.same(p) || old.stage != p.stage) throw StateError('本机原操作已变化');
    values.remove(key);
  }
}
