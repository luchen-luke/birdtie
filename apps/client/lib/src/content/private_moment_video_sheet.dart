import 'dart:async';
import 'package:flutter/material.dart';
import 'package:video_player/video_player.dart';
import '../app/birdtie_app_bar.dart';
import 'private_moment_video_controller.dart';
import 'moment_media_context_view.dart';

class PrivateMomentVideoSheet extends StatefulWidget {
  const PrivateMomentVideoSheet({super.key, required this.controller});
  final PrivateMomentVideoController controller;
  @override
  State<PrivateMomentVideoSheet> createState() =>
      _PrivateMomentVideoSheetState();
}

class _PrivateMomentVideoSheetState extends State<PrivateMomentVideoSheet>
    with WidgetsBindingObserver {
  PrivateMomentVideoController get c => widget.controller;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    unawaited(c.load());
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      unawaited(c.load());
    } else {
      c.suspend();
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    c.suspend();
    super.dispose();
  }

  Future<void> _remove(PrivateMomentVideo v) async {
    final source = c;
    final yes = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('移除这段私人视频？'),
        content: const Text('视频将从这条动态中移除，文字内容保留。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('移除'),
          ),
        ],
      ),
    );
    if (mounted && identical(c, source) && source.current && yes == true)
      await source.remove(v);
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: birdtieAppBar(context, title: const Text('私人视频')),
    body: SafeArea(
      child: AnimatedBuilder(
        animation: c,
        builder: (context, _) => ListView(
          padding: const EdgeInsets.all(20),
          children: [
            if (c.loading || c.busy) const LinearProgressIndicator(),
            if (c.error != null) Text(c.error!),
            if (c.current && !c.loading && c.items.isEmpty && c.error == null)
              const Text('这条动态没有私人视频。'),
            if (c.current) ...[
              for (final v in c.items)
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  leading: const Icon(Icons.play_circle_outline),
                  title: Text('视频 ${v.position + 1}'),
                  subtitle: Text(
                    '${(v.durationMS / 1000).toStringAsFixed(1)} 秒 · 仅自己可见',
                  ),
                  onTap: !c.busy && !c.uncertain
                      ? () => unawaited(c.play(v))
                      : null,
                  trailing: IconButton(
                    tooltip: '移除视频',
                    onPressed: !c.busy && !c.uncertain
                        ? () => unawaited(_remove(v))
                        : null,
                    icon: const Icon(Icons.delete_outline),
                  ),
                ),
              if (c.player != null) ...[
                AspectRatio(
                  aspectRatio: c.player!.value.aspectRatio,
                  child: VideoPlayer(c.player!),
                ),
                ValueListenableBuilder(
                  valueListenable: c.player!,
                  builder: (context, value, _) => IconButton(
                    tooltip: value.isPlaying ? '暂停' : '播放',
                    icon: Icon(
                      value.isPlaying ? Icons.pause : Icons.play_arrow,
                    ),
                    onPressed: () {
                      final p = c.player;
                      if (p == null || !c.current) return;
                      value.isPlaying ? p.pause() : p.play();
                    },
                  ),
                ),
              ],
              TextButton(
                onPressed: !c.busy && !c.loading
                    ? () => unawaited(c.load())
                    : null,
                child: const Text('重新读取'),
              ),
              MomentMediaContextView(
                api: c.mediaContextAPI,
                ownerID: c.ownerID,
                momentID: c.momentID,
                changes: c,
                assetKind: 'VIDEO',
                visibleAssetIDs: () => c.items.map((v) => v.id).toSet(),
              ),
            ],
          ],
        ),
      ),
    ),
  );
}
