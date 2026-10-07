# V5 Memory Reinforcement：本人明确批准的支持状态

2026-10-03；对应 BT-V5-AGE-009、AGE-005 原生 Evidence、AGE-008 Confidence。此增量提供仓库内可调用的本人管理领域服务和隔离本地验证；没有新增 HTTP、Flutter/Settings、模型出口或自动认知流程。根代理核证和共同回归另记，不能以本文件代替发布验收。

## 1. 单一 Memory 与独立强化状态

原要求是 existing Memory + new Evidence → reinforcement，不重复创建多个 likes hiking Memory。`agentreinforcement` 合同和 postgres `MemoryReinforcementService` 使用已有 Memory ID/version 与已有 005 Evidence；服务不创建 Memory、不改其内容、version、confidence、validUntil 或 lastReinforcedAt。

060 只新增 `agent_memory_reinforcement`：每个 Memory 一个独立支持状态，自己的 CAS version、当前 Memory version、已批准 Evidence/version/当前快照摘要及来源簇、最近支持时间、最后批准计划摘要。支持状态不是第二份 Memory 或领域事实正文。EXPLICIT 的 confidence 仍为 DIRECT_DECLARATION 1，表示本人明确填写，不是经行为验证的概率。View.lastSupportAt 属于这个支持状态；原 Record.lastReinforcedAt 仍为空。

056 明确要求 EXPLICIT lastReinforcedAt 为空；057 会在 Memory.version 更新时移除并 scrub 当前 Evidence，且 Evidence 绑定不可变。本增量不绕过这些约束，不改旧迁移或将旧 Evidence 绑定重写到新版本。不同语义 key 的声明不自动合并；调用者明确选择原 Memory，不能把未知字符串交给模型挑目标或生成多份 Memory。

## 2. 实际可调用服务

构造器 `postgres.NewMemoryReinforcementService(actualStore, actualServerController)`；nil Store/controller 和 066 Memory OFF 返回 Unavailable。开启开关只是服务端刹车条件，不授予源访问、身份、机器用途或 Agent 写权限。

1. `PreviewOwnReinforcement(ctx, PrivateAccess, memoryID, expectedMemoryVersion, expectedSupportVersion, evidenceIDs)`：经实际 session、active Person、精确 Personal Agent、053 metadata、当前 EXPLICIT Memory 与 005 Evidence/source 核查，形成最多 5 分钟的原生预览，期限还受 Memory 与当前 session 最早截止时间限制。
2. 预览 `Review()` 返回可检查的 Memory/支持版本、确切 Evidence IDs/版本/快照摘要、计划摘要、用途 HUMAN_EXPLICIT_REINFORCEMENT 与期限；这是只读草稿元数据，没有来源正文/活动身份声明。`View()` 是支持数投影。未来 UI 须通过原本人 Memory/Provenance 路径显示相同版本的声明和引用，再请求具体批准；本轮没有实现该 UI。
3. `ApproveOwnReinforcement(ctx, PrivateAccess, nativePreview)` 是本人明确执行的管理动作。重新读取所有当前来源，核对 session/主体/Agent/metadata、同一服务实例、066 原 ticket generation、Memory/支持 CAS、实际快照和 DB 时钟；只能消费该原生预览。preview private fields 不可由客户端设置，JSON 导入报错并清零，导出 capability 报错。复制 Review JSON、confirmed/verified、模型文本或开关 ON 均不能产生 capability。
4. `ReadOwnReinforcement(ctx, PrivateAccess, memoryID, expectedMemoryVersion)` 从实际 DB 重新读取、核查及回收过时支持。新实例能够读取已提交支持；进程重启后旧预览无效，须重新预览。

真正模型/认知调用 `ReinforceForCognition` 硬返回 `agentcognitive.ErrUnavailable`。没有当前机器数据用途授权或校准 resolver；普通本人查看/编辑权限不桥接到认知、模型出口、自动推断、候选 ACTIVE 或长期数据同意。

## 3. 当前来源与确定性去重

仅复用 005 三种闭集来源：本人私密草稿 Moment、当前 going Participation、SavedPlace；未知、撤销、跨 Memory/主体、不匹配版本、到期或原资源 ACL 不可见的来源不能批准。收藏不是到访，RSVP 不是出席；本人批准一条引用支持声明，也不把行为自动变成身份/兴趣真相。

所有来源 ACL 和当前 Memory/session 在单个最终组合 SQL 快照中核查；source row lock 延续 005 原锁序，不反向取得 Activity 锁。事务 SET LOCAL UTC，授权时间只读实际 DB `clock_timestamp()`，没有调用者时钟、deadline 自证或 fake resolver。

Moment 的簇取真实 `moment_activity_links.activity_id`；Participation 取真实 activity_id，同一活动下多条记录与 RSVP 合并为一个支持簇。多个活动交叉重叠按连通关系保守合并，结果不依赖输入顺序。未关联 Moment 使用 typed Moment ID；收藏使用 typed Place ID。多张媒体、更多 Evidence ID、重复批准不增加独立簇。

快照摘要包含当前 Evidence、源行 xmin 与 links 行 xmin 等实际元数据，避免原 ID 删后重建或未提升 Moment revision 的 links 改动复用旧批准。xmin/摘要仅是 opaque 一致性 token，不是业务 revision、可信身份、校准证据或用途授权；客户端/模型的 clusterID 不被采用。

状态可新增同簇的真实不同 Evidence，EvidenceCount 增加而 SupportClusters 保持不变。同一个原生预览并发/响应丢失重试只提交一次支持 CAS 更新；其他计划、旧版本或改变后的来源冲突，不进行盲重试写入。

## 4. 撤销、期限与清理

Memory 改版/删除及 Evidence detach 的 060 原生触发器保守清除整个已批准支持集合：版本前进，entries/plan digest/支持时间 scrub；剩余引用需要新预览和明确批准，不自动继承旧组合批准。不是提升 Confidence 或重建 Evidence。

来源改动、归档、取消、Block、隐藏、删除/重建、原目标到期等没有新增各领域异步删除管线；下一次经过当前本人权限的读取最终核查后清除过时支持。纯 DB 时间到期的 Memory 在有效本人读取时清除支持，再返回 NotFound/零 payload。session/账号/Agent/metadata 撤销首先拒绝读取；不能借失效会话执行清理或泄露支持内容。父 Memory/Agent 删除沿原 FK 清理。

最终来源快照是本次操作的权限线性化边界；之后仍核查 session/Memory 期限、ctx 与原 066 ticket，等待期间源 ACL 或 deadline 失效则回滚。没有声称能撤回已经合法返回到客户端的旧画面或提供跨领域实时擦除服务。

060 down 仅在自有隔离库验证，丢弃新支持状态并保留旧 Memory/Evidence/IDs。正式服务不得在线 down/reapply；维护后必须重建 service/重新生成预览，不能复用旧进程 capability。

## 5. 验证与 UX 边界

可复现命令：仓库根执行 `& .\work\v5-age009\verify.ps1 -Mode production -Round <fresh-alphanumeric-name>`。脚本只建随机自有隔离库，复用 001–059 和既有 dev seeds，再测 060 fresh up/down/reapply；目标 Go tests（两个包、限定新测试）、vet/build、全部旧 public 保留行及六源 SHA、finally 精确 drop 自有 DB。本轮不包含根正在实施的 061，不跑全仓 Go。

首次 compile RED 及 native1/native2/native4 RED、已修复的 fixture 原因、native3/native5 等历史绿轮均保留，不能把历史 SHA 视为更新后证明。最终结果以专属 evidence README/manifest 的实际 production round 为准；原生日志为合成本地账户的真实 PG 操作，不是真实用户/生产或 CSSA 试点。

UX-CHECK-05/06/07/08/09/10/11/12/16 有后端合同/正负用例依据：具体主体/版本/引用可检查、未知/到期拒绝、草稿与提交分开、撤权后拒旧批准、幂等、模型不可用、重启读取及不存来源正文。截图、移动/辅助技术、Now/详情交互、独立用户完成体验尚未实现/运行；UX-CHECK-01–04/13–15 不以合同检查冒充界面验收。Closed Pilot/Consumer Beta 原门槛仍有效。
