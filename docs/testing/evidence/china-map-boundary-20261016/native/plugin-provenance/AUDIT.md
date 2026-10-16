# AMap selected plugin source audit

Source: Civu-client commit `1bbf0613b513001447f326225c944c1a1fbd8e8f`, `packages/amap_map`, working tree clean. Exact mapping: `source-map.json`.

- Plugin 1.0.15 declares Apache 2.0 and retains BSD-style source headers; LICENSE and NOTICE preserve attribution. This is a source selection, not a new SDK license grant.
- Locally cached `x_amap_base` 1.0.3 and `x_common` 1.0.4 exist in both pub.dev and pub.flutter-io.cn. Both have Apache 2.0 LICENSE files. x_common also requires flutter_adaptive_scaffold and logger. Resolution NOT_RUN.
- Initialize `AMapInitializer.init(context, apiKey: AMapApiKey(androidKey: ..., iosKey: ...))`, then `AMapInitializer.updatePrivacyAgree(AMapPrivacyStatement(hasContains: true, hasShow: true, hasAgree: true))` only following the actual consent gate and before AMapWidget construction. No credential values copied.
- Controller projectors: `toScreenCoordinate(LatLng)` / `fromScreenCoordinate(ScreenCoordinate)`. Android uses raw physical pixels; iOS uses UIKit points. Visible bounds are not directly exposed; invert the actual four map corners. A paused/disposed or zero-size map must not issue a query using guessed coordinates.
- `Marker.setIdForCopy(stableId)` sets a stable Dart marker ID, `copyWith` retains it; `MarkerUpdates.from(previous,current)` computes native add/change/remove by IDs. Keep marker IDs separate from mutable source versions; click handlers need current source validation in Birdtie.
- Retain MapView, hybrid composition, MapRenderLifecycle, FPS/pause controls and SDK upgrade. Android SDK 35/min21 and AMap 11.2.100_loc11.2.100_sea9.8.1. iOS podspec retains AMap3DMap dependency, iOS minimum12, old podspec version1.0.8; CocoaPods/build not run.
- Strip capture remains because controller/native dispatch reference it; no new invocation introduced.
- Birdtie sets native debugMode false for every build mode so Android key/coordinate argument logging is not enabled by debug builds. The selected-copy audit recorded this source-line policy change with before/after SHA-256.
- No current-upstream download/diff; source compared to local committed Civu baseline only. No test, pub get, build, live service or device actions. All runtime validation is NOT_RUN.

The bullets above record the initial selection audit. A later Birdtie lifecycle repair modifies the controller, widget and method-channel implementation to cancel subscriptions, remove handlers and reject late initialization after disposal. `source-map.json` preserves the original Civu hashes and declares those additional changes. Its generated-file list includes Birdtie's new lifecycle test. The parent `receipt.json` and `lifecycle-test01.log` record the single passing mocked Dart lifecycle test; native SDK/device checks remain NOT_RUN.

The root task subsequently ran candidate-apk35, which failed AAR metadata validation because the current lifecycle dependency requires compile SDK 36. Birdtie changes `android/build.gradle` from compile SDK 35 to 36, retaining min SDK 21 and the AMap version. Source mapping and the parent's `compileSdkRevision` receipt retain the original hash, actual failed build evidence and new hash. This worker did not rebuild or rerun tests for that one-line compatibility correction.
