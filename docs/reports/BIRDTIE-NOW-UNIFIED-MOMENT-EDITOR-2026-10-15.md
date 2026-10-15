# Birdtie Now 统一动态编辑器交付记录

## 最终结果：本次请求范围已交付

2026-10-16：统一条目编辑、原 ID 接续、媒体与位置、失败恢复以及必要权限验证已完成；API169、Web19 已部署到当前远端，APK19 已安装并实际操作。源码与交付构建帧一致。此次完成状态仅指本次统一编辑器和已授权 Civu 数据适配，不改写更广的全站父任务或历史完成状态。

普通动态和串文使用同一条目模型、同一 Now 面板和原输入框，默认一条，只在点击“再接一条”时增加。原输入文字是正文；标题、城市、地点、旅程都可不填。每条位置独立选择，纯图条目没有继承 Now 的 Aberdeen 城市，另外两条显式选择伦敦和雷克雅未克。单条删除和整串撤回有不同预览与明确确认。

真机发现并修复两项实际缺陷：停止失败上传后旧 operation 引用阻塞整体检查；已发布视频的暂存过期后无法继续编辑。后者由新增169迁移处理，仅沿用同 Moment 当前合法公开副本，原权利与保留期限不放宽，所有1–168共311个历史SQL文件保持不变。最终手机编辑/排序、单删、整串撤回均成功，远端读取逐步核对版本4、5、6与原内容/媒体ID。

E = `docs/testing/evidence/civu-native-adaptation-20261015/unified-editor168/`（历史目录名保留，当前API为169）。源码审计不是Civu运行验收；不同源码时点的测试保持分批结果，不拼作一次全仓回归。

## 交付版本

| 项目 | 实际值 |
| --- | --- |
| 远端 | `https://47.110.154.133`；独立库 `birdtie_native_hz_20261015` / schema169 |
| API | `social169-20261015-build19`；SHA256 `1fdff07d26998dc6cf8cb5f5843dc1a1f9c9b910fe7a5e6a57b3d39f7684876d` |
| Web | `public-20261015-final-web19`；manifest SHA256 `75681093f618f62360d9b85f13167eea03c1e3fcaaf7d1e6c5ad73367e6d93ef` |
| APK | `89bfeaf52fdbd754468e8009ff22708558ed49ba901a8449faf99519f48f8f3a`，安装回执 `device-install17`；后续实际验收在 E `device-final19` |
| 认证 | 沿用固定验证码123456；最终手机使用合成QA03，未冒用其他旧owner |
| 最终机器回执 | E `device-final19/receipt.json`、`qa03-readbacks19-02/index.json`、`startup169-01`、`unified-video-carry-native04` |

## 最终真机与服务器核对

| 用户要求 | 实际验证与结果 |
| --- | --- |
| 单条纯文、无标题无地点 | APK17 真机发布成功；后续原ID延续。未声称在APK19重新创建一次。 |
| 单条纯媒体、无标题/正文/地点 | APK17真实图片像素；API169 readback03再次核对空标题/正文/位置，原图mediaID保持。 |
| 多条混合、发布后追加 | APK19明确发布纯图、正文、视频共三条；没有把图片或正文自动拆条。 |
| 不同条目不同城市 | 伦敦 Apsley House (51.503474,-0.151636)；雷克雅未克 Listasafn (64.149065,-21.940444)，WGS84；原图无地点。Now仍Aberdeen。外部REFERENCE的空cityId合法，不猜测成系统City。 |
| 逐条编辑、排序、原ID/互动 | APK19/API169改正文并重排，readback03版本4、原3个ID与2个媒体ID保持。互动不丢由native/实际HTTPS专项核对，手机此批未新造互动。 |
| 单条删除 | 手机只移除原图；readback04版本5，另两条仍公开；原图公开入口404。 |
| 整串撤回 | 手机再次独立预览/确认撤回全部；readback05版本6、原3条均withdrawn/private，详情/串文/地点/媒体及5旧媒体URL均404。 |
| 草稿恢复与失败重试 | APK18/19恢复原有顺序/正文/材料；真实断网视频UNKNOWN，核实GET404、明确STOP后重准备，再明确确认一次成功。原未知操作没有自动新建。 |
| Now内视频 | 真实四秒自制视频在原面板播放；`apk19-now-video-playing04.png`可见暂停及变化帧。并非只显示海报。 |
| 返回Now状态 | 收起前后地图背景相同、仍Aberdeen；重开同编辑器仍是原正文和两条。安全UI306个事件及前后截图；未用内部摄像机数值宣称逐像素一致。 |
| 权限和地图 | 原生地图私有载荷/撤回拒绝专项、媒体pin失效组件均PASS；最终线上匿名撤回入口全404。全套地图/分享/列表传播真机矩阵NOT_RUN。 |

证据截图只包含自制媒体和合成账号，交付在输出目录，Git内仅保存文字回执、摘要和哈希。全程未录屏。4个自制图库文件在逐一核对hash后删除，没有触及其他图库内容。最终留在Now，手机合成QA03会话仍登录；各HTTP辅助读取会话均注销，不能把后者描述成手机也已退出。

169必要复合测试 `TestPublicMomentVideoCarryNativeExpiredPurgedAndBoundaries`：1 PASS/0 FAIL/0 SKIP，真实转码、stage过期及清理后保留原视频继续编辑/排序；跨owner/跨Moment/字节与rights篡改/历史退休重用拒绝。schema168复现失败、169第一版SQL别名歧义失败及修正后的PASS都保留。最终启动合同测试1PASS，API及8导入工具共9二进制构建成功。未重跑无关的全仓套件。

## 未执行与范围边界

- NOT_RUN: full-repository Go/Flutter suites and full-site acceptance; only necessary affected tests were executed.
- NOT_RUN: iOS, other devices, global search and China AMap external-search acceptance.
- NOT_RUN: all legacy owners and all 407 assets individually in UI; selected old owner has no positive Journey/Moment cover samples.
- NOT_RUN: AV scanning, schema169 down execution, physical deletion of withdrawn media bytes.
- NOT_RUN_ON_APK19: fresh single-text/single-image creation was exercised on APK17; single-entry multiple locations and multiple images on prior APK08/10. Final19 covers continued existing IDs, mixed media, recovery and editing.
- NOT_RUN: complete live map/feed/profile/share propagation matrix on final phone; targeted map-native/privacy and map-media component tests passed. Anonymous detail/thread/location/media retirement was verified on live API169.
- NOT_RUN: Civu runtime/build/tests; Civu comparison is read-only source evidence.

正常字号依用户要求执行；大字号专项已取消，不列为待办。中英混合语音仍按原要求延期。缺少样本的旧owner旅程/动态封面保留NOT_RUN，不使用其他账号补造验收；其他存续用途图片的迁入、权限和所选owner图片实显沿167既有真实证据。

## 主要改动文件

客户端：`moment_thread_api.dart`、`moment_thread_controller.dart`、`moment_thread_panel.dart`、`moment_thread_pending_store.dart`、`now_creation_draft_state.dart`、`map_workspace.dart`、`agent_composer.dart`；新 `now_moment_editor_host.dart`；既有 `birdtie_app.dart`、`legacy_shell.dart`、`place_detail_sheet.dart`、`public_moment_page.dart` 和 `public_moment_photo_editor.dart` 的入口收口；地图模型/读取与本人DTO适配，相关直接测试一并保留。

后端：`internal/content/moment_thread.go`、`internal/httpapi/moment_thread.go`、`internal/postgres/moment_thread.go`、`moment_control.go`；168统一编辑迁移、169视频沿用迁移及对应native测试；`native_database_startup.go` 与测试、部署CLI识别169。既有167私人用途图片适配、媒体转码修复与客户端读取改动随本次共同提交，无回滚他人修改。

完整改动清单以交付输出的 `Birdtie-改动文件-2026-10-16.txt` 和最终Git提交为准；本报告中的路径均相对官方仓库。提交/推送之后的精确SHA和远端核对另存输出回执，避免在提交自身内容中伪写尚未产生的SHA。

## 1. Civu 本地与 GitHub 来源

审计依据为 `docs/testing/evidence/civu-native-adaptation-20261015/civu-unified-editor-source-audit01.md` 及仓库已归档同名副本。那次只读检查的 `D:/Program/Civu/Civu-client` 分支为 master，本地 HEAD 为 `1bbf0613b513001447f326225c944c1a1fbd8e8f`，提交时间 `2026-09-29T02:37:18+08:00`，tracked 修改和 untracked 都为 0。`git ls-remote` 得到 GitHub HEAD/master 同一 SHA；GitHub compare 返回 identical、ahead=0、behind=0、total_commits=0。因此所引用的编辑能力确实在当时 GitHub master 上，不是仅存在于未提交的本地改动。

本轮已再次查询远端，`civu-source-recheck02.json` 确认当前本地和 GitHub HEAD/master 相同且两文件未变；此事实不证明旧服务或旧已安装客户端运行了此版本，也不证明其它分支均已合并。本轮未改 Civu 文件、未复制旧 Session、未写原库。

| 来源文件 | 固定版本/摘要 |
| --- | --- |
| `lib/src/screens/publish_moment_screen.dart` | [固定 GitHub 源码](https://github.com/luchen-luke/Civu-client/blob/1bbf0613b513001447f326225c944c1a1fbd8e8f/lib/src/screens/publish_moment_screen.dart)；SHA256 `5d0e4beb98c635aaa6c68b59ed627318bbb5dccfd9c792fc40982ff53261621b` |
| `lib/src/models/moment_publish_draft.dart` | [固定 GitHub 源码](https://github.com/luchen-luke/Civu-client/blob/1bbf0613b513001447f326225c944c1a1fbd8e8f/lib/src/models/moment_publish_draft.dart)；SHA256 `bb9e76ce5bef1614a8fe6e193cdd4e5c6b7017f5d1fe726aea5eb00b8c808773` |

## 2. Civu 方法与 Birdtie 文件对应

下表 Civu client 路径相对 `D:/Program/Civu/Civu-client`，server 路径相对 `D:/Program/Civu/Civu-server`。行号取自来源审计，只用于定位。这里的“复用”是状态组织和交互理念的适配，**不是逐文件复制 Civu 整页、路由、全局 store、身份或业务 API**。

| Civu 真实来源/方法 | Birdtie 承接文件与实现 | 性质与边界 |
| --- | --- | --- |
| `publish_moment_screen.dart:448,491,562,1016`：一个 screen、`_entries`、`_MomentComposerEntry`，默认一条、活动条目代理 | `apps/client/lib/src/workspace/moment_thread_api.dart` 的 `MomentThreadDraftEntry`；`moment_thread_controller.dart`；`moment_thread_panel.dart`；`map_workspace.dart` 的 `_beginMomentThread`；`agent_composer.dart` | 复用条目模型与共享输入理念。Birdtie 默认 1 条，显式“再接一条”增加，1 条按普通动态展示；多图/多段正文不自动拆条。Now 不另开第二套发布表单。 |
| 同文件 `:840,859,873,1842,2019`：`_addEntryAfter`、`_removeEntry`、`_reorderEntries`、`ObjectKey(entry)` | `moment_thread_controller.dart` 的 `addEntry/removeEntry/moveEntry`，稳定 localID 和 per-entry media controller；`moment_thread_panel.dart` | 复用稳定条目身份。Birdtie 文字、可选标题、材料和 0..N 地点跟随同一 entry 移动，不靠当前数组下标归属。 |
| `civu_discover_media_post.dart:445`、`content_detail_screen.dart:610`、`app_router.dart:219`：`edit=<原ID>`、`initialThreadItems`；编辑器 `:625,658,1041` 原 noteId 回填 | `now_moment_editor_host.dart`、`birdtie_app.dart`；`public_moment_page.dart`、`legacy_shell.dart`、`place_detail_sheet.dart`；Now `_openOwnedMomentInNow` | 适配已有作品进入同一 editor。原 owner/token/origin/workspace/source 绑定后回原 Now route，保留原地图与面板；当前本人 reader 读取原 Moment ID/revision，不用 extras/cache 作权限。 |
| `moment_publish_draft.dart:4,65,152`：`entries/effectiveEntries`；编辑器 `:1090,1234,1243` 与 draft store | `moment_thread_pending_store.dart`、`now_creation_draft_state.dart`、`moment_thread_controller.dart.flushDraft` | 复用单条旧稿升级为一条 entry 的思路。Birdtie 按 origin/owner 保存稳定 ID、顺序、正文、可选标题、位置与材料选择器；未知提交 journal 与可编辑草稿分离，不持久化批准、权利授权或 raw 媒体。 |
| repository `domain_repositories.dart:4868,4930`：`_existingRemoteMedia`、新图处理/视频准备/上传 | `public_moment_media_controller.dart`、`apps/client/lib/src/content/video_material_controller.dart`、`moment_thread_api.dart.toEditItem`；API `moment_media.go`、`moment_media_video.go`；PG `moment_media_projection.go` | 适配媒体语义，不复用旧上传许可。`media:null` 明确保留当前合法公开集合；`[]` 明确清空；新素材必须本轮 READY stage/current rights。旧私人导入素材不因旧 URL 或关联自动公开。 |
| 编辑器 `:3630` → repository `:1553,1660,1826` → API `:428`：一次 POST note-threads | `apps/api/internal/content/moment_thread.go`、`internal/httpapi/moment_thread.go`、`internal/postgres/moment_thread.go`；新 `migrations/168_moment_thread_editing.sql` | Birdtie 自有 `EDIT` 闭合同，同时处理新单条、新多条、原单条升级、原串文编辑。预览之后一次 Tx 提交整组，原 3 action 摘要/收据保持兼容。 |
| 编辑器 `:3545` → repository `:1962,2114` → API `:446`：逐条 PATCH 原 notes，再单独更新 legs | 同上 PG `captureMomentThreadEdit/commitEditedThreadInTx` 与 168 display/retired 模型 | **新增实现，不能称旧 Civu 已有。** Birdtie 已发布后可追加、删单条、排序；原 Moment ID、历史 member.position 和根身份不变，当前 display order 独立；整串原子失败回滚。 |
| Civu server `notes_handlers.go:310,334`、`notes_postgres.go:291,342,1087`：当前作者、素材 owner 与 If-Match | `moment_thread.go` preview/current source/CAS/final gate；`moment_control.go` EDIT-only 原关联 gate；媒体/地点各自 current reader | 复用“原作者才能写、素材归属再验证”原则。Birdtie 再绑定当前 session、精确整组版本、media/locations/current roles；先核原关联完全一致，再允许其与可选城市解耦。原其它调用不放松。 |
| Civu time-based Idempotency-Key 与失败重试文案 | `moment_thread_pending_store.dart`、controller `confirm/recover`；API/PG 原 operation GET/STOP；不可变 receipt | Birdtie 新恢复链：同 operation+digest 重试返回原事实；UNKNOWN 只 GET 原 operation/显式 STOP，不换 ID 自动重复创建。 |
| Civu 每条 places、地图元数据/legs 补读 | `moment_locations.go`、`map_projections.go`；客户端 `map_entities.dart/map_layers_api.dart/map_media_controller.dart` | 保留每条 0..多地点跨城市；地图可选、城市可选。真实位置源及精度决定 pin，公开关联不赋予私有正文权限，不自动生成 Journey/路线。v5/v6 复合 pin 媒体绑定另有专项。 |

Civu 没有可直接继承的部分：其已发布编辑 UI 禁止增删排序；更新是逐条请求而非整串原子；editing 模式不保存草稿；旧发布强制整串至少一个媒体、最多 18 个媒体；旧媒体 copier 也不保证所有 capture/assignment/inbound 字段逐字段持久化。Birdtie 没有照搬这些限制或缺口。全量旧元数据编辑、完整路线编辑和自动 Journey 生成不在本次实现范围；自动路线本身也不符合用户当前要求。

## 3. 当前真正调用的发布、更新、媒体与读取接口

| 用途 | 当前 Birdtie 路由 | 合同要点 |
| --- | --- | --- |
| 新建/编辑/追加/排序/单项删除预览 | `POST /v1/me/moment-threads/previews`，`action=EDIT` | items 1..20 为显示顺序，removedItems 为明确删除项。全新 threadId/baseMomentId 空；原单条由 baseMomentId 指定；原串由 threadId+expectedVersion 指定。每条旧 ID+expectedRevision，原关联显式原值。空白新条目拒绝。 |
| 明确确认后的原子提交 | `PUT /v1/me/moment-threads/operations/{operationID}` | 精确 digest/sourceVersion/approval；CAS 与最终 current gate；成功 EDIT receipt v2。原 ID 的链接、点赞、评论关联保留。 |
| 整串撤回 | 同一 preview/PUT，`action=WITHDRAW` | 与 EDIT 中单条 removedItems 区分。删根不重造 root；退休根公开 lookup 不能发现兄弟正文。 |
| 不确定结果核实/停止 | `GET /v1/me/moment-threads/operations/{operationID}`；`POST .../{operationID}/stop` | 原事实只读/显式停止。相同原操作不产生重复条目；旧不可变收据不按当前内容重建。 |
| 本人当前作品/整组 | `GET /v1/me/moments/{momentID}`；`GET /v1/me/moments/{momentID}/thread`；`GET /v1/me/moment-threads/{threadID}` | 原 owner 当前读取；v2 含 active members 和 retiredMembers。已 withdrawn 项不因打开编辑器自动恢复，只能显式恢复。独立撤回后的不可用槽可被后续 EDIT 退休，但不改其原 revision/status。 |
| 公开详情/整组 | `GET /v1/moments/{momentID}`；`GET /v1/moments/{momentID}/thread`；`GET /v1/moment-threads/{threadID}` | 当前公开许可逐成员重核。单个活跃成员按普通动态；旧根私有/退休则 root 隐去，不能借旧根穿透。 |
| 图像材料准备/恢复 | `PUT /v1/me/moment-media-materials/operations/{operationID}`；对应 GET 和 POST `/stop` | 材料准备不是公开发布。保留原 operation、scope、rights 与 deadline；失败/缺失占位不静默删除。 |
| 视频准备 | `PUT /v1/me/moment-media-video-materials/operations/{operationID}`；`GET /v1/me/moment-media/video-capabilities` | 实际后端转码与图片海报；准备完成后仍须 EDIT 预览/确认。UNKNOWN 原材料 GET/STOP 使用既有 operation family。 |
| 本人/公开媒体与字节 | `GET /v1/me/moments/{momentID}/public-media`；`GET /v1/moments/{momentID}/public-media`；`GET /v1/moments/{momentID}/public-media/{momentRevision}/{setVersion}/{mediaID}/{mediaVersion}/{variant}` | exact revision/set/media 及当前 rights/retention/source；撤回或替换后的旧 URL 不能读取退役字节。单条媒体集合原独立操作 API 仍存在，统一 editor 中新提交以 EDIT 事务关联。 |
| 地点 sidecar / 历史 / 互动 | `GET /v1/[me/]moments/{momentID}/locations`；`GET /v1/[me/]moments/history`；`GET /v1/moments/{momentID}/interactions` | 原位置 0..50，跨城市选择与正文权限分别核验；互动依原 Moment ID/current public binding。地图片段不携带不必要正文或私有身份字段。 |

旧 `/v1/me/moments/{id}/publication` 等兼容服务端路由没有删除，但当前生产 UI 不再通过地点/个人详情启动第二套旧发布编辑器。仅旧图片操作恢复页保留，且生产调用 `recoveryOnly=true`：GET/STOP/状态可用，新选材/SAVE/DELETE 不可用。旧 CREATE 原操作核实入口也保留。完整穷举及证据为 `E now-moment-final-entry-audit01.md`。

## 4. Civu 数据适配与编辑器复用应分开描述

旧数据迁入是此前用户授权的独立库适配，不是导入源码就自动完成。聚合实际结果引用 `docs/reports/BIRDTIE-CIVU-NATIVE-ADAPTATION-2026-10-15.md` 的最新 schema167 节和其所列 apply/readback 回执，本次统一编辑器批次没有重跑导入。

| 数据链 | Birdtie 文件/迁移承接 | 已有事实与边界 |
| --- | --- | --- |
| 授权备份、原主人映射、普通内容/收藏 | `scripts/extract_civu_hangzhou.py`；`apps/api/cmd/legacy-import`、`legacy-native-import`、`legacy-personal-import`；`internal/legacynative/build.go`；146/149/154/156 | 20 owner 一一映射；83私人动态、22旅程/110项、3主题图、189收藏；原身份固定码接续，未复制旧 Session。原私人内容没有自动公开，已删除内容保留墓碑。 |
| 存续动态图片/视频 | `legacy-media-import`、`legacy-video-import`；`scripts/extract_civu_recovered_media.py`；157/159/161 | 171 条存续 Note 媒体关系由166图片+5视频承接。图片派生及视频副本/海报/归属核对有证据；不能把聚合字节核对说成所有主人逐条 UI 验收。 |
| 多地点、媒体上下文、旧串文 | `legacy-context-import`、`internal/legacycontext/build.go`；160/162/166 | 162 已提交且 deferred 空，原22私人串文/27成员及5条路线有保全/适配；未知 CRS 不猜 pin，4个全删除串文仅墓碑。 |
| 关系/聊天/历史/邮箱接续 | `extract_civu_social_supplement.py`、`extract_civu_social_history.py`；`legacy-social-import`、`legacy-history-import`；163/164/165 | 已接受与待处理关系分开，系统消息保留类型，旧通知不重发；AI 历史不自动提供给当前模型。固定码邮箱不是生产邮箱所有权证明。 |
| 地点/旅程等用途图片 | `scripts/extract_civu_reference_media.py`；`internal/legacyreferencemedia/build.go`；`internal/postgres/legacy_reference_media_import.go`；167；客户端 `private_reference_media_view.dart/private_reference_targets_page.dart` | 新77图+复用55图，132用途材料/153关系（133当前、20墓碑）。407原资产闭合为248存续个人用途、3系统档案、156删除/无引用受限保全；不能把受限保全资产当作全部已激活或全 UI 完成。 |

来源29表停止 writer 后的聚合对账沿已有回执保持变化0；本批统一编辑器没有新增 Civu 写操作。真实所选原 owner 没有的照片/视频/AI 历史等正向样本，仍不能借另一旧账号伪造验收。

## 5. 已执行的必要测试与范围

| 证据 | 实际结果 | 能说明什么 / 不能扩大到什么 |
| --- | --- | --- |
| `E unified-backend-direct02` | 3 PASS：`TestMomentThreadClosedOrderedContract`、`TestMomentThreadEditingClosedSinglePreserveAndOriginalDigest`、`TestMomentThreadEditingHTTPExplicitSingleEchoUnknownRecovery` | 旧闭合同兼容、新单条/保留媒体摘要、HTTP EDIT echo/UNKNOWN 路由。不是实际服务器或 UI。 |
| `work/unified-backend-context01`；最终 `unified-backend-direct03` | 每次同一必要 content 用例 1 PASS | zero-city 原 activity/community/org UUID 规范化/拒非法值；旧合同不放松。只是针对变更重验，不计作多项独立覆盖。 |
| `E unified-backend-native02/03`；最终 `native04` | 同一复合 `TestMomentThreadEditingNativeSingleUpgradeOrderedRetirementAndRecovery` 各 1 PASS，完整隔离1–168迁移 | 原ID/互动、纯媒体单条、跨城、发布后排序、删根、1条剩余、历史成员>20、恢复、旧收据不变；native04补单独撤回后继续EDIT、zero-city真实组织关联及预览后撤权拒写。 |
| 同 native03/04 中的中途故障断言 | 注入第二条 INSERT trigger 异常，Commit 必须失败；第一原条 revision/status 不变，原 operation 没有成功事实；解除故障后再成功 | 是 Tx 中途回滚，非只有缺素材预检。未扩大为每个数据库故障点的矩阵。 |
| `E unified-map-native02/outcome.json` | `TestUnifiedMap168OriginalPublicPrivacy` 1 PASS，调用原 `TestMapProjectionNativeRealPublicMomentAndNoPrivatePayload` | 私有草稿不入图、明确公开可读、篡改 proof拒绝、撤回后移除、无正文/敏感字段泄漏。仅 scratch 清理适配，原业务断言与正式源码未改。 |
| E `startup01` | `TestNativeStartupRejectsLegacyIncompleteAndUnavailableSchemas` PASS，sourceStable | 缺168合同标记的 schema 被拒绝。 |
| `E unified-map-media-client01`、`unified-map-media-analysis01` | v5/v6 compound Moment pins 唯一复合 PASS；分析0E/0W | 真实合成 JPEG，正确原 Moment ID/revision，pin binding/lease/auth 逐一失效。不是实际设备地图视频。 |
| `E unified-moment-composer-client01/02/03`、`unified-composer-source-ready01.json` | 三个直接复合分批闭合：01中2PASS/1FAIL；02仅修复受影响复合PASS；03仅扩展整串撤回复合PASS | 默认1/追加、正文标题独立、0N地点、排序删除；6×3000 UTF8字节草稿恢复；原ID/media预览、UNKNOWN原GET不重PUT；整串撤回保留未发布材料。不是整个旧thread测试文件全跑通过。 |
| E `optional-context-reader01` | `private_moment_optional_context_test.dart` exit0、sourceStable | 本人 DTO 零城市原关联读入专项。 |
| `E now-moment-editor-host02`；`now-moment-profile-host01` | 各同一必要route复合 PASS，分析0E/0W | 公开详情3入口与个人新建/图片进入同一原 Now；owner/token/origin/role/source/busy及ABA拒绝；不产生发布请求。 |
| 最终 `E now-moment-final-entries02` | `所有详情与本人列表编辑回原Now且旧图片仅核实原操作` 1 PASS；分析0E/0W | 实际无callback个人列表、地点draft/public、公开详情共9个入口回原 Now/原ID；无scope禁用。旧图片实际原 GET404→显式 STOP 后 journal清空，canSave/canDelete为真也不露新写表单。 |
| `E unified-profile-create-hook-analysis01` | 定向分析 exit0/0E/0W | Now onCreate 默认1；既有/冷恢复稿保留提示。运行行为由 route/UI 证据另证。 |
| 最新 `E unified-moment-composer-stop05`；`unified-stage-operation-analysis03` | 实际 Now宿主单一复合 PASS；分析0E/0W/177INFO | 冷恢复2VIDEO冲突journal时三条正文/标题/位置/原ID保全；显式移除、UNKNOWN/404/STOP/新op/海报、真missing保留、重启恢复、显式移除后ready。最多18历史冲突选择器保全。此组件用受控视频fixture，未跑真实codec/手机。 |
| `E unified168-http01/receipt.json`，亦归档 E | 实际默认TLS HTTPS合成本人复合 PASS；QA全部撤回、2会话注销 | 纯文无标题/地点发布；原ID/赞/评论保留；跨城追加、重排、删根、1条剩余、整串撤回、原 PUT/GET收据不变。该链未上传新媒体、未读地点sidecar/地图隐私，均有NOT_RUN。 |
| E `remote-checkpoint02.json` 与已有build回执 | API build18、schema168，补丁刷新0新迁移；API+8导入工具Linux合批构建有结果 | 服务器/编译事实，不替代客户端真机流程。最终19构建/安装结果见本文交付版本。 |

## 6. 真实失败保留

- `unified-backend-native01`：scratch 缺 `dev-seeds/001_badminton.sql`，进入业务前失败；02/03补齐fixture后通过，04只因新增边界复跑同一项。最初04快照准备因不存在的可选 seeds 目录退出也保留，不能称业务PASS。
- `unified-map-native01`：原业务断言执行后，旧逐行DELETE清理撞160外键/guard，整项实际FAIL。02只移除scratch的5处旧DELETE cleanup，保留完整随机child销毁；精确diff/hash在outcome中。未改正式SQL或原业务断言。
- E `deploy-parser-first-failure.json`：部署CLI尚未接受168参数而失败，未发起远端请求；修正后的实际部署另有回执。
- `unified-moment-composer-client01`：旧tooltip finder不匹配，原FAIL保留；`unified-composer-analysis01..06` 包含实施中编译/unused/重复 `_buildWorkspace` 等实际诊断，最终07是0E/0W。不得把旧失败改名覆盖。
- `now-moment-editor-host01`：direct虽PASS，分析2 warnings，整体非全通过；02修正后通过。
- `now-moment-final-entries01`：移除旧edit caller后unused optional parameter warning；地点第二行测试未等布局完成，点击裁切边缘失败。02显式旧CREATE existing=null、完整按钮ensureVisible+settle后同复合PASS。
- `unified-moment-composer-stop01..04`：异步视频/secure-storage fixture等待超时均保留，未宣称真实codec失败。05实际Now宿主复合通过；分析01/02同样exit0，是中间源帧结果，最终源帧以03为准。
- E `device-checkpoint03.json` 保留APK17/18真实问题：第一次断网图像尝试网络恢复后成功，**不是失败证据**；首次纯媒体preview通用失败原因未定，后重试成功；人工查看期间approval过期，GET不存在后显式STOP；STOP后video operationID旋转但材料引用未同步导致混合整体检查阻塞。最后一项源码已修、组件PASS；APK19手机复验已经通过，见本文最终验收表。


## 历史接续原文（保留当时状态）

以下为修复完成之前的原始记录，NOT_RUN/IN_PROGRESS只属于所标时点，不覆盖本报告顶部最终结果。

# Birdtie Now 统一动态编辑器交付记录

## 当前状态

2026-10-16 APK/Web19 接续（仍 IN_PROGRESS）：客户端19已构建、部署并安装，API18/schema168保持。手机真实完成旧三条草稿恢复、断网视频UNKNOWN→原GET→STOP→重新准备、三条混合内容明确确认发布及Now内嵌视频播放。远端读回保留原纯图Moment ID与媒体ID；两条显式地点实际为雷克雅未克与伦敦WGS84参考点，原图无位置。随后已发布内容编辑/排序真实失败且未落库：原视频stage于17:15:49Z过期，17:16:37Z确认时155 payload guard仍要求stage有效；公开副本仍有效。正在新增169最窄修复，旧1–168 SQL不改。手机保留原草稿/未知操作，无重复提交成功，不把此步列为通过。实际证据：`unified-editor168/qa03-readbacks19-01`。原readback helper的city名称假设失败另行保留，修正审阅只证明实际记录范围。

2026-10-16真机接续：API build18 / schema168、Web18及APK18已实际部署安装。APK17完成无标题无位置纯文发布、原ID改成纯图并显示实际图片；APK18恢复双条草稿。视频断网UNKNOWN→原GET→显式STOP→重试准备获得真实海报，但停止旧操作后材料引用未同步导致整体检查阻塞，当前正在修复；未将三条混合发布列为通过。个人/地点旧可达编辑分叉也集中收口。真实失败与证据见 `unified-editor168/device-checkpoint03.json`，完整新真机仍未完成。

2026-10-16接续：最终后端已刷新为 `social168-20261015-build18`，schema168不变、本次应用0迁移。补齐EDIT原关联与城市解耦、已单独撤回成员后继续编辑的边界；相关同一原生复合测试和content直接测试均通过。客户端本人内容DTO的零城市原关联读入专项也已通过。公开详情回Now统一编辑入口正在接线，新APK／新真机仍 **NOT_RUN**。

2026-10-15：新增后端 EDIT 合同、168迁移已部署到当前 Birdtie 独立库，API `social168-20261015-build17` 已就绪。客户端统一面板仍在收尾；新APK和新流程真机验收 **NOT_RUN**，当前手机仍为上一批APK16。本记录会按真实结果接续，不将源码阅读或静态组件测试作为发布链路验收。

## 来源对应

Civu-client 本地与 GitHub master 同为 `1bbf0613b513001447f326225c944c1a1fbd8e8f`，工作树干净。本轮只读原项目；具体函数、发布／更新／媒体／草稿／权限调用链和文件摘要见 `docs/testing/evidence/civu-native-adaptation-20261015/civu-unified-editor-source-audit01.md`。可复用的是条目模型、默认一条、显式追加和稳定排序身份；Civu 原已发布编辑禁增删排序、编辑草稿不落盘，不能称为现成完成能力。

| Civu 来源 | Birdtie 增量适配 |
| --- | --- |
| `_MomentComposerEntry` / `_entries` | `moment_thread_api.dart` 同一条目模型和 `moment_thread_controller.dart` 活动条目；普通动态与串文共用 |
| `_addEntryAfter` / `_removeEntry` / `_reorderEntries` | 统一面板显式追加、删除和排序；新增168当前顺序与历史成员位置分开 |
| `editMomentId` / `initialThreadItems` / thread ID-position | 本人读取、原 Moment ID/revision、thread version；原互动和链接保留 |
| `MomentPublishDraft.entries/effectiveEntries` | origin/owner 隔离的本地草稿；未知提交恢复原 operation，不自动重复创建 |
| 旧发布／更新／上传接口 | Birdtie现有 preview → 明确确认 → commit → GET，原生stage/rights/session及地点权限适配 |

未直接复制 Civu 整页、路由、全局状态或账号依赖。用户规则见 `docs/product/BIRDTIE-NOW-UNIFIED-MOMENT-EDITOR-2026-10-15.md`。

## 已执行验证

- 后端闭合同与HTTP直接测试：3项通过。
- 隔离PostgreSQL完整1–168迁移及统一编辑复合测试：1项通过，覆盖原ID和互动保留、纯媒体单条、跨城、发布后排序、单条删除与整串撤回、重试原回执、原首条删除后公开读取拒绝、20项后历史追加、显式恢复。事务中途故障断言原首条版本保留且没有成功回执，恢复后重新提交成功。
- 隔离schema168地图权限复合测试：1项通过，私有草稿不入地图，明确发布可见，篡改回执拒绝，撤回后移除，地图载荷不带正文等私有字段。首次旧逐行删除清理与160外键冲突而失败，保留FAIL；仅scratch fixture清理改为销毁整个随机测试库后通过，原业务断言及正式源码未改。
- 启动检查直接测试：通过，缺少168合同标记被拒绝。
- 复合地图pin读取首张真实合成JPEG组件测试：1项通过，位置绑定变化、到期和退出身份使对应像素失效。
- API及8个导入工具合批Linux编译：9个二进制通过，源码帧稳定。
- 当前远端迁移：新增168一项，原167项迁移保持，API ready。
- 实际HTTPS复合链路：无标题／无位置纯文字发布，旧动态改文并跨城追加三项，原ID、点赞及评论保留，重排、删除原首条、剩余单条、整串撤回，PUT重复提交和GET返回原回执。全部合成内容已撤回，两测试会话已注销。该链路没有读取地点sidecar或上传新媒体，因此不将它标为全部地图／媒体验证通过。

证据目录：`docs/testing/evidence/civu-native-adaptation-20261015/unified-editor168/`。首次native快照缺少dev-seeds而失败、首次部署CLI参数尚未允许168而失败的记录保留；修正后通过。后者未发起远端请求。

## 待本批合包后验收

Now当前主面板与同一输入框；纯文字、纯媒体和多条混合媒体；每条不同城市；发布后追加、编辑、删除和排序；草稿恢复、上传失败重试；返回Now保留地图及面板；明确最终确认和权限／地图不可读内容清退。此处当前均为 **NOT_RUN_NEW_APK**，旧APK结果只保留其原验证范围。
