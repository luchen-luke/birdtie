# 私密社交偏好 Seed V5

2026-10-03。BT-V5-AGE-018，源 `BT-V5-AGE-AGENT-ENRICHMENT-EPIC.md:849`。本项是普通 Person 中文编辑入口，复用既有 PrivateProfile 九字段与原生接口；不是社交执行、学校验证或模型/Memory授权。

## 中文用户流程

设置 →「社交偏好」→ 一个可多选问题 → 检查选择 →「保存私密偏好」。七个选项为小群体、大型活动、一对一、同一大学、同一城市、共同兴趣、国际社群。默认只选实际GET已存值，未配置保持空；已有其它明确描述显示并保留，用户可以明确取消。每轮只问一个问题，可跳过、取消或返回。跳过不发PUT，也不清除已有偏好。

最终检查说明仅私密保存、保留其它资料、不公开、不自动发请求；同一大学不核验教育身份，同一城市不证明居住，国际社群不构成成员关系或好友许可。选择交流偏好不修改社交Policy或真实执行开关。

选择空列表并明确保存，表示本人清空SocialPreferences；与「跳过，保留已有偏好」区分。新列表最多20项，超过时显示纠正信息并阻止保存。因另一个窗口删除的旧任意描述保留在可见草稿中，但要求取消后重新检查，不偷偷复写该过期描述。

## 实际接口与数据

复用既有 registered `GET /v1/me/agent-private-profile` 与 `PUT /v1/me/agent-private-profile`，server/Store/schema均未变。原 `agentprofile.PrivateAccess` 必须由实际Actor+session digest建立；普通人类读取与字段版本从原metadata和`agent_private_profiles`取得。

客户端controller严格解析当前PERSON/owner、positive profileVersion、完整九字段与configured状态，冻结所有list/map。save只允许原明确描述或七项选择，提交 `{expectedVersion, fields}`，仅SocialPreferences修改；PersonalPreferences、Availability、PreferredActivityTypes、TravelPreferences、InteractionPreferences、PrivateCityHistory、LanguagePreferences、AgentNotes均保留当前版本内容，隐藏字段不在UI预览或日志中展开。

原PUT是完整替换，因此409重读后必须使用新的八字段与metadata version，重新检查本人社交草稿；不能把旧完整对象重新提交覆盖其它窗口的新笔记。服务器CAS与真实当前Person/Agent/metadata/session仍为权威，客户端JSON或shape没有授权效果。新代码没有第二个身份、许可账本、SocialPolicy存储或source resolver。

原PrivateProfile写会推进metadata version；旧绑定的用途授权、证据和Policy来源继续按原current版本检查，不保证旧批准有效。本项不写普通Profile、UserIntent、Memory或Policy真实行。

## 异步与当前主体

auth/token/account/organization变化通过同步监听清空privateRecord、draft、review、未知结果和request epoch。旧GET/PUT响应、ABA、dispose均不恢复旧主体数据。Org/Biz和组织workspace不进入个人编辑。401/403/404清除私密资料；没有实际能力返回原Unavailable。

保存前绑定当前不可变record；同一选择无变化不PUT。409要求新GET与新review。网络错误、5xx及异常200回执视为结果未知，禁止盲重发；GET九字段权威当前值一致时仅说明当前资料与提交内容一致，不伪称特定请求已提交。GET不一致继续冲突纠正。离开已经发出的明确保存请求不会承诺撤销服务器提交；重新进入必须读取真实当前值。

## 复用权限边界

原Store每次查session、Person account、exact active PersonalAgent、metadata和owner，ProfileVersion CAS；Session行锁与final数据库时钟保护当前读取/提交。已有会话锁意味着并发撤权可能排在已开始的事务之后线性化，不能把pending撤权冒称已生效；实际过期在等待metadata后必须拒绝。原生权限、等待后expiry、并发CAS、rollback和private projection隔离测试实际重跑。

PrivateProfile编辑是ordinary human用途。学校/城市/好友/组织角色、public Profile grant、Policy ON或这些偏好都不给Agent/模型读取或推断许可；独立实际用途resolver未实现时仍Unavailable。070持久SocialPolicy的DISABLED/REVIEW_REQUIRED是未来行为上限，与此文本偏好不同。Org/Biz激活、Closed Pilot/Consumer Beta与外部门槛未改变。

## UI与证据

沿用Material/既有forest theme，FilterChip呈现明确selected semantics、可滚动SafeArea及正文后果，中文操作和异常恢复。UX-CHECK01/02/03/05/06/07/08/09/10/11/12/13/14/16 有对应actual controller/widget/native证据；UX04同一Settings路径复用同一旧领域动作；UX15同源APK/真实设备由根协调独立核证，不能以MockClient替代。

新增两份Go集成测试实际调用原Store/registeredHTTP，原有Store与HTTP权限测试按实际子测试复验。无新DDL；owned随机库按当时冻结001–067，记录schema SHA、全部public完整行、所有internal source before/after、owned源码与DROP。offline widget/HTTP spy只证明合同。所有native身份/内容均LOCAL_SYNTHETIC_ONLY，不是生产或真实社群试点。
