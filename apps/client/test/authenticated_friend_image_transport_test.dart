import 'dart:async';
import 'dart:typed_data';

import 'package:birdtie_client/src/auth/authenticated_client.dart';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/workspace/friend_image.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

import 'friend_image_test.dart'
    show imageFixtureBytes, imageMetadata, imageConversation, imageMessage;

class _Auth extends BirdtieAuthController {
  String? token = 'Bearer A', owner = 'A';
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

// Keep the body as an actual stream: MockClient would buffer or replace it.
// Deliberately ignore request.abortTrigger to exercise a transport whose
// already-arriving headers cannot be prevented by the feature's cancellation.
class _Transport extends http.BaseClient {
  _Transport(this.reply);
  final Future<http.StreamedResponse> Function(http.BaseRequest) reply;
  final tokens = <String?>[];
  int closes = 0;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) {
    tokens.add(request.headers['Authorization']);
    return reply(request);
  }

  @override
  void close() {
    ++closes;
  }
}

class _Body {
  _Body({this.cleanup}) {
    controller = StreamController<List<int>>(
      onListen: listened.complete,
      onCancel: () async {
        ++cancels;
        if (!cancelled.isCompleted) cancelled.complete();
        await cleanup?.future;
      },
    );
  }
  final Completer<void>? cleanup;
  final listened = Completer<void>(), cancelled = Completer<void>();
  final firstChunk = Completer<void>();
  late final StreamController<List<int>> controller;
  int chunks = 0, bytes = 0, cancels = 0;
  Stream<List<int>> get stream => controller.stream.map((chunk) {
    ++chunks;
    bytes += chunk.length;
    if (!firstChunk.isCompleted) firstChunk.complete();
    return chunk;
  });
  Future<void> dispose() async {
    if (cleanup != null && !cleanup!.isCompleted) cleanup!.complete();
    await controller.close();
  }
}

class _Fixture {
  _Fixture(
    this.bytes,
    Future<http.StreamedResponse> Function(http.BaseRequest) first,
  ) {
    transport = _Transport((request) async {
      if (transport.tokens.length == 1) {
        started.complete();
        return first(request);
      }
      return imageResponse(Stream.value(bytes), request: request);
    });
    client = AuthenticatedClient(
      auth: auth,
      client: transport,
      onUnauthorized: (_) async {
        ++recoveries;
        auth.use(null, null);
      },
    );
    source = FriendImageSource(
      client: client,
      environment: 'https://auth-image.fixture',
      authorizationHeader: () => auth.authorizationHeader,
      current: () => auth.signedIn,
      binding: () => auth.accountID,
      enabled: true,
    );
  }
  final Uint8List bytes;
  final auth = _Auth();
  final started = Completer<void>();
  late final _Transport transport;
  late final AuthenticatedClient client;
  late final FriendImageSource source;
  int published = 0, recoveries = 0;

  http.StreamedResponse imageResponse(
    Stream<List<int>> body, {
    http.BaseRequest? request,
    int status = 200,
  }) {
    final meta = imageMetadata(bytes).full;
    return http.StreamedResponse(
      body,
      status,
      request: request,
      headers: {
        'content-type': meta.mimeType,
        'content-length': meta.byteSize.toString(),
        'x-content-sha256': meta.sha256,
        'x-image-width': '4',
        'x-image-height': '4',
        'x-image-variant': 'full',
        'cache-control': 'no-store',
        'x-content-type-options': 'nosniff',
      },
    );
  }

  Future<Uint8List> read() => source
      .content(
        conversation: imageConversation,
        message: imageMessage,
        metadata: imageMetadata(bytes),
        variant: FriendImageVariant.full,
      )
      .then((result) {
        ++published;
        return result;
      });

  void dispose() {
    source.dispose();
    client.close();
    expect(transport.closes, 0, reason: 'both layers borrow the transport');
    transport.close();
    auth.dispose();
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  for (final sample in [
    (status: 200, aba: false, lateCancel: false, cleanupError: false),
    (status: 401, aba: true, lateCancel: true, cleanupError: false),
    (status: 200, aba: false, lateCancel: true, cleanupError: true),
  ]) {
    test('retired image headers $sample cancel without draining and leave '
        'the current shared transport usable', () async {
      final headers = Completer<http.StreamedResponse>();
      final cleanup = sample.lateCancel ? Completer<void>() : null;
      final body = _Body(cleanup: cleanup);
      final fixture = _Fixture(
        await imageFixtureBytes(),
        (_) => headers.future,
      );
      addTearDown(body.dispose);
      addTearDown(fixture.dispose);
      // More body bytes than the image's declared size: retirement must not
      // pull them through either parser or a background drain at all.
      for (var i = 0; i < 8; ++i) {
        body.controller.add(Uint8List(32 * 1024));
      }
      final rejected = expectLater(
        fixture.read(),
        throwsA(isA<http.ClientException>()),
      );
      await fixture.started.future;
      fixture.auth.use('Bearer B', 'B');
      if (sample.aba) fixture.auth.use('Bearer A', 'A');
      headers.complete(
        fixture.imageResponse(body.stream, status: sample.status),
      );
      await rejected.timeout(const Duration(seconds: 2));
      await Future<void>.delayed(Duration.zero);
      expect(body.cancels, 1);
      expect(body.chunks, 0);
      expect(body.bytes, 0);
      expect(fixture.published, 0);
      expect(
        fixture.recoveries,
        0,
        reason: 'a stale 401 cannot retire the new identity',
      );
      if (cleanup != null) expect(cleanup.isCompleted, isFalse);
      // A new authenticated image can complete even while old cancellation
      // cleanup is still pending. Neither layer closes the shared client.
      expect(await fixture.read(), fixture.bytes);
      expect(fixture.published, 1);
      expect(fixture.transport.tokens, [
        'Bearer A',
        sample.aba ? 'Bearer A' : 'Bearer B',
      ]);
      expect(fixture.transport.closes, 0);
      if (cleanup != null) {
        if (sample.cleanupError) {
          cleanup.completeError(
            StateError('synthetic cancellation cleanup error'),
          );
        } else {
          cleanup.complete();
        }
        await Future<void>.delayed(Duration.zero);
      }
      expect(body.chunks, 0);
    });
  }

  for (final aba in [false, true]) {
    test('accepted image prefix is never published after identity change '
        'and unread tail is cancelled; ABA=$aba', () async {
      final body = _Body();
      late _Fixture fixture;
      fixture = _Fixture(
        await imageFixtureBytes(),
        (request) async => fixture.imageResponse(body.stream, request: request),
      );
      addTearDown(body.dispose);
      addTearDown(fixture.dispose);
      final rejected = expectLater(
        fixture.read(),
        throwsA(isA<http.ClientException>()),
      );
      await body.listened.future;
      body.controller.add(fixture.bytes.sublist(0, 1));
      await body.firstChunk.future;
      await Future<void>.delayed(Duration.zero);
      fixture.auth.use('Bearer B', 'B');
      if (aba) fixture.auth.use('Bearer A', 'A');
      body.controller.add(fixture.bytes.sublist(1, 2));
      body.controller.add(fixture.bytes.sublist(2));
      await rejected;
      expect(body.cancels, 1);
      expect(
        body.bytes,
        2,
        reason:
            'guard checks the next chunk then cancels before the remaining image',
      );
      expect(body.bytes, lessThan(fixture.bytes.length));
      expect(fixture.published, 0);
      expect(fixture.recoveries, 0);
      expect(await fixture.read(), fixture.bytes);
      expect(fixture.transport.closes, 0);
    });
  }

  test(
    'concurrent image 401s still request one recovery with zero replay',
    () async {
      final auth = _Auth();
      final replies = <Completer<http.StreamedResponse>>[];
      final bodies = <_Body>[];
      final bothStarted = Completer<void>();
      var recoveries = 0, published = 0;
      final transport = _Transport((request) {
        final response = Completer<http.StreamedResponse>();
        replies.add(response);
        if (replies.length == 2) bothStarted.complete();
        return response.future;
      });
      final client = AuthenticatedClient(
        auth: auth,
        client: transport,
        onUnauthorized: (_) async {
          ++recoveries;
          auth.use(null, null);
        },
      );
      final bytes = await imageFixtureBytes();
      final source = FriendImageSource(
        client: client,
        environment: 'https://auth-image.fixture',
        authorizationHeader: () => auth.authorizationHeader,
        current: () => auth.signedIn,
        binding: () => auth.accountID,
        enabled: true,
      );
      addTearDown(() async {
        source.dispose();
        client.close();
        expect(transport.closes, 0);
        transport.close();
        auth.dispose();
        for (final body in bodies) {
          await body.dispose();
        }
      });
      Future<Object> read() => source
          .content(
            conversation: imageConversation,
            message: imageMessage,
            metadata: imageMetadata(bytes),
            variant: FriendImageVariant.full,
          )
          .then<Object>((value) {
            ++published;
            return value;
          }, onError: (Object error, StackTrace _) => error);
      final outcome = Future.wait([read(), read()]);
      await bothStarted.future;
      for (final reply in replies) {
        final body = _Body();
        bodies.add(body);
        body.controller.add(Uint8List(32 * 1024));
        reply.complete(http.StreamedResponse(body.stream, 401));
      }
      final errors = await outcome;
      expect(errors.whereType<FriendImageDeliveryFailure>().single.status, 401);
      expect(errors.whereType<http.ClientException>().length, 1);
      expect(recoveries, 1);
      expect(published, 0);
      expect(transport.tokens, ['Bearer A', 'Bearer A']);
      expect(bodies.map((b) => b.cancels), everyElement(1));
      expect(bodies.map((b) => b.bytes), everyElement(0));
    },
  );
}
