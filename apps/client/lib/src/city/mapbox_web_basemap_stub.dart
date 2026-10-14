import 'package:flutter/material.dart';

/// No browser bridge, token transport or second map instance on native targets.
class MapboxWebBasemapView extends StatelessWidget {
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
  Widget build(BuildContext context) => const SizedBox.expand();
}
