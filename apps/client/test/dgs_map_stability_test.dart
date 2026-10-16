import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/owned_map_controller.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:birdtie_client/src/workspace/map_layers_controller.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'content_map_controller_test.dart' as f;
import 'map_layers_api_test.dart' as typed;

class ResourceFailureFixture extends f.Fixture {
  ResourceFailureFixture(this.failureStatus, this.failureCode) {
    count = 2;
    covers = true;
  }
  final int failureStatus;
  final String failureCode;
  int failedMediaReads = 0;
  bool omitFailedResource = false;
  Completer<void>? mediaFailureGate;

  @override
  Map<String, Object?> page(Uri uri) {
    final value = super.page(uri);
    final pins = (value['pins'] as List).cast<Map<String, Object?>>();
    pins[1]['cover'] = null;
    if (omitFailedResource && failedMediaReads > 0) {
      pins.removeAt(0);
      value['totalContentCount'] = 1;
      value['totalAnchorCount'] = 1;
    }
    return value;
  }

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    if (request.url.path.endsWith('/content')) {
      failedMediaReads++;
      await mediaFailureGate?.future;
      final bytes = utf8.encode(
        jsonEncode({
          'error': {'code': failureCode},
        }),
      );
      return Future.value(
        http.StreamedResponse(
          Stream.value(bytes),
          failureStatus,
          headers: {
            'content-type': 'application/json',
            'cache-control': 'no-store',
          },
          contentLength: bytes.length,
        ),
      );
    }
    return super.send(request);
  }
}

void additionalTests() {
  for (final failure in [
    (401, 'unauthorized'),
    (403, 'person_account_required'),
    (403, 'forbidden'),
    (409, 'human_control_changed'),
    (410, 'not_found'),
    (404, 'unrecognized'),
  ]) {
    testWidgets(
      'unproven or global ${failure.$1}/${failure.$2} retires the whole grant',
      (t) async {
        final fixture = ResourceFailureFixture(failure.$1, failure.$2);
        final identity = ValueNotifier(0);
        final c = OwnedMapController(
          client: fixture,
          apiBaseUrl: () => f.base,
          ownerID: () => fixture.owner,
          authorizationHeader: () => 'Bearer fixture',
          organizationWorkspaceID: () => null,
          identityChanges: identity,
          refreshAllowed: () => true,
          now: () => fixture.now,
        );
        try {
          c.updateQuery('', f.bounds);
          await f.drain(t, () => fixture.failedMediaReads > 0);
          await t.runAsync(() => Future<void>.delayed(Duration.zero));
          await t.pump();
          expect(c.current, isFalse);
          expect(c.cachedViews, 0);
          expect(c.cachedMediaItems, 0);
          final requests = c.requests;
          await t.pump(const Duration(seconds: 3));
          expect(
            c.requests,
            requests,
            reason: 'No automatic replay across a global denial.',
          );
        } finally {
          c.dispose();
          identity.dispose();
          await t.pump();
        }
      },
    );
  }

  testWidgets(
    'resource recovery rechecks ACL once and never revives the old pin grant',
    (t) async {
      final fixture = ResourceFailureFixture(404, 'not_found');
      final gate = fixture.mediaFailureGate = Completer<void>();
      final identity = ValueNotifier(0);
      final c = OwnedMapController(
        client: fixture,
        apiBaseUrl: () => f.base,
        ownerID: () => fixture.owner,
        authorizationHeader: () => 'Bearer fixture',
        organizationWorkspaceID: () => null,
        identityChanges: identity,
        refreshAllowed: () => true,
        now: () => fixture.now,
      );
      try {
        c.updateQuery('', f.bounds);
        await f.drain(t, () => fixture.failedMediaReads > 0);
        final oldPin = c.items.firstWhere((p) => p.momentID == f.id(1));
        gate.complete();
        await t.runAsync(() => Future<void>.delayed(Duration.zero));
        await t.pump();
        expect(c.items.map((p) => p.momentID), [f.id(2)]);
        expect(c.sourceCurrent(oldPin), isFalse);
        await t.pump(const Duration(seconds: 2));
        await f.drain(t, () => fixture.failedMediaReads == 2 && !c.loading);
        await t.runAsync(() => Future<void>.delayed(Duration.zero));
        await t.pump();
        expect(c.items.map((p) => p.momentID), [f.id(2)]);
        await t.pump(const Duration(seconds: 4));
        expect(fixture.failedMediaReads, 2);
        expect(c.requests, 2);
        expect(c.current, isTrue);
        expect(c.sourceCurrent(oldPin), isFalse);
      } finally {
        c.dispose();
        identity.dispose();
        await t.pump();
      }
    },
  );

  testWidgets(
    'owned transient failure retains the lease, retries once and retires on expiry',
    (t) async {
      final fixture = f.Fixture();
      final identity = ValueNotifier(0);
      final c = OwnedMapController(
        client: fixture,
        apiBaseUrl: () => f.base,
        ownerID: () => fixture.owner,
        authorizationHeader: () => 'Bearer fixture',
        organizationWorkspaceID: () => null,
        identityChanges: identity,
        refreshAllowed: () => true,
        now: () => fixture.now,
      );
      try {
        c.updateQuery('', f.bounds);
        await f.drain(t, () => c.current && !c.loading);
        final pin = c.items.single, originalUntil = c.view!.validUntil;
        fixture.status = 503;
        await t.runAsync(c.refresh);
        expect(c.sourceCurrent(pin), isTrue);
        expect(c.view!.validUntil, originalUntil);
        await t.pump(const Duration(seconds: 2));
        await f.drain(t, () => !c.loading);
        final attempts = c.requests;
        await t.pump(const Duration(seconds: 3));
        expect(c.requests, attempts);
        fixture.now = fixture.now.add(const Duration(seconds: 31));
        await t.pump(const Duration(seconds: 26));
        await f.drain(t, () => !c.loading);
        expect(c.sourceCurrent(pin), isFalse);
        expect(c.error, isNotNull);
        fixture.status = 200;
        await t.runAsync(c.refresh);
        expect(c.current, isTrue);
      } finally {
        c.dispose();
        identity.dispose();
        await t.pump();
      }
    },
  );

  testWidgets(
    'typed transient failure preserves valid public and author leases only',
    (t) async {
      var status = 200;
      final api = MapLayersApi(
        apiBaseUrl: 'https://typed-dgs.test',
        client: MockClient((request) async {
          final author = request.url.queryParameters['authorId'];
          final page = typed.mapTestView(milliseconds: 6000);
          if (author != null) {
            page['schemaVersion'] = 'typed-map-layers-v2';
            page['scope'] = 'PUBLIC_AUTHOR';
            page['authorId'] = author;
            page['items'] = [typed.mapTestItem('MOMENT')];
          }
          return http.Response(jsonEncode({'data': page}), status);
        }),
      );
      final c = MapLayersController(
        api: api,
        authorizationHeader: () => null,
        organizationWorkspaceID: () => null,
      );
      try {
        await c.load('owned-city', typed.mapTestBounds);
        await c.loadAuthor('owned-city', typed.mapTestBounds, typed.mapTestID);
        final public = c.publicView, author = c.authorView;
        status = 503;
        await c.load('owned-city', typed.mapTestBounds);
        await c.loadAuthor('owned-city', typed.mapTestBounds, typed.mapTestID);
        expect(c.publicView, same(public));
        expect(c.authorView, same(author));
        expect(c.error, isNotNull);
        expect(c.authorError, isNotNull);
        await t.pump(const Duration(seconds: 7));
        expect(c.publicView, isNull);
        expect(c.authorView, isNull);
        status = 200;
        await c.load('owned-city', typed.mapTestBounds);
        status = 403;
        await c.load('owned-city', typed.mapTestBounds);
        expect(c.publicView, isNull);
      } finally {
        c.dispose();
        await t.pump();
      }
    },
  );
}

class _BadEnvelopeFixture extends f.Fixture {
  bool bad = false;
  @override
  Map<String, Object?> page(Uri uri) {
    final page = super.page(uri);
    if (bad) page['viewerAccountId'] = f.id(9001);
    return page;
  }
}

void main() {
  additionalTests();
  testWidgets(
    'invalid authority metadata cannot retain an earlier owned grant',
    (t) async {
      final fixture = _BadEnvelopeFixture();
      final identity = ValueNotifier(0);
      final c = OwnedMapController(
        client: fixture,
        apiBaseUrl: () => f.base,
        ownerID: () => fixture.owner,
        authorizationHeader: () => 'Bearer fixture',
        organizationWorkspaceID: () => null,
        identityChanges: identity,
        refreshAllowed: () => true,
        now: () => fixture.now,
      );
      try {
        c.updateQuery('', f.bounds);
        await f.drain(t, () => c.current && !c.loading);
        final pin = c.items.single;
        fixture.bad = true;
        await t.runAsync(c.refresh);
        expect(c.sourceCurrent(pin), isFalse);
        expect(c.cachedViews, 0);
        expect(c.error, isNotNull);
      } finally {
        c.dispose();
        identity.dispose();
        await t.pump();
      }
    },
  );
  for (final failure in [(404, 'not_found'), (409, 'moment_changed')]) {
    testWidgets('one cover ${failure.$1} cannot erase unrelated map content', (
      t,
    ) async {
      final fixture = ResourceFailureFixture(failure.$1, failure.$2)
        ..omitFailedResource = true;
      final identity = ValueNotifier(0);
      final controller = OwnedMapController(
        client: fixture,
        apiBaseUrl: () => f.base,
        ownerID: () => fixture.owner,
        authorizationHeader: () => 'Bearer fixture',
        organizationWorkspaceID: () => null,
        identityChanges: identity,
        refreshAllowed: () => true,
        now: () => fixture.now,
      );
      try {
        controller.updateQuery('', f.bounds);
        await f.drain(
          t,
          () => fixture.failedMediaReads > 0 && !controller.loading,
        );
        // The response stream cancellation and denial callback are asynchronous;
        // observe their completion, not merely the mock's send invocation.
        await t.runAsync(() => Future<void>.delayed(Duration.zero));
        await t.pump();
        expect(
          controller.items.map((p) => p.momentID),
          contains(f.id(2)),
          reason:
              'A resource response is not evidence that the other grant was revoked.',
        );
        expect(controller.items.any((p) => p.momentID == f.id(1)), isFalse);
      } finally {
        controller.dispose();
        identity.dispose();
        await t.pump();
      }
    });
  }
}
