# ADR：Agent 认知架构与 AGE / AIR 领域边界

日期：2026-10-02。状态：**Accepted as V5 target boundary**；本ADR建立于 `BT-V5-INT-001` Phase0，AGE001metadata、AGE002本人Private编辑及AGE003字段受众/人类投影增量由 [唯一Profile规范](../architecture/AGENT-PROFILE-FOUNDATION-V5.md) 分别记录代码/验收范围。本ADR不代表认知Profile读取、Memory、模型或执行服务已实现。实际交付以live队列、代码和独立验证证据为准。

## 1 来源、已有 canonical 与增量裁决

来源是 [AGE 原文](../product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md) 的 AGE-059、078、079、080，以及 AIR 包解出 `work/v5-materials/BT-V5-AIR-DESIGN.md` §3–8、`BT-V5-AIR-BACKLOG.json` 的 AIR-003/004。准确逐项来源与旧任务映射见 [V5 对账矩阵](../research/BIRDTIE-V5-MATERIAL-RECONCILIATION-2026-10-02.md) 和 `automation/v5_requirement_mapping.json`。源材料保留原文，以下为本仓库的增量裁决。

已扫描 `docs/architecture/`、`docs/decisions/`、`docs/product/`，未发现同职责的认知 ADR、Agent Memory 架构正文或 Personal Agent 富集产品正文。已有身份、Context、关系、SAF、AGA、Business/Venue canonical 继续拥有自己的领域，本 ADR 不复制或取代它们。

| 源要求 | 本轮裁决 | 保留 / 新增边界 |
| --- | --- | --- |
| AGE-078 认知 ADR | EXTEND | 引用既有身份与 ADR 0017；按仓库规则将决策置于 `docs/decisions/`，不另建 architecture 下同名 ADR |
| AGE-079 Memory 架构 | NEW 文档 | [Memory Architecture](../architecture/AGENT-MEMORY-ARCHITECTURE.md) 成为该目标领域唯一正文，不表示新增表/API/store |
| AGE-080 Personal 产品规范 | EXTEND | [Personal Agent Enrichment](../product/PERSONAL-AGENT-ENRICHMENT.md) 细化 V4 产品总纲及 Now，维持原产品与直接操作路径 |
| AIR-003 共用框架与权威适配 | EXTEND | 复用 ActorRef、Agent Runtime、当前普通 Profile / 本人 Context 窄只读接口；缺少的 Memory/Profile 富集服务明确为未来实现，不宣称已有全套 AGE adapter |
| AIR-004 增量队列 | 由原执行者验证 | 原 ID/状态/证据/owner/lock 保留，去重、二次追加和拓扑通过实际工具核验；本 ADR 不写队列或虚构命令 |
| AGE-059 未来 Community Agent | 仅保留扩展可能 | 当前不创建账号、Agent、能力包或认知资源；不作为本期启用范围 |

## 2 决定：身份、认知与执行分别负责

**Identity → Profile / Memory / Evidence → 当前授权 Context → Policy → 提案 / 批准 / 执行结果** 表示责任关系，不表示一条已经部署的自动执行管线。

- **Identity** 由现有 Account、ActorRef/PrincipalRef、Agent ownership 和成员服务负责。`agent_id` 稳定，provider/model 切换不创建新 Agent。Organization 的 actor ID 与 account principal ID 不可互换；模型和客户端不能声明自己代表谁。
- **Profile** 表示明确身份资料及其获准投影。当前 `identity.Profile`保持原普通资料；AGE001有六字段metadata，AGE002追加九类本人Private输入和self编辑，AGE003再增11字段五类受众和原资料ACL相交的人类投影，各自隔离证据在唯一Profile规范。普通四字段Profile不joinPrivate；新字段投影不构成认知读取或模型许可。资料字段与推断建议分开，偏好更正不能改写服务器身份、成员资格或经营权。
- **Memory / Evidence** 由 AGE 的单一权威领域负责，服务未实现前不开放。Moment、报名、收藏、聊天元数据是各自领域的记录，不自动成为 Memory 或兴趣事实。证据必须有主体、来源版本、时效与允许用途。
- **Context** 由 AGE 的未来装配边界从现有领域和获准 Memory 读取最少当前资料。已有 Context Graph 是情境节点和本人声明，不是已完成的 Context Builder，也不是位置、学历、出席或成员资格证明。
- **Policy** 由服务器确定性规则与当前领域鉴权执行。角色能力包、数据可见性、独立同意、版本、预算、行动影响分别检查；模型分数、自然语言、`confirmed:true` 或 autonomy level 都不是权限凭据。
- **Autonomy** 是未来准备或委派的产品配置，不改变身份或授权。未定义配置、未知风险、缺 resolver 默认拒绝；观察/辅助也只能读获准数据。LEVEL_3 不默认启用，本期无真实自主动作。
- **AIR** 拥有有界运行、模型适配、非权威 Observation / Candidate / ActionProposal 和执行恢复。它仅读取获准视图、提交候选给 AGE 校验，不直接改写权威表、身份或授权；生成 `ACTIVE` 或“执行成功”文本不能改变权威状态。

## 3 数据所有权与当前实现表

“已有”仅指仓库内限定实现；不表示正式环境、现实身份/活动、生产供应商或消费者验收已通过。表中代码和迁移路径相对于 `apps/api/`。

| 对象 / 责任 | 唯一权威归属 | 当前代码锚点 / 状态 | AIR 接入限制 |
| --- | --- | --- | --- |
| Account / Session / Agent / typed principal | Identity、Agent ownership、成员服务 | `internal/identity/identity.go`、`internal/actorref/actorref.go`、019 / 041 迁移、`agentruntime/policy.go` 已有 | 服务器解析并检查活跃状态，不复制主体体系 |
| 当前个人 Profile | Identity 的 Profile 服务 | `identity.Profile`、`postgres/profile_edit.go` 已有；原源实时读取，AGE003附加字段限制 | 本人窄只读桥不构成认知Profile读取许可；不合并或复制原真源 |
| AgentProfile基础metadata | AGE Profile基础领域 | AGE001：053、`agentprofile.Record`、`postgres/agent_profile.go`；六字段版本metadata及内部typed-binding store | Get/Ensure不是真人鉴权gateway，不能直接接模型/HTTP或当成认知授权 |
| Public / PrivateAgentProfile内容 | AGE Profile内容领域；Public复用Identity | AGE002：Public原四字段/ACL，054九字段Private+本人store/HTTP；独立隔离本地验收已通过，见Profile规范§10.6 | 本人编辑不授权认知读取，Public/grant不包含Private；不复制Context/Moment |
| Profile逐字段audience / 人类字段投影 | AGE Profile权限边界 | AGE003：055、11字段五audience、本人CAS及人类投影；历史结果与legacy撤权/官方Person活动自动名字副本补验见Profile规范§11，003重开后本地补验通过2780完整Go/055全源保留 | 持续受众设置不是具体发送批准；公开活动不另授权Profile，PUBLIC/AGENT_ONLY均不授模型权限 |
| cognitive Profile读授权 | AGE未来用途权限边界 | 独立consent/source/Runtime resolver仍待后续 | 不从private self编辑、人类投影、prompt或临时JSON继承许可 |
| AgentMemory / MemoryEvidence | AGE 目标 Memory 领域 | 尚无持久表、写服务或读取 API | 模型只产候选；AGE 验证、确认并原子写入 |
| Context 节点 / 本人声明 | Context Graph / 各来源领域 | `contextgraph/model.go`、`declaration.go`、033 已有 | 当前声明只用于获准本人读取，不证明其他事实 |
| Moment / Activity / Participation / Tie / Place / Organization / Business | 各现有领域 | 现有稳定实体、ACL、revision 与受控写路径 | 引用原 ID；不从参与/媒体推断出席、亲密或经营权 |
| 规则机会、关系信号、新朋友匹配 | 现有 Opportunity / Relationship / NewPeople 领域 | V4 规则及独立开关已有 | 不改作 learner、Memory 或模型同意，不复制搜索/排序 |
| 授权策略 / 同意事实 | 现有服务器政策与未来 AGE 用途授权 | 当前 SAF/AGA 纯策略和特定功能开关已有；通用 Memory resolver 尚缺 | 只接可信当前事实；public Profile / 051 / 052 / AGA 同意互不继承 |
| AgentRun / Observation / candidate staging | AIR 目标运行领域，候选暂存单一归属 | 未来实现；AgentTask 不等于完整 AgentRun | 有界、脱敏、版本化、可取消；不存第二份权威 Memory |
| Approval / dispatch / effect ledger | 可信权限与执行边界，调用原领域动作 | 当前通用持久账本 / executor 未实现 | 批准一次消费、原子重验、效果对账；没有就不开放 AIR 写 |
| Provider / 模型预算与出口 | AIR gateway + 可信出口 Policy | 当前没有 live provider adapter | 模型无数据库/对象存储/任意网络凭据 |

完整来源证据和接口状态见 [Memory 架构](../architecture/AGENT-MEMORY-ARCHITECTURE.md)。Phase 0 的纯类型、拒绝策略和已有域窄只读桥即使测试通过，也不构成 Memory store、Context assembler、Model Gateway、授权签发服务或执行器。

Phase0实际共同合同在 `apps/api/internal/agentcognitive/`：`AgentReference` 复用 typed PrincipalRef / runtime Role；`SourceReference` 与服务器内部 `SourceState` 分开；`DecideEligibility` 对合法合成认知请求仍返回 UNAVAILABLE，非法或失效输入返回 DENIED。角色描述的 AVAILABLE 仅描述既有 capability pack，不开放认知工具。`CurrentDomainAdapter` 只桥接当前本人普通Profile / Context读取，仍无接入认知ports的精确identity/source/consent resolver，不补造旧对象Version=1。MemoryReader / CandidateSubmitter当前明确ErrUnavailable，没有HTTP调用或Memory写服务。

历史Phase0结束时无持久AgentProfile，AGE001首次只交付metadata和内部Get/Ensure；随后AGE002增加Person-only私人内容和本人编辑，AGE003增加字段受众与人类当前投影，不改metadata primitive的可信调用限制。Private和投影不是AIR可读的SourceState/BoundaryFacts，不打开Memory或任何新认知port。metadata的新建版本1仍不填造普通Profile/Context/Memory版本；Business预留形状不自动创建Agent，status不得active，store/角色调用仍Unavailable。详细增量、独立正负证据及回退保护只由 [Profile规范](../architecture/AGENT-PROFILE-FOUNDATION-V5.md) 维护。

AGE003的field policy是持续受众设置，不是某一源内容的具体外发批准。native profile_version只控制Private/policy写，不取代普通UserProfile/Context source版本；人类projection实时读当前源/ACL/字段overlay。未来认知来源与具体批准必须另绑源真实updated_at/revision及独立许可，不能以metadata版本或PUBLIC/AGENT_ONLY枚举作为模型读/发送凭据；认知端口当前仍Unavailable。

拒绝名字字段时不得从handle或官方Person活动的自动名字副本恢复该名字。当前没有独立publicHandle授权；拒绝分支使用通用标签，允许分支才保留既有名字回退。自动派生标签同时受原Profile粗ACL及字段规则约束，公开发布活动不是新增Profile公开同意；真正独立明确来源仍按其原域ACL处理。相关补验状态只见唯一Profile规范，不由本ADR宣称完成。

## 4 主体与资源隔离

1. Person 只使用当前会话下本人活跃 Personal Agent 的资源。个人历史、偏好和未来 Memory 默认私密；公开 Profile 不意味着私密认知公开。
2. Organization 共享运行框架但资源属于独立组织 principal；每次解析当前真人成员/角色与同一工作台。管理员不能读取其自己或任何成员的 Personal Memory、私聊或私人关系，再转存组织记忆。
3. Business 是独立商业主体；未来 Agent 必须有当前经营权、claim、真人角色及能力规则。现 Business 领域不等于 Business Agent 可调用；旧 `organization_type=business/venue` 仍是原 Organization principal，不批量重分类。
4. Place 是情境节点，Venue 是举办能力；都不是账号或 Agent，坐标/第三方目录不证明经营权。
5. Community 当前没有账号、Agent 或私有认知工作台；其真实成员仍通过本人身份和原 Community 权限操作。CityContext 也不是城市社交账号。
6. 缓存、索引、运行、候选、批准、事件和效果键必须绑定 typed principal / subject / Agent 与隔离范围；显示名称、外部自然 ID、城市或一个 UUID 字符串不能替代类型。跨账号/组织旧草稿、旧批准与迟到响应失效。

读取继续遵守 [Context Access Policy](../architecture/AGENT-CONTEXT-ACCESS-POLICY.md)。CONNECTION 资源尚缺真实资源级授权时拒绝；CLOSE / close friend 缺关系与该资源授权 resolver 时拒绝，不能以聊天次数、已接受联系、共同报名或模型推断代替。新文档不因写有这些 scope 就增加访问入口。

## 5 同意、来源版本与执行边界

上传/保存原内容、私人分析、给模型供应商的出口、保存长期记忆、公开字段、跨 Agent 共享以及真实外部动作分别授权。每种许可限定主体、purpose、资源/字段、接收方或目的地、当前 revision、期限与撤销状态；未知或缺失拒绝。

每次读取、恢复、候选审阅/确认和提交前重新核验来源版本及当前 ACL。来源编辑、删除、撤回、同意变化、账号/Agent 停用、成员撤权、Block 和到期使旧快照失效；旧 worker、缓存或备份恢复不得复活已撤回数据。AIR 调用不能永久复制任务级获准信息到 Organization/Business Memory。

未来批准绑定具体可见版本、完整 action digest、actor/subject/Agent、logical operation、目标/受众、资源/许可/成员/policy 版本及有效期。**批准收据不等于效果成功**；digest 不是幂等键。提交边界原子排序新鲜权限校验、单次批准消费、dispatch commit 与稳定 effect key / 账本记录，和撤权在同一权威序列中排序。

撤权先于 dispatch commit 则拒绝；commit 已先发生则可能在飞，停止后续步骤并对账，不能承诺收回网络请求或标“未发送”。结果不明保持 UNKNOWN_OUTCOME / 待核实，不能因 lease 到期或恢复盲目重发。此为后续必须实现的协议，本期没有真实 dispatch 或外部效果。

当前人类主动点击的报名、邀请、聊天、发布等仍使用原领域鉴权与确认；[SAF 合同](../architecture/AGENT-SOCIAL-INFERENCE-SAFETY-CONTRACT-V4.md) 的 Agent 闭集导航不授予写权限。[AGA 合同](../architecture/AGENT-TO-AGENT-PERMISSION-CONTRACT-V4.md) 的 ASK_USER 只表达可询问下一步，不表示兴趣、已送达或动作成功。

## 6 兼容、依赖与放行

- 原主体、Agent、Activity、Place、Moment、任务 ID 与旧 API 语义保留；不新增模型专属 Agent 或平行 Memory 服务，不推定新迁移号、HTTP 路由或接口已存在。
- AGE 最小 Profile/Memory/Evidence 写合同先于 AIR 候选提交；高层 learner 后消费候选结果，避免依赖环。Context 只需最小权威适配，不等待全量学习；活动只读工具复用原搜索，不等待第二 provider、视觉或 A2A。
- 必要撤权/删除传播、幂等、批准消费与未知效果恢复属于该功能 P0 安全内核，不能因为完整观测/平台工程为 P1 就延后。
- 中文主要界面、普通直接路径与地图/键盘/选择约束继续由 [Global UX](../ux/GLOBAL-UX-INTERACTION-CONTRACT.md) 和现 Now canonical 管理，本 ADR 不重做 UX。
- 离线 pure contract、受控假模型、本地合成验收、live provider canary 与正式试点分别留证。模型出口、视觉、AIR 真实写和 live A2A 默认关闭；本期只仓库及隔离本地验证。
- **Closed Pilot Ready：NO。** 缺真实 IdP/HTTPS、已核验组织与授权活动、生产 API/地图、调度告警、部署日志、值守支持及真实 A→H 时保留原 Gate，不以本 ADR 或纯测试解除。

验收至少覆盖模型切换 ID 不变、跨主体/错类型拒绝、角色不继承私人数据、未知 CLOSE、source current revision/删除/撤权、用途同意不继承、模型/客户端自证无效、窄 adapter 不宣称 Memory 服务，以及旧队列保留/幂等追加/无依赖环。实际命令和结果由当前任务证据记录，不在本文伪填 PASS。

## AGE003 最终补验结果（2026-10-02）

此前确实取得的2196三轮和055历史结果、初次DONE后立即重开的记录保留；现在4个旧多语句最终撤权guard、成员列表当前资格、同值handle拒绝回退、官方Person活动自动姓名的源粗ACL相交，以及原生维护者自动复制标签均已修复并取得当前源码证据。

最新3轮默认完整Go与055迁移独立fullGo各2780 PASS、0失败/测试跳过；全量vet/build exit0，当前API ready200、5未开放推断/协调路由404。fresh/seeded两自有库各62严格SQL拒绝+74真实helper断言，所有public基表完整旧行/版本保留，非空down exit3原子保护，明确清空后empty down/reapply保留Private v2/native v4；自有数据库/进程已清理。归档182文件/38源码hash完整核对、没有bearer泄漏；后续源码增量另取证，不覆盖此时真实日志。

自动维护者专项实际Store16 PASS、四包vet exit0；Social/Submit Community、Place publish/link_existing真实入口及历史Activity source reader覆盖。旧复制由服务器exact audit/source链接识别，仿prefix但无/错audit、真正独立manual及Host/组织信息保持；不删除或回填旧源。四legacy竞态43叶/48含父、成员9叶+1父、Person自动活动510、同值handle753/四包2077的实际限定结果及首次RED保留，不用顺序测试代替并发撤权。

**独立未修复缺陷**：实际City Seed ReviewActivity新候选发布返回23514 activity_organizer_exactly_one。已增量登记BT-FIX-CITY-001；合法历史source reader fixture和完整Go不算此业务入口成功，不把审核员或提交者猜作主办方、不削弱主办方约束。[缺陷与恢复条件](../research/CITY-SEED-ACTIVITY-PUBLISH-REGRESSION-2026-10-02.md)。

准确线性化点为最终payload SQL statement snapshot；不承诺之后网络中数据绝对撤回。11字段五受众、人类gateway及原源ACL与field policy相交是真实本地能力；无新Flutter设置页面、模型Profile读取、Memory/候选写、provider/视觉/A2A/自主动作或现实试点证据。AGENT_ONLY不授模型许可；普通源updated_at与native CAS仍分开。Closed Pilot / Consumer Beta均NO，原IdP/HTTPS、现实组织/活动授权、地图API、部署日志、值守、提醒运营与真实A→H门槛保留。[当前实测/历史与源码hash](../testing/evidence/agent-profile-visibility-2026-10-02/README.md)。

## AGE004 当前实现与验收（2026-10-03）

本次新增单一原生 `agentmemory.Record` / `056_agent_memory`，由当前 Person 本人直接管理明确声明。GET `/v1/me/agent-memories`、PUT/DELETE `/{memoryID}` 使用真实会话、活跃 Person / 精确 Personal Agent / native metadata 绑定和最终数据库会话检查；PRIVATE / AGENT_ONLY 都只供本人管理，不因此开放认知读取、模型或跨主体访问。UUID 是对象地址和幂等键，不是身份选择器。

13种类型、17个推荐字段加schema/owner/独立Memory版本已实现。EXPLICIT confidence=1 表示本人声明，绝不表示概率、客观事实或已核验身份；INFERRED 只预留 PENDING_REVIEW/EXPIRED/DELETED 的受约束持久形状，人工API拒绝推断写入。实际 Evidence、候选审阅/接纳、置信评估、自动学习及消费界面分别属于后续任务，认知 MemoryReader / CandidateSubmitter 仍硬 Unavailable。

创建version1、更新CAS+1；等值重试使用实际 jsonb 精确语义和微秒期限，避免指数/负零或相邻大整数误判。取得行锁后刷新数据库时间，写入与等值重试最终同时复核当前会话和保存期限；实际锁等待及触发器等待跨期限均拒绝并回滚。储存shape预检将越界数值返回400，不吞为503；wire与jsonb两层数字/字节约束明确记录，jsonb不能还原重复键，wire先拒绝。

删除清空正文和值、保留终态tombstone；重复删除不增版本，旧ID不可复活。过期读取仅投影EXPIRED，不伪造已持久的版本或过期调度；物理ACTIVE key仍由本人更新原ID或删除。已有Profile/Private/metadata源码版本不因Memory保存改变。任何含Memory行（包括tombstone）的down原子拒绝；仅真实父记录移除允许FK清理。

**源码复核修正**：053已有 agents AFTER INSERT metadata bootstrap trigger及既有Agent metadata backfill。004没有Ensure/重建被删除的metadata；缺metadata本人Memory入口拒绝，防止重新恢复已删隐私控制。前期审计中建议“server生成UUID / confidence NULL / Memory路径Ensure”不是最终实现决定，应以此节和真实代码为准。

正式证据：[168文件归档、14源码hash与原始失败](../testing/evidence/agent-memory-2026-10-02/README.md)。官方命令 `pwsh -NoProfile -File automation/verify_agent_memory_migration.ps1 -EvidencePrefix production-final2 -FullGoRounds 3` 实际三轮完整Go各3812 PASS、0失败/测试skip，19无测试包单列；Memory域/Store/HTTP scope433，全vet/build0。修复验证脚本路径清理后另以 `-EvidencePrefix production-final3-cleanup -FullGoRounds 0` 实测scope433、vet/build0及自动停止自有API；该轮没有另跑完整Go。领域252、最终Store68、107严格SQL拒绝/33正shape断言均已在对应日志核对。

自有fresh001–055/三开发seed/056、完整旧public行与nativeProfilev3保留、ACTIVE/EXPIRED/DELETED非空down exit3原子保护、清空自有数据后down/reapply与DB清理通过。实际编译API ready200，三本人Memory入口匿名401/no-store；重组API/Store后读取持久数据通过，不冒称真机或正式部署重启验收。最初重试冲突、数字503、跨期限保存以及fixture/清理脚本失败完整保留，修复后复验；只清理已核对路径/SHA的自有进程，手机预览未改。

Windows CGO0无gcc，race detector未运行；真实数据库并发已测。本项无新Flutter/UI、真实IdP/provider、现实组织/活动授权、HTTPS/地图、提醒调度运营、部署日志和值守/真实A→H证据。**Closed Pilot / Consumer Beta：NO。**
