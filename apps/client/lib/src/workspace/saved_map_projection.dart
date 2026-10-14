import 'package:flutter/foundation.dart';
import 'map_canvas.dart';
import 'map_entities.dart';
import 'saved_items.dart';

/// A display projection of current authorized Saved rows, never a new map.
class SavedMapProjectionSource {
  SavedMapProjectionSource({
    required this.saved,
    required List<SavedItem> items,
    required this.cityID,
    required this.boundaryChanges,
    required this.sourceCurrent,
    required this.onRetired,
    String title = '收藏地点',
  }) : items = List.unmodifiable(items) {
    if (items.isEmpty ||
        items.any((i) => !i.mapCurrent || i.cityId != cityID) ||
        items.map((i) => i.targetId).toSet().length != items.length)
      throw ArgumentError('没有当前获准地图地点');
    _generation = saved.identityGeneration;
    _query = (saved.query, saved.queryKind, saved.queryCity);
    projection = PersonalCollectionProjection(
      id: 'saved:$cityID:$_generation:${identityHashCode(this)}',
      cityID: cityID,
      title: title,
      sourceCurrent: () => current,
      entities: [
        for (final item in items)
          MapEntity(
            id: 'place:${item.targetId}',
            kind: MapEntityKind.place,
            placeID: item.targetId,
            title: item.title,
            subtitle: item.summary,
            latitude: item.latitude!,
            longitude: item.longitude!,
            precision: 'point',
            publicLocation: true,
          ),
      ],
    );
    saved.addListener(_changed);
    boundaryChanges.addListener(_changed);
  }
  final SavedController saved;
  final List<SavedItem> items;
  final String cityID;
  final Listenable boundaryChanges;
  final bool Function() sourceCurrent;
  final VoidCallback onRetired;
  late final PersonalCollectionProjection projection;
  late final int _generation;
  late final (String, String?, String?) _query;
  bool _retired = false, _disposed = false;
  final selectedIDs = <String>{};
  bool get current =>
      !_disposed &&
      !_retired &&
      sourceCurrent() &&
      !saved.failed &&
      !saved.pageChanged &&
      saved.identityGeneration == _generation &&
      _query == (saved.query, saved.queryKind, saved.queryCity) &&
      items.isNotEmpty &&
      items.every(
        (i) =>
            i.mapCurrent &&
            i.cityId == cityID &&
            saved.items.any(
              (v) =>
                  v.id == i.id &&
                  v.targetId == i.targetId &&
                  v.kind == 'place' &&
                  v.available &&
                  v.latitude == i.latitude &&
                  v.longitude == i.longitude &&
                  v.sourceExpiresAt == i.sourceExpiresAt,
            ),
      );
  DateTime get expiresAt => items
      .map((i) => i.sourceExpiresAt!)
      .reduce((a, b) => a.isBefore(b) ? a : b);
  void _changed() {
    if (!_retired && !current) {
      _retired = true;
      selectedIDs.clear();
      onRetired();
    }
  }

  void toggle(String mapID) {
    if (!current || !projection.entities.any((e) => e.id == mapID)) return;
    if (!selectedIDs.remove(mapID) && selectedIDs.length < 4)
      selectedIDs.add(mapID);
  }

  List<SavedItem> get comparison => current
      ? items.where((i) => selectedIDs.contains('place:${i.targetId}')).toList()
      : const [];
  void dispose() {
    _disposed = true;
    saved.removeListener(_changed);
    boundaryChanges.removeListener(_changed);
    selectedIDs.clear();
  }
}
