# Architecture Index

- [Agent Data API Contracts](AGENT-DATA-API-CONTRACTS.md) records proposed target entities and endpoint concepts; accepted ownership and implemented API decisions remain in the model and ADR 0007.
- [Agent Identity and Ownership Model](AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md) owns Personal Agent, Organization Agent, principal authority and CityContext identity boundaries.
- [Agent Cognitive Architecture ADR](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md) extends the existing identity/runtime decisions with AGE/AIR ownership, current implementation limits and version/consent/execution boundaries.
- [AgentProfile Foundation V5](AGENT-PROFILE-FOUNDATION-V5.md) owns the shared Profile canonical: AGE001 metadata/binding/store and AGE002 Public/Private separation with ordinary self editing. It records each task's implementation and evidence separately; field audiences, cognitive access, Memory and new UI remain follow-up work.
- [Agent Memory Architecture](AGENT-MEMORY-ARCHITECTURE.md) owns the V5 target Profile/Memory/Evidence and source-invalidation boundary; Phase 0 contracts are not implemented Memory services or live model adapters.
- [Community and Activity Social Model](COMMUNITY-AND-ACTIVITY-SOCIAL-MODEL.md) owns Community membership, constrained Activity organizer, visibility and permission boundaries; [ADR 0016](../decisions/0016-community-social-layer-and-activity-organizer.md) records the decision and its relationship to earlier ADRs.
- [ADR 0017](../decisions/0017-v4-actor-agent-context-place-model.md) defines V4 ActorRef, Agent roles, global Context and Place/Venue/Business targets while preserving existing runtime identities until migration.
- [Context Graph V4](CONTEXT-GRAPH-V4.md) records typed Context nodes, private Person relations, Agent Task compatibility and migration 033 verification.
- [Place Context Model V4](PLACE-CONTEXT-MODEL-V4.md) records canonical Place IDs, sourced addresses, Activity/Moment links and public read boundaries.
- [Opportunity Engine V1](OPPORTUNITY-ENGINE-V1.md) records rule-based candidate inputs, hard constraints, entity references, reasons and privacy limits.
- [Agent Context Access Policy](AGENT-CONTEXT-ACCESS-POLICY.md) defines public, connection, close, private and workspace-history reads for the shared Agent runtime.
- [Map Runtime Performance Contract](MAP-RUNTIME-PERFORMANCE-CONTRACT.md) owns map widget lifetime, marker updates, camera events, explicit bounds searches and device performance acceptance.
- [Map Provider Configuration](MAP-PROVIDER-CONFIGURATION.md) owns Mapbox/AMap credentials and provider-specific setup.
- [City Editor Access Runbook](CITY-EDITOR-ACCESS-RUNBOOK.md) owns local editorial access setup.
- [Civu Reuse Matrix](CIVU-REUSE-MATRIX.md) records the boundary for reference-source reuse.
- [Foundation Implementation Status](FOUNDATION-IMPLEMENTATION-STATUS.md) records implementation progress; it is not a replacement for accepted architecture contracts.
- [Architecture V1](03-Birdtie-技术架构-V1.0.md) provides the broad Birdtie architecture overview.

Agent task and query API decisions are recorded in [ADR 0007](../decisions/0007-agent-workspace-query-boundary.md). The Now feature specification is [here](../product/NOW-AGENT-MAP-WORKSPACE.md).
- [Agent Intelligence Runtime V5](AGENT-INTELLIGENCE-RUNTIME-V5.md) preserves the AIR design target under the cognitive ADR; it does not claim a live provider or executor.
