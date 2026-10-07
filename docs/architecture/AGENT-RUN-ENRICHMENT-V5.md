# 2026-10-05 AIR017 根代理最终本地验收补注

根代理已独立核证 CODE_AND_LOCAL_VERIFICATION 原验收范围。唯一 live 队列状态由根代理维护；worker 不修改状态。

- 定向 native4：47 PASS，0 FAIL/SKIP/package fail，test/vet/build/两个CLI构建五命令均0。
- 当前 schema90 默认Go包并发完整 `go test ./... -count=1 -timeout=30m -json`：10677 PASS，0 FAIL/SKIP/package fail；test/vet/build/两CLI五命令均0。927输入（924 API+3原seed）当前/不可变执行字节一致。
- 48个旧public行及全部xmin、完整可见语义catalog、090unused down/reapply与测试前后保持；248个实际主库/HTTP/迁移owned测试库由根独立SQL核实不存在。seed smoke原Run为空不替代本任务已执行的非空旧Run/Step/Dispatch/Audit往返。
- 原观察审计50、配置19、上下文预算15等已列父子回归项，以及既有韧性、权限/Run矩阵包含于whole；这是原能力兼容性回归，未新增这些产品能力。数字含原测试父子事件，不等于新增功能数。
- 本轮只验后端，Flutter314源哈希复用核对，未重跑Flutter/analyze/build。phoneRunUI、TalkBack/辅助技术、race、真实provider/模型网络、运营调度与生产未验；旧after_commit原因未记录的UNKNOWN仍不作新归因。

根证据：`work/v5-age038-resume/air017-current-whole-root38hr-root-whole1.json`。原PENDING、开发/fixture/migration首RED与旧失败字节全部保留，后续终态不覆盖历史。源码13 Go/SQL继续固定 `work/v5-air017-recovery/source-freeze1.json`。Closed Pilot / Consumer Beta NO。

---

## 以下为本补注前原文（完整字节保留）

# 2026-10-05 AIR017 当前受控恢复增量

当前本地实现与定向矩阵已完成；根代理 schema90 完整回归待核，状态以唯一 live 队列为准。下方 AIR016 的 2026-10-04 正文为原历史快照，完整保留；其中“耗尽即全部 NO_EFFECT”已被本节更严格的原生核对规则替代。

## 原生有限失败与明确恢复

范围仅原 `STAGE_CANDIDATE`：复用 079/080 具体分析及保留许可、064 原 outbox/inbox、082 原候选 effect 和 083 原 Run/Step/Dispatch/Audit。永久无效输入立即 FAILED；权限/源改变、取消和到期不重试。旧 `ATTEMPTS_EXHAUSTED` 只说明预算耗尽，原通用故障原因没有持久记录，分类为 EXHAUSTED（原因未分类）；`RETRY_UNAVAILABLE` 为 RECONCILIATION_REQUIRED，不能称已证明临时失败。已提交 SUCCEEDED/原 effect 先核对，不降级为重新派发。

090 不改原终态：原 5 次自动尝试之后，最多 1 次本人明确恢复 generation；child 最多领取 1 次，root 与 child 总尝试上限 6。这是 AIR017 新有限策略，不是原083已有能力，也不是模型费用预算。原FAILED行/xmin/审计历史不更新；新增child保留同owner/Agent/Session/authority/source revision/event/logical operation/grant，期限只收紧且不超过原 deadline。原082逻辑效果地址不含generation，因此新child也只能产生原同一候选效果。新 Session、许可、版本或更宽期限不能复活原Run；需要新批准时应沿原领域流程产生新操作，不在此恢复旧Run。

本人接口：`GET /v1/me/agent-runs/failures` 为最多50项历史元数据，有限列表没有分页/完整性承诺；`GET /v1/me/agent-runs/{runID}/failure` 显示本次原生核查状态；`POST /v1/me/agent-runs/{runID}/recover` 只接受 `expectedVersion` 和闭集 `reason=RETRY_TRANSIENT_NO_EFFECT`。固定 `RETRY_TRANSIENT_NO_EFFECT` 是本人明确恢复动作的闭集命令标签，不是对旧失败原因作“临时故障”归因。GET不是执行批准，POST同事务重新核验原具体源和许可/Session/期限；未知、不完整对账、变化或另一generation存在时拒绝。无正文、模型响应、私聊、token/digest、authority proof；旧稳定IDs及版本/期限为操作元数据。当前接口不含客户端DLQ界面，也不批量重放。

## 原生无效果证明、锁与时钟

不能从 error/attempt exhausted 推断 NO_EFFECT。实际核对同owner/Agent/logical operation 的 effect、原event/outbox、consumer inbox、所有相关dispatch。原Run行锁与fence关闭旧guarded082写入，原outbox `FOR UPDATE NOWAIT` 关闭独立候选/消费者写入；仅同一个已持锁并退休Run的原pending行可从检查中精确排除，然后在原效果不存在、outbox=PENDING或UNAVAILABLE且无LEASED/CANDIDATE_STAGED inbox时记录NO_EFFECT。LEASED或已有effect/staged、其它pending、缺原event时保持RECONCILIATION_REQUIRED，不允许人审child。

人工恢复按原current Session/account/Agent → 原grant/source/Task/City共享锁 → 原outbox NOWAIT → 原root NOWAIT执行，遵循082source-first写入顺序，并拒绝反序锁竞争而不长等形成环。实际 `beginContextBuilder` 对account/Session/Agent/profile持共享锁；原enrichment对Task/Moment/City/context共享锁，原retention/analysis grant持更新锁。合法撤权或修改若被这些原锁串行阻塞，不声称可在恢复尾核前成功提交。自然到期仍会穿越等待，所以全部child/checkpoint/audit/FK写入及等待后，最后SQL复核原grant/source/Session/版本和最短PG期限；最后只有commit，无新的DB写/锁。线性化点是末SQL所观察的当前状态，不保证commit解锁或响应到达人类之后瞬时撤权能收回已完成元数据。

090 down先锁四原Run表，再检查使用历史；存在generation/recovery或新增永久reason历史则55000拒绝，原子保留所有数据。无已用新历史时恢复当前089 Run表/guard/unique语义；原086 request_id/default/index/constraint及089审计FK、077/078历史保护未改。真实非空generation0 FAILED/Step/Dispatch/Audit行与xmin已上/下/再上比较，当前089语义catalog准确恢复，不以seed中空Run表替代。

## 当前证据（完整回归待根核证）

`work/v5-air017-recovery/native4/result.json`：47 PASS、0 FAIL/SKIP/package fail；target test、全仓vet/build、两CLI build共五命令exit0。924 API+3原seed=927输入逐字节稳定；fresh090完整public行/all-table xmin、unused down/reapply与可见语义catalog保留，自有库 `birdtie_air017_7f9acc5b1290` 实际删除并独立查询不存在。`source-freeze1.json` 固定13个获租Go/SQL当前SHA，根可在同不可变帧执行默认并发whole。

实际正负：两并发人审只有一个child、root5+child1、原候选effect一次/重读同结果、permanent invalid立即停、child单次、pending/LEASED/confirmed/UNAVAILABLE控制对账、原grant撤销/source-Task-account-profile xmin ABA/跨owner/Session过期/版本理由错误、正常idle刷新、注册HTTP匿名/组织/跨本人/严格命令/重复恢复、真实child INSERT→audit advisory等待跨原4秒批准期限回滚零child/Step/Audit/effect、used-down拒和非空旧089往返。原Run默认OFF/OS kill+原效果恢复/旧后台矩阵一并target执行，没有启用模型或变更原088ModelRun。

首失败保留：compile-red1为新增Record尚未接字段的开发编译RED；native1为090约束名误猜SQL42704（PG自动截断实际保留_key，按原catalog修复）；native2 33 PASS/2失败事件来自同expiredSession fixture SQL42601、vet1为未键名literal；native3 43 PASS/0FAIL、vet1余一处literal；native4全部五命令绿。cwd路径失败属HARNESS，不算功能RED。这些不伪称已复现旧生产授权漏洞。

适用 UX-CHECK-03/07/08/09/10/13/14/16：明确操作/具体版本/未知核对/权限与迟到/可控失败/最少元数据。本轮无新Flutter/手机Run UI、TalkBack、race、部署调度、真实模型/供应商或生产演练；原历史 after_commit 未记录根因的RunUNKNOWN仍不作推断解除。Closed Pilot / Consumer Beta NO。

---

## 2026-10-04 AIR016 原历史正文（完整字节保留）

# Agent Run 与异步丰富（V5 canonical）

2026-10-04；BT-V5-AIR-016 / AGE-065，本轮代码与隔离本地验收通过，按此范围交根队列 DONE。正式发布验收尚未完成。

## 唯一真源与范围

Moment 先经原人类领域服务提交；本任务不向该写链添加 Run、模型、候选抽取或等待。原 064 outbox 捕获原生事件元数据，原 079 分析许可、080 候选保留许可和 082 候选效果事务继续作为权限与效果真源。原 AgentTask 是对话任务；新 AgentRun 是独立、有限的异步步骤进度。一个 Run 当前只执行 `STAGE_CANDIDATE`，不宣称通用工作流或对外推理可用。

083 仅增加 `agent_enrichment_runs`、`agent_run_steps`、`agent_run_dispatches`、`agent_run_audit` 元数据表和闭合迁移。没有原文、私聊、模型响应、精确位置、Session token/digest、隐藏推理或随意字符串审计。原 Session ID 与绑定只在原生内部用于重新核验，不出现在公开 Run DTO 中。候选不等于正式 Memory，不证明偏好或到场。

## 人类授权与操作

本人已登录时可以在原 Moment 提交后显式调度其当前私密草稿；未提供独立具体保留许可时记录 `WAITING_CONFIRMATION`，不能把等待称为成功。本人另行提供已批准且仍有效的原保留 grant，才可调度或用 `expectedVersion` 将当前待确认 Run 排队。后台仅处理此类人类明确调度的原生 Run，不自动收集其他 grant、不绕过原批准、不自动替用户确认。

`POST /v1/me/agent-runs` 接受严格的 `momentId`、`retentionGrantId`（空字符串表示未批准）；`GET /v1/me/agent-runs/{runID}` 读取本人元数据；`POST .../cancel` 仅接受 `expectedVersion`；`POST .../retention-grant` 仅接受版本及原 grant ID。原 source/event/logical-operation/Agent/owner IDs 保留。组织工作区、匿名、跨本人、缺失 Session 和客户端 worker/fence/confirmed/来源内容字段拒绝。GET 不调度，不派生新授权。

## 有限状态与恢复

QUEUED、RUNNING、WAITING_CONFIRMATION、RETRY_WAIT、SUCCEEDED、FAILED、CANCELLED、EXPIRED。四个终态不能复活。首次调度固定 deadline，最长 15 分钟且不超过原 Moment/Session/许可有效期；重试、新 grant、重新登录不得延长原操作。最多 5 次尝试、单租约最多 30 秒。每次领取持久推进 fence；失效 RUNNING 先进入 RECONCILE_EFFECT，再持久领取新的 fence。

两个 worker 使用有限 SKIP LOCKED 领取。原候选 writer 在原 source/许可/outbox 锁之后，锁定并核验当前 Run/Session/绑定/fence。在真实候选、effect ledger、consumer inbox/checkpoint 同一事务中提交 Run SUCCEEDED 和 dispatch COMMITTED；最终检查位于原延迟效果约束及其等待之后。取消、撤权、到期、迟到租约不会另行派发；已提交结果先核实原效果及 Run，不能仅凭 lease 到期盲发。

`cmd/agent-enrichment-worker` 复用原 `BIRDTIE_AGENT_FEATURE_FLAGS` 配置，默认全部关闭，一次最多 25 项、60 秒上下文，不安装调度器，不改外部服务、不调用模型。worker CLI 的开发手机号默认关闭；隔离合成 fixture 可以显式构造同一后端。生产调度、日志值守、真实批准与身份需要独立 LIVE 证据。

## 当前证据与边界

原生首轮 native1：4 PASS / 7 FAIL，暴露 Run guard 将 retention preview ID 混为 grant ID，以及测试尝试绕过原 metadata 版本规则；错误帧保留。修复为明确传入原 grant ID，测试通过原 metadata 递增动作核验变化。native2：11 PASS / 0 FAIL-SKIP，Go target test/vet/build 0；当前 source 817 + 原 seed 3 文件执行帧核验，完整旧 public/catalog/xmin、083 unused down/reapply 均保持，独占隔离库已删除。

已实际 kill 原生子进程于领取之后、候选 effect 写入但未提交之后，再使用持久 checkpoint/fence 恢复；最终只产生一次原候选效果。HTTP、自然期限、最终锁等待、提交后进程丢失结果及完整回归仍在补验。该阶段不能标 DONE，旧 9718 Go / 991 Flutter 属前批已完成执行帧，不能代替本批回归。

适用 UX-CHECK-03/07/08/09/10/13/14/16：直接本人操作、检查具体许可、控制取消/进度、稳定原动作、具体版本批准、未知结果核实、错误保留、会话边界、元数据观测。此次未新增客户端 Run UI，真机 Run/TalkBack 尚未运行。Closed Pilot / Consumer Beta 均 NO。

## 2026-10-04 尾部等待与耗尽队首实测修复（仍 IN_PROGRESS）

独立只读复核发现两处真实缺口，并保留原不可变执行帧：child-wait-red1 实际锁住 `agent_run_steps`，Run 成功 UPDATE 的 AFTER trigger 等待跨过固定租约后原实现仍提交；exhausted-red1 实际五次短租约领取到期，第一项耗尽被误报为空队列，后续原排队项未处理。两轮真实 test exit 1，完整源、日志、隔离库所有权与删除证据保留；其旧 inputFrameScope 是继承081标签误注，overlay实际是 full083-joint1 产品帧，只追加诊断测试，不据此称081执行。

修复：完成全部 Run trigger、Step/Audit/Dispatch 写入和等待后，最后一条只读 SQL 重新核验原具体 grant/Session/源、同一 Run/fence/attempt/candidate/effect、原固定 lease 和 deadline；之后仅原 feature-ticket 检查和同事务 commit，没有后续数据库写入或锁。耗尽队首提交 FAILED、所有未决dispatch NO_EFFECT，返回独立原生控制结果 ErrClaimExhausted；有限 RunOnce 将其计入 Failed、消耗一批处理槽，再继续，真正 ErrNotFound 才表示空队列。没有扩大权限或延长期限。

native6-tail-and-head：108 PASS、0 FAIL/SKIP/package fail，Go target test/vet/build 0；820源+3原seed逐字节冻结，完整旧public/可见语义catalog/参与xmin、083 unused down/reapply保持，ownedDB已删除。native7-child-load：1 PASS，真实固定5秒租约下先观察Step行锁，再实际等待至租约后释放，候选/原效果回滚。全量full083-joint2为9896 PASS/1 FAIL：900ms测试租约在并发负载下于进入目标锁之前已经到期；没有观测到要求的锁，不能按通过算。调整为原生可合法领取的固定5秒测试租约，完整锁等待及回滚断言不改、不续租/造时钟。full083-joint3正在执行，之前full083-joint1真实意图snapshot失败和包10min超时亦保留；当前20min完整运行不跳测试。

手机只安装通过全量Flutter的Debug构建、使用默认全部OFF且不增加082/083的原owned081开发库。原生Run手机不可用/正式Memory/模型出口/部署调度/真人发布仍未验收，Closed Pilot与Consumer Beta均NO。

## 最终本地验收 2026-10-04

完整默认包并发 Go `test ./... -count=1 -timeout=20m -json`：full083-joint3 9897 PASS / 0 FAIL-SKIP，vet/build全仓退出0；当前820原生输入+3原seed逐字节冻结且当前源稳定。083 current-data up、unused down/reapply、完整旧public/可见语义catalog/原参与xmin均保持，最终所有验收fixtures清理后完整public/catalog相同，owned隔离库已删除。之前两轮失败和独立RED保持原样，不以最终成功覆盖历史。全Flutter current-map-intent4 analyze/test/Debug build 0，1037功能+129加载事件，275源稳定；APK e1d9b4a... 已装手机、已真实App/API重启且原数据/xmin一致。手机不增加082/083，仅旧owned081默认关闭库；它证明本地Map/Intent兼容，不冒充Run部署/模型/生产身份或真人Pilot。

根核验见 `work/v5-age038-resume/phone-map-intent-proof36.json`、`latest-worker-root-proof35.json`、`work/v5-air016-runs/full083-joint3/result.json`，完成证据另行存档。当前仅原生显式授权、单步候选并发安全运行能力；真实调度、模型、正式Memory、TalkBack及真人A→H仍未验，Closed Pilot/Consumer Beta NO。


## 2026-10-06：原生领取的公平与额度增量

BT-V5-AIR-020 已接入原 claim 事务：全局4、每本人1，复用持久 Run audit 最近领取时间选择用户，原 fence/源版本/具体许可/候选效果对账保留。容量占用独立记为 throttled，原 worker 不忙轮询、不当空队列。原090根6次、深度1和封闭事件目录保持。详见[实现与边界](AGENT-RUN-DISPATCH-BOUNDS-V5.md)。仅相关单位20事件及原5单位86事件通过；真实PG公平/并发、通用causation调度和短窗口刷新合并尚未完成，PARTIAL，不新开A2A/MemoryUpdated消费。全面/build/手机NOT_RUN，Pilot/Beta NO。
