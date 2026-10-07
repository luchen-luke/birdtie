# BT-V4-AGA-001 Agent 间权限合同审计

2026-10-02，合同与纯策略已验收；状态以 live 队列为准。OPP-004 实际完成后立即 next/start，保持一次一项。

## 实际代码

`agentruntime/policy.go` 只有 Personal/Organization 角色与工具 pack，未开放协调能力；Business 不可调用，Community/City 没有 Agent。`context_access.go` 是现有上下文读取策略，不包含 request/task/purpose/recipient/有效期绑定，不能直接升级为通信授权。Agent principal 使用活跃 owned Agent 与会话工作台解析；Organization actor ID 与 account principal 不同。

034 的 accepted friend 与匹配双方 active Tie 可以作为连接证据，Follow/聊天/共同活动不能替代。048 展示、051 本人关系事实读取、052 新朋友匹配及 profile_view grant 各有独立用途，均不是跨 Agent 的任务级同意。当前没有跨 Agent transport、任务级 live consent store、送达/重放状态或询问用户的消费界面。

## 本项最小计划

仓库未发现既有 Agent-to-Agent canonical；新增独立合同并在身份与上下文访问文件中链接。实现严格 wire DTO/解码、默认拒绝的纯策略以及只返回 requestId/ASK_USER 状态的最小响应投影；有明确用途、关系范围、同一资源/任务/发送与接收 Agent、有限期限、双侧独立同意及当前 revision 的可信服务端事实才可通过。不得从 JSON、生成文本或旧开关造授权。

v1 只定义 Personal↔Personal、CONNECTION、活动询问用户这一有限目的；组织/Business/Community/City、私人记忆/日程/正文/精确位置和 Close 默认拒绝，不新增工具或 HTTP 入口。合同有效 fixture 只证明策略模型，不证明通信已工作。随后做合同/红队/全 Go 回归与 no-route 实测，记录证据后才完成 Define Contract；AGA-002 的实际协作、持久化/重放/并发与中文 UI 另做。V5 AGE 不抢占，Closed Pilot NO。


## 实际实现与验收

严格 wire DTO/解码、独立可信事实、默认拒绝、双侧用途/任务/资源/主体/期限/revision 同意、匹配 accepted friend/Tie/Block、最少 ASK_USER 投影已实现。合同/红队 430 条 PASS 记录（含父子测试）；fresh 001–052 与三个 seed 默认并发 full Go 连续三轮每轮 766 条 PASS，0 FAIL/0 被跳过测试，vet/build PASS。当前编译 API readyz200、三个探测入口404，未开放真实通信。fresh migration 验证旧 ID 和039–052保护/down-reapply PASS。

完整回归发现真实 Community UPDATE unused $2/42P18，先保存同一fixture修前FAIL，再修连续参数并保留权限/audit；修后范围与完整回归PASS。并发借用他包临时Person/Place/Activity及全库公开len假设修为自己的fixture和精确ID，清理错误失败；migration verifier恢复默认并发。未替换开发样例为真实资料，未新增schema/Flutter/transport/live grants；AGA002仍需实际持久化、并发、重放及中文消费流程。见 [完整命令、首次失败和复测](../testing/evidence/agent-permission-contract-2026-10-02/README.md)。Closed Pilot NO。
