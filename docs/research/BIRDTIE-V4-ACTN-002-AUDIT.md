# BT-V4-ACTN-002 共同到场后连接建议：前置核查

2026-10-05。原任务、依赖、优先级和 Beta 门槛保留。TIE001 与 PLN001 本地实现已完成；本项尚未实现，不能用共同报名替代原验收的 shared attendance。

## 实际来源

- 原生 `activity_participations` / RSVP / Plans 保留 going、pending、cancelled。going 说明报名，不证明到场。
- [活动参与信号](../architecture/AGENT-PARTICIPATION-SIGNAL-V5.md) 明确 attendance、completed 无权威来源；单次参与也不产生稳定兴趣。
- [Enrichment 事件](../architecture/AGENT-ENRICHMENT-EVENTS-V5.md) 的 ActivityCompleted 当前 UNAVAILABLE，不调用 resolver，不从活动结束或 Plans 推断。
- [Place Memory](../architecture/AGENT-PLACE-MEMORY-V5.md) 不从收藏、Moment 地点关联或 RSVP 推断访问。078 明确公开报名也不是到场核验。
- 已有原生连接请求和双方接受 Tie 可复用；不得自动成为好友，不另建关系台账。

## 阻碍与恢复条件

缺少权威 attendance/completion writer 及其产品授权合同。需要明确：谁有权确认、本人/对方如何许可展示与建议、记录如何更正或撤销、来源版本与当前公开资格如何复核。主办方核验、双方自报和系统推断不是相同证据；如采用自报，界面和数据必须明确自报，不能标已核验到场。

根代理已向用户请求这项产品决策，尚未收到答案时不把默认选项当批准。不需要外部凭据才能做静态代码核查，但未确认的事实来源不能由开发者虚构。恢复后先增量补同一 canonical 及领域写入合同，再实施当前版本、授权和重复安全测试；不重复导入 backlog。

任务记为 **BLOCKED**。本项本轮仅审计，功能、E2E、真人到场核验、生产环境均 **NOT_RUN**。共同历史 ACTN003 继续独立推进，使用准确的“共同报名”和“活动关联地点”语义；它不解除本项。Closed Pilot / Consumer Beta = **NO**。
