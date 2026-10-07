# 原生 Moment 运行的公平领取与额度

2026-10-06，BT-V5-AIR-020 的增量实现说明。权威运行模型仍为 [AGENT-RUN-ENRICHMENT-V5](AGENT-RUN-ENRICHMENT-V5.md)，不新增需求队列或模型权限。

## 实际接线

原 `AgentRuns.claimAgentRun` 调用 `selectAgentRunDispatch`，在原领取事务、Run 行锁、fence、候选 writer 和结果对账内增加领取控制。所有采用新实现的领取进程先取得同一事务级 advisory lock，再以真实数据库时钟统计未到期的 RUNNING 租约：全局最多 4、每个本人最多 1。锁随原事务提交或回滚释放；领取不调模型、不发消息、不增加源权限。

从原运行表读取每个 ready 用户的一项队列头及原 `agent_run_audit` 最近领取时间。最多读取 256 个用户头，SQL 先按可用额度、最近服务时间排序，Go 复核元数据并选择最久未服务的人；同用户仍按原 next_attempt_at/id 领取。队列长度不提高优先级，一个用户的大量 ready 运行不会占满所有领取槽。元数据异常闭合失败；选定行已退休/被锁以及额度占用返回独立 `ErrDispatchBusy`。原有限 worker 明确记录 throttled 并结束本轮，不将其当空队列，不立即轮询或重发。

## 复用边界

- 原 Moment/current source 校验、原 outbox 的 owner/Agent/logical-operation/version 去重及具体批准保持；旧版本不能由调度控制获得写权限。
- 原 090 保持不变：同根最多原 5 次加人工核验的 1 次恢复；恢复深度最多 1，不能续原 deadline 或跨主体。它是当前原生 Moment 运行的实际根预算与链深度限制。
- 原封闭事件目录不接收 `MemoryUpdated`，没有新建记忆自触发/A2A消费循环。通用 AIR root_trace/causation 调度和跨事件链控制仍未实现，不能把元数据字段称为已上线的事件调度能力。
- 原任务/实体/源 ID、审核、effects 和授权真源不改。无需新迁移，没有激活旧手机或生产运行服务。

## 验证与剩余缺口

仅相关 Go 单元：新领取控制及原有限 worker 共 20 个 run/pass 事件；另独立执行原 5 个运行状态/租约/恢复单位（86 个 run/pass，含子场景），均 exit0、0失败/跳过。命令、目录、日志、差异见 `docs/testing/evidence/run-dispatch-bounds-2026-10-06/README.md`。10,000 对 10 的突发场景验证的是实际 Go 选择内核；native 核心使用 pgx spy 验证锁先行、真实时钟查询、所选 owner 绑定及 SKIP LOCKED 路径，**不是数据库压测**。

任务保留 **PARTIAL**：真实 PG 多进程公平性/锁等待/并发额度及性能、通用事件因果链/短窗口刷新合并、整合构建与运行未验证或未实现。部署时必须确保所有领取进程采用同一新实现；没有进行部署。全面测试、vet/build、真机及外部服务均 NOT_RUN。Closed Pilot / Consumer Beta 仍 NO。


## 2026-10-06 消费链错误分类修正

在当前真实 `claimAgentRun → selectAgentRunDispatch → runError → RunOnce` 链中，选择器已经返回额度占用 `ErrDispatchBusy` 和真正无 ready 用户 `ErrNotFound`，但原 `runError` 会将它们统一改为 `ErrUnavailable`，使原 worker 无法区分限流和空队列。本次仅在已有输入、权限、过期、版本与 PostgreSQL 冲突分类之后保留这两个封闭 sentinel；不移除 `pgx.ErrNoRows → ErrDenied`，不返回未净化底层错误，不改当前 source/session/具体许可/fence 校验。

首次实际相关单元 RED 为 25 个 run、15 个 pass、10 个 fail（8 个子场景及 2 个父测试，非编译失败）。覆盖直接及 wrapped sentinel、真实选择器到生产 mapper 再到原有限 worker 的组合。修复后运行相关新旧单位：9 个顶层、45 个 run/pass、0 fail/skip、3 个包，exit0；其中原 20 个领取/有限 worker 场景保持。本轮未重复执行原 86 个恢复状态场景。

组合单位中的 pgx transaction 与 worker backend 是明确的 unit spy，仅调用实际选择器、生产错误 mapper 和原 `RunOnce`，不表示原生数据库或完整 claim 事务已实测。两个命令的 Go/module 输入 SHA 前后均稳定，详见 `docs/testing/evidence/event-storm-consumer-2026-10-06/README.md`。

本次仍不足以关闭整个 AIR020：通用 AIR 因果根及最大链深度消费、跨事件短窗口刷新合并没有实际 consumer 接线；现封闭目录不接收 `MemoryUpdated`，不能据此宣称已有通用循环检测。090 的根 6/深度 1 仅对应原生 Moment 人工恢复。真实 PG 并发/锁/公平性/吞吐量、集成、迁移、全量、vet/build、真机、外部服务均 NOT_RUN，任务继续 PARTIAL，Closed Pilot / Consumer Beta NO。
