# Codex Master Execution Prompt - Birdtie Functional MVP

Source: `Birdtie_Execution_and_CSSA_Partner_Pack.zip!/Birdtie_Execution_and_CSSA_Partner_Pack/automation/CODEX_MASTER_EXECUTION_PROMPT.md` (imported 2026-09-30). This is the execution workflow. `AGENTS.md` and the repository's domain-specific canonical documents govern product and architecture details. `automation/codex_task_queue.json` is the live status source; the imported workbook is a planning snapshot.

把这份文档全文作为 Codex 的长期执行规则。

---

你正在维护 Birdtie repository。

从现在开始，**不要把“页面能显示”“Flutter build PASS”“接口类型存在”当作功能完成**。当前产品已经有 UI/technical shell，但目标是完成真实可用的 Aberdeen closed-pilot MVP。

## 0. Before any implementation

先阅读并遵守 repository 中现有 canonical docs，至少包括：

- Agent identity / ownership canonical model
- Now Agent Map Workspace spec
- Global UX / Interaction Contract
- Map Runtime Performance Contract
- 本执行包中的 Product/Architecture docs

如果本包与仓库已有 canonical docs 重复：
- 合并到现有 authoritative file
- 更新引用
- 不制造两个互相矛盾的 source of truth

## 1. Task source of truth

任务队列：

`automation/codex_task_queue.json`

不要一次执行整个 backlog。

每轮：

```bash
python automation/taskctl.py next
```

只领取一个满足依赖的任务。

开始前：

```bash
python automation/taskctl.py start <TASK_ID>
```

## 2. For every task

### Step A - Audit

先确认当前实现属于：
- REAL
- PARTIAL
- MOCK
- HARDCODED
- NOT_IMPLEMENTED

不要猜。检查实际 Flutter/Go/DB/CI 代码路径。

### Step B - Implementation plan

在修改前写 3-8 条简短计划，明确：
- files/modules involved
- state/data ownership
- migrations/API changes
- tests

如果发现 canonical docs 与实现冲突，先处理文档/架构一致性。

### Step C - Implement minimum complete behavior

优先完成真实 vertical slice，不做无关重构。

禁止：
- 用 hardcoded data 伪装 API 成功
- 用 mock RSVP 伪装数据库持久化
- 把 UI placeholder 描述成 implemented
- 为了通过测试删除测试
- 忽略 analyzer/vet error

Development seed data 可以存在，但必须进入真实 DB/API flow，不能直接写死在 production Widget。

### Step D - States

所有用户可见功能至少检查：
- loading
- success
- empty
- error
- permission/authorization（适用）

### Step E - Tests

根据任务 acceptance criteria 添加或更新适当测试。

### Step F - Verification

优先使用 repository 已有 Makefile/CI/scripts；否则执行适用：

```bash
flutter pub get
flutter analyze
flutter test
flutter build apk --debug
go test ./...
go vet ./...
```

对于关键用户链路，必须执行 deterministic integration/manual real-device scenario，并记录结果。

### Step G - Mark status

只有 acceptance criteria 全部通过：

```bash
python automation/taskctl.py done <TASK_ID> --evidence "<commands + scenario evidence>"
```

如果缺 credential、外部依赖、产品决策或安全确认：

```bash
python automation/taskctl.py block <TASK_ID> --reason "<truthful reason>"
```

不要绕过 blocker。

## 3. Functional completion contract

`DONE` 代表：
- UI exists
- real backend/data behavior exists where required
- persistence exists where required
- authorization is enforced
- states handled
- tests/verification passed
- no production hardcode/mock

否则只能 PARTIAL/BLOCKED。

## 4. Highest-priority journey

在这个 journey 全部 PASS 前，不新增大型 feature：

```text
Organization Admin
 -> Create Activity
 -> Publish
 -> Real DB
 -> Public discovery/index

Student
 -> Open Now
 -> Local Pulse / Personal Agent
 -> Real ResultSet
 -> Activity Detail
 -> Join/RSVP
 -> Real DB
 -> Plans
 -> App restart retains state
```

## 5. Runtime rules

继续遵守：
- persistent map instance
- keyboard/focus 不 recreate map
- marker incremental diff
- camera move 不 destructive refresh
- explicit Search this area
- selection independent from viewport
- stale response protection
- one failure -> one primary error owner
- visible controls must work or be hidden/clearly disabled

## 6. Report after each task

输出简洁但完整：

### Task
ID + title

### Audit result
REAL / PARTIAL / MOCK / HARDCODED / NOT_IMPLEMENTED

### Changes
files/modules + behavior

### Verification
commands and PASS/FAIL

### Acceptance criteria
逐条 PASS/FAIL

### Remaining risk
只有真实存在的风险

然后领取下一个任务，不要自行跳过依赖。

## 2026-10-06 用户对开发验证频率的更新

本轮开发阶段按用户最新指示，默认只运行当前需求直接相关的单元测试（含必要的 Flutter widget 单元）。上文 Step F 的全面检查清单不再逐个小项重复执行，全面回归、分析、构建与数据库整检留到必要的整合或发布检查点。保留全部旧测试和历史证据，精确记录实际命令、目录、退出码与未运行范围；未执行的集成、迁移、真机和真实服务验收明确 NOT_RUN，不能以单元通过替代。

手机会长时间离线，先持续完成可独立执行的仓库任务，不等待设备或用户回复；现有权限、依赖、DONE 证据与发布门槛不降低。详情遵循 AGENTS.md 最新验证规则与 V5 增量执行协议。
