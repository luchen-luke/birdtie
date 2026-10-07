# Birdtie 全部任务状态明细（2026-10-07）

唯一队列的冻结快照。开发 PAUSED；Closed Pilot / Consumer Beta 均 NO。

256项：DONE180 / PARTIAL14 / TODO48 / BLOCKED14 / IN_PROGRESS0；76项未全完成。

P0共150项：DONE121 / PARTIAL7 / TODO13 / BLOCKED9；29项未全完成。

DONE沿用原队列历史证据，不等于该功能已在生产或当前全部设备上重新验收。完整goal/acceptance/verify/history/evidence保留在同包TASKS.snapshot.json；未重写旧任务。

源要求/提案/验收场景与队列工作包不同，137条V5来源映射不能与256相加。

## 全部150项P0状态

| ID | 状态 | 原任务标题 | 尚未DONE的依赖 |
|---|---|---|---|
| BT-AUD-001 | DONE | Create functional MVP gap audit | — |
| BT-RUN-001 | DONE | Fix physical-device API connectivity and environment config | — |
| BT-RUN-002 | DONE | Add request tracing and debug diagnostics | — |
| BT-DAT-001 | DONE | Finalize core schema for Organization Activity Place Participation | — |
| BT-DAT-002 | DONE | Create development seed fixtures through real database path | — |
| BT-AUT-001 | DONE | Ensure real authentication session reaches backend | — |
| BT-ORG-001 | DONE | Implement organization membership and admin authorization | — |
| BT-ACT-001 | DONE | Implement Activity create/edit/publish/cancel APIs | — |
| BT-ORG-002 | DONE | Build minimal Organization Console activity publisher | — |
| BT-SRC-001 | DONE | Implement real nearby Activity search | — |
| BT-PUL-001 | DONE | Implement Area Pulse API from real data | — |
| BT-NOW-001 | DONE | Connect Now Workspace to real Area Pulse and Activity data | — |
| BT-MAP-001 | DONE | Stabilize persistent map and keyboard lifecycle | — |
| BT-MAP-002 | DONE | Implement stable semantic marker system and selection persistence | — |
| BT-AGT-001 | DONE | Implement normalized MVP intent parser/router | — |
| BT-AGT-002 | DONE | Implement AgentResponse and ResultSet contract | — |
| BT-AGT-003 | DONE | Connect Now composer to real Agent query flow | — |
| BT-AGT-004 | DONE | Persist conversation task context for follow-ups | — |
| BT-RSV-001 | DONE | Implement RSVP persistence and Activity detail primary action | — |
| BT-PLN-001 | DONE | Implement Plans from real participation/save data | — |
| BT-DET-001 | DONE | Complete consumer-grade Activity Detail page | — |
| BT-SAF-001 | DONE | Enforce map privacy boundary | — |
| BT-PER-001 | DONE | Protect async flows from stale responses | — |
| BT-TST-001 | DONE | Create end-to-end vertical-slice integration test/checklist | — |
| BT-REL-001 | BLOCKED | Pass closed-pilot release gate | BT-AUT-002(BLOCKED), BT-TST-002(BLOCKED) |
| BT-RUN-003 | DONE | Centralize environment configuration and reject release loopback API | — |
| BT-AUT-002 | BLOCKED | Complete verified native sign-in path and production auth boundary | — |
| BT-ORG-004 | DONE | Complete membership invite, role and revoke lifecycle | — |
| BT-MAP-004 | DONE | Show public Organization markers only from explicit location data | — |
| BT-NTF-002 | DONE | Make starts-soon reminders durable across service downtime | — |
| BT-TST-002 | BLOCKED | Run real closed-pilot staging E2E and verify external dependencies | BT-AUT-002(BLOCKED) |
| BT-V4-AUD-001 | DONE | Snapshot current repository and running-task state | — |
| BT-V4-AUD-002 | DONE | Reconcile V3 backlog, Community tasks, and latest 39/3/3 status | — |
| BT-V4-CAN-001 | DONE | Publish BirdTie Canonical Product Spec V4 | — |
| BT-V4-ADR-001 | DONE | Create ADR for ActorRef/PrincipalRef, Agent roles, Context, Place/Venue/Business | — |
| BT-V4-MIG-001 | DONE | Create non-destructive V4 schema/API migration plan | — |
| BT-V4-AUT-001 | DONE | Upgrade autonomous Codex queue to V4 | — |
| BT-V4-TST-001 | DONE | Lock existing vertical-slice regression baseline | — |
| BT-V4-ACT-001 | DONE | Introduce ActorRef/PrincipalRef domain abstraction | — |
| BT-V4-AGF-001 | DONE | Create shared Agent Runtime with role/capability packs | — |
| BT-V4-AGF-002 | DONE | Normalize Agent ownership and identity boundary | — |
| BT-V4-CTX-001 | DONE | Replace city-root assumptions with generic Context Graph semantics | — |
| BT-V4-PRV-001 | DONE | Define Agent context-access policy | — |
| BT-V4-TIE-001 | DONE | Implement persistent Connection/Friend requests | — |
| BT-V4-TIE-002 | DONE | Implement connection lifecycle, remove and block | — |
| BT-V4-CHT-001 | DONE | Create canonical Conversation/Message model | — |
| BT-V4-CHT-002 | DONE | Implement 1:1 Chat for connected users | — |
| BT-V4-CHT-003 | DONE | Share BirdTie entities into Chat | — |
| BT-V4-PRV-002 | DONE | Enforce social messaging and discovery privacy controls | — |
| BT-V4-INT-001 | DONE | Create first-class social Intent entity | — |
| BT-V4-INT-002 | DONE | Support IN_PERSON / ONLINE / HYBRID intent modality | — |
| BT-V4-INT-003 | DONE | Implement Intent audience and visibility policy | — |
| BT-V4-INT-004 | DONE | Implement Intent lifecycle and expiry | — |
| BT-V4-INT-005 | DONE | Bridge AgentTask intent parsing into first-class Intent | — |
| BT-V4-OPP-001 | DONE | Implement Opportunity Engine v1 | — |
| BT-V4-OPP-002 | DONE | Make routing Tie-aware | — |
| BT-V4-PLC-001 | DONE | Promote Place to canonical social-context entity | — |
| BT-V4-VEN-001 | DONE | Add Venue capability model | — |
| BT-V4-BIZ-001 | DONE | Add Business principal and Place/Venue ownership relations | — |
| BT-V4-PLC-003 | DONE | Implement Intent-to-Place matching v1 | — |
| BT-V4-PLC-004 | DONE | Normalize Activity ↔ Place/Venue relation | — |
| BT-V4-PLC-005 | PARTIAL | Build/upgrade Place Detail page | — |
| BT-V4-MOM-001 | DONE | Link Moment to Place/Activity/Community context | — |
| BT-V4-COMM-001 | DONE | Reconcile current Community implementation against canonical model | — |
| BT-V4-ACTY-001 | DONE | Generalize Activity organizer to ActorRef | — |
| BT-V4-ACTY-002 | DONE | Normalize Activity modality and visibility | — |
| BT-V4-ORG-002 | DONE | Unify actor-specific activity ownership authorization | — |
| BT-V4-NOW-001 | DONE | Refactor Now into context-aware social workspace | — |
| BT-V4-MAP-001 | DONE | Add typed map layers for Opportunities/Activities/Places/Moments/Org/Business | — |
| BT-V4-MAP-002 | PARTIAL | Preserve existing map lifecycle/performance guarantees after V4 layers | — |
| BT-V4-NOW-002 | DONE | Add Active Intent model to Now | — |
| BT-V4-NOW-004 | DONE | Generalize Agent ResultSet/entity cards to V4 entity types | — |
| BT-V4-NOW-005 | DONE | Implement non-map flow for online intents | — |
| BT-V4-ACTN-001 | DONE | Normalize entity actions across BirdTie | — |
| BT-V4-PLN-001 | DONE | Integrate Intent/Activity/Place with Plans | — |
| BT-V4-SAF-001 | DONE | Re-run and extend public precise-location privacy boundary | — |
| BT-V4-SAF-002 | BLOCKED | Implement relationship/intent/profile visibility matrix | — |
| BT-V4-SAF-003 | DONE | Unify block/report/support across social entities | — |
| BT-V4-SAF-004 | DONE | Require consent for sensitive social inference/actions | — |
| BT-V4-OBS-001 | DONE | Extend audit logs to social and agent actions | — |
| BT-V4-E2E-001 | PARTIAL | E2E: Friend → Chat → Share entity | — |
| BT-V4-E2E-002 | PARTIAL | E2E: Intent → Opportunity → Place → Activity → Join → Plans | — |
| BT-V4-E2E-003 | DONE | E2E: Organization/Community/Business actor authorization | — |
| BT-V4-E2E-004 | DONE | E2E: Cross-city / Online intent | — |
| BT-V4-REL-001 | TODO | Pass Foundation + Social Alpha release gate | BT-V4-E2E-001(PARTIAL), BT-V4-E2E-002(PARTIAL), BT-V4-SAF-002(BLOCKED) |
| BT-V4-PIL-001 | BLOCKED | Resolve real IdP/HTTPS/physical-device production login | BT-V4-REL-001(TODO) |
| BT-V4-PIL-002 | BLOCKED | Prepare real CSSA/activity/production map/API/support fallback | BT-V4-REL-001(TODO) |
| BT-V4-PIL-003 | BLOCKED | Pass Aberdeen Closed Pilot release gate | BT-V4-PIL-001(BLOCKED), BT-V4-PIL-002(BLOCKED) |
| BT-V5-INT-001 | DONE | V5 Phase 0：增量映射、认知边界与权威接口 | — |
| BT-V5-AGE-001 | DONE | Agent Profile 基础模型 | — |
| BT-V5-AGE-002 | DONE | Public Profile 与 Private Agent Profile 分离 | — |
| BT-V5-AGE-003 | DONE | Profile Field Visibility | — |
| BT-V5-AGE-004 | DONE | Agent Memory | — |
| BT-V5-AGE-005 | DONE | Memory Evidence | — |
| BT-V5-AGE-007 | PARTIAL | Memory Candidate | — |
| BT-V5-AGE-008 | DONE | Confidence Model | — |
| BT-V5-AGE-011 | DONE | Memory Correction | — |
| BT-V5-AGE-015 | DONE | Agent Seed Onboarding | — |
| BT-V5-AGE-020 | TODO | Moment Enrichment Pipeline | BT-V5-AIR-043(TODO) |
| BT-V5-AGE-021 | TODO | Moment Place Context | BT-V5-AGE-020(TODO) |
| BT-V5-AGE-022 | TODO | Moment Activity Context | BT-V5-AGE-020(TODO) |
| BT-V5-AGE-024 | DONE | Activity Participation Signal | — |
| BT-V5-AGE-027 | PARTIAL | Place Memory | — |
| BT-V5-AGE-031 | DONE | Community Membership Signal | — |
| BT-V5-AGE-033 | DONE | Context Builder | — |
| BT-V5-AGE-034 | DONE | Context Relevance | — |
| BT-V5-AGE-036 | DONE | Current Context | — |
| BT-V5-AGE-037 | DONE | Attention Policy Model | — |
| BT-V5-AGE-038 | DONE | Notification Routing | — |
| BT-V5-AGE-041 | DONE | Social Policy | — |
| BT-V5-AGE-044 | DONE | Autonomy Level | — |
| BT-V5-AIR-040 | DONE | Outbound Action Safety | — |
| BT-V5-AGE-046 | DONE | Agent Profile Page | — |
| BT-V5-AGE-049 | DONE | Agent Privacy Controls | — |
| BT-V5-AGE-060 | DONE | Organization Memory Boundary | — |
| BT-V5-AGE-062 | DONE | Purpose Limitation | — |
| BT-V5-AGE-064 | DONE | Agent Enrichment Events | — |
| BT-V5-AIR-016 | DONE | Async Enrichment | — |
| BT-V5-AGE-066 | DONE | Feature Flag | — |
| BT-V5-AGE-068 | DONE | Profile APIs | — |
| BT-V5-AGE-069 | DONE | Memory APIs | — |
| BT-V5-AGE-070 | DONE | Policy APIs | — |
| BT-V5-AGE-071 | TODO | Unit Tests | BT-V5-AGE-007(PARTIAL) |
| BT-V5-AIR-054 | TODO | Regression Tests（代码/本地；live 另验） | BT-V5-AIR-009(TODO), BT-V5-AIR-043(TODO), BT-V5-AIR-044(TODO), BT-V5-AIR-048(TODO), BT-V5-AIR-050(TODO), BT-V5-AIR-053(TODO) |
| BT-V5-AIR-005 | TODO | Migration Tests | BT-V5-AGE-007(PARTIAL) |
| BT-V5-AIR-048 | TODO | Quality Metrics | BT-V5-AIR-043(TODO), BT-V5-AIR-044(TODO), BT-V5-AGE-007(PARTIAL) |
| BT-V5-AIR-007 | DONE | 统一 Model Gateway 契约 | — |
| BT-V5-AIR-008 | DONE | 能力登记与路由资格验证 | — |
| BT-V5-AIR-009 | TODO | 接入一个已批准的真实推理适配器（代码/本地；live 另验） | — |
| BT-V5-AIR-010 | DONE | 统一超时重试与隐私安全降级 | — |
| BT-V5-AIR-011 | DONE | 预算与请求前数据出口检查 | — |
| BT-V5-AIR-014 | DONE | 事件信封与类型注册 | — |
| BT-V5-AIR-015 | DONE | 事务 outbox 与幂等消费 | — |
| BT-V5-AIR-018 | DONE | 撤权删除取消和过期传播 | — |
| BT-V5-AIR-022 | DONE | 读取 AGE 权威数据与最小上下文 | — |
| BT-V5-AIR-023 | DONE | 主体权限过滤与上下文快照 | — |
| BT-V5-AIR-027 | DONE | Prompt 与 schema 版本登记 | — |
| BT-V5-AIR-028 | DONE | 结构化输出验证与注入隔离 | — |
| BT-V5-AIR-036 | DONE | 受限 Planner 和类型化提案 | — |
| BT-V5-AIR-037 | DONE | Tool Registry 和确定性许可判定 | — |
| BT-V5-AIR-043 | TODO | 文字证据到 MemoryCandidate | BT-V5-AIR-009(TODO), BT-V5-AGE-007(PARTIAL) |
| BT-V5-AIR-044 | TODO | 受控候选确认与删除闭环 | BT-V5-AIR-043(TODO) |
| BT-V5-AIR-050 | TODO | Flutter 候选审阅和Agent控制 | BT-V5-AIR-043(TODO), BT-V5-AIR-044(TODO) |
| BT-V5-AIR-053 | TODO | 默认关闭和内部受控发布 | BT-V5-AIR-044(TODO), BT-V5-AIR-048(TODO), BT-V5-AIR-050(TODO) |
| BT-V5-AIR-009-LIVE | BLOCKED | 接入一个已批准的真实推理适配器：独立live验收 | BT-V5-AIR-009(TODO) |
| BT-V5-AIR-054-LIVE | BLOCKED | 交付 P0 文本闭环与真实证据：独立live验收 | BT-V5-AIR-054(TODO), BT-V5-AIR-009-LIVE(BLOCKED) |
| BT-FIX-CITY-001 | DONE | City Seed 活动审核发布的主办方兼容修复 | — |
| BT-FIX-NOW-UI-001 | PARTIAL | 录屏消费者交互回归：运行/选城/Now面板/查询/焦点/身份入口/私人草稿 | — |
| BT-FIX-INT-DRAFT-001 | DONE | 录屏私人意图回归：渐进同一草稿、真实回执与未知结果安全 | — |
| BT-FIX-AGENT-PUBLIC-001 | DONE | 录屏实测回归：匿名特殊查询真实身份传递与澄清响应 | — |

## 原 Functional MVP：45项

DONE39 / PARTIAL0 / TODO3 / BLOCKED3

| ID | 优先级 | 状态 | 原任务标题 | 依赖 |
|---|---|---|---|---|
| BT-AUD-001 | P0 | DONE | Create functional MVP gap audit | — |
| BT-RUN-001 | P0 | DONE | Fix physical-device API connectivity and environment config | BT-AUD-001 |
| BT-RUN-002 | P0 | DONE | Add request tracing and debug diagnostics | BT-RUN-001 |
| BT-DAT-001 | P0 | DONE | Finalize core schema for Organization Activity Place Participation | BT-AUD-001 |
| BT-DAT-002 | P0 | DONE | Create development seed fixtures through real database path | BT-DAT-001 |
| BT-AUT-001 | P0 | DONE | Ensure real authentication session reaches backend | BT-RUN-001 |
| BT-ORG-001 | P0 | DONE | Implement organization membership and admin authorization | BT-DAT-001, BT-AUT-001 |
| BT-ACT-001 | P0 | DONE | Implement Activity create/edit/publish/cancel APIs | BT-ORG-001, BT-DAT-001 |
| BT-ORG-002 | P0 | DONE | Build minimal Organization Console activity publisher | BT-ACT-001 |
| BT-SRC-001 | P0 | DONE | Implement real nearby Activity search | BT-ACT-001, BT-DAT-002 |
| BT-PUL-001 | P0 | DONE | Implement Area Pulse API from real data | BT-SRC-001 |
| BT-NOW-001 | P0 | DONE | Connect Now Workspace to real Area Pulse and Activity data | BT-PUL-001, BT-SRC-001, BT-RUN-002 |
| BT-MAP-001 | P0 | DONE | Stabilize persistent map and keyboard lifecycle | BT-NOW-001 |
| BT-MAP-002 | P0 | DONE | Implement stable semantic marker system and selection persistence | BT-MAP-001 |
| BT-MAP-003 | P1 | DONE | Implement explicit Search this area flow | BT-MAP-002, BT-SRC-001 |
| BT-NOW-002 | P1 | DONE | Implement Local Pulse and Active Intent as separate UI models | BT-NOW-001, BT-MAP-003 |
| BT-AGT-001 | P0 | DONE | Implement normalized MVP intent parser/router | BT-SRC-001, BT-AUT-001 |
| BT-AGT-002 | P0 | DONE | Implement AgentResponse and ResultSet contract | BT-AGT-001 |
| BT-AGT-003 | P0 | DONE | Connect Now composer to real Agent query flow | BT-AGT-002, BT-NOW-001 |
| BT-AGT-004 | P0 | DONE | Persist conversation task context for follow-ups | BT-AGT-003 |
| BT-NOW-003 | P1 | DONE | Split EntityPeekCard from AgentResultsSheet | BT-MAP-002, BT-AGT-003 |
| BT-RSV-001 | P0 | DONE | Implement RSVP persistence and Activity detail primary action | BT-ACT-001, BT-AUT-001 |
| BT-PLN-001 | P0 | DONE | Implement Plans from real participation/save data | BT-RSV-001 |
| BT-SAV-001 | P1 | DONE | Implement Save independent from RSVP | BT-AUT-001, BT-ACT-001 |
| BT-DET-001 | P0 | DONE | Complete consumer-grade Activity Detail page | BT-RSV-001 |
| BT-NTF-001 | P1 | DONE | Implement critical activity notifications | BT-RSV-001, BT-ACT-001 |
| BT-INB-001 | P1 | DONE | Build Inbox notification center | BT-NTF-001 |
| BT-ORG-003 | P1 | DONE | Build verified Organization profile and public activity list | BT-ACT-001 |
| BT-OAG-001 | P1 | DONE | Implement organization verified FAQ knowledge | BT-ORG-003, BT-ORG-002 |
| BT-ANA-001 | P1 | DONE | Implement pilot funnel analytics | BT-DET-001, BT-RSV-001 |
| BT-SAF-001 | P0 | DONE | Enforce map privacy boundary | BT-MAP-002 |
| BT-SAF-002 | P1 | DONE | Add report/support entry points and audit log baseline | BT-AUT-001 |
| BT-PER-001 | P0 | DONE | Protect async flows from stale responses | BT-AGT-003, BT-MAP-003 |
| BT-PER-002 | P1 | DONE | Profile Now hot paths and remove avoidable jank | BT-MAP-001, BT-NOW-003 |
| BT-TST-001 | P0 | DONE | Create end-to-end vertical-slice integration test/checklist | BT-ORG-002, BT-AGT-004, BT-DET-001, BT-PLN-001 |
| BT-REL-001 | P0 | BLOCKED | Pass closed-pilot release gate | BT-TST-001, BT-SAF-001, BT-PER-001, BT-RUN-002, BT-RUN-003, BT-AUT-002, BT-ORG-004, BT-MAP-004, BT-NTF-002, BT-TST-002 |
| BT-RUN-003 | P0 | DONE | Centralize environment configuration and reject release loopback API | BT-RUN-001 |
| BT-AUT-002 | P0 | BLOCKED | Complete verified native sign-in path and production auth boundary | BT-AUT-001, BT-RUN-003 |
| BT-ORG-004 | P0 | DONE | Complete membership invite, role and revoke lifecycle | BT-ORG-001 |
| BT-MAP-004 | P0 | DONE | Show public Organization markers only from explicit location data | BT-MAP-002, BT-ORG-003 |
| BT-NTF-002 | P0 | DONE | Make starts-soon reminders durable across service downtime | BT-NTF-001 |
| BT-TST-002 | P0 | BLOCKED | Run real closed-pilot staging E2E and verify external dependencies | BT-TST-001, BT-RUN-003, BT-AUT-002, BT-ORG-004, BT-MAP-004, BT-NTF-002 |
| BT-PIL-001 | P1 | TODO | Prepare CSSA verified profile and one-event pilot | BT-REL-001, BT-ORG-003, BT-ANA-001 |
| BT-PIL-002 | P1 | TODO | Run CSSA pilot and produce post-event learning report | BT-PIL-001 |
| BT-POL-001 | P2 | TODO | Finalize visual motion and micro-interaction polish | BT-REL-001 |

## Community Social Layer：12项

DONE12 / PARTIAL0 / TODO0 / BLOCKED0

| ID | 优先级 | 状态 | 原任务标题 | 依赖 |
|---|---|---|---|---|
| BT-COM-001 | P1 | DONE | Community domain specification | — |
| BT-COM-002 | P1 | DONE | Community database and membership model | BT-COM-001 |
| BT-COM-003 | P1 | DONE | Activity organizer abstraction | BT-COM-002 |
| BT-COM-004 | P1 | DONE | Community authorization | BT-COM-003 |
| BT-COM-005 | P1 | DONE | Community API | BT-COM-004 |
| BT-COM-006 | P1 | DONE | Activity visibility and organizer permissions | BT-COM-005 |
| BT-COM-007 | P1 | DONE | Flutter Community UI | BT-COM-006 |
| BT-COM-008 | P1 | DONE | Activity organizer UI | BT-COM-007 |
| BT-COM-009 | P1 | DONE | Community synthetic seed and E2E | BT-COM-008 |
| BT-COM-010 | P1 | DONE | Community regression verification | BT-COM-009 |
| BT-COM-011 | P1 | DONE | Community physical-device verification | BT-COM-010 |
| BT-COM-012 | P1 | DONE | Community completion report | BT-COM-011 |

## V4：87项

DONE70 / PARTIAL5 / TODO7 / BLOCKED5

| ID | 优先级 | 状态 | 原任务标题 | 依赖 |
|---|---|---|---|---|
| BT-V4-AUD-001 | P0 | DONE | Snapshot current repository and running-task state | — |
| BT-V4-AUD-002 | P0 | DONE | Reconcile V3 backlog, Community tasks, and latest 39/3/3 status | BT-V4-AUD-001 |
| BT-V4-CAN-001 | P0 | DONE | Publish BirdTie Canonical Product Spec V4 | BT-V4-AUD-002 |
| BT-V4-ADR-001 | P0 | DONE | Create ADR for ActorRef/PrincipalRef, Agent roles, Context, Place/Venue/Business | BT-V4-CAN-001 |
| BT-V4-MIG-001 | P0 | DONE | Create non-destructive V4 schema/API migration plan | BT-V4-ADR-001 |
| BT-V4-AUT-001 | P0 | DONE | Upgrade autonomous Codex queue to V4 | BT-V4-AUD-002, BT-V4-CAN-001 |
| BT-V4-TST-001 | P0 | DONE | Lock existing vertical-slice regression baseline | BT-V4-AUD-002 |
| BT-V4-ACT-001 | P0 | DONE | Introduce ActorRef/PrincipalRef domain abstraction | BT-V4-ADR-001, BT-V4-MIG-001 |
| BT-V4-AGF-001 | P0 | DONE | Create shared Agent Runtime with role/capability packs | BT-V4-ACT-001 |
| BT-V4-AGF-002 | P0 | DONE | Normalize Agent ownership and identity boundary | BT-V4-AGF-001 |
| BT-V4-CTX-001 | P0 | DONE | Replace city-root assumptions with generic Context Graph semantics | BT-V4-ADR-001 |
| BT-V4-CTX-002 | P1 | DONE | Support current, historical and destination contexts | BT-V4-CTX-001 |
| BT-V4-PRV-001 | P0 | DONE | Define Agent context-access policy | BT-V4-AGF-002, BT-V4-CTX-001 |
| BT-V4-TIE-001 | P0 | DONE | Implement persistent Connection/Friend requests | BT-V4-ACT-001, BT-V4-PRV-001 |
| BT-V4-TIE-002 | P0 | DONE | Implement connection lifecycle, remove and block | BT-V4-TIE-001 |
| BT-V4-FOL-001 | P1 | DONE | Implement asymmetric Follow relationships | BT-V4-ACT-001, BT-V4-PRV-001 |
| BT-V4-TIE-003 | P1 | DONE | Expose mutual connections and shared-context signals | BT-V4-TIE-001, BT-V4-CTX-002 |
| BT-V4-CHT-001 | P0 | DONE | Create canonical Conversation/Message model | BT-V4-TIE-001, BT-V4-PRV-001 |
| BT-V4-CHT-002 | P0 | DONE | Implement 1:1 Chat for connected users | BT-V4-CHT-001, BT-V4-TIE-001 |
| BT-V4-CHT-003 | P0 | DONE | Share BirdTie entities into Chat | BT-V4-CHT-002, BT-V4-PLC-001, BT-V4-BIZ-001, BT-V4-MOM-001 |
| BT-V4-CHT-004 | P1 | DONE | Implement Activity conversation | BT-V4-CHT-001 |
| BT-V4-CHT-005 | P1 | DONE | Implement Community conversation baseline | BT-V4-CHT-001, BT-V4-COMM-001 |
| BT-V4-PRV-002 | P0 | DONE | Enforce social messaging and discovery privacy controls | BT-V4-TIE-002, BT-V4-CHT-002 |
| BT-V4-SOC-001 | P1 | DONE | Build relationship context for Personal Agent | BT-V4-TIE-001, BT-V4-CHT-002 |
| BT-V4-INT-001 | P0 | DONE | Create first-class social Intent entity | BT-V4-CTX-001, BT-V4-TIE-001 |
| BT-V4-INT-002 | P0 | DONE | Support IN_PERSON / ONLINE / HYBRID intent modality | BT-V4-INT-001 |
| BT-V4-INT-003 | P0 | DONE | Implement Intent audience and visibility policy | BT-V4-INT-001, BT-V4-PRV-002 |
| BT-V4-INT-004 | P0 | DONE | Implement Intent lifecycle and expiry | BT-V4-INT-001 |
| BT-V4-INT-005 | P0 | DONE | Bridge AgentTask intent parsing into first-class Intent | BT-V4-INT-001, BT-V4-AUD-002 |
| BT-V4-OPP-001 | P0 | DONE | Implement Opportunity Engine v1 | BT-V4-INT-003, BT-V4-CTX-001, BT-V4-PLC-001 |
| BT-V4-OPP-002 | P0 | DONE | Make routing Tie-aware | BT-V4-OPP-001, BT-V4-TIE-001 |
| BT-V4-OPP-003 | P1 | DONE | Add privacy-safe new-people routing | BT-V4-OPP-001, BT-V4-PRV-002 |
| BT-V4-OPP-004 | P1 | DONE | Expose explainable opportunity reasons | BT-V4-OPP-001 |
| BT-V4-AGA-001 | P1 | DONE | Define Agent-to-Agent permission contract | BT-V4-PRV-001, BT-V4-AGF-001 |
| BT-V4-AGA-002 | P2 | TODO | Implement bounded Agent-to-Agent social coordination | BT-V4-AGA-001, BT-V4-OPP-002 |
| BT-V4-PLC-001 | P0 | DONE | Promote Place to canonical social-context entity | BT-V4-CTX-001, BT-V4-MIG-001 |
| BT-V4-VEN-001 | P0 | DONE | Add Venue capability model | BT-V4-PLC-001 |
| BT-V4-BIZ-001 | P0 | DONE | Add Business principal and Place/Venue ownership relations | BT-V4-ACT-001, BT-V4-VEN-001 |
| BT-V4-BIZ-002 | P1 | PARTIAL | Add Business Agent capability pack | BT-V4-BIZ-001, BT-V4-AGF-001 |
| BT-V4-PLC-002 | P1 | DONE | Add semantic Place Profile | BT-V4-PLC-001 |
| BT-V4-PLC-003 | P0 | DONE | Implement Intent-to-Place matching v1 | BT-V4-INT-002, BT-V4-PLC-001, BT-V4-VEN-001 |
| BT-V4-PLC-004 | P0 | DONE | Normalize Activity ↔ Place/Venue relation | BT-V4-PLC-001, BT-V4-VEN-001 |
| BT-V4-PLC-005 | P0 | PARTIAL | Build/upgrade Place Detail page | BT-V4-PLC-001 |
| BT-V4-MOM-001 | P0 | DONE | Link Moment to Place/Activity/Community context | BT-V4-PLC-001, BT-V4-COMM-001 |
| BT-V4-MOM-002 | P1 | DONE | Aggregate Place Memory | BT-V4-MOM-001, BT-V4-PLC-002 |
| BT-V4-BIZ-003 | P1 | DONE | Implement Business Claim + Console Lite | BT-V4-BIZ-001 |
| BT-V4-BIZ-004 | P1 | DONE | Support verified availability/booking-link metadata | BT-V4-BIZ-003 |
| BT-V4-BIZ-005 | P2 | TODO | Add native reservation contract | BT-V4-BIZ-004 |
| BT-V4-BIZ-006 | P2 | TODO | Enable Business Agent reservation negotiation | BT-V4-BIZ-002, BT-V4-BIZ-005, BT-V4-AGA-001 |
| BT-V4-ADS-001 | P1 | DONE | Separate organic recommendation from sponsored opportunity | BT-V4-BIZ-001, BT-V4-OPP-001 |
| BT-V4-COMM-001 | P0 | DONE | Reconcile current Community implementation against canonical model | BT-V4-AUD-002, BT-V4-ACT-001 |
| BT-V4-ACTY-001 | P0 | DONE | Generalize Activity organizer to ActorRef | BT-V4-ACT-001, BT-V4-COMM-001, BT-V4-BIZ-001 |
| BT-V4-ACTY-002 | P0 | DONE | Normalize Activity modality and visibility | BT-V4-ACTY-001, BT-V4-INT-002 |
| BT-V4-ORG-001 | P1 | DONE | Migrate Organization Agent to shared capability framework | BT-V4-AGF-001, BT-V4-AUD-002 |
| BT-V4-COMM-002 | P1 | DONE | Build Community discovery and durable membership UX | BT-V4-COMM-001 |
| BT-V4-ORG-002 | P0 | DONE | Unify actor-specific activity ownership authorization | BT-V4-ACTY-001 |
| BT-V4-ORG-003 | P1 | DONE | Normalize verified public profiles for organizations and businesses | BT-V4-BIZ-003, BT-V4-ORG-001 |
| BT-V4-NOW-001 | P0 | DONE | Refactor Now into context-aware social workspace | BT-V4-OPP-001, BT-V4-CTX-001, BT-V4-TST-001 |
| BT-V4-MAP-001 | P0 | DONE | Add typed map layers for Opportunities/Activities/Places/Moments/Org/Business | BT-V4-NOW-001, BT-V4-PLC-001, BT-V4-MOM-001 |
| BT-V4-MAP-002 | P0 | PARTIAL | Preserve existing map lifecycle/performance guarantees after V4 layers | BT-V4-MAP-001 |
| BT-V4-NOW-002 | P0 | DONE | Add Active Intent model to Now | BT-V4-INT-004, BT-V4-NOW-001 |
| BT-V4-NOW-003 | P1 | DONE | Add My Network / Social Now surface | BT-V4-TIE-001, BT-V4-OPP-002 |
| BT-V4-NOW-004 | P0 | DONE | Generalize Agent ResultSet/entity cards to V4 entity types | BT-V4-NOW-001, BT-V4-ACT-001 |
| BT-V4-NOW-005 | P0 | DONE | Implement non-map flow for online intents | BT-V4-INT-002, BT-V4-NOW-001 |
| BT-V4-NOW-006 | P1 | DONE | Add current/destination context switching | BT-V4-CTX-002, BT-V4-NOW-001 |
| BT-V4-ACTN-001 | P0 | DONE | Normalize entity actions across BirdTie | BT-V4-NOW-004, BT-V4-CHT-003 |
| BT-V4-PLN-001 | P0 | DONE | Integrate Intent/Activity/Place with Plans | BT-V4-INT-004, BT-V4-PLC-004, BT-V4-ACTN-001 |
| BT-V4-NOT-001 | P1 | DONE | Add meaningful social/opportunity notifications | BT-V4-OPP-002, BT-V4-TIE-001 |
| BT-V4-ACTN-002 | P1 | BLOCKED | Suggest Connection after shared real activity | BT-V4-TIE-001, BT-V4-PLN-001 |
| BT-V4-ACTN-003 | P1 | DONE | Persist shared activity/context history | BT-V4-TIE-001, BT-V4-PLN-001 |
| BT-V4-NAV-001 | P1 | TODO | Normalize Navigate action from Place/Activity | BT-V4-PLC-005, BT-V4-ACTN-001 |
| BT-V4-BKG-001 | P1 | DONE | Expose booking action when verified booking path exists | BT-V4-BIZ-004, BT-V4-ACTN-001 |
| BT-V4-SAF-001 | P0 | DONE | Re-run and extend public precise-location privacy boundary | BT-V4-MAP-001, BT-V4-OPP-003 |
| BT-V4-SAF-002 | P0 | BLOCKED | Implement relationship/intent/profile visibility matrix | BT-V4-PRV-001, BT-V4-INT-003, BT-V4-FOL-001 |
| BT-V4-SAF-003 | P0 | DONE | Unify block/report/support across social entities | BT-V4-TIE-002, BT-V4-CHT-002 |
| BT-V4-SAF-004 | P0 | DONE | Require consent for sensitive social inference/actions | BT-V4-SOC-001, BT-V4-AGA-001 |
| BT-V4-OBS-001 | P0 | DONE | Extend audit logs to social and agent actions | BT-V4-ACTN-001, BT-V4-AGF-001 |
| BT-V4-E2E-001 | P0 | PARTIAL | E2E: Friend → Chat → Share entity | BT-V4-CHT-003, BT-V4-TIE-001 |
| BT-V4-E2E-002 | P0 | PARTIAL | E2E: Intent → Opportunity → Place → Activity → Join → Plans | BT-V4-OPP-002, BT-V4-PLC-003, BT-V4-PLN-001 |
| BT-V4-E2E-003 | P0 | DONE | E2E: Organization/Community/Business actor authorization | BT-V4-ORG-002, BT-V4-BIZ-001, BT-V4-COMM-001 |
| BT-V4-E2E-004 | P0 | DONE | E2E: Cross-city / Online intent | BT-V4-NOW-005, BT-V4-CTX-001 |
| BT-V4-REL-001 | P0 | TODO | Pass Foundation + Social Alpha release gate | BT-V4-E2E-001, BT-V4-E2E-002, BT-V4-E2E-003, BT-V4-E2E-004, BT-V4-SAF-001, BT-V4-SAF-002 |
| BT-V4-PIL-001 | P0 | BLOCKED | Resolve real IdP/HTTPS/physical-device production login | BT-V4-REL-001 |
| BT-V4-PIL-002 | P0 | BLOCKED | Prepare real CSSA/activity/production map/API/support fallback | BT-V4-REL-001 |
| BT-V4-PIL-003 | P0 | BLOCKED | Pass Aberdeen Closed Pilot release gate | BT-V4-PIL-001, BT-V4-PIL-002 |
| BT-V4-ANA-001 | P1 | TODO | Add social-coordination pilot metrics | BT-V4-REL-001 |
| BT-V4-PIL-004 | P1 | TODO | Run CSSA + user social pilot learning loop | BT-V4-PIL-003, BT-V4-ANA-001 |

## V5 增量接入：1项

DONE1 / PARTIAL0 / TODO0 / BLOCKED0

| ID | 优先级 | 状态 | 原任务标题 | 依赖 |
|---|---|---|---|---|
| BT-V5-INT-001 | P0 | DONE | V5 Phase 0：增量映射、认知边界与权威接口 | BT-V4-AUD-001, BT-V4-AUD-002, BT-V4-CAN-001, BT-V4-SAF-004 |

## V5 AGE：52项

DONE36 / PARTIAL4 / TODO11 / BLOCKED1

| ID | 优先级 | 状态 | 原任务标题 | 依赖 |
|---|---|---|---|---|
| BT-V5-AGE-001 | P0 | DONE | Agent Profile 基础模型 | BT-V4-ACT-001, BT-V4-AGF-002, BT-V5-INT-001 |
| BT-V5-AGE-002 | P0 | DONE | Public Profile 与 Private Agent Profile 分离 | BT-V5-AGE-001, BT-V4-PRV-001, BT-V5-INT-001 |
| BT-V5-AGE-003 | P0 | DONE | Profile Field Visibility | BT-V5-AGE-002, BT-V4-COMM-001, BT-V4-TIE-001, BT-V5-INT-001 |
| BT-V5-AGE-004 | P0 | DONE | Agent Memory | BT-V5-AGE-001, BT-V5-AGE-003, BT-V5-INT-001 |
| BT-V5-AGE-005 | P0 | DONE | Memory Evidence | BT-V5-AGE-004, BT-V5-INT-001 |
| BT-V5-AGE-007 | P0 | PARTIAL | Memory Candidate | BT-V5-AGE-004, BT-V5-AGE-005, BT-V5-AGE-008, BT-V4-SAF-004, BT-V5-INT-001 |
| BT-V5-AGE-008 | P0 | DONE | Confidence Model | BT-V5-AGE-005, BT-V4-SAF-004, BT-V5-INT-001 |
| BT-V5-AGE-009 | P1 | DONE | Memory Reinforcement | BT-V5-AGE-005, BT-V5-AGE-008, BT-V5-INT-001 |
| BT-V5-AGE-010 | P1 | DONE | Memory Decay | BT-V5-AGE-009, BT-V5-INT-001 |
| BT-V5-AGE-011 | P0 | DONE | Memory Correction | BT-V5-AGE-004, BT-V5-AGE-005, BT-V5-INT-001 |
| BT-V5-AGE-015 | P0 | DONE | Agent Seed Onboarding | BT-V5-AGE-068, BT-V5-INT-001 |
| BT-V5-AGE-018 | P1 | DONE | Social Preference Seed | BT-V5-AGE-015, BT-V5-AGE-070, BT-V5-INT-001 |
| BT-V5-AGE-019 | P1 | DONE | Progressive Completion | BT-V5-AGE-015, BT-V5-AGE-068, BT-V5-INT-001 |
| BT-V5-AGE-020 | P0 | TODO | Moment Enrichment Pipeline | BT-V5-AIR-043, BT-V5-AGE-069, BT-V5-AGE-064, BT-V5-INT-001 |
| BT-V5-AGE-021 | P0 | TODO | Moment Place Context | BT-V5-AGE-020, BT-V4-MOM-001, BT-V5-INT-001 |
| BT-V5-AGE-022 | P0 | TODO | Moment Activity Context | BT-V5-AGE-020, BT-V4-MOM-001, BT-V5-INT-001 |
| BT-V5-AGE-023 | P1 | DONE | Historical Moment | BT-V4-MOM-001, BT-V5-INT-001 |
| BT-V5-AGE-024 | P0 | DONE | Activity Participation Signal | BT-V5-AGE-005, BT-V5-AGE-064, BT-RSV-001, BT-V5-INT-001 |
| BT-V5-AGE-025 | P1 | BLOCKED | Repeated Activity Preference | BT-V5-AGE-009, BT-V5-AGE-024, BT-V5-INT-001 |
| BT-V5-AGE-026 | P1 | TODO | 已核验共同出席到AGE关系证据的适配 | BT-V5-AGE-005, BT-V5-AGE-024, BT-V4-ACTN-002, BT-V4-ACTN-003, BT-V5-INT-001 |
| BT-V5-AGE-027 | P0 | PARTIAL | Place Memory | BT-V5-AGE-005, BT-V5-AGE-064, BT-V4-MOM-001, BT-V4-PLC-001, BT-V5-INT-001 |
| BT-V5-AGE-028 | P1 | DONE | City History | BT-V5-AGE-001, BT-V4-CTX-002, BT-V5-INT-001 |
| BT-V5-AGE-029 | P1 | TODO | Life Map | BT-V5-AGE-027, BT-V5-AGE-028, BT-V5-INT-001 |
| BT-V5-AGE-030 | P2 | TODO | Life Import | BT-V5-AGE-029, BT-V5-AIR-021, BT-V5-INT-001 |
| BT-V5-AGE-031 | P0 | DONE | Community Membership Signal | BT-V5-AGE-005, BT-V4-COMM-001, BT-ORG-004, BT-V5-INT-001, BT-V4-ORG-002 |
| BT-V5-AGE-033 | P0 | DONE | Context Builder | BT-V5-AGE-002, BT-V5-AGE-004, BT-V5-AGE-005, BT-V5-AGE-031, BT-V5-AGE-036, BT-V5-AGE-060, BT-V5-INT-001 |
| BT-V5-AGE-034 | P0 | DONE | Context Relevance | BT-V5-AGE-033, BT-V5-INT-001 |
| BT-V5-AGE-035 | P1 | DONE | Context Budget | BT-V5-AGE-034, BT-V5-AIR-011, BT-V5-INT-001 |
| BT-V5-AGE-036 | P0 | DONE | Current Context | BT-V5-AGE-004, BT-V4-CTX-002, BT-V5-INT-001 |
| BT-V5-AGE-037 | P0 | DONE | Attention Policy Model | BT-V5-AGE-001, BT-V5-INT-001 |
| BT-V5-AGE-038 | P0 | DONE | Notification Routing | BT-V5-AGE-037, BT-INB-001, BT-NTF-002, BT-V5-INT-001 |
| BT-V5-AGE-040 | P1 | PARTIAL | Digest | BT-V5-AGE-038, BT-V5-AIR-019, BT-V5-INT-001 |
| BT-V5-AGE-041 | P0 | DONE | Social Policy | BT-V5-AGE-001, BT-V4-PRV-002, BT-V4-SAF-004, BT-V5-INT-001 |
| BT-V5-AGE-042 | P1 | PARTIAL | Message Request Policy | BT-V5-AGE-041, BT-V5-AIR-037, BT-V5-INT-001 |
| BT-V5-AGE-043 | P1 | DONE | Introduction Policy | BT-V5-AGE-041, BT-V5-AGE-034, BT-V4-OPP-003, BT-V4-OPP-004, BT-V5-INT-001 |
| BT-V5-AGE-044 | P0 | DONE | Autonomy Level | BT-V5-AGE-041, BT-V5-AGE-066, BT-V5-INT-001 |
| BT-V5-AGE-046 | P0 | DONE | Agent Profile Page | BT-V5-AGE-068, BT-V5-AGE-069, BT-V5-AGE-070, BT-V5-INT-001 |
| BT-V5-AGE-047 | P1 | TODO | Memory Center | BT-V5-AGE-046, BT-V5-AGE-069, BT-V5-AIR-050, BT-V5-INT-001 |
| BT-V5-AGE-048 | P1 | TODO | Why This? | BT-V5-AGE-005, BT-V5-AIR-046, BT-V4-OPP-004, BT-V5-INT-001 |
| BT-V5-AGE-049 | P0 | DONE | Agent Privacy Controls | BT-V5-AGE-066, BT-V5-AGE-068, BT-V5-AGE-069, BT-V5-AGE-070, BT-V5-INT-001 |
| BT-V5-AGE-051 | P1 | DONE | Organization Memory | BT-V5-AGE-004, BT-V5-AGE-005, BT-V4-ORG-001, BT-V5-INT-001 |
| BT-V5-AGE-060 | P0 | DONE | Organization Memory Boundary | BT-V5-INT-001, BT-V5-AGE-002, BT-V5-AGE-003, BT-V5-AGE-004, BT-V5-AGE-005, BT-V4-PRV-001, BT-V4-AGF-002 |
| BT-V5-AGE-053 | P1 | DONE | Organization Knowledge Source | BT-V5-AGE-051, BT-V4-ORG-003, BT-V5-INT-001 |
| BT-V5-AGE-056 | P1 | TODO | Business Memory | BT-V5-AGE-004, BT-V5-AGE-005, BT-V4-BIZ-002, BT-V4-BIZ-003, BT-V5-INT-001 |
| BT-V5-AGE-062 | P0 | DONE | Purpose Limitation | BT-V5-AGE-060, BT-V5-AGE-005, BT-V4-SAF-004, BT-V5-INT-001 |
| BT-V5-AGE-064 | P0 | DONE | Agent Enrichment Events | BT-V5-AIR-014, BT-V4-MOM-001, BT-RSV-001, BT-V4-COMM-001, BT-ORG-004, BT-V5-INT-001 |
| BT-V5-AGE-066 | P0 | DONE | Feature Flag | BT-V5-INT-001 |
| BT-V5-AGE-068 | P0 | DONE | Profile APIs | BT-V5-AGE-001, BT-V5-AGE-002, BT-V5-AGE-003, BT-V5-INT-001 |
| BT-V5-AGE-069 | P0 | DONE | Memory APIs | BT-V5-AGE-004, BT-V5-AGE-005, BT-V5-AGE-008, BT-V5-AGE-011, BT-V5-AGE-060, BT-V5-INT-001 |
| BT-V5-AGE-070 | P0 | DONE | Policy APIs | BT-V5-AGE-037, BT-V5-AGE-041, BT-V5-AGE-044, BT-V5-INT-001 |
| BT-V5-AGE-071 | P0 | TODO | Unit Tests | BT-V5-AGE-003, BT-V5-AGE-007, BT-V5-AGE-008, BT-V5-AGE-009, BT-V5-AGE-011, BT-V5-AGE-037, BT-V5-AGE-041, BT-V5-INT-001 |
| BT-V5-AGE-081 | P1 | TODO | Organization / Business Agent Spec | BT-V4-ORG-001, BT-V4-BIZ-002, BT-V5-AGE-051, BT-V5-AGE-056, BT-V5-INT-001 |

## V5 AIR（代码/本地）：50项

DONE19 / PARTIAL4 / TODO27 / BLOCKED0

| ID | 优先级 | 状态 | 原任务标题 | 依赖 |
|---|---|---|---|---|
| BT-V5-AIR-040 | P0 | DONE | Outbound Action Safety | BT-V5-AGE-044, BT-V4-SAF-004, BT-V5-INT-001, BT-V5-AIR-016, BT-V5-AIR-018, BT-V5-AIR-037 |
| BT-V5-AIR-016 | P0 | DONE | Async Enrichment | BT-V5-INT-001, BT-V5-AIR-014, BT-V5-AIR-015, BT-V5-AGE-066 |
| BT-V5-AIR-054 | P0 | TODO | Regression Tests（代码/本地；live 另验） | BT-V5-INT-001, BT-V4-AUD-001, BT-V4-AUD-002, BT-V4-AUT-001, BT-V4-SAF-004, BT-V4-CAN-001, BT-V5-AIR-007, BT-V5-AIR-008, BT-V5-AIR-009, BT-V5-AIR-010, BT-V5-AIR-011, BT-V5-AIR-014, BT-V5-AIR-015, BT-V5-AIR-016, BT-V5-AIR-018, BT-V5-AIR-022, BT-V5-AIR-023, BT-V5-AIR-027, BT-V5-AIR-028, BT-V5-AIR-036, BT-V5-AIR-037, BT-V5-AIR-040, BT-V5-AIR-043, BT-V5-AIR-044, BT-V5-AIR-048, BT-V5-AIR-050, BT-V5-AIR-053, BT-V5-AGE-066, BT-V5-AGE-069 |
| BT-V5-AIR-005 | P0 | TODO | Migration Tests | BT-V5-AGE-004, BT-V5-AGE-005, BT-V5-AGE-007, BT-V5-AGE-011, BT-V5-AIR-015, BT-V5-AIR-016, BT-V5-AIR-040, BT-V5-INT-001 |
| BT-V5-AIR-049 | P1 | DONE | Enrichment Metrics | BT-V5-AGE-004, BT-V5-AGE-064, BT-V5-INT-001, BT-V5-AIR-011, BT-V5-AIR-016, BT-V5-AIR-027 |
| BT-V5-AIR-048 | P0 | TODO | Quality Metrics | BT-V5-INT-001, BT-V5-AIR-028, BT-V5-AIR-037, BT-V5-AIR-040, BT-V5-AIR-043, BT-V5-AIR-044, BT-V5-AGE-060, BT-V5-AGE-007 |
| BT-V5-AIR-007 | P0 | DONE | 统一 Model Gateway 契约 | BT-V5-INT-001 |
| BT-V5-AIR-008 | P0 | DONE | 能力登记与路由资格验证 | BT-V5-AIR-007, BT-V5-INT-001 |
| BT-V5-AIR-009 | P0 | TODO | 接入一个已批准的真实推理适配器（代码/本地；live 另验） | BT-V5-AIR-007, BT-V5-AIR-008, BT-V5-AIR-011, BT-V5-INT-001 |
| BT-V5-AIR-010 | P0 | DONE | 统一超时重试与隐私安全降级 | BT-V5-AIR-007, BT-V5-AIR-008, BT-V5-INT-001 |
| BT-V5-AIR-011 | P0 | DONE | 预算与请求前数据出口检查 | BT-V5-AIR-007, BT-V5-AIR-008, BT-V5-AGE-066, BT-V4-SAF-004, BT-V5-INT-001 |
| BT-V5-AIR-012 | P1 | TODO | 增加第二 provider 和规范历史适配（代码/本地；live 另验） | BT-V5-AIR-009, BT-V5-AIR-010, BT-V5-AIR-011, BT-V5-AIR-027, BT-V5-AIR-028, BT-V5-INT-001 |
| BT-V5-AIR-013 | P2 | TODO | 依据评测优化模型路由和成本 | BT-V5-AIR-012, BT-V5-AIR-048, BT-V5-AIR-049, BT-V5-INT-001 |
| BT-V5-AIR-014 | P0 | DONE | 事件信封与类型注册 | BT-V5-INT-001 |
| BT-V5-AIR-015 | P0 | DONE | 事务 outbox 与幂等消费 | BT-V5-AIR-014, BT-V5-INT-001 |
| BT-V5-AIR-017 | P1 | DONE | 重试死信和人工恢复入口 | BT-V5-AIR-010, BT-V5-AIR-016, BT-V5-INT-001 |
| BT-V5-AIR-018 | P0 | DONE | 撤权删除取消和过期传播 | BT-V5-AIR-011, BT-V5-AIR-015, BT-V5-AIR-016, BT-V5-AGE-011, BT-V5-AGE-066, BT-V5-INT-001 |
| BT-V5-AIR-019 | P1 | DONE | 计划与上下文触发器 | BT-V5-AIR-014, BT-V5-AIR-016, BT-V5-AIR-018, BT-V5-AIR-040, BT-V5-INT-001 |
| BT-V5-AIR-020 | P1 | PARTIAL | 并发顺序和事件风暴控制 | BT-V5-AIR-015, BT-V5-AIR-016, BT-V5-AIR-018, BT-V5-INT-001 |
| BT-V5-AIR-021 | P2 | TODO | 大型历史导入批处理 | BT-V5-AIR-017, BT-V5-AIR-020, BT-V5-AIR-030, BT-V5-AIR-035, BT-V5-AIR-044, BT-V5-INT-001 |
| BT-V5-AIR-022 | P0 | DONE | 读取 AGE 权威数据与最小上下文 | BT-V5-INT-001, BT-V5-AIR-007, BT-V5-AGE-033 |
| BT-V5-AIR-023 | P0 | DONE | 主体权限过滤与上下文快照 | BT-V5-AIR-018, BT-V5-AIR-022, BT-V5-AGE-060, BT-V5-AGE-062, BT-V5-INT-001 |
| BT-V5-AIR-024 | P1 | PARTIAL | 来源证据冲突与事实时间模型 | BT-V5-AIR-022, BT-V5-AIR-023, BT-V5-INT-001 |
| BT-V5-AIR-025 | P1 | TODO | 为既有跨城/线上领域接入AIR最小Context适配 | BT-V5-AIR-023, BT-V5-AIR-024, BT-V5-AGE-033, BT-V5-AGE-036, BT-V4-NOW-005, BT-V4-NOW-006, BT-V4-E2E-004, BT-V4-ACTN-003, BT-V5-INT-001 |
| BT-V5-AIR-026 | P2 | TODO | 可评测的检索和排序升级 | BT-V5-AIR-022, BT-V5-AIR-023, BT-V5-AIR-044, BT-V5-AIR-048, BT-V5-INT-001 |
| BT-V5-AIR-027 | P0 | DONE | Prompt 与 schema 版本登记 | BT-V5-INT-001, BT-V5-AIR-007 |
| BT-V5-AIR-028 | P0 | DONE | 结构化输出验证与注入隔离 | BT-V5-AIR-007, BT-V5-AIR-023, BT-V5-AIR-027, BT-V4-SAF-004, BT-V5-INT-001 |
| BT-V5-AIR-029 | P1 | TODO | Prompt 变更评审和影子回放 | BT-V5-AIR-027, BT-V5-AIR-028, BT-V5-AIR-048, BT-V5-AIR-049, BT-V5-INT-001 |
| BT-V5-AIR-030 | P1 | PARTIAL | 媒体授权与本地出口前过滤 | BT-V5-AIR-011, BT-V5-AIR-018, BT-V5-AIR-023, BT-V5-AIR-028, BT-V5-INT-001 |
| BT-V5-AIR-031 | P1 | TODO | 结构化视觉观察接口（代码/本地；live 另验） | BT-V5-AIR-008, BT-V5-AIR-009, BT-V5-AIR-028, BT-V5-AIR-030, BT-V5-INT-001 |
| BT-V5-AIR-032 | P1 | TODO | 媒体地点和时间证据验证 | BT-V5-AIR-024, BT-V5-AIR-030, BT-V5-AIR-031, BT-V5-INT-001 |
| BT-V5-AIR-033 | P1 | TODO | Moment 富集作业和用户反馈 | BT-V5-AIR-016, BT-V5-AIR-018, BT-V5-AIR-031, BT-V5-AIR-032, BT-V5-AIR-043, BT-V5-AIR-044, BT-V5-AIR-050, BT-V5-INT-001 |
| BT-V5-AIR-034 | P2 | TODO | 视频音频与多帧扩展（代码/本地；live 另验） | BT-V5-AIR-030, BT-V5-AIR-031, BT-V5-AIR-033, BT-V5-AIR-048, BT-V5-INT-001 |
| BT-V5-AIR-035 | P1 | TODO | 富集去重与事实归属 | BT-V5-AIR-024, BT-V5-AIR-031, BT-V5-AIR-043, BT-V5-INT-001 |
| BT-V5-AIR-036 | P0 | DONE | 受限 Planner 和类型化提案 | BT-V5-AIR-016, BT-V5-AIR-023, BT-V5-AIR-028, BT-V5-INT-001 |
| BT-V5-AIR-037 | P0 | DONE | Tool Registry 和确定性许可判定 | BT-V5-AIR-018, BT-V5-AIR-023, BT-V5-AIR-028, BT-V5-AIR-036, BT-V5-AGE-041, BT-V5-AGE-044, BT-V5-INT-001 |
| BT-V5-AIR-038 | P1 | PARTIAL | 写工具幂等与结果对账 | BT-V5-AIR-015, BT-V5-AIR-016, BT-V5-AIR-037, BT-V5-AIR-040, BT-V5-INT-001 |
| BT-V5-AIR-039 | P1 | DONE | 活动人物地点检索适配器 | BT-V5-AIR-022, BT-V5-AIR-023, BT-V5-AIR-037, BT-V5-INT-001 |
| BT-V5-AIR-041 | P1 | TODO | 通知提案和节制触达 | BT-V5-AIR-019, BT-V5-AIR-037, BT-V5-AIR-038, BT-V5-AIR-040, BT-V5-AIR-050, BT-V5-INT-001 |
| BT-V5-AIR-042 | P2 | TODO | 为既有有界A2A接入AIR许可与运行预算适配（保持关闭） | BT-V5-AIR-020, BT-V5-AIR-023, BT-V5-AIR-037, BT-V5-AIR-038, BT-V5-AIR-040, BT-V4-AGA-001, BT-V4-AGA-002, BT-V4-ORG-001, BT-V4-BIZ-002, BT-V5-AGE-060, BT-V5-AGE-062, BT-V5-INT-001 |
| BT-V5-AIR-043 | P0 | TODO | 文字证据到 MemoryCandidate | BT-V5-AIR-009, BT-V5-AIR-011, BT-V5-AIR-016, BT-V5-AIR-018, BT-V5-AIR-023, BT-V5-AIR-027, BT-V5-AIR-028, BT-V5-AGE-005, BT-V5-AGE-007, BT-V5-AGE-008, BT-V5-AGE-069, BT-V5-INT-001 |
| BT-V5-AIR-044 | P0 | TODO | 受控候选确认与删除闭环 | BT-V5-AIR-018, BT-V5-AIR-023, BT-V5-AIR-037, BT-V5-AIR-040, BT-V5-AIR-043, BT-V5-AGE-011, BT-V5-AGE-069, BT-V5-INT-001 |
| BT-V5-AIR-045 | P1 | TODO | 兴趣证据聚合与可校准置信度 | BT-V5-AIR-024, BT-V5-AIR-035, BT-V5-AIR-043, BT-V5-AIR-044, BT-V5-AIR-048, BT-V5-INT-001 |
| BT-V5-AIR-046 | P1 | TODO | 记忆使用授权与公开资料边界 | BT-V5-AIR-023, BT-V5-AIR-044, BT-V5-INT-001 |
| BT-V5-AIR-047 | P1 | TODO | 来源纠错与变更传播 | BT-V5-AIR-018, BT-V5-AIR-024, BT-V5-AIR-044, BT-V5-AIR-045, BT-V5-INT-001 |
| BT-V5-AIR-050 | P0 | TODO | Flutter 候选审阅和Agent控制 | BT-V5-AIR-016, BT-V5-AIR-018, BT-V5-AIR-040, BT-V5-AIR-043, BT-V5-AIR-044, BT-V5-AGE-046, BT-V5-AGE-049, BT-V5-AGE-069, BT-V5-INT-001 |
| BT-V5-AIR-051 | P1 | TODO | 计划审阅及执行结果界面 | BT-V5-AIR-038, BT-V5-AIR-040, BT-V5-AIR-050, BT-V5-INT-001 |
| BT-V5-AIR-052 | P1 | TODO | 全链路故障与质量回归 | BT-V5-AIR-012, BT-V5-AIR-017, BT-V5-AIR-020, BT-V5-AIR-033, BT-V5-AIR-038, BT-V5-AIR-041, BT-V5-AIR-042, BT-V5-AIR-045, BT-V5-AIR-046, BT-V5-AIR-047, BT-V5-AIR-049, BT-V5-AIR-051, BT-V5-INT-001 |
| BT-V5-AIR-053 | P0 | TODO | 默认关闭和内部受控发布 | BT-V5-AIR-011, BT-V5-AIR-016, BT-V5-AIR-018, BT-V5-AIR-037, BT-V5-AIR-044, BT-V5-AIR-048, BT-V5-AIR-050, BT-V5-AGE-066, BT-V5-INT-001 |
| BT-V5-AIR-056 | P2 | TODO | 专业模型和自托管研究门槛 | BT-V5-AIR-013, BT-V5-AIR-026, BT-V5-AIR-048, BT-V5-AIR-049, BT-V5-INT-001 |

## V5 LIVE（外部验收）：5项

DONE0 / PARTIAL0 / TODO0 / BLOCKED5

| ID | 优先级 | 状态 | 原任务标题 | 依赖 |
|---|---|---|---|---|
| BT-V5-AIR-009-LIVE | P0 | BLOCKED | 接入一个已批准的真实推理适配器：独立live验收 | BT-V5-AIR-009 |
| BT-V5-AIR-012-LIVE | P1 | BLOCKED | 增加第二 provider 和规范历史适配：独立live验收 | BT-V5-AIR-012, BT-V5-AIR-009-LIVE |
| BT-V5-AIR-031-LIVE | P1 | BLOCKED | 结构化视觉观察接口：独立live验收 | BT-V5-AIR-031, BT-V5-AIR-009-LIVE |
| BT-V5-AIR-034-LIVE | P2 | BLOCKED | 视频音频与多帧扩展：独立live验收 | BT-V5-AIR-034, BT-V5-AIR-031-LIVE |
| BT-V5-AIR-054-LIVE | P0 | BLOCKED | 交付 P0 文本闭环与真实证据：独立live验收 | BT-V5-AIR-054, BT-V5-AIR-009-LIVE |

## 兼容/录屏修复增量：4项

DONE3 / PARTIAL1 / TODO0 / BLOCKED0

| ID | 优先级 | 状态 | 原任务标题 | 依赖 |
|---|---|---|---|---|
| BT-FIX-CITY-001 | P0 | DONE | City Seed 活动审核发布的主办方兼容修复 | BT-COM-003 |
| BT-FIX-NOW-UI-001 | P0 | PARTIAL | 录屏消费者交互回归：运行/选城/Now面板/查询/焦点/身份入口/私人草稿 | BT-V4-TST-001, BT-V5-INT-001 |
| BT-FIX-INT-DRAFT-001 | P0 | DONE | 录屏私人意图回归：渐进同一草稿、真实回执与未知结果安全 | BT-V4-INT-005, BT-V5-INT-001 |
| BT-FIX-AGENT-PUBLIC-001 | P0 | DONE | 录屏实测回归：匿名特殊查询真实身份传递与澄清响应 | BT-V5-AIR-023, BT-AGT-003 |

## 全部14项PARTIAL：当前缺口与历史原文

以下为队列原partial_reason。旧文字中的“未跑全量”按当时版本理解，后续API02/Client06的覆盖补充见主报告，不能推断整项DONE。

### BT-V4-BIZ-002 — Add Business Agent capability pack

2026-10-07 本地Business过期核验确认原来无反馈已修，真实widget RED1→GREEN1，0POST/重新读取再明确确认唯一1POST；原管理台滚动夹具修正保留14行为。完整Flutter2836及当前API Go12736事件PASS、Go vet/build0和最终Debug安装通过；原生旧契约测试仅夹具纠正、首次失败不删除。尚无真实已核验Business资料/获准AI推理provider/真实经营授权，不能称完整verified AI layer；analyze退出1、真机Business身份/AT/Profile NOT_RUN，整体PARTIAL。详见 work/v5-age038-resume/full-verification-2026-10-07/README.md，Pilot/Beta NO。

### BT-V4-PLC-005 — Build/upgrade Place Detail page

2026-10-02 Android 16 25098PN5AC 真机已用隔离开发库验证地图/Agent 同 Place ID、合成审核 Venue、私密 Moment 登录/退出、分享、无坐标/空活动、断连重试；Flutter 定向 analyze/test 3 PASS，Debug APK 重新构建安装。证据 docs/testing/BIRDTIE-V4-PLACE-DETAIL-DEVICE-REVIEW.md。仍缺真实授权 Venue/Business 经营关系、正式身份和试点环境下的完整设备复核，开发 seed 不是正式试点证据。

### BT-V4-MAP-002 — Preserve existing map lifecycle/performance guarantees after V4 layers

Explicit new two-surface supplement locally implemented/actual physical Place false-retirement+dark contrast fixed; original20IME/Pin proof/history retained. 37targetPASS and complete296-frozen Flutter1123PASS0FAIL-SKIP/analyze-test-Debugbuild0. ExactProfile6 and verifiedDebug38x same296Dart; actual45s keyboard/query/Place stable-body/return/drag recording, darkUI/font2 and original settings restored; correct previewAPK identity+pm exactSHA. Current Debug live and DevToolsHTTP200, browser open queued. Root220 archivebytes verified bc5f593e... at docs/testing/evidence/interaction-quality-2026-10-04/root-sample38z/manifest.json, proof work/v4-map002-quality20261004/root-sample-proof38z.json. PARTIAL supplemental gate: native dark map, AT/low-device/independent user and controlled comparable performance missing; raw before4/after6 display120vs60, nativeGPU/marker-counter/systempresent/latency unmeasured, no performance improvement or global rollout. Phone APIold081 synthetic local4173 not newACTN/native7/production. Original map lifecycle CODE_LOCAL evidence not erased. ClosedPilot/ConsumerBetaNO.

### BT-V4-E2E-001 — E2E: Friend → Chat → Share entity

LOCAL_SYNTHETIC_INSTRUMENTATION_ONLY code/runner complete. Root independently verified worker archive48+manifest06b50f98 and reran pure19 PASS plus automation/verify_social_e2e_local.py --round root-independent1 exit0: two independent native dev Sessions, original Tie/conversation/four messages and Place/Activity stable IDs, two actual API restarts/three owned processes, all46 request IDs/method/status logs match once, old message hash unchanged, anonymity/logout/hidden-source denials and owned DB/process cleanup PASS. Evidence work/v5-age038-resume/parallel-worker-root-proof1.json and work/v4-e2e001-local/root-independent1/result.json. Full original AC not passed: need two actual consenting testers with their own account/App operation and restart evidence, plus one real currently public authorized Place OR organizer-confirmed Activity and source/share authorization. Scripts/seed/Debug/two dev actors are not two real testers or true authorized supply. Restore only with documented actual testers/source plus original App/HTTP instrumented acceptance. Closed Pilot/Consumer Beta NO; no external services/deployment/contact.

### BT-V4-E2E-002 — E2E: Intent → Opportunity → Place → Activity → Join → Plans

Local registered synthetic E2E and two actual API OS restarts implemented/verified only. Worker native3 and root-independent1 each11 PASS/0FAIL-SKIP; test/vet/build+2CLI all0; frozen original880+newtest881 exact SHA, fullpublic/catalog/xmin/085 unuseddown-reapply and ownDBDROP/independentabsence. Root proof work/v5-age038-resume/e2e002-root-proof38bz.json; immutable worker-final1 1043files and root-final1 archive. Native1 four test expectation FAIL retained, original Block/City404 hiding behavior preserved. Full original real-world acceptance NOT_RUN: require currently public authorized Place/Venue plus organizer-confirmed real Activity/source authorization; actual consenting tester own account/current App/registered flow/build/requestIDs and App/API restart evidence. Synthetic approval/going does not prove real supply/attendance; RULE_BASED not live model; joint086 whole/phone/AT not yet verified. Keep original gate, E2E001 PARTIAL, ClosedPilot/ConsumerBeta NO; no external deployment/publishing/contact/service change.

### BT-V5-AGE-007 — Memory Candidate

CODE_LOCAL human candidate slice verified: six explicit activity categories including hiking, native single/multi current-source retention and five-state lifecycle, human exact-version EXPLICIT accept, bodyless original approval metadata recovery and actual native bare-wire transport. Whole10922 Go PASS/0fail-skip with5 exit0; Flutter1534 functional PASS/3 exit0; native receipt12/target007 client309 included in whole. Root proofs work/v5-age038-resume/joint093-whole-root38ly.json,work/v5-age038-resume/joint093-client-root38lw.json,work/v5-age038-resume/profile019-client-archive-root38lu.json,work/v5-age038-resume/worker007-and-client-archive-root38lx.json,work/v5-age038-resume/joint093-whole-archive-root38ly.json. Original probability calibration0.25->0.82, legal representative calibration dataset and INFERRED ACTIVE progression NOT_IMPLEMENTED, cannot use ORDINAL LOW or independent human declaration confidence1 as calibrated evidence. Full007 remains PARTIAL; restore only after lawful data/usage approval, actual calibrated evaluation/threshold and native transition/identity/source/revocation evidence. Old native resolver, CandidateSubmitter, consumer UI and historical metadata gaps are now implemented local slices, no longer current blocker. Exact first failures and raw preserved. Device/performance/TalkBack/OS keystore NOT_RUN while user away; no ADB. Model/real writes/vision/A2A OFF; ClosedPilot/ConsumerBeta NO.

### BT-V5-AGE-027 — Place Memory

原五依赖全部DONE；旧reason中ACTN001/003、PLN001、NOW004/NOW001及CHT003未完成描述已陈旧，这些不是新增原依赖。当前saved、liked、本人口述visited（未核验）和本人private draft Moment地点关联已有真实实现。根只读复核历史whole10797中的206 PlaceMemory PASS/38顶层、11相关Go before/after/copied/current SHA一致，以及历史whole1459中的57功能+4loading PASS/0FAIL-SKIP、7Dart同SHA。原第五信号attended activity at仍只有枚举，无合法source/native producer/revalidation，不能DONE；ACTN002已另记到场来源、确认主体、许可和纠错合同决策待定。报名、Plans、聊天、Moment、时间或GPS不代替出席。保留原AC/deps、全部首失败与历史证据。本轮只读，没有新增功能/测试/真机或外部执行；完整当前093 Go和334客户端仍等待独立增量冻结/全仓复测，旧whole不冒充新帧。证据work/v5-age038-resume/place027-current-original-ac-root38lq.json。Closed Pilot/Consumer Beta NO。

### BT-V5-AGE-040 — Digest

2026-10-07 用户明确停止开发并交接，释放 worker lease。后端公开商家更新106来源/审核发布事务、Follow当前资格、原099摘要预算/幂等、Block退休和实际40P01锁序修复已增量实施；跨阶段去重20个成功Go叶场景，不是单次最终全量PASS。188封存证据及19scope源码哈希根代理只读核验；15文件变更保留。客户端消费者未接通：原remote_inbox_source.dart/inbox.dart未实施新公开通知路径；新增两文件实际2成功/4error/2loading、exit1，三条mounted路径未到最终断言。新Dart未格式化/分析，三个已改旧Go测试本片未跑，106仅owned测试库实测、未用于手机/整合库；新全量Go/Flutter、vet/analyze/build、真机/真实身份/运营均NOT_RUN。owned测试库已清理；不撤销历史失败、不标DONE、不解除Gate。证据 docs/testing/evidence/business-public-update-digest-2026-10-07/stopped01/MANIFEST.json SHA94450bbdcabf819f2d813491bb76f4d2f94c572e0bf9f600b69e47454218c7d1；总交接 docs/reports/BIRDTIE-DEVELOPMENT-HANDOFF-2026-10-07.md。

### BT-V5-AGE-042 — Message Request Policy

原103 keyed决定同事务历史及专用原GET/POST返回边界已实测；实际after-encoding Block/policy200泄露RED已修为当前拒绝且原COMMITTED保持。相关20顶层/88父子PASS含10原生顶层39父子，无fail/skip；实际103up/emptydown/reapply/55000 useddown、幂等并发/丢失回执/新pool/最终Session等待已验；自有库清理实证。根核161叶SHA、6精确源、其余1205API输入及unkeyed/generic/DDL逆证。docs/testing/evidence/age042-decision-native-2026-10-07/MANIFEST.json；root proof work/next-integration-checkpoint-2026-10-07/root-worker-review/age042.json。真实IdP、实际OS安全存储跨进程CAS/手机私人操作、完整SCREEN及098矩阵仍未验，整体PARTIAL；本片后whole另行检查点，不复用api01旧全量为当前。Pilot/Beta NO。

### BT-V5-AIR-020 — 并发顺序和事件风暴控制

2026-10-07 已实际修复Preference与Memory原生消费的pgx revision参数错误：两个原谓词仅$4::text→$4::bigint::text，权限/来源CAS/TTL/原预算字节保持。原生configured/clear两阶段、根6次/depth1、真实4全局1owner/5租户公平、30秒自然过期行锁/进程退出后原CLI恢复、102空库/旧历史down-reapply/used-down拒绝分阶段12唯一父子事件（8顶层）有PASS，原始失败与容量夹具错误保留，不拼成一次整体PASS。根独立核122证据叶、5当前源SHA、1200原API复制输入及两cast精确inverse、canonical原prefix，24自有库不存在SQL/退出0。证据docs/testing/evidence/preference-invalidation-native-2026-10-07/MANIFEST.json与work/next-integration-checkpoint-2026-10-07/root-worker-review/air020.json。当前API完整test正在运行，vet/build/API build0；本片仅Memory两阶段补回归，完整Memory配额/重启压力、通用13事件认知/真实模型、PG Commit应答网络故障与运营负载仍未完成。整体PARTIAL，不沿用历史全量结果为当前PASS，ClosedPilot/ConsumerBeta NO。

### BT-V5-AIR-024 — 来源证据冲突与事实时间模型

原本人同快照SelfReview后端已由原记忆页实际消费：固定preferredActivityTypes+当前memoryID+policy[]，严格owner/Agent/selected object/version/summary/native times/claims/conflict双方短read，未知confidence不补；原更正/preview/journal/confirm独立，无自动winner/写/模型或新导航。真实缺入口RED1；67新单位首次66PASS/1新fixture编码失败exit1，仅修fixture+精确1PASS，不重67；原控制1与旧caption两直接场景PASS，去重70，不冒充一次最终整命令PASS。root核10源72证据/原9wire+index/26readonly/414静态逆3delta，API只新suffix/controller只隔离读追加，旧test只1共享文本反还原与2canonical原字节prefix，不重跑。caption启动regex255/0测试保留，非业务失败。直接Page身份与来源/ABA/迟到/期限和320双主题48dp局部units已验；Settings父route切源下一帧前dispatch集成仍NOT_RUN，当前真实PG/session/ACL/ABA、媒体/vision/外部供给/IdP/手机/AT/性能/whole/analyze/build仍未验或未实现。整体AIR024 PARTIAL，ClosedPilot/Beta NO。
2026-10-07 跨原UI001证据映射，非新实现/测试：旧Settings父route来源切换下一帧前dispatch NOT_RUN已由实际后续freeze14守卫及根current14全量覆盖。根独立核12审计叶、current14 raw SHA，testID2986为1个widget testDone成功/0skip，内部9组有成对Start/Pass（client/base×read/preview/confirm六负向、503控制、确认核实协议、晚回/ABA）。当前四个关键整文件与实际执行帧SHA相同；五段guard与旧freeze14仅归一化片段相同，不能称旧整文件全同。实际API首次send行476，审计正文480属旧行号笔误。本轮零源码改变/零新测试，0增加DONE；原024其余媒体/原生权限/真实供给/设备/AT/性能独立未验，整体PARTIAL，Pilot/Beta NO。

### BT-V5-AIR-030 — 媒体授权与本地出口前过滤

原私人图片consumer新增用户主动本机文字检查→可能邮箱/电话区域→勾选检查→具体确认→复用原实色mask像素；Android实际MethodChannel/Tesseract4Android4.9.0 standard及官方immutable中英模型随包接线，严格request/hash/确定性采样尺寸/文本rect边界/图片与主体代际，单在途与合作式取消；其他平台真实Unavailable/人工路径，UNKNOWN或USER_MASKED不称安全，无自动mask/upload/save/model/Memory。原入口缺失1业务RED exit1，green02新15行为13PASS/2widget夹具FAIL保留exit1，widget03未启动255保留；仅两夹具标准滚动命中修正widget04 2PASS exit0，未改产品/未重跑13或旧52。15unique均有PASS证据，不称最终整组一次PASS；21最终source/assets SHA、66证据、13readonly/旧tests及canonical原prefix核验，根无单位重跑。平台mock与合成图片不等于原生OCR/真实照片；Gradle/Kotlin/模型打包/原生取消质量/离线网络捕获/CPU/手机/AT/PG100/真实上传认证/全量/分析/build均NOT_RUN，完整PII与Vision授权/provider未实现，原AIR030整体PARTIAL，Pilot/Beta NO。

### BT-V5-AIR-038 — 写工具幂等与结果对账

2026-10-07 原人类私信同操作回执/metadata-only恢复、105真实PG与原权限边界已实现核证；后续成功回执会清掉正在编辑的新草稿的两个实际RED已修，仅原payloadDigest匹配才清输入。25新证据/2源/6readonly和唯一产品hunk原字节inverse根独立核验；新两case+原9共11直接行为PASS。最后当前client04完整2904行为PASS/0fail-skip、analyze0/Debug APK build0；当前api01完整12905父子事件PASS/0fail-skip，vet/build/API与两原CLI build0；专属001–105+3开发seed0，automation93及8结构拒绝检查通过。完整Go运行期间仅独立Dart修复，API1210输入全程不变；客户端504最后全程不变，不称同一整体编译依赖图。证据docs/testing/evidence/full-integration-2026-10-07-api01-client04/MANIFEST.json及docs/testing/evidence/air038-draft-preservation-2026-10-07/MANIFEST.json。Debug包已生成未安装，ADB为空；新105本地调试API3698仅原手机库独立副本，原104库与3697服务未改，device3697→host3698转发尚未执行。RSVP/获批Agent写适配器、真实身份/设备重启SecureStorage跨进程/对方读取/AT/性能/运营仍未完或NOT_RUN，整体PARTIAL，ClosedPilot/ConsumerBeta NO。

### BT-FIX-NOW-UI-001 — 录屏消费者交互回归：运行/选城/Now面板/查询/焦点/身份入口/私人草稿

2026-10-07 用户明确停止开发并交接，释放 worker lease，保留既有历史。当前对话可见性源码 slice 已完成：原错误/缺范围恢复标题优先与最新成功回复滚动 guard；五个相关文件141行为PASS，Client06完整2921行为PASS、analyze/build exit0。实际安装 APK SHA af26447bba8ddb7c869babfc7517b4cbda7a3939f9482dc88ab5c0f6b1fa9999；真机中文周末羽毛球→近一点的呢最新回复、Pin轻卡/拖图保留、真实断连→原重试恢复有PNG证据。当前06五段录屏命令 exit0 但拉取均 exit1（远端文件不存在），动态文件未交付；06设置返回/全32场景、真实身份/性能/AT未完成，不把单个slice称整轮UI验收。证据 docs/testing/evidence/now-conversation-latest-2026-10-07，work/next-integration-checkpoint-2026-10-07/phone06，work/birdtie-handoff-2026-10-07/source-and-evidence-check.json；总交接 docs/reports/BIRDTIE-DEVELOPMENT-HANDOFF-2026-10-07.md。


## 全部14项BLOCKED：准确原因与恢复条件

### BT-REL-001 — Pass closed-pilot release gate

2026-10-01 Closed Pilot Ready NO. All repository-executable P0 code tasks RUN-003/ORG-004/MAP-004/NTF-002 completed with local evidence, but AUT-002 lacks configured real IdP/HTTPS device login and TST-002 real A-H cannot start. No verified organizer/event/location rights, production map/API deployment, named duty/backup channel, independent production reminder schedule/alert/log access, or real-device/restart evidence. Release only after every gate in docs/product/RELEASE-QUALITY-GATES.md and docs/testing/FUNCTIONAL-MVP-E2E.md has actual evidence; no waiver recorded.

### BT-AUT-002 — Complete verified native sign-in path and production auth boundary

Native Android/iOS OIDC callback, secure 5-minute PKCE pending state, API verified account check, secure session restoration and revocation implemented. Flutter analyze PASS, 74 tests PASS, debug APK installed, adb callback Activity route PASS, Go OIDC redirect test PASS, loopback auth boundary script PASS. apps/api /v1/auth/oidc/status remains configured:false: no trusted issuer, client registration/secret, HTTPS API callback, deployed service or real verified user; production provider-device E2E cannot run. Need those exact IdP/deployment inputs and real-device verification before DONE. See docs/research/2026-10-01-native-oidc-boundary-verification.md.

### BT-TST-002 — Run real closed-pilot staging E2E and verify external dependencies

2026-10-01 dependency BT-AUT-002 BLOCKED and real A-H prerequisites absent: verified organizer authority/organization; organizer-confirmed activity and public location rights; configured production IdP and real user; HTTPS API plus deployed DB/log access; valid production Mapbox config; named Birdtie/CSSA duty operators and backup contact. Local seeds, fixed dev code, Debug APK and synthetic map point are not pilot evidence. Resume only after these inputs, then execute docs/testing/FUNCTIONAL-MVP-E2E.md on physical device and capture environment, activity/request IDs, participants, screenshots, restart, notifications and support evidence.

### BT-V4-ACTN-002 — Suggest Connection after shared real activity

Original AC requires shared attendance before optional connection, but real native RSVP/Plans only going/pending/cancelled; no attendance/completion writer or authorization contract. AGENT-PARTICIPATION-SIGNAL-V5 and AGENT-ENRICHMENT-EVENTS-V5 explicitly UNAVAILABLE; going, ended Activity,078 public RSVP,location/Moment/Save cannot fabricate verified attendance. Audit docs/research/BIRDTIE-V4-ACTN-002-AUDIT.md. Need product decision on organizer-verified vs explicitly self-reported source, issuer authority, both disclosure/consent, correction/revocation/current source versions; then implement native writer+permission/idempotency/query and real E2E. User question pending, no answer/default inferred as approval. Original Tie/request reused, never auto-friend. ACTN003 independently derives only currently permitted shared RSVP/explicit public linked Places. This item implementation/E2E NOT_RUN; original AC/gate unchanged, ClosedPilot/ConsumerBeta NO.

### BT-V4-SAF-002 — Implement relationship/intent/profile visibility matrix

2026-10-02 dependency audit: acceptance requires public/followers/connections/close/private server-side profile/intent visibility, but Follow relation/API does not exist and BT-V4-FOL-001 remains TODO (P1/Beta). Existing private/public plus explicit profile grants, Friend Tie, scoped Social Intent and Block tests remain enforced. Added BT-V4-FOL-001 dependency; must first implement Follow lifecycle and explicit Close management, then full API/UI authorization matrix. Do not infer Follow from Friend or Close from a grant. See docs/research/BIRDTIE-V4-SAF-002-AUDIT.md; Social Alpha/Closed Pilot NO.

### BT-V4-PIL-001 — Resolve real IdP/HTTPS/physical-device production login

规划阶段标记 BLOCKED_EXTERNAL；须先满足依赖，并取得真实身份/合作/部署证据后重新审计。

### BT-V4-PIL-002 — Prepare real CSSA/activity/production map/API/support fallback

规划阶段标记 BLOCKED_EXTERNAL；须先满足依赖，并取得真实身份/合作/部署证据后重新审计。

### BT-V4-PIL-003 — Pass Aberdeen Closed Pilot release gate

规划阶段标记 BLOCKED_EXTERNAL；须先满足依赖，并取得真实身份/合作/部署证据后重新审计。

### BT-V5-AGE-025 — Repeated Activity Preference

内部真实接口缺失，NOT_IMPLEMENTED。根核 docs/research/BIRDTIE-V5-AGE-025-AUDIT.md（07695a2ecdcb80967163f69133be4b060b77288aa68cecabc97eaac64665d895）及work/v5-age025 18-source快照：024强制Attendance/StableInterest UNKNOWN且仅当前RSVP；009只有本人EXPLICIT人工来源支持，064 ActivityCompleted SourceUnavailable。恢复须ACTN003及实际writer（可能ACTN002）的可纠错出席source ID/version/current reader，再将Activity category/revision绑定去重与批准支持/用途桥；不能用going/end/Moment/GPS当到场。AGE026不是机械前置，实际依赖提案已保存。仅静态审计，功能测试NOT_RUN，无产品实现或发布；ClosedPilot/ConsumerBeta NO。

### BT-V5-AIR-009-LIVE — 接入一个已批准的真实推理适配器：独立live验收

BLOCKED_EXTERNAL：缺负责人provider/model批准、凭证、费用/地域/保留/出口配置、真实canary证据与外部调用授权。恢复需获准后经过出口/预算/开关的合成非个人场景实测。门槛：PROVIDER_APPROVAL,PROVIDER_CREDENTIALS,SPEND_LIMIT

### BT-V5-AIR-012-LIVE — 增加第二 provider 和规范历史适配：独立live验收

BLOCKED_EXTERNAL：缺负责人provider/model批准、凭证、费用/地域/保留/出口配置、真实canary证据与外部调用授权。恢复需获准后经过出口/预算/开关的合成非个人场景实测。门槛：SECOND_PROVIDER_APPROVAL,PROVIDER_CREDENTIALS,SPEND_LIMIT

### BT-V5-AIR-031-LIVE — 结构化视觉观察接口：独立live验收

BLOCKED_EXTERNAL：缺负责人provider/model批准、凭证、费用/地域/保留/出口配置、真实canary证据与外部调用授权。恢复需获准后经过出口/预算/开关的合成非个人场景实测。门槛：VISION_PROVIDER_APPROVAL

### BT-V5-AIR-034-LIVE — 视频音频与多帧扩展：独立live验收

BLOCKED_EXTERNAL：缺负责人provider/model批准、凭证、费用/地域/保留/出口配置、真实canary证据与外部调用授权。恢复需获准后经过出口/预算/开关的合成非个人场景实测。门槛：MEDIA_PROVIDER_APPROVAL

### BT-V5-AIR-054-LIVE — 交付 P0 文本闭环与真实证据：独立live验收

BLOCKED_EXTERNAL：缺负责人provider/model批准、凭证、费用/地域/保留/出口配置、真实canary证据与外部调用授权。恢复需获准后经过出口/预算/开关的合成非个人场景实测。门槛：LIVE_CANARY_FOR_LIVE_READY


## 全部48项TODO：目标与依赖

### BT-PIL-001 — Prepare CSSA verified profile and one-event pilot

Run smallest truthful partner pilot.

依赖：BT-REL-001, BT-ORG-003, BT-ANA-001

### BT-PIL-002 — Run CSSA pilot and produce post-event learning report

Turn real usage into product decisions and case-study evidence.

依赖：BT-PIL-001

### BT-POL-001 — Finalize visual motion and micro-interaction polish

Polish only after functional reliability is proven.

依赖：BT-REL-001

### BT-V4-AGA-002 — Implement bounded Agent-to-Agent social coordination

Let agents coordinate availability/interest with explicit authorization.

依赖：BT-V4-AGA-001, BT-V4-OPP-002

### BT-V4-BIZ-005 — Add native reservation contract

Prepare in-product booking without coupling social core to one provider.

依赖：BT-V4-BIZ-004

### BT-V4-BIZ-006 — Enable Business Agent reservation negotiation

Let Personal Agent coordinate group venue options after user approval.

依赖：BT-V4-BIZ-002, BT-V4-BIZ-005, BT-V4-AGA-001

### BT-V4-NAV-001 — Normalize Navigate action from Place/Activity

Make real-world movement a first-class action without vendor lock-in.

依赖：BT-V4-PLC-005, BT-V4-ACTN-001

### BT-V4-REL-001 — Pass Foundation + Social Alpha release gate

Make the new model usable before wider feature expansion.

依赖：BT-V4-E2E-001, BT-V4-E2E-002, BT-V4-E2E-003, BT-V4-E2E-004, BT-V4-SAF-001, BT-V4-SAF-002

### BT-V4-ANA-001 — Add social-coordination pilot metrics

Measure whether BirdTie creates meaningful connections and actions, not screen time.

依赖：BT-V4-REL-001

### BT-V4-PIL-004 — Run CSSA + user social pilot learning loop

Validate both organization supply and person-to-person coordination.

依赖：BT-V4-PIL-003, BT-V4-ANA-001

### BT-V5-AGE-020 — Moment Enrichment Pipeline

原 Moment 写链存在；没有 Signal Extractor→Evidence→MemoryCandidate 管线。

依赖：BT-V5-AIR-043, BT-V5-AGE-069, BT-V5-AGE-064, BT-V5-INT-001

### BT-V5-AGE-021 — Moment Place Context

明确 Moment→Place/City 引用已存；没有 evidence 管线，关联不证明本人到访。

依赖：BT-V5-AGE-020, BT-V4-MOM-001, BT-V5-INT-001

### BT-V5-AGE-022 — Moment Activity Context

明确 Moment→Activity 链及授权存在；没有经历 Evidence，报名/关联不等于真实到场。

依赖：BT-V5-AGE-020, BT-V4-MOM-001, BT-V5-INT-001

### BT-V5-AGE-026 — 已核验共同出席到AGE关系证据的适配

已授权共同 going 活动可读；缺真实 attended/shared-history 模型，不能推断亲密。

依赖：BT-V5-AGE-005, BT-V5-AGE-024, BT-V4-ACTN-002, BT-V4-ACTN-003, BT-V5-INT-001

### BT-V5-AGE-029 — Life Map

没有 Life Map 聚合或 UI。

依赖：BT-V5-AGE-027, BT-V5-AGE-028, BT-V5-INT-001

### BT-V5-AGE-030 — Life Import

没有 Life Import；P2 不作 P0 前置。

依赖：BT-V5-AGE-029, BT-V5-AIR-021, BT-V5-INT-001

### BT-V5-AGE-047 — Memory Center

没有 Memory Center 或编辑纠正删除界面。

依赖：BT-V5-AGE-046, BT-V5-AGE-069, BT-V5-AIR-050, BT-V5-INT-001

### BT-V5-AGE-048 — Why This?

中文规则理由与真实当前实体已验；没有 Memory 证据解释，不能把 going 当 attended。

依赖：BT-V5-AGE-005, BT-V5-AIR-046, BT-V4-OPP-004, BT-V5-INT-001

### BT-V5-AGE-056 — Business Memory

没有 Business Memory；营业/菜单/报价/预订等知识不可编造。

依赖：BT-V5-AGE-004, BT-V5-AGE-005, BT-V4-BIZ-002, BT-V4-BIZ-003, BT-V5-INT-001

### BT-V5-AGE-071 — Unit Tests

现context/coordination纯策略真实测试存在；Memory强化/置信/提升/纠正测试未实现，SAF004仍进行。

依赖：BT-V5-AGE-003, BT-V5-AGE-007, BT-V5-AGE-008, BT-V5-AGE-009, BT-V5-AGE-011, BT-V5-AGE-037, BT-V5-AGE-041, BT-V5-INT-001

### BT-V5-AIR-054 — Regression Tests（代码/本地；live 另验）

052默认并发三轮Go、189 Flutter和手机旧链真实已验；AGE增量尚未实施，需每项继续回归。；只读规则活动链可复用；两条文本Memory/沙箱审批P0未实现，live canary缺provider配置；原Closed Pilot保持NO。 当前只验证服务端代码与本地合同，不声称取得供应商批准、真实调用或原 source 全部完成。

依赖：BT-V5-INT-001, BT-V4-AUD-001, BT-V4-AUD-002, BT-V4-AUT-001, BT-V4-SAF-004, BT-V4-CAN-001, BT-V5-AIR-007, BT-V5-AIR-008, BT-V5-AIR-009, BT-V5-AIR-010, BT-V5-AIR-011, BT-V5-AIR-014, BT-V5-AIR-015, BT-V5-AIR-016, BT-V5-AIR-018, BT-V5-AIR-022, BT-V5-AIR-023, BT-V5-AIR-027, BT-V5-AIR-028, BT-V5-AIR-036, BT-V5-AIR-037, BT-V5-AIR-040, BT-V5-AIR-043, BT-V5-AIR-044, BT-V5-AIR-048, BT-V5-AIR-050, BT-V5-AIR-053, BT-V5-AGE-066, BT-V5-AGE-069

### BT-V5-AIR-005 — Migration Tests

001–052旧ID/up/down/reapply已验；AGE增量schema尚无，禁止拿现基线代替新迁移。；既有兼容001–052迁移规范和旧ID证据可复用；AIR最小持久schema尚未实施，P0不能等待本P1。

依赖：BT-V5-AGE-004, BT-V5-AGE-005, BT-V5-AGE-007, BT-V5-AGE-011, BT-V5-AIR-015, BT-V5-AIR-016, BT-V5-AIR-040, BT-V5-INT-001

### BT-V5-AIR-048 — Quality Metrics

没有 Memory正确性/推荐相关度/拒绝率基线或评测集。；已有Go/Flutter安全回归不是AIR40条模型+候选+沙箱效果评测；未建立AIR-S01…14版本化验收集。

依赖：BT-V5-INT-001, BT-V5-AIR-028, BT-V5-AIR-037, BT-V5-AIR-040, BT-V5-AIR-043, BT-V5-AIR-044, BT-V5-AGE-060, BT-V5-AGE-007

### BT-V5-AGE-081 — Organization / Business Agent Spec

现组织/Business主体规范可复用；缺完成的Agent知识/pack/权威Memory规范。

依赖：BT-V4-ORG-001, BT-V4-BIZ-002, BT-V5-AGE-051, BT-V5-AGE-056, BT-V5-INT-001

### BT-V5-AIR-009 — 接入一个已批准的真实推理适配器（代码/本地；live 另验）

没有适配器；真实调用缺provider批准/凭证/费用上限/地域保留决定。可独立先做fake契约，live单列。 当前只验证服务端代码与本地合同，不声称取得供应商批准、真实调用或原 source 全部完成。

依赖：BT-V5-AIR-007, BT-V5-AIR-008, BT-V5-AIR-011, BT-V5-INT-001

### BT-V5-AIR-012 — 增加第二 provider 和规范历史适配（代码/本地；live 另验）

第二provider尚未实现且缺批准/凭证/费用配置；P1不能阻断单provider P0。 当前只验证服务端代码与本地合同，不声称取得供应商批准、真实调用或原 source 全部完成。

依赖：BT-V5-AIR-009, BT-V5-AIR-010, BT-V5-AIR-011, BT-V5-AIR-027, BT-V5-AIR-028, BT-V5-INT-001

### BT-V5-AIR-013 — 依据评测优化模型路由和成本

没有任务质量/成本评测或多provider优化；不为用户训练模型。

依赖：BT-V5-AIR-012, BT-V5-AIR-048, BT-V5-AIR-049, BT-V5-INT-001

### BT-V5-AIR-021 — 大型历史导入批处理

无历史导入/批次/断点/费用预估；P2不作交互P0前置。

依赖：BT-V5-AIR-017, BT-V5-AIR-020, BT-V5-AIR-030, BT-V5-AIR-035, BT-V5-AIR-044, BT-V5-INT-001

### BT-V5-AIR-025 — 为既有跨城/线上领域接入AIR最小Context适配

033多Context/线上找伙伴明确约束已有；当前活动查询仍City UI为主，NOW005/E2E004与广泛推荐未完成。

依赖：BT-V5-AIR-023, BT-V5-AIR-024, BT-V5-AGE-033, BT-V5-AGE-036, BT-V4-NOW-005, BT-V4-NOW-006, BT-V4-E2E-004, BT-V4-ACTN-003, BT-V5-INT-001

### BT-V5-AIR-026 — 可评测的检索和排序升级

现数据库规则基线可复用；没有检索对照评测、受控embedding/reranker/撤销索引。

依赖：BT-V5-AIR-022, BT-V5-AIR-023, BT-V5-AIR-044, BT-V5-AIR-048, BT-V5-INT-001

### BT-V5-AIR-029 — Prompt 变更评审和影子回放

无prompt评审/影子回放/版本对照，不能拿现单元fixtures当LLM评测。

依赖：BT-V5-AIR-027, BT-V5-AIR-028, BT-V5-AIR-048, BT-V5-AIR-049, BT-V5-INT-001

### BT-V5-AIR-031 — 结构化视觉观察接口（代码/本地；live 另验）

没有视觉观察适配与数据集；live另缺VISION_PROVIDER_APPROVAL，文本P0不依赖。 当前只验证服务端代码与本地合同，不声称取得供应商批准、真实调用或原 source 全部完成。

依赖：BT-V5-AIR-008, BT-V5-AIR-009, BT-V5-AIR-028, BT-V5-AIR-030, BT-V5-INT-001

### BT-V5-AIR-032 — 媒体地点和时间证据验证

现已授权Place ID/OccurredAt可引用；无照片metadata/Observation验证与归属候选，不能将关联推断为到访。

依赖：BT-V5-AIR-024, BT-V5-AIR-030, BT-V5-AIR-031, BT-V5-INT-001

### BT-V5-AIR-033 — Moment 富集作业和用户反馈

无异步视觉Moment富集作业、候选状态与恢复，原Moment写链继续可用。

依赖：BT-V5-AIR-016, BT-V5-AIR-018, BT-V5-AIR-031, BT-V5-AIR-032, BT-V5-AIR-043, BT-V5-AIR-044, BT-V5-AIR-050, BT-V5-INT-001

### BT-V5-AIR-034 — 视频音频与多帧扩展（代码/本地；live 另验）

无视频/音频/多帧管线，live缺MEDIA_PROVIDER_APPROVAL；P2不阻塞文本/图片。 当前只验证服务端代码与本地合同，不声称取得供应商批准、真实调用或原 source 全部完成。

依赖：BT-V5-AIR-030, BT-V5-AIR-031, BT-V5-AIR-033, BT-V5-AIR-048, BT-V5-INT-001

### BT-V5-AIR-035 — 富集去重与事实归属

没有sourceCluster或照片所有者/发布者/被摄者/Agent主体归属模型。

依赖：BT-V5-AIR-024, BT-V5-AIR-031, BT-V5-AIR-043, BT-V5-INT-001

### BT-V5-AIR-041 — 通知提案和节制触达

确定性服务提醒继续可用；没有语义通知提案/频控/静默/类别policy，不能由AIR故障影响旧提醒。

依赖：BT-V5-AIR-019, BT-V5-AIR-037, BT-V5-AIR-038, BT-V5-AIR-040, BT-V5-AIR-050, BT-V5-INT-001

### BT-V5-AIR-042 — 为既有有界A2A接入AIR许可与运行预算适配（保持关闭）

AGA001合同-only已验，独立双侧授权与最小响应可复用；无transport/live grant/hop预算，服从Post-Pilot gate。

依赖：BT-V5-AIR-020, BT-V5-AIR-023, BT-V5-AIR-037, BT-V5-AIR-038, BT-V5-AIR-040, BT-V4-AGA-001, BT-V4-AGA-002, BT-V4-ORG-001, BT-V4-BIZ-002, BT-V5-AGE-060, BT-V5-AGE-062, BT-V5-INT-001

### BT-V5-AIR-043 — 文字证据到 MemoryCandidate

没有授权文字→typedMemoryCandidate→AGE write adapter；缺AGE_WRITE_CONTRACT；provider live缺配置但offline代码可先做。

依赖：BT-V5-AIR-009, BT-V5-AIR-011, BT-V5-AIR-016, BT-V5-AIR-018, BT-V5-AIR-023, BT-V5-AIR-027, BT-V5-AIR-028, BT-V5-AGE-005, BT-V5-AGE-007, BT-V5-AGE-008, BT-V5-AGE-069, BT-V5-INT-001

### BT-V5-AIR-044 — 受控候选确认与删除闭环

没有Memory候选确认/拒绝/纠正/删源/撤销传播及旧任务防复活闭环。

依赖：BT-V5-AIR-018, BT-V5-AIR-023, BT-V5-AIR-037, BT-V5-AIR-040, BT-V5-AIR-043, BT-V5-AGE-011, BT-V5-AGE-069, BT-V5-INT-001

### BT-V5-AIR-045 — 兴趣证据聚合与可校准置信度

没有sourceCluster兴趣聚合/冲突/衰减/校准；数值样例不是概率。

依赖：BT-V5-AIR-024, BT-V5-AIR-035, BT-V5-AIR-043, BT-V5-AIR-044, BT-V5-AIR-048, BT-V5-INT-001

### BT-V5-AIR-046 — 记忆使用授权与公开资料边界

当前公私资料/单用途同意边界可复用；无purpose-scopedMemoryView或独立公开字段确认，SAF004未完成。

依赖：BT-V5-AIR-023, BT-V5-AIR-044, BT-V5-INT-001

### BT-V5-AIR-047 — 来源纠错与变更传播

现Moment revision与withdraw不是Memory依赖失效图；缺源变更→检索缓存候选失效传播。

依赖：BT-V5-AIR-018, BT-V5-AIR-024, BT-V5-AIR-044, BT-V5-AIR-045, BT-V5-INT-001

### BT-V5-AIR-050 — Flutter 候选审阅和Agent控制

中文审阅/来源/关闭开关/迟到auth保护组件可复用；没有MemoryCandidate审阅或run停止/删除控制。

依赖：BT-V5-AIR-016, BT-V5-AIR-018, BT-V5-AIR-040, BT-V5-AIR-043, BT-V5-AIR-044, BT-V5-AGE-046, BT-V5-AGE-049, BT-V5-AGE-069, BT-V5-INT-001

### BT-V5-AIR-051 — 计划审阅及执行结果界面

当前发布/邀请preview确认组件可复用；无versionedApproval、多动作逐步批准或UNKNOWN_RECONCILE页面。

依赖：BT-V5-AIR-038, BT-V5-AIR-040, BT-V5-AIR-050, BT-V5-INT-001

### BT-V5-AIR-052 — 全链路故障与质量回归

现766 Go PASS事件三轮/189 Flutter是V4基线；没有AIR120用例/双provider/视觉/live写/A2A版本报告。

依赖：BT-V5-AIR-012, BT-V5-AIR-017, BT-V5-AIR-020, BT-V5-AIR-033, BT-V5-AIR-038, BT-V5-AIR-041, BT-V5-AIR-042, BT-V5-AIR-045, BT-V5-AIR-046, BT-V5-AIR-047, BT-V5-AIR-049, BT-V5-AIR-051, BT-V5-INT-001

### BT-V5-AIR-053 — 默认关闭和内部受控发布

现Business/推断/自治没有开放入口；无AIR分出口/候选/视觉/写/A2A kill switch及授权灰度配置。

依赖：BT-V5-AIR-011, BT-V5-AIR-016, BT-V5-AIR-018, BT-V5-AIR-037, BT-V5-AIR-044, BT-V5-AIR-048, BT-V5-AIR-050, BT-V5-AGE-066, BT-V5-INT-001

### BT-V5-AIR-056 — 专业模型和自托管研究门槛

没有业务收益/TCO/数据权利评测；不采购GPU、不训练模型；包中供应商当时声明未作当前账户核验。

依赖：BT-V5-AIR-013, BT-V5-AIR-026, BT-V5-AIR-048, BT-V5-AIR-049, BT-V5-INT-001


## 全部180项DONE：历史交付证据原文

以下原证据按任务保留。本次没有重新运行这180项或将开发证据改称生产证据。

### BT-AUD-001 — Create functional MVP gap audit

Reviewed Flutter/Go/SQL code paths in docs/product/FUNCTIONAL-MVP-GAP-ANALYSIS.md; 13/13 domains classified with 50 resolvable code links; 39 queue IDs match workbook Requirements; local API at 127.0.0.1:3692 unavailable during audit, so live DB behavior not claimed.

### BT-RUN-001 — Fix physical-device API connectivity and environment config

apps/client/tool/run_device_debug.ps1 -CheckOnly passed on ADB device c641566b; explicit adb reverse tcp:3692 to tcp:3692; device curl /readyz HTTP 200 and /v1/cities/aberdeen-gb/activities HTTP 200 with 2 DB-backed development records and stable IDs; Android debug manifest permits loopback cleartext; apps/client/README.md documents setup and Mapbox 403 boundary; flutter analyze PASS (No issues found).

### BT-RUN-002 — Add request tracing and debug diagnostics

Go request-ID tests, go test ./..., go vet ./..., go build ./...; Flutter analyze and 27 tests passed; debug APK built and installed on c641566b; device /readyz echoed request ID; Agent badminton returned 2 activities and Settings showed HTTP 200/result count 2/request ID matching server log 7c168b06497ff253317b435994fe80d4; debug panel guarded by kDebugMode.

### BT-DAT-001 — Finalize core schema for Organization Activity Place Participation

Migration 021 applied twice idempotently on local PostgreSQL, down migration succeeded and 021 reapplied; core_schema_021_verify.sql transaction passed organization host, cancellation, person-only RSVP and duplicate RSVP constraints with ROLLBACK; go test ./... passed; existing Agent API still returned 2 activities and no test rows persisted.

### BT-DAT-002 — Create development seed fixtures through real database path

Clean isolated DB: 27 migrations PASS; seed 001+002 twice PASS, CityContext/Places/Organizations/future Activities/People/Memberships=1/3/2/5/2/3; isolated API and physical-device Now show 5 activities and Chinese category labels; Flutter analyze/67 tests/debug APK build+install PASS; Go test/vet/build PASS; see docs/research/2026-10-01-functional-mvp-seed-verification.md.

### BT-AUT-001 — Ensure real authentication session reaches backend

Physical device c641566b local dev-phone login succeeded, secure Session survived force-stop/relaunch, logout remained signed out after another relaunch; API /v1/me identified a person, anonymous Activity Plan mutation returned 401, authenticated create/list/delete returned 201/1/204, logout 204 and revoked Session /v1/me 401; Flutter analyze, 28 tests and debug APK build passed; go test ./..., go vet ./..., go build ./... passed. Production OIDC/SMS remains partial and is documented separately.

### BT-ORG-001 — Implement organization membership and admin authorization

Owner/admin membership persisted in DB and SQL-gated organization profile update passed local API integration: owner 200, admin 200, member 403, nonmember 403, anonymous 401. Agent workspace denial now returns immediately. Organization profile validation unit test passed; go test ./..., go vet ./..., go build ./... passed. No membership invitation UI or activity publisher claimed.

### BT-ACT-001 — Implement Activity create/edit/publish/cancel APIs

Local API+Postgres integration passed: validation 400, member create/edit/publish 403, draft 201 and absent from public list, edit revision 2, publish 200 and visible publicly, duplicate publish 409, edit published 200, cancel 200 with cancelled public detail, duplicate cancel 409, managed list 200. SQL rollback test confirmed valid Participation and rejection after cancellation. Agent query found Chinese-titled category-coded badminton Activity. Anonymous Agent query regression fixed and HTTP 200 response body rechecked. go test ./..., go vet ./..., go build ./... passed.

### BT-ORG-002 — Build minimal Organization Console activity publisher

Chinese-first self-service organization activity console implemented; Flutter analyze 0 issues, 32 tests passed, debug APK built and reinstalled on c641566b; real device created, previewed, published and canceled test activities with persisted database state; local test activities cleaned. Scope and map limitation documented in docs/research/2026-10-01-organization-console-device-acceptance.md.

### BT-SRC-001 — Implement real nearby Activity search

GET city activities supports validated bounds, RFC3339 interval and category filters against PostgreSQL; live API checks returned 2 in bounds, 0 outside, 1 in interval, empty array for unmatched category, and HTTP 400 for invalid bounds. Temporary live fixtures proved draft, cancelled, expired and unlisted rows excluded while published control remained visible; fixtures cleaned. Stable UUIDs observed. Go test ./..., vet ./..., build ./... passed. API and contract docs updated.

### BT-PUL-001 — Implement Area Pulse API from real data

Area Pulse API aggregates real public eligible Activity rows with required viewport and optional RFC3339 time; live API returned populated total 2/category badminton 2, empty area and time total 0/categories [], and HTTP 400 for missing bounds. Shares discovery predicate with search; Go test, vet and build passed; contract/API docs updated. Client consumption remains next task.

### BT-NOW-001 — Connect Now Workspace to real Area Pulse and Activity data

Now connects to real Pulse API for viewport counts and stable Activity ID map pins; initial valid viewport or explicit area action requests snapshot, camera movement does not. Client no longer selects local badminton demo without API; loading/empty/error and stale responses covered by 36 Flutter tests and analyze 0 issues. Debug APK built, installed and Flutter debugger attached on c641566b. Physical-device Mapbox source remains HTTP 403, so pin rendering could not be observed there; truthful Chinese city-level API activity fallback and detail were device-tested, screenshots documented in docs/research/2026-10-01-now-discovery-device-acceptance.md. Go test/vet/build passed.

### BT-MAP-001 — Stabilize persistent map and keyboard lifecycle

Android device c641566b shows real Aberdeen Mapbox basemap and cluster pin; automation/verify_map_keyboard.ps1 passed 20/20 keyboard cycles with Now foreground, composer relocation and restoration; open/closed evidence screenshots show same pin and camera. Flutter MapCanvas Widget/Element identity 20-cycle test and bounded rebuild implementation documented in docs/research/2026-10-01-map-keyboard-lifecycle-audit.md.

### BT-MAP-002 — Implement stable semantic marker system and selection persistence

Native Mapbox semantic marker bitmap covers Activity/Group/Place/Person/Cluster; stable entity and sorted cluster IDs plus incremental annotation fingerprint diff. Fixed cluster tap to show members and idle Place selection card. Real device c641566b: cluster list, Activity card, selected Pin and card persisted after pan, Place card, Chinese status labels; screenshots and report docs/research/2026-10-01-map-marker-selection-verification.md. Flutter analyze, 54 tests and Android debug build/install passed.

### BT-MAP-003 — Implement explicit Search this area flow

Camera drag preserves ResultSet/selection and clears stale CTA until latest settled bounds; explicit tap requests Pulse and Agent for those bounds; stale async responses ignored and results atomically replaced. Flutter analyze and 57 tests pass; latest APK built/installed on c641566b, real map pan exposes CTA and tap returns 3 published activities. See docs/research/2026-10-01-search-this-area-verification.md.

### BT-NOW-002 — Implement Local Pulse and Active Intent as separate UI models

Separate NowPulse and ActiveIntentSummary UI models; top-left Pulse capped at total plus two factual categories, hidden on camera move/active task; structured action/category/time heading keeps original query in conversation. Flutter analyze, 63 tests, Android build/install pass; real device c641566b screenshots of Pulse and badminton/weekend intent at docs/research/2026-10-01-local-pulse-active-intent-verification.md.

### BT-AGT-001 — Implement normalized MVP intent parser/router

Intent corpus, real API activity/organization/place and signed-in two-turn refine/compare/restore; Go test/vet/build and Flutter analyze/39 tests passed; docs/research/2026-10-01-agent-intent-router-verification.md

### BT-AGT-002 — Implement AgentResponse and ResultSet contract

Go contract unit tests + go test/vet/build, Flutter analyze and parser/action widget tests; live API result refs for 1 organization/2 places/2 activities, empty and unsupported states; docs/research/2026-10-01-agent-response-contract-verification.md

### BT-AGT-003 — Connect Now composer to real Agent query flow

Flutter analyze/41 tests, Go test/vet/build, debug APK installed and DevTools attached; device query answer before 2 real test results, Chinese follow-up distance response, unobstructed conversation screenshots; docs/research/2026-10-01-now-agent-composer-device-acceptance.md. Visible map Pin remains blocked by Mapbox tile 403.

### BT-AGT-004 — Persist conversation task context for follow-ups

Signed-in two-turn area API integration retained task/resultSet IDs and badminton/weekend filters through restore; anonymous bounds request worked without local task ID; Go test/vet/build, Flutter analyze/42 tests/debug APK and device install; docs/research/2026-10-01-agent-conversation-context-verification.md

### BT-NOW-003 — Split EntityPeekCard from AgentResultsSheet

Flutter analyze and 66 tests passed; Android debug APK built and installed on c641566b; pin card, Agent results, expanded conversation and detents reviewed on device; docs/research/2026-10-01-entity-peek-results-sheet-verification.md

### BT-RSV-001 — Implement RSVP persistence and Activity detail primary action

docs/research/2026-10-01-rsvp-device-verification.md: Go test/vet/build, Flutter analyze/43 tests/build, real API duplicate/full/cancelled cases, Android RSVP cancellation/rejoin and force-stop restart persisted

### BT-PLN-001 — Implement Plans from real participation/save data

docs/research/2026-10-01-my-activities-verification.md: real participation list, API upcoming/past, Flutter 44 tests/analyze, Go test/vet/build, APK install and device restart screenshot

### BT-SAV-001 — Implement Save independent from RSVP

docs/research/2026-10-01-save-rsvp-independence.md: E2E API save/unsave without RSVP change, Android detail save/unsave while joined, Go test/vet/build, Flutter analyze/46 tests

### BT-DET-001 — Complete consumer-grade Activity Detail page

docs/research/2026-10-01-activity-detail-verification.md: Flutter analyze/46 tests/debug APK/device; Go test/vet/build; real API full fields; native share, map navigation, save, long content screenshot

### BT-NTF-001 — Implement critical activity notifications

Migration 023 and atomic participant notifications; idempotent reminder worker; loopback integration script PASS for reminder/edit/cancel/link/read/targeting; Android device inbox-to-cancelled-detail observed; Go test/vet/build and Flutter analyze/46 tests/debug APK PASS; docs/research/2026-10-01-activity-notification-verification.md

### BT-INB-001 — Build Inbox notification center

Migration 024 conversation target/backfill and Chinese message notifications; removed fake preview messages; Inbox empty/error/retry/read/link behavior and Flutter widget tests; local conversation integration PASS with cleanup; Android inbox message to real conversation observed; Flutter analyze/48 tests/debug APK and Go test/vet/build PASS; docs/research/2026-10-01-inbox-verification.md

### BT-ORG-003 — Build verified Organization profile and public activity list

Public Organization API exposes stored verification state, HTTPS links and upcoming public Activities; private profile 404 and synthetic integration PASS with original data restored; Chinese-first Flutter profile and Activity host link; device navigation observed; Go test/vet/build, Flutter analyze/49 tests/debug APK PASS; docs/research/2026-10-01-public-organization-verification.md

### BT-OAG-001 — Implement organization verified FAQ knowledge

Migration 025 and Chinese FAQ admin/public ask UI; verified public FAQ/profile/activity/link answers cite stored sources, unknown and unverified questions do not invent facts; synthetic API scenario PASS with data restored; Android device sourced answer observed and screenshot; Go test/vet/build, Flutter analyze/51 tests/debug APK PASS; docs/research/2026-10-01-organization-agent-faq-verification.md

### BT-ANA-001 — Implement pilot funnel analytics

Migration 026 and privacy-limited activity events; Now/Agent impressions and sourced detail openings, server-side idempotent RSVP/save conversions, admin-only aggregate Chinese metrics UI; synthetic integration PASS and fixture cleaned; Android display/detail events and admin metrics screen observed; Go test/vet/build, Flutter analyze/53 tests/debug APK PASS; docs/research/2026-10-01-activity-analytics-verification.md

### BT-SAF-001 — Enforce map privacy boundary

Audited Android manifest, API routes, published Place/Activity/Group and opt-in Person area query; no device GPS permission or public precise tracking route. Added server response sanitization of non-opted person coordinates and client map-pin guard. Go privacy table tests and Flutter malicious-response test pass; go test ./..., flutter analyze, 55 Flutter tests passed. See docs/research/2026-10-01-map-privacy-audit.md.

### BT-SAF-002 — Add report/support entry points and audit log baseline

Migration 027, owner-private report/support API with rate limit and operator queue; same-transaction person-attributed organization/activity/FAQ audit; Chinese Settings/Activity/Organization support entry; API and audit scenario scripts PASS with all fixtures cleaned; Android report form and Settings entry observed; Go test/vet/build, Flutter analyze/54 tests/debug APK PASS; docs/research/2026-10-01-safety-support-verification.md

### BT-PER-001 — Protect async flows from stale responses

Client turn serial ignores stale Agent responses; Pulse serial also invalidates pending area when returning to cached area. Composer permits next send during loading. Automated reverse-completion A/B/C, Pulse return and pending-submit tests; Flutter analyze, 60 tests and Android debug build/install passed. Device c641566b conversation shows badminton, football, tennis with tennis as final response. Local ignore semantics and cross-device persistence limitation documented in docs/architecture/AGENT-DATA-API-CONTRACTS.md and docs/research/2026-10-01-stale-response-verification.md.

### BT-PER-002 — Profile Now hot paths and remove avoidable jank

Flutter profile VM timeline captured pan, keyboard and sheet; removed per-frame timer churn and same-zoom marker sync; Flutter analyze and 66 tests pass; updated debug APK installed, 8 pan gestures and keyboard cycles 21-24 pass; post-change profile install denied by device, documented in docs/research/2026-10-01-now-performance-profile.md

### BT-TST-001 — Create end-to-end vertical-slice integration test/checklist

docs/research/2026-10-01-vertical-slice-e2e.md: repeatable loopback API script twice (retained and cleanup), admin publish/student discovery/Agent/detail/RSVP/Plans PASS, Android app and Go restart persistence screenshots

### BT-RUN-003 — Centralize environment configuration and reject release loopback API

Central BirdtieEnvironment across API and map; flutter analyze PASS; flutter test PASS 72; direct release build rejected; release wrapper rejected missing, localhost and HTTP fixtures; debug APK build/install and c641566b ADB reverse /readyz plus app API GET 200 PASS. docs/research/2026-10-01-runtime-environment-verification.md. No real staging/production credentials or endpoint validated.

### BT-ORG-004 — Complete membership invite, role and revoke lifecycle

Migration 028 forward 28/28 blank DB, 028 down/reapply and dev seeds PASS. Go test/vet/build PASS. Flutter analyze 0, test 76, debug APK build/install c641566b PASS. automation/verify_membership_lifecycle.py PASS owner/admin/member/nonmember, invite/accept/duplicate, last owner, revoke and cross-org. PostgreSQL audit 8 rows with target and old/new role/status. Physical device Birdtie device QA member changed to admin; screenshot docs/research/evidence/2026-10-01-member-role-device.png. See docs/research/2026-10-01-organization-membership-verification.md. Synthetic development identities only.

### BT-MAP-004 — Show public Organization markers only from explicit location data

2026-10-01: migration 029 forward/down/up PASS; loopback API privacy matrix PASS via automation/verify_organization_map.py (pending/private/unverified/hidden/reedited absent; independent review, audit); Flutter analyze 0/test 76 + marker tests 2; Go test/vet/build PASS; debug APK installed on c641566b and cluster->organization card->detail screenshots in docs/research/2026-10-01-organization-map-verification.md. Synthetic local verification flag reset; no pilot claim.

### BT-NTF-002 — Make starts-soon reminders durable across service downtime

2026-10-01 code/local acceptance: independent worker while API stopped inserted=1, retry inserted=0, API restart Inbox exactly one; wrong DB port exit 1 status=failed; edit resets reminder, cancellation removes it, audit/Inbox PASS via automation/verify_activity_notifications.ps1; Go test/vet/build PASS. SQL locks Activity and Participation rows; docs/research/2026-10-01-activity-reminder-reliability.md. Production scheduler/log alert still required by BT-REL-001.

### BT-COM-001 — Community domain specification

Canonical identity model, COMMUNITY-AND-ACTIVITY-SOCIAL-MODEL.md (10 sections), ADR 0016 and architecture index updated; rg confirmed links and definitions; git diff --check exit 0.

### BT-COM-002 — Community database and membership model

030 migration up/down/reapply all psql exit 0 on local Postgres; synthetic two-person Community and membership transaction passed and rolled back; deleting sole owner rejected at deferred constraint (expected psql exit 1); go test ./internal/community ./internal/postgres passed. Existing IDs preserved; no real Community data written.

### BT-COM-003 — Activity organizer abstraction

031 organizer migration up/down/reapply passed; existing Activity count 9, organizer count 9, all 9 Organization, 0 orphan; synthetic Person and Community organizer insert/rollback passed; go test ./internal/activitypublish ./internal/postgres ./internal/httpapi passed.

### BT-COM-004 — Community authorization

Server-side PostgreSQL methods enforce owner/admin/member/outsider boundaries, invite, role, transfer and archive; TestCommunitySocialPermissionsIntegration passed against local Postgres (owner approval, duplicate join conflict, member self-promotion forbidden, admin archive/transfer forbidden, old owner revoked, archived join 404); synthetic rows cleaned.

### BT-COM-005 — Community API

Community REST routes and PostgreSQL CRUD/membership/request/invite/role/transfer/archive implemented; TestCommunitySocialHTTPIntegration passed against local Postgres: anonymous 401, create 201, private detail 200, request 200, member/outsider approval 403, owner approval 200, repeat join 409, leave 204; synthetic sessions/data cleaned.

### BT-COM-006 — Activity visibility and organizer permissions

032 migration up/down/reapply passed; shared SQL ACL enforces public, organizer_members and invite_only in list/detail/RSVP and DB participation guard; local Postgres TestSocialActivityVisibilityIntegration passed private Community public RSVP without auto-membership, member-only member access/outsider 404, explicit invite access; Organization matrix SQL BEGIN/DO/ROLLBACK passed; Person members-only HTTP validation test passed; Go postgres/httpapi tests passed.

### BT-COM-007 — Flutter Community UI

2026-10-01 Flutter test/community_api_test.dart + community_page_test.dart 4/4 PASS; Go postgres/httpapi integration PASS with local PostgreSQL. Android c641566b debug build installed: owner created synthetic Community bf427e93-934a-4c68-8fe1-c7f45703917c, second local test account submitted request, owner approved, detail showed 2 members. Screenshots work/community-created.png, work/community-request.png, work/community-owner-detail.png, work/community-approved.png. Dev fixed-code identities only; no production identity evidence.

### BT-COM-008 — Activity organizer UI

Flutter analyze PASS; flutter test activity_detail_sheet_test.dart activity_organizer_test.dart community_page_test.dart PASS; physical Android c641566b screenshots work/activity-organizers.png, work/activity-post-result.png, work/activity-person-created.png, work/org-activity-created.png, work/community-activity-default.png, work/org-activity-default.png, work/community-activity-detail.png; local DB activity_organizers confirms Person 9dc357e0-6752-46db-9793-afe131819b76, Community 37d7ceac-f86f-4998-acdd-1eae15d7d21d, Organization cfd739ba-345a-4aef-94cf-2217b7b3da15; map Activity detail routes all three organizer types.

### BT-COM-009 — Community synthetic seed and E2E

Local-only dev-seeds/003_community_social.sql applied twice: INSERT 3+2 then 0+0; DB verifies 3 fixtures/3 active owners/0 Community agents. automation/verify_community_social.ps1 against loopback API :3696 PASS public/open, private/request, hidden/invite discovery; Person/Community/Organization publish, public outsider RSVP without membership, members-only 404, invite-only grant; 4 synthetic Activities all cancelled. Go PostgreSQL/HTTP integration tests TestCommunitySocial* and TestSocialActivity* PASS with BIRDTIE_DATABASE_URL.

### BT-COM-010 — Community regression verification

Flutter analyze PASS; flutter test PASS 87 tests; flutter build apk --debug PASS (app-debug.apk 241659750 bytes). Go test ./... -count=1 with PostgreSQL PASS; go vet ./... PASS; go build ./... PASS. automation/verify_community_migrations.ps1 PASS on disposable DB: migrations 001-032, existing Activities 5->5 and 5 organizers, 032/031/030 down then 030/031/032 reapply, SQL invariants and seed twice. automation/verify_community_social.ps1 PASS; automation/verify_vertical_slice.ps1 PASS existing Organization->Agent discovery->RSVP->Plans flow. No observed auth/org/map/reminder regression in suite; operational production evidence remains blocked.

### BT-COM-011 — Community physical-device verification

Physical Android c641566b 10/10 synthetic scenarios; docs/testing/COMMUNITY-SOCIAL-PHYSICAL-DEVICE-2026-10-01.md; C public RSVP activity 37d7ceac-f86f-4998-acdd-1eae15d7d21d going with zero Community memberships; B sees members-only, C does not; Organization Activity cfd739ba-345a-4aef-94cf-2217b7b3da15 remains published; flutter analyze and community_page_test PASS after refresh fix.

### BT-COM-012 — Community completion report

docs/reports/FUNCTIONAL-MVP-COMPLETION-REPORT.md Community section lists all BT-COM-001..012, migration/Go/Flutter files, full verification and separate Community YES (local synthetic) vs Closed Pilot NO; docs/product/FUNCTIONAL-MVP-GAP-ANALYSIS.md and docs/testing/FUNCTIONAL-MVP-E2E.md updated; P0 28 DONE/3 BLOCKED preserved; taskctl summary reviewed.

### BT-V4-AUD-001 — Snapshot current repository and running-task state

docs/research/BIRDTIE-V4-STATUS-RECONCILIATION.md inventory: master HEAD d6e86d3, 75 tracked modifications/158 untracked entries saved in work/v4-2026-10-01-git-status.txt; old live queue 57=51 DONE/3 TODO/3 BLOCKED, all COM001-012 DONE; 001-032 migrations/docs/Go/Flutter inventoried; no reset/stash/commit. XLSX and Markdown both 87 unique IDs, no missing dependencies; import_v4_backlog.py added 87 without overwriting old tasks, idempotent rerun added 0.

### BT-V4-AUD-002 — Reconcile V3 backlog, Community tasks, and latest 39/3/3 status

docs/research/BIRDTIE-V4-LEGACY-TASK-MATRIX.md generated 57/57 prior task rows from live queue (51 DONE/3 TODO/3 BLOCKED) with V4 overlap and evidence excerpts; docs/product/BIRDTIE-V4-GAP-ANALYSIS.md audits SQL/Go/Flutter across identity, tie/chat, intent, context, place/business, opportunity, Now and release as REAL/PARTIAL/NOT_IMPLEMENTED; docs/research/BIRDTIE-V4-STATUS-RECONCILIATION.md records old 39/3/3 as stale, V4 87 ID/workbook-Markdown match, 57 P0/27 P1/3 P2, no duplicate IDs or missing dependencies. Old tasks/status/evidence preserved.

### BT-V4-CAN-001 — Publish BirdTie Canonical Product Spec V4

docs/product/BIRDTIE-CANONICAL-PRODUCT-SPEC-V4.md defines Identity/Tie/Intent/Context/Agent plus Opportunity/Action, global Person, Aberdeen pilot boundary, Place/Venue/Business, social/chat, privacy and staged gates; explicitly preserves Community no-Agent and Chinese-first UI. docs/product/README.md and BIRDTIE-MASTER-ROADMAP.md link to it and identify old roadmap as historical; referenced local canonical paths checked present. V4 gap audit separates target from implementation.

### BT-V4-ADR-001 — Create ADR for ActorRef/PrincipalRef, Agent roles, Context, Place/Venue/Business

Accepted docs/decisions/0017-v4-actor-agent-context-place-model.md: typed ActorRef with server authorization, current Personal/Organization vs future Business/Community Agent roles, global Context without City Agent, Place context/Venue capacity, independent Business target and safe legacy organization_type business bridge; explicitly extends ADR0014/0016 without changing runtime schema. Cross-linked from AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md, AGENT-DATA-API-CONTRACTS.md and architecture index; migration plan is next dependency, no V4 schema claim yet.

### BT-V4-MIG-001 — Create non-destructive V4 schema/API migration plan

docs/migration/BIRDTIE-V4-MIGRATION-PLAN.md orders proposed additive 033-041 Context/Agent/Tie/Chat/Intent/Venue/Business/Activity/Moment stages with backfill, old API dual compatibility, rollback/data-retention and no-drop window; ADR0017 and Agent API contracts crosslink. Disposable local DB verify_community_migrations.ps1 PASS migrations 001-032, Activity 5->5, 5 organizers, 032/031/030 down/reapply, seed twice. PowerShell 5.1 vertical slice attempt failed unsupported SkipHttpErrorCheck; pwsh 7.6.5 rerun PASS Organization->Agent->RSVP->Plans/Save, synthetic activity cancelled. No 033+ schema yet.

### BT-V4-AUT-001 — Upgrade autonomous Codex queue to V4

Live queue 144 tasks validates with unique IDs, known acyclic dependencies, DONE evidence/BLOCKED reason, one active task. Idempotent import_v4_backlog.py brought 87 V4 rows; rerun adds 0. automation/snapshots/pre_v4_2026-10-01.json stores all 57 prior records exactly preserved (51 DONE/3 TODO/3 BLOCKED). taskctl.py now supports --queue, validate, PARTIAL and atomic save; automation/verify_taskctl.py PASS disposable summary/next/start/done/block/partial/dependency/evidence guard, live queue SHA unchanged. Python py_compile PASS; docs/product/BIRDTIE-V4-EXECUTION-BACKLOG.md and CODEX_V4_EXECUTION_PROTOCOL.md added; existing birdtie heartbeat updated ACTIVE.

### BT-V4-TST-001 — Lock existing vertical-slice regression baseline

docs/research/BIRDTIE-V4-REGRESSION-BASELINE.md reusable per-change smoke checklist and 2026-10-01 results. Flutter analyze PASS 0 issues; flutter test PASS 87; flutter build apk --debug with local ignored map config PASS 241659966 bytes. Go test ./... -count=1 with local PostgreSQL PASS, go vet ./... PASS, go build ./... PASS. Disposable migration 001-032 and 030-032 down/reapply PASS, old Activity 5->5 and 5 organizers, seed twice. pwsh 7 verify_vertical_slice.ps1 on loopback :3696 PASS Organization->Activity->Agent/discovery->RSVP->Plans/Save, synthetic activity cancelled. PowerShell 5.1 attempt failed unsupported parameter and 7.6.5 rerun passed. Local-only evidence; production pilot remains blocked.

### BT-V4-ACT-001 — Introduce ActorRef/PrincipalRef domain abstraction

internal/actorref defines typed UUID ActorRef/PrincipalRef for PERSON/ORGANIZATION/BUSINESS/COMMUNITY; activity organizer validation and Agent Task create/continue/read/list use typed refs with server-derived workspace checks; Business publish remains closed pending authorization. go test ./... -count=1 with local BIRDTIE_DATABASE_URL PASS; verbose PostgreSQL CommunitySocialPermissions and SocialActivityVisibility integration PASS; go vet ./... PASS; go build ./... PASS. See docs/research/BIRDTIE-V4-COMPLETION-REPORT.md.

### BT-V4-AGF-001 — Create shared Agent Runtime with role/capability packs

Existing shared Agent task route now uses internal/agentruntime role/capability policy for server-derived Personal and Organization workspaces; Business role is reserved but unavailable pending independent ownership, Community has no Agent. Agent role isolation/capability contract tests PASS. go test ./... -count=1 with local PostgreSQL PASS; go vet ./... PASS; go build ./... PASS. See docs/architecture/AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md and docs/research/BIRDTIE-V4-COMPLETION-REPORT.md.

### BT-V4-AGF-002 — Normalize Agent ownership and identity boundary

Ownership audit distinguishes Organization ActorRef organizations.id from Agent PrincipalRef organizations.account_id; old principalId JSON unchanged. Organization workspace now requires active person membership, organization account and Organization Agent; Agent Task routes require active owned Agent. Local PostgreSQL TestAgentOwnershipAndWorkspaceAuthorizationIntegration PASS for correct ownership, outsider, revoked member, suspended Personal/Organization Agent, no City Agent; go test ./... -count=1 with DB PASS; go vet ./... PASS; go build ./... PASS. See ADR 0017 and V4 completion report.

### BT-V4-CTX-001 — Replace city-root assumptions with generic Context Graph semantics

Migration 033 adds constrained City/Country/Institution/Community/Online Context nodes, private Person associations, old Agent Task CITY backfill and nullable city for non-city tasks; old city writer gets typed Context via trigger, new CityContext gets node; API Task/Results carry contextType/contextId. Disposable DB pwsh automation/verify_context_graph_migration.ps1 PASS: legacy Task 1->1, Activities 5->5, SQL cross-city+online/privacy/type invariants, full Go test ./... -count=1, guarded down and reapply; prior 001-032 community migration regression PASS. go vet ./... PASS; go build ./... PASS; git diff --check PASS. No deployment or online HTTP/Intent claim. See docs/architecture/CONTEXT-GRAPH-V4.md and V4 report.

### BT-V4-CTX-002 — Support current, historical and destination contexts

contextgraph.TestDeclarationFixtures and HTTP TestPersonContextLifecycleIntegration in disposable PostgreSQL: current/past/destination City, past Institution, Online, current replacement, private owner-only read/delete, anonymous/other-person denial, same two account IDs; pwsh -NoProfile -File automation/verify_place_context_migration.ps1 -Through 46 PASS full Go API/DB and ID preservation. Flutter Chinese settings UI test and 118 full tests PASS; flutter analyze, debug APK, go vet/build, git diff --check PASS. docs/research/BIRDTIE-V4-CTX-002-AUDIT.md. No formal deployment or verified institution membership.

### BT-V4-PRV-001 — Define Agent context-access policy

agentruntime.DecideContext implements PUBLIC/CONNECTION/CLOSE/PRIVATE/WORKSPACE_PRIVATE policy with server-derived authority, release, block, verified relation and resource grant inputs; Agent task create/continue/read/list invoke policy after session, workspace and active Agent checks. Organization Agent cannot read Personal private memory or personal Tie; no unverified Tie/Close data path exists. 19 red-team fixtures PASS via go test -v ./internal/agentruntime -run TestAgentContextAccessRedTeamFixtures -count=1; local DB go test ./... -count=1 PASS; go vet ./... PASS; go build ./... PASS; git diff --check PASS. See docs/architecture/AGENT-CONTEXT-ACCESS-POLICY.md and V4 report.

### BT-V4-TIE-001 — Implement persistent Connection/Friend requests

034_person_ties.sql and guarded down: disposable PostgreSQL 001-034, old Activities 5->5, requests/messages unchanged, no conversation backfill, SQL invariants, down guard and down/reapply PASS via pwsh automation/verify_person_ties_migration.ps1. go test -p 1 ./... -count=1 with isolated DB PASS including TestPersonTieLifecycleIntegration and TestFriendTieHTTPIntegration (anonymous, invalid city, reject/accept, bilateral list, duplicate, block, fresh pool). go vet ./... PASS; go build ./... PASS. flutter analyze PASS; flutter test 89 PASS including connection_source_test.dart; debug APK build PASS with repository isolated GRADLE_USER_HOME. Development-only, no production or device acceptance.

### BT-V4-TIE-002 — Implement connection lifecycle, remove and block

Authorization/privacy integration: pwsh automation/verify_person_ties_migration.ps1 PASS on disposable PostgreSQL 001-034; Go HTTP and Store tests cover authenticated Tie delete, outsider/anonymous rejection, bilateral removal, reapply/accept, Block hides profile/Tie/conversation and denies friend request/message, explicit Unblock without Tie restoration, block/remove/unblock audit. Migration old-row preservation and guarded down/reapply PASS. go vet ./... PASS; go build ./... PASS; flutter analyze PASS; flutter test 90 PASS including request/remove/block client paths; debug APK build PASS with repository isolated GRADLE_USER_HOME. Synthetic development only, migration not deployed.

### BT-V4-FOL-001 — Implement asymmetric Follow relationships

2026-10-02: pwsh -NoProfile -File automation/verify_place_context_migration.ps1 -Through 47 PASS (001-047, full Go API/DB integration, Follow auth/privacy/Block, ID preservation, 047 guarded down/reapply); go vet ./... PASS; go build ./... PASS; flutter analyze PASS; flutter test 121 PASS; flutter build apk --debug with ignored local map config PASS; docs/research/BIRDTIE-V4-FOL-001-AUDIT.md. Synthetic development only; 047 not deployed, Closed Pilot NO.

### BT-V4-TIE-003 — Expose mutual connections and shared-context signals

2026-10-02: verify_place_context_migration.ps1 -Through 48 PASS (001-048, Go API/DB privacy/query matrix, explicit default-private disclosure, withdrawal, hidden/Block/cancelled/pending, audit, ID preservation and guarded down/reapply); go vet/build PASS; flutter analyze PASS; flutter test 124 PASS; run_device_debug.ps1 c641566b debug build/install PASS; Flutter attach connected; phone settings false->true->re-enter true->false with DB and two audits. docs/research/BIRDTIE-V4-TIE-003-AUDIT.md and docs/testing/evidence/shared-context-2026-10-02/README.md. Only local development DB 046-048 applied; Closed Pilot NO.

### BT-V4-CHT-001 — Create canonical Conversation/Message model

Existing durable 1:1 Conversation/Message UUID/member/timestamps retained; 035_conversation_read_state.sql adds two private member cursors and unknown-read legacy baseline. pwsh automation/verify_conversation_read_migration.ps1 PASS on disposable PostgreSQL 001-035: old rows preserved, HTTP/Store tests for anonymous/outsider/block, message ownership, unread count, forward-only cursor and fresh-pool persistence, guarded down and down/reapply. go vet ./... PASS; go build ./... PASS; flutter analyze PASS; flutter test 91 PASS; debug APK build PASS with isolated repository GRADLE_USER_HOME. Synthetic only; 035 not deployed.

### BT-V4-CHT-002 — Implement 1:1 Chat for connected users

Active Friend Tie explicit POST /v1/me/ties/{tieID}/conversation; existing accepted pair conversation reused, 1:1 human messages persisted by existing SendMessage; friend-sourced chat blocked after Tie removal or Block. pwsh automation/verify_conversation_read_migration.ps1 PASS on disposable 001-035 DB with HTTP and Store tests: friend request/accept/start idempotence, send/receive, new pgx pool restart history ID/body, Block/remove deny. pwsh automation/verify_person_ties_migration.ps1 PASS on 001-034 independent regression. go vet ./... PASS; go build ./... PASS; flutter analyze PASS; flutter test 92 PASS including friend chat API and offline no phantom message; isolated Gradle debug APK build PASS. Synthetic development only, no production deployment.

### BT-V4-CHT-003 — Share BirdTie entities into Chat

原AC七类Activity/Place/Person/Community/Organization/Business/Moment stable-ID结构卡全部真实native078七类型TestPASS；最新冻结Go9483/0FAIL-SKIP/test-vet-build0/750stable/旧public/catalog/down-reapply/ownedDROP（新079独立WIP不在此帧）。原双方ACL/撤销等待/并发幂等/100窗外原键回执与注册HTTP真实通过。实际同keyendpoint/client/getter/store/workspace ABA、晚GET/POST、借用client错误close真实3RED→绑定修复54PASS；新真机正常消息仅三分之一/大空白真实缺陷→布局widget两RED+发送40dp缺陷→最小LayoutBuilder/48dp修复56目标PASS。当前251冻结wholeFlutter950功能+115loading/0FAIL-SKIP/analyze-test-Debugbuild0，5源与47不可变worker归档逐bytes/SHA核证，manifestaa0edff8e98c0c4a6dffb5bf03a5e596bd2e84a2b1a9abe5721f288e67eacbd2。cf0APK实际install-r-t手机c641566b保留数据/默认地图token，ownedschema078 API47120 frozen9483无重复migration/所有原public+xmin升级同值；普通收件人具体预览→真实UI201 request09c306f39a6eb91c63b6aaf9004270fc→单一原key/Message4aef1271-d0b1-40d9-89d5-1bb146516f9b，cancel全行hash不变，旧未知GET404不当未发送/不盲重发，App实际重启12消息原rows+xminhash不变，正常/IME/back截图真实核验。保留所有旧PARTIAL与失败/旧APK帧；旧几何手机测试不冒充新fix通过。CODE_LOCAL原AC未发现缺功能；AT/音频/未提示真人消费/生产IdP-CSSA-部署真实场景未测仍如实NOT_RUN，不单独机械无限阻塞此CODE_LOCAL原结构卡需求，ClosedPilot/ConsumerBeta NO。根proof work/v5-age038-resume/cht003-code-local-done-proof2.json。

### BT-V4-CHT-004 — Implement Activity conversation

docs/testing/evidence/activity-chat-2026-10-02/README.md: 049 explicit participant conversation/authorization/moderation/report API and Chinese UI; disposable 001-049 migrations, seeds, full Go API/DB tests incl concurrent idempotency/privacy/lifecycle and down guards PASS; Go vet/build PASS; Flutter analyze, 129 tests, debug APK PASS; Android16 install/Flutter attach and RSVP-consent-message-API restart-removal-leave-RSVP cancellation PASS. Local synthetic data only; Closed Pilot NO.

### BT-V4-CHT-005 — Implement Community conversation baseline

docs/testing/evidence/community-chat-2026-10-02/README.md: 050 independent Community conversation and membership-revoke trigger, current active member/admin authorization, consent, messages/moderation/report and shared Chinese UI. Disposable 001-050+seeds/full Go API/DB tests/down guards/old IDs PASS; Go vet/build PASS; Flutter analyze/131 tests/debug APK PASS; Android16 discovery-membership-consent-send-chat leave preserves membership-rejoin/remove-Community leave revokes chat PASS. Synthetic local data; Closed Pilot NO.

### BT-V4-PRV-002 — Enforce social messaging and discovery privacy controls

2026-10-01: pwsh automation/verify_conversation_read_migration.ps1 PASS disposable PostgreSQL 001-035, full Go integration including TestSocialDiscoveryAndReportPrivacyIntegration (private/public profile, consent, Block/Unblock, withdrawn Intent, blocked reporter, owner-only reports, rate limit), 035 guarded down/reapply; go vet ./... PASS; go build ./... PASS; flutter analyze PASS; flutter test 93 PASS; flutter build apk --debug with local map config and isolated Gradle home PASS; git diff --check PASS. Flutter friend/chat account report and block UI; fixed Profile grant UUID SQL cast. Development evidence only, no deployment.

### BT-V4-SOC-001 — Build relationship context for Personal Agent

docs/testing/evidence/agent-relationships-2026-10-02/README.md: canonical + 051; default-off owner/live Personal Agent SQL/HTTP tool and Chinese review/Now action; full disposable 001-051 + 3 seeds + Go tests + old IDs/down/reapply PASS; Go vet/build PASS; Flutter analyze + 135 tests + Debug APK PASS; actual Android local consent/query/review/revoke and old-task empty API proof PASS. Friend-entry count defect found on device and fixed/regressed; no raw private content/history/Pin. Closed Pilot NO.

### BT-V4-INT-001 — Create first-class social Intent entity

2026-10-01: pwsh automation/verify_social_intents_migration.ps1 PASS disposable PostgreSQL 001-036, seeded legacy Intent 1->1 same ID and Activity 5->5, full go test -p 1 ./... -count=1 PASS including TestSocialIntentDraftHTTPIntegration (anonymous/org/invalid denied, owner-only draft/read, fresh pool persistence, DB creator guard), populated down refused and empty down/reapply PASS; go vet ./... PASS; go build ./... PASS; git diff --check PASS. New 036 social_intents draft entity and owner-only API; no public discovery or production deployment.

### BT-V4-INT-002 — Support IN_PERSON / ONLINE / HYBRID intent modality

2026-10-01: socialintent TestParseConstraintsByModality PASS; pwsh automation/verify_social_intents_migration.ps1 PASS disposable PostgreSQL 001-036 and full go test -p 1 ./... including ONLINE no location, IN_PERSON coarse area, HYBRID physical+online, invalid combinations and owner-only API, old Intent 1->1 stable ID and Activity 5->5, guarded down/reapply; go vet ./... PASS; go build ./... PASS; flutter analyze PASS; flutter test 95 PASS including Chinese online/hybrid UI; flutter build apk --debug with local map config and isolated Gradle home PASS; git diff --check PASS. Draft-only, no public discovery or production deployment.

### BT-V4-INT-003 — Implement Intent audience and visibility policy

2026-10-01: pwsh automation/verify_social_intents_migration.ps1 -Through 37 PASS disposable PostgreSQL 001-037, full go test -p 1 ./... including TestSocialIntentAudienceAuthorizationIntegration (draft/privacy, FRIENDS Tie, COMMUNITY active membership/revocation, LOCAL explicit current City Context, PUBLIC anonymous/profile visibility, INVITE_ONLY/revocation, Block, unauthorized targets), HTTP public list/detail tests; legacy Intent 1->1 same ID, Activity 5->5; 037 guarded down/reapply PASS. Independent 036 regression script PASS. go vet ./... PASS; go build ./... PASS; flutter analyze PASS; flutter test 95 PASS; debug APK build with local map config/isolated Gradle home PASS; git diff --check PASS. No user publication flow or production deployment.

### BT-V4-INT-004 — Implement Intent lifecycle and expiry

2026-10-01: pwsh automation/verify_social_intents_migration.ps1 -Through 37 PASS disposable PostgreSQL 001-037 with full go test -p 1 ./... including owner-confirmed DRAFT->ACTIVE, cancellation, unauthorized/unconfirmed/duplicate/expired rejection, public-profile prerequisite, audit, read-time EXPIRED and public disappearance; legacy Intent 1->1 same ID and Activity 5->5, 037 guarded down/reapply PASS. Separate 036-only script PASS with guarded down/reapply. socialintent lifecycle transition unit test PASS; go vet ./... PASS; go build ./... PASS; flutter analyze PASS; flutter test 96 PASS; debug APK with local map config/isolated Gradle home PASS; git diff --check PASS. MATCHED/CONVERTED domain transitions reserved for verified workflows; no production deployment.

### BT-V4-INT-005 — Bridge AgentTask intent parsing into first-class Intent

2026-10-01: pwsh automation/verify_social_intents_migration.ps1 -Through 38 PASS disposable PostgreSQL 001-038, full go test -p 1 ./... including TestSocialIntentTaskBridgeIntegration (anonymous/org/foreign/refine/area/unfinished/unconfirmed denied, explicit private draft created once, source provenance, search task unchanged); legacy Intent ID 1->1 and Activity 5->5; 038 populated down refused and empty down/reapply PASS. go vet ./... PASS; go build ./... PASS; flutter analyze PASS; flutter test 98 PASS; isolated Gradle flutter build apk --debug --dart-define-from-file=.env.maps.mobile.local.json PASS; git diff --check PASS. Explicit personal FIND_ACTIVITY task conversion only, no auto creation/publication or production deployment.

### BT-V4-OPP-001 — Implement Opportunity Engine v1

2026-10-01: pwsh automation/verify_place_context_migration.ps1 PASS disposable PostgreSQL 001-039 and full go test -p 1 ./... -count=1 incl deterministic opportunity fixtures and TestOpportunityHTTPRealSupplyIntegration (active own Intent -> actual authorized Activity/Place refs, anonymous/org rejected, invite-only Activity and hidden Place removed); existing Place/Activity/legacy Intent IDs unchanged, 039 guarded down/reapply PASS. Fixed socialintent Place UUID validation and tested full/truncated UUID. go vet ./... PASS; go build ./... PASS; git diff --check PASS. Previous Flutter analyze/test 100/debug APK PASS; no Flutter changes for this task. Rule-based API only, no synthetic production supply, auto action, client consumption or deployment.

### BT-V4-OPP-002 — Make routing Tie-aware

2026-10-01: deterministic TestGenerateRanksExistingTieThenJoinedCommunityThenOtherSupply PASS (existing Tie > joined Community > public > other authorized, revoked inputs remove reasons/rank); pwsh automation/verify_place_context_migration.ps1 PASS disposable PostgreSQL 001-039/full go test -p 1 ./... incl TestOpportunityHTTPRealSupplyIntegration with real friend request/accept, active Community join, Block/leave revocation, private Activity/hidden Place filtering; existing Place/Activity/legacy Intent IDs unchanged and 039 guarded down/reapply PASS. go vet ./... PASS; go build ./... PASS; git diff --check PASS. Earlier Flutter analyze/test 100/debug APK PASS; no Flutter changes this task. Friend-of-friend/closeness not inferred without permission; owner-only rule-based routing, no production deployment.

### BT-V4-OPP-003 — Add privacy-safe new-people routing

Canonical NEW-PEOPLE-ROUTING-V4; 052 default-off explicit consent + mutual active/visible source SQL matching/minimal JSON and transactionally revalidated confirmed friend invitation; Chinese independent draft/preview/query/invite UI and generic Agent entry no Pin; 001-052 migrations/three seeds/full Go SQL+HTTP/old IDs/down-reapply PASS; privacy/matching + 60 Consent/Block/Cancel lock races PASS after actual 40P01 fix; Go vet/build PASS; Flutter analyze 0 issues/147 tests/debug APK PASS; installed Android16 phone and local API fixture lifecycle/revocation/restart/debug verified, 10 PNG + API IDs/request logs in docs/testing/evidence/new-people-2026-10-02/README.md. Local synthetic only; Closed Pilot NO.

### BT-V4-OPP-004 — Expose explainable opportunity reasons

2026-10-02 当前授权活动title/placeName、明确组织主办和有效accepted好友依据、严格本人API、活动/新朋友中文代码隔离、Now/设置消费与PRIVATE草稿预览确认和授权详情已实现；001–052/3seed/full Go SQL+HTTP/旧ID/down-reapply、Go vet/build、Flutter analyze/189 tests/APK PASS。Android16 实际源任务保存私人DRAFT、预览返回不写、确认后3条授权机会/中文理由、同ID详情、取消/App+API重启 PASS；参与/申请/会话/Tie/Plans保持基线。证据 docs/testing/evidence/opportunity-reasons-2026-10-02/README.md，9截图+ID/request日志。仅本地合成；Closed Pilot NO。

### BT-V4-AGA-001 — Define Agent-to-Agent permission contract

2026-10-02: canonical docs/architecture/AGENT-TO-AGENT-PERMISSION-CONTRACT-V4.md + agentruntime coordination strict wire/pure deny-by-default policy and least ASK_USER response; verified typed owner/Agent/session/completed FIND_ACTIVITY/resource, accepted request + active matching Tie/noBlock, independent human grants bound to request/task/agents/principals/purpose/scope/resource/fields/TTL/current revision. Contract/security 430 PASS events; fresh001-052 + all3devseeds default parallel fullGo3 rounds each766 PASS/0FAIL/0skipped-tests; go vet/build exit0; fresh migration052 fullGo/defaultparallel/oldIDs/guards/down-reapply exit0. Current built API ready200 and3Agent transport routes404 with requestIDs/hash. Full regression found Community unused$2 SQL42P18, sameownfixture pre-fixFAIL/post-fixPASS and corrected contiguousparams preservingowner/admin/audit; crosspackage fixture borrowing/publicglobal-count assumptions fixed with owned mutable data, exactIDs/strictcleanup and all3fullroundsPASS. All initialFAIL and finallogs/reproducer archived docs/testing/evidence/agent-permission-contract-2026-10-02/README.md. Contract-only, no transport/live consent/tool/UI, no newFlutter/schema; AGA002 remains realcoordination work; ClosedPilotNO.

### BT-V4-PLC-001 — Promote Place to canonical social-context entity

2026-10-01: pwsh automation/verify_place_context_migration.ps1 PASS disposable PostgreSQL 001-039/full go test -p 1 ./... -count=1 incl TestPlaceContextIntegration (sourced candidate address, independent review, stable Place ID, Activity/Moment FK, invite-only Activity/private Moment non-leak, hidden/expired Place 404); prior Place/Activity/legacy Intent ID sets unchanged; 039 populated down refused, empty down/reapply PASS. go vet ./... PASS; go build ./... PASS; flutter analyze PASS; flutter test 100 PASS; isolated Gradle debug APK build PASS; git diff --check PASS. Existing public Place/Agent/map IDs preserved; no production data/deployment or Venue/Business claim.

### BT-V4-VEN-001 — Add Venue capability model

2026-10-01: pwsh automation/verify_place_context_migration.ps1 -Through 40 PASS disposable PostgreSQL 001-040/full go test -p 1 ./... -count=1 incl TestVenueEditorialIntegration: anonymous/non-editor denied, independent review, pending/hidden/expired invisible, optional operator null, duplicate review and audit; old Place/Activity/Intent ID sets unchanged; 040 populated down refused, empty down/reapply PASS, 039 guard/reapply PASS. Go vet ./... PASS; go build ./... PASS; git diff --check PASS. Synthetic editor/venue only; no real venue/operator claim or production deployment.

### BT-V4-BIZ-001 — Add Business principal and Place/Venue ownership relations

2026-10-01: pwsh automation/verify_place_context_migration.ps1 -Through 41 PASS disposable PostgreSQL 001-041, pre/post legacy Account/Agent/Organization/Place/Activity/Intent IDs and organizer ownership unchanged, full go test -p 1 ./... -count=1 incl TestBusinessOrganizerAuthorizationIntegration (synthetic verified principal/venue only): anonymous/member/outsider/pending claim/pending venue relation denied, verified owner create/publish, hidden Place/revoked operation 404; 041 populated down refused, empty down/reapply and 040/039 guard/reapply PASS. go vet ./... PASS; go build ./... PASS; flutter analyze PASS; flutter test 102 PASS; isolated Gradle debug APK PASS; git diff --check PASS. No real merchant claim, console, Business Agent, or production deployment.

### BT-V4-PLC-002 — Add semantic Place Profile

CODE_LOCAL：七字段Place语义profile真实持久candidate→独立City reviewer→发布/撤回、current Session/source/PGtime/CAS/immutable version，未知nil不猜 amenity；EDITOR_ASSESSMENT_UNCALIBRATED不是事实概率。原Venue硬门槛与IntentCategory/participants语义局部排序不被覆盖。五实际注册HTTP/current place-matches真消费。worker fresh001–067三轮各122PASS/0FAIL-SKIP/vet-build0/17freeze，root独立fresh067再122PASS/0FAIL-SKIP/vet-build0，三非空pending/published/withdrawn down各exit3完整public原子保持，emptydown/reapply0、8旧非空源及全public/17owned/allAPI SHA稳定、自有库DROP。worker783档案+17当前prod根逐SHA重核，archive-manifest 188A2C9F4B3D41DC2F6E55E69A114904D0AD3F783E9774EB786EBFEB39EB17F0，根126档案manifest d09d5e67aab9329f6248fd15a30c73771b87143b639122734b59eb92b5abef80 receipt在docs/testing/evidence/organization-agent-capability-2026-10-03/root-place-final/receipt.json。初次fixture/vet/compile/withdraw-down-probe失败保留，当前shared068另待root051冻结，不借066或067称当前全Go已验。无client改动/真实Place材料或真机场地UI未验/模型Unavailable/Closed Pilot与ConsumerBeta NO。

### BT-V4-PLC-003 — Implement Intent-to-Place matching v1

2026-10-01: Deterministic placematch unit fixtures PASS for reviewed category/capacity, explicit Place unknown, FIND_COMPANION, wrong owner/draft/online/expired/other city/empty supply. pwsh automation/verify_place_context_migration.ps1 -Through 41 PASS disposable PostgreSQL 001-041/full go test -p 1 ./... -count=1 incl TestPlaceMatchingRealReviewedSupplyIntegration: anonymous/other Person/Organization denied; synthetic reviewed Venue match; low capacity/hidden Place/expired Venue removed; empty supply empty. Legacy IDs/organizers preserved; 041/040/039 down guards/reapply PASS. go vet ./... PASS; go build ./... PASS; git diff --check PASS. Rule-based own API only, no Flutter consumer, booking, real venue verification or deployment.

### BT-V4-PLC-004 — Normalize Activity ↔ Place/Venue relation

2026-10-01: migration 042 Activity modality/place-state/optional reviewed Venue relation with old IDs preserved; API validates legacy and explicit in-person confirmed/TBD/online, current public Place and Venue, DB checks/trigger + publish guard; Chinese editor/detail/navigation. pwsh automation/verify_place_context_migration.ps1 -Through 42 PASS disposable PostgreSQL 001-042 and full go test -p 1 ./... including legacy IDs, reviewed Venue link, TBD/online transitions, hidden Place and mixed-state rejection, 042/041/040/039 guarded down/reapply. go vet ./... PASS; go build ./... PASS; flutter analyze PASS; flutter test 104 PASS including new location detail widgets; flutter build apk --debug with ignored local map config and isolated Gradle home PASS; git diff --check PASS. Synthetic development evidence only; no deployment or real Venue.

### BT-V4-MOM-001 — Link Moment to Place/Activity/Community context

2026-10-02 repository-only: migration 043 adds FK-backed Community/Organization Moment links and reuses existing Place/activity links, with guarded down; Go Person-only Moment HTTP, owner-only private read/write, same-city/visibility/membership validation, explicit clear versus omitted preservation and transaction rollback; Flutter Chinese private draft context choices and token-isolated list. pwsh -NoProfile -File automation/verify_place_context_migration.ps1 -Through 43 PASS on disposable PostgreSQL 001-043 with full Go DB/API tests, legacy Moment ID preserved, 043 protected down/reapply and 039-042 regressions. go vet ./... PASS; go build ./... PASS; flutter analyze PASS; flutter test 108 PASS including context controller and widget choice; isolated Gradle flutter build apk --debug --dart-define-from-file=.env.maps.mobile.local.json PASS; git diff --check PASS. No public Moment/Place Memory or production pilot claim.

### BT-V4-MOM-002 — Aggregate Place Memory

CODE_AND_LOCAL_VERIFICATION: privacy-safe bounded Place public sharing/published-arrangement patterns/reviewed seven-field suitability, current owner/session/source/withdraw/block/precision and Org workspace boundaries. Root current fresh001-070 full scoped PlaceHistory+MomentPublication80 PASS/0FAIL-SKIP/test-vet-build0, 606 observed API/SQL/mod sources, complete public/catalog unchanged,owned DB actuallyDROP; work/v5-age051/moment-current-final/native-current1/result.json. Root independently verified worker-final2 726 immutable files and phone device-workspace3-final3 243 files (manifestSHA c7a2de47834bb282422b2124f1a148a5074eace7c62339c8064fece0d80fc4ee),24 current products. Full Flutter analyze/test/Debugbuild0,188 stable source,316 namedPASS/69 loading separately; actual installed APK SHA7d2a7ceffc152c35459b0c67995416544e89c17c03f074a261ef9da04bebd819. Real local UI newsyntheticMoment2edfb2af-9189-4e38-98d6-67699b00bdab private draft rev1→exact public approval rev2→withdraw rev3; cancel preview13domain+audit unchanged,public history0→1→0,oldprivate910107 whole rev5 unchanged, Org actual Place entry hides personal approval/records,returnPersonal works,font1.6 tested restored1.0. Read-only review SHA97c29f792b87768bb4d5365ae67602658a4d61e9f174bc89b9397a280b6bf35b. Arrangement/suitability positive native cases complement phone empty states; no attendance/inference/recommendation-ranking claim. App/APIrestart,TalkBack,landscape,external keyboard NOT_RUN; requestIDwrite notcaptured; local synthetic Debug/dev_phone only,not production/CSSA/real identity/pilot;ClosedPilotReady=NO,ConsumerBeta=NO.

### BT-V4-BIZ-003 — Implement Business Claim + Console Lite

CODE_AND_LOCAL_SYNTHETIC_VERIFICATION: current owner/admin claim-profile-venue console plus owner member lifecycle and independent finite current-source review implemented native070/HTTP/Chinese UI. native-final2 36PASS/0FAIL-SKIP/test-vet-build0; schema2 fresh up/atomic nonempty down refused/empty down restores069/reapply/full-old-public preserved; ui-target11 34functionalPASS/4loading/analyze-test0/font1-1.6, real RED2 then return-to-edit preserves draft and invalidates approval. whole-go071-3 8487PASS/0FAIL-SKIP/test-vet-build0/631Stable/fullpublic-catalog unchanged/ownedDROP. full-client5 analyze-test-Debugbuild0/497functionalPASS+75loading/179Stable, APK1119c68621eafb5a5e3f4ca1a9c6f10d9687f9b512ce3582cddc9f52d7074175 ADB install Success Android16. Actual phone cancellation profile1/audit5 unchanged; explicit submit profile2 pending/audit6 sameBusiness. Root independently197 archive files byteSHA zero mismatch, manifest889aaa6b8cd9dcdbbff00e8bbe6bcf8aaee7f377ccab4abb1327b729ef3b2b34 at docs/testing/evidence/business-claim-console-2026-10-03/root-final1; receipt work/v4-biz003/root-business-delivery.json. Original failures/cache failures/interruption retained. TalkBack/landscape/device revocation/App-API restart/realCSSA A-H NOT_RUN; no production merchant verification/operational review/IdP/Agent model enablement. ClosedPilotReady and ConsumerBeta NO.

### BT-V4-BIZ-004 — Support verified availability/booking-link metadata

CODE_LOCAL verified booking metadata and current-source finite URL approval: worker-final2 60 targeted PASS, root verified final3 32 archive files bytes/SHA with manifest a6c3dbc03cfaf1352f70bf0d05bf9e7761ef4c1f4788ab52c52c53367ce4b1ee; current full-client9 564 functional PASS/analyze/test/debug APK build0 and source189 stable; Go8506/vet/build0/source640 stable. Actual Pandora Android16 APK9 d06fe8102c009f2f5f8ca6dc47b22afecf8b1ce717ce1e7f7492a50be2963551 Now venue query -> native Place -> exact URL/source/review/expiry preview -> Cancel returns same detail; independent PNG review. Existing native040 contributor plus independent reviewer publishes approved metadata; private070 booking data not exposed implicitly. No external site launch, real booking, production identity or TalkBack acceptance; ClosedPilot/ConsumerBeta NO. docs/testing/evidence/business-booking-metadata-2026-10-03/worker-final3/manifest.json; work/v4-org003/root-final-validation.json

### BT-V4-ADS-001 — Separate organic recommendation from sponsored opportunity

CODE_AND_LOCAL_SYNTHETIC_VERIFICATION: independently typed ACTIVITY/PLACE sponsor declaration/review with finite dedicated current reviewer grant/source/session and exact version; approved sponsor lane separate from unchanged organic array/ranking/ResultSet/Pin/reasons. Native12 two rounds152PASS and root-sponsored1 independent152PASS/0FAIL-SKIP/test-vet-build0/oldpublic preserved/071 nonempty down refusals-emptydown-reapply/DROP; root full whole-go071-3 default8487PASS/0FAIL-SKIP/vetbuild0/631Stable/fullpublic-catalog unchanged/DROP. Strict UTC DTO real red malformed25hour then147 functionalPASS+2loading/analyze0. Full-client5 analyze-test-Debugbuild0/497functional+75loading/179Stable; APK1119c68621eafb5a5e3f4ca1a9c6f10d9687f9b512ce3582cddc9f52d7074175 installedSuccess Android16. Actual phone ordinary Now badminton→personal opportunities organic3→fixed Sponsored disclosure→same typedActivity detail, before-after organic arrays exactly unchanged; no payment/real merchant verification. Immutable worker-final1 1057 files manifest28ff6e34a078c2d7c2f8ebd85d0ab0a8bbb874ec76aa70e117c5849418e9e53c; final2 22 UTCdelta manifestcd6b3915da4b4fbaee8f31a7a35ec5662e8b42eb888c699b05612f2f79c0330b; final3 33 final evidence/doc manifest440d60645c2a9d226b5a83649da26d8dcce6ce8bd1bdcba83e3cc93b9e2e40bc, root independent bytes/SHA verified all and current21 ADS sources equal. Root receipt work/v4-biz003/root-sponsored-final2-validation.json; canonical/docs/testing/evidence/sponsored-opportunity-trust-2026-10-03. Not native booking, payment, ads production, model activation; TalkBack/device revocation/App-API restart/realA-H NOT_RUN; ClosedPilotReady/ConsumerBeta NO.

### BT-V4-COMM-001 — Reconcile current Community implementation against canonical model

2026-10-01: docs/research/BIRDTIE-V4-COMMUNITY-RECONCILIATION.md maps identity, owner/member lifecycle, PUBLIC/PRIVATE/HIDDEN, join policy, activity organizer XOR and UI gaps to existing additive tasks. Fixed HIDDEN known-ID disclosure and invitee list limited detail in postgres/community_social.go; Go store/HTTP integration assertions added. automation/verify_community_migrations.ps1 PASS (030-032 up/down/reapply, 5->5 organizers, invariants, repeated seed); automation/verify_place_context_migration.ps1 -Through 42 PASS (full Go DB/API tests, old IDs, 039-042 down/reapply); go vet ./... PASS; go build ./... PASS; flutter test test/community_api_test.dart test/community_page_test.dart 4 PASS; git diff --check PASS. Synthetic repository evidence only; not deployed/pilot.

### BT-V4-ACTY-001 — Generalize Activity organizer to ActorRef

031/041 migration activity_organizers four-FK exactly-one plus stable Organizer{type,id}; CreateSocialDraft and Organization route enforce Person self, Community/Organization owner/admin, verified Business principal Owner/Admin and verified Venue relation. Added GET /v1/me/activity-organizers/businesses filtered by active verified Business and owner/admin, Flutter Chinese organizer picker with old-API 404 compatibility. Disposable 001-044 via pwsh automation/verify_place_context_migration.ps1 -Through 44 PASS full Go API/DB and up/down guards; business integration covers anonymous, Business account, pending claim, member, outsider, owner/admin, publication/revocation; flutter analyze PASS, flutter test 111 PASS, go vet ./... and go build ./... PASS; Flutter debug APK rebuilt and installed on Android16 via flutter run with stored Mapbox config and USB-local API. Synthetic development evidence only; no real Business claim or Closed Pilot proof.

### BT-V4-ACTY-002 — Normalize Activity modality and visibility

2026-10-02: 032/041/042 server-side public/member/invite-only, invite manager guard and online/in-person/hybrid location policy reconciled; added hybrid Chinese editor/detail/share and truthful visibility copy. Disposable PostgreSQL 001-044 + full Go DB/API policy tests, old ID snapshot, guarded down/reapply PASS via pwsh automation/verify_place_context_migration.ps1 -Through 44; added non-organizer invite rejection and hybrid TBD draft/publish/public read tests. flutter analyze 0 issues; flutter test 113 PASS; go vet ./... and go build ./... PASS. Android 16 c641566b debug APK built, installed and Flutter debugger attached; preview package app.civu.civu_mobile.birdtiepreview. Development synthetic evidence only; online participation link, authorized live supply and pilot deployment not claimed.

### BT-V4-ORG-001 — Migrate Organization Agent to shared capability framework

CODE_LOCAL：原registered组织问答真实native共享capability，保留原中文known/unknown与sourceID，只读当前核验公开org/profile/FAQ/activity；未知/私密/过期/撤核/成员私人资料拒绝，Organization Memory/model/purpose不授予。root native6真实69PASS/0FAIL-SKIP/vet-build0/9owned与428源码前后同SHA/全public原行保持/ownDBDROP；actualAccount-beforeSession/原真人Profile写无死锁、Session初始及最终真实锁等待absolute/idle过期401、RC原FAQ锁等待撤核转私密拒绝。work-only真实历史源码控制旧session7failEvents和旧FAQ4failEvents→final正负全PASS，初次expiry-fixture与编辑失败保留。共同schema066完整默认Go三轮每轮7807PASS/0FAIL-SKIP/vet-build0/557源与1288归档相符；当前更晚PLC067未据此声称通过。根review/原始源码/JSONL/DDL全行归档4108文件manifest e6791175665ad3c50f146b9c9be2f5f98725a5f3b5d80d0e5fca24d88a96b36c；docs/testing/evidence/organization-agent-capability-2026-10-03/root-local-final/receipt.json。真实CSSA材料/生产身份/正式试点未核验，Closed Pilot/Consumer Beta NO。

### BT-V4-COMM-002 — Build Community discovery and durable membership UX

CODE_AND_LOCAL_VERIFICATION_DONE。中文社区发现/join/request/leave/member/privacy和原域管理可用，公开活动不要求membership；当前真人Session与具体版本确认严格，不以Agent/Profile授权普通人类。原生native7 fresh001-068两轮各58PASS/0fail-skip/vet-build0，根独立root-community1再58PASS/0fail-skip/vet-build0、10owned及全API SHA/public保持、自有库DROP。目标Flutter30PASS/analyze0；根完整Flutter analyze0/278非hiddenPASS/Debugbuild0/175客户端源同帧，APK EEA7C143445FD53F2F413CCE9C731F43B912E42183B4F43706F17604F7C53DB3。根逐SHA核验worker840档案及20产品源，根280不可变档案manifest fad6a19af3f7f5bcae9ee022e7ff273771fad6b06403e2f6218213aadcde902d，receipt docs/testing/evidence/organization-agent-capability-2026-10-03/root-community-final/receipt.json。最初原生越权/等待撤权RED、fixtures/setup/触达/ABA失败及真实修复raw保留。实测transfer只转membership owner，原creator及059/061源权限独立保留，不伪造createdBy或称源权完整迁移。合成Windows widget截图非手机，当前APK手机/TalkBack/生产release NOT_RUN。当前整库Go第三轮Outbox未复现失败由root另追，不称完整Go已过；Closed Pilot/Consumer Beta NO，未部署。

### BT-V4-ORG-002 — Unify actor-specific activity ownership authorization

2026-10-02: Four-actor server ownership matrix reconciled. Organization legacy and Social Activity paths require active organization principal and active Person owner/admin; Social manager rechecks active Person and each Community/Business role. Added disposable DB/API tests: Person only self; Community ordinary member denied and admin edit; Organization outsider/member denied create/edit/publish/cancel, admin edit, owner publish/cancel, disabled principal denied; Business ordinary/outsider denied, admin publish, owner edit/cancel. Migration 045 permits pure cancellation after reviewed Business Venue relation revocation while other writes remain guarded. pwsh automation/verify_place_context_migration.ps1 -Through 45 PASS: 001-045 full Go tests, stable ID snapshots and 045/044/043/042/041/040/039 down/reapply guards. go vet ./... and go build ./... PASS. 045 applied only to isolated phone-review development DB, API restarted on 127.0.0.1:3697; Android debug preview still attached via USB reverse. No production identity or real pilot claim.

### BT-V4-ORG-003 — Normalize verified public profiles for organizations and businesses

CODE_LOCAL explicit072 permission/CAS/audit and truthful public Organization/Business canonical profiles. Root current source matches native6 19 PASS and fresh001-072 whole-go072-2 8506 PASS/0fail-skip, vet/build0,640 stable, full public rows/catalog unchanged, owned DB dropped; schema1 current072 DDL SHA atomic nonempty down exit3 and empty down/reapply verified. Flutter full-client9 564 functional PASS/0fail-skip, analyze/test/debugbuild0,189 stable/APK9 installed. Actual synthetic phone publish0->1 audit0->1, checkbox0effects; revoke cancel1/1 unchanged and confirm2/2, public links/description cleared while canonical public Activity retained; owner, Place/Activity organizer, same Activity detail Chinese UI. Independent review defects fixed. Immutable1188 files manifest887f70d511abc34fae06f211162d254fb9772f3fa394c31076ae550c72f0f479 independently bytes/SHA checked. Race/TalkBack/unaided users/external official links/production credentials/true A-H NOT_RUN, gates NO. docs/architecture/public-supplier-profile-v4.md; docs/testing/evidence/public-supplier-profile-2026-10-03/root-final1/manifest.json

### BT-V4-NOW-001 — Refactor Now into context-aware social workspace

CODE_LOCAL original acceptance: native authorized non-CITY ONLINE declared Context query and original Task/Intent identity; answer first, no map point assumption, real current detail action, preserved CITY map. Root work/v5-age038-resume/three-task-acceptance32.json and parallel-checkpoint31.json: whole082 9718PASS/0FAIL-SKIP/test-vet-build0/794 stable+3 original seeds/up-down-reapply/oldpublic-catalog-xmin/owned DB DROP; whole current Flutter991functional+120loading/analyze-test-Debug build0/259 stable. APK44a2506 installed c641566b; real native local synthetic ONLINE query/original detail/same Task four turns/App+API process restart and UI restore; old chat12 hash unchanged. Native34/Dart90 target, source-freeze2 all30 independently matched and both worker archives verified. Prior PARTIAL history and failures retained. TalkBack/independent six-human/provider/real supply/true cross-city Opportunities and future typed/action tasks not asserted. Closed Pilot and Consumer Beta NO.

### BT-V4-MAP-001 — Add typed map layers for Opportunities/Activities/Places/Moments/Org/Business

ROOT actual acceptance work/v5-age038-resume/three-task-acceptance36.json: frozen Go 9897 PASS/0 FAIL/0 SKIP test/vet/build exit0; Flutter 1037 functional PASS/0 FAIL analyze/test/Debug build exit0; worker final source/archives hashes verified; current physical Android phone proof36 13 checks true including original IDs, preview zero writes, RESET zero writes, restart persistence and anonymous close. CODE_AND_LOCAL_VERIFICATION only; Closed Pilot/Consumer Beta NO; failures and NOT_RUN preserved.

### BT-V4-NOW-002 — Add Active Intent model to Now

ROOT actual acceptance work/v5-age038-resume/three-task-acceptance36.json: frozen Go 9897 PASS/0 FAIL/0 SKIP test/vet/build exit0; Flutter 1037 functional PASS/0 FAIL analyze/test/Debug build exit0; worker final source/archives hashes verified; current physical Android phone proof36 13 checks true including original IDs, preview zero writes, RESET zero writes, restart persistence and anonymous close. CODE_AND_LOCAL_VERIFICATION only; Closed Pilot/Consumer Beta NO; failures and NOT_RUN preserved.

### BT-V4-NOW-003 — Add My Network / Social Now surface

CODE_AND_LOCAL_VERIFICATION: worker immutable1278 manifest95a4af150f34f2e75d0eb4d488aad1d65222b0cf85972611ce2b9e0fb3248302, two fresh068 scope28 PASS/0 FAIL-SKIP/vet-build0; root independently28 PASS/461Go and fullpublic stable/ownedDB DROP; current15 product hashes verified. Captured175-source Flutter analyze/test/build0, actual278 visiblePASS, saved EEA7 APK actualADB installSuccess. Android Now+Settings paths/Chinese bounded empty/refresh/font1.6 scroll/font restore1.0 and no source writes; root576 archive manifest66f68e3fab83f2363b9450c18f80ba1cfbc82912a963e3a5ed2671f9dce3dca4. Private longterm friend interests not inferred; native positives use local fixtures. FullGo full3 round1/2=8133 PASS, third=8131 PASS with two root diagnostic failures real276usPGclock reversal outside NOW15 scope, bounded recovery47 PASS/new full pending. CurrentApp/APIrestart automatic approval rejected blocked by policy reason absent, TalkBack/real CSSA/IdP/release NOT_RUN. ClosedPilotReady/ConsumerBeta NO.

### BT-V4-NOW-004 — Generalize Agent ResultSet/entity cards to V4 entity types

CODE_LOCAL original typed7 stable original Ref/Source/Task/Session/expiry/ABA original ACL native implementation; all seven real original domain read/share SQL, captured legacy DTOs and HMAC native projection, original authorization/immutable source unchanged; model/public builder not broadened; private activity native human only/public commercial excludes; unsupported extra opportunity filter explicitly refused Chinese. Root now004-root-code-acceptance38u.json independent all18Go/Dart freeze2SHA,352 archived bytes. Frozennative22 target80PASS0FAIL-SKIP/test-vet-build0/publiccatalog/drop;pure174PASS/Fluttertarget89/analyze4=0; root currentwhole corresponding casesPASS/HTTPpkgPASS and fullFlutter1118PASS0FAIL-SKIP/analyze-test-Debugbuild0/296stable. WholeGo38p10080PASS4FAIL2unrelatedAIR015 fixtures tracked originalreopened; not wholePASS. Phone currentold081 testsPlacecompat only; newnative7 deployed phone/realusers/AT/provider not claimed. Defaultmodel/AgentWrites/A2A OFF, ClosedPilot/BetaNO. OriginalRED native18/root38m raw compatibility failures/fixes preserved.

### BT-V4-NOW-005 — Implement non-map flow for online intents

ROOT verified work/v4-now004-results/now005-006-root-acceptance1.json; NOW005 native32/cross-city/two-process E2E + client24; NOW006 native34 + client15 + shared-state27; whole current Flutter analyze0/test1087 no fail skip/build0,293 sources stable. Local code acceptance only; whole-current Go/device/TalkBack pending; Pilot/Beta NO.

### BT-V4-NOW-006 — Add current/destination context switching

ROOT verified work/v4-now004-results/now005-006-root-acceptance1.json; NOW005 native32/cross-city/two-process E2E + client24; NOW006 native34 + client15 + shared-state27; whole current Flutter analyze0/test1087 no fail skip/build0,293 sources stable. Local code acceptance only; whole-current Go/device/TalkBack pending; Pilot/Beta NO.

### BT-V4-ACTN-001 — Normalize entity actions across BirdTie

CODE_AND_LOCAL_VERIFICATION: work/v5-age038-resume/actn001-root-whole-go-proof38ai.json and actn001-root-whole-client38aj-proof.json. Root raw10154 Go functional PASS/0FAIL-SKIP, test/vet/build+2CLI0; 853+3seed live/archive bytes and public/catalog083 up/down/reapply/xmin preserved; owned DB DROP. Flutter whole1180 functional+145loading/0FAIL-SKIP, analyze/test/Debugbuild0,305 frozen live/archive sources; immutable APK50f133fb1150a8d54e8f93b792b0f270a366197eaaf326612931d28813934cf1. Root final4 archive96 and74 owned sources byte-verified; target164/native66. Closed6 contract reused current native transactions/actors/source versions and final confirmations, no generic executor/newledger; anonymous/session/org/switch/revocation/cancel/late/unknown boundaries tested. Previous final38ah Debug APK successfully installed/pulled-identical and realDebug attached; local public export review/cancel observed. Final one-line profile-login direction built, phone check NOT_RUN because user ChatGPT foreground. Private phone writes,IME/pin,dark native map,controlledProfile,TalkBack and real users NOT_RUN; existing MAP002 and release gates retain these. Original failed fixtures/unsupported reason/full regressions/timeout preserved; City RR repair remains PARTIAL/UNKNOWN. No provider/autonomous/A2A/model or pilot activation; ClosedPilotReady/ConsumerBeta NO.

### BT-V4-PLN-001 — Integrate Intent/Activity/Place with Plans

CODE_AND_LOCAL_VERIFICATION ONLY. PLN original RSVP/Plans and FIND_ACTIVITY own explicit conversion reuse original Activity/Participation IDs, confirmed/TBD/ONLINE native place/time, stable detail, current identity/source/deadline/ACL and encoded-result revalidation. work/v4-pln001-20261005/conversion-native9: go test targeted64 PASS/0FAIL-SKIP; test/vet/build+2CLI0; schema085 old complete rows/catalog/xmin, unused down/reapply, used-down deny, actual twoOS API restart association persists; ordinary RSVP no Agent prerequisite. Dart target15:46 PASS/analyze0; IME200/260 at320/font3 confirmation/cancel48dp reachable. Root byte/hash proof work/v5-age038-resume/pln-final-root-proof38bl.json verifies25GoSQL+14Dart and2338 archive files. Final immutable wholeGo38bk:10248 PASS/0FAIL-SKIP, test/vet/build+2CLI0,880 input bytes stable; root whole085-root-proof38br.json independent DB absent/snapshots. wholeFlutter38bj:1235 functional+149 loading PASS/analyze/test/Debug build0,313 input stable. Real device c641566b adb install-r-t PASS and installed APK SHA5bcfb8ac32b89e08b568e4d408c16412724b9eb241b251c0e1bf2282f64b5af3 verified; Debug attached VM31053/asserts/DevTools200. Current physical conversion/Plans scenario, TalkBack and Profile performance NOT_VERIFIED; local synthetic DB85/no real IdP or pilot. First whole38bf three Place loader503 fixed by exact shared scanner reuse, rawRED retained. Separate Run after_commit firstRED/currentGREEN with unchanged code: original root cause UNKNOWN,2s lease only hypothesis; AGE028 existing PARTIAL unchanged. Other intent types and unknown area/platform/group-size unsupported; old CONVERTED NULL unknown. ClosedPilot/ConsumerBeta NO; no production release/activation.

### BT-V4-NOT-001 — Add meaningful social/opportunity notifications

CODE_AND_LOCAL_VERIFICATION: actual native activity-change/invitation/connection-request/current-rule-grounded opportunity four classes with current source/session permission, five routes and eight-category preferences. Root fresh001-069 native369PASS/0FAIL-SKIP/test-vet-build0/fullPublicPreserved/DBDROP at work/v5-age051/root-notifications1/result.json; root default wholeGo current0708251PASS/0FAIL-SKIP/vetbuild0,606 observed Go/SQL/mod frame stable/public-catalog preserved/DBDROP at work/v5-age051/current070-full/full1/result.json. Independent archive3591+phone191 files byte/SHA0mismatch,current27 products0mismatch; work/v5-age051/root-notifications-phone-verification.json,review SHA7ab68ed549127d95981e2ccf58c3d3d1459358a241d4aca0a5d90bbbaeab4963. Actual local phone Inbox/Settings Chinese direct entry,V0→BLOCK V1→disabled V2,previewcancel selecteddomainunchanged,font1.6 actions restored1.0. Actual synthetic other-Person publish Activity8cbadfcf-d479-4035-b08d-a93aac0d0a5e rev2 BLOCK noInbox;rev3 NORMAL exactly1Inbox,secondmatchingIntent noDuplicate;tapnative markRead→sameActivity/zeroRSVP. Source-owned MOM DB106tables91rows fullyunchanged; phoneclone originalSeed1row preserved(table addsotherQArow),oldSessions1row normalrevoke/idle changed,allotheroldpublicrows preserved. Current069 health200; installed188-source APK7d2a7ceffc152c35459b0c67995416544e89c17c03f074a261ef9da04bebd819/fullFlutter316 namedPASS/69loading/analyze-test-Debugbuild0 separateframe. Phone oldrequest disposed/canceled50321701ms thenaccountswitch,notlate200afternewidentity; widget/native late guards separate. 069 actualnonemptydown3atomic/emptydown0/reapply0/catalogexact/DBDROP,initialfailuresretained. ProductionIdP/providerPush/DIGESTdelivery/TalkBack/AppAPIrestart/realpilot NOT_RUN;devsynthetic only;ClosedPilotReady=NO,ConsumerBeta=NO.

### BT-V4-ACTN-003 — Persist shared activity/context history

CODE_AND_LOCAL_VERIFICATION ONLY. Original human GET derives current bilateral defaultOFF shared communities/going activities and their explicit confirmed IN_PERSON-HYBRID public Place association, original IDs and source ACL; attendance/visit UNKNOWN. Native CurrentStore required, real Session/current source/PG clock and after-encode recheck, lawful Block-Unblock audit epoch prevents old result revival; no duplicate historical ledger/machine grant. Worker native7 and root-independent1 each55PASS/0FAIL-SKIP/fivechecks0, schema85 compatibility/full public/catalog/xmin and owned DB absence; unchanged7Go verified in latest087joint frame. Root immutable schema087 whole Go: 10345 PASS/0FAIL-SKIP/packageFAIL, test/vet/build and two CLI builds0; all895 Go-SQL-mod-seed inputs independently frozen/live byte matched, full public rows/visible semantic catalog/Participation xmin/unused087down-reapply preserved and owned DB independently absent. Proof work/v5-age038-resume/joint087-whole-root-proof38cz.json. Chinese original profile Panel strict wire, borrowed-owned client lifetime, transport/getter/listener/workspace/account ABA retirement, late response and320/font3/IME260/48dp tests. Real client clock/network lease RED2 retained, monotonic request elapsed now consumes original server lease; clocks only tighten. Target21PASS/analyze0; root whole Flutter1245functional+150loading PASS/0FAIL-SKIP/analyze-test-Debugbuild0,314 Dart/pubspec stable. Immutable APK75c36833f0ceca38c47c1e42ccc51307855abf7d75f2284785f04398ec2a5b90/242893717bytes not installed. Root all1319archive1+21archive2 bytes verified;11current7Go4Dart matchfreeze3, proofwork/v5-age038-resume/joint-final-archives38cy.json. New phone UI/positive real people/TalkBack/Profile comparison NOT_RUN; original authenticated ordinary permission not Agent/machine authorization, no coordinates/arrival inference. ClosedPilot/ConsumerBeta NO.

### BT-V4-BKG-001 — Expose booking action when verified booking path exists

CODE_AND_LOCAL_VERIFICATION only: root verified native8 schema084 35 raw PASS/0 FAIL-SKIP, test/vet/build 0, frozen868 exact bytes, unchanged complete public/catalog, exact owned DB independently absent; Flutter4 target87 PASS/analyze7 0, native missing-port panic RED and 320/font3/IME260 overflow108 RED fixed preserving 48dp. Verified public Venue CTA only on approved current path, current ACL/source/ABA/expiry/closed analytics, UUID idempotency, original040 vs070 privacy separation; CLIENT_REPORTED_EXTERNAL_OPEN separated from supplier confirmed UNAVAILABLE/UNKNOWN. Root proofs work/v5-age038-resume/bkg001-root-final-scoped-proof38as.json and bkg001-root-archive-proof38at.json (1219 files, SHA d3099958c1cb87353740847f51b60ab59353c403e6a91ccf8ca34ded6fe5dfeb). Shared server frozen then explicitly handed to PLN; routes preserved. Task verify UI/analytics tests fulfilled; joint085 whole Go/whole Flutter build pending current PLN final freeze, not claimed passed. Real external booking/provider receipt, current phone/TalkBack, deployed analytics/retention and Business Pilot NOT_RUN; ClosedPilot/ConsumerBeta NO. Earlier RED/runner missing DB identity history retained, no fabricated native7 DB.

### BT-V4-SAF-001 — Re-run and extend public precise-location privacy boundary

Root independently verified seven current SHA and 5217 immutable archive bytes; real registered-HTTP five late-revocation RED fixed with sealed native source+Session+PG-clock revalidation, native6 130PASS/0FAIL-SKIP and test/vet/build0. Current whole Go full083-parallel38 10027PASS/840+3seed stable, test/vet/build+2CLI0, actual migration up/down/reapply/full public+catalog preserved and ownedDB DROP. Whole Flutter7 1098PASS/294stable/analyze-test-build0. Code/local privacy acceptance only; actual OS-GPS-denied/TalkBack NOT_RUN; Closed Pilot/Beta NO. Receipt work/v5-age038-resume/saf001-root-acceptance38b.json.

### BT-V4-SAF-003 — Unify block/report/support across social entities

046 migration up/down guard and reapply, full disposable PostgreSQL Go API/DB integration PASS via pwsh -NoProfile -File automation/verify_place_context_migration.ps1 -Through 46; social report visibility/block/rate/receipt integration; flutter analyze PASS, flutter test 117 PASS, flutter build apk --debug PASS, go vet/build PASS, git diff --check PASS. Synthetic development only; no pilot operations or deployment evidence. docs/research/BIRDTIE-V4-SAF-003-AUDIT.md

### BT-V4-SAF-004 — Require consent for sensitive social inference/actions

docs/testing/evidence/agent-social-safety-2026-10-02/README.md: 203 pure policy PASS, 168 action/real-PG scoped PASS; fresh 001-052+three seeds default-parallel full Go 3 rounds each1112 PASS (0 fail/skip), vet/build and migration/old IDs/down-reapply PASS; Flutter analyze+198 tests+Debug APK PASS; current hash installed via ADB, Chinese query/followup same task, A logout/B login/App restart isolate history, nine business counts unchanged; dev only, no inference runtime/provider/live grants/autonomous effects. Closed Pilot NO.

### BT-V4-OBS-001 — Extend audit logs to social and agent actions

CODE_AND_LOCAL_VERIFICATION ONLY. Native private request context validated ASCII8-64, parameterized tx LOCAL correlation, no auth/consent/idempotency by trace; original actor/resource and nullable target/request in original audit. 086 seven original tables plus087 original organization public-map permission audit; explicit tested producer matrix in docs/architecture/social-agent-audit-correlation-v4.md, not all historic repository writers. Native11 fresh087 799PASS/0FAIL-SKIP/packageFAIL, five builds/checks0; original rows/catalog/xmin/up/down/reapply, used-down denial and077/078 append-only guards unchanged. Root verified60 source files and1257 archive2 files74916830bytes manifest959afd0a88df65809741204885e8a5df1f2aea96e86d6d778ec98259e87ea40f; old1272 archive1 unchanged. Root immutable schema087 whole Go: 10345 PASS/0FAIL-SKIP/packageFAIL, test/vet/build and two CLI builds0; all895 Go-SQL-mod-seed inputs independently frozen/live byte matched, full public rows/visible semantic catalog/Participation xmin/unused087down-reapply preserved and owned DB independently absent. Proof work/v5-age038-resume/joint087-whole-root-proof38cz.json. Real original whole1 RED10279PASS/7testFAIL/3packageFAIL and10account8audit residuals retained; six old tests now assert precise lawful minimal audit delta, preserved old rows/CAS/permissions and exact owned cleanup, no broad shared seed-actor deletion. Native9 original map correlation RED and native10 invalid-role fixture diagnostic retained; no fabricated SQLSTATE. No public audit API/new ledger/external log deployment/model activation; original actor-only Session window not newly fixed. Old Run after_commit failure root cause UNKNOWN and AGE028 PARTIAL unchanged. ClosedPilot/ConsumerBeta NO; phone/AT/production audit-operation not verified.

### BT-V4-E2E-003 — E2E: Organization/Community/Business actor authorization

actor_authorization_matrix_integration_test.go: one person Owner on one and Member on two other principals, outsider; Organization/Community/verified Business create/edit/publish/list/public organizer projection and suspended Owner checked. pwsh -NoProfile -File automation/verify_place_context_migration.ps1 -Through 46 PASS (disposable DB 001-046, full Go API/DB tests, ID preservation, down guards); go vet ./... and go build ./... PASS. docs/research/BIRDTIE-V4-E2E-003-AUDIT.md. Synthetic developer evidence only, no real IdP or pilot principals.

### BT-V4-E2E-004 — E2E: Cross-city / Online intent

CODE_AND_LOCAL_VERIFICATION; root independent fresh083 registered HTTP 5 functional PASS+1 child guard, 0 FAIL/SKIP, Go test/vet/build0; 72 requests and actual two OS HTTP restart PIDs26516/40724 with24 GET200; native8 worker same841 captured frame; actual uninvited Plans201 RED fixed original ACL, revoke/member removed/both block/PG City+Activity expiry/past/cancelled current authorization regression PASS; own stable identity/Agent/Ties/current contexts and complete public/catalog/owned cleanup including sessions unchanged;6959 archive files all original bytes SHA independently verified manifest b18bb9d922d3da3ec2f9bc77742418c65a08c8c06e2cfd968bfcfc643720a152; current two owned sources frozen exactSHA. Evidence work/v5-age038-resume/e2e004-root-acceptance38d.json and e2e004-root38d-native; docs/research/BIRDTIE-V4-E2E-004-AUDIT.md; docs/testing/evidence/cross-city-online-e2e-2026-10-04/worker-final1. Frame-specific tests not global live WIP NOW004 validation; subsequent joint whole Go/Flutter at NOW004 safe freeze. RealTwoHumans/phone/TalkBack NOT_RUN; ClosedPilot/ConsumerBeta NO.

### BT-V5-INT-001 — V5 Phase 0：增量映射、认知边界与权威接口

Phase0 native cognitive contracts/current self Profile-Context narrow readonly PostgreSQL adapters and Unavailable future ports; 112 cognitive and 180 parallel scoped PASS; 26 importer+20 dependency scheduling+workflow PASS with initial failures retained; old144 exact preserved, 108 append,137 sources6reuse3verify128delta, repeat0; fresh001-052/3seeds full default-parallel Go3rounds1224PASS each0FAIL/SKIP, vet/build/migration compatibility/down-reapply PASS, API ready200+5unopened probes404. Canonical ADR/Memory/product/source receipts and complete reproducible evidence docs/testing/evidence/v5-integration-2026-10-02/README.md. No persisted AgentProfile/Memory/provider/executor/liveA2A or newUI; Gates NO.

### BT-V5-AGE-001 — Agent Profile 基础模型

Unified six-field native AgentProfile metadata foundation and053 migration; stableAgent/account typed compositeFK, immutablebinding/+1revision, oldidentity+UserProfile untouched and newAgentbootstrap; Business reserved suspended/retired notenabled, internalGet/Ensure current binding only(nohuman authgateway/content/HTTP/UI), BusinessUnavailable.37model+41realPGstore+779parallelScopePASS;8concurrentEnsure,2connectionCAS1success1zero; defaultparallelfullGo3rounds1302PASSeach0FAIL/SKIP,vet/build;fresh/seededup,3seeds,fullmigrationGo1302,protecteddown+emptyreapplyPASS. Complete reproducible docs/testing/evidence/agent-profile-foundation-2026-10-02/README.md; initialtestcompilefailure retained. No Memory/provider/live or formalPilot; GatesNO.

### BT-V5-AGE-002 — Public Profile 与 Private Agent Profile 分离

docs/testing/evidence/agent-private-profile-2026-10-02/README.md: direct human Person-only private store/GET PUT separate from public four-field ACL; 87 private model, 34 real PG store, 161 HTTP spy +13 real route/DB, 1012 parallel scoped PASS; fresh 001-054+3 seeds full Go three rounds each1597 PASS zero test fail/skip; vet/build PASS; 054 fresh/seeded full-row preservation,33 strict SQL guards each, protected nonempty down/empty reapply PASS. CAS/revoke/expiry/reconnect/clear and public no-leak verified; all own DBs dropped. No client UI, field sharing or cognitive/provider permission; release gates NO; initial fixture SQLSTATE mismatch retained and corrected.

### BT-V5-AGE-003 — Profile Field Visibility

docs/testing/evidence/agent-profile-visibility-2026-10-02/README.md: final postreview local verification. Current final-statement guards for four legacy revocation windows/roster, denied same-handle fallback, native Person activity coarseACL AND field policy, trusted-origin maintainer masking; actual native16 PASS, historical race48/roster10/label510/handle753+2077 retained. 3 default full Go rounds and independent055 fullGo each2780 PASS,0fail/testskip; vet/build0, ready200/5unopened404. Fresh/seeded055 each62strictSQL+74helper, all public full rows/revisions preserved, protected nonemptydown3/emptydownreapply/ownedcleanup pass. 182 archive/38current source hashes verified; initial failures/reopened history preserved. Independent actual CitySeed ReviewActivity23514 not fixed, queued BT-FIX-CITY-001. Human fields only; no new FlutterUI/model permission/provider/real pilot; ClosedPilot/ConsumerBetaNO.

### BT-V5-AGE-004 — Agent Memory

2026-10-03 CODE_LOCAL: native Person explicit AgentMemory 13-type/17-field persistent CRUD/CAS, independent versions, exact jsonb retry, current session/source binding, microsecond limits, row-lock/final-trigger deadline rollback, expired projection and scrubbed terminal delete implemented. INFERRED only pending/expired/deleted reserved shape; no inference acceptance, cognitive reader, model, new UI or production claim. Official fresh056+3seed fullGo 3x3812 PASS/0FAIL/testSkip, scope433 twice, domain252/Store68, SQL107 negative+33 positive, vet/build0; all public old rows/native v3 retained, three protected down exit3 atomic and empty down/reapply, owned DB/runtime cleared. Earlier retry/numeric/deadline/fixture/cleanup failures retained and fixed. Evidence docs/testing/evidence/agent-memory-2026-10-02/README.md and verification-summary.json, 168-file manifest/14 source hashes. CGO0 race not run; Closed Pilot/Consumer Beta NO.

### BT-V5-AGE-005 — Memory Evidence

CODE_AND_LOCAL_VERIFICATION: 057/MemoryEvidence＋本人三原生源/版本/Memory绑定持久Store与PUT/DELETE evidence、GET provenance真实HTTP；固定中文本人关联解释、当前ACL/session/期限、UTC digest、并发幂等/终态清除/懒失效，不推断参与或到访。正式113文件/14源脚本SHA归档root逐项核对。fresh001–056/3seed＋057 nativeProfilev3/Memoryv2全public旧行保留，58 SQL拒绝/5正shape，CURRENT/REMOVED非空down exit3原子保护、emptydown/reapply与自有DB清理PASS。scope905，新Evidence域246/fullMemory域498，HTTP spy197，三轮完整Go各4837 PASS/0FAIL/测试SKIP、19无测试包，全vet/build0、全部API Go/SQL/mod hash稳定。独立runtime当前编译API ready200/三新入口匿名401/no-store/自有PID路径SHA停止。初次七leaf错误解释透传、领域/fixture失败与中断round2原始记录保留。没有INFERRED接纳/分析purpose resolver/认知Memory读取/自动分析/provider/UI/真机新验收；race CGO0未跑。证据docs/testing/evidence/agent-memory-evidence-2026-10-03/README.md及Memory canonical。ClosedPilot/ConsumerBeta NO。

### BT-V5-AGE-008 — Confidence Model

CODE_AND_LOCAL_VERIFICATION：finite0..1非boolean的DIRECT_DECLARATION/UNCALIBRATED_SCORE/ORDINAL语义，未经真实校准CALIBRATED_PROBABILITY硬Unavailable；当前Memory原数据EXPLICIT数值1仅本人声明。新内部真实本人metadata reader双当前SQL重验Session/Person/exactPersonalAgent/native metadata/Memory版本状态期限与ctx；无源正文或新Memory账本。最终production独立fresh057库三轮各88 PASS/0FAIL/SKIP，root独立88 PASS，vet/build0/完整public行、旧004005与4新GoSHA不变/ownedDBDROP；75档案6source SHA，前三夹具RED原样留存。共同fresh058三轮完整Go各5106 PASS/0FAIL/0测试SKIP(19无测试包单列)、全vet/build0、410API Go/SQL/mod hash稳定、每轮全部public行相同、ownedDBDROP；docs/testing/evidence/agent-memory-confidence-2026-10-03/manifest.json与root-review-058/manifest.json。首全Go工具错误将19无测试包误作SKIP，分类修正后另库3轮实际PASS，旧失败不覆盖。无校准估计/候选接纳/图像或证据簇聚合/自动Memory/分析purpose/认知模型读取/HTTP/Flutter/新真机，CGO0未跑race，ClosedPilot/ConsumerBeta NO。

### BT-V5-AGE-009 — Memory Reinforcement

2026-10-03 CODE_LOCAL: native human-approved support of existing EXPLICIT Memory; independent89 PASS/0fail/skip, vet/build0;060 fresh/current down/up/reapply and full old public rows retained; six current/archive SHA equal; current001-062 default wholeGo3 each6161 PASS/0fail/0testskip,18 no-test packages,full vet/build0,459 API hashes stable,all old public retained,owned DBs dropped. docs/testing/evidence/agent-memory-reinforcement-2026-10-03/root-independent-current062.json and ../agent-notification-routing-2026-10-03/root-current062-manifest.json. No automatic cognition, new HTTP/UI,live or release proof; initial failures preserved; ClosedPilot/ConsumerBeta NO.

### BT-V5-AGE-010 — Memory Decay

CODE_AND_LOCAL_VERIFICATION: root independently fresh001-071+retained59 TestPASS/0FAIL-SKIP/test-vet-build0, all631 sourceStable/fullpublic-catalog exact/ownedDROP (work/v4-biz003/root-decay1/result.json). Current wholeGo071-3 default concurrent go test ./... count1 =8487PASS/0FAIL-SKIP/vet-build0/631Stable/public-catalog exact/ownedDROP. Worker native2=59, native1=57 historical; immutable1328 archive root bytesSHA zero mismatch and6 current sourceSHA equal, manifest e635b43ff0a1046bf4f38dae62132e3d4e1527e72a57396ff7b0e21fbbedc9d6. Native baseline-based uncalibrated temporal projection; EXPLICIT DirectDeclaration1 no auto decay; INFERRED staysPENDING_REVIEW; PG current exactPerson/Agent/session/metadata/version/deadline final statement, real reinforcement anchor only, no compounding/mutation. No HTTP/UI/DDL/inference producer/model adapter installed; modelAccess UNAVAILABLE, race NOT_RUN CGO0/noGCC. No production/realCSSA. ClosedPilotReady/ConsumerBeta NO.

### BT-V5-AGE-011 — Memory Correction

Original AGE011+AGE063 CODE_AND_LOCAL_VERIFICATION: human exact-version EDIT/REJECT/DELETE/NEGATE preview→approval→once-only authoritative receipt; reserved INFERRED/PENDING_REVIEW old memory DELETED/scrubbed plus separate EXPLICIT negative, no probability claim. Explicit six-category persistent suppression rejects stale approvals and new source/ID/manual-single-multi producers, missing/disabled guard fails closed; source mutation appends minimal invalidation, original current-source/xmin resolver invalidates/scrubs unsupported candidate, independent human EXPLICIT retained.094 fresh/retained-data unused down/reapply plus old137 tables48 rows/xmin/full semantic catalog and postnative140 table invariant preserved. Root wholeGo 11003 PASS/0fail-skip/packagefail, test/vet/build/twoCLIbuild0; all970 copied/current/before-after hashes equal, all actual raw-emitted owned databases independently SQL absent. Root full Flutter1608 functional+165loading PASS/0fail-skip, analyze/test/DebugAPK build0, all342 source before/copy/target/after/current exact, original1534 passing names retained, current target83 included. Native actual registered5JSON final-client4 work probes PASS separately. Immutable source/raw/16 CJK/MaterialIcons widget images/APK preserved. First storage/time/expiry/lease/result-scroll product RED, first full parent-invariant RED and five oldentry tap RED all preserved then repaired with original assertions; only two old Context tests owned-DB isolation and three old entry real-scroll fixtures added. Evidence work/v5-age038-resume/native3-target-root38mr.json SHA256 ef4a858c8d1807ee62306763fa13fb82bf55fb1a78e977ab4b18a73601abb5f7,work/v5-age038-resume/011-final-targets-root38mt.json SHA256 ce180cff2eb5455701fcc99ed43ab9b3307c7f7e3ef341b48f79a3aa4111d25f,work/v5-age038-resume/context-red011-root38nc.json SHA256 3a85d2666c6b101a7899d8555d6f517b805140265fed485ab6a39639e98ef2e3,work/v5-age038-resume/client-old-entry-red38ne.json SHA256 801e839e8909fe2173280eefb34abd547a8a4a03a9f425824bc0012cb5fc9ce8,work/v5-age038-resume/context-isolation011-root38nj.json SHA256 ef6a794d71405575ff9bbf2f97a855f10c82716ab7870eaa416b2f2fec471064,work/v5-age038-resume/whole094-fixed-root38ng.json SHA256 63582581caa3f307d8b0de52b862e111e8f028e50cf490c0c987d7b963877935,work/v5-age038-resume/whole094-fixed-archive-root38ng.json SHA256 81659dd4e79220c40bb1fb69695e6e5acc227793b87618c716f2d0d73282cfda,work/v5-age038-resume/whole-client011-root38nh.json SHA256 aa1721b2325fde931a072827b35df8a47260d7f0b7be472054221869059d6e7c,work/v5-age038-resume/whole-client011-archive-root38nh.json SHA256 de9a64c1c654855ee9f1bdd17b6065498b427016db45a696f7d02e8d81652799,work/v5-age038-resume/root-native-wire-client011-final38nl/result.json SHA256 68c4714a91f3693ff1b39f2ab7a9e744817941b1e2af24f0b67502b0a0b35f88,work/v5-age038-resume/phone094-install38ns/result.json SHA256 1235e38ffbdd6900a58416aba2ed1c6a47dbc430f6df449c7a783fd58a1ca13a,work/v5-age038-resume/phone094-debug38nv.json SHA256 cbbde5df199759a7722b75f42f30a0c91636b2e9bac53d9b7c9149cc8fb219f3. During local regression noADB while user away. After explicit reconnect message same immutable Debug preview APK installed/pulled exact SHA and Flutter attached; no app data cleared, old Civu unchanged. Phone functional/real OS secure storage/TalkBack/VoiceOver/profile performance/release/production/CSSA A→H NOT_RUN. Bounded background propagation belongs018; no real vector/index/provider deletion port claimed.007 probability calibration/INFERRED ACTIVE remainsPARTIAL, model/real automatic writes/vision/A2A OFF, ClosedPilot/Beta NO.

### BT-V5-AGE-015 — Agent Seed Onboarding

CODE_LOCAL：渐进中文Agent Seed实际注册GET/PUT与本地登录后入口/设置直接入口；displayName/currentCity/language/basicIntent和可跳过兴趣。worker19产品/3576归档根逐SHA重核，root-native82PASS/0FAIL-SKIP/vet-build0/schema066非空down拒绝、空down-reapply/完整public保持/自有库DROP；Flutter analyze0/222实际可见PASS/Debugbuild0，155源一致，APK6B302167安装Success。真机c641566b真实localAPI：defer v1不猜位置/意图、取消/重启不再弹；实际明确city/language/intent/skip后v2COMPLETED，App+精确ownedAPI重启会话与所有本人资料不变，旧privateProfile/历史Moment v5保持，键盘及font1.6滚动按钮可达恢复1.0。root记录与原始XML/PNG/SQL/log在docs/testing/evidence/organization-agent-capability-2026-10-03/root-local-final/work/seed-root-independent-final.json；worker原证据agent-seed-onboarding-2026-10-03。TalkBack/真实IdP/生产构建未运行，不称身份归属/模型分析或CSSA。独立共同066最终3×7807PASS；之后067不借本帧。Closed Pilot/Consumer Beta NO。

### BT-V5-AGE-018 — Social Preference Seed

CODE_LOCAL：七项中文社交偏好实际复用本人PrivateProfile GET/PUT和current metadata CAS，跳过/取消0PUT，八个其它字段及旧freeform保留，sameUniversity不是核验/无自动申请或model purpose。根逐SHA验证worker1415归档/8产品源；root fresh001-067实际211PASS/0FAIL-SKIP/vet-build0（HTTP175含旧相关，新增native矩阵3，非211权限场景），447源/全public/schema稳定自有库DROP。Flutter analyze0/233可见PASS/Debugbuild0、160源同SHA、APK BD7F2840真机c641566b安装Success。本人local dev真实手机跳过及review不写，确认仅social三choice/元数据v2→v3，其余八字段、Seed v2和旧privateMoment v5保持；App+精确自有schema066 API二次重启保持完整本人行/当前UI读取，font1.6正常按钮可达恢复1.0，TalkBackNOTRUN/chips键盘NA。worker首次编译/scroll/analyze及nonowned-sourcechanged轮保留。根528档案与receipt docs/testing/evidence/organization-agent-capability-2026-10-03/root-social-final/receipt.json，manifest ffaf43ac1b737e1c1b7f287b58ecbe91ee52cf9914dc00aab31fc69a90dcf277。完整shared068另待当前产品冻结回归，不借066phone声称后端新DDL真机。Closed Pilot/Consumer Beta NO/IdP未配置。

### BT-V5-AGE-019 — Progressive Completion

Original AGE019 human profile-completion CODE_AND_LOCAL_VERIFICATION passed. Existing own ACTIVE PRIVATE EXPLICIT activity preferences -> suggestions -> UUID preview -> specific plan approval -> authoritative once-only093 receipt; profile CAS, source expiry/version/xmin, account/session boundaries and late responses verified. Native112 PASS, client29 PASS, root frozen whole10922 PASS/0FAIL-SKIP/packageFail with test/vet/build/twoCLIbuild exit0; root Flutter1534 functional PASS/0FAIL-SKIP, analyze/test/DebugAPK build exit0. All958 Go+334 client current/copied/before-after SHA verified; original48 rows/xmin/catalog unchanged, unused093 down/reapply and used-down refusals checked; actual325 owned databases SQL absent. Evidence: work/v5-age038-resume/joint093-whole-root38ly.json SHA256 843c4063311bc642b58438d5ca4b78b33cf63c4b4370c3d349ccb1cbb6c814b5,work/v5-age038-resume/joint093-client-root38lw.json SHA256 d76ae8ab2620b3d492b5c6fcfb45951aefeca0e6e21a936ee3281808b91fc102,work/v5-age038-resume/profile019-client-archive-root38lu.json SHA256 b0cbf4680634428df8eb3533295ef9f8fa84677031a829429316a477bc64cad4,work/v5-age038-resume/worker007-and-client-archive-root38lx.json SHA256 496e58c0d5dfe0f59ee47a41f178376888cc863f83f17ee267e69d599ab34ac8,work/v5-age038-resume/joint093-whole-archive-root38ly.json SHA256 da305bca1f3e386efcaffd852ae5d17e2b5e557e6233fbd7a7d79acd9f29f1ba. Native first72/1 HTTP/PG compile and99/12 nil-return failures, client2+1 review failures retained then repaired. Widget-only Chinese/light/dark/font3/IME screenshots and mocked secure storage only; phone/TalkBack/OS keystore/release/pilot NOT_RUN. Model/automatic memory/provider OFF; ClosedPilot/ConsumerBeta NO.

### BT-V5-AGE-023 — Historical Moment

CODE_AND_LOCAL_VERIFICATION: docs/testing/evidence/historical-moment-time-2026-10-03/root-independent-final.json; root167PASS/0fail/skip + worker sameSHA167x2, late revoke/expiry RED200 fixed401/no effects, real metadata/Moment deadlock RED0→1 fixed both successful0→0, optional missing Agent/Profile human CRUD; 13 source/5405+442 archive verified. Flutter analyze/test209 visible/debug build0/current150SHA stable; APK9FAA373E5CB0890721F2748DCC1A67235AFFD9F4AAB80D2D2D63823B781A90DC installed c641566b, five precisions/createdAt separate/invalid day/cancel/1.6 font/App+currentAPI4 restart same private native draft. Full current0657449x3/vet/build0/528SHA/fullpublic/DROP. Synthetic dev-phone explicitly unverified, not production/CSSA/TalkBack/independent usability; Closed Pilot/Consumer Beta NO.

### BT-V5-AGE-024 — Activity Participation Signal

CODE_AND_LOCAL_VERIFICATION: docs/testing/evidence/agent-participation-signal-2026-10-03/README.md and final-source-manifest.json. Final two native production rounds each58 PASS/0fail/0testskip, explicit READ COMMITTED+READ ONLY after real default-REPEATABLE-READ barrier RED, root independent58, current four sources/archive match; real going/cancelled native writes and actual activity reschedule/rebuilt reader; complete old public rows preserved, vet/build0, ownedDB dropped. Shared current059 final three default fullGo rounds each5484 PASS/0fail/0testskip, fullvet/build0, all425 API source hashes stable; docs/testing/evidence/city-activity-organizer-publish-2026-10-03/manifest.json. Pending only legal SQL shape; no waitlist/attendance/automatic hooks/Memory promotion/model purpose permission; ClosedPilot/ConsumerBeta NO.

### BT-V5-AGE-028 — City History

Original CODE_LOCAL AC independently verified by work/v5-age038-resume/city028-original-ac-root38la.json SHA256 e20e664003dd0b466595ce0db28e60170546373171f505969566317d1c317d92: native CURRENT/LIVED/VISITED/INTERESTED separation,132 City PASS/0FAIL-SKIP/18 top tests in actual whole10797; six before/after/current source SHA exact; five commands0/307 actual owned DB absent. Original RR cause UNKNOWN preserved, not claimed explained. No new city HTTP/UI, objective experiences/dates, cognitive permission or phone verification. ClosedPilot/ConsumerBeta NO.

### BT-V5-AGE-031 — Community Membership Signal

CODE_AND_LOCAL_VERIFICATION: docs/testing/evidence/agent-membership-signal-2026-10-03/README.md and source-manifest-clockfix.json, current five source/archive hashes match. Real Community/Organization seven native membership roles read and Personal Context Evidence assembled with exact currentPerson/PersonalAgent/session/metadata/source/version/Block/expiry; private member access scoped to self, Organization resource and principal namespaces kept distinct; sixteen actual SQL revoke/role/session/Agent/block/flag/cancel/deadline barriers, transfer owner and timezone/current resource reads. Real common059 RED preserved (old PG ObservedAt versus Windows wallclock), strict current database clock fix, staging/production166 and root independent166 PASS/0fail/0testskip, vet/build0/all public rows preserved/ownedDB dropped. New shared059 default fullGo three rounds each5484 PASS/0fail/0testskip, fullvet/build0/425 API hashes stable, city-activity-organizer-publish-2026-10-03/manifest.json. Old154 archive and first failures retained. Not persistent005 Memory Evidence,033 unified builder, historical membership/identity/Friend or machine-purpose permission; cognition unavailable, ClosedPilot/ConsumerBeta NO.

### BT-V5-AGE-033 — Context Builder

CODE_AND_LOCAL_VERIFICATION: native exact TASK_CONTEXT_READ Preview/Approve/Read/Revoke on original consent_grants+076 immutable binding; actual human selected values, six sources+configured Policy, current Task/Session/Agent/version/source bounds; real registered local Runtime consumes values and sealed revalidates, no model/effect. Final worker native310PASS/0FAIL-SKIP,076 old complete public/catalog/empty+unconsumed down-reapply/history atomic refusal; original Preview expiry RED0/3 corrected no renewal. Root fresh076 whole-go076-3 default go test ./... -count=1 -json 8945PASS/0FAIL-SKIP/pkgFAIL,vet/build0,680source stable/fullpublic-catalog same/ownedDROP. WholeFlutter analyze/test/debugbuild0,623functional+86loading/0failSkip,199stable. Root independently verified worker74/current6 and currentGo680/client199. docs/testing/evidence/agent-context-purpose-2026-10-03/README.md, root-final1 manifest1003files SHA1f2bfcdc8ff5e06eb7a31834ecffcc774691a2616ab6065ab806ef7bf3bf6846. First failed whole1/2 preserved; oldCHT double-clock fixture fixed singletimestamp,unchanged real400ms/550ms/rejection/noeffect assertions. NO new phone/AT/model/production evidence. ClosedPilotNO ConsumerBetaNO. AGE034 relevance/AIR022 adapter/AGE035 budgets separate.

### BT-V5-AGE-034 — Context Relevance

CODE_AND_LOCAL_VERIFICATION native6 384PASS/0FAIL-SKIP/694stable/fullpublic-catalog same/ownedDBDROP; root independently checked150fileSHA/current10 and real registered Runtime. Root parallel-full076-2 default go test ./... -count=1 -json 9066PASS/0FAIL-SKIP/pkgFAIL,vet/build0,694stable/completepublic-catalog same/ownedDROP. Original whole1 failures retained: shared063DDL isolated via existing helper, native327us backwards clock correctly failclosed, no timing relax. Archived docs/testing/evidence/chat-entity-device-2026-10-04/parallel-code-final1/manifest.json; root receipt work/v4-cht003-resume/parallel-code-root-acceptance.json. Exact-approved bounded six-source relevance, selected-known-empty UNKNOWN/unrelated NOT_RELEVANT/unselected NOT_REQUESTED, original whole seal including filtered-source changes/revoke/expiry kept; literal matching not model understanding. No new grant/history/Memory write/model/phone/AT/production claim; ClosedPilot/ConsumerBeta NO.

### BT-V5-AGE-035 — Context Budget

CODE_AND_LOCAL_VERIFICATION only. Original limit/priority/confidence/recency controls implemented in existing Adapter/Builder. Native4 478 PASS/0FAIL-SKIP/pkg; root schema88 default-concurrency whole 10573 PASS/0FAIL-SKIP/pkg, Go test/vet/build/2CLIs all0. Root verified913 current frozen inputs/910 API keys, 48 synthetic old public rows/all xmin/catalog/unused088 down-reapply unchanged, all 219 actually logged owned DBs independently absent. All9 Go+2 existing canonical current/final SHA verified;99 annex files/95 original copies/all prior86 unchanged. Three real RED 3/1/3 FAIL including parents preserved/fixed. Old whole2/cause1 xmin false preserved; only city-hidden fixture uses existing owned DB; old exact7793032 attribution UNKNOWN. Full sealed omitted-source authority/ABA/revoke/actual wait expiry retained; confidence1 DIRECT_DECLARATION, no score invention. No tokenizer/provider/DDL/UI/062 shadow budget; registered client grantId-only/default. Root AC work/v5-age038-resume/age035-original-local-ac-decision38ga.json, whole work/v5-age038-resume/age035-current-isolated-whole-root38fx.json, delivery work/v5-age038-resume/age035-native4-annex-root38fz.json. Current314 Flutter inputs unchanged; prior1245+150 analyze/test/Debugbuild0 reused, not rerun. Phone088/performance/AT/race/live not accepted. ClosedPilot/Beta NO.

### BT-V5-AGE-036 — Current Context

CODE_AND_LOCAL_VERIFICATION：真实本人当前Declaration/ACTIVE Task/显式SelectedCity有界Context与ReadOwn/RevalidateOwn；不是GPS或长期Memory，旧Task/Declaration无持久expiry。production与root独立fresh057/3seed各107 PASS/0FAIL/0SKIP，scope vet/build0、5current/archive SHA一致、完整旧public原源行保持/自有库DROP；实际时区/未来时间/身份/撤权/同源改删重建/租期与OFF再ON边界已验。共同fresh058三轮完整Go各5106 PASS/0FAIL/0测试SKIP（19无测试包单列），全vet/build0、410全部API Go/SQL/mod hash稳定、每轮全部public行相同、DBdrop；docs/testing/evidence/agent-current-context-2026-10-03/root-review及agent-memory-confidence-2026-10-03/root-review-058/manifest.json。新增独占5Go，无DDL/HTTP/Flutter/模型认知用途/统一033Builder，实际ReadForCognition保持Unavailable；CGO0未跑race/新真机，ClosedPilot/ConsumerBeta NO。

### BT-V5-AGE-037 — Attention Policy Model

AttentionPolicy基础模型已验：Personal精确Agent/独立policyCAS+撤销、IMMEDIATE/NORMAL/DIGEST/SILENT/BLOCK确定性五路offline分类、当前clock/来源合同与066前后brake；5Go90PASS/0FAIL/SKIP、scopevet/build0。当前056隔离库三轮全Go各3812PASS/0FAIL/testSKIP（19无测试包），每轮attention90、5主体SHA与冻结快照和当前源逐项一致，全vet/build0、全部public完整源保留及owned清理。docs/testing/evidence/agent-attention-policy-2026-10-02/README.md /source-manifest.json 与 v5-parallel-regression-2026-10-02。仅进程内模型/合成规则，不是持久用户设置或通知投递；真实Service缺用途授权resolver仍Unavailable无Decision，039/038/040后续，race CGO0未运行，无Flutter/live试点，ClosedPilot/ConsumerBeta NO。

### BT-V5-AGE-038 — Notification Routing

Original product evidence/history retained; root reaccepted precise Task wait regression: exact request appname/PID/GetTask SQL/native relation lock, unrelated waiter excluded, cancellable rollback/drain; five original 200/404/401/404/401 modes preserved. Worker current frozen native22 final-native2 sixPASS0FAIL-SKIP/test-vet-build0/allpublic-catalog preserved/ownedDROP. Root whole native22 full38p sixTask casesPASS/HTTPpackagePASS,850 frozen original bytesSHA and1766 current+1888 historical archivefiles independently checked. Root current-native22-root38s-proof.json. WholeGo10080PASS4FAIL events from two unrelated AIR015 fixtures; originalAIR015 reopened, globalPASS not claimed. No production grant weakening, no real identity/AT/ops claim. ClosedPilot/ConsumerBetaNO.

### BT-V5-AGE-041 — Social Policy

2026-10-03 CODE_LOCAL: seven typed categories SocialInteractionPolicy, all disabled by default, conservative review brake, independent policy CAS/revocation/current-time controls and 066 gate boundary implemented in 5 frozen Go files. Worker production121 and root independent work/v5-age041/verify.ps1 -Mode production -Round root-review each121 PASS/0fail/skip, scoped vet/build0, source SHA equals staging+archive. Actual current purpose/pair/education resolver missing: enabled Service always Unavailable, no new UI/persistence/ALLOW action/legacy consent inheritance; 042-044 remain separate. Evidence docs/testing/evidence/agent-social-policy-2026-10-03/README.md/source-manifest.json; root-review result actual. No new full057/live claim; race CGO0 unrun. Closed Pilot/Consumer Beta NO.

### BT-V5-AGE-043 — Introduction Policy

四类来源+双侧Social Policy+普通中文人工路径已实现并按原CODE_AND_LOCAL_VERIFICATION核证。SharedActivity=双方明确有限期公开报名而非实际到场；Community明确本人兴趣writer/当前公开源+上下文ABA、原兴趣/城市公开来源；SHARED_ACTIVITY原048同意AND双方原070 REVIEW_REQUIRED，不开启model/message/member。最新两Go源关联社群City期限/metadata ABA真实RED3PASS/3FAIL→105定向PASS→wholeGo9483PASS/0FAIL-SKIP/test-vet-build0/750stable、旧全public/xmin/catalog/down/reapply与ownedDROP真实PASS。1616worker归档根逐文件bytes/SHA核证，manifest52d1e3a831d6637d3fcfb7552adea2f26123761464884b95fa62496c1003076c。当前043全部12 Dart逐文件与wholeFlutter920/114loading/analyze-test-Debugbuild0冻结250帧吻合；该APK125cd真机原RSVP PRIVATE v1→PUBLIC2→PRIVATE3保持going→无需刷新PUBLIC4、cancel/back无副作用、实际API OS旧opaque409和App重启原生持久化、原取消使同ID cancelled/PRIVATE5且3审计保留、其他六账本不变。原13/12scope源与稳定ID/默认模型OFF保留。TalkBack/race/双人完整普通UI消费未运行如实NOT_RUN，生产/CSSA/部署未替代原CODE_LOCAL；ClosedPilot/ConsumerBeta NO。另Cht独立source delta新wholeFlutter待root核验，旧920不声称覆盖。根证据work/v5-age038-resume/age043-code-local-done-proof.json、native-full-activity078-3/result.json与phone-activity078-2/actual-acceptance-checkpoint26.json。

### BT-V5-AGE-044 — Autonomy Level

2026-10-03 CODE_LOCAL original four-level definition and closed12 operations implemented; root independent2x221 PASS/0fail/skip, vet/build0 and26 source frozen; contributor final2x221/31 archived files verified. Current three-task scope409/full default7063 PASS/0fail/testskip, fullvet/build0,504 API+18owned source hashes stable, full old public rows unchanged, fresh001-064/seed29/33/nonemptydown refusal3/emptydown-reapply/ownedDB dropped. docs/testing/evidence/agent-autonomy-2026-10-03/root-independent-final.json and654-file root index. Process-local config is NOT native identity/persistent API; actual source/purpose/approval resolver absent => emptyUnavailable,Level3 disabled,zero autonomous effects. Client unchanged baseline analyze0/198PASS/debugbuild0 only; Gradle first failure preserved. No device/new UI/production/race proof; ClosedPilot/ConsumerBeta NO.

### BT-V5-AIR-040 — Outbound Action Safety

CODE_LOCAL root38zg6/zgi: final18 1051 live/frozen/archive exact;21newinputs + registry metadata only;1029 other old/alloldtests/001-096DDL/3seed/mod byteexact and3canonical prefixes. Original037CONFIRM -> actual originalconsent_grants independent OWN_SANDBOX_ACTION explicitownerapproval -> immutable tenant/actor/session/task/tool/payload/source/policy/xmin/expiry binding -> oneconsume atomic dispatch/revoke -> realprivate sandboxrow/UNKNOWN durable recovery. ModelJSON/confirmed cannotgrant; no originalhumanAPI/HTTP/provider activation. Currentownership before privateequality oracle actualRED fixed uniformforeign/anonymous/expiredSession/unknownID DENIED andcurrentowner CHANGED. TwoWorkers/onceeffect, independent logicalop IDs, lastPGclock/contextlostcommit, real3crashpoints/6newprocessops, lease/fence/noresend, truthfulactualreceipt/revocationorders and auditredaction/SQLtamper nativePASS. RED01/oracle13 and fixture03-07/13/14/17 kept; regress10 early sourceStable=false not finalproof. Root fullGo test ./... count1 timeout30m json0 11728 run/pass/92pkg/no fail-skip;11644oldmultiset UUID-onlyretained+exact84target. Vet/build/original2CLI0. Isolated001-097/3seed+retainedfixture,097unuseddown/reapply, original141tables48rows/xmin/catalog intact, after144stable. 401 rawactualrootchild+parent+36worker (438distinct) SQLabsent. Root18687ZIPdata byte/SHA/CRC verified. Evidence docs/testing/evidence/outbound-action-safety-2026-10-06/root-whole38zgi/README.md manifestd265bb247a1a81886885e0f00a488bcdea5e812a1dda1f833884345b2e9770fc. No actualexternalwrite/productionIdP/model/cost/UI/phone/productiondeployment verified;raceCGO0NOT_RUN. DefaultOFF/providerUnavailable;phone997/096/9676/reverse3697 remains. Now recording remains IP/current normal5196627 source367 full1916PASS separately. ClosedPilot/BetaNO.

### BT-V5-AGE-046 — Agent Profile Page

Original AGE046 CODE_AND_LOCAL_VERIFICATION. Settings → 我的智能体 real personal-only read aggregation with six original required groups: What knows/Preferences/Places/Activities/Communities/Agent settings. Uses original068 private-profile/069 Memory detail/070 policies plus original community interest and participation APIs; no fake attendance, visited-place, membership, learning or personalization activation. Ten exact scoped Dart changes,349 full frozen lib/test/pubspec before/copied/after/current SHA exact, original342 inputs retained. Whole Flutter analyze/test/Debug build exit0;1646 functional+169 loading PASS/0FAIL-SKIP, original1608 branch multiplicities and61 targeted preserved. First whole1644PASS/2 old lazy-scroll-fixture FAIL retained, real scroll/build/ensureVisible/hit-test repair preserves original source/domain assertions. 1000 Memory lazy <30 mounted/last reachable, stream send+body total12s/2MiB, identity/transport/workspace ABA and late-response retirement, relative-only-narrowing detail lease and no expired body; widget320/font3/IME260/light-dark/48dp/keyboard/semantics checked. Actual local095 frozen981 backend and final unchanged API bytes:1 real Dart HTTP IO PASS/0FAIL-SKIP, no MockClient, original auxiliary session logout204. Root833-file immutable archive docs/testing/evidence/agent-profile-page-2026-10-06/root-whole349/manifest.json SHA256 4170acbd7ac69737b4f5a313618eef204c418afe1cb22589ee919f898a245bf9. Current349 Debug APK installed and pulled same SHA4813f9d7943278576a4de7e5360b9a7a0fb74f7c23ce8f4e9a89db8ab931517a, appdata not cleared. New home logged out; original phoneSession native idle expired19:17:08UTC before19:52 install, no storage-loss conclusion. New My Agent page physical six groups NOT_RUN because foreground moved to user app; debug attached then lost, no current connection claim. Current controlled Profile comparison/TalkBack/VoiceOver/productionIdP NOT_RUN. Old uncontrolled296 comparison remains historical. Full currentGo023 pending, no release claim. Model/real auto-write/Vision/A2A OFF, calibration007/attendance027 unchanged, Closed Pilot/Consumer Beta NO. [{"path": "work/v5-age046-agent-profile-page/source-freeze2.json", "sha256": "7642f191eaec7f1cbf725eb2e1349f9521f2e469a25fad48c2411b76e0b6000b"}, {"path": "work/v5-age038-resume/profile046-proof38pv/result.json", "sha256": "03c5a6c83f8595766f2c419ca533bc59198721504dd4f3848b4cdd879414224e"}, {"path": "work/v5-age038-resume/profile046-archive-root38pv.json", "sha256": "dd8fc845660ae4dc7f3cb1a4a03be620911ab41017189f4a114f2f8517bdb82d"}]

### BT-V5-AGE-049 — Agent Privacy Controls

2026-10-07 ENRICHMENT_PRIVACY_INVENTORY_RELATED_UNIT_PASS_PARTIAL: 原049新增实际本人本地分析许可GET清单和设置中文消费入口，原079/consent_grants唯一真源，current owner/Agent/Session/同statement时钟及final clock复用；50项显式截断且有效许可优先，不含正文/新授权。具体版本检查确认→原journal→原by-ID GET→DELETE expectedRevision→重新读取；未知/409/重开只GET核实，不复活批准/重发/把当前状态说成因果回执，不自动学习或删除独立Memory。真实缺route/Settings入口及SQL-spy有效许可优先RED已修；37Go首次33PASS/4夹具FAIL、11Flutter首次10PASS/1夹具FAIL原exit1保留，仅受影响用例重测，最终跨版本38唯一Go父子事件(8顶层)/11Flutter行为有PASS，不称最终组合一次通过。根核94实文件含manifest/16source/9readonly/726旧tests静态字节/2程序插入逆证/2canonical前缀/两份原注册合成wire，未重跑单位。PG/排序/锁/持久化/OS存储/真实身份/手机/AT/性能/全量/分析/vet/build均NOT_RUN；字段受众UI与Personalization总门禁未完成，五族原049整体PARTIAL，Pilot/Beta NO。; manifest 1c8631f5094817a45b228f6a25a438dd5f0061f5b41e1546fdf6e6e9937c5632; root work/v5-age038-resume/privacy-inventory-unit-review-01/proof.json.
2026-10-07 PROFILE_FIELD_VISIBILITY_RELATED_UNIT_PASS_PARTIAL: 原049新增设置各项资料的可见范围→原GET十一字段/五受众→逐项修改→当前版本完整规则检查→明确原PUT expectedVersion/rules。复用原CommunityApi.mine实际active本人社群；100项上限未列出原ID保留标未知，不强迫变受众，新ID只从真实active列表选，最终资格由原native复验。PUBLIC原整体ACL/AGENT_ONLY当前用途授权不变，不复制私人正文/开模型或学习；current身份/来源/ABA退役、409新GET新批准、未知只GET不盲重发/因果假成功。实际原Settings入口RED1失败；green02新fixture编译失败0行为/3失败loader/5error保留，修新fixture与旧社群ID消费限制后green03 16行为+3加载PASS exit0/0skip/error，未跑旧控制/全量。根核55实文件含manifest/8源/16readonly/24行插入原Settingsbyte inverse/canonical prefix/28声明输入/raw，不重跑；最后仅11条原上下文CRLF逐字恢复，与实际green03归一化字节等同，格式恢复版未另测，净6误报/封包失败留档。真实认证/会员资格撤权/PG/原生/手机/AT/性能/Go/全量/analyze/build均NOT_RUN。原049五族整体仍PARTIAL；Personalization控制覆盖继续按原文/实际接口审计，不把推断总门禁新增为独立需求。Pilot/Beta NO。; manifest 7fd7565043b694bfed72c6c917ec9e94ed4cc9bf3155a70432861e221b9c0714; root work/v5-age038-resume/profile-visibility-unit-review-01/proof.json.
2026-10-07 TASK_CONTEXT_CURRENT_SESSION_PRIVACY_RELATED_UNIT_PASS_PARTIAL: 原049新增同Person/Agent/原Session的TASK_CONTEXT_READ最多50项/截断/未撤且未到期优先元数据清单→Settings具体版本检查→原DELETE expectedRevision→刷新。只公开选择范围/时间/状态，不输出queryDigest/正文/sourcehandle/新权限。原来源GET403/Task不可读不阻断原native撤销；freshinventory即具体版本检查来源，原Session/CAS/final clock不放宽。未知只GET核状态，原GET拒绝时可同Agent清单同ID/scope/更高撤销版本核当前撤回，不能作因果回执；缺项/截断不清pending，不重复DELETE。实际RED路由404和Settings无入口各1FAIL；新API Environment.current编译失败0行为保留并修静态配置，Flutter最终12独立行为分阶段PASS（green03整体exit1因1新widget误统计背景GET，只affected04精确1PASS），Go三包7顶层/31含子事件exit0。根只核63文件含manifest/15源/10readonly/原Settings3插入23行与server1行逆证/canonical prefix/raw，无重复单位或最终组合。当前native PG/真实Session与来源撤权/OS/真机/AT/性能/全量/analyze/vet/build/部署均NOT_RUN；跨重启精确未知恢复、新grant批准入口及原049五族全项仍PARTIAL，Pilot/Beta NO。; manifest d5813b44a8d8a8a3e03c9e32e841665799f21d01126e2349149685d39044e768; root work/v5-age038-resume/task-context-inventory-unit-review-01/proof.json.
2026-10-07 PRIVACY_CONTROL_COPY_RELATED_UNIT_PASS_PARTIAL: 原049五族分项控制说明已准确接到智能体资料页；只替换一个误导全局开关缺失的Text，原权限与路由不变，canonical仅追加纠正。真实Page一条widget先旧提示ERROR exit1，再同用例PASS exit0；小屏320x640主要动作48dp可达、5GET/0写。仅8直接输入帧/6只读/原页面字节逆证及24证据文件核验，根不重复单位。完整五族原生Session/权限、真实认证、设备/AT、数据库、全量与编译仍NOT_RUN，原049仍PARTIAL，ClosedPilot/Beta NO。; manifest b38a97014f63c17aaf03c369d0726aaefc4269acfd7b9a6cc00e50400592a405.
2026-10-07 FIVE_FAMILY_CODE_AND_LOCAL_DONE: 五族原真实消费链及原生生命周期PASS同SHA已核；Task/Moment新许可库存真实native注册GET、原preview/approve52条/有效优先50+sentinel、原Session vs本人新Session历史差异、来源变化后精确版本撤回/重试/无额外行动全部实测。两条接口真实退休Agent后仍200缺陷RED03/RED05→复用原ResolveOwnContextAgent编码后核同Owner/Agent/Personal角色，缺能力503/退休403/正常刷新200，不造权限或DDL。最终四包96父子事件PASS/exit0/0fail/skip，8新native顶层/21父子/18叶场景；自有001-104+3开发seed及fixture残留0，原失败与证据计数修正均留档。详见work/v5-age038-resume/age049-inventory-native-2026-10-07/README.md与final-proof.json。源码与本地验收收口，不等于真实IdP/隐私真机/AT/生产已验；当前07整仓Go另库RUNNING未提前计PASS，模型出网/真实Agent写/Vision/A2A关闭，Pilot/Beta NO。

### BT-V5-AGE-051 — Organization Memory

CODE_AND_LOCAL_VERIFICATION: Organization Memory eight-category explicit private administrator declarations in original single typed ledger; current auth/source/version/expiry/revocation/Person-vs-Org boundaries. Root immutable archive docs/testing/evidence/organization-agent-memory-2026-10-03/manifest.json SHA256 6059249f1f5eece576c0b0f79b779012ee4272e77229ab3eeaa0b1f9772e5f20; 9665 files independently copied/hashed. Native7 actual321 PASS/0FAIL-SKIP/test-vet-build0;068 migration2 four nonempty down exit3 and atomic all-public preservation, emptydown/reapply0; current068-full4 default whole Go3 rounds each8218 PASS/0FAIL-SKIP/pkgFAIL,vet/build0,608source and all-public/catalog/old-data stable,owned DB actuallyDROP. Initial failed full1/2/3 preserved; trusted capture bounded retry proof RED old writer and GREEN47, unchanged064strict guard/source TTL. No OrgMemory Client/phone/automatic cognition/OrgEvidence/real IdP/real external facts/pilot/release claims; ClosedPilotReady=NO,ConsumerBeta=NO,raceNOT_RUN_CGO0.

### BT-V5-AGE-060 — Organization Memory Boundary

2026-10-03 CODE_LOCAL actual native default private isolation matrix: existing owner-positive Profile/EXPLICIT Memory/Evidence vs peer/Org/admin/owner/reserved dormantBusiness, public profile_view no canaries, block/grant revoke and runtime OFF/ON actualStoreUnavailable; no new auth bypass. Root independent453PASS incl54matrix/0fail/skip, test/vet/build0,379internalGo and3owned stable/fullpublic equal/ownedDBDROP; worker1153+392 indexed files independently verified. Earlier worker whole-source change frames retainedFAIL; current full504-API freeze default7063PASS/0fail/testskip/fullvet-build0/fresh001-064/18owned/oldrow/down-reapply/drop verified. docs/testing/evidence/agent-memory-isolation-2026-10-03/root-independent-final.json and392 rootfiles. Specific-content cognitive delegation/persistent native social policy port and Business activation stillUnavailable; constructor/role/shop flags do not authorize. ClosedPilot/ConsumerBeta NO; no client/device/race new proof.

### BT-V5-AGE-053 — Organization Knowledge Source

CODE_AND_LOCAL_VERIFICATION: 5 native sources profile/activity/announcement/admin-input/publicFAQ on original057Evidence,073/075 version+human preview/publish/withdraw+currentSource and Session. Root fresh075 independent242PASS and wholeGo075-2 8781PASS/0FAIL-SKIP/pkgFail,test/vet/build0,671 source stable,complete public/catalog unchanged,oldPERSON2/ORG8 IDs retained and075 down/reapply exact,owned DBs dropped. PublicANN nonUTC and finalpoolwait revoke/expiry/session-accountABA defects actually repaired. Receipt work/v4-cht003-resume/root-final-validation.json,root archive5153files manifest e65396913b9fc0d8bd47dd736a545f6338f3d0e10418fc5cbb46258be4f8c9f4; worker-final3 71files948bf9efa37bb21cefa38ed5ea8ff71fa03f4855dde7ced9beb1770d7b1187cf and older archives root hash checked. CODE_LOCAL only; no announcementUI/model/broadcast/prod/AT,ClosedPilotNO.

### BT-V5-AGE-062 — Purpose Limitation

Original negative CODE_LOCAL AC verified by work/v5-age038-resume/purpose062-original-ac-root38lk.json SHA256 ccf0a8442f5a4dccf41953886260c9b5124d523b6498e6033c40e94f41441f57:95 PurposeLimitation PASS/0fail-skip/6top in actual prior10797 fullGo,11 relevant before-after-current-copied source SHA exact; actual guard invoked by ports and ON wrapper, native nonempty Memory/RSVP/profile_view and registered HTTP rejection. Positive temporary Activity delegation/use/copy-summary-index cleanup remains NOT_IMPLEMENTED, not claimed. Historical reason/evidence preserved; no new code/tests/DDL/UI/provider/phone. ClosedPilot/ConsumerBeta NO.

### BT-V5-AGE-064 — Agent Enrichment Events

2026-10-03 CODE_LOCAL: compatible v1 catalog13 =12enrichment types+UserQuery, ten retained native enrichment sources/old query, explicit Completed/Visited Unavailable; source native revision or namespaced updated/created digest, final current session/owner/PersonalAgent/source/ACL, retry IDs/TTL and revalidation implemented. Root independent pwsh work/v5-age064/scope.ps1 -EvidenceLabel root-review -Production actual fresh001-056+3seeds472 PASS/0fail/skip/scoped vet0, full original public rows preserved and owned DB dropped, owned8 production hashes stable; worker two472 rounds/pure293/vet/build. docs/testing/evidence/agent-enrichment-events-2026-10-03 manifest63/source-hashes8. Metadata callable producers only, retained states not historical join/attendance/visit, processing Unavailable; no hooks/outbox/consumer/grant/model. New057 full pending shared verification, not counted passed; race CGO0 exit2 unrun. Closed Pilot/Consumer Beta NO.

### BT-V5-AIR-016 — Async Enrichment

CODE_AND_LOCAL_VERIFICATION ONLY; root acceptance work/v5-age038-resume/three-task-acceptance36.json; Go full083-joint3 9897PASS/0FAIL-SKIP/test-vet-build0/820+3seed stable/public-catalog-xmin/up-down-reapply/ownedDROP; Flutter current-map-intent4 1037functional+129loading/0FAIL-SKIP/analyze-test-Debugbuild0/275stable, actual e1d9b4a APK installed; root immutable archive manifest c30acc5209ff0508369472a4cea22a64cdc5b9fe146202cead2cd891aaf688b0 (12637 files verified) plus worker byte proofs33/34/35 and phone proof36; original Moment independent + original082 effect sameTx durable bounded Run/fence/checkpoint, real kill/unknown/tail lock/5attempt head RED→GREEN; Memory/model/scheduler defaultOFF not deployed; failed frames retained. NOT_RUN: CGO race/noGCC, TalkBack, all6originalhuman tasks, real2testers, productionIdP/HTTPS/map/CSSA/ops/live scheduler; ClosedPilot/ConsumerBeta NO.

### BT-V5-AGE-066 — Feature Flag

2026-10-02 本地代码验收：五服务端功能开关默认OFF/拒一次全量开启、独立Capture/Disable+父brake、配置CAS/revision与迟到撤销、主程序真实受控adapter context装配；普通本人管理保留，Memory/candidate认知硬Unavailable。fresh001–055+3seed两独立库各263 PASS，根第三库三轮全Go各3100 PASS/0FAIL/testSKIP（19无测试包单列），全vet/build0，真实binary非法配置exit1固定中文脱敏；全部public完整源行和全部API源码前后SHA保持、自有DB均DROP。证据 docs/testing/evidence/agent-feature-flags-2026-10-02/README.md /verification-summary.json /source-manifest.json。无schema/client改动，race detector CGO0无gcc未运行，未声称领域消费者/UI/live推理；ClosedPilot/ConsumerBeta NO。

### BT-V5-AGE-068 — Profile APIs

2026-10-03 CODE_LOCAL three existing registered Profile APIs implemented and native-verified. Real public-profile session expiry/revoke RED reproduced then fixed at RC/current native account/session/profile locks+finalPGclock; strict bounded wire, nil/cancel/no-op/CAS/public-private metadata independence. Worker final134x2PASS + existing410PASS, root independent134PASS/0fail/skip/vet-build0/8owned SHA/fresh001-064/fullpublic/drop. 578 original archive and88 rootfiles independently SHA checked; current full default7063PASS/0fail/testskip/fullvet-build0/504API+18owned stable/fulloldrows/up/nonemptydown3/emptydown-reapply/drop. docs/testing/evidence/profile-apis-2026-10-03/root-independent-final.json. Initial real RED/tool/compile/fixture failures retained. No alternate auth/IdP bypass/Agent activation/newClient/device/production/race proof; ClosedPilot/ConsumerBeta NO.

### BT-V5-AGE-069 — Memory APIs

Original AGE069 CODE_AND_LOCAL_VERIFICATION: GET list/PUT/update/DELETE reuse original human Memory; new stable GET detail closed server-only proof binds current owner/Agent/Session/full Session row+xmin/source/record expiry and postencode native revalidation; deleted/expired body not released, immutable DTO bounds and relative 2min lease retained. New path-bound POST reject requires original094 immutable MEMORY/actionREJECT/operationID/digest/current same Session; wrong path/kind/action0Confirm, same committed receipt100 repeats no duplicate deletion/audit. Native5 103PASS/five0,13 actual owned DBabsence; real Session token/revoke ABA RED and relation-wait idle expiry classification RED corrected only newPG file; eightGo frozen archive8810 files. No source schema/old Session/Memory writer/provider/calibration changes. Current merged095 whole Go11126 PASS/0FAIL-SKIP/packagefail; test/vet/build/twoCLIbuild0.981 current/copied/before-after sourceSHA exact at closure. Original11003 whole and own72/103 all passing branches/multiplicities retained, normalizing ONLY random fixture UUID in parameter names; initial exact-name comparator24 mismatches are retained as proof harness error, not missing tests. Original094137 tables48 rows/xmin/full catalog/down-reapply and095140 tables rows/xmin/only cleanup function delta/down-reapply exact; native parent140 exact,369 actual raw emitted owned DB+parent independently SQL absent. Immutable root1431files manifest 33b1d43ef35a96b8fbab9e638e0a425c8f42f431ff13e0c28661d34ed61c97be. work/v5-age038-resume/whole095-joint-root38ox.json SHA256 d8c7815e4621515f0934336903d9897b01b943decf9fe7685e0d9fe774439f62,work/v5-age038-resume/whole095-archive-root38oy.json SHA256 2adb826746bf384c98c853de0856808a3a4cda4bcae6f770024ae32ab9db44f3,work/v5-age038-resume/air018-target-native6-root38ok.json SHA256 7e3e7748ed2fb22061b556f2c410bd6cf0ccefdbe441b9c842e69ecf778ce419,work/v5-age038-resume/age069-target-and-joint-preflight-root38op.json SHA256 df89714c0da03034d5f08c47b4811b10f43876ee428b6a83712fb682c6b45bce. Phone remains verified094 Debug, current342 Flutter1608/three0 source unchanged at closure; no claim current095 phone or real vector/provider deletion/production scheduler. Initial native failures and exact repairs preserved; race not run CGO0/noGCC. Model/real automatic write/Vision/A2A OFF; calibration007 and attendance027/ACTN002 gates unchanged, ClosedPilot/Beta NO.

### BT-V5-AGE-070 — Policy APIs

CODE_AND_LOCAL_VERIFICATION: docs/testing/evidence/agent-policy-apis-2026-10-03/root-independent-final.json; root native210PASS/0fail/skip, original502 separate, current065 full7449x3/vet/build0/528SHA/fullpublic stable/DROP; 065 old nonempty up preserved/nonempty down refusal3 atomic/empty down-reapply; three native family CAS/session final/current clock/strict JSON, 14 frozen source/316+108 archive verified. Settings do not grant cognition/model/action permission; no new client/LIVE, Closed Pilot and Consumer Beta NO.

### BT-V5-AIR-049 — Enrichment Metrics

CODE_AND_LOCAL_VERIFICATION. Actual089 derived observation metadata tied to original same-transaction audit and Memory/Candidate lifecycle; original writers remain authority. Native owner/organization metrics, current retained notification policy five dispositions, original083/088 bounded redacted metadata traces and actual062 budget alerts. native5 50PASS/0FAIL-SKIP/pkg including all19metrics events plus complete077 positive/negative cases; production Go/SQL unchanged after native4. Root current089 whole3 10652PASS/0FAIL-TEST_SKIP/pkgFAIL, Go test/vet/build/2CLI0;921current/frozen inputs and918completeAPI stable;48old synthetic rows/all original public xmin/catalog/089unused down-reapply stable;245actualownedDBs independentlySQLabsent. Original AC decision work/v5-age038-resume/bt-v5-air-049-original-local-ac-decision38hi.json; whole work/v5-age038-resume/air010-air049-current-whole-root38he.json. Archives3884(10)/4034+1032appendix(49) independent original byte/SHA checks; all true REDs retained, old077 single-table maintenance SQL narrower due actual089FK, protected specific077guard preserved. No live provider/real monthlyquota/production alert/device089/race/AT acceptance, exact314Flutter unchanged prior results reused. ClosedPilot/Beta NO.

### BT-V5-AIR-007 — 统一 Model Gateway 契约

本地网关基础契约已验：统一业务Request/Result/provider adapter、严格schema/tools/错误/未知usage、稳定Agent引用、cancel/真实deadline/迟到丢弃；7文件scope189PASS/95.1%coverage，10次同套重复1890非独立用例，vet/build/SDK及直接网络扫描0。当前056隔离库三轮全Go各3812PASS/0FAIL/testSKIP（19无测试包），每轮gateway189、主体7SHA与冻结快照和当前源一致，全vet/build0，所有public源完整行保留及owned清理。docs/testing/evidence/model-gateway-2026-10-02/README.md 与 v5-parallel-regression-2026-10-02。默认Gateway无provider始终Unavailable，fake仅OFFLINE_CONTRACT，无模型出网/工具或Memory写/真实上下文批准，live不通过；race CGO0未运行，ClosedPilot/ConsumerBeta NO。

### BT-V5-AIR-008 — 能力登记与路由资格验证

CODE_LOCAL: apps/api/internal/modelcapability五源SHA匹配正式manifest；worker production-clock及root独立production-root-review各130 PASS/0 FAIL/0 SKIP、目标go vet/build exit0。登记六类三态能力和exact provider/model/version/wire，按合成当前许可→能力→质量/成本路由；007缺vision/stream/state/storage消费者时明确拒绝零fake调用。实际Service默认Disabled/Unavailable，无真实adapter/出口/预算resolver，无UI/provider/live验收。历史129源码与初次失败保留；证据docs/testing/evidence/model-capabilities-2026-10-03；全API回归另记005当前057证明，不由本包局部检查推断。Closed Pilot/Consumer Beta NO。

### BT-V5-AIR-010 — 统一超时重试与隐私安全降级

CODE_AND_LOCAL_VERIFICATION. Reuse actual original LocalModelRunRunner/062 four budget scopes/current native source grants/066 Ticket/088 persistent Run+Step/058 pinned configuration; new fault matrix demonstrates the implemented composition. No new provider authority, shadow monetary or Run ledger. native4 376PASS/0FAIL-SKIP/pkg,41 new matrix events; pure-current1 original530 separate compatibility check, not additive capability count. Root current089 whole3 10652PASS/0FAIL-TEST_SKIP/pkgFAIL, Go test/vet/build/2CLI0;921current/frozen inputs and918completeAPI stable;48old synthetic rows/all original public xmin/catalog/089unused down-reapply stable;245actualownedDBs independentlySQLabsent. Original AC decision work/v5-age038-resume/bt-v5-air-010-original-local-ac-decision38hi.json; whole work/v5-age038-resume/air010-air049-current-whole-root38he.json. Archives3884(10)/4034+1032appendix(49) independent original byte/SHA checks; all true REDs retained, old077 single-table maintenance SQL narrower due actual089FK, protected specific077guard preserved. No live provider/real monthlyquota/production alert/device089/race/AT acceptance, exact314Flutter unchanged prior results reused. ClosedPilot/Beta NO.

### BT-V5-AIR-011 — 预算与请求前数据出口检查

CODE_AND_LOCAL_VERIFICATION: work/v5-age038-resume/air011-original-local-ac-decision38fg.json; native29 316 PASS/0 FAIL-SKIP; actual current schema88 default-concurrency go test ./... -count=1 -timeout=30m -json 10536 PASS/0 FAIL-SKIP/pkg, vet/build/2CLI all exit0, 911 frozen inputs/908 API byte-stable; 088 up/down/reapply original public/catalog/xmin preserved; four actual OS kill/restart original-ID control only; original062 four budget ledger every Once/Retry/Run; source/Session/fence/lease/ticket waits current and atomic; metadata/redaction scanned; main+9 HTTP owned DBs independently absent. PERSON scalar ActivityQuery only; other purpose denied/default OFF; real provider/secret/tokenizer/price/billing/-LIVE NOT_RUN. No native88 phone/Pilot/Beta activation; unrelated PARTIAL/UNKNOWN untouched.

### BT-V5-AIR-014 — 事件信封与类型注册

docs/testing/evidence/agent-events-2026-10-02/README.md: actual 7-file closed EventEnvelope/catalog/native PG metadata producers MomentCreated/UserQuery, server current session/exact Agent/source ownership and replay; native Moment revision, Task real updated_at+SHA opaque token (not CAS/consent), source-derived lifetime and stable logical-op/event IDs. Scope170 PASS, full Go3x2950 PASS,0fail/testskip; vet/build0, whole public rows+API source stable, ownDBremoved; 40original evidence/7source hashes verified, coverage90.5%. -race blocked by CGO0/no gcc, initial test/tool failures retained. CODE_LOCAL only/all eventsUNAVAILABLE; no schema/route/hooks/outbox/consumer/model/external ops; ClosedPilot/ConsumerBetaNO.

### BT-V5-AIR-015 — 事务 outbox 与幂等消费

Root original AC verified: work/v5-age038-resume/air015-local-closure-root38kw.json SHA256 561c2537d4f70555d15eff6ccd979cb82c9720f006855141b5c817c99a61609b. Current frozen946 API+seed whole Go10797 PASS/0FAIL-SKIP-packageFAIL, test/vet/build/two CLI exits0;307 raw-owned databases SQL absent. Three real Moment mutations same transaction with outbox; real nonzero candidate/effect,100 duplicate deliveries and handler upgrade preserve original effect; actual child os.Exit86 precommit rollback/restart and committed HTTP lost-response original receipt reconciliation. Two canonical old prefixes and original2015 archive hashes verified. Original 38p natural initial cause UNKNOWN, not explained/fixed; other registry hooks, production consumer/deployment, phone and end-to-end exactly-once not claimed. ClosedPilot/ConsumerBeta NO.

### BT-V5-AIR-017 — 重试死信和人工恢复入口

CODE_AND_LOCAL_VERIFICATION original017 AC completed. Current090 frozen whole go test ./... -count=1 -timeout=30m -json:10677 PASS/0FAIL-SKIP/pkg; vet/build/two actual CLI build all0. 927 current/frozen inputs=924 completeAPI+3 originalseeds stable;48 old publicrows/allxmin/visiblecatalog up-unuseddown-reapply/nativecleanup preserved;248 actual owned whole main/HTTP/migration DBs independently SQL absent. Native4 47PASS/five0, original nonempty089 FAILED/step/dispatch/audit+xmin roundtrip +090 useddown55000, actual child audit wait across original4s deadline rollback, concurrent one child/root6/child1/original effect once, source/grant/Session/ABA/actor/strict registeredHTTP deny covered. Closed classes distinguish exhausted/unknown historical cause; permanent invalid never retry; immutable FAILED history retained, original no-effect/outbox/ledger/inbox/dispatch proof required; original082/064 authority reused/defaultOFF. Root original AC decision38hv, wholeproof38hr-root-whole1/native4proof38hs/archiveproof38ht/finalcanonicalproof38hw. Original2991 archive files134443142bytes and all initial native1-3/compile/HARNESS failures preserved; finalannex78ee8c... current2canonical exactold suffix. Flutter314 exact Dart/test/pubspec hash reuse of prior1245functional+150loading/analyze-test-Debugbuild0; no newFlutter/device090/performance claim. No production/provider/scheduler/generic ModelRun or control replay/full DLQ paging/phoneRunUI/TalkBack/race; ClosedPilot/ConsumerBeta NO. Original goal remains pre-implementation gap snapshot; current_gap_audit records actual implementation.

### BT-V5-AIR-018 — 撤权删除取消和过期传播

Original AIR018 CODE_AND_LOCAL_VERIFICATION: original native authority/approval/effect/dispatch ordering retained. Before commit revoke blocks new dispatch; after commit original in-flight and UNKNOWN accounting retained without next steps/refund/network recall claims. Original source version/xmin plus minimal094 invalidation guards reject late old-source candidate/Memory. Actual unexpired single edit/withdraw cleanup0 RED fixed by bounded095 original current-source IS NOT TRUE scrub, own5 precise scopes only; NULL/0/101 bounds, no human read required, SKIP LOCKED, concurrent sum1, independent human row/xmin, native current-data roundtrip and actual cleanup CLI repeated/concurrent/restart verified. Native6 72PASS/five0 and six-round123 owned DBabsence; source/code archive actual1510 files. No new grant/effect/Run/source authority ledger. Current merged095 whole Go11126 PASS/0FAIL-SKIP/packagefail; test/vet/build/twoCLIbuild0.981 current/copied/before-after sourceSHA exact at closure. Original11003 whole and own72/103 all passing branches/multiplicities retained, normalizing ONLY random fixture UUID in parameter names; initial exact-name comparator24 mismatches are retained as proof harness error, not missing tests. Original094137 tables48 rows/xmin/full catalog/down-reapply and095140 tables rows/xmin/only cleanup function delta/down-reapply exact; native parent140 exact,369 actual raw emitted owned DB+parent independently SQL absent. Immutable root1431files manifest 33b1d43ef35a96b8fbab9e638e0a425c8f42f431ff13e0c28661d34ed61c97be. work/v5-age038-resume/whole095-joint-root38ox.json SHA256 d8c7815e4621515f0934336903d9897b01b943decf9fe7685e0d9fe774439f62,work/v5-age038-resume/whole095-archive-root38oy.json SHA256 2adb826746bf384c98c853de0856808a3a4cda4bcae6f770024ae32ab9db44f3,work/v5-age038-resume/air018-target-native6-root38ok.json SHA256 7e3e7748ed2fb22061b556f2c410bd6cf0ccefdbe441b9c842e69ecf778ce419,work/v5-age038-resume/age069-target-and-joint-preflight-root38op.json SHA256 df89714c0da03034d5f08c47b4811b10f43876ee428b6a83712fb682c6b45bce. Phone remains verified094 Debug, current342 Flutter1608/three0 source unchanged at closure; no claim current095 phone or real vector/provider deletion/production scheduler. Initial native failures and exact repairs preserved; race not run CGO0/noGCC. Model/real automatic write/Vision/A2A OFF; calibration007 and attendance027/ACTN002 gates unchanged, ClosedPilot/Beta NO.

### BT-V5-AIR-019 — 计划与上下文触发器

2026-10-06 SOURCE_RELATED_UNIT_VERIFIED_PARTIAL: API/native Store/独立CLI/DST/静默/滚动触达额度及原Inbox接线已实现，397相关单位事件通过；099迁移、实际PG源/会话/时钟/锁/并发/持久化、消费者设置入口/真机/部署未验证或未接入。只spy/单位不能证明实际运营调度，恢复条件为设置接线和独立整合验证。 Evidence docs/testing/evidence/notification-schedules-2026-10-06/worker-delivery-final04.json; root work/v5-age038-resume/private-draft-air019-unit-review-01/proof.json / this checkpoint verifies source9 and raw20+86. No whole/vet/build/DB/device/provider/production; ClosedPilot/Beta NO.
2026-10-06 NOTIFICATION_SCHEDULE_CONSUMER_SOURCE_RELATED_UNIT_VERIFIED_PARTIAL: original061 classifier remains distinct from actual099 schedule Settings consumer, no UNCONFIGURED implicit plan; complete explicit fields/preview/version0 CAS, current source and safe UNKNOWN-only read reconciliation. Actual entry/time-picker ABA/missing API/feedback/SQL clock gaps repaired; initial harness and business failures retained, unfrozen intermediate source SHA not fabricated. final05 64behavior+4loader/exit0; final06 copy-only3source/new-page assertion updates, only3direct cases each1behavior+1loader/exit0 (not64 rerun). Go13events/4tops/0skipFail/exit0, static productionSQL plus actualvalidator notPG. Root verifies102manifest/10current sources/frozen9historical05,364other inputs exact/oldentry bytes recovered/original061tests and controller exact/canonical prefix. docs/testing/evidence/notification-schedule-consumer-2026-10-06/README.md; root work/v5-age038-resume/notification-consumer-unit-review-01/proof.json; manifestSHA 5f062e184d43190191959e70e2bf31d994cd67fa317da2b6a676e1cd547e9285. 本人Settings真实定时汇总入口/099 DTO/明确草稿预览及CAS/409重审/未知GET-only恢复、身份/transport/ABA退休、长表单反馈与单共享写时间戳已源码完成。final05相关64行为+4加载PASS；final06仅3源文案及新测试中文断言，3直接case通过；Go相关13事件PASS。新099迁移/原生PG/HTTP持久化/锁并发重启/实际调度部署送达/IdP/真机尚NOT_RUN，单元与配置保存不是运营可靠性。恢复条件为这些真实证据及部署条件，本轮按用户仅单位，不自动解除原发布门槛。 ClosedPilot/Beta NO.
2026-10-06 LATE_AUTH_SAVE_UNCERTAINTY_RELATED_UNIT_PASS_PARTIAL: 已修复原保存后会话复验返回401/403的客户端误判；5行生产增加，原单元正文保留，3定向单元PASS，原始RED两行为失败保留。保存未知只GET当前状态，不把匹配当前计划当原操作回执。099/原生HTTP/PG/锁/重启持久化、实际调度投递、真机/认证/全面检查与构建未运行，整个AIR019仍PARTIAL。 Root source/raw/hash review without unit rerun: docs/testing/evidence/notification-schedule-late-auth-2026-10-06/root-delivery-freeze03.json; frozen manifestSHA256 4428843e6e49172b24e0102b67c9f9d8156802bd51e3eae23e2d1456f20ab711; ClosedPilot/Beta NO.
2026-10-06 ACTUAL_NOTIFICATION_PRE_WIRE_IDENTITY_BOUNDARY_UNIT_VERIFIED_PARTIAL: 实际原通知设置/计划控制器同步通知后发出旧身份请求的窗口已修复，捕获本次请求头且在GET/PUT/未知核实GET前复验原generation/request；晚路由401/403按保存未知且只读当前设置。29定向单位PASS，20真实RED失败保留。原生097/099/真实HTTP并发/锁/撤权/重启持久化、调度投递、真机/认证、全量分析测试与构建NOT_RUN，整体仍PARTIAL。 work/notification-transport-boundary-2026-10-06/freeze03/manifest.json SHA256 f4c2e2e8716093ef4f85917d6b724dd979f5bd2bbfba27c74d7d8688b7fad234; docs/testing/evidence/notification-transport-boundary-2026-10-06/README.md. red01 exit1 29run/9pass/20fail, green02 exit0 29pass/0failSkip 3.015s. Only one new unit file, preserved old helpers main not invoked/old suites not rerun, eight related input frame stable. No current whole-tree/native/device claim. ClosedPilot/Beta NO.
2026-10-06 ORIGINAL_NOTIFICATION_TIMEOUT_RESULT_UNIT_VERIFIED_PARTIAL: 本人通知设置/计划及发请求前身份guard、晚鉴权未知结果已写源。新增PUT408传输超时沿原GET-only核实、不重复批准；10直接单元PASS/8真实RED行为失败，2条件修改，旧29/64未重跑。408为合成transport并非当前真实服务输出；原生097/099/PG/HTTP/锁/撤权/重启、调度运营、手机/真实身份、全面检查/构建NOT_RUN，整体仍PARTIAL。 docs/testing/evidence/notification-save-timeout-2026-10-06/root-delivery-freeze03.json; freeze manifestSHA256 be80aa2d0a1b3254f0963c137646089e4fdbfd5d5979db247251a1490aac6a6a. Exact command Flutter test --no-pub --reporter json test/notification_save_timeout_test.dart at D:/Project/birdtie/apps/client; red01 exit1 run10/pass2/fail8, green02 exit0 run10/pass10 in3.047s. Each current8 related input frame stable, raw failure and original fixtures preserved; no receipt invented from matched current settings, no root/whole unit rerun; ClosedPilot/Beta NO.
2026-10-07 CODE_AND_LOCAL_DONE：新Settings父transport来源同帧替换/ABA/弹窗确认前守卫已实际Widget RED→修原current/entryChanges，4新行为+旧入口1PASS，最终全Flutter2840PASS/Debug当前07安装SHA核验；真实IdP缺失所以认证Settings手机仍未验。原生099复用真实Store/GET/PUT/原Inbox，合法PG未来分钟两次到期、2pool唯一slot/Inbox/EMPTY、提前off/quiet/session撤销0effects、新Session不换绑、DST真实SQL6、跨时区滚动budget及receipt SETNULL不清零、原活动提醒与故障隔离全部实测。新增native21叶+原受影响HTTP19叶分三命令40unique/47父子PASS；首compile/fixture失败保留，不称最后一次整文件通过。真实HTTP编码后退休Agent仍200已RED→复用原当前同Person/Agent/Role，GET/PUT403且已提交PUT不称撤销。fresh001-104、099 emptydown/reapply/current-data/down/up0；148原非099表rows/xmin/catalogexact，used-down实际P0001拒绝；唯一ownedDB删除和独立absence核验0。证据 docs/testing/evidence/notification-schedule-native-2026-10-07/README.md 及root-review。范围只是本地；部署调度器/外推送/provider/OS restart/race/认证真机/AT/生产仍NOT_RUN，Pilot/Beta NO。current08全Go首4prereq失败补跑四PASS但整命令仍exit1，analyze8warning279info仍1，不借旧PASS冒充新HTTP全量帧。

### BT-V5-AIR-022 — 读取 AGE 权威数据与最小上下文

CODE_AND_LOCAL_VERIFICATION native5 96PASS/0FAIL-SKIP/694stable/fulloldpublic/ownedDBDROP; adapter94.7pct coverage,targetvet/build0. Root independent9archivefileSHA/current6 checked; parallel-full076-2 whole9066PASS/0FAIL-SKIP/pkgFAIL,Go vet/build0/694stable/fullpublic-catalog same/ownedDROP, initial whole1 failures preserved and original assertions retained. Root archive docs/testing/evidence/chat-entity-device-2026-10-04/parallel-code-final1 and work/v4-cht003-resume/parallel-code-root-acceptance.json. Actual registered native Runtime→bounded related projection→single adapter→complete original seal final Revalidate; UTF8_JSON_BYTES_V1 whole encoded answer/facts/provenance/closed counts included, tokenizer UNKNOWN_NOT_TOKENIZED, complete policy anchor, whole fragment omission, UNKNOWN/NOT_REQUESTED/NOT_RELEVANT/OMITTED_BUDGET distinct. Data is untrusted not execution permission; no model/Memory write/provider tokens/phone/AT/production claim; ClosedPilot/ConsumerBeta NO.

### BT-V5-AIR-023 — 主体权限过滤与上下文快照

CODE_AND_LOCAL_VERIFICATION only. Final frozen986 whole root-whole023-09538qz:11157 RUN/PASS,0FAIL/SKIP/package failures; Go test/vet/build/control-cli/run-cli exit0. Root independent38rj confirms all986 live/copied/before/after,977 unchanged original inputs,3 original implementation+1 clock-fixture changed and5 new; all11126 old/11154 first/123 target/11155 prior passed branch multiplicities retained. Schema094/095 fresh,140 tables rows/xmin/catalog,unused down/reapply unchanged;388 actual raw childDBs+parent independently SQL absent. Native actual memberB200 audit misattributed B0/A2 RED fixed using current authorized server actor while immutable creator preserved; forged actor400/revoked member403 no effects. First whole11155PASS2FAIL clock fixture kept, only two volatile timestamps changed to statement_timestamp, assertions/production auth unchanged. Full raw/final986/archive1314 files in docs/testing/evidence/agent-context-authority-2026-10-06/final-root38rk; archiveSHA256=b9a4bcdaa7d8a7bd8152a659d6aede4ec482d3f5edd94727fda25f9d56791a2d. Native synthetic/local only; CGO0 no GCC raceNOT_RUN; phone currently349 Profile+baseline981API,final986 device NOT_RUN; model/automatic write/Vision/A2A OFF,Org private model port UNAVAILABLE. Current recording repair continues. ClosedPilot/ConsumerBetaNO.

### BT-V5-AIR-027 — Prompt 与 schema 版本登记

CODE_AND_LOCAL_VERIFICATION. Actual088 Run/Step→058 immutable binding/central config reused, all accepted native model requests validate pin before dispatch; deterministic083 not model Run. Actual PLANNED R1v1→v2/R2→two OS reads/exit0/full config+artifact hashes→rollback/R3v1 preserves history; RunID!=BindingID, no private answer/Ticket/UNKNOWN resend. Native3 372PASS/0FAIL-SKIP/pkg (19config cases), current full10592PASS/0FAIL-SKIP/pkg, Go test/vet/build/2CLI0. Root914frozen/current bytes/911complete API,48synthetic old rows/native all xmin/catalog/unused088 down-reapply stable. 2945archive original bytes/SHA/45754795 verified, native33/full229owned DBs independently absent. Native1 12PASS/7FAIL fixture raw preserved; compile summary not raw. No production Go/DDL/UI/provider addition. Root original AC work/v5-age038-resume/air027-original-local-ac-decision38gi.json, whole work/v5-age038-resume/air027-current-whole-root38gg.json, archive work/v5-age038-resume/air027-native3-and-archive-root38ge.json. Current314Flutter hash matches prior1245+150/analyze-test-Debugbuild0 reused, no rerun/device088/performance/AT/race/live approval. ClosedPilot/Beta NO.

### BT-V5-AIR-028 — 结构化输出验证与注入隔离

CODE_LOCAL root38zaf/zah: final05 actual1003 live/frozen SHA exact, original990 source and001-096 migration/seeds retained; true native accountUUID-as-activity RED and terminal classification RED preserved. Root go test ./... count1 timeout30m json exit0/11314 cases/89pkg/0fail-skip, original11215 multiplicity retained after fixtureUUID normalization, +99 events/90 new leaves; vet/build/two CLI builds each0. Owned root141 rows+xmin+catalog preserved with seed094095096 up/down/reapply; independent readonly SQL400actualchildren+root absent. Worker426target PASS/7292 index+7296archive allSHA exact +27DB absent checked. Evidence docs/testing/evidence/model-output-isolation-2026-10-06/root-whole38zah/README.md MANIFEST cd7bf010072f0272699ce13bcedf113eccd5b78b146d333e1ed7f1c68a0ed810; raw commands/CWD/exit/source/events archived. PHONE API still997/9676; formal provider/IdP/network/tokenizer/region/billing and new format-specific OSkill NOT_RUN, CGO0 race NOT_RUN. No outbound input expansion/tool authority/autoWrite/Vision/A2A; Closed Pilot/Beta NO.

### BT-V5-AIR-036 — 受限 Planner 和类型化提案

CODE_LOCAL root38zbs/zbv: actual final07 1015 live/frozen exact; original1003 retained1000 exact/3 non-test changes+12new; all001-096 DDL/3seeds/go.mod/sum/oldtests unchanged. Native unknown-goal no-model clarification, typed query/detail/current-owncandidate-review only,3step/2call/30s caps, malicious/ambiguous/code/confirmation/ACL/session/source-xmin/expiry controls verified; no candidate accept/reject/refresh/Memory write. Root whole Go test count1 timeout30m json exit0/11423 case events/90pkg/0fail-skip, original11314 multiplicity UUID-normalized retained +109 actual final07 target exact. Vet/build/twoCLI each0. Owned001-096 migration/3seed/094095096 up-down-reapply and141table rows/xmin/catalog retained. SQL 400 raw actualchildren+parent andworker22 names (423 distinct) absent; root9460 ZIPbyte/SHA/CRC and1015source exact. Evidence docs/testing/evidence/bounded-planner-2026-10-06/root-whole38zbv/README.md manifestf470261dc2ba3da48b5d974388dab088bc18ed98929b091da27d42628b9980f9. Existing native RED retained; fiveappendonlycanonical. Model/provider/mainHTTP unavailable/defaultOFF; physical plannerUI/formalIdP/realprovider/newplanner-specificOSkill NOT_RUN; originalOSkill/restart fullrerun,raceCGO0NOT_RUN. Phone remains997/9676. No outbound or authority expansion/autoWrite/Vision/A2A. ClosedPilot/BetaNO. RecordingNow retains firstcoldtapUNKNOWN_OPEN and IN_PROGRESS.

### BT-V5-AIR-037 — Tool Registry 和确定性许可判定

CODE_LOCAL root38zen/zeq: final08 1030 live/frozen/archive exact;5 existing non-test source changes+15new; old1010other/alloldtests/001-096DDL/3seed/mod byte exact,5 canonical prefixes preserved. Closed registry original public activity.search/detail and sandbox.write only; native original ModelRun/Planner/ResultProjection handle, JSON/model confirmed cannot grant. Current session/subject/source/ACL/xmin/065policy/expiry independent ALLOW/DENY/CONFIRM; real current reads, true empty, wrongIDs/injection/ABA/revoke/crossRole/final-payload PG expiry/lock deadline/3step one-use caps verified. Sandbox CONFIRM only, NO approval/dispatch/effect/durable writes (AIR040 pending). Actual runtimeRED01 and compile03-05/fixture06/wrongpackage regress10 preserved; successful220target/1095regression not counted as whole. Root Go test ./... count1 timeout30m json0 11644 run/pass/91pkg/no fail-skip, all11423 old multiplicities UUID-normalized retained+221 exactly actual220target+1 LocalPlanner antiforgery. Vet/build/original2CLI0; worker activity-reminders/outbox-maintenance0. Actual isolated001-096+original3seeds+094095096 up/down/reapply,141rows/xmin/catalog retained. 400 actualrootchild+parent+30worker (431distinct) SQLabsent. Root11763 ZIP byte/SHA/CRC and1030source independently verified. Evidence docs/testing/evidence/deterministic-tool-permission-2026-10-06/root-whole38zeq/README.md manifest03c74e4ac09274cdc06771d879dc648e4b4b49fc54cc9cd768b3b519b76fd67e. Default mainHTTP/model/provider/autoWrite/Vision/A2A unavailableOFF; phone remains997/9676, no activation/outbound/fees/publicwrite/deploy. FormalIdP/newtoolUI/physical/production/concurrency capacity/newToolPlanOSrestart NOT_RUN; old ModelRun OSkill/restart rerun, raceCGO0NOT_RUN. Recording Now remains IP, firsttapUNKNOWN; moretools6file source+auto increment underway separately. ClosedPilot/BetaNO.

### BT-V5-AIR-039 — 活动人物地点检索适配器

2026-10-06 SOURCE_REUSE_AUDIT_ONLY: Activity READ registration/native current projection already implemented in AIR040; actual source inspected, reused instead of duplicated. Place/person READ descriptors/schema/native ACL adapters still absent; task remains TODO. Evidence work/v5-age038-resume/air039-source-reuse-audit-01/proof.json. No unit/full/build/device run for this audit; no activation.
2026-10-06 CURRENT_READONLY_REGISTERED_CONSUMER_SOURCE_RELATED_UNIT_VERIFIED_PARTIAL: place.search/person.search use original task slots/bounds/field ACL/source and exact native owner/session/policy; explicit person.match reuses original bilateral opt-in source, not public FindPerson or machine SOCIAL authority. Actual registered ordinary POST/special/GET encode-before-final-revalidate, real empty vs typed source errors; legacy human activity invitation visibility retained separately from existing PUBLIC model tool. Native policy writer advisory added before new human settings-table freeze; action proposal deadlines capped before mapping. red01 2 actual handler failures and expiry-red06 actual mapper failure preserved; final09 clock fixture precision failure disclosed, validator unchanged. final10 90/90 events,21 top/3pkg; old-related11 238/238,11 top/4pkg; -vet=off/exit0/CWD apps/api, no root rerun. Root verified22 frozen source/19Go/10modified12new, original3 descriptors exact, original3 registry cases only catalogue3->6,2canonical prefixes,7 explicitly synthetic registered wires. docs/testing/evidence/current-readonly-tools-2026-10-06/worker-delivery-freeze13.json; work/v5-age038-resume/current-readonly-unit-review-01/proof.json; manifestSHA 478118b5dda014342190a3825e936c4ea0eefc46808a3c53892b859bcbd070a2. 地点/公开人物只读registration及普通HTTP实际消费、独立明确双侧opt-in匹配、原同事务来源与末次当前授权重验已接线。普通活动邀请ACL和模型PUBLIC-only原工具保持；新human policy锁复用原writer advisory，卡片操作期限先随policy收口。90新增/238旧相关单位事件PASS，22源冻结及原失败核证。实际新PG SQL/锁并发/撤权ABA/真实来源与匹配/原生HTTP/客户端真机尚NOT_RUN；表SHARE跨owner性能及旧037/040锁未核验。需这些实际当前证据后才可完整DONE，单位不是生产或发布验收。 ClosedPilot/Beta NO.
2026-10-07 NATIVE_CODE_AND_LOCAL_VERIFICATION_DONE: 原生注册Place/Person/Match、原Activity最新人数0→1→0/容量/标题/改期、未知ID/真空/不可见/双向封禁/撤权ABA/seal拒绝，真实PG钟期限和两种原writer等待全部实测。final-current12三包133父子测试PASS/0fail/0skip/exit0，45新增叶场景；fresh001-104/3开发seed，自有fixture/子schema残留0。最后现有API生产源码和旧测试与上轮全量current06 SHA全相同，仅新1原生测试文件。完整首败及暂改去xmin造成原安全RED03后完全恢复保留。普通Match session刷新409兼容限制及全表SHARE跨owner等待已明示，不声称修复或性能全面通过。独占证据 work/v5-age038-resume/air039-native-checkpoint-2026-10-07/README.md、final-proof.json、stage-index.json，原Task/source/权限/ID重用，无新DDL/出网/真实Agent写/生产激活；真实IdP/人员供给/完整UI/部署A-H仍NOT_RUN，Pilot/Beta NO。

### BT-FIX-CITY-001 — City Seed 活动审核发布的主办方兼容修复

docs/testing/evidence/city-activity-organizer-publish-2026-10-03/README.md + manifest.json: actual four typed Store/HTTP submit-review-public-rebuiltPool path, nine source hashes, native154/HTTP124x3, legacy nil recoverable409/reject, revoked/private/wrongnamespace/member/source-expiry/City expiry403 and no duplicate; real final-cursor withdrawal and expiredCity reject RED preserved/fixed. 059 fresh/058 full old public rows/up/protected nonemptydown3/emptydown/reapply/84 expected commands pass. Current059 three default fullGo each5484/0fail/testskip (18 no-test packages separate), fullvet/build0,425APIhash/fullpublic rows stable,ownedDBdrop. 219 archives/root independent02458/031166 verified. No UI/finalSession/COMMIT TTL/Venue-lockwait/production claim; ClosedPilot/ConsumerBetaNO.

### BT-FIX-INT-DRAFT-001 — 录屏私人意图回归：渐进同一草稿、真实回执与未知结果安全

CODE_AND_LOCAL_VERIFICATION ONLY. Real progressive same-object PRIVATE drafts + UNKNOWN modality/area/date, same summary/full editor/save. Original API096 keyed transactional intent/audit/immutable owner-operation-source receipt; session/current authority; secure prewrite-readback and owner/environment/transport ABA; reopen reads not autoPOST. Final native-wire05 target24PASS/test0; client09 target72PASS/analyze0 preserving original50;247 artifact files+zip and22 source SHA exact. Root frozen367 Flutter1825 behavior/184 loading/0FAIL/SKIP analyze/test/Debug/Profile0; frozen995 Go11181RUN/PASS 0testFAIL/SKIP,16no-test packages,vet/build/2CLI0 and001-096 real3seeds/migration/current rows-xmin-catalog preserved. Original11157 normalized randomUUID branch multiplicity retained. Final installed PROFILE APK e7e4c5e9fc4fe7f783aaea0f28e82eba395075b996cd50d2182878fbabcbc183. Actual native localhost3697->995API9675->ownedPG096 POST201 requestID, intent dcb61c1c-bea7-4ca1-9ded-d55454c9f2ae +operation89fd4b11-716e-40e7-8b66-c6bd37c324e0:1intent/1receipt/1audit PRIVATE DRAFT; same APK reinstall at COMMITTED checkpoint old21528/new27183 reads sameID and counts with restored reserved development session. First amkill did not kill, not restart evidence. Original HTTP commit-response-drop/new handler reconciliation actual automatic; physical pending-UNKNOWN/APIrestart/multipleOrg-account matrix NOT_RUN. Dev noSMS/noverifiedphoneownership not productionIdP; original201 phone body not captured, actualUI/server requestID+SQL separately labelled. Root final456 files screenshots/10videos/30 samples and per95source matrix/commands in docs/reports/BIRDTIE-RECORDING-REPAIR-2026-10-06.md and docs/testing/evidence/recording-repair-2026-10-06/root-final38um/MANIFEST.json. Final independent work/v5-age038-resume/recording-final-independent38uj/result.json. No public activation/invites/messages/production. Now full32QA stillIN_PROGRESS,IdPfalseBLOCKED,ClosedPilot/ConsumerBetaNO.

### BT-FIX-AGENT-PUBLIC-001 — 录屏实测回归：匿名特殊查询真实身份传递与澄清响应

DONE_CODE_LOCAL_ONLY: real503 RED18 retained; caller anonymous actor corrected without relaxing human/organization guard. Worker107 old+new PASS; root frozen997 native096 full11215 test cases/89 packages test/vet/build/twoCLIs exits0; old11181 UUID-normalized multiplicities preserved,400 raw children+rootSQL absent; migrations094095096 up/down/reapply retain data/xmin/catalog. Current phone API9676/ADB3697 actual weekend200 unsupported message, boundedCity cancel0/resume1 HTTP200 req6605da3a7cb21f82bf6bdf0da981d682; final3b03581 Profile/badminton3 local fixture activities+realMapbox. Evidence work/v5-age038-resume/go997-independent-closure38xs.json, root-whole-anon09638wh, phone-final-query38ww/cancel-resume-proof.json; formal1191 SHA7c9a777a7177c233ef30de433faca0a562b18d4a14800aed7deacd53fbf6e27c. Full productionIdP/pilot BLOCKED, NO.

