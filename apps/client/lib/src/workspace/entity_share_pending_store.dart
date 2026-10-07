import 'dart:convert';
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
    required this.conversationID,
    required this.type,
    required this.entityID,
  });
  final String operationID, conversationID, type, entityID;
  Map<String, dynamic> toJson() => {
    'operationId': operationID,
    'conversationId': conversationID,
    'type': type,
    'entityId': entityID,
  };
  factory PendingEntityShare.fromJson(Map<String, dynamic> d) {
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
}

class SecureEntitySharePendingStore implements EntitySharePendingStore {
  const SecureEntitySharePendingStore({
    this.storage = const FlutterSecureStorage(),
  });
  final FlutterSecureStorage storage;
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
  ) async {
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
  }

  @override
  Future<void> write(
    String environment,
    String ownerID,
    PendingEntityShare value,
  ) async {
    PendingEntityShare.fromJson(value.toJson());
    final key = '${_prefix(environment, ownerID)}${value.operationID}';
    final old = await storage.read(key: key), json = jsonEncode(value.toJson());
    if (old != null && old != json) throw StateError('原操作不能换成另一张卡片');
    await storage.write(key: key, value: json);
  }

  @override
  Future<void> delete(String environment, String ownerID, String operationID) {
    if (!chatEntityUUID.hasMatch(operationID)) {
      throw const FormatException('操作标识不符');
    }
    return storage.delete(key: '${_prefix(environment, ownerID)}$operationID');
  }
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
    if (old != null && jsonEncode(old.toJson()) != jsonEncode(value.toJson())) {
      throw StateError('原操作不能换成另一张卡片');
    }
    values[key] = value;
  }

  @override
  Future<void> delete(
    String environment,
    String ownerID,
    String operationID,
  ) async {
    values.remove((environment, ownerID, operationID));
  }
}
