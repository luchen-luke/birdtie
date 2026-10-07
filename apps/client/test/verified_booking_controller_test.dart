import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/verified_booking_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

final bookingNow = DateTime.utc(2026, 10, 3, 12);
Map<String, dynamic> bookingData({
  String support = 'external_url',
  String url = 'https://example.org/book',
}) => {
  'placeId': 'place',
  'reservationSupport': support,
  if (support == 'external_url') 'reservationUrl': url,
  'sourceUrl': 'https://example.org/source',
  'reviewedAt': '2026-10-02T12:00:00Z',
  'expiresAt': '2026-10-04T12:00:00Z',
  'bookingSourceVersion':
      'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
  'bookingSourceRevision':
      'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
  'bookingObservedAt': '2026-10-03T12:00:00Z',
  'bookingValidUntil': '2026-10-03T12:00:30Z',
};
http.Response bookingReply(Map<String, dynamic> data) =>
    http.Response(jsonEncode({'data': data}), 200);

void main() {
  test(
    'dispose after preview allows harmless cancel and rejects approval',
    () async {
      var opens = 0;
      final c = VerifiedBookingController(
        placeID: 'place',
        authorizationHeader: () => null,
        now: () => bookingNow,
        apiBaseUrl: 'https://api.test',
        client: MockClient((_) async => bookingReply(bookingData())),
        openExternal: (_) async {
          opens++;
          return true;
        },
      );
      c.adopt(bookingData());
      final p = (await c.prepare())!;
      c.dispose();
      c.cancel(p);
      expect(await c.approve(p), isFalse);
      expect(opens, 0);
    },
  );
  test(
    'identity change during launcher cannot return old success receipt',
    () async {
      String? token = 'Bearer A';
      final pending = Completer<bool>();
      var opens = 0;
      final c = VerifiedBookingController(
        placeID: 'place',
        authorizationHeader: () => token,
        now: () => bookingNow,
        apiBaseUrl: 'https://api.test',
        client: MockClient((_) async => bookingReply(bookingData())),
        openExternal: (_) async {
          opens++;
          return pending.future;
        },
      );
      addTearDown(c.dispose);
      c.adopt(bookingData());
      final p = (await c.prepare())!;
      final operation = c.approve(p);
      await Future<void>.delayed(Duration.zero);
      expect(opens, 1);
      token = 'Bearer B';
      c.syncIdentity();
      pending.complete(true);
      expect(await operation, isFalse);
      expect(c.metadata, isNull);
    },
  );
  test('workspace ABA epoch cannot revive an old approval', () async {
    String? workspace;
    var epoch = 0, opens = 0;
    final c = VerifiedBookingController(
      placeID: 'place',
      authorizationHeader: () => null,
      workspaceID: () => workspace,
      authorityEpoch: () => epoch,
      now: () => bookingNow,
      apiBaseUrl: 'https://api.test',
      client: MockClient((_) async => bookingReply(bookingData())),
      openExternal: (_) async {
        opens++;
        return true;
      },
    );
    addTearDown(c.dispose);
    c.adopt(bookingData());
    final p = (await c.prepare())!;
    workspace = 'org';
    epoch++;
    c.invalidate();
    workspace = null;
    epoch++;
    c.invalidate();
    expect(await c.approve(p), isFalse);
    expect(opens, 0);
  });
  for (final status in [401, 403, 500]) {
    test('HTTP $status rejects cached booking metadata', () async {
      var opens = 0;
      final c = VerifiedBookingController(
        placeID: 'place',
        authorizationHeader: () => null,
        now: () => bookingNow,
        apiBaseUrl: 'https://api.test',
        client: MockClient((_) async => http.Response('{}', status)),
        openExternal: (_) async {
          opens++;
          return true;
        },
      );
      addTearDown(c.dispose);
      c.adopt(bookingData());
      expect(await c.prepare(), isNull);
      expect(c.metadata, isNull);
      expect(opens, 0);
    });
  }
  test('re-reads twice and consumes a specific preview once', () async {
    var reads = 0, opens = 0;
    final c = VerifiedBookingController(
      placeID: 'place',
      authorizationHeader: () => null,
      now: () => bookingNow,
      apiBaseUrl: 'https://api.test',
      client: MockClient((r) async {
        expect(r.method, 'GET');
        expect(r.url.path, '/v1/places/place/venue');
        reads++;
        return bookingReply(bookingData());
      }),
      openExternal: (uri) async {
        expect(uri.toString(), 'https://example.org/book');
        opens++;
        return true;
      },
    );
    addTearDown(c.dispose);
    c.adopt(bookingData());
    final p = (await c.prepare())!;
    expect(opens, 0);
    expect(await c.approve(p), isTrue);
    expect(await c.approve(p), isFalse);
    expect(reads, 2);
    expect(opens, 1);
  });
  test('updated URL needs fresh preview and confirmation', () async {
    var data = bookingData(), opens = 0;
    final c = VerifiedBookingController(
      placeID: 'place',
      authorizationHeader: () => null,
      now: () => bookingNow,
      apiBaseUrl: 'https://api.test',
      client: MockClient((_) async => bookingReply(data)),
      openExternal: (_) async {
        opens++;
        return true;
      },
    );
    addTearDown(c.dispose);
    c.adopt(data);
    final old = (await c.prepare())!;
    data = bookingData(url: 'https://example.org/new');
    expect(await c.approve(old), isFalse);
    expect(opens, 0);
    expect(c.error, contains('重新确认'));
    final fresh = (await c.prepare())!;
    expect(await c.approve(fresh), isTrue);
    expect(opens, 1);
  });
  for (final changed in ['sourceUrl', 'reviewedAt', 'expiresAt']) {
    test('changed $changed invalidates approval', () async {
      var data = bookingData(), opens = 0;
      final c = VerifiedBookingController(
        placeID: 'place',
        authorizationHeader: () => null,
        now: () => bookingNow,
        apiBaseUrl: 'https://api.test',
        client: MockClient((_) async => bookingReply(data)),
        openExternal: (_) async {
          opens++;
          return true;
        },
      );
      addTearDown(c.dispose);
      c.adopt(data);
      final p = (await c.prepare())!;
      data = {
        ...data,
        changed: changed == 'sourceUrl'
            ? 'https://example.org/source2'
            : changed == 'reviewedAt'
            ? '2026-10-03T10:00:00Z'
            : '2026-10-05T12:00:00Z',
      };
      expect(await c.approve(p), isFalse);
      expect(opens, 0);
    });
  }
  for (final stage in ['prepare', 'approve']) {
    test('404 during $stage clears old metadata, no launch', () async {
      var unavailable = stage == 'prepare', opens = 0;
      final c = VerifiedBookingController(
        placeID: 'place',
        authorizationHeader: () => null,
        now: () => bookingNow,
        apiBaseUrl: 'https://api.test',
        client: MockClient(
          (_) async => unavailable
              ? http.Response('{}', 404)
              : bookingReply(bookingData()),
        ),
        openExternal: (_) async {
          opens++;
          return true;
        },
      );
      addTearDown(c.dispose);
      c.adopt(bookingData());
      final p = await c.prepare();
      if (p != null) {
        unavailable = true;
        expect(await c.approve(p), isFalse);
      }
      expect(c.metadata, isNull);
      expect(c.error, contains('不可查看'));
      expect(opens, 0);
    });
  }
  test(
    'natural expiry invalidates prepared approval without any launcher',
    () async {
      var now = bookingNow, opens = 0;
      final c = VerifiedBookingController(
        placeID: 'place',
        authorizationHeader: () => null,
        now: () => now,
        apiBaseUrl: 'https://api.test',
        client: MockClient((_) async => bookingReply(bookingData())),
        openExternal: (_) async {
          opens++;
          return true;
        },
      );
      addTearDown(c.dispose);
      c.adopt(bookingData());
      final p = (await c.prepare())!;
      now = DateTime.utc(2026, 10, 4, 12);
      expect(c.canPrepare, isFalse);
      expect(c.isCurrent(p), isFalse);
      expect(await c.approve(p), isFalse);
      expect(opens, 0);
    },
  );
  for (final change in ['token', 'workspace', 'epoch']) {
    for (final stage in ['prepare', 'approve']) {
      test(
        '$change changes during delayed $stage suppress old response',
        () async {
          String? token = 'Bearer A', workspace;
          var epoch = 0, opens = 0;
          Completer<http.Response>? pending;
          final c = VerifiedBookingController(
            placeID: 'place',
            authorizationHeader: () => token,
            workspaceID: () => workspace,
            authorityEpoch: () => epoch,
            now: () => bookingNow,
            apiBaseUrl: 'https://api.test',
            client: MockClient((r) async {
              expect(r.headers['Authorization'], 'Bearer A');
              return pending?.future ?? bookingReply(bookingData());
            }),
            openExternal: (_) async {
              opens++;
              return true;
            },
          );
          addTearDown(c.dispose);
          c.adopt(bookingData());
          BookingPreview? p;
          if (stage == 'approve') p = (await c.prepare())!;
          pending = Completer<http.Response>();
          final operation = stage == 'prepare' ? c.prepare() : c.approve(p!);
          if (change == 'token') token = 'Bearer B';
          if (change == 'workspace') workspace = 'organization';
          if (change == 'epoch') epoch++;
          c.syncIdentity();
          pending.complete(bookingReply(bookingData()));
          await operation;
          expect(opens, 0);
          expect(c.metadata, isNull);
          expect(c.busy, isFalse);
        },
      );
    }
  }
  test(
    'cancel never launches; returned stale preview cannot be approved',
    () async {
      var opens = 0;
      final c = VerifiedBookingController(
        placeID: 'place',
        authorizationHeader: () => null,
        now: () => bookingNow,
        apiBaseUrl: 'https://api.test',
        client: MockClient((_) async => bookingReply(bookingData())),
        openExternal: (_) async {
          opens++;
          return true;
        },
      );
      addTearDown(c.dispose);
      c.adopt(bookingData());
      final p = (await c.prepare())!;
      c.cancel(p);
      expect(await c.approve(p), isFalse);
      expect(opens, 0);
    },
  );
  for (final throws in [false, true]) {
    test(
      'launcher ${throws ? 'exception' : 'false'} is error, not booked',
      () async {
        final c = VerifiedBookingController(
          placeID: 'place',
          authorizationHeader: () => null,
          now: () => bookingNow,
          apiBaseUrl: 'https://api.test',
          client: MockClient((_) async => bookingReply(bookingData())),
          openExternal: (_) async {
            if (throws) throw StateError('platform unavailable');
            return false;
          },
        );
        addTearDown(c.dispose);
        c.adopt(bookingData());
        expect(await c.approve((await c.prepare())!), isFalse);
        expect(c.error, isNotNull);
      },
    );
  }
  for (final support in ['contact', 'unknown', 'none']) {
    test('$support has no invented contact or availability', () async {
      var opens = 0;
      final c = VerifiedBookingController(
        placeID: 'place',
        authorizationHeader: () => null,
        now: () => bookingNow,
        apiBaseUrl: 'https://api.test',
        client: MockClient(
          (_) async => bookingReply(bookingData(support: support)),
        ),
        openExternal: (_) async {
          opens++;
          return true;
        },
      );
      addTearDown(c.dispose);
      c.adopt(bookingData(support: support));
      expect(c.canPrepare, isFalse);
      expect(await c.prepare(), isNull);
      expect(opens, 0);
    });
  }
  for (final invalid in [
    {'reservationUrl': 'http://example.org/book'},
    {'reservationUrl': 'https://secret@example.org/book'},
    {'reservationUrl': 'https://example.org/book#secret'},
    {'sourceUrl': 'javascript:alert(1)'},
    {'sourceUrl': null},
    {'placeId': 'other'},
    {'reservationSupport': 'yes'},
    {'reviewedAt': '2026-02-30T12:00:00Z'},
    {'reviewedAt': '2026-10-03T13:00:00Z'},
    {'expiresAt': '2026-10-03T12:00:00Z'},
    {'expiresAt': '0001-01-01T00:00:00+23:00'},
  ]) {
    test('reject invalid metadata $invalid', () {
      expect(
        () => VerifiedBookingMetadata.fromJson(
          {...bookingData(), ...invalid},
          'place',
          bookingNow,
        ),
        throwsFormatException,
      );
    });
  }
  test('dispose during transport suppresses response and launcher', () async {
    final pending = Completer<http.Response>();
    var opens = 0;
    final c = VerifiedBookingController(
      placeID: 'place',
      authorizationHeader: () => null,
      now: () => bookingNow,
      apiBaseUrl: 'https://api.test',
      client: MockClient((_) => pending.future),
      openExternal: (_) async {
        opens++;
        return true;
      },
    );
    c.adopt(bookingData());
    final operation = c.prepare();
    c.dispose();
    pending.complete(bookingReply(bookingData()));
    expect(await operation, isNull);
    expect(opens, 0);
  });
}
