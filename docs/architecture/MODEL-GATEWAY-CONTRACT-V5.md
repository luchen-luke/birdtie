# Unified Model Gateway Contract — V5

2026-10-02；BT-V5-AIR-007 的唯一 Go 网关契约规范。范围为 **CODE_AND_LOCAL_VERIFICATION**。它补充 [AIR 运行架构 §7.2](AGENT-INTELLIGENCE-RUNTIME-V5.md)，复用 [认知边界](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md)；不替代能力路由、来源授权、预算、模型出口、领域执行或发布门槛。

来源：`work/v5-materials/BT-V5-AIR-REQUIREMENTS.md` 的 BT-V5-AIR-007、`work/v5-materials/BT-V5-AIR-BACKLOG.json`；增量导入已在原映射/队列登记，本文件没有新建 backlog。扫描 `docs/` 后未发现已有 Model Gateway canonical，因此新建本文件。

## 1 当前实际能力

实现：`apps/api/internal/modelgateway`。统一 Go 接口为 `ModelGateway.Complete(ctx, Request) (Result, error)`；业务不需要选择 provider、SDK 或模型 ID。

| 入口 | 当前行为 | 能否用于真实推理 |
| --- | --- | --- |
| `NewGateway(LiveGate)` | 校验请求；默认开关缺失/关闭返回 `UNAVAILABLE`。即使开关返回 true，也因缺当前来源/用途、出口、预算及批准 provider，返回 `UNAVAILABLE` | 否；没有 provider 字段、注册或调用路径 |
| `NewOfflineHarness(ProviderAdapter)` | 显式本地合成契约适配器；只有 `OFFLINE_CONTRACT` descriptor 可构造；所有结果携该标记 | 否；没有 HTTP/main/业务调用点、SDK、网络 adapter 或 live fallback |

`LiveGate.InferenceEnabled(ctx)` 是可信服务端开关刹车的接缝，可以后续适配 AGE066；本轮没有改 AGE066 或把其开关票据当作来源、同意、角色、出口或预算授权。`OfflineHarness` 只给开发者测试合成输入，descriptor 的自称不是出网批准。正式 provider 激活仍由后续 AIR008/009/011 等实际接口与审批约束；不能将本包的 interface、fake flag 或 fake descriptor 接成 production adapter。

没有新 migration、DB 写、HTTP route、活动/消息/Memory hook、工具执行器、外部调用或模型请求日志。默认产品行为和旧直接领域路径不变；不将测试合成输出展示给学生或称为真实 Agent 回答。

## 2 Request：闭集内部消息

`air.model_request.v1` 要求以下精确字段：

| 字段 | 限制与语义 |
| --- | --- |
| `schema_version`, `run_id` | 固定版本；非零规范小写 UUID 的当前请求标识 |
| `agent_ref` | 复用 native `agentcognitive.AgentReference`；其现有 Go JSON 名为 `AgentID` / `Principal` / `Role`，Principal 为 `type` / `id`；PERSON/PersonalAgent 或 ORGANIZATION/OrganizationAgent，组织 principal ID 是账户 ID，并非组织 actor ID |
| `task_kind` | 当前只接 `ACTIVITY_QUERY`、`MEMORY_CANDIDATE_EXTRACTION` |
| `prompt_version` | 有界固定机器版本标识；不接远程 URL |
| `input_schema_version` | `air.messages.v1` |
| `output_schema_version` | `air.answer.v1` 或 `air.candidate_proposal.v1`，与任务匹配 |
| `context_snapshot_ref`, `data_policy_ref`, `budget_ref` | 内部非零 UUID 选择器；没有 dereference/resolver，不能以格式有效证明存在、当前可读、已授权或资金获批 |
| `budget.max_output_tokens` | 1–4096 的本次输出上限；不是账单/费用批准 |
| `messages` | 1–16 段 `role`/`content`；role 为 system/user/context，system 仅首段；单段 ≤4096 bytes，总正文 ≤16 KiB，UTF-8、不为空、无 NUL |
| `output_mode` | TEXT / STRUCTURED / TOOL_PROPOSALS |
| `tool_allowlist` | 唯一闭集 activity.search/activity.detail，最多4项；tools 模式非空，候选提取为空 |
| `capabilities_required` | TEXT 需 text；STRUCTURED 加 structured_output_validatable；TOOL_PROPOSALS 再加 tools。精确集合，不接 vision/未知/重复值；是任务需求，不是 provider 已验证能力记录 |
| `deadline_at` | 必须在当前时刻后、2分钟内；adapter 子 context 使用真实绝对截止时间 |

内部 typed Agent 引用保留已存在的稳定 Agent/账户主体命名空间；没有凭示例创建 tenant ID、模型 Agent ID、组织 actor/principal 等价关系。结构验证不查 live Agent、session、组织成员权限或源存活，因此默认 Gateway 没有放行支路。

`DecodeRequest(data, now)` 与 `Request.UnmarshalJSON` 均拒绝未知字段、重复键（包括 Unicode 转义等价键）、大小写别名、坏嵌套对象、trailing JSON、无效 UTF-8、null 必需字段及超过32 KiB 的输入。typed Request 也检查实际编码后的32 KiB上限，防止 JSON 控制字符转义放大。没有当前 ContextAssembler，所以本轮只有显式合成消息测试；不会默认读取历史会话、私密 Moment、关系或 Profile。

## 3 ProviderAdapter：转换边界

`ProviderAdapter` 只有 `Descriptor()` 和 `Complete(ctx, ProviderRequest) ([]byte, error)`。`ProviderRequest` 仅携任务、prompt版本、规范消息、输出模式/schema、允许工具、输出上限与截止时间；复制消息/工具 slices，避免 adapter 修改原请求。

内部 run/Agent/principal/context/policy/budget refs 不进入 provider DTO。DTO 剥除的是原生字段，不是自动清洗正文：任何真实正文进入正式 adapter 前仍必须经过获准的 ContextAssembler/source-purpose/egress 检查；当前默认网关不提供此能力。

Provider/model/version 标识有界，不能为 UUID 或 URL。模型切换只改变结果的 `provider_model_version`，不改变原 Request 的 native AgentReference、run ID 或账号归属。adapter 不具备来源读取、工具执行、Memory/策略写入、权限决定或预算批准接口。

## 4 Result、schema 和错误

`air.model_result.v1` 保留原 run/Agent 引用和 execution_mode；身份永远取自规范请求，不信任 provider 返回的身份字段。所有 provider 响应均有界32 KiB，精确 required `status` / `request_id` / `finish_reason`，只允许 text/structured/tool_proposals/usage 的对应变体；未知权限、确认、trace、模型身份字段和不匹配变体直接 `INVALID`，不释放部分正文/提案或 provider request ID。

| 状态/输出 | 本地 contract 行为 |
| --- | --- |
| COMPLETED/TEXT | 有界非空文本；finish_reason=stop |
| COMPLETED/STRUCTURED Activity | answer + 唯一 ACTIVITY UUID entity_refs；只是模型建议，真实实体存在、版本、可见性和详情由原领域核验 |
| COMPLETED/STRUCTURED Candidate | 固定 CANDIDATE、ACTIVITY_CATEGORY、UNVERIFIED，1–8唯一 source_ref UUID；value 复用 SAF004 的 badminton/basketball/football/sports/culture 闭集；不接 ACTIVE、敏感/未知个人特征或伪确认。它不是原生 MemoryCandidate/Evidence 写入或来源授权 |
| COMPLETED/TOOL_PROPOSALS | 最多4条当前 allowlist 内的 activity.search/query（可选city_id）或 activity.detail/activity_id；参数和 reason_summary 也闭集/有界。仅提案，没有工具调用或成功回执 |
| REFUSED | finish_reason=refusal，不允许正文、结构或提案 |
| TRUNCATED | finish_reason=length；丢弃部分文本，拒绝部分结构/工具提案，不能误执行 |
| UNAVAILABLE | finish_reason=unavailable，不允许内容；默认生产入口没有实际 provider 结果 |
| INVALID | schema/边界错误；固定原因、没有 provider 部分 payload |

缺 usage 或显式 UNKNOWN 时，token 数为 nil/省略，不伪记零；只有显式 KNOWN 且完整非负整数计数可接受，输入 ≤1,000,000、输出不超过本次上限。费用始终 UNKNOWN，没有价格/账单事实。ProviderError 只留固定白名单代码与规范 retryable；未知异常、原始 provider 消息、密钥和私密上下文不进入 Error()/Result。不会在本包自动重试或收费。

2026-10-03 AIR010增量：保留ProviderError原两字段及旧规范化分类，仅由安全wrapper附带合法RATE_LIMIT/TEMPORARY的Retry-After时长。`ParseRetryAfter`只解析单个有界delta-seconds/HTTP-date，不保存raw header；`RetryAfter`明确区分缺失与present-but-invalid，后者应停止而非忽略后提前重试。终态、未知或冲突代码不可获得有效hint。24小时是解析上限，实际等待仍受调用策略与原截止约束，不能截短provider下限。OfflineHarness只保留经过规范化的安全hint；Gateway仍无provider入口，默认DISABLED/UNAVAILABLE。实际本地有界调度见唯一[模型重试规范](MODEL-RESILIENCE-POLICY-V5.md)，不以合成grant称真实出口/计费或Run恢复完成。

父 context 取消、真实子 context 截止、响应迟到或 descriptor 在执行中变更均丢弃响应内容。默认 Gateway 始终无 live 调用，尚不存在真实请求账号切换/撤权后的 provider pipeline；本地并发测试只核验每次合成 Request 的稳定主体/运行 ID，不冒充真实授权验收。

## 5 验收与后续接缝

任务专属证据由 `work/v5-air007/README.md` 与 root 的统一证据归档记录：text、structured、tool proposals、拒答、截断、错误，以及严格字段/工具/预算、deadline、模型切换、默认关闭、敏感候选拒绝、缺usage和并发隔离。实际证据完成后按 root 队列更新；本文件出现不等于 DONE。

依赖扫描必须区分已有 OIDC IdP、地图/地点 provider 与推理SDK；本包没有推理SDK或直接网络 import/调用，业务模块不能绕过网关调用推理SDK。复用 native AgentReference 后，经 AGE066 的 `agentcognitive/feature_gates.go` HTTP 状态映射继承已有 net/http 传递依赖；扫描明确保留该边，不能将“无直接网络 import”表述为传递依赖里没有网络包。本轮未改变 Flutter，Flutter/真机/界面截屏不适用；Go race 如工具链不可用须记录，不能记 PASS。

适用 UX-CHECK-06/07/08/09/10/11/16：未知保留未知、建议与真实事实分开、提案不执行、当前身份/批准不由模型决定、迟到丢弃、模型不可用明确表达。上线前仍须实际完成源/用途/角色授权、ContextAssembler、已验证 capability 路由、approved provider、出口/预算与确定性领域执行；Closed Pilot/Consumer Beta 保持原门槛，本地 contract PASS 不构成 LIVE 或试点证据。

## 2026-10-06 AIR028：输出验证与注入隔离

原 parser 的闭集、32 KiB、UTF-8、嵌套重复键、长度、枚举与工具参数检查继续有效。解析失败仍可用 `errors.Is(ErrAdapter)` 检查，并增带固定 `ErrOutputSchema`；仅确实收到、非空、有界、有效 UTF-8 的不完整/不合法 JSON 可产生私有可修复格式分类。空响应、超限、坏 UTF-8、有效 JSON 中的未知权限/工具字段、错误实体和用途不获修复许可。拒答、截断各有固定专用错误；截断正文不会释放。

`ValidateEntityScope` 是纯匹配函数，不是权威来源。实际原生 `ModelRequestRun` 在原事务里，从原 Task 的明确城市、查询、类别、时间、地图范围及比较 ID 读取原 `agent_result_projection`。只将原结果中当前公开 Activity 的 ID 作为服务端私有输出验证集合；仅获邀请的活动不因此获得公开模型引用权限。错误类型/UUID、其他查询结果、未选实体、私密活动、候选 Memory 或工具提案均拒绝。原 scalar/query-only Task 不获得实体权限；非空实体引用必须由当前原生领域结果确认。

这个私有集合及版本证明不进入 `ProviderRequest`，不扩大 query-only 出口。用途是输出核验，不能借 TASK_CONTEXT_READ、Memory、MODEL_EGRESS、公开分享或领域动作批准。外部文本、OCR、模型回答中自称 system/approved/SQL/shell/URL 的内容只作数据；结构中的伪权限和执行字段拒绝。没有模型 SQL/shell/URL 执行器，也不因输出创建活动、报名、SocialIntent 或 Memory。

原生同事务最终收口和单次格式修复见 [ModelRequestRun](MODEL-REQUEST-RUN-V5.md)、[重试规范](MODEL-RESILIENCE-POLICY-V5.md)。实际验证材料在 `work/model-output-isolation-2026-10-06` 与 `docs/testing/evidence/model-output-isolation-2026-10-06`；状态仍由根核验后更新。当前生产 Gateway/Service/main/HTTP 无新 provider 接线，MODEL_ACCESS 仍 UNAVAILABLE；正式模型、Vision、A2A、自动写、真机模型交互与生产证据均未运行。Closed Pilot / Consumer Beta = NO。


## 2026-10-06 AIR036：闭集规划与模型输出分离

新的只读计划 schema 不替代 `air.model_result.v1`，也不是 provider 自授的 ToolCall。当前原生入口继续使用原 `ActivityQuery`、`air.answer.v1` 和空 `ToolAllowlist`，provider 只能收到原 query-only 输入。已获准的领域 Activity 结果只供服务端输出验证，不给 provider 原始来源、Memory、坐标、组织上下文或工具权限。

通过原 parser/AIR028 当前实体验证的模型 Activity refs，包装为最多3条 `activity.search` / `activity.detail` 提案；真实本人现有候选则走独立原生 `memory_candidate.review` 人审读取，不调用模型。action参数与资源ID闭集、有界、对应互斥union；原计划 decoder拒绝未知/重复/大小写别名、确认/权限/代码字段。ModelAnswer里的SQL/shell/system/URL只为数据，不复制为执行步骤；没有可执行代码或工具调用接口。

2调用上限含原一次格式修复/规范暂时错误；原拒答、截断、无效分类、未知 usage/费用、未知 provider/Settle 无重发合同不变。规划 JSON只是显示形状，当前权限/来源来自原 SQL和私有 Run handle，不来自解析通过或 `requires_confirmation=false`。默认网关和 main/HTTP仍 UNAVAILABLE；没有外部推理SDK/网络/provider启用。完整 native 检查与合成 adapter 的范围见本轮证据，正式模型/真机模型/生产仍 NOT_RUN。


## 2026-10-06 AIR037：工具元数据不成为模型工具权利

本地注册表与原生许可检查消费已经通过 AIR028/AIR036 的提案和原当前领域范围，未向 provider 发出工具、ACL、活动正文、策略、Session 或任何工具许可；原 query-only 输入与空 tool allowlist 保持。输出 `confirmed` / `requires_confirmation` / permission_override、图像/OCR/第三方命令和代码字段仍被原闭集 schema 拒绝。活动正文中的同类指令只作为不可信领域文字，不改变当前权限。

`LocalPlannerOutcome.ToolPlan()` 私有进程内关联只能由原成功 native handle 给出；展示 JSON 无法重建，纯注册表或 041/044 检查不能替代原 Session/Task/source/公开 ACL。未知工具 fail closed，只读操作不再调用模型，不增加原062四级预算使用。沙箱只给 `CONFIRM` 版本视图，未调用写工具或开启外部执行。正式 IdP/provider/出网/自动写仍未获准，不把本地合成 provider 返回称为正式模型能力。
