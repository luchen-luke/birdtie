# AIR015 原生清理命令回归（2026-10-04）

## 范围与依据

原 BT-V5-AIR-015 经 root 显式重开；仅修改 `apps/api/internal/postgres/agent_candidate_pipeline_integration_test.go` 的 `TestCandidatePipelineNativeCleanupActualCommandRepeatedConcurrentRestart`。生产许可、079/080/082 DDL、错误映射、候选消费及原短期限拒绝用例均不修改。AGENTS/V5 协议与 UX-CHECK-05/06/07/10/12/16 的具体批准、期限、撤权和证据要求继续有效。

## 真实失败及原因界限

root `work/v5-age038-resume/full-e2e004-root38e` 原结果为 10032 Test PASS / 1 FAIL / 0 SKIP、1 package FAIL；唯一清理 CLI case 的 Stage 返回 ErrUnavailable。原完整失败帧、原 800 ms 源和原字节失败事件保留。

原测试先选择 host-clock +800ms 截止，再执行真实 Preview、Approve 和 Stage；未计这些步骤成本，随后 build CLI、固定睡 850ms。CLI 对库内候选扫描，原 fixture 还与并发 package 使用同库。因此测试既有不足以保证提交成功的期限假设，也有清理非本 fixture 候选的可能。

在原冻结 841 源诊断副本，保留原 800ms/850ms 与权限断言，仅增加 PG 时间观测（查询本身有成本；无人工 sleep 或故意锁延迟），3 次运行 1 PASS / 2 FAIL。两次 `before_commit` 尚未到期，但 `after_stage` 分别已越截止约 14.2ms / 37.4ms，返回 ErrDenied；第三次 Stage 仍在截止前约 32.8ms，清理通过。这证明固定短窗口可以自然在提交内到期，安全拒绝符合原规则；**未精确重现 root ErrUnavailable 的具体分支，其原因仍 UNKNOWN**。不得用新的长窗口绿色取代原失败或把诊断副本冒称原字节测试。

## 最小修复

1. 原 case 使用现有 `ownedMigrationDatabase(t)` 创建真正自有 fresh083 数据库，原 CLI 扫描与其他 package 隔离。
2. 先构建真实 CLI，再创建 fixture；用实际 PostgreSQL clock 选择一次明确 10s deadline。原 Preview→Approve→Stage 原样执行，不重获 grant、不续期、不改库期限。
3. 新增候选已提交且其有效截止不晚于原 selection / grant 的严格断言；仅记录脱敏阶段时间和截止。
4. 按已提交候选原有效截止轮询 PostgreSQL clock，15s host timeout 仅限制测试，不充当授权时钟；随后执行原两个并发真实进程总 expired=1、两次重启/重复 expired=0、EXPIRED 与正文消除的全部断言。
5. 原 before-commit 短期限拒绝、撤权、ABA、原子故障及其他 tests 不变。

## 验证

- `observed800ms-native1`：3 次诊断 case，1 PASS / 2 FAIL / 0 SKIP；test exit1，vet/build0；841 复制源稳定，完整 parent public/catalog 不变，自有 parent DROP。
- `fixed-cli-native2`：真实 fresh083 + 三原 seed + 三非空旧资料，1 PASS / 0 FAIL-SKIP；test/vet/build0，完整 public/catalog 不变、841 复制源稳定、owned DB DROP；CLI 子库 DROP 由原 helper 的 t.Cleanup 实际执行，失败会令 case FAIL。
- `fixed-related-native3`：116 PASS / 0 FAIL-SKIP / 0 package FAIL；test/vet/build0；841 复制源稳定，完整 parent public/catalog 不变、owned DB DROP。包括原短期 before-commit 拒绝、真实等待撤权、候选与许可迁移/故障原子矩阵。三个本轮 helper 生成的自有子库名字另用实际 pg_database 查询确认均不存在（`owned-child-cleanup.json`）。

复制帧是 root full38e 输入 source841，仅叠加唯一已获租测试；三原 seed 按字节补齐。当前 NOW004 并行源不作为该帧通过/失败原因，也不称 current live 全仓已通过。无生产 / 手机 / 外部服务动作；本轮 Flutter、真机、TalkBack NOT_RUN；Closed Pilot / Consumer Beta 均 NO。根代理负责整仓独立复验和唯一队列状态。
