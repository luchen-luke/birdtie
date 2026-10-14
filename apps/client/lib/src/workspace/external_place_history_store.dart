import 'dart:convert';

import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// Device-local anonymous CITY conversation snapshots, isolated by API origin.
/// A saved row never supplies native entity membership or account authority.
abstract interface class ExternalPlaceHistoryStore {
  Future<List<Map<String, dynamic>>> read(String environment);
  Future<void> write(
    String environment,
    Map<String, dynamic> task, {
    required bool Function() current,
  });
}

class SecureExternalPlaceHistoryStore implements ExternalPlaceHistoryStore {
  SecureExternalPlaceHistoryStore({
    this.storage = const FlutterSecureStorage(),
  });
  final FlutterSecureStorage storage;
  static const maxBytes = 8 * 1024 * 1024;
  static Future<void> _tail = Future.value();

  Future<T> _exclusive<T>(Future<T> Function() action) {
    final next = _tail.then((_) => action());
    _tail = next.then<void>((_) {}, onError: (Object _, StackTrace _) {});
    return next;
  }

  String _key(String environment) {
    final uri = Uri.tryParse(environment);
    if (uri == null ||
        !uri.hasAuthority ||
        !const {'http', 'https'}.contains(uri.scheme) ||
        uri.userInfo.isNotEmpty ||
        uri.hasQuery ||
        uri.hasFragment) {
      throw const FormatException('本机对话环境无法核验');
    }
    return 'birdtie.public-external-history.v2.'
        '${sha256.convert(utf8.encode(environment))}';
  }

  Future<List<Map<String, dynamic>>> _read(String environment) async {
    final raw = await storage.read(key: _key(environment));
    if (raw == null) return [];
    if (utf8.encode(raw).length > maxBytes) {
      throw const FormatException('本机对话记录过大');
    }
    final decoded = jsonDecode(raw);
    if (decoded is! Map<String, dynamic> ||
        decoded['schema'] != 'birdtie-public-history-v2' ||
        decoded['tasks'] is! List) {
      throw const FormatException('本机对话记录无法读取');
    }
    return [
      for (final item in decoded['tasks'] as List)
        if (item is Map<String, dynamic>)
          item
        else
          throw const FormatException('本机对话无法读取'),
    ];
  }

  @override
  Future<List<Map<String, dynamic>>> read(String environment) =>
      _exclusive(() => _read(environment));

  @override
  Future<void> write(
    String environment,
    Map<String, dynamic> task, {
    required bool Function() current,
  }) => _exclusive(() async {
    if (!current()) return;
    final rows = await _read(environment);
    if (!current()) return;
    rows.removeWhere((row) => row['id'] == task['id']);
    rows.insert(0, task);
    String encode() =>
        jsonEncode({'schema': 'birdtie-public-history-v2', 'tasks': rows});
    var encoded = encode();
    if (utf8.encode(encoded).length > maxBytes) {
      throw const FormatException('本机对话存储已满，旧对话已保留，本次暂时无法保存');
    }
    if (current()) await storage.write(key: _key(environment), value: encoded);
  });
}

class MemoryExternalPlaceHistoryStore implements ExternalPlaceHistoryStore {
  final _rows = <String, List<Map<String, dynamic>>>{};
  @override
  Future<List<Map<String, dynamic>>> read(String environment) async =>
      (jsonDecode(jsonEncode(_rows[environment] ?? [])) as List)
          .cast<Map<String, dynamic>>();
  @override
  Future<void> write(
    String environment,
    Map<String, dynamic> task, {
    required bool Function() current,
  }) async {
    if (!current()) return;
    final rows = _rows.putIfAbsent(environment, () => []);
    rows.removeWhere((row) => row['id'] == task['id']);
    rows.insert(0, jsonDecode(jsonEncode(task)) as Map<String, dynamic>);
  }
}
