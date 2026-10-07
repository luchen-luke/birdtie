import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import 'now_context_selection_api.dart';
import 'now_context_selection_controller.dart';

/// Chooses a view only. The host owns query submission, map, current Task,
/// ResultSet and identity; this page never changes those domain objects.
class NowContextSelectionPage extends StatefulWidget {
  const NowContextSelectionPage({
    super.key,
    required this.auth,
    required this.onSelect,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.onOpenOnlineOpportunities,
  });
  final BirdtieAuthController auth;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final ValueChanged<NowContextChoice> onSelect;
  final VoidCallback? onOpenOnlineOpportunities;
  @override
  State<NowContextSelectionPage> createState() =>
      _NowContextSelectionPageState();
}

class _NowContextSelectionPageState extends State<NowContextSelectionPage> {
  NowContextSelectionController _create() => NowContextSelectionController(
    api: NowContextSelectionAPI(
      client: widget.client,
      apiBaseUrl: widget.apiBaseUrl,
    ),
    identity: () => NowSelectionIdentity(
      widget.auth.authorizationHeader,
      widget.auth.accountID,
      widget.organizationWorkspaceID?.call(),
    ),
  );
  late NowContextSelectionController _data = _create();
  Timer? _timer;
  @override
  void initState() {
    super.initState();
    widget.auth.addListener(_identity);
    widget.workspaceChanges?.addListener(_identity);
    _data.addListener(_changed);
    unawaited(_data.load());
    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted) _data.expire();
    });
  }

  void _changed() {
    if (!mounted) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  void _identity() {
    _data.sync();
  }

  @override
  void didUpdateWidget(NowContextSelectionPage old) {
    super.didUpdateWidget(old);
    if (old.auth != widget.auth) {
      old.auth.removeListener(_identity);
      widget.auth.addListener(_identity);
    }
    if (old.workspaceChanges != widget.workspaceChanges) {
      old.workspaceChanges?.removeListener(_identity);
      widget.workspaceChanges?.addListener(_identity);
    }
    if (old.auth != widget.auth ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.workspaceChanges != widget.workspaceChanges ||
        old.organizationWorkspaceID != widget.organizationWorkspaceID ||
        old.onSelect != widget.onSelect) {
      _data.removeListener(_changed);
      _data.dispose();
      _data = _create();
      _data.addListener(_changed);
      final created = _data;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && identical(created, _data)) unawaited(created.load());
      });
    } else {
      _data.sync();
    }
  }

  Future<void> _select() async {
    final data = _data, callback = widget.onSelect, gen = data.generation;
    final result = await data.resolve();
    if (!mounted ||
        !identical(data, _data) ||
        callback != widget.onSelect ||
        !data.current ||
        gen != data.generation ||
        result == null ||
        !result.expiresAt.isAfter(DateTime.now())) {
      return;
    }
    callback(result);
  }

  @override
  void dispose() {
    _timer?.cancel();
    widget.auth.removeListener(_identity);
    widget.workspaceChanges?.removeListener(_identity);
    _data.removeListener(_changed);
    _data.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final selected = _data.selected;
    return Scaffold(
      appBar: AppBar(title: const Text('选择查看情境')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            const Text(
              '切换查看范围不会修改所在地声明、加入社群或发起查询。当前意图、结果和地图视角保持；之后主动查询才使用新范围。',
            ),
            const SizedBox(height: 16),
            if (!_data.current)
              Text(
                widget.organizationWorkspaceID?.call() != null
                    ? '请切回个人身份选择情境。'
                    : widget.auth.authorizationHeader == null
                    ? '登录后可选择本人声明或公开目的地。'
                    : '账号或工作区已变化，请关闭后重新打开情境选择。',
              )
            else ...[
              if (_data.busy) const LinearProgressIndicator(),
              TextButton.icon(
                onPressed: _data.busy ? null : _data.load,
                style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
                icon: const Icon(Icons.refresh),
                label: const Text('刷新可用情境'),
              ),
              if (widget.onOpenOnlineOpportunities != null)
                TextButton.icon(
                  style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
                  onPressed: _data.ready
                      ? () {
                          _data.sync();
                          _data.expire();
                          if (_data.ready) {
                            widget.onOpenOnlineOpportunities?.call();
                          }
                        }
                      : null,
                  icon: const Icon(Icons.language),
                  label: const Text('查找线上活动与伙伴'),
                ),
              if (_data.options?.items.isEmpty == true)
                const Text('没有可用情境。可以先在设置中的“我的情境”声明；这里不会自动添加。'),
              for (final option in _data.options?.items ?? <NowContextOption>[])
                ListTile(
                  minTileHeight: 48,
                  selected: selected?.same(option) == true,
                  leading: Icon(
                    option.contextType == 'ONLINE'
                        ? Icons.language
                        : option.declared
                        ? Icons.person_outline
                        : Icons.travel_explore,
                  ),
                  title: Text(option.label),
                  subtitle: Text(option.description),
                  trailing: selected?.same(option) == true
                      ? const Icon(Icons.check)
                      : null,
                  onTap: _data.ready ? () => _data.select(option) : null,
                ),
              if (_data.options?.truncated == true)
                const Text('最多展示 100 个当前可用选项，不代表完整历史。'),
              if (selected != null) ...[
                const Divider(),
                Text(
                  '本次查看：${selected.label}',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                Text(selected.description),
                if (selected.contextType == 'INSTITUTION' ||
                    selected.contextType == 'COMMUNITY')
                  const Text('本人声明不证明学历、机构或社群成员资格。'),
                if (selected.queryRoute == 'UNAVAILABLE')
                  const Text('该情境的公开查询尚未开放；本次只选择查看范围，不读取他人私密资料。'),
                FilledButton(
                  onPressed: _data.ready ? _select : null,
                  style: FilledButton.styleFrom(
                    minimumSize: const Size(48, 48),
                  ),
                  child: const Text('核验并切换查看范围'),
                ),
              ],
            ],
            if (_data.message != null) Text(_data.message!),
          ],
        ),
      ),
    );
  }
}
