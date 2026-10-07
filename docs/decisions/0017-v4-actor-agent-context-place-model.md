# ADR 0017：V4 Actor、Agent、Context、Place/Venue/Business 边界

日期：2026-10-01。状态：Accepted as V4 target architecture；实现以 live queue、迁移和测试为准。来源：[V4 产品规范](../product/BIRDTIE-CANONICAL-PRODUCT-SPEC-V4.md)及用户提供的 V4 执行包。此 ADR 扩展 [Agent 身份规范](../architecture/AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md)、[ADR 0014](0014-explicit-human-connections.md)和 [ADR 0016](0016-community-social-layer-and-activity-organizer.md)，保留其已实现阶段的事实。

增量 schema、backfill 和旧 API 兼容顺序见 [V4 迁移计划](../migration/BIRDTIE-V4-MIGRATION-PLAN.md)。

## Context

当前账户/Agent 表已严格区分 Person、Organization 与平台 CityContext。Community 有成员身份和三类 Activity 主办方约束，尚无 Agent。旧 `organizations.organization_type` 接受 `business` 和 `venue`，而 V4 计划给商业经营主体、地点与场地明确分工；既有 1:1 会话又来自显式联系请求，但不是完整持久 Friend 图。若直接重命名旧表、用一张弱约束超级表替代外键、或把地点当 Agent，将破坏已验证的组织活动、权限与隐私路径。

## Decision

1. **ActorRef/PrincipalRef 是领域/API 的带类型引用**，含 `type` 与稳定 `id`。V4 类型为 `PERSON`、`ORGANIZATION`、`BUSINESS`、`COMMUNITY`；引用本身不授予行动权限。每次读写都由服务端从会话、真实主体记录及角色/授权规则解析。数据库保持针对具体对象的外键、唯一性与互斥约束；不为了统一命名先拆掉当前 `activity_organizers` 的 Person/Community/Organization XOR 关系。

   已实现兼容细节：Organization 的 `ActorRef.id` 是 `organizations.id`；现有 Agent 表与 `agent_tasks.owner_account_id` 以 `organizations.account_id` 为账户主体键，因此 Agent `PrincipalRef.id` 在 Organization 情况下是该账户 ID。两者均稳定但不可互换，Go 使用不同类型，服务端通过已授权的 Organization 记录映射。旧 Agent task JSON 的 `principalId` 暂继续返回账户主体 ID；未来统一公开 ActorRef 字段时须增量新增，不可静默改变旧字段语义。
2. **Person 是全球身份**。其 Personal Agent、Tie、Chat、Intent 与历史跨城市保留。CityContext 是公共地理/发现配置，不是 Person 所有者、用户账号或 City Agent。V4 可增加国家、区域、机构、社交与数字 Context；在线 Intent 无须拥有 City 或坐标。Context 只参与筛选、授权与解释，不自动变成主体。
3. **Agent Runtime 共用实现，能力由角色与政策决定**。当前可执行主体是 Personal Agent 和 Organization Agent。Business Agent 需独立经营权/claim 与 capability policy 后才可启用；Community Agent 仅未来可选，不随社群创建自动生成。Agent-to-Agent 协同须显式授权，不能借生成内容或共享 Runtime 跨主体访问。
4. **Place 是情境节点，Venue 是 Place/子地点的举办能力，Business 是商业经营主体**。一个 Business 可关联多个 Venue；地点可以没有 Business，也没有 Agent。Venue 可记录容量、设施、适用场景、预约能力与营业约束，但不能从坐标、别名或第三方目录自动推断经营权。Place 的稳定 ID 贯穿地图、Agent、Activity、Moment、详情和分享。
5. **Business 与 Organization 的兼容边界**：V4 新 Business 是单独的商业领域/Actor 类型。旧 `organization_type='business'` 或 `venue` 的行继续是 Organization principal，原组织活动、成员和 Organization Agent 均不改变。未来经过身份/经营权审核及明确迁移映射后，才可把指定记录关联到 Business；不得批量重分类、双重授权或创造假 Person。具体新增表、backfill 和 API 兼容窗口由 V4 迁移计划决定。
6. **Activity 主办方渐进扩展**。当前 Person/Community/Organization 三类和既有 `organization_id` 兼容字段继续有效。引入 Business 主办方时用可检查的第四种关系和后端经营权校验扩展唯一性，不允许多主办方或无主办方；旧 Activity ID、发布状态与 RSVP 保留。
7. **隐私和推荐**：Connection/Follow/参与/共同情境分别建模，参与活动不自动成为好友。Block 与举报边界跨发现、聊天、Intent 和 Agent 工具生效。赞助机会显式标注，不能静默覆盖自然相关性排序。

## Supersession and compatibility

Agent 身份规范中“business/venue 与其他 Organization 共用模型”描述的是已实现 V2 数据库事实；本 ADR 对 V4 新 Business 的目标语义作窄范围后续决定，**未将旧行即时转换**。ADR 0016 的三类 Activity organizer 仍是当前运行约束，V4 Business 扩展须以迁移、服务端鉴权和回归验收后才生效。ADR 0014 的真人会话同意路径也继续有效；V4 持久 Tie 不可从一次接受会话静默推断或向 Agent 开放消息正文。

## Consequences and verification

- 新 ActorRef 解析器必须拒绝未知类型、错类型 ID、无权主体与过期/撤销成员资格；Flutter 的主办方选择只作 UX。
- Business/Agent/Activity 的新增 schema 必须向后兼容，提供旧组织类型映射审计、可逆开发迁移、原 Activity 数量/ID 不变的检查。
- online/cross-city Context 与旧 `intents.city_id NOT NULL` 的冲突须通过新增路径/迁移解决，不可给在线意图填伪城市。
- Agent 能力、消息读取、位置和赞助信息都必须有可观测授权决策与对应测试。
- 此 ADR 是设计验收；各能力是否已实现以 live 队列和独立证据为准。当前 ActorRef、V4 Agent Runtime、Venue 基础与独立 Business 主体/活动授权已在隔离库实现；真实 Business claim/Console、Business Agent 与正式部署未完成。
