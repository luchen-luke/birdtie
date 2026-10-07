# Entity Action Contract

Canonical 增量实现中。继承 V4 ACTN001 与 GLOBAL-UX-INTERACTION-CONTRACT，不增加独立 backlog。

`entity-actions-v1` 是本人当前六闭集动作的可检查提案。普通人类 UI 和本地规则 Agent 读取同一原生来源；它不是 capability、具体版本批准、机器 purpose grant 或提交成功。没有万能动作提交接口。

- CONNECT：明确目标 Person 与申请说明，通过原连接申请接口；已是好友或待处理申请不能再表示可申请。
- MESSAGE：当前 accepted FriendTie 后打开原会话，不能向任意公开 Person 自动发消息。
- SHARE：具体接收会话预览，双方当前源可读，使用原 operation ID/回执；Opportunity 只分享原 Activity。
- JOIN：Activity 参加与 Community 加入/申请分别复用原领域状态、权限与确认，不表示已参加/出席。
- SAVE：原 Save 仅 Place、公开 Activity、真实公开 Community；group 仅原 API 的 Community FK 兼容值，不能把 legacy Group 推成 Community。
- NAVIGATE：仅明确仍公开且有效的实际 point；线上/无点位/Person area 无此动作。

原生使用 PG 时钟和最长 30 秒提案期限；opaque `sourceVersion` 与 `validUntil` 可返回 wire；内部 receipt/seal、source 权限 proof 与原生 xmin 不返回。编码/所有读取之后最后复核当前来源及会话；正常 idle refresh 不冒充新授权。写入仍通过原领域 handler 再鉴权。账号/组织/transport 重绑、迟到响应和到期使旧提案失效。

实施范围、初失败与当前尚未覆盖项见本轮审计。模型、真实 Agent 写入、真人/正式部署/发布证据未提供；生产门槛不因合同出现解除。

## 2026-10-05 本地闭环增量（取代上文实施中状态的范围说明）

只读入口为 `/v1/me/entity-actions/{entityType}/{entityID}` 与显式匿名 `/v1/public/entity-actions/{entityType}/{entityID}`。匿名无 Actor/Session，仅当前公开 Activity/Place 的 NAVIGATE、EXPORT_PUBLIC；携失效 Authorization 的请求不能降级匿名。公开、获准的人类私密活动与机器 PUBLIC ContextBuilder 上限保持分别鉴权。

六 kind 的 `allowedOperations` 为闭集，默认 operation 必须在首位，无空集、重复、未知或跨 kind 操作。邀请可同时明确 ACCEPT_INVITATION/DECLINE_INVITATION；连接可明确 REQUEST_FRIEND/REQUEST_CONVERSATION，不把私信申请变好友。公开 OS 导出属于 SHARE/EXPORT_PUBLIC，由用户选择应用和收件人，不表示已送达；CHOOSE_RECIPIENT 仍复用原聊天 operationID 与未知回执找回。

具体确认按实际操作显示“发送好友申请”“发送私信申请”“确认报名”“确认取消报名”“确认接受邀请”“确认拒绝邀请”“确认收藏”“确认取消收藏”“打开地图”“打开系统分享”。OPEN_CHAT 只打开当前 Tie 的原会话，不自动发消息，不添加机械的多层确认。费用、日期及公开导航点在原详情和 native 同版本读取间检查；来源变化需重新审阅，不能用 opaque hash 当用户读过条款。

### 原事务条件与兼容边界

新界面通过原 Activity RSVP、Saved、Community、人类连接申请及 Friend conversation writer 传三头 `X-Birdtie-Action-Version/Until/Operation`。条件是乐观并发输入，不是授权或新的批准账本。原事务取得目标/会话锁后同 SQL 捕获当前 full closure、精确自身预期变更外的 tail closure 和 PG 最早期限；所有原写入、通知/FK/审计等待之后同事务再验证。错误回滚原效果，任何未知结果不以空白或 timeout 推断成功。

CONNECT 只精确处理原事务到期申请 housekeeping 的原行和新请求 ID；MESSAGE 只排本次新建的原会话行，旧条件不能跨首次会话创建复用。正常重复打开要读新 descriptor，仍返回同会话 ID。Community 接受/开放加入期望 active、申请期望 pending、拒绝/退出期望 left，保持原成员 ID。收藏删除绑定原收藏 ID，不能删除后来新收藏。

原不带三头的领域 API 保持旧兼容；不能把这些调用计为新版具体条件保护证据。SavedPage 删除已不可读内容的本人收藏，是 ownership-only 私人关系清理，不能为提案 source404 而禁止它，也不泄露已隐藏原资料。Activity Plans 是独立本人“个人提醒”，不是 JOIN/RSVP 或 SAVE；typed 活动卡保留添加/移除个人提醒，参加另走原活动详情。该私人资源操作不增六 kind 或机器许可。NewPeople/线上机会等原专用人类 source/回执检查保持，不将回调存在当权限或自动执行。

### 客户端与验收分界

账号、工作区、transport、授权 getter/监听对象重绑与 A→B→A 永久退休原 proposal；普通同源重建保留在途 fence。借用 client 不 close，迟到结果不再读新身份或自动新条件重发。Map/Place 原稳定 provider、边界返回与 MapCanvas 身份保留；Sheet/快捷入口仅修主题语义色，中文、大字、IME 和48dp，不推广新设计。

本任务只作 CODE_LOCAL 实现与可复现测试；实际完整回归/真机由根代理统一登记。真实人、生产业务资料、自动 Agent 写入/模型、原生暗地图、SDK性能仪器/TalkBack不由单测推定完成。Closed Pilot / Consumer Beta 仍 NO。


### 正常取消与错误区分（2026-10-05）

用户点击取消、系统返回或关闭确认框是放弃该提案，不是权限失败，不显示“不支持此操作”。仍保留原403/当前来源失效/具体版本改变与未知提交提示；取消0写，未知不自动重试。仅提示分类增量，不改原领域授权或具体批准边界。


## 2026-10-06 AIR036：只读规划提案接缝

`air.action_proposal.v1` 是受限规划的检索/候选审阅提案，与原 `entity-actions-v1` 六闭集人类领域动作并存，不能转换为 capability、批准或成功回执。P0只可提议 `activity.search`、`activity.detail`、`memory_candidate.review`；真实资源 ID和不透明 `resource_version`绑定原当前来源，内部 xmin/source proof不直接输出。稳定 action ID只在原 logical operation 中寻址，不登记领域效果。

中文说明明确“查看/由你审阅”“不是报名或批准”；候选审阅不接受/拒绝/转ACTIVE、不写 Memory。模型 `confirmed` / `requires_confirmation` / permission/code 均不产生权限；闭集 schema拒绝这些结构字段。若未来037消费提案，仍必须通过其独立确定性许可与原领域当前版本/身份检查，本轮没有工具执行器或跨域通用提交。

普通既有 RSVP/Save/Plans/消息/人审路径、原六动作和原迟到/取消/未知回执规则保留。账号/Session/组织错主体、Source/Task ABA、撤权、到期和 OFF→ON禁止交付旧 native计划；线性化点后变化仍须重新读取，不声称追回已交付内容。适用 UX-CHECK-06至11/16；纯 decoder、合成 adapter、实际 native事务/候选 row+xmin证据独立记录。本任务不提供 Flutter/真机规划UI、正式模型或试点就绪证据。


## 2026-10-06 AIR037：提案、许可检查与确认仍分离

原 `entity-actions-v1` 六动作与既有用户直达路径不变。AIR036 的 `air.action_proposal.v1` 可经原生 `ToolPlan` 检查，只有精确原 Action/operation/参数/来源版本匹配、当前原权限与公开领域 ACL 均有效时，检索工具给短时一次性 Read 句柄。展示 `air.tool_decision.v1` 不是 capability；`air.readonly_tool_result.v1` 使用原真实活动 ID，空数组仅代表真实公开范围没有结果。

`sandbox.write` 的唯一原生正例是本人当前 PreparedGoal 和 044 Prepare 限制下的 `CONFIRM`：具体参数变化会改变摘要与 Decision ID，仍需要本人确认。输出不包含 approved、executed、effect_key，不产生任何领域或沙箱写。元数据列出幂等与对账目前 `UNAVAILABLE_UNTIL_AIR040`；不得将确认信息当作已持久批准或成功回执。未知真实写工具、组织/商业/他人主体、失效 Session、source ABA 或默认 OFF/Observe 均拒绝。

适用 UX-CHECK-06 至11、16。backend 原生事务/权限/真实 wire 核验与纯 schema 测试分开；本轮没有新增 Flutter 提案或沙箱 UI/真机/辅助技术验收。Closed Pilot / Consumer Beta 仍 NO。


## 2026-10-06 AIR040：本人沙箱批准、单次提交与结果核查

原 AIR037 `CheckOwnSandboxTool` 继续只生成 `CONFIRM`，不是批准或执行。新 `agentaction.Service` 通过原 `postgres.Store` 的 `PreviewOwnSandboxAction` → 本人显式 `ApproveOwnSandboxAction` → `CommitOwnSandboxAction` 接入真实原生领域。仅闭集 `sandbox.write.v1` 写入隔离的 `agent_sandbox_writes`，不调用消息、报名、资料、预订、承诺或外部服务。主 HTTP、模型工具入口、provider、客户端未接入；默认 Controller OFF 保持。普通人类 API、原候选/预算/Run/旧037读取边界不改。

097 仅新增三个最小表：不可变审阅绑定、唯一 dispatch/effect 状态账本、真实私有沙箱数据。独立 `OWN_SANDBOX_ACTION` / `sandbox_write` 用途复用原 `consent_grants`；它仍是唯一批准生命周期，不借用模型出口、Task context 或记忆授权。原 064/082/091 `MEMORY_CANDIDATE` 效果账本及守卫不扩权。

绑定包含实际 tenant/actor/subject/agent/Session/Task、logical operation/action、工具版本、目标、完整参数摘要、源 token+xmin、原身份/065策略指纹、独立 consent 版本和期限。PERSON 本人范围的 membership 为明确不适用，不能推导组织权限。批准期限不延长原生目标的30秒上限；过期需新审阅与明确确认。Session/身份/Task/policy/approval/grant 原行锁、原策略 absence 锁和最终数据库时钟将批准消费、dispatch commit 与撤权排序。参数、目标或主体变化不能复用批准。

批准消费只说明 `DISPATCH_COMMITTED`，不表示成功。只有新提交且事务确认返回的非 JSON 进程内 Commitment 能一次 Begin；Claim 绑定同实际 Store/Access/Controller ticket 和持久 fence。恢复进程不能从 JSON 重新构造发送能力。实际效果先在单独沙箱事务写入，最终真实时钟/fence/lease重新检查；收到响应不明或实际写入后的进程退出不会生成第二个发送能力。

`MarkOwnSandboxUnknown` 明确持久 `UNKNOWN_OUTCOME`。`ReconcileOwnSandboxDispatch` 在锁住原 dispatch 后读取真实沙箱效果：有实际 row 才为 `SUCCEEDED`；只有同一原生封闭沙箱内、排他锁与递增 fence 永久封闭旧 Claim 且不存在实际 row 时，才能确定 `NO_EFFECT`。不存在 ID、读取失败、404、批准已消费和 lease 到期都不等于成功或失败；不支持外部工具以 absence 推导结果。已提交动作先于撤权时仍可能在飞，后续只核查实际结果，不谎称撤回。源编辑/撤权后原已发生回执不重写；禁止追加步骤或自动重发。同一 logical operation/action/effect kind 仅一真实 row；两次刻意相同新操作保留各自效果。内容摘要/handler版本不作效果键。

审计复用原 `audit_events` 与 request correlation，只记录实际 actor、闭集决策/状态、用途和原生 ID；不存沙箱正文、Session token/digest、权限指纹、私聊或隐藏思维链。使用后的097 down拒绝丢弃历史；空库 down/reapply 与原001–096数据/xmin/catalog保持分别验证。

适用 UX-CHECK-06/07/08/09/10/11/16。实际原生数据库正负/并发/撤权双序、最后时钟、真实子进程崩溃/新进程恢复、未知提交、SQL篡改和旧回归证据在 `work/outbound-action-safety-2026-10-06` 与 `docs/testing/evidence/outbound-action-safety-2026-10-06`。纯 canonical/schema 测试、原生合成身份/隔离库实测和根代理全量验收分别记录。未运行 Flutter/沙箱UI/真机/辅助技术、正式 IdP/provider、真实用户或生产发布；Closed Pilot / Consumer Beta 仍 NO。此增量不取代原任何发布门槛。


AIR040最终边界补验：Approve/Commit在比较私人摘要或参数前，先通过原真实Session/Task/owner检查；peer、匿名、失效Session和未知审批ID统一拒绝，避免透露猜测是否命中私人内容。当前本人编辑具体版本仍返回ACTION_CHANGED并要求新确认。初次原生错误分类RED在隔离副本记录，原freeze11与后续修复版分开封存。


## 2026-10-06 AGE042 接收人路由条件

原 CONNECT/MESSAGE 提案、条件及领域授权保留。原注册 HTTP 写者接入 `connection.CurrentStore` 和接收人消息策略；可选 `X-Birdtie-Message-Policy-Version` 是来源变化条件，不是许可或确认。BLOCK 不写，SCREEN 只创建同一个原 pending Request、无普通申请通知或自动聊天；ALLOW 只来自 current accepted Tie，旧已同意专用 Conversation 不升级全局权限。旧 EntityAction 描述投影尚未增加098路由展示，消费入口需将当前路由与原提案相交；写者始终再查当前原领域，不能用旧 AVAILABLE 绕过策略。详见 [消息请求契约](AGENT-MESSAGE-REQUEST-POLICY-V5.md)。相关单元通过不替代098/native/界面验收，本轮这些项 NOT_RUN。


## 2026-10-06 AIR019 本人定时汇总（相关单元阶段）

新增本人 GET/PUT `/v1/me/notification-schedule` 和独立 `cmd/notification-schedules`，复用原notification决定、当前来源/偏好和typed Inbox，不改原001–098、活动提醒main/CLI或默认模型关闭。用户明确IANA时区、当地时间、DST缺口SKIP/重复EARLIER_ONCE、静默窗口、类别和滚动24小时实际Inbox触达上限；无配置不建计划，off/会话失效不投递，跨版本/时区改动不清零receipt预算。槽只服务器元数据，不能作为UserQuery、推理或工具批准。

新099只有设置、日槽与交付关联；源当前检查、Session、版本/xmin、最后PG时钟及未知提交去重分别保留。CLI先独立调用原活动提醒，汇总依赖nil/typednil/失败不停止提醒；错误阶段数量UNKNOWN。正常/立即分支仍只有原061谓词，DIGEST只确有原生交付关联才可见。具体契约见 [本人通知计划](AGENT-NOTIFICATION-SCHEDULES-V5.md)。

本阶段只相关单元和静态SQL契约，不能替代原生事实。099迁移/真实PG锁与并发/持久化/重启/原生HTTP、客户端设置/可用性/真机、部署推送/真实IdP和试点均NOT_RUN，建议PARTIAL，Closed Pilot/Consumer Beta仍NO。适用UX-CHECK-05/06/07/08/09/10/11/12/16仅后端范围，原历史验收不覆盖新099。精确证据在 `docs/testing/evidence/notification-schedules-2026-10-06`，状态由根代理更新唯一live队列。
