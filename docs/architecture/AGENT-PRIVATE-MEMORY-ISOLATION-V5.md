# Agent 私人 Memory 隔离 V5

日期：2026-10-03。BT-V5-AGE-060（源AGE052/057/060/072）默认隔离规范。复用[认知ADR](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md)、[Memory权威领域](AGENT-MEMORY-ARCHITECTURE.md)、[Profile规范](AGENT-PROFILE-FOUNDATION-V5.md)、[066服务端开关](AGENT-FEATURE-FLAGS-V5.md)、[Attention基础](AGENT-ATTENTION-POLICY-V5.md)、[SocialPolicy基础](AGENT-SOCIAL-INTERACTION-POLICY-V5.md)及[持续UX合同](../ux/GLOBAL-UX-INTERACTION-CONTRACT.md)。

## 原标准与当前能力

原文要求组织不能读取成员private AgentProfile/Memory/social policy，Business不能读取个人private Memory或因用户到店获得PersonalAgentProfile，PersonalAgent A不能读取B的private Memory，并明确测试这些负例。052的“除非明确授权具体内容”限定读取的必要条件；本项没有新增具体内容授权签发或跨主体正向读取能力。

当前Person本人PrivateProfile、EXPLICIT Memory和人工MemoryEvidence已经是真实持久领域，有本人Store和HTTP路径。旧goal中的“没有Memory存储”是历史背景。人类本人管理不授机器处理许可；未来授权也不能从旧profile_view、公开资料、组织角色、好友/成员关系、到店声明或066 ON推导。

| 数据/入口 | 当前本人正例 | 另一人/组织/Business |
| --- | --- | --- |
| Native PrivateProfile | 实际会话解析本人活跃PersonalAgent、metadata及版本 | 严格Person本人绑定；跨owner/组织workspace/组织account/Business拒绝，空record |
| Native Memory | 原AGE004权威Store、owner+Agent约束及Memory独立CAS | 别人自身列表只含自己；声明别人owner、跨类型会话/工作台拒绝。全局地址不能绕过owner |
| Native Evidence/provenance | 当前本人Memory版本+真实源版本/ACL；固定中文人工来源说明，不复制源正文 | 别人具体Memory地址NotFound或无效身份Forbidden且空payload；旧来源引用不授认知用途 |
| 已注册HTTP | `/v1/me/agent-private-profile`、`/v1/me/agent-memories`及原evidence/provenance | Authenticate后实际Person本人；组织workspace header与query owner选择器拒绝；no-store，错误脱敏 |
| 普通人类Profile投影 | 原UserProfile粗ACL与AGE003字段audience相交 | 普通授权公开字段可见，但不携带私人Memory/Evidence或私有Profile；不授模型许可 |
| 实际认知MemoryReader | 当前Unavailable，包括本人和合法066 ON | 固定空KnowledgeView/Unavailable，不从nativeStorage或caller source reference读取 |
| Attention/SocialPolicy | 原037/041进程内领域基础与独立版本 | 真实用途/当前来源resolver未实现，Service有效ON仍Unavailable；不是可用本人设置HTTP或成员policy reader |

权威身份复用Account、typed PrincipalRef/ActorRef及stable agents.id，不新增模型身份。组织entity ID与account principal ID保持独立；组织管理员作为Person可管理其**本人**私人内容，但切入组织workspace不继承该内容。BusinessAgent保持reserved suspended，HTTP fixture没有BusinessAgent；这项负例不声称现实商业Agent已激活。PlaceSaved是真实收藏记录，既不证明到店，也不授店铺访问Person私人内容。

## 实际守卫与时效

`postgres/agent_private_profile.go` 的原 `lockOwnAgentPrivateBinding`/metadata/session helper是PrivateProfile、Memory与Evidence共享的唯一本人权威绑定。实际session principal必须匹配Person，Account/PersonalAgent当前活跃，metadata必须存在；缺失不重建已删控制。原native最后session/时间复核保留。

`postgres/agent_memory_evidence.go`仍复用005来源解析与最终payload SQL当前版本/ACL，UTC本地事务和独立Memory绑定；源改删、撤回、无效版本/期限使旧引用停止有效，原provenance读取懒清，不将引用复制成权限或客观偏好。线性化点是原最终SQL statement snapshot；不承诺之后已传出的网络数据绝对即时撤回。

`agentcognitive.FeatureGatedDomains.ReadMemory`调用固定UnavailableCognitivePorts，当前域窄桥只接普通本人Profile/Context。Reference、请求/任务UUID形状、pure BoundaryFacts与开关均不证明实际认知用途许可；不会把Native本人管理路径接成机器读取器。当前具体内容Org/Biz认知授权桥仍缺。

SocialPolicy/Attention的内部Store Snapshot只适用于持有进程内对象的可信代码，不是HTTP身份读取器或持久私有设置。PrivateProfile.socialPreferences是本人描述字段，不能冒充041七类SocialInteractionPolicy。OfflineContract的正例不说明生产授权、通知或推荐成功。

## 实际验证与范围

新增测试只补明确矩阵，复用现有真实fixture、Store和已注册router：

- `postgres/agent_memory_isolation_integration_test.go`：真实Memory与Moment/SavedPlace Evidence，owner/peer/active Org/admin/workspace/entity-principal/旧grant/Block。
- `httpapi/agent_memory_isolation_integration_test.go`：真实PrivateProfile/Memory/Evidence内容canary，普通人类Profile正例不泄漏，registered routes四主体、会话、workspaces和客户端owner/confirmed/atShop拒绝。
- `agentcognitive/memory_isolation_integration_test.go`：真实native持久Memory正例；四类nativeAgent reference经实际FeatureGatedDomains OFF/ON均空Unavailable，传入适配对象的Memory/current-domain方法观察计数为零，原Memory不变。计数只观察该对象的调用，不是所有数据库访问的审计；实际固定Unavailable端口与CurrentDomainStore不含Memory方法是可检源码边界。纯Runtime探针另标OfflineContract；随机TaskID只是形状，不伪称真实认知任务批准。

同时选原native/HTTP/Runtime/认知/Attention/Social测试复验当前身份、撤权、源ACL/version、过期、晚锁等待、CAS、终态和默认不可用。可复现命令：`pwsh -NoProfile -File work/v5-age060/verify.ps1 -Round <新名称>`。runner只建owned随机本地库，001–064及三开发seed，无新DDL；比较完整public行、全部API internal Go源和三新测试SHA，目标test/vet/build，最终drop。原始结果、失败和冻结manifest见[专属证据](../testing/evidence/agent-memory-isolation-2026-10-03/README.md)。结果和完成判定以该归档当前轮为准，不用旧Memory或007计数替代。

**完成分类：CODE_AND_LOCAL_VERIFICATION的默认隔离条件。** 如将“明确授权后组织认知正向读取”纳入交付，该功能仍PARTIAL/Unavailable，须真实当前用途与具体内容授权适配、主体/Agent/source/member版本绑定、到期撤权及实际许可正负证据后再恢复，不能靠假confirmed完成。

无UI/Flutter/真机/读屏/截图、模型出网、费用、视觉/A2A、真实自动写或部署。适用UX-CHECK-06/08/10/11/16记录来源、撤权、主体隔离、Unavailable和脱敏边界；其他消费交互未测。本项不解除原IdP/HTTPS、现实组织活动授权、地图、日志值守、提醒运营及真实A→H门槛。**Closed Pilot Ready：NO；Consumer Beta Ready：NO。**

## 根最终独立复验（2026-10-03）

根复验453 PASS（54项新矩阵）/0FAIL/SKIP，test/vet/build0，379 internalGo和3 owned源稳定、全public完整原行一致、自有库DROP。worker1153+392历史归档全部逐SHA核验；早期并行源码变动导致的整体冻结FAIL保留，未改为PASS。当前504全API冻结帧默认全Go7063 PASS/0fail/testskip/fullvet-build0，覆盖060最终3源。见[根receipt](../testing/evidence/agent-memory-isolation-2026-10-03/root-independent-final.json)、392根文件manifest及共享044原日志/源帧。默认私有隔离达到CODE_LOCAL DONE；具体内容认知授权、原生Social配置读取、Business activation仍Unavailable，070接续本人设置API不自动授权组织机器读取。Closed Pilot/Consumer Beta NO。
