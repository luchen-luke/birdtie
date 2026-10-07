# 2026-10-06 AIR023 当前上下文权限复核（本地定向已通过，根独立整仓待核）

本节是当前增量，下面原文完整保留。队列状态由根代理维护，本文不宣称 DONE、真实模型供应商或发布可用。

- 原 Personal Builder 继续使用既有 Service seal、TASK_CONTEXT_READ grant ID/revision、Source 的实际版本/xmin、Policy family/native_revision和原最短租期。缓存命中须 `RevalidateOwn`，控制对象不能从 JSON、同名主体或另一 Service 重建。未新增 grant/epoch/source/effect ledger 或缓存。
- 组织原 Task GET/List/POST 使用本次服务端 opaque snapshot，原成员角色、组织/账号、OrgAgent、稳定 Session身份与 Task xmin 均来自 native SQL；正常 idle 更新时间不是撤权世代。组织 City 查询先走原公开资源 ACL，不继承管理员 Personal 私密活动、邀请、Memory 或模型许可。
- POST 查询前绑定此次城市依赖；原 SaveTask/UpdateTask复用同事务 helper。只有本次合法自身 Task 写入推进对应 xmin，原来源/authority/期限保持。原 Task锁后、audit 后及编码/商业投影后再核当前 source和身份，不以晚读的新版本吸收外部 ABA。
- 当前 native snapshot 是不可序列化的本次读取凭据，不是通用执行批准。公开来源依赖只使用内部 ID/xmin/native已存在 source证明；不把私密正文放 wire。城市依赖保守拒绝同城相关来源变化，不是精确缓存命中优化。
- Session absolute/idle、公开来源自然期限与30秒本次组织上限取最短。每个成功PG观察通过查询前 monotonic锚只能收紧，正常刷新不能延长旧快照；SQL/Commit等待后仍检查。最后 native SQL是持锁线性化点，不承诺 Commit后到HTTP送达间任意瞬时撤权能撤回已发数据。
- 原 Session revoke API单调撤销；本轮不声称识别任意管理员将所有安全字节复原的非法操作。Org/Account/Agent/Task/source合法原行 ABA则以具体 xmin拒绝。
- 模型/自动写/Vision/A2A默认OFF。Org私密模型出口仍UNAVAILABLE。手机、AT、race/生产未运行，Closed Pilot/Consumer Beta NO。

已执行定向：native1成员撤权等待真实200 RED；native6观察短Session期限后续刷新复活RED；native8查询后来源ABA RED；native10旧Task等待ABA RED；native12组织继承Personal私密来源及新writer合法成员兼容RED。对应 native3/7/9/11/13修复帧各有原raw；native13 26PASS，5命令0，986执行输入稳定，旧完整public rows/xmin/catalog保持且自有库删除。native12 City自然到期已正确拒绝，仅错误期望类型属HARNESS；编译/路径/SQL诊断单列而不冒充业务RED。

最终 native15 已107PASS/0FAIL-SKIP/pkg、test/vet/build与两CLI全部0；986固定whole095基线加本轮8Go覆盖执行输入/live前后稳定。含原Builder/Purpose、真实registered responseSafety、Org source/Task/角色/Session及audit等待回滚；主库和17实际HTTP子库已DROP并SQL确认不存在。native14原97PASS/2FAIL保留：300ms fixture在目标读取前已经自然过期，属于HARNESS时序；native15使用固定合法4秒原期限并真实等待4.2秒，未放宽拒绝或续期条件。旧menu纯fixture缺native端口的同SHA baseline0→current1证据保留，仅明确OFFLINE_TEST_ONLY测试adapter恢复原断言；生产缺port503与不可wire构造权威另有真实负例。根独立整仓尚待核，不能把定向结果宣称当前整仓通过。证据目录 `work/v5-air023-context-authority`；本节不使用旧whole095 11126冒充本轮新增源码全仓通过。

## 以下为此前已核证历史正文（完整保留）

# Agent Context Builder V5

2026-10-03。BT-V5-AGE-033 唯一增量规范。来源为 `docs/product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md:1231`；复用认知 ADR、Context Access Policy、Current Context、Private Memory Isolation 与 Policy APIs。扫描未发现同职责已存在的 Builder canonical。未建立第二本 Memory、Policy 或 Conversation 账本。

## 已实现范围与未完成验收

`agentcontextbuilder.Service` 真实调用 PostgreSQL 窄 Store；现有 `/v1/cities/{cityID}/agent/tasks` 的本人活动、地点、比较及活动 follow-up 路径真实构造并消费结果。输入包含精确 Personal Agent、当前本人 Session/typed workspace、原生 ACTIVE Task、当前 query、真实 Task updated_at 及有界选择；输出包含当前公开供给的最少字段、真实源版本、观察时刻与有限读取租期。

**当前 AGE-033 恢复实现已通过 CODE_AND_LOCAL_VERIFICATION**：公开规则与本人审阅沿用原范围；新增 MACHINE_TASK_CONTEXT 本进程最小只读用途，明确选择 Profile/Memory/Relationship/City/Place/Task 与 Policy，由原生具体预览、批准和 consent_grants 唯一生命周期控制。它不授权模型出口、候选、永久 Memory、自动操作、组织或 Business 跨主体认知。恢复前证据的 PARTIAL 说明作为历史保留，不能替代当前真实验收。

ModelAccess 始终 UNAVAILABLE；MemoryPromotionAllowed=false。本次具体批准只授予本地 TASK_CONTEXT_READ；没有模型出网、推理、后台分析、自动写或 Run/Step 历史，也没有真实试点证据。Closed Pilot / Consumer Beta 仍 NO。机器用途 resolver、当前源/字段/Agent/Task/期限与撤权、Runtime 消费已由本次恢复实现；034 relevance、035 budget、AIR016 ledger 和模型出口属于各自任务。

## 三种闭集读取用途

| 模式 | 选择 | 数据与边界 |
|---|---|---|
| RULES_PUBLIC_QUERY | ACTIVITY_SEARCH / PLACE_SEARCH | 活动至多30、地点至多100；真实 current Person/native Task/City/Session；PRIVATE selector 在 native 端口读取前直接拒绝 |
| HUMAN_SELF_REVIEW | EXACT_SELF_REVIEW | 明确选取本人字段/Memory ID/Policy family；没有 query/Task/public-ID 混入；本人查看不授认知或外发权 |
| MACHINE_TASK_CONTEXT | EXACT_TASK_CONTEXT | 原生具体当前任务批准；最多3 Profile/3 Memory/3 Tie/1 Policy/5 Place/5 Activity，固定 City/Task；本地只读，无模型或动作授权 |

活动 Bundle 只含 ID、标题、分类、时间、原生 typed organizer；地点只含 ID、名称、分类。不默认复制描述、摘要、坐标、私人偏好、历史 Conversation 或其他 Memory。原规则检索授权可见的 invite-only/member-visible 结果继续保留在原人类领域 DTO，用途独立，**不加入 PUBLIC Bundle**。Builder 返回的公开领域 DTO替换旧查询结果，实际影响最终中文回答和实体内容；不是一次未被消费的空调用。

Request 和 BuiltContext 是 server-only 控制对象，JSON 编解码均拒绝；Bundle 可供内部/人类查看，但不是许可对象。原公开 Runtime bridge 从已注入的 `s.catalog` 窄能力构造服务，不新增 Server 字段、构造器或默认 provider；本次具体用途另有下述五个明确本人路由。实际能力缺失时本人检索安全503，不按 owner ID 回退。匿名和组织保持原合法检索路径，不继承 Person 私密 Context。

## 当前身份、源与时间

- PostgreSQL transaction 明确 READ COMMITTED，内部 SET LOCAL TIME ZONE UTC；不继承 pool 默认 RR。
- 锁序 Account → Session → exact active Personal Agent/metadata → City/Task → 本次源。缺 metadata、错误主体/Agent、停用、撤销和过期拒绝；不读时补造 metadata。
- Task 必须本人当前 ACTIVE，typed City 一致，CurrentQuery 等于真实持久 filters.currentQuery（旧行无值才用原 query），TaskUpdatedAt 等于实际原生行。原历史 query 不能代替最新 follow-up。
- 公开源必须当前 published/public，City active且未过期；取消/结束/过期活动、隐藏/过期地点、当前 Block 拒绝。最终单条当前SQL再检查权威源与 Block 等新插入关系；保留行锁不被误称可阻止 phantom ACL。
- PUBLIC_CITY / CURRENT_TASK_REQUEST / PUBLIC_ACTIVITY / PUBLIC_PLACE 使用本包闭集 opaque UPDATED_AT_DIGEST。基于实际 UTC 时间、最小当前内容与真实 xmin 摘要，不冒充单调 revision、CAS 或许可。不会扩全局 event catalog、伪造 version=1。
- Human Profile 用真实 written_profile_version，Memory 用真实 version，Policy 用真实 native_revision；均绑定实际 xmin RowToken，不能用 metadata 当前版本代替内容版本。无配置明确 UNCONFIGURED，不借默认版本恢复数据。
- 观察时刻来自当前 PG clock。最终读取所有可能阻塞源后再查 PG clock；未来原生源时间拒绝。租期取 Session/request/source期限和5分钟最小上界，不给原生 Participation/Task 补写 expiry。
- 同一 Service 的 HMAC 绑定选择、当前数据、实际 authority 与期限。Revalidate 重新解析实际 native 当前行，比较 authority/source/version，保持旧 ExpiresAt、不续租。Account/Agent停用恢复、metadata重建、源变更、跨会话/主体、篡改和重建服务不能恢复旧控制对象。

这证明 Builder 的读取边界，**不宣称原 Task Save/Update 或所有旧领域接口已变成同一授权事务**；旧人类结果的字段投影仍遵循原领域权限。没有把本次读取租期转化成提交或执行许可。

## 验证与 UX

实际命令、首次失败、原始 JSONL、旧非空完整 public 行、生产源码 SHA 和自有库清理由本项 [证据](../testing/evidence/agent-context-builder-2026-10-03/README.md) 记录。目标纯测试明确使用 offline synthetic transport，仅 native PG + registered HTTP 测试证明当前数据库授权/数据消费；本地合成供给不是 CSSA 或生产数据。

UX-CHECK-02、04、06、08、09、10、12、16：复用当前请求与领域实体，回答/错误为中文，未知和过期明确拒绝，换主体/撤权和迟到读取不借旧数据；不添加搜索确认仪式或将控制对象称为批准。该项没有 Client 源码更改；真机、移动端、辅助技术和截图本项未测。Flutter及全 Go 共同回归由根代理实际统一验收，不能借070旧结果替代033。

## 2026-10-04 原任务恢复：本地具体用途与六源

### 实际读取与许可

- `MACHINE_TASK_CONTEXT / EXACT_TASK_CONTEXT` 只读本次当前 Personal Agent/native ACTIVE Task。当前 query、updated_at、City、Source ID、字段与实际版本精确绑定；Task 更新后不能沿用原批准。
- 076复用原 `consent_grants` 唯一 revision/expiry/revoked；immutable Preview 只存选择ID、queryDigest、原生版本和身份摘要，binding仅 grantID→previewID，不复制权威状态或源正文。旧 profile_view、开关、human seal 和无binding许可均不授新用途。
- Human `PurposeReview` 展示同一 native transaction 中的具体所选值；不是 Bundle、控制对象或授权，正文不另存。具体 previewID 显式批准；同preview结果未知后重试核实原grant、不新增或续期。撤回以原grantID/expectedRevision幂等恢复原终态。
- 原Profile最多3键、Memory3条、Tie3条、Policy1类、公开Place/Activity各5条；City和当前Task为必需锚点。无关历史、私聊、描述和坐标不加入。关系仅原accepted好友元数据，不等于亲密、兴趣或出席；公开City/Place不等于居住或到访。
- Profile真实written_profile_version、Memory真实version、Policy真实native_revision及xmin；其余Source取实际当前最小投影/updated_at/xmin摘要，不填version=1。
- Request/BuiltContext/PurposeCapture/PurposeResolution仍server-only；Service HMAC绑定新增grant、原purpose期限和Tie选择。native Build先resolve、组装最少领域payload，再在同transaction最终重读grant/source/phantom Block与时限。Runtime实际消费所选字段、Memory summary、Policy设置、City时区、Task query、Place/Activity/Tie值，回应生成后再次sealed native Revalidate；无UpdateTask/消息/通知/Memory写。
- Session稳定身份含原ID/digest/创建时刻/authMethod/absoluteExpiry；正常idle续期不错误撤销批准。不同Session和当前真实撤销/到期拒绝；Account/Agent/metadata/Task/source版本ABA不能恢复旧批准。不存在的Session恢复API不被假设为能力，任意数据库管理员原样恢复Session的场景未声称覆盖。

### 2026-10-05 AGE-035 原生 confidence 元数据

`ReviewMemory.Confidence` 为可选 `agentconfidence.Assessment`；实际 reader 在原 HUMAN/具体 MACHINE 共用的所选 Memory 读取处，从同一行 `scanAgentMemory` 已核验的实际 `memory.Confidence` 生成 `DIRECT_DECLARATION` 评估。原 ACTIVE/EXPLICIT/PRIVATE/版本/时效/主体约束保持，Native confidence=1仅标记本人明确声明，不是正确概率。不存在的元数据、旧纯合同 fixture 或 Profile/公开来源不填默认1；提供非 DIRECT_DECLARATION 或非法数值即拒绝。

来源映射保持 `HUMAN_EXPLICIT_MEMORY / PURPOSE_EXPLICIT_MEMORY` 原ID、实际revision、NativeTime与内部xmin；confidence 不独立创造 source/ref、用途或新的授权。没有列变更，062及088等原预算/Run结构不修改。

完整 Bundle JSON 已被原 Service HMAC seal 及深复制覆盖，新增字段使用同一 seal；删除/修改 confidence 不能绕过。实际 Runtime 仍在 AGE034相关投影与既有 Adapter 的 [唯一预算规则](AGENT-CONTEXT-ADAPTER-V5.md) 后，以原完整 Request/所有原选择源作最终 Revalidate；筛出项变化、ABA、撤权或等待后到期同样拒绝。没有新的 SQL 查询集、用途批准、DDL、Memory 服务或模型调用。

本项 native3 定向468PASS/0FAIL-SKIP，涵盖真实原生 confidence=row、删除元数据seal拒绝、省略源Memory/Tie/Account ABA、撤权/跨账号和实际锁等待后Session到期；原注册HTTP用途正负及最终pool等待原场景同时执行。完整数据/catalog/xmin保留和冻结来源见 [证据](../testing/evidence/agent-context-budget-2026-10-05/README.md)，root whole独立负责。客户端/模型/生产未启用，ClosedPilot/Beta NO。

### 原预览期限保持

末轮真实审查发现：批准时Authenticate延长idle可能使新grant晚于原预览显示到期。新native RED实际复现chosen10m与原Preview5m、原idle2m→正常Authenticate→Approve4m两种情形（0PASS/3FAIL连父）。已修为批准有效期同时取原Preview.expiresAt最小值；原选择deadline仍用于精确绑定，**实际许可最多5分钟且不晚于原预览或任一源/会话期限**。原grant时间不可延长，撤回不可恢复；不是用放宽断言消除失败。

### 实际 HTTP 与消费

| 路由 | 本人具体操作 |
| --- | --- |
| POST `/v1/me/agent-context/previews` | 完整12键明确选择，返回具体human Review与源版本 |
| POST `/v1/me/agent-context/approvals` | 仅previewId批准具体版本 |
| GET `/v1/me/agent-context/grants/{grantID}` | 无body/query，核实当前原许可 |
| DELETE 同grant路径 | expectedRevision显式撤回/同键核实 |
| POST `/v1/me/agent-context/runtime` | 仅grantId，真实本地确定性读取与消费 |

接口拒RawQuery/ForceQuery、Org workspace、未知/重复/null/大小写别名键、伪owner/authority/confirmed。过期请求deadline为409，当前源/权限失效为403。响应ModelAccess始终UNAVAILABLE、MemoryPromotionAllowed=false，内部RowToken不进入Runtime回应。

### 最终仓库核证与边界

独立HTTP native6 51PASS/0FAIL-SKIP；原native3及根target3各307PASS；期限修复后worker native5 310PASS/0FAIL-SKIP，680源稳定，076旧非空全public/catalog与empty/unconsumed Preview down/reapply一致、批准/撤回history down原子拒、自有DB已删。worker-final1历史与final2修复帧分别保留。根 whole-go076-3 最终默认并发 `go test ./... -count=1 -json` 实际8945PASS/0FAIL-SKIP/pkgFail，Go vet/build exit0、680源码稳定、完整public/catalog保持、自有库已删除。根再次核对worker-final2的74个实际文件字节/SHA及六份当前源码。whole1旧063 migration死锁与source变化、whole2旧CHT双clock fixture违反idle<=absolute均保留失败日志；后者改用单MATERIALIZED时间戳，原400ms期限、550ms真实等待及拒绝/零消息断言不变，最终全量包含该用例。首次fixture/noop/CAS/状态/时钟错误与真实期限缺陷分别记录。

客户端源未改；本轮统一Flutter analyze/test/DebugAPK build实际0，623功能+86loading/0FAIL-SKIP、199源稳定。现新增本人用途为后端CODE_LOCAL，不代表已有人类移动端批准界面、真实用户验收或模型服务。UX-CHECK-02/04/06/08/09/10/12/16适用；真机、TalkBack与生产门槛独立，Closed Pilot/Consumer Beta NO。后续AGE034相关性、AIR022适配按各自任务接续；AGE035原source仅四项本轮投影控制（limit、priority、confidence threshold、recency weighting），其定向核证见上文。原request/task/owner/global四层及全Run预算属独立AIR011真源，本项不复制或重新扩大它的范围；后续模型出口仍按原任务与发布门槛推进。


## 2026-10-06 AIR023 当前操作者与最终全量核验（root38rl）

当前任务已自然完成其原 CODE_AND_LOCAL_VERIFICATION 范围：986冻结输入，Go全量11157 RUN/PASS、0FAIL/SKIP/pkgFail，test/vet/build/两CLI均exit0。977个原981输入字节完全保持，原实现3项与会话夹具1项修改，另5新增；初轮11154/旧11126/定向123/失败轮已通过11155的所有分支multiplicity均核对，仅随机UUID规范。当前成员B合法200审计写给creatorA的真实RED已修：UPDATE审计使用当前服务端授权成员，原task creator和CREATE审计不改；伪主体400/撤权403零副作用。先前11155PASS/2FAIL保留，旧会话夹具双volatile时间违反idle<=expires，只改同statement时间，权限断言和生产认证策略不改。初 verifier 旧输入计数及猜测audit文字错误只修核验器，不改测试。

094/095 fresh/current全140表rows/xmin/catalog、unused down/reapply保持；388个实际raw HTTP子库及父库独立SQL不存在。最终1314文件逐SHA与zip复核，证据 docs/testing/evidence/agent-context-authority-2026-10-06/final-root38rk/README.md，独立核验 work/v5-age038-resume/whole023-fixture-independent38rj/result.json。模型/自动写/Vision/A2A保持OFF、Org私密模型口仍Unavailable；CGO0无GCC raceNOT_RUN。手机原349Profile/baseline981API，当前986未真机或生产IdP验收。

原255项只AIR023状态及证据改变，其余254整对象、原DONE、两录屏lease保留；当前171DONE/2IN_PROGRESS/8PARTIAL/60TODO/14BLOCKED。录屏修复继续，S4局部50PASS及148证据文件已根核，原创建API持久operation key/source409回执关联是仓库内部差距，不能称外部条件或完成功能。Now无目录底图解耦/几何仍实施，联合whole/build/device待当前源码安全冻结。ClosedPilot/ConsumerBetaNO，不以本轮GoPASS宣称产品可用。


## 2026-10-07：Rules 结果字段说明的实际响应消费

AIR024 公开结果消费在原 HTTP `prepareNativeAgentResults` 的此次最终 native Receipt 上复用 `PublicVersion` 和每对象6/2字段的 `BuildFieldEvidenceSet`。此处只是已有结果的附加说明，不调用 MACHINE_TASK_CONTEXT 适配器、不申请 TASK_CONTEXT_READ/MODEL_EGRESS，也不读取旧 ACTIVE Context 或新增私有来源。原 ContextBuilder RulesPublicQuery nil 字段说明与64KiB、30活动/100地点规则保持；原模型/媒体访问仍 UNAVAILABLE。

`WithContract` 只能重建同 owner/Task 版本和当前引用集合的说明，随后原 native/current-policy response fence 继续拒绝撤权、会话失效、源变更或迟到响应。附加 wrapper 单独按现有 UTF8_JSON_BYTES_V1 默认16KiB/100claims限额整组省略，不改变领域结果。源过期/时间未采集明确 unavailable/unknown，不把来源有效期续到读 lease，不补身份/事实/概率；原领域允许显示的过期来源仍可保持原 freshness。

相关单元命令、初次失败及原 registered HTTP 合成响应在 `docs/testing/evidence/public-rules-field-evidence-2026-10-07`。本轮没有执行 PG、生产身份、真实公开供给、媒体、公开消费 UI、真机或全面检查，整项 AIR024 仍 PARTIAL；适用 UX-CHECK-06/08/10/16。


## 2026-10-07：本人精确多来源审阅的实际读取接口

原 AIR024 增量接入 `POST /v1/me/agent-context/self-review`，只接受三个非 null 数组 `profileFields`、`memoryIds`、`policyFamilies`，分别至多3、3、1项；只选既有封闭字段、本人 Memory ID 和政策族，合计不可为空。8192字节正文、JSON MIME、重复/未知键、查询串和组织工作区继续拒绝。Agent、Person/Session、请求标识与最长2分钟截止由原服务端身份解析/Resolve 决定，客户端不能传 owner、Task、grant、批准或期限。

该接口直接复用原 HUMAN_SELF_REVIEW / EXACT_SELF_REVIEW 的同主体 Store、Service.Build seal 和 Service.RevalidateOwn；不调用 purpose-preview、Task grant 或 MACHINE_TASK_CONTEXT。专用 human DTO 只投影所选 Profile 值、Memory 摘要/版本/已知时间与置信度、政策 Record 和同一快照字段证据。完整 `data` wrapper（含末尾换行）先编码并检查64KiB，再用同一 Service 和原 Access 执行最终重验、检查取消，之后才输出任何私人数据。原 source/version/xmin/Authority/native clock/Session/期限判断全部保留；读取租期可被原生 Session、Memory 或政策期限裁短，最后重验不续租。

不序列化 BuiltContext、RawBundle、Request、Access、SessionDigest、RowToken、Authority、seal、MemoryKey 或隐藏 StructuredValue。旧端口缺字段说明时仅从这份已校验同体快照派生，原 sealed 对象不变。UNCONFIGURED/NOT_REQUESTED 保持原语义，未配置政策的 LEVEL_0_OBSERVE 不是访问批准。DTO 与原读取之间不共用可变指针。

直接注册路由缺失的404为有效初始 RED。相关 HTTP 55、DTO 12及此前精确原冲突控制1个父子测试事件通过（去重68、12顶层）；HTTP/DTO最终两命令 CGO_ENABLED=0、-vet=off、-count=1，未重跑整仓。新夹具纳秒时钟不满足原政策微秒契约及 Authority canary误匹配合法 grantsAuthority 键的失败历史保留，只修新夹具，不放松原校验。实际注册 transport 使用合成 Store，非原生PG/身份/权限验收。前端人工审阅、真实PG/并发/撤权时序、媒体、模型、真机、整仓/构建及发布均 NOT_RUN，整个 AIR024 仍 PARTIAL。证据见 `docs/testing/evidence/human-self-review-field-evidence-2026-10-07`；适用 UX-CHECK-05/06/08/10/16 仅后端覆盖。


## 2026-10-07：本次登录会话的任务资料许可管理

原 AGE049 的相关个性化控制复用既有 TASK_CONTEXT_READ，而不是新增未在原文定义的全局 Personalization Boolean。设置新增“本次会话的任务资料许可”，读取 `GET /v1/me/agent-context/grants` 的独立人类元数据投影。此读取只限当前 Person、当前唯一 Personal Agent 和原批准绑定的同一 Session；不列出其它登录记录，不扩大原五个用途接口或授予任何新许可。

原 076 表与 ContextBuilder Account→Session→Agent 当前绑定复用。单条原生 materialized clock 判定读取时未撤回且未过期，按此状态、创建时间、ID 优先排序，最多50条并读取第51条作为截断标记；包括哨兵的全部元数据须校验。读取视图最长30秒且受 Session 剩余期限限制，最终原身份/期限回校后才提交，HTTP 编码后再次核当前人类 Session 和取消。仅投影具体许可 ID/版本、任务/城市引用与版本时间、明确选择的字段或记录 ID、创建/到期/撤回时间；不导出 queryDigest、查询正文、审阅正文、SessionDigest、Authority、RowToken 或内部 source handles。清单不宣称当前原来源仍可供机器读取。

客户端先显示具体范围、已知时间及读取时状态，用户检查一个具体版本后明确调用原 DELETE grant 路径与 expectedRevision，再刷新。原 by-ID GET 会重验 Task/来源，来源改变或过期可导致拒绝；原 RevokeOwnContextPurpose 只要求原当前主体/Session/绑定与 revision，允许撤回过期或来源已失效的许可。因此原来源 GET 成功不是人类撤回前提，新鲜本会话清单可以供具体版本检查，最终删除仍以原后端权威 CAS 为准。没有放宽跨 Session 操作，也没有 source 读取的匿名或模型旁路。

写请求已发送后响应丢失、409或其它拒绝均不借旧检查重发；保留未知引用，只通过原 GET 或重新读取清单核当前状态。清单缺失或截断、GET 拒绝都不表示之前写入未生效；看到当前已撤回不声称是先前那次请求的因果回执。身份/组织/入口/传输来源退休后旧视图不可发新 wire 或复活旧批准，已发请求不被描述为撤销。关页重开只读当前许可状态，不提供跨重启的精确操作回执恢复。许可创建/新预览批准界面仍未接入，本页不伪造授予能力；撤回不删除原资料或独立 Memory。

仅本需求新增相关单位执行：Go 三包 `^TestTaskContextInventory` 7顶层/31含子测试事件通过；Flutter 新三文件阶段11通过加1夹具错误，该错误仅把设置页合法后台 GET 误计为许可请求，修正后单独受影响1条通过，共12 unique行为。初始 registered route 404、实际 Settings 缺入口的 RED，以及新增 API 环境常量编译错误全部保留。Postgres 使用 SQL spy/静态断言、HTTP 使用原注册服务与合成 Store，均不是原生数据库、真实身份或实际权限撤销验收。最后只修新夹具，没有重复全面测试。

适用 UX-CHECK-01/05/06/07/08/09/10/12/13/16，只证明对应本地读取、具体版本检查、恢复与来源隔离子场景。原生 PG/迁移、真实认证与 Session 生命周期、设备/TalkBack/性能、构建和整仓检查均 NOT_RUN；模型、分析、自动写与 Vision 不启用，完整 AGE049 仍 PARTIAL，Closed Pilot/Consumer Beta NO。证据 `docs/testing/evidence/task-context-privacy-control-2026-10-07`。


## 2026-10-07 AGE049 源码与原生本地验收收口

原五族隐私控制复用现有真实消费链；两种新增库存实际PG与原注册HTTP补证：原授权ID/版本、跨主体与两种会话边界、来源改变仍可明确撤回、52条有效优先/50截断及零额外领域行动已验。两条接口“编码后Agent已退役仍200”实际RED已修为复用原当前Agent解析，退休403、缺能力503、正常刷新200。最终四包96父子事件通过（8新native顶层/21父子/18叶场景），0失败/跳过/exit0；001–104/3开发seed及自有fixture清理退出0。此前五族原生命周期四个native顶层的PASS与未变源码逐个核实；不是新增全局开关/训练/权限真源。证据 [AGE049原生检查点](../../work/v5-age038-resume/age049-inventory-native-2026-10-07/README.md)。

原049仅 CODE_AND_LOCAL_VERIFICATION 标DONE；真实IdP、认证隐私界面真机、AT/性能及生产许可仍未验，旧历史原文保留，Pilot/Beta NO。模型出网/真实Agent写/Vision/A2A关闭。新当前07全量Go已在独立库运行，尚未完成，不提前沿用旧全量PASS；Flutter源未变，上轮2836与Debug真机包维持其原版本范围，analyze8warning+279info仍exit1。

队列256：{"DONE": 179, "BLOCKED": 14, "TODO": 49, "PARTIAL": 13, "IN_PROGRESS": 1}；P0 {"DONE": 121, "BLOCKED": 9, "PARTIAL": 7, "TODO": 13}。已立即显式接续原AIR019 Settings定时汇总计划父来源寿命核验：先标准弹窗点击复现，确认违约才修，不改历史DONE、不加重复任务或跳provider/PostPilotgate。
