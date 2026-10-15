# Civu 只读现状审计与 Birdtie 复用清单

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
