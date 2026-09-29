# Foundation implementation status

Date: 2026-09-29  
Scope: Birdtie-native first Foundation slice. Civu remained read-only; no Civu code, schema, data or secrets were copied.

## Implemented

- `apps/api/migrations/001_foundation.sql`: Birdtie-owned Account/User profile, Consent, City, Place, provider references, private Media/EXIF, public media variant and audit tables. WGS84 coordinate ranges and public-media approval/metadata conditions are database constraints.
- `apps/api/migrations/002_place_precision_guard.sql`: upgrades existing local databases with the Place rule that `location_precision=none` must have no stored coordinates. New databases receive the same rule through `001`.
- `apps/api/migrations/003_sessions_and_profile_consent.sql`: verified-provider identity mapping, hashed sessions with absolute/idle expiry, account block edges and unique active Profile Consent grants.
- `apps/api/migrations/004_city_seed_places.sql`: city editor memberships, private Place candidates, per-Place source evidence and City Seed links. No editor or Place is seeded.
- `apps/api/migrations/005_oidc_login_handoff.sql`: five-minute OIDC state and one-minute Birdtie exchange codes, both stored as SHA-256 digests and consumed once.
- `apps/api/migrations/006_city_graph_content.sql`: draft-by-default Moment, Activity, Journey and Intent records with typed City/Place and cross-object constraints. No content is seeded.
- `apps/api/migrations/007_city_seed_activities.sql`: external host labels and private Activity candidate/source/maintenance tables for separate reviewer decisions.
- `apps/api/migrations/008_city_map_viewports.sql`: explicit per-City map provider and sourced display viewport; Aberdeen uses Mapbox and a City Council city-centre view anchor.
- `apps/api/internal/postgres/catalog.go`: published City/Place queries. Public responses include source, maintainer and freshness. Unpublished Place rows are not returned.
- The published City Place list accepts a bounded literal name/summary query. Explore List now issues this query within the selected City; search never requests device location.
- Published City responses include a map provider/viewport when configured. The Flutter web Map view uses Mapbox raster tiles, visible attribution and only public point-precision Place markers from the same City query. The China AMap renderer remains gated on a platform-matched key and security proxy.
- `apps/api/internal/postgres/identity.go` and `internal/httpapi/identity.go`: server-derived Actor, authenticated `/v1/me`, logout, narrow Profile grant/list/revoke routes and Profile read checks. Browser origins require exact configuration.
- `apps/api/internal/postgres/cityseed.go` and `internal/httpapi/cityseed.go`: authenticated Place candidate submit/list/review. Only a different Account with city reviewer permission can publish or link a candidate to an existing canonical Place; decisions and sources are retained transactionally.
- `apps/api/internal/oidcauth/service.go`, `internal/postgres/oidc.go` and `internal/httpapi/oidc.go`: generic OIDC Authorization Code/PKCE flow, state/nonce and ID Token verification, private Account enrollment, and PKCE-bound one-time exchange into a Birdtie Session.
- `apps/api/internal/media/contract.go`: provider-neutral quarantine/private/public storage, scanner, private metadata and derivative interfaces. No upload endpoint is enabled.
- `apps/api/internal/postgres/activities.go`, `internal/postgres/activity_seed.go` and corresponding HTTP handlers: published Activity reads, time-derived status, and candidate submission/review with independent editor Accounts. Public reads omit a hidden Place ID.
- `apps/api/internal/postgres/blocks.go` and `internal/httpapi/blocks.go`: owner-derived Account block/list/unblock, immediate revocation of grants between the two Accounts, and audit records. Authenticated Activity reads filter Account hosts involved in a block.
- `apps/api/internal/postgres/moments.go` and `internal/httpapi/moments.go`: Session-derived private Moment drafts, owner-only list/detail, revision-checked edits and withdrawal with audit events. No publishing route exists.
- `apps/client/lib/src/content/private_moment_controller.dart`: in-memory Session Bearer access to own drafts. Create and My Birdtie now offer text-only private draft creation, editing and withdrawal after login.
- `apps/api/cmd/city-editor-access`: trusted local operator command for ticketed grant/revoke with transactional audit. It does not create Accounts or offer a public role-management route.
- `apps/api/compose.yaml`: loopback-only local PostgreSQL 16; deployment provider remains undecided.
- Aberdeen city seed has `content_status=building` and `freshness=unverified`. No Places, Activities, users or media are seeded.

## Local verification

- `go test ./...` compiled all API packages and passed the Source freshness cases for unverified, current, changed-since-verification and expired records.
- Applied `001_foundation.sql` to local PostgreSQL 16, then applied it again successfully (`INSERT 0 0` on the second pass).
- Applied `002_place_precision_guard.sql` successfully to the existing local PostgreSQL 16 database.
- Applied `003_sessions_and_profile_consent.sql` successfully to the existing local PostgreSQL 16 database. The new Go packages compiled with `go build ./...`. No end-to-end login or Profile/Consent HTTP flow was exercised because no external identity adapter is configured.
- Applied `004_city_seed_places.sql` successfully to the existing local PostgreSQL 16 database; the City Seed API packages compiled with `go build ./...`. No editorial HTTP flow was exercised because no OIDC identity or city editor membership is configured.
- Applied `005_oidc_login_handoff.sql` successfully to the existing local PostgreSQL 16 database. OIDC API code compiled with `go build ./...`; no real provider flow was exercised because issuer and client registration are not configured.
- The Flutter web auth connector and OIDC status route passed `flutter analyze`, `flutter build web` and `go build ./...` on 2026-09-30. No provider-backed callback, Session or Profile HTTP flow was exercised.
- The client now reads published Cities and Places through the public API. Now and Explore share the selected City; Explore defaults to List and shows actual published Places and provenance. `flutter analyze` and `flutter build web` passed for this client integration on 2026-09-30. Map layers and content object routes remain disconnected.
- Applied `006_city_graph_content.sql` and `007_city_seed_activities.sql` to local PostgreSQL 16 on 2026-09-30. The new API and operator command compiled with `go build ./...`; the updated Flutter client passed `flutter analyze` and `flutter build web`. No editor, Activity or user content was inserted or exercised through HTTP.
- Owner-only Moment draft API compiled with `go build ./...`. The draft client passed `flutter build web`; static analysis was rerun after a formatting fix. No real Session or draft HTTP flow was exercised because no OIDC provider is configured.
- The bounded Place search API compiled with `go build ./...` and the Explore search client passed `flutter analyze` and `flutter build web`. No HTTP search query was exercised against seeded Place data.
- Applied `008_city_map_viewports.sql` to local PostgreSQL 16 on 2026-09-30. The API compiled with `go build ./...`; the map client passed `flutter analyze` and `flutter build web` with a Civu public Mapbox token read only into the ignored local build. No tile loading or map interaction was exercised in a browser.
- Live HTTP calls returned 200 for `GET /v1/cities`, `GET /v1/cities/aberdeen-gb`, `GET /v1/cities/aberdeen-gb/places` and `GET /readyz`. The City was `building`/`unverified`; the Place list was empty.
- Temporary public/hidden Place rows verified that published content returned 200 with `freshness=expired`, the draft returned 404, and an invalid Place ID returned 400. Both temporary rows were deleted. Final local counts: one City, zero Places, zero Media.
- Stopped the local API and Compose services after validation. The named local database volume is retained for development.

## Still required for T1 Foundation

1. Configure a real OIDC issuer/client registration and fixed browser redirect, then complete end-to-end login, Session/Profile Consent and Account recovery review. The server and Flutter web callback exist, but no provider is configured and no real login has been exercised. Native callbacks remain separate work.
2. A deployment-approved operator identity and database privilege model, editorial UI, source review operation and canonical matching assistance. A local ticketed provisioning command and candidate review boundary exist, but no real editor can enter them until OIDC and operator workflow are configured.
3. Object-storage provider and region, quarantine/scan/EXIF processing, Media/Memory ACL, key management, approved public derivatives and delete/withdrawal behavior. A provider-neutral contract exists, but no upload route is open.
4. Report handling, rate limits, session audit, pagination, hosting/PostGIS decision, managed migration tooling, backup/restore and deployment configuration. Account block/unblock is implemented for Profiles and Account-hosted Activity reads; all future social queries must apply the same boundary.

The Flutter web shell connects OIDC status, callback, Session and own Profile reads; the Bearer credential stays in running memory. The shell also reads public Cities, Places and reviewed Activities without login, and can manage private text-only Moment drafts after login. Aberdeen Mapbox map rendering is integrated for the web shell but still needs Birdtie-scoped token registration and live tile verification. Domestic AMap Web/native adapters, editorial UI, user Moment/Journey/Intent publishing, discovery ranking, connections and Agents belong to subsequent stages. No Mapbox or AMap credential is stored in source.
