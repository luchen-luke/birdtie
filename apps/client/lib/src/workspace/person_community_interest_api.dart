import 'dart:convert';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'model_egress_api.dart' show egressTime, egressIDValid;

Map<String, dynamic> _m(dynamic v) {
  if (v is! Map<String, dynamic>) throw const FormatException('声明结构无效');
  return v;
}

void _keys(
  Map<String, dynamic> v,
  Set<String> required, [
  Set<String> optional = const {},
]) {
  if (!v.keys.toSet().containsAll(required) ||
      !required.union(optional).containsAll(v.keys)) {
    throw const FormatException('声明字段无效');
  }
}

String _id(dynamic v) {
  if (!egressIDValid(v)) throw const FormatException('声明标识无效');
  return v as String;
}

String _text(dynamic v, {int max = 4096}) {
  if (v is! String || v.trim().isEmpty || v.length > max) {
    throw const FormatException('声明文本无效');
  }
  return v;
}

const interestSchema = 'human-community-interest-v1';
void _identity(Map<String, dynamic> m, String owner) {
  if (m['schemaVersion'] != interestSchema ||
      _id(m['ownerId']) != owner ||
      m['modelAccess'] != false ||
      m['sendAllowed'] != false ||
      m['membershipGranted'] != false) {
    throw const FormatException('声明主体或权限不符');
  }
  _id(m['agentId']);
}

class CommunityInterestRecord {
  CommunityInterestRecord(Map<String, dynamic> m)
    : communityID = _id(m['communityId']),
      name = _text(m['name'], max: 320),
      state = _text(m['state']) {
    _keys(
      m,
      {'communityId', 'name', 'sourceAvailable', 'relation', 'state'},
      {'contextId'},
    );
    if (m['relation'] != 'interest' ||
        m['sourceAvailable'] is! bool ||
        !['ABSENT', 'PRIVATE', 'PUBLIC'].contains(state)) {
      throw const FormatException('声明状态无效');
    }
    sourceAvailable = m['sourceAvailable'] as bool;
    contextID = m['contextId'] == null ? null : _id(m['contextId']);
    if (state != 'ABSENT' && contextID == null) {
      throw const FormatException('声明情境缺失');
    }
  }
  final String communityID, name, state;
  late final String? contextID;
  late final bool sourceAvailable;
}

class CommunityInterestOption {
  CommunityInterestOption(Map<String, dynamic> m)
    : id = _id(m['communityId']),
      name = _text(m['name'], max: 320) {
    _keys(m, {'communityId', 'name'});
  }
  final String id, name;
}

class CommunityInterestView {
  CommunityInterestView(Map<String, dynamic> m, String owner)
    : ownerID = _id(m['ownerId']),
      agentID = _id(m['agentId']),
      observedAt = egressTime(m['observedAt']) {
    _keys(m, {
      'schemaVersion',
      'ownerId',
      'agentId',
      'observedAt',
      'records',
      'options',
      'limit',
      'truncated',
      'modelAccess',
      'sendAllowed',
      'membershipGranted',
    });
    _identity(m, owner);
    if (m['limit'] != 100 ||
        m['truncated'] is! bool ||
        m['records'] is! List ||
        m['options'] is! List) {
      throw const FormatException('声明列表无效');
    }
    truncated = m['truncated'] as bool;
    records = List.unmodifiable(
      (m['records'] as List).map((v) => CommunityInterestRecord(_m(v))),
    );
    options = List.unmodifiable(
      (m['options'] as List).map((v) => CommunityInterestOption(_m(v))),
    );
    if (records.length > 100 ||
        options.length > 100 ||
        records.map((v) => v.communityID).toSet().length != records.length ||
        options.map((v) => v.id).toSet().length != options.length) {
      throw const FormatException('声明列表重复或超限');
    }
  }
  final String ownerID, agentID;
  final DateTime observedAt;
  late final bool truncated;
  late final List<CommunityInterestRecord> records;
  late final List<CommunityInterestOption> options;
}

class CommunityInterestPreview {
  CommunityInterestPreview(
    Map<String, dynamic> m,
    String owner,
    String requestedID,
    String requestedOperation,
  ) : ownerID = _id(m['ownerId']),
      agentID = _id(m['agentId']),
      communityID = _id(m['communityId']),
      name = _text(m['name'], max: 320),
      originalState = _text(m['state']),
      targetState = _text(m['targetState']),
      operation = _text(m['operation']),
      token = _text(m['preview'], max: 24000),
      consequence = _text(m['consequence']),
      observedAt = egressTime(m['observedAt']),
      expiresAt = egressTime(m['expiresAt']) {
    _keys(
      m,
      {
        'schemaVersion',
        'ownerId',
        'agentId',
        'communityId',
        'name',
        'sourceAvailable',
        'relation',
        'state',
        'operation',
        'targetState',
        'preview',
        'observedAt',
        'expiresAt',
        'consequence',
        'modelAccess',
        'sendAllowed',
        'membershipGranted',
      },
      {'contextId'},
    );
    _identity(m, owner);
    if (communityID != requestedID ||
        operation != requestedOperation ||
        !['PUBLIC', 'PRIVATE', 'DELETE'].contains(operation) ||
        targetState != (operation == 'DELETE' ? 'ABSENT' : operation) ||
        !['PUBLIC', 'PRIVATE', 'ABSENT'].contains(originalState) ||
        targetState == originalState ||
        m['relation'] != 'interest' ||
        m['sourceAvailable'] is! bool ||
        (operation == 'PUBLIC' && m['sourceAvailable'] != true) ||
        !expiresAt.isAfter(observedAt) ||
        expiresAt.difference(observedAt) > const Duration(seconds: 90) ||
        !RegExp(r'^[A-Za-z0-9_-]{20,24000}$').hasMatch(token)) {
      throw const FormatException('具体预览版本不符');
    }
    contextID = m['contextId'] == null ? null : _id(m['contextId']);
    if (originalState != 'ABSENT' && m['contextId'] == null) {
      throw const FormatException('具体原声明情境缺失');
    }
  }
  final String ownerID,
      agentID,
      communityID,
      name,
      originalState,
      targetState,
      operation,
      token,
      consequence;
  final DateTime observedAt, expiresAt;
  late final String? contextID;
}

class CommunityInterestHTTPError implements Exception {
  const CommunityInterestHTTPError(this.status);
  final int status;
}

class PersonCommunityInterestAPI {
  PersonCommunityInterestAPI({http.Client? client, String? apiBaseUrl})
    : _client = client ?? http.Client(),
      _owned = client == null,
      _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final http.Client _client;
  final bool _owned;
  final String _base;
  Future<Map<String, dynamic>> _request(
    String method,
    String path,
    String auth, [
    Map<String, dynamic>? body,
  ]) async {
    final uri = Uri.parse('${_base.replaceFirst(RegExp(r'/$'), '')}$path');
    final headers = {'Authorization': auth, 'Content-Type': 'application/json'};
    final response =
        await (method == 'GET'
                ? _client.get(uri, headers: headers)
                : _client.post(uri, headers: headers, body: jsonEncode(body)))
            .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) {
      throw CommunityInterestHTTPError(response.statusCode);
    }
    final v = _m(jsonDecode(utf8.decode(response.bodyBytes)));
    _keys(v, {'data'});
    return _m(v['data']);
  }

  Future<CommunityInterestView> read(
    String auth,
    String owner, {
    bool options = false,
  }) async => CommunityInterestView(
    await _request(
      'GET',
      '/v1/me/community-interests${options ? '/options' : ''}',
      auth,
    ),
    owner,
  );
  Future<CommunityInterestPreview> preview(
    String auth,
    String owner,
    String communityID,
    String operation,
  ) async {
    _id(communityID);
    if (!['PUBLIC', 'PRIVATE', 'DELETE'].contains(operation)) {
      throw const FormatException('操作无效');
    }
    return CommunityInterestPreview(
      await _request('POST', '/v1/me/community-interests/preview', auth, {
        'communityId': communityID,
        'operation': operation,
      }),
      owner,
      communityID,
      operation,
    );
  }

  Future<CommunityInterestView> approve(
    String auth,
    CommunityInterestPreview p,
  ) async {
    final v = CommunityInterestView(
      await _request('POST', '/v1/me/community-interests/approve', auth, {
        'preview': p.token,
      }),
      p.ownerID,
    );
    if (v.agentID != p.agentID ||
        v.records.length != 1 ||
        v.records.single.communityID != p.communityID ||
        v.records.single.state != p.targetState ||
        v.observedAt.isBefore(p.observedAt) ||
        !v.observedAt.isBefore(p.expiresAt) ||
        (p.contextID != null && v.records.single.contextID != p.contextID) ||
        v.options.isNotEmpty ||
        v.truncated) {
      throw const FormatException('权威结果与具体预览不符');
    }
    return v;
  }

  void dispose() {
    if (_owned) _client.close();
  }
}
