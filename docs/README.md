# Birdtie Documentation Index

This index routes to the current authoritative documentation. Product behavior, shared UX rules, map runtime requirements and identity/ownership each have one owning document; ADRs record decisions and their implementation boundaries.

## Canonical Now workspace

- [Now — Agent Map Workspace](product/NOW-AGENT-MAP-WORKSPACE.md): product position, user flow, state ownership, result/conversation behavior, Quick Actions, map search, privacy and acceptance criteria.
- [Global UX Interaction Contract](ux/GLOBAL-UX-INTERACTION-CONTRACT.md): stable entity projections, independent state axes, async safety, input/accessibility and honest controls.
- [Map Runtime Performance Contract](architecture/MAP-RUNTIME-PERFORMANCE-CONTRACT.md): persistent map lifecycle, marker diffing, camera behavior, explicit area searches and physical-device checks.
- [Agent Identity and Ownership Model](architecture/AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md): Personal Agent, Organization Agent, principal ownership and CityContext boundaries.
- [ADR 0007](decisions/0007-agent-workspace-query-boundary.md): deterministic query/task API, supported intent and backend privacy boundary.

## Product

- [Functional MVP Gap Analysis](product/FUNCTIONAL-MVP-GAP-ANALYSIS.md): code-backed status of the first 13 functional domains; the task queue lives under `automation/`.
- [Functional MVP PRD](product/FUNCTIONAL-MVP-PRD.md), [Organization Console PRD](product/ORGANIZATION-CONSOLE-PRD.md), [Release Quality Gates](product/RELEASE-QUALITY-GATES.md), and [Master Roadmap](product/BIRDTIE-MASTER-ROADMAP.md): imported target requirements and sequencing, not implementation claims.
- [Frontend IA and Shell Baseline](product/FRONTEND-IA-AND-SHELL-BASELINE.md): accepted global navigation/shell baseline. Its former Now interaction details are superseded by the canonical documents above.
- [MVP Implementation Plan](product/MVP-IMPLEMENTATION-PLAN.md)
- [Birdtie Product V3](product/01-Birdtie-产品文档-V3.0.md)

## Architecture and decisions

- [Agent Data API Contracts](architecture/AGENT-DATA-API-CONTRACTS.md): proposed target API and ResultSet structure, subordinate to accepted identity and current API decisions.
- [Agent Cognitive Architecture ADR](decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md), [Agent Memory Architecture](architecture/AGENT-MEMORY-ARCHITECTURE.md), and [Personal Agent Enrichment](product/PERSONAL-AGENT-ENRICHMENT.md): V5 cognitive ownership, target domain contracts and user outcomes; these do not claim implemented Memory/provider/execution services.
- [Architecture index](architecture/README.md)
- [Decisions](decisions/): accepted system and product decisions.
- [Migration notes](migration/): staged transition from Civu.

## Other collections

- Business: [Aberdeen Pilot Plan](business/ABERDEEN-PILOT-PLAN.md), [CSSA Partnership Playbook](business/CSSA-PARTNERSHIP-PLAYBOOK.md), and [Outreach Scripts](business/CSSA-OUTREACH-SCRIPTS.md). These are planning material, not evidence of live product capability or an agreed partnership.
- Research: `research/`
- UX contracts: `ux/`

The 2026-09-30 execution pack is preserved at the repository root as `Birdtie_Execution_and_CSSA_Partner_Pack.zip`. Its `docs/02_NOW_WORKSPACE_PRD.md` was reconciled into the existing canonical Now specification rather than copied as a second authority. Its other imported documents record the ZIP member path at their top. The workbook planning snapshot is under `requirements/`; `automation/codex_task_queue.json` owns live task status.
