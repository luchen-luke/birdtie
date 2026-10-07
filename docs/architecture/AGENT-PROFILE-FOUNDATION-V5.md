# AgentProfile Foundation V5

日期：2026-10-02。状态：V5共享Profile唯一canonical。§1–9保留已完成AGE001的053、六字段metadata与内部store；§10记录AGE002的054、Public/Private分离与本人编辑；§11记录AGE003的055、11字段受众配置、人类投影及历史本地证据。最终复审发现旧多语句并发撤权窗口及官方Person活动自动名字副本缺口，003重开后的补验已通过2780完整回归与055全源保留，原历史和失败保留；不复用002证据替代。认知Profile读取、Memory或模型使用服务未开放，各项状态仍以live队列及独立任务证据为准。

来源：[AGE原文](../product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md) AGE-001（第246行）；AGE-002/003分别拥有公开/私密内容分离与逐字段可见性。沿用 [身份与ownership](AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md)、[认知ADR](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md)、[Memory边界](AGENT-MEMORY-ARCHITECTURE.md)、[Business](BUSINESS-PRINCIPAL-MODEL-V4.md) 和 [AGE001审计](../research/BIRDTIE-V5-AGE-001-AUDIT.md)。源快照不被本规范修改。

## 1. 领域职责与身份分离

一个 `agent_profiles` 表及 `agentprofile.Record` 服务PERSON / ORGANIZATION / BUSINESS基础类型，不复制三套Profile基础设施。AgentProfile引用原 `agents.id`；owner引用原 `accounts.id` 的typed principal，不能把公开 `organizations.id` / `businesses.id`当owner account。身份、登录、真人成员角色、经营权和业务事实仍属于原领域。

现 `identity.Profile` / `user_profiles` 的displayName、bio及资料级visibility保持原语义；本轮不复制其内容、公开许可或任何私人声明。模型/provider配置不属于Profile metadata，更换模型不更换Agent或owner。001的新metadata版本1只属于这条新权威记录，不能补造普通Profile、Context或Memory的来源版本。

## 2. 六个领域字段与两个数据库内部键

实际文件：[053 up](../../apps/api/migrations/053_agent_profiles.sql)、[Go模型](../../apps/api/internal/agentprofile/model.go)。Go结构及序列化只包含以下六个领域字段。

| 数据库字段 | Go / JSON | 约束与含义 |
| --- | --- | --- |
| `agent_id` | `AgentID` / `agentId` | 原 `agents.id`；主键，一Agent至多一条基础Profile |
| `owner_type` | `OwnerType` / `ownerType` | actorref闭集PERSON / ORGANIZATION / BUSINESS；不是用户自由填写的角色或权限 |
| `owner_id` | `OwnerID` / `ownerId` | 对应类型的 `accounts.id`，不是Organization/Business实体ID |
| `profile_version` | `ProfileVersion` / `profileVersion` | 正bigint；新建1、受控变更严格+1；仅该native metadata/Private/field policy写版本；不是普通UserProfile/Context全域source revision，也不表示同意或权限版本 |
| `created_at` | `CreatedAt` / `createdAt` | 新metadata创建时间，后续不可变；不是Agent原注册时间或经历发生时间 |
| `updated_at` | `UpdatedAt` / `updatedAt` | metadata最近变更时间，数据库更新时保证不倒退 |

数据库另有两个内部 `GENERATED ALWAYS ... STORED` 键：`owner_account_type=lower(owner_type)`、`owner_agent_type` 将PERSON/ORGANIZATION/BUSINESS分别映射到personal/organization/business。它们只落实复合外键，不是独立内容字段、外部请求字段或API返回字段。

数据库要求时间有限且updated_at不早于created_at。Go `New` / `Validate` 要求合法非零UUID、允许的typed owner、正版本和可序列化的有效时间；New规范化UUID大小写和UTC时间。纯形状校验不核实数据库ownership或真人调用资格。

## 3. 当前身份绑定与数据库保护

053为accounts增加 `(id, account_type)` 唯一约束，为agents增加 `(id, principal_account_id, agent_type)` 唯一约束，再以两个复合外键绑定Profile：

| Profile键 | 当前权威目标 |
| --- | --- |
| `(agent_id, owner_id, owner_agent_type)` | `agents(id, principal_account_id, agent_type)` |
| `(owner_id, owner_account_type)` | `accounts(id, account_type)` |

因此，另一个合法UUID、错误owner类型、错误Agent/owner组合、Organization实体ID或并发改Agent principal/account类型不能被当成正确ownership。单独的Agent/Account ID外键不足以替代完整绑定。

`birdtie_guard_agent_profile_revision` 要求INSERT版本为1；UPDATE时agent_id、owner_type、owner_id、created_at不可变，版本必须精确+1，max bigint不得溢出。updated_at由数据库设为 `GREATEST(clock_timestamp(), OLD.updated_at)`。这不是已提供的内容更新/API/通用CAS服务；今后具体写动作仍须绑定已展示旧版本、核验权限并防止覆盖。

复合外键 `ON DELETE CASCADE` 只处理原Agent/Account实际删除后的metadata依赖；不表示已经完成Memory、Evidence、候选、缓存或provider数据删除传播。

## 4. 已有和新Agent的生命周期

迁移对已有personal/organization及合法预留business Agent只补六字段metadata，不更新Account、Agent或任何Profile内容/业务实体ID。即使旧Agent处于suspended/retired，其metadata保留也不表示可调用；当前store另查活跃状态。System、Community、CityContext、Place和Venue不创建Profile。

`agents_bootstrap_profile` 在新personal/organization/business Agent插入后，同事务建立唯一metadata。已有Person注册/OIDC及Organization创建仍走原身份路径；触发器补metadata，不创建第二个Agent，不改变真人、组织或workspace身份。冲突不覆盖现有Profile版本。`EnsureAgentProfile` 是内部补缺失metadata操作，不是注册或重新授权入口。

旧 `organizations.organization_type='business'/'venue'` 继续属于ORGANIZATION principal及其原Agent，不批量重分类，不从Place名称、Venue经营者引用或城市目录创建Business主体。

## 5. Business仅预留形状，不开放调用

053允许 `agents.agent_type='business'`，要求principal确实为独立Business account，并以 `agents_business_not_enabled` 约束status只能suspended/retired。迁移不自动创建Business Agent或claim记录；未来单独实现者不能通过设置active绕过当前约束。

共享schema/类型仅保证基础设施兼容。`agentruntime` 的Business能力包仍不可调用；当前Get/Ensure对BUSINESS立即返回 `agentprofile.ErrUnavailable`。新Profile不提供Person/Organization能力、经营权、真人管理权限、公开资料或模型调用资格。Business启用须后续明确设计、当前claim/角色边界、迁移与验收，不因本基础模型完成启用。

Community仍是原社交实体，无账号/Agent；City是平台Context，Place/Venue是情境/举办能力，均不因AgentProfile引入变成principal。

## 6. Get / Ensure是内部metadata primitive

实际文件：[postgres/agent_profile.go](../../apps/api/internal/postgres/agent_profile.go)。这两个方法接收明确AgentID及 `actorref.PrincipalRef`，输出六字段 `agentprofile.Record`。

| 内部方法 | 当前行为 | 不提供的能力 |
| --- | --- | --- |
| `GetAgentProfile` | 事务中以share锁核精确Agent/owner/type、当前Account与Agent active；Organization还核当前组织active；精确读取metadata | 不创建记录、不返回Profile内容，不核真人会话/成员/purpose/consent |
| `EnsureAgentProfile` | 同样核验当前绑定后，仅INSERT缺少metadata并重读；已存在时不覆盖、不重置版本 | 不创建Agent/账号、不编辑内容/版本、不启用Business或认知工具 |

owner参数是**内部受信绑定声明**，不是人类授权凭据。方法不接HTTP、Flutter页面或模型工具；未来gateway必须先解析当前真人会话、workspace、成员角色、资源用途和独立同意，再决定是否调用。不能把Get读到正确metadata解释为“该真人/Agent获准读取私密Profile”。

模型/客户端传入 `confirmed:true`、正版本、正确owner或成功解码Record都不能补齐授权。`ErrInvalid`、`ErrForbidden`、`ErrNotFound`及Business `ErrUnavailable`保持不同边界；AGE001当时的`ErrConflict`类型预留没有提供内容更新/CAS API。随后AGE002的本人Private CAS接口只在§10限定范围实现，不改变Get/Ensure的内部metadata职责。

## 7. 与Phase0及未来AGE/AIR接口的关系

历史Phase0 `BT-V5-INT-001` 完成时尚无持久AgentProfile。AGE001交付时仅增加上述metadata模型、表、绑定和内部store，未修改 `agentcognitive.CurrentDomainAdapter` 或认知端口。

`CurrentDomainAdapter` 仍只桥接本人普通 `identity.Profile` / Context读取；`DecideEligibility` 对合法认知请求仍UNAVAILABLE；`UnavailableCognitivePorts` 的Memory读取/候选提交仍ErrUnavailable。Profile metadata的版本和精确数据库绑定不自动成为BoundaryFacts、通用来源/consent resolver、人类grant或模型出口许可。

AGE001交付时，AGE002的Public/Private内容分离和本人Private gateway仍是后续；当时没有新增HTTP路由或内容更新API。该历史范围不表示当前仍缺这些接口：AGE002现已交付§10的Person-only九字段内容，AGE003的字段受众配置与人类投影见§11。独立认知读取许可、Memory/Evidence、ContextAssembler、候选审阅、纠正/删除、ModelGateway及运行/批准/effect账本/执行器仍待各自后续任务；尚无新Private/受众消费编辑UI或真实自主动作。

## 8. 有数据回退保护

[053 down](../../apps/api/migrations/053_agent_profiles.down.sql) 在任何 `agent_profiles` 行（包括bootstrap版本1）或business Agent存在时，事务内直接拒绝。不能删除Profile/Agent来假装可安全回退，也不在生产执行down。

只有真正无Profile、无business Agent的数据场景才支持移除新触发器/表/约束并恢复旧Agent类型形状。空隔离库down/reapply与带旧数据up是两个独立验收场景，执行者须分别留证；不能把带数据down拒绝报告为回退成功。

## 9. 验收、证据与发布范围

验收至少包括：六字段JSON无内容泄露、三类共用模型、非法类型/版本/时间拒绝；精确owner与Agent匹配、重复Ensure无覆盖、账号/Agent/组织停用拒绝、BusinessUnavailable；SQL身份/类型/版本保护、已有/新Agentmetadata、旧ID/API、seed幂等、带数据down拒绝、空库down/reapply与默认并发Go回归。

AGE001已由根任务实际完成仓库/隔离本地验收。结果见 [完整证据与复现](../testing/evidence/agent-profile-foundation-2026-10-02/README.md) 和 [机器汇总](../testing/evidence/agent-profile-foundation-2026-10-02/verification-summary.json)：

| AGE001实际检查 | 结果 |
| --- | --- |
| Go metadata模型 | 37 PASS事件，0失败/跳过；scoped vet通过 |
| 053 PostgreSQL store | 41 PASS事件，0失败/跳过；精确绑定、状态、并发Ensure、版本与重连覆盖 |
| 限定包默认并发回归 | 779 PASS事件，0失败/跳过 |
| 默认完整Go三轮 | 每轮1302 PASS事件，0失败/跳过；全量vet/build通过 |
| 053 fresh及带seed的052→053 | up、旧身份/UserProfile/Activity/Place保留、绑定/版本/Business禁用、后续Agent bootstrap及三开发seed通过 |
| 回退保护 | 非空down原子拒绝并保留metadata；真正空隔离表down/reapply通过 |

计数含主测试和子场景，不是功能数量或真实用户数；复现命令、日志、源码hash与首次纯测试重命名编译失败及修复均在证据目录保留。这里引用真实已存在结果，没有由文档子任务重跑Go/迁移。全部属于本地一次性库及合成资料验证，不是生产身份、正式部署或试点证据；[审计](../research/BIRDTIE-V5-AGE-001-AUDIT.md) 的验收清单与实际结果分别记录。

本项无Flutter/UI变更，不产生新的中文页面、真机截图、地图/键盘或辅助技术验收；原中文主要语言、直接路径和稳定实体/地图约束继续有效。

**Closed Pilot Ready：NO；Consumer Beta Ready：NO。** metadata不补真实IdP/HTTPS、核验组织/活动、生产地图/API、部署日志、提醒运营、值守或真实A→H证据，也不放行live provider、真实Agent自动写或A2A。

## 10. AGE002 Public / Private内容增量（隔离本地验收已通过）

本文件继续为共享Profile唯一canonical；§1–9保留AGE001metadata范围，以下记录AGE002实际054、模型、store及HTTP增量，独立运行结果在§10.6；基线、静态审阅与历史记录见 [AGE002审计](../research/BIRDTIE-V5-AGE-002-AUDIT.md)。AGE003后续增量另见§11，不反写002当时范围。

### 10.1 Public复用原对象，Private不进入其投影

Public复用 `identity.Profile` / `user_profiles` 和原 `ReadProfile` ACL，实际响应只有accountId / displayName / bio / visibility；源文avatar/current_city/interests及公开内容列表是后续示例范围，不从私人Context/Moment/Agent字段派生或补入。public、本人或有效profile_view grant仍受active账号、Block、期限和撤销约束；不可见继续not_found。

新的Private只属于本人PERSON Personal Agent；profile_view grant单独不能读取Private self接口或Private字段，好友、组织/商家角色、051/052/AGA、公开普通资料或自声明均不继承。003另增人类字段投影，只有原资料ACL与明确字段规则同时允许时才返回对应值，见§11。普通Profile编辑、公开Intent/活动发现及RSVP/Plans等原动作不改变；Private写入不改user_profiles、关系、Context/Moment、活动供给或任何许可开关。

### 10.2 九类本人直接输入及有限规范化

实际模型：[agentprofile/private.go](../../apps/api/internal/agentprofile/private.go)。`PrivateFields`只有六个文本数组personalPreferences、socialPreferences、preferredActivityTypes、travelPreferences、interactionPreferences、languagePreferences，及availability、privateCityHistory、agentNotes三个人工文本。

列表至多20项，每项160rune；文本各2000rune；规范化后的fields JSON至多12KiB，整体HTTP/解码body至多16KiB。合法UTF8，列表拒空白项/控制字符，规范化trim/CRLF和列表去重，返回新数组避免别名；省略的具体fields归空数组/文本，是**完整替换，不是PATCH**。请求null/未知/重复键、非法类型、非正整数expectedVersion和尾随JSON均拒绝。

仅存本人当前明确输入，不分析、推断或复制旧Profile/Context/Moment。空值是未说明，不表示无兴趣、不愿社交或空闲；availability不是时间承诺，私人城市叙述不是定位/身份事实，notes不是policy override。没有confidence、推断provenance、provider或模型使用grant字段。

### 10.3 独立持久内容与唯一aggregate版本

[054 up](../../apps/api/migrations/054_agent_private_profiles.sql) 新增 `agent_private_profiles`，以agent_id为PK，owner_type仅PERSON，复合FK绑定原 `agent_profiles(agent_id, owner_id, owner_type)`。既有数据不backfill私人字段，metadata仍为单一权威版本；`written_profile_version`只记录最近内容提交对应的aggregate版本，不是另一个外部版本真源。

此aggregate限定为native metadata、Private内容及AGE003新增的字段policy写入，不代表旧普通UserProfile/Context的全域源版本。普通displayName/bio仍归UserProfile及其实际updated_at；不为了新受众设置把原普通编辑改成复制内容的新真源。AGE003的受众配置是持续选项，并非对某一内容版本的具体外发批准；projection必须实时读取当前普通源与ACL。未来认知source/具体批准需要另绑该源的真实当前updated_at/revision，不能补造Version=1或只用本metadata版本。

数据库约束内容只含完整九键，六数组/三文本类型与长度受限，fields JSONB文本存储上限16KiB；Go规范化的12KiB限制另在store/HTTP落实。写入须匹配当前metadata版本且版本>1，更新绑定/创建时间不可变，不复用旧written版本。清空先推进aggregate版本再删除Private行；原身份实际删除可沿FK级联，不代表Memory/provider删除传播已经实现。

[054 down](../../apps/api/migrations/054_agent_private_profiles.down.sql) 在任何Private行存在时拒绝，不擦除内容。真正无Private内容才移除该表/trigger/复合唯一约束，原metadata版本、身份和Public数据保持；空down/reapply和有数据拒绝已分别运行留证，见§10.6。053对任意metadata存在时仍拒绝down，原保护不被054解除。

### 10.4 当前本人store与HTTPgateway

实际文件：[Private store](../../apps/api/internal/postgres/agent_private_profile.go)、[HTTP gateway](../../apps/api/internal/httpapi/agent_private_profile.go)。这是**普通本人直接编辑**接口，区别于AGE001内部metadata Get/Ensure，不是AIR/模型工具、组织委派或认知读取许可。

GET / PUT `/v1/me/agent-private-profile`由唯一有效bearer解析PERSON；出现`X-Birdtie-Organization-Workspace`即拒绝，包括空值或重复头。无owner/Agent/workspace路由参数，非空query拒绝，GET不接受body。PUT只 `{expectedVersion, fields}` 且Content-Type为application/json；响应为 `{data:{schemaVersion:'private-agent-profile-v1', profile:<六字段metadata>, fields:<九字段>, configured:<bool>}}`。no-store贯穿正常及错误；返回数据再核PERSON/owner一致，错误只给固定代码。

`PrivateAccess`只在服务器传session digest + typed本人principal，JSON编码/解码拒绝。store每次锁定当前未撤销/未过期session、active Person/精确Personal Agent和metadata；在返回/提交前用wall-clock复查session到期。组织/Business或错owner、停用身份/Agent拒绝，不接受客户端正确UUID、confirmed、grant或模型文本作为权限。

`ReadOwnAgentPrivateProfile`缺Private行时返回空fields/configured=false，不创建记录。`ReplaceOwnAgentPrivateProfile`以expectedVersion锁定/CAS原metadata并+1，原子替换Private内容；显式全空也推进版本并删除行，返回configured=false。重复提交旧版本返回冲突，不能静默覆盖；有确认HTTP保存意图不等于同意模型分析/长期Memory/公开。PG失败详情不从store透传，日志只留request ID/固定分类，不输出Private正文、token或PG DETAIL。

### 10.5 未开放能力与验收范围

AGE002交付时没有逐字段audience或跨主体Private投影；随后AGE003在§11限定范围实现人类字段受众和当前读取，并非模型读取。AIR/Runtime/provider使用许可、独立purpose/consent、认知来源/读取授权、Memory及模型出口仍待后续；Private数据存在不改变DecideEligibility/UnavailableCognitivePorts，没有新的消费编辑UI。

AGE002已完成下节独立模型/store/HTTP、迁移和无泄露正负验证。002没有新Flutter/真机界面验收，原中文及普通路径继续有效；AGE001测试与手机基线不替代本项身份、版本、持久及泄露防护验收。Closed Pilot / Consumer Beta继续NO。

### 10.6 AGE002实际证据与复现

完整结果及命令见 [证据README](../testing/evidence/agent-private-profile-2026-10-02/README.md)、[机器汇总](../testing/evidence/agent-private-profile-2026-10-02/verification-summary.json)、[复现入口](../testing/evidence/agent-private-profile-2026-10-02/reproduce.ps1) 与 [manifest](../testing/evidence/agent-private-profile-2026-10-02/manifest.json)。文档审计者独立读取十份原始JSONL并复核38个manifest文件hash一致；没有重跑或替换根任务日志。

| AGE002实际检查 | 结果 |
| --- | --- |
| Private领域 / 全Profile领域 | 87 / 124 PASS事件，0失败/测试跳过 |
| 真实PostgreSQL Private store / 四包并发 | 34 / 1012 PASS事件，0失败/测试跳过 |
| HTTP spy / 注册路由与真实PostgreSQL | 161 spy；合计174（161 spy + 13集成）PASS事件，0失败/测试跳过 |
| 默认完整Go三轮 / 迁移单独fullGo | 各1597 PASS事件，0失败/测试跳过；全量vet/build exit0 |
| 054 fresh / 带三开发seed的053→054 | 两个自有一次性库up；每库33个严格SQL拒绝及合法CAS/write/clear/cascade通过；旧Account/Agent/UserProfile/metadata/Activity/Place全行和版本保留，无Private backfill |
| 回退保护 / 持久与清理 | 有Private内容down exit3且完整数据不变；明确CAS清空后空private down/reapply保持metadata/Public；独立连接恢复九字段，随机fixture及自有库清理通过 |

计数包含主测试及子场景，不是功能数或真实用户数；19个无测试文件包的package skip不计为测试跳过。覆盖另一Person仅读自己、匿名/普通Profile grant无Private canary、组织/Business/workspace/撤销/到期/停用拒绝、并发CAS、失败回滚、锁等待到期、全空v3清空及原领域无副作用。资料和session均为合成，本项不构成生产身份、正式部署、现实活动或新消费UI证据。

首次DDL fixture误把PostgreSQL拒绝非法scalar JSON的22023视为失败，已只修测试期望并保留首次记录，054/store未改；第二轮DDL和第三轮完整回归通过。早期未运行草稿的`jsonb_object_length`在首次实测前改正，不记为实际migration FAIL；store runner相对日志路径错误发生于测试启动前，修绝对路径后实际通过。详细历史和限定范围见 [AGE002审计](../research/BIRDTIE-V5-AGE-002-AUDIT.md)。AGE003及认知授权/Memory/模型出口继续独立验收，Closed Pilot / Consumer Beta仍NO。

## 11. AGE003逐字段受众与人类投影（仓库与隔离本地补验通过）

本增量只记录实际 [字段模型](../../apps/api/internal/agentprofile/visibility.go)、[store](../../apps/api/internal/postgres/agent_profile_visibility.go)、[HTTP](../../apps/api/internal/httpapi/agent_profile_visibility.go) 和 [055 up](../../apps/api/migrations/055_agent_profile_field_visibility.sql)。来源为AGE-003；基线、范围裁决与失败历史见 [本项审计](../research/BIRDTIE-V5-AGE-003-AUDIT.md)。没有新增Flutter页面、认知reader、模型/provider许可或第二份Profile内容。

### 11.1 11个真实字段与保守默认

可配置字段仅为普通源的displayName、bio和§10.2的九个Private人工输入：personalPreferences、socialPreferences、availability、preferredActivityTypes、travelPreferences、interactionPreferences、privateCityHistory、languagePreferences、agentNotes。不包含accountId、资料级visibility、metadata、avatar或城市定位等未有真源的示例字段。

policy是独立overlay，不保存字段内容。未配置时displayName/bio默认PUBLIC，仍受原资源ACL；九个Private字段默认PRIVATE。GET默认不写表，055不回填规则、私人值、Community目标或同意。配置每次完整替换11条规则，未知字段、缺字段、额外权限属性、null、重复wire键或未定义audience拒绝。

| 字段受众 | 当前人类读取资格 | 不授予的许可 |
| --- | --- | --- |
| PUBLIC | 当前字段规则允许；新projection还须通过原普通资料ACL、活跃主体/Agent与Block检查 | 不绕过原资料级private或自动公开其他字段 |
| CONNECTIONS | 精确双方active Person、active `person_ties`、matching accepted friend request及无Block；读时重验 | 单向follow、pending request、聊天、共同报名或推断Close不算好友许可 |
| COMMUNITY | owner明确选择1–8个真实非零UUID；所选Community当前active/published/未到期，双方同一目标active成员 | pending/invited/left/rejected、仅浏览或Organization角色不授权；Community无Agent |
| PRIVATE | 只有当前owner的普通隐私控制/本人查看 | 普通profile_view grant、好友或工作台不继承 |
| AGENT_ONLY | owner仍可检查/纠正自己的设置；其他人不可读 | 枚举不启用Runtime/Memory/模型分析、出口或A2A，认知ports仍Unavailable |

COMMUNITY目标必须由owner明确输入，不能从共同活动、学校或城市推断；目标hidden但published时，当前双方active成员资格可满足成员范围，外人不可读。退出、归档、取消发布或到期后重读拒绝。双向Block优先；Unblock不恢复已删除Tie/撤销grant。

### 11.2 本人配置gateway与严格wire

GET / PUT `/v1/me/agent-profile-visibility`只由当前有效bearer派生本人PERSON及精确active Personal Agent，复用服务器内部 `PrivateAccess`。Organization workspace头即拒绝；不接受caller指定owner/Agent/workspace、非空query或GET body。PUT需application/json，body至多16KiB，只 `{expectedVersion,rules}`；完整规则中的每项只有visibility及communityIds，不接受confirmed或模型权威属性。非COMMUNITY不得带目标，COMMUNITY须1–8个不同合法UUID；Go规范化为小写/排序，数据库写入另核当前目标资格。

响应为 `{data:{schemaVersion:'agent-profile-visibility-v1',profile:<六字段metadata>,rules:<完整11规则>,configured:<bool>}}`，仅给本人控制。返回默认规则时configured=false；明确保存全部默认规则也推进metadata版本，然后删除policy行，不重置Private内容或普通资料。旧expectedVersion冲突返回409，失败事务不留下部分版本/规则；当前会话、主体、Agent及Community在提交前重新核验。

正常和错误响应均no-store。无/失效bearer401；本人主体/工作台或权限错误403，非法wire400，媒体类型415、过大413、冲突409、不可见404、不可用503。错误/日志只保留固定分类与request ID，不输出正文、token或PG DETAIL；成功解码与positive version本身不构成权限。

### 11.3 人类字段projection与原资料ACL相交

GET `/v1/accounts/{accountID}/agent-profile-fields`当前只返回 `{data:{accountId,fields}}`。匿名无bearer可进入公开判定，提供但失效的bearer不能降级为匿名；query/body/workspace头同样拒绝。每次从原user_profiles和Private表实时读取，仅获准且有非空值的字段进入SQL结果，应用层不先取出被拒绝的Private值再过滤。

原普通资料ACL（public / 本人 / 当前特定profile_view read grant）与字段overlay同时满足，grant不能单独授权Private或AGENT_ONLY。target须active Person、精确active Personal Agent及当前metadata；viewer非匿名时须当前有效会话和active账号。读取锁定当前源、规则、会话、相关Tie/request/成员/grant，资格与wall-clock期限在返回前复查。无可见非空字段为not_found；不返回metadata、rules、Community IDs、成员/好友名单、被隐藏字段名或其他认知证明。

这是人类资料读取，不是AIR ContextBundle、认知SourceState或模型输入。PRIVATE/AGENT_ONLY的owner查看用于隐私管理；不因成功读取签发分析、Memory、供应商或跨Agent许可。

### 11.4 唯一native版本、当前源和数据库保护

`agent_profile_field_visibility`以原Agent为PK，PERSON-only，复合FK绑定当前metadata的agent_id/owner_id/owner_type；规则必须为完整11键，每项闭集形状，JSONB存储至多16KiB，时间有限且有序。绑定与created_at不可改；首次/更新written_profile_version必须对应metadata当前正版本且严格推进，不能凭旧版本插入、覆盖或清空。真实parent删除可沿FK级联。

Private与policy共用metadata CAS；本地验证已有Private written v2后policy v3、清空policy v4，Private仍保持原v2内容。普通displayName/bio和资料级visibility仍归UserProfile及其真实updated_at，普通编辑不推进本metadata，也不复制进新表。字段配置是持续受众设置，**不是批准某一版内容发送**；每次人类投影实时读当前源/ACL/overlay。未来认知source及具体批准必须另绑各源真实当前updated_at/revision与独立许可，不能补造Version=1或把native版本称通用source revision。

[055 down](../../apps/api/migrations/055_agent_profile_field_visibility.down.sql)在任何policy行存在时原子拒绝；完整rules、Private、源行和版本均保留。真正空规则表才允许down/reapply；本地明确CAS清空规则后回退和重应用不删除普通资料、Private或metadata版本。053/054既有有数据保护不由055解除。

### 11.5 旧12条消费路径及EntityCard修复

`birdtie_agent_profile_field_allowed`是附加字段限制，保留各资源原ACL，不替代它们。除了新projection，现有12个PostgreSQL消费文件已接入：identity、agent_workspace、connections、friend_chat、follows、community_social、organization_memberships、activity_chat、community_chat、relationship_context、new_people、social_activity_publish。涵盖普通Profile、Agent人物搜索、请求/Tie/会话/人卡、follow列表、成员列表、房间发送者、关系/新朋友名称及个人活动自动host label。被拒绝的普通字段返回空值或原通用中文标签，hidden displayName不参与可推知的搜索命中，不回填Private文本。

名字字段拒绝时直接返回通用中文标签，不能改用该Person的handle作为绕行。当前`identity.Actor.Handle`用于Authenticate绑定本人，公开普通Profile四字段不含handle，也没有独立publicHandle授权；合法旧数据的handle可能与私密displayName同值。仅在名字字段及其原资源ACL均允许时，才保留原name→handle→通用标签回退语义；native自动派生活动标签还需原Profile粗ACL允许，具体边界见下文。此裁决不新增可配置handle字段，也不以helper拒绝推导另一种姓名来源授权；相关同值canary和最终source回归仍在补验。

EntityCard发送先分别核两位participant可见性，但发送响应只使用sender自己的当前标签；不能把recipient视图标签写回sender响应。之后每次消息读取按实际reader重新解析引用，不复用旧公开名称快照。活动/Community chat insert/replay、组织invite/accept/role response也按实际caller判定，管理员不继承成员私人名字。

真正独立且有明确来源的legacy/外部/Organization/Community/Business label仍归原供给源及其ACL，不能用Person字段overlay删除无关来源。**当前官方PERSON `CreateSocialDraft`自动由UserProfile复制的host_label/maintainer_label不属于该豁免**：Input没有hostLabel，Organizer.Name不作为此独立输入，现UI也无单独名字批准；公开发布活动不等于新增Profile公开授权。其创建owner/host、typed PERSON及服务器来源标识可识别，读取PublicActivity的HostLabel、Organizer.Name、Source.Maintainer及Agent termsearch需实时读当前普通Profile，并同时满足原source粗ACL（public / 本人 / 当前特定profile_view read grant）和displayName字段helper；默认字段PUBLIC不能越过原Profile整体private，失效/过期/撤销grant不授权，grant也不能越过PRIVATE/AGENT_ONLY。

该粗ACL相交限定于原Profile投影及上述自动派生source，不扩大为所有旧chat/member/关系资源必须额外申请profile_view；那些保留原room/Tie/成员等资源ACL与field overlay。真正独立来源保留原源；不以清空旧行或改schema代替保护。这组新路径/匿名、peer、有效/撤销grant、字段私人及本人复核case当前补验，实际结果见下节。

[旧投影测试](../../apps/api/internal/postgres/agent_profile_visibility_legacy_integration_test.go)覆盖5个audience各19个场景，另有follow不授权、邀请/角色响应、撤Tie/退Community/撤成员/删Agent等场景；[普通源并发编辑](../../apps/api/internal/postgres/agent_profile_visibility_source_integration_test.go)证明实时源变化不绕过overlay，也不漂移native版本。无需修改地图、键盘、Pin或前端布局来落实这些服务器边界。

### 11.6 已核验历史结果、失败记录与当前门槛

以下均为自有随机合成资料和隔离库，不是现实身份、真实活动或消费者验收。文档审计者已独立读取相关JSONL/result和完整快照差异，未重写根任务运行日志。它们证明当时已覆盖场景；后续发现的旧多语句撤权和Person活动自动名字副本当时须修复、补确定性竞态/投影/search新case及最终source复验；这些补验已取得本页末尾当前结果，旧PASS仍只证明当时范围。

| AGE003实际检查 | 当前已取得的结果 |
| --- | --- |
| 字段模型 / 全Profile领域 | 312 / 436 PASS事件，0失败/测试跳过 |
| PostgreSQL store初轮范围 / 含旧消费及源并发最终限定范围 | 72 / 185 PASS事件；对应四包并发1396 / 1509 PASS，0失败/测试跳过 |
| HTTP spy / 合计注册路由及真实库限定范围 | 89 spy；合计102（89 spy + 13集成）PASS事件，0失败/测试跳过；本人GET/PUT和新projection均覆盖 |
| fresh001–055 / 三seed的054→055 | 每库62个严格SQL拒绝、74个真实helper权限断言；所有public基表完整源行、Private v2与原版本保留，无规则回填 |
| 第五轮默认完整Go及数据核对 | 2196 PASS事件，0失败/测试跳过，exit0；测试后所有public原完整行/版本及精确fixture清理通过 |
| 回退/重应用 | policy非空down exit3且全行原子保留；明确CAS清空后空down/reapply保留普通源、Private v2/native v4，两库严格清理 |
| 4条legacy确定性撤权竞态（修复后限定范围） | 43叶场景/48含父PASS；focused233 / 四包1557 PASS、0失败/测试跳过、vet exit0，自有库已清理 |
| Community成员列表最终资格（独立focused） | ListSocialMembers最终payload核当前lifecycle/Person/member，pending只给当前owner/admin；9叶+1父=10 PASS。后续混合scope因活动自动label REDcase整体FAIL，不能称该整轮通过 |
| Person自动副本限定green5 | 510 PASS、0失败/测试跳过：派生212、独立源8、首次Private25、handle同值拒绝98、源粗ACL167；store/vet exit0，自有库删除且前后fixture统计一致 |
| 同值handle修复后完整字段限定范围 | visibility753 / 四包2077 PASS、0失败/测试跳过，含legacy112、竞态48、成员列表10；store/parallel/vet exit0，前后fixture统计一致且自有库已删除 |
| Person自动副本 / 最终source回归 | 003仍重开；Person派生label/search新case及全部稳定修复后的完整三轮/vet/build另行补验，未提前标通过 |

计数包含主测试和子场景，不是功能数或真实用户数；无测试文件package skip与test skip分开记录。[证据入口与当前重开说明](../testing/evidence/agent-profile-visibility-2026-10-02/README.md)保留历史第五轮及初次三轮原始日志/源码hash，最终并发修复后的结果另由根任务追加；本文件只引用，不创建或改写该证据目录。

失败历史必须保留：第一轮DDL fixture的局部community_id与列同名造成歧义，只修fixture为owned_community_id后第二轮DDL通过。第三/四轮完整Go各2196 PASS、exit0，**但完整源行快照FAIL**，不能称迁移整体通过。第四轮保留before/after、表checksum、真实主键与逐列差异，定位旧HTTP Context生命周期fixture留下2个INSTITUTION/ONLINE节点，原源行无修改；根任务将固定SourceKey改为自有随机值、按返回ID清理并断言无残留，未改055/helper或豁免contexts。第五轮相同严格全行比较及完整回退重应用全部通过，首轮和第3/4轮失败记录不被覆盖。

最新复审另发现4条legacy RepeatableRead多语句路径存在并发撤权窗口：`activity_chat.go: ActivityChatMessages`、`community_chat.go: CommunityChatMessages`、`relationship_context.go: OwnRelationshipContext`、`new_people.go: FindNewPeople`。此前房间/consent/source preflight完成后，旧事务快照可能使后来提交的字段或资源撤权不进入最终label SELECT；根任务在初次登记完成后立即重开003，未领取AGE004。

既有2196日志和初版manifest保留为历史source证据，该竞态不能由顺序撤权测试证明。4方法现已改ReadCommitted及最终投影同一SQL重新核原资源guard：chat当前房间/成员/RSVP/Block，关系当前本人consent/Agent/accepted Tie及shared activities，新朋友当前source约束和candidate。确定性测试仅用自有pgx QueryTracer，在preflight完成后暂停、真实提交撤权再放行最终SQL，不添加生产延迟hook。43叶场景（48含父）及focused233/四包1557、vet已独立核对，日志为 `work/v5-age003-visibility-race-round3-*`，fresh001–055/三seed自有库清理且前后统计一致；最初编译和RSVP fixture检查失败保留。该限定范围不代替随后官方Person派生label及最终source完整回归。

实际线性化点是最终投影statement的snapshot及原资源guard；helper为STABLE，资格与payload同条SQL判定。不承诺该点之后网络已在飞的数据可绝对撤回；全部最新修复与最终source回归取得前，不宣称003完整验收完成。

根任务另补 `community_social.go: ListSocialMembers` 最终payload的当前资格EXISTS，保留preflight和原ACL：当前Community生命周期、active Person/成员，pending列表只对当前owner/admin。独立 [确定性成员列表测试](../../apps/api/internal/postgres/agent_profile_visibility_members_revocation_integration_test.go) 9叶+1父PASS，真实commit字段私密/退成员/停用/归档/admin降级后无越权。该scope后半四包执行碰到当时尚未修复的活动自动label REDcase，整体为FAIL；不能把成员focused成功记成整轮PASS，最终混合回归待全部source稳定后重跑。

另一次最终裁决纠正了先前“发布label均属独立源”的范围：官方Person创建的两个自动名字副本没有独立输入/批准，必须跟当前字段限制；只有真正独立明确来源保留。根任务正在补受众投影与搜索路径并保留旧行/schema，无新消费者批准界面或公开授权。原记录的静态PASS不能替代新副本来源/撤权case。

最终标签检查还发现拒绝分支仍可能回退到handle；当handle与私密displayName相同，字段拒绝会被这一别名绕过。根任务裁决拒绝时固定通用标签、允许时才沿用既有名字回退，并对人物搜索、自动Person标签及旧消费路径补同值canary。最初RED与后续限定green均保留，不能将旧标签测试通过记成最终全部source通过。

同值canary后的 `work/v5-age003-visibility-handle-green1-{store,parallel}.jsonl` 已独立核对753/2077 PASS、零失败/测试跳过；112个legacy、48个竞态与10个成员列表事件均在其中，原handle RED40个失败事件仍保留。该四包范围和前后fixture数量一致仍不替代最终全package/完整源行快照回归。

本项没有Flutter/UI变化，不新增用户可见受众开关、手机/地图/辅助技术或无提示消费者完成证据；中文为主、普通非AI直接路径及稳定实体约束继续适用。字段可见性不开放Memory、Runtime/provider、真实自主写、ASK_USER送达或A2A。**Closed Pilot Ready：NO；Consumer Beta Ready：NO。** 原IdP/HTTPS、核验组织/授权活动、生产地图/API、部署日志、提醒运营、值守及真实A→H门禁保持。

## AGE003 最终补验结果（2026-10-02）

此前确实取得的2196三轮和055历史结果、初次DONE后立即重开的记录保留；现在4个旧多语句最终撤权guard、成员列表当前资格、同值handle拒绝回退、官方Person活动自动姓名的源粗ACL相交，以及原生维护者自动复制标签均已修复并取得当前源码证据。

最新3轮默认完整Go与055迁移独立fullGo各2780 PASS、0失败/测试跳过；全量vet/build exit0，当前API ready200、5未开放推断/协调路由404。fresh/seeded两自有库各62严格SQL拒绝+74真实helper断言，所有public基表完整旧行/版本保留，非空down exit3原子保护，明确清空后empty down/reapply保留Private v2/native v4；自有数据库/进程已清理。归档182文件/38源码hash完整核对、没有bearer泄漏；后续源码增量另取证，不覆盖此时真实日志。

自动维护者专项实际Store16 PASS、四包vet exit0；Social/Submit Community、Place publish/link_existing真实入口及历史Activity source reader覆盖。旧复制由服务器exact audit/source链接识别，仿prefix但无/错audit、真正独立manual及Host/组织信息保持；不删除或回填旧源。四legacy竞态43叶/48含父、成员9叶+1父、Person自动活动510、同值handle753/四包2077的实际限定结果及首次RED保留，不用顺序测试代替并发撤权。

**独立未修复缺陷**：实际City Seed ReviewActivity新候选发布返回23514 activity_organizer_exactly_one。已增量登记BT-FIX-CITY-001；合法历史source reader fixture和完整Go不算此业务入口成功，不把审核员或提交者猜作主办方、不削弱主办方约束。[缺陷与恢复条件](../research/CITY-SEED-ACTIVITY-PUBLISH-REGRESSION-2026-10-02.md)。

准确线性化点为最终payload SQL statement snapshot；不承诺之后网络中数据绝对撤回。11字段五受众、人类gateway及原源ACL与field policy相交是真实本地能力；无新Flutter设置页面、模型Profile读取、Memory/候选写、provider/视觉/A2A/自主动作或现实试点证据。AGENT_ONLY不授模型许可；普通源updated_at与native CAS仍分开。Closed Pilot / Consumer Beta均NO，原IdP/HTTPS、现实组织/活动授权、地图API、部署日志、值守、提醒运营与真实A→H门槛保留。[当前实测/历史与源码hash](../testing/evidence/agent-profile-visibility-2026-10-02/README.md)。


## 2026-10-06 AGE046：本人中文智能体资料页本地实现

本增量保留上文所有历史实现和原生权限边界。个人设置新增“我的智能体”，按本人资料与记忆、偏好、地点、活动、社群、智能体设置六组读取原领域真源。新页自身仅发送 GET，领域编辑沿用原页面与其具体人工检查；没有新增批准、模型用途、公开发布或身份切换。

068 私密资料保留原九项本人内容；069 记忆列表先呈类型/状态，点击原 Memory ID 才读取短租期原生详情。EXPLICIT 是本人声明，不是外部已核实事实；INFERRED 仅允许原待审/过期/丢弃预留形状，不支持 ACTIVE。070 Attention/Social/Autonomy 展示当前配置或未配置，设置不授予模型、保留、自动执行许可。社群兴趣不当作成员资格，共同报名不当作到场，地点声明不当作位置轨迹。

所有来源严格核闭集、本人和 Agent。401/403 或身份/账号/工作区、auth/client/base/getter/listener 替换永久退休原页并清私密数据；迟到响应不得回显。单项异常明确不可读，不用其它资料补空。记忆详情寿命取原服务器 observedAt/expiresAt 减从请求前开始的单调耗时，设备时间只收紧。其他缺乏原生短租期 DTO 的显示最多缓存 30 秒；这是客户端显示新鲜度，不是 PrivateRecord TTL、权限世代或授权续期。每次 GET 的发送与整个响应流共用 12 秒总期限，并有 2 MiB 响应上限。

真实本地初测保留：设置入口缺失 RED；1000 条 Memory 在 Column 中全挂载 RED，改为按视口挂载的 SliverList 后首/末行均少于 30 条挂载且末行可达。后续 target8-final 为 59 功能与 10 加载 PASS、0 FAIL/SKIP，test/analyze 两命令 0；349 Dart/pubspec 输入前后稳定，8 个改动文件已冻结。涵盖原设置入口回归、借用 client 零关闭、身份/transport ABA、短租期及网络迟到、键盘 Enter、语义 tap、48dp、320px/font3/IME260/light/dark。

以上是仓库定向与合成 widget 证据；根代理整仓 Flutter、真实095原生 wire 和当前手机入口验证仍待单独核证。widget 中文截图使用 Windows 测试字体，不是假称 Android 截图或真人数据。真实 TalkBack、OS/手机交互和生产运营本增量均 NOT_RUN；未实现概率校准、INFERRED ACTIVE、个性化/学习总开关或 AGE049 全部控制。原 Closed Pilot / Consumer Beta 仍 NO。原任务状态仅根 live queue 为准。

证据：[AGE046 审计](../research/BIRDTIE-V5-AGE-046-AUDIT.md)、[本地证据索引](../testing/evidence/agent-profile-page-2026-10-06/README.md)。适用 UX-CHECK-01/02/04/05/06/07/08/09/10/11/12/13/14/16；规则接入与 widget 语义检查不代替真实辅助技术验收。


### 2026-10-06：整仓入口回归与 freeze2（保留首轮 RED）

根首轮整仓349实际 analyze0/test1：1644功能 PASS、2 FAIL、169加载 PASS、0 skip，build未运行。两个失败是旧入口 fixture：近况按钮中心y619.2超过600px视口，scrollUntilVisible仅构建而未保证可命中；偏好按钮尚未由懒列表构建，直接ensureVisible无element。原路由、权限和接线未改。原raw位于`work/v5-age038-resume/root-whole-client04638ph/test.log`，首轮结果不能写成通过。

根精确追加两条旧测试scope后，只补真实scrollUntilVisible → ensureVisible → hitTestable再tap；原2次ties、禁止relationshipContext、服务端原偏好、读2次、回开及初始设置断言全部保留，没有skip或关闭warning。原8个AGE046源码不变。target9-freeze2：61功能+12加载 PASS、0失败/跳过，test/analyze0，349输入前后稳定，10owned已冻结。freeze1/target8/worker-final1均历史只读，新的compatibility-annex2保存原两test字节、精确diff、whole1真实RED引用及新目标原raw。

新root整仓349/Debugbuild、真实原生095 wire与手机入口验收仍待独立核证。没有以fixture GREEN冒称全包GREEN；phone/TalkBack/真实OSSecureStorage/真实设备性能仍未由本worker运行。发布Gate仍NO。


## 2026-10-06 AGE046本地闭合并立即领取AGE049（root38pw）

原AGE046已按CODE_AND_LOCAL_VERIFICATION DONE，Settings“我的智能体”与六组原要求实际实现，复用本人068/069/070及兴趣/报名原生路径，未捏造到访、出席、成员或个性化/学习开关。349输入全包 Flutter analyze/test/Debug build0，1646功能+169加载PASS/0FAIL-SKIP，原1608分支/multiplicity与61目标全部保留。首轮1644PASS/两lazy-scroll旧fixture FAIL原raw保留，实际构建/滚动/可命中tap修复，未弱化领域断言。真095原生HTTP+最终API Dart IO1PASS/0FAIL-SKIP，辅助Session原logout204，合成测试未称生产。root833files immutable archive docs/testing/evidence/agent-profile-page-2026-10-06/root-whole349/manifest.json SHA 4170acbd7ac69737b4f5a313618eef204c418afe1cb22589ee919f898a245bf9，proof work/v5-age038-resume/profile046-proof38pv/result.json。

新349Debug已真ADB安装/拉回同SHA4813f9d7943278576a4de7e5360b9a7a0fb74f7c23ce8f4e9a89db8ab931517a，应用数据未清。新首页显示未登录，原phoneSession的native idle期限19:17:08UTC早于19:52安装，不推断SecureStorage丢失。手机转到用户其它应用后停止所有输入；新页面真机六组、AT、当前349受控Profile比较NOT_RUN。曾attach后Lost connection，不能称现在debug在线；旧296真实120/60Hz比较不是受控性能通过。

AIR023首root回归过程终止，已确认runner41280/test39048不存在、工具handle失效且无result，保留7804PASS/7807RUN的部分raw，原因UNKNOWN，不算整仓PASS或产品FAIL；原raw发出owned children全部SQLabsent，精确停止parent无连接后仅清该库。新独立root-whole023-09538pu同986freeze原runner已实际重新启动，待完整结果，未开始Go写入。

AGE046完成后同轮领取P0 AGE049(privacy049_audit)，先只读真实五类隐私控制/原用途与native源，scratch独占scope；完整实现范围经审计后精确扩lease，不拿进程featureflag冒称个人持久开关。AIR020另一worker并行只读审计顺序/公平/causation预算，尚未领取/改源码。队列 {"DONE": 170, "BLOCKED": 14, "TODO": 60, "PARTIAL": 7, "IN_PROGRESS": 2}；P0 {"DONE": 113, "BLOCKED": 9, "PARTIAL": 6, "TODO": 17, "IN_PROGRESS": 2}。原其它任务、source、依赖、发布gate保留；007概率校准及027真实attendance未解除。model/真实自动写/Vision/A2A OFF。现实IdP/授权活动/HTTPS地图/部署日志/值守/调度/A→H未齐，Closed Pilot/Consumer Beta NO。


## 2026-10-07 AGE049：本人字段受众客户端消费

本增量复用 §11 与原 GET/PUT `/v1/me/agent-profile-visibility`，不新增权限、schema、私人内容镜像或后端写路径。设置中的“各项资料的可见范围”可直接读取完整11项、编辑五种受众、检查当前metadata版本及完整规则，再由本人明确一次提交 `{expectedVersion,rules}`。原普通资料编辑、九字段Private writer与整体资料ACL保持独立。未配置的名称/简介PUBLIC及另九项PRIVATE保持原值，GET不写；PUBLIC仍与原源权限相交。“本人智能体”对应AGENT_ONLY，对他人隐藏，仍需当前用途授权，不启模型、分析、学习或Memory。

社群选择只读取原 CommunityApi.mine 对应 `/v1/me/social-communities` 的当前列表，筛选status=active且myStatus=active；invited/pending和兴趣声明不授予成员资格。用户明确选择1–8个ID；列表最大100项，未列出不等于已退出。原字段规则已有的ID保留，不能借这些未核实ID给其他字段添加新目标。检查中标明未核实社群，完整PUT仍由原后端重新检查全部当前资格。CONNECTIONS不推断好友或自造名单，继续依赖原真实Tie/accepted request/Block resolver。此UI不是权限证明。

入口复用Settings的个人路由boundary、捕获身份/source epoch与稳定current；Page/Controller在身份、token、组织及来源变化后永久退休，前置通知后的真正send边界和迟到结果均检查当前代际。修改草稿、刷新或版本变化使本地检查失效；本地检查不等于native授权。409须新GET后重新检查；已发PUT无法撤回。响应丢失/503/异常成功形状显示结果未知，只GET核对当前设置，不自动重发、不以当前值相同宣称原请求造成成功。关页后重新读取仅是当前状态核对，本接口没有新的持久因果回执或operation journal。

证据：`docs/testing/evidence/agent-profile-visibility-consumer-2026-10-07/MANIFEST.json`。初始实际Settings缺入口1widget失败保留；首次GREEN尝试因新测试夹具null类型/缺getter编译失败，0行为执行，不作为产品/原生失败。修正夹具及原100项列表未核实ID处理后，直接相关3新测试文件一次执行16行为PASS、3加载、exit0、0错误/skip，28个相关输入前后稳定；它不是完整依赖图或全面回归。3个执行阶段的命令、CWD、原始日志、退出码及执行时源码均保留，旧16输入字节保持，green03 Settings在24行新增之外有11条旧行CRLF变LF；最终只恢复这些旧EOL，归一化内容不变且移除插入后原字节exact，未另行重测换行版。初用净CRLF差6误报行数已更正；本文原前缀保持。适用UX-CHECK-01/03/06/08/09/10/12/14/16仅为对应本地消费子场景，不代表整条验收通过。

未运行：旧tests、全量Flutter/Go、analyze、build、迁移、数据库、真实身份、真实成员撤权、OS/手机、TalkBack及性能。AGE049完整五族控制与Personalization总门禁仍PARTIAL；Closed Pilot与Consumer Beta NO，不沿用旧构建或实际运营证据。
