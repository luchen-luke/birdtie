import 'dart:convert';

import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:http/http.dart' as http;
import 'remote_inbox_source.dart';
import 'person_avatar.dart';
import 'community_avatar.dart';

class NowSocialReadFailure implements Exception {
  const NowSocialReadFailure(this.statusCode);
  final int statusCode;
  String get message => switch (statusCode) {
    401 => '登录已失效，请重新登录。',
    403 => '当前身份没有访问权限。',
    503 => '服务暂不可用，请稍后重试。',
    _ => '暂时无法读取，请重试。',
  };
}

final _edgeUUID = RegExp(
  r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
);
bool validSocialEdgeID(String id) =>
    _edgeUUID.hasMatch(id) && id != '00000000-0000-0000-0000-000000000000';

String _edgeLabel(String value) {
  final label = value.trim();
  if (label.length > 160 || label.contains(RegExp(r'[\x00-\x1f]'))) {
    throw const FormatException('社交名称不符');
  }
  return label.isEmpty ? 'Birdtie 成员' : label;
}

/// A server-authorized Follow label. Follow is not friendship or live location.
class NowFollowEntry {
  const NowFollowEntry({
    required this.id,
    required this.type,
    required this.targetID,
    required this.label,
    required this.createdAt,
    this.avatar,
    this.communityAvatar,
  });
  final String id, type, targetID, label;
  final DateTime createdAt;
  final PersonAvatarMetadata? avatar;
  final CommunityAvatarMetadata? communityAvatar;
  bool get isPerson => type == 'PERSON';
  String get typeLabel => switch (type) {
    'PERSON' => '已关注的人',
    'ORGANIZATION' => '已关注组织',
    'COMMUNITY' => '已关注社群',
    _ => '已关注商家',
  };
}

/// Read-only adapter over the original transports. This class never owns or
/// closes ConnectionSource / its client, and never writes relationships/readAt.
class NowSocialEdgeSource {
  const NowSocialEdgeSource(
    this.connections, {
    this.onUnauthorized,
    this.identityEpoch,
  });
  final ConnectionSource connections;
  final Future<void> Function(String expectedAuthorization)? onUnauthorized;
  final int Function()? identityEpoch;
  bool get configured => connections.configured && !connections.isClosed;
  String? get authorization => connections.authorizationHeader();

  Future<dynamic> _readData(String path, {Map<String, String>? query}) async {
    final token = authorization, epoch = identityEpoch?.call();
    if (!configured || token == null) throw const NowSocialReadFailure(401);
    final endpoint = Uri.parse(
      '${connections.environment}$path',
    ).replace(queryParameters: query);
    final response = await connections.followClient
        .get(endpoint, headers: {'Authorization': token})
        .timeout(const Duration(seconds: 12));
    if (!configured ||
        authorization != token ||
        identityEpoch?.call() != epoch) {
      throw StateError('社交身份已变化');
    }
    if (response.statusCode == 401) await onUnauthorized?.call(token);
    if (response.statusCode != 200)
      throw NowSocialReadFailure(response.statusCode);
    if (response.bodyBytes.length > 1024 * 1024) {
      throw const FormatException('列表暂不可完整读取');
    }
    final root = jsonDecode(utf8.decode(response.bodyBytes));
    if (root is! Map<String, dynamic> || !root.containsKey('data')) {
      throw const FormatException('社交来源不符');
    }
    return root['data'];
  }

  Future<List<dynamic>> readList(String path) async {
    final data = await _readData(path);
    if (data is! List || data.length > 5000) {
      throw const FormatException('社交来源不符');
    }
    return data;
  }

  Future<HumanConversationListPage> conversationPage({String? cursor}) async {
    final data = await _readData(
      '/v1/me/conversations/page',
      query: socialPageQuery(cursor: cursor),
    );
    if (data is! Map<String, dynamic>) throw const FormatException('会话来源不符');
    final page = HumanConversationListPage.fromJson(
      data,
      limit: 20,
      previousCursor: cursor,
    );
    final seen = <String>{};
    for (final chat in page.items) {
      if (!validSocialEdgeID(chat.id) ||
          !validSocialEdgeID(chat.otherAccountId) ||
          chat.unreadCount < 0 ||
          !seen.add(chat.id)) {
        throw const FormatException('对话来源不符');
      }
      _edgeLabel(chat.otherName);
    }
    return page;
  }

  Future<SocialListPage<FriendTie>> friendsPage({String? cursor}) async {
    final data = await _readData(
      '/v1/me/ties/page',
      query: socialPageQuery(cursor: cursor),
    );
    final page = SocialListPage<FriendTie>.fromJson(
      data,
      decode: socialPageFriend,
      key: (v) => v.otherAccountId,
      previousCursor: cursor,
    );
    for (final row in page.items) {
      _edgeLabel(row.otherName);
    }
    return page;
  }

  Future<SocialListPage<ContactRequest>> requestsPage({String? cursor}) async {
    final data = await _readData(
      '/v1/me/connection-requests/page',
      query: socialPageQuery(cursor: cursor),
    );
    return SocialListPage<ContactRequest>.fromJson(
      data,
      decode: ContactRequest.fromReviewJson,
      key: (v) => v.id,
      previousCursor: cursor,
    );
  }

  Future<SocialListPage<NowFollowEntry>> followsPage({String? cursor}) async {
    final data = await _readData(
      '/v1/me/follows/page',
      query: socialPageQuery(cursor: cursor),
    );
    final ids = <String>{};
    return SocialListPage<NowFollowEntry>.fromJson(
      data,
      decode: (row) {
        if (row.keys.any(
              (k) => !const {
                'id',
                'target',
                'label',
                'createdAt',
                'avatar',
                'communityAvatar',
              }.contains(k),
            ) ||
            row['target'] is! Map<String, dynamic> ||
            (row['target'] as Map<String, dynamic>).keys.any(
              (k) => !const {'targetType', 'targetId'}.contains(k),
            )) {
          throw const FormatException('关注来源不符');
        }
        final result = _followRows([row]).single;
        if (!ids.add(result.id)) throw const FormatException('关注分页重复');
        return result;
      },
      key: (v) => '${v.type}:${v.targetID}',
      previousCursor: cursor,
    );
  }

  Future<List<FriendTie>> friends() async {
    final raw = await readList('/v1/me/ties');
    final rows = [
      for (final row in raw) FriendTie.fromJson(row as Map<String, dynamic>),
    ];
    final seen = <String>{};
    for (final row in rows) {
      if (!validSocialEdgeID(row.id) ||
          !validSocialEdgeID(row.otherAccountId) ||
          !seen.add(row.otherAccountId)) {
        throw const FormatException('好友来源不符');
      }
      _edgeLabel(row.otherName);
    }
    return List.unmodifiable(rows);
  }

  Future<List<HumanConversation>> conversations() async {
    final raw = await readList('/v1/me/conversations');
    final rows = [
      for (final row in raw)
        HumanConversation.fromJson(row as Map<String, dynamic>),
    ];
    final seen = <String>{};
    for (final row in rows) {
      if (!validSocialEdgeID(row.id) ||
          !validSocialEdgeID(row.otherAccountId) ||
          row.unreadCount < 0 ||
          !seen.add(row.id)) {
        throw const FormatException('对话来源不符');
      }
      _edgeLabel(row.otherName);
    }
    return List.unmodifiable(rows);
  }

  Future<List<NowFollowEntry>> follows() async {
    final rows = await readList('/v1/me/follows');
    return _followRows(rows);
  }

  List<NowFollowEntry> _followRows(List<dynamic> rows) {
    if (rows.length > 5000) {
      throw const FormatException('关注列表暂不可完整读取');
    }
    final seen = <String>{}, ids = <String>{};
    final entries = <NowFollowEntry>[];
    for (final row in rows) {
      if (row is! Map<String, dynamic> ||
          row['id'] is! String ||
          !validSocialEdgeID(row['id'] as String) ||
          row['target'] is! Map<String, dynamic> ||
          row['label'] is! String ||
          row['createdAt'] is! String) {
        throw const FormatException('关注条目不符');
      }
      final target = row['target'] as Map<String, dynamic>;
      final type = target['targetType'], id = target['targetId'];
      final stamp = row['createdAt'] as String;
      if (!const {
            'PERSON',
            'ORGANIZATION',
            'COMMUNITY',
            'BUSINESS',
          }.contains(type) ||
          id is! String ||
          !validSocialEdgeID(id) ||
          !seen.add('$type:$id') ||
          !ids.add(row['id'] as String) ||
          !RegExp(r'(Z|[+-]\d{2}:\d{2})$').hasMatch(stamp)) {
        throw const FormatException('关注目标不符');
      }
      if (row['communityAvatar'] != null && type != 'COMMUNITY') {
        throw const FormatException('关注社群头像目标不符');
      }
      entries.add(
        NowFollowEntry(
          id: row['id'] as String,
          type: type as String,
          targetID: id,
          label: _edgeLabel(row['label'] as String),
          createdAt: DateTime.parse(stamp),
          avatar: type == 'PERSON'
              ? PersonAvatarMetadata.optional(row['avatar'], id)
              : null,
          communityAvatar: type == 'COMMUNITY'
              ? CommunityAvatarMetadata.optional(row['communityAvatar'], id)
              : null,
        ),
      );
    }
    return List.unmodifiable(entries);
  }

  RemoteInboxSource get inbox => _SocialInboxSource(this);
}

/// Uses the original Inbox destination/mark-read behavior with the same 401
/// boundary as the other reads. It borrows the connection client, never closes it.
class _SocialInboxSource extends RemoteInboxSource {
  _SocialInboxSource(this.edge)
    : super(
        authorizationHeader: edge.connections.authorizationHeader,
        client: edge.connections.followClient,
        apiBaseUrl: edge.connections.environment,
        onUnauthorized: edge.onUnauthorized,
        identityEpoch: edge.identityEpoch,
        isCurrent: () => edge.configured,
      );
  final NowSocialEdgeSource edge;
  @override
  Future<List<InboxItem>> load() async {
    final rows = await edge.readList('/v1/me/inbox');
    if (rows.length > 100) throw const FormatException('通知列表过长');
    final items = [
      for (final row in rows) InboxItem.fromJson(row as Map<String, dynamic>),
    ];
    if (items.map((v) => v.id).toSet().length != items.length) {
      throw const FormatException('重复通知');
    }
    return List.unmodifiable(items);
  }

  @override
  Future<InboxItem> markRead(String id) async {
    final token = edge.authorization, epoch = edge.identityEpoch?.call();
    if (!edge.configured || token == null)
      throw const NowSocialReadFailure(401);
    final http.Response response = await edge.connections.followClient
        .post(
          Uri.parse(
            '${edge.connections.environment}/v1/me/inbox/${Uri.encodeComponent(id)}/read',
          ),
          headers: {'Authorization': token},
        )
        .timeout(const Duration(seconds: 10));
    if (!edge.configured ||
        edge.authorization != token ||
        edge.identityEpoch?.call() != epoch) {
      throw StateError('通知身份已变化');
    }
    if (response.statusCode == 401) await edge.onUnauthorized?.call(token);
    if (response.statusCode != 200)
      throw NowSocialReadFailure(response.statusCode);
    if (response.bodyBytes.length > 1024 * 1024)
      throw const FormatException('通知回执过长');
    final root = jsonDecode(utf8.decode(response.bodyBytes));
    final item = InboxItem.fromJson(root['data'] as Map<String, dynamic>);
    if (item.id != id || item.readAt == null)
      throw const FormatException('通知回执不符');
    return item;
  }
}
