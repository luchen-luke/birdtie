import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import 'agent_profile_api.dart' show profileFieldLabels;
import 'task_context_privacy_api.dart';
import 'task_context_privacy_controller.dart';

class TaskContextPrivacyPage extends StatefulWidget {
  const TaskContextPrivacyPage({
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
  State<TaskContextPrivacyPage> createState() => _TaskContextPrivacyPageState();
}

class _TaskContextPrivacyPageState extends State<TaskContextPrivacyPage> {
  late final _source = (
    widget.auth,
    widget.client,
    widget.apiBaseUrl,
    widget.workspaceChanges,
    widget.organizationWorkspaceID,
    widget.current,
  );
  bool _retired = false;
  Timer? _timer;
  bool _current() {
    if (!mounted || _retired) return false;
    if (_source !=
            (
              widget.auth,
              widget.client,
              widget.apiBaseUrl,
              widget.workspaceChanges,
              widget.organizationWorkspaceID,
              widget.current,
            ) ||
        widget.current?.call() == false) {
      _retired = true;
    }
    return !_retired;
  }

  late final _data = TaskContextPrivacyController(
    api: TaskContextPrivacyAPI(client: _source.$2, apiBaseUrl: _source.$3),
    authorizationHeader: () => _source.$1.authorizationHeader,
    ownerID: () => _source.$1.accountID,
    organizationWorkspaceID: _source.$5,
    current: _current,
  );
  @override
  void initState() {
    super.initState();
    _source.$1.addListener(_identity);
    _source.$4?.addListener(_identity);
    _data.addListener(_changed);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_current()) unawaited(_data.load());
    });
  }

  void _identity() => _data.synchronizeIdentity();
  void _changed() {
    if (!mounted) return;
    _timer?.cancel();
    final until = _data.inventory?.validUntil;
    if (until != null && _data.fresh) {
      _timer = Timer(until.difference(_data.now()), _data.expire);
    }
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  @override
  void didUpdateWidget(TaskContextPrivacyPage old) {
    super.didUpdateWidget(old);
    _identity();
  }

  @override
  void dispose() {
    _timer?.cancel();
    _source.$1.removeListener(_identity);
    _source.$4?.removeListener(_identity);
    _data.removeListener(_changed);
    _data.dispose();
    super.dispose();
  }

  String _time(DateTime t) =>
      '${t.toLocal().year}年${t.toLocal().month}月${t.toLocal().day}日 ${t.toLocal().hour.toString().padLeft(2, '0')}:${t.toLocal().minute.toString().padLeft(2, '0')}';
  Widget _action(String text, VoidCallback? tap, {bool primary = false}) =>
      primary
      ? FilledButton(
          onPressed: tap,
          style: FilledButton.styleFrom(minimumSize: const Size(48, 48)),
          child: Text(text),
        )
      : OutlinedButton(
          onPressed: tap,
          style: OutlinedButton.styleFrom(minimumSize: const Size(48, 48)),
          child: Text(text),
        );
  @override
  Widget build(BuildContext context) {
    final v = _data.inventory, a = _data.review;
    return Scaffold(
      appBar: AppBar(title: const Text('本次会话的任务资料许可')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            const Text('仅显示本次登录会话，不含其它登录记录。这里查看当前任务获准使用哪些资料，并可检查具体许可后撤回。'),
            const SizedBox(height: 12),
            const Text(
              '仅用于本地任务读取；不会开启模型、分析、自动学习、公开分享或长期记忆。清单中的状态不保证当前来源或任务仍可读取，实际操作以服务端核验为准。',
            ),
            const SizedBox(height: 8),
            const Text(
              '撤回停止后续使用这项许可；已读取的内容不会被召回，独立记忆不会因此删除。关页重开只查看当前状态，不提供先前请求的因果回执。',
            ),
            if (_data.busy) const LinearProgressIndicator(),
            if (_data.error != null)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 12),
                child: Text(
                  _data.error!,
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
              ),
            if (_data.notice != null)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 12),
                child: Text(_data.notice!),
              ),
            if (!_data.retired)
              _action(
                '重新读取许可',
                _data.busy ? null : () => unawaited(_data.load()),
              ),
            if (_data.pending != null)
              _action(
                '仅核实原许可当前状态',
                _data.busy || _data.retired
                    ? null
                    : () => unawaited(_data.verifyPending()),
              ),
            if (v != null) ...[
              Text('读取时间：${_time(v.observedAt)}'),
              if (!_data.fresh) const Text('清单已过读取期限，请重新读取；这不是原许可的到期时间。'),
              if (v.truncated)
                const Text('优先显示读取时尚未撤回且未到期的许可，最多50项。未显示不代表不存在或已撤回。'),
              if (v.grants.isEmpty)
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 16),
                  child: Text('本次会话没有可显示的任务资料许可。'),
                ),
              for (final g in v.grants)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 16),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      Text(
                        '许可版本 ${g.revision}',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      if (g.profileFields.isNotEmpty)
                        Text(
                          '资料范围：${g.profileFields.map((f) => profileFieldLabels[f]).join('、')}',
                        ),
                      Text(
                        '明确记忆 ${g.memoryIDs.length}条 · 地点 ${g.placeIDs.length}项 · 活动 ${g.activityIDs.length}项 · 关系 ${g.tieIDs.length}项',
                      ),
                      if (g.policyFamilies.isNotEmpty)
                        Text(
                          '设置范围：${g.policyFamilies.map((f) => const {'ATTENTION': '注意力', 'SOCIAL': '社交建议', 'AUTONOMY': '自主操作'}[f]).join('、')}',
                        ),
                      Text(
                        '允许时间：${_time(g.createdAt)}\n许可到期：${_time(g.expiresAt)}',
                      ),
                      Text(
                        g.revoked
                            ? '已撤回'
                            : !g.expiresAt.isAfter(v.observedAt)
                            ? '读取时已到期'
                            : '读取时尚未撤回且未到期',
                      ),
                      ExpansionTile(
                        title: const Text('查看具体记录范围'),
                        children: [
                          SelectableText(
                            '原任务 ${g.taskID}\n城市 ${g.cityID}\n任务版本时间 ${_time(g.taskUpdatedAt)}',
                          ),
                          for (final e in [
                            ('记忆', g.memoryIDs),
                            ('地点', g.placeIDs),
                            ('活动', g.activityIDs),
                            ('关系', g.tieIDs),
                          ])
                            if (e.$2.isNotEmpty)
                              SelectableText('${e.$1}：${e.$2.join('、')}'),
                        ],
                      ),
                      if (!g.revoked)
                        _action(
                          '检查这项撤回',
                          _data.busy || !_data.fresh || _data.pending != null
                              ? null
                              : () => _data.prepare(g),
                        ),
                      const Divider(),
                    ],
                  ),
                ),
            ],
            if (a != null) ...[
              Text(
                '确认撤回许可版本 ${a.grant.revision}',
                style: Theme.of(context).textTheme.titleMedium,
              ),
              const Text('只撤回上述具体范围的读取许可，不会删除原资料、任务或记忆。'),
              _action('取消本次检查', _data.busy ? null : _data.cancel),
              _action(
                '确认撤回这项许可',
                _data.busy || !_data.fresh
                    ? null
                    : () => unawaited(_data.withdraw(a)),
                primary: true,
              ),
            ],
            const SizedBox(height: 16),
            const Text('本页不创建或恢复读取批准。再次允许需要当前来源的新预览与明确批准；此页面未提供新的批准入口。'),
          ],
        ),
      ),
    );
  }
}
