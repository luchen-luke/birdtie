import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import '../config/birdtie_environment.dart';

const seedIntentLabels = <String, String>{
  'FIND_PEOPLE': '认识新朋友',
  'FIND_ACTIVITIES': '找活动',
  'EXPLORE_CITY': '探索城市',
  'SIMILAR_INTERESTS': '认识兴趣相近的人',
  'JOIN_COMMUNITIES': '加入社群',
  'DISCOVER_PLACES': '发现地点',
  'JUST_EXPLORE': '先随便看看',
};

class SeedCity {
  const SeedCity(this.id, this.name, this.snapshot);
  final String id;
  final String name;
  final String snapshot;
}

class AgentSeedRecord {
  AgentSeedRecord._(Map<String, dynamic> raw)
    : ownerID = raw['ownerId'] as String,
      snapshot = raw['snapshot'] as String,
      displayName = raw['displayName'] as String,
      visibility = raw['profileVisibility'] as String,
      currentCityID =
          (raw['currentCity'] as Map<String, dynamic>?)?['id'] as String?,
      cities = List.unmodifiable([
        for (final city in raw['cities'] as List<dynamic>)
          SeedCity(
            city['id'] as String,
            city['name'] as String,
            city['sourceSnapshot'] as String,
          ),
      ]),
      languages = List<String>.unmodifiable(raw['languagePreferences'] as List),
      interests = List<String>.unmodifiable(raw['interests'] as List),
      basicIntent = raw['userIntent']['basicIntent'] as String,
      progress = raw['userIntent']['progress'] as String,
      interestChoice = raw['userIntent']['interestChoice'] as String,
      needsPrompt = raw['needsPrompt'] as bool;

  factory AgentSeedRecord.fromJson(Map<String, dynamic> raw) {
    if (raw['schemaVersion'] != 'personal-agent-seed-v1' ||
        raw['ownerId'] is! String ||
        raw['snapshot'] is! String ||
        !RegExp(r'^[a-f0-9]{64}$').hasMatch(raw['snapshot'] as String)) {
      throw const FormatException('Invalid seed source');
    }
    final record = AgentSeedRecord._(raw);
    if (!const ['public', 'private'].contains(record.visibility) ||
        !const ['UNSET', 'DEFERRED', 'COMPLETED'].contains(record.progress) ||
        (record.basicIntent.isNotEmpty &&
            !seedIntentLabels.containsKey(record.basicIntent))) {
      throw const FormatException('Invalid seed state');
    }
    return record;
  }

  final String ownerID;
  final String snapshot;
  final String displayName;
  final String visibility;
  final String? currentCityID;
  final List<SeedCity> cities;
  final List<String> languages;
  final List<String> interests;
  final String basicIntent;
  final String progress;
  final String interestChoice;
  final bool needsPrompt;
}

class AgentSeedController extends ChangeNotifier {
  AgentSeedController({
    required String? Function() authorizationHeader,
    required this.accountID,
    String? Function()? organizationWorkspaceID,
    http.Client? client,
    String? apiBaseUrl,
  }) : _authorization = authorizationHeader,
       _organizationWorkspace = organizationWorkspaceID ?? (() => null),
       _client = client ?? http.Client(),
       _ownsClient = client == null,
       _apiBase = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;

  final String? Function() _authorization;
  final String? Function() accountID;
  final String? Function() _organizationWorkspace;
  final http.Client _client;
  final bool _ownsClient;
  final String _apiBase;
  String? _observedToken;
  String? _observedOwner;
  String? _observedWorkspace;
  int _serial = 0;
  bool _closed = false;
  bool busy = false;
  bool denied = false;
  bool conflict = false;
  String? error;
  String? message;
  AgentSeedRecord? record;
  Map<String, dynamic>? _unknownInput;
  bool get resultUnknown => _unknownInput != null;
  Uri get _uri =>
      Uri.parse('${_apiBase.replaceFirst(RegExp(r'/$'), '')}/v1/me/agent-seed');

  void synchronizeIdentity() {
    if (_closed) return;
    final token = _authorization();
    final owner = accountID();
    final workspace = _organizationWorkspace();
    if (token == _observedToken &&
        owner == _observedOwner &&
        workspace == _observedWorkspace) {
      return;
    }
    _observedToken = token;
    _observedOwner = owner;
    _observedWorkspace = workspace;
    ++_serial;
    record = null;
    _unknownInput = null;
    busy = false;
    denied = false;
    conflict = false;
    error = null;
    message = null;
    _notify();
  }

  bool _current(int serial, String token, String owner) =>
      !_closed &&
      serial == _serial &&
      _authorization() == token &&
      accountID() == owner &&
      _organizationWorkspace() == null;

  void _notify() {
    if (!_closed) notifyListeners();
  }

  AgentSeedRecord _decode(http.Response response, String owner) {
    final raw =
        jsonDecode(utf8.decode(response.bodyBytes)) as Map<String, dynamic>;
    final value = AgentSeedRecord.fromJson(raw['data'] as Map<String, dynamic>);
    if (value.ownerID != owner) throw const FormatException('Foreign seed');
    return value;
  }

  void _failure(int code) {
    if (code == 401 || code == 403) {
      denied = true;
      record = null;
      _unknownInput = null;
      error = '当前登录或本人资料已失效，请重新登录。';
    } else if (code == 409) {
      conflict = true;
      error = '资料已在别处更新。重新读取后检查草稿，再确认保存。';
    } else {
      error = code == 400 ? '请检查昵称、城市和所选内容。' : '初始设置暂不可用，请稍后重试。';
    }
  }

  Future<bool> load() async {
    synchronizeIdentity();
    if (_closed || busy || _organizationWorkspace() != null) return false;
    final token = _authorization();
    final owner = accountID();
    if (token == null || owner == null || _apiBase.isEmpty) return false;
    final serial = ++_serial;
    busy = true;
    error = null;
    message = null;
    _notify();
    try {
      final response = await _client
          .get(_uri, headers: {'Authorization': token})
          .timeout(const Duration(seconds: 12));
      if (!_current(serial, token, owner)) return false;
      if (response.statusCode != 200) {
        _failure(response.statusCode);
        return false;
      }
      final value = _decode(response, owner);
      record = value;
      denied = false;
      conflict = false;
      final pending = _unknownInput;
      if (pending != null) {
        final matches = pending['action'] == 'DEFER'
            ? value.progress == 'DEFERRED'
            : value.progress == 'COMPLETED' &&
                  value.displayName == pending['displayName'] &&
                  value.currentCityID == pending['currentCityId'] &&
                  value.basicIntent == pending['basicIntent'] &&
                  listEquals(
                    value.languages,
                    pending['languagePreferences'] as List,
                  ) &&
                  listEquals(
                    value.interests,
                    pending['_expectedInterests'] as List,
                  );
        _unknownInput = null;
        if (matches) {
          message = '已核实：当前设置与你提交的内容一致。';
        } else {
          conflict = true;
          error = '当前设置与刚才提交的内容不同，请检查后重新确认。';
        }
      }
      return true;
    } catch (_) {
      if (_current(serial, token, owner)) error = '无法读取初始设置，请检查连接并重试。';
      return false;
    } finally {
      if (_current(serial, token, owner)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<bool> save(
    AgentSeedRecord reviewed,
    Map<String, dynamic> choices,
  ) async {
    synchronizeIdentity();
    if (_closed ||
        busy ||
        _organizationWorkspace() != null ||
        resultUnknown ||
        conflict ||
        !identical(record, reviewed)) {
      return false;
    }
    const keys = {
      'action',
      'displayName',
      'currentCityId',
      'currentCitySnapshot',
      'languagePreferences',
      'basicIntent',
      'interestChoice',
      'interests',
    };
    if (choices.keys.any((key) => !keys.contains(key))) return false;
    final token = _authorization();
    final owner = accountID();
    if (token == null || owner == null || reviewed.ownerID != owner) {
      return false;
    }
    final payload = <String, dynamic>{
      'expectedSnapshot': reviewed.snapshot,
      ...choices,
    };
    // Freeze the reviewed lists before the request and unknown-result recovery.
    for (final key in ['languagePreferences', 'interests']) {
      if (payload[key] is List) {
        payload[key] = List<dynamic>.from(payload[key] as List);
      }
    }
    final expected = choices['interestChoice'] == 'SET'
        ? List<String>.from(choices['interests'] as List)
        : List<String>.from(reviewed.interests);
    final serial = ++_serial;
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
            body: jsonEncode(payload),
          )
          .timeout(const Duration(seconds: 12));
      if (!_current(serial, token, owner)) return false;
      if (response.statusCode != 200) {
        if (response.statusCode >= 500) {
          _unknownInput = {...payload, '_expectedInterests': expected};
          error = '保存结果尚未确认，请先读取当前设置核实。';
          return false;
        }
        _failure(response.statusCode);
        return false;
      }
      record = _decode(response, owner);
      message = choices['action'] == 'DEFER' ? '已记下：稍后再完善。' : '初始设置已保存。';
      return true;
    } catch (_) {
      if (_current(serial, token, owner)) {
        _unknownInput = {...payload, '_expectedInterests': expected};
        error = '保存结果尚未确认，请先读取当前设置核实。';
      }
      return false;
    } finally {
      if (_current(serial, token, owner)) {
        busy = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    if (_closed) return;
    _closed = true;
    ++_serial;
    record = null;
    _unknownInput = null;
    if (_ownsClient) _client.close();
    super.dispose();
  }
}
