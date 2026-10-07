# Birdtie V4 执行队列索引

日期：2026-10-01。来源：`BirdTie_V4_Execution_Package.zip!/BirdTie_V4_Execution_Backlog.md` 与同包 Excel `Requirements`；两者的 87 个任务 ID 完全一致。**实时状态、依赖、验收和证据以 `automation/codex_task_queue.json` 为准**。本文给出顺序与 Gate，不复制会立刻过期的 TODO 状态。既有 57 项 Functional MVP/Community 任务保留在同一队列，并有 [逐项对应矩阵](../research/BIRDTIE-V4-LEGACY-TASK-MATRIX.md)。

| 阶段 | 依赖主线 | Gate |
| --- | --- | --- |
| 0 基线 | `AUD-001` → `AUD-002` → `CAN-001` → `ADR-001` → `MIG-001`；`AUT-001` 和 `TST-001` 在各自依赖满足后领取 | Foundation |
| 1 主体/上下文 | `ACT-001`、`AGF-001/002`、`CTX-001`、`PRV-001` | Social Alpha 基础 |
| 2 持久社交 | `TIE-001/002`、`CHT-001/002/003`、`PRV-002` | Social Alpha 社交闭环 |
| 3 Intent/Opportunity | `INT-001..005`、`OPP-001/002` | Social Alpha 发现闭环 |
| 4 Place/Business | `PLC-001/003/004/005`、`VEN-001`、`BIZ-001`、`MOM-001` | Social Alpha 真实地点/经营权 |
| 5 供给主体 | `COMM-001`、`ACTY-001/002`、`ORG-002` | 主办方兼容与授权 |
| 6 Now | `NOW-001/002/004/005`、`MAP-001/002` | 地图与非地图投影 |
| 7 协同 | `ACTN-001`、`PLN-001` | 机会转成真实行动 |
| 8 安全/观测 | `SAF-001/002/003`、`OBS-001` | 隐私、滥用与审计 |
| 9 E2E/发布 | `E2E-001..004`、`REL-001`；外部 `PIL-001..003` 独立受真实条件约束 | Foundation → Social Alpha → Aberdeen Closed Pilot |

V4 计划含 57 P0、27 P1、3 P2。任务只有满足全部依赖才可领取；某些 P0 故意依赖 P1 前置能力，所以不能跳过依赖。`BT-V4-PIL-001..003` 是计划中的外部阻塞任务；既有 `BT-AUT-002`、`BT-TST-002`、`BT-REL-001` 仍保持原 Closed Pilot BLOCKED。新增 V4 工作不自动证明真实 CSSA 组织、活动、身份或部署条件。

每项执行遵循 [V4 自动执行协议](../../automation/CODEX_V4_EXECUTION_PROTOCOL.md)；审计结果见 [V4 差距分析](BIRDTIE-V4-GAP-ANALYSIS.md)，schema 顺序见 [V4 迁移计划](../migration/BIRDTIE-V4-MIGRATION-PLAN.md)。中文是默认主要界面语言。先保持旧 Organization/Activity/Community vertical slice、身份边界和地图生命周期，然后逐项扩展。
