import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/booking_analytics_api.dart';
import 'package:birdtie_client/src/workspace/verified_booking_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'verified_booking_controller_test.dart'
    show bookingData, bookingReply, bookingNow;

Map<String, dynamic> eventReceipt(http.Request r) => {
  'schemaVersion': 'booking-external-event-v1',
  'eventId': jsonDecode(r.body)['eventId'],
  'placeId': 'place',
  'eventType': 'EXTERNAL_BOOKING_CLICK',
  'outcome': 'CLIENT_REPORTED_EXTERNAL_OPEN',
  'recordedAt': '2026-10-03T20:00:00+08:00',
  'confirmedCapability': 'UNAVAILABLE',
  'confirmedBooking': 'UNKNOWN',
};
void main() {
  test(
    'fresh read may renew its own timestamp without renewing old confirmation',
    () async {
      var reads = 0, reports = 0, opens = 0;
      final c = VerifiedBookingController(
        placeID: 'place',
        authorizationHeader: () => null,
        now: () => bookingNow,
        apiBaseUrl: 'https://api.test',
        client: MockClient((r) async {
          if (r.method == 'GET') {
            reads++;
            final data = bookingData();
            if (reads == 2) {
              data['bookingSourceVersion'] = 'b' * 64;
              data['bookingValidUntil'] = bookingNow
                  .add(const Duration(seconds: 29))
                  .toIso8601String();
            }
            return bookingReply(data);
          }
          reports++;
          expect(jsonDecode(r.body)['sourceVersion'], 'a' * 64);
          expect(
            jsonDecode(r.body)['validUntil'],
            bookingNow.add(const Duration(seconds: 30)).toIso8601String(),
          );
          return http.Response(jsonEncode({'data': eventReceipt(r)}), 200);
        }),
        openExternal: (_) async {
          opens++;
          return true;
        },
      );
      c.adopt(bookingData());
      final p = (await c.prepare())!;
      expect(await c.approve(p), true);
      expect(opens, 1);
      expect(reports, 1);
      c.dispose();
    },
  );
  test(
    'closed external report never contains identity URL or provider fact',
    () async {
      var calls = 0;
      final client = MockClient((r) async {
        calls++;
        expect(r.method, 'POST');
        final body = jsonDecode(r.body) as Map;
        expect(body.keys.toSet(), {
          'eventId',
          'eventType',
          'outcome',
          'sourceVersion',
          'validUntil',
        });
        expect(r.body, isNot(contains('https://')));
        expect(r.headers.containsKey('Authorization'), false);
        return http.Response(jsonEncode({'data': eventReceipt(r)}), 200);
      });
      await BookingAnalyticsApi(
        client: client,
        base: 'https://api.test',
      ).report(
        placeID: 'place',
        eventID: BookingAnalyticsApi.newEventID(),
        sourceVersion: 'a' * 64,
        validUntil: bookingNow.add(const Duration(seconds: 30)),
        token: null,
      );
      expect(calls, 1);
    },
  );
  for (final bad in [
    '2026-13-01T12:00:00Z',
    '2026-02-30T12:00:00Z',
    '2026-10-03T25:00:00Z',
    '2026-10-03T12:00:00+24:00',
    '2026-10-03T12:00:00',
    '0001-01-01T00:00:00+23:00',
  ]) {
    test('strict PG receipt timestamp rejects $bad', () {
      expect(BookingAnalyticsApi.validStamp(bad), false);
    });
  }
  for (final outcome in ['PROVIDER_CONFIRMED', 'UNKNOWN']) {
    test('telemetry response cannot confirm provider $outcome', () async {
      final api = BookingAnalyticsApi(
        client: MockClient((r) async {
          final data = eventReceipt(r)..['confirmedBooking'] = outcome;
          return http.Response(jsonEncode({'data': data}), 200);
        }),
        base: 'https://api.test',
      );
      if (outcome == 'UNKNOWN') {
        await api.report(
          placeID: 'place',
          eventID: BookingAnalyticsApi.newEventID(),
          sourceVersion: 'a' * 64,
          validUntil: bookingNow.add(const Duration(seconds: 30)),
          token: null,
        );
      } else {
        await expectLater(
          api.report(
            placeID: 'place',
            eventID: BookingAnalyticsApi.newEventID(),
            sourceVersion: 'a' * 64,
            validUntil: bookingNow.add(const Duration(seconds: 30)),
            token: null,
          ),
          throwsFormatException,
        );
      }
    });
  }
  for (final kind in [
    'positive',
    'cancel',
    'false',
    'throw',
    'lateIdentity',
    'expiry',
    'changed',
    'statsFailure',
  ]) {
    test('booking open and telemetry boundary $kind', () async {
      var opens = 0, reports = 0, reads = 0, epoch = 0;
      var now = bookingNow;
      final c = VerifiedBookingController(
        placeID: 'place',
        authorizationHeader: () => null,
        authorityEpoch: () => epoch,
        now: () => now,
        apiBaseUrl: 'https://api.test',
        client: MockClient((r) async {
          if (r.method == 'GET') {
            reads++;
            final data = bookingData();
            if (kind == 'changed' && reads == 2) {
              data['bookingSourceRevision'] = 'b' * 64;
            }
            return bookingReply(data);
          }
          reports++;
          expect(opens, 1);
          expect(jsonDecode(r.body)['eventType'], 'EXTERNAL_BOOKING_CLICK');
          return kind == 'statsFailure'
              ? http.Response('{}', 503)
              : http.Response(jsonEncode({'data': eventReceipt(r)}), 200);
        }),
        openExternal: (_) async {
          opens++;
          if (kind == 'throw') throw StateError('synthetic');
          if (kind == 'lateIdentity') epoch++;
          if (kind == 'expiry') {
            now = bookingNow.add(const Duration(seconds: 31));
          }
          return kind != 'false';
        },
      );
      c.adopt(bookingData());
      final p = (await c.prepare())!;
      if (kind == 'cancel') {
        c.cancel(p);
        expect(await c.approve(p), false);
      } else {
        final success = await c.approve(p);
        expect(success, kind == 'positive' || kind == 'statsFailure');
      }
      expect(opens, kind == 'cancel' || kind == 'changed' ? 0 : 1);
      expect(reports, kind == 'positive' || kind == 'statsFailure' ? 1 : 0);
      if (kind == 'statsFailure') {
        expect(c.telemetryNotice, contains('已打开；外跳统计未确认'));
      }
      c.dispose();
    });
  }
  test('unknown telemetry response never retries or reopens outside', () async {
    final pending = Completer<http.Response>();
    var opens = 0, reports = 0;
    final c = VerifiedBookingController(
      placeID: 'place',
      authorizationHeader: () => null,
      now: () => bookingNow,
      apiBaseUrl: 'https://api.test',
      client: MockClient((r) async {
        if (r.method == 'GET') return bookingReply(bookingData());
        reports++;
        return pending.future;
      }),
      openExternal: (_) async {
        opens++;
        return true;
      },
    );
    c.adopt(bookingData());
    final p = (await c.prepare())!;
    final done = c.approve(p);
    await Future<void>.delayed(Duration.zero);
    expect(await c.approve(p), false);
    pending.completeError(StateError('synthetic unknown'));
    expect(await done, true);
    expect(opens, 1);
    expect(reports, 1);
    expect(c.telemetryNotice, contains('统计未确认'));
    c.dispose();
  });
}
