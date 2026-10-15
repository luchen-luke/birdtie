import 'dart:async';
import 'package:flutter/material.dart';
import 'entity_cover.dart';
import 'now_ui_tokens.dart';
import 'public_moment_media.dart';
import 'public_moment_media_controller.dart';

/// The parent owns the shared text composer and material pickers. This editor
/// reviews the complete ordered collection and its declared public rights.
class PublicMomentMediaEditor extends StatefulWidget {
  const PublicMomentMediaEditor({
    super.key,
    required this.controller,
    this.embedded = false,
    this.ownsController = false,
    this.onChooseImages,
    this.onChooseVideo,
    this.onResolved,
    this.onMaterialsReady,
  });
  final PublicMomentMediaController controller;
  final bool embedded, ownsController;
  final VoidCallback? onChooseImages, onChooseVideo, onResolved;
  final ValueChanged<List<PublicMomentMediaSelection>>? onMaterialsReady;
  @override
  State<PublicMomentMediaEditor> createState() =>
      _PublicMomentMediaEditorState();
}

class _PublicMomentMediaEditorState extends State<PublicMomentMediaEditor> {
  PublicMomentMediaController get c => widget.controller;
  void _loadIfNeeded() {
    if (!c.journalReady || (!c.materialOnly && !(c.state?.current ?? false))) {
      unawaited(c.load());
    }
  }

  @override
  void initState() {
    super.initState();
    _loadIfNeeded();
  }

  @override
  void didUpdateWidget(PublicMomentMediaEditor old) {
    super.didUpdateWidget(old);
    if (!identical(old.controller, c)) {
      if (old.ownsController) old.controller.dispose();
      _loadIfNeeded();
    }
  }

  Future<void> _declareLicense(int index) async {
    if (!c.canEdit) return;
    final identity = c, operation = c.images[index].operationID;
    final source = TextEditingController();
    final attribution = TextEditingController();
    final declared = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('确认这份材料的授权'),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: source,
                decoration: const InputDecoration(labelText: '授权来源或许可说明'),
                maxLength: 1024,
              ),
              TextField(
                controller: attribution,
                decoration: const InputDecoration(labelText: '署名（如需）'),
                maxLength: 320,
              ),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('我确认拥有公开使用许可'),
          ),
        ],
      ),
    );
    if (declared == true &&
        mounted &&
        identical(identity, c) &&
        c.canEdit &&
        index < c.images.length &&
        c.images[index].operationID == operation &&
        source.text.trim().isNotEmpty) {
      c.setRights(
        index,
        EntityCoverRights(
          basis: 'LICENSED',
          attribution: attribution.text.trim(),
          licenseReference: source.text.trim(),
          retentionUntil: DateTime.now().toUtc().add(const Duration(days: 30)),
          canRetain: true,
          canPublish: true,
        ),
      );
    }
    source.dispose();
    attribution.dispose();
  }

  Future<void> _confirm() async {
    final confirmed = await c.commit();
    if (mounted && confirmed) widget.onResolved?.call();
  }

  Future<void> _prepareForThread() async {
    final controller = c;
    final materials = await controller.prepareMaterials();
    if (mounted &&
        identical(c, controller) &&
        controller.current &&
        materials != null)
      widget.onMaterialsReady?.call(materials);
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: c,
    builder: (context, _) {
      final tokens = NowUiTokens.of(context), preview = c.preview;
      final video = c.images.any(
        (item) => item.mime == 'video/mp4' || item.stage?.isVideo == true,
      );
      final content = Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            video ? '动态视频 · 1 段' : '动态图片 · ${c.images.length}/9',
            style: tokens.chromeLabel,
          ),
          Text('最多 9 张静态图片，或者 1 段 30 秒以内的视频。', style: tokens.metadata),
          if (c.state?.status == 'READY' && c.images.isEmpty)
            PublicMomentMediaGallery(set: c.state!, api: c.api),
          for (var index = 0; index < c.images.length; index++)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 8),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Row(
                    children: [
                      Expanded(
                        child: Text(
                          video ? '视频' : '图片 ${index + 1}',
                          style: tokens.body,
                        ),
                      ),
                      IconButton(
                        tooltip: '将图片 ${index + 1} 前移',
                        onPressed: c.canEdit && index > 0
                            ? () => unawaited(c.reorder(index, index - 1))
                            : null,
                        icon: const Icon(Icons.arrow_upward),
                      ),
                      IconButton(
                        tooltip: '将图片 ${index + 1} 后移',
                        onPressed: c.canEdit && index < c.images.length - 1
                            ? () => unawaited(c.reorder(index, index + 1))
                            : null,
                        icon: const Icon(Icons.arrow_downward),
                      ),
                      IconButton(
                        tooltip: '移除图片 ${index + 1}',
                        onPressed: c.canEdit
                            ? () => unawaited(c.remove(index))
                            : null,
                        icon: const Icon(Icons.close),
                      ),
                    ],
                  ),
                  if (c.images[index].stage != null &&
                      !c.images[index].stage!.current)
                    Text('这份暂存材料已过期，请移除后重新选择。', style: tokens.metadata)
                  else if (preview != null && index < preview.thumbnails.length)
                    Image.memory(
                      preview.thumbnails[index],
                      height: 144,
                      fit: BoxFit.contain,
                      semanticLabel: '将公开的图片 ${index + 1}',
                    )
                  else if (video && c.images[index].poster != null)
                    Image.memory(
                      c.images[index].poster!,
                      height: 144,
                      fit: BoxFit.contain,
                      semanticLabel: '规范视频的实际海报',
                    )
                  else if (video)
                    Text('本机视频材料已选择，准备后会显示规范视频的实际海报。', style: tokens.metadata)
                  else if (c.images[index].bytes != null)
                    Image.memory(
                      c.images[index].bytes!,
                      height: 144,
                      fit: BoxFit.contain,
                      cacheWidth: 320,
                      semanticLabel: '本机选择的图片 ${index + 1}',
                    )
                  else
                    Text('原图片材料已找回；检查全部图片时会显示实际派生图。', style: tokens.metadata),
                  CheckboxListTile(
                    contentPadding: EdgeInsets.zero,
                    title: const Text('这是我拥有使用权的材料'),
                    value: c.images[index].rights?.basis == 'OWNED',
                    onChanged: c.canEdit
                        ? (checked) => c.setRights(
                            index,
                            checked == true
                                ? EntityCoverRights(
                                    basis: 'OWNED',
                                    attribution: '',
                                    licenseReference: '',
                                    retentionUntil: DateTime.now().toUtc().add(
                                      const Duration(days: 30),
                                    ),
                                    canRetain: true,
                                    canPublish: true,
                                  )
                                : null,
                          )
                        : null,
                  ),
                  TextButton(
                    onPressed: c.canEdit
                        ? () => unawaited(_declareLicense(index))
                        : null,
                    child: Text(
                      c.images[index].rights?.basis == 'LICENSED'
                          ? '已声明公开使用许可'
                          : '这份材料已有公开使用许可',
                    ),
                  ),
                ],
              ),
            ),
          if (video && c.images.first.stage?.video != null)
            Text(
              '规范 MP4 · ${(c.images.first.stage!.video!['durationMs'] as int) / 1000} 秒 · '
              '${c.images.first.stage!.width} × ${c.images.first.stage!.height} · H.264 / '
              '${c.images.first.stage!.video!['codecAudio'] == 'none' ? '无音轨' : 'AAC'}。'
              '已完成转码、重新探测和完整解码；未运行病毒扫描，隐私风险未知。',
              style: tokens.metadata,
            ),
          if (widget.onChooseImages != null)
            TextButton(
              onPressed: c.canEdit && !video && c.images.length < 9
                  ? widget.onChooseImages
                  : null,
              child: const Text('添加图片'),
            ),
          if (widget.onChooseVideo != null)
            TextButton(
              onPressed: c.canEdit && c.images.isEmpty
                  ? widget.onChooseVideo
                  : null,
              child: const Text('添加一段视频'),
            ),
          if (c.busy)
            const LinearProgressIndicator(semanticsLabel: '正在处理当前图片集合'),
          if (c.error != null)
            Semantics(
              liveRegion: true,
              child: Text(c.error!, style: tokens.body),
            ),
          if (c.notice != null)
            Semantics(
              liveRegion: true,
              child: Text(c.notice!, style: tokens.body),
            ),
          if (c.hasUnresolvedStagedMaterials && !c.busy)
            TextButton(
              onPressed: c.current ? () => unawaited(c.load()) : null,
              child: const Text('重读原暂存材料'),
            ),
          if (preview != null) ...[
            Text(
              preview.input.action == 'DELETE'
                  ? '将撤回这一版全部公开材料。'
                  : video
                  ? '将公开上面这段规范视频及实际海报，并替换当前公开材料。'
                  : '将按上面的顺序公开全部 ${preview.thumbnails.length} 张规范派生图片，并替换当前公开材料。',
              style: tokens.body,
            ),
            FilledButton(
              onPressed: !c.busy && preview.current
                  ? () => unawaited(_confirm())
                  : null,
              child: Text(
                preview.input.action == 'DELETE' ? '确认撤回全部材料' : '确认公开这版全部材料',
              ),
            ),
          ] else
            FilledButton(
              onPressed: c.canPreview
                  ? () => unawaited(
                      c.materialOnly ? _prepareForThread() : c.prepare(),
                    )
                  : null,
              child: Text(c.materialOnly ? '准备这条材料，随动态一起确认' : '检查全部材料和公开范围'),
            ),
          if (c.state?.canDelete == true && preview == null)
            TextButton(
              onPressed: c.canEdit
                  ? () => unawaited(c.prepare(delete: true))
                  : null,
              child: const Text('撤回全部公开材料'),
            ),
          for (final pending in c.pending)
            Wrap(
              children: [
                TextButton(
                  onPressed: !c.busy
                      ? () => unawaited(c.recover(pending))
                      : null,
                  child: const Text('读取原材料操作结果'),
                ),
                TextButton(
                  onPressed: !c.busy
                      ? () => unawaited(c.recover(pending, stop: true))
                      : null,
                  child: const Text('停止原材料操作'),
                ),
              ],
            ),
          if (c.state == null && !c.busy)
            TextButton(
              onPressed: c.current ? () => unawaited(c.load()) : null,
              child: const Text('重试读取材料资料'),
            ),
        ],
      );
      if (widget.embedded) return content;
      return Scaffold(
        appBar: AppBar(title: const Text('管理动态材料')),
        body: SingleChildScrollView(
          padding: const EdgeInsets.all(16),
          child: content,
        ),
      );
    },
  );
  @override
  void dispose() {
    if (widget.ownsController) c.dispose();
    super.dispose();
  }
}
