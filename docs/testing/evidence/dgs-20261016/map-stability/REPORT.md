# DGS map reading stability — deterministic regression and repair

Date: 2026-10-16. Parent task: `BT-FIX-NOW-UI-001`. This is a scoped client reading-boundary result, not completion of the complete DGS task or real-device acceptance.

## Baseline and boundaries

- Read baseline: `cef1a11ada611247021c4e81593c50437f336a66`, branch `codex/birdtie-import`. The attachment's `4637cfb` baseline predates current implementation and was not restored.
- Preserved other contributors' pending changes, Android Key/style work and the current task queue. Civu was read only.
- The reproduction uses isolated mock HTTP responses and widget timers. No production data, media, identity or relationship writes were performed.
- The Android 37 source/build freeze precedes these edits. Its package must not be treated as containing or validating this patch.

## Reproduced failure and repair

Two authorized map pins are loaded. The first pin's cover returns either `404/not_found` or `409/moment_changed`. Before the repair, the API called a global denial callback; the controller retired the entire map, including the second, unaffected pin. Both deterministic cases failed the expected-unaffected-pin assertion. This establishes a real code-path defect; it does not prove that this is the sole cause of the user's production disappearance report.

The API now supplies a bounded, structured failure to the controller. Confirmed resource failures remove the affected source, retire its active media, invalidate the old source object and schedule one coalesced revalidation per resource and identity. Other authorized pins remain. Old detail/media grants do not become current merely because a fresh read returns the same source version.

| HTTP error | Current verified contract | Handling |
| --- | --- | --- |
| 404 with `not_found`, trusted moment/place resource path | Resource is not currently readable | Invalidate that resource and revalidate |
| 409 with `moment_changed`, trusted resource path | Resource version changed | Invalidate that resource and revalidate |
| 401 | Identity invalid | Retire all grants; no automatic replay |
| 403, including `person_account_required`, `organization_admin_required`, or unknown code | No reliable resource-only scope in the current API | Retire all grants |
| 409 `human_control_changed` | Identity/control binding changed | Retire all grants |
| Unknown 404/409, all 410 | Scope cannot be established safely | Retire all grants |
| Timeout, HTTP client failure, 5xx | Transient reading failure | Keep still-valid snapshots only to their original deadline; bounded retry |

The current server does not provide a distinct resource-scoped 403/410 contract. This patch does not infer such a scope from the status alone. Error inspection is limited to 2 KiB and one second, accepts only the existing code whitelist, and does not persist response bodies.

## Decode and lease rules

Both map decoders previously mapped the entire item list through a throwing decoder. A single malformed row could therefore discard the entire batch. Each decoder now rejects a complete malformed item independently, reports a local `rejectedItemCount`, and preserves other validated items. No wire-schema relaxation or new wire field was introduced. An all-rejected nonempty batch throws an explicit read error rather than pretending to be empty.

| Decoder | Retained strict envelope / identity rules | Isolated row handling |
| --- | --- | --- |
| `OwnedMapView` / `map-content-v2` | Schema, `MAP_CONTENT` scope, viewer, city, bounds, zoom, source version, original lease, count/pagination contract, duplicate valid map IDs | Bad pin rejected whole; untitled `MOMENT_ANCHOR` accepted; actual place name still required |
| `TypedMapView` | Schema, public/private/author scope, city/author binding, bounds, source version, original lease, duplicate valid IDs | Bad item rejected whole; existing untitled MOMENT behavior preserved; position bounds use dateline-aware `MapBounds.contains` |

Invalid global metadata remains fail closed. A malformed viewer binding cannot retain an old Owned map grant. Transient failure does not extend an authorization deadline. Typed public and author refreshes retain a previous valid snapshot for transient failures only; permission or malformed-contract failures still remove it.

## Changed paths

- `apps/client/lib/src/workspace/owned_map_api.dart`: structured failure scope, bounded error-code inspection, strict per-item decode isolation, untitled coordinate anchor acceptance.
- `apps/client/lib/src/workspace/owned_map_controller.dart`: source-scoped invalidation, old-grant retirement, bounded revalidation/recovery, explicit partial-decode state.
- `apps/client/lib/src/workspace/owned_map_media_controller.dart`: revoke active pixels for affected pins, including shared active media entries.
- `apps/client/lib/src/workspace/map_layers_api.dart`: strict per-item decode isolation and dateline-aware point bounds.
- `apps/client/lib/src/workspace/map_layers_controller.dart`: retain transiently unavailable public/author snapshots only until their original lease; partial-decode state.
- `apps/client/test/dgs_map_stability_test.dart`: resource/global denial, recovery bounds, lease and global-metadata regression tests.
- `apps/client/test/dgs_map_decode_isolation_test.dart`: partial/all-bad/global-metadata decoder tests.
- `apps/client/test/map_layers_api_test.dart`: update six existing row-error assertions to the approved isolation contract; global-error assertions remain.
- `apps/client/test/map_layers_partial_read_test.dart`: update transient 503 expectation to retain the still-valid public snapshot.

The host identity scheduling changes in `map_workspace.dart` belong to the parent task and are not included in this scoped result.

## Executed verification

Working directory: `D:\Project\birdtie\apps\client`. Flutter executable: `D:\DevTools\flutter\bin\flutter.bat`.

```powershell
flutter test --no-pub test/dgs_map_stability_test.dart test/dgs_map_decode_isolation_test.dart test/content_map_controller_test.dart test/content_map_coverage_test.dart test/map_layers_auto_refresh_test.dart test/map_layers_partial_read_test.dart test/map_layers_api_test.dart --reporter expanded
```

Final result: **49 tests PASS, exit 0**. `final-targeted.log` contains full expanded output. `final-targeted.json` contains the command, timestamps, exit code and SHA-256 for all nine changed source/test files at execution. The selected existing coverage includes cancellation, bounded coverage/media caching, permissions, automatic refresh and partial reads. No full matrix was repeated. Scoped `git diff --check` also returned exit 0; Git emitted only pre-existing line-ending conversion notices.

Failure history is retained:

- `before-fix.log` / `.json`: initial test waited only for request dispatch. The assertion ran before asynchronous denial cancellation settled. This is test-timing evidence, not the final global-clear reproduction.
- `before-fix-settled.log` / `.json`: corrected settling with unmodified production sources; **2 FAIL, exit 1**. Both unaffected pins disappeared. This is the valid pre-fix regression evidence.
- One intermediate test-file placement mistake failed compilation and was corrected before execution; its tool output was not saved as a log. It is not runtime evidence.
- `targeted-first.log`: initial 14 new tests passed before the final metadata regression was added.
- `affected-existing-first.log`: **27 PASS / 7 FAIL, exit 1**; six old whole-batch row-rejection assertions and the old immediate-clear-on-503 assertion conflicted with the approved new behavior. Parent registered and authorized the two exact existing test paths before they were updated.
- `final-targeted.log`: all selected **49 PASS**, including 15 new tests, after the authorized assertion updates.

## Not executed / remaining external validation

| Verification | Status |
| --- | --- |
| APK build containing this patch | NOT_RUN |
| Installation / real-device map interaction | NOT_RUN |
| Production API / live Civu-content disappearance reproduction | NOT_RUN |
| Deployment | NOT_RUN |
| Complete DGS end-to-end matrix | NOT_RUN |
| Parent host identity/organization automatic recovery acceptance | Outside this scoped result; do not infer PASS |

This evidence does not replace a new package and device check against the matching API. No production data cleanup, destination search or social acceptance is claimed here.
