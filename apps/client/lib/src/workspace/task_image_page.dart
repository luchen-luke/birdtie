import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';

import 'task_image_controller.dart';
import '../content/moment_image_picker.dart';
import '../app/birdtie_app_bar.dart';

class TaskImagePage extends StatefulWidget {
  const TaskImagePage({super.key, required this.controller, this.initialImage});
  final TaskImageController controller;
  final TaskImageReceipt? initialImage;
  @override
  State<TaskImagePage> createState() => _TaskImagePageState();
}

/// The local phase of the same Task image flow. Returning to the composer is
/// not an upload: the original Task image page still owns final saving.
class TaskImageDraftPage extends StatefulWidget {
  const TaskImageDraftPage({
    super.key,
    required this.controller,
    this.selectOnOpen = true,
  });
  final TaskImageDraftController controller;
  final bool selectOnOpen;
  @override
  State<TaskImageDraftPage> createState() => _TaskImageDraftPageState();
}

class _TaskImageDraftPageState extends State<TaskImageDraftPage> {
  @override
  void initState() {
    super.initState();
    if (widget.selectOnOpen) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && widget.controller.localBytes == null) {
          unawaited(widget.controller.select());
        }
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final draft = widget.controller;
    return Scaffold(
      appBar: birdtieAppBar(context, title: const Text('会话图片草稿')),
      body: AnimatedBuilder(
        animation: Listenable.merge([draft, draft.sourceChanges]),
        builder: (context, _) {
          if (!draft.current) {
            return const Center(
              child: Padding(
                padding: EdgeInsets.all(24),
                child: Text('账户或探索范围已变化，请返回后重新选择图片。'),
              ),
            );
          }
          final bytes = draft.localBytes;
          return ListView(
            padding: const EdgeInsets.fromLTRB(20, 12, 20, 32),
            children: [
              Text('仅保留在本机', style: Theme.of(context).textTheme.titleMedium),
              const SizedBox(height: 8),
              const Text(
                'PNG/JPEG，最多 10 MiB。可先遮盖敏感区域；原始 EXIF 不会保留。助手暂不读取图片，也不会据图回答。',
              ),
              const SizedBox(height: 8),
              const Text('返回原输入框后发送文字；问答完成后，再确认是否保存到这条本人消息。关闭草稿不会上传。'),
              if (draft.busy)
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 12),
                  child: LinearProgressIndicator(),
                ),
              if (draft.error != null)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Semantics(liveRegion: true, child: Text(draft.error!)),
                ),
              const SizedBox(height: 16),
              if (bytes != null) ...[
                _TaskImageMaskPreview(
                  bytes: bytes,
                  aspectRatio: draft.localAspectRatio,
                  canEdit: draft.canSelect,
                  onMask: draft.mask,
                ),
                OutlinedButton(
                  onPressed: draft.canSelect ? draft.removeLocal : null,
                  child: const Text('移除本机图片'),
                ),
                CheckboxListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('我已检查图片，返回原输入框继续'),
                  value: draft.reviewed,
                  onChanged: draft.busy
                      ? null
                      : (v) => draft.setReviewed(v ?? false),
                ),
                FilledButton(
                  onPressed: draft.canUse
                      ? () => Navigator.pop(context, true)
                      : null,
                  child: const Text('使用这张图片'),
                ),
              ] else ...[
                FilledButton.icon(
                  onPressed: draft.canSelect ? draft.select : null,
                  style: FilledButton.styleFrom(
                    minimumSize: const Size.fromHeight(48),
                  ),
                  icon: const Icon(Icons.photo_library_outlined),
                  label: const Text('从相册选择图片'),
                ),
                if (draft.supportsSource(MomentImageSource.camera))
                  OutlinedButton.icon(
                    onPressed: draft.canSelectSource(MomentImageSource.camera)
                        ? () => unawaited(
                            draft.select(source: MomentImageSource.camera),
                          )
                        : null,
                    icon: const Icon(Icons.photo_camera_outlined),
                    label: const Text('拍照'),
                  ),
                OutlinedButton.icon(
                  onPressed: draft.canSelectSource(MomentImageSource.file)
                      ? () => unawaited(
                          draft.select(source: MomentImageSource.file),
                        )
                      : null,
                  icon: const Icon(Icons.folder_open_outlined),
                  label: const Text('从文件选择图片'),
                ),
              ],
            ],
          );
        },
      ),
    );
  }
}

class _TaskImageMaskPreview extends StatefulWidget {
  const _TaskImageMaskPreview({
    required this.bytes,
    required this.aspectRatio,
    required this.canEdit,
    required this.onMask,
  });
  final Uint8List bytes;
  final double aspectRatio;
  final bool canEdit;
  final Future<void> Function(Rect) onMask;
  @override
  State<_TaskImageMaskPreview> createState() => _TaskImageMaskPreviewState();
}

class _TaskImageMaskPreviewState extends State<_TaskImageMaskPreview> {
  Rect? _mask;
  Offset? _start;
  @override
  void didUpdateWidget(_TaskImageMaskPreview oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.bytes != widget.bytes || !widget.canEdit) {
      _mask = null;
      _start = null;
    }
  }

  @override
  Widget build(BuildContext context) => Column(
    mainAxisSize: MainAxisSize.min,
    children: [
      LayoutBuilder(
        builder: (context, bounds) {
          if (bounds.maxWidth <= 0) return const SizedBox.shrink();
          final width = bounds.maxWidth
              .clamp(0.0, 300 * widget.aspectRatio)
              .toDouble();
          final height = width / widget.aspectRatio;
          Offset point(Offset local) => Offset(
            (local.dx / width).clamp(0.0, 1.0),
            (local.dy / height).clamp(0.0, 1.0),
          );
          return Center(
            child: SizedBox(
              width: width,
              height: height,
              child: GestureDetector(
                onPanStart: widget.canEdit
                    ? (d) => _start = point(d.localPosition)
                    : null,
                onPanUpdate: widget.canEdit
                    ? (d) {
                        final start = _start;
                        if (start != null)
                          setState(
                            () => _mask = Rect.fromPoints(
                              start,
                              point(d.localPosition),
                            ),
                          );
                      }
                    : null,
                child: Stack(
                  fit: StackFit.expand,
                  children: [
                    Image.memory(widget.bytes, fit: BoxFit.fill),
                    if (_mask != null)
                      Positioned(
                        left: _mask!.left * width,
                        top: _mask!.top * height,
                        width: _mask!.width * width,
                        height: _mask!.height * height,
                        child: DecoratedBox(
                          decoration: BoxDecoration(
                            color: Colors.black.withValues(alpha: .35),
                            border: Border.all(
                              color: Theme.of(context).colorScheme.primary,
                              width: 2,
                            ),
                          ),
                        ),
                      ),
                  ],
                ),
              ),
            ),
          );
        },
      ),
      TextButton(
        onPressed: widget.canEdit && _mask?.isEmpty == false
            ? () async {
                final mask = _mask!;
                setState(() {
                  _mask = null;
                  _start = null;
                });
                await widget.onMask(mask);
              }
            : null,
        child: const Text('遮盖选中区域'),
      ),
    ],
  );
}

class _TaskImagePageState extends State<TaskImagePage> {
  Uint8List? _openedImage;
  TaskImageReceipt? _openedReceipt;
  int _pageSerial = 0;
  int _expiryGeneration = 0;
  Timer? _expiryTimer;
  TaskImageController get images => widget.controller;

  @override
  void initState() {
    super.initState();
    final image = widget.initialImage;
    if (image != null) {
      WidgetsBinding.instance.addPostFrameCallback((_) => _open(image));
    }
  }

  Future<void> _open(TaskImageReceipt image) async {
    final original = images, serial = ++_pageSerial;
    if (!mounted || !original.current || original.busy) return;
    final bytes = await original.readBytes(image);
    if (mounted &&
        identical(images, original) &&
        serial == _pageSerial &&
        original.current &&
        bytes != null) {
      setState(() {
        _openedImage = bytes;
        _openedReceipt = image;
      });
      _scheduleExpiry();
    }
  }

  void _scheduleExpiry() {
    _expiryTimer?.cancel();
    final generation = ++_expiryGeneration;
    final receipt = _openedReceipt, original = images;
    if (receipt == null || !mounted) return;
    final remaining = receipt.retainUntil.difference(original.nowUtc);
    final delay = remaining <= Duration.zero
        ? Duration.zero
        : (remaining > const Duration(days: 1)
              ? const Duration(days: 1)
              : remaining);
    _expiryTimer = Timer(delay, () {
      if (!mounted ||
          generation != _expiryGeneration ||
          !identical(receipt, _openedReceipt) ||
          !identical(original, images))
        return;
      if (!original.isReadable(receipt)) {
        setState(() {
          _openedImage = null;
          _openedReceipt = null;
        });
      } else {
        _scheduleExpiry();
      }
    });
  }

  Future<void> _remove(TaskImageReceipt image) async {
    final original = images, serial = ++_pageSerial;
    if (!mounted || !original.current || original.busy) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('移除这张会话图片？'),
        content: const Text('图片会从本人的会话附件中移除。消息和回答会保留。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('返回'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('移除'),
          ),
        ],
      ),
    );
    if (confirmed != true ||
        !mounted ||
        !identical(images, original) ||
        serial != _pageSerial ||
        !original.current)
      return;
    await original.remove(image);
    if (mounted &&
        identical(images, original) &&
        serial == _pageSerial &&
        original.current &&
        _openedReceipt?.id == image.id) {
      setState(() {
        ++_expiryGeneration;
        _expiryTimer?.cancel();
        _openedImage = null;
        _openedReceipt = null;
      });
    }
  }

  @override
  void didUpdateWidget(TaskImagePage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller != widget.controller ||
        oldWidget.initialImage != widget.initialImage) {
      ++_pageSerial;
      ++_expiryGeneration;
      _expiryTimer?.cancel();
      _openedImage = null;
      _openedReceipt = null;
    }
  }

  @override
  void dispose() {
    ++_pageSerial;
    ++_expiryGeneration;
    _expiryTimer?.cancel();
    _openedImage = null;
    _openedReceipt = null;
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: birdtieAppBar(context, title: const Text('会话图片')),
    body: AnimatedBuilder(
      animation: Listenable.merge([images, images.sourceChanges]),
      builder: (context, _) {
        if (!images.current) {
          ++_expiryGeneration;
          _expiryTimer?.cancel();
          _openedImage = null;
          _openedReceipt = null;
          return const Center(
            child: Padding(
              padding: EdgeInsets.all(24),
              child: Text('会话或账户已变化，请返回后重新打开。'),
            ),
          );
        }
        if (_openedReceipt != null && !images.isReadable(_openedReceipt!)) {
          ++_expiryGeneration;
          _expiryTimer?.cancel();
          _openedReceipt = null;
          _openedImage = null;
        }
        final local = images.localBytes;
        return ListView(
          padding: const EdgeInsets.fromLTRB(20, 12, 20, 32),
          children: [
            Text('仅本人可见', style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 8),
            const Text('图片保存到当前会话中最近一条你的消息。助手暂不读取图片，公开检索和回答不会使用图片内容。'),
            if (images.task.status != 'COMPLETED')
              const Text('当前问答尚未完成，暂时不能新增图片；已保存的图片仍可读取。'),
            if (images.lastUserIndex >= 0) ...[
              const SizedBox(height: 12),
              Text(
                '新图片将保存到：${images.task.messages[images.targetUserIndex].text}',
                maxLines: 3,
                overflow: TextOverflow.ellipsis,
              ),
            ],
            if (images.busy)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 12),
                child: LinearProgressIndicator(),
              ),
            if (images.error != null)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 12),
                child: Semantics(liveRegion: true, child: Text(images.error!)),
              ),
            if (_openedImage != null) ...[
              const SizedBox(height: 16),
              if (_openedReceipt != null)
                Text(
                  '这张图片的消息：${images.task.messages[_openedReceipt!.index].text}',
                  maxLines: 3,
                  overflow: TextOverflow.ellipsis,
                ),
              const SizedBox(height: 8),
              SizedBox(
                height: 300,
                child: InteractiveViewer(
                  child: Image.memory(
                    _openedImage!,
                    fit: BoxFit.contain,
                    errorBuilder: (_, __, ___) => const Text('图片无法显示，请重新读取。'),
                  ),
                ),
              ),
              TextButton(
                onPressed: () => setState(() {
                  ++_expiryGeneration;
                  _expiryTimer?.cancel();
                  _openedImage = null;
                  _openedReceipt = null;
                }),
                child: const Text('收起图片'),
              ),
            ],
            if (local != null) ...[
              const SizedBox(height: 16),
              const Text('本机预览 · 可以先遮盖需要保留在本机的信息'),
              const SizedBox(height: 8),
              _TaskImageMaskPreview(
                bytes: local,
                aspectRatio: images.localAspectRatio,
                canEdit: images.canSelect,
                onMask: images.mask,
              ),
              if (images.canSelect) ...[
                OutlinedButton(
                  onPressed: images.removeLocal,
                  child: const Text('移除本机图片'),
                ),
              ],
              CheckboxListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('我已检查这张图片，同意仅保存到本人会话'),
                value: images.reviewed,
                onChanged: images.busy || images.uncertain
                    ? null
                    : (value) => images.setReviewed(value ?? false),
              ),
              if (images.preview == null && !images.uncertain)
                FilledButton(
                  onPressed: images.canPreview ? images.prepare : null,
                  child: const Text('准备保存'),
                ),
              if (images.preview != null && !images.uncertain)
                FilledButton(
                  onPressed: images.canSave ? images.save : null,
                  child: const Text('保存到这条消息'),
                ),
            ] else if (images.preview == null && !images.uncertain) ...[
              const SizedBox(height: 16),
              FilledButton.icon(
                onPressed: images.canSelect ? images.select : null,
                style: FilledButton.styleFrom(
                  minimumSize: const Size.fromHeight(48),
                ),
                icon: const Icon(Icons.photo_library_outlined),
                label: const Text('从相册选择图片'),
              ),
              if (images.supportsSource(MomentImageSource.camera))
                OutlinedButton.icon(
                  onPressed: images.canSelectSource(MomentImageSource.camera)
                      ? () => unawaited(
                          images.select(source: MomentImageSource.camera),
                        )
                      : null,
                  icon: const Icon(Icons.photo_camera_outlined),
                  label: const Text('拍照'),
                ),
              OutlinedButton.icon(
                onPressed: images.canSelectSource(MomentImageSource.file)
                    ? () => unawaited(
                        images.select(source: MomentImageSource.file),
                      )
                    : null,
                icon: const Icon(Icons.folder_open_outlined),
                label: const Text('从文件选择图片'),
              ),
            ],
            if (images.uncertain) ...[
              const SizedBox(height: 12),
              FilledButton(
                onPressed: images.busy ? null : images.readOperation,
                child: const Text('读取操作状态'),
              ),
            ],
            if (images.pendingOperations.isNotEmpty &&
                !images.uncertain &&
                images.preview == null) ...[
              const SizedBox(height: 16),
              const Text('待核实的图片操作'),
              for (final pending in images.pendingOperations)
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('核实之前的保存状态'),
                  subtitle: const Text('只读取原操作，不会重新上传图片'),
                  onTap: images.busy || local != null
                      ? null
                      : () {
                          images.choosePending(pending);
                          unawaited(images.readOperation());
                        },
                ),
            ],
            if (images.preview != null)
              TextButton(
                onPressed: images.busy ? null : () => _remove(images.preview!),
                child: const Text('移除待保存图片'),
              ),
            const SizedBox(height: 24),
            Text('已保存的会话图片', style: Theme.of(context).textTheme.titleMedium),
            if (images.images.isEmpty)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 12),
                child: Text('尚无已保存图片。'),
              ),
            for (final image in images.images)
              ListTile(
                contentPadding: EdgeInsets.zero,
                leading: const Icon(Icons.image_outlined),
                title: Text('第 ${image.index + 1} 条消息的图片'),
                subtitle: const Text('本人可见 · 点击读取'),
                onTap: images.busy ? null : () => _open(image),
                trailing: IconButton(
                  tooltip: '移除图片',
                  onPressed: images.busy ? null : () => _remove(image),
                  icon: const Icon(Icons.delete_outline),
                ),
              ),
            TextButton(
              onPressed: images.busy ? null : images.load,
              child: const Text('重新读取会话图片'),
            ),
          ],
        );
      },
    ),
  );
}
