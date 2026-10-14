import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:web/web.dart' as web;
import 'oidc_pending_store.dart';

bool get browserAuthAvailable => true;

String _key(String selector) =>
    'birdtie.oidc.browser.v2.' +
    sha256.convert(utf8.encode(selector)).toString();

void storeVerifier(String selector, String verifier) {
  final binding = decodeOidcBrowserSelector(selector);
  final pending = PendingOidcLogin(
    origin: binding.origin,
    channel: 'WEB',
    challenge: binding.challenge,
    verifier: verifier,
    createdAt: DateTime.now().toUtc(),
  );
  if (!pending.validFor(binding.origin, 'WEB')) {
    throw const FormatException('登录回调记录无法核验。');
  }
  web.window.sessionStorage.setItem(
    _key(selector),
    jsonEncode(pending.toJson()),
  );
}

String? takeVerifier(String selector) {
  final binding = decodeOidcBrowserSelector(selector);
  final key = _key(selector), raw = web.window.sessionStorage.getItem(key);
  if (raw == null) return null;
  // A callback consumes only its exact origin/challenge record. Never inspect
  // the old naked-challenge key or another API's pending browser flow.
  web.window.sessionStorage.removeItem(key);
  try {
    final pending = PendingOidcLogin.decode(
      jsonDecode(raw),
      binding.origin,
      'WEB',
    );
    return pending?.challenge == binding.challenge ? pending?.verifier : null;
  } catch (_) {
    return null;
  }
}

void navigateTo(String target) => web.window.location.assign(target);

void clearAuthCallback() {
  final current = Uri.base;
  final parameters = Map<String, String>.from(current.queryParameters)
    ..remove('code')
    ..remove('challenge')
    ..remove('error');
  final clean = current.replace(
    query: parameters.isEmpty ? '' : null,
    queryParameters: parameters.isEmpty ? null : parameters,
  );
  web.window.history.replaceState(null, '', clean.toString());
}
