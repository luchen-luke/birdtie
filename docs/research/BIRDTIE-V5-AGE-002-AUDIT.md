# BT-V5-AGE-002 — Public / Private Agent Profile 分离审计

日期：2026-10-02。范围：`BT-V5-AGE-002`；§1–5保留任务开始时的实际边界、拟实施范围与验收要求，§6分别记录只读代码审查、草稿历史及最终实际验证。已读取根任务store/HTTP/迁移/全Go证据，限定隔离本地验收完成；最终状态仍在原live队列。未部署正式环境，没有新消费Private UI。

## 1. 来源与唯一规范

来源是 [AGE原文](../product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md) 第286行AGE-002；Public是别人获准可见的信息，Private只供本人Personal Agent的私人用途，Agent知道信息不等于可以公开。第344行AGE-003的逐字段PUBLIC / CONNECTIONS / COMMUNITY / PRIVATE / AGENT_ONLY另为后续任务。

复用唯一 [AgentProfile基础规范](../architecture/AGENT-PROFILE-FOUNDATION-V5.md)，在其内增量定义本轮内容边界，不新建第二份Profile canonical。沿用 [认知ADR](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md)、[Memory](../architecture/AGENT-MEMORY-ARCHITECTURE.md)、[Context Access](../architecture/AGENT-CONTEXT-ACCESS-POLICY.md)、[身份](../architecture/AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md)及原Business/Community无私人继承规则。

AGE001仅六字段metadata、053绑定与内部Get/Ensure已凭 [独立证据](../testing/evidence/agent-profile-foundation-2026-10-02/README.md) 完成；其精确binding primitive不是面向真人的私密内容授权gateway。

## 2. 已有Public / 私密内容 / 发现代码基线

| 已读路径 | 当前实际行为 | 本轮复用或缺口 |
| --- | --- | --- |
| `identity/identity.go:28` | `identity.Profile`实际JSON仅accountId / displayName / bio / visibility | REUSE普通资料；源文avatar/current_city/interests/公开内容列表是示例目标，不宣称现API全部已有 |
| `postgres/identity.go:42`、`httpapi/identity.go:92` | 活跃目标账号；public、本人或当前profile_view特定grant读取；双方Block优先拒绝；不可见返回not_found | Public投影保持原ACL及响应字段，不join新Private内容 |
| `postgres/profile_edit.go:9`、`httpapi/identity.go:67` | 当前会话本人修改普通资料，严格JSON、有限displayName/bio、public/private；隐藏时撤回原公开Intent并记录审计 | 不改成新Private写路径；旧visibility不是Memory/分析/模型出口同意 |
| `httpapi/server.go:249,272–275` | 普通本人Profile编辑、本人profile grants及按accountID普通资料读取 | 不复用profile_view grant读取新的PrivateAgentProfile；不建立按他人AgentID读取私密内容的通用接口 |
| `postgres/identity.go:102,163` | 明确recipient/purpose/read/expiry grant，撤销增加revision；Block、过期、撤销不能继续授权 | 独立许可边界继续保留；好友、组织角色、051/052或AGA不继承 |
| `httpapi/person_contexts.go:11`、`postgres/person_contexts.go:22` | 本人Person路由读取本人CITY/INSTITUTION/ONLINE声明，声明存private | 不将Context当前位置/历史自动复制Private或公开Profile，不当位置/学历证明 |
| `httpapi/moments.go:15`、`postgres/moments.go:74,94` | 有效Person本人Moment草稿/内容列表及本人ID详情；私人链接/编辑/撤回走原领域 | 不把正文、Activity/Place链接自动做偏好、Memory或Public列表 |
| `postgres/activities.go`、`activity_publish.go` | 原公开发布、活动受众、Block、主办方/场地状态控制发现与详情 | Private偏好写入不能发布活动、改RSVP/Plans或改变当前发现/供给 |
| `postgres/new_people.go:155`、`social_intents.go:358` | 新朋友公开信号使用显式PUBLIC Intent和公开普通Profile；PUBLIC激活核普通Profile | 不读取新Private字段或把社交偏好自动变成PUBLIC Intent |
| `postgres/social_privacy_integration_test.go:73–106` | 既有测试证明grant可读普通private Profile但不发布Intent，public仍受Block，撤权不会因解除Block复活 | 本轮保留此边界，再用Private字段canary验证所有旧公共响应无泄露 |

基线缺口：无独立持久Private内容模型/本人安全store/当前会话HTTPgateway；AGE001metadata没有这类字段。现普通Profile和Context/Moment也不能当成PrivateAgentProfile真源。

## 3. 本轮拟实施的最小私人内容

仅接受本人明确输入的九类字段，不从模型、报名、聊天、Moment、Context、目录或账号类型推断。字段名和上限最终按真实代码/合同核对，未落地前本表不当作现API。

| 输入域 | 拟内容形状 | 必须保持的含义 |
| --- | --- | --- |
| personalPreferences | 有界文本数组 | 用户自己表述的偏好，非推断画像 |
| socialPreferences | 有界文本数组 | 私人协调偏好，不等于对方意愿/匹配授权 |
| preferredActivityTypes | 有界文本数组 | 明确偏好，不由RSVP/收藏生成 |
| travelPreferences | 有界文本数组 | 偏好，不是实际旅行或到访证明 |
| interactionPreferences | 有界文本数组 | 用户声明，不授予主动消息/A2A许可 |
| languagePreferences | 有界文本数组 | 语言偏好，不推断民族/国籍/语言能力 |
| availability | 有界人工文本 | 用户说明，空值为未说明，不承诺真实档期/可预约 |
| privateCityHistory | 有界人工文本 | 私人叙述，不是当前定位或核验居住历史 |
| agentNotes | 有界人工文本 | 本人备忘，不作为Agent指令、policy override或可自动执行内容 |

空数组/空文本表示没有填写，不能变成“没有兴趣/不愿社交/空闲”等事实；可通过明确更新清空。持久记录只保存直接输入与必要主体/版本/来源性质，不自动出模型、不写Memory/推断candidate、不公开。自声明不能覆盖服务端身份、成员、claim、出席等权威事实。

## 4. 本人安全store / HTTP验收边界

本轮只面向有效当前Person会话的本人Personal Agent，不接受request owner/agent/account参数作为权限。至少在读与最终写事务核当前session（未撤销/过期）、active account、精确Personal Agent和metadata绑定；组织/Business账号、外部workspace selector、他人的Private内容和未知角色均拒绝。另一合法Person访问self路由只能返回其本人记录/空值，不能读取前一Person的内容；这不要求拒绝其正常本人操作。

写入由本人主动保存请求、明确提交内容和当前旧profile_version共同约束；身份与权限仍由服务器读取，不添加可自证权限的confirmed字段或模型批准路径。改稿/换主体/版本冲突、会话撤销、Agent停用必须拒绝，不覆盖旧版本；重复/无变更行为及并发保证按真实实现和测试记录。

严格有限JSON、未知字段拒绝、数组项数/单项/总payload及文本上限、有效UTF8/不合法输入、响应no-store与日志脱敏均需实际核验。私密正文不进入audit_events、请求日志、trace、公共Profile、发现、聊天卡片或错误响应；审计只存必要动作/主体/资源/版本信息。

Public继续走原identity.Profile/read ACL；private字段改动不改变user_profiles.visibility、不生成PUBLIC Intent、不修改Context/Moment/Activity/Plans、关系或新朋友授权。个人Profile grant、公开Profile、组织成员权限与所有认知许可分开。

### 4.1 任务开始时确定的目标合同（实码与结果见§6）

根任务已确定GET / PUT `/v1/me/agent-private-profile`：无caller指定Agent/owner或外部workspace，所有query拒绝；由有效bearer派生PERSON并由store核当前session及本人稳定Personal Agent。PUT仅 `{expectedVersion, fields}`，拒未知/重复/null字段；没有confirmed或modelUse/scope共享字段。

六个列表每个至多20项、每项160rune；availability/privateCityHistory/agentNotes每个至多2000rune，fields限12KiB、HTTP body限16KiB。全空表示显式清空，在版本+1事务中删除Private行并返回configured=false；缺行GET返回空/未配置，不写表。这里保留任务开始时的计划，不用计划自证完成；最终已按§6分别复查实际代码与独立运行证据。

## 5. 适用验收

| 层次 | 必须实际覆盖 |
| --- | --- |
| 模型 | 九类直接输入；正确边界/空值/清空；非法/超长/未知字段；不注入owner/Agent/visibility/confirmed为权威 |
| 持久 | 同账号/Agent稳定、版本不倒退、显式保存、并发冲突/重复/重连、仅本人的数据；fixtures独占和精确清理 |
| HTTP身份 | 匿名、过期/撤销session、另一Person、organization/business、停用Account/Agent；正确self读取与确认写；换token/最终身份拒绝 |
| 分离 | 同时写有独特canary的9个Private字段后，匿名/好友/grantee/组织管理者只获原普通Profile字段或原不可见结果；全部公共发现响应没有canary，读后再撤grant/Block仍守原ACL |
| 兼容 | 原普通Profile编辑、profile_view grant、Context/Moment、公开活动详情/RSVP/Plans、新朋友显式PUBLIC信号不变；原ID不变 |
| schema | 增量迁移fresh/current-data up、旧metadata/Profile/领域数据保留、直接SQL不跨主体、内容有数据down拒绝、空库down/reapply |
| 认知端口 | Private持久存在不开放Memory、cognitive读取、ModelContextEgress、provider、自动消息或A2A；不把owner输入自动喂给Runtime |

本轮无Flutter/UI新增，中文消费页、真机、IME、TalkBack和无提示用户观察**未实施/不适用**；原页面兼容检查不称新的Private编辑UI验收。Go/DB/HTTP成功与失败必须由实际命令留证后才标完成；此前AGE001的1302回归不是AGE002的功能验收。

## 6. 当前审阅 / 运行状态

本记录完成已有Public/Profile grants/Context/Moment/发现边界只读审计。其后已读 `agentprofile/private.go`、`postgres/agent_private_profile.go` 与054 up/down：实际九字段规范化模型、server-only PrivateAccess拒JSON、Person session/Account/精确Personal Agent share锁、metadata更新锁/CAS、替换/清空和最终wall-clock session复查均与目标边界一致；错误统一为不带私密JSON的固定域错误。此为代码审閱，不代表HTTP/迁移/持久运行已经通过。

### 6.1 草稿SQL兼容检查，最终DDL已改正

054初版Private trigger使用 `jsonb_object_length(NEW.fields)`。审计者对实际本地数据库执行只读catalog查询：`docker exec api-db-1 psql -v ON_ERROR_STOP=1 -U birdtie -d postgres -Atc "SELECT version(); SELECT count(*) FROM pg_proc WHERE proname='jsonb_object_length'; SELECT count(*) FROM pg_proc WHERE proname='jsonb_object_keys';"`。

结果为PostgreSQL16.15、object_length计数0、object_keys计数1。根任务确认该早期草稿尚未用于migration运行，并已在首次实测前改为 `(SELECT count(*) FROM jsonb_object_keys(NEW.fields))<>9`；审计者重新读取最新054确认最终DDL已使用此实际可用表达式。因此记录为只读草稿发现及预先改正，**不是实际migration FAIL或仍待修阻碍**。文档审计者没有改迁移或数据库；最终实际Private写入/约束运行证据现已在§6.4读取。

### 6.2 HTTP实码核对

已读 `httpapi/agent_private_profile.go` 与server真实GET/PUT注册：有效bearer派生PERSON，非空query及GET body拒绝；无owner/Agent/workspace选择，PUT只expectedVersion/fields，完整替换和严格domain解码。no-store贯穿身份/错误/正常返回，PrivateAccess拒JSON；响应核PERSON/owner，错误与日志只含固定分类/request ID而不带私密JSON或PG DETAIL。表及store的读取/替换只从本人原稳定Personal Agent解析。

上述实码审查时Root PrivateStore、HTTP与migration仍在运行，未提前记Private读取/保存PASS；最终证据完成后按§6.4单独记录。AGE001测试不代替002验收，文档审计者未修改队列、来源或实现。

### 6.3 稳定源码的第二轮权限复核

本轮只读复核最终054、domain、store、HTTP及原身份入口，未发现阻止AGE002限定范围验收的新增越权或公共投影泄露。该结论是源码审查，不代替根任务正在执行的运行结果。

| 边界 | 实际核对位置与结果 |
| --- | --- |
| 私密所有权与写入版本 | `migrations/054_agent_private_profiles.sql:8` 的Person-only owner、metadata完整复合FK；`:32` 重读当前aggregate revision；`:42` 写入必须匹配当前版本；`:50` 更新不得重用旧written版本；`:58` 完整九键及后续值类型/长度检查。`:35` 允许真实父身份删除级联，普通clear须先推进版本；054 down在任何Private行存在时拒绝。 |
| 当前真人会话与稳定Agent | `postgres/agent_private_profile.go:140` 在同一事务精确匹配session digest + Person owner + active Personal Agent，并share锁住三行；`:163` 读share/写update锁metadata；`:182` 锁等待后再次核wall-clock session期限。账号/Agent停用、错误token绑定或组织/商家工作台不返回Private内容。 |
| 完整替换、清空和失败 | 同文件`:71` expectedVersion/CAS推进唯一metadata版本，再写Private或全空删除；中途错误和最终期限失败回滚全部效果。数据库错误不透传失败行JSON，重复旧版本不覆盖当前内容。`written_profile_version`仅最后内容写入关联，不是另一个对外版本。 |
| HTTP主体与请求闭集 | `httpapi/agent_private_profile.go:24` 先设no-store，出现`X-Birdtie-Organization-Workspace`即拒绝（含空值或重复头）；原`httpapi/identity.go:15` 只接受一个有效Authorization。仅本人GET/PUT路由，不接受owner/Agent/workspace选择；非空query、GET body、PUT未知/重复/null字段拒绝。响应再次核PERSON及本人owner，错误与日志不回显输入、token或PG DETAIL。 |
| 直接输入不授认知许可 | `agentprofile/private.go:27` 明确九字段是owner说明；`:81` PrivateAccess拒JSON；`:331` PUT解码只有expectedVersion/fields，不能夹入confirmed、visibility、policy、grant或主体覆盖。字段保存未修改认知Eligibility、Unavailable端口、公开UserProfile或领域许可。 |
| 公共与其他域无Private消费 | 非测试引用搜索显示Private表只在专属store被读取/修改，两个store方法只由该self HTTP调用；原Profile ACL、Context/Moment、公开活动、公开Intent/机会检索和CurrentDomainAdapter没有新增Private join。原`profile_view` grant、好友与组织角色仍只沿各自已有领域权限。 |

已只读核对stable测试源码含全部九字段canary、普通Profile四字段、grant撤销/Block不继承、另一有效Person仅读自己空记录、组织workspace头、跨digest主体、账号/Agent停用、读写锁等待期间session到期、CAS冲突与独立数据库连接重读。源码本身不推断测试PASS；实际执行日志/counts另见§6.4。

文档历史边界已收敛：Foundation §7 将“AGE002仍待后续/没有新HTTP”明确限定为AGE001交付当时，当前已交付AGE002以同一canonical §10及独立证据为准；没有重写源快照或重复新建规范。

### 6.4 根任务实际验收与独立证据复核

实际结果见 [证据README](../testing/evidence/agent-private-profile-2026-10-02/README.md)、[机器汇总](../testing/evidence/agent-private-profile-2026-10-02/verification-summary.json)、[manifest](../testing/evidence/agent-private-profile-2026-10-02/manifest.json)及 [复现入口](../testing/evidence/agent-private-profile-2026-10-02/reproduce.ps1)。审计者独立读取十份JSONL重新统计run/pass/fail/test skip，核对38个manifest文件SHA256无不一致；没有重跑根任务检查或替换原始日志。

| 已实际运行项目 | 结果 |
| --- | --- |
| Private model / 全Profile model | 87 / 124 PASS，0 fail / test skip |
| Private PostgreSQL store / 四包默认并发 | 34 / 1012 PASS，0 fail / test skip |
| HTTP spy / spy + 真实注册路由PostgreSQL | 161 / 174（161 spy + 13集成）PASS，0 fail / test skip |
| 默认完整Go三轮 / 迁移单独完整Go | 各1597 PASS，0 fail / test skip |
| 全量vet / build | exit0 |
| 054两个自有库fresh / seeded up | 每库33 strict SQL拒绝 + 合法CAS/write/clear/cascade；旧全行/版本保留，无Private回填 |
| 非空down / 空down-reapply | 非空exit3且完整Private/版本/旧行不变；明确CAS清空后空private down/reapply保留metadata及Public |
| 故障、时效、持久与清理 | 原子rollback/redaction、锁等待到期拒绝、独立连接恢复、全空v3/delete、随机fixture精准清理及自有数据库删除通过 |

PASS含主测试与子场景，不是功能数或真实用户数；无测试文件包的package skip不算测试skip。HTTP真实结果检验全部九字段canary不出现在普通Profile/grant响应中；被拒身份/选择器不能写入，本人内容编辑不更改任务、RSVP/Plans、关系、Context、公共资料或独立许可。根当前编译API ready200，五个未开放认知/协调端点404只是关闭边界探测，不是这些能力完成。

首次migration fixture仅允许P0001/23514而误判PostgreSQL以22023拒绝非法scalar JSON；第一轮原始失败已保留，修fixture接受正确拒绝SQLSTATE后第二轮DDL及第三轮完整回归通过，054/store实现不因该误判修改。Store初次runner相对日志路径错误发生在测试启动前，改绝对路径后执行通过。早期未运行SQL草稿的问题见§6.1，未伪记运行失败。

全部资料/会话为隔离本地合成验证；没有真实IdP、CSSA授权、正式部署、新Private编辑UI或消费者观察。AGE003已接续，但其逐字段可见性不能从002这些结果自动标PASS。

逐字段audience（AGE003）、独立purpose/consent、Runtime读授权、Memory/Evidence及provider仍为后续；没有Private内容消费UI或真实启用。**Closed Pilot Ready：NO；Consumer Beta Ready：NO。** 正式身份、授权组织/活动、HTTPS/地图/部署日志/值守/调度和A→H发布门禁保留。
