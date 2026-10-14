import 'dart:async';
import 'dart:convert';

import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import 'map_entities.dart';
import 'now_ui_tokens.dart';

const onlinePlaceSearchSchema = 'birdtie-online-places-v1';
const durableOnlinePlaceSearchSchema = 'birdtie-online-places-v2';
const externalPlaceHistoryNotice = '外部结果仅本轮有效，请重新搜索查看最新信息。';
const openStreetMapAttribution = '© OpenStreetMap contributors (ODbL 1.0)';

/// A source observation belongs to this live request, never a native Place.
class ExternalPlaceCandidate {
  const ExternalPlaceCandidate({
    required this.id,
    required this.cityID,
    required this.name,
    required this.addressLabel,
    required this.latitude,
    required this.longitude,
    required this.sourceUri,
    required this.observedAt,
    required this.expiresAt,
  });
  final String id, cityID, name, addressLabel;
  final double latitude, longitude;
  final Uri sourceUri;
  final DateTime observedAt, expiresAt;
  String get mapID => 'external-place:$id';
  MapEntity get mapEntity => MapEntity(
    id: mapID,
    kind: MapEntityKind.place,
    title: name,
    subtitle: '联网来源 · 餐厅 · 信息待核实',
    latitude: latitude,
    longitude: longitude,
    publicLocation: true,
    isTaskResult: true,
  );
  Map<String, dynamic> toJson() => {
    'id': id,
    'cityId': cityID,
    'name': name,
    'categoryCode': 'restaurant',
    'addressLabel': addressLabel,
    'location': {
      'precision': 'point',
      'coordinateSystem': 'wgs84',
      'latitude': latitude,
      'longitude': longitude,
    },
    'sourceUrl': sourceUri.toString(),
    'attribution': openStreetMapAttribution,
    'observedAt': observedAt.toIso8601String(),
    'expiresAt': expiresAt.toIso8601String(),
    'reviewStatus': 'UNREVIEWED',
  };
}

/// Strict same-round binding with both wall-clock and monotonic expiry.
class ExternalPlaceSearch {
  ExternalPlaceSearch._({
    required this.requestID,
    required this.cityID,
    required this.query,
    required this.status,
    required this.scope,
    required this.observedAt,
    required this.expiresAt,
    required this.schema,
    this.snapshotID = '',
    this.taskID = '',
    this.turnDigest = '',
    this.messageIndex = -1,
    this.cacheVersion = 0,
    this.origin = 'LIVE',
    this.freshness = 'FRESH',
    this.refreshFailed = false,
    required List<ExternalPlaceCandidate> places,
    required bool Function() sourceCurrent,
    required DateTime Function() clock,
  }) : _places = List.unmodifiable(places),
       _current = sourceCurrent,
       _now = clock,
       _lease = expiresAt.difference(clock().toUtc());

  final String requestID, cityID, query, status, scope;
  final String schema, snapshotID, taskID, turnDigest, origin, freshness;
  final int messageIndex, cacheVersion;
  final bool refreshFailed;
  bool get durable => schema == durableOnlinePlaceSearchSchema;
  Uri get licenseUri =>
      Uri.parse('https://opendatacommons.org/licenses/odbl/1-0/');
  final DateTime observedAt, expiresAt;
  final List<ExternalPlaceCandidate> _places;
  final bool Function() _current;
  final DateTime Function() _now;
  final Duration _lease;
  final Stopwatch _watch = Stopwatch()..start();
  bool _retired = false;
  Duration get remaining => _lease - _watch.elapsed;
  bool get stale =>
      freshness == 'STALE' ||
      !_now().toUtc().isBefore(expiresAt) ||
      remaining <= Duration.zero;
  bool get current {
    final now = _now().toUtc();
    if (_retired ||
        !_current() ||
        now.isBefore(observedAt.subtract(const Duration(seconds: 2))) ||
        (!durable &&
            (!now.isBefore(expiresAt) || remaining <= Duration.zero))) {
      _retired = true;
    }
    return !_retired;
  }

  void retire() => _retired = true;
  List<ExternalPlaceCandidate> get places => current ? _places : const [];

  Map<String, dynamic> toJson() => {
    'schema': schema,
    'requestId': requestID,
    'cityId': cityID,
    'query': query,
    'status': status,
    'provider': 'openstreetmap',
    'attribution': openStreetMapAttribution,
    'scope': scope,
    'observedAt': observedAt.toIso8601String(),
    'expiresAt': expiresAt.toIso8601String(),
    'places': [for (final place in _places) place.toJson()],
    if (durable) ...{
      'snapshotId': snapshotID,
      'taskId': taskID,
      'turnDigest': turnDigest,
      'messageIndex': messageIndex,
      'cacheVersion': cacheVersion,
      'origin': origin,
      'freshness': stale ? 'STALE' : freshness,
      'licenseUrl': licenseUri.toString(),
      if (refreshFailed) 'refreshFailed': true,
    },
  };

  static ExternalPlaceSearch? read(
    Object? raw, {
    required String requestID,
    required String cityID,
    required String query,
    required bool Function() current,
    required DateTime Function() now,
    MapBounds? bounds,
    String? expectedTaskID,
    int? expectedMessageIndex,
    bool history = false,
  }) {
    if (raw == null) return null;
    void require(bool condition) {
      if (!condition) throw const FormatException('联网地点来源无法核验');
    }

    bool text(Object? value, int maximum, {bool empty = false}) =>
        value is String &&
        value.trim() == value &&
        (empty || value.isNotEmpty) &&
        value.runes.length <= maximum &&
        !value.runes.any((rune) => rune < 32 || rune == 127);
    DateTime time(Object? value) {
      require(
        value is String &&
            RegExp(
              r'^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?Z$',
            ).hasMatch(value),
      );
      final parsed = DateTime.tryParse(value as String);
      require(parsed != null && parsed.isUtc && parsed.year > 1);
      return parsed!;
    }

    require(raw is Map<String, dynamic>);
    final value = raw as Map<String, dynamic>;
    final durable = value['schema'] == durableOnlinePlaceSearchSchema;
    require(
      value.keys.toSet().difference(const {
            'schema',
            'requestId',
            'cityId',
            'query',
            'status',
            'provider',
            'attribution',
            'scope',
            'observedAt',
            'expiresAt',
            'places',
            'snapshotId',
            'taskId',
            'turnDigest',
            'messageIndex',
            'cacheVersion',
            'origin',
            'freshness',
            'licenseUrl',
            'refreshFailed',
          }).isEmpty &&
          (durable ||
              (!history && value['schema'] == onlinePlaceSearchSchema)) &&
          value['requestId'] == requestID &&
          value['cityId'] == cityID &&
          value['query'] == (durable ? query : query.trim()) &&
          value['provider'] == 'openstreetmap' &&
          value['attribution'] == openStreetMapAttribution &&
          const {'SUCCESS', 'EMPTY', 'UNAVAILABLE'}.contains(value['status']) &&
          const {'city', 'viewport'}.contains(value['scope']) &&
          value['places'] is List &&
          (value['places'] as List).length <= (durable ? 15 : 20),
    );
    if (durable) {
      require(
        text(requestID, 200) &&
            text(cityID, 200) &&
            query.isNotEmpty &&
            utf8.encode(query).length <= 240 &&
            value['snapshotId'] is String &&
            RegExp(
              r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
            ).hasMatch(value['snapshotId'] as String) &&
            value['snapshotId'] != '00000000-0000-0000-0000-000000000000' &&
            (value['taskId'] == null || value['taskId'] is String) &&
            ((value['taskId'] ?? '') == '' ||
                RegExp(
                  r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
                ).hasMatch(value['taskId'] as String)) &&
            (expectedTaskID == null ||
                (value['taskId'] ?? '') == expectedTaskID) &&
            value['messageIndex'] is int &&
            value['messageIndex'] >= 0 &&
            (expectedMessageIndex == null ||
                value['messageIndex'] == expectedMessageIndex) &&
            value['turnDigest'] ==
                sha256.convert(utf8.encode(query)).toString() &&
            value['cacheVersion'] is int &&
            value['cacheVersion'] >= 0 &&
            const {'LIVE', 'CACHE', 'HISTORY'}.contains(value['origin']) &&
            const {'FRESH', 'STALE'}.contains(value['freshness']) &&
            value['licenseUrl'] ==
                'https://opendatacommons.org/licenses/odbl/1-0/' &&
            (value['refreshFailed'] == null || value['refreshFailed'] is bool),
      );
    } else {
      require(
        value.keys.toSet().difference(const {
          'schema',
          'requestId',
          'cityId',
          'query',
          'status',
          'provider',
          'attribution',
          'scope',
          'observedAt',
          'expiresAt',
          'places',
        }).isEmpty,
      );
    }
    final observed = time(value['observedAt']);
    final expires = time(value['expiresAt']);
    final clock = now().toUtc();
    require(
      !observed.isAfter(clock.add(const Duration(seconds: 2))) &&
          (durable || expires.isAfter(clock)) &&
          expires.isAfter(observed) &&
          expires.difference(observed) <=
              (durable
                  ? const Duration(hours: 24)
                  : const Duration(minutes: 15)),
    );
    final seen = <String>{};
    final places = <ExternalPlaceCandidate>[];
    for (final rawPlace in value['places'] as List) {
      require(rawPlace is Map<String, dynamic>);
      final place = rawPlace as Map<String, dynamic>;
      require(
        place.keys.toSet().difference(const {
              'id',
              'cityId',
              'name',
              'categoryCode',
              'addressLabel',
              'location',
              'sourceUrl',
              'attribution',
              'observedAt',
              'expiresAt',
              'reviewStatus',
            }).isEmpty &&
            place['id'] is String &&
            (durable
                    ? RegExp(r'^osm:node:[1-9][0-9]{0,18}$')
                    : RegExp(r'^osm:node:[1-9][0-9]{0,19}$'))
                .hasMatch(place['id']) &&
            (!durable ||
                (int.tryParse((place['id'] as String).split(':').last) ?? 0) >
                    0) &&
            seen.add(place['id'] as String) &&
            place['cityId'] == cityID &&
            text(place['name'], 300) &&
            text(place['addressLabel'], 600, empty: true) &&
            (!durable ||
                (utf8.encode(place['name'] as String).length <= 150 &&
                    utf8.encode(place['addressLabel'] as String).length <=
                        240)) &&
            place['categoryCode'] == 'restaurant' &&
            place['reviewStatus'] == 'UNREVIEWED' &&
            place['attribution'] == openStreetMapAttribution &&
            place['sourceUrl'] ==
                'https://www.openstreetmap.org/node/${(place['id'] as String).split(':').last}' &&
            place['location'] is Map<String, dynamic>,
      );
      final location = place['location'] as Map<String, dynamic>;
      require(
        location.length == 4 &&
            location['precision'] == 'point' &&
            location['coordinateSystem'] == 'wgs84' &&
            location['latitude'] is num &&
            location['longitude'] is num,
      );
      final latitude = (location['latitude'] as num).toDouble();
      final longitude = (location['longitude'] as num).toDouble();
      require(
        latitude.isFinite &&
            longitude.isFinite &&
            latitude >= -90 &&
            latitude <= 90 &&
            longitude >= -180 &&
            longitude <= 180 &&
            (value['scope'] != 'viewport' ||
                bounds == null ||
                latitude >= bounds.south &&
                    latitude <= bounds.north &&
                    longitude >= bounds.west &&
                    longitude <= bounds.east) &&
            time(place['observedAt']) == observed &&
            time(place['expiresAt']) == expires,
      );
      places.add(
        ExternalPlaceCandidate(
          id: place['id'] as String,
          cityID: cityID,
          name: place['name'] as String,
          addressLabel: place['addressLabel'] as String,
          latitude: latitude,
          longitude: longitude,
          sourceUri: Uri.parse(place['sourceUrl'] as String),
          observedAt: observed,
          expiresAt: expires,
        ),
      );
    }
    require(value['status'] == 'SUCCESS' ? places.isNotEmpty : places.isEmpty);
    return ExternalPlaceSearch._(
      requestID: requestID,
      cityID: cityID,
      query: durable ? query : query.trim(),
      schema: value['schema'] as String,
      snapshotID: durable ? value['snapshotId'] as String : '',
      taskID: durable ? value['taskId'] as String? ?? '' : '',
      turnDigest: durable ? value['turnDigest'] as String : '',
      messageIndex: durable ? value['messageIndex'] as int : -1,
      cacheVersion: durable ? value['cacheVersion'] as int : 0,
      origin: durable ? value['origin'] as String : 'LIVE',
      freshness: durable ? value['freshness'] as String : 'FRESH',
      refreshFailed: value['refreshFailed'] == true,
      status: value['status'] as String,
      scope: value['scope'] as String,
      observedAt: observed,
      expiresAt: expires,
      places: places,
      sourceCurrent: current,
      clock: now,
    );
  }
}

class ExternalPlaceCandidatesPanel extends StatelessWidget {
  const ExternalPlaceCandidatesPanel({
    super.key,
    required this.search,
    required this.current,
    required this.changes,
    required this.onMap,
    this.onRefresh,
  });
  final ExternalPlaceSearch search;
  final bool Function() current;
  final Listenable changes;
  final ValueChanged<ExternalPlaceCandidate> onMap;
  final VoidCallback? onRefresh;

  @override
  Widget build(BuildContext context) {
    final tokens = NowUiTokens.reviewedOf(context);
    if (!current() ||
        !search.current ||
        (!search.durable && search.status != 'SUCCESS')) {
      return const SizedBox.shrink();
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.only(top: 8, bottom: 8),
          child: Text(
            search.stale
                ? '已保存的餐厅资料 · 信息可能已变化'
                : search.status == 'EMPTY'
                ? '已保存本次空结果'
                : search.status == 'UNAVAILABLE'
                ? '餐厅资料暂时不可用'
                : '餐厅资料 · 尚未核实评分与营业状态',
            style: tokens.metadata,
          ),
        ),
        if (search.durable) ...[
          Text(
            '资料采集：${_observationLabel(search.observedAt)}',
            style: tokens.metadata,
          ),
          if (search.refreshFailed)
            Text('暂时无法更新，已保留上次查到的资料。', style: tokens.body),
          if (onRefresh != null)
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton.icon(
                onPressed: current() ? onRefresh : null,
                icon: const Icon(Icons.refresh),
                label: const Text('更新餐厅信息'),
                style: TextButton.styleFrom(
                  minimumSize: const Size(48, 48),
                  textStyle: tokens.action,
                ),
              ),
            ),
        ],
        for (final place in search.places)
          ExternalPlaceCandidateCard(
            key: ValueKey('external-place-${place.id}'),
            place: place,
            stale: search.stale,
            onOpen: () {
              if (current() && search.places.contains(place)) {
                showExternalPlaceDetails(
                  context,
                  search: search,
                  place: place,
                  current: current,
                  changes: changes,
                );
              }
            },
            onMap: () {
              if (current() && search.places.contains(place)) onMap(place);
            },
          ),
        Text(openStreetMapAttribution, style: tokens.metadata),
        Align(
          alignment: Alignment.centerLeft,
          child: TextButton.icon(
            style: TextButton.styleFrom(
              minimumSize: const Size(48, 48),
              textStyle: tokens.action,
            ),
            onPressed: () async {
              try {
                final opened = await launchUrl(
                  Uri.parse('https://www.openstreetmap.org/copyright'),
                  mode: LaunchMode.externalApplication,
                );
                if (opened || !context.mounted) return;
              } catch (_) {
                if (!context.mounted) return;
              }
              if (context.mounted) {
                ScaffoldMessenger.of(context).showSnackBar(
                  const SnackBar(content: Text('暂时无法打开数据许可，请稍后重试。')),
                );
              }
            },
            icon: const Icon(Icons.open_in_new, size: 18),
            label: const Text('OpenStreetMap 数据许可'),
          ),
        ),
      ],
    );
  }
}

String _observationLabel(DateTime instant) {
  final local = instant.toLocal();
  String two(int number) => number.toString().padLeft(2, '0');
  return '${local.year}-${two(local.month)}-${two(local.day)} '
      '${two(local.hour)}:${two(local.minute)}';
}

/// Every restaurant uses the same card, including the selected map preview.
/// No action accepts its OSM identity as a native Birdtie Place ID.
class ExternalPlaceCandidateCard extends StatelessWidget {
  const ExternalPlaceCandidateCard({
    super.key,
    required this.place,
    required this.onOpen,
    this.onMap,
    this.stale = false,
  });
  final ExternalPlaceCandidate place;
  final VoidCallback onOpen;
  final VoidCallback? onMap;
  final bool stale;
  @override
  Widget build(BuildContext context) {
    final tokens = NowUiTokens.reviewedOf(context);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Material(
        color: Theme.of(context).colorScheme.surface,
        elevation: 1,
        borderRadius: BorderRadius.circular(18),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: onOpen,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 12, 12, 4),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(place.name, style: tokens.placeTitle),
                          const SizedBox(height: 4),
                          Text(
                            place.addressLabel.isEmpty
                                ? '地址待核实'
                                : place.addressLabel,
                            style: tokens.address,
                          ),
                          const SizedBox(height: 6),
                          Text(
                            stale
                                ? 'OpenStreetMap · 资料需更新'
                                : 'OpenStreetMap · 外部来源',
                            style: tokens.metadata,
                          ),
                        ],
                      ),
                    ),
                    Padding(
                      padding: const EdgeInsets.only(left: 12, top: 2),
                      child: ExcludeSemantics(
                        child: Icon(
                          Icons.restaurant_outlined,
                          size: 22,
                          color: Theme.of(context).colorScheme.primary,
                        ),
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 4),
                Wrap(
                  alignment: WrapAlignment.end,
                  spacing: 4,
                  children: [
                    if (onMap != null)
                      TextButton.icon(
                        style: TextButton.styleFrom(
                          minimumSize: const Size(48, 48),
                          textStyle: tokens.action,
                        ),
                        onPressed: onMap,
                        icon: const Icon(Icons.map_outlined, size: 18),
                        label: const Text('地图查看'),
                      ),
                    TextButton.icon(
                      style: TextButton.styleFrom(
                        minimumSize: const Size(48, 48),
                        textStyle: tokens.action,
                      ),
                      onPressed: onOpen,
                      icon: const Icon(Icons.chevron_right, size: 18),
                      label: const Text('查看详情'),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

Future<void> showExternalPlaceDetails(
  BuildContext context, {
  required ExternalPlaceSearch search,
  required ExternalPlaceCandidate place,
  required bool Function() current,
  required Listenable changes,
}) async {
  if (!current() || !search.places.contains(place)) return;
  await showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    useSafeArea: true,
    builder: (_) => _ExternalPlaceDetails(
      search: search,
      place: place,
      current: current,
      changes: changes,
    ),
  );
}

class _ExternalPlaceDetails extends StatefulWidget {
  const _ExternalPlaceDetails({
    required this.search,
    required this.place,
    required this.current,
    required this.changes,
  });
  final ExternalPlaceSearch search;
  final ExternalPlaceCandidate place;
  final bool Function() current;
  final Listenable changes;
  @override
  State<_ExternalPlaceDetails> createState() => _ExternalPlaceDetailsState();
}

class _ExternalPlaceDetailsState extends State<_ExternalPlaceDetails> {
  Timer? _expiry;
  @override
  void initState() {
    super.initState();
    widget.changes.addListener(_changed);
    final remaining = widget.search.remaining;
    if (remaining > Duration.zero) _expiry = Timer(remaining, _changed);
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  @override
  void dispose() {
    widget.changes.removeListener(_changed);
    _expiry?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final tokens = NowUiTokens.reviewedOf(context);
    final current = widget.current() && widget.search.current;
    final place = widget.place;
    return SafeArea(
      child: DefaultTextStyle.merge(
        style: tokens.body,
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(20),
          child: current
              ? Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Expanded(
                          child: Text(place.name, style: tokens.sheetTitle),
                        ),
                        IconButton(
                          tooltip: '关闭地点详情',
                          onPressed: () => Navigator.of(context).pop(),
                          icon: const Icon(Icons.close),
                          constraints: const BoxConstraints(
                            minWidth: 48,
                            minHeight: 48,
                          ),
                        ),
                      ],
                    ),
                    Text('餐厅 · OpenStreetMap', style: tokens.metadata),
                    if (widget.search.durable) ...[
                      const SizedBox(height: 8),
                      Text(
                        '资料采集：${_observationLabel(widget.search.observedAt)}',
                        style: tokens.metadata,
                      ),
                      if (widget.search.stale)
                        const Text('这些资料可能已变化，可回到对话更新餐厅信息。'),
                    ],
                    if (place.addressLabel.isNotEmpty) ...[
                      const SizedBox(height: 8),
                      Text('地址', style: tokens.placeTitle),
                      const SizedBox(height: 4),
                      SelectableText(place.addressLabel, style: tokens.body),
                    ],
                    const SizedBox(height: 8),
                    const Text('评分、营业时间和服务信息尚未核实，请查看原始来源。'),
                    const SizedBox(height: 16),
                    Text(openStreetMapAttribution, style: tokens.metadata),
                    const SizedBox(height: 4),
                    Text(
                      'OpenStreetMap 外部来源快照 · 尚未发布到 Birdtie',
                      style: tokens.metadata,
                    ),
                    const SizedBox(height: 8),
                    TextButton.icon(
                      style: TextButton.styleFrom(
                        minimumSize: const Size(48, 48),
                        textStyle: tokens.action,
                      ),
                      onPressed: () async {
                        if (!widget.current() || !widget.search.current) return;
                        var opened = false;
                        try {
                          opened = await launchUrl(
                            place.sourceUri,
                            mode: LaunchMode.externalApplication,
                          );
                        } catch (_) {
                          // An unavailable browser still has a local recovery.
                        }
                        if (!opened && context.mounted) {
                          ScaffoldMessenger.of(context).showSnackBar(
                            const SnackBar(content: Text('暂时无法打开来源，请稍后重试。')),
                          );
                        }
                      },
                      icon: const Icon(Icons.open_in_new),
                      label: const Text('打开原始来源'),
                    ),
                  ],
                )
              : const Text('这次联网结果已失效，请重新搜索查看最新信息。'),
        ),
      ),
    );
  }
}
