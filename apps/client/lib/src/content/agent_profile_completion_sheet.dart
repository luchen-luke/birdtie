import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import 'agent_profile_completion_api.dart';
import 'agent_profile_completion_controller.dart';
import 'agent_profile_completion_pending_store.dart';

class AgentProfileCompletionSheet extends StatefulWidget {
  const AgentProfileCompletionSheet({
    super.key,
    required this.auth,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.pendingStore,
  });
  final BirdtieAuthController auth;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final AgentProfileCompletionPendingStore? pendingStore;
  @override
  State<AgentProfileCompletionSheet> createState() =>
      _AgentProfileCompletionSheetState();
}

class _AgentProfileCompletionSheetState
    extends State<AgentProfileCompletionSheet> {
  late final AgentProfileCompletionController c;
  late final BirdtieAuthController auth;
  late final Listenable? workspace;
  Timer? _expiry;
  @override
  void initState() {
    super.initState();
    auth = widget.auth;
    workspace = widget.workspaceChanges;
    c = AgentProfileCompletionController(
      authorizationHeader: () => auth.authorizationHeader,
      accountID: () => auth.accountID,
      organizationWorkspaceID: widget.organizationWorkspaceID,
      api: AgentProfileCompletionAPI(
        client: widget.client,
        apiBaseUrl: widget.apiBaseUrl,
      ),
      pendingStore: widget.pendingStore,
    )..addListener(_changed);
    auth.addListener(c.identityChanged);
    workspace?.addListener(c.identityChanged);
    unawaited(c.load());
  }

  void _changed() {
    _expiry?.cancel();
    final end = c.preview?.expiresAt;
    if (end != null && end.isAfter(DateTime.now().toUtc())) {
      _expiry = Timer(end.difference(DateTime.now().toUtc()), () {
        if (mounted) setState(() {});
      });
    }
    if (mounted) setState(() {});
  }

  @override
  void didUpdateWidget(AgentProfileCompletionSheet old) {
    super.didUpdateWidget(old);
    if (old.auth != widget.auth ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.workspaceChanges != widget.workspaceChanges ||
        old.organizationWorkspaceID != widget.organizationWorkspaceID ||
        old.pendingStore != widget.pendingStore) {
      c.retire();
    }
  }

  @override
  void dispose() {
    _expiry?.cancel();
    auth.removeListener(c.identityChanged);
    workspace?.removeListener(c.identityChanged);
    c.removeListener(_changed);
    c.dispose();
    super.dispose();
  }

  String deadline(DateTime v) {
    final d = v.toLocal();
    return '${d.year}年${d.month}月${d.day}日 ${d.hour.toString().padLeft(2, '0')}:${d.minute.toString().padLeft(2, '0')}';
  }

  Widget action(String label, VoidCallback? run, {bool primary = false}) =>
      Padding(
        padding: const EdgeInsets.only(top: 12),
        child: primary
            ? FilledButton(
                style: FilledButton.styleFrom(minimumSize: const Size(0, 48)),
                onPressed: run,
                child: Text(label),
              )
            : OutlinedButton(
                style: OutlinedButton.styleFrom(minimumSize: const Size(0, 48)),
                onPressed: run,
                child: Text(label),
              ),
      );
  @override
  Widget build(BuildContext context) {
    final p = c.preview, s = c.suggestions;
    return Scaffold(
      appBar: AppBar(title: const Text('补齐活动偏好')),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: EdgeInsets.fromLTRB(
            20,
            20,
            20,
            32 + MediaQuery.viewInsetsOf(context).bottom,
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text(
                '用本人已确认的声明，补一项资料',
                style: Theme.of(context).textTheme.headlineSmall,
              ),
              const SizedBox(height: 12),
              const Text('当前代表本人。仅补齐私密活动偏好的空项，其它八项保持原值；不会发布或发送给模型。'),
              const SizedBox(height: 20),
              if (c.busy) const LinearProgressIndicator(),
              if (c.message != null)
                Semantics(liveRegion: true, child: Text(c.message!)),
              if (c.error != null)
                Padding(
                  padding: const EdgeInsets.only(top: 12),
                  child: Semantics(liveRegion: true, child: Text(c.error!)),
                ),
              if (c.unknown || c.hasPending && p == null) ...[
                const SizedBox(height: 12),
                const Text('本机仅保留操作引用，不保留来源正文、审阅内容或登录凭据。核实只读取原操作，不会恢复旧批准。'),
                action(
                  '核实原操作',
                  c.busy || c.retired ? null : () => unawaited(c.verify()),
                  primary: true,
                ),
              ] else if (p != null) ...[
                const SizedBox(height: 24),
                Text('检查这一项', style: Theme.of(context).textTheme.titleLarge),
                const SizedBox(height: 12),
                const Text('原值：未填写'),
                const SizedBox(height: 8),
                Text('保存后：${p.source.value}'),
                const SizedBox(height: 12),
                Text(p.explanation),
                const SizedBox(height: 12),
                Text('来源记忆有效至：${deadline(p.source.validUntil)}'),
                Text('本次审阅有效至：${deadline(p.expiresAt)}'),
                const SizedBox(height: 12),
                const Text('确认后形成一条独立人工声明。来源记忆以后到期或撤回，不会自动删除这项资料。'),
                action(
                  '确认补齐这一项',
                  c.canAccept ? () => unawaited(c.accept()) : null,
                  primary: true,
                ),
                if (!c.canAccept && !c.busy) ...[
                  const Text('审阅已失效，请核实原操作后重新检查。'),
                  action(
                    '核实原操作',
                    c.retired ? null : () => unawaited(c.verify()),
                  ),
                ],
              ] else if (s != null && !s.alreadySet) ...[
                const SizedBox(height: 20),
                for (final source in s.sources)
                  action(
                    '检查：${source.value}',
                    c.canReview ? () => unawaited(c.review(source)) : null,
                  ),
              ],
              if (!c.hasPending && p == null && !c.denied && !c.retired)
                action('重新读取当前来源', c.busy ? null : () => unawaited(c.load())),
              action('关闭，保留当前资料', () => Navigator.of(context).pop()),
            ],
          ),
        ),
      ),
    );
  }
}
