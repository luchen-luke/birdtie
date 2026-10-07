<!-- 来源：D:\Project\birdtie\BirdTie-BT-V5-AIR-Codex-Package-2026-10-02.zip!/BT-V5-AIR-REQUIREMENTS.md；原文 SHA256 3ad10255bb7056849215e04763f9d6800e4fc753a155fadba1dd46134dc63406。2026-10-02 选取收录，原ZIP/work来源保留。 -->

> 仓库接入说明：下文原始证据范围及“未取得AGE/未导入”指编制当时；现四份材料已读取、137源项已对账，安全检查点后追加108去重任务。实现状态只见 automation/codex_task_queue.json 与 BIRDTIE-V5-MATERIAL-RECONCILIATION-2026-10-02.md；源码接口/Debug/fake不代表完整Memory/模型/生产能力。AIR拥有推理和提案，AGE拥有权威数据；ADR及AGENTS/既有领域规范共同约束实现。原Closed Pilot门槛NO；当前不授权模型出网/正式部署/真实Agent自动写或A2A。源要求source别名和独立LIVE验证见 automation/v5_requirement_mapping.json。

# BirdTie BT V5 AIR 完整需求清单

共56项：26 P0、25 P1、5 P2。初始均为 TODO_AUDIT / UNVERIFIED，表示待对账的规划，不是当前代码缺失统计。

以 BT-V5-AIR-DESIGN.md 的模块、权限、状态机和契约为准。dependencies是本包前置，existing_requirement_refs是需审计绑定的旧需求锚点，external_prerequisites是外部条件。P0最小持久化、脱敏审计与沙箱效果账本必须完成，P1的完整迁移/观测/真实写工具不会阻塞或取代这些P0控制。

## BT-V5-AIR-001 读取安全检查点和真实开发快照

优先级：P0　分组：A 审计接入　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

先确认运行中的 Codex 任务、文件归属、当前分支/提交/未提交工作与队列格式。活跃写入期间只读；同一工作项完成且现有执行者记录检查点后才接入。不能把另一版本规划数量当完成数量。

- 输入：现有任务状态、只读 Git 状态、已有报告和队列
- 输出：开发快照、检查点证据、不可触碰文件集合
- 前置：无
- 复用锚点：BT-V4-AUD-001, BT-V4-AUD-002, BT-V4-AUT-001
- 外部条件：SAFE_CHECKPOINT
- 正向验收：记录时间、仓库根、分支、提交、运行任务、已有变更；正在编译时仅生成待接入说明
- 负向验收：不得停止进程、夺取锁或运行 abort/reset/revert/stash/discard/clean；无证据时标明 UNKNOWN
- 完成证据：快照摘要、现有执行者的安全检查点记录；不收集凭证

## BT-V5-AIR-002 对账 V4 AGE AIR 并复用已有实现

优先级：P0　分组：A 审计接入　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

逐项核对 V4、Community、AGE 及本 AIR 的代码、契约、迁移、测试和用户可见入口。已完成模块优先复用，部分完成只补差异。AGE 的 81 项细节未见原文不得猜编号。

- 输入：规范包、001快照、可访问代码和报告
- 输出：追踪矩阵及每条实现分类
- 前置：BT-V5-AIR-001
- 复用锚点：BT-V4-AGF-001, BT-V4-AGF-002, BT-V4-AUD-001, BT-V4-AUD-002, BT-V4-CAN-001, BT-V4-COMM-001
- 外部条件：无新增外部条件
- 正向验收：每条 AIR 对应代码位置/测试或明确待核实；记录旧 ID 和新 ID；分别记录 REAL/PARTIAL/MOCK/HARDCODED/NOT_IMPLEMENTED/UNKNOWN
- 负向验收：规划 TODO 不代表代码未做；测试 mock 不得当真实 provider/生产验证；不重置原队列状态
- 完成证据：BIRDTIE-V5-AIR-GAP-ANALYSIS.md 与 STATUS-RECONCILIATION.md

## BT-V5-AIR-003 固定 AIR 与 AGE 的领域边界

优先级：P0　分组：A 审计接入　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

AIR 负责事件、推理、工具提案和候选生成；AGE 负责 Profile/Memory/Policy/Context 的权威模型与写服务。共享 Agent Runtime 扩展角色能力包，禁止再建同类平行服务。

- 输入：002追踪矩阵、现有 Agent/权限/数据服务
- 输出：ADR、模块责任、版本化接口及错误码
- 前置：BT-V5-AIR-002
- 复用锚点：BT-V4-ACT-001, BT-V4-ADR-001, BT-V4-AGF-001, BT-V4-AGF-002, BT-V4-PRV-001, BT-V4-SAF-004
- 外部条件：无新增外部条件
- 正向验收：Personal/Organization/Business 共用框架但主体/资源/委派权限隔离；现有 AGE 服务有适配器
- 负向验收：不得新建第二套用户记忆真源；模型不获数据库写权限；模型切换不改变 agent_id
- 完成证据：ADR、接口契约测试、数据所有权表

## BT-V5-AIR-004 追加需求队列并按依赖调度

优先级：P0　分组：A 审计接入　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

先读已有队列工具及帮助再以支持的形式追加 AIR。保留 V4/AGE 状态、负责人、证据及进行中任务。按依赖可执行性调度，阶段/优先级作次级排序。

- 输入：现有队列与导入格式、002矩阵、AIR JSON
- 输出：去重追加记录、依赖检查和接续说明
- 前置：BT-V5-AIR-002, BT-V5-AIR-003
- 复用锚点：BT-V4-SAF-001, BT-V4-SAF-004, BT-V4-OPP-001, BT-V4-MOM-001
- 外部条件：SAFE_CHECKPOINT
- 正向验收：二次导入无重复 ID；已满足需求映射为复用并保留证据；检测跨版本环路
- 负向验收：不得臆造 taskctl 命令或批量把旧 DONE 改 TODO；依赖 P1 的 P0 不能永久饿死
- 完成证据：导入 dry-run、前后差异、拓扑检查

## BT-V5-AIR-005 安全迁移和旧客户端兼容

优先级：P1　分组：B 数据演进　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

只用现有数据库和迁移规范增量增加必需表/列；扩展兼容、分批回填、迁移幂等，破坏性清理另行批准。

- 输入：现有 schema、003契约
- 输出：增量迁移、回填作业、兼容说明
- 前置：BT-V5-AIR-003, BT-V5-AIR-004
- 复用锚点：BT-V4-MIG-001
- 外部条件：无新增外部条件
- 正向验收：空库/现有数据升级均通过；旧 Flutter/Go 版本在兼容窗口可读；恢复演练不丢用户数据
- 负向验收：不得占用正在实施的迁移编号；不得在生产跑 destructive down；未验证迁移不能标 DONE
- 完成证据：迁移测试、抽样校验、恢复演练日志

## BT-V5-AIR-006 核验已有产品流程基线

优先级：P1　分组：A 审计接入　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

检查组织发布、发现、详情、RSVP、持久化 Plans、组织点位审核与独立提醒的既有能力，不因 AIR 覆盖原验收。

- 输入：既有报告/测试/运行环境
- 输出：已核验基线与回归清单
- 前置：BT-V5-AIR-001, BT-V5-AIR-002
- 复用锚点：BT-V4-PIL-001, BT-V4-PIL-002, BT-V4-PIL-003, BT-V4-TST-001
- 外部条件：无新增外部条件
- 正向验收：已报告 78 项 Flutter 测试仅作历史记录，执行时记录实际新测试数/版本；PIL 门禁单列
- 负向验收：API health200、合成 E2E 或 Debug 安装不能推出正式登录/真实活动/闭测就绪
- 完成证据：基线回归报告和原门禁引用

## BT-V5-AIR-007 统一 Model Gateway 契约

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

业务只依赖 task kind、规范消息、输出 schema、允许工具和预算；适配器负责 provider 请求/响应转换。使用现有 Go 接口方式。

- 输入：ModelRequest（契约见规范）
- 输出：ModelResult、Usage、ProviderError
- 前置：BT-V5-AIR-003
- 复用锚点：BT-V4-AGF-001, BT-V4-AGF-002
- 外部条件：无新增外部条件
- 正向验收：假 provider 契约测试覆盖 text、结构化输出、tool proposals、拒答、截断及错误
- 负向验收：业务模块不得调用 provider SDK；响应中未知工具/字段拒绝；模型 id 不成为 agent id
- 完成证据：接口测试、依赖扫描、adapter contract suite

## BT-V5-AIR-008 能力登记与路由资格验证

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

按 provider+model+版本记录 vision/tools/schema/streaming/state/storage 能力及验证日期。路由先过滤数据授权和能力，再比较质量/成本。

- 输入：能力记录、租户配置、任务需求
- 输出：合格路由或 UNSUPPORTED_CAPABILITY
- 前置：BT-V5-AIR-007
- 复用锚点：审计后绑定现有模块
- 外部条件：无新增外部条件
- 正向验收：不支持图片的模型不收到图片；没有严格 schema 支持时本地验证仍强制执行
- 负向验收：OpenAI-compatible 不等于 wire/state/tool语义相同；未知能力默认禁用；不得静默降成不受约束输出
- 完成证据：能力矩阵、正反能力测试、官方文档链接

## BT-V5-AIR-009 接入一个已批准的真实推理适配器

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

P0 先落地一个满足文本和结构化提案要求的 provider；模型/API 名从已核验配置选取。开发可用 fake，live readiness 单独验证。 所有 live canary 也必须经过 AIR-011 出口与预算边界，不得直接调用 SDK 绕过。

- 输入：批准的 provider 配置、规范 ModelRequest
- 输出：适配器及合成数据 live canary
- 前置：BT-V5-AIR-007, BT-V5-AIR-008, BT-V5-AIR-011
- 复用锚点：BT-V4-AGF-001
- 外部条件：PROVIDER_APPROVAL, PROVIDER_CREDENTIALS, SPEND_LIMIT
- 正向验收：无凭证也可运行单测；凭证配置后用合成非个人数据验证一次完整请求、响应和计费记录
- 负向验收：fake 成功不能标 live ready；不得把 API key 写进 Flutter、Git、日志或文档
- 完成证据：单测、合成 canary 的 request id/模型版本/耗时/usage

## BT-V5-AIR-010 统一超时重试与隐私安全降级

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

区分可重试限流/临时故障、不可重试认证/能力/参数错误、拒答和结果未知。跨 provider 降级必须同时满足原数据授权与地域/保留策略。

- 输入：ProviderError、run剩余预算、路由策略
- 输出：有界重试或明确失败/等待
- 前置：BT-V5-AIR-007, BT-V5-AIR-008
- 复用锚点：审计后绑定现有模块
- 外部条件：无新增外部条件
- 正向验收：429 按 Retry-After 与抖动退避；超时不会无限递归；无合格路由返回 unavailable
- 负向验收：禁止为了成功把隐私数据发给未授权 provider；拒答不得自动换模型绕过；工具未知结果不盲重试
- 完成证据：故障注入、降级拒绝、预算耗尽测试

## BT-V5-AIR-011 预算与请求前数据出口检查

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

每次模型请求前执行租户/主体/任务预算预留和出口策略；记录 token/费用上界，完成后结算；所有重试计数。密钥只经现有服务端秘密管理注入。

- 输入：许可范围、数据标签、预算配置、ModelRequest
- 输出：EgressDecision、BudgetReservation
- 前置：BT-V5-AIR-007, BT-V5-AIR-008
- 复用锚点：BT-V4-PRV-001, BT-V4-SAF-004
- 外部条件：无新增外部条件
- 正向验收：超预算在出网前拒绝；撤销授权后后续请求停止；同一 root_trace 下级任务共享上限
- 负向验收：缺少价格/费用上界配置时禁止启用付费 live；日志不能包含凭证、完整私聊和原图
- 完成证据：网络边界测试、并发预算原子性、脱敏扫描

## BT-V5-AIR-012 增加第二 provider 和规范历史适配

优先级：P1　分组：C 扩展能力　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

以完整规范历史为真源，显式转换不同角色/tool调用语义。DeepSeek Responses 无服务端 conversation/previous_response_id/storage/background；不依赖其 developer 角色执行策略。

- 输入：已批准第二 provider、规范消息历史
- 输出：第二适配器、能力兼容测试
- 前置：BT-V5-AIR-009, BT-V5-AIR-010, BT-V5-AIR-011, BT-V5-AIR-027, BT-V5-AIR-028
- 复用锚点：BT-V4-AGF-002
- 外部条件：SECOND_PROVIDER_APPROVAL, PROVIDER_CREDENTIALS, SPEND_LIMIT
- 正向验收：同一个固定合成场景可换 provider；工具并行建议由 runtime 串行或按安全依赖执行
- 负向验收：不得盲转 developer 消息或 hosted tools；不得混用 provider response id；历史不得跨主体
- 完成证据：双 adapter 契约测试、role转换快照、历史隔离测试

## BT-V5-AIR-013 依据评测优化模型路由和成本

优先级：P2　分组：F 后续优化　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

先收集任务成功、拒答、延迟和费用，再设置任务级路由；缓存只缓存允许的去标识或主体隔离结果。训练/自托管仅形成可行性 ADR。

- 输入：基准评测、经允许的匿名统计
- 输出：路由实验与是否继续优化的决定
- 前置：BT-V5-AIR-012, BT-V5-AIR-048, BT-V5-AIR-049
- 复用锚点：审计后绑定现有模块
- 外部条件：无新增外部条件
- 正向验收：固定评测集同时报告质量/成本/延迟，降成本不降低关键安全通过率
- 负向验收：不因价格标签假设某品牌更好；不把个人记忆变成用户专属训练权重
- 完成证据：实验记录、回退阈值、成本对比；训练不属于本轮交付

## BT-V5-AIR-014 事件信封与类型注册

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

定义 EventEnvelope 及版本化事件目录。优先复用已有事件机制；文本 MomentCreated 与 UserQuery 作为 P0 两条入口。

- 输入：领域提交事件、用户请求
- 输出：可信事件信封、schema校验
- 前置：BT-V5-AIR-003
- 复用锚点：BT-V4-AGF-001
- 外部条件：无新增外部条件
- 正向验收：事件含稳定 event_id/subject/source_version/occurred_at；服务端解析主体，不信任客户端声明
- 负向验收：重复/未知/过大/错主体事件不能触发模型；历史事件时间不等于当前状态
- 完成证据：schema正反样本、事件 producer 测试

## BT-V5-AIR-015 事务 outbox 与幂等消费

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

领域变更和待处理事件同事务或使用现有等效可靠机制；消费者用 event_id+handler_version+subject 去重，候选提交与效果幂等。 effect_key 基于稳定 logical_operation_id/action_id；handler升级可以重新分析，但不能再次提交同一业务效果。最小持久结构需复用证据或兼容迁移及恢复测试，属于本P0交付。

- 输入：领域事务、事件信封
- 输出：outbox/inbox记录、去重结果
- 前置：BT-V5-AIR-014
- 复用锚点：审计后绑定现有模块
- 外部条件：无新增外部条件
- 正向验收：在 commit 前后和消费中途模拟崩溃，重放后只产生一次候选/效果；达到最终可恢复状态；handler_version升级重放仍不重复已提交效果；必要增量schema升级通过
- 负向验收：不宣称网络全链路 exactly-once；无跨服务事务时用幂等键和对账而非双写假成功
- 完成证据：故障矩阵、重复投递 100 次仍单一业务效果

## BT-V5-AIR-016 持久化有界 AgentRun 状态机

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

运行状态从 QUEUED 到 RUNNING/WAITING_CONFIRMATION/RETRY_WAIT 及 SUCCEEDED/FAILED/CANCELLED/EXPIRED。每步有 checkpoint、deadline、attempt、lease fencing。 P0含最小持久dispatch/effect账本和脱敏审计，记录决策码、主体范围、policy/consent版本、源引用与效果状态，限定访问/保留期且不记录隐藏思维链。新表须兼容增量迁移并测恢复。

- 输入：事件、任务计划、run预算
- 输出：AgentRun、RunStep、可恢复检查点
- 前置：BT-V5-AIR-014, BT-V5-AIR-015
- 复用锚点：审计后绑定现有模块
- 外部条件：无新增外部条件
- 正向验收：进程重启后从安全步骤恢复；取消/过期是终态；重复 worker 不越过 fencing token
- 负向验收：无 while true 推理；恢复不能重复已完成副作用；不能把 waiting当success；lease到期不能重发结果不明动作；缺持久化/审计不能启用live
- 完成证据：状态转换测试、进程 kill/restart 测试、并发 worker 测试

## BT-V5-AIR-017 重试死信和人工恢复入口

优先级：P1　分组：C 扩展能力　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

任务失败分类、有限重试、死信隔离及受控重放；人工恢复须保留原事件/新版本/原因。

- 输入：失败run、重试策略
- 输出：DLQ项、重放请求、告警
- 前置：BT-V5-AIR-010, BT-V5-AIR-016
- 复用锚点：审计后绑定现有模块
- 外部条件：无新增外部条件
- 正向验收：永久错误不重试；临时错误耗尽后入DLQ；重放前重验源版本和授权
- 负向验收：不得批量重放已删除或撤权源；DLQ不保存多余原始隐私payload
- 完成证据：恢复手册、注入失败及重放审计

## BT-V5-AIR-018 撤权删除取消和过期传播

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

将撤权、成员资格变更与授权检查/approval消费/effect ledger登记/dispatch commit放在同一可排序的权威边界，不能仅做检查后立即调用。撤权先于提交则阻止新提交；提交先于撤权则如实标记在飞并对账。候选持久化前同样校验源版本/tombstone/授权epoch，拒绝晚到旧响应。

- 输入：ConsentRevoked、SourceDeleted、CancelRun、授权版本
- 输出：取消状态、拒绝晚到结果、清理指令
- 前置：BT-V5-AIR-011, BT-V5-AIR-015, BT-V5-AIR-016
- 复用锚点：BT-V4-PRV-001, BT-V4-SAF-004
- 外部条件：无新增外部条件
- 正向验收：用barrier测试撤权在dispatch commit前后两种顺序：前者零新提交，后者已在飞效果允许完成/对账但不再追加步骤；源删除/撤权后晚响应不产生新记忆
- 负向验收：不得保证收回已提交网络请求；不能以lease过期绕过未知效果对账；远端数据按实际保留/删除能力披露
- 完成证据：竞态测试、source删除重放测试、取消审计

## BT-V5-AIR-019 计划与上下文触发器

优先级：P1　分组：C 扩展能力　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

在已有调度器中支持用户选择的 digest、活动前提醒和社交请求事件。明确时区/DST/静默时段/去重和触达预算。

- 输入：用户设置、领域事件、schedule version
- 输出：ScheduledEvent、可解释通知提案
- 前置：BT-V5-AIR-014, BT-V5-AIR-016, BT-V5-AIR-018, BT-V5-AIR-040
- 复用锚点：BT-INB-001, BT-NTF-001, BT-V4-NOT-001, BT-V4-PIL-003
- 外部条件：无新增外部条件
- 正向验收：跨DST/重复时刻仅按定义触发一次；用户关闭后不发；重要活动提醒复用原实现
- 负向验收：不能因 AIR 故障停止现有确定性提醒；不默认为每人创建每日推理任务
- 完成证据：时区测试、关停测试、与既有提醒回归

## BT-V5-AIR-020 并发顺序和事件风暴控制

优先级：P1　分组：C 扩展能力　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

按主体/来源版本去重与有界并发；合并短窗口重复刷新；追踪 causation、root预算及最大链深度以阻断A2A/记忆事件自触发风暴。

- 输入：多个主体的乱序/突发事件
- 输出：可控吞吐、隔离队列、超限记录
- 前置：BT-V5-AIR-015, BT-V5-AIR-016, BT-V5-AIR-018
- 复用锚点：审计后绑定现有模块
- 外部条件：无新增外部条件
- 正向验收：同源新旧版本乱序时旧写被拒；一个租户突发不挤占全局；链深度超限终止
- 负向验收：不得无限消费自己生成的 MemoryUpdated；不能把无序队列当顺序保证
- 完成证据：突发压测、租户公平性和循环检测测试

## BT-V5-AIR-021 大型历史导入批处理

优先级：P2　分组：F 后续优化　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

用户主动导入过往 Moments 时分批、断点续传、预估费用和取消；与交互请求隔离配额。

- 输入：明确选择的历史来源、导入授权
- 输出：进度、失败项和可恢复批次
- 前置：BT-V5-AIR-017, BT-V5-AIR-020, BT-V5-AIR-030, BT-V5-AIR-035, BT-V5-AIR-044
- 复用锚点：审计后绑定现有模块
- 外部条件：无新增外部条件
- 正向验收：重新提交同批次不重复记忆；取消后停止后续费用；展示历史拍摄与导入时间
- 负向验收：不能把加入应用等同于授权扫描全部相册；不能阻塞交互和现有开发任务
- 完成证据：大批合成导入、取消与续跑测试

## BT-V5-AIR-022 读取 AGE 权威数据与最小上下文

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

通过现有服务读取完成当前任务所需的 Profile/Memory/Context，限定字段和 token预算。P0 可使用关系库/现有索引，向量库不作前置。

- 输入：已授权主体、task purpose、AGE接口
- 输出：ContextBundle及证据引用
- 前置：BT-V5-AIR-003, BT-V5-AIR-007
- 复用锚点：BT-V4-AGF-001, BT-V4-CTX-001, BT-V4-CTX-002, BT-V4-PRV-001
- 外部条件：无新增外部条件
- 正向验收：来源字段可追溯；同场景只装载相关数据；无 AGE 接口时用版本化适配器/显式阻塞而不新建真源
- 负向验收：不把整个个人档案/私聊历史默认发给模型；缺值写unknown不补造
- 完成证据：AGE契约测试、token裁剪与缺值测试

## BT-V5-AIR-023 主体权限过滤与上下文快照

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

检索前按 subject/tenant/purpose/资源ACL过滤，生成 consent_epoch/resource_versions/policy_version 快照；执行前再次验证。

- 输入：ContextRequest、角色/ACL、源版本
- 输出：最小授权 ContextBundle
- 前置：BT-V5-AIR-018, BT-V5-AIR-022
- 复用锚点：BT-V4-AGF-002, BT-V4-PRV-001, BT-V4-SAF-004
- 外部条件：无新增外部条件
- 正向验收：Personal不能读Organization未授权资料；角色移除后的新授权/dispatch提交被拒；缓存命中仍核验权限且不能恢复旧角色
- 负向验收：不能在全量检索后只靠 prompt 要求保密；拒绝同名实体和客户端伪造主体
- 完成证据：跨租户/角色/撤权安全测试

## BT-V5-AIR-024 来源证据冲突与事实时间模型

优先级：P1　分组：C 扩展能力　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

按字段维护声明者、来源、有效期、观察时间、拍摄时间和采集时间；冲突显式保留。用户偏好可被显式修正，但身份/授权/地理事实不由自由文本覆盖。

- 输入：显式输入、系统记录、外部来源、推断
- 输出：EvidenceSet、冲突状态和可解释优先规则
- 前置：BT-V5-AIR-022, BT-V5-AIR-023
- 复用锚点：BT-V4-SAF-004
- 外部条件：无新增外部条件
- 正向验收：去年的照片仅生成历史候选；GPS注明media_metadata而非本人到访；证据互相矛盾时待确认
- 负向验收：单一全局优先级不能允许用户文字改身份/权限；模型confidence不能当校准概率
- 完成证据：冲突/时间/地理错配测试

## BT-V5-AIR-025 多城市线上线下语境和社会关系

优先级：P1　分组：C 扩展能力　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

依据 V4 IN_PERSON/ONLINE/HYBRID 和跨城市身份构建语境；只使用被允许的好友/关注/共享历史做推荐。

- 输入：城市/时间/模式、已授权Tie关系
- 输出：带约束的推荐Context
- 前置：BT-V5-AIR-023, BT-V5-AIR-024
- 复用锚点：BT-V4-ACTN-003, BT-V4-AGA-001, BT-V4-AGA-002, BT-V4-INT-003, BT-V4-INT-004, BT-V4-SOC-001
- 外部条件：无新增外部条件
- 正向验收：线上活动不被当前城市误过滤；过去留学城市不当现居；好友可见性尊重关系变化
- 负向验收：不把Aberdeen硬编码为所有用户城市；不读取好友私有记忆
- 完成证据：跨城市/线上/撤销关系用例

## BT-V5-AIR-026 可评测的检索和排序升级

优先级：P2　分组：F 后续优化　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

基于小规模命中率/延迟评测决定是否增加混合检索、embedding或reranker；保留文本基线和可撤销索引。

- 输入：评测集、现有数据库检索指标
- 输出：检索ADR和受控实现
- 前置：BT-V5-AIR-022, BT-V5-AIR-023, BT-V5-AIR-044, BT-V5-AIR-048
- 复用锚点：审计后绑定现有模块
- 外部条件：无新增外部条件
- 正向验收：向量结果仍应用主体/来源有效性过滤；删除后缓存/索引同步不可命中
- 负向验收：不得为 AIR 默认更换数据库或建立跨租户共享隐私向量缓存
- 完成证据：离线检索对照、删除传播和隔离测试

## BT-V5-AIR-027 Prompt 与 schema 版本登记

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

每个任务绑定不可变 prompt_version/input_schema/output_schema/tool_allowlist/policy版本和模型能力要求；历史 run 保存版本引用。

- 输入：任务配置、Git版本化prompt/schema
- 输出：PromptRegistry、配置发布记录
- 前置：BT-V5-AIR-003, BT-V5-AIR-007
- 复用锚点：BT-V4-AGF-001
- 外部条件：无新增外部条件
- 正向验收：缺失版本在启动/任务前失败；旧run可定位到准确配置；更换prompt仅影响允许的新run
- 负向验收：不依赖 provider 存储 prompt 作为唯一真源；不在代码散落用户无关长prompt
- 完成证据：配置验证、版本固定/回退测试

## BT-V5-AIR-028 结构化输出验证与注入隔离

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

模型输出进行 schema/长度/枚举/实体引用/业务语义验证，拒答/截断/无效输出有专用错误；外部文字和OCR均为数据，不能授予工具权力。

- 输入：ModelResult、来源标签、实体范围
- 输出：ValidatedResult或明确失败
- 前置：BT-V5-AIR-007, BT-V5-AIR-023, BT-V5-AIR-027
- 复用锚点：BT-AGT-002, BT-V4-NOW-004, BT-V4-SAF-001, BT-V4-SAF-004
- 外部条件：无新增外部条件
- 正向验收：错ID/越权工具/多余字段/截断JSON全部失败关闭；修复最多一次且计入预算
- 负向验收：不执行模型生成SQL/shell/URL；不通过prompt字符串宣称解决所有注入
- 完成证据：对抗样本、拒答/截断/字段污染测试

## BT-V5-AIR-029 Prompt 变更评审和影子回放

优先级：P1　分组：C 扩展能力　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

prompt/schema/tool配置变更需同版本评测、对照和可回退；影子模式无外部副作用、不落用户权威记忆。

- 输入：候选prompt、脱敏允许样本
- 输出：评测差异和发布决定
- 前置：BT-V5-AIR-027, BT-V5-AIR-028, BT-V5-AIR-048, BT-V5-AIR-049
- 复用锚点：审计后绑定现有模块
- 外部条件：无新增外部条件
- 正向验收：候选版本退化时保留线上版本；回放不影响用户；记录输入数据权利范围
- 负向验收：不把用户原始私聊默认收集到评测库；不得记录隐藏思维链
- 完成证据：版本比较、权限检查和影子无副作用证明

## BT-V5-AIR-030 媒体授权与本地出口前过滤

优先级：P1　分组：D 视觉闭环　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

确认上传归属、MIME/大小/解码限制、用户是否允许AI分析，剥离无关EXIF；隐私检测/遮挡/用户选择在外部视觉调用前发生。

- 输入：已选媒体、分析授权、媒体策略
- 输出：AuthorizedMediaReference或拒绝
- 前置：BT-V5-AIR-011, BT-V5-AIR-018, BT-V5-AIR-023, BT-V5-AIR-028
- 复用锚点：BT-V4-SAF-004
- 外部条件：无新增外部条件
- 正向验收：无AI同意不调用provider；敏感/无法安全判断图片交给用户选择本地处理/遮挡/不处理
- 负向验收：不把原图发第三方后再声称已做前置隐私检查；不默认识别人脸/推断敏感属性
- 完成证据：出网捕获、EXIF清除、恶意文件和授权矩阵测试

## BT-V5-AIR-031 结构化视觉观察接口

优先级：P1　分组：D 视觉闭环　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

将可用视觉模型输出变成场景/可见物体/OCR/活动候选/地点候选，附证据来源和不确定性；P0文本不依赖该功能。

- 输入：AuthorizedMediaReference、视觉能力配置
- 输出：Observation[]（非用户画像）
- 前置：BT-V5-AIR-008, BT-V5-AIR-009, BT-V5-AIR-028, BT-V5-AIR-030
- 复用锚点：BT-V4-MOM-001
- 外部条件：VISION_PROVIDER_APPROVAL
- 正向验收：模糊/无GPS/不相关图片可以返回unknown；OCR保持外部数据标签
- 负向验收：不得用照片认定敏感属性/人名/精确住址；地点猜测不自动成为到访事实
- 完成证据：允许数据集视觉测试、unknown/误判样例与schema结果

## BT-V5-AIR-032 媒体地点和时间证据验证

优先级：P1　分组：D 视觉闭环　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

对照用户说明、捕获/修改/上传时间、EXIF/GPS可信度及可用地点ID；保留冲突和来源，精确坐标按任务最小化。

- 输入：Observation、media metadata、显式说明
- 输出：Place/Time EvidenceCandidate
- 前置：BT-V5-AIR-024, BT-V5-AIR-030, BT-V5-AIR-031
- 复用锚点：BT-V4-MOM-002, BT-V4-PLC-002
- 外部条件：无新增外部条件
- 正向验收：照片经转发、截图、改EXIF或时间冲突时不认定本人到访；历史与当前上下文分离
- 负向验收：GPS不是防伪事实；低置信地标猜测不覆盖经过验证的地点ID
- 完成证据：编辑元数据/历史照片/多人相册负例集

## BT-V5-AIR-033 Moment 富集作业和用户反馈

优先级：P1　分组：D 视觉闭环　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

在原Moment保存完成后异步运行，结果以可编辑候选显示；分析失败不影响发布流程；用户可重试/关闭/取消。

- 输入：MomentCreated、分析状态
- 输出：Enrichment状态及候选摘要
- 前置：BT-V5-AIR-016, BT-V5-AIR-018, BT-V5-AIR-031, BT-V5-AIR-032, BT-V5-AIR-043, BT-V5-AIR-044, BT-V5-AIR-050
- 复用锚点：BT-V4-MOM-001
- 外部条件：无新增外部条件
- 正向验收：上传立即完成原有流程；网络失败显示待处理；重复事件不重复候选
- 负向验收：未授权私密Moment不公开推荐；失败不能写空画像替代旧值
- 完成证据：Flutter界面测试、失败恢复、私密Moment回归

## BT-V5-AIR-034 视频音频与多帧扩展

优先级：P2　分组：F 后续优化　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

在图片闭环通过后明确用户授权、成本与必要性，再采样视频关键帧/转录；限制时长、数据量、第三方范围。

- 输入：用户选择的媒体、允许的能力
- 输出：带时间定位的Observation
- 前置：BT-V5-AIR-030, BT-V5-AIR-031, BT-V5-AIR-033, BT-V5-AIR-048
- 复用锚点：BT-V4-MOM-001
- 外部条件：MEDIA_PROVIDER_APPROVAL
- 正向验收：单文件超过限制拒绝并解释；重试不重复付费处理已完成片段
- 负向验收：不能默认上传整段视频或后台持续麦克风分析
- 完成证据：资源上限、删除、转录注入测试

## BT-V5-AIR-035 富集去重与事实归属

优先级：P1　分组：D 视觉闭环　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

同组照片/同一活动形成共享source_cluster；明确照片所有者、发布者、被摄者和Agent主体并不天然相同。

- 输入：多Moment/媒体引用、观察候选
- 输出：去重EvidenceCluster
- 前置：BT-V5-AIR-024, BT-V5-AIR-031, BT-V5-AIR-043
- 复用锚点：审计后绑定现有模块
- 外部条件：无新增外部条件
- 正向验收：8张同次徒步照片计作一个相关证据簇；他人照片不写成本人经历
- 负向验收：不得按每张图片累加兴趣置信度；不能把转发当亲历
- 完成证据：重复照片/转发/同一事件聚类测试

## BT-V5-AIR-036 受限 Planner 和类型化提案

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

P0只允许小步只读检索及候选审阅流程。planner输出动作提案，schema无权限授予字段；步骤/模型调用/执行时间有硬上限。

- 输入：ValidatedIntent、ContextBundle、允许工具
- 输出：ActionProposal[]或澄清请求
- 前置：BT-V5-AIR-016, BT-V5-AIR-023, BT-V5-AIR-028
- 复用锚点：BT-V4-ACTN-001, BT-V4-INT-005
- 外部条件：无新增外部条件
- 正向验收：未知目标先澄清；超步骤终止；提案只包含允许的参数和资源ID
- 负向验收：模型requires_confirmation=false不得影响权限；不能生成可执行代码任意运行
- 完成证据：有界计划/恶意动作/歧义实体测试

## BT-V5-AIR-037 Tool Registry 和确定性许可判定

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

工具登记输入输出schema、读写类型、资源范围、风险、必要权限、幂等/对账能力。Policy Engine独立决定ALLOW/DENY/CONFIRM，不读模型自授的权限字段。

- 输入：ActionProposal、可信身份、授权和最新资源状态
- 输出：PolicyDecision、可执行单步调用
- 前置：BT-V5-AIR-018, BT-V5-AIR-023, BT-V5-AIR-028, BT-V5-AIR-036
- 复用锚点：BT-V4-PRV-001, BT-V4-SAF-004, BT-V4-AGF-002
- 外部条件：无新增外部条件
- 正向验收：读取只在ACL允许时执行；发消息/改档案等写操作默认进入批准；Organization/Business不能继承个人全部工具
- 负向验收：模型/图片/OCR/第三方消息都不能授予权限；deny优先；未知工具fail closed
- 完成证据：表驱动权限矩阵、跨主体工具拒绝、执行前撤权测试

## BT-V5-AIR-038 写工具幂等与结果对账

优先级：P1　分组：E 动作与社交扩展　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

对确认后的外部写操作绑定effect_key和请求摘要；不支持幂等的工具在超时后进入UNKNOWN_RECONCILE，先查询结果再决定重试。

- 输入：已批准ActionProposal、effect_key
- 输出：ActionResult及业务效果记录
- 前置：BT-V5-AIR-015, BT-V5-AIR-016, BT-V5-AIR-037, BT-V5-AIR-040
- 复用锚点：BT-V4-AGA-002
- 外部条件：无新增外部条件
- 正向验收：确认后响应丢失不会重复发消息/RSVP；二次执行同effect_key返回同结果
- 负向验收：不能把未知当失败立即重发；计划中后续动作不覆盖前一步副作用
- 完成证据：故障注入、远端已成功本地超时测试

## BT-V5-AIR-039 活动人物地点检索适配器

优先级：P1　分组：E 动作与社交扩展　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

将已有搜索/匹配/地图能力注册为只读工具，保持实体ID与来源，新建适配器而非复制搜索服务。

- 输入：授权查询、模式/地点/时间过滤
- 输出：来源可追溯的候选实体
- 前置：BT-V5-AIR-022, BT-V5-AIR-023, BT-V5-AIR-037
- 复用锚点：BT-V4-OPP-001, BT-V4-OPP-002, BT-V4-OPP-003, BT-V4-OPP-004, BT-V4-PLC-001, BT-V4-PLC-003
- 外部条件：无新增外部条件
- 正向验收：推荐链接指向真实存在且可见实体；空结果如实返回；报名人数/活动变更取最新源
- 负向验收：模型不能虚构活动和可报名名额；禁止绕过私群/封禁可见性
- 完成证据：现有API契约、虚构ID/空结果/不可见实体测试

## BT-V5-AIR-040 用户确认协议和安全默认

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

确认服务绑定tenant、actor、subject、agent、logical_operation_id、action_id、工具版本、目标、参数canonical digest、资源版本、policy/consent/membership版本、有效期和单次消费。P0含沙箱写工具、持久effect/dispatch账本、脱敏决策审计与UNKNOWN_OUTCOME恢复，真实外部写工具默认关闭。批准内容摘要不作为幂等键；效果键独立绑定逻辑操作。

- 输入：待批准ActionProposal、PolicyDecision、可信主体与授权版本
- 输出：ApprovalRequest/Receipt、原子dispatch commitment、effect ledger和拒绝/未知结果状态
- 前置：BT-V5-AIR-016, BT-V5-AIR-018, BT-V5-AIR-037
- 复用锚点：BT-V4-SAF-004
- 外部条件：无新增外部条件
- 正向验收：参数/目标/actor/tenant变化需新确认；两worker竞争同批准仅一个dispatch commitment；崩溃于提交/发送后恢复不重发未知动作；同一操作重复投递一次效果，而两次刻意相同新操作可产生两次效果
- 负向验收：客户端/模型字段不能当批准；approval消费不等于执行成功；lease过期不能盲重发；audit无秘密/原始私聊/隐藏思维链
- 完成证据：批准篡改/过期/竞态/崩溃/lease测试、原子撤权边界测试、最小兼容迁移与恢复证据、脱敏审计样例

## BT-V5-AIR-041 通知提案和节制触达

优先级：P1　分组：E 动作与社交扩展　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

确定性紧急/服务通知保持现有路径；语义通知需合并、频率上限、静默、类别设置及可解释理由，发送仍过Tool/Policy。

- 输入：事件、通知偏好、已许可上下文
- 输出：NotificationProposal及投递结果
- 前置：BT-V5-AIR-019, BT-V5-AIR-037, BT-V5-AIR-038, BT-V5-AIR-040, BT-V5-AIR-050
- 复用锚点：BT-V4-SAF-004, BT-V4-PIL-003
- 外部条件：无新增外部条件
- 正向验收：同活动多次更新合并；关闭类别后不再触达；服务提醒无LLM时仍可运行
- 负向验收：不因模型推断用户焦虑/健康等敏感状态主动营销；不重复轰炸
- 完成证据：频控/静默/关闭/LLM离线回归测试

## BT-V5-AIR-042 有界 Agent 间协作和角色包

优先级：P1　分组：E 动作与社交扩展　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

复用 V4 A2A，代理间只交换本次获准结构化信息，每方独立授权；限制hop/预算/有效期，组织/商家有独立资格和业务权限。 服从V4 Post-Pilot发布边界；本P1只是优先级，不能提前开放A2A/native booking。

- 输入：A2A proposal、双方委派和允许字段
- 输出：许可交换记录或拒绝
- 前置：BT-V5-AIR-020, BT-V5-AIR-023, BT-V5-AIR-037, BT-V5-AIR-038, BT-V5-AIR-040
- 复用锚点：BT-V4-AGA-001, BT-V4-AGA-002, BT-V4-AGF-002, BT-V4-BIZ-002, BT-V4-BIZ-006, BT-V4-ORG-001, BT-V4-ORG-002
- 外部条件：无新增外部条件
- 正向验收：朋友关系不自动共享私有记忆；一方撤权立即阻止后续；组织转交管理员不泄漏私人上下文
- 负向验收：不是给每个地点造Agent；对方Agent输出不能发起未批准工具；无无限代理互聊
- 完成证据：跨角色/撤权/hop上限/共享字段测试

## BT-V5-AIR-043 文字证据到 MemoryCandidate

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

P0选择用户已授权文本Moment的受限属性候选；输出 subject/predicate/value/source/version/time/evidence/uncertainty，交 AGE 验证服务。

- 输入：授权文本、ContextBundle、EvidenceRefs
- 输出：MemoryCandidate（待审阅）
- 前置：BT-V5-AIR-009, BT-V5-AIR-011, BT-V5-AIR-016, BT-V5-AIR-018, BT-V5-AIR-023, BT-V5-AIR-027, BT-V5-AIR-028
- 复用锚点：BT-V4-SAF-004
- 外部条件：AGE_WRITE_CONTRACT
- 正向验收：显式兴趣陈述能生成一个有源候选；一条观察不自动成为稳定偏好；可返回无候选
- 负向验收：不得直接更新Profile/Memory真表；敏感属性推断直接拒绝；不能凭无证据高confidence落库
- 完成证据：合成文本候选集、来源丢失/敏感推断负例

## BT-V5-AIR-044 受控候选确认与删除闭环

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

AGE权威服务做去重/敏感检查/版本校验/用户确认后写入。用户拒绝/纠正/删除立即停止检索使用，并传播到缓存、派生候选和已存在向量索引；防旧任务复活。 源编辑亦先失效旧候选/已确认派生记忆和检索缓存；P0先保证不返回旧断言，完整重算可留 AIR-047。

- 输入：MemoryCandidate、用户选择、SourceDeleted
- 输出：记忆状态、tombstone、清理记录
- 前置：BT-V5-AIR-018, BT-V5-AIR-023, BT-V5-AIR-037, BT-V5-AIR-040, BT-V5-AIR-043
- 复用锚点：BT-V4-PRV-001, BT-V4-SAF-004
- 外部条件：无新增外部条件
- 正向验收：确认后才激活P0候选；拒绝后不反复建议同来源；删除后下一次查询零命中；异步清理有可检查状态；候选确认后编辑来源，在重算前查询不再返回旧断言
- 负向验收：删除不能只藏UI；不得默认删除用户原Moment；已出网数据无法保证即时远端删除需明示实际能力
- 完成证据：确认/纠正/删源/缓存向量/晚响应重放一组E2E

## BT-V5-AIR-045 兴趣证据聚合与可校准置信度

优先级：P1　分组：C 扩展能力　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

关联事件按source_cluster去重，显式陈述、历史行为、推断分开；定义置信度含义和校准方法，支持过期/衰减/冲突/用户修正。

- 输入：多个候选、来源簇、反馈
- 输出：解释得出的偏好状态
- 前置：BT-V5-AIR-024, BT-V5-AIR-035, BT-V5-AIR-043, BT-V5-AIR-044, BT-V5-AIR-048
- 复用锚点：审计后绑定现有模块
- 外部条件：无新增外部条件
- 正向验收：同次8张照片不提升8次；用户说不再喜欢后旧兴趣不继续推荐；无校准只显示等级/原始score
- 负向验收：禁止把示例0.28+0.15当真实概率；不能仅靠次数自动产生敏感画像
- 完成证据：聚合单测、校准报告或明确未校准标签

## BT-V5-AIR-046 记忆使用授权与公开资料边界

优先级：P1　分组：C 扩展能力　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

私人记忆、公开Profile、组织知识和商家知识分离；使用与公开是独立授权，所有引用遵守目的限制。

- 输入：AGE字段权限、用户共享设置
- 输出：purpose-scoped MemoryView
- 前置：BT-V5-AIR-023, BT-V5-AIR-044
- 复用锚点：BT-V4-PRV-001, BT-V4-SAF-004
- 外部条件：无新增外部条件
- 正向验收：个人推荐可用的私密偏好不出现在公共主页/组织回复；共享前显示具体字段和受众
- 负向验收：不能以已上传Moment为由将所有推断公开；组织管理员看不到用户私有记忆
- 完成证据：公共API泄漏扫描、共享与撤销测试

## BT-V5-AIR-047 来源纠错与变更传播

优先级：P1　分组：C 扩展能力　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

源Moment/活动/人工声明修改时标记旧候选失效，再生成新版；保留必要审计但不继续使用被撤回私密内容。

- 输入：SourceUpdated、PolicyUpdated、用户纠正
- 输出：受影响记忆重算/失效事件
- 前置：BT-V5-AIR-018, BT-V5-AIR-024, BT-V5-AIR-044, BT-V5-AIR-045
- 复用锚点：审计后绑定现有模块
- 外部条件：无新增外部条件
- 正向验收：修改一次只产生一个有效版本；删除/撤回优先于重算；有依赖的推荐缓存失效
- 负向验收：不把历史声明覆盖为从未发生；无权恢复被用户删除的原内容
- 完成证据：源更新→检索变化E2E、历史引用处理测试

## BT-V5-AIR-048 最小离线评测与安全发布门槛

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

建立至少40个合成/获许可用例，覆盖文本候选、空/歧义、拒答、注入、越权、删除和限额；每例注明期望及禁止结果。 P0安全集包含原子撤权/角色移除/批准竞争、dispatch后崩溃、未知效果禁止盲重试、handler升级去重、审计脱敏、来源编辑失效、重复相同独立请求分离。

- 输入：固定版本数据集、prompt/model配置
- 输出：评测结果和失败样本
- 前置：BT-V5-AIR-028, BT-V5-AIR-037, BT-V5-AIR-040, BT-V5-AIR-043, BT-V5-AIR-044
- 复用锚点：审计后绑定现有模块
- 外部条件：无新增外部条件
- 正向验收：安全用例100%通过；结构化有效输出≥95%；文本抽取精确率目标≥90%并报告样本数；不足门槛不得发布
- 负向验收：不得仅用LLM judge判安全；不能把无执行结果当pass；阈值为本包建议需实际测量
- 完成证据：机器报告、数据许可、测试命令/环境/版本和失败样本

## BT-V5-AIR-049 运行追踪和成本告警

优先级：P1　分组：C 扩展能力　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

记录run/event/subject的受控标识、版本、工具决策、错误、费用/耗时；敏感字段脱敏、分权访问、有限保留。保存简短决策依据，不收集隐藏思维链。

- 输入：RunStep、usage、PolicyDecision
- 输出：trace、指标和告警
- 前置：BT-V5-AIR-011, BT-V5-AIR-016, BT-V5-AIR-027
- 复用锚点：BT-RUN-002, BT-V4-ANA-001, BT-V4-OBS-001
- 外部条件：无新增外部条件
- 正向验收：单次事件可定位到provider/prompt/schema/policy版本；预算报警；脱敏测试不过不能发布
- 负向验收：不记录API keys/原始相册/完整私聊/隐藏思维链；不向组织管理员显示个人trace
- 完成证据：trace样例、脱敏扫描、告警演练、保留配置

## BT-V5-AIR-050 Flutter 候选审阅和Agent控制

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

在已有界面增加待确认候选、来源说明、同意/拒绝/纠正/删除和停止AI处理入口；状态展示排队/失败/取消，权限由服务端决定。

- 输入：已有Flutter页面、MemoryCandidate/Run状态
- 输出：用户控制界面和服务端调用
- 前置：BT-V5-AIR-016, BT-V5-AIR-018, BT-V5-AIR-040, BT-V5-AIR-043, BT-V5-AIR-044
- 复用锚点：BT-PER-001
- 外部条件：无新增外部条件
- 正向验收：拒绝/删除跨设备后生效；排队时可退出；离线动作重连幂等；清楚显示AI推断
- 负向验收：不把AI候选伪装成用户自填事实；前端关闭开关不能只是本地隐藏
- 完成证据：widget/集成测试、真机或明确模拟器证据、可访问性截图

## BT-V5-AIR-051 计划审阅及执行结果界面

优先级：P1　分组：E 动作与社交扩展　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

展示工具动作目标、内容、授权范围、费用风险和有效期，允许逐步批准/拒绝/取消。状态UNKNOWN_RECONCILE明确可见。

- 输入：ApprovalRequest、计划和工具结果
- 输出：审阅确认页面、可追踪结果
- 前置：BT-V5-AIR-038, BT-V5-AIR-040, BT-V5-AIR-050
- 复用锚点：审计后绑定现有模块
- 外部条件：无新增外部条件
- 正向验收：用户看到最终收件人与内容再批准；动作变化重新确认；未批准动作保持等待
- 负向验收：不采用全选默认批准新动作；不能把建议发送说成已发送
- 完成证据：Flutter流程测试、服务端批准绑定测试

## BT-V5-AIR-052 全链路故障与质量回归

优先级：P1　分组：E 动作与社交扩展　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

扩展至视觉/双provider/写工具/A2A/跨租户/并发/数据删除至少120用例，记录版本化对照和开关回退行为。

- 输入：P1功能与评测fixtures
- 输出：全链路报告和缺陷清单
- 前置：BT-V5-AIR-012, BT-V5-AIR-017, BT-V5-AIR-020, BT-V5-AIR-033, BT-V5-AIR-038, BT-V5-AIR-041, BT-V5-AIR-042, BT-V5-AIR-045, BT-V5-AIR-046, BT-V5-AIR-047, BT-V5-AIR-049, BT-V5-AIR-051
- 复用锚点：BT-V4-E2E-001, BT-V4-E2E-002, BT-V4-E2E-003, BT-V4-E2E-004, BT-V4-TST-001
- 外部条件：无新增外部条件
- 正向验收：关键安全全部通过；改prompt/provider不破坏候选和旧产品流程；失败可复现
- 负向验收：任何跨主体泄漏/未授权副作用为零容忍阻断；mock测试不能替代live契约
- 完成证据：E2E/负载/安全报告、失败复现说明

## BT-V5-AIR-053 默认关闭和内部受控发布

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

AIR新入口默认off，按功能/主体灰度；开关分别控制模型出网、候选生成、写工具、视觉、A2A。关闭后阻止新步骤并取消危险待办，既有确定性业务可用。

- 输入：功能配置、授权名单、合成测试环境
- 输出：分功能kill switch、灰度策略
- 前置：BT-V5-AIR-011, BT-V5-AIR-016, BT-V5-AIR-018, BT-V5-AIR-037, BT-V5-AIR-044, BT-V5-AIR-048, BT-V5-AIR-050
- 复用锚点：BT-V4-PIL-003, BT-V4-REL-001
- 外部条件：无新增外部条件
- 正向验收：在执行前关停阻止副作用；关闭AIR不影响活动发布/RSVP/原提醒；无有效预算/provider配置不能开live
- 负向验收：不能一次打开所有用户全部自治；不能将撤权队列留作恢复后自动执行
- 完成证据：开关演练、回归、上线/回退清单

## BT-V5-AIR-054 交付 P0 文本闭环与真实证据

优先级：P0　分组：B 最小运行内核　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

交付两个垂直用例：授权文本Moment→候选→用户确认→记忆→删除不可检索；用户问活动→现有只读工具结果→有来源回答。明确fake/live、未启用功能和剩余外部门禁。

- 输入：所有P0证据、已有活动工具
- 输出：Completion Report、证据索引和接续队列
- 前置：BT-V5-AIR-001, BT-V5-AIR-002, BT-V5-AIR-003, BT-V5-AIR-004, BT-V5-AIR-007, BT-V5-AIR-008, BT-V5-AIR-009, BT-V5-AIR-010, BT-V5-AIR-011, BT-V5-AIR-014, BT-V5-AIR-015, BT-V5-AIR-016, BT-V5-AIR-018, BT-V5-AIR-022, BT-V5-AIR-023, BT-V5-AIR-027, BT-V5-AIR-028, BT-V5-AIR-036, BT-V5-AIR-037, BT-V5-AIR-040, BT-V5-AIR-043, BT-V5-AIR-044, BT-V5-AIR-048, BT-V5-AIR-050, BT-V5-AIR-053
- 复用锚点：BT-V4-PIL-001, BT-V4-PIL-002, BT-V4-PIL-003
- 外部条件：LIVE_CANARY_FOR_LIVE_READY
- 正向验收：每个DONE有代码位置、测试、版本和证据；先复用现有活动检索，缺失时只加最小只读adapter并对账039；最小schema持久性、effect账本和脱敏审计在P0验证，不能因AIR-005/049是P1而后置
- 负向验收：AIR_P0_PASSED不能自动改Closed Pilot Ready=YES；P1/P2未完成不能隐藏；live未验记录BLOCKED_EXTERNAL
- 完成证据：BIRDTIE-V5-AIR-COMPLETION-REPORT.md；功能状态与live状态分开

## BT-V5-AIR-055 外部配置和试点门禁复核

优先级：P1　分组：E 动作与社交扩展　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

用最新实际证据核验provider选择/费用/数据策略、真实IdP/HTTPS/真机、CSSA身份权力/确认活动/生产API地图和提醒部署/支持渠道；需要人的动作汇总一次。 门禁可按当前拟发布范围逐项复核，不等待Post-Pilot A2A或全部P1完成；AIR-052全功能报告不作首次Pilot的统一前置。

- 输入：已授权部署配置、真实试点证据
- 输出：门禁表与最小待决问题
- 前置：BT-V5-AIR-006, BT-V5-AIR-009, BT-V5-AIR-049, BT-V5-AIR-054
- 复用锚点：BT-V4-PIL-001, BT-V4-PIL-002, BT-V4-PIL-003
- 外部条件：OWNER_EXTERNAL_DECISIONS
- 正向验收：每个门禁有负责人/证据/时间/下一步；缺失标BLOCKED_EXTERNAL，其他可做工作继续
- 负向验收：创建CSSA账号不等于代表CSSA；API正常不等于通知投递成功；不新建凭证/接受条款/付费或对外发送来代替批准
- 完成证据：外部门禁表、实际演练证据，不含秘密

## BT-V5-AIR-056 专业模型和自托管研究门槛

优先级：P2　分组：F 后续优化　规划状态：TODO_AUDIT　实现状态：UNVERIFIED

只有在重复任务质量/成本数据表明需要时，研究轻模型、自托管、蒸馏或微调；先核验供应商当时可用性、数据权利、安全和TCO。

- 输入：稳定任务数据和质量成本指标
- 输出：研究ADR及可选实验建议
- 前置：BT-V5-AIR-013, BT-V5-AIR-026, BT-V5-AIR-048, BT-V5-AIR-049
- 复用锚点：审计后绑定现有模块
- 外部条件：无新增外部条件
- 正向验收：将是否值得研究与是否上线分开；个人隐私不进入未经单独授权的训练集
- 负向验收：不为每用户训练模型；不依赖OpenAI新自助微调可用；没有业务收益不购买GPU
- 完成证据：收益/风险/供应可用性评估；本轮无训练和采购
