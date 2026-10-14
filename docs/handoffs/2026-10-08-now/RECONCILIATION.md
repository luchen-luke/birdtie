# Now 2026-10-08 需求接续对账

> 后续增量（2026-10-09）：下文为原只读观察与当时NOT_RUN，保留不改。已开发的当前API20/APK16、单元/隔离HTTP/真机证据见[增量报告](../../testing/evidence/now-place-growth-2026-10-09/REPORT.md)和[逐项映射](../../testing/evidence/now-place-growth-2026-10-09/TASK-MAPPING.md)；全包仍PARTIAL。


**状态：PARTIAL。对账完成不等于实现或验收完成。** 本次只写两个新对账文件；未改源码、队列、运行态、配置、数据或旧报告。

- 正式仓库 `D:\Project\birdtie`；分支 `codex/birdtie-import`；HEAD `4637cfb6e7758fc4fa5dd548fad623a8484c4cfb`。
- 原执行入口仍为 **BT-FIX-NOW-UI-001（PARTIAL）**。30 NOW、42 UI、22 DISC是重叠来源索引，不加总为独立工作量、不导入新队列；旧DONE和历史保留。
- 已读本目录master/完整规范/JSON/CSV/README/VALIDATION、原四queue-state、137来源别名、UI42映射与报告、DISC原文§12、Civu矩阵和实际代码。30 JSON/CSV ID及包文件哈希一致，仅结构核对不是应用测试。
- 原auth/source-v2/Place followup/human-task-images未提交工作保留；HEAD不能冒充当前API16/APK14全源码。root新首切片在途，代码锚点记录观察时字节，不是新构建通过证明。

## 冲突判定

- **C-PLUS**：加号只保留真实接入当前会话附件 → 同输入材料与操作加号，并转换可编辑typed draft。新包覆盖菜单范围限制；保留真实接线/当前权限/确认/UNKNOWN，未接入口明确不可用，不恢复无调用helper冒充能力。

- **C-MAP**：旧Civu/业务各自Journey/ThemeMap地图方向 → Place/Saved/自己他人的ThemeMap/Journey/Moment地点在同Now地图并恢复原上下文。复用原Now host/state；不搬旧业务地图页面，不宣称TODO模块已完成，领域能力和数据保留。

- **C-HOME**：首页常驻IntentPulse/ActiveIntent/MySocialNow/seed/内部范围卡 → 首页地图+单输入，偏好/声明管理到账户设置或显式查询。新展示优先，保留SocialIntent/SocialNow/seed/Activity服务和旧DONE；AreaPulse活动计数不冒充所有地图类型。

- **C-CITY-PICKER**：唯一city入口混排城市/本人/ONLINE/组织范围 → 城市目的地选择器只城市目的地，内部范围不混排。仍唯一城市入口；原ONLINE/声明/query服务迁移而不删除，探索城市不变本人位置/公开范围。

其余身份、权限、隐私、确认、幂等、UNKNOWN、发布门仍有效。旧AGENTS暂停条款依用户后续明确恢复接续，暂停/恢复历史不重置。

## 已有有界证据

- **API16 / APK14同构建**：Museum卡→Mapbox选中pin→同Shiprow详情→返回会话保留选择。06未裁剪视频实际5.573389秒，不是完整四轮连续提交；原Task四轮与最新来源有独立GET/HTTP/native证据。
- **Human-only图片**：已持久化本人CITY Task真实user消息，合成PNG经选择/遮罩/两次复核/prepare/save/read/delete，messageIndex6。ready收据和实际像素读取共同证明保存；保存启动2.979700秒视频独自不能证明完成。Task/Run/预算摘要不变，图片不参与AI回答。
- **来源v2与有界Place追问**：第四轮实际WSA+模型交通回答保留旧六消息及Museum refs，保留周日冲突，未再把旅游网页称官方。旧v1历史不改写；未等于全事实校验、通用Agent或完整中文闭环。
- API16 `64ec29d1101d7dc2dfd29419c061eb4596834e7cfe1bcfeaf94bcc2f258b4c9f`；APK14 `e2277f15c21f71de7394c567d573cb5545ac1359f384da627e73af32aa341300`。原API14/APK12、未运行APK13与旧失败均按历史保留，不混用。

原 [TASK-MAPPING](../../testing/evidence/ui-repair-2026-10-07/TASK-MAPPING.md)、[REPORT](../../testing/evidence/ui-repair-2026-10-07/REPORT.md)、[CHECKPOINT16](../../testing/evidence/ui-repair-2026-10-07/CHECKPOINT16-DEVICE.json)仍权威；本次未重跑。

## 30项逐项映射

covered=复用基础；modified=原路径补缺；new_delta=无等价完成能力；conflict=新包明确覆盖旧展示/范围。**所有新验收仍PARTIAL**，旧任务DONE不是新验收DONE。完整原change/acceptance、依赖、函数行/哈希及双向ID见 [RECONCILIATION.json](RECONCILIATION.json)。

| NOW | 分类 | 原任务当前状态 / 旧索引 | 已覆盖部分 | 实际缺口 |
|---|---|---|---|---|
| BT-NOW-20261008-001 核对当前实现并合并既有任务 | covered / PARTIAL | BT-FIX-NOW-UI-001=PARTIAL；BT-AUD-001=DONE；BT-V4-AUD-001=DONE；BT-V5-INT-001=DONE；BT-RUN-001=DONE<br>UI 001,035,038,039,040；DISC 001,020,021,022 | 原队列、137来源别名、42 UI候选和API16/APK14已有权威位置；本文件完成接收对账，不建立第二队列。 | 对账不是执行验收；后续由root在原BT-FIX-NOW-UI-001接续，保留原门槛。 |
| BT-NOW-20261008-002 访客首页默认阿伯丁并支持城市恢复 | modified / PARTIAL | BT-V4-CTX-002=DONE；BT-V4-NOW-006=DONE；BT-MAP-001=DONE；BT-RUN-001=DONE<br>UI 011,012,013,035；DISC 004,019 | 城市目录、显式selectCity、底图错误恢复已有；原loadCities首次选择仍为空。 | 待补仅访客默认Aberdeen、探索选择持久化/冷恢复优先级；目录移除/拒定位/国内provider缺失恢复待验。root首切片在途，默认探索不等于device location。 |
| BT-NOW-20261008-003 隔离实际位置探索城市查询范围与公开范围 | modified / PARTIAL | BT-V4-CTX-001=DONE；BT-V4-CTX-002=DONE；BT-V4-NOW-006=DONE；BT-V4-PRV-001=DONE；BT-V4-PRV-002=DONE<br>UI 011,013,028,037；DISC 004,010,016,019 | Task目的地、ONLINE与手动viewport已分开；resolved出口只公开CITY+Task slots，不携完整history/GPS/私有画像。 | 缺统一探索持久模型和GPS观察授权独立状态的完整验收；默认城市不能改本人公开范围，手动探索不能被GPS覆盖。 |
| BT-NOW-20261008-004 重做城市选择器并核查 Civu 目的地复用 | conflict / PARTIAL | BT-V4-CTX-002=DONE；BT-V4-NOW-006=DONE；BT-V4-PLC-001=DONE；BT-POL-001=TODO<br>UI 010,011,012,037,041；DISC 001,004,010 | 唯一顶栏city及公开目录loading/error/retry已有；当前仍混排线上/声明/组织范围。 | 新包城市目的地选择器覆盖旧011/037城市与范围同页展示；城市封面/国家/搜索/最近/中英别名/Civu目的地许可映射待补。保留原ONLINE/声明查询服务，迁移其入口。 |
| BT-NOW-20261008-005 精简首页并迁移社交与内部范围入口 | conflict / PARTIAL | BT-V4-NOW-001=DONE；BT-V4-NOW-002=DONE；BT-V4-NOW-003=DONE；BT-NOW-001=DONE；BT-PUL-001=DONE；BT-V5-AGE-015=DONE；BT-V5-AGE-018=DONE；BT-V5-AGE-019=DONE<br>UI 008,014,036,037；DISC 013,016,019 | 账户工具、SocialNow、本人Intent/seed原能力存在；首页还挂ActiveSocialIntentCard/AreaPulseStack/社交入口。 | 按新包移走常驻空卡和内部维护范围，保留真实能力于账户设置/显式查询；范围失败局部恢复待补。root cleanhome在途未验收。 |
| BT-NOW-20261008-006 明确区域探索和地图数量语义 | modified / PARTIAL | BT-PUL-001=DONE；BT-SRC-001=DONE；BT-V4-MAP-001=DONE；BT-V4-NOW-004=DONE<br>UI 014,017,028,036；DISC 005,016,019 | typed地图有PLACE/ACTIVITY/MOMENT等许可投影；AreaPulse/search-area为Activity主路径，error与empty区分。 | 待统一已开启类型/实际bounds/filter计数/聚合及文案；0活动不能说0地点/故事，范围探索不等于Place续问。 |
| BT-NOW-20261008-007 统一 Now 地图工作区和状态恢复 | conflict / PARTIAL | BT-MAP-001=DONE；BT-MAP-002=DONE；BT-MAP-003=DONE；BT-AGT-004=DONE；BT-NOW-003=DONE；BT-V4-CTX-002=DONE；BT-V4-NOW-006=DONE；BT-V4-MAP-001=DONE；BT-V4-MAP-002=PARTIAL<br>UI 002,013,018,027,028,029,041,042；DISC 002,012,013,015,016 | 同Now地图实例、Task/每轮结果/selection与前台GET-only安静续读已有；旧轮不嫁接最新轮。 | 新包所有业务对象同Now地图覆盖旧业务另开图方向；Saved未上图、Theme/Journey未接、完整账号分区/持久草稿/自动冷启动/临时restore待补。 |
| BT-NOW-20261008-008 建立唯一输入框和材料与操作加号菜单 | conflict / PARTIAL | BT-AGT-003=DONE；BT-V5-AIR-030=PARTIAL；BT-V5-AIR-031=TODO；BT-V4-AGF-001=DONE；BT-V4-ACTN-001=DONE<br>UI 007,024,025,026；DISC 002,003,014 | 唯一文本输入与有当前本人CITY Task/user才显示的human-only图片加号真实接通；showMaterialTools只定义无现行入口。 | 材料+操作加号覆盖旧仅附件范围；真实接线/权限/确认仍必须。语音/视频/文件/链接/地点refs与同输入可编辑操作draft未接，旧独立表单不能冒充统一输入。 |
| BT-NOW-20261008-009 支持中英文语音转文字和安全取消 | new_delta / PARTIAL | BT-AGT-003=DONE；BT-V5-AIR-034=TODO<br>UI 024,026；DISC 020,021 | 文本/composing提交可复用；AIR034是相关音视频候选，不是STT实现，无等价已完成语音任务。 | UNCONNECTED：录音/STT、中英混合、stop/cancel/retry、告知/音频删除/编辑后手动发送全缺；新包未授权新供应商调用。 |
| BT-NOW-20261008-010 复用 Runtime 建立类型化可编辑草稿 | modified / PARTIAL | BT-AGT-001=DONE；BT-AGT-002=DONE；BT-AGT-003=DONE；BT-V4-AGF-001=DONE；BT-V4-ACTN-001=DONE；BT-V5-AIR-036=DONE；BT-V5-AIR-037=DONE；BT-V5-AIR-038=PARTIAL；BT-V4-MOM-001=DONE；BT-V4-ORG-001=DONE<br>UI 002,015,024,026,041；DISC 002,003,012,020 | 原Runtime、typed proposals/确认及独立SocialIntent/Moment/Activity drafts已有，native handle不可反序列化，UNKNOWN保持。 | 待统一Now typed envelope、材料/受众/收件人/版本及跨类型转换、缺项/检查/提交/恢复。不能继承领域DONE或另建Runtime。 |
| BT-NOW-20261008-011 接通附件生命周期和上传恢复 | modified / PARTIAL | BT-V5-AIR-030=PARTIAL；BT-V5-AIR-031=TODO；BT-V5-AIR-032=TODO；BT-V5-AGE-023=DONE<br>UI 024,025,027,041；DISC 006,007,020 | API16/APK14本人已存CITY Task user图片select/mask/prepare/save/read/delete和失效preview元数据恢复；Task/Run/预算未改。 | 仅PNG/JPEG human-only。首次无Task、draft消费、视频/文件、上传进度/取消/大文件续传、手机journal冷启动/完整权限未验；不参与模型回答。 |
| BT-NOW-20261008-012 照片地点建议与发布隐私 | modified / PARTIAL | BT-V5-AIR-030=PARTIAL；BT-V5-AIR-032=TODO；BT-V5-AGE-020=TODO；BT-V5-AGE-021=TODO；BT-V5-AIR-033=TODO；BT-V4-MOM-001=DONE；BT-V5-AGE-003=DONE；BT-V5-AGE-049=DONE<br>UI 025,041；DISC 006,007,010 | 像素重编码去EXIF、本地mask和Moment原受众/发布确认基础已有。 | 待EXIF地点/时间候选、多地/陈旧/未知冲突建议与统一图文draft；去EXIF不是地点建议，不能自动改GPS/探索/公开坐标或长期traits。 |
| BT-NOW-20261008-013 区分提及好友与发送给好友 | modified / PARTIAL | BT-V4-TIE-001=DONE；BT-V4-CHT-003=DONE；BT-V4-ACTN-001=DONE；BT-V5-AIR-040=DONE；BT-V5-AGE-042=PARTIAL<br>UI 026,032,041；DISC 007,018,020 | 原好友实体share有具体收件人/confirm/operation/撤权与结果恢复。 | UNCONNECTED：@ref与显式send-to-friend typed draft、文字媒体预览和变更重确认未接。@不发送，真实对外消息依旧守原授权。 |
| BT-NOW-20261008-014 来源改为逐段紧凑引用标签 | modified / PARTIAL | BT-AGT-002=DONE；BT-V4-NOW-004=DONE；BT-V5-AIR-024=PARTIAL；BT-V5-AIR-027=DONE<br>UI 015,017,030,034；DISC 008,011,014,018 | 每条assistant来源ID/title/URL/site/time、run/evidence归属与独立点击权限已有；当前长来源列表及正文source-N仍在。 | 待逐段紧凑标签、折叠来源/同轮去重、field关联/无来源诚实状态；不能压缩时改provenance/混轮或放宽授权。compact展示可由root指定独占agent实施。 |
| BT-NOW-20261008-015 保持来源事实一致与冲突可见 | modified / PARTIAL | BT-V5-AIR-024=PARTIAL；BT-V5-AIR-027=DONE；BT-V5-AIR-028=DONE；BT-V5-AIR-039=DONE；BT-V4-PLC-001=DONE；BT-V4-PLC-005=PARTIAL<br>UI 016,017,030,035；DISC 008,009,010,011,019 | 新immutable v2 prompt与闭集citation/权威断言拒绝接API16；真实交通追问保留周日冲突，未再称旅游网页官方，旧v1历史保留。 | 未完成全部字段事实校验/官方身份分类/field refs/hours-address一致性/原页详情读取；bounded措辞守卫不等于事实完全可靠。 |
| BT-NOW-20261008-016 消除地点卡重复动作并明确导航语义 | modified / PARTIAL | BT-V4-ACTN-001=DONE；BT-V4-NAV-001=TODO；BT-MAP-002=DONE；BT-V4-NOW-004=DONE；BT-V4-PLC-005=PARTIAL<br>UI 017,018,028,032,036；DISC 010,014,015,018 | 同ref cardbody详情、地图查看/Save/share已有；body和查看详情按钮重复。Navigate目前明确外部地图。 | 待去冗余详情动作、明确同图定位/真实route preview/share draft；外部导航不是Now路线。Save真实结果/无起点澄清待验。 |
| BT-NOW-20261008-017 收藏持久化和可感知反馈 | covered / PARTIAL | BT-SAV-001=DONE；BT-PLN-001=DONE；BT-V4-ACTN-001=DONE；BT-V4-PRV-001=DONE<br>UI 009,032,039,041；DISC 018,019 | 独立Save有真实GET/POST/DELETE、去重/ACL及busy/error反馈，旧DONE保留。 | 新验收PARTIAL：NL召回/登录续办/持久重启/删除地图恢复/Save UNKNOWN journal及当前真机实体Save未完成；附件save不是实体收藏。 |
| BT-NOW-20261008-018 收藏整理复用与 Now 地图展示 | new_delta / PARTIAL | BT-SAV-001=DONE；BT-PLN-001=DONE；BT-V5-AGE-027=PARTIAL；BT-V5-AGE-029=TODO；BT-V5-AGE-030=TODO<br>UI 041；DISC 013,016,018 | Saved canonical private refs与原Now地图可复用。 | UNCONNECTED：SavedPage仅列表/取消，无onTap/筛选/notes/list/compare/map；我的阿伯丁酒吧收藏、三站route/Theme/Journey复用待接。 |
| BT-NOW-20261008-019 同页分享预览和明确发送结果 | modified / PARTIAL | BT-V4-CHT-003=DONE；BT-V4-ACTN-001=DONE；BT-V4-TIE-001=DONE；BT-V4-E2E-001=PARTIAL；BT-V5-AIR-038=PARTIAL；BT-V5-AIR-040=DONE<br>UI 018,032,041；DISC 007,018,020 | 原实体share sheet收件人/confirm/pending op/result/UNKNOWN恢复可复用；公开地点无需先Save。 | 缺同Now完整share draft与文字/媒体/route/theme/journey payload、临时地图恢复；系统分享不能称好友已收到。 |
| BT-NOW-20261008-020 分享链接权限和生命周期 | new_delta / PARTIAL | BT-V4-CHT-003=DONE；BT-V4-ACTN-001=DONE；BT-V4-PRV-001=DONE；BT-V4-PRV-002=DONE<br>UI 005,041；DISC 007,018,020 | 内部Chat entity引用重读原ACL已有。 | UNCONNECTED：snapshot/live分享链接发行、audience/expiry/revoke/对象编辑删除语义未接；引用ACL/外部URL复制不是链接生命周期。 |
| BT-NOW-20261008-021 路线计划在原地图预览与编辑 | new_delta / PARTIAL | BT-V4-NAV-001=TODO；BT-V4-ACTN-001=DONE；BT-MAP-001=DONE；BT-MAP-002=DONE<br>UI 018,028；DISC 010,015,018 | selectedPlace/detail和外部地图打开路径存在。 | UNCONNECTED：Now真实route geometry、明确起点/mode/stop顺序/time/edit/source未接；第二家怎么去需ordinal实体绑定和缺起点询问，不能直线伪装route或以探索城市作GPS。 |
| BT-NOW-20261008-022 迁移动态创建的最小完整链路 | modified / PARTIAL | BT-V4-MOM-001=DONE；BT-V4-MOM-002=DONE；BT-V5-AGE-023=DONE；BT-V5-AGE-020=TODO；BT-V5-AGE-021=TODO；BT-V5-AGE-022=TODO；BT-V5-AIR-030=PARTIAL；BT-V5-AIR-033=TODO<br>UI 017,025,041；DISC 005,006,007,014,016 | 原Moment创建/修改/private media/发布撤回与地图MOMENT投影基础保留。 | 缺Now同输入文字+媒体→可编辑位置/受众→confirm publish→实际Agent发现/同map detail连续链；故事卡非现行完整ResultSet类型，Civu许可/备份先审。 |
| BT-NOW-20261008-023 迁移主题地图创建收藏和复用 | new_delta / PARTIAL | BT-SAV-001=DONE；BT-V5-AGE-029=TODO；BT-V5-AGE-030=TODO；BT-V4-MAP-001=DONE<br>UI 041；DISC 001,013,016,018 | 原Now地图/Place refs/Save为基础；不等待ThemeMap才补Agent。 | UNCONNECTED：ThemeMap标题/有序地点/可见性draft、持久save/reopen同Now/ref-save与许可copy/撤权/private notes未接。 |
| BT-NOW-20261008-024 迁移旅程与外部计划导入 | modified / PARTIAL | BT-AGT-003=DONE；BT-V5-AGE-029=TODO；BT-V5-AGE-030=TODO；BT-V5-AIR-021=TODO<br>UI 026,041；DISC 001,013,016,018,020 | 有界clipboard helper、Runtime/context/Place基础可复用；LifeMap/LifeImport原TODO保留。 | UNCONNECTED：外部text/AI计划解析→date/place/route可编辑Journey→逐日/他人临时Now地图恢复未接；导入不能执行命令/订票/发消息。 |
| BT-NOW-20261008-025 统一活动询问创建与加入 | modified / PARTIAL | BT-ACT-001=DONE；BT-RSV-001=DONE；BT-NTF-002=DONE；BT-AGT-001=DONE；BT-V4-ORG-001=DONE；BT-V4-ACTN-001=DONE；BT-V4-CHT-004=DONE；BT-V5-AGE-022=TODO<br>UI 016,017,032,041；DISC 005,009,014,018 | 原Activity query/detail/RSVP/容量/提醒/org创建权限路径保留；创建为独立ActivityEditorPage。 | 缺同Now query/create draft/joinconfirm分离与voice/menu，@不邀请/query不RSVP/org撤权/dupclick/fees依旧守原边界；外部场次核验未完成。 |
| BT-NOW-20261008-026 中文优先的完整业务验收 | modified / PARTIAL | BT-AGT-001=DONE；BT-AGT-002=DONE；BT-AGT-004=DONE；BT-V5-AIR-007=DONE；BT-V5-AIR-009=IN_PROGRESS；BT-V5-AIR-024=PARTIAL；BT-V5-AIR-025=TODO；BT-V5-AIR-026=TODO；BT-V5-AIR-039=DONE；BT-TST-002=BLOCKED<br>UI 015,016,023,028,035,038,039,040；DISC 003,004,005,008,009,010,011,017,021,022 | 中英文parser/raw query+公开slots、唯一PUBLIC_PLACE pronoun/显式换地点及限定真实四轮已有；Gallery/Gallery/Museum/交通按membership恢复。 | 完整中文酒吧发现/第二家/安静点/只站内、多源故事/未来活动、中文Save/route/friend动作未验；英语有界链不能冒充完整中文/混合闭环。 |
| BT-NOW-20261008-027 复用 Civu 能力前完成契约与数据保护审查 | covered / PARTIAL | BT-AUD-001=DONE；BT-V4-AUD-001=DONE；BT-V5-AIR-005=TODO；BT-V4-SAF-004=DONE；BT-V4-PLC-001=DONE；BT-V4-MOM-001=DONE；BT-V5-AGE-029=TODO；BT-V5-AGE-030=TODO<br>UI 001,041；DISC 001,006,010,018 | CIVU-REUSE-MATRIX/cutover边界已有：只读参考/原Birdtie模型、样式与受限key曾明确授权、纯解析候选，业务顶层许可证未确认。 | 缺每个精确模块license/依赖/ownership/API适配以及backup/isolated restore/count/ref验收；无新生产迁移授权，矩阵存在不等于保护门通过。 |
| BT-NOW-20261008-028 统一权限确认幂等与未知结果恢复 | covered / PARTIAL | BT-V4-ACTN-001=DONE；BT-V4-SAF-004=DONE；BT-V4-PRV-001=DONE；BT-V4-PRV-002=DONE；BT-V5-AGE-003=DONE；BT-V5-AGE-041=DONE；BT-V5-AGE-042=PARTIAL；BT-V5-AGE-049=DONE；BT-V5-AGE-062=DONE；BT-V5-AIR-011=DONE；BT-V5-AIR-018=DONE；BT-V5-AIR-023=DONE；BT-V5-AIR-037=DONE；BT-V5-AIR-038=PARTIAL；BT-V5-AIR-040=DONE<br>UI 003,004,005,006,009,025,027,038；DISC 002,007,012,018,019,020,021 | 原actor/owner/ACL/purpose/版本确认/native current/once发送/UNKNOWN全额hold/op结果读具基础；失效imagepreview元数据恢复不续上传授权。 | 新draft/voice/media/route/theme/link每条写路还须绑定最终身份/对象/版本/受众与负例；AIR038 PARTIAL不解除，隐藏按钮不替代server guard。 |
| BT-NOW-20261008-029 面板键盘可访问性和性能回归 | covered / PARTIAL | BT-MAP-001=DONE；BT-PER-001=DONE；BT-PER-002=DONE；BT-V4-MAP-002=PARTIAL；BT-POL-001=TODO；BT-TST-001=DONE；BT-TST-002=BLOCKED<br>UI 002,007,010,019,020,021,022,023,027,028,033,034,038,042；DISC 012,013,015,019,021,022 | 连续消息/IME一次避让/focus展开/composing/quietGET/到期不移图、48dp/部分大字/reducedMotion已有历史覆盖。 | 新cleanhome/compactsources/plus后需相关回归；全orientation/读屏/platformIME/weaknetwork/真实profile帧基线未验，debug不等于生产性能。 |
| BT-NOW-20261008-030 分批验收和真实证据交付 | covered / PARTIAL | BT-FIX-NOW-UI-001=PARTIAL；BT-TST-001=DONE；BT-TST-002=BLOCKED；BT-V4-E2E-001=PARTIAL；BT-V4-E2E-002=PARTIAL；BT-V4-PIL-001=BLOCKED；BT-V4-PIL-002=BLOCKED；BT-V4-PIL-003=BLOCKED；BT-REL-001=BLOCKED；BT-V5-AIR-009-LIVE=BLOCKED；BT-V5-AIR-054-LIVE=BLOCKED<br>UI 001,031,035,038,039,040,041,042；DISC 001,019,020,021,022 | 按source/build/mode/rawmedia/限界记录的原证据保留；API16/APK14地点和human-only图片有有界真实证据，旧失败/NOT_RUN保留。 | 全30验收未满足；G1–G15/中文连续真实与fail恢复/旧org-chat-activity-reminder-assets回归/IdP-deploy-pilot门继续。新包不增外部授权。 |

## DISC22 保留要求

原 [DISC文件](../../../BirdTie-Now-Agent-Discovery-P0-2026-10-07.md) §12候选不是live任务。保留原工具/故事/多轮/放行缺口，不用新菜单或私人图片替代。原队列反向映射见JSON。

| DISC | 原范围 | NOW关联 | 未完成边界 |
|---|---|---|
| BT-NOW-DISC-001 | 当前状态、故障复现与 Civu 复用核验 | 001,004,023,024,027,030 | 覆盖与真实缺口见关联NOW；旧DONE或有限样本不能推出全项完成。 |
| BT-NOW-DISC-002 | 统一 ResultSet、来源、选择和能力契约 | 007,008,010,028 | 覆盖与真实缺口见关联NOW；旧DONE或有限样本不能推出全项完成。 |
| BT-NOW-DISC-003 | 真实模型与工具循环 | 008,010,026 | 真实受控source→model两步不等于通用模型自主多工具循环；原DISC工具闭环保持未完成。 |
| BT-NOW-DISC-004 | 自然语言与短期上下文 | 002,003,004,026 | 有界唯一Place公开问题/显式换地点及slots继承不等于任意三轮语义约束。 |
| BT-NOW-DISC-005 | 站内混合内容检索 | 006,022,025,026 | 原Place/Activity/Person等搜索与typed地图可复用；Moment/story混合Agent检索未完整接。 |
| BT-NOW-DISC-006 | 最小媒体/故事发布到地图 | 011,012,022,027 | Moment领域/地图投影不等于真实publish→有权他人Agent发现→revoke设备闭环。 |
| BT-NOW-DISC-007 | 站内 ACL 与撤权 | 011,012,013,019,020,022,028 | 原native ACL/代际可复用，故事媒体计数/缓存/引用全链不能由私人图片闭环代替。 |
| BT-NOW-DISC-008 | 联网搜索与详情读取 | 014,015,026 | 真实WSA query-only已有；通用read_external_source/主办正文详情未接，摘要不能称已读原页。 |
| BT-NOW-DISC-009 | 外部场次、时区和新鲜度 | 015,025,026 | 真实外部具体场次/时区/售罄取消/新鲜度及主办原页核验仍缺。 |
| BT-NOW-DISC-010 | Place 消歧与坐标来源 | 003,004,012,015,016,021,026,027 | 已批准唯一公开Place/CITY/location refs绑定存在；泛同名消歧/外部坐标解析未完成。 |
| BT-NOW-DISC-011 | 融合排序和逐项来源 | 014,015,026 | 来源ID/URL和模型解释具基础，融合排序/逐项字段事实支撑仍PARTIAL。 |
| BT-NOW-DISC-012 | 流式协议与版本隔离 | 007,010,028,029 | run/fence/current/late response存在，目前不是SSE流式已验证实体增量协议。 |
| BT-NOW-DISC-013 | Now 共享结果与视图生命周期 | 005,007,018,023,024,029 | 覆盖与真实缺口见关联NOW；旧DONE或有限样本不能推出全项完成。 |
| BT-NOW-DISC-014 | 对话内实体卡与出处 | 008,014,016,022,025 | 覆盖与真实缺口见关联NOW；旧DONE或有限样本不能推出全项完成。 |
| BT-NOW-DISC-015 | 地图双向选择与真机稳定 | 007,016,021,029 | 覆盖与真实缺口见关联NOW；旧DONE或有限样本不能推出全项完成。 |
| BT-NOW-DISC-016 | 地图故事层、范围和聚合 | 003,005,006,007,018,022,023,024 | typed MOMENT/PLACE投影存在；AreaPulse/search-area仍Activity，故事聚合/混合范围验收不足。 |
| BT-NOW-DISC-017 | 连续追问与局部更新 | 026 | 完整第二家/安静点/只站内/局部约束未接，boundedPlace追问不能代表DISC017。 |
| BT-NOW-DISC-018 | 详情、原来源和私人收藏 | 013,014,016,017,018,019,020,021,023,024,025,027,028 | 旧详情/share/Save基础存在，当前同构建实际Save/reopen及全域权限未验收。 |
| BT-NOW-DISC-019 | 故障隔离与诚实降级 | 002,003,005,006,015,017,028,029,030 | error/empty/UNKNOWN分离已有，单路失败保留成功结果完整矩阵未在本新包跑。 |
| BT-NOW-DISC-020 | 运行边界、成本与可观测性 | 001,009,010,011,013,019,020,024,028,030 | 原预算/once/timeout/cancel/UNKNOWN/脱敏stage保持，本次无paidcall或扩权限。 |
| BT-NOW-DISC-021 | 自动化契约与场景测试 | 001,009,026,028,029,030 | 历史unit/SQL/真实与mock分别保留；本次对账没有运行DISC全场景。 |
| BT-NOW-DISC-022 | 真实环境与真机放行 | 001,026,029,030 | API16/APK14有界证据不满足DISC§14完整连续链/CSSA原能力回归和pilot放行。 |

## 42 UI反向索引

只列关联，不改原候选证据或领域任务。原Inbox/Plans等能力通过NOW030保留回归，未要求删除。

| UI | 原标题 | NOW关联 |
|---|---|---|
| BT-UXR-001 | 锁定当前构建与现有任务 | 001,027,030 |
| BT-UXR-002 | 确定壳层与唯一状态归属 | 007,010,029 |
| BT-UXR-003 | 统一游客/已登录/恢复中状态 | 028 |
| BT-UXR-004 | 按 capability 生成导航和动作 | 028 |
| BT-UXR-005 | 服务端与深链越权反例 | 020,028 |
| BT-UXR-006 | 有权管理者显式切换工作区 | 028 |
| BT-UXR-007 | 底部固定新对话与账户入口 | 008,029 |
| BT-UXR-008 | 收敛导航并释放历史空间 | 005 |
| BT-UXR-009 | 就地登录并回到原任务 | 017,028 |
| BT-UXR-010 | 实现三槽对称顶栏 | 004,029 |
| BT-UXR-011 | 合并城市选择唯一入口 | 002,003,004 |
| BT-UXR-012 | 城市选择器自适应内容 | 002,004 |
| BT-UXR-013 | 分开任务目的地和地图视口 | 002,003,007 |
| BT-UXR-014 | 替换顶部标签与更多工具白页 | 005,006 |
| BT-UXR-015 | 移除结果/对话模式分叉 | 010,014,026 |
| BT-UXR-016 | 把状态句替换成真实回答链 | 015,025,026 |
| BT-UXR-017 | 统一地点/活动/故事卡 | 006,014,015,016,022,025 |
| BT-UXR-018 | 复用一套实体详情与选择预览 | 007,016,019,021 |
| BT-UXR-019 | 把手独立居中与有限停靠状态 | 029 |
| BT-UXR-020 | 输入聚焦时会话使用可用高度 | 029 |
| BT-UXR-021 | IME/safe area 只避让一次 | 029 |
| BT-UXR-022 | 统一面板与消息滚动归属 | 029 |
| BT-UXR-023 | 原生编辑与中文 IME 正确提交 | 026,029 |
| BT-UXR-024 | 清理加号菜单语义 | 008,009,010,011 |
| BT-UXR-025 | 附件真实接到当前会话 | 008,011,012,022,028 |
| BT-UXR-026 | 粘贴后按内容给轻量后续动作 | 008,009,010,013,024 |
| BT-UXR-027 | 统一叠层和返回优先级 | 007,011,028,029 |
| BT-UXR-028 | 共享结果、相机和双向选择 | 003,006,007,016,021,026,029 |
| BT-UXR-029 | 会话历史恢复与新建语义 | 007 |
| BT-UXR-030 | 检索/来源/错误统一呈现 | 014,015 |
| BT-UXR-031 | 替换 Inbox/我的活动大空白 | 030 |
| BT-UXR-032 | 重排活动详情决策信息 | 013,016,017,019,025 |
| BT-UXR-033 | 统一字体/间距/按钮/圆角 | 029 |
| BT-UXR-034 | 长标题与文字缩放/读屏 | 014,029 |
| BT-UXR-035 | 隔离本地测试与真实服务验收 | 001,002,015,026,030 |
| BT-UXR-036 | 删除空字段孤立图标与旧组件调用 | 005,006,016 |
| BT-UXR-037 | 改用用户理解的上下文语言 | 003,004,005 |
| BT-UXR-038 | 执行键盘/屏幕/权限状态回归 | 001,026,028,029,030 |
| BT-UXR-039 | 同构建真实发现与原能力回归 | 001,017,026,030 |
| BT-UXR-040 | 交付同路径前后录屏与逐项证据 | 001,026,030 |
| BT-UXR-041 | 保留个人资产与 Civu 复用关系 | 004,007,010,011,012,013,017,018,019,020,022,023,024,025,027,030 |
| BT-UXR-042 | 减少无意义动效与布局重建 | 007,029,030 |

## 状态、所有权和门

- 唯一live状态仍为 `automation/codex_task_queue.json`。AGE文件READY仅说明接收/基础，不激活Memory/model，不解除LIVE；137来源映射只是别名。
- `BT-V5-AIR-009=IN_PROGRESS`；原来源/成本/写工具PARTIAL、LifeMap/LifeImport和AIR031/032/033等TODO、所有旧DONE/history均保持。
- 原Auth/IdP/Closed Pilot/部署/生产地图与供给、通用Live评测BLOCKED/NO保持，不称Consumer Beta或pilot放行。
- root是唯一queue/共用报告写入者。首切片拥有map_workspace、探索存储/defaultAberdeen/cleanhome；来源compact若并行须独占agent_conversation并先对scope。本worker只这两个文件。
- 新包没有新增key/收费额/好友发送/公共发布/生产迁移/旧server停用/deploy/push授权。既有有效授权按原范围；外部依赖不足时记录并继续独立代码。
- Civu矩阵已读；精确license/ownership/依赖与backup+isolated restore/count/ref未验收，不复制数据/业务代码/旧页面。

## 本次动作和接续

本次仅只读文件/Git状态与文档数据核对。应用单元、SQL/迁移、构建、设备、provider均 **NOT_RUN**；供应商/设备/SQL/queue/code/Git写操作0。第一次文档组装因UI031反向索引遗漏断言失败，尚未写文件；此次补原能力回归关联，失败不当应用PASS。

四个原queue/state字节在读取起止和publication05精确匹配：

- `automation/codex_task_queue.json`：`51a483dd3a6a890aab4edc2f89096d33c837a0cab94acae6e4f020597e6e3ad4`。
- `automation/bt_v5_age_status.json`：`74de1fdab27fb13559492a0ecb4d4d1ccec2b0b21923b4a0bf35a6bb2b4fac8b`。
- `automation/v5_requirement_mapping.json`：`984711f7556f370cad9abceb6f798f14dd8d62d3828e225d6a522d2a6f7abf11`。
- `automation/snapshots/pre_v4_2026-10-01.json`：`6aa908792736c4168e585df7b92b2db470b515055e6b148f2170fc0e813e8cc3`。

接续root先默认探索Aberdeen/恢复与cleanhome/唯一city目的地，再compact来源/动作去重，原Runtime typed draft、材料/voice，Save→map→route/share，最后按依赖接Moment/Theme/Journey/Activity。中文主要、英混对照；G1–G15与DISC§13/14保留。只跑直接相关单元后在新同构建验收，未运行继续NOT_RUN，不等待完整画像/Trip/ThemeMap补基础Agent。


## 后续实际检查点 · 2026-10-09 API26/APK18

原只读记录保留；[实际代码与真实运行证据](../../testing/evidence/now-place-growth-2026-10-09/increment-26/REPORT.md)、[逐项差异与原任务映射](../../testing/evidence/now-place-growth-2026-10-09/increment-26/TASK-MAPPING.md)。原ID/历史不变，整包PARTIAL。


## 后续实际检查点 · 2026-10-09 API27/APK20

原记录保留；[实际代码和同构建运行证据](../../testing/evidence/now-place-growth-2026-10-09/increment-27/REPORT.md)、[逐项差异与原任务映射](../../testing/evidence/now-place-growth-2026-10-09/increment-27/TASK-MAPPING.md)。真实模型三轮及中文同Task地址通过；整包仍PARTIAL。
