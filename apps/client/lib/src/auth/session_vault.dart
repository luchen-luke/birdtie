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
    this.refreshToken,
    this.ownerID,
    this.pendingRefreshOperationID,
  });

  final String token, method, origin;
  final String? refreshToken, ownerID, pendingRefreshOperationID;
  bool get hasRefresh => refreshToken != null;
  bool belongsTo(String expectedOrigin) =>
      canonicalStoredAuthOrigin(origin) &&
      origin == expectedOrigin &&
      token.isNotEmpty &&
      utf8.encode(token).length <= 4096 &&
      const {'dev_phone', 'dev_email', 'oidc'}.contains(method) &&
      (refreshToken == null
          ? pendingRefreshOperationID == null && ownerID == null
          : method == 'dev_phone' &&
                RegExp(r'^btr1_[A-Za-z0-9_-]{43}$').hasMatch(refreshToken!) &&
                _sessionUUID(ownerID) &&
                (pendingRefreshOperationID == null ||
                    _sessionUUID(pendingRefreshOperationID)));

  StoredSession withRefreshOperation(String operationID) => StoredSession(
    token: token,
    method: method,
    origin: origin,
    refreshToken: refreshToken,
    ownerID: ownerID,
    pendingRefreshOperationID: operationID,
  );
}

bool _sessionUUID(String? value) =>
    value != null &&
    RegExp(r'^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$').hasMatch(value);

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
            data.length == 7 &&
            data.keys.toSet().containsAll({
              'schema',
              'origin',
              'token',
              'method',
              'refreshToken',
              'ownerID',
              'pendingRefreshOperationID',
            }) &&
            data['schema'] == 'birdtie-session-v3' &&
            data['origin'] == expected &&
            data['token'] is String &&
            data['method'] is String &&
            data['refreshToken'] is String &&
            data['ownerID'] is String &&
            (data['pendingRefreshOperationID'] == null ||
                data['pendingRefreshOperationID'] is String)) {
          final saved = StoredSession(
            token: data['token'],
            method: data['method'],
            origin: expected,
            refreshToken: data['refreshToken'],
            ownerID: data['ownerID'],
            pendingRefreshOperationID: data['pendingRefreshOperationID'],
          );
          if (saved.belongsTo(expected)) return saved;
        }
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
          'schema': session.hasRefresh
              ? 'birdtie-session-v3'
              : 'birdtie-session-v2',
          'origin': expected,
          'token': session.token,
          'method': session.method,
          if (session.hasRefresh) ...{
            'refreshToken': session.refreshToken,
            'ownerID': session.ownerID,
            'pendingRefreshOperationID': session.pendingRefreshOperationID,
          },
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
