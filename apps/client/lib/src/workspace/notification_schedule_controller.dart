import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'notification_policy_controller.dart' show notificationCategories;

const notificationScheduleSchema = 'native-notification-schedule-v1';
const notificationScheduleStatuses = {
  'UNCONFIGURED': '尚未设置',
  'ACTIVE': '已启用',
  'DISABLED': '已关闭',
  'EXPIRED': '已过期',
  'PAUSED_SESSION': '原会话已失效，需要本人重新保存',
};
final _scheduleID = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
int _integer(Object? v, int min, int max) {
  if (v is! int || v < min || v > max) {
    throw const FormatException('Invalid integer');
  }
  return v;
}

DateTime _time(Object? v) {
  if (v is! String) throw const FormatException('Missing absolute time');
  final r = RegExp(
    r'^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,6})?(Z|[+-]\d{2}:\d{2})$',
  ).firstMatch(v);
  if (r == null) throw const FormatException('Invalid absolute time');
  final n = [for (var i = 1; i <= 6; i++) int.parse(r.group(i)!)];
  final check = DateTime.utc(n[0], n[1], n[2], n[3], n[4], n[5]);
  if (n[0] < 1 ||
      check.year != n[0] ||
      check.month != n[1] ||
      check.day != n[2] ||
      check.hour != n[3] ||
      check.minute != n[4] ||
      check.second != n[5]) {
    throw const FormatException('Normalized invalid time');
  }
  final zone = r.group(7)!;
  if (zone != 'Z' &&
      (int.parse(zone.substring(1, 3)) > 23 ||
          int.parse(zone.substring(4)) > 59)) {
    throw const FormatException('Invalid offset');
  }
  return DateTime.parse(v).toUtc();
}

bool validScheduleZone(String? s) =>
    s != null &&
    s.isNotEmpty &&
    s.length <= 64 &&
    s != 'Local' &&
    !s.contains('..') &&
    !s.contains('\\') &&
    !s.contains('\u0000') &&
    !s.startsWith('/') &&
    (s == 'UTC' || s.contains('/'));

@immutable
class NotificationQuietWindow {
  const NotificationQuietWindow(this.startMinute, this.endMinute);
  final int startMinute, endMinute;
  bool get valid =>
      startMinute >= 0 &&
      startMinute < 1440 &&
      endMinute >= 0 &&
      endMinute < 1440 &&
      startMinute != endMinute;
  Map<String, int> get wire => {
    'startMinute': startMinute,
    'endMinute': endMinute,
  };
  bool same(NotificationQuietWindow? q) =>
      q != null && q.startMinute == startMinute && q.endMinute == endMinute;
}

@immutable
class NotificationSchedule {
  NotificationSchedule._(
    this.version,
    this.agentID,
    this.configured,
    this.status,
    this.enabled,
    this.timeZone,
    this.localMinute,
    this.quiet,
    this.maxContactsPerDay,
    List<String> categories,
    this.validFrom,
    this.expiresAt,
    this.updatedAt,
  ) : categories = List.unmodifiable(categories);
  final int version, localMinute, maxContactsPerDay;
  final String agentID, status, timeZone;
  final bool configured, enabled;
  final NotificationQuietWindow? quiet;
  final List<String> categories;
  final DateTime? validFrom, expiresAt, updatedAt;
  factory NotificationSchedule.read(Object? input) {
    final m = input as Map<String, dynamic>;
    final version = _integer(m['version'], 0, 9007199254740991),
        id = m['agentId'];
    if (m['schemaVersion'] != notificationScheduleSchema ||
        id is! String ||
        !_scheduleID.hasMatch(id) ||
        id == '00000000-0000-0000-0000-000000000000' ||
        m['configured'] is! bool ||
        m['enabled'] is! bool ||
        !notificationScheduleStatuses.containsKey(m['status']) ||
        m['budgetWindowHours'] != 24 ||
        !m.containsKey('quiet')) {
      throw const FormatException('Unknown schedule');
    }
    final configured = m['configured'] as bool,
        enabled = m['enabled'] as bool,
        status = m['status'] as String;
    final zone = m['timeZone'] as String,
        minute = _integer(m['localMinute'], 0, 1439),
        budget = _integer(m['maxContactsPerDay'], 0, 20);
    final raw = m['categories'] as List, categories = <String>[];
    for (final c in raw) {
      if (c is! String ||
          !notificationCategories.contains(c) ||
          categories.contains(c) ||
          (categories.isNotEmpty && categories.last.compareTo(c) >= 0)) {
        throw const FormatException('Invalid categories');
      }
      categories.add(c);
    }
    NotificationQuietWindow? quiet;
    if (m['quiet'] != null) {
      final q = m['quiet'] as Map<String, dynamic>;
      if (q.length != 2) throw const FormatException('Unknown quiet window');
      quiet = NotificationQuietWindow(
        _integer(q['startMinute'], 0, 1439),
        _integer(q['endMinute'], 0, 1439),
      );
      if (!quiet.valid) throw const FormatException('Invalid quiet window');
    }
    final from = m['validFrom'] == null ? null : _time(m['validFrom']),
        expires = m['expiresAt'] == null ? null : _time(m['expiresAt']),
        updated = m['updatedAt'] == null ? null : _time(m['updatedAt']);
    if (!configured) {
      if (version != 0 ||
          status != 'UNCONFIGURED' ||
          enabled ||
          zone != '' ||
          minute != 0 ||
          m['gapPolicy'] != '' ||
          m['foldPolicy'] != '' ||
          quiet != null ||
          budget != 0 ||
          categories.isNotEmpty ||
          from != null ||
          expires != null ||
          updated != null) {
        throw const FormatException('Invalid unconfigured schedule');
      }
    } else if (version == 0 ||
        status == 'UNCONFIGURED' ||
        !validScheduleZone(zone) ||
        m['gapPolicy'] != 'SKIP' ||
        m['foldPolicy'] != 'EARLIER_ONCE' ||
        categories.isEmpty ||
        categories.length > 8 ||
        from == null ||
        expires == null ||
        updated != from ||
        !expires.isAfter(from) ||
        expires.difference(from) > const Duration(hours: 720) ||
        (status == 'ACTIVE' && !enabled) ||
        (status == 'DISABLED' && enabled)) {
      throw const FormatException('Invalid configured schedule');
    }
    return NotificationSchedule._(
      version,
      id,
      configured,
      status,
      enabled,
      zone,
      minute,
      quiet,
      budget,
      categories,
      from,
      expires,
      updated,
    );
  }
}

@immutable
class NotificationScheduleDraft {
  NotificationScheduleDraft({
    this.enabled = false,
    this.timeZone,
    this.localMinute,
    this.quiet,
    this.maxContactsPerDay,
    List<String> categories = const [],
    this.expiresAt,
  }) : categories = List.unmodifiable(categories);
  final bool enabled;
  final String? timeZone;
  final int? localMinute, maxContactsPerDay;
  final NotificationQuietWindow? quiet;
  final List<String> categories;
  final DateTime? expiresAt;
  factory NotificationScheduleDraft.fromPolicy(NotificationSchedule p) =>
      !p.configured
      ? NotificationScheduleDraft()
      : NotificationScheduleDraft(
          enabled: p.enabled,
          timeZone: p.timeZone,
          localMinute: p.localMinute,
          quiet: p.quiet,
          maxContactsPerDay: p.maxContactsPerDay,
          categories: p.categories,
          expiresAt: p.expiresAt,
        );
  NotificationScheduleDraft copyWith({
    bool? enabled,
    String? timeZone,
    int? localMinute,
    NotificationQuietWindow? quiet,
    bool clearQuiet = false,
    int? maxContactsPerDay,
    List<String>? categories,
    DateTime? expiresAt,
  }) => NotificationScheduleDraft(
    enabled: enabled ?? this.enabled,
    timeZone: timeZone ?? this.timeZone,
    localMinute: localMinute ?? this.localMinute,
    quiet: clearQuiet ? null : quiet ?? this.quiet,
    maxContactsPerDay: maxContactsPerDay ?? this.maxContactsPerDay,
    categories: categories ?? this.categories,
    expiresAt: expiresAt ?? this.expiresAt,
  );
  bool valid(DateTime now) =>
      validScheduleZone(timeZone) &&
      localMinute != null &&
      localMinute! >= 0 &&
      localMinute! < 1440 &&
      (quiet == null || quiet!.valid) &&
      maxContactsPerDay != null &&
      maxContactsPerDay! >= 0 &&
      maxContactsPerDay! <= 20 &&
      categories.isNotEmpty &&
      categories.length <= 8 &&
      categories.toSet().length == categories.length &&
      categories.every(notificationCategories.contains) &&
      expiresAt != null &&
      expiresAt!.isAfter(now) &&
      expiresAt!.difference(now) <= const Duration(hours: 720);
  Map<String, dynamic> wire(int version) => {
    'expectedVersion': version,
    'enabled': enabled,
    'timeZone': timeZone,
    'localMinute': localMinute,
    'gapPolicy': 'SKIP',
    'foldPolicy': 'EARLIER_ONCE',
    'quiet': quiet?.wire,
    'maxContactsPerDay': maxContactsPerDay,
    'categories': [...categories]..sort(),
    'expiresAt': expiresAt!.toUtc().toIso8601String(),
  };
  bool same(NotificationSchedule p) =>
      p.configured &&
      enabled == p.enabled &&
      timeZone == p.timeZone &&
      localMinute == p.localMinute &&
      (quiet == null ? p.quiet == null : quiet!.same(p.quiet)) &&
      maxContactsPerDay == p.maxContactsPerDay &&
      setEquals(categories.toSet(), p.categories.toSet()) &&
      expiresAt?.toUtc() == p.expiresAt;
}

class NotificationScheduleApproval {
  NotificationScheduleApproval._(
    this.generation,
    this.version,
    this.agentID,
    this.draft,
  );
  final int generation, version;
  final String agentID;
  final NotificationScheduleDraft draft;
}

/// Only original human GET/PUT. This never runs a scheduler or model, sends
/// notifications, computes DST, or treats a readback as delivery evidence.
class NotificationScheduleController extends ChangeNotifier {
  NotificationScheduleController({
    required this.authorizationHeader,
    required this.accountID,
    this.organizationWorkspaceID,
    this.identityChanges,
    this.current,
    http.Client? client,
    String? apiBaseUrl,
    DateTime Function()? now,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl,
       _now = now ?? DateTime.now {
    synchronizeIdentity();
    identityChanges?.addListener(_identityChanged);
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
  int generation = 0, _request = 0;
  bool _closed = false, loading = false, saving = false, uncertain = false;
  bool _sourceRetired = false;
  NotificationSchedule? policy;
  NotificationScheduleDraft? draft;
  NotificationScheduleApproval? _pending;
  String? error, message;
  bool get personal =>
      !_sourceRetired &&
      current?.call() != false &&
      authorizationHeader()?.isNotEmpty == true &&
      accountID()?.isNotEmpty == true &&
      organizationWorkspaceID?.call() == null;
  bool get editable =>
      personal && policy != null && !loading && !saving && !uncertain;
  void _notify() {
    if (!_closed) notifyListeners();
  }

  void synchronizeIdentity() {
    if (_closed) return;
    if (_sourceRetired) return;
    if (current?.call() == false) {
      // Retire the captured Settings source synchronously, before the boundary
      // can render its replacement. Source ABA never revives this approval.
      _sourceRetired = true;
      generation++;
      _request++;
      policy = null;
      draft = null;
      _pending = null;
      loading = saving = uncertain = false;
      error = message = null;
      _notify();
      return;
    }
    final next = (
      authorizationHeader(),
      accountID(),
      organizationWorkspaceID?.call(),
    );
    if (next == _identity) return;
    _identity = next;
    generation++;
    _request++;
    policy = null;
    draft = null;
    _pending = null;
    loading = saving = uncertain = false;
    error = message = null;
    _notify();
  }

  void _identityChanged() {
    final old = generation;
    synchronizeIdentity();
    if (generation != old && personal) unawaited(load());
  }

  bool _current(int g, int s) {
    if (_closed) return false;
    synchronizeIdentity();
    return g == generation && s == _request && personal;
  }

  Uri get _url => Uri.parse(
    '${_base.replaceFirst(RegExp(r'/$'), '')}/v1/me/notification-schedule',
  );
  Map<String, String> get _headers => {
    'Authorization': authorizationHeader()!,
    'Content-Type': 'application/json',
  };
  NotificationSchedule _read(http.Response r) {
    if (r.statusCode != 200) throw _ScheduleHTTP(r.statusCode);
    return NotificationSchedule.read(
      (jsonDecode(r.body) as Map<String, dynamic>)['data'],
    );
  }

  String _failure(Object e) => switch (e) {
    _ScheduleHTTP(status: 401) => '登录已失效，请重新登录后读取本人计划。',
    _ScheduleHTTP(status: 403) => '请切回本人账号，恢复访问权限后重新读取。',
    _ScheduleHTTP(status: 400) => '请检查明确时区、时间、静默窗口、类别、额度和有效期限。',
    _ScheduleHTTP(status: 409) => '计划版本已更新，请重新读取后检查。',
    _ => '定时汇总计划暂不可读，请稍后重新读取。',
  };
  void _adopt(NotificationSchedule p) {
    policy = p;
    draft = p.configured ? NotificationScheduleDraft.fromPolicy(p) : null;
  }

  bool _matchesPending(NotificationSchedule p) =>
      _pending != null &&
      p.agentID == _pending!.agentID &&
      p.version == _pending!.version + 1 &&
      _pending!.draft.same(p);
  Future<void> load() async {
    synchronizeIdentity();
    if (_closed || !personal || saving) return;
    if (_base.trim().isEmpty) {
      policy = null;
      draft = null;
      error = '当前运行环境未配置API，无法读取或保存定时汇总计划。';
      message = null;
      _notify();
      return;
    }
    final g = generation, s = ++_request;
    final headers = Map<String, String>.unmodifiable(_headers);
    loading = true;
    error = message = null;
    policy = null;
    draft = null;
    _notify();
    if (!_current(g, s)) return;
    try {
      final p = _read(
        await _client
            .get(_url, headers: headers)
            .timeout(const Duration(seconds: 10)),
      );
      if (!_current(g, s)) return;
      _adopt(p);
      if (uncertain) {
        if (_matchesPending(p)) {
          uncertain = false;
          _pending = null;
          message = '已核实当前计划与确认内容一致；不代表已投递。';
        } else {
          error = '已读回当前计划，原提交结果仍无法确定。请检查后明确采用当前设置，不会重复提交旧内容。';
        }
      }
    } catch (e) {
      if (_current(g, s)) {
        error = _failure(e);
        policy = null;
        draft = null;
      }
    } finally {
      if (_current(g, s)) {
        loading = false;
        _notify();
      }
    }
  }

  void startDraft() {
    synchronizeIdentity();
    if (editable && policy != null && !policy!.configured) {
      draft = NotificationScheduleDraft();
      message = error = null;
      _notify();
    }
  }

  void edit(NotificationScheduleDraft d) {
    synchronizeIdentity();
    if (!editable || draft == null) return;
    draft = d;
    message = error = null;
    _notify();
  }

  NotificationScheduleApproval? preview() {
    synchronizeIdentity();
    if (!editable || draft == null || !draft!.valid(_now().toUtc())) {
      return null;
    }
    return NotificationScheduleApproval._(
      generation,
      policy!.version,
      policy!.agentID,
      draft!,
    );
  }

  void adoptReadback() {
    synchronizeIdentity();
    if (!personal || loading || saving || !uncertain || policy == null) return;
    uncertain = false;
    _pending = null;
    draft = policy!.configured
        ? NotificationScheduleDraft.fromPolicy(policy!)
        : null;
    error = null;
    message = '已采用读回的当前版本。修改后需重新检查和确认，不会重发旧提交。';
    _notify();
  }

  Future<void> save(NotificationScheduleApproval a) async {
    synchronizeIdentity();
    if (!editable ||
        a.generation != generation ||
        a.version != policy?.version ||
        a.agentID != policy?.agentID ||
        !identical(a.draft, draft) ||
        !a.draft.valid(_now().toUtc())) {
      return;
    }
    final g = generation, s = ++_request;
    final headers = Map<String, String>.unmodifiable(_headers);
    var conflict = false;
    saving = true;
    error = message = null;
    _notify();
    if (!_current(g, s)) return;
    try {
      final r = await _client
          .put(
            _url,
            headers: headers,
            body: jsonEncode(a.draft.wire(a.version)),
          )
          .timeout(const Duration(seconds: 10));
      if (!_current(g, s)) return;
      if (r.statusCode == 409) {
        conflict = true;
        policy = null;
        draft = null;
        final p = _read(
          await _client
              .get(_url, headers: headers)
              .timeout(const Duration(seconds: 10)),
        );
        if (!_current(g, s)) return;
        _adopt(p);
        error = '计划已被更新，旧草稿未覆盖它。请检查当前版本后重新确认。';
        return;
      }
      if (r.statusCode >= 500) throw const _ScheduleUnknown();
      final p = _read(r);
      if (p.version != a.version + 1 ||
          p.agentID != a.agentID ||
          !a.draft.same(p) ||
          (p.status != 'ACTIVE' && p.status != 'DISABLED')) {
        throw const _ScheduleUnknown();
      }
      _adopt(p);
      message = '计划设置已保存，不代表消息已投递；请在收件箱查看实际汇总。';
    } catch (e) {
      if (!_current(g, s)) return;
      if (conflict) {
        policy = null;
        draft = null;
        error = '计划版本已变化，当前版本也暂不可读。请重新读取后检查；不会重复提交。';
      } else if (e is _ScheduleHTTP && e.status < 500 && e.status != 408) {
        error = _failure(e);
        if (e.status == 401 || e.status == 403) {
          policy = null;
          draft = null;
          // The original HTTP response rechecks Session after Store.Put.
          // A late denial does not establish that no save was committed.
          uncertain = true;
          _pending = a;
          error = '${_failure(e)} 保存结果尚未确认，恢复本人访问后只重新读取当前计划。';
        }
      } else {
        uncertain = true;
        _pending = a;
        error = '提交结果未知，正在读取当前计划；不会重复提交。';
        _notify();
        if (!_current(g, s)) return;
        try {
          final p = _read(
            await _client
                .get(_url, headers: headers)
                .timeout(const Duration(seconds: 10)),
          );
          if (!_current(g, s)) return;
          _adopt(p);
          if (_matchesPending(p)) {
            uncertain = false;
            _pending = null;
            error = null;
            message = '已核实当前计划与确认内容一致；不代表已投递。';
          } else {
            error = '已读回当前计划，原提交结果仍无法确定。请检查后明确采用当前设置。';
          }
        } catch (_) {
          if (_current(g, s)) {
            policy = null;
            draft = null;
            error = '提交结果未知，当前计划也暂不可读。请稍后重新读取，不要重复提交。';
          }
        }
      }
    } finally {
      if (_current(g, s)) {
        saving = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    _closed = true;
    generation++;
    _request++;
    identityChanges?.removeListener(_identityChanged);
    if (_ownsClient) _client.close();
    super.dispose();
  }
}

class _ScheduleHTTP implements Exception {
  const _ScheduleHTTP(this.status);
  final int status;
}

class _ScheduleUnknown implements Exception {
  const _ScheduleUnknown();
}
