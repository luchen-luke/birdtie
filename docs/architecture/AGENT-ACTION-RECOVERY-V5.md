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


## 2026-10-10 本人人类报名未知结果恢复（AIR038本地子阶段）

沿原人类 RSVP JOIN/CANCEL_RSVP writer、当前具体 entity action 批准、Session/活动/来源权限执行；没有启用 Agent 工具或自动报名。客户端在唯一执行 POST 前保存环境/本人/活动/稳定 operationId/动作/摘要/stopRequested 的有限恢复元数据，不持久化 token、批准或正文。保存失败不发送；401/403/404/冲突、超时与坏回包均保留 UNKNOWN，重开仅 GET 原编号，不重发或换键。

117 原生不可变回执与原报名/审计同事务，以本人+operationId 唯一；request digest绑定原活动、动作、工具版本、具体来源版本及UTC微秒期限，效果地址不因内容变化而换键。同键重放只读原历史，后续取消或再报名不会重写旧回执。缺记录不是 NO_EFFECT。用户另行确认“停止这次提交”才允许 close 原编号；它创建永久关闭栅栏或确认已有终态，不取消已有报名。撤去来源后只能读取本人不透露旧效果的 opaque closure；原实际报名状态仍独立刷新。

新增原 /v1/activities/{activityID}/participation-operations 的执行/历史/close/closure四端口；缺 typed store/schema 不退回旧无编号写者。原 POST/DELETE wire兼容保留。JSON编码后分别再走原完整结果或关闭栅栏的当前权限检查；原锁等待后最终数据库时钟和 Session/来源校验保留。客户端账号、组织、来源、transport替换及迟到响应永久退休；来源替换同步释放旧 busy，避免当前页面被旧等待阻塞。

实际最终相关Go90个不同叶PASS，包含64单位和26原生PostgreSQL/注册HTTP；真实socket丢回复、新连接池恢复、100同键并发、两种execute/close锁顺序、117空down/reapply及used-down拒绝均实测。只使用随机自有数据库，九座精确所有权库已清理；原手机库未迁移。Flutter最终同源90PASS；14份受影响文件analyze0，map_workspace的既有20条info/warning未纳入0问题声明。实际native wire原字节被现客户端解析器读取，Mock transport及SecureStorage插件mock不等于真实手机/跨进程存储验收。

API02与Android APK03构建成功、源码帧稳定，未激活/装包；当前手机仍API01/APK02。12张320dp widget截图覆盖中文/混排、浅深色与100/130/200%，保留TextScaler和48dp主要操作。真机RSVP、真实IdP、OS安全存储进程重启、运营恢复、获批Agent消息/报名工具适配器、provider和生产迁移均NOT_RUN或未实现，AIR038整体仍PARTIAL，Consumer Beta/Closed Pilot NO。适用UX-CHECK-06/07/08/09/10/11/12/16。

最终本地证据：docs/testing/evidence/unfinished-completion-2026-10-10/rsvp/root-checkpoint/MANIFEST.json；保留全部早期失败、真实RED与夹具修复日志，不把历史失败计为通过。


### 2026-10-10 同批独立复核修正：停止请求未抵达的恢复死路

初版stopRequested=true后仅允许GET核实；若首次close POST根本没有抵达服务器，持续404会永远UNKNOWN，本机也无法继续报名。这是实际可用性缺口，原封存93之前的90项没有覆盖，保留旧证据不改写。已增加独立人审“再次确认停止提交”：重新打开仍只GET，不自动再次POST；每次新的对话框确认捕获原pending对象和身份/来源代际，控制器拒绝旧对象确认。唯一可再次发送的是同operationId/动作/摘要的close，不是原执行、不换键、不撤销报名、不把404当NO_EFFECT；原117永久栅栏协议不变。

新增反例单位实际90PASS/2FAIL（控制器1次close而预期2；界面缺再次确认按钮），另含新夹具括号info；修复阶段final02测试夹具错误变量closes导致编译失败，保留而不冒充业务RED。最终reclose-final-04同源93PASS、14 scoped analyze0，16张320dp widget截图包括新增浅100/深200再次确认；源码和diff核验通过。RSVP retry构建01 SHA28eec12504719ba327caf2f61aab69213ddd072bda9a76af7e6a492f7deeddb8（283输入/242生产Dart）成功且未安装；原API02与117后端11源逐SHA保持，未重新运行Go或迁移。真实手机/OS存储/IdP/Agent适配器及完整产品门槛仍未齐，AIR038整体PARTIAL。最终增量证据rsvp/reclose/root-checkpoint/MANIFEST.json。


### 2026-10-10 同批报名恢复地址兼容收口

实际复现同一个API地址末尾有无“/”导致本机UNKNOWN恢复记录分裂：HTTP请求地址一致，但地图/对话入口或新界面可能读不到原编号并重新创建提交。三项cross-entry反例全部失败后，controller/Memory/Secure store统一同一HTTP base规范；只去掉单个末尾斜线，不合并不同主机拼写/端口/path，不接受凭据/query/fragment或双末尾斜线。

原两个地址命名空间均兼读，读取不迁移不删旧值；只有完整binding一致才能合并，stop=true优先。新写入仅canonical；终态清除先删所有stop=false再删stop=true，同状态canonical最后，任一步失败立即保留未知状态。冲突、损坏、写入或部分清除失败均阻止新执行，恢复仍只GET原编号。Secure原同isolate串行队列保留，不声称OS跨进程CAS。Memory本轮新static Future队列导致完整widget回归等待未结束，日志按INTERRUPTED保留；删除该多余队列，校验和Map操作无await，未禁用或弱化测试。

最终namespace-final-02：9个相关Flutter测试文件106PASS/0FAIL/SKIP、14份scoped analyze0、注册15源SHA稳定、diff0；此前36直接PASS是同组子集和历史字节，不能相加。16张320dp组件PNG延续已确认规范，root实际看100%浅色UNKNOWN及200%深色再次停止dialog。独立只读复核四源/原日志SHA及状态规则无阻断。RSVP retry构建02 SHA0e506699a858ca4f0d66e3a39f811532ef2f9929177fb2dae1a118916a2f6555（283输入/242生产Dart）PASS、完整构建帧稳定；包含本轮消息请求时间解析和地址修复，未安装。

原117及报名后端11源逐SHA保持，本地址阶段未跑Go/迁移；共享EntityAction此前四态阶段已经改两处SQL，不能称旧90Go为当前全部后端重跑。手机仍API01/APK02，没有117/新报名路由，未将新客户端直接装入不匹配后端。真机报名/IdP/OS安全存储重启与跨进程CAS/获批Agent效果工具/全应用视觉、性能和发布NOT_RUN或未完成；AIR038整体PARTIAL，Consumer Beta/Closed Pilot仍NO。新增付费调用0、未部署、未生成ZIP。最终证据rsvp/namespace/root-checkpoint/MANIFEST.json。
