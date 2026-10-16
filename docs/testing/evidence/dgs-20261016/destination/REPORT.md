# DGS-09 destination search: Civu audit and client boundary implementation

Date: 2026-10-16. Parent: `BT-FIX-NOW-UI-001`. This scoped result covers the independent browsing target, geocoder adapters/composition and MapCanvas handoff. The parent owns the picker/header/host integration and Android bridge. This is not complete-device acceptance.

## Reference baseline and actual reuse

Civu was read only. No reference service was called and no original configuration/data was changed.

| Reference | Verified implementation and limits | Birdtie reuse decision |
| --- | --- | --- |
| `D:/Program/Civu/Civu-server/api/internal/httpapi/router.go:169` and `places_handlers.go:30` | `/v1/place-destinations` calls `Store.SearchPlaceDestinations`. Only an already matched CN administrative directory item with a missing center may call `AreaCenterProvider`. That GET can invoke `UpdateGeoAreaCenter`, so it is not safely treated as a read-only external probe. | Reuse domain distinction and directory semantics, not the endpoint as a global geocoder. No live Civu call. |
| `D:/Program/Civu/Civu-server/api/internal/store/place_destinations_postgres.go:11` | Searches existing `geo_areas`, aliases and associated catalog/content statistics. No forward global geocoder. | Keep Birdtie's catalog as shortcuts; do not enumerate or seed all cities. |
| `D:/Program/Civu/Civu-server/api/internal/httpapi/places_handlers.go:57`; `place_destinations.go:10`; `place_destinations_postgres.go:248` | `/resolve` selects a registered formal city within 80 km. Outside that range it returns no destination. | Not a provider-neutral global reverse resolver. Do not require it for external target selection. |
| `D:/Program/Civu/Civu-server/api/internal/placeprovider/amap.go:212,395`; `multi_source.go:216` | Existing server area-center resolution uses AMap district data. The old `AreaCenter` shape has no CRS field and returns parsed provider coordinates without a conversion at that method. | Do not copy the ambiguous center contract. Birdtie's Android bridge declares GCJ-02; its Dart adapter converts once at the existing WGS84 boundary. |
| `D:/Program/Civu/Civu-client/lib/src/data/repositories/domain_repositories.dart:2391,2436`; `api/api_client.dart:912` | Repository delegates to those catalog endpoints; the alternate branch uses demo/bundled data. | Do not import demo fallback, provider state, authentication or repository. |
| `D:/Program/Civu/Civu-client/lib/src/models/place_destination.dart` | Separates geographic hierarchy, display name, center and optional camera framing, but assumes catalog identity and has no explicit Temporary storage permission. | Adapt concept into independent `BrowseDestination` with optional internal City identity and explicit storage mode. |
| `D:/Program/Civu/Civu-client/lib/src/widgets/civu_destination_picker.dart:517` | 300 ms debounce and generation checks; empty directory cache and complete persisted visit snapshots; manual-city candidate may have no verified center. | Reuse debounce/latest-query semantics. Do not copy directory persistence for Temporary provider responses, manual guessed centers, old route or widget dependencies. Add IME composing and transport cancellation. |
| `D:/Program/Civu/Civu-client/packages/amap_map/android/build.gradle:37` | Bundle already contains `3dmap-location-search:11.2.100_loc11.2.100_sea9.8.1`. No implemented Civu Dart/MethodChannel geocoding/POI/district-search entry was found in client `lib`, `android`, or vendored plugin source. | Parent adds a narrow Android search bridge using the included SDK and current Manifest Android Key. No Android Key is sent to WebService. Earlier audit's “no reusable search entry” is not a missing-SDK or missing-Key claim. |

Versions: Civu root HEAD and tracked `origin/master` were `38d4aee05ecf86e29b365545273a87efc6f267cf`; client HEAD/tracked origin were `1bbf0613b513001447f326225c944c1a1fbd8e8f`; server HEAD/tracked origin were `0489536a0eb0792deff7793c8b143ad3e741d5ce`. Audited tracked client/server files had no local diff from their tracked origin. A small live `git ls-remote` confirmed client master at `1bbf0613`; server GitHub connection failed after 21 seconds, so newest server remote HEAD is **NOT_VERIFIED**. No fetch or original-tree writes occurred. Civu's REQ-324 “global destination” refers to a search-group naming/metadata fix and explicitly excludes changing directory data; its historical DONE status is not evidence of global geocoding.

## Birdtie implementation

| Actual path | Behavior |
| --- | --- |
| `apps/client/lib/src/city/browse_destination.dart` | Geographic browsing object, WGS84 center, optional bounds, explicit provider/external ID, country/region/type/attribution/storage mode, nullable internal City ID; conservative bounds-based camera hint. |
| `apps/client/lib/src/city/destination_search_service.dart` | Provider-neutral interface; Mapbox v6 Temporary adapter; combination with parent's consent-gated AMap adapter. No country or current-viewport hard filter. Bounded response/timeout, sanitized failure, no provider result cache/storage. Independent provider failures retain available results with `partialError` and bounded provider backoff. |
| `apps/client/lib/src/city/destination_search_controller.dart` | 300 ms debounce, IME composing guard, explicit submit/retry/cancel/dispose, abort and latest-query generation fencing, 20-query/minute budget, bounded failure backoff. Recent queries retain user-entered strings only. |
| `apps/client/lib/src/city/public_city_controller.dart` | `selectedDestination` and `browseKey` are distinct from `selectedCity`. `selectedCity` remains an actual catalog row. `renderCity` is a renderer-only compatibility projection; its external view key never enters `cities`, old City requests, persistent store or publication/task identity. `contentCityID` is empty without a real internal City. Catalog reload and late local restore cannot overwrite an explicit external destination. Android may restore an existing AMap catalog camera. |
| `apps/client/lib/src/workspace/map_canvas.dart` | Uses renderer target for the existing map, enables real viewport callbacks for external targets, sends empty City ID to existing viewport content API, and fences clicks/callbacks with the browsing key. Existing native map key/subtree remains. Legacy typed layers and catalog operations retain actual internal City guards. |
| `apps/client/test/destination_search_test.dart` | Provider contract and storage boundary, composition/consent/backoff, sanitized errors, IME/debounce/cancellation/late-result tests. |
| `apps/client/test/destination_map_target_test.dart` | No fake City requests/persistence, catalog/restore stability, provider rendering boundary, actual MapCanvas-to-OwnedMap viewport request without `cityId`, retained map subtree and stale-click fence. |
| `apps/client/test/public_city_exploration_test.dart` | Existing exploration regression, updated Android AMap availability expectations, explicit unsupported-platform fallback. Parent registered this exact test path before edits. |

`PublicCityMapView`, native map engines, schema and business tables were not modified by this scoped worker. The existing `OwnedMapAPI.read` omits empty `cityId`; server `content.MapContentQuery.Valid` already supports it. No pseudo POI or City row was created. Existing city-specific operations are not silently rerouted through an external ID.

## Provider permission and real capability probe

Official Mapbox documentation checked on this date: <https://docs.mapbox.com/api/search/geocoding/>. Temporary is the default and cannot be cached; Permanent has separate storage entitlement. Geocoding results must accompany a Mapbox map. V6 does not provide POI search. Consequently Mapbox CN candidates are not forwarded to AMap. Native AMap results must declare their own provider/CRS and are converted only once by the parent's adapter.

Two authorized, read-only Mapbox v6 calls using existing public mobile configuration returned **HTTP 200**, five candidates each for `Paris` and `巴黎`, with attribution. They sent no hard country/bounds filter. Credential values, exact coordinates and raw responses were not logged or persisted. `live-provider-check.json` records only sanitized status/count/configuration-path facts. This proves those calls worked with the existing configuration; it does not establish Permanent entitlement or native AMap authentication.

The default composition invokes native search only after `AMapDestinationSearchService.enabled` confirms explicit current-session agreement. When unavailable, CJK input or a CN candidate produces `domesticUnavailable` rather than falsely claiming that the place does not exist. A provider failure is surfaced even when another provider supplies usable results. A failed/cancelled search does not choose a destination or move the map.

## Executed tests and evidence

Working directory: `D:\Project\birdtie\apps\client`.

```powershell
flutter test --no-pub test/destination_search_test.dart test/destination_map_target_test.dart test/public_city_exploration_test.dart --reporter expanded
```

- First run: **22 PASS / 2 FAIL, exit 1**; `targeted-first.log`. Both failures were old exploration assertions that assumed Android AMap was unavailable. The seven initial new tests passed. No failures were removed; the parent registered the exact existing test file before expectations were adapted to the now-implemented native adapter.
- Final scoped run after composition and the unsupported-platform test: **26 PASS, exit 0**; `targeted-composite.log`. Eight new destination tests plus eighteen exploration tests. This is a direct selected regression set, not a full matrix.
- `targeted-composite.json` contains command, log timestamps and eight source/test SHA-256 values. Hashes were captured after the run with no assigned-file edits after the preceding formatter. Parent's eventual integrated run should capture its complete pre-run sourceframe separately.
- Scoped `git diff --check`: exit 0; only Git's line-ending conversion notices.

**NOT_RUN:** APK build/install, real-device interaction, native AMap actual authentication/search, complete UI end-to-end flow, deployment, performance measurements. Parent-owned bridge/picker tests are separate and not claimed by this scoped receipt. No production/Civu data writes occurred. Web remains out of scope.
