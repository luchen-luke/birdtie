# BT-V5-AGE-025：重复活动偏好专项审计

2026-10-03。结论：**NOT_IMPLEMENTED；建议 BLOCKED（仓库内部权威来源/消费接口缺失）**。本轮只写根登记的本文件及 `work/v5-age025`，没有修改 live 队列、产品 Go/DDL/UI 或现有 DONE 状态。不是缺外部模型凭据，也不是再次建立 RSVP 或兴趣数据库的理由。

## 原需求和当前依赖

来源 `docs/product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md:1021` AGE-025：连续参加 badminton 可以强化 badminton preference。当前队列仅依赖 AGE009、AGE024、INT001，均 DONE，但这些局部完成范围没有提供权威参加历史及兴趣分类强化接口。不能因为依赖状态为 DONE 就将原生报名当已到场，或把人工来源支持当自动兴趣结论。

| 能力 | 实际代码证据 | 本任务可复用/缺口 |
| --- | --- | --- |
| 活动及分类 | 021 `activities.category_code` 与真实 activity revision | 可复用既有分类和领域 ID，不另建活动/分类副本 |
| RSVP | 021 `activity_participations.status` 仅 going/pending/cancelled；`postgres/activity_participations.go:19` 的 completed 是活动 ends_at 投影 | 报名/取消事实，不是本人 attendance |
| AGE024 参与信号 | `agentparticipationsignal/signal.go:116` 强制 Attendance/StableInterest=UNKNOWN、ProcessingStatus=UNAVAILABLE；reader 当前 SQL 只读尚未结束的活动 | 可复用当前授权/版本/TTL思想；没有历史确认到场或稳定兴趣 |
| AGE064 完成事件 | `agentevent/registry.go:104` ActivityCompleted 为 SourceUnavailable，原因 NO_NATIVE_ATTENDANCE_OR_COMPLETION_FACT | 有目录名称，没有可消费的完成/到场来源 |
| AGE005 Evidence | `agentmemory/evidence.go` 为 HUMAN MANUAL_REFERENCE；闭集 Moment/Participation/SavedPlace；eventTime 为更新/收藏时间 | 原 ID/version/来源核验可复用；不是到场、分析或长期保留许可 |
| AGE009 强化 | `postgres/agent_memory_reinforcement.go:106` 仅 EXPLICIT ACTIVE；060 支持原 Memory 的已批准 Evidence/来源簇和 last_support_at | 去重/版本/批准边界可复用；不聚合 category、不改变 inferred confidence/lastReinforcedAt；ReinforceForCognition 硬 Unavailable |
| AGE010 衰减 | 已实现现有 Memory 的有效评分投影 | 不是推断 producer 或强化写入入口，不能生成参与事实 |
| 已有关系 Context | `postgres/relationship_context.go:99` shared activities 使用双方 going 与 disclosure/ACL | 不称权威共同到场，不转存到兴趣或出席历史 |
| 机器用途桥 | `agentcognitive/unavailable.go:47/52` ReadMemory/SubmitMemoryCandidate 缺当前 purpose/独立 retention 时拒绝；AGE033/062 仍 PARTIAL | 人工可见或 Memory flag ON 不授权私人分析、自动长期写入 |

消费评估 `BirdTie-Consumer-Readiness-Assessment-2026-10-02 (1).md:297–305` 要求 RSVP 与 attendance 分离，确认方式需获准且可纠错，未知仍未知，不从 GPS、照片或报名认定。这里只补原验收要求，不导入第二套任务。

## 内部分工与依赖修正建议

原队列 ACTN002「shared real activity 后自愿连接」与 ACTN003「共享活动/context history」仍 TODO。AGE026 的原增量 acceptance 已明确 **ACTN002/003 拥有出席与历史领域，026 只适配已核验共同出席到 AGE**；025 应复用同一个事实来源，不另造 attendance 表或公共关系真源。

- 建议根在 AGE025 的 dependency record 追加 ACTN002/003 的领域前置依据，实际 `depends_on` 至少等待 ACTN003 提供可调用、版本化的本人确认参加历史。如果确认来源 writer 最终由 ACTN002 实施，则亦将 ACTN002 设为必需依赖；当前两个任务 acceptance 尚未明确哪一个实现 writer，不能把「提出连接建议」自动解释为已实现到场生产者。
- 可以保守追加两者作为当前恢复门槛，并在它们完成后的真实接口审计中缩减为实际 provider；不要仅凭它们被标 DONE 解除 blocker。
- **AGE026 不机械作为 AGE025 前置**：前者需要双主体共同出席与关系权限；后者需要本人按活动类别聚合。应共享原 attendance source ID/version/current resolver，领域事实入口齐全后可以独立并行消费，不把关系适配强行串到个人偏好前面。
- 不增加重复 attendance/RSVP/backlog，也不改旧 DONE 的实现证据。建议记录存于 `work/v5-age025/dependency-record-proposal.json`，由根核实后更新唯一队列。

## 精确恢复条件

1. ACTN 领域提供实际持久 authority：本人/Activity 的 attendance 或完成证据，稳定 source ID、独立版本、适用模式（线下/线上/混合）、确认方与获准的确认方法、事实时间、纠正/撤销/未知状态。RSVP、endAt、Moment、GPS 单独不能生成确认。
2. 提供当前身份/精确 Personal Agent/来源归属/Activity 当前版本与分类/visibility/block/session 的 native reader/revalidator；来源改期、取消、纠错、删除或撤权能使旧 evidence 与晚到响应失效。005 来源闭集如何扩展由原 Evidence owner 协调，不能只插 SQL fixture 绕过 native writer。
3. 025 复用真实不同活动 occurrence/source cluster 去重，同一活动多条报名、Moment、重投递只算一次；类别来自同一 Activity 的权威 code/version，不按字符串标题猜测。重复行为仅形成带来源的偏好支持/待审阅建议，绝不自动变成客观兴趣事实或 ACTIVE。
4. 选择实际消费模式：若为本人明确审阅支持，复用009的具体预览、单次批准、原 Memory ID 与 CAS，并接通新的受授权 source；若原目标确需机器分析/推断，则先提供 source-purpose 与独立持久保留许可的 current resolver，不能以「用户可以查看」或 model confirmed 替代。此确定性能力不必依赖真实 LLM 凭据，但也不能调用现有硬关闭端口并声称成功。
5. 原生验证至少覆盖三个不同已确认活动、重复同一 source、未确认 RSVP/结束但缺席、纠错/取消、source/Memory版本变化、撤权与晚到、跨主体、类别改动、幂等重启，以及不生成多份相同偏好 Memory。以上当前均未运行，不能用合成 CONFIRMED 字段测试替代真实领域动作。

## 本轮实际命令与证据

`python work/v5-age025/inspect.py` 读取实际队列、18 个产品/规范源、所有 up migrations 的表定义/attendance关键词，保存 source bytes、行号、SHA、任务完整快照与 lease；结构核对 PASS，输出 `work/v5-age025/audit-source-snapshot.json`。这是静态依赖审计，不是功能测试。

本轮 Go/Flutter/PG/真机测试 **NOT_RUN（仅审计，无产品实现）**；根当前全 Go/Client 构建用于其他已实现任务，不能成为025功能完成证据。原 IdP/HTTPS/真实 CSSA 活动/地图/部署日志/值守和 A→H 缺口不因本文改变，Closed Pilot / Consumer Beta **NO**。建议根记录内部 blocker 后立即领取独立可执行任务，不等待定时。
