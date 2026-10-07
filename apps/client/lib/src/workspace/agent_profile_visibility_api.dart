import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import '../content/agent_profile_completion_api.dart'
    show completionMap, completionTime, completionVersion;
import 'agent_memory_candidate_api.dart' show candidateIDValid;
import 'agent_profile_api.dart' show profileFieldLabels;
import 'community_api.dart';

const profileVisibilityLabels = {
  'displayName': '名称',
  'bio': '简介',
  ...profileFieldLabels,
};
const profileAudienceLabels = {
  'PUBLIC': '公开范围',
  'CONNECTIONS': '已确认的好友',
  'COMMUNITY': '指定社群的当前成员',
  'PRIVATE': '仅本人',
  'AGENT_ONLY': '本人智能体',
};
const profileAudienceConsequences = {
  'PUBLIC': '仍受整体资料范围及原内容权限限制，不会自动公开其他字段。',
  'CONNECTIONS': '只有当前有效好友；关注、聊天或待处理请求不算好友。',
  'COMMUNITY': '仅在你与对方仍是所选有效社群成员时适用。',
  'PRIVATE': '仅本人查看与管理，好友和组织身份不会继承。',
  'AGENT_ONLY': '对他人隐藏；仍需当前用途授权，不会开启模型、分析、学习或记忆。',
};

Never _invalidVisibility() => throw const FormatException('资料受众设置格式或本人绑定不符。');

class ProfileFieldAudience {
  ProfileFieldAudience(
    String visibility, [
    List<String> communityIDs = const [],
  ]) : visibility = visibility,
       communityIDs = List.unmodifiable(communityIDs) {
    if (!profileAudienceLabels.containsKey(visibility) ||
        (visibility == 'COMMUNITY'
            ? communityIDs.isEmpty ||
                  communityIDs.length > 8 ||
                  communityIDs.any(
                    (id) => !candidateIDValid(id) || id != id.toLowerCase(),
                  ) ||
                  communityIDs.toSet().length != communityIDs.length
            : communityIDs.isNotEmpty)) {
      _invalidVisibility();
    }
  }
  final String visibility;
  final List<String> communityIDs;
  factory ProfileFieldAudience.read(dynamic raw) {
    final m = completionMap(raw, {'visibility', 'communityIds'});
    if (m['visibility'] is! String ||
        m['communityIds'] is! List ||
        (m['communityIds'] as List).any((id) => id is! String)) {
      _invalidVisibility();
    }
    return ProfileFieldAudience(
      m['visibility'],
      List<String>.from(m['communityIds']),
    );
  }
  Map<String, Object> toJson() => {
    'visibility': visibility,
    'communityIds': [...communityIDs]..sort(),
  };
  bool same(ProfileFieldAudience other) =>
      visibility == other.visibility &&
      setEquals(communityIDs.toSet(), other.communityIDs.toSet());
}

bool profileRulesSame(
  Map<String, ProfileFieldAudience> a,
  Map<String, ProfileFieldAudience> b,
) =>
    a.length == b.length &&
    a.keys.every((key) => b[key] != null && a[key]!.same(b[key]!));
Map<String, Object> profileRulesWire(Map<String, ProfileFieldAudience> rules) {
  if (!setEquals(rules.keys.toSet(), profileVisibilityLabels.keys.toSet())) {
    _invalidVisibility();
  }
  return {
    for (final key in profileVisibilityLabels.keys) key: rules[key]!.toJson(),
  };
}

class ProfileVisibilityRecord {
  ProfileVisibilityRecord.read(dynamic raw, String owner) {
    final m = completionMap(raw, {
      'schemaVersion',
      'profile',
      'rules',
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
    if (m['schemaVersion'] != 'agent-profile-visibility-v1' ||
        m['configured'] is! bool ||
        p['ownerType'] != 'PERSON' ||
        p['ownerId'] != owner ||
        !candidateIDValid(owner) ||
        !candidateIDValid(p['agentId']) ||
        !completionVersion(p['profileVersion'])) {
      _invalidVisibility();
    }
    createdAt = completionTime(p['createdAt']);
    updatedAt = completionTime(p['updatedAt']);
    if (updatedAt.isBefore(createdAt)) _invalidVisibility();
    ownerID = owner;
    agentID = p['agentId'];
    version = p['profileVersion'];
    configured = m['configured'];
    final r = completionMap(m['rules'], profileVisibilityLabels.keys.toSet());
    rules = Map.unmodifiable({
      for (final key in profileVisibilityLabels.keys)
        key: ProfileFieldAudience.read(r[key]),
    });
    if (!configured &&
        rules.entries.any(
          (e) =>
              e.value.visibility !=
              (const {'displayName', 'bio'}.contains(e.key)
                  ? 'PUBLIC'
                  : 'PRIVATE'),
        )) {
      _invalidVisibility();
    }
  }
  late final String ownerID, agentID;
  late final int version;
  late final bool configured;
  late final DateTime createdAt, updatedAt;
  late final Map<String, ProfileFieldAudience> rules;
}

class ProfileVisibilityHTTPError implements Exception {
  const ProfileVisibilityHTTPError(this.status);
  final int status;
}

class AgentProfileVisibilityAPI {
  AgentProfileVisibilityAPI({http.Client? client, String? apiBaseUrl})
    : _client = client ?? http.Client(),
      _owned = client == null,
      _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final http.Client _client;
  final bool _owned;
  final String _base;
  bool _closed = false;
  Future<dynamic> _request(
    String method,
    String auth, {
    Map<String, Object>? body,
  }) async {
    final uri = Uri.parse(
      '${_base.replaceFirst(RegExp(r'/$'), '')}/v1/me/agent-profile-visibility',
    );
    if (_closed ||
        auth.isEmpty ||
        !const {'http', 'https'}.contains(uri.scheme) ||
        uri.host.isEmpty ||
        uri.userInfo.isNotEmpty ||
        uri.hasQuery ||
        uri.hasFragment) {
      throw const FormatException('当前资料接口未配置或已关闭。');
    }
    final request = http.Request(method, uri)
      ..headers.addAll({'Authorization': auth, 'Accept': 'application/json'});
    if (body != null) {
      request.headers['Content-Type'] = 'application/json';
      request.body = jsonEncode(body);
      if (request.bodyBytes.length > 16 * 1024) _invalidVisibility();
    }
    final elapsed = Stopwatch()..start();
    Duration remaining() {
      final time = const Duration(seconds: 12) - elapsed.elapsed;
      if (time <= Duration.zero) throw TimeoutException('读取或保存暂未取得结果。');
      return time;
    }

    final response = await _client.send(request).timeout(remaining());
    final parts = StreamIterator(response.stream);
    final bytes = <int>[];
    try {
      while (await parts.moveNext().timeout(remaining())) {
        if (bytes.length + parts.current.length > 64 * 1024) {
          _invalidVisibility();
        }
        bytes.addAll(parts.current);
      }
    } finally {
      unawaited(parts.cancel().catchError((Object _) {}));
    }
    if (response.statusCode != 200) {
      throw ProfileVisibilityHTTPError(response.statusCode);
    }
    return completionMap(jsonDecode(utf8.decode(bytes)), {'data'})['data'];
  }

  Future<ProfileVisibilityRecord> read(String auth, String owner) async =>
      ProfileVisibilityRecord.read(await _request('GET', auth), owner);
  Future<ProfileVisibilityRecord> replace(
    String auth,
    ProfileVisibilityRecord original,
    Map<String, ProfileFieldAudience> rules,
  ) async {
    final result = ProfileVisibilityRecord.read(
      await _request(
        'PUT',
        auth,
        body: {
          'expectedVersion': original.version,
          'rules': profileRulesWire(rules),
        },
      ),
      original.ownerID,
    );
    if (result.agentID != original.agentID ||
        result.version != original.version + 1 ||
        result.createdAt != original.createdAt ||
        result.updatedAt.isBefore(original.updatedAt) ||
        !profileRulesSame(result.rules, rules)) {
      _invalidVisibility();
    }
    return result;
  }

  Future<List<CommunityItem>> communities(
    String auth,
    bool Function() current,
  ) async {
    if (_closed || !current()) throw const FormatException('当前资料入口已失效。');
    // Reuse the registered human membership reader with the same borrowed
    // transport. CommunityApi.dispose closes borrowed clients, so this wrapper
    // is not separately disposed; only the owning API manages its client.
    final source = CommunityApi(
      client: _client,
      apiBaseUrl: _base,
      authorizationHeader: () => !_closed && current() ? auth : null,
    );
    final rows = await source.mine();
    if (_closed || !current()) throw const FormatException('当前资料入口已失效。');
    if (rows.length > 100 ||
        rows.map((r) => r.id).toSet().length != rows.length ||
        rows.any(
          (r) =>
              !candidateIDValid(r.id) ||
              r.name.trim().isEmpty ||
              r.name.runes.length > 160,
        )) {
      _invalidVisibility();
    }
    return List.unmodifiable(
      rows.where((r) => r.status == 'active' && r.myStatus == 'active'),
    );
  }

  void dispose() {
    if (_closed) return;
    _closed = true;
    if (_owned) _client.close();
  }
}
