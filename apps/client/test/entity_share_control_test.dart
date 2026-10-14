import 'dart:convert';
import 'package:birdtie_client/src/workspace/entity_share_control.dart';
import 'package:birdtie_client/src/workspace/entity_share_pending_store.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'entity_share_pending_store_test.dart';

Map<String, dynamic> sharePreviewWire(
  EntityShareInput input, {
  String title = '当前公开动态',
  String peer = sharePeer,
  int seconds = 30,
}) {
  final now = DateTime.now().toUtc();
  return {
    'operationId': input.operationID,
    'recipient': input.recipient.toJson(),
    'entity': input.entity,
    'entityTitle': title,
    'recipientAccountId': peer,
    'sourceVersion': 'a' * 64,
    'payloadDigest': input.digest,
    'observedAt': now.toIso8601String(),
    'expiresAt': now.add(Duration(seconds: seconds)).toIso8601String(),
    'approval': 'fixture-specific-approval',
  };
}

Map<String, dynamic> tinyShareWire(
  PendingEntityShare p, {
  bool stopped = false,
}) => {
  'operationId': p.operationID,
  'status': stopped ? 'STOPPED' : 'SUCCEEDED',
  'recipient': {'type': p.recipientType, 'id': p.recipientID},
  if (!stopped) ...{
    'conversationId': shareConversation,
    'messageId': shareMessage,
    'entity': {'type': p.type, 'id': p.entityID},
    'payloadDigest': entitySharePayloadDigest(
      p.operationID,
      p.recipientType,
      p.recipientID,
      p.type,
      p.entityID,
    ),
    'committedAt': '2026-10-14T01:00:00Z',
  },
};
http.Response shareControlJSON(Object? value, [int status = 200]) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': value})), status);
const controlInput = EntityShareInput(
  operationID: shareOperation,
  recipient: EntityShareRecipient('CONVERSATION', shareConversation),
  type: 'moment',
  entityID: shareTarget,
);

void main() {
  for (final kind in chatEntityTypes) {
    test('143 live selector $kind digest binds exact original recipient', () {
      final input = EntityShareInput(
        operationID: shareOperation,
        recipient: controlInput.recipient,
        type: kind,
        entityID: shareTarget,
      );
      final p = EntitySharePreview.fromJson(
        sharePreviewWire(input),
        input,
        sharePeer,
        shareOwner,
      );
      expect(p.body.keys.toSet(), {
        'operationId',
        'entity',
        'sourceVersion',
        'approval',
      });
      final pending = input.pending(p.sourceVersion, 'UNKNOWN');
      expect(
        EntityShareReceipt.fromJson(tinyShareWire(pending), pending).succeeded,
        true,
      );
      expect(
        EntityShareReceipt.fromJson(
          tinyShareWire(pending, stopped: true),
          pending,
        ).status,
        'STOPPED',
      );
    });
  }
  for (final change in [
    'extra',
    'peer',
    'recipient',
    'entity',
    'digest',
    'expiry',
    'source',
    'private',
  ]) {
    test('143 closed preview rejects $change', () {
      final wire = sharePreviewWire(controlInput);
      switch (change) {
        case 'extra':
          wire['body'] = '不可复制正文';
        case 'peer':
          wire['recipientAccountId'] = shareOwner;
        case 'recipient':
          wire['recipient'] = {'type': 'FRIEND_TIE', 'id': shareConversation};
        case 'entity':
          wire['entity'] = {'type': 'place', 'id': shareTarget};
        case 'digest':
          wire['payloadDigest'] = 'b' * 64;
        case 'expiry':
          wire['expiresAt'] = DateTime.now()
              .toUtc()
              .add(const Duration(seconds: 31))
              .toIso8601String();
        case 'source':
          wire['sourceVersion'] = 'future';
        case 'private':
          wire['entity'] = {'type': 'memory', 'id': shareTarget};
      }
      expect(
        () => EntitySharePreview.fromJson(
          wire,
          controlInput,
          sharePeer,
          shareOwner,
        ),
        throwsFormatException,
      );
    });
  }
  for (final change in [
    'messageBody',
    'wrongRecipient',
    'wrongEntity',
    'wrongDigest',
    'stoppedEffects',
  ]) {
    test('143 original tiny receipt rejects $change', () {
      final pending = controlInput.pending('a' * 64, 'UNKNOWN');
      final wire = tinyShareWire(pending, stopped: change == 'stoppedEffects');
      switch (change) {
        case 'messageBody':
          wire['body'] = '私信正文';
        case 'wrongRecipient':
          wire['recipient'] = {'type': 'CONVERSATION', 'id': sharePeer};
        case 'wrongEntity':
          wire['entity'] = {'type': 'place', 'id': shareTarget};
        case 'wrongDigest':
          wire['payloadDigest'] = 'b' * 64;
        case 'stoppedEffects':
          wire['messageId'] = shareMessage;
      }
      expect(
        () => EntityShareReceipt.fromJson(wire, pending),
        throwsFormatException,
      );
    });
  }
  test(
    '143 history only exact original GET or bodyless STOP; v1 bridge retained',
    () async {
      final paths = <String>[];
      final api = EntityShareControlAPI(
        authorizationHeader: () => 'Bearer fixture',
        apiBaseUrl: 'https://api.example',
        client: MockClient((r) async {
          paths.add('${r.method} ${r.url.path}');
          expect(r.body, isEmpty);
          return shareControlJSON(tinyShareWire(pendingShare));
        }),
      );
      await api.recover(pendingShare, () => true);
      await api.recover(pendingShare, () => true, stop: true);
      expect(paths, [
        'GET /v1/me/conversations/$shareConversation/entity-shares/$shareOperation',
        'POST /v1/me/conversations/$shareConversation/entity-shares/$shareOperation/stop',
      ]);
    },
  );
}
