import 'dart:async';
import 'dart:collection';
import 'dart:convert';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:http/http.dart' as http;

import '../config/birdtie_environment.dart';
import '../content/moment_image_header.dart';

/// A reviewed system destination image, independent of private entity covers.
class CityArtworkMetadata {
  const CityArtworkMetadata({
    required this.cityID,
    required this.version,
    required this.descriptorPath,
  });
  final String cityID, version, descriptorPath;
  String get basePath =>
      '/v1/cities/${Uri.encodeComponent(cityID)}/artwork/$version';
  String get key => '$cityID:$version';

  static CityArtworkMetadata? optional(Object? raw, String cityID) {
    if (raw == null) return null;
    // A broken optional illustration must never remove a selectable city.
    if (raw is! Map<String, dynamic> ||
        raw['version'] is! String ||
        !RegExp(r'^[0-9a-f]{64}$').hasMatch(raw['version']))
      return null;
    final value = CityArtworkMetadata(
      cityID: cityID,
      version: raw['version'],
      descriptorPath: raw['descriptorPath'] is String
          ? raw['descriptorPath']
          : '',
    );
    return value.descriptorPath == '${value.basePath}/descriptor'
        ? value
        : null;
  }
}

/// Exact source identity, never a name/coordinate guess. This is Civu's
/// original souvenir, verified against the supplied provenance manifest.
String? destinationLocalArtwork(String cityID, {String? remoteVersion}) {
  final asset = switch (cityID) {
    'civu-geo-7a536573-67c6-42f0-9ede-c423fb5dba00' => (
      'assets/destinations/qingdao-d2988c88.png',
      '9a62fcce9f889e12fa192cb58ed0bb010089e7b59b6595070949be9bfa0182f0',
    ),
    'civu-geo-bbffd2af-68b4-49ee-bb01-5e5a7673aa9b' => (
      'assets/destinations/jinan-69b9d3d6.png',
      '98e9115e25a8d3b7b80c517fab9819457277fcaac31045322e6d3792e3125fd9',
    ),
    'civu-geo-66920ecb-9717-42bb-8320-a43a1d2851d2' => (
      'assets/destinations/edinburgh.png',
      '',
    ),
    _ => null,
  };
  // A known older souvenir never replaces an unknown/newer server version.
  return asset != null && (remoteVersion == null || remoteVersion == asset.$2)
      ? asset.$1
      : null;
}

/// Only visible thumbnails enter this queue. Two reads and a small versioned
/// LRU bound transfer/decode pressure independently of catalog size.
class DestinationArtworkLoader {
  DestinationArtworkLoader({http.Client? client, String? apiBaseUrl})
    : _client = client ?? http.Client(),
      _ownsClient = client == null,
      _base = Uri.tryParse(apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl);
  final http.Client _client;
  final bool _ownsClient;
  final Uri? _base;
  final _cache = LinkedHashMap<String, Uint8List>();
  final _pending = <String, Future<Uint8List>>{};
  final _waiting = Queue<Completer<void>>();
  int _active = 0, _cacheBytes = 0;
  bool _closed = false;

  Uri _uri(String path) {
    final base = _base;
    if (base == null ||
        !{'http', 'https'}.contains(base.scheme) ||
        base.host.isEmpty ||
        base.userInfo.isNotEmpty ||
        !path.startsWith('/v1/cities/')) {
      throw const FormatException('目的地图服务不可用');
    }
    return base.replace(path: path, query: '', fragment: '');
  }

  Future<Uint8List> load(CityArtworkMetadata metadata) {
    if (_closed) return Future.error(StateError('artwork loader closed'));
    if (metadata.descriptorPath != '${metadata.basePath}/descriptor' ||
        !RegExp(r'^[0-9a-f]{64}$').hasMatch(metadata.version)) {
      return Future.error(const FormatException('目的地图来源不符'));
    }
    final cached = _cache.remove(metadata.key);
    if (cached != null) {
      _cache[metadata.key] = cached;
      return Future.value(cached);
    }
    return _pending.putIfAbsent(metadata.key, () async {
      try {
        return await _read(metadata);
      } finally {
        _pending.remove(metadata.key);
      }
    });
  }

  Future<http.Response> _get(String path, int maxBytes) async {
    final request = http.Request('GET', _uri(path))..followRedirects = false;
    final response = await _client
        .send(request)
        .timeout(const Duration(seconds: 10));
    if (response.statusCode != 200 ||
        (response.contentLength != null &&
            response.contentLength! > maxBytes)) {
      await response.stream.listen((_) {}).cancel();
      throw const FormatException('目的地图暂不可用');
    }
    final body = BytesBuilder(copy: false);
    await for (final chunk in response.stream.timeout(
      const Duration(seconds: 10),
    )) {
      if (_closed || body.length + chunk.length > maxBytes) {
        throw const FormatException('目的地图读取已结束');
      }
      body.add(chunk);
    }
    return http.Response.bytes(
      body.takeBytes(),
      response.statusCode,
      headers: response.headers,
    );
  }

  Future<Uint8List> _read(CityArtworkMetadata metadata) async {
    if (_active >= 2) {
      final slot = Completer<void>();
      _waiting.add(slot);
      await slot.future;
    } else {
      _active++;
    }
    try {
      if (_closed) throw StateError('artwork loader closed');
      final descriptor = await _get(metadata.descriptorPath, 8192);
      final value = jsonDecode(utf8.decode(descriptor.bodyBytes));
      if (value is! Map<String, dynamic>)
        throw const FormatException('目的地图格式不符');
      // Both the normal API data envelope and the descriptor object are read
      // without accepting a third-party URL, different city or new version.
      final raw = value['data'] is Map<String, dynamic> ? value['data'] : value;
      if (raw['cityId'] != metadata.cityID ||
          raw['version'] != metadata.version ||
          raw['contentPath'] != '${metadata.basePath}/content' ||
          raw['contentType'] != 'image/png' ||
          raw['width'] is! int ||
          raw['height'] is! int ||
          raw['width'] < 1 ||
          raw['height'] < 1 ||
          raw['width'] > 324 ||
          raw['height'] > 324 ||
          raw['byteCount'] is! int ||
          raw['byteCount'] < 1 ||
          raw['byteCount'] > 512 * 1024 ||
          raw['sha256'] is! String ||
          !RegExp(r'^[0-9a-f]{64}$').hasMatch(raw['sha256'])) {
        throw const FormatException('目的地图来源或尺寸不符');
      }
      final image = await _get(raw['contentPath'], raw['byteCount']);
      final bytes = image.bodyBytes;
      final header = MomentImageHeader.inspect(bytes);
      if (image.headers['content-type']?.split(';').first != 'image/png' ||
          bytes.length != raw['byteCount'] ||
          sha256.convert(bytes).toString() != raw['sha256'] ||
          header.width != raw['width'] ||
          header.height != raw['height']) {
        throw const FormatException('目的地图校验失败');
      }
      if (_closed) throw StateError('artwork loader closed');
      _cache[metadata.key] = bytes;
      _cacheBytes += bytes.length;
      while (_cache.length > 24 || _cacheBytes > 4 * 1024 * 1024) {
        final first = _cache.keys.first;
        _cacheBytes -= _cache.remove(first)!.length;
      }
      return bytes;
    } finally {
      if (_waiting.isNotEmpty) {
        _waiting.removeFirst().complete();
      } else {
        _active--;
      }
    }
  }

  void dispose() {
    _closed = true;
    _cache.clear();
    _cacheBytes = 0;
    if (_ownsClient) _client.close();
  }
}
