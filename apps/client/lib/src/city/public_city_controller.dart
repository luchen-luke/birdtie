import 'dart:convert';
import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';

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
    this.addressLabel = '',
    required this.source,
    required this.location,
  });

  final String id;
  final String name;
  final String categoryCode;
  final String summary;
  final String addressLabel;
  final PublicSource source;
  final PublicPlaceLocation location;

  factory PublicPlace.fromJson(Map<String, dynamic> json) => PublicPlace(
    id: json['id'] as String,
    name: json['name'] as String,
    categoryCode: json['categoryCode'] as String? ?? '',
    summary: json['summary'] as String? ?? '',
    addressLabel: json['addressLabel'] as String? ?? '',
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
    this.organizer,
    this.organizationID,
    required this.hostLabel,
    required this.placeName,
    this.modality = 'unspecified',
    this.physicalPlaceStatus = 'unknown',
    required this.title,
    required this.summary,
    this.description = '',
    this.categoryCode = '',
    this.capacity,
    this.participantCount,
    this.priceMinor = 0,
    this.currency = '',
    this.eligibility = '',
    this.languageCode = '',
    this.officialURL = '',
    required this.startsAt,
    required this.endsAt,
    required this.timeZone,
    required this.schedule,
    this.endSchedule = '',
    required this.status,
    required this.source,
    required this.location,
  });

  final String id;
  final PublicActivityOrganizer? organizer;
  final String? organizationID;
  final String hostLabel;
  final String placeName;
  final String modality;
  final String physicalPlaceStatus;
  final String title;
  final String summary;
  final String description;
  final String categoryCode;
  final int? capacity;
  final int? participantCount;
  final int priceMinor;
  final String currency;
  final String eligibility;
  final String languageCode;
  final String officialURL;
  final DateTime startsAt;
  final DateTime endsAt;
  final String timeZone;
  final String schedule;
  final String endSchedule;
  final String status;
  final PublicSource source;
  final PublicPlaceLocation? location;

  factory PublicActivity.fromJson(Map<String, dynamic> json) => PublicActivity(
    id: json['id'] as String,
    organizer: json['organizer'] is Map<String, dynamic>
        ? PublicActivityOrganizer.fromJson(
            json['organizer'] as Map<String, dynamic>,
          )
        : null,
    organizationID: json['organizationId'] as String?,
    hostLabel: json['hostLabel'] as String? ?? '',
    placeName: json['placeName'] as String? ?? '',
    modality: json['modality'] as String? ?? 'unspecified',
    physicalPlaceStatus: json['physicalPlaceStatus'] as String? ?? 'unknown',
    title: json['title'] as String,
    summary: json['summary'] as String? ?? '',
    description: json['description'] as String? ?? '',
    categoryCode: json['categoryCode'] as String? ?? '',
    capacity: json['capacity'] as int?,
    participantCount: json['participantCount'] as int?,
    priceMinor: json['priceMinor'] as int? ?? 0,
    currency: json['currency'] as String? ?? '',
    eligibility: json['eligibility'] as String? ?? '',
    languageCode: json['languageCode'] as String? ?? '',
    officialURL: json['officialUrl'] as String? ?? '',
    startsAt: DateTime.parse(json['startsAt'] as String),
    endsAt: DateTime.parse(json['endsAt'] as String),
    timeZone: json['timeZone'] as String,
    schedule: json['schedule'] as String? ?? '',
    endSchedule: json['endSchedule'] as String? ?? '',
    status: json['status'] as String,
    source: PublicSource.fromJson(json['source'] as Map<String, dynamic>),
    location: json['location'] is Map<String, dynamic>
        ? PublicPlaceLocation.fromJson(json['location'] as Map<String, dynamic>)
        : null,
  );
}

class PublicActivityOrganizer {
  const PublicActivityOrganizer({
    required this.type,
    required this.id,
    required this.name,
    this.avatarUrl,
  });
  final String type;
  final String id;
  final String name;
  final String? avatarUrl;

  factory PublicActivityOrganizer.fromJson(Map<String, dynamic> json) =>
      PublicActivityOrganizer(
        type: json['type'] as String? ?? '',
        id: json['id'] as String? ?? '',
        name: json['name'] as String? ?? '',
        avatarUrl: json['avatarUrl'] as String?,
      );
}

class PublicOrganizationPin {
  const PublicOrganizationPin({
    required this.id,
    required this.name,
    required this.latitude,
    required this.longitude,
  });

  final String id;
  final String name;
  final double latitude;
  final double longitude;

  factory PublicOrganizationPin.fromJson(Map<String, dynamic> json) =>
      PublicOrganizationPin(
        id: json['id'] as String,
        name: json['name'] as String,
        latitude: (json['latitude'] as num).toDouble(),
        longitude: (json['longitude'] as num).toDouble(),
      );
}

class PublicCityController extends ChangeNotifier {
  PublicCityController({
    this.authorizationHeader,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _apiBase = (apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl).trim();

  final String _apiBase;
  final http.Client _client;
  final bool _ownsClient;
  final String? Function()? authorizationHeader;
  List<PublicCity> _cities = const [];
  List<PublicPlace> _places = const [];
  List<PublicActivity> _activities = const [];
  List<PublicOrganizationPin> _organizationPins = const [];
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
  int _organizationPinRequest = 0;

  bool get configured => _apiBase.isNotEmpty;
  List<PublicCity> get cities => _cities;
  List<PublicPlace> get places => _places;
  String get placeQuery => _placeQuery;
  List<PublicActivity> get activities => _activities;
  List<PublicOrganizationPin> get organizationPins => _organizationPins;
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
    if (_closed || _citiesLoading) return;
    if (!configured) {
      _cityError = '尚未配置城市服务，请检查运行设置后重试。';
      _notify();
      return;
    }
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
        // A catalog option is not a location observation. First load or removal
        // of an explicit choice requires the person to choose again.
        _selectedCityID = null;
        _placeRequest++;
        _places = const [];
        _placesLoading = false;
        _placeError = null;
        _activityRequest++;
        _activities = const [];
        _activitiesLoading = false;
        _activityError = null;
        _organizationPinRequest++;
        _organizationPins = const [];
      }
      _notify();
      if (_selectedCityID != null) {
        await Future.wait([
          loadPlaces(),
          loadActivities(),
          loadOrganizationPins(),
        ]);
      }
    } catch (_) {
      _cityError = '无法读取城市，请稍后重试。';
    } finally {
      _citiesLoading = false;
      _notify();
    }
  }

  void selectCity(String cityID) {
    if (_closed ||
        cityID == _selectedCityID ||
        !cities.any((city) => city.id == cityID)) {
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
    _organizationPinRequest++;
    _organizationPins = const [];
    _notify();
    unawaited(loadPlaces());
    unawaited(loadActivities());
    unawaited(loadOrganizationPins());
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

  Future<void> loadOrganizationPins() async {
    final cityID = _selectedCityID;
    if (!configured || cityID == null) return;
    final request = ++_organizationPinRequest;
    try {
      final response = await _client
          .get(
            _endpoint(
              '/v1/cities/${Uri.encodeComponent(cityID)}/organizations/map',
            ),
          )
          .timeout(const Duration(seconds: 10));
      if (response.statusCode != 200) {
        throw const FormatException('organization map unavailable');
      }
      final records =
          (jsonDecode(response.body) as Map<String, dynamic>)['data']
              as List<dynamic>;
      final next = records
          .map(
            (item) =>
                PublicOrganizationPin.fromJson(item as Map<String, dynamic>),
          )
          .toList(growable: false);
      if (request == _organizationPinRequest) {
        _organizationPins = List.unmodifiable(next);
      }
    } catch (_) {
      // A failed public read must never leave a previously approved point visible.
      if (request == _organizationPinRequest) _organizationPins = const [];
    } finally {
      if (request == _organizationPinRequest) _notify();
    }
  }

  @override
  void dispose() {
    _closed = true;
    if (_ownsClient) _client.close();
    super.dispose();
  }
}
