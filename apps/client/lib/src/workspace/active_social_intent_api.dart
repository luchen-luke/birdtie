import 'dart:convert';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'model_egress_api.dart' show egressIDValid, egressTime;

const intentTypes = {
  'FIND_ACTIVITY': '找活动',
  'FIND_COMPANION': '找同伴',
  'ORGANIZE': '组织活动',
  'ASK_HELP': '寻求帮助',
  'OTHER': '其他',
};
const intentModes = {'IN_PERSON': '线下', 'ONLINE': '线上', 'HYBRID': '线上与线下'};
const intentAudiences = {
  'PRIVATE': '仅自己',
  'FRIENDS': '好友',
  'PUBLIC': '公开',
  'LOCAL': '本地用户',
  'COMMUNITY': '指定社群',
  'INVITE_ONLY': '指定邀请对象',
};
const intentStates = {
  'DRAFT': '草稿',
  'ACTIVE': '正在寻找',
  'MATCHED': '已有匹配',
  'CONVERTED': '已转为活动',
  'EXPIRED': '已到期',
  'CANCELLED': '已取消',
};
Map<String, dynamic> intentMap(dynamic v) {
  if (v is! Map<String, dynamic>) throw const FormatException('意图结构无效');
  return v;
}

void intentKeys(
  Map<String, dynamic> m,
  Set<String> required, [
  Set<String> optional = const {},
]) {
  if (!m.keys.toSet().containsAll(required) ||
      !required.union(optional).containsAll(m.keys)) {
    throw const FormatException('意图字段无效');
  }
}

String intentID(dynamic v) {
  if (!egressIDValid(v)) throw const FormatException('意图标识无效');
  return v as String;
}

String intentText(dynamic v, [int max = 4096]) {
  if (v is! String || v.trim().isEmpty || v.runes.length > max) {
    throw const FormatException('意图文本无效');
  }
  return v;
}

const _identity = {
  'schemaVersion',
  'owner',
  'agentId',
  'observedAt',
  'modelAccess',
  'sendAllowed',
};

class ActiveIntentEnvelope {
  ActiveIntentEnvelope(Map<String, dynamic> m, String owner)
    : ownerID = owner,
      agentID = intentID(m['agentId']),
      observedAt = egressTime(m['observedAt']) {
    final who = intentMap(m['owner']);
    intentKeys(who, {'type', 'id'});
    if (m['schemaVersion'] != 'active-social-intents-v1' ||
        who['type'] != 'PERSON' ||
        intentID(who['id']) != owner ||
        m['modelAccess'] != false ||
        m['sendAllowed'] != false) {
      throw const FormatException('意图主体或权限不符');
    }
  }
  final String ownerID, agentID;
  final DateTime observedAt;
}

class ActiveSocialIntent {
  ActiveSocialIntent(Map<String, dynamic> m, String owner) {
    intentKeys(m, {
      'intent',
      'version',
      'sourceAvailable',
      'locationLabel',
      'audienceLabel',
    });
    intent = intentMap(m['intent']);
    intentKeys(
      intent,
      {
        'id',
        'creatorAccountId',
        'type',
        'title',
        'constraints',
        'audience',
        'modality',
        'status',
        'expiresAt',
        'createdAt',
        'updatedAt',
      },
      {
        'contextId',
        'cityId',
        'communityId',
        'inviteeAccountIds',
        'convertedActivityId',
        'convertedParticipationId',
        'convertedAt',
      },
    );
    id = intentID(intent['id']);
    if (intentID(intent['creatorAccountId']) != owner) {
      throw const FormatException('意图归属不符');
    }
    intentText(intent['title'], 160);
    if (!intentTypes.containsKey(intent['type']) ||
        !intentModes.containsKey(intent['modality']) ||
        !intentAudiences.containsKey(intent['audience']) ||
        !intentStates.containsKey(intent['status'])) {
      throw const FormatException('意图状态无效');
    }
    version = intentText(m['version'], 64);
    if (!RegExp(r'^[a-f0-9]{64}$').hasMatch(version) ||
        m['sourceAvailable'] is! bool) {
      throw const FormatException('意图版本无效');
    }
    sourceAvailable = m['sourceAvailable'] as bool;
    locationLabel = intentText(m['locationLabel']);
    audienceLabel = intentText(m['audienceLabel']);
    expiresAt = egressTime(intent['expiresAt']);
    createdAt = egressTime(intent['createdAt']);
    updatedAt = egressTime(intent['updatedAt']);
    if (updatedAt.isBefore(createdAt)) throw const FormatException('意图时间无效');
    final association = [
      'convertedActivityId',
      'convertedParticipationId',
      'convertedAt',
    ];
    final n = association.where(intent.containsKey).length;
    if (n != 0 && n != 3) throw const FormatException('活动关联缺项');
    if (n == 3) {
      intentID(intent['convertedActivityId']);
      intentID(intent['convertedParticipationId']);
      final t = egressTime(intent['convertedAt']);
      if (intent['status'] != 'CONVERTED' ||
          t.isBefore(createdAt) ||
          t.isAfter(updatedAt)) {
        throw const FormatException('活动关联状态或时间无效');
      }
    }

    constraints = intentMap(intent['constraints']);
    intentKeys(constraints, {}, {
      'category',
      'minParticipants',
      'maxParticipants',
      'placeId',
      'areaLabel',
      'onlinePlatform',
      'startsAt',
      'endsAt',
    });
    for (final k in ['category', 'areaLabel', 'onlinePlatform']) {
      if (constraints.containsKey(k)) {
        intentText(constraints[k], k == 'areaLabel' ? 160 : 80);
      }
    }
    if (constraints.containsKey('placeId')) intentID(constraints['placeId']);
    for (final k in ['minParticipants', 'maxParticipants']) {
      if (constraints.containsKey(k) &&
          (constraints[k] is! int ||
              (constraints[k] as int) < 1 ||
              (constraints[k] as int) > 100)) {
        throw const FormatException('人数范围无效');
      }
    }
    if (constraints['minParticipants'] != null &&
        constraints['maxParticipants'] != null &&
        constraints['minParticipants'] > constraints['maxParticipants']) {
      throw const FormatException('人数范围无效');
    }
    if (constraints.containsKey('startsAt') !=
        constraints.containsKey('endsAt')) {
      throw const FormatException('活动时间缺项');
    }
    startsAt = constraints.containsKey('startsAt')
        ? egressTime(constraints['startsAt'])
        : null;
    endsAt = constraints.containsKey('endsAt')
        ? egressTime(constraints['endsAt'])
        : null;
    if (startsAt != null &&
        (!endsAt!.isAfter(startsAt!) ||
            endsAt!.difference(startsAt!) > const Duration(days: 90))) {
      throw const FormatException('活动时间无效');
    }
    if (intent['contextId'] != null) intentID(intent['contextId']);
    final audience = intent['audience'];
    final city = intent['cityId'];
    final community = intent['communityId'];
    final peers = intent['inviteeAccountIds'];
    if (audience == 'LOCAL') {
      intentText(city, 80);
    } else if (city != null && city != '') {
      throw const FormatException('本地范围不符');
    }
    if (audience == 'COMMUNITY') {
      intentID(community);
    } else if (community != null && community != '') {
      throw const FormatException('社群范围不符');
    }
    if (peers != null) {
      if (peers is! List ||
          peers.length > 20 ||
          peers.map(intentID).toSet().length != peers.length) {
        throw const FormatException('邀请对象无效');
      }
    }
    if (audience == 'INVITE_ONLY' && (peers is! List || peers.isEmpty) ||
        audience != 'INVITE_ONLY' && peers is List && peers.isNotEmpty) {
      throw const FormatException('邀请范围不符');
    }
    final physical =
        constraints['placeId'] != null || constraints['areaLabel'] != null;
    if (intent['modality'] == 'ONLINE' && physical ||
        intent['modality'] != 'ONLINE' && !physical ||
        intent['modality'] == 'IN_PERSON' &&
            constraints['onlinePlatform'] != null ||
        intent['modality'] == 'HYBRID' &&
            constraints['onlinePlatform'] == null) {
      throw const FormatException('活动方式与地点不符');
    }
  }
  late final Map<String, dynamic> intent, constraints;
  late final String id, version, locationLabel, audienceLabel;
  late final bool sourceAvailable;
  late final DateTime expiresAt, createdAt, updatedAt;
  late final DateTime? startsAt, endsAt;
  String get title => intent['title'] as String;
  String get status => intent['status'] as String;
  bool editable(DateTime now) =>
      ['DRAFT', 'ACTIVE', 'MATCHED'].contains(status) && expiresAt.isAfter(now);
  Map<String, dynamic> get draft => {
    for (final k in [
      'type',
      'title',
      'constraints',
      'audience',
      'modality',
      'expiresAt',
      'cityId',
      'communityId',
      'inviteeAccountIds',
    ])
      if (intent[k] != null) k: jsonDecode(jsonEncode(intent[k])),
    if (intent['contextId'] != null) 'contextId': intent['contextId'],
  };
}

class ActiveIntentList extends ActiveIntentEnvelope {
  ActiveIntentList(Map<String, dynamic> m, String owner) : super(m, owner) {
    intentKeys(m, {..._identity, 'items', 'limit', 'truncated'});
    if (m['limit'] != 100 ||
        m['truncated'] is! bool ||
        m['items'] is! List ||
        (m['items'] as List).length > 100) {
      throw const FormatException('意图列表无效');
    }
    items = (m['items'] as List)
        .map((v) => ActiveSocialIntent(intentMap(v), owner))
        .toList(growable: false);
    if (items.map((i) => i.id).toSet().length != items.length) {
      throw const FormatException('重复意图');
    }
    truncated = m['truncated'] as bool;
  }
  late final List<ActiveSocialIntent> items;
  late final bool truncated;
}

class ActiveIntentOptions extends ActiveIntentEnvelope {
  ActiveIntentOptions(Map<String, dynamic> m, String owner) : super(m, owner) {
    intentKeys(m, {
      ..._identity,
      'cities',
      'places',
      'communities',
      'invitees',
      'limit',
      'truncated',
    });
    if (m['limit'] != 100 || m['truncated'] is! bool) {
      throw const FormatException('范围列表无效');
    }
    truncated = m['truncated'] as bool;
    for (final k in ['cities', 'places', 'communities', 'invitees']) {
      final a = m[k];
      if (a is! List || a.length > 100) throw const FormatException('范围数量无效');
      final parsed = <String, String>{};
      for (final x in a) {
        final v = intentMap(x);
        intentKeys(v, {'id', 'label'});
        final id = k == 'cities' ? intentText(v['id'], 80) : intentID(v['id']);
        if (parsed.containsKey(id)) throw const FormatException('重复范围');
        parsed[id] = intentText(v['label']);
      }
      choices[k] = parsed;
    }
  }
  final Map<String, Map<String, String>> choices = {};
  late final bool truncated;
}

class ActiveIntentPreview extends ActiveIntentEnvelope {
  ActiveIntentPreview(
    Map<String, dynamic> m,
    String owner,
    ActiveSocialIntent original,
    String op,
  ) : super(m, owner) {
    intentKeys(m, {
      ..._identity,
      'previewId',
      'operation',
      'before',
      'after',
      'expiresAt',
      'explanation',
    });
    token = intentText(m['previewId'], 20000);
    operation = intentText(m['operation']);
    before = ActiveSocialIntent(intentMap(m['before']), owner);
    after = ActiveSocialIntent(intentMap(m['after']), owner);
    expiresAt = egressTime(m['expiresAt']);
    explanation = intentText(m['explanation']);
    if (token.length < 24 ||
        operation != op ||
        !['EDIT', 'ACTIVATE', 'CANCEL'].contains(op) ||
        before.id != original.id ||
        before.version != original.version ||
        after.id != before.id ||
        after.createdAt != before.createdAt ||
        !expiresAt.isAfter(observedAt) ||
        expiresAt.difference(observedAt) > const Duration(seconds: 90) ||
        after.status !=
            switch (op) {
              'EDIT' => 'DRAFT',
              'ACTIVATE' => 'ACTIVE',
              _ => 'CANCELLED',
            }) {
      throw const FormatException('具体预览不符');
    }
  }
  late final String token, operation, explanation;
  late final ActiveSocialIntent before, after;
  late final DateTime expiresAt;
}

class ActiveIntentReceipt extends ActiveIntentEnvelope {
  ActiveIntentReceipt(Map<String, dynamic> m, ActiveIntentPreview p)
    : super(m, p.ownerID) {
    intentKeys(m, {
      ..._identity,
      'item',
      'operation',
      'committed',
      'explanation',
    });
    item = ActiveSocialIntent(intentMap(m['item']), p.ownerID);
    explanation = intentText(m['explanation']);
    if (m['operation'] != p.operation ||
        m['committed'] != true ||
        agentID != p.agentID ||
        observedAt.isBefore(p.observedAt) ||
        !observedAt.isBefore(p.expiresAt) ||
        item.id != p.before.id ||
        item.createdAt != p.before.createdAt ||
        item.status != p.after.status ||
        !_sameIntentDraft(item, p.after) ||
        item.version == p.before.version) {
      throw const FormatException('权威结果与具体预览不符');
    }
  }
  late final ActiveSocialIntent item;
  late final String explanation;
}

// PostgreSQL JSONB may reorder object keys and normalize RFC3339 offsets.
// Compare the selected semantics, preserving ordered invitation identities.
bool _sameIntentDraft(ActiveSocialIntent a, ActiveSocialIntent b) {
  Object? stable(Object? value) {
    if (value is Map) {
      final keys = value.keys.cast<String>().toList()..sort();
      return {for (final k in keys) k: stable(value[k])};
    }
    if (value is List) return value.map(stable).toList();
    return value;
  }

  final left = a.draft, right = b.draft;
  left['expiresAt'] = a.expiresAt.toUtc().toIso8601String();
  right['expiresAt'] = b.expiresAt.toUtc().toIso8601String();
  for (final entry in [(left, a), (right, b)]) {
    final constraints = intentMap(entry.$1['constraints']);
    if (entry.$2.startsAt != null) {
      constraints['startsAt'] = entry.$2.startsAt!.toUtc().toIso8601String();
      constraints['endsAt'] = entry.$2.endsAt!.toUtc().toIso8601String();
    }
  }
  return jsonEncode(stable(left)) == jsonEncode(stable(right));
}

class ActiveIntentHTTPError implements Exception {
  const ActiveIntentHTTPError(this.status);
  final int status;
}

class ActiveSocialIntentAPI {
  ActiveSocialIntentAPI({http.Client? client, String? apiBaseUrl})
    : _client = client ?? http.Client(),
      _owned = client == null,
      _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final http.Client _client;
  final bool _owned;
  final String _base;
  bool _closed = false;
  Future<Map<String, dynamic>> _request(
    String method,
    String path,
    String auth, [
    Map<String, dynamic>? body,
  ]) async {
    if (_closed) throw StateError('意图页面已关闭');
    final uri = Uri.parse(
      '${_base.replaceFirst(RegExp(r'/$'), '')}/v1/me/active-social-intents$path',
    );
    final headers = {'Authorization': auth, 'Content-Type': 'application/json'};
    final r =
        await (method == 'GET'
                ? _client.get(uri, headers: headers)
                : _client.post(uri, headers: headers, body: jsonEncode(body)))
            .timeout(const Duration(seconds: 12));
    if (r.statusCode != 200) throw ActiveIntentHTTPError(r.statusCode);
    final v = intentMap(jsonDecode(utf8.decode(r.bodyBytes)));
    intentKeys(v, {'data'});
    return intentMap(v['data']);
  }

  Future<ActiveIntentList> list(String auth, String owner) async =>
      ActiveIntentList(await _request('GET', '', auth), owner);
  Future<ActiveIntentOptions> options(String auth, String owner) async =>
      ActiveIntentOptions(await _request('GET', '/options', auth), owner);
  Future<ActiveSocialIntent> read(String auth, String owner, String id) async {
    intentID(id);
    final v = await _request('GET', '/$id', auth);
    intentKeys(v, {..._identity, 'item'});
    ActiveIntentEnvelope(v, owner);
    final item = ActiveSocialIntent(intentMap(v['item']), owner);
    if (item.id != id) throw const FormatException('意图详情不符');
    return item;
  }

  Future<ActiveIntentPreview> preview(
    String auth,
    String owner,
    ActiveSocialIntent item,
    String op,
    Map<String, dynamic>? edit,
  ) async {
    if (!['EDIT', 'ACTIVATE', 'CANCEL'].contains(op) ||
        (op == 'EDIT') != (edit != null)) {
      throw const FormatException('请检查本次操作');
    }
    return ActiveIntentPreview(
      await _request('POST', '/${item.id}/preview', auth, {
        'operation': op,
        'expectedVersion': item.version,
        'edit': ?edit,
      }),
      owner,
      item,
      op,
    );
  }

  Future<ActiveIntentReceipt> approve(
    String auth,
    ActiveIntentPreview p,
  ) async => ActiveIntentReceipt(
    await _request('POST', '/${p.before.id}/approve', auth, {
      'previewId': p.token,
    }),
    p,
  );
  void dispose() {
    if (_closed) return;
    _closed = true;
    if (_owned) _client.close();
  }
}
