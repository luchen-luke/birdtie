# Introduction Policy V5：当前公开声明的人类只读建议

2026-10-04。BT-V5-AGE-043 **PARTIAL 建议**；状态由 root 独立核证后记录唯一 queue。本合同不替代 [Social Policy](AGENT-SOCIAL-INTERACTION-POLICY-V5.md)、原人类找搭子或发布门槛。

## 1. 可调用边界

`GET /v1/me/agent-introductions?sourceIntentId=<本人当前公开FIND_COMPANION UUID>` 是普通本人查看入口。当前 opaque Session digest 与 Person workspace 来自 server Authenticate；拒组织/Business身份、workspace header（包括空 header）、任意 owner/agent/peer/sourceBinding selector、重复 query 和 GET body。只有原始 current source ID 可选。

新 Store `ReadOwnIntroductionSuggestions(ctx, agentprofile.PrivateAccess, sourceIntentID)` 从真实 PG 重读账号、Personal Agent/metadata、Session、原052 consent、065 Social Settings和当前公开源；不接 JSON 控制对象、离线 Social Policy Decision 或 AGE033/034 本人 private Task grant。它不调用模型、候选记忆、Memory提升、A2A或发送动作。

输出 `human-introduction-suggestions-v1`、`HUMAN_REVIEW_ONLY`：中文解释、当前候选的稳定 Person/Intent ID、通用公开依据、最小 PUBLIC 名称（若该字段不公开则“Birdtie 成员”）、不可当许可的 current read receipt、PG observedAt/有效上界。`modelAccess/sendAllowed/memoryPromotionAllowed=false`，schema/闭集来源状态/候选数量/ID/期限/依据在 HTTP 输出前校验。Cache-Control=no-store。

## 2. 唯一 native 真源和四类差距

|原要求|实际本轮能力|限制|
|---|---|---|
|Shared interest|双方 PUBLIC ACTIVE FIND_COMPANION 明确 category；复用 `newpeople.Match` 当前兼容规则|只是当前意图类别声明，不读取私密偏好或推断长期兴趣|
|Shared city|同一当前公开意图明确 CITY；城市、明确 Place 均仍公开有效|不是本人所在地/距离/访问史；明确过期 Place 不退回 City|
|Shared community|条件性读取双方 PUBLIC person_contexts COMMUNITY affiliation/interest 与当前公开有效 Community 的最小声明元数据|不读取成员列表、不证明成员身份；现 `DeclarationInput` 无visibility、NormalizeDeclaration拒COMMUNITY，普通用户尚无公开发布该声明的入口。只有schema/resolver合成验证，不能称消费闭环完成|
|Shared activity|`UNAVAILABLE`|RSVP未公开、不证明到场；Moment普通公共DTO明确排除 activity/community links，具体公开预览没有审批这些关联。未扩大后台link的公开权限|

三类状态 `PUBLIC_DECLARATIONS_ONLY` 表示可被严格当前原生 resolver 消费的来源形状，不代表已有真实供给、所有人的共同关系或新发布入口。类别/城市目前复用真实人类创建草稿→本人激活领域路径；Community仅条件resolver。

## 3. 保守 Social Settings 与私密隔离

唯一 065 `agent_policy_settings` SOCIAL row、native_revision、valid_from、expires_at 是策略设置真源。不建第二 policy 表、许可、consent、预览账本或 grant。双方现052 opt-in和public Profile粗ACL仍必须成立；双向 Block、active Tie、pending Request、当前双方visible Intent共同收紧候选。

041七类闭集没有 SHARED_INTEREST/SHARED_CITY；本入口未知对象基础要求双方 UNKNOWN_PERSON=REVIEW_REQUIRED。如果确实有共同公开Community声明，还同时要求双方 SHARED_COMMUNITY=REVIEW_REQUIRED，任一关闭/缺少/过期优先。策略只抑制已经公开的候选，不授读取、消息、好友或模型许可。对方 Social Settings 是私密设置，SQL仅内部检验，不返回原文、revision、拒绝原因或逐人拒绝记录。空结果通用表达。

旧 `agentsocialpolicy.Service.Decide` 仍缺独立认知 purpose/source resolver，保持 Unavailable；本项没有将052 matching opt-in冒充其 SOCIAL_POLICY_PREFERENCE_EVALUATION consent。普通 human public read 不生成可被 Agent 机器读取复用的授权。原065真实持久设置后续覆盖041早期foundation“无native设置”的历史描述，不重写早期证据。

## 4. 当前版本、期限、并发

READ COMMITTED 同事务两次最小原生 projection。每一轮 SQL同一 current PG clock 检查当前源、双方 consent/policy/active身份、public scope、session及Block。先验证明确引用仍在；不从损失的明确Place/City猜替代资源。公开姓名只用 NULL viewer 的 PUBLIC字段规则，不从Connection/共同社群私密姓名补值。

内部来源 frame 含实际 selected Intent、账号/Agent/metadata、原consent、Social policy、public资源和公开Community声明的 native xmin/时间；Session绑定id/createdAt/authMethod/绝对期限/digest当前结果，正常 Authenticate idle变化不作稳定身份。对同一次读取两轮 source+实际入选候选原生frame做完全比较，变更409，当前失效403，不续旧有效上界。`sourceBinding`仅SHA当前查看receipt；API不接受它作新请求许可，不提供永久快照/缓存复活路径。

候选复用原052明确兼容规则，无第二rank/model系统；sql先限定当前同modality，最多考察101当前公开意图，按稳定Person/Intent排序，输出最多50去重Person。考察上限或输出上限到达则truncated。未入选不相关public声明不是本次human结果授权，不加入机器批准source set；AGE033/034被过滤源仍完整revalidate的规则不受影响。

自然到期按PG时间，实际 Session 撤销/过期在 native table lock等待后被最终当前查询拒绝。没有宣称数据库全局串行化、网络已显示结果可永久有效、已发引荐可撤回、跨请求审批或Session撤回历史账本；后续实际动作仍必须走原领域重新授权和本人具体批准。

## 5. 检查与未测范围

本项 [证据](../testing/evidence/agent-introduction-policy-2026-10-04/README.md)：fresh001–076、真实nonempty旧数据与完整public/catalog对照，定向native3 **45 PASS/0 FAIL/0 SKIP**、Go test/vet/build0、704全API源稳定、7owned稳定、自有库DROP。覆盖注册HTTP、双方设置/consent、private/撤回/expiry、city/Place、active主体/Block、原生policy ABA使旧receipt改变、等待后Session撤销/自然到期和无领域副作用。首两轮失败保留，root全仓验证另记，定向不冒充whole。

尚未运行本项 Flutter页面/真机/键盘/截图/TalkBack、完整用户评估、race instrumentation（CGO=0）、模型出口、真实消息/引荐、共同活动公开批准或Community人类发布入口。仅对应 UX-CHECK-06/08/10/11/16 的来源最小化、权限/撤权、真实拒绝与无正文日志边界，未完成整套UX验收。Closed Pilot Ready=NO，Consumer Beta Ready=NO；原IdP/真实组织与活动/HTTPS/Map/值守/提醒/部署A→H条件继续有效。

## 6. 2026-10-04 普通用户引荐与社交偏好界面增量

前节未运行客户端是原生批次的历史边界；本次在原PARTIAL之内增量补齐本人普通界面，未解除完整需求门槛。Settings“引荐与社交许可”打开当前本人公开意图与原070 SOCIAL设置。来源从原`me/new-people/intents`重读，仅当前本人PUBLIC/ACTIVE/FIND_COMPANION且未到期才可选；实际native历史状态MATCHED/CONVERTED及FRIENDS/LOCAL等受众可读取但不可冒充公开来源。未开启consent或缺来源时回原“找新朋友”管理页，不创建假UUID/意图或自动开启。

社交偏好编辑复用原`GET me/agent-policies`与`PUT me/agent-policies/social`七类完整CAS，具体版本、修改项和期限先检查再确认；其他五类原值保留。偏好仅允许本人审阅，不授予对方消息、模型或Memory许可。冲突或未知结果先重读原设置，不盲目重发或续期。换账号/Session/workspace或API transport退休旧正文、候选与确认。

当前候选按原稳定accountId进入现`ChatEntityDetail(type:person)`和原公开Person详情；检查好友申请回原`NewPeoplePage`，该页重新读当前本人consent与意图才初选带入的PUBLIC来源。不直接POST邀请、不合成Conversation，不继承引荐查看回执为申请许可。原管理页已修复同key transport仍沿用旧client的真实RED，组织切换清草稿/候选并关闭旧邀请对话，切回个人需重新选择，不恢复旧批准。未知邀请结果仍使用原收件箱核实路径。

当前新六Dart定向23 PASS/analyze0；根共享Settings/原管理页及聊天实体详情定向38 PASS/analyze0，包括跨owner来源拒绝的实际RED修复。worker复验原生依赖308 PASS/0 FAIL-SKIP/pkgFail、test/vet/build0、727 API源稳定、旧public/catalog保持、fresh076 up/down/reapply和独占DB DROP；这些是既有原生接口复验，不是新增Go实现或全仓结果。证据在`work/v5-age043-ui/`及`work/v5-age038-resume/shared-introduction-*`，首次enum/UTF8/transport失败保留。**新的合并全Flutter、Debug构建及真机尚待执行**，旧741帧不覆盖这些新文件。TalkBack、键盘（此引荐页没有自由文本）、现实双人引荐和完整消费验收未测。

### 2026-10-04 合并复核补充

后续定向六Dart24 PASS/原生注册12响应实际→Dart严格解析、原生依赖308 PASS；root共享39 PASS/analyze0。新增系统返回键真实RED已修（首次void callback编译失败保留），32回归PASS后wholeFlutter807功能+104loading/analyze-test-Debug构建0/234源稳定；原727Go与全9299帧SHA一致，本轮无新Go/DDL。1d827 Debug真机当前关闭态/取消0PUT/原管理流程、系统back逐级返回实际核证。入口scope还覆盖API transport重绑与全部nested确认窗口；永久失效不能A-B-A复活。证据root-code-final3与phone-introduction-business2。旧本节“尚未构建”是原检查点历史；当前完整SHARED_ACTIVITY/Community发布闭环仍未完成，阳性双人引荐/TalkBack/生产身份及试点未跑，整个AGE043仍未达DONE。

## 7. Community 兴趣声明增量合同（待实现、待验收）

本节在原043自然检查点接续，唯一任务仍为原队列。不是另一份产品规范、权限台账或已经可用的服务。root保留077迁移号与共享路由/Settings接线，memory_decay只在明确lease实施。本轮首版只提供本人 `interest`：中文“我对这个社群感兴趣”，不开放 `affiliation`，不表示成员、学历、现实关系或到场。

### 7.1 真源与普通人类路径

- 唯一声明仍是033 `contexts` 的 COMMUNITY FK 与 `person_contexts(person_account_id,context_id,relation='interest',visibility)`。不复制成员或偏好，不新增 statement/policy/grant 表。PRIVATE默认；PUBLIC是本人检查具体社群、原状态、后果后批准的声明，不由checkbox、模型或旧Community管理批准推导。
- 独立本人路径拟为 `GET /v1/me/community-interests`（自己的最小当前声明）、`GET /v1/me/community-interests/options`（当前可公开声明的社群）、`POST /v1/me/community-interests/preview`（准备具体操作）、`POST /v1/me/community-interests/approve`（仅提交服务器具体预览）。最终路由以实施与注册证据确认，不能宣称文件存在便可调用。拒组织/Business/workspace selector、任意owner/Agent selector、未知字段和重复参数。
- options只给当前公开、published、active、owner-confirmed、有效Community及其有效City的最小ID/公开名称，不返回成员、MyRole、私有rightsNote或非公开精确资料。自己的旧声明仍可撤回/设为private；资源hidden/expired时不显示其隐藏名称，不把资源消失当作批准已撤回或声明已删除。
- PUBLIC只作为现043普通当前公开来源的一个条件；双方原052 opt-in、070 SOCIAL关闭/期限、双方可见公开意图、Block与原ACL仍分别生效。没有通知、加入、follow、好友申请、Conversation、Memory提升、模型出网或认知 grant。读取/准备预览均零领域写入。

### 7.2 具体版本批准、并发与恢复

预览由服务端进程随机密钥 AEAD 密封，短期有效（最多90秒，并受Session绝对期限/当前公开源有效上界限制）。密封内部绑定当前本人Account、PersonalAgent及metadata、原Session稳定身份、Community/City/context当前源frame、本人旧interest行xmin或明确absence、操作/目标visibility、该本人+Community+interest审计epoch、issued/expires/nonce。DTO仅展示必要领域ID、当前允许的公开名称、具体原/新状态、中文后果、预览期限；不泄露Session摘要、xmin或内部key。客户端`confirmed`、body digest或policy receipt都不是批准凭据。进程重启使旧预览失效；不声称持久preview、跨重启幂等回执或网络exactly-once。

批准用原生事务串行锁本人、原声明和必要源，遵循实际领域锁顺序避免死锁；不存在的本人声明由本人Account锁防并发首次写。**所有锁/表等待完成后**，用当前PG clock重新检查Account/Agent/metadata/Session未撤销、绝对/idle期限、操作/原声明frame、source有效性和审计epoch；与密封版本逐项一致才在原 `person_contexts` 写PUBLIC/PRIVATE或删除。上下文节点只能在批准事务中创建/复用精确Community FK，不在GET/preview创建。

每个真正变更在同事务追加原`audit_events`最小元数据，最终检查当前源、Session和该操作新增auditID仍为本资源max，再提交。成功操作必递增epoch；第一次成功、其他并发批准、撤回重建或PUBLIC→PRIVATE→PUBLIC后旧预览不得复活。状态相等不是批准被消费的证明，重复/不一致返回当前冲突，不能报假成功；未知结果先只读权威当前声明，明确结果未能确认，重新检查预览，不能自动重发旧批准。

### 7.3 077 窄审计保护与迁移验收

现001 `audit_events` 没有不可变保护，不能只拿max(id)/xmin/count作不可逆epoch。077只保护新闭集 `resource_type='person_community_declaration'` + `purpose='HUMAN_COMMUNITY_INTEREST_DECLARATION'`；resource_id为规范Community UUID加`:interest`，actor为本人，allowed action为具体PUBLIC/PRIVATE/撤回。拒保护资源UPDATE/DELETE及改走其他type/purpose；存在新保护历史时 statement-level BEFORE TRUNCATE 原子拒绝清空，不能靠清审计使原absence预览复活。无保护历史时旧资源审计保持原行为；只新增查询索引和保护机制，不建第二授权真源。当前没有账户硬删除产品路径；保护审计的原Account FK意味着未来硬删除/数据保留方案必须单独协调，不能暗中删除本历史来恢复旧批准。

077 down仅限隔离本地无此资源历史时运行；存在保护历史须原子拒绝而保持迁移/原历史，不能通过关闭trigger或删除历史强行回滚。验收需fresh up、无新历史down/reapply、带合法操作历史down拒绝、完整旧非空public行/catalog保持及独占DB清理。原旧 `RemoveContextDeclaration` 的COMMUNITY删除旁路须拒绝；CITY/INSTITUTION/ONLINE原私密行为保持。特权DDL管理员可修改schema属于单独运维权限，不宣称本规则对恶意数据库管理员防篡改。

GET、preview和approve都检查实际 row guard 与 statement truncate guard 的种类、启用状态、同一保护函数和索引；未迁移、缺失或禁用保护时503且零声明/审计副作用。077当前仅用于隔离本地验收，未执行生产迁移；无可靠低权限生产角色配置证据，不能将 app role 能清空历史笼统归入“DDL管理员不在范围”。普通TRUNCATE的实际RED已复现并修复。后续禁用trigger/修改schema/运维授权属于独立边界，不能宣称全面防篡改。

### 7.4 期限与验收边界

声明持续至本人撤回/设为private，没有臆造原表不存在的声明expiry；预览90秒不等于声明90秒。关联社群/City隐藏、取消、归档或到期后公开投影与共同依据立即不可用，源恢复后仍须当前完整检查；不把后台自动再公开或重建当本人新批准。需测当前公开正例、PRIVATE/hidden/expired/source修改、跨owner/Session/组织、等待后撤权及自然到期、竞争/ABA/重启、未知结果、旧旁路零写和副作用隔离，并提供中文普通路径/具体预览/取消/换身份迟到/窄屏大字证据。

本节是待实现合同。SHARED_ACTIVITY仍UNAVAILABLE；048共同报名展示许可、后台Moment Activity link和旧Moment publication token不能当匿名公开声明或机器许可。完整043、Closed Pilot、Consumer Beta门槛保持，只有实际代码/迁移/API/客户端与真机场景获得证据后才更新相应实现状态。

## 8. 共同公开报名：接续实施合同（当前未实现）

2026-10-04自然安全检查点接入。共同活动依据只可描述为**双方逐活动明确公开的当前报名**，参加或到场事实仍UNKNOWN。原048全局shared_activities开关、成员、私密RSVP、Moment关联都不授逐活动公开许可。读本人原报名与本人选项只是准备；检查具体版本、有限受众/期限后，单独批准才改变披露。

### 8.1 原事实、默认与有限公开

- 唯一报名事实仍原`activity_participations`，原Activity/Participation/Person IDs、原going/pending/cancelled、报名取消API及Plans保持。不建参与副本、Memory/context类别、第二consent或preview账本。
- 078迁移拟在原参与行附加默认PRIVATE的披露字段、原行单调版本、批准活动版本与最小public-source-frame绑定。合法取消、状态变更和重新报名须清除原公开状态；同一原行ID重用不复活原批准。窄触发器不得在Cancel已锁Participation后逆向锁Activity。
- PUBLIC须本人当前going/cancelledAt NULL、当前PUBLIC/published/未取消且未结束的Activity，所有实际引用的City/Place/Venue/Organizer当前公开有效并且ACL允许；不从缺少或过期明确来源推断替代事实。批准界面要具体显示活动允许公开名称、起止时间、本人报名、受众、期限及“不代表到场/成员，不发送消息”。
- 首版公开期限由人类明确检查选择，**最多24小时且不超过当下所有来源有效上界及Activity.endsAt**。不是无限公开，不能把90秒预览期限当公开TTL。PRIVATE撤回只隐藏披露，不取消报名；隐藏/失效旧来源以中性名称允许本人撤回。
- 当前public-source-frame只包含实际必需的公开标量、原Activity revision/xmin及实际相关源native版本，不密封私密整行。当前每次重新计算并匹配，改期、隐藏再恢复、合法源ABA不恢复旧披露。披露自身变更字段不能参与其来源事实digest导致自失效。

### 8.2 具体批准与权威结果

拟独立本人路径`/v1/me/activity-participation-disclosures`及`/options`、`/preview`、`/approve`，只有实际注册/JSON/测试证据后才称可调用。严格当前原Session/Person/PersonalAgent，拒组织/Business/header/query任意主体选择及未知/重复/trailing/null字段；GET/preview零领域写，列表bounded/truncated。

服务端AEAD进程随机key的短期预览≤90秒且≤Session/源期限，绑定原Session稳定身份、本人/Agent/metadata、原参与行版本/事实、具体动作/公开expiry、当前source frame、原保护审计epoch。客户端`confirmed`、digest、旧receipt不批准；重启使旧token失效。新Session不继承原操作权限，已提交人类PUBLIC领域状态依其本身具体版本与期限继续存在。

批准先取得实际写表relation锁并遵循已核领域锁序，优先Activity→原Participation；身份读锁不得复制Community Account UPDATE造成原业务锁逆序。所有实际等待后用PG clock/同snapshot重新检查当前Session/身份/source/原参与行/原epoch，再写原披露元数据与原审计；审计等待后再作最终current/time guard。40P01/timeout原子rollback并明确失败，不自动重发旧批准。相等状态不证明本次成功；重复token/版本变更拒绝，未知回复只读当前状态，不自动重试。实际权威结果必须绑定原本人/Agent/Activity/Participation/具体目标和native观测时间。

### 8.3 原审计保护、迁移与引荐消费

078仅为新闭集`activity_participation_disclosure`/`HUMAN_ACTIVITY_PARTICIPATION_DISCLOSURE`增加独立窄审计INSERT约束、UPDATE/DELETE/TRUNCATE保护与epoch查询；不能改写077 Community合同或已有账本。不宣称对恶意DDL管理员防篡改。未迁移/缺失/禁用实际guard时Unavailable零写；有披露历史/有效披露/已用版本lineage时down原子拒绝。未使用时隔离up/down/reapply要保留旧全部列、ID、语义、完整旧行投影与catalog，新增PRIVATE基线不当旧业务变更。

Introduction原当前两次frame追加最多一条共同当前PUBLIC报名依据，持久具体source version与期限纳入两次比较/response期限。保持双方原052 opt-in、公开Profile/当前公开FIND_COMPANION、UNKNOWN_PERSON及SHARED_ACTIVITY=REVIEW_REQUIRED、048全局展示限制、双侧Block/Tie/请求等原收紧条件。不查询或返回对方私密报名正文、名单、策略或其余活动。旧global SharedSocialContext/RelationshipContext RSVP查询不充当新匿名公开源。不授消息、好友、Memory或模型权限。

### 8.4 实际验收门槛

原Store发布本地明确测试Activity→两普通人原Join→分别具体PUBLIC预览/批准→原Introduction出现共同公开报名；global-only、单方公开、pending/cancelled/PRIVATE/hidden/invite-only/expiry/改期、source或主体ABA/撤回重报、策略关闭/缺opt-in均负例。真实等待后的Session/自然expiry、竞争/旧token/进程重启/未知回复、副作用隔离与原报名/Plans/通知回归必须实际执行。需fresh/current非空旧数据迁移投影、保护历史拒down/TRUNCATE、实际registered HTTP JSON到Dart、中文具体版本/取消/窄屏大字/迟到/手机与辅助技术证据，未运行如实记录。

当前仅审计与合同，SHARED_ACTIVITY仍UNAVAILABLE；不能把新文件、纯fixture或预留迁移号称完成，不能让本节解除机器purpose、默认OFF或原Closed Pilot/Consumer Beta门槛。

### 078 真机生命周期检查点26

本地Debug APK125cd、冻结Go9477/API078原帧：原RSVP创建PRIVATE v1，显式有限PUBLIC v2、隐藏PRIVATE v3、不刷新PUBLIC v4；取消/返回不写，原报名事实保持。实际API OS重启旧opaque409且审计/公开不变；App重启原生GET保持。原取消报名使同ID cancelled/PRIVATE v5、公开元数据清除、3旧审计保留。记录证据 `work/v5-age038-resume/phone-activity078-2/actual-acceptance-checkpoint26.json`，不是出席/模型/成员/消息许可。另发现共同社群City source绑定/期限缺口已真实RED并修2Go源，最新全仓仍进行；旧9477/此手机binary不冒充新City终验。TalkBack/双人普通完整引荐消费未跑，ClosedPilot NO。
