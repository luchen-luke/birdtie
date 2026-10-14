import 'dart:convert';

import 'package:flutter/material.dart';

import 'mapbox_style.dart';
import 'mapbox_web_basemap_stub.dart'
    if (dart.library.html) 'mapbox_web_basemap_web.dart'
    as platform;

/// FlutterMap owns the camera and all permission-filtered entity overlays.
/// Only the public viewport crosses this basemap bridge.
@immutable
class MapboxWebCamera {
  const MapboxWebCamera({
    required this.latitude,
    required this.longitude,
    required this.zoom,
    this.rotation = 0,
  });

  final double latitude;
  final double longitude;
  final double zoom;
  final double rotation;

  bool get valid =>
      latitude.isFinite &&
      latitude.abs() <= 85.0511287798066 &&
      longitude.isFinite &&
      zoom.isFinite &&
      zoom >= 1 &&
      zoom <= 24 &&
      rotation.isFinite;

  // FlutterMap's Mercator world is 256 * 2^zoom CSS pixels. GL JS uses 512,
  // so equal scale requires one less GL zoom, independently of device DPR.
  double get glZoom => zoom - 1;
  double get glBearing => -rotation;

  Map<String, Object> get bridgeValues => {
    'center': [longitude, latitude],
    'zoom': glZoom,
    'bearing': glBearing,
    'pitch': 0,
  };
}

class MapboxWebBasemap extends StatelessWidget {
  const MapboxWebBasemap({
    super.key,
    required this.accessToken,
    required this.camera,
    this.onReady,
    this.onUnavailable,
  });

  final String accessToken;
  final MapboxWebCamera camera;
  final VoidCallback? onReady;
  final VoidCallback? onUnavailable;

  @override
  Widget build(BuildContext context) {
    if (!camera.valid || !accessToken.startsWith('pk.')) {
      return const SizedBox.expand();
    }
    return platform.MapboxWebBasemapView(
      accessToken: accessToken,
      styleURI: mapboxStyleUri,
      cameraJSON: jsonEncode(camera.bridgeValues),
      onReady: onReady,
      onUnavailable: onUnavailable,
    );
  }
}
