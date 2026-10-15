import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';

import 'connections.dart';
import 'friend_audio.dart';
import 'friend_image.dart';
import 'friend_conversation_controller.dart';

/// Reusable message content only. The host owns the single visible composer.
class FriendConversationThread extends StatelessWidget {
  const FriendConversationThread({
    super.key,
    required this.controller,
    this.onOpenEntity,
    this.showRecipient = true,
    this.imageCaption,
    this.onImageOperation,
  });
  final FriendConversationController controller;
  final ValueChanged<ChatEntityCard>? onOpenEntity;
  final bool showRecipient;
  final String Function()? imageCaption;
  final Future<bool> Function(PendingFriendImage pending, bool retry)?
  onImageOperation;
  String _duration(int millis) {
    final sec = (millis / 1000).ceil();
    return '${sec ~/ 60}:${(sec % 60).toString().padLeft(2, '0')}';
  }

  Widget _button({
    required String tooltip,
    required IconData icon,
    required VoidCallback? action,
  }) => IconButton(
    tooltip: tooltip,
    onPressed: action,
    constraints: const BoxConstraints(minWidth: 48, minHeight: 48),
    icon: Icon(icon, size: 20),
  );
  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: controller,
    builder: (context, _) {
      final c = controller, theme = Theme.of(context);
      if (!c.current)
        return const Padding(
          padding: EdgeInsets.all(16),
          child: Text('登录后继续这段对话。'),
        );
      return NotificationListener<ScrollNotification>(
        onNotification: (_) {
          c.imageViewportChanged();
          return false;
        },
        child: ListView(
          key: PageStorageKey(controller),
          shrinkWrap: true,
          padding: const EdgeInsets.fromLTRB(12, 8, 12, 12),
          children: [
            if (showRecipient)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 8),
                child: Text(
                  c.conversation.personalGroup
                      ? c.conversation.otherName
                      : '发给 ${c.conversation.otherName}',
                  style: theme.textTheme.titleMedium,
                ),
              ),
            if (c.readOnlyHistory)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 8),
                child: Text('原聊天记录 · 当前仅可阅读', style: theme.textTheme.bodySmall),
              ),
            if (c.loading) const LinearProgressIndicator(minHeight: 2),
            if (c.hasEarlierMessages)
              TextButton.icon(
                key: const Key('friend-load-earlier'),
                onPressed: c.loading || c.loadingEarlier || c.sending
                    ? null
                    : c.loadEarlier,
                icon: c.loadingEarlier
                    ? const SizedBox(
                        width: 16,
                        height: 16,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.history, size: 18),
                label: Text(c.loadingEarlier ? '正在读取更早消息…' : '查看更早消息'),
              ),
            if (c.error != null)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 8),
                child: Text(c.error!, style: theme.textTheme.bodySmall),
              ),
            if (c.historyAvailable &&
                c.messages.isEmpty &&
                !c.loading &&
                c.error == null)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 16),
                child: Text('还没有消息。', style: theme.textTheme.bodySmall),
              ),
            for (final m in c.messages)
              if (m.system || m.unavailable)
                Padding(
                  padding: const EdgeInsets.symmetric(
                    vertical: 8,
                    horizontal: 16,
                  ),
                  child: Center(
                    child: Text(
                      m.unavailable ? '这条消息已不可查看' : m.body,
                      textAlign: TextAlign.center,
                      style: theme.textTheme.bodySmall,
                    ),
                  ),
                )
              else
                Align(
                  alignment: !c.sentByMe(m)
                      ? Alignment.centerLeft
                      : Alignment.centerRight,
                  child: Container(
                    constraints: const BoxConstraints(maxWidth: 340),
                    margin: const EdgeInsets.symmetric(vertical: 4),
                    padding: const EdgeInsets.symmetric(
                      horizontal: 12,
                      vertical: 8,
                    ),
                    decoration: BoxDecoration(
                      color: !c.sentByMe(m)
                          ? theme.colorScheme.surfaceContainerLow
                          : theme.colorScheme.primaryContainer.withValues(
                              alpha: .65,
                            ),
                      borderRadius: BorderRadius.circular(16),
                    ),
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        if (c.conversation.personalGroup && !c.sentByMe(m))
                          Padding(
                            padding: const EdgeInsets.only(bottom: 4),
                            child: Text(
                              'Birdtie 成员',
                              style: theme.textTheme.labelSmall,
                            ),
                          ),
                        if (m.kind == 'STICKER')
                          const Icon(Icons.emoji_emotions_outlined, size: 22),
                        if (m.image != null)
                          _FriendImageMessage(
                            key: ValueKey((m.id, c.imageEpoch)),
                            controller: c,
                            message: m,
                          ),
                        if (m.audio case final a?)
                          Row(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              _button(
                                tooltip: '播放语音消息',
                                icon: c.playingMessageID == m.id
                                    ? Icons.volume_up_outlined
                                    : Icons.play_arrow_rounded,
                                action: c.audioActive
                                    ? null
                                    : () => c.playMessage(m),
                              ),
                              Text(
                                _duration(a.durationMillis),
                                style: theme.textTheme.bodyMedium,
                              ),
                            ],
                          ),
                        if (m.entity case final e?)
                          TextButton(
                            onPressed: e.available && onOpenEntity != null
                                ? () => onOpenEntity!(e)
                                : null,
                            child: Text(
                              e.available
                                  ? e.title ?? e.typeLabel
                                  : '${e.typeLabel}已不可查看',
                            ),
                          ),
                        if (m.body.isNotEmpty &&
                            m.audio == null &&
                            (m.entity == null || m.body != '分享了一张卡片') &&
                            (m.image == null || m.body != '图片消息'))
                          Text(m.body, style: theme.textTheme.bodyMedium),
                      ],
                    ),
                  ),
                ),
            for (final p in c.textPending)
              TextButton.icon(
                onPressed: c.sending ? null : () => c.recoverText(p),
                icon: const Icon(Icons.sync, size: 18),
                label: const Text('核实这次发送'),
              ),
            if (c.imagePending.isNotEmpty &&
                c.imageDraft.localBytes == null &&
                (c.canSendText || c.canRecord || c.canSendAudio))
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 8),
                child: Text(
                  '图片结果仍待核实，可以继续发送文字或语音。',
                  style: theme.textTheme.bodySmall,
                ),
              ),
            for (final p in c.imagePending)
              Wrap(
                children: [
                  TextButton.icon(
                    onPressed: c.sending || c.audioActive
                        ? null
                        : () => onImageOperation != null
                              ? onImageOperation!(p, false)
                              : c.recoverImage(p),
                    icon: const Icon(Icons.sync, size: 18),
                    label: const Text('核实这次图片'),
                  ),
                  if (onImageOperation != null &&
                      c.canRetryImage(p, imageCaption?.call() ?? ''))
                    TextButton(
                      onPressed: c.sending
                          ? null
                          : () => onImageOperation!(p, true),
                      child: const Text('重试同一次图片'),
                    ),
                ],
              ),
            for (final p in c.audioPending)
              Wrap(
                crossAxisAlignment: WrapCrossAlignment.center,
                children: [
                  TextButton.icon(
                    onPressed: c.sending ? null : () => c.recoverAudio(p),
                    icon: const Icon(Icons.sync, size: 18),
                    label: const Text('核实这次语音'),
                  ),
                  if (c.audioDraft?.metadata.same(p.metadata) == true)
                    TextButton(
                      onPressed: c.sending ? null : () => c.retryAudio(p),
                      child: const Text('重试同一次'),
                    ),
                ],
              ),
            if (c.audioDraft case final d?)
              Container(
                key: const Key('friend-audio-preview'),
                margin: const EdgeInsets.only(top: 12),
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                decoration: BoxDecoration(
                  color: theme.colorScheme.surfaceContainer,
                  borderRadius: BorderRadius.circular(16),
                ),
                child: Row(
                  children: [
                    _button(
                      tooltip: '试听语音',
                      icon: Icons.play_arrow_rounded,
                      action: c.sending || c.audioActive ? null : c.playDraft,
                    ),
                    Expanded(
                      child: Text(
                        '语音 · ${_duration(d.durationMillis)}',
                        style: theme.textTheme.bodyMedium,
                      ),
                    ),
                    _button(
                      tooltip: '取消这段语音',
                      icon: Icons.close_rounded,
                      action: c.sending ? null : c.cancelAudio,
                    ),
                  ],
                ),
              ),
            if (c.audioActive)
              Row(
                children: [
                  Expanded(
                    child: Text(switch (c.audioPhase) {
                      FriendAudioPhase.permission => '等待麦克风授权',
                      FriendAudioPhase.processing => '正在整理录音',
                      _ => '录音 ${_duration(c.elapsed.inMilliseconds)}',
                    }, style: theme.textTheme.bodyMedium),
                  ),
                  if (c.recording)
                    _button(
                      tooltip: '停止录音',
                      icon: Icons.stop_rounded,
                      action: c.stopRecording,
                    ),
                  _button(
                    tooltip: '取消录音',
                    icon: Icons.close_rounded,
                    action: c.cancelAudio,
                  ),
                ],
              ),
          ],
        ),
      );
    },
  );
}

/// Only a currently intersecting row requests its thumbnail. A full image is
/// requested by a separate tap, before opening the owned read-only viewer.
class _FriendImageMessage extends StatefulWidget {
  const _FriendImageMessage({
    super.key,
    required this.controller,
    required this.message,
  });
  final FriendConversationController controller;
  final HumanMessage message;
  @override
  State<_FriendImageMessage> createState() => _FriendImageMessageState();
}

class _FriendImageMessageState extends State<_FriendImageMessage> {
  FriendReceivedImage? _thumbnail;
  bool _loading = false, _opening = false, _attempted = false;
  String? _error;
  @override
  void initState() {
    super.initState();
    widget.controller.imageViewport.addListener(_schedule);
    _schedule();
  }

  @override
  void didUpdateWidget(covariant _FriendImageMessage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller != widget.controller ||
        oldWidget.message.id != widget.message.id) {
      oldWidget.controller.imageViewport.removeListener(_schedule);
      widget.controller.imageViewport.addListener(_schedule);
      _thumbnail = null;
      _attempted = false;
    }
    _schedule();
  }

  void _schedule() => WidgetsBinding.instance.addPostFrameCallback((_) {
    if (mounted && !_attempted && _visible()) unawaited(_load());
  });
  bool _visible() {
    if (!widget.controller.historyAvailable ||
        widget.controller.isVisible?.call() == false)
      return false;
    final box = context.findRenderObject();
    if (box is! RenderBox || !box.attached || !box.hasSize || box.size.isEmpty)
      return false;
    var visible = box.localToGlobal(Offset.zero) & box.size;
    // Respect every nested viewport; a shrink-wrapped inner list alone does
    // not prove the row is inside the host social panel's clip.
    RenderObject? ancestor = box.parent;
    while (ancestor != null) {
      if (ancestor is RenderAbstractViewport) {
        final bounds = MatrixUtils.transformRect(
          ancestor.getTransformTo(null),
          ancestor.paintBounds,
        );
        visible = visible.intersect(bounds);
        if (visible.isEmpty) return false;
      }
      ancestor = ancestor.parent;
    }
    final screen = Offset.zero & MediaQuery.sizeOf(context);
    return !visible.intersect(screen).isEmpty;
  }

  Future<void> _load() async {
    if (_loading || !_visible()) return;
    final c = widget.controller, epoch = c.imageEpoch, id = widget.message.id;
    setState(() {
      _loading = true;
      _attempted = true;
      _error = null;
    });
    final image = await c.receiveImage(
      widget.message,
      FriendImageVariant.thumbnail,
    );
    if (!mounted ||
        c != widget.controller ||
        id != widget.message.id ||
        !c.imageViewCurrent(epoch))
      return;
    setState(() {
      _loading = false;
      _thumbnail = image;
      _error = image == null ? '图片暂不可查看' : null;
    });
  }

  Future<void> _open() async {
    if (_opening || !_visible()) return;
    final c = widget.controller, epoch = c.imageEpoch;
    setState(() => _opening = true);
    final image = await c.receiveImage(widget.message, FriendImageVariant.full);
    if (!mounted ||
        !c.imageViewCurrent(epoch) ||
        image == null ||
        !image.current) {
      if (mounted)
        setState(() {
          _opening = false;
          _error = '图片暂不可查看';
        });
      return;
    }
    final full = widget.message.image!.full;
    await showDialog<void>(
      context: context,
      builder: (dialogContext) => AnimatedBuilder(
        animation: c,
        builder: (dialogContext, _) {
          final valid = c.imageViewCurrent(epoch) && image.current;
          final size = MediaQuery.sizeOf(dialogContext);
          final dpr = MediaQuery.devicePixelRatioOf(dialogContext);
          return Dialog(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Flexible(
                  child: valid
                      ? Image(
                          image: image.provider(
                            width: (size.width * dpr)
                                .ceil()
                                .clamp(1, full.width)
                                .toInt(),
                            height: (size.height * dpr)
                                .ceil()
                                .clamp(1, full.height)
                                .toInt(),
                          ),
                          fit: BoxFit.contain,
                          semanticLabel: '当前对话图片',
                          errorBuilder: (_, __, ___) => const Text('图片暂不可查看'),
                        )
                      : const Padding(
                          padding: EdgeInsets.all(24),
                          child: Text('图片访问已失效'),
                        ),
                ),
                TextButton(
                  onPressed: () => Navigator.pop(dialogContext),
                  child: const Text('关闭图片'),
                ),
              ],
            ),
          );
        },
      ),
    );
    if (mounted) setState(() => _opening = false);
  }

  @override
  Widget build(BuildContext context) {
    if (!widget.controller.imageEnabled) {
      return const Padding(
        padding: EdgeInsets.symmetric(vertical: 12),
        child: Text('当前版本暂不支持查看图片'),
      );
    }
    final meta = widget.message.image!, image = _thumbnail;
    return Semantics(
      label: '对话图片',
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (image?.current == true)
            SizedBox(
              width: 224,
              height: 160,
              child: Image(
                image: image!.provider(
                  width: meta.thumbnail.width,
                  height: meta.thumbnail.height,
                ),
                fit: BoxFit.contain,
                excludeFromSemantics: true,
                errorBuilder: (_, __, ___) => const Text('图片暂不可查看'),
              ),
            ),
          if (_loading)
            const Padding(padding: EdgeInsets.all(12), child: Text('正在读取图片…')),
          if (_error != null) Text(_error!),
          TextButton.icon(
            onPressed: _loading || _opening
                ? null
                : image?.current == true
                ? _open
                : _load,
            icon: const Icon(Icons.image_outlined),
            label: Text(
              _opening
                  ? '正在读取原图…'
                  : image?.current == true
                  ? '查看原图'
                  : '查看图片',
            ),
          ),
        ],
      ),
    );
  }

  @override
  void dispose() {
    widget.controller.imageViewport.removeListener(_schedule);
    super.dispose();
  }
}
