# BirdTie Now 内容地图增量交付 — 2026-10-16

本轮执行 `BirdTie-Now-Content-Map-Civu-Reuse-2026-10-16.md`。默认地图改为服务端按视口和当前观看者权限筛选内容，纯坐标动态独立上图，并在原 Now 面板读取准确条目。没有复制 CIVU 页面、导航、全局账户或地图引擎，没有将锚点写成 POI，没有回滚已有统一编辑器、多地点、媒体和旧数据适配修改。

状态按实际证据区分。CM-01～CM-08 的本轮实现与定向检查完成；CM-09 的 Android/Web 受影响链路和性能样本见下表，严格同相机前后性能、跨设备撤回推送及超大聚合真机连续分页仍 NOT_RUN。父任务保持 IN_PROGRESS，不把这次地图增量等同于所有历史任务完成。

## 版本与源码基线

| 对象 | 执行前 | 本轮交付 |
| --- | --- | --- |
| Birdtie 文档阅读基线 | `4637cfb6e7758fc4fa5dd548fad623a8484c4cfb` | 未重置到该版本 |
| 本地与 GitHub 工作分支 | `codex/birdtie-import`，`57f4141fb3c28dc6a03aaab8dcfd69be5d4ac7a5` | 在既有修改上增量；最终提交见 GitHub 回执 |
| Civu-client 本地 / GitHub | `1bbf0613b513001447f326225c944c1a1fbd8e8f`，工作树干净 | 只读、零写入，源码阅读不是 Civu 运行验收 |
| API / 独立数据库 | API23 / schema170 | API26 / schema171，原 1–170 SQL 保持 |
| Web / Android | Web24 / APK24 | Web29 / APK29（APK 与28逐字节一致），构建前后生产源码摘要一致 |
| 远端 | `https://47.110.154.133` | 同一入口、独立 Birdtie 数据库；固定开发验证码沿用 123456 |

APK28/29 SHA256：`4e23d39b20c7d2af8ca649b07e9a1770981dd66798bf13f87a1f2a2359c52f32`。API26 SHA256：`d77531962e0a89fa4f136849d5e883342d8d8f66caf37df5079871b722e42afe`。安装包实际摘要与构建摘要另见 `device-start29/receipt.json`、`android29-artifact-identity.json`；部署结果见 `public-client29/receipt.json` 及 `docs/testing/evidence/social-remote-migration-20261015/upgrade171-03/receipt.json`。最终源码与API26/Web29/APK29构建摘要核对通过，见 `final-source-commit-check29.json`，包含 Web HTML。

## 小点来源和真实调用链

| 来源 | 核对结果 / 本轮处理 |
| --- | --- |
| Mapbox provider 普通 POI symbol | Android 与 Web vector style 隐藏 POI 图标/标签；道路、必要地名、attribution 保留。不是通过隐藏业务层假装减少 API。 |
| PublicCityController 城市地点目录 | Now 初始化禁用无关键词的城市 places 请求。显式关键词搜索保留；清除搜索不重新铺全城。 |
| Birdtie typed PLACE/MOMENT 旧默认实体 | 仅 Now 内容模式排除其默认回灌；不是全局删除地点/动态对象。 |
| `/v1/me/map-content` 业务内容 | 四角投影视口 → 400ms native settled / Web 对应事件调度 → authenticated GET → 空间 SQL → viewer ACL → 完整计数/聚合 → ≤40 marker → 原 Now 面板。 |
| 主动任务、活动、收藏、旅程、主题地图 | 保留各自显式来源、权限和导航逻辑；任务标记单独预算、重叠可展开，不回灌默认层。 |

当前 Web 是 Mapbox GL JS 矢量底图与 Flutter overlay，不能沿用文档旧版本的栅格假设。手机底图小点与业务标记分别核实。旧默认接口在三组视口返回 304～384 个业务地点减少到有限内容聚合，并非把全部地点下载后在前端隐藏。

## CIVU 复用映射

无覆盖业务源码的明确 LICENSE/COPYING/NOTICE，因此本次抽取语义并在 Birdtie 原有组件实现，未直接搬运业务源文件。

| CIVU 只读来源 | Birdtie 适配 |
| --- | --- |
| `lib/src/state/persistent_map_scene.dart` / `_momentAnchorPlace()` | `apps/client/lib/src/workspace/owned_map_api.dart` 的 MOMENT_ANCHOR、anchorId/momentId/entryId 与对应媒体；纯显示空间内容，非 POI。 |
| `lib/src/widgets/explore_map_marker_icon.dart` | `apps/client/lib/src/workspace/map_marker_bitmap.dart` 方形媒体家族；纯文字、封面失败保持可点击占位；视频只用封面。 |
| `lib/src/widgets/persistent_map_host.dart` | `apps/client/lib/src/workspace/owned_map_controller.dart` settled、取消、代次、同键复用及有界 LRU；不用 Civu 全局状态，不叠第二层延迟。 |
| `lib/src/services/explore_map_viewport.dart` | `apps/client/lib/src/city/native_city_map_io.dart` 四角投影、实际 zoom、经度跨界；`public_city_map.dart` 的 Web 调度监听 wheel/doubletap/rotate/resize 结束。 |
| `lib/src/widgets/civu_explore_map_mapbox.dart` / marker delta | `native_city_map_io.dart` annotation 增量；`apps/client/lib/src/workspace/owned_map_media_controller.dart` 稳定媒体键、12 封面 / 4 并发起点、选中优先和单地图实例。 |
| `lib/src/services/explore_map_marker_density.dart` | `apps/client/lib/src/workspace/map_display_policy.dart` 参考避让、选中保护；其 cluster 函数返回空，未把它宣称成成熟聚合。`apps/api/internal/postgres/map_content_spatial.go` 使用受权限过滤的服务端空间网格与完整成员分页。 |

## 数据、接口与权限

沿用 GET `/v1/me/map-content`，升级为 `map-content-v2`：bounds（含跨日期线）、zoom、DEFAULT_CONTENT、可选选中标记；身份只取认证。返回 PLACE_CONTENT / MOMENT_ANCHOR / CLUSTER，稳定 markerId、对应 entry、位置精度、完整 distinct 动态数和锚点数、轻量封面及聚合成员路径。未取“前 40 条”冒充总数。

SQL 先按空间索引缩小候选，再做当前会话/内容/地图授权；无 N+1 地点查动态、无外部地点搜索、无另一套数据库。反复区域查询实际发现 PostgreSQL generic plan 严重退化，采用本事务 `plan_cache_mode=force_custom_plan`，没有修改服务器全局配置。近景细分，远景逐级合并到预算；成员分页可继续访问。

171 迁移添加独立旧锚点事实、约束、索引和证明账本。实际从已授权备份迁入 **16 个锚点（15 个媒体、1 个正文锚点）**。来源均有原地图授权和 WGS84 证明；48 个已有地点关联媒体继续沿地点，不因 EXIF 自动扩散。原库、原项目、原备份零写入，places/moments 数量不变、原 ID 保留、无新公开发布。其余无充分地理授权的数据保留原访问路径，不造坐标。

覆盖独立锚点与真实地点相同落点的精确去重；不同真实位置不按距离合并。点击锚点按 entryId 读取，关联公开地点/旅程不扩大原动态权限。

视口 LRU：最多 8 个视图 / 320 pins / 2MiB；媒体 RAM 有界，12 封面、4 并发，真实服务端 ≤160px JPEG；客户端按 DPR 限制解码。稳定媒体身份/版本复用，不按签名 URL 或对象 hash 重下载。旧授权到期清除文字/像素；401/403/404、账号/权限变化清缓存。续读约 15 秒、内容授权最多 30 秒。已校验当前 binding 的前台 push 只触发清除并重新权威读取，事件本身不授予访问权；未收到远端信号时不能保证即时得知撤回，离线亦然。

聚合 cursor 是有界 30 分钟的签名位置凭据，每页仍重新执行 ACL 和 sourceVersion 校验，内容授权仍最多 30 秒。列表最多保留 8 页 / 320 项当前窗口，续读从当前窗口起点开始；并非把所有历史页留内存。选中/视口变化可以重新授权相同来源，权限 epoch 变化不得恢复旧账号内容。

## CM 开发和验证对账

| ID | 状态 | 证据范围 |
| --- | --- | --- |
| CM-01 | DONE | 工作树、APK24 实际摘要、API23/schema170、Civu/GitHub 基线；provider/业务分层审计 |
| CM-02 | DONE | 默认入口不请求空关键词目录；真实 SQL/远端/Android/Web 无空内容普通业务地点 |
| CM-03 | DONE | 16 个事实锚点实际提交；世界范围分页全部找到；无 placeId 真实条目/媒体可读 |
| CM-04 | DONE | 方形媒体/内容聚合；Android 与 Web 原 Now 面板定位正确锚点、正文/图片、返回 |
| CM-05 | DONE | SQL 100/1000/10000；完整计数与首/次/末页；81 同点真实 DB 跨 lease，400 成员 widget 当前窗口续页 |
| CM-06 | DONE | 旧请求取消、同键去重、LRU、权限隔离；native focus/gesture 与 Web wheel 回归 |
| CM-07 | DONE | 小图/并发/解码上限、同媒体续租零 annotation 重传、latest-wins；真机一个地图实例 |
| CM-08 | DONE | 27 个 map_canvas 任务回归、收藏回归、显式搜索/活动入口；原生 DB 权限与注销拒绝、账号切换清理。跨设备推送实测仍 NOT_RUN。 |
| CM-09 | IN_PROGRESS | 本次支持端实际链路及性能样本已执行；严格同相机前后性能与下列未执行项尚缺，不能称全面性能验收通过 |
| CM-10 | TODO | 仅在有证据瓶颈后再升级 source/style layer，不在本轮重写引擎 |

## 测试记录

| 验证 | 结果 | 证据（相对 `docs/testing/evidence/content-map-cm-20261016/`） |
| --- | --- | --- |
| 整合四个定向测试文件（27批次） | PASS，14 项 | `final-target27-01/`：renderer、web scroll、controller、panel |
| Now 不预读全城、主动搜索及活动 | PASS，1 项 | `default-entry01/` |
| 任务图层/地图画布 | PASS，27 项 | `task-regression02/` |
| 原收藏列表定向回归 | PASS，1 项 | `task-regression01/output.log` 中 personal_collection_map 用例；同批另有2失败后修复，不把整批称通过 |
| 最终相关 Dart analyze | PASS，0 error / 0 warning | `root-check27/`，INFO 保留 |
| startup/native/map contract / restore | PASS | `root-check01/`、`backend/verification.json`、`backend/source-ready-final.json` |
| 独立 PostgreSQL 连续位置及媒体版本 | PASS，3 项 | `backend/continuation-source-ready.json` |
| API26 远端当前原账号、分页、媒体与注销 | PASS | `live-api26-01/receipt.json` |
| APK28 实装、手机操作和运行计数 | PASS，受影响流程 | `device-start28/`、`phone28-acceptance.json`、`phone-apk28-final01/` |
| Web27 真实 Chrome wheel 及地图实例回归 | PASS，受影响流程 | `web27-acceptance/final-receipt.json` |
| Web28 普通画布聚合、独立锚点、正确详情与返回 | PARTIAL，普通画布通过；无障碍DOM误点失败 | `web28-acceptance/receipt.json` |
| Web29 无障碍DOM相邻标记点选补修 | PASS_AFFECTED_ACCESSIBILITY_POINTER_FLOW | `web29-acceptance/receipt.json`，键盘观察单独记录；直接renderer/widget 5项PASS见`semantics29-02/` |

失败原样保留：APK27 的旧聚合 DPR/标签遮挡视觉失败，在 APK28 修复并重新实测；Web27首个脚本提前读取parsed page造成的时序失败以完成ResourceTiming和final-receipt核实；早期 SQL 保留字/fixture时钟与 generic plan 超时；任务层 filter 作用域过宽导致 2 项失败后收窄；native focus、panel lease、续租 delta 的中间失败；Web26 实际滚轮未更新 zoom 明确 FAIL，修复后以 Web27 回执为准。源码阅读、widget/Fake SDK、SQL、API、真实界面分别记录，互不代替。

## 性能前后对比

三组完全相同 bounds、同 HTTPS 入口、每组 3 次的 API 采样。API23 没有 zoom 参数，最终请求 zoom12。网络/时段并非实验室恒定条件，展示中位数而非宣称普遍加速。

| 视口 | API23 → 最终 API 中位耗时 | 结果 JSON 字节 | 返回标记与完整统计 |
| --- | --- | --- | --- |
| wide | 619.7 → 133.1 ms | 166,459 → 4,005 | 384 → 2（完整 9 动态 / 7 锚点） |
| phone | 616.1 → 121.7 ms | 132,194 → 4,041 | 304 → 2（完整 9 动态 / 7 锚点） |
| adjacent | 759.0 → 121.3 ms | 133,498 → 4,041 | 307 → 2（完整 9 动态 / 7 锚点） |

远景没有封面是聚合结果，不能把旧 6.3MB 代表原图预算与聚合 0 字节当作同媒体压缩收益。另有真实独立媒体 marker JPEG 2,855 字节，摘要与对应条目媒体匹配。

隔离真实 PostgreSQL 压力数据每组另含 10,000 个范围外及 100 个无权限候选：100 / 1,000 / 10,000 范围内候选，SQL 从 23.656/47.454/289.789ms 到 15.333/37.109/290.594ms；API 从 82.023/140.679/689.867ms 到 70.464/130.438/749.393ms。10,000 项 API 略慢，未隐去；输出预算、总数和可继续分页通过。这是本次中间空间 SQL 实测，最后媒体版本修复后的同规模性能重跑 NOT_RUN。

Android：同一 25098PN5AC、1220×2656、Flutter profile、Mapbox native + Flutter 面板。APK24 四次左右拖动样本：PSS 853,473→922,789KiB；Animator p95 1.430ms，GPU Rasterizer p95 3.819ms；306 次 Animator 中 1 次超过16.67ms。APK26样本：PSS 617,593→586,567KiB；Animator p95 1.786ms，GPU Rasterizer p95 2.562ms，326样本均未超过16.67ms。最终 APK28 数值见附录。

**限制：旧样本没有记录 camera/zoom，不能称严格同相机 A/B，不能推导 FPS 改善比例；Flutter trace 不覆盖全部 Mapbox 原生渲染线程。PSS 为前后采样，不是持续内存峰值。** 最终包增加只含数值的样式可用、首内容、首封面时点（相对 MapWidget state 创建），不是进程启动到可交互的系统指标。

## 未执行边界

- NOT_RUN：严格同相机/缩放/轨迹的手机前后 A/B；旧包首可交互/首内容/首封面时点；完整原生 Mapbox 帧追踪与持续内存峰值。
- NOT_RUN：10,000 同点成员连续 250 页真实 HTTP 走完、手机 100/1000/10000 压力场景、数百页手机分页。不能用首/次/构造尾页和400项 widget 冒充这些。
- NOT_RUN：跨设备真实撤回/拉黑 push 投递到另一台设备并立即清图的全链路；当前原账号 API 注销拒绝和本地权限失效已测。
- NOT_RUN：iOS、全仓回归、所有账号/城市、本最终包全部发布流程。已有 APK19 统一编辑器真机发布记录保留，未改记为本轮重测。
- NOT_RUN：录屏，依用户后续明确“不录屏”要求，本轮只用截图、真实操作日志和计数。

## 改动路径

以下均相对 `D:/Project/birdtie`，详细源摘要和完整 Git diff 随提交保留。

- `apps/api/internal/content/map_content.go`
- `apps/api/internal/content/map_content_test.go`
- `apps/api/internal/httpapi/map_content.go`
- `apps/api/internal/postgres/map_content.go`
- `apps/api/internal/postgres/map_content_media.go`
- `apps/api/internal/postgres/map_content_native_test.go`
- `apps/api/native_database_startup.go`
- `apps/api/native_database_startup_test.go`
- `apps/client/lib/src/app/birdtie_app.dart`
- `apps/client/lib/src/city/mapbox_style.dart`
- `apps/client/lib/src/city/native_city_map_io.dart`
- `apps/client/lib/src/city/native_city_map_stub.dart`
- `apps/client/lib/src/city/public_city_controller.dart`
- `apps/client/lib/src/city/public_city_map.dart`
- `apps/client/lib/src/workspace/map_canvas.dart`
- `apps/client/lib/src/workspace/map_display_policy.dart`
- `apps/client/lib/src/workspace/map_entities.dart`
- `apps/client/lib/src/workspace/map_marker_bitmap.dart`
- `apps/client/lib/src/workspace/map_marker_widget.dart`
- `apps/client/lib/src/workspace/map_workspace.dart`
- `apps/client/lib/src/workspace/now_map_content_panel.dart`
- `apps/client/lib/src/workspace/owned_map_api.dart`
- `apps/client/lib/src/workspace/owned_map_controller.dart`
- `apps/client/lib/src/workspace/owned_map_media_controller.dart`
- `apps/client/test/owned_map_test.dart`
- `apps/client/web/birdtie_mapbox.js`
- `apps/client/web/index.html`
- `apps/api/cmd/legacy-map-anchor-restore/main.go`
- `apps/api/internal/legacymapanchor/build.go`
- `apps/api/internal/postgres/legacy_map_anchor_restore.go`
- `apps/api/internal/postgres/map_content_spatial.go`
- `apps/api/internal/postgres/map_content_spatial_perf_native_test.go`
- `apps/api/migrations/171_content_map_anchors.down.sql`
- `apps/api/migrations/171_content_map_anchors.sql`
- `apps/client/test/content_map_cluster_bitmap_test.dart`
- `apps/client/test/content_map_controller_test.dart`
- `apps/client/test/content_map_now_default_test.dart`
- `apps/client/test/content_map_panel_test.dart`
- `apps/client/test/content_map_renderer_test.dart`
- `apps/client/test/content_map_web_scroll_test.dart`

## 回滚与兼容

先停用新入口或切回已有 release/caddy/APK 备份；不要删除 Civu 源库或覆盖原备份。schema171 对170为增量，上一 API25 仍可使用171；API23回滚需要结合旧 map-content-v1 客户端配套，不把新客户端直接接旧合同。171 down SQL 已提供供离线核对；生产未执行 down、未删除锚点账本。原始事实能由 sourceHash/source IDs 追溯，迁入没有创建额外公开动态。固定开发码未改成新的短信服务。

本轮遵循 UX-CHECK-01～16 中适用的单 Now 面板、显式意图、账号/权限隔离、失败不丢内容、稳定返回和真实证据要求；未运行的辅助技术专项不借测试代码宣布通过。正常字号，无新增图层开关或动态类型选择页。

最终附录可用回执：`live-api26-01/receipt.json`, `web27-acceptance/final-receipt.json`, `web28-acceptance/receipt.json`, `web29-acceptance/receipt.json`, `android29-artifact-identity.json`, `phone28-acceptance.json`, `phone-apk28-final01/receipt.json`, `phone28-metrics-initial.json`, `phone28-renewal-delta.json`。

Web29 部署 manifest SHA256：`bb00c1c9fb2790c14289fc3a9cbcecb488832a457a1fd6880d7378c0d80939f6`。Web28 采集脚本交互等待曾阻塞自身 route 事件，相关到期画面不能直接作为线上续租缺陷证据；消除干扰后普通画布正确，但实际发现无障碍DOM隐藏标签溢出按钮、截获相邻动态点击。Web29仅为地图点添加独立Semantics.identifier和局部CSS overflow:hidden，完整语义标签、48dp点击目标和路由保留；没有全局裁剪页面按钮。新widget测试首批因测试handle释放时序失败，修正后5项通过；Web28产品失败和该测试失败记录均保留。

Android29重构建和重新安装后读取base.apk，SHA256与28完全相同（`android29-artifact-identity.json`）。因此手机截图、受影响链路与性能仍直接对应当前安装字节；未重复全套真机流程，不将其伪记为29版重新跑过的验收。

APK28 初次 MapWidget state 创建后：样式可用 97.939ms，首批已授权内容 annotation 完成 4,884.699ms，初次 1 个视口请求 / 4,259 字节 / 2 个聚合标记。首封面计数含用户主动放大等待，不能称冷启动封面耗时。35 秒静止、跨两次续租：marker 新增/更新/删除均 0，封面请求与解码均 0，媒体缓存命中 4 次；地图实例数一直为 1。

最后补修定向测试：final-target28-01/receipt.json，9项PASS（含DPR实际PNG、相邻标签遮挡，以及当前窗口位置游标跨超过32次新签名的LRU续读）。与上方14项存在重叠，不相加成23项独立用例。

各批次存在用例重叠，后续5项也包含此前renderer回归，不相加为独立测试总数。Web29真实浏览器回归只覆盖已失败的无障碍DOM误点；完整读屏软件专项验证NOT_RUN。

APK28 实际四次拖动数值：
```json
{
  "status": "PASS_MEASUREMENT_ONLY",
  "scenario": "same four map swipes east/west, 600ms each with 2s settle",
  "firstInteractive": "NOT_RUN",
  "firstContent": "NOT_RUN",
  "firstCover": "NOT_RUN",
  "rawPrivateDataSaved": false,
  "pssBeforeKiB": 561673,
  "elapsedMs": 11250,
  "pssAfterKiB": 553763,
  "requests": [
    {
      "category": "map-content",
      "status": 200,
      "durationMs": 512.663,
      "responseBytes": null,
      "completed": true
    },
    {
      "category": "map-content",
      "status": 200,
      "durationMs": 626.32,
      "responseBytes": null,
      "completed": true
    }
  ],
  "frameEvents": {
    "Rasterizer::DoDraw": {
      "samples": 340,
      "p50Ms": 1.635,
      "p95Ms": 2.834,
      "maxMs": 8.338,
      "over16_67ms": 0
    },
    "GPURasterizer::Draw": {
      "samples": 340,
      "p50Ms": 1.645,
      "p95Ms": 2.84,
      "maxMs": 8.366,
      "over16_67ms": 0
    },
    "Animator::BeginFrame": {
      "samples": 339,
      "p50Ms": 0.251,
      "p95Ms": 1.665,
      "maxMs": 6.578,
      "over16_67ms": 0
    }
  }
}
```
