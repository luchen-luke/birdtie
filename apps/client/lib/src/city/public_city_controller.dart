import 'dart:convert';
import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'exploration_city_store.dart';
import '../workspace/entity_cover.dart';
import '../workspace/place_photo.dart';

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
    this.countryCode = '',
    this.timeZone = 'UTC',
    required this.contentStatus,
    required this.source,
    required this.map,
  });

  final String id;
  final String name;
  final String region;
  final String countryCode;
  final String timeZone;
  final String contentStatus;
  final PublicSource source;
  final PublicCityMap? map;

  factory PublicCity.fromJson(Map<String, dynamic> json) => PublicCity(
    id: json['id'] as String,
    name: json['name'] as String,
    region: json['region'] as String? ?? '',
    countryCode: json['countryCode'] as String? ?? '',
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
    this.placePhoto,
  });

  final String id;
  final String name;
  final String categoryCode;
  final String summary;
  final String addressLabel;
  final PublicSource source;
  final PublicPlaceLocation location;
  final EntityCoverMetadata? placePhoto;

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
    placePhoto: EntityCoverMetadata.optional(
      json['placePhoto'],
      PlacePhotoRef(json['id'] as String),
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
    this.cover,
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
  final EntityCoverMetadata? cover;
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
    cover: EntityCoverMetadata.optional(
      json['cover'],
      EntityCoverRef('activity', json['id'] as String),
    ),
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
    this.loadUnrequestedPlaces = true,
    http.Client? client,
    String? apiBaseUrl,
    ExplorationCityStore? explorationStore,
    this.defaultExplorationCityID = const String.fromEnvironment(
      'BIRDTIE_DEFAULT_EXPLORATION_CITY_ID',
      defaultValue: 'aberdeen-gb',
    ),
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _explorationStore = explorationStore,
       _apiBase = (apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl).trim();

  final bool loadUnrequestedPlaces;
  final String _apiBase;
  final http.Client _client;
  final bool _ownsClient;
  final ExplorationCityStore? _explorationStore;
  final String defaultExplorationCityID;
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
  int _explorationChoiceRevision = 0;
  String? _storedExplorationCityID;
  String? _explorationNotice;
  bool _explorationFallback = false;
  Future<void>? _explorationRestore;
  Future<void> _explorationWriteTail = Future.value();

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
  String? get explorationNotice => _explorationNotice;

  @visibleForTesting
  Future<void> get explorationRestoreSettled =>
      _explorationRestore ?? Future.value();
  @visibleForTesting
  Future<void> get explorationWritesSettled => _explorationWriteTail;

  Uri _endpoint(String path) =>
      Uri.parse('${_apiBase.replaceFirst(RegExp(r'/$'), '')}$path');

  void _notify() {
    if (!_closed) notifyListeners();
  }

  bool _hasUsableMap(PublicCity city) {
    final map = city.map;
    if (map == null ||
        !map.latitude.isFinite ||
        !map.longitude.isFinite ||
        map.latitude.abs() > 90 ||
        map.longitude.abs() > 180 ||
        !map.defaultZoom.isFinite ||
        map.defaultZoom < 1 ||
        map.defaultZoom > 18) {
      return false;
    }
    // A configured city camera is usable even if its tile service temporarily
    // fails. The existing map presents that error and retry. AMap currently has
    // a web implementation only, so native startup must not default to it.
    return map.provider == 'mapbox' ||
        (map.provider == 'amap' &&
            kIsWeb &&
            BirdtieEnvironment.amapWebPublicKey.isNotEmpty &&
            BirdtieEnvironment.amapServiceHost.isNotEmpty);
  }

  PublicCity? _defaultExplorationCity() {
    for (final city in _cities) {
      if (city.id == defaultExplorationCityID && _hasUsableMap(city)) {
        return city;
      }
    }
    for (final city in _cities) {
      if (_hasUsableMap(city)) return city;
    }
    for (final city in _cities) {
      if (city.id == defaultExplorationCityID) return city;
    }
    return _cities.isEmpty ? null : _cities.first;
  }

  void _restoreExplorationChoice() {
    final store = _explorationStore;
    if (_explorationRestore != null || store == null) return;
    final revision = _explorationChoiceRevision;
    _explorationRestore = () async {
      try {
        final saved = await store.readCityID().timeout(
          const Duration(seconds: 2),
        );
        if (_closed || revision != _explorationChoiceRevision) return;
        _storedExplorationCityID = saved;
        if (_cities.isNotEmpty && saved != null) {
          _selectRestoredOrDefault();
          _notify();
          unawaited(_loadSelectedCityContent());
        }
      } catch (_) {
        if (_closed || revision != _explorationChoiceRevision) return;
        _explorationNotice = '未能恢复本机探索城市，仍可选择城市继续浏览。';
        _notify();
      }
    }();
  }

  void _selectRestoredOrDefault({String? removedCityName}) {
    PublicCity? saved;
    for (final city in _cities) {
      if (city.id == _storedExplorationCityID && _hasUsableMap(city)) {
        saved = city;
        break;
      }
    }
    final chosen = saved ?? _defaultExplorationCity();
    _setSelectedCity(chosen?.id);
    _explorationFallback = _storedExplorationCityID != null && saved == null;
    if (removedCityName != null) {
      _explorationNotice = chosen == null
          ? '原探索城市已从目录移除，请重新读取或选择城市。'
          : '原探索城市已从目录移除，已暂时切换到${chosen.name}；这不代表你的实际位置。';
    } else if (_storedExplorationCityID != null && saved == null) {
      _explorationNotice = chosen == null
          ? '上次探索城市暂不可用，请重新读取或选择城市。'
          : '上次探索城市暂不可用，已暂时切换到${chosen.name}；可手动选择其他城市。';
    } else if (saved != null) {
      _explorationNotice = null;
    }
  }

  void _setSelectedCity(String? cityID) {
    if (_selectedCityID == cityID) return;
    _selectedCityID = cityID;
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

  Future<void> _loadSelectedCityContent() async {
    await Future.wait([loadPlaces(), loadActivities(), loadOrganizationPins()]);
  }

  Future<void> loadCities() async {
    if (_closed || _citiesLoading) return;
    // Read in parallel. A stalled/failed local store cannot block the catalog
    // default. A later read may restore a destination only before a new choice.
    _restoreExplorationChoice();
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
      if (_closed) return;
      final previous = selectedCity;
      _cities = List.unmodifiable(next);
      if (!next.any((city) => city.id == _selectedCityID)) {
        _selectRestoredOrDefault(removedCityName: previous?.name);
      } else if (_explorationFallback &&
          next.any(
            (city) =>
                city.id == _storedExplorationCityID && _hasUsableMap(city),
          )) {
        _selectRestoredOrDefault();
      }
      _notify();
      if (_selectedCityID != null) {
        await _loadSelectedCityContent();
      }
    } catch (_) {
      _cityError = '无法读取城市，请稍后重试。';
    } finally {
      _citiesLoading = false;
      _notify();
    }
  }

  void selectCity(String cityID, {bool rememberExploration = true}) {
    if (_closed || !cities.any((city) => city.id == cityID)) {
      return;
    }
    // Even a temporary Task view must fence an older startup restore. Only an
    // explicit manual exploration choice is persisted across launches.
    _explorationChoiceRevision++;
    _explorationFallback = false;
    if (rememberExploration) {
      _storedExplorationCityID = cityID;
      _explorationNotice = null;
      final store = _explorationStore;
      if (store != null) {
        _explorationWriteTail = _explorationWriteTail.then((_) async {
          if (_closed || _storedExplorationCityID != cityID) return;
          try {
            await store.writeCityID(cityID);
          } catch (_) {
            if (!_closed && _storedExplorationCityID == cityID) {
              _explorationNotice = '本次选城已生效，但未能保存到本机；重启后可重新选择。';
              _notify();
            }
          }
        });
      }
    }
    if (cityID == _selectedCityID) {
      _notify();
      return;
    }
    _setSelectedCity(cityID);
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
    if (!configured ||
        cityID == null ||
        (!loadUnrequestedPlaces && _placeQuery.isEmpty))
      return;
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
