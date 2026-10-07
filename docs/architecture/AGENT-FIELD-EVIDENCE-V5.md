# Agent 字段证据与冲突投影

2026-10-06，BT-V5-AIR-024。来源：[AIR 原需求](../product/BT-V5-AIR-REQUIREMENTS.md#bt-v5-air-024-来源证据冲突与事实时间模型)、[认知 ADR](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md)、[Memory 权威边界](AGENT-MEMORY-ARCHITECTURE.md)。本文件补充原体系，不建立第二个 Memory、权限或事实账本。

## 实际入口

- `postgres.BuildOwnAgentContext` 在原最终 session、purpose grant、source/version/xmin、ACL 和数据库时间复核后，为 `HUMAN_SELF_REVIEW` / `MACHINE_TASK_CONTEXT` 生成 `agent-field-evidence-v1`。完整 Bundle 仍受原 seal、64 KiB 与期限限制。
- `agentcontextrelevance.project` 清空完整证据投影，再按实际保留的字段、实体与来源重建。完整批准与最终 revalidation 留在原内部对象中，过滤后的 DTO 不能当新授权。
- 实际 `runtimeContextPurpose` 使用的 Adapter 将证据随条目加入，计入最终 UTF-8 JSON 字节限额；未纳入条目的值、字段及 ID 不经证据旁路返回。原结构化 Memory 内容与 xmin 不进入新响应。
- 本人原 `GET /v1/me/agent-memories/{memoryID}` 的 `data` 增加只读 `fieldEvidenceSet`；原 Memory、target、proof 和最后 `RevalidateOwnMemoryDetail` 保持。没有新读取或写入路由。
- 旧 `RULES_PUBLIC_QUERY` 保持原 100 地点 / 30 活动及 64 KiB 投影，不附加重复字段元数据。本轮没有把该旧入口标为完成字段化；其公开实体可在已有用途批准的机器上下文原 5 项范围内获得字段证据。

## 时间与声明语义

每字段含实际来源类型、ID、版本，声明者/主体，来源更新时间，最终观察与本次采集时间，以及实际已知的声明、创建和有效期时间。`CURRENT_NATIVE_READ` 表示本次读取，不能解释为原媒体上传时间。来源更新时间也不是活动发生、出席或到访时间。

没有记录的拍摄时间保持 `UNKNOWN_NOT_COLLECTED`，不以创建、上传、观察时间或当前城市补齐。较早进程内 DTO 缺少创建/生效时间或置信值时保持未知；native reader 从原 Memory 记录提供这些字段。读取 lease 与来源有效期分别表达。

显式声明的 `DIRECT_DECLARATION:1` 只表示本人填写。原 `UNCALIBRATED_SCORE`、`ORDINAL` 和校准不可用规则复用 `agentconfidence`；不能把模型 0.86 当正确概率或批准。待审推断在详情中只标为 `MODEL_INFERENCE / CANDIDATE_ONLY`，不会变成本人声明。

## 冲突与领域事实

没有适用于所有字段的全局来源排序。当前真实闭集比较限于原活动类别偏好：Profile 明确列出的类别，与 `PREFERENCE / activity_category:<类别>` 的原 `human-declaration` 或 `human-correction` 结构化声明；还必须与原 `Statement` / `NegativeStatement` 的可见摘要完全匹配，防止隐藏结构化内容经字段名称泄漏。相反声明保留两边的来源，状态为 `AWAITING_CONFIRMATION`，不自动写入或选择胜者。相同声明不计为矛盾，不解析自由文字来猜否定、日期、身份或地点。

若关联或字节预算省略冲突另一边，只留下当前可见一边的引用及 `otherClaimsOmitted:true`；仍待确认，不带被省略方的 ID、值或字段。整个冲突相关字段均被省略时不返回该冲突。

身份、授权、组织资格和地理事实仍由原领域服务负责。本人 Identity、City、Place、History、Experience 声明不是身份或到访证明；活动字段只表示当前记录或安排，不表示出席；好友状态不赋予发送消息权限。公开域记录、用户声明和模型候选分别标记，全部 `grantsAuthority:false`。原手工 Memory Evidence、来源撤回与 `BuildOwnProvenance` 冲突错误语义保持。

## 媒体边界与未完成部分

本轮没有安装 `MetadataExtractor` 或读取 `private_media_metadata`，没有接入照片上传、获准分析、模型出口或长期记忆写入。已有离线 codec 不等于这些真实入口已接通。`MediaMetadataDisposition` 只验证关闭媒体端口下的类型契约：旧照片最多历史候选，GPS 的来源为 `media_metadata`，没有个人到访或当前城市结论。该函数的纯单元测试不能当真实照片流水线证据。

当前模型与媒体访问保持 `UNAVAILABLE`，自动写、vision 和 A2A 不启用。实际数据库 ACL/并发/持久化与照片消费、完整外部来源/推断处理、冲突确认客户端及真机未验证，本项只能报告代码与相关单元覆盖，整体为 PARTIAL，Closed Pilot / Consumer Beta 仍 NO。

## 核验

适用 UX-CHECK-06、08、10、16：来源/建议/未知/过期分开，当前主体与版本保持，迟到或撤权不返回旧数据，不默认记录媒体/位置/Memory 内容。

本轮仅相关单元：注册 Memory GET 实际缺字段 RED → GREEN、native DTO 时间投影、冲突/置信/历史媒体契约、relevance 与预算不泄漏、旧 seal/current revalidation、原 HTTP 权限负例。原始命令、退出码、失败历史及源 SHA 在 `docs/testing/evidence/field-evidence-2026-10-06`。数据库/迁移/全量/vet/build/手机/真实媒体均 NOT_RUN，不沿用旧版本的验收结果。


## 2026-10-07：本人单条记忆来源与时间消费入口（AIR024，CODE_LOCAL）

- 复用「管理我的记忆」原个人入口与原同 ID `GET /v1/me/agent-memories/{id}`。裸列表及更正 DTO、具体预览/本机待核实引用/批准/确认接口均保持。查看来源仅人类只读，不启用模型、媒体、写入或新增事实。
- 新详情消费严格绑定 owner/agent/target(id/version/status)、同版本正文、`HUMAN_MEMORY_DETAIL` 当前 0/1 条 `memory.summary` claim、来源 REVISION/nativeTime、观察/采集/记录创建与更新/声明/有效区间、原短读期限和 unavailable/authority=false。目标新版本或同版本不同正文不混用旧列表正文与批准，提示重新读取当前记忆。
- 本人明确声明不是已核验事实；DIRECT_DECLARATION 值1不作为可靠率。INFERRED 仅未经核验的候选推断，不能补声明人/声明时间或概率。拍摄时间未采集，记录/读取时刻不能证明发生、到场、到访或当前位置。缺少 fieldEvidenceSet 时只显示详情中实际记录的创建/更新/有效区间与读取期限，不补声明时间。
- 当前单条详情 conflicts=[] 不表示全部声明没有冲突。页面明确说明跨来源冲突核对尚未提供，不从本地列表拼接来源、冲突或事件事实。
- 只读状态、请求 serial 与单调/UTC 双截止独立于原更正状态；身份/会话/组织 ABA、原对象与 transport/widget 替换、刷新、关闭、较新详情与迟到失败均退役旧读。同步 loading 通知后在实际 wire 前重验；仅详情到期不会清原 pending/preview 或恢复批准。
- 中文本人权限/版本/缺失/网络错误保留真实恢复条件，网络错误允许用户再次 GET；不伪装空成功、自动写入或重新批准。原修改/拒绝/删除/偏好纠正路径继续直接可达。
- 证据：docs/testing/evidence/memory-field-evidence-consumer-2026-10-07。新3直接单元76项与1条原接口控制通过；最终缺metadata文案/真实记录时间增量另1条直接单元通过（重复项不另计需求数）。初次Page缺入口实际RED与新测试编译ERROR均保留，不作原生缺陷。
- 当前仅 Flutter 相关单位；Go/数据库/迁移/native事务/正式身份/手机/辅助技术/模型/媒体/完整回归/analysis/build 均 NOT_RUN。真实冲突消费与其它 AIR024 原门槛未完成，整体仍 PARTIAL，不表示试点可发布。


## 2026-10-07：公开 Rules 结果的可选字段说明（AIR024，CODE_LOCAL）

- 原注册个人 Task GET/POST 的共享响应消费链在 `prepareNativeAgentResults` 使用此次已封口 Receipt 派生 `resultSet.publicFieldEvidence`，经 `WithContract` 重建，再经过原编码后的 native/current-tool 最终校验。没有新增来源查询、Context 许可、模型出口或权限账本。
- PUBLIC 必须同时符合当前 typed Item、原 `PublicCommercialRefs`、同 ID 的原生领域记录；活动还须 `visibility=public`。原领域获准的邀请或成员活动继续保留完整原结果，不据 `AUTHORIZED_VIEW` 推断公开。人物、组织、机会、匿名、ONLINE 和组织工作区不借用此说明。
- 复用原 `BuildFieldEvidenceSet`：公开活动每对象六字段、地点两字段。`NATIVE_DOMAIN_RECORD` 表示领域记录来源，不伪造声明者或声明时间；来源更新/有效截止与原 PG 观察钟/读取期限分别表达。缺少创建/拍摄/发生/到访时间保持未知，模型或媒体访问 UNAVAILABLE，置信值不补，冲突不造，说明不给身份/授权/地理事实权威。
- 原来源有效期已过时，附加说明为 UNAVAILABLE / EXPIRED_SOURCE；原领域仍允许显示的结果及 freshness 保留，不能借附加说明更改原来源 ACL 或续期。未知更新时间、缺少同 ID 记录或可选元数据只影响附加说明；原会话、源版本/ABA、权限、取消或必需 Receipt 错误仍拒绝整个响应。
- 完整附加 wrapper（含预算统计）按现有 UTF8_JSON_BYTES_V1 默认 16KiB 和最多100 claims 限制，仅整组省略。`budget.omitted` 为闭集原因的对象数量，没有被省略的 ID、值、字段、坐标或私有行版本。保留原100地点/30活动及旧公共 Context nil/64KiB 行为，不能先造180 claims 再过滤。
- 私有响应绑定固定同一 owner/Task/任务版本及当前 Item/public-ref 集合，副本不共享指针。重建不能从旧 ResultSet 恢复旧说明；任务、账号、版本、引用变化或 native=false 时清除。此绑定仅防元数据错配，不替代原最终来源权限复验，也不充当批准或操作回执。
- 适用 UX-CHECK-06/08/10/16。证据在 `docs/testing/evidence/public-rules-field-evidence-2026-10-07`：原注册 GET 字段缺失的实际单位 RED、直接字段/响应单位和实际注册 HTTP transport spy 正负响应。源码路径实际接通不表示已执行 SQL/正式认证或现实公开活动。
- 仅相关 Go 单位；PG/session/ACL/ABA 原生实测、真实来源/媒体/跨来源冲突、公开结果客户端、手机/辅助技术、全量/vet/build 均 NOT_RUN。整个 AIR024 继续 PARTIAL，Closed Pilot / Consumer Beta NO。

## 2026-10-07：公开查询结果来源说明消费（AIR024，CODE_LOCAL）

- 在原 `RemoteAgentTaskSource` → `AgentResultSet` → `AgentResultsSheet` → `AgentEntityResultCard` 加入可选只读说明；同一 Item、结果、详情操作与地图选择继续复用。Now 仅接本人 owner getter、已有 seed/workspace 代际与原同实例 Router 的 `current` / `workspaceChanges`；未顺便修改固定 transport 初始化或热更行为。
- 原注册 Go handler 的已封存合成 wire 为 `{data:{...}}`，外层 `PERSON`、Task `person`；活动 wire 确实有 `places:null`。客户端仅活动/地点集合允许显式 null，缺失字段及坏 non-list 仍拒绝，人物/社群必需集合保持。真实 PostgreSQL projection 初始化为 `[]`，此兼容单位不作为生产数据库缺陷证据。
- 独立闭集 decoder 绑定请求前本人 owner、当前 token/组织与已有代际，响应 Task/ResultSet ID 和纳秒任务版本、同 ID typed refs 与原领域 public 记录、PUBLIC_ACTIVITY/PLACE + UPDATED_AT_DIGEST 命名空间。它只验证说明关联，不把 public source token 当原动作 CAS；原 Item closed keys、Router 与领域写权限不变。
- 六/二字段整组、同组版本、真实来源更新/独立到期、原观察/短租期、100 claims 与整个 UTF8_JSON_BYTES_V1 wrapper 16KiB/准确闭集省略计数必须有效。来源到期不替代短租期；等待耗时、单调时间与 UTC 双截止不能因时钟回退、重建卡片或 Task.updatedAt / generatedAt 起新租期。通知撤回、身份/来源 ABA、同旧 carrier 的 Item 替换永久退役附加说明。
- 用户可展开中文「查看来源与时间」：来源更新时间、来源到期或未提供、此次读取/说明期限、预算省略和未知拍摄明确分开。缺少/错误/过期说明仅降级，原合法邀请活动/详情按钮/结果和选择保留。未采集拍摄时间，来源或读取时间不证明发生、出席、到访或当前位置；当前未提供跨来源冲突核对，不宣称全体无冲突。模型、媒体 UNAVAILABLE / grantsAuthority=false 不因展开而变更。
- 来源截止是本次获准读取快照的记录时间，当前面板不按它实时轮询或计时重判来源状态，也不宣称来源现仍有效。返回途中跨过来源截止的动态状态未验证；短读取期限的 Timer 只退役附加说明。原领域操作仍独立由原当前权限路径判断，不借说明或时间授予权限。
- 适用 UX-CHECK-06/08/10/16；Theme tokens、48dp 浮动操作、浅深主题、320宽/3倍字号/键盘场景仅相关 widget 单位覆盖。原实际空集合兼容 RED、原 Sheet→Card 无展开入口 RED、一次新接线/夹具编译 ERROR 均保留；最终3新直接单元60行为及1条原48dp控制通过，不重复作为需求数量。
- 证据：`docs/testing/evidence/public-query-field-evidence-consumer-2026-10-07`。保存的 Go registered-handler wires 是 SYNTHETIC_TRANSPORT；未修改其时间、nil 集合或真实输入来模拟生产。Go/原生 PG/ACL/session/ABA/来源复验、真机/辅助技术、现实活动/IdP/地图、全量/analysis/build、真实媒体/多来源冲突与发布门槛均 NOT_RUN，整体 AIR024 仍 PARTIAL，Closed Pilot / Consumer Beta NO。


## 2026-10-07：本人同体多来源冲突读取（AIR024 接续）

`POST /v1/me/agent-context/self-review` 使用原精确 HumanSelfReview Store 与同实例 Service，返回 `human-self-review-field-evidence-v1` 的同体可读 DTO。本人最多3个 Profile 字段、3条显式私人 Memory、1个政策族；未选值、隐藏 StructuredValue、MemoryKey、内部 source xmin/authority/session 不输出。已知更新时间/来源区间、当前读取钟与短租期分别保留，未知采集/拍摄/发生不补齐。原 FieldEvidenceSet 的声明者/来源版本只说明这份读取，不成为权限、CAS、Memory 写、模型输入或业务结果凭据。

真正可比较的冲突仍只限明确活动类别偏好：Profile 的选中类别 true 与原封闭 PREFERENCE correction false；同一活动类别的双方 claim 保留为 AWAITING_CONFIRMATION，接口不会选赢家、更新记忆或自动确认。中文 notice 明确“没有列出冲突不代表所有声明都没有冲突”。自由文字、身份/GPS和照片不转换成事实。UNCONFIGURED 与未请求保持区别；旧 nil 字段说明端口的派生只能基于这份已校验快照，不修改原 Service seal。

完整 human wrapper<=64KiB 后才同 Service 复验原主体/Session/Source/期限，再输出；变更、撤权、过期或取消不返回旧私人快照。原字段证据100claims上限和深拷贝不变。仅直接相关单位68个去重父子事件（其中旧精确控制1个）通过；注册HTTP spy/source/xmin/版本和15秒裁短租期均为合成契约单位，真实数据库时序没有执行。多源前端检查/确认、原生PG、真实媒体/身份、手机/辅助技术、整仓与构建、视觉及生产均 NOT_RUN；整体 AIR024/Pilot 门槛不因本接口通过而解除。适用 UX-CHECK-05/06/08/10/16。轻量源、原始失败、命令/目录/退出码与实际合成 wire 在 `docs/testing/evidence/human-self-review-field-evidence-2026-10-07`。


## 2026-10-07：本人活动偏好与当前明确记忆同体核对消费（AIR024，CODE_LOCAL）

- 在原「管理我的记忆」页每条 ACTIVE / EXPLICIT 记忆旁提供「核对活动偏好与这条记忆」，直接复用原 API transport 的唯一只读 `self-review` suffix。请求固定选择 preferredActivityTypes + 当前1条 memoryID + 空 policyFamilies；单个 registered SelfReview 响应提供同一快照，不拼独立 Profile / Memory GET，不创建新导航、授权或批准路径。
- 专用闭集 DTO 按实际本人 owner、Agent、选中列表对象的 ID / version / summary / source time / 已知区间，以及精确 1/1/0 selection、sections 和 FieldEvidenceSet 校验。wrapper UTF8 JSON 含换行 <=64KiB，来源声明/版本与实际所选字段一致；隐藏 StructuredValue / MemoryKey / Authority / grant 不消费。固定已封存 registered-handler wire 的原日期和未提供 confidence 保留；别的 selection（包括原 UNCONFIGURED policy wire）只作拒绝样本，不能换成这份成功。
- 页面中文并列双方明确声明、版本、实际已知来源时间、未知创建/拍摄/置信描述和短读租期。原闭集同活动类别 true/false 冲突保持 AWAITING_CONFIRMATION「待本人确认」；不从自由文字、GPS或照片推导矛盾，也不把未列出冲突说成全体无冲突。直接声明的 confidence1（若实际提供）不是事实概率或权限。
- 新读状态与原 correction draft / preview / journal / confirm 独立。token/owner/组织、直接 Page transport / clock / workspace 来源替换、通知 ABA、同 ID 对象替换、迟到/旧 serial、原快照期限与单调耗时不能复活说明或续期。关闭/读取失败/到期仅退役说明；修改记忆仍走原 fresh version review。活动偏好可返回设置「我的智能体」检查，不声称此处能修改资料。原单条说明统一为「这份单条来源说明不包含跨来源比较。」，不暗示过期或 INFERRED 行存在联合入口。
- 适用 UX-CHECK-05/06/08/10/16。320宽、2倍字、浅深主题、48dp、真实点击/滚动、原更正入口保留仅相关 widget 单位覆盖。有效缺入口 RED、首次67场景66PASS与1个新 preview fixture charset 失败均保留；只补该精确 fixture场景，不重刷67。另1条原独立读/journal控制和2条受单条 caption影响的旧场景通过，合计70个去重行为有通过证据；loading 不当需求数。首次整条单位命令仍 exit1，最终组合整套并未重新运行。
- 证据：`docs/testing/evidence/human-self-review-consumer-2026-10-07`，原 wire 是 SYNTHETIC_REGISTERED_HANDLER / synthetic transport，未重写时间或伪造 native 数据。既有 Settings 父 route 源替换仅下一帧 NotificationDestinationBoundary 退休，本切片只覆盖直接 Page/current 通知，父入口下一帧前 dispatch 集成保护 NOT_RUN，未在此修复。真实 PG/Session/ACL/Source 时序、媒体、多来源外部确认、IdP、手机/辅助技术/性能、全量/analyze/build 与发布验收均 NOT_RUN，整体 AIR024 PARTIAL，Closed Pilot / Consumer Beta NO。
