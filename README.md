# Birdtie

Birdtie is an **Agent-native local social network**: a place-centered social product where people and agents can discover, share, and build local context together.

Birdtie is the next version of 栖游/Civu, rebuilt with a new product and data model in this repository. Existing users, content, interfaces and services will migrate in stages. The final mobile release retains the existing Android/iOS store app identities; Android debug uses a separate preview suffix for side-by-side device work. The migration gates are in [`docs/migration/CIVU-TO-BIRDTIE-CUTOVER.md`](docs/migration/CIVU-TO-BIRDTIE-CUTOVER.md).

## Canonical repository

`D:\Project\birdtie` is the only official Birdtie repository and source of truth for Birdtie code and project documentation. Keep product, business, architecture, research, and decision documents in `docs/`.

## Current product slice

The client follows the Birdtie V2 [Agent-first IA baseline](docs/product/FRONTEND-IA-AND-SHELL-BASELINE.md): a full-screen Map Workspace with an intent composer, contextual entity layer, result sheet, Sidebar and Inbox action center. With an API URL configured, tasks run a Birdtie-owned rule-based query across public City Graph records; signed-in task history is private and restored with current visibility rules. Sidebar → Groups provides Owner submission for independent review, and Inbox reads real review outcomes. Without an API URL, the isolated local source uses clearly labelled demo People/Group/map entities. Private Moment drafts, Profile editing, independently reviewed People Intent submission, browser OIDC, and optional local development phone login are available from Sidebar → Profile. Aberdeen's map uses the configured Mapbox style. The former Now/Explore UI is retained in `apps/client/lib/src/legacy/` while its useful capabilities move into the new shell. Connections, AI matching and messaging are not connected yet.

The Go API has a Birdtie-owned PostgreSQL Foundation and City Graph schema, public City/Place/Activity reads, a public Agent query endpoint, private Agent task history and Moment drafts, Session/Profile Consent, reviewed City Seed Place/Activity/Community/Intent publication, owner-only Inbox review updates and a provider-neutral OIDC server flow. No OIDC issuer/client is configured. An optional loopback-only development phone login uses fixed code `123456` to exercise signed-in flows without verifying phone ownership or linking Civu accounts. Owners can edit Profiles and submit public People Intents for independent City review; there is no contact request or messaging route yet. Media storage has a provider-neutral contract but no upload route; user Moment/Journey publishing remains closed. Aberdeen is the only pilot city record and is marked as content `building` and source `unverified`; no Place, Activity, public Intent or Community records are seeded. See [`apps/api/README.md`](apps/api/README.md) for local setup.

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
- Research: `docs/research/`
- Decisions: `docs/decisions/`
- Migration notes: `docs/migration/`
- MVP implementation sequence: `docs/product/MVP-IMPLEMENTATION-PLAN.md`
