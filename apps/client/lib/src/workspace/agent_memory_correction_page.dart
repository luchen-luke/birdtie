import 'dart:async';
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import 'agent_memory_candidate_api.dart'
    show HumanMemoryCandidate, candidateCategories;
import 'agent_memory_correction_api.dart';
import 'agent_memory_correction_controller.dart';
import 'agent_memory_correction_pending_store.dart';

class AgentMemoryCorrectionPage extends StatefulWidget {
  const AgentMemoryCorrectionPage({
    super.key,
    required this.auth,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.pendingStore,
    this.now,
    this.elapsed,
    this.current,
  });
  final BirdtieAuthController auth;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final AgentMemoryCorrectionPendingStore? pendingStore;
  final DateTime Function()? now;
  final Duration Function()? elapsed;
  final bool Function()? current;
  @override
  State<AgentMemoryCorrectionPage> createState() =>
      _AgentMemoryCorrectionPageState();
}

class _AgentMemoryCorrectionPageState extends State<AgentMemoryCorrectionPage> {
  late final _auth = widget.auth;
  late final _client = widget.client;
  late final _base = widget.apiBaseUrl;
  late final _workspaceChanges = widget.workspaceChanges;
  late final _workspace = widget.organizationWorkspaceID;
  late final _store = widget.pendingStore;
  late final _now = widget.now;
  late final _elapsed = widget.elapsed;
  late final _current = widget.current;
  late final AgentMemoryCorrectionController _data =
      AgentMemoryCorrectionController(
        authorizationHeader: () => _auth.authorizationHeader,
        accountID: () => _auth.accountID,
        organizationWorkspaceID: () => _workspace?.call(),
        api: AgentMemoryCorrectionAPI(client: _client, apiBaseUrl: _base),
        pendingStore: _store,
        now: _now,
        elapsed: _elapsed,
        current: _current,
      );
  final _summary = TextEditingController();
  final _scroll = ScrollController();
  CorrectableMemory? _memory;
  HumanMemoryCandidate? _candidate;
  String? _action, _category;
  Timer? _timer;
  bool _wasPending = false;
  bool _rebuildQueued = false;
  @override
  void initState() {
    super.initState();
    _auth.addListener(_identity);
    _workspaceChanges?.addListener(_identity);
    _data.addListener(_changed);
    unawaited(_data.load());
    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted) {
        _data.expireReview();
        _data.expireFieldEvidence();
        _data.expireSelfReview();
        setState(() {});
      }
    });
  }

  void _changed() {
    final completed =
        _wasPending &&
        !_data.hasPending &&
        _data.preview == null &&
        _data.message != null;
    if ((_wasPending && !_data.hasPending || _data.unknown) &&
        _data.preview == null) {
      _clearDraft();
    }
    _wasPending = _data.hasPending;
    if (!mounted) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      if (!_rebuildQueued) {
        _rebuildQueued = true;
        WidgetsBinding.instance.addPostFrameCallback((_) {
          _rebuildQueued = false;
          if (mounted) setState(() {});
        });
      }
    } else {
      setState(() {});
    }
    if (completed) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted) return;
        _data.identityChanged();
        if (_data.retired || !_scroll.hasClients) return;
        FocusScope.of(context).unfocus();
        _scroll.jumpTo(0);
      });
    }
  }

  void _identity() {
    _data.identityChanged();
    if (_data.retired) {
      _clearDraft();
    }
  }

  void _clearDraft() {
    _memory = null;
    _candidate = null;
    _action = _category = null;
    _summary.clear();
  }

  @override
  void didUpdateWidget(covariant AgentMemoryCorrectionPage old) {
    super.didUpdateWidget(old);
    if (!identical(_auth, widget.auth) ||
        !identical(_client, widget.client) ||
        _base != widget.apiBaseUrl ||
        !identical(_workspaceChanges, widget.workspaceChanges) ||
        !identical(_workspace, widget.organizationWorkspaceID) ||
        !identical(_store, widget.pendingStore) ||
        !identical(_now, widget.now) ||
        !identical(_elapsed, widget.elapsed) ||
        !identical(_current, widget.current)) {
      _data.retire();
      _clearDraft();
    } else {
      _identity();
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    _auth.removeListener(_identity);
    _workspaceChanges?.removeListener(_identity);
    _data.removeListener(_changed);
    _data.dispose();
    _summary.dispose();
    _scroll.dispose();
    super.dispose();
  }

  String _time(DateTime t) {
    final v = t.toLocal();
    return '${v.year}年${v.month}月${v.day}日 ${v.hour.toString().padLeft(2, '0')}:${v.minute.toString().padLeft(2, '0')}（本地时间）';
  }

  Widget _button(String label, VoidCallback? action, {bool primary = false}) =>
      Padding(
        padding: const EdgeInsets.symmetric(vertical: 6),
        child: ConstrainedBox(
          constraints: const BoxConstraints(
            minHeight: 52,
            minWidth: double.infinity,
          ),
          child: primary
              ? FilledButton(
                  onPressed: action,
                  child: Padding(
                    padding: const EdgeInsets.symmetric(vertical: 8),
                    child: Text(label, textAlign: TextAlign.center),
                  ),
                )
              : OutlinedButton(
                  onPressed: action,
                  child: Padding(
                    padding: const EdgeInsets.symmetric(vertical: 8),
                    child: Text(label, textAlign: TextAlign.center),
                  ),
                ),
        ),
      );
  void _choose(CorrectableMemory? m, HumanMemoryCandidate? c, String action) {
    if (!_data.canReview) return;
    setState(() {
      _memory = m;
      _candidate = c;
      _action = action;
      _category = null;
      _summary.text = m?.summary ?? '';
    });
  }

  Future<void> _review() async {
    if (_action == null || !_data.canReview) return;
    if (_memory != null) {
      await _data.reviewMemory(
        _memory!,
        _action!,
        summary: _summary.text,
        category: _category,
      );
    } else if (_candidate != null) {
      await _data.reviewCandidate(_candidate!, _action!, category: _category);
    }
  }

  Widget _draft() {
    final sourceCategory = _memory?.category ?? _candidate?.category;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(
          '准备${correctionActions[_action]}',
          style: Theme.of(context).textTheme.titleLarge,
        ),
        const SizedBox(height: 8),
        Text('对象版本 ${_memory?.version ?? _candidate?.version} · 当前本人'),
        if (_action == 'EDIT')
          TextField(
            controller: _summary,
            minLines: 2,
            maxLines: 6,
            maxLength: 400,
            enabled: _data.canReview,
            decoration: const InputDecoration(
              labelText: '修订后的本人声明',
              helperText: '其他补充内容、类别和可见范围保持原值。',
            ),
          ),
        if (_action == 'NEGATE') ...[
          const Text('请明确选择要否定的活动类别；不会根据记忆正文猜测。'),
          DropdownButtonFormField<String>(
            initialValue: _category,
            isExpanded: true,
            decoration: const InputDecoration(labelText: '本人明确不偏好的类别'),
            items: [
              if (sourceCategory != null)
                DropdownMenuItem(
                  value: sourceCategory,
                  child: Text(candidateCategories[sourceCategory]!),
                ),
            ],
            onChanged: _data.canReview
                ? (v) => setState(() => _category = v)
                : null,
          ),
          const Text('该类候选将持续被抑制。纠正声明删除或到期也不会恢复旧候选或旧批准。'),
        ],
        if (_action == 'DELETE' || _action == 'REJECT')
          const Text('不保留这条记忆或候选的正文与支持。原 Moment 不会被修改或删除。'),
        _button(
          '检查具体纠正版本',
          _data.canReview && (_action != 'NEGATE' || _category != null)
              ? () => unawaited(_review())
              : null,
          primary: true,
        ),
        _button('取消本地草稿', _data.canReview ? () => setState(_clearDraft) : null),
      ],
    );
  }

  Widget _reviewView(CorrectionPreview p) {
    final affected = p.raw['affected'] as List;
    final candidateCount = affected
        .where((v) => v['kind'] == 'CANDIDATE')
        .length;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text('检查这次具体纠正', style: Theme.of(context).textTheme.titleLarge),
        Text(
          '动作：${correctionActions[p.input['action']]} · 原版本 ${p.input['expectedVersion']}',
        ),
        Text('本次确认截止：${_time(p.expiresAt)}'),
        for (final m in p.memories) ...[
          const Divider(height: 28),
          Text(
            '记忆版本 ${m.version} · ${m.sourceType == 'EXPLICIT' ? '本人明确声明' : '待审预留形状'}',
          ),
          SelectableText(m.summary),
          Text('原记忆有效至：${_time(m.validUntil)}'),
          Text(
            m.raw['visibility'] == 'PRIVATE' ? '仅本人私密记忆' : '仅个人助理范围；本次仍由本人检查',
          ),
          if ((m.raw['structuredValue'] as Map).isNotEmpty)
            ExpansionTile(
              title: const Text('检查原补充内容'),
              children: [
                Padding(
                  padding: const EdgeInsets.all(12),
                  child: SelectableText(
                    const JsonEncoder.withIndent(
                      '  ',
                    ).convert(m.raw['structuredValue']),
                  ),
                ),
              ],
            ),
        ],
        if (p.input['action'] == 'EDIT') ...[
          const Text('确认后替换为：'),
          SelectableText(p.input['replacement']['summary']),
          const Text('原其他内容与可见范围保持，按当前版本保存。'),
        ],
        if (p.input['action'] == 'DELETE' || p.input['action'] == 'REJECT')
          Text(
            p.input['targetKind'] == 'MEMORY'
                ? '确认后丢弃所选记忆并清除正文，使用原 DELETED 终态。原 Moment 不改。'
                : '确认后拒绝所选候选并清除正文与支持，使用原 REJECTED 终态。原 Moment 不改。',
          ),
        if (p.input['action'] == 'NEGATE') ...[
          Text(
            '确认后保存本人纠正：${correctionNegativeStatements[p.input['category']]}',
          ),
          Text(
            '纠正声明有效至：${_time(correctionTime(p.raw['newMemoryValidUntil']))}',
          ),
          Text('这类当前候选 $candidateCount 条将失效。该类新候选持续被抑制；删除或到期不恢复旧批准。'),
        ],
        ExpansionTile(
          title: Text('检查全部受影响对象（${affected.length} 项）'),
          children: [
            for (final target in affected)
              Padding(
                padding: const EdgeInsets.all(12),
                child: SelectableText(
                  '${target['kind'] == 'MEMORY' ? '记忆' : '候选'} · 版本 ${target['version']}\n对象标识：${target['id']}',
                ),
              ),
          ],
        ),
        const SizedBox(height: 12),
        const Text(correctionExplanation),
        _button(
          '确认此版本的纠正',
          _data.canConfirm ? () => unawaited(_data.confirm()) : null,
          primary: true,
        ),
        _button(
          '关闭审阅并核实原操作',
          !_data.busy ? () => unawaited(_data.verify()) : null,
        ),
      ],
    );
  }

  Widget _fieldEvidence(CorrectableMemory m) {
    final detail = _data.fieldDetail;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        TextButton(
          onPressed: _data.canReadFieldEvidence(m) && !_data.fieldLoading
              ? () => unawaited(_data.readFieldEvidence(m)) : null,
          child: const Padding(
            padding: EdgeInsets.symmetric(vertical: 12),
            child: Text('查看来源与时间'),
          ),
        ),
        if (_data.fieldMemoryID == m.id) ...[
          if (_data.fieldLoading)
            const LinearProgressIndicator(semanticsLabel: '正在读取这条记忆的来源'),
          if (_data.fieldError != null)
            Semantics(liveRegion: true, child: Text(_data.fieldError!,
              style: TextStyle(color: Theme.of(context).colorScheme.error))),
          if (detail != null && detail.id == m.id) ...[
            const SizedBox(height: 8),
            Text('这条记忆的来源与时间', style: Theme.of(context).textTheme.titleSmall),
            if (detail.memory != null) ...[
              Text('记录创建时间：${_time(detail.createdAt!)}'),
              Text('记录更新时间：${_time(detail.updatedAt!)}'),
              Text('有效区间：${_time(detail.validFrom!)} 至 ${_time(detail.memory!.validUntil)}'),
            ],
            if (!detail.evidenceAvailable)
              const Text('当前未提供这条记忆的来源说明；不会猜测或补全。')
            else if (detail.memory == null)
              const Text('这条记忆已到期或已删除，当前详情不提供原内容。')
            else ...[
              Text(detail.inferred ? '性质：未经本人确认的推断，仅供检查。'
                  : '性质：本人明确声明，不等于已核验事实。'),
              const Text('来源：这条记忆的当前记录'),
              if (detail.declaredAt != null)
                Text('声明时间：${_time(detail.declaredAt!)}')
              else const Text('声明时间：未提供本人声明时间'),
              const Text('拍摄时间未采集。记录与读取时间不证明事情发生、到场或到访。'),
              const Text('这份单条来源说明不包含跨来源比较。'),
            ],
            Text('本次详情读取时间：${_time(detail.observedAt)}'),
            Text('详情快照有效至：${_time(detail.expiresAt)}'),
            const Text('查看来源不会批准更正、启用模型或自动写入。'),
          ],
        ],
      ],
    );
  }

  Widget _selfReview(CorrectableMemory m) {
    final view = _data.selfReview;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        TextButton(
          onPressed: _data.canReadSelfReview(m) && !_data.selfReviewLoading
              ? () => unawaited(_data.readSelfReview(m)) : null,
          child: const Padding(
            padding: EdgeInsets.symmetric(vertical: 12),
            child: Text('核对活动偏好与这条记忆', textAlign: TextAlign.center),
          ),
        ),
        if (_data.selfReviewMemoryID == m.id) ...[
          if (_data.selfReviewLoading)
            const LinearProgressIndicator(semanticsLabel: '正在读取本人同体声明'),
          if (_data.selfReviewError != null)
            Semantics(liveRegion: true, child: Text(_data.selfReviewError!,
              style: TextStyle(color: Theme.of(context).colorScheme.error))),
          if (view != null) ...[
            const SizedBox(height: 8),
            Text('一起核对这两份声明', style: Theme.of(context).textTheme.titleSmall),
            Text(view.profileStatus == 'UNCONFIGURED'
                ? '活动偏好：尚未设置，不会补全。'
                : view.categories.isEmpty ? '活动偏好：当前明确选择为空。'
                : '资料中的活动偏好：${view.categories.map((c) => candidateCategories[c]).join('、')}'),
            SelectableText('这条记忆的声明：${m.summary}'),
            Text('当前记忆版本 ${m.version} · 本人明确声明，不代表已核验事实。'),
            if (view.conflictCategories.isNotEmpty)
              Semantics(liveRegion: true, child: Text(
                '声明不一致：${view.conflictCategories.map((c) => candidateCategories[c]).join('、')} · 待本人确认',
              ))
            else const Text('本次所选来源未列出相反的明确活动类别偏好；不是全体无冲突。'),
            for (final claim in view.raw['fieldEvidenceSet']['claims'])
              if (claim['field'] == 'memory.summary' || claim['field'] == 'profile.preferredActivityTypes')
                Text('${claim['itemKind'] == 'profile' ? '活动偏好来源' : '记忆来源'}更新时间：${_time(correctionTime(claim['sourceUpdatedAt']))}'),
            Text(view.createdAt == null ? '本次未提供记忆创建时间。'
                : '记忆创建时间：${_time(view.createdAt!)}'),
            Text(view.validFrom == null ? '本次未提供记忆有效区间起点。'
                : '记忆有效区间起点：${_time(view.validFrom!)}'),
            Text('记忆来源有效至：${_time(m.validUntil)}（这是本次读取中的来源区间）'),
            Text(view.confidenceProvided ? '置信说明仅描述本人直接声明，不是真实概率或授权。'
                : '本次未提供置信描述；不会补成分值或概率。'),
            const Text('拍摄时间未采集。更新时间与读取时间不证明发生、到场、到访或当前位置。'),
            Text('本次同体读取时间：${_time(view.observedAt)}'),
            Text('本次审阅快照有效至：${_time(view.expiresAt)}'),
            Text(view.raw['notice']),
            const Text('核对不会执行更正、选择赢家、批准写入或启用模型。修改记忆仍须重新检查具体版本。'),
            const Text('活动偏好仍可返回设置中的“我的智能体”检查；此处不会替你修改资料。'),
            TextButton(onPressed: _data.closeSelfReview,
              child: const Padding(padding: EdgeInsets.symmetric(vertical: 12), child: Text('关闭本次核对'))),
          ],
        ],
      ],
    );
  }

  Widget _memoryItem(CorrectableMemory m) {
    final active =
        m.status != 'DELETED' && m.validUntil.isAfter(_now?.call() ?? DateTime.now().toUtc());
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(m.summary, style: Theme.of(context).textTheme.titleMedium),
        Text(
          '版本 ${m.version} · ${m.sourceType == 'EXPLICIT' ? '本人明确声明' : '待审推断预留形状，尚未确认'}',
        ),
        Text('有效至：${_time(m.validUntil)}'),
        _fieldEvidence(m),
        if (m.status == 'ACTIVE' && m.sourceType == 'EXPLICIT') _selfReview(m),
        if (!active) const Text('已到期，请重新读取后检查；本次不能批准旧版本。'),
        if (active)
          Wrap(
            spacing: 8,
            runSpacing: 4,
            children: [
              if (m.sourceType == 'EXPLICIT')
                TextButton(
                  onPressed: _data.canReview
                      ? () => _choose(m, null, 'EDIT')
                      : null,
                  child: const Padding(
                    padding: EdgeInsets.symmetric(vertical: 12),
                    child: Text('修改这条记忆'),
                  ),
                ),
              TextButton(
                onPressed: _data.canReview
                    ? () => _choose(m, null, 'REJECT')
                    : null,
                child: const Padding(
                  padding: EdgeInsets.symmetric(vertical: 12),
                  child: Text('拒绝保留'),
                ),
              ),
              TextButton(
                onPressed: _data.canReview
                    ? () => _choose(m, null, 'DELETE')
                    : null,
                child: const Padding(
                  padding: EdgeInsets.symmetric(vertical: 12),
                  child: Text('删除'),
                ),
              ),
              if (m.category != null)
                TextButton(
                  onPressed: _data.canReview
                      ? () => _choose(m, null, 'NEGATE')
                      : null,
                  child: const Padding(
                    padding: EdgeInsets.symmetric(vertical: 12),
                    child: Text('纠正活动偏好'),
                  ),
                ),
            ],
          ),
        const Divider(height: 28),
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final scale = MediaQuery.textScalerOf(context).scale(1);
    return Scaffold(
      appBar: AppBar(
        toolbarHeight: 56 + (scale - 1) * 32,
        title: const Text(
          '管理我的记忆',
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
        ),
      ),
      body: SafeArea(
        child: ListView(
          controller: _scroll,
          padding: EdgeInsets.fromLTRB(
            18,
            16,
            18,
            24 + MediaQuery.viewInsetsOf(context).bottom,
          ),
          children: [
            const Text('本人管理 · 不启用模型或自动写入'),
            if (_data.busy)
              const LinearProgressIndicator(semanticsLabel: '正在核实本人记忆'),
            if (_data.error != null)
              Semantics(
                liveRegion: true,
                child: Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Text(
                    _data.error!,
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                ),
              ),
            if (_data.message != null)
              Semantics(
                liveRegion: true,
                child: Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Text(_data.message!),
                ),
              ),
            if (_data.unknown || _data.hasPending && _data.preview == null) ...[
              const Text('仅读取原操作回执；关闭重开或更换会话 不会恢复旧正文或批准。'),
              _button(
                '核实原纠正结果',
                !_data.busy && !_data.retired
                    ? () => unawaited(_data.verify())
                    : null,
              ),
            ],
            if (!_data.retired &&
                !_data.denied &&
                (!_data.hasPending || _data.storageBlocked))
              _button(
                '重新读取当前记忆',
                !_data.busy ? () => unawaited(_data.load()) : null,
              ),
            if (_data.preview != null)
              _reviewView(_data.preview!)
            else if (_action != null && !_data.unknown)
              _draft(),
            if (_data.canReview && _action == null) ...[
              Text('我的当前记忆', style: Theme.of(context).textTheme.titleLarge),
              const SizedBox(height: 12),
              for (final m in _data.memories) _memoryItem(m),
              if (_data.candidatesUnavailable)
                const Text('待审候选暂不可用；仍可管理本人明确声明。'),
              if (_data.candidates.any((c) => c.status == 'CANDIDATE'))
                Text('待审候选', style: Theme.of(context).textTheme.titleLarge),
              for (final c in _data.candidates.where(
                (c) => c.status == 'CANDIDATE',
              )) ...[
                Text(
                  '待审${candidateCategories[c.category]}候选 · 版本 ${c.version}',
                ),
                Text('有效至：${_time(c.validUntil)}'),
                _button('拒绝这条候选', () => _choose(null, c, 'REJECT')),
                _button('明确不偏好这类活动', () => _choose(null, c, 'NEGATE')),
                const Divider(height: 28),
              ],
            ],
          ],
        ),
      ),
    );
  }
}
