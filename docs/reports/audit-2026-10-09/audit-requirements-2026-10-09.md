# Birdtie 需求与实现核查（2026-10-09）

本报告只读核查正式仓库 `D:\Project\birdtie` 的需求、队列、源码入口和已保存验收记录；没有启动测试、构建、手机操作、供应商请求、数据库或部署，没有修改代码或原队列。报告本身是独立新文件。依据截至核查时的 `codex/birdtie-import` / `4637cfb6e7758fc4fa5dd548fad623a8484c4cfb` 加大量未提交变更；HEAD 不能代表当前全部实现。`c535`、`3e34` 两个工作树是旧基础版本，不作为当前产品状态。

## 可直接给用户的结论

1. 256 个执行任务中，180 DONE、14 PARTIAL、47 TODO、14 BLOCKED、1 IN_PROGRESS；仍有 76 个未完全完成。这个数量是工程工作包数量，不能据此称产品完成 70%。唯一 IN_PROGRESS 是 `BT-V5-AIR-009`。
2. 原 Functional MVP 和 V4 的数据、身份、组织、活动、报名、Plans、关系、聊天以及 Agent 安全基础已经有大量实际实现和历史本地验证。当前产品具备部分真实链路，不是只有空壳；但这些成果没有形成全部用户需求已满足、UI 已可验收、已上线的结论。
3. 10 月 8 日 Now30 + PG26 的 56 条来源要求整体都仍是 PARTIAL；UI 修复 42 条也未整包验收。统一方案 S1 公开动态→地点→作者→返回已有有界真实 API/数据库/真机验证，S2 部分关注及作者边界已有实现，S3 Journey/ThemeMap、S4 统一草稿、S5 完整材料/语音等未完成。
4. 已有真实模型调用和有来源地点回答，但通用即时联网、多领域中文多轮、稳定事实一致性仍未完成。API32 最近一次请求实际遇到供应商 HTTP402/业务403004；已有 Prego 模型回答“缺邮编”而事实已有邮编的矛盾尚未修复。
5. 页面无法按消费标准验收是一个真实未完成项。最新 API33/APK32 仍复现侧边栏冷启动首点无响应；整个原 Now 修复任务仍为 PARTIAL。单元测试通过没有证明整体页面排版、观感、全部键盘/面板/辅助技术状态已合格。
6. Closed Pilot、Consumer Beta 均不能放行。正式 IdP/HTTPS、获授权现实主办方与活动、生产部署、运营值守、真实用户全流程等仍缺证据。

## 需求来源与计数（不能相加）

| 来源/版本 | 原来源条目 | 进入唯一live队列的方式 | 本次核查边界 |
|---|---:|---|---|
| 产品V3.0、Now规范、身份归属规范 | 产品方向/领域合同，没有可直接相加的当前任务总数 | 经Functional MVP/V4/AGE/AIR逐步实施 | 宏观目标不能以旧shell或设计文档出现认定完成 |
| Functional MVP（2026-09-30） | 45执行任务 | 独立原任务45，39 DONE/3 TODO/3 BLOCKED | 原工程范围；正式身份/发布仍阻塞 |
| Community Social Layer | 12执行任务 | 12原任务，全部历史DONE | 原领域已有实现，不等于最新全用户体验 |
| V4（2026-10-01） | 87执行任务 | 保留原57并追加87 | 70 DONE/5 PARTIAL/7 TODO/5 BLOCKED |
| AGE原文（2026-10-01）、AIR（2026-10-02） | AGE81 + AIR56 = 137来源 | 128未覆盖来源合并108任务，6复用+3覆盖核验；最终队列AGE52、AIR本地50、LIVE5、INT1 | 原来源编号与任务编号不一一对应；当前AGE36 DONE/4 PARTIAL/11 TODO/1 BLOCKED；AIR19 DONE/4 PARTIAL/26 TODO/1 IP；LIVE5全BLOCKED |
| UI Repair（2026-10-07） | 42候选 | 映射原BT-FIX-NOW-UI-001和相关领域任务 | 整包PARTIAL；同构建设备证据只覆盖若干切片 |
| Discovery（2026-10-07） | 22来源项 | 原AGT/MAP/AIR等任务映射；未建立另一个live队列 | 补审已取得根原文§12全部22条验收/依赖，结合UI42和API27–34形成64项附录；没有重新运行整篇场景或将局部证据称整项DONE |
| Now（2026-10-08）/Place Growth（2026-10-08） | 30 + 26 = 56来源 | 统一包映射149已有任务，不追加56个新任务 | 本报告附56逐项当前状态；整体PARTIAL |
| Unified十屏/33场景（2026-10-08） | 10参考屏 + T01–T33场景 | S1–S9是既有需求的执行视图 | 包结构校验不是应用验收；后续S1/S2有界证据单独记录 |

队列当前仍共256项。P0共150：121 DONE、7 PARTIAL、12 TODO、9 BLOCKED、1 IN_PROGRESS，29个P0未完全完成。UI候选、症状、诊断、QA、来源要求、已有执行任务不能加成一个“剩余任务数”。若其他聊天出现AGE70/AIR67等数字，需先核对其版本与条目口径；本仓这份原文和唯一137映射支持的是81/56。

独立完整附录：`D:\BirdTie\audit-tasks-256-2026-10-09.md`（256原任务）、`D:\BirdTie\audit-v5-sources-137-2026-10-09.md`（137来源→当前任务）、`D:\BirdTie\audit-now-pg-56-2026-10-09.md`（56来源当前缺口）、`D:\BirdTie\audit-discovery-ui-2026-10-09.md`（DISC22与UI42共64来源的局部证据/当前缺口）。这些文件只含开发任务状态/标题/依赖/证据路径，不导出用户私密任务和日志。

## 状态和证据怎么读

本报告区分四层：设计已定义、源码已存在、有限流程已验证、正式部署/交付。队列 DONE 是该任务规定范围内的历史状态，尤其 `CODE_AND_LOCAL_VERIFICATION` 只说明代码和本地验证；不自动包含新需求、当前组合源码、真机视觉、生产运行。测试 mock、SQL spy、合成 UGC/活动、Debug APK 都不能单独证明现实供给或用户验收。

10 月 8 日统一包的 `VALIDATION-REPORT.md` 验证 56 个来源 ID、33 场景和十屏参考文件的完整性，不是功能测试。`ACCEPTANCE-SCENARIOS.md` 内的 NOT_RUN 是包制作当时状态，需要叠加后续增量证据；不能只看原文断言所有场景从未执行。

## 关键产品能力矩阵

| 用户需要的结果 | 实际已有内容与证据 | 尚缺/当前判断 | 层级 |
|---|---|---|---|
| Now 地图、卡片、详情显示同一结果 | `agent_result_projection.go`、`map_projections.go`、`map_workspace.dart`、`agent_entity_result_card.dart`；API27/APK20 原 Glenhouse 同实体，API29/APK22 十个真实 OSM 餐厅卡/点一致 | 当前有界路径通过；全实体/全范围/撤权/旧轮/失效/分页仍未整包验证；失败ACTIVE尾轮全城回退在API33才补上 | 代码+部分真实流程 |
| 清晰稳定的 Now 会话与返回 | 同父页面的稳定 key、消息/列表滚动、进入退出相机和选择恢复；API30/APK26 公共内容返回，API32/APK30 手动阅读返回有原视频 | 全键盘、多层面板、冷启动、性能、无障碍尚未完成；侧栏第一次点击无响应仍复现 | 代码+有限真机 |
| 多轮有来源回答 | API27真实模型3次；预约资料不足明确说明；本地“地址”0模型继续同Task；API29十餐厅成功 | 通用Compare/Refine、多实体、多领域、多种中文表达、普通账号即时库外补搜未完成；供应商余额/服务隔离阻断后续真实复验；事实矛盾保留 | 部分真实流程，整体PARTIAL |
| 地点采集与复用 | 原候选、独立审核、字段许可/TTL、原Place ID，真实Overpass15条已在隔离库经批准发布，重复采集复用15条、0新地点 | 普通Now自动库外采集/多人审核/跨源合并未完成；SearchPro保存许可PENDING仅限制该来源 | 隔离本地流程 |
| Agent属于谁、可代谁行动 | 019迁移唯一(personal/organization, principal)；Person/PersonalAgent、Organization/OrgAgent和成员角色分离；CityContext、Community没有独立Agent；服务器解析工作台和会话 | Business仅经审核的suspended元数据创建，不能调用runtime；完整A2A仍TODO；真实生产身份未验 | 真实代码/本地基础 |
| Agent能记住用户并在推荐使用 | Private Profile、Memory/Evidence、候选五态、更正删除、Context/Policy、我的智能体已实现；海鲜/辣显式偏好→确认→普通Now排序→纠正/删除有真实HTTP/PG证据 | 仅闭集词法偏好；通用行为学习、文字→候选、校准、完整模型私人Memory消费未完成；没有用户专属模型训练证据 | 代码+有界学习链 |
| Agent学习可信且可控制 | AGE007候选、本人明确版本确认，AGE011更正，AGE046“我的智能体”六组，隐私/政策接口已存在 | 候选概率0.25→0.82的合法校准与推断ACTIVE未实现；Memory Center/WhyThis仍TODO；AGE046六组真机NOT_RUN | 大量代码/单元，消费闭环不完整 |
| 活动→报名→Plans | 活动发布、权限、报名取消、容量/重复保护、Plans、通知原领域有历史隔离DB/真机闭环 | 同统一Now输入草稿中的创建/加入连续流程仍未完整验收；现实已核验活动/生产身份缺证据；报名不等于真实出席 | 历史本地流程完成，正式未完成 |
| 关注、好友、聊天、分享 | 连接申请/接受/移除/屏蔽，个人/活动/社群持久会话，实体分享、读状态，组织管理/社群成员已实现；API32/APK28关注/取消关注有中间构建实测 | 全发现/关注/好友/我的范围仍未整包；最终APK30关注路径NOT_RUN；@提及与发好友统一草稿、Now分享预览/链接撤销未接通 | 原领域有实现，统一体验PARTIAL |
| 公开动态→地点→作者→返回 | API30/APK26隔离合成UGC经真实发布API，B无需问Agent可看地点/Moment/公开作者；隐私/屏蔽/撤回服务端和导航重验有证据 | T03多动态分页、T04 A/B/C计数、全部缓存/媒体即时撤权未验；T07作者地图API34仅后端，客户端/手机NOT_RUN；合成UGC不是现实经历 | S1有限真机通过，S2部分 |
| 收藏整理、备注并在原地图复用 | 原Save持久化和Plans已有实现 | NOW018整理/筛选/notes/比较/上图/路线复用UNCONNECTED；库外pending reference和canonical解析未完成 | 基础有，扩展未接通 |
| 路线、Trips/Journey、ThemeMap、导入 | 目标合同和部分基础数据模型；同Now读取设计已明确 | 无ThemeMap/Journey完整领域/API/UI实现证据；同图真实route geometry、交通方式/站点编辑、逐日地图/外部计划导入均UNCONNECTED；AGE029/030和AIR021 TODO | 设计+基础占位 |
| 唯一输入、语音与材料 | 文字/短链接，human-only PNG/JPEG本机检查→确认保存到本人消息→读取/删除；server.go399–404为真实会话图片路由 | 助手明确不读取图片；中英STT、视频/文件、统一typed draft、图片理解、完整冷恢复/续传未完成 | 部分材料真实代码 |
| 大文件/视频上传 | 媒体协议与部署预案；现有human-only图片不能等同视频直传 | Birdtie视频multipart/对象存储/处理worker和英中网络实测未实现/未验证；Civu原上传历史不能算Birdtie交付 | 设计，视频未实现 |
| Civu账户/内容迁移和正式上线 | 保留应用身份、只读复用矩阵、cutover/路由快照/本地迁移演练设计 | 未执行生产用户/媒体/数据迁移、签名覆盖升级、线上切流/旧服务退役、生产备份恢复与部署 | 设计/本地，不是线上完成 |

主要证据：`docs/testing/evidence/now-place-growth-2026-10-09/{REPORT.md,increment-26/REPORT.md,increment-27/REPORT.md,increment-30/REPORT.md,increment-32/REPORT.md,increment-33/REPORT.md}`；原队列的逐项evidence/partial_reason；`docs/handoffs/birdtie-codex-unified-20261008/BirdTie-Codex-Unified-Development-Plan.md`。

## 为什么“已DONE”与用户感受差距很大

| 历史DONE任务 | 已完成的原范围 | 当前用户期待仍没做到的部分 |
|---|---|---|
| BT-AGT-003 Now输入连接查询 | 用户消息、回答顺序、同实体、单一错误、加号不固定羽毛球 | 语音/多材料/类型化草稿/业务操作统一入口；原验收甚至记录地图Pin当时403阻塞 |
| BT-AGT-004上下文持久化 | 窄二轮area/refine，稳定Task/筛选/恢复 | 任意中文多轮、多个实体、通用Compare/Refine、当前供应商失败后完整恢复 |
| BT-V4-NOW-004类型化卡片 | 七种类型稳定Ref、原ACL，代码/本地验收 | 当时证据明确“新native7未部署手机”；不能代表当前所有新素材/场景视觉和同图体验 |
| BT-V5-AGE-046我的智能体 | 原API读聚合、六组内容、相关/整体Flutter历史证据 | 六组页面真机实际操作NOT_RUN，未知手机/消费者效果不能叫已验收；还没有完整记忆中心 |
| BT-V5-AGE-008置信度模型 | 置信度结构与有限规则等工程能力 | 原AGE007概率校准数据、合法校准、推断晋级仍未实现，不能叫通用学习完成 |
| BT-SAV-001收藏 | 本人独立于报名的持久保存 | 整理/备注/城市集合/同Now上图/组合路线/ThemeMap |
| BT-V4-CHT-003实体分享 | 原会话中的实体消息与权限 | Now统一分享预览、snapshot/live链接、到期/撤销/原对象变化生命周期 |
| BT-V5-AIR-039检索适配器 | 确定性领域工具与许可边界 | 不代表真实provider实时补搜；AIR009仍进行中，独立LIVE门仍BLOCKED |

这些证据主要说明完成口径窄于产品交付口径，不能直接据此断言所有历史DONE是假完成。真正的遗漏是新需求已有映射却仍未接通、整包UI/UX未验收、原DONE没有独立更新为“当前产品可验收”的证据。不能让用户自己从几千字历史日志里找这些差别。

## 未完成：本地实现缺口与外部阻塞分开

本地可以继续补的明确缺口：NOW008–013统一typed draft/语音/完整附件/图片地点建议/@与send；NOW018–024收藏整理上图、权限分享链接、真实路线、ThemeMap/Journey/外部导入；S2全部关系范围/作品分页/TA地图客户端；AGE020–022自动Moment富集、AGE029/030 Life Map/Import、AGE047/048 Memory Center/WhyThis、AIR043–050文字到候选与模型记忆消费、AGE040商家摘要客户端。原任务仍TODO/PARTIAL，不能归因于SearchPro或生产身份；其本地设计和实现可以独立推进。

外部事实/授权/环境缺口：生产IdP和正式HTTPS/地图发布配置、现实已核验组织和活动、真实共同出席来源/纠错合同、运营值守/备用支持/生产提醒告警、正式迁移/部署；SearchPro持久化许可PENDING；腾讯最新模型请求HTTP402/403004账号服务隔离。它们不都要求扩大修改，也不应阻塞已有站内合法内容和不依赖provider的界面修复。

两个混合例子需特别说明：AGE007的人类候选、确认、五态和消费UI已经有本地实现，剩余校准和INFERRED ACTIVE并非只缺一个按钮；AGE027五信号中attended activity at只有枚举，无合法出席来源/producer，不能把报名/Plans/GPS当真实出席补齐。Business Agent可创建suspended身份不等于可调用经营知识pack/runtime。

10月8日统一包56来源映射149个原任务后，后续API20→34/API16→APK32都继续归属原BT-FIX-NOW-UI-001的增量证据，而该任务一直PARTIAL；唯一AIR009仍进行中，LIVE门没有重写。这解释了“改了很多但清单完成项不增加”：开发在补同一用户链的很多细节，整体需求验收尚未达成。它也揭示交付需要更小而完整的可验收用户结果，不能一直只积累构建编号和切片证据。

## 已确认的管理与文档问题

- 未完成需求有多种原因：Now/PG/UI扩展仍只交付切片；底层领域DONE被误解为统一用户体验DONE；少数合同/enum/接口只是占位；真实身份、供给、许可和账号欠费属外部阻塞；测试/文档/发布状态缺少一张当前可读矩阵。不能把它们统称“还在测试”。
- 日常验证频率的用户要求已经在AGENTS 2026-10-06写清：每项只跑相关单元，必要Flutter widget算单元，不重复全Go/Flutter、全分析、构建、DB整检；后续整合/发布单独安排。旧RELEASE-QUALITY-GATES和V4/V5协议有逐项全量/构建门，要由该最新用户指示覆盖。原测试和失败记录必须保留；不能删测试提速。
- 流程已保存大量冻结帧、逆字节核对、归档、报告与重验。它们是当前执行负担的一部分，但本报告未测量时间，不能编造占比或断言编译器本身就是全部原因。
- 文档新旧混排，甚至标canonical的目标架构正文仍写“没有Memory表/provider”，而后续实际代码和队列已有Memory/真实模型；2026-10-08媒体部署预案仍写无上传路由，但当前已注册会话图片路由。应明确历史快照和当前增量，视频未实现与图片已实现必须分开。
- `TASK-MAPPING.json`首页仍停API20，`increment-26/TASK-MAPPING.md`有较新逐项状态，而最新API33的增量报告没有对应同名逐项矩阵。实际进展存在，但状态入口分散，需求追踪没有成为用户能直接看懂的当前结果。

## 建议的最小下一步（复用原任务，不新增一套队列）

1. 先收敛Now页面、唯一面板/键盘/返回、冷启动首点、顶栏与输入层级到可截图验收的状态。用实际主仓库同构建截图核对十屏参考；只做改动相关单元，相关用户路径跑通。视觉合格应是该切片的明确验收结果。
2. 收口当前已实施的S1/S2：作者主页、本人编辑入口、TA地图当前API34的客户端、内容权限/数量、返回恢复。补T03/04/06/07差异，不能把全部S1/S2叫已完成。
3. 查询链单独修事实矛盾、失败轮不保留不属于本轮的成功点/卡、宽化回退、通用中文跟进。真实模型复验等待腾讯账号恢复；站内内容不必等待SearchPro许可。
4. 交付收藏→原地图→选点→路线/分享，再做Journey/ThemeMap基本同图读取；按用户统一方案继续S3。收藏基础DONE不阻止补已有Now018–024。
5. 做统一typed draft，再扩材料/语音；把“材料保存到本人消息”和“模型处理材料”分别标明。不要用有图片按钮冒充识图完成。
6. Agent成长继续明确偏好、实际排序、纠正删除的可见闭环；通用行为候选/模型私人Memory必须按原AIR043–050补齐，不能把Profile表/API当训练完成。
7. 数据迁移、生产上线、全量整合回归保留为独立后续检查点。当前报告整理不自动授权生产切流，也不需要用全量测试才能完成本次只读核查。

## Now30 + PG26 逐项状态附录

下表基于已保存的increment-26逐项矩阵，再叠加API27–34的后续证据。整需求状态仍均PARTIAL；“未接通”更准确反映具体子能力。每行原任务ID保持不变，不把56条来源再加成56个执行任务。

|需求|原任务|本次实际差异|仍缺|状态|
|---|---|---|---|---|
|BT-NOW-20261008-001：核对当前实现并合并既有任务|BT-FIX-NOW-UI-001, BT-AUD-001, BT-V4-AUD-001, BT-V5-INT-001, BT-RUN-001|保留此前实现和验收历史；本切片无新增整项验收。|整包业务验收未完成。|PARTIAL|
|BT-NOW-20261008-002：访客首页默认阿伯丁并支持城市恢复|BT-V4-CTX-002, BT-V4-NOW-006, BT-MAP-001, BT-RUN-001|保留此前实现和验收历史；本切片无新增整项验收。|真实手选城市冷恢复、GPS拒绝及国内底图失败全链未验。|PARTIAL|
|BT-NOW-20261008-003：隔离实际位置探索城市查询范围与公开范围|BT-V4-CTX-001, BT-V4-CTX-002, BT-V4-NOW-006, BT-V4-PRV-001, BT-V4-PRV-002|保留此前实现和验收历史；本切片无新增整项验收。|GPS观测/权限/失败及持久相机状态。|PARTIAL|
|BT-NOW-20261008-004：重做城市选择器并核查 Civu 目的地复用|BT-V4-CTX-002, BT-V4-NOW-006, BT-V4-PLC-001, BT-POL-001|保留此前实现和验收历史；本切片无新增整项验收。|选择器仍混排城市与ONLINE/本人内部范围；封面/国家/搜索/最近/别名未完成。|PARTIAL|
|BT-NOW-20261008-005：精简首页并迁移社交与内部范围入口|BT-V4-NOW-001, BT-V4-NOW-002, BT-V4-NOW-003, BT-NOW-001, BT-PUL-001, BT-V5-AGE-015, BT-V5-AGE-018, BT-V5-AGE-019|对称32dp视觉/48dp触摸胶囊；侧栏固定底部账户仅图标，新对话保留原行为。|全角色/城市选择器仍未整包验收。|PARTIAL|
|BT-NOW-20261008-006：明确区域探索和地图数量语义|BT-PUL-001, BT-SRC-001, BT-V4-MAP-001, BT-V4-NOW-004|保留此前实现和验收历史；本切片无新增整项验收。|bounds/筛选/已开启实体类型/聚合统一数量语义。|PARTIAL|
|BT-NOW-20261008-007：统一 Now 地图工作区和状态恢复|BT-MAP-001, BT-MAP-002, BT-MAP-003, BT-AGT-004, BT-NOW-003, BT-V4-CTX-002, BT-V4-NOW-006, BT-V4-MAP-001, BT-V4-MAP-002|API30/APK26公开内容按层返回、相机/地点滚动恢复；API32/APK30手动消息阅读返回有同构建录像。|S3 Journey/ThemeMap/Saved、全部关系范围、跨账号、冷相机恢复和全键盘仍未整包。|PARTIAL|
|BT-NOW-20261008-008：建立唯一输入框和材料与操作加号菜单|BT-AGT-003, BT-V5-AIR-030, BT-V5-AIR-031, BT-V4-AGF-001, BT-V4-ACTN-001|文字/短链接、PNG/JPEG本机检查并保存到本人消息；API16/APK14 human-only保存/读像素/删除已有设备证据。|语音、视频、文件、统一业务typed draft未完成；模型不读取图片。|PARTIAL|
|BT-NOW-20261008-009：支持中英文语音转文字和安全取消|BT-AGT-003, BT-V5-AIR-034|保留此前实现和验收历史；本切片无新增整项验收。|UNCONNECTED：中英STT、停止/取消/重试、编辑后手动发送。|PARTIAL|
|BT-NOW-20261008-010：复用 Runtime 建立类型化可编辑草稿|BT-AGT-001, BT-AGT-002, BT-AGT-003, BT-V4-AGF-001, BT-V4-ACTN-001, BT-V5-AIR-036, BT-V5-AIR-037, BT-V5-AIR-038, BT-V4-MOM-001, BT-V4-ORG-001|保留此前实现和验收历史；本切片无新增整项验收。|通用typed envelope、跨类型转换、受众/材料版本、检查/提交/恢复。|PARTIAL|
|BT-NOW-20261008-011：接通附件生命周期和上传恢复|BT-V5-AIR-030, BT-V5-AIR-031, BT-V5-AIR-032, BT-V5-AGE-023|human-only图片原prepare/save/read/delete；已有有界同构建设备证据与晚回/ABA单元。|图片理解、视频/文件、全冷恢复和续传仍未接齐；图片保存不等于模型接通。|PARTIAL|
|BT-NOW-20261008-012：照片地点建议与发布隐私|BT-V5-AIR-030, BT-V5-AIR-032, BT-V5-AGE-020, BT-V5-AGE-021, BT-V5-AIR-033, BT-V4-MOM-001, BT-V5-AGE-003, BT-V5-AGE-049|保留此前实现和验收历史；本切片无新增整项验收。|GeoDraft候选、冲突与统一图文发布draft；去EXIF不是地点建议。|PARTIAL|
|BT-NOW-20261008-013：区分提及好友与发送给好友|BT-V4-TIE-001, BT-V4-CHT-003, BT-V4-ACTN-001, BT-V5-AIR-040, BT-V5-AGE-042|保留此前实现和验收历史；本切片无新增整项验收。|UNCONNECTED：@引用与显式send-to-friend统一draft。|PARTIAL|
|BT-NOW-20261008-014：来源改为逐段紧凑引用标签|BT-AGT-002, BT-V4-NOW-004, BT-V5-AIR-024, BT-V5-AIR-027|API27/APK20真实OSM有来源模型回复，紧凑标签/来源详情；后续区分资料维护者与原网站。|通用多源与全文事实正确仍未整包；Prego邮编文本矛盾保留，原网站外部加载未全部验证。|PARTIAL|
|BT-NOW-20261008-015：保持来源事实一致与冲突可见|BT-V5-AIR-024, BT-V5-AIR-027, BT-V5-AIR-028, BT-V5-AIR-039, BT-V4-PLC-001, BT-V4-PLC-005|字段原文/TTL/来源校验；API27不足预约资料明确说明；API32/33阻止过期和失败尾轮宽化回退。|Prego已有邮编却模型称缺仍未修；当前营业/假日时区/通用冲突及全文事实核验未完成。|PARTIAL|
|BT-NOW-20261008-016：消除地点卡重复动作并明确导航语义|BT-V4-ACTN-001, BT-V4-NAV-001, BT-MAP-002, BT-V4-NOW-004, BT-V4-PLC-005|Glenhouse同消息卡/地图/详情已在API27/APK20；API29十餐厅卡/地图同ID；同Task刷新校验原实体/来源。|全部新Entity/Save/share/route完整原地图链未验；最终APK30延迟聚类首击选择未验。|PARTIAL|
|BT-NOW-20261008-017：收藏持久化和可感知反馈|BT-SAV-001, BT-PLN-001, BT-V4-ACTN-001, BT-V4-PRV-001|保留此前实现和验收历史；本切片无新增整项验收。|中文召回、登录续办、重启、取消后的地图恢复与UNKNOWN完整验收。|PARTIAL|
|BT-NOW-20261008-018：收藏整理复用与 Now 地图展示|BT-SAV-001, BT-PLN-001, BT-V5-AGE-027, BT-V5-AGE-029, BT-V5-AGE-030|保留此前实现和验收历史；本切片无新增整项验收。|UNCONNECTED：收藏整理/筛选/notes/compare/Now地图与路线复用。|PARTIAL|
|BT-NOW-20261008-019：同页分享预览和明确发送结果|BT-V4-CHT-003, BT-V4-ACTN-001, BT-V4-TIE-001, BT-V4-E2E-001, BT-V5-AIR-038, BT-V5-AIR-040|保留此前实现和验收历史；本切片无新增整项验收。|统一Now文字媒体/route/theme/journey预览与发送结果。|PARTIAL|
|BT-NOW-20261008-020：分享链接权限和生命周期|BT-V4-CHT-003, BT-V4-ACTN-001, BT-V4-PRV-001, BT-V4-PRV-002|保留此前实现和验收历史；本切片无新增整项验收。|UNCONNECTED：snapshot/live链接发行、到期、撤销及对象变化生命周期。|PARTIAL|
|BT-NOW-20261008-021：路线计划在原地图预览与编辑|BT-V4-NAV-001, BT-V4-ACTN-001, BT-MAP-001, BT-MAP-002|保留此前实现和验收历史；本切片无新增整项验收。|UNCONNECTED：同Now真实route geometry、起点/mode/stop顺序/编辑。|PARTIAL|
|BT-NOW-20261008-022：迁移动态创建的最小完整链路|BT-V4-MOM-001, BT-V4-MOM-002, BT-V5-AGE-023, BT-V5-AGE-020, BT-V5-AGE-021, BT-V5-AGE-022, BT-V5-AIR-030, BT-V5-AIR-033|API30/APK26真实API发布隔离合成Moment，B直接Now地点→Moment→作者→返回；隐私/Block/withdraw与导航重验有证据；API33本人编辑入口实际返回。|多动态/分页T03、A/B/C聚合T04、全部缓存/媒体撤权、统一输入draft及S2全范围未完成；API34作者地图仅后端。|PARTIAL|
|BT-NOW-20261008-023：迁移主题地图创建收藏和复用|BT-SAV-001, BT-V5-AGE-029, BT-V5-AGE-030, BT-V4-MAP-001|保留此前实现和验收历史；本切片无新增整项验收。|UNCONNECTED：ThemeMap创建/保存/重开/权限副本/复用。|PARTIAL|
|BT-NOW-20261008-024：迁移旅程与外部计划导入|BT-AGT-003, BT-V5-AGE-029, BT-V5-AGE-030, BT-V5-AIR-021|保留此前实现和验收历史；本切片无新增整项验收。|UNCONNECTED：Journey与外部计划解析/可编辑导入/逐日地图恢复。|PARTIAL|
|BT-NOW-20261008-025：统一活动询问创建与加入|BT-ACT-001, BT-RSV-001, BT-NTF-002, BT-AGT-001, BT-V4-ORG-001, BT-V4-ACTN-001, BT-V4-CHT-004, BT-V5-AGE-022|保留此前实现和验收历史；本切片无新增整项验收。|统一Now创建/加入draft连续业务验收。|PARTIAL|
|BT-NOW-20261008-026：中文优先的完整业务验收|BT-AGT-001, BT-AGT-002, BT-AGT-004, BT-V5-AIR-007, BT-V5-AIR-009, BT-V5-AIR-024, BT-V5-AIR-025, BT-V5-AIR-026, BT-V5-AIR-039, BT-TST-002|API27真实3模型/4轮同Task、中文IME地址0模型；API29真实十OSM餐厅；API31名称地址跟进实际HTTP。|通用中文Compare/Refine/多实体仍未完成；API32腾讯402/403004账号服务隔离待恢复；全文事实矛盾保留。|PARTIAL|
|BT-NOW-20261008-027：复用 Civu 能力前完成契约与数据保护审查|BT-AUD-001, BT-V4-AUD-001, BT-V5-AIR-005, BT-V4-SAF-004, BT-V4-PLC-001, BT-V4-MOM-001, BT-V5-AGE-029, BT-V5-AGE-030|保留此前实现和验收历史；本切片无新增整项验收。|文件级许可/依赖/数据归属及隔离备份恢复验收。|PARTIAL|
|BT-NOW-20261008-028：统一权限确认幂等与未知结果恢复|BT-V4-ACTN-001, BT-V4-SAF-004, BT-V4-PRV-001, BT-V4-PRV-002, BT-V5-AGE-003, BT-V5-AGE-041, BT-V5-AGE-042, BT-V5-AGE-049, BT-V5-AGE-062, BT-V5-AIR-011, BT-V5-AIR-018, BT-V5-AIR-023, BT-V5-AIR-037, BT-V5-AIR-038, BT-V5-AIR-040|保留此前实现和验收历史；本切片无新增整项验收。|所有新draft动作和所有领域字段使用者的完整授权/撤权验收。|PARTIAL|
|BT-NOW-20261008-029：面板键盘可访问性和性能回归|BT-MAP-001, BT-PER-001, BT-PER-002, BT-V4-MAP-002, BT-POL-001, BT-TST-001, BT-TST-002|相关widget与API27中文IME可见性，API30公开面板与API32消息阅读返回有界真机。|侧栏冷首点APK31/32仍无响应；全部面板/键盘/辅助技术/系统动效/性能及消费视觉未验收。|PARTIAL|
|BT-NOW-20261008-030：分批验收和真实证据交付|BT-FIX-NOW-UI-001, BT-TST-001, BT-TST-002, BT-V4-E2E-001, BT-V4-E2E-002, BT-V4-PIL-001, BT-V4-PIL-002, BT-V4-PIL-003, BT-REL-001, BT-V5-AIR-009-LIVE, BT-V5-AIR-054-LIVE|API27/30/32/33增量冻结与视频保存，API34作者范围SQL/HTTP42项检查；仍保留256原任务。|Now30/PG26/UI42整包PARTIAL；全部T01–33未整体通过，正式IdP/部署/全量NOT_RUN；API34不冒充手机API33。|PARTIAL|
|BT-PG-20261008-01：证据审计与旧队列对账|BT-AUD-001, BT-V4-AUD-001, BT-V5-INT-001, BT-FIX-NOW-UI-001|保留此前实现和验收历史；本切片无新增整项验收。|全部26项业务验收未完成。|PARTIAL|
|BT-PG-20261008-02：来源使用权限与留存审查|BT-V4-PLC-001, BT-V4-PLC-002, BT-V5-AIR-024, BT-V5-AIR-030, BT-V5-AGE-062|已用开放OSM事实原批准字段；API27模型分支可使用站内合法地点，不依赖SearchPro。|SearchPro持久化/复用许可仍PENDING仅限制该源；普通Now通用库外自动补搜未完成。|PARTIAL|
|BT-PG-20261008-03：查询与学习链路可观测性|BT-V5-AIR-049, BT-V5-AIR-011, BT-V5-AIR-014, BT-V5-AIR-015, BT-V5-AIR-017, BT-V5-AGE-064, BT-V5-AIR-048|保留此前实现和验收历史；本切片无新增整项验收。|通用全链观测与生产监控未验。|PARTIAL|
|BT-PG-20261008-04：规范地点实体与合并规则|BT-DAT-001, BT-V4-PLC-001, BT-V4-PLC-002|15条通过原审核生成原Place ID；原Submit事务按provider/node、完整同source事实及最终期限复验复用，API26真实重采reusedCurrent15且0新增。|跨来源大规模实体合并仍未整包验收。|PARTIAL|
|BT-PG-20261008-05：合规地点候选入库|BT-DAT-001, BT-DAT-002, BT-V4-PLC-001, BT-V4-PLC-002|原真实Overpass15候选经用户核对仅隔离发布；再次采集重复候选FAIL保留；当前第三次真实采集原15复用且不延长48字段TTL。|普通Now自动库外采集/多人审核/批量跨源闭环仍未完成。|PARTIAL|
|BT-PG-20261008-06：营业信息字段级新鲜度|BT-V4-PLC-002, BT-V4-PLC-005, BT-V5-AIR-024|原营业字段6小时刷新期，名字/地址/点坐标24小时；缺失不补造、原文不推断营业状态。|假日例外/时区/当前开门状态未实现。|PARTIAL|
|BT-PG-20261008-07：活动独立时效实体|BT-ACT-001, BT-V4-PLC-004, BT-V4-NOT-001, BT-V5-AIR-024|保留此前实现和验收历史；本切片无新增整项验收。|两投影真实DB撤权NOT_RUN；机会deadline原始地点路径尚未覆盖。|PARTIAL|
|BT-PG-20261008-08：本地充分性判定与受控补搜|BT-AGT-001, BT-AGT-002, BT-V5-AIR-025, BT-V5-AIR-026, BT-V5-AIR-039, BT-V5-AIR-011|原已审核客观地点本地充分性/地址/营业闭集；API27真实模型，API29真实十OSM餐厅。|通用不足补搜/普通账号多领域未完成；API32供应商HTTP402阻断后续真实成功验收。|PARTIAL|
|BT-PG-20261008-09：授权缓存与并发去重|BT-V5-AIR-020, BT-V5-AIR-026, BT-V5-AIR-011|保留此前实现和验收历史；本切片无新增整项验收。|UNCONNECTED：授权cache/singleflight/并发去重。|PARTIAL|
|BT-PG-20261008-10：库外地点收藏与身份解析|BT-SAV-001, BT-V4-PLC-001, BT-V4-PLC-005|保留此前实现和验收历史；本切片无新增整项验收。|库外pending reference持久化与canonical身份解析。|PARTIAL|
|BT-PG-20261008-11：地点与站内内容权限关联|BT-V4-MOM-001, BT-V4-MOM-002, BT-V5-AGE-020, BT-V5-AGE-021, BT-V5-AGE-027, BT-V5-AIR-030|API30公开Moment/Place/作者API与原地图同实体；隐私作者成对NULL、Block/撤回读取拒绝；API34仅后端public-author范围。|关系范围/好友/作品分页/T03T04T06T07、所有媒体和cache即时撤权未整包。|PARTIAL|
|BT-PG-20261008-12：个人数据与城市公共资料分层|BT-V5-AGE-002, BT-V5-AGE-003, BT-V5-AGE-060, BT-V5-AGE-062, BT-V5-AIR-022, BT-V5-AIR-023, BT-V5-AIR-046|保留此前实现和验收历史；本切片无新增整项验收。|全部私人资料类型/可选cache/通用模型私人Memory使用未验。|PARTIAL|
|BT-PG-20261008-13：日常事件与学习资格接线|BT-V5-AGE-064, BT-V5-AIR-014, BT-V5-AIR-015, BT-V5-AIR-016, BT-V5-AIR-019, BT-V5-AIR-043, BT-V5-AGE-024, BT-V5-AGE-026|保留此前实现和验收历史；本切片无新增整项验收。|日常聊天/收藏事件学习资格接线。|PARTIAL|
|BT-PG-20261008-14：明确偏好写入闭环|BT-V5-AGE-004, BT-V5-AGE-005, BT-V5-AGE-007, BT-V5-AGE-069, BT-V5-AIR-043, BT-V5-AIR-044|本人海鲜/不辣→明确确认→PRIVATE EXPLICIT Memory→普通Now使用→更正删除已有隔离真实HTTP/PG证据。|仍为海鲜/辣闭集；通用偏好/行为学习/模型私人Memory未接齐；当前手机全写纠正删除链NOT_RUN。|PARTIAL|
|BT-PG-20261008-15：行为证据与候选偏好|BT-V5-AGE-005, BT-V5-AGE-007, BT-V5-AGE-008, BT-V5-AGE-009, BT-V5-AGE-025, BT-V5-AGE-027, BT-V5-AIR-045, BT-V5-AIR-035|保留此前实现和验收历史；本切片无新增整项验收。|行为证据候选链与可信度评估。|PARTIAL|
|BT-PG-20261008-16：矛盾修正与时效处理|BT-V5-AGE-011, BT-V5-AGE-010, BT-V5-AIR-018, BT-V5-AIR-047, BT-V5-AIR-044|保留此前实现和验收历史；本切片无新增整项验收。|跨来源通用冲突与所有衍生消费者时效。|PARTIAL|
|BT-PG-20261008-17：记忆实际进入检索与排序|BT-V5-AGE-033, BT-V5-AGE-034, BT-V5-AGE-035, BT-V5-AGE-036, BT-V5-AGE-062, BT-V5-AIR-022, BT-V5-AIR-023, BT-V5-AIR-025, BT-V5-AIR-026, BT-V5-AIR-046|有界显式口味Memory在普通Now同事务真实改变筛选排序；当前版本与旧来源/卡失效。|通用检索质量、更多Memory类型、模型私人Memory消费和可见解释未完成。|PARTIAL|
|BT-PG-20261008-18：用户记忆控制与反馈入口|BT-V5-AGE-046, BT-V5-AGE-047, BT-V5-AGE-048, BT-V5-AGE-049, BT-V5-AGE-011, BT-V5-AIR-050|保留此前实现和验收历史；本切片无新增整项验收。|统一控制面、所有类型效果反馈、导出与真机操作。|PARTIAL|
|BT-PG-20261008-19：撤回删除与衍生数据清理|BT-V5-AGE-011, BT-V5-AIR-018, BT-V5-AIR-044, BT-V5-AIR-047, BT-V5-AGE-049|保留此前实现和验收历史；本切片无新增整项验收。|所有cache/draft/outbox/Opportunity字段消费者撤回清理全链。|PARTIAL|
|BT-PG-20261008-20：主动接收筛选与通知|BT-V5-AGE-037, BT-V5-AGE-038, BT-V5-AGE-040, BT-V5-AGE-041, BT-V5-AGE-042, BT-V5-AGE-043, BT-V5-AIR-041, BT-V4-NOT-001|保留此前实现和验收历史；本切片无新增整项验收。|用户明确开启后的主动匹配/通知筛选。|PARTIAL|
|BT-PG-20261008-21：对外行动与邀请接受边界|BT-V5-AGE-044, BT-V5-AGE-041, BT-V5-AIR-036, BT-V5-AIR-037, BT-V5-AIR-038, BT-V5-AIR-040, BT-V5-AIR-042, BT-V4-ACTN-001|保留此前实现和验收历史；本切片无新增整项验收。|新偏好驱动邀请筛选和实际接受两条真实授权链。|PARTIAL|
|BT-PG-20261008-22：安全与敏感推断防线|BT-V5-AGE-002, BT-V5-AGE-003, BT-V5-AGE-060, BT-V5-AGE-062, BT-V5-AGE-066, BT-V5-AIR-011, BT-V5-AIR-018, BT-V5-AIR-023, BT-V5-AIR-028, BT-V5-AIR-046, BT-V5-AIR-053, BT-V4-SAF-004|保留此前实现和验收历史；本切片无新增整项验收。|所有敏感推断/网页注入/材料/新工具出口完整回归。|PARTIAL|
|BT-PG-20261008-23：中文纵向验收与回归|BT-FIX-NOW-UI-001, BT-TST-001, BT-TST-002, BT-V5-AGE-071, BT-V5-AIR-054|API27中文IME地址与真实有来源模型；API30S1和API32阅读返回有界手机证据；API33本人返回。|中文全部纵向/全T01–33/真实供给生产身份未整包；供应商402与事实矛盾未解除。|PARTIAL|
|BT-PG-20261008-24：匹配质量与节约评估|BT-V5-AIR-013, BT-V5-AIR-048, BT-V5-AIR-049, BT-V5-AIR-052, BT-V5-AIR-054, BT-V5-AGE-008, BT-V5-AGE-025|保留此前实现和验收历史；本切片无新增整项验收。|真实匹配质量/真实账单成本评估与生产基线。|PARTIAL|
|BT-PG-20261008-25：迁移发布回滚与独立门禁|BT-V5-AIR-005, BT-V5-AIR-053, BT-V5-AGE-066, BT-TST-002, BT-REL-001, BT-V4-PIL-001, BT-V4-PIL-002, BT-V4-PIL-003|保留此前实现和验收历史；本切片无新增整项验收。|生产迁移/备份恢复/发布回滚/真实IdP/独立门禁NOT_RUN或BLOCKED。|PARTIAL|
|BT-PG-20261008-26：增量交付与状态回写|BT-FIX-NOW-UI-001, BT-AUD-001, BT-V4-AUD-001, BT-V5-INT-001|保留原Now/PG/AGE/AIR/DISC映射和完成历史，追加实际代码与证据。|全包PARTIAL，不改变原上线门。|PARTIAL|
