# Agent 用途限制 V5

唯一来源：BT-V5-AGE-062，原文要求“因为某个 Activity 临时授权的信息，不能永久进入 Organization / Business Memory”。本文件接续现有 Memory/认知/CurrentContext 与 AGA 边界，不新增第二套记忆、委派、同意或任务状态真源。

## 当前实现

`apps/api/internal/agentpurpose` 是闭集**禁止**内核。它只有 `ErrProhibited`、`ErrUnavailable` 和取消错误，没有 Allow/Verified/批准构造器、正文、TTL 或数据库。来自临时 Activity 用途的信息不能用于持久记忆或持久候选；组织与商家认知端口在没有真实保留许可桥时也明确拒绝持久候选。其他读取、模型出口和转发仍不可用。请求对象拒绝 JSON，客户端 purpose、来源声明、组织角色、RSVP、本人 profile_view、公开可见或更长有效期均不能制造权限。

现有 `UnavailableCognitivePorts.ReadMemory` 与 `SubmitMemoryCandidate` **实际执行**该内核。现有 SourceReference 无原生临时 Activity provenance，真实接线固定 `OriginUnknown`，不从 MemoryEvidence/RSVP/正文推断许可。Read 只返回零 KnowledgeView；Candidate 只返回 Unavailable。错误以 `errors.Join` 保留原 ErrUnavailable 与固定用途分类，组织/商家候选 reason 为 `memory_purpose_retention_prohibited`，不回显来源或正文。

FeatureGatedDomains 先核取消和当前 feature ticket，再保留具体闭集端口的固定分类；OFF/撤回只返回通用不可用。nil context 提前安全拒绝。开关不是同意。当前 ports 字段是私有具体失败实现；未来若改为 provider 接口须重新限制错误/回执，不继承任意外部回执。

原本的人类本人 EXPLICIT Memory / Manual Evidence 管理、资料查看的特定用途、领域按钮和 public supply 不被替换。原生身份、Agent、workspace、当前源版本、CAS、期限与最终 PGclock 仍由各自领域验证。

## 当前完成边界

**BT-V5-AGE-062整体裁决为 PARTIAL，唯一live状态以原队列记录为准。** 仓库内负面保护已通过原生验证，但没有真实的 Activity 委派签发、具体 Agent/接收者/字段/sourceversion/用途 resolver、受限临时投影消费、到期/撤回后的副本/摘要/索引清理链。纯 TEMPORARY_ACTIVITY 矩阵是 OfflineContract 禁止假设，不是原生获授权数据被使用的正例；机器端口零结果也不是完整生命周期验收。

恢复条件：在已有唯一 AGA/AGE/AIR 任务接通原生授权及 current resolver，明确处理许可与独立保留许可，消费前复核接收者/Activity/source/grant/task/expiry，所有 copy/summary/index writer 强制禁止升级，撤权/到期传播有可复现正负/并发证据。组织/商家长期 Memory 的正向能力、A2A、模型出口和全域删除分别保留原队列职责；本任务不为满足负面要求打开这些门禁，不增加重复backlog。

## 实证位置

审计及具体命令：`docs/research/BIRDTIE-V5-AGE-062-AUDIT.md`；原始代码、Go JSONL、隔离 PG、完整原行、guard 移除敏感性红绿与清理证据：`docs/testing/evidence/agent-purpose-limitation-2026-10-03/`。最终证据生成前不能预称完成。无新DDL、HTTP机器路由、客户端或provider；Closed Pilot / Consumer Beta 均 NO。


## 2026-10-05 原AGE062用途禁止本地验收重新核定（38lk/38ll）

根再次读取原AGE062、真实agentpurpose禁止内核、实际ports/ON wrapper、本人单/多Moment候选native写口及组织公开Activity人工Evidence边界；独立解析root-whole2原raw：95 PurposeLimitation PASS、0 FAIL/SKIP、6顶层。11相关代码before/after/current/执行copied SHA均一致，原整仓10797与5命令exit0、原public rows/xmin/catalog不变；019正在新增实现，因此未称10797覆盖当前新增代码。根证据work/v5-age038-resume/purpose062-original-ac-root38lk.json。

原source只要求临时Activity用途信息不能永久进入Org/Biz Memory：没有据纯合同或OFF判断完成，实际禁止调用/ON、原非空Memory/RSVP/profile_view正向控制及注册HTTP负例已核。现按原CODE_AND_LOCAL_VERIFICATION记DONE。历史PARTIAL、旧native460/95、guard移除红绿/故障及新增恢复条件完整保留；正文上方的历史PARTIAL快照由此条更新，唯一live状态仍原队列。

真实临时Activity授权签发、跨Agent临时消费、副本/摘要/索引到期撤回清理及真实provider保留行为仍NOT_IMPLEMENTED/NOT_VERIFIED，不因原负面AC完成而开放。普通本人EXPLICIT和组织自己当前公开活动的人工引用不是临时委派复制。未新增代码、测试、DDL、UI或对外服务；手机/TalkBack/真实试点NOT_RUN，Closed Pilot/Consumer Beta NO。
