import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import '../app/birdtie_app_bar.dart';
import '../workspace/now_ui_tokens.dart';
import 'collection_copy_controller.dart';
import 'personal_collection_controller.dart';

class CollectionCopyPage extends StatefulWidget {
  const CollectionCopyPage({
    super.key,
    required this.controller,
    this.placeNameFor,
    this.onReloadSource,
    this.onOpenPrivateCopy,
  });
  final CollectionCopyController controller;
  final String? Function(String placeID)? placeNameFor;
  final Future<void> Function()? onReloadSource;
  final Future<void> Function(PersonalCollection)? onOpenPrivateCopy;
  @override
  State<CollectionCopyPage> createState() => _CollectionCopyPageState();
}

class _CollectionCopyPageState extends State<CollectionCopyPage> {
  final _title = TextEditingController();
  final _retirement = ValueNotifier<int>(0);
  final _dialogTimers = <Timer>{};
  bool _disposed = false;
  bool _retirementDisposed = false;
  int _dialogs = 0, _retirementEpoch = 0, _targetEpoch = 0;
  void _retire() {
    ++_retirementEpoch;
    void notify() {
      if (!_retirementDisposed) _retirement.value = _retirementEpoch;
    }

    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) => notify());
    } else {
      notify();
    }
  }

  void _disposeRetirementIfUnused() {
    if (_disposed && _dialogs == 0 && !_retirementDisposed) {
      _retirementDisposed = true;
      _retirement.dispose();
    }
  }

  CollectionCopyController get c => widget.controller;
  @override
  void initState() {
    super.initState();
    _title.text = c.title;
    c.addListener(_changed);
    _scheduleRestore();
  }

  void _scheduleRestore() {
    final controller = c;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && identical(c, controller) && controller.current)
        unawaited(controller.restore());
    });
  }

  @override
  void didUpdateWidget(CollectionCopyPage old) {
    super.didUpdateWidget(old);
    if (old.controller != c) {
      old.controller.removeListener(_changed);
      c.addListener(_changed);
      ++_targetEpoch;
      _retire();
      _title.text = c.title;
      _scheduleRestore();
    }
  }

  void _changed() {
    if (!mounted) return;
    _retire();
    if (_title.text != c.title)
      _title.value = TextEditingValue(
        text: c.title,
        selection: TextSelection.collapsed(offset: c.title.length),
      );
    setState(() {});
  }

  Future<void> _prepare() async {
    final controller = c, epoch = c.epoch, target = _targetEpoch;
    final p = await controller.prepare();
    if (p == null ||
        !mounted ||
        c != controller ||
        target != _targetEpoch ||
        epoch != c.epoch ||
        !c.previewCurrent(p))
      return;
    final yes = await _confirm(p);
    if (yes &&
        mounted &&
        c == controller &&
        target == _targetEpoch &&
        epoch == c.epoch &&
        c.previewCurrent(p))
      await c.submit(p);
  }

  Future<bool> _confirm(CollectionCopyPreview p) async {
    final controller = c,
        retired = _retirementEpoch,
        epoch = c.epoch,
        live = ValueNotifier(true);
    bool valid() =>
        live.value &&
        !_disposed &&
        identical(widget.controller, controller) &&
        retired == _retirementEpoch &&
        epoch == controller.epoch &&
        controller.previewCurrent(p);
    late final DialogRoute<bool> route;
    route = DialogRoute<bool>(
      context: context,
      builder: (dialogContext) => ListenableBuilder(
        listenable: Listenable.merge([live, _retirement]),
        builder: (context, _) {
          final enabled = valid();
          return AlertDialog(
            title: Text(enabled ? '确认创建私人副本' : '许可或确认已失效'),
            content: SingleChildScrollView(
              child: enabled
                  ? Column(
                      mainAxisSize: MainAxisSize.min,
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          p.input.title,
                          style: NowUiTokens.of(context).placeTitle,
                        ),
                        Text(
                          '${p.input.orderedPlaceIDs.length} 个公开地点 · 按作者当前公开顺序',
                        ),
                        for (var i = 0; i < p.input.orderedPlaceIDs.length; i++)
                          Text(
                            '${i + 1}. ${widget.placeNameFor?.call(p.input.orderedPlaceIDs[i]) ?? '当前获准的公开地点'}',
                          ),
                        const SizedBox(height: 12),
                        const Text(
                          '创建新的本人私人草稿。只带入地点与顺序；作者的名称、说明、备注和坐标都不复制，不自动公开，也不授权 Agent。',
                        ),
                        Text(
                          '公开版本 ${p.input.publicationRevision} · 复制许可 ${p.input.grantVersion}',
                        ),
                      ],
                    )
                  : const Text('这次确认已失效，确认中的原资料已隐藏。关闭后重新检查，不会执行旧批准。'),
            ),
            actions: [
              TextButton(
                onPressed: () {
                  if (live.value) Navigator.of(dialogContext).pop(false);
                },
                child: const Text('取消'),
              ),
              FilledButton(
                key: const Key('collection-copy-confirm'),
                onPressed: enabled
                    ? () {
                        if (!valid()) return;
                        Navigator.of(dialogContext).pop(true);
                      }
                    : null,
                child: const Text('创建本人私人副本'),
              ),
            ],
          );
        },
      ),
    );
    ++_dialogs;
    final timer = Timer(p.expiresAt.difference(DateTime.now().toUtc()), () {
      if (live.value) live.value = false;
    });
    _dialogTimers.add(timer);
    unawaited(
      route.completed.then((_) {
        timer.cancel();
        _dialogTimers.remove(timer);
        live.dispose();
        --_dialogs;
        _disposeRetirementIfUnused();
      }),
    );
    try {
      final result = await Navigator.of(context).push(route);
      final yes = result == true && valid();
      live.value = false;
      return yes;
    } finally {
      timer.cancel();
      _dialogTimers.remove(timer);
    }
  }

  Future<void> _stop() async {
    final controller = c,
        epoch = c.epoch,
        original = c.pending,
        retired = _retirementEpoch;
    if (!c.current || c.busy || original == null || c.locallyPrepared) return;
    final live = ValueNotifier(true);
    bool valid() =>
        live.value &&
        !_disposed &&
        identical(c, controller) &&
        retired == _retirementEpoch &&
        epoch == controller.epoch &&
        controller.current &&
        identical(controller.pending, original) &&
        !controller.busy;
    final route = DialogRoute<bool>(
      context: context,
      builder: (dialogContext) => ListenableBuilder(
        listenable: Listenable.merge([_retirement, live]),
        builder: (context, _) {
          final enabled = valid();
          return AlertDialog(
            title: const Text('停止并核实原副本操作'),
            content: Text(
              enabled
                  ? '若原副本已创建，会读取实际原回执；否则原操作将永久停止。不会再创建、公开或删除任何副本。'
                  : '原账号或操作已变化，请关闭后重新核实。',
            ),
            actions: [
              TextButton(
                onPressed: () {
                  if (live.value) Navigator.of(dialogContext).pop(false);
                },
                child: const Text('保留待核实状态'),
              ),
              FilledButton(
                key: const Key('collection-copy-confirm-stop'),
                onPressed: enabled
                    ? () {
                        if (!valid()) return;
                        Navigator.of(dialogContext).pop(true);
                      }
                    : null,
                child: const Text('停止并核实'),
              ),
            ],
          );
        },
      ),
    );
    ++_dialogs;
    unawaited(
      route.completed.then((_) {
        live.dispose();
        --_dialogs;
        _disposeRetirementIfUnused();
      }),
    );
    final yes = await Navigator.of(context).push(route);
    final confirmed = yes == true && valid();
    live.value = false;
    if (confirmed) await controller.recover(stop: true);
  }

  @override
  void dispose() {
    _disposed = true;
    for (final timer in _dialogTimers) {
      timer.cancel();
    }
    _dialogTimers.clear();
    c.removeListener(_changed);
    ++_targetEpoch;
    _retire();
    _disposeRetirementIfUnused();
    _title.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final controller = c,
        current = c.current,
        retired = _retirementEpoch,
        epoch = c.epoch,
        g = c.currentGrant,
        pending = c.pending,
        destination = c.destination;
    bool frame() =>
        !_disposed &&
        identical(c, controller) &&
        retired == _retirementEpoch &&
        controller.current &&
        controller.epoch == epoch;
    return Scaffold(
      appBar: birdtieAppBar(context, title: const Text('本人私人副本')),
      body: !current
          ? Center(child: Text(c.error ?? '当前个人来源已变化。'))
          : ListView(
              padding: const EdgeInsets.all(20),
              children: [
                if (c.busy) const LinearProgressIndicator(),
                if (c.notice != null)
                  Text(c.notice!, key: const Key('collection-copy-notice')),
                if (c.error != null)
                  Text(c.error!, key: const Key('collection-copy-error')),
                if (pending != null) ...[
                  const Text('本人原副本操作'),
                  Text(
                    pending.selector.sourceRef.code,
                    style: NowUiTokens.of(context).metadata,
                  ),
                  if (c.locallyPrepared)
                    TextButton(
                      key: const Key('collection-copy-repair'),
                      onPressed: c.busy
                          ? null
                          : () {
                              if (frame() &&
                                  identical(controller.pending, pending))
                                unawaited(controller.repairPrepared());
                            },
                      child: const Text('检查本机准备记录'),
                    )
                  else ...[
                    FilledButton(
                      key: const Key('collection-copy-recover'),
                      onPressed: c.busy
                          ? null
                          : () {
                              if (frame() &&
                                  identical(controller.pending, pending))
                                unawaited(controller.recover());
                            },
                      child: const Text('核实原副本结果'),
                    ),
                    OutlinedButton(
                      key: const Key('collection-copy-stop'),
                      onPressed: c.busy
                          ? null
                          : () {
                              if (frame() &&
                                  identical(controller.pending, pending))
                                unawaited(_stop());
                            },
                      child: const Text('停止并核实原操作'),
                    ),
                  ],
                ] else if (!c.storageReady)
                  TextButton(
                    key: const Key('collection-copy-check-local'),
                    onPressed: c.busy
                        ? null
                        : () {
                            if (frame()) unawaited(c.restore());
                          },
                    child: const Text('检查本人原操作记录'),
                  ),
                if (c.receipt?.status == 'SUCCEEDED') ...[
                  if (destination == null)
                    OutlinedButton(
                      key: const Key('collection-copy-read-destination'),
                      onPressed: c.busy
                          ? null
                          : () {
                              if (frame()) unawaited(c.loadDestination());
                            },
                      child: const Text('读取本人已创建副本'),
                    ),
                  if (destination != null && widget.onOpenPrivateCopy != null)
                    FilledButton(
                      key: const Key('collection-copy-open-destination'),
                      onPressed: c.busy
                          ? null
                          : () {
                              if (frame() &&
                                  identical(
                                    controller.destination,
                                    destination,
                                  ))
                                unawaited(
                                  widget.onOpenPrivateCopy!(destination),
                                );
                            },
                      child: const Text('打开本人副本编辑'),
                    ),
                ],
                if (c.receipt == null && pending == null && g != null) ...[
                  const Text('只复制当前获准的公开地点及顺序。请填写自己的标题；作者正文、备注和坐标不会带入。'),
                  TextField(
                    key: const Key('collection-copy-title'),
                    controller: _title,
                    enabled: !c.busy,
                    decoration: const InputDecoration(labelText: '本人副本标题'),
                    onChanged: c.setTitle,
                  ),
                  TextButton(
                    key: const Key('collection-copy-neutral-title'),
                    onPressed: c.busy
                        ? null
                        : () {
                            if (frame()) c.chooseNeutralTitle();
                          },
                    child: const Text('使用默认副本名称'),
                  ),
                  for (var i = 0; i < g.eligiblePlaceIDs.length; i++)
                    CheckboxListTile(
                      key: Key(
                        'collection-copy-place-${g.eligiblePlaceIDs[i]}',
                      ),
                      contentPadding: EdgeInsets.zero,
                      title: Text(
                        '${i + 1}. ${widget.placeNameFor?.call(g.eligiblePlaceIDs[i]) ?? '当前获准的公开地点'}',
                      ),
                      value: c.selected.contains(g.eligiblePlaceIDs[i]),
                      onChanged: c.busy
                          ? null
                          : (v) {
                              if (frame() && identical(c.currentGrant, g))
                                c.selectPlace(g.eligiblePlaceIDs[i], v == true);
                            },
                    ),
                  if (g.eligiblePlaceIDs.isEmpty)
                    const Text('作者允许复制，但当前没有获准的公开地点可复制。'),
                  FilledButton(
                    key: const Key('collection-copy-preview'),
                    onPressed: c.canPreview
                        ? () {
                            if (frame() && identical(c.currentGrant, g))
                              unawaited(_prepare());
                          }
                        : null,
                    child: const Text('检查私人副本'),
                  ),
                ] else if (c.receipt == null && pending == null)
                  const Text('当前没有可用的复制许可或公开地点。原操作核实不授予新许可。'),
                if (widget.onReloadSource != null)
                  TextButton(
                    key: const Key('collection-copy-read-source'),
                    onPressed: c.busy
                        ? null
                        : () {
                            if (frame()) unawaited(widget.onReloadSource!());
                          },
                    child: const Text('重新读取公开来源'),
                  ),
              ],
            ),
    );
  }
}
