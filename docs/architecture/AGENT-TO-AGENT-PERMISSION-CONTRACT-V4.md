# V4 Agent 间协作权限合同

2026-10-02，BT-V4-AGA-001。此文件定义合同与可执行纯权限策略，**当前没有通信端点、任务级 live grant 或真实送达**。运行框架不增加可调用协调工具。实际询问用户、持久化、重放保护与并发撤销属于 AGA-002；有效测试 fixture 不能作为这些能力完成的证据。

依据 [身份与所有权](AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md)、[情境访问](AGENT-CONTEXT-ACCESS-POLICY.md)、V4 canonical。Organization ActorRef 的 ID 不能替代 Agent 的 account PrincipalRef；用户选择公开 Profile、051 本人关系读取、048 共同展示或 052 新朋友匹配均不授予跨 Agent 同意。临时用途信息不能进入其他主体的持久记忆，V5 AGE-061/062 的公开/明确/task-scoped 限制继续有效。

## v1 请求与最小响应

`CoordinationRequest`：`version=agent-coordination-v1`、UUID `requestId` 与本人 completed `taskId`、`sender`/`recipient`（UUID agentId + typed account principal）、`purpose=ASK_ACTIVITY_INTEREST`、`scope=CONNECTION`、`resource={type:ACTIVITY,id:UUID}`、`fields=[NEXT_STEP]`、`expiresAt`。请求必须在当前服务器时钟后且至多 15 分钟；双方 Agent/Person 不同。枚举严格大小写、UUID 规范化、体积/字段数有限；未知字段、嵌套扩展、重复 JSON 键、多段 JSON、缺失/非法引用和私密数据字段均拒绝。

这一 purpose 仅表达“可以向用户提出这个活动的问题”。它不能解释为兴趣已存在、用户已收到问题、可参加或有空；不读取日程或推断意愿。当前 v1 不开放 PUBLIC/PRIVATE/WORKSPACE_PRIVATE/CLOSE，不支持 Organization/Business/Community/City 的协作请求。公共信息继续经现有普通读取接口；其他用途需要独立合同与授权，不能改枚举绕过。

`CoordinationResponse` 仅 `version`、同一 `requestId` 与 `nextStep=ASK_USER`，不得返回自由文本、兴趣判断、画像、网络、原始 Intent/记忆/聊天、时间表、联系方式或坐标。构建响应重新执行权限策略，不接受已缓存 Allowed 结果；拒绝只返回通用错误，不携带任何候选/资料或详细拒绝原因。响应消费同样严格解码并匹配原始 requestId。

## 可信服务端事实与独立同意

Wire 声明只能标识所请求的对象。`CoordinationFacts` 是独立服务端输入，不允许由 JSON 反序列化，禁止作为工具参数或可持久 wire blob；没有可信 resolver 时用零值，必须拒绝。之后 transport 必须在每次发送/响应以及事务内执行动作前从真实记录重新取得事实，不能把 fixture 布尔值移入路由。

准入逐项核验：

1. 当前活跃 Person 会话对应 sender principal 与 acting user；双方 active Person、active Personal Agent 与真实 owned Agent ID 完全匹配请求。接收 Agent 的所有权由服务端记录核验，不能靠发送方声明。
2. 源任务属于同一 sender、同一 Personal Agent，已 completed 且有明确找活动目的、请求资源来自该任务当前授权范围；未知/失败/匿名/其他用途任务拒绝。会话/任务/Agent 证据必须由服务器验证。
3. 资源是仍已发布、未结束/取消/过期的 Activity，双方按现有 Activity 可见性均可读。存在当前匹配 pair 的 accepted friend request、active Tie、active 双方账户，无双向 Block；聊天、Follow、同社群和共享活动不替代 Tie。
4. 双侧独立的任务级 `CoordinationGrant`：每条绑定独立 grant UUID、各自 owner principal、同一 requestId/taskId/senderAgentId/recipientAgentId/purpose/scope/Activity/NEXT_STEP；人类明确确认的时间与期限有效、未撤销，并且授权 revision 与服务端 current revision 一致且正数。任何用途、对象、接收者、字段、任务或 revision 错配拒绝。Profile 授权或关系强度不提供这种 grant。
5. 一侧撤销、授权/请求过期、任务/资源撤销或可见性变化、Tie 移除/Block、账户/Agent 停用，重新读取后立即拒绝；旧请求或同意不能用于另一任务。当前没有 live grant，所有真实新协调路径保持未开放。

纯策略输出可带内部稳定 reason code 供安全测试/审计；不得向接收方泄露哪条私人状态导致拒绝。受信证据不是数据源可见性的替代品。

## 动作和验收边界

通过合同和 ASK_USER 状态不触发联系申请、消息、报名、预订、群成员变化或资料持久化。未来真实动作必须绑定本次 purpose/target/action/期限，由人确认并在写入事务重新鉴权与幂等核验；协作许可不等于这些动作的许可。不能仅靠模型文本或 HTTP 200 执行动作。

本项验收：严格请求/响应合同、跨用途/主体/任务/资源/revision/期限重放拒绝、独立同意、真实关系类型、组织私人继承与未知私密字段红队、最小 JSON 和默认拒绝；全 Go 回归，现有 API 中新通信路径仍不存在。无需新增 schema/Flutter 或安装新包，本任务不声称交付消费者协作流程。真实 IdP/授权供给/HTTPS/运营及 A→H 未齐，Closed Pilot NO。


## 合同验收证据

430 条合同/红队 PASS（父子场景）；fresh052默认并发full Go三轮每轮766条PASS且无跳过，vet/build、旧ID/migration保护/down-reapply PASS。当前编译API实际readyz200，三个未开放协作入口404。曾发现的Community SQL错误和测试隔离失败均保留并修复复测，见 [完整证据](../testing/evidence/agent-permission-contract-2026-10-02/README.md)。这证明合同/纯策略，不证明真实同意、通信或送达；AGA002另行实现。
