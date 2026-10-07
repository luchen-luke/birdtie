# Codex Task Automation

Source: `Birdtie_Execution_and_CSSA_Partner_Pack.zip!/Birdtie_Execution_and_CSSA_Partner_Pack/automation/` (imported 2026-09-30). The JSON queue is the live task-status source; `requirements/Birdtie_TAPD_Backlog.xlsx` is the imported planning snapshot and does not update when `taskctl.py` changes a status. Reviewers must use the queue for current status.

On this Windows host, `python` resolves to the Microsoft Store launcher. Use the configured Python interpreter, for example `C:\Users\chens\.cache\codex-runtimes\codex-primary-runtime\dependencies\python\python.exe automation/taskctl.py next`.

这套自动化不是“让 Codex 一次性修改整个仓库”，而是把开发变成一个带依赖、验收和证据的任务队列。

## Files

- `codex_task_queue.json` - machine-readable queue
- `taskctl.py` - stdlib-only queue controller
- `CODEX_MASTER_EXECUTION_PROMPT.md` - 给 Codex 的长期执行协议
- `CODEX_V4_EXECUTION_PROTOCOL.md` - V4 阶段的执行协议；旧协议保留为 Functional MVP 历史
- `import_v4_backlog.py` - 从用户 V4 ZIP 的 Excel 规划表幂等导入任务，不覆盖现有状态
- `verify_taskctl.py` - 用独立临时队列验证控制器，不触及 live queue

V4 规划顺序和 Gate 见 `docs/product/BIRDTIE-V4-EXECUTION-BACKLOG.md`。87 项 V4 任务已经导入；既有 57 项和快照 `automation/snapshots/pre_v4_2026-10-01.json` 保留。ZIP/Excel 是初始规划来源，不能代替 JSON 中的实时证据。

## Workflow

```bash
python automation/taskctl.py summary
python automation/taskctl.py validate
python automation/taskctl.py next
python automation/taskctl.py start BT-XXX-001
# Codex implements + verifies
python automation/taskctl.py done BT-XXX-001 --evidence "flutter test ...; go test ...; manual scenario ..."
```

阻塞：

```bash
python automation/taskctl.py block BT-XXX-001 --reason "Missing Mapbox token in dev env"
```

阻碍解除后重新领取：

```bash
python automation/taskctl.py start BT-XXX-001
```

## Rules

- 依赖未完成的任务不能 `start`。
- `done` 必须包含 evidence。
- `partial` 必须包含 reason；PARTIAL 不满足依赖。
- Codex 不得自己把 BLOCKED 解释成完成。
- 每次只做一个 task；task 完成后重新领取 next。
- `next` 先按 priority，再按 queue order。
- 若有 `IN_PROGRESS` 任务，`next` 显示该任务且 `start` 拒绝另一项，保证同一时间只领取一项。
- `requirements/Birdtie_TAPD_Backlog.xlsx` 是导入时的计划快照；完成状态以 JSON 队列为准。
- P0 全部完成并通过相关 release gate 前，不开始纯 P2 polish。
