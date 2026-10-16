# Birdtie 国内外地图与坐标边界修复 — 2026-10-16

本轮已实现并编译国内高德、海外 Mapbox 的分流，尚未部署或安装这版国内地图。当前在线仍为 API30/schema173、Web34，手机仍为 APK34。174 迁移与候选 APK/Web35 已保存；高德预览包 Key、网页 JS Key 和安全代理缺配置，真实高德对齐验收为 **NOT_RUN**。父任务 BT-FIX-NOW-UI-001 保持 IN_PROGRESS。

30/34/35是本项目交付批次编号；客户端当前源码版本为0.1.0+1，预览包身份不变。精确产物以SHA256和源码构建回执区分，不把批次号当作商店versionCode。

适用交互规则：UX-CHECK-01/02/04（Now、已有上下文与同一内容入口）、06/08（来源未知、撤权和旧授权退休）、10/12（迟到响应隔离、返回与恢复）、14（按用户指定正常字号检查）、16（调试与证据不默认保存私人位置或媒体）。规则接线不表示这些场景已全部真机通过。

## 已核对的原因与边界

之前为上海、汕头补相机时，173 使用 Mapbox 补上入口。这不能作为国内地图方案完成的证据。本轮按 CIVU 的国家分流规则恢复：中国大陆 CN/CHN 使用高德，其余明确国家使用 Mapbox；未知国家保留已审核的配置。地图随所选城市决定，不随每次相机移动猜测国家。

只读核对此前实际访问的上海、汕头两个公开地点：源标记为 provider=amap、sourceCoordinateSystem=GCJ02、storedCoordinateSystem=WGS84；原 Civu 两轮反算重放误差小于 1e-6 度，Birdtie 保存值与原存储值完全一致。这两条没有“迁移错位或重复转换”的证据；没有据此推断全部中国地点正确，也没有平移数据库坐标。用户反馈的实际视觉偏差仍需真实高德底图确认。

Birdtie 数据库和 API 继续使用 WGS84。进入高德显示时转换为 GCJ02；SDK 返回的四角视口和明确选点转换回 WGS84 后查询或提交。纯坐标动态仍是独立动态锚点，不写成 POI、不继承设备位置。范围数值仅是转换算法包络，不作为国界判断。

## 实际路径与复用映射

| 实现路径 | 改动和来源 |
| --- | --- |
| `apps/client/lib/src/city/map_provider_policy.dart`、`public_city_controller.dart`、`public_city_map.dart` | 从现有 API 解析 countryCode，按明确城市选择供应商，接 Native/Web 适配器及同一阅读入口。CIVU `services/map_provider_policy.dart` 只读参考，不复制业务页面。 |
| `apps/client/lib/src/city/map_coordinate_boundary.dart`、`third_party/coordtransform.LICENSE`、`apps/client/LICENSE` | MIT coordtransform 固定提交的前向公式、输入检查与有界反算；修正转换包络边缘的迭代分支。许可证随源码和客户端产物保存。 |
| `apps/client/lib/src/city/amap_city_map_io.dart` | 新增原生高德适配：同地图常驻、已有标记布局与 12/4 封面预算、稳定 ID 增量更新、真实 DPR 投影、当前权限/来源检查、四角视口、180ms 静止回调、显式选点和原阅读面板。SDK 启用前展示实际服务说明并等待明确同意。 |
| `apps/client/packages/amap_map`、`pubspec.yaml`、`pubspec.lock` | 只迁入 CIVU 已用的 Apache-2.0 高德插件及其 Android16 MapView 修补，保留 LICENSE/NOTICE/来源哈希。不是复制 CIVU 整页或导航。关闭原生调试日志，并补 controller 订阅、channel 与迟到初始化的销毁边界。 |
| `apps/client/lib/src/city/amap_city_map_web.dart`、`amap_city_map_stub.dart`、`apps/client/web/birdtie_amap.js` | Web 接真实 SDK 四角视口、owned 内容点击和增量节点；官方前向转换结果最多缓存 320 项、每批 40、最多 2 并发。手势使旧定位请求失效，防止转换迟到后回拉相机。 |
| `apps/client/lib/src/workspace/map_marker_visual.dart`、`owned_map_media_controller.dart` | 将实际 grant/pin 期限传给地图像素副本，过期及时退休；缓存不能代替当前权限。 |
| `apps/api/migrations/174_mainland_city_map_provider.sql` | 只将完全匹配 173 写入身份和字段的上海、汕头 provider 改为 amap；原 WGS 中心、其他城市和所有内容坐标不变。尚未生产执行。 |

CIVU 客户端基线 `1bbf0613b513001447f326225c944c1a1fbd8e8f`，服务器基线 `0489536a0eb0792deff7793c8b143ad3e741d5ce`。源项目/库未写入。插件逐文件映射见 `docs/testing/evidence/china-map-boundary-20261016/native/plugin-provenance/source-map.json`，Web 映射见同目录 `web/REUSE-MAPPING.md`。源码阅读不等于 CIVU 运行验收。

## 实际验证与失败保留

| 范围 | 实际结果 |
| --- | --- |
| 城市 provider、Web 视口/媒体边界 | 原 14 JS 与 9 Dart/widget 通过；后续旧定位竞态新增 1 项先 FAIL、修复后 PASS。未重复全部矩阵。 |
| 坐标与像素租期 | 第一批 6 项通过；反算边缘修复后只重跑坐标 4 项通过。独立固定公开坐标样本，不是官方精度或真实底图校准。 |
| 174 PostgreSQL 迁移 | 1 项真实隔离子库测试通过，覆盖 exact2 更新、防误匹配、其他字段保留和重复执行；未写生产。 |
| 原生接线 | 定向 analyzer 无 error/warning；新增1项真实Flutter widget配mock通道的生命周期检查PASS，覆盖9订阅取消、handler移除、幂等销毁和迟到init。日志见 `native/receipt.json`；不是 SDK 真机验证。 |
| 依赖解析 | 离线依赖解析完成，但 Flutter Windows symlink 后置步骤 exit1，原日志保留。未修改操作系统设置；候选编译使用 --no-pub。 |
| 首次 APK 编译 | FAIL：插件 compileSdk35 不满足当前 flutter_plugin_android_lifecycle 所需36。只对齐插件 compileSdk36 后重建，原日志在 candidate-apk35 保留。 |
| 候选编译 | APK35、Web35 编译通过且构建前后源码一致；含原生插件全部资源哈希。缺高德配置，没有安装或部署候选包。 |
| 高德官方转换服务核对 | 仅发送两个公开城市点；既有 Web Service Key 返回 USER_KEY_RECYCLED/10013。未用私人地点试探。不能据此判断 Android Key 有效性。 |
| 高德真实地标对齐、内容点击、往返、性能 A/B | NOT_RUN。Web fake SDK、Dart 测试和编译不能替代这些验证。 |

原地图性能预算与覆盖缓存继续复用。新高德适配已在直接检查中证明同范围未变节点复用、转换缓存复用和有界排队；真实高德请求数、帧耗时及同相机前后性能仍 **NOT_RUN**，不提供推算的百分比。

## 接续只缺的配置

Android 预览包：`app.civu.civu_mobile.birdtiepreview`，APK34 实际签名 SHA1 `73:84:0A:17:D0:CA:7B:4B:F4:15:41:B4:2C:EC:B7:A2:34:D5:05:23`。需要与该包名/签名匹配的 Android SDK Key，放入忽略提交的构建文件 `BIRDTIE_AMAP_ANDROID_KEY`；清单 `android/amap.properties` 的 `android.key` 也应匹配。当前本机清单仍是 Civu 正式包的 Key，不能推定适用预览包。

Web 需要忽略提交的 `BIRDTIE_AMAP_WEB_PUBLIC_KEY` 和 `BIRDTIE_AMAP_SERVICE_HOST`。安全密钥只留服务端代理，不放到客户端。现有 Web Service Key 是另一平台且已失效，不能代替 JS Key。已向用户询问配置文件路径或已登录控制台；没有重复请求部署或手机操作权限。

拿到配置后，合并 174 与配置包部署，验证上海/汕头地点和动态位置、阿伯丁回归、视频首次播放及返回地图。完整发布回归、iOS、CM09 严格同相机性能与跨设备撤权仍未执行；不把本批关闭为整个任务完成。

候选 APK35 SHA256：`b9b5f6b6881e290ecd4aa00ba33a5d55f687a0eabd3989ae6d981982712dec40`；Web35 文件清单 SHA256：`e3595059ac934b716ef439c11c33e981327851ce7683415c86be0d092514d81a`。这些候选产物未运行。
