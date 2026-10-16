# Birdtie 按需媒体与城市入口接续 — 2026-10-16

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
