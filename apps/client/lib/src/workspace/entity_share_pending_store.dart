import 'dart:convert';
import 'dart:async';
import 'dart:math';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

const chatEntityTypes = {
  'activity',
  'place',
  'person',
  'community',
  'organization',
  'business',
  'moment',
};
final chatEntityUUID = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
String entitySharePayloadDigest(
  String op,
  String recipientType,
  String recipientID,
  String type,
  String entityID,
) => sha256
    .convert(
      utf8.encode(
        jsonEncode([
          'human-entity-share-v1',
          op,
          recipientType,
          recipientID,
          type,
          entityID,
        ]),
      ),
    )
    .toString();
String newEntityShareOperationID() {
  final random = Random.secure();
  final bytes = List.generate(16, (_) => random.nextInt(256));
  bytes[6] = (bytes[6] & 15) | 64;
  bytes[8] = (bytes[8] & 63) | 128;
  final value = bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
  return '${value.substring(0, 8)}-${value.substring(8, 12)}-${value.substring(12, 16)}-${value.substring(16, 20)}-${value.substring(20)}';
}

/// Recovery references only. Neither an approval nor evidence of a sent message.
class PendingEntityShare {
  const PendingEntityShare({
    required this.operationID,
    required String conversationID,
    required this.type,
    required this.entityID,
  }) : recipientType = 'CONVERSATION',
       recipientID = conversationID,
       sourceVersion = '',
       payloadDigest = '',
       stage = 'UNKNOWN',
       legacy = true;
  const PendingEntityShare.control({
    required this.operationID,
    required this.recipientType,
    required this.recipientID,
    required this.type,
    required this.entityID,
    required this.sourceVersion,
    required this.payloadDigest,
    required this.stage,
  }) : legacy = false;
  final String operationID,
      recipientType,
      recipientID,
      type,
      entityID,
      sourceVersion,
      payloadDigest,
      stage;
  final bool legacy;
  String get conversationID =>
      recipientType == 'CONVERSATION' ? recipientID : '';
  bool sameSelector(PendingEntityShare other) =>
      operationID == other.operationID &&
      recipientType == other.recipientType &&
      recipientID == other.recipientID &&
      type == other.type &&
      entityID == other.entityID &&
      legacy == other.legacy &&
      sourceVersion == other.sourceVersion &&
      payloadDigest == other.payloadDigest;
  PendingEntityShare get unknown => PendingEntityShare.control(
    operationID: operationID,
    recipientType: recipientType,
    recipientID: recipientID,
    type: type,
    entityID: entityID,
    sourceVersion: sourceVersion,
    payloadDigest: payloadDigest,
    stage: 'UNKNOWN',
  );
  Map<String, dynamic> toJson() => legacy
      ? {
          'operationId': operationID,
          'conversationId': conversationID,
          'type': type,
          'entityId': entityID,
        }
      : {
          'version': 2,
          'operationId': operationID,
          'recipientType': recipientType,
          'recipientId': recipientID,
          'type': type,
          'entityId': entityID,
          'sourceVersion': sourceVersion,
          'payloadDigest': payloadDigest,
          'stage': stage,
        };
  factory PendingEntityShare.fromJson(Map<String, dynamic> d) {
    if (d.containsKey('version')) {
      const keys = {
        'version',
        'operationId',
        'recipientType',
        'recipientId',
        'type',
        'entityId',
        'sourceVersion',
        'payloadDigest',
        'stage',
      };
      if (d.length != keys.length ||
          d.keys.any((k) => !keys.contains(k)) ||
          d['version'] != 2 ||
          !const {'CONVERSATION', 'FRIEND_TIE'}.contains(d['recipientType']) ||
          !const {'PREPARED', 'UNKNOWN'}.contains(d['stage']) ||
          !chatEntityTypes.contains(d['type']) ||
          [
            'operationId',
            'recipientId',
            'entityId',
          ].any((k) => d[k] is! String || !chatEntityUUID.hasMatch(d[k])) ||
          ['sourceVersion', 'payloadDigest'].any(
            (k) => d[k] is! String || !RegExp(r'^[0-9a-f]{64}$').hasMatch(d[k]),
          ))
        throw const FormatException('分享恢复记录不符');
      if (d['payloadDigest'] !=
          entitySharePayloadDigest(
            d['operationId'],
            d['recipientType'],
            d['recipientId'],
            d['type'],
            d['entityId'],
          )) {
        throw const FormatException('分享恢复记录摘要不符');
      }
      return PendingEntityShare.control(
        operationID: d['operationId'],
        recipientType: d['recipientType'],
        recipientID: d['recipientId'],
        type: d['type'],
        entityID: d['entityId'],
        sourceVersion: d['sourceVersion'],
        payloadDigest: d['payloadDigest'],
        stage: d['stage'],
      );
    }
    const keys = {'operationId', 'conversationId', 'type', 'entityId'};
    if (d.length != keys.length ||
        d.keys.toSet().difference(keys).isNotEmpty ||
        !chatEntityTypes.contains(d['type']) ||
        [
          'operationId',
          'conversationId',
          'entityId',
        ].any((k) => d[k] is! String || !chatEntityUUID.hasMatch(d[k]))) {
      throw const FormatException('分享恢复记录不符');
    }
    return PendingEntityShare(
      operationID: d['operationId'],
      conversationID: d['conversationId'],
      type: d['type'],
      entityID: d['entityId'],
    );
  }
}

abstract interface class EntitySharePendingStore {
  Future<List<PendingEntityShare>> read(String environment, String ownerID);
  Future<void> write(
    String environment,
    String ownerID,
    PendingEntityShare value,
  );
  Future<void> delete(String environment, String ownerID, String operationID);
  Future<void> remove(
    String environment,
    String ownerID,
    PendingEntityShare value,
  );
}

void _checkTransition(PendingEntityShare? old, PendingEntityShare next) {
  PendingEntityShare.fromJson(next.toJson());
  if (old != null && !old.sameSelector(next))
    throw StateError('原操作不能换成另一张卡片或收件人');
  if (!next.legacy && next.stage == 'UNKNOWN' && old == null)
    throw StateError('原本机准备记录已退休，不能提交');
  if (old != null && old.stage == 'UNKNOWN' && next.stage != 'UNKNOWN')
    throw StateError('未知操作不能重新准备');
}

class SecureEntitySharePendingStore implements EntitySharePendingStore {
  const SecureEntitySharePendingStore({
    this.storage = const FlutterSecureStorage(),
  });
  final FlutterSecureStorage storage;
  static final _tails = <String, Future<void>>{};
  Future<T> _locked<T>(String key, Future<T> Function() action) async {
    final prior = _tails[key];
    final done = Completer<void>();
    _tails[key] = done.future;
    try {
      if (prior != null) await prior;
      return await action();
    } finally {
      if (identical(_tails[key], done.future)) _tails.remove(key);
      done.complete();
    }
  }

  String _prefix(String environment, String ownerID) {
    if (environment.isEmpty || !chatEntityUUID.hasMatch(ownerID)) {
      throw const FormatException('分享归属不符');
    }
    return 'birdtie.entity-share.pending.v1.${sha256.convert(utf8.encode('$environment\n$ownerID'))}.';
  }

  @override
  Future<List<PendingEntityShare>> read(
    String environment,
    String ownerID,
  ) => _locked(_prefix(environment, ownerID), () async {
    final prefix = _prefix(environment, ownerID),
        values = await storage.readAll();
    final result = <PendingEntityShare>[];
    for (final entry in values.entries.where((e) => e.key.startsWith(prefix))) {
      // Corruption blocks new sends: silently dropping an unknown operation could duplicate it.
      final value = PendingEntityShare.fromJson(
        jsonDecode(entry.value) as Map<String, dynamic>,
      );
      if (entry.key != '$prefix${value.operationID}') {
        throw const FormatException('分享恢复记录不符');
      }
      result.add(value);
    }
    return result;
  });

  @override
  Future<void> write(
    String environment,
    String ownerID,
    PendingEntityShare value,
  ) => _locked(_prefix(environment, ownerID), () async {
    final key = '${_prefix(environment, ownerID)}${value.operationID}';
    final old = await storage.read(key: key), json = jsonEncode(value.toJson());
    _checkTransition(
      old == null ? null : PendingEntityShare.fromJson(jsonDecode(old)),
      value,
    );
    await storage.write(key: key, value: json);
  });

  @override
  Future<void> delete(String environment, String ownerID, String operationID) {
    if (!chatEntityUUID.hasMatch(operationID)) {
      throw const FormatException('操作标识不符');
    }
    return _locked(_prefix(environment, ownerID), () async {
      final key = '${_prefix(environment, ownerID)}$operationID',
          raw = await storage.read(key: key);
      if (raw != null && !PendingEntityShare.fromJson(jsonDecode(raw)).legacy)
        throw StateError('必须按完整原操作清理');
      await storage.delete(key: key);
    });
  }

  @override
  Future<void> remove(
    String environment,
    String ownerID,
    PendingEntityShare value,
  ) => _locked(_prefix(environment, ownerID), () async {
    final key = '${_prefix(environment, ownerID)}${value.operationID}',
        raw = await storage.read(key: key);
    if (raw == null) return;
    final old = PendingEntityShare.fromJson(jsonDecode(raw));
    if (!old.sameSelector(value) || old.stage != value.stage)
      throw StateError('本机原操作已变化');
    await storage.delete(key: key);
  });
}

class MemoryEntitySharePendingStore implements EntitySharePendingStore {
  final Map<(String, String, String), PendingEntityShare> values = {};
  @override
  Future<List<PendingEntityShare>> read(
    String environment,
    String ownerID,
  ) async => values.entries
      .where((e) => e.key.$1 == environment && e.key.$2 == ownerID)
      .map((e) => e.value)
      .toList();
  @override
  Future<void> write(
    String environment,
    String ownerID,
    PendingEntityShare value,
  ) async {
    final key = (environment, ownerID, value.operationID), old = values[key];
    _checkTransition(old, value);
    values[key] = value;
  }

  @override
  Future<void> delete(
    String environment,
    String ownerID,
    String operationID,
  ) async {
    final key = (environment, ownerID, operationID);
    if (values[key]?.legacy == false) throw StateError('必须按完整原操作清理');
    values.remove(key);
  }

  @override
  Future<void> remove(
    String environment,
    String ownerID,
    PendingEntityShare value,
  ) async {
    final key = (environment, ownerID, value.operationID), old = values[key];
    if (old == null) return;
    if (!old.sameSelector(value) || old.stage != value.stage)
      throw StateError('本机原操作已变化');
    values.remove(key);
  }
}
