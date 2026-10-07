import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import 'active_social_intent_api.dart';
import 'active_social_intent_controller.dart';
import 'active_social_intent_page.dart';

/// Native own-intent selector. Layout height is intrinsic, no map movement or
/// Task writes. The caller keeps its canvas mounted when this card changes.
class ActiveSocialIntentCard extends StatefulWidget {
  const ActiveSocialIntentCard({
    super.key,
    required this.auth,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.onClose,
  });
  final BirdtieAuthController auth;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;

  /// Only a modal host supplies its own close action; inline cards never pop.
  final VoidCallback? onClose;
  @override
  State<ActiveSocialIntentCard> createState() => _ActiveSocialIntentCardState();
}

class _ActiveSocialIntentCardState extends State<ActiveSocialIntentCard> {
  ActiveSocialIntentController _create() => ActiveSocialIntentController(
    api: ActiveSocialIntentAPI(
      client: widget.client,
      apiBaseUrl: widget.apiBaseUrl,
    ),
    identity: () => ActiveIntentIdentity(
      widget.auth.authorizationHeader,
      widget.auth.accountID,
      widget.organizationWorkspaceID?.call(),
    ),
  );
  late ActiveSocialIntentController _data = _create();
  Route<void>? _route;
  Timer? _timer;
  NavigatorState? _navigator;
  @override
  void initState() {
    super.initState();
    widget.auth.addListener(_identity);
    widget.workspaceChanges?.addListener(_identity);
    _data.addListener(_changed);
    unawaited(_data.load());
    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted) _changed();
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

  void _closeRoute() {
    final r = _route, nav = _navigator;
    _route = null;
    void remove() {
      if (nav?.mounted == true && r?.isActive == true) nav!.removeRoute(r!);
    }

    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) => remove());
    } else {
      remove();
    }
  }

  void _identity() {
    _data.sync();
    if (!_data.current) _closeRoute();
  }

  @override
  void didUpdateWidget(ActiveSocialIntentCard old) {
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
        old.organizationWorkspaceID != widget.organizationWorkspaceID) {
      _data.removeListener(_changed);
      _data.dispose();
      _closeRoute();
      _data = _create();
      _data.addListener(_changed);
      final created = _data;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && identical(created, _data)) unawaited(created.load());
      });
    } else {
      _identity();
    }
  }

  Future<void> _open() async {
    if (!_data.ready) return;
    final old = _data;
    final auth = widget.auth,
        client = widget.client,
        base = widget.apiBaseUrl,
        changes = widget.workspaceChanges,
        workspace = widget.organizationWorkspaceID,
        id = _data.selectedID;
    final route = MaterialPageRoute<void>(
      builder: (_) => ActiveSocialIntentPage(
        auth: auth,
        client: client,
        apiBaseUrl: base,
        workspaceChanges: changes,
        organizationWorkspaceID: workspace,
        initialIntentID: id,
      ),
    );
    _route = route;
    _navigator = Navigator.of(context);
    await _navigator!.push(route);
    if (identical(_route, route)) _route = null;
    if (mounted && identical(old, _data) && old.current) await old.load();
  }

  @override
  void dispose() {
    _timer?.cancel();
    widget.auth.removeListener(_identity);
    widget.workspaceChanges?.removeListener(_identity);
    _data.removeListener(_changed);
    _data.dispose();
    _closeRoute();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!_data.current) {
      final organization = widget.organizationWorkspaceID?.call() != null;
      final anonymous =
          widget.auth.authorizationHeader == null ||
          widget.auth.accountID == null;
      return Material(
        color: Theme.of(context).colorScheme.surfaceContainerLow,
        borderRadius: BorderRadius.circular(12),
        child: Padding(
          padding: const EdgeInsets.all(12),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('我的社交意图', style: Theme.of(context).textTheme.titleSmall),
              const SizedBox(height: 8),
              Text(
                organization
                    ? '请切回个人身份查看社交意图。'
                    : anonymous
                    ? '登录后可查看和管理自己的社交意图。'
                    : '账号或工作区已变化，请关闭后重新打开意图入口。',
              ),
              if (anonymous && !organization) const Text('请先在个人资料中登录，再重新打开此入口。'),
              if (widget.onClose != null)
                TextButton(
                  style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
                  onPressed: widget.onClose,
                  child: const Text('关闭'),
                ),
            ],
          ),
        ),
      );
    }
    final entries =
        _data.list?.items.where((i) => i.editable(DateTime.now())).toList() ??
        [];
    final selected = _data.selected;
    return Material(
      color: Theme.of(context).colorScheme.surfaceContainerLow,
      borderRadius: BorderRadius.circular(12),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    '当前社交意图',
                    style: Theme.of(context).textTheme.titleSmall,
                  ),
                ),
                IconButton(
                  tooltip: '管理我的意图',
                  onPressed: _data.ready ? _open : null,
                  icon: const Icon(Icons.tune),
                ),
              ],
            ),
            if (_data.busy)
              const LinearProgressIndicator()
            else if (_data.list == null)
              TextButton(
                onPressed: () => _data.load(),
                child: const Text('重试读取意图'),
              )
            else if (entries.isEmpty)
              const Text('尚未开启意图，可在 Now 表达需求。')
            else
              DropdownButton<String>(
                value: entries.any((i) => i.id == _data.selectedID)
                    ? _data.selectedID
                    : null,
                isExpanded: true,
                hint: const Text('选择本轮意图'),
                items: entries
                    .map(
                      (i) => DropdownMenuItem(
                        value: i.id,
                        child: Text(
                          i.title,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                    )
                    .toList(),
                onChanged: _data.ready ? _data.select : null,
              ),
            if (selected != null) ...[
              Text(
                '${intentStates[selected.status]} · ${intentModes[selected.intent['modality']]} · ${selected.audienceLabel}',
              ),
              Text(
                selected.locationLabel,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
              ),
              TextButton(
                onPressed: _data.ready ? _data.reset : null,
                child: const Text('重置本地选择'),
              ),
            ],
            if (_data.message != null) Text(_data.message!),
            if (_data.list?.truncated == true) const Text('仅列出最近 100 项意图。'),
          ],
        ),
      ),
    );
  }
}
