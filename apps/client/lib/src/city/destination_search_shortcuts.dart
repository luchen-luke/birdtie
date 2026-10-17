import 'package:flutter/material.dart';

/// Approved existing artwork without a matching Birdtie City identity. These
/// invoke the ordinary global search; they do not select a city or copy a camera.
class DestinationSearchShortcut {
  const DestinationSearchShortcut(
    this.key,
    this.name,
    this.countryCode,
    this.region,
    this.query,
  );
  final String key, name, countryCode, region, query;
  String get asset => 'assets/destinations/$key.png';
}

const destinationSearchShortcuts = [
  DestinationSearchShortcut('dali', '大理', 'CN', '中国 · 云南', '大理 中国'),
  DestinationSearchShortcut(
    'skye',
    '天空岛',
    'GB',
    '英国 · 苏格兰',
    'Isle of Skye United Kingdom',
  ),
  DestinationSearchShortcut(
    'islay',
    '艾雷岛',
    'GB',
    '英国 · 苏格兰',
    'Islay United Kingdom',
  ),
  DestinationSearchShortcut('iceland', '冰岛', 'IS', '冰岛', 'Iceland'),
  DestinationSearchShortcut('lisbon', '里斯本', 'PT', '葡萄牙', 'Lisbon Portugal'),
];

List<DestinationSearchShortcut> destinationShortcutsFor(String filter) =>
    destinationSearchShortcuts
        .where(
          (item) => filter == 'CN'
              ? item.countryCode == 'CN'
              : filter == 'abroad'
              ? item.countryCode != 'CN'
              : true,
        )
        .toList(growable: false);

class DestinationSearchShortcutGrid extends StatelessWidget {
  const DestinationSearchShortcutGrid({
    super.key,
    required this.filter,
    required this.onSearch,
  });
  final String filter;
  final ValueChanged<String> onSearch;
  @override
  Widget build(BuildContext context) {
    final items = destinationShortcutsFor(filter);
    return SliverLayoutBuilder(
      builder: (context, box) => SliverGrid(
        gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
          crossAxisCount: box.crossAxisExtent >= 640 ? 3 : 2,
          mainAxisExtent: 196,
          crossAxisSpacing: 12,
          mainAxisSpacing: 8,
        ),
        delegate: SliverChildBuilderDelegate((context, index) {
          final item = items[index];
          return Semantics(
            button: true,
            label: '搜索${item.name}，${item.region}',
            child: Material(
              color: Colors.transparent,
              child: InkWell(
                key: Key('destination-search-${item.key}'),
                borderRadius: BorderRadius.circular(12),
                onTap: () => onSearch(item.query),
                child: ExcludeSemantics(
                  child: Padding(
                    padding: const EdgeInsets.all(4),
                    child: Column(
                      children: [
                        SizedBox.square(
                          dimension: 108,
                          child: Image.asset(
                            item.asset,
                            fit: BoxFit.contain,
                            cacheWidth:
                                (108 * MediaQuery.devicePixelRatioOf(context))
                                    .round()
                                    .clamp(1, 324),
                          ),
                        ),
                        const SizedBox(height: 6),
                        Text(
                          item.name,
                          style: Theme.of(context).textTheme.titleSmall,
                          textAlign: TextAlign.center,
                        ),
                        const SizedBox(height: 2),
                        Text(
                          '${item.region} · 搜索',
                          style: Theme.of(context).textTheme.bodySmall,
                          maxLines: 2,
                          textAlign: TextAlign.center,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          );
        }, childCount: items.length),
      ),
    );
  }
}
