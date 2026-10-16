# Native China map adapter — source validation

The source implementation is `apps/client/lib/src/city/amap_city_map_io.dart`. It keeps Birdtie's current account, source/lease checks, MapDisplayPolicy, square moment bitmaps, overlap chooser and media controllers, and uses only the AMap plugin for the native map. The shared `MapCoordinateBoundary` converts WGS84 at the SDK display/projector boundary; choosing a point converts back and does not rewrite an existing content location.

## Reuse and behavior

| Responsibility | Implementation |
| --- | --- |
| Native SDK bridge | Selected Civu `packages/amap_map` at commit `1bbf0613b513001447f326225c944c1a1fbd8e8f`; 122 source files, no old application screens or navigation. Exact file hashes, exclusions and patch history: `plugin-provenance/source-map.json`, `plugin-provenance/AUDIT.md`. |
| Marker policy | Existing `MapDisplayPolicy.plan`, current source/version validation, stable IDs and plugin `MarkerUpdates` add/change/remove. Unchanged marker instances are reused. |
| Shapes and media | Existing `MapMarkerBitmap` and permission-aware `MapMediaPixels`. Moment marks stay square. Adapter does not fetch media or increase the upstream 12-item/4-request budget. |
| Viewport | Actual native four-corner and center inverse projection; 180 ms settle debounce. Camera/source epochs retire stale async results. Android uses the real screen DPR, independently of bitmap raster ratio; iOS uses logical points. PNG dimensions are normalized to real screen scale when needed, because Android passes bitmap pixels through and iOS decodes with UIScreen.scale. |
| Bounded work | Existing latest-only render queue; at most four in-flight projection calls. Geometry cache holds only current source IDs at the current camera, and reuses projections for cover-only refreshes. Scope changes clear it; resize/camera/DPR changes invalidate it. |
| Current interaction | Touches use current real projected hit targets and current entity source version; actual overlaps open the existing selector or current content overlap callback. No fallback to unrelated POI. |
| Native pixels and lifecycle | Session/source changes retire hit targets and cover native pixels until the incremental update crosses a platform ordering barrier. Original media grant deadline also triggers retirement. Map widget survives normal panel/source rebuilds, with SDK pause/FPS controls and observer cleanup. Runtime validation is still required. |
| Privacy and configuration | Explicit Chinese service disclosure, official policy link and session consent before initialization; `BIRDTIE_AMAP_ANDROID_KEY` / `BIRDTIE_AMAP_IOS_KEY`. No device-location request. Missing key reports unconfigured state. Native debug argument logging is disabled in all build modes. |

The SDK's base labels remain map cartography. POI interaction is disabled. Birdtie business markers come exclusively from its current reader source; the adapter does not add the city's ordinary POIs.

## Actual validation

- `pub-get01.log`: dependency resolution from the existing offline cache completed and added five dependencies. **The command failed with exit 1** in Flutter's Windows symlink-support post-step. No global OS setting was changed.
- `analyze01.log`: initial adapter analysis exit 0; 0 errors, 0 warnings, 8 informational lints. This predates the final projection-cache/expiry refinements; no old source hash was captured.
- `analyze02.log`: geometry/expiry refinement analysis exit 0; 0 errors, 0 warnings, 9 informational lints.
- `analyze03.log`: final adapter, including real DPR and PNG scale normalization, analysis exit 0; 0 errors, 0 warnings, 10 informational lints. Formatting exit 0. Exact commands, cwd and source hashes are in `receipt.json`.

`plugin-file-hashes.json` inventories every vendored file, including NOTICE. `dependency-file-hashes.json` inventories every file of the five newly resolved packages from their actual package-config locations; source dependency bodies were not copied into the evidence directory. Their counts and inventory hashes are included in the receipt. The lockfile and manifest hashes are also recorded.

Consent initialization was reviewed against the actual plugin: its Dart setters store the privacy statement/key before the widget is built; Android applies the statement before constructing MapView, and iOS before allocating MAMapView. No native map is created before this adapter's explicit consent button. This is source review, not device proof.

The plugin audit's `NOT_RUN` resolution statement records the earlier independent audit. This receipt records the later actual `pub get` result and does not relabel that command as a pass.

## Subsequent plugin lifecycle repair

The controller now cancels its nine event subscriptions, drops its widget-state reference and disposes idempotently. Channel disposal removes the incoming handler. Widget disposal is synchronous; a pending or late native initialization cannot invoke `onMapCreated`, and resumed asynchronous overlay updates check whether the widget is still mounted. Source provenance and NOTICE explicitly record these three production-file changes.

`lifecycle-test01.log`: the single test `AMap disposal cancels all streams and native handlers without late revival` passed, exit 0. The exact file is `apps/client/packages/amap_map/test/amap_lifecycle_test.dart`. It exercises the real AMapWidget/controller/dispatch with a mock native view, confirms nine subscription cancellations and removed incoming handlers, repeated disposal, and a delayed initialization after unmount. Only this test file was run. Lifecycle revision hashes and its new full file inventories are recorded separately in `receipt.json`; previous analysis and inventory files remain intact.

**NOT_RUN:** Android/iOS builds, SDK initialization with a valid preview key, device interactions, native marker permission retirement, projection calibration, native performance before/after, APK installation, live API and deployment. Code paths and bounded-work design are not measured performance or full publish-chain evidence.

## Candidate APK compile-SDK alignment

The root task's real `candidate-apk35` build failed at `:amap_map:checkProfileAarMetadata`: the resolved `flutter_plugin_android_lifecycle` dependency requires compile SDK 36 or newer, while the vendored plugin declared 35. Birdtie changes only the plugin's `compileSdkVersion` to 36, matching the app; min SDK 21 and the AMap SDK dependency remain unchanged. This worker performed no additional test or build for the change. The historical NOT_RUN statements above describe the earlier source-validation phase; the new failure is recorded separately, and a successful rebuild is not yet claimed.
