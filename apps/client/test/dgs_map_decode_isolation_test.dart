import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:birdtie_client/src/workspace/owned_map_api.dart';
import 'package:flutter_test/flutter_test.dart';

import 'content_map_controller_test.dart' as owned;
import 'map_layers_api_test.dart' as typed;

void main() {
  final fixture = owned.Fixture()..count = 2;
  Map<String, Object?> ownedPage() => fixture.page(
    Uri.parse(owned.base).replace(
      queryParameters: {
        ...owned.bounds.toJson().map((key, value) => MapEntry(key, '$value')),
        'zoom': '12',
      },
    ),
  );
  OwnedMapView decodeOwned(Object? raw) => OwnedMapView.decode(
    raw,
    ownerID: fixture.owner,
    cityID: '',
    bounds: owned.bounds,
    now: fixture.now,
  );
  TypedMapView decodeTyped(Object? raw) =>
      TypedMapView.decode(raw, 'owned-city', typed.mapTestBounds, false);

  test(
    'v2 isolates one malformed pin, keeps untitled no-POI coordinate content',
    () {
      final page = ownedPage();
      final pins = page['pins'] as List;
      (pins[0] as Map)['name'] = null;
      (pins[1] as Map)['name'] = '';
      final result = decodeOwned(page);
      expect(result.items, hasLength(1));
      expect(result.rejectedItemCount, 1);
      expect(result.items.single.name, isEmpty);
      expect(result.items.single.placeID, isEmpty);
      expect(result.items.single.momentID, owned.id(2));
      expect(result.items.single.entryID, owned.id(2));
      (pins[1] as Map)['visibilitySources'] = ['UNKNOWN_PERMISSION'];
      expect(
        () => decodeOwned(page),
        throwsFormatException,
        reason: 'All rejected rows are a read failure, never an empty city.',
      );
    },
  );

  test('typed row rejection preserves strict successful row semantics', () {
    final page = typed.mapTestView();
    page['items'] = [
      typed.mapTestItem('PLACE')..['title'] = null,
      typed.mapTestItem('MOMENT')..['title'] = '',
    ];
    final result = decodeTyped(page);
    expect(result.items.single.kind, 'MOMENT');
    expect(result.items.single.title, isEmpty);
    expect(result.rejectedItemCount, 1);
    (page['items'] as List).removeLast();
    expect(() => decodeTyped(page), throwsFormatException);
  });

  test(
    'global identity schema range lease and duplicates remain fail closed',
    () {
      for (final mutation in <void Function(Map<String, Object?>)>[
        (p) => p['schemaVersion'] = 'unknown',
        (p) => p['scope'] = 'PUBLIC',
        (p) => p['viewerAccountId'] = owned.id(9001),
        (p) => p['bounds'] = {...owned.bounds.toJson(), 'west': 1},
        (p) => p['validUntil'] = fixture.now.toIso8601String(),
        (p) => (p['pins'] as List)[1] = (p['pins'] as List)[0],
        (p) => p['unapprovedPermission'] = true,
      ]) {
        final page = ownedPage();
        mutation(page);
        expect(() => decodeOwned(page), throwsFormatException);
      }
      for (final mutation in <void Function(Map<String, dynamic>)>[
        (p) => p['schemaVersion'] = 'unknown',
        (p) => p['scope'] = 'SELF_PRIVATE',
        (p) => p['cityId'] = 'other',
        (p) => p['validUntil'] = p['observedAt'],
        (p) => (p['items'] as List)[1] = (p['items'] as List)[0],
        (p) => p['unapprovedPermission'] = true,
      ]) {
        final page = typed.mapTestView();
        mutation(page);
        expect(() => decodeTyped(page), throwsFormatException);
      }
    },
  );
}
