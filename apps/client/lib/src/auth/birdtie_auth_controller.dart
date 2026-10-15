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
       _ownsClient = client == null,
       _apiBase = apiBaseUrl ?? apiBase,
       _apiOrigin = canonicalAuthOrigin(apiBaseUrl ?? apiBase),
       _sessionVault =
           sessionVault ??
           SecureSessionVault(
             origin: canonicalAuthOrigin(apiBaseUrl ?? apiBase),
           ),
       _oidcPendingStore =
           oidcPendingStore ??
           SecureOidcPendingStore(
             origin: canonicalAuthOrigin(apiBaseUrl ?? apiBase),
           ),
       _nativeGateway =
           nativeGateway ??
           (nativeOidcAvailable ? PlatformNativeOidcGateway() : null);

  static const apiBase = BirdtieEnvironment.apiBaseUrl;
  final http.Client _client;
  final bool _ownsClient;
  final String _apiBase, _apiOrigin;
  String get apiOrigin => _apiOrigin;
  final SessionVault _sessionVault;
  final OidcPendingStore _oidcPendingStore;
  final NativeOidcGateway? _nativeGateway;
  StreamSubscription<Uri>? _nativeLinks;
  String? _accessToken;
  String? _displayName;
  String? _accountID;
  String? _rejectedSessionOwnerID;
  String? _error;
  bool _busy = false;
  bool _closed = false;
  bool _providerConfigured = false;
  bool _devPhoneConfigured = false;
  bool _devEmailConfigured = false;
  bool _configurationChecked = false;
  bool _sessionRestoring = false;
  Future<void>? _initialization;
  Future<void>? _configurationRead;
  bool _nativeCallbackHandling = false;
  String? _loginMethod;
  int _authAttempt = 0;
  String? _pendingChallenge;
  bool Function()? _pendingEntryCurrent;
  int? _launchAttempt;
  Uri? _queuedNativeCallback;
  // Auth controllers may be replaced while secure storage awaits the platform.
  // Serialize local storage operations across those instances; this is not an
  // identity or a server permission.
  static Future<void>? _storageTail;
  static Future<T> _withStorage<T>(Future<T> Function() action) async {
    while (_storageTail != null) {
      await _storageTail;
    }
    final completed = Completer<void>();
    _storageTail = completed.future;
    try {
      return await action();
    } finally {
      _storageTail = null;
      completed.complete();
    }
  }

  int? _beginAttempt() {
    if (_closed || _busy) return null;
    final attempt = ++_authAttempt;
    _busy = true;
    _error = null;
    _notify();
    return attempt;
  }

  bool _ownsAttempt(int attempt) =>
      !_closed && _busy && attempt == _authAttempt;

  void _finishAttempt(int attempt) {
    if (!_ownsAttempt(attempt)) return;
    _busy = false;
    _notify();
  }

  Future<void> _clearOwnPending(String challenge) => _withStorage(() async {
    final pending = await _oidcPendingStore.read();
    if (pending?.origin == _apiOrigin &&
        pending?.channel == 'NATIVE' &&
        pending?.challenge == challenge) {
      await _oidcPendingStore.delete();
    }
    if (_pendingChallenge == challenge) {
      _pendingChallenge = null;
      _pendingEntryCurrent = null;
    }
  });

  bool get available =>
      (browser.browserAuthAvailable || _nativeGateway != null) &&
      _apiBase.isNotEmpty &&
      _providerConfigured;
  bool get devPhoneAvailable =>
      !kReleaseMode && _apiBase.isNotEmpty && _devPhoneConfigured;
  bool get devEmailAvailable =>
      !kReleaseMode && _apiBase.isNotEmpty && _devEmailConfigured;
  bool get configurationChecked => _configurationChecked;
  bool get configurationChecking => _configurationRead != null;
  // Display state only. It is neither a Session nor permission to read data.
  bool get sessionRestoring => _sessionRestoring;
  bool get signedIn => _accessToken != null;
  bool get busy => _busy;
  String? get displayName => _displayName;
  String? get accountID => _accountID;
  // A rejected credential may retain local drafts for this owner only. This
  // value grants no access and is never used as an authenticated account ID.
  String? get rejectedSessionOwnerID => _rejectedSessionOwnerID;
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

  Future<void> initialize() {
    final existing = _initialization;
    if (existing != null) return existing;
    // Install before notifying: a listener may re-enter initialize immediately.
    final completion = Completer<void>();
    _initialization = completion.future;
    unawaited(
      _initialize().then(
        completion.complete,
        onError: completion.completeError,
      ),
    );
    return completion.future;
  }

  Future<void> _initialize() async {
    if (_closed) return;
    _sessionRestoring = true;
    _notify();
    try {
      if (_nativeGateway != null) {
        _nativeLinks ??= _nativeGateway.links.listen((uri) {
          unawaited(resumeNativeCallback(uri));
        });
      }
      await resumeCallback();
      if (!_closed && _accessToken == null && !_busy && _apiBase.isNotEmpty) {
        final attempt = _beginAttempt()!;
        try {
          final saved = await _withStorage(_sessionVault.read);
          if (_ownsAttempt(attempt) &&
              saved != null &&
              saved.belongsTo(_apiOrigin)) {
            try {
              await _acceptSession(
                saved.token,
                method: saved.method,
                persist: false,
                isCurrent: () => _ownsAttempt(attempt),
              );
            } on _SessionRejected {
              await _withStorage(() async {
                if (!_ownsAttempt(attempt)) return;
                final current = await _sessionVault.read();
                if (_ownsAttempt(attempt) &&
                    current?.belongsTo(_apiOrigin) == true &&
                    current?.token == saved.token) {
                  await _sessionVault.delete();
                }
              });
            } catch (_) {
              // Keep the saved credential for a later start if the API is offline.
            }
          }
        } catch (_) {
          if (_ownsAttempt(attempt)) _error = '无法读取本机登录会话，请检查设备存储。';
        } finally {
          _finishAttempt(attempt);
        }
      }
    } finally {
      _sessionRestoring = false;
      _notify();
    }
    if (_closed) return;
    await refreshLoginConfiguration();
  }

  /// Re-read public login availability after a transient connection failure.
  /// This never reads saved credentials again or requests/verifies a code.
  /// Concurrent gestures share one read; initialize remains a one-time restore.
  Future<void> refreshLoginConfiguration() {
    if (_closed || _sessionRestoring || _busy) return Future<void>.value();
    final existing = _configurationRead;
    if (existing != null) return existing;
    final completion = Completer<void>();
    _configurationRead = completion.future;
    unawaited(
      _readLoginConfiguration().then(
        completion.complete,
        onError: completion.completeError,
      ),
    );
    return completion.future;
  }

  Future<void> _readLoginConfiguration() async {
    _configurationChecked = false;
    _notify();
    try {
      await _loadLoginConfiguration();
    } finally {
      _configurationRead = null;
      if (!_closed) {
        _configurationChecked = true;
        _notify();
      }
    }
  }

  Future<void> _loadLoginConfiguration() async {
    if (_closed) return;
    if (_apiBase.isEmpty) {
      return;
    }
    try {
      final dev = await _client
          .get(_endpoint('/v1/auth/dev-phone/status'))
          .timeout(const Duration(seconds: 10));
      if (_closed) return;
      _devPhoneConfigured = false;
      if (dev.statusCode == 200) {
        final data =
            (jsonDecode(dev.body) as Map<String, dynamic>)['data']
                as Map<String, dynamic>;
        _devPhoneConfigured = data['enabled'] == true;
      }
    } catch (_) {
      if (_closed) return;
      _devPhoneConfigured = false;
    }
    try {
      final response = await _client
          .get(_endpoint('/v1/auth/dev-email/status'))
          .timeout(const Duration(seconds: 10));
      if (_closed) return;
      _devEmailConfigured = false;
      if (response.statusCode == 200) {
        final data =
            (jsonDecode(response.body) as Map<String, dynamic>)['data']
                as Map<String, dynamic>;
        _devEmailConfigured = data['enabled'] == true;
      }
    } catch (_) {
      if (_closed) return;
      _devEmailConfigured = false;
    }
    if (browser.browserAuthAvailable || _nativeGateway != null) {
      try {
        final response = await _client
            .get(_endpoint('/v1/auth/oidc/status'))
            .timeout(const Duration(seconds: 10));
        if (_closed) return;
        _providerConfigured = false;
        if (response.statusCode == 200) {
          final payload = jsonDecode(response.body) as Map<String, dynamic>;
          final data = payload['data'] as Map<String, dynamic>;
          _providerConfigured = data['configured'] == true;
        }
      } catch (_) {
        if (_closed) return;
        _providerConfigured = false;
      }
    }
  }

  Future<bool> requestDevPhoneCode(
    String phone, {
    bool Function()? isCurrent,
  }) async {
    if (!devPhoneAvailable ||
        _busy ||
        signedIn ||
        sessionRestoring ||
        (isCurrent != null && !isCurrent()))
      return false;
    final attempt = _beginAttempt();
    if (attempt == null) return false;
    bool current() =>
        _ownsAttempt(attempt) && (isCurrent == null || isCurrent());
    try {
      if (!current()) return false;
      final response = await _client
          .post(
            _endpoint('/v1/auth/dev-phone/code'),
            headers: {'Content-Type': 'application/json'},
            body: jsonEncode({'phone': phone.trim()}),
          )
          .timeout(const Duration(seconds: 10));
      if (!current()) return false;
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
      if (current()) _error = '无法获取测试验证码，请检查本地 API。';
      return false;
    } finally {
      _finishAttempt(attempt);
    }
  }

  Future<bool> verifyDevPhoneCode(
    String phone,
    String code, {
    bool Function()? isCurrent,
  }) async {
    if (!devPhoneAvailable ||
        _busy ||
        signedIn ||
        sessionRestoring ||
        (isCurrent != null && !isCurrent()))
      return false;
    final attempt = _beginAttempt();
    if (attempt == null) return false;
    bool current() =>
        _ownsAttempt(attempt) && (isCurrent == null || isCurrent());
    String? issuedToken;
    try {
      if (!current()) return false;
      final response = await _client
          .post(
            _endpoint('/v1/auth/dev-phone/verify'),
            headers: {'Content-Type': 'application/json'},
            body: jsonEncode({'phone': phone.trim(), 'code': code.trim()}),
          )
          .timeout(const Duration(seconds: 10));
      if (response.statusCode != 200 && !current()) return false;
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
      if (!current()) throw StateError('login entry retired');
      await _acceptSession(token, method: 'dev_phone', isCurrent: current);
      return true;
    } catch (_) {
      if (issuedToken != null) await _discardIssuedToken(issuedToken);
      if (current()) _error = '登录未完成，请稍后重试。';
      return false;
    } finally {
      _finishAttempt(attempt);
    }
  }

  Future<bool> requestDevEmailCode(
    String email, {
    bool Function()? isCurrent,
  }) async {
    if (!devEmailAvailable ||
        _busy ||
        signedIn ||
        sessionRestoring ||
        (isCurrent != null && !isCurrent()))
      return false;
    final attempt = _beginAttempt();
    if (attempt == null) return false;
    bool current() =>
        _ownsAttempt(attempt) && (isCurrent == null || isCurrent());
    try {
      if (!current()) return false;
      final response = await _client
          .post(
            _endpoint('/v1/auth/dev-email/code'),
            headers: {'Content-Type': 'application/json'},
            body: jsonEncode({'email': email.trim()}),
          )
          .timeout(const Duration(seconds: 10));
      if (!current()) return false;
      if (response.statusCode == 429) {
        _error = '请求过于频繁，请稍后再试。';
        return false;
      }
      if (response.statusCode != 200) {
        _error = '请输入有效邮箱地址。';
        return false;
      }
      return true;
    } catch (_) {
      if (current()) _error = '无法获取测试验证码，请检查登录服务。';
      return false;
    } finally {
      _finishAttempt(attempt);
    }
  }

  Future<bool> verifyDevEmailCode(
    String email,
    String code, {
    bool Function()? isCurrent,
  }) async {
    if (!devEmailAvailable ||
        _busy ||
        signedIn ||
        sessionRestoring ||
        (isCurrent != null && !isCurrent()))
      return false;
    final attempt = _beginAttempt();
    if (attempt == null) return false;
    bool current() =>
        _ownsAttempt(attempt) && (isCurrent == null || isCurrent());
    String? issuedToken;
    try {
      if (!current()) return false;
      final response = await _client
          .post(
            _endpoint('/v1/auth/dev-email/verify'),
            headers: {'Content-Type': 'application/json'},
            body: jsonEncode({'email': email.trim(), 'code': code.trim()}),
          )
          .timeout(const Duration(seconds: 10));
      if (response.statusCode != 200 && !current()) return false;
      if (response.statusCode == 429) {
        _error = '尝试次数过多，请稍后重新获取测试验证码。';
        return false;
      }
      if (response.statusCode != 200) {
        _error = response.statusCode == 403
            ? '该邮箱暂不可接续旧账号，请检查原登录邮箱。'
            : '测试验证码无效、已过期或尚未获取。';
        return false;
      }
      final data =
          (jsonDecode(response.body) as Map<String, dynamic>)['data']
              as Map<String, dynamic>;
      final token = data['accessToken'];
      if (token is! String || token.isEmpty) {
        throw const FormatException('session missing');
      }
      issuedToken = token;
      if (data['credentialKind'] != 'email') {
        throw const FormatException('email session kind missing');
      }
      if (!current()) throw StateError('login entry retired');
      await _acceptSession(token, method: 'dev_email', isCurrent: current);
      return true;
    } catch (_) {
      if (issuedToken != null) await _discardIssuedToken(issuedToken);
      if (current()) _error = '登录未完成，请稍后重试。';
      return false;
    } finally {
      _finishAttempt(attempt);
    }
  }

  Future<void> signIn({bool Function()? isCurrent}) async {
    if (!available ||
        _busy ||
        signedIn ||
        sessionRestoring ||
        (isCurrent != null && !isCurrent()))
      return;
    final attempt = _beginAttempt();
    if (attempt == null) return;
    bool current() =>
        _ownsAttempt(attempt) && (isCurrent == null || isCurrent());
    String? writtenChallenge;
    try {
      if (!current()) return;
      final random = Random.secure();
      final raw = List<int>.generate(32, (_) => random.nextInt(256));
      final verifier = base64UrlEncode(raw).replaceAll('=', '');
      final challenge = base64UrlEncode(
        sha256.convert(ascii.encode(verifier)).bytes,
      ).replaceAll('=', '');
      if (_nativeGateway == null) {
        browser.storeVerifier(
          oidcBrowserSelector(_apiOrigin, challenge),
          verifier,
        );
      } else {
        await _withStorage(() async {
          if (!current()) return;
          await _oidcPendingStore.write(
            PendingOidcLogin(
              origin: _apiOrigin,
              verifier: verifier,
              challenge: challenge,
              createdAt: DateTime.now(),
            ),
          );
          writtenChallenge = challenge;
          _pendingChallenge = challenge;
          _pendingEntryCurrent = isCurrent;
        });
      }
      if (!current()) {
        if (writtenChallenge != null) await _clearOwnPending(writtenChallenge!);
        return;
      }
      _error = null;
      _notify();
      if (!current()) {
        if (writtenChallenge != null) await _clearOwnPending(writtenChallenge!);
        return;
      }
      final target = _endpoint(
        '/v1/auth/oidc/start',
      ).replace(queryParameters: {'challenge': challenge});
      if (_nativeGateway == null) {
        browser.navigateTo(target.toString());
      } else {
        _launchAttempt = attempt;
        final opened = await _nativeGateway.open(target);
        if (!opened || !current()) {
          await _clearOwnPending(challenge);
          if (!opened) throw StateError('authorization browser unavailable');
        }
      }
    } catch (_) {
      if (writtenChallenge != null) await _clearOwnPending(writtenChallenge!);
      if (current()) _error = '无法启动登录，请检查浏览器设置。';
    } finally {
      final queued = _launchAttempt == attempt ? _queuedNativeCallback : null;
      if (_launchAttempt == attempt) {
        _launchAttempt = null;
        _queuedNativeCallback = null;
      }
      _finishAttempt(attempt);
      if (queued != null && !_closed) await resumeNativeCallback(queued);
    }
  }

  Future<void> resumeNativeCallback(Uri callback) async {
    if (!_closed &&
        _busy &&
        _launchAttempt == _authAttempt &&
        isNativeOidcCallback(callback) &&
        !signedIn &&
        callback.queryParameters['challenge'] == _pendingChallenge &&
        (_pendingEntryCurrent == null || _pendingEntryCurrent!())) {
      // One matching launch callback; never queue across Dev/restore attempts.
      _queuedNativeCallback ??= callback;
      return;
    }
    if (_nativeGateway == null ||
        !isNativeOidcCallback(callback) ||
        _busy ||
        _nativeCallbackHandling ||
        signedIn) {
      return;
    }
    final attempt = _beginAttempt();
    if (attempt == null) return;
    _nativeCallbackHandling = true;
    bool entryCurrent() =>
        _pendingEntryCurrent == null || _pendingEntryCurrent!();
    bool current() => _ownsAttempt(attempt) && entryCurrent();
    try {
      final pending = await _withStorage(() async {
        if (!_ownsAttempt(attempt)) return null;
        final saved = await _oidcPendingStore.read();
        if (!_ownsAttempt(attempt)) return null;
        // Origin-less v1 or injected foreign records never become a callback
        // grant and are never deleted as though they belonged to this API.
        if (saved != null && !saved.validFor(_apiOrigin, 'NATIVE')) return null;
        if (saved != null &&
            _pendingChallenge != null &&
            (saved.challenge != _pendingChallenge || !entryCurrent())) {
          if (saved.challenge == _pendingChallenge)
            await _oidcPendingStore.delete();
          return null;
        }
        if (saved != null) {
          final expected = base64UrlEncode(
            sha256.convert(ascii.encode(saved.verifier)).bytes,
          ).replaceAll('=', '');
          if (callback.queryParameters['challenge'] != saved.challenge ||
              expected != saved.challenge)
            return null;
        }
        // Consume under the same local storage serialization as writes.
        if (saved != null) await _oidcPendingStore.delete();
        return saved;
      });
      if (!current()) return;
      if (pending == null) {
        _error = '登录会话已失效，请重新尝试。';
        _notify();
        return;
      }
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
      await _exchangeOidc(
        code,
        pending.verifier,
        ownedAttempt: attempt,
        isCurrent: current,
      );
    } catch (_) {
      if (current()) _error = '无法读取登录会话，请稍后重试。';
    } finally {
      _nativeCallbackHandling = false;
      _finishAttempt(attempt);
    }
  }

  Future<void> resumeCallback() async {
    if (_closed || _busy || signedIn || !browser.browserAuthAvailable) return;
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
      verifier = browser.takeVerifier(
        oidcBrowserSelector(_apiOrigin, challenge),
      );
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

  Future<void> _exchangeOidc(
    String code,
    String verifier, {
    int? ownedAttempt,
    bool Function()? isCurrent,
  }) async {
    final attempt = ownedAttempt ?? _beginAttempt();
    if (attempt == null) return;
    bool current() =>
        _ownsAttempt(attempt) && (isCurrent == null || isCurrent());
    String? issuedToken;
    try {
      if (!current()) return;
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
      if (!current()) throw StateError('login entry retired');
      await _acceptSession(token, method: 'oidc', isCurrent: current);
    } catch (_) {
      if (issuedToken != null) {
        await _discardIssuedToken(issuedToken);
      }
      if (current()) _error = '登录未完成，请稍后重试。';
    } finally {
      if (ownedAttempt == null) _finishAttempt(attempt);
    }
  }

  Future<void> _acceptSession(
    String token, {
    required String method,
    bool persist = true,
    bool Function()? isCurrent,
  }) async {
    bool current() => !_closed && (isCurrent == null || isCurrent());
    if (!current()) throw StateError('login entry retired');
    final me = await _client
        .get(_endpoint('/v1/me'), headers: {'Authorization': 'Bearer $token'})
        .timeout(const Duration(seconds: 10));
    if (!current()) throw StateError('login entry retired');
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
      await _withStorage(() async {
        if (!current()) throw StateError('login entry retired');
        try {
          await _sessionVault.write(
            StoredSession(token: token, method: method, origin: _apiOrigin),
          );
          if (!current()) throw StateError('login entry retired');
        } catch (_) {
          try {
            final saved = await _sessionVault.read();
            if (saved?.belongsTo(_apiOrigin) == true && saved?.token == token) {
              await _sessionVault.delete();
            }
          } catch (_) {
            // A failed platform cleanup is never treated as authentication.
          }
          rethrow;
        }
      });
    }
    if (!current()) {
      throw StateError('login entry retired');
    }
    _accessToken = token;
    _accountID = accountID;
    _rejectedSessionOwnerID = null;
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

  Future<void> _discardIssuedToken(String token) async {
    try {
      await _withStorage(() async {
        final saved = await _sessionVault.read();
        if (saved?.belongsTo(_apiOrigin) == true && saved?.token == token) {
          await _sessionVault.delete();
        }
      });
    } catch (_) {
      // Failed cleanup never publishes a credential; revoke is still attempted.
    }
    await _revokeIssuedToken(token);
  }

  Future<void> signOut() async {
    if (_closed) return;
    final token = _accessToken;
    if (token == null) {
      if (_rejectedSessionOwnerID != null) {
        // An explicit logout cancels recovery even after the rejected token
        // was removed. In-flight login replies cannot undo that decision.
        ++_authAttempt;
        _busy = false;
        _rejectedSessionOwnerID = null;
        _notify();
      }
      return;
    }
    if (_busy) return;
    final attempt = _beginAttempt();
    if (attempt == null) return;
    // Local access and visible private projections end at the user's logout
    // gesture, even if remote revocation is slow or the device is offline.
    _accessToken = null;
    _displayName = null;
    _accountID = null;
    _rejectedSessionOwnerID = null;
    _loginMethod = null;
    _notify();
    try {
      if (!_ownsAttempt(attempt)) return;
      await _withStorage(() async {
        final saved = await _sessionVault.read();
        if (_ownsAttempt(attempt) &&
            saved?.belongsTo(_apiOrigin) == true &&
            saved?.token == token) {
          await _sessionVault.delete();
        }
      });
      if (!_ownsAttempt(attempt)) return;
      final response = await _client
          .post(
            _endpoint('/v1/session/logout'),
            headers: {'Authorization': 'Bearer $token'},
          )
          .timeout(const Duration(seconds: 10));
      if (response.statusCode != 204 && response.statusCode != 401) {
        throw const FormatException('logout unavailable');
      }
      if (!_ownsAttempt(attempt)) return;
    } catch (_) {
      if (_ownsAttempt(attempt)) _error = '本机已退出，服务端会话注销暂未确认。';
    } finally {
      _finishAttempt(attempt);
    }
  }

  /// Retires only the credential that actually received a 401. A late response
  /// from an old session cannot sign out a newly authenticated account. Clear
  /// display/access state immediately; serialize the exact vault deletion.
  Future<bool> invalidateRejectedSession(String expectedAuthorization) async {
    if (_closed ||
        authorizationHeader != expectedAuthorization ||
        _accessToken == null)
      return false;
    final token = _accessToken!;
    _rejectedSessionOwnerID = _accountID;
    ++_authAttempt;
    _busy = false;
    _accessToken = null;
    _accountID = null;
    _displayName = null;
    _loginMethod = null;
    _error = null;
    _notify();
    await _withStorage(() async {
      final saved = await _sessionVault.read();
      if (saved?.belongsTo(_apiOrigin) == true && saved?.token == token) {
        await _sessionVault.delete();
      }
    });
    return true;
  }

  @override
  void dispose() {
    if (_closed) return;
    _closed = true;
    ++_authAttempt;
    _queuedNativeCallback = null;
    unawaited(_nativeLinks?.cancel());
    if (_ownsClient) _client.close();
    super.dispose();
  }
}
