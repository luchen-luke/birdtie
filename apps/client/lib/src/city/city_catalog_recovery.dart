import 'dart:async';

import 'public_city_controller.dart';

/// Recovers an unavailable startup catalog without resetting a usable city.
/// Only the existing public read is retried; the user's manual retry stays live.
class CityCatalogRecovery {
  CityCatalogRecovery(this.city);

  final PublicCityController city;
  static const _retryDelays = [
    Duration(seconds: 2),
    Duration(seconds: 5),
    Duration(seconds: 10),
  ];

  Timer? _retryTimer;
  int _retryIndex = 0;
  bool _started = false;
  bool _foreground = false;
  bool _readStarted = false;
  bool _closed = false;

  void start({bool foreground = true}) {
    if (_closed || _started) return;
    _started = true;
    _foreground = foreground;
    _readStarted = city.citiesLoading;
    city.addListener(_onCityChanged);
    _readIfNeeded();
  }

  void setForeground(bool foreground) {
    if (_closed || !_started || foreground == _foreground) return;
    _foreground = foreground;
    _cancelRetry();
    if (foreground) {
      // Returning to the app is a fresh opportunity to reach the service.
      _retryIndex = 0;
      _readIfNeeded();
    }
  }

  bool get _canRead =>
      !_closed &&
      _started &&
      _foreground &&
      city.cities.isEmpty &&
      !city.citiesLoading;

  bool get _needsRetry => _canRead && city.configured && city.cityError != null;

  void _readIfNeeded() {
    // The first read also lets the controller expose a missing configuration.
    // Configuration errors do not receive automatic or resume retries.
    if (!_canRead || (_readStarted && !_needsRetry)) return;
    _readStarted = true;
    unawaited(city.loadCities());
  }

  void _onCityChanged() {
    if (_closed) return;
    if (city.citiesLoading) _readStarted = true;
    if (!_needsRetry) {
      _cancelRetry();
      return;
    }
    if (_retryTimer != null || _retryIndex >= _retryDelays.length) return;
    _retryTimer = Timer(_retryDelays[_retryIndex], () {
      _retryTimer = null;
      if (!_needsRetry) return;
      _retryIndex++;
      _readIfNeeded();
    });
  }

  void _cancelRetry() {
    _retryTimer?.cancel();
    _retryTimer = null;
  }

  void dispose() {
    if (_closed) return;
    _closed = true;
    _cancelRetry();
    if (_started) city.removeListener(_onCityChanged);
  }
}
