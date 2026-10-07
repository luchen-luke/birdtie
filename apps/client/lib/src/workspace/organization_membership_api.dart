import 'dart:convert';

import 'package:http/http.dart' as http;

import '../config/birdtie_environment.dart';

class OrganizationMember {
  const OrganizationMember({
    required this.id,
    required this.organizationID,
    required this.userAccountID,
    required this.displayName,
    required this.role,
    required this.status,
    this.organizationName = '',
  });
  final String id;
  final String organizationID;
  final String userAccountID;
  final String displayName;
  final String role;
  final String status;
  final String organizationName;

  factory OrganizationMember.fromJson(Map<String, dynamic> json) =>
      OrganizationMember(
        id: json['id'] as String,
        organizationID: json['organizationId'] as String,
        userAccountID: json['userAccountId'] as String,
        displayName: json['displayName'] as String,
        role: json['role'] as String,
        status: json['status'] as String,
        organizationName: json['organizationName'] as String? ?? '',
      );
}

class OrganizationMembershipException implements Exception {
  const OrganizationMembershipException(this.status);
  final int status;
  String get message => switch (status) {
    400 => '账号 ID 或角色无效。',
    401 => '登录已失效，请重新登录。',
    403 => '没有执行此操作的权限。',
    409 => '邀请已存在、角色未变化，或不能移除最后一位所有者。',
    _ => '成员操作暂不可用，请稍后重试。',
  };
}

class OrganizationMembershipApi {
  OrganizationMembershipApi({
    required this.authorizationHeader,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;

  final String? Function() authorizationHeader;
  final http.Client _client;
  final String _base;
  void dispose() => _client.close();

  Uri _uri(String path) =>
      Uri.parse('${_base.replaceFirst(RegExp(r'/$'), '')}$path');
  Map<String, String> get _headers => {
    'Accept': 'application/json',
    'Authorization': ?authorizationHeader(),
  };

  Future<http.Response> _send(
    String method,
    String path, {
    Map<String, dynamic>? body,
  }) async {
    if (_base.isEmpty || authorizationHeader() == null) {
      throw const OrganizationMembershipException(401);
    }
    final request = http.Request(method, _uri(path))..headers.addAll(_headers);
    if (body != null) {
      request.headers['Content-Type'] = 'application/json';
      request.body = jsonEncode(body);
    }
    final response = await _client
        .send(request)
        .timeout(const Duration(seconds: 10));
    return http.Response.fromStream(response);
  }

  List<OrganizationMember> _list(http.Response response) {
    if (response.statusCode != 200) {
      throw OrganizationMembershipException(response.statusCode);
    }
    final data =
        (jsonDecode(response.body) as Map<String, dynamic>)['data']
            as List<dynamic>;
    return data
        .map(
          (item) => OrganizationMember.fromJson(item as Map<String, dynamic>),
        )
        .toList();
  }

  OrganizationMember _one(http.Response response, int expected) {
    if (response.statusCode != expected) {
      throw OrganizationMembershipException(response.statusCode);
    }
    return OrganizationMember.fromJson(
      (jsonDecode(response.body) as Map<String, dynamic>)['data']
          as Map<String, dynamic>,
    );
  }

  Future<List<OrganizationMember>> list(String organizationID) async =>
      _list(await _send('GET', '/v1/me/organizations/$organizationID/members'));
  Future<List<OrganizationMember>> invitations() async =>
      _list(await _send('GET', '/v1/me/organization-invitations'));
  Future<OrganizationMember> invite(
    String organizationID,
    String accountID,
    String role,
  ) async => _one(
    await _send(
      'POST',
      '/v1/me/organizations/$organizationID/members',
      body: {'userAccountId': accountID, 'role': role},
    ),
    201,
  );
  Future<OrganizationMember> accept(String membershipID) async => _one(
    await _send('POST', '/v1/me/organization-invitations/$membershipID/accept'),
    200,
  );
  Future<OrganizationMember> changeRole(
    String organizationID,
    String membershipID,
    String role,
  ) async => _one(
    await _send(
      'PUT',
      '/v1/me/organizations/$organizationID/members/$membershipID/role',
      body: {'role': role},
    ),
    200,
  );
  Future<void> revoke(String organizationID, String membershipID) async {
    final response = await _send(
      'DELETE',
      '/v1/me/organizations/$organizationID/members/$membershipID',
    );
    if (response.statusCode != 204) {
      throw OrganizationMembershipException(response.statusCode);
    }
  }
}
