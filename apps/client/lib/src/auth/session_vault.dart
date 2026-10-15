import 'dart:async';
import 'dart:convert';

import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// Only an explicit HTTP(S) API origin scopes an authentication credential.
/// Empty development configuration has no credential namespace.
String canonicalAuthOrigin(String apiBase) {
  if (apiBase.isEmpty) return '';
  final uri = Uri.tryParse(apiBase);
  if (uri == null ||
      !const {'http', 'https'}.contains(uri.scheme) ||
      !uri.hasAuthority ||
      uri.host.isEmpty ||
      uri.userInfo.isNotEmpty ||
      uri.hasQuery ||
      uri.hasFragment ||
      (uri.path.isNotEmpty && uri.path != '/')) {
    throw const FormatException('登录服务来源无效。');
  }
  return uri.origin;
}

bool canonicalStoredAuthOrigin(String origin) {
  try {
    return origin.isNotEmpty && canonicalAuthOrigin(origin) == origin;
  } on FormatException {
    return false;
  }
}

// Shared per-key serialization is retired before a caller completes. A prior
// test/event-loop Future is never retained as a permanent tail.
final _authStoreTails = <String, Future<void>>{};
Future<T> withAuthStorageKey<T>(String key, Future<T> Function() action) async {
  final previous = _authStoreTails[key], done = Completer<void>();
  _authStoreTails[key] = done.future;
  try {
    if (previous != null) await previous;
    return await action();
  } finally {
    if (identical(_authStoreTails[key], done.future)) {
      _authStoreTails.remove(key);
    }
    done.complete();
  }
}

class StoredSession {
  const StoredSession({
    required this.token,
    required this.method,
    this.origin = '',
  });

  final String token, method, origin;
  bool belongsTo(String expectedOrigin) =>
      canonicalStoredAuthOrigin(origin) &&
      origin == expectedOrigin &&
      token.isNotEmpty &&
      utf8.encode(token).length <= 4096 &&
      const {'dev_phone', 'dev_email', 'oidc'}.contains(method);
}

abstract interface class SessionVault {
  Future<StoredSession?> read();
  Future<void> write(StoredSession session);
  Future<void> delete();
}

class SecureSessionVault implements SessionVault {
  const SecureSessionVault({
    this.origin = '',
    this.storage = const FlutterSecureStorage(),
  });

  final String origin;
  final FlutterSecureStorage storage;
  static String keyForOrigin(String origin) {
    final canonical = canonicalAuthOrigin(origin);
    if (canonical.isEmpty) {
      throw const FormatException('缺少登录服务来源。');
    }
    return 'birdtie.session.v2.' +
        sha256.convert(utf8.encode(canonical)).toString();
  }

  @override
  Future<StoredSession?> read() async {
    final expected = canonicalAuthOrigin(origin);
    if (expected.isEmpty) return null;
    final key = keyForOrigin(expected);
    return withAuthStorageKey(key, () async {
      final raw = await storage.read(key: key);
      if (raw == null) return null;
      try {
        final data = jsonDecode(raw);
        if (data is Map<String, dynamic> &&
            data.length == 4 &&
            data.keys.toSet().containsAll({
              'schema',
              'origin',
              'token',
              'method',
            }) &&
            data['schema'] == 'birdtie-session-v2' &&
            data['origin'] == expected &&
            data['token'] is String &&
            (data['token'] as String).isNotEmpty &&
            utf8.encode(data['token']).length <= 4096 &&
            const {'dev_phone', 'dev_email', 'oidc'}.contains(data['method'])) {
          return StoredSession(
            token: data['token'],
            method: data['method'],
            origin: expected,
          );
        }
      } catch (_) {
        // A damaged entry grants no credential. Retire only this origin key.
      }
      await storage.delete(key: key);
      return null;
    });
  }

  @override
  Future<void> write(StoredSession session) async {
    final expected = canonicalAuthOrigin(origin);
    if (!session.belongsTo(expected) ||
        session.token.isEmpty ||
        utf8.encode(session.token).length > 4096 ||
        !const {'dev_phone', 'dev_email', 'oidc'}.contains(session.method)) {
      throw const FormatException('登录记录来源不符。');
    }
    final key = keyForOrigin(expected);
    return withAuthStorageKey(
      key,
      () => storage.write(
        key: key,
        value: jsonEncode({
          'schema': 'birdtie-session-v2',
          'origin': expected,
          'token': session.token,
          'method': session.method,
        }),
      ),
    );
  }

  @override
  Future<void> delete() async {
    final expected = canonicalAuthOrigin(origin);
    if (expected.isEmpty) return;
    final key = keyForOrigin(expected);
    await withAuthStorageKey(key, () => storage.delete(key: key));
  }
}

class MemorySessionVault implements SessionVault {
  StoredSession? session;

  @override
  Future<StoredSession?> read() async => session;

  @override
  Future<void> write(StoredSession value) async => session = value;

  @override
  Future<void> delete() async => session = null;
}
