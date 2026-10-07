import 'dart:async';
import 'dart:convert';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'agent_memory_candidate_api.dart';
import 'agent_profile_api.dart' show profileFieldLabels;

const taskContextPrivacyPath = '/v1/me/agent-context/grants';
Map<String, dynamic> _object(
  dynamic v,
  Set<String> keys, [
  Set<String> optional = const {},
]) {
  if (v is! Map<String, dynamic> ||
      !v.keys.toSet().containsAll(keys) ||
      v.keys.toSet().difference(keys.union(optional)).isNotEmpty) {
    throw const FormatException('任务资料许可结构不符。');
  }
  return v;
}

void _require(bool v) {
  if (!v) throw const FormatException('任务资料许可内容不符。');
}

DateTime _time(dynamic v) {
  final t = candidateTime(v);
  _require(t.year >= 1 && t.year <= 9999);
  return t;
}

List<String> _strings(dynamic v, int max, {Set<String>? choices}) {
  _require(v is List && v.length <= max);
  final values = (v as List).map((x) {
    _require(x is String && (choices?.contains(x) ?? candidateIDValid(x)));
    return x as String;
  }).toList();
  _require(values.toSet().length == values.length);
  return List.unmodifiable(values);
}

class TaskContextPrivacyGrant {
  TaskContextPrivacyGrant._(
    this.id,
    this.revision,
    this.taskID,
    this.cityID,
    this.taskUpdatedAt,
    this.profileFields,
    this.memoryIDs,
    this.placeIDs,
    this.activityIDs,
    this.tieIDs,
    this.policyFamilies,
    this.createdAt,
    this.expiresAt,
    this.revokedAt,
  );
  final String id, taskID, cityID;
  final int revision;
  final DateTime taskUpdatedAt, createdAt, expiresAt;
  final DateTime? revokedAt;
  final List<String> profileFields,
      memoryIDs,
      placeIDs,
      activityIDs,
      tieIDs,
      policyFamilies;
  bool get revoked => revokedAt != null;
  static TaskContextPrivacyGrant read(dynamic v, DateTime observed) {
    final m = _object(
      v,
      {
        'id',
        'revision',
        'purpose',
        'taskId',
        'cityId',
        'taskUpdatedAt',
        'profileFields',
        'memoryIds',
        'placeIds',
        'activityIds',
        'relationshipTieIds',
        'policyFamilies',
        'createdAt',
        'expiresAt',
      },
      {'revokedAt'},
    );
    _require(
      candidateIDValid(m['id']) &&
          candidateIDValid(m['taskId']) &&
          m['purpose'] == 'TASK_CONTEXT_READ' &&
          m['revision'] is int &&
          m['revision'] > 0 &&
          m['revision'] <= 9007199254740991 &&
          m['cityId'] is String &&
          m['cityId'].isNotEmpty &&
          m['cityId'].length <= 100 &&
          m['cityId'].trim() == m['cityId'] &&
          !m['cityId'].contains(RegExp(r'[\x00\r\n]')),
    );
    final updated = _time(m['taskUpdatedAt']),
        created = _time(m['createdAt']),
        expires = _time(m['expiresAt']),
        revoked = m['revokedAt'] == null ? null : _time(m['revokedAt']);
    _require(
      !updated.isAfter(created) &&
          !created.isAfter(observed) &&
          expires.isAfter(created) &&
          expires.difference(created) <= const Duration(minutes: 5) &&
          (revoked == null ||
              (!revoked.isBefore(created) && !revoked.isAfter(observed))),
    );
    final fields = _strings(
          m['profileFields'],
          3,
          choices: profileFieldLabels.keys.toSet(),
        ),
        memory = _strings(m['memoryIds'], 3),
        places = _strings(m['placeIds'], 5),
        activities = _strings(m['activityIds'], 5),
        ties = _strings(m['relationshipTieIds'], 3),
        policies = _strings(
          m['policyFamilies'],
          1,
          choices: {'ATTENTION', 'SOCIAL', 'AUTONOMY'},
        );
    _require(
      fields.length +
              memory.length +
              places.length +
              activities.length +
              ties.length +
              policies.length >
          0,
    );
    return TaskContextPrivacyGrant._(
      m['id'],
      m['revision'],
      m['taskId'],
      m['cityId'],
      updated,
      fields,
      memory,
      places,
      activities,
      ties,
      policies,
      created,
      expires,
      revoked,
    );
  }

  bool sameScope(TaskContextPrivacyGrant b) =>
      taskID == b.taskID &&
      cityID == b.cityID &&
      taskUpdatedAt == b.taskUpdatedAt &&
      createdAt == b.createdAt &&
      expiresAt == b.expiresAt &&
      jsonEncode([
            profileFields,
            memoryIDs,
            placeIDs,
            activityIDs,
            tieIDs,
            policyFamilies,
          ]) ==
          jsonEncode([
            b.profileFields,
            b.memoryIDs,
            b.placeIDs,
            b.activityIDs,
            b.tieIDs,
            b.policyFamilies,
          ]);
  static TaskContextPrivacyGrant original(
    dynamic v,
    String agentID,
    DateTime observed,
  ) {
    final m = _object(
      v,
      {
        'id',
        'revision',
        'purpose',
        'selection',
        'sources',
        'createdAt',
        'expiresAt',
      },
      {'revokedAt'},
    );
    final s = _object(
      m['selection'],
      {
        'agentId',
        'taskId',
        'cityId',
        'queryDigest',
        'taskUpdatedAt',
        'profileFields',
        'memoryIds',
        'placeIds',
        'activityIds',
        'relationshipTieIds',
        'policyFamilies',
        'deadlineAt',
      },
      {'currentQuery'},
    );
    _require(
      s['agentId'] == agentID &&
          s['queryDigest'] is String &&
          RegExp(r'^[0-9a-f]{64}$').hasMatch(s['queryDigest']) &&
          m['sources'] is List &&
          (m['sources'] as List).length <= 21,
    );
    final deadline = _time(s['deadlineAt']);
    _require(!deadline.isBefore(_time(m['expiresAt'])));
    // Validate known legacy metadata, but never retain/display query digests,
    // private query strings, native source row handles or execution authority.
    for (final raw in m['sources']) {
      final source = _object(
        raw,
        {'kind', 'id', 'version', 'nativeTime'},
        {'rowToken'},
      );
      _require(source['kind'] is String && source['id'] is String);
      _time(source['nativeTime']);
      final version = _object(
        source['version'],
        {'kind'},
        {'revision', 'token'},
      );
      _require(
        version['kind'] == 'REVISION' || version['kind'] == 'UPDATED_AT_DIGEST',
      );
    }
    final result = read({
      for (final k in [
        'id',
        'revision',
        'purpose',
        'createdAt',
        'expiresAt',
        'revokedAt',
      ])
        if (m.containsKey(k)) k: m[k],
      for (final k in [
        'taskId',
        'cityId',
        'taskUpdatedAt',
        'profileFields',
        'memoryIds',
        'placeIds',
        'activityIds',
        'relationshipTieIds',
        'policyFamilies',
      ])
        k: s[k],
    }, observed);
    return result;
  }
}

class TaskContextPrivacyInventory {
  TaskContextPrivacyInventory._(
    this.ownerID,
    this.agentID,
    this.observedAt,
    this.validUntil,
    this.truncated,
    this.grants,
  );
  final String ownerID, agentID;
  final DateTime observedAt, validUntil;
  final bool truncated;
  final List<TaskContextPrivacyGrant> grants;
  static TaskContextPrivacyInventory read(dynamic v, String owner) {
    final m = _object(v, {
          'schemaVersion',
          'owner',
          'agentId',
          'observedAt',
          'validUntil',
          'limit',
          'truncated',
          'grants',
        }),
        p = _object(m['owner'], {'type', 'id'});
    final at = _time(m['observedAt']), until = _time(m['validUntil']);
    _require(
      m['schemaVersion'] == 'agent-task-context-inventory-v1' &&
          p['type'] == 'PERSON' &&
          p['id'] == owner &&
          candidateIDValid(owner) &&
          candidateIDValid(m['agentId']) &&
          m['limit'] == 50 &&
          m['truncated'] is bool &&
          m['grants'] is List &&
          (m['grants'] as List).length <= 50 &&
          (m['truncated'] == false || (m['grants'] as List).length == 50) &&
          until.isAfter(at) &&
          until.difference(at) <= const Duration(seconds: 30),
    );
    final grants = (m['grants'] as List)
        .map((v) => TaskContextPrivacyGrant.read(v, at))
        .toList();
    _require(grants.map((g) => g.id).toSet().length == grants.length);
    return TaskContextPrivacyInventory._(
      owner,
      m['agentId'],
      at,
      until,
      m['truncated'],
      List.unmodifiable(grants),
    );
  }
}

class TaskContextPrivacyHTTPError implements Exception {
  const TaskContextPrivacyHTTPError(this.status);
  final int status;
}

class TaskContextPrivacyAPI {
  TaskContextPrivacyAPI({http.Client? client, String? apiBaseUrl})
    : _client = client ?? http.Client(),
      _owned = client == null,
      _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final http.Client _client;
  final bool _owned;
  final String _base;
  bool _closed = false;
  Future<dynamic> _request(
    String method,
    String auth,
    String path, {
    Map<String, Object>? body,
  }) async {
    final uri = Uri.parse('${_base.replaceFirst(RegExp(r'/$'), '')}$path');
    _require(
      !_closed &&
          auth.isNotEmpty &&
          {'http', 'https'}.contains(uri.scheme) &&
          uri.host.isNotEmpty &&
          uri.userInfo.isEmpty &&
          !uri.hasQuery &&
          !uri.hasFragment,
    );
    final request = http.Request(method, uri)
      ..headers.addAll({'Authorization': auth, 'Accept': 'application/json'});
    if (body != null) {
      request.headers['Content-Type'] = 'application/json';
      request.body = jsonEncode(body);
    }
    final watch = Stopwatch()..start();
    Duration left() {
      final d = const Duration(seconds: 12) - watch.elapsed;
      if (d <= Duration.zero) throw TimeoutException('许可结果暂未确认。');
      return d;
    }

    final response = await _client.send(request).timeout(left());
    final stream = StreamIterator(response.stream);
    final bytes = <int>[];
    try {
      while (await stream.moveNext().timeout(left())) {
        _require(bytes.length + stream.current.length <= 64 * 1024);
        bytes.addAll(stream.current);
      }
    } finally {
      unawaited(stream.cancel().catchError((Object _) {}));
    }
    if (response.statusCode != 200) {
      throw TaskContextPrivacyHTTPError(response.statusCode);
    }
    return _object(jsonDecode(utf8.decode(bytes)), {'data'})['data'];
  }

  Future<TaskContextPrivacyInventory> list(String auth, String owner) async =>
      TaskContextPrivacyInventory.read(
        await _request('GET', auth, taskContextPrivacyPath),
        owner,
      );
  Future<TaskContextPrivacyGrant> readGrant(
    String auth,
    String agent,
    TaskContextPrivacyGrant original,
    DateTime Function() now,
  ) async {
    final v = TaskContextPrivacyGrant.original(
      await _request('GET', auth, '$taskContextPrivacyPath/${original.id}'),
      agent,
      now(),
    );
    _require(v.id == original.id && v.sameScope(original));
    return v;
  }

  Future<TaskContextPrivacyGrant> revoke(
    String auth,
    String agent,
    TaskContextPrivacyGrant original,
    DateTime Function() now,
  ) async {
    final v = TaskContextPrivacyGrant.original(
      await _request(
        'DELETE',
        auth,
        '$taskContextPrivacyPath/${original.id}',
        body: {'expectedRevision': original.revision},
      ),
      agent,
      now(),
    );
    _require(
      v.id == original.id &&
          v.sameScope(original) &&
          v.revoked &&
          v.revision == original.revision + 1,
    );
    return v;
  }

  void dispose() {
    if (_closed) return;
    _closed = true;
    if (_owned) _client.close();
  }
}
