import 'package:flutter/material.dart';

import '../workspace/map_entities.dart';
import 'map_cluster_members_sheet.dart' show CurrentMapReaderVisibility;

/// Resolves overlapping touch targets without grouping or moving map objects.
/// Providers supply only current, authorized objects from their visible source.
Future<MapEntity?> showCurrentMapPointChoices({
  required BuildContext context,
  required Listenable changes,
  required List<MapEntity> Function() currentCandidates,
  required Object? Function() currentScope,
  ValueChanged<({Object token, bool visible})>? onVisibilityChanged,
}) async {
  final scope = currentScope(), token = Object();
  var visible = false;
  void report(bool next) {
    if (visible == next) return;
    visible = next;
    onVisibilityChanged?.call((token: token, visible: next));
  }

  if (currentCandidates().isEmpty) return null;
  try {
    final selected = await showModalBottomSheet<MapEntity>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Theme.of(context).colorScheme.surface,
      builder: (_) => CurrentMapReaderVisibility(
        current: () => currentScope() == scope,
        onChanged: report,
        child: _CurrentPointChoices(
          changes: changes,
          currentCandidates: currentCandidates,
          current: () => currentScope() == scope,
        ),
      ),
    );
    if (selected == null || currentScope() != scope) return null;
    return currentCandidates()
        .where(
          (e) =>
              e.id == selected.id && e.kind == selected.kind && e.hasMapAnchor,
        )
        .firstOrNull;
  } finally {
    report(false);
  }
}

String mapPointKindLabel(MapEntityKind kind) => switch (kind) {
  MapEntityKind.place => '地点',
  MapEntityKind.moment => '动态',
  MapEntityKind.activity => '活动',
  MapEntityKind.person => '大致区域',
  MapEntityKind.organization => '组织',
  MapEntityKind.business => '经营场地',
  MapEntityKind.opportunity => '活动机会',
  MapEntityKind.group || MapEntityKind.community => '群组',
  _ => '内容',
};

class _CurrentPointChoices extends StatefulWidget {
  const _CurrentPointChoices({
    required this.changes,
    required this.currentCandidates,
    required this.current,
  });
  final Listenable changes;
  final List<MapEntity> Function() currentCandidates;
  final bool Function() current;
  @override
  State<_CurrentPointChoices> createState() => _CurrentPointChoicesState();
}

class _CurrentPointChoicesState extends State<_CurrentPointChoices> {
  final _scroll = ScrollController();
  bool _retired = false;
  @override
  void initState() {
    super.initState();
    widget.changes.addListener(_changed);
  }

  void _changed() {
    if (!mounted || _retired) return;
    if (!widget.current() || widget.currentCandidates().isEmpty) {
      _retired = true; // A -> B -> A must never revive old selection.
      setState(() {});
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted) return;
        final route = ModalRoute.of(context);
        if (route?.isCurrent == true) {
          Navigator.of(context).pop();
        } else if (route != null && route.isActive) {
          Navigator.of(context).removeRoute(route);
        }
      });
    } else {
      setState(() {});
    }
  }

  @override
  void dispose() {
    widget.changes.removeListener(_changed);
    _scroll.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final rows = _retired || !widget.current()
        ? const <MapEntity>[]
        : widget.currentCandidates();
    return SafeArea(
      child: SizedBox(
        key: const Key('current-map-point-choices'),
        height: MediaQuery.sizeOf(context).height * .62,
        child: Column(
          children: [
            ListTile(
              title: const Text('选择对象'),
              trailing: IconButton(
                tooltip: '关闭对象选择',
                onPressed: () => Navigator.pop(context),
                constraints: const BoxConstraints(minWidth: 48, minHeight: 48),
                icon: const Icon(Icons.close),
              ),
            ),
            Expanded(
              child: ListView(
                controller: _scroll,
                children: [
                  for (final row in rows)
                    ListTile(
                      key: ValueKey('map-point-choice:${row.id}'),
                      minTileHeight: 48,
                      leading:
                          {
                                MapEntityKind.place,
                                MapEntityKind.moment,
                              }.contains(row.kind) &&
                              row.mediaPixels?.current() == true
                          ? SizedBox(
                              width: 36,
                              height: 36,
                              child: ClipRRect(
                                borderRadius: BorderRadius.circular(
                                  row.kind == MapEntityKind.moment ? 4 : 999,
                                ),
                                child: RawImage(
                                  key: ValueKey(
                                    'map-point-choice-photo:${row.id}',
                                  ),
                                  image: row.mediaPixels!.image,
                                  fit: BoxFit.cover,
                                ),
                              ),
                            )
                          : Icon(
                              row.kind == MapEntityKind.moment
                                  ? Icons.notes_outlined
                                  : Icons.place_outlined,
                            ),
                      title: row.title.isEmpty
                          ? null
                          : Text(
                              row.title,
                              maxLines: 2,
                              overflow: TextOverflow.ellipsis,
                            ),
                      subtitle: Text(
                        '${mapPointKindLabel(row.kind)} · ${row.subtitle}',
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                      ),
                      onTap: () {
                        if (_retired || !widget.current()) return;
                        final latest = widget
                            .currentCandidates()
                            .where(
                              (e) =>
                                  e.id == row.id &&
                                  e.kind == row.kind &&
                                  e.hasMapAnchor,
                            )
                            .firstOrNull;
                        if (latest != null) Navigator.pop(context, latest);
                      },
                    ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}
