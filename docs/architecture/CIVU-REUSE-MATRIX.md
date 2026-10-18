# Civu 只读现状审计与 Birdtie 复用清单

## 2026-10-18 PMAP 增量（本轮当前状态）

用户明确要求按 `D:/Project/birdtie/Birdtie-Now-Pin-AutoMap-Codex-2026-10-18.md` 及 `birdtie-pin-automap-handoff/references/` 两图实施分类小 pin、独立地理动态方形媒体、必要局部聚合与自动地图路由。本轮的局部聚合授权覆盖下文10月12日“不聚合”的旧设计快照；它不允许随机移动坐标、伪造位置或隐藏权限边界。

复用对象为已经在 Birdtie 的 Civu 原生内容迁移关联、`map-content-v2` 当前 ACL、真实 taxonomy 字段、WGS84/GCJ02 边界、目录图卡与两端地图适配器。仍先审矩阵和现有权利，不整体复制 Civu、不调用旧中心补齐写库接口、不新增另一套地点库。`D:/Program/Civu` 和 `D:/BirdTie` 零写；本轮没有从两处复制新业务源码、账号、Session、坐标或媒体。现有获准的 GeoHUB 两份样式原字节保持，进一步高德路名/区名权重需要新的获准样式导出，当前 BLOCKED。

新区域判断数据独立来自官方 Natural Earth map units5.1.1 public domain，原归档、派生方法、许可、SHA均在 `apps/client/assets/map_routing/README.md`；不是 Civu 业务数据、城市目录或供应商结果，不写回旧坐标。地图类别符号沿用项目 Material 图标体系，无截取参考App资产或生产emoji。目录/临时地理结果/动态/收藏/旅程目标共享 `MapNavigationTarget`，保留其原来源、坐标系、访问上下文与寿命；临时搜索数据不持久，不升级为 City/Place 或批准。

API36只更新读取投影，schema178及账本完全不变；隔离数据库用例与真实环境零fixture边界分开记录。源码/直接测试/构建/装机/真实截图分别记账，不能用旧APK59证明本轮视觉和性能。当前状态以 [PMAP执行报告](../reports/BIRDTIE-PIN-AUTOMAP-2026-10-18.md) 与原队列增量字段为准。

64版Android性能适配仅修改Birdtie已vendored的Apache-2.0 `amap_map`宿主：使用同一锁定SDK的TextureMapView并同步原控制器类型、生命周期与Flutter TLHC/HC回退，不从Civu复制新代码或引入新地图服务/许可证。原作者许可保留；已核本机11.2.100 jar API与官方TextureMapView文档。性能和原生输入/读屏仍按新包实际证据验收，不能由接口兼容或编译通过推定。

2026-10-14 集合映射增量：当前 Birdtie `124_personal_collections.sql` 已有私人 ThemeMap 表与原个人集合 writer，旧材料中的“ThemeMap 没有目标表”不再是当前事实。批 F 增加 Birdtie 自有的 `scripts/prepare_civu_collections.py`，只接收明确选择、规范化的导出与映射，生成本地待确认 Journey/ThemeMap 候选；没有复制 Civu 业务代码、依赖、数据、Session 或媒体。仅只读核对了旧 `006_journeys_workspace.sql`、`032_creator_theme_maps.sql` 的字段与当前原输入闭集，未把旧公开、复制、成员、备注、媒体或地点权利直接映射为新许可。真实保全、隔离恢复、身份/Place 当前读取及导入仍未运行。见 [准备合同](../migration/CIVU-SELECTED-COLLECTION-PREPARATION.md)。

Birdtie 当前身份和 Agent 权限边界以 [Accepted Agent Identity and Ownership Model](AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md) 为准；早期复用表中平行 City Agent 的表述已被 shared Agent Runtime + CityContext 设计替代。

2026-10-12 Now 地图设计研究补充：用户要求地点与动态逐点展示、密集时保留淡色单点，取消数字聚合和“放大后才散开”。只读复核 Civu `docs/requirements/REQ-283-compact-category-map-markers.md` 的 2026-09-05 用户修订，以及 `Civu-client/lib/src/services/explore_map_marker_density.dart:80–114` 的逐点布局/空聚合集合、REQ-437 的圆地点/方拍摄点后，形成 Birdtie 自有可点击设计稿与 [第四轮说明](../ux/designs/2026-10-12-now-composer/REVISION-04.md)。仅借鉴视觉和交互概念，没有复制 Civu 业务代码、社交内容或用户数据，没有改生产地图策略/原队列；后续 LOD 和预算的隐藏规则不被当成本轮全显要求。预览纯底图沿用本仓已获准的 Mapbox 样式与现有公开配置，静态图片内不含凭据，动态为独立演示数据。实际原生实现、权限和性能仍待单独验证，既有发布门槛不变。

2026-09-30 Birdtie V2 补充：Agent-first 首页复用的是本仓已有的 `PublicCityMapView`、原生 Mapbox adapter、`PublicCityController` 和 City API。新增 Entity Layer、Agent Workspace 状态、规则查询及任务历史均为 Birdtie 自有实现；没有从 Civu 仓库复制代码或引入新的 Civu 运行时依赖。已审核 Activity 仅通过关联的公开点精度 Place 提供地图坐标；客户端不推测私有位置。未配置 API 时的演示 People、Group、Activity 地图实体与真实 API 模型隔离。

2026-10-01 Mapbox 凭据补充：用户提供的新 `lookluo` 公共令牌自检有效、样式可读，但标准公开矢量瓦片返回 403；之前已获准复用的 Civu 公共令牌在同一请求返回 200，复合源 TileJSON 与复合矢量瓦片也返回 200。旧令牌仅从 `D:\Program\Civu\Civu-client\.env.local` 只读取得并写入 Birdtie 被 Git 忽略的 `apps/client/.env.maps.mobile.local.json` 供本地 Android 验收；用户提供的新令牌保留在另一个忽略的 `apps/client/.env.maps.mobile.provided.local.json`。没有复制 Civu 代码、数据或提交令牌。该发现不改变 Mapbox 作为海外地图 provider 的架构选择；正式分发仍需在 Mapbox 账号核对令牌限制、额度及发布许可。

2026-09-30 内容供给与 Inbox 补充：Community Owner 提交、独立城市审核、撤回和审核结果 Inbox 事件均为 Birdtie 自有代码与数据表。本阶段没有从 Civu 复制消息、社交关系、群组内容或通知数据；它们不构成 Birdtie 的授权或内容来源。

2026-09-30 人员供给补充：Profile 编辑、本人确认的公开 Intent、独立城市审核、撤回及 Agent People 发现均在 Birdtie 模型中实现；没有复用 Civu Profile、Intent 或社交关系代码/数据。固定码测试身份仍与 Civu 用户隔离，正式身份绑定和联系请求尚待设计。

2026-09-30 边界修正：根据产品方决定，Group 和公开 Intent 的独立人工审核已由 ADR 0011 取代为本人确认后直接发布。此修正只修改 Birdtie 自有代码、迁移和文档，没有新增 Civu 复用；Place/Activity 的 City Seed 来源审核保持原边界。

审计日期：2026-09-29  
状态：完成路径、依赖、Git 状态及关键代码的只读盘点；未执行构建/测试、未连接生产环境、未复制或修改 Civu 文件。  
参考仓：`D:\Program\Civu`；Birdtie 正式仓：`D:\Project\birdtie`。

## 1. 结论摘要

Birdtie 是栖游/Civu 的下一版产品，在独立仓库按 Agent-native local social network 重新建模；现有用户、数据、接口与线上服务须逐步映射迁移，最终沿用原应用身份发布覆盖更新。Birdtie 以城市为入口，连接人、地点和现实生活，支持发现、兴趣/Intent、双向同意连接、Activity，以及经用户确认的 Moment/Experience/Journey。产品文档第 4–5 节已经定义主导航 IA（Now/Today、Explore、Network、Inbox、My Birdtie、Personal Agent、Organization/City workspace）与城市页面模块。当前 `apps/client/` 已替换为新 shell scaffold，并具备浏览器 OIDC 回调与 Session/Profile 读取连接、公开 City/Place 列表和 Activity 状态，以及登录后私人文字 Moment 草稿管理；`apps/api/` 有 Birdtie 自有 Foundation/City Graph 数据库迁移、公开 City/Place/Activity 读取、私有 Moment 草稿、Session/Profile Consent 授权边界、City Seed Place/Activity 跨账号审核接口和通用 OIDC 服务端流程。媒体完成可替换存储契约，但服务商、扫描、密钥和上传路由未定；Journey/Intent 仍只有受约束的初始数据模型。OIDC 发行方与客户端注册、真实登录验证和编辑角色开通未完成。产品方已选定国内高德、海外首城 Aberdeen Mapbox；Birdtie 的 Aberdeen Android 地图已在真机加载 Civu 定制 Mapbox 样式；Web 地图需要新 token 的 `styles:tiles` 权限，国内高德 Web/原生渲染仍需单独适配。先前早期演示屏幕和样例内容不作为实现基线。

前端已只读审阅 2026-09-25 之后的参考结构，并按 Birdtie 产品 IA 重新映射。Civu client 在 2026-09-26 至 2026-09-29 的 home shell、map/discovery/profile 变更仅作结构研究，其产品导航与业务模型不自动适用于 Birdtie，也不构成代码复制许可。

**复用判断：**

- 可以直接采用的首先是**已核验的小型纯逻辑算法/安全模式**，不是整个 Civu 产品模块。EXIF 解析与坐标解析函数可进入候选；媒体上传协议和对象存储客户端必须经 Birdtie 接口适配后重写；Place 解析/Provider 边界可借鉴，Civu 地点目录和数据不可直接搬。
- 地图渲染、Place 目录/详情、Journey、Moment/Post、User/Profile 均有大量实现，但它们深度绑定 Civu UI、路由、API、数据模型、供应商或权限假设，应围绕 Birdtie City Graph 重构。
- Feed、Recommendation 与搜索发现依赖 Civu 社交图、内容模型、行为信号和排序策略；Birdtie 不以无限 Feed 为目标，应先写权限过滤后的 City Graph 查询和可解释规则，再决定是否需要排序/推荐。
- 当前审计支持进入 Birdtie Foundation/City Graph 的详细设计与开发准备，但不等于某个 Civu 代码已获复制许可，也不替代对具体文件、依赖 license、数据权属、安全和外部服务的逐项审批。

## 2. Civu 仓库与依赖真实状态

### 仓库拓扑和 Git 状态

Civu 根仓是编排仓，不直接承载正式业务应用代码；由以下独立 Git 子模块构成：

| 子仓 | HEAD（审计时） | 最近提交（本地记录） | 状态 |
|---|---|---|---|
| `Civu-client` | `1bbf0613b` | 2026-09-29，诊断安装/原生地图审查记录 | `master...origin/master`，干净 |
| `Civu-server` | `0489536` | 2026-09-28，遥测 rollout 记录 | `master...origin/master`，有 `api/build/`、`build/`、`docs/work/...sh`、`tmp/` 未跟踪内容 |
| `Civu-website` | `9f29fad` | 2026-09-20，交付证据记录 | `master...origin/master`，有 `build/` 未跟踪内容 |
| `Civu-backend-anagement` | `2bca66d` | 2026-09-21，后台部署记录 | 本地 `master`（未显示 upstream），有 `NUL` 与 `build/` 未跟踪内容 |

根仓自身在 `master...origin/master`，有多张 `artifacts/` 图片、`build/` 和多个策略交付件未跟踪。根仓子模块状态显示 server、website、backend management 为带未提交变更状态；以上均只记录，不清理、不改写。根仓另有多个 `*-REQ*` worktree/checkout 目录，正式子模块以其自身 `.git` 拓扑为准。

### 规模、技术和集成点

- Client：Flutter/Dart；`lib/src` 约 392 个 Dart 文件。依赖含 Riverpod、GoRouter、Dio、EXIF、图像/视频/相册/文件插件、定位、WebSocket、AMap、Mapbox 和 flutter_map；地图代码并非单一 provider-neutral widget，而是含高德、Mapbox、OSM/Stub、多套生命周期/坐标边界和地图样式。
- Server：Go 1.23、PostgreSQL/pgx；约 772 个 Go 文件、149 个 SQL migration。模块包括 `auth`, `config`, `geocoord`, `httpapi`, `mediametadata`, `ossmedia`, `placeacquisition`, `placeprovider`, `placeresolver`, `routeprovider`, `store` 等。
- Server 直接依赖有 pgx、websocket、crypto、text、image、robotstxt 等；地图、路线、LLM/视觉、OSS 等能力由配置和 provider 接口接入。代码中可见 AMap、Mapbox、Google Routes、Pelias、阿里云 OSS 与 OpenAI-compatible LLM 配置。运行态是否启用、具体密钥/额度、生产部署与数据状态本审计未探测。
- 客户端地图组件分别达到约 1.7k 行（AMap、Mapbox），Place resolver 核心约 1.9k 行；系统间耦合较高，应优先提炼契约与测试覆盖的纯逻辑。
- 目前在代码仓根及子仓没有发现明确的顶层 LICENSE/COPYING 文件；递归路径中能发现的 license 多来自 `.tmp`/`node_modules`。不能据此认定业务代码可复用。复用前必须由负责人确认各子仓代码授权、第三方依赖许可、媒体/地点数据权属及地图/LLM/对象存储条款。

## 3. 分项复用清单

状态定义：**可候选直用**只限低耦合函数，经选定文件及测试再确认；**适配后评估**表示需先隔离接口或替换依赖；**重构**表示保留少量设计/UX 概念、另立 Birdtie 模型；**重写**表示不迁移业务实现。

| 能力 | Civu 事实位置/依赖 | 判定 | 可执行条件与行动 |
|---|---|---|---|
| Geo 基础坐标 | Server `internal/geocoord/` 含中国坐标转换（GCJ/BD Mercator）；Client `amap_coordinate_boundary.dart` 等处理 provider 坐标边界 | 适配后评估 | Birdtie canonical 坐标应先定 WGS84；抽取坐标系类型/转换接口和边界测试。中国地图偏移算法不可作用于 Aberdeen/通用 City Graph 数据。验证地图供应商条款、精度、CRS 和数据出处。 |
| Map renderer/layers | Client `widgets/civu_explore_map*.dart`, `civu_theme_map*.dart`, `persistent_map_host.dart`; AMap/Mapbox/flutter_map | 重构；指定样式/受限凭据复用 | 产品方已确定国内高德、海外 Aberdeen Mapbox，并明确授权沿用 Civu 的地图样式/可适用 Key。Birdtie 自建 City provider/viewport、公开 Place marker、Mapbox Web raster 与 iOS/Android SDK adapter；海外两端都指向 `lookluo/cmth7kwad001p01ssc9kw7kco`。Civu Mapbox public token 已复制到忽略的本地配置，Android 真机已加载定制样式；Web Static Tiles 返回缺少 `styles:tiles` 的 403，需在原账户创建带权限的 Web token。Civu AMap Web Service Key 已放入忽略的 API 本地文件，尚无调用方。最终 Android 包名沿用 `app.civu.civu_mobile`，原高德 Android Key 已复制到忽略的本地配置；只有沿用原发布签名证书时才可在正式包中生效。调试包带 `.birdtiepreview` 后缀，不能借此验证高德 Key。iOS 沿用 `app.civu.civuMobile`，但真正 Key 文件缺失，需从账号找回或按同一 Bundle ID 重新配置。高德 Web JS Key/安全代理仍需配置。不可搬 Civu 主 widget/state/navigation。详见 `docs/architecture/MAP-PROVIDER-CONFIGURATION.md`。 |
| Place entity/catalog/detail | Server `store` Place migrations/API、`placeprovider`, `placeresolver`, place acquisition; Client place screens/gallery/resolution | 重构；Provider 边界可借鉴 | Birdtie 建 canonical `Place` 与 `City` 归属、来源/维护者/有效期、外部 ID 映射、去重/合并、用户贡献 ACL。先写领域/API contract，再移植 provider-neutral 候选匹配思路。Civu schema/migration、目录数据和 taxonomy 不直接复用。 |
| Place resolver / provider | Server `internal/placeresolver/`, `placeprovider/`; Client `place_resolution_service.dart` | 适配后评估 | 接口化 `Resolve(query, region, locale, provider policy) -> candidates + source + confidence`；替换 AMap/中国地图链接假设，加入 Google/OSM/英国数据 attribution/额度策略。候选必须由人确认才能连到 canonical Place。 |
| Media upload | Client `api_client.dart` 上传调用、`photo_upload_preparer.dart`; Server `internal/ossmedia/`, `store` MediaAsset 和 media HTTP handlers | 适配后评估（协议模式） | 保留分阶段 create/upload/complete、checksum、短时签名、客户端直传和上传状态机的模式。抽象 `MediaStorage`/签名 URL provider；Birdtie 重新设计 quarantine/private/public、鉴权、病毒扫描、派生文件、ACL、删除及签名策略。阿里 OSS endpoint/config 与 Civu routes 不搬。 |
| EXIF/photo metadata | Client `services/photo_location_metadata.dart`（约 133 行）、`photo_upload_preparer.dart`（约 149 行）、`models/moment_media_metadata.dart`; pubspec `exif` | 可候选直用（提取纯解析逻辑） | 核对 parser license 与小型边界测试后，可独立采用 GPS DMS/日期解析或重写等价实现。上传派生图去 EXIF 的处理逻辑作为参考。Birdtie 必须明确 raw EXIF 私有存储、用户选择精度、地点候选置信度、公开副本移除元数据和删除生命周期；严禁自动从 EXIF 公开位置。 |
| Moment/Post | Client moment editor/cards/map/context、`moment_publish_draft_store.dart`; Server social/moment store、feed/moment APIs 与 migrations | 重构 | 重新实现 `Moment` 和 `Experience` 语义、作者确认、受众、地点/时间精度、来源/版本和多上下文关系。可参考编辑器草稿/回执/媒体身份稳定性；Civu 发布/互动/协作/审核 schema 与接口不复用。 |
| Journey | Client journey screens/workspace/route tools; Server journeys/stops/story/location grants、routeprovider、AI journeygen；大量 schema | 重构（后续阶段） | 把 Birdtie Journey 定义成历史/实时体验路线，可链接 Moment、Place、Activity；分阶段先可读历史/收藏，再做多站点编辑/复刻。保留位置逐站授权和版本/撤权设计作为参考；不搬旅行规划工作区、成员协作及中国路线 provider。 |
| User/Profile/social graph | Client profile screens/models; Server auth/profile/user/follow/chat/store | 重构；现有账户需迁移 | 建立 Birdtie Account 与 User/Agent ownership、Consent、visibility、双向 Connection Request、block/report 和显式 Community membership。保留稳定的 Civu 用户 ID 映射并设计原手机号/微信/邮箱登录到通用 OIDC 的安全绑定路径；不能让旧用户因产品更新丢失账户或内容。Civu token/session/auth、Profile schema、粉丝关系、聊天授权不能原样作为 Birdtie 的授权依据。 |
| Phone development login | `D:\Program\Civu\Civu-server\api\internal\store\phone.go`, `httpapi\phone_auth_handlers.go`, client `welcome_screen.dart` | 仅借鉴交互概念；Birdtie 重写 | 2026-09-30 只读复核了 Civu 固定 `123456`、短时 challenge 和请求限流。Birdtie 在本仓库实现独立的本机测试身份；未复制 Civu 代码、数据库 migration、身份映射或 Session。该流程不证明手机号所有权，不迁移旧账户。正式短信验证及原用户绑定仍须单独设计。 |
| Activity/Event | Civu place events、活动发现和 route/旅程关联代码可提供状态/时间处理参考 | 重构 | 定义 City Activity 状态机（upcoming/ongoing/past/cancelled）、时区、主办者、source/freshness、报名/双向同意边界；活动不等同于 Journey 站点或 Feed post。 |
| Feed / Moment stream | Client `discover_feed_providers.dart`, `discover_screen.dart`, `mixed_discover_screen.dart`; Server `feed_*`, social store | 重写 | Birdtie 不是无限滚动社交 Feed。以 City Graph 查询（类型、时间窗、城市/地点、权限和维护新鲜度）组合 Now/Explore 页面；仅需有限分页/时间序。不得直接搬 feed scope、曝光模型或关注流语义。 |
| Recommendation / ranking | Civu journey/place recommendation、用户偏好反馈、外部 AI/路线 provider 和 exposure schema | 重写 | 第一阶段使用确定性筛选与可解释规则（topic、时间、粗粒度距离、freshness）；硬 ACL/consent/block 过滤在排序前执行。达到真实规模且能证明提升行动转化时，再评估推荐。Civu 权重、曝光历史和用户数据不迁移。 |
| Search | Civu grouped search/provider 接入与 Place 搜索 | 重构 | 统一搜索可见 City Graph 对象；先 Postgres FTS/trigram + 地理过滤。Provider Search 只对 Place 补全使用。Agent 与 UI 共用授权过滤。 |
| AI/Agent | Civu journey generation/context recommendations/provider config | 设计参考，Birdtie 重写 | Birdtie Personal Agent/City Context 权限与上下文分离；Agent 只能生成提案，不替用户发布/联系/报名。城市回答要求来源对象引用和 freshness。不要迁移 journey prompt、外部服务日志策略或 Civu tool 权限。 |

## 4. Birdtie Foundation 与 City Graph

下列是基于 Birdtie V3 产品模型的实现顺序建议，所有权威对象和边由 Birdtie 自己建模，不依赖复制 Civu 表结构。

### Foundation：先把信任、身份和事实锚点建稳

1. **Account/User 与权限基础**：Account/session boundary、user profile 最小字段、Actor/owner、visibility、consent revision、block/report hooks、审计与对象版本。Agent 作为有明确 owner 的 actor 类型，先预留关系，不先开放自主动作。
2. **City 与 Geo**：City canonical ID、显示名/别名、区域边界/粗粒度定位、timezone、locale、source/freshness。用户可不提供实时位置而指定城市。统一 WGS84，Geo 坐标精度和展示精度分离。
3. **Place Graph**：canonical Place ID、`in_city`/区域归属、类别、几何、来源、维护人、有效期、外部 provider refs、重复合并审计；地点关联是边，不复制 Place。
4. **Media/Memory boundary**：上传意图、quarantine、私人原件、衍生版本、checksum、EXIF 私有元数据、删除/撤权、签名短时 URL。上传与公开发布是不同动作。
5. **通用对象关系/投影骨架**：typed relation edges、ACL 谓词、source/freshness、outbox/幂等写入、列表/地图共享查询模型。地图/Search/Agent 投影都只能索引对象 ID，读回时重验权限。

### City Graph：先建可信城市骨架，再加入现实经历

建议 MVP 对象：`City`, `Place`, `User`, `Media`, `Moment`, `Activity`, `Intent`, `ConnectionRequest/Connection`。Journey 以轻量可读对象/Experience 关联预留，并在产品闭环可用后扩展多站点。社区/组织内容可作为 City Seed 来源与维护者，组织管理界面可分期。

通用可见关系至少包括：`Place in City`, `User authored Moment`, `Moment happened at Place/City`, `Moment related to Activity/Journey/Community`, `Activity hosted by Organization and located at Place/City`, `Intent scoped to City/coarse area`, `Connection accepted between explicit actors`, `Media attached to private Memory or user-approved public object`。一份对象用多个关系进入个人页、地点页、城市页、地图和搜索；禁止复制正文形成多个权威副本。

## 5. 推荐实施顺序与退出标准

| 阶段 | Birdtie 实现 | 退出/验收条件 |
|---|---|---|
| A. Foundation | User/Account + Consent/ACL/审计骨架；City/Geo；Place canonical graph 与 seed 来源/时效 | 同一 Place 多来源可去重；不带定位可浏览城市；来源、维护者、freshness 可追溯；API 不信任 body 中的 actor/owner。 |
| B. Media foundation | 私人上传、校验、派生媒体、EXIF 解析和精度策略、删除/撤权 | 原始 EXIF 与私人原件不会进入公共 City/Search/Agent；用户确认前无公开对象；公开图像清除敏感元数据。 |
| C. 现实内容对象 | Moment/Experience 基础对象及 User/Place/City 多关系；Activity 状态与来源 | 一个 Moment 在 User/Place/City 读到同一 ID；更新/删除关系一致；Past/Cancelled Activity 不出现参与入口。 |
| D. Journey + Intent | 可读历史 Journey/Experience；限时 Intent（主题、时间窗、粗区域、受众、过期）；未来再做多站点 | Intent 到期退出发现；地点精度可控；Journey 站点按对象授权；复刻创建新 ID 并保留来源。 |
| E. 城市发现 | Now/Explore Anywhere 城市查询、地图与列表、确定性过滤/有限排序 | UI/API/地图共享权限过滤；城市空态和来源新鲜度真实；地图聚合无 ACL 侧信道；无需无限 Feed。 |
| F. 双向连接 | 对 Intent/Activity 的联系请求、accept/decline/expire/block/report；之后才开放真人会话 | 双方明确接受才建 Connection；共同活动、浏览、收藏不自动建关系；拒绝/屏蔽后 Agent 不继续联系。 |
| G. Agent | Personal Agent 私有素材整理草稿；City Context 仅检索获授权 City Graph 并附来源；动作 proposal + 人工确认 | 撤权立即阻断新读取；Agent 无法绕过 ACL、直接发布或建连接；引用可回到来源对象；数据不足时明确说明。 |

## 6. 继续正式开发前必须过的审计门

1. **产品/领域门**：确认 Foundation 对象、关系、对象生命周期、City Seed 维护责任和 MVP 首城范围；将本清单与 Birdtie 产品文档/技术架构的冲突同步修订。
2. **来源与授权门**：对准备借鉴或复制的每个精确文件/函数核实作者、license、依赖 license、允许的派生/分发范围；核验地点/媒体/用户数据权属。当前未发现可确认业务代码许可的顶层 license，故不得直接拷贝。
3. **供应商/隐私门**：明确 Birdtie 地图/地理编码/路线/LLM/媒体存储供应商、英国首城条款、attribution/缓存/数据区域；完成 EXIF、位置精度、Memory 隔离、删除/撤权和日志数据流审查。
4. **接口/安全门**：完成 Birdtie 的 auth/session、ACL、Consent、Media/Memory、City/Place、Moment/Activity/Intent contracts；证明普通 UI、搜索、地图、Agent 共用硬权限过滤；双向 Connection 状态机经威胁审查。
5. **迁移/实现门**：产品方已确定现有用户、数据、接口和线上服务逐步迁移，且最终沿用原应用身份。逐项迁移须记录来源路径、版本/commit、许可证、数据权属、映射、校验和回滚；生产数据仅能通过经授权、审计的迁移流程导入，历史 migration 不直接执行于 Birdtie。地图样式与可适用地图凭据复用已单独授权；选定值仅在忽略的本地配置，不进入源码或 Git。平台 Key 仍受包名、签名、Bundle ID、权限和额度约束。详见 `docs/migration/CIVU-TO-BIRDTIE-CUTOVER.md`。
6. **验证/发布门**：在 Birdtie 仓用自身 schema/API 与隐私场景验证；代码存在、feature flag 开启、部署和用户验收分别记录。审计通过前不扩大到正式用户数据或对外承诺已上线功能。

## 7. 本次审计未覆盖事项

- 本次 Civu 审计未运行 Civu 的 Flutter/Go 测试或构建；未对整仓逐行审查，也未确认线上 deployment、feature flag、供应商额度或密钥是否有效。Birdtie 自有 API 的本地编译和 PostgreSQL 验证另记于 Foundation 工作记录。
- 未检索/导出 Civu 生产数据库，不审查或搬运其用户内容、地点库、媒体或分析数据。
- 未确认各子仓完整许可证与第三方 SDK 商用/缓存条款；这属于任何实际代码迁移的先决条件。
- Birdtie 本身尚无首个 Git commit（检查时所有项目文件均未跟踪），故本文更新后的文档也尚未处于提交版本控制状态。

## 8. 2026-10-08 UI 修复包接续核验

本节为原矩阵的增量复核；来源均在只读的 `D:\Program\Civu`。本轮没有复制 Civu 代码、数据库、用户内容或媒体。以下只选择可借鉴的边界，实际实现继续沿用 Birdtie 的领域对象和已有服务。

| 能力 | 已核验的具体来源 | Birdtie 接续结论 |
|---|---|---|
| Place/provider 身份 | `Civu-server/api/internal/placeprovider/provider.go` 的 provider、external ID、来源、attribution、CRS、Search/Detail；`store/migrations/018_place_provider_refs.sql` 的 provider/external ID 别名 | 可借鉴显式供应商身份和详情路由；不把网页结果或外部 ID 冒充 Birdtie canonical Place，不搬目录或原 migration。 |
| Post/Moment | `Civu-server/api/internal/store/note_types.go` 的稳定 Note ID、媒体/地点/线程/协作者/version 与查看权限 | 关系概念可参考，正文和权限继续由 Birdtie `content/moment.go` 管理；不复制 Civu 社交 ACL 或保存时的隐式写入。 |
| 单媒体地理关系 | `store/photo_locations.go`、`photo_locations_postgres.go`；`Civu-client/lib/src/models/moment_media_metadata.dart`、`moment_place_selection.dart` 和 `services/photo_location_metadata.dart` | 参考按媒体 ID 关联、显式 mapEnabled、归属及地点有效性校验。Note 的地点不能扩散成每张照片的精确坐标；EXIF 不直接公开。Birdtie 保留自己的私人媒体隔离契约。 |
| 收藏 | `store/places_postgres.go` 的 SavePlaceWithSource、`place_save_sources_postgres.go` 的可见性处理；`notes_postgres.go` 的 SaveNote | Place 幂等及不可见来源的处理可参考。SaveNote 会验证 friends 关系、通知作者并同步公开 Note 的地点，因此不能直接复用为 Birdtie 通用 SAVE。继续使用现有 SavedController 与 `/v1/me/saved`。 |

许可证检查发现：Civu 根及主要子仓未找到 LICENSE/COPYING/NOTICE，不能据此认定源文件可以直接复制。已抽查 exif、dio、pgx 的 MIT 与 AMap 依赖的 Apache-2.0；Mapbox SDK 仍受其服务条款约束。这是部分依赖复核，尚未完成全量许可证、数据权属和生产服务审查。

本轮先前的网页公开地图令牌在真机及有限 HTTP 诊断中出现“样式 200、瓦片 403”。用户提供的设置截图确认它仅允许 `http://localhost:7357`，而 Mapbox 的 URL restriction 不支持原生移动 SDK。此结论只针对该令牌与构建，不覆盖或撤销此前版本的地图证据。

2026-10-08 用户另提供移动端公开令牌后，实际瓦片预检 HTTP 200，Android build04 在 Xiaomi 25098PN5AC / Android 16 已显示 Mapbox 底图，完成开发地点卡片→地图→原生领域详情→返回流程。凭据仅存被 Git 忽略的本地配置，未从 Civu 复制。对应 APK、源码及视频 hash 见 `work/ui-repair-2026-10-07/device/BUILD04-EVIDENCE.json`。这些地点仍是 dev-seed 开发示例；底图可用不等于真实城市供给、联网发现或基础 Agent 已完成。

## 9. 2026-10-10 Now UI V2 地图展示复核

用户指定 V2 规范，本轮只读 Civu-client，本地 HEAD `1bbf0613b513001447f326225c944c1a1fbd8e8f`。前三个指定来源的实际 blob 已匹配：`assets/config/poi-display-config.json` 为 `3a0bd10a1d035b637d6ea65263a5655b6df46c29`、`lib/src/services/poi_display_policy.dart` 为 `47412559743b3862e738741fdbe9de5bcf18d11f`、`lib/src/widgets/civu_explore_map.dart` 为 `994d1819f699ae376a2302da2130904c21eb5244`。

配置状态仍是 **proposed**（2.0.0，2026-09-01）。实际 loader 读取 type/context/minZoom/eligibility/预算，ExploreMap 转交 renderer；本地 Mapbox renderer 的 density 值仍硬编码为104/86/68/54/44，兼容cluster返回空groups，地图坐标投影仍用256-world推算。因此不能把配置中的全部值或文件名视为已部署/已验收行为。

BirdTie 只借鉴缩放分级、局部密度、场地唯一锚点、选择/任务保护和不裁列表的原则；使用自身现有实体/ACL、Mapbox Flutter锁定2.26.0的SDK屏幕投影及原annotation差分。未复制整页、权限、数据、未发布taxonomy或Civu算法。实际源、SDK单位、测试/真机边界详见 `docs/testing/evidence/now-ui-v2-2026-10-10/MAP-V2-ROOT-CAUSE.md`。这些参考核验不替代 BirdTie 本轮真机验收、供应商归属和发布门槛。

## 10. 2026-10-14 地图内容呈现增量

用户明确选定地图执行方案，原件 `C:\Users\chens\Documents\Codex\2026-10-13\d-project-birdtie-docs-testing-evidence\outputs\BIRDTIE-MAP-CONTENT-EXECUTION-SPEC-2026-10-14.md` 已逐字节复制到 `D:\Project\birdtie\docs\product\BIRDTIE-MAP-CONTENT-EXECUTION-SPEC-2026-10-14.md`，SHA256 `f6e75377a5135c90a179522ff27c815641bc0d14cf05052bf0571175048870c3`。接续正式仓当前未提交树和原BT-FIX-NOW-UI-001；先完成正在进行的B5/C1/143客户端合包自然检查点，再优先地点照片/近30天当前可见动态计数，随后公开Moment获准派生图。旧队列状态、依赖、lease和历史证据保留，不重开历史DONE、不另建任务队列。

只读参考Civu-client HEAD `1bbf0613b513001447f326225c944c1a1fbd8e8f` 的 `explore_map_marker_icon.dart`、`persistent_map_host.dart`、`explore_map_marker_layout.dart`、`persistent_map_scene.dart`。借鉴圆地点、方动态、轻标签和稳定详情概念；实际28dp/36dp/16dp及同ID全显沿Birdtie明确方案实现，不复制Civu预算、红点已读、去重、数据或业务代码。许可边界仍按原矩阵，不新增依赖或搬凭据、旧Session、媒体、分类素材。原视觉图只为视觉参考，不授截图内容迁移。

Birdtie原138/140地点媒体、PlaceHistory近30天授权集合、typed map current source/read lease与原详情继续复用。地图照片退回同点符号与实体失权退休分开；地图媒体仅有界临时内存，实体/media/version/SHA/权限/DPR/形状参与key、迟到结果复核。Moment点位仍关联Place，不能称拍摄点。未读合同缺失不画红点；私人图/EXIF不会随文字公开自动公开。

本节为来源和实施边界记录，尚不证明地图图片、公开动态媒体、provider/真机/真实权限验收已通过；对应检查/产物按后续实际批次封存，ClosedPilot/ConsumerBeta继续NO。

2026-10-14 Map1已实际接通Birdtie原138/140照片→typed-map-v3→原MapCanvas及当前三renderer代码，圆28/36/16与方记录符号沿上述用户方案；真实完整近期计数沿原PlaceHistory权限集合，未借Civu未读/去重/坐标算法。原生PG/API、实际JPEG像素与版本/ABA、正常组件图、API/APK/Webbc7构建和隔离Android安装已封存于 `docs/testing/evidence/remaining-execution-20261014/batch-c/checkpoint-map1-moment144/receipt.json`。真机现用缺图符号、没有获准真实Place图片；公开Moment图片Map2尚待本轮下一步编码，AMap/Mapbox Web真实媒体与iOS未运行，发布门槛仍NO。旧边界与证据不撤销。

2026-10-14 Map2已沿Birdtie原Moment、138/140 Place媒体及native控制帧实现独立145明确公开派生图→public detail-v3/map-v4→方形照片与原选点，Civu仅为已授权交互概念参照，不复制业务/media/Session。API/APK/Webbc8、原生隔离PG与实际JPEG/widget像素证据封存在 `docs/testing/evidence/remaining-execution-20261014/batch-c/checkpoint-map2-public-moment145/receipt.json`。真机当前仍无获准实图，旧新手机镜头不匹配；真实照片权利、Web媒体、iOS/全站体验NOT_RUN，发布NO。上文Map2未实施为历史时间点，由本段接续，不删除原证据。

2026-10-14 杭州真实数据接续：已核对原聊天两个直接用户消息的远端连接/适配导入和唯一Birdtie入口授权；明确选择的交接原件复制到 docs/migration/HANGZHOU-BIRDTIE-EXECUTION-2026-10-14.md（原件和目标SHA256 e114dd0769f7ec6a85a577b43a9d93ecbbbdf666ea8a92d92a1d040a78d89c45）。加密DB/媒体备份及DPAPI密钥仍在原受限Documents目录，未复制私人数据到仓库。Docker隔离源birdtie-civu-restore-20261014已复核network none/无端口/tmpfs；原库不成为Birdtie运行库。先完成当前本机合包，再实施Birdtie自有extractor/稳定映射/独立库/身份续接和受控Linux部署；只迁获准数据语义，不复制旧业务schema或Session。实际迁移/远端切流尚未完成，费用和不可恢复资源删除未授权，原发布门槛保留。


2026-10-15 原生146旧内容档案：Birdtie独立实现29表/396列closed extractor、schema/snapshot/文件摘要对账、20 owner永久映射与103旧Note最小私人档案。原Civu/DPAPI密钥/真实JSON仍受限仓外，未复制业务代码或旧schema；实际编译CLI对真实源prepare得到20owner/83现存/20墓碑，未native apply/激活。导入模型不搬credential/Auth/Session/公开授权，默认Account及Agent暂停/private/UNCLAIMED。146仅在owned QA及独立synthetic子库验证，旧DDL/QA原表计数未动；媒体仍UNAVAILABLE，Place/收藏/集合/CRS/rights有依赖继续deferred。实际本地API/APK/Web/Linux cross-build与新本人档案界面证据见batch-c/checkpoint-legacy-owner-archive146；不是旧用户登录、公网切流或全站验收。

## 11. 2026-10-15 动态通知和好友闭环复核

用户要求先跑通页面上的动态发布、修改、查看、删除、通知和好友，并整理可由 Codex 接续的完整清单。原 `BT-FIX-NOW-UI-001` 增量规范位于 [动态通知和好友闭环执行清单](../product/BIRDTIE-SOCIAL-CORE-EXECUTION-SPEC-2026-10-15.md)，接入原 live 队列与执行主提示；SOC 编号仅是父项检查点，不新建功能队列或改历史 DONE。

本次只读 Civu-client HEAD `1bbf0613b513001447f326225c944c1a1fbd8e8f`、Civu-server HEAD `0489536a0eb0792deff7793c8b143ad3e741d5ce`。根及主要子仓仍未找到明确业务 LICENSE/COPYING/NOTICE；客户端 AMap package 的许可不覆盖社交业务代码。Civu 页面深度依赖 Dio/Riverpod/GoRouter、旧 auth、follows/friendships、Postgres、媒体和原生推送，不能作为 Birdtie 直接复制模块。只借鉴用户结果、稳定实体、版本、状态和失效处理，继续 Birdtie 自有领域实现；没有复制代码、旧 schema、内容、媒体、凭据或 Session，没有改 Civu 文件或执行 Civu 测试/构建。

| 能力 | 具体只读来源，Client 相对 `D:\Program\Civu\Civu-client\lib\src`，Server 相对 `D:\Program\Civu\Civu-server\api\internal` | 本轮 Birdtie 决定 |
|---|---|---|
| 发布、编辑、查看和删除 | Client `app_router.dart:183/220/225`、`screens/content_detail_screen.dart:898`；Server `httpapi/notes_handlers.go:184/310/402`、`store/notes_postgres.go:291/612/713` | 借鉴同 ID 详情与本人操作；先补 Birdtie 公开编辑和管理入口，撤回后端已有。Civu 软删不代表即时物理擦除媒体。 |
| 版本与重试 | Server `notes_handlers.go:328/407`；Client `api/api_client.dart:428/433/446/452` | Civu If-Match 可选且客户端编辑/删除不传；串文每调用生成新时间戳幂等 key。不能直接当 Birdtie 严格并发或未知结果保障；使用本仓具体批准、revision、稳定 operation 和原操作回执。 |
| 串文 | Server `store/note_types.go:187`、`notes_thread_postgres.go:11/66/168`；Client `screens/publish_moment_screen.dart:3544` | 原子创建、共享受众及每条独立媒体/地点可参考。Civu 串文编辑是循环逐条操作，不称原子编辑；Birdtie 不用循环发普通动态替代串文。 |
| 通知 | Server `store/notification_queries.go:138/396/411/431/446/455`、`notification_cleanup_postgres.go:55/64`；Client `screens/notifications_screen.dart:591/673/947` | 已见与已读分开、批量动作有本人/分类/快照边界、失效目标去敏感摘要。复用 Birdtie 原 Inbox，补权威剩余计数和候选结果入口，不另建收件箱。 |
| 好友与关注 | Server `store/friends_postgres.go:97/119/185/228`、`messaging_workspace.go:324/336`；Client `screens/add_friends_screen.dart:174`、`follow_and_friend_requests_screen.dart:36` | Civu 有独立 friendship，但申请额外要求互关；Birdtie 继续当前 Connection 双向同意与独立 Follow，不搬互关前置。 |
| 好友受众冲突 | Server `store/notes_postgres.go:722/724`；Client `screens/publish_moment_screen.dart:3533` | Civu friends 可见实际判断双向 follows，未使用独立 accepted friendship。Birdtie 不复制这套 ACL；未来受限 Moment 受众须有自身统一合同。 |
| 私信和推送 | Server `store/messaging_postgres.go:42/62/97`、`mutual_follow_messaging.go:65/98/104`；Client `messaging/push_service.dart:10/73/214` | 借鉴当前参与者/Block/隐私复验和账号绑定推送。互关自动建会话、接受请求及自动问候不迁移；站内通知与系统后台送达分开验收。 |

Birdtie 当前工作树核对：144 UPDATE 只允许私人草稿，公开撤回及地点详情入口已有；公开 publication 当前 revision/snapshot 校验不等于持久原操作回执，补未知结果事实时不能仅按 Moment 当前 published 状态推定原提交。145 已实现独立公开单图，但 Now 图片/视频 selection 尚未形成完整公开图文流程。本人记录最多100条，作者阅读是当前地图范围摘要。通知点击已读、主要深链及申请处理已接；候选审核结果缺直达目标，已读后全局计数未知被首页当作0。Now 旧好友管理回调没有调用点，原移除/屏蔽/举报组件需补可达入口。当前没有完整城市/关注动态列表、动态点赞评论或原子串文，不以旧 DONE 推断它们存在。

本次仅规划与执行接入：功能改码、受影响运行、视觉和新包交付均未在本聊天执行。后续以原队列、当前源码及实际证据更新，优先合批编码和编译，正常字号、不录屏，原身份、媒体、迁移和发布门槛保持。

## 12. 2026-10-15 原生150–155与公网接续

上节是实施前审计快照。当前Birdtie原创城市PUBLIC动态、完整历史／筛选、互动通知、九图或一视频及原子串文已实现于原生150–155；沿原Connection／Follow／会话／143分享，不复制Civu业务代码、受众判断或推荐系统。聊天Moment卡片已对齐城市可选Place的公开读合同，两名合成账号在真实API05验证同canonical引用／撤回退休；未运行完整双角色UI／聊天媒体。

原杭州公网唯一IP入口已切Birdtie Web02与API05，旧Civu writer和Pelias停写，旧库／媒体／配置保留。146／149私人档案与154固定验证码123456的18唯一旧手机号原owner绑定已实际提交；真实一人接续、Person激活而Agent暂停，旧Session不复制。原映射historic UNCLAIMED保持，测试登录不伪称原所有权认证。

20 owner／103 Note／34 Journey／3 ThemeMap／189 SavedPlace为受限私人档案，非当前全可编辑／公开对象。5511地点／339城市CRS与live_only合法目录映射、407旧媒体完整性差额和扫描／权利、旧关系消息组织、2仅邮箱账号及最终停写增量仍未补齐。使用原保护合同，未知事实不猜。完整证据与产物见 [交付记录](../reports/BIRDTIE-GITHUB-REMOTE-MIGRATION-2026-10-15.md)，没有据本批关闭整站或发布门槛。

## 13. 2026-10-15 既有备份原生适配增量

用户本轮明确授权“允许读取既有备份并迁入 Birdtie”，同时保留原库、私人内容归原账号且不自动公开。新实现仅为 Birdtie 自有 `legacycatalog`、`legacynative`、156阶段账本和守护CLI；没有复制Civu业务代码或旧schema，没有向旧库写入。146/149来源档案、删除墓碑及原阶段回执保持不可变。

实际源纠正：5484个旧region_id为空不代表缺城市；5245个地点能沿明确admin_area_id及父链映射城市。339个旧中国城市由CN+adcode唯一映射，1067个active城市目录含唯一确证的Aberdeen→既有aberdeen-gb，避免重复身份。源中未发现Mapbox/live_only字段或值，前节关于它们的泛化不能作为本备份实际事实。第三方地点来源权利仍须逐类核对，不能由这项纠正推出公开许可。

城市目录只采用行政名称、国家、父级与确定IANA时区，城市中心保持未知。只读来源 `D:\Program\Civu\Civu-server\api\internal\placeprovider\amap.go`、`httpapi/places_handlers.go`、`cmd/sync-china-destinations/main.go`、`store/china_administrative_catalog.go` 与 `store/cities_postgres.go` 显示geo center混合了未转换AMap值和地点均值，禁止整体猜定WGS或再做全量GCJ转换。地点只有storedCoordinateSystem明确WGS或wgs坐标对与当前stored值匹配且无冲突时保留坐标；其他位置为空。

转换保留source→native永久映射、原owner归属、私人草稿状态与未承载字段清单。复杂旅程、未映射地点及旧媒体不能靠截断、第一地点或公开URL伪装成完成。实际落库、验证数量和仍未完成部分见 [原生适配记录](../reports/BIRDTIE-CIVU-NATIVE-ADAPTATION-2026-10-15.md)。

156/157现已真实提交：1,067城市、5,245草稿地点、83私人动态、3私人主题图、184收藏，以及135张归属8原账号／52动态的私人照片；不恢复墓碑、旧Session或Agent激活。原回执与重复执行一致，导入照片实际字节和归属已核对。旧方向0标记按原编码画面重编码，不猜旋转；EXIF已移除，扫描仍为NOT_RUN。现有已接续用户实际16动态、33收藏、2主题图／9私有地点引用读取通过，全部验证会话退出。私有地点明细只沿原146/149/156及当前owner关系读取，不开放全局draft目录；其编辑、删除、撤权沿Birdtie自身领域控制。22存续旅程详细安排、5视频、缺失媒体、旧关系／消息／组织与最终增量仍未完成，详见上述记录。


2026-10-15 原生158/159继续：在用户既有迁入和部署授权内，Birdtie自有legacyjourney/legacyvideo已将22旅程及110条日程、5完整私人MP4迁入同一独立Birdtie库。仅转换原备份数据语义，没有复制Civu业务实现或旧schema。原owner／来源永久映射、12旅程墓碑、原回执保持不可变；视频原字节保留，兼容播放副本按完整帧／时长／AAC包回验，AV仍NOT_RUN。两阶段重复执行不会覆盖编辑或复活删除。日期／站点／安排／交通及原文沿普通Journey模型读取编辑，不另建运行时旧库。具体实际数量、原账号读取和缺口以原生适配报告158–159节及checkpoint159为准；缺失媒体、未映射数据、旧关系消息组织和最终增量仍未完成。


2026-10-15 原生160–166实际接续：复用仅限经保全源的数据语义与用户授权迁入，Birdtie自有位置集合、外部稳定引用、legacycontext／legacysocial／legacyhistory与固定码email模型已实际运行。全部171条存续Note媒体归原owner，原135图事实不改；31图恢复的HEIC以固定Ubuntu插件完整解码。247无城市Place保持draft、原未知CRS仅作私人历史，原社交关系与typed消息保持事实、旧AI／通知／偏好保持惰性。原Civu不作为在线运行库，不复制旧Session、不激活Agent或自动公开。独立Pelias复用既有GB／IS索引副本，来源WGS84及OSM归属显式传递；原索引、容器与原库保留。软件仍采用此前Civu provider容器的固定摘要和既有许可配置，未引入未核实新数据集。实际导入、未决236其他用途资产与手机离线界限详见适配报告160–166节及checkpoint166；不把29/29停写一致性和本次native聚合当全407像素适配或真机通过。


## 2026-10-15 统一动态条目编辑器来源复核

本次只读 `D:/Program/Civu/Civu-client`，本地与 GitHub master 同为 `1bbf0613b513001447f326225c944c1a1fbd8e8f`，工作树干净，compare identical/ahead0/behind0；指定两文件摘要前后不变。源码审计与真实调用链归档于 `docs/testing/evidence/civu-native-adaptation-20261015/civu-unified-editor-source-audit01.md`。这不是 Civu 运行验收。

| 来源能力 | Birdtie 对应与处理 |
| --- | --- |
| `publish_moment_screen.dart` 的 `_MomentComposerEntry`、`_entries`、默认一条、`_addEntryAfter`、`_removeEntry`、`_reorderEntries` 和稳定对象 key | 复用状态组织和交互语义；在现 `moment_thread_api/controller/panel` 中实现 1..20 条，Now 原 `AgentComposer` 绑定活动条目。不复制 Civu screen、Riverpod、GoRouter 或全局仓库。 |
| `editMomentId`、`initialThreadItems`、`threadId/threadPosition` | 沿 Birdtie 本人当前读取、原 Moment ID/revision/thread version 接续；Civu extras/缓存不作为权限证明，历史位置不能覆盖。 |
| `MomentPublishDraft.entries/effectiveEntries` | 借鉴旧单条草稿兼容为首条；适配现 origin/owner 的持久草稿和稳定材料 scope。旧未知 operation 必须先核实，不擦除或重发。 |
| `createNoteThread` → `/v1/note-threads` 同事务新建 | 继续 Birdtie 原 preview/commit/GET/STOP family，新增明确统一编辑合同。旧 Civu 每次生成新幂等键、媒体前置和私人 raw 文件复制不能照搬。 |
| Civu 已发布编辑逐条 PATCH，另 PUT legs；编辑模式禁增删排序且不保存编辑草稿 | **不能直接复用为完成能力。** Birdtie 新增168事务、当前展示顺序、明确删除、原 ID 保留和恢复逻辑。串文不自动生成旅程／路线。 |
| Civu 媒体上传、owner 校验 | 只参考流程；使用 Birdtie stage/rights/current source/Session 与逐条多位置合同，关联地点不扩大可见性。 |

根与子仓未找到覆盖业务源码的明确 LICENSE/COPYING/NOTICE，故本轮不直接复制业务代码。实现为 Birdtie 原有组件和原生领域合同增量，不迁入旧 auth、Session、schema、全局依赖或隐式公开权限。新要求的产品规范见 `docs/product/BIRDTIE-NOW-UNIFIED-MOMENT-EDITOR-2026-10-15.md`；实施与验收仍进行中，旧 APK16 不算新编辑器验收。


## 2026-10-16 统一编辑器最终接续

前节为来源复核与实施前状态。现已完成Now统一条目编辑、默认一条/显式追加、原ID编辑与原子排序删除、每条独立跨城地点、持久草稿和未知上传恢复。原个人/地点/详情入口返回同一Now编辑器；无普通/串文类型选择页。新增169处理已发布视频暂存过期后的合法原副本沿用，原1–168共311SQL保持。API19/schema169、Web/APK19已实际部署安装；手机三条混合发布、修改排序、单条删除、整串撤回及服务器版本4/5/6核对通过。Civu本地/GitHub再次只读核对仍同SHA，未改旧项目/库。源码与运行、迁移与UI样本分开，完整报告 `docs/reports/BIRDTIE-NOW-UNIFIED-MOMENT-EDITOR-2026-10-15.md`，未执行边界与历史失败保留。父任务更广的全站要求不因此自动关闭。


## 2026-10-16 Now 地图遗漏重新核实

用户报告真实 APK19 的阿伯丁地图空白。补充只读核对发现83条存续旧Note原本均public/ok，366个阿伯丁Place原本active；迁入私人草稿与旧PUBLIC筛选共同遮蔽了原浏览内容。历史Civu-server提交4438472的promote-place-catalog逐值转送OSM提供者坐标，只留promotedFrom/sourcePlaceId，故仅依赖storedCoordinateSystem漏掉可证明的坐标。前节私人隔离描述保留作为已执行迁入事实，不再将其误认为原始可见性。

本次抽取地点圆形照片/类别、右侧地点名、关联动态及同面板阅读语义，Birdtie新MAP_CONTENT读投影沿原sourceHash/visibility和当前授权恢复。只读历史代码/截图不作为Civu运行验收；不复制无明确许可的业务源码，不向旧库写入，不将私人内容转公开。新增170证明账本与实现/验收见本次产品及报告，当前IN_PROGRESS。


## 2026-10-16 Now 地图与原动态阅读实际修复

本次发现“已迁入”并不等于“首页可见”：旧地点/动态的迁入草稿与PUBLIC筛选使Now空白，历史OSM来源链也漏接。独立库170已保留原公开事实5,492个地点和83条存续动态，3,724个地点有确切坐标；1,768个无充分坐标证明的地点保留不上图。阿伯丁及周边指定范围实际返回384个地点、8条动态地点关联、6个含动态地点与封面。原私人内容不扩大权限，原库/项目零写入。

API23/schema170、Web24与APK24已部署安装。真实手机已显示Civu式地点照片、名称和动态数；点选、在Now阅读正文/图片、返回与续期已验收。修复Android照片被旧图标名覆盖、透明画布误选邻点和弹层可见性交接。定向数据库/API/客户端测试及真实媒体摘要通过，完整记录见 `docs/reports/BIRDTIE-NOW-CIVU-MAP-2026-10-16.md` 和 `docs/testing/evidence/now-civu-map-20261016/checkpoint170.json`。

此结论仅针对地图与阅读范围，不把APK19发布证据改记为APK24重测。全仓回归、iOS、Web完整交互、全部城市/账号和最终包完整发布重跑仍NOT_RUN，父任务保持IN_PROGRESS。


## 2026-10-16 内容地图 CM 增量接续

此节覆盖前轮“默认铺全部地点”的行为，不改历史执行事实。Civu-client本地/GitHub仍同1bbf0613，原项目只读；抽取persistent_map_scene的独立moment anchor、shootingPoint方形媒体、persistent_map_host的常驻/settled/取消/LRU、explore_map_viewport四角跨界以及Mapbox marker delta和12/4预算语义。未找到明确业务源码许可，未直接复制页面/导航/global state；Civu density返回空clusters不能作为聚合完成证据。

Birdtie以原GET /v1/me/map-content适配v2视口SQL，ACL前置后聚合并完整分页，171恢复16个确有授权的旧地理锚点，零fake POI、源库零写。Now默认目录空读关闭；任务、收藏、旅程、主题地图、活动保留独立上下文。方形动态锚点→正确entry→原Now面板正文/媒体；无位置发布不变。原生按DPR居中方形内容聚合、稳定图片身份和增量annotation；Web滚轮/旋转/双击查询漏触发已修。授权与分页位置凭据分离，过期内容清除，当前320成员窗口可续页。

API26/schema171、APK/Web29（APK与28逐字节一致）实际构建部署安装与目标验收，以docs/testing/evidence/content-map-cm-20261016/checkpoint171.json为准。来源映射、真实路径、测试/失败/性能和NOT_RUN见docs/reports/BIRDTIE-NOW-CONTENT-MAP-2026-10-16.md。严格同camera手机A/B、全部250页大聚合真机、真实跨设备撤回push未测，不称全部CM09或父任务完成。


## 2026-10-16 用户截图反馈：真实坐标、覆盖缓存与阅读面板

只读Civu-client基线仍为1bbf0613，Civu-server为0489536a；不搬页面/导航/全局状态。延续persistent_map_scene、explore_map_viewport、marker budget/persistent host的能力语义：不同坐标不再网格合并，精确同点以小真实封面角标表示；视口覆盖求差及有界缓存、增量marker和地图常驻。Civu discover/feed/media viewer的作者、原时间、图片比例与显式视频播放语义接入Birdtie既有Now主面板。

实际新路径、限制与源码映射见docs/reports/BIRDTIE-NOW-MAP-REFINEMENT-2026-10-16.md。独立Birdtie schema172给既有83条动态补原公开作者归属（9作者/6头像），不公开当前私密profile；API29提供受当前权限验证的旧图片阅读图，原件不变。API29/APK33/Web33实际部署/安装与针对性阅读验收有回执；缺城市视图的3个视频目标、200张冷图性能和完整发布回归仍未完成。


## 2026-10-16 国内供应商、坐标边界与按需媒体增量

Civu-client仍只读1bbf0613。两条此前实际访问的上海/汕头公开地点源有GCJ02→storedWGS84证明，Birdtie值与原值一致；未推定全库正确或转换全库。借鉴明确countryCode供应商分流，Birdtie原生适配现地图状态、权限、媒体budget和阅读入口；不搬Civu页面/导航。坐标前向公式取固定MIT coordtransform并保留完整许可；反算有界且区分数值包络与国家。

本轮明确迁入窄包Civu packages/amap_map（Apache-2.0，含既有Android16补丁和11个公开插件图标），保留LICENSE/NOTICE/逐文件SHA；关闭原生日志、修订阅和迟到初始化销毁。此许可审计只覆盖该插件，不扩大到Civu业务源码。map_provider_policy、Native/Web适配、174保护迁移与来源路径见BIRDTIE-CHINA-MAP-BOUNDARY-2026-10-16.md。

按需媒体由Birdtie自身API/阅读面板实现，API30已上线、34有目标图片及部分Web视频证据；候选35含取消/新租期和高德修复，未部署安装。高德配置缺口和国内真实对齐NOT_RUN，严格性能及更广父任务仍IN_PROGRESS，详见两份本轮报告。


## 2026-10-16 Android37 增量更正（当前状态）

本节优先于下方较早配置快照；旧失败证据保留。用户本轮明确暂不做网页，Web34保持，不要求网页Key，也不部署174。

- 本机原本已有CIVU Android Key，旧问题包含Dart适配器未识别native manifest配置。现已接入只返回配置存在性的通道，不把Key传入诊断。
- APK36第一次真机失败为未注册AMap平台视图；离线依赖解析此前在Windows symlink步骤退出，生成注册器陈旧。已通过仅进程级关闭非Android平台的离线pub get正常生成，未手改生成文件或全局系统设置。
- APK36r2真机加载到高德底图，但原CIVU Key返回INVALID_USER_SCODE。用户随后已为预览包与实签名配置新Key；当前Key与CIVU不同，已核对配置和manifest一致，无需再次提供Key。原CIVU配置未改。
- 已复用CIVU用户GeoHUB导出的原始 `style.data` 与 `style_extra.data`，Android首次创建map前加载，两文件字节与CIVU原件一致。加载失败可重试；成功同进程复用，抑制底图POI图标与文字，保留道路标注及Birdtie业务内容。插件的基础许可证与用户自有样式来源分开记录。
- APK37已合包，SHA256 `d2a6cba403298bc605e27649c390cd71f3fd880a516e43ad690f1397eef85465`。包名 `app.civu.civu_mobile.birdtiepreview`，签名SHA1 `73:84:0A:17:D0:CA:7B:4B:F4:15:41:B4:2C:EC:B7:A2:34:D5:05:23`。Key不会提交Git。安装时ADB无设备，最新手机仍是36r2；37的新Key鉴权、自定义样式与地标对齐均NOT_RUN，外部依赖BLOCKED。
- 6项Key桥接测试、4项样式测试PASS；样式接入后Key6项复跑PASS（不是新增6项）。保留两次fixture异步同步失败及修复记录。证据分别在 `docs/testing/evidence/china-map-boundary-20261016/android-key-reuse`、`android-civu-style`、`android36-device`、`android36-r2-device`、`android37-device`。
- APK37不包含随后DGS地图稳定性和身份恢复代码。后续源码不能沿用APK37合包/真机结果，需合批新构建。

新增源文件：`amap_native_configuration.dart`、`amap_offline_style.dart`、Android `MapConfiguration.kt`；适配 `MainActivity.kt`、`amap_city_map_io.dart` 与pubspec；样式来源 `D:\Program\Civu\Civu-client\assets\map_styles\civu_android` → `apps/client/assets/map_styles/civu_android`。CIVU只读，未整体迁移旧导航。


## 2026-10-16 DGS 地图稳定性与全球目的地增量

在当前 cef1a11 基线继续修改，未回退附件4637cfb，也未写原Civu。Civu目的地目录实际不是全球地理服务；旧GET中心补齐存在写库路径，因此只读源码、未调用旧在线接口。复用300ms debounce、迟到响应淘汰和独立地理显示语义，接Birdtie新BrowseDestination、现有MapCanvas和bounds-only内容接口；不复制旧导航、持久Temporary结果或造内部City/POI。完整逐函数映射见 `docs/testing/evidence/dgs-20261016/destination/REPORT.md`。

Android原包含3dmap-location-search 11.2.100/search9.8.1，此次新增窄MethodChannel地理搜索桥，沿Manifest Android Key和明确会话同意。SDK结果GCJ02只在明确边界转换，海外Mapbox搜索不受当前浏览国家限制，两类provider显示许可分开。Now选择器保留原正文与地图，真实空城市正常可选；产品规则见 `docs/product/BIRDTIE-GLOBAL-DESTINATION-2026-10-16.md`。

地图内容单封面错误全清及个人/组织切换不恢复已复现并修复；49项读取测试、7项根组件/原生桥测试及26项目的地测试通过，包含重叠不可加作新增总数。APK38已编译及资产/签名核验，ADB离线，真实鉴权、自定义样式效果和坐标对齐仍NOT_RUN。后续通知/分享等修改不属于APK38证据。

真实数据限定清理已执行22表103行（8动态、1会话、3消息、2请求及依赖），保留83条Civu动态、19条未知动态、39条旧消息、29账号；原Civu零写。精确恢复验证/重复执行与保护摘要见 `docs/data/SYNTHETIC-DATA-CLEANUP-REPORT.md`。此状态不等于所有旧数据端到端验收完成。

## 后续实际交付更新：APK40 / API31 / schema175

此前候选包/ADB离线记录为历史快照。当前40已替换安装，新高德Key和原Civu两份样式已在上海真机验证；直辖市城市尺度修复已入包。真实Civu owner会话、Aberdeen内容/图片/作者原时间、超过30秒后台后的地图恢复均已执行。巴黎外部目的地定位为APK39实际证据，40未重复。地图平移封面复用与性能限制、数据清理/隔离及社交缺口统一见 `docs/reports/BIRDTIE-DGS-EXECUTION-2026-10-16.md`；完整验收尚未完成，不能把旧NOT_RUN统称已通过。网页仍延期。

## 2026-10-16 APK40空图复发：登录接续增量

用户20:46截图后，原账号access在19:48:33已idle超时，absolute仍到翌日02:23:28；手机冷启动静默变为未登录。原102动态/83条Civu映射未丢失。前批32秒后台验收只覆盖地图30秒读取租期，未覆盖会话30分钟idle，不能视为长期登录恢复通过。

只读Civu认证源码显示access/refresh分离（access约30分钟、refresh约30天）。本次仅复用接续语义，不复制Civu SharedPreferences凭据存放、非原子轮换或任意HTTP写重试。Birdtie继续origin绑定的secure vault，新增176设备凭据family：30天绝对期限、原身份仍需active/current、原子轮换、同操作60秒加密响应重放、退出撤销family；旧access不自动升级，首个新pair来自真实用户明确固定码登录。权限/审批继续按新Session重新验证。

对应路径为 `apps/api/internal/sessionrefresh/`、`internal/postgres/session_refresh.go`、`internal/httpapi/session_refresh.go`、`migrations/176_persistent_device_sessions.sql`，以及 `apps/client/lib/src/auth/{birdtie_auth_controller,session_vault}.dart`；Now独立更新恢复提示并等待owned/typed源，保留地图实例。仅dev_phone接入持久接续，OIDC/dev_email保留既有行为。精确合包、部署、实际手机与NOT_RUN边界见 `docs/reports/BIRDTIE-SESSION-RECOVERY-2026-10-16.md`，不能把源码复用当Civu运行验收。


## 2026-10-17 Now 目的地图与连续阅读增量

本轮接续原 `BT-FIX-NOW-UI-001` 的 DGS-09/10/11、既有视觉与内容地图工作。外部来源为 `D:\Project\birdtie\Birdtie-Now-Unified`，先读 `START-HERE-CODEX.md`，按本机 `f9645af` 逐 hunk 接回3文件候选补丁；未整体复制旧仓或覆盖后续修复。

| 选定来源 | Birdtie 去向与适配 | 复用与权限边界 |
| --- | --- | --- |
| Civu 国内/境外目录、透明纪念品 contain 的既有交互概念 | `apps/client/lib/src/city/destination_picker_grid.dart` 与原 `_NowCityPicker`；沿用现中文主题、唯一查询焦点和原全球搜索 controller | 不复制旧导航、Riverpod、城市全集或 provider 结果持久快照；无图仍可选，无可靠镜头由当前查询显式消歧 |
| 受限 `civu-private-reference-images-20261015-02` 同 run `c5df0d7c-f361-4275-9838-feb6d1d92352` 的青岛 REQ-360 与济南系统档案原字节 | `internal/cityartwork/fallbackexport` 按摘要生成 `apps/client/assets/destinations/qingdao-d2988c88.png`、`jinan-69b9d3d6.png`；均324px，非原图替换 | 原3351927/2749346字节文件保持不变。只有 sourceGeo→同run city alias→系统媒体精确关系可生成公开只读投影；禁止 owner 私有文件、通用档案下载或名称猜映射 |
| 包 `assets/edinburgh.png`，原 Civu-client `assets/destinations/edinburgh.png` | `apps/client/assets/destinations/edinburgh.png`，原512px透明PNG，SHA256 `1cab9f1418801643a59ce4eaafd8b94be485c69d79b1465603ab653674a565e7` | 仅真实 `civu-geo-66920ecb-9717-42bb-8320-a43a1d2851d2` 本地fallback；不声称系统档案绑定。实际第三份系统档案是York |
| 既有原串文成员与逐条可见性事实 | Birdtie原生 `map-content/moments/{id}/thread` 与现reader；不从作者、相邻地点或发布时间推断 | 复用现 moment_threads 权威顺序与 map ACL；不可读段保留位置，不包含原ID/正文/媒体。无地图位置的真实兄弟段也可按当前权限读取 |

图像是本次明确选定的已有系统/包素材，保留来源、原版本、摘要和尺寸；未引入新第三方素材或AI替代图。业务实现为Birdtie原生代码，不复制许可不明的旧业务源码；未扩大历史私人媒体权限。远端新版或非法远端metadata不能被旧本地图静默覆盖。原始档案不可变，派生缓存与客户端已读媒体缓存均有界，并按当前身份/来源/期限退休。

初次合批编译阶段仅准备迁移177。用户随后要求安装、部署与上传GitHub；已核对真实live两城同run映射和系统原图，新增受限备份、保留原176条迁移摘要，事务执行177并启用API33。两城公网324px派生摘要与APK一致；原系统档案与Civu均无写入。完整三城原版本、源ID、目标ID、派生SHA和证据见 `docs/reports/BIRDTIE-NOW-UNIFIED-INTEGRATION-2026-10-17.md` 与 `docs/testing/evidence/now-unified-20261017/artwork-r15/`。代码、编译、装机和真实验收分别记账，不能由旧APK40/42推定本轮通过。

原字节完整本机来源目录：C:/Users/chens/Documents/Codex/2026-10-14/birdtie-hangzhou/civu-private-reference-images-20261015-02/。选定源文件分别为 d2988c88b86453e1ead0c637ffeb85abdcfd89c9ce3894d12351a609b4429988.blob 与 69b9d3d653f3b7f64fd1423c54e18d73cb90f1186a824bb1cfe1319a835a4218.blob；对应去向为上表两份324px PNG，非复制整个受限导出。


## 2026-10-17 用户反馈：常用目的地与错误城市投影

只读复核Civu-client/lib/src/widgets/civu_destination_picker.dart:357–398、featured_destinations.dart和destination_souvenirs.dart后，沿用“上海/阿伯丁＋真实访问/近期、少量有图目的地”的入口形式，移除空查询对全1067项的网格枚举；全球联网搜索不受精选或分类限制。不迁旧provider快照、坐标或demo fallback。

明确选定原图来源 `D:/Program/Civu/Civu-client/assets/destinations/{shanghai,aberdeen,beijing}.png` → `apps/client/assets/destinations/`同名文件，逐字节一致；真实ID与country/source reference经当前公共catalog核实，详见artwork-r15/feedback-receipt.json。原图和Civu仓均只读，remote新版优先规则保留。

8项IS数字标签经受限原source SHA与10个公开OSM地点city/address字段核实为地址片段误分类，非本轮合成fixture。178仅新增精确审查排除，ListCities过滤当前仍匹配的审查事实，GetCity/ListPlaces与业务实体不变；保护10个Place和1条Moment，不能按数字名清空真实数据。API34已启用该投影隔离；45号已在用户重连后实际覆盖安装；同版本常用图卡、正反移动的静止预览几何与当前单条地点展开已核实，详见feedback/install03/ui-receipt.json。44号旧失败与45号首次断开保留；多账号串文/性能/历史空图仍未验收。

## 2026-10-18 目的地分类完整展示与中文归属

用户本轮要求国内/境外显示此前已有图标和名称，并纠正CN/中国、Scotland/苏格兰混用；这覆盖上一节最多8项精选的展示决定。只读核查 `D:/Program/Civu/Civu-client/assets/destinations/README.md`（REQ-301/303/304用户获准生成/用户附件来源）、`destination_souvenirs.dart` 与当前Birdtie1059目录。没有复制旧名称猜绑定、provider结果、坐标、Session、Riverpod或业务权限代码。

明确选定新增原PNG来源 `D:/Program/Civu/Civu-client/assets/destinations/` → `D:/Project/birdtie/apps/client/assets/destinations/`，下列每个同名文件逐字节复制并核对SHA256：`chengdu.png`、`chongqing.png`、`guangzhou.png`、`hangzhou.png`、`sanya.png`、`xiamen.png`、`xian.png`、`glasgow.png`、`inverness.png`、`london.png`、`oban.png`、`york.png`（共3602393B）；以及 `dali.png`、`skye.png`、`islay.png`、`iceland.png`、`lisbon.png`（共2576982B）。Civu源及README保持只读；既有6份Birdtie图不替换，不重新生成、裁图或引入第三方素材/依赖。

前12份加既有6份，使用当前真实ID、国家与sourceRef独立核实的18个目录投影（国内11、境外7）；只改中文展示字段与搜索别名，原名称/region/身份不变。上海显示“中国”、青岛/济南“中国 · 山东”、阿伯丁/爱丁堡“英国 · 苏格兰”等；分类在完整展示集合上执行，全球搜索仍独立，不再先截8再过滤。远端未知新图版本不回退旧本地图。

另5份原图没有合适的当前City身份：大理旧为532901区县而当前只有大理州；天空岛/艾雷岛原为travel_region；冰岛原为country；里斯本原为manual-city且当前目录无PT。这5项以明确“搜索”的已有图卡进入原全球搜索和用户显式候选选择，不制造PublicCity/ID，不把旧坐标或国家/旅行区域绑到别的城市。合计23个已有图标入口，国内12、境外11；18个目录入口与5个搜索入口的行为由中文动作说明。

来源、逐图摘要、18条实际目录绑定及直接检查保存在 `docs/testing/evidence/now-unified-20261017/feedback/performance53/dest/receipt.json` 和 `search-shortcut-assets.json`；实现位于 `city/destination_{presentation,shortlist,artwork,picker_grid,search_shortcuts}.dart` 与原Now目的地入口。复制与单元通过不等于新包实机验收；构建、装机及性能结果由本批总报告另记，原发布门槛保留。
