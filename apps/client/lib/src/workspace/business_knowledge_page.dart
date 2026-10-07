import 'dart:async';
import 'package:flutter/material.dart';
import 'business_api.dart';
import 'business_knowledge_api.dart';
import 'business_knowledge_controller.dart';

class BusinessKnowledgePage extends StatefulWidget {
  const BusinessKnowledgePage({
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
  State<BusinessKnowledgePage> createState() => _BusinessKnowledgePageState();
}

class _BusinessKnowledgePageState extends State<BusinessKnowledgePage>
    with WidgetsBindingObserver {
  late BusinessKnowledgeController c;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _bind();
  }

  void _bind() {
    c = BusinessKnowledgeController(
      api: widget.api,
      businessID: widget.businessID,
      accountID: widget.accountID,
      authorizationHeader: widget.authorizationHeader,
      workspaceID: widget.workspaceID,
      currentBusinessID: widget.currentBusinessID,
      bindingCurrent: widget.bindingCurrent,
      sourceFrame: widget.sourceFrame,
      identityChanges: widget.identityChanges,
    );
    unawaited(c.load());
  }

  @override
  void didUpdateWidget(covariant BusinessKnowledgePage old) {
    super.didUpdateWidget(old);
    if (!identical(old.api, widget.api) ||
        old.businessID != widget.businessID ||
        !identical(old.identityChanges, widget.identityChanges) ||
        !identical(old.accountID, widget.accountID) ||
        !identical(old.authorizationHeader, widget.authorizationHeader) ||
        !identical(old.workspaceID, widget.workspaceID) ||
        !identical(old.currentBusinessID, widget.currentBusinessID) ||
        !identical(old.bindingCurrent, widget.bindingCurrent) ||
        !identical(old.sourceFrame, widget.sourceFrame)) {
      c.dispose();
      _bind();
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state != AppLifecycleState.resumed) {
      c.invalidate('返回前台后重新读取当前资料。');
    } else {
      unawaited(c.load());
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: c,
    builder: (context, _) => Scaffold(
      appBar: AppBar(title: const Text('商家资料回答')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            const Text(
              '以本人管理身份查看已核验资料',
              style: TextStyle(fontWeight: FontWeight.bold),
            ),
            if (c.businessName.isNotEmpty) Text('当前商家：${c.businessName}'),
            const SizedBox(height: 12),
            const Text('这是现有资料的只读规则回答。商家 Agent 和模型未启用；不会公开管理资料、联系他人或自动预约。'),
            const SizedBox(height: 20),
            if (c.loading)
              const LinearProgressIndicator(semanticsLabel: '正在读取当前商家资料'),
            if (c.error != null)
              Semantics(liveRegion: true, child: Text(c.error!)),
            if (!c.ready) ...[
              if (c.permitted)
                OutlinedButton(
                  onPressed: c.loading ? null : c.load,
                  child: const Text('重新读取当前资料'),
                ),
              if (!c.permitted) const Text('请返回工作台，以当前本人身份重新选择商家。'),
            ] else ...[
              const Text('想查看哪项资料？'),
              for (final q in businessKnowledgeQuestions)
                Padding(
                  padding: const EdgeInsets.only(top: 8),
                  child: Semantics(
                    selected: c.query == q,
                    child: OutlinedButton(
                      onPressed: c.loading ? null : () => c.selectQuestion(q),
                      style: OutlinedButton.styleFrom(
                        minimumSize: const Size(48, 48),
                      ),
                      child: Text(q == '商家简介' ? '商家简介（与介绍使用同一资料）' : q),
                    ),
                  ),
                ),
              if (c.query != null) ...[
                const SizedBox(height: 16),
                Text('当前问题：${c.query}'),
              ],
              if (c.query != null && businessKnowledgeNeedsPlace(c.query!)) ...[
                const SizedBox(height: 20),
                const Text('选择本次要查看的具体场地'),
                if (c.venues.isEmpty) const Text('暂无当前经营关系已核验的场地。不会猜测场地或预约信息。'),
                for (final v in c.venues)
                  Padding(
                    padding: const EdgeInsets.only(top: 8),
                    child: Semantics(
                      selected: c.placeID == v.id,
                      child: OutlinedButton(
                        onPressed: c.loading ? null : () => c.selectPlace(v.id),
                        style: OutlinedButton.styleFrom(
                          minimumSize: const Size(48, 48),
                        ),
                        child: Text(
                          '${v.name}${c.placeID == v.id ? '（已选）' : ''}',
                        ),
                      ),
                    ),
                  ),
              ],
              const SizedBox(height: 20),
              FilledButton(
                onPressed: c.canAsk ? c.ask : null,
                style: FilledButton.styleFrom(minimumSize: const Size(48, 48)),
                child: const Text('查看资料回答'),
              ),
            ],
            if (c.answer case final a?) ...[
              const Divider(height: 32),
              Semantics(
                liveRegion: true,
                child: Text(a.status == 'known' ? '来自这次读取的已核验资料' : '当前资料未知'),
              ),
              const SizedBox(height: 8),
              Text(a.text),
              for (final s in a.sources) ...[
                const SizedBox(height: 12),
                Text(
                  '${s.type == 'business_profile' ? '商家资料' : '所选场地资料'} · 版本 ${s.version}',
                ),
                Text('资料有效至 ${_stamp(s.validUntil.toLocal())}（设备本地时间）'),
              ],
              const SizedBox(height: 12),
              const Text('回答是本次资料快照，不保证此刻营业、有空位或预约成功。资料撤销、修改或到期后需重新读取。'),
            ],
          ],
        ),
      ),
    ),
  );
}

String _stamp(DateTime d) =>
    '${d.year}年${d.month}月${d.day}日 ${d.hour.toString().padLeft(2, '0')}:${d.minute.toString().padLeft(2, '0')}';
