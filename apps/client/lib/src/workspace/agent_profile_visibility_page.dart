import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import 'agent_profile_visibility_api.dart';
import 'agent_profile_visibility_controller.dart';

class AgentProfileVisibilityPage extends StatefulWidget {
  const AgentProfileVisibilityPage({
    super.key,
    required this.auth,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.current,
  });
  final BirdtieAuthController auth;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final bool Function()? current;
  @override
  State<AgentProfileVisibilityPage> createState() =>
      _AgentProfileVisibilityPageState();
}

class _AgentProfileVisibilityPageState
    extends State<AgentProfileVisibilityPage> {
  late final _source = (
    widget.auth,
    widget.client,
    widget.apiBaseUrl,
    widget.workspaceChanges,
    widget.organizationWorkspaceID,
    widget.current,
  );
  bool _retired = false, _queued = false;
  bool _current() {
    if (!mounted || _retired) return false;
    if (_source !=
            (
              widget.auth,
              widget.client,
              widget.apiBaseUrl,
              widget.workspaceChanges,
              widget.organizationWorkspaceID,
              widget.current,
            ) ||
        widget.current?.call() == false) {
      _retired = true;
    }
    return !_retired;
  }

  late final _data = AgentProfileVisibilityController(
    api: AgentProfileVisibilityAPI(client: _source.$2, apiBaseUrl: _source.$3),
    authorizationHeader: () => _source.$1.authorizationHeader,
    ownerID: () => _source.$1.accountID,
    organizationWorkspaceID: _source.$5,
    current: _current,
  );
  @override
  void initState() {
    super.initState();
    _source.$1.addListener(_identity);
    _source.$4?.addListener(_identity);
    _data.addListener(_changed);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_current()) unawaited(_data.load());
    });
  }

  void _identity() => _data.synchronizeIdentity();
  void _changed() {
    if (!mounted) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      if (_queued) return;
      _queued = true;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        _queued = false;
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  @override
  void didUpdateWidget(AgentProfileVisibilityPage old) {
    super.didUpdateWidget(old);
    _identity();
  }

  @override
  void dispose() {
    _source.$1.removeListener(_identity);
    _source.$4?.removeListener(_identity);
    _data.removeListener(_changed);
    _data.dispose();
    super.dispose();
  }

  String _ruleText(ProfileFieldAudience value) {
    final label = profileAudienceLabels[value.visibility]!;
    if (value.visibility != 'COMMUNITY') return label;
    return '$label：${value.communityIDs.map((id) {
      final options = _data.communities.where((r) => r.id == id);
      return options.isNotEmpty ? options.first.name : '已保存社群（名称待核对）：$id';
    }).join('、')}';
  }

  Future<void> _pick(String field) async {
    if (!_data.canEdit) return;
    final original = _data.draft[field]!;
    var audience = original.visibility;
    final selected = original.communityIDs.toSet();
    await showModalBottomSheet<void>(
      context: context,
      useSafeArea: true,
      isScrollControlled: true,
      builder: (sheetContext) => SizedBox(
        height: MediaQuery.sizeOf(sheetContext).height * .75,
        child: AnimatedBuilder(
          animation: _data,
          builder: (_, _) => StatefulBuilder(
            builder: (_, update) {
              if (!_data.personal) {
                return const Padding(
                  padding: EdgeInsets.all(24),
                  child: Text('身份或来源已变化，请返回设置重新打开。'),
                );
              }
              final validIDs = _data.communities.map((r) => r.id).toSet();
              final retainedIDs = original.communityIDs.toSet();
              final unavailable = selected.difference(validIDs);
              final valid =
                  audience != 'COMMUNITY' ||
                  (selected.isNotEmpty &&
                      selected.length <= 8 &&
                      unavailable.difference(retainedIDs).isEmpty);
              return Column(
                children: [
                  ListTile(
                    title: Text('${profileVisibilityLabels[field]}的受众'),
                    trailing: IconButton(
                      tooltip: '取消本次修改',
                      icon: const Icon(Icons.close),
                      onPressed: () => Navigator.of(sheetContext).pop(),
                    ),
                  ),
                  Expanded(
                    child: RadioGroup<String>(
                      groupValue: audience,
                      onChanged: (v) {
                        if (_data.busy || !_data.personal || v == null) return;
                        update(() => audience = v);
                        if (v == 'COMMUNITY' && !_data.communitiesLoaded) {
                          unawaited(_data.loadCommunities());
                        }
                      },
                      child: ListView(
                      padding: const EdgeInsets.symmetric(horizontal: 16),
                      children: [
                        for (final a in profileAudienceLabels.entries)
                          RadioListTile<String>(
                            title: Text(a.value),
                            subtitle: Text(profileAudienceConsequences[a.key]!),
                            value: a.key,
                            enabled: !_data.busy,
                          ),
                        if (audience == 'COMMUNITY') ...[
                          const Text(
                            '从当前已加入且仍有效的社群中明确选择1–8项。列表最多100项，未列出不代表未加入。',
                          ),
                          if (_data.busy) const Text('正在核对本人社群…'),
                          if (_data.error != null) Text(_data.error!),
                          if (!_data.busy)
                            TextButton(
                              onPressed: () =>
                                  unawaited(_data.loadCommunities()),
                              child: const Text('重新读取本人社群'),
                            ),
                          if (_data.communitiesLoaded &&
                              _data.communities.isEmpty)
                            const Text('当前没有可选择的有效成员社群。'),
                          for (final row in _data.communities)
                            CheckboxListTile(
                              title: Text(row.name),
                              value: selected.contains(row.id),
                              onChanged: _data.busy
                                  ? null
                                  : (v) {
                                      if (!_data.personal) return;
                                      update(() {
                                        if (v == true) {
                                          selected.add(row.id);
                                        } else {
                                          selected.remove(row.id);
                                        }
                                      });
                                    },
                            ),
                          for (final id in unavailable)
                            CheckboxListTile(
                              title: const Text('原已保存社群本次列表未核实'),
                              subtitle: Text(id),
                              value: true,
                              onChanged: _data.busy
                                  ? null
                                  : (v) {
                                      if (!_data.personal || v == true) return;
                                      update(() => selected.remove(id));
                                    },
                            ),
                        ],
                      ],
                    ),
                    ),
                  ),
                  SafeArea(
                    top: false,
                    minimum: const EdgeInsets.all(16),
                    child: SizedBox(
                      width: double.infinity,
                      child: FilledButton(
                        style: FilledButton.styleFrom(
                          minimumSize: const Size.fromHeight(48),
                        ),
                        onPressed: _data.canEdit && valid
                            ? () {
                                if (!_data.canEdit) return;
                                _data.change(
                                  field,
                                  ProfileFieldAudience(
                                    audience,
                                    audience == 'COMMUNITY'
                                        ? (selected.toList()..sort())
                                        : const [],
                                  ),
                                );
                                Navigator.of(sheetContext).pop();
                              }
                            : null,
                        child: const Text('保留为待检查的修改'),
                      ),
                    ),
                  ),
                ],
              );
            },
          ),
        ),
      ),
    );
  }

  Future<void> _review() async {
    if (!_data.canEdit) return;
    if (_data.needsCommunities && !_data.communitiesLoaded) {
      await _data.loadCommunities();
    }
    if (!_current()) return;
    _data.prepareReview();
  }

  @override
  Widget build(BuildContext context) {
    final review = _data.review;
    return Scaffold(
      appBar: AppBar(title: const Text('各项资料的可见范围')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            const Text('决定谁可以读取每项资料。这里只修改受众，不改写原名称、简介或私人内容。'),
            const SizedBox(height: 8),
            const Text(
              '公开范围仍受整体资料权限限制。选择“本人智能体”对他人隐藏，仍需当前用途授权，不会开启模型、分析、自动学习或记忆。',
            ),
            if (!_data.personal) ...[
              const SizedBox(height: 16),
              Text(
                _data.retired
                    ? '身份或入口来源已变化，请返回设置重新打开。'
                    : '请使用个人账号；组织工作区不能管理本人资料受众。',
              ),
            ] else ...[
              const SizedBox(height: 16),
              if (_data.busy) const Text('正在读取或保存，请稍候…'),
              if (_data.error != null) Text(_data.error!),
              if (_data.notice != null) Text(_data.notice!),
              TextButton(
                style: TextButton.styleFrom(
                  minimumSize: const Size.fromHeight(48),
                ),
                onPressed: _data.busy ? null : () => unawaited(_data.load()),
                child: Text(_data.resultUnknown ? '核对当前设置（仅读取）' : '重新读取当前设置'),
              ),
              if (_data.record != null) ...[
                Text(
                  _data.record!.configured
                      ? '当前已保存自定义范围。'
                      : '尚未自定义：名称与简介沿用公开范围，其余九项仅本人。',
                ),
                const SizedBox(height: 8),
                for (final field in profileVisibilityLabels.entries)
                  ListTile(
                    key: ValueKey('audience-field-${field.key}'),
                    contentPadding: EdgeInsets.zero,
                    title: Text(field.value),
                    subtitle: Text(_ruleText(_data.draft[field.key]!)),
                    trailing: const Icon(Icons.edit_outlined),
                    onTap: _data.canEdit
                        ? () => unawaited(_pick(field.key))
                        : null,
                  ),
                if (_data.changed && review == null)
                  FilledButton(
                    style: FilledButton.styleFrom(
                      minimumSize: const Size.fromHeight(48),
                    ),
                    onPressed: _data.canEdit
                        ? () => unawaited(_review())
                        : null,
                    child: const Text('检查受众改动'),
                  ),
              ],
              if (review != null) ...[
                const Divider(height: 32),
                Text(
                  '检查当前版本 ${review.original.version} 的十一项受众设置',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                const SizedBox(height: 8),
                for (final field in profileVisibilityLabels.entries)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 12),
                    child: Text(
                      '${field.value}：${_ruleText(review.rules[field.key]!)}',
                    ),
                  ),
                const Text(
                  '保存会替换以上完整规则。好友、成员资格和原资料权限仍以读取时的有效状态为准；不会撤回已被读取的内容。',
                ),
                if (_data.needsCommunities &&
                    (!_data.communitiesLoaded ||
                        _data.unavailableIDs.isNotEmpty))
                  const Text(
                    '原已保存的部分社群本次列表未核实，仍保留原ID；保存时会再次核对当前成员资格。未列出不代表已退出。',
                  ),
                const SizedBox(height: 12),
                FilledButton(
                  style: FilledButton.styleFrom(
                    minimumSize: const Size.fromHeight(48),
                  ),
                  onPressed: _data.canEdit
                      ? () => unawaited(_data.save(review))
                      : null,
                  child: const Text('确认保存以上受众设置'),
                ),
                TextButton(
                  style: TextButton.styleFrom(
                    minimumSize: const Size.fromHeight(48),
                  ),
                  onPressed: _data.busy ? null : _data.cancelReview,
                  child: const Text('返回修改'),
                ),
              ],
            ],
          ],
        ),
      ),
    );
  }
}
