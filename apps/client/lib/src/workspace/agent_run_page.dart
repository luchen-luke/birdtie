import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import 'agent_run_api.dart';
import 'agent_run_controller.dart';
import 'agent_run_pending_store.dart';
import 'now_ui_tokens.dart';

class AgentRunPage extends StatefulWidget {
  const AgentRunPage({
    super.key,
    required this.auth,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.sourceCurrent,
    this.pendingStore = const SecureAgentRunPendingStore(),
  });
  final BirdtieAuthController auth;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final bool Function()? sourceCurrent;
  final AgentRunPendingStore pendingStore;
  @override
  State<AgentRunPage> createState() => _AgentRunPageState();
}

class _AgentRunPageState extends State<AgentRunPage> {
  late final AgentRunAPI api;
  late final AgentRunController c;
  @override
  void initState() {
    super.initState();
    api = AgentRunAPI(client: widget.client, apiBaseUrl: widget.apiBaseUrl);
    final auth = widget.auth,
        workspace = widget.organizationWorkspaceID,
        source = widget.sourceCurrent;
    c = AgentRunController(
      api: api,
      authorizationHeader: () => auth.authorizationHeader,
      ownerID: () => auth.accountID,
      organizationWorkspaceID: workspace,
      sourceCurrent: source,
      identityChanges: Listenable.merge([auth, widget.workspaceChanges]),
      pendingStore: widget.pendingStore,
    );
    unawaited(c.initialize());
  }

  @override
  void didUpdateWidget(AgentRunPage old) {
    super.didUpdateWidget(old);
    if (!identical(old.auth, widget.auth) ||
        !identical(old.client, widget.client) ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        !identical(old.workspaceChanges, widget.workspaceChanges) ||
        !identical(
          old.organizationWorkspaceID,
          widget.organizationWorkspaceID,
        ) ||
        !identical(old.sourceCurrent, widget.sourceCurrent) ||
        !identical(old.pendingStore, widget.pendingStore)) {
      c.retire();
    }
  }

  @override
  void dispose() {
    c.dispose();
    api.dispose();
    super.dispose();
  }

  String _time(DateTime t) {
    final d = t.toLocal();
    return '${d.month}月${d.day}日 ${d.hour.toString().padLeft(2, '0')}:${d.minute.toString().padLeft(2, '0')}';
  }

  Future<void> _stop() async {
    final checked = await c.prepareCancel();
    if (!mounted || checked == null || !c.current) return;
    final yes = await showDialog<bool>(
      context: context,
      builder: (ctx) => AnimatedBuilder(
        animation: c,
        builder: (_, __) {
          final valid =
              c.current && identical(c.review, checked) && checked.fresh;
          return AlertDialog(
            title: const Text('停止这项处理？'),
            content: SingleChildScrollView(
              child: Text(
                valid
                    ? '当前状态：${agentRunStates[checked.record.state]}\n停止后不再继续这项处理。原文字与已经形成的候选保留。'
                    : '检查已失效，请返回重新读取当前状态。',
              ),
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(ctx, false),
                child: const Text('保留处理'),
              ),
              FilledButton(
                onPressed: valid ? () => Navigator.pop(ctx, true) : null,
                child: const Text('确认停止'),
              ),
            ],
          );
        },
      ),
    );
    if (mounted && yes == true) await c.confirmCancel(checked);
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: c,
    builder: (ctx, _) {
      final current = c.current, v = c.selected;
      return Scaffold(
        appBar: AppBar(title: const Text('智能体处理状态')),
        body: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            if (!current)
              Text(c.error ?? '请返回后重新打开。')
            else ...[
              if (c.busy) const LinearProgressIndicator(),
              if (c.error != null)
                Semantics(liveRegion: true, child: Text(c.error!)),
              if (c.notice != null)
                Semantics(liveRegion: true, child: Text(c.notice!)),
              if (c.pending != null)
                FilledButton(
                  onPressed: c.busy ? null : () => unawaited(c.recover()),
                  child: const Text('核实原停止操作'),
                ),
              if (c.items.isEmpty && !c.busy)
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 20),
                  child: Text('当前没有可显示的处理记录。'),
                ),
              for (final r in c.items)
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  title: Text(agentRunStates[r.state]!),
                  subtitle: Text('文字候选处理 · ${_time(r.createdAt)}'),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: c.busy ? null : () => unawaited(c.select(r.id)),
                ),
              Wrap(
                spacing: 12,
                runSpacing: 8,
                children: [
                  OutlinedButton(
                    onPressed: c.busy ? null : () => unawaited(c.load()),
                    child: const Text('重新读取'),
                  ),
                  if (c.nextCursor != null)
                    OutlinedButton(
                      onPressed: c.busy
                          ? null
                          : () => unawaited(c.load(more: true)),
                      child: const Text('加载更多'),
                    ),
                ],
              ),
              if (v != null) ...[
                const Divider(height: 32),
                Text('所选处理', style: NowUiTokens.of(context).sheetTitle),
                Text('当前状态：${agentRunStates[v.state]}'),
                if (v.state == 'SUCCEEDED')
                  const Text('已形成待检查的候选，是否保存为本人记忆仍由你另行确认。'),
                if (v.state == 'WAITING_CONFIRMATION')
                  const Text('尚未取得继续处理所需的具体许可。'),
                if (!v.terminal)
                  FilledButton(
                    onPressed: c.busy || c.pending != null ? null : _stop,
                    child: const Text('检查并停止'),
                  ),
                OutlinedButton(
                  onPressed: c.busy ? null : () => unawaited(c.select(v.id)),
                  child: const Text('读取所选状态'),
                ),
              ],
            ],
          ],
        ),
      );
    },
  );
}
