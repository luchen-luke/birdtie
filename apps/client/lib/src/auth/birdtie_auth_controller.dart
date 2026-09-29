import 'dart:convert';
import 'dart:math';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import 'browser_auth.dart' as browser;

class BirdtieAuthController extends ChangeNotifier {
  BirdtieAuthController() : _client = http.Client();

  static const _apiBase = String.fromEnvironment('BIRDTIE_API_BASE_URL');
  final http.Client _client;
  String? _accessToken;
  String? _displayName;
  String? _error;
  bool _busy = false;
  bool _closed = false;
  bool _providerConfigured = false;
  bool _configurationChecked = false;

  bool get available =>
      browser.browserAuthAvailable &&
      _apiBase.isNotEmpty &&
      _providerConfigured;
  bool get configurationChecked => _configurationChecked;
  bool get signedIn => _accessToken != null;
  bool get busy => _busy;
  String? get displayName => _displayName;
  String? get error => _error;
  String? get authorizationHeader =>
      _accessToken == null ? null : 'Bearer $_accessToken';

  Uri _endpoint(String path) =>
      Uri.parse('${_apiBase.replaceFirst(RegExp(r'/$'), '')}$path');

  void _notify() {
    if (!_closed) notifyListeners();
  }

  Future<void> initialize() async {
    await resumeCallback();
    if (!browser.browserAuthAvailable || _apiBase.isEmpty) {
      _configurationChecked = true;
      _notify();
      return;
    }
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
    } finally {
      _configurationChecked = true;
      _notify();
    }
  }

  void signIn() {
    if (!available || _busy) return;
    try {
      final random = Random.secure();
      final raw = List<int>.generate(32, (_) => random.nextInt(256));
      final verifier = base64UrlEncode(raw).replaceAll('=', '');
      final challenge = base64UrlEncode(
        sha256.convert(ascii.encode(verifier)).bytes,
      ).replaceAll('=', '');
      browser.storeVerifier(challenge, verifier);
      _error = null;
      _notify();
      browser.navigateTo(
        _endpoint(
          '/v1/auth/oidc/start',
        ).replace(queryParameters: {'challenge': challenge}).toString(),
      );
    } catch (_) {
      _error = '无法启动登录，请检查浏览器设置。';
      _notify();
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
      final me = await _client
          .get(_endpoint('/v1/me'), headers: {'Authorization': 'Bearer $token'})
          .timeout(const Duration(seconds: 10));
      if (me.statusCode != 200) {
        throw const FormatException('account unavailable');
      }
      final account =
          (jsonDecode(me.body) as Map<String, dynamic>)['data']
              as Map<String, dynamic>;
      final accountID = account['id'];
      if (accountID is! String) {
        throw const FormatException('account missing');
      }
      String label = 'Birdtie account';
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
      _accessToken = token;
      _displayName = label;
    } catch (_) {
      if (issuedToken != null) {
        try {
          await _client
              .post(
                _endpoint('/v1/session/logout'),
                headers: {'Authorization': 'Bearer $issuedToken'},
              )
              .timeout(const Duration(seconds: 5));
        } catch (_) {
          // The credential is discarded in memory even if server revocation fails.
        }
      }
      _accessToken = null;
      _displayName = null;
      _error = '登录未完成，请稍后重试。';
    } finally {
      _busy = false;
      _notify();
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
      _accessToken = null;
      _displayName = null;
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
    _client.close();
    super.dispose();
  }
}
