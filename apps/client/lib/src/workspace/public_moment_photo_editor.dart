import 'dart:async';
import 'dart:typed_data';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import '../app/birdtie_app_bar.dart';
import '../content/moment_image_picker.dart';
import 'entity_cover.dart' show EntityCoverRights;
import 'now_ui_tokens.dart';
import 'public_moment_photo.dart';
import 'public_moment_photo_controller.dart';
import 'public_moment_photo_pending_store.dart';

class PublicMomentPhotoEditor extends StatefulWidget {
  const PublicMomentPhotoEditor({
    super.key,
    required this.controller,
    this.momentTitle = '',
    this.momentTitleRevision,
    this.picker,
    this.embedded = false,
    this.ownsController = true,
    this.initialImage,
    this.allowImageSelection = true,
    this.recoveryOnly = false,
    this.onPrepared,
    this.onResolved,
  });
  final PublicMomentPhotoController controller;
  final String momentTitle;
  final int? momentTitleRevision;
  final Future<Uint8List?> Function()? picker;
  final bool embedded, ownsController, allowImageSelection, recoveryOnly;
  final Uint8List? initialImage;
  final ValueChanged<PublicMomentPhotoPreview>? onPrepared;
  final void Function(PublicMomentPhotoPending, PublicMomentPhotoReceipt)?
  onResolved;
  @override
  State<PublicMomentPhotoEditor> createState() =>
      _PublicMomentPhotoEditorState();
}

class _PublicMomentPhotoEditorState extends State<PublicMomentPhotoEditor> {
  final _attribution = TextEditingController(),
      _license = TextEditingController(),
      _until = TextEditingController();
  bool _retain = false, _publish = false, _suppress = false;
  String _basis = 'OWNED';
  String? _localError;
  @override
  void initState() {
    super.initState();
    for (final t in [_attribution, _license, _until]) t.addListener(_edited);
    widget.controller.addListener(_observe);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) unawaited(_initialize());
    });
  }

  Future<void> _initialize() async {
    final c = widget.controller, bytes = widget.initialImage;
    await c.load();
    if (!mounted ||
        widget.recoveryOnly ||
        widget.controller != c ||
        !c.canStart ||
        bytes == null)
      return;
    if (widget.momentTitleRevision != null &&
        c.state?.momentRevision != widget.momentTitleRevision) {
      setState(() => _localError = '动态版本已变化，请重新检查当前文字后再公开图片。');
      return;
    }
    c.select(Uint8List.fromList(bytes));
    if (mounted) setState(() => _localError = null);
  }

  void _edited() {
    if (!_suppress && mounted) widget.controller.draftChanged();
  }

  void _clearRights() {
    _suppress = true;
    _attribution.clear();
    _license.clear();
    _until.clear();
    _retain = false;
    _publish = false;
    _basis = 'OWNED';
    _suppress = false;
  }

  void _observe() {
    if (!mounted) return;
    if (!widget.controller.current || widget.controller.sourceUnavailable)
      _clearRights();
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  @override
  void didUpdateWidget(PublicMomentPhotoEditor old) {
    super.didUpdateWidget(old);
    if (old.controller != widget.controller) {
      old.controller.removeListener(_observe);
      if (old.ownsController) old.controller.dispose();
      _clearRights();
      widget.controller.addListener(_observe);
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) unawaited(_initialize());
      });
    }
  }

  Future<void> _pick() async {
    if (widget.recoveryOnly) return;
    final c = widget.controller, valid = c.capture();
    if (!c.canStart || c.state?.canSave != true) return;
    try {
      final bytes =
          await (widget.picker?.call() ??
              MomentImagePicker().select().then((v) => v?.bytes));
      if (!mounted ||
          widget.recoveryOnly ||
          widget.controller != c ||
          !valid() ||
          bytes == null)
        return;
      c.select(bytes);
      setState(() => _localError = null);
    } catch (_) {
      if (mounted && valid())
        setState(() => _localError = '本机选图未完成，请重新选择静态JPEG或PNG。');
    }
  }

  EntityCoverRights? _rights() {
    final date = DateTime.tryParse(_until.text.trim());
    if (date == null) return null;
    final rights = EntityCoverRights(
      basis: _basis,
      attribution: _attribution.text.trim(),
      licenseReference: _license.text.trim(),
      retentionUntil: date.toUtc(),
      canRetain: _retain,
      canPublish: _publish,
    );
    return rights.valid ? rights : null;
  }

  Future<void> _prepare(String action) async {
    if (!mounted || widget.recoveryOnly) return;
    final c = widget.controller;
    if (action == 'SAVE' &&
        widget.initialImage != null &&
        c.state?.momentRevision != widget.momentTitleRevision) {
      setState(() => _localError = '动态版本已变化，请重新检查当前文字后再公开图片。');
      return;
    }
    final rights = action == 'SAVE' ? _rights() : null;
    if (action == 'SAVE' && rights == null) {
      setState(() => _localError = '请检查图片权利、保留期限，并明确允许保留和公开。');
      return;
    }
    final p = await c.prepare(action: action, rights: rights);
    if (!mounted || widget.recoveryOnly || widget.controller != c || p == null)
      return;
    widget.onPrepared?.call(p);
    final epoch = c.epoch;
    var open = true, painted = action == 'DELETE';
    final frame = ValueNotifier<bool>(painted);
    bool current() =>
        open &&
        mounted &&
        !widget.recoveryOnly &&
        widget.controller == c &&
        c.previewCurrent(p, epoch);
    final timer = Timer.periodic(const Duration(milliseconds: 100), (timer) {
      if (!current()) {
        timer.cancel();
        c.observe();
      }
    });
    c.retainDialog(timer);
    final route = DialogRoute<bool>(
      context: context,
      builder: (d) => ListenableBuilder(
        listenable: Listenable.merge([c, frame]),
        builder: (d, _) {
          final live = current();
          return AlertDialog(
            title: Text(action == 'SAVE' ? '检查公开图片' : '检查移除公开图片'),
            content: SingleChildScrollView(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (live) ...[
                    if (action == 'SAVE')
                      Image.memory(
                        p.derivative!,
                        width: 320,
                        height: 180,
                        fit: BoxFit.cover,
                        gaplessPlayback: false,
                        frameBuilder: (context, child, number, synchronous) {
                          if (number != null && !frame.value) {
                            WidgetsBinding.instance.addPostFrameCallback((_) {
                              if (current() && !frame.value) {
                                painted = true;
                                frame.value = true;
                              }
                            });
                          }
                          return child;
                        },
                        errorBuilder: (_, __, ___) =>
                            const Text('这张规范图片无法显示，不能确认公开。'),
                      ),
                    if (action == 'SAVE') ...[
                      const SizedBox(height: 12),
                      const Text(
                        '这张规范图片将公开出现在此动态详情，并作为同一地图上动态的方形图片。不会公开其他私人图片或修改正文、地点和坐标。',
                      ),
                      Text(
                        '图片署名：${rights!.attribution.isEmpty ? '未填写' : rights.attribution}',
                      ),
                      Text(
                        '保留到：${rights.retentionUntil.toLocal().toString().split(' ').first}',
                      ),
                      Text('权利：${rights.basis == 'OWNED' ? '本人拥有' : '已获许可'}'),
                      if (rights.licenseReference.isNotEmpty)
                        Text('许可来源：${rights.licenseReference}'),
                    ] else
                      const Text(
                        '当前公开图片将无法继续读取，地图图片也会退休。这个操作不撤回动态正文，不改变地点或坐标，已被他人看到的内容无法收回。',
                      ),
                  ] else
                    const Text(
                      '这次批准已到期或来源已变化。确认中的图片和权利已隐藏，不能执行旧批准。本人尚未提交的材料可继续编辑。',
                    ),
                ],
              ),
            ),
            actions: [
              TextButton(
                onPressed: () {
                  open = false;
                  Navigator.pop(d, false);
                },
                child: const Text('取消'),
              ),
              FilledButton(
                onPressed: live && frame.value
                    ? () {
                        if (!current() || !frame.value) return;
                        open = false;
                        Navigator.pop(d, true);
                      }
                    : null,
                child: Text(action == 'SAVE' ? '确认公开图片' : '确认移除图片'),
              ),
            ],
          );
        },
      ),
    );
    route.completed.whenComplete(() {
      frame.dispose();
      c.releaseDialog(timer);
    });
    final approved = await Navigator.of(
      context,
      rootNavigator: true,
    ).push(route);
    open = false;
    if (!mounted || widget.controller != c) return;
    if (approved == true && c.previewCurrent(p, epoch)) {
      await c.commit(p, epoch, painted: painted);
      _resolved(c, PublicMomentPhotoPending.preview(p));
    } else
      c.cancel(p);
  }

  void _resolved(PublicMomentPhotoController c, PublicMomentPhotoPending p) {
    final r = c.receipt;
    if (mounted &&
        widget.controller == c &&
        c.current &&
        r != null &&
        r.operationID == p.operationID)
      widget.onResolved?.call(p, r);
  }

  Future<void> _recover(PublicMomentPhotoPending p, {bool stop = false}) async {
    final c = widget.controller;
    await c.recover(p, stop: stop);
    _resolved(c, p);
  }

  Future<void> _stop(PublicMomentPhotoPending p) async {
    final c = widget.controller, valid = c.capture();
    final yes = await showDialog<bool>(
      context: context,
      builder: (d) => AlertDialog(
        title: const Text('停止原图片操作'),
        content: const Text('只停止这个原操作。若已经成功公开或移除，将保留原结果，不会撤销。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(d, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () {
              if (valid()) Navigator.pop(d, true);
            },
            child: const Text('停止原操作'),
          ),
        ],
      ),
    );
    if (mounted && widget.controller == c && yes == true && valid())
      await _recover(p, stop: true);
  }

  @override
  void dispose() {
    widget.controller.removeListener(_observe);
    // This editor owns its concrete selection and reader lifetime, including
    // Navigator teardown where a push future may never complete. The notifier
    // itself stays alive until every owned DialogRoute has actually unmounted.
    if (widget.ownsController) widget.controller.dispose();
    for (final t in [_attribution, _license, _until]) {
      t.removeListener(_edited);
      t.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final c = widget.controller, tokens = NowUiTokens.of(context);
    final ownCurrent = c.current;
    final body = ListView(
      shrinkWrap: widget.embedded,
      primary: false,
      physics: widget.embedded ? const NeverScrollableScrollPhysics() : null,
      padding: EdgeInsets.fromLTRB(
        16,
        16,
        16,
        widget.embedded ? 16 : 20 + MediaQuery.viewInsetsOf(context).bottom,
      ),
      children: [
        if (!ownCurrent) Text(c.error ?? '个人身份或来源已变化，请返回当前入口。'),
        if (ownCurrent) ...[
          if (c.busy) const LinearProgressIndicator(),
          if (c.state?.current == true) ...[
            if (c.state?.canSave == true &&
                c.state?.momentRevision == widget.momentTitleRevision &&
                widget.momentTitle.isNotEmpty)
              Text(widget.momentTitle, style: tokens.sheetTitle),
            PublicMomentPhoto(
              metadata: c.state!.photo,
              api: c.api,
              current: () => c.current && c.state?.current == true,
              identityChanges: c,
            ),
          ],
          if (c.notice != null) Text(c.notice!),
          if (c.error != null) Text(c.error!),
          if (_localError != null) Text(_localError!),
          TextButton(
            onPressed: c.busy
                ? null
                : () => widget.initialImage != null && c.localBytes == null
                      ? _initialize()
                      : c.load(),
            child: const Text('重新读取当前状态'),
          ),
          if (c.recoveryOperations.isNotEmpty) ...[
            Text('待核实的原公开图片操作', style: tokens.placeTitle),
            for (final p in c.recoveryOperations)
              Wrap(
                spacing: 8,
                children: [
                  TextButton(
                    onPressed: c.busy ? null : () => _recover(p),
                    child: Text(c.locallyPrepared(p) ? '检查本机准备记录' : '核实原结果'),
                  ),
                  if (!c.locallyPrepared(p))
                    TextButton(
                      onPressed: c.busy ? null : () => _stop(p),
                      child: const Text('停止原操作'),
                    ),
                ],
              ),
          ],
          if (widget.recoveryOnly) const Text('这里只核实原图片操作；修改图片请返回 Now。'),
          if (!widget.recoveryOnly && c.state?.canSave == true) ...[
            Text(
              widget.allowImageSelection
                  ? '请本人重新选择一张本机图片。私人动态附件不会自动用于公开。'
                  : '本机选中的图片仍未公开，请检查图片权利、保留期限和公开后的图片。',
            ),
            if (widget.allowImageSelection)
              OutlinedButton.icon(
                key: const Key('public-photo-select'),
                onPressed: c.canStart ? _pick : null,
                icon: const Icon(Icons.add_photo_alternate_outlined),
                label: const Text('重新选择本机图片'),
              ),
            if (c.localBytes != null) ...[
              Image.memory(
                c.localBytes!,
                height: 180,
                fit: BoxFit.contain,
                errorBuilder: (_, __, ___) => const Text('本机图片无法显示，请重新选择。'),
              ),
              DropdownButtonFormField<String>(
                initialValue: _basis,
                items: const [
                  DropdownMenuItem(value: 'OWNED', child: Text('本人拥有图片')),
                  DropdownMenuItem(value: 'LICENSED', child: Text('已获图片许可')),
                ],
                onChanged: c.busy
                    ? null
                    : (v) {
                        setState(() => _basis = v!);
                        c.draftChanged();
                      },
                decoration: const InputDecoration(labelText: '图片权利'),
              ),
              TextField(
                key: const Key('public-photo-attribution'),
                controller: _attribution,
                enabled: !c.busy,
                decoration: const InputDecoration(labelText: '图片署名'),
              ),
              if (_basis == 'LICENSED')
                TextField(
                  controller: _license,
                  enabled: !c.busy,
                  decoration: const InputDecoration(labelText: '许可来源'),
                ),
              TextField(
                key: const Key('public-photo-retention'),
                controller: _until,
                enabled: !c.busy,
                decoration: const InputDecoration(
                  labelText: '允许保留到（YYYY-MM-DD）',
                ),
              ),
              CheckboxListTile(
                key: const Key('public-photo-retain'),
                value: _retain,
                onChanged: c.busy
                    ? null
                    : (v) {
                        setState(() => _retain = v ?? false);
                        c.draftChanged();
                      },
                title: const Text('我有权让Birdtie保留这张图片'),
              ),
              CheckboxListTile(
                key: const Key('public-photo-publish'),
                value: _publish,
                onChanged: c.busy
                    ? null
                    : (v) {
                        setState(() => _publish = v ?? false);
                        c.draftChanged();
                      },
                title: const Text('我确认允许公开这张图片'),
              ),
              FilledButton(
                key: const Key('public-photo-preview'),
                onPressed: c.canStart ? () => _prepare('SAVE') : null,
                child: const Text('检查公开图片'),
              ),
            ],
          ],
          if (!widget.recoveryOnly && c.state?.canDelete == true)
            TextButton(
              onPressed: c.canStart ? () => _prepare('DELETE') : null,
              child: const Text('检查移除当前公开图片'),
            ),
        ],
      ],
    );
    if (widget.embedded) return body;
    return Scaffold(
      appBar: birdtieAppBar(context, title: const Text('公开动态图片')),
      body: SafeArea(child: body),
    );
  }
}
