# AGE-009 原生强化审计与验收边界

2026-10-03。源要求：`docs/product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md:580`，唯一队列 BT-V5-AGE-009。原只读审计在 AGE-031 lease 下写入 `work/v5-age031/read-only-next-memory-reinforcement-audit.md`，实施领取后复制至 `work/v5-age009/source-audit-from-age031.md` 留来源；未修改原审计、031 源、队列或共用报告。

## 接口判断

- 原生 `agentmemory.Record` 是唯一 Memory：`model.go:236/267/289` 限定 EXPLICIT=1、lastReinforcedAt=nil；INFERRED 不能 ACTIVE。
- `postgres/agent_memory.go:117/257/274` 已具本人 session/Agent/metadata、CAS、真实期限和终态删除，可复用锁绑定；没有原生 Reinforce 方法。
- 005 `agent_memory_evidence.go:53/81/108/148/193/270/315` 有当前 Moment/Participation/SavedPlace 真实 source gate、来源锁、当前 Memory/session 最终 SQL、手工 attachment CAS、删除 scrub 和当前读取回收。
- `agentmemory/evidence.go:166/198` 只按同 sourceType/ID 去重，未做跨类型同 Activity 去重；`moments.go:14`、`content/moment.go:37` 与实际 moment_activity_links/participation.activity_id 可补原生来源簇。
- 056:138/166、057:48/84/91 意味着直接提升 Memory.version 会清空原 Evidence，不能重新绑定其不可变 Memory version。故 060 新增独立支持 CAS，并在 Memory/Evidence 改变时保守清除，不改旧 shape。
- 008 confidence `model.go:50/84`、`reader.go:43` 是声明/ordinal/uncalibrated 语义，校准 probability 仍不可用；认知端口 `agentcognitive/unavailable.go:45/49` 仍关闭。支持计数不得当真概率或新推断许可。

## 实际实现

新 `agentreinforcement` 包＋同 postgres 新文件复用原 helper，构造实际 Store/066 Controller 的本人 service；Preview/Review/Approve/Read 可调用且有真实 PG 验证。支持始终绑定原 Memory/Evidence 版本，不创建 Memory，不改 confidence/声明、lastReinforcedAt 或长期有效期。同事件跨类型及媒体不重复支持，真正独立来源簇可增加计数。

预览 capability 不可 JSON 自证；Review 只提供具体计划元数据。批准重新核查当前主体/session/Agent/metadata、开关 generation、DB 时间、原 Memory/支持 CAS、source epoch/links。SQL 原 gate 与簇元数据来自同快照；原 source row 锁复用 005 顺序。实际 INSERT 的等待后 ACL/Session 失效会回滚；撤销与真实时间到期不会返回旧 payload。

实际 scope 为根登记的新包、postgres 两个新文件、060 up/down、新 canonical/audit/evidence/work。没有 HTTP/server/Flutter、旧 Memory/Evidence/Confidence 文件或 056/057 改动。schema 与根 038 的 061 由根协调；本包本地验证限定 001–060，非盲目全 schema 并行。

## 验收分类

原 source 要求的“已有 Memory＋新增 Evidence→不生成多个 Memory 的强化”可按人工明确批准的 CODE_AND_LOCAL_VERIFICATION 路径验收，须由根独立核证实际最后源码/日志；只 pure fixture 或服务合同不够。

本轮没有自动语义归并、机器推断、INFERRED 激活、校准概率、当前机器用途 resolver、模型出口/真实 Agent 写或 UI。普通本人事实/ON flag/JSON confirmed 不填补这些权限；`ReinforceForCognition` 为实际硬 Unavailable。若将上述额外能力作为验收要求，分类应为 Partial，不能使用本轮人工支持证据冒充其通过。未做部署/LIVE/消费验收。

实际验证与失败历史、六源 SHA、原数据对照/DDL往返/DB清理记录见 `docs/testing/evidence/agent-memory-reinforcement-2026-10-03/README.md`；没有由旧共同 059/5484 结果推断本轮新增 source 已受全量回归。
