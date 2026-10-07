# AIR016 异步运行能力审计

2026-10-04，root；原253 live队列接续，CODE_LOCAL，Closed Pilot/Consumer Beta NO。

## 实际已有

- 原064 `agent_domain_outbox` 捕获私人Moment变更，原source/context/audit/outbox同Tx；不调用模型，也不隐含分析许可。outbox基础持久attempt/fence/lease及control inbox已有。
- 原079/080独立具体分析与有限候选保留许可，原Session/Task/私人Moment/字段/版本/期限当前复查。
- 原082实际063候选＋064effect/inbox/checkpoint同Tx、跨handler稳定logical operation key、当前具体原grant receipt恢复已通过根9718全Go。原候选不是永久Memory。
- 原AgentTask同步查询与Conversation持久化；它不具有完整AgentRun/RunStep状态机，不能更名代替。
- 原feature controller配置默认OFF，所有认知/候选路径使用同controller；开关不是同意/身份或source事实。

## 尚缺与此次实现顺序

1. 持久Run/Step及有界本地worker；QUEUED/RUNNING/WAITING_CONFIRMATION/RETRY_WAIT与SUCCEEDED/FAILED/CANCELLED/EXPIRED分别表达，等待不作成功。
2. 当前原源引用、不可续命deadline、有限attempt、单调fence和run-specific当前lease。具体旧批准不跨Session；不持久化bearer/token/digest/正文/坐标/隐藏推理。
3. Cancel/expiry终态不复活；lease过期和commit结果未知先核实原082effect/receipt，不盲重发。
4. 使用原候选事务的可信native Run检查点，使已取消或失去lease的worker无法提交新副作用，已有候选不复制到新账本；dispatch/effect引用原ID和key。
5. 原Moment人类保存先完成，异步Run不成为原保存必须等待的模型调用。既有原领域数据库/事务故障语义保持；不能把下游未授权/模型失败传播成原保存失败。
6. 实际进程exit/重启、并发worker、fence/Session/源撤回/自然expiry、未知回执、迁移fresh/current-data/unused down/used拒绝/旧完整public与catalog/xmin验收。只做本地开发运行，不部署scheduler或启动模型。

## 并行边界

MAP001拥有只读typed投影及MapWorkspace/MapCanvas；NOW002拥有原Intent constraints闭集扩展及具体编辑/取消端口。root独占新Run包、PG/HTTP、083与共享server/main注册，先登记两个worker真实端口。此审计没有新运行能力，也不等于已启用自动认知。
