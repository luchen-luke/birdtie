# 社交与 Agent 原生审计关联（BT-V4-OBS-001）

2026-10-05 · 本地代码验收；生产与 Closed Pilot 未验收。

## 原合同与边界

原验收为 Connection、Block、Activity mutation、Business claim、Agent action 和 permission changes 可查原 actor、target、request。复用原领域提交与原审计（086 七套，087 组织点位第八套），不建立新的授权、幂等或效果台账，不增加公开审计 API。适用 UX-CHECK-04/05/08/11/16：请求追踪不能代替本人具体批准或当前权限；失败不冒称成功；不记录私人正文、验证码、会话令牌、供应商 URL、精确位置或许可 proof。

HTTP 中间件保留原请求 ID 的格式规则和生成行为，并将通过验证的 ID 放入 `audittrace` 私有 context key。字符串键不能冒充该 key；格式仅允许 ASCII 字母、数字、下划线、连字符，8–64 字节。该 ID 仍是调用方可提供的关联信息，不能证明真实身份、归属、批准或效果，重复 ID 不会绕过原 CAS/权限。

原事务通过参数化 `set_config(..., true)` 设置 LOCAL 关联值。七原审计表默认读取该值；空值/后台没有 HTTP context 时为 NULL。事务结束后不污染池中下一条连接；Run 原触发器在同事务读取该值。没有搬动 Run 生命周期、权限、期限或重试分类。

## 086 增量

七表新增 nullable `request_id`：`audit_events`、`admin_audit_events`、`business_console_audit_events`、`business_public_profile_audit`、`organization_announcement_audit`、`model_budget_audit`、`agent_run_audit`。先添加 nullable 字段再设置默认值，历史数据继续 NULL，不伪造旧请求关联。每表有相同格式 CHECK 和非 NULL 请求索引。

`audit_events.target_resource_id` 为可空原领域 UUID；保持原 `resource_id` 含义。用于原 Profile grant/三个目的 grant 的具体原 grant ID、实际 RSVP/收藏/提醒关联目标与原意图转换 Participation ID；并非新资源或授权。

Down 只允许没有新关联/具体 target 数据的情况，锁原七表后拒绝销毁已用证据。077/078 原 append-only INSERT/UPDATE/DELETE/TRUNCATE 守卫保留。没有改变各原表的访问权限、保留策略或运营调度；内部审计仍供原受信任运维/开发访问，未部署新查询权限或保留作业。

## Producer 覆盖矩阵

| 原验收域 | 原 writer 与审计落点 | 行为 |
|---|---|---|
| Connection/Block | `connections.go`、`friend_chat.go`、`new_people.go`、`chat_entity_sharing.go`、`blocks.go` → base audit | 保留原请求/会话/消息/Tie ID；Block 只记录真实新 block；真实安全撤 grant 记录原 grant ID |
| 活动与管理员 | `activity_publish.go`、`social_activity_publish.go`、`admin_audit.go`、`organizations.go`、`organization_faq.go`、`organization_memberships.go` | 原 base/Admin audit 同事务关联；原权限、发布、通知链保留 |
| RSVP/私有清单 | `activity_participations.go`、`activity_plans.go`、`saved.go` → base audit | 新增真实参加/取消、提醒/删除、收藏/删除的最小事件；重复无真实变更不新增成功记录；提醒不冒充报名 |
| Business | `business_console.go`、`supplier_profiles.go` | 原 claim/review/revoke 与明示公开 permission 各自原审计；不复制管理资料/核验 URL |
| Agent 人类改写 | `agent_workspace.go`、`agent_seed.go`、`agent_private_profile.go`、`agent_memory.go`、`agent_memory_candidate.go` | 原 Task create/真实 update；Seed 真实意图/City 声明变更；本人私有资料/Memory 及人工候选接受/拒绝；不记录 query/conversation/Memory 正文 |
| Agent 组织与公告 | `organization_agent_memory.go`、`organization_announcements.go` | 原 Admin 组织人工 Memory 审计及原 ANN 独立审计，不冒称公告广播/运营 |
| Permission/settings | `identity.go`、`agent_context_purpose.go`、`agent_enrichment_purpose.go`、`agent_candidate_retention.go`、`agent_profile_visibility.go`、`agent_policy_settings.go`、`notification_routing.go`、`social_context.go`、`relationship_context.go`、`new_people.go` | 原批准/撤回/current grant 链不变；声明同值提交不重复制造成功审计；原末条权限/源/PG 时钟检查仍在审计之后 |
| 人类意图/转换 | `social_intents.go`、`active_intent_lifecycle.go`、`intents.go`、`intent_activity_conversion.go` | 原 draft/明示生效/改稿/取消/转换，原稳定 ID；085 转换 target 是原 Participation，不能推定公开报名或已到场 |
| 模型预算/Run 控制 | `model_egress_budget.go`、`agent_runs.go` | 原 human budget review/audit、原 Run trigger 同事务关联；后台没有原请求则 NULL；不启用模型/自动动作或更改未知 Run 失败原因 |
| 组织公开点位许可 | `organization_map_locations.go` → 原 `organization_map_location_audit` | 原 Submit/Hide/独立 City reviewer approve/reject 在同原事务关联 request；保持原 org/actor/revision/action，不复制坐标或审核正文 |

这不是“所有仓库历史审计已全接入”。原非本轮 producer 继续保留原审计且请求未知为 NULL：Activity/Community 专属聊天、Moments/公开发布、地点语义/场地审核、City/Activity 导入、OIDC/开发登录等。`organization_map_location_audit` 未扩入 086 七表迁移；freeze1 后审计发现它属于原公开许可 AC，增量纳入独立 087，保留 029/086 原字节。独立 `person_contexts.go` 通用声明没有被本轮改写；仅 Seed 同事务真实 City 选择记录事件。具体列表以 leased source manifest 和专属审计清单为准。

## 087 组织点位审计增量

仅在原第八表新增 nullable `request_id`、同格式 CHECK、非 NULL 索引与原私有 LOCAL context 默认值。旧行仍 NULL，down 在独占锁内发现已用请求关联即拒绝。Submit/Hide/Review 三个原 INSERT 使用同事务 `auditExec`；Review 的原 pool.Begin 不会借用此前连接的请求。没有新增权限、公开 API 或坐标日志，没有声称修复原 actor-only writer 的会话窗口。

原 Submit 的重复提交、原 Hide 对已隐藏记录的再次修改仍按原 revision-changing 语义处理，不把这些真实变更称为 no-op。GET、无角色、撤回角色、自己审核、重复审核或业务失败不能制造成功审计。原本有幂等/no-op 的其他领域继续要求零增量。

## 事务与无变化语义

原 source/Session/期限末核不得被审计置后覆盖。新增审计在原修改之后、原最终 native 检查之前，任一失败回滚原业务和审计。特别候选接受、三个目的批准/撤回、077/078 人类声明、085 意图转换保留原 sealed source currentness；请求 ID 不进入许可条件。

既有 legacy API 的原 actor-only 权限接口仍保留；本轮不宣称它们获得新的 Session-bound/具体版本批准，也不将其等同机器目的许可。Bound ACTN 写口仍复用原完整末核。内部 SQL 审计可记录原数据实际提交，不能宣称客户端已查看、模型已执行、外部分享已送达或真实人已参加。

## 证据

原失败与每轮不可变复制帧保存在 `work/v4-obs001-20261005`，正式证据索引在 `docs/testing/evidence/audit-correlation-2026-10-05`。命令测试使用 fresh owned 本地 PostgreSQL，合成身份与资料，不是生产/真人/合作方资料。

当前已通过 native4 的 27 测试事件及 test/vet/build、两 CLI 构建；086 unused down/reapply、旧完整行/catalog/Participation xmin 保留、077/078 守卫及 owned DROP 已实测。原 native3 取消等待断言失败保留：最初新增 SELECT 锁使旧原 UPDATE 等待观察失配，改用同 statement previous/changed CTE 后原旧断言 GREEN，无删减测试。

扩大回归 native6：671 PASS、0 FAIL/SKIP/package failure；test/vet/build 和两 CLI build 退出 0。51 owned Go/SQL 稳定且与 freeze1 相同；执行完整 885 源码加 3 原 seed 的不可变副本。其他并行任务使 live all-API 在执行期间发生变化，未声称它们也稳定或本轮整仓通过。086 完整旧行/catalog/Participation xmin、unused down/reapply、最终完整 public/catalog 相同、owned DB `birdtie_obs001_06f809deb0e3` 实际 DROP。

新增 Memory 本身有效期限 900ms 在实际审计锁等待中自然到期，原 ErrInvalid、原源 snapshot 不变且成功审计 0；会话 idle 700ms 到期则原 ErrForbidden，原 Memory 与成功审计均 0。两者观察具体独占 application_name 与持锁 PID 的 `pg_blocking_pids`，不是睡眠猜测或只断言错误。

根代理联合全量仍 PENDING。本轮没有 Dart 改动，Flutter/真机/辅助技术、本地以外部署、模型供应商调用、真实人员/Closed Pilot 均不是这批证据。旧 Run `after_commit` 首次 RED 的原因仍 UNKNOWN，本轮未把 2 秒 lease 猜测当结论或修复。

## 原整仓 RED 与测试边界增量

根独立 `root-whole1`：10279 PASS、7 FAIL（含父用例）、3 package FAIL，vet/build/两 CLI 退出 0。四类失败为旧 Profile/Policy 把合法新增审计也列入“不变”快照，及原 cognitive/social/bridge/Context Task 清理遗漏新审计外键。真实前后快照有新增 10 个自有 fixture Account 与 8 条 base audit；原 RED 与残留证据保留，不拿定向绿色替整仓结果。

获准六测试文件增量保持原 public、Memory、purpose 及 CAS 单赢家断言。成功 private/policy 修改精确核验新增 action、actor、原 Agent resource、purpose、request ID、decision 和无私密正文；并保留全部旧 audit 行。GET、重启读取、同值 no-op、过期版本及并发 CAS 失败仍要求零新增。清理检查全部错误与零残留；随机新账号仅删本测试 actor 行，共享 seed actor 的 Context 测试仅清它自己的 Task IDs，未泛删 seed actor 审计。

native7 fresh086：699 PASS/0 FAIL-SKIP、test/vet/build/两 CLI 0，57 owned 与全部 890 API 源稳定，3 seed 为明确额外输入；完整 public/catalog 相同、owned DROP。native8 扩全部 Profile/Policy 边界：151 PASS/0 FAIL-SKIP，同样 57 owned/完整数据与结构保持，freeze2 保留。

HARNESS_REVIEW：root 初审把 base audit ID 当随机 UUID；实际 `audit_events.id` 是 bigint，native7 没有排序失败。测试 helper 仍改按 ID→完整 row 集合差分，独立保留新增行用于脱敏检查，纯前插/夹插/旧行改删/重复 ID 用例属于辅助函数健壮性核验，不冒称业务 RED。

native9 fresh087 真正功能 RED：registered PUT 已 200，但原点位审计精确 request 匹配 0，期望 1；修为三 INSERT 同事务关联。native10：152 PASS/1 FAIL，新增 fixture 把组织 membership 写成非法 `revoked`；原 019 实际为 active/invited/removed，City reviewer 才有 revoked。原 helper 只输出 SQL failed、未捕获 SQLSTATE；不补造原错误码。保持原约束和 403，改合法 removed 后执行 native11。旧 51/freeze1/worker-final1 和新 57/freeze2 均保留。

当前 freeze3 为 60 Go/SQL；native11 fresh087 已实际终态：799 PASS、0 FAIL/SKIP/package failure，test/vet/build 与两 CLI 退出 0；完整 892 API 源加 3 原 seed 的不可变输入（895）稳定，60 owned 与 freeze3 字节一致。旧完整 public/catalog/Participation xmin、087 unused down/reapply/used down拒绝、077/078 原守卫通过，owned DB `birdtie_obs001_b0e21be20483` 实际 DROP。

该轮原注册 HTTP 点位提交、独立 City reviewer approve/reject、Hide、无权限/角色撤回/自己审核/重复审核/GET 零成功审计、后台 NULL 均真实通过；旧 Profile/Policy/CAS/Memory 与原目的链扩大回归也通过。freeze3 在 native11 终态前写入，PENDING 字段是时间顺序记录，最终结果以 native11/result.json 为准。根联合完整整仓回归仍 PENDING；定向不代替全量。当前关联不改变业务权限或消费可用性；Closed Pilot / Consumer Beta 仍 NO。

## 2026-10-05 根代理联合 087 最终本地验收

后续终态已由根代理独立核证，参见 `work/v5-age038-resume/joint087-whole-root-proof38cz.json`：整仓 Go 10345 PASS、0 FAIL/SKIP/package failure，test/vet/build 与两 CLI 构建共五个命令退出 0。895 个不可变执行输入（892 API 源加 3 原 seed）与 live 对应字节一致；完整旧 public 行、可见语义 catalog、原 Participation xmin 和 087 unused down/reapply 保持。独占库 `birdtie_obs001_93fe139a7a01` 已独立 SQL 确认不存在。

根归档核证 `work/v5-age038-resume/joint-final-archives38cy.json`：旧 archive1 的 1272 文件不变，新 archive2 的 1257 文件、74916830 字节均逐字节匹配且无额外文件；archive2 manifest SHA256 为 `959afd0a88df65809741204885e8a5df1f2aea96e86d6d778ec98259e87ea40f`，当前 60 源与冻结字节一致。该较早归档核证文件的 wholeGo087 RUNNING，以及本文此前 PENDING/原 whole1 RED，均保留其当时状态；最终 Go 终态以较晚的 38cz 证明为准，未覆盖原失败。

这是 OBS001 CODE_AND_LOCAL 的本地验收，不表示所有历史 writer/request 已覆盖；其他 producer 与历史 NULL 仍按上述矩阵保留。Run after_commit 原 UNKNOWN 原因未修复。race（CGO0/无 GCC）、本批新真机界面、TalkBack、真实人员/合作方/正式部署均未验；联合 Flutter 构建不代替这些证据。Closed Pilot Ready / Consumer Beta 仍为 NO。
