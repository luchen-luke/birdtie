# Agent 本人沙箱动作执行契约 V5

来源：BT-V5-AIR-040 / AGE-045；本文件细化现有 AIR 与 ENTITY-ACTION-CONTRACT，不新增需求队列或权限真源。


## 2026-10-06 AIR040：本人沙箱批准、单次提交与结果核查

原 AIR037 `CheckOwnSandboxTool` 继续只生成 `CONFIRM`，不是批准或执行。新 `agentaction.Service` 通过原 `postgres.Store` 的 `PreviewOwnSandboxAction` → 本人显式 `ApproveOwnSandboxAction` → `CommitOwnSandboxAction` 接入真实原生领域。仅闭集 `sandbox.write.v1` 写入隔离的 `agent_sandbox_writes`，不调用消息、报名、资料、预订、承诺或外部服务。主 HTTP、模型工具入口、provider、客户端未接入；默认 Controller OFF 保持。普通人类 API、原候选/预算/Run/旧037读取边界不改。

097 仅新增三个最小表：不可变审阅绑定、唯一 dispatch/effect 状态账本、真实私有沙箱数据。独立 `OWN_SANDBOX_ACTION` / `sandbox_write` 用途复用原 `consent_grants`；它仍是唯一批准生命周期，不借用模型出口、Task context 或记忆授权。原 064/082/091 `MEMORY_CANDIDATE` 效果账本及守卫不扩权。

绑定包含实际 tenant/actor/subject/agent/Session/Task、logical operation/action、工具版本、目标、完整参数摘要、源 token+xmin、原身份/065策略指纹、独立 consent 版本和期限。PERSON 本人范围的 membership 为明确不适用，不能推导组织权限。批准期限不延长原生目标的30秒上限；过期需新审阅与明确确认。Session/身份/Task/policy/approval/grant 原行锁、原策略 absence 锁和最终数据库时钟将批准消费、dispatch commit 与撤权排序。参数、目标或主体变化不能复用批准。

批准消费只说明 `DISPATCH_COMMITTED`，不表示成功。只有新提交且事务确认返回的非 JSON 进程内 Commitment 能一次 Begin；Claim 绑定同实际 Store/Access/Controller ticket 和持久 fence。恢复进程不能从 JSON 重新构造发送能力。实际效果先在单独沙箱事务写入，最终真实时钟/fence/lease重新检查；收到响应不明或实际写入后的进程退出不会生成第二个发送能力。

`MarkOwnSandboxUnknown` 明确持久 `UNKNOWN_OUTCOME`。`ReconcileOwnSandboxDispatch` 在锁住原 dispatch 后读取真实沙箱效果：有实际 row 才为 `SUCCEEDED`；只有同一原生封闭沙箱内、排他锁与递增 fence 永久封闭旧 Claim 且不存在实际 row 时，才能确定 `NO_EFFECT`。不存在 ID、读取失败、404、批准已消费和 lease 到期都不等于成功或失败；不支持外部工具以 absence 推导结果。已提交动作先于撤权时仍可能在飞，后续只核查实际结果，不谎称撤回。源编辑/撤权后原已发生回执不重写；禁止追加步骤或自动重发。同一 logical operation/action/effect kind 仅一真实 row；两次刻意相同新操作保留各自效果。内容摘要/handler版本不作效果键。

审计复用原 `audit_events` 与 request correlation，只记录实际 actor、闭集决策/状态、用途和原生 ID；不存沙箱正文、Session token/digest、权限指纹、私聊或隐藏思维链。使用后的097 down拒绝丢弃历史；空库 down/reapply 与原001–096数据/xmin/catalog保持分别验证。

适用 UX-CHECK-06/07/08/09/10/11/16。实际原生数据库正负/并发/撤权双序、最后时钟、真实子进程崩溃/新进程恢复、未知提交、SQL篡改和旧回归证据在 `work/outbound-action-safety-2026-10-06` 与 `docs/testing/evidence/outbound-action-safety-2026-10-06`。纯 canonical/schema 测试、原生合成身份/隔离库实测和根代理全量验收分别记录。未运行 Flutter/沙箱UI/真机/辅助技术、正式 IdP/provider、真实用户或生产发布；Closed Pilot / Consumer Beta 仍 NO。此增量不取代原任何发布门槛。


AIR040最终边界补验：Approve/Commit在比较私人摘要或参数前，先通过原真实Session/Task/owner检查；peer、匿名、失效Session和未知审批ID统一拒绝，避免透露猜测是否命中私人内容。当前本人编辑具体版本仍返回ACTION_CHANGED并要求新确认。初次原生错误分类RED在隔离副本记录，原freeze11与后续修复版分开封存。
