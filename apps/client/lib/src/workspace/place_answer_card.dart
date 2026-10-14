import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'agent_answer_sources.dart';
import 'entity_action_contract.dart';
import 'now_ui_tokens.dart';
import 'place_fact_presentation.dart';

/// One answer body, one provenance entrance and one primary domain action.
class PlaceAnswerCard extends StatelessWidget {
  const PlaceAnswerCard({
    super.key,
    required this.presentation,
    required this.current,
    required this.changes,
    this.onFocus,
    this.onNavigate,
    this.onShare,
    this.onSave,
    this.saved = false,
    this.saving = false,
    this.openSource,
    this.resolveSources,
  });
  final PlaceFactPresentation presentation;
  final bool Function() current;
  final Listenable changes;
  final VoidCallback? onFocus, onNavigate, onShare, onSave;
  final bool saved, saving;
  final Future<bool> Function(Uri)? openSource;
  final AgentPlaceSourceBinding? Function()? resolveSources;

  bool _allowed(EntityActionKind kind) =>
      current() && presentation.item.action(kind)?.available == true;

  void _run(VoidCallback? action, {EntityActionKind? kind}) {
    if (!current() || (kind != null && !_allowed(kind))) return;
    action?.call();
  }

  @override
  Widget build(BuildContext context) {
    final tokens = NowUiTokens.reviewedOf(context);
    return Column(
      key: ValueKey('place-answer-${presentation.entityID}'),
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Expanded(
              child: Semantics(
                button: onFocus != null,
                label:
                    '${presentation.name}，${presentation.formattedAddress}'
                    '${onFocus == null ? '' : '，在地图上定位'}',
                child: InkWell(
                  key: const Key('place-answer-map-focus'),
                  onTap: onFocus == null ? null : () => _run(onFocus),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(vertical: 8),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        Text(presentation.name, style: tokens.placeTitle),
                        const SizedBox(height: 4),
                        Text(
                          presentation.formattedAddress,
                          style: tokens.address,
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ),
            AgentPlaceSourceEntry(
              sources: presentation.sources,
              current: current,
              changes: changes,
              placeSource: presentation.place.source,
              openSource: openSource,
              resolveCurrent: resolveSources,
            ),
          ],
        ),
        const SizedBox(height: 12),
        Wrap(
          spacing: 12,
          runSpacing: 8,
          alignment: WrapAlignment.spaceBetween,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            if (onNavigate != null)
              FilledButton.icon(
                key: const Key('place-answer-navigate'),
                style: FilledButton.styleFrom(
                  minimumSize: const Size(48, 48),
                  textStyle: tokens.action,
                ),
                onPressed: _allowed(EntityActionKind.navigate)
                    ? () => _run(onNavigate, kind: EntityActionKind.navigate)
                    : null,
                icon: const Icon(Icons.directions_outlined, size: 20),
                label: const Text('导航'),
              ),
            PopupMenuButton<String>(
              key: const Key('place-answer-more'),
              tooltip: '更多地点操作',
              onSelected: (action) async {
                if (!current()) return;
                switch (action) {
                  case 'share':
                    _run(onShare, kind: EntityActionKind.share);
                  case 'save':
                    if (!saving) _run(onSave, kind: EntityActionKind.save);
                  case 'copy':
                    await Clipboard.setData(
                      ClipboardData(text: presentation.formattedAddress),
                    );
                    if (context.mounted && current()) {
                      ScaffoldMessenger.of(
                        context,
                      ).showSnackBar(const SnackBar(content: Text('地址已复制。')));
                    }
                }
              },
              itemBuilder: (_) => [
                if (onShare != null)
                  PopupMenuItem(
                    value: 'share',
                    enabled: _allowed(EntityActionKind.share),
                    child: Text('分享给好友', style: tokens.action),
                  ),
                if (onSave != null)
                  PopupMenuItem(
                    value: 'save',
                    enabled: !saving && _allowed(EntityActionKind.save),
                    child: Text(saved ? '取消收藏' : '收藏', style: tokens.action),
                  ),
                PopupMenuItem(
                  value: 'copy',
                  enabled: current(),
                  child: Text('复制地址', style: tokens.action),
                ),
              ],
              child: ConstrainedBox(
                constraints: const BoxConstraints(minWidth: 48, minHeight: 48),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text('更多', style: tokens.action),
                    const SizedBox(width: 4),
                    const Icon(Icons.more_horiz, size: 20),
                  ],
                ),
              ),
            ),
          ],
        ),
      ],
    );
  }
}
