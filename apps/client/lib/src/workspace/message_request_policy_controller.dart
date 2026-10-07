import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';

const messageRequestLabels = {
  'REQUEST': '由我决定是否接受',
  'SCREEN': '先留待人工审阅',
  'BLOCK': '不接收新的请求',
};
const messageRequestDetails = {
  'REQUEST': '保留普通请求流程；接受前不能聊天。',
  'SCREEN': '仅留待人工审阅，不发送普通申请通知。Agent 筛查尚不可用，不会自动接受或聊天。',
  'BLOCK': '阻止新的请求，不会删除已有关系；现有聊天仍检查屏蔽和原会话权限。',
};
final _id = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
bool _validID(Object? s) =>
    s is String &&
    _id.hasMatch(s) &&
    s != '00000000-0000-0000-0000-000000000000';
DateTime _time(Object? value) {
  if (value is! String) throw const FormatException('Missing time');
  final m = RegExp(
    r'^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,6})?(Z|[+-]\d{2}:\d{2})$',
  ).firstMatch(value);
  if (m == null) throw const FormatException('Invalid time');
  final n = [for (var i = 1; i <= 6; i++) int.parse(m.group(i)!)];
  final t = DateTime.utc(n[0], n[1], n[2], n[3], n[4], n[5]);
  if (n[0] < 1 ||
      t.year != n[0] ||
      t.month != n[1] ||
      t.day != n[2] ||
      t.hour != n[3] ||
      t.minute != n[4] ||
      t.second != n[5]) {
    throw const FormatException('Invalid time');
  }
  final z = m.group(7)!;
  if (z != 'Z' &&
      (int.parse(z.substring(1, 3)) > 23 || int.parse(z.substring(4)) > 59)) {
    throw const FormatException('Invalid zone');
  }
  return DateTime.parse(value).toUtc();
}

@immutable
class MessageRequestPolicy {
  const MessageRequestPolicy._(
    this.ownerID,
    this.agentID,
    this.version,
    this.configured,
    this.status,
    this.incoming,
    this.observedAt,
    this.validFrom,
    this.expiresAt,
  );
  final String ownerID, agentID, status, incoming;
  final int version;
  final bool configured;
  final DateTime observedAt;
  final DateTime? validFrom, expiresAt;
  factory MessageRequestPolicy.read(Object? value, String owner) {
    if (value is! Map<String, dynamic>) {
      throw const FormatException('Missing policy');
    }
    const keys = {
      'schemaVersion',
      'ownerId',
      'agentId',
      'nativeRevision',
      'configured',
      'status',
      'incomingRequests',
      'observedAt',
      'validFrom',
      'expiresAt',
    };
    final v = value['nativeRevision'],
        configured = value['configured'],
        status = value['status'],
        incoming = value['incomingRequests'];
    if (value.keys.any((k) => !keys.contains(k)) ||
        value['schemaVersion'] != 'agent-message-request-policy-v1' ||
        !_validID(owner) ||
        value['ownerId'] != owner ||
        !_validID(value['agentId']) ||
        v is! int ||
        v < 0 ||
        v > 9007199254740991 ||
        configured is! bool ||
        !{'UNCONFIGURED', 'ACTIVE', 'EXPIRED'}.contains(status) ||
        !messageRequestLabels.containsKey(incoming)) {
      throw const FormatException('Invalid policy');
    }
    final observed = _time(value['observedAt']),
        from = value['validFrom'] == null ? null : _time(value['validFrom']),
        end = value['expiresAt'] == null ? null : _time(value['expiresAt']);
    if (!configured) {
      if (v != 0 ||
          status != 'UNCONFIGURED' ||
          incoming != 'REQUEST' ||
          from != null ||
          end != null) {
        throw const FormatException('Invalid unconfigured policy');
      }
    } else {
      if (v < 1 ||
          from == null ||
          end == null ||
          !end.isAfter(from) ||
          end.difference(from) > const Duration(days: 30) ||
          (status == 'ACTIVE') !=
              (!observed.isBefore(from) && observed.isBefore(end))) {
        throw const FormatException('Invalid current policy');
      }
    }
    return MessageRequestPolicy._(
      owner,
      value['agentId'] as String,
      v,
      configured,
      status as String,
      incoming as String,
      observed,
      from,
      end,
    );
  }
}

@immutable
class MessageRequestDraft {
  const MessageRequestDraft(this.incoming, this.expiresAt);
  final String incoming;
  final DateTime? expiresAt;
  bool same(MessageRequestDraft d) =>
      incoming == d.incoming && expiresAt == d.expiresAt;
}

class MessageRequestApproval {
  MessageRequestApproval._(
    this.generation,
    this.sourceEpoch,
    this.ownerID,
    this.agentID,
    this.version,
    this.draft,
  );
  final int generation, sourceEpoch, version;
  final String ownerID, agentID;
  final MessageRequestDraft draft;
}

class MessageRequestPolicyController extends ChangeNotifier {
  MessageRequestPolicyController({
    required this.authorizationHeader,
    required this.accountID,
    this.organizationWorkspaceID,
    this.identityChanges,
    http.Client? client,
    String? apiBaseUrl,
    DateTime Function()? now,
    this.current,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl,
       _now = now ?? DateTime.now {
    identityChanges?.addListener(synchronizeIdentity);
    synchronizeIdentity();
  }
  final String? Function() authorizationHeader, accountID;
  final String? Function()? organizationWorkspaceID;
  final Listenable? identityChanges;
  final bool Function()? current;
  final http.Client _client;
  final bool _ownsClient;
  final String _base;
  final DateTime Function() _now;
  (String?, String?, String?)? _identity;
  int _generation = 0, _request = 0, _sourceEpoch = 0;
  bool _closed = false;
  MessageRequestPolicy? policy;
  MessageRequestDraft? draft;
  MessageRequestApproval? _approval;
  bool loading = false, saving = false, uncertain = false;
  String? error, message;
  int get generation => _generation;
  bool get personal =>
      authorizationHeader() != null &&
      _validID(accountID()) &&
      organizationWorkspaceID?.call() == null &&
      (current?.call() ?? true);
  bool get editable =>
      personal && policy != null && !loading && !saving && !uncertain;
  Uri get _url => Uri.parse(
    '${_base.replaceFirst(RegExp(r'/$'), '')}/v1/me/message-request-policy',
  );
  void _notify() {
    if (!_closed) notifyListeners();
  }

  void synchronizeIdentity() {
    final next = (
      authorizationHeader(),
      accountID(),
      organizationWorkspaceID?.call(),
    );
    if (_identity == next && (current?.call() ?? true)) return;
    _identity = next;
    _generation++;
    _request++;
    _sourceEpoch++;
    _approval = null;
    policy = null;
    draft = null;
    loading = saving = uncertain = false;
    error = message = null;
    _notify();
  }

  bool _active(int g, int serial) {
    synchronizeIdentity();
    return !_closed && g == _generation && serial == _request && personal;
  }

  MessageRequestPolicy _read(http.Response r, String owner) {
    if (r.statusCode != 200) throw _PolicyHTTP(r.statusCode);
    final raw = jsonDecode(r.body);
    if (raw is! Map<String, dynamic> ||
        raw.length != 1 ||
        !raw.containsKey('data')) {
      throw const FormatException('Invalid envelope');
    }
    return MessageRequestPolicy.read(raw['data'], owner);
  }

  String _failure(Object e) => e is _PolicyHTTP
      ? switch (e.status) {
          401 => '登录已失效，请重新登录。',
          403 => '请切回本人身份后重新打开。',
          409 => '设置版本已变化，请重新读取并检查。',
          400 => '请检查请求方式和有效期限。',
          _ => '消息请求设置暂不可用，请重新读取。',
        }
      : '消息请求设置暂不可用，请重新读取。';
  void _adopt(MessageRequestPolicy p) {
    policy = p;
    draft = MessageRequestDraft(
      p.incoming,
      p.status == 'ACTIVE' ? p.expiresAt : null,
    );
    _sourceEpoch++;
    _approval = null;
  }

  Future<void> load() async {
    synchronizeIdentity();
    if (_closed || !personal || loading || saving) return;
    final g = _generation,
        serial = ++_request,
        owner = accountID()!,
        token = authorizationHeader()!;
    final wasUncertain = uncertain;
    loading = true;
    _approval = null;
    _sourceEpoch++;
    error = message = null;
    _notify();
    if (!_active(g, serial)) return;
    try {
      final r = await _client
          .get(_url, headers: {'Authorization': token})
          .timeout(const Duration(seconds: 12));
      if (!_active(g, serial)) return;
      final p = _read(r, owner);
      _adopt(p);
      if (wasUncertain) {
        uncertain = false;
        message = '已读取当前设置，但不能确认上次保存是否成功。需要再次修改时，请重新检查并确认。';
      }
    } catch (e) {
      if (_active(g, serial)) {
        policy = null;
        draft = null;
        _approval = null;
        error = _failure(e);
      }
    } finally {
      if (_active(g, serial)) {
        loading = false;
        _notify();
      }
    }
  }

  void edit(MessageRequestDraft d) {
    synchronizeIdentity();
    if (!editable) return;
    draft = d;
    _sourceEpoch++;
    _approval = null;
    error = message = null;
    _notify();
  }

  MessageRequestApproval? preview() {
    synchronizeIdentity();
    final p = policy, d = draft, at = _now().toUtc();
    if (!editable ||
        p == null ||
        d == null ||
        !messageRequestLabels.containsKey(d.incoming) ||
        d.expiresAt == null ||
        !d.expiresAt!.isAfter(at) ||
        d.expiresAt!.difference(at) > const Duration(days: 30) ||
        d.expiresAt!.year > 9999) {
      return null;
    }
    return _approval = MessageRequestApproval._(
      _generation,
      _sourceEpoch,
      p.ownerID,
      p.agentID,
      p.version,
      d,
    );
  }

  Future<void> save(MessageRequestApproval a) async {
    synchronizeIdentity();
    final p = policy, d = draft, at = _now().toUtc();
    if (!editable ||
        !identical(_approval, a) ||
        a.generation != _generation ||
        a.sourceEpoch != _sourceEpoch ||
        p == null ||
        d == null ||
        p.ownerID != a.ownerID ||
        p.agentID != a.agentID ||
        p.version != a.version ||
        !d.same(a.draft) ||
        d.expiresAt == null ||
        !d.expiresAt!.isAfter(at) ||
        d.expiresAt!.difference(at) > const Duration(days: 30)) {
      return;
    }
    final g = _generation,
        serial = ++_request,
        token = authorizationHeader()!,
        owner = accountID()!;
    final payload = jsonEncode({
      'expectedVersion': a.version,
      'incomingRequests': a.draft.incoming,
      'expiresAt': a.draft.expiresAt!.toUtc().toIso8601String(),
    });
    _approval = null;
    saving = true;
    error = message = null;
    _notify();
    if (!_active(g, serial)) return;
    try {
      final r = await _client
          .put(
            _url,
            headers: {
              'Authorization': token,
              'Content-Type': 'application/json',
            },
            body: payload,
          )
          .timeout(const Duration(seconds: 12));
      if (!_active(g, serial)) return;
      if ({400, 401, 403, 409}.contains(r.statusCode)) {
        throw _PolicyHTTP(r.statusCode);
      }
      final result = _read(r, owner);
      if (result.agentID != a.agentID ||
          result.version != a.version + 1 ||
          result.status != 'ACTIVE' ||
          result.incoming != a.draft.incoming ||
          result.expiresAt != a.draft.expiresAt!.toUtc()) {
        throw const FormatException('Unconfirmed save');
      }
      _adopt(result);
      message = '消息请求设置已保存。';
    } catch (e) {
      if (_active(g, serial)) {
        if (e is _PolicyHTTP && {400, 401, 403, 409}.contains(e.status)) {
          policy = null;
          draft = null;
          error = _failure(e);
          // The original handler revalidates the Session after Store.Put.
          // A late authentication rejection does not prove no commit occurred.
          if (e.status == 401 || e.status == 403) {
            uncertain = true;
            error = '${_failure(e)} 保存结果尚未确认，恢复本人登录后只重新读取当前设置。';
          }
        } else {
          uncertain = true;
          message = '保存结果尚未确认。请只重新读取当前设置，不要重复保存。';
          error = null;
        }
      }
    } finally {
      if (_active(g, serial)) {
        saving = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    _closed = true;
    _generation++;
    _request++;
    _approval = null;
    identityChanges?.removeListener(synchronizeIdentity);
    if (_ownsClient) _client.close();
    super.dispose();
  }
}

class _PolicyHTTP {
  const _PolicyHTTP(this.status);
  final int status;
}
