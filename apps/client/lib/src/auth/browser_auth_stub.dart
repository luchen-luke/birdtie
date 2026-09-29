bool get browserAuthAvailable => false;

void storeVerifier(String challenge, String verifier) =>
    throw UnsupportedError('Browser OIDC is unavailable');

String? takeVerifier(String challenge) => null;

void navigateTo(String target) =>
    throw UnsupportedError('Browser OIDC is unavailable');

void clearAuthCallback() {}
