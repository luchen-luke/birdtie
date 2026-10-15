import 'package:flutter/material.dart';

import 'map_layers_api.dart';
import 'map_layers_controller.dart';

/// Only the current server-authorized author snapshot, shared with the map.
/// This body never filters the city collection or requests a wider fallback.
class PublicAuthorMoments extends StatelessWidget {
  const PublicAuthorMoments({
    super.key,
    required this.layers,
    required this.authorID,
    required this.authorScopeCurrent,
    required this.onOpenMoment,
    this.notice,
  });
  final MapLayersController layers;
  final String authorID;
  final bool Function() authorScopeCurrent;
  final ValueChanged<TypedMapItem> onOpenMoment;
  final String? notice;

  bool _current() {
    final view = layers.authorView;
    return authorScopeCurrent() &&
        view != null &&
        view.scope == 'PUBLIC_AUTHOR' &&
        view.authorID == authorID &&
        layers.authorID == authorID &&
        view.cityID == layers.authorCityID &&
        view.cityID == layers.cityID &&
        view.validUntil.isAfter(DateTime.now().toUtc()) &&
        layers.authorError == null &&
        !layers.authorLoading &&
        identical(layers.items, view.items) &&
        view.items.every(
          (i) =>
              i.kind == 'MOMENT' &&
              i.detailType == 'MOMENT' &&
              i.detailID == i.id,
        );
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: layers,
    builder: (_, _) {
      if (!_current()) {
        return Center(
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Text(
              layers.authorLoading
                  ? '正在核实作者当前范围的公开动态。'
                  : layers.authorError ?? '作者范围的来源已变化或到期，请返回重新打开。',
            ),
          ),
        );
      }
      final view = layers.authorView!;
      final items = layers.items;
      return ListView(
        key: PageStorageKey('public-author-moments-scroll:$authorID'),
        primary: false,
        padding: const EdgeInsets.all(24),
        children: [
          const Text('读取时的地图范围 · 仅公开动态，不代表作者现在位于这里'),
          const SizedBox(height: 16),
          if (notice != null) ...[Text(notice!), const SizedBox(height: 16)],
          if (items.isEmpty) const Text('当前范围暂无该作者可见的公开动态。'),
          for (final item in items)
            ListTile(
              key: ValueKey('author-moment:${item.id}'),
              contentPadding: EdgeInsets.zero,
              minVerticalPadding: 12,
              leading: const Icon(Icons.article_outlined),
              title: item.title.isEmpty ? null : Text(item.title),
              subtitle: Text(item.anchorLabel),
              trailing: const Icon(Icons.chevron_right),
              onTap: () {
                // Saved callbacks may outlive an identity/source transition.
                if (!_current() ||
                    !identical(view, layers.authorView) ||
                    !identical(item, layers.find(item.mapID))) {
                  return;
                }
                onOpenMoment(item);
              },
            ),
          if (view.truncated) const Text('仅展示当前范围已加载的公开动态；可返回地图缩小范围。'),
        ],
      );
    },
  );
}
