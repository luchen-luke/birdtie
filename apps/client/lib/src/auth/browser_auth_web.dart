import 'package:web/web.dart' as web;

bool get browserAuthAvailable => true;

String _key(String challenge) => 'birdtie_oidc_verifier_$challenge';

void storeVerifier(String challenge, String verifier) {
  web.window.sessionStorage.setItem(_key(challenge), verifier);
}

String? takeVerifier(String challenge) {
  final key = _key(challenge);
  final verifier = web.window.sessionStorage.getItem(key);
  web.window.sessionStorage.removeItem(key);
  return verifier;
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
