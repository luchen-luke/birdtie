# AGE038 Task 当前边界回归（2026-10-04）

## 任务与范围

原 BT-V5-AGE-038 经根代理显式重开并交接，本 worker 范围仅原 `apps/api/internal/httpapi/agent_task_current_integration_test.go` 与本审计、专属 work/evidence；测试当前 SHA `57b2144cdd99350e78af223fdc9f7df7ba15ade49fc05446de22991457711c5e` 保持冻结，不能放宽 404/401/no-store/no-data。生产七类结果最终 SQL 的修正属于 sponsored_trust NOW004 scope，worker 不修改。唯一队列/总报告归根代理。

适用 AGENTS/V5 与 UX-CHECK-05/06/07/10/12/16 的当前身份、具体版本、撤权、迟到和证据约束。本轮无界面修改，无模型或正式服务动作。

## 原失败38i及精确屏障修正

`full-air015-overlay38i` 为 10031 Test PASS、2 FAIL 事件（一个叶子与父用例），退役等待 case 返回 403 agent_unavailable，原期望 404。403 来自初始 HasActiveAgent，先于原 GetTask；旧 helper 只匹配数据库名与 locker PID，可匹配其他包任一等待，无该 HTTP backend / SQL 身份。日志没有原误匹配 query，因此原具体阻塞对象 UNKNOWN。

原 test 精确修正由根代理完成：自有 fresh083 DB 隔离真实全表 ACCESS EXCLUSIVE；HTTP 独占 postgres.Store/pgxpool application_name；原实际 GetTask SQL、locker PID、agent_tasks 未授 AccessShareLock 同时匹配；撤权前真实创建无关 native SQL 等待，旧 helper 在 HTTP 尚未启动时即可通过。该对照不是假 Store 或许可。HTTP 真正经历 Authenticate→HasActiveAgent→GetTask→响应编码→最终验证，原所有结果断言保留。

Fatal 清理也补为请求 WithContext、统一取消与有界 rollback，drain 尚未消费且已启动的两个 goroutine 后释放连接；正常 consumed 标记避免重复 drain，之后才 pool/fixture cleanup。不得把 API 403 改为期望以绕过该阶段验证。

## 历史 GREEN（非当前新接口通过）

- `task-boundary-native38j`：6 PASS（5场景+父）、0 FAIL/SKIP，test/vet/build0；先前清理版本。
- `task-boundary-native38k`：清理修订后的 SHA57b214，6 PASS、0 FAIL/SKIP，test/vet/build0。
- `full-task-boundary38k`：841 原冻结帧加唯一 AIR015 test 与本 test；10033 Test PASS、0 FAIL/SKIP，test/vet/build0；完整旧 public/catalog、迁移 up/down/reapply/原 participation xmin、ownedDB DROP 真。

这三组执行的是旧冻结生产帧；allObservedAPISourceStable=false 是执行期间移动 live 诊断，frozenExecutionSourceVerified=true 证明实际复制帧未改。不能据此声称随后 NOW004 native7 也通过。

## 当前新接口 RED

`full-now004-native38m/tests.log` 的原字节事件：真实精确 GetTask barrier 到达后退役 Agent，HTTP 返回 401 unauthorized 而非原契约 404；子用例 FAIL 仍保留。完整38m运行未结束时不填写其总通过数量。

七类接入路径的新 current SQL 需分别判定有效 Session（失效→401）与 Agent/Task（不可见→404），维持原读取、最后权限及时间检查。不得弱化匹配、放行旧 Agent 或把该新接口失败掩作旧屏障问题。生产修复和 native 新结果由对应 owner 交付，随后 worker 对冻结当前源码独立验证这五场景。

## 证据与后续

`docs/testing/evidence/task-boundary-regression-2026-10-04/history1/` 按原字节保留38i/j/k/fullk日志与复制源、38m本任务原始事件和冻结当前 test；不可覆盖。独立当前注册 HTTP 验证将使用新 label/source frame，不改旧档案。生产 IdP、真人场景、手机、Flutter、TalkBack、外部部署均 NOT_RUN；Closed Pilot / Consumer Beta NO。

## 当前修复后独立 GREEN（current-native1）

已在 sponsored_trust 明确 production freeze 后，从其真实 `work/v4-now004-native/native21-frame` 按847个原Go/SQL/mod SHA逐一核验复制，包含原注册路由、3原seed和未变测试57b214。独立 fresh083、三份原非空资料运行：`go test ./internal/httpapi -run ^TestNotificationDestinationHTTPCurrentTaskIntegration$ -count=1 -json`、同包 `go vet` / `go build`，全部 exit0；6 PASS（5场景+父）、0 FAIL/SKIP/package FAIL。实际请求日志顺序 200/404/401/404/401，并保存原 requestID。完整 parent public/catalog 不变、847复制源稳定、owned parent DROP；helper 实际自有子库 DROP 成功日志也保留。

生产 PostgreSQL source SHA `f39c7a0866fcfd2ffcafad2202314893d2ccb4de0448be1781401fd88cf25067`、HTTP projection SHA `2abcbade16705e59abc734c2fee35bf5d9bd4b9d527479249b426485c2302948`；新 SQL 分开 currentSession 与 Agent/Task authority，真正会话/账号失效401，当前 Agent/Task不存在404。本worker未修改这些生产文件。新 peer HMAC测试随后追加属于另一文件，当前验收只称此847复制帧，不称后续整live whole或所有新七类验收。根代理仍须合并最新双方冻结源独立全仓并记录唯一队列。

原38i假屏障失败、38m新接口401分类失败及先前旧帧绿色独立保存。状态码断言没有改动；根将进一步核完整原始档案字节。无新增DDL、许可、模型调用、生产部署或真实人验收。

## 最终双方冻结对齐（final-native2）

peer 正式 freeze2 后再次复制其 `native22-frame`，847 源各 SHA 核验；与 native21 唯一变化是其 PostgreSQL projection integration test，生产和本57b214测试均未改。独立新 fresh083 再跑原注册 HTTP 五场景，6 PASS / 0 FAIL-SKIP，test/vet/build0；实际状态仍 200/404/401/404/401、原 requestID 见 freeze2。完整 parent public/catalog 不变、847复制源 stable、owned parent DROP；子库 helper DROP 日志 PASS。当前源码状态判断依据此最终新帧，历史绿色与红色不替换。

`history1` 原1888文件/87,912,648 bytes，manifest SHA `47f773ba9cc99df725a47d73c09ceb8a552b7b5c66272d1041df4aeb97c6c5cc`；最终 worker-final1 独立保存两次 current 验证 raw/source、脚本、冻结/状态/审计。根全当前 Go 仍待单独核证，不自行 DONE。
