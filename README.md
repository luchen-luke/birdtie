# Birdtie

Birdtie is an **Agent-native local social network**: a place-centered social product where people and agents can discover, share, and build local context together.

Birdtie is the next version of 栖游/Civu, rebuilt with a new product and data model in this repository. Existing users, content, interfaces and services will migrate in stages. The final mobile release retains the existing Android/iOS store app identities; Android debug uses a separate preview suffix for side-by-side device work. The migration gates are in [`docs/migration/CIVU-TO-BIRDTIE-CUTOVER.md`](docs/migration/CIVU-TO-BIRDTIE-CUTOVER.md).

## Canonical repository

`D:\Project\birdtie` is the only official Birdtie repository and source of truth for Birdtie code and project documentation. Keep product, business, architecture, research, and decision documents in `docs/`.

## Current product slice

The client currently has a new responsive Birdtie shell in `apps/client/`, based on the product IA in `docs/product/01-Birdtie-产品文档-V3.0.md`. It provides Now, Explore, Network, Inbox, My Birdtie, and a responsive desktop sidebar. Its web build includes an OIDC callback and Session/Profile read connection. Now and Explore share the published City API state; Explore List reads and searches published Places with provenance, while Aberdeen Map uses a configured Mapbox public token and the same public Place result. The Android preview loaded Civu's custom Mapbox style on a real phone. Now reads reviewed Activities with time-derived status. Signed-in users can manage private text-only Moment drafts. Connections and public creation actions are not connected yet. The UI labels these states instead of presenting sample Aberdeen records as live content. The earlier local demo screen was replaced and is not the implementation baseline.

The Go API now has a Birdtie-owned PostgreSQL Foundation and City Graph schema, public City/Place/Activity reads, private Moment draft operations, Session/Profile Consent, reviewed City Seed Place/Activity editorial routes and a provider-neutral OIDC server flow. No OIDC issuer/client is configured, so no real login has been exercised. Media storage has a provider-neutral contract but no upload route; user Moment/Journey/Intent publishing remains closed. Aberdeen is the only pilot city record and is marked as content `building` and source `unverified`; no Place or Activity records are seeded. See [`apps/api/README.md`](apps/api/README.md) for local setup.

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
