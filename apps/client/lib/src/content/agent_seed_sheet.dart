import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../auth/birdtie_auth_controller.dart';
import 'agent_seed_controller.dart';
import 'agent_profile_completion_sheet.dart';
import 'profile_completion_choice.dart';

class AgentSeedSheet extends StatefulWidget {
  const AgentSeedSheet({
    super.key,
    required this.auth,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.onlyMissing = false,
    this.progressive = false,
  });
  final BirdtieAuthController auth;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final bool onlyMissing;
  final bool progressive;
  @override
  State<AgentSeedSheet> createState() => _AgentSeedSheetState();
}

class _AgentSeedSheetState extends State<AgentSeedSheet> {
  final _name = TextEditingController();
  final _form = GlobalKey<FormState>();
  late final AgentSeedController _controller;
  late final BirdtieAuthController _boundAuth;
  late final Listenable? _boundWorkspaceChanges;
  late final String? Function()? _boundWorkspace;
  bool _retired = false;
  AgentSeedRecord? _base;
  String? _city;
  String? _intent;
  Set<String> _languages = {};
  Set<String> _interests = {};
  bool _skipInterests = true;
  bool _dirty = false;
  int _step = 0;
  ProfileCompletionGroup? _chosen;
  List<int> _completionSteps = [];
  String? _validation;
  String? _token;
  String? _owner;
  String? _workspace;
  static const _suggestedInterests = [
    '羽毛球',
    '徒步',
    '摄影',
    '咖啡',
    '音乐',
    '旅行',
    '游戏',
    '跑步',
    '美食',
    '语言交流',
  ];
  bool get _personal => _boundWorkspace?.call() == null;

  @override
  void initState() {
    super.initState();
    _boundAuth = widget.auth;
    _boundWorkspaceChanges = widget.workspaceChanges;
    _boundWorkspace = widget.organizationWorkspaceID;
    _token = _boundAuth.authorizationHeader;
    _owner = _boundAuth.accountID;
    _workspace = _boundWorkspace?.call();
    _controller = AgentSeedController(
      authorizationHeader: () => _boundAuth.authorizationHeader,
      accountID: () => _boundAuth.accountID,
      organizationWorkspaceID: _boundWorkspace,
      client: widget.client,
      apiBaseUrl: widget.apiBaseUrl,
    )..addListener(_changed);
    _boundAuth.addListener(_identityChanged);
    _boundWorkspaceChanges?.addListener(_identityChanged);
    unawaited(_controller.load());
    if (widget.progressive) _step = -1;
  }

  @override
  void didUpdateWidget(AgentSeedSheet old) {
    super.didUpdateWidget(old);
    if (!_retired &&
        (old.auth != widget.auth ||
            old.client != widget.client ||
            old.apiBaseUrl != widget.apiBaseUrl ||
            old.workspaceChanges != widget.workspaceChanges ||
            old.organizationWorkspaceID != widget.organizationWorkspaceID ||
            old.onlyMissing != widget.onlyMissing ||
            old.progressive != widget.progressive)) {
      // The old review belongs to one transport and identity frame. Rebinding
      // this State must not send new credentials to its old endpoint, or allow
      // A -> B -> A to revive its draft, approval or uncertain result.
      _retired = true;
      _detachBinding();
      _clearDraft();
      _controller.dispose();
    }
  }

  void _detachBinding() {
    _boundAuth.removeListener(_identityChanged);
    _boundWorkspaceChanges?.removeListener(_identityChanged);
    _controller.removeListener(_changed);
  }

  void _clearDraft() {
    _name.clear();
    _city = null;
    _intent = null;
    _languages = {};
    _interests = {};
    _skipInterests = true;
    _base = null;
    _dirty = false;
    _step = 0;
    if (widget.progressive) _step = -1;
    _chosen = null;
    _completionSteps = [];
    _validation = null;
  }

  void _identityChanged() {
    if (_retired || !mounted) return;
    final token = _boundAuth.authorizationHeader;
    final owner = _boundAuth.accountID;
    final workspace = _boundWorkspace?.call();
    if (token == _token && owner == _owner && workspace == _workspace) return;
    _token = token;
    _owner = owner;
    _workspace = workspace;
    _clearDraft();
    _controller.synchronizeIdentity();
    if (mounted) setState(() {});
    if (token != null && _personal) unawaited(_controller.load());
  }

  void _changed() {
    if (!mounted || _retired) return;
    if (_controller.denied) _clearDraft();
    final value = _controller.record;
    if (value != null && !identical(value, _base)) {
      _base = value;
      if (!_dirty || (widget.progressive && _chosen?.index != 0)) {
        _name.text = value.displayName;
        _city = value.currentCityID;
      }
      if (!_dirty || (widget.progressive && _chosen?.index != 1)) {
        _languages = value.languages.toSet();
        _intent = value.basicIntent.isEmpty ? null : value.basicIntent;
      }
      if (!_dirty || (widget.progressive && _chosen?.index != 2)) {
        _interests = value.interests.toSet();
      }
      _step = widget.progressive
          ? -1
          : widget.onlyMissing &&
                !_dirty &&
                value.currentCityID != null &&
                value.displayName.trim().isNotEmpty
          ? (value.languages.isNotEmpty && value.basicIntent.isNotEmpty ? 2 : 1)
          : 0;
    }
    setState(() {});
  }

  void _choose(ProfileCompletionGroup group) {
    final base = _base!;
    // A fresh choice discards the previous unsaved group. After a conflict the
    // same group retains its explicit draft, while untouched sources refresh.
    if (_chosen != group) {
      _name.text = base.displayName;
      _city = base.currentCityID;
      _languages = base.languages.toSet();
      _intent = base.basicIntent.isEmpty ? null : base.basicIntent;
      _interests = base.interests.toSet();
      _dirty = false;
    }
    if (_chosen != group) {
      _skipInterests = group != ProfileCompletionGroup.interests;
    }
    _chosen = group;
    _completionSteps = completionSteps(
      base,
      group,
    ).map((v) => v.index).toList();
    setState(() {
      _step = _completionSteps.first;
      _validation = null;
    });
  }

  void _back() => setState(() {
    if (widget.progressive) {
      final index = _completionSteps.indexOf(_step);
      _step = _step == 3
          ? _completionSteps.last
          : index > 0
          ? _completionSteps[index - 1]
          : -1;
    } else {
      _step--;
    }
    _validation = null;
  });

  void _next() {
    if (_name.value.composing.isValid && !_name.value.composing.isCollapsed) {
      return;
    }
    if (_step == 0) {
      if (!_form.currentState!.validate()) return;
      if (_city == null || !_base!.cities.any((c) => c.id == _city)) {
        setState(() => _validation = '请选择当前仍可用的城市，也可以稍后再说。');
        return;
      }
    }
    if (_step == 1 && (_languages.isEmpty || _intent == null)) {
      setState(() => _validation = '请选择交流语言和这次想做的事。');
      return;
    }
    FocusScope.of(context).unfocus();
    setState(() {
      if (widget.progressive) {
        final index = _completionSteps.indexOf(_step);
        _step = index + 1 < _completionSteps.length
            ? _completionSteps[index + 1]
            : 3;
      } else {
        _step++;
      }
      _validation = null;
    });
  }

  Future<void> _save({bool defer = false}) async {
    final base = _base;
    if (_retired || base == null || !_personal) return;
    if (!defer &&
        (!base.cities.any((c) => c.id == _city) ||
            _languages.isEmpty ||
            !seedIntentLabels.containsKey(_intent) ||
            utf8.encode(_name.text.trim()).length < 2 ||
            utf8.encode(_name.text.trim()).length > 80)) {
      setState(() {
        _validation = '必要设置仍有缺项，请返回修改。草稿尚未保存。';
        _step = widget.progressive ? -1 : 0;
      });
      return;
    }
    final choices = defer
        ? <String, dynamic>{'action': 'DEFER'}
        : <String, dynamic>{
            'action': 'SAVE',
            'displayName': _name.text.trim(),
            'currentCityId': _city,
            'currentCitySnapshot': base.cities
                .firstWhere((c) => c.id == _city)
                .snapshot,
            'languagePreferences': _languages.toList(),
            'basicIntent': _intent,
            'interestChoice': _skipInterests ? 'SKIP' : 'SET',
            'interests': _skipInterests ? <String>[] : _interests.toList(),
          };
    final saved = await _controller.save(base, choices);
    if (!mounted ||
        _retired ||
        !saved ||
        _boundAuth.authorizationHeader != _token ||
        !_personal) {
      return;
    }
    if (!defer) {
      _boundAuth.updateProfileDisplayName(_controller.record!.displayName);
    }
    Navigator.of(context).pop(true);
  }

  @override
  void dispose() {
    if (!_retired) _detachBinding();
    _controller.dispose();
    _name.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (_retired) {
      return Scaffold(
        appBar: AppBar(title: Text(widget.progressive ? '完善我的选择' : '初始设置')),
        body: SafeArea(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(20),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                const Text('设置入口已变化，请返回后重新打开。'),
                const SizedBox(height: 16),
                const Text('原草稿和确认已失效。若保存请求已发出，返回后请读取当前设置核实；此处不会再次提交。'),
                const SizedBox(height: 16),
                FilledButton(
                  onPressed: () => Navigator.of(context).maybePop(),
                  child: const Text('返回设置'),
                ),
              ],
            ),
          ),
        ),
      );
    }
    final busy = _controller.busy;
    final base = _base;
    final editable =
        !busy &&
        !_controller.resultUnknown &&
        !_controller.denied &&
        !_controller.conflict;
    return Scaffold(
      appBar: AppBar(title: Text(widget.progressive ? '完善我的选择' : '初始设置')),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(20, 20, 20, 32),
          child: Form(
            key: _form,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(
                  '让 Birdtie 更了解你的选择',
                  style: Theme.of(context).textTheme.headlineSmall,
                ),
                const SizedBox(height: 8),
                const Text('当前以本人身份设置。城市是你的声明；语言、兴趣和这次意图保存在私密设置中，不会自动发布或交给模型。'),
                const SizedBox(height: 24),
                if (!_personal)
                  const Text('请切回个人身份后再完善初始设置。')
                else if (!_boundAuth.signedIn)
                  const Text('请先登录后再完善初始设置。')
                else if (base == null && busy)
                  const LinearProgressIndicator()
                else if (base != null) ...[
                  if (widget.progressive && _step == -1) ...[
                    const Text('这次想完善哪一项？'),
                    const SizedBox(height: 8),
                    const Text('一次选择一组。切换项目会放弃上一组未保存的修改；取消不会写入。'),
                    ListTile(
                      contentPadding: EdgeInsets.zero,
                      title: const Text('用已确认记忆补齐活动偏好'),
                      subtitle: const Text('仅补空项，先检查来源与独立保存后果'),
                      trailing: const Icon(Icons.chevron_right),
                      onTap: editable && !_dirty
                          ? () async {
                              final token = _boundAuth.authorizationHeader;
                              final owner = _boundAuth.accountID;
                              final currentWorkspace = _boundWorkspace?.call();
                              await Navigator.of(context).push<void>(
                                MaterialPageRoute(
                                  builder: (_) => AgentProfileCompletionSheet(
                                    auth: _boundAuth,
                                    client: widget.client,
                                    apiBaseUrl: widget.apiBaseUrl,
                                    workspaceChanges: _boundWorkspaceChanges,
                                    organizationWorkspaceID: _boundWorkspace,
                                  ),
                                ),
                              );
                              if (mounted &&
                                  !_retired &&
                                  !_dirty &&
                                  token == _boundAuth.authorizationHeader &&
                                  owner == _boundAuth.accountID &&
                                  currentWorkspace == _boundWorkspace?.call()) {
                                // The native private CAS also advances the metadata
                                // revision used by the original seed. Reload before
                                // another parent write, retaining any active draft.
                                await _controller.load();
                              }
                            }
                          : null,
                    ),
                    for (final group in ProfileCompletionGroup.values)
                      ListTile(
                        contentPadding: EdgeInsets.zero,
                        title: Text(group.label),
                        subtitle: Text(
                          requiredCompletionGroups(base).contains(group)
                              ? '当前必要设置有缺项'
                              : group == ProfileCompletionGroup.interests &&
                                    base.interests.isEmpty
                              ? '可选，可以以后再说'
                              : '已有选择，可查看或修改',
                        ),
                        trailing: const Icon(Icons.chevron_right),
                        onTap: editable ? () => _choose(group) : null,
                      ),
                  ] else ...[
                    if (widget.progressive &&
                        _completionSteps.length > 1 &&
                        _step != 3)
                      const Text('其它必要设置仍有缺项。每次只补一组，检查全部选择后才会一起保存。'),
                    Text(
                      const [
                        '怎么称呼你，当前在哪座城市？',
                        '用什么语言，这次想做什么？',
                        '兴趣可以以后再说',
                        '检查后保存',
                      ][_step],
                      style: Theme.of(context).textTheme.titleLarge,
                    ),
                    const SizedBox(height: 16),
                    if (_step == 0) ...[
                      TextFormField(
                        controller: _name,
                        enabled: editable,
                        decoration: InputDecoration(
                          labelText: '昵称',
                          helperText:
                              '原个人资料范围：${base.visibility == 'public' ? '公开' : '私密'}。保存沿用现有隐私设置。',
                          helperMaxLines: 3,
                        ),
                        textInputAction: TextInputAction.next,
                        onChanged: (_) => _dirty = true,
                        validator: (v) {
                          final text = v?.trim() ?? '';
                          if (utf8.encode(text).length < 2 ||
                              utf8.encode(text).length > 80) {
                            return '昵称不能为空，过长时请缩短。';
                          }
                          return null;
                        },
                      ),
                      const SizedBox(height: 16),
                      DropdownButtonFormField<String>(
                        initialValue: base.cities.any((c) => c.id == _city)
                            ? _city
                            : null,
                        isExpanded: true,
                        decoration: const InputDecoration(
                          labelText: '本人声明的当前城市',
                        ),
                        items: [
                          for (final c in base.cities)
                            DropdownMenuItem(
                              value: c.id,
                              child: Text(
                                c.name,
                                overflow: TextOverflow.ellipsis,
                              ),
                            ),
                        ],
                        onChanged: editable
                            ? (v) => setState(() {
                                _city = v;
                                _dirty = true;
                              })
                            : null,
                      ),
                      if (base.cities.isEmpty) const Text('暂无可选择的城市，可以稍后再完善。'),
                    ],
                    if (_step == 1) ...[
                      const Text('交流语言（不会改变界面语言）'),
                      Wrap(
                        spacing: 8,
                        runSpacing: 8,
                        children: [
                          for (final code in {..._languages, 'zh-CN', 'en'})
                            FilterChip(
                              label: Text(
                                code == 'zh-CN'
                                    ? '简体中文'
                                    : code == 'en'
                                    ? '英语'
                                    : code,
                              ),
                              selected: _languages.contains(code),
                              onSelected: editable
                                  ? (on) => setState(() {
                                      on
                                          ? _languages.add(code)
                                          : _languages.remove(code);
                                      _dirty = true;
                                    })
                                  : null,
                            ),
                        ],
                      ),
                      const SizedBox(height: 16),
                      DropdownButtonFormField<String>(
                        initialValue: _intent,
                        isExpanded: true,
                        decoration: const InputDecoration(
                          labelText: '这次来到 Birdtie 想做什么？',
                        ),
                        items: [
                          for (final e in seedIntentLabels.entries)
                            DropdownMenuItem(
                              value: e.key,
                              child: Text(e.value),
                            ),
                        ],
                        onChanged: editable
                            ? (v) => setState(() {
                                _intent = v;
                                _dirty = true;
                              })
                            : null,
                      ),
                    ],
                    if (_step == 2) ...[
                      SwitchListTile(
                        contentPadding: EdgeInsets.zero,
                        title: const Text('跳过初始兴趣'),
                        subtitle: const Text('跳过会保留已有兴趣，不会猜测或清空。'),
                        value: _skipInterests,
                        onChanged: editable
                            ? (v) => setState(() {
                                _skipInterests = v;
                                _dirty = true;
                              })
                            : null,
                      ),
                      if (!_skipInterests)
                        Wrap(
                          spacing: 8,
                          runSpacing: 8,
                          children: [
                            for (final item in {
                              ..._interests,
                              ..._suggestedInterests,
                            })
                              FilterChip(
                                label: Text(item),
                                selected: _interests.contains(item),
                                onSelected: editable
                                    ? (on) => setState(() {
                                        on
                                            ? _interests.add(item)
                                            : _interests.remove(item);
                                        _dirty = true;
                                      })
                                    : null,
                              ),
                          ],
                        ),
                    ],
                    if (_step == 3) ...[
                      Text('昵称：${_name.text.trim()}'),
                      Text(
                        '原个人资料范围：${base.visibility == 'public' ? '公开' : '私密'}，沿用现有设置；字段仍受隐私设置控制。',
                      ),
                      Text(
                        '保存为私人城市声明：${base.cities.where((c) => c.id == _city).first.name}',
                      ),
                      if (widget.progressive)
                        const Text(
                          '复用已有城市也会将这条当前城市声明设为私密。保存沿用原设置的完整原子提交，未改的选择采用当前读取值。',
                        ),
                      Text(
                        '交流语言：${_languages.map((v) => v == 'zh-CN'
                            ? '简体中文'
                            : v == 'en'
                            ? '英语'
                            : v).join('、')}',
                      ),
                      Text('私密意图：${seedIntentLabels[_intent]}'),
                      Text(
                        _skipInterests
                            ? '兴趣：跳过，保留已有内容'
                            : '私密兴趣：${_interests.isEmpty ? '未选择' : _interests.join('、')}',
                      ),
                      const SizedBox(height: 12),
                      const Text('保存的是你的明确选择，不代表身份、到访证明或允许模型分析。'),
                    ],
                    if (_validation != null)
                      Padding(
                        padding: const EdgeInsets.only(top: 12),
                        child: Text(
                          _validation!,
                          style: TextStyle(
                            color: Theme.of(context).colorScheme.error,
                          ),
                        ),
                      ),
                    const SizedBox(height: 24),
                    if (_step > 0 || widget.progressive)
                      TextButton(
                        onPressed: busy ? null : _back,
                        child: const Text('返回修改'),
                      ),
                    FilledButton(
                      onPressed: editable
                          ? (_step == 3 ? () => _save() : _next)
                          : null,
                      child: Text(
                        busy
                            ? '正在保存…'
                            : _step == 3
                            ? '保存这些设置'
                            : '下一步',
                      ),
                    ),
                    if (!widget.progressive)
                      TextButton(
                        onPressed: editable ? () => _save(defer: true) : null,
                        child: const Text('稍后再说'),
                      ),
                  ],
                  if (widget.progressive)
                    TextButton(
                      onPressed: busy
                          ? null
                          : () => Navigator.of(context).pop(),
                      child: const Text('取消，保留当前设置'),
                    ),
                ],
                if (_controller.error != null) ...[
                  const SizedBox(height: 16),
                  Text(
                    _controller.error!,
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                  TextButton(
                    onPressed: busy ? null : () => _controller.load(),
                    child: Text(
                      _controller.resultUnknown ? '读取当前设置核实' : '重新读取（保留草稿）',
                    ),
                  ),
                ],
                if (_controller.message != null)
                  Padding(
                    padding: const EdgeInsets.only(top: 12),
                    child: Text(_controller.message!),
                  ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
