import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import 'notification_policy_controller.dart'
    show notificationCategories, notificationCategoryLabels;
import 'notification_schedule_controller.dart';

class NotificationSchedulePage extends StatefulWidget {
  const NotificationSchedulePage({
    super.key,
    required this.auth,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.current,
    this.now,
  });
  final BirdtieAuthController auth;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final bool Function()? current;
  final DateTime Function()? now;
  @override
  State<NotificationSchedulePage> createState() =>
      _NotificationSchedulePageState();
}

class _NotificationSchedulePageState extends State<NotificationSchedulePage> {
  late NotificationScheduleController _data;
  final _zone = TextEditingController();
  Route<bool>? _approvalRoute;
  NotificationScheduleApproval? _approval;
  NotificationScheduleController? _approvalController;
  Route<TimeOfDay>? _timeRoute;
  NotificationScheduleController? _timeController;
  int? _timeGeneration;
  NotificationSchedule? _shownPolicy;
  String? _localError;
  bool _quietWanted = false;
  int? _quietStart, _quietEnd;
  DateTime _now() => widget.now?.call() ?? DateTime.now();
  @override
  void initState() {
    super.initState();
    _bind();
  }

  void _bind() {
    final auth = widget.auth, workspace = widget.organizationWorkspaceID;
    _data = NotificationScheduleController(
      authorizationHeader: () => auth.authorizationHeader,
      accountID: () => auth.accountID,
      organizationWorkspaceID: workspace,
      current: widget.current,
      identityChanges: Listenable.merge([auth, widget.workspaceChanges]),
      client: widget.client,
      apiBaseUrl: widget.apiBaseUrl,
      now: _now,
    );
    _data.addListener(_changed);
    unawaited(_data.load());
  }

  void _cancelApproval() {
    final route = _approvalRoute;
    _approvalRoute = null;
    _approval = null;
    _approvalController = null;
    if (route?.isActive ?? false) Navigator.of(context).removeRoute(route!);
  }

  void _changed() {
    if (!mounted) return;
    void apply() {
      if (!mounted) return;
      if (_approval != null &&
          (_approval!.generation != _data.generation ||
              !identical(_approvalController, _data))) {
        _cancelApproval();
      }
      if (_timeRoute != null &&
          (_timeGeneration != _data.generation ||
              !identical(_timeController, _data))) {
        final route = _timeRoute;
        _timeRoute = null;
        _timeController = null;
        _timeGeneration = null;
        if (route!.isActive) Navigator.of(context).removeRoute(route);
      }
      if (!identical(_shownPolicy, _data.policy)) {
        _shownPolicy = _data.policy;
        final q = _data.policy?.quiet;
        _quietWanted = q != null;
        _quietStart = q?.startMinute;
        _quietEnd = q?.endMinute;
        _localError = null;
        _zone.text = _data.policy?.timeZone ?? '';
      }
      setState(() {});
    }

    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) => apply());
    } else {
      apply();
    }
  }

  @override
  void didUpdateWidget(NotificationSchedulePage old) {
    super.didUpdateWidget(old);
    if (old.auth != widget.auth ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.workspaceChanges != widget.workspaceChanges ||
        old.organizationWorkspaceID != widget.organizationWorkspaceID ||
        old.current != widget.current ||
        old.now != widget.now) {
      _data.removeListener(_changed);
      _data.dispose();
      _bind();
      _changed();
    }
  }

  @override
  void dispose() {
    _data.removeListener(_changed);
    _data.dispose();
    _zone.dispose();
    super.dispose();
  }

  String _minute(int? n) => n == null
      ? '请选择'
      : '${(n ~/ 60).toString().padLeft(2, '0')}:${(n % 60).toString().padLeft(2, '0')}';
  String _absolute(DateTime? t) =>
      t == null ? '请选择有效期限' : t.toUtc().toIso8601String();
  ButtonStyle get _button =>
      OutlinedButton.styleFrom(minimumSize: const Size(48, 48));
  void _edit(NotificationScheduleDraft d) {
    _localError = null;
    _data.edit(d);
  }

  Widget _feedback() => Semantics(
    liveRegion: true,
    child: Padding(
      padding: const EdgeInsets.symmetric(vertical: 12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (_data.error != null || _localError != null)
            Text(_data.error ?? _localError!),
          if (_data.message != null) Text(_data.message!),
        ],
      ),
    ),
  );

  Future<void> _pickTime(String part) async {
    _data.synchronizeIdentity();
    final data = _data, g = data.generation, d = data.draft;
    if (!data.editable || d == null) return;
    final minute = part == 'time'
        ? d.localMinute
        : part == 'start'
        ? _quietStart
        : _quietEnd;
    final route = DialogRoute<TimeOfDay>(
      context: context,
      builder: (context) => TimePickerDialog(
        initialTime: TimeOfDay(
          hour: (minute ?? 0) ~/ 60,
          minute: (minute ?? 0) % 60,
        ),
        helpText: part == 'time'
            ? '选择汇总时间'
            : part == 'start'
            ? '选择静默开始时间'
            : '选择静默结束时间',
        cancelText: '取消',
        confirmText: '使用此时间',
      ),
    );
    _timeRoute = route;
    _timeController = data;
    _timeGeneration = g;
    final picked = await Navigator.of(context).push(route);
    if (_timeRoute == route) {
      _timeRoute = null;
      _timeController = null;
      _timeGeneration = null;
    }
    if (!mounted ||
        picked == null ||
        !identical(data, _data) ||
        widget.current?.call() == false ||
        g != data.generation ||
        !data.editable ||
        !identical(d, data.draft)) {
      return;
    }
    final n = picked.hour * 60 + picked.minute;
    if (part == 'time') {
      _edit(d.copyWith(localMinute: n));
      return;
    }
    setState(() {
      if (part == 'start') {
        _quietStart = n;
      } else {
        _quietEnd = n;
      }
    });
    if (_quietStart != null && _quietEnd != null) {
      _edit(
        d.copyWith(quiet: NotificationQuietWindow(_quietStart!, _quietEnd!)),
      );
    }
  }

  Future<void> _confirm() async {
    _data.synchronizeIdentity();
    if (widget.current?.call() == false) return;
    if (_quietWanted &&
        (_quietStart == null ||
            _quietEnd == null ||
            _quietStart == _quietEnd)) {
      setState(() => _localError = '请选择不同的静默开始与结束时间。');
      return;
    }
    final data = _data, a = data.preview();
    if (a == null) {
      setState(() => _localError = '请明确时区、汇总时间、类别、滚动额度和未来有效期限。');
      return;
    }
    final d = a.draft;
    final route = DialogRoute<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('确认定时汇总计划'),
        content: SingleChildScrollView(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: [
              Text('本人计划 · 当前版本 ${a.version}'),
              Text(d.enabled ? '启用确定性收件箱汇总' : '关闭未来汇总，不回放旧记录'),
              Text('明确时区：${d.timeZone}'),
              Text('当地汇总时间：${_minute(d.localMinute)}'),
              const Text('夏令时：不存在的时间当日跳过；重复时间取较早的一次。'),
              Text(
                d.quiet == null
                    ? '静默：不设置'
                    : '静默：[${_minute(d.quiet!.startMinute)}, ${_minute(d.quiet!.endMinute)})，按所选时区，可跨午夜',
              ),
              Text(
                '类别：${d.categories.map((c) => notificationCategoryLabels[c]).join('、')}',
              ),
              Text('滚动24小时最多 ${d.maxContactsPerDay} 条实际收件箱记录；0条停止本计划投递。'),
              Text('绝对有效期限（UTC）：${_absolute(d.expiresAt)}'),
              const SizedBox(height: 12),
              const Text('不生成AI摘要，不保证系统推送。关闭或改计划不清零额度、不回放旧汇总，也不停止原活动提醒。'),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('返回修改'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('确认保存'),
          ),
        ],
      ),
    );
    _approval = a;
    _approvalController = data;
    _approvalRoute = route;
    final yes = await Navigator.of(context).push(route);
    if (_approvalRoute == route) {
      _approvalRoute = null;
      _approval = null;
      _approvalController = null;
    }
    if (!mounted ||
        yes != true ||
        !identical(data, _data) ||
        widget.current?.call() == false ||
        a.generation != data.generation) {
      return;
    }
    if (!d.valid(_now().toUtc())) {
      setState(() => _localError = '有效期限已经过去，请更新期限后重新检查。');
      return;
    }
    await data.save(a);
  }

  @override
  Widget build(BuildContext context) {
    final data = _data, d = data.draft;
    final active = data.editable;
    return Scaffold(
      appBar: AppBar(title: const Text('定时汇总计划')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            Text('在你选择的时间汇总', style: Theme.of(context).textTheme.headlineSmall),
            const SizedBox(height: 8),
            const Text('只把当前获准、仍有效且分类为“待汇总”的记录放入收件箱。不生成AI摘要，不自动报名、发送消息或运行模型。'),
            const SizedBox(height: 16),
            if (!data.personal)
              const Text('请登录并切换到本人身份管理定时汇总。')
            else ...[
              if (data.loading || data.saving) const LinearProgressIndicator(),
              if (d == null) _feedback(),
              if (data.policy case final p?) ...[
                Text(
                  p.configured
                      ? '当前状态：${notificationScheduleStatuses[p.status]} · 版本 ${p.version}'
                      : '尚未设置定时汇总：不会自动创建每日计划。',
                ),
                if (p.status == 'PAUSED_SESSION')
                  const Text('原保存会话已失效。请检查当前本人计划并明确重新保存，不会自动换用新会话。'),
                if (p.status == 'EXPIRED')
                  const Text('当前计划已到期。更新期限并明确保存后才可能恢复未来汇总。'),
                if (!p.configured && d == null)
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: 12),
                    child: OutlinedButton(
                      style: _button,
                      onPressed: active ? data.startDraft : null,
                      child: const Text('创建汇总草稿'),
                    ),
                  ),
              ],
              if (d != null) ...[
                const SizedBox(height: 12),
                SwitchListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('启用定时汇总'),
                  subtitle: const Text('关闭只停止本计划未来汇总，不等于静音所有提醒。'),
                  value: d.enabled,
                  onChanged: active
                      ? (v) => _edit(d.copyWith(enabled: v))
                      : null,
                ),
                TextFormField(
                  controller: _zone,
                  decoration: const InputDecoration(
                    labelText: '明确时区（IANA）',
                    hintText: '例如 Europe/London；不猜当前所在地',
                  ),
                  enabled: active,
                  onChanged: (v) => _edit(d.copyWith(timeZone: v.trim())),
                ),
                const SizedBox(height: 8),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    for (final option in [
                      ('中国大陆', 'Asia/Shanghai'),
                      ('英国', 'Europe/London'),
                      ('UTC', 'UTC'),
                    ])
                      OutlinedButton(
                        style: _button,
                        onPressed: active
                            ? () {
                                _zone.text = option.$2;
                                _edit(d.copyWith(timeZone: option.$2));
                              }
                            : null,
                        child: Text('${option.$1} · ${option.$2}'),
                      ),
                  ],
                ),
                // The selected value is shown independently of a form field's
                // initialValue, so choosing a preset never leaves an old label.
                Text('所选时区：${d.timeZone ?? '未选择'}'),
                const SizedBox(height: 12),
                OutlinedButton(
                  style: _button,
                  key: const ValueKey('schedule-local-time'),
                  onPressed: active ? () => _pickTime('time') : null,
                  child: Text('汇总时间：${_minute(d.localMinute)}'),
                ),
                const Text('以所选时区的当地时间执行。夏令时缺口当日跳过，重复时刻只取较早一次；不回补保存前或跨日错过的时槽。'),
                const SizedBox(height: 16),
                Text('汇总类别', style: Theme.of(context).textTheme.titleMedium),
                for (final c in notificationCategories)
                  CheckboxListTile(
                    contentPadding: EdgeInsets.zero,
                    title: Text(notificationCategoryLabels[c]!),
                    value: d.categories.contains(c),
                    onChanged: active
                        ? (v) {
                            final cs = [...d.categories];
                            if (v == true) {
                              cs.add(c);
                            } else {
                              cs.remove(c);
                            }
                            _edit(d.copyWith(categories: cs));
                          }
                        : null,
                  ),
                const SizedBox(height: 12),
                DropdownButtonFormField<int>(
                  key: ValueKey(
                    'schedule-budget-${data.generation}-${data.policy?.version}',
                  ),
                  initialValue: d.maxContactsPerDay,
                  isExpanded: true,
                  decoration: const InputDecoration(labelText: '滚动24小时触达上限'),
                  hint: const Text('请选择额度'),
                  items: [
                    for (var n = 0; n <= 20; n++)
                      DropdownMenuItem(
                        value: n,
                        child: Text(n == 0 ? '0条（停止本计划投递）' : '最多 $n 条收件箱记录'),
                      ),
                  ],
                  onChanged: active
                      ? (v) {
                          if (v != null) {
                            _edit(d.copyWith(maxContactsPerDay: v));
                          }
                        }
                      : null,
                ),
                const Text('按实际成功插入的记录计数，不是批次数。关闭、改时区或改计划均不会清零已经发生的额度。'),
                const SizedBox(height: 16),
                ExpansionTile(
                  title: const Text('静默时段'),
                  tilePadding: EdgeInsets.zero,
                  children: [
                    SwitchListTile(
                      contentPadding: EdgeInsets.zero,
                      title: const Text('设置静默窗口'),
                      value: _quietWanted,
                      onChanged: active
                          ? (v) {
                              setState(() => _quietWanted = v);
                              if (!v) {
                                _quietStart = _quietEnd = null;
                                _edit(d.copyWith(clearQuiet: true));
                              }
                            }
                          : null,
                    ),
                    if (_quietWanted) ...[
                      OutlinedButton(
                        style: _button,
                        onPressed: active ? () => _pickTime('start') : null,
                        child: Text('静默开始：${_minute(_quietStart)}'),
                      ),
                      OutlinedButton(
                        style: _button,
                        onPressed: active ? () => _pickTime('end') : null,
                        child: Text('静默结束：${_minute(_quietEnd)}'),
                      ),
                      const Text(
                        '包含开始、不包含结束，可跨午夜。时槽落在静默中当日跳过；执行时暂处静默可在当天退出静默后核查。',
                      ),
                    ],
                  ],
                ),
                const SizedBox(height: 12),
                Text('绝对有效期限（UTC）：${_absolute(d.expiresAt)}'),
                DropdownButtonFormField<int>(
                  isExpanded: true,
                  decoration: const InputDecoration(labelText: '明确选择新的有效期限'),
                  hint: const Text('保留当前期限，或明确选择'),
                  items: [
                    for (final days in [1, 7, 30])
                      DropdownMenuItem(
                        value: days,
                        child: Text('从此刻起 $days 天'),
                      ),
                  ],
                  onChanged: active
                      ? (days) {
                          if (days != null) {
                            _edit(
                              d.copyWith(
                                expiresAt: _now().toUtc().add(
                                  Duration(days: days),
                                ),
                              ),
                            );
                          }
                        }
                      : null,
                ),
                const Text('这里显示设备时间生成的草稿期限，保存仍由服务时钟校验；检查和提交不会延长已经选择的期限。'),
                const SizedBox(height: 16),
                _feedback(),
                FilledButton(
                  style: FilledButton.styleFrom(
                    minimumSize: const Size(48, 48),
                  ),
                  onPressed: active ? _confirm : null,
                  child: const Text('检查并保存计划'),
                ),
              ],
              const SizedBox(height: 12),
              TextButton(
                style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
                onPressed: data.loading || data.saving ? null : data.load,
                child: const Text('重新读取当前计划'),
              ),
              if (data.uncertain && data.policy != null)
                OutlinedButton(
                  style: _button,
                  onPressed: data.loading || data.saving
                      ? null
                      : data.adoptReadback,
                  child: const Text('已检查，采用读回的当前设置'),
                ),
            ],
          ],
        ),
      ),
    );
  }
}
