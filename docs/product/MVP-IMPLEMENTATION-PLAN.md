# Birdtie MVP implementation plan

Date: 2026-09-29
Status: Initial implementation sequence; product and architecture assumptions remain subject to validation.

Update (2026-09-30): The Agent-first shell supersedes the older navigation wording below. Owner Profile editing and public Intent submission now exist; a different City reviewer must approve an Intent before it enters People results. No real reviewer operation or content seed is configured. Contact requests, messaging and production identity remain later slices.

## MVP outcome

Prepare a usable first-city product for an initial Aberdeen cohort. The first release should help a newcomer discover trustworthy local information and upcoming activities, express interest, contact another person or organizer with consent, take part, and optionally share a recap.

## Product information architecture baseline

The canonical information architecture is in [`01-Birdtie-产品文档-V3.0.md`](01-Birdtie-产品文档-V3.0.md), section 4: **Now / Today, Explore, Network, Inbox, My Birdtie, Personal Agent, and Organization / City workspace**. City pages organize Now, Things to Do, Places, Moments & Experiences, Activities, Journeys, Communities & Organizations, and People & Alumni (section 5). The frontend navigation and shell mapping is recorded in [`FRONTEND-IA-AND-SHELL-BASELINE.md`](FRONTEND-IA-AND-SHELL-BASELINE.md).

The previous Flutter screen was an early local demo and was replaced by the new shell. Its navigation, screen hierarchy, sample content, and local state are not part of the production baseline and must not be restored as the app structure.

The post-2026-09-25 reference review and Birdtie-specific IA mapping are recorded in [`FRONTEND-IA-AND-SHELL-BASELINE.md`](FRONTEND-IA-AND-SHELL-BASELINE.md). The read-only Civu client had active shell/map/profile/discovery changes from 2026-09-26 to 2026-09-29 (including `Civu-client/lib/src/screens/page_home_shell.dart`); its structural patterns informed the review, while its navigation/domain decisions remain excluded where they conflict with Birdtie. No Civu code was copied.

## Delivery slices

1. **Birdtie shell replacement (implemented scaffold):** responsive Now/Explore/Network/Inbox navigation, My Birdtie entry, desktop sidebar and honest disconnected states. Public City/Place and reviewed Activity reads now connect to the API.
2. **Foundation (in progress):** Birdtie-owned PostgreSQL schema separates Account/User, Consent, City/Geo, canonical Place/provider references, private Media/EXIF, public variants and audit records. Public City/Place/Activity reads, the first server-derived Session/Profile Consent boundary, separate-Account City Seed Place/Activity review, generic OIDC server flow and Flutter web callback exist. OIDC provider registration, real login validation, native callbacks and editor role provisioning remain. Media has a provider-neutral contract; storage, scanner, key management and upload are still undecided. User content publishing and deployment choices remain.
3. **City Graph content:** first Moment, Activity, Journey and Intent schema and typed City/Place/cross-object relations are implemented. Reviewed City Seed Activity publishing and public Activity reads exist. Owner-only private Moment draft create/read/edit/withdraw routes and text-only client controls exist. Next complete user content moderation, public visibility policy and author confirmation before opening publishing; expand Journey and Intent authoring later.
4. **City discovery:** Now/Explore share published City context; Explore Map/List share the Place query and detail, with source/freshness and public point markers. Aberdeen Mapbox web rendering is integrated; production token/origin and actual tile loading still need review. China AMap rendering, broader City Graph queries, category/time filters, pagination and camera restoration follow. Personalized ranking follows deterministic discovery.
5. **Mutual connection:** add contact requests and accept/decline/expire/block/report states; open a conversation or activity coordination only after explicit acceptance.
6. **Agent layer:** add private Personal Agent draft assistance and source-grounded City Agent retrieval after ACL, Consent and object provenance are enforced by shared services. External actions remain user-approved proposals.
7. **Pilot hardening:** privacy review, observability, backup/restore, moderation operations, deployment and cohort onboarding.
8. **Existing-user upgrade and cutover:** retain the Civu store app identities, migrate existing accounts/content/media through the new model, bridge active old-client routes, validate the signing and device-data upgrade, then retire old services by measured route usage. The executable gates and route inventory are in [`CIVU-TO-BIRDTIE-CUTOVER.md`](../migration/CIVU-TO-BIRDTIE-CUTOVER.md).

## Initial exclusions

Automated bulk Memory import, autonomous/general-purpose Agents, multi-stop Journey authoring, recommendation models, multi-city rollout, monetization, and full organization administration are later slices. The initial Agent slice is limited to permission-bounded draft assistance and cited City Graph retrieval. Do not copy the Civu repository or its code wholesale; use `docs/architecture/CIVU-REUSE-MATRIX.md` for any future targeted reuse assessment.

## Existing preview status

The earlier local Flutter demo screen was replaced by the new Birdtie shell. Its Aberdeen sample records and screen hierarchy are not carried forward as product data or frontend structure.

## Frontend baseline acceptance

- Navigation and page hierarchy map to the Birdtie IA in the product document, including clear Now/Explore/Network/Inbox entry points and My Birdtie access.
- City Explore supports map and list with shared filters, selected-city browsing without device location, and clear source/freshness.
- Responsive behavior is defined for narrow and wide layouts before connecting backend data.
- The post-2026-09-25 reference structure has been reviewed and its Birdtie-specific adoption decisions recorded in the shell baseline; Civu product/domain semantics are excluded where they conflict.
- Local sample data is labeled as prototype-only and is not carried as live Aberdeen content.

Current state: the shell scaffold, browser auth connector, public City/Place context, Aberdeen Mapbox web map, reviewed Activity reads and private text-only Moment drafts are implemented. The Android preview also loaded the Civu custom Mapbox style on a real device without replacing the installed Civu app. Public Moment/Intent queries and the remaining product destinations are not implemented. The provider direction is mainland China AMap and overseas Aberdeen Mapbox; see ADR 0006. The final update requires the account/data/service migration gates above.


