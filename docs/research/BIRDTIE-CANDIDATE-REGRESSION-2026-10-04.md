# AIR015 候选回归审计（38s）

## 范围与原失败

原任务 `BT-V5-AIR-015` 的回归恢复，仅修改 `apps/api/internal/postgres/agent_candidate_pipeline_integration_test.go`。未修改生产验证、错误分类、领域模型或 DDL。原 `38p` 850 文件执行帧、两项叶子失败及父失败事件保留在 `work/v5-air015-regression38s`。

原整包 `full-now004-native38p`：10080 PASS、4 FAIL 事件（两个叶子及父用例）、0 SKIP，test exit1；vet/build exit0、原完整 public/catalog、迁移及自有数据库清理通过。

1. `RealWaitRejectsCurrentGrantSourceAndDeadline/natural_expiry` 在进入真实等待前，具体保留预览已过期。原期限为主机时钟起算 450ms，覆盖预览、批准、等待准备全部成本。原叶子报告 0.28s，故不能仅凭总耗时推断其具体过期分支或时钟差。
2. `MissingGuardAndOriginalIntentConflict/revoke_guard` 在 `pipelineNative` 创建原生 Moment fixture 时返回“事件控制记录无效”，尚未禁用撤销护栏；不能当作撤销护栏验证失败。静态路径为 `CreateMomentDraft → appendMomentOutboxTx → NewPending/ValidateEnvelope`，具体不合法字段在原日志中没有记录，原因 **UNKNOWN**。

## 计划与已实现修复

先保存原始字节和失败日志，再做不改变验证规则的诊断副本；期限用例必须先获得有效原许可，随后在真实锁等待中跨越这个许可原期限，最后证明零候选、零 effect、零 inbox。

- `natural_expiry` 仅一次从 PostgreSQL `clock_timestamp()` 选择 5 秒具体期限，仍走原 Preview/Approve。
- 批准后检查原 grant 尚有效且不晚于原用户选择期限；未重获 grant、续期或改数据库授权。
- 等待屏障额外匹配实际 locker PID，保留原 Moment 表真实锁等待。
- 持锁期间按 PG 时钟等待原 `grant.ExpiresAt`；10 秒上下文只是测试等待上界，不作为授权时钟。
- 保留原错误、空 Receipt、零 effect 断言，并增加候选与 inbox 合计为零。其他短期 before-commit 过期、安全护栏和故障矩阵未削弱。

该变更解决测试将审批成本混入短自然过期窗口的问题；它不证明原 38p 的确切时间链已经复现。

## 已运行结果

| 帧 | 实际结果 | 结论 |
| --- | --- | --- |
| diagnostic-native1 | 缺原 dev-seeds，未运行测试；owned DROP | 初次 runner 失败保留 |
| diagnostic-native2（原 450ms，12次） | 46 PASS /2 FAIL 事件，test1，vet/build0，public/catalog 同，DROP | 一次在批准阶段跨过实际 PG 截止；不是原 preview-expired 分支的准确复现。12次 revoke_guard 通过，未捕获 ErrInvalid |
| diagnostic-native3 | 200个独立原生 Moment 草稿，1 PASS，vet/build0，public/catalog 同，DROP | 未复现 source envelope 错误；无失败后重试、无改验证 |
| current-native4 | 26 PASS /0 FAIL-SKIP，test/vet/build0，850源稳定，public/catalog 同，DROP | 初版期限修复与原故障/guard 验证 |
| current-native5 | 116 PASS /0 FAIL-SKIP/pkgFail，test/vet/build0，850源稳定，public/catalog 同，DROP | 最终唯一 test 版本及 CandidateRetention/Pipeline 关联矩阵 |

最终测试 SHA256：`93f1a3e71a7766521c547b2ca9313cf15ea583dcbe2eaddad4d6e2a3f860338f`。

这些是原 root38p 完整复制帧叠加唯一测试变更的结果；并行移动的 live 仓库不因此获得整包稳定或通过声明。

## 诊断整包及未测范围

`whole-diagnostic1` 正在独占复制帧执行 `go test ./... -count=1 -json -timeout=20m`、`go vet ./...`、`go build ./...`。副本 `agent_outbox.go` 只在原 `NewPending` 拒绝时输出截止、revision/status/kind 和闭集布尔，排除 sourceBody、令牌、连接串和原始主体 ID；没有新重试、sleep 或 validation 改动。它是 **DIAGNOSTIC_NOT_IMPLEMENTATION_NOT_FINAL_ACCEPTANCE**，结果另行补充，不能替代根代理生产字节验收。

本轮两项原失败叶子已实际通过：自然到期选择时 PG 为 `12:28:31.941605Z`，原期限 `12:28:36.941605Z`，批准后 PG 为 `12:28:32.107443Z`；真实持锁后 PG 为 `12:28:36.952790Z`，原截止已经到达，零副作用断言通过（叶子 5.53s）。`revoke_guard` 0.62s 通过且未捕获初始 envelope 非法字段。整包尚未结束，以上叶子结果不替代整包终态。

运行期间新增四项真实诊断 runner 准备失败：三个 `TestAgentOutboxMaintenanceNative*` 缺 `BIRDTIE_OUTBOX_CONTROL_BINARY`，一个 `TestAgentRunNativeActualCLIIsBoundedDefaultOffAndRedactsConfiguration` 缺 `BIRDTIE_AGENT_RUN_TEST_BINARY`。根代理原整包 runner 已真实编译这两个程序；本诊断 runner 从定向脚本改造时漏接两项。原缺失断言不跳过、不修改，第一轮完整原始结果保留；下一轮仅补 `./cmd/agent-outbox-control`、`./cmd/agent-enrichment-worker` 同帧构建、二进制 SHA 和既有 env 注入，再执行全包诊断。

`whole-diagnostic1` 最终：10080 PASS /4 FAIL /0 SKIP /1 package FAIL，test1、vet/build0；850复制源稳定，完整 public/catalog 相同，owned DROP。未捕获初始 Invalid。`whole-diagnostic2` 已真实启动，并实际构建两个所需 CLI（均 exit0，SHA 记录在本轮 `*-binary.json`），源码仍同一诊断帧，未新增领域修复；终态待补。

第二轮期间出现范围外真实失败：`TestAgentHTTPResponseSafetyPersistenceIntegration`，`agent_action_safety_test.go:496` 的本人 Task GET 返回 `409 result_source_changed`，原断言为 200（叶子 0.59s）。没有更改或降低该断言；已将原日志位置交根代理和 NOW004 唯一生产 writer 审查。第一轮相同源码该用例通过，不能据此断定环境或 fixture 污染。第二轮继续保存完整终态及数据库清理。

第二轮终态：**10083 PASS /1 FAIL /0 SKIP /1 package FAIL**；test exit1、vet/build exit0、两真实 CLI build exit0，850复制源全部稳定、完整 public/catalog 相同、owned DROP。唯一叶子为上述范围外 HTTP409。本次没有初始 `NewPending` 非法字段观测；原候选两叶子均通过，四个 CLI 前置用例均实际通过（含真实双进程与退出/恢复）。此轮不称整包通过，不称生产字节整包验收。

根代理无观测器整包 `38v` 另有 Run `after_commit` 失败：真实 child 返回 `RETRY_WAIT/RETRY_UNAVAILABLE`，未发 READY；原固定2秒 claim 的实际 LeaseUntil/Stage 原因没有记录，具体因果 UNKNOWN。已只读保存原日志并反馈根代理；它与 HTTP409、AIR015 原 envelope Invalid 三条路径分开，不修改 Run 生产或测试。

本切片交付为已证实的 PG 原截止自然过期 fixture 修复和严格零副作用回归；唯一 live 测试源已冻结，全部旧失败保留。队列状态和范围外缺陷后续由根代理核证决定。

源 envelope 错误在实际字段证据出现前保持 UNKNOWN；真实用户、生产模型、真机、TalkBack 与发布验收未运行。Closed Pilot / Consumer Beta 保持 NO。本次无界面更改；适用 UX-CHECK-06（未知、过期与实际结果）、08（撤权和版本）、09（零重复副作用）、12（原 CLI 重启检查）、16（诊断不保存正文或令牌）。这些是原域回归核验，不是新界面或发布验收，不新增许可来源。
