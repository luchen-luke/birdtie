# Birdtie Foundation API

Birdtie's Go API reads published City, Place and Activity records from a Birdtie-owned PostgreSQL schema. The initial Aberdeen city is marked `building` and `unverified`; no sample Places, Activities, People or Groups are presented as live data. The Agent task endpoint searches public, current City Graph records with transparent text matching. Session and Profile Consent routes establish the private access boundary. City Seed Place and Activity editorial routes can publish only after separate reviewer approval. A generic OIDC login flow is implemented but remains disabled until an issuer and client registration are configured. Owner-only Moment drafts and Agent task history are available after login. Profile editing, media upload and user content publishing are not implemented.

## Local database

From `D:\Project\birdtie\apps\api`:

```powershell
docker compose up -d
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/001_foundation.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/002_place_precision_guard.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/003_sessions_and_profile_consent.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/004_city_seed_places.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/005_oidc_login_handoff.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/006_city_graph_content.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/007_city_seed_activities.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/008_city_map_viewports.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/009_agent_workspace.sql
$env:BIRDTIE_DATABASE_URL = 'postgres://birdtie:birdtie_local_only@127.0.0.1:55432/birdtie?sslmode=disable'
go run .
```

The Compose database binds only to local loopback. Its password is for local development, not deployment. Migration `001` is idempotent for the initial schema and seed; `002` adds the coordinate privacy constraint to older local databases; `003` adds Session and Profile Consent; `004` adds City Seed Place editorial tables; `005` adds short-lived OIDC state and one-time Birdtie exchange codes; `006` adds draft-by-default City Graph content tables; `007` adds reviewed City Seed Activity candidates; `008` adds per-City map provider and sourced, display-only viewport. Use an independent credential and managed migration process before deployment.

Migration `009` adds private Agent task history and draft-by-default Communities. No Community or Agent task data is seeded.

`BIRDTIE_DATABASE_URL` is required. `BIRDTIE_API_ADDR` defaults to `127.0.0.1:8080`. `BIRDTIE_ALLOWED_ORIGINS` is an optional comma-separated allowlist for browser clients; no cross-origin access is enabled by default.

## Public read routes

- `GET /healthz`
- `GET /readyz`
- `GET /v1/cities`
- `GET /v1/cities/{cityID}`
- `GET /v1/cities/{cityID}/places`
- `GET /v1/places/{placeID}`
- `GET /v1/cities/{cityID}/activities`
- `GET /v1/activities/{activityID}`
- `GET /v1/accounts/{accountID}/profile` (only explicitly public Profiles for anonymous callers)
- `POST /v1/cities/{cityID}/agent/tasks` with JSON `{"query":"Find badminton this weekend"}` (anonymous or Bearer; query up to 240 UTF-8 bytes)

The Agent endpoint returns `{data:{cityId,query,mode:"rules",activities,people,groups,places,taskId?}}`. It searches published, current records in one City using up to six significant query terms and bounded lists. This is literal text matching, not semantic matching, recommendation or an LLM. People come from owner-confirmed, active public Intents joined to explicitly public Profiles; only a coarse area label is returned. Groups come from owner-confirmed, verified, published public Communities. Neither People nor Groups have seeded records or an open publishing flow. Signed-in searches apply mutual Account blocks to person-owned results. Public point coordinates can come only from an eligible published, unexpired Place; People have no map point. Anonymous searches are not saved.

The City Place list accepts optional `q` (up to 240 UTF-8 bytes) for a literal, case-insensitive name/summary substring search. It searches published Places in the selected published City and returns at most 100 results ordered by name, with the same provenance and location-precision rules as the unfiltered list. It does not use a map provider or location permission. Pagination, category/time filters and a search index remain future work.

Published City responses may include `map: {provider, latitude, longitude, defaultZoom, sourceRef}`. This is a display viewport, not a City centroid or a Place. Aberdeen selects `mapbox`; no other City is seeded. Public Place coordinates remain WGS84, and only `location.precision=point` is eligible for an exact map marker. See ADR 0006 for the Mapbox/AMap boundary.

## Private Agent task history

- `GET /v1/me/agent-tasks`: list up to 50 recent tasks belonging to the Bearer Session's Account.
- `GET /v1/me/agent-tasks/{taskID}`: restore one owned task. A task owned by another Account returns 404.

Signed-in `POST /v1/cities/{cityID}/agent/tasks` saves the query and City after a successful search and returns `taskId`. Restore reruns the query against current publication, expiry and block rules; result snapshots and conversation turns are not stored. The client currently keeps conversation and viewport state in memory only. No task deletion or completion route exists yet. Apply rate limits and a fuller identity review before external rollout.

## Session and Profile Consent routes

- `GET /v1/me`: resolve the Account from an opaque Bearer session.
- `POST /v1/session/logout`: revoke the current session.
- `GET /v1/me/blocks`: list the caller's blocked Account IDs.
- `POST /v1/me/blocks`: block `{"accountId":"<UUID>"}`; current grants between the two Accounts are revoked.
- `DELETE /v1/me/blocks/{accountID}`: remove the caller's block; revoked grants remain revoked.
- `GET /v1/me/consents`: list up to 100 of the caller's Profile grants.
- `POST /v1/me/consents`: grant one active Account `profile_view/read` for a bounded period. JSON body: `{"recipientAccountId":"<UUID>","expiresAt":"<RFC3339 time>"}`.
- `DELETE /v1/me/consents/{grantID}`: revoke one of the caller's Profile grants.
- `GET /v1/accounts/{accountID}/profile`: allow the owner, an eligible grant recipient, or an anonymous viewer of an explicitly public Profile; inaccessible Profiles return 404.

The protected routes require `Authorization: Bearer <opaque session>`. The server creates a Session only after the OIDC callback verifies an ID Token and a client redeems a one-time, PKCE-bound Birdtie code. Never use a client-supplied Account ID as the actor. Sessions have an eight-hour absolute lifetime and a rolling thirty-minute idle limit.

An authenticated Activity read filters records hosted by an Account involved in a block with the viewer. Anonymous public reads cannot apply a viewer-specific block. The Flutter web shell sends its in-memory Bearer credential on Activity reads after login and refreshes that list when login state changes. Place and external-host City Seed Activity records are not person-owned social content.

## Private Moment drafts

- `POST /v1/me/moments`: create a private draft for a published City, with an optional published Place in that City. Required fields: `cityId`, `title`, `body`, `timePrecision`, `locationPrecision`; `placeId` is empty or a Place UUID, and `occurredAt` is optional only when time precision is `unknown`.
- `GET /v1/me/moments`: list up to 100 of the caller's active drafts, newest first.
- `GET /v1/me/moments/{momentID}`: read the caller's own Moment.
- `PUT /v1/me/moments/{momentID}`: update an own draft with the full input and its current `revision`; a stale revision returns 409.
- `DELETE /v1/me/moments/{momentID}?revision=N`: withdraw an own Moment using its current revision; withdrawn records leave the active draft list.

The actor always comes from the Bearer Session. Creates, updates and withdrawals are audited. Drafts remain private; there is no public Moment read or publish route. The Flutter client currently creates and edits only city-level, unknown-time, text-only drafts.

## Generic OIDC login

Configure a registered OIDC client through `BIRDTIE_OIDC_ISSUER`, `BIRDTIE_OIDC_CLIENT_ID`, optional `BIRDTIE_OIDC_CLIENT_SECRET`, `BIRDTIE_OIDC_REDIRECT_URI` (the API's `/v1/auth/oidc/callback` URL), and `BIRDTIE_OIDC_CLIENT_REDIRECT` (a fixed client callback URL). Configure `BIRDTIE_ALLOWED_ORIGINS` for a separate browser client origin. If OIDC is unconfigured, auth routes return `oidc_not_configured`; partial configuration fails startup. External URLs and provider endpoints must use HTTPS; HTTP is accepted only for loopback API/client callbacks.

`GET /v1/auth/oidc/status` returns `{"data":{"configured":true|false}}` without disclosing provider settings. The Flutter web shell uses it to enable Sign in only when the API has a provider configured.

1. The client generates and temporarily retains a PKCE verifier, then navigates to `GET /v1/auth/oidc/start?challenge=<S256 challenge>`.
2. Birdtie stores one-time state, nonce and a separate provider PKCE verifier, then redirects to the configured OIDC issuer.
3. `GET /v1/auth/oidc/callback` exchanges the provider authorization code and verifies the signed ID Token, audience and nonce. A newly verified issuer/subject receives a private Birdtie Account/Profile; an existing Account is reused. Birdtie redirects only to the configured client URL with a one-minute Birdtie exchange code and the client challenge.
4. The client checks that challenge against its pending login and calls `POST /v1/auth/oidc/exchange` with JSON `{"code":"<one-time code>","verifier":"<original client verifier>"}`. The response contains the opaque Bearer Session. The client removes the code from browser history and keeps the Bearer credential in memory or platform secure storage, not a URL or browser local storage.

The Flutter web client implements the fixed callback and holds the Birdtie Bearer credential only in memory; reload requires another login. Native mobile/desktop callbacks remain unimplemented. The server does not retain provider access or refresh tokens. No issuer, client ID, client secret or callback URL is configured in this repository, so a real OIDC login has not been exercised. Before external use, complete provider registration, rate limits, deployment HTTPS, secret storage and end-to-end identity review.

## City Seed Place and Activity editorial routes

- `POST /v1/cities/{cityID}/place-candidates`: an active city contributor submits a sourced Place candidate. Body: `name`, `categoryCode`, optional `summary`, `locationPrecision`, optional WGS84 `latitude`/`longitude`, `sourceLabel`, HTTPS `sourceUrl`, `rightsNote`, `expiresAt`, and optional `providerCode`/`providerPlaceId`/`attribution` together.
- `GET /v1/cities/{cityID}/place-candidates`: active city editors see up to 100 pending candidates.
- `POST /v1/place-candidates/{candidateID}/review`: another city reviewer submits `decision` (`publish`, `link_existing`, or `reject`), `note`, and `targetPlaceId` only for `link_existing`.

The reviewer must use a different Account from the submitter; operational policy must also establish independent human review. Source and rights evidence require human review. No city editor membership or Place candidate is seeded. OIDC has no provider configuration yet, so these routes are not yet usable by real editors. See `docs/decisions/0003-city-seed-editorial-and-oidc-direction.md`.

- `POST /v1/cities/{cityID}/activity-candidates`: an active city contributor submits `title`, optional `summary`, `hostLabel`, `startsAt`, `endsAt`, `timeZone`, optional published `placeId`, `sourceLabel`, HTTPS `sourceUrl`, `rightsNote` and `expiresAt`.
- `GET /v1/cities/{cityID}/activity-candidates`: city editors see up to 100 pending Activity candidates.
- `POST /v1/activity-candidates/{candidateID}/review`: another reviewer submits `decision` (`publish` or `reject`) and a substantive `note`.

An Activity candidate is private until review. Publishing writes one canonical Activity, source evidence and City Seed maintenance relation in a transaction. `hostLabel` is source-provided information, not a verified Birdtie organization identity. Public Activity status is computed from start/end and cancellation time: `upcoming`, `ongoing`, `past` or `cancelled`; the API offers no participation action for past/cancelled records. No Activity candidate or editor membership is seeded.

Media upload remains disabled. `docs/decisions/0004-media-storage-and-exif-boundary.md` records the provider-neutral storage contract; object storage, scanner and key management have not been chosen. Moment, Journey and Intent tables exist as private/draft-first schema; only owner-only Moment draft operations are open, with no publishing routes.

Successful domain responses use `{ "data": ... }`. Missing published objects return 404. Places are empty until reviewed and sourced City Seed records are added. Source metadata includes maintainer, update time, optional verification/expiry times, and a computed freshness state. API responses never include private EXIF fields or original media storage keys.

The schema and current scope are recorded in `docs/decisions/` at the repository root.
