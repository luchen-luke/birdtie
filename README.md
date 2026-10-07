# Birdtie

Birdtie is an **Agent-native local social network**: a place-centered social product where people and agents can discover, share, and build local context together.

Birdtie is the next version of 栖游/Civu, rebuilt with a new product and data model in this repository. Existing users, content, interfaces and services will migrate in stages. The final mobile release retains the existing Android/iOS store app identities; Android debug uses a separate preview suffix for side-by-side device work. The migration gates are in [`docs/migration/CIVU-TO-BIRDTIE-CUTOVER.md`](docs/migration/CIVU-TO-BIRDTIE-CUTOVER.md).

## Canonical repository

`D:\Project\birdtie` is the only official Birdtie repository and source of truth for Birdtie code and project documentation. Keep product, business, architecture, research, and decision documents in `docs/`.

## Current product slice

The client follows the Birdtie V2 [Agent-first IA baseline](docs/product/FRONTEND-IA-AND-SHELL-BASELINE.md) and the canonical [Now — Agent Map Workspace](docs/product/NOW-AGENT-MAP-WORKSPACE.md): a persistent map with an intent composer, stable entity projections, result sheet, conversation, Sidebar and Inbox action center. With an API URL configured, tasks run Birdtie-owned deterministic public Activity discovery; follow-ups keep the active task context and Search this area sends explicit settled map bounds. Signed-in task history is private and restored with current visibility rules. Sidebar → Groups lets Owners publish and withdraw their own Groups without an external source link or human review. Inbox reads Place and Activity review outcomes. Without an API URL, the isolated local source uses clearly labelled demo People/Group/map entities. Private Moment drafts, Profile editing, owner-confirmed public People Intent submission, browser OIDC, and optional local development phone login are available from Sidebar → Profile. Aberdeen's map uses the configured Mapbox style. Connections, semantic Agent matching and Agent-created Activities are not connected yet.

The Go API has a Birdtie-owned PostgreSQL Foundation and City Graph schema, public City/Place/Activity reads, a public rule-based Agent query endpoint, private Agent task history and Moment drafts, Session/Profile Consent, reviewed City Seed Place/Activity publication, owner-confirmed Group/Intent publication, owner-only Place/Activity review updates and a provider-neutral OIDC server flow. No OIDC issuer/client is configured. An optional loopback-only development phone login uses fixed code `123456` to exercise signed-in flows without verifying phone ownership or linking Civu accounts. Owners can edit Profiles and publish time-limited public People Intents; there is no contact request or messaging route yet. Media storage has a provider-neutral contract but no upload route; user Moment/Journey publishing remains closed. Aberdeen is the only pilot city record and is marked as content `building` and source `unverified`; no Place, Activity, public Intent or Group records are seeded. See [`apps/api/README.md`](apps/api/README.md) for local setup.

To run the Flutter client:

```powershell
cd D:\Project\birdtie\apps\client
flutter run -d chrome
```

## Civu reference source

`D:\Program\Civu` is a read-only reference source. It is not part of this repository and must not be modified as part of Birdtie work. Do not copy the entire Civu repository into Birdtie. Any proposed reuse or migration must first be evaluated in [`docs/architecture/CIVU-REUSE-MATRIX.md`](docs/architecture/CIVU-REUSE-MATRIX.md), including dependency, licensing, privacy, and architecture risks.

The old `D:\BirdTie` working directory is retained as-is. Only explicitly selected Birdtie materials may be copied from it; do not delete or move its contents.

## Documentation

- Product: `docs/product/`
- Business: `docs/business/`
- Architecture: `docs/architecture/`
- UX interaction contracts: `docs/ux/`
- Research: `docs/research/`
- Decisions: `docs/decisions/`
- Migration notes: `docs/migration/`
- MVP implementation sequence: `docs/product/MVP-IMPLEMENTATION-PLAN.md`
