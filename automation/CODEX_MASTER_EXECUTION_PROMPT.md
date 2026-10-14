# Codex Master Execution Prompt - Birdtie Functional MVP

Source: `Birdtie_Execution_and_CSSA_Partner_Pack.zip!/Birdtie_Execution_and_CSSA_Partner_Pack/automation/CODEX_MASTER_EXECUTION_PROMPT.md` (imported 2026-09-30). This is the execution workflow. `AGENTS.md` and the repository's domain-specific canonical documents govern product and architecture details. `automation/codex_task_queue.json` is the live status source; the imported workbook is a planning snapshot.

把这份文档全文作为 Codex 的长期执行规则。

## 2026-10-15 当前优先执行动态通知和好友闭环

用户本次要求先让页面跑通动态发布、修改、查看、删除、通知和好友。当前执行者保存正在进行工作的自然安全检查点后，接续 [完整执行清单](../docs/product/BIRDTIE-SOCIAL-CORE-EXECUTION-SPEC-2026-10-15.md) 的 SOC-01 至 SOC-11；随后按真实接口依赖补动态浏览、互动、多媒体和串文。此顺序覆盖下面旧版“最高优先旅程”的本轮开发排序，原活动/组织需求与发布门槛继续有效。

清单挂接当前 `BT-FIX-NOW-UI-001.social_core_execution_20261015`，`taskctl.py next` 可读到；不新增替代队列、不重复 start、不重开历史 DONE 或自动解除 PARTIAL/BLOCKED。共享源和新迁移先审计并登记精确范围；只有执行根代理更新 live 队列与共用报告。

已有 144 私人草稿、publication、145 单张公开图片、撤回后端、通知已读和好友申请继续复用。主要差额是公开发布原操作事实、公开正文编辑、本人动态管理入口、好友管理死入口、通知已读后的权威剩余计数和候选审核结果跳转。公开草稿和公开正文的修改不能混算，私人选图也不能当作公开图片发布。

2026-10-15 实际编码接续：148 原发布/公开编辑操作、本人管理、通知剩余计数、好友管理菜单、Now 单图的两步公开批准与本人候选审核结果入口已接线；最新配对产物为 `api-build02`、`apk-build02`、`web-build02`，本机运行证据为 `runtime02` 和 `web-runtime02`。先读原总报告最新节及 `docs/testing/evidence/social-core-execution-20261015/checkpoint-phase2.json`，区分代码、直接验证与未运行真实界面。SOC-08/09/10 原关系/会话动作继续复用；真实双账号、当前图在详情/地图的真实展示及完整流程仍 NOT_RUN，不能据局部通过关闭第一批验收。下一独立编码差额是 SOC-12 城市动态、SOC-13 完整历史及随后依赖项，视频、多图、串文仍未实现；继续原父项和真实依赖，不重复建立任务。

本轮先编码和接口/客户端接线，关联改动只做必要直接验证并统一编译；不逐项重复全仓测试、全量分析、数据库整检、装包和截图。正常字号、不录屏、中英混合语音延期；代码、流程、视觉和交付分别记证。缺真实设备或外部条件时继续独立本地实现，未运行记录 NOT_RUN。没有新的付费、生产部署、真实用户消息或数据迁移授权。

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
