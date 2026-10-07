import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import '../city/public_city_controller.dart';
import 'map_entities.dart';
import '../config/birdtie_environment.dart';

class PulseCategory {
  const PulseCategory({required this.code, required this.count});
  final String code;
  final int count;

  factory PulseCategory.fromJson(Map<String, dynamic> json) =>
      PulseCategory(code: json['code'] as String, count: json['count'] as int);
}

class NowPulse {
  const NowPulse({
    required this.cityID,
    required this.bounds,
    required this.status,
    required this.total,
    required this.categories,
    required this.activities,
    required this.truncated,
  });

  final String cityID;
  final MapBounds bounds;
  final String status;
  final int total;
  final List<PulseCategory> categories;
  final List<PublicActivity> activities;
  final bool truncated;

  factory NowPulse.fromJson(Map<String, dynamic> json) {
    final coordinates = (json['bounds'] as List<dynamic>)
        .map((value) => (value as num).toDouble())
        .toList();
    if (coordinates.length != 4) {
      throw const FormatException('invalid pulse bounds');
    }
    final bounds = MapBounds(
      west: coordinates[0],
      south: coordinates[1],
      east: coordinates[2],
      north: coordinates[3],
    );
    final categories = [
      for (final item in json['categories'] as List<dynamic>)
        PulseCategory.fromJson(item as Map<String, dynamic>),
    ];
    final activities = [
      for (final item in json['activities'] as List<dynamic>)
        PublicActivity.fromJson(item as Map<String, dynamic>),
    ];
    final status = json['status'] as String;
    final total = json['total'] as int;
    if (!bounds.isValid ||
        (status != 'empty' && status != 'populated') ||
        total < activities.length ||
        (status == 'empty') != (total == 0)) {
      throw const FormatException('invalid pulse payload');
    }
    return NowPulse(
      cityID: json['cityId'] as String,
      bounds: bounds,
      status: status,
      total: total,
      categories: List.unmodifiable(categories),
      activities: List.unmodifiable(activities),
      truncated: json['truncated'] as bool,
    );
  }
}

class NowDiscoveryController extends ChangeNotifier {
  NowDiscoveryController({
    required this.authorizationHeader,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _apiBaseUrl =
           apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;

  final String? Function() authorizationHeader;
  final http.Client _client;
  final String _apiBaseUrl;
  int _serial = 0;
  int _citySerial = 0;
  bool _closed = false;
  bool loading = false;
  String? error;
  NowPulse? pulse;
  List<PublicActivity> cityActivities = const [];
  bool cityLoading = false;
  String? cityError;

  void _notify() {
    if (!_closed) notifyListeners();
  }

  void clear() {
    _serial++;
    _citySerial++;
    pulse = null;
    error = null;
    loading = false;
    cityActivities = const [];
    cityLoading = false;
    cityError = null;
    _notify();
  }

  Future<void> loadCityActivities(String cityID) async {
    final serial = ++_citySerial;
    cityLoading = true;
    cityError = null;
    _notify();
    if (_apiBaseUrl.isEmpty) {
      cityLoading = false;
      cityError = '尚未连接 Birdtie 服务。';
      _notify();
      return;
    }
    try {
      final endpoint = Uri.parse(
        '${_apiBaseUrl.replaceFirst(RegExp(r'/$'), '')}'
        '/v1/cities/${Uri.encodeComponent(cityID)}/activities',
      ).replace(queryParameters: {'category': ''});
      final bearer = authorizationHeader();
      final response = await _client
          .get(
            endpoint,
            headers: bearer == null ? null : {'Authorization': bearer},
          )
          .timeout(const Duration(seconds: 10));
      if (response.statusCode != 200) {
        throw const FormatException('activities unavailable');
      }
      final records =
          (jsonDecode(utf8.decode(response.bodyBytes))
                  as Map<String, dynamic>)['data']
              as List<dynamic>;
      final next = [
        for (final item in records)
          PublicActivity.fromJson(item as Map<String, dynamic>),
      ];
      if (serial == _citySerial) cityActivities = List.unmodifiable(next);
    } on Object {
      if (serial == _citySerial) cityError = '无法读取当前城市的活动，请稍后重试。';
    } finally {
      if (serial == _citySerial) {
        cityLoading = false;
        _notify();
      }
    }
  }

  Future<void> load(String cityID, MapBounds bounds) async {
    if (!bounds.isValid) return;
    if (pulse?.cityID == cityID && pulse?.bounds == bounds && error == null) {
      // Returning to the displayed area supersedes any request for a
      // different area that is still in flight.
      if (loading) {
        ++_serial;
        loading = false;
        _notify();
      }
      return;
    }
    final serial = ++_serial;
    loading = true;
    error = null;
    _notify();
    if (_apiBaseUrl.isEmpty) {
      loading = false;
      error = '尚未连接 Birdtie 服务，暂时无法读取附近活动。';
      _notify();
      return;
    }
    try {
      final endpoint =
          Uri.parse(
            '${_apiBaseUrl.replaceFirst(RegExp(r'/$'), '')}'
            '/v1/cities/${Uri.encodeComponent(cityID)}/pulse',
          ).replace(
            queryParameters: {
              'bounds': [
                bounds.west,
                bounds.south,
                bounds.east,
                bounds.north,
              ].join(','),
            },
          );
      final bearer = authorizationHeader();
      final response = await _client
          .get(
            endpoint,
            headers: bearer == null ? null : {'Authorization': bearer},
          )
          .timeout(const Duration(seconds: 10));
      if (response.statusCode != 200) {
        throw const FormatException('pulse unavailable');
      }
      final data =
          (jsonDecode(utf8.decode(response.bodyBytes))
                  as Map<String, dynamic>)['data']
              as Map<String, dynamic>;
      final next = NowPulse.fromJson(data);
      if (next.cityID != cityID || next.bounds != bounds) {
        throw const FormatException('pulse context mismatch');
      }
      if (serial == _serial) pulse = next;
    } on Object {
      if (serial == _serial) {
        error = '无法读取这片区域的活动，请点击重试。';
      }
    } finally {
      if (serial == _serial) {
        loading = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    _closed = true;
    _serial++;
    _citySerial++;
    _client.close();
    super.dispose();
  }
}
