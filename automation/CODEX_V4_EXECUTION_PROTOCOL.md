# Birdtie V4 自动执行协议

来源：用户提供的 `BirdTie_V4_Execution_Package.zip!/BirdTie_Codex_Master_Prompt_V4.md`，按 `AGENTS.md` 的仓库/文档目录规则和 live 队列状态调整。此前 `CODEX_MASTER_EXECUTION_PROMPT.md` 继续记录 Functional MVP 阶段方法；V4 任务使用本协议与 [V4 产品规范](../docs/product/BIRDTIE-CANONICAL-PRODUCT-SPEC-V4.md)。

1. 只在 `D:\Project\birdtie` 写入；`D:\Program\Civu` 仅只读参考，`D:\BirdTie` 仅保留/选取。先看 Git、`AGENTS.md`、canonical 文档和既有未提交改动；不重置、清理或覆盖用户工作。
2. `automation/codex_task_queue.json` 是唯一实时任务状态。先 `taskctl.py validate`、`summary`、`next`。默认串行：有 IN_PROGRESS 就完成或如实 PARTIAL/BLOCKED；否则领依赖已满足的最高优先级任务。用户最新授权的独立需求可按下述受控并行段推进，不能仅因开启多个agent就绕过依赖或写入范围。
3. 每项按 **审计 → 简短计划 → 最小完整实现 → 适用测试/真实场景 → 证据 → 状态**。旧实现应 RECONCILE，不能因计划标题重写或自动 DONE。开发 seed、Debug 包、mock/hardcoded、单独 build 都不是正式产品能力。
4. 新 schema 要求增量、外键/约束、旧 ID 与旧 API 兼容、隔离库 up/down/reapply、旧 vertical slice 回归。不得给在线 Intent 填伪城市，不得把 legacy Business Organization 批量改类，不得自动创建 Community Agent。
5. 所有后端权限在服务端落实；中文为主要界面语言；地图键盘、Pin、选择与显式区域搜索规范继续有效。每阶段检查加载、空、错误和权限状态。
6. `DONE` 要有可复现命令与结果、数据库/API 或真机场景证据；`PARTIAL`/`BLOCKED` 写明余项、原因及恢复条件。完成后更新队列与受影响文档，立即运行 `next` 并领取下一项，连续推进。2026-10-02 用户明确取消每两小时领取一轮的安排：以当前聊天的持续目标自动衔接，不因单项完成或汇报主动结束执行，也不为等待定时触发插入休眠。遇到外部阻碍先记录并继续其他独立任务；只有用户要求暂停、系统限制或没有可执行任务时才停止。持续目标遵守工具的状态/阻碍审计要求。
7. 用户当前只授权仓库内实现/验证；不正式部署、发布、联系 CSSA/商家或更改外部服务。真实 IdP、HTTPS、已授权活动、运营与正式环境未齐时 Closed Pilot Ready 保持 NO。
8. 没有实质变化时保持安静；在任务完成、失败或需要用户行动时通知。任务状态和当前 Gate 汇总写入 [V4 进度与验收记录](../docs/research/BIRDTIE-V4-COMPLETION-REPORT.md)。


## UIUX 与 V5 增量接入

2026-10-02 最新用户要求：每项用户可见开发引用 [唯一交互规则正文](../docs/ux/GLOBAL-UX-INTERACTION-CONTRACT.md) 的适用 UX-CHECK-01 至 16，复用现有组件/动作，先整理有效授权上下文，减少重复表单输入，维持中文、直接入口与版本确认。UIUX 是持续质量规则，不另造功能队列。

在当前 atomic task 的自然安全检查点核对 AGE、AIR、规则和消费级评估原文与实际代码/测试/队列；先接规则，再只将未覆盖需求按真实接口依赖增量追加，原 ID/状态/证据不重置或重复导入。AGE/AIR 无需机械全串行，分别拥有 Memory/Profile/Evidence/Policy 与受控提案/运行/执行职责；以复用和接口前置安排，不抢占当前任务或绕过发布 Gate。消费级评估只补核验/验收要求；缺材料明确记录，不推测原文。live provider、对外写和 A2A 仍需各自开关与外部门槛，当前授权仅仓库/隔离本地验证。

实际SAF004检查点已完成，V5按 [接续协议](CODEX_V5_EXECUTION_PROTOCOL.md) 和 [唯一来源映射](v5_requirement_mapping.json) 增量加入；当前状态不再由历史AGE QUEUED快照决定，旧V4对象及Gate保留。

## 独立需求的受控并行（2026-10-02增量授权）

根代理是live队列的唯一写入者；最多3个IN_PROGRESS，对应根代理与两个实施worker。先审计候选的真实依赖、接口、DDL与所有将修改文件，不能把依赖相连或共享真源的任务拆成互相竞争的写入。每项仍独立审计、实现、测试、核证及状态，中文和正式发布门槛不变。

```text
taskctl.py parallel-config --coordinator root --limit 3
taskctl.py lease <已有IN_PROGRESS> --coordinator root --owner root --write-scope <精确文件或独占包>
taskctl.py next --parallel
taskctl.py start <TODO_ID> --parallel --coordinator root --owner worker-a --write-scope <路径1> --write-scope <路径2>
taskctl.py done <ID> --coordinator root --evidence <可复现证据>
```

`next --parallel`只读输出按既有优先级/必要前置公平规则排列的候选，不推测scope，不领取。只允许全部依赖DONE且通过本地执行分类的TODO；LIVE_ONLY、任何非空external/activation gates、未知completion/gate、release_stage、BLOCKED/PARTIAL均不能借并行解除。已有active先显式登记lease；scope按规范化路径组件比较，拒绝相同或父子包含、重复owner、空/root/未知顶层、穿越、仓库外绝对路径与通配符。

lease和上限存于队列根metadata，不改旧task的priority/phase/owner等历史内容。启用后所有写命令由根代理携声明`--coordinator`执行；worker只交付代码/证据，不save live队列或要求queue写范围。coordinator只是协作约定，不是OS授权。持久文件锁与读取hash防止误并发丢失更新，冲突拒绝后必须重读。`done/block/partial`释放该lease并追加历史；共用报告、queue及发布核验由根代理维护，不削弱原Gate或删除旧归档。
