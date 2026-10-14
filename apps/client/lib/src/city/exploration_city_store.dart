import 'dart:async';
import 'dart:convert';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// A local map preference, separate from a session, GPS or public location.
/// Only an explicit destination ID is retained; no private workspace is stored.
abstract interface class ExplorationCityStore {
  Future<String?> readCityID();
  Future<void> writeCityID(String cityID);
}

class SecureExplorationCityStore implements ExplorationCityStore {
  const SecureExplorationCityStore({
    FlutterSecureStorage storage = const FlutterSecureStorage(),
  }) : _storage = storage;

  static const storageKey = 'birdtie.exploration.city.v1';
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
    while (_writeTail != null) {
      await _writeTail;
    }
    final finished = Completer<void>();
    _writeTail = finished.future;
    try {
      await _storage.write(
        key: storageKey,
        value: jsonEncode({'schema': storageKey, 'cityId': cityID}),
      );
    } finally {
      _writeTail = null;
      finished.complete();
    }
  }
}
