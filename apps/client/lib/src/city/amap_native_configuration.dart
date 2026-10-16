import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:x_amap_base/x_amap_base.dart';

enum AMapNativeKeySource { dartDefine, androidManifest, missing, unavailable }

/// Configuration presence is distinct from SDK authentication. Android's
/// manifest key stays native; only an explicitly supplied Dart define crosses
/// the plugin API. This check performs no SDK initialization or network I/O.
class AMapNativeConfiguration {
  const AMapNativeConfiguration._(this.source, this.apiKey);

  static const channel = MethodChannel('birdtie/map_configuration');
  final AMapNativeKeySource source;
  final AMapApiKey? apiKey;

  bool get configured =>
      source == AMapNativeKeySource.dartDefine ||
      source == AMapNativeKeySource.androidManifest;

  static Future<AMapNativeConfiguration> inspect({
    TargetPlatform? platform,
    String androidKey = const String.fromEnvironment(
      'BIRDTIE_AMAP_ANDROID_KEY',
    ),
    String iosKey = const String.fromEnvironment('BIRDTIE_AMAP_IOS_KEY'),
    Duration timeout = const Duration(seconds: 5),
  }) async {
    final target = platform ?? defaultTargetPlatform;
    if (target == TargetPlatform.android && androidKey.trim().isNotEmpty) {
      return AMapNativeConfiguration._(
        AMapNativeKeySource.dartDefine,
        AMapApiKey(androidKey: androidKey.trim()),
      );
    }
    if (target == TargetPlatform.iOS && iosKey.trim().isNotEmpty) {
      return AMapNativeConfiguration._(
        AMapNativeKeySource.dartDefine,
        AMapApiKey(iosKey: iosKey.trim()),
      );
    }
    if (target != TargetPlatform.android) {
      return const AMapNativeConfiguration._(AMapNativeKeySource.missing, null);
    }
    try {
      final present = await channel
          .invokeMethod<bool>('hasAndroidAMapKey')
          .timeout(timeout);
      return AMapNativeConfiguration._(
        present == true
            ? AMapNativeKeySource.androidManifest
            : present == false
            ? AMapNativeKeySource.missing
            : AMapNativeKeySource.unavailable,
        null,
      );
    } catch (_) {
      // Never surface a native exception message, which may contain metadata.
      return const AMapNativeConfiguration._(
        AMapNativeKeySource.unavailable,
        null,
      );
    }
  }
}
