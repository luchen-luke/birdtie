import 'dart:convert';
import 'dart:js_interop';

import 'package:flutter/material.dart';
import 'package:web/web.dart' as web;

import 'public_city_controller.dart';
import '../workspace/map_entities.dart';

@JS('birdtieAMapMount')
external JSPromise<JSNumber> _mountAMap(
  JSString elementID,
  JSString config,
  JSFunction onSelect,
);

@JS('birdtieAMapDestroy')
external void _destroyAMap(JSNumber handle);

class AMapCityMapView extends StatefulWidget {
  const AMapCityMapView({
    super.key,
    required this.city,
    required this.places,
    required this.entities,
    required this.selectedEntityId,
    required this.onPlaceSelected,
    required this.onEntitySelected,
    required this.fullBleed,
  });

  final PublicCity city;
  final List<PublicPlace> places;
  final List<MapEntity> entities;
  final String? selectedEntityId;
  final ValueChanged<PublicPlace> onPlaceSelected;
  final ValueChanged<MapEntity>? onEntitySelected;
  final bool fullBleed;

  @override
  State<AMapCityMapView> createState() => _AMapCityMapViewState();
}

class _AMapCityMapViewState extends State<AMapCityMapView> {
  static const _key = String.fromEnvironment('BIRDTIE_AMAP_WEB_PUBLIC_KEY');
  static const _serviceHost = String.fromEnvironment(
    'BIRDTIE_AMAP_SERVICE_HOST',
  );
  static int _nextID = 0;
  JSNumber? _handle;
  JSFunction? _onSelectCallback;
  bool _failed = false;

  Future<void> _created(Object element) async {
    final div = element as web.HTMLDivElement;
    final id = 'birdtie-amap-${_nextID++}';
    div.id = id;
    div.style.width = '100%';
    div.style.height = '100%';
    _onSelectCallback = ((JSString rawID) {
      if (!mounted) return;
      final selected = rawID.toDart;
      for (final place in widget.places) {
        if ('place:${place.id}' == selected) {
          widget.onPlaceSelected(place);
          return;
        }
      }
      for (final entity in widget.entities) {
        if (entity.id == selected) {
          widget.onEntitySelected?.call(entity);
          return;
        }
      }
    }).toJS;
    final viewport = widget.city.map!;
    final markers = [
      for (final place in widget.places)
        if (place.location.hasPublicPoint)
          {
            'id': 'place:${place.id}',
            'title': place.name,
            'latitude': place.location.latitude,
            'longitude': place.location.longitude,
          },
      for (final entity in widget.entities)
        {
          'id': entity.id,
          'title': entity.kind == MapEntityKind.person
              ? 'Approximate area · ${entity.title}'
              : entity.title,
          'latitude': entity.latitude,
          'longitude': entity.longitude,
        },
    ];
    try {
      final handle = await _mountAMap(
        id.toJS,
        jsonEncode({
          'key': _key,
          'serviceHost': _serviceHost,
          'latitude': viewport.latitude,
          'longitude': viewport.longitude,
          'zoom': viewport.defaultZoom,
          'markers': markers,
        }).toJS,
        _onSelectCallback!,
      ).toDart;
      if (!mounted) {
        _destroyAMap(handle);
      } else {
        _handle = handle;
      }
    } catch (_) {
      if (mounted) setState(() => _failed = true);
    }
  }

  @override
  void dispose() {
    if (_handle case final handle?) _destroyAMap(handle);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final host = Uri.tryParse(_serviceHost);
    if (_key.isEmpty ||
        host == null ||
        !(host.scheme == 'https' ||
            (host.scheme == 'http' &&
                (host.host == 'localhost' || host.host == '127.0.0.1'))) ||
        !host.path.endsWith('/_AMapService')) {
      return const Center(child: Text('高德地图尚未配置，请使用地点列表。'));
    }
    return ClipRRect(
      borderRadius: BorderRadius.circular(widget.fullBleed ? 0 : 18),
      child: SizedBox(
        height: widget.fullBleed ? double.infinity : 430,
        child: Stack(
          children: [
            Positioned.fill(
              child: HtmlElementView.fromTagName(
                tagName: 'div',
                onElementCreated: _created,
              ),
            ),
            if (_failed)
              const Align(
                alignment: Alignment.topCenter,
                child: Card(
                  child: Padding(
                    padding: EdgeInsets.all(10),
                    child: Text('高德底图暂不可用，请使用地点列表。'),
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}
