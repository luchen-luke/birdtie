import 'dart:math' as math;

import 'package:flutter/material.dart';

import 'now_creation_draft.dart';
import 'now_ui_tokens.dart';
import 'aberdeen_schedule.dart';

/// A preview inside Now. All text editing stays in the shared composer.
class NowCreationPanel extends StatelessWidget {
  const NowCreationPanel({
    super.key,
    required this.draft,
    this.collapsed = false,
    this.busy = false,
    this.notice,
    this.error,
    required this.onToggle,
    required this.onClose,
    required this.onEditTitle,
    required this.onEditBody,
    required this.onChoosePlace,
    required this.onChooseTime,
    required this.onSave,
    this.onPublish,
    this.onRecover,
    this.extraActions = const [],
    this.mediaPreview,
    this.extraContent,
    this.statusLabel = '本地草稿 · 仅自己可见',
    this.saveEnabled = true,
    this.editingEnabled = true,
  });

  final NowCreationDraft draft;
  final bool collapsed, busy;
  final String? notice, error;
  final VoidCallback onToggle,
      onClose,
      onEditTitle,
      onEditBody,
      onChoosePlace,
      onChooseTime,
      onSave;
  final VoidCallback? onPublish, onRecover;
  final List<Widget> extraActions;
  final Widget? mediaPreview, extraContent;
  final String statusLabel;
  final bool saveEnabled, editingEnabled;

  IconData get _icon => switch (draft.kind) {
    'moment' => Icons.article_outlined,
    'activity' => Icons.event_outlined,
    'journey' => Icons.route_outlined,
    'theme_map' || 'theme-map' || 'collection' => Icons.map_outlined,
    _ => Icons.edit_note_outlined,
  };

  @override
  Widget build(BuildContext context) {
    final tokens = NowUiTokens.of(context);
    final theme = Theme.of(context);
    return LayoutBuilder(
      builder: (context, constraints) {
        final fallbackHeight = MediaQuery.sizeOf(context).height * .48;
        final maximumHeight = math.min(
          420.0,
          constraints.hasBoundedHeight ? constraints.maxHeight : fallbackHeight,
        );
        return ConstrainedBox(
          constraints: BoxConstraints(maxHeight: maximumHeight),
          child: Material(
            color: theme.colorScheme.surface,
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(20),
              side: BorderSide(color: NowUiTokens.materialEdge(theme)),
            ),
            clipBehavior: Clip.antiAlias,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                _header(tokens),
                if (!collapsed)
                  Flexible(
                    fit: FlexFit.loose,
                    child: SingleChildScrollView(
                      primary: false,
                      padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
                      child: _preview(context, tokens),
                    ),
                  ),
              ],
            ),
          ),
        );
      },
    );
  }

  Widget _header(NowUiTokens tokens) => Row(
    children: [
      Expanded(
        child: Semantics(
          expanded: !collapsed,
          child: InkWell(
            onTap: onToggle,
            child: ConstrainedBox(
              constraints: const BoxConstraints(minHeight: 48),
              child: Padding(
                padding: const EdgeInsets.only(left: 16, right: 8),
                child: Row(
                  children: [
                    Icon(_icon, size: 20),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Text(
                        '创建${draft.label}',
                        style: tokens.chromeLabel,
                      ),
                    ),
                    if (busy)
                      const Padding(
                        padding: EdgeInsets.only(right: 8),
                        child: SizedBox.square(
                          dimension: 16,
                          child: CircularProgressIndicator(
                            strokeWidth: 2,
                            semanticsLabel: '正在处理草稿',
                          ),
                        ),
                      ),
                    Icon(
                      collapsed ? Icons.expand_more : Icons.expand_less,
                      size: 20,
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
      IconButton(
        onPressed: onClose,
        tooltip: '关闭${draft.label}草稿',
        constraints: const BoxConstraints(minWidth: 48, minHeight: 48),
        icon: const Icon(Icons.close, size: 20),
      ),
    ],
  );

  Widget _preview(BuildContext context, NowUiTokens tokens) {
    final actionStyle = TextButton.styleFrom(
      minimumSize: const Size(48, 48),
      textStyle: tokens.action,
      padding: const EdgeInsets.symmetric(horizontal: 12),
    );
    final hasBody = draft.body.trim().isNotEmpty;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const SizedBox(height: 4),
        Text(statusLabel, style: tokens.metadata),
        const SizedBox(height: 8),
        Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Expanded(
              child: Padding(
                padding: const EdgeInsets.only(top: 10),
                child: Text(
                  draft.title.trim().isEmpty
                      ? '未命名${draft.label}'
                      : draft.title,
                  style: tokens.sheetTitle,
                ),
              ),
            ),
            IconButton(
              onPressed: busy || !editingEnabled ? null : onEditTitle,
              tooltip: '修改标题',
              constraints: const BoxConstraints(minWidth: 48, minHeight: 48),
              icon: const Icon(Icons.edit_outlined, size: 18),
            ),
          ],
        ),
        const SizedBox(height: 4),
        if (hasBody)
          Text(draft.body, style: tokens.body)
        else
          Text('在下方输入框写下${draft.label}内容。', style: tokens.body),
        Align(
          alignment: Alignment.centerLeft,
          child: TextButton(
            style: actionStyle,
            onPressed: busy || !editingEnabled ? null : onEditBody,
            child: Text(hasBody ? '修改内容' : '开始写内容'),
          ),
        ),
        if (mediaPreview != null) ...[
          const SizedBox(height: 8),
          mediaPreview!,
          const SizedBox(height: 8),
        ],
        _contextAction(
          icon: Icons.place_outlined,
          text: _placeLabel,
          tooltip: '选择关联地点',
          onPressed: busy || !editingEnabled ? null : onChoosePlace,
          tokens: tokens,
        ),
        if (draft.kind == 'activity')
          _contextAction(
            icon: Icons.schedule_outlined,
            text: _timeLabel(context),
            tooltip: '选择活动时间',
            onPressed: busy || !editingEnabled ? null : onChooseTime,
            tokens: tokens,
          ),
        if (notice?.trim().isNotEmpty == true) ...[
          const SizedBox(height: 8),
          Semantics(liveRegion: true, child: Text(notice!, style: tokens.body)),
        ],
        if (error?.trim().isNotEmpty == true) ...[
          const SizedBox(height: 8),
          Semantics(
            liveRegion: true,
            child: Text(
              error!,
              style: tokens.body.copyWith(
                color: Theme.of(context).colorScheme.error,
              ),
            ),
          ),
        ],
        const SizedBox(height: 12),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            FilledButton(
              style: FilledButton.styleFrom(
                minimumSize: const Size(48, 48),
                textStyle: tokens.action,
              ),
              onPressed: busy || !saveEnabled ? null : onSave,
              child: const Text('保存草稿'),
            ),
            if (onPublish != null)
              TextButton(
                style: actionStyle,
                onPressed: busy ? null : onPublish,
                child: const Text('检查公开发布'),
              ),
            if (onRecover != null)
              TextButton(
                style: actionStyle,
                onPressed: busy ? null : onRecover,
                child: const Text('核实上次操作'),
              ),
            ...extraActions,
          ],
        ),
        if (extraContent != null) ...[
          const SizedBox(height: 12),
          extraContent!,
        ],
      ],
    );
  }

  String get _placeLabel {
    final city = draft.cityName.trim();
    final place = draft.placeName.trim();
    if (place.isNotEmpty) return city.isEmpty ? place : '$city · $place';
    if (draft.placeID.isNotEmpty) {
      return city.isEmpty ? '关联地点名称待核实' : '$city · 地点名称待核实';
    }
    return city.isEmpty ? '选择城市或地点' : '$city · 选择关联地点';
  }

  String _timeLabel(BuildContext context) {
    final start = draft.startsAt, end = draft.endsAt;
    if (start == null && end == null) return '选择活动时间';
    String stamp(DateTime time) {
      final local = draft.cityID == 'aberdeen-gb'
          ? utcToAberdeenLocal(time)
          : time.toUtc();
      final date = MaterialLocalizations.of(context).formatMediumDate(local);
      final hour = local.hour.toString().padLeft(2, '0');
      final minute = local.minute.toString().padLeft(2, '0');
      return '$date $hour:$minute${draft.cityID == 'aberdeen-gb' ? '（阿伯丁时间）' : ' UTC'}';
    }

    if (start == null) return '开始时间待定 · 结束 ${stamp(end!)}';
    if (end == null) return '${stamp(start)} · 结束时间待定';
    return '${stamp(start)} — ${stamp(end)}';
  }

  Widget _contextAction({
    required IconData icon,
    required String text,
    required String tooltip,
    required VoidCallback? onPressed,
    required NowUiTokens tokens,
  }) => Tooltip(
    message: tooltip,
    child: TextButton(
      onPressed: onPressed,
      style: TextButton.styleFrom(
        minimumSize: const Size(48, 48),
        alignment: Alignment.centerLeft,
        padding: EdgeInsets.zero,
      ),
      child: Row(
        children: [
          Icon(icon, size: 18),
          const SizedBox(width: 8),
          Expanded(child: Text(text, style: tokens.listSubtitle)),
          const SizedBox(width: 8),
          const Icon(Icons.chevron_right, size: 18),
        ],
      ),
    ),
  );
}
