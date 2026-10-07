> 最新CODE_LOCAL状态以末尾2026-10-04根最终验收及live队列为准，旧阶段边界保留。

# Agent 事务事件队列与控制消费

2026-10-03，AIR015 唯一增量规范。依据 [AIR 设计](AGENT-INTELLIGENCE-RUNTIME-V5.md)、[认知 ADR](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md)、[Memory 边界](AGENT-MEMORY-ARCHITECTURE.md)、[实施审计](../research/BIRDTIE-V5-AIR-015-AUDIT.md) 与原队列 acceptance。当前完成本地 metadata/control 基础设施，原需求验收为 **PARTIAL**：没有实际分析目的许可、候选消费者或已提交业务效果。证据见 [本地归档](../testing/evidence/agent-transactional-outbox-2026-10-03/README.md)。

## 权威和元数据

原生 Moment 内容、revision、拥有者与人类授权仍由现有领域负责。agent-outbox-v1 仅存 typed Person/exact Agent、当前源 ID/revision/status、不含正文的本地指纹、原生变更时间、固定十五分钟 expiry 和服务器生成的操作/事件 ID。来源指纹是最小控制绑定，不是权限、分析同意或通用历史 revision。

Envelope 与 AIR014 的 `air.event.v1` 分开演进，不能把新三个变更 hook 说成旧 catalog 全部 producer 已接入。PERSON tenant/subject/actor 必须相同，source 为该主体当前私人 Moment，Agent 为当前 active Personal Agent，已有 AgentProfile metadata 必须匹配。Organization/Business 没有此消费者入口，Community 不能因此获得 Agent。

指纹在数据库中由 Moment、Account、Agent、metadata 的实际行与 xmin 及当前 Activity/Community/Organization links 计算 SHA256；数据库仅持久化摘要。它不保存源正文、媒体、坐标、Session/token 或授权正文，也不使创建者成为长期机器授权主体。schema 的指纹格式约束不能代替此可信计算与当前来源复核。

仅 MOMENT_CREATED、MOMENT_UPDATED、MOMENT_WITHDRAWN。Withdrawal 不称物理删除事件；Task、参与、收藏、成员变更尚未接入。创建请求本身没有新增 HTTP 幂等，重新创建两条 Moment 是两个原生对象。

| 原生动作 | 事件 | 版本和时间 |
| --- | --- | --- |
| CreateMomentDraft | MOMENT_CREATED | 真实 revision=1、created_at |
| UpdateMomentDraft | MOMENT_UPDATED | 真实 revision>1、updated_at |
| WithdrawMoment | MOMENT_WITHDRAWN | 真实 revision>1、withdrawn、updated_at |

上述 source clock 是领域行变更时间；用户填写的 occurred_at 不成为投递时钟。received_at 来自 PG 当前时钟，expires_at 必须等于 source clock +15 分钟。重试、领取和 handler 升级均不更新这三个源字段。

## 事务、领取和回放

原 source/context/audit/outbox 使用同一 Read Committed 事务；未建立 metadata 的原人类路径不创建认知对象。数据库 outbox 故障必须回滚业务事务，不吞掉错误。元数据采集不调用模型，不将人类保存等同授权机器分析。

独立 consumer inbox 的键为 event/subject/handlerVersion。SKIP LOCKED、真实 PG 时钟和单调 fence 共同负责领取；领取、提交时重新核当前源、主体、Agent、metadata。失去当前 lease 的旧 worker 不能 checkpoint。租约不延长 source expiry，重试和版本回放不能续命。

迁移 064 新增三张表，不回填旧源或重写旧 ID/version：

| 表 | 实际责任 | 唯一键/边界 |
| --- | --- | --- |
| agent_domain_outbox | 不可变事件元数据与投递控制 | event_id；单调 attempt/fence；attempt≤20 |
| agent_consumer_inbox | 每个 handler 的持久控制 receipt | event_id + subject_id + handler_version；终态不复活 |
| agent_effect_ledger | 未来真实 writer 的地址约束预留 | subject_id + effect_key；当前 INSERT/UPDATE 一律 SQLSTATE 55000 |

lease_owner 是服务器 worker UUID，lease_until 最多 30 秒且不晚于 source expiry；Claim 不能 JSON 编解码。Claim 与 Consume 均显式 Read Committed，锁 outbox 后锁当前源/账户/Agent/metadata，不依赖 pool 默认隔离级别。

两个提交前时钟检查分别在 Claim 写入/更新 consumer receipt 后、Consume 更新 receipt 后取得真正的 clock_timestamp()，再检查 lease/fence 与 source expiry。真实 PG 触发器等待跨过截止时间的测试证明整笔控制事务回滚。这里保证该检查点的当前期限；不承诺网络发送后绝对撤回，也不将 Go 形状验证器当当前数据库授权器。

handler mom-control-v1/v2 是控制 registry，不能选择候选算法或授予权限。超时恢复须同 handler；终态 receipt 不复活；v2 的控制 receipt 不改 v1 历史。源物理删除/编辑、主体/Agent/metadata 变化使旧控制失效。源没有 FK，保留有界最小控制记录；账号/Agent 正常父删除 cascade。当前尚无运营清理调度证据，不宣称部署的无限期或自动清理能力。

内部 Store 提供 ClaimAgentOutboxControl、ClaimAgentOutboxControlForSubject、ConsumeAgentOutboxControl。subject selector 只约束可信维护任务选择，不能授权本人、第三方或模型。没有新 HTTP 处理端点、部署 worker 或人类权限旁路。metadata 删除不补建；失效/过期处理可关闭当前 LEASED receipt，不能把旧租约恢复成新批准。

## 效果与未实现范围

EffectKey 不含 handlerVersion 或 eventID，绑定 typed tenant/subject、exact Agent、服务器逻辑操作/action/effectKind。但地址和摘要不会授权真实动作。

实际 consumer 硬 UNAVAILABLE，effect 0；064 effect ledger 的 INSERT/UPDATE 明确拒绝。当前没有真实分析 purpose resolver、自动候选提交、完整 AgentRun、外部消费者、网络 exactly-once、授权自动写或运营恢复证据。后续接入须原权威事务复核和真正 effect writer；不能靠状态文本、provider fixture、空表或纯合同测试解除门槛。

100 次重复及 v1→v2 回放已经验证单个 handler 的同一 receipt 不重复、升级只新增相应控制 receipt，effect 始终为 0。该结果没有满足“重复 100 次仍只有一个真实候选/业务效果”的原验收。解除 PARTIAL 至少需要真正当前 analysis-purpose/source resolver、候选提交与效果同事务/等效可靠机制、稳定 effectKey 的真实去重和升级/故障对账证据；不能仅移除拒写 trigger。

## 迁移与证据范围

非空 064 down 对三表任一保留行原子拒绝；只有隔离自有库明确空表时才验证 down/reapply。最终 native4 两轮各 260 Test PASS（230 领域 +30 PG，包含父测试/子测试）、0 FAIL/SKIP，target vet/build exit 0。升级/失败回退/空表回退/重建及本地测试前后旧 public 完整行均相等，自有库已 DROP。

native4 原始 owned hash 清单实际为 **13** 个生产 Go/SQL 源，前后及归档时匹配。其全 API 源快照并不完全一致：并行 AIR027 修改了 agentplacememory/model.go、model_test.go、postgres/agent_place_memory.go；不把此轮写成全仓源稳定通过。

根代理后续独立 `current064-full1` 窗口已完成：001–064 自有隔离库、scope260 PASS、默认 `go test ./... -count=1 -json` 三轮各6654 Test PASS/0 FAIL/SKIP，全仓 vet/build exit0；490 个全 API Go/SQL/mod/sum SHA、13 owned SHA及各轮全部 public 完整行稳定，自有 `birdtie_air015_93bf2a1a5fe4` 已 DROP。每轮18个无测试包不计为 Test SKIP。正式 [收尾 receipt](../testing/evidence/agent-transactional-outbox-2026-10-03/root-full-receipt.json) 单独保存真实 raw/result；不逆改 native4 的全源变动记录。完整回归通过不解除缺失真实候选效果的 PARTIAL。

本任务当前没有新增 UI，Flutter/真机/移动端/辅助技术未运行。后续遵循 [全局 UX 合同](../ux/GLOBAL-UX-INTERACTION-CONTRACT.md) 的 UX-CHECK-06/08/09/10/16：来源与过期不猜事实、撤权后旧批准失效、重试无重复副作用、跨主体不串状态、控制事件不默认存私密内容。实际测试与未测边界继续逐项记录。本文不是界面整改或试点证据。Closed Pilot / Consumer Beta 均 NO。

## 2026-10-04：单次本地维护入口

原 PARTIAL 需求按依赖接续，只补充原生控制维护入口。新 `internal/agentoutboxmaintenance` 与 `cmd/agent-outbox-control` 复用现有 `ClaimAgentOutboxControlForSubject` / `ConsumeAgentOutboxControl`；没有新表、grant、领域 writer、HTTP 端点或调度器。

命令要求明确 `--local-development-only`、规范 PERSON UUID `--subject`，handler 仅 `mom-control-v1` / `mom-control-v2`。默认 batch 25、timeout 15 秒，最大 batch 1000、timeout 1 分钟；从池创建到一轮结束共享总截止时间。worker UUID 由本进程随机生成，不接受外来 Claim/fence/lease。`BIRDTIE_DATABASE_URL` 只从环境读取，primary/fallback 和实际 dial 都必须 loopback，localhost 固定为127.0.0.1。loopback **不能证明目标不是生产隧道**；操作者必须选择其有权维护的独立本地开发数据库。本轮仅在自有可丢弃库执行。

本地构建与调用（环境中须已安全配置隔离库，UUID为该库的合成本人）：

```powershell
go build -o D:/Project/birdtie/work/v5-air015-maintenance/agent-outbox-control.exe ./cmd/agent-outbox-control
D:/Project/birdtie/work/v5-air015-maintenance/agent-outbox-control.exe --local-development-only --subject <PERSON-UUID> --handler mom-control-v1 --batch 25 --timeout 15s
```

命令必须从 `apps/api` 构建。selector 只是可信数据库维护选择，**不是普通用户或 Agent 的业务许可**。不启用模型、永久 Memory、候选提交、外发或 effect ledger 写入。输出仅有闭集 schema/status/stage/reason、确认控制回执数量与四类计数；不输出行、ID、Claim、正文、坐标、底层错误或数据库连接串。`business_execution` 固定 `UNAVAILABLE`。

无工作仅接受 native 精确 `ErrNotFound` + 空 Record/Claim。任何其他 Claim 错误立即停止；原生可能已将失效/过期行置终态却返回空 `ErrUnavailable`，命令仍报告未确认且计数0。Consume 只有精确 native `ErrUnavailable` 与有效终态回执可计数，必须匹配 event/subject/handler/fence/attempt，回执时间不得早于已领取行、不得达到 lease/source 截止。已有 handler 的旧 createdAt 可保留。空、矛盾、包装错误或提交结果未知立即停止且不重试；下一次独立运行仍由原 inbox/fence 控制，不另造对账 ledger。若已收到有效已提交回执后才取消，则保留该确认计数，下一轮停止。

退出码0仅表示本轮控制结束（无工作或达到上限）；配置不合法为2，数据库/控制未确认或截止为1。三者都不表示业务执行完成。真实部署调度器、清理服务、真正分析用途许可、候选/effect 原子性及全部 producer 仍未实现，完整 AIR015 保持 PARTIAL。新增证据见 [维护验收](../testing/evidence/agent-outbox-maintenance-2026-10-04/README.md)，原064历史证据及原发布门槛保留。

本批最终 fresh077 `native3` 四包目标（原agentoutbox领域、maintenance、command、postgres，`^TestAgentOutbox`）实际 **369 Test PASS / 0 FAIL-SKIP / 0 packageFail**，包括父/子测试及辅助子进程驱动。实际CLI构建、target test/vet/build均exit0；741全API Go/SQL/mod/sum前后SHA相同，旧public完整行与functions/triggers/constraints catalog相同，自有库已DROP。五Go冻结清单及最终命令二进制SHA见专属证据。此轮不是 `go test ./...` 整仓结果，根代理独立核证另记；不继承旧9328全仓帧作为本新增命令验收。

## 2026-10-04 原AIR015具体来源许可接续（在制品）

根对实际253项 formal depends_on 做只读审计未发现图循环。AIR015直接AIR014/INT001均DONE；AIR016单向依赖AIR015，不能以完整Run先完成作为015当前native候选效果前置。原015故障矩阵与100次真实单一候选/效果验收保持。

当前第一切片见 [具体来源许可绑定](AGENT-ENRICHMENT-PURPOSE-BINDING-V5.md)（worker正在实现，尚未核证）。Person当前Session/PersonalAgent/ACTIVE Task/本人私人Moment title/body版本明确预览、批准、resolver和撤回复用原consent_grants唯一状态/revision/expiry；079仅metadata预览和不可变1:1 binding。076 TASK_CONTEXT_READ、062 SELF_TASK_QUERY出网许可不替代此分析许可。批准分析也不授Memory候选持久保留；后续原015仍需明确STAGE_MEMORY_CANDIDATE和实际候选/effect/inbox同事务，064原硬拒写不能直接删除来虚报消费者。源码、registered HTTP及隔离079测试在制品不在旧冻结078全Go9483证据内。外部模型/自动动作仍OFF，Closed Pilot/Consumer Beta NO。

## 2026-10-04 后续核证：079/080许可已实现，082原子链继续实施

上节“正在实现/尚未核证”保留其当时范围。079实际来源许可首切片34 native PASS，根079第二轮全Go9523 PASS/0 FAIL-SKIP、test/vet/build0、759源稳定和非空旧public/catalog/迁移往返/清理已核证。080另一个明确候选保留许可最新42 native PASS、test/vet/build0、770冻结投影帧及旧public/7类catalog/xmin/used-down拒绝/清理通过。根逐字节复核5671份不可变Phase A归档，见 `work/v5-age038-resume/retention-phase-a-root-proof1.json`；原079归档保留。上述完整Go仅当时079帧，不覆盖当前082在制代码。

080仅许可批准过的单Moment、明确字段、原Session/Task/版本/算法与有限截止下的词法 LOW 假设。未写063候选、未写064effect、没有模型出网/Memory promotion。不同Session不能继承分析/Stage权限。否定/歧义/多类别与关联Activity/Community/Organization明确closed；关键词不称已喜欢、参与、出席或资格。具体保留截止不得通过重试/换grant/版本回放延长。

原AIR015继续IN_PROGRESS，同一lease增量接续082原子消费者，而非复制手工SaveOwnCandidate或先造完整Run。21精确路径及root登记见 `air015-consumer-phase-b-lease1.json`，方案见 `work/v5-air015-retention/PHASE-B-PROPOSAL.md`。同可信Controller默认OFF、原当前079/080、真实063 CANDIDATE、原064 stable EffectKey、同事务 effect/inbox/checkpoint、source-first/outbox NOWAIT、末PG时钟与撤权/崩溃/100重试必须通过实际native矩阵。不得移除/禁用原effect拒写trigger或用GUC/confirmed绕过；新窄proof分支和deferred核验尚在实现，不能提前称effect消费者可用。共享main/server由root接线，worker不修改。

本任务不借运行记录授新的机器分析、自动动作或现实供给权限；尚无真实部署调度/清理服务、provider或完整Run证据。Closed Pilot/Consumer Beta NO，当前手机仍已核证078版本；新079/080/081/082尚未安装为整体验收版本。


## 2026-10-04 根最终验收：原AIR015 CODE_LOCAL已完成

前文PARTIAL/在制是各历史阶段当时边界，保留作来源。当前079/080具体独立许可加082原063候选/064effect-inbox-checkpoint真实同Tx已经完成；root whole0829718PASS/0FAIL-SKIP/vet-build0/794stable+3seed、up/down/reapply/旧public-catalog-xmin/ownedDROP，完整21源与6693/1682档全部hash核证。真实100重试、跨handler稳定effectKey、原子崩溃及HTTP断连后原grantreceipt对账、撤权/expiry清pendingpayload已验证；旧064control硬拒绝分支仍保留。新AIR016专司异步Run恢复，不反向撤销已验证015。未启用部署consumer/provider/永久Memory，Closed Pilot/Beta NO。证据work/v5-age038-resume/three-task-acceptance32.json。


## 2026-10-05 初次与重读 Envelope 失败观测（实现切片，原首因 UNKNOWN）

上文各阶段 PARTIAL/在制/CODE_LOCAL 是当时快照，原源码与证据保留。根在 schema091 whole10735 PASS 的安全点，显式恢复原 AIR015 后，追加本失败观测切片的五个精确 Go 范围。原38p的初次 NewPending ErrInvalid 精确字段原因仍 UNKNOWN；本节不把 controlled negative 或绿色回归当作原故障解释，也不构成原 AIR015 完整 DONE。

`internal/agentoutbox/envelope_diagnostics.go` 的 DiagnoseEnvelope 只观察调用方已有 envelope 与已经取得的 now；复用同包 validID/validTime/digest/StableEventID，返回恰好32个 bool。它不取时钟、不持久化、不决定错误、来源有效性或权限。model.go 的 ValidateEnvelope/NewPending/ValidateRecord 原校验与错误保持字节，不把 flags 代替权威校验。

postgres 仅在原初次或既有 retry 的 NewPending 已失败后调用 slog.WarnContext，然后返回原错误。固定中文消息为“原生事件控制形状校验失败”，phase 仅 INITIAL_NEW_PENDING / RETRY_NEW_PENDING，error_class 仅 INVALID / EXPIRED / UNKNOWN，conditions 为固定32键的 bool。无自由 phase/error text、ID值、正文、fingerprint、token、e.OccurredAt/ReceivedAt/ExpiresAt/now 精确时刻或源类型原始值。日志 handler 的普通记录时刻是日志元数据，不是源时刻。未知内部 phase 不输出，不 panic；序列化失败仅反映为 bool，不取代原错误。成功路径0条日志。

生产 agent_outbox.go 仅增加两个失败观察调用；原 current SQL、PGclock、source/TTL/绑定检查、原3次有界 SQL拒绝重读、2ms等待与错误回传不变。原 agent_outbox_capture_diagnostics_test.go 的 SQL/4000样本 Test 完整字节保留，它针对后续 P0001，不能把它改称本初次失败观测。

本任务 pure unit1 在独占副本使用已核原940输入（937API+3seed）加4个新Go，944输入冻结，实际47 PASS /0 FAIL-SKIP/package failure。矩阵对照原 ValidateEnvelope/NewPending，覆盖32条件以及严格 future/expiry/oversize 零Record；safe-log allowlist、初次/重读真实 append 控制流的 synthetic pgx返回行、成功无日志通过。这里的 retry fake 仅证明控制流和日志分类，不是原生PG重现或原宿主时钟首因。

新 native Test 复用 ownedMigrationDatabase/newOutboxFixture，当前三原生变更与受控未来来源、非空显式 Memory canary、全public完整行/xmin对照。artifact env可选，常规go test不因缺自造变量失败；只有显式绝对 work artifact路径才持久化 synthetic snapshots。实际native/current whole待根和007共同冻结最新092后执行，旧091不能作当前092验收。当前证据见 [失败观测切片](../testing/evidence/agent-outbox-initial-diagnostic-2026-10-05/README.md)。

此前 work-only 1988文件、940帧和53PASS诊断证据冻结保留；其唯一初次失败来自受控未来负例，自然capture=0，原38p首因仍 UNKNOWN。没有新增领域producer、UI、DDL、grant、模型调用、调度/清理部署或权限；Closed Pilot / Consumer Beta NO。


## 2026-10-05 schema092 本地收尾：原 AC 与历史首因分别记录

根的默认并发整仓已经终态，实际命令为 `go test ./... -count=1 -timeout=30m -json`，10797 个测试 pass、0 fail/skip/package fail，vet/build/两个真实 CLI build 均退出0。执行的是 `work/v5-age038-resume/joint092-source38km` 的943 API文件加3个原seed，共946输入；全48条旧行、所有旧xmin、语义catalog、unused092 up/down/reapply及307个实际日志自有库SQL absence由根独立核证。原首轮10794PASS/2FAIL及UUID测试名比较的脚本失败原证据保留，不改raw。证据为 `work/v5-age038-resume/hiking-whole2-root38kr.json`；whole raw为 `work/v5-age038-resume/age007-hiking-root-verification/root-whole2/tests.jsonl`。

本任务五Go仍逐字节匹配 `work/v5-air015-initial-envelope-implementation/source-freeze1.json`。I15定向native1实际100PASS/五命令0，全部100条case也在本次whole通过；unit1纯矩阵47PASS。五对136个public表完整行/xmin保持，current三原生writer成功0warning；唯一initial warning来自明确受控future负例，固定32bool、原ErrInvalid、零Record及完整rollback保持。pure retry只证明append控制流，不能声称native clock重现。原4000 Test与model.go完整字节不改，whole中原4000也实际通过。

| 原 AIR015 AC / verify | 实际实现和原生测试 | 本地判断 |
| --- | --- | --- |
| 领域变更与待处理事件同事务 | moments.go三writer在原pgx.Tx内append；TestAgentOutboxNativeThreeWritersMetadata、TestAgentOutboxOriginalCommitAtomicFailures | 三个闭集Moment变更通过；BEFORE/AFTER INSERT故障使业务、audit、outbox同时回滚 |
| event_id+handler_version+subject去重，候选/effect幂等 | agent_candidate_pipeline.go同Tx写063候选、064effect/inbox/checkpoint；TestCandidatePipelineNativeExactlyOnce100AndHandlerUpgrade | 一个实际非零CANDIDATE效果后100次重投及v2回放，原完整候选/effect/inbox/outbox集合不变；不是早期零效果control证明 |
| 稳定logical_operation_id/action_id effect_key | pipelineKey复用agentoutbox.EffectKey，地址不含handler；同一native测试和TestAgentOutboxStableEffectKeyIgnoresHandlerUpgrade | handler升级返回原效果，不重复提交 |
| commit前/消费中断与恢复 | TestCandidatePipelineNativeFaultAtomicityAndControllerLate；TestCandidatePipelineNativeProcessCrashRollsBackAndRestartReconciles | 六事务故障点及真实子进程exit86回滚，重启原grant提交一次；子进程测试覆盖after_candidate/effect/inbox/checkpoint/before_commit |
| commit后结果未知和恢复 | TestCandidatePipelineHTTPNativeLostResponseUsesOriginalReceipt | 实际handler先提交，再关闭loopback连接；GET原grant原生receipt及同key重试不增加候选/effect；不宣称网络exactly-once |
| 当前来源、撤权、过期、跨主体和错误权限拒绝 | TestCandidatePipelineNativeCurrentSourcesIdentityAndLease、TestCandidatePipelineNativeRealWaitRejectsCurrentGrantSourceAndDeadline、TestCandidatePipelineNativeMissingGuardAndOriginalIntentConflict、TestCandidatePipelineNativeSubmitterIsActualAndRefsCannotGrant | 原来源/Session/Agent/permission/lease末核和零效果拒绝通过；source reference、digest、客户端confirmed不能授予权限 |
| 必要增量schema与旧数据兼容 | 064/079/080/082既有迁移；TestCandidatePipelineNativeUsedDownAndTTLFullPayloadCleanup、TestMemoryCandidateNativeCurrentDataMigrationRoundtrip；091/092当前往返和whole48旧行/xmin/catalog | 非空历史down拒绝与当前fresh/current-data兼容证据通过；不在生产执行down |
| 当前仓库和隔离本地范围 | 默认OFF controller，registered HTTP明确许可入口；10797whole/五命令0 | CODE_AND_LOCAL_VERIFICATION原AC已有本地证据；未启用模型出网、真实自动Agent写、视觉、A2A或发布试点 |

原38p `revoke_guard` fixture初次NewPending ErrInvalid的自然精确字段首因仍 **UNKNOWN**。旧raw没有原32条件，当前自然capture=0；failure-only观察为后续原失败提供安全分类，并没有解释或修复该历史首因。当前严格future/expiry/oversize拒绝、原错误回传和零副作用是独立已验证行为。本次本地AC覆盖判断依据上述具体实现/故障/非零效果证据，不用一次controlled negative或重复绿替代历史解释。

原队列completion_scope为CODE_AND_LOCAL_VERIFICATION、external_gate_ids为空；原source/AC没有“全部registry事件接线”或“部署生产scheduler”条款。agent-outbox-v1始终闭集三个Moment native mutation；AIR014/AGE064的air.event.v1 catalog是另一metadata入口。UserQuery、ActivityJoined/Left、PlaceSaved、CommunityJoined/Left、ProfileUpdated、PreferenceUpdated八个当前metadata源仍未接入此outbox，ActivityCompleted/PlaceVisited仍UNAVAILABLE，不造访问/居住/出席事实。本地收尾不称全域hooks、运行部署、运营SLO或网络全链路exactly-once。新的007客户端恢复、概率校准和真机验收不借本后端Go结果称完成。原任务正式状态与六总报告仅由root更新，Closed Pilot / Consumer Beta仍NO。

完整精确路径、whole raw包含计数与docs-only收尾receipt见 `work/v5-air015-initial-envelope-implementation/local-closure2/AC-MATRIX.md` 及 `docs-only-receipt.json`。本段是两份canonical追加；五Go和旧2015文件归档不变。


## 2026-10-06：原生刷新短窗口合并（AIR020 相关单元切片）

实际接线为原 `appendMomentOutboxTx` 的新事件 INSERT 成功后、原 capture SAVEPOINT 释放前。新增逻辑没有另造 producer、scheduler、receipt 或权限：仅新 `MOMENT_UPDATED` 的当前私人 draft、revision>1 才考虑合并；创建和撤回保持原 capture 路径。

同 Person、同 Personal Agent、同 Moment 的低 revision `MOMENT_CREATED`/`MOMENT_UPDATED` 控制，只有仍为 PENDING、attempt=0、fence=0、无 lease、无 causation、原 root=logical operation，且旧 received_at 在新 capture 与最终实际时钟的五秒窗口内、旧/新 expiry 均未到期时，才可将旧控制置为 INVALIDATED。每次最多64条；旧记录完整保留，更新仅 delivery_state 与真实 updated_at，不改事件 ID、来源版本/指纹、logical operation、root、occurred/received/expiry、attempt/fence。新事件同样保留原十五分钟 TTL；合并不重写内容，不替用户进行分析、保留 Memory 或授权模型。

writer 已持 Moment 变更锁，原 Claim 则先锁 outbox 后读取 Moment；因此旧候选必须 `FOR UPDATE OF d SKIP LOCKED`，不等待已锁旧事件。候选锁集合读取完成后才取得单个实际 clock_timestamp，在同句复核新事件与当前 Person/Agent/profile/Moment 完整原生指纹、版本/状态/时间，以及旧事件当前投递状态与期限。任何 inbox、effect、run、079分析预览、080保留预览、091多候选预览历史均阻止合并，不仅保护当前有效批准；分析预览按本人/Agent/Moment 保守保护全部历史。原分析预览先持 Moment SHARE，与 source writer 的更新锁相交。

仅在实际保护表及所用列类型完整时启用此优化。064 effect ledger 的稳定 subject/Agent/operation/source/revision 列可用，但不假设它具有082才新增的 event_id；缺任一后续保护结构时跳过优化，不能将缺表理解为没有同意/历史。优化使用自己的 SAVEPOINT；仅实际 PostgreSQL 42P01/42703 的缺表/列竞态回滚优化并保留原新事件插入，其他错误仍返回原事务，不能吞掉数据库故障或假装 source 保存成功。取消或回滚/释放失败也不能释放原 capture 成功。

验证只运行当前需求直接相关单位。实际原 append 首 RED：1个测试失败，原成功只有3条控制命令，没有合并；修后同实际 append 的 pgx spy、缺结构/缺结构竞态、权限/冲突/未知错误、回滚/释放/取消、原创建/撤回与原初始 capture 两单位，以及静态 SQL 合同，共6顶层、29个 run/pass、0 fail/skip、exit0。940项 Go/module 源码库存分别在两命令前后 SHA 稳定；库存不是证明所有文件都被该 selector 执行。实际命令、原始日志、修改差异和保留证明见 `docs/testing/evidence/outbox-refresh-coalescing-2026-10-06/README.md`。

pgx spy 调用真实原 append 和本次接线，但不执行 PostgreSQL；静态 SQL 检查证明发送的保护谓词、候选界限、单时钟和字段不变，不能证明数据库锁、并发、吞吐或真实表行已执行合并。本轮无 DDL、未运行原生PG/集成/迁移/压测、全面回归/vet/build、手机、部署或外部模型。通用 causation/root 预算、最大链深度和全部事件消费仍未实现；封闭 Moment 目录没有 MemoryUpdated，不表示通用循环检测已完成。整个 AIR020 继续 PARTIAL，Closed Pilot / Consumer Beta NO。


## 2026-10-06 原 AIR020：独立控制维护领取配额

原 `claimAgentOutboxControl` 已实际接到私有选择器，在同一原 Read Committed 事务、原 outbox/source 行锁之前取得独立 `pg_try_advisory_xact_lock`。锁占用只结束本轮，不在进程内重试或等待该锁。复用原 `agentrun.PickDispatchTenant` 与 4/1/256 数值：当前独立 metadata control 最多全局四个、同本人一个，最多返回 256 个主体队列头。这是控制维护配额，**不是 Run 配额、候选写入配额或新的访问许可**。

早期真实 PG 时钟统计仅包含当前 outbox LEASED、lease/expiry 未到期，且原 inbox 的 event/subject/fence/attempt 一致、control_state=LEASED、handler 为 mom-control-v1/v2。原 candidate-local handler 的同事务暂态不计为控制配额，不修改其 writer、许可、effect 或原 Run 调度。选择器返回私有 observedAt；原 claim 最后真实时钟须不早于该次 admission 时钟。这个额外闭合检查位于原 CheckFence/expiry 检查之后，旧错误优先级和最后校验保持。没有给时钟增加容差，未实测原生 PG 时钟回退或提交配额并发。

全局内部 Store selector 对已有 control inbox 服务历史读取 max(updated_at)，两 control handler 共用该排序历史；它表达最近控制服务/确认，**不是精确历史领取时间或权限真源**。同本人仍沿原 occurred_at/event_id 顺序和 SKIP LOCKED。显式 subject 的单次本地 CLI 仍只选择原本人，不能选择其他人，也不保证多个 subject CLI 进程之间的轮转公平。选定行退休/被锁返回 Busy，只有真正无 eligible head 才返回 NotFound。

准确剩余：原初始 PENDING source-invalid/expired/deadletter 分支只更新 outbox 和已有 inbox，可能没有 inbox 行，提交后仍返回空 Unavailable。因此 inbox-only 服务历史不能保证大量初始终态 backlog 的主体公平；无历史的两头仍按原 next_due 排序。直接单位明确保留这个事实，没有插入假 receipt、补 grant 或增加公平授权账本。**本轮容量限制及有服务历史的选择内核不等于完整事件突发公平性验收。**

原维护 runner 只接受 exact ErrDispatchBusy 加空 Record/Claim，返回 FINISHED/CLAIM/DISPATCH_CAPACITY_BUSY，计数零、无 Consume、无业务效果、无重试；原 CLI 的退出0仍只表示本轮有界维护结束。包装/拼接 Busy、非空 Busy、不明提交结果保持 STOPPED/CLAIM_UNCONFIRMED；取消保持原 CANCELLED/DEADLINE_EXCEEDED。旧 exact empty NotFound 仍是 NO_ELIGIBLE_WORK，原空 Unavailable 不当确认回执。

证据见 [本切片](../testing/evidence/outbox-control-dispatch-2026-10-06/README.md)。原 selector 行为保持提取并实接原 claim 后，以 pgx unit spy 得到有效 quota RED：4 run/fail（两个额度子场景、其父和一个 runner 场景），exit1，非编译失败。修复后 11 顶层/53 run/pass，另原 runner 七个直接相关顶层/45 run/pass，均 exit0、0 fail/skip；原旧断言和源码前缀保持。SQL/调用位置单位标为 UNIT_STATIC；spy 选择器→runner 组合不是完整 pool Claim 或数据库执行。两次单位执行的 945 个 Go/module 历史输入清单各自稳定，不称精确执行依赖图或全仓验收。

无新 DDL、HTTP/客户端入口、provider、自动写、部署或手机服务变化。通用 root/causation 预算、最大因果链深度、MemoryUpdated 自触发真实消费者仍未接入；原090和已完成 Run 公平/短刷新合并行为保留。native PostgreSQL/锁/并发/迁移/压测、公平终态 backlog 验收、整仓测试/vet/build、手机与外部服务均 NOT_RUN。整个 AIR020 继续 PARTIAL，Closed Pilot / Consumer Beta NO。


## 2026-10-06 原 AIR020：无 inbox 终态队列进度排序

接续上节已冻结配额，不另建消费者或授权账本。当前真实 `selectAgentOutboxControlTx` 的同一 head SQL 将原 control v1/v2 inbox 的 max(updated_at) 与原 outbox 的 max(updated_at) 取 `GREATEST`。后者仅 schema=agent-outbox-v1、source=MOMENT、同 subject、INVALIDATED/EXPIRED/DEAD_LETTER、attempt=0/fence=0、无 lease_owner/lease_until 且无任何同 event/subject inbox。两者都没有值时仍保留 nil 与原 next_due 顺序，不编造历史。

这补上上节 inbox-only 排名的代码缺口：原 `claimAgentOutboxControl` 初始失效/过期路径已有真实 outbox terminal updated_at，却可能没有新 inbox；现在这些保留的终态记录参与排名。源失效的短刷新合并也更新 INVALIDATED 的真实队列行，它同样只表示队列取得进度，**不是 claim、服务回执、访问许可或某次人类行为**。原 terminal branch 不新增 INSERT receipt，不改其 empty Unavailable 返回语义。UNAVAILABLE/CANDIDATE_STAGED/LEASED、已有任何 inbox 或非零 attempt/fence 不走新 fallback；旧带 inbox 的 control 历史仍由原 control 分支负责，candidate-local 历史不获得新控制权限。

current subject filter、真实 PG stamp、独立 try-advisory、4/1/256 配额、原 due 分支、同 owner 排序、原 source/lease/fence/expiry/最后时钟都保持。未来/非法进度由复用的选择内核失败关闭，不降成零时刻或重置。没有 provider/自动写、HTTP/客户端/部署/DDL变化。上节 no-inbox 公平缺口是当时准确历史，此节只补当前查询投影及本地单位；不能据此称已原生验证公平运行。

第一轮 query-aware pgx spy 沿实际旧 head SQL 未投影终态进度，选择 A 而不是未服务 B，一项单位 FAIL/exit1，非编译失败。新实际 SQL 后七个顶层、28 个 run/pass（含父/子事件）/exit0，覆盖原 no-history due 断言、三终态/合并语义、max 两种历史、candidate/lease/已有 inbox/非零/不同owner排除、本人筛选、未来/零/越界时钟和实际原 writer SQL 路径。旧 no-history case 的断言和正文保持，只更新名称与当前说明；未重复旧53/45/Run86。

**验证类别为 query-aware UNIT_SPY + UNIT_STATIC，SQL没有执行。** synthetic DEAD_LETTER+attempt0/fence0 是闭集 SQL 契约数据，不声称原 claim 自然产生过该行；当前正常耗尽行带 attempt/fence 和既有 inbox，继续走原历史分支。两轮945 Go/module历史输入清单各自稳定，不称编译精确依赖图或全仓通过。轻量源码/差异/raw/命令见 [终态进度切片](../testing/evidence/outbox-terminal-progress-2026-10-06/README.md)。

整个 AIR020 仍 PARTIAL：通用 root/causation 调度预算、最大链深度及 MemoryUpdated 自触发真实 consumer 未实现；实际 PostgreSQL SQL/锁/多进程配额/公平压测/时钟回退、迁移、full/vet/build、手机与外部服务 NOT_RUN。未更新手机或运行服务，Closed Pilot / Consumer Beta NO。

## 2026-10-07：闭集 Memory 失效 control consumer（AIR020）

新增 `agent-memory-outbox-v1`/`memory-invalidation-v1` 精确分支；旧 Moment 082/091 candidate/effect/inbox guard 本体保留，旧 handler 不能消费 Memory。101 只扩原 outbox/inbox guards、约束与 Memory AFTER capture/index，不建立新授权或效果表。真正事件根由 native source ID/revision/闭集 kind 的稳定 UUID 派生，不接受客户端根/因果权威；child 必须由同事务完成的原父 row/inbox 与相同 fence/attempt/xmin 创建，第三级或自循环被拒绝。

原控制 admission 使用全局 4/每人 1/256 heads；新分支先只提名，metadata→owner/root→Memory→outbox 锁顺序，最后原生钟及 source/lease/CAS 校验，不调用会先锁 outbox 的旧 Moment selector。共同 receipt ID/Person/fence/attempt/time 校验先执行，再分派 Memory state/reason；claim inbox ON CONFLICT 更新零行必须 ErrConflict，不能返回有效 claim。

两个阶段分别 128 unbound preview/100 stale grant，每根 SUM(attempt) 共享上限 6，不随重试或 handler 另起。PENDING/CLEANUP_MORE 是进度；只有实际清完为 INVALIDATION_COMPLETE。父在第六次清完可生成 child，但 child 已无额度时 outbox DEAD_LETTER 保留，绝不报告整体完成。初始 stale/expired/exhausted 终态只有 outbox 更新，可能没有 inbox；此前进度回执保留，不能伪造 claim/receipt 或把它当后来的最终回执。revision 已耗尽的 stale grant 留在 more 查询，因此不能假清完。

已锁定原行/fence 与最后 UPDATE CAS 提供当前控制依据；纯 CheckFence 是结构与 TTL 检查，不是再取一次当前 row。Commit 未知返回空新结果，runner STOP，不盲创建新事件/自动重发效果。相同原 event 的已完成回执只能读历史，不代替当前 ACL。101 down 拒绝已存在 Memory event/progress/receipt，保留历史与不可逆原许可撤销。

单位覆盖闭集 schema/CLI、共同形状、两阶段真实 Tx helper 的 SQL-aware spy、配额/同根耗尽/旧 fence/源版本/迟到/历史与 unknown 出口、runner；migration 为 UNIT_STATIC，没有执行 SQL。原生 PG 提交/锁/时钟回退/并发/fairness/重启、实际迁移/up-down-reapply/数据保留、全量/vet/build/手机均 NOT_RUN。AIR020 PARTIAL；通用根因预算/反思和完整 storm 验收未完成。证据：`docs/testing/evidence/memory-causal-invalidation-2026-10-07/`。


## 2026-10-07：PreferenceUpdated 的有限元数据失效链（AIR020）

- 102 仅捕获原本人私人偏好 writer 的 native 变更元数据。已设置来源使用 `written_profile_version` 与私人行 `xmin`；清空使用已递增的 `agent_profiles.profile_version`、该行 `xmin` 和私人行实际不存在。source ID 为原 personal Agent ID。事件不存字段正文，不推断 RSVP、到访或概率。
- `preference-invalidation-v1` 由原 `agent-outbox-control` 和 Store 的原 claim/consume 入口消费：根阶段有界删除最多 128 个未绑定的旧 `PURPOSE_PRIVATE_PROFILE` 预览；同事务完成回执只能派生唯一 depth=1 子事件，子阶段对原 `consent_grants` 中本人 `TASK_CONTEXT_READ/read/self` 许可做最多 100 个版本 CAS 撤销。不删除绑定、已消费或已发出历史，不声称召回内容，不新增批准权源。
- 复用原 control 的 global=4、owner=1、256 tenant-head 上限。仅先提名，原 native metadata → owner 76033 → root 76034 → 当前私人来源 → outbox 锁序；最后复验原来源、held row/fence CAS、原生 clock 和期限。`CheckFence(c,c)` 是形状/TTL 检查，本身不等于 native fence 回读。
- 同根/子/重试总 attempt≤6，depth≤1，15 分钟源 TTL、30 秒以内 lease；未完成残余保持 PENDING，预算耗尽保留 DEAD_LETTER，revision 耗尽许可仍出现在 more 检查，不能假称全部清完。未知 commit 不交出新 claim/receipt，runner 停止，不自动重发。
- 固定 5 秒内只合并同 owner/Agent/source 的较低版本 configured 根事件，且必须 PENDING、attempt/fence=0、无 lease、无任何 inbox；旧事件仅置 INVALIDATED、保留原身份/来源/root/times。clear、child、已领取与已有回执不合并。此进度不是批准或授权。
- 本切片验证只含直接 Go 单元与 `UNIT_STATIC` 102 合同检查，Tx spy 调用真实 Store helper但不执行 SQL。102 up/down、PG 锁序/并发/隔离/native 时间/真实提交、生产与手机均 NOT_RUN。down 源码拒绝已有 Preference 事件/进度/回执。AIR020 总体仍 PARTIAL，13 事件通用认知、自反思/模型与外部消费者未完成；原 Moment 082/091、Memory 101 的已封存分支/证据不被本切片替代。


## 2026-10-07：101/102 原生验证与参数类型修复

原 PreferenceHandler 和 MemoryHandler 的版本筛选使用 `$4::text`，而真实调用传入 `int64`。隔离 PostgreSQL 实测显示 pgx 无法把版本参数编码为 text，真实父阶段回滚并返回 Unavailable。两处仅改为 `$4::bigint::text`，保留原 JSON 版本字符串比较、源行 epoch、权限、CAS、锁顺序及最后数据库时钟。旧迁移和旧测试不变；新单位中的安全回滚诊断不提交第二份回执。

新 Preference102 原生专项分阶段验证：本人配置/清空的真实捕获与当前源、五秒配置合并且清空不合并、同事务父回执和唯一深度1子事件、旧未绑定预览删除及 sole consent_grants 撤销、已绑定历史保留、源 ABA 不恢复、租户突发排名与实际全局4/每人1配额、501份原076批准产生六次根预算且剩余1份保持可见 dead-letter、真实旧 grant 行等待及本人 metadata 等待后自然30秒租期过期回滚、真实子进程领取后退出及原 CLI 新进程恢复、102空库往返与旧行/xmin/catalog保留、使用后 down 原子拒绝。Memory101另有原本人写→两阶段 Store→真实旧许可撤销专项；未声称 Memory 全部压力/重启验收。

这些结果来自随机独占本地数据库和合成测试身份。配额检查不是持续负载性能结论；进程恢复覆盖领取后退出，不冒称数据库提交回执网络丢失已故障注入。102专项在独占104库中先移除空的102，再验证 pre102 状态；独立103/104保留。所有阶段实际命令、源版本、失败与清理证明见 `docs/testing/evidence/preference-invalidation-native-2026-10-07/MANIFEST.json`。完整 AIR020、通用13事件认知、真实身份/Provider/部署及运营发布仍按原门槛另核，Closed Pilot 与 Consumer Beta 保持 NO。
