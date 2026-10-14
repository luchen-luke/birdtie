import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;
import '../app/birdtie_app_bar.dart';
import '../auth/birdtie_auth_controller.dart';
import 'agent_memory_candidate_api.dart'
    show HumanMemoryCandidate, candidateCategories;
import 'agent_memory_correction_api.dart';
import 'agent_memory_correction_controller.dart';
import 'agent_memory_correction_pending_store.dart';
import 'memory_content_fields.dart';

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
        if (_action == 'EDIT')
          TextField(
            controller: _summary,
            minLines: 2,
            maxLines: 6,
            maxLength: 400,
            enabled: _data.canReview,
            decoration: const InputDecoration(
              labelText: '修改后的内容',
              helperText: '其他补充内容、类别和可见范围保持原值。',
            ),
          ),
        if (_action == 'NEGATE') ...[
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
          const Text('此后不再建议你喜欢这类活动；删除这条纠正也不会恢复旧建议。'),
        ],
        if (_action == 'DELETE' || _action == 'REJECT')
          const Text('将清除这条记忆的内容，不会删除原动态。'),
        _button(
          '检查修改',
          _data.canReview && (_action != 'NEGATE' || _category != null)
              ? () => unawaited(_review())
              : null,
          primary: true,
        ),
        _button('取消', _data.canReview ? () => setState(_clearDraft) : null),
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
        Text('检查修改', style: Theme.of(context).textTheme.titleLarge),
        Text('操作：${correctionActions[p.input['action']]}'),
        for (final m in p.memories) ...[
          const Divider(height: 28),
          const Text('当前记忆'),
          SelectableText(m.summary),
          Text(m.raw['visibility'] == 'PRIVATE' ? '仅自己可见' : '仅供个人助理使用'),
          MemoryContentFields(values: m.raw['structuredValue'] as Map),
        ],
        if (p.input['action'] == 'EDIT') ...[
          const Text('修改后'),
          SelectableText(p.input['replacement']['summary']),
          const Text('其他内容和可见范围不变。'),
        ],
        if (p.input['action'] == 'DELETE' || p.input['action'] == 'REJECT')
          Text(
            p.input['targetKind'] == 'MEMORY'
                ? '确认后删除所选记忆并清除内容；原动态不受影响。'
                : '确认后拒绝这条建议并清除内容；原动态不受影响。',
          ),
        if (p.input['action'] == 'NEGATE') ...[
          Text(
            '确认后保存本人纠正：${correctionNegativeStatements[p.input['category']]}',
          ),
          Text(
            '纠正声明有效至：${_time(correctionTime(p.raw['newMemoryValidUntil']))}',
          ),
          Text('将移除这类活动的 $candidateCount 条待确认建议，并停止再次建议你喜欢这类活动。'),
        ],
        ExpansionTile(
          title: Text('受影响的记忆（${affected.length} 项）'),
          children: [
            for (final target in affected)
              Padding(
                padding: const EdgeInsets.all(12),
                child: Text(
                  target['kind'] == 'MEMORY'
                      ? p.memories
                            .firstWhere((m) => m.id == target['id'])
                            .summary
                      : '待确认的活动偏好建议',
                ),
              ),
          ],
        ),
        const SizedBox(height: 12),
        if (p.input['action'] == 'NEGATE')
          const Text('这条纠正删除或到期后，也不会恢复旧建议；原动态不受影响。'),
        _button(
          '确认修改',
          _data.canConfirm ? () => unawaited(_data.confirm()) : null,
          primary: true,
        ),
        _button(
          '取消并核实结果',
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
              ? () => unawaited(_data.readFieldEvidence(m))
              : null,
          child: const Padding(
            padding: EdgeInsets.symmetric(vertical: 12),
            child: Text('查看来源与时间'),
          ),
        ),
        if (_data.fieldMemoryID == m.id) ...[
          if (_data.fieldLoading)
            const LinearProgressIndicator(semanticsLabel: '正在读取这条记忆的来源'),
          if (_data.fieldError != null)
            Semantics(
              liveRegion: true,
              child: Text(
                _data.fieldError!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ),
          if (detail != null && detail.id == m.id) ...[
            const SizedBox(height: 8),
            Text('这条记忆的来源与时间', style: Theme.of(context).textTheme.titleSmall),
            if (detail.memory != null) ...[
              Text('记录创建时间：${_time(detail.createdAt!)}'),
              Text('记录更新时间：${_time(detail.updatedAt!)}'),
              Text(
                '有效区间：${_time(detail.validFrom!)} 至 ${_time(detail.memory!.validUntil)}',
              ),
            ],
            if (!detail.evidenceAvailable)
              const Text('暂无来源说明。')
            else if (detail.memory == null)
              const Text('这条记忆已到期或删除。')
            else ...[
              Text(detail.inferred ? '来源：待确认建议' : '来源：本人填写'),
              if (detail.declaredAt != null)
                Text('声明时间：${_time(detail.declaredAt!)}')
              else
                const Text('声明时间：未提供'),
            ],
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
              ? () => unawaited(_data.readSelfReview(m))
              : null,
          child: const Padding(
            padding: EdgeInsets.symmetric(vertical: 12),
            child: Text('核对活动偏好', textAlign: TextAlign.center),
          ),
        ),
        if (_data.selfReviewMemoryID == m.id) ...[
          if (_data.selfReviewLoading)
            const LinearProgressIndicator(semanticsLabel: '正在读取活动偏好'),
          if (_data.selfReviewError != null)
            Semantics(
              liveRegion: true,
              child: Text(
                _data.selfReviewError!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ),
          if (view != null) ...[
            const SizedBox(height: 8),
            Text('一起核对这两份声明', style: Theme.of(context).textTheme.titleSmall),
            Text(
              view.profileStatus == 'UNCONFIGURED'
                  ? '活动偏好：尚未设置，不会补全。'
                  : view.categories.isEmpty
                  ? '活动偏好：当前明确选择为空。'
                  : '资料中的活动偏好：${view.categories.map((c) => candidateCategories[c]).join('、')}',
            ),
            SelectableText('这条记忆的声明：${m.summary}'),
            if (view.conflictCategories.isNotEmpty)
              Semantics(
                liveRegion: true,
                child: Text(
                  '声明不一致：${view.conflictCategories.map((c) => candidateCategories[c]).join('、')} · 待本人确认',
                ),
              )
            else if (view.profileStatus != 'UNCONFIGURED' &&
                view.categories.isNotEmpty)
              const Text('这两份活动偏好未发现不一致。'),
            for (final claim in view.raw['fieldEvidenceSet']['claims'])
              if (claim['field'] == 'memory.summary' ||
                  claim['field'] == 'profile.preferredActivityTypes')
                Text(
                  '${claim['itemKind'] == 'profile' ? '活动偏好来源' : '记忆来源'}更新时间：${_time(correctionTime(claim['sourceUpdatedAt']))}',
                ),
            if (view.createdAt != null)
              Text('记忆创建时间：${_time(view.createdAt!)}'),
            TextButton(
              onPressed: _data.closeSelfReview,
              child: const Padding(
                padding: EdgeInsets.symmetric(vertical: 12),
                child: Text('关闭本次核对'),
              ),
            ),
          ],
        ],
      ],
    );
  }

  Widget _memoryItem(CorrectableMemory m) {
    final active =
        m.status != 'DELETED' &&
        m.validUntil.isAfter(_now?.call() ?? DateTime.now().toUtc());
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(m.summary, style: Theme.of(context).textTheme.titleMedium),
        Text(m.sourceType == 'EXPLICIT' ? '已确认' : '待确认'),
        Text('有效至：${_time(m.validUntil)}'),
        _fieldEvidence(m),
        if (m.status == 'ACTIVE' && m.sourceType == 'EXPLICIT') _selfReview(m),
        if (!active) const Text('已到期，请重新读取。'),
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
      appBar: birdtieAppBar(
        context,
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
            const Text('仅自己可见'),
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
            if (_data.message != null &&
                _data.message != '选择本人当前记忆，先检查具体版本再确认。')
              Semantics(
                liveRegion: true,
                child: Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Text(
                    _data.unknown || _data.hasPending && _data.preview == null
                        ? '保存结果尚未确认，请核实后继续。'
                        : _data.preview != null
                        ? '请检查内容和影响，确认后才会保存。'
                        : _data.message!,
                  ),
                ),
              ),
            if (_data.unknown || _data.hasPending && _data.preview == null) ...[
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
              if (_data.hasMoreMemories)
                _button(
                  '继续读取记忆',
                  !_data.busy
                      ? () => unawaited(_data.loadMoreMemories())
                      : null,
                ),
              if (_data.candidatesUnavailable)
                const Text('待审候选暂不可用；仍可管理本人明确声明。'),
              if (_data.candidates.any((c) => c.status == 'CANDIDATE'))
                Text('待审候选', style: Theme.of(context).textTheme.titleLarge),
              for (final c in _data.candidates.where(
                (c) => c.status == 'CANDIDATE',
              )) ...[
                Text('可能喜欢${candidateCategories[c.category]}活动'),
                Text('有效至：${_time(c.validUntil)}'),
                _button('拒绝这条候选', () => _choose(null, c, 'REJECT')),
                _button('明确不偏好这类活动', () => _choose(null, c, 'NEGATE')),
                const Divider(height: 28),
              ],
              if (_data.hasMoreCandidates)
                _button(
                  '继续读取待审候选',
                  !_data.busy
                      ? () => unawaited(_data.loadMoreCandidates())
                      : null,
                ),
            ],
          ],
        ),
      ),
    );
  }
}
