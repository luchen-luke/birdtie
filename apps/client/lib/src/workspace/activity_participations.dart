import 'dart:convert';
import 'model_egress_api.dart' show egressTime;

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';

class ActivityParticipation {
  const ActivityParticipation({
    required this.id,
    required this.activityId,
    required this.status,
    required this.title,
    required this.cityId,
    required this.startsAt,
    required this.endsAt,
    required this.activityStatus,
    required this.available,
    this.modality = 'unspecified',
    this.physicalPlaceStatus = 'unknown',
    this.placeId = '',
    this.placeName = '',
    this.venuePlaceId = '',
    this.timeZone = '',
  });

  final String id;
  final String activityId;
  final String status;
  final String title;
  final String cityId;
  final DateTime? startsAt;
  final DateTime? endsAt;
  final String activityStatus;
  final bool available;
  final String modality,
      physicalPlaceStatus,
      placeId,
      placeName,
      venuePlaceId,
      timeZone;

  bool get joined => status == 'going' || status == 'pending';
  bool get past =>
      activityStatus == 'completed' || activityStatus == 'cancelled';

  factory ActivityParticipation.fromJson(Map<String, dynamic> json) =>
      ActivityParticipation(
        id: json['id'] as String,
        activityId: json['activityId'] as String,
        status: json['status'] as String,
        title: json['title'] as String? ?? '',
        cityId: json['cityId'] as String? ?? '',
        startsAt: plansNullableTime(json['startsAt']),
        endsAt: plansNullableTime(json['endsAt']),
        activityStatus: json['activityStatus'] as String? ?? 'unavailable',
        available: json['available'] as bool? ?? false,
        modality: json['modality'] as String? ?? 'unspecified',
        physicalPlaceStatus:
            json['physicalPlaceStatus'] as String? ?? 'unknown',
        placeId: json['placeId'] as String? ?? '',
        placeName: json['placeName'] as String? ?? '',
        venuePlaceId: json['venuePlaceId'] as String? ?? '',
        timeZone: json['timeZone'] as String? ?? '',
      );
}

class ActivityParticipationController extends ChangeNotifier {
  ActivityParticipationController({
    required this.authorizationHeader,
    http.Client? client,
    String? apiBaseUrl,
    this.identityChanges,
  }) : _client = client ?? http.Client(),
       _apiBaseUrl = apiBaseUrl ?? apiBase,
       _ownsClient = client == null {
    identityChanges?.addListener(clear);
  }

  static const apiBase = BirdtieEnvironment.apiBaseUrl;
  final String? Function() authorizationHeader;
  final http.Client _client;
  final bool _ownsClient;
  final Listenable? identityChanges;
  bool _disposed = false;
  final String _apiBaseUrl;
  List<ActivityParticipation> items = const [];
  bool loading = false;
  bool failed = false;
  int _serial = 0;

  bool get configured => _apiBaseUrl.isNotEmpty;

  void clear({bool notify = true}) {
    if (_disposed) return;
    ++_serial;
    items = const [];
    loading = false;
    failed = false;
    if (notify) notifyListeners();
  }

  Future<void> load() async {
    if (_disposed) return;
    final auth = authorizationHeader();
    if (!configured || auth == null) {
      clear();
      return;
    }
    final serial = ++_serial;
    loading = true;
    failed = false;
    notifyListeners();
    if (_disposed || serial != _serial || auth != authorizationHeader()) return;
    try {
      final uri = Uri.parse(
        '${_apiBaseUrl.replaceFirst(RegExp(r'/$'), '')}/v1/me/participations',
      );
      final response = await _client
          .get(uri, headers: {'Authorization': auth})
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) {
        throw StateError('Participations unavailable');
      }
      final rows =
          (jsonDecode(response.body) as Map<String, dynamic>)['data']
              as List<dynamic>;
      if (_disposed || serial != _serial || auth != authorizationHeader()) {
        return;
      }
      items = [
        for (final row in rows)
          ActivityParticipation.fromJson(row as Map<String, dynamic>),
      ];
    } catch (_) {
      if (_disposed || serial != _serial || auth != authorizationHeader()) {
        return;
      }
      failed = true;
      items = const [];
    } finally {
      if (!_disposed && serial == _serial && auth == authorizationHeader()) {
        loading = false;
        notifyListeners();
      }
    }
  }

  @override
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    identityChanges?.removeListener(clear);
    ++_serial;
    if (_ownsClient) _client.close();
    super.dispose();
  }
}

// Parser reuse is lexical RFC3339 only; it grants no model/export permission.
DateTime? plansNullableTime(dynamic value) =>
    value == null ? null : egressTime(value);

String plansLocation(String modality, String status, String name) {
  final place = status == 'tbd'
      ? '地点待定'
      : name.isNotEmpty
      ? name
      : '地点暂不可用';
  return switch (modality) {
    'online' => '线上活动 · 进入方式请查看活动详情',
    'hybrid' => '线上与线下 · $place',
    'in_person' => '线下活动 · $place',
    _ => '活动方式待确认',
  };
}

String plansSchedule(DateTime? starts, DateTime? ends, String activityZone) {
  if (starts == null) return '时间待确认';
  String stamp(DateTime t) {
    final local = t.toLocal(), offset = t.toLocal().timeZoneOffset.inMinutes;
    String two(int n) => n.toString().padLeft(2, '0');
    final z =
        '${offset >= 0 ? '+' : '-'}${two(offset.abs() ~/ 60)}:${two(offset.abs() % 60)}';
    return '${local.year}年${local.month}月${local.day}日 ${two(local.hour)}:${two(local.minute)} UTC$z';
  }

  return '${stamp(starts)}${ends == null ? '' : ' 至 ${stamp(ends)}'}\n你的设备时间${activityZone.isEmpty ? ' · 活动时区待确认' : ' · 活动时区 $activityZone（未换算）'}';
}
