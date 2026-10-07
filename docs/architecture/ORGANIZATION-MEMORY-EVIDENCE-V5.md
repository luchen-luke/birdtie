# Organization Memory Evidence V5

2026-10-03，BT-V5-AGE-053。原需求来源见 [AGE Epic](../product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md)；复用 [Organization Memory](ORGANIZATION-AGENT-MEMORY-V5.md)、[Memory 边界](AGENT-MEMORY-ARCHITECTURE.md)、认知 ADR 与原发布门槛。

## 权限和归属

这是当前 Person owner/admin 管理人工声明的来源说明 API。`agentorganizationmemory.Access` 仅由服务器实际 session/actor/path 构建；实际组织实体 ID 不能当 Organization account principal，source 绑定当前原生组织 Agent、Memory version 和 account principal。复用 068 Organization → 有序 Accounts → Membership → Session → Agent → metadata → Memory 的真实锁顺序；每个操作最终检查 PG 时钟、session absolute/idle expiry、当前角色、账号、Agent 与 metadata。

公共资料可读、JSON 形状正确、source ID 存在、人工 reference 或 Memory annotation 均不授予 cognitive purpose、推理、模型出口、自动写、消息发布或事实核验。PERSON 057 wire/validator/SQL 分支保持；AIR 来源注册没有扩展。无新的 production permission、source grant 或服务配置。

## 来源

| closed sourceType | 原生权威来源 | 版本 | 当前可用条件 |
|---|---|---|---|
| ORGANIZATION_PROFILE | organizations 的现有资料字段 | 真正 updated_at + 封闭公开资料 snapshot SHA256 | 精确该组织、active/public/verified、实际当前管理员未被组织 block |
| ORGANIZATION_ACTIVITY | activities 与 activity_organizers | 实际 Activity revision | 精确组织 entity + 原生组织 organizer，公开已发布、未取消、未来有效时间、活动/城市 expiry、原始 Activity ACL 与 blocks |
| ORGANIZATION_ADMIN_INPUT | 同组织另一条 068 Memory | 实际 Memory version | 当前 Agent/owner，同组织、explicit/active/有效期内，禁止自引用；管理员人工声明不叫独立核验事实 |
| ORGANIZATION_PUBLIC_FAQ | organization_faqs 已有发布问答 | 真正 updated_at + 封闭 FAQ snapshot SHA256 | 精确同组织、已发布、当前 active/public/verified、blocks；不伪装通用内容 feed |
| ORGANIZATION_ANNOUNCEMENT | 075 organization_announcements 独立领域 | 实际 Announcement revision | 同组织 entity/account、人工明确 PUBLISHED/PUBLIC、当前组织公开 context 一致、active/public/verified、Agent/metadata 当前、原始 blocks、PG clock 未过期；不借用 Memory 类别或 FAQ |

Profile/FAQ 的 digest 带 sourceType、真实 retained updated_at、原生 source/principal ID 和规范字段；不是 CAS/revision、概率或永久授权。Activity 和 admin Memory 用实际原生 version。`eventTime` 只是所存状态 updated_at，`observedAt` 是服务器关联时刻；两者不是举办、到场、访问或用户实际动作时间。不复制原文、媒体、人员、地图坐标或外部 URL 到 ledger 或 API，仅输出 minimal source metadata。

## 同一 ledger / 迁移

073 对 `agent_memory_evidence` 增加独立 ORGANIZATION owner 分支，不新增第二份 evidence 表。073 历史版本只支持四类；075 保持 PERSON 分支原文，再加第五种 ANNOUNCEMENT REVISION。自引用 admin input / NULL / Person 来源继续拒绝。原 composite Memory FK、不可变 binding、单次 tombstone、current source unique 与 Memory-version-change 同事务清除继续生效。075 最小 control 只保留 evidence ID→公告来源类别，未复制 source 地址、版本或正文；用于 source scrub 和真实父公告消失后防止破坏性 down，不是第二份 provenance ledger。

down 获取表锁后先查任何组织行，包括 REMOVED；有历史则原子拒绝，不删除证据或降级其 owner。组织行为空时才恢复原 057 owner/shape；个人记录和其他全库数据/函数/触发器不改。迁移只在本任务新建隔离库测试，未部署到 live DB。

## 人工 API

- PUT `/v1/me/organizations/{organizationID}/agent-memories/{memoryID}/evidence/{evidenceID}`：strict 三键 expectedMemoryVersion/sourceType/sourceId。客户端不能提供 owner/Agent/confirmed/version/status/weight/time/purpose。重复相同 ID、当前 target/source snapshot 精确相同才幂等；改版/重复 source/已 removed ID 返回冲突。每条 Memory 最多100条 current source。
- DELETE 同一路径：strict expectedVersion，单次 tombstone scrub source 元数据，匹配原版本或已 removed retry 幂等。不允许删除原生历史行。
- GET `/.../{memoryID}/provenance`：中文固定 declaration、当前 Memory version、Organization entity/account/Agent、当前 source metadata 与计数。正文/旧版不可访问来源不返回。未关联时如实解释“尚未关联当前有效来源”。

响应 no-store 与现有 request ID。未知状态提示重读，不自动重试写入。HTTP PUT 在真实提交后重新核当前管理员并重读 provenance，只返回当前存在的同 evidence/source；GET 在首次读后核当前管理员，再做最终原生读，拒绝主体/Agent/Memory version 漂移。正式认证仍受既有 IdP 与 release gate，不以 synthetic session 代替。

## 并发与撤回

读/写 target Memory 受同一个组织事务与 Memory lock 保护。FAQ/Activity 原生写会先锁源行，再写带 Organization FK 的审计；本 API 不在 Organization FOR UPDATE 后反向获取 FAQ/Activity 行锁，避免死锁。所有 source 条件、target Memory version/状态/有效期与 session absolute/idle expiry 在一个最终 SQL snapshot 求值；源码字符串封闭，sourceID 只作绑定参数。一次 snapshot 后发生的新变化不属于该 snapshot，不声称永久授权或网络瞬时撤权。

当前读先取一个完整 snapshot，invalid reference 退役并 scrub，再取最终完整 snapshot；期间变化的 source 被剔除，必要时下一次管理读再清理。属于人工管理边界的 lazy cleanup，没有安装异步全域 source-change 服务。source 已改版要由管理员选择新的 evidence ID 重新关联，不能 revive 已撤回版本。Memory 改版/删除仍由原 trigger 在同一事务清除旧 provenance。

## 验收范围

实际 native tests 与 fresh/current-data migration 命令和结果在专属 evidence；不得用 fixture 推断生产组织/活动真实性。适用 UX-CHECK-02/03/05/08/09/11/13/16；本任务无新 Flutter UI，真机/视觉/TalkBack、真实合作方 announcement、组织 cognitive source grant、模型/自动学习、真实 IdP/部署/Pilot 未运行或未开放。原生合成 announcement 验收另有075帧，历史073四源181PASS和 worker-final1保持不可变。Closed Pilot / Consumer Beta 条件未因本 API 改变。

## 第五来源人工发布

参见 [独立公告来源契约](ORGANIZATION-ANNOUNCEMENT-SOURCE-V5.md)。人工私密 draft → 当前管理员/真实 session/实际角色与 source version 绑定的短预览 → 明确 publish；编辑退回 draft，撤回终态，过期失去公共来源资格。组织 context 变化不自动复用旧公开批准。所有当前来源解析复用同一 native public predicate；人工 evidence 不授权模型认知或广播。
