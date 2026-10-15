import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:video_player/video_player.dart';
import 'moment_media_context_view.dart';
import 'private_moment_media_controller.dart';
import 'private_moment_video_controller.dart';
import 'private_reference_media_view.dart';

/// Existing owner materials stay in the Now panel. Only an explicit click
/// fetches pixels or starts a player; the shared composer handles new material.
class LegacyMomentMaterialsPanel extends StatefulWidget {
  const LegacyMomentMaterialsPanel({
    super.key,
    required this.momentID,
    required this.revision,
    required this.authorizationHeader,
    required this.ownerID,
    required this.sourceCurrent,
    required this.identityChanges,
    required this.client,
    required this.baseUrl,
  });
  final String momentID, baseUrl;
  final int revision;
  final String? Function() authorizationHeader, ownerID;
  final bool Function() sourceCurrent;
  final Listenable identityChanges;
  final http.Client client;
  @override
  State<LegacyMomentMaterialsPanel> createState() =>
      _LegacyMomentMaterialsPanelState();
}

class _LegacyMomentMaterialsPanelState extends State<LegacyMomentMaterialsPanel>
    with WidgetsBindingObserver {
  late PrivateMomentMediaController images;
  late PrivateMomentVideoController videos;
  void _create() {
    images = PrivateMomentMediaController(
      momentID: widget.momentID,
      momentRevision: widget.revision,
      authorizationHeader: widget.authorizationHeader,
      ownerID: widget.ownerID,
      identityChanges: widget.identityChanges,
      sourceCurrent: widget.sourceCurrent,
      apiBaseUrl: widget.baseUrl,
      client: widget.client,
    );
    videos = PrivateMomentVideoController(
      momentID: widget.momentID,
      authorizationHeader: widget.authorizationHeader,
      ownerID: widget.ownerID,
      sourceCurrent: widget.sourceCurrent,
      apiBaseUrl: widget.baseUrl,
      identityChanges: widget.identityChanges,
      client: widget.client,
    );
    unawaited(images.load());
    unawaited(videos.load());
  }

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _create();
  }

  @override
  void didUpdateWidget(covariant LegacyMomentMaterialsPanel old) {
    super.didUpdateWidget(old);
    if (old.momentID != widget.momentID ||
        old.revision != widget.revision ||
        old.identityChanges != widget.identityChanges) {
      images.dispose();
      videos.dispose();
      _create();
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed && widget.sourceCurrent()) {
      images.dispose();
      videos.dispose();
      setState(_create);
    } else {
      images.retire();
      videos.suspend();
    }
  }

  Future<void> _removeImage(PrivateMomentImageReceipt image) async {
    final yes = await _confirm('图片');
    if (!mounted || yes != true || !widget.sourceCurrent()) return;
    await images.remove(image, confirmed: true);
  }

  Future<void> _removeVideo(PrivateMomentVideo video) async {
    final yes = await _confirm('视频');
    if (!mounted || yes != true || !widget.sourceCurrent()) return;
    await videos.remove(video);
  }

  Future<bool?> _confirm(String kind) => showDialog<bool>(
    context: context,
    builder: (dialog) => AlertDialog(
      title: Text('移除这项私人$kind？'),
      content: const Text('移除后无法继续读取，动态文字保留。'),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(dialog, false),
          child: const Text('取消'),
        ),
        FilledButton(
          onPressed: () => Navigator.pop(dialog, true),
          child: const Text('移除'),
        ),
      ],
    ),
  );
  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    images.dispose();
    videos.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: Listenable.merge([images, videos]),
    builder: (context, _) {
      if (!widget.sourceCurrent() || images.retired || !videos.current)
        return const SizedBox.shrink();
      final originalCover = PrivateReferenceMediaView(
        key: ValueKey(
          'moment-original-cover-${widget.momentID}-${widget.revision}',
        ),
        targetType: 'MOMENT',
        targetID: widget.momentID,
        authorizationHeader: widget.authorizationHeader,
        ownerID: widget.ownerID,
        sourceCurrent: widget.sourceCurrent,
        identityChanges: widget.identityChanges,
        apiBaseUrl: widget.baseUrl,
        client: widget.client,
      );
      final count = images.images.length + videos.items.length;
      if (count == 0 &&
          images.loaded &&
          !videos.loading &&
          images.error == null &&
          videos.error == null)
        return originalCover;
      return ExpansionTile(
        tilePadding: EdgeInsets.zero,
        title: Text('原有私人材料${count == 0 ? '' : ' · $count'}'),
        children: [
          originalCover,
          if (images.busy || videos.loading || videos.busy)
            const LinearProgressIndicator(),
          if (images.error != null) Text(images.error!),
          if (videos.error != null) Text(videos.error!),
          for (var i = 0; i < images.images.length; i++)
            ListTile(
              contentPadding: EdgeInsets.zero,
              leading: const Icon(Icons.image_outlined),
              title: Text('图片 ${i + 1}'),
              subtitle: const Text('仅自己可见'),
              onTap: images.canRead && !images.unknown
                  ? () => unawaited(images.read(images.images[i]))
                  : null,
              trailing: IconButton(
                tooltip: '移除图片',
                onPressed: images.canRead && !images.unknown
                    ? () => unawaited(_removeImage(images.images[i]))
                    : null,
                icon: const Icon(Icons.delete_outline),
              ),
            ),
          if (images.displayedBytes != null)
            Image.memory(
              images.displayedBytes!,
              fit: BoxFit.contain,
              cacheWidth: 1536,
            ),
          if (images.images.isNotEmpty)
            MomentMediaContextView(
              api: images.mediaContextAPI,
              ownerID: widget.ownerID,
              momentID: widget.momentID,
              expectedRevision: widget.revision,
              changes: images,
              assetKind: 'IMAGE',
              visibleAssetIDs: () => images.images.map((v) => v.id).toSet(),
            ),
          for (final video in videos.items)
            ListTile(
              contentPadding: EdgeInsets.zero,
              leading: const Icon(Icons.play_circle_outline),
              title: Text('视频 ${video.position + 1}'),
              subtitle: Text(
                '${(video.durationMS / 1000).toStringAsFixed(1)} 秒 · 仅自己可见',
              ),
              onTap: !videos.busy && !videos.uncertain
                  ? () => unawaited(videos.play(video))
                  : null,
              trailing: IconButton(
                tooltip: '移除视频',
                onPressed: !videos.busy && !videos.uncertain
                    ? () => unawaited(_removeVideo(video))
                    : null,
                icon: const Icon(Icons.delete_outline),
              ),
            ),
          if (videos.player != null) ...[
            AspectRatio(
              aspectRatio: videos.player!.value.aspectRatio,
              child: VideoPlayer(videos.player!),
            ),
            ValueListenableBuilder(
              valueListenable: videos.player!,
              builder: (context, value, _) => IconButton(
                tooltip: value.isPlaying ? '暂停' : '播放',
                icon: Icon(value.isPlaying ? Icons.pause : Icons.play_arrow),
                onPressed: () {
                  final p = videos.player;
                  if (p != null && videos.current) {
                    value.isPlaying ? p.pause() : p.play();
                  }
                },
              ),
            ),
          ],
          if (videos.items.isNotEmpty)
            MomentMediaContextView(
              api: videos.mediaContextAPI,
              ownerID: widget.ownerID,
              momentID: widget.momentID,
              expectedRevision: widget.revision,
              changes: videos,
              assetKind: 'VIDEO',
              visibleAssetIDs: () => videos.items.map((v) => v.id).toSet(),
            ),
          TextButton(
            onPressed: images.busy || videos.busy
                ? null
                : () {
                    unawaited(images.load());
                    unawaited(videos.load());
                  },
            child: const Text('刷新原材料'),
          ),
        ],
      );
    },
  );
}
