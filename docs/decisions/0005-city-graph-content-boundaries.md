# ADR 0005: First City Graph content boundaries

Date: 2026-09-30  
Status: Schema, reviewed public Activity read path and owner-only Moment drafts added; user content publishing remains gated.

## Decision

`006_city_graph_content.sql` defines Birdtie-owned Moment, Activity, Journey and Intent records. It seeds no content. Each object keeps one canonical ID and a City relation; Place and cross-object links use foreign keys rather than copying titles or bodies into multiple contexts. Place/Activity/Journey links must belong to the same City. A later multi-city Journey model will require an explicit migration.

- **Moment** starts private and draft. A public published Moment requires an author confirmation timestamp. `place_id` may remain a private link while public `location_precision` is `none` or `city`; public reads must omit the place and any precise location unless the author chose `place`. Time precision is explicit. Memory/EXIF media is not linked into public reads by this schema.
- **Activity** has start/end instants, an IANA time zone, source, maintainer, verification and expiry fields. Upcoming, ongoing and past are derived from time at read time; cancellation is explicit. Past and cancelled Activities must never expose a live participation action. Publishing is a separate reviewed write path.
- **Journey** starts private and draft; publication requires owner confirmation. Stops are ordered and may hold a private Place link while `location_visibility` hides or coarsens it. A copy/replicate uses a new Journey ID and may retain an inspiration reference.
- **Intent** requires a time window, time zone, coarse area and expiry. It starts private and draft. Active state requires owner confirmation; public discovery additionally requires `audience=public`, a future expiry and the relevant authorization/block policy.

City Seed Activity candidates use active editor membership and a different reviewer Account. The reviewer checks source and rights, then `publish` creates one canonical Activity, source evidence and City Seed maintenance relation transactionally. Public Activity reads require published City and Activity status; a hidden Place ID is omitted. An authenticated viewer's blocks also filter Account-hosted Activities. The public API derives upcoming/ongoing/past/cancelled from time and cancellation, and exposes no participation action. External `hostLabel` remains a source assertion, not verified organization identity. No editor or Activity is seeded.

Owner-only Moment draft creation, listing, detail, revision-checked update and withdrawal are now implemented with Session-derived authors and audit events. The initial client supports text-only City-level drafts and has no public publish control. Withdrawal removes a Moment from the active draft list. Real user exercise depends on an OIDC provider configuration.

Schema constraints alone do not make user content safe for public release. Before opening user Moment/Journey/Intent publishing and public reads, implement moderation/block handling, author confirmation, rate limits and a shared visibility policy. UI, public search, maps and Agents must call the same policy. No generic arbitrary graph edge writer is exposed.

The earlier Civu audit remains a reference only. No Civu migration, data or business code was copied.
