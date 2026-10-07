import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import 'message_request_policy_controller.dart';

class MessageRequestPolicyPage extends StatefulWidget {
  const MessageRequestPolicyPage({
    super.key,
    required this.auth,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.now,
  });
  final BirdtieAuthController auth;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final DateTime Function()? now;
  @override
  State<MessageRequestPolicyPage> createState() =>
      _MessageRequestPolicyPageState();
}

class _MessageRequestPolicyPageState extends State<MessageRequestPolicyPage> {
  late final MessageRequestPolicyController _data;
  late final BirdtieAuthController _auth;
  late final Listenable? _workspaceChanges;
  bool _retired = false;
  String? _localError;
  DialogRoute<bool>? _approvalRoute;
  NavigatorState? _approvalNavigator;
  @override
  void initState() {
    super.initState();
    _auth = widget.auth;
    _workspaceChanges = widget.workspaceChanges;
    _data = MessageRequestPolicyController(
      authorizationHeader: () => widget.auth.authorizationHeader,
      accountID: () => widget.auth.accountID,
      organizationWorkspaceID: () => widget.organizationWorkspaceID?.call(),
      client: widget.client,
      apiBaseUrl: widget.apiBaseUrl,
      now: widget.now,
      current: () => mounted && !_retired,
    );
    _data.addListener(_changed);
    _auth.addListener(_identityChanged);
    _workspaceChanges?.addListener(_identityChanged);
    unawaited(_data.load());
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  void _closeApproval() {
    final r = _approvalRoute, n = _approvalNavigator;
    _approvalRoute = null;
    _approvalNavigator = null;
    void close() {
      if (n?.mounted == true && (r?.isActive ?? false)) n!.removeRoute(r!);
    }

    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        close();
      });
    } else {
      close();
    }
  }

  void _retire() {
    if (_retired) return;
    _retired = true;
    _data.synchronizeIdentity();
    _localError = null;
    _closeApproval();
    if (mounted) setState(() {});
  }

  void _identityChanged() {
    final g = _data.generation;
    _data.synchronizeIdentity();
    if (g != _data.generation) _retire();
  }

  @override
  void didUpdateWidget(MessageRequestPolicyPage old) {
    super.didUpdateWidget(old);
    if (old.auth != widget.auth ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.workspaceChanges != widget.workspaceChanges ||
        old.organizationWorkspaceID != widget.organizationWorkspaceID ||
        old.now != widget.now) {
      _retire();
    } else {
      _identityChanged();
    }
  }

  @override
  void dispose() {
    _auth.removeListener(_identityChanged);
    _workspaceChanges?.removeListener(_identityChanged);
    _data.removeListener(_changed);
    _data.dispose();
    super.dispose();
  }

  String _time(DateTime? t) {
    if (t == null) return '尚未选择';
    final v = t.toLocal();
    return '${v.year}年${v.month}月${v.day}日 ${v.hour.toString().padLeft(2, '0')}:${v.minute.toString().padLeft(2, '0')}（${v.timeZoneName}）';
  }

  void _edit(MessageRequestDraft d) {
    _localError = null;
    _data.edit(d);
  }

  Future<void> _confirm() async {
    final a = _data.preview();
    if (a == null) {
      setState(() => _localError = '请选择未来且不超过 30 天的有效期限。');
      return;
    }
    final data = _data, g = data.generation;
    final route = DialogRoute<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('确认消息请求设置'),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('当前账号：${widget.auth.displayName ?? '本人'}'),
              Text('当前设置版本：${a.version}'),
              const SizedBox(height: 12),
              Text(messageRequestLabels[a.draft.incoming]!),
              Text(messageRequestDetails[a.draft.incoming]!),
              const SizedBox(height: 12),
              Text('有效至：${_time(a.draft.expiresAt)}'),
              const SizedBox(height: 12),
              const Text('这只改变收件路由，不会接受请求、建立关系或发送消息。到期后新的请求会关闭，已有关系仍按原权限检查。'),
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
    _approvalNavigator = Navigator.of(context);
    final confirmed = await _approvalNavigator!.push<bool>(route);
    if (identical(_approvalRoute, route)) {
      _approvalRoute = null;
      _approvalNavigator = null;
    }
    if (!mounted ||
        _retired ||
        data != _data ||
        data.generation != g ||
        confirmed != true) {
      return;
    }
    await data.save(a);
  }

  @override
  Widget build(BuildContext context) {
    final p = _data.policy,
        d = _data.draft,
        busy = _data.loading || _data.saving;
    return Scaffold(
      appBar: AppBar(title: const Text('消息请求设置')),
      body: SafeArea(
        child: RadioGroup<String>(
          groupValue: d?.incoming,
          onChanged: (v) {
            if (_data.editable && v != null) {
              _edit(MessageRequestDraft(v, d?.expiresAt));
            }
          },
          child: ListView(
          padding: const EdgeInsets.all(24),
          children: [
            if (_retired) ...[
              const Text('工作身份或来源已变化，请返回设置重新打开。'),
            ] else if (!_data.personal) ...[
              const Text('请在本人登录状态下管理消息请求设置。'),
            ] else ...[
              Text('谁能发来新的请求', style: Theme.of(context).textTheme.titleLarge),
              const SizedBox(height: 12),
              const Text('仅管理当前个人账号的收件方式。已接受的关系可能显示“允许”，由现有关系决定，不能在这里设置。'),
              const SizedBox(height: 16),
              if (busy)
                Semantics(
                  liveRegion: true,
                  child: const LinearProgressIndicator(),
                ),
              if (p != null) ...[
                Text(switch (p.status) {
                  'UNCONFIGURED' => '尚未设置：沿用原普通请求流程。',
                  'EXPIRED' => '设置已过期：新的请求已关闭，请重新检查。',
                  _ => '当前设置：${messageRequestLabels[p.incoming]}',
                }),
                if (p.configured) Text('已保存有效至：${_time(p.expiresAt)}'),
                const SizedBox(height: 12),
                for (final entry in messageRequestLabels.entries)
                  RadioListTile<String>(
                    contentPadding: EdgeInsets.zero,
                    title: Text(entry.value),
                    subtitle: Text(messageRequestDetails[entry.key]!),
                    value: entry.key,
                    enabled: _data.editable,
                  ),
                const SizedBox(height: 16),
                Text(
                  '设置有效期（最长 30 天）',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                Text('草稿有效至：${_time(d?.expiresAt)}'),
                const SizedBox(height: 8),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    for (final days in [1, 7, 30])
                      OutlinedButton(
                        onPressed: _data.editable
                            ? () => _edit(
                                MessageRequestDraft(
                                  d!.incoming,
                                  (widget.now?.call() ?? DateTime.now())
                                      .toUtc()
                                      .add(Duration(days: days)),
                                ),
                              )
                            : null,
                        child: Text('$days 天'),
                      ),
                  ],
                ),
                const SizedBox(height: 12),
                const Text(
                  '未保存前只是草稿；检查后还需明确确认。SCREEN 只表示待人工审阅，真正的 Agent 筛查尚未提供。',
                ),
                const SizedBox(height: 16),
                FilledButton(
                  onPressed: _data.editable ? _confirm : null,
                  child: const Text('检查并保存设置'),
                ),
              ],
              if (_data.error != null ||
                  _data.message != null ||
                  _localError != null)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Semantics(
                    liveRegion: true,
                    child: Text(_data.error ?? _localError ?? _data.message!),
                  ),
                ),
              const SizedBox(height: 12),
              OutlinedButton(
                onPressed: busy ? null : () => _data.load(),
                child: Text(_data.uncertain ? '只重新读取当前设置' : '重新读取当前设置'),
              ),
            ],
          ],
        ),
        ),
      ),
    );
  }
}
