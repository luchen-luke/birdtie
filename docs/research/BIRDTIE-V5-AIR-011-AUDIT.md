# AIR011 预算与请求前出口审计

2026-10-03；根代理已实际领取 `BT-V5-AIR-011`，worker仅写登记的新包、两个postgres文件、062 up/down和本任务文档/证据目录。来源为现队列完整source及 `work/v5-materials/BT-V5-AIR-BACKLOG.json`，不改旧队列。此文初稿为审计与计划，**尚未实现或测试**。

## 实际基础和缺口

| 能力 | 现代码证据 | 结论 |
| --- | --- | --- |
| 当前Session/Person/PersonalAgent | 原sessions/active accounts/agents/native AgentProfile metadata，058 `lockModelConfigurationTask`及最终PG clock检查 | 可复用；不能用HasActiveAgent布尔补exact Agent |
| Task与源版本 | 原native Task query为本人输入，保留完整row；`QueryVersion(updated_at,to_jsonb(task))`不透明摘要 | 可复用；不称递增业务revision，conversation不得默认发送 |
| 精确配置 | 058不可变prompt/policy/schema/tools/capability制品与Task pre-run绑定 | 可复用；policy SHA不是已执行出口许可；无price表，binding不是Run历史 |
| 现consent_grants | profile_view等真实owner/person资源授予与撤销 | 不能变成provider/地域/保留/费用许可；没有现成MODEL_CONTEXT_EGRESS官方写路径 |
| 普通context/Memory | 人类本人读取/显式Memory与当前context可用，认知port仍Unavailable | 不继承本人读取为机器purpose，零Profile/Memory/聊天/图片自动输入 |
| 066 flags | 当前Controller/Capture/Current、kill/revision | 只收紧，ON不是批准；人类查看/撤销不因OFF失效 |
| 007/008/010 | 默认Service关闭；fake规范化、合成能力路由与有界调用级重试 | 复用合同，不使用OfflineGrant作为真实native事实；全Run预算/恢复仍缺 |
| 当前出口/持久预算 | 未发现真实scope批准、价格、费用预留结算或root_trace共享账本 | 本任务新增真实持久边界；目前仍NOT_IMPLEMENTED |

实际008 canonical为MODEL-CAPABILITY-ROUTING-V5，066只有config/controller没有service.go。初次猜文件路径的只读错误、Windows wildcard path错误不计实现或测试；改以rg文件列表核对。原011 goal中SAF004仍进行也落后于当前DONE依赖，根代理更新记录。

## 最小native许可范围

新范围严格为PERSON的 **SELF_TASK_QUERY**：服务器从真实当前本人ACTIVE Task的query与058中央prompt组装007 Request，绑定exact Session/PersonalAgent/metadata generation、Task QueryVersion、pre-run配置fingerprint、payload摘要、deadline、明确destination/region/retention和价格版本。

用户提供的request/messages或confirmed不成为来源/许可；不能扩大到conversation、Profile、Memory、图片、组织源或任意client Context。已有普通grant/Memory EXPLICIT/066 ON不能批准。Human preview须显示具体query及中央prompt/版本、价格假设、上限、destination及期限，确认消费服务器生成的精确版本；源/主体/版本/期限变更要求重新预览。授权持久记录由真实当前owner动作写入，未运行真实试点用户不称已获生产同意。当前Person开发身份验收是CODE_LOCAL。

新native许可只证明本地领域中具体owner批准及当前性，不批准供应商运营或付费；007生产Gateway仍无provider入口。Organization/Business和其它源默认Unavailable，不以role boolean补造完整resolver。

## 持久预算设计

062新增独立价格版本、预算账户、root预算组、受控Task关联、exact source egress批准、attempt预留/结算与最小审计。预算组是本任务的费用/次数聚合，不是AgentRun/RunStep。root_trace是服务器验证的selector；子Task必须当前同PERSON/Agent并明确关联根预算，不能客户端换root获得新上限，不生成虚构的运行层级。

PERSON v1的tenant principal就是现PERSON命名空间，subject同本人Person；两级预算仍分别检查，不捏造外部租户ID。root与实际Task额度独立，所有attempt（含失败重试）共用根和本人账户的有限上限；root配置固定不被子Task重置。Organization租户预算另待完整原域resolver。

费用采用整数微单位，同一预算货币固定；价格和token/费用上界需要完整不可变精确版本，缺失/过期/未知价格不当0。开发注册的价格是合成配置，不能写成真实收费/供应商批准。输入token上界若缺获准adapter可保证的计数/截断协议，付费live继续Unavailable，不能把字节数直接称token数。

Reserve通过真正事务和固定锁序验证native当前源/身份/批准/价格/config/clock，再原子检查tenant/subject/root/task计数、token与费用并预留。attempt logical operation UUID幂等：重复读取原reservation，不发新call permit；改payload/route/source/amount冲突。计数不因失败/重启或重复settle重置。

RESERVED、IN_FLIGHT、SETTLED、UNKNOWN和明确发送前取消分别表达。未知usage/响应丢失保守持有原费用/token上界，不写paid或退款；已发不能假称“未发”释放。settle需exact reservation/version、真实当前owner或可信后端对账路径、界内完整整数usage；不同结算重复冲突。source/授权撤销阻止后续reserve/dispatch，已在飞只做保守账务状态，不承诺撤回网络数据。

原生Task/metadata删除或重建不能恢复旧批准；计费上界不能随 source cascade意外释放。时间和源均以当前PG clock/真实版本核验，事务固定ReadCommitted/UTC；所有阻塞锁后再检查session/approval/价格/截止。066 ticket不是持久同意epoch，重启不能凭其revision恢复批准。

## 精确scope与顺序

已lease：`internal/modelegressbudget`；`postgres/model_egress_budget.go`与`_integration_test.go`；`062_model_egress_budget.sql`/`.down.sql`；唯一MODEL-EGRESS-BUDGET-V5规范、此audit、新work/v5-air011与独立证据。原058/007/008/010/066/main/server/Memory不写。root预留062，060/061归另两任务。

实施顺序：domain纯数值/版本/状态合同 → 062闭集DDL与非空down保护 → native preview/approve/revoke及准确Task源读取 → 真实持久账户/root/task/Reserve/settle/重验证 → 正负和实库并发 → old public完整行/migration/down/reapply → manifest/证据 → root核证和队列。共享Go全量冻结另行协调，不盲跑别人的半完成DDL。

## 待运行的精确验收

- missing/unknown/过期price、界外token/费用、不同currency、整数溢出；付费live为零出网。
- 真实native本人Task/config创建、预览批准、重新开pool读取，源与同timestamp内容变更、Agent/metadata重建/Session撤销、错owner/Org/其它source、过期锁等待均拒绝，批准不由JSON恢复。
- root与至少两个真实native Task共同额度；并发reserve只有可容纳数量成功；重复attempt不重复预留/释放；换root/price/digest/amount/childTask越权拒绝。
- known usage界内结算、同结算幂等/不同结算冲突、UNKNOWN保留上界、IN_FLIGHT不提前释放；重启持久读取不重发请求。
- 066 OFF/中途kill及native revoke阻止后续请求；gate ON与native批准、预算齐仍不使007 live成为可调用。
- 新表仅保存metadata/摘要/费用，无bearer/APIkey/完整query/prompt/conversation/图片；日志固定reason，redaction canary不泄漏。
- fresh001–062、真实旧058绑定/Memory/source行全部保留、非空down拒绝、空down/reapply；隔离随机库精准清理、scope正负/vet/build与root共同全Go分别计数。

## 实际native3检查点（未完成最终交付）

实际 `work/v5-air011/scope.ps1 -Label native3 -Rounds 2` 在随机自有001–062库运行2轮，各74 PASS、0 FAIL、0 SKIP；scope vet/build各exit0。062 upgrade/空down/reapply全部旧public完整行保留，非空price down实际exit3，fixtures清理后全部public完整行相等，7生产源码SHA前后稳定，owned DB已DROP。仅本项范围，不是全仓回归。

首轮native1真实失败保留：期限从数据库scan后时区表示变化，使精确request digest不一致，native批准步骤全部拒绝；负例没有进入，不能算授权验证。修正内部UTC微秒后native2真实59 PASS，仅Task CANCELLED fixture违反真实原生闭集导致3fail事件；改用真实FAILED，没有更改旧schema。native3完整旧负例及新增SQL防篡改、真实revoke先于reserve barrier实际通过。首个work纯测试gate方法名编译错误也保留，未称功能RED。

native3旧源冻结供根代理001–062共同回归期间，自审实际发现Account/Agent先停用后恢复同一ID、不撤原Session、不改ap的场景可能复用旧批准。work-only `native-suspend-red1`真实复现两种情况均创建reservation(nil error)，原源7 SHA未动；不是fixture定义出的权限成功。保留0 PASS/4 fail事件及精确反例。增入actual account/Agent整row+xmin、排除Session xmin后，work-only `native-authority-green1`实际77 PASS/0 FAIL/0 SKIP；恢复后旧scope拒绝，当前owner能明确创建新scope并批准。

根代理旧源共同001–062三轮各6161 Test PASS、0 FAIL、0测试SKIP、18无测试包另计、全vet/build0，只证明当时native3旧源兼容，不能代替修复后的全仓证据。窗口解除后复制修复及新增native Authenticate刷新、有效request trueGate继续Unavailable和IN_FLIGHT非空ledger实际down拒绝测试；`native-final1`实际生产2轮各78 PASS（39纯域+39 PG）、0 FAIL、0 SKIP，scope vet/build各0。7生产SHA稳定、旧public完整行upgrade/down/reapply及fixture cleanup保持相同、自有库已DROP。根代理新源全仓及独立复核待核证。

011完整标准涉及所有真实请求入口、价格/token上界与获准出口/provider接线，仍PARTIAL候选；恢复要求AIR009/016/018/022及010真实attempt连接。不能以CODE_LOCAL抹掉原source。真实IdP/授权活动/HTTPS/生产地图/运营/A→H等发布条件不变，Closed Pilot / Consumer Beta仍NO。


## 2026-10-05 根实际能力复核（38ct；没有领取或运行新切片）

原队列goal“没有预算/费用预留”与当前062实现冲突，已保留旧goal/partial原文于gap_audit_history，并更正当前事实；唯一状态继续PARTIAL，原source/AC/verify/dependencies及历史证据不变。原native四端口ReserveOwnModelAttempt、BeginOwnLocalModelAttempt、SettleOwnLocalModelAttempt、CancelOwnReservedModelAttempt真实存在，人审UI及preview原ID回执也存在。当前ReadOwnModelEgressReceipt读取model_egress_previews，不是model_budget_reservations原Operation回执；AIR010 OfflineRunner只消费合成ResolveOffline，Service/Gateway真实构造继续Unavailable，当前原083Run处理Moment候选保留许可，不能作MODEL_CONTEXT_EGRESS授权。

后续最小slice计划：复用原062的SELF_TASK_QUERY，补server-only OwnAttemptService与当前本人按原Operation ID只读恢复。只接受Access与原ReserveInput，数据库拼原具体请求，不接受客户端Request/Usage/confirmed；真实native批准和四层预算后，测试可用显式LOCAL_SYNTHETIC transport验证，默认没有transport零dispatch。不新DDL、模型Run、账本、批准UI或模型网络。RESERVED原ID仅能当前原许可Begin一次；IN_FLIGHT/UNKNOWN只能查账/原结算，未知Begin不得重发，未知Reserve先查同原ID；只有明确未Begin可取消，UNKNOWN继续占满原上界，attempt不退。下一次attempt（含retry）需新明确operation和原生Reserve，已有destination只有一个，不扩大fallback目的地。验证需真实native源/Session变更、PG锁后期限、两Task共享root、同ID并发、各边界未知与实际OS重启、同结算幂等/不同结算冲突、defaultOFF/缺price/transport零dispatch及脱敏。

精确潜在范围：modelegressbudget/attempt.go和attempt_test.go；postgres/model_egress_attempts.go和model_egress_attempts_integration_test.go；原canonical与本审计/专属evidence/work。优先在新PG同包复用唯一readEgressReservation及授权函数，未经进一步审计不改原062writer、AIR010 OfflineRunner或083Run/HTTP/UI。现OBS/ACTN仍IN_PROGRESS，先完成当前完整联合回归；该slice尚未start、未测试，不能当实现证据。恢复原PARTIAL时须根显式审计并保留全部旧记录，不能自动变TODO/解除gate。

AGE007同步只读更正：原五态/人工接受真实存在，079/080/082/083现有单来源producer可复用，旧“所有purpose/Submitter不存在”理由过时；但pipeline精确len(ref.Sources)==1且一般SubmitInferredCandidate仍Unavailable。多源独立分析/保留许可与具体候选桥接仍缺、需要共享consent/producer及未来增量schema审计，不能借普通报名/浏览权限或高confidence自动ACTIVE。完整AGE007状态继续PARTIAL。默认模型/Agent真实写/视觉/A2A关闭；Closed Pilot/Consumer Beta NO。未更改外部服务。


## 2026-10-05 单次本地尝试接续审计与交付

本轮仅根显式 resume 的八精确范围（`work/v5-age038-resume/air011-resume-lease-proof38db.json`）：两个新 modelegressbudget 文件、两个新 postgres 文件、原本规范/本审计、专属 evidence/work。原 062 writer/DDL/server/main/HTTP/UI/Service.Complete/083Run 不改；原完整 PARTIAL 对象、source/acceptance/verify/dependencies/gates 保留。

新增缺口已实现：原生 Reserve/Begin/Settle 的单次 OFFLINE_CONTRACT 编排、同 OperationID 最小控制恢复、完成编码后的原生当前批准/来源复核，以及最短期限的 monotonic checkpoint。会计 Settle/Read 不成为答案许可。详见唯一 canonical 的本日接续节。

原证据顺序：pure-red1 undefined 是新增测试编译 RED，没有执行业务断言；compile2 错用 BudgetView.Used（实际 Allocated）为新 fixture 编译错误；pure-target2 gate schema 常量误写为 fixture ERROR。首命令 cwd 在 apps/api，误建 `D:/Project/birdtie/apps/api/work/v5-air011-local-attempt` 空目录，日志重定向不存在导致未启动 Go；该目录零文件保留、不删除，不算测试。之后所有写入用绝对获租 work 路径。

native1=146、native2=157 PASS。native3-forged-red 真正0 PASS/1 FAIL：实际 Preview/Approve/Reserve/Begin/Settle(2/3)后，原新增 release 接受 output999伪造结果，断言失败；旧执行源与 raw 保留。新 buffer 绑定原 operation/request、不可 JSON 重建；native 复核实际结算用量及上界，native4=160、native5=173、native6=180 PASS/0 FAIL-SKIP/pkgFAIL，五命令 exit0，各自帧独立，不覆盖旧轮。最终896源+3seed899稳定、public/catalog/xmin/087往返、owned DROP如实见 native6/result.json。

最终 readonly 下一阶段接口计划（非新任务/实施）：①原 AIR010 的 OfflineResolver 只合成，需 native exact批准/最小源/config/destination/expiry resolver，不能替代062 source proof；②将逐 request/retry/fallback 的预算约束接入真实运行入口，每 attempt 新原 OperationID、Reserve/Begin、合法结算、未知停派；③其它 purpose 的合法出口 resolver 与最后 release，不能把普通 Session 或 HumanSelfReview 作机器许可；④真实 ModelRun/attempt持久恢复依赖尚无本 slice 接线，058 binding/083 enrichment Run 不替代；⑤provider/region-retention/tokenizer/tariff/secret/billing 是独立外部条件。无外部条件的一次本地闭环不等于全 AIR011 source 完成，最终 status 交根核证后仍应如实 PARTIAL。

Source 当前性线性化于持锁的最终 PG SQL；monotonic checkpoint 覆盖后续期限迟到，但不许承诺 Commit 解锁到实际网络交付之间瞬时撤权完全封闭。来源锁等待中 revoke 用例含两类：真实原 Store 在调用间/池等待期间撤回，以及直接合法存储状态模拟在 owner 锁期间变化；后者不冒充原 API 并发批准。OS 子进程两次真实重启仅恢复账目元数据，不能恢复未持久答案；模拟 commit 回执丢失不称真实网络故障。全仓/root独立、真实模型/收费/新 UI/AT/生产均另计；旧 Run UNKNOWN 不改。


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
