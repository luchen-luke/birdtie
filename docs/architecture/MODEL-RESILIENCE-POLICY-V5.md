# 2026-10-05 根代理当前 089 联合验收摘要

根代理已完成 BT-V5-AIR-010 原 CODE_AND_LOCAL_VERIFICATION 范围验收；任务状态以根代理维护的唯一 live 队列为准。本摘要不替根代理修改状态，也不代表真实供应商或发布就绪。

- 当前 schema089 默认包并发完整 Go：10,652 PASS，0 FAIL / TEST_SKIP / packageFAIL；test、vet、build、两 CLI 均退出 0。
- 根代理独立核验 921 份当前/冻结执行输入（918 API 文件 + 3 原 seed），48 行旧 public 全部行内容及 xmin、semantic catalog、089 unused down/reapply 和测试前后快照一致。
- 根代理对实际记录的 245 个 parent/子测试数据库独立 SQL 核实不存在。证据：`work/v5-age038-resume/air010-air049-current-whole-root38he.json`。
- 本 worker 原 native4 的 xmin 仅覆盖 Participation；上面的完整旧 public xmin 来自根代理当前 089 联合回归，不能倒写为 native4 原 runner 的能力。
- 历史 PARTIAL、native1/3 初败和 root whole2 的 FK / 077 审计 TRUNCATE 兼容性失败均保留；当前 whole3 通过不抹去旧日志或归档。下面全部旧文档字节原样保留为历史与阶段记录。

唯一新增 native 测试 Go SHA 仍为 `57084b2bac96d6a796d2403662d1b37d0b48e5313dd2ddbe789118909aa30b12`，本次只追加文档摘要。原 worker-final1、source-final1/2、annex2 和所有执行帧不变。真实 provider / SDK / tokenizer / 供应商地域及保留 / 生产费用 / 真机089 / 辅助技术 / 生产调度均 NOT_RUN；默认模型网络 OFF，Closed Pilot / Consumer Beta 保持 NO。

## 历史正文与阶段证据（完整保留）

# 2026-10-05 当前审阅摘要

BT-V5-AIR-010 本地原生 AC 故障矩阵已实现并完成定向核验，根代理同当前 schema 的联合整仓验收仍待核；这里不声明任务 DONE。已复用 AIR011 实际 062 四预算、066 原 Ticket、088 ModelRun/RunStep，以及 AIR027 固定配置版本；没有新建权限、计费或运行账本。

- native4：376 PASS、0 FAIL/SKIP；41 个新增矩阵测试事件，test/vet/build/两 CLI 均退出 0。915 个执行输入为 912 API 文件和 3 个原 seed。
- 原四包 pure-current1：530 PASS，作为兼容性证据，不与原生测试相加为独立能力。
- fresh088 完整 public 行与 semantic catalog 保持；本 runner 的 xmin 仅覆盖 activity_participations，未采集所有旧 public 行 xmin。根代理后续联合验收补齐此范围。
- 所有 adapter 为 LOCAL_SYNTHETIC；真实供应商网络、地域/保留证据、生产费用、网络强制取消、真机与辅助技术均未以本矩阵验收。Closed Pilot / Consumer Beta 保持 NO。

下面的 2026-10-03 正文为历史快照：当时 PARTIAL、尚无原生预算、027 PARTIAL 等结论不代表本次实现现状。旧正文全部保留；文末“2026-10-05”增量章节说明当前原生路径、覆盖与限制。原 immutable worker-final1 和 source-final1 保持不变，本次仅追加顶层审阅说明及独立 source-final2/annex。

## 2026-10-03 历史快照（原文保留）

# Birdtie 模型超时、重试与隐私安全降级

2026-10-03；BT-V5-AIR-010的唯一增量规范。复用[007 Gateway](MODEL-GATEWAY-CONTRACT-V5.md)、[008能力路由](MODEL-CAPABILITY-ROUTING-V5.md)、[027配置版本](MODEL-CONFIGURATION-REGISTRY-V5.md)及[AIR规范](AGENT-INTELLIGENCE-RUNTIME-V5.md)，不新建事件、Memory、授权、计费或Run账本。

## 已实现范围与任务缺口

`apps/api/internal/modelresilience` 提供实际可调用的 **OFFLINE_CONTRACT** 重试内核；它调用008的公开路由和007的既有适配、闭集响应解析，真实运行本地合成adapter，不是仅接口或枚举。007增量保留安全Retry-After元数据。正常Service仍委托原Gateway，没有provider/permit注入方法；nil/false/true gate均为DISABLED/UNAVAILABLE。

**本轮建议任务状态为PARTIAL。** 已验证有界请求次数、退避/抖动、真实context等待与取消、源/许可合成快照的收窄、真实fake secondary标识及关闭门禁；尚无真实AgentRun剩余预算、原生source-purpose/出口当前授权、AIR011费用预留/结算、跨root_trace预算或持久Run重启恢复。当前调用级计数不能替代这些原要求。恢复条件由根代理统筹：011建立实际每attempt当前出口/预算边界，016建立真实Run/Step并固定剩余次数/截止与027版本，009提供获准adapter后仍须单独live门槛和真实正负验证。不得用fixture的ALLOWED作为真实私密数据许可。

本项没有HTTP/main/Flutter接线、DDL、真实provider SDK、模型出网、工具执行、自动Memory写、部署或外部配置。获准的独立本地开发不解除Closed Pilot / Consumer Beta NO，也不解除027 PARTIAL。

## 接口与固定配置

| 接口 | 实际职责 |
| --- | --- |
| `DefaultPolicy` / `ValidatePolicy` / `RetryDelay` | 集中且有界的次数、时间、退避和抖动；纯计算不消费费用预算 |
| `NewService(gate)` / `Service.Complete` | 复用007关闭入口；不接受离线adapter、grant、可伪造的费用许可或clock |
| `NewOfflineRunner(registry, adapters, configurations, resolver, policy)` | 显式接受008 OFFLINE_CONTRACT适配器与合成current resolver，复制adapter数组与策略 |
| `OfflineRunner.Complete(ctx, request, needs, reference)` | 固定027精确配置，按有限次数顺序调用008/007，复核许可并只释放仍有效响应 |
| `OfflineResolver.ResolveOffline` | 开发者合成当前检查；收到这次exact check instant，返回008 OfflineGrant；不能传入Service |

每次调用先 `ResolveReference` 与 `BindRequest`，要求同一Run selector、exact typed Agent、配置fingerprint、中央system prompt、prompt/schema/tool/policy/capability版本。缺配置或失配在零adapter调用前失败。不读取active pointer替换旧prompt；重试、secondary和延迟响应均使用原Request。messages、tool/capability切片和grant destination切片复制，resolver或adapter不能通过共享数组改原请求。

reference仍是027的固定配置合同，不是实际Run row。模型/provider标识不成为AgentID；模型和caller declared confirmed不授权限。

## 有界调度

默认4次模型请求、总60秒、单次20秒、基础退避250ms、最长退避5秒、0–20%正向抖动、同一route两次后允许一次exact route切换。这些是开发默认值，不是生产SLA。策略最多8次、总时间不超过007的2分钟、单次不超过总时间、退避至少1ms且最多1分钟；非法配置拒绝。

全局截止为原Request、原context、调用开始+策略总时间的最早值，并收窄到起始grant及后来较短grant的expiry。单attempt另设context截止，原Request.DeadlineAt及完整digest不变。每次调用依序返回后才能决定下一次，没有递归、while true或未完成并发重发。首attempt、失败与所有实际adapter调用均消费本次次数；每次新调用是独立本地范围，不称跨Run共享总额度。

等待使用可取消context timer。退避指数和抖动均有界；合法Retry-After是最早时间，绝不截小后提前重试。hint超过MaxBackoff或剩余全截止则明确预算/等待耗尽；畸形hint终止。真实1秒delta-seconds header已通过fake适配器、007规范化和真实context等待验证；纯抖动计算与实际等待证据分开。

adapter必须合作遵守context；当前同步端口不能强杀任意忽略context的函数。没有为超时开无限goroutine或在未知调用仍在飞时重新请求。实际SDK的网络级超时和外部结果对账仍待获准adapter/运行任务核验。

## 错误与安全hint

ProviderError仍是原来的两字段Code/Retryable；旧nonkeyed literal保持编译与规范分类。RATE_LIMIT/TEMPORARY可重试，REFUSED/AUTHENTICATION/INVALID_REQUEST不可重试，其他/取消/截止UNKNOWN为终态。wrapper不保存原始error、headers、response body、URL或输入，Error仅输出白名单代码。

007新增 `ParseRetryAfter`、`NewRetryAfterError`、`NewRetryAfterHeaderError`、`RetryAfter`。支持单个有界delta-seconds或标准HTTP-date，adapter接收时间为服务端clock；不继承provider Date作为权限时钟。0合法，负数、溢出、CRLF、多值、超过24小时或畸形值拒绝。24小时仅解析上限，010仍受更小调度上限。畸形hint保留present-but-invalid，使调用者停止；未知、拒答等终态代码不能携有效hint。错误链中冲突代码的hint也不促成重试。

007原规范化函数不改，Gateway只增量改变离线错误返回分支；普通无hint错误保持ProviderError。拒答响应、截断、unknown outcome、不可用响应、invalid schema均不换模型。工具仍只提案：未知工具schema拒绝，工具结果不明不重发；不存在本项工具执行或效果对账证据。

## 合成权限内降级与迟到响应

1. 每attempt重新调用合成resolver，使用008核对exact Agent/RequestDigest/RegistryDigest、source与current source、purpose、consent/policy revision、checkedAt与expiry，再过滤能力。
2. 起始合法grant固定本次许可上界。后来source/主体/目的/政策/consent版本变化即拒绝；新grant仅收窄原始destination+region+state/storage与expiry，不借新较宽grant扩大原许可或续租。
3. route切换只选择原授权范围内仍支持能力的exact provider/model/version/wire。复制并收窄当前grant到实际选中key，再通过008执行；不在一个adapter内部隐藏secondary。成功结果和metadata实际标明secondary。
4. 008的合成checkedAt仍是快照。resolver返回后另外用实际wall clock检查request、grant和选中能力记录的expiry；不改旧checkedAt自证新许可。响应后再次解析合成current grant并收窄，再做能力和实际clock最终复核。过期、撤销、来源改变、迟到、workspace/主体失配内容不释放。

该复核是可运行的**合成合同验证**，不是原生会话/来源/出口的原子授权。没有真实source-purpose、region-retention resolver。008当前state/storage/vision/streaming没有消费者，010同样零adapter调用拒绝，不删需求偷偷走text。OfflineAdapter接口不能阻止恶意自定义实现自行出网；当前仓库没有这样的实现或生产接线，合成descriptor不构成审批。

## 返回与记录

Outcome包含007规范Result及有限attempt metadata：编号、exact destination、region、固定reason、计划等待时间、配置fingerprint。计划等待时间不是已执行时长或持久retry_wait。失败没有messages、参数、源正文、密钥或原始provider错误。usage仍由007解析；缺usage/费用保持UNKNOWN，失败attempt的usage没有被当作0或真实账单。

没有隐藏思维链、完整聊天/图片日志、新增内存兴趣或业务事实。正常COMPLETED/REFUSED/TRUNCATED结果仍服从007提案界限，不作为发布/写操作批准。

## 证据与未测范围

`work/v5-air010/scope.ps1 -EvidenceLabel <新标签>`实际测试四个生产包；final2/final3各530 test PASS、0 FAIL、0 SKIP，分包007221（包含全部旧189）、008130、027纯37、010142；go vet/build/dependency均0。8个生产Go源码SHA前后稳定。完整API源码449文件在该短轮观察相同，仅是观察，不称全API/实库回归。根代理安排共同全量验证并独立核证。

初次GMT fixture失败、真实resolver延迟导致过期结果释放RED、修复和跨包nonkeyed literal vet失败都保留在[正式证据](../testing/evidence/model-resilience-2026-10-03/README.md)。没有数据库或迁移；未运行Flutter/真机/真实网络/race（本机CGO0），未称消费级UI验收。对应UX-CHECK-06/08/10/11/16的来源、主体、撤权、迟到与不可用合同有领域负例，完整UI另行验收。

## 2026-10-05 原生预算与 ModelRequestRun 接续验收

以上是2026-10-03历史实现与当时缺口；本节是原 AIR010 的增量验证，不新建 retry kernel。现已复用真实062 Preview → 精确 Approve → 原066 Ticket → 088 ModelRequestRun/步骤 → 每attempt原 Reserve/Begin/Settle/末次释放。088模型请求运行与083候选富集运行保持不同用途和原ID；Request.RunID仍为原058 binding。原Service.Complete没有接入本地合成adapter，缺合格生产路由仍 Unavailable，不能用本轮本地调用开启供应商网络。

唯一新增 Go 文件为 `postgres/model_resilience_native_integration_test.go`。它使用实际 Postgres 权威端口而非 OfflineGrant：真实 prior A/B 各自批准、不同 price/digest/operation；每实际attempt计入 TENANT_PERSON、SUBJECT_PERSON、ROOT、TASK 四层预算。未用备用步骤保持 PLANNED、无reservation；不同ModelRun也不能绕过同root上限。UNKNOWN结算保持真实未知 settled_cost=NULL，并保守占原上界，不伪造零收费或已核验供应商账单。

新矩阵覆盖合法RATE_LIMIT/TEMPORARY的RetryAfter实际等待及有界抖动、同route无切换额度的合法retry、有限attempt/共享root耗尽；AUTHENTICATION、INVALID_REQUEST、REFUSED、未知派发、畸形/超大hint、规范拒答/截断、非法结构、取消和迟到raw/error均无备用调用。实际原生failure检查之后的350ms backoff期间撤B、Task ABA、OFF→ON、Session自然到期及取消也拒绝续发。普通Session刷新不扩大已见最短期限；原ticket与具体批准不得被新版本恢复。

联合目标复跑原Run四个真实OS kill/restart点（RESERVED、IN_FLIGHT、已返回未结算、SETTLED）、pool/audit等待、原完整plan当前源/期限及旧入口防绕过、原配置历史版本。重启仅恢复原ID控制metadata，不从账目恢复私答、不重发未知operation、不自动Reserve备用路线；工具未知结果不授新请求，原ActivityQuery不获得任意工具执行权限。

`work/v5-air010-native/native4`：376 PASS测试事件、0 FAIL/SKIP/package FAIL，含新矩阵41事件；test/vet/build/两CLI build均退出0。同不可变915输入（912 API Go/SQL/mod/sum +3原seed），执行/live/前后SHA一致。`pure-current1`另复跑原007/008/027/010四包530 PASS、0 FAIL/SKIP；它是兼容证据，不代替原生许可验证，不把两个命令重叠事件相加称独立功能数。

fresh088旧完整public行与可见semantic catalog经过up/unused down/reapply及目标前后保持；**xmin仅核activity_participations，不含完整public所有旧行xmin**。主库及实际HTTP/migration子库清理见专属记录，根将按后续当前schema另做全表/xmin及整仓联合验证。本轮没有新增DDL。native1两超时fixture未到adapter（各calls=0）属于预设100ms不足的故障注入边界；native3 hook port漏嵌LocalRetryPort导致PG测试编译失败；原始帧均保留。改为实际adapter接收ctx后等待真实截止再返回，并透明复用原native port，不修改授权或生产代码来迁就测试。

仅 CODE_AND_LOCAL 验证，任务状态由根核证后修改。新Flutter、真机、辅助技术、真实provider/tokenizer/地域/保留/收费/硬网络取消及部署均 NOT_RUN；同步不合作adapter不能强制终止，只能丢迟到结果、禁止后续调用。现许可仅本人PERSON/SELF_TASK_QUERY原scalar ActivityQuery，未开放的用途和模型网络保持关闭。AIR009与其LIVE门槛、AIR018等下游不倒置为本任务前置。Closed Pilot / Consumer Beta仍 NO；同当前schema整仓联检仍由根执行。

## 2026-10-06 AIR028：一次格式修复复用原预算

历史拒答/截断/invalid 不触发运输 fallback 的规则保留。显式原生 `RunValidated` 可在实际有界 malformed JSON 或明确 TRUNCATED 响应后，进行最多一次固定 schema 重试；这不是把错误泛转为 RATE_LIMIT/TEMPORARY。REFUSED、未知 provider/派发/结算、空/超限/坏 UTF-8、有效 JSON 注入字段、错误实体、来源变化、取消、迟到或 OFF→ON 均不获格式修复。

下一步必须是最初完整 088 plan 中已经本人精确批准的独立 operation，完整 request 字节、provider/model/version/region/retention 与原步相同；不把污染响应、修复指令、来源 ID、正文或 proof 加到 provider 输入，不在错误后创建/批准新 preview。每个实际修复重新经过原 Reserve→Begin→Settle，消耗 TENANT_PERSON、SUBJECT_PERSON、ROOT_TRACE、TASK 四层原 062 预算及策略全截止；无剩余额度时不会调用 adapter。未知 usage 保守保留原上界，不能伪记零费用。

本进程原生 handle 和循环各限制一次修复。第二次仍 malformed/truncated 即停止，不执行第三次；运输重试仍受同一个已批准 plan 和总次数/期限，不另起预算循环。Settle 回执丢失时即使后台已提交也不生成继续凭据；Control、重启或新 Store 不能从 UNKNOWN 推断可重发。迟到结果丢弃，同步不合作 adapter 无法被强杀的原限制继续如实保留。

没有下一批准步或第二次格式仍无效时，终止错误保留 `ErrOutputRepair` 和原 `ErrOutputSchema` / `ErrOutputTruncated` 的固定分类。仅合并这两个已净化安全类型；不合并 provider exception、原始正文或响应 request ID。单步截断、单步 malformed JSON 和末次无效有实际 native RED→PASS 证据，不用笼统耗尽错误隐藏结果性质。

本轮未增加网络、HTTP 或 main 入口，生产 provider 和自动写继续 OFF。实际原生正反例及原失败帧由 AIR028 独占材料记录，最终整仓回归由根核验，不能将本地合成格式修复当正式模型/试点证据。
