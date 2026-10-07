import 'dart:async';
import 'dart:convert';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'agent_memory_candidate_api.dart';
import 'agent_multi_candidate_api.dart';

Map<String, dynamic> _object(
  dynamic value,
  Set<String> required, [
  Set<String> optional = const {},
]) {
  if (value is! Map<String, dynamic> ||
      !value.keys.toSet().containsAll(required) ||
      value.keys.toSet().difference(required.union(optional)).isNotEmpty) {
    throw const FormatException('许可清单结构不符。');
  }
  return value;
}

DateTime _time(dynamic value) {
  final at = candidateTime(value);
  if (at.year < 1 || at.year > 9999) throw const FormatException('许可时间不符。');
  return at;
}

class EnrichmentPrivacyInventory {
  EnrichmentPrivacyInventory._(
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
  final List<CandidatePurposeGrant> grants;
  static EnrichmentPrivacyInventory read(dynamic value, String owner) {
    final m = _object(value, {
      'schemaVersion',
      'owner',
      'agentId',
      'observedAt',
      'validUntil',
      'limit',
      'truncated',
      'grants',
    });
    final p = _object(m['owner'], {'type', 'id'});
    final at = _time(m['observedAt']), until = _time(m['validUntil']);
    if (m['schemaVersion'] != 'agent-enrichment-purpose-inventory-v1' ||
        p['type'] != 'PERSON' ||
        p['id'] != owner ||
        !candidateIDValid(owner) ||
        !candidateIDValid(m['agentId']) ||
        m['limit'] != 50 ||
        m['truncated'] is! bool ||
        m['grants'] is! List ||
        (m['grants'] as List).length > 50 ||
        (m['truncated'] == true && (m['grants'] as List).length != 50) ||
        !until.isAfter(at) ||
        until.difference(at) > const Duration(seconds: 30)) {
      throw const FormatException('许可清单的本人身份、范围或期限不符。');
    }
    final grants = <CandidatePurposeGrant>[];
    for (final v in m['grants'] as List) {
      final g = _object(
        v,
        {
          'schemaVersion',
          'id',
          'previewId',
          'purpose',
          'owner',
          'agentId',
          'selection',
          'revision',
          'createdAt',
          'expiresAt',
          'observedAt',
          'modelAccess',
          'candidateRetentionAllowed',
        },
        {'revokedAt'},
      );
      _object(g['owner'], {'type', 'id'});
      if (!_time(g['observedAt']).isAtSameMomentAs(at)) {
        throw const FormatException('许可不是同一读取快照。');
      }
      grants.add(
        CandidatePurposeGrant.read(
          g,
          owner,
          multi: false,
          selection: _object(g['selection'], {
            'taskId',
            'momentId',
            'momentRevision',
            'fields',
            'deadlineAt',
          }),
          previewID: g['previewId'] as String,
          agentID: m['agentId'] as String,
        ),
      );
    }
    if (grants.map((g) => g.id).toSet().length != grants.length) {
      throw const FormatException('许可清单重复。');
    }
    return EnrichmentPrivacyInventory._(
      owner,
      m['agentId'] as String,
      at,
      until,
      m['truncated'] as bool,
      List.unmodifiable(grants),
    );
  }
}

class EnrichmentPrivacyAPI {
  EnrichmentPrivacyAPI({http.Client? client, String? apiBaseUrl})
    : _client = client ?? http.Client(),
      _owned = client == null,
      _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final http.Client _client;
  final bool _owned;
  final String _base;
  late final _transport = AgentMemoryCandidateAPI(
    client: _client,
    apiBaseUrl: _base,
  );
  late final original = AgentMultiCandidateAPI(_transport);
  static void _metadata(CandidatePurposeGrant g) {
    _object(
      g.raw,
      {
        'schemaVersion',
        'id',
        'previewId',
        'purpose',
        'owner',
        'agentId',
        'selection',
        'revision',
        'createdAt',
        'expiresAt',
        'observedAt',
        'modelAccess',
        'candidateRetentionAllowed',
      },
      {'revokedAt'},
    );
    _object(g.raw['owner'], {'type', 'id'});
  }

  Future<CandidatePurposeGrant> readGrant(
    String auth,
    String owner,
    CandidatePurposeGrant g,
  ) async {
    final value = await original.readGrant(auth, owner, g);
    _metadata(value);
    return value;
  }

  Future<CandidatePurposeGrant> revoke(
    String auth,
    String owner,
    CandidatePurposeGrant g,
  ) async {
    final value = await original.revoke(auth, owner, g);
    _metadata(value);
    return value;
  }

  String get environment => _transport.recoveryEnvironment;
  Future<EnrichmentPrivacyInventory> list(String auth, String owner) async {
    Future<EnrichmentPrivacyInventory> read() async {
      final uri = Uri.parse(
        '${_base.replaceFirst(RegExp(r'/$'), '')}/v1/me/agent-enrichment-purpose/grants',
      );
      if (!uri.hasAuthority ||
          !const ['http', 'https'].contains(uri.scheme) ||
          uri.userInfo.isNotEmpty ||
          uri.hasQuery ||
          uri.hasFragment) {
        throw const FormatException('当前接口未配置。');
      }
      final request = http.Request('GET', uri)
        ..headers.addAll({'Authorization': auth, 'Accept': 'application/json'});
      final response = await _client.send(request);
      if (response.statusCode != 200) {
        throw CandidateAPIError(response.statusCode);
      }
      final bytes = <int>[];
      await for (final part in response.stream) {
        if (bytes.length + part.length > 64 * 1024) {
          throw const FormatException('许可清单超出大小限制。');
        }
        bytes.addAll(part);
      }
      final wrapper = _object(jsonDecode(utf8.decode(bytes)), {'data'});
      return EnrichmentPrivacyInventory.read(wrapper['data'], owner);
    }

    return read().timeout(const Duration(seconds: 20));
  }

  void dispose() {
    _transport.dispose();
    if (_owned) _client.close();
  }
}
