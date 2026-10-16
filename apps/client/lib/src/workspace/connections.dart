import 'connection_request_decision_operation.dart';
import 'chat_message_operation.dart';
import 'chat_message_pending_store.dart';
import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;

import '../app/birdtie_app_bar.dart';
import '../auth/birdtie_auth_controller.dart';
import '../config/birdtie_environment.dart';
import 'support_page.dart';
import 'privacy_control_page.dart';
import 'human_causal_control.dart';
import 'human_causal_operation_controller.dart';
import 'social_intent_drafts.dart';
import 'entity_share_pending_store.dart';
import 'entity_share_control.dart';
import 'entity_share_control_controller.dart';
import 'chat_entity_router.dart';
import 'notification_destination_router.dart';
import 'entity_action_contract.dart';
import 'connection_request_review_page.dart';
import 'now_ui_tokens.dart';
import 'close_connections_page.dart';
import 'friend_audio.dart';
import 'friend_image.dart';
import 'person_avatar.dart';

class ConnectionTransportFailure implements Exception {
  const ConnectionTransportFailure(this.status);
  final int status;
}

String _requestState(String state) => switch (state) {
  'pending' => '待处理',
  'accepted' => '已接受',
  'declined' => '已拒绝',
  'withdrawn' => '已撤回',
  'expired' => '已过期',
  _ => '状态待核实',
};

class ContactRequest {
  const ContactRequest({
    required this.id,
    required this.direction,
    required this.otherAccountId,
    required this.otherName,
    this.avatar,
    required this.note,
    required this.state,
    required this.conversationId,
    required this.scope,
    this.policyDisposition = '',
    this.origin = '',
    this.screeningStatus = '',
    this.expiresAt,
    this.createdAt,
  });
  final String id;
  final String direction;
  final String otherAccountId;
  final String otherName;
  final PersonAvatarMetadata? avatar;
  final String note;
  final String state;
  final String conversationId;
  final String scope;
  final String policyDisposition;
  final String origin;
  final String screeningStatus;
  final DateTime? expiresAt;
  final DateTime? createdAt;
  bool get pendingReview =>
      direction == 'incoming' &&
      state == 'pending' &&
      policyDisposition == 'SCREEN' &&
      screeningStatus == 'PENDING_REVIEW';

  factory ContactRequest.fromJson(Map<String, dynamic> json) => ContactRequest(
    id: json['id'] as String,
    direction: json['direction'] as String? ?? '',
    otherAccountId: json['otherAccountId'] as String? ?? '',
    otherName: json['otherName'] as String? ?? '',
    avatar: PersonAvatarMetadata.optional(
      json['avatar'],
      json['otherAccountId'] as String? ?? '',
    ),
    note: json['note'] as String? ?? '',
    state: json['state'] as String? ?? '',
    conversationId: json['conversationId'] as String? ?? '',
    scope: json['scope'] as String? ?? 'conversation',
    policyDisposition: json['policyDisposition'] as String? ?? '',
    origin: json['origin'] as String? ?? '',
    screeningStatus: json['screeningStatus'] as String? ?? '',
    expiresAt: _requestStamp(json['expiresAt']),
    createdAt: _requestStamp(json['createdAt']),
  );

  factory ContactRequest.fromReviewJson(Map<String, dynamic> value) {
    final r = ContactRequest.fromJson(value);
    if (value.keys.any((k) => !_requestWireFields.contains(k)) ||
        !chatEntityUUID.hasMatch(r.id) ||
        !chatEntityUUID.hasMatch(r.otherAccountId) ||
        !const {'incoming', 'outgoing'}.contains(r.direction) ||
        !const {'friend', 'conversation'}.contains(r.scope) ||
        !const {
          'pending',
          'accepted',
          'declined',
          'withdrawn',
          'expired',
        }.contains(r.state) ||
        !const {'', 'REQUEST', 'SCREEN'}.contains(r.policyDisposition) ||
        !const {'', 'PENDING_REVIEW'}.contains(r.screeningStatus) ||
        r.screeningStatus == 'PENDING_REVIEW' &&
            (r.state != 'pending' || r.policyDisposition != 'SCREEN') ||
        r.state == 'pending' &&
            r.policyDisposition == 'SCREEN' &&
            r.screeningStatus != 'PENDING_REVIEW' ||
        (r.expiresAt == null && r.origin != 'LEGACY_IMPORT') ||
        r.createdAt == null ||
        r.expiresAt != null && !r.expiresAt!.isAfter(r.createdAt!) ||
        !const {'', 'LEGACY_IMPORT'}.contains(r.origin) ||
        r.origin == 'LEGACY_IMPORT' &&
            (r.expiresAt != null || r.policyDisposition.isNotEmpty) ||
        r.note.length > 1000 ||
        r.otherName.isEmpty ||
        r.otherName.length > 500 ||
        r.conversationId.isNotEmpty &&
            !chatEntityUUID.hasMatch(r.conversationId)) {
      throw const FormatException('申请格式不符，请重新核实');
    }
    return r;
  }
}

const _requestWireFields = {
  'id',
  'direction',
  'otherAccountId',
  'otherName',
  'avatar',
  'cityId',
  'note',
  'scope',
  'state',
  'conversationId',
  'policyDisposition',
  'screeningStatus',
  'createdAt',
  'expiresAt',
  'origin',
};

DateTime? _requestStamp(dynamic value) {
  if (value is! String) {
    return null;
  }
  final m = RegExp(
    r'^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?(Z|[+-]\d{2}:\d{2})$',
  ).firstMatch(value);
  if (m == null) {
    return null;
  }
  final n = [for (var i = 1; i <= 6; i++) int.parse(m.group(i)!)];
  final wall = DateTime.utc(n[0], n[1], n[2], n[3], n[4], n[5]);
  if (n[0] < 1 ||
      wall.year != n[0] ||
      wall.month != n[1] ||
      wall.day != n[2] ||
      wall.hour != n[3] ||
      wall.minute != n[4] ||
      wall.second != n[5]) {
    return null;
  }
  final zone = m.group(7)!;
  if (zone != 'Z' &&
      (int.parse(zone.substring(1, 3)) > 23 ||
          int.parse(zone.substring(4)) > 59)) {
    return null;
  }
  // Original Go/PG Request timestamps can carry an RFC3339 offset. Keep the
  // exact instant in UTC while rejecting calendar normalization and bad zones.
  final parsed = DateTime.tryParse(value)?.toUtc();
  return parsed != null && parsed.year >= 1 && parsed.year <= 9999
      ? parsed
      : null;
}

class ConnectionReviewException implements Exception {
  const ConnectionReviewException(this.status, {this.errorCode});
  final int status;
  final String? errorCode;
}

/// The original decision DTO is deliberately smaller than the list DTO.
class ConnectionDecisionReceipt {
  const ConnectionDecisionReceipt(this.id, this.state, this.conversationID);
  final String id, state, conversationID;
}

class FriendTie {
  const FriendTie({
    required this.id,
    required this.otherAccountId,
    required this.otherName,
    this.avatar,
  });
  final String id;
  final String otherAccountId;
  final String otherName;
  final PersonAvatarMetadata? avatar;

  factory FriendTie.fromJson(Map<String, dynamic> json) => FriendTie(
    id: json['id'] as String,
    otherAccountId: json['otherAccountId'] as String,
    otherName: json['otherName'] as String,
    avatar: PersonAvatarMetadata.optional(
      json['avatar'],
      json['otherAccountId'] as String,
    ),
  );
}

class HumanConversation {
  const HumanConversation({
    required this.id,
    required this.otherAccountId,
    required this.otherName,
    this.avatar,
    this.unreadCount = 0,
    this.preview = '',
    this.kind = 'DIRECT',
    this.origin = '',
    this.canSend = true,
    this.lastMessageAt,
  });
  final String id;
  final String otherAccountId;
  final String otherName;
  final PersonAvatarMetadata? avatar;
  final int unreadCount;
  final String preview;
  final String kind;
  final String origin;
  final bool canSend;
  bool get historical => origin == 'LEGACY_IMPORT';
  bool get personalGroup => kind == 'PERSONAL_GROUP';
  final DateTime? lastMessageAt;

  factory HumanConversation.fromJson(Map<String, dynamic> json) =>
      HumanConversation(
        id: json['id'] as String,
        otherAccountId: json['otherAccountId'] as String,
        otherName: json['otherName'] as String,
        avatar: PersonAvatarMetadata.optional(
          json['avatar'],
          json['otherAccountId'] as String,
        ),
        unreadCount: json['unreadCount'] as int? ?? 0,
        preview: json['lastMessagePreview'] as String? ?? '',
        kind: json['kind'] as String? ?? 'DIRECT',
        origin: json['origin'] as String? ?? '',
        canSend: json['canSend'] as bool? ?? true,
        lastMessageAt: json['lastMessageAt'] == null
            ? null
            : DateTime.parse(json['lastMessageAt'] as String).toUtc(),
      );
}

class HumanMessage {
  const HumanMessage({
    required this.id,
    required this.senderAccountId,
    required this.body,
    required this.createdAt,
    this.kind = 'TEXT',
    this.origin = '',
    this.sequence,
    this.stickerPackID,
    this.stickerID,
    this.unavailable = false,
    this.entity,
    this.audio,
    this.image,
  });
  final String id;
  final String senderAccountId;
  final String body;
  final DateTime createdAt;
  final String kind;
  final String origin;
  final int? sequence;
  final String? stickerPackID;
  final String? stickerID;
  final bool unavailable;
  bool get system => kind == 'SYSTEM';
  final ChatEntityCard? entity;
  final FriendAudioMetadata? audio;
  final FriendImageMetadata? image;

  factory HumanMessage.fromJson(Map<String, dynamic> json) {
    if ([
          json['entity'],
          json['audio'],
          json['image'],
        ].where((value) => value != null).length >
        1) {
      throw const FormatException('一条消息不能同时包含多种附件');
    }
    return HumanMessage(
      id: json['id'] as String,
      senderAccountId: json['senderAccountId'] as String,
      body: json['body'] as String,
      kind: json['kind'] as String? ?? 'TEXT',
      origin: json['origin'] as String? ?? '',
      sequence: json['sequence'] as int?,
      stickerPackID:
          (json['sticker'] as Map<String, dynamic>?)?['packId'] as String?,
      stickerID:
          (json['sticker'] as Map<String, dynamic>?)?['stickerId'] as String?,
      unavailable: json['unavailable'] as bool? ?? false,
      createdAt: DateTime.parse(json['createdAt'] as String),
      entity: json['entity'] is Map<String, dynamic>
          ? ChatEntityCard.fromJson(json['entity'] as Map<String, dynamic>)
          : null,
      audio: json['audio'] == null
          ? null
          : FriendAudioMetadata.decode(json['audio']),
      image: json['image'] == null
          ? null
          : FriendImageMetadata.decode(json['image']),
    );
  }
}

/// One bounded server page. The cursor is a location hint, never a permission.
class HumanConversationListPage {
  HumanConversationListPage({
    required List<HumanConversation> items,
    required this.nextCursor,
    required this.unreadCount,
  }) : items = List.unmodifiable(items);
  final List<HumanConversation> items;
  final String? nextCursor;
  final int unreadCount;

  factory HumanConversationListPage.fromJson(
    Map<String, dynamic> value, {
    int limit = 50,
    String? previousCursor,
  }) {
    final data = _chatPageObject(value, const {
      'items',
      'nextCursor',
      'unreadCount',
    });
    final rows = data['items'], total = data['unreadCount'];
    if (limit < 1 ||
        limit > 50 ||
        rows is! List ||
        rows.length > limit ||
        total is! int ||
        total < 0) {
      throw const FormatException('对话分页状态不符');
    }
    final items = [for (final row in rows) _chatPageConversation(row)];
    if (items.map((v) => v.id).toSet().length != items.length) {
      throw const FormatException('对话分页重复');
    }
    return HumanConversationListPage(
      items: items,
      unreadCount: total,
      nextCursor: _chatPageNext(
        data['nextCursor'],
        previousCursor,
        items.isNotEmpty,
      ),
    );
  }
}

Map<String, String> socialPageQuery({String? cursor, int limit = 20}) {
  if (limit < 1 ||
      limit > 50 ||
      cursor != null && !_chatPageCursor.hasMatch(cursor)) {
    throw const FormatException('联系人分页参数不符');
  }
  return {'limit': '$limit', if (cursor != null) 'cursor': cursor};
}

/// Closed, bounded page over one original social list. A cursor is opaque;
/// absent items/nextCursor and repeated cursors are invalid, not an empty list.
class SocialListPage<T> {
  SocialListPage({required List<T> items, required this.nextCursor})
    : items = List.unmodifiable(items);
  final List<T> items;
  final String? nextCursor;

  factory SocialListPage.fromJson(
    dynamic value, {
    required T Function(Map<String, dynamic>) decode,
    required String Function(T) key,
    int limit = 20,
    String? previousCursor,
  }) {
    if (value is! Map<String, dynamic> ||
        value.length != 2 ||
        !value.containsKey('items') ||
        !value.containsKey('nextCursor') ||
        value['items'] is! List ||
        limit < 1 ||
        limit > 50 ||
        (value['items'] as List).length > limit) {
      throw const FormatException('联系人分页格式不符');
    }
    final items = <T>[];
    final seen = <String>{};
    for (final raw in value['items'] as List) {
      if (raw is! Map<String, dynamic>) {
        throw const FormatException('联系人条目不符');
      }
      final item = decode(raw);
      if (!seen.add(key(item))) throw const FormatException('联系人分页重复');
      items.add(item);
    }
    final next = value['nextCursor'];
    if (next != null &&
        (next is! String ||
            !_chatPageCursor.hasMatch(next) ||
            next == previousCursor ||
            items.isEmpty)) {
      throw const FormatException('联系人分页无法继续');
    }
    return SocialListPage(items: items, nextCursor: next as String?);
  }
}

FriendTie socialPageFriend(Map<String, dynamic> value) {
  if (value.keys.any(
        (k) => !const {
          'id',
          'otherAccountId',
          'otherName',
          'createdAt',
          'avatar',
        }.contains(k),
      ) ||
      value['id'] is! String ||
      !_validChatPageID(value['id']) ||
      value['otherAccountId'] is! String ||
      !_validChatPageID(value['otherAccountId']) ||
      value['otherName'] is! String ||
      (value['otherName'] as String).isEmpty ||
      (value['otherName'] as String).length > 500 ||
      _requestStamp(value['createdAt']) == null) {
    throw const FormatException('好友来源不符');
  }
  return FriendTie.fromJson(value);
}

class HumanMessagePage {
  HumanMessagePage({
    required List<HumanMessage> items,
    required this.nextCursor,
  }) : items = List.unmodifiable(items);
  final List<HumanMessage> items;
  final String? nextCursor;
}

final _chatPageCursor = RegExp(r'^[A-Za-z0-9_-]{1,1024}$');
bool _validChatPageID(String id) =>
    chatEntityUUID.hasMatch(id) && id != '00000000-0000-0000-0000-000000000000';

Map<String, dynamic> _chatPageObject(dynamic value, Set<String> fields) {
  if (value is! Map<String, dynamic> ||
      value.keys.any((k) => !fields.contains(k))) {
    throw const FormatException('消息分页格式不符');
  }
  return value;
}

String? _chatPageNext(dynamic value, String? previous, bool hasItems) {
  if (value == null || value == '') return null;
  if (value is! String ||
      !_chatPageCursor.hasMatch(value) ||
      value == previous ||
      !hasItems) {
    throw const FormatException('消息分页无法继续');
  }
  return value;
}

HumanConversation _chatPageConversation(dynamic value) {
  final v = _chatPageObject(value, const {
    'id',
    'otherAccountId',
    'otherName',
    'createdAt',
    'unreadCount',
    'lastMessagePreview',
    'lastMessageAt',
    'lastReadAt',
    'avatar',
    'kind',
    'origin',
    'canSend',
  });
  if (v['id'] is! String ||
      !_validChatPageID(v['id']) ||
      v['otherAccountId'] is! String ||
      !(v['origin'] == 'LEGACY_IMPORT' &&
              v['kind'] == 'PERSONAL_GROUP' &&
              v['otherAccountId'] == '' &&
              v['canSend'] == false &&
              v['avatar'] == null) &&
          !_validChatPageID(v['otherAccountId']) ||
      v['otherName'] is! String ||
      (v['otherName'] as String).isEmpty ||
      (v['otherName'] as String).length > 500 ||
      !const {null, 'DIRECT', 'PERSONAL_GROUP'}.contains(v['kind']) ||
      !const {null, 'LEGACY_IMPORT'}.contains(v['origin']) ||
      v['kind'] == 'PERSONAL_GROUP' && v['origin'] != 'LEGACY_IMPORT' ||
      v['origin'] == 'LEGACY_IMPORT' && v['canSend'] is! bool ||
      v['canSend'] != null && v['canSend'] is! bool ||
      v['unreadCount'] is! int ||
      (v['unreadCount'] as int) < 0 ||
      _requestStamp(v['createdAt']) == null ||
      v['lastReadAt'] != null && _requestStamp(v['lastReadAt']) == null ||
      v['lastMessageAt'] != null && _requestStamp(v['lastMessageAt']) == null ||
      v['lastMessagePreview'] != null &&
          (v['lastMessagePreview'] is! String ||
              (v['lastMessagePreview'] as String).length > 500)) {
    throw const FormatException('对话来源无法核实');
  }
  return HumanConversation.fromJson(v);
}

HumanMessage _chatPageMessage(dynamic value, String conversation) {
  final v = _chatPageObject(value, const {
    'id',
    'conversationId',
    'senderAccountId',
    'speakerKind',
    'body',
    'createdAt',
    'entity',
    'audio',
    'image',
    'kind',
    'origin',
    'sequence',
    'sticker',
    'unavailable',
  });
  if (v['id'] is! String ||
      !_validChatPageID(v['id']) ||
      v['conversationId'] != conversation ||
      v['senderAccountId'] is! String ||
      !_validChatPageID(v['senderAccountId']) ||
      !(v['speakerKind'] == 'human' ||
          v['speakerKind'] == 'system' &&
              v['origin'] == 'LEGACY_IMPORT' &&
              v['kind'] == 'SYSTEM') ||
      v['body'] is! String ||
      utf8.encode(v['body'] as String).length > 2000 ||
      (v['body'] as String).contains('\u0000') ||
      _requestStamp(v['createdAt']) == null) {
    throw const FormatException('消息来源无法核实');
  }
  final kind = v['kind'] ?? 'TEXT';
  final imported = v['origin'] == 'LEGACY_IMPORT';
  final unavailable = v['unavailable'] == true;
  if (!const {'TEXT', 'IMAGE', 'ENTITY', 'SYSTEM', 'STICKER'}.contains(kind) ||
      !const {null, 'LEGACY_IMPORT'}.contains(v['origin']) ||
      v['sequence'] != null &&
          (v['sequence'] is! int || (v['sequence'] as int) < 1) ||
      imported && v['sequence'] == null ||
      const {'SYSTEM', 'STICKER'}.contains(kind) && !imported ||
      v['unavailable'] != null && v['unavailable'] is! bool ||
      unavailable &&
          (!imported ||
              v['body'] != '' ||
              v['entity'] != null ||
              v['image'] != null ||
              v['audio'] != null ||
              v['sticker'] != null)) {
    throw const FormatException('历史消息类型不符');
  }
  if (!unavailable && kind == 'STICKER') {
    final sticker = _chatPageObject(v['sticker'], const {
      'packId',
      'stickerId',
    });
    if (sticker.length != 2 ||
        sticker['packId'] is! String ||
        sticker['stickerId'] is! String ||
        (sticker['packId'] as String).isEmpty ||
        (sticker['packId'] as String).length > 100 ||
        (sticker['stickerId'] as String).isEmpty ||
        (sticker['stickerId'] as String).length > 100 ||
        v['body'] == '')
      throw const FormatException('原贴纸来源不符');
  } else if (v['sticker'] != null) {
    throw const FormatException('贴纸类型不符');
  }
  if (v['entity'] != null) {
    final entity = _chatPageObject(v['entity'], const {
      'type',
      'id',
      'title',
      'available',
    });
    if (entity['type'] is! String ||
        (entity['type'] as String).length > 80 ||
        entity['available'] is! bool ||
        entity['id'] != null &&
            (entity['id'] is! String || !_validChatPageID(entity['id'])) ||
        entity['title'] != null &&
            (entity['title'] is! String ||
                (entity['title'] as String).length > 2000) ||
        entity['available'] == true &&
            (entity['id'] == null || entity['title'] == null) ||
        entity['available'] == false &&
            (entity['id'] != null || entity['title'] != null)) {
      throw const FormatException('消息卡片来源无法核实');
    }
  }
  return HumanMessage.fromJson(v);
}

bool humanMessageBefore(HumanMessage a, HumanMessage b) {
  if (a.sequence != null || b.sequence != null) {
    return a.sequence != null &&
        b.sequence != null &&
        a.sequence! < b.sequence!;
  }
  return a.createdAt.isBefore(b.createdAt) ||
      a.createdAt.isAtSameMomentAs(b.createdAt) && a.id.compareTo(b.id) < 0;
}

class ChatEntityCard {
  const ChatEntityCard({
    required this.type,
    required this.available,
    this.id,
    this.title,
  });
  final String type;
  final bool available;
  final String? id;
  final String? title;

  factory ChatEntityCard.fromJson(Map<String, dynamic> json) => ChatEntityCard(
    type: json['type'] as String,
    available: json['available'] as bool? ?? false,
    id: json['id'] as String?,
    title: json['title'] as String?,
  );

  String get typeLabel => switch (type) {
    'activity' => '活动',
    'place' => '地点',
    'person' => '用户',
    'community' => '社群',
    'organization' => '组织',
    'business' => '商家',
    'moment' => '动态',
    _ => '内容',
  };
}

Object? _activeEntityShareToken;

Future<void> shareEntityToChat(
  BuildContext context, {
  required String? Function() authorizationHeader,
  required String type,
  required String id,
  String? apiBaseUrl,
  http.Client? client,
  Listenable? identityChanges,
  String? Function()? workspaceID,
  String? Function()? ownerID,
  bool Function()? sourceCurrent,
  Object? Function()? sourceKey,
  EntitySharePendingStore? pendingStore,
}) async {
  if (authorizationHeader() == null || workspaceID?.call() != null) {
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(const SnackBar(content: Text('请使用已登录的个人身份给好友分享。')));
    return;
  }
  if (!chatEntityTypes.contains(type) ||
      !chatEntityUUID.hasMatch(id) ||
      _activeEntityShareToken != null)
    return;
  final flow = Object();
  _activeEntityShareToken = flow;
  try {
    final disposed = Completer<void>();
    final navigator = Navigator.of(context);
    final route = ModalBottomSheetRoute<Object?>(
      capturedThemes: InheritedTheme.capture(
        from: context,
        to: navigator.context,
      ),
      barrierLabel: MaterialLocalizations.of(context).modalBarrierDismissLabel,
      isScrollControlled: true,
      builder: (_) => _EntityShareSheet(
        authorizationHeader: authorizationHeader,
        type: type,
        id: id,
        apiBaseUrl: apiBaseUrl,
        client: client,
        identityChanges: identityChanges,
        workspaceID: workspaceID,
        ownerID: ownerID,
        sourceCurrent: sourceCurrent,
        sourceKey: sourceKey,
        pendingStore: pendingStore ?? const SecureEntitySharePendingStore(),
        onDisposed: () {
          if (!disposed.isCompleted) disposed.complete();
        },
      ),
    );
    void release() {
      if (identical(_activeEntityShareToken, flow))
        _activeEntityShareToken = null;
    }

    route.completed.whenComplete(release);
    await Future.any<Object?>([
      navigator.push<Object?>(route),
      route.completed,
      disposed.future.then<Object?>((_) => null),
    ]);
  } finally {
    if (identical(_activeEntityShareToken, flow))
      _activeEntityShareToken = null;
  }
}

class _ShareRecipient {
  const _ShareRecipient(this.accountID, this.name, this.target);
  final String accountID, name;
  final EntityShareRecipient target;
}

class _EntityShareSheet extends StatefulWidget {
  const _EntityShareSheet({
    required this.authorizationHeader,
    required this.type,
    required this.id,
    required this.pendingStore,
    required this.onDisposed,
    this.apiBaseUrl,
    this.client,
    this.identityChanges,
    this.workspaceID,
    this.ownerID,
    this.sourceCurrent,
    this.sourceKey,
  });
  final String? Function() authorizationHeader;
  final String type, id;
  final String? apiBaseUrl;
  final http.Client? client;
  final Listenable? identityChanges;
  final String? Function()? workspaceID, ownerID;
  final bool Function()? sourceCurrent;
  final Object? Function()? sourceKey;
  final EntitySharePendingStore pendingStore;
  final VoidCallback onDisposed;
  @override
  State<_EntityShareSheet> createState() => _EntityShareSheetState();
}

class _EntityShareSheetState extends State<_EntityShareSheet> {
  late final ConnectionSource _source;
  late final EntityShareControlController _control;
  List<_ShareRecipient> _recipients = [];
  bool _loading = false, _ready = false;
  String? _error;
  int _loadEpoch = 0;
  @override
  void initState() {
    super.initState();
    _source = ConnectionSource(
      authorizationHeader: widget.authorizationHeader,
      client: widget.client,
      apiBaseUrl: widget.apiBaseUrl,
    );
    _control = EntityShareControlController(
      api: EntityShareControlAPI(
        authorizationHeader: widget.authorizationHeader,
        client: _source.followClient,
        apiBaseUrl: _source.followApiBaseUrl,
      ),
      store: widget.pendingStore,
      ownerID: widget.ownerID,
      identityChanges: widget.identityChanges,
      workspaceID: widget.workspaceID,
      sourceCurrent: widget.sourceCurrent,
      sourceKey: widget.sourceKey,
    );
    _control.addListener(_changed);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) unawaited(_load());
    });
  }

  void _changed() {
    if (!mounted) return;
    if (!_control.current || !_control.storageReady) {
      _recipients = [];
      _ready = false;
    }
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  @override
  void didUpdateWidget(_EntityShareSheet old) {
    super.didUpdateWidget(old);
    if (old.id != widget.id ||
        old.type != widget.type ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl)
      _control.dispose();
  }

  Future<List<_ShareRecipient>> _readRecipients() async {
    final conversations = await _source.conversations(),
        ties = await _source.ties();
    final values = <String, _ShareRecipient>{};
    for (final c in conversations) {
      if (!chatEntityUUID.hasMatch(c.id) ||
          !chatEntityUUID.hasMatch(c.otherAccountId) ||
          c.otherAccountId == _control.owner)
        throw const FormatException('会话收件人不符');
      values[c.otherAccountId] = _ShareRecipient(
        c.otherAccountId,
        c.otherName.trim().isEmpty ? '姓名暂不可用' : c.otherName,
        EntityShareRecipient('CONVERSATION', c.id),
      );
    }
    for (final t in ties) {
      if (!chatEntityUUID.hasMatch(t.id) ||
          !chatEntityUUID.hasMatch(t.otherAccountId) ||
          t.otherAccountId == _control.owner)
        throw const FormatException('好友收件人不符');
      values.putIfAbsent(
        t.otherAccountId,
        () => _ShareRecipient(
          t.otherAccountId,
          t.otherName.trim().isEmpty ? '姓名暂不可用' : t.otherName,
          EntityShareRecipient('FRIEND_TIE', t.id),
        ),
      );
    }
    return values.values.toList();
  }

  Future<void> _load() async {
    final epoch = ++_loadEpoch;
    setState(() {
      _loading = true;
      _ready = false;
      _recipients = [];
      _error = null;
    });
    await _control.load();
    final valid = _control.capture();
    try {
      if (!_control.storageReady || !valid()) return;
      final list = await _readRecipients();
      if (!mounted || epoch != _loadEpoch || !valid()) return;
      setState(() {
        _recipients = list;
        _ready = true;
      });
    } catch (_) {
      if (mounted && epoch == _loadEpoch && valid())
        setState(() {
          _ready = false;
          _recipients = [];
          _error = '当前好友或获准私信暂不可读取。原操作仍可核实。';
        });
    } finally {
      if (mounted && epoch == _loadEpoch) setState(() => _loading = false);
    }
  }

  Future<bool> _recipientCurrent(_ShareRecipient r) async {
    final valid = _control.capture();
    try {
      final list = await _readRecipients();
      return valid() &&
          list.any(
            (n) =>
                n.target.same(r.target) &&
                n.accountID == r.accountID &&
                n.name == r.name,
          );
    } catch (_) {
      return false;
    }
  }

  Future<void> _share(_ShareRecipient r) async {
    if (!_ready || _loading || _control.busy || !_control.newSourceCurrent)
      return;
    final input = EntityShareInput(
      operationID: newEntityShareOperationID(),
      recipient: r.target,
      type: widget.type,
      entityID: widget.id,
    );
    final preview = await _control.prepare(
      input,
      r.accountID,
      () => _recipientCurrent(r),
    );
    if (!mounted || preview == null) return;
    final epoch = _control.epoch;
    var open = true;
    final expiry = Timer.periodic(const Duration(milliseconds: 100), (_) {
      if (open) _control.observe();
    });
    _control.retainDialog(expiry);
    final route = DialogRoute<bool>(
      context: context,
      builder: (_) => ListenableBuilder(
        listenable: _control,
        builder: (dialogContext, _) {
          bool current() => open && _control.approvedCurrent(preview, epoch);
          final live = current();
          return AlertDialog(
            title: Text(
              '检查分享',
              style: NowUiTokens.of(dialogContext).sheetTitle,
            ),
            content: SingleChildScrollView(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (live) ...[
                    Text('收件人：${r.name}'),
                    Text(
                      '个人账号 · ${preview.recipientAccountID.substring(preview.recipientAccountID.length - 6)}',
                    ),
                    const SizedBox(height: 12),
                    Text(
                      preview.entityTitle,
                      style: NowUiTokens.of(dialogContext).placeTitle,
                    ),
                    const SizedBox(height: 12),
                    const Text(
                      '将发送这张当前可见的卡片到双方私信。只包含获准公开资料；动态仅分享本人明确公开的内容。对方之后是否可查看仍取决于当前权限。',
                    ),
                  ] else
                    const Text('这次确认已失效。旧标题与收件人资料已隐藏，不会执行旧批准。请关闭后重新检查。'),
                ],
              ),
            ),
            actions: [
              TextButton(
                onPressed: () {
                  open = false;
                  Navigator.pop(dialogContext, false);
                },
                child: const Text('取消'),
              ),
              FilledButton(
                onPressed: live
                    ? () {
                        if (!current()) return;
                        open = false;
                        Navigator.pop(dialogContext, true);
                      }
                    : null,
                child: const Text('确认发送'),
              ),
            ],
          );
        },
      ),
    );
    route.completed.whenComplete(() => _control.releaseDialog(expiry));
    final approved = await Navigator.of(
      context,
      rootNavigator: true,
    ).push(route);
    open = false;
    if (!mounted) return;
    if (approved == true && _control.approvedCurrent(preview, epoch))
      await _control.submit(preview, epoch, () => _recipientCurrent(r));
    else
      _control.cancelPreview(preview);
    if (mounted && _control.current && _control.receipt?.succeeded == true) {
      await _load();
    }
  }

  Future<void> _stop(PendingEntityShare p) async {
    final valid = _control.capture();
    final approved = await showDialog<bool>(
      context: context,
      builder: (c) => ListenableBuilder(
        listenable: _control,
        builder: (c, _) => AlertDialog(
          title: const Text('停止原分享'),
          content: const Text('只停止这个原操作。若已成功发送，将保留原成功结果，不会撤回消息。'),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(c, false),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: valid()
                  ? () {
                      if (valid()) Navigator.pop(c, true);
                    }
                  : null,
              child: const Text('停止原操作'),
            ),
          ],
        ),
      ),
    );
    if (mounted && approved == true && valid())
      await _control.recover(p, stop: true);
  }

  @override
  void dispose() {
    ++_loadEpoch;
    widget.onDisposed();
    _control.removeListener(_changed);
    _control.dispose();
    _source.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final current = _control.current;
    return SafeArea(
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxHeight: MediaQuery.sizeOf(context).height * .78,
        ),
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(20),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('发给好友', style: NowUiTokens.of(context).sheetTitle),
              if (_loading || _control.busy)
                const Padding(
                  padding: EdgeInsets.all(12),
                  child: LinearProgressIndicator(),
                ),
              if (_control.error != null) Text(_control.error!),
              if (_error != null && current) Text(_error!),
              if (_control.notice != null && current) Text(_control.notice!),
              if (current) ...[
                TextButton(
                  onPressed: _loading || _control.busy ? null : _load,
                  child: const Text('刷新当前状态'),
                ),
                if (_control.pending.isNotEmpty) ...[
                  const SizedBox(height: 16),
                  Text('待核实的原分享', style: NowUiTokens.of(context).sheetTitle),
                  for (final p in _control.pending)
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 8),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            '${ChatEntityCard(type: p.type, available: false).typeLabel} · 原${p.recipientType == 'CONVERSATION' ? '私信' : '好友'}操作',
                            style: NowUiTokens.of(context).placeTitle,
                          ),
                          Wrap(
                            spacing: 8,
                            children: [
                              TextButton(
                                onPressed: _control.busy
                                    ? null
                                    : () => _control.recover(p),
                                child: Text(
                                  _control.locallyPrepared(p)
                                      ? '检查本机准备记录'
                                      : '核实原结果',
                                ),
                              ),
                              if (!_control.locallyPrepared(p))
                                TextButton(
                                  onPressed: _control.busy
                                      ? null
                                      : () => _stop(p),
                                  child: const Text('停止原分享'),
                                ),
                            ],
                          ),
                        ],
                      ),
                    ),
                ],
                if (!_loading && _ready && _recipients.isEmpty)
                  const Text('暂无已接受好友或获准私信。请先建立联系。'),
                if (_ready && _control.newSourceCurrent) ...[
                  const SizedBox(height: 12),
                  const Text('选择收件人后还需检查当前卡片并确认发送。'),
                  for (final r in _recipients)
                    ListTile(
                      title: Text(
                        r.name,
                        style: NowUiTokens.of(context).placeTitle,
                      ),
                      subtitle: Text(
                        '个人账号 · ${r.accountID.substring(r.accountID.length - 6)}',
                        style: NowUiTokens.of(context).metadata,
                      ),
                      leading: const Icon(Icons.person_outline),
                      enabled:
                          !_control.busy && !_loading && _control.storageReady,
                      onTap: () => _share(r),
                    ),
                ],
              ],
              TextButton(
                onPressed: () => Navigator.pop(context),
                child: const Text('关闭'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class ConnectionSource {
  ConnectionSource({
    required this.authorizationHeader,
    http.Client? client,
    String? apiBaseUrl,
    Listenable? identityChanges,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _apiBaseUrl = apiBaseUrl ?? apiBase,
       _identityChanges = identityChanges {
    _identityChanges?.addListener(_pageIdentityChanged);
  }

  static const apiBase = BirdtieEnvironment.apiBaseUrl;
  final String? Function() authorizationHeader;
  final http.Client _client;
  http.Client get followClient => _client;
  String get followApiBaseUrl => _apiBaseUrl;
  final bool _ownsClient;
  final String _apiBaseUrl;
  final Listenable? _identityChanges;
  int _pageIdentityEpoch = 0;
  void _pageIdentityChanged() => ++_pageIdentityEpoch;
  bool _closed = false;
  bool get isClosed => _closed;
  bool get configured => _apiBaseUrl.isNotEmpty;
  String get environment => _apiBaseUrl.replaceFirst(RegExp(r'/$'), '');
  Uri _endpoint(String path) =>
      Uri.parse('${_apiBaseUrl.replaceFirst(RegExp(r'/$'), '')}$path');
  Map<String, String> _headers({bool json = false}) {
    if (_closed) throw StateError('个人消息连接已关闭');
    final token = authorizationHeader();
    if (token == null) throw StateError('请登录后联系其他人');
    return {
      'Authorization': token,
      if (json) 'Content-Type': 'application/json',
    };
  }

  Future<String> currentPersonID() async {
    final response = await _client
        .get(_endpoint('/v1/me'), headers: _headers())
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw StateError('请重新登录');
    final data =
        (jsonDecode(utf8.decode(response.bodyBytes))
            as Map<String, dynamic>)['data'];
    if (data is! Map<String, dynamic> ||
        data['accountType'] != 'person' ||
        data['id'] is! String ||
        !chatEntityUUID.hasMatch(data['id'])) {
      throw const FormatException('当前个人身份不符');
    }
    return data['id'] as String;
  }

  Future<String> shareTargetTitle(String type, String id) async {
    if (!chatEntityTypes.contains(type) || !chatEntityUUID.hasMatch(id)) {
      throw const FormatException('卡片标识不符');
    }
    final path = switch (type) {
      'person' => '/v1/accounts/$id/profile',
      'activity' => '/v1/activities/$id',
      'place' => '/v1/places/$id',
      'community' => '/v1/communities/$id',
      'organization' => '/v1/organizations/$id',
      'business' => '/v1/businesses/$id',
      _ => '/v1/moments/$id',
    };
    final headers = _headers(), epoch = _pageIdentityEpoch;
    final response = await _client
        .get(_endpoint(path), headers: headers)
        .timeout(const Duration(seconds: 12));
    if (_closed ||
        epoch != _pageIdentityEpoch ||
        authorizationHeader() != headers['Authorization']) {
      throw StateError('账号或入口已变化，请重新读取');
    }
    if (response.statusCode != 200) throw StateError('内容暂不可分享');
    final data =
        (jsonDecode(utf8.decode(response.bodyBytes))
            as Map<String, dynamic>)['data'];
    final idKey = type == 'person' ? 'accountId' : 'id';
    final titleKey = switch (type) {
      'person' => 'displayName',
      'activity' || 'moment' => 'title',
      _ => 'name',
    };
    if (data is! Map<String, dynamic> ||
        data[idKey] != id ||
        data[titleKey] is! String ||
        type != 'moment' && (data[titleKey] as String).trim().isEmpty ||
        type == 'person' && data['visibility'] != 'public') {
      throw const FormatException('公开目标不符');
    }
    if (type == 'moment') {
      if (data['body'] is! String) {
        throw const FormatException('动态正文不符');
      }
      // This labels a share preview only. It never changes the Moment title.
      final title = (data['title'] as String).trim();
      if (title.isNotEmpty) return title;
      final body = (data['body'] as String).trim();
      return body.isEmpty ? '分享动态' : String.fromCharCodes(body.runes.take(120));
    }
    return data[titleKey] as String;
  }

  Future<EntityShareReceipt?> recoverEntityShare(
    PendingEntityShare value,
    String ownerID,
  ) async {
    if (!chatEntityUUID.hasMatch(ownerID))
      throw const FormatException('当前个人身份不符');
    final token = authorizationHeader();
    try {
      return await EntityShareControlAPI(
        authorizationHeader: authorizationHeader,
        client: _client,
        apiBaseUrl: _apiBaseUrl,
      ).recover(
        value,
        () => !_closed && token != null && authorizationHeader() == token,
      );
    } on HumanCausalFailure catch (error) {
      if (error.status == 404) return null;
      rethrow;
    }
  }

  Future<HumanMessage> submitEntityShare(
    PendingEntityShare value,
    String ownerID,
  ) async {
    throw StateError('分享需要具体预览和一次批准，不能重放旧提交');
  }

  Future<void> request(
    String recipientAccountId,
    String cityId,
    String note, {
    EntityActionDescriptor? approved,
  }) async {
    if (approved != null &&
        (approved.target != EntityActionRef('person', recipientAccountId) ||
            approved.kind != EntityActionKind.connect ||
            approved.operation != 'REQUEST_CONVERSATION')) {
      throw StateError('私信申请对象或操作已变化。');
    }
    final response = await _client
        .post(
          _endpoint('/v1/me/connection-requests'),
          headers: {
            ..._headers(json: true),
            if (approved != null) ...approved.conditionHeaders,
          },
          body: jsonEncode({
            'recipientAccountId': recipientAccountId,
            'cityId': cityId,
            'note': note,
          }),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 201) {
      throw StateError('Contact request unavailable: ${response.statusCode}');
    }
  }

  Future<void> requestFriend(
    String recipientAccountId,
    String note, {
    EntityActionDescriptor? approved,
  }) async {
    if (approved != null &&
        (approved.target != EntityActionRef('person', recipientAccountId) ||
            approved.kind != EntityActionKind.connect ||
            approved.operation != 'REQUEST_FRIEND')) {
      throw StateError('好友申请对象或操作已变化。');
    }
    final response = await _client
        .post(
          _endpoint('/v1/me/connection-requests'),
          headers: {
            ..._headers(json: true),
            if (approved != null) ...approved.conditionHeaders,
          },
          body: jsonEncode({
            'recipientAccountId': recipientAccountId,
            'scope': 'friend',
            'note': note,
          }),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 201) {
      EntityActionRequestFailure? rejection;
      try {
        rejection = EntityActionRequestFailure.decodeFriendRequest(
          response.statusCode,
          jsonDecode(utf8.decode(response.bodyBytes)),
        );
      } on FormatException {
        // An unreadable receipt cannot prove the original write was refused.
      }
      if (rejection != null) throw rejection;
      throw StateError('好友申请暂不可用：${response.statusCode}');
    }
  }

  Future<SocialListPage<FriendTie>> tiesPage({
    String? cursor,
    int limit = 20,
  }) async {
    final data = await _readChatPage(
      '/v1/me/ties/page',
      cursor: cursor,
      limit: limit,
      fields: const {'items', 'nextCursor'},
    );
    return SocialListPage<FriendTie>.fromJson(
      data,
      decode: socialPageFriend,
      key: (v) => v.otherAccountId,
      limit: limit,
      previousCursor: cursor,
    );
  }

  Future<SocialListPage<ContactRequest>> requestsPage({
    String? cursor,
    int limit = 20,
  }) async {
    final data = await _readChatPage(
      '/v1/me/connection-requests/page',
      cursor: cursor,
      limit: limit,
      fields: const {'items', 'nextCursor'},
    );
    return SocialListPage<ContactRequest>.fromJson(
      data,
      decode: ContactRequest.fromReviewJson,
      key: (v) => v.id,
      limit: limit,
      previousCursor: cursor,
    );
  }

  /// A current single request, including records beyond the first list page.
  /// A missing record is not a receipt or proof that an earlier write failed.
  Future<ContactRequest?> reviewRequest(String id) async {
    if (!_validChatPageID(id)) throw const FormatException('申请标识不符');
    try {
      final root = await _readCurrentChatObject(
        '/v1/me/connection-requests/$id',
      );
      if (root['data'] is! Map<String, dynamic>)
        throw const FormatException('申请来源不符');
      final item = ContactRequest.fromReviewJson(
        root['data'] as Map<String, dynamic>,
      );
      if (item.id != id) throw const FormatException('申请对象已变化');
      return item;
    } on ConnectionTransportFailure catch (e) {
      if (e.status == 404) return null;
      throw ConnectionReviewException(e.status);
    }
  }

  Future<List<FriendTie>> ties() async {
    final response = await _client
        .get(_endpoint('/v1/me/ties'), headers: _headers())
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw StateError('好友列表暂不可用');
    final rows =
        (jsonDecode(utf8.decode(response.bodyBytes))
                as Map<String, dynamic>)['data']
            as List<dynamic>;
    return [
      for (final row in rows) FriendTie.fromJson(row as Map<String, dynamic>),
    ];
  }

  Future<void> removeTie(String id) async {
    final response = await _client
        .delete(
          _endpoint('/v1/me/ties/${Uri.encodeComponent(id)}'),
          headers: _headers(),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 204) throw StateError('移除好友失败');
  }

  Future<void> blockAccount(String accountId) async {
    throw StateError('屏蔽需先读取当前版本、检查具体操作并确认。');
  }

  Future<HumanConversation> startFriendChat(
    String tieId, {
    EntityActionDescriptor? approved,
  }) async {
    if (approved != null &&
        (approved.target.type != 'person' ||
            approved.kind != EntityActionKind.message ||
            approved.operation != 'OPEN_CHAT')) {
      throw StateError('好友会话对象或操作已变化。');
    }
    final response = await _client
        .post(
          _endpoint('/v1/me/ties/${Uri.encodeComponent(tieId)}/conversation'),
          headers: {
            ..._headers(),
            if (approved != null) ...approved.conditionHeaders,
          },
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw StateError('暂时无法开启私信');
    final data =
        (jsonDecode(utf8.decode(response.bodyBytes))
                as Map<String, dynamic>)['data']
            as Map<String, dynamic>;
    return HumanConversation.fromJson(data);
  }

  Future<List<ContactRequest>> requests() async {
    final response = await _client
        .get(_endpoint('/v1/me/connection-requests'), headers: _headers())
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw StateError('Requests unavailable');
    final rows =
        (jsonDecode(utf8.decode(response.bodyBytes))
                as Map<String, dynamic>)['data']
            as List<dynamic>;
    return [
      for (final row in rows)
        ContactRequest.fromJson(row as Map<String, dynamic>),
    ];
  }

  Future<void> decide(String id, String action) async {
    final response = await _client
        .post(
          _endpoint(
            '/v1/me/connection-requests/${Uri.encodeComponent(id)}/decision',
          ),
          headers: _headers(json: true),
          body: jsonEncode({'action': action}),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw StateError('Decision unavailable');
  }

  Future<List<ContactRequest>> reviewRequests() async {
    final response = await _client
        .get(_endpoint('/v1/me/connection-requests'), headers: _headers())
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200)
      throw ConnectionReviewException(response.statusCode);
    final root = jsonDecode(utf8.decode(response.bodyBytes));
    if (root is! Map<String, dynamic> ||
        root['data'] is! List ||
        (root['data'] as List).length > 100) {
      throw const FormatException('申请列表不符');
    }
    return [
      for (final row in root['data'] as List)
        ContactRequest.fromReviewJson(row as Map<String, dynamic>),
    ];
  }

  Future<ConnectionDecisionReceipt> decideReviewed(
    ContactRequest original,
    String action,
  ) async {
    final expected = const {
      'accept': 'accepted',
      'decline': 'declined',
      'withdraw': 'withdrawn',
    }[action];
    if (expected == null || !chatEntityUUID.hasMatch(original.id)) {
      throw const FormatException('申请操作不符');
    }
    final response = await _client
        .post(
          _endpoint('/v1/me/connection-requests/${original.id}/decision'),
          headers: _headers(json: true),
          body: jsonEncode({'action': action}),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) {
      // Only closed original error codes; never retain a server message/body.
      String? code;
      try {
        if (response.bodyBytes.length <= 4096) {
          final failure = jsonDecode(utf8.decode(response.bodyBytes));
          final error = failure is Map<String, dynamic>
              ? failure['error']
              : null;
          if (error is Map<String, dynamic> &&
              const {
                'connection_conflict',
                'invalid_decision',
                'message_policy_changed',
              }.contains(error['code'])) {
            code = error['code'] as String;
          }
        }
      } catch (_) {
        /* An unknown error is not evidence that no write occurred. */
      }
      throw ConnectionReviewException(response.statusCode, errorCode: code);
    }
    final root = jsonDecode(utf8.decode(response.bodyBytes));
    if (root is! Map<String, dynamic> ||
        root['data'] is! Map<String, dynamic>) {
      throw const FormatException('申请回执不符');
    }
    final v = root['data'] as Map<String, dynamic>, chat = v['conversationId'];
    final cid = chat is String ? chat : '';
    if (v.keys.any((k) => !_requestWireFields.contains(k)) ||
        v['id'] != original.id ||
        v['state'] != expected ||
        v['direction'] != null &&
            v['direction'] != '' &&
            v['direction'] != original.direction ||
        v['otherAccountId'] != null &&
            v['otherAccountId'] != '' &&
            v['otherAccountId'] != original.otherAccountId ||
        v['otherName'] != null &&
            v['otherName'] != '' &&
            v['otherName'] != original.otherName ||
        v['scope'] != original.scope ||
        v['note'] != original.note ||
        _requestStamp(v['createdAt']) != original.createdAt ||
        _requestStamp(v['expiresAt']) != original.expiresAt ||
        original.scope == 'friend' && cid.isNotEmpty ||
        original.scope == 'conversation' &&
            expected == 'accepted' &&
            !chatEntityUUID.hasMatch(cid) ||
        expected != 'accepted' && cid.isNotEmpty) {
      throw const FormatException('申请回执与本次操作不符');
    }
    return ConnectionDecisionReceipt(original.id, expected, cid);
  }

  Future<ConnectionRequestOperationReceipt> decideReviewedOperation(
    String owner,
    ContactRequest original,
    String action,
    String operation,
  ) async {
    connectionDecisionDigest(owner, original.id, action);
    if (!chatEntityUUID.hasMatch(operation))
      throw const FormatException('服务操作编号不符');
    final response = await _client
        .post(
          _endpoint('/v1/me/connection-requests/${original.id}/decision'),
          headers: _headers(json: true),
          body: jsonEncode({'action': action, 'operationId': operation}),
        )
        .timeout(const Duration(seconds: 12));
    return _operationReceipt(
      response,
      owner,
      original.id,
      operation,
      action,
      original.scope,
    );
  }

  Future<ConnectionRequestOperationReceipt> readDecisionOperation(
    String owner,
    String request,
    String operation,
    String action,
    String scope,
  ) async {
    connectionDecisionDigest(owner, request, action);
    if (!chatEntityUUID.hasMatch(operation))
      throw const FormatException('服务操作编号不符');
    final response = await _client
        .get(
          _endpoint(
            '/v1/me/connection-requests/$request/decision-operations/$operation',
          ),
          headers: _headers(),
        )
        .timeout(const Duration(seconds: 12));
    return _operationReceipt(
      response,
      owner,
      request,
      operation,
      action,
      scope,
    );
  }

  ConnectionRequestOperationReceipt _operationReceipt(
    http.Response response,
    String owner,
    String request,
    String operation,
    String action,
    String scope,
  ) {
    if (response.statusCode != 200) {
      String? code;
      try {
        if (response.bodyBytes.length <= 4096) {
          final root = jsonDecode(utf8.decode(response.bodyBytes));
          final e = root is Map<String, dynamic> ? root['error'] : null;
          if (e is Map<String, dynamic> &&
              const {
                'connection_operation_changed',
                'invalid_decision',
                'invalid_operation_id',
              }.contains(e['code']))
            code = e['code'];
        }
      } catch (_) {
        /* Unknown errors are not causal receipts. */
      }
      throw ConnectionReviewException(response.statusCode, errorCode: code);
    }
    if (response.bodyBytes.length > 4096) throw const FormatException('服务回执过大');
    final root = jsonDecode(utf8.decode(response.bodyBytes));
    if (root is! Map<String, dynamic> ||
        root.length != 1 ||
        root['data'] is! Map<String, dynamic>)
      throw const FormatException('服务回执格式不符');
    return ConnectionRequestOperationReceipt.decode(
      root['data'],
      owner: owner,
      request: request,
      operation: operation,
      action: action,
      scope: scope,
    );
  }

  Future<List<HumanConversation>> conversations() async {
    final response = await _client
        .get(_endpoint('/v1/me/conversations'), headers: _headers())
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) {
      throw StateError('Conversations unavailable');
    }
    final rows =
        (jsonDecode(utf8.decode(response.bodyBytes))
                as Map<String, dynamic>)['data']
            as List<dynamic>;
    return [
      for (final row in rows)
        HumanConversation.fromJson(row as Map<String, dynamic>),
    ];
  }

  Future<Map<String, dynamic>> _readChatPage(
    String path, {
    String? cursor,
    required int limit,
    required Set<String> fields,
  }) async {
    if (limit < 1 ||
        limit > 50 ||
        cursor != null && !_chatPageCursor.hasMatch(cursor)) {
      throw const FormatException('消息分页参数不符');
    }
    final root = await _readCurrentChatObject(
      path,
      query: {'limit': '$limit', if (cursor != null) 'cursor': cursor},
    );
    final data = _chatPageObject(root['data'], fields), items = data['items'];
    if (items is! List || items.length > limit) {
      throw const FormatException('消息分页数量不符');
    }
    return data;
  }

  Future<Map<String, dynamic>> _readCurrentChatObject(
    String path, {
    Map<String, String>? query,
  }) async {
    final headers = _headers(), epoch = _pageIdentityEpoch;
    final response = await _client
        .get(
          query == null
              ? _endpoint(path)
              : _endpoint(path).replace(queryParameters: query),
          headers: headers,
        )
        .timeout(const Duration(seconds: 12));
    if (_closed ||
        epoch != _pageIdentityEpoch ||
        authorizationHeader() != headers['Authorization']) {
      throw http.ClientException('消息身份已变化');
    }
    if (response.statusCode != 200)
      throw ConnectionTransportFailure(response.statusCode);
    if (response.bodyBytes.length > 512 * 1024) {
      throw const FormatException('消息分页过大');
    }
    return _chatPageObject(jsonDecode(utf8.decode(response.bodyBytes)), const {
      'data',
    });
  }

  Future<HumanConversation> conversation(String id) async {
    if (!_validChatPageID(id)) throw const FormatException('对话对象不符');
    final root = await _readCurrentChatObject('/v1/me/conversations/$id');
    final item = _chatPageConversation(root['data']);
    if (item.id != id) throw const FormatException('对话对象已变化');
    return item;
  }

  Future<HumanConversationListPage> conversationsPage({
    String? cursor,
    int limit = 20,
  }) async {
    final data = await _readChatPage(
      '/v1/me/conversations/page',
      cursor: cursor,
      limit: limit,
      fields: const {'items', 'nextCursor', 'unreadCount'},
    );
    return HumanConversationListPage.fromJson(
      data,
      limit: limit,
      previousCursor: cursor,
    );
  }

  Future<HumanMessagePage> messagesPage(
    String conversationId, {
    String? cursor,
    int limit = 20,
  }) async {
    if (!_validChatPageID(conversationId))
      throw const FormatException('对话对象不符');
    final data = await _readChatPage(
      '/v1/me/conversations/$conversationId/messages/page',
      cursor: cursor,
      limit: limit,
      fields: const {'items', 'nextCursor'},
    );
    final items = [
      for (final row in data['items'] as List)
        _chatPageMessage(row, conversationId),
    ];
    final seen = <String>{};
    HumanMessage? previous;
    for (final message in items) {
      if (!seen.add(message.id) ||
          previous != null && !humanMessageBefore(previous, message)) {
        throw const FormatException('消息分页顺序不符');
      }
      previous = message;
    }
    return HumanMessagePage(
      items: items,
      nextCursor: _chatPageNext(data['nextCursor'], cursor, items.isNotEmpty),
    );
  }

  Future<List<HumanMessage>> messages(String conversationId) async {
    final response = await _client
        .get(
          _endpoint(
            '/v1/me/conversations/${Uri.encodeComponent(conversationId)}/messages',
          ),
          headers: _headers(),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200)
      throw ConnectionTransportFailure(response.statusCode);
    final rows =
        (jsonDecode(utf8.decode(response.bodyBytes))
                as Map<String, dynamic>)['data']
            as List<dynamic>;
    return [
      for (final row in rows)
        HumanMessage.fromJson(row as Map<String, dynamic>),
    ];
  }

  Future<void> markRead(String conversationId, String throughMessageId) async {
    final response = await _client
        .post(
          _endpoint(
            '/v1/me/conversations/${Uri.encodeComponent(conversationId)}/read',
          ),
          headers: _headers(json: true),
          body: jsonEncode({'throughMessageId': throughMessageId}),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) {
      throw ConnectionTransportFailure(response.statusCode);
    }
  }

  Future<HumanMessageOperationReceipt> sendMessageOperation(
    String owner,
    PendingHumanMessage pending,
    String body,
  ) async {
    if (humanMessagePayloadDigest(pending.conversationID, body) !=
        pending.payloadDigest) {
      throw StateError('发送正文已变化，请重新检查。');
    }
    final response = await _client
        .post(
          _endpoint(
            '/v1/me/conversations/${pending.conversationID}/message-operations',
          ),
          headers: _headers(json: true),
          body: jsonEncode({'operationId': pending.operationID, 'body': body}),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 201)
      throw ConnectionTransportFailure(response.statusCode);
    return HumanMessageOperationReceipt.decode(
      (jsonDecode(utf8.decode(response.bodyBytes))
          as Map<String, dynamic>)['data'],
      owner,
      pending,
    );
  }

  Future<HumanMessageOperationReceipt> readMessageOperation(
    String owner,
    PendingHumanMessage pending,
  ) async {
    final response = await _client
        .get(
          _endpoint(
            '/v1/me/conversations/${pending.conversationID}/message-operations/${pending.operationID}',
          ),
          headers: _headers(),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200)
      throw ConnectionTransportFailure(response.statusCode);
    return HumanMessageOperationReceipt.decode(
      (jsonDecode(utf8.decode(response.bodyBytes))
          as Map<String, dynamic>)['data'],
      owner,
      pending,
    );
  }

  Future<void> send(String conversationId, String body) async {
    final response = await _client
        .post(
          _endpoint(
            '/v1/me/conversations/${Uri.encodeComponent(conversationId)}/messages',
          ),
          headers: _headers(json: true),
          body: jsonEncode({'body': body}),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 201) throw StateError('消息发送失败');
  }

  Future<void> sendEntity(String conversationId, String type, String id) async {
    throw StateError('卡片分享需要具体预览和批准，请使用发给好友入口');
  }

  void dispose() {
    if (_closed) return;
    _closed = true;
    _identityChanges?.removeListener(_pageIdentityChanged);
    if (_ownsClient) _client.close();
  }
}

class SocialInboxSection extends StatefulWidget {
  const SocialInboxSection({
    super.key,
    required this.auth,
    this.workspaceChanges,
    this.workspaceID,
    this.client,
    this.apiBaseUrl,
  });
  final BirdtieAuthController auth;
  final Listenable? workspaceChanges;
  final String? Function()? workspaceID;
  final http.Client? client;
  final String? apiBaseUrl;

  @override
  State<SocialInboxSection> createState() => _SocialInboxSectionState();
}

class _SocialInboxSectionState extends State<SocialInboxSection> {
  late ConnectionSource _source;
  int _identityEpoch = 0;
  final _reviewChanges = ValueNotifier<int>(0);
  final _reportSource = HumanCausalSourceSerial();
  void _observeReportSource() => _reportSource.observe((
    widget.auth,
    widget.auth.accountID,
    widget.workspaceID?.call(),
    widget.client,
    widget.apiBaseUrl,
  ));
  List<ContactRequest> _requests = const [];
  List<FriendTie> _ties = const [];
  List<HumanConversation> _conversations = const [];
  bool _loading = false;
  bool _failed = false;
  final _nextPages = <String, String?>{};
  final _pageBusy = <String>{},
      _pageErrors = <String>{},
      _pageRestart = <String>{};
  final _pageCursors = <String, Set<String>>{};
  void _resetPages() {
    _nextPages.clear();
    _pageBusy.clear();
    _pageErrors.clear();
    _pageRestart.clear();
    _pageCursors.clear();
  }

  String? _busyID;
  int _serial = 0;
  late (String?, String?, String?) _identity;
  (String?, String?, String?) get _currentIdentity => (
    widget.auth.authorizationHeader,
    widget.auth.accountID,
    widget.workspaceID?.call(),
  );

  @override
  void initState() {
    super.initState();
    _identity = _currentIdentity;
    _observeReportSource();
    widget.auth.addListener(_identityChanged);
    widget.workspaceChanges?.addListener(_identityChanged);
    _source = ConnectionSource(
      authorizationHeader: () => widget.auth.authorizationHeader,
      client: widget.client,
      apiBaseUrl: widget.apiBaseUrl,
    );
    if (widget.auth.signedIn &&
        widget.workspaceID?.call() == null &&
        _source.configured)
      unawaited(_load());
  }

  void _identityChanged() {
    _observeReportSource();
    if (_identity == _currentIdentity) return;
    _identity = _currentIdentity;
    ++_serial;
    ++_identityEpoch;
    _reviewChanges.value++;
    setState(() {
      _resetPages();
      _requests = const [];
      _ties = const [];
      _conversations = const [];
      _loading = false;
      _failed = false;
      _busyID = null;
    });
    if (widget.auth.signedIn &&
        widget.workspaceID?.call() == null &&
        _source.configured) {
      unawaited(_load());
    }
  }

  @override
  void didUpdateWidget(SocialInboxSection old) {
    super.didUpdateWidget(old);
    _observeReportSource();
    if (old.auth != widget.auth) {
      old.auth.removeListener(_identityChanged);
      widget.auth.addListener(_identityChanged);
    }
    if (old.workspaceChanges != widget.workspaceChanges) {
      old.workspaceChanges?.removeListener(_identityChanged);
      widget.workspaceChanges?.addListener(_identityChanged);
    }
    if (old.auth != widget.auth ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.workspaceChanges != widget.workspaceChanges ||
        old.workspaceID != widget.workspaceID) {
      ++_serial;
      ++_identityEpoch;
      _reviewChanges.value++;
      _source.dispose();
      _source = ConnectionSource(
        authorizationHeader: () => widget.auth.authorizationHeader,
        client: widget.client,
        apiBaseUrl: widget.apiBaseUrl,
      );
      _identity = _currentIdentity;
      _resetPages();
      _requests = const [];
      _ties = const [];
      _conversations = const [];
      _loading = false;
      _failed = false;
      _busyID = null;
      if (widget.auth.signedIn &&
          widget.workspaceID?.call() == null &&
          _source.configured) {
        unawaited(_load());
      }
    } else {
      _identityChanged();
    }
  }

  Future<void> _load() async {
    if (!widget.auth.signedIn || widget.workspaceID?.call() != null || _loading)
      return;
    final serial = ++_serial;
    setState(() {
      _resetPages();
      _loading = true;
      _failed = false;
    });
    try {
      final requests = await _source.requestsPage();
      if (!mounted || serial != _serial) return;
      final ties = await _source.tiesPage();
      if (!mounted || serial != _serial) return;
      final conversations = await _source.conversationsPage();
      if (!mounted || serial != _serial) return;
      setState(() {
        _requests = requests.items;
        _ties = ties.items;
        _conversations = conversations.items;
        _nextPages.addAll({
          'requests': requests.nextCursor,
          'ties': ties.nextCursor,
          'conversations': conversations.nextCursor,
        });
      });
    } catch (_) {
      if (mounted && serial == _serial) setState(() => _failed = true);
    } finally {
      if (mounted && serial == _serial) setState(() => _loading = false);
    }
  }

  Future<void> _loadMore(String kind) async {
    final cursor = _nextPages[kind],
        source = _source,
        serial = _serial,
        identity = _currentIdentity;
    bool current() =>
        mounted &&
        serial == _serial &&
        source == _source &&
        identity == _currentIdentity &&
        widget.auth.signedIn &&
        widget.workspaceID?.call() == null;
    if (!current() || _loading || _pageBusy.contains(kind) || cursor == null)
      return;
    setState(() {
      _pageBusy.add(kind);
      _pageErrors.remove(kind);
    });
    try {
      String? next;
      final page = switch (kind) {
        'requests' => await source.requestsPage(cursor: cursor),
        'ties' => await source.tiesPage(cursor: cursor),
        _ => await source.conversationsPage(cursor: cursor),
      };
      if (!current()) return;
      next = switch (page) {
        SocialListPage<Object> p => p.nextCursor,
        HumanConversationListPage p => p.nextCursor,
        _ => throw const FormatException('联系人分页不符'),
      };
      if (next != null && (_pageCursors[kind]?.contains(next) ?? false))
        throw const FormatException('联系人分页无法继续');
      setState(() {
        if (page is SocialListPage<ContactRequest>) {
          _requests = List.unmodifiable(
            <String, ContactRequest>{
              for (final v in _requests) v.id: v,
              for (final v in page.items) v.id: v,
            }.values,
          );
        } else if (page is SocialListPage<FriendTie>) {
          _ties = List.unmodifiable(
            <String, FriendTie>{
              for (final v in _ties) v.otherAccountId: v,
              for (final v in page.items) v.otherAccountId: v,
            }.values,
          );
        } else if (page is HumanConversationListPage) {
          _conversations = List.unmodifiable(
            <String, HumanConversation>{
              for (final v in _conversations) v.id: v,
              for (final v in page.items) v.id: v,
            }.values,
          );
        }
        _nextPages[kind] = next;
        (_pageCursors[kind] ??= <String>{}).add(cursor);
      });
    } catch (e) {
      if (current())
        setState(() {
          if (e is ConnectionTransportFailure &&
              (e.status == 401 || e.status == 403)) {
            _requests = const [];
            _ties = const [];
            _conversations = const [];
            _resetPages();
            _failed = true;
          } else {
            _pageErrors.add(kind);
            if (e is ConnectionTransportFailure && e.status == 409)
              _pageRestart.add(kind);
          }
        });
    } finally {
      if (current()) setState(() => _pageBusy.remove(kind));
    }
  }

  Widget _morePage(String kind, String label) => TextButton(
    key: ValueKey('social-management-more-$kind'),
    style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
    onPressed: _loading || _pageBusy.contains(kind)
        ? null
        : _pageRestart.contains(kind)
        ? _load
        : () => _loadMore(kind),
    child: Text(
      _pageBusy.contains(kind)
          ? '正在加载'
          : _pageRestart.contains(kind)
          ? '列表已更新，重新加载'
          : _pageErrors.contains(kind)
          ? '重试加载更多'
          : label,
    ),
  );

  Future<void> _decide(ContactRequest request, String action) async {
    if (_busyID != null) return;
    final identity = _currentIdentity,
        epoch = _identityEpoch,
        source = _source,
        auth = widget.auth;
    bool current() =>
        mounted &&
        epoch == _identityEpoch &&
        identity == _currentIdentity &&
        identical(source, _source) &&
        identical(auth, widget.auth) &&
        widget.workspaceID?.call() == null &&
        source.authorizationHeader() == identity.$1;
    if (!current()) return;
    final workspaceChanges = widget.workspaceChanges,
        workspaceID = widget.workspaceID;
    final changes = Listenable.merge([auth, workspaceChanges, _reviewChanges]);
    setState(() => _busyID = request.id);
    try {
      await Navigator.of(context).push(
        MaterialPageRoute<void>(
          builder: (_) => NotificationDestinationBoundary(
            identityChanges: changes,
            current: current,
            builder: (_) => ConnectionRequestReviewPage(
              auth: auth,
              source: source,
              requestID: request.id,
              initialAction: action,
              current: current,
              workspaceChanges: workspaceChanges,
              organizationWorkspaceID: workspaceID,
            ),
          ),
        ),
      );
      if (current()) await _load();
    } catch (_) {
      if (mounted && current()) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('申请暂不可审阅，请重新核实。')));
      }
    } finally {
      if (current()) setState(() => _busyID = null);
    }
  }

  Future<void> _manageTie(FriendTie tie, String action) async {
    if (_busyID != null) return;
    final identity = _identity, serial = _serial;
    bool current() =>
        mounted && identity == _currentIdentity && serial == _serial;
    if (action == 'report') {
      await _reportAccount(tie.otherAccountId);
      return;
    }
    if (action == 'chat') {
      setState(() => _busyID = tie.id);
      try {
        final conversation = await _source.startFriendChat(tie.id);
        if (current()) unawaited(_openConversation(conversation));
      } catch (_) {
        if (mounted) {
          ScaffoldMessenger.of(
            context,
          ).showSnackBar(const SnackBar(content: Text('暂时无法打开私信，请稍后重试。')));
        }
      } finally {
        if (mounted) setState(() => _busyID = null);
      }
      return;
    }
    if (action == 'block') {
      await _openBlock(tie.otherAccountId);
      return;
    }
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text('移除好友 ${tie.otherName}？'),
        content: const Text('移除后双方不再是好友。若要重新成为好友，需要重新发送并接受申请。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialogContext, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(dialogContext, true),
            child: const Text('确认移除'),
          ),
        ],
      ),
    );
    if (confirmed != true || !current()) return;
    setState(() => _busyID = tie.id);
    try {
      await _source.removeTie(tie.id);
      await _load();
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('好友操作失败，请重试。')));
      }
    } finally {
      if (mounted) setState(() => _busyID = null);
    }
  }

  Future<void> _openCloseConnections() async {
    final source = _source,
        epoch = _identityEpoch,
        identity = _currentIdentity,
        auth = widget.auth;
    if (!auth.signedIn || identity.$3 != null || !source.configured) return;
    final workspaceChanges = Listenable.merge([
      widget.workspaceChanges,
      _reviewChanges,
    ]);
    final workspaceID = widget.workspaceID;
    bool bindingCurrent() =>
        mounted &&
        identical(source, _source) &&
        epoch == _identityEpoch &&
        identity == _currentIdentity &&
        identical(auth, widget.auth);
    await Navigator.push(
      context,
      MaterialPageRoute<void>(
        builder: (_) => CloseConnectionsPage(
          auth: auth,
          client: source.followClient,
          apiBaseUrl: source.followApiBaseUrl,
          workspaceChanges: workspaceChanges,
          organizationWorkspaceID: workspaceID,
          bindingCurrent: bindingCurrent,
        ),
      ),
    );
  }

  Future<void> _openConversation(HumanConversation conversation) async {
    final identity = _identity, serial = _serial;
    bool current() =>
        mounted && identity == _currentIdentity && serial == _serial;
    await Navigator.push(
      context,
      MaterialPageRoute<void>(
        builder: (context) => HumanConversationRoute(
          auth: widget.auth,
          conversation: conversation,
          workspaceChanges: widget.workspaceChanges,
          workspaceID: widget.workspaceID,
          actions: [
            PopupMenuButton<String>(
              tooltip: '对话安全操作',
              onSelected: (action) async {
                if (!current()) return;
                if (action == 'report') {
                  await _reportAccount(conversation.otherAccountId);
                } else if (action == 'block') {
                  await _blockConversationContact(conversation);
                }
              },
              itemBuilder: (context) => const [
                PopupMenuItem(value: 'report', child: Text('举报此账号')),
                PopupMenuItem(value: 'block', child: Text('屏蔽此账号')),
              ],
            ),
          ],
        ),
      ),
    );
    if (mounted) unawaited(_load());
  }

  Future<void> _reportAccount(String accountId) {
    return Navigator.push(
      context,
      MaterialPageRoute<void>(
        builder: (context) => humanSupportDestination(
          authorizationHeader: () => widget.auth.authorizationHeader,
          ownerID: () => widget.auth.accountID,
          identityChanges: Listenable.merge([
            widget.auth,
            widget.workspaceChanges,
            _reportSource,
          ]),
          sourceCurrent: () => mounted && widget.workspaceID?.call() == null,
          sourceKey: () => _reportSource.generation,
          targetType: 'account',
          targetID: accountId,
          client: _source.followClient,
          apiBaseUrl: _source.environment,
        ),
      ),
    );
  }

  Future<void> _blockConversationContact(HumanConversation conversation) async {
    await _openBlock(conversation.otherAccountId);
  }

  Future<void> _openBlock(String id) async {
    final auth = widget.auth, source = _source, owner = widget.auth.accountID;
    bool current() =>
        mounted &&
        auth == widget.auth &&
        identical(source, _source) &&
        owner == auth.accountID &&
        auth.signedIn &&
        widget.workspaceID?.call() == null;
    if (!current()) return;
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => HumanCausalEntryBoundary(
          ownerID: () => auth.accountID,
          authorizationHeader: () => auth.authorizationHeader,
          sourceKey: () => (
            widget.auth,
            widget.workspaceID?.call(),
            widget.client,
            widget.apiBaseUrl,
          ),
          identityChanges: Listenable.merge([auth, widget.workspaceChanges]),
          sourceCurrent: current,
          builder: (_, live) => PrivacyControlPage(
            family: HumanCausalFamily.block,
            targetID: id,
            authorizationHeader: () => auth.authorizationHeader,
            ownerID: () => auth.accountID,
            identityChanges: Listenable.merge([auth, widget.workspaceChanges]),
            sourceCurrent: live,
            client: source.followClient,
            apiBaseUrl: source.environment,
          ),
        ),
      ),
    );
    if (current()) await _load();
  }

  @override
  void dispose() {
    _reportSource.dispose();
    ++_serial;
    ++_identityEpoch;
    _reviewChanges.value++;
    widget.auth.removeListener(_identityChanged);
    widget.workspaceChanges?.removeListener(_identityChanged);
    _source.dispose();
    _reviewChanges.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!widget.auth.signedIn ||
        widget.workspaceID?.call() != null ||
        !_source.configured) {
      return const SizedBox.shrink();
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            const Expanded(child: Text('好友与对话')),
            IconButton(
              tooltip: '写社交意图草稿',
              onPressed: () => Navigator.push(
                context,
                MaterialPageRoute<void>(
                  builder: (context) => SocialIntentDraftPage(
                    authorizationHeader: () => widget.auth.authorizationHeader,
                    authorizationChanges: widget.auth,
                  ),
                ),
              ),
              icon: const Icon(Icons.edit_note),
            ),
            IconButton(
              tooltip: '刷新好友、申请与对话',
              onPressed: _loading ? null : _load,
              icon: const Icon(Icons.refresh),
            ),
          ],
        ),
        TextButton.icon(
          onPressed: _openCloseConnections,
          icon: const Icon(Icons.people_outline),
          label: const Text('管理亲密好友'),
        ),
        if (_loading) const LinearProgressIndicator(),
        if (_failed)
          TextButton(onPressed: _load, child: const Text('联系人加载失败，点击重试')),
        for (final request in _requests)
          ListTile(
            contentPadding: EdgeInsets.zero,
            leading: PersonAvatar(
              metadata: request.avatar,
              authorizationHeader: () => widget.auth.authorizationHeader,
              baseUrl: _source.environment,
              client: _source.followClient,
              identityChanges: _reviewChanges,
              current: () =>
                  widget.auth.signedIn && widget.workspaceID?.call() == null,
            ),
            title: Text(request.otherName),
            subtitle: Text(
              '${request.scope == 'friend' ? '好友申请' : '联系申请'} · ${request.direction == 'incoming' ? '收到' : '已发送'} · ${_requestState(request.state)}${request.pendingReview ? '\n待人工审阅；尚未进行 Agent 筛查。' : ''}${request.expiresAt == null ? '' : '\n有效期至 ${request.expiresAt!.toLocal().year}年${request.expiresAt!.toLocal().month}月${request.expiresAt!.toLocal().day}日（设备当地时间）'}${request.note.isEmpty ? '' : '\n${request.note}'}',
              maxLines: 5,
              overflow: TextOverflow.ellipsis,
            ),
            trailing: request.state != 'pending'
                ? null
                : PopupMenuButton<String>(
                    tooltip: '申请操作',
                    enabled: _busyID == null,
                    onSelected: (action) => _decide(request, action),
                    itemBuilder: (context) => request.direction == 'incoming'
                        ? const [
                            PopupMenuItem(value: 'accept', child: Text('接受')),
                            PopupMenuItem(value: 'decline', child: Text('拒绝')),
                          ]
                        : const [
                            PopupMenuItem(value: 'withdraw', child: Text('撤回')),
                          ],
                  ),
          ),
        if (_nextPages['requests'] != null) _morePage('requests', '更多申请'),
        for (final tie in _ties)
          ListTile(
            contentPadding: EdgeInsets.zero,
            leading: PersonAvatar(
              metadata: tie.avatar,
              authorizationHeader: () => widget.auth.authorizationHeader,
              baseUrl: _source.environment,
              client: _source.followClient,
              identityChanges: _reviewChanges,
              current: () =>
                  widget.auth.signedIn && widget.workspaceID?.call() == null,
            ),
            title: Text(tie.otherName),
            subtitle: const Text('好友'),
            trailing: PopupMenuButton<String>(
              tooltip: '好友操作',
              enabled: _busyID == null,
              onSelected: (action) => _manageTie(tie, action),
              itemBuilder: (context) => const [
                PopupMenuItem(value: 'chat', child: Text('发消息')),
                PopupMenuItem(value: 'remove', child: Text('移除好友')),
                PopupMenuItem(value: 'block', child: Text('屏蔽账号')),
                PopupMenuItem(value: 'report', child: Text('举报此账号')),
              ],
            ),
          ),
        if (_nextPages['ties'] != null) _morePage('ties', '更多好友'),
        for (final conversation in _conversations)
          ListTile(
            contentPadding: EdgeInsets.zero,
            leading: PersonAvatar(
              metadata: conversation.avatar,
              authorizationHeader: () => widget.auth.authorizationHeader,
              baseUrl: _source.environment,
              client: _source.followClient,
              identityChanges: _reviewChanges,
              current: () =>
                  widget.auth.signedIn && widget.workspaceID?.call() == null,
            ),
            title: Text(conversation.otherName),
            subtitle: Text(
              conversation.unreadCount > 0
                  ? '${conversation.unreadCount} 条未读消息'
                  : '私信对话',
            ),
            onTap: () => _openConversation(conversation),
            trailing: const Icon(Icons.chevron_right),
          ),
        if (_nextPages['conversations'] != null)
          _morePage('conversations', '更多会话'),
        if (!_loading &&
            !_failed &&
            _requests.isEmpty &&
            _ties.isEmpty &&
            _conversations.isEmpty)
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 12),
            child: Text('还没有好友、申请或对话。'),
          ),
        const Divider(height: 28),
      ],
    );
  }
}

/// Binds the outer title and safety actions to the same identity as the body.
class HumanConversationRoute extends StatefulWidget {
  const HumanConversationRoute({
    super.key,
    required this.auth,
    required this.conversation,
    this.workspaceChanges,
    this.workspaceID,
    this.actions = const [],
    this.apiBaseUrl,
    this.client,
    this.pendingStore,
    this.messagePendingStore,
  });
  final BirdtieAuthController auth;
  final HumanConversation conversation;
  final Listenable? workspaceChanges;
  final String? Function()? workspaceID;
  final List<Widget> actions;
  final String? apiBaseUrl;
  final http.Client? client;
  final EntitySharePendingStore? pendingStore;
  final HumanMessagePendingStore? messagePendingStore;
  @override
  State<HumanConversationRoute> createState() => _HumanConversationRouteState();
}

class _RetiredHumanConversation extends StatelessWidget {
  const _RetiredHumanConversation();
  @override
  Widget build(BuildContext context) => SafeArea(
    child: ListView(
      padding: const EdgeInsets.all(24),
      children: [
        const Text('工作身份已变化，或消息连接已更改。请返回个人消息，重新选择会话。'),
        const SizedBox(height: 16),
        TextButton(
          style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
          onPressed: () => Navigator.of(context).maybePop(),
          child: const Text('返回个人消息'),
        ),
      ],
    ),
  );
}

class _HumanConversationRouteState extends State<HumanConversationRoute> {
  final _reportSource = HumanCausalSourceSerial();
  void _observeReportSource() => _reportSource.observe((
    widget.auth,
    widget.auth.accountID,
    widget.workspaceID?.call(),
    widget.conversation.id,
    widget.conversation.otherAccountId,
    widget.client,
    widget.apiBaseUrl,
  ));
  late final (String?, String?, String?) _identity;
  late final Listenable _changes;
  bool _expired = false;
  bool _listening = true;
  (String?, String?, String?) get _current => (
    widget.auth.authorizationHeader,
    widget.auth.accountID,
    widget.workspaceID?.call(),
  );
  @override
  void initState() {
    super.initState();
    _identity = _current;
    _observeReportSource();
    _changes = Listenable.merge([widget.auth, widget.workspaceChanges]);
    _changes.addListener(_changed);
  }

  void _changed() {
    _observeReportSource();
    if (_expired || _identity == _current) return;
    _retire();
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  void _retire() {
    _expired = true;
    if (_listening) {
      _listening = false;
      _changes.removeListener(_changed);
    }
  }

  @override
  void didUpdateWidget(HumanConversationRoute old) {
    super.didUpdateWidget(old);
    _observeReportSource();
    if (old.auth != widget.auth ||
        old.workspaceChanges != widget.workspaceChanges ||
        old.workspaceID != widget.workspaceID ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.client != widget.client ||
        old.pendingStore != widget.pendingStore ||
        old.messagePendingStore != widget.messagePendingStore ||
        old.conversation.id != widget.conversation.id ||
        old.conversation.otherAccountId != widget.conversation.otherAccountId ||
        _identity != _current) {
      _retire();
    }
  }

  @override
  void dispose() {
    _reportSource.dispose();
    _retire();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final expired = _expired || _identity.$3 != null;
    return Scaffold(
      appBar: birdtieAppBar(
        context,
        title: Text(expired ? '个人消息' : widget.conversation.otherName),
        actions: expired ? const [] : widget.actions,
      ),
      body: expired
          ? const _RetiredHumanConversation()
          : HumanConversationPage(
              auth: widget.auth,
              conversation: widget.conversation,
              workspaceChanges: widget.workspaceChanges,
              workspaceID: widget.workspaceID,
              apiBaseUrl: widget.apiBaseUrl,
              client: widget.client,
              pendingStore: widget.pendingStore,
              messagePendingStore: widget.messagePendingStore,
              reportSourceChanges: _reportSource,
              reportSourceKey: () => _reportSource.generation,
              reportSourceCurrent: () =>
                  mounted && widget.workspaceID?.call() == null,
            ),
    );
  }
}

class HumanConversationPage extends StatefulWidget {
  const HumanConversationPage({
    super.key,
    required this.auth,
    required this.conversation,
    this.workspaceChanges,
    this.workspaceID,
    this.apiBaseUrl,
    this.client,
    this.pendingStore,
    this.messagePendingStore,
    this.reportSourceChanges,
    this.reportSourceKey,
    this.reportSourceCurrent,
  });
  final BirdtieAuthController auth;
  final HumanConversation conversation;
  final Listenable? workspaceChanges;
  final String? Function()? workspaceID;
  final String? apiBaseUrl;
  final http.Client? client;
  final EntitySharePendingStore? pendingStore;
  final HumanMessagePendingStore? messagePendingStore;
  final Listenable? reportSourceChanges;
  final Object? Function()? reportSourceKey;
  final bool Function()? reportSourceCurrent;
  @override
  State<HumanConversationPage> createState() => _HumanConversationPageState();
}

class _HumanConversationPageState extends State<HumanConversationPage> {
  final _text = TextEditingController();
  late final ConnectionSource _source;
  late final EntitySharePendingStore _pendingStore;
  late final HumanMessagePendingStore _messageStore;
  List<PendingHumanMessage> _messagePending = [];
  bool _journalReady = false;
  String? _messageFeedback;
  late final (String?, String?, String?) _identity;
  late final Listenable _changes;
  List<HumanMessage> _messages = [];
  List<PendingEntityShare> _pending = [];
  bool _loading = false, _sending = false, _failed = false, _expired = false;
  bool _listening = true;
  int _serial = 0;
  final _reportSource = HumanCausalSourceSerial();
  void _observeReportSource() => _reportSource.observe((
    widget.auth,
    widget.auth.accountID,
    widget.workspaceID?.call(),
    widget.conversation.id,
    widget.client,
    widget.apiBaseUrl,
  ));
  (String?, String?, String?) get _current => (
    widget.auth.authorizationHeader,
    widget.auth.accountID,
    widget.workspaceID?.call(),
  );
  bool get _valid => mounted && !_expired && _identity == _current;
  @override
  void initState() {
    super.initState();
    _identity = _current;
    _observeReportSource();
    _source = ConnectionSource(
      authorizationHeader: () => _valid ? _identity.$1 : null,
      apiBaseUrl: widget.apiBaseUrl,
      client: widget.client,
    );
    _pendingStore =
        widget.pendingStore ?? const SecureEntitySharePendingStore();
    _messageStore =
        widget.messagePendingStore ?? SecureHumanMessagePendingStore();
    _changes = Listenable.merge([widget.auth, widget.workspaceChanges]);
    _changes.addListener(_changed);
    unawaited(_load());
  }

  void _changed() {
    _observeReportSource();
    if (_identity == _current || _expired) return;
    _retire();
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  void _retire() {
    _expired = true;
    ++_serial;
    _messages = [];
    _pending = [];
    _messagePending = [];
    _journalReady = false;
    _messageFeedback = null;
    _loading = false;
    _sending = false;
    _failed = false;
    _text.clear();
    if (_listening) {
      _listening = false;
      _changes.removeListener(_changed);
    }
  }

  @override
  void didUpdateWidget(HumanConversationPage old) {
    super.didUpdateWidget(old);
    _observeReportSource();
    if (old.auth != widget.auth ||
        old.conversation.id != widget.conversation.id ||
        old.workspaceChanges != widget.workspaceChanges ||
        old.workspaceID != widget.workspaceID ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.client != widget.client ||
        old.pendingStore != widget.pendingStore ||
        old.messagePendingStore != widget.messagePendingStore ||
        old.conversation.otherAccountId != widget.conversation.otherAccountId ||
        _identity != _current) {
      _retire();
    }
  }

  Future<void> _load() async {
    if (!_valid || _identity.$3 != null) return;
    final serial = ++_serial;
    setState(() {
      _loading = true;
      _failed = false;
      _journalReady = false;
    });
    try {
      final owner = _identity.$2;
      if (owner == null || !chatEntityUUID.hasMatch(owner))
        throw StateError('请在本人账号中读取发送记录。');
      unawaited(_loadMessageJournal(serial, owner));
      if (!_valid || serial != _serial) return;
      final messages = await _source.messages(widget.conversation.id);
      if (!_valid || serial != _serial) return;
      if (messages.isNotEmpty) {
        await _source.markRead(widget.conversation.id, messages.last.id);
        if (!_valid || serial != _serial) return;
      }
      setState(() => _messages = messages);
      if (chatEntityUUID.hasMatch(owner)) {
        final pending = await _pendingStore.read(_source.environment, owner);
        if (_valid && serial == _serial) {
          setState(
            () => _pending = pending
                .where((p) => p.conversationID == widget.conversation.id)
                .toList(),
          );
        }
      }
    } catch (_) {
      if (_valid && serial == _serial) setState(() => _failed = true);
    } finally {
      if (_valid && serial == _serial) setState(() => _loading = false);
    }
  }

  Future<void> _loadMessageJournal(int serial, String owner) async {
    if (!_messageCurrent(serial)) return;
    try {
      final journal = await _messageStore.read(_source.environment, owner);
      if (!_messageCurrent(serial)) return;
      setState(() {
        _messagePending = journal
            .where((p) => p.conversationID == widget.conversation.id)
            .toList();
        _journalReady = true;
      });
    } catch (_) {
      if (!_messageCurrent(serial)) return;
      setState(() {
        _journalReady = false;
        _messageFeedback = '发送恢复记录暂不可读取，新消息暂不能发送；请刷新核对。';
      });
    }
  }

  bool _messageCurrent(int serial) =>
      _valid && serial == _serial && _identity.$3 == null;

  Future<void> _send() async {
    final body = _text.text.trim(), serial = _serial, owner = _identity.$2;
    if (body.isEmpty ||
        _sending ||
        !_messageCurrent(serial) ||
        owner == null ||
        !_journalReady ||
        _messagePending.isNotEmpty) {
      return;
    }
    if (utf8.encode(body).length > 2000 || body.contains('\u0000')) {
      setState(() => _messageFeedback = '消息过长，请缩短后再发送。');
      return;
    }
    final p = PendingHumanMessage(
      operationID: newEntityShareOperationID(),
      conversationID: widget.conversation.id,
      payloadDigest: humanMessagePayloadDigest(widget.conversation.id, body),
    );
    setState(() => _sending = true);
    var journalAttempted = false;
    try {
      journalAttempted = true;
      await _messageStore.write(_source.environment, owner, p);
      if (!_messageCurrent(serial)) return;
      setState(() => _messagePending = [..._messagePending, p]);
      await _source.sendMessageOperation(owner, p, body);
      if (!_messageCurrent(serial)) return;
      await _messageStore.delete(_source.environment, owner, p);
      if (!_messageCurrent(serial)) return;
      setState(() {
        _messagePending.removeWhere((v) => v.operationID == p.operationID);
        _sending = false;
        _messageFeedback = '已核实消息已保存到会话，不代表对方已阅读。';
      });
      if (humanMessagePayloadDigest(
            widget.conversation.id,
            _text.text.trim(),
          ) ==
          p.payloadDigest) {
        _text.clear();
      }
      await _load();
    } catch (_) {
      if (_messageCurrent(serial)) {
        setState(() {
          // Even a storage error may have persisted metadata. Re-read before new writes.
          if (_messagePending.isEmpty && journalAttempted)
            _journalReady = false;
          _messageFeedback = _messagePending.isEmpty
              ? '发送记录未获确认，消息没有继续提交；请刷新核对恢复记录。'
              : '发送结果暂未确认，请核实这次发送，不要重复发送。';
        });
      }
    } finally {
      if (_messageCurrent(serial)) setState(() => _sending = false);
    }
  }

  Future<void> _verifyMessage(PendingHumanMessage p) async {
    final serial = _serial, owner = _identity.$2;
    if (!_messageCurrent(serial) ||
        _sending ||
        owner == null ||
        !_messagePending.contains(p))
      return;
    setState(() => _sending = true);
    try {
      await _source.readMessageOperation(owner, p);
      if (!_messageCurrent(serial) || !_messagePending.contains(p)) return;
      await _messageStore.delete(_source.environment, owner, p);
      if (!_messageCurrent(serial)) return;
      setState(() {
        _messagePending.removeWhere((v) => v.operationID == p.operationID);
        _messageFeedback = '已核实原消息已保存到会话，不代表对方已阅读。';
        _sending = false;
      });
      // Preserve an unrelated edited draft; never restore the original message text.
      if (humanMessagePayloadDigest(
            widget.conversation.id,
            _text.text.trim(),
          ) ==
          p.payloadDigest)
        _text.clear();
      await _load();
    } catch (_) {
      if (_messageCurrent(serial))
        setState(() => _messageFeedback = '尚无法核实这次发送；原记录已保留，不会自动重发。');
    } finally {
      if (_messageCurrent(serial)) setState(() => _sending = false);
    }
  }

  Future<void> _recover(PendingEntityShare pending, BuildContext inner) async {
    if (!_valid) return;
    await shareEntityToChat(
      inner,
      authorizationHeader: () => widget.auth.authorizationHeader,
      ownerID: () => widget.auth.accountID,
      sourceKey: () => _reportSource.generation,
      sourceCurrent: () => mounted && !_expired,
      identityChanges: Listenable.merge([_changes, _reportSource]),
      workspaceID: widget.workspaceID,
      apiBaseUrl: widget.apiBaseUrl,
      client: widget.client,
      type: pending.type,
      id: pending.entityID,
      pendingStore: _pendingStore,
    );
    if (_valid) await _load();
  }

  @override
  void dispose() {
    _retire();
    _reportSource.dispose();
    _source.dispose();
    _text.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (_expired || _identity.$3 != null) {
      return const _RetiredHumanConversation();
    }
    return NotificationDestinationBoundary(
      identityChanges: _changes,
      current: () => _valid && _identity.$3 == null,
      builder: _buildConversation,
    );
  }

  Widget _buildConversation(BuildContext inner) => LayoutBuilder(
    builder: (context, constraints) {
      final height =
          (constraints.maxHeight.isFinite
                  ? constraints.maxHeight
                  : MediaQuery.sizeOf(context).height)
              .clamp(0.0, double.infinity);
      return SizedBox(
        height: height,
        child: Column(
          children: [
            ConstrainedBox(
              constraints: BoxConstraints(maxHeight: height * .25),
              child: SingleChildScrollView(
                child: Column(
                  children: [
                    const Padding(
                      padding: EdgeInsets.all(12),
                      child: Text('人与人私信 · 不加入 Agent 上下文。'),
                    ),
                    if (_loading) const LinearProgressIndicator(),
                    if (_messageFeedback != null) Text(_messageFeedback!),
                    if (!_journalReady) const Text('发送恢复记录暂不可用，请先刷新核对。'),
                    for (final p in _messagePending)
                      TextButton.icon(
                        onPressed: _sending ? null : () => _verifyMessage(p),
                        icon: const Icon(Icons.history),
                        label: const Text('核实这次发送'),
                      ),
                    if (_failed)
                      TextButton(
                        onPressed: _load,
                        child: const Text('消息或恢复记录暂不可用，点击重试'),
                      ),
                  ],
                ),
              ),
            ),
            Expanded(
              child: RefreshIndicator(
                onRefresh: _load,
                child: ListView(
                  padding: const EdgeInsets.all(16),
                  children: [
                    for (final p in _pending)
                      TextButton.icon(
                        onPressed: () => _recover(p, inner),
                        icon: const Icon(Icons.history),
                        label: const Text('核实这次卡片分享'),
                      ),
                    if (_messages.isEmpty && !_loading) const Text('还没有消息。'),
                    for (final message in _messages)
                      Align(
                        alignment:
                            message.senderAccountId ==
                                widget.conversation.otherAccountId
                            ? Alignment.centerLeft
                            : Alignment.centerRight,
                        child: Card(
                          child: Padding(
                            padding: const EdgeInsets.all(12),
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                if (message.entity case final entity?)
                                  Semantics(
                                    label: entity.available
                                        ? '${entity.typeLabel}卡片：${entity.title}'
                                        : '${entity.typeLabel}卡片已不可查看',
                                    child: ListTile(
                                      contentPadding: EdgeInsets.zero,
                                      leading: const Icon(Icons.link_outlined),
                                      title: Text(
                                        entity.available
                                            ? entity.title ?? entity.typeLabel
                                            : '${entity.typeLabel}已不可查看',
                                      ),
                                      subtitle: Text(entity.typeLabel),
                                      onTap:
                                          entity.available &&
                                              chatEntityTypes.contains(
                                                entity.type,
                                              ) &&
                                              entity.id != null &&
                                              chatEntityUUID.hasMatch(
                                                entity.id!,
                                              )
                                          ? () => openChatEntity(
                                              inner,
                                              entity,
                                              auth: widget.auth,
                                              workspaceChanges:
                                                  widget.workspaceChanges,
                                              workspaceID: widget.workspaceID,
                                              apiBaseUrl: widget.apiBaseUrl,
                                              client: widget.client,
                                            )
                                          : null,
                                    ),
                                  ),
                                if (message.entity == null ||
                                    message.body != '分享了一张卡片')
                                  Text(message.body),
                                if (message.senderAccountId ==
                                    widget.conversation.otherAccountId)
                                  Align(
                                    alignment: Alignment.centerRight,
                                    child: TextButton(
                                      onPressed: () => Navigator.push(
                                        // The owned report has a separate personal lifetime
                                        // from this credential-bound chat navigator.
                                        Navigator.of(
                                          inner,
                                          rootNavigator: true,
                                        ).context,
                                        MaterialPageRoute<void>(
                                          builder: (_) =>
                                              humanSupportDestination(
                                                authorizationHeader: () =>
                                                    widget
                                                        .auth
                                                        .authorizationHeader,
                                                targetType: 'message',
                                                targetID: message.id,
                                                ownerID: () =>
                                                    widget.auth.accountID,
                                                identityChanges:
                                                    Listenable.merge([
                                                      _changes,
                                                      _reportSource,
                                                      widget
                                                          .reportSourceChanges,
                                                    ]),
                                                sourceCurrent: () =>
                                                    widget.reportSourceCurrent
                                                        ?.call() ??
                                                    (mounted &&
                                                        widget.workspaceID
                                                                ?.call() ==
                                                            null),
                                                sourceKey: () =>
                                                    widget.reportSourceKey
                                                        ?.call() ??
                                                    _reportSource.generation,
                                                client: widget.client,
                                                apiBaseUrl: _source.environment,
                                              ),
                                        ),
                                      ),
                                      child: const Text('举报此消息'),
                                    ),
                                  ),
                              ],
                            ),
                          ),
                        ),
                      ),
                  ],
                ),
              ),
            ),
            ConstrainedBox(
              constraints: BoxConstraints(maxHeight: height * .5),
              child: SingleChildScrollView(
                child: SafeArea(
                  top: false,
                  child: Padding(
                    padding: const EdgeInsets.all(12),
                    child: Row(
                      children: [
                        Expanded(
                          child: TextField(
                            controller: _text,
                            maxLength: 2000,
                            maxLines: 3,
                            minLines: 1,
                            decoration: const InputDecoration(hintText: '输入消息'),
                            onSubmitted: (_) => _send(),
                          ),
                        ),
                        IconButton(
                          tooltip: '发送消息',
                          constraints: const BoxConstraints(
                            minWidth: 48,
                            minHeight: 48,
                          ),
                          onPressed:
                              _sending ||
                                  !_journalReady ||
                                  _messagePending.isNotEmpty
                              ? null
                              : _send,
                          icon: const Icon(Icons.send),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          ],
        ),
      );
    },
  );
}
