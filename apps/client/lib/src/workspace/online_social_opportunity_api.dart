import 'dart:convert';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'now_context_query_api.dart' show onlineStamp, validOnlineID;

Map<String, dynamic> _map(dynamic value, Set<String> keys) {
  if (value is! Map<String, dynamic> ||
      value.keys.toSet().difference(keys).isNotEmpty ||
      keys.difference(value.keys.toSet()).isNotEmpty) {
    throw const FormatException('线上发现结构无效');
  }
  return value;
}

String _id(dynamic value) {
  if (value is! String || !validOnlineID(value)) {
    throw const FormatException('线上来源标识无效');
  }
  return value;
}

String _text(dynamic value, int limit) {
  if (value is! String ||
      value.trim() != value ||
      value.isEmpty ||
      value.runes.length > limit ||
      value.contains('\u0000')) {
    throw const FormatException('线上来源文字无效');
  }
  return value;
}

class OnlineOpportunityIntent {
  OnlineOpportunityIntent.decode(dynamic raw)
    : this._(_map(raw, {'id', 'title', 'updatedAt', 'expiresAt'}));
  OnlineOpportunityIntent._(Map<String, dynamic> j)
    : id = _id(j['id']),
      title = _text(j['title'], 300),
      updatedAt = onlineStamp(j['updatedAt']),
      expiresAt = onlineStamp(j['expiresAt']);
  final String id, title;
  final DateTime updatedAt, expiresAt;
}

class OnlineSocialOpportunity {
  OnlineSocialOpportunity.decode(dynamic raw, String own)
    : this._(
        _map(raw, {
          'id',
          'title',
          'sourceRef',
          'relation',
          'sourceVersion',
          'expiresAt',
          'tieId',
          'communityId',
        }),
        own,
      );
  OnlineSocialOpportunity._(Map<String, dynamic> j, String own)
    : id = _text(j['id'], 120),
      title = _text(j['title'], 300),
      relation = _text(j['relation'], 20),
      sourceVersion = _text(j['sourceVersion'], 180),
      expiresAt = onlineStamp(j['expiresAt']),
      tieID = j['tieId'] as String,
      communityID = j['communityId'] as String {
    final ref = _map(j['sourceRef'], {'type', 'id'});
    type = _text(ref['type'], 20);
    sourceID = _id(ref['id']);
    if (!{'SOCIAL_INTENT', 'ACTIVITY'}.contains(type) ||
        id != '$own:$type:$sourceID' ||
        !{'FRIEND', 'COMMUNITY', 'PUBLIC'}.contains(relation) ||
        (relation == 'FRIEND' &&
            (!validOnlineID(tieID) || communityID.isNotEmpty)) ||
        (relation == 'COMMUNITY' &&
            (!validOnlineID(communityID) || tieID.isNotEmpty)) ||
        (relation == 'PUBLIC' &&
            (tieID.isNotEmpty || communityID.isNotEmpty))) {
      throw const FormatException('线上来源动作无效');
    }
    onlineStamp(sourceVersion);
  }
  final String id, title, relation, sourceVersion, tieID, communityID;
  late final String type, sourceID;
  final DateTime expiresAt;
  String get relationLabel => switch (relation) {
    'FRIEND' => '已有好友',
    'COMMUNITY' => '已加入社群',
    _ => '公开来源',
  };
}

class OnlineOpportunityView {
  OnlineOpportunityView.decode(dynamic raw, String owner, String intent)
    : this._(
        _map(raw, {
          'schemaVersion',
          'ownerId',
          'intentId',
          'observedAt',
          'validUntil',
          'truncated',
          'intents',
          'items',
        }),
        owner,
        intent,
      );
  OnlineOpportunityView._(Map<String, dynamic> j, String owner, String intent)
    : ownerID = _id(j['ownerId']),
      intentID = j['intentId'] as String,
      observedAt = onlineStamp(j['observedAt']),
      validUntil = onlineStamp(j['validUntil']),
      truncated = j['truncated'] as bool {
    if (j['schemaVersion'] != 'online-social-opportunities-v1' ||
        ownerID != owner ||
        intentID != intent ||
        (!validOnlineID(intentID) && intentID.isNotEmpty) ||
        !validUntil.isAfter(observedAt) ||
        validUntil.difference(observedAt) > const Duration(minutes: 2) ||
        j['intents'] is! List ||
        j['items'] is! List) {
      throw const FormatException('线上发现主体或期限无效');
    }
    intents = List.unmodifiable(
      (j['intents'] as List).map(OnlineOpportunityIntent.decode),
    );
    items = List.unmodifiable(
      (j['items'] as List).map(
        (v) => OnlineSocialOpportunity.decode(v, intentID),
      ),
    );
    if (intents.length > 20 ||
        items.length > 30 ||
        intents.map((i) => i.id).toSet().length != intents.length ||
        items.map((i) => i.id).toSet().length != items.length ||
        (intentID.isEmpty && items.isNotEmpty) ||
        (intentID.isNotEmpty &&
            (intents.length != 1 || intents.single.id != intentID)) ||
        intents.any(
          (i) =>
              !i.expiresAt.isAfter(observedAt) ||
              i.updatedAt.isAfter(observedAt),
        ) ||
        items.any((i) => !i.expiresAt.isAfter(observedAt))) {
      throw const FormatException('线上发现来源无效');
    }
  }
  final String ownerID, intentID;
  final DateTime observedAt, validUntil;
  final bool truncated;
  late final List<OnlineOpportunityIntent> intents;
  late final List<OnlineSocialOpportunity> items;
  bool live(DateTime now) =>
      validUntil.isAfter(now) &&
      intents.every((i) => i.expiresAt.isAfter(now)) &&
      items.every((i) => i.expiresAt.isAfter(now));
}

class OnlineOpportunityHTTPError implements Exception {
  const OnlineOpportunityHTTPError(this.status);
  final int status;
}

class OnlineSocialOpportunityAPI {
  OnlineSocialOpportunityAPI({http.Client? client, String? apiBaseUrl})
    : _client = client ?? http.Client(),
      _owned = client == null,
      _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final http.Client _client;
  final bool _owned;
  final String _base;
  bool _closed = false;
  Future<OnlineOpportunityView> read(
    String authorization,
    String owner, {
    String intentID = '',
  }) async {
    if (_closed) throw StateError('线上发现已关闭');
    if (!validOnlineID(owner) ||
        (intentID.isNotEmpty && !validOnlineID(intentID))) {
      throw const FormatException('请重新选择线上意图');
    }
    final suffix = intentID.isEmpty ? 'options' : intentID;
    final response = await _client
        .get(
          Uri.parse(
            '${_base.replaceFirst(RegExp(r'/$'), '')}/v1/me/online-social-opportunities/$suffix',
          ),
          headers: {'Authorization': authorization},
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) {
      throw OnlineOpportunityHTTPError(response.statusCode);
    }
    final envelope = _map(jsonDecode(utf8.decode(response.bodyBytes)), {
      'data',
    });
    return OnlineOpportunityView.decode(envelope['data'], owner, intentID);
  }

  void dispose() {
    if (_closed) return;
    _closed = true;
    if (_owned) _client.close();
  }
}
