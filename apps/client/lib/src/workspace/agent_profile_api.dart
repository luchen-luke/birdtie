import 'dart:async';
import 'dart:convert';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import '../content/agent_profile_completion_api.dart'
    show completionMap, completionVersion;
import 'agent_memory_candidate_api.dart' show candidateIDValid;
import 'agent_memory_correction_api.dart';
import 'agent_introduction_api.dart'
    show IntroductionPolicy, introductionCategories;
import 'person_community_interest_api.dart';
import 'activity_participation_disclosure_api.dart';

void profileRequire(bool value) {
  if (!value) throw const FormatException('智能体资料与当前本人不符');
}

const profileListFields = {
  'personalPreferences',
  'socialPreferences',
  'preferredActivityTypes',
  'travelPreferences',
  'interactionPreferences',
  'languagePreferences',
};
const profileTextFields = {'availability', 'privateCityHistory', 'agentNotes'};
const profileFieldLabels = {
  'personalPreferences': '个人偏好',
  'socialPreferences': '社交偏好',
  'preferredActivityTypes': '偏好的活动类型',
  'travelPreferences': '出行偏好',
  'interactionPreferences': '交流方式',
  'languagePreferences': '交流语言',
  'availability': '本人填写的可用时间',
  'privateCityHistory': '本人填写的城市经历',
  'agentNotes': '本人填写的补充说明',
};

class AgentPrivateProfileView {
  AgentPrivateProfileView(dynamic raw, String owner) {
    final m = completionMap(raw, {
      'schemaVersion',
      'profile',
      'fields',
      'configured',
    });
    final p = completionMap(m['profile'], {
      'agentId',
      'ownerType',
      'ownerId',
      'profileVersion',
      'createdAt',
      'updatedAt',
    });
    profileRequire(
      m['schemaVersion'] == 'private-agent-profile-v1' &&
          m['configured'] is bool &&
          p['ownerType'] == 'PERSON' &&
          p['ownerId'] == owner &&
          candidateIDValid(owner) &&
          candidateIDValid(p['agentId']) &&
          completionVersion(p['profileVersion']),
    );
    final created = correctionTime(p['createdAt']),
        updated = correctionTime(p['updatedAt']);
    profileRequire(!updated.isBefore(created));
    agentID = p['agentId'];
    configured = m['configured'];
    final f = completionMap(m['fields'], {
      ...profileListFields,
      ...profileTextFields,
    });
    final result = <String, Object>{};
    for (final k in profileListFields) {
      final v = f[k];
      profileRequire(v is List && v.length <= 20);
      final values = <String>[];
      for (final item in v as List) {
        profileRequire(
          item is String &&
              item.trim().isNotEmpty &&
              item.runes.length <= 160 &&
              !values.contains(item),
        );
        values.add(item as String);
      }
      result[k] = List<String>.unmodifiable(values);
    }
    for (final k in profileTextFields) {
      final v = f[k];
      profileRequire(v is String && v.runes.length <= 2000);
      result[k] = v as String;
    }
    profileRequire(
      configured ||
          result.values.every(
            (v) => v is List ? v.isEmpty : (v as String).isEmpty,
          ),
    );
    fields = Map.unmodifiable(result);
  }
  late final String agentID;
  late final bool configured;
  late final Map<String, Object> fields;
}

const memoryDetailExplanation =
    '本人管理记忆；过期或已丢弃内容不在详情中返回。待审推断仅为预留形状，不代表已确认事实或模型读取许可。';

class AgentMemoryDetailView {
  AgentMemoryDetailView(dynamic raw, String owner, String requestedID) {
    final m = completionMap(raw, {
      'schemaVersion',
      'owner',
      'agentId',
      'target',
      'memory',
      'observedAt',
      'expiresAt',
      'explanation',
      'modelAccess',
    }, optional: {'fieldEvidenceSet'});
    final o = completionMap(m['owner'], {'type', 'id'}),
        t = completionMap(m['target'], {'id', 'version', 'status'});
    profileRequire(
      m['schemaVersion'] == 'agent-memory-detail-v1' &&
          o['type'] == 'PERSON' &&
          o['id'] == owner &&
          candidateIDValid(owner) &&
          candidateIDValid(m['agentId']) &&
          t['id'] == requestedID &&
          candidateIDValid(requestedID) &&
          completionVersion(t['version']) &&
          m['modelAccess'] == false &&
          m['explanation'] == memoryDetailExplanation,
    );
    agentID = m['agentId'];
    id = t['id'];
    status = t['status'] as String;
    observedAt = correctionTime(m['observedAt']);
    expiresAt = correctionTime(m['expiresAt']);
    profileRequire(
      expiresAt.isAfter(observedAt) &&
          expiresAt.difference(observedAt) <= const Duration(minutes: 2),
    );
    if ({'ACTIVE', 'PENDING_REVIEW'}.contains(status)) {
      memory = CorrectableMemory.read(m['memory'], owner);
      profileRequire(
        memory!.id == id &&
            memory!.version == t['version'] &&
            memory!.agentID == agentID &&
            memory!.status == status &&
            !correctionTime(memory!.raw['validFrom']).isAfter(observedAt) &&
            !correctionTime(memory!.raw['createdAt']).isAfter(observedAt) &&
            !correctionTime(memory!.raw['updatedAt']).isAfter(observedAt) &&
            memory!.validUntil.isAfter(observedAt) &&
            !expiresAt.isAfter(memory!.validUntil),
      );
    } else {
      profileRequire(
        {'EXPIRED', 'DELETED'}.contains(status) && m['memory'] == null,
      );
      memory = null;
    }
  }
  late final String agentID, id, status;
  late final CorrectableMemory? memory;
  late final DateTime observedAt, expiresAt;
}

const profileRoutes = {'IMMEDIATE', 'NORMAL', 'DIGEST', 'SILENT', 'BLOCK'};
const profileEvents = {
  'MomentCreated',
  'UserQuery',
  'MomentUpdated',
  'MomentDeleted',
  'ActivityJoined',
  'ActivityLeft',
  'ActivityCompleted',
  'PlaceSaved',
  'PlaceVisited',
  'CommunityJoined',
  'CommunityLeft',
  'ProfileUpdated',
  'PreferenceUpdated',
};

class AgentPolicySummary {
  AgentPolicySummary(dynamic raw, String family, DateTime observed) {
    final m = completionMap(
      raw,
      {'family', 'configured', 'nativeRevision', 'status', 'settings'},
      optional: {'validFrom', 'expiresAt', 'updatedAt'},
    );
    profileRequire(
      m['family'] == family &&
          m['configured'] is bool &&
          m['nativeRevision'] is int &&
          m['nativeRevision'] >= 0 &&
          m['nativeRevision'] <= 9223372036854775806 &&
          m['status'] is String,
    );
    configured = m['configured'];
    status = m['status'];
    final s = m['settings'];
    if (family == 'ATTENTION') {
      final v = completionMap(
        s,
        {'defaultRoute', 'rules'},
        optional: {'pauseUntil'},
      );
      profileRequire(
        profileRoutes.contains(v['defaultRoute']) &&
            v['rules'] is List &&
            v['rules'].length <= profileEvents.length,
      );
      final seen = <String>{};
      for (final r in v['rules']) {
        final rule = completionMap(r, {'eventType', 'route'});
        profileRequire(
          profileEvents.contains(rule['eventType']) &&
              profileRoutes.contains(rule['route']) &&
              seen.add(rule['eventType']),
        );
      }
      if (v.containsKey('pauseUntil')) correctionTime(v['pauseUntil']);
      caption = const {
        'IMMEDIATE': '即时提醒',
        'NORMAL': '普通提醒',
        'DIGEST': '汇总提醒',
        'SILENT': '静默',
        'BLOCK': '不提醒',
      }[v['defaultRoute']]!;
      profileRequire(
        configured ||
            (v['defaultRoute'] == 'BLOCK' &&
                (v['rules'] as List).isEmpty &&
                v['pauseUntil'] == null),
      );
    } else if (family == 'AUTONOMY') {
      final v = completionMap(s, {'level'});
      const levels = {
        'LEVEL_0_OBSERVE': '观察',
        'LEVEL_1_ASSIST': '辅助',
        'LEVEL_2_PREPARE': '准备供本人确认',
        'LEVEL_3_DELEGATE': '委托',
      };
      profileRequire(
        levels.containsKey(v['level']) &&
            (configured || v['level'] == 'LEVEL_0_OBSERVE'),
      );
      caption = levels[v['level']]!;
    } else {
      final v = completionMap(s, {'rules'});
      profileRequire(v['rules'] is List && v['rules'].length == 7);
      final seen = <String>{};
      for (final r in v['rules']) {
        final rule = completionMap(r, {'category', 'preference'});
        profileRequire(
          introductionCategories.contains(rule['category']) &&
              {'DISABLED', 'REVIEW_REQUIRED'}.contains(rule['preference']) &&
              seen.add(rule['category']) &&
              (configured || rule['preference'] == 'DISABLED'),
        );
      }
      caption = '社交建议需按当前规则逐项检查';
    }
    if (!configured) {
      profileRequire(
        m['nativeRevision'] == 0 &&
            status == 'UNCONFIGURED' &&
            m['validFrom'] == null &&
            m['expiresAt'] == null &&
            m['updatedAt'] == null,
      );
      expiresAt = null;
    } else {
      final start = correctionTime(m['validFrom']),
          updated = correctionTime(m['updatedAt']);
      expiresAt = correctionTime(m['expiresAt']);
      profileRequire(
        m['nativeRevision'] > 0 &&
            updated == start &&
            !start.isAfter(observed) &&
            expiresAt!.isAfter(start) &&
            expiresAt!.difference(start) <= const Duration(days: 30) &&
            status == (observed.isBefore(expiresAt!) ? 'ACTIVE' : 'EXPIRED'),
      );
      if (family == 'ATTENTION' && (s as Map).containsKey('pauseUntil')) {
        final pause = correctionTime(s['pauseUntil']);
        profileRequire(!pause.isBefore(start) && !pause.isAfter(expiresAt!));
      }
    }
  }
  late final bool configured;
  late final String status, caption;
  late final DateTime? expiresAt;
}

class AgentPoliciesView {
  AgentPoliciesView(dynamic raw, String owner) {
    final m = completionMap(raw, {
      'schemaVersion',
      'ownerType',
      'ownerId',
      'agentId',
      'observedAt',
      'attention',
      'social',
      'autonomy',
    });
    profileRequire(
      m['schemaVersion'] == 'agent-policy-settings-v1' &&
          m['ownerType'] == 'PERSON' &&
          m['ownerId'] == owner &&
          candidateIDValid(owner) &&
          candidateIDValid(m['agentId']),
    );
    agentID = m['agentId'];
    observedAt = correctionTime(m['observedAt']);
    // Reuse the complete native social rule validation as well.
    IntroductionPolicy(m, owner);
    records = Map.unmodifiable({
      for (final f in ['ATTENTION', 'SOCIAL', 'AUTONOMY'])
        f: AgentPolicySummary(m[f.toLowerCase()], f, observedAt),
    });
  }
  late final String agentID;
  late final DateTime observedAt;
  late final Map<String, AgentPolicySummary> records;
}

class AgentProfileHTTPError implements Exception {
  AgentProfileHTTPError(this.status);
  final int status;
}

/// This read-only adapter exposes no approval or mutation methods.
class AgentProfileAPI {
  AgentProfileAPI({http.Client? client, String? apiBaseUrl})
    : _client = client ?? http.Client(),
      _owns = client == null,
      base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final http.Client _client;
  final bool _owns;
  final String base;
  bool _closed = false;
  Future<dynamic> _get(String token, String path) async {
    final elapsed = Stopwatch()..start();
    Duration remaining() {
      final value = const Duration(seconds: 12) - elapsed.elapsed;
      if (value <= Duration.zero) throw TimeoutException('读取已超时');
      return value;
    }

    profileRequire(!_closed && token.isNotEmpty);
    final uri = Uri.parse('${base.replaceFirst(RegExp(r'/$'), '')}$path');
    profileRequire(
      {'http', 'https'}.contains(uri.scheme) &&
          uri.host.isNotEmpty &&
          uri.userInfo.isEmpty &&
          !uri.hasQuery &&
          !uri.hasFragment,
    );
    final response = await _client
        .send(http.Request('GET', uri)..headers['Authorization'] = token)
        .timeout(remaining());
    final bytes = <int>[];
    final stream = StreamIterator(response.stream);
    try {
      while (await stream.moveNext().timeout(remaining())) {
        bytes.addAll(stream.current);
        profileRequire(bytes.length <= 2 * 1024 * 1024);
      }
    } finally {
      unawaited(stream.cancel().catchError((Object _) {}));
    }
    if (response.statusCode != 200) {
      throw AgentProfileHTTPError(response.statusCode);
    }
    return completionMap(jsonDecode(utf8.decode(bytes)), {'data'})['data'];
  }

  Future<AgentPrivateProfileView> profile(String token, String owner) async =>
      AgentPrivateProfileView(
        await _get(token, '/v1/me/agent-private-profile'),
        owner,
      );
  Future<List<CorrectableMemory>> memories(String token, String owner) async {
    final v = await _get(token, '/v1/me/agent-memories');
    profileRequire(v is List && v.length <= 1000);
    final rows = List<CorrectableMemory>.unmodifiable(
      (v as List).map((r) => CorrectableMemory.read(r, owner)),
    );
    profileRequire(
      rows.map((r) => r.id).toSet().length == rows.length &&
          rows.map((r) => r.agentID).toSet().length <= 1,
    );
    return rows;
  }

  Future<AgentMemoryDetailView> detail(
    String token,
    String owner,
    String id,
  ) async {
    profileRequire(candidateIDValid(id));
    return AgentMemoryDetailView(
      await _get(token, '/v1/me/agent-memories/$id'),
      owner,
      id,
    );
  }

  Future<AgentPoliciesView> policies(String token, String owner) async =>
      AgentPoliciesView(await _get(token, '/v1/me/agent-policies'), owner);
  Future<CommunityInterestView> interests(String token, String owner) async =>
      CommunityInterestView(
        Map<String, dynamic>.from(
          await _get(token, '/v1/me/community-interests'),
        ),
        owner,
      );
  Future<ParticipationDisclosureView> participations(
    String token,
    String owner,
  ) async => ParticipationDisclosureView(
    Map<String, dynamic>.from(
      await _get(token, '/v1/me/activity-participation-disclosures'),
    ),
    owner,
  );
  void close() {
    if (_closed) return;
    _closed = true;
    if (_owns) _client.close();
  }
}
