import 'dart:convert';

import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:birdtie_client/src/workspace/map_layers_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'map_layers_api_test.dart' as f;
import 'map_media_contract_test.dart' show mediaView;

Map<String, dynamic> _dto({
  bool private = false,
  String? author,
  DateTime? observed,
  DateTime? until,
}) {
  final at = observed ?? DateTime.now().toUtc();
  return {
    ...f.mapTestView(private: private),
    'observedAt': at.toIso8601String(),
    'validUntil': (until ?? at.add(const Duration(minutes: 2)))
        .toIso8601String(),
    if (author != null) ...{
      'schemaVersion': 'typed-map-layers-v2',
      'scope': 'PUBLIC_AUTHOR',
      'authorId': author,
      'items': [f.mapTestItem('MOMENT')],
    },
  };
}

http.Response _response(Map<String, dynamic> dto) => http.Response(
  jsonEncode({'data': dto}),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

MapLayersController _controller(http.Response Function(http.Request) answer) =>
    MapLayersController(
      api: MapLayersApi(
        client: MockClient((request) async {
          expect(request.method, 'GET');
          return answer(request);
        }),
        apiBaseUrl: 'http://owned.test',
      ),
      authorizationHeader: () => 'Bearer owner',
      organizationWorkspaceID: () => null,
    );

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'photo/count withdrawal cannot invalidate identical retained point snapshot',
    () async {
      var reads = 0;
      final c = _controller((_) {
        final dto = mediaView(DateTime.now().toUtc());
        if (++reads == 2) {
          (dto['items'][0] as Map)
            ..remove('placePhoto')
            ..remove('recentMoments');
        }
        return _response(dto);
      });
      addTearDown(c.dispose);
      await c.load('owned-city', f.mapTestBounds);
      final original = c.publicView!;
      await c.load('owned-city', f.mapTestBounds);
      expect(c.retainsSnapshot(original), isTrue);
      expect(c.items.single.placePhoto, isNull);
      expect(c.items.single.recentMoments, isNull);
      expect(c.items.single.mapID, original.items.single.mapID);
    },
  );

  test('identical refreshed content retains the original DTO lease', () async {
    var reads = 0;
    final c = _controller((_) {
      final dto = _dto();
      if (++reads == 2) {
        dto['items'] = (dto['items'] as List).reversed.toList();
      }
      return _response(dto);
    });
    addTearDown(c.dispose);
    await c.load('owned-city', f.mapTestBounds);
    final original = c.publicView!;
    final originalUntil = original.validUntil;
    await c.load('owned-city', f.mapTestBounds);

    expect(reads, 2);
    expect(identical(original, c.publicView), false);
    expect(original.observedAt, isNot(c.publicView!.observedAt));
    expect(c.retainsSnapshot(original), true);
    expect(original.validUntil, originalUntil);
  });

  final changes = <String, void Function(Map<String, dynamic>)>{
    'version': (dto) => (dto['items'] as List).first['sourceVersion'] = 'v2',
    'title': (dto) => (dto['items'] as List).first['title'] = '更新后的标题',
    'place': (dto) =>
        (dto['items'] as List).first['anchor']['placeId'] = f.mapTestID,
    'anchor label': (dto) =>
        (dto['items'] as List).first['anchor']['label'] = '更新后的公开锚点',
    'latitude': (dto) =>
        (dto['items'] as List).first['anchor']['latitude'] = 57.21,
    'longitude': (dto) =>
        (dto['items'] as List).first['anchor']['longitude'] = -2.11,
    'entity and detail identity': (dto) {
      final item = (dto['items'] as List).first as Map;
      item['id'] = f.mapTestPlace;
      item['entityRef'] = {'type': 'PLACE', 'id': f.mapTestPlace};
      item['detailRef'] = {'type': 'PLACE', 'id': f.mapTestPlace};
    },
    'kind and corresponding detail': (dto) {
      final item = f.mapTestItem('BUSINESS');
      item['id'] = f.mapTestPlace;
      item['entityRef'] = {'type': 'BUSINESS', 'id': f.mapTestPlace};
      item['detailRef'] = {'type': 'BUSINESS', 'id': f.mapTestPlace};
      (dto['items'] as List)[0] = item;
    },
    'removed item': (dto) => (dto['items'] as List).removeAt(0),
    'added item': (dto) {
      final item = f.mapTestItem('PLACE');
      item['id'] = f.mapTestPlace;
      item['entityRef'] = {'type': 'PLACE', 'id': f.mapTestPlace};
      item['detailRef'] = {'type': 'PLACE', 'id': f.mapTestPlace};
      (dto['items'] as List).add(item);
    },
    'coverage truncation': (dto) => dto['truncated'] = true,
  };
  for (final change in changes.entries) {
    test(
      'refresh with changed ${change.key} retires the old snapshot',
      () async {
        var reads = 0;
        final c = _controller((_) {
          final dto = _dto();
          if (++reads == 2) change.value(dto);
          return _response(dto);
        });
        addTearDown(c.dispose);
        await c.load('owned-city', f.mapTestBounds);
        final original = c.publicView!;
        await c.load('owned-city', f.mapTestBounds);

        expect(reads, 2);
        expect(
          c.error,
          null,
          reason: 'changed fixture remains a valid typed DTO',
        );
        expect(c.publicView, isNotNull);
        expect(c.retainsSnapshot(original), false);
      },
    );
  }

  test('private HTTP 403 cannot retain an earlier private snapshot', () async {
    var privateFails = false;
    final c = _controller((request) {
      final private = request.url.path.contains('map-opportunities');
      return private && privateFails
          ? http.Response('{}', 403)
          : _response(_dto(private: private));
    });
    addTearDown(c.dispose);
    await c.load('owned-city', f.mapTestBounds);
    c.visible.add('OPPORTUNITY');
    await c.load('owned-city', f.mapTestBounds);
    final public = c.publicView!;
    final private = c.privateView!;
    privateFails = true;
    await c.load('owned-city', f.mapTestBounds);

    expect(c.partial, true);
    expect(c.privateView, null);
    expect(c.retainsSnapshot(private), false);
    expect(c.retainsSnapshot(public), true);
  });

  test('scope retirement and city changes reject old snapshots', () async {
    final c = _controller((request) {
      final dto = _dto();
      if (request.url.path.contains('new-city')) dto['cityId'] = 'new-city';
      return _response(dto);
    });
    addTearDown(c.dispose);
    await c.load('owned-city', f.mapTestBounds);
    final original = c.publicView!;
    c.retire();
    expect(c.retainsSnapshot(original), false);
    await c.load('new-city', f.mapTestBounds);
    expect(c.retainsSnapshot(original), false);
  });

  test(
    'PUBLIC never substitutes an author scope or a different author',
    () async {
      final c = _controller((request) {
        final author = request.url.queryParameters['authorId'];
        final dto = _dto(author: author);
        dto['items'] = [f.mapTestItem('MOMENT')];
        return _response(dto);
      });
      addTearDown(c.dispose);
      await c.load('owned-city', f.mapTestBounds);
      expect(
        await c.loadAuthor('owned-city', f.mapTestBounds, f.mapTestID),
        true,
      );
      final original = c.authorView!;
      expect(
        await c.loadAuthor('owned-city', f.mapTestBounds, f.mapTestID),
        true,
      );
      expect(identical(original, c.authorView), false);
      expect(c.retainsSnapshot(original), true);

      expect(
        await c.loadAuthor('owned-city', f.mapTestBounds, f.mapTestPlace),
        true,
      );
      expect(c.retainsSnapshot(original), false);
      c.leaveAuthor();
      expect(c.publicView!.items.single.mapID, original.items.single.mapID);
      expect(c.retainsSnapshot(original), false);
    },
  );

  test('refresh does not extend the original absolute lease', () async {
    var reads = 0;
    final c = _controller((_) {
      final now = DateTime.now().toUtc();
      return _response(
        ++reads == 1
            ? _dto(
                observed: now,
                until: now.add(const Duration(milliseconds: 250)),
              )
            : _dto(observed: now),
      );
    });
    addTearDown(c.dispose);
    await c.load('owned-city', f.mapTestBounds);
    final original = c.publicView!;
    await c.load('owned-city', f.mapTestBounds);
    expect(c.retainsSnapshot(original), true);

    final remaining = original.validUntil.difference(DateTime.now().toUtc());
    await Future<void>.delayed(
      (remaining > Duration.zero ? remaining : Duration.zero) +
          const Duration(milliseconds: 10),
    );
    expect(c.publicView, isNotNull);
    expect(c.retainsSnapshot(c.publicView!), true);
    expect(c.retainsSnapshot(original), false);
  });

  test(
    'an already expired latest lease cannot retain a live old DTO',
    () async {
      var reads = 0;
      final c = _controller((_) {
        final now = DateTime.now().toUtc();
        return _response(
          ++reads == 1
              ? _dto(observed: now)
              : _dto(
                  observed: now.subtract(const Duration(seconds: 10)),
                  until: now.subtract(const Duration(milliseconds: 1)),
                ),
        );
      });
      addTearDown(c.dispose);
      await c.load('owned-city', f.mapTestBounds);
      final original = c.publicView!;
      await c.load('owned-city', f.mapTestBounds);
      expect(c.publicView, isNull);
      expect(c.items, isEmpty);
      expect(original.validUntil.isAfter(DateTime.now()), true);
      expect(c.retainsSnapshot(original), false);
    },
  );
}
