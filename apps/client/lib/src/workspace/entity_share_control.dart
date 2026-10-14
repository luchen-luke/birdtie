import 'dart:convert';
import 'package:http/http.dart' as http;
import 'entity_share_pending_store.dart';
import 'human_causal_control.dart';

class EntityShareRecipient {
  const EntityShareRecipient(this.type, this.id);
  final String type, id;
  bool get valid =>
      const {'CONVERSATION', 'FRIEND_TIE'}.contains(type) &&
      chatEntityUUID.hasMatch(id);
  Map<String, Object> toJson() => {'type': type, 'id': id};
  bool same(EntityShareRecipient other) => type == other.type && id == other.id;
  factory EntityShareRecipient.fromJson(Object? raw) {
    final m = humanControlObject(raw, {'type', 'id'});
    if (m['type'] is! String || m['id'] is! String)
      throw const FormatException('收件人不符');
    final r = EntityShareRecipient(m['type'], m['id']);
    if (!r.valid) throw const FormatException('收件人不符');
    return r;
  }
}

class EntityShareInput {
  const EntityShareInput({
    required this.operationID,
    required this.recipient,
    required this.type,
    required this.entityID,
  });
  final String operationID, type, entityID;
  final EntityShareRecipient recipient;
  bool get valid =>
      chatEntityUUID.hasMatch(operationID) &&
      recipient.valid &&
      chatEntityTypes.contains(type) &&
      chatEntityUUID.hasMatch(entityID);
  Map<String, Object> get entity => {'type': type, 'id': entityID};
  String get digest => entitySharePayloadDigest(
    operationID,
    recipient.type,
    recipient.id,
    type,
    entityID,
  );
  Map<String, Object> toJson() => {
    'operationId': operationID,
    'entity': entity,
  };
  PendingEntityShare pending(String sourceVersion, String stage) =>
      PendingEntityShare.control(
        operationID: operationID,
        recipientType: recipient.type,
        recipientID: recipient.id,
        type: type,
        entityID: entityID,
        sourceVersion: sourceVersion,
        payloadDigest: digest,
        stage: stage,
      );
}

class EntitySharePreview {
  const EntitySharePreview({
    required this.input,
    required this.entityTitle,
    required this.recipientAccountID,
    required this.sourceVersion,
    required this.approval,
    required this.observedAt,
    required this.expiresAt,
  });
  final EntityShareInput input;
  final String entityTitle, recipientAccountID, sourceVersion, approval;
  final DateTime observedAt, expiresAt;
  bool get live => DateTime.now().toUtc().isBefore(expiresAt);
  Map<String, Object> get body => {
    ...input.toJson(),
    'sourceVersion': sourceVersion,
    'approval': approval,
  };
  factory EntitySharePreview.fromJson(
    Object? raw,
    EntityShareInput input,
    String peer,
    String owner,
  ) {
    final m = humanControlObject(raw, {
      'operationId',
      'recipient',
      'entity',
      'entityTitle',
      'recipientAccountId',
      'sourceVersion',
      'payloadDigest',
      'observedAt',
      'expiresAt',
      'approval',
    });
    final entity = humanControlObject(m['entity'], {'type', 'id'});
    if (!input.valid ||
        m['operationId'] != input.operationID ||
        !EntityShareRecipient.fromJson(m['recipient']).same(input.recipient) ||
        entity['type'] != input.type ||
        entity['id'] != input.entityID ||
        m['payloadDigest'] != input.digest ||
        !humanControlHash(m['sourceVersion']) ||
        !humanControlID(peer) ||
        peer == owner ||
        m['recipientAccountId'] != peer ||
        m['entityTitle'] is! String ||
        (m['entityTitle'] as String).trim().isEmpty ||
        utf8.encode(m['entityTitle']).length > 8192 ||
        m['approval'] is! String ||
        (m['approval'] as String).isEmpty ||
        utf8.encode(m['approval']).length > 8192)
      throw const FormatException('具体分享预览不符');
    final observed = humanControlTime(m['observedAt']),
        expires = humanControlTime(m['expiresAt']);
    humanControlLease(observed, expires);
    if (expires.isAfter(observed.add(const Duration(seconds: 30))))
      throw const FormatException('批准期限不符');
    return EntitySharePreview(
      input: input,
      entityTitle: m['entityTitle'],
      recipientAccountID: peer,
      sourceVersion: m['sourceVersion'],
      approval: m['approval'],
      observedAt: observed,
      expiresAt: expires,
    );
  }
}

class EntityShareReceipt {
  const EntityShareReceipt({
    required this.operationID,
    required this.recipient,
    required this.status,
    this.conversationID,
    this.messageID,
  });
  final String operationID, status;
  final EntityShareRecipient recipient;
  final String? conversationID, messageID;
  bool get succeeded => status == 'SUCCEEDED';
  factory EntityShareReceipt.fromJson(Object? raw, PendingEntityShare p) {
    if (raw is! Map<String, dynamic>) throw const FormatException('原分享回执不符');
    final stopped = raw['status'] == 'STOPPED';
    final m = humanControlObject(
      raw,
      stopped
          ? {'operationId', 'status', 'recipient'}
          : {
              'operationId',
              'status',
              'recipient',
              'conversationId',
              'messageId',
              'entity',
              'payloadDigest',
              'committedAt',
            },
    );
    final recipient = EntityShareRecipient.fromJson(m['recipient']);
    if (m['operationId'] != p.operationID ||
        recipient.type != p.recipientType ||
        recipient.id != p.recipientID ||
        (!stopped && m['status'] != 'SUCCEEDED'))
      throw const FormatException('原分享回执不符');
    if (!stopped) {
      final entity = humanControlObject(m['entity'], {'type', 'id'});
      final digest = EntityShareInput(
        operationID: p.operationID,
        recipient: recipient,
        type: p.type,
        entityID: p.entityID,
      ).digest;
      if (!humanControlID(m['conversationId']) ||
          !humanControlID(m['messageId']) ||
          (recipient.type == 'CONVERSATION' &&
              m['conversationId'] != recipient.id) ||
          entity['type'] != p.type ||
          entity['id'] != p.entityID ||
          m['payloadDigest'] != digest)
        throw const FormatException('原分享事实不符');
      humanControlTime(m['committedAt']);
    }
    return EntityShareReceipt(
      operationID: p.operationID,
      recipient: recipient,
      status: m['status'],
      conversationID: m['conversationId'],
      messageID: m['messageId'],
    );
  }
}

class EntityShareControlAPI {
  EntityShareControlAPI({
    required String? Function() authorizationHeader,
    required http.Client client,
    required String apiBaseUrl,
  }) : api = HumanCausalApi(
         authorizationHeader: authorizationHeader,
         client: client,
         baseUrl: apiBaseUrl,
       );
  final HumanCausalApi api;
  String get origin => api.origin;
  String _base(EntityShareRecipient r) {
    if (!r.valid) throw const FormatException('收件人不符');
    return '/v1/me/entity-shares/${r.type}/${r.id}';
  }

  Future<String> owner(bool Function() current) => api.owner(current);
  Future<EntitySharePreview> preview(
    EntityShareInput input,
    String peer,
    String owner,
    bool Function() current,
  ) async => EntitySharePreview.fromJson(
    await api.json(
      'POST',
      '${_base(input.recipient)}/previews',
      body: input.toJson(),
      current: current,
    ),
    input,
    peer,
    owner,
  );
  Future<EntityShareReceipt> submit(
    EntitySharePreview preview,
    PendingEntityShare pending,
    bool Function() current,
  ) async => EntityShareReceipt.fromJson(
    await api.json(
      'PUT',
      '${_base(preview.input.recipient)}/operations/${pending.operationID}',
      body: preview.body,
      current: current,
    ),
    pending,
  );
  String _original(PendingEntityShare p) => p.legacy
      ? '/v1/me/conversations/${p.conversationID}/entity-shares/${p.operationID}'
      : '${_base(EntityShareRecipient(p.recipientType, p.recipientID))}/operations/${p.operationID}';
  Future<EntityShareReceipt> recover(
    PendingEntityShare p,
    bool Function() current, {
    bool stop = false,
  }) async => EntityShareReceipt.fromJson(
    await api.json(
      stop ? 'POST' : 'GET',
      '${_original(p)}${stop ? '/stop' : ''}',
      current: current,
    ),
    p,
  );
}
