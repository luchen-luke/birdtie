# 栖游/Civu 到 Birdtie 的产品升级与切流计划

日期：2026-09-30  
状态：产品方向已确认；迁移设计与本地 Android 预览已开始；生产数据、身份、签名与线上切流均未执行。

## 已确认的产品与应用身份

Birdtie 是栖游的下一版产品，在 `D:\Project\birdtie` 按 Agent-native local social network 的新模型建设。Civu 是只读参考源。现有用户、数据、接口和线上服务需要逐步映射到 Birdtie；最终不再运行旧栖游产品，但不得在迁移、兼容与回滚验证前停用旧服务。

正式更新沿用 Android 包名 `app.civu.civu_mobile` 和 iOS Bundle ID `app.civu.civuMobile`，继续使用现有商店应用记录。Android debug 采用 `app.civu.civu_mobile.birdtiepreview`，可与手机上的 Civu 并存；这不是最终发布包。覆盖安装仍须 Android 原发布签名/Play App Signing 兼容的上传身份、递增的 versionCode，以及 iOS 原应用签名与描述文件配置。当前手机上已安装的 Civu 是 versionCode **2038**；发布时还要核验商店中所有轨道的最高 versionCode。iOS 现有应用更新仍需提交 Apple 审核，沿用 Bundle ID 不免审。

发布规则以 [Google Play 应用更新说明](https://support.google.com/googleplay/android-developer/answer/9859350?hl=en)、[Apple App Review](https://developer.apple.com/app-store/review/) 和 [Apple 提交审核流程](https://developer.apple.com/help/app-store-connect/manage-submissions-to-app-review/submit-an-app) 为准。

现有安装的本地数据库、缓存、草稿、下载和权限状态可能随覆盖更新保留。新客户端不能假定旧本地 schema 可读；必须先盘点、备份并设计一次性升级或安全忽略策略，尤其保护未同步草稿。推送、Deep Link、Associated Domains、OAuth 回调及高德/Mapbox 平台绑定也需逐项核对。

## 已核实的基线

| 项目 | 事实 | 对切流的影响 |
|---|---|
| Civu 服务路由 | `D:\Program\Civu\Civu-server\api\internal\httpapi\router.go` 在 revision `0489536` 有 375 个 `mux.HandleFunc` 注册项；逐条快照见 [`CIVU-HTTP-ROUTES.csv`](CIVU-HTTP-ROUTES.csv)。该数只覆盖这个文件。 | 不可把 32 个 Birdtie 路由视为旧 API 的替代品。先找调用者、数据依赖和兼容期限。 |
| Birdtie 服务路由 | `apps/api/internal/httpapi/server.go` 当前有 32 个注册项，主要覆盖公开 City/Place/Activity、私有 Moment 草稿、Consent/Profile、City Seed 审核和通用 OIDC 骨架。 | 尚无旧登录方式、旧 Journey/Note/Media 等接口的完整兼容层。 |
| Civu 登录 | 路由包含邮箱、手机号、微信、refresh/logout；Birdtie 只有待配置的通用 OIDC 流程。 | 需保证旧用户可恢复同一账户，避免覆盖更新后被当作新用户。 |
| 地图 | Mapbox 旧公开 token + 定制样式已在 Android 预览上加载；Web token 缺少 `styles:tiles`。Civu Android 高德 Key 已存于 Birdtie 忽略文件。 | 最终高德 Android Key 只有在原包名及注册的发布签名一致时可复用；预览包需单独 Key。 |
| 生产数据 | 本次只读审计未连接、导出或修改 Civu 生产库/对象存储。 | 任何数量、质量、权属和迁移时长都须在获授权的生产盘点后实测。 |

## 路由与客户端兼容工作簿

`CIVU-HTTP-ROUTES.csv` 是原服务路由快照，初始 `disposition=untriaged`；不能因路由名相似就认定 Birdtie 实现兼容。按下面顺序为**每一条**补充调用方、所有者、Birdtie 对象/接口、读写副作用、迁移策略、灰度指标和停用时间。Civu 手机客户端及管理后台的网络调用也要扫描，避免漏掉动态路由、WebSocket、上传直链和第三方回调。

| 路由域（当前文件的主要组） | 初步目标 | 最先要核实的风险 |
|---|---|---|
| `auth`、`me`、`users` | 身份桥接、账户映射、个人资料迁移；旧会话在过渡期继续由原服务验证或安全换票。 | 手机号/微信/邮箱与 OIDC subject 的绑定、重复账户、注销、封禁与 Consent 版本。 |
| `places`、地点解析/目录、`place-events` | 映射为 City/Place/Activity，保留旧 ID 到新 canonical ID 的映射表。 | 坐标系、来源许可、去重、权限、公开精度、时区、维护责任。 |
| `media`、`notes`、Moment 相关 | 先迁移对象元数据与所有权，再迁移二进制与派生版本；旧 URL/ID 设过渡兼容。 | 原件/EXIF、私有访问、对象存储签名、删除/撤权、未完成上传。 |
| `journeys`、`theme-maps`、`actions` | 按 Birdtie Journey/Activity/Intent 语义逐类映射；无语义对应者保留只读历史或经用户确认转换。 | 成员授权、位置共享、发布状态、历史路线与协作权限不可静默改变。 |
| `admin`、通知、实时、AI、推荐与其他 | 将仍有调用者的管理/通知能力接入新治理流程；Agent/推荐逻辑重新实现。 | 管理操作审计、推送 token、消息投递、工具权限、旧任务/回调未结束。 |

## 执行顺序与每阶段出口

1. **锁定清单**：记录 Civu 服务/客户端 revision、当前线上版本、所有商店轨道、域名、部署、数据库、对象存储、后台任务、Webhook、Push 和第三方账户。对 375 条路由与其他注册点标记真实调用量及客户端版本。出口：每个活跃调用均有所有者与继续服务路径。
2. **身份先行**：保留不可变 `civu_user_id -> birdtie_account_id` 映射；定义手机号/微信/邮箱到通用 OIDC 的验证与绑定流程，处理重复、封禁、注销和丢失凭据。旧 refresh token 不直接变成 Birdtie session。出口：老用户从覆盖更新进入原账户，且不能索取他人内容。
3. **Foundation/City Graph 迁移**：按 User/City/Place/Media → Moment/Activity/Journey/Intent 建可重跑 ETL。每类先盘点权属、字段、CRS、隐私、许可，再做映射、去重、引用重连和来源记录。使用稳定旧 ID 对照表，增量同步期间确保幂等。出口：数量、哈希/外键、权限和抽样对象逐项对账；未映射对象列出处理方式。
4. **接口兼容与新客户端**：在明确的 API 网关/兼容层按路由或客户端版本切流。旧客户端继续调用可用的旧接口；新客户端只使用 Birdtie contract。涉及登录、上传、删除和支付/消息等写入的路由，先设计单写所有者、幂等和失败补偿，禁止两个系统同时成为权威写入点。出口：新旧客户端核心路径和回滚路径均可运行。
5. **影子读、灰度和可观测性**：先对非敏感公开对象做对照读，再在经过授权的环境中比较账户/私有内容投影；按内部用户、灰度人群和版本逐步切流。记录错误率、映射缺失、延迟、授权拒绝、上传失败和回滚演练。出口：指标满足事先确定的阈值，且能退回旧服务而不丢写入。
6. **发布覆盖更新**：用原商店身份和正确证书签名，Android versionCode 高于所有已发布轨道；iOS 提交新版本审核。核对设备本地数据升级、推送/Deep Link、地图 SDK Key 和商店合规。出口：真实设备从现有 Civu 安装覆盖升级并保留账户/内容；Android 和 iOS 分别验收。
7. **退役旧服务**：确认活跃旧客户端低于约定阈值或仍有兼容代理，所有数据/媒体已对账、后台任务已迁移、法律保留/删除义务有承接、告警与回滚窗口结束，再按域/路由逐步关闭。最后撤销不再使用的旧 Key、Webhook 和机器凭据。出口：没有实际调用者或未迁移数据依赖。

## 当前可继续的开发与外部依赖

- 可继续 Birdtie 自有模型、公开 City Graph、权限边界和预览版开发；Civu 保持只读，生产流量不变。
- 需要获得原 Android 发布签名/Play App Signing 信息、iOS Team/证书/Provisioning 与商店角色；当前仓库无可用发布签名配置。不得用 debug 签名构建覆盖正式包。
- Android release 构建现在要求 `BIRDTIE_ANDROID_STORE_FILE`、`BIRDTIE_ANDROID_KEY_ALIAS`、`BIRDTIE_ANDROID_STORE_PASSWORD`、`BIRDTIE_ANDROID_KEY_PASSWORD` 环境变量，Keystore 文件存在，且 `pubspec.yaml` 的 build number 大于已装 Civu 的 2038。这里的 2038 只是设备基线；发布前仍须核对 Play 所有轨道最新版本，并用原证书指纹复核签名。密钥与密码不进入仓库。
- 需要指定 OIDC 提供方，并完成旧身份绑定设计；提供方尚未选定，通用 OIDC 边界可先推进。
- 媒体存储服务/区域尚未选定，可先完成可替换存储契约与隐私处理。
- 高德原生地图、Web JS Key/安全代理、iOS Key 找回与 Mapbox Web `styles:tiles` token 尚待完成。配置步骤见 [`MAP-PROVIDER-CONFIGURATION.md`](../architecture/MAP-PROVIDER-CONFIGURATION.md)。

## 本地 Android 预览记录

2026-09-30：设备 `c641566b`（Xiaomi，Android 16）上已有 `app.civu.civu_mobile` versionCode 2038。Birdtie debug 构建为 `app.civu.civu_mobile.birdtiepreview` versionCode 1；`adb install --no-streaming -r` 成功，两包并存。通过 `adb reverse tcp:8080 tcp:48523` 连接本机 Birdtie API，从本地数据库读取 Aberdeen；Explore Map 展示 Civu 定制 Mapbox 样式。当前本地 City Seed 无已发布 Place，因此尚未验证 marker/点击；高德渲染、登录、上传、覆盖升级和 iOS 均未验证。此预览不构成迁移或发布验收。
