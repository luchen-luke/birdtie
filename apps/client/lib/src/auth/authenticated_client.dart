import 'dart:async';

import 'package:http/http.dart' as http;

import 'birdtie_auth_controller.dart';

/// A shared credential boundary. It never retries a write or adds credentials
/// to an unauthenticated/public request. Server permissions remain authoritative.
class AuthenticatedClient extends http.BaseClient {
  AuthenticatedClient({
    required BirdtieAuthController auth,
    required this.onUnauthorized,
    http.Client? client,
  }) : _auth = auth,
       _client = client ?? http.Client(),
       _ownsClient = client == null {
    auth.addListener(_identityChanged);
    _identity = (auth.authorizationHeader, auth.accountID);
  }

  BirdtieAuthController _auth;
  BirdtieAuthController get auth => _auth;
  final Future<void> Function(String) onUnauthorized;
  final http.Client _client;
  final bool _ownsClient;
  late (String?, String?) _identity;
  int _epoch = 0;
  bool _closed = false;

  void updateAuth(BirdtieAuthController next) {
    if (_closed || identical(next, _auth)) return;
    _auth.removeListener(_identityChanged);
    _auth = next;
    _auth.addListener(_identityChanged);
    _identity = (next.authorizationHeader, next.accountID);
    ++_epoch;
  }

  void _identityChanged() {
    final next = (auth.authorizationHeader, auth.accountID);
    if (next != _identity) {
      _identity = next;
      ++_epoch;
    }
  }

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    if (_closed) throw http.ClientException('连接已关闭', request.url);
    final token = request.headers['Authorization'];
    final epoch = _epoch;
    if (token != null &&
        (token != auth.authorizationHeader || !auth.signedIn)) {
      throw http.ClientException('账户已变化，请重新操作', request.url);
    }
    final response = await _client.send(request);
    if (token != null && (_closed || epoch != _epoch)) {
      // Retired private responses must stop at the transport boundary, not
      // drain the previous account's whole body. Cancellation cleanup may be
      // slow or fail; it must not delay retirement or close the shared client.
      final discarded = response.stream.listen(null, onError: (Object _) {});
      unawaited(discarded.cancel().catchError((Object _) {}));
      throw http.ClientException('账户已变化，请重新操作', request.url);
    }
    if (token != null &&
        response.statusCode == 401 &&
        !_closed &&
        epoch == _epoch &&
        token == auth.authorizationHeader) {
      await onUnauthorized(token);
    }
    if (token != null &&
        response.statusCode >= 200 &&
        response.statusCode < 300) {
      final body = _guardResponseBody(response.stream, epoch, request.url);
      return response is http.BaseResponseWithUrl
          ? _GuardedResponseWithUrl(
              body,
              response,
              (response as http.BaseResponseWithUrl).url,
            )
          : _GuardedResponse(body, response);
    }
    return response;
  }

  Stream<List<int>> _guardResponseBody(
    Stream<List<int>> body,
    int epoch,
    Uri url,
  ) async* {
    void requireCurrent() {
      if (_closed || epoch != _epoch) {
        throw http.ClientException('账户已变化，请重新操作', url);
      }
    }

    await for (final chunk in body) {
      requireCurrent();
      yield chunk;
    }
    // A buffered prefix can arrive while current, followed by an account
    // change before the final byte. Never publish that prefix as a complete
    // successful private response, including zero-length bodies.
    requireCurrent();
  }

  @override
  void close() {
    if (_closed) return;
    _closed = true;
    ++_epoch;
    auth.removeListener(_identityChanged);
    if (_ownsClient) _client.close();
  }
}

class _GuardedResponse extends http.StreamedResponse {
  _GuardedResponse(Stream<List<int>> body, http.StreamedResponse source)
    : super(
        body,
        source.statusCode,
        contentLength: source.contentLength,
        request: source.request,
        headers: source.headers,
        isRedirect: source.isRedirect,
        persistentConnection: source.persistentConnection,
        reasonPhrase: source.reasonPhrase,
      );
}

class _GuardedResponseWithUrl extends _GuardedResponse
    implements http.BaseResponseWithUrl {
  _GuardedResponseWithUrl(super.body, super.source, this.url);
  @override
  final Uri url;
}
