# Birdtie 按需媒体与城市入口接续 — 2026-10-16

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


API30/schema173 已部署，APK34 已安装，Web34 已部署。图片按需阅读已完成目标验证；随后补上的阅读取消/续期与国内高德修复在候选35中，尚未安装部署。本报告保留实际失败和边界，不将编译或页面截图视为完整链路通过。

沿 UX-CHECK-01/02/04/06/08/10/12/14/16 的当前权威交互规则实施；正常字号、不录屏，保留原Now面板与地图。

## 改动路径

`apps/api/internal/content/map_content.go`、`internal/httpapi/map_content.go`/`map_content_display.go`、`internal/postgres/map_content.go`/`map_content_media.go`/`map_content_display.go`：旧详情默认 eager 合同不变，新客户端明确 `mediaMode=lazy`，原图片元数据带确切 source 绑定的 descriptor；仅选择某页后生成/取阅读副本，最长边1280、最多256KiB，原图不变。详情、descriptor、图片字节分别检查当前身份、权限与原来源。派生缓存最多32项/8MiB、元数据4096项、并发解码2。

`apps/client/lib/src/workspace/owned_map_api.dart`、`owned_map_controller.dart`、`now_map_content_panel.dart`：沿当前媒体页取 descriptor 与字节；翻页、关闭、来源变化退休迟到响应。显式播放视频先重读同一来源的当前详情，将新租期写入面板后再加载；旧请求不能覆盖新详情。邻条目过期不会重建仍获准播放的条目。没有自动播放或自动公开。

`apps/api/migrations/173_reviewed_city_camera_viewports.sql`：曾为上海/汕头补公开 WGS 城市相机，已随 API30 执行。这只是浏览相机，不是地点坐标。其 Mapbox provider 已被用户后续国内要求覆盖，新增174改为高德，尚未生产执行；详见国内地图报告。

## 直接验证与实际性能

后端10项独立顶层测试通过（含173）；原生测试首次 fixture 缺 link id 的失败保留，修正后再跑通过。阅读面板最终定向13项通过，分析器无 error/warning。历史重复执行不相加成独立测试数量。命令、cwd、退出码、源码摘要见 `docs/testing/evidence/content-map-lazy-display-20261016/`。

| 同一200张合成小图的真实 PostgreSQL 路径 | eager | lazy |
| --- | ---: | ---: |
| 冷详情及证明耗时 | 842.851ms | 166.670ms |
| 完整像素解码 | 200 | 0 |
| 阅读副本生成路径取完整图体 | 1,064,000B | 0B |
| 详情JSON | 120,453B | 162,453B |

每张 fixture 为240×320、5,320B；lazy 选择两页后解码0→1→2，耗时194.692/181.066ms。详情/地点/证明仍读取4,256,000B图片头数据（每项最多64KiB），并非零图片I/O；JSON因descriptor路径而增大。这不是200张原分辨率照片的手机性能结论。

线上同一真实图片：详情4,514B/60.006ms，descriptor949B/1,278.829ms，阅读图54,903B、960×1280/57.794ms；原件1,519,753B、5760×7680且摘要不变。错误来源/动态、匿名及退出登录后读取被拒绝。缓存状态未控制，不能称冷启动测量。

APK34 真机实际四图动态只查看前两张，HTTP 记录只有对应2个 descriptor 和2个 display-image 请求；其他鉴权/地图续期请求仍存在。作者与原发布时间可见。阅读、返回、键盘、选城市期间 mapCreations 保持1。实际截图在交付目录 `BirdTie-APK34-lazy-image1.png`、`BirdTie-APK34-lazy-image2.png`；截图属于34，不是候选35或高德验证。

Web34 实际汕头视频播放15.079→25.832秒；上海49,250,278B视频第一次未出现播放器，第二次明确重试后播放20.666→40.991秒，返回保留同DOM地图。第一次失败已保留，整体状态PARTIAL。后续客户端已修过期租期/迟到响应，但真实同一视频首次播放复验尚未完成，不能把旧失败唯一归因为租期。

## 交付界限

API30在 `/opt/birdtie/releases/social173-20261016-build30`，API SHA256 `4cc70262b66cffa0061a79fa91fdc7f622de23aedd5552a713b5e1070a7b9900`；已装APK34 SHA256 `d3e150c3f496a51e8ebe5701e9a3d6996204385c3c5170ebe46c424fc8e22ff7`。Web34在 `/opt/birdtie/web/releases/public-20261016-final-web34`。线上与已装包均不是候选35。

未执行：候选35真机/真实网页、高德真实对齐、上海汕头Android视频、第三条上海Web视频、200原分辨率照片手机全链路、完整最终发布/串文回归、严格同相机性能和跨设备撤权。均为NOT_RUN；CIVU原库/项目、用户内容未写，测试会话退出，手机HTTP观察与ADB转发已清理，不录屏，正常字号。父任务继续IN_PROGRESS。

## 后续实际交付更新：APK40 / API31 / schema175

此前候选包/ADB离线记录为历史快照。当前40已替换安装，新高德Key和原Civu两份样式已在上海真机验证；直辖市城市尺度修复已入包。真实Civu owner会话、Aberdeen内容/图片/作者原时间、超过30秒后台后的地图恢复均已执行。巴黎外部目的地定位为APK39实际证据，40未重复。地图平移封面复用与性能限制、数据清理/隔离及社交缺口统一见 `docs/reports/BIRDTIE-DGS-EXECUTION-2026-10-16.md`；完整验收尚未完成，不能把旧NOT_RUN统称已通过。网页仍延期。
