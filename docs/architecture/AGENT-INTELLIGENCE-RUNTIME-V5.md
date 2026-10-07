<!-- 来源：D:\Project\birdtie\BirdTie-BT-V5-AIR-Codex-Package-2026-10-02.zip!/BT-V5-AIR-DESIGN.md；原文 SHA256 839a0af991b1701b61714eeb34c73c7143a8dcb8bdacde30a574965c1273cc8c。2026-10-02 选取收录，原ZIP/work来源保留。 -->

> 仓库接入说明：下文原始证据范围及“未取得AGE/未导入”指编制当时；现四份材料已读取、137源项已对账，安全检查点后追加108去重任务。实现状态只见 automation/codex_task_queue.json 与 BIRDTIE-V5-MATERIAL-RECONCILIATION-2026-10-02.md；源码接口/Debug/fake不代表完整Memory/模型/生产能力。AIR拥有推理和提案，AGE拥有权威数据；ADR及AGENTS/既有领域规范共同约束实现。原Closed Pilot门槛NO；当前不授权模型出网/正式部署/真实Agent自动写或A2A。源要求source别名和独立LIVE验证见 automation/v5_requirement_mapping.json。

# BirdTie BT V5 AIR 设计与实施规范

版本 1.0　编制日期 2026 年 10 月 2 日　适用对象 产品负责人和 Codex 执行者

## 1 结论与本轮交付

BirdTie 需要在现有 Flutter 和 Go 产品上，增量建立由自身代码控制的 Agent Intelligence Runtime。模型承担理解、抽取、推理和动作提案；BirdTie 的身份、数据、权限、预算与执行服务掌握最终控制权。本规范先定义可实现的边界、契约、运行流程和验收，再由这些设计派生 56 项需求。现阶段不需要训练 BirdTie 基础大模型，也不需要为每个用户训练一个模型。

首个 P0 版本只有两个产品闭环：

1. 用户明确允许分析一条文字 Moment → 生成有来源的记忆候选 → 用户确认或拒绝 → AGE 服务写入或拒绝 → 删除后不再被检索
2. 用户询问活动 → AIR 读取最少授权语境 → 通过已有只读检索工具找到活动 → 返回具有真实实体引用的回答

同一最小环境中使用一个沙箱写工具验证审批、撤权、幂等和未知结果恢复。P0 不开放真实自动发消息、报名、预订或 A2A 写操作。完整图片富集、第二模型供应商、社交代理协作和通知智能化按后续阶段接入。这样能够验证全部安全内核，又不会让每一种高级功能成为首版阻塞条件。

本包是设计和待审计需求，未表示已修改 D:\Project\birdtie、导入现有任务队列或开始实施。需求初始状态统一为 TODO_AUDIT，不能据此认定当前代码没有这些功能。

## 2 当前证据和可信边界

| 证据 | 已知内容 | 本包如何使用 |
| --- | --- | --- |
| 当前用户请求 | 要求结合 Codex 开发情况，先完善 AIR 设计，再给完整清单 | 本包直接回应，允许读取证据和制作需求 |
| 2026 年 10 月 1 日用户提供的完成报告 | 39 DONE、0 PARTIAL、3 TODO、3 BLOCKED；31 项 P0 中 28 DONE、3 BLOCKED；Flutter analyze、78 测试、Debug 构建安装及 Go test/vet/build 等报告通过 | 属用户报告进度，本轮未读本地实际文件；不能宣称独立复核或全部 P0 完成 |
| 可直接读取的 V4 原始方案、执行清单和 master prompt | 87 个规划任务，其中 57 P0、27 P1、3 P2；初始 84 TODO、3 BLOCKED_EXTERNAL | 用于精确复用和依赖映射；初始规划状态不是当前开发状态 |
| AGE 清单 | 前序对话报告已提供 81 项 AGE 增补方案；要求安全检查点接入且不中断现有编译开发 | 未取得完整原文和实际队列变更证据；只使用能力边界，不猜 AGE 编号 |
| 当前本地检查条件 | 用户电脑离线，未读到当前 D:\Project\birdtie 的源码、队列和最新报告 | 所有代码路径、API、表结构均为目标契约，须在阶段 A 映射到实际实现 |
| 可访问的 Civu 仓库 | 未能证明它就是当前 BirdTie 本地项目 | 不用它的提交证明 BirdTie 当前完成度 |

需要 Codex 在现有任务的安全检查点核验以下已有文件，文件不存在也应记录，不得伪造：

- docs/reports/FUNCTIONAL-MVP-COMPLETION-REPORT.md
- docs/testing/FUNCTIONAL-MVP-E2E.md
- docs/product/FUNCTIONAL-MVP-GAP-ANALYSIS.md
- docs/research/2026-10-01-organization-map-verification.md
- docs/research/2026-10-01-activity-reminder-reliability.md
- 已有 V4 GAP ANALYSIS、STATUS RECONCILIATION、COMPLETION REPORT 及实际 AGE 队列

原试点门禁仍单列：真实 IdP 与 HTTPS 回调及生产真机登录；真实组织身份权力、组织者确认活动、生产 API/地图与支持渠道；提醒调度告警和最终发布验证。AIR 完成不能自动关闭 BT-V4-PIL-001/002/003 或旧 BT-AUT-002/BT-TST-002/BT-REL-001。

## 3 必须修正的概念边界

### 3.1 Agent 身份与模型选择

agent_id 是持久领域身份，provider 和 model 是可替换执行配置。切换模型不改变用户、Agent、授权、记忆或任务归属。Person、Organization、Business 使用同一 Runtime 的角色能力包；它们的资源范围与委派链独立。Place/Venue 不是自动拥有 Agent 的业务主体。

### 3.2 权威事实与观察证据

来源按字段类型解释，不能采用一个适用于所有字段的简单全局排序。用户可以更正自己的偏好，但自由文字不能直接覆盖服务器身份、组织成员资格或权限记录。系统记录也可能过期，GPS/EXIF 可能被编辑或属于转发照片。

照片 GPS 只能支持“这份媒体带有某位置元数据”的声明，不能单独证明本人去过。模型地点判断是候选；历史照片的拍摄时间、上传时间和当前城市必须分开。出现冲突，保留来源并让用户确认。

confidence 必须标记语义为 UNCALIBRATED_SCORE、ORDINAL 或 CALIBRATED_PROBABILITY。没有校准数据时不得把 0.86 宣称为真实正确概率。8 张同一次徒步照片通常属于一个相关证据簇，不能按 8 份独立证据相加。

### 3.3 可解释性与隐私

保存工具决策码、数据来源、版本、结果、简短可公开的判断依据；不要求保存模型隐藏思维链。原始照片、OCR、私聊正文与秘密不得默认进入 trace。上传到 BirdTie、允许 AI 分析、允许模型供应商接收、允许写记忆、允许公开展示是不同的权限。

## 4 与 V4 和 AGE 的复用分工

| 模块 | V4 复用锚点 | AIR 增量责任 |
| --- | --- | --- |
| 审计与任务接入 | AUD-001/002、CAN-001、ADR-001、MIG-001、AUT-001 | 安全快照、差异审计、追加队列、AIR 证据 |
| Agent 主体 | ACT-001、AGF-001/002 | 扩展共享 Runtime，建立模型适配和运行状态；不复制主体体系 |
| 上下文与隐私 | PRV-001、CTX-001/002、SOC-001、SAF-001/002/004 | 最小字段装配、来源时间/版本、调用前出口与授权检查 |
| 结构化客户端结果 | NOW-004、旧 BT-AGT-002 | 内部提案转换为已有 ResultSet/Action Envelope，保留兼容 |
| Moments 和地点记忆 | MOM-001/002、PLC-001/002、COMM-001 | 视觉观察、候选富集、来源归属和去重 |
| 意图与供给检索 | INT-003/004/005、OPP-001/002/003/004、ACTN-001/003、PLC-003 | 受限计划调用现有引擎，不能用模型替换全部搜索和供给排序 |
| 组织商家及代理协作 | ORG-001/002、BIZ-002/006、AGA-001/002 | 角色隔离、授权交换、限额和工具许可 |
| 通知和运行观测 | NOT-001、OBS-001、ANA-001、旧 BT-NTF-001/BT-INB-001/BT-RUN-002 | 事件持久化、幂等、任务trace；用户投递语义仍复用既有规则 |
| 质量与发布 | TST-001、E2E-001..004、REL-001、PIL-001/002/003 | AIR 独立门禁与回归，保留原试点门禁 |

本表未写完整前缀的 V4 编号统一指 BT-V4-*。实际 AGE 编号必须读原文后绑定。AGE 是 Profile、Memory、Evidence、Policy、Context 的权威领域服务；AIR 只经受控接口读取或提交候选。对现有接口能够复用的字段不另建表或平行服务。

已发现的计划依赖问题须审计后调整：V4 SAF-001 的 Social Alpha P0 依赖 P1 OPP-003；SAF-004 虽为 P0 却在 Beta，并依赖 SOC-001/AGA-001；OPP-001、MOM-001 的前置任务也跨数字阶段。因此调度采用依赖图，不采用“全部 P0 完成后才能做任何 P1”的硬规则。AIR 启用隐私推断或写工具前必须有对应安全控制，不能等到后续整包 Beta 才补。

## 5 建议模块结构与运行责任

以下是逻辑模块，具体 Go 包名以审计后仓库规范为准；初期可在现有后端进程与现有任务 worker 中实现，不要求拆微服务、不要求新消息中间件。

| 模块 | 输入 | 输出 | 数据责任 |
| --- | --- | --- | --- |
| Ingress / Event Adapter | 已认证 UserQuery、领域提交事件 | EventEnvelope | 服务器解析 actor/subject/tenant |
| Job Runtime | 事件、检查点、预算 | AgentRun、RunStep | 持久状态和恢复，限制执行次数 |
| Context Builder | 主体、目的、授权快照 | ContextBundle | 读 AGE 和领域服务，不写画像 |
| Model Gateway | 规范化模型请求 | 模型结果、usage、错误 | provider 适配、能力和出口资格 |
| Prompt Registry | task kind、版本 | prompt/schema/tools 配置 | Git/现有配置服务不可变版本 |
| Perception | 已许可文本/媒体 | Observation | 可见内容与候选，非权威事实 |
| Planner | 已验证意图、工具目录 | ActionProposal | 只提议，不授予权限 |
| Policy / Approval | 提案、可信身份、最新授权 | Decision、ApprovalReceipt | 服务端确定性许可 |
| Tool Executor | 授权后的单步动作 | ActionResult / UNKNOWN_OUTCOME | 幂等、效果账本和对账 |
| Memory Evaluator | 观察、来源、候选 | 送往 AGE 的验证请求 | 不直接写权威表 |
| Evaluation / Trace | 各步结构化记录 | 回归报告、告警 | 脱敏、保留期、权限隔离 |

P0的最小持久schema、effect/dispatch账本和脱敏审计属于本期内核；必须有复用证据或兼容增量迁移与恢复测试，不能因完整迁移与观测条目标P1而推后。基础设施优先复用现有数据库和 worker。事务 outbox 可由已有关系库表实现；只有实际吞吐证据证明需要时才引入独立队列。向量检索也不属于 P0 前置条件。

## 6 标识与版本语义

- actor_id：当前请求/操作的人或受控服务身份
- subject_id：本次数据与代理所代表的主体；与 actor_id 不一定相同
- agent_id：稳定 Agent 身份，必须归属 subject 和 tenant
- tenant_id：资源隔离命名空间；个人与每个组织/商家不可混用
- logical_operation_id：用户一次意图产生的稳定操作 ID，重试、恢复、重规划沿用；两次刻意相同请求仍有不同 ID
- event_id：领域事件去重 ID；handler_version 使重新处理策略显式
- run_id / step_id：技术执行标识，不取代 logical_operation_id
- action_id：本逻辑操作中的具体动作 ID；同动作重试稳定
- action_digest：规范化目标、payload、资源版本的内容摘要，用来绑定批准；不是单独的幂等键
- effect_key：tenant + logical_operation_id + action_id + effect_kind 派生的稳定业务效果键，按外部 provider 能力映射
- consent_epoch / policy_version / membership_version / source_version：可用于执行前失效检测的权威版本

缓存、索引、outbox、approval、effect ledger 都必须带 tenant 和 subject 范围。禁止仅凭外部自然 ID 或显示名称做键。

## 7 目标接口契约

这些是拟定接口，不宣称仓库已有同名 API。优先在现有 Go 类型和路由适配，保持 Flutter 现有 response envelope。服务端拒绝未知字段、越权实体和过大 payload。

### 7.1 EventEnvelope

```json
{
  "schema_version": "air.event.v1",
  "event_id": "evt_demo_001",
  "event_type": "MomentCreated",
  "tenant_id": "tenant_demo",
  "subject_id": "person_demo",
  "actor_id": "person_demo",
  "agent_id": "agent_demo",
  "logical_operation_id": "op_demo_001",
  "occurred_at": "2026-10-02T10:00:00Z",
  "received_at": "2026-10-02T10:00:01Z",
  "source": {"type": "moment", "id": "moment_demo", "version": 1},
  "purpose": "memory_candidate",
  "consent_epoch": 3,
  "root_trace_id": "trace_demo",
  "causation_id": null,
  "payload_ref": "internal_authorized_reference"
}
```

payload_ref 为内部已授权对象引用，不是任意远程 URL。事件原始内容不能要求 runtime 修改身份或策略。Consumer 在读取引用时再次校验源版本和 ACL。

### 7.2 ContextBundle 和 ModelRequest

ContextBundle 包含 task purpose、允许字段、EvidenceRef 列表、时间范围、授权快照、token 预算与不可信内容片段。每个片段有 trusted_origin 分类，但“可信来源”不等于可执行指令。

```json
{
  "schema_version": "air.model_request.v1",
  "run_id": "run_demo",
  "task_kind": "MEMORY_CANDIDATE_EXTRACTION",
  "principal_ref": {"tenant_id": "tenant_demo", "subject_id": "person_demo", "agent_id": "agent_demo"},
  "prompt_version": "moment_text_extract.v1",
  "input_schema_version": "context.v1",
  "output_schema_version": "memory_candidate.v1",
  "context_snapshot_ref": "ctx_demo",
  "tool_allowlist": [],
  "capabilities_required": ["text", "structured_output_validatable"],
  "data_policy_ref": "policy_demo",
  "budget_ref": "budget_demo",
  "deadline_at": "2026-10-02T10:01:00Z"
}
```

内部 principal/budget/policy 引用只给 gateway 使用，不应原样发模型供应商。由 Adapter 生成必要、最小化的 provider payload。

ModelResult：status 为 COMPLETED / REFUSED / TRUNCATED / INVALID / UNAVAILABLE；包含结构化 result、tool_proposals、usage、provider_model_version、request_id、finish_reason。缺 usage 时标 UNKNOWN，不能伪记为零成本。服务端验证 schema 后还要检查业务语义。

### 7.3 Observation 和 MemoryCandidate

```json
{
  "schema_version": "air.observation.v1",
  "observation_id": "obs_demo",
  "subject_id": "person_demo",
  "source_ref": {"type": "moment", "id": "moment_demo", "version": 1},
  "kind": "activity_candidate",
  "value": "hiking",
  "assessment": {"kind": "ORDINAL", "value": "possible", "calibration_version": null},
  "provenance": "MODEL_INFERENCE",
  "observed_at": "2026-10-02T10:00:20Z",
  "event_time": null,
  "sensitivity": "ordinary",
  "subject_attribution": "UNVERIFIED"
}
```

MemoryCandidate 另含 candidate_id、predicate、value、evidence_refs、source_cluster_id、valid_from/valid_until、用户可见说明、suggested_status、task purpose。模型只能提议 CANDIDATE，不能生成有权限含义的 ACTIVE。AGE 检查来源存活、身份归属、敏感限制、重复与用户确认后决定是否写入。

### 7.4 ActionProposal 和确定性 PolicyDecision

```json
{
  "schema_version": "air.action_proposal.v1",
  "action_id": "act_demo",
  "logical_operation_id": "op_demo_002",
  "tool": "activity.search",
  "arguments": {"mode": "IN_PERSON", "city_id": "city_demo", "time_window_ref": "weekend_demo"},
  "evidence_refs": ["ctx_demo"],
  "reason_summary": "查找符合用户已确认时间和城市的可见活动"
}
```

模型输出中没有授予权限的字段。若收到 requires_confirmation 或 permission_override 等额外字段，按 schema 拒绝或明确丢弃并记录；任何情况下都不能驱动许可。

PolicyDecision 由可信服务产生：decision_id、ALLOW/DENY/CONFIRM、reason_codes、工具版本、权限/成员关系/资源版本快照、data_destinations、purpose、expires_at。许可按操作影响和用户明确授权判定，不能仅按模型 confidence 或 autonomy level 放行。

### 7.5 ApprovalRequest 和执行提交

ApprovalRequest 必须绑定：tenant、actor、subject、agent、logical_operation_id、action_id、tool+version、目标/受众、完整参数的 canonical digest、可披露的内容与后果、policy/consent/membership/resource 版本、有效期和一次性消费状态。批准后产生 receipt；receipt 不等于工具成功。

执行前将以下内容作为同一可排序的提交边界处理：新鲜权威版本检查、单次 approval 预留/消费、effect ledger 记录、dispatch intent 记录。该边界必须与撤权/成员移除在同一权威序列中排序：可用现有数据库行锁/事务/CAS，跨服务时使用 AGE 签发且可线性化校验消费的许可版本与 dispatch commit 协议。不能仅靠“检查后立刻调用”消除竞态。

撤权先于 dispatch commit：拒绝此次提交。dispatch commit 先于撤权：标为已承诺/可能已在飞，不承诺收回网络请求；对结果做对账、后续效果停止，必要的撤销或补偿也必须另受授权。lease 到期不代表可以再次发送一个未知结果的写操作。

### 7.6 错误协议

错误码至少区分 INVALID_SCHEMA、UNAUTHENTICATED、NOT_AUTHORIZED、CONSENT_REVOKED、SOURCE_STALE、SOURCE_DELETED、UNSUPPORTED_CAPABILITY、BUDGET_EXCEEDED、PROVIDER_UNAVAILABLE、MODEL_REFUSAL、MODEL_OUTPUT_INVALID、APPROVAL_REQUIRED、APPROVAL_EXPIRED、ACTION_CHANGED、UNKNOWN_OUTCOME、CANCELLED、EXPIRED。

error 对客户端给可操作说明与 retryable 字段；敏感细节仅在分权诊断中可见。retryable 由 runtime/adapter 判定，不接收模型自报。

## 8 状态机和持久存储

### 8.1 Run 生命周期

QUEUED → RUNNING → SUCCEEDED。中间允许 RUNNING → WAITING_CONFIRMATION → RUNNING，RUNNING → RETRY_WAIT → RUNNING。任意尚未提交的新步骤可变为 CANCELLED 或 EXPIRED；确定失败为 FAILED。已提交但结果不明的外部动作留在 UNKNOWN_OUTCOME，不能伪称取消成功。

每次恢复验证 lease fencing、源有效性、预算和最新权限。旧 worker 即便恢复也不得写入新检查点。run成功只代表本次定义的闭环成功，不意味着产品发布就绪。

### 8.2 候选生命周期

PROPOSED → VALIDATED → AWAITING_USER → ACCEPTED 或 REJECTED。源变更、删除或撤权可变为 INVALIDATED。确认后的权威记忆由 AGE 管理 ACTIVE/SUPERSEDED/DELETED 等状态；AIR 不创建另一套状态真源。

P0 全部长期候选都要求审阅。以后若产品批准低风险自动记忆，仍需单独的规则、可撤销范围、证据和测试，不能从当前需求默认推导。

### 8.3 Action 生命周期

PROPOSED → POLICY_CHECKED → DENIED 或 AWAITING_APPROVAL / READY → DISPATCH_COMMITTED → SUCCEEDED / FAILED / UNKNOWN_OUTCOME。取消发生在提交前为 CANCELLED。提交后只能停止后续步骤并对账，不把可能已发送的动作说成未发送。

### 8.4 存储所有权

| 记录 | 所有者 | 最少字段与约束 |
| --- | --- | --- |
| agent_run / run_step | AIR | 主体、逻辑操作、版本、状态、lease、deadline、预算与检查点 |
| domain_outbox / consumer_inbox | 现有事件设施或 AIR adapter | 原子提交、事件唯一键、消费者版本 |
| proposal / approval / effect ledger | 权限和执行服务 | 内容摘要、稳定效果键、权威版本、单次消费、对账状态 |
| Profile / Memory / Evidence / Policy | AGE 现有权威服务 | 单一写边界，版本化授权和来源 |
| prompt / schema / capability registry | AIR 配置 | 不可变版本、供应能力、验证日期 |
| observations / candidate staging | AIR 或 AGE 适配后的单一归属 | 非权威结果、来源版本、过期、删除关联 |
| trace / evaluation | 现有观测设施扩展 | 脱敏、分权、有限保留，不存隐藏思维链 |

保留期由产品/数据策略明确配置；未设置不得无期限保留原始上下文。审计最小元数据与用户内容删除分开解释。删除先同步阻止读取/新提交，再异步清理派生对象并显示可核验状态；异步清理未结束不能称物理清除全部完成。

## 9 四条具体产品流程

### 9.1 照片 Moment 富集

1. 原有上传/发布先完成，选择的可见范围保持不变
2. 用户明确选择 AI 分析；服务器确认归属和授权
3. 本地/已批准可信媒体处理层检查MIME、解码大小、敏感风险，移除不必要EXIF；不能先发外部视觉模型再做前置隐私过滤
4. 运行出网决策，确认具体 provider/region/retention 在授权内
5. 视觉模型只产出 Observation；OCR/图片中的指令仍是不可信内容
6. 地点和时间作为来源声明对照，主体归属不明时不认定亲历
7. 聚合同事件证据，生成可审阅 MemoryCandidate
8. 用户同意/拒绝/纠正；AGE 决定持久化。模型错误不改公开Profile
9. 用户删除来源/撤销后，旧作业和晚到响应不能重新生成记忆

失败显示“分析暂未完成，可重试”；原Moment不能因模型失败而消失。P0支持同流程的文字子集，完整视觉在P1启用。

### 9.2 周末活动建议

1. 验证用户身份，解析时间范围和IN_PERSON/ONLINE/HYBRID；缺必要信息才提一个澄清问题
2. 读取用户明确偏好和可用的已确认记忆，带证据和有效期
3. 当前城市取当前明确语境，不从去年相册推断现居城市
4. 用已有活动/人物/地点服务检索；ACL、封禁和群体可见性由服务端处理
5. 模型只在真实候选中组织说明；活动ID、报名状态和人数等需要最新领域数据
6. 空结果如实呈现，允许改时间/模式；不虚构活动
7. 用户快速连续询问A/B/C时，保留旧 BT-PER-001 的最终C状态；取消或忽略A/B晚结果，旧回答不能覆盖最新页面

### 9.3 分享活动给好友

1. 用户点击分享并选择具体好友/会话；名字歧义先解析
2. Planner提议send_message，工具目录标记外部写
3. Policy检查关系/会话/内容/主体范围；显示最终目标和消息预览
4. 用户批准绑定不可变动作摘要和有效期
5. 原子检查/消费批准并提交效果记录，再进行一次受控发送
6. 响应丢失：有provider幂等键则沿用同键查询/重试；没有则UNKNOWN_OUTCOME，先对账，不能盲发第二条
7. “已建议”“待确认”“发送中”“已发送”“结果待核实”必须明确区分

本场景P0只在沙箱工具验证，P1通过对应门禁后才能启用真实工具。

### 9.4 撤权和删除

1. 用户关闭AI处理或删除来源/记忆
2. AGE递增授权epoch或登记tombstone，并在可排序的权威边界生效
3. 新模型出口、未提交工具、候选激活和检索全部拒绝旧版本
4. 取消待处理事件/run；在飞模型响应不再写入
5. 源相关缓存、派生候选/媒体与现有向量索引失效和清理
6. 已在撤权前提交的外部动作显示真实状态并对账；任何补偿动作按权限处理
7. 用旧事件/备份恢复/worker重启测试，不能复活被删除的主体和内容

## 10 用户界面和体验契约

优先扩展已有Flutter页面。P0应提供：Agent设置、候选审阅、候选详情与来源、运行状态/错误提示、关闭分析和删除控制。不要新增一个脱离现有信息架构的大型独立管理App。

| 页面或状态 | 用户看到 | 可执行动作 |
| --- | --- | --- |
| 尚未允许AI分析 | 使用什么内容、用于什么任务、可能交给何种已批准服务处理 | 明确开启或保持关闭 |
| 候选列表 | AI建议、来源、历史/当前时间、未确认标签 | 同意、拒绝、查看证据 |
| 候选详情 | 原始声明/观察和推断区别、纠正影响范围 | 修改、拒绝、删除 |
| 运行中 | 排队/处理中，有限等待，离开页面不丢原操作 | 取消、返回 |
| 模型不可用 | 本次未完成分析，原Moment/已有活动功能仍可用 | 合规重试或稍后处理 |
| 权限拒绝 | 不泄漏隐藏资源的可理解原因 | 选择有权资源或联系相应管理员 |
| 待动作确认 | 明确目标、内容、范围、后果、有效期 | 批准、拒绝、编辑后重新批准 |
| 结果待核实 | 请求可能已生效，正在核验 | 查看状态；不诱导重复提交 |
| 撤权/删除 | 已停止新处理；剩余清理状态与远端实际边界 | 查看结果、修改其他设置 |

跨设备一致性以服务端为准；离线重连操作幂等。恢复已关闭的分析不得自动执行旧的未批准副作用。

## 11 模型供应与适配规则

截至本包编制时，官方资料支持以下设计依据，实施时仍应核验选定账户/区域/模型可用性：

- OpenAI 新集成使用 Responses；Assistants API 已于 2026 年 8 月 26 日关闭。迁移说明：https://developers.openai.com/api/docs/assistants/migration
- DeepSeek deepseek-flash 的当前说明列出 V4.1-Flash 及视觉、Responses、工具能力。型号表：https://api-docs.deepseek.com/quick_start/pricing/
- DeepSeek Responses 为无状态，不支持 previous_response_id/conversation/storage/background；托管工具不执行，parallel_tool_calls始终启用，developer消息按user处理。适配必须保持规范历史，并由BirdTie执行确定性工具授权与安全调度：https://api-docs.deepseek.com/guides/responses_api/
- OpenAI 自助微调目前受现有合格组织限制，新训练将于 2027 年 1 月 6 日结束。不能写成新项目默认可用的本期依赖：https://developers.openai.com/api/docs/deprecations#update-to-openais-self-serve-fine-tuning

P0只需要一个批准的provider。不能预设某品牌一定便宜/更聪明，具体模型选择依赖真实评测、能力、地域、费用和数据策略。API兼容只意味着局部形状兼容，不意味着状态、工具、角色、严格schema或存储语义相同。

替代供应商必须已在本次授权的数据出口范围中。隐私敏感数据不能因为主模型故障自动流向另一个服务。provider拒答不得当作要用别的模型绕过的错误。

## 12 预算和运行默认值

以下为开发初始建议，不是已测量的生产SLA；集中配置、评测后再调整：每run最多4次模型请求（含失败重试/输出修复），最多8次工具调用，计划深度最多2，交互run总截止60秒，单模型网络超时20秒。到达任何上限立即终止或返回部分结果，不能层层子任务重新获得预算。

付费live启用前必须有用户认可的总费用上限和可计算的单run费用上界；没有价格或usage不能把成本当0。实时交互和历史批处理分别限额，root_trace共享总预算防事件风暴。货币费用用整数最小单位或精确decimal，不能用二进制浮点累加计费。

质量门槛先用合成或获得许可的数据：P0至少40条功能及对抗用例，关键安全用例100%通过，结构化输出有效率建议≥95%，文本抽取精确率建议≥90%且报告样本数和评测口径。小样本不证明全面安全；所有安全失败均需修复，不能被平均成功率稀释。

## 13 安全验收集

- AIR-S01 并发重复事件与重启回放：一个逻辑事件不产生重复业务效果；每个动作最多一个有效dispatch commitment
- AIR-S02 两个worker同时消费批准：恰好一个提交；目标、payload、费用、actor、tenant或有效期变化均使旧批准失效
- AIR-S03 撤权竞态：用barrier测试撤权在提交前/后两种顺序；前者拒绝，后者如实在飞/对账
- AIR-S04 审批后移除组织成员：不能新增组织作用域提交，旧缓存不能恢复权限
- AIR-S05 远端成功但响应丢失：仅在支持幂等时沿用同键；不支持则UNKNOWN_OUTCOME，lease超时不导致重发
- AIR-S06 图片/OCR/文件名/检索内容/tool结果的恶意指令不能改身份、批准、策略或外传秘密
- AIR-S07 删除用户时事件、计划、已批准任务和重试并存：不复活数据，不新增提交；此前提交的动作进入对账
- AIR-S08 trace有决策码、来源和版本；无隐藏思维链、秘密、不必要原图/OCR/跨租户内容
- AIR-S09 Personal、Org A、Org B、Business 使用相同外部ID时仍隔离，切换workspace不继承别处许可
- AIR-S10 fallback不扩大provider/地域/保留范围；权限服务不可用时写操作fail closed
- AIR-S11 转发照片、改EXIF与旧时间只算来源声明；未校准score不能绕过用户同意
- AIR-S12 删除传播到已有索引/缓存，重复删除幂等，旧事件不能重建对象；审计保留与内容清理边界可解释
- AIR-S13 两次用户有意发出相同操作各有新logical_operation_id，可执行两次；同一操作重投递只能执行一次。action_digest不能替代操作ID
- AIR-S14 快速A/B/C查询最终页面保持C，A/B的晚响应不能覆盖

## 14 实施顺序与完成判据

A阶段读取安全检查点、对账、固定契约和追加队列。B阶段实施26个P0需求组成的文本/只读/沙箱审批最小闭环。C阶段加入恢复、第二provider、完整证据与观测能力；D阶段图片富集；E阶段受控真实动作和通知；已有A2A扩展仍服从V4 Post-Pilot门禁。P1只是实现优先级，不改变功能原有发布阶段，也不自动扩大首轮闭测范围。F阶段只根据实际收益推进后续优化。

阶段是沟通分组，执行顺序以依赖图为准。已完成的V4/AGE模块在审计后映射为REUSE，并提供现有测试证据；不要为了新编号重写。若P0最小活动只读adapter已存在，直接复用；AIR-039后续扩展人物/地点，不阻塞P0基线。

每项DONE至少包含：当前提交/工作区标识、代码或配置位置、正向验收、负向用例、具体测试命令与结果、依赖验证、证据位置、已知限制。纯设计、mock、单元测试、合成live canary、真实外部演练分别标记，不能混用。

功能实现完成与live验证就绪分开：P0_CODE_COMPLETE、P0_LOCAL_TEST_PASSED、P0_LIVE_CANARY_PASSED、VISION_ENABLED、WRITES_ENABLED、PILOT_READY都是不同结论。无凭证可先做代码和合成测试；不能把付费/真实provider调用视为已授权。原PIL门禁未通过时PILOT_READY保持NO或UNKNOWN。

## 15 不中断现有开发的队列合并

1. 识别现有执行者与进行中的工作项；无法验证时等待安全检查点，不争抢文件
2. 只读快照并保存当前任务状态、锁、证据；不运行abort/reset/revert/stash/discard/clean
3. 阅读实际队列工具帮助与schema；本包不给未经验证的taskctl命令
4. 对每项AIR选择REUSE/EXTEND/NEW/NOT_APPLICABLE；连接旧需求与代码证据
5. dry-run追加，不覆盖旧任务状态/估算/负责人，不插入到当前正在运行的工作项中途
6. 在原执行者确认的检查点一次性合入未冲突需求，重复导入无重复ID
7. 有外部阻塞仅阻塞对应依赖路径，继续其它已就绪工作
8. 遇到影响产品决策/权限/付费/外部承诺的问题，汇总最小问题给负责人；不能自行创建凭证、扩大访问或向真实用户试发

输出建议文档名称为 BIRDTIE-V5-AIR-GAP-ANALYSIS.md、BIRDTIE-V5-AIR-STATUS-RECONCILIATION.md、BIRDTIE-V5-AIR-COMPLETION-REPORT.md。目录沿用现有仓库惯例，不能把建议路径说成已经存在。

## 16 本包文件与阅读顺序

先读本设计规范，再读 BT-V5-AIR-REQUIREMENTS.md 的56项详细条目。CSV提供表格导入格式，JSON保留数组依赖和机器可读字段；两者内容一致。BT-V5-AIR-CODEX-MASTER-PROMPT.md可作为执行入口。导入前须完成安全检查点和实际队列适配。本包不包含真实用户数据、凭证或未经审计的代码补丁。


## 2026-10-06 AIR036 原生受限规划增量

本段接入原需求036，不新增需求体系或037执行器。闭集 `air.readonly_plan.v1` 返回 `PROPOSED` / `CLARIFICATION` / `UNAVAILABLE`、有界提案/中文问题、实际 Task/logical operation、短期限与 `model_access=UNAVAILABLE`。提案 `air.action_proposal.v1` 只有稳定 action ID、logical operation ID、闭集工具/对应参数、真实资源 ID、不透明来源版本及固定中文说明。`confirmed`、`requires_confirmation`、权限/代码/执行字段、别名、重复键和未知参数均不成为许可。

输入映射使用原实际本人 Task 的 intent/query/filters/状态、原 Session/Agent/metadata 与 AIR028 的当前领域结果；它不扩大 ContextBundle 出口。私有 `PreparedGoal` 绑定实际 Store/access/session/agent/task/source/xmin、逻辑操作及原相对时钟限制；JSON、其他 Store、其他 Session、变化后的 Task 或恢复 Control 无法复用。逻辑操作复用原 metadata-only UserQuery 的 ID，技术 ModelRun/RootTrace 不是效果或批准。

活动规划实际走 `LocalPlannerRunner` → 原 `LocalModelRunRunner.runAt` → 原 native `Create/Reserve/Begin/Settle/Release`，与 `RunValidated` 同一个重试循环、原 budget/ModelRun/ticket。P0最多3提案（一次当前活动检索、最多两条当前公开 Activity 详情）、2模型调用、30秒；所有错误/修复仍计入同一原四预算。超限拒绝，不截取输出冒充完整计划。未知/歧义目标先用普通当前身份读取产生1–2问，0模型调用或预算。

原 provider input 仍 query-only、工具列表为空；模型只建议当前公开集合中的 ID，原生来源/版本验证后才包装为提案。模型回答里的 system/SQL/shell/approved 文本不复制到提案说明。原最终事务与 FINISHED 后同身份、同 ticket 的原生回读共同检查来源/Run/fence/Session/期限；隐藏、ABA、撤权、迟到或 OFF→ON 均丢弃。未知 dispatch/Settle 或新进程恢复不重新发送，不从历史 Control 重建结果。

候选审阅通过原 `MemoryCandidateService` 的当前本人身份、metadata、候选版本/状态、source ACL/fingerprint 与 Memory gate；只输出原候选 ID/版本的 review 提案，不调用 List/Read 的过期刷新，不接受/拒绝、不创建 Memory/effect ledger，也不把候选/来源送给模型。未选候选给中文澄清；默认关闭保持不可用。

适用 UX-CHECK-06/07/08/09/10/11/16。本轮原生合成身份、真实事务/领域记录、adapter 合成输出与纯 schema 测试分别记录在 `work/bounded-planner-2026-10-06` 和 `docs/testing/evidence/bounded-planner-2026-10-06`。本机相对 elapsed/原 ClockBound 只限制时间，不能取代数据库原最终 SQL 当前性；线性化点后瞬时撤权不能追回已交付内容。没有新增097 DDL、main/HTTP/provider/UI 接线、网络许可或执行器；正式 IdP/provider、真机规划 UI/真人/生产/试点未运行，原发布门槛保持。


## 2026-10-06 AIR037：原生 Tool Registry 与许可接缝

闭集注册 `activity.search` / `activity.detail`（只读当前获准公开活动）和 `sandbox.write`（只准备本人沙箱版本的确认信息）。真实消息、资料、报名、代码、任意网络工具均未注册。注册描述的必要权限是约束，不是获准事实；输出 `ALLOW` / `DENY` / `CONFIRM` 必须来自确定性的原生来源，模型确认标志、图像/OCR、第三方文字、041 离线 Allowed 或 044 等级都不能授予权限。

成功原 `LocalPlannerRunner` 持有私有 `nativeModelRunHandle`；其 `ToolPlan()` 才能检查原完整提案，再给一次性的进程内 Read 句柄。JSON 展示、原 Control/恢复记录、未知结果不重建该句柄。工具只返回当前投影 `PublicCommercialRefs` 对应的原 `foundation.Activity`，不泄露邀请可见活动；同标题使用真实不同 ID，真实空数组与拒绝区分。最大3读取/原30秒，重复工厂获取不重置预算，读取不重发模型。

原账户/Session/Agent、Task/source/xmin、ModelRun fence/租期、原结果范围/主办方/公开 ACL/拉黑及 OFF→ON 约束保持。复用065设置的同事务值、native_revision、xmin 与缺省行缺席，策略变更或相同内容 ABA 禁止旧许可；短事务共享表锁冻结缺席与配置写入，原 elapsed deadline 限制等待，最终 payload SQL 同时校验策略指纹与数据库期限。该表锁会暂时阻塞设置写入，未声称高并发容量验收。提交线性化点后的变化不能追回已交付内容，后续读取重新检查。

沙箱只返回 `CONFIRM` 及 actor/agent/subject、具体动作/logical operation、参数摘要、用途、本地目的地、资源与策略版本、期限；不写业务、记忆、效果、批准或沙箱值。AIR040 保留持久批准与执行/对账职责。原 provider query-only、工具 allowlist 空、主 HTTP 未接入、正式 provider/自动写/Vision/A2A 仍 OFF。本地代码核验和原生产/真机/发布门槛分开。


## 2026-10-06 AIR040：本人沙箱批准、单次提交与结果核查

原 AIR037 `CheckOwnSandboxTool` 继续只生成 `CONFIRM`，不是批准或执行。新 `agentaction.Service` 通过原 `postgres.Store` 的 `PreviewOwnSandboxAction` → 本人显式 `ApproveOwnSandboxAction` → `CommitOwnSandboxAction` 接入真实原生领域。仅闭集 `sandbox.write.v1` 写入隔离的 `agent_sandbox_writes`，不调用消息、报名、资料、预订、承诺或外部服务。主 HTTP、模型工具入口、provider、客户端未接入；默认 Controller OFF 保持。普通人类 API、原候选/预算/Run/旧037读取边界不改。

097 仅新增三个最小表：不可变审阅绑定、唯一 dispatch/effect 状态账本、真实私有沙箱数据。独立 `OWN_SANDBOX_ACTION` / `sandbox_write` 用途复用原 `consent_grants`；它仍是唯一批准生命周期，不借用模型出口、Task context 或记忆授权。原 064/082/091 `MEMORY_CANDIDATE` 效果账本及守卫不扩权。

绑定包含实际 tenant/actor/subject/agent/Session/Task、logical operation/action、工具版本、目标、完整参数摘要、源 token+xmin、原身份/065策略指纹、独立 consent 版本和期限。PERSON 本人范围的 membership 为明确不适用，不能推导组织权限。批准期限不延长原生目标的30秒上限；过期需新审阅与明确确认。Session/身份/Task/policy/approval/grant 原行锁、原策略 absence 锁和最终数据库时钟将批准消费、dispatch commit 与撤权排序。参数、目标或主体变化不能复用批准。

批准消费只说明 `DISPATCH_COMMITTED`，不表示成功。只有新提交且事务确认返回的非 JSON 进程内 Commitment 能一次 Begin；Claim 绑定同实际 Store/Access/Controller ticket 和持久 fence。恢复进程不能从 JSON 重新构造发送能力。实际效果先在单独沙箱事务写入，最终真实时钟/fence/lease重新检查；收到响应不明或实际写入后的进程退出不会生成第二个发送能力。

`MarkOwnSandboxUnknown` 明确持久 `UNKNOWN_OUTCOME`。`ReconcileOwnSandboxDispatch` 在锁住原 dispatch 后读取真实沙箱效果：有实际 row 才为 `SUCCEEDED`；只有同一原生封闭沙箱内、排他锁与递增 fence 永久封闭旧 Claim 且不存在实际 row 时，才能确定 `NO_EFFECT`。不存在 ID、读取失败、404、批准已消费和 lease 到期都不等于成功或失败；不支持外部工具以 absence 推导结果。已提交动作先于撤权时仍可能在飞，后续只核查实际结果，不谎称撤回。源编辑/撤权后原已发生回执不重写；禁止追加步骤或自动重发。同一 logical operation/action/effect kind 仅一真实 row；两次刻意相同新操作保留各自效果。内容摘要/handler版本不作效果键。

审计复用原 `audit_events` 与 request correlation，只记录实际 actor、闭集决策/状态、用途和原生 ID；不存沙箱正文、Session token/digest、权限指纹、私聊或隐藏思维链。使用后的097 down拒绝丢弃历史；空库 down/reapply 与原001–096数据/xmin/catalog保持分别验证。

适用 UX-CHECK-06/07/08/09/10/11/16。实际原生数据库正负/并发/撤权双序、最后时钟、真实子进程崩溃/新进程恢复、未知提交、SQL篡改和旧回归证据在 `work/outbound-action-safety-2026-10-06` 与 `docs/testing/evidence/outbound-action-safety-2026-10-06`。纯 canonical/schema 测试、原生合成身份/隔离库实测和根代理全量验收分别记录。未运行 Flutter/沙箱UI/真机/辅助技术、正式 IdP/provider、真实用户或生产发布；Closed Pilot / Consumer Beta 仍 NO。此增量不取代原任何发布门槛。


AIR040最终边界补验：Approve/Commit在比较私人摘要或参数前，先通过原真实Session/Task/owner检查；peer、匿名、失效Session和未知审批ID统一拒绝，避免透露猜测是否命中私人内容。当前本人编辑具体版本仍返回ACTION_CHANGED并要求新确认。初次原生错误分类RED在隔离副本记录，原freeze11与后续修复版分开封存。


## 2026-10-06 AIR019 本人定时汇总（相关单元阶段）

新增本人 GET/PUT `/v1/me/notification-schedule` 和独立 `cmd/notification-schedules`，复用原notification决定、当前来源/偏好和typed Inbox，不改原001–098、活动提醒main/CLI或默认模型关闭。用户明确IANA时区、当地时间、DST缺口SKIP/重复EARLIER_ONCE、静默窗口、类别和滚动24小时实际Inbox触达上限；无配置不建计划，off/会话失效不投递，跨版本/时区改动不清零receipt预算。槽只服务器元数据，不能作为UserQuery、推理或工具批准。

新099只有设置、日槽与交付关联；源当前检查、Session、版本/xmin、最后PG时钟及未知提交去重分别保留。CLI先独立调用原活动提醒，汇总依赖nil/typednil/失败不停止提醒；错误阶段数量UNKNOWN。正常/立即分支仍只有原061谓词，DIGEST只确有原生交付关联才可见。具体契约见 [本人通知计划](AGENT-NOTIFICATION-SCHEDULES-V5.md)。

本阶段只相关单元和静态SQL契约，不能替代原生事实。099迁移/真实PG锁与并发/持久化/重启/原生HTTP、客户端设置/可用性/真机、部署推送/真实IdP和试点均NOT_RUN，建议PARTIAL，Closed Pilot/Consumer Beta仍NO。适用UX-CHECK-05/06/07/08/09/10/11/12/16仅后端范围，原历史验收不覆盖新099。精确证据在 `docs/testing/evidence/notification-schedules-2026-10-06`，状态由根代理更新唯一live队列。


## 2026-10-06 AIR024 字段证据增量

复用原 native ContextBuilder、当前来源与最后权限/时间边界；本人审阅和已批准任务上下文的字段证据、冲突与字节预算见 [字段证据规范](AGENT-FIELD-EVIDENCE-V5.md)。关联/预算剔除字段必须同步移除证据 ID 与内容；保留可见一侧的待确认状态不能隐含已解决冲突。自由文字、模型评分或媒体 GPS 不成为身份、权限、出席、到访或当前城市真源。旧公开 rules 查询保留原范围/响应上限；真实照片提取与获准分析未接入，纯契约单位不作 native 或产品完成证据。模型/vision/自动写仍关闭。


## 2026-10-06 AIR039 当前人类只读检索与匹配（相关单元阶段）

原 Task POST/GET 的地点/人物查询实际接入 CurrentSearch，原本人 New People candidates GET 接入独立 CurrentMatch，均复用原领域 resolver、真实 Session/source/字段 ACL、065 策略/xmin/最后时钟与输出前重验；详见 [只读适配器契约](CURRENT-READONLY-TOOLS-V5.md)。普通活动 HTTP 保留原可见性，包括获准邀请活动；原模型 activity.search/detail 仍仅 PUBLIC，不扩大 ACTIVITY schema、query-only 出网、041目的权限或044等级。新注册描述与 Decision 不表示已批准、邀请或消息权限。

真实空候选保留原非 nil 数组/ID；未知、撤权、ABA、迟到不退回旧读或伪装空成功。新 human policy helper 复用原 PutOwnPolicy 的 advisory 顺序后再冻结 settings；本轮只核验调用顺序，原生 PG 锁/并发与不同 owner 性能未验证，也不宣称旧037/040锁路径已整改。原 action deadline 与当前读取一起收紧。

原 GET 2项实际 handler spy RED、mapper deadline RED及最终90新单位/238旧相关单位证据保留；纯契约/注册HTTP/Tx row spies不是数据库或真实匹配证据。PG/原生HTTP/真实供给/消费UI/真机/辅助技术/整仓/vet/build/正式模型与试点 NOT_RUN，建议 PARTIAL，Closed Pilot/Consumer Beta仍NO。索引在 docs/testing/evidence/current-readonly-tools-2026-10-06；适用 UX-CHECK-06/07/08/09/10/11/16仅本轮后端范围，原门槛不替代。

## AIR020 增量：Memory 来源有限两阶段失效（2026-10-07）

这是原 outbox 的受限本地维护消费，不扩展公开 13 类型事件 ingress，也不开放 Memory 写入、反思模型、provider、A2A、HTTP 或外部出口。真实已提交 PERSON/EXPLICIT Memory 版本或删除由 101 的原事务 AFTER hook 产生 metadata-only `MEMORY_UPDATED`；父完成的同一事务创建固定深度 1 的 `MEMORY_CONTEXT_INVALIDATION`，绑定相同 owner/agent/source revision/fingerprint/时间/root 及父 event ID/实际 inbox fence/attempt。schema 构造与模型输出都不能赋权。

消费职责复用 076：删除未绑定旧来源 preview，再撤销 sole `consent_grants` 中精确 stale `TASK_CONTEXT_READ` 许可。保留 bound/consumed 历史，未自动创建或更正记忆。每根跨父/子/重试共享 6 次 claim，最多两个固定逻辑事件，原 control 全局 4/本人 1/256 heads；并非 090 Moment recovery 或 062 模型费用批准。100 grant/128 preview 分批，未清完为 PENDING；额度/期限终止保留 outbox 未完成状态，不能宣称全清完。

实际 native 消费依次 metadata UPDATE、owner 76033、root 76034、Memory SHARE、outbox row/fence、preview/grant，提名不早锁 outbox；原 Moment 路径不换锁序。当前 fence 根据实际已锁定 outbox 行及最后 CAS；`CheckFence(claim,claim)` 仅 TTL/形状，不是独立重读权威。最后 source/native clock/lease 检查拒绝迟到；未知提交不返回新成功回执，runner STOP，之后按原事件/持久状态读取或 fenced retry，不造新事件身份或召回已消费内容。

本轮只有直接 unit：原 CLI/runner、真实 pgx.Tx 操作的 spy、closed root/receipt 及 UNIT_STATIC 迁移检查。PG/migration/多进程/真实 clock/崩溃与重启/生产/全量/构建 NOT_RUN。没有全面 storm 压测或通用 13-event 因果完成证据；AIR020 PARTIAL，Pilot/Beta NO。详见 `docs/testing/evidence/memory-causal-invalidation-2026-10-07/`。


## 2026-10-07：PreferenceUpdated 的有限元数据失效链（AIR020）

- 102 仅捕获原本人私人偏好 writer 的 native 变更元数据。已设置来源使用 `written_profile_version` 与私人行 `xmin`；清空使用已递增的 `agent_profiles.profile_version`、该行 `xmin` 和私人行实际不存在。source ID 为原 personal Agent ID。事件不存字段正文，不推断 RSVP、到访或概率。
- `preference-invalidation-v1` 由原 `agent-outbox-control` 和 Store 的原 claim/consume 入口消费：根阶段有界删除最多 128 个未绑定的旧 `PURPOSE_PRIVATE_PROFILE` 预览；同事务完成回执只能派生唯一 depth=1 子事件，子阶段对原 `consent_grants` 中本人 `TASK_CONTEXT_READ/read/self` 许可做最多 100 个版本 CAS 撤销。不删除绑定、已消费或已发出历史，不声称召回内容，不新增批准权源。
- 复用原 control 的 global=4、owner=1、256 tenant-head 上限。仅先提名，原 native metadata → owner 76033 → root 76034 → 当前私人来源 → outbox 锁序；最后复验原来源、held row/fence CAS、原生 clock 和期限。`CheckFence(c,c)` 是形状/TTL 检查，本身不等于 native fence 回读。
- 同根/子/重试总 attempt≤6，depth≤1，15 分钟源 TTL、30 秒以内 lease；未完成残余保持 PENDING，预算耗尽保留 DEAD_LETTER，revision 耗尽许可仍出现在 more 检查，不能假称全部清完。未知 commit 不交出新 claim/receipt，runner 停止，不自动重发。
- 固定 5 秒内只合并同 owner/Agent/source 的较低版本 configured 根事件，且必须 PENDING、attempt/fence=0、无 lease、无任何 inbox；旧事件仅置 INVALIDATED、保留原身份/来源/root/times。clear、child、已领取与已有回执不合并。此进度不是批准或授权。
- 本切片验证只含直接 Go 单元与 `UNIT_STATIC` 102 合同检查，Tx spy 调用真实 Store helper但不执行 SQL。102 up/down、PG 锁序/并发/隔离/native 时间/真实提交、生产与手机均 NOT_RUN。down 源码拒绝已有 Preference 事件/进度/回执。AIR020 总体仍 PARTIAL，13 事件通用认知、自反思/模型与外部消费者未完成；原 Moment 082/091、Memory 101 的已封存分支/证据不被本切片替代。
