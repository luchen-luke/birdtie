# Birdtie V5 AIR 差距核查索引

2026-10-02。此页索引既有逐项证据，不建立新backlog或重复产品真源。

56项AIR来源取得时分类：BLOCKED_EXTERNAL 6、NOT_IMPLEMENTED 19、PARTIAL 30、REAL 1。

每项source/状态/实际代码/现有任务/未覆盖差距详见 [137来源矩阵](BIRDTIE-V5-MATERIAL-RECONCILIATION-2026-10-02.md) 和 [机器矩阵](../../work/v5-material-reconciliation.json)；该矩阵是import前审计快照。实时scope/状态/证据只在 [队列](../../automation/codex_task_queue.json)，source completion在 [映射](../../automation/v5_requirement_mapping.json)，不能将旧DONE或CODE_LOCAL当成关联LIVE完成。

AIR003/004与AGE认知ADR职责已在INT001用实际代码和测试验收：[Phase0](../testing/evidence/v5-integration-2026-10-02/README.md)。AIR001/002/006/055复用旧核验/合同/发布门槛；055所复用门槛仍NO。模型网关/ContextBundle/Planner/候选/执行等后续尚未实现。009/012/031/034/054的live验证缺外部批准/凭据/供应商数据与实际canary，分开BLOCKED；不阻止独立离线代码，但不授权出网。消费报告仅作已有任务验收，ClosedPilot/ConsumerBeta均NO。
