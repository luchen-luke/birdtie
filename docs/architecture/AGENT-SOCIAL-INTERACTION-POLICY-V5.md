# Agent Social Interaction Policy — V5

2026-10-03。BT-V5-AGE-041（源AGE-041）唯一SocialInteractionPolicy foundation正文。来源为[AGE原文](../product/BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md) §15 AGE041及[137来源映射](../../automation/v5_requirement_mapping.json)。遵守[认知ADR](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md)、[Memory边界](AGENT-MEMORY-ARCHITECTURE.md)、[066服务端开关](AGENT-FEATURE-FLAGS-V5.md)及[持续UX规则](../ux/GLOBAL-UX-INTERACTION-CONTRACT.md)。本项只完成CODE_LOCAL领域模型基础，没有可用消费设置/互动流程。

## 1. 七类偏好及实现范围

独占`apps/api/internal/agentsocialpolicy`复用`actorref.ActorRef/PrincipalRef`、`agentcognitive.AgentReference`、`agentruntime.PersonalAgent`、`agentevent.SourceVersion/QueryVersion`及`agentfeature.Controller`。不修改旧事件注册、PG/schema、main/server、队列或共用报告。

| Category | 本项设置职责 | 权限限制 |
| --- | --- | --- |
| SAME_UNIVERSITY | 记录同大学情境的偏好 | 大学字符串、组织university类型、本人context声明不证明学籍；实际education分类resolver缺失 |
| SHARED_COMMUNITY | 记录共同社群情境的偏好 | 当前双方明确membership及独立用途/源授权仍须重核；不是Community Agent |
| SHARED_ACTIVITY | 记录共同活动情境的偏好 | 报名或共同活动不证明出席、关系强度或长期可读 |
| EXISTING_CONNECTION | 记录既有连接情境的偏好 | 当前accepted/active Tie与独立处理许可分别检查；好友不授权Private/模型 |
| UNKNOWN_PERSON | 记录尚无获准关系依据者的偏好 | 不能从相同名字、城市、大学文字或未知类别自动归关系 |
| BUSINESS | 存储未来Business情境的预留偏好 | Business Agent未激活；离线判断也保持Unavailable |
| ORGANIZATION | 记录组织情境的偏好 | Counterparty用组织account principal，不能用organization actor entity ID冒充；角色不授私人处理 |

偏好仅闭集`DISABLED`与`REVIEW_REQUIRED`。省略任何category默认DISABLED，完全空Rules即七类全部关闭；未知/重复/超过七类与任何ALLOW/REQUEST/SCREEN/BLOCK值拒绝。它是本地设置刹车，REVIEW_REQUIRED只是将来需本人审阅的偏好，不授予读取、发送、好友申请、接收/投递、候选或Agent协商资格。

源AGE042拥有message-request四路策略，043拥有推荐介绍；044拥有自治分级及执行限制。本项没有提前实现这三项或其他筛选/分组流程，没有第二份matching/ranking系统、inbox、聊天自动处理或模型。

## 2. 已有安全边界审计

真实审计见[计划](../../work/v5-age041/AUDIT-PLAN.md)，根代理确认本项限定foundation后实施。

- `postgres/blocks.go`的BlockAccount在原事务中移除active Tie、结束pending申请并revoke双向grant；真人入口继续查双向Block。Unblock不自动恢复关系、旧许可或旧策略快照。
- `postgres/connections.go`的friend申请与accepted/active Tie仍是人类联系领域；不能把它们作为Agent用途同意、Private读取或自动发消息授权。
- `postgres/new_people.go`的FindNewPeople是真实双方052 consent enabled、双方intent visibility、当前source/scope/期限与Block相交的明确声明匹配。Match不是教育/身份/友谊证明，不读取私密Profile或长期Memory，不复用052作本项purpose许可。
- `agentprofile/visibility.go`的11字段与PUBLIC/CONNECTIONS/COMMUNITY/PRIVATE/AGENT_ONLY只控制人类获准投影；原Profile粗ACL与field规则继续相交。PUBLIC、好友或共同社群不授模型/本项源处理同意，不恢复被拒名字或私密字段。
- `contextgraph/declaration.go`明确本人声明不证明institution enrollment、community membership或access。仓库尚无education authority resolver；本项无大学字符串匹配或学籍核验实现。

本项未改这些真人领域接口或DDL。对其现有检查的只读审计不是本项新增真实resolver/数据库验收。

## 3. 独立策略版本与主体

`NewStore`创建一个精确Person/PersonalAgent的进程内Policy，形状合法后revision=1。actor只能该Person；counterparty为另一typed Person、Organization或Business的account principal。组织actor/entity ID与组织account principal保持两命名空间：UUID形状不证明映射，实际Service缺当前resolver仍Unavailable。

`Snapshot`复制Rules数组；`Replace(expectedRevision,spec)`独立CAS精确+1，冲突/零/overflow/非法规则原子拒绝；`Revoke(expectedRevision)`撤销并+1。显式内部Replace可恢复设置，但不复活旧revision、不授当前source/用途许可。内部mutex保护并发CAS和snapshot；32同旧版本并发更新只有一个胜者。

policy revision不替代055 native profile_version、普通源updated_at/digest、feature revision、双方consent revision或执行批准。上述041进程内Store自身没有DB持久化或消费编辑入口；后续065已建立原生本人Policy Settings、HTTP及Flutter设置，不能再将仓库整体描述为“没有持久设置”。065的Social设置作为偏好刹车，不是本节独立用途同意，也不激活041 Service的机器处理许可。具体原生版本、Session、owner和重启验收以065当前契约及证据为准。

有效区间为`[ValidFrom, ExpiresAt)`；Request期限精确15分钟，RequestedAt不晚于真实now。零时刻、非法UTC规范化年份、逆向期限、过期/未来请求拒绝。实际Service内部取`time.Now()`，历史request时间不能延长授权。

## 4. 来源合同、独立用途和保守规则

`Request`只含typed Actor/Agent/counterparty、operation ID、七类Relation、metadata source引用和期限，purpose固定`SOCIAL_POLICY_PREFERENCE_EVALUATION`。没有大学字符串、名字、Profile字段、消息、兴趣分数、学历证明、身份或model override。

前四类的`SourceReference`为synthetic pair metadata合同，包含对应闭集kind、资源UUID、双方typed principal及`UPDATED_AT_DIGEST`。复用Event的真实native source fingerprint形状，不补造Revision=1或把policy revision当原源版本。EducationDeclarationPair仅表达合成声明源的shape，不是验证学籍的资源、表或resolver；shared participation也不证明到场。后三类不允许隐藏关系source。

`RequestDigest`绑定准确Actor/Agent、双方typed principal、purpose、operation、原source fingerprint和期限，对Relation顺序做稳定排序；它不是来源授权、一次批准或effect key。current source必须逐项完全相同，不接受删项、额外项或owner/peer/kind/id/digest变化。

`EvaluateOffline(policy,request,OfflineBoundary,now)`仅synthetic合同，Decision恒带`Mode=OFFLINE_CONTRACT`。先查结构/角色/时限与policy状态，再查boundary：ALLOWED、双方active、明确双向BlockClear、双方独立正consent revision与current revision一致、两侧purpose精确匹配、无revoke/source withdrawn、当前policy revision、请求digest和sources一致。任何未知/缺失/撤权/版本变化/跨主体拒绝；CheckedAt必须等于now且boundary未到期，旧snapshot不能作永久许可。

原human_friend/profile_view/NEW_PEOPLE_052等purpose即使有正版本也不能替换该独立用途；permission/source设置不是同意。请求中UNKNOWN_PERSON必须单独出现，Business/Organization必须与精确peer类型且单独出现；Person不能借business/organization分类绕行。

全部安全检查通过后，任一matched category的DISABLED压过REVIEW_REQUIRED，缺省同样DISABLED；只有全部命中类别都配置REVIEW_REQUIRED才返回需审阅偏好。Rule/Relation输入顺序不改变结果。Business即使在合成合同配置Review、active与ALLOWED，也仍Unavailable，不改变dormant运行边界。

## 5. 实际可调用Service保持Unavailable

`NewService(Store, agentfeature.Controller)`真实构造Go后端领域边界。`Decide(ctx,request)`先检查SocialPolicy和Enrichment父开关，读取当前policy、核结构/typed主体/真实时间/取消，返回前再核同Controller Ticket和current policy。关闭开关、期间换配置/撤权使请求失效；这是本进程核验，不是持久dispatch commit或批准消费。

**当前没有SocialInteractionPolicy独立用途、教育身份、当前双侧关系/source/consent resolver。** Service不接受OfflineBoundary、客户端Verified、fake resolver或大学字符串，没有生产入口调用EvaluateOffline；合法七类request和两开关ON仍返回`ErrUnavailable`与空Decision。现真实好友同意/匹配开关/人类Profile projection均不接成该resolver。开关只收紧能力，不补身份/角色、大学验证、私密字段访问或模型许可。

Policy、Specification、Request、OfflineBoundary禁止JSON编码/解码控制对象；synthetic正例不能被wire传入actual service。未实现UI/Settings/持久个性配置、042/043可用互动流程、044自治或任何真实效果；后续消费服务须从权威域重新解析当前主体/关系/source/field privacy/purpose并保留原动作确认与执行边界。

## 6. 真实限定检查与未测范围

复现见[verify.ps1](../../work/v5-age041/verify.ps1)，仅新pkg Go tests/vet/build及前后source SHA；无DB、DDL、源行或外部写。初轮与加入更精确purpose负例的第二轮原始日志均保留，不替换旧版本结果。

2026-10-03（UTC 2026-10-02T16:18:14Z）staging stable1真实：[机器结果](../../work/v5-age041/staging-stable1/result.json)、[tests](../../work/v5-age041/staging-stable1/tests.jsonl)、[vet](../../work/v5-age041/staging-stable1/vet.log)、[build](../../work/v5-age041/staging-stable1/build.log)。**121 PASS事件（含父/子），0fail、0skip；target vet/build exit0，5源码hash前后一致。** 七类default/review偏好、保守顺序、双向Block/两侧独立purpose和consent、source变化/撤回、精确TTL、wrong typed主体、大学文字/Revision=1拒绝、policy CAS/revoke、取消/并发kill及actual Unavailable均真实执行。生产copy及root全量回归另留最终证据，不把staging结果冒充最终源码验收。

UX-CHECK-06/08/10/11/16仅引用本项来源未知/撤权/typed隔离/不可用/无正文埋点合同证据，不宣称完整端到端通过。没有新界面，UX-CHECK-01–05/07/09/12–15的实际入口、确认、键盘/手机/读屏、恢复、截图和消费者观察均未运行。本项不以offline fixture证明真实Block/Tie/membership/大学核验、当前用途授权或投递。

Windows当前CGO_ENABLED=0，无gcc；race detector未运行，mutex并发检查不冒充race instrumentation。没有真实消息、推荐/筛选、大学身份核验、私密模型访问、Memory写、provider/视觉/A2A、自主动作或部署。**Closed Pilot Ready：NO；Consumer Beta Ready：NO。** 原IdP/HTTPS、真实组织/授权活动、生产地图API、日志/值守/提醒运营及真实A→H Gate继续有效。

### 6.1 最终正式源码验证

根代理完成056共享验证窗口后明确批准单次生产copy。本worker仅复制五个固定Go文件到`apps/api/internal/agentsocialpolicy`，2026-10-03（UTC 2026-10-02T16:28:14Z）正式target检查真实**121 PASS事件、0fail、0skip，Go vet/build exit0**：[机器结果](../../work/v5-age041/production-stable1/result.json)、[tests原始](../../work/v5-age041/production-stable1/tests.jsonl)、[vet](../../work/v5-age041/production-stable1/vet.log)、[build](../../work/v5-age041/production-stable1/build.log)。5Go前后SHA且与staging stable1相同，已冻结供root统一后续回归。

归档见[完整限定证据](../testing/evidence/agent-social-policy-2026-10-03/README.md)及[source manifest](../testing/evidence/agent-social-policy-2026-10-03/source-manifest.json)。root此前056三轮3812全Go是本项copy前真实结果，不作为包含本项的新全仓验证；本项最终仅新增pkg target，root另取合并当前源的共同回归证据。未修改live queue、共用报告、旧Event、PG/schema/main/server。


## 2026-10-06 AGE042 消息请求路由增量

AGE042 的 ALLOW / REQUEST / SCREEN / BLOCK 原生人类入口扩展见 [消息请求契约](AGENT-MESSAGE-REQUEST-POLICY-V5.md)。它不改变本文041七类 DISABLED / REVIEW_REQUIRED、不启用用途 resolver、模型、A2A 或自动发送。SCREEN 只在同一个原 pending Request 留待人工审阅，真正筛查 Unavailable/OFF；ALLOW 只来自原 current accepted Tie，旧专用已同意会话继续原 ACL。本人持久配置与当前 HTTP 接线已有源码及相关单元，098/native/重启/UI 本轮未运行，不以单元宣称产品完成。
