# Birdtie V4 非破坏性迁移计划

日期：2026-10-01。状态：Accepted plan；`033`–`042` 已在一次性开发数据库逐项实现/验证，均未正式部署。依据：[ADR 0017](../decisions/0017-v4-actor-agent-context-place-model.md)、[V4 产品规范](../product/BIRDTIE-CANONICAL-PRODUCT-SPEC-V4.md)、[代码差距审计](../product/BIRDTIE-V4-GAP-ANALYSIS.md)。旧迁移 `001`–`032` 保留原文件和稳定 ID。旧 `001`–`020` 没有统一 down 脚本，不能承诺全历史自动回滚；新迁移各自提供可测试的 down 或明确数据保留回退。

## 1. 安全准则

1. 先保留和核对当前工作树，按任务修改；不 reset、覆盖或批量重分类未知数据。新 schema 先 **expand**，服务端双读/兼容写，再迁移客户端；只有运营数据和旧客户端兼容窗验收后才讨论 **contract**。本轮不执行删列/删表。
2. Person、Organization、Community、Activity、Place、Session 的既有主键保持。现有 `organization_type='business'`、`'venue'`、`'community'` 仍是 Organization；无独立经营权证明时不生成 Business claim、Agent 或公开点位。
3. Agent 权限、Activity organizer、Conversation 成员、Intent 可见性由服务端核验。新 ActorRef 是领域/API typed reference；数据库仍以可验证外键/唯一性/检查约束表示对象关系。
4. 开发 seed 只经真实 DB/API 验收，不作为正式 CSSA、真实关系或经营权。新迁移在隔离数据库 up/down/reapply，验证旧数据数量/ID、授权和端到端回归；正式迁移另需备份、演练和监控。

## 2. 预留的增量顺序

实际迁移文件名按实施时最终 schema 命名；以下编号从当前最新 `032` 后连续预留，不提前创建空迁移。

| 顺序 | 阶段及拟议迁移 | Backfill / API 兼容 | 独立验收与回退 |
| --- | --- | --- | --- |
| 033 已实现 | `contexts` 五类节点、`person_contexts` 私密声明、Agent Task typed Context | `cities`/`city_contexts` 与旧 API 字段保留；旧任务城市经明确 FK 回填，旧城市写入由触发器补齐；非城市任务允许无城市。Person 不从旧城市迁移归属。 | 隔离库旧任务 1→1、Activity 5→5、跨城市/在线任务、Go 全量测试、down/reapply PASS；down 遇新 Context 用户数据拒绝。 |
| Agent 策略已实现，无迁移号 | 服务端共享 role/capability policy | 保留既有 `agents` 的 personal/organization 数据；Business 暂不可调用，Community 无 Agent。 | Go 角色与能力隔离测试通过；未增加无必要策略表。 |
| 034 已实现 | 持久 Person Tie、显式 friend 申请与旧 conversation 申请隔离 | 旧 accepted conversation 不自动变 friend；friend 请求不绑定城市，接受后仅创建 Tie，不自动开私信。保留旧聊天/请求 API。 | 隔离库旧 Activity 5→5、request/message 数量不变；双向唯一、移除/重新申请、Block/Unblock、HTTP/Go/Flutter、down 拒绝新数据与无新数据时 down/reapply PASS。 |
| 035 已实现 | 会话成员私有已读状态 | 保留旧 `conversations` 与 `conversation_messages` 的稳定 ID、正文和成员；每人回填独立状态，以迁移时间为旧消息未知已读基线，不伪造回执。 | 旧 Activity/Message 数量不变、成员验证、游标只向前、HTTP/Go/Flutter、Block、重连、down 拒绝真实游标及 down/reapply 在隔离库 PASS。结构化附件留待后续独立迁移。 |
| 036 已实现 | V4 社交 Intent 实体、模态与私人草稿 | 新 `social_intents` 与旧 `intents` 并存；ONLINE 不需城市/地点，旧 ID/API 不变，不自动转换。 | 一次性库旧 Intent 1→1/Activity 5→5、本人权限、模态规则、有数据 down 拒绝及空表 down/reapply PASS。 |
| 037 已实现 | 社交 Intent 受众目标与读取策略 | LOCAL/COMMUNITY/INVITE_ONLY 使用外键目标表；缺目标的旧草稿使迁移失败。统一函数核验 ACTIVE/到期、Profile、Tie、成员、本人城市 Context、邀请及 Block；状态激活/取消由 INT-004 API 实现。 | `-Through 37` 一次性库全量 Go 授权/生命周期测试及数据保护 down/reapply PASS；实际匹配/转化仍待机会任务。 |
| 038 已实现 | 显式 AgentTask → 社交 Intent 私人草稿来源 | 唯一来源外键、Personal Agent 已完成找活动任务触发器；旧任务不回填、不自动转换，原查询与对话保持。 | `-Through 38` 一次性库跨账号/组织/非找活动/重复/未确认测试、旧 ID 保留及来源数据保护 down/reapply PASS。 |
| 039 已实现 | Place 审核地址与公开详情关联 | 不改 `places.id`；候选/公开 Place 可记录有来源、经独立审核的地址。已有 Activity/Moment 外键继续指同一 ID；新地点活动 API 复用现有可见性。 | `verify_place_context_migration.ps1` 一次性库审核链、私密 Activity/Moment 不泄露、隐藏/过期 Place、旧 ID 保留及地址数据保护 down/reapply PASS。 |
| 040 已实现 | Venue：Place 的举办能力 | `places.id` 不变；候选的容量、预约、适配和设施有 HTTPS 来源、有效期及独立审核；无来源时不生成 Venue，经营组织引用可空。 | `verify_place_context_migration.ps1 -Through 40` 一次性库审核/API、旧 ID 保留、候选数据保护 down/reapply PASS；正式环境未迁移。 |
| 041 已实现 | Business 主体、成员、claim 与 Venue 经营关系 | 独立 Business 账户和主体，claim 默认待核验；Venue 关系默认待审核；旧 Organization business/venue 不自动重分类。真实 claim/Console 由 `BT-V4-BIZ-003` 接续。 | 旧账户/Agent/Organization/Activity/Intent/Place ID 与旧 organizer 关系前后不变；Owner/Admin 才能代表已核验 Business 创建活动，场地须有已核验关系；撤销后公开读取立即收回；有 Business 数据时 down 拒绝。 |
| 041 已实现（后续继续） | Business Activity organizer 与统一授权 | 新 Business organizer FK 与旧三类 XOR 共存；旧 `organization_id`、Host 与 JSON 兼容，旧 Activity 不回填为 Business。Activity↔Venue 与 TBD/线上规则由 042 接续。 | 隔离库四类主办方约束、旧 Activity/RSVP 回归、Business 会话与成员授权及 041 down/reapply。 |
| 042 已实现 | Activity ↔ Place/Venue 与线上、地点待定语义 | 旧有 Place 的活动回填为线下已确认；无 Place 的旧活动保持未知，不推断线上。新线下活动须有已确认公开 Place 或明确待定；线上不带物理 Place。可选 Venue 必须对应同一已审核 Place。保留旧 ID 和 `placeId`。 | `verify_place_context_migration.ps1 -Through 42` 在一次性库验全量 Go、状态切换、非法组合拒绝、旧 ID、数据保护 down/reapply；未部署。 |
| 043 已实现（仅仓库） | 私人 Moment ↔ Activity/Community/Organization | 复用旧 `moment_activity_links`，增量增加社群/组织外键链接表；Place 继续复用 `moments.place_id`，不公开私人记录。 | `verify_place_context_migration.ps1 -Through 43` 验全量 Go、旧 ID、043 数据保护 down/reapply；Flutter 私人草稿 UI 与 API 回归。正式环境未应用。 |
| 后续编号待定 | Place Memory 与可解释 Opportunity 投影 | 只用稳定 ID 和有权资料；当前私人 Moment 不进入公开地点详情。 | 私密 Moment 不泄露；Pin/Card/Agent 同 ID；旧查询与地图回归。 |

此顺序是 schema 安全顺序，不改变业务任务依赖；某阶段可拆成多个实际 migration，但编号须重新核对最新仓库后领取。`BT-V4-ACT-001` 先提供纯领域 ActorRef，无需先创建松散多态表。033 已有专门 SQL invariants、Go 集成测试和旧写入兼容检查；后续各迁移继续遵守此要求。

## 3. 发布与回滚协议

- **Expand**：在一次事务内新增表/列/索引/约束；大表索引和 backfill 分批执行并观测耗时。已存在行先审计，再决定映射；任何不可映射行进入诊断清单，迁移失败而非生成假主体。
- **双读/兼容写**：新服务可读旧客户端产生的 Organization Activity、联系请求、Intent、Place；新增响应字段可选，旧字段在兼容窗口保留。服务端写入同时满足旧约束，关键新对象只由新受权 API 创建。旧客户端不得误把 Business 当 Organization 管理。
- **客户端切换**：在同一 API 版本的兼容契约下逐屏接入 V4；等待可观测请求和真机回归后改变默认读取路径。空/错/权限和中文 UI 一并验收。
- **失败恢复**：应用可退回旧读取/功能开关，同时保留新表数据供调查；只在无新增用户数据依赖时运行对应 down。含真实 Tie、Chat、Intent 或 Business claim 的列/表不能以 down 静默丢弃，须事前备份和显式人工迁移决策。当前仅开发隔离数据库执行 up/down/reapply。
- **Contract**：另立任务和 ADR 复核旧客户端比例、真实数据、授权审计与备份；本计划无自动删旧结构步骤。

## 4. 固定回归证据

在每个阶段记录前后对象数及 ID 集合散列、孤儿数、旧 API 状态、权限拒绝、Flutter analyze/test/build、Go test/vet/build、合成 E2E，必要时真机与重启。最低固定 SQL 检查：Person Agent 1:1、Community 活跃 owner、旧 Activity 主办方恰好一个、Organization Activity 不失联、RSVP 不变、Place ID 稳定、私密 Intent/Activity 不出现在公开列表。

现有隔离脚本 `automation/verify_community_migrations.ps1` 覆盖 `001`–`032` 的前向应用、`032/031/030` down/reapply、旧 Activity 数量及新社群 seed 重复导入。V4 第一项 schema 实现时，扩展或新增 disposable-DB 脚本逐步覆盖 033+，不把当前脚本的 PASS 误称为尚未编写的新迁移 PASS。旧 vertical slice 使用 `automation/verify_vertical_slice.ps1` 回归。

2026-10-01 基线复核：`automation/verify_community_migrations.ps1` 在一次性本地 PostgreSQL 库 PASS，既有 Activity `5→5`、主办方 5、032/031/030 down/reapply、SQL invariants 和社群 seed 两次执行均 PASS。`pwsh -NoProfile -File automation/verify_vertical_slice.ps1 -ApiBase http://127.0.0.1:3696` PASS：组织管理→草稿/发布→公开发现与 Agent→详情→RSVP→我的活动→收藏，并取消其合成活动。首次用 Windows PowerShell 5.1 运行后者因不支持 `Invoke-WebRequest -SkipHttpErrorCheck` 失败；使用脚本需要的 PowerShell 7.6.5 重跑通过。033–035 已在隔离库验证，未部署。



