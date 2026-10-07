import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import '../config/birdtie_environment.dart';

const socialPreferenceChoices = <String>[
  '小群体',
  '大型活动',
  '一对一',
  '同一大学',
  '同一城市',
  '共同兴趣',
  '国际社群',
];
const _listFields = <String>{
  'personalPreferences',
  'socialPreferences',
  'preferredActivityTypes',
  'travelPreferences',
  'interactionPreferences',
  'languagePreferences',
};
const _textFields = <String>{
  'availability',
  'privateCityHistory',
  'agentNotes',
};

bool _fieldsEqual(Map<String, dynamic> a, Map<String, dynamic> b) =>
    a.length == b.length &&
    a.keys.every(
      (key) => a[key] is List
          ? b[key] is List && listEquals(a[key] as List, b[key] as List)
          : a[key] == b[key],
    );

class SocialPreferenceRecord {
  SocialPreferenceRecord._(
    this.ownerID,
    this.agentID,
    this.version,
    this.fields,
  );
  factory SocialPreferenceRecord.fromJson(Map<String, dynamic> raw) {
    final profile = raw['profile'] as Map<String, dynamic>;
    final fields = raw['fields'] as Map<String, dynamic>;
    if (raw['schemaVersion'] != 'private-agent-profile-v1' ||
        profile['ownerType'] != 'PERSON' ||
        profile['ownerId'] is! String ||
        profile['agentId'] is! String ||
        (profile['agentId'] as String).isEmpty ||
        profile['profileVersion'] is! int ||
        (profile['profileVersion'] as int) <= 0 ||
        raw['configured'] is! bool ||
        fields.length != _listFields.length + _textFields.length ||
        !fields.keys.every(
          (k) => _listFields.contains(k) || _textFields.contains(k),
        )) {
      throw const FormatException('Invalid private source');
    }
    final frozen = <String, dynamic>{};
    for (final key in _listFields) {
      final values = List<String>.from(fields[key] as List);
      if (values.length > 20 ||
          values.any((v) => v.isEmpty || v.runes.length > 160)) {
        throw const FormatException('Invalid private list');
      }
      frozen[key] = List<String>.unmodifiable(values);
    }
    for (final key in _textFields) {
      final value = fields[key] as String;
      if (value.runes.length > 2000) {
        throw const FormatException('Invalid private text');
      }
      frozen[key] = value;
    }
    if (raw['configured'] == false &&
        frozen.values.any(
          (value) =>
              value is List ? value.isNotEmpty : (value as String).isNotEmpty,
        )) {
      throw const FormatException('Unconfigured hidden contents');
    }
    return SocialPreferenceRecord._(
      profile['ownerId'] as String,
      profile['agentId'] as String,
      profile['profileVersion'] as int,
      Map<String, dynamic>.unmodifiable(frozen),
    );
  }
  final String ownerID;
  final String agentID;
  final int version;
  final Map<String, dynamic> fields;
  List<String> get preferences => fields['socialPreferences'] as List<String>;
}

// Ordinary self-editing through the existing registered private Profile API.
// A preference, response shape or version is never machine/policy permission.
class SocialPreferenceSeedController extends ChangeNotifier {
  SocialPreferenceSeedController({
    required String? Function() authorizationHeader,
    required String? Function() accountID,
    String? Function()? organizationWorkspaceID,
    http.Client? client,
    String? apiBaseUrl,
  }) : _authorization = authorizationHeader,
       _account = accountID,
       _workspace = organizationWorkspaceID ?? (() => null),
       _client = client ?? http.Client(),
       _ownsClient = client == null,
       _apiBase = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final String? Function() _authorization;
  final String? Function() _account;
  final String? Function() _workspace;
  final http.Client _client;
  final bool _ownsClient;
  final String _apiBase;
  String? _token, _owner, _organization;
  int _epoch = 0;
  bool _closed = false;
  bool busy = false, denied = false, conflict = false;
  String? error, message;
  SocialPreferenceRecord? record;
  Map<String, dynamic>? _expectedFields;
  bool get resultUnknown => _expectedFields != null;
  Uri get _uri => Uri.parse(
    '${_apiBase.replaceFirst(RegExp(r'/$'), '')}/v1/me/agent-private-profile',
  );
  void _notify() {
    if (!_closed) notifyListeners();
  }

  void synchronizeIdentity() {
    final token = _authorization(),
        owner = _account(),
        organization = _workspace();
    if (token == _token && owner == _owner && organization == _organization) {
      return;
    }
    _token = token;
    _owner = owner;
    _organization = organization;
    ++_epoch;
    record = null;
    _expectedFields = null;
    busy = false;
    denied = false;
    conflict = false;
    error = null;
    message = null;
    _notify();
  }

  bool _current(int epoch, String token, String owner) =>
      !_closed &&
      epoch == _epoch &&
      token == _authorization() &&
      owner == _account() &&
      _workspace() == null;
  SocialPreferenceRecord _decode(http.Response response, String owner) {
    final raw =
        jsonDecode(utf8.decode(response.bodyBytes)) as Map<String, dynamic>;
    final value = SocialPreferenceRecord.fromJson(
      raw['data'] as Map<String, dynamic>,
    );
    if (value.ownerID != owner) {
      throw const FormatException('Foreign private source');
    }
    return value;
  }

  void _failure(int code) {
    if (code == 401 || code == 403 || code == 404) {
      denied = true;
      record = null;
      _expectedFields = null;
      error = '当前身份不能编辑这份私密资料。请检查登录状态。';
    } else if (code == 409) {
      conflict = true;
      error = '资料已在别处更新。重新读取后检查草稿，再确认。';
    } else {
      error = code == 400 ? '请检查所选偏好。' : '社交偏好暂不可用，请稍后重试。';
    }
  }

  Future<bool> load() async {
    synchronizeIdentity();
    if (_closed || busy || _workspace() != null) return false;
    final token = _authorization(), owner = _account();
    if (token == null || owner == null || _apiBase.isEmpty) return false;
    final epoch = ++_epoch;
    busy = true;
    error = null;
    message = null;
    _notify();
    try {
      final response = await _client
          .get(_uri, headers: {'Authorization': token})
          .timeout(const Duration(seconds: 12));
      if (!_current(epoch, token, owner)) return false;
      if (response.statusCode != 200) {
        _failure(response.statusCode);
        return false;
      }
      final value = _decode(response, owner);
      record = value;
      denied = false;
      conflict = false;
      final expected = _expectedFields;
      _expectedFields = null;
      if (expected != null) {
        if (_fieldsEqual(value.fields, expected)) {
          message = '已核实：当前私密资料与你提交的内容一致。';
        } else {
          conflict = true;
          error = '当前资料与刚才提交的内容不同，请检查后重新确认。';
        }
      }
      return true;
    } catch (_) {
      if (_current(epoch, token, owner)) error = '无法读取社交偏好，请检查连接并重试。';
      return false;
    } finally {
      if (_current(epoch, token, owner)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<bool> save(
    SocialPreferenceRecord reviewed,
    List<String> selected,
  ) async {
    synchronizeIdentity();
    if (_closed ||
        busy ||
        denied ||
        conflict ||
        resultUnknown ||
        _workspace() != null ||
        !identical(record, reviewed)) {
      return false;
    }
    final token = _authorization(), owner = _account();
    if (token == null ||
        owner == null ||
        reviewed.ownerID != owner ||
        selected.length > 20 ||
        selected.toSet().length != selected.length ||
        selected.any(
          (v) =>
              !(socialPreferenceChoices.contains(v) ||
                  reviewed.preferences.contains(v)),
        )) {
      return false;
    }
    final expected = Map<String, dynamic>.unmodifiable({
      ...reviewed.fields,
      'socialPreferences': List<String>.unmodifiable(selected),
    });
    if (_fieldsEqual(reviewed.fields, expected)) {
      message = '当前社交偏好已与你的选择一致。';
      _notify();
      return true;
    }
    final epoch = ++_epoch;
    busy = true;
    error = null;
    message = null;
    _notify();
    try {
      final response = await _client
          .put(
            _uri,
            headers: {
              'Authorization': token,
              'Content-Type': 'application/json',
            },
            body: jsonEncode({
              'expectedVersion': reviewed.version,
              'fields': expected,
            }),
          )
          .timeout(const Duration(seconds: 12));
      if (!_current(epoch, token, owner)) return false;
      if (response.statusCode != 200) {
        if (response.statusCode >= 500) {
          _expectedFields = expected;
          error = '保存结果尚未确认，请先读取当前资料核实。';
        } else {
          _failure(response.statusCode);
        }
        return false;
      }
      final value = _decode(response, owner);
      if (value.agentID != reviewed.agentID ||
          value.version != reviewed.version + 1 ||
          !_fieldsEqual(value.fields, expected)) {
        throw const FormatException('Unconfirmed private update');
      }
      record = value;
      message = '私密社交偏好已保存。';
      return true;
    } catch (_) {
      if (_current(epoch, token, owner)) {
        _expectedFields = expected;
        error = '保存结果尚未确认，请先读取当前资料核实。';
      }
      return false;
    } finally {
      if (_current(epoch, token, owner)) {
        busy = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    _closed = true;
    ++_epoch;
    record = null;
    _expectedFields = null;
    if (_ownsClient) _client.close();
    super.dispose();
  }
}
