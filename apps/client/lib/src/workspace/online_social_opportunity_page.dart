import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import 'chat_entity_router.dart';
import 'connections.dart';
import 'notification_destination_router.dart';
import 'online_social_opportunity_api.dart';
import 'online_social_opportunity_controller.dart';

class _OnlineBindingSignal extends ChangeNotifier {
  void tick() => notifyListeners();
}

/// User-selected human discovery. No City/GPS, model, Memory or Task mutation.
class OnlineSocialOpportunityPage extends StatefulWidget {
  const OnlineSocialOpportunityPage({
    super.key,
    required this.auth,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.initialIntentID,
    this.onOpenActivity,
  });
  final BirdtieAuthController auth;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final String? initialIntentID;
  final Future<void> Function(BuildContext context, String activityID)?
  onOpenActivity;
  @override
  State<OnlineSocialOpportunityPage> createState() =>
      _OnlineSocialOpportunityPageState();
}

class _OnlineSocialOpportunityPageState
    extends State<OnlineSocialOpportunityPage> {
  OnlineDiscoveryIdentity _identity() => (
    widget.auth.authorizationHeader,
    widget.auth.accountID,
    widget.organizationWorkspaceID?.call(),
  );
  late final OnlineSocialOpportunityController _data =
      OnlineSocialOpportunityController(
        api: OnlineSocialOpportunityAPI(
          client: widget.client,
          apiBaseUrl: widget.apiBaseUrl,
        ),
        identity: _identity,
      );
  final _bindingChanges = _OnlineBindingSignal();
  late final Listenable _changes;
  bool _retired = false, _queued = false;
  Timer? _clock;
  DateTime? _openedLease;
  @override
  void initState() {
    super.initState();
    _changes = Listenable.merge([
      widget.auth,
      if (widget.workspaceChanges != null) widget.workspaceChanges!,
      _bindingChanges,
    ]);
    widget.auth.addListener(_identityChanged);
    widget.workspaceChanges?.addListener(_identityChanged);
    _data.addListener(_changed);
    unawaited(_data.load(initialIntentID: widget.initialIntentID));
    _clock = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted) _data.expire();
    });
  }

  void _changed() {
    if (!mounted) return;
    if (_openedLease != null && !_openedLease!.isAfter(DateTime.now())) {
      _retired = true;
      _bindingChanges.tick();
    }
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

  void _identityChanged() {
    _data.sync();
    if (!_data.current) _bindingChanges.tick();
  }

  bool _valid() =>
      mounted &&
      !_retired &&
      _data.current &&
      (_openedLease == null || _openedLease!.isAfter(DateTime.now()));
  @override
  void didUpdateWidget(OnlineSocialOpportunityPage old) {
    super.didUpdateWidget(old);
    if (old.auth != widget.auth) {
      old.auth.removeListener(_identityChanged);
      widget.auth.addListener(_identityChanged);
    }
    if (old.workspaceChanges != widget.workspaceChanges) {
      old.workspaceChanges?.removeListener(_identityChanged);
      widget.workspaceChanges?.addListener(_identityChanged);
    }
    if (old.auth != widget.auth ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.workspaceChanges != widget.workspaceChanges ||
        old.organizationWorkspaceID != widget.organizationWorkspaceID ||
        old.initialIntentID != widget.initialIntentID ||
        old.onOpenActivity != widget.onOpenActivity) {
      _retired = true;
      _data.dispose();
      _bindingChanges.tick();
    } else {
      _identityChanged();
    }
  }

  Future<void> _open(
    BuildContext inner,
    OnlineSocialOpportunity item, {
    bool chat = false,
    bool community = false,
  }) async {
    final data = _data, epoch = data.epoch;
    final fresh = await data.resolve(item);
    if (!mounted ||
        !inner.mounted ||
        !_valid() ||
        data.epoch != epoch ||
        fresh == null) {
      return;
    }
    final auth = widget.auth,
        client = widget.client,
        base = widget.apiBaseUrl,
        workspace = widget.organizationWorkspaceID;
    bool current() =>
        _valid() &&
        data.epoch == epoch &&
        data.view?.live(DateTime.now()) == true;
    _openedLease = data.view!.validUntil;
    try {
      if (chat) {
        if (fresh.tieID.isEmpty) return;
        final source = ConnectionSource(
          authorizationHeader: () => current() ? data.captured.$1 : null,
          client: client,
          apiBaseUrl: base,
        );
        try {
          final conversation = await source.startFriendChat(fresh.tieID);
          if (!inner.mounted || !current()) return;
          await Navigator.of(inner).push<void>(
            MaterialPageRoute(
              builder: (_) => HumanConversationRoute(
                auth: auth,
                conversation: conversation,
                workspaceChanges: _changes,
                workspaceID: workspace,
                client: client,
                apiBaseUrl: base,
              ),
            ),
          );
        } finally {
          source.dispose();
        }
      } else if (community) {
        if (fresh.communityID.isEmpty) return;
        await openChatEntity(
          inner,
          ChatEntityCard(
            type: 'community',
            id: fresh.communityID,
            available: true,
          ),
          auth: auth,
          workspaceChanges: _changes,
          workspaceID: workspace,
          client: client,
          apiBaseUrl: base,
        );
      } else if (fresh.type == 'ACTIVITY') {
        final callback = widget.onOpenActivity;
        if (callback != null) {
          await callback(inner, fresh.sourceID);
        } else {
          await openChatEntity(
            inner,
            ChatEntityCard(
              type: 'activity',
              id: fresh.sourceID,
              available: true,
            ),
            auth: auth,
            workspaceChanges: _changes,
            workspaceID: workspace,
            client: client,
            apiBaseUrl: base,
          );
        }
      } else {
        await showDialog<void>(
          context: inner,
          useRootNavigator: false,
          builder: (context) => AlertDialog(
            title: Text(fresh.title),
            content: SingleChildScrollView(
              child: Text(
                '${fresh.relationLabel}的当前线上意图。\n这是已表达的需求，不代表对方愿意私信或已参加活动；没有发送消息或邀请。',
              ),
            ),
            actions: [
              TextButton(
                style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
                onPressed: () => Navigator.pop(context),
                child: const Text('关闭'),
              ),
            ],
          ),
        );
      }
    } catch (_) {
      if (inner.mounted && current()) {
        ScaffoldMessenger.of(inner).showSnackBar(
          const SnackBar(content: Text('暂时无法打开。聊天结果未知时不会自动重试或发送消息，请返回消息核实。')),
        );
      }
    } finally {
      if (!_retired) _openedLease = null;
    }
  }

  Widget _body(BuildContext inner) {
    final options = _data.options, view = _data.view;
    return Scaffold(
      appBar: AppBar(title: const Text('线上社交发现')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          const Text('选择自己的线上意图，查看当前可见的好友、社群和公开来源。不需要定位或地图城市。'),
          const SizedBox(height: 12),
          if (_data.busy) const LinearProgressIndicator(),
          if (_data.error != null) Text(_data.error!),
          if (options == null && !_data.busy)
            FilledButton(
              onPressed: () =>
                  _data.load(initialIntentID: widget.initialIntentID),
              child: const Text('重新读取线上意图'),
            ),
          if (options != null && options.intents.isEmpty)
            const Text('尚无有效线上意图。请返回 Now 保存并明确开启线上意图后再来。'),
          if (options != null && options.intents.isNotEmpty)
            DropdownButtonFormField<String>(
              key: const Key('online-opportunity-intent-selector'),
              initialValue: options.intents.any((i) => i.id == _data.selectedID)
                  ? _data.selectedID
                  : null,
              isExpanded: true,
              decoration: const InputDecoration(labelText: '本轮线上意图'),
              items: [
                for (final i in options.intents)
                  DropdownMenuItem(
                    value: i.id,
                    child: Text(
                      i.title,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
              ],
              onChanged: _data.busy
                  ? null
                  : (id) {
                      if (id != null) unawaited(_data.select(id));
                    },
            ),
          if (view != null) ...[
            const SizedBox(height: 16),
            Text(
              view.items.isEmpty
                  ? '没有找到符合当前意图且仍可见的线上来源。私密、已撤回或过期内容不会显示。'
                  : '找到 ${view.items.length} 条当前可见线上来源。这是类别或标题的规则匹配，不代表双方兴趣一致，也没有发送消息。',
            ),
            if (view.truncated) const Text('本次最多展示30条，请修改自己的意图以缩小范围。'),
            for (final item in view.items)
              Card(
                key: ValueKey(item.id),
                child: Padding(
                  padding: const EdgeInsets.all(12),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        item.title,
                        style: Theme.of(inner).textTheme.titleMedium,
                      ),
                      Text(
                        '${item.relationLabel} · ${item.type == 'ACTIVITY' ? '线上活动' : '线上意图'}',
                      ),
                      Wrap(
                        spacing: 8,
                        runSpacing: 8,
                        children: [
                          TextButton(
                            style: TextButton.styleFrom(
                              minimumSize: const Size(48, 48),
                            ),
                            onPressed: _data.busy
                                ? null
                                : () => _open(inner, item),
                            child: Text(
                              item.type == 'ACTIVITY' ? '查看活动' : '查看意图',
                            ),
                          ),
                          if (item.tieID.isNotEmpty)
                            TextButton(
                              style: TextButton.styleFrom(
                                minimumSize: const Size(48, 48),
                              ),
                              onPressed: _data.busy
                                  ? null
                                  : () => _open(inner, item, chat: true),
                              child: const Text('打开好友聊天'),
                            ),
                          if (item.communityID.isNotEmpty)
                            TextButton(
                              style: TextButton.styleFrom(
                                minimumSize: const Size(48, 48),
                              ),
                              onPressed: _data.busy
                                  ? null
                                  : () => _open(inner, item, community: true),
                              child: const Text('查看社群'),
                            ),
                        ],
                      ),
                    ],
                  ),
                ),
              ),
            TextButton(
              style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
              onPressed: _data.busy ? null : () => _data.select(view.intentID),
              child: const Text('刷新当前来源'),
            ),
          ],
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    if (_retired || !_data.current) {
      return Scaffold(
        appBar: AppBar(title: const Text('线上社交发现')),
        body: ListView(
          padding: const EdgeInsets.all(24),
          children: [
            Text(
              widget.organizationWorkspaceID?.call() != null
                  ? '请切回个人身份重新打开线上发现。'
                  : widget.auth.authorizationHeader == null
                  ? '登录后可查看自己的线上机会。'
                  : '账号或连接已变化，请返回 Now 重新打开线上发现。',
            ),
          ],
        ),
      );
    }
    return NotificationDestinationBoundary(
      identityChanges: _changes,
      current: _valid,
      builder: _body,
    );
  }

  @override
  void dispose() {
    _clock?.cancel();
    widget.auth.removeListener(_identityChanged);
    widget.workspaceChanges?.removeListener(_identityChanged);
    _data.removeListener(_changed);
    _data.dispose();
    _bindingChanges.dispose();
    super.dispose();
  }
}
