import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'organization_human_control.dart';

bool organizationWGS84(Object? lat, Object? lon) =>
    lat is num &&
    lon is num &&
    lat.isFinite &&
    lon.isFinite &&
    lat.abs() <= 90 &&
    lon.abs() <= 180;

class OrganizationCanonicalPoint {
  const OrganizationCanonicalPoint({
    required this.organizationID,
    required this.cityID,
    required this.latitude,
    required this.longitude,
    required this.visibility,
    required this.reviewStatus,
    required this.revision,
  });
  final String organizationID, cityID, visibility, reviewStatus;
  final double latitude, longitude;
  final int revision;
  factory OrganizationCanonicalPoint.fromJson(Object? raw, String org) {
    final m = organizationControlObject(raw, const {
      'organizationId',
      'cityId',
      'latitude',
      'longitude',
      'visibility',
      'reviewStatus',
      'revision',
    });
    if (m['organizationId'] != org ||
        !organizationControlText(m['cityId'], 160, empty: false) ||
        !organizationWGS84(m['latitude'], m['longitude']) ||
        !const {'public', 'hidden'}.contains(m['visibility']) ||
        !const {
          'pending',
          'approved',
          'rejected',
        }.contains(m['reviewStatus']) ||
        !organizationControlVersion(m['revision'], zero: false))
      throw const FormatException('组织公开位置资料不符');
    return OrganizationCanonicalPoint(
      organizationID: org,
      cityID: m['cityId'],
      latitude: (m['latitude'] as num).toDouble(),
      longitude: (m['longitude'] as num).toDouble(),
      visibility: m['visibility'],
      reviewStatus: m['reviewStatus'],
      revision: m['revision'],
    );
  }
}

class OrganizationLocationProposal {
  const OrganizationLocationProposal({
    required this.id,
    required this.organizationID,
    required this.cityID,
    required this.latitude,
    required this.longitude,
    required this.status,
    required this.version,
    required this.canonicalRevision,
    required this.submittedBy,
    required this.canApprove,
    required this.canReject,
    required this.canWithdraw,
  });
  final String id, organizationID, cityID, status, submittedBy;
  final double latitude, longitude;
  final int version, canonicalRevision;
  final bool canApprove, canReject, canWithdraw;
  factory OrganizationLocationProposal.fromJson(Object? raw, String org) {
    final m = organizationControlObject(raw, const {
      'id',
      'organizationId',
      'cityId',
      'latitude',
      'longitude',
      'status',
      'version',
      'canonicalRevision',
      'submittedBy',
      'canApprove',
      'canReject',
      'canWithdraw',
    });
    if (!organizationControlID(m['id']) ||
        m['organizationId'] != org ||
        !organizationControlID(m['submittedBy']) ||
        !organizationControlText(m['cityId'], 160, empty: false) ||
        !organizationWGS84(m['latitude'], m['longitude']) ||
        !const {
          'PENDING',
          'APPROVED',
          'REJECTED',
          'WITHDRAWN',
        }.contains(m['status']) ||
        !organizationControlVersion(m['version'], zero: false) ||
        !organizationControlVersion(m['canonicalRevision']) ||
        m['canApprove'] is! bool ||
        m['canReject'] is! bool ||
        m['canWithdraw'] is! bool ||
        (m['status'] != 'PENDING' ||
                m['version'] == organizationMaxControlVersion) &&
            (m['canApprove'] == true ||
                m['canReject'] == true ||
                m['canWithdraw'] == true)) {
      throw const FormatException('组织位置提案或权限资料不符');
    }
    return OrganizationLocationProposal(
      id: m['id'],
      organizationID: org,
      cityID: m['cityId'],
      latitude: (m['latitude'] as num).toDouble(),
      longitude: (m['longitude'] as num).toDouble(),
      status: m['status'],
      version: m['version'],
      canonicalRevision: m['canonicalRevision'],
      submittedBy: m['submittedBy'],
      canApprove: m['canApprove'],
      canReject: m['canReject'],
      canWithdraw: m['canWithdraw'],
    );
  }
}

class OrganizationMapControl {
  const OrganizationMapControl({
    required this.organizationID,
    required this.authorityVersion,
    required this.role,
    required this.canonical,
    required this.proposals,
    required this.observedAt,
    required this.expiresAt,
  });
  final String organizationID, role;
  final int authorityVersion;
  final OrganizationCanonicalPoint? canonical;
  final List<OrganizationLocationProposal> proposals;
  final DateTime observedAt, expiresAt;
  int get canonicalRevision => canonical?.revision ?? 0;
  bool get current => expiresAt.isAfter(DateTime.now().toUtc());
  bool get canManage =>
      current &&
      const {'owner', 'admin'}.contains(role) &&
      authorityVersion < organizationMaxControlVersion;
  factory OrganizationMapControl.fromJson(Object? raw, String org) {
    final m = organizationControlObject(raw, const {
      'organizationId',
      'authorityVersion',
      'role',
      'canonical',
      'proposals',
      'observedAt',
      'expiresAt',
    });
    final observed = organizationControlTime(m['observedAt']),
        expires = organizationControlTime(m['expiresAt']);
    organizationControlLease(observed, expires);
    if (m['organizationId'] != org ||
        !organizationControlID(org) ||
        !organizationControlVersion(m['authorityVersion'], zero: false) ||
        !const {
          'owner',
          'admin',
          'moderator',
          'member',
          'reviewer',
        }.contains(m['role']) ||
        m['proposals'] is! List ||
        (m['proposals'] as List).length > 20)
      throw const FormatException('组织位置来源不符');
    final proposals = (m['proposals'] as List)
        .map((p) => OrganizationLocationProposal.fromJson(p, org))
        .toList(growable: false);
    if (proposals.map((p) => p.id).toSet().length != proposals.length)
      throw const FormatException('组织位置提案重复');
    return OrganizationMapControl(
      organizationID: org,
      authorityVersion: m['authorityVersion'],
      role: m['role'],
      canonical: m['canonical'] == null
          ? null
          : OrganizationCanonicalPoint.fromJson(m['canonical'], org),
      proposals: proposals,
      observedAt: observed,
      expiresAt: expires,
    );
  }
}

class OrganizationMapLocationApi {
  OrganizationMapLocationApi({
    required this.authorizationHeader,
    http.Client? client,
    String? apiBaseUrl,
    bool Function()? current,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       baseUrl = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl,
       current = current;
  final http.Client _client;
  final bool _ownsClient;
  final String baseUrl;
  final String? Function() authorizationHeader;
  final bool Function()? current;
  http.Client get client => _client;
  OrganizationHumanControlApi get controlApi => OrganizationHumanControlApi(
    client: _client,
    baseUrl: baseUrl,
    authorizationHeader: authorizationHeader,
    current: current,
  );
  Future<OrganizationMapControl> control(
    String org, {
    bool Function()? guard,
  }) async {
    final api = guard == null ? controlApi : controlApi.scoped(guard);
    return OrganizationMapControl.fromJson(
      await api.json('GET', '${api.path(org)}/map-location/control'),
      org,
    );
  }

  void dispose() {
    if (_ownsClient) _client.close();
  }
}
