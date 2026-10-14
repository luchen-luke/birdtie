import 'dart:convert';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'activity_participation_operation.dart' show participationWireJSON;

final _id = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
bool closePersonIDValid(Object? v) =>
    v is String &&
    _id.hasMatch(v) &&
    v != '00000000-0000-0000-0000-000000000000';

Map<String, dynamic> _map(Object? value) {
  if (value is! Map<String, dynamic>) throw const FormatException('亲密好友格式无法核实');
  return value;
}

void _keys(Map<String, dynamic> v, Set<String> names) {
  if (v.keys.length != names.length || !v.keys.toSet().containsAll(names)) {
    throw const FormatException('亲密好友字段无法核实');
  }
}

String _uuid(Object? value) {
  if (!closePersonIDValid(value)) throw const FormatException('亲密好友身份无法核实');
  return value as String;
}

DateTime _stamp(Object? v) {
  if (v is! String) throw const FormatException('亲密好友时间无法核实');
  final m = RegExp(
    r'^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,6})?Z$',
  ).firstMatch(v);
  if (m == null) throw const FormatException('亲密好友时间格式无法核实');
  final n = [for (var i = 1; i <= 6; i++) int.parse(m.group(i)!)];
  final d = DateTime.utc(n[0], n[1], n[2], n[3], n[4], n[5]);
  if (n[0] < 1 ||
      d.year != n[0] ||
      d.month != n[1] ||
      d.day != n[2] ||
      d.hour != n[3] ||
      d.minute != n[4] ||
      d.second != n[5]) {
    throw const FormatException('亲密好友时间无效');
  }
  return DateTime.parse(v).toUtc();
}

class CloseConnection {
  CloseConnection.decode(Object? raw) {
    final v = _map(raw);
    _keys(v, {
      'personId',
      'tieId',
      'requestId',
      'isClose',
      'nativeRevision',
      'sourceVersion',
      'displayName',
    });
    personID = _uuid(v['personId']);
    tieID = _uuid(v['tieId']);
    requestID = _uuid(v['requestId']);
    if (v['isClose'] is! bool ||
        v['nativeRevision'] is! int ||
        (v['nativeRevision'] as int) < 0 ||
        (v['isClose'] == true && (v['nativeRevision'] as int) < 1) ||
        v['sourceVersion'] is! String ||
        !RegExp(r'^[a-f0-9]{64}$').hasMatch(v['sourceVersion'] as String) ||
        v['displayName'] is! String ||
        (v['displayName'] as String).trim().isEmpty ||
        (v['displayName'] as String).runes.length > 80) {
      throw const FormatException('亲密好友关系无法核实');
    }
    isClose = v['isClose'] as bool;
    revision = v['nativeRevision'] as int;
    sourceVersion = v['sourceVersion'] as String;
    displayName = v['displayName'] as String;
  }
  late final String personID, tieID, requestID, sourceVersion, displayName;
  late final bool isClose;
  late final int revision;
}

class CloseConnectionsEnvelope {
  CloseConnectionsEnvelope(Map<String, dynamic> v, String owner, DateTime now) {
    if (v['schemaVersion'] != 'close-connections-v1' ||
        _uuid(v['ownerId']) != owner ||
        v['modelAccess'] != false) {
      throw const FormatException('亲密好友主体或权限无法核实');
    }
    observedAt = _stamp(v['observedAt']);
    validUntil = _stamp(v['validUntil']);
    if (!validUntil.isAfter(observedAt) ||
        validUntil.difference(observedAt) > const Duration(seconds: 30) ||
        observedAt.isAfter(now.toUtc().add(const Duration(seconds: 5)))) {
      throw const FormatException('亲密好友来源期限无法核实');
    }
  }
  late final DateTime observedAt, validUntil;
}

class CloseConnectionsList extends CloseConnectionsEnvelope {
  CloseConnectionsList.decode(
    Map<String, dynamic> v,
    String owner,
    DateTime now,
  ) : super(v, owner, now) {
    _keys(v, {
      'schemaVersion',
      'ownerId',
      'observedAt',
      'validUntil',
      'connections',
      'limit',
      'truncated',
      'modelAccess',
    });
    if (v['limit'] != 100 ||
        v['truncated'] is! bool ||
        v['connections'] is! List ||
        (v['connections'] as List).length > 100 ||
        v['truncated'] == true && (v['connections'] as List).length != 100) {
      throw const FormatException('亲密好友名单无法核实');
    }
    connections = List.unmodifiable(
      (v['connections'] as List).map(CloseConnection.decode),
    );
    if (connections.any((v) => v.personID == owner) ||
        connections.map((v) => v.personID).toSet().length !=
            connections.length ||
        connections.map((v) => v.tieID).toSet().length != connections.length) {
      throw const FormatException('亲密好友名单有重复或身份不符');
    }
    truncated = v['truncated'] as bool;
  }
  late final List<CloseConnection> connections;
  late final bool truncated;
}

class CloseConnectionReceipt extends CloseConnectionsEnvelope {
  CloseConnectionReceipt.decode(
    Map<String, dynamic> v,
    String owner,
    DateTime now,
  ) : super(v, owner, now) {
    _keys(v, {
      'schemaVersion',
      'ownerId',
      'observedAt',
      'validUntil',
      'connection',
      'modelAccess',
    });
    connection = CloseConnection.decode(v['connection']);
    if (connection.personID == owner || !connection.isClose) {
      throw const FormatException('亲密好友回执关系无法核实');
    }
  }
  late final CloseConnection connection;
}

class CloseConnectionsException implements Exception {
  const CloseConnectionsException(this.status);
  final int status;
}

class CloseConnectionsAPI {
  CloseConnectionsAPI({
    http.Client? client,
    String? apiBaseUrl,
    DateTime Function()? now,
  }) : _client = client ?? http.Client(),
       _owns = client == null,
       _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl,
       now = now ?? DateTime.now;
  final http.Client _client;
  final bool _owns;
  final String _base;
  final DateTime Function() now;
  bool _closed = false;
  Uri _uri([String? person]) => Uri.parse(
    '${_base.replaceFirst(RegExp(r'/$'), '')}/v1/me/close-connections${person == null ? '' : '/$person'}',
  );
  Map<String, String> _headers(String auth) => {
    'Authorization': auth,
    'Content-Type': 'application/json',
  };
  void _ready(String auth, String owner) {
    if (_closed ||
        _base.isEmpty ||
        auth.isEmpty ||
        !closePersonIDValid(owner)) {
      throw const FormatException('请重新打开当前个人账号的亲密好友管理');
    }
  }

  Map<String, dynamic> _data(http.Response r) {
    if (r.statusCode != 200) throw CloseConnectionsException(r.statusCode);
    if (r.bodyBytes.length > 256 * 1024) {
      throw const FormatException('亲密好友结果过大');
    }
    final outer = _map(participationWireJSON(utf8.decode(r.bodyBytes)));
    _keys(outer, {'data'});
    return _map(outer['data']);
  }

  Future<CloseConnectionsList> list(String auth, String owner) async {
    _ready(auth, owner);
    final response = await _client
        .get(_uri(), headers: _headers(auth))
        .timeout(const Duration(seconds: 12));
    return CloseConnectionsList.decode(_data(response), owner, now());
  }

  Future<CloseConnectionReceipt> add(
    String auth,
    String owner,
    CloseConnection entry,
    DateTime deadline,
  ) async {
    _ready(auth, owner);
    if (!deadline.isAfter(now().toUtc())) {
      throw const FormatException('当前关系已到核验期限，请刷新');
    }
    final response = await _client
        .put(
          _uri(entry.personID),
          headers: _headers(auth),
          body: jsonEncode({
            'tieId': entry.tieID,
            'requestId': entry.requestID,
            'sourceVersion': entry.sourceVersion,
            'validUntil': deadline.toUtc().toIso8601String(),
            'confirmed': true,
          }),
        )
        .timeout(const Duration(seconds: 12));
    final receipt = CloseConnectionReceipt.decode(
      _data(response),
      owner,
      now(),
    );
    final current = receipt.connection;
    if (current.personID != entry.personID ||
        current.tieID != entry.tieID ||
        current.requestID != entry.requestID ||
        !current.isClose ||
        current.revision < entry.revision) {
      throw const FormatException('亲密好友保存结果与批准对象不符');
    }
    return receipt;
  }

  Future<void> remove(String auth, String owner, CloseConnection entry) async {
    _ready(auth, owner);
    final response = await _client
        .delete(
          _uri(entry.personID),
          headers: _headers(auth),
          body: jsonEncode({
            'tieId': entry.tieID,
            'requestId': entry.requestID,
            'expectedRevision': entry.revision,
            'confirmed': true,
          }),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 204) {
      throw CloseConnectionsException(response.statusCode);
    }
  }

  void dispose() {
    if (_closed) return;
    _closed = true;
    if (_owns) _client.close();
  }
}
