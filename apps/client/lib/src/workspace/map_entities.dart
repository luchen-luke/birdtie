import 'package:flutter/material.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:latlong2/latlong.dart';

enum MapEntityKind { person, peopleCluster, activity, group, place }

class MapEntity {
  const MapEntity({
    required this.id,
    required this.kind,
    required this.title,
    required this.subtitle,
    required this.latitude,
    required this.longitude,
  });
  final String id;
  final MapEntityKind kind;
  final String title;
  final String subtitle;
  final double latitude;
  final double longitude;
}

class MapEntityLayer extends StatelessWidget {
  const MapEntityLayer({
    super.key,
    required this.entities,
    required this.selectedId,
    required this.onSelected,
  });
  final List<MapEntity> entities;
  final String? selectedId;
  final ValueChanged<MapEntity> onSelected;

  @override
  Widget build(BuildContext context) => MarkerLayer(
    markers: [
      for (final entity in entities)
        Marker(
          point: LatLng(entity.latitude, entity.longitude),
          width: selectedId == entity.id ? 142 : 112,
          height: 58,
          child: switch (entity.kind) {
            MapEntityKind.person => PersonMarker(
              entity: entity,
              selected: selectedId == entity.id,
              onTap: () => onSelected(entity),
            ),
            MapEntityKind.peopleCluster => PeopleClusterMarker(
              entity: entity,
              selected: selectedId == entity.id,
              onTap: () => onSelected(entity),
            ),
            MapEntityKind.activity => ActivityMarker(
              entity: entity,
              selected: selectedId == entity.id,
              onTap: () => onSelected(entity),
            ),
            MapEntityKind.group => GroupMarker(
              entity: entity,
              selected: selectedId == entity.id,
              onTap: () => onSelected(entity),
            ),
            MapEntityKind.place => ActivityMarker(
              entity: entity,
              selected: selectedId == entity.id,
              onTap: () => onSelected(entity),
            ),
          },
        ),
    ],
  );
}

class SelectedMarkerState extends StatelessWidget {
  const SelectedMarkerState({
    super.key,
    required this.entity,
    required this.icon,
    required this.selected,
    required this.onTap,
  });
  final MapEntity entity;
  final IconData icon;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) => Center(
    child: Semantics(
      button: true,
      label:
          '${entity.title}${entity.kind == MapEntityKind.person ? ', approximate public area' : ''}',
      child: Material(
        color: selected ? const Color(0xFF193B32) : Colors.white,
        elevation: selected ? 7 : 3,
        borderRadius: BorderRadius.circular(28),
        child: InkWell(
          borderRadius: BorderRadius.circular(28),
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(
                  icon,
                  size: 18,
                  color: selected ? Colors.white : const Color(0xFF193B32),
                ),
                const SizedBox(width: 5),
                Flexible(
                  child: Text(
                    entity.kind == MapEntityKind.person
                        ? '≈ ${entity.title}'
                        : entity.title,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontSize: 11,
                      fontWeight: FontWeight.w600,
                      color: selected ? Colors.white : const Color(0xFF193B32),
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    ),
  );
}

class PersonMarker extends StatelessWidget {
  const PersonMarker({
    super.key,
    required this.entity,
    required this.selected,
    required this.onTap,
  });
  final MapEntity entity;
  final bool selected;
  final VoidCallback onTap;
  @override
  Widget build(BuildContext context) => SelectedMarkerState(
    entity: entity,
    icon: Icons.person_outline,
    selected: selected,
    onTap: onTap,
  );
}

class PeopleClusterMarker extends StatelessWidget {
  const PeopleClusterMarker({
    super.key,
    required this.entity,
    required this.selected,
    required this.onTap,
  });
  final MapEntity entity;
  final bool selected;
  final VoidCallback onTap;
  @override
  Widget build(BuildContext context) => SelectedMarkerState(
    entity: entity,
    icon: Icons.people_outline,
    selected: selected,
    onTap: onTap,
  );
}

class ActivityMarker extends StatelessWidget {
  const ActivityMarker({
    super.key,
    required this.entity,
    required this.selected,
    required this.onTap,
  });
  final MapEntity entity;
  final bool selected;
  final VoidCallback onTap;
  @override
  Widget build(BuildContext context) => SelectedMarkerState(
    entity: entity,
    icon: Icons.sports_tennis,
    selected: selected,
    onTap: onTap,
  );
}

class GroupMarker extends StatelessWidget {
  const GroupMarker({
    super.key,
    required this.entity,
    required this.selected,
    required this.onTap,
  });
  final MapEntity entity;
  final bool selected;
  final VoidCallback onTap;
  @override
  Widget build(BuildContext context) => SelectedMarkerState(
    entity: entity,
    icon: Icons.group_outlined,
    selected: selected,
    onTap: onTap,
  );
}
