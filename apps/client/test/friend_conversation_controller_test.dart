import 'package:birdtie_client/src/content/moment_image_picker.dart';
import 'package:birdtie_client/src/workspace/friend_image.dart';
import 'friend_image_test.dart' show imageFixtureBytes, imageMetadata, imageReceipt, imageWire;
import 'private_moment_media_controller_test.dart' show TestMomentImagePicker;
import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/workspace/chat_message_pending_store.dart';
import 'package:birdtie_client/src/workspace/chat_message_operation.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/friend_audio.dart';
import 'package:birdtie_client/src/workspace/friend_conversation_controller.dart';
import 'package:birdtie_client/src/workspace/friend_conversation_thread.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'friend_audio_test.dart'
    show
        audioFixture,
        audioReceipt,
        audioWire,
        audioOwner,
        audioPeer,
        audioConversation,
        audioMessage;
import 'connection_chat_page_test.dart'
    show chatPageWire, pageMessage, pageMessageID;

http.Response _historyPage(List<Object> items) =>
    audioWire({'items': items, 'nextCursor': null});

class _Auth extends BirdtieAuthController {
  _Auth() : super(client: MockClient((_) async => http.Response('{}', 404)));
  String? owner = audioOwner, credential = 'Bearer synthetic-a', rejected;
  @override
  String? get accountID => owner;
  @override
  String? get authorizationHeader => credential;
  @override
  bool get signedIn => credential != null;
  @override
  String? get rejectedSessionOwnerID => rejected;
  void change(String? next, String? token, {String? expired}) {
    owner = next;
    credential = token;
    rejected = expired;
    notifyListeners();
  }
}

class _Audio extends FriendAudioPort {
  final cancelled = <String>[];
  Completer<FriendAudioDraft>? stopping;
  int starts = 0, plays = 0;
  @override
  bool get supported => true;
  @override
  Future<void> start(String id, ValueChanged<FriendAudioPhase> phase) async {
    starts++;
    phase(FriendAudioPhase.recording);
  }

  @override
  Future<FriendAudioDraft> stop(String id) =>
      stopping?.future ?? Future.value(audioFixture());
  @override
  Future<void> cancel(String id) async {
    cancelled.add(id);
  }

  @override
  Future<void> play(
    String id,
    FriendAudioDraft draft,
    VoidCallback finished,
  ) async {
    plays++;
  }

  @override
  Future<void> stopPlayback(String id) async {}
  @override
  void dispose() {}
}

const _chat = HumanConversation(
  id: audioConversation,
  otherAccountId: audioPeer,
  otherName: '测试联系人',
);
FriendConversationController _controller(
  _Auth auth,
  _Audio audio,
  http.Client client, {
  MemoryFriendAudioPendingStore? pending,
  void Function(String)? rejected,
  bool Function()? visible,
  bool imageEnabled = false,
  MomentImagePicker? imagePicker,
  MemoryFriendImagePendingStore? imageStore,
}) {
  final c = FriendConversationController(
    auth: auth,
    conversation: _chat,
    apiBaseUrl: 'https://unit.example',
    client: client,
    audioPort: audio,
    audioPendingStore: pending ?? MemoryFriendAudioPendingStore(),
    messagePendingStore: MemoryHumanMessagePendingStore(),
    onUnauthorized: rejected,
    isVisible: visible,
    imageEnabled: imageEnabled, imagePicker: imagePicker,
    imagePendingStore: imageStore ?? MemoryFriendImagePendingStore(),
  );
  addTearDown(c.dispose);
  addTearDown(auth.dispose);
  return c;
}

Map<String, Object> _historyMessage(String id, {String body = '来自联系人的消息'}) => {
  'id': id,
  'conversationId': audioConversation,
  'senderAccountId': audioPeer,
  'speakerKind': 'human',
  'body': body,
  'createdAt': '2026-10-11T01:02:03Z',
};

const _newerMessage = 'a892882b-edb7-4c40-a5ad-32d69108b997';

Future<void> _paintThread(
  WidgetTester tester,
  FriendConversationController controller,
) => tester.pumpWidget(
  MaterialApp(
    home: Scaffold(body: FriendConversationThread(controller: controller)),
  ),
);

Future<({PendingFriendImage pending, FriendImageMetadata metadata})>
_seedUnconfirmedImage(MemoryFriendImagePendingStore store) async {
  final bytes = await imageFixtureBytes();
  final pending = PendingFriendImage.review(
    operationID: '44444444-4444-4444-8444-444444444444',
    conversationID: audioConversation,
    recipientID: audioPeer,
    draft: FriendImageDraft(bytes, width: 4, height: 4),
    caption: '原图片说明',
  );
  await store.write('https://unit.example', audioOwner, pending);
  return (pending: pending, metadata: imageMetadata(bytes));
}

http.Response _independentTextReceipt(http.Request request) {
  final body = jsonDecode(request.body) as Map<String, dynamic>;
  return audioWire({
    'schemaVersion': humanMessageOperationSchema,
    'ownerId': audioOwner,
    'conversationId': audioConversation,
    'operationId': body['operationId'],
    'effectKey': humanMessageEffectKey(audioOwner, body['operationId']),
    'payloadDigest': humanMessagePayloadDigest(audioConversation, body['body']),
    'toolVersion': humanMessageToolVersion,
    'messageId': audioMessage,
    'createdAt': '2026-10-12T01:02:03Z',
  }, 201);
}

void imageRecoveryContinuationTests() {
  test(
    'unconfirmed image after restart permits explicit text and later receipt recovery',
    () async {
      WidgetsBinding.instance.handleAppLifecycleStateChanged(
        AppLifecycleState.resumed,
      );
      final store = MemoryFriendImagePendingStore();
      final seed = await _seedUnconfirmedImage(store);
      final original = jsonEncode(seed.pending.toJson());
      final posts = <http.Request>[];
      var imageReads = 0, found = false;
      final client = MockClient((r) async {
        if (r.method == 'POST') {
          posts.add(r);
          expect(
            r.url.path,
            '/v1/me/conversations/$audioConversation/message-operations',
          );
          return _independentTextReceipt(r);
        }
        if (r.url.path.contains('/image-message-operations/')) {
          imageReads++;
          expect(r.url.path, endsWith('/${seed.pending.operationID}'));
          return found
              ? imageWire(
                  imageReceipt(seed.pending, seed.metadata, owner: audioOwner),
                )
              : imageWire({}, status: 404);
        }
        return _historyPage([]);
      });
      addTearDown(client.close);
      final c = _controller(
        _Auth(),
        _Audio(),
        client,
        imageEnabled: true,
        imageStore: store,
        visible: () => true,
      );
      await c.initialise();
      expect(c.imageDraft.localBytes, isNull);
      expect(await c.recoverImage(c.imagePending.single), isFalse);
      expect(c.pending, isTrue);
      expect(c.hasPendingTextOrAudio, isFalse);
      expect(c.canSelectImage, isFalse);
      expect(c.canSendImage('新图片'), isFalse);
      expect(c.canRetryImage(c.imagePending.single, '原图片说明'), isFalse);
      expect(c.canSendText, isTrue);
      expect(c.canRecord, isTrue);
      expect(posts, isEmpty);
      expect(await c.sendText('继续发独立文字'), isTrue);
      expect(posts, hasLength(1));
      expect(jsonDecode(posts.single.body)['body'], '继续发独立文字');
      expect(
        jsonDecode(posts.single.body)['operationId'],
        isNot(seed.pending.operationID),
      );
      expect(c.textPending, isEmpty);
      expect(c.imagePending, hasLength(1));
      expect(jsonEncode(c.imagePending.single.toJson()), original);
      expect(
        jsonEncode(
          (await store.read(
            'https://unit.example',
            audioOwner,
          )).single.toJson(),
        ),
        original,
      );
      expect(
        imageReads,
        1,
        reason: 'Independent text must not recover or replay the image.',
      );
      found = true;
      expect(await c.recoverImage(c.imagePending.single), isTrue);
      expect(imageReads, 2);
      expect(c.imagePending, isEmpty);
      expect(await store.read('https://unit.example', audioOwner), isEmpty);
      expect(
        posts,
        hasLength(1),
        reason: 'Receipt recovery never POSTs the image.',
      );
    },
  );

  testWidgets(
    'unconfirmed image permits manual voice but recovery cannot lock an active recording',
    (tester) async {
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      final store = MemoryFriendImagePendingStore();
      final seed = (await tester.runAsync(() => _seedUnconfirmedImage(store)))!;
      final audioStore = MemoryFriendAudioPendingStore(), audio = _Audio();
      var imageReads = 0, voicePosts = 0, found = false;
      final client = MockClient((r) async {
        if (r.method == 'POST') {
          expect(
            r.url.path,
            '/v1/me/conversations/$audioConversation/audio-message-operations',
          );
          voicePosts++;
          final p = (await audioStore.read(
            'https://unit.example',
            audioOwner,
          )).single;
          expect(p.operationID, isNot(seed.pending.operationID));
          return audioWire(audioReceipt(p), 201);
        }
        if (r.url.path.contains('/image-message-operations/')) {
          imageReads++;
          return found
              ? imageWire(
                  imageReceipt(seed.pending, seed.metadata, owner: audioOwner),
                )
              : imageWire({}, status: 404);
        }
        return _historyPage([]);
      });
      addTearDown(client.close);
      final c = _controller(
        _Auth(),
        audio,
        client,
        imageEnabled: true,
        imageStore: store,
        pending: audioStore,
        visible: () => true,
      );
      await tester.runAsync(() async {
        await c.initialise();
        expect(await c.recoverImage(c.imagePending.single), isFalse);
      });
      await _paintThread(tester, c);
      await tester.pump();
      expect(find.text('图片结果仍待核实，可以继续发送文字或语音。'), findsOneWidget);
      expect(find.text('核实这次图片'), findsOneWidget);
      await c.startRecording();
      await tester.pump();
      expect(c.recording, isTrue);
      expect(c.sending, isFalse);
      final recovery = tester.widget<TextButton>(
        find.ancestor(
          of: find.text('核实这次图片'),
          matching: find.byWidgetPredicate((widget) => widget is TextButton),
        ),
      );
      expect(recovery.onPressed, isNull);
      expect(await c.recoverImage(c.imagePending.single), isFalse);
      expect(imageReads, 1);
      expect(c.recording, isTrue);
      expect(c.sending, isFalse);
      await c.stopRecording();
      expect(c.canSendAudio, isTrue);
      expect(voicePosts, 0);
      expect(await tester.runAsync(c.sendAudio), isTrue);
      await tester.pump();
      expect(voicePosts, 1);
      expect(c.imagePending, hasLength(1));
      expect(
        await store.read('https://unit.example', audioOwner),
        hasLength(1),
      );
      expect(imageReads, 1);
      await c.startRecording();
      await c.stopRecording();
      final unsent = c.audioDraft;
      expect(unsent, isNotNull);
      found = true;
      expect(
        await tester.runAsync(() => c.recoverImage(c.imagePending.single)),
        isTrue,
      );
      expect(
        c.audioDraft,
        same(unsent),
        reason: 'Metadata receipt cannot clear a new voice draft.',
      );
      expect(c.imagePending, isEmpty);
      expect(await store.read('https://unit.example', audioOwner), isEmpty);
      expect(voicePosts, 1);
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );

  test(
    'unknown image keeps same-owner voice restoration independent and rejects another owner',
    () async {
      WidgetsBinding.instance.handleAppLifecycleStateChanged(
        AppLifecycleState.resumed,
      );
      final store = MemoryFriendImagePendingStore();
      await _seedUnconfirmedImage(store);
      var posts = 0;
      final client = MockClient((r) async {
        if (r.method == 'POST') posts++;
        return r.url.path.contains('/image-message-operations/')
            ? imageWire({}, status: 404)
            : _historyPage([]);
      });
      addTearDown(client.close);
      final firstAuth = _Auth();
      final first = _controller(
        firstAuth,
        _Audio(),
        client,
        imageEnabled: true,
        imageStore: store,
        visible: () => true,
      );
      await first.initialise();
      expect(await first.recoverImage(first.imagePending.single), isFalse);
      await first.startRecording();
      await first.stopRecording();
      firstAuth.change(null, null, expired: audioOwner);
      final retained = first.retainAudioDraft();
      expect(retained, isNotNull);
      first.dispose();
      final renewedAuth = _Auth()..credential = 'Bearer renewed-same-owner';
      final renewed = _controller(
        renewedAuth,
        _Audio(),
        client,
        imageEnabled: true,
        imageStore: store,
        visible: () => true,
      );
      await renewed.initialise();
      expect(renewed.pending, isTrue);
      expect(renewed.hasPendingTextOrAudio, isFalse);
      expect(
        renewed.restoreAudioDraft(
          retained!,
          owner: audioOwner,
          conversationID: audioConversation,
        ),
        isTrue,
      );
      expect(renewed.audioDraft!.bytes, orderedEquals(retained.bytes));
      expect(renewed.canSendAudio, isTrue);
      expect(renewed.imagePending, hasLength(1));
      final otherAuth = _Auth()
        ..change('66666666-6666-4666-8666-666666666666', 'Bearer other-owner');
      final other = _controller(
        otherAuth,
        _Audio(),
        client,
        imageEnabled: true,
        imageStore: store,
        visible: () => true,
      );
      await other.initialise();
      expect(other.imagePending, isEmpty);
      expect(
        other.restoreAudioDraft(
          retained,
          owner: audioOwner,
          conversationID: audioConversation,
        ),
        isFalse,
      );
      expect(other.audioDraft, isNull);
      expect(
        posts,
        0,
        reason: 'Restoration preserves a draft, never approval.',
      );
    },
  );

  test(
    'late image receipt after account A-B-A cannot retire journal or enable old actions',
    () async {
      WidgetsBinding.instance.handleAppLifecycleStateChanged(
        AppLifecycleState.resumed,
      );
      final store = MemoryFriendImagePendingStore();
      final seed = await _seedUnconfirmedImage(store);
      final late = Completer<http.Response>();
      var imageReads = 0, posts = 0;
      final auth = _Auth(), audio = _Audio();
      final client = MockClient((r) async {
        if (r.method == 'POST') posts++;
        if (r.url.path.contains('/image-message-operations/')) {
          if (++imageReads == 1) return imageWire({}, status: 404);
          return late.future;
        }
        return _historyPage([]);
      });
      addTearDown(client.close);
      final c = _controller(
        auth,
        audio,
        client,
        imageEnabled: true,
        imageStore: store,
        visible: () => true,
      );
      await c.initialise();
      expect(await c.recoverImage(c.imagePending.single), isFalse);
      final original = c.imagePending.single;
      final recovery = c.recoverImage(original);
      await Future<void>.delayed(Duration.zero);
      expect(imageReads, 2);
      auth.change('66666666-6666-4666-8666-666666666666', 'Bearer owner-b');
      auth.change(audioOwner, 'Bearer synthetic-a');
      late.complete(
        imageWire(imageReceipt(seed.pending, seed.metadata, owner: audioOwner)),
      );
      expect(await recovery, isFalse);
      expect(c.current, isFalse);
      expect(c.imagePending, isEmpty);
      expect(c.canSendText, isFalse);
      expect(c.canRecord, isFalse);
      expect(await c.sendText('旧账户动作'), isFalse);
      await c.startRecording();
      expect(audio.starts, 0);
      expect(posts, 0);
      expect(
        await store.read('https://unit.example', audioOwner),
        hasLength(1),
      );
    },
  );
}

void main() {
  imageRecoveryContinuationTests();

  test('restart has metadata recovery only; image bytes and retry grant are absent', () async {
    WidgetsBinding.instance.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    final bytes = await imageFixtureBytes(), store = MemoryFriendImagePendingStore();
    final pending = PendingFriendImage.review(
        operationID: '44444444-4444-4444-8444-444444444444',
        conversationID: audioConversation, recipientID: audioPeer,
        draft: FriendImageDraft(bytes, width: 4, height: 4), caption: 'caption');
    await store.write('https://unit.example', audioOwner, pending);
    var posts = 0, reads = 0;
    final client = MockClient((r) async {
      if (r.method == 'POST') posts++;
      if (r.url.path.contains('/image-message-operations/')) {
        reads++; return imageWire({}, status: 404);
      }
      return _historyPage([]);
    });
    addTearDown(client.close);
    final c = _controller(_Auth(), _Audio(), client, imageEnabled: true,
        imageStore: store, visible: () => true);
    await c.initialise();
    expect(c.imagePending, hasLength(1)); expect(c.imageDraft.localBytes, isNull);
    final recovered = c.imagePending.single;
    expect(c.canRetryImage(recovered, 'caption'), isFalse);
    expect(await c.retryImage(recovered, 'caption', draftCurrent: () => true), isFalse);
    expect(await c.recoverImage(recovered), isFalse);
    expect(posts, 0); expect(reads, 1); expect(c.imagePending, hasLength(1));
  });

  test('bounded per-message thumbnail/full cache retires exact entries on leaving recipient', () async {
    WidgetsBinding.instance.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    final bytes = await imageFixtureBytes(), metadata = imageMetadata(bytes);
    final items = [for (var i = 1; i <= 9; i++) {
      ..._historyMessage('66666666-6666-4666-8666-${i.toString().padLeft(12, '0')}'),
      'createdAt': DateTime.utc(2026, 10, 12).add(Duration(seconds: i)).toIso8601String(),
      'image': metadata.toJson(),
    }];
    final client = MockClient((r) async {
      if (!r.url.path.endsWith('/image')) return _historyPage(items);
      return http.Response.bytes(bytes, 200, headers: {
        'content-type': 'image/png', 'content-length': bytes.length.toString(),
        'x-content-sha256': metadata.full.sha256, 'x-image-width': '4', 'x-image-height': '4',
        'x-image-variant': r.url.queryParameters['variant']!,
        'cache-control': 'no-store', 'x-content-type-options': 'nosniff',
      });
    });
    addTearDown(client.close);
    final c = _controller(_Auth(), _Audio(), client, imageEnabled: true, visible: () => true);
    await c.initialise();
    final first = await c.receiveImage(c.messages.first, FriendImageVariant.thumbnail);
    expect(first, isNotNull);
    for (final message in c.messages.skip(1)) {
      expect(await c.receiveImage(message, FriendImageVariant.thumbnail), isNotNull);
    }
    expect(first!.current, isFalse, reason: 'the ninth thumbnail evicts the oldest of eight');
    final full = await c.receiveImage(c.messages.first, FriendImageVariant.full);
    final replacement = await c.receiveImage(c.messages.last, FriendImageVariant.full);
    expect(full!.current, isFalse); expect(replacement!.current, isTrue);
    c.retireImageView();
    expect(replacement.current, isFalse);
  });


  test('image selection has no HTTP and excludes audio until local removal', () async {
    WidgetsBinding.instance.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    var calls = 0;
    final client = MockClient((_) async { calls++; return _historyPage([]); });
    addTearDown(client.close);
    final c = _controller(_Auth(), _Audio(), client, imageEnabled: true,
        imagePicker: TestMomentImagePicker(await imageFixtureBytes()), visible: () => true);
    await c.initialise();
    final before = calls;
    await c.selectImage(gestureCurrent: () => true);
    expect(c.imageDraft.hasPixels, isTrue); expect(calls, before);
    expect(c.canRecord, isFalse); expect(c.canSendText, isFalse);
    expect(c.canSendImage(''), isTrue);
    c.imageDraft.removeLocal();
    expect(c.canRecord, isTrue); expect(c.canSendText, isTrue);
  });

  test('unknown image operation never auto replays; 404 stays unknown and explicit retry keeps operation', () async {
    WidgetsBinding.instance.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    final bytes = await imageFixtureBytes();
    var posts = 0, operationReads = 0;
    String? operationID;
    final client = MockClient((r) async {
      if (r.url.path.endsWith('/messages/page')) return _historyPage([]);
      if (r.url.path.endsWith('/image-message-operations') && r.method == 'POST') {
        posts++;
        final input = jsonDecode(r.body) as Map<String, dynamic>;
        operationID ??= input['operationId'];
        expect(input['operationId'], operationID);
        if (posts == 1) return imageWire({}, status: 503);
        final draft = FriendImageDraft(base64Decode(input['contentBase64']), width: 4, height: 4);
        final pending = PendingFriendImage.review(operationID: input['operationId'],
            conversationID: audioConversation, recipientID: audioPeer,
            draft: draft, caption: input['caption']);
        return imageWire(imageReceipt(pending, imageMetadata(bytes), owner: audioOwner), status: 201);
      }
      if (r.url.path.contains('/image-message-operations/')) {
        operationReads++; return imageWire({}, status: 404);
      }
      if (r.url.path.endsWith('/conversations/$audioConversation')) {
        return imageWire({'id': audioConversation, 'otherAccountId': audioPeer,
          'otherName': '测试联系人', 'createdAt': '2026-10-12T01:02:03Z', 'unreadCount': 0});
      }
      return http.Response('{}', 404);
    });
    addTearDown(client.close);
    final store = MemoryFriendImagePendingStore();
    final c = _controller(_Auth(), _Audio(), client, imageEnabled: true,
        imagePicker: TestMomentImagePicker(bytes), imageStore: store, visible: () => true);
    await c.initialise(); await c.selectImage(gestureCurrent: () => true);
    expect(await c.sendImage('caption', draftCurrent: () => true), isFalse);
    await Future<void>.delayed(Duration.zero);
    expect(posts, 1); expect(c.imagePending, hasLength(1));
    final pending = c.imagePending.single;
    expect(await c.recoverImage(pending), isFalse);
    await Future<void>.delayed(Duration.zero);
    expect(posts, 1); expect(c.imagePending, hasLength(1));
    expect(c.error, contains('未确认'));
    expect(await c.retryImage(pending, 'edited', draftCurrent: () => true), isFalse);
    expect(posts, 1);
    expect(await c.retryImage(pending, 'caption', draftCurrent: () => true), isTrue);
    await Future<void>.delayed(Duration.zero);
    expect(posts, 2); expect(operationReads, 2);
    expect(c.imagePending, isEmpty); expect(c.imageDraft.localBytes, isNull);
  });

  test('late successful image send preserves edited text approval and its original image draft', () async {
    WidgetsBinding.instance.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    final bytes = await imageFixtureBytes(), response = Completer<http.Response>();
    late PendingFriendImage pending;
    final client = MockClient((r) async {
      if (r.method == 'GET') return _historyPage([]);
      final input = jsonDecode(r.body) as Map<String, dynamic>;
      pending = PendingFriendImage.review(operationID: input['operationId'],
          conversationID: audioConversation, recipientID: audioPeer,
          draft: FriendImageDraft(base64Decode(input['contentBase64']), width: 4, height: 4),
          caption: input['caption']);
      return response.future;
    });
    addTearDown(client.close);
    final c = _controller(_Auth(), _Audio(), client, imageEnabled: true,
        imagePicker: TestMomentImagePicker(bytes), visible: () => true);
    await c.initialise(); await c.selectImage(gestureCurrent: () => true);
    var revision = 0;
    final sending = c.sendImage('original', draftCurrent: () => revision == 0);
    await Future<void>.delayed(Duration.zero);
    revision++;
    response.complete(imageWire(imageReceipt(pending, imageMetadata(bytes), owner: audioOwner), status: 201));
    expect(await sending, isTrue);
    expect(c.imageDraft.hasPixels, isTrue); expect(c.imagePending, isEmpty);
  });

  test('new owner retires a late selected image without delivering any pixels', () async {
    WidgetsBinding.instance.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    final picker = TestMomentImagePicker(await imageFixtureBytes())
      ..pending = Completer<MomentImageSelection?>();
    final auth = _Auth(), client = MockClient((_) async => _historyPage([]));
    addTearDown(client.close);
    final c = _controller(auth, _Audio(), client, imageEnabled: true,
        imagePicker: picker, visible: () => true);
    await c.initialise();
    final selecting = c.selectImage(gestureCurrent: () => true);
    await Future<void>.delayed(Duration.zero);
    auth.change(audioPeer, 'Bearer synthetic-b');
    picker.pending!.complete(MomentImageSelection(picker.bytes));
    await selecting;
    expect(c.current, isFalse); expect(c.imageDraft.localBytes, isNull);
  });

  TestWidgetsFlutterBinding.ensureInitialized();
  testWidgets(
    'latest 20 then explicit earlier pages reach full history without extra ACK',
    (tester) async {
      final requests = <http.Request>[];
      final c = _controller(
        _Auth(),
        _Audio(),
        MockClient((r) async {
          requests.add(r);
          if (r.method == 'POST') return audioWire({});
          expect(
            r.url.path,
            '/v1/me/conversations/$audioConversation/messages/page',
          );
          expect(r.url.queryParameters['limit'], '20');
          return switch (r.url.queryParameters['cursor']) {
            null => chatPageWire([
              for (var n = 26; n <= 45; n++) pageMessage(n),
            ], nextCursor: 'older_25'),
            'older_25' => chatPageWire([
              for (var n = 6; n <= 25; n++) pageMessage(n),
            ], nextCursor: 'older_5'),
            'older_5' => chatPageWire([
              for (var n = 1; n <= 5; n++) pageMessage(n),
            ]),
            _ => throw StateError('Unrequested page'),
          };
        }),
        visible: () => true,
      );
      await _paintThread(tester, c);
      await c.initialise();
      await tester.pump();
      expect(c.messages, hasLength(20));
      expect(c.messages.first.id, pageMessageID(26));
      expect(requests.where((r) => r.method == 'GET'), hasLength(1));
      expect(requests.where((r) => r.method == 'POST'), hasLength(1));
      expect(find.byKey(const Key('friend-load-earlier')), findsOneWidget);

      await tester.tap(find.byKey(const Key('friend-load-earlier')));
      await tester.pump();
      expect(c.messages, hasLength(40));
      expect(c.messages.first.id, pageMessageID(6));
      await c.loadEarlier();
      await tester.pump();
      expect(c.messages.map((m) => m.id), [
        for (var n = 1; n <= 45; n++) pageMessageID(n),
      ]);
      expect(c.hasEarlierMessages, isFalse);
      expect(find.byKey(const Key('friend-load-earlier')), findsNothing);
      await c.loadEarlier();
      await c.acknowledgeVisible();
      expect(requests.where((r) => r.method == 'GET'), hasLength(3));
      final acks = requests.where((r) => r.method == 'POST').toList();
      expect(acks, hasLength(1));
      expect(jsonDecode(acks.single.body), {
        'throughMessageId': pageMessageID(45),
      });
    },
  );

  test(
    'one earlier request at a time and offline retry preserves newest history',
    () async {
      final delayed = Completer<http.Response>();
      var reads = 0;
      final c = _controller(
        _Auth(),
        _Audio(),
        MockClient((r) async {
          reads++;
          if (r.url.queryParameters['cursor'] == null)
            return chatPageWire([pageMessage(5)], nextCursor: 'older_4');
          if (reads == 2) return delayed.future;
          return chatPageWire([pageMessage(4)]);
        }),
      );
      await c.initialise();
      final earlier = c.loadEarlier();
      await Future<void>.delayed(Duration.zero);
      expect(c.loadingEarlier, isTrue);
      await c.loadEarlier();
      expect(reads, 2);
      delayed.completeError(http.ClientException('synthetic offline'));
      await earlier;
      expect(c.loadingEarlier, isFalse);
      expect(c.messages.single.id, pageMessageID(5));
      expect(c.hasEarlierMessages, isTrue);
      expect(c.error, contains('连接中断'));
      await c.loadEarlier();
      expect(c.error, isNull);
      expect(c.messages.map((m) => m.id), [pageMessageID(4), pageMessageID(5)]);
    },
  );

  for (final status in [403, 404]) {
    testWidgets(
      'denied earlier HTTP$status clears all retained private rows and pagination',
      (tester) async {
        var gets = 0, posts = 0;
        final c = _controller(
          _Auth(),
          _Audio(),
          MockClient((r) async {
            if (r.method != 'GET') {
              posts++;
              return audioWire({});
            }
            if (++gets == 1)
              return chatPageWire([pageMessage(5)], nextCursor: 'older_4');
            return http.Response('{}', status);
          }),
        );
        await _paintThread(tester, c);
        await c.initialise();
        await tester.pump();
        await c.loadEarlier();
        await tester.pump();
        expect(c.messages, isEmpty);
        expect(c.historyAvailable, isFalse);
        expect(c.hasEarlierMessages, isFalse);
        expect(c.loadingEarlier, isFalse);
        expect(c.canSendText, isFalse);
        expect(await c.sendText('不会发送'), isFalse);
        expect(posts, 0);
        expect(find.text('还没有消息。'), findsNothing);
        expect(find.textContaining('登录'), findsNothing);
        expect(c.error, contains(status == 403 ? '访问权限' : '不可访问'));
      },
    );
  }

  test(
    'late earlier success cannot restore history after newer permission denial',
    () async {
      final delayed = Completer<http.Response>();
      var reads = 0;
      final c = _controller(
        _Auth(),
        _Audio(),
        MockClient((r) async {
          reads++;
          if (reads == 1)
            return chatPageWire([pageMessage(5)], nextCursor: 'older_4');
          if (r.url.queryParameters['cursor'] != null) return delayed.future;
          return http.Response('{}', 404);
        }),
      );
      await c.initialise();
      final older = c.loadEarlier();
      await Future<void>.delayed(Duration.zero);
      await c.load();
      expect(c.messages, isEmpty);
      delayed.complete(chatPageWire([pageMessage(4)]));
      await older;
      expect(c.messages, isEmpty);
      expect(c.historyAvailable, isFalse);
      expect(c.error, contains('不可访问'));
      expect(c.hasEarlierMessages, isFalse);
    },
  );

  test(
    'late earlier success cannot survive another account or same-token ABA',
    () async {
      for (final aba in [false, true]) {
        final auth = _Auth(), delayed = Completer<http.Response>();
        final c = _controller(
          auth,
          _Audio(),
          MockClient((r) async {
            if (r.url.queryParameters['cursor'] != null) return delayed.future;
            return chatPageWire([pageMessage(5)], nextCursor: 'older_4');
          }),
        );
        await c.initialise();
        final older = c.loadEarlier();
        await Future<void>.delayed(Duration.zero);
        auth.change(audioPeer, 'Bearer synthetic-b');
        if (aba) auth.change(audioOwner, 'Bearer synthetic-a');
        delayed.complete(chatPageWire([pageMessage(4)]));
        await older;
        expect(c.messages, isEmpty);
        expect(c.historyAvailable, isFalse);
        expect(c.hasEarlierMessages, isFalse);
      }
    },
  );

  test(
    'overlapping earlier rows are rejected without changing retained history',
    () async {
      var reads = 0;
      final c = _controller(
        _Auth(),
        _Audio(),
        MockClient((r) async {
          if (++reads == 1)
            return chatPageWire([pageMessage(5)], nextCursor: 'older_4');
          return chatPageWire([pageMessage(5)]);
        }),
      );
      await c.initialise();
      await c.loadEarlier();
      expect(c.messages.single.id, pageMessageID(5));
      expect(c.hasEarlierMessages, isTrue);
      expect(c.error, contains('来源无法核实'));
    },
  );
  test(
    'guest does not request history or record; current member records then explicitly sends',
    () async {
      final auth = _Auth(), audio = _Audio(), calls = <String>[];
      final client = MockClient((r) async {
        calls.add('${r.method} ${r.url.path}');
        if (r.method == 'POST') {
          // The controller stores the operation before the HTTP call.
          final body = r.body;
          expect(body, contains('contentBase64'));
          return http.Response('{}', 503);
        }
        return _historyPage([]);
      });
      auth.change(null, null);
      final c = _controller(auth, audio, client);
      await c.initialise();
      await c.startRecording();
      expect(calls, isEmpty);
      expect(audio.starts, 0);
      // Each controller is immutable to its original identity. A new member
      // context uses a new controller, as the Now host does.
    },
  );
  test(
    'stopped recording stays a preview; successful explicit send clears it',
    () async {
      final auth = _Auth(),
          audio = _Audio(),
          pending = MemoryFriendAudioPendingStore();
      var sends = 0;
      final c = _controller(
        auth,
        audio,
        MockClient((r) async {
          if (r.method == 'POST') {
            sends++;
            final p = (await pending.read(
              'https://unit.example',
              audioOwner,
            )).single;
            return audioWire(audioReceipt(p), 201);
          }
          return _historyPage([]);
        }),
        pending: pending,
      );
      await c.initialise();
      await c.startRecording();
      await c.stopRecording();
      expect(c.audioPhase, FriendAudioPhase.preview);
      expect(c.canSendAudio, isTrue);
      expect(sends, 0);
      expect(await c.sendAudio(), isTrue);
      expect(sends, 1);
      expect(c.audioDraft, isNull);
      expect(c.audioPending, isEmpty);
    },
  );
  test(
    'unknown outcome keeps exact operation; refresh and recover never auto resend',
    () async {
      final auth = _Auth(), audio = _Audio();
      var sends = 0, recoveries = 0;
      final c = _controller(
        auth,
        audio,
        MockClient((r) async {
          if (r.method == 'POST') {
            sends++;
            return http.Response('{}', 503);
          }
          if (r.url.path.contains('audio-message-operations')) {
            recoveries++;
            return http.Response('{}', 404);
          }
          return _historyPage([]);
        }),
      );
      await c.initialise();
      await c.startRecording();
      await c.stopRecording();
      expect(await c.sendAudio(), isFalse);
      final p = c.audioPending.single;
      expect(c.audioDraft, isNotNull);
      expect(c.canSendAudio, isFalse);
      await c.load();
      expect(sends, 1);
      expect(await c.recoverAudio(p), isFalse);
      expect(sends, 1);
      expect(recoveries, 1);
      expect(c.audioPending.single, same(p));
      expect(await c.retryAudio(p), isFalse);
      expect(sends, 2);
      expect(recoveries, 2);
      expect(c.audioPending.single.operationID, p.operationID);
    },
  );
  test(
    'account switch during native stop discards late recording and prior history',
    () async {
      final auth = _Auth(),
          audio = _Audio()..stopping = Completer<FriendAudioDraft>();
      final c = _controller(
        auth,
        audio,
        MockClient((_) async => _historyPage([])),
      );
      await c.initialise();
      await c.startRecording();
      final stopping = c.stopRecording();
      auth.change(audioPeer, 'Bearer synthetic-b');
      audio.stopping!.complete(audioFixture());
      await stopping;
      expect(c.current, isFalse);
      expect(c.audioDraft, isNull);
      expect(c.messages, isEmpty);
      expect(c.audioPending, isEmpty);
      expect(audio.cancelled, isNotEmpty);
      expect(await c.sendAudio(), isFalse);
    },
  );
  test(
    '401 asks current identity to reauthenticate once and never sends after it returns',
    () async {
      final auth = _Auth(), audio = _Audio();
      var sends = 0, rejections = 0;
      final c = _controller(
        auth,
        audio,
        MockClient((r) async {
          if (r.method == 'POST') {
            sends++;
            return http.Response('{}', 401);
          }
          return _historyPage([]);
        }),
        rejected: (token) {
          rejections++;
          expect(token, 'Bearer synthetic-a');
          auth.change(null, null, expired: audioOwner);
        },
      );
      await c.initialise();
      await c.startRecording();
      await c.stopRecording();
      expect(await c.sendAudio(), isFalse);
      expect(rejections, 1);
      expect(sends, 1);
      expect(c.messages, isEmpty);
      auth.change(audioOwner, 'Bearer synthetic-renewed');
      await Future<void>.delayed(Duration.zero);
      expect(sends, 1);
      expect(c.audioDraft, isNotNull);
      expect(c.audioPending, hasLength(1));
    },
  );
  test('late old history cannot clear the newer session history', () async {
    final auth = _Auth(), audio = _Audio(), old = Completer<http.Response>();
    var reads = 0;
    final c = _controller(
      auth,
      audio,
      MockClient((r) async {
        reads++;
        if (reads == 1) return old.future;
        return _historyPage([
          {
            'id': audioMessage,
            'conversationId': audioConversation,
            'senderAccountId': audioPeer,
            'speakerKind': 'human',
            'body': '新会话可见消息',
            'createdAt': '2026-10-11T01:02:03Z',
          },
        ]);
      }),
    );
    final loading = c.initialise();
    await Future<void>.delayed(Duration.zero);
    auth.change(audioOwner, 'Bearer synthetic-renewed');
    await Future<void>.delayed(Duration.zero);
    expect(c.messages.single.body, '新会话可见消息');
    old.complete(_historyPage([]));
    await loading;
    expect(c.messages.single.body, '新会话可见消息');
  });
  test(
    'logout clears private recording; expired owner suspension hides it until same owner returns',
    () async {
      final auth = _Auth(), audio = _Audio();
      final c = _controller(
        auth,
        audio,
        MockClient((_) async => _historyPage([])),
      );
      await c.initialise();
      expect(c.canRecord, isTrue);
      await c.startRecording();
      await c.stopRecording();
      expect(c.elapsedLabel, '0:01');
      expect(c.canRecord, isFalse);
      final original = c.audioDraft;
      auth.change(null, null, expired: audioOwner);
      expect(c.audioDraft, isNull);
      expect(c.audioPending, isEmpty);
      expect(c.retainAudioDraft(), same(original));
      auth.change(audioOwner, 'Bearer synthetic-renewed');
      await Future<void>.delayed(Duration.zero);
      expect(c.audioDraft, same(original));
      auth.change(null, null);
      expect(c.audioDraft, isNull);
      expect(c.retainAudioDraft(), isNull);
      auth.change(audioOwner, 'Bearer synthetic-again');
      await Future<void>.delayed(Duration.zero);
      expect(c.current, isFalse);
      expect(c.audioDraft, isNull);
    },
  );
  test(
    'denied history does not enable recording and retries the authorised read',
    () async {
      for (final status in [403, 404, 503]) {
        final auth = _Auth(), audio = _Audio();
        var available = false, reads = 0;
        final c = _controller(
          auth,
          audio,
          MockClient((_) async {
            reads++;
            return available ? _historyPage([]) : http.Response('{}', status);
          }),
        );
        await c.initialise();
        expect(c.canRecord, isFalse);
        await c.startRecording();
        expect(audio.starts, 0);
        expect(reads, 1);
        expect(
          c.error,
          contains(switch (status) {
            403 => '访问权限',
            404 => '不可访问',
            _ => '服务暂不可用',
          }),
        );
        available = true;
        await c.load();
        expect(reads, 2);
        expect(c.canRecord, isTrue);
        expect(c.error, isNull);
      }
    },
  );
  testWidgets(
    'visible authorised history acknowledges newest message after its frame only',
    (tester) async {
      final auth = _Auth(), audio = _Audio(), calls = <http.Request>[];
      var historyWasExposed = false;
      late FriendConversationController c;
      c = _controller(
        auth,
        audio,
        MockClient((r) async {
          calls.add(r);
          if (r.method == 'POST') {
            expect(historyWasExposed, isTrue);
            expect(c.loading, isFalse);
            expect(r.url.path, '/v1/me/conversations/$audioConversation/read');
            expect(jsonDecode(r.body), {'throughMessageId': _newerMessage});
            expect(r.headers['authorization'], 'Bearer synthetic-a');
            return audioWire({});
          }
          return _historyPage([
            _historyMessage(audioMessage),
            _historyMessage(_newerMessage),
          ]);
        }),
        visible: () => true,
      );
      c.addListener(() {
        if (!c.loading && c.messages.length == 2) historyWasExposed = true;
      });
      await _paintThread(tester, c);
      await c.initialise();
      expect(calls.where((r) => r.method == 'POST'), isEmpty);
      await tester.pump();
      expect(calls.where((r) => r.method == 'POST'), hasLength(1));
      await c.acknowledgeVisible();
      await c.load();
      await tester.pump();
      expect(calls.where((r) => r.method == 'POST'), hasLength(1));
      expect(calls.any((r) => r.url.path.contains('notification')), isFalse);
    },
  );
  testWidgets(
    'hidden history and closing or changing recipient before paint do not consume unread',
    (tester) async {
      for (final hiddenByDefault in [true, false]) {
        final auth = _Auth(), audio = _Audio();
        var selected = audioConversation, reads = 0;
        final c = _controller(
          auth,
          audio,
          MockClient((r) async {
            if (r.method == 'POST') {
              reads++;
              return audioWire({});
            }
            return _historyPage([_historyMessage(audioMessage)]);
          }),
          visible: hiddenByDefault ? null : () => selected == audioConversation,
        );
        await _paintThread(tester, c);
        await c.initialise();
        selected = '';
        await tester.pump();
        await c.acknowledgeVisible();
        expect(reads, 0);
        expect(c.messages, hasLength(1));
        if (!hiddenByDefault) {
          selected = audioConversation;
          await c.load();
          await tester.pump();
          expect(reads, 1);
        }
      }
    },
  );
  testWidgets(
    'late old-session history never acknowledges the old message or credential',
    (tester) async {
      final auth = _Auth(), audio = _Audio(), old = Completer<http.Response>();
      final acknowledgements = <http.Request>[];
      var histories = 0;
      final c = _controller(
        auth,
        audio,
        MockClient((r) async {
          if (r.method == 'POST') {
            acknowledgements.add(r);
            return audioWire({});
          }
          if (++histories == 1) return old.future;
          return _historyPage([_historyMessage(_newerMessage)]);
        }),
        visible: () => true,
      );
      await _paintThread(tester, c);
      final loading = c.initialise();
      await tester.pump();
      auth.change(audioOwner, 'Bearer synthetic-renewed');
      await tester.pump();
      await tester.pump();
      expect(acknowledgements, hasLength(1));
      expect(
        acknowledgements.single.headers['authorization'],
        'Bearer synthetic-renewed',
      );
      expect(jsonDecode(acknowledgements.single.body), {
        'throughMessageId': _newerMessage,
      });
      old.complete(_historyPage([_historyMessage(audioMessage)]));
      await loading;
      await tester.pump();
      expect(acknowledgements, hasLength(1));
      expect(c.messages.single.id, _newerMessage);
    },
  );
  testWidgets(
    'denied current read acknowledgement retires history and cannot enable sending',
    (tester) async {
      for (final status in [403, 404]) {
        final auth = _Auth(), audio = _Audio();
        var reads = 0;
        final c = _controller(
          auth,
          audio,
          MockClient((r) async {
            if (r.method == 'POST') {
              reads++;
              return http.Response('{}', status);
            }
            return _historyPage([_historyMessage(audioMessage)]);
          }),
          visible: () => true,
        );
        await _paintThread(tester, c);
        await c.initialise();
        await tester.pump();
        expect(reads, 1);
        expect(c.messages, isEmpty);
        expect(c.canRecord, isFalse);
        expect(await c.sendText('不会自动发送'), isFalse);
        expect(c.error, contains(status == 403 ? '访问权限' : '不可访问'));
      }
    },
  );
  testWidgets(
    '401 acknowledgement invalidates its exact owner once and performs no message send',
    (tester) async {
      final auth = _Auth(), audio = _Audio(), writes = <String>[];
      var rejected = 0;
      final c = _controller(
        auth,
        audio,
        MockClient((r) async {
          if (r.method == 'POST') {
            writes.add(r.url.path);
            return http.Response('{}', 401);
          }
          return _historyPage([_historyMessage(audioMessage)]);
        }),
        visible: () => true,
        rejected: (token) {
          rejected++;
          expect(token, 'Bearer synthetic-a');
          auth.change(null, null, expired: audioOwner);
        },
      );
      await _paintThread(tester, c);
      await c.initialise();
      await tester.pump();
      await c.acknowledgeVisible();
      expect(rejected, 1);
      expect(writes, ['/v1/me/conversations/$audioConversation/read']);
      expect(c.messages, isEmpty);
      expect(c.current, isFalse);
      expect(c.canRecord, isFalse);
    },
  );
  testWidgets(
    'late acknowledgement denial cannot replace newer history, close state or another account',
    (tester) async {
      for (final replacement in ['history', 'close', 'account']) {
        final auth = _Auth(),
            audio = _Audio(),
            old = Completer<http.Response>();
        var visible = true, histories = 0, acknowledgements = 0;
        final c = _controller(
          auth,
          audio,
          MockClient((r) async {
            if (r.method == 'POST') {
              if (++acknowledgements == 1) return old.future;
              return audioWire({});
            }
            return _historyPage([
              _historyMessage(++histories == 1 ? audioMessage : _newerMessage),
            ]);
          }),
          visible: () => visible,
        );
        await _paintThread(tester, c);
        await c.initialise();
        await tester.pump();
        expect(acknowledgements, 1);
        if (replacement == 'history') {
          await c.load();
          await tester.pump();
          expect(acknowledgements, 2);
        } else if (replacement == 'close') {
          visible = false;
        } else {
          auth.change(audioPeer, 'Bearer synthetic-b');
        }
        old.complete(http.Response('{}', 403));
        await tester.pump();
        expect(c.error, replacement == 'account' ? isNotNull : isNull);
        if (replacement == 'history') {
          expect(c.messages.single.id, _newerMessage);
          expect(c.canRecord, isTrue);
        } else if (replacement == 'close') {
          expect(c.messages.single.id, audioMessage);
        } else {
          expect(c.messages, isEmpty);
          expect(c.current, isFalse);
        }
      }
    },
  );
}
