import 'dart:async';
import 'package:flutter/material.dart';
import '../workspace/now_ui_tokens.dart';
import 'video_material_controller.dart';
import 'video_material_picker.dart';

class VideoMaterialPage extends StatefulWidget {
  const VideoMaterialPage({
    super.key,
    required this.controller,
    this.selectOnOpen = false,
  });
  final VideoMaterialController controller;
  final bool selectOnOpen;
  @override
  State<VideoMaterialPage> createState() => _VideoMaterialPageState();
}

class _VideoMaterialPageState extends State<VideoMaterialPage>
    with WidgetsBindingObserver {
  bool _archive = false;
  VideoMaterialController get c => widget.controller;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    unawaited(c.initialize());
    unawaited(c.load());
    if (widget.selectOnOpen)
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && c.canSelect) unawaited(c.select());
      });
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state != AppLifecycleState.resumed) unawaited(c.pausePreview());
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    unawaited(c.pausePreview());
    super.dispose();
  }

  Future<void> _delete(VideoMaterialReceipt v) async {
    final approved = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('删除本人视频？'),
        content: const Text('停止新的读取并清理私人存储。清理完成前会显示当前状态。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('删除'),
          ),
        ],
      ),
    );
    if (approved == true && c.current) await c.delete(v);
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: c,
    builder: (context, _) {
      final tokens = NowUiTokens.of(context);
      return Scaffold(
        appBar: AppBar(
          title: Text('视频', style: tokens.pageTitle),
          actions: [
            IconButton(
              tooltip: _archive ? '返回本机视频' : '本人已保存视频',
              onPressed: c.current && !c.busy
                  ? () => setState(() => _archive = !_archive)
                  : null,
              icon: Icon(
                _archive
                    ? Icons.video_file_outlined
                    : Icons.folder_open_outlined,
              ),
            ),
          ],
        ),
        body: SafeArea(
          child: !c.current
              ? Center(
                  child: Padding(
                    padding: const EdgeInsets.all(24),
                    child: Text(c.error ?? '当前来源已变化，请返回。', style: tokens.body),
                  ),
                )
              : ListView(
                  padding: const EdgeInsets.all(16),
                  children: [
                    if (c.error != null)
                      Padding(
                        padding: const EdgeInsets.only(bottom: 16),
                        child: Semantics(
                          liveRegion: true,
                          child: Text(c.error!, style: tokens.body),
                        ),
                      ),
                    if (_archive) ...[
                      Text('本人私有视频', style: tokens.sheetTitle),
                      if (c.loading) const LinearProgressIndicator(),
                      if (c.items.isEmpty && !c.loading)
                        const Padding(
                          padding: EdgeInsets.symmetric(vertical: 24),
                          child: Text('还没有已保存视频。'),
                        ),
                      for (final v in c.items)
                        ListTile(
                          contentPadding: EdgeInsets.zero,
                          title: Text(
                            '${(v.durationMS / 1000).toStringAsFixed(1)} 秒 · ${(v.byteSize / 1024 / 1024).toStringAsFixed(1)} MiB',
                            style: tokens.placeTitle,
                          ),
                          subtitle: Text(v.statusLabel, style: tokens.metadata),
                          trailing: IconButton(
                            tooltip: '删除本人视频',
                            onPressed:
                                !c.busy &&
                                    v.status != 'deleted' &&
                                    v.status != 'deleting'
                                ? () => unawaited(_delete(v))
                                : null,
                            icon: const Icon(Icons.delete_outline),
                          ),
                          onTap: !c.busy
                              ? () => unawaited(() async {
                                  if (v.status == 'ready_private') {
                                    if (await c.openStoredPreview(v) && mounted)
                                      setState(() => _archive = false);
                                  } else {
                                    await c.recover(receipt: v);
                                  }
                                }())
                              : null,
                        ),
                      Wrap(
                        spacing: 12,
                        children: [
                          TextButton(
                            onPressed: !c.loading
                                ? () => unawaited(c.load())
                                : null,
                            child: const Text('重新读取'),
                          ),
                          if (c.nextCursor != null)
                            TextButton(
                              onPressed: !c.loading
                                  ? () => unawaited(c.load(more: true))
                                  : null,
                              child: const Text('加载更多'),
                            ),
                        ],
                      ),
                    ] else ...[
                      Text(
                        c.selection == null ? '添加视频' : '视频预览',
                        style: tokens.sheetTitle,
                      ),
                      const SizedBox(height: 8),
                      Text(
                        'MP4 · 最多 30 秒、20 MiB、1080p',
                        style: tokens.metadata,
                      ),
                      const SizedBox(height: 4),
                      Text('选择和预览只在本机进行，不会上传或发送。', style: tokens.body),
                      const SizedBox(height: 16),
                      if (c.selection case final selected?) ...[
                        AspectRatio(
                          aspectRatio:
                              selected.preview.width / selected.preview.height,
                          child: ClipRect(child: selected.preview.view()),
                        ),
                        const SizedBox(height: 8),
                        Text(
                          '${(selected.durationMS / 1000).toStringAsFixed(1)} 秒 · ${(selected.bytes.length / 1024 / 1024).toStringAsFixed(1)} MiB · ${selected.preview.width}×${selected.preview.height}',
                          style: tokens.metadata,
                        ),
                        Wrap(
                          spacing: 12,
                          children: [
                            OutlinedButton.icon(
                              onPressed: c.current
                                  ? () => unawaited(c.togglePreview())
                                  : null,
                              icon: Icon(
                                selected.preview.playing
                                    ? Icons.pause
                                    : Icons.play_arrow,
                              ),
                              label: Text(
                                selected.preview.playing ? '暂停' : '播放预览',
                              ),
                            ),
                            TextButton(
                              onPressed: !c.busy && !c.uncertain
                                  ? () => unawaited(c.removeLocal())
                                  : null,
                              child: const Text('移除本机视频'),
                            ),
                          ],
                        ),
                        const SizedBox(height: 16),
                      ],
                      if (c.selecting) const LinearProgressIndicator(),
                      OutlinedButton.icon(
                        key: const Key('video-select'),
                        onPressed: c.canSelect
                            ? () => unawaited(c.select())
                            : null,
                        icon: const Icon(Icons.video_library_outlined),
                        label: Text(c.selection == null ? '选择视频' : '更换视频'),
                      ),
                      OutlinedButton.icon(
                        key: const Key('video-select-file'),
                        onPressed: c.canSelectSource(VideoMaterialSource.file)
                            ? () => unawaited(
                                c.select(source: VideoMaterialSource.file),
                              )
                            : null,
                        icon: const Icon(Icons.folder_open_outlined),
                        label: const Text('从文件选择视频'),
                      ),
                      if (!c.canSelect && !c.busy && !c.uncertain)
                        Text('此设备暂不支持选取视频。可返回原草稿，稍后换设备继续。', style: tokens.body),
                      const SizedBox(height: 20),
                      if (c.capabilities?.available == true) ...[
                        Text('保存后仅本人可见，不会发送或公开。', style: tokens.body),
                      ] else ...[
                        Text('暂时无法保存视频，你仍可在本机选择和预览。', style: tokens.body),
                        TextButton(
                          onPressed: !c.busy
                              ? () => unawaited(c.initialize())
                              : null,
                          child: const Text('重试保存服务'),
                        ),
                      ],
                      const SizedBox(height: 16),
                      if (c.uploading)
                        LinearProgressIndicator(
                          value: c.progress == 1 ? null : c.progress,
                        ),
                      if (c.active != null)
                        Padding(
                          padding: const EdgeInsets.only(bottom: 12),
                          child: Semantics(
                            liveRegion: true,
                            child: Text(c.active!.statusLabel),
                          ),
                        ),
                      Wrap(
                        spacing: 12,
                        runSpacing: 8,
                        children: [
                          FilledButton(
                            key: const Key('video-save'),
                            onPressed: c.canSave
                                ? () => unawaited(c.save())
                                : null,
                            child: Text(c.uncertain ? '按原视频重试' : '保存为本人可见'),
                          ),
                          if (c.active != null || c.uncertain)
                            OutlinedButton(
                              onPressed: !c.busy
                                  ? () => unawaited(c.recover())
                                  : null,
                              child: const Text('核实保存状态'),
                            ),
                          if (c.uploading)
                            TextButton(
                              onPressed: () => unawaited(c.cancelUpload()),
                              child: const Text('取消上传'),
                            ),
                          if (c.active != null &&
                              !c.busy &&
                              c.active!.status != 'deleted' &&
                              c.active!.status != 'deleting')
                            TextButton(
                              onPressed: () => unawaited(_delete(c.active!)),
                              child: const Text('删除当前材料'),
                            ),
                        ],
                      ),
                      ExpansionTile(
                        key: const Key('video-storage-details'),
                        tilePadding: EdgeInsets.zero,
                        title: Text('保存说明', style: tokens.action),
                        childrenPadding: const EdgeInsets.only(bottom: 12),
                        expandedCrossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          Text(
                            '视频可能包含拍摄信息或个人资料。请检查画面，再决定是否上传到私人存储。此处不会发送给 Agent、好友或公开发布。',
                            style: tokens.body,
                          ),
                          if (c.capabilities?.available == true)
                            Text(
                              '存储服务：${c.capabilities!.providerLabel} · 区域：${c.capabilities!.region}',
                              style: tokens.metadata,
                            )
                          else
                            Text(
                              '私人存储服务当前不可用。重试会重新读取真实服务状态，不会上传本机视频。',
                              style: tokens.metadata,
                            ),
                          Text(
                            '使用 H.264 编码的 MP4，文件上限为 20 MiB（20,971,520 字节）。服务端会核验格式、完整解码并进行安全检查，通过后才可读取。',
                            style: tokens.metadata,
                          ),
                        ],
                      ),
                      if (c.pendingOperations.isNotEmpty) ...[
                        const SizedBox(height: 24),
                        Text('找回未确认的视频', style: tokens.placeTitle),
                        for (final p in c.pendingOperations)
                          ListTile(
                            title: Text(
                              '${((p.input['durationMs'] as int) / 1000).toStringAsFixed(1)} 秒的视频',
                            ),
                            subtitle: const Text('只核实原保存结果，不自动重发'),
                            trailing: const Icon(Icons.refresh),
                            onTap: !c.busy
                                ? () => unawaited(c.recover(pending: p))
                                : null,
                          ),
                      ],
                    ],
                  ],
                ),
        ),
      );
    },
  );
}
