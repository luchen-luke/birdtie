import 'dart:convert';

import 'package:http/http.dart' as http;

import '../config/birdtie_environment.dart';
import 'entity_action_contract.dart';

class CommunityItem {
  const CommunityItem({
    required this.id,
    required this.name,
    required this.description,
    required this.visibility,
    required this.joinPolicy,
    required this.status,
    required this.memberCount,
    this.avatarUrl,
    this.cityId,
    this.myRole,
    this.myStatus,
  });

  final String id;
  final String name;
  final String description;
  final String visibility;
  final String joinPolicy;
  final String status;

  final int memberCount;
  final String? avatarUrl;
  final String? cityId;
  final String? myRole;
  final String? myStatus;

  bool get managed =>
      myStatus == 'active' && (myRole == 'owner' || myRole == 'admin');
  bool get joined => myStatus == 'active';

  factory CommunityItem.fromJson(Map<String, dynamic> json) => CommunityItem(
    id: json['id'] as String,
    name: json['name'] as String,
    description: json['description'] as String? ?? '',
    visibility: json['visibility'] as String,
    joinPolicy: json['joinPolicy'] as String,
    status: json['status'] as String,
    memberCount: json['memberCount'] as int? ?? 0,
    avatarUrl: json['avatarUrl'] as String?,
    cityId: json['cityId'] as String?,
    myRole: json['myRole'] as String?,
    myStatus: json['myStatus'] as String?,
  );
}

class CommunityMember {
  const CommunityMember({
    required this.id,
    required this.userAccountId,
    required this.displayName,
    required this.role,
    required this.status,
  });
  final String id;
  final String userAccountId;
  final String displayName;
  final String role;
  final String status;
  String get roleLabel => switch (role) {
    'owner' => '所有者',
    'admin' => '管理员',
    _ => '成员',
  };

  factory CommunityMember.fromJson(Map<String, dynamic> json) =>
      CommunityMember(
        id: json['id'] as String,
        userAccountId: json['userAccountId'] as String,
        displayName: json['displayName'] as String? ?? 'Birdtie 成员',
        role: json['role'] as String,
        status: json['status'] as String,
      );
}

class CommunityApiException implements Exception {
  const CommunityApiException(this.status);
  final int status;
  String get message => switch (status) {
    401 => '登录已失效，请重新登录。',
    403 => '你没有执行此操作的权限。',
    404 => '社群或内容暂不可访问。',
    409 => '当前状态不允许重复操作，请刷新后重试。',
    428 => '请先检查当前操作预览并确认。',
    422 => '请检查填写的信息。',
    _ => '社群暂不可用，请稍后重试。',
  };
}

class CommunityApi {
  CommunityApi({
    required this.authorizationHeader,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;

  final String? Function() authorizationHeader;
  final http.Client _client;
  http.Client get followClient => _client;
  String get followApiBaseUrl => _base;
  final String _base;

  void dispose() => _client.close();

  Future<http.Response> _send(
    String method,
    String path, {
    Map<String, dynamic>? body,
    Map<String, String> headers = const {},
  }) async {
    final token = authorizationHeader();
    if (_base.isEmpty || token == null) {
      {
        throw const CommunityApiException(401);
      }
    }
    final request = http.Request(
      method,
      Uri.parse('${_base.replaceFirst(RegExp(r'/$'), '')}$path'),
    )..headers['Accept'] = 'application/json';
    request.headers['Authorization'] = token;
    request.headers.addAll(headers);
    if (body != null) {
      request.headers['Content-Type'] = 'application/json';
      request.body = jsonEncode(body);
    }
    final streamed = await _client
        .send(request)
        .timeout(const Duration(seconds: 12));
    final response = await http.Response.fromStream(streamed);
    if (authorizationHeader() != token) throw const CommunityApiException(401);
    return response;
  }

  dynamic _data(http.Response response, int expected) {
    if (response.statusCode != expected) {
      throw CommunityApiException(response.statusCode);
    }
    if (expected == 204) return null;
    return (jsonDecode(utf8.decode(response.bodyBytes))
        as Map<String, dynamic>)['data'];
  }

  Future<List<CommunityItem>> mine() async =>
      (_data(await _send('GET', '/v1/me/social-communities'), 200)
              as List<dynamic>)
          .map((item) => CommunityItem.fromJson(item as Map<String, dynamic>))
          .toList();

  Future<List<CommunityItem>> discover({String? cityId}) async {
    final query = cityId == null
        ? ''
        : '?cityId=${Uri.encodeQueryComponent(cityId)}';
    final data =
        _data(await _send('GET', '/v1/communities$query'), 200)
            as List<dynamic>;
    return data
        .map((item) => CommunityItem.fromJson(item as Map<String, dynamic>))
        .toList();
  }

  Future<CommunityItem> detail(String id) async {
    _communityId(id);
    final item = CommunityItem.fromJson(
      _data(await _send('GET', '/v1/communities/$id'), 200)
          as Map<String, dynamic>,
    );
    if (item.id != id) throw const CommunityApiException(503);
    return item;
  }

  Future<CommunityItem> create({
    required String name,
    required String description,
    required String visibility,
    required String joinPolicy,
    String cityId = '',
    String avatarUrl = '',
  }) async => CommunityItem.fromJson(
    _data(
          await _send(
            'POST',
            '/v1/communities',
            body: {
              'name': name,
              'description': description,
              'visibility': visibility,
              'joinPolicy': joinPolicy,
              'cityId': cityId,
              'avatarUrl': avatarUrl,
            },
          ),
          201,
        )
        as Map<String, dynamic>,
  );

  Future<void> join(String id, {EntityActionDescriptor? approved}) async {
    _communityId(id);
    final data =
        _data(
              await _send(
                'POST',
                '/v1/communities/$id/join',
                headers: approved?.conditionHeaders ?? const {},
              ),
              200,
            )
            as Map<String, dynamic>;
    if (data['communityId'] != id ||
        data['status'] != 'active' && data['status'] != 'pending') {
      throw const CommunityApiException(503);
    }
  }

  Future<void> leave(String id, {EntityActionDescriptor? approved}) async =>
      _data(
        await _send(
          'POST',
          '/v1/communities/$id/leave',
          headers: approved?.conditionHeaders ?? const {},
        ),
        204,
      );
  Future<void> archive(String id, {CommunityApproval? approval}) async {
    if (approval == null ||
        approval.action.communityId != id ||
        approval.action.operation != 'archive') {
      throw const CommunityApiException(428);
    }
    await submit(approval);
  }

  Future<List<CommunityMember>> members(
    String id, {
    bool requests = false,
  }) async {
    final suffix = requests ? 'requests' : 'members';
    _communityId(id);
    final data =
        _data(await _send('GET', '/v1/communities/$id/$suffix'), 200)
            as List<dynamic>;
    if (data.any(
      (dynamic m) => m is! Map<String, dynamic> || m['communityId'] != id,
    )) {
      throw const CommunityApiException(503);
    }
    return data
        .map((item) => CommunityMember.fromJson(item as Map<String, dynamic>))
        .toList();
  }

  Future<void> decide(
    String id,
    String requestId, {
    required bool approve,
    CommunityApproval? approval,
  }) async {
    final decision = approve ? 'approve' : 'reject';
    if (approval == null ||
        approval.action.communityId != id ||
        approval.action.targetId != requestId ||
        approval.action.operation != decision) {
      throw const CommunityApiException(428);
    }
    await submit(approval);
  }

  Future<CommunityApproval> preview(
    CommunityAction action, {
    required String actorId,
  }) async {
    _communityId(actorId);
    _communityId(action.communityId);
    if (action.targetId.isNotEmpty) _communityId(action.targetId);
    final captured = authorizationHeader();
    final data =
        _data(
              await _send(
                action.method,
                action.path,
                body: action.body,
                headers: {'X-Birdtie-Community-Preview': '1'},
              ),
              200,
            )
            as Map<String, dynamic>;
    final expiry = DateTime.tryParse(data['expiresAt'] as String? ?? '');
    if (captured == null ||
        captured != authorizationHeader() ||
        data['actorId'] != actorId ||
        data['communityId'] != action.communityId ||
        data['operation'] != action.operation ||
        (data['targetId'] ?? '') != action.targetId ||
        (data['role'] ?? '') != action.role ||
        data['snapshot'] is! String ||
        (data['snapshot'] as String).isEmpty ||
        expiry == null) {
      throw const CommunityApiException(503);
    }
    return CommunityApproval._(
      action,
      data['snapshot'] as String,
      captured,
      actorId,
      expiry,
    );
  }

  Future<void> submit(CommunityApproval approval) async {
    if (approval._authorization != authorizationHeader()) {
      throw const CommunityApiException(401);
    }
    if (!approval.expiresAt.isAfter(DateTime.now().toUtc())) {
      throw const CommunityApiException(409);
    }
    final a = approval.action;
    final expected = a.operation == 'invite'
        ? 201
        : ({'archive', 'remove', 'transfer'}.contains(a.operation) ? 204 : 200);
    final data = _data(
      await _send(
        a.method,
        a.path,
        body: a.body,
        headers: {'X-Birdtie-Community-Snapshot': approval._snapshot},
      ),
      expected,
    );
    if (expected != 204) {
      if (data is! Map<String, dynamic> ||
          ((a.operation == 'update')
              ? data['id'] != a.communityId
              : data['communityId'] != a.communityId)) {
        throw const CommunityApiException(503);
      }
      if ({'approve', 'reject', 'role'}.contains(a.operation) &&
          data['id'] != a.targetId) {
        throw const CommunityApiException(503);
      }
      if (a.operation == 'invite' && data['userAccountId'] != a.targetId) {
        throw const CommunityApiException(503);
      }
      final expectedStatus = switch (a.operation) {
        'approve' => 'active',
        'reject' => 'rejected',
        'invite' => 'invited',
        _ => null,
      };
      if (expectedStatus != null && data['status'] != expectedStatus) {
        throw const CommunityApiException(503);
      }
      if (a.operation == 'role' &&
          (data['role'] != a.role || data['status'] != 'active')) {
        throw const CommunityApiException(503);
      }
    }
  }
}

final _communityUUID = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
void _communityId(String id) {
  if (!_communityUUID.hasMatch(id)) throw const CommunityApiException(422);
}

class CommunityAction {
  CommunityAction._(
    this.operation,
    this.communityId,
    this.targetId,
    this.role,
    this.method,
    this.path,
    Map<String, dynamic>? body,
  ) : body = body == null ? null : Map.unmodifiable(body);
  final String operation, communityId, targetId, role, method, path;
  final Map<String, dynamic>? body;
  factory CommunityAction.invite(String id, String person) => CommunityAction._(
    'invite',
    id,
    person,
    '',
    'POST',
    '/v1/communities/$id/invitations',
    {'userAccountId': person},
  );
  factory CommunityAction.decide(
    String id,
    String member, {
    required bool approve,
  }) {
    final op = approve ? 'approve' : 'reject';
    return CommunityAction._(
      op,
      id,
      member,
      '',
      'POST',
      '/v1/communities/$id/requests/$member/$op',
      null,
    );
  }
  factory CommunityAction.role(String id, String member, String role) =>
      CommunityAction._(
        'role',
        id,
        member,
        role,
        'PUT',
        '/v1/communities/$id/members/$member/role',
        {'role': role},
      );
  factory CommunityAction.remove(String id, String member) => CommunityAction._(
    'remove',
    id,
    member,
    '',
    'DELETE',
    '/v1/communities/$id/members/$member',
    null,
  );
  factory CommunityAction.transfer(String id, String person) =>
      CommunityAction._(
        'transfer',
        id,
        person,
        '',
        'POST',
        '/v1/communities/$id/transfer-owner',
        {'userAccountId': person},
      );
  factory CommunityAction.archive(String id) => CommunityAction._(
    'archive',
    id,
    '',
    '',
    'POST',
    '/v1/communities/$id/archive',
    null,
  );
  factory CommunityAction.update(String id, Map<String, dynamic> input) =>
      CommunityAction._(
        'update',
        id,
        '',
        '',
        'PATCH',
        '/v1/communities/$id',
        input,
      );
}

class CommunityApproval {
  const CommunityApproval._(
    this.action,
    this._snapshot,
    this._authorization,
    this.actorId,
    this.expiresAt,
  );
  final CommunityAction action;
  final String _snapshot, _authorization, actorId;
  final DateTime expiresAt;
}
