# Birdtie V4 第一类社交 Intent：草稿边界

日期：2026-10-01。对应 `BT-V4-INT-001/002/003/004/005`。本页描述仓库内已实现的独立实体与接口；尚无正式环境部署或真实公开数据。

`036_social_intents.sql` 新建 `social_intents`，**不改写旧 `intents`、AgentTask 或对话**。旧 `intents` 仍是带必填城市的已存在路径，旧 ID 和 API 不变；新表允许 `context_id` 留空，不能为在线意图填写伪城市。新实体拥有 UUID、活跃 Person 创建者、类型、标题、JSON 对象约束、受众、`IN_PERSON/ONLINE/HYBRID` 模态、状态、到期与创建/更新时间。数据库用外键、枚举检查、对象大小/形状、创建者触发器和索引约束结构；有用户草稿时，036 down 明确拒绝删除。

当前 `POST /v1/me/social-intents` 只创建 `DRAFT`；`GET /v1/me/social-intents` 和 `GET /v1/me/social-intents/{id}` 仅返回当前已认证 Person 本人的记录，外人得到空列表或 404。服务器验证必填枚举、标题、约束对象、Context UUID 和到期；客户端不能提交 `status` 或创建者 ID 来伪造发布。草稿中指定 `PUBLIC` 受众只是**未来目标**，不会被公共发现读取。没有创建公开 Intent、Agent 自动转化或在线匹配的暗含路径。

`BT-V4-INT-002` 已加入模态约束解析：`ONLINE` 不要求城市、位置或 Place，且拒绝填写物理区域；`IN_PERSON` 要求经发布的 Place ID 或不含精确坐标的粗区域；`HYBRID` 要求物理区域/Place 与线上参与方式。服务端解析已知约束字段并拒绝未知字段、无效 Place、人数范围冲突。`context_id` 仍可留空，线上不被强行挂到 Aberdeen。中文“我的社交意图草稿”界面仅保存私人草稿，展示方式与真实服务端列表；没有自动公开或自动邀请。当前尚无机会引擎驱动的真实匹配或协调闭环。

`BT-V4-INT-003` 增加 `037_social_intent_audiences.sql`：LOCAL 的城市、COMMUNITY 的 Community 和 INVITE_ONLY 的真实 Person 受邀者使用外键表，既有草稿若缺明确目标则迁移拒绝而非编造目标。创建时服务端核验已发布城市、创建者活跃 Community 成员、受邀者活跃且双方无 Block。邀请记录当前只存储目标，**不发送通知**；草稿始终仅本人可见。公开 `GET /v1/social-intents`/详情仅消费已激活、未到期记录，并经数据库统一谓词核验：PUBLIC 需创建者公开资料，FRIENDS 需活跃双向 Tie，COMMUNITY 需双方活跃成员，LOCAL 需阅读者本人主动声明的当前 City Context，INVITE_ONLY 需未撤销邀请；所有范围均拒绝双向 Block。LOCAL 是用户选择的城市浏览范围，不证明居住地，也不读取或返回其 Person Context。对外返回的受邀 Intent 不含其他受邀者名单；本人管理接口可读自己的目标。草稿 API 本身不自动激活。

`BT-V4-INT-004` 增加显式本人状态接口：`POST /v1/me/social-intents/{id}/activate` 必须提交 `{"confirmed":true}`，只允许未过期的 `DRAFT→ACTIVE`，并重新核验公开资料、发布城市、Community 成员、有效受邀者和 Place 状态；`POST .../cancel` 允许本人取消仍有效的 `DRAFT/ACTIVE/MATCHED`，重复操作与过期转换返回冲突。激活和取消写审计。`MATCHED→CONVERTED` 与 `ACTIVE→MATCHED` 是已定义的服务端状态机边界，在036–038首批阶段尚无客户端可调用转换入口，不能让用户伪造匹配/转化。2026-10-05本人已报名活动关联增量见下节；该人工流程不代表机会引擎。到期采用**读取时规则**：过期的草稿、有效和已匹配 Intent 在本人 API 返回 `EXPIRED`，公开发现同时排除，取消/转化终态不被覆盖；无需依赖进程内定时器。Flutter 首批阶段只提供私人草稿创建，不暴露尚未完整设计的公开发布控制；API 激活已可用于受权集成验收，但未部署为正式产品。

`BT-V4-INT-005` 的 `038_social_intent_agent_origin.sql` 为**显式**由 AgentTask 保存的草稿记录来源任务 ID，并用外键、唯一索引和数据库触发器限制为本人已完成的 Personal Agent `FIND_ACTIVITY` 任务。`POST /v1/me/agent-tasks/{taskID}/social-intent-drafts` 要求活跃 Person 会话、活跃 Personal Agent、`confirmed=true`、有效且受众为 `PRIVATE` 的已编辑草稿。服务端重新核验任务归属、执行人、类型、意图与状态；重复保存返回冲突。普通搜索、筛选、区域搜索、组织任务、失败或未完成任务不自动或经此入口创建社交意图。Now 结果面板只对符合条件的个人找活动任务提供“保存为社交意图草稿”，进入中文表单供修改标题、类别、模态和粗区域后再保存。任务/对话与原有搜索 API 不变；保存不会激活、公开、邀请或匹配。038 带来源数据时拒绝 down；尚未正式部署。

`automation/verify_social_intents_migration.ps1` 在一次性 PostgreSQL 库先建立一条真实旧 schema 的合成 Intent，再应用 036（`-Through 37` 验证受众迁移，`-Through 38` 验证 Agent 来源迁移），执行全量 Go DB/API 测试与重连读取，核对旧 Intent ID 和 Activity 数量、空表回退/重做及有数据时受保护的 down。授权矩阵测试用合成数据直接置为 ACTIVE，以检验读取策略；这不是正式发布接口或真实用户发布结果。数据仅供开发验证，运行后库被删除。


## 2026-10-05：人工关联原已报名活动（085）

在原 `BT-V4-PLN-001` 内增加 [具体转换契约](INTENT-ACTIVITY-CONVERSION-V4.md)。当前个人在“我的社交意图”选择一条 `FIND_ACTIVITY` 的 ACTIVE/MATCHED 意图，经“从已报名活动完成这条意图”读取本人实际 going 报名，选择满足已明确约束且当前仍可见的活动，检查具体版本后确认关联。服务器在同一原生事务重新核验当前会话、本人 Agent/意图、原活动/报名与受众权益，按原状态机 ACTIVE→MATCHED→CONVERTED 写入原意图的稳定活动/参与关联和审计；不新增或替换 RSVP，不扩大受众、邀请或模型权限。普通报名、计划、私人提醒仍不需要此转换。

085 仅给原 social_intents 添加可空的关联字段与窄写入守卫。旧 CONVERTED 无关联记录保留为未知，不能补造 Activity 或 Participation；客户端不得提交 status 或关联字段绕过具体批准。人数、粗区域、线上平台等缺少权威可匹配事实时返回无可用候选，其他意图类型不借此转化。公开发现仍只读取有效 ACTIVE，不公开终态参与ID。已转换本人可通过原稳定详情与新只读关联入口核对；App/API 重启保留原关联，进程重启使旧具体预览失效。

本轮61个原生目标/迁移及45个客户端目标的本地证据在 `docs/testing/evidence/plans-integration-2026-10-05/conversion-final1/`。整仓结果由根代理另核；这些合成开发验收不证明部署、真实到场或试点通过。
