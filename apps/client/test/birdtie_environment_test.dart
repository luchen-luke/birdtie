import 'package:birdtie_client/src/config/birdtie_environment.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  String? check({
    String environment = 'production',
    String apiBase = 'https://api.birdtie.example',
    bool release = true,
    bool web = false,
    String webToken = 'pk.public-test-token-for-web',
    String mobileToken = 'pk.public-test-token-for-mobile',
    String amapWebKey = '',
    String amapHost = '',
  }) => BirdtieEnvironment.validationError(
    environment: environment,
    apiBase: apiBase,
    release: release,
    web: web,
    mapboxWebToken: webToken,
    mapboxMobileToken: mobileToken,
    amapWebKey: amapWebKey,
    amapHost: amapHost,
  );

  test('debug development may be intentionally offline', () {
    expect(
      check(environment: 'development', apiBase: '', release: false),
      isNull,
    );
    expect(
      check(
        environment: 'development',
        apiBase: 'http://127.0.0.1:3694',
        release: false,
      ),
      isNull,
    );
  });

  test('release rejects missing environment, API and local development', () {
    expect(check(environment: ''), isNotNull);
    expect(check(apiBase: ''), isNotNull);
    expect(check(environment: 'development'), isNotNull);
  });

  test('release rejects HTTP and loopback hosts', () {
    for (final url in [
      'http://api.birdtie.example',
      'http://127.0.0.1:3694',
      'https://localhost:3694',
      'https://sub.localhost',
      'https://127.0.0.1',
      'https://10.0.2.2',
      'https://user:pass@api.birdtie.example',
      'https://api.birdtie.example?test=1',
    ]) {
      expect(check(apiBase: url), isNotNull, reason: url);
    }
  });

  test('release requires a platform public map token', () {
    expect(check(mobileToken: ''), isNotNull);
    expect(check(web: true, webToken: ''), isNotNull);
    expect(check(), isNull);
    expect(check(web: true), isNull);
  });

  test('GL JS preview selects only approved public platform input', () {
    String select(String environment, String web, String mobile) =>
        BirdtieEnvironment.selectMapboxGlWebToken(
          environment: environment,
          webToken: web,
          mobileToken: mobile,
        );
    expect(select('development', '', 'pk.preview-public'), 'pk.preview-public');
    expect(
      select('development', 'pk.web-public', 'pk.preview-public'),
      'pk.web-public',
    );
    expect(
      select('development', 'sk.invalid', 'pk.preview-public'),
      'sk.invalid',
    );
    expect(select('development', '', 'sk.invalid'), isEmpty);
    for (final environment in ['staging', 'production', '']) {
      expect(select(environment, '', 'pk.preview-public'), isEmpty);
      expect(
        select(environment, 'pk.web-public', 'pk.preview-public'),
        'pk.web-public',
      );
    }
    expect(
      check(web: true, webToken: '', mobileToken: 'pk.preview-public'),
      isNotNull,
    );
  });

  test('staging also rejects insecure API and incomplete AMap Web setup', () {
    expect(
      check(environment: 'staging', apiBase: 'http://api.birdtie.example'),
      isNotNull,
    );
    expect(check(web: true, amapWebKey: 'public-key'), isNotNull);
    expect(
      check(
        web: true,
        amapWebKey: 'public-key',
        amapHost: 'https://maps.birdtie.example/_AMapService',
      ),
      isNull,
    );
  });
}
