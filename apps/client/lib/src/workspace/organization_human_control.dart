import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;

const organizationMaxControlVersion = 9007199254740991;
bool organizationControlID(Object? s) =>
    s is String &&
    RegExp(
      r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
    ).hasMatch(s);
bool organizationControlVersion(Object? n, {bool zero = true}) =>
    n is int && n >= (zero ? 0 : 1) && n <= organizationMaxControlVersion;
bool organizationControlText(Object? s, int bytes, {bool empty = true}) =>
    s is String &&
    (empty || s.isNotEmpty) &&
    s == s.trim() &&
    utf8.encode(s).length <= bytes &&
    !RegExp(r'[\x00-\x1f\x7f-\x9f]').hasMatch(s);
Map<String, dynamic> organizationControlObject(
  Object? raw,
  Set<String> required, {
  Set<String> optional = const {},
}) {
  if (raw is! Map<String, dynamic> ||
      !required.every(raw.containsKey) ||
      raw.keys.any((k) => !required.contains(k) && !optional.contains(k))) {
    throw const FormatException('组织操作资料结构不符');
  }
  return raw;
}

DateTime organizationControlTime(Object? raw) {
  if (raw is! String || !RegExp(r'Z$|[+-]\d\d:\d\d$').hasMatch(raw)) {
    throw const FormatException('组织资料期限不符');
  }
  final t = DateTime.tryParse(raw)?.toUtc();
  if (t == null) throw const FormatException('组织资料期限不符');
  return t;
}

void organizationControlLease(DateTime observed, DateTime expires) {
  final now = DateTime.now().toUtc();
  if (observed.isAfter(now.add(const Duration(seconds: 2))) ||
      !expires.isAfter(now) ||
      !expires.isAfter(observed) ||
      expires.isAfter(observed.add(const Duration(seconds: 31))) ||
      expires.isAfter(now.add(const Duration(seconds: 31)))) {
    throw const FormatException('组织资料已到期或来源不符');
  }
}

String _floatHex(double number) {
  final data = ByteData(8)..setFloat64(0, number == 0 ? 0 : number, Endian.big);
  return data.buffer
      .asUint8List()
      .map((v) => v.toRadixString(16).padLeft(2, '0'))
      .join();
}

String _goJSON(Object value) => jsonEncode(value)
    .replaceAll('<', r'\u003c')
    .replaceAll('>', r'\u003e')
    .replaceAll('&', r'\u0026')
    .replaceAll('\u2028', r'\u2028')
    .replaceAll('\u2029', r'\u2029');

/// Closed original command. Names never substitute for target identifiers.
class OrganizationControlInput {
  const OrganizationControlInput({
    required this.action,
    required this.operationID,
    required this.expectedAuthorityVersion,
    this.userAccountID = '',
    this.membershipID = '',
    this.role = '',
    this.expectedMemberVersion = 0,
    this.cityID = '',
    this.latitude = 0,
    this.longitude = 0,
    this.proposalID = '',
    this.expectedProposalVersion = 0,
    this.expectedCanonicalRevision = 0,
  });
  final String action,
      operationID,
      userAccountID,
      membershipID,
      role,
      cityID,
      proposalID;
  final int expectedAuthorityVersion,
      expectedMemberVersion,
      expectedProposalVersion,
      expectedCanonicalRevision;
  final double latitude, longitude;
  bool get memberAction =>
      const {'INVITE', 'ACCEPT', 'ROLE', 'REVOKE'}.contains(action);
  bool get valid {
    if (!organizationControlID(operationID) ||
        !organizationControlVersion(expectedAuthorityVersion, zero: false) ||
        expectedAuthorityVersion >= organizationMaxControlVersion ||
        !organizationControlVersion(expectedMemberVersion) ||
        !organizationControlVersion(expectedProposalVersion) ||
        !organizationControlVersion(expectedCanonicalRevision))
      return false;
    if (memberAction) {
      if (cityID.isNotEmpty ||
          latitude != 0 ||
          longitude != 0 ||
          proposalID.isNotEmpty ||
          expectedProposalVersion != 0 ||
          expectedCanonicalRevision != 0 ||
          expectedMemberVersion >= organizationMaxControlVersion)
        return false;
      if (action == 'INVITE')
        return organizationControlID(userAccountID) &&
            membershipID.isEmpty &&
            const {'member', 'moderator', 'admin'}.contains(role);
      return organizationControlID(membershipID) &&
          expectedMemberVersion > 0 &&
          userAccountID.isEmpty &&
          (action == 'ROLE'
              ? const {'member', 'moderator', 'admin', 'owner'}.contains(role)
              : role.isEmpty);
    }
    if (userAccountID.isNotEmpty ||
        membershipID.isNotEmpty ||
        role.isNotEmpty ||
        expectedMemberVersion != 0)
      return false;
    switch (action) {
      case 'PROPOSE':
        return organizationControlText(cityID, 160, empty: false) &&
            latitude.isFinite &&
            longitude.isFinite &&
            latitude.abs() <= 90 &&
            longitude.abs() <= 180 &&
            proposalID.isEmpty &&
            expectedProposalVersion == 0 &&
            expectedCanonicalRevision < organizationMaxControlVersion;
      case 'APPROVE_POINT':
      case 'REJECT_POINT':
      case 'WITHDRAW_POINT':
        return organizationControlID(proposalID) &&
            expectedProposalVersion > 0 &&
            expectedProposalVersion < organizationMaxControlVersion &&
            cityID.isEmpty &&
            latitude == 0 &&
            longitude == 0 &&
            (action == 'WITHDRAW_POINT'
                ? expectedCanonicalRevision == 0
                : expectedCanonicalRevision < organizationMaxControlVersion);
      case 'HIDE_POINT':
        return expectedCanonicalRevision > 0 &&
            expectedCanonicalRevision < organizationMaxControlVersion &&
            proposalID.isEmpty &&
            expectedProposalVersion == 0 &&
            cityID.isEmpty &&
            latitude == 0 &&
            longitude == 0;
      default:
        return false;
    }
  }

  Map<String, Object> toJson({String? approval}) {
    if (!valid) throw const FormatException('组织操作输入或版本不符');
    return {
      'action': action,
      'operationId': operationID,
      'expectedAuthorityVersion': expectedAuthorityVersion,
      if (action == 'INVITE') ...{
        'userAccountId': userAccountID,
        'role': role,
        'expectedMemberVersion': expectedMemberVersion,
      },
      if (const {'ACCEPT', 'ROLE', 'REVOKE'}.contains(action)) ...{
        'membershipId': membershipID,
        'expectedMemberVersion': expectedMemberVersion,
        if (action == 'ROLE') 'role': role,
      },
      if (action == 'PROPOSE') ...{
        'cityId': cityID,
        'latitude': latitude == 0 ? 0.0 : latitude,
        'longitude': longitude == 0 ? 0.0 : longitude,
        'expectedCanonicalRevision': expectedCanonicalRevision,
      },
      if (const {
        'APPROVE_POINT',
        'REJECT_POINT',
        'WITHDRAW_POINT',
      }.contains(action)) ...{
        'proposalId': proposalID,
        'expectedProposalVersion': expectedProposalVersion,
        if (action != 'WITHDRAW_POINT')
          'expectedCanonicalRevision': expectedCanonicalRevision,
      },
      if (action == 'HIDE_POINT')
        'expectedCanonicalRevision': expectedCanonicalRevision,
      if (approval != null) 'approval': approval,
    };
  }

  String digest(String org) => sha256
      .convert(
        utf8.encode(
          _goJSON([
            'ORG_HUMAN_139',
            org,
            action,
            operationID,
            expectedAuthorityVersion,
            userAccountID,
            membershipID,
            role,
            expectedMemberVersion,
            cityID,
            _floatHex(latitude),
            _floatHex(longitude),
            proposalID,
            expectedProposalVersion,
            expectedCanonicalRevision,
          ]),
        ),
      )
      .toString();
  OrganizationControlSelector selector(
    String organizationID, {
    String invitedMembershipID = '',
  }) => OrganizationControlSelector(
    organizationID: organizationID,
    operationID: operationID,
    action: action,
    expectedAuthorityVersion: expectedAuthorityVersion,
    membershipID: action == 'INVITE' ? invitedMembershipID : membershipID,
    expectedMemberVersion: expectedMemberVersion,
    proposalID: proposalID,
    expectedProposalVersion: expectedProposalVersion,
    expectedCanonicalRevision: expectedCanonicalRevision,
  );
}

/// Durable recovery contains only this original selector, never draft body.
class OrganizationControlSelector {
  const OrganizationControlSelector({
    required this.organizationID,
    required this.operationID,
    required this.action,
    required this.expectedAuthorityVersion,
    this.membershipID = '',
    this.expectedMemberVersion = 0,
    this.proposalID = '',
    this.expectedProposalVersion = 0,
    this.expectedCanonicalRevision = 0,
  });
  final String organizationID, operationID, action, membershipID, proposalID;
  final int expectedAuthorityVersion,
      expectedMemberVersion,
      expectedProposalVersion,
      expectedCanonicalRevision;
  bool get memberAction =>
      const {'INVITE', 'ACCEPT', 'ROLE', 'REVOKE'}.contains(action);
  bool get valid =>
      organizationControlID(organizationID) &&
      organizationControlID(operationID) &&
      organizationControlVersion(expectedAuthorityVersion, zero: false) &&
      expectedAuthorityVersion < organizationMaxControlVersion &&
      organizationControlVersion(expectedMemberVersion) &&
      organizationControlVersion(expectedProposalVersion) &&
      organizationControlVersion(expectedCanonicalRevision) &&
      (memberAction
          ? proposalID.isEmpty &&
                expectedProposalVersion == 0 &&
                expectedCanonicalRevision == 0 &&
                expectedMemberVersion < organizationMaxControlVersion &&
                (action == 'INVITE'
                    ? expectedMemberVersion == 0 && membershipID.isEmpty ||
                          expectedMemberVersion > 0 &&
                              organizationControlID(membershipID)
                    : expectedMemberVersion > 0 &&
                          organizationControlID(membershipID))
          : const {
                  'PROPOSE',
                  'APPROVE_POINT',
                  'REJECT_POINT',
                  'WITHDRAW_POINT',
                  'HIDE_POINT',
                }.contains(action) &&
                membershipID.isEmpty &&
                expectedMemberVersion == 0 &&
                (const {'PROPOSE', 'HIDE_POINT'}.contains(action)
                    ? proposalID.isEmpty &&
                          expectedProposalVersion == 0 &&
                          expectedCanonicalRevision <
                              organizationMaxControlVersion &&
                          (action != 'HIDE_POINT' ||
                              expectedCanonicalRevision > 0)
                    : organizationControlID(proposalID) &&
                          expectedProposalVersion > 0 &&
                          expectedProposalVersion <
                              organizationMaxControlVersion &&
                          (action == 'WITHDRAW_POINT'
                              ? expectedCanonicalRevision == 0
                              : expectedCanonicalRevision <
                                    organizationMaxControlVersion)));
  Map<String, Object> toJson() => {
    'organizationId': organizationID,
    'operationId': operationID,
    'action': action,
    'expectedAuthorityVersion': expectedAuthorityVersion,
    'membershipId': membershipID,
    'expectedMemberVersion': expectedMemberVersion,
    'proposalId': proposalID,
    'expectedProposalVersion': expectedProposalVersion,
    'expectedCanonicalRevision': expectedCanonicalRevision,
  };
  bool same(OrganizationControlSelector o) =>
      organizationID == o.organizationID &&
      operationID == o.operationID &&
      action == o.action &&
      expectedAuthorityVersion == o.expectedAuthorityVersion &&
      membershipID == o.membershipID &&
      expectedMemberVersion == o.expectedMemberVersion &&
      proposalID == o.proposalID &&
      expectedProposalVersion == o.expectedProposalVersion &&
      expectedCanonicalRevision == o.expectedCanonicalRevision;
  static OrganizationControlSelector fromJson(Object? raw) {
    final m = organizationControlObject(raw, const {
      'organizationId',
      'operationId',
      'action',
      'expectedAuthorityVersion',
      'membershipId',
      'expectedMemberVersion',
      'proposalId',
      'expectedProposalVersion',
      'expectedCanonicalRevision',
    });
    for (final k in [
      'organizationId',
      'operationId',
      'action',
      'membershipId',
      'proposalId',
    ]) {
      if (m[k] is! String) throw const FormatException('原组织操作标识不符');
    }
    for (final k in [
      'expectedAuthorityVersion',
      'expectedMemberVersion',
      'expectedProposalVersion',
      'expectedCanonicalRevision',
    ]) {
      if (m[k] is! int) throw const FormatException('原组织操作版本不符');
    }
    final s = OrganizationControlSelector(
      organizationID: m['organizationId'],
      operationID: m['operationId'],
      action: m['action'],
      expectedAuthorityVersion: m['expectedAuthorityVersion'],
      membershipID: m['membershipId'],
      expectedMemberVersion: m['expectedMemberVersion'],
      proposalID: m['proposalId'],
      expectedProposalVersion: m['expectedProposalVersion'],
      expectedCanonicalRevision: m['expectedCanonicalRevision'],
    );
    if (!s.valid) throw const FormatException('原组织操作标识不符');
    return s;
  }
}

class OrganizationControlPreview {
  const OrganizationControlPreview(
    this.organizationID,
    this.input,
    this.approval,
    this.expiresAt,
    this.canonicalRevision,
  );
  final String organizationID, approval;
  final OrganizationControlInput input;
  final DateTime expiresAt;
  final int canonicalRevision;
  static OrganizationControlPreview fromJson(
    Object? raw,
    String org,
    OrganizationControlInput input,
  ) {
    final m = organizationControlObject(raw, const {
      'organizationId',
      'operationId',
      'action',
      'approval',
      'expiresAt',
      'inputSha256',
      'authorityVersion',
      'memberVersion',
      'canonicalRevision',
      'proposalVersion',
    });
    final expires = organizationControlTime(m['expiresAt']),
        now = DateTime.now().toUtc();
    if (!input.valid ||
        m['organizationId'] != org ||
        m['operationId'] != input.operationID ||
        m['action'] != input.action ||
        m['inputSha256'] != input.digest(org) ||
        !organizationControlVersion(m['authorityVersion'], zero: false) ||
        !organizationControlVersion(m['memberVersion']) ||
        !organizationControlVersion(m['proposalVersion']) ||
        m['authorityVersion'] != input.expectedAuthorityVersion ||
        m['memberVersion'] != input.expectedMemberVersion ||
        m['proposalVersion'] != input.expectedProposalVersion ||
        !organizationControlVersion(m['canonicalRevision']) ||
        (const {
              'PROPOSE',
              'APPROVE_POINT',
              'REJECT_POINT',
              'HIDE_POINT',
            }.contains(input.action) &&
            m['canonicalRevision'] != input.expectedCanonicalRevision) ||
        m['approval'] is! String ||
        !RegExp(r'^oh1\.\d+\.\d+\.[0-9a-f]{64}$').hasMatch(m['approval']) ||
        !expires.isAfter(now) ||
        expires.isAfter(now.add(const Duration(seconds: 31)))) {
      throw const FormatException('组织确认与原输入、版本或来源不符');
    }
    return OrganizationControlPreview(
      org,
      input,
      m['approval'],
      expires,
      m['canonicalRevision'],
    );
  }
}

class OrganizationControlReceipt {
  const OrganizationControlReceipt({
    required this.selector,
    required this.status,
    required this.authorityVersion,
    required this.memberVersion,
    required this.canonicalRevision,
    required this.proposalVersion,
    this.membershipID = '',
    this.proposalID = '',
  });
  final OrganizationControlSelector selector;
  final String status, membershipID, proposalID;
  final int authorityVersion, memberVersion, canonicalRevision, proposalVersion;
  static OrganizationControlReceipt fromJson(
    Object? raw,
    OrganizationControlSelector s,
  ) {
    final m = organizationControlObject(
      raw,
      const {
        'organizationId',
        'operationId',
        'action',
        'status',
        'authorityVersion',
        'memberVersion',
        'canonicalRevision',
        'proposalVersion',
      },
      optional: const {'membershipId', 'proposalId'},
    );
    if (!s.valid ||
        m['organizationId'] != s.organizationID ||
        m['operationId'] != s.operationID ||
        !organizationControlVersion(m['authorityVersion']) ||
        !organizationControlVersion(m['memberVersion']) ||
        !organizationControlVersion(m['canonicalRevision']) ||
        !organizationControlVersion(m['proposalVersion'])) {
      throw const FormatException('原组织操作结果不符');
    }
    final stopped = m['status'] == 'STOPPED';
    bool good = stopped
        ? m['action'] == 'STOP' &&
              m['authorityVersion'] == 0 &&
              m['memberVersion'] == 0 &&
              m['canonicalRevision'] == 0 &&
              m['proposalVersion'] == 0 &&
              !m.containsKey('membershipId') &&
              !m.containsKey('proposalId')
        : m['status'] == 'SUCCEEDED' && m['action'] == s.action;
    if (!stopped && good) {
      if (s.memberAction) {
        good =
            m['authorityVersion'] == s.expectedAuthorityVersion + 1 &&
            m['memberVersion'] == s.expectedMemberVersion + 1 &&
            m['canonicalRevision'] == 0 &&
            m['proposalVersion'] == 0 &&
            !m.containsKey('proposalId') &&
            m['membershipId'] ==
                (s.action == 'INVITE' && s.expectedMemberVersion == 0
                    ? s.operationID
                    : s.membershipID);
      } else {
        good =
            m['authorityVersion'] == s.expectedAuthorityVersion &&
            m['memberVersion'] == 0 &&
            !m.containsKey('membershipId');
        if (s.action == 'HIDE_POINT') {
          good =
              good &&
              m['canonicalRevision'] == s.expectedCanonicalRevision + 1 &&
              m['proposalVersion'] == 0 &&
              !m.containsKey('proposalId');
        } else {
          good =
              good &&
              m['proposalId'] ==
                  (s.action == 'PROPOSE' ? s.operationID : s.proposalID) &&
              m['proposalVersion'] ==
                  (s.action == 'PROPOSE' ? 1 : s.expectedProposalVersion + 1) &&
              (s.action == 'WITHDRAW_POINT'
                  ? true
                  : m['canonicalRevision'] ==
                        s.expectedCanonicalRevision +
                            (s.action == 'APPROVE_POINT' ? 1 : 0));
        }
      }
    }
    if (!good) throw const FormatException('原组织操作回执、目标或版本不符');
    return OrganizationControlReceipt(
      selector: s,
      status: m['status'],
      authorityVersion: m['authorityVersion'],
      memberVersion: m['memberVersion'],
      canonicalRevision: m['canonicalRevision'],
      proposalVersion: m['proposalVersion'],
      membershipID: m['membershipId'] ?? '',
      proposalID: m['proposalId'] ?? '',
    );
  }
}

class OrganizationControlFailure implements Exception {
  const OrganizationControlFailure(this.status);
  final int status;
  String get message => switch (status) {
    400 => '操作、目标或版本资料有误，请检查。',
    401 => '登录已失效，请重新登录。',
    403 => '当前没有查看或执行此组织操作的权限。',
    404 => '组织、目标或原操作暂不可见。',
    409 => '组织、成员或位置版本已变化，请重新读取后检查。',
    _ => '组织服务暂不可用，请稍后重试读取。',
  };
}

class OrganizationHumanControlApi {
  const OrganizationHumanControlApi({
    required this.client,
    required this.baseUrl,
    required this.authorizationHeader,
    this.current,
  });
  final http.Client client;
  final String baseUrl;
  final String? Function() authorizationHeader;
  final bool Function()? current;
  OrganizationHumanControlApi scoped(bool Function() guard) =>
      OrganizationHumanControlApi(
        client: client,
        baseUrl: baseUrl,
        authorizationHeader: authorizationHeader,
        current: () => current?.call() != false && guard(),
      );
  String path(String org) {
    if (!organizationControlID(org)) throw const FormatException('组织标识不符');
    return '/v1/me/organizations/${Uri.encodeComponent(org)}';
  }

  Future<Object?> json(
    String method,
    String path, {
    Map<String, Object>? body,
    Map<String, String>? query,
  }) async {
    final base = Uri.tryParse(baseUrl), token = authorizationHeader();
    if (base == null ||
        !const {'http', 'https'}.contains(base.scheme) ||
        base.host.isEmpty ||
        base.userInfo.isNotEmpty ||
        base.hasQuery ||
        base.hasFragment ||
        token == null ||
        current?.call() == false)
      throw const OrganizationControlFailure(401);
    var url = Uri.parse('${baseUrl.replaceFirst(RegExp(r'/$'), '')}$path');
    if (query != null) url = url.replace(queryParameters: query);
    if (url.origin != base.origin) throw const FormatException('组织操作来源不符');
    final request = http.Request(method, url)..followRedirects = false;
    request.headers['Authorization'] = token;
    if (body != null) {
      request.headers['Content-Type'] = 'application/json';
      request.body = jsonEncode(body);
    }
    final result = await client
        .send(request)
        .timeout(const Duration(seconds: 12));
    final bytes = BytesBuilder(copy: false);
    await for (final chunk in result.stream.timeout(
      const Duration(seconds: 12),
    )) {
      if (chunk.length > 1024 * 1024 - bytes.length)
        throw const FormatException('组织响应过大');
      bytes.add(chunk);
    }
    if (token != authorizationHeader() || current?.call() == false)
      throw const OrganizationControlFailure(401);
    if (result.statusCode != 200)
      throw OrganizationControlFailure(result.statusCode);
    return organizationControlObject(
      jsonDecode(utf8.decode(bytes.takeBytes())),
      const {'data'},
    )['data'];
  }

  Future<OrganizationControlPreview> preview(
    String org,
    OrganizationControlInput input,
  ) async => OrganizationControlPreview.fromJson(
    await json('POST', '${path(org)}/control/preview', body: input.toJson()),
    org,
    input,
  );
  Future<OrganizationControlReceipt> apply(
    OrganizationControlPreview preview,
    OrganizationControlSelector selector,
  ) async {
    if (!selector.same(
          preview.input.selector(
            preview.organizationID,
            invitedMembershipID: selector.action == 'INVITE'
                ? selector.membershipID
                : '',
          ),
        ) ||
        !preview.expiresAt.isAfter(DateTime.now().toUtc()))
      throw const FormatException('这次组织确认已变化或到期');
    return OrganizationControlReceipt.fromJson(
      await json(
        'PUT',
        '${path(preview.organizationID)}/control',
        body: preview.input.toJson(approval: preview.approval),
      ),
      selector,
    );
  }

  Future<OrganizationControlReceipt> operation(
    OrganizationControlSelector s,
  ) async => OrganizationControlReceipt.fromJson(
    await json(
      'GET',
      '${path(s.organizationID)}/control/operations/${s.operationID}',
    ),
    s,
  );
  Future<OrganizationControlReceipt> stop(
    OrganizationControlSelector s,
  ) async => OrganizationControlReceipt.fromJson(
    await json(
      'POST',
      '${path(s.organizationID)}/control/operations/${s.operationID}/stop',
      body: {'expectedAuthorityVersion': s.expectedAuthorityVersion},
    ),
    s,
  );
}

class OrganizationControlPending {
  const OrganizationControlPending(this.selector, {this.stage = 'UNKNOWN'});
  final OrganizationControlSelector selector;
  final String stage;
  bool same(OrganizationControlPending o) => selector.same(o.selector);
  Map<String, Object> toJson() => {
    'selector': selector.toJson(),
    'stage': stage,
  };
  static OrganizationControlPending fromJson(Object? raw) {
    final m = organizationControlObject(raw, const {'selector', 'stage'});
    if (!const {'PREPARED', 'UNKNOWN'}.contains(m['stage']))
      throw const FormatException('原组织记录阶段不符');
    return OrganizationControlPending(
      OrganizationControlSelector.fromJson(m['selector']),
      stage: m['stage'],
    );
  }
}

abstract interface class OrganizationControlPendingStore {
  Future<List<String>> organizations(String origin, String owner);
  Future<OrganizationControlPending?> read(
    String origin,
    String owner,
    String org,
  );
  Future<void> write(
    String origin,
    String owner,
    String org,
    OrganizationControlPending pending,
  );
  Future<void> remove(
    String origin,
    String owner,
    String org,
    OrganizationControlPending pending,
  );
}

class SecureOrganizationControlPendingStore
    implements OrganizationControlPendingStore {
  SecureOrganizationControlPendingStore({FlutterSecureStorage? storage})
    : _storage = storage ?? const FlutterSecureStorage();
  final FlutterSecureStorage _storage;
  static final _tails = <String, Future<void>>{};
  String _key(String o, String owner, String org) =>
      'birdtie.organization.control.pending.v1.${sha256.convert(utf8.encode('$o\n$owner\n$org'))}';
  String _index(String o, String owner) =>
      'birdtie.organization.control.index.v1.${sha256.convert(utf8.encode('$o\n$owner'))}';
  Future<T> _serial<T>(String key, Future<T> Function() work) async {
    final old = _tails[key], done = Completer<void>();
    _tails[key] = done.future;
    try {
      if (old != null) await old;
      return await work();
    } finally {
      if (identical(_tails[key], done.future)) _tails.remove(key);
      done.complete();
    }
  }

  Future<OrganizationControlPending?> _read(String key) async {
    final raw = await _storage.read(key: key);
    return raw == null
        ? null
        : OrganizationControlPending.fromJson(jsonDecode(raw));
  }

  Future<List<String>> _orgs(String key) async {
    final raw = await _storage.read(key: key);
    if (raw == null) return [];
    final list = jsonDecode(raw);
    if (list is! List ||
        list.length > 1000 ||
        list.any((id) => !organizationControlID(id))) {
      throw const FormatException('本机组织操作索引不符');
    }
    return list.cast<String>().toSet().toList();
  }

  @override
  Future<List<String>> organizations(String o, String owner) =>
      _serial(_index(o, owner), () => _orgs(_index(o, owner)));
  @override
  Future<OrganizationControlPending?> read(
    String o,
    String owner,
    String org,
  ) => _serial(_index(o, owner), () => _read(_key(o, owner, org)));
  @override
  Future<void> write(
    String o,
    String owner,
    String org,
    OrganizationControlPending p,
  ) => _serial(_index(o, owner), () async {
    OrganizationControlPending.fromJson(p.toJson());
    if (p.selector.organizationID != org)
      throw const FormatException('原组织记录归属不符');
    final key = _key(o, owner, org), old = await _read(key);
    if (old != null &&
        (!old.same(p) || old.stage == 'UNKNOWN' && p.stage == 'PREPARED'))
      throw const FormatException('原组织操作尚未核实');
    await _storage.write(key: key, value: jsonEncode(p.toJson()));
    final index = _index(o, owner), orgs = await _orgs(_index(o, owner));
    if (!orgs.contains(org)) orgs.add(org);
    await _storage.write(key: index, value: jsonEncode(orgs));
    if (!(await _orgs(index)).contains(org))
      throw const FormatException('本机组织操作索引未准备完成');
  });
  @override
  Future<void> remove(
    String o,
    String owner,
    String org,
    OrganizationControlPending p,
  ) => _serial(_index(o, owner), () async {
    final key = _key(o, owner, org), old = await _read(key);
    if (old != null && (!old.same(p) || old.stage != p.stage))
      throw const FormatException('原组织记录已变化');
    await _storage.delete(key: key);
    final index = _index(o, owner), orgs = await _orgs(_index(o, owner));
    orgs.remove(org);
    await _storage.write(key: index, value: jsonEncode(orgs));
  });
}

class MemoryOrganizationControlPendingStore
    implements OrganizationControlPendingStore {
  final values = <(String, String, String), OrganizationControlPending>{};
  @override
  Future<List<String>> organizations(String o, String owner) async => values
      .keys
      .where((k) => k.$1 == o && k.$2 == owner)
      .map((k) => k.$3)
      .toList();
  @override
  Future<OrganizationControlPending?> read(
    String o,
    String owner,
    String org,
  ) async => values[(o, owner, org)];
  @override
  Future<void> write(
    String o,
    String owner,
    String org,
    OrganizationControlPending p,
  ) async {
    OrganizationControlPending.fromJson(p.toJson());
    final old = values[(o, owner, org)];
    if (p.selector.organizationID != org ||
        old != null &&
            (!old.same(p) || old.stage == 'UNKNOWN' && p.stage == 'PREPARED'))
      throw const FormatException('原组织操作尚未核实');
    values[(o, owner, org)] = p;
  }

  @override
  Future<void> remove(
    String o,
    String owner,
    String org,
    OrganizationControlPending p,
  ) async {
    final old = values[(o, owner, org)];
    if (old != null && (!old.same(p) || old.stage != p.stage))
      throw const FormatException('原组织记录已变化');
    values.remove((o, owner, org));
  }
}

String organizationControlMessage(Object error) =>
    error is OrganizationControlFailure
    ? error.message
    : error is FormatException
    ? error.message
    : '读取或准备未完成，请稍后核实。';

/// Source generations detect A -> B -> A; matching a late token or ID alone is
/// insufficient. A normal credential renewal keeps owner-scoped local drafts.
class OrganizationHumanPageBinding {
  OrganizationHumanPageBinding({
    required this.api,
    required this.target,
    required this.header,
    required this.sourceCurrent,
    required this.onRetired,
    this.ownerID,
    this.identityChanges,
    OrganizationControlPendingStore? store,
  }) : store = store ?? SecureOrganizationControlPendingStore() {
    _seen = _identity;
    _scopeSeen = _scope;
    identityChanges?.addListener(observe);
  }
  final OrganizationHumanControlApi api;
  final Object Function() target;
  final String? Function() header;
  final bool Function() sourceCurrent;
  final String? Function()? ownerID;
  final Listenable? identityChanges;
  final void Function(bool ownerOrTargetChanged) onRetired;
  final OrganizationControlPendingStore store;
  final operations = <String, OrganizationControlOperation>{};
  Object? _seen, _scopeSeen;
  String? _resolvedOwner;
  int epoch = 0;
  bool disposed = false;
  Object get _identity =>
      (target(), ownerID?.call(), header(), sourceCurrent());
  Object get _scope => (target(), ownerID?.call(), sourceCurrent());
  String? get owner => ownerID == null ? _resolvedOwner : ownerID!();
  bool get current =>
      !disposed && sourceCurrent() && _seen == _identity && header() != null;
  bool Function() capture() {
    observe();
    final generation = epoch, identity = _seen;
    return () => current && epoch == generation && _seen == identity;
  }

  void observe() {
    if (disposed || _seen == _identity) return;
    final scopeChanged = _scopeSeen != _scope;
    _seen = _identity;
    _scopeSeen = _scope;
    ++epoch;
    _resolvedOwner = null;
    for (final op in operations.values) {
      op.retire();
    }
    onRetired(scopeChanged);
  }

  Future<void> resolveOwner() async {
    final guard = capture();
    if (!guard()) throw const OrganizationControlFailure(401);
    if (ownerID != null) {
      if (!organizationControlID(owner))
        throw const OrganizationControlFailure(401);
      return;
    }
    final raw = await api.scoped(guard).json('GET', '/v1/me');
    if (!guard()) throw const OrganizationControlFailure(401);
    if (raw is! Map<String, dynamic> || !organizationControlID(raw['id']))
      throw const FormatException('当前个人账号不符');
    _resolvedOwner = raw['id'];
  }

  OrganizationControlOperation operation(String org) => operations.putIfAbsent(
    org,
    () => OrganizationControlOperation(
      api: api,
      organizationID: org,
      ownerID: () => owner,
      current: () => current,
      store: store,
    ),
  );
  Future<void> restore(String org) async {
    final op = operation(org);
    if (!op.active || !op.storageReady) await op.restore();
  }

  void dispose() {
    if (disposed) return;
    disposed = true;
    ++epoch;
    identityChanges?.removeListener(observe);
    for (final op in operations.values) {
      op.dispose();
    }
    operations.clear();
  }
}

void organizationPageUpdate(State state, VoidCallback change) {
  if (!state.mounted) return;
  if (SchedulerBinding.instance.schedulerPhase ==
      SchedulerPhase.persistentCallbacks) {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (state.mounted) change();
    });
  } else {
    change();
  }
}

/// One original operation per owner/origin/organization. The approval and input
/// remain only in memory; a restart recovers identifiers, never a write body.
class OrganizationControlOperation extends ChangeNotifier {
  OrganizationControlOperation({
    required this.api,
    required this.organizationID,
    required this.ownerID,
    required this.current,
    OrganizationControlPendingStore? store,
  }) : store = store ?? SecureOrganizationControlPendingStore();
  final OrganizationHumanControlApi api;
  final String organizationID;
  final String? Function() ownerID;
  final bool Function() current;
  final OrganizationControlPendingStore store;
  String get origin => api.baseUrl.replaceFirst(RegExp(r'/$'), '');
  OrganizationControlPending? pending;
  OrganizationControlReceipt? receipt;
  String? message;
  bool busy = false, storageReady = false;
  bool _disposed = false;
  int _epoch = 0;
  String? _owner, _header;
  OrganizationControlSelector? _notSubmitted;
  OrganizationControlPreview? _preview;
  final retirement = ValueNotifier<int>(0);
  int _dialogs = 0;
  bool get active =>
      !_disposed &&
      current() &&
      _owner != null &&
      _owner == ownerID() &&
      _header == api.authorizationHeader();
  bool get canWrite => active && storageReady && !busy && pending == null;
  bool get locallyPrepared =>
      pending?.stage == 'PREPARED' ||
      pending != null && _notSubmitted?.same(pending!.selector) == true;
  void _changed() {
    if (!_disposed) notifyListeners();
  }

  void retire() {
    ++_epoch;
    _owner = null;
    _header = null;
    _preview = null;
    pending = null;
    receipt = null;
    message = null;
    _notSubmitted = null;
    storageReady = false;
    busy = false;
    retirement.value++;
  }

  void invalidatePreview() {
    _preview = null;
    retirement.value++;
  }

  bool _live(int epoch, String owner, String? header) =>
      !_disposed &&
      current() &&
      epoch == _epoch &&
      owner == ownerID() &&
      header == api.authorizationHeader();
  Future<void> restore() async {
    if (_disposed || !current() || !organizationControlID(ownerID())) return;
    final epoch = _epoch,
        owner = ownerID()!,
        header = api.authorizationHeader();
    _owner = owner;
    _header = header;
    busy = true;
    _changed();
    try {
      final p = await store.read(origin, owner, organizationID);
      if (!_live(epoch, owner, header)) return;
      if (p != null && p.selector.organizationID != organizationID) {
        throw const FormatException('本机操作属于其他组织');
      }
      pending = p;
      storageReady = true;
      message = p == null
          ? null
          : p.stage == 'PREPARED'
          ? '上次仅准备了本机记录，未发送操作。请检查本机记录后重新预览。'
          : '上次操作结果尚未核实。只能读取原回执或明确停止，不能重新发送。';
    } catch (e) {
      if (_live(epoch, owner, header)) {
        storageReady = false;
        message = '本机恢复记录暂不可读；未发送操作。请检查本机记录。';
      }
    } finally {
      if (_live(epoch, owner, header)) {
        busy = false;
        _changed();
      }
    }
  }

  bool previewCurrent(OrganizationControlPreview p) =>
      canWrite &&
      identical(_preview, p) &&
      p.organizationID == organizationID &&
      p.expiresAt.isAfter(DateTime.now().toUtc());
  Future<OrganizationControlPreview?> preview(
    OrganizationControlInput input,
  ) async {
    if (!canWrite) return null;
    final epoch = _epoch, owner = _owner!, header = _header;
    busy = true;
    message = null;
    _preview = null;
    _changed();
    try {
      final p = await api
          .scoped(() => _live(epoch, owner, header))
          .preview(organizationID, input);
      if (!_live(epoch, owner, header)) return null;
      _preview = p;
      return p;
    } catch (e) {
      if (_live(epoch, owner, header)) message = organizationControlMessage(e);
      return null;
    } finally {
      if (_live(epoch, owner, header)) {
        busy = false;
        _changed();
      }
    }
  }

  Future<OrganizationControlReceipt?> submit(
    OrganizationControlPreview p,
    OrganizationControlSelector s, {
    required bool Function() sourceCurrent,
  }) async {
    if (!previewCurrent(p) || !sourceCurrent() || !s.valid) return null;
    final epoch = _epoch, owner = _owner!, header = _header;
    final guard = () => _live(epoch, owner, header) && sourceCurrent();
    bool sent = false;
    OrganizationControlPending prepared = OrganizationControlPending(
      s,
      stage: 'PREPARED',
    );
    busy = true;
    message = null;
    _changed();
    try {
      await store.write(origin, owner, organizationID, prepared);
      if (!guard()) throw const FormatException('原来源已变化，尚未发送操作。');
      final back = await store.read(origin, owner, organizationID);
      if (!guard()) throw const FormatException('原来源已变化，尚未发送操作。');
      if (back == null || !back.same(prepared) || back.stage != 'PREPARED') {
        throw const FormatException('本机准备记录未核对完成');
      }
      pending = prepared;
      final unknown = OrganizationControlPending(s);
      await store.write(origin, owner, organizationID, unknown);
      if (!guard()) throw const FormatException('原来源已变化，尚未发送操作。');
      final finalBack = await store.read(origin, owner, organizationID);
      if (!guard()) throw const FormatException('原来源已变化，尚未发送操作。');
      if (finalBack == null ||
          !finalBack.same(unknown) ||
          finalBack.stage != 'UNKNOWN') {
        throw const FormatException('本机提交记录未核对完成');
      }
      pending = unknown;
      if (!p.expiresAt.isAfter(DateTime.now().toUtc()))
        throw const FormatException('这次确认已到期；操作未发送。');
      sent = true;
      final r = await api.scoped(guard).apply(p, s);
      if (!guard()) return null;
      await _finish(r, unknown, owner, epoch, header);
      return guard() ? r : null;
    } catch (e) {
      if (_live(epoch, owner, header)) {
        _preview = null;
        if (!sent) {
          _notSubmitted = s;
          try {
            final stored = await store.read(origin, owner, organizationID);
            if (_live(epoch, owner, header)) {
              pending = stored;
              if (stored == null) storageReady = false;
            }
          } catch (_) {
            storageReady = false;
          }
          if (_live(epoch, owner, header))
            message = '操作未发送；草稿仍在本页。请检查本机准备记录后重新预览。';
        } else {
          pending = OrganizationControlPending(s);
          _notSubmitted = null;
          message = '操作结果尚未核实。请核实上次操作，或明确停止原操作；不会重新发送。';
        }
      }
      return null;
    } finally {
      if (_live(epoch, owner, header)) {
        busy = false;
        _changed();
      }
    }
  }

  Future<void> repairPrepared() async {
    if (!active || busy) return;
    final epoch = _epoch, owner = _owner!, header = _header;
    busy = true;
    _changed();
    try {
      final p = await store.read(origin, owner, organizationID);
      if (!_live(epoch, owner, header)) return;
      if (p != null &&
          (p.stage == 'PREPARED' || _notSubmitted?.same(p.selector) == true)) {
        await store.remove(origin, owner, organizationID, p);
        if (!_live(epoch, owner, header)) return;
        pending = null;
        _notSubmitted = null;
        storageReady = true;
        message = '本机准备记录已清理；尚未发送。请重新检查并预览。';
      } else {
        pending = p;
        storageReady = true;
        _notSubmitted = null;
        message = p == null ? null : '发现本人另一条尚未核实的原操作，请读取原回执。';
      }
    } catch (e) {
      if (_live(epoch, owner, header)) message = '本机记录尚未核对完成；不会发送操作。';
    } finally {
      if (_live(epoch, owner, header)) {
        busy = false;
        _changed();
      }
    }
  }

  Future<OrganizationControlReceipt?> recover({bool stop = false}) async {
    if (!active || busy || pending == null || locallyPrepared) return null;
    final epoch = _epoch, owner = _owner!, header = _header, p = pending!;
    busy = true;
    message = null;
    _changed();
    try {
      final scoped = api.scoped(() => _live(epoch, owner, header));
      final r = stop
          ? await scoped.stop(p.selector)
          : await scoped.operation(p.selector);
      if (!_live(epoch, owner, header)) return null;
      await _finish(r, p, owner, epoch, header);
      return _live(epoch, owner, header) ? r : null;
    } catch (e) {
      if (_live(epoch, owner, header)) message = '原操作仍未核实。当前不可读不代表没有执行；不会重新发送。';
      return null;
    } finally {
      if (_live(epoch, owner, header)) {
        busy = false;
        _changed();
      }
    }
  }

  Future<void> _finish(
    OrganizationControlReceipt r,
    OrganizationControlPending p,
    String owner,
    int epoch,
    String? header,
  ) async {
    receipt = r;
    _preview = null;
    _notSubmitted = null;
    message = r.status == 'STOPPED' ? '原操作已停止。' : '原操作已完成。';
    try {
      await store.remove(origin, owner, organizationID, p);
      if (!_live(epoch, owner, header)) return;
      pending = null;
      storageReady = true;
    } catch (_) {
      if (_live(epoch, owner, header))
        message =
            '原操作已确证${r.status == 'STOPPED' ? '停止' : '完成'}，本机记录清理尚未完成；可再次核实同一原回执。';
    }
  }

  void _dialogEntered() => ++_dialogs;
  void _dialogLeft() {
    --_dialogs;
    if (_disposed && _dialogs == 0) retirement.dispose();
  }

  @override
  void dispose() {
    if (_disposed) return;
    retire();
    _disposed = true;
    if (_dialogs == 0) retirement.dispose();
    super.dispose();
  }
}

Widget organizationOperationRecovery(
  OrganizationControlOperation operation,
  Future<void> Function({required bool stop}) recover,
) {
  final original = operation.pending;
  bool current() =>
      operation.active &&
      !operation.busy &&
      identical(operation.pending, original);
  return Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      if (operation.message != null)
        Text(
          operation.message!,
          key: const Key('organization_operation_message'),
        ),
      if (operation.locallyPrepared || !operation.storageReady)
        TextButton(
          key: const Key('organization_check_local'),
          onPressed: operation.busy ? null : operation.repairPrepared,
          child: const Text('检查本机准备记录'),
        ),
      if (operation.pending != null && !operation.locallyPrepared) ...[
        TextButton(
          key: const Key('organization_verify_operation'),
          onPressed: operation.busy
              ? null
              : () {
                  if (current()) unawaited(recover(stop: false));
                },
          child: const Text('核实上次操作'),
        ),
        const Text('停止只影响原尚未执行的操作；若已执行，会返回原回执。'),
        TextButton(
          key: const Key('organization_stop_operation'),
          onPressed: operation.busy
              ? null
              : () {
                  if (current()) unawaited(recover(stop: true));
                },
          child: const Text('停止原操作并核实'),
        ),
      ],
    ],
  );
}

/// Own notifier/controller resources through the route's reverse animation.
Future<bool> confirmOrganizationControl(
  BuildContext context,
  OrganizationControlOperation operation,
  OrganizationControlPreview preview, {
  required String title,
  required String consequence,
  required String target,
  required bool Function() sourceCurrent,
}) async {
  final live = ValueNotifier<bool>(true);
  final retired = operation.retirement.value;
  bool valid() =>
      live.value &&
      retired == operation.retirement.value &&
      operation.previewCurrent(preview) &&
      sourceCurrent();
  late final DialogRoute<bool> route;
  operation._dialogEntered();
  route = DialogRoute<bool>(
    context: context,
    builder: (dialogContext) => ListenableBuilder(
      listenable: Listenable.merge([live, operation.retirement]),
      builder: (context, _) {
        final enabled = valid();
        return AlertDialog(
          title: Text(enabled ? title : '这次确认已到期或来源已变化'),
          content: SingleChildScrollView(
            child: enabled
                ? Column(
                    mainAxisSize: MainAxisSize.min,
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(target),
                      const SizedBox(height: 12),
                      Text(consequence),
                      const SizedBox(height: 12),
                      Text(
                        '按这次读取的具体版本确认；组织权限 ${preview.input.expectedAuthorityVersion}。',
                      ),
                    ],
                  )
                : const Text('旧目标与私人资料已移除。关闭后重新读取；不会自动执行。'),
          ),
          actions: [
            TextButton(
              onPressed: () {
                if (live.value) Navigator.of(dialogContext).pop(false);
              },
              child: const Text('取消'),
            ),
            FilledButton(
              key: const Key('organization_confirm_control'),
              onPressed: enabled
                  ? () {
                      if (!valid()) return;
                      Navigator.of(dialogContext).pop(true);
                    }
                  : null,
              child: const Text('确认执行'),
            ),
          ],
        );
      },
    ),
  );
  final timer = Timer(preview.expiresAt.difference(DateTime.now().toUtc()), () {
    if (live.value) live.value = false;
  });
  try {
    final result = await Navigator.of(context).push(route);
    final accepted = result == true && valid();
    live.value = false;
    return accepted;
  } finally {
    timer.cancel();
    unawaited(
      route.completed.then((_) {
        live.dispose();
        operation._dialogLeft();
      }),
    );
  }
}
