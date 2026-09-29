# Birdtie Foundation API

Birdtie's Go API reads published City, Place and Activity records from a Birdtie-owned PostgreSQL schema. The initial Aberdeen city is marked `building` and `unverified`; no sample Places, Activities, People or Groups are presented as live data. The Agent task endpoint searches public, current City Graph records with transparent text matching. Session and Profile Consent routes establish the private access boundary. City Seed Place and Activity editorial routes can publish only after separate reviewer approval. A generic OIDC login flow is implemented but remains disabled until an issuer and client registration are configured. Owner-only Moment drafts, Agent task history, Profile editing and owner-published Group/Intent routes are available after login. Media upload is not implemented.

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
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/010_community_review_and_inbox.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/011_dev_phone_auth.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/012_reviewed_public_intents.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/013_owner_published_groups_and_intents.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/014_optional_group_source.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/015_saved_items.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/016_activity_plans.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/017_connections_and_messages.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/018_public_intent_area_markers.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/019_agent_identity_organizations.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/020_agent_task_context.sql
$env:BIRDTIE_DATABASE_URL = 'postgres://birdtie:birdtie_local_only@127.0.0.1:55432/birdtie?sslmode=disable'
go run .
```

The Compose database binds only to local loopback. Its password is for local development, not deployment. Migration `001` is idempotent for the initial schema and seed; `002` adds the coordinate privacy constraint to older local databases; `003` adds Session and Profile Consent; `004` adds City Seed Place editorial tables; `005` adds short-lived OIDC state and one-time Birdtie exchange codes; `006` adds draft-by-default City Graph content tables; `007` adds reviewed City Seed Activity candidates; `008` adds per-City map provider and sourced, display-only viewport. Use an independent credential and managed migration process before deployment.

Migration `009` adds private Agent task history and draft-by-default Communities. No Community or Agent task data is seeded.

Migration `010` adds independent Community review evidence and owner-only Inbox items. It has no seed content.

Migration `011` adds short-lived, local development phone challenges. It stores a phone digest, not a clear-text phone number, and has no seed content.

Migration `012` introduced independent Intent review. Migration `013` supersedes that requirement for Group and public Intent, and keeps old pending submissions hidden; owners can submit them again. Apply `013` before running the current API. These migrations contain no seed content.

Migration `014` allows an owner-created Group without an external source URL. It does not mark owner claims as verified.

Migration `015` adds owner-only Saved references to public Place, Activity and Group objects. Saved does not duplicate the content or grant access to a now-hidden object.

Migration `016` adds owner-only Activity plans. A plan is a private intention to attend, not registration or confirmed participation.

Migration `017` adds explicit human contact requests and one-to-one conversations; it seeds no accounts or messages.

Migration `018` adds an optional broad map-zone code to public Intent. It stores no personal coordinates.

Migration `019` adds Personal and Organization Agents, platform-managed City Contexts, organization principals and role-based memberships. It creates no City Agent. Migration `020` stores principal, human actor, intent, filters and conversation on Agent tasks; organization tasks require an active member actor.

For an end-to-end local Agent demonstration only, apply the synthetic fixture after migrations `019` and `020`:

```powershell
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /dev-seeds/001_badminton.sql
```

It creates a clearly named local development organization, a synthetic sports venue and two synthetic weekend Activities with map points. The fixture is outside `migrations/`, is never loaded by the API, and must not be applied to shared or production databases. It is safe to reapply to the local Compose database.

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

The deterministic MVP recognizes `Find badminton this weekend` and a context-aware `Anything closer?` follow-up. It filters current published Activities by badminton and the selected City-local weekend, then orders a closer follow-up by distance from the configured City view center without requesting device location. Result Activities include public Place name, schedule and point coordinates when available. Unsupported requests receive a capability message instead of fabricated results. The general Agent endpoint still supports bounded literal matching for other queries; this is not semantic matching, recommendation or an LLM.

The City Place list accepts optional `q` (up to 240 UTF-8 bytes) for a literal, case-insensitive name/summary substring search. It searches published Places in the selected published City and returns at most 100 results ordered by name, with the same provenance and location-precision rules as the unfiltered list. It does not use a map provider or location permission. Pagination, category/time filters and a search index remain future work.

Published City responses may include `map: {provider, latitude, longitude, defaultZoom, sourceRef}`. This is a display viewport, not a City centroid or a Place. Aberdeen selects `mapbox`; no other City is seeded. Public Place coordinates remain WGS84, and only `location.precision=point` is eligible for an exact map marker. See ADR 0006 for the Mapbox/AMap boundary.

## Private Agent task history

- `GET /v1/me/agent-tasks`: list up to 50 recent tasks belonging to the active Personal or Organization workspace principal.
- `GET /v1/me/agent-tasks/{taskID}`: restore one task owned by that workspace principal. Another principal's task returns 404.

Signed-in `POST /v1/cities/{cityID}/agent/tasks` creates an `ACTIVE` task before resolving and returns its `taskId`, principal type/ID, acting user, intent, City Context, filters and conversation. Follow-ups send the same task ID and append to its conversation. Tasks end in `COMPLETED` or `FAILED`; Recent restores the stored context and reruns against current publication, expiry and block rules rather than storing result snapshots. An unauthenticated local follow-up is stateless and carries its original query forward. Organization reads and task writes require a current membership role. Apply rate limits and a fuller identity review before external rollout.

## Profile and owner-published People intents

- `PUT /v1/me/profile`: edit the Session owner's `displayName` (2–80 characters), `bio` (up to 500 characters), and `visibility` (`private` or `public`). This is an explicit public Profile choice; anonymous users can read a public Profile. Switching to `private` atomically withdraws the owner's public Intents; editing a still-public Profile does not withdraw them.
- `POST /v1/cities/{cityID}/intents`: publish an owner-confirmed public Intent for an existing published City. Body: `confirmed:true`, `topic`, optional `details`, `availableFrom`, `availableUntil`, IANA `timeZone`, `coarseAreaLabel`, optional `publicMapZone` (`city_centre`, `north`, `south`, `east`, `west`), `expiresAt`. Availability is within 31 days; expiry is at least an hour away and no later than the availability end. The client currently offers 1, 3, 7, 14 or 30 days from submission. A saved public Profile is required. At most three unexpired active Intents are allowed per owner as a temporary limit.
- `GET /v1/me/intents`: list up to 100 of the owner's Intents, including publication state.
- `POST /v1/me/intents/{intentID}/withdraw`: immediately remove an own pending or active Intent from discovery.

The Agent People query reads only active, owner-confirmed public Intents joined to public Profiles. It returns a display name, topic and coarse area, never a precise personal coordinate or Intent details. An optional broad-zone display anchor is derived from the City view, not supplied by a device. Viewer Account blocks filter results. Explicit contact requests and human messages are described below. Local fixed-code accounts are testing identities, not proof of real-world identity. See ADRs 0011, 0014 and 0015.

## Owner-published Groups and Inbox

- `POST /v1/cities/{cityID}/communities`: an authenticated owner publishes a group. Body: `name`, optional `summary`, published `placeId`, `sourceLabel`, HTTPS `sourceUrl`, `rightsNote`, and required `expiresAt` within one year. External source fields and rights note are optional; if a URL is given, it must be HTTPS and have a source label.
- `GET /v1/me/communities`: list up to 100 of the owner's own Groups, including publication status.
- `POST /v1/me/communities/{communityID}/withdraw`: hide an own draft or published Group immediately.
- `GET /v1/me/inbox`: list up to 100 of the signed-in Account's Inbox items.
- `POST /v1/me/inbox/{itemID}/read`: mark one owned item read; another Account's item returns 404.

Group submission publishes directly after owner confirmation. An optional HTTPS URL and rights statement are owner-provided and are not independent verification. Place/Activity review decisions and human contact/message events now produce Inbox items; Agent updates and general notifications have no producers. No real OIDC provider is configured in this repository; fixed-code accounts remain local development identities. See ADRs 0011 and 0014.

## Owner-only Saved

- `GET /v1/me/saved`: list up to 100 own Place/Activity/Group bookmarks. Current publication, expiry and viewer blocks are checked on every read. Unavailable targets return no title, summary or City.
- `POST /v1/me/saved`: idempotently save a visible target with `{"kind":"place|activity|group","targetId":"<UUID>"}`. A hidden or inaccessible target returns 404.
- `DELETE /v1/me/saved/{savedID}`: remove only the caller's bookmark. Another Account's ID returns 404.

All routes require a Birdtie Session. Saving does not contact a person, join a Group or Activity, or expand Agent access. See ADR 0012.

## My Activities: private plans

- `GET /v1/me/activity-plans`: list up to 100 own plans, rechecking current visibility and block rules. Hidden targets return an unavailable placeholder without old title, City or time.
- `POST /v1/me/activity-plans`: idempotently add a currently visible, upcoming or ongoing Activity with `{"activityId":"<UUID>"}`. Past, cancelled, expired or inaccessible targets return 404.
- `DELETE /v1/me/activity-plans/{planID}`: remove only the caller's plan.

These routes do not register a participant, notify a host or change an Activity's attendee count. See ADR 0013.

## Explicit human contact

- `POST /v1/me/connection-requests`: send an explicit request with `recipientAccountId`, `cityId` and a 1–280 byte `note`, only to a currently visible public Intent owner in that City.
- `GET /v1/me/connection-requests`: list up to 100 own incoming and outgoing requests. Hidden or blocked counterpart names and notes are redacted.
- `POST /v1/me/connection-requests/{requestID}/decision`: recipient chooses `accept` or `decline`; sender chooses `withdraw`. An accepted request creates a one-to-one conversation.
- `GET /v1/me/conversations`: list up to 100 current unblocked conversations.
- `GET /v1/me/conversations/{conversationID}/messages`: read the latest 100 messages as a member, oldest first.
- `POST /v1/me/conversations/{conversationID}/messages`: send a 1–2000 byte human message after acceptance. Members, Account status and blocks are rechecked.

Requests expire after seven days. Sender limits are ten live outgoing requests and sixty messages per hour. Inbox request/message events contain no private body. The client offers manual refresh, without push, read receipts or Agent access to messages. See ADR 0014.

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

The protected routes require `Authorization: Bearer <opaque session>`. A production Session requires a verified OIDC callback and a one-time, PKCE-bound Birdtie code exchange. The explicitly enabled local development flow below can also issue a Session, but does not verify phone ownership. Never use a client-supplied Account ID as the actor. Sessions have an eight-hour absolute lifetime and a rolling thirty-minute idle limit.

## Local development phone login

OIDC is a protocol for login through an external identity provider; it is not an SMS code. To develop signed-in Birdtie flows before a provider or SMS service is ready, set `BIRDTIE_DEV_PHONE_AUTH=1` when starting the API after migration `011`. It is off by default. For Flutter web, set `BIRDTIE_ALLOWED_ORIGINS` to the exact loopback client origin, for example `http://localhost:7357`.

```powershell
$env:BIRDTIE_DATABASE_URL = 'postgres://birdtie:birdtie_local_only@127.0.0.1:55432/birdtie?sslmode=disable'
$env:BIRDTIE_ALLOWED_ORIGINS = 'http://localhost:7357'
$env:BIRDTIE_DEV_PHONE_AUTH = '1'
go run .
```

The API and database must both use loopback hosts, and every allowed browser origin must use a loopback host; startup fails otherwise. `GET /v1/auth/dev-phone/status` reports whether the flow is enabled. `POST /v1/auth/dev-phone/code` with `{"phone":"13800138000"}` creates a five-minute challenge but sends no SMS and returns no code. `POST /v1/auth/dev-phone/verify` with `{"phone":"13800138000","code":"123456"}` consumes it and returns an opaque Birdtie Bearer Session. Requests have a 60-second per-phone cooldown and at most five incorrect attempts per challenge. Disabling the flag immediately makes development Sessions unusable.

This fixed code proves nothing about ownership of the supplied number. The resulting identity uses a separate `urn:birdtie:local-dev-phone` namespace and a private Profile; it does not match or import Civu accounts, grant editorial roles, or become a verified phone identity. A future real SMS flow and existing-account binding require their own verification and migration design. Never enable this mode on an externally reachable API or against a production database.

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

Media upload remains disabled. `docs/decisions/0004-media-storage-and-exif-boundary.md` records the provider-neutral storage contract; object storage, scanner and key management have not been chosen. Moment drafts remain owner-only and private, and Journey publishing is not open. Owner-confirmed public Intent and Group publication is available as described above.

Successful domain responses use `{ "data": ... }`. Missing published objects return 404. Places are empty until reviewed and sourced City Seed records are added. Source metadata includes maintainer, update time, optional verification/expiry times, and a computed freshness state. API responses never include private EXIF fields or original media storage keys.

The schema and current scope are recorded in `docs/decisions/` at the repository root.
