import 'dart:convert';
import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

class PublicSource {
  const PublicSource({
    required this.label,
    this.reference = '',
    required this.maintainer,
    required this.freshness,
    required this.updatedAt,
  });

  final String label;
  final String reference;
  final String maintainer;
  final String freshness;
  final DateTime? updatedAt;

  factory PublicSource.fromJson(Map<String, dynamic> json) => PublicSource(
    label: json['label'] as String? ?? '',
    reference: json['reference'] as String? ?? '',
    maintainer: json['maintainer'] as String? ?? '',
    freshness: json['freshness'] as String? ?? 'unverified',
    updatedAt: DateTime.tryParse(json['updatedAt'] as String? ?? ''),
  );
}

class PublicCity {
  const PublicCity({
    required this.id,
    required this.name,
    required this.region,
    this.timeZone = 'UTC',
    required this.contentStatus,
    required this.source,
    required this.map,
  });

  final String id;
  final String name;
  final String region;
  final String timeZone;
  final String contentStatus;
  final PublicSource source;
  final PublicCityMap? map;

  factory PublicCity.fromJson(Map<String, dynamic> json) => PublicCity(
    id: json['id'] as String,
    name: json['name'] as String,
    region: json['region'] as String? ?? '',
    timeZone: json['timeZone'] as String? ?? 'UTC',
    contentStatus: json['contentStatus'] as String? ?? 'building',
    source: PublicSource.fromJson(json['source'] as Map<String, dynamic>),
    map: json['map'] is Map<String, dynamic>
        ? PublicCityMap.fromJson(json['map'] as Map<String, dynamic>)
        : null,
  );
}

class PublicCityMap {
  const PublicCityMap({
    required this.provider,
    required this.latitude,
    required this.longitude,
    required this.defaultZoom,
    required this.sourceRef,
  });

  final String provider;
  final double latitude;
  final double longitude;
  final double defaultZoom;
  final String sourceRef;

  factory PublicCityMap.fromJson(Map<String, dynamic> json) => PublicCityMap(
    provider: json['provider'] as String,
    latitude: (json['latitude'] as num).toDouble(),
    longitude: (json['longitude'] as num).toDouble(),
    defaultZoom: (json['defaultZoom'] as num).toDouble(),
    sourceRef: json['sourceRef'] as String? ?? '',
  );
}

class PublicPlace {
  const PublicPlace({
    required this.id,
    required this.name,
    required this.categoryCode,
    required this.summary,
    required this.source,
    required this.location,
  });

  final String id;
  final String name;
  final String categoryCode;
  final String summary;
  final PublicSource source;
  final PublicPlaceLocation location;

  factory PublicPlace.fromJson(Map<String, dynamic> json) => PublicPlace(
    id: json['id'] as String,
    name: json['name'] as String,
    categoryCode: json['categoryCode'] as String? ?? '',
    summary: json['summary'] as String? ?? '',
    source: PublicSource.fromJson(json['source'] as Map<String, dynamic>),
    location: PublicPlaceLocation.fromJson(
      json['location'] as Map<String, dynamic>,
    ),
  );
}

class PublicPlaceLocation {
  const PublicPlaceLocation({
    required this.coordinateSystem,
    required this.precision,
    required this.latitude,
    required this.longitude,
  });

  final String coordinateSystem;
  final String precision;
  final double? latitude;
  final double? longitude;

  bool get hasPublicPoint =>
      coordinateSystem == 'wgs84' &&
      precision == 'point' &&
      latitude != null &&
      longitude != null;

  factory PublicPlaceLocation.fromJson(Map<String, dynamic> json) =>
      PublicPlaceLocation(
        coordinateSystem: json['coordinateSystem'] as String? ?? '',
        precision: json['precision'] as String? ?? 'none',
        latitude: (json['latitude'] as num?)?.toDouble(),
        longitude: (json['longitude'] as num?)?.toDouble(),
      );
}

class PublicActivity {
  const PublicActivity({
    required this.id,
    required this.hostLabel,
    required this.placeName,
    required this.title,
    required this.summary,
    required this.startsAt,
    required this.endsAt,
    required this.timeZone,
    required this.schedule,
    required this.status,
    required this.source,
    required this.location,
  });

  final String id;
  final String hostLabel;
  final String placeName;
  final String title;
  final String summary;
  final DateTime startsAt;
  final DateTime endsAt;
  final String timeZone;
  final String schedule;
  final String status;
  final PublicSource source;
  final PublicPlaceLocation? location;

  factory PublicActivity.fromJson(Map<String, dynamic> json) => PublicActivity(
    id: json['id'] as String,
    hostLabel: json['hostLabel'] as String? ?? '',
    placeName: json['placeName'] as String? ?? '',
    title: json['title'] as String,
    summary: json['summary'] as String? ?? '',
    startsAt: DateTime.parse(json['startsAt'] as String),
    endsAt: DateTime.parse(json['endsAt'] as String),
    timeZone: json['timeZone'] as String,
    schedule: json['schedule'] as String? ?? '',
    status: json['status'] as String,
    source: PublicSource.fromJson(json['source'] as Map<String, dynamic>),
    location: json['location'] is Map<String, dynamic>
        ? PublicPlaceLocation.fromJson(json['location'] as Map<String, dynamic>)
        : null,
  );
}

class PublicCityController extends ChangeNotifier {
  PublicCityController({this.authorizationHeader}) : _client = http.Client();

  static const _apiBase = String.fromEnvironment('BIRDTIE_API_BASE_URL');
  final http.Client _client;
  final String? Function()? authorizationHeader;
  List<PublicCity> _cities = const [];
  List<PublicPlace> _places = const [];
  List<PublicActivity> _activities = const [];
  String? _selectedCityID;
  String _placeQuery = '';
  String? _cityError;
  String? _placeError;
  String? _activityError;
  bool _citiesLoading = false;
  bool _placesLoading = false;
  bool _activitiesLoading = false;
  bool _closed = false;
  int _placeRequest = 0;
  int _activityRequest = 0;

  bool get configured => _apiBase.isNotEmpty;
  List<PublicCity> get cities => _cities;
  List<PublicPlace> get places => _places;
  String get placeQuery => _placeQuery;
  List<PublicActivity> get activities => _activities;
  PublicCity? get selectedCity {
    for (final city in _cities) {
      if (city.id == _selectedCityID) return city;
    }
    return null;
  }

  String? get cityError => _cityError;
  String? get placeError => _placeError;
  String? get activityError => _activityError;
  bool get citiesLoading => _citiesLoading;
  bool get placesLoading => _placesLoading;
  bool get activitiesLoading => _activitiesLoading;

  Uri _endpoint(String path) =>
      Uri.parse('${_apiBase.replaceFirst(RegExp(r'/$'), '')}$path');

  void _notify() {
    if (!_closed) notifyListeners();
  }

  Future<void> loadCities() async {
    if (!configured || _citiesLoading) return;
    _citiesLoading = true;
    _cityError = null;
    _notify();
    try {
      final response = await _client
          .get(_endpoint('/v1/cities'))
          .timeout(const Duration(seconds: 10));
      if (response.statusCode != 200) {
        throw const FormatException('cities unavailable');
      }
      final body = jsonDecode(response.body) as Map<String, dynamic>;
      final records = body['data'] as List<dynamic>;
      final next = records
          .map((item) => PublicCity.fromJson(item as Map<String, dynamic>))
          .toList(growable: false);
      _cities = List.unmodifiable(next);
      if (!next.any((city) => city.id == _selectedCityID)) {
        _selectedCityID = next.isEmpty ? null : next.first.id;
        _placeRequest++;
        _places = const [];
        _placesLoading = false;
        _placeError = null;
        _activityRequest++;
        _activities = const [];
        _activitiesLoading = false;
        _activityError = null;
      }
      _notify();
      if (_selectedCityID != null) {
        await Future.wait([loadPlaces(), loadActivities()]);
      }
    } catch (_) {
      _cityError = '无法读取城市，请稍后重试。';
    } finally {
      _citiesLoading = false;
      _notify();
    }
  }

  void selectCity(String cityID) {
    if (cityID == _selectedCityID || !cities.any((city) => city.id == cityID)) {
      return;
    }
    _selectedCityID = cityID;
    _placeRequest++;
    _places = const [];
    _placeError = null;
    _activityRequest++;
    _activities = const [];
    _activitiesLoading = false;
    _activityError = null;
    _notify();
    unawaited(loadPlaces());
    unawaited(loadActivities());
  }

  void searchPlaces(String query) {
    final next = query.trim();
    if (next == _placeQuery) return;
    _placeQuery = next;
    _placeRequest++;
    _places = const [];
    _placeError = null;
    _placesLoading = false;
    _notify();
    unawaited(loadPlaces());
  }

  Future<void> loadPlaces() async {
    final cityID = _selectedCityID;
    if (!configured || cityID == null) return;
    final request = ++_placeRequest;
    _placesLoading = true;
    _placeError = null;
    _notify();
    try {
      final endpoint = _endpoint(
        '/v1/cities/${Uri.encodeComponent(cityID)}/places',
      );
      final uri = _placeQuery.isEmpty
          ? endpoint
          : endpoint.replace(queryParameters: {'q': _placeQuery});
      final response = await _client
          .get(uri)
          .timeout(const Duration(seconds: 10));
      if (response.statusCode != 200) {
        throw const FormatException('places unavailable');
      }
      final body = jsonDecode(response.body) as Map<String, dynamic>;
      final records = body['data'] as List<dynamic>;
      final next = records
          .map((item) => PublicPlace.fromJson(item as Map<String, dynamic>))
          .toList(growable: false);
      if (request == _placeRequest) _places = List.unmodifiable(next);
    } catch (_) {
      if (request == _placeRequest) {
        _placeError = '无法读取地点，请稍后重试。';
      }
    } finally {
      if (request == _placeRequest) {
        _placesLoading = false;
        _notify();
      }
    }
  }

  Future<void> loadActivities() async {
    final cityID = _selectedCityID;
    if (!configured || cityID == null) return;
    final request = ++_activityRequest;
    _activitiesLoading = true;
    _activityError = null;
    _notify();
    try {
      final bearer = authorizationHeader?.call();
      final response = await _client
          .get(
            _endpoint('/v1/cities/${Uri.encodeComponent(cityID)}/activities'),
            headers: bearer == null ? null : {'Authorization': bearer},
          )
          .timeout(const Duration(seconds: 10));
      if (response.statusCode != 200) {
        throw const FormatException('activities unavailable');
      }
      final body = jsonDecode(response.body) as Map<String, dynamic>;
      final records = body['data'] as List<dynamic>;
      final next = records
          .map((item) => PublicActivity.fromJson(item as Map<String, dynamic>))
          .toList(growable: false);
      if (request == _activityRequest) _activities = List.unmodifiable(next);
    } catch (_) {
      if (request == _activityRequest) {
        _activityError = '无法读取活动，请稍后重试。';
      }
    } finally {
      if (request == _activityRequest) {
        _activitiesLoading = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    _closed = true;
    _client.close();
    super.dispose();
  }
}
