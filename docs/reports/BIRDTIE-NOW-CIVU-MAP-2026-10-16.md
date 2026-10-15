# Birdtie Now 地图内容修复 — 2026-10-16

状态：PASS_NOW_MAP_AND_READER_SCOPE。API23/schema170、Web24、APK24 已实际部署安装，手机地图及关联动态阅读通过。本报告不将父任务和所有发布流程标为完成。GitHub 精确提交号另存交付回执。

## 为什么原来只有底图

这是实际接线缺陷。旧数据已进入 Birdtie 独立库，但 5,492 个地点均为草稿，83 条存续旧动态也被隔离为私人草稿；旧 PUBLIC 地图筛选排除了它们。原备份能证明这 83 条动态原本为 public/ok，阿伯丁的 366 个地点原本 active。此前把“已迁入”当成“Now 已可见”，遗漏了真实浏览链路。

另一处遗漏是坐标来源：Civu 历史提交 4438472 的地点提升过程逐值传递 OSM 坐标，但只保留 promotedFrom/sourcePlaceId。只检查 storedCoordinateSystem 会漏掉这些有完整来源证明的坐标。本次沿原始来源核实，不按国家或城市猜坐标。

## 实际数据与读取

170 来源证明账本已在独立 Birdtie 数据库真实提交。原项目、原库、原备份零写入，原内容 ID 与旧迁移回执保留。

| 项目 | 实际结果 |
| --- | --- |
| 原公开地点事实 | 5,492 |
| 有确切坐标证明的地点 | 3,724 |
| 原公开存续动态 | 83 |
| 缺少坐标证明，继续保留但不上地图 | 1,768 |
| 合并地点来源别名 | 19，沿原合并关系处理 |
| 阿伯丁及周边验证范围 [-3,56,-1,58] | 384 个可见地点、8 条动态地点关联、6 个含动态地点、6 份封面 |

上述 384 是指定阿伯丁及周边范围结果，不等于行政城市内的 366 个地点。手机相机视野内实际数量由范围决定。

新 MAP_CONTENT 投影同时支持原公开目录、原公开动态、本人私人内容和 Birdtie 当前公开内容。原公开来源证明与当前账号、屏蔽、内容版本、删除/撤回及授权共同决定读取；原私人内容仍只归原账号。关联地点不会扩大正文可见性。图片经同源鉴权读取、字节摘要和尺寸校验后仅缓存在内存，不把私人 URL 或令牌交给地图供应商。

## 界面与真实缺陷修复

Now 使用地点圆形照片或类别图标、右侧名称和实际动态数。密集位置保留原点位，以较小圆点减少遮挡；不移动经纬度、不制造数字聚合。点击地点先在首页预览，用户展开后在 Now 主面板阅读正文与媒体，编辑仍走既有统一条目编辑器。

真机发现并修复：

- 地图读取续期过晚，原 30 秒读取期限到时会让待完成请求失效；现在提前续读，权限到期仍清除。
- 数据库查询被 JIT 编译拖慢；只对地图读取事务关闭 JIT，没有改全库配置。实际同范围 API 读取从约 6.6 秒降至 760 毫秒。
- Android 地图注解更新照片后又回写旧自动图标名，覆盖新照片；更新时清除旧图标名。
- 地点选择弹层在首帧没有接续 Now 读取状态，导致刚打开就失效；打开前完成可见性交接，覆盖或身份变化仍撤销读取。
- 原生注解的透明画布可命中相邻地点；改按真实触点的 48×48 dp 范围命中，并按离触点距离排序。真实手机 Causeway View 点击得到正确首项。
- 预览展开到另一父容器时内部状态丢失，第一次点击仍停在缩略预览；保留同一面板 State 后，一次点击进入完整正文和媒体。APK23 的失败及第二次点击诊断保留，最终由 APK24 重验。
- 启动网络失败后登录服务状态无法恢复；增加公开配置重试，并让 Settings 沿统一登录面板进入固定验证码登录。不会自动登录或发送验证码。

## Civu 源码对应

Civu-client 本地及 GitHub master 为 `1bbf0613b513001447f326225c944c1a1fbd8e8f`，只读核对。旧截图只作视觉参照，没有当作本轮运行证据。没有直接复制整页、旧路由、全局状态或无明确许可的业务源码。

| Civu 来源 | Birdtie 实施 |
| --- | --- |
| explore_map_marker_layout / explore_map_marker_icon | map_marker_visual / bitmap / widget：照片、类别、名称、触区 |
| civu_explore_map_mapbox 的封面升级行为 | owned_map_media_controller：有界、并发受限、当前权限图片 |
| place_detail_screen / loadPlaceContent | map-content place moments API + now_map_content_panel：当前地点关联内容 |
| OSM 地点提升历史链 | legacymap + 170 账本：确切来源坐标及可见性证明 |
| 原发布与串文能力 | 沿用已交付的 Now 统一编辑器；本次不复制旧页面或另建写接口 |

详细来源核对：`docs/testing/evidence/now-civu-map-20261016/civu-map-source-audit01.md`；行为与边界：`docs/product/BIRDTIE-NOW-CIVU-MAP-2026-10-16.md`。

主要改动包括 API 的 legacymap、map_content、170 migration、路由与启动校验；客户端 owned_map_api/controller/media_controller、Now 地图与阅读面板、地图标记和点选、登录恢复；部署脚本、相关直接测试和本批证据。完整改动路径随 Git 提交及交付清单提供。

## 验证范围

本批按实际改动执行定向验证，未重复全仓测试。结果以各次 source frame 和原始日志为准，不把重复修复轮次累加成新增覆盖：

- 坐标来源、地图范围/来源类型、HTTP 参数边界直接 Go 测试通过。
- 真实 PostgreSQL 复合链路通过：当前权限、原公开来源、媒体、撤权、游标和事务 JIT 设置。
- 客户端严格解析、迟到/到期清除、图片缓存与慢请求续读直接测试通过；地图接线 2 项、视觉/面板 16 项、登录恢复 2 项、Settings 1 项、弹层交接 1 项通过。
- 原生实际触点 2 项直接测试通过；真实 Now 展开回归测试修复前失败、修复后 1 项通过；相关定向分析无 ERROR/WARNING。未把重复通过次数累计成新增用例。
- 远端真实 API：2 个原公开动态详情、4 份媒体共 4,549,593 字节摘要匹配；匿名地图/详情、登出后媒体访问均拒绝。所有创建的验证会话退出；一次网关更新中断留下的测试会话已精确撤销，其余会话未改。
- APK24 真机：地点照片与动态数、Causeway View 正确候选、一次展开、旧动态完整媒体浏览、返回 Now 与跨 30 秒续期实际通过。详细动作、时间及截图摘要见 device-final24/receipt.json；仅对回执列出的动作作通过结论。

保留首轮 native/fixture 测试失败、APK20/21 的显示失败、API 请求中断和会话清理记录。源码阅读、合成 fixture、构建、部署、真机阅读分别记录。


## 最终构建、设备与交付

- API：`/opt/birdtie/releases/social170-20261015-build23`，二进制 SHA256 `0421a2ee24491bfd94aea5fc6b85fed456e4195425133b8b5f51c1d3c69f736b`。
- Web：`/opt/birdtie/web/releases/public-20261015-final-web24`，清单 SHA256 `64e3eae00bdae9d80032e96e5d26497c8b92e9eb87e78f4dae76668663eb5b10`。
- APK24：SHA256 `aeae427da6d87038794f79016a2defd0a60335ef3c8efd7b05d09338fe6f2257`，已覆盖安装到本次用户授权的 Android 手机。
- 源码 frame 与对应构建前后完全匹配；既有 SQL 和源库保护记录保留。
- 固定验证码 123456 保持。本次手机使用已恢复的既有会话；没有把本地开发账号认证说成生产短信身份验证。
- APK21 曾有短暂网络失败，同 UID HTTPS 请求及后续 Flutter 请求成功，但初次失败根因未确定，记为 NOT_DETERMINED；不据此宣称 TLS/网络根因已经修复。

本地真机截图放在交付目录，不向 Git 上传图片正文、原备份、令牌或账号资料。Git 保存必要源码、脱敏回执、日志摘要及失败历史。

## 边界

NOT_RUN：本次全仓回归、iOS、网页版完整交互、所有城市/所有旧账号逐项浏览、最终新包完整发布/视频重跑、真机断网后登录配置重试。已有 APK19 统一编辑器真实发布/编辑/排序/删除证据保留，不算新包重跑。

1,768 个没有可证明坐标的地点继续保留，不伪造位置。城市选择和图层菜单相似的首帧重读风险仅有源码发现，未在本次扩大修复及验证；原任务继续记录。父任务及全站交付门槛不因本地图范围修复而全部关闭。正常字号、不录屏、中英混合语音延期继续遵循用户要求。


## 改动文件（源码、测试、文档与任务记录）

- `apps/api/cmd/legacy-map-restore/main.go`
- `apps/api/internal/content/map_content.go`
- `apps/api/internal/content/map_content_test.go`
- `apps/api/internal/httpapi/map_content.go`
- `apps/api/internal/httpapi/map_content_test.go`
- `apps/api/internal/httpapi/server.go`
- `apps/api/internal/legacymap/build.go`
- `apps/api/internal/legacymap/build_test.go`
- `apps/api/internal/legacymap/model.go`
- `apps/api/internal/postgres/legacy_map_restore.go`
- `apps/api/internal/postgres/map_content.go`
- `apps/api/internal/postgres/map_content_media.go`
- `apps/api/internal/postgres/map_content_native_test.go`
- `apps/api/migrations/170_legacy_map_visibility.down.sql`
- `apps/api/migrations/170_legacy_map_visibility.sql`
- `apps/api/native_database_startup.go`
- `apps/client/lib/src/auth/birdtie_auth_controller.dart`
- `apps/client/lib/src/auth/birdtie_login_sheet.dart`
- `apps/client/lib/src/city/map_cluster_members_sheet.dart`
- `apps/client/lib/src/city/map_point_selection_sheet.dart`
- `apps/client/lib/src/city/native_city_map_io.dart`
- `apps/client/lib/src/workspace/map_canvas.dart`
- `apps/client/lib/src/workspace/map_display_policy.dart`
- `apps/client/lib/src/workspace/map_entities.dart`
- `apps/client/lib/src/workspace/map_marker_bitmap.dart`
- `apps/client/lib/src/workspace/map_marker_visual.dart`
- `apps/client/lib/src/workspace/map_marker_widget.dart`
- `apps/client/lib/src/workspace/map_workspace.dart`
- `apps/client/lib/src/workspace/now_map_content_panel.dart`
- `apps/client/lib/src/workspace/owned_map_api.dart`
- `apps/client/lib/src/workspace/owned_map_controller.dart`
- `apps/client/lib/src/workspace/owned_map_media_controller.dart`
- `apps/client/lib/src/workspace/settings_page.dart`
- `apps/client/test/auth_configuration_recovery_test.dart`
- `apps/client/test/map_marker_bitmap_test.dart`
- `apps/client/test/now_map_content_integration_test.dart`
- `apps/client/test/now_map_content_test.dart`
- `apps/client/test/now_map_reader_handoff_test.dart`
- `apps/client/test/now_v2_map_display_policy_test.dart`
- `apps/client/test/now_v2_native_map_display_test.dart`
- `apps/client/test/owned_map_test.dart`
- `apps/client/test/settings_login_recovery_test.dart`
- `automation/codex_task_queue.json`
- `docs/architecture/CIVU-REUSE-MATRIX.md`
- `docs/product/BIRDTIE-NOW-CIVU-MAP-2026-10-16.md`
- `docs/reports/BIRDTIE-NOW-CIVU-MAP-2026-10-16.md`
- `scripts/deploy_birdtie_hangzhou.py`
