import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;
import 'private_place_memory_api.dart';
import 'private_place_memory_controller.dart';

/// One stable Place, current human owner. Public Place History is a separate
/// route; no location permission, cognitive grant or model is needed here.
class PrivatePlaceMemoryPage extends StatefulWidget {
  const PrivatePlaceMemoryPage({
    super.key,
    required this.placeID,
    required this.placeName,
    required this.authorizationHeader,
    required this.accountID,
    required this.identityChanges,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.client,
    this.apiBaseUrl,
    this.pendingStore,
  });
  final String placeID, placeName;
  final String? Function() authorizationHeader, accountID;
  final Listenable identityChanges;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final http.Client? client;
  final String? apiBaseUrl;
  final PlaceDeclarationPendingStore? pendingStore;
  @override
  State<PrivatePlaceMemoryPage> createState() => _PrivatePlaceMemoryPageState();
}

class _PrivatePlaceMemoryPageState extends State<PrivatePlaceMemoryPage> {
  late final String _placeID = widget.placeID;
  late final String? Function() _authorization = widget.authorizationHeader;
  late final String? Function() _accountID = widget.accountID;
  late final String? Function()? _workspaceID = widget.organizationWorkspaceID;
  late final Listenable _identityChanges = widget.identityChanges;
  late final Listenable? _workspaceChanges = widget.workspaceChanges;
  late final http.Client? _client = widget.client;
  late final String? _apiBaseUrl = widget.apiBaseUrl;
  late final PlaceDeclarationPendingStore? _pendingStore = widget.pendingStore;
  bool _retired = false, _listening = false;
  late final PrivatePlaceMemoryController _data = PrivatePlaceMemoryController(
    placeID: _placeID,
    authorizationHeader: () => _retired ? null : _authorization(),
    accountID: () => _retired ? null : _accountID(),
    organizationWorkspaceID: () => _workspaceID?.call(),
    api: PrivatePlaceMemoryApi(client: _client, apiBaseUrl: _apiBaseUrl),
    pendingStore: _pendingStore,
  );
  Route<bool>? _confirmation;
  String _kind = 'LIKED', _visibility = 'PRIVATE';
  int _days = 7;
  @override
  void initState() {
    super.initState();
    _identityChanges.addListener(_identityChanged);
    _workspaceChanges?.addListener(_identityChanged);
    _listening = true;
    _data.addListener(_changed);
    unawaited(_data.load());
  }

  void _changed() {
    if (!mounted || _retired) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && !_retired) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  void _closeConfirmation() {
    final route = _confirmation;
    _confirmation = null;
    void close() {
      if (route?.isActive ?? false) route!.navigator?.removeRoute(route);
    }

    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) => close());
    } else {
      close();
    }
  }

  void _identityChanged() {
    if (_retired) return;
    final g = _data.generation;
    _data.synchronizeIdentity();
    if (g == _data.generation) return;
    _closeConfirmation();
    _kind = 'LIKED';
    _visibility = 'PRIVATE';
    _days = 7;
    if (_data.personal) unawaited(_data.load());
  }

  @override
  void didUpdateWidget(covariant PrivatePlaceMemoryPage old) {
    super.didUpdateWidget(old);
    if (_retired) return;
    if (_placeID != widget.placeID ||
        !identical(_authorization, widget.authorizationHeader) ||
        !identical(_accountID, widget.accountID) ||
        !identical(_workspaceID, widget.organizationWorkspaceID) ||
        !identical(_identityChanges, widget.identityChanges) ||
        !identical(_workspaceChanges, widget.workspaceChanges) ||
        !identical(_client, widget.client) ||
        _apiBaseUrl != widget.apiBaseUrl ||
        !identical(_pendingStore, widget.pendingStore)) {
      _retired = true;
      _closeConfirmation();
      _data.removeListener(_changed);
      _data.dispose();
      _detachIdentity();
      _kind = 'LIKED';
      _visibility = 'PRIVATE';
      _days = 7;
    } else {
      _identityChanged();
    }
  }

  void _detachIdentity() {
    if (!_listening) return;
    _listening = false;
    _identityChanges.removeListener(_identityChanged);
    _workspaceChanges?.removeListener(_identityChanged);
  }

  @override
  void dispose() {
    _retired = true;
    _detachIdentity();
    _data.removeListener(_changed);
    _data.dispose();
    super.dispose();
  }

  bool get _busy => _data.loading || _data.saving;
  String _date(DateTime time) {
    final t = time.toLocal();
    return '${t.year}年${t.month}月${t.day}日 ${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}（本机时间）';
  }

  Future<bool> _confirm(
    PrivatePlaceMemoryController data,
    String title,
    List<Widget> content,
    String action,
  ) async {
    final g = data.generation;
    final route = DialogRoute<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(title),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: content,
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('返回修改'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(action),
          ),
        ],
      ),
    );
    _confirmation = route;
    final yes = await Navigator.of(context).push(route);
    if (identical(_confirmation, route)) _confirmation = null;
    if (!mounted || _retired || !identical(data, _data)) return false;
    data.synchronizeIdentity();
    return yes == true && g == data.generation;
  }

  Future<void> _save() async {
    if (_retired) return;
    final data = _data;
    data.edit(
      PlaceDeclarationDraft(
        kind: _kind,
        visibility: _visibility,
        validUntil: DateTime.now().toUtc().add(Duration(days: _days)),
      ),
    );
    final p = data.preview();
    if (p == null) {
      await data.load();
      return;
    }
    final d = p.draft!;
    final existing = p.controls.declaration(d.kind);
    if (await _confirm(data, '检查本人声明', [
      Text('以本人账号保存，目标：${widget.placeName}'),
      const SizedBox(height: 12),
      Text(placeMemoryKindLabels[d.kind]!),
      Text(
        existing == null
            ? '新建本人声明。'
            : '更新原记录第 ${existing.version} 版${existing.status == 'EXPIRED' ? '（已过期）' : ''}，保留原记录编号。',
      ),
      Text('可见范围：${placeMemoryVisibilityLabels[d.visibility]}'),
      Text('有效至：${_date(d.validUntil)}'),
      const SizedBox(height: 12),
      const Text('这只是本人声明，不会公开为到访或出席证明。Agent 范围标记不会授予模型读取许可。'),
    ], '确认保存本人声明')) {
      await data.submit(p);
    }
  }

  Future<void> _delete(PrivatePlaceDeclaration signal) async {
    if (_retired) return;
    final data = _data;
    final p = data.preview(deleting: signal);
    if (p == null) {
      await data.load();
      return;
    }
    if (await _confirm(data, '检查撤回内容', [
      Text(_data.view == null ? '所选地点的本人声明' : widget.placeName),
      Text(placeMemoryKindLabels[signal.kind]!),
      const Text('仅撤回这条本人声明；不会删除收藏或动态。已撤回的记录编号不会重新启用。'),
    ], '确认撤回声明')) {
      await data.submit(p);
    }
  }

  Future<void> _stop() async {
    if (_retired) return;
    final data = _data, pending = _data.pending;
    if (await _confirm(data, '停止本机核实？', [
          const Text('原提交可能已经保存或撤回。停止核实不会撤销服务端操作，也不会把未知结果变成成功。重新编辑前仍需读取当前记录。'),
        ], '停止本机核实') &&
        pending != null &&
        identical(pending, data.pending)) {
      await data.stopRecovery();
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_retired) {
      return Scaffold(
        appBar: AppBar(title: const Text('我的地点记录')),
        body: SafeArea(
          child: ListView(
            padding: const EdgeInsets.all(20),
            children: [
              const Text('地点记录连接已变更，请返回后重新打开。'),
              const SizedBox(height: 16),
              const Text('旧预览不能继续提交。未确认的操作仍按原连接、本人账号和地点保留在本机；没有自动重发、迁移、清除或撤回。'),
              const SizedBox(height: 16),
              const Text('返回原入口重新打开后，读取当前记录并核实原操作；结果未知不代表成功或回滚。'),
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
    final view = _data.view;
    final controls = _data.controls;
    return Scaffold(
      appBar: AppBar(title: const Text('我的地点记录')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            Text(
              view == null ? '所选地点' : widget.placeName,
              style: Theme.of(context).textTheme.headlineSmall,
            ),
            const SizedBox(height: 12),
            const Text('只查看本人当前记录。收藏、动态关联、本人声明分别展示；它们不证明到访或出席。无需 AI 或定位。'),
            const SizedBox(height: 16),
            if (!_data.personal) const Text('请使用已登录的本人账号；组织工作台不能查看这些私人记录。'),
            if (_busy)
              const LinearProgressIndicator(semanticsLabel: '正在读取或提交地点记录'),
            if (_data.error != null)
              Semantics(
                liveRegion: true,
                child: Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Text(_data.error!),
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
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton.icon(
                onPressed: _busy ? null : _data.load,
                icon: const Icon(Icons.refresh),
                label: const Text('读取当前记录'),
              ),
            ),
            if (_data.uncertain) ...[
              const Text('原操作结果待核实。再次核实只使用原记录编号和原版本，不创建新记录。'),
              Text(
                _data.pending!.deleting
                    ? '原操作：撤回这条本人声明。'
                    : '原操作：${placeMemoryKindLabels[_data.pending!.draft!.kind]}',
              ),
              if (!_data.pending!.deleting) ...[
                Text(
                  '范围：${placeMemoryVisibilityLabels[_data.pending!.draft!.visibility]}',
                ),
                Text('原有效至：${_date(_data.pending!.draft!.validUntil)}'),
              ],
              Align(
                alignment: Alignment.centerLeft,
                child: FilledButton(
                  onPressed: _data.canRetryOriginal
                      ? _data.retryOriginal
                      : null,
                  child: const Text('核实原操作（安全重试）'),
                ),
              ),
              Align(
                alignment: Alignment.centerLeft,
                child: TextButton(
                  onPressed: _busy ? null : _stop,
                  child: const Text('停止本机核实（结果仍未知）'),
                ),
              ),
            ],
            if (controls != null) ...[
              const Divider(height: 32),
              Text('本人声明管理', style: Theme.of(context).textTheme.titleMedium),
              const SizedBox(height: 8),
              const Text(
                '独立读取本人声明；与当前公开地点来源并非同一次快照。过期声明不作为当前信号，仍可用原记录编号续期或撤回。',
              ),
              if (controls.declarations.isEmpty)
                const Text('当前没有待管理的本人声明；这不证明到访情况或原操作结果。'),
              for (final d in controls.declarations)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 8),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(placeMemoryKindLabels[d.kind]!),

                      Text(placeMemoryVisibilityLabels[d.visibility]!),
                      Text(
                        d.status == 'EXPIRED'
                            ? '已过期，当前不作为地点信号'
                            : '有效的本人声明（未经核验）',
                      ),
                      Text('有效至：${_date(d.validUntil)}'),
                      TextButton(
                        onPressed: _busy || _data.uncertain
                            ? null
                            : () => _delete(d),
                        child: const Text('撤回这条声明'),
                      ),
                    ],
                  ),
                ),
            ],
            if (view != null) ...[
              const Divider(height: 32),
              Text('当前本人来源', style: Theme.of(context).textTheme.titleMedium),
              const SizedBox(height: 8),
              if (view.signals.isEmpty) const Text('目前没有可见的本人地点记录。这不表示从未到访。'),
              for (final signal in view.signals.where((s) => !s.declaration))
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 8),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(placeMemoryKindLabels[signal.kind]!),
                      Text('记录更新：${_date(signal.updatedAt)}'),
                      if (signal.validUntil != null)
                        Text('有效至：${_date(signal.validUntil!)}'),
                    ],
                  ),
                ),
              const SizedBox(height: 12),
              const Text('到访核验、活动出席及模型读取：当前不可用。'),
              if (!_data.uncertain && controls != null) ...[
                const Divider(height: 32),
                Text('填写本人声明', style: Theme.of(context).textTheme.titleMedium),
                const SizedBox(height: 12),
                const Text('声明内容'),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    for (final kind in ['LIKED', 'VISITED'])
                      ChoiceChip(
                        label: Text(kind == 'LIKED' ? '我喜欢这里' : '自述到访（未核验）'),
                        selected: _kind == kind,
                        onSelected: _busy
                            ? null
                            : (_) => setState(() => _kind = kind),
                        materialTapTargetSize: MaterialTapTargetSize.padded,
                      ),
                  ],
                ),
                const SizedBox(height: 16),
                const Text('可见范围'),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    for (final v in placeMemoryVisibilityLabels.keys)
                      ChoiceChip(
                        label: Text(v == 'PRIVATE' ? '仅本人管理' : 'Agent 范围标记'),
                        selected: _visibility == v,
                        onSelected: _busy
                            ? null
                            : (_) => setState(() => _visibility = v),
                        materialTapTargetSize: MaterialTapTargetSize.padded,
                      ),
                  ],
                ),
                const Padding(
                  padding: EdgeInsets.only(top: 8),
                  child: Text('两种范围都不是公开展示；Agent 范围标记仍需另行认知授权，不会直接启用模型。'),
                ),
                const SizedBox(height: 16),
                const Text('有效期（从确认前计算）'),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    for (final d in [1, 7, 30])
                      ChoiceChip(
                        label: Text('$d 天'),
                        selected: _days == d,
                        onSelected: _busy
                            ? null
                            : (_) => setState(() => _days = d),
                        materialTapTargetSize: MaterialTapTargetSize.padded,
                      ),
                  ],
                ),
                const SizedBox(height: 20),
                FilledButton(
                  onPressed: _busy ? null : _save,
                  child: const Text('检查并保存本人声明'),
                ),
              ],
            ],
            const SizedBox(height: 24),
          ],
        ),
      ),
    );
  }
}
