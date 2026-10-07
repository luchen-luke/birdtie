# BT-V5-AGE-003 — Profile Field Visibility 审计

日期：2026-10-02。范围：BT-V5-AGE-003；§1–4为实施前审计，§5保留首次实现与重开历史，本页末尾记录全部补验后的2780完整Go/055全源及已知独立缺陷。唯一live状态仍在原队列；无正式部署。

## 1. 来源与唯一规范

来源：[AGE原文](../product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md) 第344行AGE-003。每个可配置Profile field需支持至少`PUBLIC / CONNECTIONS / COMMUNITY / PRIVATE / AGENT_ONLY`，不能仅用`is_public:bool`。原文没有给出Community目标、AGENT_ONLY认知使用同意、匿名/账号失效及修改事务的完整规则，不用枚举存在代替服务器授权实现。

沿用唯一 [Profile Foundation](../architecture/AGENT-PROFILE-FOUNDATION-V5.md)，不建立第二套Profile规范或复制Public内容；[认知ADR](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md)、[Memory](../architecture/AGENT-MEMORY-ARCHITECTURE.md)、[Context Access](../architecture/AGENT-CONTEXT-ACCESS-POLICY.md)、[Community模型](../architecture/COMMUNITY-AND-ACTIVITY-SOCIAL-MODEL.md)、[共同关系披露](../architecture/SHARED-SOCIAL-CONTEXT-V4.md)保持各自职责。

[137来源对账](BIRDTIE-V5-MATERIAL-RECONCILIATION-2026-10-02.md) 第314行已判AGE-003为PARTIAL / EXTEND：旧资料级public/private和披露开关可复用，缺五类逐字段控制。这是源覆盖判断，不是当前003 live完成状态。

AGE001 [metadata证据](../testing/evidence/agent-profile-foundation-2026-10-02/README.md)与AGE002 [内容分离证据](../testing/evidence/agent-private-profile-2026-10-02/README.md)均已独立本地验收；后者仅九类本人人工Private字段、self GET/PUT/CAS/clear，不提供逐字段共享或认知读取许可。

## 2. 实施前代码、权限事实与真实缺口（历史基线）

| 实际路径 | 开始本项时语义 | AGE003裁决边界 / 缺口 |
| --- | --- | --- |
| `apps/api/internal/identity/identity.go:28`、`postgres/identity.go:42` | 普通Profile仅accountId/displayName/bio/visibility；资料级public、自读或具体有效profile_view grant，active target和双向Block优先 | REUSE现身份及ACL；不是逐字段五类audience，不向旧响应自动joinPrivate |
| `postgres/profile_edit.go:9` | 自读/编辑普通资料及资料级public/private；隐藏时撤旧公开Intent，审计原动作 | 兼容原普通编辑；新field配置不应暗改旧资料级visibility或源内容 |
| `agentprofile/private.go:31`、054、`postgres/agent_private_profile.go:24,71`、`httpapi/agent_private_profile.go:24` | 九字段直接输入、Person-only独立表；本人当前session/精确active Personal Agent；唯一metadata版本CAS + 替换/清空 | 可复用内容和严格本人编辑边界；开始本项时没有audience模型/表/共享projection。Private读取不能因新枚举直接升级为Runtime读取 |
| `migrations/034_person_ties.sql:31`、`postgres/connections.go:341,370` | 持久Tie必须匹配双方active Person及accepted friend request；状态active/removed，删除后非active；旧conversation不回填好友 | CONNECTIONS必须当前真实好友Tie，不接受聊天、单向follow、pending request或模型推断亲密代替；还需读时核request/pair、双方active及Block |
| `migrations/030_community_social_memberships.sql`、`postgres/community_social.go:33,82,192,235` | Community无账号/Agent；角色owner/admin/member，成员active/pending/invited/left/rejected；Community active/archived及published/visibility独立 | COMMUNITY需要owner显式选择具体Community及当前双方active成员。浏览、pending、invite或Organization成员不授权；Community管理角色不继承个人Private |
| `migrations/037_social_intent_audiences.sql:87` | Intent的COMMUNITY已有具体target FK，读时双方active成员及Community active | 可复用明确target和实时成员检查思路，不能复用Intent本体/许可来授Profile读取；本项仍须自己的字段目标与闭集策略 |
| `postgres/identity.go:102,163` | profile_view绑定owner/recipient/resource/read/purpose/期限，撤销revision+1 | grant仍只授权原普通Profile，不绕过Private/AGENT_ONLY或改变新field配置；Memory/模型/跨Agent同意各自独立 |
| `postgres/blocks.go:30,97` | Block事务内移除好友Tie、结束pending请求、撤双方recipient grants；Unblock只删除Block，不复活旧Tie/grant | 各audience读时Block优先，不缓存一次好友/成员结果；Unblock不能凭旧快照恢复新field读取 |
| `postgres/social_context.go:52`、`postgres/relationship_context.go:46` | 原共同信息/关系工具各自明确且默认关闭授权，最少事实或公开供给；不读九Private字段 | 048/051/052和AGA同意不能成为field共享/Memory或model出口授权，不改既有工具读取路径 |
| `agentcognitive`、`agentruntime/context_access.go` | 认知合法请求仍Unavailable；CONNECTION/CLOSE等范围需资源授权，未定义Close默认拒绝 | AGENT_ONLY仅表示字段受众目标；本轮若没有独立purpose/consent/current source resolver则认知port仍Unavailable；不按名称放开provider或A2A |

开始本项时的非测试引用搜索只发现九Private字段的self HTTP/store消费；没有五类field visibility表或其他Profile field projection。原已有Profile grant、关系和成员规则可作为权威输入来源，尚不是完成的AGE003接口。

## 3. 实施前根任务最小计划（历史；实际结果见§5）

根任务设计来源：`D:\Project\birdtie\work\v5-age003-design.md`。该文件是本项实施计划，不是已完成代码或第二份canonical；实现稳定后仅在唯一Profile Foundation增量收敛。计划只扩展共享Profile领域的字段受众配置与权威读取，沿用原稳定Account/Agent/metadata，不另造Profile、关系、Community或模型权限体系。

| 根设计范围 | 当前已明确的目标，未称实现完成 |
| --- | --- |
| 11真实字段 | ordinary displayName / bio + AGE002九个Private人工字段；不配置系统accountId/visibility或metadata，不造avatar/current_city等源示例 |
| 默认及overlay | 独立policy不复制内容；displayName/bio无policy默认PUBLIC但保留原资源ACL，Private九字段默认PRIVATE；未知拒绝 |
| COMMUNITY | owner明确1–8个真实UUID目标；当前active/published/未到期的Community和owner active member；viewer也须同一目标active member。hidden Community允许其真实授权成员范围，不能按发现可见性猜成员资格 |
| CONNECTIONS | 精确active Tie + matching accepted friend request + 双方active Person + 无Block；pending、聊天、follow和Close推断不授字段 |
| 本人配置gateway | GET/PUT `/v1/me/agent-profile-visibility`，完整11规则替换，唯一metadata版本CAS+1；明确默认清空，无owner/Agent/query/workspace/confirmed权威字段；055增量绑定原Person metadata，有数据down拒绝 |
| 人类字段projection | GET `/v1/accounts/{accountID}/agent-profile-fields`；匿名零digest可进入公开判定，提供失效token不能降级匿名；当前field ACL与原普通UserProfile粗ACL相交。只accountId/fields，无可见非空字段NotFound，不返回rules/metadata/Community IDs或隐藏字段名 |
| 原数据消费限制 | SQL helper作为附加字段限制覆盖旧普通Profile/name/bio读取和search匹配，保留各原资源ACL；被隐藏名使用通用标签，不以隐藏displayName回填，不依隐藏文本产生可推知搜索命中；未配置旧数据保留原行为 |
| 未开放能力 | AGENT_ONLY可由本人隐私管理检查，但不授Runtime/Memory/model/出口/A2A；不新增consumer UI或普通Profile示例字段 |

此表记录实施前裁决，不以计划表称运行成功；后来稳定wire/055实现见唯一Profile规范§11，运行结果见本记录§5。新projection的粗Profile ACL相交与既有chat/member等资源自己的ACL是两个范围，SQL helper不能替换原各资源权限。

1. 明确闭集字段与五类audience；未知字段/类型/拼写、额外权威属性、null/重复JSON/冲突版本拒绝。无配置与新字段默认不扩大读取；旧Private不得因迁移自动PUBLIC或自动选择Community。
2. 配置写入仍需本人当前有效Person会话及精确active Personal Agent。普通owner查看/管理与AGENT_ONLY的未来模型使用分开，用户应能检查和纠正自己的设置，不通过把字段隐藏于本人编辑接口来宣称认知隔离。
3. 每次投影只返回当前获准且有值的字段；不返回隐藏字段值、Private canary、内部授权证明或与目标无关的Community/成员/好友名单。非法或不可见资源用既有隐藏语义，不能暴露“字段不存在”和“你无权”的敏感差异。
4. CONNECTIONS读权来自当前精确pair的active好友Tie及其accepted friend request，而不是PUBLIC Profile、普通grant、互关、聊天室或一次活动。双向Block、停用、Tie撤销后重读立即阻止继续读取。
5. COMMUNITY只对owner明确target的同一Community，核其当前可用状态及owner/viewer双方active membership；没有target或资格不满足时拒绝。社区内容public、邀约/报名或Organization角色不授个人field读权。
6. PUBLIC配置除旧ordinary默认兼容外，必须由owner明确选择；新projection仍与ordinary Profile的public/self/具体有效profile_view粗ACL相交，并核当前账号/Block边界。普通grant单独不能读取非获准Private；PRIVATE只给本人管理；AGENT_ONLY不意味着分析、长期记忆、model egress或另一Agent共享获准。没有这些独立许可时保留认知端口Unavailable。
7. Private内容与field policy写复用同一native metadata版本，更新/清空原子CAS，不另造漂移的policy版本。该版本不覆盖ordinary UserProfile/Context；受众是持续设置，并非某一内容版本的外发批准。人类projection实时读普通源和ACL，未来cognitive source/具体批准另绑其真实当前updated_at/revision及独立许可；不能用metadata版本补造源版本。直接SQL约束、复合FK、target形状和有数据down保护也需实际验证。
8. 保留普通Profile四字段响应形状，原名称/bio获准值须受附加字段限制；保留九字段self内容边界和Context/Moment/Activity/Now、地图/Pin/键盘及中文直接路径，本项如无UI变更不虚构真机/消费者完成证据。

### 3.1 普通源内容与native版本的范围裁决

当前基线`postgres/profile_edit.go`修改displayName/bio/资料级visibility，只写`user_profiles.updated_at`，没有推进`agent_profiles.profile_version`；AGE002九Private字段替换/清空已推进该版本。根任务明确003不把普通编辑强改为新aggregate事务，以保留普通UserProfile真源：native metadata版本只负责Private + field policy写，field配置是持续受众选项，不是绑定具体内容的外发批准；每次projection实时读UserProfile/ACL并应用overlay。

未来cognitive source与具体批准必须同时绑定普通源真实当前updated_at/revision，不能补version=1或把本metadata称通用source revision；当前模型端口Unavailable，没有可凭旧批准执行的路径。此处是范围裁决，不记为003当前漏洞或待修阻碍；审计者未修改普通编辑实现。

## 4. 实施前验收要求

以下是003要求清单，实际运行结果另在§5。测试用自有随机fixtures，不能借全库LIMIT 1或修改共享seed作为权限证明。

| 维度 | 应有正例与拒绝 / 失效例 |
| --- | --- |
| 模型与wire | 五类枚举/闭集字段及合法target；未知/CLOSE/布尔替代、重复/null/客户端actor/Agent/成员/consent权威注入拒绝；本人工具读取不等于model读取 |
| 默认 / 兼容 | 旧Private无配置保持不共享；新identity无受众回填；普通四字段/public-private/grant响应与旧ID/领域记录保留 |
| PUBLIC | 明确公开字段可获最少投影；无配置/非公开/账号或Agent停用/Block/隐藏源不泄露Private；公开其他字段不能带出PRIVATE或AGENT_ONLY |
| CONNECTIONS | accepted friend + active精确Tie可读所配字段；conversation、pending/declined、removed、错pair、inactive、Block及取消好友后无字段；普通Profile grant不绕过 |
| COMMUNITY | 明确同一Community且双方active可读对应字段；无target/错target、owner退出、viewer退出、pending/invited/rejected、归档及不满足根可用状态时拒绝；private/hidden Community成员与外人分别检验；组织管理员不继承 |
| PRIVATE / AGENT_ONLY | 本人配置/检查路径合法；另一Person/Org/Business或授权Profile grantee不读；AGENT_ONLY不开放Memory/Runtime/模型/出口/A2A |
| 当前身份 / 并发 | 无会话、错digest/workspace、撤销/到期、停用Account/Agent；CAS一个成功一个冲突，无部分写；等待资格或会话失效后无旧字段返回/写入 |
| schema / 回退 | fresh/seeded up、旧完整行与版本、每类audience/target/owner完整FK与严格SQL拒绝；带配置数据down拒绝，空down/reapply明确留证 |
| 泄露 / 恢复 | 九字段canary只有合法投影可见；拒绝响应/日志不含值、token或PG DETAIL；新连接重读与撤配置后不可从缓存复活；fixture精准清理 |
| 回归 | 全Go、vet/build及原身份/成员/发现/Intent/RSVP/Plans/独立许可测试；适用UI变化才新增Flutter/真机证据 |

## 5. 实际增量、独立结果及限定结论

### 5.1 基线缺口的实际收敛

实际 [字段模型](../../apps/api/internal/agentprofile/visibility.go)、[store](../../apps/api/internal/postgres/agent_profile_visibility.go)、[HTTP](../../apps/api/internal/httpapi/agent_profile_visibility.go) 与 [055](../../apps/api/migrations/055_agent_profile_field_visibility.sql) 已落地。唯一规范继续为 [Profile Foundation §11](../architecture/AGENT-PROFILE-FOUNDATION-V5.md)，不另建共享正文：11真实字段、五类audience、PERSON本人配置完整替换、native CAS、默认/明确清空、显式Community目标与强好友Tie、人类当前字段投影及原粗Profile ACL相交均有正负代码与运行证据。AGENT_ONLY保留本人隐私控制，不开放模型读取。

Public普通displayName/bio仍归UserProfile及真实updated_at，Private和policy写共用metadata版本；普通编辑不改native版本、不复制源。字段受众是持续设置，不是某版内容发送批准；未来认知SourceState、模型出口与具体批准仍需真正当前源版本和独立许可。003没有把普通源补成Version=1或把metadata叫通用source revision。

附加SQL helper实际接入旧12个消费文件：identity、agent_workspace、connections、friend_chat、follows、community_social、organization_memberships、activity_chat、community_chat、relationship_context、new_people、social_activity_publish。旧各资源ACL保留；隐藏名称/bio不从其他响应或搜索匹配回流，通用标签不含Private文本。EntityCard发送分别验证两位participant，但返回采用sender当前视图，读取再按实际reader实时解析；修复不能用另一participant标签形成响应泄露。旧路径5×19 audience场景及follow/撤Tie/成员退出/邀请/角色/删Agent均进入最终限定回归，不靠新endpoint独自通过宣称所有旧响应安全。

### 5.2 已独立核对的历史实际结果

文档审计者读取原始JSONL/result、迁移日志及第四轮完整合成快照差异，未重写运行证据。计数为主测试/子场景PASS事件，非功能数；package无测试文件与test skip分开记录。以下属于当时已覆盖的场景，不替代后续发现的并发撤权竞态验证。

| 检查 | 实际结果 |
| --- | --- |
| 字段 / 全Profile模型 | 312 / 436 PASS，0失败/测试跳过 |
| PostgreSQL初范围 / 最终含旧路径及源并发 | 72 / 185 PASS；对应四包1396 / 1509 PASS，0失败/测试跳过 |
| HTTP spy / 合计注册路由+真实库范围 | 89 spy；合计102（89 spy + 13集成）PASS，0失败/测试跳过 |
| fresh001–055、三seed的054→055及严格SQL | 每库62个严格拒绝和74个真实helper场景；全部public原完整行、Private内容和native版本保留，无policy自动回填 |
| 第五轮迁移中的默认完整Go | 2196 PASS、0失败/测试跳过、exit0；完整所有public源行比较通过 |
| 回退与恢复 | 非空055 down exit3且rules/Private/版本/原全部行不变；明确CAS清空后空down/reapply保留Private v2/native v4，两库已严格清理 |
| 多语句撤权修复后限定范围 | 43叶场景（48含父）PASS；focused233 / 四包1557 PASS、0失败/测试跳过，vet exit0；自有fresh55/三seed库已清理、前后统计一致 |
| Community成员列表独立focused | ListSocialMembers最终同SQL原资格，9叶+1父=10 PASS；其后混合scope遇自动label RED整体FAIL，不能把该整轮称PASS |
| 自动副本限定green5 | 510 PASS、0失败/测试跳过（派生212、独立源8、首次Private25、handle同值98、粗source ACL167）；store/vet0，库清理与fixture统计一致 |
| handle同值修复后字段限定回归 | visibility753 / 四包2077 PASS、0失败/测试跳过，含legacy112、竞态48、成员10；store/parallel/vet0，精确fixture统计一致且自有库已清理 |
| 自动派生标签 / 最终source三轮与vet/build | 003仍重开；Person自动label/search新case与全部稳定修复后的source回归待实际结果，未提前记PASS |

[证据入口与重开说明](../testing/evidence/agent-profile-visibility-2026-10-02/README.md)已经保留第五轮及初次三轮完整回归等历史source结果，待根任务追加确定性竞态及最终source复验。完整源码hash、复现命令、原始日志及历史manifest由该证据目录维护，本审计只引用，不创建或改写根证据。

### 5.3 失败记录与真实修复

- 第一轮DDL fixture在已通过fresh/seeded全行保护后，因局部community_id与列同名报歧义。只修本项fixture为owned_community_id，第二轮DDL全部通过；没有修改055来迁就测试。
- 第三/四轮完整Go各2196 PASS、exit0，**其后全行快照FAIL**，因此当时迁移整体验收未通过。第四轮保存before/after、实际PK、表checksum与changed列：只新增2个contexts节点，原行无删除/改列。
- 精确原因是旧 `httpapi/person_contexts_integration_test.go` 用固定Institution/Online SourceKey，cleanup只删除City节点，遗留API声明生成的其他节点。根任务改随机独占key、跟踪返回context ID、只删除本fixture实体并核无残留；没有豁免contexts、排序、时间或只比count。第五轮相同全部public全行比较、完整Go及保护down/空down/reapply全部通过。
- 第一轮和第3/4轮失败log/result完整保留，后续PASS不覆盖历史；独立Domain/HTTP最初编译或fixture修正的详情仍由根任务证据记录，不能记成产品DDL或live运行失败。
- 最终隐私复查另发现 `postgres/activity_chat.go: ActivityChatMessages`、`community_chat.go: CommunityChatMessages`、`relationship_context.go: OwnRelationshipContext`、`new_people.go: FindNewPeople` 的RepeatableRead多语句撤权窗口：preflight建立的快照持续到label SELECT，可能漏看二者之间真实提交的字段/资源撤权。003已重开，未推进AGE004；旧2196及初版manifest保留为历史，不能当该竞态通过证据。
- 4方法现已改ReadCommitted与最终投影同条SQL复核原ACL：chat房间/成员/RSVP/Block、关系本人consent/Agent/accepted Tie与shared activities、新朋友当前source及candidate约束。独占pgx QueryTracer在preflight后暂停、真实commit撤权再放行，无生产hook；43叶（48含父）及focused233/四包1557、vet0已独立核对 `work/v5-age003-visibility-race-round3-{store,parallel}.jsonl` / result，自有库已清理且统计一致，最初编译和RSVP fixture检查失败保留。线性化点为最终投影statement snapshot（STABLE helper）及原资源guard，不承诺SQL之后网络在飞的值可绝对撤回；这组限定PASS不代替自动label及最终source全部回归。
- 根任务另补 `community_social.go: ListSocialMembers` final payload当前lifecycle/active Person/member EXISTS，pending需当前owner/admin，保留preflight及原ACL；[成员列表竞态](../../apps/api/internal/postgres/agent_profile_visibility_members_revocation_integration_test.go) 9叶+1父focused PASS。其后四包混合scope包含HTTP agent当时新增的 `TestAgentProfileVisibilityActivityDerivedLabelIntegration` REDcase而整体FAIL，所有该轮fail均属该活动副本场景；focused成员结果不能伪称混合整轮通过，原artifact保留，最终label稳定后复验。
- 再次裁决纠正先前“Activity已发布label可独立豁免”的范围：官方PERSON `CreateSocialDraft`从UserProfile自动复制host_label/maintainer_label，Input无hostLabel、Organizer.Name未作为此独立输入、UI无额外名字批准；发布活动不是新增Profile公开授权。typed PERSON/createowner/host/服务器Birdtie来源可识别的这些自动副本，需在PublicActivity HostLabel/Organizer.Name/Source.Maintainer和Agent termsearch实时核字段audience。根任务正在补新case，无schema/旧行擦除；只有真正独立且明确来源的legacy/外部/Organization/Community/Business label保留原源/ACL，不能泛化豁免。旧2196不足以验证本新场景。
- 对该native自动derived source还须实时读普通Profile、将field helper与原source粗ACL（public/self/当前特定有效profile_view read grant）相交：默认字段PUBLIC不跨越资料级PRIVATE；grant失效/撤销/过期不授权，PRIVATE/AGENT_ONLY不能被普通grant越过，本人仍可检查。HTTP agent补真实正负case待稳定结果；该裁决不改变所有旧chat的原room/Tie/资源ACL，真正独立source也不套用无关Person资料限制。
- 再次只读检查发现拒绝displayName的分支仍能回退`identity.Actor.Handle`。现公开Profile四字段没有handle，仓库也没有独立publicHandle授权；合法旧行handle与私密名字同值会形成可复现绕行。裁决为名字字段或native源粗ACL拒绝时直接通用中文标签，允许时才保留既有name→handle→通用标签；不新增handle字段或许可，不将helper DENY视为另一个姓名来源可公开。人物搜索、首次Person自动copy及旧七个handle消费路径补同值canary；初次RED和限定green保留，最新源码完整回归仍待实际结果。
- handle最初RED记录40个失败事件；补同值canary与拒绝分支后，`work/v5-age003-visibility-handle-green1-{store,parallel}.jsonl` / result实际753/2077 PASS、零失败/测试跳过、vet0，含legacy112/竞态48/成员10且自有库清理与统计一致。另label green5的510只证明其限定投影范围；最终全package和全部原源完整行保护仍需新source实际结果，不能由四包或count一致替代。

### 5.4 未实现能力与发布结论

只有仓库和自有隔离合成库证据；本项没有新增Flutter/UI，未运行新受众页面、手机、地图/键盘、辅助技术或无提示消费者验收，不把原手机基线充作本项UI证据。中文为主和非AI直接路径保持。

逐字段可见性不等于分析同意、Memory、Runtime source/consent resolver、provider、发送消息、ASK_USER送达或A2A资格；认知ports仍Unavailable。真实IdP、CSSA授权组织/活动、HTTPS/正式地图、部署与日志、值守/提醒调度、真实A→H仍是外部发布门槛。**Closed Pilot Ready：NO；Consumer Beta Ready：NO。**

## AGE003 最终补验结果（2026-10-02）

此前确实取得的2196三轮和055历史结果、初次DONE后立即重开的记录保留；现在4个旧多语句最终撤权guard、成员列表当前资格、同值handle拒绝回退、官方Person活动自动姓名的源粗ACL相交，以及原生维护者自动复制标签均已修复并取得当前源码证据。

最新3轮默认完整Go与055迁移独立fullGo各2780 PASS、0失败/测试跳过；全量vet/build exit0，当前API ready200、5未开放推断/协调路由404。fresh/seeded两自有库各62严格SQL拒绝+74真实helper断言，所有public基表完整旧行/版本保留，非空down exit3原子保护，明确清空后empty down/reapply保留Private v2/native v4；自有数据库/进程已清理。归档182文件/38源码hash完整核对、没有bearer泄漏；后续源码增量另取证，不覆盖此时真实日志。

自动维护者专项实际Store16 PASS、四包vet exit0；Social/Submit Community、Place publish/link_existing真实入口及历史Activity source reader覆盖。旧复制由服务器exact audit/source链接识别，仿prefix但无/错audit、真正独立manual及Host/组织信息保持；不删除或回填旧源。四legacy竞态43叶/48含父、成员9叶+1父、Person自动活动510、同值handle753/四包2077的实际限定结果及首次RED保留，不用顺序测试代替并发撤权。

**独立未修复缺陷**：实际City Seed ReviewActivity新候选发布返回23514 activity_organizer_exactly_one。已增量登记BT-FIX-CITY-001；合法历史source reader fixture和完整Go不算此业务入口成功，不把审核员或提交者猜作主办方、不削弱主办方约束。[缺陷与恢复条件](CITY-SEED-ACTIVITY-PUBLISH-REGRESSION-2026-10-02.md)。

准确线性化点为最终payload SQL statement snapshot；不承诺之后网络中数据绝对撤回。11字段五受众、人类gateway及原源ACL与field policy相交是真实本地能力；无新Flutter设置页面、模型Profile读取、Memory/候选写、provider/视觉/A2A/自主动作或现实试点证据。AGENT_ONLY不授模型许可；普通源updated_at与native CAS仍分开。Closed Pilot / Consumer Beta均NO，原IdP/HTTPS、现实组织/活动授权、地图API、部署日志、值守、提醒运营与真实A→H门槛保留。[当前实测/历史与源码hash](../testing/evidence/agent-profile-visibility-2026-10-02/README.md)。
