# Birdtie MVP implementation plan

Identity and authority scope is governed by [Agent Identity and Ownership Model](../architecture/AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md). Organization principal, membership and Agent lifecycle are foundational MVP work; earlier references to a parallel City Agent are superseded by platform-managed CityContext retrieval in the shared runtime.

Date: 2026-09-29
Status: historical implementation plan. The Agent-first shell in `FRONTEND-IA-AND-SHELL-BASELINE.md` is the current frontend baseline; older Now/Explore/Network wording below is historical context only. The [Functional MVP PRD](FUNCTIONAL-MVP-PRD.md), [Master Roadmap](BIRDTIE-MASTER-ROADMAP.md), [Gap Analysis](FUNCTIONAL-MVP-GAP-ANALYSIS.md) and task queue now govern planned functional delivery. Earlier statements below that full organization administration belongs to a later slice describe the prior scope; organization publishing is required for the new functional vertical slice.

Update (2026-09-30): The Agent-first shell supersedes the older navigation wording below. Owner Profile editing and public Intent submission now exist. Owner-confirmed Group and public Intent submissions publish directly under ADR 0011; no real content seed is configured. Explicit contact requests and human messages are available under ADR 0014; production identity remains a later slice.

## MVP outcome

Prepare a usable first-city product for an initial Aberdeen cohort. The first release should help a newcomer discover trustworthy local information and upcoming activities, express interest, contact another person or organizer with consent, take part, and optionally share a recap.

## Product information architecture baseline

The current frontend information architecture is **Map Workspace + Agent + Sidebar + Inbox**, defined in [`FRONTEND-IA-AND-SHELL-BASELINE.md`](FRONTEND-IA-AND-SHELL-BASELINE.md). [`01-Birdtie-产品文档-V3.0.md`](01-Birdtie-产品文档-V3.0.md) remains a domain reference, but its Now / Explore / Network top-level navigation predates the Agent-first decision.

The previous Flutter screen was an early local demo and was replaced by the new shell. Its navigation, screen hierarchy, sample content, and local state are not part of the production baseline and must not be restored as the app structure.

The post-2026-09-25 reference review and Birdtie-specific IA mapping are recorded in [`FRONTEND-IA-AND-SHELL-BASELINE.md`](FRONTEND-IA-AND-SHELL-BASELINE.md). The read-only Civu client had active shell/map/profile/discovery changes from 2026-09-26 to 2026-09-29 (including `Civu-client/lib/src/screens/page_home_shell.dart`); its structural patterns informed the review, while its navigation/domain decisions remain excluded where they conflict with Birdtie. No Civu code was copied.

## Delivery slices

1. **Agent-first shell (implemented scaffold):** full-screen Map Workspace, Sidebar, Inbox overlay, Agent Composer, contextual Entity Layer and Result Sheet replace the mobile bottom navigation. The state flow uses a local preview or clearly labeled rule-based City Graph query; it is not a full AI Agent.
2. **Foundation (in progress):** Birdtie-owned PostgreSQL schema separates Account/User, Consent, City/Geo, canonical Place/provider references, private Media/EXIF, public variants and audit records. Public City/Place/Activity reads, server-derived Session/Profile Consent, separate-Account City Seed Place/Activity review, generic OIDC server flow and Flutter web callback exist. OIDC provider registration, real login validation, native callbacks and editor role provisioning remain. Media storage, scanning, key management and upload remain undecided.
3. **City Graph content (partial):** reviewed City Seed Activity and owner-confirmed public Group/Intent publication exist. Owner-only private Moment draft controls exist; public Moment and Journey publishing do not. Group/Intent publication has no mandatory human review under ADR 0011. Explicit contact requests and human messages require mutual acceptance under ADR 0014.
4. **City discovery (partial):** public City/Place/Activity reads and contextual Agent results exist. Real People appear in results; only owner-opted-in broad area markers appear on the map (ADR 0015). Aberdeen Mapbox web rendering exists; production token/origin and actual tile loading still need review. China AMap Web rendering is wired but awaits Key/proxy and live verification; native AMap rendering, broader filters, pagination and camera restoration remain later work.
5. **Mutual connection:** add contact requests and accept/decline/expire/block/report states; open a conversation or activity coordination only after explicit acceptance.
6. **Agent layer:** add private Personal Agent draft assistance and source-grounded City Context retrieval after ACL, Consent and object provenance are enforced by shared services. External actions remain user-approved proposals.
7. **Pilot hardening:** privacy review, observability, backup/restore, moderation operations, deployment and cohort onboarding.
8. **Existing-user upgrade and cutover:** retain the Civu store app identities, migrate existing accounts/content/media through the new model, bridge active old-client routes, validate the signing and device-data upgrade, then retire old services by measured route usage. The executable gates and route inventory are in [`CIVU-TO-BIRDTIE-CUTOVER.md`](../migration/CIVU-TO-BIRDTIE-CUTOVER.md).

## Initial exclusions

Automated bulk Memory import, autonomous/general-purpose Agents, multi-stop Journey authoring, recommendation models, multi-city rollout, monetization, and full organization administration are later slices. The initial Agent slice is limited to permission-bounded draft assistance and cited City Graph retrieval. Do not copy the Civu repository or its code wholesale; use `docs/architecture/CIVU-REUSE-MATRIX.md` for any future targeted reuse assessment.

## Existing preview status

The earlier local Flutter demo screen was replaced by the new Birdtie shell. Its Aberdeen sample records and screen hierarchy are not carried forward as product data or frontend structure.

## Frontend baseline acceptance

- Navigation and page hierarchy follow the Agent-first shell baseline: full-screen Map Workspace, Agent Composer, Sidebar and Inbox.
- Map Workspace and Result Sheet share the selected City and task context. Published City records carry source/freshness; no device location is requested. Broader filters remain later work.
- The same shell adapts to narrow and wide layouts around the connected City API and the labelled local preview source.
- The post-2026-09-25 reference structure has been reviewed and its Birdtie-specific adoption decisions recorded in the shell baseline; Civu product/domain semantics are excluded where they conflict.
- Local sample data is labeled as prototype-only and is not carried as live Aberdeen content.

Current state: the Agent-first shell, browser auth connector, public City/Place context, Aberdeen Mapbox web map, reviewed Activity reads, owner-published Group/Intent flow and private text-only Moment drafts are implemented. Owner-only Saved supports Place/Activity/Group bookmarks with visibility rechecks (ADR 0012). My Activities holds private plans to attend, explicitly separate from registration or hosting (ADR 0013). Settings uses the existing Session, Block and Profile Consent APIs for account controls. A real People result can start an explicit contact request; only acceptance opens a human conversation (ADR 0014). The Android preview previously loaded the selected Mapbox style on a real device without replacing Civu. The provider direction is mainland China AMap and overseas Aberdeen Mapbox; see ADR 0006. Existing-user upgrade still requires the account/data/service migration gates above.


