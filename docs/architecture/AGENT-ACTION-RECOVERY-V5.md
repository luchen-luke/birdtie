# 本人沙箱动作回执恢复（BT-V5-AIR-038 增量）

本文件说明原 AIR040 的恢复补口，不重新定义批准、效果账本、外部工具或发布门槛。主状态仍在原队列。适用 UX-CHECK-06/07/08/09/10/11/16；本轮仅源码与相关单元验证。

## 已复用的权威链

原 `agentaction.Binding`、`Canonical`、`EffectKey`、097 的批准/dispatch/私有沙箱数据、`consent_grants` 及永久 claim fence 保留。效果键仍由 tenant、logical operation、action 与 effect kind 派生；正文摘要和工具版本只绑定批准，不改变效果地址。新代码没有消息、RSVP、公开活动、provider、模型或网络调用，也不新建 DDL、批准或账本。

原流程是 Preview → 本人具体版本 Confirm → Commit → 非 JSON Commitment/Claim → 私有沙箱效果 → 原生 Reconcile。被消费的批准、提交回执、在飞和未知结果不能视为成功，也不能重新构造执行能力。

## 补齐丢失提交响应时的地址

Commit 成功或结果不明后，调用方可能没有收到服务端随机生成的 dispatch ID，但已经展示并持有原 approval ID。旧 `Reconcile` 只按 dispatch ID 读取；再次 Commit 并不是恢复协议，且旧批准到期、来源修改或撤权后不能借此重新发起动作。

新增 `Service.RecoverApproval` 只调用可选 `ApprovalRecoveryPort.ReconcileOwnSandboxApproval`。真实 `postgres.Store` 先在原事务中验证当前 owner/session，再以 `approval_id AND owner_id` 查找**已有** dispatch；之后进入原 `actionReadDispatch` 的真实效果读取、排他行锁、永久 fence、最后 session/controller 与提交边界。已提交原操作允许在当前本人有效会话下核查，包括原批准或源已变化；这不会恢复旧批准，也不授权新步骤。

查询无行、数据库错误、取消后迟到结果或非法 receipt 地址保留 UNKNOWN。没有 dispatch 不等于 NO_EFFECT。只有进入原封闭私有沙箱的排他效果检查、永久封闭旧 Claim 后，原协议才能认定该已存在 dispatch 的 NO_EFFECT。此规则不能移植到远端消息/报名工具：远端未查到对象、404 或租期结束不证明未发送。

恢复只返回原 `Dispatch`；不产生 Commitment/Claim，不调用 Confirm/Commit/Begin/Execute，不重发、不自动重批，不更改其他 action 或效果。旧按 dispatch ID 的读取/标未知/核查路径不变。没有可选恢复端口的旧实现返回不可用，不假造成功兼容。

## 服务边界修复

已实际复现原 `Service.Reconcile` 在 typed nil port 下调用导致 panic，以及 nil/已取消 context 仍到达 port 的三项单位失败。现在所有原 Preview/Confirm/Commit/Reconcile 与新增恢复共用提前校验：nil context → 参数无效；取消/期限 → 原 context 错误；nil/typed nil port → 不可用。校验只阻止无效调用，不授予业务权限。当前 source/session/controller、批准摘要和原生最终时钟检查仍属于原 port。

## 证据与未完成范围

`docs/testing/evidence/action-recovery-2026-10-06/README.md` 保存精确命令、目录、退出码、首次失败及最终源码。最终相关单位39 run/pass、13顶层、2包、0 fail/skip。它覆盖服务转发/取消/不可用、稳定回执读取不重发、原生地址 SQL 参数和原真实收尾的静态接入；port/Tx spy 是单位夹具，不能称为数据库或远端已成功的验收。

原 AIR040 历史 native 证据保留，未沿用为本次新恢复路径通过。新路径的真实 PostgreSQL、锁/并发、重启持久化、HTTP/客户端恢复入口与真机均 NOT_RUN；主 HTTP/模型未接入该沙箱恢复 API。外部写工具、远端效果查询、RSVP/消息的批准绑定适配器及 UNKNOWN_RECONCILE 运维消费仍未实现，AIR038 保持 PARTIAL。全量、vet、build、migration、seed 不在本轮执行。默认特性/模型出网/自动写关闭，Closed Pilot / Consumer Beta NO。


## AIR038 本人沙箱回执核实 HTTP 消费者（2026-10-07）

新增本人 `POST /v1/me/agent-sandbox-approvals/{approvalID}/reconcile`，只接受原批准地址，无正文、query、组织工作区或客户端 feature override。它复用原 `Service.RecoverApproval` → `Store.ReconcileOwnSandboxApproval` 与同一个启动 featureController；原本人 Session、原生 owner/fence/最终钟和默认 OFF 不变。采用 POST，因为原核实可能推进已有 receipt/fence；没有注册 Preview/Confirm/Commit/Begin/Execute，也不发送外部消息/报名、重做原效果或恢复旧批准。

返回 <=8192 字节完整 `data` wrapper：固定 schema、本人、原 approval/dispatch ID、原状态及提交时间，成功时原 effect ID/发生时间。该历史视图不含 Binding、正文、Session、authority/grant、效果键或执行 handle；它不是新批准。编码后再次验证捕获的原本人 Session，并检查取消。缺地址、未知提交或错误不能推导 NO_EFFECT，也不自动重发；NO_EFFECT 只可来自原私有沙箱排他核实/永久 fence。已核实原历史不撤回已消费内容。

主应用仅增量构造 gateway 并作为可信 httpapi Option 注入，未更改开关。新注册 HTTP、domain DTO 与 gateway/原 Service 转发相关单元 20 unique 父子事件（6 顶层）有通过证据：首次路由缺失404为有效行为 RED；units02 整命令失败，原因是新 HTTP fixture 缺原 AuthenticateHumanSocial 方法，原失败保留。修正仅该 fixture，http03 的17 HTTP事件通过；02中3个 domain/adapter事件未重跑。详见 `docs/testing/evidence/action-recovery-http-2026-10-07/freeze05/MANIFEST.json`（README相对 docs/architecture 文档路径需以仓库根解析）。

以上仅合成 transport/port 单元；主应用 main.go 编译和启动、PostgreSQL/097/锁/重启/持久化、真实身份、手机、客户端恢复入口、外部消息/RSVP写工具、运维和整仓/vet/build 均 NOT_RUN。原39单位没有重跑或当本接口通过。AIR038整体仍 PARTIAL，Closed Pilot/Consumer Beta NO。适用 UX-CHECK-06/07/08/09/10/11/16，不新建批准或恢复权限体系。


## AIR038 本人人类私信结果核实消费者（2026-10-07）

本增量复用原 Friend/Tie/Conversation 与 SendMessage 事务，不启用 Agent、模型、消息工具自动写或外部发送。用户原私信界面明确发送，先将环境、本人、会话、操作编号、正文摘要和固定工具版本的安全元数据写入本机恢复记录；不持久化正文、token、批准或执行能力。写前记录失败不发送。关闭后新界面只能读取元数据并用原操作 GET 核实，不恢复原正文、不自动重新 POST/换编号；404、拒绝或超时仍为未核实，不认定 NO_EFFECT。

新增 `POST /v1/me/conversations/{conversationID}/message-operations` 和 `GET /v1/me/conversations/{conversationID}/message-operations/{operationID}`。105 增量允许原 `conversation_messages.client_operation_id` 绑定人类普通消息，并以本人/操作编号唯一约束。原消息行就是不可变效果凭据，原074触发器同样保护正文、会话、发送者、ID、时间和操作编号；不在发送后另开事务登记凭据。目标会话和正文摘要绑定原操作，固定工具版本不改变效果地址。新边界只接受规范小写 UUID，与原 domain/数据库检查一致。

同一次操作重复调用返回原消息元数据，不再次写消息、通知或配额；改正文或合法目标会话拒绝。原当前 Person/Session、双方关系、阻止、接受状态、接收策略、账户配额、源锁与单末钟全部保留。响应编码后再次进入原完整消息读取栅栏核本次来源和 Session；之后取消拒绝输出。拒绝迟到回执不撤回已经提交的消息。客户端成功核实只表示原消息已经存于会话，不表示对方已阅读，也不声称该 GET 是本次请求的因果回执。

恢复存储在同 Dart isolate 内跨实例串行，清理必须匹配原具体元数据；不宣称 OS 跨进程 CAS。原旧无操作编号发送、实体分享和群聊路径保留。真实本机安全存储持久性、操作系统进程重启、设备/IdP/性能/部署仍 NOT_RUN；平台方法模拟与 Memory store 只属单位夹具。

相关证据见 `docs/testing/evidence/air038-human-message-recovery-2026-10-07/MANIFEST.json`。有效 RED 是原 Page 回执丢失后二次点击产生两 POST，以及旧服务未注册新路径404。随后 12 直接 Flutter 行为通过；domain/注册 HTTP/SQL spy 单位与专属 PostgreSQL 001–105+开发 seed 原生验证另列，不能混作同一次整组通过。实际验证同事务单消息/通知、跨连接重放与两连接池并发、原消息 rows/xmin、105 无使用 down/reapply 和有数据 down 拒绝、原不可变触发器、阻止与旧 Session 回执拒绝，以及真实提交后 ResponseWriter 丢回复再 GET。原生数据仍是本地合成数据；独占库已移除并 SQL 核无残留。一个 PG 命令期间 peer 旧测试文件变化，所测产品/本测试稳定而完整编译帧不稳定；原证据保留不改。

全量、分析、构建、真机与生产环境本轮未运行。RSVP、获批 Agent effect-key 工具适配器及其它 AIR038 子项不属于此人类消费链；整体 AIR038 仍 PARTIAL，Closed Pilot / Consumer Beta NO。适用 UX-CHECK-07/08/09/10/11/12，不新建权限或恢复批准体系。


### 同批收口补注

发送 journal 读取失败不再阻断原受鉴权消息历史 GET；仅禁用新发送并保留“恢复记录暂不可读取”的反馈和已有未知引用。该原路径先取得真实 GET=0 的 RED，再以唯一相关 widget GREEN 核原历史可见及零 POST。此前 12 行为与这 1 行为属于不同阶段，13 unique 不表述为同一次最终整套重跑。

105 往返测试最终复用原 `ownedMigrationDatabase`，在独占 child 上执行，避免根全量跨包的 parent runtime 被降级。原声明 rows/xmin 断言保留并增加原消息表完整 column/constraint/index 定义比较；新 catalog SQL 首次多一个括号的42601为夹具错误，保留后精准修正，两受影响子场景与 parent 共3事件实际通过。专属 parent 及两次 child 均用 SQL 核不存在。新文案与语句样式后续如有改变，根整合检查另记，不沿用为该最终字节版已全面通过。


### 本轮整合补充：消息历史与恢复存储分离

本机恢复记录失败或尚未完成，均只限制新发送并提示刷新核对，不阻断已受原权限校验的消息历史 GET。历史和恢复记录分别读取，恢复结果仍受本页身份、来源与请求代际约束；未完成时不显示空恢复列表，不删旧引用、不改用内存存储冒充生产持久化。

精准未完成存储 RED 的历史请求为零，最小修复后该单元历史 GET 一次且零发送；读取失败分支也有独立 RED/GREEN。根 client02 完整检查的29个旧页面 error 和13条新样式 info 原样保留。本轮仅修原两文件的跨 rebuild 稳定元数据 store 夹具，以及真实普通消息操作接口的闭合 receipt 回包；34个旧行为单元通过，原页面权限、身份、来源、ABA、迟到、实体分享、布局与借用/自有 transport 断言保留。13条样式修正只补必要括号、弃用参数名和测试函数声明，不全文件格式化旧产品源。

12个新增行为在此前 journal03 版本通过，后续读取失败、读取未完成各一条精准单元通过；14个唯一新增行为是分阶段证据，不声称最终源整套14单元已重新执行。最后 syntax-only 改动在34旧行为执行前完成，最终整合分析/全量/构建由根代理独立安排；本片不把之前整合失败记作成功。105往返测试现运行在其自身独占子库，used-down拒绝和原 rows/xmin/catalog 保护保留；子库和父库实际不存在验证均保留。AIR038全任务仍 PARTIAL，RSVP、获批Agent效果适配器与真机/部署门槛不变。
