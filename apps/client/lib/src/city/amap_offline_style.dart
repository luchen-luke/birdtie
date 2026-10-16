import 'dart:typed_data';

import 'package:amap_map/amap_map.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/services.dart';

/// The user's Android-only export is local data, independent of account and
/// viewport. Share its successful load across maps; failed attempts can retry.
class AMapOfflineStyleLoader {
  AMapOfflineStyleLoader({AssetBundle? bundle})
    : _bundle = bundle ?? rootBundle;

  static const assetDirectory = 'assets/map_styles/civu_android';
  static const _digests = {
    'style.data':
        '2e58328b9a676e23705f8a11ded22f9eb5d5482d1c2d1d00b9d67494ec25c8d1',
    'style_extra.data':
        'a2055f0e095badfbbb402eb5e61b49ec5a7ff71795be6600a6a55a92bb9e034b',
  };
  final AssetBundle _bundle;
  Future<CustomStyleOptions>? _pending;

  Future<CustomStyleOptions> load() =>
      _pending ??= _read().catchError((Object error, StackTrace stack) {
        _pending = null;
        Error.throwWithStackTrace(error, stack);
      });

  Future<CustomStyleOptions> _read() async {
    final files = <String, Uint8List>{};
    for (final entry in _digests.entries) {
      final data = await _bundle.load('$assetDirectory/${entry.key}');
      final bytes = data.buffer.asUint8List(
        data.offsetInBytes,
        data.lengthInBytes,
      );
      if (sha256.convert(bytes).toString() != entry.value) {
        throw StateError('Android map style asset verification failed.');
      }
      files[entry.key] = bytes.asUnmodifiableView();
    }
    return CustomStyleOptions(
      true,
      styleData: files['style.data'],
      styleExtraData: files['style_extra.data'],
    );
  }
}

final androidAMapOfflineStyle = AMapOfflineStyleLoader();
