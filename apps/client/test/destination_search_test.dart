import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/city/browse_destination.dart';
import 'package:birdtie_client/src/city/destination_search_controller.dart';
import 'package:birdtie_client/src/city/destination_search_service.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

// Isolated synthetic geocoder payloads, not saved provider responses.
Map<String, Object?> feature(String id, String country) => {
  'type': 'Feature',
  'id': id,
  'geometry': {
    'type': 'Point',
    'coordinates': [2.3, 48.8],
  },
  'properties': {
    'mapbox_id': id,
    'feature_type': 'place',
    'name': 'Synthetic destination $id',
    'bbox': [2.1, 48.6, 2.5, 49.0],
    'context': {
      'country': {'country_code': country, 'name': 'Synthetic $country'},
      'region': {'name': 'Synthetic region'},
    },
  },
};

const destination = BrowseDestination(
  provider: 'mapbox',
  externalID: 'synthetic-one',
  name: 'Synthetic One',
  countryCode: 'FR',
  region: 'Synthetic',
  latitude: 48.8,
  longitude: 2.3,
  featureType: 'place',
  attribution: 'UNIT attribution',
  storageMode: DestinationStorageMode.temporary,
);

class PendingSearch implements DestinationSearchService {
  final calls = <String>[];
  final pending = <Completer<DestinationSearchResult>>[];
  int cancellations = 0;
  @override
  Future<DestinationSearchResult> search(
    String query, {
    Future<void>? abortTrigger,
  }) {
    calls.add(query);
    final next = Completer<DestinationSearchResult>();
    pending.add(next);
    abortTrigger?.then((_) => cancellations++);
    return next.future;
  }

  @override
  void dispose() {}
}

class FixedSearch implements DestinationSearchService {
  FixedSearch({this.result, this.failure});
  final DestinationSearchResult? result;
  final DestinationSearchFailure? failure;
  int calls = 0;
  @override
  Future<DestinationSearchResult> search(
    String query, {
    Future<void>? abortTrigger,
  }) async {
    calls++;
    if (failure != null) throw failure!;
    return result!;
  }

  @override
  void dispose() {}
}

void main() {
  test(
    'catalog resolution reuses request fencing and never infers identity',
    () async {
      final service = PendingSearch();
      final controller = DestinationSearchController(service: service);
      addTearDown(controller.dispose);
      const city = PublicCity(
        id: 'unit-city',
        name: 'Synthetic City',
        region: 'Synthetic Region',
        countryCode: 'FR',
        contentStatus: 'unit',
        map: null,
        source: PublicSource(
          label: 'unit',
          maintainer: 'unit',
          freshness: 'unit',
          updatedAt: null,
        ),
      );
      final resolving = controller.resolveCity(city);
      expect(service.calls, ['Synthetic City Synthetic Region FR']);
      expect(controller.resolvingCity, same(city));
      expect(controller.loading, true);
      service.pending[0].complete(
        const DestinationSearchResult(
          destinations: [
            destination,
            BrowseDestination(
              provider: 'mapbox',
              externalID: 'wrong-country',
              name: 'Synthetic City',
              countryCode: 'GB',
              region: 'Synthetic',
              latitude: 55,
              longitude: -3,
              featureType: 'place',
              attribution: 'UNIT',
              storageMode: DestinationStorageMode.temporary,
            ),
            BrowseDestination(
              provider: 'mapbox',
              externalID: 'wrong-level',
              name: 'Synthetic City',
              countryCode: 'FR',
              region: 'Synthetic',
              latitude: 48,
              longitude: 2,
              featureType: 'region',
              attribution: 'UNIT',
              storageMode: DestinationStorageMode.temporary,
            ),
          ],
        ),
      );
      await resolving;
      expect(controller.items, [destination]);
      expect(controller.items.single.internalCityID, isNull);
      expect(
        controller.recentQueries,
        isEmpty,
        reason: 'generated query is not user text',
      );
      final retry = controller.retry();
      controller.cancelResolution();
      expect(controller.resolvingCity, isNull);
      expect(controller.query, isEmpty);
      service.pending[1].complete(
        const DestinationSearchResult(destinations: [destination]),
      );
      await retry;
      expect(controller.items, isEmpty);
      expect(controller.loading, false);
      expect(service.cancellations, 1);
    },
  );

  testWidgets(
    'edited search exits catalog resolution and retires old candidates',
    (t) async {
      final service = PendingSearch();
      final controller = DestinationSearchController(service: service);
      addTearDown(controller.dispose);
      const city = PublicCity(
        id: 'unit-city',
        name: 'Synthetic City',
        region: '',
        countryCode: 'FR',
        contentStatus: 'unit',
        map: null,
        source: PublicSource(
          label: 'unit',
          maintainer: 'unit',
          freshness: 'unit',
          updatedAt: null,
        ),
      );
      final resolving = controller.resolveCity(city);
      controller.updateInput(const TextEditingValue(text: 'New user query'));
      expect(controller.resolvingCity, isNull);
      await t.pump(const Duration(milliseconds: 300));
      expect(service.calls, ['Synthetic City FR', 'New user query']);
      service.pending[0].complete(
        const DestinationSearchResult(destinations: [destination]),
      );
      await resolving;
      expect(controller.items, isEmpty);
      service.pending[1].complete(
        const DestinationSearchResult(destinations: []),
      );
      await t.pump();
      expect(controller.items, isEmpty);
      expect(controller.recentQueries, ['New user query']);
      expect(service.cancellations, 1);
    },
  );

  test(
    'composition keeps available provider, respects consent and independent backoff',
    () async {
      final overseas = FixedSearch(
        failure: const DestinationSearchFailure(
          'UNIT failure',
          retryAfter: Duration(seconds: 30),
        ),
      );
      final domestic = FixedSearch(
        result: const DestinationSearchResult(
          destinations: [destination],
          attribution: 'UNIT',
        ),
      );
      final combined = CompositeDestinationSearchService(
        overseas: overseas,
        domestic: domestic,
        domesticEnabled: () => true,
      );
      for (var i = 0; i < 2; i++) {
        final result = await combined.search('Paris');
        expect(result.destinations, [destination]);
        expect(result.partialError, isNotNull);
      }
      expect(overseas.calls, 1);
      expect(domestic.calls, 2);
      combined.dispose();
      final global = FixedSearch(
        result: const DestinationSearchResult(destinations: [destination]),
      );
      final gated = CompositeDestinationSearchService(
        overseas: global,
        domestic: domestic,
        domesticEnabled: () => false,
      );
      final result = await gated.search('巴黎');
      expect(
        result.domesticUnavailable,
        false,
        reason: 'Chinese spelling of an overseas target is not CN evidence',
      );
      expect(result.destinations, [destination]);
      expect(domestic.calls, 2);
      gated.dispose();
      final needsDomestic = CompositeDestinationSearchService(
        overseas: FixedSearch(
          result: const DestinationSearchResult(
            destinations: [],
            domesticUnavailable: true,
          ),
        ),
        domestic: domestic,
        domesticEnabled: () => false,
      );
      expect(
        (await needsDomestic.search('Shanghai')).domesticUnavailable,
        true,
      );
      expect(domestic.calls, 2, reason: 'no implicit SDK consent');
      needsDomestic.dispose();
    },
  );
  test(
    'v6 is global Temporary, no cache, separates domestic renderer scope',
    () async {
      final calls = <http.Request>[];
      final service = MapboxDestinationSearchService(
        accessToken: 'pk.synthetic-unit-only',
        client: MockClient((r) async {
          calls.add(r);
          return http.Response(
            jsonEncode({
              'type': 'FeatureCollection',
              'features': [
                feature('one', 'fr'),
                feature('two', 'us'),
                feature('domestic', 'cn'),
              ],
              'attribution': 'UNIT attribution',
            }),
            200,
          );
        }),
      );
      addTearDown(service.dispose);
      for (var i = 0; i < 2; i++) {
        final result = await service.search('巴黎');
        expect(result.destinations.map((v) => v.countryCode), ['FR', 'US']);
        expect(result.domesticUnavailable, true);
        expect(
          result.destinations.every(
            (v) => v.internalCityID == null && v.isTemporary,
          ),
          true,
        );
        expect(result.destinations.first.bounds, isNotNull);
      }
      expect(calls, hasLength(2));
      final q = calls.first.url.queryParameters;
      expect(calls.first.url.path, '/search/geocode/v6/forward');
      expect(q['permanent'], 'false');
      expect(q['q'], '巴黎');
      expect(
        q.containsKey('bbox') ||
            q.containsKey('country') ||
            q.containsKey('proximity'),
        false,
      );
      expect(calls.first.headers.containsKey('authorization'), false);
    },
  );

  test(
    'malformed envelope and transport token text never become UI errors',
    () async {
      for (final failure in ['body', 'transport', 'rate']) {
        final service = MapboxDestinationSearchService(
          accessToken: 'pk.synthetic-unit-only',
          client: MockClient((_) async {
            if (failure == 'transport')
              throw StateError('access_token=SECRET_UNIT_CANARY');
            return http.Response(
              failure == 'body' ? '{}' : 'SECRET_UNIT_CANARY',
              failure == 'rate' ? 429 : 200,
            );
          }),
        );
        try {
          await service.search('query');
          fail('Expected sanitized failure');
        } on DestinationSearchFailure catch (error) {
          expect(error.message, isNot(contains('SECRET_UNIT_CANARY')));
          expect(
            error.retryAfter,
            failure == 'rate'
                ? const Duration(seconds: 30)
                : const Duration(seconds: 2),
          );
        } finally {
          service.dispose();
        }
      }
    },
  );

  testWidgets(
    'composing, debounce, abort, latest response and cancel own only active results',
    (t) async {
      final service = PendingSearch();
      final controller = DestinationSearchController(service: service);
      controller.updateInput(
        const TextEditingValue(
          text: 'bali',
          composing: TextRange(start: 0, end: 4),
        ),
      );
      await t.pump(const Duration(seconds: 1));
      expect(service.calls, isEmpty);
      controller.updateInput(const TextEditingValue(text: '巴黎'));
      await t.pump(const Duration(milliseconds: 299));
      expect(service.calls, isEmpty);
      await t.pump(const Duration(milliseconds: 1));
      expect(service.calls, ['巴黎']);
      controller.updateInput(const TextEditingValue(text: 'Paris'));
      final submitted = controller.submit();
      await t.pump();
      expect(service.cancellations, 1);
      service.pending[1].complete(
        const DestinationSearchResult(destinations: [destination]),
      );
      await submitted;
      service.pending[0].complete(
        const DestinationSearchResult(
          destinations: [],
          domesticUnavailable: true,
        ),
      );
      await t.pump();
      expect(controller.items, [destination]);
      expect(controller.domesticUnavailable, false);
      expect(controller.recentQueries, ['Paris']);
      controller.cancel();
      expect(controller.items, isEmpty);
      controller.dispose();
    },
  );

  testWidgets(
    'rate failure uses bounded retry and identity retirement fences late result',
    (t) async {
      var current = true;
      final service = PendingSearch();
      final controller = DestinationSearchController(
        service: service,
        current: () => current,
      );
      controller.updateInput(const TextEditingValue(text: 'Paris'));
      final submitted = controller.submit();
      service.pending.single.completeError(
        const DestinationSearchFailure(
          '稍后重试',
          retryAfter: Duration(seconds: 30),
        ),
      );
      await submitted;
      await controller.retry();
      expect(service.calls, hasLength(1));
      expect(controller.error, contains('稍后重试'));
      controller.dispose();
      final next = DestinationSearchController(
        service: service,
        current: () => current,
      );
      next.updateInput(const TextEditingValue(text: 'Paris'));
      final nextSubmitted = next.submit();
      current = false;
      service.pending.last.complete(
        const DestinationSearchResult(destinations: [destination]),
      );
      await nextSubmitted;
      expect(next.items, isEmpty);
      next.dispose();
    },
  );
}
