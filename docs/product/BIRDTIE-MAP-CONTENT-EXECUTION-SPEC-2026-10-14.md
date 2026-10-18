# Birdtie 地图内容呈现执行方案

日期：2026-10-14，Asia/Shanghai。

用户已明确授权“给出方案，然后让 Codex 进行执行”。本轮接续 `D:\Project\birdtie` 当前未提交工作树，把已有地点照片、关联动态和动态详情呈现在同一个 Now 地图中。第一批完成地点照片与真实近期动态提示，第二批补公开动态图片合同和方形照片标点。按正常系统字号开发，保留现有系统缩放；不录屏，不开展大字号专项和延期的中英混合语音验收。

执行时先完成正在进行的二级页、草稿引用和实体分享客户端合包检查点，再优先接入本方案第一批，随后接续第二批可独立完成的本地实现。无需再次等待方向批准；缺少真实图片、设备或运营身份时记录实际缺口并继续本地编码。

## 2026-10-19 增量规则：独立点与累积读取

已核对用户在聊天 `01a14feb-51fe-70b0-b702-406e24edc0bb` 的原始要求及“把这些提交给codex进行编辑”的明确授权；该要求接入原 `BT-FIX-NOW-UI-001`。本节覆盖早期材料中的空间聚合、同坐标合并、地点照片以及替换式地图页规则，其余权限、自动地图路由、Now 入口和既有发布门槛继续有效。原材料与失败证据保留，不把参考原型中的示例数据写入真实环境。

- 一个稳定 Place ID 显示一个分类圆点，承接当前访问者有权读取的关联动态；不同 Place 即使重合也不合并。真正独立、获准的地理动态使用独立方点，不能仅凭同一 Moment ID 或坐标相等吞掉另一条合法地理关系。同一已表示的 Place 关系不重复画成独立动态。
- 按实际米/逻辑像素和实际可用空间分级：近景类别、有限名称/数量以及真实动态缩略图；中景先收起辅助行和名称；远景或拥挤时保留圆形7dp、方形8dp微点。选中对象保持较大图形。名称预算和媒体预算不能删除基础点，不生成数字聚合球，不挪经纬度。完全重叠对象沿原选择器逐个进入；48dp触达与微点图形大小分开。
- 默认点集只包含获准且有内容的 Place 和独立动态，不铺满全 POI 目录；搜索、收藏、旅程、主题沿各自入口语义。位置来自现有授权合同，不猜中心点、EXIF或拍摄地点。
- 传输仍按每页最多40项读取；客户端自动有限续页，并把基础点按稳定 ID 累积。当前批默认最多4页，后续“继续加载”追加点；当前活动点集与8视图/320点的机会缓存分开，另受2048点/16MiB有限预算控制。预算到达明确提示已加载位置保留、需缩小范围，不把部分点称全量完成；没有随机采样或按标签数量截断。
- 空续页、重复游标、失败、来源变化不能进入无限循环，也不能清空仍获准的已加载点。完整新权威结果、撤权、过期、账号/组织/城市切换及迟到响应仍按原授权边界退休；旧原生像素不能借新读取延长原期限。
- 拖动和缩放只更新投影及可见媒体兴趣，不自动提交新的搜索区域；首次有效视图、明确目的地导航和用户“搜索此区域”才改变内容查询范围。详情返回保留相机和选择，同一原生地图不因键盘、查询或标记更新重建。

本轮按正常字号仅验收手机。用户明确将 Web 保留未验收，并暂不做 TalkBack；两者写 `NOT_RUN`，不伪称通过。本节不追加录屏要求；原先获准的真实录屏、跨区域证据和历史失败继续保留。实现须以当前报告中的真实测试、构建、部署与设备记录为准，规则写入本身不代表完成。

## 用户结果

用户在地图上能辨认地点是什么，知道这里是否有近期公开动态，并沿原入口读到相关内容。公开动态有获准图片时，使用方形照片表达内容；没有图片时，使用同形状记录符号。地图、结果卡、轻预览、详情继续使用同一对象 ID。

本轮保留 Now 联系与通知、短头像列、局部透明材质、唯一输入框和当前底图。图片用于表达内容，不新增常驻大面板、收件箱入口、底部立体分类条或第二张地图。

## 当前实现和真实差额

| 能力 | 当前工作树 | 本轮动作 |
|---|---|---|
| 地点受控照片 | 138/140 合同及详情、结果、收藏、Now 轻预览已有接线 | 复用获准图片投影到地图，避免重做上传体系 |
| 地点关联动态 | 已有最近 30 天公开摘要、真实计数和分页入口 | 补地图摘要字段及简短标签 |
| 地图实体及详情 | typed map 和原 Place/Moment ID 路径已有 | 保留原实体与导航 |
| 地图媒体 | Go map Item、TypedMapItem、MapEntity 无媒体字段；地图仍是类别符号 | 补服务端投影、客户端媒体状态及各渲染路径 |
| 公开动态图片 | PublicMoment 仅承载公开文字与地点语义 | 第二批补作者明确批准的公开派生图及详情、地图接线 |
| 动态位置 | 当前取关联公开 Place 锚点，不表示作者当前位置或实际拍摄点 | 继续使用地点关联语义，不推断拍摄坐标 |
| 密集及同坐标 | 保留原 ID、真实锚点、淡点与当前对象选择器 | 在现有策略上改视觉，不恢复数字聚合和点位移位 |
| 未读状态 | 未接权威未读修订与查看回执 | 本轮不画“未读红点”，不以关联数量推断未读 |

基线见 `docs/reports/BIRDTIE-CODEX-EXECUTION-2026-10-14.md` 的 B1、B2 和当前合包记录；不要沿用 10 月 13 日报告中已经被覆盖的媒体缺口。2026-10-14 06:37 的真实 Now 预览图为 `docs/testing/evidence/remaining-execution-20261014/batch-c/device-copy-bc4-observe01/phone-after-install.png`，其正常渲染证据不代表本方案已实施。

## 标点和标签

| 状态 | 地点 | 动态 | 文字与点击 |
|---|---|---|---|
| 普通 | 28dp 圆形获准照片，1.5dp 轻边框 | 28dp 方形获准照片，4dp 圆角；第一批先保留诚实记录符号 | 普通完整标签每手机视口最多 6 个，优先选中与当前任务对象 |
| 选中 | 36dp 圆照片，2dp Birdtie 深绿选择环 | 36dp 方照片，保留方形区别和选择环 | 当前名称最多两行；有计数时第二行显示“近30天 · N条动态” |
| 密集 | 16dp 淡圆点 | 16dp 淡方点 | 不塞不可辨认的小照片，省略普通标签；所选对象仍为 36dp |
| 缺图、读取中或失败 | 同尺寸圆形分类符号 | 同尺寸方形记录符号 | 地图和点位立即可用；图片失败不移除仍合法的对象 |

标点边框统一、无气泡尾巴、重阴影和厚底座；普通点与所选点的轮廓在当前底图上清晰可辨。标签沿用 Birdtie 文本 tokens，名称与数量正常字号均不低于 12sp，主次靠字重和颜色区分，候选标签宽度 144dp；长内容在原详情完整显示。实际窄屏布局需要时可缩减普通标签数量，不能通过缩小必要文字或隐藏对象处理。

标签可以退让，坐标不退让。只对当前来源允许显示的原对象做密集降级，不能因图片预算或碰撞删除点，也不能重新生成绿色数字聚合球。小图形保留不小于 48dp 的 Android 有效操作范围，iOS 遵循至少 44pt 的既有边界；命中重叠时使用当前对象选择器。

同坐标对象保持真实锚点和稳定叠绘顺序。选择器中的每条内容保留名称、类型、当前获准缩略图和独立详情入口；选择前再次确认当前身份、城市、来源和对象仍有效。不得重新采用扇形展开、假坐标、引线或“放大后才有入口”。

## 第一批 地点照片和关联动态

### 服务端投影

1. 扩展 mapprojection 与当前严格 wire 合同，在 base PUBLIC 的 PLACE 项加入当前获准 `placePhoto` 和近期动态摘要。字段名和复用类型以当前代码为准，不另造 Place 媒体实体。
2. 照片 metadata 复用原 Place 照片合同，绑定 canonical Place ID、照片 ID/version、SHA、当前图片权限、保留期限和署名。只读取当前可公开派生图，旧外链和私人 Moment 附件不能成为地点封面。
3. 计数定义为“最近 30 天、当前访问者仍有权查看、作者明确公开且关联该 Place 的动态条数”。复用 Place history 的权限过滤和时间窗，服务端对完整合法成员集合计数，不使用地图最多返回的项数、已下载页长度或展示卡片数作为总数。
4. 计数成功为 0 时省略提示；未读取、读取失败或来源失效为未知，不显示 0。当前作者范围第一批不加全体作者数字；以后若加入，必须保留所选作者范围语义。
5. 当前 DTO 严格字段/版本校验，API 和各客户端需要协调升级或支持明确的旧版本读取。编译与本机切换不能留下旧客户端因新增字段整张地图不可读的状态。
6. 照片加入当前 snapshot/revalidate 后，保留原读取租约再比较，参考 `place_photo_projection.go` 的 PreserveLease 思路，防止新 capture 的 expiresAt 自然变化造成 false source-changed。不能通过放宽所有来源比较解决。

计数按已有时间窗与权限进行批量读取，避免每个标点独立请求 Place history。关联动态提示是阅读摘要，不是热门、未读、到访证明或“正在发生”的信号。

### 客户端和渲染

将照片和动态摘要接到 TypedMapItem → MapEntity → 原 MapCanvas；媒体只修饰当前地点实体，不创造新 ID 或改变经纬度。接入一份有界的地图媒体状态和共享视觉参数，provider 只负责投影、像素适配与命中。

- Mapbox Android/iOS：`city/native_city_map_io.dart` 与 `workspace/map_marker_bitmap.dart`。
- Mapbox Web：`city/public_city_map.dart` 的 WebMapDisplayLayer → MapEntityLayer；不要误把 JS 底图文件当作实际 Flutter 标点入口。
- AMap Web：`city/amap_city_map_web.dart` 与 `web/birdtie_amap.js`。bridge 只接收到经过校验的临时像素，不传认证头或私人媒体 URL。
- 其他条件未齐的 provider 保持现有诚实降级，源合同和共享状态仍同步适配；不可宣称未运行的 provider 验收通过。

图片到期或撤回时退回同一地点的类别符号；若地点本身同时失权，按原来源退休整个对象。两者不能混成同一种删除。图片失败不重置结果、所选 ID、草稿或地图视口。

### 点击

地点照片点 → 原 Now 地点轻预览 → 关联动态入口/完整地点 → 同一公开动态详情。数量提示复用同一阅读入口，不另建 Feed 页面。动态点 → 原 PublicMoment 详情 → 关联地点/作者公开内容。返回时保留原相机、选择和草稿；拖图、开键盘或图像解码不重建地图。

## 第二批 公开动态照片

先补完整公开图片合同，再接方照片标点。作者必须明确选图并批准公开派生图；图片公开与原文字公开分别受具体 Moment revision、media ID/version、权利、受众、当前会话/权限及撤回约束。不能因为文字公开而自动公开私人原图、私人附件或 EXIF。

先贯通公开动态模型、API 和原详情中的实际图片，再把同一当前获准缩略图投影到地图。只有照片详情、权限和来源均合法时画方照片；文字动态、无图和权限失效图片使用方形记录符号。来源撤回、删除、屏蔽和身份变化后，详情和地图同时退休旧图片。

当前 MOMENT 锚点继续是关联 Place。面板明确区分“关联地点”和“拍摄位置”；本轮不会从 Place 坐标、EXIF、照片内容或作者资料猜拍摄点。若以后有独立、明确授权的拍摄点合同，再按原需求接入，不能提前伪造位置。

公开媒体实现复用现有受控派生图、批准、当前读取和原操作恢复模式，保留原 Moment writer 与领域动作。涉及新迁移时，执行者重新读取最新编号并登记精确范围，不修改已应用迁移，不对真实生产库运行。

## 缓存和异步

现有 MapMarkerBitmap 的 128 项类别缓存 key 不足以区分照片。新的图像/合成 bitmap 缓存需要区分实体与媒体 ID/version/SHA、当前权限绑定、DPR、形状、密集与选中状态；同类地点不能互相串图。缓存同时限制项数和总字节，并采用当前可见点的有界临时内存；不新增持久磁盘缓存或保存 token/批准。

复用 map_layers_controller 的当前读取租约、前台可见状态、单次暂时错误恢复与来源退休，复用既有媒体 JPEG/SHA/大小验证；不为每个标点新增轮询。相同图像并发读取合并，初始并发最多 3，缩略图按实际派生尺寸解码。相机每一帧不重复下载或解码。

换账号、组织、城市、范围、来源或媒体版本时，旧异步读取和解码必须核对 serial/scope/generation 后才能绘制。图片到期、撤权和删除同时清除内存展示与旧 native annotation 图像，临时 bridge 资源回收。照片加载完成只能更新对应现有标点；原 annotation 差分与串行刷新保留，不整批删除重建。

## 代码定位

| 链路 | 优先检查的原文件 |
|---|---|
| 地图领域/API | `apps/api/internal/mapprojection/model.go`、`internal/postgres/map_projections.go`、`internal/httpapi/map_projections.go` |
| 地点照片 | `internal/placephoto/model.go`、`internal/postgres/place_photo_projection.go`、客户端 `workspace/place_photo.dart`、`workspace/entity_cover.dart` |
| 关联计数 | `internal/placehistory/model.go`、`internal/postgres/place_history.go`、`placehistory/page.go` |
| 地图模型/状态 | `apps/client/lib/src/workspace/map_layers_api.dart`、`map_layers_controller.dart`、`map_entities.dart`、`map_canvas.dart`、`map_display_policy.dart` |
| 绘制 | `workspace/map_marker_bitmap.dart`、`city/native_city_map_io.dart`、`city/public_city_map.dart`、`city/amap_city_map_web.dart`、`apps/client/web/birdtie_amap.js` |
| 阅读入口 | `workspace/map_workspace.dart`、`now_place_preview.dart`、`now_public_place_panel.dart`、`place_detail_sheet.dart`、`city/map_point_selection_sheet.dart` |
| 公开动态媒体 | `apps/api/internal/content/public_moment.go`、`internal/postgres/public_moments.go`、`internal/httpapi/public_moments.go`、客户端 `workspace/public_moment_api.dart`、`workspace/public_moment_page.dart` |

以上是定位入口，不是预授整目录写权限。先核对最新文件归属，协调当前共享写入，再声明所有将修改的精确文件。

## 原任务接续和来源

唯一 live 队列仍为 `automation/codex_task_queue.json`。将本轮作为正在进行的 `BT-FIX-NOW-UI-001` 消费界面增量，引用原 `BT-V4-MAP-001` typed layers、`BT-V4-MAP-002` 生命周期和 `BT-V4-MOM-002` 地点聚合来源；历史 DONE 不重开，本轮局部通过也不把 PARTIAL 自动解除或整项标 DONE。不创建替代功能队列，不将本方案批次当作新的独立 live 任务。

只有原执行根代理维护队列和共用报告。实施前用当前持久锁/读取 hash 保护的已有机制增量登记 scope，保留所有旧 lease、owner、历史和任务对象；不要 reset/release 已有 lease 来给新文档腾范围。独立 worker 只在明确分配的文件内写入，涉及共享 DTO、DDL 和媒体真源时按真实依赖串行衔接。

本执行方案原件：`C:\Users\chens\Documents\Codex\2026-10-13\d-project-birdtie-docs-testing-evidence\outputs\BIRDTIE-MAP-CONTENT-EXECUTION-SPEC-2026-10-14.md`。执行根代理登记新文档范围后，将明确选定的本文件复制到 `D:\Project\birdtie\docs\product\BIRDTIE-MAP-CONTENT-EXECUTION-SPEC-2026-10-14.md` 并记录原始与目标路径，再在 `docs/architecture/CIVU-REUSE-MATRIX.md` 增量记录本次视觉概念借鉴和实际接线。只复制这份方案，不搬 Civu 业务代码、依赖、用户动态、媒体、分类素材或 Session。

Civu 只读参考：`D:\Program\Civu\Civu-client\lib\src\widgets\explore_map_marker_icon.dart`、`persistent_map_host.dart`、`services/explore_map_marker_layout.dart`、`state/persistent_map_scene.dart`。采用的是圆地点、方动态、轻标签与稳定详情概念；Civu 的红点已读机制、地点动态去重和缩放预算不自动成为 Birdtie 契约。

原视觉参考是用户提供的 `C:\Users\chens\Documents\xwechat_files\wxid_10l40cmwgt8y12_87f3\temp\RWTemp\2026-10\9e20f478899dc29eb19741386f9343c8\cedfd72373d42a82273cf6476fba564e.jpg`。它只作视觉依据，不证明截图版本的实际权限或点击行为，也不授权导入其中内容。

## 合批检查和交付

遵循 `docs/ux/GLOBAL-UX-INTERACTION-CONTRACT.md`，适用 UX-CHECK-01/04/05/06/07/08/09/10/11/12/13/14/15/16；只执行本次修改实际涉及的条目。正常字号下验证长名称、窄屏、键盘、操作可达和读屏语义，大字号不属于后续范围。

每批完成关联接线后，再统一运行直接相关单元/widget 检查。服务端覆盖真实计数与权限过滤、严格 schema/版本、照片租约稳定、媒体与坐标独立退休；客户端覆盖真实 JPEG 像素、缺图/失败、同类不同图、版本变化、迟到解码、身份与城市切换、图片过期、选中保持、密集与同坐标选择、原导航和键盘不重建地图。公开媒体新写入另覆盖具体版本批准、取消、撤回、重复提交与原操作未知结果恢复。只有变更风险所需的定位检查才增补，不重复全仓测试、全量分析和数据库整检。

每批产出一次当前源 API/APK/Web 合包检查点，注明本次确实编译的对象和未编译平台。现有可运行调试会话可热重载观察 Dart/UI；原生、依赖、编译期配置或交付新包需要时才重建。合包与当前 QA 服务版本对应，不能用旧包的照片或检查结果冒称新接线。

同数据、同相机、同尺寸、正常字号保存必要前后截图，合批覆盖普通照片、选中、密集、同坐标、无图/加载失败、键盘开合和图片权限退休。隔离样例必须明确标识；真实操作截图和模拟 inset 分开。没有真实获准媒体时，不把 AI 样例或私人图片映射到实际地点。无设备或真实 provider 时记录 NOT_RUN，不等待该条件才继续本地实现。

每批交付：实际改动与原任务映射、相关检查命令/cwd/退出码及覆盖范围、保留的首失败与修复、源码与产物路径/SHA、前后截图及其来源、剩余实现和 NOT_RUN。第一批完成不能宣称公开动态照片完成；第二批编译不能宣称真机、媒体生产链或全站体验验收通过。Closed Pilot/Consumer Beta 门槛继续有效。
