# 本人渐进完善 V5

2026-10-03。BT-V5-AGE-019，原AGE文档869行。普通Person从Now或设置明确选择一组本人资料，不重新强制填写已完整组。复用015的同一SeedRecord/controller、registered GET/PUT和原子SAVE；018社交偏好入口保留。没有新DDL、server或后端生产方法。

## 中文流程

Now →「完善我的选择」或设置同名入口 → GET本人当前源 →「这次想完善哪一项？」→ 昵称与当前城市、交流语言与私密意图、私密兴趣中的一组 → 检查后保存。一个问题组最多两个输入；兴趣可选，未知城市/意图/语言不默认填写。

完整基础记录只询问所选组。SAVE仍要求昵称、当前有效CITY source snapshot、非空交流语言、真实闭集basicIntent；其它必需组缺失时逐页只补必要组，取消可随时返回且零PUT。该模式不提供DEFER：旧DEFER只保存进度，不会保存未提交草稿；原initial模式仍保留原「稍后」合同。切换选择组会放弃上一组未保存修改，界面说明这一点。

完整review展示所有原子提交字段、原Profile可见范围、当前城市私密声明、交流语言、私密意图、兴趣SET或SKIP；即使复用已有当前CITY，也会沿用旧Seed动作将该声明设为private，预览明确披露。未编辑组采用当前读取值。SAVE不是任意单字段PATCH，也不说未提交草稿已保存。城市是本人声明，不证明居住/到访；英语是交流偏好，不代表开放英文UI。

## 当前源与批准

`profile_completion_choice.dart`只从原SeedRecord计算有限缺项；缺失目录城市即未满足，不用浏览城市/搜索/RSVP/行为或模型推断补事实。计算结果是界面选择，不是许可或源验证器。

`AgentSeedSheet.progressive`复用原AgentSeedController：具体不可变record和opaque expectedSnapshot绑定当前session、Account/Agent/metadata/Profile/private/UserIntent/CITY源，实际Store仍最终核查并原子CAS。未知/撤权/过期/缺能力按原gate拒绝，JSON、confirmed、ON flag不能授权。

409需重新读取；刷新非所选组字段、保留本人所选组草稿，再回选择页并重新检查。其它为补齐而编辑的组可能被刷新，未形成旧批准复用。切换组采用当前源重新开始；相同组保留草稿的SET/SKIP含义。auth/token/account/organization变更和ABA清空record/draft/步骤/批准，迟到GET/PUT和dispose不能恢复旧主体。新初始设置listener也显式观察accountID，原外部签名保持。

5xx/网络/异常回执由原controller标记结果未知并禁止盲重发；GET匹配完整当前值只说「已核实：当前设置与你提交的内容一致」，不证明某个请求提交成功。离开已发的明确保存请求不承诺撤回服务器提交；重新进入读取真实当前值。

## 权限和原文差距

这是普通人类明确选择的逐组完善。没有模型/Agent自动读取行为、自动推断或写入长期字段。原文「Agent自然使用过程中逐渐enrich」仍缺用于自动资料富集的具体用途resolver、当前分析授权/来源与自动消费证据，整项 **PARTIAL**；手动流程具备CODE_AND_LOCAL_VERIFICATION证据。恢复条件是独立真实当前用途与分析resolver、合法source/Evidence和失效处理、具体版本接受/原领域写入以及实际自动消费正负验证；不能靠fixture、ON或本人的普通编辑许可恢复。2026-10-04，033已完成独立 `TASK_CONTEXT_READ` 本地选定源只读链，但其合同明确不授权推断、模型外发、Profile/Memory写入，不替代本项缺失权限；062和007剩余边界保持。

未激活Org/Biz Agent保持原边界，组织workspace不进入个人编辑；普通Profile grant、CITY/成员/好友/社交描述不是认知许可。PrivateProfile metadata的原进版可能使旧bound用途/Evidence/Policy来源失效，不继承旧批准。无生产部署、现实试点或Closed Pilot/Consumer Beta提升。

## UX与验证

沿用Material/forest token、单页可滚动SafeArea、真实ListTile/FilterChip、中文错误恢复和选择语义。UX-CHECK01–14/16覆盖本人结果、当前上下文、Now/Settings同动作、最多1–2问、完整后果、未知保持、返回取消、当前版本、未知提交、语言/大字/IME/异步；UX15完整APK/截图/设备重启由根协调，worker不替代。

新增choice/widget/entry共8测试；定向还复跑原Seed controller/sheet/entry13和018入口1，共22事件。320px/2x文字/viewInsets、选择语义、账号Org/ABA/迟到、取消、冲突刷新非本组、未知提交核实有actual组件证据；MockClient是offline合同而非native身份。

新增PG两矩阵及HTTP一矩阵真实调用原SeedStore/registeredHTTP，计父子6事件；再准确复跑原`TestAgentSeed*`，总38事件，HTTP14/PG24。验证同组修改保留其它私密canary、缺必需值零副作用、metadata来源冲突、当前读取与公开投影、原权限/时间/rollback/CAS/重放。所有身份/内容LOCAL_SYNTHETIC_ONLY。

默认两包并发暴露原HTTP测试的DB宽范围barrier误认另一个fixture；根扩第14scope，只用实际owned metadata locker PID的blocking_pids成员判定修旧测试。权限生产代码/expected503未改，-p1仅诊断，不代替最终默认并发。所有首次失败保留；fresh固定001–067、完整public、源码/schemaSHA、ownedDROP和默认并发最终结果见独占归档。068不在此基线，不声称当前全schema/全Go或race。

## 2026-10-04 身份与接口重绑接续

本切片仍是普通本人资料编辑；实际复用原 `GET/PUT /v1/me/agent-seed`、expectedSnapshot 和同一原子 SAVE，不改后端、DDL 或用途权限。适用 UX-CHECK-01/02/04/07/08/09/10/11/12/14/15/16。

`AgentSeedSheet` 创建时捕获实际 auth、workspace getter/监听对象及原 transport。同 key 更换 auth、client、API地址、监听对象、workspace getter 或编辑模式时，该旧 State 永久退役，清空私密正文、草稿、确认和未知结果，解除实际旧对象监听并 dispose 原 controller；A→B→A 不恢复旧页，也不把新凭据送往旧地址。中文页面说明返回后重新打开；已发保存请求不承诺服务端取消，重新进入仅先 GET 当前设置，不自动重发。正常原 auth 对象的 token/account/workspace 通知继续沿原合同清空后读取当前源，旧响应受 serial 拒绝。Settings/Now 入口的整棵子路由退休由根代理接入并独立验收。

Controller 关闭只发生一次，关闭后 synchronize/load/save 不再通知、恢复记录或发请求；自有 client 关闭，借用 client 不关闭。原 403、409、5xx未知结果、完整后果展示和必要缺项顺序保留。

本 worker 实际 RED 为三项：普通初始和渐进页重绑后重试把新身份凭据送到旧 endpoint，旧私密昵称在同 key 重绑后仍显示。修后六文件目标回归43功能PASS，其中新增17项，五文件 analyze零问题，五源前后SHA稳定；320px/三倍字/240px键盘下，退役说明与48dp语义返回操作可滚动抵达。证据见 `docs/testing/evidence/progressive-binding-human-2026-10-04/` 和 `work/v5-age019-binding/`。这些是 MockClient/widget 本地合同证明，不能称真实 native 身份、真机/TalkBack/重启或自动富集；本切片完整Flutter/build/device由根独立补证。原历史native/migration失败与随后根全量修复分别保留，不能把历史失败报告冒充当前失败，也不把既有9299冻结帧算本切片新增通过。


## 2026-10-05 本人有效记忆 → 单项具体确认 → 原 Profile CAS

以下为原 AGE-019 的增量实现；前文逐组 Seed 编辑和历史验证保持。当前新增原生 `agentprofilecompletion` 人工动作、093 单表与四条本人 HTTP 路由，并接入原「完善我的选择」入口。它让本人复用已确认上下文补一个空项，不要求重填九字段长表。

### 来源、结果与权限

来源只取本人当前 `ACTIVE PRIVATE EXPLICIT PREFERENCE` Memory，六类 `activity_category:<category>` 与原 `Statement(category)`、两项 structured 值精确一致。EXPLICIT confidence=1 是原人工声明真值，不是模型概率；任意 EXPLICIT 私密文本不属于这个白名单。INFERRED、其它类别、未知、未来、已过期、已撤回、跨主体、非规范 structured/source 形状均不能形成可提交草稿。

目标只填空 `preferredActivityTypes`，值为原规范中文声明；已有内容不给覆盖或清除按钮。其它八字段保留原完整值，实际写入复用 `replaceAgentPrivateInTx` 与旧九字段版本 CAS、原 audit。Preview 说明：保存后成为本人新确认的独立资料声明，来源记忆以后到期或撤回不会自动删除它。来源 Memory 不被修改。此项不授予模型、Context/analysis/retention/Policy 或自动机器写入权限，默认机器开关保持关闭。

### 原生动作与一次回执

`GET /v1/me/agent-profile-completion/suggestions` → `POST .../previews` → 显示一项 before/after、来源期限、当前审阅截止与独立声明后果 → `POST .../previews/{previewID}/accept`。`GET .../previews/{previewID}` 仅返回引用/状态/版本，不返回来源或 Profile 正文，不构造批准。

093 的唯一新表保留具体 Memory/完整 Profile/主体/原 Session 绑定与 once receipt；不建第二份九字段资料或机器 grant。Memory 全原生行与 xmin、完整 Profile/metadata 的版本和原生 hash/xmin，以及 owner/Agent 原生身份绑定在同事务复核；原 Session、source、preview 期限在实际 relation/row/audit/constraint 等待后使用新取 PG 时刻末核。重复同 key 不延长期限，100 次和并发重复只形成一次 CAS、一次新增人工编辑 audit 和一次回执。直接 SQL 新表触发器也拒绝非有限来源/Session/metadata 时间，不仅依赖 Go DTO。unused down 原样还原092目录；任何已用预览/回执使 down 拒绝并保留历史。

### 客户端异步与恢复

同 transport/本人/Session/workspace 的具体审阅才能提交；State 的身份或依赖对象变化永久退役，A→B→A 不恢复批准。POST 前 Secure journal reserve/readback 成功后，再次核当前身份与原期限；持久失败/损坏/迟到、未知或错误回执不会发新 POST。本机只保存环境及 Session 哈希、闭集引用/版本/原期限/planDigest，不保存 token、Memory 正文或旧审阅内容。原选择的 source.validUntil 与 Preview 必须精确一致；Accept 回执须精确匹配原 expiry。

关闭重开或新 Session 只 GET 原操作 metadata。PENDING 保留 UNKNOWN，旧审阅正文不存在时只能等待原 deadline 后核实再新 review；404 可能先于原迟到请求，仍保留引用/UNKNOWN，不冒称未生效或自动重发。错误 target/version/权限/格式不清 journal，exact captured deletion 不删除其它 State 的新 pending。COMMITTED 只说明原写入；`CurrentProfileMatches=false` 明示当前资料已变化，不说当前字段仍是旧值。子流程返回原 Seed 页后，只在当前同主体且无 dirty 输入时 reload 旧 Seed/metadata snapshot，防止旧父版本继续提交。

### 当前本地证据与边界

原生 native3：958 API Go/SQL/seed 冻结输入，112 PASS，0 FAIL/SKIP/package FAIL，test/vet/build/两个CLI编译均0；136旧表非空数据/全部xmin/语义目录 up/unused down/reapply 保持，目标后137表旧账本保持；实际主1、PG8、HTTP1 共10库独立 SQL absence。真实 New 路由正负原生验证已包含，叶子 spy 不替代它。

客户端 client-target4：334 Dart/pubspec 冻结副本，29功能PASS + 6loading PASS，0 FAIL/SKIP，11文件 analyze 零问题。覆盖当前 review、持久先于 POST、身份/迟到/ABA、跨 Session metadata、pending/expiry、错误 target/version、双 State、404 UNKNOWN、重复拒绝身份、来源 TTL 扩大拒绝、原父入口 reload、新旧按钮、320/字号3/IME260/48dp/语义。OS中文字体仅用于 widget test render，不复制或打包到 app；截图标为 widget/MockClient，非手机、真实 OS SecureStorage 或 TalkBack。

证据：`docs/testing/evidence/agent-profile-memory-completion-2026-10-05/`；完整旧失败与冻结源仍在 `work/v5-age019-memory-profile-completion/`。native1 HTTP响应契约 FAIL/PG未使用import编译 FAIL、native2 nil成功映射造成12业务FAIL、客户端入口缺失/ORG重入/404误删/TTL扩大真实RED，以及复制/后处理/渲染 fixture harness失败和21条lint info均分别保留。没有改失败断言为宽松通过。

此段只记录本 worker 的当前定向证据。根联合默认全Go、完整Flutter analyze/test/build及最终原AC状态由根独立核定；手机断开，真机/OS SecureStorage/真实辅助技术未运行；生产部署、Closed Pilot、Consumer Beta 均未升级。未校准概率或 provider 不是这项确定性本人动作的依赖。
