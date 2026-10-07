import 'dart:convert';

import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';

class ActivityOrganizer {
  const ActivityOrganizer({
    required this.type,
    required this.id,
    required this.name,
  });

  final String type;
  final String id;
  final String name;

  String get label => switch (type) {
    'PERSON' => '我自己',
    'COMMUNITY' => '社群：$name',
    'ORGANIZATION' => '组织：$name',
    'BUSINESS' => '商家：$name',
    _ => name,
  };

  Map<String, String> toJson() => {'type': type, 'id': id};

  factory ActivityOrganizer.fromJson(Map<String, dynamic> json) =>
      ActivityOrganizer(
        type: json['type'] as String? ?? '',
        id: json['id'] as String? ?? '',
        name: json['name'] as String? ?? '',
      );
}

class OrganizationActivity {
  const OrganizationActivity({
    required this.id,
    required this.title,
    required this.summary,
    required this.description,
    required this.cityId,
    required this.placeId,
    this.modality = 'unspecified',
    this.physicalPlaceStatus = 'unknown',
    this.venuePlaceId,
    required this.startsAt,
    required this.endsAt,
    required this.categoryCode,
    required this.capacity,
    required this.priceMinor,
    required this.currency,
    required this.eligibility,
    required this.languageCode,
    required this.visibility,
    required this.publicationStatus,
    required this.cancelledAt,
    this.organizer,
  });

  final String id;
  final String title;
  final String summary;
  final String description;
  final String cityId;
  final String? placeId;
  final String modality;
  final String physicalPlaceStatus;
  final String? venuePlaceId;
  final DateTime startsAt;
  final DateTime endsAt;
  final String? categoryCode;
  final int? capacity;
  final int priceMinor;
  final String? currency;
  final String eligibility;
  final String? languageCode;
  final String visibility;
  final String publicationStatus;
  final DateTime? cancelledAt;
  final ActivityOrganizer? organizer;

  factory OrganizationActivity.fromJson(
    Map<String, dynamic> data,
  ) => OrganizationActivity(
    id: data['id'] as String,
    title: data['title'] as String,
    summary: data['summary'] as String? ?? '',
    description: data['description'] as String? ?? '',
    cityId: data['cityId'] as String,
    placeId: data['placeId'] as String?,
    modality: data['modality'] as String? ?? 'unspecified',
    physicalPlaceStatus: data['physicalPlaceStatus'] as String? ?? 'unknown',
    venuePlaceId: data['venuePlaceId'] as String?,
    startsAt: DateTime.parse(data['startsAt'] as String),
    endsAt: DateTime.parse(data['endsAt'] as String),
    categoryCode: data['categoryCode'] as String?,
    capacity: data['capacity'] as int?,
    priceMinor: data['priceMinor'] as int? ?? 0,
    currency: data['currency'] as String?,
    eligibility: data['eligibility'] as String? ?? '',
    languageCode: data['languageCode'] as String?,
    visibility: data['visibility'] as String? ?? 'public',
    publicationStatus: data['publicationStatus'] as String,
    cancelledAt: data['cancelledAt'] == null
        ? null
        : DateTime.parse(data['cancelledAt'] as String),
    organizer: data['organizer'] is Map<String, dynamic>
        ? ActivityOrganizer.fromJson(data['organizer'] as Map<String, dynamic>)
        : null,
  );
}

class OrganizationActivityException implements Exception {
  const OrganizationActivityException(this.code, this.status);
  final String code;
  final int status;

  String get chineseMessage => switch (code) {
    'organization_admin_required' => '只有组织所有者或管理员可以管理活动。',
    'activity_state_conflict' => '活动状态已变化，请刷新后重试。',
    'title_length_invalid' => '活动标题需为 2 至 160 个字。',
    'schedule_invalid' => '结束时间必须晚于开始时间。',
    'schedule_too_long' => '活动时长不能超过 7 天。',
    'timeZone_city_mismatch' => '活动时间必须使用当前城市的时区。',
    'place_city_mismatch' => '所选地点不属于当前城市。',
    'activity_location_invalid' => '请选择线下地点、地点待定或线上活动。',
    'venue_unavailable' => '所选活动场地尚未公开或已过期。',
    'capacity_invalid' => '人数上限必须在 1 至 100000 之间。',
    'invalid_body' => '提交内容格式有误。',
    _ => '操作失败，请检查内容或稍后重试。',
  };

  @override
  String toString() => chineseMessage;
}

class OrganizationActivityApi {
  OrganizationActivityApi({
    required this.authorizationHeader,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;

  final String? Function() authorizationHeader;
  final http.Client _client;
  final bool _ownsClient;
  final String _base;

  Uri _uri(String path) =>
      Uri.parse('${_base.replaceFirst(RegExp(r'/$'), '')}$path');

  Map<String, String> get _headers {
    final headers = <String, String>{'Content-Type': 'application/json'};
    final token = authorizationHeader();
    if (token != null) headers['Authorization'] = token;
    return headers;
  }

  Future<Map<String, dynamic>> _read(
    http.Response response,
    int expected,
  ) async {
    final body =
        jsonDecode(utf8.decode(response.bodyBytes)) as Map<String, dynamic>;
    if (response.statusCode != expected) {
      throw OrganizationActivityException(
        (body['error'] as Map<String, dynamic>?)?['code'] as String? ??
            'request_failed',
        response.statusCode,
      );
    }
    return body;
  }

  Future<List<OrganizationActivity>> list(String organizationId) async {
    final response = await _client
        .get(
          _uri('/v1/me/organizations/$organizationId/activities'),
          headers: _headers,
        )
        .timeout(const Duration(seconds: 12));
    final body = await _read(response, 200);
    return [
      for (final raw in body['data'] as List<dynamic>)
        OrganizationActivity.fromJson(raw as Map<String, dynamic>),
    ];
  }

  Future<OrganizationActivity> save(
    String organizationId,
    Map<String, dynamic> input, {
    String? activityId,
  }) async {
    final path = '/v1/me/organizations/$organizationId/activities';
    final response = activityId == null
        ? await _client
              .post(_uri(path), headers: _headers, body: jsonEncode(input))
              .timeout(const Duration(seconds: 12))
        : await _client
              .put(
                _uri('$path/$activityId'),
                headers: _headers,
                body: jsonEncode(input),
              )
              .timeout(const Duration(seconds: 12));
    final body = await _read(response, activityId == null ? 201 : 200);
    return OrganizationActivity.fromJson(body['data'] as Map<String, dynamic>);
  }

  Future<OrganizationActivity> publish(
    String organizationId,
    String activityId,
  ) => _changeState(organizationId, activityId, 'publish');

  Future<OrganizationActivity> cancel(
    String organizationId,
    String activityId,
  ) => _changeState(organizationId, activityId, 'cancel');

  Future<OrganizationActivity> _changeState(
    String organizationId,
    String activityId,
    String action,
  ) async {
    final response = await _client
        .post(
          _uri(
            '/v1/me/organizations/$organizationId/activities/$activityId/$action',
          ),
          headers: _headers,
        )
        .timeout(const Duration(seconds: 12));
    final body = await _read(response, 200);
    return OrganizationActivity.fromJson(body['data'] as Map<String, dynamic>);
  }

  void dispose() {
    if (_ownsClient) _client.close();
  }

  Future<List<OrganizationActivity>> listMine() async {
    final response = await _client
        .get(_uri('/v1/me/activities'), headers: _headers)
        .timeout(const Duration(seconds: 12));
    final body = await _read(response, 200);
    return [
      for (final raw in body['data'] as List<dynamic>)
        OrganizationActivity.fromJson(raw as Map<String, dynamic>),
    ];
  }

  Future<List<ActivityOrganizer>> managedBusinessOrganizers() async {
    final response = await _client
        .get(_uri('/v1/me/activity-organizers/businesses'), headers: _headers)
        .timeout(const Duration(seconds: 12));
    // Older API deployments have no Business principal route. Preserve the
    // existing Person/Community/Organization editor during staged rollout.
    if (response.statusCode == 404) return const [];
    final body = await _read(response, 200);
    return [
      for (final raw in body['data'] as List<dynamic>)
        ActivityOrganizer.fromJson(raw as Map<String, dynamic>),
    ];
  }

  Future<OrganizationActivity> saveAs(
    ActivityOrganizer organizer,
    Map<String, dynamic> input, {
    String? activityId,
  }) async {
    final path = activityId == null
        ? '/v1/me/activities'
        : '/v1/me/activities/$activityId';
    final payload = {...input, 'organizer': organizer.toJson()};
    final response = activityId == null
        ? await _client
              .post(_uri(path), headers: _headers, body: jsonEncode(payload))
              .timeout(const Duration(seconds: 12))
        : await _client
              .put(_uri(path), headers: _headers, body: jsonEncode(payload))
              .timeout(const Duration(seconds: 12));
    final body = await _read(response, activityId == null ? 201 : 200);
    return OrganizationActivity.fromJson(body['data'] as Map<String, dynamic>);
  }

  Future<OrganizationActivity> publishMine(String activityId) =>
      _changeMineState(activityId, 'publish');

  Future<OrganizationActivity> cancelMine(String activityId) =>
      _changeMineState(activityId, 'cancel');

  Future<OrganizationActivity> _changeMineState(
    String activityId,
    String action,
  ) async {
    final response = await _client
        .post(_uri('/v1/me/activities/$activityId/$action'), headers: _headers)
        .timeout(const Duration(seconds: 12));
    final body = await _read(response, 200);
    return OrganizationActivity.fromJson(body['data'] as Map<String, dynamic>);
  }
}
