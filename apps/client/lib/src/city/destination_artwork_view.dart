import 'dart:typed_data';

import 'package:flutter/material.dart';

import 'destination_artwork.dart';

class DestinationArtworkView extends StatefulWidget {
  const DestinationArtworkView({
    super.key,
    required this.cityID,
    required this.artwork,
    required this.loader,
    this.hasRemoteArtwork = false,
    this.size = 108,
  });
  final String cityID;
  final CityArtworkMetadata? artwork;
  final DestinationArtworkLoader loader;
  final bool hasRemoteArtwork;
  final double size;
  @override
  State<DestinationArtworkView> createState() => _DestinationArtworkViewState();
}

class _DestinationArtworkViewState extends State<DestinationArtworkView> {
  Future<Uint8List>? _image;
  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void didUpdateWidget(DestinationArtworkView old) {
    super.didUpdateWidget(old);
    if (old.artwork?.key != widget.artwork?.key || old.loader != widget.loader)
      _load();
  }

  void _load() {
    _image = widget.artwork == null
        ? null
        : widget.loader.load(widget.artwork!);
  }

  Widget _fallback() {
    if (widget.hasRemoteArtwork && widget.artwork == null) return _neutral();
    final asset = destinationLocalArtwork(
      widget.cityID,
      remoteVersion: widget.artwork?.version,
    );
    return asset == null
        ? _neutral()
        : Image.asset(
            asset,
            fit: BoxFit.contain,
            cacheWidth: (widget.size * MediaQuery.devicePixelRatioOf(context))
                .round()
                .clamp(1, 324),
            errorBuilder: (_, _, _) => _neutral(),
          );
  }

  Widget _neutral() => Icon(
    Icons.place_outlined,
    size: widget.size > 48 ? 32 : 24,
    color: Theme.of(context).colorScheme.onSurfaceVariant,
  );
  @override
  Widget build(BuildContext context) => SizedBox.square(
    dimension: widget.size,
    child: ExcludeSemantics(
      child: _image == null
          ? _fallback()
          : FutureBuilder<Uint8List>(
              key: ValueKey(widget.artwork!.key),
              future: _image,
              builder: (context, result) => result.hasData
                  ? Image.memory(
                      result.data!,
                      fit: BoxFit.contain,
                      gaplessPlayback: true,
                      cacheWidth:
                          (widget.size * MediaQuery.devicePixelRatioOf(context))
                              .round()
                              .clamp(1, 324),
                      errorBuilder: (_, _, _) => _fallback(),
                    )
                  : _fallback(),
            ),
    ),
  );
}
