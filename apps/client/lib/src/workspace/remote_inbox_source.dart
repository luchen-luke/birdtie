import 'dart:convert';

import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';

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
  });
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
    final route = data['notificationRoute'];
    if (route != null && !const ['NORMAL', 'IMMEDIATE', 'DIGEST'].contains(route)) {
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
            data['resourceType'] != 'business_claim_review') ||
        (data['resourceType'] == 'business_claim_review' &&
            (data['targetBusinessId'] == null || semantic != 'BUSINESS')) ||
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
    return InboxItem(
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
    );
  }
}

class RemoteInboxSource {
  RemoteInboxSource({
    required this.authorizationHeader,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _apiBaseUrl = apiBaseUrl ?? apiBase;

  static const apiBase = BirdtieEnvironment.apiBaseUrl;
  final String? Function() authorizationHeader;
  final http.Client _client;
  final String _apiBaseUrl;
  http.Client get followClient => _client;
  String get followApiBaseUrl => _apiBaseUrl;

  Uri _endpoint(String path) =>
      Uri.parse('${_apiBaseUrl.replaceFirst(RegExp(r'/$'), '')}$path');

  Map<String, String> _headers() {
    final bearer = authorizationHeader();
    if (bearer == null) throw StateError('Sign in to use Inbox');
    return {'Authorization': bearer};
  }

  Future<List<InboxItem>> load() async {
    final headers = _headers();
    final response = await _client
        .get(_endpoint('/v1/me/inbox'), headers: headers)
        .timeout(const Duration(seconds: 10));
    if (headers['Authorization'] != authorizationHeader()) {
      throw StateError('Identity changed');
    }
    if (response.statusCode != 200) throw StateError('Inbox unavailable');
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
    final response = await _client
        .post(
          _endpoint('/v1/me/inbox/${Uri.encodeComponent(id)}/read'),
          headers: headers,
        )
        .timeout(const Duration(seconds: 10));
    if (headers['Authorization'] != authorizationHeader()) {
      throw StateError('Identity changed');
    }
    if (response.statusCode != 200) throw StateError('Cannot mark item read');
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

  void dispose() => _client.close();
}
