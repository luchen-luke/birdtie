import 'dart:convert';
import 'dart:io';
import 'dart:math' as math;

import 'package:birdtie_client/src/city/mapbox_web_basemap.dart';
import 'package:flutter/material.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:latlong2/latlong.dart';

void main() {
  test(
    'GL 512 world and FlutterMap 256 world keep two point projections equal',
    () {
      for (final zoom in [3.0, 12.0, 14.35, 18.0]) {
        const center = LatLng(57.14786, -2.09524);
        final camera = MapboxWebCamera(
          latitude: center.latitude,
          longitude: center.longitude,
          zoom: zoom,
          rotation: 37,
        );
        expect(camera.valid, isTrue);
        final flutterCenter = const Epsg3857().latLngToPoint(center, zoom);
        final glWorld = 512 * math.pow(2, camera.glZoom);
        math.Point<double> glProject(LatLng point) {
          final sinLat = math.sin(point.latitude * math.pi / 180);
          return math.Point(
            (point.longitude + 180) / 360 * glWorld,
            (0.5 - math.log((1 + sinLat) / (1 - sinLat)) / (4 * math.pi)) *
                glWorld,
          );
        }

        final glCenter = glProject(center);
        for (final point in [
          const LatLng(57.14821, -2.0926),
          const LatLng(57.1442, -2.09971),
        ]) {
          final actual = glProject(point) - glCenter;
          final expected =
              const Epsg3857().latLngToPoint(point, zoom) - flutterCenter;
          expect((actual - expected).magnitude, lessThan(0.0001));
        }
        expect(camera.glBearing, -37);
        expect(camera.bridgeValues.keys.toSet(), {
          'center',
          'zoom',
          'bearing',
          'pitch',
        });
        expect(camera.bridgeValues['pitch'], 0);
      }
    },
  );

  test('invalid camera values cannot cross the browser basemap bridge', () {
    for (final camera in [
      const MapboxWebCamera(latitude: 90, longitude: 0, zoom: 12),
      const MapboxWebCamera(latitude: 0, longitude: double.nan, zoom: 12),
      const MapboxWebCamera(latitude: 0, longitude: 0, zoom: double.infinity),
      const MapboxWebCamera(latitude: 0, longitude: 0, zoom: 0),
      const MapboxWebCamera(
        latitude: 0,
        longitude: 0,
        zoom: 12,
        rotation: double.nan,
      ),
    ]) {
      expect(camera.valid, isFalse);
    }
  });

  testWidgets('native stub keeps browser transport and semantics absent', (
    tester,
  ) async {
    var callbacks = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: MapboxWebBasemap(
          accessToken: 'pk.synthetic-public-test',
          camera: const MapboxWebCamera(
            latitude: 57.14,
            longitude: -2.1,
            zoom: 12,
          ),
          onReady: () => callbacks++,
          onUnavailable: () => callbacks++,
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.byType(HtmlElementView), findsNothing);
    expect(callbacks, 0);
    expect(tester.takeException(), isNull);
  });

  test(
    'actual JS bridge retires pending mounts, applies newest camera and retries loader',
    () async {
      final executable = Platform.environment['BIRDTIE_TEST_NODE'] ?? 'node';
      final bridge = File('web/birdtie_mapbox.js').absolute.path;
      final process = await Process.start(executable, [
        '-e',
        _bridgeHarness,
        bridge,
      ]);
      final output = process.stdout.transform(utf8.decoder).join();
      final errors = process.stderr.transform(utf8.decoder).join();
      final exit = await process.exitCode;
      final stdout = await output;
      final stderr = await errors;
      expect(exit, 0, reason: stderr);
      expect(stdout.trim(), 'BRIDGE_BOUNDARIES_PASS');
    },
  );
}

// Real bridge source runs in an isolated JS VM. Its fake GL/DOM transport
// makes no network/provider request; the browser separately verifies GL pixels.
const _bridgeHarness = r'''
const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const source = fs.readFileSync(process.argv[1], 'utf8');
function fixture() {
  const created = [], elements = new Map();
  let assetFailure = false;
  class FakeMap {
    constructor(options) { this.options = options; this.listeners = {}; this.camera = options; this.removed = 0; this.resized = 0; created.push(this); }
    on(kind, fn) { this.listeners[kind] = fn; }
    jumpTo(camera) { this.camera = camera; }
    resize() { this.resized++; }
    remove() { this.removed++; }
    project(point) { return {x: point[0], y: point[1]}; }
  }
  class Observer {
    constructor(callback) { this.callback = callback; this.disconnected = false; }
    observe(element) { element.observer = this; }
    disconnect() { this.disconnected = true; }
  }
  const window = {mapboxgl: {Map: FakeMap, supported: () => true}};
  const document = {
    createElement: () => ({remove() {}}),
    head: {appendChild: element => queueMicrotask(() => assetFailure ? element.onerror() : element.onload())},
    getElementById: id => elements.get(id),
  };
  vm.runInNewContext(source, {window, document, ResizeObserver: Observer, setTimeout, clearTimeout, performance, console});
  function element(id, attached = true) { const e = {isConnected: attached, clientWidth: 600, clientHeight: 800, style: {}}; elements.set(id, e); return e; }
  return {window, created, element, failAssets: value => assetFailure = value};
}
const config = camera => JSON.stringify({accessToken: 'pk.synthetic-public-test', style: 'mapbox://styles/lookluo/synthetic', camera});
const first = {center: [-2.1, 57.14], zoom: 11, bearing: 0, pitch: 0};
const next = {center: [-2.09, 57.15], zoom: 13.5, bearing: -37, pitch: 0};
(async () => {
  const f = fixture(), status = [];
  f.element('cancel-loader');
  const cancelled = f.window.birdtieMapboxMount('cancel-loader', config(first), s => status.push(s));
  f.window.birdtieMapboxDestroy('cancel-loader');
  assert.equal(await cancelled, false);
  assert.equal(f.created.length, 0);
  assert.equal(status.length, 0);

  const element = f.element('active');
  const mounting = f.window.birdtieMapboxMount('active', config(first), s => status.push(s));
  f.window.birdtieMapboxUpdate('active', JSON.stringify(next));
  assert.equal(await mounting, true);
  const map = f.created[0];
  assert.equal(JSON.stringify(map.camera), JSON.stringify(next));
  assert.equal(map.options.interactive, false);
  assert.equal(map.options.projection, 'mercator');
  map.listeners.load();
  map.listeners.error(); map.listeners.error();
  assert.equal(JSON.stringify(status), JSON.stringify(['ready', 'failed']));
  element.observer.callback();
  assert.equal(map.resized, 1);
  assert.equal(f.window.birdtieMapboxProjection('active', '[[-2.1,57.14]]'), '[{"x":-2.1,"y":57.14}]');
  f.window.birdtieMapboxDestroy('active');
  map.listeners.load(); map.listeners.error();
  assert.equal(map.removed, 1);
  assert.equal(element.observer.disconnected, true);
  assert.equal(status.length, 2);
  assert.equal(f.window.birdtieMapboxProjection('active', '[]'), null);

  f.element('cancel-attach', false);
  const detached = f.window.birdtieMapboxMount('cancel-attach', config(first), s => status.push(s));
  await new Promise(resolve => setTimeout(resolve, 0));
  f.window.birdtieMapboxDestroy('cancel-attach');
  assert.equal(await detached, false);
  assert.equal(f.created.length, 1);
  assert.equal(status.length, 2);

  const retry = fixture(), retryStatus = [];
  retry.failAssets(true); retry.element('failed');
  assert.equal(await retry.window.birdtieMapboxMount('failed', config(first), s => retryStatus.push(s)), false);
  assert.equal(retryStatus[0], 'failed');
  retry.failAssets(false); retry.element('retry');
  assert.equal(await retry.window.birdtieMapboxMount('retry', config(first), s => retryStatus.push(s)), true);
  retry.window.birdtieMapboxDestroy('retry');
  assert.equal(retry.created.length, 1);
  assert.equal(retry.created[0].removed, 1);
  console.log('BRIDGE_BOUNDARIES_PASS');
})().catch(error => { console.error(error.message); process.exitCode = 1; });
''';
