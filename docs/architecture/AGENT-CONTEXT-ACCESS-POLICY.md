# V4 Agent 情境访问策略

状态：`BT-V4-PRV-001` 已实现的服务端策略边界，2026-10-01。依据 [Agent 身份规范](AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md)、[ADR 0017](../decisions/0017-v4-actor-agent-context-place-model.md)、`apps/api/internal/agentruntime/context_access.go`。本策略管理 Agent 对情境资料的读取；它不代替数据源自己的可见性、Block、Consent 或成员权限检查。

## 范围与准入

| 范围 | 可读取条件 | 当前状态 |
| --- | --- | --- |
| PUBLIC | 数据源确实已发布，双方无 Block；匿名公开城市查询可用。Organization Agent 仍先检查组织成员与 Agent 状态。 | 城市 Agent Workspace 和公开搜索沿用现有数据库可见性检查；路由增加策略准入。 |
| CONNECTION | 所有者明确向连接开放，服务端能核验持久连接，未 Block。只允许阅读者自己的 Personal Agent，不继承组织管理员的私人关系。 | 034 已建立持久 Person Tie，但尚无资源级授权和 Agent 关系读取入口；当前继续默认拒绝。接受过一次聊天请求不等于持久连接。 |
| CLOSE | 除连接条件外，需服务端核验 Close 关系及**该资源的明确授权**。 | Close Tie 与资源级授权尚未实现，默认拒绝。普通 Profile 查看授权不能升级为私密记忆授权。 |
| PRIVATE | 仅资料所有者的活跃 Personal Agent，在本人会话中读取。 | 051 增加本人默认关闭的关系信号工具，独立明确授权，只读事实元数据与双方允许的公开活动报名，见 [关系合同](PERSONAL-AGENT-RELATIONSHIP-CONTEXT-V4.md)。没有原始私密记忆读取工具；Organization、Business 和其他人的 Agent 一律拒绝。 |
| WORKSPACE_PRIVATE | 本人 Personal Agent 的任务历史，或经过活跃成员资格解析的同一 Organization Agent 任务历史。 | 当前 Agent Task 创建后的继续、读取和列表使用此策略；组织成员不能借此读取个人历史。 |

策略输入中的主体、Agent 所有权、会话、成员资格、Block、关系和资源授权必须由服务端从实时记录取得，不接受请求 JSON 或生成文本声明为依据。`DecideContext` 是最终准入条件之一；公开活动、Person、Community 的 SQL 仍独立验证已发布、受众、有效期与 Block。`person_contexts` 默认私密且当前无公共读取接口，Context 节点本身也不会证明居住、机构资格或 Community 成员身份。

## 当前实现与后续边界

`agentruntime` 先用 Personal/Organization 的角色能力包限制可用工具，再以情境访问策略限制作用范围。Agent Task 路由在解析会话、组织工作台和活跃 Agent 后，对公开城市 Context 与私有工作台历史执行准入。Business Agent 仍不可调用，Community 没有 Agent。后续 Close/Business/共享记忆任务必须从数据库提供关系和资源级授权证据，并做跨主体撤销测试，不能只把策略布尔值设为 true。

`context_access_test.go` 的红队场景覆盖草稿/Block、伪造 Personal principal、无成员授权、组织管理员读取个人私密记忆、聊天不等于 Tie、Close 无资源授权、Business 未启用以及组织/个人任务历史隔离。Go 全量测试和现有 PostgreSQL Community/Activity 可见性测试是本阶段回归；没有真实试点数据或生产身份参与。

跨 Agent 请求另遵守 [任务与用途权限合同](AGENT-TO-AGENT-PERMISSION-CONTRACT-V4.md)。本文件的 Scope 或本人读取授权不能直接升级为其他 Agent 的共享许可；任务级双侧同意、发送/接收主体、资源、期限和 revision 必须独立核验。该合同尚未开放通信入口，PRIVATE/CLOSE/组织管理员继承等跨 Agent 路径继续拒绝。
