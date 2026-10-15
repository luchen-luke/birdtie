import 'dart:async';
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'now_ui_tokens.dart';
import 'location_reference_api.dart';
import '../content/moment_locations.dart';
import '../content/moment_control.dart';

class MomentLocationChoice {
  const MomentLocationChoice({
    this.cityID = '',
    this.cityName = '',
    this.placeID = '',
    this.referenceID = '',
    this.placeName = '',
    String displayLabel = '',
  }) : _displayLabel = displayLabel;
  final String cityID, cityName, placeID, placeName, referenceID;
  final String _displayLabel;
  String get displayLabel => _displayLabel.isNotEmpty
      ? _displayLabel
      : placeName.isNotEmpty
      ? placeName
      : cityName;
  MomentLocationInput? toLocation({String? id}) {
    if (referenceID.isNotEmpty)
      return MomentLocationInput(
        id: id ?? newMomentOperationID(),
        kind: 'REFERENCE',
        referenceID: referenceID,
        precision: 'place',
      );
    if (cityID.isEmpty && placeID.isEmpty) return null;
    return MomentLocationInput(
      id: id ?? newMomentOperationID(),
      kind: placeID.isNotEmpty ? 'PLACE' : 'CITY',
      cityID: placeID.isEmpty ? cityID : '',
      placeID: placeID,
      precision: placeID.isEmpty ? 'city' : 'place',
    );
  }
}

/// Explicit author selection; the exploration city is never a hard filter.
class MomentLocationPicker extends StatefulWidget {
  const MomentLocationPicker({
    super.key,
    required this.client,
    required this.baseUrl,
    required this.current,
  });
  final http.Client client;
  final String baseUrl;
  final bool Function() current;
  @override
  State<MomentLocationPicker> createState() => _MomentLocationPickerState();
}

class _MomentLocationPickerState extends State<MomentLocationPicker> {
  final _query = TextEditingController();
  Timer? _debounce;
  int _generation = 0;
  bool _loading = false;
  String? _error, _externalError;
  List<LocationReferenceChoice> _references = [];
  Timer? _referenceLease;
  List<Map<String, dynamic>> _places = [], _cities = [];

  @override
  void initState() {
    super.initState();
    unawaited(_loadCities());
  }

  Future<void> _loadCities() async {
    try {
      final response = await widget.client
          .get(Uri.parse('${widget.baseUrl}/v1/cities'))
          .timeout(const Duration(seconds: 12));
      if (!mounted || !widget.current() || response.statusCode != 200) return;
      final data = jsonDecode(response.body)['data'] as List;
      setState(() => _cities = data.cast<Map<String, dynamic>>());
    } catch (_) {
      /* Place choices remain usable if optional city data fails. */
    }
  }

  Future<void> _searchExternal(int generation, String query) async {
    try {
      final items = await searchLocationReferences(
        widget.client,
        widget.baseUrl,
        query,
      );
      if (!mounted || !widget.current() || generation != _generation) return;
      setState(() => _references = items);
      _referenceLease?.cancel();
      if (items.isNotEmpty) {
        final until = items
            .map((v) => v.validUntil)
            .reduce((a, b) => a.isBefore(b) ? a : b);
        _referenceLease = Timer(until.difference(DateTime.now().toUtc()), () {
          if (mounted && generation == _generation)
            setState(() {
              _references = [];
              _externalError = '地点结果已到期，请重新搜索。';
            });
        });
      }
    } catch (_) {
      if (mounted && widget.current() && generation == _generation)
        setState(() => _externalError = '外部地点暂不可用，仍可不加位置继续。');
    }
  }

  Future<void> _search() async {
    final generation = ++_generation, query = _query.text.trim();
    _referenceLease?.cancel();
    setState(() {
      _references = [];
      _externalError = null;
    });
    if (query.isEmpty) {
      setState(() {
        _places = [];
        _loading = false;
        _error = null;
      });
      return;
    }
    setState(() {
      _loading = true;
      _error = null;
    });
    final external = _searchExternal(generation, query);
    try {
      final uri = Uri.parse(
        '${widget.baseUrl}/v1/places',
      ).replace(queryParameters: {'q': query});
      final response = await widget.client
          .get(uri)
          .timeout(const Duration(seconds: 12));
      if (!mounted || !widget.current() || generation != _generation) return;
      if (response.statusCode != 200) throw const FormatException();
      final data = (jsonDecode(response.body)['data'] as List)
          .cast<Map<String, dynamic>>();
      setState(() => _places = data);
    } catch (_) {
      if (mounted && widget.current() && generation == _generation)
        setState(() => _error = '位置暂时无法读取，可重试或暂不添加。');
    } finally {
      await external;
      if (mounted && generation == _generation)
        setState(() => _loading = false);
    }
  }

  void _choose(MomentLocationChoice value) {
    if (widget.current()) Navigator.pop(context, value);
  }

  String _label(Map<String, dynamic> city) => [
    city['name'],
    city['region'],
    city['countryCode'],
  ].whereType<String>().where((v) => v.isNotEmpty).toSet().join(' · ');

  @override
  Widget build(BuildContext context) {
    final query = _query.text.trim().toLowerCase();
    final cities = query.isEmpty
        ? <Map<String, dynamic>>[]
        : _cities
              .where((c) => _label(c).toLowerCase().contains(query))
              .take(30)
              .toList();
    return Padding(
      padding: EdgeInsets.fromLTRB(
        16,
        8,
        16,
        MediaQuery.viewInsetsOf(context).bottom + 8,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text('添加位置', style: NowUiTokens.of(context).sheetTitle),
          const SizedBox(height: 12),
          TextField(
            controller: _query,
            autofocus: true,
            decoration: const InputDecoration(hintText: '搜索地点或城市'),
            onChanged: (_) {
              ++_generation;
              _debounce?.cancel();
              setState(() {
                _places = [];
                _references = [];
                _externalError = null;
                _referenceLease?.cancel();
                _error = null;
              });
              _debounce = Timer(const Duration(milliseconds: 300), _search);
            },
          ),
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton(
              onPressed: () {
                if (widget.current()) Navigator.pop(context);
              },
              child: const Text('暂不添加'),
            ),
          ),
          if (_loading) const LinearProgressIndicator(),
          if (_error != null)
            Row(
              children: [
                Expanded(child: Text(_error!)),
                TextButton(onPressed: _search, child: const Text('重试')),
              ],
            ),
          if (_externalError != null)
            TextButton(onPressed: _search, child: Text('$_externalError · 重试')),
          Flexible(
            child: ListView(
              shrinkWrap: true,
              children: [
                for (final place in _places)
                  ListTile(
                    title: Text(place['name'] as String),
                    subtitle: Text(
                      [
                        place['addressLabel'] as String? ?? '',
                        ..._cities
                            .where((c) => c['id'] == place['cityId'])
                            .map(_label),
                      ].where((v) => v.isNotEmpty).join(' · '),
                    ),
                    onTap: () => _choose(
                      MomentLocationChoice(
                        // Selecting a Place does not make its parent City compulsory.
                        placeID: place['id'] as String,
                        placeName: place['name'] as String,
                        displayLabel: [
                          place['name'] as String,
                          place['addressLabel'] as String? ?? '',
                          ..._cities
                              .where((c) => c['id'] == place['cityId'])
                              .map(_label),
                        ].where((v) => v.isNotEmpty).toSet().join(' · '),
                      ),
                    ),
                  ),
                for (final ref in _references)
                  ListTile(
                    leading: const Icon(Icons.place_outlined),
                    title: Text(ref.name),
                    subtitle: Text('${ref.address}\n${ref.attribution}'),
                    onTap: () => ref.current
                        ? _choose(
                            MomentLocationChoice(
                              referenceID: ref.id,
                              placeName: ref.name,
                              displayLabel: ref.address.isEmpty
                                  ? ref.name
                                  : ref.address,
                            ),
                          )
                        : null,
                  ),
                for (final city in cities)
                  ListTile(
                    leading: const Icon(Icons.location_city_outlined),
                    title: Text(_label(city)),
                    onTap: () => _choose(
                      MomentLocationChoice(
                        cityID: city['id'] as String,
                        cityName: city['name'] as String,
                        displayLabel: _label(city),
                      ),
                    ),
                  ),
                if (query.isNotEmpty &&
                    !_loading &&
                    _error == null &&
                    _places.isEmpty &&
                    _references.isEmpty &&
                    _externalError == null &&
                    cities.isEmpty)
                  const Padding(
                    padding: EdgeInsets.all(16),
                    child: Text('没有找到已收录的位置，可以不添加位置继续。'),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  @override
  void dispose() {
    ++_generation;
    _debounce?.cancel();
    _referenceLease?.cancel();
    _query.dispose();
    super.dispose();
  }
}
