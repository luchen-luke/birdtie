import 'dart:convert';
import 'dart:math';
import 'dart:async';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import 'browser_auth.dart' as browser;
import 'native_oidc.dart';
import 'oidc_pending_store.dart';
import 'session_vault.dart';
import '../config/birdtie_environment.dart';

class _SessionRejected implements Exception {
  const _SessionRejected();
}

class BirdtieAuthController extends ChangeNotifier {
  BirdtieAuthController({
    http.Client? client,
    String? apiBaseUrl,
    SessionVault? sessionVault,
    OidcPendingStore? oidcPendingStore,
    NativeOidcGateway? nativeGateway,
  }) : _client = client ?? http.Client(),
       _apiBase = apiBaseUrl ?? apiBase,
       _sessionVault = sessionVault ?? const SecureSessionVault(),
       _oidcPendingStore = oidcPendingStore ?? const SecureOidcPendingStore(),
       _nativeGateway =
           nativeGateway ??
           (nativeOidcAvailable ? PlatformNativeOidcGateway() : null);

  static const apiBase = BirdtieEnvironment.apiBaseUrl;
  final http.Client _client;
  final String _apiBase;
  final SessionVault _sessionVault;
  final OidcPendingStore _oidcPendingStore;
  final NativeOidcGateway? _nativeGateway;
  StreamSubscription<Uri>? _nativeLinks;
  String? _accessToken;
  String? _displayName;
  String? _accountID;
  String? _error;
  bool _busy = false;
  bool _closed = false;
  bool _providerConfigured = false;
  bool _devPhoneConfigured = false;
  bool _configurationChecked = false;
  bool _nativeCallbackHandling = false;
  String? _loginMethod;

  bool get available =>
      (browser.browserAuthAvailable || _nativeGateway != null) &&
      _apiBase.isNotEmpty &&
      _providerConfigured;
  bool get devPhoneAvailable =>
      !kReleaseMode && _apiBase.isNotEmpty && _devPhoneConfigured;
  bool get configurationChecked => _configurationChecked;
  bool get signedIn => _accessToken != null;
  bool get busy => _busy;
  String? get displayName => _displayName;
  String? get accountID => _accountID;
  String? get loginMethod => _loginMethod;
  String? get error => _error;
  String? get authorizationHeader =>
      _accessToken == null ? null : 'Bearer $_accessToken';

  void updateProfileDisplayName(String name) {
    if (!signedIn) return;
    _displayName = name;
    _notify();
  }

  Uri _endpoint(String path) =>
      Uri.parse('${_apiBase.replaceFirst(RegExp(r'/$'), '')}$path');

  void _notify() {
    if (!_closed) notifyListeners();
  }

  Future<void> initialize() async {
    if (_nativeGateway != null) {
      _nativeLinks ??= _nativeGateway.links.listen((uri) {
        unawaited(resumeNativeCallback(uri));
      });
    }
    await resumeCallback();
    if (_accessToken == null && _apiBase.isNotEmpty) {
      try {
        final saved = await _sessionVault.read();
        if (saved != null) {
          try {
            await _acceptSession(
              saved.token,
              method: saved.method,
              persist: false,
            );
          } on _SessionRejected {
            await _sessionVault.delete();
          } catch (_) {
            // Keep the saved credential for a later start if the API is offline.
          }
        }
      } catch (_) {
        _error = '无法读取本机登录会话，请检查设备存储。';
      }
    }
    if (_apiBase.isEmpty) {
      _configurationChecked = true;
      _notify();
      return;
    }
    try {
      final dev = await _client
          .get(_endpoint('/v1/auth/dev-phone/status'))
          .timeout(const Duration(seconds: 10));
      if (dev.statusCode == 200) {
        final data =
            (jsonDecode(dev.body) as Map<String, dynamic>)['data']
                as Map<String, dynamic>;
        _devPhoneConfigured = data['enabled'] == true;
      }
    } catch (_) {
      _devPhoneConfigured = false;
    }
    if (browser.browserAuthAvailable || _nativeGateway != null) {
      try {
        final response = await _client
            .get(_endpoint('/v1/auth/oidc/status'))
            .timeout(const Duration(seconds: 10));
        if (response.statusCode == 200) {
          final payload = jsonDecode(response.body) as Map<String, dynamic>;
          final data = payload['data'] as Map<String, dynamic>;
          _providerConfigured = data['configured'] == true;
        }
      } catch (_) {
        _providerConfigured = false;
      }
    }
    _configurationChecked = true;
    _notify();
  }

  Future<bool> requestDevPhoneCode(String phone) async {
    if (!devPhoneAvailable || _busy || signedIn) return false;
    _busy = true;
    _error = null;
    _notify();
    try {
      final response = await _client
          .post(
            _endpoint('/v1/auth/dev-phone/code'),
            headers: {'Content-Type': 'application/json'},
            body: jsonEncode({'phone': phone.trim()}),
          )
          .timeout(const Duration(seconds: 10));
      if (response.statusCode == 429) {
        _error = '请求过于频繁，请稍后再试。';
        return false;
      }
      if (response.statusCode != 200) {
        _error = '请输入有效手机号。';
        return false;
      }
      return true;
    } catch (_) {
      _error = '无法获取测试验证码，请检查本地 API。';
      return false;
    } finally {
      _busy = false;
      _notify();
    }
  }

  Future<bool> verifyDevPhoneCode(String phone, String code) async {
    if (!devPhoneAvailable || _busy || signedIn) return false;
    _busy = true;
    _error = null;
    _notify();
    String? issuedToken;
    try {
      final response = await _client
          .post(
            _endpoint('/v1/auth/dev-phone/verify'),
            headers: {'Content-Type': 'application/json'},
            body: jsonEncode({'phone': phone.trim(), 'code': code.trim()}),
          )
          .timeout(const Duration(seconds: 10));
      if (response.statusCode == 429) {
        _error = '尝试次数过多，请稍后重新获取测试验证码。';
        return false;
      }
      if (response.statusCode != 200) {
        _error = '测试验证码无效、已过期或尚未获取。';
        return false;
      }
      final token =
          (jsonDecode(response.body)
              as Map<String, dynamic>)['data']['accessToken'];
      if (token is! String || token.isEmpty) {
        throw const FormatException('session missing');
      }
      issuedToken = token;
      await _acceptSession(token, method: 'dev_phone');
      return true;
    } catch (_) {
      if (issuedToken != null) await _revokeIssuedToken(issuedToken);
      _accessToken = null;
      _displayName = null;
      _accountID = null;
      _loginMethod = null;
      _error = '登录未完成，请稍后重试。';
      return false;
    } finally {
      _busy = false;
      _notify();
    }
  }

  Future<void> signIn() async {
    if (!available || _busy || signedIn) return;
    try {
      final random = Random.secure();
      final raw = List<int>.generate(32, (_) => random.nextInt(256));
      final verifier = base64UrlEncode(raw).replaceAll('=', '');
      final challenge = base64UrlEncode(
        sha256.convert(ascii.encode(verifier)).bytes,
      ).replaceAll('=', '');
      if (_nativeGateway == null) {
        browser.storeVerifier(challenge, verifier);
      } else {
        await _oidcPendingStore.write(
          PendingOidcLogin(
            verifier: verifier,
            challenge: challenge,
            createdAt: DateTime.now(),
          ),
        );
      }
      _error = null;
      _notify();
      final target = _endpoint(
        '/v1/auth/oidc/start',
      ).replace(queryParameters: {'challenge': challenge});
      if (_nativeGateway == null) {
        browser.navigateTo(target.toString());
      } else if (!await _nativeGateway.open(target)) {
        await _oidcPendingStore.delete();
        throw StateError('authorization browser unavailable');
      }
    } catch (_) {
      _error = '无法启动登录，请检查浏览器设置。';
      _notify();
    }
  }

  Future<void> resumeNativeCallback(Uri callback) async {
    if (_nativeGateway == null ||
        !isNativeOidcCallback(callback) ||
        _busy ||
        _nativeCallbackHandling ||
        signedIn) {
      return;
    }
    _nativeCallbackHandling = true;
    try {
      final pending = await _oidcPendingStore.read();
      if (pending == null) {
        _error = '登录会话已失效，请重新尝试。';
        _notify();
        return;
      }
      // Consume before network exchange so duplicate deep links cannot race.
      await _oidcPendingStore.delete();
      final code = callback.queryParameters['code'];
      final challenge = callback.queryParameters['challenge'];
      final expected = base64UrlEncode(
        sha256.convert(ascii.encode(pending.verifier)).bytes,
      ).replaceAll('=', '');
      if (callback.queryParameters['error'] != null ||
          code == null ||
          challenge != pending.challenge ||
          expected != pending.challenge) {
        _error = '登录校验失败，请重新尝试。';
        _notify();
        return;
      }
      await _exchangeOidc(code, pending.verifier);
    } catch (_) {
      _error = '无法读取登录会话，请稍后重试。';
      _notify();
    } finally {
      _nativeCallbackHandling = false;
    }
  }

  Future<void> resumeCallback() async {
    if (!browser.browserAuthAvailable) return;
    final code = Uri.base.queryParameters['code'];
    final challenge = Uri.base.queryParameters['challenge'];
    final providerError = Uri.base.queryParameters['error'];
    if (code == null && challenge == null && providerError == null) return;
    try {
      browser.clearAuthCallback();
    } catch (_) {
      _error = '无法清理登录回调，请检查浏览器设置。';
      _notify();
      return;
    }
    if (_apiBase.isEmpty || code == null || challenge == null) {
      _error = '登录未完成，请重新尝试。';
      _notify();
      return;
    }
    String? verifier;
    try {
      verifier = browser.takeVerifier(challenge);
    } catch (_) {
      _error = '无法读取登录会话，请检查浏览器设置。';
      _notify();
      return;
    }
    if (verifier == null) {
      _error = '登录会话已失效，请重新尝试。';
      _notify();
      return;
    }
    final expectedChallenge = base64UrlEncode(
      sha256.convert(ascii.encode(verifier)).bytes,
    ).replaceAll('=', '');
    if (expectedChallenge != challenge) {
      _error = '登录校验失败，请重新尝试。';
      _notify();
      return;
    }
    await _exchangeOidc(code, verifier);
  }

  Future<void> _exchangeOidc(String code, String verifier) async {
    _busy = true;
    _error = null;
    _notify();
    String? issuedToken;
    try {
      final exchange = await _client
          .post(
            _endpoint('/v1/auth/oidc/exchange'),
            headers: {'Content-Type': 'application/json'},
            body: jsonEncode({'code': code, 'verifier': verifier}),
          )
          .timeout(const Duration(seconds: 10));
      if (exchange.statusCode != 200) {
        throw const FormatException('exchange denied');
      }
      final payload = jsonDecode(exchange.body) as Map<String, dynamic>;
      final token = (payload['data'] as Map<String, dynamic>)['accessToken'];
      if (token is! String || token.isEmpty) {
        throw const FormatException('session missing');
      }
      issuedToken = token;
      await _acceptSession(token, method: 'oidc');
    } catch (_) {
      if (issuedToken != null) {
        await _revokeIssuedToken(issuedToken);
      }
      _accessToken = null;
      _displayName = null;
      _accountID = null;
      _loginMethod = null;
      _error = '登录未完成，请稍后重试。';
    } finally {
      _busy = false;
      _notify();
    }
  }

  Future<void> _acceptSession(
    String token, {
    required String method,
    bool persist = true,
  }) async {
    final me = await _client
        .get(_endpoint('/v1/me'), headers: {'Authorization': 'Bearer $token'})
        .timeout(const Duration(seconds: 10));
    if (me.statusCode == 401) throw const _SessionRejected();
    if (me.statusCode != 200) {
      throw const FormatException('account unavailable');
    }
    final account =
        (jsonDecode(me.body) as Map<String, dynamic>)['data']
            as Map<String, dynamic>;
    final accountID = account['id'];
    if (accountID is! String) throw const FormatException('account missing');
    String label = 'Birdtie 账号';
    try {
      final profile = await _client
          .get(
            _endpoint('/v1/accounts/$accountID/profile'),
            headers: {'Authorization': 'Bearer $token'},
          )
          .timeout(const Duration(seconds: 10));
      if (profile.statusCode == 200) {
        final details =
            (jsonDecode(profile.body) as Map<String, dynamic>)['data']
                as Map<String, dynamic>;
        final name = details['displayName'];
        if (name is String && name.isNotEmpty) label = name;
      }
    } catch (_) {
      // Identity was verified by /v1/me; profile display is optional.
    }
    if (persist) {
      await _sessionVault.write(StoredSession(token: token, method: method));
    }
    _accessToken = token;
    _accountID = accountID;
    _displayName = label;
    _loginMethod = method;
  }

  Future<void> _revokeIssuedToken(String token) async {
    try {
      await _client
          .post(
            _endpoint('/v1/session/logout'),
            headers: {'Authorization': 'Bearer $token'},
          )
          .timeout(const Duration(seconds: 5));
    } catch (_) {
      // The credential is discarded in memory even if revocation fails.
    }
  }

  Future<void> signOut() async {
    final token = _accessToken;
    if (token == null || _busy) return;
    _busy = true;
    _error = null;
    _notify();
    try {
      final response = await _client
          .post(
            _endpoint('/v1/session/logout'),
            headers: {'Authorization': 'Bearer $token'},
          )
          .timeout(const Duration(seconds: 10));
      if (response.statusCode != 204 && response.statusCode != 401) {
        throw const FormatException('logout unavailable');
      }
      await _sessionVault.delete();
      _accessToken = null;
      _displayName = null;
      _accountID = null;
      _loginMethod = null;
    } catch (_) {
      _error = '退出登录未完成，请重试。';
    } finally {
      _busy = false;
      _notify();
    }
  }

  @override
  void dispose() {
    _closed = true;
    unawaited(_nativeLinks?.cancel());
    _client.close();
    super.dispose();
  }
}
