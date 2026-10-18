import 'dart:async';
import 'dart:convert';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';

import 'map_navigation_target.dart';

/// A local map preference, separate from a session, GPS or public location.
/// Only an explicit destination ID is retained; no private workspace is stored.
abstract interface class ExplorationCityStore {
  Future<String?> readCityID();
  Future<void> writeCityID(String cityID);
}

/// Optional extension; existing City-only stores keep their original contract.
abstract interface class ExplorationCameraStore {
  Future<MapCameraSnapshot?> readCamera(String cityID);
  Future<void> writeCamera(MapNavigationTarget target);
}

class SecureExplorationCityStore
    implements ExplorationCityStore, ExplorationCameraStore {
  const SecureExplorationCityStore({
    FlutterSecureStorage storage = const FlutterSecureStorage(),
  }) : _storage = storage;

  static const storageKey = 'birdtie.exploration.city.v1';
  static const cameraSchema = 'birdtie.exploration.camera.v1';
  static Future<void>? _writeTail;
  final FlutterSecureStorage _storage;

  static bool validCityID(String value) =>
      value.isNotEmpty &&
      value.length <= 160 &&
      value == value.trim() &&
      !value.runes.any((r) => r < 32 || r == 127);

  @override
  Future<String?> readCityID() async {
    final raw = await _storage.read(key: storageKey);
    if (raw == null) return null;
    try {
      final value = jsonDecode(raw);
      if (value is! Map<String, dynamic> ||
          value.length != 2 ||
          value['schema'] != storageKey ||
          value['cityId'] is! String) {
        return null;
      }
      final cityID = value['cityId'] as String;
      return validCityID(cityID) ? cityID : null;
    } on FormatException {
      return null;
    }
  }

  @override
  Future<void> writeCityID(String cityID) async {
    if (!validCityID(cityID)) throw ArgumentError('Invalid exploration city');
    await _serialize(
      () => _storage.write(
        key: storageKey,
        value: jsonEncode({'schema': storageKey, 'cityId': cityID}),
      ),
    );
  }

  static String cameraKey(String cityID) =>
      '$cameraSchema:${base64Url.encode(utf8.encode(cityID)).replaceAll('=', '')}';

  @override
  Future<MapCameraSnapshot?> readCamera(String cityID) async {
    if (!validCityID(cityID)) return null;
    final raw = await _storage.read(key: cameraKey(cityID));
    if (raw == null || raw.length > 4096) return null;
    try {
      final value = jsonDecode(raw);
      if (value is! Map ||
          value.length != 3 ||
          value['schema'] != cameraSchema ||
          value['cityId'] != cityID)
        return null;
      return MapCameraSnapshot.fromJson(value['camera']);
    } on FormatException {
      return null;
    }
  }

  @override
  Future<void> writeCamera(MapNavigationTarget target) async {
    if (!target.canPersistCamera || !validCityID(target.catalogCityID!)) {
      throw ArgumentError('Only verified catalog City cameras can be retained');
    }
    final cityID = target.catalogCityID!;
    // No source entity, owner, token, grant, query, provider result or task is
    // stored. Temporary geocoder sources cannot be upgraded by a matching ID.
    await _serialize(
      () => _storage.write(
        key: cameraKey(cityID),
        value: jsonEncode({
          'schema': cameraSchema,
          'cityId': cityID,
          'camera': target.camera.toJson(),
        }),
      ),
    );
  }

  static Future<void> _serialize(Future<void> Function() write) async {
    while (_writeTail != null) {
      await _writeTail;
    }
    final finished = Completer<void>();
    _writeTail = finished.future;
    try {
      await write();
    } finally {
      _writeTail = null;
      finished.complete();
    }
  }
}
