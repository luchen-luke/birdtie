# ADR 0001: Birdtie Foundation data boundaries

Date: 2026-09-29  
Status: Accepted for the first local Foundation slice; deployment vendors remain undecided.

## Context

Birdtie needs its own User, City, Place, Geo and Media model before City Graph content and discovery. The prior local API held illustrative Aberdeen records in Go variables; the current frontend no longer uses those records. The Civu reuse audit found substantial product and provider coupling, and no Civu code or data is migrated here.

## Decision

1. PostgreSQL is the authority for accounts, grants, cities, places, source references and media records. The API uses [pgx's documented PostgreSQL pool](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool) and requires an explicit database URL. This repository owns its migration lineage.
2. Public City/Place reads are the first API capability. Published status is checked in SQL. Unknown or unpublished objects return 404. Responses include source, maintainer, update time and freshness; expiry is visible instead of silently presenting stale content as current.
3. `aberdeen-gb` is seeded only as Birdtie's selected pilot city, with `building` content and unverified source status. No Place, Activity, user, attendance or popularity data is fabricated.
4. Canonical Place IDs are separate from provider references. Place coordinates use WGS84 latitude/longitude with database range checks and explicit precision. A Place marked with no location precision cannot store coordinates. PostGIS remains a later decision once the target database and actual map query needs are known.
5. Account/profile, consent, media assets, private EXIF and media variants are separate tables. Original media stays private; a public variant requires stripped metadata and an approval reference. No endpoint accepts client-supplied actor identity, publishes media, or exposes private metadata in this slice.
6. Browser CORS is an exact origin allowlist configured at runtime. The API binds to loopback by default. Production authentication, storage, moderation and organization write policies are prerequisites for write endpoints.

## Consequences and follow-up

- The new shell can discover a real `building` City state without treating demo content as live. Place lists remain empty until source review and City Seed operations exist.
- Account/session implementation must establish a server-derived actor and enforce Consent before private reads or writes.
- Media upload needs an approved object-storage provider, quarantine/scan/EXIF pipeline, key management and delete path before any media route is added.
- City and Place maintenance need authenticated editorial actions, provenance review, duplicate reconciliation and audit before pilot content publication.
- PostGIS, hosting region, storage region, supplier contracts and operational backup/restore are open decisions before deployment.
