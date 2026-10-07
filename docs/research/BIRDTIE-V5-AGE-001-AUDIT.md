# BT-V5-AGE-001 — AgentProfile 基础模型审计

日期：2026-10-02。范围：`BT-V5-AGE-001`基础metadata；保留任务开始时的代码基线、范围与静态审阅，实际已完成的根任务结果另见§6.1。live状态以 `automation/codex_task_queue.json` 为准。仅仓库/隔离本地验收，不授予生产部署、模型出网、真实Agent自动写或A2A权限。

## 1. 来源与任务边界

- [AGE 原文](../product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md) 第246行 `AGE-001`：统一 AgentProfile，绑定 `agent_id`、`owner_type`、`owner_id`、`profile_version`、`created_at`、`updated_at`；PERSON / ORGANIZATION / BUSINESS 共享基础设施；Profile 与 Agent Identity 分离；迁移向后兼容。
- 同一源第286行 `AGE-002` 才定义 Public Profile / Private Agent Profile 的内容与分离，第344行 `AGE-003` 才定义逐字段 PUBLIC / CONNECTIONS / COMMUNITY / PRIVATE / AGENT_ONLY。不能因001的统一模型验收而把002/003标为完成。
- [137项唯一映射](../../automation/v5_requirement_mapping.json) 将上述要求映射到三个独立任务；001依赖已完成的身份/共用角色基础及 `BT-V5-INT-001`。
- 遵守 [认知 ADR](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md)、[Memory 边界](../architecture/AGENT-MEMORY-ARCHITECTURE.md)、[身份与 ownership](../architecture/AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md)、[Business](../architecture/BUSINESS-PRINCIPAL-MODEL-V4.md)、[Venue](../architecture/VENUE-CAPABILITY-MODEL-V4.md) 及 [V5 执行协议](../../automation/CODEX_V5_EXECUTION_PROTOCOL.md)。本轮准确schema/store范围由唯一 [Profile基础规范](../architecture/AGENT-PROFILE-FOUNDATION-V5.md) 维护。

## 2. 实际基线与缺口

本表记录001开始时的基线；本轮新增实现和检查结果另由任务证据记录，不能以本表历史“缺少”句覆盖后续 live 状态。

| 领域 | 实际代码与行为 | 对001的裁决 |
| --- | --- | --- |
| 稳定账号 / Agent | `001_foundation.sql` 定义 accounts；`019_agent_identity_organizations.sql` 定义稳定 `agents.id`、principal account、personal / organization / system 类型和生命周期；Person / Organization 创建路径已在同事务中创建自己的 Agent | REUSE；不建立第二套身份、登录或 principal |
| 当前个人 Profile | `identity.Profile` 仅有 accountId / displayName / bio / visibility；`postgres/identity.go` 有公开、本人、特定 profile_view grant、Block 和活跃账号读取；`profile_edit.go` 是本人既有编辑及审计 | REUSE 原服务；不把 user_profiles 重命名或直接视为 AgentProfile，也不复制其内容到新基础表 |
| 当前组织资料 | `postgres/organizations.go` 以 Organization实体ID管理 name / description / links，以活跃真人 Owner/Admin鉴权；Organization Agent 属于 `organizations.account_id`，不属于管理员 | REUSE；管理员更换不替换 Agent/Profile/owner |
| 独立 Business | 041新增 Business Account / businesses / memberships / venue关系；旧 Organization business/venue 类型保持原类和ID | REUSE；共享Profile类型不改变经营权、claim或组织历史 |
| Typed ownership | `actorref.PrincipalRef` 对 Organization 使用 account_id；`actorref.ActorRef` 对 Organization 使用 organizations.id，二者明确不同 | REUSE；Profile owner绑定 principal account，不能混用实体ID |
| 共用 runtime | `agentruntime.PolicyFor` 的 Personal / Organization复用能力包；Business描述存在但 Available=false，Community无Agent | REUSE；metadata存在不是调用许可 |
| Phase0认知合同 | `agentcognitive` 有 AgentReference / SourceReference / SourceState，角色描述与资格判定分离；当前窄桥仅本人普通Profile / Context读取；Memory ports返回 ErrUnavailable | REUSE 边界；001不开放模型工具或Memory |
| 统一版本化 AgentProfile | 基线无独立持久 `agent_profiles`、正版本metadata类型、精确 Agent / owner绑定与版本管理store | EXTEND：001本轮增量 |
| Public / Private内容、字段可见性 | 基线无PrivateAgentProfile真源和逐字段audience实现 | 后续002/003；本轮不能用空JSON或保留字段声称完成 |

基线检索覆盖 `apps/api/internal`、`apps/api/migrations` 和 `apps/client/lib` 的 AgentProfile / agent_profiles / profile_version，并检查019、041及身份/组织/角色实现；仅枚举出 Phase0 AGENT_PROFILE source类型不代表存在Profile服务。

## 3. 本轮最小完整实现

1. 建立一个共用 AgentProfile metadata 模型和增量持久表，包含源001要求的六个字段；`profile_version` 为正数，初始版本是新建metadata的真实版本，不能被用来伪造旧Profile/Memory来源版本。
2. 一条Profile绑定一个真实、稳定的Agent和唯一 typed owner principal。`owner_id` 表示 `accounts.id`；PERSON对应Personal Agent，ORGANIZATION对应Organization Agent，BUSINESS只支持明确独立Business ownership形状。System/Community/City/Place/Venue不是本表的社交Profile owner。
3. 用外键、约束或数据库触发器落实Agent类型、principal类型、owner与Agent的精确匹配，并保护绑定不被更新为另一主体。仅Go校验或接受客户端传入 owner_type/owner_id 不足够。
4. 版本变更必须由store/数据库受控路径管理；精确旧版本冲突失败，不能静默覆盖或允许版本倒退。创建时间与稳定绑定不得随编辑变化；updated_at须与真实更新一致。
5. 为已存在的可绑定Agent建立metadata，不更改旧 Agent、Account、Organization、Business、Place、Activity、Intent、Participation或Plan ID。兼容未来正常Person注册/Organization创建，不留下新Agent没有metadata的生命周期缺口。
6. 当前store可限定为内部受信metadata primitive，检查精确Agent/owner绑定和当前账号/Agent/组织状态；必须声明它不核验真人会话、成员、purpose/consent或私密字段授权，不能直接挂HTTP或模型工具。未来面向人类或AIR调用的gateway另查当前会话、workspace、成员和动作角色；共享schema不能扩大Person私密内容权限，不把客户端ownership字段当权威。
7. 若为共享基础设施预留Business Agent/Profile记录，其状态、创建条件和回退行为须在本轮架构中明确；不能自动启用Business Runtime、继承Person或Organization能力、以metadata代替claim审核，或批量迁移旧Organization。

没有Profile内容字段、公共读取接口、PrivateAgentProfile、字段audience投影、消费编辑UI、Memory/Evidence存储、ContextAssembler、模型出口或自动动作。这些仍按其独立任务实施；001无需新增空壳按钮、页面或声称真机体验已改变。

## 4. 权限、生命周期与隐私边界

| 场景 | 必须保持的结果 |
| --- | --- |
| 伪造Agent/owner绑定或把Organization实体ID用作owner account | 内部store/数据库拒绝；不能只验证UUID存在 |
| 未来Person A 请求 B的私有AgentProfile | 当前不存在新HTTP/私密内容路径；后续gateway必须拒绝，metadata primitive不是人类授权API |
| 未来活跃本人 / 指定组织授权管理员消费资料 | 后续gateway核验当前身份/成员，仅访问授权范围；当前store不宣称实现真人权限或字段/Memory可见性 |
| Organization owner转移 / 成员移除 | Agent/Profile/owner account稳定；旧管理员失去权限，不继承其个人内容 |
| Account停用、Agent suspended/retired、组织closed | 调用侧当前资格检查失败；保留metadata并不证明可调用 |
| 独立Business有metadata | Business角色仍不可调用；普通Person/Organization不得冒充Business；claim与当前真人成员资格仍由原领域决定 |
| 旧organization_type=business/venue | 仍是ORGANIZATION principal / Agent，不批量变为BUSINESS |
| Community、CityContext、Place、Venue | 不创建账号/Agent/Profile，不由成员身份、名称或坐标推导owner |
| Profile公开、051/052开关、AGA同意或模型confirmed | 不继承到Memory、provider出口、跨Agent共享或真实写权限 |
| 来源改版/删除/撤权 | metadata版本不是完整来源resolver；本轮缺少的来源功能继续Unavailable，不能自动填Authority Facts |

## 5. 适用验收与复现证据要求

下列保留原验收清单；文档审计者没有运行Go、数据库迁移或客户端检查，不以清单存在计为PASS。根任务随后执行的真实结果与证据见§6.1。

| 验收层 | 正向检查 | 负向 / 回归检查 |
| --- | --- | --- |
| Go类型 | 一个模型表达PERSON / ORGANIZATION / BUSINESS；有效Agent/owner/version/time | 未知类型、Community/System/Place/Venue、空或非法ID、非正版本、owner/Agent不匹配拒绝 |
| 持久与store | 精确Agent/owner绑定读取；Ensure重复调用不改旧版本；数据库受控版本更新/重读；声明更新路径时用旧版本防覆盖 | 错配、不存在、不可用账号/Agent/组织；Business返回Unavailable；原认知权限不开放。尚无HTTP、真人成员gateway或store版本编辑API，如实记未实现 |
| 增量migration | 空库up、现有052数据up、已有及新注册/组织Agent绑定、再跑seed不会重复 | 类型/ID错配、重复绑定、非法直接SQL更新、Agent ownership重绑定受约束；旧ID/API完全保留 |
| down / reapply | 明确受支持的空或仅基础metadata场景down/reapply；事务失败不留下半改schema | 若新增记录/版本或未来内容会丢失，down拒绝并保留表/数据/旧身份；若已出现business Agent，不能靠删除真实数据恢复旧类型约束 |
| 默认并发Go | 带隔离数据库的默认 `go test ./...`、vet、build，既有身份/权限/活动/RSVP/Plans回归 | 测试创建自己fixtures、只清理自己ID，不借全库LIMIT1实体或假设公共列表总数 |
| Phase0兼容 | 旧 CurrentDomainAdapter 普通本人Profile/Context仍可读，stableID不变 | Memory读取/候选、模型出口/动作/A2A仍Unavailable；metadata不直接变为认知资格AVAILABLE |

适用UX规则：实体稳定ID、原领域动作与权限、具体版本和真实验收边界。此次仅后端基础metadata，不修改Flutter页面/导航/地图/Pin/键盘；移动端视觉、辅助技术和真机验收属于本项**不适用**。如全项目构建或原页面回归由根任务运行，作为兼容检查记录，不能写成新的AgentProfile消费UI已验收。

## 6. 实际053与store的只读设计审阅

基线时053尚未落地；随后已实际读取 [053 up](../../apps/api/migrations/053_agent_profiles.sql)、[053 down](../../apps/api/migrations/053_agent_profiles.down.sql)、[metadata模型](../../apps/api/internal/agentprofile/model.go)、[store](../../apps/api/internal/postgres/agent_profile.go) 与模型测试。以下保留静态设计核对，根任务后来完成的隔离库/Go结果另列§6.1。

| 已读实现 | 设计核对结果与边界 |
| --- | --- |
| 053六字段与复合FK（up第40–61行） | 一个 `agent_profiles` 表；agent_id为PK，owner_type闭集三个；正bigint版本与有限/有序时间。生成内部binding keys，复合FK核 `agent_id + owner_id + agent_type` 及 `accounts.id + account_type`；Entity ID不能冒充principal |
| revision guard（up第64–87行） | 新metadata版本必须1；更新不能更换Agent/owner/创建时间，必须精确+1；max bigint溢出拒绝，updated_at由数据库单调管理。没有宣称已提供用户编辑/API/通用来源resolver |
| bootstrap与backfill（up第89–110行） | 新Agent插入触发同事务创建metadata；已有personal/organization绑定只补metadata；不写user_profiles/私有声明或替换旧ID；System/Community/CityContext/Place/Venue不创建Profile |
| Business预留（up第3–29行） | 只扩展独立business Agent形状，status只能suspended/retired；迁移不插入Business Agent。旧Organization business/venue仍Organization。原runtime unavailable，store的两个入口对BUSINESS直接ErrUnavailable |
| Get / Ensure（store第15–77行） | 只六字段metadata；New/Validate为shape，事务share锁核精确Agent、当前Account/Agent及活跃Organization。Ensure只补缺失metadata，不覆盖、重置版本或创建身份；不复制普通Profile内容 |
| 内部授权边界（store第15–18行） | 明确owner是内部binding claim，不是human session/membership/model/purpose/consent grant；没有HTTP/client接入，也不接认知资格。后续必须建立真正gateway，不得把此primitive作为授权证据 |
| 有数据down（down第3–9行） | 任意Profile（包括bootstrap metadata）或预留Business Agent存在即事务拒绝，不删除有价值记录恢复旧constraint；仅真正空的隔离数据场景支持down/reapply，无生产回退承诺 |

静态审阅未发现阻止001基础模型验收的设计冲突；之后根任务实际运行以下验收并记录任务完成。文档审计者只读检查已存在日志与计数，没有重跑功能测试。

### 6.1 根任务已完成的实际local证据

[AGE001证据README](../testing/evidence/agent-profile-foundation-2026-10-02/README.md)、[汇总JSON](../testing/evidence/agent-profile-foundation-2026-10-02/verification-summary.json)、[053迁移日志](../testing/evidence/agent-profile-foundation-2026-10-02/v5-age001-migration053.log) 和 [复现脚本](../testing/evidence/agent-profile-foundation-2026-10-02/reproduce.ps1) 均已读取。

- model37、store41、限定并发779 PASS事件，完整默认Go三轮各1302 PASS事件，所有这些运行均0失败/跳过；Go vet/build通过。计数包括主测试及子场景。
- 053空库fresh、带开发seed的052→053、旧身份/UserProfile/Activity/Place保留、SQL绑定/版本保护、预留Business禁用和后续Agent bootstrap通过；三开发seed不创建Business Agent。
- 非空down原子拒绝并保留数据，真正空的隔离表down/reapply通过；8并发Ensure同记录，双连接CAS恰一次成功、一次零写；fixture前后计数一致，专用库已移除。
- 初次纯测试重命名导致的编译失败与修复、首次SkipFullGo迁移检查及其后完整通过日志均保留；预期受保护down的ERROR不是放弃回退验证。

证据等级为 `LOCAL_DISPOSABLE_SYNTHETIC_ONLY`。没有新增Flutter/UI/真机验收，没有生产库down或正式部署；不把上述事件计数解释为真实用户授权或功能全部完成。

## 7. 仍未实现与发布门槛

001完成最多证明统一metadata schema/store/类型及其兼容性，不能证明完整AgentProfile enrichment。002 Public/Private分离、003字段可见性、Memory/Evidence、版本化来源/consent resolver、ContextAssembler、候选审阅、更正删除、ModelGateway、运行/批准/effect账本与执行器仍是后续能力。

**Closed Pilot Ready：NO；Consumer Beta Ready：NO。** 本项不补真实IdP/HTTPS、授权组织/活动、生产地图/API、部署日志、调度运营、值守渠道或真实A→H证据；也不补live provider批准或真实Agent写许可。
