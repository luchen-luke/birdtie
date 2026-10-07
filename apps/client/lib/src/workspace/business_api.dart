import 'dart:convert';

import 'package:http/http.dart' as http;

import '../config/birdtie_environment.dart';
import 'business_knowledge_api.dart';
import 'business_agent_identity_model.dart';

/// The server resolves Person, merchant membership and independent review
/// grants. No Organization/Agent selector or client approval grants authority.
class BusinessApiException implements Exception {
  const BusinessApiException(this.status, {this.outcomeUnknown = false});
  final int status;
  final bool outcomeUnknown;

  String get message => outcomeUnknown
      ? '操作结果尚未确认，请先重新读取商家状态，再决定下一步。'
      : switch (status) {
          400 => '资料格式无效，请检查填写内容。',
          401 => '登录已失效，请重新登录。',
          403 => '当前账号没有管理或审核这项资料的权限。',
          404 => '未找到当前商家或资料。',
          409 => '资料版本或权限已变化，请重新读取后检查。',
          _ => '商家工作台暂不可用，请稍后重试。',
        };
}

class BusinessSummary {
  const BusinessSummary({
    required this.id,
    required this.name,
    required this.claimStatus,
    required this.role,
  });
  final String id;
  final String name;
  final String claimStatus;
  final String role;

  factory BusinessSummary.fromJson(Map<String, dynamic> json) {
    final id = json['id'];
    final name = json['name'];
    final state = json['claimStatus'];
    final role = json['role'];
    if (id is! String ||
        !BusinessApi.validID(id) ||
        name is! String ||
        name.trim().isEmpty ||
        state is! String ||
        !const {'pending', 'verified', 'rejected', 'revoked'}.contains(state) ||
        role is! String ||
        !const {'owner', 'admin', 'reviewer'}.contains(role)) {
      throw const BusinessApiException(503);
    }
    return BusinessSummary(id: id, name: name, claimStatus: state, role: role);
  }
}

dynamic _immutable(dynamic value) {
  if (value is Map<String, dynamic>) {
    return Map<String, dynamic>.unmodifiable(
      value.map((key, item) => MapEntry(key, _immutable(item))),
    );
  }
  if (value is List) {
    return List<dynamic>.unmodifiable(value.map(_immutable));
  }
  return value;
}

class BusinessConsoleSnapshot {
  BusinessConsoleSnapshot._(this.data, this.business);
  final Map<String, dynamic> data;
  final BusinessSummary business;
  bool get canManage => data['canManage'] as bool;
  bool get canManageMembers => data['canManageMembers'] as bool;
  List<String> get reviewPermissions =>
      List<String>.unmodifiable(data['reviewPermissions'] as List);
  int get membershipVersion => data['membershipVersion'] as int;
  Map<String, dynamic>? get claim => data['claim'] as Map<String, dynamic>?;
  Map<String, dynamic>? get profile => data['profile'] as Map<String, dynamic>?;
  List<Map<String, dynamic>> get venues =>
      (data['venues'] as List).cast<Map<String, dynamic>>();
  List<Map<String, dynamic>> get members =>
      (data['members'] as List).cast<Map<String, dynamic>>();

  factory BusinessConsoleSnapshot.fromJson(Map<String, dynamic> json) {
    final business = json['business'];
    final permissions = json['reviewPermissions'];
    if (business is! Map<String, dynamic> ||
        json['canManage'] is! bool ||
        json['canManageMembers'] is! bool ||
        json['canReview'] is! bool ||
        json['membershipVersion'] is! int ||
        (json['membershipVersion'] as int) < 0 ||
        permissions is! List ||
        permissions.length > 3 ||
        permissions.any(
          (p) => !const {'claim', 'profile', 'venue'}.contains(p),
        ) ||
        permissions.toSet().length != permissions.length ||
        json['venues'] is! List ||
        (json['venues'] as List).length > 100 ||
        json['members'] is! List ||
        (json['members'] as List).length > 100 ||
        (json['venues'] as List).any((v) => v is! Map<String, dynamic>) ||
        (json['members'] as List).any((v) => v is! Map<String, dynamic>) ||
        (json['claim'] != null && json['claim'] is! Map<String, dynamic>) ||
        (json['profile'] != null && json['profile'] is! Map<String, dynamic>)) {
      throw const BusinessApiException(503);
    }
    return BusinessConsoleSnapshot._(
      _immutable(json) as Map<String, dynamic>,
      BusinessSummary.fromJson(business),
    );
  }
}

class BusinessApi {
  BusinessApi({
    required this.authorizationHeader,
    http.Client? client,
    String? apiBaseUrl,
    this.timeout = const Duration(seconds: 15),
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;

  final String? Function() authorizationHeader;
  final http.Client _client;
  final bool _ownsClient;
  final String _base;
  final Duration timeout;

  static bool validID(String id) =>
      RegExp(
        r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
      ).hasMatch(id) &&
      id != '00000000-0000-0000-0000-000000000000';

  void dispose() {
    if (_ownsClient) _client.close();
  }

  String _path(String id, [String suffix = '/console']) {
    if (!validID(id)) throw const BusinessApiException(400);
    return '/v1/me/businesses/$id$suffix';
  }

  Future<dynamic> _request(
    String method,
    String path, {
    Map<String, dynamic>? body,
    bool readOnly = false,
  }) async {
    final token = authorizationHeader();
    if (token == null || !token.startsWith('Bearer ') || token.length <= 7) {
      throw const BusinessApiException(401);
    }
    if (_base.isEmpty) throw const BusinessApiException(503);
    final write = method != 'GET' && !readOnly;
    final request = http.Request(
      method,
      Uri.parse('${_base.replaceFirst(RegExp(r'/$'), '')}$path'),
    )..headers.addAll({'Accept': 'application/json', 'Authorization': token});
    if (body != null) {
      request.headers['Content-Type'] = 'application/json';
      request.body = jsonEncode(body);
      if (utf8.encode(request.body).length > 24576) {
        throw const BusinessApiException(400);
      }
    }
    try {
      final response = await (() async {
        final streamed = await _client.send(request);
        final bytes = <int>[];
        await for (final chunk in streamed.stream) {
          bytes.addAll(chunk);
          if (bytes.length > 1048576) {
            throw BusinessApiException(503, outcomeUnknown: write);
          }
        }
        return http.Response.bytes(bytes, streamed.statusCode);
      })().timeout(timeout);
      if (authorizationHeader() != token) {
        throw BusinessApiException(409, outcomeUnknown: write);
      }
      if (response.statusCode != 200) {
        throw BusinessApiException(
          response.statusCode,
          // A Session/source denial can occur after the server committed
          // and rechecked response visibility. Never auto-repeat that write.
          outcomeUnknown:
              write && !const {400, 404, 409}.contains(response.statusCode),
        );
      }
      final envelope = jsonDecode(utf8.decode(response.bodyBytes));
      if (envelope is! Map<String, dynamic> || !envelope.containsKey('data')) {
        throw BusinessApiException(503, outcomeUnknown: write);
      }
      return envelope['data'];
    } on BusinessApiException {
      rethrow;
    } on Object {
      throw BusinessApiException(503, outcomeUnknown: write);
    }
  }

  Future<Map<String, dynamic>> _object(
    String method,
    String path, {
    Map<String, dynamic>? body,
  }) async {
    final data = await _request(method, path, body: body);
    if (data is! Map<String, dynamic>) {
      throw BusinessApiException(503, outcomeUnknown: method != 'GET');
    }
    return _immutable(data) as Map<String, dynamic>;
  }

  Future<List<BusinessSummary>> list() async {
    final data = await _request('GET', '/v1/me/businesses');
    if (data is! List || data.length > 100) {
      throw const BusinessApiException(503);
    }
    return List<BusinessSummary>.unmodifiable(
      data.map((item) {
        if (item is! Map<String, dynamic>) {
          throw const BusinessApiException(503);
        }
        return BusinessSummary.fromJson(item);
      }),
    );
  }

  Future<BusinessConsoleSnapshot> read(String id) async {
    final snapshot = BusinessConsoleSnapshot.fromJson(
      await _object('GET', _path(id)),
    );
    if (snapshot.business.id != id) throw const BusinessApiException(503);
    return snapshot;
  }

  /// A POST query reads approved human management facts; it cannot mutate them.
  Future<BusinessKnowledgeAnswer> askKnowledge(
    String id,
    String query, {
    String placeID = '',
    DateTime? now,
  }) async {
    if (!businessKnowledgeQuestions.contains(query) ||
        (businessKnowledgeNeedsPlace(query)
            ? !validID(placeID)
            : placeID != '')) {
      throw const BusinessApiException(400);
    }
    final data = await _request(
      'POST',
      _path(id, '/knowledge/ask'),
      body: {'query': query, 'placeId': placeID},
      readOnly: true,
    );
    try {
      return BusinessKnowledgeAnswer.read(
        data,
        businessID: id,
        query: query,
        placeID: placeID,
        now: now ?? DateTime.now().toUtc(),
      );
    } on Object {
      throw const BusinessApiException(503);
    }
  }

  Future<BusinessAgentIdentity> readAgentIdentity(String id, {DateTime? now}) async {
    return BusinessAgentIdentity.read(
      await _object('GET', _path(id, '/agent-identity')),
      businessID: id, now: now ?? DateTime.now().toUtc(),
    );
  }

  Future<BusinessAgentIdentity> establishAgentIdentity(String id, int claimVersion, {DateTime? now}) async {
    if (claimVersion < 1) throw const BusinessApiException(400);
    final data = await _object('POST', _path(id, '/agent-identity'), body: {'expectedClaimVersion': claimVersion});
    try {
      final result = BusinessAgentIdentity.read(data, businessID: id, now: now ?? DateTime.now().toUtc());
      if (result.agentID == null || result.claimVersion != claimVersion) throw const BusinessApiException(503);
      return result;
    } on Object {
      throw const BusinessApiException(503, outcomeUnknown: true);
    }
  }

  Future<Map<String, dynamic>> submitClaim(
    String id,
    Map<String, dynamic> draft,
  ) => _mutateFacts('PUT', _path(id, '/claim'), draft);

  Future<Map<String, dynamic>> putProfile(
    String id,
    Map<String, dynamic> draft,
  ) => _mutateFacts('PUT', _path(id, '/profile'), draft);

  Future<Map<String, dynamic>> putVenue(
    String id,
    String placeID,
    Map<String, dynamic> draft,
  ) {
    if (!validID(placeID)) throw const BusinessApiException(400);
    return _mutateFacts(
      'PUT',
      _path(id, '/venues/$placeID/facts'),
      draft,
      placeID: placeID,
    );
  }

  Future<Map<String, dynamic>> review(
    String id,
    String resource,
    Map<String, dynamic> approval, {
    String? placeID,
  }) {
    final suffix = switch (resource) {
      'claim' => '/claim/review',
      'profile' => '/profile/review',
      'venue' when placeID != null && validID(placeID) =>
        '/venues/$placeID/facts/review',
      _ => throw const BusinessApiException(400),
    };
    return _mutateFacts(
      'POST',
      _path(id, suffix),
      approval,
      placeID: resource == 'venue' ? placeID : null,
    );
  }

  Future<Map<String, dynamic>> _mutateFacts(
    String method,
    String path,
    Map<String, dynamic> body, {
    String? placeID,
  }) async {
    final result = await _object(method, path, body: body);
    if (result['version'] is! int ||
        (result['version'] as int) <= 0 ||
        !const {
          'pending',
          'verified',
          'rejected',
          'revoked',
        }.contains(result['state']) ||
        result['submittedBy'] is! String ||
        !validID(result['submittedBy'] as String) ||
        (placeID != null && result['placeId'] != placeID)) {
      throw const BusinessApiException(503, outcomeUnknown: true);
    }
    return result;
  }

  Future<BusinessConsoleSnapshot> changeMember(
    String id,
    Map<String, dynamic> approval,
  ) async {
    final response = await _object(
      'PUT',
      _path(id, '/members'),
      body: approval,
    );
    final BusinessConsoleSnapshot snapshot;
    try {
      snapshot = BusinessConsoleSnapshot.fromJson(response);
    } on BusinessApiException {
      throw const BusinessApiException(503, outcomeUnknown: true);
    }
    if (snapshot.business.id != id) {
      throw const BusinessApiException(503, outcomeUnknown: true);
    }
    return snapshot;
  }
}
