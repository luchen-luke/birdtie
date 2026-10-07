import 'dart:convert';

import 'package:http/http.dart' as http;

import '../city/public_city_controller.dart';
import '../config/birdtie_environment.dart';
import 'business_api.dart';

Uri? supplierHTTPS(String value) {
  final uri = Uri.tryParse(value);
  return uri != null &&
          uri.scheme == 'https' &&
          uri.host.isNotEmpty &&
          uri.userInfo.isEmpty &&
          !uri.hasFragment &&
          value.length <= 2048 &&
          !RegExp(r'[\x00\r\n\t ]').hasMatch(value)
      ? uri
      : null;
}

DateTime supplierStamp(dynamic value) {
  if (value is! String ||
      !RegExp(
        r'^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$',
      ).hasMatch(value)) {
    throw const BusinessApiException(503);
  }
  final parsed = DateTime.tryParse(value);
  if (parsed == null ||
      parsed.year < 1 ||
      parsed.year != int.parse(value.substring(0, 4)) ||
      parsed.month != int.parse(value.substring(5, 7)) ||
      parsed.day != int.parse(value.substring(8, 10)) ||
      parsed.hour != int.parse(value.substring(11, 13)) ||
      parsed.minute != int.parse(value.substring(14, 16)) ||
      parsed.second != int.parse(value.substring(17, 19))) {
    throw const BusinessApiException(503);
  }
  return parsed;
}

String supplierDate(DateTime value) {
  final local = value.toLocal();
  String two(int n) => n.toString().padLeft(2, '0');
  return '${local.year}年${local.month}月${local.day}日 ${two(local.hour)}:${two(local.minute)}（设备当地时间）';
}

class PublicBusinessProfile {
  PublicBusinessProfile._(
    this.id,
    this.name,
    this.description,
    this.profileStatus,
    this.links,
    this.activities,
    this.reviewedAt,
    this.validUntil,
    this.version,
  );
  final String id, name, description, profileStatus;
  final List<String> links;
  final List<PublicActivity> activities;
  final DateTime? reviewedAt, validUntil;
  final int version;
  factory PublicBusinessProfile.fromJson(Map<String, dynamic> j) {
    const keys = {
      'id',
      'name',
      'verificationStatus',
      'profileStatus',
      'description',
      'officialLinks',
      'profileVersion',
      'reviewedAt',
      'validUntil',
      'upcomingActivities',
    };
    if (j.keys.toSet().difference(keys).isNotEmpty ||
        keys.difference(j.keys.toSet()).isNotEmpty ||
        j['id'] is! String ||
        !BusinessApi.validID(j['id'] as String) ||
        j['name'] is! String ||
        (j['name'] as String).trim().isEmpty ||
        j['verificationStatus'] != 'verified' ||
        !const {'verified', 'unpublished'}.contains(j['profileStatus']) ||
        j['description'] is! String ||
        j['profileVersion'] is! int ||
        (j['profileVersion'] as int) < 0 ||
        j['officialLinks'] is! List ||
        (j['officialLinks'] as List).length > 8 ||
        (j['officialLinks'] as List).any(
          (v) => v is! String || supplierHTTPS(v) == null,
        ) ||
        j['upcomingActivities'] is! List ||
        (j['upcomingActivities'] as List).length > 20) {
      throw const BusinessApiException(503);
    }
    final verified = j['profileStatus'] == 'verified';
    final reviewed = j['reviewedAt'] == null
        ? null
        : supplierStamp(j['reviewedAt']);
    final until = j['validUntil'] == null
        ? null
        : supplierStamp(j['validUntil']);
    if (verified &&
            (reviewed == null ||
                until == null ||
                (j['profileVersion'] as int) < 1) ||
        !verified &&
            (reviewed != null ||
                until != null ||
                j['description'] != '' ||
                (j['officialLinks'] as List).isNotEmpty ||
                j['profileVersion'] != 0)) {
      throw const BusinessApiException(503);
    }
    final activities = <PublicActivity>[];
    for (final v in j['upcomingActivities'] as List) {
      if (v is! Map<String, dynamic> ||
          v['visibility'] != 'public' ||
          v['organizer'] is! Map<String, dynamic> ||
          (v['organizer'] as Map)['type'] != 'BUSINESS' ||
          (v['organizer'] as Map)['id'] != j['id']) {
        throw const BusinessApiException(503);
      }
      activities.add(PublicActivity.fromJson(v));
    }
    return PublicBusinessProfile._(
      j['id'] as String,
      j['name'] as String,
      j['description'] as String,
      j['profileStatus'] as String,
      List<String>.unmodifiable((j['officialLinks'] as List).cast<String>()),
      List<PublicActivity>.unmodifiable(activities),
      reviewed,
      until,
      j['profileVersion'] as int,
    );
  }
}

class BusinessPublicationPreview {
  BusinessPublicationPreview._(
    this.version,
    this.name,
    this.description,
    this.links,
    this.reviewedAt,
    this.until,
    this.snapshot,
  );
  final int version;
  final String name, description, snapshot;
  final List<String> links;
  final DateTime reviewedAt, until;
  factory BusinessPublicationPreview.fromJson(Map<String, dynamic> j) {
    if (j['profileVersion'] is! int ||
        (j['profileVersion'] as int) < 1 ||
        j['name'] is! String ||
        (j['name'] as String).trim().isEmpty ||
        j['description'] is! String ||
        j['sourceSnapshot'] is! String ||
        !RegExp(r'^[0-9a-f]{64}$').hasMatch(j['sourceSnapshot'] as String) ||
        j['officialLinks'] is! List ||
        (j['officialLinks'] as List).length > 8 ||
        (j['officialLinks'] as List).any(
          (v) => v is! String || supplierHTTPS(v) == null,
        )) {
      throw const BusinessApiException(503);
    }
    return BusinessPublicationPreview._(
      j['profileVersion'] as int,
      j['name'] as String,
      j['description'] as String,
      List<String>.unmodifiable((j['officialLinks'] as List).cast<String>()),
      supplierStamp(j['reviewedAt']),
      supplierStamp(j['validUntil']),
      j['sourceSnapshot'] as String,
    );
  }
}

class BusinessPublicationState {
  BusinessPublicationState._(this.version, this.status, this.preview);
  final int version;
  final String status;
  final BusinessPublicationPreview? preview;
  factory BusinessPublicationState.fromJson(Map<String, dynamic> j) {
    final p = j['permission'];
    if (p is! Map<String, dynamic> ||
        p['version'] is! int ||
        (p['version'] as int) < 0 ||
        !const {
          'unpublished',
          'active',
          'revoked',
          'source_changed',
        }.contains(j['disclosureStatus']) ||
        j['preview'] != null && j['preview'] is! Map<String, dynamic>) {
      throw const BusinessApiException(503);
    }
    return BusinessPublicationState._(
      p['version'] as int,
      j['disclosureStatus'] as String,
      j['preview'] == null
          ? null
          : BusinessPublicationPreview.fromJson(
              j['preview'] as Map<String, dynamic>,
            ),
    );
  }
}

class SupplierProfileApi {
  SupplierProfileApi({
    required this.authorizationHeader,
    this.workspaceID,
    http.Client? client,
    String? apiBaseUrl,
    this.timeout = const Duration(seconds: 15),
  }) : _client = client ?? http.Client(),
       _owns = client == null,
       _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final String? Function() authorizationHeader;
  final String? Function()? workspaceID;
  final http.Client _client;
  final bool _owns;
  final String _base;
  final Duration timeout;
  void dispose() {
    if (_owns) _client.close();
  }

  String _publicPath(String id) {
    if (!BusinessApi.validID(id)) throw const BusinessApiException(400);
    return '/v1/businesses/$id';
  }

  String _permissionPath(String id) {
    if (!BusinessApi.validID(id)) throw const BusinessApiException(400);
    return '/v1/me/businesses/$id/public-profile/permission';
  }

  Future<Map<String, dynamic>> _request(
    String method,
    String path, {
    Map<String, dynamic>? body,
    bool owner = false,
  }) async {
    final token = authorizationHeader(), workspace = workspaceID?.call();
    final write = method != 'GET';
    if (owner && (token == null || workspace != null)) {
      throw BusinessApiException(token == null ? 401 : 403);
    }
    final base = Uri.tryParse(_base);
    if (base == null ||
        !base.hasAuthority ||
        base.host.isEmpty ||
        base.userInfo.isNotEmpty ||
        base.hasQuery ||
        base.hasFragment ||
        !const {'http', 'https'}.contains(base.scheme)) {
      throw const BusinessApiException(503);
    }
    try {
      final req =
          http.Request(
              method,
              Uri.parse('${_base.replaceFirst(RegExp(r'/$'), '')}$path'),
            )
            ..headers.addAll({
              'Accept': 'application/json',
              'Authorization': ?token,
            });
      if (body != null) {
        req.headers['Content-Type'] = 'application/json';
        req.body = jsonEncode(body);
      }
      final response = await (() async {
        final stream = await _client.send(req);
        final bytes = <int>[];
        await for (final chunk in stream.stream) {
          bytes.addAll(chunk);
          if (bytes.length > 1048576) {
            throw BusinessApiException(503, outcomeUnknown: write);
          }
        }
        return http.Response.bytes(bytes, stream.statusCode);
      })().timeout(timeout);
      if (authorizationHeader() != token || workspaceID?.call() != workspace) {
        throw BusinessApiException(409, outcomeUnknown: write);
      }
      if (response.statusCode != 200) {
        throw BusinessApiException(
          response.statusCode,
          outcomeUnknown:
              write && !const {400, 404, 409}.contains(response.statusCode),
        );
      }
      final envelope = jsonDecode(utf8.decode(response.bodyBytes));
      if (envelope is! Map<String, dynamic> ||
          envelope['data'] is! Map<String, dynamic>) {
        throw BusinessApiException(503, outcomeUnknown: write);
      }
      return envelope['data'] as Map<String, dynamic>;
    } on BusinessApiException {
      rethrow;
    } catch (_) {
      throw BusinessApiException(503, outcomeUnknown: write);
    }
  }

  Future<PublicBusinessProfile> readPublic(String id) async {
    final result = PublicBusinessProfile.fromJson(
      await _request('GET', _publicPath(id)),
    );
    if (result.id != id) throw const BusinessApiException(503);
    return result;
  }

  Future<BusinessPublicationState> readPermission(String id) async =>
      BusinessPublicationState.fromJson(
        await _request('GET', _permissionPath(id), owner: true),
      );
  Future<void> changePermission(String id, Map<String, dynamic> body) async {
    await _request('PUT', _permissionPath(id), body: body, owner: true);
  }
}
