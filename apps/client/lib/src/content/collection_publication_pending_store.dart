import 'dart:convert';
import 'dart:async';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// Only durable recovery selectors, never Session, source content or approval.
class CollectionPublicationPendingStore {
  CollectionPublicationPendingStore({
    required this.origin,
    required this.owner,
    required this.kind,
    required this.id,
    FlutterSecureStorage? storage,
  }) : _storage = storage ?? const FlutterSecureStorage();
  final String origin, owner, kind, id;
  final FlutterSecureStorage _storage;
  static final _tails = <String, Future<void>>{};
  Future<T> _serialized<T>(Future<T> Function() action) async {
    final previous = _tails[key], finished = Completer<void>();
    _tails[key] = finished.future;
    try {
      if (previous != null) await previous;
      return await action();
    } finally {
      finished.complete();
      if (identical(_tails[key], finished.future)) _tails.remove(key);
    }
  }

  String get key =>
      'birdtie.collection-publication.v1.${sha256.convert(utf8.encode(jsonEncode([origin, owner, kind, id])))}';
  Future<Map<String, dynamic>?> load() => _serialized(_load);
  Future<Map<String, dynamic>?> _load() async {
    final raw = await _storage.read(key: key);
    if (raw == null) return null;
    final j = jsonDecode(raw) as Map<String, dynamic>;
    if (j['origin'] != origin ||
        j['owner'] != owner ||
        j['kind'] != kind ||
        j['id'] != id ||
        j.keys.toSet().difference({
          'origin',
          'owner',
          'kind',
          'id',
          'operationId',
          'revision',
          'action',
        }).isNotEmpty ||
        j['revision'] is! int ||
        (j['revision'] as int) < 1 ||
        !const ['PUBLISH', 'UNPUBLISH'].contains(j['action']) ||
        j['operationId'] is! String ||
        !RegExp(
          r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
        ).hasMatch(j['operationId'] as String)) {
      throw const FormatException('invalid collection recovery selectors');
    }
    return Map.unmodifiable(
      {...j}
        ..remove('origin')
        ..remove('owner'),
    );
  }

  Future<void> save(Map<String, dynamic> selectors) => _serialized(() async {
    if (selectors.keys.toSet().difference({
          'kind',
          'id',
          'operationId',
          'revision',
          'action',
        }).isNotEmpty ||
        selectors['kind'] != kind ||
        selectors['id'] != id ||
        selectors['revision'] is! int ||
        (selectors['revision'] as int) < 1 ||
        !const ['PUBLISH', 'UNPUBLISH'].contains(selectors['action']) ||
        selectors['operationId'] is! String ||
        !RegExp(
          r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
        ).hasMatch(selectors['operationId'] as String)) {
      throw const FormatException('invalid collection recovery selectors');
    }
    final old = await _load();
    if (old != null && selectors.entries.any((e) => old[e.key] != e.value)) {
      throw StateError('An original publication operation is still pending');
    }
    await _storage.write(
      key: key,
      value: jsonEncode({
        ...selectors,
        'origin': origin,
        'owner': owner,
        'kind': kind,
        'id': id,
      }),
    );
  });
  Future<void> clear() => _serialized(() => _storage.delete(key: key));
  Future<void> clearIfOperation(String operationID) => _serialized(() async {
    final pending = await _load();
    if (pending?['operationId'] == operationID) await _storage.delete(key: key);
  });
}
