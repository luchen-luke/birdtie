import 'dart:convert';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'model_egress_api.dart' show egressIDValid, egressTime;

const nowContextModes = {
  'CURRENT': '当前声明',
  'DESTINATION': '目的地',
  'PAST': '过去情境',
  'HOME': '家乡声明',
  'ONLINE': '线上情境',
  'INTEREST': '兴趣声明',
  'AFFILIATION': '关联声明',
};
const nowContextTypes = {
  'CITY': '城市',
  'ONLINE': '线上',
  'INSTITUTION': '机构',
  'COMMUNITY': '社群',
  'COUNTRY': '国家',
};
Map<String, dynamic> _map(dynamic v) {
  if (v is! Map<String, dynamic>) throw const FormatException('情境结构无效');
  return v;
}

void _keys(Map<String, dynamic> m, Set<String> keys) {
  if (m.length != keys.length || !keys.containsAll(m.keys)) {
    throw const FormatException('情境字段无效');
  }
}

String _id(dynamic v) {
  if (!egressIDValid(v)) throw const FormatException('情境标识无效');
  return v as String;
}

String _text(dynamic v, {bool empty = false}) {
  if (v is! String ||
      v.trim() != v ||
      (!empty && v.isEmpty) ||
      v.runes.length > 160 ||
      v.contains(RegExp(r'[\r\n\x00]'))) {
    throw const FormatException('情境文本无效');
  }
  return v;
}

String _hex(dynamic v) {
  if (v is! String || !RegExp(r'^[0-9a-f]{64}$').hasMatch(v)) {
    throw const FormatException('选项标识无效');
  }
  return v;
}

const _envelopeKeys = {
  'schemaVersion',
  'owner',
  'agentId',
  'observedAt',
  'expiresAt',
  'viewOnly',
  'modelAccess',
  'sendAllowed',
};

class NowContextOption {
  NowContextOption(Map<String, dynamic> m)
    : optionID = _hex(m['optionId']),
      contextID = _id(m['contextId']),
      contextType = _text(m['contextType']),
      cityID = _text(m['cityId'], empty: true),
      label = _text(m['label']),
      relation = _text(m['relation'], empty: true),
      viewMode = _text(m['viewMode']),
      declared = m['declared'] == true,
      queryRoute = _text(m['queryRoute']) {
    _keys(m, {
      'optionId',
      'contextId',
      'contextType',
      'cityId',
      'label',
      'relation',
      'viewMode',
      'declared',
      'queryRoute',
    });
    if (m['declared'] is! bool ||
        !nowContextTypes.containsKey(contextType) ||
        !nowContextModes.containsKey(viewMode) ||
        !{'CITY', 'ONLINE', 'UNAVAILABLE'}.contains(queryRoute)) {
      throw const FormatException('情境类型无效');
    }
    if (contextType == 'CITY'
        ? cityID.isEmpty || queryRoute != 'CITY'
        : cityID.isNotEmpty) {
      throw const FormatException('城市和情境不相容');
    }
    if (!declared) {
      if (contextType != 'CITY' ||
          relation.isNotEmpty ||
          viewMode != 'DESTINATION') {
        throw const FormatException('不能推断本人声明');
      }
    } else {
      if (!{
        'current',
        'home',
        'past',
        'destination',
        'affiliation',
        'interest',
      }.contains(relation)) {
        throw const FormatException('声明关系无效');
      }
      if (contextType == 'CITY' &&
          (!{'current', 'home', 'past', 'destination'}.contains(relation) ||
              viewMode != relation.toUpperCase())) {
        throw const FormatException('城市关系无效');
      }
      if (contextType == 'ONLINE' &&
          (viewMode != 'ONLINE' ||
              (queryRoute == 'ONLINE' &&
                  !{
                    'interest',
                    'current',
                    'affiliation',
                  }.contains(relation)))) {
        throw const FormatException('线上来源无效');
      }
      if (!{'CITY', 'ONLINE'}.contains(contextType) &&
          (viewMode != relation.toUpperCase() || queryRoute != 'UNAVAILABLE')) {
        throw const FormatException('尚无此查询接口');
      }
    }
  }
  final String optionID,
      contextID,
      contextType,
      cityID,
      label,
      relation,
      viewMode,
      queryRoute;
  final bool declared;
  String get description =>
      '${nowContextModes[viewMode]} · ${nowContextTypes[contextType]} · ${declared ? '本人明确声明' : '公开目的地浏览，不代表所在地'}';
  bool same(NowContextOption b) =>
      optionID == b.optionID &&
      contextID == b.contextID &&
      contextType == b.contextType &&
      cityID == b.cityID &&
      label == b.label &&
      relation == b.relation &&
      viewMode == b.viewMode &&
      declared == b.declared &&
      queryRoute == b.queryRoute;
}

class NowContextEnvelope {
  NowContextEnvelope(Map<String, dynamic> m, String owner)
    : ownerID = _id(owner),
      agentID = _id(m['agentId']),
      observedAt = egressTime(m['observedAt']),
      expiresAt = egressTime(m['expiresAt']) {
    final who = _map(m['owner']);
    _keys(who, {'type', 'id'});
    if (m['schemaVersion'] != 'now-context-selection-v1' ||
        who['type'] != 'PERSON' ||
        _id(who['id']) != owner ||
        m['viewOnly'] != true ||
        m['modelAccess'] != false ||
        m['sendAllowed'] != false ||
        !expiresAt.isAfter(observedAt) ||
        expiresAt.difference(observedAt) > const Duration(seconds: 90)) {
      throw const FormatException('当前主体、期限或查看边界不符');
    }
  }
  final String ownerID, agentID;
  final DateTime observedAt, expiresAt;
}

class NowContextOptions extends NowContextEnvelope {
  NowContextOptions(Map<String, dynamic> m, String owner)
    : optionsToken = m['optionsToken'] as String,
      items = _options(m['items']),
      truncated = m['truncated'] == true,
      super(m, owner) {
    _keys(m, {..._envelopeKeys, 'optionsToken', 'items', 'limit', 'truncated'});
    if (!RegExp(r'^[A-Za-z0-9_-]{80,12000}$').hasMatch(optionsToken) ||
        m['limit'] != 100 ||
        m['truncated'] is! bool) {
      throw const FormatException('情境选项不完整');
    }
  }
  final String optionsToken;
  final List<NowContextOption> items;
  final bool truncated;
  static List<NowContextOption> _options(dynamic v) {
    if (v is! List || v.length > 100) throw const FormatException('情境选项过多');
    final result = v.map((x) => NowContextOption(_map(x))).toList();
    if (result.map((x) => x.optionID).toSet().length != result.length) {
      throw const FormatException('重复选项');
    }
    return List.unmodifiable(result);
  }
}

class NowContextChoice extends NowContextEnvelope {
  NowContextChoice(Map<String, dynamic> m, String owner)
    : option = NowContextOption(_map(m['choice'])),
      super(m, owner) {
    _keys(m, {..._envelopeKeys, 'choice'});
  }
  final NowContextOption option;
  String get contextID => option.contextID;
  String get contextType => option.contextType;
  String get cityID => option.cityID;
  String get queryRoute => option.queryRoute;
  String get label => option.label;
  String get viewMode => option.viewMode;
  String get relation => option.relation;
  bool get declared => option.declared;
}

class NowContextSelectionAPI {
  NowContextSelectionAPI({http.Client? client, String? apiBaseUrl})
    : _client = client ?? http.Client(),
      _owned = client == null,
      _base = (apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl).replaceFirst(
        RegExp(r'/$'),
        '',
      );
  final http.Client _client;
  final bool _owned;
  final String _base;
  bool _closed = false;
  Future<Map<String, dynamic>> _request(
    String method,
    String suffix,
    String auth, [
    Map<String, dynamic>? body,
  ]) async {
    if (_closed) throw StateError('情境读取已关闭');
    final uri = Uri.parse('$_base/v1/me/now/context-selection/$suffix');
    final headers = {'Authorization': auth, 'Content-Type': 'application/json'};
    final r = method == 'GET'
        ? await _client
              .get(uri, headers: headers)
              .timeout(const Duration(seconds: 12))
        : await _client
              .post(uri, headers: headers, body: jsonEncode(body))
              .timeout(const Duration(seconds: 12));
    if (_closed) throw StateError('旧运输端已关闭');
    if (r.statusCode != 200) throw const FormatException('当前情境或会话无法核验');
    final m = _map(jsonDecode(r.body));
    _keys(m, {'data'});
    return _map(m['data']);
  }

  Future<NowContextOptions> options(String auth, String owner) async =>
      NowContextOptions(await _request('GET', 'options', auth), owner);
  Future<NowContextChoice> resolve(
    String auth,
    String owner,
    NowContextOptions options,
    NowContextOption selected,
  ) async {
    if (options.ownerID != owner ||
        !options.items.any((x) => x.same(selected))) {
      throw const FormatException('未选定当前本人选项');
    }
    final v = NowContextChoice(
      await _request('POST', 'resolve', auth, {
        'optionsToken': options.optionsToken,
        'optionId': selected.optionID,
      }),
      owner,
    );
    if (!v.option.same(selected) ||
        v.agentID != options.agentID ||
        v.observedAt.isBefore(options.observedAt) ||
        !v.observedAt.isBefore(options.expiresAt) ||
        v.expiresAt != options.expiresAt) {
      throw const FormatException('返回选择与本轮具体来源不符');
    }
    return v;
  }

  void dispose() {
    if (_closed) return;
    _closed = true;
    if (_owned) _client.close();
  }
}
