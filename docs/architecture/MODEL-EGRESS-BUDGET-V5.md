# 模型请求批准与预算 V5

2026-10-03。唯一canonical；AIR011源要求与审计见 `docs/research/BIRDTIE-V5-AIR-011-AUDIT.md`。本地领域及062真实验收已通过，完整需求仍 **PARTIAL**，不称真实模型/收费/Run已实现。证据在 `docs/testing/evidence/model-egress-budget-2026-10-03`。

## 当前可实现范围

独立 `modelegressbudget` 与 `postgres/model_egress_budget.go` 使用真实原生Session、active Person、exact PersonalAgent、已有metadata、ACTIVE Task和058固定pre-run绑定。没有另一本Memory、虚构AgentRun或客户自报的许可。

只支持 `SELF_TASK_QUERY / MODEL_CONTEXT_EGRESS`：服务器读取本人Task的query，添加真实不可变中央prompt。不自动发送conversation、Profile、Memory、图片或其它人的资料。PERSON v1的租户命名空间和subject均是当前本人Person；两层预算分别检查。Organization/Business尚无完整出口resolver，继续拒绝。

预览返回精确request、prompt/schema/config版本、合成价格版本、provider/model/wire、地域、保留策略、货币、整数费用/token上界、期限及digest。`Preview.Display`仅生成可检查的只读展示；Approve仅消费数据库中的ID及用户展示的exact digest，并重读当前来源。JSON构造、066 ON、普通Profile grant或Memory EXPLICIT不能批准。2026-10-04 已增加四个人审 HTTP 入口（见下文），仍没有 Flutter 人审入口；这些领域动作不能称消费级用户 UI 已完成。UX-CHECK-01/03/05/09/10/13的移动端与辅助技术验收未运行。

批准绑定实际Session ID、exactAgent、Task QueryVersion、account/Agent/metadata当前row的真实generation摘要及不可变配置。摘要由三个真实保留row及其xmin派生，不是伪造epoch或业务revision。Session xmin刻意不纳入，普通Authenticate idle refresh不会废掉本人当前批准；Session ID、撤销和期限仍必须实时有效。Task/会话/主体/metadata变化、重建、期限过期或撤销后禁止新的Reserve/Begin；不会续租旧预览。实际账号或Agent先停用再恢复同一ID也需要新scope和明确批准。撤销及账目查看/结算只要求当前合法owner会话，以便源删除或关闭开关后仍能处理账目。

## 062持久数据

| 表 | 内容 |
| --- | --- |
| model_local_price_versions | 不可变且明确LOCAL_SYNTHETIC的价格和上界假设；没有真实供应商收费证明 |
| model_budget_accounts | TENANT_PERSON、SUBJECT_PERSON不可重置额度和累计尝试 |
| model_budget_roots | root_trace费用聚合组及当前根Task/来源固定版本，最长15分钟 |
| model_budget_tasks | 实际同owner/Agent的Task明确关联及其额度；这不是Run子任务执行历史 |
| model_egress_previews | 最长2分钟的DRAFT、APPROVED、REVOKED；只存版本/摘要，不存完整request |
| model_budget_reservations | 全局operation UUID的RESERVED、IN_FLIGHT、SETTLED、UNKNOWN、CANCELLED_BEFORE_SEND |
| model_budget_audit | 受控owner/root/op及白名单decision；没有正文、密钥、思维链或原图 |

`RegisterLocalModelPrice`是未暴露的可信服务端维护函数，不是模型工具。价格、原额度和selector不可变；重复同版本必须精确匹配。不存在缺价格默认0或自动提升额度。日期统一UTC微秒后生成精确请求摘要。

## 事务与失败语义

每次事务显式READ COMMITTED，不继承连接池默认REPEATABLE READ。原生来源、Session/account/Agent/metadata用当前SQL与SHARE锁保证保留row当前性。owner advisory transaction lock把本人所有root/child账目串行化，四层额度使用真正FOR UPDATE行锁和原子事务；不同owner相互隔离。预算组身份为不可变元数据读取，避免持有SHARE锁后等待owner锁再升级的死锁。

Reserve先核精确批准、当前source/config/price，再同时检查四层次数、输入/输出token及整数费用上界；任一超额整笔回滚。operation UUID重复读取同一reservation，改变selector/digest冲突；读取旧reservation不返回新的执行权。Begin再次核当前准入并仅允许RESERVED→IN_FLIGHT一次。当前出口为本地合同且执行状态固定UNAVAILABLE，未连接真实provider。

所有attempt都递增次数，失败、发送前取消和重启也不能重置。未知结果保持原完整上界，不能假称paid/未发送/已退款。只允许RESERVED明确发送前取消；IN_FLIGHT不支持伪装取消。LOCAL_SYNTHETIC的完整normalized usage可进行本地账目估计并释放差额，UNKNOWN可之后精确结算；重复同一结算幂等、不同结算拒绝，尝试次数不退回。这些是本地合成费用估计，不能当供应商真实计费凭证。

当前数据库时间在所有潜在等待后再检查Session、request、root、price期限；066 ticket最后再检查，只收紧。撤销先于Reserve提交阻止新reservation；先提交的reservation随后撤销仍无法Begin。已在飞账目可以结算，不增加后续步骤，也不保证收回网络请求。此实现没有网络请求。

## 正式出口仍缺的条件

默认及true gate的Service仍复用007不可调用供应商的Gateway。尚无实际批准的provider adapter/secret注入、可保证输入token上界的计数/截断器、正式价格/区域/保留证明和付费账目对账。全部价格Evidence仅LOCAL_SYNTHETIC，缺任何项都不启用paid live。

尚未与真实Run/Step恢复账本、010所有retry/fallback入口、ContextBuilder的其它合法source/purpose接线。实际更多来源、组织租户与机器目的必须分别核准，不能继承SELF_TASK_QUERY批准。原验收要求“每次模型请求”在未来真实入口未接线前只PARTIAL；配置存在或合成测试通过不能抹掉未实现部分。后续恢复依赖AIR009/016/018/022及对应域授权，并需网络边界的真实证明。

新migration只在自有随机库验证；非空down拒绝、空down/reapply及完整旧public保留是交付要求。没有在生产执行down，Closed Pilot和Consumer Beta依旧NO。

## 实际证据与未测部分

生产 `work/v5-air011/scope.ps1 -Label native-final1 -Rounds 2` 实际两轮各78 PASS（39纯域、39 PostgreSQL）、0 FAIL、0 SKIP；scope vet/build各0。全部旧public完整行在062 up/空down/reapply保持相同；非空price down实际exit3；native IN_FLIGHT账本在真实down脚本下拒绝且账目保持；owned fixtures清理后全public完整行保持、自有DB已DROP。7生产Go/SQL SHA前后相同。

原始失败未覆盖：最初gate方法名compile失败；native1时区造成精确批准误拒；native2 fixture使用不存在Task CANCELLED状态；后发现account/Agent停用恢复复活旧批准的真实RED。修复后含此前全部测试、同源RowGeneration恢复负例及新scope正例，普通Authenticate refresh、valid request trueGate关闭网络均实际通过。work overlay GREEN和旧源共同三轮6161不能代替当前生产证据。

根代理当前新版完整Go回归与独立复验待核证，完整实际结果单独归档。没有Flutter/真机界面、外部网络/计费、IdP生产或现实试点验证。

## 2026-10-04 增量：本人模型预览人审 HTTP

本节接续原 PARTIAL；没有把价格配置、具体批准、预算预留和执行合并成一个“已完成”状态。仅复用原062账目和批准，不新增DDL、授权或Run账本。实现位于 `apps/api/internal/httpapi/model_egress_budget.go`，根代理在现有 `server.go` 注册。

| 方法与路径 | 精确输入 | 本地结果 |
| --- | --- | --- |
| POST `/v1/me/model-egress/previews` | `rootTraceId/taskId/priceVersion/maxOutputTokens/deadlineAt` 五键 | 原生 DRAFT 的精确本人 query + 中央 prompt；具体 digest、目标、期限、合成费用上界；`modelAccess=UNAVAILABLE` |
| POST `/v1/me/model-egress/approvals` | `previewId/requestDigest` 两键 | 当前本人 Session、Task/Agent/metadata/来源仍匹配时 APPROVED；不预留预算、不执行模型请求 |
| DELETE `/v1/me/model-egress/previews/{previewID}` | 无 body/query | 当前 owner 撤回；REVOKED 不复活；源已变更或替换会话仍可本人撤回 |
| GET `/v1/me/model-egress/roots/{rootID}/tasks/{taskID}/budget` | 无 body/query | 原生 TENANT_PERSON、SUBJECT_PERSON、ROOT、TASK 四层只读账目 |

入口要求真实 Bearer Session 与 PERSON；拒绝组织 workspace header（包含空值）、Organization/Business、匿名、过期/撤销会话、跨 owner、重复/大小写别名/缺键/null/客户端 source 或 confirmed。拒绝 RawQuery 和尾部 `?`。输入 body 使用现有严格 JSON 限额；未知错误只返回通用错误码。成功和失败均 no-store；不返回 SourceToken、AuthorityToken、Session digest 或原 conversation。

外层人审 DTO 使用明确字段；内部 `request` 继续使用007机器契约，`destination` 保留既有 Key 的 Provider/Model/Version/WireContract，不另建配置契约。`request.run_id` 是058的固定 pre-run binding，不是 AgentRun 已运行证明。预览和批准 Evidence 仅 LOCAL_SYNTHETIC；费用和token上界仅为可信本地合成配置，不能称正式价格或provider tokenizer结果。缺port返回503；原生缺price/root/binding与当前来源拒绝保持062的403，不造免费默认值或假配置。

原价格注册、配置激活和额度配置仍是未公开的可信维护原语。没有用户可发现的 root/price/config 选择流程；本轮 HTTP 不等于消费级人审 UI。无 Reserve/Begin/Settle、价格/Quota管理、Runtime或收费路由；人审批准不会创建 reservation 或调用 provider。

### 等待后期限与真实失败链

新 registered HTTP 测试发现原 Preview 在 native source 检查后等待 owner advisory，再 INSERT 时，已过期的预览被 `expires_at > created_at` CHECK 拦截，导致503；虽然没有泄漏或残留，但错误分类未按原明确过期拒绝。根代理在已登记的062源范围增加 **owner锁后、INSERT前的原 egressFinish**，保留 INSERT 后末复核及原 CHECK；本轮继续断言实际 PostgreSQL advisory 等待、403和零预览残留，未弱化测试。

人审 fixture 使用各自独占 fresh001–076 数据库与明确本地种子，避免与 postgres 包的全局配置 route 单例争用。保持默认 Go package 并发，未使用 `-p1` 掩盖问题；每个子库实际 DROP。原失败、源快照和结果保留在 `work/v5-air011-resume/native1`、`native2`。

native3 与冻结 native4 均 **110 PASS / 0 FAIL / 0 SKIP / packageFail=0**，704个API/SQL源稳定，全部旧public表行不变，外层及子库清理成功。适用 vet/build exit0。详见 `docs/testing/evidence/model-egress-budget-resume-2026-10-04/README.md`。根代理本轮全仓核证另记录；本轮未运行 Flutter、真机、AT、付费或生产出口。

### 仍需恢复

AIR010全部 retry/fallback 的原生逐次预留/Begin/结算、实际Run016与恢复、其它具体 Context 出口目的、中文人审 UI 仍未完成。真实 provider、approved region/retention、正式价格、tokenizer与付费对账仍未齐备。AIR011 完整需求保持 **PARTIAL**；Closed Pilot / Consumer Beta 保持 **NO**，不能因四个HTTP入口或本地合成验证解除AGE035的依赖门槛。

## 2026-10-04 接续：本人只读选择与原回执界面（局部核验完成）

在原062模型账目和四个人审HTTP动作之上，根代理已注册三个只读入口：`GET /v1/me/model-egress/options`、`GET /v1/me/model-egress/previews`、`GET /v1/me/model-egress/previews/{previewID}`。选择来自当前本人已有Task、root、固定binding/config、LOCAL_SYNTHETIC价格和四层额度；没有配置时显示真实空态，不接受任意UUID或自动创建配置/额度。列表最多20个选择和50条原回执，原ID直接读取不受历史列表窗口限制。

Settings“模型请求与预算”接入同auth、client、API base、workspace监听。人审页分开具体预览、确认本地许可、核实原记录、本人撤回和已有预算；未知结果先读原ID，创建预览不具幂等键时不盲目再POST。换Session、Task/来源或Agent metadata改变后，旧预览正文与批准不可继承；独立本人撤回仍需当前Session鉴权。GET不批准、不续期、不预留、不调用provider。

原生定向125 PASS/0 FAIL-SKIP、vet/build0；客户端定向18 PASS/analyze0。根代理合并冻结批次Go9299 PASS/0 FAIL-SKIP/pkgFail、test/vet/build0，727源稳定、完整旧public行/catalog保留、独占DB已DROP；Flutter741功能+98loading PASS、analyze/test/Debug build0、222源稳定。当前3153文件worker档案及原始失败已独立核SHA；根冻结1051文件manifest `3c9a51373be908f0f5efc3ec131a443905038a25643886f2b02bcdfcb755a12d`，见 `docs/testing/evidence/notification-destinations-2026-10-04/root-code-final2/manifest.json`。此帧不覆盖后续AGE043和商家界面新代码。

a438 Debug APK实际安装c641566b，保留数据、使用现有自有076本地API且功能默认OFF；Settings进入和明确刷新各两个原生GET200，显示无配置/无历史的真实中文空态，五个本人模型ledger均0且前后完整字节SHA相同。真机具体批准/撤回/未知结果恢复正例因无配置Task/root/价格/额度而NOT_RUN，没有制造配置或打开模型；TalkBack和真实部署未测。`work/v5-age038-resume/phone-current2/actual-parallel-acceptance.json`记录实际范围。

AIR011完整需求仍PARTIAL。真实收费、模型出口、Run及所有retry/fallback逐次账目、其它合法目的、原完整依赖和AGE035门槛未解除；Closed Pilot与Consumer Beta仍NO。适用UX-CHECK-01至16，规则/测试或本地空态不能替代完整可访问性与运营验收。


## 2026-10-05 接续：单次本地尝试与原操作恢复（CODE_LOCAL）

新 `modelegressbudget/local_attempt.go` 的 `LocalAttemptDriver.Once` 复用原 062 的 Reserve → Begin → Settle，且只接收独立 `modelgateway.OfflineHarness`。它没有接入 HTTP/main/Service.Complete/LiveGateway，也没有创建 083 AgentRun；058 binding 的 Request.RunID 仍不是 ModelRun。这里只运行明确的 LOCAL_SYNTHETIC / OFFLINE_CONTRACT 合成适配器，无模型网络、真实收费、自动 Memory/工具写入或新的 grant/账本/DDL。原 human preview/批准与 UI 保持原样，默认 OFF 不调用适配器且不预留。

### 原 ID、账目与结果权限

`postgres/model_egress_local_attempt.go` 新增 `ReadOwnLocalModelAttempt`，从原 reservation 读最小 operation/state/UNAVAILABLE 控制元数据。当前本人会话允许账目恢复，即使原 Task/Agent/批准已失效，也不会恢复旧查询、结果、原 digest、来源或价格正文。恢复 IN_FLIGHT/UNKNOWN/SETTLED 不再次调用适配器；未知 Reserve/Begin/Settle 不自动新建 ID、重发或退款。只有明确确认原状态 RESERVED 后，调用者可在原未过期条件下显式继续原操作；不是自动恢复任务。Begin 成功而回执丢失仍为 IN_FLIGHT，不假称未发送取消。取消复用原 `CancelOwnReservedModelAttempt`，不新增另一套状态机。

Settle 仅是会计控制，不能作为私人答案释放许可。`ReleaseOwnLocalModelAttempt` 在结果完整编码之后，在原生事务内重读原 owner/Session、APPROVED preview、Agent/Task/058 binding、实际 source/authority generation、固定配置、合成价格、原 root/期限；匹配实际原 reservation 的用量与上界。结果 buffer 只由本进程驱动生成，绑定原 OperationID 和精确 Request；buffer/Outcome/checkpoint 不可 JSON 重建，另一 operation/请求不能复用。它仍不是 grant，普通 Session owner、JSON、结果 provider 字段或 rollout ticket 均不能批准私人结果。

最终 SQL 以同一次 PG clock 检查原 Session，取 absolute/idle、root、price、固定 preview/request deadline 的最短值；Task/config/Agent 在本范围没有独立自然 expiry，未发明期限。该 query 发出前取得 server monotonic 锚，以剩余 PG 时长形成只能收紧的内部 checkpoint（query 往返延迟保守扣除）。原来源共享锁持有至事务结束；这是 source 当前性的原生线性化点，不承诺抵抗 Commit 解锁以后、网络交付以前的任意瞬时撤权。实际 Commit 返回/调度迟到时，Driver 仍核原 operation/request checkpoint、旧 gate ticket、context 和原 Request deadline；不重新 Capture 来续期。最早 Session idle 到期的返回延迟已实测零 EncodedResult。

### 本轮实际证据与限制

`work/v5-air011-local-attempt/native6`：180 PASS 事件、0 FAIL/SKIP/package failure，test/vet/build 与两个 CLI 构建共五项 exit 0；896 API 源与 3 原 seed 的 899 不可变执行输入全稳定。完整 public 行/catalog、原 Participation xmin、087 unused down/reapply 均保持，自有 `birdtie_air011_211c5b3755bb` 实际 DROP。`source-freeze1.json` 固定四文件 SHA；根独立定向/整仓结果另计，当前尚未宣称新整仓通过。

覆盖真实批准 helpers、KNOWN/UNKNOWN 用量、8 并发同 ID 只有一次 dispatch/占额、两 Task 共享 root ceiling、普通 Authenticate idle 刷新、OFF、跨 owner、不同 request/operation/Agent/Run、不同合成 destination、合法原 Store 撤回、Agent/Task ABA、owner 锁的具体 pg_blocking_pids、pool wait、原 Session/preview 自然期限、gate OFF→ON、context、未知提交、界外用量保守 hold。两个独立 OS test-exe 子进程只读同 SETTLED ID，原 row 与 xmin 不变，不重发或恢复答案；是本地仪器恢复，不是 App/生产重启。未知 commit 回执丢失通过真实原生提交后测试 port 丢回执模拟，不声称真实网络故障注入。

真正 native3 功能 RED 是旧新增 release 接口接受 output=999 伪造结果；原 raw/失败帧保持。修复为 server-only bound buffer 与原实际用量匹配后通过；旧 raw byte 接口在这四个新文件内已替换，062 原 writer 没有改动。新增测试 undefined、BudgetView 字段误名、gate schema 常量误名是编译/fixture诊断，不当业务失败；首次 cwd/日志失败单列 HARNESS。UX-CHECK-06/09/10/16适用私人结果/具体批准/未知控制/数据最小化；本轮不改 UI，移动/AT/真机新入口/外部部署未验。

### 完整 AIR011 尚未覆盖的内部接口与外部条件

内部仍需：AIR010 OfflineRunner 的 synthetic OfflineResolver 不能充当 native authority，需每个真实 request/retry/fallback 的原生 exact purpose/config/destination resolver及原批准期限/撤回闭包；每次实际 attempt 必须独立原 OperationID 的 Reserve/Begin/Settle，未知先停而不是再派发；其它 Context 出口目的需合法最小源与末释放接口；真正 ModelRun 生命周期及原 attempts/root 恢复关联需另接，不能借 058 binding 或 083 enrichment Run 冒充。当前没有把 Once 安装到真实请求入口，因此单次 helper 不代表所有请求已计预算。

外部仍缺 approved provider、地域/保留条件、真实 tokenizer/正式价格、服务器 secret 注入与供应商账单/交易凭证。付费 LIVE 与实际模型网络继续 UNAVAILABLE；LocalUsage/LOCAL_SYNTHETIC 金额不是真实支付。完整 AIR011 仍 PARTIAL，不解除原依赖或下游门槛；Closed Pilot Ready / Consumer Beta NO，旧 Run after_commit UNKNOWN 原因与证据未改。


## 2026-10-05 增量 freeze2：调用前目的地与最短期限

freeze1/native6 的实现只在结果返回后核对 provider/model。真正 `native7-descriptor-red`（4 PASS/1 FAIL）证明：传入 model=different 的合成适配器仍收到原 Task query（calls=1，期望0），最后拒绝答案不能替代调用前保护。旧执行源、原日志、freeze1 和 worker-final1 均保留。

当前构造 `NewLocalAttemptDriver(port, LocalAttemptAdapter, gate)`，不再接受先前的任意独立 harness。新本地 adapter 声明含真实 Descriptor 与 wire/region/retention；这里的“声明”只指 LOCAL_SYNTHETIC 测试设置，不是已核验供应商能力、地域或保留证据。固定 wrapper 构造原 OFFLINE_CONTRACT harness，避免两次抓取不同 Descriptor；构造时、实际委托前和委托返回后重新核对 underlying 声明。typed nil map/func/slice/chan/pointer/interface 在读取 Descriptor 前拒绝。

原 Begin 确认后，`CheckOwnLocalModelAttemptDispatch` 在原生事务内核对原 IN_FLIGHT OperationID、APPROVED preview、owner/Session、Task/Agent/绑定/来源/配置、原精确 Request、价格及 wire/region/retention。没有新预览、grant、账本或路由。最后 PG-clock 查询返回原 Session absolute/idle、root、price、原 request/preview 最短期限的私有 checkpoint。真实返回延迟跨过原 Session idle 时零 adapter calls；不会重新 Capture gate 或以新批准复活旧尝试。声明 getter 可能等待，所以 wrapper 在 getter 之后、实际递交 query 之前再次核原 monotonic checkpoint、旧 gate 与 context。源当前性的线性化点仍是持锁的原生 SQL；不能保证事务 Commit 解锁到本地调用之间任意瞬时源撤权完全封闭。不是已安装生产模型出网边界。

调用后的原编码、实际结算用量与 native release 路径保留。调用期间 Descriptor/destination 变化则丢 raw，原 UNKNOWN 控制账目继续持上界，不假退款或重新发请求。调用前失败也不将原已确认 IN_FLIGHT 冒称未发送取消。

`native8` 使用真实自有 fresh087：194 PASS、0 FAIL/SKIP/package failure；test/vet/build/两 CLI 五项 exit0，896 API+3seed 的899执行输入稳定，完整 public/catalog/xmin/087往返保持，`birdtie_air011_163384af89ed` 实际 DROP。验证包括构造 Descriptor ABA、不同 model 0calls、wire/region/retention 不匹配0calls、Begin 后原 API revoke/query变化/descriptor变更0calls、短 Session checkpoint 返回迟到0calls、调用中变化零结果、原单次/并发/账目/OS恢复全部原回归。freeze2 SHA `b965d974cd1ece0f110ca1bce02fd49ec094575a60702c51bbdf9a183b10d366`；source-before SHA `33ddec6660eb8780228d4832f9ad31fb7296130eba38106190dd112cce4134e7`；result SHA `30e0541bdace4784c739bacf020702203f473c6127e434c28270c5eaf88181dc`。根独立 targeted/whole 后续结果单独记录，不用 native6 或旧全量冒称当前通过。

完整 AIR011 保持 PARTIAL：Once 接口可以在同一个仍有效 APPROVED preview 下，以不同原 OperationID 对每次尝试分别 Reserve/Begin/Settle；这不是已接入的 retry/fallback 编排。Reserve/Begin/Settle 端口错误或 UNKNOWN commit 不得仅按错误类型重试；usage/cost UNKNOWN 与 dispatch UNKNOWN 必须分开。下一本地阶段需要只有本进程 adapter 明确 RATE_LIMIT/TEMPORARY 且合法结算确认后签出的 exact-op 失败分类，受限 retry 新 OperationID，fallback 另行原具体目的地 preview/批准；其它 purpose、真实入口与 ModelRun 原源接线仍缺。实际 provider/付费/地域保留/供应商账单另外缺；默认关闭，无外部模型/自动写/新运行授权。Closed Pilot/Consumer Beta NO；旧 Run UNKNOWN 未改。


## 2026-10-05 增量 freeze3：原生有限 retry / 事先批准 fallback

本段后来状态替代上段“下一本地阶段未实现”的当前判断，旧文字、freeze1/2 与 archive1/2 保留为历史。根代理已独立核验 freeze2 全 Go 10414 PASS/0 FAIL-SKIP/package failure、test/vet/build/两 CLI exit0，899 执行输入一致，完整 public/catalog/xmin087往返保持，自有库独立不存在；证明 `work/v5-age038-resume/air011-whole-root38dq.json`。这个结果不代替本段新源码验收。

### 当前实际接线与边界

- `NewLocalRetryRunner` 只接真实 `LocalRetryPort` 和原 066 Controller，持整轮首次 ticket；MaxElapsed/父 context 在任何 adapter getter 或 PG capture 之前生效，首次核验等待计入整轮。服务器本地步骤使用已有原 Preview/Approve + 各自不同原 OperationID，不自动批准、新增价表、替换 fallback B 或续签期限。
- `CaptureOwnLocalRetryPlan` 在原 PG 事务内先核完整已获准 A/B：同本人 owner/Session、原 Agent/Task/root/source/authority/config，允许不同已明确批准 PriceVersion/digest/destination。原 request/preview、root、价格与 Session absolute/idle 取最短期限；sealed native proof 不可 JSON 重建，普通 Recover/control metadata 不授权继续。每轮保留实际 Task xmin，恢复可见 query/updatedAt 字节不能复活已捕获计划。它不扩展原 062 pre-preview 契约，也不声称任意管理员改回所有安全字节的检测。
- 每个实际 attempt 仍用原 062 Reserve/Begin/Settle，四层预算原子占额。整轮 proof 进入实际 `onceWithTicket` 的 `CheckOwnLocalRetryDispatch`；IN_FLIGHT 原 operation、精确 request/destination 与完整 A/B 来源同事务重查。wrapper 最后 underlying getter 之后再次执行全计划 native beforeCall，返回 checkpoint 只能收紧并传入实际 delegate context。调用中合法 idle refresh 不能延长已捕获短 context；delegate 返回后、取消自身 context 前核期限，迟到 raw 直接丢弃。
- 成功答案保持单次 native Release，完整编码完成后又对原完整 A/B plan/current ticket/deadline 终检，B 未调用也不得已撤回后释放 A。账目 Read/Settle 永不替代私人答案许可。原 source 锁及最终 PG clock 是持锁 native 线性化点；不声称事务解锁到实际本地调用/网络交付之间任意瞬时撤权完全封闭。
- `LocalRetryFailure` 只在本次 wrapper 真正 delegated、实际返回 RATE_LIMIT/TEMPORARY、最终 Harness 仍是同一合法 retryable 分类、声明未变、原 Settle 确认、原 context/checkpoint/ticket 有效时签出。严格 Request clone 失败不能签凭据；分类/Outcome/proof 都不可 JSON 重建。端口 Reserve/Begin/Settle 即便返回同型 ProviderError 也不能续发；超时、取消、拒答、声明变化、畸形 RetryAfter、未知 dispatch/commit 都停在原 ID，不盲重发。
- 明确合成失败的 usage/cost UNKNOWN 仍保留完整 token/费用上界并计 requests；这是保守账目，不等于 dispatch UNKNOWN。只有前述内部凭据经 `CheckOwnLocalRetryFailure` 原生复核原已完成操作与完整原计划后才能等待；等待前后均核撤权/来源/期限。延迟复用 AIR010 的 RetryDelay、007 RetryAfter，抖动使用闭区间随机单位，可测试替换；无切换预算时保留旧 AIR010 合法同路线重试。没有把新的 native 方案插入旧 synthetic OfflineRunner 或 OFF 的 Service/LiveGateway/HTTP。
- destination/region/retention/价格均 LOCAL_SYNTHETIC adapter 声明和测试资料，不是已核验供应商合同、地域或收费事实。无新增 grant、效果台账、DDL、机器能力、永久 Memory、ModelRun 或模型网络。UX-CHECK-06/09/10/16继续适用；本轮无 UI 改动，手机/AT/生产不新增通过证据，原 Run UNKNOWN 未解释或改变。

### 真实失败、修复与最新验收

原 raw/执行源均保留，PASS 事件包含父/子而不是独立业务或真人数量：

| 原轮次 | 实际结果与原因 |
| --- | --- |
| retry-pure-red1 | 新增 undefined 编译 RED，没有执行业务断言。 |
| native9-retry-plan-red | 0 PASS/2 FAIL 两独立叶：B 撤回仍释放 A 656 bytes；capture200ms 未计入 MaxElapsed150ms，calls1。修为全计划成功末核与整轮早捕获期限。 |
| native10-retry | 217 PASS/2 FAIL事件，Task ABA 一个叶＋父。修为原运行计划保留 Task xmin；无放宽原 058/062 测试。 |
| native11-retry-dispatch-red | 0 PASS/1 FAIL：outer Revalidate 后撤 B，A 仍 calls1。修为全计划进入实际 dispatch native事务。 |
| native12-retry | 230 PASS/1 FAIL纯用例：非法 Request 夹具缺完整 schema/Agent 等；严格 parser 拒绝而 clone 忽略错误。改为合法 007 Request 的输入/返回深复制及 malformed不能签凭据，原 parser不改。 |
| native13-retry-getter-red | 0 PASS/1 FAIL：初次 native dispatch 后 getter 撤 B，A 仍 calls1。修为最后 getter 之后的全计划 native beforeCall。 |
| native14-retry | 231 PASS/1 FAIL：已为 calls0，但 Harness 把 beforeCall native拒绝规范为 UNKNOWN，与原期望 ErrDenied不符。无delegate且结算确认后保留 native精确拒绝，不签retry。 |
| native15-retry | 100其他包PASS、0叶FAIL、PG编译/pkgFAIL、vet1：新增 fixture误用不存在 ErrUnauthorized；原062会话失败就是 ErrDenied，按原分类修。不是业务失败。 |
| native16-retry-delegate-deadline-red | 0 PASS/3 FAIL事件（两叶＋父）：adapter刷新 Session 后跨短delegate期限，rateLimit A1/B1/656bytes，success A1/B0/656bytes。修为 delegate 前后实际短ctx/deadline核验，迟到丢raw/不签retry。 |
| native17-retry | 最新240 PASS/0 FAIL-SKIP/package failure；test/vet/build/两 CLI五项0。原所有单次、HTTP预算与新retry均实际通过。 |

新 native17 使用 fresh087，900 API 源＋3原seed共903 不可变输入稳定，owned八源live前后相同，完整 public 行/catalog、原 Participation xmin、087 unused down/reapply 保持；自有 `birdtie_air011_ceca07153474` 实际 DROP。本轮新增原生 full-plan 独占PGpool等待后 revokeB/Task ABA/Session自然到期拒绝；4并发同原plan只一次实际调用/请求占额；明确两次失败再成功、RetryAfter至少100ms、原四层上界 held、budget exhaustion0后续、已批准不同Price/digest B可用/未批准A0B0、backoff撤权/Task/Agent/gate ABA与自然 Session期限、typedport错误不签凭据、whole capture期限、最后 getter撤 B零调用、短context合法refresh迟到无结果/无重试、不同Session/owner原回归及两个实际OS child原操作只读恢复。

八源清单 `work/v5-air011-local-attempt/source-freeze3.json`；不可变执行帧 `native17-retry/source`，manifest SHA `e3c75dd332bb429b12686e5581649ee892b035d93973808873200de70714ecef`；freeze SHA `fae9a00bd12472b02a03962c3fcb6636e155598e3bf33ac6368cf1be570e7811`；result SHA `cf3cbfb711a66a1e8cfc7ceb9bf8da7c490b95e6e8e9bdd8bb0600a0b9df7803`。重现命令：bundled Python `work/v5-air011-local-attempt/verify-native.py --round <新目录名> --pattern '^TestModelEgress'`。当前仅定向真实原生验证，不冒称整仓；根代理正在独立核当前 frame，whole PENDING 单独追加。

### 原完整 AIR011 下一内部接口及外部条件

本地有界原生 retry/fallback helper 已实现；不能继续把这个已实现部分说成仅缺外部 provider，也不能说已接全部模型请求。真实 Service/LiveGateway/main/HTTP 尚未接入这一原生 coordinator：需要精确原 purpose/config/source/destination resolver 与具体人审批准绑定，把实际 request admission / 每次 retry/fallback 的原 Reserve/Begin/Settle 接入同 round ticket，并保持未知原ID恢复。本人 SELF_TASK_QUERY 原062来源仅原 query，不等于允许其它私人 Context 出口；各其它合法 purpose/组织租户必须先审当前原源 resolver与最小输出/末释放真源，不能用普通 Session owner授权私人答案。需要真正 ModelRun/Step 与原 attempts/root/已提交状态的持久关联及恢复接口；058绑定和083候选Run不能充当 ModelRun。范围需 root审计后增量登记；未实现接口仍属内部能力缺口，不自动解除下游。

外部另缺 approved真实provider/tokenizer/正式价格/region-retention证据/服务器secret/供应商账单对账；真实 paid请求、地域保留声明、实际真人/生产运行仍 NOT_RUN/UNAVAILABLE。完整原 AIR011 继续未完成（任务仍IN_PROGRESS，由root依据全原AC收尾），未称 DONE；Closed Pilot / Consumer Beta NO。此前旧 Run after_commit UNKNOWN 仍保留，不据本轮绿色推定因果已修。


## 2026-10-05 增量 freeze4：跨尝试和单次末释放保留曾见短期限

freeze3/native17 240 PASS 与 archive3 保留为前一检查点，根尚未对该帧做整仓，因为后来实际新增了未覆盖的最短期限负例。不能以旧绿色抹去本段真实 RED：

- `native18-retry-observed-bound-red`：0 PASS/1 FAIL。首次 dispatch 后 Session idle收紧300ms；beforeCall核到短期限；adapter在短ctx有效时立即合法idle refresh延长Session并明确RATE_LIMIT，旧 Runner仍等待400ms再调用B，实际A1/B1/656bytes。已修为 `LocalAttemptOutcome.monotonicDeadline` 私有时钟元数据，初dispatch、每次 beforeCall 和 Release只收紧；Runner每次Once返回就收紧原整轮deadline。不是新增许可，不重建proof、不借刷新续期。
- `native19-retry`：242 PASS/0 FAIL-SKIP/pkg0、五命令0；903输入稳定/public/catalog/xmin087保持/DROP。之后另发现原public Once兼容路径也需核相同末短界，没将19称最终验收。
- `native20-once-bound-release-red`：0 PASS/1 FAIL。原dispatch Session400ms短cp，adapter合法idle refresh至长期限、立即成功；原Settle已确认后本地回执延迟500ms，旧 Once重读较长Session释放656bytes（calls1，nil error）。这是本地端口返回延迟测试，不宣称真实网络Commit故障。
- 当前 Once结算确认后以及末native Release后都核保留的原最短monotonic界；迟到不签失败凭据/不放答案。原已确认SETTLED、原OperationID和四层requests1仍保持，不假退款、不重发；Runner对后续步骤只收紧，不可延长。这个私有Outcome不支持JSON，也不代表private answer authority；全部源/原批准/Session末核仍由原native事务裁决。

最新 `native21-retry-final` 实际243 PASS/0 FAIL-SKIP/package failure；test/vet/build/两个 CLI构建共五项exit0，900 API源＋3原seed共903冻结执行输入和live源一致，八owned文件稳定。完整public行/catalog、原Participation xmin和087 unused down/reapply保持；`birdtie_air011_f9de1bbfdf2b`实际DROP。范围为fresh087定向原生，根独立target与整仓当前frame尚待核，不能冒称全Go已通过。

最终8源码 `work/v5-air011-local-attempt/source-freeze4.json` SHA `c23e8a0b9311a3f623ba4bb352497ce465e201f4d2b1ce6ae518e50449860627`；不可变执行frame `native21-retry-final/source`，source-before SHA `9969d4033f754ecd1bcdf768e7b4fb5f52d65d12412ab299c95a9de6cd8d9b67`，result SHA `486d6cd2a6cf3f0efbad000ae060e0d37d0aeda7ce81912f81edc808221cfc10`。新archive4保留18–21全部失败/通过执行帧，旧archive1/2/3、freeze1/2/3、native9–17均不覆盖。

完整 AIR011仍未完成：本次本地native retry/fallback是真实原062每次占额/结算，未安装到所有真实模型请求入口、其它合法purpose/组织租户，未建立真正ModelRun/Step与attempt/root持久关联。外部真实provider/tokenizer/正式价格/region-retention/secret/供应商账单另缺；原LiveGateway/HTTP/Service.Complete/main仍OFF，没启用机器或模型网络。原Run UNKNOWN保持原记录；Closed Pilot / Consumer Beta NO。下一内部phase由根原AC审计与精确扩lease后实施，不能因为本段helper绿色将原任务标DONE或解除下游。


## 2026-10-05 增量 freeze5：standalone 与 whole-plan 的 getter 后准入一致

freeze4/native21 的243绿、archive4保留为历史检查点，根尚未对该帧做整仓；后续新增 standalone 调用前 getter 负例，不能用旧绿色当该窗口已覆盖。

`native22-once-getter-red` 真正0 PASS/1 FAIL：初次 `CheckOwnLocalModelAttemptDispatch` 返回后，arm 原 LocalDestination getter；getter通过原 `RevokeOwnModelEgress` 合法撤回原preview，仍返回原声明。旧 standalone beforeCall仅核 checkpoint/gate/context，adapter实际calls1；末native Release虽ErrDenied挡住答案，却无法阻止已经递交query。原903执行输入稳定、public/catalog/xmin087与down-reapply保持，自有库实际DROP，raw和源码不覆盖。

最小修正是统一使用当前已选定 native check，在 wrapper最后 underlying getter之后再次执行原生准入：standalone复用原 `CheckOwnLocalModelAttemptDispatch`；retry复用已绑定完整原计划的 `CheckOwnLocalRetryDispatch`。两者同样核当前原OperationID/IN_FLIGHT/精确Request/destination/原批准来源、原Session与最短期限，返回checkpoint只能收紧前述私有deadline/实际delegate context；没有新授权、通用效果账本、DDL或ModelRun。源当前性仍是持锁native SQL线性化点，不宣称最后SQL Commit解锁后的任意瞬时撤权可撤回已经发送的query。

最新 `native23-retry-final` 实际244 PASS/0 FAIL-SKIP/package failure，test/vet/build/两个CLI五exit0，900 API＋3原seed903输入和当前owned八源稳定；完整public/catalog、原Participation xmin、087 unused down/reapply保持。自有 `birdtie_air011_092dfd247fb9`实际DROP。新standalone getter撤原preview已实证adapter0calls/ErrDenied，四层原保守requests1保持；原所有单次、retry/fallback、HTTP预算、短bound跨尝试/末释放、同plan并发及pool/source/Session负例继续全部通过。

最终八源 `work/v5-air011-local-attempt/source-freeze5.json` SHA `3a2d99e7d3dcba403dbc646687d01ba1fbcd45ebc1d807b0ef681da309eb4acd`；不可变frame `native23-retry-final/source`，source-before SHA `698a08f35ee024f00ed0bbec83fdc401b7dc2b0b8f7e8482aa51ed088a2b9e90`；result SHA `30a85288932b4436d903f8689c7217ab31dac684632d8aea5db313c8ee219149`。新archive5保存22RED与23完整当前执行帧，archive1–4、旧freeze和所有原失败保留。根当前frame独立target/whole仍PENDING，不以旧freeze2 10414 whole PASS冒称最新通过。

完整AIR011仍未完成；真实request入口/其它合法purpose/组织租户/真正ModelRun-step与attempt持久关联仍是内部工作，provider/tokenizer/真实价格/region-retention/secret/供应商账单另外是外部条件。原main/HTTP/Service/LiveGateway未激活，UNKNOWN只原ID控制恢复，不自动批准、续期或派发。旧Run UNKNOWN未解释/修改，Closed Pilot与Consumer Beta均NO。八Go已停止修改，由根核证安全检查点后才继续下一阶段。


## 2026-10-05 增量 freeze6：Now HTTP 原生回归使用真实独占目录


根独立 `root-retry-independent1` freeze5 的244定向 PASS已确认。其后整仓 `root-retry-whole1` 实际10463 PASS / 1 FAIL / 0 SKIP / 1 package failure；test1，vet/build/两个CLI0。唯一叶子 `TestNowSelectionHTTPNativeRegisteredOwnerModesAndWire` 首GET希望200实际409 `now_context_selection_changed`，fixture residue0；903输入及完整public/catalog/xmin087往返保持。证明 `work/v5-age038-resume/air011-root-retry-whole-red38ej.json`；这些旧whole失败与 archive1–5/native23均保留，不改原记录为成功。具体首次变化的源行未记录，仍 **UNKNOWN_NOT_LOGGED**。

原 positive test使用共用目录，而原Now receipt包括所有纳入当前公开City的真实版本；新已登记的唯一测试修正用既有 `modelEgressHTTPOwnedDatabase(t)` 建随机独占库，实际当前87migrations与原3devseeds，再复用原 `contextBuilderHTTPNative`。不放宽原200/409/400/403/503、本人ONLINE/CITY、跨owner、组织workspace、closedwire、缺port或撤回来源断言，不使用串行包并发/首409即通过/盲retry。

增加确定性 registered native barrier：另一个本人未选择的实际公开City/Context确实在GET200选项内；原Gateway.ReadOptions完成后、HTTP编码与末Revalidate之间修改它的名称/真实xmin；GET必须409，且不返回旧公开标签、本人private ONLINE key、optionsToken。该负例实际通过，证明正确完整目录回执拒变机制，**不等于已定位旧whole是哪行并发变动**。

最新 `native24-now-regression` 在fresh087实际 276 PASS /0FAIL-SKIP/package failure；完整 `^Test(ModelEgress|NowSelection)`，test/vet/build/两个CLI五exit0。900API+3原seed=903执行输入和9owned源稳定；完整public行/semantic catalog、原Participation xmin、087 unused down/reapply保持。主owned库 `birdtie_air011_249223deea40` actualDROP；Now独占child `birdtie_owned_egress_http_6f6d0d762688d60f87c114d0` 创建/实际migrations87/DROP日志均保存，runner独立 `pg_database` 查0。未对一般会话安全、机器purpose或模型出口作改动。

freeze6 `work/v5-air011-local-attempt/source-freeze6.json` SHA `1f0a211e2297354995e061958da83d23cff843843761a3db7e6a55f1cb87a371`；当前不可变frame `native24-now-regression/source`，manifestSHA `0ed96a5df3f850dd059637827901a6cf87ab136e6c750c29f3509f3cebc1376b`，resultSHA `c280c820ad863fddffd6603ed6592ff27db1e210e29f5cd223411dbba7b6d947`。八Go保持freeze5原字节，只新增已租Now test版本；旧测试原字节及rootwholeRED proof副本在 `now-regression-before/`，新worker-final6另建不覆盖归档1–5。根新frame完整独立target/full **PENDING**，本次定向通过不替代whole。

下一真正ModelRun阶段方案只在 `work/v5-air011-local-attempt/NEXT-MODEL-RUN-IMPLEMENTATION-PLAN.md`；现未grant新范围、未修改062writer/DDL/HTTP/UI/ModelRun。完整AIR011未完成，runtime入口/其它合法purpose/组织租户/真正Run-Step关联仍属内部工作；provider/tokenizer/真实price/region-retention/secret/供应商账单另缺。原Run UNKNOWN未解释或改变；Closed Pilot / Consumer Beta NO。


## 2026-10-05 ModelRequestRun 最终目标帧补记

原 AIR011 继续，不新增批准、费用账本或机器用途。原 Request.RunID 仍是 058 binding ID，实际 ModelRunID 分离；088 两表仅保存原 ID/版本/主体和闭集执行状态。Reserve/Begin 与 Step 状态同事务；原 Settle/Cancel 的账目和关联 Step 同事务；外层唯一 Commit 后才返回 Request。原 066 ticket 不重新 Capture，旧短期限只收紧。

最新 `work/v5-air011-local-attempt/native29-run-waits`：schema88，316 PASS/0 FAIL-SKIP/pkg，test/vet/build/两个CLI 退出码全0；908 API+3 seed 的911份执行输入与 live 逐字节相同。原 public/catalog/xmin、088 unused down/reapply 保持，自有 `birdtie_air011_dee21b320267` 已实际 DROP。`source-freeze7.json` SHA256 = `32ed6ea3cafbb77bf066d28ffdb28fed3dbce42a5a98593fb50bb63089bd3efa`。真实命令见该轮 `commands.json`、`cli-build-command.json` 和 process receipts。

新增实际核验包括：跨 owner 反向 OperationID 规划仅一组成功且无部分账目；pool 等待中的 Task ABA、B 撤回、Session/lease 到期、Run 取消和 context 取消拒绝；Reserve/Begin 原 audit INSERT 等待后 Session 到期整事务回滚，原 budget/reservation/audit/Run/Step 全行及 xmin 不变。native26 已通过四个真实 OS kill 点（reserved/begun/returned/settled）及新进程仅原 ID 控制读取；恢复不会重发或重新生成私答。并发 088 down 等 Create 提交后拒绝删除新历史。

历史准确分类：native25=297全绿；native26=305全绿；native27=308 PASS/8 FAIL事件，六pool叶子及父例误等待只有 acquire 完成才更新的计数，另跨owner fixture错误使用全局配置 revision0，均未执行相应业务负向断言；native28=146其他包 PASS/PG编译失败（新增fixture漏 modelcapability import），不是PG业务测试失败。修正后 native29=316全绿。原帧/raw全部保留。原 SQL NULL CHECK 表达式反例是根独立 TEMP事务证据；新完整088 NULL拒绝实测，down竞态原版本没有实际完整RED，不能反向声称。

明确成本未知只保守持原上界；它单独不是继续许可。唯有实际本次规范 RATE_LIMIT/TEMPORARY、声明/原请求仍一致及原结算确认后可签私有失败。dispatch/commit未知、端口同型错误或迟到均停止；Control/Recover/账目不能授予私答释放。完整覆盖和逐源差分见 `work/v5-air011-local-attempt/MODEL-RUN-STAGE3-COVERAGE.json`。

当前只是原 CODE_AND_LOCAL_SUPPORTED_SCOPE，最终 fresh088 全仓由根独立运行，未提前标 DONE。仅本人 PERSON scalar ActivityQuery / SELF_TASK_QUERY 可用，其它未获明确 native 目的直接拒绝；AIR009/AIR018 下游不能倒置为本需求前置。未单独执行 FINISHED metadata-trigger 晚变负例；普通 Release 等待、Reserve/Begin audit 等待已测，不宣称所有可能等待矩阵穷尽。末次持锁 native SQL 是线性化点，不保证 Commit 解锁后任意瞬时撤权能追回已交付内容。真实provider/tokenizer/地域/收费/生产、模型网络与新Run消费UI均未运行；既有中文062人审路径不变，原083 Run UNKNOWN 不变，Closed Pilot / Consumer Beta = NO。
