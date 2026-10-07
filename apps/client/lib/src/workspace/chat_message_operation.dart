import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'entity_share_pending_store.dart' show chatEntityUUID;

const humanMessageOperationSchema = 'human.message_operation.v1';
const humanMessageToolVersion = 'human.message.send.v1';
final humanMessageHex = RegExp(r'^[0-9a-f]{64}$');
String humanMessageEffectKey(String owner, String op) => sha256
    .convert(
      utf8.encode(
        'birdtie.human-message-effect.v1\n$owner\n$op\n$op\nPLAIN_MESSAGE_SEND',
      ),
    )
    .toString();
String humanMessagePayloadDigest(String conversation, String body) => sha256
    .convert(
      utf8.encode('birdtie.human-message-payload.v1\n$conversation\n$body'),
    )
    .toString();

/// Stable recovery metadata, never message text, approval or current authority.
class PendingHumanMessage {
  const PendingHumanMessage({
    required this.operationID,
    required this.conversationID,
    required this.payloadDigest,
  });
  final String operationID, conversationID, payloadDigest;
  Map<String, dynamic> toJson() => {
    'operationId': operationID,
    'conversationId': conversationID,
    'payloadDigest': payloadDigest,
    'toolVersion': humanMessageToolVersion,
  };
  factory PendingHumanMessage.decode(Object? raw) {
    if (raw is! Map<String, dynamic> ||
        raw.length != 4 ||
        raw.keys.any(
          (k) => !const {
            'operationId',
            'conversationId',
            'payloadDigest',
            'toolVersion',
          }.contains(k),
        ) ||
        raw['toolVersion'] != humanMessageToolVersion ||
        raw['operationId'] is! String ||
        !chatEntityUUID.hasMatch(raw['operationId']) ||
        raw['conversationId'] is! String ||
        !chatEntityUUID.hasMatch(raw['conversationId']) ||
        raw['payloadDigest'] is! String ||
        !humanMessageHex.hasMatch(raw['payloadDigest'])) {
      throw const FormatException('发送核实记录无法读取，请保留原记录。');
    }
    return PendingHumanMessage(
      operationID: raw['operationId'],
      conversationID: raw['conversationId'],
      payloadDigest: raw['payloadDigest'],
    );
  }
}

class HumanMessageOperationReceipt {
  const HumanMessageOperationReceipt._(this.messageID, this.createdAt);
  final String messageID;
  final DateTime createdAt;
  static HumanMessageOperationReceipt decode(
    Object? raw,
    String owner,
    PendingHumanMessage p,
  ) {
    const keys = {
      'schemaVersion',
      'ownerId',
      'conversationId',
      'operationId',
      'effectKey',
      'payloadDigest',
      'toolVersion',
      'messageId',
      'createdAt',
    };
    final stamp = raw is Map ? raw['createdAt'] : null;
    final at = stamp is String && stamp.length <= 40 && stamp.endsWith('Z')
        ? DateTime.tryParse(stamp)
        : null;
    if (raw is! Map<String, dynamic> ||
        raw.length != keys.length ||
        raw.keys.any((k) => !keys.contains(k)) ||
        !chatEntityUUID.hasMatch(owner) ||
        raw['schemaVersion'] != humanMessageOperationSchema ||
        raw['ownerId'] != owner ||
        raw['conversationId'] != p.conversationID ||
        raw['operationId'] != p.operationID ||
        raw['payloadDigest'] != p.payloadDigest ||
        raw['toolVersion'] != humanMessageToolVersion ||
        raw['effectKey'] != humanMessageEffectKey(owner, p.operationID) ||
        raw['messageId'] is! String ||
        !chatEntityUUID.hasMatch(raw['messageId']) ||
        at == null ||
        at.year < 2000 ||
        at.year > 2200) {
      throw const FormatException('服务记录与这次发送不符，请继续核实原操作。');
    }
    return HumanMessageOperationReceipt._(raw['messageId'], at.toUtc());
  }
}
