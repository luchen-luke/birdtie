import 'dart:async';

import 'package:app_links/app_links.dart';
import 'package:url_launcher/url_launcher.dart';

import 'native_platform.dart';

/// The registered native callback is deliberately exact. PKCE prevents an
/// intercepted custom-scheme code from becoming a Birdtie session.
const nativeOidcCallback = 'birdtie-auth://callback';

abstract interface class NativeOidcGateway {
  Stream<Uri> get links;
  Future<bool> open(Uri authorizationUrl);
}

class PlatformNativeOidcGateway implements NativeOidcGateway {
  PlatformNativeOidcGateway() : _links = AppLinks();

  final AppLinks _links;

  @override
  Stream<Uri> get links => _links.uriLinkStream;

  @override
  Future<bool> open(Uri authorizationUrl) =>
      launchUrl(authorizationUrl, mode: LaunchMode.externalApplication);
}

bool get nativeOidcAvailable => isMobilePlatform;

bool isNativeOidcCallback(Uri uri) =>
    uri.scheme == 'birdtie-auth' &&
    uri.host == 'callback' &&
    uri.path.isEmpty &&
    uri.userInfo.isEmpty &&
    !uri.hasFragment;
