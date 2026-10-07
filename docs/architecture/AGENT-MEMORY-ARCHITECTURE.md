# Agent Memory Architecture

日期：2026-10-02。状态：**V5 canonical 目标领域合同**，对应 `BT-V5-INT-001` / 源 AGE-079；截至2026-10-03，AGE004/005已有Person本人EXPLICIT持久Memory、人工来源引用和固定中文provenance；候选与认知/模型读写仍待后续。目标合同与已实现范围分开，当前实测见本页AGE004节。

依据 [认知 ADR](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md)、[身份](AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md)、[Context Access](AGENT-CONTEXT-ACCESS-POLICY.md)、[SAF](AGENT-SOCIAL-INFERENCE-SAFETY-CONTRACT-V4.md)、[AGA](AGENT-TO-AGENT-PERMISSION-CONTRACT-V4.md)。AGE 源 AGE-001–014、033–036、060–067、079 与 AIR 设计 §6–8 为增量输入；来源路径和哈希见 [材料接入记录](../research/BIRDTIE-V5-MATERIAL-INTAKE-2026-10-02.md)。

## 1 已有对象不等于 Agent Memory

| 当前真实对象 / 代码 | 已有语义 | 不可据此声称 |
| --- | --- | --- |
| `identity.Profile` / `postgres/profile_edit.go` | 本人编辑的displayName、bio、visibility和Profile访问边界；原源不复制，AGE003附加字段限制 | 本对象本身不提供PrivateAgentProfile、字段级记忆展示或永久分析许可 |
| AGE001 `agentprofile.Record` / 053 / `postgres/agent_profile.go` | 独立六字段metadata、typed owner与精确数据库绑定、正版本和内部Get/Ensure | Public/Private内容、真人授权gateway、Memory来源resolver、模型出口或认知端口开放 |
| AGE002 `agentprofile/private.go` / 054 / 本人Private store及HTTP | 九类本人直接输入、self查看/完整替换/清空；独立隔离本地验收已通过，见Profile规范§10.6 | 本对象本身不授权自动分析/Memory、认知Profile读取、模型或跨Agent处理 |
| AGE003 `agentprofile/visibility.go` / 055 / 受众store及HTTP | 11真实字段、五类受众、本人native CAS与原普通资料ACL相交的人类投影；历史及重开后2780完整Go/055全源补验见Profile规范§11 | 自动名字副本不是独立公开授权；人类读取不等于认知SourceState/ContextBundle，AGENT_ONLY不启用Runtime/Memory/provider |
| `content.Moment` / `postgres/moments.go` | 私人文字草稿、作者、明确实体链接、Revision、编辑/撤回 | 自动分析、公开动态、照片经历、Place Memory 或长期兴趣 |
| `contextgraph.DeclarationStore` / `postgres/person_contexts.go` | 本人当前/过去/目的地/线上等私密声明 | 自动当前位置、学历认证、成员资格、已实现 Context Builder |
| `relationshipcontext` / `newpeople` / Opportunity | 特定独立开关与授权下规则事实、匹配、中文理由 | learner、私聊语义推断、亲密度或持续 Memory |
| `agentruntime` SAF / AGA pure policy | 合成可信事实的默认拒绝与限定正例合同 | live grant resolver、推断生成/持久化、真实 ASK_USER 送达 |
| `consent_grants.resource_type` 预留 memory / agent_context | Foundation 枚举和受约束基础表 | 存在通用 Memory grant writer、Memory route 或可继承 profile_view |

旧架构概览中完整memories / agent_profiles目标语义及AIR源文“现有AGE服务”是规划描述；必须按上表实际代码适配。Phase0结束时无持久AgentProfile；AGE001现在的agent_profiles仅metadata，准确schema/store由 [Profile基础规范](AGENT-PROFILE-FOUNDATION-V5.md) 维护，不等于完整内容服务。Phase0纯类型和当前域窄桥不提供认知Memory读取/Context assembler/provider adapter；AGE004另有本人直接管理store，不由该窄桥开放新权限。

## 2 单一权威对象与逻辑合同

以下为**认知领域目标逻辑合同**，不表示完整对象已成为当前数据库列、HTTP JSON或可调用服务。AGE001metadata、AGE002Private人工内容和AGE003人类字段受众已分项增量实现，准确当前范围仅见Profile规范§9–11；下表的认知读取和候选合同仍待后续，人工Memory/Evidence当前范围见AGE004/005节，不能由人类API直接接AIR。命名与Phase0及现有领域对齐，避免重复建表/服务。

| 对象 | 最小责任与约束 | 权威归属 |
| --- | --- | --- |
| AgentReference | 稳定 Agent ID + typed account PrincipalRef / runtime Role；读取时核验真实 ownership；模型配置独立 | 复用 Identity / Agent ownership；Phase 0 已有引用类型不等于 ownership resolver |
| AgentProfile | Agent/owner、独立 profile revision、明确声明字段及各字段可见性；公开投影和私密资料隔离 | AGE Profile 目标服务，不能复制身份 |
| SourceReference / MemoryEvidence | typed owner、资源类型/稳定 ID、正 source version / current version、来源归属、用途、时效、删除/撤回与当前可读事实；观察时间与事件时间分开 | 源内容仍归原领域；AGE 管理证据链接与验证 |
| Observation | 有来源的非权威观察、subject attribution、敏感分类及明示 assessment 语义 | AIR 非权威处理结果，不能直接变 ACTIVE |
| MemoryCandidate | 候选 ID/revision、同 owner/Agent、predicate/value、来源版本引用/簇、性质、用途与期限、用户可见简短说明、审阅状态 | 单一候选暂存归属；由 AGE 接纳校验，AIR 不另建权威副本 |
| AgentMemory | 稳定 memory ID/revision、owner/Agent、类型/键、结构化值、状态/期限、明确或推断性质、当前证据链接和纠正记录 | AGE 唯一写入与读取服务 |
| MemoryView / ContextBundle | 当前任务 purpose、允许字段、来源版本/许可版本、有效期及预算的最小视图；不保证永久访问 | AGE / 原领域当前授权读取，AIR 只消费 |
| Approval / effect reference | 人对具体候选/action版本的批准与用途，和一次逻辑效果的键/结果分别记录 | 可信权限/写服务；不是模型输出 |

来源中的完整正文、私聊、坐标、媒体或秘密不默认复制到 Evidence/trace。稳定引用也有自己的 ACL；可解释性只给当下有权看到的短依据，不泄露不必要的第三方资料。派生缓存/索引不是另一真源，必须携带主体/源版本并可失效。

### 2.1 当前 Phase 0 共同合同的准确范围

实际文件为 `apps/api/internal/agentcognitive/contract.go`、`current_domains.go` 和 `unavailable.go`，不新增 HTTP、schema、模型出口、候选记录或 Memory 数据。

| 当前类型 / 接口 | 当前含义与硬边界 |
| --- | --- |
| `AgentReference{AgentID, Principal, Role}` | 模型/provider 不在身份中；只引用既有命名空间，结构合法不证明该 Agent 的精确所有权 |
| `SourceReference{Type, ID, Owner, Version}` | Type闭集，typed owner与正版本；现普通identity.Profile/Context窄读取没有认知版本，禁止补造Version=1；AGE001metadata版本不能代替这些对象版本 |
| `SourceState` / `BoundaryFacts` | 当前来源授权、CurrentVersion、期限、删源/撤回及主体/成员事实的服务器内部输入；JSON 编解码拒绝；当前无新认知 resolver，合成 fixture 不能接入实际路由 |
| `ReadRequest` | `agent-cognitive-boundary-v1`、request/task/Agent、purpose/scope/source 和至多 15 分钟有效期；只请求一个对象，不授予权限 |
| `Purpose` 闭集 | READ_AGENT_PROFILE、READ_MEMORY、SUBMIT_MEMORY_CANDIDATE、MODEL_CONTEXT_EGRESS、AGENT_COORDINATION、EXECUTE_ACTION；枚举存在不表示功能开放 |
| `DecideEligibility` | Personal 仅本人 PRIVATE，Organization 仅实时成员绑定的 WORKSPACE_PRIVATE 且不可读 USER_PROFILE/PERSON_CONTEXT；合法事实最后仍 UNAVAILABLE，非法/过期/跨主体/源变更等 DENIED；Business 未开放，CLOSE 默认拒绝 |
| `CurrentDomainStore` / `CurrentDomainAdapter` | 仅 Authenticate、HasActiveAgent、ReadProfile、ListOwnContextDeclarations 的原生本人窄只读桥；SessionAccess 拒 JSON，读前后重新解析会话/活跃 Agent，输出原类型；不装配模型 Context 或制造 source version |
| `MemoryReader` / `CandidateSubmitter` | 仅声明后续 ports，当前 `UnavailableCognitivePorts` 返回 ErrUnavailable 和空结果；不产生记忆、候选收据成功、批准或 effect |

Organization成员事实必须绑定当前真人、membership ID、同一组织account principal、正revision与current revision；成员身份或角色描述不能继承个人内容。HasActiveAgent的布尔值只证明该principal有活跃Agent，不足以填充某个精确AgentID的可信证明。上述Phase0合同与窄桥不是完整AgentProfile内容/Memory服务或生产授权。

### 2.2 AGE001 metadata增量不开放认知端口

当前AgentProfile基础表、六字段类型和内部Get/Ensure精确核验数据库Agent/owner及活跃状态；仅为metadata受信primitive，不核真人session/membership/purpose/consent，不直接接HTTP或AIR。历史Phase0没有持久Profile的结论保留作当时范围，当前不再描述为完全没有Profile基础表。

AGE002随后新增Person-only九字段Private内容和当前session/精确Personal Agent核验的本人编辑；AGE003再增11字段五类受众与人类当前投影，实际代码及各自正负本地结果见 [唯一Profile规范](AGENT-PROFILE-FOUNDATION-V5.md) §10–11。普通四字段Profile不joinPrivate，新projection同时核原资料ACL及字段overlay；owner编辑或人类投影均不是AIR来源/认知读取或分析同意，独立purpose/consent及Runtime读取仍待后续。

该增量不修改DecideEligibility、CurrentDomainAdapter或UnavailableCognitivePorts；合法认知请求仍Unavailable，Memory读写/候选/模型出口/动作/A2A不因metadata存在取得权限。正profile_version不填造普通内容的current source revision或通用来源/许可resolver。

AGE003复用native metadata版本管理Private/field policy写，不能替代普通UserProfile/Context全域源版本。field policy是持续受众选项，projection实时读取当前源与ACL；它不是具体内容外发批准。未来Memory/cognitive source及具体批准另绑定普通源真实updated_at/revision和当时独立许可，不填Version=1或将本metadata称通用source revision。

被拒绝的名字不能由handle或官方Person活动的自动名字副本重新带出；当前没有独立publicHandle许可，拒绝分支只用通用标签，允许分支才沿用原名字回退。自动副本需原Profile粗ACL与字段overlay同时允许，不把发布活动当作公开个人资料的新增同意；这些人类标签保护不构成Memory来源或认知读取授权，当前补验详见Profile规范§11。

## 3 权威读取和候选提交分开

未来 AGE 适配边界包含：当前授权的 Profile/Memory/Evidence 窄视图读取、候选提交与验证、人类审阅决定、纠正/删除/失效，以及 source/consent/policy revision 读取。AIR 只能调用这些内部边界，不能持有数据库写权限或绕过原领域 ACL。

当前可复用的实际接口是 `identity.AccessStore.Authenticate/ReadProfile`、`contextgraph.DeclarationStore.ListOwnContextDeclarations` 和服务器活跃 Agent 检查。仅本人当前会话的窄只读桥不应返回第三方 Profile、组织资料、关系正文或永久快照。缺精确 stable Agent ID resolver、Memory reader/writer、来源版本 resolver 或 consent resolver 时，对应能力为 unavailable / 默认拒绝，不能通过 caller 传 `verified=true` 补齐。

模型输入只含已最小化获准片段；模型/客户端给出的 owner、ACTIVE、currentVersion、confirmation 或 policy override 都不作为可信事实。内部可信来源事实不得作为可由 wire 自证的授权对象；实际 resolver 必须从权威记录重新读取。

PUBLIC/PRIVATE/WORKSPACE_PRIVATE 继续遵守原政策；有 scope 枚举不意味着已有 route。CONNECTION、CLOSE、COMMUNITY 或 AGENT_ONLY 若缺其明确语义、资源授权和当前 resolver，拒绝扩大读取；未定义 close friend 继续拒绝。普通 Profile 查看、新朋友匹配、本人关系信号、双方共享活动和 AGA 的同意均不升级为 Memory/模型出口/公开许可。

## 4 证据、归属与可信程度

- EXPLICIT 是本人的明确声明；INFERRED 是待审阅建议，不能以分数高或多次参与自动变成事实。RULE_FACT 只表达来源记录确有的事实，不把计数解释成兴趣。
- 身份、组织资格、经营权、参与/出席由各权威域判定；用户自由文字可纠正自身偏好，不能覆盖这些服务器事实。
- 源 subject、发布者、照片所有者和被摄者分开。报名不等于出席，Moment 的地点链接不等于本人到访，历史照片的 EXIF 不等于当前城市或本人经历。
- 同一次活动、同一条源、其重传/转发及多张相关媒体形成 source cluster，不能充作多份独立证据。来源独立、有效且获准也不自动证明偏好正确。
- assessment 必须声明 `UNCALIBRATED_SCORE`、`ORDINAL` 或有数据依据的 `CALIBRATED_PROBABILITY`；没有校准时不把 0.82 解释为正确概率。P0 长期候选均由用户审阅，高分不是免确认阈值。
- 当前 SAF 推断合同只允许限定的非敏感 ACTIVITY_CATEGORY 合成场景。健康、政治、民族、性格、关系强弱、个人精确位置等不因本 Memory 目标模型而获准推断；扩大类别要独立设计和安全验收。

## 5 独立同意与当前版本

原内容保存、私人 AI 分析、供应商出口、长期记忆保存、公开字段、跨 Agent 共享和外部动作分别记录适用用途。许可至少绑定 actor、owner/subject、Agent、资源与字段、接收方/目的地、task 或逻辑操作范围、当前正 revision、期限与撤销状态。内容上传、公开 Profile、好友、组织角色、autonomy level 都不等于其他用途的同意。

读取和处理快照不能成为永久许可证。恢复、重试、候选展示、最终确认和写事务均重读当前 owner/Agent、source version、ACL、consent/policy revision、成员资格和 Block。来源不明/跨主体/版本为零或不一致/过期/删除/撤回/未授权均拒绝。来源版本和许可版本分别记录，不用一个布尔值替代。

## 6 候选、记忆与删源生命周期

未来候选：`PROPOSED → VALIDATED → AWAITING_USER → ACCEPTED / REJECTED`；来源、版本或许可失效进入 `INVALIDATED`。`ACCEPTED` 是用户审阅决定，只有 AGE 在当前版本事务内校验成功后才能产生或更新 ACTIVE Memory。拒绝、返回或关闭不是确认；生成候选不写 ACTIVE。

权威 Memory 可有 ACTIVE、SUPERSEDED、EXPIRED、DELETED 等明确状态。具体 schema 和 transition 将在实现任务确定；源 AGE-007 的“分数上涨即可 ACTIVE”示例不能覆盖本期用户审阅和安全规则。明确声明也须履行保存动作及适用许可，不把所有私人输入永久化。

用户纠正、拒绝或删除保存可核验的最小控制记录，抑制旧候选/旧 worker 立即再推断恢复；不同新用途或新来源需要重新请求适用确认。删源/撤回/撤分析时先同步阻止新读取和新提交，再失效候选、MemoryEvidence、MemoryView、缓存/索引、任务/事件引用，异步清理保持明确可核验状态。

Memory 唯一 Evidence 已失效则该 Memory 不可继续用于检索；其他独立证据仍有效时由 AGE 重新判定，而非复用旧分数或批准。备份恢复和 job 重放先检查当前 tombstone/revision，不复活撤回资料。逻辑阻断与物理清理状态分开，未完成清理不声称全部删除；外部 provider 的保留能力须在 live 放行前明确，不承诺无法证明的即时全网删除。

## 7 确认、事务提交和未知效果

未来记忆写批准绑定当前候选/来源版本与完整 canonical digest、人类主体/Agent/purpose/目标/期限。批准不能在换 owner、改值、换来源、撤权或到期后继续使用；一次消费、防重写与权威 Memory 更新在可排序事务边界内落实。

AIR 真实工具动作另需单步 ActionProposal / Approval / dispatch / effect 合同。`action_digest` 绑定展示内容，`effect_key` 归并同一逻辑效果；不是同一个键，也不是一次 HTTP 请求 ID。提交与撤权原子排序，提交前撤权拒绝，已提交在飞动作则对账、停止后续，不伪称取消成功或盲重发。

当前通用批准消费、Memory写事务、effect ledger和executor尚未实现。现有直接人类按钮调用原域服务并不因为本节改成 AIR 委派或长期 Memory 写入。

## 8 角色、供应商与验收边界

Personal / Organization / Business 可共享类型和 engine，不能共享私人资源。组织/商家记忆只含其自己且明确获准用途的资料，成员私人 Memory 和临时活动许可不得转存。Business Agent 当前不可调用，Place/Venue/Community/CityContext 无认知 Agent。

离线 pure contract、fake provider、隔离库、本地 UI 与 live 模型出口分别留证。provider/model 可替换但 Agent ID/资源所有权不变。没有真实 provider 批准、数据目的地/范围/保留/预算和凭据时 live 禁用；普通活动、详情、报名、Plans 等原路径保持其原权限范围，不依赖 Memory 或模型可用。

后续验收覆盖同 owner 正例、跨主体与错类型、未知 scope / Close、每种同意不继承、来源版本与并发撤回、重复证据簇、确认前删源/改稿、纠正后防复活、恢复/索引/备份失效、确认一次消费与结果未知。接口/SQL/真实来源/UI 验收完成前仅标限定合同，不将纯测试正例称真实用户授权。

**Closed Pilot Ready：NO。** 本文没有新增生产身份、真实活动、生产地图/API、调度告警、值守或真实 A→H 证据，原发布门禁保留。

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

## AGE005 / AGE012 当前人工 Evidence 与来源解释（2026-10-03）

单一 `agentmemory.Evidence` / `057_agent_memory_evidence` 为本人 EXPLICIT Memory 提供版本绑定的原生来源引用；当前真实本人 Person/精确 Personal Agent/metadata/current session 经 Store 复核。人工 PUT/DELETE `/{memoryID}/evidence/{evidenceID}`、GET `/{memoryID}/provenance` 已接 `/v1/me/agent-memories`，沿用无缓存、主体限定与当前会话网关。input仅声明实际Memory期望版本/来源类别及地址，不能自证来源版本、许可、confirmed、权重或时间；Evidence UUID是稳定控制地址与幂等键。

支持本人私人 Moment、going RSVP参与记录、Place bookmark三源：真实 Moment revision，RSVP updated_at digest，bookmark created_at digest；digest复用064共同版本合同，以事务UTC固定数据库元数据语义。source owner/ID/version、Memory version、server observedAt与native eventTime分别保存；eventTime是对应记录的updated/created时间，不是到访、亲历或照片经历声明时间。signal仅MANUAL_REFERENCE、weight=1表示手动关联，不是概率或AI结论；不保存原文/媒体/坐标。

GET解释只写“本人明确填写”与“本人关联了N条私人记录/N次报名记录/N条收藏记录”，并返回当下仍有效的最小引用。HTTP再核固定说明/实际计数、schema、Memory/owner/Agent一致、重复源/证据及100条上限，避免后端自由解释透传。最初七个HTTP leaf反例真实暴露透传（8 fail事件含parent）；ValidateProvenance与handler修复后197个spy通过。

创建、CAS移除、并发同址重试和终态不可复活；同Memory版本的同源地址唯一。更改本人声明或删除Memory时在同一事务将引用清为REMOVED；失效源编辑/撤回、报名取消、资源隐藏/到期/屏蔽、收藏移除后当前读取停止计入，下一次本人provenance读取事务懒清引用，保留最小控制版本而清source/event/signal/weight。旧引用不复活；新版本关联要新Evidence地址。未读之前旧CURRENT行可能仍占唯一/容量，人工需要先刷新；没有后台删源/到期传播服务。

最终单条payload SQL snapshot重核所有来源当前版本、资源ACL、session/Memory有效期；实际锁等待跨Memory/session期限拒绝、零提交。只锁本人的源记录，不在Participation之后追加Activity锁而反转原生Join锁序；最后SQL另核活动/City/主办与Block。线性化点是最终SQL snapshot，不能承诺之后已传出的数据绝对即时撤回。

057严格三值逻辑形状、当前Memory绑定/版本、独立Evidence控制与清除；所有非空Evidence包括REMOVED的down均原子拒绝，仅自有父记录正常cascade后empty down/reapply。完整旧public行和nativeProfilev3/Memoryv2保留。SQL形状fixture的随机source ID不作真正来源证明；原生Store/已注册HTTP另用真实开发域动作验证。

正式证据：[113归档文件/14源与脚本hash、命令、原始失败和完整快照](../testing/evidence/agent-memory-evidence-2026-10-03/README.md)。057最终三轮完整Go各4837 PASS、0FAIL/测试SKIP、19无测试包单列；目标scope905、新Evidence域246/fullMemory域498、HTTP spy197；全vet/build0，58 SQL拒绝/5正shape、两非空down exit3、empty down/reapply、自有DB清理通过。另当前编译API ready200，三个evidence/provenance匿名401/no-store，PID/路径/SHA核对后已停自有API；此runtime轮未另跑完整Go。Store/API重组持久读取已测，不冒称手机或部署进程重启。

**完成范围：CODE_AND_LOCAL_VERIFICATION。** 所有当前Memory仍为本人明确填写；没有无来源AI推断结论的接纳入口。INFERRED候选审阅/接纳、分析用途resolver、认知MemoryReader、model gateway写入、自动学习和中文消费UI仍分别待后续。PRIVATE/AGENT_ONLY人类管理不授机器许可。Windows CGO0缺gcc未跑race；100条满容量清理、所有第三方ACL竞态、视觉/辅助技术和现实试点未验。原IdP/HTTPS、已授权现实组织活动、生产地图、部署日志、值守/备用支持、提醒运营与A→H仍缺，**Closed Pilot / Consumer Beta NO**。


## 2026-10-06 AIR024 声明时间与证据投影

原本人 Memory Detail GET 在原 proof 与最后当前 revalidation 内附加只读字段证据；原 Record/Evidence writer、具体版本、纠正/丢弃/来源失效及主体边界不变。规范见 [字段证据](AGENT-FIELD-EVIDENCE-V5.md)。声明创建/更新时间、有效期、本次观察采集与未知拍摄时间分开；explicit 1不是正确概率，pending inference只候选。结构化活动偏好相反时保留来源并待确认，不自动覆盖 Profile/Memory。身份/地理声明不是域事实；真实照片/外部来源消费与 native DB 验收仍未完成。

## 2026-10-07：已提交 Memory 来源的有限失效接线（AIR020）

即时拒绝仍由原 076/current source 校验执行。新增 101 同事务 AFTER INSERT/UPDATE metadata hook 覆盖原共享 Memory 人类写者（EXPLICIT ACTIVE 或 DELETED，版本变化）；不读取正文、structuredValue、照片或 GPS，不新增自动 Memory 写入。旧 writer/source/批准与 001–100 未改。

`MEMORY_UPDATED` 只删除精确 owner/agent/source version 或 xmin 已变、且没有 binding 的 ContextPurpose preview。完成后同事务唯一 child 只从原 binding 找原 `consent_grants` 中本人收件、本资源、TASK_CONTEXT_READ/read 且未撤销许可，原 revision CAS+1 作不可逆撤销；保留批准、绑定、已消费历史。不是撤回以前读过的内容，也不撤销未知形状的任意 grant。已知合法 076 来源范围之外的异常/损坏历史不声称全部清理。revision 耗尽仍出现在 more 查询中，预算耗尽将未完成显式停下。

父子共享六次 claim/最多深度一、短原来源期限不延长、pending 不当最终成功，history/replay 不赋予当前访问。没有把 094 correction pending/committed 记录删除，也没有持久 Planner/ModelRun 提案存储被假造为已失效；这些路径继续原 fresh binding 与具体版本批准。

证据只相关 unit/Tx spy/UNIT_STATIC SQL。捕获触发器实际运行、原生许可撤销/数据保留、PG 锁/并发、迁移与重启均 NOT_RUN；没有真实媒体解析、模型或生产验收。整体 AIR020 保留 PARTIAL。证据在 `docs/testing/evidence/memory-causal-invalidation-2026-10-07/`。


## 2026-10-07：PreferenceUpdated 的有限元数据失效链（AIR020）

- 102 仅捕获原本人私人偏好 writer 的 native 变更元数据。已设置来源使用 `written_profile_version` 与私人行 `xmin`；清空使用已递增的 `agent_profiles.profile_version`、该行 `xmin` 和私人行实际不存在。source ID 为原 personal Agent ID。事件不存字段正文，不推断 RSVP、到访或概率。
- `preference-invalidation-v1` 由原 `agent-outbox-control` 和 Store 的原 claim/consume 入口消费：根阶段有界删除最多 128 个未绑定的旧 `PURPOSE_PRIVATE_PROFILE` 预览；同事务完成回执只能派生唯一 depth=1 子事件，子阶段对原 `consent_grants` 中本人 `TASK_CONTEXT_READ/read/self` 许可做最多 100 个版本 CAS 撤销。不删除绑定、已消费或已发出历史，不声称召回内容，不新增批准权源。
- 复用原 control 的 global=4、owner=1、256 tenant-head 上限。仅先提名，原 native metadata → owner 76033 → root 76034 → 当前私人来源 → outbox 锁序；最后复验原来源、held row/fence CAS、原生 clock 和期限。`CheckFence(c,c)` 是形状/TTL 检查，本身不等于 native fence 回读。
- 同根/子/重试总 attempt≤6，depth≤1，15 分钟源 TTL、30 秒以内 lease；未完成残余保持 PENDING，预算耗尽保留 DEAD_LETTER，revision 耗尽许可仍出现在 more 检查，不能假称全部清完。未知 commit 不交出新 claim/receipt，runner 停止，不自动重发。
- 固定 5 秒内只合并同 owner/Agent/source 的较低版本 configured 根事件，且必须 PENDING、attempt/fence=0、无 lease、无任何 inbox；旧事件仅置 INVALIDATED、保留原身份/来源/root/times。clear、child、已领取与已有回执不合并。此进度不是批准或授权。
- 本切片验证只含直接 Go 单元与 `UNIT_STATIC` 102 合同检查，Tx spy 调用真实 Store helper但不执行 SQL。102 up/down、PG 锁序/并发/隔离/native 时间/真实提交、生产与手机均 NOT_RUN。down 源码拒绝已有 Preference 事件/进度/回执。AIR020 总体仍 PARTIAL，13 事件通用认知、自反思/模型与外部消费者未完成；原 Moment 082/091、Memory 101 的已封存分支/证据不被本切片替代。
