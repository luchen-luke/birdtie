import 'package:flutter/foundation.dart';

import 'map_privacy_consent.dart';

/// Shared, versioned SDK privacy choice. Route choice and OS location consent
/// never set this value. The first actual service use loads its durable record.
abstract final class AMapSessionConsent {
  static MapPrivacyConsentController _controller =
      MapPrivacyConsentController();
  static MapPrivacyConsentController get controller => _controller;
  static bool get accepted => _controller.accepted;
  static bool get hasDecision => _controller.hasDecision;

  @visibleForTesting
  static MapPrivacyConsentController replaceControllerForTesting(
    MapPrivacyConsentController controller,
  ) {
    if (!kDebugMode) throw StateError('Test-only consent injection');
    final previous = _controller;
    _controller = controller;
    return previous;
  }
}
