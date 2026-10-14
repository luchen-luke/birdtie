import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:birdtie_client/src/workspace/chat_message_operation.dart';
import 'package:birdtie_client/src/workspace/friend_audio.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const audioOwner = '11111111-1111-4111-8111-111111111111';
const audioPeer = '22222222-2222-4222-8222-222222222222';
const audioConversation = '33333333-3333-4333-8333-333333333333';
const audioOperation = '44444444-4444-4444-8444-444444444444';
const audioMessage = '55555555-5555-4555-8555-555555555555';

// Synthetic transport bytes; native AAC validation and actual recording need
// their own backend and device checks.
FriendAudioDraft audioFixture() {
  final bytes = Uint8List.fromList([1, 2, 3, 4]);
  return FriendAudioDraft(
    bytes,
    FriendAudioMetadata(
      byteSize: bytes.length,
      sha256: sha256.convert(bytes).toString(),
      durationMillis: 1234,
    ),
  );
}

Map<String, Object> audioReceipt(
  PendingFriendAudio p, {
  String owner = audioOwner,
}) => {
  'schemaVersion': friendAudioReceiptSchema,
  'ownerId': owner,
  'conversationId': p.conversationID,
  'operationId': p.operationID,
  'effectKey': humanMessageEffectKey(owner, p.operationID),
  'payloadDigest': p.payloadDigest,
  'messageId': audioMessage,
  ...p.metadata.toJson(),
  'createdAt': '2026-10-11T01:02:03Z',
};
http.Response audioWire(Object data, [int status = 200]) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': data})),
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'audio receipt binds owner, recipient, original operation and content',
    () {
      final p = PendingFriendAudio(
        operationID: audioOperation,
        conversationID: audioConversation,
        metadata: audioFixture().metadata,
      );
      expect(
        FriendAudioReceipt.decode(audioReceipt(p), audioOwner, p).messageID,
        audioMessage,
      );
      for (final key in [
        'ownerId',
        'conversationId',
        'operationId',
        'effectKey',
        'payloadDigest',
        'sha256',
        'durationMillis',
        'byteSize',
      ]) {
        final raw = audioReceipt(p);
        raw[key] = raw[key] is int ? (raw[key] as int) + 1 : audioPeer;
        expect(
          () => FriendAudioReceipt.decode(raw, audioOwner, p),
          throwsFormatException,
          reason: 'changed $key cannot acknowledge the visible draft',
        );
      }
      final copy = Uint8List.fromList([1, 2, 3, 4]);
      final draft = FriendAudioDraft(copy, p.metadata);
      copy[0] = 9;
      expect(draft.bytes[0], 1);
      expect(() => draft.bytes[0] = 9, throwsUnsupportedError);
    },
  );

  test(
    'pending recovery metadata is separated by owner and stores no recording',
    () async {
      final store = MemoryFriendAudioPendingStore();
      final p = PendingFriendAudio(
        operationID: audioOperation,
        conversationID: audioConversation,
        metadata: audioFixture().metadata,
      );
      await store.write('https://unit.example', audioOwner, p);
      expect(await store.read('https://unit.example', audioPeer), isEmpty);
      expect(await store.read('https://other.example', audioOwner), isEmpty);
      expect(jsonEncode(p.toJson()), isNot(contains('contentBase64')));
      final changed = PendingFriendAudio(
        operationID: p.operationID,
        conversationID: audioPeer,
        metadata: p.metadata,
      );
      await expectLater(
        store.write('https://unit.example', audioOwner, changed),
        throwsStateError,
      );
      expect(
        (await store.read('https://unit.example', audioOwner)).single,
        same(p),
      );
    },
  );

  test(
    'transport sends only explicit original bytes and recovery GET never resends',
    () async {
      final draft = audioFixture(), calls = <String>[];
      final p = PendingFriendAudio(
        operationID: audioOperation,
        conversationID: audioConversation,
        metadata: draft.metadata,
      );
      final source = FriendAudioSource(
        authorizationHeader: () => 'Bearer synthetic',
        environment: 'https://unit.example',
        client: MockClient((r) async {
          calls.add('${r.method} ${r.url.path}');
          if (r.method == 'POST') {
            final body = jsonDecode(r.body) as Map<String, dynamic>;
            expect(body.keys.toSet(), {
              'operationId',
              'mimeType',
              'byteSize',
              'sha256',
              'durationMillis',
              'contentBase64',
            });
            expect(base64Decode(body['contentBase64']), draft.bytes);
            expect(body['operationId'], audioOperation);
            return audioWire(audioReceipt(p), 201);
          }
          return audioWire(audioReceipt(p));
        }),
      );
      await source.send(audioOwner, p, draft);
      await source.recover(audioOwner, p);
      expect(calls, [
        'POST /v1/me/conversations/$audioConversation/audio-message-operations',
        'GET /v1/me/conversations/$audioConversation/audio-message-operations/$audioOperation',
      ]);
      source.dispose();
    },
  );

  test(
    'content requires current server audio metadata before playback',
    () async {
      final draft = audioFixture();
      for (final failure in [
        'blocked',
        'size',
        'hash',
        'duration',
        'mime',
        'bytes',
      ]) {
        final source = FriendAudioSource(
          authorizationHeader: () => 'Bearer synthetic',
          environment: 'https://unit.example',
          client: MockClient.streaming((r, _) async {
            if (failure == 'blocked')
              return http.StreamedResponse(
                Stream.value(utf8.encode('{}')),
                404,
              );
            return http.StreamedResponse(
              Stream.value(failure == 'bytes' ? [9, 2, 3, 4] : draft.bytes),
              200,
              contentLength: failure == 'size' ? 5 : draft.metadata.byteSize,
              headers: {
                'content-type': failure == 'mime' ? 'audio/mpeg' : 'audio/mp4',
                'content-length':
                    '${failure == 'size' ? 5 : draft.metadata.byteSize}',
                'x-content-sha256': failure == 'hash' ? 'changed' : draft.sha,
                'x-audio-duration-millis':
                    '${failure == 'duration' ? 999 : draft.durationMillis}',
              },
            );
          }),
        );
        await expectLater(
          source.content(audioConversation, audioMessage, draft.metadata),
          failure == 'blocked'
              ? throwsA(isA<FriendAudioDeliveryFailure>())
              : throwsFormatException,
          reason: failure,
        );
        source.dispose();
      }
    },
  );
  test('native cancellation invalidates a late stopped recording', () async {
    const channel = MethodChannel('birdtie/friend_audio_test');
    final stop = Completer<Object?>(), calls = <String>[];
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
          calls.add(call.method);
          return call.method == 'stop' ? stop.future : null;
        });
    addTearDown(
      () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, null),
    );
    final port = NativeFriendAudioPort(channel: channel, supported: () => true);
    addTearDown(port.dispose);
    await port.start('friend-record-1', (_) {});
    final stopped = port.stop('friend-record-1');
    await port.cancel('friend-record-1');
    final draft = audioFixture();
    stop.complete({
      'requestId': 'friend-record-1',
      ...draft.metadata.toJson(),
      'contentBase64': base64Encode(draft.bytes),
    });
    await expectLater(stopped, throwsFormatException);
    expect(calls, ['start', 'stop', 'cancel']);
  });
  test(
    'unsupported or disposed native owner does not access microphone or play',
    () async {
      const channel = MethodChannel('birdtie/friend_audio_unavailable_test');
      var calls = 0;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (_) async {
            calls++;
            return null;
          });
      addTearDown(
        () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
            .setMockMethodCallHandler(channel, null),
      );
      final port = NativeFriendAudioPort(
        channel: channel,
        supported: () => false,
      );
      await expectLater(
        port.start('friend-record-1', (_) {}),
        throwsA(isA<PlatformException>()),
      );
      await expectLater(
        port.play('friend-play-1', audioFixture(), () {}),
        throwsA(isA<PlatformException>()),
      );
      port.dispose();
      expect(calls, 0);
      expect(port.supported, isFalse);
    },
  );
}
