import 'dart:convert';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'active_social_intent_api.dart';
import 'activity_plans.dart' show ActivityPlan;
import 'model_egress_api.dart' show egressTime;

const conversionSchema = 'intent-activity-conversion-v1';
const conversionEnvelopeKeys = {
  'schemaVersion',
  'ownerId',
  'agentId',
  'observedAt',
  'modelAccess',
  'sendAllowed',
};

class ConversionIntent {
  ConversionIntent(dynamic raw, String owner)
    : value = Map.unmodifiable(intentMap(raw)) {
    intentKeys(
      value,
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
    intentID(value['id']);
    if (intentID(value['creatorAccountId']) != owner ||
        !intentTypes.containsKey(value['type']) ||
        !intentModes.containsKey(value['modality']) ||
        !intentAudiences.containsKey(value['audience']) ||
        !intentStates.containsKey(value['status'])) {
      throw const FormatException('意图主体或状态无效');
    }
    intentText(value['title'], 160);
    final created = egressTime(value['createdAt']),
        updated = egressTime(value['updatedAt']);
    egressTime(value['expiresAt']);
    if (updated.isBefore(created)) throw const FormatException('意图时间无效');
    final constraints = intentMap(value['constraints']);
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
              constraints[k] < 1 ||
              constraints[k] > 100)) {
        throw const FormatException('人数无效');
      }
    }
    if (constraints['minParticipants'] != null &&
        constraints['maxParticipants'] != null &&
        constraints['minParticipants'] > constraints['maxParticipants']) {
      throw const FormatException('人数范围无效');
    }
    if (constraints.containsKey('startsAt') !=
        constraints.containsKey('endsAt')) {
      throw const FormatException('时间缺项');
    }
    if (constraints.containsKey('startsAt')) {
      final start = egressTime(constraints['startsAt']),
          end = egressTime(constraints['endsAt']);
      if (!end.isAfter(start) ||
          end.difference(start) > const Duration(days: 90)) {
        throw const FormatException('时间范围无效');
      }
    }
    if (value['contextId'] != null) intentID(value['contextId']);
    final audience = value['audience'],
        city = value['cityId'],
        community = value['communityId'],
        peers = value['inviteeAccountIds'];
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
    if (peers != null &&
        (peers is! List ||
            peers.length > 20 ||
            peers.map(intentID).toSet().length != peers.length)) {
      throw const FormatException('邀请对象无效');
    }
    if (audience == 'INVITE_ONLY' && (peers is! List || peers.isEmpty) ||
        audience != 'INVITE_ONLY' && peers is List && peers.isNotEmpty) {
      throw const FormatException('邀请范围不符');
    }
    final physical =
        constraints['placeId'] != null || constraints['areaLabel'] != null;
    if (value['modality'] == 'ONLINE' && physical ||
        value['modality'] != 'ONLINE' && !physical ||
        value['modality'] == 'IN_PERSON' &&
            constraints['onlinePlatform'] != null ||
        value['modality'] == 'HYBRID' &&
            constraints['onlinePlatform'] == null) {
      throw const FormatException('活动方式与地点不符');
    }
    final association = [
      'convertedActivityId',
      'convertedParticipationId',
      'convertedAt',
    ];
    final count = association.where(value.containsKey).length;
    if (count != 0 && count != 3) throw const FormatException('关联缺项');
    if (count == 3) {
      if (value['status'] != 'CONVERTED') throw const FormatException('关联状态无效');
      intentID(value['convertedActivityId']);
      intentID(value['convertedParticipationId']);
      if (egressTime(value['convertedAt']).isBefore(created) ||
          egressTime(value['convertedAt']).isAfter(updated)) {
        throw const FormatException('关联时间无效');
      }
    }
  }
  final Map<String, dynamic> value;
  String get id => value['id'];
  String get title => value['title'];
  String get status => value['status'];
  bool get linked => value.containsKey('convertedActivityId');
}

class ConversionChoice {
  ConversionChoice(dynamic raw) {
    final m = intentMap(raw);
    intentKeys(m, {'activity', 'participationId'});
    participationID = intentID(m['participationId']);
    final a = intentMap(m['activity']);
    intentKeys(
      a,
      {
        'id',
        'activityId',
        'title',
        'cityId',
        'startsAt',
        'endsAt',
        'available',
        'status',
        'createdAt',
      },
      {
        'modality',
        'physicalPlaceStatus',
        'placeId',
        'placeName',
        'venuePlaceId',
        'timeZone',
      },
    );
    final id = intentID(a['id']);
    if (intentID(a['activityId']) != id ||
        a['available'] != true ||
        !['upcoming', 'ongoing'].contains(a['status'])) {
      throw const FormatException('当前报名来源无效');
    }
    intentText(a['title']);
    intentText(a['cityId'], 80);
    final created = egressTime(a['createdAt']);
    if (created.year < 2) throw const FormatException('活动创建时间无效');
    final start = egressTime(a['startsAt']), end = egressTime(a['endsAt']);
    if (!end.isAfter(start)) throw const FormatException('活动时间无效');
    if (!['online', 'in_person', 'hybrid'].contains(a['modality']) ||
        ![
          'not_applicable',
          'confirmed',
          'tbd',
        ].contains(a['physicalPlaceStatus'])) {
      throw const FormatException('活动方式无效');
    }
    for (final k in ['placeId', 'venuePlaceId']) {
      if (a[k] != null && a[k] != '') intentID(a[k]);
    }
    if (a['placeName'] != null && a['placeName'] != '') {
      intentText(a['placeName']);
      if (a['placeId'] == null || a['placeId'] == '') {
        throw const FormatException('地点缺原标识');
      }
    }
    if (a['timeZone'] != null && a['timeZone'] != '') {
      intentText(a['timeZone'], 80);
    }
    if (a['modality'] == 'online' &&
        (a['physicalPlaceStatus'] != 'not_applicable' ||
            a['placeId'] != null && a['placeId'] != '' ||
            a['placeName'] != null && a['placeName'] != '')) {
      throw const FormatException('线上来源不应推断地址');
    }
    activity = ActivityPlan.fromJson(a);
  }
  late final String participationID;
  late final ActivityPlan activity;
}

class ConversionView {
  ConversionView(Map<String, dynamic> m, String owner, String id) {
    if (m['schemaVersion'] != conversionSchema ||
        intentID(m['ownerId']) != owner ||
        m['modelAccess'] != false ||
        m['sendAllowed'] != false) {
      throw const FormatException('当前本人或权限不符');
    }
    agentID = intentID(m['agentId']);
    observedAt = egressTime(m['observedAt']);
    intent = ConversionIntent(m['intent'], owner);
    if (intent.id != id) throw const FormatException('原意图不符');
    explanation = intentText(m['explanation']);
  }
  late final String agentID, explanation;
  late final DateTime observedAt;
  late final ConversionIntent intent;
}

String conversionVersion(dynamic v) {
  if (v is! String || !RegExp(r'^[a-f0-9]{64}$').hasMatch(v)) {
    throw const FormatException('版本无效');
  }
  return v;
}

class ConversionOptions extends ConversionView {
  ConversionOptions(Map<String, dynamic> m, String owner, String id)
    : super(m, owner, id) {
    intentKeys(
      m,
      conversionEnvelopeKeys.union({
        'intent',
        'version',
        'choices',
        'limit',
        'truncated',
        'explanation',
      }),
    );
    version = conversionVersion(m['version']);
    final raw = m['choices'];
    if (raw is! List ||
        raw.length > 100 ||
        m['limit'] != 100 ||
        m['truncated'] is! bool) {
      throw const FormatException('有限列表无效');
    }
    choices = raw.map(ConversionChoice.new).toList(growable: false);
    if (choices.map((c) => c.activity.activityId).toSet().length !=
        choices.length) {
      throw const FormatException('活动重复');
    }
    truncated = m['truncated'];
  }
  late final String version;
  late final List<ConversionChoice> choices;
  late final bool truncated;
}

class ConversionPreview extends ConversionView {
  ConversionPreview(Map<String, dynamic> m, String owner, String id)
    : super(m, owner, id) {
    intentKeys(
      m,
      conversionEnvelopeKeys.union({
        'intent',
        'version',
        'choice',
        'previewId',
        'expiresAt',
        'explanation',
      }),
    );
    version = conversionVersion(m['version']);
    choice = ConversionChoice(m['choice']);
    previewID = intentText(m['previewId'], 4096);
    expiresAt = egressTime(m['expiresAt']);
    if (!expiresAt.isAfter(observedAt) ||
        expiresAt.difference(observedAt) > const Duration(seconds: 90) ||
        !['ACTIVE', 'MATCHED'].contains(intent.status) ||
        intent.value['type'] != 'FIND_ACTIVITY') {
      throw const FormatException('具体批准时间或状态无效');
    }
  }
  late final String version, previewID;
  late final DateTime expiresAt;
  late final ConversionChoice choice;
}

class ConversionReceipt extends ConversionView {
  ConversionReceipt(Map<String, dynamic> m, String owner, String id)
    : super(m, owner, id) {
    intentKeys(
      m,
      conversionEnvelopeKeys.union({
        'intent',
        'activityId',
        'participationId',
        'committed',
        'explanation',
      }),
    );
    activityID = intentID(m['activityId']);
    participationID = intentID(m['participationId']);
    if (m['committed'] != true ||
        !intent.linked ||
        intent.value['convertedActivityId'] != activityID ||
        intent.value['convertedParticipationId'] != participationID ||
        egressTime(intent.value['convertedAt']).isAfter(observedAt)) {
      throw const FormatException('原关联回执无效');
    }
  }
  late final String activityID, participationID;
}

class IntentConversionFailure implements Exception {
  const IntentConversionFailure(this.status);
  final int status;
}

class IntentActivityConversionAPI {
  IntentActivityConversionAPI({
    http.Client? client,
    String? apiBaseUrl,
    this.timeout = const Duration(seconds: 12),
  }) : _client = client ?? http.Client(),
       _owned = client == null,
       base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  http.Client get detailClient => _client;
  final http.Client _client;
  final bool _owned;
  final String base;
  final Duration timeout;
  bool _closed = false;
  Future<Map<String, dynamic>> _request(
    String auth,
    String id,
    String suffix, [
    Map<String, dynamic>? body,
  ]) async {
    if (_closed) throw StateError('已关闭');
    intentID(id);
    final uri = Uri.parse(
      '${base.replaceFirst(RegExp(r'/$'), '')}/v1/me/social-intents/$id/activity-conversion$suffix',
    );
    final headers = {'Authorization': auth, 'Content-Type': 'application/json'};
    final r =
        await (body == null
                ? _client.get(uri, headers: headers)
                : _client.post(uri, headers: headers, body: jsonEncode(body)))
            .timeout(timeout);
    if (_closed) throw StateError('已关闭');
    if (r.statusCode != 200) throw IntentConversionFailure(r.statusCode);
    final root = intentMap(jsonDecode(utf8.decode(r.bodyBytes)));
    intentKeys(root, {'data'});
    return intentMap(root['data']);
  }

  Future<ConversionOptions> options(
    String auth,
    String owner,
    String id,
  ) async => ConversionOptions(await _request(auth, id, ''), owner, id);
  Future<ConversionPreview> preview(
    String auth,
    String owner,
    String id,
    String activity,
    String version,
  ) async {
    final v = ConversionPreview(
      await _request(auth, id, '/preview', {
        'activityId': intentID(activity),
        'expectedVersion': conversionVersion(version),
      }),
      owner,
      id,
    );
    if (v.choice.activity.activityId != activity || v.version != version) {
      throw const FormatException('被批准的来源或版本不符');
    }
    return v;
  }

  Future<ConversionReceipt> approve(
    String auth,
    String owner,
    String id,
    ConversionPreview p,
  ) async {
    final v = ConversionReceipt(
      await _request(auth, id, '/approve', {'previewId': p.previewID}),
      owner,
      id,
    );
    if (v.agentID != p.agentID ||
        v.activityID != p.choice.activity.activityId ||
        v.participationID != p.choice.participationID ||
        v.observedAt.isBefore(p.observedAt) ||
        !v.observedAt.isBefore(p.expiresAt)) {
      throw const FormatException('回执不属于本次具体预览');
    }
    return v;
  }

  void close() {
    if (_closed) return;
    _closed = true;
    if (_owned) _client.close();
  }
}
