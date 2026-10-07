import 'dart:convert';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'supplier_profile_api.dart';

class PublicMoment {
  const PublicMoment({
    required this.id,
    required this.title,
    required this.body,
    required this.placeID,
    required this.placeName,
    required this.cityID,
    required this.revision,
    required this.publishedAt,
  });
  final String id, title, body, placeID, placeName, cityID;
  final int revision;
  final DateTime publishedAt;
  static final uuid = RegExp(
    r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
  );
  factory PublicMoment.fromJson(Map<String, dynamic> d, String expectedID) {
    const keys = {
      'schemaVersion',
      'id',
      'title',
      'body',
      'placeId',
      'placeName',
      'cityId',
      'revision',
      'publishedAt',
    };
    if (d.length != keys.length ||
        d.keys.toSet().difference(keys).isNotEmpty ||
        d['schemaVersion'] != 'public-moment-v1' ||
        d['id'] != expectedID ||
        !uuid.hasMatch(expectedID) ||
        d['placeId'] is! String ||
        !uuid.hasMatch(d['placeId']) ||
        d['revision'] is! int ||
        d['revision'] < 2) {
      throw const FormatException('公开动态格式不符');
    }
    for (final entry in {
      'title': 160,
      'body': 5000,
      'placeName': 640,
      'cityId': 80,
    }.entries) {
      final value = d[entry.key];
      if (value is! String ||
          (entry.key != 'body' && value.trim().isEmpty) ||
          utf8.encode(value).length > entry.value) {
        throw const FormatException('公开动态字段不符');
      }
    }
    late final DateTime publishedAt;
    try {
      publishedAt = supplierStamp(d['publishedAt']);
    } on Exception {
      throw const FormatException('公开日期不符');
    }
    return PublicMoment(
      id: expectedID,
      title: d['title'],
      body: d['body'],
      placeID: d['placeId'],
      placeName: d['placeName'],
      cityID: d['cityId'],
      revision: d['revision'],
      publishedAt: publishedAt,
    );
  }
}

class PublicMomentApi {
  PublicMomentApi({
    required this.authorizationHeader,
    this.apiBaseUrl,
    http.Client? client,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null;
  final String? Function() authorizationHeader;
  final String? apiBaseUrl;
  final http.Client _client;
  final bool _ownsClient;
  Future<PublicMoment> read(String id) async {
    if (!PublicMoment.uuid.hasMatch(id)) throw const FormatException('动态标识不符');
    final base = (apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl).replaceFirst(
      RegExp(r'/$'),
      '',
    );
    if (base.isEmpty) throw StateError('公开动态服务尚不可用');
    final token = authorizationHeader();
    final response = await _client
        .get(
          Uri.parse('$base/v1/moments/$id'),
          headers: {'Authorization': ?token},
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw StateError('公开动态暂不可查看');
    final envelope = jsonDecode(utf8.decode(response.bodyBytes));
    if (envelope is! Map<String, dynamic> ||
        envelope['data'] is! Map<String, dynamic>) {
      throw const FormatException('公开动态响应不符');
    }
    return PublicMoment.fromJson(envelope['data'] as Map<String, dynamic>, id);
  }

  void dispose() {
    if (_ownsClient) _client.close();
  }
}
