import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../app/birdtie_app_bar.dart';
import '../auth/birdtie_auth_controller.dart';
import 'agent_memory_candidate_api.dart';
import 'agent_memory_candidate_controller.dart';
import 'agent_candidate_pending_store.dart';
import 'agent_multi_candidate_section.dart';
import 'agent_multi_candidate_pending_store.dart';

class AgentMemoryCandidatePage extends StatefulWidget {
  const AgentMemoryCandidatePage({
    super.key,
    required this.auth,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.pendingStore,
    this.multiPendingStore,
    this.current,
  });
  final AgentCandidatePendingStore? pendingStore;
  final AgentMultiCandidatePendingStore? multiPendingStore;
  final BirdtieAuthController auth;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final bool Function()? current;
  @override
  State<AgentMemoryCandidatePage> createState() =>
      _AgentMemoryCandidatePageState();
}

class _AgentMemoryCandidatePageState extends State<AgentMemoryCandidatePage> {
  late final BirdtieAuthController _auth = widget.auth;
  late final http.Client? _client = widget.client;
  late final String? _apiBaseUrl = widget.apiBaseUrl;
  late final Listenable? _workspaceChanges = widget.workspaceChanges;
  late final String? Function()? _workspaceID = widget.organizationWorkspaceID;
  late final AgentCandidatePendingStore? _pendingStore = widget.pendingStore;
  late final AgentMultiCandidatePendingStore? _multiPendingStore;
  late final bool Function()? _entryCurrent = widget.current;
  bool _entryRetired = false;
  late final bool Function() _multiCurrent = _sourceCurrent;
  bool _sourceCurrent() {
    if (_retired || _entryRetired || !mounted) return false;
    if (_entryCurrent?.call() == false) {
      _entryRetired = true;
      return false;
    }
    return true;
  }

  bool _retired = false, _listening = false;
  late final AgentMemoryCandidateController _data =
      AgentMemoryCandidateController(
        api: AgentMemoryCandidateAPI(client: _client, apiBaseUrl: _apiBaseUrl),
        authorizationHeader: () =>
            _sourceCurrent() ? _auth.authorizationHeader : null,
        accountID: () => _sourceCurrent() ? _auth.accountID : null,
        organizationWorkspaceID: () => _workspaceID?.call(),
        pendingStore: _pendingStore,
      );
  @override
  void initState() {
    super.initState();
    _multiPendingStore = widget.multiPendingStore;
    _auth.addListener(_identity);
    _workspaceChanges?.addListener(_identity);
    _listening = true;
    _data.addListener(_changed);
    unawaited(_data.load());
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  void _identity() {
    if (_retired) return;
    final g = _data.generation;
    _data.synchronizeIdentity();
    if (g != _data.generation && _data.personal) unawaited(_data.load());
  }

  @override
  void didUpdateWidget(covariant AgentMemoryCandidatePage oldWidget) {
    super.didUpdateWidget(oldWidget);
    // This State owns one concrete transport/identity frame. Replacing any
    // source permanently retires it, including A -> B -> A restoration.
    if (_retired) return;
    if (!identical(_entryCurrent, widget.current) ||
        !identical(_auth, widget.auth) ||
        !identical(_client, widget.client) ||
        _apiBaseUrl != widget.apiBaseUrl ||
        !identical(_workspaceChanges, widget.workspaceChanges) ||
        !identical(_workspaceID, widget.organizationWorkspaceID) ||
        !identical(_pendingStore, widget.pendingStore) ||
        !identical(_multiPendingStore, widget.multiPendingStore)) {
      _retired = true;
      _detachIdentity();
      _data.removeListener(_changed);
      _data.dispose();
      return;
    }
    _identity();
  }

  void _detachIdentity() {
    if (!_listening) return;
    _listening = false;
    _auth.removeListener(_identity);
    _workspaceChanges?.removeListener(_identity);
  }

  @override
  void dispose() {
    _detachIdentity();
    _data.removeListener(_changed);
    _data.dispose();
    super.dispose();
  }

  String _time(DateTime t) {
    final l = t.toLocal();
    return '${l.year}年${l.month}月${l.day}日 ${l.hour.toString().padLeft(2, '0')}:${l.minute.toString().padLeft(2, '0')}（本地时间）';
  }

  Widget _button(
    String text,
    VoidCallback? onPressed, {
    bool primary = false,
  }) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 4),
    child: SizedBox(
      height: 52,
      child: primary
          ? FilledButton(onPressed: onPressed, child: Text(text))
          : OutlinedButton(onPressed: onPressed, child: Text(text)),
    ),
  );
  @override
  Widget build(BuildContext context) {
    if (!_sourceCurrent()) {
      return Scaffold(
        appBar: birdtieAppBar(context, title: const Text('记忆候选')),
        body: SafeArea(
          child: ListView(
            padding: const EdgeInsets.all(20),
            children: [
              const Text('页面连接已变更，请返回后重新打开。'),
              const SizedBox(height: 16),
              const Text('本页旧预览不能继续提交。未确认的结果仍需核实原候选；没有自动重发、撤回或回滚。'),
              const SizedBox(height: 16),
              const Text('从原入口重新打开后，将读取当前本人记录并重新检查具体版本。'),
              const SizedBox(height: 16),
              FilledButton(
                style: FilledButton.styleFrom(minimumSize: const Size(48, 48)),
                onPressed: Navigator.of(context).canPop()
                    ? () => Navigator.of(context).maybePop()
                    : null,
                child: const Padding(
                  padding: EdgeInsets.symmetric(vertical: 12),
                  child: Text('返回'),
                ),
              ),
            ],
          ),
        ),
      );
    }
    final p = _data.preview;
    final multiBlocked =
        _data.busy || _data.unknownAccept || _data.hasPendingAcceptance;
    final enabled =
        !_data.busy && !_data.unknownAccept && !_data.hasPendingAcceptance;
    return Scaffold(
      appBar: birdtieAppBar(context, title: const Text('记忆候选')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            Text('由我检查并决定', style: Theme.of(context).textTheme.headlineSmall),
            const SizedBox(height: 12),
            const Text('个人私密记忆。这里整理的是你手动提出的活动偏好，来源不证明偏好、到场或身份。自动分析尚未开放。'),
            if (!_data.personal) ...[
              const SizedBox(height: 20),
              const Text('请登录个人账号并退出组织工作台后使用。'),
            ] else ...[
              if (_data.busy)
                Padding(
                  padding: const EdgeInsets.all(12),
                  child: Semantics(
                    label: '正在处理',
                    child: const LinearProgressIndicator(),
                  ),
                ),
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
              if (_data.notice != null)
                Semantics(
                  liveRegion: true,
                  child: Padding(
                    padding: const EdgeInsets.symmetric(vertical: 12),
                    child: Text(_data.notice!),
                  ),
                ),
              if (_data.unknownAccept)
                _button(
                  '核实原候选结果',
                  _data.busy ? null : () => unawaited(_data.verifyUnknown()),
                  primary: true,
                ),
              _button(
                '刷新候选',
                _data.busy ? null : () => unawaited(_data.load()),
              ),
              const Divider(height: 32),
              Text('提出一项待确认偏好', style: Theme.of(context).textTheme.titleLarge),
              const Text('只支持普通活动类别。候选保存后尚未写入记忆，有效期一小时。'),
              const SizedBox(height: 12),
              DropdownButtonFormField<String>(
                key: ValueKey(_data.generation),
                initialValue: _data.category,
                isExpanded: true,
                decoration: const InputDecoration(
                  labelText: '活动偏好',
                  border: OutlineInputBorder(),
                ),
                items: candidateCategories.entries
                    .map(
                      (e) =>
                          DropdownMenuItem(value: e.key, child: Text(e.value)),
                    )
                    .toList(),
                onChanged: enabled ? (v) => _data.edit(nextCategory: v) : null,
              ),
              _button(
                '选择我的真实来源',
                enabled ? () => unawaited(_data.loadSources()) : null,
              ),
              if (_data.choices.isEmpty)
                const Text('尚未加载可用来源。请打开来源列表；无资料时不会编造。'),
              for (final c in _data.choices)
                CheckboxListTile(
                  contentPadding: EdgeInsets.zero,
                  title: Text(c.title),
                  subtitle: Text(c.detail),
                  value: _data.selected.contains(c.key),
                  onChanged:
                      enabled &&
                          (_data.selected.contains(c.key) ||
                              _data.selected.length < 20)
                      ? (_) => _data.edit(toggle: c.key)
                      : null,
                  controlAffinity: ListTileControlAffinity.leading,
                ),
              const Text('最多选择20条记录。同一活动的动态和报名只计作一项来源；请至少选择两项独立来源。'),
              _button(
                '保存待确认候选',
                enabled && _data.selected.isNotEmpty
                    ? () => unawaited(_data.save())
                    : null,
                primary: true,
              ),
              if (p != null) ...[
                const Divider(height: 32),
                Text('保存前确认', style: Theme.of(context).textTheme.titleLarge),
                Text(
                  p.statement,
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                Text('将新增一条仅自己可见的记忆，不覆盖旧记忆。\n有效至：${_time(p.memoryValidUntil)}'),
                Text('本次确认有效至：${_time(p.expiresAt)}'),
                const SizedBox(height: 8),
                for (final raw in p.candidate.raw['sources'] as List)
                  Builder(
                    builder: (context) {
                      final source = raw as Map;
                      final sel = source['selector'] as Map;
                      final found = _data.choices.where(
                        (e) => e.id == sel['id'] && e.type == sel['type'],
                      );
                      final title = found.isEmpty
                          ? {
                              'MOMENT': '我的动态',
                              'ACTIVITY_PARTICIPATION': '我的报名',
                              'SAVED_PLACE': '保存的地点',
                            }[sel['type']]
                          : found.first.title;
                      return Padding(
                        padding: const EdgeInsets.symmetric(vertical: 6),
                        child: Text(
                          '来源：$title\n记录时间：${_time(candidateTime(source['eventTime']))}',
                        ),
                      );
                    },
                  ),
                const Text('请确认上面的内容。关联记录不代表这项推断已经成立。'),
                _button(
                  '确认保存这项声明',
                  !_data.busy && !_data.unknownAccept
                      ? () => unawaited(_data.accept())
                      : null,
                  primary: true,
                ),
                _button('取消预览', enabled ? _data.cancelPreview : null),
              ],
              const Divider(height: 32),
              Text('我的候选记录', style: Theme.of(context).textTheme.titleLarge),
              if (_data.records.isEmpty && !_data.busy)
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 16),
                  child: Text('当前没有候选。你可以自行选择真实来源提出一项偏好。'),
                ),
              for (final c in _data.records)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        c.category == null
                            ? '候选记录'
                            : candidateCategories[c.category]!,
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      Text(
                        '${candidateStatusLabels[c.status]} · 版本 ${c.version}',
                      ),
                      if (c.status == 'CANDIDATE') ...[
                        Text('候选有效至：${_time(c.validUntil)}'),
                        _button(
                          '检查这项候选',
                          enabled ? () => unawaited(_data.prepare(c)) : null,
                        ),
                        _button(
                          '拒绝这项候选',
                          enabled ? () => unawaited(_data.reject(c)) : null,
                        ),
                      ],
                      if (c.status == 'ACTIVE')
                        const Text('已保存为本人声明。记忆改变或撤销后，本记录会失效。'),
                    ],
                  ),
                ),
              if (_data.hasMore)
                _button(
                  '继续读取候选',
                  _data.canLoadMore ? () => unawaited(_data.loadMore()) : null,
                ),
              if (multiBlocked) const Text('请先核实原候选保存结果；当前暂不能发起新的多来源整理。'),
              Semantics(
                key: const ValueKey('candidate-multi-recovery-boundary'),
                container: true,
                label: multiBlocked ? '多来源整理暂不可操作，请先核实原候选保存结果' : '多来源整理',
                child: AbsorbPointer(
                  absorbing: multiBlocked,
                  child: ExcludeFocus(
                    excluding: multiBlocked,
                    child: ExcludeSemantics(
                      excluding: multiBlocked,
                      child: AgentMultiCandidateSection(
                        pendingStore: _multiPendingStore,
                        current: _multiCurrent,
                        key: const ValueKey('human-multi-candidate-section'),
                        auth: _auth,
                        client: _client,
                        apiBaseUrl: _apiBaseUrl,
                        workspaceChanges: _workspaceChanges,
                        organizationWorkspaceID: _workspaceID,
                        onCandidateChanged: () => unawaited(_data.load()),
                      ),
                    ),
                  ),
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}
