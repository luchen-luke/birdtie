# 本人通知计划与原生定时汇总

2026-10-06，BT-V5-AIR-019。来源：`docs/product/BT-V5-AIR-REQUIREMENTS.md` 对应019和原队列；不是新的需求体系。当前只完成仓库实现及相关单元，099迁移、真实数据库、消费设置入口与部署验收仍未运行，建议 PARTIAL。Closed Pilot / Consumer Beta 仍 NO。

## 用户结果和权限

新增 `GET/PUT /v1/me/notification-schedule` 接到原实际 `postgres.Store`、当前本人 Person/Personal Agent/Session/metadata。无配置读取返回 UNCONFIGURED，额度0，无隐式每日计划。PUT 是完整 CAS 替换，需要用户明确时区、当地分钟、DST规则、静默窗口（明确null表示无）、类别、滚动24小时触达上限及绝对期限，最长720小时。中文错误不回传源正文、token或内部错误。

当前版本只是确定性 Inbox 汇总批次：到时把当前有效的原DIGEST决定接入原卡片/原typed目标，不生成模型摘要，不从正文推断重要程度，不发送外部推送、聊天、A2A或执行工具。输出的 ScheduledEvent 是服务器调度元数据，不能编码/解码成权限；没有伪装成原UserQuery或增补13闭集事件。062模型预算不充当通知预算；实际模型主动内容/041语义通知仍Unavailable/defaultOFF，不能由模型confirmed或此偏好开启。

## 时间、关停和额度

- 明确 IANA 时区或UTC，Go内置tzdata，不使用电脑Local。DST不存在的当地时刻当日跳过；重复时刻只取最早UTC（EARLIER_ONCE），半小时转换和跨日期缺口同规则。Go与PG都计算候选最小UTC，实际不一致会拒绝。
- 槽键为本人owner+当地日期，不含可编辑revision或zone。同日期改计划不产生第二槽；改时区跨当地日期仍不能重置滚动额度。
- 有效保存之后才可触发，不回补本次保存以前的时槽。原槽已消费后重新启用也不重放。同日错过执行器但仍在计划期限内，可在当天非静默时补执行；不跨日回补。
- 静默窗口半开区间，可跨午夜。槽本身处于静默时当日跳过；执行时临时处于静默不消费槽，退出静默后可按上述当日规则执行。
- 空槽消费一次且计0触达；额度满也消费该槽，剩余决定可等之后的槽。一个槽最多20条原Inbox，额度是“滚动24小时触达上限”，按实际成功插入Inbox的receipt时间计数；不是每个批次计1或可编辑的日历日重置。
- 关闭、再次开启、CAS修改、换时区、来源/Inbox删除均不清零预算。receipt无正文，源/Inbox删除SET NULL保留已发生的计数；整个账号删除遵原级联删除。不存在真实新Inbox的唯一键冲突保存非投递回执，不算触达。
- 计划绑定保存时实际Session ID；原会话撤销/过期/删除时为PAUSED_SESSION，不自动换用后续新会话。须当前本人重新保存。计划的有效期不延长会话期限；API不输出其Session ID/digest。
- 新schedule关停停止未来汇总，不修改061通知分类器。原分类器disabled/expired恢复新事件NORMAL的旧语义保留，旧DIGEST不自动释放。

## 真实调用与持久边界

独立 `cmd/notification-schedules` 一次运行：先调用原 `EnqueueStartsSoonReminders`，再单独限时执行 `ProcessNotificationSchedules`。缺失/typednil/失败的汇总端口不阻止原提醒；原 `main.go` 五分钟提醒循环和 `cmd/activity-reminders` 保持独立且原字节未改。重要确定性提醒仍按原来源和用户通知偏好，不被新汇总额度挡住。

099只新增本人设置、日槽、交付关联三个表。原notification决定不可变，原Inbox ID、resourceType和目标复用；正常/立即分支只原visible谓词，不提前依赖099。新DIGEST投递要求当前原source/eventVersion/fingerprint、原分类器版本/route/reason、当前本人/Agent/metadata/会话、计划CAS+xmin和最后数据库时钟。调度在原owner事务锁串行，source读取沿用原函数，审计之后最终复核后才收口；新receipt与Inbox原事务一并提交，未知提交结果用同槽/同决定回读去重，不能称失败或自动重发模型。

调度worker先owner Account NO KEY UPDATE，再source关系锁、计划及Session/metadata；修复与原042账户先行writer的静态反序风险。源关系锁采用保守SHARE；实际PG锁并发/吞吐/超时没有实测，不声称不存在死锁或满足运营可靠性。CLI失败阶段数量记录UNKNOWN，不能把可能已提交的上一owner批次报成无副作用。099已使用的down拒绝删除偏好/预算/审计；历史001–098不改。

新DIGEST列表/回读保守要求原来源/分类器与同当前scheduleVersion/xmin、本人会话仍有效；关闭或编辑可能隐藏旧已投递卡片，但不删除receipt、恢复额度或撤回已经合法返回的画面。原NORMAL/IMMEDIATE完整保留。

## 验证范围

相关单元包括实际注册HTTP、字段/身份/迟到响应spy、原typed Inbox映射、生产调用的事务核心SQL-return spy、缺口/重复时刻/半小时转换、跨午夜静默、关停、跨±时区当地日期变化、CAS重启预算不清零、空槽/额度满、提醒故障隔离和CLI固定错误。SQL文本/schema静态检查不等于执行迁移；store spy输入不等于原生事实。

099 fresh/current-data/up/down/reapply/使用后down、真实原生HTTP、持久化/重启/并发/撤权/ABA/数据库时钟、部署执行器/真实通知送达、客户端设置/汇总可用性/辅助技术/真机、生产IdP/授权活动/HTTPS/map等均 NOT_RUN。适用UX-CHECK-05/06/07/08/09/10/11/12/16目前只有后端/单元范围，不能冒充消费界面或发布通过。后续验证按用户“相关单元优先”规则单独安排。

精确命令/目录/退出码、原404 RED与最终受影响文件SHA见 `docs/testing/evidence/notification-schedules-2026-10-06`；最终状态只由根代理核证的live队列决定。


## 2026-10-06 消费设置接线与共享保存时钟补充

本节增量覆盖上述“消费设置入口仍未运行”的仓库界面部分，保留原后端证据和未运行范围。BT-V5-AIR-019 仍须由根代理核证状态；099原生数据库与部署未验收，当前建议 PARTIAL，Closed Pilot / Consumer Beta 仍 NO。

本人 Settings 的“定时汇总计划”入口复用现有人类路由边界，读取原 GET/PUT `/v1/me/notification-schedule`，与061分类器分开。未配置状态不创建每日默认值；用户明确开始草稿，再选择 IANA/UTC 时区、当地分钟、类别、滚动24小时额度、可选半开静默窗口及绝对UTC期限。时区仅做客户端格式检查，真实区域有效性、DST和服务时钟仍由原Go/数据库校验；预设只有明确点击才采用，不推断位置。时间选择器初始位置不算已选择，用户确认才写入草稿；有效期限只在用户选择时生成，预览与提交不自动续期。

预览明确当前本人版本、启停后果、DST跳过/较早一次、跨午夜静默、逐条额度和期限。只有该具体草稿、版本和当前身份来源的人工确认才发送完整CAS PUT；取消和普通读取不写入、不触发调度、模型、推送或报名。新计划关闭不替换061关停→NORMAL的原语义，也不停止原独立活动提醒。旧通知页只更新确定性汇总的可用性说明，原061控制器和规则保持。

账号、会话token、组织身份、客户端/基址来源替换和同帧ABA退休旧草稿、预览及时间选择器；昵称变化不重读。晚GET/PUT成功或失败不污染当前身份；借用客户端不关闭。未配置API、坏协议与读取失败明确提示，不造未配置或空成功。409只GET当前版本并重新检查；写入超时、网络异常、5xx或坏200按结果未知处理，只GET核实，不自动重复PUT。读回需同Agent、版本+1及全部确认字段一致，文案只称“当前计划一致，不代表已投递”；不一致时保留结果未知，明确采用读回版本后才能开始新的草稿批准。长表单的失败/成功反馈位于检查保存操作旁，不能只藏在已滚离的表头。

实际PUT SQL已把 `valid_from` 和 `updated_at` 改为同一 materialized clock CTE的时间戳，原CAS、会话、审计和最终期限检查保持。纯单元证明原校验器拒绝两个不同时间戳，静态检查共享SQL时钟；没有在真实PG中复现或执行该SQL，不称迁移、锁、会话或投递验收通过。

相关Flutter单位64行为/4加载事件通过：40控制器、19新页面、2设置入口（保留原1项）、3原061页面。Go相关4顶层/9子case通过（共13 PASS事件），包结果exit0。原Settings缺入口RED、两个已打开时间选择器ABA RED、缺API配置RED、页面反馈不可见失败和执行树/滚动夹具失败均保留。命令/CWD/raw/退出码/精确差异与保留证明位于 `docs/testing/evidence/notification-schedule-consumer-2026-10-06`。

UX-CHECK-05/06/07/08/09/10/12/13/14 只有相关单位范围证据（含320dp、字号2、真实滚动和48dp确认）；UX-CHECK-11 普通人工路径无需模型但未做真实端到端，UX-CHECK-16未新增埋点。没有执行全量/analyze/编译/真机/读屏/深色/用户可用性、099迁移/真实数据库/HTTP持久化/重启并发/调度部署/实际收件箱送达/生产IdP；这些保持 NOT_RUN，不把单位绿灯当运营或发布门槛。

### final06 产品文案与单位边界

上述64行为/4加载是final05完整相关单位结果。final06只收口新计划页、保存结果和原061页的3源用户文案，并更新新页面两处结果断言；只复验3个直接widget case，各1行为/1加载、exit0。其余单位未重跑，Go共享时钟输入未变。所有执行器/数据库/部署未验细节留在本规范与证据，不放入用户确认弹窗；设置保存不表示通知已投递。


## 2026-10-06 保存后鉴权拒绝的结果核查

原HTTP在Store.Put后复核当前Session，保存响应401/403不能证明未提交。本计划控制器现清除旧可编辑来源、保留结果未知和原待核查版本，只重读当前计划，不沿用旧批准重复PUT。读回匹配仅说明当前配置一致，不宣称原操作回执或已送达。400确定无效及原409重审保持。

仅3个直接单元：首次1通过/2失败，修后3通过/0失败/跳过，加载1通过；两轮均--no-pub。未重复旧64行为单元，旧测试正文保持，原HTTP/native没有修改。精确命令、目录、退出码和raw见docs/testing/evidence/notification-schedule-late-auth-2026-10-06/README.md。099实际数据库/鉴权并发/重启/调度与手机仍NOT_RUN；任务PARTIAL，Closed Pilot/Beta NO。

## 2026-10-06 原通知消费者发出前的身份边界修复（UNIT_ONLY）

原通知设置和定时计划控制器在状态通知后、发出HTTP前存在同步监听器换账号/令牌/组织的窗口；旧批准已通过检查却可能使用新身份请求。现在每次原GET/PUT及未知保存核实GET使用本次捕获的请求头，并在通知后核对原generation/request/本人边界，失效即终止发出。正常显示状态仍不阻止本人已批准操作。路由偏好也将保存后迟到401/403视为结果未知；恢复访问只读当前设置，当前值不冒充原保存回执。

新增单一相关单位文件，复用原wire夹具但不调用旧测试main，不重跑原整套。实际RED 29运行/9通过/20失败；修后29全部通过，exit0，约3秒。两个原消费者分别覆盖load/save/未知核实前owner/token/organization/logout切换和正常控制、路由迟到401/403与400控制。原数据库/迁移/当前真实HTTP并发与撤权/原生身份/手机/调度投递/重启/全量分析测试构建均NOT_RUN；整个AIR019保留PARTIAL，Closed Pilot和Beta NO。精确命令、目录、原始结果与源码冻结见docs/testing/evidence/notification-transport-boundary-2026-10-06/README.md。


### 2026-10-06 保存传输超时：原消费者定向单元补验

原 BT-V5-AIR-019 的设置和计划消费者现将 PUT HTTP408 归入结果未知，沿原 GET-only 核实路径；不会将超时当明确未写入，也不盲目重发旧批准。只改两原控制器各一个条件，既有400拒绝、409版本变化、401/403晚鉴权及同主体/source/generation/request发出前守卫保持。当前设置匹配只表示已核实当前内容，不表示拿到原操作提交或投递回执。

10直接单元覆盖两消费者的408后当前匹配/未变化/不可读/身份退休，以及400明确拒绝控制；RED10中8行为FAIL/2PASS、exit1，最小修复后10PASS、exit0，单次约3秒。复用原wire fixtures，不调用其main；旧29/64整体未重跑。精确命令 flutter test --no-pub --reporter json test/notification_save_timeout_test.dart，目录 D:/Project/birdtie/apps/client。

408是MockClient合成传输响应，当前原HTTP服务取消响应仍为503，未声称实际服务产生408或真实提交。原生097/099/PG/HTTP持久化/锁撤权/独立调度送达/手机/认证/全面检查与构建 NOT_RUN，原任务PARTIAL，ClosedPilot/ConsumerBeta NO。证据 docs/testing/evidence/notification-save-timeout-2026-10-06/root-delivery-freeze03.json。UX-CHECK-03/08/09/11 延续明确确认、未知恢复和身份边界，不增加新需求体系。


## 2026-10-07 有限调度候选：当前身份与早期失权跳过（UNIT_ONLY）

原 AIR019 的 `ProcessNotificationSchedules` 已将当前 active Person、同主体 active Personal Agent、匹配的本人 metadata、同 owner 的 origin Session 放到真实候选 SQL 的 ORDER/LIMIT 前。先前只检查 origin Session 和时间计划，失效账号/Agent 或缺 metadata 的更早计划可能反复占候选名额，阻挡仍有效的后续计划。现复用原提名与 batch loop，仅提取一个实际 Store 调用的小 core；不是另一套调度器、授权或公平队列。

提名之后，只有每 owner 事务首条 Account 查询的精确 `pgx.ErrNoRows` 被视为本轮无副作用跳过：该分支尚未建立 slot、Inbox、审计或提交，原外层 Rollback 仍释放读取。扫描出不同 owner 仍拒绝；基础设施错误、坏扫描、后期来源变化和提交结果未知仍停止本轮，错误返回的零结构不是“确认零投递”。原 source 关系锁、同会话/CAS/xmin/最后数据库时钟、滚动触达额度、原 Inbox/receipt、关停和独立活动提醒均未修改。该筛选不是已证明的全局公平或 PG 并发保证。

两次首失败保留：原事务 core 的失权错误为 UNIT_SPY 行为 RED；实际提名 SQL 缺当前身份条件为 UNIT_STATIC RED。修后仅新文件 prefix 单元运行：5 顶层和 3 子项共 8 run/pass 事件（不是 8 个需求），exit0。query-aware synthetic 提名、pgx-return 事务与 Commit error spy不执行 SQL，不代表 PostgreSQL、真实未知提交或真实送达。具体命令/CWD/退出码及每阶段 10 个选中输入的 SHA 在 `docs/testing/evidence/notification-schedule-owner-selection-2026-10-07`，选中输入不是完整编译依赖图。

整个 AIR019 仍 PARTIAL；099 原生迁移、当前真实数据库/HTTP/会话/时钟/锁并发与重启、独立执行器和运营投递、手机/真实身份、整仓分析/测试/构建及生产均未由本切片运行。根代理另安排整合检查，不沿用旧结果；Closed Pilot / Consumer Beta = NO。


## 2026-10-07 Settings 定时计划父来源退役窗口（定向 widget 单元）

本次只补原 BT-V5-AIR-019 的消费者来源生命周期，不新建任务或审批体系。实际 Settings 路由进入定时汇总计划，编辑已有合法版本并打开具体版本确认；只更新父 Settings 的客户端或 API 基址一个渲染帧后，原确认仍能被标准点击命中。首败中两分支分别向旧地址同步发出一次 PUT；已经发出的保存收到迟到503时，还会对退役地址自动 GET。外层 NotificationDestinationBoundary 的后帧替换尚未发生，子页此前没有收到父入口 captured current，故外层显示保护不能代替发送前检查。

现复用 Settings 既有 epoch、原客户端、持有客户端、基址、本人身份与 entryChanges，将捕获的 current 和来源通知传至原计划页、控制器。控制器在原 generation/request 机制内同步且单向退休：清除旧草稿、批准和待核查来源；父来源回来也不能复活旧批准。确认和时间选择器 await 后复核来源，原 load/save/冲突读取与未知 GET-only 路径仍通过原发出前及返回后守卫。正常当前来源取消不写、明确当前版本仅一次 CAS 保存；已经发出的请求无法由退役撤销，迟到结果不成为新入口的成功，也不继续访问旧来源。借用客户端不关闭。旧确定拒绝、409版本重审、未知结果不盲重发与“设置保存不代表投递”的中文语义保留。

首轮4个新行为中3失败、1正常控制通过（exit1）；最小修后相同4行为全部通过（exit0），client/base两分支确认仍可命中但同步旧 PUT 均为0，已发晚503前后计划调用2→2。另只运行原 Settings 未配置入口1个控制（exit0），证明未配置不隐式写入或调度；未重跑旧264等整套矩阵。精确命令、目录、首败原始输出、before/after冻结源码与差异见 docs/testing/evidence/settings-notification-schedule-source-2026-10-07/README.md。

对应 UX-CHECK-05/07/08/09/10/12 的本单元切片，Synthetic BaseClient 只记录真实 Flutter 路径的发送，不是原生身份、HTTP提交或通知投递证明。本切片未运行 Go/099原生数据库、实际DST/独立调度及预算运营、全量分析/测试/构建、真机/读屏/性能、真实 IdP 或部署；其他检查点的历史证据保持各自源版本与范围。整个 AIR019 的其余原验收和原发布门槛保持未通过，不据此标整个任务DONE；Closed Pilot / Consumer Beta = NO。


## 2026-10-07 当前08全量检查与AIR019原生本地收口

用户已重新连手机并要求全面检查。本轮固定 current07/08 Flutter全量2840行为、93 automation退出0；Go完整命令12810父子PASS/4独立CLI程序前置缺失FAIL，exit1首败留档。同API源码编译程序后仅这4项在另一独立库补跑全部PASS，去重12814 Package/Test有PASS，不表述为一次全量命令退出0。Go vet/build/API及Flutter Debug APK退出0。Flutter analyze仍exit1：8 warning279 info、0 error；新版只删新测试引入的printlint，原诊断不屏蔽。fresh001–104/3开发seed是合成验证资料。精确命令、目录、退出码、输入帧在 [当前08全量证据](../../work/v5-age038-resume/full-verification-current07-2026-10-07/integration-checkpoint-current08/README.md)。该帧在后续AIR019 HTTP修复以前；不沿用到新后端全量。

AIR019原Settings父来源寿命已修，真正原生099补证通过：实际PG未来分钟投递/2pool幂等/关停静默/会话暂停/DST SQL/跨时区滚动receipt额度/旧提醒故障隔离；实际退休Agent响应200缺陷已修成403，已提交设置不能称撤销。21新native+19旧HTTP叶分阶段40unique/47父子PASS。099fresh/emptydown/reapply/current-data/down/up实测0，148原表数据/xmin/catalogexact，使用后down拒绝；专属库清理独立查不存在。原019以 CODE_AND_LOCAL_VERIFICATION 标DONE；认证通知Settings手机、AT/性能、部署/provider/推送/OS restart仍NOT_RUN，不能称运营可靠性。证据 [AIR019原生包](../testing/evidence/notification-schedule-native-2026-10-07/README.md)。原历史与初败全部保留。

AIR038新增原生注册恢复测试：7顶层22叶27父子PASS/exit0，实际成功丢回执/重复同效果/UNKNOWN与永久NO_EFFECT/原行锁取消及会话真实到期/编码后撤权有证。只新增test；消息与RSVP effect_key适配、消费者恢复入口及实际进程重启/真机仍缺，原038保持PARTIAL。证据 [AIR038原生包](../testing/evidence/air038-reconciliation-native-2026-10-07/DELIVERY.json)。

当前07 APK安装pull核SHA519d923f…；真实Mapbox、公开选城、中文周末羽毛球原API开发结果、活动轻卡与详情、键盘草稿/拖图卡/设置返回已检查并保存新截图/视频。侧栏匿名与公开范围真实，Settings说明登录服务暂不可用；OIDC configured:false，未假登录/发消息/创建公开活动。不是完整32QA、六个独立真人任务、可比Profile或生产A→H验收。

队列256：{"DONE": 180, "BLOCKED": 14, "TODO": 49, "PARTIAL": 13}；P0 {"DONE": 121, "BLOCKED": 9, "PARTIAL": 7, "TODO": 13}。Closed Pilot Ready=NO，Consumer Beta=NO：真实生产IdP/HTTPS部署/有效生产地图授权、合作方真实资料与活动授权、测试者/值守/备用渠道/日志/部署调度及A→H仍未齐；Model egress/真实Agent写/Vision/A2A OFF。继续实际可执行原任务，不等定时唤醒、不加重复backlog。
