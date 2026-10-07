import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import 'agent_memory_candidate_api.dart';
import 'agent_multi_candidate_api.dart';
import 'agent_multi_candidate_controller.dart';
import 'agent_multi_candidate_pending_store.dart';

/// An explicit, local, source-version review within the existing human page.
/// It neither creates tasks nor promotes inferred content to Memory.
class AgentMultiCandidateSection extends StatefulWidget {
  const AgentMultiCandidateSection({
    super.key,
    required this.auth,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.onCandidateChanged,
    this.pendingStore,
    this.current,
  });
  final BirdtieAuthController auth;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final bool Function()? current;
  final VoidCallback? onCandidateChanged;
  final AgentMultiCandidatePendingStore? pendingStore;
  @override
  State<AgentMultiCandidateSection> createState() =>
      _AgentMultiCandidateSectionState();
}

class _AgentMultiCandidateSectionState
    extends State<AgentMultiCandidateSection> {
  late final _auth = widget.auth;
  late final _client = widget.client;
  late final _base = widget.apiBaseUrl;
  late final _changes = widget.workspaceChanges;
  late final _workspace = widget.organizationWorkspaceID;
  late final _store =
      widget.pendingStore ?? const SecureAgentMultiCandidatePendingStore();
  late final AgentMultiCandidatePendingStore? _storeInput;
  late final bool Function()? _entryCurrent = widget.current;
  bool _entryRetired = false;
  bool _sourceCurrent() {
    if (_retired || _entryRetired || !mounted) return false;
    if (_entryCurrent?.call() == false) {
      _entryRetired = true;
      return false;
    }
    return true;
  }

  bool _retired = false, _listening = false, _expanded = false;
  MultiCandidateReceipt? _previousResult;
  late final _data = AgentMultiCandidateController(
    api: AgentMultiCandidateAPI(
      AgentMemoryCandidateAPI(client: _client, apiBaseUrl: _base),
    ),
    pendingStore: _store,
    authorizationHeader: () => _sourceCurrent() ? _auth.authorizationHeader : null,
    accountID: () => _sourceCurrent() ? _auth.accountID : null,
    organizationWorkspaceID: () => _workspace?.call(),
  );
  @override
  void initState() {
    super.initState();
    _storeInput = widget.pendingStore;
    _data.synchronizeIdentity();
    _auth.addListener(_identity);
    _changes?.addListener(_identity);
    _listening = true;
    _data.addListener(_changed);
  }

  void _identity() {
    if (!_retired) _data.synchronizeIdentity();
  }

  void _changed() {
    if (!mounted) return;
    if (!identical(_previousResult, _data.result)) {
      final previous = _previousResult;
      _previousResult = _data.result;
      if (_data.result?.staged == true ||
          (previous?.staged == true && _data.result == null)) {
        widget.onCandidateChanged?.call();
      }
    }
    setState(() {});
  }

  void _detach() {
    if (!_listening) return;
    _listening = false;
    _auth.removeListener(_identity);
    _changes?.removeListener(_identity);
  }

  @override
  void didUpdateWidget(covariant AgentMultiCandidateSection oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (_retired) return;
    if (!identical(_entryCurrent, widget.current) ||
        !identical(_auth, widget.auth) ||
        !identical(_client, widget.client) ||
        _base != widget.apiBaseUrl ||
        !identical(_changes, widget.workspaceChanges) ||
        !identical(_workspace, widget.organizationWorkspaceID) ||
        !identical(_storeInput, widget.pendingStore)) {
      _retired = true;
      _detach();
      _data.removeListener(_changed);
      _data.dispose();
      return;
    }
    _identity();
  }

  @override
  void dispose() {
    _detach();
    _data.removeListener(_changed);
    _data.dispose();
    super.dispose();
  }

  String _time(DateTime time) {
    final t = time.toLocal();
    return '${t.month}月${t.day}日 ${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}:${t.second.toString().padLeft(2, '0')}（本地时间）';
  }

  Widget _action(
    String label,
    VoidCallback? action, {
    bool primary = false,
    Key? key,
  }) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 4),
    child: primary
        ? FilledButton(
            key: key,
            style: FilledButton.styleFrom(minimumSize: const Size(48, 48)),
            onPressed: action,
            child: Padding(
              padding: const EdgeInsets.symmetric(vertical: 8),
              child: Text(label),
            ),
          )
        : OutlinedButton(
            key: key,
            style: OutlinedButton.styleFrom(minimumSize: const Size(48, 48)),
            onPressed: action,
            child: Padding(
              padding: const EdgeInsets.symmetric(vertical: 8),
              child: Text(label),
            ),
          ),
  );
  @override
  Widget build(BuildContext context) {
    if (!_sourceCurrent()) return const Text('来源页面连接已变更，请返回后重新打开；旧授权不能继续提交。');
    if (!_data.personal) return const Text('多来源整理只限个人账号；请先退出组织工作台。');
    final enabled = _data.editable,
        p = _data.preview,
        grant = _data.retentionGrant;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const Divider(height: 32),
        Text('从多条动态整理候选', style: Theme.of(context).textTheme.titleLarge),
        const SizedBox(height: 8),
        const Text('由你选择近期的私密草稿，再逐条允许本地分析。只整理普通活动类别；不会自动保存为记忆。'),
        if (!_expanded)
          _action(
            '选择来源和已有任务',
            enabled
                ? () {
                    setState(() => _expanded = true);
                    unawaited(_data.load());
                  }
                : null,
          ),
        if (_expanded) ...[
          if (_data.busy)
            Semantics(label: '正在检查来源', child: const LinearProgressIndicator()),
          if (_data.error != null)
            Semantics(
              liveRegion: true,
              child: Padding(
                padding: const EdgeInsets.symmetric(vertical: 12),
                child: Text(
                  _data.error!,
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
              ),
            ),
          if (_data.notice != null)
            Semantics(
              liveRegion: true,
              child: Padding(
                padding: const EdgeInsets.symmetric(vertical: 12),
                child: Text(_data.notice!),
              ),
            ),
          if (_data.unknown != null) ...[
            const Text('请先核实原结果。关闭重开后只核实原操作，不恢复旧许可。已提交的候选可在候选记录中找回。'),
            _action(
              '核实原授权或提交结果',
              _data.busy ? null : () => unawaited(_data.verifyUnknown()),
              primary: true,
            ),
          ],
          _action(
            '重新读取任务和来源',
            (enabled || _data.recoveryBlocked) && !_data.busy
                ? () => unawaited(_data.load())
                : null,
          ),
          if (_data.loaded && _data.tasks.isEmpty)
            const Text('没有可使用的本人城市任务。请先在 Now 提出需求，再返回读取；此处不会替你创建任务。'),
          if (_data.loaded && _data.moments.isEmpty)
            const Text('没有符合期限的私密动态草稿。只可选择最近 15 分钟更新、未关联活动或组织的本人草稿。'),
          for (final task in _data.tasks)
            CheckboxListTile(
              key: ValueKey('multi-task-${task.id}'),
              contentPadding: EdgeInsets.zero,
              title: Text(task.query),
              subtitle: Text('已有任务 · 更新于 ${_time(task.updatedAt)}'),
              value: _data.taskID == task.id,
              onChanged: enabled ? (_) => _data.edit(nextTask: task.id) : null,
            ),
          const SizedBox(height: 12),
          const Text('选择 2–5 条来源。同一条动态不会算成多条独立证据。默认仅分析标题，正文由你另行选择。'),
          for (final moment in _data.moments) ...[
            CheckboxListTile(
              key: ValueKey('multi-source-${moment.id}'),
              contentPadding: EdgeInsets.zero,
              title: Text(moment.title.isEmpty ? '未填写标题的私密草稿' : moment.title),
              subtitle: Text(
                '私密草稿 · 修订 ${moment.revision} · ${_time(moment.updatedAt)}',
              ),
              value: _data.selected.containsKey(moment.id),
              onChanged:
                  enabled &&
                      (_data.selected.containsKey(moment.id) ||
                          _data.selected.length < 5)
                  ? (_) => _data.edit(toggleMoment: moment.id)
                  : null,
            ),
            if (_data.selected.containsKey(moment.id)) ...[
              CheckboxListTile(
                key: ValueKey('multi-body-${moment.id}'),
                contentPadding: EdgeInsets.zero,
                title: const Text('这次也分析这条动态的正文'),
                value: _data.selected[moment.id],
                onChanged: enabled
                    ? (_) => _data.edit(toggleBody: moment.id)
                    : null,
              ),
              if (_data.analysisGrants[moment.id] case final sourceGrant?) ...[
                Text(
                  sourceGrant.revoked
                      ? '分析许可已撤回'
                      : sourceGrant.usable
                      ? '分析许可有效至 ${_time(sourceGrant.expiresAt)}'
                      : '分析许可已到期',
                ),
                if (!sourceGrant.revoked)
                  _action(
                    '撤回这条来源的分析许可',
                    enabled ? () => unawaited(_data.revoke(sourceGrant)) : null,
                  ),
              ],
              _action(
                '检查这条动态的分析内容',
                enabled && _data.taskID != null
                    ? () => unawaited(_data.prepareAnalysis(moment.id))
                    : null,
                key: ValueKey('multi-review-${moment.id}'),
              ),
            ],
          ],
          _action(
            '检查组合候选',
            _data.canPrepareMulti
                ? () => unawaited(_data.prepareMulti())
                : null,
          ),
          if (p != null) ...[
            const Divider(height: 24),
            Text(
              p.multi ? '组合候选预览' : '本次来源分析预览',
              style: Theme.of(context).textTheme.titleMedium,
            ),
            const Text('当前主体：本人个人账号 · 私密使用'),
            if (!p.multi) ...[
              Text('已有任务：${p.review!['taskQuery']}'),
              Text('动态具体版本：修订 ${p.selection['momentRevision']}'),
              for (final item in (p.review!['content'] as Map).entries)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 6),
                  child: Text(
                    '${item.key == 'title' ? '标题' : '正文'}：${item.value}',
                  ),
                ),
              const Text('这次批准仅允许上面显示的内容进行本地分析，不包括模型传输、候选保留或记忆写入。'),
            ] else ...[
              for (final task in _data.tasks.where((t) => t.id == _data.taskID))
                Text('已有任务：${task.query}'),
              Text(
                '建议活动类别：${candidateCategories[p.review!['proposal']['category']]}',
              ),
              const Text('依据为少量普通活动关键词，可信度较低；不是已确认偏好，也不是概率。'),
              for (final member in p.review!['sourceSelections'] as List)
                Text(
                  '${_data.moments.singleWhere((m) => m.id == member['source']['selector']['id']).title}\n'
                  '具体版本：修订 ${member['source']['version']['revision']} · '
                  '${(member['selectedFields'] as List).map((f) => f == 'title' ? '标题' : '正文').join('、')}',
                ),
              const Text('仅保留上述具体版本形成的待确认候选。保存为本人声明还需在“我的候选记录”单独检查与确认。'),
            ],
            Text('本次预览有效至：${_time(p.expiresAt)}'),
            _action(
              p.multi ? '批准这项候选的保留' : '允许这次本地分析',
              _data.canApprove ? () => unawaited(_data.approve()) : null,
              primary: true,
            ),
            _action('取消本次预览', enabled ? _data.cancelPreview : null),
          ],
          if (grant != null) ...[
            Text(
              grant.revoked
                  ? '组合保留许可已撤回'
                  : '组合保留许可有效至：${_time(grant.expiresAt)}',
            ),
            _action(
              '提交为待确认候选',
              _data.canStage ? () => unawaited(_data.stage()) : null,
              primary: true,
            ),
            if (!grant.revoked)
              _action(
                '撤回这项候选的保留许可',
                enabled ? () => unawaited(_data.revoke(grant)) : null,
              ),
          ],
          if (_data.result?.staged == true)
            const Text('候选已进入“我的候选记录”。请刷新或检查该记录后，决定是否保存为本人声明。'),
        ],
      ],
    );
  }
}
