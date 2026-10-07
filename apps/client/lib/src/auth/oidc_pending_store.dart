import 'dart:convert';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';

class PendingOidcLogin {
  const PendingOidcLogin({
    required this.verifier,
    required this.challenge,
    required this.createdAt,
  });

  final String verifier;
  final String challenge;
  final DateTime createdAt;
}

abstract interface class OidcPendingStore {
  Future<PendingOidcLogin?> read();
  Future<void> write(PendingOidcLogin pending);
  Future<void> delete();
}

class SecureOidcPendingStore implements OidcPendingStore {
  const SecureOidcPendingStore({this.storage = const FlutterSecureStorage()});

  static const key = 'birdtie.oidc.pending.v1';
  final FlutterSecureStorage storage;

  @override
  Future<PendingOidcLogin?> read() async {
    final raw = await storage.read(key: key);
    if (raw == null) return null;
    try {
      final data = jsonDecode(raw) as Map<String, dynamic>;
      final createdAt = DateTime.parse(data['createdAt'] as String);
      final verifier = data['verifier'] as String;
      final challenge = data['challenge'] as String;
      if (DateTime.now().difference(createdAt).inMinutes < 5 &&
          !createdAt.isAfter(DateTime.now()) &&
          verifier.isNotEmpty &&
          challenge.isNotEmpty) {
        return PendingOidcLogin(
          verifier: verifier,
          challenge: challenge,
          createdAt: createdAt,
        );
      }
    } catch (_) {
      // A corrupt or expired pending login is never accepted.
    }
    await delete();
    return null;
  }

  @override
  Future<void> write(PendingOidcLogin pending) => storage.write(
    key: key,
    value: jsonEncode({
      'verifier': pending.verifier,
      'challenge': pending.challenge,
      'createdAt': pending.createdAt.toIso8601String(),
    }),
  );

  @override
  Future<void> delete() => storage.delete(key: key);
}

class MemoryOidcPendingStore implements OidcPendingStore {
  PendingOidcLogin? pending;

  @override
  Future<PendingOidcLogin?> read() async => pending;

  @override
  Future<void> write(PendingOidcLogin value) async => pending = value;

  @override
  Future<void> delete() async => pending = null;
}
