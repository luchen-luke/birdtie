# Birdtie V5 增量执行协议

2026-10-02。接续 [V4执行协议](CODEX_V4_EXECUTION_PROTOCOL.md)，不替代旧队列与发布门槛。用户要求完成一项立即领取下一项，不等待定时触发。

1. 唯一任务状态仍为 `automation/codex_task_queue.json`。先 validate/summary/next；默认串行当前 IN_PROGRESS 优先。用户最新授权的独立需求按下述受控并行段推进，最多3项，不跳依赖或写入冲突。每项审计、计划、实现、适用检查/场景、证据、状态后立即 next/start。
2. 137源要求在 [映射](v5_requirement_mapping.json) 去重，128未覆盖来源合并108项；6复用、3覆盖核验不再建任务。保留旧144整对象，不能从旧DONE推断新source完整完成。查所领任务的source/verification overlay与关联依赖。
3. AGE/AIR按实际接口依赖交错；必要P1前置不能永久被无关P0挤压。执行排序不修改原priority/phase、不解除未知/外部/Post-Pilot门禁、不让未完成依赖可领取。
4. 先读 [认知ADR](../docs/decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md)、[Memory边界](../docs/architecture/AGENT-MEMORY-ARCHITECTURE.md)、[AIR设计](../docs/architecture/AGENT-INTELLIGENCE-RUNTIME-V5.md)、[AGE原文](../docs/product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md) 与对应已有canonical。复用actorref/角色框架及现有领域数据；模型不成为权限、记忆或业务事实真源。
5. 未配置/未批准的推理端口、模型出口、真实自动写、视觉和A2A保持Unavailable/默认关闭。新接口/ADR/fixture不称服务可用。CODE_LOCAL与关联-LIVE各自记录；5个LIVE外阻不以fake通过代替，也不拦无外部依赖代码。
6. 界面以中文为主，遵循 [唯一持续交互规则](../docs/ux/GLOBAL-UX-INTERACTION-CONTRACT.md)，引用实际对应UX-CHECK。消费评估只补原任务核验：[原文（实际文件名带1）](../docs/product/BirdTie-Consumer-Readiness-Assessment-2026-10-02%20(1).md)，不重复导入功能。
7. 只做仓库/隔离本地/已授权真机验证；保留用户未提交内容与来源路径。生产身份、现实授权活动、HTTPS/地图、部署日志、值守、提醒运营与真实A→H未齐时Closed Pilot/Consumer Beta均NO。不联系合作方、正式部署、付费、改外部服务或试发真实用户。
8. 记录正负命令/结果、初次失败、真实修复、未测范围和具体外阻。回归必须覆盖旧身份/授权/发现/RSVP/Plans及撤权/迟到状态；测试不得因不够fixtures跳过后计为完成。

## 受控并行领取

2026-10-02用户明确要求多个独立需求同步执行。仅根代理实际修改live队列和共用总报告，worker只在已登记范围实现及交付证据。根代理先执行`parallel-config --coordinator root --limit 3`，为当前IN_PROGRESS使用`lease <ID> --coordinator root --owner root --write-scope <精确文件>`，再读`next --parallel`候选并审计实际接口与写入范围；可用`start <TODO_ID> --parallel --coordinator root --owner worker-a --write-scope <文件或独占新包>`领取，scope可重复。

配置/active leases/释放历史只新增队列根`parallel_execution`，旧252任务对象、source映射和原Gate不重置。最多根代理加两名实施worker；owner唯一、写入scope大小写不敏感且路径组件不重叠，拒绝相同/父子包含、空/仓库根、未知顶层、穿越、仓库外或通配符。声明所有将修改文件，优先精确文件或独占新包，不能用目录粗占阻塞无关任务；有共同DDL、共享领域真源或接口前置时协调后保持串行。

`next --parallel`只读列READY候选，沿用priority及必要P1前置公平排序，不猜scope或改变状态。并行start只接受依赖全部DONE的TODO；LIVE_ONLY、外部/activation gate非空、未知completion/gate、任何release_stage及BLOCKED/PARTIAL不得自动解除。根代理完成核证后用`done/block/partial --coordinator root`释放对应lease并保留历史，其余active继续。所有写命令携声明coordinator，持久文件锁加读取hash防止误覆盖；coordinator不是OS身份或业务权限，worker不得自行save queue。原LIVE阻碍、privacy与Closed Pilot/Consumer Beta NO门槛保持。

## 2026-10-06 用户更新：相关单元测试优先，手机离线继续

用户明确要求减少全面测试，当前开发每项默认只执行本需求直接相关的单元测试（Flutter 可包含 widget 单元）。不因一个小项重复全 Flutter/Go suite、全量 analyze/vet/build 或数据库整检；必要的整合与发布验证单独安排，未运行范围明确记录 NOT_RUN，不沿用旧版本结果。

手机长时间离线时继续已授权仓库实现与相关单元验证，完成证据及状态后立即领取下一项，不等待设备、回复或定时触发。任务要求的实际持久化、迁移、真实认证、地图、真机或运营证据尚缺时保留 PARTIAL/BLOCKED/NOT_RUN；单元测试通过仅证明其覆盖范围，不自动满足完整产品或发布门槛。本节覆盖旧协议的日常全面验证频率，既有测试与历史证据保持。
