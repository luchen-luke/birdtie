# Personal Agent 渐进初始设置 V5

2026-10-03。AGE015 及原 AGE016/017 的代码本地实现；沿用 Birdtie 原身份、Agent、Profile 与 Context 真源。发布、生产身份及真实用户试点门槛保持原状态。

## 用户路径

普通 Person 完成登录或恢复有效登录后，MapWorkspace 实际读取 `GET /v1/me/agent-seed`。只在当前服务端 `needsPrompt` 为真时打开中文初始设置。失败不会阻断原 Now、地图或普通内容入口。组织身份不复用个人设置。设置 →「我的初始设置」保留直接编辑路径；服务端 DEFERRED/COMPLETED 进度在重启后保留。

每轮最多两项：昵称和本人当前城市、交流语言和基本意图；随后可跳过兴趣，最后检查全部选择并明确保存。自动入口跳过已完整的昵称/城市组。用户可返回修改、取消或「稍后再说」。城市未声明时保持未知，不从浏览城市、地图中心、搜索或首个目录项推断；语言是交流偏好，英语选项不代表已开放英文界面。

昵称沿用现有 Profile 可见范围及字段隐私；保存预览明确显示原范围。当前城市保存为原 `person_contexts` CITY/current 的 private 本人声明，不证明居住或到访。语言和兴趣保存于原 PrivateProfile 的 LanguagePreferences/PersonalPreferences；兴趣跳过保留已有内容。基本意图保存于独立私密 UserIntent，不混用公共 SocialIntent、搜索词、AgentNotes 或 Memory。

## 实际接口及真源

新增 `agentseed.HumanStore`，实际 `postgres.Store` 实现并在现有 server 的 catalog 窄能力断言注册：

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | `/v1/me/agent-seed` | 当前本人编辑读取，no-store |
| PUT | `/v1/me/agent-seed` | 绑定具体源快照的 SAVE 或 DEFER |

接口从现有可信 `s.actor` 获取 Person 和 session digest，不接 owner/Agent/confirmed/Verified 等客户端权限字段；没有实际能力返回 503，组织/Biz 或组织 workspace selector 拒绝。严格 JSON 类型、键名、重复键、未知键、Unicode、大小以及无查询参数合同均有负例。

SAVE 的输入为 expectedSnapshot、action、displayName、currentCityId、currentCitySnapshot、languagePreferences、basicIntent、interestChoice、interests。DEFER 只接受具体 expectedSnapshot 与 action，不捏造「先随便看看」或任何偏好。

基本意图闭集为 FIND_PEOPLE、FIND_ACTIVITIES、EXPLORE_CITY、SIMILAR_INTERESTS、JOIN_COMMUNITIES、DISCOVER_PLACES、JUST_EXPLORE。后者仅在用户确实选择时写入。

## 原生事务与版本

一个 ReadCommitted 事务内复用原 Profile 同事务动作（保留 bio、visibility、原审计语义）、原 person_contexts、原 PrivateProfile CAS helper 及新 UserIntent。仅抽取原 `replaceAgentPrivateInTx`，旧外部方法签名、绑定、metadata CAS、final session gate 保持。没有第二套身份或通用授权账本。

锁顺序为 Account SHARE → exact active PersonalAgent SHARE → 原 Agent Profile metadata → Profile/声明/选定城市源 → Session SHARE → 最终数据库时钟核查。Account 仅为权限源，不使用不必要的排他账户锁。实际 session revoke、absolute/idle expiry、Account/Agent 状态、metadata 缺失、取消均 fail closed。读取也在资源等待后重新锁 Session 和读取最终 payload；时间过期不能由初次认证替代。

所有源 JSON 序列化使用事务本地 UTC。opaque expectedSnapshot 包含当前 Profile、private fields、metadata、UserIntent、CITY/current 声明及当前城市原生行来源，并绑定实际 session digest；xmin 是不透明源代号，不是业务 revision 或权限。新 session 不继承旧审核。选定目录城市另有 currentCitySnapshot，当前 active/published/finite expiry 均由数据库检查；与审核无关的其他城市目录变更不作本人设置冲突。源删除重建即使版本相同也不复用旧快照。

UserIntent 有独立正整数 version、createdAt、updatedAt 及 DEFERRED/COMPLETED 进度。SAVE 与四类原生字段同事务提交；失败回滚 Profile、CITY、PrivateProfile、Intent、审计。相同当前快照与语义重放为 no-op；过期快照返回 409。真实两写竞争只成功一个。

## 066 迁移及回滚

`066_agent_seed_user_intents.sql` 仅新增 exact PERSON Agent Profile FK 绑定的私密表与版本触发器；无旧表 shape 改动。上下迁移 SHA 与 fresh 001–066、非空 down 拒绝保留真实行、本人 parent 级联清理、空 down/up reapply、全部 public 表完整行相等证据分别归档。

down 对任何非空 UserIntent 拒绝，不静默删用户选择。该 DDL proof 在专属 runner 中串行完成，不在普通 Go tests 中 drop/recreate 全包共享表；整体回归可并行测试不同随机身份。

## 异步、批准与 UX

客户端审核绑定不可变源记录，保存提交具体快照。账号、token 或组织切换清除草稿、旧批准、未知结果与 late response；ABA 通过监听身份和 request serial 失效。409 保留本人草稿但要求重新读取、检查再确认。网络错误和 5xx 均视为结果未知，不盲目重发；GET 当前值一致只报告「已核实当前设置与你提交一致」，不推断特定请求已提交。

中文 IME 组合态不自动推进。小屏、2 倍字体、键盘遮挡的滚动与语义标签有实际 widget 检查；真实截图/安装/设备重启由根代理在同源 APK 帧独立核验。相关 UX-CHECK-01/02/03/04/05/06/07/08/09/10/11/12/13/14/16 采用真实领域接口、版本检查与草稿恢复；UX-CHECK-15 设备证据独立记录，规则接入不等于发布验收。

## 权限与证据界限

这是普通本人编辑，不是模型输入、Agent 自动推断、Memory 提升或公开活动意图。现有模型/内容用途授权桥缺失时继续 Unavailable；ON flag、本人事实、City、意图和兴趣均不能自行授予认知用途。真正 Org/Biz Agent 未激活，不创建假继承。

offline Flutter/HTTP spy 只验证界面和 wire；实际 registered HTTP + postgres 隔离库证据另列。所有合成身份和城市明标 LOCAL_SYNTHETIC_ONLY，不是生产身份、真实学校/到访证明或现实用户体验样本。Closed Pilot/Consumer Beta 与外部服务门槛不改变。
