# AGE008 置信度能力审计

2026-10-03。当前源码与源AGE008为准，不依据旧报告标记完成。

- `agentmemory/model.go` 的Record.confidence是float64，EXPLICIT=1只表示本人声明；`migrations/056_agent_memory.sql` 限定0..1、EXPLICIT固定1，INFERRED只允许PENDING_REVIEW/EXPIRED/DELETED，当前不允许ACTIVE。
- `postgres/agent_memory.go` 普通本人PUT仅创建/更新EXPLICIT；strict wire不能提供confidence/sourceType/status/owner。005人工来源关联权重1不是概率，也不会改Memory.confidence。
- `agentcognitive/unavailable.go` 的机器MemoryReader/CandidateSubmitter仍Unavailable。AGENT_ONLY允许本人review，不为模型或组织授读取。
- 原源架构要求声明UNCALIBRATED_SCORE/ORDINAL/CALIBRATED_PROBABILITY语义、同次媒体作为一个source cluster，未经校准不能把0.86当正确概率。现库没有可信校准数据/模型版本/验证报告；CALIBRATED能力不能通过传ID自证。
- 008新增语义描述和精确当前本人metadata reader；不生成分数、不做证据聚合、强化或衰减。聚合属于AIR045等后续，候选接纳/校准须独立验收。

## 实施后核验

`agentconfidence`新增明确数值语义、固定中文说明和精确当前本人的metadata reader；查询后最终statement重新解析session/Agent/metadata/Memory版本与期限，输出实际PG clock另校验absolute/idle期限。nil或取消context返回空结果。没有新增schema或HTTP，没有修改原004/005源文件。

冻结版本在三个不同fresh001–057+3seed库实际运行：scope-round5/6/7各88 PASS、0 FAIL、0 SKIP，vet/build均exit0；完整public表前后行一致、旧Memory/Evidence字节一致、新包前后hash一致、自有fixture清理及DB drop核证。88是Go父/子测试pass事件数，54为纯测试，34为真实PG测试事件；不是88名真人验收或88条业务能力。

最初round1/2的失败分别为夹具误用accounts默认UUID及误以为新Person账户自动生成Agent；round3两个过期session夹具违反003时间约束，修正时以真实迁移约束为准。失败原日志保留，不把这些夹具故障称为权限漏洞RED；旧runner在round1–3只保存测试后的源hash，不作最终冻结源码证明。

可调用范围及可复现收据见[置信度合同](../architecture/AGENT-MEMORY-CONFIDENCE-V5.md)和[本地验收证据](../testing/evidence/agent-memory-confidence-2026-10-03/README.md)。审计、原字段形状、纯类型和本地合成不能证明生产身份、校准、推断接纳、模型读取、媒体聚合或真机界面。
