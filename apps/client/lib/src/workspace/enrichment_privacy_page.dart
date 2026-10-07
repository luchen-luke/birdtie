import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import 'agent_multi_candidate_pending_store.dart';
import 'enrichment_privacy_api.dart';
import 'enrichment_privacy_controller.dart';

class EnrichmentPrivacyPage extends StatefulWidget {
  const EnrichmentPrivacyPage({
    super.key,
    required this.auth,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.current,
    this.onReviewSources,
    this.pendingStore,
    this.now,
  });
  final BirdtieAuthController auth;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final bool Function()? current;
  final VoidCallback? onReviewSources;
  final AgentMultiCandidatePendingStore? pendingStore;
  final DateTime Function()? now;
  @override
  State<EnrichmentPrivacyPage> createState() => _EnrichmentPrivacyPageState();
}

class _EnrichmentPrivacyPageState extends State<EnrichmentPrivacyPage> {
  late final _source = (
    widget.auth,
    widget.client,
    widget.apiBaseUrl,
    widget.workspaceChanges,
    widget.organizationWorkspaceID,
    widget.current,
    widget.pendingStore,
    widget.now,
  );
  bool _retired = false;
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
              widget.pendingStore,
              widget.now,
            ) ||
        widget.current?.call() == false) {
      _retired = true;
      return false;
    }
    return true;
  }

  late final _data = EnrichmentPrivacyController(
    api: EnrichmentPrivacyAPI(client: _source.$2, apiBaseUrl: _source.$3),
    authorizationHeader: () => _source.$1.authorizationHeader,
    ownerID: () => _source.$1.accountID,
    organizationWorkspaceID: _source.$5,
    current: _current,
    pendingStore: _source.$7 ?? const SecureAgentMultiCandidatePendingStore(),
    now: _source.$8,
  );
  Timer? _timer;
  @override
  void initState() {
    super.initState();
    _source.$1.addListener(_identity);
    _source.$4?.addListener(_identity);
    _data.addListener(_changed);
    unawaited(_data.load());
  }

  void _identity() {
    _data.synchronizeIdentity();
  }

  void _changed() {
    if (!mounted) return;
    _timer?.cancel();
    final until = _data.inventory?.validUntil;
    if (until != null && _data.fresh) {
      _timer = Timer(until.difference(_data.now()), _data.expire);
    }
    setState(() {});
  }

  @override
  void didUpdateWidget(EnrichmentPrivacyPage old) {
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
  @override
  Widget build(BuildContext context) {
    final v = _data.inventory, a = _data.approval;
    return Scaffold(
      appBar: AppBar(title: const Text('本地分析许可')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            const Text('这里管理你明确允许的私人记录本地分析。不会开启模型、自动学习、公开分享或长期记忆。'),
            const SizedBox(height: 12),
            const Text('撤回会停止后续使用这项许可；已经消费的内容不会被召回，独立保存的记忆仍需在记忆管理中处理。'),
            const SizedBox(height: 8),
            const Text('清单是读取时的许可状态。尚未撤回且未到期，也不保证当前来源、任务或会话仍可用于分析。'),
            if (_data.busy)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 16),
                child: LinearProgressIndicator(),
              ),
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
                child: Text(_data.notice!, semanticsLabel: _data.notice),
              ),
            if (!_data.retired)
              Align(
                alignment: Alignment.centerLeft,
                child: OutlinedButton.icon(
                  onPressed: _data.busy ? null : () => unawaited(_data.load()),
                  icon: const Icon(Icons.refresh),
                  label: const Text('重新读取许可'),
                ),
              ),
            for (final p in _data.pending)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 8),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text('原操作待核实：不会恢复旧批准或自动重发。'),
                    OutlinedButton(
                      onPressed: _data.busy || _data.retired
                          ? null
                          : () => unawaited(_data.verifyPending(p)),
                      child: const Text('仅核实原操作当前状态'),
                    ),
                  ],
                ),
              ),
            if (v != null) ...[
              Text('读取时间：${_time(v.observedAt)}'),
              if (!_data.fresh) const Text('这份清单的读取期限已结束，请重新读取；这不是原许可的到期时间。'),
              if (v.truncated)
                const Text(
                  '优先显示读取时尚未撤回且未到期的许可，最多50项。撤回后重新读取可继续查看其余许可；未显示不代表不存在或已撤回。',
                ),
              if (v.grants.isEmpty)
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 16),
                  child: Text('当前没有可显示的本地分析许可。'),
                ),
              for (final g in v.grants)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 16),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        '私人记录 ${g.selection['momentId']}',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      Text(
                        '分析范围：${(g.selection['fields'] as List).map((f) => f == 'title' ? '标题' : '正文').join('、')}',
                      ),
                      Text(
                        '来源版本：${g.selection['momentRevision']}；许可版本：${g.revision}',
                      ),
                      Text('许可到期：${_time(g.expiresAt)}'),
                      Text(
                        g.revoked
                            ? '已撤回'
                            : !g.expiresAt.isAfter(v.observedAt)
                            ? '读取时已到期'
                            : '读取时尚未撤回且未到期',
                      ),
                      if (g.revoked)
                        Text(
                          '撤回时间：${_time(DateTime.parse(g.raw['revokedAt']).toUtc())}',
                        ),
                      if (!g.revoked)
                        OutlinedButton(
                          onPressed:
                              _data.busy ||
                                  !_data.fresh ||
                                  _data.pending.isNotEmpty
                              ? null
                              : () => _data.preview(g),
                          child: const Text('检查这项撤回'),
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
              Text(
                '仅撤回记录 ${a.grant.selection['momentId']} 的上述本地分析许可；不会删除原记录或独立保存的记忆。',
              ),
              Wrap(
                spacing: 12,
                runSpacing: 8,
                children: [
                  OutlinedButton(
                    onPressed: _data.busy ? null : _data.cancelPreview,
                    child: const Text('取消本次检查'),
                  ),
                  FilledButton(
                    onPressed: _data.busy || !_data.fresh
                        ? null
                        : () => unawaited(_data.withdraw(a)),
                    child: const Text('确认撤回这项许可'),
                  ),
                ],
              ),
            ],
            const SizedBox(height: 16),
            const Text('再次允许需要重新选择当前来源，并检查新预览后明确批准。撤回过的许可不会恢复或延长。'),
            if (widget.onReviewSources != null)
              Align(
                alignment: Alignment.centerLeft,
                child: TextButton(
                  onPressed: _data.personal && _current()
                      ? widget.onReviewSources
                      : null,
                  child: const Text('重新选择来源并检查'),
                ),
              ),
          ],
        ),
      ),
    );
  }
}
