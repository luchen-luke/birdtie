import 'package:flutter/material.dart';

import '../city/public_city_controller.dart';
import 'map_entities.dart';
import 'now_discovery_controller.dart';

class EntityPeekCard extends StatelessWidget {
  const EntityPeekCard({super.key, required this.entity, required this.onOpen});

  final MapEntity entity;
  final VoidCallback onOpen;

  @override
  Widget build(BuildContext context) => Material(
    color: const Color(0xFFFCFBF8),
    elevation: 5,
    borderRadius: BorderRadius.circular(18),
    child: Padding(
      padding: const EdgeInsets.fromLTRB(16, 12, 8, 12),
      child: Row(
        children: [
          Icon(switch (entity.kind) {
            MapEntityKind.moment => Icons.notes_outlined,
            MapEntityKind.business => Icons.storefront_outlined,
            MapEntityKind.opportunity => Icons.lightbulb_outline,
            MapEntityKind.activity => Icons.event_outlined,
            MapEntityKind.organization => Icons.groups_outlined,
            MapEntityKind.community => Icons.groups_outlined,
            MapEntityKind.person => Icons.person_outline,
            _ => Icons.place_outlined,
          }, color: const Color(0xFF193B32)),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  entity.title,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    color: Color(0xFF193B32),
                    fontWeight: FontWeight.w700,
                  ),
                ),
                if (entity.kind == MapEntityKind.opportunity)
                  const Text(
                    '仅你可见',
                    style: TextStyle(color: Color(0xFF193B32)),
                  ),
              ],
            ),
          ),
          TextButton(onPressed: onOpen, child: const Text('查看')),
        ],
      ),
    ),
  );
}

class AreaPulseStack extends StatelessWidget {
  const AreaPulseStack({
    super.key,
    required this.pulse,
    required this.bounds,
    required this.loading,
    this.errorMessage,
    this.fallbackActivities,
    this.fallbackLoading = false,
    this.fallbackError,
    this.onOpenActivity,
    this.onActivityImpression,
    required this.onSearch,
    this.onRetry,
  });

  final NowPulse? pulse;
  final MapBounds? bounds;
  final bool loading;
  final String? errorMessage;
  final List<PublicActivity>? fallbackActivities;
  final bool fallbackLoading;
  final String? fallbackError;
  final ValueChanged<PublicActivity>? onOpenActivity;
  final ValueChanged<PublicActivity>? onActivityImpression;
  final ValueChanged<String> onSearch;
  final VoidCallback? onRetry;

  String _categoryLabel(String code) => switch (code) {
    'badminton' => '羽毛球',
    'sports' => '运动',
    'student' => '学生社交',
    'social' => '社交',
    'culture' => '文化',
    'other' => '其他',
    _ => '其他活动',
  };

  @override
  Widget build(BuildContext context) {
    final current = pulse != null && pulse!.bounds == bounds;
    return Material(
      color: const Color(0xFFF9F8F3),
      elevation: 2,
      borderRadius: BorderRadius.circular(16),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(
              fallbackActivities == null ? '这片区域有什么' : '当前城市近期活动',
              style: const TextStyle(
                fontWeight: FontWeight.w700,
                color: Color(0xFF193B32),
              ),
            ),
            if (fallbackActivities != null) ...[
              const Text('地图暂不可用。以下活动来自当前城市公开数据。'),
              if (fallbackLoading)
                const Text('正在读取活动…')
              else if (fallbackError != null)
                Text(fallbackError!)
              else if (fallbackActivities!.isEmpty)
                const Text('当前城市暂无近期公开活动。')
              else
                for (final activity in fallbackActivities!.take(3))
                  Builder(
                    builder: (context) {
                      if (onActivityImpression != null) {
                        WidgetsBinding.instance.addPostFrameCallback(
                          (_) => onActivityImpression!(activity),
                        );
                      }
                      return TextButton(
                        onPressed: onOpenActivity == null
                            ? null
                            : () => onOpenActivity!(activity),
                        child: Align(
                          alignment: Alignment.centerLeft,
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                activity.title,
                                maxLines: 2,
                                overflow: TextOverflow.ellipsis,
                              ),
                              if (activity.organizer?.name.isNotEmpty == true)
                                Text(
                                  '主办方：${activity.organizer!.name}',
                                  style: Theme.of(context).textTheme.bodySmall,
                                ),
                              Text(
                                [
                                  if (activity.placeName.isNotEmpty)
                                    activity.placeName,
                                  if (activity.schedule.isNotEmpty)
                                    activity.schedule,
                                  if (activity.source.label.isNotEmpty)
                                    activity.source.label,
                                ].join(' · '),
                                maxLines: 2,
                                overflow: TextOverflow.ellipsis,
                                style: Theme.of(context).textTheme.bodySmall,
                              ),
                            ],
                          ),
                        ),
                      );
                    },
                  ),
            ] else if (errorMessage != null)
              onRetry == null
                  ? Text(errorMessage!)
                  : TextButton(onPressed: onRetry, child: Text(errorMessage!))
            else if (bounds == null)
              const Text('正在查看附近活动…')
            else if (loading && !current)
              const Text('正在读取这片区域的活动…')
            else if (!current)
              const Text('点击“搜索此区域”更新附近活动')
            else if (pulse!.status == 'empty')
              const Text('当前地图范围内暂无公开活动')
            else
              Wrap(
                spacing: 7,
                runSpacing: 2,
                children: [
                  ActionChip(
                    visualDensity: VisualDensity.compact,
                    label: Text('当前区域 · ${pulse!.total}'),
                    onPressed: () => onSearch('搜索此区域'),
                  ),
                  for (final category in pulse!.categories.take(2))
                    if (category.code == 'badminton')
                      ActionChip(
                        visualDensity: VisualDensity.compact,
                        label: Text(
                          '${_categoryLabel(category.code)} · ${category.count}',
                        ),
                        onPressed: () => onSearch('找羽毛球活动'),
                      )
                    else
                      Chip(
                        visualDensity: VisualDensity.compact,
                        label: Text(
                          '${_categoryLabel(category.code)} · ${category.count}',
                        ),
                      ),
                ],
              ),
          ],
        ),
      ),
    );
  }
}
