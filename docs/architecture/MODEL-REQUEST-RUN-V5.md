# ModelRequestRun：原预算同事务执行元数据

AIR-011 原需求的内部增量。CODE_LOCAL；模型网络、真实供应商、生产部署、运营验收均未启用。它不是 083 Candidate Run，也不是新的批准或费用台账。

## 原始身份与账目

`Request.RunID` 保持原 058 binding ID。新 ModelRunID 仅标识一次有界运行；原 062 preview、reservation、四层预算和 audit 是唯一批准及账目来源。规划备用步骤不 Reserve，不扣 request，不提交 query。仅 PERSON 的 SELF_TASK_QUERY / 原 ActivityQuery 可用；其它目的不会因 Run 获得模型权限。

新 088 两表只保存原 ID、preview/price/digest、source generation、owner/Session/Agent 及闭集状态/期限。无 query、结果、token、provider body、gate ticket。Control 可读元数据不是 claim；本进程 handle、原 gate ticket、规范失败及结果 buffer 不可由 JSON 恢复。

## 事务与线性化

锁序是原 Session/account SHARE → owner advisory → Run → 按 ordinal 的 Step → 原预算/source helper。规划多个全局 OperationID 按 UUID 排序取 advisory lock；已存在任何 owner 的 reservation 或关联 Step 均拒绝收编。原未关联 062 入口保留行为；已关联的 legacy Reserve/Begin/Dispatch/Release 拒绝无 handle 绕过。

Reserve+Step RESERVED、Begin+Step IN_FLIGHT、Settle/Cancel+原 Step 状态使用同一事务。所有 helper 不自行 Commit；外层唯一 Commit 成功后才签出 Request。预算、FK、audit、metadata 等等待后重新检查原全部批准路线、Task xmin、owner/Session/Agent、fence、原 ticket 及 PG clock。最后持锁原生 SQL 是许可线性化点；不保证 Commit 解锁后任意瞬间撤权能够撤回已交给调用方的数据。

运行最初在 getter/native 等待前捕获 ticket 和 MaxElapsed；每次 checkpoint 和实际 delegate context 只能收紧。Session 正常 idle 更新不能延长已经观察到的整轮短界。每个实际 retry/fallback 有自己的原 OperationID，必须经过原 Reserve→Begin；私有明确 RATE_LIMIT/TEMPORARY 失败和原 Settle 确认才可继续。端口同型错误、未知提交、迟到、取消、声明变化及账目 UNKNOWN 不能生成这个失败凭据。

## 终态与恢复

只有本次规范结果 buffer + 原精确 usage/accounting + 原全部当前许可经编码后复核，才能 FINISHED 并释放答案。单独 SETTLED/UNKNOWN 元数据不能推导成功。失败停止仅退役 Run fence，原未知/在途预算不猜退款。

重启没有旧 handle 或原 ticket。原 ID 可读取 control/accounting；不会重新 Capture 或自动续发。RESERVED 可由本人明确取消，IN_FLIGHT/UNKNOWN/SETTLED 不自动重发或恢复旧答案。终态 Run 仍允许原合法账目对账，且对账不授予答案释放。

088 down 先拿两新关系 ACCESS EXCLUSIVE 锁，再检查历史；有任何 Run/Step 历史拒绝删除。实际锁等待可能因取消、超时或数据库死锁而回滚，不将这些失败说成成功迁移；本轮只使用自有本地验证库。

## 已执行与待核证

- `work/v5-air011-local-attempt/native25-run`：新核心及旧未关联回归，297 PASS，零 FAIL/SKIP；907 API + 3 seeds 同帧，五命令 0、原 public/catalog/xmin 与 088 未用往返保持。
- `native26-run-os`：305 PASS，零 FAIL/SKIP；908 API + 3 seeds，同样五命令 0及完整保留。实际 Create 末 Step 未提交与 down 锁等待、四个 OS kill 点及新进程仅原 ID 读取已执行。
- `native27-run-waits`：新增当前 source/Session/Run 期限、跨 owner OperationID 与 after-audit 回滚检查；本段记录时仍运行，不提前表述通过。

首轮 undefined helper 编译诊断、OS test 缺 import 编译诊断、原 schema87 全回归/Now fixture RED、所有旧 native/archives 完整保留。SQL NULL 旧表达式反例由根 TEMP 事务证实；新完整 088 INSERT/UPDATE NULL 拒绝已在 native25/26 实测。并发 down 窗口来自源码审查，修后实际等待用例不反向伪称原完整 down 已执行 RED。

最终本阶段仍待根独立定向及完整 Go 核证。真实供应商地域/保留/tokenizer/费用、真人、模型网络、生产与试点均 NOT_RUN；Closed Pilot / Consumer Beta = NO。未验部分不能用合成 adapter、Debug APK 或 Control DTO 替代。


## 2026-10-05 ModelRequestRun 最终目标帧补记

原 AIR011 继续，不新增批准、费用账本或机器用途。原 Request.RunID 仍是 058 binding ID，实际 ModelRunID 分离；088 两表仅保存原 ID/版本/主体和闭集执行状态。Reserve/Begin 与 Step 状态同事务；原 Settle/Cancel 的账目和关联 Step 同事务；外层唯一 Commit 后才返回 Request。原 066 ticket 不重新 Capture，旧短期限只收紧。

最新 `work/v5-air011-local-attempt/native29-run-waits`：schema88，316 PASS/0 FAIL-SKIP/pkg，test/vet/build/两个CLI 退出码全0；908 API+3 seed 的911份执行输入与 live 逐字节相同。原 public/catalog/xmin、088 unused down/reapply 保持，自有 `birdtie_air011_dee21b320267` 已实际 DROP。`source-freeze7.json` SHA256 = `32ed6ea3cafbb77bf066d28ffdb28fed3dbce42a5a98593fb50bb63089bd3efa`。真实命令见该轮 `commands.json`、`cli-build-command.json` 和 process receipts。

新增实际核验包括：跨 owner 反向 OperationID 规划仅一组成功且无部分账目；pool 等待中的 Task ABA、B 撤回、Session/lease 到期、Run 取消和 context 取消拒绝；Reserve/Begin 原 audit INSERT 等待后 Session 到期整事务回滚，原 budget/reservation/audit/Run/Step 全行及 xmin 不变。native26 已通过四个真实 OS kill 点（reserved/begun/returned/settled）及新进程仅原 ID 控制读取；恢复不会重发或重新生成私答。并发 088 down 等 Create 提交后拒绝删除新历史。

历史准确分类：native25=297全绿；native26=305全绿；native27=308 PASS/8 FAIL事件，六pool叶子及父例误等待只有 acquire 完成才更新的计数，另跨owner fixture错误使用全局配置 revision0，均未执行相应业务负向断言；native28=146其他包 PASS/PG编译失败（新增fixture漏 modelcapability import），不是PG业务测试失败。修正后 native29=316全绿。原帧/raw全部保留。原 SQL NULL CHECK 表达式反例是根独立 TEMP事务证据；新完整088 NULL拒绝实测，down竞态原版本没有实际完整RED，不能反向声称。

明确成本未知只保守持原上界；它单独不是继续许可。唯有实际本次规范 RATE_LIMIT/TEMPORARY、声明/原请求仍一致及原结算确认后可签私有失败。dispatch/commit未知、端口同型错误或迟到均停止；Control/Recover/账目不能授予私答释放。完整覆盖和逐源差分见 `work/v5-air011-local-attempt/MODEL-RUN-STAGE3-COVERAGE.json`。

当前只是原 CODE_AND_LOCAL_SUPPORTED_SCOPE，最终 fresh088 全仓由根独立运行，未提前标 DONE。仅本人 PERSON scalar ActivityQuery / SELF_TASK_QUERY 可用，其它未获明确 native 目的直接拒绝；AIR009/AIR018 下游不能倒置为本需求前置。未单独执行 FINISHED metadata-trigger 晚变负例；普通 Release 等待、Reserve/Begin audit 等待已测，不宣称所有可能等待矩阵穷尽。末次持锁 native SQL 是线性化点，不保证 Commit 解锁后任意瞬时撤权能追回已交付内容。真实provider/tokenizer/地域/收费/生产、模型网络与新Run消费UI均未运行；既有中文062人审路径不变，原083 Run UNKNOWN 不变，Closed Pilot / Consumer Beta = NO。

## 2026-10-06 AIR028：同事务原生输出来源

不新增表或第二运行账本。原公开结果 producer 拆出同事务 helper，旧 Read/Revalidate 仍自行开启与提交原事务。ModelRun 在原 062/088 事务、原 owner/Session/Task/generation/ticket 下捕获仅用于输出核验的服务端私有集合；查询由实际 Task 的原字段生成，模型不能选择来源或扩大查询。

每次原 Run 收口在原批准路线、配置、Task、预算及 metadata 等待后，再读取当前领域结果。最后一份 source payload SQL 同时核对本 Run ID、owner、Session、fence、state、lease 与 PG clock；随后只记录已持锁会话与最短期限，原 Commit 后才释放。原 proof/xmin、授权、实体/动作描述和公开集合发生 ABA、隐藏、撤权、过期或更新均失败；新读取不能延长最初来源期限。Account/Session/Run 的旧检查和原声明一致性不削弱。线性化点之后任意瞬时撤权不被表述为能够撤回已交付结果。

原 `Run` 保留历史 scalar/拒答/截断合同，实体引用的实际原生核验对它同样生效。`RunValidated` 在同一原驱动中增加专用输出失败及最多一次格式重试；没有新 scheduler、运行时或账本。私有输出失败只由实际 adapter 响应、原 Settle 确认及当前边界产生，不能 JSON 序列化或从 Control 重建。当前回执、恢复与原四个 OS kill/restart 不自动续发；新开 Store 不能用旧 ModelRun ID 重发或恢复旧私答。

适用 UX-CHECK-06/07/08/09/10/11/16。新的原生 Activity 正例、错 ID/邀请来源/查询失配、ABA/撤权/锁等待、实际修复与未知结算材料在 AIR028 独占证据目录；合成 adapter 不构成正式 provider 或 UI 验收。无新 DDL，schema 仍 096；正式出口和原发布门槛不变。


## 2026-10-06 AIR036：复用原 ModelRun 的有界只读规划

原 `Run` / `RunValidated` 签名和无 planner 行为保留。`LocalPlannerRunner` 复用同一 `runAt`，在所有等待前捕获原066 ticket、单次30秒 deadline；native `CreateOwnLocalPlannerRun` 只接受本 Store/当前 access 的私有 prepared goal，与原058批准的实际 Task/Session/Agent/source token及 Task xmin 完全一致。仍只记录原088 Run/Step，不新增表、账本、批准或恢复发送能力。

P0 native绑定最多2调用、输出最多3提案；修复一次、规范 TEMPORARY 重试均走同一原四预算与原 ModelRun。provider未知、Settle实际提交但响应丢失、Run取消、重启同ID、来源/身份撤权或迟到停止，不自动生成新Run或重发。首次 plan 只从 AIR028 当前公开活动结果中的真实 IDs 构造；模型正文不成为执行说明。

原 `Release` 最后持锁 SQL/Commit 保留。FINISHED 后再通过相同 process-local handle 的 `ReadOwnReadonlyPlan` 复用原 current scope/Session/fence/ticket/期限检查；FINISHED、SETTLED和控制 DTO 均不能重建或授权提案。输出时间用原数据库 clock和相对 ClockBound/elapsed共同收紧，不拿其他机器绝对钟或新读取扩大寿命。

首次原生 RED及 native 正反例、真实 wire、冻结来源与命令见本轮独占证据。该接缝不修改 main/HTTP/provider/Flutter，不执行037工具，不代表生产身份或模型可用；原已完成 AIR028 证据与原096迁移完整保留。Closed Pilot / Consumer Beta 仍 NO。


## 2026-10-06 AIR037：FINISHED 原句柄的只读许可复查

原成功 `FINISHED` native handle 只保留同一进程中的 ToolPlan。每次 Check/Read 使用原 `begin`/`finalCheck`/`commit`：原 Session/owner/Agent/Task generation/source、配置与062预算路由、ModelRun fence/lease/期限、活动投影与 ACL 全部复查。065策略仅在该新工具路径安装进程内私有复查数据；旧路径没有该字段时使用原 SQL 与参数，旧断言/DDL001–096/seed完整保留。

原当前设置的 token 含全065行内容/native_revision/xmin，缺省行缺席也受同事务短共享表锁保护。原 payload 最终语句复查 token 与 native clock，锁等待有原规划 elapsed deadline；策略/身份/来源 ABA、撤权/迟到、OFF→ON、其他 controller/store 或过期禁止旧 Read。最大3次 Read、每个 Call一次，不因第二次获取工厂或 JSON 解码重置。该新路径不新增模型预算分配、dispatch 或业务效果。

恢复 Control、`FINISHED` 记录或已显示 Decision 不能重建进程内许可；UNKNOWN 不自动重发模型或执行工具。`CheckOwnSandboxTool` 只准备具体版本 `CONFIRM`，AIR040 的持久批准/dispatch/effect/UNKNOWN 保持未实现边界。本轮无新DDL、主HTTP或生产服务替换；独占证据与根全量验收各自记录。
