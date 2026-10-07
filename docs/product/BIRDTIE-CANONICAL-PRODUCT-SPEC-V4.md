# Birdtie V4 产品规范：AI 原生社交协同网络

状态：V4 新阶段产品总纲；2026-10-01。来源：用户提供的 `BirdTie_V4_Execution_Package.zip!/BirdTie_V4_Canonical_Product_and_Delivery_Plan.md`，已按本仓库身份、Community、Now 与中文界面规范核对。实现状态单独见 [V4 差距审计](BIRDTIE-V4-GAP-ANALYSIS.md)和 live 队列；本规范不把规划能力表述为已上线。

## 1. 产品定义与主循环

Birdtie 是 **AI 原生社交协同网络**。每个真实 Person 拥有一个 Personal Agent；它在明确权限内理解人的身份、持久关系、当前意图和相关情境，帮助人发现可行动的机会，并完成连接、交流、参加、收藏、导航或协调。活动和聊天是促成真实关系的动作，不以页面停留或消息数量作为目标。

```text
Person + Tie + Intent + Context + Place
    → Opportunity → Action → Shared Experience → Stronger Tie
```

Person 的 ID、Personal Agent、已授权关系、对话与历史不归属于某一个城市。Aberdeen 是首个高密度社交图试点，不是产品的数据或业务边界。在线意图可完全不依赖地点；异地、目的地与历史情境须在同一身份下表示。

## 2. 六个相互连接的领域层

| 层 | 主要对象 | 产品语义与边界 |
| --- | --- | --- |
| Identity Graph | Person、Organization、Business、Community、Agent 所有权 | 人是真实身份；Organization 是有成员管理的正式主体；Business 是商业主体；Community 是持续社交共同体。它们不可用假真人账号互相代替。 |
| Tie Graph | Connection/Friend、Follow、Membership、共同参与和历史、Block | 人与人的关系可持续存在，活动结束或 Intent 过期不自动消失。加入 Activity 不自动成为 Community 成员或好友。 |
| Intent Graph | 第一类 Intent、约束、受众、状态 | Intent 表达想做什么，可为线下、在线或混合；有时间、地点（可选）、类别、人数、可见范围和生命周期。AgentTask/对话不是持久社交 Intent 的替代品。 |
| Context Graph | 地理、机构、社交、兴趣、数字情境 | CityContext 是平台维护的公共城市上下文，不是 City Agent 或社交身份；Context 可表示当前、历史、目的地和在线情境。 |
| Agent Graph | Personal Agent、Organization Agent、未来 Business Agent | 共用运行框架但按主体角色、能力和授权隔离。Community Agent 仅未来可选，当前不创建；Place/普通 POI 不自动拥有 Agent。 |
| Opportunity + Action | Opportunity、Activity、Place/Venue、Moment、Chat、RSVP、Save、Navigate、通知 | 机会应给出真实实体和可解释原因；Action 需对应真实权限和持久状态。商业赞助与自然相关性结果必须区分标示。 |

## 3. 主体和权限

- Person：真实个人，恰好一个 Personal Agent，可参与多个 Organization/Community、创建其有权创建的 Activity。个人隐私与组织资料隔离。
- Organization：通过 OrganizationMembership 由真实人管理，有独立 Organization Agent。既有组织活动和已核验资料延续原权限边界。
- Community：一等社交实体，N:M 成员关系，可由 owner/admin 授权发布 Activity；当前无独立账号或 Agent。它不是群聊、Organization 的别名或地图点。
- Business：V4 目标是独立商业主体及其 Place/Venue 经营关系。现有 `organization_type='business'` 是兼容基线，迁移规则须由 ADR 和数据计划确定；不得直接重分类旧数据。
- `ActorRef/PrincipalRef`：领域层使用带类型和稳定 ID 的引用表达 Person/Organization/Business/Community 的行为主体。此抽象不得绕过数据库外键、用户授权或已有主办方唯一性约束。

所有 Agent 行动必须从服务端验证真实会话、主体所有权/成员角色、能力和目标数据的可见性。生成的文本不能授予权限。敏感社交推断和外部副作用须在明确同意与用户确认边界内运行。

## 4. 社交、意图与协同

最小 Social Alpha 需证明：连接申请与接受/拒绝/移除/屏蔽、同意后的 1:1 聊天、结构化实体分享、独立的持久关系、第一类 Intent、真实机会及至少一条协调到活动/计划的闭环。Follow、活动对话、共同历史与互相关系按各任务依赖和隐私规则逐步上线，不能从同场活动无授权地推断友谊。

Intent 至少表达 `IN_PERSON` / `ONLINE` / `HYBRID`、受众（朋友、Community、本地、公开、仅邀请）、时间窗口、可选地点要求、类别、人数、可见性、到期与状态。状态语义覆盖草稿、有效、匹配、转化、到期和取消。活动是临时事件；Community 是持续关系情境；Organization 是正式管理实体。

Opportunity Engine 从经授权的 Tie、Intent、Context 和 Place/Activity 生成可执行机会。早期规则实现可标记为规则式，不冒充通用推理或未核验推荐。用户能知道结果来自哪些实际数据、为何相关，以及可执行什么动作。

## 5. Place、Venue、Business 和 Moment

Place 是稳定的地理/社交情境节点，地图 Pin、Agent 结果、活动、Moment、详情和聊天附件应引用同一 ID。Venue 是可举办活动的 Place 或子地点，逐步补充容量、预约、设施及适配条件。Business 可经营多个 Venue；经营权和真实性需核验。普通公园、街道、海滩不会因出现在地图上成为 Agent。

Moment 可以连接 Person、Place、Activity、Community、Organization 和时间。Place Memory 只有在来源、隐私和授权成立时聚合近期 Moment、过往活动或朋友信号；它不是无差别公共动态。预约链接或可用性须来自已核验供应；原生预订和 Agent 间交易是后续阶段。

## 6. Now 与界面语言

Now 保持地图、Agent、结果、对话和行动同屏协作，但必须提供非地图路径承载在线意图、我的网络和共同情境。地理图层可以逐步投影 Opportunity、Activity、Place、Moment、Organization、Business；每类图层遵守来源、可见性、坐标授权和 [地图运行规范](../architecture/MAP-RUNTIME-PERFORMANCE-CONTRACT.md)。扩大图层不得使键盘重建地图、拖图清空选择、旧请求覆盖新结果。

所有新增主要界面、操作、空/错状态及 Agent 默认回复以简体中文为主；英文是次要、完整本地化后的选项。详见 [Global UX 规范](../ux/GLOBAL-UX-INTERACTION-CONTRACT.md)。专有名称和用户输入保留原语言。

## 7. 分阶段交付与 Gate

1. **Foundation**：本规范、领域 ADR、兼容迁移计划、真实队列状态和旧 vertical slice 回归齐备。
2. **Social Alpha**：持久 Tie、1:1 Chat、第一类 Intent、Place/Venue、跨城市/在线 Context、至少一条真实 Opportunity→Action→Plans 路径及相关隐私验收。
3. **Aberdeen Closed Pilot**：实际 IdP/HTTPS、真实已授权 CSSA 组织和活动、正式地图/API、值守/备用渠道、真实用户 A→H 与设备重启证据。当前为 **NO**。
4. **Beta**：Follow/Community 发现、更多机会/Place Memory/通知及可扩展运营能力。
5. **Business Pilot 与后续**：经营权/地点管理、已核验预约链接与分析；原生预订、Agent 间协同和商业变现须经独立门槛。

旧 [Functional MVP PRD](FUNCTIONAL-MVP-PRD.md)、[Community 模型](../architecture/COMMUNITY-AND-ACTIVITY-SOCIAL-MODEL.md)、[Agent 身份规范](../architecture/AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md)和 [Now 规范](NOW-AGENT-MAP-WORKSPACE.md)继续约束已实现切片的具体行为。若 V4 实现需要改变其语义，先写明新 ADR 与迁移，再更新旧文档引用。不要将本规范的 Social Alpha 新工作默认提升为当前 CSSA Closed Pilot 的已解决条件。
