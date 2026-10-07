import 'package:flutter/foundation.dart';

/// Build-time public configuration shared by every client data and map source.
/// Secret credentials are never part of Dart defines.
class BirdtieEnvironment {
  const BirdtieEnvironment._();

  static const name = String.fromEnvironment(
    'BIRDTIE_ENVIRONMENT',
    defaultValue: kReleaseMode ? '' : 'development',
  );
  static const apiBaseUrl = String.fromEnvironment('BIRDTIE_API_BASE_URL');
  static const mapboxWebPublicToken = String.fromEnvironment(
    'BIRDTIE_MAPBOX_PUBLIC_TOKEN',
  );
  static const mapboxMobilePublicToken = String.fromEnvironment(
    'BIRDTIE_MAPBOX_MOBILE_PUBLIC_TOKEN',
  );
  static const amapWebPublicKey = String.fromEnvironment(
    'BIRDTIE_AMAP_WEB_PUBLIC_KEY',
  );
  static const amapServiceHost = String.fromEnvironment(
    'BIRDTIE_AMAP_SERVICE_HOST',
  );

  // A direct release build without the checked release wrapper must fail during
  // Dart compilation. Runtime validation below also checks the actual values.
  static const _releaseGate = _ReleaseGate(
    kReleaseMode,
    bool.fromEnvironment('BIRDTIE_RELEASE_CONFIG_VALID'),
  );

  static void validate() {
    if (_releaseGate.release && !_releaseGate.checked) {
      throw StateError('正式版本配置未通过构建检查。');
    }
    final error = validationError(
      environment: name,
      apiBase: apiBaseUrl,
      release: kReleaseMode,
      web: kIsWeb,
      mapboxWebToken: mapboxWebPublicToken,
      mapboxMobileToken: mapboxMobilePublicToken,
      amapWebKey: amapWebPublicKey,
      amapHost: amapServiceHost,
    );
    if (error != null) throw StateError(error);
  }

  @visibleForTesting
  static String? validationError({
    required String environment,
    required String apiBase,
    required bool release,
    required bool web,
    required String mapboxWebToken,
    required String mapboxMobileToken,
    String amapWebKey = '',
    String amapHost = '',
  }) {
    if (!const ['development', 'staging', 'production'].contains(environment)) {
      return '必须明确指定 Birdtie 的运行环境。';
    }
    if (release && environment == 'development') {
      return '正式构建不能使用本地开发环境。';
    }
    if (apiBase.isEmpty) {
      return release || environment != 'development'
          ? '缺少 Birdtie API 地址。'
          : null;
    }
    final api = Uri.tryParse(apiBase);
    if (api == null ||
        !api.hasAuthority ||
        api.host.isEmpty ||
        api.userInfo.isNotEmpty ||
        api.hasQuery ||
        api.hasFragment) {
      return 'Birdtie API 地址无效。';
    }
    if (api.scheme != 'http' && api.scheme != 'https') {
      return 'Birdtie API 地址必须使用 HTTP 或 HTTPS。';
    }
    if (release || environment != 'development') {
      if (api.scheme != 'https' || _isLoopback(api.host)) {
        return '正式环境必须使用非本机 HTTPS API。';
      }
      final selectedToken = web ? mapboxWebToken : mapboxMobileToken;
      if (!selectedToken.startsWith('pk.') || selectedToken.length < 20) {
        return '正式环境缺少对应平台的 Mapbox 公共令牌。';
      }
      if (web && (amapWebKey.isNotEmpty || amapHost.isNotEmpty)) {
        final host = Uri.tryParse(amapHost);
        if (amapWebKey.isEmpty ||
            host == null ||
            host.scheme != 'https' ||
            _isLoopback(host.host) ||
            host.host.isEmpty) {
          return '高德地图 Web 配置不完整或未使用远程 HTTPS。';
        }
      }
    }
    return null;
  }

  static bool _isLoopback(String host) {
    final lower = host.toLowerCase();
    if (lower == 'localhost' ||
        lower.endsWith('.localhost') ||
        lower == '::1' ||
        lower == '0.0.0.0') {
      return true;
    }
    final address = Uri.tryParse('http://$host');
    final normalized = address?.host ?? lower;
    return normalized.startsWith('127.') || normalized == '10.0.2.2';
  }
}

class _ReleaseGate {
  const _ReleaseGate(this.release, this.checked)
    : assert(
        !release || checked,
        'Use tool/build_release.ps1 for release builds.',
      );

  final bool release;
  final bool checked;
}
