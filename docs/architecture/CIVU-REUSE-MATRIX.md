# Civu 只读现状审计与 Birdtie 复用清单

Birdtie 当前身份和 Agent 权限边界以 [Accepted Agent Identity and Ownership Model](AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md) 为准；早期复用表中平行 City Agent 的表述已被 shared Agent Runtime + CityContext 设计替代。

2026-09-30 Birdtie V2 补充：Agent-first 首页复用的是本仓已有的 `PublicCityMapView`、原生 Mapbox adapter、`PublicCityController` 和 City API。新增 Entity Layer、Agent Workspace 状态、规则查询及任务历史均为 Birdtie 自有实现；没有从 Civu 仓库复制代码或引入新的 Civu 运行时依赖。已审核 Activity 仅通过关联的公开点精度 Place 提供地图坐标；客户端不推测私有位置。未配置 API 时的演示 People、Group、Activity 地图实体与真实 API 模型隔离。

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
