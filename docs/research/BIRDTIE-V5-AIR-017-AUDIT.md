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

# BT-V5-AIR-017 原生失败恢复审计与实施

2026-10-05。原任务依赖 AIR010/AIR016/INT001均由根核证DONE；原source/AC/verify/gates保留，17精确范围由根授予。只在原083Run及原082效果/064控制之上增量，未新建授权或效果台账。当前原AC本地矩阵通过，根whole090待核；worker不改队列或共用报告。

## 已实现与验收映射

| 原要求 | 实际实现/证据 |
|---|---|
| 永久失败不重试 | reconcile INVALID_INPUT立即FAILED；权限/到期沿旧终态；native PermanentInvalidStopsImmediately与旧撤权/fence用例 |
| 有限临时重试/耗尽DLQ | 原5自动保持；耗尽=EXHAUSTED，未知通用故障不臆定临时；原FAILED有限本人list及具体GET |
| 人审可控恢复 | expectedVersion+闭集reason；原Session/Agent/grant/source不换；090一个generation/root≤6/child≤1，双并发一winner |
| 重放重新核对源/授权 | 原retention+analysis native capture及末082current SQL；source/Task/account/profile ABA、合法撤权、Session过期0child |
| UNKNOWN不盲发 | Run/outbox锁内ledger/inbox/dispatch证明；LEASED/staged/其它pending隔离；原effect同op一次 |
| 无私密重放日志 | 只原元数据Record、闭集中文说明；未存query/raw body/model/error；注册HTTP完整严格wire |
| 保留历史与回退 | 原FAILED json/xmin/audit完全相同；090unused非空089 roundtrip；useddown55000拒 |
| 等待后期限 | actual childINSERT在checkpoint audit BEFORE INSERT advisory等待，pg_blocking_pids确认；过原固定4s批准期后末核整tx回滚 |

实现位置：`internal/agentrun/failure_recovery.go`（closed分类/命令/metadata）；`postgres/agent_run_recovery.go`（当前proof+同txchild）；原`postgres/agent_runs.go`仅分类与no-effect精确退休；`httpapi/agent_runs.go`三本人路由；090up/down保留旧DDL。原082candidate writer/064消费者/088ModelRun未改。

锁的依据及有限list/无原始故障持久细节见唯一canonical `docs/architecture/AGENT-RUN-ENRICHMENT-V5.md` 当前AIR017节。固定 RecoveryReason=RETRY_TRANSIENT_NO_EFFECT 只表达本次人类明确恢复命令，不推断历史故障为临时。历史reason仅闭集元数据，不是故障原因或可执行grant。恢复入口featureOFF只允许人类查看/明确元数据命令；实际Claim/082当前默认OFF仍零派发。

## 可复现命令与真实过程

从repo执行 bundled Python `work/v5-air017-recovery/verify-native90.py --round <新名称> --pattern '^TestAgentRun'`；自建随机隔离库，真实001–089+3seed→090unuseddown/reapply→完整不可变APIcopy执行目标Go，生成commands/result/所有权/清理。不得覆盖已有round。

- compile-red1：新增字段开发编译失败，未执行业务断言。
- native1：090 SQL42704 原unique实际名不等于简单截断。原catalog证明后更正当前090，旧raw留存。
- native2：33 PASS，2失败事件=expiredSession一叶子+父SQL42601；vet1新外包未键名literal，build/CLI0。修fixture AS与literal，未改Session约束或权限期望。
- native3：43 PASS/0FAIL-SKIP；vet1剩余一未键名literal，其他四命令0。原lease内完成修正。
- native4：47 PASS/0FAIL-SKIP/pkg，五命令0；924API+3seed=927稳定、完整public/all表xmin/catalog与090unused回退保持、主库DROP且不存在。
- 准备HTTP追加时一次cwd重复apps/api失败，记录 `harness-cwd-diagnostic.json`；未写文件，不计业务RED。

原完整whole待根按冻结帧执行；当前目标也实际包含旧Run进程kill/两worker/fence/默认OFF/最终Step锁等待矩阵。父子owned migration DB清理沿现成ownedMigrationDatabase，根将独立核真实名字及缺行；本worker不伪造未保存的dbname。

## 未验收/限制

正式模型/provider/生产/部署/实际调度/真实账号、phoneRunUI、辅助技术与race未运行；原UNKNOWN原因未记录仍UNKNOWN，不顺带修或推断。无通用工作流、未将064控制UNAVAILABLE或088费用UNKNOWN作为业务重放批准。没有批量重放、全量DLQ分页，也没有新private字段读口。只有当前明确STAGE_CANDIDATE途径的CODE_LOCAL证据；Closed Pilot与Consumer Beta NO。
