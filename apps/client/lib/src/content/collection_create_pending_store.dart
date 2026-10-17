import 'dart:async';
import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import '../auth/session_vault.dart';
import 'collection_control.dart';

const collectionCreatePendingSchema = 'personal-collection-create-recovery-v1';

/// An original operation selector and input fingerprint, never draft or grant.
class CollectionCreatePending {
  const CollectionCreatePending({
    required this.operationID,
    required this.kind,
    required this.cityID,
    required this.payloadDigest,
    required this.stage,
  });
  factory CollectionCreatePending.prepared(
    String operationID,
    CollectionControlBody body,
  ) => CollectionCreatePending.decode({
    'schemaVersion': collectionCreatePendingSchema,
    'operationId': operationID,
    'kind': body.kind,
    'cityId': body.cityID,
    'payloadDigest': inputDigest(body),
    'stage': 'PREPARED',
  });
  final String operationID, kind, cityID, payloadDigest, stage;
  static String inputDigest(CollectionControlBody body) => sha256
      .convert(
        utf8.encode(jsonEncode([collectionCreatePendingSchema, body.tuple])),
      )
      .toString();
  bool same(CollectionCreatePending p) =>
      operationID == p.operationID &&
      kind == p.kind &&
      cityID == p.cityID &&
      payloadDigest == p.payloadDigest;
  CollectionCreatePending get unknown => CollectionCreatePending(
    operationID: operationID,
    kind: kind,
    cityID: cityID,
    payloadDigest: payloadDigest,
    stage: 'UNKNOWN',
  );
  Map<String, Object> toJson() => {
    'schemaVersion': collectionCreatePendingSchema,
    'operationId': operationID,
    'kind': kind,
    'cityId': cityID,
    'payloadDigest': payloadDigest,
    'stage': stage,
  };
  factory CollectionCreatePending.decode(Object? raw) {
    final m = collectionControlObject(raw, {
      'schemaVersion',
      'operationId',
      'kind',
      'cityId',
      'payloadDigest',
      'stage',
    });
    final city = m['cityId'];
    if (m['schemaVersion'] != collectionCreatePendingSchema ||
        !collectionControlID(m['operationId']) ||
        !collectionControlKind(m['kind']) ||
        city is! String ||
        city.trim() != city ||
        utf8.encode(city).length > 80 ||
        city.contains(RegExp(r'[\x00\r\n\t]')) ||
        !collectionControlHash(m['payloadDigest']) ||
        !const {'PREPARED', 'UNKNOWN'}.contains(m['stage'])) {
      throw const FormatException('本机原创建操作记录不符');
    }
    return CollectionCreatePending(
      operationID: m['operationId'],
      kind: m['kind'],
      cityID: city,
      payloadDigest: m['payloadDigest'],
      stage: m['stage'],
    );
  }
}

abstract interface class CollectionCreatePendingStore {
  Future<CollectionCreatePending?> read(
    String origin,
    String owner,
    String kind,
    String city,
  );
  Future<void> write(String origin, String owner, CollectionCreatePending p);
  Future<void> remove(String origin, String owner, CollectionCreatePending p);
}

String _key(String origin, String owner, String kind, String city) {
  if (!canonicalStoredAuthOrigin(origin) ||
      !collectionControlID(owner) ||
      !collectionControlKind(kind) ||
      city.trim() != city ||
      utf8.encode(city).length > 80 ||
      city.contains(RegExp(r'[\x00\r\n\t]'))) {
    throw const FormatException('原创建操作来源或本人不符');
  }
  return 'birdtie.collection-create.v1.' +
      sha256
          .convert(
            utf8.encode(jsonEncode([origin, owner, 'personal', kind, city])),
          )
          .toString();
}

void _transition(CollectionCreatePending? old, CollectionCreatePending p) {
  CollectionCreatePending.decode(p.toJson());
  if (old != null && !old.same(p)) throw StateError('原创建操作尚未核实');
  if (p.stage == 'UNKNOWN' && old == null) throw StateError('原创建记录已退休');
  if (old?.stage == 'UNKNOWN' && p.stage != 'UNKNOWN')
    throw StateError('未知原创建不能重放');
}

class SecureCollectionCreatePendingStore
    implements CollectionCreatePendingStore {
  const SecureCollectionCreatePendingStore({
    this.storage = const FlutterSecureStorage(),
  });
  final FlutterSecureStorage storage;
  static final _tails = <String, Future<void>>{};
  Future<T> _lock<T>(String key, Future<T> Function() action) async {
    final prior = _tails[key], done = Completer<void>();
    _tails[key] = done.future;
    try {
      if (prior != null) await prior;
      return await action();
    } finally {
      if (identical(_tails[key], done.future)) _tails.remove(key);
      done.complete();
    }
  }

  CollectionCreatePending _decode(
    String raw,
    String origin,
    String owner,
    String kind,
    String city,
  ) {
    final m = collectionControlObject(collectionControlJSON(raw), {
      'origin',
      'owner',
      'workspace',
      'pending',
    });
    final p = CollectionCreatePending.decode(m['pending']);
    if (m['origin'] != origin ||
        m['owner'] != owner ||
        m['workspace'] != 'personal' ||
        p.kind != kind ||
        p.cityID != city)
      throw const FormatException('原创建记录归属不符');
    return p;
  }

  @override
  Future<CollectionCreatePending?> read(
    String origin,
    String owner,
    String kind,
    String city,
  ) => _lock(_key(origin, owner, kind, city), () async {
    final raw = await storage.read(key: _key(origin, owner, kind, city));
    return raw == null ? null : _decode(raw, origin, owner, kind, city);
  });
  @override
  Future<void> write(String origin, String owner, CollectionCreatePending p) =>
      _lock(_key(origin, owner, p.kind, p.cityID), () async {
        final key = _key(origin, owner, p.kind, p.cityID),
            raw = await storage.read(key: key);
        _transition(
          raw == null ? null : _decode(raw, origin, owner, p.kind, p.cityID),
          p,
        );
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
  Future<void> remove(String origin, String owner, CollectionCreatePending p) =>
      _lock(_key(origin, owner, p.kind, p.cityID), () async {
        final key = _key(origin, owner, p.kind, p.cityID),
            raw = await storage.read(key: key);
        if (raw == null) return;
        final old = _decode(raw, origin, owner, p.kind, p.cityID);
        if (!old.same(p) || old.stage != p.stage)
          throw StateError('原创建记录阶段已变化');
        await storage.delete(key: key);
      });
}

class MemoryCollectionCreatePendingStore
    implements CollectionCreatePendingStore {
  final values = <String, CollectionCreatePending>{};
  @override
  Future<CollectionCreatePending?> read(
    String origin,
    String owner,
    String kind,
    String city,
  ) async => values[_key(origin, owner, kind, city)];
  @override
  Future<void> write(
    String origin,
    String owner,
    CollectionCreatePending p,
  ) async {
    final key = _key(origin, owner, p.kind, p.cityID);
    _transition(values[key], p);
    values[key] = p;
  }

  @override
  Future<void> remove(
    String origin,
    String owner,
    CollectionCreatePending p,
  ) async {
    final key = _key(origin, owner, p.kind, p.cityID), old = values[key];
    if (old == null) return;
    if (!old.same(p) || old.stage != p.stage) throw StateError('原创建记录阶段已变化');
    values.remove(key);
  }
}
