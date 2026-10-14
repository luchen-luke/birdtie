import 'dart:convert';

import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

import 'session_vault.dart';

final _pkceVerifier = RegExp(r'^[A-Za-z0-9._~-]{43,128}$');
bool _pkcePair(String verifier, String challenge) =>
    _pkceVerifier.hasMatch(verifier) &&
    base64UrlEncode(
          sha256.convert(ascii.encode(verifier)).bytes,
        ).replaceAll('=', '') ==
        challenge;

class PendingOidcLogin {
  const PendingOidcLogin({
    required this.verifier,
    required this.challenge,
    required this.createdAt,
    this.origin = '',
    this.channel = 'NATIVE',
  });

  final String verifier, challenge, origin, channel;
  final DateTime createdAt;

  bool validFor(
    String expectedOrigin,
    String expectedChannel, {
    DateTime? now,
  }) {
    final at = now ?? DateTime.now();
    return canonicalStoredAuthOrigin(origin) &&
        origin == expectedOrigin &&
        channel == expectedChannel &&
        const {'NATIVE', 'WEB'}.contains(channel) &&
        !createdAt.isAfter(at) &&
        at.difference(createdAt) < const Duration(minutes: 5) &&
        _pkcePair(verifier, challenge);
  }

  Map<String, Object> toJson() => {
    'schema': 'birdtie-oidc-pending-v2',
    'origin': origin,
    'channel': channel,
    'verifier': verifier,
    'challenge': challenge,
    'createdAt': createdAt.toUtc().toIso8601String(),
  };

  static PendingOidcLogin? decode(
    Object? raw,
    String expectedOrigin,
    String expectedChannel, {
    DateTime? now,
  }) {
    try {
      if (raw is! Map<String, dynamic> ||
          raw.length != 6 ||
          !raw.keys.toSet().containsAll({
            'schema',
            'origin',
            'channel',
            'verifier',
            'challenge',
            'createdAt',
          }) ||
          raw['schema'] != 'birdtie-oidc-pending-v2' ||
          raw['origin'] is! String ||
          raw['channel'] is! String ||
          raw['verifier'] is! String ||
          raw['challenge'] is! String ||
          raw['createdAt'] is! String ||
          !RegExp(r'(Z|[+-]\d\d:\d\d)$').hasMatch(raw['createdAt'])) {
        return null;
      }
      final pending = PendingOidcLogin(
        verifier: raw['verifier'],
        challenge: raw['challenge'],
        origin: raw['origin'],
        channel: raw['channel'],
        createdAt: DateTime.parse(raw['createdAt']).toUtc(),
      );
      return pending.validFor(expectedOrigin, expectedChannel, now: now)
          ? pending
          : null;
    } catch (_) {
      return null;
    }
  }
}

/// The unchanged browser two-String API carries this closed origin selector;
/// the native stub never needs a wider API. No verifier is inside the selector.
String oidcBrowserSelector(String origin, String challenge) {
  final canonical = canonicalAuthOrigin(origin);
  if (challenge.length != 43) {
    throw const FormatException('登录回调来源无效。');
  }
  final bytes = base64Url.decode(base64Url.normalize(challenge));
  if (canonical.isEmpty ||
      bytes.length != 32 ||
      base64UrlEncode(bytes).replaceAll('=', '') != challenge) {
    throw const FormatException('登录回调来源无效。');
  }
  return base64UrlEncode(
    utf8.encode(jsonEncode(['birdtie-oidc-browser-v2', canonical, challenge])),
  ).replaceAll('=', '');
}

({String origin, String challenge}) decodeOidcBrowserSelector(String selector) {
  try {
    if (selector.isEmpty || selector.length > 4096) {
      throw const FormatException('登录回调来源无效。');
    }
    final bytes = base64Url.decode(base64Url.normalize(selector));
    if (base64UrlEncode(bytes).replaceAll('=', '') != selector) {
      throw const FormatException('登录回调来源无效。');
    }
    final fields = jsonDecode(utf8.decode(bytes));
    if (fields is! List ||
        fields.length != 3 ||
        fields[0] != 'birdtie-oidc-browser-v2' ||
        fields[1] is! String ||
        fields[2] is! String ||
        !canonicalStoredAuthOrigin(fields[1]) ||
        oidcBrowserSelector(fields[1], fields[2]) != selector) {
      throw const FormatException('登录回调来源无效。');
    }
    return (origin: fields[1] as String, challenge: fields[2] as String);
  } catch (_) {
    throw const FormatException('登录回调来源无效。');
  }
}

abstract interface class OidcPendingStore {
  Future<PendingOidcLogin?> read();
  Future<void> write(PendingOidcLogin pending);
  Future<void> delete();
}

class SecureOidcPendingStore implements OidcPendingStore {
  const SecureOidcPendingStore({
    this.origin = '',
    this.storage = const FlutterSecureStorage(),
  });
  final String origin;
  final FlutterSecureStorage storage;

  static String keyForOrigin(String origin) {
    final canonical = canonicalAuthOrigin(origin);
    if (canonical.isEmpty) throw const FormatException('缺少登录服务来源。');
    return 'birdtie.oidc.pending.v2.' +
        sha256.convert(utf8.encode(canonical)).toString();
  }

  @override
  Future<PendingOidcLogin?> read() async {
    final expected = canonicalAuthOrigin(origin);
    if (expected.isEmpty) return null;
    final key = keyForOrigin(expected);
    return withAuthStorageKey(key, () async {
      final raw = await storage.read(key: key);
      if (raw == null) return null;
      PendingOidcLogin? value;
      try {
        value = PendingOidcLogin.decode(jsonDecode(raw), expected, 'NATIVE');
      } catch (_) {
        // An invalid entry grants no callback, including the old v1 key.
      }
      if (value == null) await storage.delete(key: key);
      return value;
    });
  }

  @override
  Future<void> write(PendingOidcLogin pending) async {
    final expected = canonicalAuthOrigin(origin);
    if (!pending.validFor(expected, 'NATIVE')) {
      throw const FormatException('登录回调记录来源不符。');
    }
    final key = keyForOrigin(expected);
    await withAuthStorageKey(
      key,
      () => storage.write(key: key, value: jsonEncode(pending.toJson())),
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

class MemoryOidcPendingStore implements OidcPendingStore {
  PendingOidcLogin? pending;
  @override
  Future<PendingOidcLogin?> read() async => pending;
  @override
  Future<void> write(PendingOidcLogin value) async => pending = value;
  @override
  Future<void> delete() async => pending = null;
}
