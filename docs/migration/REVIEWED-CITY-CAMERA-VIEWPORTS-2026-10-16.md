# 上海、汕头城市浏览视图补齐

本增量只补两个已存在城市的地图浏览相机。其余城市、动态、真实地点、原 ID、内容坐标、可见范围和发布规则保持既有状态。源实现和取证均未修改 Civu。

## 缺口与复用路径

已获准账号的四条可访问视频对应阿伯丁、上海和汕头三个城市。原阿伯丁已有 Mapbox 视图；上海的两条视频、汕头的一条视频对应城市均无 `map`。Web33 实际城市选择后停在“该城市尚未配置地图视图”，这是一项入口失败，不是内容不存在。先前记录保留在 `docs/testing/evidence/content-map-refinement-20261016/web33-acceptance/receipt.json`。

复用 `cities.map_*` → `internal/postgres/catalog.go` → `foundation.CityMap` → `PublicCityMap` → 当前 Native/Web Mapbox renderer → 当前已鉴权视口查询。无需增加表或启动 schema 契约，无需改客户端导航、地图引擎或内容权限接口。

Civu 只读来源：`D:/Program/Civu/Civu-server/api/internal/store/migrations/026_destination_area_model.sql` 和 `provider_place_admin_area.go` 的部分城市中心来自地点均值或候选地点；本次两个源城市虽然有中心，但无坐标系、可信边界和 `travelProfile` 视图来源，不能把它们升级为已核验城市中心。`Civu-client/lib/src/services/map_provider_policy.dart` 的中国优先 AMap 策略与 Birdtie 现状不等价：Birdtie Native AMap 仍为 stub，Web AMap 未接内容视口回调。本次按用户要求对两个已审阅城市使用现成 Mapbox/WGS84 renderer 的限定例外，未宣称 AMap 已实现，也未批量改变国家策略。

## 独立公开来源

使用 Natural Earth 官方仓库固定提交 `789c9904087846cc3361302857aa2e76b0ae71ff` 的 [10m populated places simple GeoJSON](https://raw.githubusercontent.com/nvkelso/natural-earth-vector/789c9904087846cc3361302857aa2e76b0ae71ff/geojson/ne_10m_populated_places_simple.geojson)。[数据主页](https://www.naturalearthdata.com/downloads/10m-cultural-vectors/10m-populated-places/)说明它是城市、城镇命名点数据；[许可](https://www.naturalearthdata.com/about/)为 public domain。

GeoJSON 声明 `urn:ogc:def:crs:OGC:1.3:CRS84`，顺序为经度、纬度；同提交的 [.prj](https://raw.githubusercontent.com/nvkelso/natural-earth-vector/789c9904087846cc3361302857aa2e76b0ae71ff/10m_cultural/ne_10m_populated_places_simple.prj)声明 WGS84。采用值仅作为城市浏览提示，不称为行政中心或行政范围，也不写入 Places。

| 城市 | Natural Earth 身份核对 | 纬度 | 经度 | 浏览缩放 |
| --- | --- | --- | --- | --- |
| 上海市 | Shanghai / CHN / Shanghai / Admin-1 capital | 31.218398 | 121.434559 | 11 |
| 汕头市 | Shantou / CHN / Guangdong / Populated place | 23.371952 | 116.668080 | 11 |

公开城市点分别落在[上海市政府公布范围](https://english.shanghai.gov.cn/en-Overview/20231209/705180b43f794c1ca43f4d1ddaa049a2.html)（东经120°51′–122°12′、北纬30°40′–31°53′，2025-09-10）与[汕头市政府公布范围](https://english.shantou.gov.cn/english/%EF%BB%BFoverview/environment/index.html)（东经116°14′40″–117°19′35″、北纬23°02′33″–23°38′50″，2026-06-16）内。范围只作城市匹配交叉检查；网站未声明其坐标系，不能据此作精密坐标变换。缩放 11 是 Birdtie 浏览选择，并非来源数据声称的边界。

实际下载校验：GeoJSON 4,932,147 bytes，SHA256 `fd3fa867a320cbd5c5b6bb5bc550afeec2939fb2cef688e508007282a55ac42f`；PRJ SHA256 `a02a27b1d1982c8516d83398e85a3c8b1aef1713c13ef4d84d7bde17430c07c4`。只保存两条公开记录及元信息，见 `docs/testing/evidence/content-map-refinement-20261016/city-viewports/public-source-verification.json`。未保存私人动态 ID、坐标、手机号或会话。

## 迁移与发布边界

`apps/api/migrations/173_reviewed_city_camera_viewports.sql` 只更新五个既有 `map_*` 字段。必须同时匹配已有稳定城市 ID、中文名称、`country_code=CN`、原 `civu:geo_area:*` 引用、目录来源标签和已发布状态，且五个地图字段全部为空。已配置城市不覆盖；同名不同 ID、错误国家、错误源引用、隐藏城市均跳过。它不改变 `content_status`、`verified_at`、来源更新时间或任何内容/权限事实。

发布前在明确的 Birdtie 独立库运行证据目录中的 `preflight.sql`，应见两个完全匹配、可补配置的城市。由根任务与 API30 统一执行迁移，保存原两行及 UPDATE 实际数量，并以新 `/v1/cities` 的两个 map 对象验明。若原数据已变化，先调查而非绕过匹配保护。重复执行应更新零行。本文件不自行触发生产迁移。

如需恢复，使用发布时保全的原两行地图字段，仅在当前五字段仍与本迁移写入值完全一致、原身份仍一致时恢复；不要运行宽泛城市清空或覆盖后续人工修正。纯数据补齐不需要下调数据库结构。

## 定向验证

`TestReviewedCityCameraViewportsNativeIdentityAndReplay` 使用既有显式授权、独立创建并清理的 PostgreSQL 子库和真实迁移，覆盖国家/名称/来源/发布状态/稳定 ID 防错、已有配置保留、两个城市真实 catalog 读取、重复执行零更新和业务表数量不变。与 lazy media 定向测试合批执行，PASS（2.80 秒、173 个真实迁移、源码前后相同、无 skip）；命令和退出码见 `docs/testing/evidence/content-map-lazy-display-20261016/backend/native02/receipt.json`。城市范围回执见本证据目录 `receipt.json`。生产只读 preflight 实测两个可更新目标、零身份不匹配、零既有配置，未创建会话或写生产数据。

适用交互要求为 UX-CHECK-01（城市入口）、02（复用已确认来源）、04（同一内容与动作）、06（不猜位置）、08（不扩大既有权限）、12（返回地图）。此迁移的源码阅读或 PostgreSQL 测试不代替 Native/Web 实际进入城市、读取原视频并返回地图；构建、远端迁移及真机结果由根任务在统一发布检查点另记，本 worker 未执行的部分为 NOT_RUN。


## 2026-10-16 后续国内供应商要求覆盖

上文为173当时的执行记录，173已随API30生产执行。上文“按用户要求”采用Mapbox限定例外的归因不准确：它是当时补入口的实施选择，未获得用户对国内Mapbox方案的验收。用户随后明确要求国内外分流并指出上海视觉偏差，该例外不再是后续实现方案。新增174只对这两条完全匹配的历史配置改provider=amap，保留WGS相机及内容坐标；已通过隔离PostgreSQL检查，尚未部署。Native/Web高德接线已编译为候选35，缺有效预览包/JS配置，真实对齐NOT_RUN。完整事实见 ../reports/BIRDTIE-CHINA-MAP-BOUNDARY-2026-10-16.md。历史173及失败记录保留。
