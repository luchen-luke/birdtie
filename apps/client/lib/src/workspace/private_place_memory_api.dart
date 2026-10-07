import 'dart:convert';
import 'dart:math';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';

const placeMemoryKindLabels = {
  'SAVED': '已收藏这个地点（不表示喜欢或到访）',
  'CREATED_MOMENT_AT': '本人动态关联此地点（不证明到访）',
  'LIKED': '本人明确表示喜欢这个地点',
  'VISITED': '本人自述到访过这个地点（未经核验）',
};
const placeMemoryVisibilityLabels = {
  'PRIVATE': '仅本人管理',
  'AGENT_ONLY': 'Agent 范围标记（仍需另行认知授权）',
};
final placeMemoryIDPattern = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
bool validPlaceMemoryID(Object? v) =>
    v is String &&
    placeMemoryIDPattern.hasMatch(v) &&
    v != '00000000-0000-0000-0000-000000000000';
Never _invalid() => throw const FormatException('地点记录格式无效');
Map<String, dynamic> _object(
  dynamic raw,
  Set<String> required, [
  Set<String> optional = const {},
]) {
  if (raw is! Map<String, dynamic> ||
      required.difference(raw.keys.toSet()).isNotEmpty ||
      raw.keys.toSet().difference({...required, ...optional}).isNotEmpty) {
    _invalid();
  }
  return raw;
}

String _id(dynamic raw) {
  if (!validPlaceMemoryID(raw)) _invalid();
  return raw as String;
}

int _version(dynamic raw, {bool zero = false}) {
  if (raw is! int || raw < (zero ? 0 : 1) || raw > 9007199254740990) _invalid();
  return raw;
}

DateTime placeMemoryStamp(dynamic raw) {
  if (raw is! String) _invalid();
  final m = RegExp(
    r'^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?(Z|([+-])(\d{2}):(\d{2}))$',
  ).firstMatch(raw);
  if (m == null || m.end != raw.length) _invalid();
  final v = [for (var i = 1; i <= 6; i++) int.parse(m.group(i)!)];
  final micros = int.parse((m.group(7) ?? '').padRight(6, '0').substring(0, 6));
  final t = DateTime.utc(v[0], v[1], v[2], v[3], v[4], v[5], 0, micros);
  if (v[0] < 1 ||
      v[0] > 9999 ||
      t.year != v[0] ||
      t.month != v[1] ||
      t.day != v[2] ||
      t.hour != v[3] ||
      t.minute != v[4] ||
      t.second != v[5]) {
    _invalid();
  }
  // Validate the written calendar before applying its explicit RFC3339 offset.
  // DateTime constructors normalize invalid dates, so the round trip above is
  // required even when the resulting UTC date would otherwise be valid.
  var offsetMinutes = 0;
  if (m.group(8) != 'Z') {
    final hours = int.parse(m.group(10)!);
    final minutes = int.parse(m.group(11)!);
    if (hours > 23 || minutes > 59) _invalid();
    offsetMinutes = (hours * 60 + minutes) * (m.group(9) == '-' ? -1 : 1);
  }
  final utc = t.subtract(Duration(minutes: offsetMinutes));
  if (utc.year < 1 || utc.year > 9999) _invalid();
  return utc;
}

class PrivatePlaceSignal {
  PrivatePlaceSignal._(
    this.kind,
    this.basis,
    this.sourceKind,
    this.sourceID,
    this.revision,
    this.createdAt,
    this.updatedAt,
    this.visibility,
    this.validUntil,
  );
  final String kind, basis, sourceKind, sourceID, visibility;
  final int revision;
  final DateTime createdAt, updatedAt;
  final DateTime? validUntil;
  bool get declaration => kind == 'LIKED' || kind == 'VISITED';
  factory PrivatePlaceSignal.read(
    dynamic raw,
    DateTime observed,
    DateTime lease,
  ) {
    final m = _object(
      raw,
      {
        'kind',
        'basis',
        'sourceKind',
        'sourceId',
        'revision',
        'recordCreatedAt',
        'sourceUpdatedAt',
        'visibility',
      },
      {'validUntil'},
    );
    final kind = m['kind'];
    if (!placeMemoryKindLabels.containsKey(kind)) _invalid();
    final created = placeMemoryStamp(m['recordCreatedAt']),
        updated = placeMemoryStamp(m['sourceUpdatedAt']);
    if (updated.isBefore(created) || updated.isAfter(observed)) _invalid();
    final rev = _version(m['revision'], zero: true);
    DateTime? until;
    if (kind == 'SAVED') {
      if (m['basis'] != 'CURRENT_NATIVE_BOOKMARK' ||
          m['sourceKind'] != 'SAVED_PLACE' ||
          rev != 0 ||
          m.containsKey('validUntil') ||
          m['visibility'] != 'PRIVATE' ||
          created != updated) {
        _invalid();
      }
    } else if (kind == 'CREATED_MOMENT_AT') {
      if (m['basis'] != 'CURRENT_NATIVE_MOMENT_LINK' ||
          m['sourceKind'] != 'MOMENT' ||
          rev < 1 ||
          m.containsKey('validUntil') ||
          m['visibility'] != 'PRIVATE') {
        _invalid();
      }
    } else {
      if (m['basis'] != 'SELF_DECLARATION' ||
          m['sourceKind'] != 'EXPLICIT_PLACE_MEMORY' ||
          rev < 1 ||
          !placeMemoryVisibilityLabels.containsKey(m['visibility'])) {
        _invalid();
      }
      until = placeMemoryStamp(m['validUntil']);
      if (until.isBefore(lease)) _invalid();
    }
    return PrivatePlaceSignal._(
      kind as String,
      m['basis'] as String,
      m['sourceKind'] as String,
      _id(m['sourceId']),
      rev,
      created,
      updated,
      m['visibility'] as String,
      until,
    );
  }
}

class PrivatePlaceMemory {
  PrivatePlaceMemory._(
    this.ownerID,
    this.agentID,
    this.placeID,
    this.cityID,
    this.signals,
    this.observedAt,
    this.expiresAt,
  );
  final String ownerID, agentID, placeID, cityID;
  final List<PrivatePlaceSignal> signals;
  final DateTime observedAt, expiresAt;
  factory PrivatePlaceMemory.read(dynamic raw) {
    final m = _object(raw, {
      'schemaVersion',
      'ownerId',
      'agentId',
      'placeId',
      'cityId',
      'signals',
      'observedAt',
      'expiresAt',
      'verifiedVisit',
      'attendance',
      'modelAccess',
    });
    if (m['schemaVersion'] != 'human-place-memory-v1' ||
        m['verifiedVisit'] != 'UNAVAILABLE' ||
        m['attendance'] != 'UNAVAILABLE' ||
        m['modelAccess'] != 'UNAVAILABLE' ||
        m['cityId'] is! String ||
        !RegExp(r'^[a-z0-9-]{1,100}$').hasMatch(m['cityId'])) {
      _invalid();
    }
    final observed = placeMemoryStamp(m['observedAt']),
        expiry = placeMemoryStamp(m['expiresAt']);
    if (!expiry.isAfter(observed) ||
        expiry.difference(observed) > const Duration(minutes: 2)) {
      _invalid();
    }
    final rows = m['signals'];
    if (rows is! List || rows.length > 100) _invalid();
    final signals = <PrivatePlaceSignal>[], seen = <String>{};
    for (final row in rows) {
      final s = PrivatePlaceSignal.read(row, observed, expiry);
      if (!seen.add('${s.sourceKind}:${s.sourceID}')) _invalid();
      signals.add(s);
    }
    return PrivatePlaceMemory._(
      _id(m['ownerId']),
      _id(m['agentId']),
      _id(m['placeId']),
      m['cityId'] as String,
      List.unmodifiable(signals),
      observed,
      expiry,
    );
  }
  PrivatePlaceSignal? declaration(String kind) {
    final matching = signals
        .where((s) => s.declaration && s.kind == kind)
        .toList();
    if (matching.length > 1) _invalid();
    return matching.isEmpty ? null : matching.single;
  }
}

class PlaceDeclarationDraft {
  const PlaceDeclarationDraft({
    required this.kind,
    required this.visibility,
    required this.validUntil,
  });
  final String kind, visibility;
  final DateTime validUntil;
  Map<String, dynamic> wire(String placeID, int expectedVersion) => {
    'expectedVersion': expectedVersion,
    'placeId': placeID,
    'kind': kind,
    'visibility': visibility,
    'validUntil': validUntil.toUtc().toIso8601String(),
  };
}

class PrivatePlaceDeclaration {
  PrivatePlaceDeclaration._(
    this.memoryID,
    this.version,
    this.kind,
    this.visibility,
    this.status,
    this.validUntil,
    this.updatedAt,
  );
  final String memoryID, kind, visibility, status;
  final int version;
  final DateTime validUntil, updatedAt;
  factory PrivatePlaceDeclaration.read(
    dynamic raw,
    DateTime observed,
    DateTime lease,
  ) {
    final m = _object(raw, {
      'memoryId',
      'version',
      'kind',
      'basis',
      'visibility',
      'status',
      'validUntil',
      'updatedAt',
    });
    if (!{'LIKED', 'VISITED'}.contains(m['kind']) ||
        m['basis'] != 'SELF_DECLARATION' ||
        !placeMemoryVisibilityLabels.containsKey(m['visibility']) ||
        !{'ACTIVE', 'EXPIRED'}.contains(m['status'])) {
      _invalid();
    }
    final until = placeMemoryStamp(m['validUntil']),
        updated = placeMemoryStamp(m['updatedAt']);
    if (updated.isAfter(observed) ||
        (m['status'] == 'ACTIVE' &&
            (!until.isAfter(observed) || until.isBefore(lease))) ||
        (m['status'] == 'EXPIRED' && until.isAfter(observed))) {
      _invalid();
    }
    return PrivatePlaceDeclaration._(
      _id(m['memoryId']),
      _version(m['version']),
      m['kind'],
      m['visibility'],
      m['status'],
      until,
      updated,
    );
  }
}

class PrivatePlaceDeclarationControls {
  PrivatePlaceDeclarationControls._(
    this.ownerID,
    this.agentID,
    this.placeID,
    this.declarations,
    this.observedAt,
    this.expiresAt,
  );
  final String ownerID, agentID, placeID;
  final List<PrivatePlaceDeclaration> declarations;
  final DateTime observedAt, expiresAt;
  factory PrivatePlaceDeclarationControls.read(dynamic raw) {
    final m = _object(raw, {
      'schemaVersion',
      'ownerId',
      'agentId',
      'placeId',
      'declarations',
      'observedAt',
      'expiresAt',
      'modelAccess',
    });
    if (m['schemaVersion'] != 'human-place-declaration-controls-v1' ||
        m['modelAccess'] != 'UNAVAILABLE') {
      _invalid();
    }
    final observed = placeMemoryStamp(m['observedAt']),
        expiry = placeMemoryStamp(m['expiresAt']);
    if (!expiry.isAfter(observed) ||
        expiry.difference(observed) > const Duration(minutes: 2)) {
      _invalid();
    }
    final rows = m['declarations'];
    if (rows is! List || rows.length > 100) _invalid();
    final list = <PrivatePlaceDeclaration>[], seen = <String>{};
    for (final row in rows) {
      final d = PrivatePlaceDeclaration.read(row, observed, expiry);
      if (!seen.add(d.memoryID)) _invalid();
      list.add(d);
    }
    return PrivatePlaceDeclarationControls._(
      _id(m['ownerId']),
      _id(m['agentId']),
      _id(m['placeId']),
      List.unmodifiable(list),
      observed,
      expiry,
    );
  }
  PrivatePlaceDeclaration? declaration(String kind) {
    final list = declarations.where((d) => d.kind == kind).toList();
    if (list.length > 1) _invalid();
    return list.isEmpty ? null : list.single;
  }
}

class PlaceDeclarationReceipt {
  PlaceDeclarationReceipt._(
    this.ownerID,
    this.agentID,
    this.memoryID,
    this.version,
    this.status,
    this.placeID,
    this.kind,
    this.visibility,
    this.validUntil,
  );
  final String ownerID, agentID, memoryID, status;
  final int version;
  final String? placeID, kind, visibility;
  final DateTime? validUntil;
  factory PlaceDeclarationReceipt.read(dynamic raw) {
    final m = _object(
      raw,
      {'schemaVersion', 'ownerId', 'agentId', 'memoryId', 'version', 'status'},
      {'placeId', 'kind', 'basis', 'visibility', 'validUntil'},
    );
    if (m['schemaVersion'] != 'human-place-declaration-receipt-v1' ||
        !{'ACTIVE', 'DELETED'}.contains(m['status'])) {
      _invalid();
    }
    final active = m['status'] == 'ACTIVE';
    if (active) {
      if (!{'LIKED', 'VISITED'}.contains(m['kind']) ||
          m['basis'] != 'SELF_DECLARATION' ||
          !placeMemoryVisibilityLabels.containsKey(m['visibility'])) {
        _invalid();
      }
      _id(m['placeId']);
      placeMemoryStamp(m['validUntil']);
    } else if (m.keys.any(
      (k) =>
          {'placeId', 'kind', 'basis', 'visibility', 'validUntil'}.contains(k),
    )) {
      _invalid();
    }
    return PlaceDeclarationReceipt._(
      _id(m['ownerId']),
      _id(m['agentId']),
      _id(m['memoryId']),
      _version(m['version']),
      m['status'] as String,
      active ? m['placeId'] as String : null,
      active ? m['kind'] as String : null,
      active ? m['visibility'] as String : null,
      active ? placeMemoryStamp(m['validUntil']) : null,
    );
  }
}

class PlaceMemoryHTTP implements Exception {
  const PlaceMemoryHTTP(this.status);
  final int status;
}

class PrivatePlaceMemoryApi {
  PrivatePlaceMemoryApi({http.Client? client, String? apiBaseUrl})
    : _client = client ?? http.Client(),
      _owns = client == null,
      base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final http.Client _client;
  final bool _owns;
  final String base;
  Uri _uri(String path) =>
      Uri.parse('${base.replaceFirst(RegExp(r'/$'), '')}$path');
  Map<String, String> _headers(String token) => {
    'Authorization': token,
    'Content-Type': 'application/json',
  };
  dynamic _read(http.Response r) {
    if (r.statusCode != 200) throw PlaceMemoryHTTP(r.statusCode);
    final env = _object(jsonDecode(r.body), {'data'});
    return env['data'];
  }

  Future<PrivatePlaceMemory> read(String place, String token) async {
    if (!validPlaceMemoryID(place)) _invalid();
    return PrivatePlaceMemory.read(
      _read(
        await _client
            .get(_uri('/v1/me/places/$place/memory'), headers: _headers(token))
            .timeout(const Duration(seconds: 10)),
      ),
    );
  }

  Future<PrivatePlaceDeclarationControls> readControls(
    String place,
    String token,
  ) async {
    if (!validPlaceMemoryID(place)) _invalid();
    return PrivatePlaceDeclarationControls.read(
      _read(
        await _client
            .get(
              _uri('/v1/me/places/$place/declarations'),
              headers: _headers(token),
            )
            .timeout(const Duration(seconds: 10)),
      ),
    );
  }

  Future<PlaceDeclarationReceipt> put(
    String id,
    String place,
    String agent,
    int version,
    PlaceDeclarationDraft draft,
    String token,
  ) async {
    if (!validPlaceMemoryID(id) || !validPlaceMemoryID(place)) _invalid();
    return PlaceDeclarationReceipt.read(
      _read(
        await _client
            .put(
              _uri('/v1/me/place-declarations/$id'),
              headers: _headers(token),
              body: jsonEncode({
                ...draft.wire(place, version),
                'agentId': _id(agent),
              }),
            )
            .timeout(const Duration(seconds: 10)),
      ),
    );
  }

  Future<PlaceDeclarationReceipt> delete(
    String id,
    int version,
    String token,
  ) async {
    if (!validPlaceMemoryID(id)) _invalid();
    return PlaceDeclarationReceipt.read(
      _read(
        await _client
            .delete(
              _uri('/v1/me/place-declarations/$id'),
              headers: _headers(token),
              body: jsonEncode({'expectedVersion': version}),
            )
            .timeout(const Duration(seconds: 10)),
      ),
    );
  }

  void dispose() {
    if (_owns) _client.close();
  }
}

String newPlaceMemoryID() {
  final r = Random.secure();
  final b = List.generate(16, (_) => r.nextInt(256));
  b[6] = (b[6] & 15) | 64;
  b[8] = (b[8] & 63) | 128;
  final h = b.map((v) => v.toRadixString(16).padLeft(2, '0')).join();
  return '${h.substring(0, 8)}-${h.substring(8, 12)}-${h.substring(12, 16)}-${h.substring(16, 20)}-${h.substring(20)}';
}

String placeSessionFingerprint(String token) =>
    sha256.convert(utf8.encode(token)).toString();

/// This encrypted local recovery note is not a permission/effect ledger. A
/// native CAS receipt is the only success authority. No raw token is stored.
class PendingPlaceDeclaration {
  const PendingPlaceDeclaration({
    required this.memoryID,
    required this.agentID,
    required this.expectedVersion,
    required this.sessionFingerprint,
    required this.deleting,
    this.draft,
  });
  final String memoryID, agentID, sessionFingerprint;
  final int expectedVersion;
  final bool deleting;
  final PlaceDeclarationDraft? draft;
  Map<String, dynamic> wire() => {
    'memoryId': memoryID,
    'agentId': agentID,
    'expectedVersion': expectedVersion,
    'sessionFingerprint': sessionFingerprint,
    'deleting': deleting,
    if (draft != null)
      'draft': {
        'kind': draft!.kind,
        'visibility': draft!.visibility,
        'validUntil': draft!.validUntil.toUtc().toIso8601String(),
      },
  };
  factory PendingPlaceDeclaration.read(dynamic raw) {
    final m = _object(
      raw,
      {
        'memoryId',
        'agentId',
        'expectedVersion',
        'sessionFingerprint',
        'deleting',
      },
      {'draft'},
    );
    if (m['deleting'] is! bool ||
        m['sessionFingerprint'] is! String ||
        !RegExp(r'^[0-9a-f]{64}$').hasMatch(m['sessionFingerprint'])) {
      _invalid();
    }
    PlaceDeclarationDraft? draft;
    if (m['deleting'] == false) {
      final d = _object(m['draft'], {'kind', 'visibility', 'validUntil'});
      if (!{'LIKED', 'VISITED'}.contains(d['kind']) ||
          !placeMemoryVisibilityLabels.containsKey(d['visibility'])) {
        _invalid();
      }
      draft = PlaceDeclarationDraft(
        kind: d['kind'],
        visibility: d['visibility'],
        validUntil: placeMemoryStamp(d['validUntil']),
      );
    } else if (m.containsKey('draft')) {
      _invalid();
    }
    return PendingPlaceDeclaration(
      memoryID: _id(m['memoryId']),
      agentID: _id(m['agentId']),
      expectedVersion: _version(m['expectedVersion'], zero: true),
      sessionFingerprint: m['sessionFingerprint'],
      deleting: m['deleting'],
      draft: draft,
    );
  }
}

abstract class PlaceDeclarationPendingStore {
  Future<PendingPlaceDeclaration?> read(
    String environment,
    String owner,
    String place,
  );
  Future<void> write(
    String environment,
    String owner,
    String place,
    PendingPlaceDeclaration pending,
  );
  Future<void> delete(String environment, String owner, String place);
}

class SecurePlaceDeclarationPendingStore
    implements PlaceDeclarationPendingStore {
  const SecurePlaceDeclarationPendingStore();
  static const _storage = FlutterSecureStorage();
  String _key(String env, String owner, String place) =>
      'birdtie.place-declaration.v1.${sha256.convert(utf8.encode('$env|$owner|$place'))}';
  @override
  Future<PendingPlaceDeclaration?> read(
    String env,
    String owner,
    String place,
  ) async {
    final raw = await _storage.read(key: _key(env, owner, place));
    return raw == null ? null : PendingPlaceDeclaration.read(jsonDecode(raw));
  }

  @override
  Future<void> write(
    String env,
    String owner,
    String place,
    PendingPlaceDeclaration p,
  ) =>
      _storage.write(key: _key(env, owner, place), value: jsonEncode(p.wire()));
  @override
  Future<void> delete(String env, String owner, String place) =>
      _storage.delete(key: _key(env, owner, place));
}
