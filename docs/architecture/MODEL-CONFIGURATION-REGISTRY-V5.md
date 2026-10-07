# Birdtie 模型配置版本登记与固定引用

2026-10-03；BT-V5-AIR-027。唯一对应规范。来源：`work/v5-materials/BT-V5-AIR-BACKLOG.json` 的 AIR027，经现有队列去重接续；不新增任务或替代发布门槛。

## 2026-10-05：真实 ModelRun 接续（AIR027 本轮）

AIR016 的083账本为确定性 `STAGE_CANDIDATE`，不虚构其模型 prompt。AIR011 已建立088真实 ModelRun/Step：`model_request_runs.binding_id` 外键指向058不可变绑定，再由 configuration version/fingerprint 组合外键指向中央配置与 prompt/policy 制品。真实创建复用原具体出口 Preview→Approve→Create，并在创建前核验当前本人、Session、Task、配置、批准、目的地与预算；静态版本引用不授予这些权限。

历史定位复用现有 `ReadOwnLocalModelRun` → `ReadPinnedModelTaskConfiguration(..., false)` → `ReadModelConfiguration`。088 ModelRunID 与058 BindingID不同；原 `RunReference.RunID` 仍表示 binding selector，不改名冒充真实Run。可读取配置 metadata，不从 metadata、JSON、原ID或重启重建执行handle、Controller Ticket、私人答案或 UNKNOWN 重发能力。Task改变后的历史配置仍能定位，当前执行/源核验仍拒绝旧许可。058 binding 的 `execution_status=UNAVAILABLE` 原语义不改变。

本轮专属测试 `model_request_run_configurations_integration_test.go` 使用合成的已批准本地资料创建真正持久的 PLANNED Run+Step：R1固定v1，CAS切v2后R2固定v2，两个独立OS子进程读取两Run的精确配置与中央制品摘要，回退v1后R3固定v1。这些进程只读取配置，不派发模型，不称Run已执行完成或API服务重启。原ModelEgress/ModelRequestRun相关测试单独覆盖当前批准与费用/恢复路径。无新DDL、HTTP、UI、provider或影子目录。

本轮初次fixture失败及修正不覆盖058历史，相关原始命令、全源SHA、旧行/xmin与可见语义目录、独占库清理见 [本轮证据](../testing/evidence/model-run-configurations-2026-10-05/README.md)。本地专项组合测试及根代理全仓核验结论分别记录；任务是否DONE由根按原AC核证。生产provider/费用、真机/辅助技术和真实试点未由本轮验证，Closed Pilot / Consumer Beta仍NO。

## 2026-10-03：058阶段历史记录（原文保留）

以下“PARTIAL / AIR016 TODO / pre-run”描述是058原交付时点事实，不能作为2026-10-05当前实现状态。原证据与限制完整保留。

## 已实现的本地范围

`internal/modelconfiguration` 提供可实际调用的不可变版本目录、严格解码、配置解析、请求固定与引用核验。迁移058和 `postgres/model_configurations.go` 提供真实持久的中央配置目录、独立激活指针和本人原生 Task 的 **pre-run 配置元数据绑定**。隔离 PostgreSQL 验证使用合成数据，属于 CODE_LOCAL。

真实 AIR AgentRun/Step ledger 尚未接入：`RunReference.RunID` 是未来运行关联用的规范 UUID selector，当前数据库行是 `agent_task_model_bindings`，不是已执行运行记录。现有确定性 MVP Task 入口没有自动调用新绑定接口。本任务没有新 HTTP/UI/main 接线、provider SDK、实际模型请求或工具执行。

**当前任务交付状态为 PARTIAL。** 原文“历史 run 保存版本引用 / 旧 run 可定位准确配置 / 每个模型任务绑定”尚未完整满足，不能用 CODE_LOCAL 标签抹掉缺项。AIR016 仍为 TODO，其职责包含真正的 AgentRun/RunStep 持久账本；待该任务建立受控运行创建/恢复路径，在实际运行创建或执行前接入本目录的精确版本固定，把配置引用持久保存到真实 Run，并实测重启、切版本、回退后旧 Run 读取与缺配置拒绝，才可恢复027的整项验收。根代理管理队列和后续依赖；这里不另造影子 Run。

配置存在、激活成功、历史引用可读都不授予身份、源读取、分析、模型出口、费用预算、批准或领域写权限。绑定行的 `execution_status` 只能为 `UNAVAILABLE`。Organization/Business 配置绑定未具备完整原生授权 resolver，v1 保持 Unavailable；不会用成员布尔值推断权限。

## 不可变配置

`air.model_configuration.v1` 必须完整包含下列十个字段，拒绝未知、大小写别名、重复、缺失、null、超长、不合法 UTF-8 和尾随 JSON。显式 `DecodeConfiguration` 与标准 `json.Unmarshal` 使用同一个严格解析器。

| 字段 | 当前边界 |
| --- | --- |
| schema_version / version | 固定 schema；配置版本为闭合格式的精确文本标识 |
| task_kind | 复用007的 ACTIVITY_QUERY / MEMORY_CANDIDATE_EXTRACTION |
| prompt_version | 中央 PromptDefinition 的精确版本，内容最长4096字节 |
| input_schema_version | 007当前 air.messages.v1 |
| output_schema_version / output_mode | 007实际 answer/candidate schema 及对应模式 |
| tool_allowlist | 已支持活动 search/detail 闭集；提议不执行工具 |
| policy_version | 中央不可变政策制品版本引用 |
| capabilities_required | 007当前 text、结构化校验、工具提议的精确需求闭集 |

目录启动加载会拒绝已声明配置所缺的 prompt/policy、重复版本和不支持的 schema/tool/capability；未知版本在 Resolve/Bind 和 Store 绑定前拒绝。空目录不具备任何可绑定版本，不构成服务可用证据。

能力和工具列表去重、排序、复制；返回值不会暴露目录可变数组。fingerprint 绑定规范配置、PromptDefinition 精确文本摘要和 policy artifact SHA256。政策摘要只是维护者登记的制品引用，不表示政策已执行、内容来源已获准或真实模型许可已通过。007没有公开 SchemaValidator interface，本实现实际复用其 `ValidateRequest` 和已支持 schema 关系，不声称复用不存在的接口。

Prompt 集中存于 `model_prompt_versions`，不依赖 provider 的 prompt 存储。生产模块不散落用户无关长 prompt，也不把 Task 私人正文写进中央 prompt。`PrepareRequest` 拒绝调用方 system message，添加已登记精确 prompt；`BindRequest` 再用007校验请求和静态配置。调用方声称某 prompt_version 不能替换其真实内容。

## 持久登记、激活与回退

058仅新增五张表和对应约束/触发器，不改写旧领域行：

- model_prompt_versions / model_configuration_policy_versions / model_configuration_versions：不可 UPDATE。同版本相同登记幂等；内容或 fingerprint 冲突失败。已引用制品受外键保护，不可删除；未引用制品仅可由可信本地维护者清理，不提供用户或模型入口。
- model_configuration_routes：按 task_kind/output_mode 选择一个版本，独立 revision 初始1。Store 激活必须提供当前 expectedRevision；切版本/回退均精确增加1，旧 CAS 拒绝。数据库触发器也拒绝跳版本、改路由身份或只涨 revision；激活不改变任何旧绑定。
- agent_task_model_bindings：固定 Agent/Person/Task、配置版本/fingerprint、真实源 token/time。不可 UPDATE；原生 Task 或 Agent metadata 删除可以级联清除，禁止源仍存在时直接抹掉绑定。

`RegisterModelConfiguration`、`ReadModelConfiguration` 和 `ActivateModelConfiguration` 是本地可信服务维护原语，未向客户端或模型开放；它们不是新管理员身份体系。读配置时从实际 PG 制品重新计算 fingerprint，坏内容/摘要失败。

新绑定必须匹配精确已激活配置版本及 route revision；已存在 binding UUID 只可在同本人、同 Task、同实际源版本、同配置下幂等读取原引用，即使 active pointer 已切换。不同源/版本/主体不能重用 ID。切新 prompt 只影响明确允许的新绑定，回退后旧 v2 绑定仍定位 v2。

## 当前身份与源版本

`BindModelTaskConfiguration` 在真实事务内从当前 session digest 推导 Person，联查 active account、精确 active PersonalAgent、AgentProfile metadata、本人 acting-user/owner 的 ACTIVE 原生 Task，并加 SHARE 锁。匿名、撤销/绝对过期/闲置过期会话、dev_phone 不被允许的环境、错主体、错 Agent、缺 metadata、缺 Task 或不可绑定状态失败；不回填或恢复缺失数据。

源 token 复用已有 `agentevent.QueryVersion(updated_at, to_jsonb(task))`。事务 `SET LOCAL TIME ZONE 'UTC'`，避免 session 时区改变 PostgreSQL 时间表示导致同一真实源不同 token。私人 query/conversation 只在进程内用于摘要，不复制、记录到新绑定或发送到模型。同 updated_at 内容变更仍使 token 不匹配。该 token 是不透明快照版本，不能称为递增 revision、当前批准或永久权限。

锁等待后，在同一个当前 PG `clock_timestamp()` 上重新检查会话 absolute/idle expiry 和007请求 deadline，再提交。真实等待锁后过期测试必须无绑定写入。锁能串行化本事务内的源/身份变更；提交后状态仍会变，任何将来的模型/工具执行都必须再次走实际授权和版本检查。

`ReadPinnedModelTaskConfiguration(..., revalidate=false)` 允许当前合法本人在 Task 内容改变后定位旧不可变配置，仍要求当前同一身份、精确 Agent、metadata、Task 留存，不允许撤权后访问。`revalidate=true` 还要求 ACTIVE 当前 Task 和源 token/time 匹配。历史读取不是当前执行批准。

绑定不固定完整动态 Request 正文、上下文、预算或授权快照；这些属于后续 Run/Step/context/policy/budget 任务。本地合成配置和测试的政策摘要不会成为真实许可。

## 验证与发布边界

复现入口为 `work/v5-air027/scope.ps1 -Production -EvidenceLabel <独立标签>`：创建独占随机 DB，001–057、三个开发 seed、升级前真实留存合成 Task/当前 Memory/DELETED Memoryv2，再058；检查所有旧表完整 JSON 行在升级/down/reapply前后相同。prompt-only/policy-only 非空 down 必须 psql exit3；真实 Store 制品/route/Task-binding 非空 down 必须 SQLSTATE P0001 且旧引用仍可读。空表 down/reapply 才允许通过。只 drop 本次独占 DB，不在生产执行 down。

Go 范围用例覆盖缺版本、immutable/深拷贝、严格 JSON、008可复用的能力需求、真实持久重开 pool、切版本/回退/旧引用、并发幂等、CAS、未知/跨主体/撤权/metadata 删除、同 clock 源变更、时区、等待锁后的会话与 deadline，以及 SQL 闭集/不可变/回退约束。原始失败、旧 staging 结果和每轮生产 SHA 保留在 [本任务证据](../testing/evidence/model-configurations-2026-10-03/README.md)。全 Go 回归只有对实际当前源码和058实库运行后才可计入，不引用005早期结果作为本任务结果。

本任务没有用户可见 UI 改动；中文 UI、UX-CHECK、真机、Flutter 检查仍由相关任务独立验收，本证据不声称执行这些检查。真实 IdP/HTTPS/模型出口/费用/授权活动和运营条件未齐，Closed Pilot / Consumer Beta 仍为 NO。
