import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/workspace/chat_message_pending_store.dart';
import 'package:birdtie_client/src/workspace/connection_request_review_pending_store.dart';
import 'package:birdtie_client/src/workspace/connection_request_decision_operation.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/friend_audio.dart';
import 'package:birdtie_client/src/workspace/friend_conversation_controller.dart';
import 'package:birdtie_client/src/workspace/friend_conversation_thread.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const owner = '11111111-1111-4111-8111-111111111111';
const peer = '22222222-2222-4222-8222-222222222222';
const conversationID = '33333333-3333-4333-8333-333333333333';
const stamp = '2026-10-14T00:00:00Z';

class _Auth extends BirdtieAuthController {
  _Auth() : super(client: MockClient((_) async => http.Response('{}', 404)));
  bool active = true;
  @override
  String? get accountID => active ? owner : null;
  @override
  String? get authorizationHeader => active ? 'Bearer synthetic163' : null;
  @override
  bool get signedIn => active;
  void retire() {
    active = false;
    notifyListeners();
  }
}

class _NoAudio extends FriendAudioPort {
  @override
  bool get supported => false;
  @override
  Future<void> start(String id, ValueChanged<FriendAudioPhase> phase) async =>
      throw StateError('No recording');
  @override
  Future<FriendAudioDraft> stop(String id) async =>
      throw StateError('No recording');
  @override
  Future<void> cancel(String id) async {}
  @override
  Future<void> play(
    String id,
    FriendAudioDraft draft,
    VoidCallback finished,
  ) async => throw StateError('No audio');
  @override
  Future<void> stopPlayback(String id) async {}
  @override
  void dispose() {}
}

Map<String, Object?> message(int sequence, String kind, String body) => {
  'id': '44444444-4444-4444-8444-${sequence.toString().padLeft(12, '0')}',
  'conversationId': conversationID,
  'senderAccountId': peer,
  'speakerKind': kind == 'SYSTEM' ? 'system' : 'human',
  'body': body,
  'createdAt': stamp,
  'kind': kind,
  'origin': 'LEGACY_IMPORT',
  'sequence': sequence,
  if (kind == 'STICKER')
    'sticker': {'packId': 'civu-bird-v1', 'stickerId': 'hello'},
  if (kind == 'ENTITY') 'entity': {'type': 'moment', 'available': false},
};

Map<String, Object?> group() => {
  'id': conversationID,
  'otherAccountId': '',
  'otherName': '合成原私人群聊',
  'kind': 'PERSONAL_GROUP',
  'origin': 'LEGACY_IMPORT',
  'canSend': false,
  'createdAt': stamp,
  'unreadCount': 3,
};
http.Response reply(Object data) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': data})),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

void main() {
  test(
    'source kinds and sequence preserve empty entity and unknown request expiry',
    () async {
      final rows = [
        message(1, 'TEXT', '原文字'),
        message(2, 'SYSTEM', '原系统提示'),
        message(4, 'STICKER', '[栖游鸟·你好]'),
        message(8, 'ENTITY', ''),
      ];
      final source = ConnectionSource(
        authorizationHeader: () => 'Bearer synthetic163',
        apiBaseUrl: 'https://unit.example',
        client: MockClient(
          (r) async => r.url.path.endsWith('/messages/page')
              ? reply({'items': rows, 'nextCursor': null})
              : reply(group()),
        ),
      );
      addTearDown(source.dispose);
      final current = await source.conversation(conversationID);
      expect(current.personalGroup, isTrue);
      expect(current.otherAccountId, isEmpty);
      expect(current.canSend, isFalse);
      final page = await source.messagesPage(conversationID);
      expect(page.items.map((v) => v.sequence), [1, 2, 4, 8]);
      expect(page.items[1].system, isTrue);
      expect(page.items[2].stickerID, 'hello');
      expect(page.items.last.body, isEmpty);
      expect(page.items.last.entity!.available, isFalse);
      final request = ContactRequest.fromReviewJson({
        'id': conversationID,
        'direction': 'incoming',
        'otherAccountId': peer,
        'otherName': 'Birdtie 成员',
        'cityId': '',
        'note': '',
        'scope': 'friend',
        'state': 'pending',
        'expiresAt': null,
        'createdAt': stamp,
        'origin': 'LEGACY_IMPORT',
      });
      expect(request.expiresAt, isNull);
      expect(request.state, 'pending');
      final pending = PendingConnectionReview(
        ownerID: owner,
        requestID: conversationID,
        referenceID: '55555555-5555-4555-8555-555555555555',
        action: 'accept',
        scope: 'friend',
        direction: 'incoming',
        observedAt: DateTime.parse(stamp),
        requestCreatedAt: DateTime.parse(stamp),
        requestExpiresAt: null,
        origin: 'LEGACY_IMPORT',
        operationID: '66666666-6666-4666-8666-666666666666',
        requestDigest: connectionDecisionDigest(
          owner,
          conversationID,
          'accept',
        ),
      );
      expect(
        PendingConnectionReview.fromJson(pending.toJson()).requestExpiresAt,
        isNull,
      );
      expect(
        () => PendingConnectionReview.fromJson(
          {...pending.toJson()}..remove('origin'),
        ),
        throwsFormatException,
      );
    },
  );

  testWidgets(
    'existing thread renders original system/sticker history and retires it on owner loss',
    (tester) async {
      final auth = _Auth();
      final rows = [
        message(1, 'SYSTEM', '原系统提示'),
        message(2, 'STICKER', '[栖游鸟·你好]'),
      ];
      final client = MockClient(
        (r) async => r.url.path.endsWith('/messages/page')
            ? reply({'items': rows, 'nextCursor': null})
            : reply(group()),
      );
      final c = FriendConversationController(
        auth: auth,
        conversation: HumanConversation.fromJson(group()),
        apiBaseUrl: 'https://unit.example',
        client: client,
        audioPort: _NoAudio(),
        audioPendingStore: MemoryFriendAudioPendingStore(),
        messagePendingStore: MemoryHumanMessagePendingStore(),
      );
      addTearDown(c.dispose);
      addTearDown(auth.dispose);
      addTearDown(client.close);
      await c.initialise();
      expect(c.historyAvailable, isTrue);
      expect(c.canSendText, isFalse);
      expect(c.canSelectImage, isFalse);
      expect(c.canRecord, isFalse);
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(body: FriendConversationThread(controller: c)),
        ),
      );
      await tester.pump();
      expect(find.text('原系统提示'), findsOneWidget);
      expect(find.text('[栖游鸟·你好]'), findsOneWidget);
      expect(find.text('原聊天记录 · 当前仅可阅读'), findsOneWidget);
      expect(find.byType(TextField), findsNothing);
      auth.retire();
      await tester.pump();
      expect(c.messages, isEmpty);
      expect(find.text('原系统提示'), findsNothing);
      expect(find.text('[栖游鸟·你好]'), findsNothing);
    },
  );
}
