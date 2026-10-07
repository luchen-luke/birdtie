import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import '../auth/birdtie_auth_controller.dart';
import 'connections.dart';
import 'connection_request_review_controller.dart';
import 'connection_request_review_pending_store.dart';
import 'supplier_profile_api.dart' show supplierDate;

class ConnectionRequestReviewPage extends StatefulWidget {
  const ConnectionRequestReviewPage({
    super.key,
    required this.auth,
    required this.source,
    required this.requestID,
    this.initialAction,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.current,
    this.now,
    this.pendingStore,
  });
  final BirdtieAuthController auth;
  final ConnectionSource source;
  final String requestID;
  final String? initialAction;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final bool Function()? current;
  final DateTime Function()? now;
  final ConnectionReviewPendingStore? pendingStore;
  @override
  State<ConnectionRequestReviewPage> createState() => _ReviewState();
}

class _ReviewState extends State<ConnectionRequestReviewPage> {
  late final ConnectionRequestReviewController _data;
  late final BirdtieAuthController _auth;
  late final Listenable? _workspace;
  bool _retired = false;
  DialogRoute<bool>? _dialog;
  NavigatorState? _dialogNavigator;
  @override
  void initState() {
    super.initState();
    _auth = widget.auth;
    _workspace = widget.workspaceChanges;
    _data = ConnectionRequestReviewController(
      requestID: widget.requestID,
      source: widget.source,
      accountID: () => widget.auth.accountID,
      authorizationHeader: () => widget.auth.authorizationHeader,
      organizationWorkspaceID: () => widget.organizationWorkspaceID?.call(),
      current: () => mounted && !_retired && (widget.current?.call() ?? true),
      now: widget.now,
      pendingStore: widget.pendingStore,
      operationReceipts: true,
    );
    _data.addListener(_changed);
    _auth.addListener(_identityChanged);
    _workspace?.addListener(_identityChanged);
    unawaited(_data.load());
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  void _closeDialog() {
    final d = _dialog, n = _dialogNavigator;
    _dialog = null;
    _dialogNavigator = null;
    void close() {
      if (n?.mounted == true && d?.isActive == true) n!.removeRoute(d!);
    }

    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) => close());
    } else {
      close();
    }
  }

  void _identityChanged() {
    _data.synchronizeIdentity();
    if (_data.retired) {
      _retired = true;
      _closeDialog();
    }
  }

  @override
  void didUpdateWidget(ConnectionRequestReviewPage old) {
    super.didUpdateWidget(old);
    if (old.auth != widget.auth ||
        old.source != widget.source ||
        old.requestID != widget.requestID ||
        old.initialAction != widget.initialAction ||
        old.workspaceChanges != widget.workspaceChanges ||
        old.organizationWorkspaceID != widget.organizationWorkspaceID ||
        old.current != widget.current ||
        old.pendingStore != widget.pendingStore ||
        old.now != widget.now) {
      _retired = true;
    }
    _identityChanged();
  }

  String _state(String s) => switch (s) {
    'pending' => '待处理',
    'accepted' => '已接受',
    'declined' => '已拒绝',
    'withdrawn' => '已撤回',
    'expired' => '已过期',
    _ => '状态待核实',
  };
  String _action(String s) => switch (s) {
    'accept' => '接受',
    'decline' => '拒绝',
    _ => '撤回',
  };
  String _consequence(ContactRequest r, String action) => switch (action) {
    'accept' =>
      r.scope == 'friend'
          ? '接受此好友申请后，原服务将建立好友关系。不会自动打开聊天或发送消息。'
          : '接受此联系申请后，原服务将建立此申请对应的对话。不会自动打开对话或发送消息。',
    'decline' => '拒绝此申请，不建立好友关系或对话。',
    _ => '撤回自己发送的这条申请，对方不能再接受此申请。',
  };
  Future<void> _review(String action) async {
    final approval = _data.preview(action);
    if (approval == null) return;
    final r = approval.request, n = Navigator.of(context);
    final route = DialogRoute<bool>(
      context: context,
      builder: (inner) => AlertDialog(
        title: Text('确认${_action(action)}这条申请'),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(r.otherName),
              const SizedBox(height: 8),
              Text(r.scope == 'friend' ? '好友申请' : '联系申请'),
              if (r.note.isNotEmpty) Text(r.note),
              Text('有效期至 ${supplierDate(r.expiresAt!)}'),
              const SizedBox(height: 16),
              Text(_consequence(r, action)),
              if (r.pendingReview) const Text('待人工审阅；尚未进行 Agent 筛查。'),
              const SizedBox(height: 8),
              const Text('确认只针对当前这条申请及所选操作。'),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(inner, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(inner, true),
            child: Text('确认${_action(action)}'),
          ),
        ],
      ),
    );
    _dialog = route;
    _dialogNavigator = n;
    final confirmed = await n.push(route);
    if (_dialog == route) {
      _dialog = null;
      _dialogNavigator = null;
    }
    if (!mounted || confirmed != true) return;
    _identityChanged();
    if (_retired || _data.retired) return;
    await _data.submit(approval);
  }

  @override
  void dispose() {
    _auth.removeListener(_identityChanged);
    _workspace?.removeListener(_identityChanged);
    _data.removeListener(_changed);
    _data.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final r = _data.request;
    return Scaffold(
      appBar: AppBar(title: const Text('审阅申请')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            if (_data.retired || _retired) ...[
              const Text('身份或来源已变化，请返回当前收件箱重新打开。'),
              TextButton(
                onPressed: () => Navigator.of(context).maybePop(),
                child: const Text('返回收件箱'),
              ),
            ] else ...[
              if (_data.busy) const LinearProgressIndicator(),
              if (widget.initialAction case final action?)
                Text('你准备${_action(action)}此申请。请先核对，再确认具体操作。'),
              if (r != null) ...[
                Text(
                  r.otherName,
                  style: Theme.of(context).textTheme.titleLarge,
                ),
                const SizedBox(height: 12),
                Text(
                  '${r.scope == 'friend' ? '好友申请' : '联系申请'} · ${_state(r.state)}',
                ),
                if (r.pendingReview)
                  const Padding(
                    padding: EdgeInsets.only(top: 8),
                    child: Text('待人工审阅；尚未进行 Agent 筛查。'),
                  ),
                if (r.note.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: 16),
                    child: Text(r.note),
                  ),
                Text('有效期至 ${supplierDate(r.expiresAt!)}'),
                const SizedBox(height: 20),
                if (r.direction == 'incoming') ...[
                  FilledButton(
                    onPressed: _data.canAct('accept')
                        ? () => _review('accept')
                        : null,
                    child: const Text('审阅接受'),
                  ),
                  OutlinedButton(
                    onPressed: _data.canAct('decline')
                        ? () => _review('decline')
                        : null,
                    child: const Text('审阅拒绝'),
                  ),
                ] else
                  OutlinedButton(
                    onPressed: _data.canAct('withdraw')
                        ? () => _review('withdraw')
                        : null,
                    child: const Text('审阅撤回'),
                  ),
              ],
              if (_data.receipt case final receipt?)
                Text('本次操作回执：${_state(receipt.state)}'),
              if (_data.message case final message?)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Text(message),
                ),
              if (_data.pendingReference case final reference?)
                Text(
                  '本机待核实操作：${switch (reference.action) {
                    'accept' => '接受申请',
                    'decline' => '拒绝申请',
                    _ => '撤回申请',
                  }}。关闭重开也不会重复提交；${reference.hasServiceOperation ? '会查询原服务操作回执，当前申请状态不能代替该回执。' : '这里只保存本机旧引用，没有原服务操作回执，不能升级为服务操作编号。'}',
                ),
              TextButton.icon(
                onPressed: _data.busy ? null : _data.load,
                icon: const Icon(Icons.refresh),
                label: const Text('核实当前申请'),
              ),
              const Text('列表仅显示最近 100 条申请。未找到、读取失败或当前状态相同，都不能代替本次操作回执。'),
            ],
          ],
        ),
      ),
    );
  }
}
