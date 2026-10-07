import 'dart:async';
import 'dart:convert';
import 'dart:math';
import 'package:http/http.dart' as http;

/// A supplier transaction is unavailable. This report is only untrusted client
/// telemetry about an external open, and never a booking success receipt.
class BookingAnalyticsApi {
  BookingAnalyticsApi({required this._client, required String base})
    : _base = base.replaceFirst(RegExp(r'/$'), '');
  final http.Client _client;
  final String _base;
  static bool validStamp(dynamic raw) {
    if (raw is! String ||
        !RegExp(
          r'^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?(Z|[+-]\d{2}:\d{2})$',
        ).hasMatch(raw)) {
      return false;
    }
    final y = int.parse(raw.substring(0, 4)),
        m = int.parse(raw.substring(5, 7)),
        d = int.parse(raw.substring(8, 10));
    final date = DateTime.utc(y, m, d);
    if (y < 1 ||
        date.year != y ||
        date.month != m ||
        date.day != d ||
        int.parse(raw.substring(11, 13)) > 23 ||
        int.parse(raw.substring(14, 16)) > 59 ||
        int.parse(raw.substring(17, 19)) > 59) {
      return false;
    }
    if (!raw.endsWith('Z')) {
      final offset = raw.substring(raw.length - 6);
      if (int.parse(offset.substring(1, 3)) > 23 ||
          int.parse(offset.substring(4, 6)) > 59) {
        return false;
      }
    }
    final parsed = DateTime.parse(raw).toUtc();
    return parsed.year >= 1 && parsed.year <= 9999;
  }

  static String newEventID() {
    final random = Random.secure();
    final bytes = List<int>.generate(16, (_) => random.nextInt(256));
    bytes[6] = (bytes[6] & 15) | 64;
    bytes[8] = (bytes[8] & 63) | 128;
    final hex = bytes.map((v) => v.toRadixString(16).padLeft(2, '0')).join();
    return '${hex.substring(0, 8)}-${hex.substring(8, 12)}-${hex.substring(12, 16)}-${hex.substring(16, 20)}-${hex.substring(20)}';
  }

  Future<void> report({
    required String placeID,
    required String eventID,
    required String sourceVersion,
    required DateTime validUntil,
    required String? token,
  }) async {
    if (!RegExp(r'^[0-9a-f]{64}$').hasMatch(sourceVersion)) {
      throw const FormatException();
    }
    final response = await _client
        .post(
          Uri.parse(
            '$_base/v1/places/${Uri.encodeComponent(placeID)}/booking-events',
          ),
          headers: {
            'Content-Type': 'application/json',
            'Authorization': ?token,
          },
          body: jsonEncode({
            'eventId': eventID,
            'eventType': 'EXTERNAL_BOOKING_CLICK',
            'outcome': 'CLIENT_REPORTED_EXTERNAL_OPEN',
            'sourceVersion': sourceVersion,
            'validUntil': validUntil.toUtc().toIso8601String(),
          }),
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200 || response.bodyBytes.length > 8192) {
      throw const FormatException();
    }
    final envelope = jsonDecode(utf8.decode(response.bodyBytes));
    final data = envelope is Map<String, dynamic> ? envelope['data'] : null;
    const keys = {
      'schemaVersion',
      'eventId',
      'placeId',
      'eventType',
      'outcome',
      'recordedAt',
      'confirmedCapability',
      'confirmedBooking',
    };
    if (data is! Map<String, dynamic> ||
        data.keys.toSet().difference(keys).isNotEmpty ||
        keys.difference(data.keys.toSet()).isNotEmpty ||
        data['schemaVersion'] != 'booking-external-event-v1' ||
        data['eventId'] != eventID ||
        data['placeId'] != placeID ||
        data['eventType'] != 'EXTERNAL_BOOKING_CLICK' ||
        data['outcome'] != 'CLIENT_REPORTED_EXTERNAL_OPEN' ||
        data['confirmedCapability'] != 'UNAVAILABLE' ||
        data['confirmedBooking'] != 'UNKNOWN' ||
        !validStamp(data['recordedAt'])) {
      throw const FormatException();
    }
  }
}
