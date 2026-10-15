import 'dart:convert';

import 'package:http/http.dart' as http;
import 'package:flutter/foundation.dart';
import '../config/birdtie_environment.dart';
import 'person_avatar.dart' show PersonAvatarMetadata;

class InboxItem {
  const InboxItem({
    required this.id,
    required this.category,
    required this.title,
    required this.detail,
    required this.resourceType,
    required this.resourceID,
    required this.createdAt,
    this.readAt,
    this.targetActivityID,
    this.targetConversationID,
    this.targetCommunityID,
    this.targetTaskID,
    this.targetBusinessID,
    this.notificationRoute,
    this.semanticCategory,
    this.notificationPriority,
    this.actorID,
    this.actorAvatar,
    this.origin = 'NATIVE',
    this.historicalType,
  });
  final String origin;
  final String? historicalType;
  final String id;
  final String category;
  final String title;
  final String detail;
  final String resourceType;
  final String resourceID;
  final DateTime createdAt;
  final DateTime? readAt;
  final String? targetActivityID;
  final String? targetConversationID;
  final String? targetCommunityID;
  final String? targetTaskID;
  final String? targetBusinessID;
  final String? notificationRoute;
  final String? semanticCategory;
  final int? notificationPriority;
  final String? actorID;
  final PersonAvatarMetadata? actorAvatar;

  factory InboxItem.fromJson(Map<String, dynamic> data) {
    String id(dynamic v) {
      if (v is! String ||
          !RegExp(
            r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
          ).hasMatch(v) ||
          v == '00000000-0000-0000-0000-000000000000') {
        throw const FormatException('Invalid notification target');
      }
      return v;
    }

    DateTime stamp(dynamic v) {
      if (v is! String || !RegExp(r'(Z|[+-]\d{2}:\d{2})$').hasMatch(v)) {
        throw const FormatException('Missing timezone');
      }
      final t = DateTime.parse(v);
      if (t.toUtc().year < 1 || t.toUtc().year > 9999) {
        throw const FormatException('Invalid time');
      }
      return t;
    }

    const kinds = [
      'activity_reminder',
      'activity_change',
      'activity_cancelled',
      'activity_candidate',
      'place_candidate',
      'conversation_message',
      'connection_request',
      'community_message',
      'activity_message',
      'organization_membership',
      'agent_task',
      'community',
      'opportunity_available',
      'business_claim_review',
      'business_update',
      'moment_like',
      'moment_comment',
      'legacy_history',
    ];
    if (!const [
          'needs_attention',
          'messages',
          'requests',
          'agent_updates',
          'updates',
        ].contains(data['category']) ||
        !kinds.contains(data['resourceType']) ||
        data['title'] is! String ||
        (data['title'] as String).trim().isEmpty ||
        data['detail'] is! String) {
      throw const FormatException('Unknown notification');
    }
    final origin = data['origin'] ?? 'NATIVE';
    if (!const ['NATIVE', 'LEGACY_HISTORY'].contains(origin) ||
        (origin == 'LEGACY_HISTORY') !=
            (data['resourceType'] == 'legacy_history') ||
        origin == 'LEGACY_HISTORY' &&
            (data['historicalType'] is! String ||
                [
                  data['targetActivityId'],
                  data['targetConversationId'],
                  data['targetCommunityId'],
                  data['targetTaskId'],
                  data['targetBusinessId'],
                  data['actorAvatar'],
                  data['actorId'],
                ].any((v) => v != null)))
      throw const FormatException('原通知来源不符');
    final route = data['notificationRoute'];
    if (route != null &&
        !const ['NORMAL', 'IMMEDIATE', 'DIGEST'].contains(route)) {
      throw const FormatException('Undelivered notification');
    }
    final semantic = data['semanticCategory'];
    if (semantic != null &&
        !const [
          'MESSAGE',
          'ACTIVITY',
          'COMMUNITY',
          'ORGANIZATION',
          'BUSINESS',
          'SYSTEM',
          'AGENT',
          'SOCIAL',
        ].contains(semantic)) {
      throw const FormatException('Unknown category');
    }
    final priority = data['notificationPriority'];
    // DIGEST is visible only after the original server delivery checks. This
    // shape check conveys no authority and does not release pending decisions.
    if (route == 'DIGEST' &&
        (semantic == null || priority is! int || priority != 10)) {
      throw const FormatException('Invalid delivered digest metadata');
    }
    if ((data['targetBusinessId'] != null &&
            !const [
              'business_claim_review',
              'business_update',
            ].contains(data['resourceType'])) ||
        (const [
              'business_claim_review',
              'business_update',
            ].contains(data['resourceType']) &&
            (data['targetBusinessId'] == null || semantic != 'BUSINESS')) ||
        (data['resourceType'] == 'business_update' &&
            (data['category'] != 'updates' ||
                !const ['NORMAL', 'IMMEDIATE', 'DIGEST'].contains(route))) ||
        (data['targetBusinessId'] != null &&
            [
              data['targetActivityId'],
              data['targetConversationId'],
              data['targetCommunityId'],
              data['targetTaskId'],
            ].any((value) => value != null))) {
      throw const FormatException(
        'Conflicting business notification destination',
      );
    }
    if ((data['targetCommunityId'] != null &&
            !const [
              'community',
              'community_message',
            ].contains(data['resourceType'])) ||
        (data['targetTaskId'] != null &&
            data['resourceType'] != 'agent_task') ||
        (data['targetCommunityId'] != null && data['targetTaskId'] != null) ||
        ((data['targetCommunityId'] != null || data['targetTaskId'] != null) &&
            (data['targetActivityId'] != null ||
                data['targetConversationId'] != null))) {
      throw const FormatException('Conflicting notification destinations');
    }
    final rawAvatar = data['actorAvatar'], rawActor = data['actorId'];
    if ((rawAvatar == null) != (rawActor == null) ||
        (rawAvatar != null &&
            !const {
              'connection_request',
              'conversation_message',
            }.contains(data['resourceType']))) {
      throw const FormatException('通知头像与来源不符');
    }
    final actor = rawActor == null ? null : id(rawActor);
    final avatar = rawAvatar == null
        ? null
        : PersonAvatarMetadata.fromJson(rawAvatar, actor!);
    return InboxItem(
      origin: origin as String,
      historicalType: data['historicalType'] as String?,
      id: id(data['id']),
      category: data['category'] as String,
      title: data['title'] as String,
      detail: data['detail'] as String,
      resourceType: data['resourceType'] as String,
      resourceID: id(data['resourceId']),
      createdAt: stamp(data['createdAt']),
      readAt: data['readAt'] == null ? null : stamp(data['readAt']),
      targetActivityID: data['targetActivityId'] == null
          ? null
          : id(data['targetActivityId']),
      targetConversationID: data['targetConversationId'] == null
          ? null
          : id(data['targetConversationId']),
      targetCommunityID: data['targetCommunityId'] == null
          ? null
          : id(data['targetCommunityId']),
      targetTaskID: data['targetTaskId'] == null
          ? null
          : id(data['targetTaskId']),
      targetBusinessID: data['targetBusinessId'] == null
          ? null
          : id(data['targetBusinessId']),
      notificationRoute: route as String?,
      semanticCategory: semantic as String?,
      notificationPriority: priority is int ? priority : null,
      actorID: actor,
      actorAvatar: avatar,
    );
  }
}

class InboxReadFailure implements Exception {
  const InboxReadFailure(this.statusCode);
  final int statusCode;
}

class InboxReviewResult {
  const InboxReviewResult({
    required this.itemID,
    required this.kind,
    required this.status,
    this.targetID,
  });
  final String itemID, kind, status;
  final String? targetID;
  factory InboxReviewResult.decode(Object? raw, InboxItem item) {
    bool uuid(Object? v) =>
        v is String &&
        RegExp(
          r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
        ).hasMatch(v) &&
        v != '00000000-0000-0000-0000-000000000000';
    if (raw is! Map<String, dynamic> ||
        raw.keys.any(
          (k) => !const {
            'schemaVersion',
            'itemId',
            'kind',
            'status',
            'targetId',
            'targetAvailable',
          }.contains(k),
        ) ||
        raw['schemaVersion'] != 'inbox-review-result-v1' ||
        raw['itemId'] != item.id ||
        !uuid(raw['itemId']) ||
        raw['kind'] != item.resourceType ||
        (raw['kind'] == 'activity_candidate'
            ? !const {'published', 'rejected'}.contains(raw['status'])
            : raw['kind'] != 'place_candidate' ||
                  !const {
                    'published',
                    'linked_duplicate',
                    'rejected',
                  }.contains(raw['status'])) ||
        raw['targetAvailable'] is! bool ||
        raw['targetAvailable'] != (raw['targetId'] != null) ||
        raw['targetId'] != null &&
            (!uuid(raw['targetId']) || raw['status'] == 'rejected')) {
      throw const FormatException('当前审核结果与本人通知不符');
    }
    return InboxReviewResult(
      itemID: raw['itemId'],
      kind: raw['kind'],
      status: raw['status'],
      targetID: raw['targetId'],
    );
  }
}

/// A bounded notification page. Its cursor locates data; it grants no access.
class InboxNotificationPage {
  InboxNotificationPage({
    required List<InboxItem> items,
    required this.nextCursor,
    required this.unreadCount,
    this.historicalUnreadCount = 0,
  }) : items = List.unmodifiable(items);
  final List<InboxItem> items;
  final String? nextCursor;
  final int unreadCount, historicalUnreadCount;

  factory InboxNotificationPage.fromJson(
    Map<String, dynamic> data, {
    int limit = 50,
    String? previousCursor,
  }) {
    const fields = {'items', 'nextCursor', 'unreadCount'};
    final historicalUnread = data['historicalUnreadCount'] ?? 0;
    final rows = data['items'],
        unread = data['unreadCount'],
        cursor = data['nextCursor'];
    if (data.keys.any(
          (k) => !{...fields, 'historicalUnreadCount'}.contains(k),
        ) ||
        historicalUnread is! int ||
        historicalUnread < 0 ||
        !fields.every(data.containsKey) ||
        limit < 1 ||
        limit > 50 ||
        rows is! List ||
        rows.length > limit ||
        unread is! int ||
        unread < 0) {
      throw const FormatException('Invalid notification page');
    }
    final items = <InboxItem>[];
    for (final row in rows) {
      if (row is! Map<String, dynamic>) {
        throw const FormatException('Invalid notification row');
      }
      final item = InboxItem.fromJson(row);
      if (item.resourceType == 'conversation_message') {
        throw const FormatException('Messages do not belong in notifications');
      }
      items.add(item);
    }
    if (items.map((i) => i.id).toSet().length != items.length ||
        cursor != null &&
            (cursor is! String ||
                !_validInboxCursor(cursor) ||
                cursor == previousCursor ||
                items.isEmpty)) {
      throw const FormatException(
        'Invalid notification cursor or duplicate row',
      );
    }
    return InboxNotificationPage(
      items: items,
      nextCursor: cursor as String?,
      unreadCount: unread,
      historicalUnreadCount: historicalUnread,
    );
  }
}

bool _validInboxCursor(String value) {
  if (value.isEmpty ||
      value.length > 1024 ||
      !RegExp(r'^[A-Za-z0-9_-]+$').hasMatch(value)) {
    return false;
  }
  try {
    return base64Url
            .encode(base64Url.decode(base64Url.normalize(value)))
            .replaceAll('=', '') ==
        value;
  } on FormatException {
    return false;
  }
}

class RemoteInboxSource {
  RemoteInboxSource({
    required this.authorizationHeader,
    http.Client? client,
    String? apiBaseUrl,
    this.onUnauthorized,
    this.identityEpoch,
    this.isCurrent,
    this.identityChanges,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _apiBaseUrl = apiBaseUrl ?? apiBase {
    identityChanges?.addListener(_identityChanged);
  }

  static const apiBase = BirdtieEnvironment.apiBaseUrl;
  final String? Function() authorizationHeader;
  final http.Client _client;
  final bool _ownsClient;
  final String _apiBaseUrl;
  final Future<void> Function(String expectedAuthorization)? onUnauthorized;
  final int Function()? identityEpoch;
  final bool Function()? isCurrent;
  final Listenable? identityChanges;
  int _epoch = 0;
  bool _closed = false;
  void _identityChanged() => ++_epoch;
  Object get identityVersion => (_epoch, identityEpoch?.call());
  bool get active => !_closed && isCurrent?.call() != false;
  http.Client get followClient => _client;
  String get followApiBaseUrl => _apiBaseUrl;

  Uri _endpoint(String path) =>
      Uri.parse('${_apiBaseUrl.replaceFirst(RegExp(r'/$'), '')}$path');

  Map<String, String> _headers() {
    final bearer = authorizationHeader();
    if (bearer == null) throw StateError('Sign in to use Inbox');
    return {'Authorization': bearer};
  }

  void _checkIdentity(String bearer, Object version) {
    if (!active ||
        bearer != authorizationHeader() ||
        version != identityVersion) {
      throw StateError('Inbox identity changed');
    }
  }

  Future<InboxNotificationPage> loadNotificationsPage({
    int limit = 20,
    String? cursor,
    String category = '',
    bool unreadOnly = false,
    String origin = 'NATIVE',
  }) async {
    if (!const ['NATIVE', 'LEGACY_HISTORY'].contains(origin) ||
        limit < 1 ||
        limit > 50) {
      throw const FormatException('Invalid notification limit');
    }
    final position = cursor == null || cursor.isEmpty ? null : cursor;
    if (position != null && !_validInboxCursor(position)) {
      throw const FormatException('Invalid notification cursor');
    }
    final bearer = _headers()['Authorization']!, version = identityVersion;
    _checkIdentity(bearer, version);
    final response = await _client
        .get(
          _endpoint('/v1/me/inbox/page').replace(
            queryParameters: {
              'view': 'notifications',
              if (origin == 'LEGACY_HISTORY') 'origin': origin,
              'limit': '$limit',
              'cursor': ?position,
              if (category.isNotEmpty) 'category': category,
              if (unreadOnly) 'unreadOnly': 'true',
            },
          ),
          headers: {'Authorization': bearer},
        )
        .timeout(const Duration(seconds: 10));
    _checkIdentity(bearer, version);
    if (response.statusCode == 401) await onUnauthorized?.call(bearer);
    if (response.statusCode != 200) throw InboxReadFailure(response.statusCode);
    if (response.bodyBytes.length > 1024 * 1024) {
      throw const FormatException('Unbounded notification response');
    }
    final root = jsonDecode(utf8.decode(response.bodyBytes));
    if (root is! Map<String, dynamic> ||
        root['data'] is! Map<String, dynamic>) {
      throw const FormatException('Missing notification page');
    }
    final page = InboxNotificationPage.fromJson(
      root['data'] as Map<String, dynamic>,
      limit: limit,
      previousCursor: position,
    );
    if (page.items.any(
      (i) =>
          i.origin != origin ||
          category.isNotEmpty && i.category != category ||
          unreadOnly && i.readAt != null,
    ))
      throw const FormatException('通知筛选结果不符');
    _checkIdentity(bearer, version);
    return page;
  }

  Future<({String momentID, String? commentID})> momentTarget(
    String itemID,
  ) async {
    if (!RegExp(r'^[0-9a-f-]{36}$').hasMatch(itemID))
      throw const FormatException('通知标识不符');
    final bearer = _headers()['Authorization']!, version = identityVersion;
    final response = await _client
        .get(
          _endpoint('/v1/me/inbox/$itemID/moment-target'),
          headers: {'Authorization': bearer},
        )
        .timeout(const Duration(seconds: 10));
    _checkIdentity(bearer, version);
    if (response.statusCode != 200) throw InboxReadFailure(response.statusCode);
    final root = jsonDecode(utf8.decode(response.bodyBytes));
    final data = root is Map<String, dynamic> ? root['data'] : null;
    if (data is! Map<String, dynamic> ||
        data.length != 2 ||
        !data.containsKey('momentId') ||
        !data.containsKey('commentId'))
      throw const FormatException('动态通知目标不符');
    bool valid(Object? id) =>
        id is String &&
        RegExp(
          r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
        ).hasMatch(id);
    if (!valid(data['momentId']) ||
        data['commentId'] != null && !valid(data['commentId']))
      throw const FormatException('动态通知目标不符');
    return (
      momentID: data['momentId'] as String,
      commentID: data['commentId'] as String?,
    );
  }

  Future<List<InboxItem>> load() async {
    final headers = _headers();
    final version = identityVersion;
    _checkIdentity(headers['Authorization']!, version);
    final response = await _client
        .get(_endpoint('/v1/me/inbox'), headers: headers)
        .timeout(const Duration(seconds: 10));
    _checkIdentity(headers['Authorization']!, version);
    if (response.statusCode == 401) {
      await onUnauthorized?.call(headers['Authorization']!);
    }
    if (response.statusCode != 200) throw InboxReadFailure(response.statusCode);
    final data =
        (jsonDecode(utf8.decode(response.bodyBytes))
                as Map<String, dynamic>)['data']
            as List<dynamic>;
    if (data.length > 100) {
      throw const FormatException('Unbounded inbox');
    }
    final items = [
      for (final item in data) InboxItem.fromJson(item as Map<String, dynamic>),
    ];
    if (items.map((i) => i.id).toSet().length != items.length) {
      throw const FormatException('Duplicate notification');
    }
    return List.unmodifiable(items);
  }

  Future<InboxItem> markRead(String id) async {
    final headers = _headers();
    final version = identityVersion;
    _checkIdentity(headers['Authorization']!, version);
    final response = await _client
        .post(
          _endpoint('/v1/me/inbox/${Uri.encodeComponent(id)}/read'),
          headers: headers,
        )
        .timeout(const Duration(seconds: 10));
    _checkIdentity(headers['Authorization']!, version);
    if (response.statusCode == 401) {
      await onUnauthorized?.call(headers['Authorization']!);
    }
    if (response.statusCode != 200) throw InboxReadFailure(response.statusCode);
    final item = InboxItem.fromJson(
      (jsonDecode(utf8.decode(response.bodyBytes))
              as Map<String, dynamic>)['data']
          as Map<String, dynamic>,
    );
    if (item.id != id || item.readAt == null) {
      throw const FormatException('Read result does not match current item');
    }
    return item;
  }

  Future<InboxReviewResult> reviewResult(InboxItem item) async {
    final bearer = _headers()['Authorization']!, version = identityVersion;
    _checkIdentity(bearer, version);
    final response = await _client
        .get(
          _endpoint(
            '/v1/me/inbox/${Uri.encodeComponent(item.id)}/review-result',
          ),
          headers: {'Authorization': bearer},
        )
        .timeout(const Duration(seconds: 10));
    _checkIdentity(bearer, version);
    if (response.statusCode == 401) await onUnauthorized?.call(bearer);
    _checkIdentity(bearer, version);
    if (response.statusCode != 200) throw InboxReadFailure(response.statusCode);
    if (response.bodyBytes.length > 8192) throw const FormatException('审核结果过大');
    final root = jsonDecode(utf8.decode(response.bodyBytes));
    if (root is! Map<String, dynamic>) throw const FormatException('缺少审核结果');
    final value = InboxReviewResult.decode(root['data'], item);
    _checkIdentity(bearer, version);
    return value;
  }

  void dispose() {
    if (_closed) return;
    _closed = true;
    ++_epoch;
    identityChanges?.removeListener(_identityChanged);
    if (_ownsClient) _client.close();
  }
}
