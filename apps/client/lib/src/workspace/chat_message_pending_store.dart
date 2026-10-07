import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'chat_message_operation.dart';
import 'entity_share_pending_store.dart' show chatEntityUUID;

abstract interface class HumanMessagePendingStore {
  Future<List<PendingHumanMessage>> read(String environment, String owner);
  Future<void> write(
    String environment,
    String owner,
    PendingHumanMessage pending,
  );
  Future<void> delete(
    String environment,
    String owner,
    PendingHumanMessage pending,
  );
}

class SecureHumanMessagePendingStore implements HumanMessagePendingStore {
  SecureHumanMessagePendingStore({this.storage = const FlutterSecureStorage()});
  final FlutterSecureStorage storage;
  static const maxPending = 16;
  static Future<void> _tail = Future.value();
  Future<T> _exclusive<T>(Future<T> Function() f) {
    final next = _tail.then((_) => f());
    _tail = next.then<void>((_) {}, onError: (Object _, StackTrace _) {});
    return next;
  }

  String _prefix(String environment, String owner) {
    final u = Uri.tryParse(environment);
    if (u == null ||
        !u.hasAuthority ||
        !const {'http', 'https'}.contains(u.scheme) ||
        u.userInfo.isNotEmpty ||
        u.hasQuery ||
        u.hasFragment ||
        !chatEntityUUID.hasMatch(owner)) {
      throw const FormatException('发送记录归属不符');
    }
    return 'birdtie.human-message.pending.v1.${sha256.convert(utf8.encode('$environment\n$owner'))}.';
  }

  Future<List<PendingHumanMessage>> _read(
    String environment,
    String owner,
  ) async {
    final prefix = _prefix(environment, owner), all = await storage.readAll();
    final rows = all.entries.where((e) => e.key.startsWith(prefix)).toList();
    if (rows.length > maxPending) {
      throw const FormatException('待核实记录过多，请先核实原发送。');
    }
    return [for (final e in rows) _decode(e, prefix)];
  }

  PendingHumanMessage _decode(MapEntry<String, String> e, String prefix) {
    if (e.value.length > 1024) { throw const FormatException('发送记录无法读取'); }
    final p = PendingHumanMessage.decode(jsonDecode(e.value));
    if (e.key != '$prefix${p.operationID}') {
      throw const FormatException('发送记录地址不符');
    }
    return p;
  }

  @override
  Future<List<PendingHumanMessage>> read(String e, String o) =>
      _exclusive(() => _read(e, o));
  @override
  Future<void> write(String e, String o, PendingHumanMessage p) =>
      _exclusive(() async {
        PendingHumanMessage.decode(p.toJson());
        final prefix = _prefix(e, o), all = await _read(e, o);
        final old = all.where((v) => v.operationID == p.operationID).toList();
        if (old.isNotEmpty &&
            jsonEncode(old.single.toJson()) != jsonEncode(p.toJson())) {
          throw StateError('同次发送不能更换正文或会话');
        }
        if (old.isEmpty && all.length >= maxPending) {
          throw StateError('请先核实待确认发送');
        }
        await storage.write(
          key: '$prefix${p.operationID}',
          value: jsonEncode(p.toJson()),
        );
      });
  @override
  Future<void> delete(String e, String o, PendingHumanMessage p) =>
      _exclusive(() async {
        PendingHumanMessage.decode(p.toJson());
        final rows = await _read(e, o);
        final old = rows.where((v) => v.operationID == p.operationID).toList();
        if (old.isNotEmpty &&
            jsonEncode(old.single.toJson()) != jsonEncode(p.toJson())) {
          throw StateError('原发送记录已变化，不能清除。');
        }
        await storage.delete(key: '${_prefix(e, o)}${p.operationID}');
      });
}

class MemoryHumanMessagePendingStore implements HumanMessagePendingStore {
  final values = <(String, String, String), PendingHumanMessage>{};
  @override
  Future<List<PendingHumanMessage>> read(String e, String o) async => values
      .entries
      .where((v) => v.key.$1 == e && v.key.$2 == o)
      .map((v) => v.value)
      .toList();
  @override
  Future<void> write(String e, String o, PendingHumanMessage p) async {
    PendingHumanMessage.decode(p.toJson());
    final k = (e, o, p.operationID), old = values[k];
    if (old != null && jsonEncode(old.toJson()) != jsonEncode(p.toJson())) {
      throw StateError('旧发送绑定不能更改');
    }
    values[k] = p;
  }

  @override
  Future<void> delete(String e, String o, PendingHumanMessage p) async {
    final key = (e, o, p.operationID), old = values[key];
    if (old != null && jsonEncode(old.toJson()) != jsonEncode(p.toJson())) {
      throw StateError('原发送记录已变化，不能清除。');
    }
    values.remove(key);
  }
}
