import 'dart:convert';
import 'dart:js_interop';

import 'package:flutter/material.dart';
import 'package:web/web.dart' as web;

import 'mapbox_style.dart';

@JS('birdtieMapboxMount')
external JSPromise<JSBoolean> _mountMapbox(
  JSString elementID,
  JSString config,
  JSFunction onStatus,
);

@JS('birdtieMapboxUpdate')
external void _updateMapbox(JSString elementID, JSString camera);

@JS('birdtieMapboxDestroy')
external void _destroyMapbox(JSString elementID);

class MapboxWebBasemapView extends StatefulWidget {
  const MapboxWebBasemapView({
    super.key,
    required this.accessToken,
    required this.styleURI,
    required this.cameraJSON,
    this.onReady,
    this.onUnavailable,
  });

  final String accessToken;
  final String styleURI;
  final String cameraJSON;
  final VoidCallback? onReady;
  final VoidCallback? onUnavailable;

  @override
  State<MapboxWebBasemapView> createState() => _MapboxWebBasemapViewState();
}

class _MapboxWebBasemapViewState extends State<MapboxWebBasemapView> {
  static int _nextID = 0;
  web.HTMLDivElement? _element;
  String? _elementID;
  JSFunction? _statusCallback;
  int _generation = 0;

  void _created(Object element) {
    final div = element as web.HTMLDivElement;
    _element = div;
    div.style.width = '100%';
    div.style.height = '100%';
    div.style.pointerEvents = 'none';
    div.setAttribute('aria-hidden', 'true');
    _start();
  }

  void _notify(int generation, bool ready) {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || generation != _generation) return;
      if (ready) {
        widget.onReady?.call();
      } else {
        widget.onUnavailable?.call();
      }
    });
    // A network callback may arrive after all Flutter frames have settled.
    WidgetsBinding.instance.scheduleFrame();
  }

  Future<void> _start() async {
    final element = _element;
    if (!mounted || element == null) return;
    _release();
    final generation = ++_generation;
    final id = 'birdtie-mapbox-${_nextID++}';
    _elementID = id;
    element.id = id;
    element.setAttribute('data-birdtie-mapbox-id', id);
    final callback = ((JSString status) {
      if (mounted && generation == _generation) {
        _notify(generation, status.toDart == 'ready');
      }
    }).toJS;
    _statusCallback = callback;
    try {
      final created = await _mountMapbox(
        id.toJS,
        jsonEncode({
          'accessToken': widget.accessToken,
          'style': widget.styleURI,
          'labelWeightPolicy': mapboxLabelWeightPolicy,
          'camera': jsonDecode(widget.cameraJSON),
        }).toJS,
        _statusCallback!,
      ).toDart;
      if (!mounted || generation != _generation) {
        _destroyMapbox(id.toJS);
        return;
      }
      if (!created.toDart) {
        _notify(generation, false);
      } else {
        // Updates made while the loader was pending use the current camera.
        _updateMapbox(id.toJS, widget.cameraJSON.toJS);
      }
    } catch (_) {
      if (mounted && generation == _generation) _notify(generation, false);
    }
  }

  @override
  void didUpdateWidget(MapboxWebBasemapView oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.accessToken != widget.accessToken ||
        oldWidget.styleURI != widget.styleURI) {
      _start();
    } else if (oldWidget.cameraJSON != widget.cameraJSON) {
      final id = _elementID;
      if (id != null) {
        try {
          _updateMapbox(id.toJS, widget.cameraJSON.toJS);
        } catch (_) {
          _notify(_generation, false);
        }
      }
    }
  }

  void _release() {
    ++_generation;
    final id = _elementID;
    _elementID = null;
    if (id != null) {
      try {
        _destroyMapbox(id.toJS);
      } catch (_) {
        // A missing/failed loader must not prevent route disposal.
      }
    }
    _statusCallback = null;
  }

  @override
  void dispose() {
    _release();
    _element = null;
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => ExcludeSemantics(
    child: IgnorePointer(
      child: HtmlElementView.fromTagName(
        tagName: 'div',
        onElementCreated: _created,
      ),
    ),
  );
}
