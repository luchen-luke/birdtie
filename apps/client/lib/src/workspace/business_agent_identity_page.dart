import 'dart:async';
import 'package:flutter/material.dart';
import 'business_api.dart';
import 'business_agent_identity_controller.dart';

class BusinessAgentIdentityPage extends StatefulWidget {
  const BusinessAgentIdentityPage({
    super.key,
    required this.api,
    required this.businessID,
    required this.accountID,
    required this.authorizationHeader,
    required this.workspaceID,
    required this.currentBusinessID,
    required this.bindingCurrent,
    required this.sourceFrame,
    required this.identityChanges,
  });
  final BusinessApi api;
  final String businessID;
  final String? Function() accountID,
      authorizationHeader,
      workspaceID,
      currentBusinessID;
  final bool Function() bindingCurrent;
  final Object? Function() sourceFrame;
  final Listenable identityChanges;
  @override
  State<BusinessAgentIdentityPage> createState() =>
      _BusinessAgentIdentityPageState();
}

class _BusinessAgentIdentityPageState extends State<BusinessAgentIdentityPage> {
  late final BusinessAgentIdentityController c;
  bool _retired = false;
  @override
  void initState() {
    super.initState();
    c = BusinessAgentIdentityController(
      api: widget.api,
      businessID: widget.businessID,
      accountID: () => widget.accountID(),
      authorizationHeader: () => widget.authorizationHeader(),
      workspaceID: () => widget.workspaceID(),
      currentBusinessID: () => widget.currentBusinessID(),
      bindingCurrent: () => !_retired && widget.bindingCurrent(),
      sourceFrame: () => widget.sourceFrame(),
      identityChanges: widget.identityChanges,
    );
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && !_retired) unawaited(c.load());
    });
  }

  @override
  void didUpdateWidget(covariant BusinessAgentIdentityPage old) {
    super.didUpdateWidget(old);
    if (!identical(old.api, widget.api) ||
        old.businessID != widget.businessID ||
        !identical(old.accountID, widget.accountID) ||
        !identical(old.authorizationHeader, widget.authorizationHeader) ||
        !identical(old.workspaceID, widget.workspaceID) ||
        !identical(old.currentBusinessID, widget.currentBusinessID) ||
        !identical(old.bindingCurrent, widget.bindingCurrent) ||
        !identical(old.sourceFrame, widget.sourceFrame) ||
        !identical(old.identityChanges, widget.identityChanges)) {
      _retired = true;
      c.synchronize();
    }
  }

  Future<void> _review() async {
    if (!c.prepareReview()) return;
    final approved = c.review!;
    final yes = await showDialog<bool>(
      context: context,
      builder: (d) => AlertDialog(
        title: const Text('确认建立商家智能体身份'),
        content: SingleChildScrollView(
          child: Text(
            '商家：${approved.name}\n经营权已核验，本次按当前核验版本建立身份。\n\n仅建立身份，智能体仍暂停；不会启用模型、读取资料或执行工具。',
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(d, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(d, true),
            child: const Text('确认建立'),
          ),
        ],
      ),
    );
    if (!mounted) return;
    if (yes == true) {
      await c.establish(approved);
    } else {
      c.clearReview();
    }
  }

  @override
  void dispose() {
    c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: c,
    builder: (context, _) => Scaffold(
      appBar: AppBar(title: const Text('商家智能体身份')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            const Text('仅建立身份，智能体仍暂停；不会启用模型、读取资料或执行工具。'),
            const SizedBox(height: 20),
            if (c.busy)
              const LinearProgressIndicator(semanticsLabel: '正在核实商家身份'),
            if (c.value != null) ...[
              Text(
                c.value!.name,
                style: Theme.of(context).textTheme.titleLarge,
              ),
              Text(
                '经营权：${c.value!.claimStatus == 'verified' && c.value!.claimState == 'verified' ? '已核验' : '尚未完成当前核验'}',
              ),
              Text(
                c.value!.agentID == null
                    ? '尚未建立身份'
                    : c.value!.agentStatus == 'retired'
                    ? '身份已退役'
                    : '身份已建立 · 暂停中',
              ),
            ],
            if (c.error != null)
              Semantics(liveRegion: true, child: Text(c.error!)),
            if (c.message != null)
              Semantics(liveRegion: true, child: Text(c.message!)),
            const SizedBox(height: 16),
            if (c.permitted)
              OutlinedButton(
                style: OutlinedButton.styleFrom(
                  minimumSize: const Size(48, 48),
                ),
                onPressed: c.busy ? null : c.load,
                child: Text(c.uncertain ? '核实当前身份（只读取）' : '重新读取当前身份'),
              ),
            if (c.permitted &&
                !c.uncertain &&
                c.value?.eligible(DateTime.now().toUtc()) == true)
              FilledButton(
                style: FilledButton.styleFrom(minimumSize: const Size(48, 48)),
                onPressed: c.busy ? null : _review,
                child: const Text('检查并建立身份'),
              ),
            if (!c.permitted) const Text('请返回工作台，以当前本人身份重新选择商家。'),
          ],
        ),
      ),
    ),
  );
}
