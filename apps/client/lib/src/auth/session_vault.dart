import 'dart:convert';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';

class StoredSession {
  const StoredSession({required this.token, required this.method});

  final String token;
  final String method;
}

abstract interface class SessionVault {
  Future<StoredSession?> read();
  Future<void> write(StoredSession session);
  Future<void> delete();
}

class SecureSessionVault implements SessionVault {
  const SecureSessionVault({
    this._storage = const FlutterSecureStorage(),
  });

  static const _key = 'birdtie.session.v1';
  final FlutterSecureStorage _storage;

  @override
  Future<StoredSession?> read() async {
    final raw = await _storage.read(key: _key);
    if (raw == null) return null;
    try {
      final data = jsonDecode(raw) as Map<String, dynamic>;
      final token = data['token'];
      final method = data['method'];
      if (token is String &&
          token.isNotEmpty &&
          (method == 'dev_phone' || method == 'oidc')) {
        return StoredSession(token: token, method: method as String);
      }
    } on FormatException {
      // A damaged entry must not be used as a credential.
    } on TypeError {
      // A damaged entry must not be used as a credential.
    }
    await delete();
    return null;
  }

  @override
  Future<void> write(StoredSession session) => _storage.write(
    key: _key,
    value: jsonEncode({'token': session.token, 'method': session.method}),
  );

  @override
  Future<void> delete() => _storage.delete(key: _key);
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
