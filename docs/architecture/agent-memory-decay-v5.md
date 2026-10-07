# Agent Memory Decay V5

对应 BT-V5-AGE-010。2026-10-03。本页记录原生元数据衰减能力；不是新的 Memory 真源、模型接口或概率校准。

## 已有边界与增量

复用 `agent_memories`（056）的精确 Person / Personal Agent / metadata 归属、独立 Memory version、baseline confidence、source_type、created_at、last_reinforced_at、有效期。INFERRED 只支持 PENDING_REVIEW；EXPLICIT 来自本人的直接填写。060 的 EXPLICIT reinforcement 来源支持 `last_support_at` 不替代推断的 `last_reinforced_at`。不改 056/057/060、旧 Store、HTTP、Cognition 或 DDL。

新增 `agentdecay.Project` 与 `postgres.Store.ReadOwnMemoryDecay`。后者是原生、人类本人限定的内部元数据读取端口，未安装 HTTP/UI/模型 adapter。不读取或返回 summary/structured_value。投影不持久化，因此不会改变原始 confidence、version、status、created_at、updated_at 或 reinforcement；也无需部署调度器。

## 评分语义

版本 `inferred-grace30-half90-v1` 为仓库内初始保守策略：INFERRED 从实际 last_reinforced_at（无则 created_at）起前 30 天维持 baseline；之后 effective confidence = baseline × 2^(-无强化超出30天的秒数/90天秒数)。每次按 baseline 计算，不使用前次投影复利。updated_at、读取时间、审计时间、060 last_support_at 不重置计时。零 baseline 不增加。

effective confidence 标为 `UNCALIBRATED_SCORE`，不是准确概率、校准结论、活动参与意愿或客观事实。INFERRED 始终待审阅；分数不激活、授予 consent、替代来源核验或执行批准。显式陈述保持 DIRECT_DECLARATION=1，不自动衰减；有效期仍独立执行。该默认时间策略尚未经现实用户研究或生产校准。

## 当前权限和时间

每次读取做两次原生 SQL。最终 statement 重新解析 token digest 对应的有效 Session、active Person、精确 active Personal Agent、存在的本人 metadata 与 Memory ID/version/status/validity，并使用 PostgreSQL clock_timestamp。权限/主体/版本/过期/删除/取消变化在最终边界前生效时不得返回第一次读取的旧结果。没有假 token、客户端 confirmed 或 Agent flag 绕过。拒绝跨主体、匿名、开发身份（devPhoneEnabled=false）、未知/失效时间与未来 reinforcement。

此最终 statement 是读取排序边界，不承诺回收已经发出的响应。此投影不验证外部 Evidence 存活或 consent，不返回其内容，不成为任何 source/purpose/model access 许可。新增 modelAccess 永远 UNAVAILABLE，既有不可用认知端口保持不可用。

## 验收状态

审计与实施先暂存 `work/v5-age010/stage`；根协调者原 071 全 Go 冻结释放后才落 4 个限定 Go 文件。当前最终源实测 `work/v5-age010/native2/result.json`：fresh 001–071 隔离库，59 Test PASS / 0 FAIL / 0 SKIP，test/vet/build exit 0，631 个 Go/SQL/mod 源保持同一帧，完整 public 行和函数/触发器/约束 catalog 无变化，旧非空数据保留，owned `birdtie_decay_561a4c5336a6` 实际 DROP。原始命令、全部输出、原生快照与源 bytes 见专属证据目录。此前 native1 的 57 Test PASS 为增加自然时钟 deadline 用例前的历史帧，不替代最终源。

覆盖 30 天边界、半衰期、单调降低、零值、真实 last_reinforced_at、updated_at 不重置、反复读不复利、120 天显式不衰减、当前会话、精确主体/版本、未来/删除/存储过期、最后查询前撤权/自然会话和 Memory 有效期到期/版本变化/metadata 删除/取消；独立重连且 session 时区 Honolulu 与默认 repeatable read 仍以 PG 当前时间给 UTC 投影。private canary 没有进入投影，既有认知 MemoryReader 仍 Unavailable。

首次只做可编译检查的命令未设置 disposable DB，原生 fixtures 明确 SKIP，未将其当原生通过。上述两轮实际隔离库 0 SKIP。无新 schema，因此无需本任务 DDL down/reapply；原 071 上迁移的旧数据与完整 catalog 比对只是本地测试环境准备，不能声称本任务实现了生产迁移或发布。根协调者后续当前源完整默认并发 Go 回归另记证据，不引用增加本任务文件前的完整 Go 历史帧。

本轮没有新增页面或 HTTP，UX-CHECK-07/08 的主体、撤权与迟到边界由原生测试核验；UX-CHECK-12 仅核验独立数据库连接重建后的相同 baseline，不称 App 重启实测。视觉、键盘、读屏、真机截图不适用于未新增的 UI，也未运行。现实源/推断 producer/模型、生产 IdP/部署与真实 A→H 均不在本功能的仓库局部验收中，Closed Pilot / Consumer Beta 仍 NO。
