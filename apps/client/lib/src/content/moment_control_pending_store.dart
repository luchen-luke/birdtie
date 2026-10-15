import 'dart:async';
import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import '../workspace/human_causal_control.dart';
import 'moment_control.dart';

/// Enough to reconstruct the sealed media tuple, never a reusable approval.
class MomentControlMediaSelector {
  const MomentControlMediaSelector({
    required this.mediaID,
    required this.version,
    required this.rightsDigest,
  });
  final String mediaID, rightsDigest;
  final int version;
  List<Object> get digestTuple => [mediaID, version, rightsDigest];
  Map<String, Object> toJson() => {
    'mediaId': mediaID,
    'mediaVersion': version,
    'rightsDigest': rightsDigest,
  };
  factory MomentControlMediaSelector.fromJson(Object? raw) {
    final m = humanControlObject(raw, {
      'mediaId',
      'mediaVersion',
      'rightsDigest',
    });
    if (!humanControlID(m['mediaId']) ||
        !humanControlVersion(m['mediaVersion'], zero: false) ||
        !humanControlHash(m['rightsDigest'])) {
      throw const FormatException('动态原材料恢复选择器不符');
    }
    return MomentControlMediaSelector(
      mediaID: m['mediaId'],
      version: m['mediaVersion'],
      rightsDigest: m['rightsDigest'],
    );
  }
}

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
    this.mediaSelectors,
    this.localScope = '',
  });
  final String operationID,
      action,
      momentID,
      inputHash,
      payloadDigest,
      sourceVersion,
      stage;
  final int expectedRevision;
  final List<MomentControlMediaSelector>? mediaSelectors;
  final String localScope;
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
        mediaSelectors: p.input.media == null
            ? null
            : List.unmodifiable(
                p.input.media!.map(
                  (selection) => MomentControlMediaSelector(
                    mediaID: selection.mediaID,
                    version: selection.version,
                    rightsDigest: selection.rights.digest,
                  ),
                ),
              ),
        localScope: p.input.localScope,
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
    mediaSelectors: mediaSelectors,
    localScope: localScope,
  );
  bool same(MomentControlPending p) =>
      operationID == p.operationID &&
      action == p.action &&
      momentID == p.momentID &&
      expectedRevision == p.expectedRevision &&
      inputHash == p.inputHash &&
      payloadDigest == p.payloadDigest &&
      sourceVersion == p.sourceVersion &&
      localScope == p.localScope &&
      jsonEncode(mediaSelectors?.map((v) => v.toJson()).toList()) ==
          jsonEncode(p.mediaSelectors?.map((v) => v.toJson()).toList());
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
    if (mediaSelectors != null) ...{
      'mediaSelectors': mediaSelectors!.map((v) => v.toJson()).toList(),
      'localScope': localScope,
    },
  };
  factory MomentControlPending.fromJson(Object? raw) {
    final m = humanControlObject(
      raw,
      {
        'schema',
        'operationId',
        'action',
        'momentId',
        'expectedRevision',
        'inputSha256',
        'payloadDigest',
        'sourceVersion',
        'stage',
      },
      optional: {'mediaSelectors', 'localScope'},
    );
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
    List<MomentControlMediaSelector>? selectors;
    final hasMedia = m.containsKey('mediaSelectors');
    if (hasMedia != m.containsKey('localScope')) {
      throw const FormatException('动态原材料恢复范围不符');
    }
    if (hasMedia) {
      final media = m['mediaSelectors'], scope = m['localScope'];
      if (!const {'PUBLISH', 'UPDATE_PUBLIC'}.contains(action) ||
          media is! List ||
          media.length > 9 ||
          scope is! String ||
          (media.isEmpty ? scope.isNotEmpty : !humanControlID(scope))) {
        throw const FormatException('动态原材料恢复范围不符');
      }
      selectors = List.unmodifiable(
        media.map(MomentControlMediaSelector.fromJson),
      );
      if (selectors.map((v) => v.mediaID).toSet().length != selectors.length) {
        throw const FormatException('动态原材料恢复标识重复');
      }
    }
    final digest = momentHash([
      'human-moment-control-v1',
      m['operationId'],
      action,
      m['momentId'],
      m['expectedRevision'],
      m['inputSha256'],
      if (selectors != null) ...[
        'moment-media-v1',
        m['localScope'],
        selectors.map((v) => v.digestTuple).toList(),
      ],
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
      mediaSelectors: selectors,
      localScope: hasMedia ? m['localScope'] : '',
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
