# Agent Memory Confidence

2026-10-03。当前AGE008的语义与本人只读合同，接续[Memory架构](AGENT-MEMORY-ARCHITECTURE.md)、[认知ADR](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md)和[AGE008审计](../research/BIRDTIE-V5-AGE-008-AUDIT.md)。权威Memory仍是原`agent_memories`，本任务没有新表、回填或另一份Memory。

## 1 当前数值含义

| semantics | 合法形状 | 当前能力与说明 |
| --- | --- | --- |
| DIRECT_DECLARATION | 有限数值1；没有level | 原生本人EXPLICIT人工填写的来源标记，不表示内容正确概率或系统核验事实 |
| UNCALIBRATED_SCORE | 有限数值0..1；没有level | 明确未校准的评分类型；本任务不生成或写入评分，0.86不能解释为86%正确概率 |
| ORDINAL | LOW/MEDIUM/HIGH；没有数值 | 有界等级类型，不转换为概率 |
| CALIBRATED_PROBABILITY | 枚举预留；合法数值构造仍拒绝 | 缺真实校准资料、模型版本和验证报告，当前统一ErrUnavailable；调用方传ID或分数不能自证校准 |

bool、字符串、NaN、Infinity、负数、超过1、混合数值/等级、未知语义均拒绝。`NormalizeAssessment`是结构校验，不能证明来源、同意、Agent归属或模型输出可靠性。

本人说明固定为中文：`本人明确填写；数值1标记直接声明，不代表正确概率。`、`未校准评分，不代表正确概率。`、`置信等级不代表正确概率。`。说明没有任意模型文本或原始私人正文。

## 2 实际内部本人reader

`agentconfidence.NewHumanReader(pool, devPhoneEnabled)`提供以下内部只读方法：

```go
ReadOwnMemoryConfidence(ctx, agentprofile.PrivateAccess, memoryID, expectedVersion)
    (agentconfidence.OwnMemoryView, error)
```

`PrivateAccess`沿用不可JSON自证的服务端digest与typed PERSON身份；expectedVersion必须正值。每次第一条metadata SQL从当前真实session解析activePerson及其精确activePersonalAgent、同owner的metadata和Memory。第二条最终boundary SQL在返回前重读这些原生状态、Memory版本与期限；输出时的真实PG clock另核会话expires/idle和Memory期限。nil context、取消或超时拒绝；最终版本变化、撤会话、停用主体/Agent、删除metadata、删Memory或到期不返回旧结果。

返回只有schema、Memory ID/version、精确Agent/owner、sourceType、assessment、固定中文说明和有效期/读取时间。不SELECT summary、structuredValue、Evidence正文或第三方内容；不写表、延长session、更新confidence、激活候选或授予权限。逻辑到期仅拒绝，原ACTIVE行及版本不因此改写。无metadata不补造；跨主体拒绝。最终SQL statement是权限线性化点，不承诺网络交付后绝对回收。

原生EXPLICIT=1返回DIRECT_DECLARATION。DDL允许的INFERRED PENDING_REVIEW/EXPIRED/DELETED形状仍ErrUnavailable；测试中的原始SQL形状正例不等于推断接纳服务。默认`devPhoneEnabled=false`拒绝dev_phone会话；本地合成`test`会话不证明生产IdP。

## 3 保持关闭的范围

- 没有新增HTTP/UI入口。当前只有经过真实PG验证的内部本人management reader；尚无客户端展示或真机证据。
- `agentcognitive.UnavailableCognitivePorts`不变，本人reader不实现机器MemoryReader。AGENT_ONLY不开放Runtime；组织、Business、Community不能继承本人Memory。
- 没有估计、强化、衰减、候选提升、校准、媒体分析、跨证据聚合或模型出网。Confidence不会产生分析同意或免审阅权限。
- 后续聚合须使用来源簇去重；同一次来源的多张图片不能独立累加。本任务没有聚合器，因此没有以图片数量提高置信度的路径；真实独立文字记录的新增不改变本人声明值1，不能据此声称媒体聚合已验收。
- 056原字段和写入合同、057Evidence权重/来源、原身份/资料版本及所有既有API保留。Private字段受众设置和普通profile_view许可不能当模型Memory读取许可。

## 4 验证与发布

本地验证使用随机独占库、001–057迁移和三个既有开发seed；原生Store保存Memory，真实session/当前Agent及原Memory CAS/删除/到期用于正负用例，最终延迟边界用QueryTracer channel确定具体statement阶段。完整public表源行前后比对、旧Memory/Evidence源hash和精确自有fixture清理另记录。

实际命令、初次失败、最终计数和SHA收据见`docs/testing/evidence/agent-memory-confidence-2026-10-03/`的验收记录。纯类型测试、跳过PG的本地命令和真实PG测试分别记录，不相互替代。没有新迁移，未执行生产down或修改外部服务。

适用UX-CHECK-06（区分声明、未校准评分、未知和过期）、08（换主体、撤权、版本变化后不复用旧结果）、09（只读重试没有重复副作用）、16（不记录或返回原Memory/聊天/媒体/模型输入）；中文说明遵守[持续交互规则](../ux/GLOBAL-UX-INTERACTION-CONTRACT.md)。没有新增界面，因此Flutter/键盘/辅助技术/真机未在本任务运行。实际证据与未测范围分开记录，Closed Pilot与Consumer Beta保持NO；该本地功能不提供真实IdP、部署、地图或CSSA试点证据。
