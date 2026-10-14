import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/authenticated_client.dart';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

class _Auth extends BirdtieAuthController {
  String? token = 'Bearer A';
  String? owner = 'A';
  @override
  String? get authorizationHeader => token;
  @override
  String? get accountID => owner;
  @override
  bool get signedIn => token != null;
  void use(String? next, String? account) {
    token = next;
    owner = account;
    notifyListeners();
  }
}

class _ResponseWithUrl extends http.StreamedResponse
    implements http.BaseResponseWithUrl {
  _ResponseWithUrl(
    super.stream,
    super.statusCode, {
    required this.url,
    super.contentLength,
    super.headers,
    super.request,
    super.isRedirect,
    super.persistentConnection,
    super.reasonPhrase,
  });
  @override
  final Uri url;
}

// MockClient.streaming recreates StreamedResponse and drops optional url.
// This transport keeps the actual response representation for metadata tests.
class _PreservingClient extends http.BaseClient {
  _PreservingClient(this.handler);
  final Future<http.StreamedResponse> Function(http.BaseRequest) handler;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    await request.finalize().drain<void>();
    return handler(request);
  }
}

void main() {
  test('concurrent 401s retire once without replaying writes', () async {
    final auth = _Auth();
    addTearDown(auth.dispose);
    var sends = 0, recoveries = 0;
    final pending = Completer<void>();
    final client = AuthenticatedClient(
      auth: auth,
      client: MockClient((r) async {
        sends++;
        await pending.future;
        return http.Response('{"error":"unauthorized"}', 401);
      }),
      onUnauthorized: (token) async {
        recoveries++;
        auth.use(null, null);
      },
    );
    addTearDown(client.close);
    final first = client.post(
      Uri.parse('https://unit.invalid/v1/me/send'),
      headers: {'Authorization': 'Bearer A'},
    );
    final second = client.get(
      Uri.parse('https://unit.invalid/v1/me/ties'),
      headers: {'Authorization': 'Bearer A'},
    );
    // Attach both error observers before releasing concurrent responses. The
    // response retired by the other request must not reach a private reader.
    final outcomes = Future.wait<Object>([
      first.then<Object>(
        (response) => response,
        onError: (Object error, StackTrace _) => error,
      ),
      second.then<Object>(
        (response) => response,
        onError: (Object error, StackTrace _) => error,
      ),
    ]);
    pending.complete();
    final responses = await outcomes;
    expect(responses.whereType<http.Response>().single.statusCode, 401);
    expect(
      responses.whereType<http.Response>().single.body,
      '{"error":"unauthorized"}',
      reason:
          'the initiating recovery still needs its authoritative error body',
    );
    expect(responses.whereType<http.ClientException>().length, 1);
    expect(sends, 2);
    expect(recoveries, 1);
    expect(auth.signedIn, isFalse);
  });
  test(
    'late old 401 and identity ABA cannot retire a current account',
    () async {
      final auth = _Auth();
      addTearDown(auth.dispose);
      final pending = Completer<http.Response>();
      var recoveries = 0;
      final client = AuthenticatedClient(
        auth: auth,
        client: MockClient((r) => pending.future),
        onUnauthorized: (_) async {
          recoveries++;
        },
      );
      addTearDown(client.close);
      final future = client.get(
        Uri.parse('https://unit.invalid/v1/me/ties'),
        headers: {'Authorization': 'Bearer A'},
      );
      await Future<void>.delayed(Duration.zero);
      auth.use('Bearer B', 'B');
      auth.use('Bearer A', 'A');
      final rejected = expectLater(
        future,
        throwsA(isA<http.ClientException>()),
      );
      pending.complete(http.Response('', 401));
      await rejected;
      expect(recoveries, 0);
      expect(auth.accountID, 'A');
    },
  );
  for (final returnToOriginal in [false, true]) {
    test(
      'late successful private response is rejected after account change; ABA=$returnToOriginal',
      () async {
        final auth = _Auth();
        addTearDown(auth.dispose);
        final started = Completer<void>(),
            response = Completer<http.Response>();
        var recoveries = 0, receivedPrivateBody = false;
        final client = AuthenticatedClient(
          auth: auth,
          client: MockClient((_) async {
            started.complete();
            return response.future;
          }),
          onUnauthorized: (_) async {
            recoveries++;
          },
        );
        addTearDown(client.close);
        final read = client
            .get(
              Uri.parse('https://unit.invalid/v1/me/messages'),
              headers: {'Authorization': 'Bearer A'},
            )
            .then((value) {
              receivedPrivateBody = value.body.contains('old-owner-private');
              return value;
            });
        final rejected = expectLater(
          read,
          throwsA(isA<http.ClientException>()),
        );
        await started.future;
        auth.use('Bearer B', 'B');
        if (returnToOriginal) auth.use('Bearer A', 'A');
        response.complete(http.Response('old-owner-private', 200));
        await rejected;
        expect(receivedPrivateBody, isFalse);
        expect(
          recoveries,
          0,
          reason: 'a retired success is not a login expiry',
        );
        expect(auth.accountID, returnToOriginal ? 'A' : 'B');
      },
    );
  }
  test(
    'private response body arriving after accepted headers is retired on account switch',
    () async {
      final auth = _Auth();
      addTearDown(auth.dispose);
      final bodyStarted = Completer<void>();
      final body = StreamController<List<int>>(
        onListen: () => bodyStarted.complete(),
      );
      var receivedPrivateBody = false;
      final client = AuthenticatedClient(
        auth: auth,
        client: MockClient.streaming(
          (request, _) async => http.StreamedResponse(body.stream, 200),
        ),
        onUnauthorized: (_) async {
          fail('late successful bytes must not cause login recovery');
        },
      );
      addTearDown(client.close);
      final read = client
          .get(
            Uri.parse('https://unit.invalid/v1/me/messages'),
            headers: {'Authorization': 'Bearer A'},
          )
          .then((value) {
            receivedPrivateBody = value.body.contains('old-owner-private');
            return value;
          });
      final rejected = expectLater(read, throwsA(isA<http.ClientException>()));
      await bodyStarted.future;
      auth.use('Bearer B', 'B');
      body.add(utf8.encode('old-owner-private'));
      await body.close();
      await rejected;
      expect(receivedPrivateBody, isFalse);
      expect(auth.accountID, 'B');
    },
  );
  test(
    'private body already buffered under A cannot complete successfully after identity ABA',
    () async {
      final auth = _Auth();
      addTearDown(auth.dispose);
      final prefixReceived = Completer<void>();
      final body = StreamController<List<int>>();
      final client = AuthenticatedClient(
        auth: auth,
        client: MockClient.streaming(
          (request, _) async => http.StreamedResponse(body.stream, 200),
        ),
        onUnauthorized: (_) async {},
      );
      addTearDown(client.close);
      final response = await client.send(
        http.Request('GET', Uri.parse('https://unit.invalid/v1/me/messages'))
          ..headers['Authorization'] = 'Bearer A',
      );
      // Simulate a response parser that has received the valid prefix and is
      // still waiting for completion when identity changes.
      final received = <int>[];
      final done = Completer<void>();
      final errors = <Object>[];
      response.stream.listen(
        (chunk) {
          received.addAll(chunk);
          prefixReceived.complete();
        },
        onError: (Object error) {
          errors.add(error);
        },
        onDone: done.complete,
      );
      body.add(utf8.encode('valid-private-prefix'));
      await prefixReceived.future;
      auth.use('Bearer B', 'B');
      auth.use('Bearer A', 'A');
      await body.close();
      await done.future;
      expect(utf8.decode(received), 'valid-private-prefix');
      expect(
        errors.single,
        isA<http.ClientException>(),
        reason:
            'completion must fail even when no chunk arrives after retirement',
      );
    },
  );
  test(
    'guarded success preserves transport metadata and final response URL',
    () async {
      final auth = _Auth();
      addTearDown(auth.dispose);
      final finalURL = Uri.parse('https://unit.invalid/redirected');
      final client = AuthenticatedClient(
        auth: auth,
        client: _PreservingClient(
          (request) async => _ResponseWithUrl(
            Stream.value(utf8.encode('safe')),
            206,
            url: finalURL,
            contentLength: 4,
            request: request,
            headers: {
              'content-type': 'text/plain; charset=utf-8',
              'x-owner': 'A',
            },
            isRedirect: true,
            persistentConnection: false,
            reasonPhrase: 'Partial Content',
          ),
        ),
        onUnauthorized: (_) async {},
      );
      addTearDown(client.close);
      final request = http.Request(
        'GET',
        Uri.parse('https://unit.invalid/private'),
      )..headers['Authorization'] = 'Bearer A';
      final response = await client.send(request);
      expect(response.statusCode, 206);
      expect(response.contentLength, 4);
      expect(response.request, same(request));
      expect(response.headers['x-owner'], 'A');
      expect(response.isRedirect, isTrue);
      expect(response.persistentConnection, isFalse);
      expect(response.reasonPhrase, 'Partial Content');
      expect((response as http.BaseResponseWithUrl).url, finalURL);
      expect(await response.stream.bytesToString(), 'safe');
    },
  );
  test(
    '403, offline and anonymous public reads do not request login',
    () async {
      final auth = _Auth();
      addTearDown(auth.dispose);
      var recoveries = 0;
      final client = AuthenticatedClient(
        auth: auth,
        client: MockClient((r) async {
          if (r.url.path == '/offline') throw http.ClientException('offline');
          return http.Response('', 403);
        }),
        onUnauthorized: (_) async {
          recoveries++;
        },
      );
      addTearDown(client.close);
      expect(
        (await client.get(
          Uri.parse('https://unit.invalid/private'),
          headers: {'Authorization': 'Bearer A'},
        )).statusCode,
        403,
      );
      await expectLater(
        client.get(Uri.parse('https://unit.invalid/offline')),
        throwsA(isA<http.ClientException>()),
      );
      auth.use(null, null);
      await client.get(Uri.parse('https://unit.invalid/public'));
      expect(recoveries, 0);
      await expectLater(
        client.get(
          Uri.parse('https://unit.invalid/private'),
          headers: {'Authorization': 'Bearer A'},
        ),
        throwsA(isA<http.ClientException>()),
      );
    },
  );
}
