# AIR015 事务事件队列实施审计

2026-10-03。根代理负责人 root；依据原 live 队列 AIR015、V5 增量协议、认知 ADR 和 Memory/AIR canonical；不是生产发布证据。

## 已核对的实际能力

- AIR014 当前原生 producer 用 pool 读已提交来源；不能直接嵌入 Moment 写事务或在 commit 后双写而称原子。
- Moment 三个人类 writer 已有事务、上下文引用、审计、revision CAS。Withdraw 保留 private/withdrawn 行；物理删除不是已授权的历史事件。
- 005 当前 Evidence 只绑定 ACTIVE EXPLICIT Memory；007 在独立实施人工候选。真实机器分析 purpose resolver、CandidateSubmitter 与候选 effect writer 尚不可用。
- 038 通知 decision/Inbox 与源同事务可借用原则；用户 Inbox 不能当机器 consumer inbox。
- 已审计精确租约：独占 agentoutbox、postgres/agent_outbox 两文件、moments 三 hook、064 两迁移及本任务文档/证据/work。不会改 Task writer、058–063、007 候选表或原事件 registry。

## 实施与验收顺序

1. 使用独立 agent-outbox-v1 metadata/control envelope，闭集三个真实 Moment 变更；source_clock 是原 created_at / updated_at，固定十五分钟 TTL，不使用用户 occurred_at，不持久化私密正文、位置、token 或分析许可。
2. 三个人类 writer 明确 Read Committed，在原 source/context/audit 同一个 pgx.Tx 中捕获当前 Person/exact Agent/native metadata 与 source revision。缺 metadata 不创建 Agent；outbox SQL 失败使原事务失败。
3. 064 三张独立控制表、immutable event identity、当前 native capture guard、单调 lease fence、按 event/subject/handler 去重。effect ledger 写入硬拒绝。
4. 内部控制领取使用 FOR UPDATE SKIP LOCKED；当前源重新锁定/核验、真实 DB 时钟、三十秒有界 lease，失效/过期/耗尽关闭。超时仅同 handler 恢复，v2 在 UNAVAILABLE 后单独控制回放，不继承分析许可。
5. 原子 checkpoint + consumer receipt，旧 fence/worker/期限不能提交。真实消费者仍 UNAVAILABLE、effect 0，没有运行时、网络 exactly-once 或实际候选成功。
6. 随机自有库最终001–063基线、064 fresh/current/down/reapply，全旧 public 内容保持；真实 Store writer 故障回滚、提交后重连、租约超时、并发领取、重复100次、handler升级、source/身份撤权与硬关闭 effect 正负测试。

## 已运行的本地证据

原始命令、失败、源快照与完整行比较保存在 [正式归档](../testing/evidence/agent-transactional-outbox-2026-10-03/README.md)，对应原工作路径 `D:\Project\birdtie\work\v5-air015`。本轮使用本机 Docker PG、随机自有 `birdtie_air015_*` 隔离库、001–063 +三 development seeds +明确保留旧合成数据，再应用 064。没有修改真机/shared/生产库；原始数据均为 LOCAL_SYNTHETIC_ONLY。

| 阶段 | 实际结果 | 准确分类 |
| --- | --- | --- |
| domain round1 | 162 PASS /45 FAIL /1 package FAIL，exit1 | StableEventID 真正生成 68 字符，非 UUID；另有标准 JSON 边界测试错误 |
| domain round2 | 221 PASS /9 FAIL /1 package FAIL，exit1 | 剩余断言误要求标准 json.Unmarshal 在未调用自定义方法时清空 receiver/保留外层字节 |
| domain round3 | 226 PASS /4 FAIL /1 package FAIL，exit1 | raw decoder 字节上限与标准库外层空白归一边界尚混淆 |
| domain round4 | 230 PASS /0 FAIL/SKIP，exit0 | 修复 UUID 并分别验证严格 raw decoder 与标准库边界；target vet/build exit0 |
| native1 | runner 在 Go 测试前失败，没有 result JSON | 053 已自动建立 metadata，runner 又 INSERT 同一行；fixture/runner 错误，不是产品原生测试结果 |
| native2 | 255 PASS /2 Test FAIL /1 package FAIL，exit1 | context_change 子测试使用非法 location_precision，触发原 moments check 23514；保留子/父 FAIL，不削弱领域约束 |
| claim-clock-red1 | 0 PASS /1 Test FAIL /1 package FAIL，exit1 | 真正提交过期 LEASED 并返回 nil；2.4 秒 PG receipt trigger 等待复现遗漏最终时钟检查 |
| native3 | 258 PASS /0 FAIL/SKIP，exit0 | fixture 和 Claim 最后 clock 修复后实库复验；全源/完整旧行稳定 |
| native4 round1/2 | 每轮 260 PASS /0 FAIL/SKIP，exit0 | 230 pure +30 PG，含新增 Subject selector 与 Consume 最后 clock 场景；target vet/build exit0 |
| root current064-full1 | scope260；默认全 Go3轮各6654 PASS /0 FAIL/SKIP，exit0 | 全仓 vet/build exit0；490全API源、13owned、全部public行稳定；自有库DROP |

计数为 Go JSON 的 Test 父/子事件，排除 package PASS；失败的父测试仍计入 Test FAIL。编译-only 与错误 gofmt/日志目录创建命令不算功能通过；均保留原始日志，未删除失败来制造全绿。

native4 13 个 owned 生产 Go/SQL 文件前后 SHA 相同、归档时与原窗口相同。曾口头误报 15，是把领域 6 Go 和 2 SQL 重复计数，现据原清单纠正；不改历史 raw。全 API hash `allAPISourceStable=false` 仅对应并行 AIR027 三文件变动，before/after 原件可复核；不能宣称 native4 全仓源冻结通过。

后来根独立 `current064-full1` 已完成完整回归，正式 receipt 从 raw JSONL 重新数三轮各6654 Test PASS、0 FAIL/SKIP，18个无测试包明确排除 Test SKIP。原 runner result fullVetExit/fullBuildExit 均0。490 全源 SHA before/after 相等，13 owned 亦相等，各轮完整 public snapshot 和最终 before/after 一致，随机自有 `birdtie_air015_93bf2a1a5fe4` 清理日志明确 DROP。原初步“正在运行”归档快照保留为历史，追加收尾 receipt；不以完整编译/回归通过宣称推断消费者可用。

## 真正覆盖的实现与边界

- Create/Update/Withdraw 三个真实 Moment writer 与 source/context/audit/outbox 同 tx；BEFORE/AFTER outbox 故障使整笔原生事务回滚。Create 的日期来自真实 created_at，而非 fixture 用户填写的 2017 occurred_at。
- native 当前 source revision/status/owner、active PERSON/exact PersonalAgent/既有 metadata 与当前 link 指纹实际核验；缺 metadata 不创建、源物理删除不造 deleted 事件、停用后恢复仍不复用旧 xmin token。
- 8 并发 Claim 单一 fence；真实过租约后同 handler 恢复、旧 worker/fence/agent/subject/handler/lease 拒绝；pool 默认 RR 不改变显式 RC writer/control。
- 100 次同 handler 重放保留单一 receipt；升级仅第二个控制 receipt，effect=0。真实 checkpoint PG trigger 错误回滚后可重试；原 effect INSERT 以 55000 拒绝。
- Claim 和 Consume 最后 receipt 写之后重新取 PG clock；真实 2.4 秒延迟越过短 source deadline 后拒绝并回滚。这个测试是数据库期限检查，不是网络交付后绝对撤回承诺。
- subject selector 仅维护过滤、不成为权限，实测不领取另一主体任务且 Org/Business/零 UUID 拒绝。
- 001–063 旧 public 全行在 064 up、非空 down 拒绝、空 down、reapply 及 native 测试后保持；三张控制表非空 down exit3，空 down/reapply exit0；最终自有库 DROP 日志有精确库名。

SQL schema guard 能验证 native Moment 关联形状，不能独立计算可信 fingerprint 或授机器 analysis-purpose。迁移保护中的 raw SQL guard fixture 仅证明 DDL，不算原生业务效果；真实 writer 正例单独来自 Go+实际 PG。

## 原 acceptance 逐项结果

| 原要求 | 当前证据 | 判定 |
| --- | --- | --- |
| 领域变更/待处理事件同事务 | 三个原生 Moment writer 与同 tx 故障回滚 | 本期所选来源通过；其它域未接入 |
| event_id + handler_version + subject 去重 | 真实 inbox +100 replay +升级控制 receipt | 控制层通过 |
| 稳定 logical_operation/action effect_key | pure 稳定地址不含 handlerVersion；效果表严格拒写 | 地址/拒绝边界通过；真实效果未实现 |
| 崩溃/重启/过期 fence 恢复 | PG trigger 故障、实际 lease timeout、重建 Store、最终 clock | 控制层通过，不是已部署 worker 运维 |
| 重放/升级后单一真实候选/效果 | 尚无 current analysis-purpose/candidate effect writer；effect 永远0 | **未满足**，空表不能代替一次业务效果 |
| 兼容迁移/默认并发全 Go | 064 本地 up/down/reapply/旧全行保持；root最终默认全Go3×6654 PASS、vet/build0、490源稳定 | 本地兼容/完整回归通过 |
| 部署 consumer/全域 hooks/运营对账 | 无运行部署、调度、观测和其它域 hooks 证据 | 未实现/未验收 |

## 当前验收分类与恢复条件

**PARTIAL**。恢复要求为：实际当前 analysis-purpose/source resolver；可信 CandidateSubmitter 与真实 effect writer 的同事务/可靠提交；100 重投/版本升级/故障恢复仍一个真正候选效果；相应其它域 hooks、部署消费者和运行监控另具实际证据。不能靠客户端 owner/confirmed、源 authorID、租约、空账本、注入 spy 或 pure fixture 解锁。

本任务无新增消费 UI，Flutter/真机/辅助技术未运行；无生产部署、外部调用或合作方联系。完整001–064默认Go3轮已由 root 在独立窗口运行并归档收尾 receipt，原失败与中途 pending 记录均保留。Closed Pilot / Consumer Beta 仍 NO。


## 2026-10-05 原任务恢复：安全 failure-only 初次 Envelope 观测

前文保留历史阶段证据，不据陈旧“无outbox/inbox”goal扩大本轮。原任务来源/acceptance/history/gates由根保存。091安全点实际 whole10735 PASS 后，root38jq增授五个精确Go、本独占work/evidence与两份已有canonical；当前增量为 failure-only观测，不自行解除原PARTIAL/IN_PROGRESS或原发布门槛。

work-only诊断已由根38jp核实1988冻结文件，53PASS/five commands0，全部原940live字节不变、五对全public/xmin、4自有库SQL absent；其初次失败仅明确controlled future，自然capture=0。此轮也不修改该冻结work/raw/README/旧38p38s。

真正生产增量为四个新Go与agent_outbox.go两条原失败后调用。agentoutbox复用原私有helper返回32bool；postgres只WARN固定消息/initial或retry phase/INVALID、EXPIRED、UNKNOWN/固定conditions，不输出ID、正文、digest、token、精确源时刻或自由错误字符串。成功0log；不增PGclock/retry/sleep/tolerance、不panic或改变原错误；model.go、current sourceSQL、NewPending、原4000diagnostic Test保持。观测函数不是授权/来源resolver/校验决定器。

实际unit1为独占旧940基线加本任务4新Go，944冻结输入，47PASS/0FAIL-SKIP/package failure，原model与4000Test字节保持、producer差异仅2调用。32条件矩阵逐项与原ValidateEnvelope/NewPending对照，future/expiry/oversize零Record，日志完整allowlist及synthetic pgx初次/retry/成功控制流通过。无DB使用；pure retry不是nativePG复现。当前native与共同092whole仍未运行，需根统一冻结后核验，不借旧091绿覆盖007新092代码。

新native设计复用原owned fixture，当前Create/Update/Withdraw全32true、成功0warning；受控未来仅OccurredNotAfterReceived=false、原guard拒绝/零Record/仅一initial warning，非空Memory及全public rows+xmin的拒绝和rollback完整对照。artifact变量可选，缺值不使普通whole失败。native通过也不能证明原38p时钟回退；原初次ErrInvalid精确自然首因 **UNKNOWN**。

原始pure命令/raw/sourceSHA与后续验收分别见 [新独占证据](../testing/evidence/agent-outbox-initial-diagnostic-2026-10-05/README.md)；不将实现观测切片写成完整AIR015 DONE。模型/自动动作/真机/发布门槛保持，Closed Pilot/Consumer Beta NO。


## 2026-10-05 当前092 whole终态后的原AC逐项复核

只读代码/原raw核验；没有新测试运行。根whole已真实终态，文档增量由根授权；任务状态由根决定。原goal“无outbox/inbox”与早期PARTIAL缺口是历史材料，不作当前代码事实。

## 原范围与原始证据

- 当前队列原source_requirements来自 `work/v5-materials/BT-V5-AIR-BACKLOG.json`；completion_scope=CODE_AND_LOCAL_VERIFICATION，external_gate_ids=[]；depends_on AIR014/INT001目前均DONE。
- 原文字要求领域变更/待处理事件同事务或等效机制、event_id+handler_version+subject、稳定logical_operation_id/action_id效果键、故障恢复、100重复单业务效果、handler升级不重复效果、兼容迁移。没有全部13类型接线、概率校准或生产部署条款。客户端verify为“涉及客户端”条件；本次I15只新增后端failure观察。
- 当前whole：`work/v5-age038-resume/age007-hiking-root-verification/root-whole2/tests.jsonl`，SHA256 `b906e2abe1e5dfe23a0f8be573d7ed74a352c75fb4d665a273eff1ba12d91570`；result SHA256 `296475428f145d1ee1ab20dc48cae9588fcab0520394c5396c319e6bc6e6872f`。
- 原cmd为go test ./... -count=1 -timeout=30m -json，实际10797测试PASS/0FAIL-SKIP/pkgFAIL；vet/build/controlCLIbuild/runCLIbuild均0。Go无TestFiles包的package skip事件不计测试skip。946输入=943API+3seed，实际框 `joint092-source38km`。根proof `hiking-whole2-root38kr.json`核48旧行/all xmin/semantic catalog/unused092 roundtrip/307实际raw-owned DB不存在。
- 下表数字为raw内该top-level Test及子Test的pass数量，不是100循环次数。100循环和非零候选/effect由对应原测试源码断言确认。

## 精确 AC 对照

| 原项 | 实际代码 | 实际测试及whole包含证明 | 剩余本地实现判断 |
| --- | --- | --- | --- |
| 原领域变更与outbox原子 | `apps/api/internal/postgres/moments.go` CreateMomentDraft/UpdateMomentDraft/WithdrawMoment及同Tx append；`postgres/agent_outbox.go`原current元数据 | `postgres/agent_outbox_integration_test.go` TestAgentOutboxNativeThreeWritersMetadata(1)、TestAgentOutboxOriginalCommitAtomicFailures(3) | 所选三个真实Moment mutation满足；原source/context/audit/outbox故障共同回滚。没有把BEFORE/AFTER INSERT injection说成真实OS死亡 |
| event/handler/subject inbox去重及实际CandidateSubmitter | `postgres/agent_candidate_pipeline.go` StageOwnCandidatePipeline同Tx源末核、063CANDIDATE/064effect/inbox/outbox；BindCandidateSubmitter只承认原批准grant | `postgres/agent_candidate_pipeline_integration_test.go` TestCandidatePipelineNativeSubmitterIsActualAndRefsCannotGrant(1)、TestCandidatePipelineNativeExactlyOnce100AndHandlerUpgrade(1) | 非零候选/effect真实落库，原100回放稳定；旧TestAgentOutbox100ReplayAndHandlerUpgradeZeroEffects仅control，不用于此AC |
| 稳定logicaloperation/action键、handler升级 | `postgres/agent_candidate_pipeline.go` pipelineKey；`agentoutbox/control.go` EffectKey | 上述ExactlyOnce100测试明确v2仍返回原handler效果/完整pipelineCount不变；`agentoutbox/control_test.go` TestAgentOutboxStableEffectKeyIgnoresHandlerUpgrade | 不复制账本、不换key伪造新逻辑操作、不续grant/源期限 |
| commit前/消费中途故障和可恢复状态 | 原事务hook在after_claim/candidate/effect/inbox/checkpoint/before_commit；pgx.Tx rollback和native receipt | `postgres/agent_candidate_pipeline_integration_test.go` TestCandidatePipelineNativeFaultAtomicityAndControllerLate(9)、TestCandidatePipelineNativeProcessCrashRollsBackAndRestartReconciles(6) | 真实子进程os.Exit86在五消费点后事务未commit，完整状态不变，重启原grant提交一次；hook故障与OS退出分别记录 |
| commit后结果未知/对账 | 原native Stage commit，GET原grant receipt不依赖原内存Service | `httpapi/agent_candidate_pipeline_integration_test.go` TestCandidatePipelineHTTPNativeLostResponseUsesOriginalReceipt(1) | 实际handler提交成功后hijack关闭loopback，客户端网络错误；GET得到原非零效果，再同key提交完整集合不变。没有网络全链路exactly-once承诺 |
| 未知/过期/撤权/跨主体、权限不转移 | 原079分析+080候选保留目的独立；current原Session/Task/Person/Agent/source/version/xmin/lease/flag SQL及最后PGclock | `postgres/agent_candidate_pipeline_integration_test.go` TestCandidatePipelineNativeCurrentSourcesIdentityAndLease、RealWaitRejectsCurrentGrantSourceAndDeadline(9)、MissingGuardAndOriginalIntentConflict(8)、SubmitterIsActualAndRefsCannotGrant(1)；079/080原current/ABA/finalwait测试均在whole | guard存在/撤权/期限严格拒绝，零Receipt及完整candidate/effect/inbox/outbox不变；metadata、digest、source refs或confirmed不成为批准 |
| 兼容迁移 | `apps/api/migrations/064_agent_transactional_outbox.sql/.down.sql`、079/080/082既有配对迁移，当前091/092增量 | `postgres/agent_candidate_pipeline_integration_test.go` TestCandidatePipelineNativeUsedDownAndTTLFullPayloadCleanup(1)、`postgres/agent_memory_candidate_integration_test.go` TestMemoryCandidateNativeCurrentDataMigrationRoundtrip(1)、`postgres/agent_multi_candidate_pipeline_integration_test.go` TestMultiCandidateNativeCurrentDataSingleMemoryAuditAndFullXminRoundtrip(1)；root fresh001–092及非空091→092→down/reapply48行/xmin/catalog | used082拒绝删候选/effect/handler历史，unused依赖逆序往返并精确恢复；current091往返含非零旧singlecandidate/effect、explicitMemory、Run/Step/Dispatch/audit。不会借092零新表省略迁移兼容 |
| 091多源扩展保留原原子/对账 | `postgres/agent_multi_candidate_pipeline.go`复用原purpose/候选/effect/inbox，不把human Save当producer | `postgres/agent_multi_candidate_pipeline_integration_test.go` TestMultiCandidateNativeAtomicRollbackFaultMatrix(7)；`httpapi/agent_multi_candidates_integration_test.go` TestMultiCandidateHTTPNativeUnknownOriginalGrantReconciliationNoReplay(1)及独立OSreceipt child | 扩展证据补充原AC；不以新multi替换单源100/handler测试，不自动写Memory |
| OFF/本地边界 | 原agentfeature Controller默认OFF；原human preview/明确accept与候选保留分开；server已注册明确许可入口 | 原Service OFF/realPG no-write、HTTP权限正负均whole；I15新五Go与source-freeze1逐字节相同 | 无模型出网/真实自动Agent写/视觉/A2A/deployment。未新增本任务客户端。007新Dart恢复另框验收 |

## 首因与诊断的准确关系

原 `work/v5-age038-resume/full-now004-native38p/tests.jsonl` revoke_guard子Test在pipelineNative fixture初次NewPending失败，此时guard还没禁用；原32条件未输出。因此精确自然字段原因 **UNKNOWN**。此前natural_expiry测试的原450ms选择deadline在Stage前已过期，当前fixture选既取PG原5s deadline并等待真实expire，原guard/零副作用断言保留。两项不得合为一个时钟猜想。

本次safe failure-only观测复用原私有校验helper、原已取Envelope/now，32bool；初次/retry闭集phase+INVALID/EXPIRED/UNKNOWN，成功0WARN，不输出源精确时刻、ID/token/digest/body。没有额外PGclock/retry/sleep/tolerance或批准；原model.go和4000 Test保持字节。自然capture=0，当前唯一future warning是人工受控负例；pure fake retry仅控制流。它不能解释、修复或证明原38p自然cause。

`agentoutbox/envelope_diagnostics_test.go` OriginalPredicateMatrix覆盖全部32条件/future/expiry/oversize零Record；`postgres/agent_outbox_initial_capture_integration_test.go` StructuredLogAllowlist、AppendFailurePhases、NativeCurrentMutations、NativeFutureSourceRejected均raw top-level pass。I15native100/五0与五对136表全行+xmin/4actualSQLabsent独立记录在native1；旧worker-native1的2015文件完整hash保持。本次whole也包含原4000 Test及自然原负向用例，当前正常/异常contract符合原AC；这些绿色结果不用于倒推38p原因。

原代码/本地AC各项已有具体证据；未识别当前必须补的production实现或原native验证缺口。历史未解释诊断保留为UNKNOWN，和“原本地AC当前满足”的判断分别记录，由root核定正式终态，不自行done。

## 尚未接线的闭集现状（不追加本任务验收）

agentoutbox/model.go与064仅MOMENT_CREATED/UPDATED/WITHDRAWN，production append调用只有moments.go三处。agentevent/registry.go是不同air.event.v1 metadata catalog；postgres/agent_events.go与agent_enrichment_events.go callable snapshot producer不自带outbox hook。其八个current类型尚未接此outbox：

| 类型 | 原native表 / 当前writer精确路径 |
| --- | --- |
| UserQuery | agent_tasks；postgres/agent_workspace.go SaveTask/UpdateTask |
| ActivityJoined / ActivityLeft | activity_participations；postgres/activity_participations.go JoinActivity/CancelParticipation及Bound版本 |
| PlaceSaved | saved_items；postgres/saved.go Save/SaveBound |
| CommunityJoined / CommunityLeft | community_memberships；postgres/community_social.go JoinSocialCommunity/LeaveSocialCommunity |
| ProfileUpdated | user_profiles；postgres/profile_edit.go UpdateOwnProfile（human session wrapper在profile_human_access.go） |
| PreferenceUpdated | agent_private_profiles；postgres/agent_private_profile.go ReplaceOwnAgentPrivateProfile |

ActivityCompleted/PlaceVisited没有真实attendance/completion/visitfact，仍UNAVAILABLE；MomentDeleted catalog语义为retained withdrawn，并非物理删除。没有从登记metadata推断历史动作/事实。原source/AC未要求八项全hook，也没把生产consumer/scheduler部署、运营SLO/监控/校准写作本任务无外部gate的必需本地条件；若以后另有明确既有source授权，必须重新审各域事务/authority/lease，不直接扩此任务。

Closed Pilot / Consumer Beta均NO；运营部署、007新客户端/校准/真机不由本次后端证据完成。所有旧raw/历史/报告保留，livequeue和共享总报告仅root更新。
