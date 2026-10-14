# Birdtie 全站文案与媒体整改结果（2026-10-13）

本轮以 D:\Project\birdtie 当前未提交工作树为准，保留旧任务、数据、失败记录及已有改动，没有回滚报告引用提交。18项已逐项核验：2项已修保留、12项本轮源修复、4项媒体/旧数据仍部分完成。SOURCE_FIXED 是源码处置结果，不能解释为全站真人体验验收。

首页遵守10月11日的联系＋通知、短头像列和局部透明材质；全宽底部仅用于展开后的长阅读/编辑。没有新增收件箱入口。

| 项目 | 状态 | 实际处置与剩余 | 关键源文件（workspace目录，已保存最终SHA） |
| --- | --- | --- | --- |
| BT-COPY-001 | ALREADY_FIXED | Now首页已无常驻空意图卡；本轮核验，不重复修改。 | `map_workspace.dart / active_social_intent_card.dart` |
| BT-COPY-002 | ALREADY_FIXED | 城市选择只保留城市搜索、名称、地区与当前选择；生活情境另页。 | `map_workspace.dart` |
| BT-COPY-003 | SOURCE_FIXED | 删除工具重复副标题及专用间距；私人草稿显示仅自己可见，正文与发布确认保留。 | `map_workspace.dart` |
| BT-COPY-004 | PARTIAL_MEDIA | 人物受权头像、社群受控头像贯通SQL→Go→Dart→Image；活动真实时间地点、位置精度与私密边界保留。地点/活动/组织受控封面仍缺。 | `agent_entity_result_card.dart / agent_result_projection.dart / postgres/agent_result_projection.go` |
| BT-COPY-005 | SOURCE_FIXED | 真实来源名、HTTPS链接、更新时间、到期与未核实提示保留；删除读取租期/预算说明，内部时效与权限不变。 | `public_query_field_evidence.dart / public_query_field_evidence_panel.dart` |
| BT-COPY-006 | PARTIAL_MEDIA | 无场地资料删除整块及间距；有资料按需展开；简短错误和原请求核实保留。地点照片接口/渲染仍缺。 | `place_detail_sheet.dart` |
| BT-COPY-007 | PARTIAL_MEDIA | 真实cityName贯通SQL/DTO/列表；社群真实缩略图已贯通，保留分页、备注和原详情。地点/活动媒体仍缺。 | `saved_items.dart / saved/model.go / postgres/saved_page.go` |
| BT-COPY-008 | SOURCE_FIXED | 现有通知组件去技术页头；非空分组此前已修。原请求对方/消息发送者头像与正文保留；不新增收件箱入口。 | `inbox.dart / remote_inbox_source.dart / postgres/inbox_page.go` |
| BT-COPY-009 | SOURCE_FIXED | 删除空分组及说明/间距；全空一行；有数据保留人物活动、日期地点与理由，不暗示已参加。 | `social_now_page.dart` |
| BT-COPY-010 | SOURCE_FIXED | 空历史整区省略；错误短状态及重试，不透异常；个人商家入口仍按原身份能力开放。 | `sidebar.dart` |
| BT-COPY-011 | SOURCE_FIXED | 按账号/通知/隐私/智能体与偏好/帮助组织，保留授权与预算入口、当前真实值，诊断仍仅debug。 | `settings_page.dart` |
| BT-COPY-012 | SOURCE_FIXED | 原摘要主标题此前已修；JSON改中文字段与真实正文，未知补充诚实说明且编辑时完整保留。 | `agent_profile_page.dart / memory_content_fields.dart` |
| BT-COPY-013 | SOURCE_FIXED | 中文记忆状态、修改前后与确认，去对象ID/终态/租期术语；CAS、授权、删除后果与未知核实保留。 | `agent_memory_correction_page.dart` |
| BT-COPY-014 | SOURCE_FIXED | 昵称/所在城市/交流语言等普通标签，私密状态和保存后果可见；原预填/可选跳过不重复开发。 | `content/agent_seed_sheet.dart` |
| BT-COPY-015 | SOURCE_FIXED | 中文城市/学校/兴趣字段，私密徽标；保持原CITY/INSTITUTION/ONLINE语义，过去生活不伪称到访证明。 | `person_contexts_page.dart` |
| BT-COPY-016 | PARTIAL_LEGACY_MEDIA | 社群空组/宣传删除；新受控真实头像已接列表、详情、结果和收藏。旧avatar_url未迁入，不静默抓取或删数据。 | `community_page.dart / community_avatar.dart / migrations/135_community_avatars.sql` |
| BT-COPY-017 | SOURCE_FIXED | 可选本地选择与预览，原创建只提交一次，头像独立具体批准/CAS/原操作核实；简介/受众/加入方式保留。 | `community_avatar_editor.dart / community_page.dart / httpapi/community_avatar.go` |
| BT-COPY-018 | SOURCE_FIXED | 删除线上标题快照说明与专用间距，保留真实标题、情境、期限与已实现操作及权限检查。 | `map_workspace.dart` |

## 二级页和补扫

另外清理任务授权ID、模型价格/提示模板版本、运行版本、候选来源token、定时汇总工程解释；保留30天滚动额度、模型预算、撤回/停止后果、所选授权范围与独立确认到期时间。活动页面无数据组完整退场，非阿伯丁创建明确禁用原因；个人资料重复大标题退场；商家认领提示修正实际入口；二维码不裸露账号UUID；非人物关注使用对应图标、不借个人头像且未接详情的行不伪装按钮。互动记录授权使用中文，好友关系/30天数量/活跃日、关闭、不读正文及共同报名非到场等边界保留。

47组实际路径的静态补扫含弹层、空/错/草稿/未知/身份分支。组织UUID邀请、商家新增成员的目标输入、组织任意坐标选择、部分旧写操作的原操作UNKNOWN恢复、非人物关注详情接线仍需后续；静态存在不计运行通过。

## 媒体与生成素材

Person头像来自原当前受权账号；Inbox头像只来自原connection request对方/原消息发送者。新Community缩略图采用独立社区ID、owner/admin、当前成员/屏蔽/会话、规范化摘要、具体批准、CAS及原操作回执，贯通原结果与收藏。旧外链不自动抓取；私人Moment/视频不借作公开地点/活动封面。

按用户后续请求生成4张独立插画（咖啡馆、公园、徒步、桌游），均标“AI合成样例”，有原文件/尺寸/SHA及来源记录；没有映射到真实POI、官方活动或自动导入生产。它们不补齐当前尚缺的Place/Activity/Organization媒体合同。DATA001保留开发虚构标识，完整获准多图/真人/设备/并行性能数据集仍PARTIAL。

## 证据与验收边界

[17对前后截图清单](../testing/evidence/now-v4-v41-integration-20261010/continuation20/copy29/final/screenshots.json) · [真实测试与NOT_RUN矩阵](../testing/UI-CONTENT-MEDIA-ACCEPTANCE.md) · [最终源码SHA](../testing/evidence/now-v4-v41-integration-20261010/continuation20/copy29/final/source-manifest.json) · [合成图](../testing/evidence/now-v4-v41-integration-20261010/continuation20/copy29/generated-media/README.md)

前后共13对同数据、同尺寸、正常字号的隔离Flutter样例及4对实际localhost匿名截图。样例标识明确；无图列表不伪称媒体成功。旧Ahem方块、错误stage和测试失败保留并排除合格截图。匿名登录服务不可用、未接Android设备、真实账号与生产场景、键盘/读屏及地图媒体性能为NOT_RUN。

默认严格analyze首轮0错误/16警告/721提示、exit1；16项编译器认定多余导入/空值断言已精确清理。后续 --no-fatal-infos 为0错误/0警告/721提示、exit0，未宣称“无问题”。API11个程序、Android profile与Web42文件编译通过，不能据编译宣布全站体验通过。

原任务队列256项：DONE183、TODO47、PARTIAL12、BLOCKED13、IN_PROGRESS1；73项未闭环。本轮只向原BT-FIX-NOW-UI-001追加证据，未重复建任务或篡改255个其它任务。仍继续当前整改与原任务清单。

## Copy30 后续完成与当前交付

商家成员公开姓名已贯通原授权名单的 PostgreSQL → Go 可选 displayName → Dart 类型模型 → 名单和已有成员选择器。字段授权与当前会话/来源再核验仍有效；管理权限不依赖姓名是否可展示。已有成员通常无需输入 UUID；缺名、重名和新增成员仍按明确对象核实，姓名不是实名证明，也不扩大授权。

本增量改动 11 个已登记文件。Go 3 个顶层测试含 17 个子用例通过，原生数据库 2 项通过/0 SKIP。客户端 50 个不同直接用例分批闭合，另 1 个隔离渲染用例；首轮 46 PASS/3 FAIL、定向修复和之后新增分支记录均保留，不能称一次最终源码 50 PASS。当前 5 文件分析为 0 error/0 warning/1 info，明确使用 --no-fatal-infos。当前 API 11 程序、Android profile 和 Web 42 文件编译通过。

API/Web 本机 QA 使用 127.0.0.1:18112；APK 编译配置仍为设备本地 127.0.0.1:18091，尚未装设备，也未通过端口转发连接新服务，不宣称 APK 已连入新运行时。原库身份、表计数保持，未重复迁移 135，外部提供方仍关闭。

[19 对累计前后截图](../testing/evidence/now-v4-v41-integration-20261010/continuation20/copy30/final/screenshots.json) 含上批 17 对与本批 2 对商家隔离样例，均保留独立来源与 SHA。商家名单前图使用相同含可选姓名的 fixture；其前后图片不能代替后端字段缺失/新增的合同证据。本批后图取自 after03，之后转移返回编辑的角色修复由 targeted04 验证，后图不冒称最新装包真人界面。另有最新本机匿名首页单张运行截图；真实商家、真机、键盘/读屏、性能、所有角色全流程及生产验收仍 NOT_RUN。

[Copy30 最终源码与证据](../testing/evidence/now-v4-v41-integration-20261010/continuation20/copy30/final/receipt.json) · [最新 APK](../../work/now-v4-v41-integration-20261010/continuation20/copy30-client-apk-build01/birdtie-members30.apk)。Copy29 历史总报告已按原 SHA 保存，不覆盖其冻结记录。当前原任务仍 IN_PROGRESS，73 项未闭环。
