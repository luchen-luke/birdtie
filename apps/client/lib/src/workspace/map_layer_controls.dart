import 'package:flutter/material.dart';
import 'map_layers_api.dart';
import 'map_layers_controller.dart';

class MapLayerControls extends StatelessWidget {
  const MapLayerControls({
    super.key,
    required this.controller,
    required this.current,
    required this.onRefresh,
    required this.onOpen,
  });
  final MapLayersController controller;
  final bool Function() current;
  final VoidCallback? onRefresh;
  final ValueChanged<TypedMapItem> onOpen;
  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: controller,
    builder: (context, _) => SafeArea(
      child: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          Text('地图图层', style: Theme.of(context).textTheme.titleLarge),
          const Text('仅显示明确公开的点位。公开记录的地点不代表作者当前位置；商家点位代表经营场地。'),
          if (!current())
            const Text('账号或情境已变化，请关闭后重新打开。')
          else ...[
            for (final kind in mapLayerKinds)
              SwitchListTile(
                key: ValueKey('map-layer-$kind'),
                contentPadding: EdgeInsets.zero,
                title: Text(mapLayerLabel(kind)),
                subtitle: kind == 'OPPORTUNITY'
                    ? const Text('仅本人可见，不公开意图或关系。打开后读取当前公开活动。')
                    : null,
                value: controller.visible.contains(kind),
                onChanged:
                    kind == 'OPPORTUNITY' &&
                        (controller.authorizationHeader() == null ||
                            controller.organizationWorkspaceID() != null)
                    ? null
                    : (v) {
                        if (current()) controller.toggle(kind, v);
                      },
              ),
            FilledButton.icon(
              onPressed: controller.loading || onRefresh == null
                  ? null
                  : () {
                      if (current()) onRefresh?.call();
                    },
              icon: const Icon(Icons.refresh),
              label: const Text('读取当前地图范围'),
            ),
            if (onRefresh == null)
              const Text('地图范围暂未就绪，请先选择有地图配置的城市。')
            else if (controller.loading)
              const Text('正在读取当前来源…')
            else if (controller.error != null)
              Text(controller.error!)
            else if (controller.publicView == null)
              const Text('尚未读取此范围的公开点位。'),
            if (controller.publicView?.truncated == true ||
                controller.privateView?.truncated == true)
              const Text('当前范围内容较多，请缩小范围后重新读取。'),
            if (!controller.loading &&
                onRefresh != null &&
                controller.publicView != null &&
                controller.error == null &&
                controller.items.isEmpty)
              const Text('当前图层没有符合公开点位条件的内容。'),
            for (final item
                in onRefresh != null &&
                        controller.error == null &&
                        !controller.loading
                    ? controller.items
                    : <TypedMapItem>[])
              ListTile(
                key: ValueKey('map-layer-card-${item.mapID}'),
                contentPadding: EdgeInsets.zero,
                title: Text(item.title),
                subtitle: Text(
                  '${mapLayerLabel(item.kind)} · ${item.explanation}',
                ),
                trailing: const Icon(Icons.chevron_right),
                onTap: () {
                  if (current() && controller.find(item.mapID) != null) {
                    onOpen(item);
                  }
                },
              ),
          ],
        ],
      ),
    ),
  );
}
