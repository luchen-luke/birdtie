# Now 分类 Pin 与自动地图：2026-10-18

状态：API37 已部署且就绪，Android70 已覆盖安装、数据保留，实际产物与构建哈希一致。69真人确认键盘3次弹出/收起已不卡顿；拖动缩放仍重新读取并整图闪动，明确 FAIL_USER_REPRODUCED。70修复候选已通过相关19项回归（最终时间边界变动后controller6项再次通过），高德实际拖缩尚未验收，不能当作已解决。本轮仍为 PARTIAL，未关闭 PMAP-12 或原发布门槛。唯一 live 状态继续放在 `BT-FIX-NOW-UI-001.pinAutoMap20261018`，不另建任务队列。

## 本轮输入与边界

- 用户指定附件：`D:/Project/birdtie/Birdtie-Now-Pin-AutoMap-Codex-2026-10-18.md`；相同副本在 `birdtie-pin-automap-handoff/`。
- 两张来源截图：`birdtie-pin-automap-handoff/references/01-xiaohongshu-reference.jpg` 与 `02-birdtie-current.jpg`。仅作设计/现状证据，不能充当运行后截图。现状截图比例尺为 2 km，验收不可借放大镜头改变密度。
- 开始 HEAD：`f729aaf6ce822c886aa55a90abed5871a71635c3`。开始无 tracked 修改；三处未跟踪附件目录完整保留，排除提交。
- 本次明确授权跨区域切换录屏，取代旧任务的禁止录屏限制；仍仅使用正常字号。
- 不向生产写入假地点、动态、用户或测试 Task；临时地理搜索数据不落盘；权限和供应商署名保留。

## PMAP-01 实际代码审计

| 项目 | 当前实现与问题 | 本轮处理方向 |
| --- | --- | --- |
| 渲染器 | Android 真原生 AMap / Mapbox；Mapbox Flutter 2.26.0、native 11.26.0；vendored AMap 1.0.15，Android 地图/定位 11.2.100，搜索 9.8.1。Web Mapbox GL JS 3.32.0 / AMap JS 2.0 | 在现有适配器增量修复，不能用静态图片替代 |
| Place / Moment | 原生优先使用 `entity.mediaPixels`，Place cover 因此覆盖分类图标；媒体同步还为 Place 加载封面 | 地点用本地分类 pin；只有独立地理动态保留方形媒体 |
| 分类来源 | MapEntity / PublicPlace 及 `map-content-v2` 已有 categoryCode | 使用真实 taxonomy；未知类明确中性，不猜名称 |
| 尺寸与 DPR | 共用标记尺寸存在 28/36/6 等旧值；DPR 路径在 bitmap 和原生投影内，截图像素不等于 dp | 分离逻辑可见尺寸、位图像素与触达区域，逐条核查 |
| 名称 / 密度 | owned pin 绕过密度降级；固定右侧 112×40 标签、总量 6；previouslyVisibleIDs 未用于稳定；数量/照片影响排序 | 基于地表范围和实际文字宽度；有限方向布局；先弱化数量，再名称，再局部聚合 |
| 内容语义 | 普通内容层与显式搜索/收藏层已有区别；搜索能上图真实无动态地点 | 保留显式层能力；普通层不灌入完整 POI 库 |
| 坐标 | MapBounds 为 WGS84；AMap 边界转换现有；geocoder 为临时数据 | 路由与坐标转换分开；转换函数的矩形包络不能判定服务区域 |
| 供应商 | 只按选中城市国别路由 CN→AMap、HK/MO/TW/其他→Mapbox；自由拖动不会切换 | 统一目标与代次，稳定视口使用有出处的区域判断 |
| 隐私 | 两个“启用”按钮承载 AMap 真实同意，并非单纯开关；目前同意只保留本进程 | 合并真实地图服务隐私说明，同意后自动路由；不能默认同意 |
| 状态恢复 | 只保存 City ID；没有按目的地的镜头持久状态；browseKey 仅 City ID 不足以防 A→B→A 迟到 | 增加独立目标代次、按目录目标恢复镜头；临时搜索坐标不持久 |
| 跨入口 | 跨城收藏被当前城市过滤；旅程/主题地图有回调接口但实际入口未完整连接 | 复用现有授权投影与真实数据，接入统一目标 |
| 平台范围 | iOS hasUsableMap 与适配器支持判断不一致；原生 AMap iOS 未锁版本；本机不能运行 iOS 真机 | 修复可判断逻辑；未执行平台证据保持 NOT_RUN/BLOCKED |
| 定位 | 当前没有实际 GPS 功能或权限申请；指南针不是定位 | 不在启动时索取定位；不伪称已验收 GPS 回归 |

主要路径：`apps/client/lib/src/city/{public_city_map,native_city_map_io,amap_city_map_io,map_provider_policy,public_city_controller,map_camera_focus,exploration_city_store}.dart`，`workspace/{map_entities,map_marker_visual,map_marker_bitmap,map_marker_widget,map_display_policy,owned_map_controller,map_canvas,map_workspace}.dart`，`apps/api/internal/{content/map_content.go,postgres/map_content_spatial.go}`。

已复核 `docs/architecture/CIVU-REUSE-MATRIX.md`：复用 Birdtie 已迁移内容接口、分类字段和地图适配能力。本轮不复制 Civu 仓库或生产数据。适用 UX-CHECK-01、04、05、06、07、08、09、10、11、12、13、14（正常字号）、15、16。

## 依赖与验收状态

| 项目 | 依赖 | 当前状态 |
| --- | --- | --- |
| PMAP-01 渲染/坐标/开关审计 | 无 | 审计完成；2 km 的 APK59 实机基线已采集，不能作为修复后截图 |
| PMAP-02 地点/独立动态/显式层语义 | 01 | 代码、隔离 ACL/去重检查通过；61 上海真实方形独立动态、类别地点与局部聚合可见；全部点击/撤权真机组合 NOT_RUN |
| PMAP-03 尺寸/名称/触达 | 02 | DPR、文字测量、命中测试通过；61 两端实际正常字号截图可用；多 DPR 真机与完整名称读屏 NOT_RUN |
| PMAP-04 分类与颜色 | 02 | 共用分类映射与未知兜底通过；61 两端真实分类符号截图已取得；相同地点跨供应商对照 NOT_RUN |
| PMAP-05 密度/碰撞/稳定 | 03、04 | 63/64真实上海2km已隐藏概览计数、分类pin与方形动态保留；67/68/69真人闪屏失败，70修复候选待真机复验 |
| PMAP-06 统一地图目标 | 01 | 目录/临时目标/动态/收藏/旅程/主题地图接线；直接路由矩阵通过 |
| PMAP-07 坐标与区域出处 | 01、06 | 共用 WGS84 边界与有出处的陆地区域判断；未改写 Civu/业务坐标；两本城图可见；逐点实地坐标测量 NOT_RUN |
| PMAP-08 无缝切换/迟到/恢复 | 06、07 | 导航/迟到/目录恢复直接检查通过；61跨区有白底/默认北美地球；62补同源创建镜头，66/67拆分普通query与真实退休；最新跨区录屏仍待复验 |
| PMAP-09 删除供应商选择/启用入口 | 06、08 | 旧启用按钮与强制选择入口移除；真实用户在60隐私页同意、61重启读取恢复；跨区自动切换与署名已取证 |
| PMAP-10 底图/安全区域 | 05 | Mapbox实际style命中1/applied1；两端署名与键盘截图已取得；高德路名/区名进一步降权 BLOCKED（需获准新样式导出） |
| PMAP-11 缓存/媒体/性能 | 05、08 | 69真实键盘反馈通过，拖缩闪屏失败；70有限授权显示基线及增量路径修复通过相关回归，真机待验；完整网络计数 NOT_RUN |
| PMAP-12 真机/权限/构建交付 | 02–11 | API37已部署；Android60–70构建成功，67重连后及68/69/70覆盖安装均核对实际APK；70高德拖缩未验收；iOS/Web真运行、真实读屏及其余缺证据仍未关闭 |

## 本轮运行证据

以下均为本轮实际运行，不与旧 APK59 的历史性能合并计数。各批有交叠，不将批次数相加当唯一测试总数。

| 检查 | 本轮结果与证据 |
| --- | --- |
| 共用标记、密度、媒体过滤 | 73 个唯一直接 Flutter 检查通过；`work/pin-automap-20261018/markers/receipt.json`，保留 direct01 夹具失败 |
| 路由、区域、真实隐私、目录相机、搜索 | 35 个唯一直接检查通过；`work/pin-automap-20261018/routing/receipt.json` |
| 导航与真实 MapCanvas | `root-navigation04`：39 PASS，退出0，含同城迟到取消、原会话保留、ONLINE 备份不复活、REFERENCE 无目录渲染、身份退休与实际地表跨度 |
| 动态/聊天/旅程入口 | 39 个正常字号直接用例通过，包含8项新增回调与源失效用例；`work/pin-automap-20261018/entry-tests/receipt.json` |
| 地点面板 | `owned-panel-tests` 最终1 PASS，包含分页、滚动不跳、撤权擦除媒体字节、旧编辑授权失效；旧 UI 选择器失败保留 |
| 原生相机/AMap运动/IME/底图策略 | `root-owned-panel01` 的35项通过；该批另有旧面板夹具失败（后已修）、误填不存在的坐标测试路径失败（保留）；不能称该整批通过 |
| Web AMap JS bridge | `web-bridge-current02`：16 PASS、0 FAIL，退出0；真实非线性 SDK 投影的隔离 bridge 回归，不等于浏览器真 SDK 验收 |
| Mapbox底图策略 | 6项 Flutter helper 与 Node 检查通过，见 `work/pin-automap-20261018/styles/receipt.json`；61/65实际profile日志candidate1/applied1，不把helper当作真SDK样式证据 |
| Go 当前地图 ACL/去重 | 新隔离 PostgreSQL：4 PASS、0 FAIL、0 SKIP，退出0；真实170/171、源版本变体/私密/城市/撤回/Session拒绝均已运行。`server-tests/receipt.json`；临时 parent、2个 child DB 和专属容器均删除，原首轮媒体 variant 夹具失败保留 |
| 整合静态分析 | `integration-analyze02`：实际 **0 error、14 warning、2252 info**，标准 `flutter analyze --no-pub` 退出1；不报告为全绿。首轮1个旧测试 override 编译错误已修复；其余 lint 原记录保留，未大范围改无关代码 |
| 61地表/观测入口 | `ground-calibration61` 44 PASS（含旧字号矩阵，不标正常字号专批）；`build61/observe-direct01` 4 PASS。只观察60秒不注入触摸/业务请求 |
| 62增量修复 | `overview62` 1 PASS：8/15.99/16/18.49/24/40m/dp与不同供应商zoom；`render-lifecycle62-repair` 4 PASS：真实运动、inactive不暂停GL、乱序native await合并、退休后不续调用；`native-creation62` 2正常字号PASS：实际平台创建镜头与viewport同源。首轮误用const Semantics编译失败完整保留 |
| 62源分析 | 四个直接变动源 `dart analyze` 退出0，0 error/0 warning/**42 info**；`source-analysis62/receipt.json`。不外推全仓库无lint |
| 63遮挡 | `chrome63` 1 PASS、`chrome-related63` 4 PASS；保留完整授权entities与cluster成员，只裁剪被控制区盖住的glyph。四源分析退出0/42info；`source-analysis63` |
| 64语义/原生宿主 | `semantics64` 1 PASS：真实widget按settled contentRect隐藏/恢复glyph和读屏语义，原生实例不重建；`texture64-repair` 2 PASS：实际平台create请求TLHC+HC fallback/禁debug/once dispose，旧生命周期/迟到回调检查仍通过。原首轮fake resize/bytes codec失败保留。两变动Dart源分析退出0/21info；native TextureMapView API/构造由锁定jar javap核验，64实际Gradle与编译后DEX核验通过 |
| 65 IME浮层 | `ime-glass65-repair` 3 PASS：实际中间IME inset序列1/150/300/1/0中禁用BackdropFilter合成采样，TextField元素/输入稿/范围保持，关闭后恢复；正常surface和辅助效果fallback原断言仍通过。首轮移除wrapper导致真实输入元素退役的失败保留，已改用enabled开关。两实际修改源分析退出0、无issue；`late-analysis/receipt.json` |
| 最终65整合分析 | 当前完整工作树 `flutter analyze --no-pub` 实际0 error、14 warning、2257 info，退出1（2271 issues），`integration-analyze-final65/{receipt,severity-counts}.json`。不将标准命令写成通过，不沿用旧62前结果 |
| 最终67整合分析 | `flutter analyze --no-pub`实际 **0 error、14 warning、2258 info**，退出1（2272 issues）；`analyze-final67/receipt.json`与原log。工具分析7.7秒，wrapper16.098秒含分析后输入检查；8126输入摘要前后一致。没有把“零编译error”写成标准分析通过 |

直接运行的每条命令、工作目录、退出码及范围在以上 receipt/log 中。历史大字号用例仍保留；根批 `root-entry-integration01` 与 `root-config-boundary-final01` 曾误选既有放大字号用例，失败未删除，这两批不得标为 normal-only。后续验收只选正常字号，未由这次误运行宣称大字号通过。整套 Flutter/Go 测试未执行，不将相关单元通过称全套通过。

### 已实现的关键行为

地点使用同一分类表和 Material 符号，20/24/28逻辑像素；独立地理动态保留30/34圆角媒体，聚合28/30。名称12/13、实际测宽单行省略、辅助11中性灰；只有空间允许才显示动态数。48dp触达按可见形状/距离解算，不铺满透明手势控件。地点 pin 不再请求或解码封面；位图缓存128项/2MiB，48dp画布，DPR只在边界处理一次。布局先减辅助行，再尝试有限方向，再隐藏名称/局部聚合；原坐标和完整成员不挪动，数量不作热度排序。

服务端仅对当前授权且有效的叶子集合去重：当前 Place 已承接的同一 Moment 不再额外作为独立点；不可见或不在本次集合的 Place 不能抑制独立动态。聚合按地点/去重动态/混合项区分，不增 DTO 非法字段。此修复只改读取投影，没有清空或批改真实数据库。

所有地图入口汇入 `MapNavigationTarget`，只带获准当前坐标、来源版本和结构化国家/区域。目录相机可安全持久恢复，临时供应商结果和内容目标不升级为目录记录。显式目标立即路由；自由拖动在实际可见跨度、稳定期和陆地区域都确定后才切换，海上/世界/边界保留当前供应商。500m留白是保守策略，不是数据精度声明。切换用递增导航代次淘汰旧 callback；同城新导航也取消旧查询，保留原 Task/会话/输入稿但退出其旧地图层。REFERENCE 无 City ID 仍可通过 renderer-only 视图读取合法 bounds 内容，不能产生假 City 或业务授权。身份变化退休内容相机及其旧代次。

62根据实际上海2km画面把概览边界定为16m/dp（375dp约6km覆盖），不按供应商zoom或比例尺取整文字判断。独立动态照片不受地点去封面影响。高德生命周期把严格的内容foreground守卫和原生GL暂停分开：inactive仍退休私有投影，但仅hidden/paused/detached暂停渲染；串行合并并去重paused/FPS命令，退休/撤回同意后不续调用。原生前台FPS仍30，未降低FPS掩盖卡顿。Mapbox锁定2.26.0的viewport在平台创建后才transition，因此同时为native creation传入同源cameraOptions，防止先从默认北美地球镜头飞入目标；未升级SDK。

### 构建、部署与真机边界

API36：Linux amd64/CGO0，退出0，2084项输入前后完全一致；产物36,163,138B，SHA256 `6c39d5fb9d95b09311660eb02dc7329010eb5a447b86a0f0d663019396206e29`。已启用 `/opt/birdtie/releases/social178-20261018-build36`，ready通过；先前35保留可回滚。schema仍178，迁移账本摘要 `d54564006b976406d7402c70028a1b393f9df20ae67a1d6a086766267901635a` 完全一致；环境/公开路由不变，部署脚本无业务/DDL写入、Civu零写。见 `docs/testing/evidence/pin-automap-20261018/api36-deploy/receipt.json`。

真机基线：Xiaomi25098PN5AC、Android16、1220×2656、density520、fontScale1.0、APK59。`device/baseline59-api36/screen.png` 是原版 Place 大照片与常驻计数的 **2km 基线**，虽服务端已经36，客户端仍59，绝不是新 pin 验收。拍摄前后均核对 Birdtie 唯一前台窗口，不记录其他 App。

临时验收工具安装仍回 `INSTALL_FAILED_USER_RESTRICTED`，用户确认没有实际弹窗；系统点击仍有 `INJECT_EVENTS` 限制。现有正常预览应用覆盖安装60、61均成功，未清空数据；拉回installed base.apk与构建SHA完全相同。用户亲自在60版地图服务隐私说明选择同意，61冷启动从真实安全存储恢复；未代改系统开关或伪造隐私选择。真实手势由用户完成，录屏与read-only观察入口不注入手势。键盘由本应用QA FocusNode入口触发，明确标记 `OWN_WINDOW_SYNTHETIC_NOT_OS_HARDWARE_INPUT`，不冒充硬件输入或系统权限通过。

Android61：`app.civu.civu_mobile.birdtiepreview`，0.1.2(61)，profile，357,059,867B，SHA256 `45e3dbe64f661680e436fc45b6edc7847d99a7d9b0ef6dffdcbcc5f95ad737fb`；8126项输入前后摘要 `ef990cbfffd18613054ebf990cffe862765e3734f8935eaa4db1c794dcf527e0` 一致。与59/60同签名，证书SHA256 `58a1ae46f53decc63a8b54f3a02901f5b31def2c664214a72957777e8e88e165`。这份实际产物不能代替后续62。

### 61真实录屏与性能（62之前的版本，不外推62）

共同设备：Xiaomi25098PN5AC、Android16、1220×2656、DPR3.25、fontScale1.0、显示刷新率120Hz。仅Birdtie前台窗口、无音频，前台丢失则停止并丢弃本段；本轮记录未丢失。录屏与轮询本身有开销。下列UI/raster是Flutter帧，不包含原生地图最终呈现耗时，未将空闲时间补成帧；不宣称绝对性能达标或原生60fps。

| 场景 | 真实窗口/Flutter帧 | UI / raster p95 | 系统帧与限制 |
| --- | --- | --- | --- |
| 上海AMap用户连续拖动/缩放 | 60,000.525ms / 249帧 | 4.194 / 4.723ms | totalSpan p95 23.618ms，63/249超过16.7ms；本段系统GFX未恢复，不能称零jank |
| 阿伯丁Mapbox用户连续拖动/缩放 | 60,000.952ms / 2835帧 | 1.370 / 3.516ms | 系统4076帧、jank0%、p95 5ms；Flutter totalSpan p95 6.474ms，2/2835超过16.7ms；不等于原生present覆盖 |
| Mapbox应用内键盘显示 | 8,000.791ms / 78帧 | 5.717 / 3.397ms | 系统95帧、jank0%、p95 9ms |
| AMap应用内键盘显示 | 8,001.201ms / 106帧 | 7.276 / 6.055ms | 系统330帧、jank12(3.64%)、p95 69ms，legacy jank35.15%；不能因Flutter分项低于16.7ms报告卡顿已解决 |

原始目录均在 `docs/testing/evidence/pin-automap-20261018/device/`：`amap61-performance`、`mapbox61-performance`、`keyboard61-mapbox`、`keyboard61-amap` 的 `recording.mp4/receipt.json`。上海原包装脚本错把Android的2条metadata data轨当额外视频，后处理失败；实际H264+2metadata+0audio合法，原视频与5块/1080B/SHA校验完整的性能日志已由 `recover-amap-record.py` 恢复。失败记录保留，遗失的上海内存初始采样 **NOT_RECOVERABLE**，不伪造前后对比。阿伯丁PSS首689292/末721666/峰743006KiB；跨区首632027/峰699235KiB，单段含SDK/媒体缓存与录屏开销，不作内存泄漏通过声明。完整逐请求网络统计 **NOT_RUN**。空GPU直方图的4950ms哨兵值为不可用，不当作真实GPU延迟。

`cross-region61/recording.mp4` 是用户真实阿伯丁→上海→阿伯丁切换（141.749744秒，0音频），SHA256 `5148d2247d9d4b82060658ff99c312018e6471813f62566ac9eb6099ddd9d481`。独立只读审阅查看22个原始抽帧与两张61版图，并核对三次旧instance disposed都早于新instance created；没有长期双地图。**实际缺口保留**：抽样85.5/86.0秒上海白底，86.5秒底图恢复；89.0秒Aberdeen标题下仍为北美地球，89.5秒到本城。只抽样不推算未观看帧或精确连续时长。系统746帧、jank36(4.83%)、p95 32ms，不能报告全程无白底/无闪烁。62据此修复后需新证据。审阅回执 `work/pin-automap-20261018/device-review/audit.json`。

可用61正常字号实图：`device/shanghai61-final/screen.png`（2km，显示需继续校准的计数）、`device/shanghai61-settled/screen.png`、`device/aberdeen61/screen.png`、`device/amap61-keyboard/screen.png` 与 `device/mapbox61-keyboard/screen.png`。首张 `shanghai61-initial` 是创建时白底，不能作为成功截图。59和60均为历史材料，不替代最终版本；59与60虽同为2km，缺少同一实际bounds，不能称像素严格同视野量化对照。

### 63继续取证与64原生性能修复

63已成功覆盖61，未安装62、未清空数据；拉回installed base APK与构建一致。产物357,076,251B，SHA256 `e80d5fa04ee412fd7a045a46fb86560c63f7c2347e7fb5d67f725feeff6581e4`，8126输入前后摘要 `5f6aee39a7688a6d9e9a60d0ef220fb286b421e6fa82c87f81f1e411467d7811`。`device/shanghai63-settled/screen.png` 为63真实上海2km：概览计数已收起，20dp分类pin、独立方形媒体和局部聚合仍在，高德署名未遮挡；`device/amap63-keyboard/screen.png` 为真实键盘。该图不是61或静态稿。

63的 `keyboard63/amap-show-recorded.json` 独立8秒窗口：318 Flutter帧，UI/raster p95 6.171/4.906ms，totalSpan p95 24.841ms；系统160帧、jank65(40.62%)、p95 200ms，仍未通过。65秒录制汇总140系统帧/jank43.57%/p95 65ms与前述8秒为**不同重置窗口**，不混合。`physical63-amap-keyboard-panel/recording.mp4` 随后仅观察用户物理点击、键盘及内容阅读，无QA动作注入；系统1008帧、jank308(30.56%)、p95 150ms、慢UI线程226次、GPU p95 8ms。录像为混合操作窗口，不把它单独归因键盘、也不当作Flutter纯拖图基准。截图最终实际停在串文阅读第2段，虽然文件名含place-preview，也**不能**冒充地点预览验收。

这些证据支持继续核查宿主平台线程/合成路径，尚没有逐帧阻塞栈根因。当前vendored `amap_map` 原来是 `MapView(GLSurfaceView) + initExpensiveAndroidView`强制HC。64改用同一11.2.100 SDK的 `TextureMapView`，同步控制器构造、字段与销毁类型，原生命周期/坐标/同意/触摸边界不变；Flutter用 `initSurfaceAndroidView`允许TLHC且保留HC回退，不强制VD/HCPP、不降前台FPS。依据：[高德TextureMapView](https://a.amap.com/lbs/static/unzip/Android_Map_Doc/3D/com/amap/api/maps/TextureMapView.html)、[Flutter原生Android视图](https://docs.flutter.dev/platform-integration/android/platform-views)，并核对本机实际锁定jar/Flutter源，不依网页日期推定版本。

63的Mapbox语义列表还有一条渲染后路径漏过滤，被遮挡glyph仍产生不可见语义；64把其源改为实际 `_renderedEntities`并新增隐藏/恢复widget回归。旧 `NativeMapSurfaceEvidence.findMap`只识别MapView，未适配TextureMapView，本批全部 `surfaceSamplingArmed=false`，未用该工具宣称原生present通过。真实TalkBack仍NOT_RUN，代码语义回归不等于读屏通过。

64已实际构建并覆盖63：357,076,251B，SHA256 `5b44afcf3141d15a14b3afad7edd212ec6abee66fddf827836083303c6e2c1c2`，8126输入前后摘要 `9e4aab074e13f39558a307b28a2cb03fe62d0ae5244e52a35b843cb52e096962`，build81.905秒/退出0；编译后DEX的两宿主字段与控制器构造参数均确认为TextureMapView。装机拉回APK一致、数据未清空。64相同QA键盘入口：未录制首段系统372帧/jank1(0.27%)/p95 5ms，Flutter272帧UI/raster p95 3.571/19.083ms；录制段系统独立8秒结果见 `keyboard64/amap-show-recorded.json`，Flutter270帧UI/raster5.506/22.351ms，raster35/270超16.7ms。系统慢帧已明显下降，**Flutter采样仍未达建议目标，不报性能全通过**。有/无录制、冷/热窗口分别保留，不进行严格同条件因果量化。

65进一步在键盘可见期间禁用Now本地浮层的背景滤镜合成采样，保留原滤镜widget和TextField子树，避免树结构切换使焦点或输入连接失效；收起后恢复背景效果。`BirdtieSurface.backdropEnabled`默认true，其余页面行为保留；不在地图上加白色蒙版或改变供应商底图样式。65本版真机键盘结果见后，不能用64系统改善代替。

### 最终65构建、安装与当前证据

65实际构建命令（工作目录 `D:/Project/birdtie/apps/client`）：

```text
D:/DevTools/flutter/bin/flutter.bat build apk --profile --build-name=0.1.2 --build-number=65 --no-pub --no-tree-shake-icons --dart-define=BIRDTIE_ENVIRONMENT=development --dart-define=BIRDTIE_FRIEND_IMAGE_ENABLED=true --dart-define=BIRDTIE_API_BASE_URL=https://47.110.154.133 --dart-define-from-file=.env.maps.mobile.provided.local.json
```

退出0，79.081秒，357,076,251B；产物 `work/pin-automap-20261018/delivery/Birdtie-Pin-AutoMap-20261018-65.apk`，SHA256 `5102298168d00456c365cfc2b679e4b75b130881e7fef562d98b358045a77ffb`。8126项输入前后摘要 `04254af4234a422898a80d02b6f2da727405b5a2ad372550bfcfdbf19208b63e` 一致，64→65仅两个预期surface源变化；60–64历史APK哈希逐个未变。包身份0.1.2(65)/正常preview、路由资产、签名全部核验。构建原始sourceHead仍是开始HEAD加当时实际工作树，最终提交见本报告同批Git提交；不能把sourceHead基线冒充修复提交。

65覆盖64成功，`device/install65/receipt.json` 拉回的installed base.apk与构建SHA完全一致，dataClearCommands=0。真实用户60版隐私同意仍从安全存储恢复，没有默认写同意、重新弹供应商启用或索取GPS。Android本轮构建60/61/62/63/64/65全部成功；62未安装，其余按回执安装，均不能相互替代真机证据。

65高德应用内键盘、带录制的实际8,001.054ms窗口：341 Flutter帧，UI/raster p95 **2.641/8.998ms**、totalSpan p95 **10.058ms**；UI/raster/total均无帧超过16.7ms，raster仍38/341超过120Hz的8.33ms，因此只达到本窗口60Hz建议目标，**不报告120fps或绝对性能全通过**。独立系统窗口422帧、jank1(0.24%)、p95 **7ms**，后续65秒录制汇总1812帧/jank1(0.06%)/p95 5ms是不同窗口，不能混成同一分母。`keyboard65/amap-show-recorded.json`、`keyboard65-amap/{receipt.json,recording.mp4}`；真实键盘图 `device/amap65-keyboard/screen.png`。61/63失败和64的raster超标仍保留，不用旧材料冒充65。

### 65闪屏反馈与66修复记录

用户明确反馈两种闪屏均存在。`device/amap65-performance` 的60秒观察窗口为18:19:24–18:20:24，首个实际运动事件约18:20:42，已接近95秒录屏结尾。该窗口主要空闲，标记 **GESTURE_WINDOW_NOT_COVERED**；即使scene写mapPan、有1813帧，也不能当连续手势性能或闪屏通过。原录像、数值及61的有效手势样本保留，不相互替换。

代码审查发现高德名称使用Flutter投影层，运动开始即隐藏；另有宽reader scope随普通视口/query代次变化而开启整图退休遮罩。66将既有避让后的名称绘入同一个实例级原生marker bitmap，相对真实锚点计算新anchor，保留原经纬度；停稳后再重算布局，48dp命中与辅助语义继续走严格reader guard。私有名称/媒体组合字节不进入全局分类缓存，所有异步阶段保留当前源和媒体授权检查。

66增加独立render authority scope：运行时owner/authorization/workspace、navigation、layers epoch/author/filter、mapContent permissionEpoch、任务/收藏身份；不序列化凭据。宽reader scope及source epoch继续拦截迟到读取和点击；相同授权、相同版本有效内容的普通query更新不盖整图。账号/隐私/前后台/源变化、移除或版本改变、媒体授权失效、数量过期/变化仍保持退休屏障。未知调用方默认退回旧宽scope，不能以性能优化放宽其权限。此处是最小保守修复：不同内容集合或真实退休仍可能短暂遮罩，不能凭代码宣称所有闪屏消失。

66直接检查：`native-label66-repair` 1 PASS，覆盖四方向×DPR1/2/3.25真实锚点、相同相对布局的像素稳定与迟到授权失效；首轮Material red夹具与纯红断言不符的失败保留。`retirement66-private` 6 PASS，包含相同query/真实退休、私有live guard、数量续期/过期、生命周期及真实widget运动；`canvas66` 1 PASS，保留ABA旧任务/旧pin拒绝。`source-analysis66-repair` 六变动源0 error/0 warning/45 info、退出0；首轮nullable cascade编译错误与误填测试相对路径失败保留。此记录尚不代表66构建或真机通过。

66后续收紧到“实际复制的原生像素”：仅已绘制的媒体PNG与辅助计数登记期限，未绘制的Place封面/概览数量不定时盖整图；缓存复用也在提交前复查media live guard与期限、aux实际文案。`expiry66`最终7 PASS；`canvas66-authority`实际MapCanvas 1 PASS：普通bounds改变保持render authority、reader scope仍失效；owner/authorization/filter变化则render authority也失效。`source-analysis66-final`实际六源0 error/0 warning/45 info、退出0。**整条pin source lease依旧由上游OwnedMapController expiry清空items/通知并触发missing-row退休**，不能因未画aux保留过期名称或坐标；这与单纯装饰TTL不同。真实权限退休若发生在长手势中仍保守遮罩到安全更新，不以本次局部优化宣称所有闪烁消失。

66已构建成功但未安装：357,092,635B、87.888秒、SHA256 `90d5da7a9d3e850b86289f1eff0816c031f4986ee59beae5d1c80e72bbb2cbb6`；8126输入摘要 `1d8915ef01e0b7db07e12fa9c76a09fc32acd2d12ce7e351c7c6f4d49820fcc6` 前后一致、签名和历史APK校验通过。随后审查发现极窄窗口：contentRect移除原生旧媒体时，旧copy在SDK确认前可能提前丢失期限保护。因此66明确为非最终待装版，`analyze-final66`为NOT_RUN；67保留old∪next的复制媒体/辅助ID及期限直到当前native barrier成功，再交接next；旧期限已到先盖退休屏障。render异步还直接比较内存中的authority，不只依赖通知先后。

`authority67-final` 7 PASS；`source-lease67-verified`既有真实controller单case最终1 PASS：Place仍无媒体上图，独立private Moment继续验证像素、续期、ABA、只读分页与SHA；31秒时源到期后items/view/entities为空、旧读拒绝。HTTP12秒超时后的独立新恢复响应可携带新grant恢复，测试明确核对其observedAt晚于原expiry、validUntil晚于被挂起旧response的期限，旧reader仍拒绝，未把新授权恢复当旧内容复活。旧Place图像等待、排序、lazy参数、旧错误文案及恢复假设失败全部保留；仅修正隔离测试夹具/期待，未修改controller期限或放宽生产权限。

### 67构建及设备离线边界

67为本次闪屏修复候选，实际profile构建退出0、52.293秒，357,092,635B；产物 `work/pin-automap-20261018/delivery/Birdtie-Pin-AutoMap-20261018-67.apk`，SHA256 `9a542b1ef97cfbf71025d9859bdb7375ed311508d1836bd69c2b8899a26df709`。8126输入前后摘要 `9c288b41cec5d06838c78094ff8b7c4d1c33b15c9a9df28e5e96737ee394526f` 一致；66→67只有高德native Dart适配器一个生产源变化，测试夹具不进入APK输入。构建命令与65相同，仅build-number=67；无clean、pub解析或业务数据写入。

67签名与历史审计完成：59–67证书均为 `58a1ae46f53decc63a8b54f3a02901f5b31def2c664214a72957777e8e88e165`，60–66旧文件SHA逐个同原回执；正常preview身份0.1.2(67)、路由资产SHA `37858edb3ca811e4b2de7a85b44803b753da7b9e74113cfec66ac1934ff4cb55` 均实际验证。标准全量分析结果见上表：0 error仍有14 warning，exit1未通过；不扩大修改无关lint，不删旧失败记录。

11:04:29UTC（19:04:29本地）覆盖安装尝试返回 `device 'c641566b' not found`，随后 `adb devices -l` 无设备；`device/install67/receipt.json` installed=false、before/after为空、dataClearCommands=0。这不是USB安装被拒，不能沿用旧权限错误；没有弹出新隐私或安装选择。本轮最终67安装、上海/阿伯丁新截图、两种闪屏手势复验、键盘和跨区新录屏均 **BLOCKED_DEVICE_OFFLINE / NOT_RUN**。原61/63/64/65真实材料及65用户失败反馈保留，不能作为67已验收。已向用户请求重新连接；仓库内交付继续推进。

独立65复核 `work/pin-automap-20261018/gesture-window65-review/timeline.json` 确认：窗口主体近静止，首运动晚于窗止18.031秒；原始PTS15.37s确实有整图浅白遮罩和读取提示、15.43s底图恢复/名称短失、94.92s地图位移且名称消失。没有把空闲帧数当用户手势。源码审查终核 `flicker66-review/receipt.json` 已确认67的old∪next交接及直接authority fence，无新增已证实源码阻断；此审查不代替手机验收。

### 67重新连接、真实失败与68续授权修复

用户重新连接后，67已实际覆盖65，`device/install67-retry01/receipt.json`确认数据清理0、拉回APK哈希与构建一致。67的上海2km截图、用户真人手势及90秒无QA入口的对照录屏均已采集；用户明确反馈两种闪屏仍在。`work/pin-automap-20261018/gesture67-control-review/timeline.json`核3382帧、35个白候选帧/13组，其中多组原帧确认底图、业务标记和名称一起消失，不能将其归因QA入口。前三组静止白帧间隔约15.25秒。原67失败不得被69或数值帧率覆盖。

旧的`am start`验收入口会触发当前Activity生命周期，因而67 synthetic pan与`amap67-human-pan`的观察存在干扰；后者60秒采样也不含连续60秒真人运动，不作手势通过。68新增仅profile/debuggable/preview可注册的固定观察广播，发送者须持DUMP，转发固定null参数、检查焦点和Dart当前地图守卫；不启动Activity、不取消或注入触摸。它不是普通产品入口。

68修复两处代码路径：同一最新工作队列优先清空退休标记并取得SDK屏障，不再等待拖动结束才显示干净底图；正常续期可由新的真实授权继续许可同ID/source/key、相同不可变PNG字节，旧期限已到、权限/内容变化、已退休或原生更新待ACK时仍走原退休屏障。实际copied期限及pending old∪next均保留，未修改旧授权对象或给过期内容续命。直接测试`native-clear-renew68-pending` 8 PASS；首轮误写transport字段名失败保留。

68实际构建/安装哈希`a473824dcab6efcdc3c3891a881319cf8d591a6a06ac2f709ba7f3cf0def0fc4`，8126输入摘要`818fcb3e8b78d8ab50576f2779381973dadb1245598a72b3ff4d38063eb0582e`。75秒静止录像的2343帧白候选为0，但后续真人明确反馈仍闪屏，并在缩放时出现无动态、延迟闪屏。故68为**FAIL_USER_REPRODUCED**；不能用静止改善冒充动态验收。其标准分析退出1、0 error/14 warning/2261 info，非全绿。

用户指出的一条旧媒体定位已做精确只读来源核对，迁移前后的保存数值及媒体关联一致；存档未保留可复核GPS。用户随后明确“先不用管，相册地标确实一致”，已停止追查，未改坐标或业务数据。该事实不等于证明所有历史CRS正确。

### 69 / API37：缩放覆盖、聚合身份与实际原生源

- 缩放覆盖改为同城市、同zoom、有效期限的视口缓存。旧zoom只可给显式选中项保留原读授权；旧整页、邻居和cursor不能证明新区已覆盖。新zoom必走正确查询，选中项续读仍用原zoom/范围，原授权到期与撤权继续拒绝。
- 新完整响应仅淘汰被其完整包含的同zoom旧范围；部分重叠外侧仍受原期限约束。已确认同ID换版和同zoom完整响应中的移除仍立即退休。组合显示期限取真正贡献当前page/选中项的来源，不能把未贡献旧view的期限压到新图片上，也不取max延长任何旧pin。
- 服务端同坐标聚合ID改为reader、真实锚点与排序成员身份；局部source version仅含本组实际事实。全局viewport source仍用于分页、MembersPath、ACL和最终响应证明，未绕过权威复核。该版本仍仅聚合相同坐标和精度，不把低zoom当作移动地点的许可。
- 高德单独跟踪实际写入SDK的源ID；本地聚合跟踪真实成员。未绘制行变动不再误盖全图；待ACK保持old∪next，只有当前ACK可替换/清空。不能用刷新时已清空的Flutter `_displayed`推断SDK中没有旧glyph。

实际直接验证：`coverage69-related`既有controller5项+新增zoom2项全部PASS；新测试先在旧代码复现“不发新zoom GET”，然后验证修复、回到旧zoom复用、原选中source期限和旧cursor隔离。`unpainted69-repair2` 8 PASS，另含续期、运动中清除、pending同key重ACK及ABA；误放contentRect的夹具失败和发现原生源跟踪缺口的失败均保留。`work/pin-automap-20261018/server-tests/native03/receipt.json`实际5 PASS/0 FAIL/0 SKIP，包含170/171真实PostgreSQL权限与撤回，以及3个纯语义用例；178迁移仅在临时隔离容器，parent/child DB和容器均已清理，生产访问false。

69构建输入8126项、前后摘要`54f246920443031fb05ea04dddc63867ab57dd0c395501f8616ccd65bfbd3df8`一致。APK 357109019B，SHA256 `525a11bc85cd6b56ea287f502576957e1aa690b85d40096107a590035fff5169`，preview 0.1.2(69)、签名同59–68、路由资源哈希一致。覆盖68成功、数据清理0、installed base哈希一致。`analyze-final69`标准退出1，0 error/14 warning/2262 info，2276条与工具摘要一致、输入稳定；不能报告分析通过。

API37构建2084项宽输入摘要`1a7ae22ec531c6ace244e545a3d0eb6a41898b527f387265bf2832e6de746a8a`稳定（含测试源码哈希；测试文件不编入二进制）。Linux amd64产物SHA256 `97c4c68f0bbce3f31339db39b102e7792467640f4bf151c9c9c829a3ead07f5d`。`api37-deploy/receipt.json`确认build36→37就绪，schema178/ledger不变、业务数据库写入0、API环境和路由不变、Civu写入0、可回退未调用。只读产物审计见`work/pin-automap-20261018/build69-api37-readonly-audit/receipt.json`。69后续真人结果见下；跨区新录像仍 NOT_RUN，不沿用61材料冒充69。

### 69实际反馈与70增量显示修复

`device/amap69-idle`为75秒静止样本，不能替代拖缩。`device/amap69-keyboard-zoom`为110秒真人操作录屏，无观察广播、Activity启动或注入动作；用户实际弹出/收起键盘3次后拖缩约20秒并停留观察。用户明确确认键盘已不卡顿，但地图仍重新读取并整图闪动。该段未开启Flutter60秒采样，FrameReports为空，不能用系统jank0覆盖用户的闪屏失败。独立系统窗口4326帧/p95 5ms、874帧/p95 6ms分母分开；原生最终present仍 NOT_RUN。数值诊断记录到settle后`authorityChanged=false / expiredCopy=false / missingCopy=true`的退休，支持核查缺项触发，不单凭此认定全部白帧根因。

对照的是当前仓库实际Mapbox `_syncMarkers`（`native_city_map_io.dart`）：按annotation增删改，内容缺项不会盖住底图。高德同样已有MarkerUpdates增量路径，但此前把视口兴趣变化与原源授权失效混在一起，开启opaque退休盖层。两端队列均只保留一个最新待执行任务，未发现无限旧任务FIFO；异步投影仍可能延迟最新更新，不能当作原生零延迟证明。

70在原控制器和适配器内实施：

- 新增有限`MapEntityReadGrant`，绑定原source事实、账户/epoch、原期限及实际不可变PNG；仅用于判断SDK已经复制的内容是否须清除，不赋予新媒体读取、详情或操作权限。原源失效/到期永久锁定，安全装饰复制保留grant；issuer已知无权时过滤实体，不能降为legacy无grant裸名称/坐标。
- 高德独立保存实际原生副本的完整source+真实media/aux标志；pending保留old∪next（同ID多个版本也不合并），ACK后再次核查全部授权。普通视口缺项在原有限授权有效时可等待增量替换；真正撤权、身份变化或到期仍优先清理，不能等拖动结束。裸glyph、名称和坐标的source期限也入定时器。有限预算2048，超限失败关闭。
- 同事实、同不可变字节的接棒逐copy进行。某个合法缺项保留自身原期限，不再连带阻止其他fresh项接棒；已退休/到期/pending不能被新对象取消或复活。
- 新zoom独立取coverage；已有同城市有限lease只提供有界显示基线，不提供新区coverage、旧cursor或分页证明。按精确WGS84坐标与原pin.precision选最新表示，新的同anchor表示整体替换旧表示，不推测截断moments中的成员、不相加新旧数量。实际显示基线贡献其原view期限，续读仍用其原zoom。
- 修复“续期尾页nextCursor=null误撤第一页”的缺项判定及缓存淘汰：只有请求cursor为空、响应nextCursor为空且非truncated的完整单页读才能给负证据；负证据还不能早于基线。正向同ID换版和实际撤权继续退休。

相关测试：`source-grant70-incremental-repair1`在`apps/client`执行`flutter test --no-pub --reporter=expanded test/owned_map_zoom_coverage_test.dart test/amap_city_map_motion_test.dart test/content_map_controller_test.dart`，退出0，19 PASS；最终仅补负证据时间约束后`coverage70-final`执行zoom测试6 PASS、退出0。覆盖新zoom等待期/不完整分页保留、尾页刷新不撤首屏、同anchor不重复计数、旧cursor隔离、原期限与身份ABA，以及pending/移动期间真实source失效的优先清除。两名独立只读源码复核无剩余已发现P1，不等于真机通过。

**旧测试覆盖纠正**：68/69“同PNG接棒跨期限”的widget夹具DPR默认为3而PNG夹具为1，实际走fallback glyph；旧PASS能支持队列/ABA/裸glyph流程，不能证明照片真的复制进SDK。70已显式DPR1，断言实际Marker.icon PNG与已验证图片逐字节相等，再覆盖接棒跨旧期限、另一个长授权缺项不阻塞、旧∪新ACK及裸source期限。旧测试失败及记录均保留，不继续沿用旧照片覆盖说法。

70实际构建退出0、8126输入前后摘要`f94c4075b02c8071042594a322bd3038cad08188ad591120cf5eaf11bbb16ce8`一致。APK 357125403B，SHA256`0eba04ae2c31fb2d517d42aec633d41f32d32419dae244f6c04d9dd2c3adcd91`，preview0.1.2(70)、原签名及路由资产一致；69→70仅三个预期Dart生产源改变。`build70-readonly-audit/receipt.json`复核当前全部输入与实际产物，并重核59–69历史11个APK哈希未变。`device/install70/receipt.json`确认69→70覆盖成功、dataClearCommands=0、拉回base.apk与构建完全一致。标准`flutter analyze --no-pub`退出1、0 error/18 warning/2271 info（2289条），输入稳定；其中新增4条为MapEntityReadGrant中不必要的非空断言，未改分析器flag掩盖，不报告全量分析通过。

70冷启动实际恢复的是**阿伯丁/Mapbox**，`device/shanghai70-initial`文件名在看图前误拟，已附`annotation.json`纠正，原截图/回执保留；该图既不是上海图，也不是内容加载完成证据。`device/amap70-human-zoom`130秒录像无QA/observer/注入，当前无已收到的真人手势完成反馈，numeric-runtime-progress为空、系统帧数0，4950ms是不可用哨兵，不是延迟测量。不能把该录像文件名、阿伯丁静止画面或0 jank当高德复验。已请求用户实际切到上海后20秒快速拖缩、停留10秒；70高德实际拖缩、跨区、真实读屏与原生present均 NOT_RUN/未验收，旧69失败继续有效。API继续为已验证37，不重复迁移或向真实库写测试内容。

iOS 构建与运行：NOT_RUN（Windows 主机无 Xcode/真机）。Web 实际浏览器/供应商服务：NOT_RUN；AMap Web 的已授权 Key/受信代理缺失时为 BLOCKED，旧“暂不部署网页”指示继续有效。高德现有 GeoHUB `style.data/style_extra.data` 原字节保留，当前插件没有路名/区名的单层 opacity 接口；未通过改二进制、关闭全部标签或盖白蒙版伪造降权。

原生 SDK 官方要求：

- [高德 Android 隐私合规说明](https://lbs.amap.com/api/maps-sdk-for-android/guide/create-project/dev-attention)
- [高德 Flutter 说明](https://lbs.amap.com/api/flutter/summary)
- [Mapbox 署名要求](https://docs.mapbox.com/help/dive-deeper/attribution/)
