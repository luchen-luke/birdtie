import 'dart:convert';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'entity_action_contract.dart';

class EntityActionFailure implements Exception {
  const EntityActionFailure(this.status);
  final int status;
  String get message => switch (status) {
    401 => '请重新登录后检查可用操作。',
    403 => '请切回个人身份检查可用操作。',
    404 => '该内容当前不可访问。',
    409 => '来源已变化，请重新检查具体操作。',
    _ => '暂时无法核实可用操作，请稍后重新读取。',
  };
}

class EntityActionApi {
  EntityActionApi({http.Client? client, String? apiBaseUrl})
    : _client = client ?? http.Client(),
      _owns = client == null,
      base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final http.Client _client;
  final bool _owns;
  final String base;
  bool _closed = false;
  Future<EntityActionView> read(String? token, EntityActionRef ref) async {
    if (_closed ||
        (token != null && token.trim().isEmpty) ||
        !ref.valid ||
        base.isEmpty ||
        (token == null && !const {'activity', 'place'}.contains(ref.type))) {
      throw const EntityActionFailure(503);
    }
    final response = await _client
        .get(
          Uri.parse(
            '${base.replaceFirst(RegExp(r'/$'), '')}/v1/${token == null ? 'public' : 'me'}/entity-actions/${ref.type}/${ref.id}',
          ),
          headers: {'Authorization': ?token},
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) {
      throw EntityActionFailure(response.statusCode);
    }
    final json = jsonDecode(utf8.decode(response.bodyBytes));
    if (json is! Map<String, dynamic> ||
        json.length != 1 ||
        !json.containsKey('data')) {
      throw const FormatException('动作回执无法读取');
    }
    return EntityActionView.decode(json['data'], ref);
  }

  void dispose() {
    if (_closed) return;
    _closed = true;
    if (_owns) _client.close();
  }
}
