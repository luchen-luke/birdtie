import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'organization_human_control.dart';

const organizationMemberRoles = {'owner', 'admin', 'moderator', 'member'};

class OrganizationMember {
  const OrganizationMember({
    required this.id,
    required this.organizationID,
    required this.userAccountID,
    required this.role,
    required this.status,
    required this.version,
    required this.authorityVersion,
    this.displayName = '',
    this.organizationName = '',
  });
  final String id,
      organizationID,
      userAccountID,
      displayName,
      role,
      status,
      organizationName;
  final int version, authorityVersion;
  factory OrganizationMember.fromJson(Map<String, dynamic> raw) {
    final m = organizationControlObject(
      raw,
      const {
        'id',
        'organizationId',
        'userAccountId',
        'role',
        'status',
        'version',
        'authorityVersion',
      },
      optional: const {'displayName', 'organizationName'},
    );
    if (!organizationControlID(m['id']) ||
        !organizationControlID(m['organizationId']) ||
        !organizationControlID(m['userAccountId']) ||
        !organizationMemberRoles.contains(m['role']) ||
        !const {'invited', 'active', 'removed'}.contains(m['status']) ||
        !organizationControlVersion(m['version'], zero: false) ||
        !organizationControlVersion(m['authorityVersion'], zero: false) ||
        m.containsKey('displayName') &&
            !organizationControlText(m['displayName'], 80, empty: false) ||
        m.containsKey('organizationName') &&
            !organizationControlText(
              m['organizationName'],
              200,
              empty: false,
            )) {
      throw const FormatException('组织成员资料或版本不符');
    }
    return OrganizationMember(
      id: m['id'],
      organizationID: m['organizationId'],
      userAccountID: m['userAccountId'],
      displayName: m['displayName'] ?? '',
      role: m['role'],
      status: m['status'],
      version: m['version'],
      authorityVersion: m['authorityVersion'],
      organizationName: m['organizationName'] ?? '',
    );
  }
}

class OrganizationMembersControl {
  const OrganizationMembersControl({
    required this.organizationID,
    required this.authorityVersion,
    required this.role,
    required this.members,
    required this.observedAt,
    required this.expiresAt,
  });
  final String organizationID, role;
  final int authorityVersion;
  final List<OrganizationMember> members;
  final DateTime observedAt, expiresAt;
  bool get current => expiresAt.isAfter(DateTime.now().toUtc());
  bool get canManage =>
      current &&
      const {'owner', 'admin'}.contains(role) &&
      authorityVersion < organizationMaxControlVersion;
  bool canChange(OrganizationMember m) =>
      canManage &&
      m.version < organizationMaxControlVersion &&
      m.status != 'removed' &&
      (role == 'owner' || m.role != 'owner' && m.role != 'admin');
  factory OrganizationMembersControl.fromJson(Object? raw, String org) {
    final m = organizationControlObject(raw, const {
      'organizationId',
      'authorityVersion',
      'role',
      'members',
      'observedAt',
      'expiresAt',
    });
    final observed = organizationControlTime(m['observedAt']),
        expires = organizationControlTime(m['expiresAt']);
    organizationControlLease(observed, expires);
    if (m['organizationId'] != org ||
        !organizationControlID(org) ||
        !organizationControlVersion(m['authorityVersion'], zero: false) ||
        !organizationMemberRoles.contains(m['role']) ||
        m['members'] is! List ||
        (m['members'] as List).length > 2000) {
      throw const FormatException('组织成员来源不符');
    }
    final members = (m['members'] as List)
        .map((e) {
          if (e is! Map<String, dynamic>)
            throw const FormatException('组织成员资料不符');
          return OrganizationMember.fromJson(e);
        })
        .toList(growable: false);
    if (members.any(
          (e) =>
              e.organizationID != org ||
              e.authorityVersion != m['authorityVersion'],
        ) ||
        members.map((e) => e.id).toSet().length != members.length ||
        members.map((e) => e.userAccountID).toSet().length != members.length)
      throw const FormatException('组织成员目标或版本重复');
    return OrganizationMembersControl(
      organizationID: org,
      authorityVersion: m['authorityVersion'],
      role: m['role'],
      members: members,
      observedAt: observed,
      expiresAt: expires,
    );
  }
}

class OrganizationInvitePerson {
  const OrganizationInvitePerson(this.accountID, this.displayName);
  final String accountID, displayName;
  factory OrganizationInvitePerson.fromJson(Object? raw) {
    final m = organizationControlObject(raw, const {
      'accountId',
      'displayName',
    });
    if (!organizationControlID(m['accountId']) ||
        !organizationControlText(m['displayName'], 80, empty: false)) {
      throw const FormatException('获准的人员搜索资料不符');
    }
    return OrganizationInvitePerson(m['accountId'], m['displayName']);
  }
}

class OrganizationMembershipException implements Exception {
  const OrganizationMembershipException(this.status);
  final int status;
  String get message => OrganizationControlFailure(status).message;
}

class OrganizationMembershipApi {
  OrganizationMembershipApi({
    required this.authorizationHeader,
    http.Client? client,
    String? apiBaseUrl,
    bool Function()? current,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       baseUrl = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl,
       current = current;
  final String? Function() authorizationHeader;
  final http.Client _client;
  final bool _ownsClient;
  final String baseUrl;
  final bool Function()? current;
  http.Client get client => _client;
  OrganizationHumanControlApi get controlApi => OrganizationHumanControlApi(
    client: _client,
    baseUrl: baseUrl,
    authorizationHeader: authorizationHeader,
    current: current,
  );
  void dispose() {
    if (_ownsClient) _client.close();
  }

  Future<OrganizationMembersControl> membersControl(
    String org, {
    bool Function()? guard,
  }) async {
    final api = guard == null ? controlApi : controlApi.scoped(guard);
    return OrganizationMembersControl.fromJson(
      await api.json('GET', '${api.path(org)}/members/control'),
      org,
    );
  }

  Future<List<OrganizationMember>> list(String org) async =>
      (await membersControl(org)).members;
  Future<List<OrganizationMember>> invitations({bool Function()? guard}) async {
    final api = guard == null ? controlApi : controlApi.scoped(guard);
    final data = await api.json('GET', '/v1/me/organization-invitations');
    if (data is! List || data.length > 2000)
      throw const FormatException('组织邀请资料不符');
    final result = data
        .map((e) {
          if (e is! Map<String, dynamic>)
            throw const FormatException('组织邀请资料不符');
          return OrganizationMember.fromJson(e);
        })
        .toList(growable: false);
    if (result.any((m) => m.status != 'invited') ||
        result.map((m) => m.id).toSet().length != result.length) {
      throw const FormatException('邀请目标或状态不符');
    }
    return result;
  }

  Future<String> personalOwner({bool Function()? guard}) async {
    final api = guard == null ? controlApi : controlApi.scoped(guard);
    final data = await api.json('GET', '/v1/me');
    if (data is! Map<String, dynamic> || !organizationControlID(data['id']))
      throw const FormatException('当前个人账号不符');
    return data['id'];
  }

  Future<List<OrganizationInvitePerson>> searchPeople(
    String query, {
    bool Function()? guard,
  }) async {
    final q = query.trim();
    if (q.runes.length < 2 || !organizationControlText(q, 80, empty: false))
      throw const FormatException('请输入至少两个字，且不超过80字节的姓名。');
    final api = guard == null ? controlApi : controlApi.scoped(guard);
    final data = await api.json('GET', '/v1/people/search', query: {'q': q});
    if (data is! List || data.length > 20)
      throw const FormatException('人员搜索结果不符');
    final list = data
        .map(OrganizationInvitePerson.fromJson)
        .toList(growable: false);
    if (list.map((p) => p.accountID).toSet().length != list.length)
      throw const FormatException('人员搜索目标重复');
    return list;
  }

  // Old callers cannot bypass the explicit original preview and approval.
  Future<OrganizationMember> invite(String org, String id, String role) async =>
      throw const OrganizationMembershipException(409);
  Future<OrganizationMember> accept(String membershipID) async =>
      throw const OrganizationMembershipException(409);
  Future<OrganizationMember> changeRole(
    String org,
    String id,
    String role,
  ) async => throw const OrganizationMembershipException(409);
  Future<void> revoke(String org, String id) async =>
      throw const OrganizationMembershipException(409);
}
