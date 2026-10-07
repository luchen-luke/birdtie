# Birdtie V4 状态核验

日期：2026-10-01。任务源：仓库内 `BirdTie_V4_Execution_Package.zip` 的 Markdown 计划和 Excel `Requirements` 工作表；用户消息所指的临时预览路径在本机已不可读。两份计划均列出 87 个相同且唯一的 `BT-V4-*` ID，依赖均指向计划内任务。包中“39 DONE / 3 TODO / 3 BLOCKED”是旧快照，不能代替 live queue。

## AUD-001：安全快照

- 官方仓库：`D:\Project\birdtie`；当前分支 `master`；HEAD `d6e86d3`（2026-09-30，`Implement deterministic Agent MVP loop`）。未创建提交、未 reset、未 stash、未覆盖既有改动。
- 导入 V4 前，`automation/codex_task_queue.json` 为 **57 项：51 DONE / 3 TODO / 3 BLOCKED / 0 IN_PROGRESS**。`BT-COM-001` 至 `BT-COM-012` 均 DONE；已有三项 P0 blocker 为 `BT-AUT-002`、`BT-TST-002`、`BT-REL-001`。Community 十项真机证据见 [验收记录](../testing/COMMUNITY-SOCIAL-PHYSICAL-DEVICE-2026-10-01.md)。
- 导入 87 个 V4 计划任务后，live queue 为 144 项，既有 57 项的状态和证据原样保留。Excel 的三项 `BLOCKED_EXTERNAL` 映射成现有队列枚举 `BLOCKED`，记录其外部条件；其余新任务为 TODO。导入器 `automation/import_v4_backlog.py` 可重复执行，第二次不得改变既有状态。
- 开始 V4 审计时 Git 有 **75 个已跟踪改动、158 个未跟踪入口**（含用户提供的执行包、先前完成的 Go/Flutter/文档/迁移工作）。清单保存于本地 `work/v4-2026-10-01-git-status.txt`，跟踪文件改动摘要在 `work/v4-2026-10-01-tracked-diff-stat.txt`。这不是可安全整体提交的已审查变更集；在代码/数据库重构前须按文件核对并使用可回滚的独立迁移，不可把现有工作树当作干净基线。
- 现有 schema 从 `001_foundation.sql` 到 `032_activity_social_visibility.sql`。030/031/032 已有 forward/down/reapply 与合成 seed 验收；旧 Organization→活动→发现→Agent→RSVP→Plans 回归已 PASS。现有真机是本地 Debug 包、开发会话和本地数据库，不是正式部署。
- 已读 `AGENTS.md`、`automation/CODEX_MASTER_EXECUTION_PROMPT.md`、Agent 身份规范、Community/Activity 模型、Now 规范和 Global UX 交互规范。中文主界面要求继续适用；CityContext 不是 City Agent；Community 本版没有 Agent。

## 旧模型与 V4 的初步对照（AUD-002 持续细化）

| V4 范围 | 当前仓库证据 | 初步判断 |
| --- | --- | --- |
| Person / Organization / Community / CityContext | `019_agent_identity_organizations.sql`、`030_community_social_memberships.sql`、Agent 身份规范 | REAL（原有范围）；V4 Business 仍需独立建模，不能把旧 Organization 数据静默改类。 |
| Activity organizer / visibility | `031_activity_organizers.sql`、`032_activity_social_visibility.sql`、Go/Flutter 对应实现与真机 | REAL（Person/Community/Organization）；V4 Business organizer 尚无。 |
| 持久社交 Tie | `017_connections_and_messages.sql` 的 request/accepted conversation、`internal/connection` | PARTIAL；当前 request 的 scope 固定为 `conversation`，无完整 friend/remove/block/follow 生命周期。 |
| Chat | `017_connections_and_messages.sql`、`internal/postgres/connections.go`、Flutter `connections.dart` | PARTIAL；已有同意后 1:1 文本聊天，实体分享与独立关系语义尚无。 |
| Intent | `006_city_graph_content.sql` 的 `intents`、`internal/intent/model.go`、AgentTask | PARTIAL；当前以城市和时间地点为核心，无完整 online/hybrid、跨城市及 V4 生命周期。 |
| Place / Venue / Business | `001_foundation.sql` 的 places 和现有地图/地点 API | PARTIAL；Place 具稳定 ID，Venue 能力和 Business 主体尚未见实现。 |
| Opportunity Engine | `agentworkspace` 规则路由与真实活动查询 | PARTIAL；当前是可解释的规则式结果，不等于 Tie/Intent/Place 联合机会引擎。 |
| Now / 地图 | canonical Now 规范、Flutter `map_workspace.dart`、`public_city_controller.dart` | REAL（既有地图/活动范围）；V4 非地图在线社交机会和多类型图层尚未完成。 |
| 正式试点 | `BT-AUT-002`、`BT-TST-002`、`BT-REL-001` | BLOCKED；真实 IdP/HTTPS、CSSA 授权活动、正式环境与 A→H 缺证据。 |

V4 只作为新阶段工作跟踪；新任务的 TODO 不抹去既有功能。详细功能差距、依赖及与既有任务的对应关系由 `BT-V4-AUD-002` 完成。`docs/reports/FUNCTIONAL-MVP-COMPLETION-REPORT.md` 继续表示现有 Closed Pilot Gate；V4 的 Foundation / Social Alpha Gate 需单独验收。

## AUD-002：逐项状态和重复范围

[57 项旧任务矩阵](BIRDTIE-V4-LEGACY-TASK-MATRIX.md)逐项列出 live 状态、V4 交叉项及旧验收摘要；原始完整证据仍在队列记录中。[V4 代码差距审计](../product/BIRDTIE-V4-GAP-ANALYSIS.md)根据实际 SQL、Go、Flutter 将同名能力分为 REAL / PARTIAL / NOT_IMPLEMENTED，而非根据计划标题推断。旧 `BT-COM-001..012`、Organization/Activity/Now 路径保留；V4 `COMM`、`ACTY`、`NOW` 任务只完成增量语义及新验收。旧队列的 3 个 TODO 保持 TODO，3 个 BLOCKED 保持 BLOCKED；导入器没有删除、重置或改写这些记录。

V4 计划共 **87 项：57 P0、27 P1、3 P2**。计划内 3 个 `BLOCKED_EXTERNAL` 是 `BT-V4-PIL-001..003`，已映射成 live `BLOCKED` 并保留旧 Closed Pilot 三项 blocker；这两组门槛不是六个已解决问题。当前 V4 首个可执行任务由 `taskctl.py next` 按依赖选出。V4 的验收仍须代码、迁移、测试和真实场景证据，包内“完成”的表述不自动成为仓库状态。
