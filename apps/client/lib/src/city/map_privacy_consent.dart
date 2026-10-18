import 'dart:convert';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

const mapPrivacyPolicyVersion = 'amap-map-search-20261018-v1';
const mapPrivacyPolicyURL = 'https://lbs.amap.com/pages/privacy/';
const mapPrivacyDisclosure =
    '国内地图与地理搜索由高德提供。为提供地图服务，高德会按其隐私权政策处理所需的设备和网络信息；'
    '你主动搜索时，搜索词也会交给高德处理。这不会开启设备定位。'
    '你可以暂不使用该服务，继续选择目录目的地和查看可用内容，也可稍后在地图服务隐私说明中更改选择。';

abstract interface class MapPrivacyConsentStore {
  Future<String?> read();
  Future<void> write(String record);
}

class SecureMapPrivacyConsentStore implements MapPrivacyConsentStore {
  const SecureMapPrivacyConsentStore({
    FlutterSecureStorage storage = const FlutterSecureStorage(),
  }) : _storage = storage;
  static const storageKey = 'birdtie.map-service.privacy.v1';
  final FlutterSecureStorage _storage;
  @override
  Future<String?> read() => _storage.read(key: storageKey);
  @override
  Future<void> write(String record) =>
      _storage.write(key: storageKey, value: record);
}

@visibleForTesting
class MemoryMapPrivacyConsentStore implements MapPrivacyConsentStore {
  MemoryMapPrivacyConsentStore({this.record});
  String? record;
  @override
  Future<String?> read() async => record;
  @override
  Future<void> write(String value) async => record = value;
}

/// Device-local SDK privacy choice, separate from account grants, location
/// permission, map-provider routing and Mapbox's own attribution/telemetry UI.
/// There is no default acceptance or setter inferred from a route/search.
class MapPrivacyConsentController extends ChangeNotifier {
  MapPrivacyConsentController({
    MapPrivacyConsentStore store = const SecureMapPrivacyConsentStore(),
  }) : _store = store;

  final MapPrivacyConsentStore _store;
  Future<void>? _loading;
  bool _loaded = false, _busy = false, _closed = false, _readFailed = false;
  bool _memoryRejected = false;
  bool? _decision;
  int _generation = 0;
  String? _error;
  bool get loaded => _loaded;
  bool get busy => _busy;
  bool get hasDecision =>
      _loaded &&
      !_readFailed &&
      _decision != null &&
      !(_memoryRejected && _decision == true);
  bool get accepted => hasDecision && _decision == true && !_memoryRejected;
  String? get error => _error;
  int get generation => _generation;

  static String get _disclosureSHA =>
      sha256.convert(utf8.encode(mapPrivacyDisclosure)).toString();

  static String _encode(bool accepted) => jsonEncode({
    'schema': SecureMapPrivacyConsentStore.storageKey,
    'policyVersion': mapPrivacyPolicyVersion,
    'policyURL': mapPrivacyPolicyURL,
    'disclosureSHA256': _disclosureSHA,
    'decision': accepted ? 'accepted' : 'rejected',
  });

  static bool? _decode(String? raw) {
    if (raw == null || raw.length > 2048) return null;
    try {
      final v = jsonDecode(raw);
      if (v is! Map ||
          v.length != 5 ||
          v['schema'] != SecureMapPrivacyConsentStore.storageKey ||
          v['policyVersion'] != mapPrivacyPolicyVersion ||
          v['policyURL'] != mapPrivacyPolicyURL ||
          v['disclosureSHA256'] != _disclosureSHA ||
          !const ['accepted', 'rejected'].contains(v['decision']))
        return null;
      return v['decision'] == 'accepted';
    } on FormatException {
      return null;
    }
  }

  Future<void> load() {
    if (_closed || _loaded && !_readFailed) return Future.value();
    if (_loading != null) return _loading!;
    final generation = _generation;
    return _loading = () async {
      try {
        final raw = await _store.read();
        if (_closed || generation != _generation) return;
        _decision = _decode(raw);
        _readFailed = false;
        _error = null;
      } catch (_) {
        if (_closed || generation != _generation) return;
        _decision = null;
        _readFailed = true;
        _error = '暂时无法读取地图服务隐私选择，请重试。';
      } finally {
        if (!_closed && generation == _generation) {
          _loaded = true;
          _loading = null;
          notifyListeners();
        }
      }
    }();
  }

  /// Call only for a user's explicit decision on this version's disclosure.
  /// Acceptance becomes usable after durable write AND exact read-back.
  Future<bool> setDecision(bool accepted) async {
    if (_closed || _busy) return false;
    if (!_loaded || _readFailed) await load();
    if (_closed || _busy || !_loaded || _readFailed) return false;
    final generation = ++_generation;
    _busy = true;
    _decision = null;
    _memoryRejected = !accepted;
    _error = null;
    notifyListeners();
    final record = _encode(accepted);
    try {
      await _store.write(record);
      final saved = await _store.read();
      if (_closed || generation != _generation) return false;
      if (saved != record || _decode(saved) != accepted) {
        throw const FormatException('privacy choice read-back mismatch');
      }
      _decision = accepted;
      _memoryRejected = !accepted;
      _readFailed = false;
      return true;
    } catch (_) {
      if (!_closed && generation == _generation) {
        _decision = null;
        _readFailed = true;
        _error = '未能保存地图服务隐私选择，请重试。';
      }
      return false;
    } finally {
      if (!_closed && generation == _generation) {
        _busy = false;
        notifyListeners();
      }
    }
  }

  @override
  void dispose() {
    if (_closed) return;
    _closed = true;
    ++_generation;
    _decision = null;
    super.dispose();
  }
}
