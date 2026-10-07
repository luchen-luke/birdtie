> 最新CODE_LOCAL状态以末尾2026-10-04根最终验收及live队列为准，旧阶段边界保留。

# 原生通知路由与本人通知偏好

2026-10-03；BT-V5-AGE-038 / AGE-039 当前实现记录。复用 Inbox、活动提醒、联系申请、消息、组织成员和 Agent Task 原服务，以及 AGE037 五种 Route。只有根代理核证后的 live 队列决定任务状态；本文件不声明完整消费端、推送或发布验收完成。

## 用户结果与授权边界

用户可以控制自己收到的普通业务通知。当前本人 API 为 `GET/PUT /v1/me/notification-policy`，用实际会话、活跃 Person、精确 Personal Agent 与原生 metadata 校验；不得以组织/Business 工作台、客户端主体、模型文本或 `confirmed` 修改另一人的偏好。

这属于普通本人管理。它不授予分析消息正文、认知、Memory、模型出口、自动报名或 A2A 权限。AGE037 机器处理端口仍受原授权边界限制；没有把 OfflineBoundary 接成实际服务许可。全部新通知使用固定简体中文，不复制私密消息、姓名、Profile、活动标题或任务 query。

## 实际路径

原领域事务写入业务结果 → 当前原生来源及接收者校验 → 当前本人偏好 → 排序优先级 → 不可变决定账本 → 适用 Inbox。

061 新建 `native_notification_policies` 与 `native_notification_decisions`，Inbox 仅增加可空路由引用。既有 ID、五类展示 category 与 API 保留；八类语义 metadata 单独附加，不把机器枚举显示成主界面文案。

| Route | 优先级 | 当前行为 |
| --- | ---: | --- |
| IMMEDIATE | 100 | 创建 Inbox，按优先级排列；不是系统推送送达证明 |
| NORMAL | 50 | 创建普通 Inbox |
| DIGEST | 10 | 保存待汇总决定；目前没有汇总器、摘要或投递 |
| SILENT | 0 | 保存决定，不创建 Inbox |
| BLOCK | 0 | 保存决定，不创建 Inbox |

优先级是闭集排序值，不是可信概率。未配置返回 version 0、普通 NORMAL；没有为读取创建配置。关闭偏好分类器或配置期限结束后，新事件恢复普通 NORMAL；关闭不是静音，静音应使用 SILENT/BLOCK。有效配置独立 CAS，每次准确前进一个版本；最长绝对 720 小时，按数据库时钟校验。类别规则覆盖默认，BLOCK 优先于暂停，其余有效暂停转 SILENT。

## 来源登记与缺项

| 语义类 | 实际登记来源 |
| --- | --- |
| MESSAGE | 人际消息、活动会话消息 |
| ACTIVITY | 临近提醒、改期/变更、取消、活动线索审核、明确意图规则匹配 |
| COMMUNITY | 社区会话消息 |
| ORGANIZATION | 成员邀请、接受/角色更改/撤销 |
| BUSINESS | 081 原人类经营权 Claim 审核结果；Business Agent 与通用 business_update 仍 **Unavailable** |
| SYSTEM | 地点线索审核 |
| AGENT | 实际个人 Task 的 COMPLETED / FAILED |
| SOCIAL | 联系申请及处理结果 |

截至081共16种实际登记 kind：原061的14种、069明确意图规则匹配、081原人类经营权审核结果。Business 主办的活动仍是 ACTIVITY。任务完成通知只表示原 Task 状态；不是真实 AgentRun/RunStep 或已完成任何外部动作。BUSINESS具体来源及当前客户端边界见 [商家审核通知](BUSINESS-CLAIM-NOTIFICATION-V5.md)；下文旧日期的未实现项按其当时证据范围保留。

来源从实际原生行重新解析 actor、接收者、上下文、逻辑事件版本和当前授权 fingerprint。决定包含一个且仅一个 typed FK；不存在的来源、错误接收者、类型错配、当前不可见/过期/撤权/屏蔽来源不创建通知。FK 级联使来源删除同时删除决定和路由 Inbox，不把失效通知降成无权限检查的 legacy 行。

组织通知 actor 是组织原 account 的资源来源身份。操作人的实际身份保留在原 `admin_audit_events`，本增量未把通知 actor 字段改为实际 inviter，不能混称。社区通知额外检查维护者账号和双方屏蔽，这是新增通知边界，不声称原社区会话此前已有相同检查。

`xmin` 和 SHA256 只用于当前保留原行的相等核查，不是历史修订、分析授权或可信身份。原逻辑事件与来源授权 fingerprint 分开，更新偏好不制造新事件。Task 以真实终态作为事件版本：完全相同终态重试不改原行；终态附加内容修改会失效旧 fingerprint，但不会再次提醒。没有把同一 Task 反复改状态当新 Run。

## 并发、可见性与恢复

本人配置使用实际 owner 的事务锁与 CAS，原生通知 writer 显式 READ COMMITTED；路由拒绝其他隔离级别，避免调用方的默认快照冻结当前偏好。source、policy 与决定插入在原事务内检查，业务失败不会留下成功通知。唯一 `(recipient, kind, source, event_version)` 保证同一事件重试不重复创建决定和 Inbox。

List/Read 对新路由行重新检查当前原生 source 和偏好。当前实现保守要求 source fingerprint、偏好版本、route/reason 全部相等：偏好修改、暂停结束或期限变化可隐藏旧 Inbox。恢复 NORMAL 只影响以后真实事件；旧 DIGEST/SILENT/BLOCK 不自动释放，也不反复提醒。此语义不是永久通知历史或即时擦除已合法返回的客户端画面。

原 Inbox 行仍核当前原生来源；本人 HTTP 使用当前原生 Session，在序列化后再次检查会话，不宣称能够撤回已经合法返回的内容。Activity 消息保留目标活动，直接消息保留原目标会话。NOT001 已提供本人通知设置及 Settings/Inbox 入口；2026-10-04 增量补齐下述 Community/Task 的当前目标，成员邀请沿用既有管理入口。

### 当前 Community / Task 目标（2026-10-04）

共享 Inbox SQL 在原 source/policy 可见性条件下投影可空 `targetCommunityId` / `targetTaskId`，不增加持久字段、DDL 或另一个决定账本。社区消息从原 native message → conversation → Community 解析；`resourceId` 仍是原决定 ID，不冒充 Community 或 Task ID。原 legacy owner Community 使用当前 owner/lifecycle 条件，其原资源 ID 可作类型明确的目标；无 typed 目标时客户端不猜跳转。

客户端每次点击（包括已读项）首先读原 Inbox ID 的当前权威对象，再按当前 typed ID 进入原 Community 详情/会话或 Task GET。Community 会话独立复核成员，Task 返回严格匹配原 ID、本 Person、acting user。Task 页只读原查询、答案和当前可见结果，不创建空 query 的占位 Task、不运行模型或重发领域动作。

个人 Task 的响应在编码及商业信息读取之后，native `ValidateHumanAgentTask` 再核同一 Session、活跃 Personal Agent、当前 owner 和原任务版本，关系等待之后才读取。切账号或组织，包括切回原身份的 ABA，原 nested 页面及内部批准弹窗永久退休；共同会话的确认弹窗使用该 nested navigator，不留在上级根路由。无关闭业务 producer、DIGEST 汇总器、真实系统推送或运营验收仍分别记录缺口。

## 迁移与证据

061 不推断旧偏好或旧决定。含本人配置、决定、路由 Inbox 或新资源类型行时，down 在事务内原子拒绝；只在随机自有隔离库验证空表 down/reapply，生产不执行 down。

- `work/v5-age038/verify.ps1`：限定通知合同/HTTP/Store 检查，001–059+061；不等于全仓库回归。
- `work/v5-age038/http-delivery/`：原生业务 writer 十四 kind 正负检查；临近提醒使用自有活动的真实路由，不把它当部署 scheduler 证据。
- `work/v5-age038/verify-migration.ps1`：001–060 原完整 public 行、061 fresh/current up、两类非空 down 原子拒绝、自有 cascade、empty down/reapply。
- `work/v5-age038/verify-current.ps1`：根协调当前 schema 默认完整 Go 三轮、vet/build、全 public 与全部 API Go/SQL/mod hash；以实际结果记录为准。

真实 delivery 首轮 27 测试失败及一个包失败暴露 `digest(bytea,unknown)` 缺少扩展，是实现故障。已改用 PostgreSQL 原生 `sha256`，未在 fixture 安装扩展掩盖。迁移脚本初次 SQL 转义错误、下一轮 fixture 误用非旧 category 亦保留原失败日志，分别修正；不能归成生产代码都已通过。

原061阶段的UX-CHECK-05/06/07/08/09/10/11/12/16仅是后端范围；2026-10-04通知目标增量已包含Flutter Inbox/Task/嵌套详情与原NOT001设置复用。17个当前原生目标正负检查及冻结全Go9284、Flutter702通过；后续027时间戳修复的冻结Flutter705通过。精确源帧、先失败后修复和本地手机证据见 `docs/research/BIRDTIE-V5-AGE-038-RESUME-AUDIT.md`，旧全仓结果不覆盖正在新增的AIR011源。手机尚无可点击的Task/community通知，typed目标真机正例、真实辅助技术与推送回执NOT_RUN。完整 BUSINESS producer、DIGEST 汇总与真实运营仍缺，不能以 enum/API 或局部界面声明完整038完成。真实 IdP/HTTPS、授权试点资料、生产地图、部署日志与调度、值守及真实 A→H 未齐，Closed Pilot / Consumer Beta **NO**。

## 2026-10-04 原任务通知真机正例

更新此前“typed Task 真机正例未运行”的范围：在自有 phone DB/schema077、冻结 APK73（Flutter872帧）用 Now 普通查询 `badminton this weekend`，原生 POST200 创建本人 Task，并由原 native writer 路由唯一 `agent_task_completed` 决定与 Inbox。没有 SQL 手工插入通知，没有模型或新的 Task 占位请求。

实际点击“任务已完成”先重读原 Inbox，再 GET 同 Task；中文页显示原查询、查询完成、一个当前公开的本地测试活动，以及只读/不报名说明。刷新、系统返回原收件箱、再点已读项仍打开同一任务，原 Task 行 hash/xmin、count1 和唯一决定/Inbox 保留；原个人 Context、Community membership、PrivateProfile、SeedIntent、AgentProfile、Community审计六组账本不变。界面英文仅为用户主动输入的原 query，中文结果与控件不改为英语。

本地 Task `82b97680-5cac-4e5d-a338-a1ac11919183`；Inbox `5b7211f9-17e2-44c5-93c0-8a4b2cf0cced`；decision `e1abc391-02a6-4a71-b21e-d416d7713fbb`。请求 ID、计数/hash、实际 UI XML/截图见 `work/v5-age038-resume/phone-community-candidate-binding1/task-routing-actual-acceptance1.json` 与 `phone-current2/screens/joint-notification-*`、`joint-task-*`。手机 API 日志确实对应200，不拿截图替代请求/真源证据。

这只更新 Task 普通链路正例；Community typed 目标真机正例、真实用户可用性、辅助技术、BUSINESS producer、DIGEST 与部署推送继续未完成。当前仓库已开始新078/Settings/Place绑定改动，旧APK73/872不覆盖这些新源；fresh078全量首次9470PASS/5FAIL已保留，修旧TTL fixture后17定向PASS，全量重跑中。完整038及Closed Pilot/Consumer Beta仍未完成/NO。


## 2026-10-04 根最终验收：八类原生策略路由CODE_LOCAL完成

当前原MESSAGE/ACTIVITY/COMMUNITY/ORGANIZATION/BUSINESS/SYSTEM/AGENT/SOCIAL均有真实独立native producer；BUSINESS为原human经营权审核同Tx通知，typed target使用原Business ID，不是decision resource ID。whole0829718PASS与Flutter991功能/120loading/analyze-test-Debugbuild0/259stable、真机Inbox→当前私有Console/refresh合成拒绝v2已核证。默认OFF/current权限和原producer不变；DIGEST只durable pending，聚合仍AGE040，不称已推送送达/部署可靠性。详见BUSINESS-CLAIM-NOTIFICATION-V5和work/v5-age038-resume/three-task-acceptance32.json，Pilot/Beta NO。


## 2026-10-06 AIR019 本人定时汇总（相关单元阶段）

新增本人 GET/PUT `/v1/me/notification-schedule` 和独立 `cmd/notification-schedules`，复用原notification决定、当前来源/偏好和typed Inbox，不改原001–098、活动提醒main/CLI或默认模型关闭。用户明确IANA时区、当地时间、DST缺口SKIP/重复EARLIER_ONCE、静默窗口、类别和滚动24小时实际Inbox触达上限；无配置不建计划，off/会话失效不投递，跨版本/时区改动不清零receipt预算。槽只服务器元数据，不能作为UserQuery、推理或工具批准。

新099只有设置、日槽与交付关联；源当前检查、Session、版本/xmin、最后PG时钟及未知提交去重分别保留。CLI先独立调用原活动提醒，汇总依赖nil/typednil/失败不停止提醒；错误阶段数量UNKNOWN。正常/立即分支仍只有原061谓词，DIGEST只确有原生交付关联才可见。具体契约见 [本人通知计划](AGENT-NOTIFICATION-SCHEDULES-V5.md)。

本阶段只相关单元和静态SQL契约，不能替代原生事实。099迁移/真实PG锁与并发/持久化/重启/原生HTTP、客户端设置/可用性/真机、部署推送/真实IdP和试点均NOT_RUN，建议PARTIAL，Closed Pilot/Consumer Beta仍NO。适用UX-CHECK-05/06/07/08/09/10/11/12/16仅后端范围，原历史验收不覆盖新099。精确证据在 `docs/testing/evidence/notification-schedules-2026-10-06`，状态由根代理更新唯一live队列。

## 2026-10-06 原通知消费者发出前的身份边界修复（UNIT_ONLY）

原通知设置和定时计划控制器在状态通知后、发出HTTP前存在同步监听器换账号/令牌/组织的窗口；旧批准已通过检查却可能使用新身份请求。现在每次原GET/PUT及未知保存核实GET使用本次捕获的请求头，并在通知后核对原generation/request/本人边界，失效即终止发出。正常显示状态仍不阻止本人已批准操作。路由偏好也将保存后迟到401/403视为结果未知；恢复访问只读当前设置，当前值不冒充原保存回执。

新增单一相关单位文件，复用原wire夹具但不调用旧测试main，不重跑原整套。实际RED 29运行/9通过/20失败；修后29全部通过，exit0，约3秒。两个原消费者分别覆盖load/save/未知核实前owner/token/organization/logout切换和正常控制、路由迟到401/403与400控制。原数据库/迁移/当前真实HTTP并发与撤权/原生身份/手机/调度投递/重启/全量分析测试构建均NOT_RUN；整个AIR019保留PARTIAL，Closed Pilot和Beta NO。精确命令、目录、原始结果与源码冻结见docs/testing/evidence/notification-transport-boundary-2026-10-06/README.md。


### 2026-10-06 保存传输超时：原消费者定向单元补验

原 BT-V5-AIR-019 的设置和计划消费者现将 PUT HTTP408 归入结果未知，沿原 GET-only 核实路径；不会将超时当明确未写入，也不盲目重发旧批准。只改两原控制器各一个条件，既有400拒绝、409版本变化、401/403晚鉴权及同主体/source/generation/request发出前守卫保持。当前设置匹配只表示已核实当前内容，不表示拿到原操作提交或投递回执。

10直接单元覆盖两消费者的408后当前匹配/未变化/不可读/身份退休，以及400明确拒绝控制；RED10中8行为FAIL/2PASS、exit1，最小修复后10PASS、exit0，单次约3秒。复用原wire fixtures，不调用其main；旧29/64整体未重跑。精确命令 flutter test --no-pub --reporter json test/notification_save_timeout_test.dart，目录 D:/Project/birdtie/apps/client。

408是MockClient合成传输响应，当前原HTTP服务取消响应仍为503，未声称实际服务产生408或真实提交。原生097/099/PG/HTTP持久化/锁撤权/独立调度送达/手机/认证/全面检查与构建 NOT_RUN，原任务PARTIAL，ClosedPilot/ConsumerBeta NO。证据 docs/testing/evidence/notification-save-timeout-2026-10-06/root-delivery-freeze03.json。UX-CHECK-03/08/09/11 延续明确确认、未知恢复和身份边界，不增加新需求体系。


## 2026-10-07 AGE040：原收件箱消费已投递的定时汇总（UNIT_ONLY）

原099的 DIGEST 决定只有经过既有调度交付及当前 source/policy 检查才可进入原 Inbox。本片修正客户端仍仅接受 NORMAL/IMMEDIATE 的合同缺口：保留 InboxItem 的路由、语义类别和排序权重，接受带闭集类别与原 DIGEST 权重10的已投递条目；SILENT/BLOCK/未知路由、坏目标和不完整 DIGEST 元数据仍拒绝。权重不是概率、许可或交付回执；客户端解析不能将 pending 决定释放为通知。

现有右侧收件箱增加“定时汇总”分组，只整理本次原 GET 返回的条目。普通和立即通知仍在原类别；相同条目不重复分组、不自动标记已读、无每日槽计数或 AI 摘要。每次点击仍以原 Inbox ID 调用原 Read，得到当前权威 typed 目标后进入原域详情。社区、明确意图活动匹配与商家经营权审核各用原来源与原目标 ID；经营权审核不是通用公开商家动态。原身份/组织/source retirement、迟到响应及当前授权机制保持。

原测试以 DIGEST 为非法路由的一个夹具精确改为 SILENT，原目标/ID/读取时间断言保留。真实旧解析器 RED：合法 schema fixture 抛 Undelivered notification，exit1；修后新6行为＋原3解析控制共9PASS，exit0。直接 widget 覆盖混合分组不自动写、逐条当前读取后打开原活动、404拒绝、320宽大字号及48dp、同 source 替换/组织 ABA 后迟到 Read 不跳转。此处响应为原 Item DTO/099 合同的合成 schema fixture，不是抓取的原生 HTTP、PG 或生产通知。

本次不改调度器、SQL/DDL、producer、provider 或导航。无真实每日槽边界字段，不称一份完整每日汇总或全历史；原普通 business_update producer 仍 Unavailable。新版本 native GET/Read DIGEST→客户端链、调度实际送达/运营、真实身份/推送、真机/读屏/性能、全面 analyze/test/build 与发布 NOT_RUN，整个 AGE040 保留 PARTIAL，Closed Pilot/Consumer Beta NO。适用 UX-CHECK-01/04/05/06/07/08/09/10/11/13/14 的本片单元范围，实际可用性与设备范围不由单元代替。证据见 docs/testing/evidence/age040-digest-consumer-2026-10-07/MANIFEST.json，原日阶段历史按其原范围保留。
