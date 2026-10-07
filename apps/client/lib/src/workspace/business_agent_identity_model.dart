import 'business_api.dart';

class BusinessAgentIdentity {
  BusinessAgentIdentity._(
    this.businessID,
    this.name,
    this.principalID,
    this.role,
    this.claimStatus,
    this.claimState,
    this.claimVersion,
    this.agentID,
    this.agentStatus,
    this.profileVersion,
    this.observedAt,
    this.validUntil,
  );
  final String businessID, name, principalID, role, claimStatus, claimState;
  final String? agentID, agentStatus;
  final int claimVersion;
  final int? profileVersion;
  final DateTime observedAt, validUntil;
  bool eligible(DateTime now) =>
      agentID == null &&
      claimVersion > 0 &&
      claimStatus == 'verified' &&
      claimState == 'verified' &&
      validUntil.isAfter(now);

  static void _keys(Map<String, dynamic> m, Set<String> fields) {
    if (m.keys.toSet().difference(fields).isNotEmpty ||
        m.length != fields.length) {
      throw const BusinessApiException(503);
    }
  }

  static DateTime _time(dynamic v) {
    if (v is! String || !v.endsWith('Z')) throw const BusinessApiException(503);
    final t = DateTime.tryParse(v);
    if (t == null || t.year < 1 || t.year > 9999) {
      throw const BusinessApiException(503);
    }
    return t.toUtc();
  }

  factory BusinessAgentIdentity.read(
    dynamic raw, {
    required String businessID,
    required DateTime now,
  }) {
    if (raw is! Map<String, dynamic>) throw const BusinessApiException(503);
    _keys(raw, {
      'schema',
      'businessId',
      'businessName',
      'principal',
      'role',
      'claimStatus',
      'claimState',
      'claimVersion',
      'agent',
      'observedAt',
      'validUntil',
      'metadataOnly',
      'runtimeAvailable',
      'tools',
    });
    final p = raw['principal'];
    final version = raw['claimVersion'];
    const states = {'pending', 'verified', 'rejected', 'revoked'};
    if (raw['schema'] != 'business-agent-identity-v1' ||
        raw['businessId'] != businessID ||
        !BusinessApi.validID(businessID) ||
        raw['businessName'] is! String ||
        (raw['businessName'] as String).trim().isEmpty ||
        p is! Map<String, dynamic> ||
        p['type'] != 'BUSINESS' ||
        p['id'] is! String ||
        !BusinessApi.validID(p['id']) ||
        !const {'owner', 'admin'}.contains(raw['role']) ||
        !states.contains(raw['claimStatus']) ||
        !states.contains(raw['claimState']) ||
        version is! int ||
        version < 0 ||
        raw['metadataOnly'] != true ||
        raw['runtimeAvailable'] != false ||
        raw['tools'] is! List ||
        (raw['tools'] as List).isNotEmpty) {
      throw const BusinessApiException(503);
    }
    _keys(p, {'type', 'id'});
    final at = _time(raw['observedAt']), until = _time(raw['validUntil']);
    if (!until.isAfter(now) ||
        !until.isAfter(at) ||
        until.difference(at) > const Duration(seconds: 30) ||
        at.isAfter(now.add(const Duration(seconds: 5)))) {
      throw const BusinessApiException(409);
    }
    String? agentID, status;
    int? profileVersion;
    final agent = raw['agent'];
    if (agent != null) {
      if (agent is! Map<String, dynamic>) throw const BusinessApiException(503);
      _keys(agent, {'id', 'type', 'status', 'profile'});
      final profile = agent['profile'];
      if (agent['id'] is! String ||
          !BusinessApi.validID(agent['id']) ||
          agent['type'] != 'BUSINESS' ||
          !const {'suspended', 'retired'}.contains(agent['status']) ||
          profile is! Map<String, dynamic>) {
        throw const BusinessApiException(503);
      }
      _keys(profile, {
        'agentId',
        'ownerType',
        'ownerId',
        'profileVersion',
        'createdAt',
        'updatedAt',
      });
      final pv = profile['profileVersion'];
      final created = _time(profile['createdAt']),
          updated = _time(profile['updatedAt']);
      if (profile['agentId'] != agent['id'] ||
          profile['ownerType'] != 'BUSINESS' ||
          profile['ownerId'] != p['id'] ||
          pv is! int ||
          pv < 1 ||
          updated.isBefore(created) ||
          updated.isAfter(at)) {
        throw const BusinessApiException(503);
      }
      agentID = agent['id'];
      status = agent['status'];
      profileVersion = pv;
    }
    return BusinessAgentIdentity._(
      businessID,
      raw['businessName'],
      p['id'],
      raw['role'],
      raw['claimStatus'],
      raw['claimState'],
      version,
      agentID,
      status,
      profileVersion,
      at,
      until,
    );
  }
}
