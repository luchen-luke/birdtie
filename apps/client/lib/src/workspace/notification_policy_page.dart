import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import 'notification_policy_controller.dart';

class NotificationPolicyPage extends StatefulWidget {
  const NotificationPolicyPage({
    super.key,
    required this.auth,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.current,
  });
  final BirdtieAuthController auth;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final bool Function()? current;
  @override
  State<NotificationPolicyPage> createState() => _NotificationPolicyPageState();
}

class _NotificationPolicyPageState extends State<NotificationPolicyPage> {
  late final _source = (
    widget.auth, widget.client, widget.apiBaseUrl, widget.workspaceChanges,
    widget.organizationWorkspaceID, widget.current,
  );
  bool _sourceRetired = false;
  bool _currentSource() {
    if (!mounted || _sourceRetired) return false;
    final source = _source;
    if (source.$1 != widget.auth || source.$2 != widget.client ||
        source.$3 != widget.apiBaseUrl || source.$4 != widget.workspaceChanges ||
        source.$5 != widget.organizationWorkspaceID || source.$6 != widget.current ||
        source.$6?.call() == false) {
      _sourceRetired = true;
      return false;
    }
    return true;
  }
  late final NotificationPolicyController _data = NotificationPolicyController(
    authorizationHeader: () => widget.auth.authorizationHeader,
    accountID: () => widget.auth.accountID,
    organizationWorkspaceID: () => widget.organizationWorkspaceID?.call(),
    client: widget.client,
    apiBaseUrl: widget.apiBaseUrl,
    current: _currentSource,
  );
  Route<bool>? _approvalRoute;
  String? _localError;
  @override
  void initState() {
    super.initState();
    widget.auth.addListener(_identityChanged);
    widget.workspaceChanges?.addListener(_identityChanged);
    _data.addListener(_changed);
    unawaited(_data.load());
  }

  void _changed() {
    if (mounted) {
      setState(() {});
    }
  }

  void _identityChanged() {
    final g = _data.generation;
    _data.synchronizeIdentity();
    if (g == _data.generation && _currentSource()) {
      return;
    }
    final route = _approvalRoute;
    _approvalRoute = null;
    if (route?.isActive ?? false) {
      Navigator.of(context).removeRoute(route!);
    }
    _localError = null;
    if (_data.personal) {
      unawaited(_data.load());
    }
  }

  @override
  void didUpdateWidget(covariant NotificationPolicyPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.auth != widget.auth) {
      oldWidget.auth.removeListener(_identityChanged);
      widget.auth.addListener(_identityChanged);
    }
    if (oldWidget.workspaceChanges != widget.workspaceChanges) {
      oldWidget.workspaceChanges?.removeListener(_identityChanged);
      widget.workspaceChanges?.addListener(_identityChanged);
    }
    _identityChanged();
  }

  @override
  void dispose() {
    widget.auth.removeListener(_identityChanged);
    widget.workspaceChanges?.removeListener(_identityChanged);
    _data.removeListener(_changed);
    _data.dispose();
    super.dispose();
  }

  bool get _busy => _data.loading || _data.saving;
  void _edit({
    bool? enabled,
    String? route,
    Map<String, String>? rules,
    DateTime? expires,
    DateTime? pause,
    bool clearPause = false,
  }) {
    final d = _data.draft;
    if (d == null) {
      return;
    }
    _localError = null;
    _data.edit(
      NotificationPolicyDraft(
        enabled: enabled ?? d.enabled,
        defaultRoute: route ?? d.defaultRoute,
        rules: rules ?? d.rules,
        expiresAt: expires ?? d.expiresAt,
        pauseUntil: clearPause ? null : pause ?? d.pauseUntil,
      ),
    );
  }

  String _time(DateTime t) =>
      '${t.toLocal().year}年${t.toLocal().month}月${t.toLocal().day}日 ${t.toLocal().hour.toString().padLeft(2, '0')}:${t.toLocal().minute.toString().padLeft(2, '0')}（${t.toLocal().timeZoneName}）';
  Future<void> _confirm() async {
    final approved = _data.preview();
    if (approved == null) {
      setState(() => _localError = '请检查设置，并选择未来的有效期限。');
      return;
    }
    final d = approved.draft;
    final route = DialogRoute<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('确认通知设置'),
        content: SingleChildScrollView(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: [
              Text('本人设置 · 当前版本 ${approved.version}'),
              const SizedBox(height: 12),
              Text(
                d.enabled
                    ? '默认：${notificationRouteLabels[d.defaultRoute]}'
                    : '关闭自定义设置：未来事件恢复普通提醒。',
              ),
              for (final c in notificationCategories)
                if (d.rules.containsKey(c))
                  Text(
                    '${notificationCategoryLabels[c]}：${notificationRouteLabels[d.rules[c]]}',
                  ),
              Text('有效至：${_time(d.expiresAt)}'),
              if (d.pauseUntil != null) Text('暂停至：${_time(d.pauseUntil!)}'),
              const SizedBox(height: 12),
              const Text('这只改变未来通知的处理方式，不会重放旧的静默或待汇总通知，也不代表推送已送达。'),
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
    _approvalRoute = route;
    final yes = await Navigator.of(context).push(route);
    if (_approvalRoute == route) {
      _approvalRoute = null;
    }
    if (!mounted || yes != true || !_currentSource()) {
      return;
    }
    await _data.save(approved);
  }

  @override
  Widget build(BuildContext context) => !_currentSource()
      ? Scaffold(
          appBar: AppBar(title: const Text('通知设置')),
          body: const SafeArea(
            child: Padding(
              padding: EdgeInsets.all(20),
              child: Text('工作身份或来源已变化，请返回当前入口重新核实。'),
            ),
          ),
        )
      : Scaffold(
    appBar: AppBar(title: const Text('通知设置')),
    body: SafeArea(
      child: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          const Text(
            '只提醒值得关注的事',
            style: TextStyle(fontSize: 24, fontWeight: FontWeight.w700),
          ),
          const SizedBox(height: 8),
          const Text('活动变更、邀请、联系申请，以及与你明确意图强规则匹配的活动机会。匹配不是概率预测，也不会自动报名。'),
          const SizedBox(height: 16),
          if (!_data.personal)
            const Text('请切回本人账号管理通知设置。')
          else ...[
            if (_busy) const LinearProgressIndicator(),
            if (_data.error != null || _localError != null)
              Semantics(
                liveRegion: true,
                child: Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Text(_data.error ?? _localError!),
                ),
              ),
            if (_data.message != null)
              Semantics(liveRegion: true, child: Text(_data.message!)),
            if (_data.policy != null && _data.draft != null) ...[
              Text(
                _data.policy!.version == 0
                    ? '尚未自定义：当前按普通提醒处理。'
                    : '当前设置版本：${_data.policy!.version}',
              ),
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('使用自定义通知设置'),
                subtitle: const Text('关闭会恢复未来事件的普通提醒，不等于静音。'),
                value: _data.draft!.enabled,
                onChanged: _busy || _data.uncertain
                    ? null
                    : (v) => _edit(enabled: v),
              ),
              if (_data.draft!.enabled) ...[
                DropdownButtonFormField<String>(
                  initialValue: _data.draft!.defaultRoute,
                  isExpanded: true,
                  decoration: const InputDecoration(labelText: '默认处理方式'),
                  items: [
                    for (final r in notificationRoutes)
                      DropdownMenuItem(
                        value: r,
                        child: Text(notificationRouteLabels[r]!),
                      ),
                  ],
                  onChanged: _busy || _data.uncertain
                      ? null
                      : (v) {
                          if (v != null) {
                            _edit(route: v);
                          }
                        },
                ),
                const SizedBox(height: 8),
                const Text(
                  '静默与不接收均不会进入收件箱。待汇总保留待处理记录，可在设置的“定时汇总计划”中另行选择汇总；不生成AI摘要，实际通知以收件箱为准。优先提醒仅改变站内排序，不保证系统推送。',
                ),
                ExpansionTile(
                  tilePadding: EdgeInsets.zero,
                  title: const Text('按类型设置'),
                  children: [
                    for (final c in notificationCategories)
                      Padding(
                        padding: const EdgeInsets.only(bottom: 12),
                        child: DropdownButtonFormField<String>(
                          key: ValueKey('notification-rule-$c'),
                          initialValue: _data.draft!.rules[c] ?? 'DEFAULT',
                          isExpanded: true,
                          decoration: InputDecoration(
                            labelText: notificationCategoryLabels[c],
                          ),
                          items: [
                            const DropdownMenuItem(
                              value: 'DEFAULT',
                              child: Text('跟随默认设置'),
                            ),
                            for (final r in notificationRoutes)
                              DropdownMenuItem(
                                value: r,
                                child: Text(notificationRouteLabels[r]!),
                              ),
                          ],
                          onChanged: _busy || _data.uncertain
                              ? null
                              : (v) {
                                  final rules = Map<String, String>.of(
                                    _data.draft!.rules,
                                  );
                                  if (v == 'DEFAULT') {
                                    rules.remove(c);
                                  } else if (v != null) {
                                    rules[c] = v;
                                  }
                                  _edit(rules: rules);
                                },
                        ),
                      ),
                  ],
                ),
              ],
              ExpansionTile(
                tilePadding: EdgeInsets.zero,
                title: const Text('有效期限与暂停'),
                children: [
                  Text('有效至：${_time(_data.draft!.expiresAt)}'),
                  const SizedBox(height: 12),
                  DropdownButtonFormField<int>(
                    isExpanded: true,
                    decoration: const InputDecoration(labelText: '更新有效期限'),
                    hint: const Text('保留当前期限'),
                    items: [
                      for (final d in [1, 7, 30])
                        DropdownMenuItem(value: d, child: Text('$d 天')),
                    ],
                    onChanged: _busy || _data.uncertain
                        ? null
                        : (days) {
                            if (days != null) {
                              _edit(
                                expires: DateTime.now().toUtc().add(
                                  Duration(days: days),
                                ),
                                clearPause: true,
                              );
                            }
                          },
                  ),
                  const SizedBox(height: 12),
                  if (_data.draft!.pauseUntil != null)
                    Text('当前暂停至：${_time(_data.draft!.pauseUntil!)}'),
                  DropdownButtonFormField<int>(
                    isExpanded: true,
                    decoration: const InputDecoration(labelText: '暂停普通提醒'),
                    hint: const Text('保留当前暂停设置'),
                    items: [
                      const DropdownMenuItem(value: 0, child: Text('不暂停')),
                      for (final h in [1, 8, 24])
                        DropdownMenuItem(value: h, child: Text('$h 小时')),
                    ],
                    onChanged: _busy || _data.uncertain
                        ? null
                        : (hours) {
                            if (hours != null) {
                              _edit(
                                pause: DateTime.now().toUtc().add(
                                  Duration(hours: hours),
                                ),
                                clearPause: hours == 0,
                              );
                            }
                          },
                  ),
                  const SizedBox(height: 12),
                  const Text(
                    '期限为设备时间下的草稿，保存时由服务校验。到期后未来事件恢复普通提醒；暂停期间静默，不改变“不接收”。',
                  ),
                ],
              ),
              const SizedBox(height: 16),
              FilledButton(
                onPressed: _busy || _data.uncertain ? null : _confirm,
                child: const Text('检查并保存'),
              ),
            ],
            TextButton(
              onPressed: _busy ? null : () => _data.load(),
              child: const Text('重新读取当前设置'),
            ),
          ],
        ],
      ),
    ),
  );
}
