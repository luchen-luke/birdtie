import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import 'browse_destination.dart';
import 'destination_search_service.dart';
import 'public_city_controller.dart';

class DestinationSearchController extends ChangeNotifier {
  DestinationSearchController({
    DestinationSearchService? service,
    bool Function()? current,
    this.debounce = const Duration(milliseconds: 300),
  }) : _service = service ?? CompositeDestinationSearchService(),
       _ownsService = service == null,
       _current = current;

  final DestinationSearchService _service;
  final bool _ownsService;
  final bool Function()? _current;
  final Duration debounce;
  TextEditingValue _input = TextEditingValue.empty;
  Timer? _pending;
  Completer<void>? _abort;
  int _generation = 0;
  bool _closed = false, _loading = false, _domesticUnavailable = false;
  List<BrowseDestination> _items = const [];
  final List<String> _recentQueries = [];
  final List<DateTime> _calls = [];
  DateTime? _retryAt;
  String? _error;
  PublicCity? _resolvingCity;

  String get query => _input.text.trim();
  List<BrowseDestination> get items => _items;
  bool get loading => _loading;
  String? get error => _error;
  bool get domesticUnavailable => _domesticUnavailable;
  List<String> get recentQueries => List.unmodifiable(_recentQueries);
  PublicCity? get resolvingCity => _resolvingCity;
  bool get _valid => !_closed && (_current?.call() ?? true);
  bool get _composing =>
      _input.composing.isValid && !_input.composing.isCollapsed;

  void updateInput(TextEditingValue value) {
    if (_closed || value == _input) return;
    if (value.text == _input.text && value.composing == _input.composing) {
      _input = value;
      return;
    }
    _resolvingCity = null;
    _input = value;
    _cancelPending();
    _items = const [];
    _error = null;
    _domesticUnavailable = false;
    if (_valid && !_composing && query.isNotEmpty) {
      final generation = _generation;
      _pending = Timer(debounce, () => unawaited(_search(generation)));
    }
    notifyListeners();
  }

  Future<void> submit() async {
    if (!_valid || _composing || query.isEmpty) return;
    _cancelPending();
    await _search(_generation);
  }

  Future<void> retry() => submit();

  /// Reuses the same request budget, abort and latest-response fencing. This
  /// never selects a city or moves the map: the caller presents these candidates
  /// with their country/region and binds only the user's explicit choice.
  Future<void> resolveCity(PublicCity city) async {
    if (!_valid) return;
    _cancelPending();
    _resolvingCity = city;
    _input = TextEditingValue(
      text: [
        city.name,
        city.region,
        city.countryCode,
      ].where((part) => part.trim().isNotEmpty).join(' '),
    );
    _items = const [];
    _error = null;
    _domesticUnavailable = false;
    if (!RegExp(r'^[A-Z]{2}$').hasMatch(city.countryCode)) {
      _error = '此目录城市缺少可核对的国家信息，请直接搜索目的地。';
      notifyListeners();
      return;
    }
    await _search(_generation);
  }

  void cancelResolution() {
    if (_closed) return;
    _resolvingCity = null;
    _input = TextEditingValue.empty;
    cancel();
  }

  Future<void> _search(int generation) async {
    if (!_valid || generation != _generation || _composing || query.isEmpty)
      return;
    final now = DateTime.now();
    _calls.removeWhere(
      (at) => now.difference(at) >= const Duration(minutes: 1),
    );
    if ((_retryAt?.isAfter(now) ?? false) || _calls.length >= 20) {
      _error = '搜索较频繁，请稍后重试。';
      _loading = false;
      notifyListeners();
      return;
    }
    final value = query, abort = Completer<void>();
    _abort = abort;
    _loading = true;
    _error = null;
    _calls.add(now);
    notifyListeners();
    bool live() => _valid && generation == _generation && value == query;
    try {
      final result = await _service.search(value, abortTrigger: abort.future);
      if (!live()) return;
      final city = _resolvingCity;
      _items = city == null
          ? result.destinations
          : List.unmodifiable(
              result.destinations.where(
                (item) => item.canLocateCatalogCity(city.countryCode),
              ),
            );
      _domesticUnavailable = result.domesticUnavailable;
      _error = result.partialError;
      _retryAt = null;
      if (city == null) {
        _recentQueries.remove(value);
        _recentQueries.insert(0, value);
        if (_recentQueries.length > 8) _recentQueries.removeLast();
      }
    } catch (failure) {
      if (!live()) return;
      _items = const [];
      _domesticUnavailable = false;
      _error = failure is DestinationSearchFailure
          ? failure.message
          : '目的地搜索失败，请重试。';
      if (failure is DestinationSearchFailure && failure.retryAfter != null) {
        _retryAt = DateTime.now().add(failure.retryAfter!);
      }
    } finally {
      if (identical(_abort, abort)) _abort = null;
      if (live()) {
        _loading = false;
        notifyListeners();
      }
    }
  }

  void _cancelPending() {
    ++_generation;
    _pending?.cancel();
    _pending = null;
    if (_abort != null && !_abort!.isCompleted) _abort!.complete();
    _abort = null;
    _loading = false;
  }

  void cancel() {
    if (_closed) return;
    _cancelPending();
    _items = const [];
    _error = null;
    _domesticUnavailable = false;
    _resolvingCity = null;
    notifyListeners();
  }

  @override
  void dispose() {
    _closed = true;
    _cancelPending();
    _items = const [];
    if (_ownsService) _service.dispose();
    super.dispose();
  }
}
