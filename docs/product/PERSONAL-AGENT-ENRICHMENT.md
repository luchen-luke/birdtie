# Personal Agent Enrichment

日期：2026-10-02。状态：**V5 Personal Agent 富集产品 canonical 目标**，对应 `BT-V5-INT-001` / 源 AGE-080。产品定位仍为 Agent-native local social network；本文件扩展 [V4 产品总纲](BIRDTIE-CANONICAL-PRODUCT-SPEC-V4.md)，不重新设计 Now、导航或组织产品。当前实现状态以 live 队列、代码与任务证据为准，目标流程不代表已经上线。

来源：[AGE 原文](BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md) 的 Profile/Memory/Context/Policy/Autonomy/UI 章节，以及 AIR 包 `work/v5-materials/BT-V5-AIR-DESIGN.md` §3、7–10。领域与权限裁决见 [认知 ADR](../decisions/ADR-AGENT-COGNITIVE-ARCHITECTURE.md) 和 [Memory Architecture](../architecture/AGENT-MEMORY-ARCHITECTURE.md)。源路径/哈希、137 项对账及消费验收见 [材料记录](../research/BIRDTIE-V5-MATERIAL-INTAKE-2026-10-02.md)。

## 1 用户结果

让用户在自己的控制下减少重复说明，找到可信且可行动的活动、地点和自愿联系机会；主动授权的长期信息能够检查、纠正、删除和撤回。用户无需学习 Agent、Evidence、revision 或模型术语，也不需要启用 AI 记忆才能使用普通活动与社交协调功能。

Personal Agent 始终代表当前真实 Person，跨城市仍为同一个 Agent。Organization 工作台属于另一主体，不继承个人私人资料；Business、Venue、Community 和 City 不冒充个人身份。模型更换不改变 Agent ID、用户关系或已保存资源。

## 2 当前能力与后续目标

| 能力 | 当前限定实现 | 未来增量，不当作完成 |
| --- | --- | --- |
| 本人资料与Context | 既有普通Profile编辑和本人私密Context声明，独立于下述Private输入；原源实时读取 | 消费富集设置及认知读取另项，不自动复制Context |
| Agent资料基础metadata | AGE001六字段版本metadata、绑定和内部store；无新消费UI | 本行仅metadata；不能据此宣称完整Private/认知服务或“Agent已了解你” |
| 本人Private资料直接编辑 | AGE002九类人工输入、本人self API；Public保持原四字段/ACL；本项隔离本地运行验收已通过，无新UI | 用户可见编辑页、Runtime读取及独立分析许可仍另项；不宣传“已学习画像” |
| 字段受众与人类资料读取 | AGE003十一真实字段/五类受众、本人CAS及原资料ACL相交的人类投影；legacy撤权/自动名字及维护者副本补验已通过，实际最终结果见Profile规范§11，无新UI | 中文消费设置页、认知source/consent resolver仍后续；发布活动不另授权Profile，AGENT_ONLY不表示“AI记忆已开启” |
| 找活动 / 地点 | 当前明确输入、授权检索、规则理由及稳定实体详情 | 受控 AIR 理解与最少 Context 装配，不替换原供给/ACL |
| 本人显式记忆管理 | AGE004真实持久人工CRUD/CAS、期限检查与删除tombstone；本地验收通过，无新UI | 关联Evidence、候选确认、认知读取和自动学习仍另项，不宣称已理解长期兴趣 |
| 关系信号 | 051 默认关闭本人授权，30 天事实元数据 | 不由计数推断亲密、人格、对方意愿或长期偏好 |
| 找新朋友 | 052 默认关闭的独立匹配授权、双方明确 PUBLIC Intent、本人请求及邀请前确认 | 不自动好友/聊天，不读取 Memory/私聊/位置推断匹配 |
| 私人 Moment | 文字草稿、明确来源实体链接、编辑/撤回 | 单独分析许可、模型出口、候选审阅与 Memory 写入；公开/媒体仍独立 |
| 社交推断 / AGA | SAF/AGA 限定纯策略及拒绝合同 | 没有 live learner/Memory/provider/协作送达/真实自主动作 |
| 报名、发布、邀请、聊天 | 人类在现有页面主动确认并经原域授权 | AIR 不以建议/导航/`confirmed:true` 获得替用户操作权限 |

代码定位：`identity.Profile`、`contextgraph.DeclarationStore`、`content.MomentStore`、`agentruntime/policy.go`、`social_inference.go`、`coordination.go`、`httpapi/agent_workspace.go`及对应V4 canonical。历史Phase0无持久AgentProfile，AGE001仅交付metadata，AGE002追加人工Private输入/self编辑，AGE003追加人类字段受众/投影，实际限定结果只见 [唯一Profile规范](../architecture/AGENT-PROFILE-FOUNDATION-V5.md) 独立证据。AGE004现有本人显式Memory持久API；没有新消费编辑UI或自动画像，认知ports仍Unavailable；不能因主动填写资料宣传“Agent已学会你”“AI记忆已开启”。

AGE003字段可见性属于持续受众设置，区别于让Agent发送某一版内容的具体批准。普通Profile仍使用其原真实源/updated_at，不复制或合并成metadata版本；native版本只控制Private与policy写，未来认知来源及批准另绑真实源版本和独立许可。字段共享与模型分析、Memory、模型出口及跨Agent发送分别控制，当前没有模型执行或可借旧批准执行的路径。好友需真实当前Tie、Community需owner明确具体目标和双方active成员；组织管理角色不继承个人字段，公开资料也不自动扩大原粗ACL。

名字不获准显示时只显示通用称呼，不能换成可能与私密名字相同的账号handle；仓库当前没有独立publicHandle授权。官方Person活动自动带出的发起人名字跟随当前资料粗ACL及字段设置，发布活动不等于额外公开个人资料。真正独立填写且来源明确的活动/组织信息仍按原供给边界处理，相关本地补验只以Profile规范§11为准。

## 3 入口与语言

交互继续遵守 [Global UX](../ux/GLOBAL-UX-INTERACTION-CONTRACT.md)、[Now Workspace](NOW-AGENT-MAP-WORKSPACE.md) 和 [Shell 基线](FRONTEND-IA-AND-SHELL-BASELINE.md)。Now 是主要意图入口，普通发现列表、实体详情、Plans、消息、组织工作台和设置保留直接路径；不另造一套提交或权限逻辑。

用户界面默认简体中文，英文为完整本地化后的次要支持。实体名称和用户原文保留，内部 ID、模型版本、policy code、未经校准分数不作为主要文案。已有有效且获准信息可预填，推断标“建议/待确认”，未知保留未知，默认只问 1–2 个必要缺项，不靠猜事实减少输入。

未来“Agent 资料”“我的记忆”“为什么推荐”“隐私与授权”仅在对应能力实际实现并验收后提供可用入口；不提前展示不能操作的开关、记忆中心或确认按钮。设置、返回、取消和普通非 AI 路径持续可达。

## 4 长期信息的目标消费流程

以下为未来需要实现的流程示例，当前文字 Moment 保存不触发它：

1. 用户选择本人文字 Moment 或主动说明偏好。解释本次分析用途、所选来源和可选模型目的地；不同许可分别请求，默认不公开。
2. AIR 只读取本次获准且仍有效的最少来源，产生非权威候选。用户看到简短建议、可检查来源及“待确认”，不是确定画像。
3. 用户可以修改、拒绝、返回或取消。最终确认明确“只供我使用”及当前具体内容；来源/主体/内容改变后重新审阅，不复用旧确认。
4. AGE 重新核验当前来源、权限和版本，并在受控写边界保存或拒绝。成功只在权威结果确认后显示；失败保留适当输入，结果未知先核实，不能自动重复写。
5. 用户能找到保存结果，查看获准来源、纠正或删除。编辑/撤回源、关闭分析或删除记忆后，后续检索和在途任务遵守新状态，不立即由旧候选复活。

首次长期候选都需用户审阅。源示例的 0.25/0.82 是示例未校准分数，不是概率、自动激活阈值或真实偏好证据。报名不是出席、收藏不是喜欢、历史 Moment/照片不是当前位置；推断不覆盖服务器身份、成员资格或经营权。

## 5 权限与用户控制

| 用户选择 | 应解释的用途与后果 | 不能继承的许可 |
| --- | --- | --- |
| 保存原内容 | 私人草稿或用户明确选择的原对象范围 | 不自动分析、模型出口或公开 |
| 分析这一来源 | 本次用途、最少来源、时限及可撤回边界 | 不自动存长期 Memory |
| 模型供应商处理 | 目的地、数据范围、保留限制和失败降级 | 不由上传/好友/组织成员许可代替 |
| 保存长期信息 | 当前具体候选、来源及仅本人使用范围 | 不自动公开或给另一 Agent |
| 公开一个资料字段 | 当下实际字段与受众、独立最终确认 | 私人 Memory/分析授权不等于公开 |
| 跨 Agent / 真正外部动作 | 明确主体、任务、接收方、对象、内容及具体影响 | 原 Profile、关系、新朋友开关或 autonomy level 不授予此许可 |

所有读写仍由服务器核验，客户端开关和模型文本不是授权事实。Personal/Organization/Business 资源隔离；组织管理员不能因为工作台权限读取成员私人偏好。CLOSE / close friend 缺真实关系与资源授权时默认拒绝，不从聊天或参与次数生成。

账号/组织变化、退出、撤权、来源/版本/受众变更、Block 与过期清除相关私人上下文和旧确认，迟到响应不能写回新身份。原地图实例、viewport、键盘稳定约束保持；真正公共实体选择只按当前可见性处理，不为保留选择泄露旧私人内容。

未来 Autonomous Level 仅调节准备/委派范围，不授予权限。LEVEL_2 可准备但仍须具体确认；LEVEL_3 默认关闭。Agent 导航不自动报名、发送、发布、接受邀请、预订或形成承诺。ASK_USER 不是兴趣事实、已询问/送达或承诺。

## 6 异常、撤回和普通路径

- 模型不可用、拒绝分析、预算不足或 provider 未获准时解释影响，保留原权限内普通发现、详情、报名/Plans、已有联系路径；在线意图不强制 City、GPS 或地图点。
- 没有供给、缺权限、网络错误、源已撤回、候选失效和结果未知分别表达，提供真实可用下一步，不用假活动或生成文本填满界面。
- 取消预览不提交；提交前撤权拒绝；已经提交但结果可能在飞时显示待核实，停止后续并对账，不能承诺“完全没发出”或自动再发。
- 删除/纠正结果需能检查；逻辑停止使用、派生对象失效与物理清理分开。外部供应商保留限制如实说明，未证明不能承诺即时全网删除。
- 组织临时获得的活动用途信息不永久进入组织/商家记忆，不把一次任务同意变成新目的许可。

## 7 分阶段交付与验收

Phase0交付认知分工、数据所有权、共同纯合同/窄适配和增量队列证据；AGE001后续只补metadata基础，不作为完整Profile/Memory/Evidence接口交付。其后先完成所需权威内容/授权接口，再接AIR有界候选、审阅/纠正/删除与受控运行。必要安全与幂等前置完成，活动只读适配复用原搜索；视觉、第二provider、真实写与A2A不阻断最小文字闭环，也不借该闭环自动启用。

每项按现 canonical 选择适用 UX-CHECK：01 页面任务、02 合法预填、04 同实体/同动作、05 当前主体/后果、06 来源/未知、07 真实状态、08 旧批准失效、09 防重复、10 身份/迟到隔离、11 非 AI/定位路径、12 恢复、13 组件、14 辅助技术、15 无提示完成、16 最少埋点。未运行或不适用说明范围，不复制新 UX 任务队列。

后续验收需覆盖同一候选正常审阅、拒绝不写入、改稿/换主体旧确认失效、来源删改/撤权并发、重复提交/超时/恢复不重复、缓存/旧 worker 防复活，以及当前中文真机截图和大字体/TalkBack/无提示用户观察。只证明代码合同或本地合成流程时明确限定；不能用工程测试表示消费者已理解并信任。

**Closed Pilot Ready：NO；Consumer Beta Ready：NO。** 原 IdP/HTTPS、真实组织和授权供给、生产地图/API、部署日志、调度告警、值守支持及真实 A→H 门禁保留。Memory、live provider 和 AIR 真写另有启用门槛，当前均不因本规范接入取得生产许可。

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
