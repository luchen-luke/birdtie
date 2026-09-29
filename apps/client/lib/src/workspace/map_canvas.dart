import 'dart:async';

import 'package:flutter/material.dart';

import '../city/public_city_controller.dart';
import '../city/public_city_map.dart';
import 'agent_workspace_controller.dart';
import 'map_entities.dart';

class MapCanvas extends StatelessWidget {
  const MapCanvas({super.key, required this.city, required this.workspace});
  final PublicCityController city;
  final AgentWorkspaceController workspace;

  @override
  Widget build(BuildContext context) {
    final selectedCity = city.selectedCity;
    if (selectedCity == null) {
      return Container(
        color: const Color(0xFFE6EBE4),
        alignment: Alignment.center,
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Text(
            city.cityError ??
                (city.citiesLoading
                    ? 'Loading map…'
                    : 'Choose a city to open the map.'),
            textAlign: TextAlign.center,
            style: const TextStyle(color: Color(0xFF193B32)),
          ),
        ),
      );
    }
    final demoAllowed =
        workspace.demoMode &&
        selectedCity.name.toLowerCase().contains('aberdeen');
    final entities = demoAllowed
        ? (workspace.task == null
              ? demoIdleEntities
              : workspace.result?.entities ?? const <MapEntity>[])
        : (workspace.task == null
              ? [
                  for (final activity in city.activities.take(8))
                    if (activity.status == 'upcoming' ||
                        activity.status == 'ongoing')
                      if (activity.location case final location?)
                        if (location.hasPublicPoint)
                          MapEntity(
                            id: 'activity:${activity.id}',
                            kind: MapEntityKind.activity,
                            title: activity.title,
                            subtitle: activity.status,
                            latitude: location.latitude!,
                            longitude: location.longitude!,
                          ),
                ]
              : workspace.result?.entities ?? const <MapEntity>[]);
    final places =
        workspace.result?.places
            .where((place) => place.location.hasPublicPoint)
            .toList() ??
        const <PublicPlace>[];
    return Stack(
      children: [
        Positioned.fill(
          child: PublicCityMapView(
            city: selectedCity,
            places: places,
            entities: entities,
            selectedEntityId: workspace.selectedEntityId,
            contextKey: workspace.task?.id ?? 'idle',
            onEntitySelected: (entity) {
              if (workspace.task == null) {
                unawaited(
                  workspace.submit(
                    entity.title,
                    city.activities,
                    city.places,
                    cityID: selectedCity.id,
                  ),
                );
              } else {
                workspace.selectEntity(entity.id);
              }
            },
            onPlaceSelected: (place) =>
                workspace.selectEntity('place:${place.id}'),
            fullBleed: true,
          ),
        ),
        if (entities.any((entity) => entity.isDemo))
          const Positioned(
            top: 105,
            left: 16,
            child: DecoratedBox(
              decoration: BoxDecoration(
                color: Color(0xF5FCFBF8),
                borderRadius: BorderRadius.all(Radius.circular(18)),
              ),
              child: Padding(
                padding: EdgeInsets.symmetric(horizontal: 10, vertical: 6),
                child: Text(
                  'LOCAL MAP PREVIEW',
                  style: TextStyle(
                    fontSize: 10,
                    letterSpacing: 1,
                    fontWeight: FontWeight.w700,
                    color: Color(0xFF193B32),
                  ),
                ),
              ),
            ),
          ),
      ],
    );
  }
}
