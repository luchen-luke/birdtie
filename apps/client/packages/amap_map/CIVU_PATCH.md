# Vendored `amap_map` 1.0.15 (Civu patch)

Upstream: https://pub.dev/packages/amap_map

## Why vendored

Official Android code uses `TextureMapView`. On some devices (observed Xiaomi /
Adreno) that path aborts in `libAMapOpenMap.so` during `onSurfaceCreated`
(SIGABRT). Dart cannot catch that native crash.

## Patch

- `TextureMapView` → `MapView` in:
  - `android/.../AMapPlatformView.java`
  - `android/.../core/MapController.java`

Re-evaluate when upstream ships a fix; prefer dropping this path package then.


## Birdtie selected vendoring, 2026-10-16

- Copied source is Civu-client HEAD `1bbf0613b513001447f326225c944c1a1fbd8e8f`, package working tree clean. Existing source files remain byte-identical except this appended provenance note, the privacy patch, lifecycle repairs and compile SDK alignment described below.
- Retained Android `MapView` (rather than `TextureMapView`), expensive Android hybrid composition, combined Activity/covered-sheet `MapRenderLifecycle`, and native render FPS/pause controls. These are source findings, not new Birdtie runtime verification.
- Retained Android 16 AMap SDK upgrade from Civu commit `de8383ca984a5e13ff062341c63d3a7967f99885`: `3dmap-location-search:11.2.100_loc11.2.100_sea9.8.1`, min SDK 21. Birdtie changes compile SDK 35 to 36 to match its app and the current lifecycle dependency. The real candidate-apk35 build failed at `:amap_map:checkProfileAarMetadata` with the compile-SDK requirement; the failure is retained, and the root task owns the subsequent rebuild.
- Retained iOS render cadence/lifecycle and strip-capture methods. Strip capture is directly referenced by the exported controller and native dispatch; it is not invoked by this vendoring and is kept to avoid a partial API or build change.
- No current-upstream fetch/diff, dependency resolution, test, build, device or production check was run for this copy. Those checks are `NOT_RUN`; existing Civu history does not establish Birdtie acceptance.
- Android privacy/key initialization precedes MapView construction in the factory, but invalid or omitted consent is not a reliable host gate: native code catches errors and still attempts construction. Birdtie must withhold AMapWidget until explicit consent and a usable platform key. Initializer state is static in Dart; consent must not be inferred from an old widget or natural-language text.
- Android screen projections use physical pixels; iOS projections use UIKit points. No native visible-bounds method is exposed; use actual view corners and platform-appropriate units with `fromScreenCoordinate`.
- Birdtie privacy patch: `lib/src/core/method_channel_amap_map.dart` sets native `debugMode` to `false` for all build modes, replacing `kDebugMode`. This prevents the Android plugin argument logs (creation key and coordinates) from being enabled by debug builds.
- Birdtie lifecycle repair: `lib/src/amap_controller.dart` owns and cancels all nine stream subscriptions, guards queued callbacks, releases the old MapState reference and disposes idempotently. `lib/src/core/method_channel_amap_map.dart` unregisters the incoming method handler when disposing its channel. `lib/src/amap_widget.dart` disposes synchronously, removes a pending-init channel, retires a late controller without a creation callback, and guards asynchronous overlay updates after unmount. The public misspelled `disponse` API is retained for compatibility.
- New Birdtie `test/amap_lifecycle_test.dart` uses the real widget, controller and incoming method dispatch with mocked native construction. One test passed with `flutter test --no-pub --reporter expanded packages/amap_map/test/amap_lifecycle_test.dart` from `apps/client`; this is a Dart lifecycle test, not Android/iOS SDK runtime acceptance. The earlier selected-copy audit and analysis results remain historical evidence.
- Third-party SDK usage terms and application-key/signature validity are separate from the plugin source license. No keys were copied or embedded.
- Per-file source/destination hashes are in `docs/testing/evidence/china-map-boundary-20261016/native/plugin-provenance/source-map.json`.
