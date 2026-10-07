import 'connection_request_decision_operation.dart';
import 'dart:async';
import 'dart:convert';

import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

import 'entity_share_pending_store.dart' show chatEntityUUID;

/// referenceID is local; v2 operationID correlates the original service outcome.
/// Neither is approval, current domain permission or a replay authorization.
class PendingConnectionReview {
  const PendingConnectionReview({
    required this.ownerID,
    required this.requestID,
    required this.referenceID,
    required this.action,
    required this.scope,
    required this.direction,
    required this.observedAt,
    required this.requestCreatedAt,
    required this.requestExpiresAt,
    this.operationID,
    this.requestDigest,
  });
  final String ownerID, requestID, referenceID, action, scope, direction;
  final String? operationID, requestDigest;
  bool get hasServiceOperation => operationID != null;
  final DateTime observedAt, requestCreatedAt, requestExpiresAt;

  Map<String, dynamic> toJson() => {
    if (hasServiceOperation) ...{'schemaVersion': 'birdtie.connection-review.pending.v2','operationId':operationID,'requestDigest':requestDigest},
    'ownerId': ownerID,
    'requestId': requestID,
    'referenceId': referenceID,
    'action': action,
    'scope': scope,
    'direction': direction,
    'observedAt': observedAt.toUtc().toIso8601String(),
    'requestCreatedAt': requestCreatedAt.toUtc().toIso8601String(),
    'requestExpiresAt': requestExpiresAt.toUtc().toIso8601String(),
  };

  factory PendingConnectionReview.fromJson(Map<String, dynamic> value) {
    final v2 = value.containsKey('schemaVersion');
    final keys = {
      if (v2) ...{'schemaVersion','operationId','requestDigest'},
      'ownerId',
      'requestId',
      'referenceId',
      'action',
      'scope',
      'direction',
      'observedAt',
      'requestCreatedAt',
      'requestExpiresAt',
    };
    DateTime stamp(String key) {
      final raw = value[key];
      if (raw is! String || raw.length > 40 || !RegExp(r'Z$').hasMatch(raw)) {
        throw const FormatException('申请恢复引用格式不符');
      }
      final parsed = DateTime.tryParse(raw);
      if (parsed == null ||
          parsed.year < 2000 ||
          parsed.year > 2200 ||
          parsed.toUtc().toIso8601String() != raw) {
        throw const FormatException('申请恢复引用时间不符');
      }
      return parsed.toUtc();
    }

    if (value.length != keys.length ||
        value.keys.any((k) => !keys.contains(k)) ||
        ['ownerId', 'requestId', 'referenceId'].any(
          (k) => value[k] is! String || !chatEntityUUID.hasMatch(value[k]),
        ) ||
        !const {'friend', 'conversation'}.contains(value['scope']) ||
        !const {'incoming', 'outgoing'}.contains(value['direction']) ||
        !(value['direction'] == 'incoming' &&
                const {'accept', 'decline'}.contains(value['action']) ||
            value['direction'] == 'outgoing' &&
                value['action'] == 'withdraw')) {
      throw const FormatException('申请恢复引用格式不符');
    }
    if (v2 && (value['schemaVersion']!='birdtie.connection-review.pending.v2' ||
        value['operationId'] is! String || !chatEntityUUID.hasMatch(value['operationId']) ||
        value['requestDigest'] != connectionDecisionDigest(value['ownerId'],value['requestId'],value['action']))) {
      throw const FormatException('服务恢复引用不符');
    }
    final observed = stamp('observedAt'),
        created = stamp('requestCreatedAt'),
        expires = stamp('requestExpiresAt');
    if (!expires.isAfter(created) || !expires.isAfter(observed)) {
      throw const FormatException('申请恢复引用期限不符');
    }
    return PendingConnectionReview(
      operationID: v2 ? value['operationId'] : null,
      requestDigest: v2 ? value['requestDigest'] : null,
      ownerID: value['ownerId'],
      requestID: value['requestId'],
      referenceID: value['referenceId'],
      action: value['action'],
      scope: value['scope'],
      direction: value['direction'],
      observedAt: observed,
      requestCreatedAt: created,
      requestExpiresAt: expires,
    );
  }
}

abstract interface class ConnectionReviewPendingStore {
  Future<PendingConnectionReview?> read(
    String environment,
    String owner,
    String request,
  );
  Future<void> write(String environment, PendingConnectionReview value);
  Future<bool> compareDelete(String environment, PendingConnectionReview value);
}

String _key(String environment, String owner, String request) {
  final uri = Uri.tryParse(environment);
  if (uri == null ||
      !const {'https', 'http'}.contains(uri.scheme) ||
      uri.host.isEmpty ||
      uri.userInfo.isNotEmpty ||
      uri.hasQuery ||
      uri.hasFragment ||
      !chatEntityUUID.hasMatch(owner) ||
      !chatEntityUUID.hasMatch(request)) {
    throw const FormatException('申请恢复引用归属不符');
  }
  final normalized = uri
      .replace(path: uri.path.replaceFirst(RegExp(r'/+$'), ''))
      .toString();
  return 'birdtie.connection-review.pending.v1.${sha256.convert(utf8.encode('$normalized\n$owner\n$request'))}';
}

String _encode(PendingConnectionReview value) {
  final checked = PendingConnectionReview.fromJson(value.toJson());
  return jsonEncode(checked.toJson());
}

PendingConnectionReview? _decode(String? raw, String owner, String request) {
  if (raw == null) return null;
  if (raw.length > 2048) throw const FormatException('申请恢复引用过大');
  final parsed = jsonDecode(raw);
  if (parsed is! Map<String, dynamic>) {
    throw const FormatException('申请恢复引用格式不符');
  }
  final result = PendingConnectionReview.fromJson(parsed);
  if (result.ownerID != owner || result.requestID != request) {
    throw const FormatException('申请恢复引用归属不符');
  }
  return result;
}

/// Serializes same-key operations in this isolate. Platform cross-process CAS
/// is not provided by SecureStorage and requires separate native validation.
class SecureConnectionReviewPendingStore
    implements ConnectionReviewPendingStore {
  const SecureConnectionReviewPendingStore({
    this.storage = const FlutterSecureStorage(),
  });
  final FlutterSecureStorage storage;
  static final _tails = <String, Future<void>>{};
  Future<T> _serial<T>(String key, Future<T> Function() body) async {
    final previous = _tails[key], gate = Completer<void>();
    _tails[key] = gate.future;
    try {
      if (previous != null) await previous;
      return await body();
    } finally {
      if (identical(_tails[key], gate.future)) _tails.remove(key);
      gate.complete();
    }
  }

  @override
  Future<PendingConnectionReview?> read(
    String environment,
    String owner,
    String request,
  ) {
    final key = _key(environment, owner, request);
    return _serial(
      key,
      () async => _decode(await storage.read(key: key), owner, request),
    );
  }

  @override
  Future<void> write(String environment, PendingConnectionReview value) {
    final key = _key(environment, value.ownerID, value.requestID),
        raw = _encode(value);
    return _serial(key, () async {
      final old = await storage.read(key: key);
      if (old != null) {
        final parsed = _decode(old, value.ownerID, value.requestID)!;
        if (_encode(parsed) != raw) throw StateError('此申请还有原操作待核实');
      }
      await storage.write(key: key, value: raw);
    });
  }

  @override
  Future<bool> compareDelete(
    String environment,
    PendingConnectionReview value,
  ) {
    final key = _key(environment, value.ownerID, value.requestID),
        raw = _encode(value);
    return _serial(key, () async {
      final old = await storage.read(key: key);
      if (old == null) return false;
      if (_encode(_decode(old, value.ownerID, value.requestID)!) != raw) {
        return false;
      }
      await storage.delete(key: key);
      return true;
    });
  }
}

/// Explicit unit fixture; not native secure-storage evidence.
class MemoryConnectionReviewPendingStore
    implements ConnectionReviewPendingStore {
  final values = <String, String>{};
  @override
  Future<PendingConnectionReview?> read(
    String environment,
    String owner,
    String request,
  ) async => _decode(values[_key(environment, owner, request)], owner, request);
  @override
  Future<void> write(String environment, PendingConnectionReview value) async {
    final key = _key(environment, value.ownerID, value.requestID),
        raw = _encode(value);
    final old = values[key];
    if (old != null &&
        _encode(_decode(old, value.ownerID, value.requestID)!) != raw) {
      throw StateError('此申请还有原操作待核实');
    }
    values[key] = raw;
  }

  @override
  Future<bool> compareDelete(
    String environment,
    PendingConnectionReview value,
  ) async {
    final key = _key(environment, value.ownerID, value.requestID),
        raw = _encode(value);
    final old = values[key];
    if (old == null ||
        _encode(_decode(old, value.ownerID, value.requestID)!) != raw) {
      return false;
    }
    values.remove(key);
    return true;
  }
}
