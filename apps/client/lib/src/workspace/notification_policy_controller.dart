import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';

const notificationRoutes = ['IMMEDIATE', 'NORMAL', 'DIGEST', 'SILENT', 'BLOCK'];
const notificationCategories = [
  'ACTIVITY',
  'AGENT',
  'BUSINESS',
  'COMMUNITY',
  'MESSAGE',
  'ORGANIZATION',
  'SOCIAL',
  'SYSTEM',
];
const notificationRouteLabels = {
  'IMMEDIATE': '优先提醒',
  'NORMAL': '普通提醒',
  'DIGEST': '待汇总（尚未投递）',
  'SILENT': '静默',
  'BLOCK': '不接收',
};
const notificationCategoryLabels = {
  'ACTIVITY': '活动与匹配机会',
  'AGENT': '任务状态',
  'BUSINESS': '商家（当前无独立来源）',
  'COMMUNITY': '社群消息',
  'MESSAGE': '会话消息',
  'ORGANIZATION': '组织邀请与成员状态',
  'SOCIAL': '联系申请',
  'SYSTEM': '系统审核',
};
final _notificationID = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
DateTime _stamp(dynamic value) {
  if (value is! String || !RegExp(r'(Z|[+-]\d{2}:\d{2})$').hasMatch(value)) {
    throw const FormatException('Missing timezone');
  }
  final t = DateTime.parse(value).toUtc();
  if (t.year < 1 || t.year > 9999) {
    throw const FormatException('Invalid time');
  }
  return t;
}

class NotificationPolicy {
  NotificationPolicy({
    required this.version,
    required this.agentID,
    required this.enabled,
    required this.defaultRoute,
    required Map<String, String> rules,
    this.pauseUntil,
    this.expiresAt,
    this.updatedAt,
  }) : rules = Map.unmodifiable(rules);
  final int version;
  final String agentID, defaultRoute;
  final bool enabled;
  final Map<String, String> rules;
  final DateTime? pauseUntil, expiresAt, updatedAt;
  factory NotificationPolicy.read(dynamic input) {
    final m = input as Map<String, dynamic>;
    final v = m['version'];
    final id = m['agentId'];
    if (m['schemaVersion'] != 'agent-notification-policy-v1' ||
        v is! int ||
        v < 0 ||
        v > 9007199254740991 ||
        id is! String ||
        !_notificationID.hasMatch(id) ||
        id == '00000000-0000-0000-0000-000000000000' ||
        m['enabled'] is! bool ||
        !notificationRoutes.contains(m['defaultRoute'])) {
      throw const FormatException('Unknown policy');
    }
    final raw = m['rules'] as List;
    if (raw.length > 8) {
      throw const FormatException('Too many rules');
    }
    final rules = <String, String>{};
    String? previous;
    for (final item in raw) {
      final r = item as Map<String, dynamic>;
      final c = r['category'];
      final route = r['route'];
      if (r.length != 2 ||
          c is! String ||
          !notificationCategories.contains(c) ||
          !notificationRoutes.contains(route) ||
          rules.containsKey(c) ||
          (previous != null && previous.compareTo(c) >= 0)) {
        throw const FormatException('Unknown rule');
      }
      rules[c] = route as String;
      previous = c;
    }
    final pause = m['pauseUntil'] == null ? null : _stamp(m['pauseUntil']);
    final expires = m['expiresAt'] == null ? null : _stamp(m['expiresAt']);
    final updated = m['updatedAt'] == null ? null : _stamp(m['updatedAt']);
    if (v == 0) {
      if (m['enabled'] ||
          m['defaultRoute'] != 'NORMAL' ||
          rules.isNotEmpty ||
          pause != null ||
          expires != null ||
          updated != null) {
        throw const FormatException('Invalid default');
      }
    } else if (expires == null ||
        updated == null ||
        !expires.isAfter(updated) ||
        expires.difference(updated) > const Duration(hours: 720) ||
        (pause != null && pause.isAfter(expires))) {
      throw const FormatException('Invalid policy deadline');
    }
    return NotificationPolicy(
      version: v,
      agentID: id,
      enabled: m['enabled'] as bool,
      defaultRoute: m['defaultRoute'] as String,
      rules: rules,
      pauseUntil: pause,
      expiresAt: expires,
      updatedAt: updated,
    );
  }
}

class NotificationPolicyDraft {
  NotificationPolicyDraft({
    required this.enabled,
    required this.defaultRoute,
    required Map<String, String> rules,
    required this.expiresAt,
    this.pauseUntil,
  }) : rules = Map.unmodifiable(rules);
  final bool enabled;
  final String defaultRoute;
  final Map<String, String> rules;
  final DateTime expiresAt;
  final DateTime? pauseUntil;
  Map<String, dynamic> wire(int version) => {
    'expectedVersion': version,
    'enabled': enabled,
    'defaultRoute': defaultRoute,
    'rules': [
      for (final c in notificationCategories)
        if (rules.containsKey(c)) {'category': c, 'route': rules[c]},
    ],
    if (pauseUntil != null) 'pauseUntil': pauseUntil!.toUtc().toIso8601String(),
    'expiresAt': expiresAt.toUtc().toIso8601String(),
  };
  bool same(NotificationPolicy p) =>
      p.enabled == enabled &&
      p.defaultRoute == defaultRoute &&
      mapEquals(p.rules, rules) &&
      p.expiresAt == expiresAt.toUtc() &&
      p.pauseUntil == pauseUntil?.toUtc();
}

class NotificationPolicyApproval {
  NotificationPolicyApproval._(
    this.generation,
    this.version,
    this.agentID,
    this.draft,
  );
  final int generation, version;
  final String agentID;
  final NotificationPolicyDraft draft;
}

/// Offline UI shape checks cannot authorize; the existing native API owns CAS,
/// actor/session/source and current-clock eligibility.
class NotificationPolicyController extends ChangeNotifier {
  NotificationPolicyController({
    required this.authorizationHeader,
    required this.accountID,
    this.organizationWorkspaceID,
    this.current,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final String? Function() authorizationHeader, accountID;
  final String? Function()? organizationWorkspaceID;
  final bool Function()? current;
  final http.Client _client;
  final bool _ownsClient;
  final String _base;
  (String?, String?, String?)? _identity;
  int _generation = 0, _request = 0;
  bool _closed = false;
  NotificationPolicy? policy;
  NotificationPolicyDraft? draft;
  bool loading = false, saving = false, uncertain = false;
  String? error, message;
  int get generation => _generation;
  bool get personal =>
      current?.call() != false &&
      authorizationHeader() != null &&
      accountID() != null &&
      organizationWorkspaceID?.call() == null;
  void _notify() {
    if (!_closed) {
      notifyListeners();
    }
  }

  void synchronizeIdentity() {
    final next = (
      authorizationHeader(),
      accountID(),
      organizationWorkspaceID?.call(),
    );
    if (_identity == next) {
      return;
    }
    _identity = next;
    _generation++;
    _request++;
    policy = null;
    draft = null;
    loading = false;
    saving = false;
    uncertain = false;
    error = null;
    message = null;
    _notify();
  }

  bool _current(int g, int serial) {
    synchronizeIdentity();
    return !_closed && g == _generation && serial == _request && personal;
  }

  Uri get _url => Uri.parse(
    '${_base.replaceFirst(RegExp(r'/$'), '')}/v1/me/notification-policy',
  );
  Map<String, String> _headers() => {
    'Authorization': authorizationHeader()!,
    'Content-Type': 'application/json',
  };
  NotificationPolicy _read(http.Response r) {
    if (r.statusCode != 200) {
      throw _PolicyHTTP(r.statusCode);
    }
    return NotificationPolicy.read(
      (jsonDecode(r.body) as Map<String, dynamic>)['data'],
    );
  }

  String _failure(Object e) => e is _PolicyHTTP
      ? switch (e.status) {
          401 => '登录已失效，请重新登录。',
          403 => '请切回本人账号，或重新确认当前权限。',
          404 => '当前个人通知设置不可用，请稍后重试。',
          409 => '设置版本已更新，请重新读取。',
          _ => '通知设置暂不可用，请重试。',
        }
      : '通知设置暂不可用，请重试。';
  void _adopt(NotificationPolicy p) {
    policy = p;
    draft = NotificationPolicyDraft(
      enabled: p.enabled,
      defaultRoute: p.defaultRoute,
      rules: p.rules,
      expiresAt:
          p.expiresAt ?? DateTime.now().toUtc().add(const Duration(days: 7)),
      pauseUntil: p.pauseUntil,
    );
  }

  Future<void> load() async {
    synchronizeIdentity();
    if (!personal) {
      error = '请登录本人账号管理通知设置。';
      _notify();
      return;
    }
    final g = _generation, serial = ++_request;
    final headers = Map<String, String>.unmodifiable(_headers());
    final wasUncertain = uncertain;
    loading = true;
    error = null;
    message = null;
    _notify();
    if (!_current(g, serial)) return;
    try {
      final p = _read(
        await _client
            .get(_url, headers: headers)
            .timeout(const Duration(seconds: 10)),
      );
      if (!_current(g, serial)) {
        return;
      }
      _adopt(p);
      uncertain = false;
      if (wasUncertain) {
        message = '已读取当前设置，但不能确认上次保存是否成功。';
      }
    } catch (e) {
      if (!_current(g, serial)) {
        return;
      }
      policy = null;
      draft = null;
      error = _failure(e);
    } finally {
      if (_current(g, serial)) {
        loading = false;
        _notify();
      }
    }
  }

  void edit(NotificationPolicyDraft value) {
    synchronizeIdentity();
    if (!personal || policy == null || saving || loading || uncertain) {
      return;
    }
    draft = value;
    error = null;
    message = null;
    _notify();
  }

  NotificationPolicyApproval? preview() {
    synchronizeIdentity();
    final p = policy, d = draft;
    if (!personal ||
        p == null ||
        d == null ||
        saving ||
        loading ||
        uncertain ||
        !notificationRoutes.contains(d.defaultRoute) ||
        d.rules.keys.any((c) => !notificationCategories.contains(c)) ||
        d.rules.values.any((r) => !notificationRoutes.contains(r)) ||
        !d.expiresAt.isAfter(DateTime.now().toUtc()) ||
        (d.pauseUntil != null && d.pauseUntil!.isAfter(d.expiresAt))) {
      return null;
    }
    return NotificationPolicyApproval._(_generation, p.version, p.agentID, d);
  }

  Future<void> save(NotificationPolicyApproval approved) async {
    synchronizeIdentity();
    if (!personal ||
        saving ||
        loading ||
        uncertain ||
        approved.generation != _generation ||
        approved.version != policy?.version ||
        approved.agentID != policy?.agentID ||
        !identical(approved.draft, draft)) {
      return;
    }
    final g = _generation, serial = ++_request;
    final headers = Map<String, String>.unmodifiable(_headers());
    saving = true;
    error = null;
    message = null;
    _notify();
    if (!_current(g, serial)) return;
    try {
      final response = await _client
          .put(
            _url,
            headers: headers,
            body: jsonEncode(approved.draft.wire(approved.version)),
          )
          .timeout(const Duration(seconds: 10));
      if (!_current(g, serial)) {
        return;
      }
      if (response.statusCode == 409) {
        final p = _read(
          await _client
              .get(_url, headers: headers)
              .timeout(const Duration(seconds: 10)),
        );
        if (!_current(g, serial)) {
          return;
        }
        _adopt(p);
        error = '设置已被更新。请检查当前版本后重新确认，旧草稿未覆盖它。';
        return;
      }
      if (response.statusCode >= 500) {
        throw const _PolicyUnknown();
      }
      final p = _read(response);
      if (p.version != approved.version + 1 ||
          p.agentID != approved.agentID ||
          !approved.draft.same(p)) {
        throw const _PolicyUnknown();
      }
      _adopt(p);
      message = '通知设置已保存。';
    } catch (e) {
      if (!_current(g, serial)) {
        return;
      }
      if (e is _PolicyHTTP && e.status < 500 && e.status != 408) {
        error = _failure(e);
        if (e.status == 401 || e.status == 403 || e.status == 404) {
          policy = null;
          draft = null;
        }
        // The original HTTP response rechecks the Session after Store.Put.
        if (e.status == 401 || e.status == 403) {
          uncertain = true;
          error = '${_failure(e)} 保存结果尚未确认，恢复本人访问后只重新读取当前设置。';
        }
      } else {
        uncertain = true;
        error = '提交结果尚未确定，正在读取当前设置，请勿重复提交。';
        _notify();
        if (!_current(g, serial)) return;
        try {
          final p = _read(
            await _client
                .get(_url, headers: headers)
                .timeout(const Duration(seconds: 10)),
          );
          if (!_current(g, serial)) {
            return;
          }
          _adopt(p);
          if (p.agentID == approved.agentID &&
              p.version == approved.version + 1 &&
              approved.draft.same(p)) {
            uncertain = false;
            error = null;
            message = '已核实当前设置与确认内容一致。';
          } else {
            error = '已读回当前设置，但无法确定刚才提交的结果。请稍后重新读取并检查。';
          }
        } catch (_) {
          if (_current(g, serial)) {
            error = '提交结果未知，当前设置也暂不可读。请稍后重新读取。';
          }
        }
      }
    } finally {
      if (_current(g, serial)) {
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
    if (_ownsClient) {
      _client.close();
    }
    super.dispose();
  }
}

class _PolicyHTTP implements Exception {
  const _PolicyHTTP(this.status);
  final int status;
}

class _PolicyUnknown implements Exception {
  const _PolicyUnknown();
}
