import 'dart:convert';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'supplier_profile_api.dart';
import 'public_moment_photo.dart';
import 'human_causal_control.dart';

/// Only the minimal explicitly public author projection; no profile inference.
class PublicMomentAuthorProfile {
  const PublicMomentAuthorProfile({
    required this.accountID,
    required this.displayName,
  });
  final String accountID, displayName;

  static PublicMomentAuthorProfile? fromPublicPair(Map<String, dynamic> data) {
    if (!data.containsKey('authorId') ||
        !data.containsKey('authorPublicProfile')) {
      throw const FormatException('公开作者字段不完整');
    }
    final id = data['authorId'], profile = data['authorPublicProfile'];
    if (id == null && profile == null) return null;
    if (id is! String ||
        !PublicMoment.uuid.hasMatch(id) ||
        profile is! Map<String, dynamic> ||
        profile.length != 2 ||
        profile.keys.toSet().difference({
          'accountId',
          'displayName',
        }).isNotEmpty ||
        profile['accountId'] != id ||
        profile['displayName'] is! String ||
        (profile['displayName'] as String).trim().isEmpty ||
        utf8.encode(profile['displayName'] as String).length > 640) {
      throw const FormatException('公开作者格式不符');
    }
    return PublicMomentAuthorProfile(
      accountID: id,
      displayName: profile['displayName'] as String,
    );
  }
}

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
    this.authorPublicProfile,
    this.observedAt,
    this.validUntil,
    this.photo,
  });
  final String id, title, body, placeID, placeName, cityID;
  final int revision;
  final DateTime publishedAt;
  final PublicMomentAuthorProfile? authorPublicProfile;
  final DateTime? observedAt, validUntil;
  final PublicMomentPhotoMetadata? photo;
  bool get current =>
      validUntil == null || DateTime.now().toUtc().isBefore(validUntil!);
  String? get authorID => authorPublicProfile?.accountID;
  static final uuid = RegExp(
    r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
  );
  factory PublicMoment.fromJson(
    Map<String, dynamic> d,
    String expectedID, {
    bool includePublicAuthor = false,
  }) {
    final media = d['schemaVersion'] == 'public-moment-v3';
    final keys = {
      'schemaVersion',
      'id',
      'title',
      'body',
      'placeId',
      'placeName',
      'cityId',
      'revision',
      'publishedAt',
      if (includePublicAuthor || media) ...{'authorId', 'authorPublicProfile'},
      if (media) ...{'observedAt', 'validUntil', 'photo'},
    };
    if (d.length != keys.length ||
        d.keys.toSet().difference(keys).isNotEmpty ||
        d['schemaVersion'] !=
            (media
                ? 'public-moment-v3'
                : includePublicAuthor
                ? 'public-moment-v2'
                : 'public-moment-v1') ||
        d['id'] != expectedID ||
        !uuid.hasMatch(expectedID) ||
        d['placeId'] is! String ||
        !uuid.hasMatch(d['placeId']) ||
        d['revision'] is! int ||
        d['revision'] < 2 ||
        (media && !humanControlVersion(d['revision'], zero: false))) {
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
    DateTime? observed, until;
    PublicMomentPhotoMetadata? photo;
    if (media) {
      observed = humanControlTime(d['observedAt']);
      until = humanControlTime(d['validUntil']);
      final now = DateTime.now().toUtc();
      if (observed.isAfter(now.add(const Duration(seconds: 2))) ||
          !until.isAfter(observed) ||
          until.isAfter(observed.add(const Duration(seconds: 60))))
        throw const FormatException('公开动态来源期限不符');
      if (!includePublicAuthor &&
          (d['authorId'] != null || d['authorPublicProfile'] != null))
        throw const FormatException('未请求的公开作者字段不符');
      PublicMomentAuthorProfile.fromPublicPair(d);
      if (d['photo'] != null)
        photo = PublicMomentPhotoMetadata.decode(
          d['photo'],
          expectedID,
          observed,
          expectedMomentRevision: d['revision'],
          validUntil: until,
        );
    }
    return PublicMoment(
      observedAt: observed,
      validUntil: until,
      photo: photo,
      id: expectedID,
      title: d['title'],
      body: d['body'],
      placeID: d['placeId'],
      placeName: d['placeName'],
      cityID: d['cityId'],
      revision: d['revision'],
      publishedAt: publishedAt,
      authorPublicProfile: includePublicAuthor
          ? PublicMomentAuthorProfile.fromPublicPair(d)
          : null,
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
  http.Client get borrowedClient => _client;
  String get baseUrl => apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  Future<PublicMoment> read(
    String id, {
    bool includePublicAuthor = false,
  }) async {
    if (!PublicMoment.uuid.hasMatch(id)) throw const FormatException('动态标识不符');
    final base = (apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl).replaceFirst(
      RegExp(r'/$'),
      '',
    );
    if (base.isEmpty) throw StateError('公开动态服务尚不可用');
    final token = authorizationHeader();
    final response = await _client
        .get(
          Uri.parse('$base/v1/moments/$id').replace(
            queryParameters: includePublicAuthor
                ? {'include': 'public-author'}
                : null,
          ),
          headers: {
            'Authorization': ?token,
            'X-Birdtie-Public-Moment-Media': 'photo-v1',
          },
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw StateError('公开动态暂不可查看');
    if (authorizationHeader() != token) throw StateError('公开动态原会话已更新');
    final envelope = jsonDecode(utf8.decode(response.bodyBytes));
    if (envelope is! Map<String, dynamic> ||
        envelope['data'] is! Map<String, dynamic>) {
      throw const FormatException('公开动态响应不符');
    }
    return PublicMoment.fromJson(
      envelope['data'] as Map<String, dynamic>,
      id,
      includePublicAuthor: includePublicAuthor,
    );
  }

  void dispose() {
    if (_ownsClient) _client.close();
  }
}
