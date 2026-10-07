# BT-V4-COMM-001：Community 现状与 V4 规范核对

日期：2026-10-01。以当前工作树的迁移、Go 服务、Flutter 客户端和回归测试为证据；以下 REAL 只指仓库内开发环境，不代表正式试点。规范来源：[Community 与 Activity 社交模型](../architecture/COMMUNITY-AND-ACTIVITY-SOCIAL-MODEL.md)和 [V4 产品总纲](../product/BIRDTIE-CANONICAL-PRODUCT-SPEC-V4.md)。

| 规范能力 | 当前代码与判断 | 仍需补齐的范围 |
| --- | --- | --- |
| Community 是持久社交实体，无账号、Agent 或地图点；旧 `organization_type='community'` 保持 Organization | REAL：既有 `communities` 表由 030 增量扩展；真人 owner 成员在同一创建事务写入。未见创建 Community 时创建账号、Agent 或 Organization 的路径。 | 无重复迁移需要。 |
| 所有权、成员和归档 | REAL（服务端）：030 的角色、状态、至少一位活跃 owner 约束；`postgres/community_social.go` 实现加入、申请审批、邀请、角色更改、移除、转让与归档，写审计。HTTP 由已验证 Person 会话进入。 | Flutter `CommunityApi`/`CommunityDetailPage` 当前只消费创建、发现、加入/退出、申请审批和归档；邀请、角色更改、移除、所有权转让及管理/空/错误状态的完整 UI 应在 **BT-V4-COMM-002** 增量实现。 |
| PUBLIC / PRIVATE / HIDDEN 与 OPEN / REQUEST / INVITE_ONLY 相互独立 | REAL（本地服务端）：030 约束、发现查询与成员检查；PRIVATE 非成员只见有限介绍。此次核对发现按已知 ID 读取 HIDDEN 仍可暴露名称和成员数，已改为非活跃成员且非有效受邀者返回 404；“我的社群”不再列出变为 HIDDEN 后的待审批者。 | BT-V4-COMM-002 需补齐用户端受邀入口和权限状态；正式数据及真机隐私验收仍缺。 |
| Activity 恰好一位主办方，独立可见性，RSVP 不自动入群 | REAL（本地服务端）：031 的外键和唯一主办方约束、032 的 PUBLIC/ORGANIZER_MEMBERS/INVITE_ONLY 可见性及邀请；041 增量加入 Business 外键，四者 XOR，不改旧 Organization ID。`social_activity_publish.go` 对 Person/Community/Organization/Business 分别校验；参与与成员是两张表。 | BT-V4-ACTY-001、BT-V4-ORG-002 仍需按 V4 四主体范围独立验收，不能以旧 BT-COM 任务 DONE 替代。Flutter 社群活动入口目前限制选中 Aberdeen，跨城市和无地理活动由后续 ACTY/NOW 任务处理。 |

本次保留旧 BT-COM 实现、数据库 ID 和接口，不建立第二套 Community。`BT-V4-COMM-002`、`BT-V4-ACTY-001` 和 `BT-V4-ORG-002` 已在 live 队列中，足以承接上述增量缺口；无需复制或新增同义任务。回归命令与结果记录在 [V4 完成记录](BIRDTIE-V4-COMPLETION-REPORT.md)及 live 队列证据中。
