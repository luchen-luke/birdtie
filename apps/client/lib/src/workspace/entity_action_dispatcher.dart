import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'entity_action_contract.dart';
import 'entity_action_api.dart';
import 'entity_action_controller.dart';

typedef EntityActionHandler =
    Future<void> Function(EntityActionDescriptor action);
typedef EntityActionReview =
    Future<bool> Function(EntityActionView view, EntityActionDescriptor action);

/// Dispatches only compiled domain callbacks; there is no URL/method/payload
/// executor and no implied Agent permission. Unknown writes are never retried.
class EntityActionDispatcher {
  EntityActionDispatcher({required this.controller, required this.handlers});
  final EntityActionController controller;
  Map<EntityActionKind, EntityActionHandler> handlers;
  void updateHandlers(Map<EntityActionKind, EntityActionHandler> next) {
    handlers = Map.unmodifiable(next);
  }

  bool _busy = false;
  Future<bool> propose(
    EntityActionKind kind,
    EntityActionReview review, {
    Future<bool> Function(EntityActionView, EntityActionDescriptor)?
    prepareReview,
    String? operation,
  }) async {
    if (_busy || !controller.current || !handlers.containsKey(kind)) {
      return false;
    }
    _busy = true;
    final handler = handlers[kind]!;
    try {
      var view = await controller.load();
      if (view == null || !controller.current) return false;
      final originalAction = view.action(kind);
      if (operation != null && !originalAction.operations.contains(operation)) {
        return false;
      }
      final action = operation == null
          ? originalAction
          : originalAction.selectOperation(operation);
      if (!action.available) return false;
      if (prepareReview != null) {
        if (!await prepareReview(view, action) || !controller.current) {
          return false;
        }
        final original = view;
        final next = await controller.load();
        if (next == null ||
            !controller.current ||
            next.sourceVersion != original.sourceVersion ||
            !original.live(DateTime.now())) {
          return false;
        }
        view = next;
      }
      if (action.requiresConfirmation && (!await review(view, action))) {
        return false;
      }
      if (!controller.current) return false;
      final fresh = await controller.recheck(view, kind);
      if (fresh == null || !controller.current) return false;
      if (operation != null && !fresh.operations.contains(operation)) {
        return false;
      }
      await handler(
        (operation == null ? fresh : fresh.selectOperation(operation)).reviewed(
          view,
        ),
      );
      // An exception, timeout or unknown result is not permission to repeat the
      // original write. The original domain's recovery UI owns that outcome.
      return true;
    } finally {
      _busy = false;
    }
  }
}

/// Shared native action contract for direct detail routes. Each supplied
/// callback remains an existing concrete domain flow and is captured per frame.
class EntityActionControls extends StatefulWidget {
  const EntityActionControls({
    super.key,
    required this.ref,
    required this.authorizationHeader,
    required this.handlers,
    this.accountID,
    this.workspaceID,
    this.identityChanges,
    this.apiBaseUrl,
    this.client,
  });
  final EntityActionRef ref;
  final String? Function() authorizationHeader;
  final String? Function()? accountID, workspaceID;
  final Listenable? identityChanges;
  final String? apiBaseUrl;
  final http.Client? client;
  final Map<EntityActionKind, EntityActionHandler> handlers;
  @override
  State<EntityActionControls> createState() => _EntityActionControlsState();
}

class _EntityActionControlsState extends State<EntityActionControls> {
  late EntityActionApi _api;
  late EntityActionController _controller;
  late EntityActionDispatcher _dispatcher;
  bool _operating = false;
  EntityActionIdentity _identity() => (
    widget.authorizationHeader(),
    widget.accountID?.call(),
    widget.workspaceID?.call(),
  );
  void _bind() {
    _operating = false;
    _api = EntityActionApi(
      client: widget.client,
      apiBaseUrl: widget.apiBaseUrl,
    );

    _controller = EntityActionController(
      api: _api,
      ref: widget.ref,
      identity: _identity,
    );
    _dispatcher = EntityActionDispatcher(
      controller: _controller,
      handlers: Map.unmodifiable(widget.handlers),
    );
    widget.identityChanges?.addListener(_changed);
    if (_controller.current) unawaited(_controller.load());
  }

  @override
  void initState() {
    super.initState();
    _bind();
  }

  void _changed() => _controller.sync();
  @override
  void didUpdateWidget(EntityActionControls old) {
    super.didUpdateWidget(old);
    if (old.ref != widget.ref ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.authorizationHeader != widget.authorizationHeader ||
        old.accountID != widget.accountID ||
        old.workspaceID != widget.workspaceID ||
        old.identityChanges != widget.identityChanges) {
      old.identityChanges?.removeListener(_changed);
      _controller.retire();
      _controller.dispose();
      _api.dispose();
      _bind();
    } else {
      _controller.sync();
      _dispatcher.updateHandlers(widget.handlers);
    }
  }

  @override
  void dispose() {
    widget.identityChanges?.removeListener(_changed);
    _controller.dispose();
    _api.dispose();
    super.dispose();
  }

  Future<bool> _review(
    EntityActionController control,
    EntityActionView view,
    EntityActionDescriptor action,
  ) async {
    if (!mounted || !control.current) return false;
    return await showDialog<bool>(
          context: context,
          builder: (dialog) => AnimatedBuilder(
            animation: control,
            builder: (_, _) => AlertDialog(
              title: Text(action.label),
              content: SingleChildScrollView(
                child: Text(
                  control.current && view.live(DateTime.now())
                      ? '目标：${view.title}\n${_consequence(action.kind)}\n${action.reason}'
                      : '身份或来源已变化，请取消后重新打开。',
                ),
              ),
              actions: [
                TextButton(
                  onPressed: () => Navigator.pop(dialog, false),
                  style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
                  child: const Text('取消'),
                ),
                FilledButton(
                  onPressed: control.current && view.live(DateTime.now())
                      ? () => Navigator.pop(dialog, true)
                      : null,
                  style: FilledButton.styleFrom(
                    minimumSize: const Size(48, 48),
                  ),
                  child: Text(_confirmOperation(action)),
                ),
              ],
            ),
          ),
        ) ==
        true;
  }

  Future<void> _run(EntityActionKind kind) async {
    if (_operating) return;
    final control = _controller, dispatcher = _dispatcher;
    setState(() => _operating = true);
    try {
      await dispatcher.propose(kind, (v, a) => _review(control, v, a));
    } catch (_) {
      if (mounted && identical(control, _controller) && control.current) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('操作结果未获确认，请先核实原操作，不要重复提交。')),
        );
      }
    } finally {
      if (mounted && identical(control, _controller)) {
        setState(() => _operating = false);
      }
    }
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: _controller,
    builder: (_, _) {
      _controller.sync();
      if (!_controller.current) return const Text('请使用当前个人身份重新打开操作入口。');
      final view = _controller.view;
      if (view == null || !view.live(DateTime.now())) {
        return TextButton(
          onPressed: _controller.busy ? null : () => _controller.load(),
          style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
          child: Text(
            _controller.busy ? '正在检查可用操作…' : _controller.error ?? '检查可用操作',
          ),
        );
      }
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              for (final action in view.actions)
                if (widget.handlers.containsKey(action.kind))
                  Tooltip(
                    message: action.reason,
                    child: OutlinedButton(
                      key: Key('entity-action-${action.kind.name}'),
                      onPressed:
                          action.available && !_controller.busy && !_operating
                          ? () => _run(action.kind)
                          : null,
                      style: OutlinedButton.styleFrom(
                        minimumSize: const Size(48, 48),
                      ),
                      child: Text(action.label),
                    ),
                  ),
            ],
          ),
          if (_controller.error != null) Text(_controller.error!),
        ],
      );
    },
  );
}

String _consequence(EntityActionKind kind) => switch (kind) {
  EntityActionKind.connect => '下一步检查具体好友申请；不会自动发送消息。',
  EntityActionKind.message => '打开已有好友会话，不自动发送消息。',
  EntityActionKind.share => '下一步选择具体好友并确认原卡片；私密匹配理由不会分享。',
  EntityActionKind.join => '将使用原参加或加入流程，请检查名额、受众和当前身份。',
  EntityActionKind.save => '仅修改自己的收藏，不公开或加入活动。',
  EntityActionKind.navigate => '下一步将打开外部地图，只有已公开的实际位置可用。',
};

String _operationConsequence(EntityActionDescriptor action) =>
    switch (action.operation) {
      'CANCEL_RSVP' => '将取消自己当前的活动报名；不会取消主办方的活动。',
      'LEAVE' => '将退出社群或撤回自己的待审核申请。',
      'ACCEPT_INVITATION' => '将接受这份社群邀请并成为成员。',
      'DECLINE_INVITATION' => '将拒绝当前这份社群邀请，不会加入社群。',
      'REQUEST_JOIN' => '将提交自己的加入申请，仍需管理员审核。',
      'UNSAVE' => '将从自己的收藏移除，不改变参加状态。',
      'REQUEST_CONVERSATION' => '将申请开启这次私信，对方可以接受或拒绝；不会建立持续好友关系，也不自动发送聊天消息。',
      'EXPORT_PUBLIC' => '将打开系统分享面板，展示当前公开内容；由你选择其他应用或收件人，这不表示已经送达。',
      _ => _consequence(action.kind),
    };

final Expando<bool> _activeDetailProposals = Expando<bool>();

String _confirmOperation(EntityActionDescriptor a) => switch (a.operation) {
  'REQUEST_FRIEND' => '发送好友申请',
  'REQUEST_CONVERSATION' => '发送私信申请',
  'OPEN_CHAT' => '打开好友会话',
  'CHOOSE_RECIPIENT' => '选择好友',
  'EXPORT_PUBLIC' => '打开系统分享',
  'JOIN' => a.target.type == 'community' ? '确认加入社群' : '确认报名',
  'CANCEL_RSVP' => '确认取消报名',
  'REQUEST_JOIN' => '提交加入申请',
  'ACCEPT_INVITATION' => '确认接受邀请',
  'DECLINE_INVITATION' => '确认拒绝邀请',
  'LEAVE' => a.label == '撤回申请' ? '确认撤回申请' : '确认退出社群',
  'SAVE' => '确认收藏',
  'UNSAVE' => '确认取消收藏',
  'NAVIGATE' => '打开地图',
  _ => throw const FormatException('未知具体操作'),
};

/// Existing detail buttons and Agent routes use the same native proposal and
/// concrete-version human review. Only an explicitly provided domain callback
/// can run. Transport and identity are captured for the entire interaction.
Future<bool> runEntityAction(
  BuildContext context, {
  required EntityActionRef ref,
  required EntityActionKind kind,
  required String? Function() authorizationHeader,
  required EntityActionHandler handler,
  String? Function()? accountID,
  String? Function()? workspaceID,
  Listenable? identityChanges,
  String? apiBaseUrl,
  http.Client? client,
  bool Function()? domainCurrent,
  Future<bool> Function(EntityActionView, EntityActionDescriptor)?
  prepareReview,
  String Function(EntityActionDescriptor)? reviewDetails,
  String? operation,
}) async {
  if (_activeDetailProposals[context] == true) return false;
  _activeDetailProposals[context] = true;
  final api = EntityActionApi(client: client, apiBaseUrl: apiBaseUrl);
  final control = EntityActionController(
    api: api,
    ref: ref,
    allowPublic:
        kind == EntityActionKind.navigate ||
        (kind == EntityActionKind.share && operation == 'EXPORT_PUBLIC'),
    identity: () =>
        (authorizationHeader(), accountID?.call(), workspaceID?.call()),
  );
  void changed() => control.sync();
  identityChanges?.addListener(changed);
  final dispatcher = EntityActionDispatcher(
    controller: control,
    handlers: {
      kind: (a) async {
        if (!context.mounted ||
            !control.current ||
            domainCurrent?.call() == false) {
          return;
        }
        await handler(a);
      },
    },
  );
  var reviewDismissed = false;
  try {
    final done = await dispatcher.propose(
      kind,
      (view, action) async {
        if (!context.mounted ||
            !control.current ||
            domainCurrent?.call() == false) {
          return false;
        }
        final decision = await showDialog<bool>(
          context: context,
          builder: (dialog) => AnimatedBuilder(
            animation: control,
            builder: (_, _) => AlertDialog(
              title: Text(action.label),
              content: SingleChildScrollView(
                child: Text(
                  control.current && view.live(DateTime.now())
                      ? '目标：${view.title}\n${_operationConsequence(action)}\n${reviewDetails?.call(action) ?? ''}\n${action.reason}'
                      : '身份或来源已变化，请取消后重新打开。',
                ),
              ),
              actions: [
                TextButton(
                  onPressed: () => Navigator.pop(dialog, false),
                  style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
                  child: const Text('取消'),
                ),
                FilledButton(
                  onPressed:
                      control.current &&
                          view.live(DateTime.now()) &&
                          domainCurrent?.call() != false
                      ? () => Navigator.pop(dialog, true)
                      : null,
                  style: FilledButton.styleFrom(
                    minimumSize: const Size(48, 48),
                  ),
                  child: Text(_confirmOperation(action)),
                ),
              ],
            ),
          ),
        );
        reviewDismissed = decision != true;
        return decision == true;
      },
      prepareReview: prepareReview,
      operation: operation,
    );
    if (!done && !reviewDismissed && context.mounted && control.current) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(control.error ?? '当前来源不支持此操作，请检查最新内容。')),
      );
    }
    return done;
  } catch (_) {
    if (context.mounted && control.current) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('结果尚未确认，请核实原操作，不要重复提交。')));
    }
    return false;
  } finally {
    identityChanges?.removeListener(changed);
    control.dispose();
    api.dispose();
    _activeDetailProposals[context] = false;
  }
}
