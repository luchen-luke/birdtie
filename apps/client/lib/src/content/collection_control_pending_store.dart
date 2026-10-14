import 'dart:async';
import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import '../auth/session_vault.dart';
import 'collection_control.dart';

/// Only the original operation selectors survive a restart, never the draft.
class CollectionControlPending {
  const CollectionControlPending({
    required this.operationID,
    required this.action,
    required this.kind,
    required this.collectionID,
    required this.expectedRevision,
    required this.payloadDigest,
    required this.stage,
  });
  factory CollectionControlPending.preview(CollectionControlPreview p) =>
      CollectionControlPending(
        operationID: p.input.operationID,
        action: p.input.action,
        kind: p.input.kind,
        collectionID: p.input.collectionID,
        expectedRevision: p.input.expectedRevision,
        payloadDigest: p.input.payloadDigest,
        stage: 'PREPARED',
      );
  final String operationID, action, kind, collectionID, payloadDigest, stage;
  final int expectedRevision;
  bool same(CollectionControlPending p) =>
      operationID == p.operationID &&
      action == p.action &&
      kind == p.kind &&
      collectionID == p.collectionID &&
      expectedRevision == p.expectedRevision &&
      payloadDigest == p.payloadDigest;
  CollectionControlPending get unknown => CollectionControlPending(
    operationID: operationID,
    action: action,
    kind: kind,
    collectionID: collectionID,
    expectedRevision: expectedRevision,
    payloadDigest: payloadDigest,
    stage: 'UNKNOWN',
  );
  Map<String, Object> toJson() => {
    'schemaVersion': collectionControlSchema,
    'operationId': operationID,
    'action': action,
    'kind': kind,
    'collectionId': collectionID,
    'expectedRevision': expectedRevision,
    'payloadDigest': payloadDigest,
    'stage': stage,
  };
  factory CollectionControlPending.decode(Object? raw) {
    final m = collectionControlObject(raw, {
      'schemaVersion',
      'operationId',
      'action',
      'kind',
      'collectionId',
      'expectedRevision',
      'payloadDigest',
      'stage',
    });
    if (m['schemaVersion'] != collectionControlSchema ||
        !collectionControlID(m['operationId']) ||
        !collectionControlID(m['collectionId']) ||
        !collectionControlKind(m['kind']) ||
        !collectionControlVersion(m['expectedRevision']) ||
        !const {'UPDATE', 'WITHDRAW'}.contains(m['action']) ||
        m['action'] == 'UPDATE' &&
            m['expectedRevision'] == collectionControlMaxRevision ||
        !collectionControlHash(m['payloadDigest']) ||
        !const {'PREPARED', 'UNKNOWN'}.contains(m['stage'])) {
      throw const FormatException('本机原集合操作记录不符');
    }
    return CollectionControlPending(
      operationID: m['operationId'],
      action: m['action'],
      kind: m['kind'],
      collectionID: m['collectionId'],
      expectedRevision: m['expectedRevision'],
      payloadDigest: m['payloadDigest'],
      stage: m['stage'],
    );
  }
}

abstract interface class CollectionControlPendingStore {
  Future<List<CollectionControlPending>> read(String origin, String owner);
  Future<void> write(String origin, String owner, CollectionControlPending p);
  Future<void> remove(String origin, String owner, CollectionControlPending p);
}

void _transition(CollectionControlPending? old, CollectionControlPending p) {
  CollectionControlPending.decode(p.toJson());
  if (old != null && !old.same(p)) throw StateError('原集合操作不能换输入');
  if (p.stage == 'UNKNOWN' && old == null) throw StateError('原准备记录已经退休');
  if (old?.stage == 'UNKNOWN' && p.stage != 'UNKNOWN')
    throw StateError('未知原操作不能重放');
}

String _namespace(String origin, String owner) {
  if (!canonicalStoredAuthOrigin(origin) || !collectionControlID(owner)) {
    throw const FormatException('集合操作归属来源不符');
  }
  return 'birdtie.personal-collection-control.v1.' +
      sha256
          .convert(utf8.encode(jsonEncode([origin, owner, 'personal'])))
          .toString() +
      '.';
}

class SecureCollectionControlPendingStore
    implements CollectionControlPendingStore {
  const SecureCollectionControlPendingStore({
    this.storage = const FlutterSecureStorage(),
  });
  final FlutterSecureStorage storage;
  static final _tails = <String, Future<void>>{};
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

  CollectionControlPending _decode(String raw, String origin, String owner) {
    final m = collectionControlObject(collectionControlJSON(raw), {
      'origin',
      'owner',
      'workspace',
      'pending',
    });
    if (m['origin'] != origin ||
        m['owner'] != owner ||
        m['workspace'] != 'personal') {
      throw const FormatException('本机原记录属于其他来源或本人');
    }
    return CollectionControlPending.decode(m['pending']);
  }

  @override
  Future<List<CollectionControlPending>> read(String origin, String owner) =>
      _lock(_namespace(origin, owner), () async {
        final prefix = _namespace(origin, owner), all = await storage.readAll();
        final values = <CollectionControlPending>[];
        for (final e in all.entries.where((e) => e.key.startsWith(prefix))) {
          final p = _decode(e.value, origin, owner);
          if (e.key != prefix + p.operationID)
            throw const FormatException('原操作键不符');
          values.add(p);
        }
        return List.unmodifiable(values);
      });
  @override
  Future<void> write(String origin, String owner, CollectionControlPending p) =>
      _lock(_namespace(origin, owner), () async {
        final key = _namespace(origin, owner) + p.operationID,
            raw = await storage.read(key: key);
        _transition(raw == null ? null : _decode(raw, origin, owner), p);
        await storage.write(
          key: key,
          value: jsonEncode({
            'origin': origin,
            'owner': owner,
            'workspace': 'personal',
            'pending': p.toJson(),
          }),
        );
      });
  @override
  Future<void> remove(
    String origin,
    String owner,
    CollectionControlPending p,
  ) => _lock(_namespace(origin, owner), () async {
    final key = _namespace(origin, owner) + p.operationID,
        raw = await storage.read(key: key);
    if (raw == null) return;
    final old = _decode(raw, origin, owner);
    if (!old.same(p) || old.stage != p.stage) throw StateError('本机原操作阶段已变化');
    await storage.delete(key: key);
  });
}

class MemoryCollectionControlPendingStore
    implements CollectionControlPendingStore {
  final values = <(String, String, String), CollectionControlPending>{};
  @override
  Future<List<CollectionControlPending>> read(
    String origin,
    String owner,
  ) async {
    _namespace(origin, owner);
    return List.unmodifiable(
      values.entries
          .where((e) => e.key.$1 == origin && e.key.$2 == owner)
          .map((e) => e.value),
    );
  }

  @override
  Future<void> write(
    String origin,
    String owner,
    CollectionControlPending p,
  ) async {
    _namespace(origin, owner);
    final key = (origin, owner, p.operationID);
    _transition(values[key], p);
    values[key] = p;
  }

  @override
  Future<void> remove(
    String origin,
    String owner,
    CollectionControlPending p,
  ) async {
    _namespace(origin, owner);
    final key = (origin, owner, p.operationID), old = values[key];
    if (old == null) return;
    if (!old.same(p) || old.stage != p.stage) throw StateError('本机原操作阶段已变化');
    values.remove(key);
  }
}
