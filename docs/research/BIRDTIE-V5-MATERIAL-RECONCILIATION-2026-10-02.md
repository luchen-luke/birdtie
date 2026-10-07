# Birdtie V5 四份材料对账（2026-10-02）

## 范围与证据口径

本报告独占新增，与 root 正在实施的 **BT-V4-SAF-004** 分开；不领取任务，不修改源码/迁移/AGENTS/既有UI规则，不导入或改写实时队列。本轮仅读取实际代码、来源和保存证据；未运行会写状态的测试、构建、安装、数据库或外部服务。不是功能实现报告。

- 官方仓库 D:/Project/birdtie；分支 **master**，HEAD **d6e86d3e6d82e17eaf9ca2e46d43b47e5e0cbf2e**，UTC **2026-10-02T10:50:50.072463+00:00**。
- 未提交文件 421 项，状态摘要 SHA256 a2e767dc8cbb1eda62fc7a45df9fdd5c48ebf96bf4416eb5d4700296cb13b428。活跃写入期间仅 best-effort 只读快照，没有夺取 lease 或冻结执行。
- 实时队列 144 项：{"DONE": 96, "BLOCKED": 7, "TODO": 37, "PARTIAL": 3, "IN_PROGRESS": 1}；当前 BT-V4-SAF-004。队列 SHA256 8C24BD63BD2021F6526BF254C1DD6210ECE03BBEDF6715D44EA5102C68886659。规划81/56不改变旧任务完成数。
- AGE 原文81项（57 P0/22 P1/2 P2）；AIR原JSON56项（26 P0/25 P1/5 P2）。保留源ID；AGE规范化BT-V5-AGE-*仅供未来去重，未创建任务。
- REAL仅限定范围已有代码/审计交付；PARTIAL补真实差异；NOT_IMPLEMENTED核心目标尚无实现；BLOCKED_EXTERNAL单列live外阻（offline code_state另存）；UNKNOWN不猜测。REUSE/EXTEND/NEW是处置，均不自动DONE。
- SAF004未完成，所有新安全能力只记PARTIAL；当前新写文件不是验收。没有learner/live inference grant、模型provider、Memory service或live A2A。

## 1 原材料与到达记录

| 来源 | 实际路径 | 状态 / SHA256 |
| --- | --- | --- |
| AGE | docs/product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md | MATERIAL_AVAILABLE / 1ACD60FEF5642633C7E91B10909931989FFB59FF413BF8F242E5AABA00368218 |
| AIR_ZIP | BirdTie-BT-V5-AIR-Codex-Package-2026-10-02.zip | MATERIAL_AVAILABLE / E4251A7A4053CB39FE34E26C1A8D5D8A7F195DA910B0B8B474820B29B043BAD4 |
| AIR_DESIGN | work/v5-materials/BT-V5-AIR-DESIGN.md | MATERIAL_AVAILABLE / 839A0AF991B1701B61714EEB34C73C7143A8DCB8BDACDE30A574965C1273CC8C |
| AIR_REQUIREMENTS | work/v5-materials/BT-V5-AIR-REQUIREMENTS.md | MATERIAL_AVAILABLE / 3AD10255BB7056849215E04763F9D6800E4FC753A155FADBA1DD46134DC63406 |
| AIR_BACKLOG | work/v5-materials/BT-V5-AIR-BACKLOG.json | MATERIAL_AVAILABLE / 1A97E0E50E1E3DB2E2AF71EF91144D2B4EBE62487C3B9816536E2E3863B3E42F |
| AIR_EXECUTION_ENTRY | work/v5-materials/BT-V5-AIR-CODEX-MASTER-PROMPT.md | MATERIAL_AVAILABLE / 1F657310B7789EC1727B1EF01B05D7D1BEE535B1EFF3A03EFAD9DB0CD2C83D4E |
| UIUX_RULES | docs/product/BirdTie-Interaction-and-UIUX-Development-Rules-2026-10-02.md | MATERIAL_AVAILABLE / 20EC0D43442D4D95B9EEEA9C1E325572A09A7538307B66E8C79207039C8781CB |
| CONSUMER_ASSESSMENT | docs/product/BirdTie-Consumer-Readiness-Assessment-2026-10-02 (1).md | MATERIAL_AVAILABLE / E90AF1A47058DB23C653C88CF325FBD0D9A57774185050EC7620D3BD202B044B |

AGE原附件路径保留于JSON；AIR ZIP七个文件的根静态验证不是产品测试。AIR供应商/API日期声明本轮不作为当前账户事实核验，不据此选择provider或付费。

消费评估最初预期无后缀路径未找到，UNKNOWN是当时事实；之后root提供(1).md，已完整读取，**MATERIAL_AVAILABLE**，SHA256 E90AF1A47058DB23C653C88CF325FBD0D9A57774185050EC7620D3BD202B044B。缺原文问题已解决，旧missing截点不伪改为早已取得。源材料“当时无法读取AGE/仓库”的历史表述不冻结今天实际代码状态。

## 2 统计（不是产品成熟度百分比）

| Epic | 条数 | REAL | PARTIAL | NOT_IMPLEMENTED | BLOCKED_EXTERNAL |
| --- | --- | --- | --- | --- | --- |
| AGE | 81 | 2 | 50 | 29 | 0 |
| AIR | 56 | 1 | 30 | 19 | 6 |
| combined | 137 | 3 | 80 | 48 | 6 |

处置REUSE 6 / EXTEND 81 / NEW 50；不自动新增50个队列项，不用PARTIAL覆盖旧DONE。
三条REAL：AGE-055（Business≠Venue native边界）、AGE-058（Community无Agent）、AIR-002（本轮逐条审计交付，不代表候选/模型能力）。前两条读取真实041/role code与保存DB/HTTP测试，非只凭报告自述。
六条REUSE：AGE-055、AGE-058、AIR-001、AIR-002、AIR-006、AIR-055。AIR-001尚需当前checkpoint，AIR-006需新版本回归，AIR-055外部门禁阻断；复用不等于完整验收。
六条外阻：AIR-009/012/031/034 provider/媒体批准、凭证、费用与范围；AIR-054 live canary；AIR-055生产身份/HTTPS/组织活动/API地图/支持与调度。前四核心代码未成，后两代码范围PARTIAL；offline契约与受控实现可继续。

## 3 复用代码/测试/证据索引

同索引在逐条矩阵引用，JSON完整展开每条路径。保存PASS是原任务实际证据，本轮未复跑。已读041独立约束、role policy与052真实DB三轮日志；测试源码/fake仅证明限定合同，不作live provider证据。

### identity

稳定身份和可调用角色基础；Business Runtime 仍未开放、Community 不是 Agent。

代码/规范：[apps/api/internal/actorref/actorref.go](<../../apps/api/internal/actorref/actorref.go>)；[apps/api/internal/agentruntime/policy.go](<../../apps/api/internal/agentruntime/policy.go>)；[apps/api/migrations/019_agent_identity_organizations.sql](<../../apps/api/migrations/019_agent_identity_organizations.sql>)；[apps/api/migrations/041_business_principals.sql](<../../apps/api/migrations/041_business_principals.sql>)。

对应测试：[apps/api/internal/agentruntime/policy_test.go](<../../apps/api/internal/agentruntime/policy_test.go>)；[apps/api/internal/postgres/agent_ownership_integration_test.go](<../../apps/api/internal/postgres/agent_ownership_integration_test.go>)；[apps/api/internal/httpapi/actorref_workspace_test.go](<../../apps/api/internal/httpapi/actorref_workspace_test.go>)。

保存证据：[docs/testing/evidence/agent-permission-contract-2026-10-02/README.md](<../../docs/testing/evidence/agent-permission-contract-2026-10-02/README.md>)。

### profile

现普通个人公开/私密 Profile 与本人编辑；无版本化 AgentProfile/PrivateAgentProfile。

代码/规范：[apps/api/internal/identity/identity.go](<../../apps/api/internal/identity/identity.go>)；[apps/api/internal/postgres/profile_edit.go](<../../apps/api/internal/postgres/profile_edit.go>)。

对应测试：[apps/api/internal/postgres/social_privacy_integration_test.go](<../../apps/api/internal/postgres/social_privacy_integration_test.go>)。

保存证据：[docs/testing/evidence/agent-permission-contract-2026-10-02/parallel-round-1.jsonl](<../../docs/testing/evidence/agent-permission-contract-2026-10-02/parallel-round-1.jsonl>)。

### context

本人私密、多 Context 的明确声明；无访居事实推断或 Memory Builder。

代码/规范：[apps/api/migrations/033_context_graph.sql](<../../apps/api/migrations/033_context_graph.sql>)；[apps/api/internal/contextgraph/model.go](<../../apps/api/internal/contextgraph/model.go>)；[apps/api/internal/contextgraph/declaration.go](<../../apps/api/internal/contextgraph/declaration.go>)；[apps/api/internal/postgres/person_contexts.go](<../../apps/api/internal/postgres/person_contexts.go>)；[apps/client/lib/src/workspace/person_contexts_page.dart](<../../apps/client/lib/src/workspace/person_contexts_page.dart>)。

对应测试：[apps/api/internal/contextgraph/declaration_test.go](<../../apps/api/internal/contextgraph/declaration_test.go>)；[apps/api/internal/postgres/context_graph_integration_test.go](<../../apps/api/internal/postgres/context_graph_integration_test.go>)；[apps/api/internal/httpapi/person_contexts_integration_test.go](<../../apps/api/internal/httpapi/person_contexts_integration_test.go>)；[apps/client/test/person_contexts_page_test.dart](<../../apps/client/test/person_contexts_page_test.dart>)。

保存证据：[docs/research/BIRDTIE-V4-CTX-002-AUDIT.md](<../../docs/research/BIRDTIE-V4-CTX-002-AUDIT.md>)。

### memory

全仓库查找未发现权威 AgentMemory/MemoryEvidence/MemoryCandidate schema/service/API；相关规划是目标而不是代码。

代码/规范：无对应已实现记录。

对应测试：无对应已实现记录。

保存证据：无对应已实现记录。

### moment

原文字草稿、来源 revision/withdraw、OccurredAt 与明确实体链；没有分析出口/视觉/MemoryEvidence。

代码/规范：[apps/api/internal/content/moment.go](<../../apps/api/internal/content/moment.go>)；[apps/api/internal/postgres/moments.go](<../../apps/api/internal/postgres/moments.go>)；[apps/api/internal/httpapi/moments.go](<../../apps/api/internal/httpapi/moments.go>)；[apps/api/migrations/043_moment_context_links.sql](<../../apps/api/migrations/043_moment_context_links.sql>)；[apps/client/lib/src/content/private_moment_controller.dart](<../../apps/client/lib/src/content/private_moment_controller.dart>)。

对应测试：[apps/api/internal/postgres/moments_context_integration_test.go](<../../apps/api/internal/postgres/moments_context_integration_test.go>)；[apps/api/internal/httpapi/moments_context_integration_test.go](<../../apps/api/internal/httpapi/moments_context_integration_test.go>)；[apps/client/test/private_moment_context_test.dart](<../../apps/client/test/private_moment_context_test.dart>)。

保存证据：[docs/testing/evidence/agent-permission-contract-2026-10-02/migration052.log](<../../docs/testing/evidence/agent-permission-contract-2026-10-02/migration052.log>)。

### activity

真实后端开发 Participation/Plans 持久性；不代表现实出席、真实试点或未知外部效果账本。

代码/规范：[apps/api/migrations/021_core_activity_participation.sql](<../../apps/api/migrations/021_core_activity_participation.sql>)；[apps/api/internal/postgres/activity_participations.go](<../../apps/api/internal/postgres/activity_participations.go>)；[apps/api/internal/httpapi/activity_participations.go](<../../apps/api/internal/httpapi/activity_participations.go>)；[apps/client/lib/src/workspace/activity_detail_sheet.dart](<../../apps/client/lib/src/workspace/activity_detail_sheet.dart>)；[apps/client/lib/src/workspace/activity_plans.dart](<../../apps/client/lib/src/workspace/activity_plans.dart>)。

对应测试：[apps/api/internal/postgres/activity_owner_integration_test.go](<../../apps/api/internal/postgres/activity_owner_integration_test.go>)；[apps/client/test/activity_participations_test.dart](<../../apps/client/test/activity_participations_test.dart>)；[apps/client/test/activity_detail_sheet_test.dart](<../../apps/client/test/activity_detail_sheet_test.dart>)。

保存证据：[docs/reports/FUNCTIONAL-MVP-COMPLETION-REPORT.md](<../../docs/reports/FUNCTIONAL-MVP-COMPLETION-REPORT.md>)；[docs/testing/FUNCTIONAL-MVP-E2E.md](<../../docs/testing/FUNCTIONAL-MVP-E2E.md>)。

### places

已发布/当前授权 Place、Source freshness、保存和明确匹配；没有 semantic Place Profile 或 Place Memory。

代码/规范：[apps/api/internal/foundation/model.go](<../../apps/api/internal/foundation/model.go>)；[apps/api/internal/postgres/catalog.go](<../../apps/api/internal/postgres/catalog.go>)；[apps/api/internal/postgres/saved.go](<../../apps/api/internal/postgres/saved.go>)；[apps/api/internal/placematch/model.go](<../../apps/api/internal/placematch/model.go>)；[apps/api/internal/postgres/place_matches.go](<../../apps/api/internal/postgres/place_matches.go>)。

对应测试：[apps/api/internal/httpapi/place_matches_integration_test.go](<../../apps/api/internal/httpapi/place_matches_integration_test.go>)；[apps/api/internal/httpapi/place_context_integration_test.go](<../../apps/api/internal/httpapi/place_context_integration_test.go>)；[apps/client/test/place_detail_sheet_test.dart](<../../apps/client/test/place_detail_sheet_test.dart>)。

保存证据：[docs/testing/evidence/agent-permission-contract-2026-10-02/migration052.log](<../../docs/testing/evidence/agent-permission-contract-2026-10-02/migration052.log>)。

### relationship

本人独立默认关闭授权和好友/本人30天元数据；无正文、亲密度、推断兴趣或跨主体 Memory。

代码/规范：[apps/api/migrations/051_agent_relationship_consent.sql](<../../apps/api/migrations/051_agent_relationship_consent.sql>)；[apps/api/internal/relationshipcontext/model.go](<../../apps/api/internal/relationshipcontext/model.go>)；[apps/api/internal/postgres/relationship_context.go](<../../apps/api/internal/postgres/relationship_context.go>)；[apps/api/internal/httpapi/relationship_context.go](<../../apps/api/internal/httpapi/relationship_context.go>)；[apps/api/internal/httpapi/agent_intent_router.go](<../../apps/api/internal/httpapi/agent_intent_router.go>)。

对应测试：[apps/api/internal/httpapi/relationship_context_integration_test.go](<../../apps/api/internal/httpapi/relationship_context_integration_test.go>)；[apps/api/internal/agentworkspace/relationship_intent_test.go](<../../apps/api/internal/agentworkspace/relationship_intent_test.go>)；[apps/client/test/agent_relationship_page_test.dart](<../../apps/client/test/agent_relationship_page_test.dart>)。

保存证据：[docs/testing/evidence/agent-relationships-2026-10-02/README.md](<../../docs/testing/evidence/agent-relationships-2026-10-02/README.md>)。

### socialcontext

双侧独立允许的共同关系计数/社群/公开共同报名事实；不证明出席或兴趣。

代码/规范：[apps/api/migrations/048_social_disclosure.sql](<../../apps/api/migrations/048_social_disclosure.sql>)；[apps/api/internal/socialcontext/model.go](<../../apps/api/internal/socialcontext/model.go>)；[apps/api/internal/postgres/social_context.go](<../../apps/api/internal/postgres/social_context.go>)。

对应测试：[apps/api/internal/httpapi/social_context_integration_test.go](<../../apps/api/internal/httpapi/social_context_integration_test.go>)；[apps/client/test/social_context_test.dart](<../../apps/client/test/social_context_test.dart>)。

保存证据：[docs/testing/evidence/shared-context-2026-10-02/README.md](<../../docs/testing/evidence/shared-context-2026-10-02/README.md>)。

### intent

第一类明确 Intent、受众/生命周期及单独 PRIVATE 草稿和发布确认；非 Memory 或通用批准账本。

代码/规范：[apps/api/migrations/036_social_intents.sql](<../../apps/api/migrations/036_social_intents.sql>)；[apps/api/migrations/037_social_intent_audiences.sql](<../../apps/api/migrations/037_social_intent_audiences.sql>)；[apps/api/migrations/038_social_intent_agent_origin.sql](<../../apps/api/migrations/038_social_intent_agent_origin.sql>)；[apps/api/internal/socialintent/model.go](<../../apps/api/internal/socialintent/model.go>)；[apps/api/internal/httpapi/social_intents.go](<../../apps/api/internal/httpapi/social_intents.go>)；[apps/client/lib/src/workspace/social_intent_drafts.dart](<../../apps/client/lib/src/workspace/social_intent_drafts.dart>)。

对应测试：[apps/api/internal/httpapi/social_intent_task_bridge_integration_test.go](<../../apps/api/internal/httpapi/social_intent_task_bridge_integration_test.go>)；[apps/api/internal/postgres/social_intent_audience_integration_test.go](<../../apps/api/internal/postgres/social_intent_audience_integration_test.go>)；[apps/client/test/social_intent_drafts_test.dart](<../../apps/client/test/social_intent_drafts_test.dart>)。

保存证据：[docs/testing/evidence/opportunity-reasons-2026-10-02/README.md](<../../docs/testing/evidence/opportunity-reasons-2026-10-02/README.md>)。

### newpeople

双方明确意图、双侧开关/可见性、发送前事务重验、并发撤销/Block/取消与重复申请；不读推断私密偏好。

代码/规范：[apps/api/migrations/052_new_people_consent.sql](<../../apps/api/migrations/052_new_people_consent.sql>)；[apps/api/internal/newpeople/model.go](<../../apps/api/internal/newpeople/model.go>)；[apps/api/internal/newpeople/matching.go](<../../apps/api/internal/newpeople/matching.go>)；[apps/api/internal/postgres/new_people.go](<../../apps/api/internal/postgres/new_people.go>)；[apps/api/internal/httpapi/new_people.go](<../../apps/api/internal/httpapi/new_people.go>)；[apps/client/lib/src/workspace/new_people_page.dart](<../../apps/client/lib/src/workspace/new_people_page.dart>)。

对应测试：[apps/api/internal/newpeople/matching_test.go](<../../apps/api/internal/newpeople/matching_test.go>)；[apps/api/internal/postgres/new_people_integration_test.go](<../../apps/api/internal/postgres/new_people_integration_test.go>)；[apps/api/internal/httpapi/new_people_integration_test.go](<../../apps/api/internal/httpapi/new_people_integration_test.go>)；[apps/client/test/new_people_page_test.dart](<../../apps/client/test/new_people_page_test.dart>)。

保存证据：[docs/testing/evidence/new-people-2026-10-02/README.md](<../../docs/testing/evidence/new-people-2026-10-02/README.md>)。

### opportunity

current authorized real-entity RULE_BASED 规则与中文最小理由；既有 Tie/社群/关注不代表真实兴趣。

代码/规范：[apps/api/internal/opportunity/model.go](<../../apps/api/internal/opportunity/model.go>)；[apps/api/internal/opportunity/engine.go](<../../apps/api/internal/opportunity/engine.go>)；[apps/api/internal/postgres/opportunities.go](<../../apps/api/internal/postgres/opportunities.go>)；[apps/api/internal/httpapi/opportunities.go](<../../apps/api/internal/httpapi/opportunities.go>)；[apps/client/lib/src/workspace/opportunity_reasons.dart](<../../apps/client/lib/src/workspace/opportunity_reasons.dart>)；[apps/client/lib/src/workspace/opportunity_page.dart](<../../apps/client/lib/src/workspace/opportunity_page.dart>)。

对应测试：[apps/api/internal/opportunity/engine_test.go](<../../apps/api/internal/opportunity/engine_test.go>)；[apps/api/internal/postgres/opportunity_reasons_integration_test.go](<../../apps/api/internal/postgres/opportunity_reasons_integration_test.go>)；[apps/api/internal/httpapi/opportunity_reasons_test.go](<../../apps/api/internal/httpapi/opportunity_reasons_test.go>)；[apps/client/test/opportunity_page_test.dart](<../../apps/client/test/opportunity_page_test.dart>)；[apps/client/test/opportunity_reasons_test.dart](<../../apps/client/test/opportunity_reasons_test.dart>)。

保存证据：[docs/testing/evidence/opportunity-reasons-2026-10-02/README.md](<../../docs/testing/evidence/opportunity-reasons-2026-10-02/README.md>)。

### organization

现组织主体、成员角色、FAQ/发布授权；未完成扩展 Agent Profile、Memory 或AIR工具。

代码/规范：[apps/api/internal/organization/model.go](<../../apps/api/internal/organization/model.go>)；[apps/api/internal/postgres/organization_memberships.go](<../../apps/api/internal/postgres/organization_memberships.go>)；[apps/api/internal/postgres/organization_faq.go](<../../apps/api/internal/postgres/organization_faq.go>)；[apps/api/migrations/028_organization_membership_lifecycle.sql](<../../apps/api/migrations/028_organization_membership_lifecycle.sql>)；[apps/client/lib/src/workspace/organization_console.dart](<../../apps/client/lib/src/workspace/organization_console.dart>)。

对应测试：[apps/api/internal/postgres/actor_authorization_matrix_integration_test.go](<../../apps/api/internal/postgres/actor_authorization_matrix_integration_test.go>)；[apps/api/internal/httpapi/business_organizer_integration_test.go](<../../apps/api/internal/httpapi/business_organizer_integration_test.go>)；[apps/client/test/organization_membership_pages_test.dart](<../../apps/client/test/organization_membership_pages_test.dart>)；[apps/client/test/organization_faq_page_test.dart](<../../apps/client/test/organization_faq_page_test.dart>)。

保存证据：[docs/research/BIRDTIE-V4-E2E-003-AUDIT.md](<../../docs/research/BIRDTIE-V4-E2E-003-AUDIT.md>)。

### business

Business 与 Venue 独立主体/关系、活动授权已验；Business Agent pack/claim UI/native booking 未实现。

代码/规范：[apps/api/migrations/041_business_principals.sql](<../../apps/api/migrations/041_business_principals.sql>)；[apps/api/internal/agentruntime/policy.go](<../../apps/api/internal/agentruntime/policy.go>)；[apps/api/internal/postgres/social_activity_publish.go](<../../apps/api/internal/postgres/social_activity_publish.go>)。

对应测试：[apps/api/internal/postgres/actor_authorization_matrix_integration_test.go](<../../apps/api/internal/postgres/actor_authorization_matrix_integration_test.go>)；[apps/api/internal/httpapi/business_organizer_integration_test.go](<../../apps/api/internal/httpapi/business_organizer_integration_test.go>)；[apps/api/internal/agentruntime/policy_test.go](<../../apps/api/internal/agentruntime/policy_test.go>)。

保存证据：[docs/research/BIRDTIE-V4-E2E-003-AUDIT.md](<../../docs/research/BIRDTIE-V4-E2E-003-AUDIT.md>)；[docs/testing/evidence/agent-permission-contract-2026-10-02/migration052.log](<../../docs/testing/evidence/agent-permission-contract-2026-10-02/migration052.log>)。

### community

Community 成员与领域实体存在，但无 Agent runtime 或自动生成。

代码/规范：[apps/api/internal/community/social.go](<../../apps/api/internal/community/social.go>)；[apps/api/internal/postgres/community_social.go](<../../apps/api/internal/postgres/community_social.go>)；[apps/api/internal/agentruntime/policy.go](<../../apps/api/internal/agentruntime/policy.go>)；[apps/api/internal/actorref/actorref.go](<../../apps/api/internal/actorref/actorref.go>)。

对应测试：[apps/api/internal/agentruntime/policy_test.go](<../../apps/api/internal/agentruntime/policy_test.go>)；[apps/api/internal/postgres/community_social_integration_test.go](<../../apps/api/internal/postgres/community_social_integration_test.go>)；[apps/api/internal/httpapi/community_social_integration_test.go](<../../apps/api/internal/httpapi/community_social_integration_test.go>)。

保存证据：[docs/research/BIRDTIE-V4-COMMUNITY-RECONCILIATION.md](<../../docs/research/BIRDTIE-V4-COMMUNITY-RECONCILIATION.md>)；[docs/testing/evidence/agent-permission-contract-2026-10-02/README.md](<../../docs/testing/evidence/agent-permission-contract-2026-10-02/README.md>)。

### notification

确定性数据库幂等提醒、API离线独立worker/失败记录；无AttentionPolicy、Digest、模型触达或生产部署告警实证。

代码/规范：[apps/api/migrations/023_activity_notifications.sql](<../../apps/api/migrations/023_activity_notifications.sql>)；[apps/api/internal/postgres/activity_notifications.go](<../../apps/api/internal/postgres/activity_notifications.go>)；[apps/api/cmd/activity-reminders/main.go](<../../apps/api/cmd/activity-reminders/main.go>)；[apps/api/internal/inbox/model.go](<../../apps/api/internal/inbox/model.go>)。

对应测试：[automation/verify_activity_notifications.ps1](<../../automation/verify_activity_notifications.ps1>)；[apps/client/test/remote_inbox_source_test.dart](<../../apps/client/test/remote_inbox_source_test.dart>)。

保存证据：[docs/research/2026-10-01-activity-reminder-reliability.md](<../../docs/research/2026-10-01-activity-reminder-reliability.md>)。

### runtime

role/context pure policy 与同步确定性任务历史；没有推理gateway/run/outbox/tool/approval/effect执行内核。

代码/规范：[apps/api/internal/agentruntime/policy.go](<../../apps/api/internal/agentruntime/policy.go>)；[apps/api/internal/agentworkspace/model.go](<../../apps/api/internal/agentworkspace/model.go>)；[apps/api/internal/agentworkspace/intent_parser.go](<../../apps/api/internal/agentworkspace/intent_parser.go>)；[apps/api/internal/agentworkspace/response.go](<../../apps/api/internal/agentworkspace/response.go>)；[apps/api/internal/postgres/agent_workspace.go](<../../apps/api/internal/postgres/agent_workspace.go>)；[apps/api/internal/httpapi/agent_workspace.go](<../../apps/api/internal/httpapi/agent_workspace.go>)。

对应测试：[apps/api/internal/agentruntime/policy_test.go](<../../apps/api/internal/agentruntime/policy_test.go>)；[apps/api/internal/agentworkspace/intent_parser_test.go](<../../apps/api/internal/agentworkspace/intent_parser_test.go>)；[apps/api/internal/agentworkspace/response_test.go](<../../apps/api/internal/agentworkspace/response_test.go>)；[apps/client/test/remote_agent_task_source_test.dart](<../../apps/client/test/remote_agent_task_source_test.dart>)。

保存证据：[docs/testing/evidence/opportunity-reasons-2026-10-02/README.md](<../../docs/testing/evidence/opportunity-reasons-2026-10-02/README.md>)。

### coordination

AGA001 strict wire/server-only facts/最小ASK_USER纯合同；无持久grant、route、传输或live许可。

代码/规范：[apps/api/internal/agentruntime/coordination.go](<../../apps/api/internal/agentruntime/coordination.go>)。

对应测试：[apps/api/internal/agentruntime/coordination_test.go](<../../apps/api/internal/agentruntime/coordination_test.go>)；[apps/api/internal/agentruntime/coordination_wire_security_test.go](<../../apps/api/internal/agentruntime/coordination_wire_security_test.go>)。

保存证据：[docs/testing/evidence/agent-permission-contract-2026-10-02/README.md](<../../docs/testing/evidence/agent-permission-contract-2026-10-02/README.md>)；[docs/testing/evidence/agent-permission-contract-2026-10-02/no-transport.json](<../../docs/testing/evidence/agent-permission-contract-2026-10-02/no-transport.json>)。

### safety

当前 owner/role/purpose/默认拒绝基础可复用；SAF004专用推断与action/filter中央收口仍IN_PROGRESS，本审计不抢正在编辑文件。

代码/规范：[apps/api/internal/agentruntime/context_access.go](<../../apps/api/internal/agentruntime/context_access.go>)；[apps/api/internal/agentruntime/policy.go](<../../apps/api/internal/agentruntime/policy.go>)；[apps/api/internal/httpapi/agent_intent_router.go](<../../apps/api/internal/httpapi/agent_intent_router.go>)。

对应测试：[apps/api/internal/agentruntime/context_access_test.go](<../../apps/api/internal/agentruntime/context_access_test.go>)；[apps/api/internal/httpapi/new_people_integration_test.go](<../../apps/api/internal/httpapi/new_people_integration_test.go>)；[apps/api/internal/httpapi/relationship_context_integration_test.go](<../../apps/api/internal/httpapi/relationship_context_integration_test.go>)。

保存证据：[docs/testing/evidence/agent-permission-contract-2026-10-02/README.md](<../../docs/testing/evidence/agent-permission-contract-2026-10-02/README.md>)。

### observation

requestId、最少请求日志和业务audit/analytics；无Memory事件/模型费用/run trace/保留policy。

代码/规范：[apps/api/internal/httpapi/request_trace.go](<../../apps/api/internal/httpapi/request_trace.go>)；[apps/api/internal/postgres/admin_audit.go](<../../apps/api/internal/postgres/admin_audit.go>)；[apps/api/internal/postgres/activity_analytics.go](<../../apps/api/internal/postgres/activity_analytics.go>)。

对应测试：[apps/api/internal/httpapi/request_trace_test.go](<../../apps/api/internal/httpapi/request_trace_test.go>)；[apps/api/internal/analytics/model_test.go](<../../apps/api/internal/analytics/model_test.go>)。

保存证据：[docs/reports/FUNCTIONAL-MVP-COMPLETION-REPORT.md](<../../docs/reports/FUNCTIONAL-MVP-COMPLETION-REPORT.md>)。

### ui

已有中文设置/任务/有限真实审阅、device screenshots；无Memory中心、AIR运行恢复或独立目标用户/读屏验收。

代码/规范：[apps/client/lib/src/workspace/settings_page.dart](<../../apps/client/lib/src/workspace/settings_page.dart>)；[apps/client/lib/src/workspace/agent_relationship_page.dart](<../../apps/client/lib/src/workspace/agent_relationship_page.dart>)；[apps/client/lib/src/workspace/new_people_page.dart](<../../apps/client/lib/src/workspace/new_people_page.dart>)；[apps/client/lib/src/workspace/opportunity_page.dart](<../../apps/client/lib/src/workspace/opportunity_page.dart>)；[apps/client/lib/src/workspace/agent_result_sheet.dart](<../../apps/client/lib/src/workspace/agent_result_sheet.dart>)；[apps/client/lib/src/workspace/map_workspace.dart](<../../apps/client/lib/src/workspace/map_workspace.dart>)。

对应测试：[apps/client/test/settings_page_test.dart](<../../apps/client/test/settings_page_test.dart>)；[apps/client/test/agent_relationship_page_test.dart](<../../apps/client/test/agent_relationship_page_test.dart>)；[apps/client/test/new_people_page_test.dart](<../../apps/client/test/new_people_page_test.dart>)；[apps/client/test/opportunity_entry_test.dart](<../../apps/client/test/opportunity_entry_test.dart>)；[apps/client/test/map_workspace_shell_test.dart](<../../apps/client/test/map_workspace_shell_test.dart>)。

保存证据：[docs/testing/evidence/opportunity-reasons-2026-10-02/README.md](<../../docs/testing/evidence/opportunity-reasons-2026-10-02/README.md>)。

### regression

先前真实已保存的052/3seed全Go默认包并发766 PASS事件×3与Flutter189；本材料审计不重跑会写状态测试。

代码/规范：无对应已实现记录。

对应测试：[automation/verify_place_context_migration.ps1](<../../automation/verify_place_context_migration.ps1>)；[docs/testing/evidence/agent-permission-contract-2026-10-02/reproduce.ps1](<../../docs/testing/evidence/agent-permission-contract-2026-10-02/reproduce.ps1>)。

保存证据：[docs/testing/evidence/agent-permission-contract-2026-10-02/README.md](<../../docs/testing/evidence/agent-permission-contract-2026-10-02/README.md>)；[docs/testing/evidence/agent-permission-contract-2026-10-02/parallel-round-1.jsonl](<../../docs/testing/evidence/agent-permission-contract-2026-10-02/parallel-round-1.jsonl>)；[docs/testing/evidence/agent-permission-contract-2026-10-02/parallel-round-2.jsonl](<../../docs/testing/evidence/agent-permission-contract-2026-10-02/parallel-round-2.jsonl>)；[docs/testing/evidence/agent-permission-contract-2026-10-02/parallel-round-3.jsonl](<../../docs/testing/evidence/agent-permission-contract-2026-10-02/parallel-round-3.jsonl>)；[docs/testing/evidence/opportunity-reasons-2026-10-02/flutter-test.log](<../../docs/testing/evidence/opportunity-reasons-2026-10-02/flutter-test.log>)。

### migration

001–052现schema旧ID/受保护down/reapply证据；V5新增schema未实施，不能据此称已部署。

代码/规范：[apps/api/migrations/033_context_graph.sql](<../../apps/api/migrations/033_context_graph.sql>)；[apps/api/migrations/041_business_principals.sql](<../../apps/api/migrations/041_business_principals.sql>)；[apps/api/migrations/043_moment_context_links.sql](<../../apps/api/migrations/043_moment_context_links.sql>)；[apps/api/migrations/052_new_people_consent.sql](<../../apps/api/migrations/052_new_people_consent.sql>)。

对应测试：[automation/verify_place_context_migration.ps1](<../../automation/verify_place_context_migration.ps1>)。

保存证据：[docs/testing/evidence/agent-permission-contract-2026-10-02/migration052.log](<../../docs/testing/evidence/agent-permission-contract-2026-10-02/migration052.log>)。

### queue

现144项真实队列/连续next协议；本次不导入V5、不领任务。

代码/规范：[automation/taskctl.py](<../../automation/taskctl.py>)；[automation/codex_task_queue.json](<../../automation/codex_task_queue.json>)；[automation/CODEX_V4_EXECUTION_PROTOCOL.md](<../../automation/CODEX_V4_EXECUTION_PROTOCOL.md>)。

对应测试：无对应已实现记录。

保存证据：[docs/research/BIRDTIE-V4-AGA-001-AUDIT.md](<../../docs/research/BIRDTIE-V4-AGA-001-AUDIT.md>)；[docs/research/BIRDTIE-V5-MATERIAL-RECONCILIATION-2026-10-02.md](<../../docs/research/BIRDTIE-V5-MATERIAL-RECONCILIATION-2026-10-02.md>)。

### documents

既有领域canonical和AGE原文；材料规划正文并非新功能/认知ADR验收。

代码/规范：[docs/product/BIRDTIE-CANONICAL-PRODUCT-SPEC-V4.md](<../../docs/product/BIRDTIE-CANONICAL-PRODUCT-SPEC-V4.md>)；[docs/architecture/AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md](<../../docs/architecture/AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md>)；[docs/architecture/AGENT-CONTEXT-ACCESS-POLICY.md](<../../docs/architecture/AGENT-CONTEXT-ACCESS-POLICY.md>)；[docs/architecture/AGENT-TO-AGENT-PERMISSION-CONTRACT-V4.md](<../../docs/architecture/AGENT-TO-AGENT-PERMISSION-CONTRACT-V4.md>)；[docs/product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md](<../../docs/product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md>)。

对应测试：无对应已实现记录。

保存证据：[docs/research/BIRDTIE-V5-MATERIAL-RECONCILIATION-2026-10-02.md](<../../docs/research/BIRDTIE-V5-MATERIAL-RECONCILIATION-2026-10-02.md>)。

### gates

生产IdP/HTTPS、真实组织/活动/地图API支持、提醒部署与值守等缺实际证据；Closed Pilot NO。

代码/规范：[docs/product/RELEASE-QUALITY-GATES.md](<../../docs/product/RELEASE-QUALITY-GATES.md>)；[docs/business/CLOSED-PILOT-OPERATIONS-RUNBOOK.md](<../../docs/business/CLOSED-PILOT-OPERATIONS-RUNBOOK.md>)。

对应测试：无对应已实现记录。

保存证据：[docs/research/2026-10-01-closed-pilot-release-gate.md](<../../docs/research/2026-10-01-closed-pilot-release-gate.md>)；[docs/testing/evidence/agent-permission-contract-2026-10-02/README.md](<../../docs/testing/evidence/agent-permission-contract-2026-10-02/README.md>)。

## 4 AGE 81项逐条矩阵

| 源ID / 标题 | 状态 / 处置 | 现任务锚点 | 证据索引 | 真实差异 |
| --- | --- | --- | --- | --- |
| AGE-001 Agent Profile 基础模型 | PARTIAL / EXTEND | BT-V4-ACT-001,BT-V4-AGF-002 | identity,profile | 稳定 Agent 与主体已存在；缺统一且版本化 AgentProfile、Private Profile 与 Business Profile 服务。 |
| AGE-002 Public Profile 与 Private Agent Profile 分离 | PARTIAL / EXTEND | BT-V4-PRV-001,BT-V4-CTX-002,BT-V4-SOC-001 | profile,context,relationship | 已有 Public Profile、私密 Context 和本人关系工具；没有 Private Agent Profile 真源。 |
| AGE-003 Profile Field Visibility | PARTIAL / EXTEND | BT-V4-PRV-001,BT-V4-SAF-002 | profile,socialcontext | 资料级 public/private 与披露开关可复用；没有逐字段 PUBLIC/CONNECTIONS/COMMUNITY/PRIVATE/AGENT_ONLY。 |
| AGE-004 Agent Memory | NOT_IMPLEMENTED / NEW | BT-V4-AGF-001 | memory | 没有 AgentMemory schema、领域模型、受控写服务或 API；AgentTask 不是 Memory。 |
| AGE-005 Memory Evidence | NOT_IMPLEMENTED / NEW | BT-V4-MOM-001 | memory,moment | Moment 实体引用不是 MemoryEvidence；缺 memory_id/source/version/weight/observed_at。 |
| AGE-006 Explicit Memory 与 Inferred Memory | NOT_IMPLEMENTED / NEW | BT-V4-SAF-004 | memory | 没有 Explicit/Inferred Memory 持久分离；显式查询条件不可冒称已确认长期兴趣。 |
| AGE-007 Memory Candidate | NOT_IMPLEMENTED / NEW | BT-V4-SAF-004 | memory | 没有候选提升/拒绝/过期/替代生命周期或权威候选存储。 |
| AGE-008 Confidence Model | NOT_IMPLEMENTED / NEW | BT-V4-SAF-004 | memory | 没有 Memory confidence；当前安全合同正在实现，不能代替语义和校准模型。 |
| AGE-009 Memory Reinforcement | NOT_IMPLEMENTED / NEW | BT-V4-MOM-002 | memory | 没有强化、来源簇去重或跨证据更新。 |
| AGE-010 Memory Decay | NOT_IMPLEMENTED / NEW | 无现成同项 | memory | 没有衰减；显式陈述不得沿用推断衰减。 |
| AGE-011 Memory Correction | NOT_IMPLEMENTED / NEW | 无现成同项 | memory | 没有 Memory 纠正/拒绝/删除与阻止同源复活服务。 |
| AGE-012 Memory Provenance | NOT_IMPLEMENTED / NEW | BT-V4-OPP-004 | memory,opportunity | 已有推荐规则理由；没有 Memory 的证据链解释，不能用原因代码替代 Memory provenance。 |
| AGE-013 Sensitive Attribute Protection | PARTIAL / EXTEND | BT-V4-SAF-004 | safety | 当前确定性工具未推断敏感画像；专用敏感推断策略正在实现，尚无已验收学习管线。 |
| AGE-014 Weak Evidence Protection | PARTIAL / EXTEND | BT-V4-SAF-004,BT-V4-SOC-001 | safety,relationship | 关系频率不作亲密推断；缺未来 Memory 弱证据提升门槛，SAF004 未完成。 |
| AGE-015 Agent Seed Onboarding | PARTIAL / EXTEND | BT-AUT-002,BT-V4-CTX-002 | profile,context,ui | 现有显示名/城市选择可复用；缺渐进 Agent Seed，生产登录外部门禁仍未齐。 |
| AGE-016 What Brings You Here | NOT_IMPLEMENTED / NEW | 无现成同项 | ui,intent | 没有 What Brings You Here 私密 onboarding intent 采集。 |
| AGE-017 Interest Seed | NOT_IMPLEMENTED / NEW | 无现成同项 | ui,profile | 没有可跳过的初始兴趣 seed；搜索关键词不是种子 Profile。 |
| AGE-018 Social Preference Seed | NOT_IMPLEMENTED / NEW | 无现成同项 | ui,profile | 没有私密 Social Preference Seed。 |
| AGE-019 Progressive Completion | NOT_IMPLEMENTED / NEW | 无现成同项 | ui,profile | 没有渐进补齐机制或版本化 Profile 编辑流程。 |
| AGE-020 Moment Enrichment Pipeline | NOT_IMPLEMENTED / NEW | BT-V4-MOM-001 | moment,memory | 原 Moment 写链存在；没有 Signal Extractor→Evidence→MemoryCandidate 管线。 |
| AGE-021 Moment Place Context | PARTIAL / EXTEND | BT-V4-MOM-001 | moment,places | 明确 Moment→Place/City 引用已存；没有 evidence 管线，关联不证明本人到访。 |
| AGE-022 Moment Activity Context | PARTIAL / EXTEND | BT-V4-MOM-001 | moment,activity | 明确 Moment→Activity 链及授权存在；没有经历 Evidence，报名/关联不等于真实到场。 |
| AGE-023 Historical Moment | PARTIAL / EXTEND | BT-V4-MOM-001 | moment | OccurredAt 与 CreatedAt 已分离；当前客户端草稿写 timePrecision=unknown，缺完整历史时间编辑验收。 |
| AGE-024 Activity Participation Signal | PARTIAL / EXTEND | BT-RSV-001 | activity | Participation 持久、改期/取消可复用；没有独立 participation evidence 生产与候选消费。 |
| AGE-025 Repeated Activity Preference | NOT_IMPLEMENTED / NEW | 无现成同项 | memory,activity | 没有重复活动证据聚合或兴趣强化。 |
| AGE-026 Activity Social Context | PARTIAL / EXTEND | BT-V4-SOC-001,BT-V4-ACTN-003 | relationship,socialcontext | 已授权共同 going 活动可读；缺真实 attended/shared-history 模型，不能推断亲密。 |
| AGE-027 Place Memory | PARTIAL / EXTEND | BT-V4-MOM-002 | places,moment,activity | Save/Place/Moment/Activity 链存在；没有统一 Place Memory、访问/喜欢事实服务。 |
| AGE-028 City History | PARTIAL / EXTEND | BT-V4-CTX-002 | context | current/past/home/destination 私密声明可复用；未覆盖 visited/lived 证据与历史时间，不从声明证明居住。 |
| AGE-029 Life Map | NOT_IMPLEMENTED / NEW | 无现成同项 | context,ui | 没有 Life Map 聚合或 UI。 |
| AGE-030 Life Import | NOT_IMPLEMENTED / NEW | 无现成同项 | memory,ui | 没有 Life Import；P2 不作 P0 前置。 |
| AGE-031 Community Membership Signal | PARTIAL / EXTEND | BT-V4-COMM-001,BT-V4-OPP-001 | community,opportunity | 真实 Community membership 当前参与匹配；未接 AGE Evidence/ContextBuilder，成员不代表兴趣。 |
| AGE-032 Organization Membership | PARTIAL / EXTEND | BT-ORG-004,BT-V4-ORG-002 | organization | 成员 owner/admin/moderator/member 与撤销已有；未装配为 AGE 的相关 Context。 |
| AGE-033 Context Builder | PARTIAL / EXTEND | BT-V4-CTX-001,BT-V4-SOC-001 | context,runtime,relationship | 已有目的限定工具、有限当前查询；缺统一最小 AgentContextBuilder 和 Memory/Policy 装配。 |
| AGE-034 Context Relevance | PARTIAL / EXTEND | BT-V4-SOC-001,BT-V4-OPP-001 | runtime,relationship,opportunity | 现有请求限定检索与截断可复用；没有跨 Profile/Memory 的 relevance retriever。 |
| AGE-035 Context Budget | PARTIAL / EXTEND | BT-V4-SOC-001 | relationship,opportunity | 现有 limit/truncated 存在；缺 token、confidence、recency 和全 run 预算。 |
| AGE-036 Current Context | PARTIAL / EXTEND | BT-V4-CTX-002,BT-V4-INT-004 | context,intent,runtime | 声明/请求/task 有独立语境与 expiry；未有长期 Memory 对照和统一 Builder。 |
| AGE-037 Attention Policy Model | NOT_IMPLEMENTED / NEW | 无现成同项 | notification | 没有 AttentionPolicy 五级路由模型。 |
| AGE-038 Notification Routing | PARTIAL / EXTEND | BT-NTF-001,BT-NTF-002 | notification | 确定性活动/消息通知与 worker 真实存在；缺 AttentionPolicy 插层和语义通知。 |
| AGE-039 Notification Categories | PARTIAL / EXTEND | BT-INB-001,BT-V4-NOT-001 | notification | Inbox 有类别及当前业务类型；未实现完整 MESSAGE/ACTIVITY/COMMUNITY/ORGANIZATION/BUSINESS/SYSTEM/AGENT/SOCIAL 策略。 |
| AGE-040 Digest | NOT_IMPLEMENTED / NEW | 无现成同项 | notification | 没有用户选择 Digest、静默时间和聚合投递。 |
| AGE-041 Social Policy | PARTIAL / EXTEND | BT-V4-PRV-002,BT-V4-OPP-003,BT-V4-SAF-004 | socialcontext,newpeople,safety | 当前 Block/好友同意/双侧匹配授权可复用；没有统一 SocialInteractionPolicy。 |
| AGE-042 Message Request Policy | PARTIAL / EXTEND | BT-V4-PRV-002 | relationship,newpeople | 好友申请+接受后聊天可复用；没有 Agent SCREEN 或统一四态 message policy。 |
| AGE-043 Introduction Policy | PARTIAL / EXTEND | BT-V4-OPP-003,BT-V4-OPP-004 | newpeople,opportunity | 明确意图+双侧同意找伙伴已验；共享兴趣/City/Community 的更广引荐政策未实现，不能读取私密偏好。 |
| AGE-044 Autonomy Level | PARTIAL / EXTEND | BT-V4-SAF-004 | safety,runtime | 当前仅确定性查询与审阅导航，无自治工具；没有已验收四级 autonomy 配置/API。 |
| AGE-045 Outbound Action Safety | PARTIAL / EXTEND | BT-V4-SAF-004,BT-V4-INT-005 | safety,newpeople,intent | 公开意图/邀请独立确认已实测；中央 action 收口在进行，缺 AIR 批准消费/效果账本，真实自治仍关闭。 |
| AGE-046 Agent Profile Page | PARTIAL / EXTEND | BT-V4-SOC-001 | ui,relationship | 设置/关系信号/意图/机会入口可复用；没有 What Birdie knows 的统一 My Agent 页面。 |
| AGE-047 Memory Center | NOT_IMPLEMENTED / NEW | 无现成同项 | ui,memory | 没有 Memory Center 或编辑纠正删除界面。 |
| AGE-048 Why This? | PARTIAL / EXTEND | BT-V4-OPP-004 | opportunity,ui | 中文规则理由与真实当前实体已验；没有 Memory 证据解释，不能把 going 当 attended。 |
| AGE-049 Agent Privacy Controls | PARTIAL / EXTEND | BT-V4-SOC-001,BT-V4-OPP-003 | ui,profile,relationship,newpeople | 已有资料与048/051/052独立开关；缺 Agent Private Profile/Memory/Personalization/Learning 控制。 |
| AGE-050 Organization Agent Profile | PARTIAL / EXTEND | BT-V4-ORG-001,BT-ORG-003 | identity,organization,runtime | 组织 Profile/成员/资料与共享 Runtime 有基础；完整 Organization Agent Profile pack 未实现。 |
| AGE-051 Organization Memory | NOT_IMPLEMENTED / NEW | 无现成同项 | memory,organization | FAQ/活动事实服务可复用，但不是 Organization Memory。 |
| AGE-052 Organization Memory Boundary | PARTIAL / EXTEND | BT-V4-PRV-001,BT-V4-AGA-001 | safety,coordination,relationship | 组织访问本人关系上下文被拒；尚无成员 Private Profile/Memory，因此仅边界基础通过，不宣称完整 Memory 测试。 |
| AGE-053 Organization Knowledge Source | PARTIAL / EXTEND | BT-OAG-001,BT-ORG-003 | organization | 管理员 FAQ/活动/公开资料事实可复用；缺版本化 Organization MemoryEvidence。 |
| AGE-054 Business Agent Profile | PARTIAL / EXTEND | BT-V4-BIZ-001,BT-V4-BIZ-002 | business,identity | Business principal/membership/地点关系已有；Business Runtime unavailable，Profile pack 和管理入口未成。 |
| AGE-055 Business ≠ Venue | REAL / REUSE | BT-V4-BIZ-001,BT-V4-PLC-004 | business | 041 business/venue 独立主体和审核关系、typed organizer及撤销已真实隔离测试；不含 Agent/预约。 |
| AGE-056 Business Memory | NOT_IMPLEMENTED / NEW | 无现成同项 | business,memory | 没有 Business Memory；营业/菜单/报价/预订等知识不可编造。 |
| AGE-057 Business Privacy Boundary | PARTIAL / EXTEND | BT-V4-PRV-001,BT-V4-BIZ-002 | safety,business | Business Runtime 默认 unavailable + PRIVATE 默认拒绝可复用；无 live Business Agent/私人 Memory 读取能力。 |
| AGE-058 Community Agent Decision | REAL / REUSE | BT-V4-AGF-001,BT-V4-COMM-001 | identity,community | actorref Community不是Principal，ForType不给能力、无Community Agent自动创建；当前决策已有测试。 |
| AGE-059 Future Extension Point | PARTIAL / EXTEND | BT-V4-ADR-001 | identity | typed ActorRef 可扩展但 Community Principal 被拒；未新增未来 Agent ADR，本 Epic 不实施 Community Agent。 |
| AGE-060 Memory Isolation | PARTIAL / EXTEND | BT-V4-PRV-001,BT-V4-AGF-002 | safety,runtime | 本人 task/context/关系读取隔离有实测；没有 Memory 存储与跨主体检索测试。 |
| AGE-061 Cross-Agent Sharing | PARTIAL / EXTEND | BT-V4-AGA-001 | coordination | strict bilateral task/resource/purpose/grant policy已验；无live grant、传输或共享持久化，AGA002未完成。 |
| AGE-062 Purpose Limitation | PARTIAL / EXTEND | BT-V4-AGA-001,BT-V4-SAF-004 | coordination,safety | AGA最小ASK_USER、不含私人信息或写工具；无跨Agent Memory保留/删除链，SAF004未验收。 |
| AGE-063 Delete Cascade | NOT_IMPLEMENTED / NEW | 无现成同项 | moment,memory | Moment withdraw/revision可复用；没有 Memory evidence→candidate/index/cache失效传播。 |
| AGE-064 Agent Enrichment Events | PARTIAL / EXTEND | BT-V4-OBS-001 | notification,moment,activity,observation | 业务audit/通知事件存在；没有统一版本化 enrichment event/outbox 目录。 |
| AGE-065 Async Enrichment | NOT_IMPLEMENTED / NEW | 无现成同项 | runtime,memory | 没有异步 enrichment worker/持久 run/恢复；不得阻塞原Moment写链。 |
| AGE-066 Feature Flag | NOT_IMPLEMENTED / NEW | 无现成同项 | runtime | 没有 agent_enrichment/memory/attention/social_policy/life_map 分功能开关。 |
| AGE-067 Pilot Defaults | PARTIAL / EXTEND | BT-V4-SAF-004,BT-V4-PIL-003 | safety,runtime | 当前自治与敏感推断无执行入口；没有 Memory basic 的Pilot设置或分功能kill switch，Pilot NO。 |
| AGE-068 Profile APIs | PARTIAL / EXTEND | BT-V4-PRV-002,BT-V4-AGF-002 | profile | 已有本人 Public Profile编辑 API；没有 AgentProfile/PrivateAgentProfile API。 |
| AGE-069 Memory APIs | NOT_IMPLEMENTED / NEW | 无现成同项 | memory | 没有列举/详情/更新/删除/拒绝 Memory API。 |
| AGE-070 Policy APIs | PARTIAL / EXTEND | BT-V4-SOC-001,BT-V4-OPP-003 | relationship,newpeople,ui | 独立 consent API可复用；缺 Attention/Social/Autonomy 统一policy API。 |
| AGE-071 Unit Tests | PARTIAL / EXTEND | BT-V4-SAF-004 | safety,regression | 现context/coordination纯策略真实测试存在；Memory强化/置信/提升/纠正测试未实现，SAF004仍进行。 |
| AGE-072 Privacy Tests | PARTIAL / EXTEND | BT-V4-PRV-001,BT-V4-AGA-001 | safety,coordination,regression | 当前PRIVATE/角色/owner隔离有纯policy与真实SQL/HTTP证据；未来Memory模型尚无对应测试。 |
| AGE-073 Sensitive Inference Tests | PARTIAL / EXTEND | BT-V4-SAF-004 | safety | 当前无敏感推断器；专用推断拒绝合同与测试正在实现，不能标完整学习链通过。 |
| AGE-074 Regression Tests | PARTIAL / EXTEND | BT-V4-TST-001,BT-V4-E2E-003 | regression | 052默认并发三轮Go、189 Flutter和手机旧链真实已验；AGE增量尚未实施，需每项继续回归。 |
| AGE-075 Migration Tests | PARTIAL / EXTEND | BT-V4-MIG-001 | migration,regression | 001–052旧ID/up/down/reapply已验；AGE增量schema尚无，禁止拿现基线代替新迁移。 |
| AGE-076 Enrichment Metrics | NOT_IMPLEMENTED / NEW | 无现成同项 | observation | 没有 Memory created/rejected/corrected/deleted/promoted 等指标。 |
| AGE-077 Quality Metrics | NOT_IMPLEMENTED / NEW | 无现成同项 | observation | 没有 Memory正确性/推荐相关度/拒绝率基线或评测集。 |
| AGE-078 Cognitive Architecture ADR | PARTIAL / EXTEND | BT-V4-ADR-001 | documents | 现身份/Context/permission ADR可引用；缺 Cognitive Architecture ADR 的AGE/AIR分工与审批持久性。 |
| AGE-079 Memory Architecture | NOT_IMPLEMENTED / NEW | 无现成同项 | documents,memory | 没有 Memory Architecture 权威正文；本矩阵不是该设计完成。 |
| AGE-080 Personal Agent Product Spec | PARTIAL / EXTEND | BT-V4-CAN-001 | documents | AGE Epic原文存在；未完成统一Personal Agent产品设计及实际能力合同。 |
| AGE-081 Organization / Business Agent Spec | PARTIAL / EXTEND | BT-V4-ORG-001,BT-V4-BIZ-002 | documents,business,organization | 现组织/Business主体规范可复用；缺完成的Agent知识/pack/权威Memory规范。 |

## 5 AIR 56项逐条矩阵

| 源ID / 标题 | 状态 / 处置 | 现任务锚点 | 证据索引 | 真实差异 |
| --- | --- | --- | --- | --- |
| BT-V5-AIR-001 读取安全检查点和真实开发快照 | PARTIAL / REUSE | BT-V4-AUD-001,BT-V4-AUD-002,BT-V4-AUT-001 | queue | 只读真实快照已有，本次SAF004在写；导入必须等root记录本项安全checkpoint，不能抢当前文件。 |
| BT-V5-AIR-002 对账 V4 AGE AIR 并复用已有实现 | REAL / REUSE | BT-V4-AGF-001,BT-V4-AGF-002,BT-V4-AUD-001,BT-V4-AUD-002,BT-V4-CAN-001,BT-V4-COMM-001 | queue,documents,regression | 本次137项逐条对账及机器矩阵为审计交付，非137项功能完成或队列DONE。 |
| BT-V5-AIR-003 固定 AIR 与 AGE 的领域边界 | PARTIAL / EXTEND | BT-V4-ACT-001,BT-V4-ADR-001,BT-V4-AGF-001,BT-V4-AGF-002,BT-V4-PRV-001,BT-V4-SAF-004 | identity,safety,documents | 共享主体/角色/领域边界已存在；缺AGE/AIR认知ADR、真正权威Memory接口和live adapters。 |
| BT-V5-AIR-004 追加需求队列并按依赖调度 | NOT_IMPLEMENTED / EXTEND | BT-V4-SAF-001,BT-V4-SAF-004,BT-V4-OPP-001,BT-V4-MOM-001 | queue | taskctl支持现队列；本子任务不导入，缺AGE/AIR去重dry-run、跨包拓扑及原任务状态保留实证。 |
| BT-V5-AIR-005 安全迁移和旧客户端兼容 | PARTIAL / EXTEND | BT-V4-MIG-001 | migration,regression | 既有兼容001–052迁移规范和旧ID证据可复用；AIR最小持久schema尚未实施，P0不能等待本P1。 |
| BT-V5-AIR-006 核验已有产品流程基线 | PARTIAL / REUSE | BT-V4-PIL-001,BT-V4-PIL-002,BT-V4-PIL-003,BT-V4-TST-001 | regression,activity,notification | 已存在发布→检索→详情→RSVP→Plans与点位/提醒本地证据；V5变更需最新回归，外部门禁仍NO。 |
| BT-V5-AIR-007 统一 Model Gateway 契约 | NOT_IMPLEMENTED / NEW | BT-V4-AGF-001,BT-V4-AGF-002 | runtime | 没有ModelGateway/规范ModelRequest/ModelResult/provider adapter；role policy不是推理网关。 |
| BT-V5-AIR-008 能力登记与路由资格验证 | NOT_IMPLEMENTED / NEW | 审计后绑定 | runtime | 没有provider-model能力登记/验证时间/地域与授权路由；不要把角色capability等同模型能力。 |
| BT-V5-AIR-009 接入一个已批准的真实推理适配器 | BLOCKED_EXTERNAL / NEW | BT-V4-AGF-001 | runtime | 没有适配器；真实调用缺provider批准/凭证/费用上限/地域保留决定。可独立先做fake契约，live单列。 |
| BT-V5-AIR-010 统一超时重试与隐私安全降级 | NOT_IMPLEMENTED / NEW | 审计后绑定 | runtime | 没有ProviderError、模型Retry-After/全run预算/授权fallback；HTTP错误处理不等于模型故障策略。 |
| BT-V5-AIR-011 预算与请求前数据出口检查 | PARTIAL / EXTEND | BT-V4-PRV-001,BT-V4-SAF-004 | safety | 现主体/资源/目的准入基础可复用；没有模型出网许可、费用预留/结算与并发预算；SAF004仍进行。 |
| BT-V5-AIR-012 增加第二 provider 和规范历史适配 | BLOCKED_EXTERNAL / NEW | BT-V4-AGF-002 | runtime | 第二provider尚未实现且缺批准/凭证/费用配置；P1不能阻断单provider P0。 |
| BT-V5-AIR-013 依据评测优化模型路由和成本 | NOT_IMPLEMENTED / NEW | 审计后绑定 | observation | 没有任务质量/成本评测或多provider优化；不为用户训练模型。 |
| BT-V5-AIR-014 事件信封与类型注册 | PARTIAL / EXTEND | BT-V4-AGF-001 | observation,notification | 已有业务audit/requestId/通知生产；无统一EventEnvelope、sourceVersion、逻辑操作ID与schema目录。 |
| BT-V5-AIR-015 事务 outbox 与幂等消费 | PARTIAL / EXTEND | 审计后绑定 | notification | 数据库幂等提醒/事务通知可复用原则；无enrichment outbox/inbox、handlerVersion与稳定effectKey。 |
| BT-V5-AIR-016 持久化有界 AgentRun 状态机 | PARTIAL / EXTEND | 审计后绑定 | runtime | AgentTask同步持久历史可复用；缺AgentRun/RunStep/lease fencing/checkpoint/dispatch-effect账本。 |
| BT-V5-AIR-017 重试死信和人工恢复入口 | NOT_IMPLEMENTED / NEW | 审计后绑定 | runtime | 无run死信/受控重放/原来源授权复核；worker非零退出不是DLQ。 |
| BT-V5-AIR-018 撤权删除取消和过期传播 | PARTIAL / EXTEND | BT-V4-PRV-001,BT-V4-SAF-004 | newpeople,relationship,intent | 当前开关/Block/Cancel锁与重验真实通过；无AIR源epoch/tombstone、dispatch承诺/删除链，不能只将检查布尔值设true。 |
| BT-V5-AIR-019 计划与上下文触发器 | PARTIAL / EXTEND | BT-INB-001,BT-NTF-001,BT-V4-NOT-001,BT-V4-PIL-003 | notification | 独立活动提醒与Inbox可靠路径可复用；缺用户digest/静默/DST计划及AI触达预算。 |
| BT-V5-AIR-020 并发顺序和事件风暴控制 | PARTIAL / EXTEND | 审计后绑定 | newpeople,runtime | 已有交易并发/迟到响应保护可复用；无run主体队列、公平性、根预算/causation最大链深度。 |
| BT-V5-AIR-021 大型历史导入批处理 | NOT_IMPLEMENTED / NEW | 审计后绑定 | memory | 无历史导入/批次/断点/费用预估；P2不作交互P0前置。 |
| BT-V5-AIR-022 读取 AGE 权威数据与最小上下文 | PARTIAL / EXTEND | BT-V4-AGF-001,BT-V4-CTX-001,BT-V4-CTX-002,BT-V4-PRV-001 | context,runtime,relationship | 已有最小授权Context/明确query/有限关系读取；AGE Profile/Memory接口未成，需单一权威adapter。 |
| BT-V5-AIR-023 主体权限过滤与上下文快照 | PARTIAL / EXTEND | BT-V4-AGF-002,BT-V4-PRV-001,BT-V4-SAF-004 | safety,coordination | 当前session/owner/角色/Block/resource SQL可复用；无ContextBundle版本快照、授权epoch与模型出口绑定。 |
| BT-V5-AIR-024 来源证据冲突与事实时间模型 | PARTIAL / EXTEND | BT-V4-SAF-004 | context,moment,places | 已有OccurredAt/CreatedAt、明确声明/来源freshness；缺Memory EvidenceSet冲突模型与confidence语义，metadata不证明本人亲历。 |
| BT-V5-AIR-025 多城市线上线下语境和社会关系 | PARTIAL / EXTEND | BT-V4-ACTN-003,BT-V4-AGA-001,BT-V4-AGA-002,BT-V4-INT-003,BT-V4-INT-004,BT-V4-SOC-001 | context,intent,newpeople | 033多Context/线上找伙伴明确约束已有；当前活动查询仍City UI为主，NOW005/E2E004与广泛推荐未完成。 |
| BT-V5-AIR-026 可评测的检索和排序升级 | NOT_IMPLEMENTED / EXTEND | 审计后绑定 | opportunity,places | 现数据库规则基线可复用；没有检索对照评测、受控embedding/reranker/撤销索引。 |
| BT-V5-AIR-027 Prompt 与 schema 版本登记 | NOT_IMPLEMENTED / NEW | BT-V4-AGF-001 | runtime | 现规则version不是prompt/schema/model配置registry；没有不可变prompt配置与历史run引用。 |
| BT-V5-AIR-028 结构化输出验证与注入隔离 | PARTIAL / EXTEND | BT-AGT-002,BT-V4-NOW-004,BT-V4-SAF-001,BT-V4-SAF-004 | safety,coordination,runtime | strict JSON/实体/输出action收口基础可复用；缺真实ModelResult拒答/截断/注入/最多一次修复预算，SAF004未验收。 |
| BT-V5-AIR-029 Prompt 变更评审和影子回放 | NOT_IMPLEMENTED / NEW | 审计后绑定 | runtime,observation | 无prompt评审/影子回放/版本对照，不能拿现单元fixtures当LLM评测。 |
| BT-V5-AIR-030 媒体授权与本地出口前过滤 | NOT_IMPLEMENTED / NEW | BT-V4-SAF-004 | moment | 现文字Moment没有外部分析；无媒体授权/解码上限/EXIF移除/出网捕获，上传许可不等于AI出口。 |
| BT-V5-AIR-031 结构化视觉观察接口 | BLOCKED_EXTERNAL / NEW | BT-V4-MOM-001 | moment | 没有视觉观察适配与数据集；live另缺VISION_PROVIDER_APPROVAL，文本P0不依赖。 |
| BT-V5-AIR-032 媒体地点和时间证据验证 | PARTIAL / EXTEND | BT-V4-MOM-002,BT-V4-PLC-002 | moment,places | 现已授权Place ID/OccurredAt可引用；无照片metadata/Observation验证与归属候选，不能将关联推断为到访。 |
| BT-V5-AIR-033 Moment 富集作业和用户反馈 | NOT_IMPLEMENTED / NEW | BT-V4-MOM-001 | moment,runtime,ui | 无异步视觉Moment富集作业、候选状态与恢复，原Moment写链继续可用。 |
| BT-V5-AIR-034 视频音频与多帧扩展 | BLOCKED_EXTERNAL / NEW | BT-V4-MOM-001 | moment | 无视频/音频/多帧管线，live缺MEDIA_PROVIDER_APPROVAL；P2不阻塞文本/图片。 |
| BT-V5-AIR-035 富集去重与事实归属 | NOT_IMPLEMENTED / NEW | 审计后绑定 | memory,moment | 没有sourceCluster或照片所有者/发布者/被摄者/Agent主体归属模型。 |
| BT-V5-AIR-036 受限 Planner 和类型化提案 | PARTIAL / EXTEND | BT-V4-ACTN-001,BT-V4-INT-005 | runtime,intent | 现明确Intent parser+只读检索+导航可复用；没有模型Planner、类型化ActionProposal或run硬上限。 |
| BT-V5-AIR-037 Tool Registry 和确定性许可判定 | PARTIAL / EXTEND | BT-V4-PRV-001,BT-V4-SAF-004,BT-V4-AGF-002 | safety,runtime | 角色capability+服务端领域授权已有；缺Tool Registry/schema/风险/幂等元数据及统一可信许可，SAF004仍进行。 |
| BT-V5-AIR-038 写工具幂等与结果对账 | PARTIAL / EXTEND | BT-V4-AGA-002 | newpeople,activity,notification | 现Invite/RSVP/提醒幂等可复用；没有批准绑定effectKey、外部写UNKNOWN_RECONCILE与恢复账本。 |
| BT-V5-AIR-039 活动人物地点检索适配器 | PARTIAL / EXTEND | BT-V4-OPP-001,BT-V4-OPP-002,BT-V4-OPP-003,BT-V4-OPP-004,BT-V4-PLC-001,BT-V4-PLC-003 | opportunity,places,newpeople | 真实活动/人物/地点检索已存在且授权重验；缺AIR只读工具registration adapter，P0先直接复用活动查询不等待全P1。 |
| BT-V5-AIR-040 用户确认协议和安全默认 | PARTIAL / EXTEND | BT-V4-SAF-004 | newpeople,intent,safety | 现人类确认公开/邀请和纯AGA grant有基础；无persistentApproval/one-time消费/dispatch/effect/UNKNOWN闭环，confirmed:true非模型权限。 |
| BT-V5-AIR-041 通知提案和节制触达 | PARTIAL / EXTEND | BT-V4-SAF-004,BT-V4-PIL-003 | notification | 确定性服务提醒继续可用；没有语义通知提案/频控/静默/类别policy，不能由AIR故障影响旧提醒。 |
| BT-V5-AIR-042 有界 Agent 间协作和角色包 | PARTIAL / EXTEND | BT-V4-AGA-001,BT-V4-AGA-002,BT-V4-AGF-002,BT-V4-BIZ-002,BT-V4-BIZ-006,BT-V4-ORG-001,BT-V4-ORG-002 | coordination,business,organization | AGA001合同-only已验，独立双侧授权与最小响应可复用；无transport/live grant/hop预算，服从Post-Pilot gate。 |
| BT-V5-AIR-043 文字证据到 MemoryCandidate | NOT_IMPLEMENTED / NEW | BT-V4-SAF-004 | moment,memory | 没有授权文字→typedMemoryCandidate→AGE write adapter；缺AGE_WRITE_CONTRACT；provider live缺配置但offline代码可先做。 |
| BT-V5-AIR-044 受控候选确认与删除闭环 | NOT_IMPLEMENTED / NEW | BT-V4-PRV-001,BT-V4-SAF-004 | memory,moment | 没有Memory候选确认/拒绝/纠正/删源/撤销传播及旧任务防复活闭环。 |
| BT-V5-AIR-045 兴趣证据聚合与可校准置信度 | NOT_IMPLEMENTED / NEW | 审计后绑定 | memory | 没有sourceCluster兴趣聚合/冲突/衰减/校准；数值样例不是概率。 |
| BT-V5-AIR-046 记忆使用授权与公开资料边界 | PARTIAL / EXTEND | BT-V4-PRV-001,BT-V4-SAF-004 | profile,safety,coordination | 当前公私资料/单用途同意边界可复用；无purpose-scopedMemoryView或独立公开字段确认，SAF004未完成。 |
| BT-V5-AIR-047 来源纠错与变更传播 | NOT_IMPLEMENTED / NEW | 审计后绑定 | memory,moment | 现Moment revision与withdraw不是Memory依赖失效图；缺源变更→检索缓存候选失效传播。 |
| BT-V5-AIR-048 最小离线评测与安全发布门槛 | NOT_IMPLEMENTED / NEW | 审计后绑定 | regression | 已有Go/Flutter安全回归不是AIR40条模型+候选+沙箱效果评测；未建立AIR-S01…14版本化验收集。 |
| BT-V5-AIR-049 运行追踪和成本告警 | PARTIAL / EXTEND | BT-RUN-002,BT-V4-ANA-001,BT-V4-OBS-001 | observation,notification | 现requestId/无payload日志/audit可复用；无run/event/prompt/model/usage成本trace与有限保留告警。 |
| BT-V5-AIR-050 Flutter 候选审阅和Agent控制 | PARTIAL / EXTEND | BT-PER-001 | ui,relationship,newpeople | 中文审阅/来源/关闭开关/迟到auth保护组件可复用；没有MemoryCandidate审阅或run停止/删除控制。 |
| BT-V5-AIR-051 计划审阅及执行结果界面 | PARTIAL / EXTEND | 审计后绑定 | ui,newpeople | 当前发布/邀请preview确认组件可复用；无versionedApproval、多动作逐步批准或UNKNOWN_RECONCILE页面。 |
| BT-V5-AIR-052 全链路故障与质量回归 | PARTIAL / EXTEND | BT-V4-E2E-001,BT-V4-E2E-002,BT-V4-E2E-003,BT-V4-E2E-004,BT-V4-TST-001 | regression | 现766 Go PASS事件三轮/189 Flutter是V4基线；没有AIR120用例/双provider/视觉/live写/A2A版本报告。 |
| BT-V5-AIR-053 默认关闭和内部受控发布 | PARTIAL / EXTEND | BT-V4-PIL-003,BT-V4-REL-001 | runtime,safety | 现Business/推断/自治没有开放入口；无AIR分出口/候选/视觉/写/A2A kill switch及授权灰度配置。 |
| BT-V5-AIR-054 交付 P0 文本闭环与真实证据 | BLOCKED_EXTERNAL / EXTEND | BT-V4-PIL-001,BT-V4-PIL-002,BT-V4-PIL-003 | regression,opportunity,memory | 只读规则活动链可复用；两条文本Memory/沙箱审批P0未实现，live canary缺provider配置；原Closed Pilot保持NO。 |
| BT-V5-AIR-055 外部配置和试点门禁复核 | BLOCKED_EXTERNAL / REUSE | BT-V4-PIL-001,BT-V4-PIL-002,BT-V4-PIL-003 | gates | 原生产IdP/HTTPS/CSSA活动/地图API/运营调度门禁仍缺；本轮另缺provider/费用/出口批准，不能用合成验收替代。 |
| BT-V5-AIR-056 专业模型和自托管研究门槛 | NOT_IMPLEMENTED / NEW | 审计后绑定 | observation | 没有业务收益/TCO/数据权利评测；不采购GPU、不训练模型；包中供应商当时声明未作当前账户核验。 |

## 6 AGE ↔ AIR交错依赖

| 顺序 | 能力段 | AGE归属 | AIR接入 | 限制 |
| --- | --- | --- | --- | --- |
| 0 | SAF004收尾checkpoint、审计/ADR/增量导入dry-run | AGE-078 | BT-V5-AIR-001,BT-V5-AIR-002,BT-V5-AIR-003,BT-V5-AIR-004 | 本子任务只对账，不导入，不抢当前代码 |
| 1 | AGE权威Profile/Memory/Evidence基础 ↔ AIR网关安全 | AGE-001,AGE-002,AGE-003,AGE-004,AGE-005,AGE-006,AGE-007,AGE-008,AGE-011,AGE-012,AGE-013,AGE-014,AGE-060,AGE-063,AGE-066 | BT-V5-AIR-007,BT-V5-AIR-008,BT-V5-AIR-011,BT-V5-AIR-027 | fake gateway/预算/出口代码可先做；live批准凭证单列；不重建identity |
| 2 | 共享事件/outbox、最小Context、持久AgentRun及撤权 | AGE-064,AGE-033,AGE-034,AGE-036 | BT-V5-AIR-014,BT-V5-AIR-015,BT-V5-AIR-016,BT-V5-AIR-018,BT-V5-AIR-022,BT-V5-AIR-023 | AIR022读最小AGE权威接口，不等完整learner；AGE064与AIR015共享事件真源 |
| 3 | 输出验证、有限Planner/Tool、沙箱原子审批/效果恢复 | AGE-041,AGE-044,AGE-045,AGE-067 | BT-V5-AIR-028,BT-V5-AIR-036,BT-V5-AIR-037,BT-V5-AIR-040 | 复用SAF004；仅沙箱写，必要账本/脱敏/撤权竞争属于P0不后置到AIR005/049 |
| 4 | 文字候选 ↔ AGE确认/纠正/删源 ↔ 客户端审阅 | AGE-020,AGE-005,AGE-007,AGE-011,AGE-012,AGE-063,AGE-046,AGE-049 | BT-V5-AIR-009,BT-V5-AIR-010,BT-V5-AIR-043,BT-V5-AIR-044,BT-V5-AIR-050 | AGE基础write contract先于AIR043；AGE020高层管线后消费AIR结果，不制造循环 |
| 5 | 复用现只读活动adapter、独立P0评测/开关/交付gate | AGE-034,AGE-071,AGE-072,AGE-073,AGE-074,AGE-075 | BT-V5-AIR-048,BT-V5-AIR-053,BT-V5-AIR-054 | 既有SearchActivities作最小adapter；全AIR039 P1不增加为P0依赖；offline/live分开 |
| 6 | 后续视觉/双provider/Attention/记忆升级/真实写/A2A | AGE-009,AGE-010,AGE-021,AGE-022,AGE-024,AGE-025,AGE-027,AGE-031,AGE-032,AGE-037,AGE-038,AGE-040,AGE-050,AGE-051,AGE-054,AGE-056,AGE-061,AGE-062 | BT-V5-AIR-012,BT-V5-AIR-019,BT-V5-AIR-024,BT-V5-AIR-030,BT-V5-AIR-031,BT-V5-AIR-038,BT-V5-AIR-041,BT-V5-AIR-042,BT-V5-AIR-045,BT-V5-AIR-047,BT-V5-AIR-052,BT-V5-AIR-055 | 依赖排序而非整AGE先或整AIR先；A2A/native booking服从V4 Post-Pilot |

AIR原dependencies保留于JSON；AGE原文只有阶段而无每项依赖数组，本表是建议能力序，不导入/重排。AGE-004/005/007基础write contract先于AIR-043；AGE-020高层行为管线随后消费AIR结果，避免将整体AGE020反做AIR043前置而成环。AIR022/023读AGE最小权威adapter，缺值unknown，不另造记忆真源。AGE064/AIR014/015共享单一事件边界。

P0最小schema/outbox/inbox/approval/effect/原子撤权/脱敏/恢复本期须有；完整迁移/观测虽P1，不能后置必要控制。活动只读adapter直接复用既有SearchActivities，不让整AIR039/第二provider/视觉/A2A阻断P0。A2A/native booking仍V4 Post-Pilot。

## 7 消费评估：附既有验收，不新建任务

完整五旅程/十五领域原文已读。下表是质量与放行核验，不创建ID、不另建backlog，不凭评估建议改变启用范围。已有证据只证明已测细分范围，不证明普通用户无需讲解。

| 原章节 / 旅程 | 当前结论 | 既有绑定 | 差距和最小验收 |
| --- | --- | --- | --- |
| 3.1 新用户发现活动并真正参加 | BLOCKED_EXTERNAL | BT-V4-E2E-002,BT-V4-PIL-001,BT-V4-PIL-002,BT-V4-PIL-003,BT-RSV-001,BT-PLN-001 | 本地API/Plans有基线；真实IdP/供给/生产真机旅程和出席证据缺。 |
| 3.2 组织者发布活动并处理临时变化 | BLOCKED_EXTERNAL | BT-V4-ORG-002,BT-ORG-004,BT-V4-E2E-003,BT-NTF-002,BT-V4-PIL-002 | 本地授权及变更/取消通知有测试；真实主办方、送达/值守和多设备演练待验。 |
| 3.3 活動后安全建立联系并分享内容 | PARTIAL | BT-V4-TIE-001,BT-V4-CHT-003,BT-V4-SAF-002,BT-V4-SAF-003,BT-V4-ACTN-002,BT-V4-ACTN-003,BT-V4-E2E-001 | 请求/Block/卡片实时权限可复用；出席不是going，完整缓存/深链/真实用户旅程未齐。 |
| 3.4 拒绝定位或AI仍完成普通协调 | PARTIAL | BT-V4-CTX-001,BT-V4-INT-002,BT-V4-NOW-005,BT-V4-NOW-006,BT-V4-E2E-004 | 普通业务不用LLM、线上Intent无伪City；非地图活动入口/无GPS无AI普通路径待贯通真机验收。 |
| 3.5 主动启用AI记忆并可撤回 | NOT_IMPLEMENTED | BT-V4-SAF-004,AGE-004,AGE-011,AGE-063,BT-V5-AIR-043,BT-V5-AIR-044,BT-V5-AIR-050 | Moment写链可复用；Memory/候选/模型出口/确认删源闭环未成，新朋友开关不等于AI同意。 |

| 原章节 / 领域 | 当前结论 | 既有绑定 | 需补证据 |
| --- | --- | --- | --- |
| 4.1 产品承诺、信息架构与导航 | PARTIAL | BT-V4-NOW-001,BT-V4-ACTN-001 | 现入口/局部真机截图可查；缺无提示消费者独立完成观察。 |
| 4.2 首次使用、登录与账户恢复 | BLOCKED_EXTERNAL | BT-AUT-002,BT-V4-PIL-001 | 生产IdP/HTTPS/正式构建证据缺；删除/恢复/深链需按拟发行范围核验。 |
| 4.3 真实供给、首次价值与空状态 | BLOCKED_EXTERNAL | BT-V4-PIL-002,BT-V4-OPP-001 | 演练活动不是真实CSSA供给；负责人/有效时段/资格与持续维护未齐。 |
| 4.4 搜索推荐与可解释性 | PARTIAL | BT-V4-OPP-003,BT-V4-OPP-004 | 当前授权规则理由已有实证；现实查询可参加结果与消费者体验未验。 |
| 4.5 活动详情与行动状态 | PARTIAL | BT-RSV-001,BT-V4-ACTN-001 | 详情授权/时区/本地Participation有回归；真实资格规则与未知结果需拟开放范围验收。 |
| 4.6 组织工作流与运营 | PARTIAL | BT-V4-ORG-002,BT-ORG-004,BT-V4-E2E-003 | 多actor授权/发布有真实DB；普通组织者独立操作、运营负责人缺。 |
| 4.7 Plans提醒与关键变更送达 | PARTIAL | BT-PLN-001,BT-NTF-002,BT-V4-NOT-001 | Inbox/幂等worker已验；排队不是送达，生产调度告警/跨设备待验。 |
| 4.8 出席连接聊天与分享 | PARTIAL | BT-V4-ACTN-002,BT-V4-ACTN-003,BT-V4-CHT-003,BT-V4-E2E-001 | acceptedTie/同意chat/读时撤回存在；缺attendance来源，不从报名推断。 |
| 4.9 隐私同意和AI身份 | PARTIAL | BT-V4-PRV-001,BT-V4-SOC-001,BT-V4-SAF-004 | 本人独立开关/隔离已验；SAF004进行，AI分析/模型处理/Memory/公开尚无live许可链。 |
| 4.10 滥用预防举报和支持 | PARTIAL | BT-V4-SAF-003,BT-V4-PIL-002 | 举报/Block代码存在；真实值守/治理/处理演练缺，不宣称渠道合规。 |
| 4.11 非AI降级与恢复 | PARTIAL | BT-V4-NOW-005,BT-V4-E2E-004,BT-V5-AIR-010 | 确定性普通服务可复用；GPS拒绝/地图失败路径、UNKNOWN写恢复与模型降级未完成。 |
| 4.12 设备性能与可访问性 | PARTIAL | BT-PER-001,BT-PER-002,BT-V4-MAP-002,BT-V4-E2E-004 | Android16局部截图/键盘基线存在；未做TalkBack/大字体/弱网/其他平台独立验收。 |
| 4.13 数据安全正确性与恢复 | PARTIAL | BT-V4-MIG-001,BT-V4-E2E-003,BT-V4-SAF-004,BT-V5-AIR-040 | 052/权限/旧ID/并发有本地证据；AIR dispatch和备份删除传播未实现。 |
| 4.14 发布监控和运营 | BLOCKED_EXTERNAL | BT-REL-001,BT-V4-PIL-003,BT-V4-OBS-001 | 原发布NO；生产监控告警/回退/负责人证据缺。 |
| 4.15 价值留存和持续供给 | BLOCKED_EXTERNAL | BT-V4-ANA-001,BT-V4-PIL-004 | 无真实cohort完整观察窗/分母/非合成供给，内部测试不作价值证明。 |

PARTIAL/UNKNOWN是证据边界，不把外部评估待核验写成已发生漏洞。报名不是出席；图片位置/Moment链或双方going不证明本人到访/共同到场；分享仍按当前ACL重读，不获得永久许可。缺真实cohort、无提示用户研究、TalkBack/大字体/跨平台证据时不宣称消费者验收通过。

## 8 UIUX16检查：规则而非任务

root负责单一正文和AGENTS短链接，本子任务不编辑原UIUX正文。中文主要语言；Now为主意图入口且列表/详情/Plans/组织/设置保留直接路径。1–2问题是交互预算，不能猜事实或隐藏后果。批准对应当前可见版本，未知结果先核实。

| 检查 | 状态 | 附现任务 |
| --- | --- | --- |
| UX-CHECK-01 | PARTIAL | BT-V4-NOW-001,BT-V4-ACTN-001 |
| UX-CHECK-02 | PARTIAL | BT-V4-INT-005,AGE-033 |
| UX-CHECK-03 | UNKNOWN | AGE-015,AGE-019 |
| UX-CHECK-04 | PARTIAL | BT-V4-OPP-004,BT-V4-ACTN-001 |
| UX-CHECK-05 | PARTIAL | BT-V4-OPP-003,BT-V4-ORG-002,BT-V5-AIR-040 |
| UX-CHECK-06 | PARTIAL | BT-V4-SAF-004,BT-V5-AIR-024 |
| UX-CHECK-07 | PARTIAL | BT-V5-AIR-016,BT-V5-AIR-038,BT-V5-AIR-040 |
| UX-CHECK-08 | PARTIAL | BT-V5-AIR-040,BT-V5-AIR-051 |
| UX-CHECK-09 | PARTIAL | BT-NTF-002,BT-V5-AIR-038 |
| UX-CHECK-10 | PARTIAL | BT-PER-001,BT-V4-E2E-003 |
| UX-CHECK-11 | PARTIAL | BT-V4-NOW-005,BT-V4-E2E-004 |
| UX-CHECK-12 | PARTIAL | BT-PLN-001,BT-V5-AIR-050 |
| UX-CHECK-13 | PARTIAL | BT-V4-NOW-001,BT-V4-ACTN-001 |
| UX-CHECK-14 | UNKNOWN | BT-V4-NOW-001,BT-V5-AIR-050 |
| UX-CHECK-15 | UNKNOWN | BT-V4-PIL-004 |
| UX-CHECK-16 | PARTIAL | BT-RUN-002,BT-V4-OBS-001,BT-V5-AIR-049 |

适用功能须记录正常/空/错误/权限/批准版本/迟到/双击/弱网/重启/无AI无GPS/移动及辅助技术。现189 Flutter和Android16局部真实截图只能复用已测范围；截图不是无提示用户完成，平均通过率不能抵消越权/假成功/重复外部效果。

## 9 放行与接续

- **Closed Pilot Ready：NO；Consumer Beta Ready：NO。** 真实IdP/HTTPS拟发布真机、核验组织及授权活动、生产API地图、部署日志、值守/备用支持、提醒调度告警和真实A→H未齐。不用开发资料伪造。
- **AGE Foundation Ready：NO；AIR P0_CODE_COMPLETE：NO；P0_LOCAL_TEST_PASSED：未对V5运行；P0_LIVE_CANARY_PASSED：BLOCKED_EXTERNAL。**
- 模型视觉/AIR真实写/live A2A未启用；既有人类点击领域写不因本报告改变。当前Agent为受控查询/导航，不宣称代发/报名/公开偏好或共同兴趣。
- 当前SAF004先完成本项policy/action/HTTP/auth UI与证据；root自然checkpoint后评审矩阵并增量导入dry-run。保留144项ID/状态/证据，消费者/UIUX检查只附现有工作项。
- 缺provider批准/凭证/费用/数据策略只阻相关live调用，缺真实CSSA只阻试点路线；其他独立仓库代码工作继续。

机器矩阵：[work/v5-material-reconciliation.json](../../work/v5-material-reconciliation.json)，保留来源hash、源ID/acceptance/dependencies、当前task绑定、代码/测试/证据、live与offline状态及消费者/规则映射。本报告与JSON是静态审计截点，不是新的实时任务真源。

