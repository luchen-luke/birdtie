import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import 'social_preference_seed_controller.dart';

class SocialPreferenceSeedSheet extends StatefulWidget {
  const SocialPreferenceSeedSheet({
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
  // Optional lifetime of a caller-owned entry; direct callers keep existing behavior.
  final bool Function()? current;
  @override
  State<SocialPreferenceSeedSheet> createState() =>
      _SocialPreferenceSeedSheetState();
}

class _SocialPreferenceSeedSheetState extends State<SocialPreferenceSeedSheet> {
  late final SocialPreferenceSeedController _controller;
  SocialPreferenceRecord? _base;
  List<String> _selected = [];
  bool _dirty = false, _review = false;
  String? _token, _owner, _workspace;
  bool get _current => mounted && (widget.current?.call() ?? true);
  bool get _personal => widget.organizationWorkspaceID?.call() == null;
  @override
  void initState() {
    super.initState();
    _token = widget.auth.authorizationHeader;
    _owner = widget.auth.accountID;
    _workspace = widget.organizationWorkspaceID?.call();
    _controller = SocialPreferenceSeedController(
      authorizationHeader: () => widget.auth.authorizationHeader,
      accountID: () => widget.auth.accountID,
      organizationWorkspaceID: widget.organizationWorkspaceID,
      client: widget.client,
      apiBaseUrl: widget.apiBaseUrl,
    )..addListener(_changed);
    widget.auth.addListener(_identityChanged);
    widget.workspaceChanges?.addListener(_identityChanged);
    unawaited(_loadCurrent());
  }

  Future<void> _loadCurrent() async {
    if (!_current) return;
    await _controller.load();
  }

  void _clear() {
    _base = null;
    _selected = [];
    _dirty = false;
    _review = false;
  }

  void _identityChanged() {
    if (!_current) {
      _clear();
      return;
    }
    final token = widget.auth.authorizationHeader,
        owner = widget.auth.accountID;
    final workspace = widget.organizationWorkspaceID?.call();
    if (token == _token && owner == _owner && workspace == _workspace) return;
    _token = token;
    _owner = owner;
    _workspace = workspace;
    _clear();
    _controller.synchronizeIdentity();
    if (mounted) setState(() {});
    if (_personal && token != null) unawaited(_loadCurrent());
  }

  void _changed() {
    if (!_current) return;
    if (_controller.denied) _clear();
    final value = _controller.record;
    if (value != null && !identical(value, _base)) {
      _base = value;
      if (!_dirty) _selected = List<String>.from(value.preferences);
      _review = false;
    }
    setState(() {});
  }

  Future<void> _save() async {
    final base = _base;
    if (!_current || base == null || !_personal || !_review) return;
    final saved = await _controller.save(base, List<String>.from(_selected));
    if (!mounted || !_current ||
        !saved ||
        widget.auth.authorizationHeader != _token ||
        widget.auth.accountID != _owner ||
        !_personal) {
      return;
    }
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(SnackBar(content: Text(_controller.message!)));
    Navigator.of(context).pop(true);
  }

  @override
  void dispose() {
    widget.auth.removeListener(_identityChanged);
    widget.workspaceChanges?.removeListener(_identityChanged);
    _controller.removeListener(_changed);
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final base = _base, busy = _controller.busy;
    final editable =
        _current &&
        !busy &&
        !_controller.denied &&
        !_controller.conflict &&
        !_controller.resultUnknown;
    final staleChoices =
        base != null &&
        _selected.any(
          (value) =>
              !socialPreferenceChoices.contains(value) &&
              !base.preferences.contains(value),
        );
    final selectionValid = !staleChoices && _selected.length <= 20;
    return Scaffold(
      appBar: AppBar(title: const Text('社交偏好')),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(20, 20, 20, 32),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text(
                _review ? '检查你的选择' : '你更喜欢怎样认识人？',
                style: Theme.of(context).textTheme.headlineSmall,
              ),
              const SizedBox(height: 12),
              const Text('这些选择仅保存在你的私密资料中。可以多选，也可以跳过；不会公开或自动发出申请。'),
              const SizedBox(height: 24),
              if (!_personal)
                const Text('请切回个人身份后再编辑社交偏好。')
              else if (!widget.auth.signedIn)
                const Text('请先登录后再编辑社交偏好。')
              else if (base == null && busy)
                const LinearProgressIndicator()
              else if (base != null) ...[
                if (!_review)
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [
                      for (final choice in {
                        ...socialPreferenceChoices,
                        ...base.preferences,
                        ..._selected,
                      })
                        FilterChip(
                          label: Text(choice),
                          selected: _selected.contains(choice),
                          onSelected: editable
                              ? (selected) => setState(() {
                                  _dirty = true;
                                  if (selected) {
                                    _selected.add(choice);
                                  } else {
                                    _selected.remove(choice);
                                  }
                                })
                              : null,
                        ),
                    ],
                  )
                else ...[
                  Text(
                    _selected.isEmpty
                        ? '不保留社交偏好'
                        : '私密偏好：${_selected.join('、')}',
                  ),
                  const SizedBox(height: 12),
                  const Text(
                    '保存为本人明确选择，保留其它私密资料。选择“同一大学”不会核验学校身份，也不授予 Agent 或模型使用权限。',
                  ),
                ],
                const SizedBox(height: 24),
                if (staleChoices) const Text('部分原有描述已在别处删除。请取消这些项，再检查当前选择。'),
                if (_selected.length > 20) const Text('最多保留 20 项，请减少所选偏好。'),
                if (_review)
                  TextButton(
                    onPressed: editable
                        ? () => setState(() => _review = false)
                        : null,
                    child: const Text('返回修改'),
                  ),
                FilledButton(
                  onPressed: editable && selectionValid
                      ? (_review ? _save : () => setState(() => _review = true))
                      : null,
                  child: Text(
                    busy
                        ? '正在保存…'
                        : _review
                        ? '保存私密偏好'
                        : '检查选择',
                  ),
                ),
                TextButton(
                  onPressed: busy
                      ? null
                      : () => Navigator.of(context).pop(false),
                  child: Text(_review ? '取消' : '跳过，保留已有偏好'),
                ),
              ],
              if (_controller.error != null) ...[
                const SizedBox(height: 16),
                Text(
                  _controller.error!,
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
                TextButton(
                  onPressed: busy ? null : _loadCurrent,
                  child: Text(
                    _controller.resultUnknown ? '读取当前资料核实' : '重新读取（保留草稿）',
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
    );
  }
}
