import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:birdtie_client/src/workspace/map_layers_controller.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'map_layers_api_test.dart' as f;

const _author = '92345678-1234-4234-8234-123456789abc';

http.Response _frame({
  bool author = false,
  bool expired = false,
  int leaseMilliseconds = 12000,
}) {
  final now = DateTime.now().toUtc();
  final wire = f.mapTestView()
    ..['observedAt'] = now
        .subtract(const Duration(seconds: 10))
        .toIso8601String()
    ..['validUntil'] = now
        .add(Duration(milliseconds: expired ? -1 : leaseMilliseconds))
        .toIso8601String();
  if (author) {
    wire['schemaVersion'] = 'typed-map-layers-v2';
    wire['scope'] = 'PUBLIC_AUTHOR';
    wire['authorId'] = _author;
    wire['items'] = [f.mapTestItem('MOMENT')];
  }
  return http.Response(
    jsonEncode({'data': wire}),
    200,
    headers: {'content-type': 'application/json; charset=utf-8'},
  );
}

MapLayersController _controller(
  Future<http.Response> Function(http.Request) read, {
  bool Function()? refreshAllowed,
}) => MapLayersController(
  api: MapLayersApi(client: MockClient(read), apiBaseUrl: 'http://owned.test'),
  authorizationHeader: () => null,
  organizationWorkspaceID: () => null,
  refreshAllowed: refreshAllowed,
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets(
    'return after tiny hidden expiry wakes before minimum timer without extending old source',
    (t) async {
      var reads = 0, visible = true;
      final c = _controller((_) async {
        reads++;
        return _frame(leaseMilliseconds: 100);
      }, refreshAllowed: () => visible);
      c.updateRefreshVisibility();
      await c.load('owned-city', f.mapTestBounds);
      final old = c.publicView!, until = c.publicView!.validUntil;
      visible = false;
      c.updateRefreshVisibility();
      await t.pump(const Duration(milliseconds: 150));
      expect(reads, 1);
      expect(c.items, isEmpty);
      visible = true;
      c.updateRefreshVisibility();
      c.updateRefreshVisibility();
      await t.pump();
      expect(reads, 2);
      expect(c.items, isNotEmpty);
      expect(old.validUntil, until);
      c.dispose();
    },
  );

  testWidgets(
    'tiny visible leases retire and reread with a bounded two-second interval',
    (t) async {
      var reads = 0;
      final c = _controller((_) async {
        reads++;
        return _frame(leaseMilliseconds: 100);
      }, refreshAllowed: () => true);
      c.updateRefreshVisibility();
      await c.load('owned-city', f.mapTestBounds);
      final old = c.publicView!, until = c.publicView!.validUntil;
      await t.pump(const Duration(milliseconds: 150));
      expect(reads, 1);
      expect(c.items, isEmpty);
      await t.pump(const Duration(milliseconds: 1850));
      expect(reads, 2);
      for (var i = 0; i < 3; i++) {
        await t.pump(const Duration(seconds: 2));
      }
      expect(reads, 5, reason: 'Short grants cannot create a busy HTTP loop.');
      expect(old.validUntil, until);
      c.dispose();
    },
  );

  testWidgets(
    'visible short author grant renews exact author range without city fallback',
    (t) async {
      var baseReads = 0, authorReads = 0;
      final c = _controller((r) async {
        if (r.url.queryParameters.containsKey('authorId')) {
          authorReads++;
          return _frame(author: true, leaseMilliseconds: 3000);
        }
        baseReads++;
        return _frame();
      }, refreshAllowed: () => true);
      c.updateRefreshVisibility();
      await c.load('owned-city', f.mapTestBounds);
      await c.loadAuthor('owned-city', f.mapTestBounds, _author);
      final old = c.authorView!, until = c.authorView!.validUntil;
      await t.pump(const Duration(milliseconds: 2700));
      expect(baseReads, 1);
      expect(authorReads, 2);
      expect(c.authorView!.authorID, _author);
      expect(c.items.every((i) => i.kind == 'MOMENT'), true);
      expect(old.validUntil, until);
      c.dispose();
    },
  );

  for (final status in [401, 403, 409, 500]) {
    testWidgets(
      'automatic HTTP $status is ${status < 409 ? 'not retried' : 'retried once'} without stale points',
      (t) async {
        var reads = 0;
        final c = _controller(
          (_) async => ++reads == 1 ? _frame() : http.Response('{}', status),
          refreshAllowed: () => true,
        );
        c.updateRefreshVisibility();
        await c.load('owned-city', f.mapTestBounds);
        await t.pump(const Duration(seconds: 10));
        expect(reads, 2);
        expect(c.items, isEmpty);
        await t.pump(const Duration(seconds: 20));
        expect(reads, status < 409 ? 2 : 3);
        expect(c.items, isEmpty);
        c.dispose();
      },
    );
  }

  testWidgets('malformed automatic response cannot start a retry loop', (
    t,
  ) async {
    var reads = 0;
    final c = _controller(
      (_) async => ++reads == 1 ? _frame() : http.Response('{"data":{}}', 200),
      refreshAllowed: () => true,
    );
    c.updateRefreshVisibility();
    await c.load('owned-city', f.mapTestBounds);
    await t.pump(const Duration(seconds: 10));
    await t.pump(const Duration(seconds: 30));
    expect(reads, 2);
    expect(c.items, isEmpty);
    c.dispose();
  });

  testWidgets(
    'visible valid three-second map grant renews automatically before its original expiry',
    (t) async {
      var reads = 0;
      final c = _controller((_) async {
        reads++;
        return _frame(leaseMilliseconds: 3000);
      }, refreshAllowed: () => true);
      c.updateRefreshVisibility();
      await c.load('owned-city', f.mapTestBounds);
      final old = c.publicView!, until = c.publicView!.validUntil;
      await t.pump(const Duration(milliseconds: 2700));
      expect(reads, 2);
      expect(c.items, isNotEmpty);
      expect(old.validUntil, until);
      c.dispose();
    },
  );

  testWidgets(
    'missed due wakes once after visible return, original deadline retires while hidden',
    (t) async {
      var reads = 0, visible = true;
      final c = _controller((_) async {
        reads++;
        return _frame();
      }, refreshAllowed: () => visible);
      c.updateRefreshVisibility();
      await c.load('owned-city', f.mapTestBounds);
      final old = c.publicView!;
      final until = old.validUntil;
      visible = false;
      c.updateRefreshVisibility();
      await t.pump(const Duration(seconds: 13));
      expect(reads, 1);
      expect(c.items, isEmpty);
      visible = true;
      c.updateRefreshVisibility();
      c.updateRefreshVisibility();
      await t.pump();
      expect(reads, 2);
      expect(c.items, isNotEmpty);
      expect(old.validUntil, until);
      c.dispose();
    },
  );

  testWidgets(
    'late pre-pause automatic response cannot restore after visibility ABA',
    (t) async {
      var reads = 0, visible = true;
      final held = Completer<http.Response>();
      final c = _controller(
        (_) async => ++reads == 2 ? held.future : _frame(),
        refreshAllowed: () => visible,
      );
      c.updateRefreshVisibility();
      await c.load('owned-city', f.mapTestBounds);
      await t.pump(const Duration(seconds: 10));
      expect(reads, 2);
      visible = false;
      c.updateRefreshVisibility();
      visible = true;
      c.updateRefreshVisibility();
      c.updateRefreshVisibility();
      await t.pump();
      expect(reads, 3);
      final current = c.publicView;
      held.complete(_frame());
      await t.pump();
      expect(identical(c.publicView, current), true);
      c.dispose();
    },
  );

  testWidgets('failed automatic read has one foreground retry then stops', (
    t,
  ) async {
    var reads = 0, visible = true;
    final c = _controller(
      (_) async => ++reads == 1 ? _frame() : http.Response('{}', 503),
      refreshAllowed: () => visible,
    );
    c.updateRefreshVisibility();
    await c.load('owned-city', f.mapTestBounds);
    await t.pump(const Duration(seconds: 10));
    expect(reads, 2);
    await t.pump(const Duration(seconds: 2));
    expect(reads, 3);
    await t.pump(const Duration(seconds: 30));
    expect(reads, 3);
    expect(c.items, isEmpty);
    expect(c.error, isNotNull);
    c.dispose();
  });

  testWidgets(
    'background retry timer cannot emit HTTP; foreground recovery gets fresh source',
    (t) async {
      var reads = 0, visible = true;
      final c = _controller(
        (_) async => ++reads == 2 ? http.Response('{}', 503) : _frame(),
        refreshAllowed: () => visible,
      );
      c.updateRefreshVisibility();
      await c.load('owned-city', f.mapTestBounds);
      await t.pump(const Duration(seconds: 10));
      expect(reads, 2);
      visible = false;
      c.updateRefreshVisibility();
      await t.pump(const Duration(seconds: 20));
      expect(reads, 2);
      expect(c.items, isEmpty);
      visible = true;
      c.updateRefreshVisibility();
      await t.pump();
      expect(reads, 3);
      expect(c.items, isNotEmpty);
      c.dispose();
    },
  );

  test('range ABA does not coalesce a late old range response', () async {
    var reads = 0;
    final held = Completer<http.Response>();
    final c = _controller((_) async => ++reads == 1 ? held.future : _frame());
    addTearDown(c.dispose);
    const other = MapBounds(
      west: -2.18,
      south: 57.14,
      east: -2.03,
      north: 57.24,
    );
    final old = c.load('owned-city', f.mapTestBounds);
    await c.load('owned-city', other);
    await c.load('owned-city', f.mapTestBounds);
    expect(reads, 3);
    final current = c.publicView;
    held.complete(_frame());
    await old;
    expect(identical(c.publicView, current), true);
  });

  test('same exact pending map binding performs one canonical GET', () async {
    var reads = 0;
    final response = Completer<http.Response>();
    final c = _controller((_) {
      reads++;
      return response.future;
    });
    addTearDown(c.dispose);
    final first = c.load('owned-city', f.mapTestBounds);
    final second = c.load('owned-city', f.mapTestBounds);
    await Future<void>.delayed(Duration.zero);
    expect(reads, 1);
    response.complete(_frame());
    await Future.wait([first, second]);
    expect(c.items, hasLength(5));
  });

  test('absolute expired public source never installs point or card', () async {
    final c = _controller((_) async => _frame(expired: true));
    addTearDown(c.dispose);
    await c.load('owned-city', f.mapTestBounds);
    expect(c.publicView, isNull);
    expect(c.items, isEmpty);
    expect(c.error, contains('到期'));
  });

  test(
    'absolute expired author source never substitutes underlying city',
    () async {
      final c = _controller(
        (r) async => _frame(
          author: r.url.queryParameters.containsKey('authorId'),
          expired: r.url.queryParameters.containsKey('authorId'),
        ),
      );
      addTearDown(c.dispose);
      await c.load('owned-city', f.mapTestBounds);
      expect(c.baseItems, isNotEmpty);
      expect(await c.loadAuthor('owned-city', f.mapTestBounds, _author), false);
      expect(c.authorID, _author);
      expect(c.authorView, isNull);
      expect(c.items, isEmpty);
    },
  );
}
