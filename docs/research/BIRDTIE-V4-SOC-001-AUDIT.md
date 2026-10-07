# BT-V4-SOC-001 个人 Agent 关系上下文审计

2026-10-02，仓库/隔离开发与本地真机实现及验证完成。CHT-005 验收并更新队列后立即由 live `next` 领取；TIE-001 与 CHT-002 已 DONE。

实际已有：034 持久好友 Tie 与实时 Block/移除边界、好友对话、公开机会和 048 默认关闭的共同信息展示授权；Personal/Organization Agent 共用 `agentruntime.DecideContext`，PRIVATE 只允许本人活跃 Personal Agent，组织不能借成员权限读取私人资料。已有策略矩阵不等于关系检索工具；当前没有供 Agent 使用的关系强度、共同活动与互动分类聚合层。

本项必须提供实际可调用的策略约束读取，而非只增加策略 bool 或把两人同场推断为好友。计划先定义本人明确控制、可撤销的关系信号使用权限与受限聚合合同，默认不开放原始正文/私密记忆；数据库只选当前有效 Tie、活跃双方与无 Block。共同活动/社群资料继续满足数据源可见性和双方共享授权，不能绕过 TIE-003 的默认关闭设置。互动分类来自已授权参与范围的事实元数据，不将统计等级推断为 CLOSE 或亲密程度。必须核验 Personal Agent 实际所有权/活跃状态，不提供 Organization/Business/其他账户透传参数。

实施顺序：canonical 授权/聚合合同 → 增量偏好/撤销记录 → 真实 SQL 检索和 Agent 工具接入 → 中文本人设置/可审阅信号 → 策略及数据库检索红队测试 → 全量回归/构建 → 本地场景证据。证据完整后才 DONE；真实 IdP、供给、部署与 Closed Pilot 仍受原 blocker 约束。V5 AGE 保持 QUEUED，本项属于当前 V4。

最终合同、051、真实 Store/HTTP/Agent 路由和中文审阅页已落地。实际好友入口对话漏计在真机被发现并修复；不是只依据单测完成。完整 001–051/seed/Go/回退、Flutter analyze/135 tests/APK、Go vet/build 与最终真机开启→规则式 Agent 查询→审阅→关闭→旧任务重载→合成 Tie 清理证据见 [记录](../testing/evidence/agent-relationships-2026-10-02/README.md)。原始正文/私密记忆仍不被读取；生产/完整认知/跨 Agent 协作均未验收。
