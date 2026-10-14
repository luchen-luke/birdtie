import 'dart:async';
import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

const collectionCopyMaxVersion = 8999999999999999;
bool collectionCopyID(Object? value) =>
    value is String &&
    RegExp(
      r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
    ).hasMatch(value);
bool collectionCopyVersion(Object? value) =>
    value is int && value >= 1 && value <= collectionCopyMaxVersion;
bool collectionCopyHash(Object? value) =>
    value is String && RegExp(r'^[0-9a-f]{64}$').hasMatch(value);
Map<String, dynamic> collectionCopyObject(Object? raw, Set<String> keys) {
  if (raw is! Map<String, dynamic> ||
      raw.length != keys.length ||
      !keys.every(raw.containsKey))
    throw const FormatException('副本资料结构不符');
  return raw;
}

class CollectionCopyReference {
  const CollectionCopyReference(this.kind, this.id);
  final String kind, id;
  bool get valid =>
      const {'journey', 'theme_map'}.contains(kind) && collectionCopyID(id);
  bool same(CollectionCopyReference other) =>
      kind == other.kind && id == other.id;
  Map<String, String> toJson() => {'kind': kind, 'id': id};
  String get code => 'birdtie:collection/$kind/$id';
  factory CollectionCopyReference.fromJson(Object? raw) {
    final m = collectionCopyObject(raw, {'kind', 'id'});
    if (m['kind'] is! String || m['id'] is! String)
      throw const FormatException('副本来源标识不符');
    final ref = CollectionCopyReference(m['kind'], m['id']);
    if (!ref.valid) throw const FormatException('副本来源标识不符');
    return ref;
  }
}

/// These selectors cannot recreate the title, location IDs, body or approval.
class CollectionCopySelector {
  const CollectionCopySelector({
    required this.sourceRef,
    required this.operationID,
    required this.publicationRevision,
    required this.grantVersion,
    required this.orderedPlaceIDsHash,
    required this.inputHash,
  });
  final CollectionCopyReference sourceRef;
  final String operationID, orderedPlaceIDsHash, inputHash;
  final int publicationRevision, grantVersion;
  bool get valid =>
      sourceRef.valid &&
      collectionCopyID(operationID) &&
      collectionCopyVersion(publicationRevision) &&
      publicationRevision >= 2 &&
      collectionCopyVersion(grantVersion) &&
      collectionCopyHash(orderedPlaceIDsHash) &&
      collectionCopyHash(inputHash);
  bool same(CollectionCopySelector other) =>
      sourceRef.same(other.sourceRef) &&
      operationID == other.operationID &&
      publicationRevision == other.publicationRevision &&
      grantVersion == other.grantVersion &&
      orderedPlaceIDsHash == other.orderedPlaceIDsHash &&
      inputHash == other.inputHash;
  Map<String, Object> toJson() => {
    'sourceRef': sourceRef.toJson(),
    'operationId': operationID,
    'publicationRevision': publicationRevision,
    'grantVersion': grantVersion,
    'orderedPlaceIdsSha256': orderedPlaceIDsHash,
    'inputSha256': inputHash,
  };
  factory CollectionCopySelector.fromJson(Object? raw) {
    final m = collectionCopyObject(raw, {
      'sourceRef',
      'operationId',
      'publicationRevision',
      'grantVersion',
      'orderedPlaceIdsSha256',
      'inputSha256',
    });
    if (m['operationId'] is! String ||
        m['publicationRevision'] is! int ||
        m['grantVersion'] is! int ||
        m['orderedPlaceIdsSha256'] is! String ||
        m['inputSha256'] is! String)
      throw const FormatException('原副本操作资料不符');
    final s = CollectionCopySelector(
      sourceRef: CollectionCopyReference.fromJson(m['sourceRef']),
      operationID: m['operationId'],
      publicationRevision: m['publicationRevision'],
      grantVersion: m['grantVersion'],
      orderedPlaceIDsHash: m['orderedPlaceIdsSha256'],
      inputHash: m['inputSha256'],
    );
    if (!s.valid) throw const FormatException('原副本操作资料不符');
    return s;
  }
}

class CollectionCopyPending {
  const CollectionCopyPending(this.selector, {this.stage = 'UNKNOWN'});
  final CollectionCopySelector selector;
  final String stage;
  bool same(CollectionCopyPending other) =>
      selector.same(other.selector) && stage == other.stage;
  bool get valid =>
      selector.valid && const {'PREPARED', 'UNKNOWN'}.contains(stage);
  Map<String, Object> toJson() => {
    'selector': selector.toJson(),
    'stage': stage,
  };
  factory CollectionCopyPending.fromJson(Object? raw) {
    final m = collectionCopyObject(raw, {'selector', 'stage'});
    if (m['stage'] is! String) throw const FormatException('本机副本准备阶段不符');
    final p = CollectionCopyPending(
      CollectionCopySelector.fromJson(m['selector']),
      stage: m['stage'],
    );
    if (!p.valid) throw const FormatException('本机副本准备阶段不符');
    return p;
  }
}

abstract interface class CollectionCopyPendingStore {
  Future<CollectionCopyPending?> read(String origin, String owner);
  Future<void> write(
    String origin,
    String owner,
    CollectionCopyPending pending,
  );
  Future<void> remove(
    String origin,
    String owner,
    CollectionCopyPending pending,
  );
}

class SecureCollectionCopyPendingStore implements CollectionCopyPendingStore {
  SecureCollectionCopyPendingStore({FlutterSecureStorage? storage})
    : _storage = storage ?? const FlutterSecureStorage();
  final FlutterSecureStorage _storage;
  static final _tails = <String, Future<void>>{};
  String _key(String origin, String owner) =>
      'birdtie.collection-private-copy.v1.${sha256.convert(utf8.encode(jsonEncode([origin, owner])))}';
  Future<T> _serial<T>(String key, Future<T> Function() action) async {
    final previous = _tails[key], done = Completer<void>();
    _tails[key] = done.future;
    try {
      if (previous != null) await previous;
      return await action();
    } finally {
      if (identical(_tails[key], done.future)) _tails.remove(key);
      done.complete();
    }
  }

  Future<CollectionCopyPending?> _read(String origin, String owner) async {
    final raw = await _storage.read(key: _key(origin, owner));
    if (raw == null) return null;
    final m = collectionCopyObject(jsonDecode(raw), {
      'origin',
      'owner',
      'pending',
    });
    if (m['origin'] != origin || m['owner'] != owner)
      throw const FormatException('原副本操作属于其他账号或来源');
    return CollectionCopyPending.fromJson(m['pending']);
  }

  void _context(String origin, String owner) {
    final uri = Uri.tryParse(origin);
    if (!collectionCopyID(owner) ||
        uri == null ||
        !const {'http', 'https'}.contains(uri.scheme) ||
        uri.host.isEmpty ||
        uri.userInfo.isNotEmpty ||
        uri.hasFragment ||
        uri.hasQuery)
      throw const FormatException('副本操作来源不符');
  }

  @override
  Future<CollectionCopyPending?> read(String origin, String owner) {
    _context(origin, owner);
    return _serial(_key(origin, owner), () => _read(origin, owner));
  }

  @override
  Future<void> write(String origin, String owner, CollectionCopyPending p) {
    _context(origin, owner);
    return _serial(_key(origin, owner), () async {
      if (!p.valid) throw const FormatException('副本操作本机记录不符');
      final old = await _read(origin, owner);
      if (old != null &&
          (!old.selector.same(p.selector) ||
              old.stage == 'UNKNOWN' && p.stage == 'PREPARED'))
        throw const FormatException('另一份原副本操作尚未核实');
      await _storage.write(
        key: _key(origin, owner),
        value: jsonEncode({
          'origin': origin,
          'owner': owner,
          'pending': p.toJson(),
        }),
      );
    });
  }

  @override
  Future<void> remove(String origin, String owner, CollectionCopyPending p) {
    _context(origin, owner);
    return _serial(_key(origin, owner), () async {
      final old = await _read(origin, owner);
      if (old != null && !old.same(p))
        throw const FormatException('本机原副本记录已变化');
      await _storage.delete(key: _key(origin, owner));
    });
  }
}

class MemoryCollectionCopyPendingStore implements CollectionCopyPendingStore {
  final values = <(String, String), CollectionCopyPending>{};
  @override
  Future<CollectionCopyPending?> read(String origin, String owner) async =>
      values[(origin, owner)];
  @override
  Future<void> write(
    String origin,
    String owner,
    CollectionCopyPending p,
  ) async {
    if (!p.valid) throw const FormatException('副本操作本机记录不符');
    final old = values[(origin, owner)];
    if (old != null &&
        (!old.selector.same(p.selector) ||
            old.stage == 'UNKNOWN' && p.stage == 'PREPARED'))
      throw const FormatException('另一份原副本操作尚未核实');
    values[(origin, owner)] = p;
  }

  @override
  Future<void> remove(
    String origin,
    String owner,
    CollectionCopyPending p,
  ) async {
    final old = values[(origin, owner)];
    if (old != null && !old.same(p)) throw const FormatException('本机原副本记录已变化');
    values.remove((origin, owner));
  }
}
