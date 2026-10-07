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
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/021_core_activity_participation.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/022_activity_participation_open_guard.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/023_activity_notifications.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/024_inbox_conversation_targets.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/025_organization_faq.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/026_activity_analytics.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/027_reports_and_admin_audit.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/028_organization_membership_lifecycle.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/029_organization_map_locations.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/030_community_social_memberships.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/031_activity_organizers.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/032_activity_social_visibility.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/033_context_graph.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/034_person_ties.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/035_conversation_read_state.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/036_social_intents.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /migrations/037_social_intent_audiences.sql
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

Migration `030` extends the existing Community entity with join policy, lifecycle, optional City and constrained CommunityMembership. It backfills an active Owner for every prior Community, and the legacy publish path creates its Owner in the same transaction. `030_community_social_memberships.down.sql` supports rollback before new social data is written; it refuses a rollback that would discard such data.

Migration `033` adds typed City/Country/Institution/Community/Online Context nodes and private Person associations. Existing Agent Tasks keep their city and principal IDs while receiving a typed City Context; old city-based writes still work. Non-city Agent Tasks can be stored without a fake city, but public Agent HTTP routes and legacy Intents remain city-based until later V4 tasks. Run `pwsh -NoProfile -File automation/verify_context_graph_migration.ps1` from the repository root to test 033 in a disposable database, including its guarded down/reapply path. Applying 033 to the local development database is separate from that test.

Migration `036` creates a separate owner-only V4 social Intent draft table with no required City. It preserves every legacy Intent and Agent Task. `POST /v1/me/social-intents` creates a draft; `GET /v1/me/social-intents` and `GET /v1/me/social-intents/{id}` read only the signed-in Person's drafts. Recorded audience and modality do not make drafts discoverable. Run `pwsh -NoProfile -File automation/verify_social_intents_migration.ps1` from the repository root for a disposable legacy-data, API and guarded rollback check. Production migration requires a separate review.

The existing `033` Context tables now have owner-only `GET/POST /v1/me/contexts` and `DELETE /v1/me/contexts/{contextID}/{relation}`. POST accepts `{ "contextType": "CITY|INSTITUTION|ONLINE", "sourceKey": "...", "relation": "current|past|destination|home|affiliation|interest" }` with a type-specific relation. CITY keys must name a published City; Institution and Online keys are private, self-declared labels, not verified memberships. A new current City replaces the old current City for that Person. No public context-declaration endpoint exists. The client's Chinese settings page lets a Person manage current/past/destination City, past school, and online interest. No schema migration beyond `033` is needed. `automation/verify_place_context_migration.ps1 -Through 46` exercises this API against a disposable database with two distinct people and multiple contexts.

Migration `037` adds relational LOCAL City, COMMUNITY and INVITE_ONLY targets. `GET /v1/social-intents` and detail use one database visibility policy: only active, unexpired records permitted by public Profile, Tie, Community membership, explicitly selected current City Context or invitation; a two-way Block always suppresses a result. The draft API cannot activate an Intent. Run `pwsh -NoProfile -File automation/verify_social_intents_migration.ps1 -Through 37` on a disposable database for the audience authorization matrix and guarded rollback. Existing targeted drafts without explicit targets make migration fail for manual review.

An owner can explicitly activate an unexpired draft with `POST /v1/me/social-intents/{id}/activate` and `{"confirmed":true}`; the server rechecks current profile, Community, City, invitee and Place conditions before making it visible. `POST .../cancel` removes an own live Intent from discovery and records an audit event. Owner reads project elapsed `DRAFT/ACTIVE/MATCHED` records as `EXPIRED` at read time, while public reads exclude them. Matching and conversion remain reserved for verified future workflows; the client cannot claim either outcome. The current Flutter screen creates private drafts and supports cancellation; it does not expose public publishing controls.

Migration `031` adds exactly one constrained Activity organizer, backfills Organization and Person hosts, and keeps legacy Activity writers compatible. The migration aborts if any existing Activity cannot be mapped. Its down migration refuses to discard a Community Activity organizer.

Migration `032` adds organizer-members and invite-only Activity visibility plus explicit invitations, while retaining old legacy visibility values for compatible reads. It rejects Person members-only Activities at transaction commit.

Migration `019` adds Personal and Organization Agents, platform-managed City Contexts, organization principals and role-based memberships. It creates no City Agent. Migration `020` stores principal, human actor, intent, filters and conversation on Agent tasks; organization tasks require an active member actor.

Migration `021` adds organization publishing metadata, optional Organization ownership and publishing fields to Activity, and a separate persisted RSVP table. `activity_plans` stays a private intent and is not a participation. A single person/Activity pair has one participation row even after cancellation. Activity completion is derived from its end time; cancellation is explicit through `cancelled_at`. The migration is additive and transactional, so existing City Seed Activities remain valid. For an empty development migration, `021_core_activity_participation.down.sql` reverses it; the down script refuses to discard any data entered in the new fields or table. `testdata/core_schema_021_verify.sql` checks the constraints inside a rolled-back transaction.

Migration `022` prevents new or reactivated Participation on a draft, cancelled, private, or completed Activity at the database boundary. `testdata/activity_publish_guard_verify.sql` checks a valid join and cancelled-Activity rejection in a rolled-back transaction.

Migration `023` adds activity-targeted Inbox notifications. Published Activity time and Place changes notify `going`/`pending` participants in the same transaction as the edit; cancellation does the same. The API queues a starts-soon reminder at startup and every five minutes for publicly visible Activities starting within two hours. Its Inbox uniqueness key prevents duplicates on retry or restart. The reminder query locks Activity and Participation rows against concurrent edit/cancel so no old reminder can appear after the edit transaction. A time change removes the old reminder so the revised schedule can be reminded, and cancellation removes an obsolete reminder. `go run ./cmd/activity-reminders` runs the same reminder query once for an independent scheduler or local check. It exits nonzero and logs `activity_reminder_run status=failed` on failure; success logs count and duration. Schedule the built worker at least every five minutes, collect its exit status and logs, and alert a named operator on failure or missing runs. This deployment and alert configuration is a Closed Pilot prerequisite, not established by the repository. Notifications point to the Activity ID; the detail API remains readable after cancellation and returns `status=cancelled`. The loopback-only `automation/verify_activity_notifications.ps1` checks the full flow with synthetic accounts and cleans its fixture unless `-KeepTestData` is specified; its `-OfflineGatePath` mode checks delivery while the API process is stopped.

Migration `024` adds an explicit conversation target to Inbox items and backfills existing message notifications. New human messages populate it in the same transaction. The Flutter Inbox uses this target to open the actual conversation after marking the item read. `automation/verify_inbox_conversation.ps1` checks a synthetic local message, its recipient, target and read state, then cleans the fixture unless `-KeepTestData` is specified.

Migration `029` adds an organization-owned, explicit WGS84 point with `precision=point`, public/hidden intent, independent city-review status, revision and audit. `PUT /v1/me/organizations/{organizationID}/map-location` accepts `{cityId,latitude,longitude}` from an active owner/admin; edits reset review. `DELETE` hides immediately. `POST .../review` accepts `{decision:"approve"|"reject",note}` from an active city reviewer other than the submitter. `GET /v1/cities/{cityID}/organizations/map` returns only stable organization ID, name and coordinates for approved public points on active, public, verified organizations. It never derives an organization point from a member, account address or event venue. The private `GET .../map-location` is limited to owner/admin. No synthetic point or manual local verification flag is a real-world authorization.

`GET /v1/organizations/{organizationID}` is an anonymous public read of an active, public Organization's name, type, description, stored verification status, organization-provided HTTPS links and at most 20 upcoming public Activities. Private or inactive Organizations return 404. The Activity read includes `organizationId` only when its Organization is itself public and active, so the Flutter Activity detail can offer a working Organization profile link. A stored `unverified` or `pending` state must never be presented as verified. No public Follow or Ask action is implemented. `automation/verify_public_organization.ps1` checks the public projection and private visibility against the synthetic local Organization, then restores its original fields.

For an end-to-end local Functional MVP demonstration only, apply both synthetic fixtures after migration `027`:

```powershell
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /dev-seeds/001_badminton.sql
docker compose exec db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -f /dev-seeds/002_functional_mvp.sql
```

Together these create Aberdeen CityContext, three fictional Places, two unverified fictional Organizations, five public synthetic Activities, two private test People with Personal Agents, and three OrganizationMembership rows. One Organization uses an explicit `Aberdeen CSSA（虚构本地测试，非官方）` label solely to exercise the product path; it is not an official CSSA profile or partnership. The fixed development login identifiers are `+999000000071` (CSSA fixture owner) and `+999000000072` (student and badminton fixture owner), with the local-only development code documented below. The fixtures are outside `migrations/`, are never loaded by the API automatically, can be reapplied, and must never be used in shared or production data.

`BIRDTIE_DATABASE_URL` is required. `BIRDTIE_API_ADDR` defaults to `127.0.0.1:8080`. `BIRDTIE_ALLOWED_ORIGINS` is an optional comma-separated allowlist for browser clients; no cross-origin access is enabled by default.

## Public read routes

- `GET /healthz`
- `GET /readyz`
- `GET /v1/cities`
- `GET /v1/cities/{cityID}`
- `GET /v1/cities/{cityID}/places`
- `GET /v1/places/{placeID}`
- `GET /v1/cities/{cityID}/activities`
- `GET /v1/cities/{cityID}/pulse?bounds=west,south,east,north&from=RFC3339&to=RFC3339`
- `GET /v1/activities/{activityID}`

The city Activity list accepts optional `bounds=west,south,east,north` (WGS84), `from` and `to` (RFC3339 timestamps), and `category` (category code). Supplying any of these selects the public discovery query. Results are limited to 100 published, public, unexpired, uncancelled Activities that have not ended; requested time intervals overlap the Activity schedule (`endsAt > from`, `startsAt < to`). Bounded results require a published Place with precise WGS84 coordinates. Results are sorted by start time and stable Activity ID. A successful empty search returns `{"data":[]}`; malformed filters return HTTP 400 with a specific error code. Without filters, the existing city Activity list behavior remains available.

The Area Pulse requires valid WGS84 `bounds` and accepts optional RFC3339 `from`/`to`. It returns `{cityId,bounds,from?,to?,status,total,categories,activities,truncated}`. `total` counts all eligible Activities in the selected viewport and time interval; up to two leading category codes and their exact counts are derived from the same database predicate as Activity discovery. The first 100 Activities are included with stable IDs for Now map pins; `truncated` indicates that the exact count exceeds this list. An empty area returns `status:"empty", total:0, categories:[], activities:[]`; invalid or missing bounds return HTTP 400. The API supplies data only: camera movement never triggers a request, and the Now client remains responsible for the explicit area action and Chinese labels.
- `GET /v1/accounts/{accountID}/profile` (only explicitly public Profiles for anonymous callers)
- `POST /v1/cities/{cityID}/agent/tasks` with JSON `{"query":"Find badminton this weekend"}` (anonymous or Bearer; query up to 240 UTF-8 bytes)

The deterministic MVP recognizes the published badminton Activity flow and a context-aware `Anything closer?` follow-up. It filters current published Activities by badminton and the selected City-local weekend, then orders a closer follow-up by distance from the configured City view center without requesting device location. An explicit `mapBounds` object on a task filters public Activity point Places to those visible bounds; camera movement alone never starts a query. Result Activities include public Place name, schedule and point coordinates when available. Unsupported requests receive a capability message instead of fabricated results. This is not semantic matching, recommendation or an LLM.

The City Place list accepts optional `q` (up to 240 UTF-8 bytes) for a literal, case-insensitive name/summary substring search. It searches published Places in the selected published City and returns at most 100 results ordered by name, with the same provenance and location-precision rules as the unfiltered list. It does not use a map provider or location permission. Pagination, category/time filters and a search index remain future work.

Published City responses may include `map: {provider, latitude, longitude, defaultZoom, sourceRef}`. This is a display viewport, not a City centroid or a Place. Aberdeen selects `mapbox`; no other City is seeded. Public Place coordinates remain WGS84, and only `location.precision=point` is eligible for an exact map marker. See ADR 0006 for the Mapbox/AMap boundary.

## Organization membership and profile administration

- `POST /v1/me/organizations` creates an Organization principal and Agent, and persists the creating person as its `owner` member.
- `GET /v1/me/organizations` lists active Organizations in which the signed-in person has an active membership, including their role.
- `PUT /v1/me/organizations/{organizationID}/profile` updates `name`, `description` and `officialLinks` (HTTPS only). The signed-in person must hold an active `owner` or `admin` membership in an active Organization; the database update enforces this condition. Other members and nonmembers receive 403, and anonymous requests receive 401. Hiding controls in the client is never the authorization boundary.

This endpoint does not invite members or change roles. The Organization Console is a separate queue task.

## Organization Activity publishing

- `POST /v1/me/organizations/{organizationID}/activities`: create a draft from `cityId`, optional `placeId`, `title`, `summary`, `description`, `startsAt`, `endsAt`, `timeZone`, optional `categoryCode`, `capacity`, `priceMinor`, `currency`, `eligibility`, `languageCode`, and `visibility`.
- `GET /v1/me/organizations/{organizationID}/activities`: list up to 100 recent managed Activities.
- `PUT /v1/me/organizations/{organizationID}/activities/{activityID}`: replace editable fields. The City remains fixed. A published Activity may be edited only before its start and must remain public.
- `POST /v1/me/organizations/{organizationID}/activities/{activityID}/publish`: publish a future draft whose visibility is `public`.
- `POST /v1/me/organizations/{organizationID}/activities/{activityID}/cancel`: cancel a published Activity before it ends. Repeated publish/cancel and invalid state transitions return 409.

Every route requires an authenticated person with an active `owner` or `admin` membership. Each database mutation repeats the membership check. Drafts are absent from public discovery; published public Activities appear through the existing City and Agent searches. Cancelling preserves the public detail with `status: cancelled`, while the Agent search excludes it. A database trigger blocks new Participation after cancellation. Input errors return a specific 400 code such as `title_length_invalid` or `timeZone_city_mismatch`. Organization Console UI, RSVP API, and capacity enforcement follow in separate tasks.

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

An authenticated Activity read filters records hosted by an Account involved in a block with the viewer. Anonymous public reads cannot apply a viewer-specific block. The Flutter shell sends its validated Bearer Session on Activity reads after login and refreshes that list when login state changes. Place and external-host City Seed Activity records are not person-owned social content.

## Private Moment drafts

- `POST /v1/me/moments`: create a private draft for a published City, with an optional published Place in that City. Required fields: `cityId`, `title`, `body`, `timePrecision`, `locationPrecision`; `placeId` is empty or a Place UUID, and `occurredAt` is optional only when time precision is `unknown`.
- `GET /v1/me/moments`: list up to 100 of the caller's active drafts, newest first.
- `GET /v1/me/moments/{momentID}`: read the caller's own Moment.
- `PUT /v1/me/moments/{momentID}`: update an own draft with the full input and its current `revision`; a stale revision returns 409.
- `DELETE /v1/me/moments/{momentID}?revision=N`: withdraw an own Moment using its current revision; withdrawn records leave the active draft list.

The actor always comes from the Bearer Session. Creates, updates and withdrawals are audited. Drafts remain private; there is no public Moment read or publish route. The Flutter client currently creates and edits only city-level, unknown-time, text-only drafts.

## Generic OIDC login

Configure a registered OIDC client through `BIRDTIE_OIDC_ISSUER`, `BIRDTIE_OIDC_CLIENT_ID`, optional `BIRDTIE_OIDC_CLIENT_SECRET`, `BIRDTIE_OIDC_REDIRECT_URI` (the API's `/v1/auth/oidc/callback` URL), and `BIRDTIE_OIDC_CLIENT_REDIRECT` (a fixed client callback URL). Configure `BIRDTIE_ALLOWED_ORIGINS` for a separate browser client origin. If OIDC is unconfigured, auth routes return `oidc_not_configured`; partial configuration fails startup. External URLs and provider endpoints must use HTTPS; HTTP is accepted only for loopback API/client callbacks.

`GET /v1/auth/oidc/status` returns `{"data":{"configured":true|false}}` without disclosing provider settings. Flutter web and Android/iOS use it to enable Sign in only when the API has a provider configured. For mobile, set the fixed client redirect to `birdtie-auth://callback`; this exact custom scheme is the only non-HTTPS client redirect accepted. The provider's own callback remains an HTTPS API URL.

1. The client generates and temporarily retains a PKCE verifier, then navigates to `GET /v1/auth/oidc/start?challenge=<S256 challenge>`.
2. Birdtie stores one-time state, nonce and a separate provider PKCE verifier, then redirects to the configured OIDC issuer.
3. `GET /v1/auth/oidc/callback` exchanges the provider authorization code and verifies the signed ID Token, audience and nonce. A newly verified issuer/subject receives a private Birdtie Account/Profile; an existing Account is reused. Birdtie redirects only to the configured client URL with a one-minute Birdtie exchange code and the client challenge.
4. The client checks that challenge against its pending login and calls `POST /v1/auth/oidc/exchange` with JSON `{"code":"<one-time code>","verifier":"<original client verifier>"}`. The response contains the opaque Bearer Session. The web client removes the code from browser history; the client keeps the Bearer credential in platform secure storage, not a URL or browser local storage.

The Flutter web client implements the fixed callback; Android/iOS implement the `birdtie-auth://callback` handoff and keep the short-lived PKCE verifier in platform secure storage. Desktop callbacks remain unimplemented. The server does not retain provider access or refresh tokens. Birdtie Sessions expire after eight hours; valid requests extend a thirty-minute idle window, and logout revokes the session. No issuer, client ID, client secret or callback URL is configured in this repository, so a real OIDC login has not been exercised. Before external use, complete provider registration, rate limits, deployment HTTPS, secret storage and end-to-end identity review.

## Organization membership lifecycle

Migration `028_organization_membership_lifecycle.sql` adds member action details to `admin_audit_events` and permits more than one active owner so ownership can transfer safely. A personal account with an active owner/admin membership can read `GET /v1/me/organizations/{organizationID}/members` and invite an existing individual by account ID with `POST` to the same path (`userAccountId`, `role`). The recipient reads `GET /v1/me/organization-invitations` and accepts its own invitation through `POST /v1/me/organization-invitations/{membershipID}/accept`. An authorized manager changes a role through `PUT /v1/me/organizations/{organizationID}/members/{membershipID}/role` or removes the member with `DELETE /v1/me/organizations/{organizationID}/members/{membershipID}`. Admins can manage members and moderators; owners can also promote admins and add a second owner. The final active owner cannot leave or be demoted. All writes log target account and old/new role/status in the same database transaction. The recipient receives a private in-app invitation; no email or SMS invitation is sent. Synthetic development accounts do not prove production membership or organization authorization.

## City Seed Place and Activity editorial routes

`GET /v1/me/opportunities` returns the signed-in Person's rule-based Activity candidates from active first-class social Intents and currently visible Activity/Place records. Each candidate includes stable Intent/Activity/Place references, machine-readable reasons, a Chinese explanation and an `OPEN_ACTIVITY` action. It never publishes an Intent, reserves a seat or creates a social connection. Online/hybrid Intents return no candidate until verified compatible supply exists. See `docs/architecture/OPPORTUNITY-ENGINE-V1.md`.

Migration `039_place_context_address.sql` preserves existing Place IDs and adds an optional sourced `addressLabel` to Place candidates and approved Place responses. A candidate address requires point-level coordinates and independent review. `GET /v1/places/{placeID}/activities` returns current activities through the existing Activity visibility and Block checks; hidden or expired Places return 404. Private Moment links are never included in public Place responses. `automation/verify_place_context_migration.ps1` tests this on a disposable database only.

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

## Request diagnostics

Every API response includes `X-Request-ID`. A client may send a short, safe ID in that header; otherwise the API generates one. Server logs record the same ID with method, status and latency, so a failed client request can be matched to its server entry. Logs omit request bodies, URL query strings and credentials. Browser clients may read the header through CORS.

The schema and current scope are recorded in `docs/decisions/` at the repository root.

## Explicit friends and block lifecycle

After migration `034_person_ties.sql`, `POST /v1/me/connection-requests` accepts `scope: "friend"` with `recipientAccountId` and `note` but no `cityId`. The old omitted scope or `scope: "conversation"` keeps its city-bound, one-off chat request behavior. Only an accepted friend request creates a persistent bilateral Tie; it does not create a conversation. `GET /v1/me/ties` lists the authenticated Person's visible active Ties, and `DELETE /v1/me/ties/{tieID}` removes one. Either member may remove a Tie. A new request and acceptance are required to reconnect.

Existing `POST /v1/me/blocks` and `DELETE /v1/me/blocks/{accountID}` remain the explicit block/unblock controls. Blocking ends the Tie and both directions of pending contact requests, revokes Profile grants, and prevents requests, profile/discovery reads and messages while blocked. Unblocking does not restore the Tie or grants. An old accepted chat can be used again after explicit unblock; this behavior is recorded in ADR 0014. Writes create audit events. The 034 verification script uses a disposable local database; it is not a production migration or pilot acceptance.

Migration `035_conversation_read_state.sql` adds one private read cursor per conversation member. `GET /v1/me/conversations` includes only the caller's `unreadCount` and optional `lastReadAt`. After actually showing messages, the client calls `POST /v1/me/conversations/{conversationID}/read` with `throughMessageId`; the server verifies membership and message ownership. Historical messages have unknown read status and are not counted as new unread items. `automation/verify_conversation_read_migration.ps1` checks this on a disposable database; it does not deploy the migration.

An active friend explicitly starts or reopens their 1:1 chat through `POST /v1/me/ties/{tieID}/conversation`. This reuses an existing accepted conversation for the pair where possible. It does not send a message. `POST /v1/me/conversations/{conversationID}/messages` persists a human message; the recipient reloads the existing messages API. Friend-sourced chats require an active Tie on every list, read, send and read-state operation. Removing or blocking the Tie hides that chat and denies further messages; an older conversation consent request remains independent. The development API and Flutter integration tests verify persisted history after a new database connection and honest error handling.


## 2026-10-06 AIR036：受限只读规划（仓库与隔离原生验证）

`agentplanner` 定义闭集 `air.readonly_plan.v1` / `air.action_proposal.v1`；现有 `Store.PrepareOwnReadonlyPlan`、`modelegressbudget.LocalPlannerRunner` 与 `MemoryCandidateService.PlanOwnCandidateReview` 是实际原生接缝，不是新 HTTP 或生产 provider 入口。默认 main/HTTP/Gateway 仍不调用模型，正式 IdP/provider、自动写、Vision/A2A 未激活。

规划最多3个提案、2次模型调用（含原格式修复/规范暂时错误）、30秒；可以收紧，不能放宽。未知目标/城市/原查询/比较对象/地图范围给1–2个中文澄清，0模型调用。P0仅 `activity.search`、真实当前公开 `activity.detail` 与本人已存在 `memory_candidate.review`；提案没有权限、确认或执行字段，没有代码/工具执行器，不报名、不接受候选、不写 Memory。

活动路径复用实际 Task、原058批准、062四个预算、088 ModelRun、066原 ticket，以及 AIR028 同事务领域来源核验。当前 query-only provider 消息/空工具列表不扩大；领域结果只用于服务端输出验证。候选审阅通过原本人身份、Session、metadata、source version/ACL 与 Memory开关读取真实候选，直接保留原状态，不调用自动推断或 lifecycle refresh。`resource_version` 是不透明版本摘要；JSON不能重建原生 prepared goal、handle、批准或回执。

独占材料：`work/bounded-planner-2026-10-06`、`docs/testing/evidence/bounded-planner-2026-10-06`。首次3条实际原生 RED、后续 Task ABA/时钟误拒绝及夹具诊断保留。命令、CWD、退出码、来源 SHA 和原始事件以对应 result/manifest 为准；主代理完整回归与发布状态另记。Flutter/真机规划 UI、正式模型、生产和试点不是本任务证据，Closed Pilot / Consumer Beta 仍为 NO。


## 2026-10-06 AIR037：本地原生工具注册与确定性许可

`internal/agenttool` 的闭集注册表包含 `activity.search`、`activity.detail` 和唯一写描述 `sandbox.write`。元数据包含版本、输入/输出 schema、读写类别、资源范围、风险、必要权限、用途、幂等与对账边界。未知工具（包括消息发送、资料更新、shell 和 HTTP）拒绝。注册不表示获得许可。

实际接线为原 `LocalPlannerRunner.Run` → `LocalPlannerOutcome.ToolPlan()` → `agenttool.NewService(...).Check/Read`。仅成功原生规划保留非 JSON 的进程内句柄；展示提案、`Decision`、旧 Control 或反序列化结果都不能重建许可。读操作复用当前原 ModelRun/Task/Session、结果投影、活动公开 ACL 和 065 策略，在最终原事务收口重新检查。最多3次读取，复用原30秒 elapsed bound；不追加模型调用或费用预算。真实空结果返回非 nil 空数组，隐藏或撤权不是空成功。

`Store.CheckOwnSandboxTool` 只为本人当前原生目标、具体参数摘要/来源/策略版本生成 `CONFIRM`，不是批准、可执行句柄或效果回执。默认 Observe、错误主体、未知来源、过期或关闭时拒绝。原 041 未获准 resolver 不伪造；044 仅限制等级，不能给模型授权。持久批准、dispatch、效果、UNKNOWN 和重启恢复仍属 AIR040。本轮不启主 HTTP、外部模型或真实写。

本轮独占原生 RED、真实 Activity ID/公开 ACL/真实空/ABA/撤权/迟到/锁等待/策略期限、行+xmin/catalog 保留和冻结证据见 `docs/testing/evidence/deterministic-tool-permission-2026-10-06/`。合成适配器与隔离本地数据库证据不算生产模型或试点验收；全 Go 由根代理独立核证。Closed Pilot / Consumer Beta 仍 NO。


## 2026-10-06 AIR040：本人沙箱批准、单次提交与结果核查

原 AIR037 `CheckOwnSandboxTool` 继续只生成 `CONFIRM`，不是批准或执行。新 `agentaction.Service` 通过原 `postgres.Store` 的 `PreviewOwnSandboxAction` → 本人显式 `ApproveOwnSandboxAction` → `CommitOwnSandboxAction` 接入真实原生领域。仅闭集 `sandbox.write.v1` 写入隔离的 `agent_sandbox_writes`，不调用消息、报名、资料、预订、承诺或外部服务。主 HTTP、模型工具入口、provider、客户端未接入；默认 Controller OFF 保持。普通人类 API、原候选/预算/Run/旧037读取边界不改。

097 仅新增三个最小表：不可变审阅绑定、唯一 dispatch/effect 状态账本、真实私有沙箱数据。独立 `OWN_SANDBOX_ACTION` / `sandbox_write` 用途复用原 `consent_grants`；它仍是唯一批准生命周期，不借用模型出口、Task context 或记忆授权。原 064/082/091 `MEMORY_CANDIDATE` 效果账本及守卫不扩权。

绑定包含实际 tenant/actor/subject/agent/Session/Task、logical operation/action、工具版本、目标、完整参数摘要、源 token+xmin、原身份/065策略指纹、独立 consent 版本和期限。PERSON 本人范围的 membership 为明确不适用，不能推导组织权限。批准期限不延长原生目标的30秒上限；过期需新审阅与明确确认。Session/身份/Task/policy/approval/grant 原行锁、原策略 absence 锁和最终数据库时钟将批准消费、dispatch commit 与撤权排序。参数、目标或主体变化不能复用批准。

批准消费只说明 `DISPATCH_COMMITTED`，不表示成功。只有新提交且事务确认返回的非 JSON 进程内 Commitment 能一次 Begin；Claim 绑定同实际 Store/Access/Controller ticket 和持久 fence。恢复进程不能从 JSON 重新构造发送能力。实际效果先在单独沙箱事务写入，最终真实时钟/fence/lease重新检查；收到响应不明或实际写入后的进程退出不会生成第二个发送能力。

`MarkOwnSandboxUnknown` 明确持久 `UNKNOWN_OUTCOME`。`ReconcileOwnSandboxDispatch` 在锁住原 dispatch 后读取真实沙箱效果：有实际 row 才为 `SUCCEEDED`；只有同一原生封闭沙箱内、排他锁与递增 fence 永久封闭旧 Claim 且不存在实际 row 时，才能确定 `NO_EFFECT`。不存在 ID、读取失败、404、批准已消费和 lease 到期都不等于成功或失败；不支持外部工具以 absence 推导结果。已提交动作先于撤权时仍可能在飞，后续只核查实际结果，不谎称撤回。源编辑/撤权后原已发生回执不重写；禁止追加步骤或自动重发。同一 logical operation/action/effect kind 仅一真实 row；两次刻意相同新操作保留各自效果。内容摘要/handler版本不作效果键。

审计复用原 `audit_events` 与 request correlation，只记录实际 actor、闭集决策/状态、用途和原生 ID；不存沙箱正文、Session token/digest、权限指纹、私聊或隐藏思维链。使用后的097 down拒绝丢弃历史；空库 down/reapply 与原001–096数据/xmin/catalog保持分别验证。

适用 UX-CHECK-06/07/08/09/10/11/16。实际原生数据库正负/并发/撤权双序、最后时钟、真实子进程崩溃/新进程恢复、未知提交、SQL篡改和旧回归证据在 `work/outbound-action-safety-2026-10-06` 与 `docs/testing/evidence/outbound-action-safety-2026-10-06`。纯 canonical/schema 测试、原生合成身份/隔离库实测和根代理全量验收分别记录。未运行 Flutter/沙箱UI/真机/辅助技术、正式 IdP/provider、真实用户或生产发布；Closed Pilot / Consumer Beta 仍 NO。此增量不取代原任何发布门槛。


AIR040最终边界补验：Approve/Commit在比较私人摘要或参数前，先通过原真实Session/Task/owner检查；peer、匿名、失效Session和未知审批ID统一拒绝，避免透露猜测是否命中私人内容。当前本人编辑具体版本仍返回ACTION_CHANGED并要求新确认。初次原生错误分类RED在隔离副本记录，原freeze11与后续修复版分开封存。


## AGE042 消息请求策略（2026-10-06）

本人 GET/PUT `/v1/me/message-request-policy`、GET `/v1/me/message-request-policy/decisions/{personID}` 及原请求/聊天 CurrentStore 接线见 [canonical 契约](../../docs/architecture/AGENT-MESSAGE-REQUEST-POLICY-V5.md)。098仅新增偏好和原Request注释，不启用Agent筛查、自动接受、模型或消息执行。SCREEN待人工审阅；ALLOW只当前accepted Tie；旧已同意专用Conversation继续原ACL。当前相关单元测试可复现；098实际迁移/原生并发/持久化重启/新注册HTTP GREEN/生产身份/真机/整仓检查本轮 NOT_RUN，不能视为发布证据。


## 2026-10-06 AIR019 本人定时汇总（相关单元阶段）

新增本人 GET/PUT `/v1/me/notification-schedule` 和独立 `cmd/notification-schedules`，复用原notification决定、当前来源/偏好和typed Inbox，不改原001–098、活动提醒main/CLI或默认模型关闭。用户明确IANA时区、当地时间、DST缺口SKIP/重复EARLIER_ONCE、静默窗口、类别和滚动24小时实际Inbox触达上限；无配置不建计划，off/会话失效不投递，跨版本/时区改动不清零receipt预算。槽只服务器元数据，不能作为UserQuery、推理或工具批准。

新099只有设置、日槽与交付关联；源当前检查、Session、版本/xmin、最后PG时钟及未知提交去重分别保留。CLI先独立调用原活动提醒，汇总依赖nil/typednil/失败不停止提醒；错误阶段数量UNKNOWN。正常/立即分支仍只有原061谓词，DIGEST只确有原生交付关联才可见。具体契约见 [本人通知计划](../../docs/architecture/AGENT-NOTIFICATION-SCHEDULES-V5.md)。

本阶段只相关单元和静态SQL契约，不能替代原生事实。099迁移/真实PG锁与并发/持久化/重启/原生HTTP、客户端设置/可用性/真机、部署推送/真实IdP和试点均NOT_RUN，建议PARTIAL，Closed Pilot/Consumer Beta仍NO。适用UX-CHECK-05/06/07/08/09/10/11/12/16仅后端范围，原历史验收不覆盖新099。精确证据在 `docs/testing/evidence/notification-schedules-2026-10-06`，状态由根代理更新唯一live队列。

单次执行（由外部调度器调用；本轮未部署或执行真实数据库）：

```text
go run ./cmd/notification-schedules -owners 100 -deadline 15s
```

必须显式配置 `BIRDTIE_DATABASE_URL`，不使用PG环境回退；输出不包含DSN、token、本人ID或内容。原 `cmd/activity-reminders` 独立保留。新计划需明确完整JSON：expectedVersion、enabled、timeZone、localMinute、gapPolicy=SKIP、foldPolicy=EARLIER_ONCE、quiet=null或明确起止分钟、maxContactsPerDay（响应budgetWindowHours=24）、categories和expiresAt。正式通知/消费可用性不能由此示例或单元绿灯推断。


### AIR024 当前字段证据（2026-10-06）

原本人 Memory Detail GET 的 `data.fieldEvidenceSet` 是同一当前记录的只读声明/来源/时间元数据，不是 Memory 写批准。原 ContextBuilder 本人审阅与 purpose-approved 本地任务读取提供相同证据，relevance/Adapter按保留字段重建并计入输入字节限额，不能带出被剔除值/ID。旧公开 rules 查询保持原数量与64KiB上限。参见 [字段证据契约](../../docs/architecture/AGENT-FIELD-EVIDENCE-V5.md)。本轮仅相关单位验证；模型/媒体/自动写关闭，native数据库、照片提取/获准分析、消费端确认与真机 NOT_RUN，整体 PARTIAL。


## AIR039 当前只读检索与匹配（2026-10-06）

原 Task POST/GET 的 FindPlace/FindPerson 使用新增 current native read port；本人 GET /v1/me/new-people/candidates 保留原明确来源/双向 opt-in 的规则匹配，经独立 current match port 在编码后重验。继续原 typed ResultSet 与 RULE_BASED NewPeople DTO，不建新搜索服务或通用工具HTTP。普通活动查询维持原邀请/公开领域ACL；原 ModelRun activity.search/detail 仍 PUBLIC、ACTIVITY schema，模型/出网/自动写未激活。

current actor/Session、原 Task/source、Personal Agent及065政策指纹/期限在同一原生事务收口；原 entityaction 只提案且不超读取期限。新 human helper 与原策略 writer 共用 human-agent-policy advisory，随后 settings SHARE。未知或变更不返回数据、不给模型/邀请权限，不回退旧读或伪报空。native SQL/PG并发与表锁性能未实测。

契约见 [当前只读工具](../../docs/architecture/CURRENT-READONLY-TOOLS-V5.md)。精确范围/原始失败/最终相关单元命令与SHA在 work/current-readonly-tools-2026-10-06 及 docs/testing/evidence/current-readonly-tools-2026-10-06。仅90新单位＋238旧相关单位通过；注册HTTP与Tx spies使用合成来源，不是原生数据库或产品验收。数据库/迁移/真实供给/完整客户端/手机/整仓/vet/build/部署与正式provider本轮 NOT_RUN，整体建议 PARTIAL，Closed Pilot/Consumer Beta仍NO。


## AIR024 本人多来源只读审阅（2026-10-07）

新增本人 `POST /v1/me/agent-context/self-review`，JSON 仅三个必需 selector 数组：`profileFields`（最多3个现有字段）、`memoryIds`（最多3个本人显式 Memory ID）、`policyFamilies`（最多1个 ATTENTION/SOCIAL/AUTONOMY），至少选一项。沿原 bearer Session 与本人 Personal Agent，不接受 owner/Task/grant/deadline；组织工作区、未知/重复/null键、查询串和超8192字节正文拒绝。

同一个原 Service.Build/最终 RevalidateOwn 保护完整<=64KiB `data` DTO：所选可见值/摘要/原版本/已知时间、FieldEvidenceSet 与明确范围中文说明；不返回 raw context、grant、隐藏 StructuredValue、MemoryKey、xmin/session/authority。相反明确活动偏好保留双方待确认，不自动更新/授予模型或动作。短读取有效期不是来源期限或批准，UNCONFIGURED 不是已开权限；无列出冲突也不是全体无冲突。

相关注册HTTP/DTO单位55+12以及已执行原精准冲突控制1去重68父子事件通过；仅合成Store/transport，非原生HTTP/数据库证明。初始404 RED及新fixture时间精度/canary键错误原始失败保留。原生PG/持久化/并发/真实身份与媒体、消费端多源审阅、手机/辅助技术、whole/vet/build/发布本轮 NOT_RUN，整个 AIR024仍PARTIAL，Closed Pilot/Consumer Beta NO。契约见 [上下文构建器](../../docs/architecture/AGENT-CONTEXT-BUILDER-V5.md) 和 [字段证据](../../docs/architecture/AGENT-FIELD-EVIDENCE-V5.md)，精确源码/raw/两最终相关命令见 `docs/testing/evidence/human-self-review-field-evidence-2026-10-07`。

### AIR020：有限 Memory 失效链（2026-10-07，单元验证）

新增 101 迁移中的同事务 `agent_memories` metadata-only 捕获：本人 EXPLICIT 记忆的提交版本或删除产生固定 `MEMORY_UPDATED` 根；现有一次性控制入口接受 `--local-development-only --subject <本人 UUID> --handler memory-invalidation-v1`。本轮没有执行该命令、迁移或实际数据库；默认主进程、模型/视觉/自动记忆写入/外部服务仍无新增入口。

实际 native Store 路径先删除绑定旧 Memory revision/xmin 的未批准 ContextPurpose preview，保留已绑定历史，再在同事务生成唯一 `MEMORY_CONTEXT_INVALIDATION` 子事件；子事件只撤销原 `consent_grants` 中本人、`agent_context`、`TASK_CONTEXT_READ`、`read` 的旧来源许可。没有第二套授权账本、自动 winner 或内容召回。旧 076 当前来源检查仍先于异步清理发挥拒绝作用；原 Moment 082/091 分支保留。

闭集最大深度 1、两个固定事件、原根共享总 claim attempt 上限 6，128 preview/100 grant 批次；延续原 control admission 的全局 4/本人 1/256 heads。旧根完成后仍可生成同根 child，但若六次已用尽，child 的 outbox 显示 `DEAD_LETTER`，不能说失效工作全部完成。revision 耗尽的 stale grant 会保留为未清完，而不是静默跳过成功。初始终态只有 outbox 队列进度，不伪造 inbox claim/receipt；既有 `PENDING` 进度回执与后来终态的差别保留。

未知 commit 返回空新 claim/receipt，runner STOPPED 且不盲重发。单元覆盖的是实际 Tx helper/commit exit + spy 和原 runner 合成未知错误，未实测 Store.pool/PG 提交崩溃或重启。新读取预览的批准与 Memory 内容写入不由这些 metadata 授权；已消费的数据不可撤回。101 down 拒绝删除已有 Memory event/progress/receipt。

证据：`docs/testing/evidence/memory-causal-invalidation-2026-10-07/`。仅直接 Go 单元及 UNIT_STATIC SQL/旧 guard 保留检查；SQL 执行、迁移 up/down、锁与多进程配额/并发、公平吞吐、重启、正式身份、全量/vet/build/真机均 NOT_RUN。整个 AIR020 继续 PARTIAL，Closed Pilot/Beta NO；13 类型通用事件、自动反思与跨事件全局因果预算不因此完成。


## AGE049 本人本地分析许可清单（2026-10-07 增量）

`GET /v1/me/agent-enrichment-purpose/grants` 是本人、当前 Personal Agent 的有界许可元数据读取。复用原 `consent_grants`/079 唯一用途许可，当前仅 `MOMENT_LOCAL_ANALYSIS` / `analyze_local`；没有新 grant、模型/视觉出口或总学习开关。接口无 query/body，拒绝组织 Workspace，先原 native 本人 Session/Agent 绑定，返回前原 native final clock 和 HTTP Session 再验。新 `InventoryStore` 是可选能力，不改变原 by-ID Store/GET/DELETE。

响应 `data` 为 `agent-enrichment-purpose-inventory-v1`，包括本人、Agent、读取钟、最多 30 秒的读取期限、limit=50、truncated、原 Grant DTO。只给原选择 ID/字段名/版本/期限等元数据，不导出正文、taskQuery、source/task binding 或 Session digest。查询在同 statement 的 PG clock 上优先未撤回且未到期的许可，再按 createdAt/id 稳定排列；撤回后刷新可继续发现余下的有效许可。清单截断不是不存在/已撤回。过期/撤回历史不构成当前分析权限，列表中的“未撤回且未到期”也不能替代原分析入口的当前来源/Task/Session/许可收口。

真实客户端入口为设置 → 本地分析许可 → 检查具体许可版本 → 确认撤回 → 原 by-ID GET/DELETE(expectedRevision) → 重新读取。复用原 SecureAgentMultiCandidatePendingStore 的 bodyless revocation journal：写前落盘、await 后复验主体/入口寿命，结果未知及重开仅原 ID GET 核实，不自动重发或恢复旧批准。当前已撤回是观察结果，不证明先前请求的因果。已消费内容不能被召回，独立记忆未删除；再次允许只能原当前来源选择/新预览/明确批准。

相关单位与完整失败历史在 `docs/testing/evidence/enrichment-privacy-control-2026-10-07/freeze11/MANIFEST.json` 和该目录 README。Go 注册 handler 与 pgx spies、Flutter 当前设置入口/控制器/大字号 widget 均有直接单位证据；SQL 未实际执行。PG/锁/持久化、OS 安全存储及跨进程 compare-delete、真实认证、手机、全量分析/构建/发布均 NOT_RUN。本轮不改变 AGE049 五族整体 PARTIAL、Personalization 总门禁及字段受众 UI 缺口，也不改变 Pilot/Beta NO。


## AIR038 本人沙箱回执核实 HTTP 消费者（2026-10-07）

新增本人 `POST /v1/me/agent-sandbox-approvals/{approvalID}/reconcile`，只接受原批准地址，无正文、query、组织工作区或客户端 feature override。它复用原 `Service.RecoverApproval` → `Store.ReconcileOwnSandboxApproval` 与同一个启动 featureController；原本人 Session、原生 owner/fence/最终钟和默认 OFF 不变。采用 POST，因为原核实可能推进已有 receipt/fence；没有注册 Preview/Confirm/Commit/Begin/Execute，也不发送外部消息/报名、重做原效果或恢复旧批准。

返回 <=8192 字节完整 `data` wrapper：固定 schema、本人、原 approval/dispatch ID、原状态及提交时间，成功时原 effect ID/发生时间。该历史视图不含 Binding、正文、Session、authority/grant、效果键或执行 handle；它不是新批准。编码后再次验证捕获的原本人 Session，并检查取消。缺地址、未知提交或错误不能推导 NO_EFFECT，也不自动重发；NO_EFFECT 只可来自原私有沙箱排他核实/永久 fence。已核实原历史不撤回已消费内容。

主应用仅增量构造 gateway 并作为可信 httpapi Option 注入，未更改开关。新注册 HTTP、domain DTO 与 gateway/原 Service 转发相关单元 20 unique 父子事件（6 顶层）有通过证据：首次路由缺失404为有效行为 RED；units02 整命令失败，原因是新 HTTP fixture 缺原 AuthenticateHumanSocial 方法，原失败保留。修正仅该 fixture，http03 的17 HTTP事件通过；02中3个 domain/adapter事件未重跑。详见 `docs/testing/evidence/action-recovery-http-2026-10-07/freeze05/MANIFEST.json`（README相对 docs/architecture 文档路径需以仓库根解析）。

以上仅合成 transport/port 单元；主应用 main.go 编译和启动、PostgreSQL/097/锁/重启/持久化、真实身份、手机、客户端恢复入口、外部消息/RSVP写工具、运维和整仓/vet/build 均 NOT_RUN。原39单位没有重跑或当本接口通过。AIR038整体仍 PARTIAL，Closed Pilot/Consumer Beta NO。适用 UX-CHECK-06/07/08/09/10/11/16，不新建批准或恢复权限体系。


## 2026-10-07：PreferenceUpdated 的有限元数据失效链（AIR020）

- 102 仅捕获原本人私人偏好 writer 的 native 变更元数据。已设置来源使用 `written_profile_version` 与私人行 `xmin`；清空使用已递增的 `agent_profiles.profile_version`、该行 `xmin` 和私人行实际不存在。source ID 为原 personal Agent ID。事件不存字段正文，不推断 RSVP、到访或概率。
- `preference-invalidation-v1` 由原 `agent-outbox-control` 和 Store 的原 claim/consume 入口消费：根阶段有界删除最多 128 个未绑定的旧 `PURPOSE_PRIVATE_PROFILE` 预览；同事务完成回执只能派生唯一 depth=1 子事件，子阶段对原 `consent_grants` 中本人 `TASK_CONTEXT_READ/read/self` 许可做最多 100 个版本 CAS 撤销。不删除绑定、已消费或已发出历史，不声称召回内容，不新增批准权源。
- 复用原 control 的 global=4、owner=1、256 tenant-head 上限。仅先提名，原 native metadata → owner 76033 → root 76034 → 当前私人来源 → outbox 锁序；最后复验原来源、held row/fence CAS、原生 clock 和期限。`CheckFence(c,c)` 是形状/TTL 检查，本身不等于 native fence 回读。
- 同根/子/重试总 attempt≤6，depth≤1，15 分钟源 TTL、30 秒以内 lease；未完成残余保持 PENDING，预算耗尽保留 DEAD_LETTER，revision 耗尽许可仍出现在 more 检查，不能假称全部清完。未知 commit 不交出新 claim/receipt，runner 停止，不自动重发。
- 固定 5 秒内只合并同 owner/Agent/source 的较低版本 configured 根事件，且必须 PENDING、attempt/fence=0、无 lease、无任何 inbox；旧事件仅置 INVALIDATED、保留原身份/来源/root/times。clear、child、已领取与已有回执不合并。此进度不是批准或授权。
- 本切片验证只含直接 Go 单元与 `UNIT_STATIC` 102 合同检查，Tx spy 调用真实 Store helper但不执行 SQL。102 up/down、PG 锁序/并发/隔离/native 时间/真实提交、生产与手机均 NOT_RUN。down 源码拒绝已有 Preference 事件/进度/回执。AIR020 总体仍 PARTIAL，13 事件通用认知、自反思/模型与外部消费者未完成；原 Moment 082/091、Memory 101 的已封存分支/证据不被本切片替代。


### 2026-10-07 原消息申请决定的因果回执（AGE042，单元验证切片）

- 原 `POST /v1/me/connection-requests/{requestID}/decision` 可选 `operationId`（UUID）；未带 key 的旧协议继续同一个原 native Tx writer。
- 新 `GET /v1/me/connection-requests/{requestID}/decision-operations/{operationID}` 仅当前本人/Session 查询原操作历史；无 body/query/Org workspace。不会开启新的批准或消息工具。
- 原页明确使用 v2，POST 前独立本机引用与服务 key 落盘；关闭重开的 UNKNOWN 只 GET 原回执，无 no-key 降级。旧 v1 本机引用永不追认为服务 key。
- `COMMITTED` 是原决定/原 Tie 或 Conversation 的历史结果，不含可执行聊天 handle；`NO_EFFECT` 仅当前 POST 权威确认原来源 `EXPIRED` 或 `ALREADY_DECIDED` 后同 Tx 留存。401/403/404、错误、坏 DTO、超时和缺回执均不能证明未生效。重放不重复决定、通知、审计或关系效果。
- 103 source 新增单个不可变领域回执表：原 Request 行与审计同 XID 的 COMMITTED guard、闭集 NO_EFFECT/显式非 NULL、使用后 down 拒绝；001–102不修改。先原当前角色/权限再对私有 key/digest，比对失败独立 `connection_operation_changed`；最后仍复用原当前 Session/policy/source/原生 clock fence。
- 验证仅新相关 Go 单元（SQL/Tx spies 与 UNIT_STATIC103）和四个 Flutter 单元文件；原生 PG、迁移/回滚、真实 Commit 不确定、并发锁、跨进程 SecureStorage CAS、全量/构建/真机/生产身份均 NOT_RUN。fixture 为合成 Store/Session，不能称原生持久化或正式身份。正式索引见 `docs/testing/evidence/connection-decision-operation-recovery-2026-10-07/freeze10/MANIFEST.json`。
