# BT-V4-SAF-002 权限矩阵依赖审计

状态：等待 `BT-V4-FOL-001`。2026-10-02。

当前 `user_profiles.visibility` 数据库只允许 `private/public`。私人资料可由本人向具体真人授予可撤销的 `profile_view` 权限；公开资料、受众 Intent、Block 和活跃好友 Tie 有服务端过滤与隔离库回归。Social Intent 已有 `PRIVATE/FRIENDS/COMMUNITY/LOCAL/PUBLIC/INVITE_ONLY` 受众，`FRIENDS` 通过持久 `person_ties` 判定；既有 grant 不使私密 Intent 自动公开。

本任务的验收要求同时覆盖 `public/followers/connections/close/private`。其中 `followers` 没有对应关系表或 API，独立任务 `BT-V4-FOL-001` 仍 TODO 且计划为 P1/Beta；`close` 也没有明确的分级关系/本人管理入口。不能把现有 Friend 当 Follow、把明示 grant 当自动 Close，或仅扩展枚举就声称矩阵有效。故补充队列依赖 `BT-V4-FOL-001`，本任务在当前状态无法完整验收。Followers/Close 尚未上线时维持原有更严格的 private/public/显式授权路径，Social Alpha 和 Closed Pilot 仍 NO。

恢复：先完成 Follow 生命周期、Block/撤销语义及测试，再定义 Close 的本人显式管理与移除；为 Profile/Intent 实体分别增加受众选择、服务端统一可见性谓词、匿名/Follow/Connection/Close/Block/过期/撤销矩阵测试及 UI。`BT-V4-FOL-001` 的 P1/Beta 排期与本任务 P0/Social Alpha 门槛冲突，需要在后续队列规划中同步校准；目前保留原优先级和 Gate，不伪称已通过。
