import 'dart:ui' as ui;
import 'package:flutter/material.dart';
import 'moment_image_header.dart';
import 'private_moment_media_controller.dart';

class PrivateMomentMediaSheet extends StatefulWidget {
  const PrivateMomentMediaSheet({
    super.key,
    required this.controller,
    required this.momentTitle,
  });
  final PrivateMomentMediaController controller;
  final String momentTitle;
  @override
  State<PrivateMomentMediaSheet> createState() =>
      _PrivateMomentMediaSheetState();
}

class _PrivateMomentMediaSheetState extends State<PrivateMomentMediaSheet> {
  bool _masking = false;
  ui.Offset? _start, _end;
  @override
  void initState() {
    super.initState();
    widget.controller.checkLostSelection();
    widget.controller.load();
  }

  @override
  void didUpdateWidget(covariant PrivateMomentMediaSheet old) {
    super.didUpdateWidget(old);
    if (!identical(old.controller, widget.controller)) {
      old.controller.retire();
      widget.controller.checkLostSelection();
      widget.controller.load();
    }
  }

  Future<void> _confirmSave() async {
    final c = widget.controller, v = c.preview;
    if (v == null || !c.canSave) return;
    final answer = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('保存这张私人图片？'),
        content: Text(
          '记录：${widget.momentTitle}\n只对本人开放，最长保留 30 天。\n敏感像素${c.pixelRisk == 'USER_MASKED' ? '只遮挡了你划定的部分' : '尚未检测，可能仍在图片中'}。不公开，不用于 AI 分析，也不加入记忆。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('返回检查'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('确认保存私人图片'),
          ),
        ],
      ),
    );
    if (!mounted ||
        !identical(widget.controller, c) ||
        !identical(c.preview, v)) {
      return;
    }
    await c.saveReviewed(confirmed: answer == true);
  }

  Future<void> _remove(PrivateMomentImageReceipt v) async {
    final c = widget.controller;
    final yes = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('移除私人图片？'),
        content: const Text('图片将无法继续读取。这个操作不会删除文字记录，也不会撤回已经公开的其他内容。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('确认移除'),
          ),
        ],
      ),
    );
    if (!mounted || !identical(widget.controller, c)) return;
    await c.remove(v, confirmed: yes == true);
  }

  Future<void> _confirmTextMask() async {
    final c = widget.controller, hints = c.textHints, bytes = c.localBytes;
    final selected = c.selectedTextHintIndices;
    if (!c.canChangeLocal || selected.isEmpty) return;
    final yes = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('遮挡所选文字区域？'),
        content: const Text(
          '请检查图片中的提示框。将用实色遮挡所选整行区域，可能同时遮挡其他文字。其他敏感内容仍未检测；此操作只修改本机图片，不会保存或上传。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('返回检查'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('确认遮挡所选区域'),
          ),
        ],
      ),
    );
    if (!mounted ||
        !identical(widget.controller, c) ||
        !identical(c.textHints, hints) ||
        !identical(c.localBytes, bytes) ||
        selected.length != c.selectedTextHintIndices.length ||
        !selected.every(c.textHintSelected)) {
      return;
    }
    await c.maskSelectedTextHints(confirmed: yes == true);
  }

  Widget _localPreview(PrivateMomentMediaController c) {
    final b = c.localBytes!, h = MomentImageHeader.inspect(b);
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 560, maxHeight: 320),
        child: AspectRatio(
          aspectRatio: h.width / h.height,
          child: LayoutBuilder(
            builder: (context, box) {
              ui.Offset normalize(ui.Offset p) => ui.Offset(
                (p.dx / box.maxWidth).clamp(0, 1),
                (p.dy / box.maxHeight).clamp(0, 1),
              );
              return GestureDetector(
                key: const ValueKey('private-image-mask'),
                onPanStart: _masking && c.canChangeLocal
                    ? (d) => setState(() {
                        _start = normalize(d.localPosition);
                        _end = _start;
                      })
                    : null,
                onPanUpdate: _masking && c.canChangeLocal
                    ? (d) => setState(() => _end = normalize(d.localPosition))
                    : null,
                onPanEnd: _masking && c.canChangeLocal
                    ? (d) {
                        final a = _start, b = _end;
                        setState(() {
                          _start = null;
                          _end = null;
                        });
                        if (a != null && b != null) {
                          c.mask(ui.Rect.fromPoints(a, b));
                        }
                      }
                    : null,
                child: Stack(
                  fit: StackFit.expand,
                  children: [
                    Image.memory(b, fit: BoxFit.fill, gaplessPlayback: true),
                    for (var i = 0; i < c.textHints.length; i++)
                      Positioned.fromRect(
                        rect: ui.Rect.fromLTRB(
                          c.textHints[i].region.left * box.maxWidth,
                          c.textHints[i].region.top * box.maxHeight,
                          c.textHints[i].region.right * box.maxWidth,
                          c.textHints[i].region.bottom * box.maxHeight,
                        ),
                        child: IgnorePointer(
                          child: DecoratedBox(
                            decoration: BoxDecoration(
                              border: Border.all(
                                width: 2,
                                color: c.textHintSelected(i)
                                    ? Theme.of(context).colorScheme.primary
                                    : Theme.of(context).colorScheme.outline,
                              ),
                            ),
                          ),
                        ),
                      ),
                    if (_start != null && _end != null)
                      Positioned.fromRect(
                        rect: ui.Rect.fromPoints(
                          ui.Offset(
                            _start!.dx * box.maxWidth,
                            _start!.dy * box.maxHeight,
                          ),
                          ui.Offset(
                            _end!.dx * box.maxWidth,
                            _end!.dy * box.maxHeight,
                          ),
                        ),
                        child: const ColoredBox(color: Colors.black),
                      ),
                  ],
                ),
              );
            },
          ),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: const Text('私人图片'),
      leading: IconButton(
        tooltip: '关闭私人图片',
        icon: const Icon(Icons.close),
        onPressed: () => Navigator.pop(context),
      ),
    ),
    body: SafeArea(
      child: AnimatedBuilder(
        animation: widget.controller,
        builder: (context, _) {
          final c = widget.controller;
          return ListView(
            padding: const EdgeInsets.all(20),
            children: [
              Text(
                widget.momentTitle,
                style: Theme.of(context).textTheme.titleLarge,
              ),
              const SizedBox(height: 8),
              const Text('仅管理这条已保存私人记录的图片。选择后先在本机处理；不会自动上传、分析或公开。'),
              if (c.retired || c.requiresReopen) ...[
                const SizedBox(height: 16),
                Text(c.error!, key: const ValueKey('private-image-retired')),
              ] else ...[
                const SizedBox(height: 12),
                if (c.error != null)
                  Text(
                    c.error!,
                    key: const ValueKey('private-image-error'),
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                if (c.message != null)
                  Text(
                    c.message!,
                    key: const ValueKey('private-image-message'),
                  ),
                if (c.busy) const LinearProgressIndicator(),
                if (c.unknown) ...[
                  const Text(
                    '结果尚未核实。关闭后重新打开这条私人记录，可以核实原操作；不会恢复照片或旧确认，也不会自动重复提交。',
                  ),
                  for (final pending in c.pendingOperations)
                    ListTile(
                      selected: c.isSelectedPending(pending),
                      title: Text(switch (pending.phase) {
                        'preview' => '预览结果待核实',
                        'save' => '图片保存结果待核实',
                        _ => '图片移除结果待核实',
                      }),
                      subtitle: c.isSelectedPending(pending)
                          ? const Text('当前核实的操作')
                          : null,
                      trailing: const Icon(Icons.chevron_right),
                      onTap: c.canRead ? () => c.choosePending(pending) : null,
                    ),
                  FilledButton(
                    onPressed: c.canRead ? c.reconcile : null,
                    child: const Text('核实当前操作结果'),
                  ),
                ],
                if (c.recoveryBlocked)
                  TextButton(
                    onPressed: c.canRead ? c.load : null,
                    child: const Text('重试读取本机恢复记录'),
                  ),
                OutlinedButton.icon(
                  onPressed: c.canChangeLocal ? c.select : null,
                  icon: const Icon(Icons.photo_outlined),
                  label: const Text('选择一张图片'),
                ),
                if (c.hasLocalImage) ...[
                  const SizedBox(height: 12),
                  _localPreview(c),
                  const SizedBox(height: 12),
                  OutlinedButton.icon(
                    onPressed: c.canChangeLocal && !c.textChecking
                        ? c.checkLocalText
                        : null,
                    icon: const Icon(Icons.text_fields),
                    label: const Text('检查图片中的文字（本机）'),
                  ),
                  const Text(
                    '仅在本机提示可能的邮箱、电话文字区域；不会分析人脸、发送给 AI 或自动保存。小字或模糊文字可能遗漏，请仍然自行检查。',
                  ),
                  if (c.textChecking) ...[
                    const Text('正在本机检查文字…'),
                    TextButton(
                      onPressed: c.cancelLocalText,
                      child: const Text('停止文字检查'),
                    ),
                  ],
                  if (c.textMessage != null) Text(c.textMessage!),
                  for (var i = 0; i < c.textHints.length; i++)
                    CheckboxListTile(
                      key: ValueKey('private-image-text-hint-$i'),
                      title: Text('提示 ${i + 1}：${c.textHints[i].label}'),
                      subtitle: const Text('仅为可能的文字所在行，请查看图片中的框。'),
                      value: c.textHintSelected(i),
                      onChanged: c.canChangeLocal && !c.textChecking
                          ? (value) => c.selectTextHint(i, value == true)
                          : null,
                    ),
                  if (c.textHints.isNotEmpty)
                    OutlinedButton(
                      onPressed:
                          c.canChangeLocal &&
                              c.hasSelectedTextHints &&
                              !c.textChecking
                          ? _confirmTextMask
                          : null,
                      child: const Text('检查并遮挡所选文字'),
                    ),
                  const Text(
                    '未自动检测人脸、地址等敏感像素。去除图片元数据不等于遮挡图片内容；你可以在图上划定实色遮挡，或不处理并清除图片。',
                  ),
                  SwitchListTile(
                    title: const Text('在图上划定实色遮挡'),
                    subtitle: Text(
                      c.pixelRisk == 'USER_MASKED'
                          ? '已遮挡你划定的部分，其他像素仍未检测。'
                          : '拖动划定区域后才会遮挡。',
                    ),
                    value: _masking,
                    onChanged: c.canChangeLocal
                        ? (v) => setState(() => _masking = v)
                        : null,
                  ),
                  CheckboxListTile(
                    key: const ValueKey('private-image-local-review'),
                    title: const Text('仅在本机处理，我已检查当前图片及未遮挡内容'),
                    value: c.localReviewed,
                    onChanged: c.canChangeLocal
                        ? (v) => c.acknowledgeLocal(v == true)
                        : null,
                  ),
                  TextButton(
                    onPressed: c.canChangeLocal ? c.discard : null,
                    child: const Text('不处理，清除本次图片'),
                  ),
                  if (c.preview == null)
                    FilledButton(
                      onPressed: c.canPreview ? c.prepare : null,
                      child: const Text('生成私人保存预览'),
                    ),
                  if (c.preview?.status == 'preview') ...[
                    Text(
                      '已选图片 ${c.preview!.byteSize} 字节 · 预览有效至 ${c.preview!.previewExpiresAt.toLocal()}\n最长保留 30 天，仅自己可读。',
                    ),
                    FilledButton(
                      onPressed: c.canSave ? _confirmSave : null,
                      child: const Text('检查并保存私人图片'),
                    ),
                  ],
                ],
                const SizedBox(height: 20),
                Text(
                  '已保存的私人图片',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                if (c.loaded && c.images.isEmpty) const Text('当前记录没有可读取的私人图片。'),
                if (!c.loaded && !c.busy)
                  TextButton(
                    onPressed: c.canRead && !c.unknown ? c.load : null,
                    child: const Text('重读私人图片列表'),
                  ),
                for (final v in c.images)
                  ListTile(
                    title: const Text('私人图片'),
                    subtitle: Text('仅自己可读 · 保留至 ${v.retainUntil.toLocal()}'),
                    onTap: !c.canRead || c.unknown ? null : () => c.read(v),
                    trailing: IconButton(
                      tooltip: '移除私人图片',
                      onPressed: !c.canRead || c.unknown
                          ? null
                          : () => _remove(v),
                      icon: const Icon(Icons.delete_outline),
                    ),
                  ),
                if (c.displayedBytes != null)
                  Image.memory(c.displayedBytes!, fit: BoxFit.contain),
                const SizedBox(height: 12),
                const Text(
                  '图片无法用于公开发布或 AI 分析。公开或撤回这条记录会移除其私人图片；普通文字编辑不会自动公开图片。',
                ),
              ],
            ],
          );
        },
      ),
    ),
  );
}
