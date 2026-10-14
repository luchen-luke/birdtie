import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;
import '../app/birdtie_app_bar.dart';
import '../auth/birdtie_auth_controller.dart';
import 'close_connections_api.dart';
import 'close_connections_controller.dart';
import 'now_ui_tokens.dart';

class CloseConnectionsPage extends StatefulWidget {
  const CloseConnectionsPage({
    super.key,
    required this.auth,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.bindingCurrent,
  });
  final BirdtieAuthController auth;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final bool Function()? bindingCurrent;
  @override
  State<CloseConnectionsPage> createState() => _CloseConnectionsPageState();
}

class _CloseConnectionsPageState extends State<CloseConnectionsPage> {
  CloseConnectionsController _create() => CloseConnectionsController(
    api: CloseConnectionsAPI(
      client: widget.client,
      apiBaseUrl: widget.apiBaseUrl,
    ),
    identity: () => CloseConnectionsIdentity(
      widget.auth.authorizationHeader,
      widget.auth.accountID,
      widget.organizationWorkspaceID?.call(),
      sourceCurrent: widget.bindingCurrent?.call() != false,
    ),
  );
  late CloseConnectionsController _data = _create();
  Timer? _timer;
  DialogRoute<bool>? _dialog;
  NavigatorState? _navigator;
  @override
  void initState() {
    super.initState();
    widget.auth.addListener(_identity);
    widget.workspaceChanges?.addListener(_identity);
    _data.addListener(_changed);
    unawaited(_data.load());
    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (!mounted) return;
      _data.expire();
      if (!_data.current || _data.stale) _closeDialog();
    });
  }

  void _changed() {
    if (!mounted) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  void _closeDialog({bool defer = false}) {
    final route = _dialog, nav = _navigator;
    _dialog = null;
    void close() {
      if (nav?.mounted == true && route?.isActive == true) {
        nav!.removeRoute(route!);
      }
    }

    if (defer ||
        SchedulerBinding.instance.schedulerPhase ==
            SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) => close());
    } else {
      close();
    }
  }

  void _identity() {
    final generation = _data.generation;
    _data.sync();
    if (generation != _data.generation) _closeDialog();
  }

  @override
  void didUpdateWidget(CloseConnectionsPage old) {
    super.didUpdateWidget(old);
    if (old.auth != widget.auth) {
      old.auth.removeListener(_identity);
      widget.auth.addListener(_identity);
    }
    if (old.workspaceChanges != widget.workspaceChanges) {
      old.workspaceChanges?.removeListener(_identity);
      widget.workspaceChanges?.addListener(_identity);
    }
    if (old.auth != widget.auth ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.workspaceChanges != widget.workspaceChanges ||
        old.organizationWorkspaceID != widget.organizationWorkspaceID ||
        old.bindingCurrent != widget.bindingCurrent) {
      _closeDialog();
      _data.removeListener(_changed);
      _data.dispose();
      _data = _create();
      _data.addListener(_changed);
      final data = _data;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && identical(data, _data)) unawaited(data.load());
      });
    } else {
      _identity();
    }
  }

  Future<void> _review(CloseConnection entry) async {
    final data = _data,
        epoch = data.generation,
        deadline = data.list?.validUntil;
    if (deadline == null ||
        !data.reviewCurrent(entry, epoch, deadline) ||
        _dialog != null) {
      return;
    }
    final add = !entry.isClose, nav = Navigator.of(context);
    _navigator = nav;
    final route = DialogRoute<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(
          add ? '设为亲密好友？' : '撤销亲密好友？',
          style: NowUiTokens.of(context).sheetTitle,
        ),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                entry.displayName,
                style: NowUiTokens.of(context).placeTitle,
              ),
              const SizedBox(height: 12),
              Text(
                add
                    ? '这位当前好友将能查看你选择“亲密好友”范围的资料和意图。'
                    : '这位好友将不再按“亲密好友”范围查看你的资料和意图；仍保留普通好友关系。',
              ),
              const SizedBox(height: 12),
              const Text('只管理你自己的可见范围；不会开放私聊、Agent 资料或记忆。你可以随时撤销。'),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: Text(add ? '确认设为亲密好友' : '确认撤销亲密好友'),
          ),
        ],
      ),
    );
    _dialog = route;
    final accepted = await nav.push(route);
    if (identical(_dialog, route)) _dialog = null;
    if (!mounted ||
        !identical(data, _data) ||
        accepted != true ||
        !data.reviewCurrent(entry, epoch, deadline)) {
      return;
    }
    await data.confirm(entry, add: add, epoch: epoch, deadline: deadline);
  }

  @override
  void dispose() {
    widget.auth.removeListener(_identity);
    widget.workspaceChanges?.removeListener(_identity);
    _timer?.cancel();
    _data.removeListener(_changed);
    _data.dispose();
    _closeDialog(defer: true);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final tokens = NowUiTokens.of(context), list = _data.list;
    return Scaffold(
      appBar: birdtieAppBar(
        context,
        title: const Text('亲密好友'),
        actions: [
          IconButton(
            tooltip: '刷新亲密好友名单',
            onPressed: _data.current && !_data.busy ? () => _data.load() : null,
            icon: const Icon(Icons.refresh),
          ),
        ],
      ),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            Text('决定谁可以查看亲密好友内容', style: tokens.sheetTitle),
            const SizedBox(height: 8),
            Text(
              '从当前好友中明确选择。仅影响你自己设为“亲密好友”范围的资料和意图，不会授予 Agent 或模型新的访问权限。',
              style: tokens.body,
            ),
            const SizedBox(height: 16),
            if (!_data.current)
              Text('请使用当前个人账号管理；账号或工作区变化后，请返回并重新打开。', style: tokens.body)
            else ...[
              if (_data.busy)
                const LinearProgressIndicator(semanticsLabel: '正在核对亲密好友名单'),
              if (_data.message != null)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Text(_data.message!, style: tokens.body),
                ),
              if (_data.unknown || _data.readFailed)
                FilledButton(
                  onPressed: !_data.busy ? () => _data.load() : null,
                  child: const Text('核对当前名单'),
                ),
              if (list != null &&
                  _data.stale &&
                  !_data.busy &&
                  !_data.unknown &&
                  !_data.readFailed) ...[
                Text('当前关系核验已到期。刷新后再确认修改。', style: tokens.body),
                TextButton(
                  onPressed: () => _data.load(),
                  child: const Text('刷新后继续'),
                ),
              ],
              if (list != null && list.connections.isEmpty && !_data.busy)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 16),
                  child: Text(
                    '还没有当前好友。先互相接受好友申请，再从这里选择亲密好友。',
                    style: tokens.body,
                  ),
                ),
              if (list != null && list.connections.isNotEmpty) ...[
                Text(
                  '已选 ${list.connections.where((v) => v.isClose).length} 位亲密好友',
                  style: tokens.metadata,
                ),
                const SizedBox(height: 8),
                for (final entry in list.connections)
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: 12),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(entry.displayName, style: tokens.placeTitle),
                        const SizedBox(height: 4),
                        Text(
                          entry.isClose ? '已选为亲密好友' : '当前好友',
                          style: tokens.metadata,
                        ),
                        const SizedBox(height: 8),
                        OutlinedButton.icon(
                          key: ValueKey('close-person:${entry.personID}'),
                          onPressed: _data.ready ? () => _review(entry) : null,
                          icon: Icon(
                            entry.isClose
                                ? Icons.person_remove_outlined
                                : Icons.person_add_outlined,
                          ),
                          label: Text(entry.isClose ? '撤销亲密好友' : '设为亲密好友'),
                        ),
                        const Divider(height: 24),
                      ],
                    ),
                  ),
              ],
              if (list?.truncated == true)
                Text('仅展示前 100 位当前好友；未显示的好友不能从此名单选择。', style: tokens.metadata),
            ],
          ],
        ),
      ),
    );
  }
}
