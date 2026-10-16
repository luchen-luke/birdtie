# Now 地图与动态阅读修复（2026-10-16）

本轮针对用户提供的三张真机截图继续修复：跨地点数字聚合、拖动重复加载、动态阅读排版及作者/原时间缺失。属于既有 `BT-FIX-NOW-UI-001` 的增量，不表示此前全部功能和迁移任务完成。父任务保持 IN_PROGRESS。

## 基线与问题原因

- 工作树基线：`codex/birdtie-import`，`556c56f6b9964c536468d1854a3a103a6329df1f`；开始时 APK/Web29、API26、schema171。保留现有修改，没有 reset/clean。
- Civu-client 本地与 GitHub HEAD 同为 `1bbf0613b513001447f326225c944c1a1fbd8e8f`，工作树干净；Civu-server HEAD 同为 `0489536a0eb0792deff7793c8b143ad3e741d5ce`，原工作树已有修改。原项目只读，相关服务端文件最终重算摘要一致。源码阅读不是旧 Civu 运行验收。
- 截图中的绿色数字块、圆形地点照片和方形动态封面来自 Birdtie 业务标记；道路、公交/铁路标识、地名和 Mapbox 标识来自底图。数字 2/5/7 是原有按 zoom 网格生成的业务聚合，不代表同一真实地点。
- 旧相机处理把每个新矩形当成一份新视图；已有覆盖范围无法复用，既有内容被先清空。恢复到期内容也可能取消正在续期的请求，留下空白。
- 旧阅读面板缺作者输出；迁入私密 profile 并不等于还原原公开动态附带的作者归属。旧时间不能用迁入时间代替。

## 本轮行为

1. 不同真实坐标不再按 zoom 网格合并。只有坐标、坐标系及精度完全一致才形成同点集合；显示真实成员的小封面和角标。动态方形、地点圆形，普通/选中封面约28/36dp；触控区域仍48dp。
2. 每次先按视口服务端查询并过滤权限，再返回有界结果；每页最多40个真实坐标组，更多通过下一组/上一组访问，不加载全城后在客户端筛选。已迁入的独立坐标动态仍不生成 POI。
3. 客户端按已覆盖矩形求差，仅补读新条带；有效范围内往返和缩放直接复用。缓存上限8份视图、320点、2MiB元数据；封面上限24份/24MiB，最多12个候选、4路请求、160px解码。身份/组织/权限变更会清退；到期只移除过期授权范围，立即补读缺口，保留新条带和正在进行的续期。
4. 地图实例常驻，标记增量更新；已缓存且来源未变的封面不会因拖动重新下载。后台权限续期仍需要接口读取，不能把“缓存”解释为永久跳过权限检查。
5. 阅读保持在 Now 主面板：作者与头像、可信原时间、正文、完整比例图片、多媒体分页、同面板查看大图/返回；标题与正文相同时不重复。视频先展示封面，明确点击后播放。缺失作者/时间保持诚实占位。
6. 主动搜索、收藏、旅程、主题地图、活动的其他有效图层保留；没有迁入 Civu 导航或替换地图引擎。

## 原作者补适配

schema172 为**已经迁入的83条原公开动态**补充不可变作者归属，涉及9位作者、6个有保全证据的头像（128×128 JPEG，共32,295bytes）。这不是新建83条动态，也不表示所有旧数据已完成迁移。

原称呼/原创建时间绑定认证备份、原 note/user 摘要和当前 native ID；不公开当前私密 profile，不迁入邮箱及由邮箱推导的 handle。头像只经当前可读动态的专属受控路由读取，每次重新鉴权。6张头像均经过真实HTTP读取和摘要验证；2位作者原本无头像，另1个外部头像没有可证实的保全对象，继续缺省。

当前 Birdtie 独立库实际提交、inspect、幂等重放通过，原 moments/places/profiles及Civu备份指纹不变。只新增172，原1–171迁移摘要不变。受控头像原图留在仓库外，Git只包含代码和脱敏证据。

## CIVU 复用映射与实际改动

所有 Birdtie 路径相对于 `D:/Project/birdtie`，Civu 路径相对于只读 `D:/Program/Civu/Civu-client/lib/src`；为能力与语义适配，不宣称整页源码直接复制。

| Civu参考 | Birdtie实际路径 | 适配内容 |
|---|---|---|
| `state/persistent_map_scene.dart` | `apps/api/internal/postgres/map_content_spatial.go`、`apps/api/internal/content/map_content.go` | 独立位置/具体动态条目、真实同点、稳定选择器、视口分页 |
| `services/explore_map_viewport.dart` | `apps/client/lib/src/workspace/map_content_coverage.dart`、`owned_map_controller.dart` | 视口求差、跨日期线、取消、防抖、有界缓存、续期缺口修复 |
| `services/explore_map_marker_budget.dart`、`widgets/civu_explore_map_mapbox.dart` | `owned_map_media_controller.dart`、`map_marker_bitmap.dart`、`map_entities.dart`、`apps/client/lib/src/city/native_city_map_io.dart` | 小封面预算、真实媒体、增量更新、小同点角标 |
| `widgets/persistent_map_host.dart` | 既有原生地图宿主、`apps/client/lib/src/workspace/map_canvas.dart` | 地图常驻、分页可达，保留其他业务图层 |
| `widgets/civu_discover_media_post.dart`、`civu_moment_feed_card.dart` | `apps/client/lib/src/workspace/now_map_content_panel.dart` | 作者/头像/时间层级，正文去重，媒体主导布局 |
| `widgets/civu_moment_media_viewer.dart`、`civu_media_viewer.dart` | 同文件 `_MapMediaPager`、`_LeasePicture`、`_MapVideo` | 仅当前页解码、比例展示、放大、页码恢复、明确播放 |
| Civu-server `api/internal/store/note_types.go`、`notes_postgres.go`、`moment_browse.go` | `apps/api/internal/postgres/map_content_authors.go`、`internal/legacymomentauthor/`、`legacy_moment_author_restore.go`、`migrations/172_legacy_moment_authors*.sql` | 原公开动态最小作者归属与原创建时间；保持当前权限边界 |
| Birdtie现有头像与媒体契约 | `apps/client/lib/src/workspace/owned_map_api.dart`、`apps/api/internal/httpapi/map_content.go`、`apps/api/internal/postgres/map_content_media.go` | 严格路径、来源、摘要、尺寸及权限验证 |

新增旧图片阅读适配：`apps/api/internal/postgres/map_content_display.go`及`map_content_display_test.go`、`map_content_display_native_test.go`；客户端`apps/client/test/content_map_display_test.dart`覆盖严格display描述解析。

其他改动：`apps/api/cmd/legacy-moment-author-restore/`、`native_database_startup.go`及对应测试；`scripts/enable_birdtie_response_compression.py`、`scripts/deploy_birdtie_public_entry.py`；任务队列及本报告。完整逐文件提交列表以 Git diff 为准。

## 测试与线上证据

证据根目录：`docs/testing/evidence/content-map-refinement-20261016/`。保留失败尝试，不用重跑次数冒充新增测试数。

| 检查 | 实际结果 |
|---|---|
| 客户端控制器/覆盖/阅读/标记/严格头像解析 | 25个唯一测试通过；`client-tests01`、`render-tests02`及reader/parser回执。render首次1项旧尺寸断言失败，修正预期后通过，失败日志保留 |
| 部分条带到期且前一次续期失败 | 新增回归PASS；覆盖/控制器合批7项PASS，`client-expiry-tests.log` |
| 新display路径严格范围/字节/尺寸/来源验证 | 新增回归PASS，阅读合批5项PASS，`client-display-tests.log` |
| 81个不同坐标、40/40/1分页、当前页续期 | 客户端及真实隔离PostgreSQL测试PASS |
| 同点81成员、续页重查权限 | 隔离PostgreSQL PASS；线上数据无同点集合，真实线上同点翻页NOT_RUN |
| 无place锚点/坐标撤销/退出拒绝 | 原生库测试及当前线上原账号读取PASS |
| 172导入计划/不可变归属/私密profile隔离/阻断与注销 | 直接测试及隔离PostgreSQL PASS，`backend-build28` |
| 启动拒绝旧/残缺schema | 精确父测试及29子用例PASS，包括两个172新增拒绝用例；fake probe，无生产库写入 |
| 当前线上9作者、9原时间、全部6头像 | PASS，`backend-build28/live01`；并非83条详情全部逐条HTTP重跑 |
| 低/中/高zoom、世界有界分页、16个原锚点可达 | API27真实读取PASS：3个zoom均7真实点；world两页40+15点、43条去重内容 |
| 原始Civu运行、全仓回归、1万候选新SQL压力重跑、跨设备撤权推送 | NOT_RUN |
| 本批完整创建/串文发布/上传失败重试链 | NOT_RUN；本批是地图与阅读增量，不能用静态页面替代发布验收 |

## 性能实测及限制

原城市列表405,909bytes，手机未压缩下载曾用59.117s并超过10s读取期限。网关启用gzip/zstd后gzip约40,835bytes，减少约89.94%，解压内容摘要一致，no-store保留。APK30失败记录保留，后续启动真实加载成功。压缩不改变图片字节。

相同手机、相同四次东西拖动手势脚本，各约11.2s：

| 项目 | APK29/API26 | APK32/API28 |
|---|---:|---:|
| 该窗口内容接口请求 | 2 | 1 |
| 内容接口实际耗时ms | 1192.604、1792.035 | 618.203 |
| GPU Draw p95 ms | 2.323 | 2.468 |
| GPU Draw >16.67ms | 0/329 | 0/329 |
| PSS开始→结束KiB | 484135→509949 | 514316→509846 |

本次手势起点没有保存完全相同camera矩阵，且接口返回内容从数字块改为真实封面；不是严格等负载A/B，也不能声称帧率或总内存显著提升。APK32计数快照：地图创建保持1；封面请求/解码均保持7，已有图片没有因这次拖动再次下载。指标前后间隔包含后台续期，不能把该间隔全部HTTP计数当成仅手势窗口。

服务端同bounds/zoom、各3次的API26→27中位数：wide133.117→230.852ms，phone121.704→219.058ms，adjacent121.307→219.446ms；JSON约4KB→10.4KB。新接口实际返回7个真实位置/9条内容/7封面，恢复真实内容增加了处理与返回成本，**不宣称API本身提速**。world首40点4,071.897ms，近视口3点155.473ms。API28只做单次smoke，未重做该A/B。

## 最终实际版本与阅读图修复

| 交付物 | 实际版本与SHA256 |
|---|---|
| API | build29 / schema172；`73a97552316ae642f4b97aea37c04266c3fd0959eeb0d346dd03bc6e54702818` |
| Android | APK33，已安装、启动、回读摘要一致；`ad30ee1401d9b154e77e2f91f5e908427e31f15cf8462c77a70588807e9b4646` |
| Web | Web33 已部署；canonical manifest `5bb2eb4d68e60c3c5d763df718a00766cb65cfec3e5098385579b46a53c5ac1a`；main.dart.js `c5091b591ae2beeda358ec1f431d3fe27b72df391990a56cced6848280ce2771` |

API29继续使用172，无新DDL。API28发布原作者归属；API29为旧`private-image`增加受权限保护的阅读图，最长边1280px、最多256KiB、保持完整画面和比例，不替换数据库原件。缓存32份/8MiB，描述缓存4096项，两路解码。客户端严格验证图片类型、来源、路径、尺寸与字节上限；视频不能冒充阅读图。较复杂图片可能降低质量/分辨率，透明区域转白，不新增原件下载入口。

实际旧图1,519,753bytes、5760×7680 → 54,903bytes、960×1280，减小96.387%。真实HTTP三次摘要一致，原件未变；服务端侧58.704/58.221/57.613ms。此前手机原图读取9.509/12.409s与这个服务端测量位置不同，不能直接换算为端到端提速倍数。Web另一张真实图为239,313bytes、960×1280并通过摘要核对。

最终客户端相关唯一测试共27项PASS；最终改动文件分析0 error、0 warning、40 info。API29两项直接测试和一项隔离PostgreSQL测试PASS（完整画面限制、缓存隔离、当前权限）。首次native fixture双时间读取违反约束的失败、首个未部署build及修正重跑均保留。

## 最终手机和Web实际操作

- 手机APK33：独立坐标小封面、真实地点/对应动态、作者/头像/原时间、图片2/4、大图与返回保留页码、明确点击视频后播放到52.081s片尾、退出并缩放原地图，均已实际执行。地图创建计数始终1，封面请求/解码始终7；长会话的内容HTTP仍含后台权限续期。截图和动作语义见`phone33-acceptance.json`。
- 视频中段工具读取到26.007s正在播放；保存的`phone33-video-progression.json`两次均为片尾，不能冒充持续推进采样；截图`video-ended`也标为片尾。Web近片尾51.8s有相同横转场景，未发现Android独有的方向错误；未审计源编码历史。
- Web32实际普通canvas往返拖动180px，返回截图摘要一致，mapQueries23/markerReads14不变，两个方向均无新媒体请求；原地图DOM未重建。强制Flutter semantics模式下拖动没有移动地图，该模式的零请求不计为缓存通过。
- Web33实际阅读图、作者、原时间、头像、大图与返回同一DOM通过。有效Aberdeen视频明确播放后0.836→12.029s、406×720、error=null，关闭后视频节点为0，原地图DOM保留。测试新建会话均已注销；手机调试HTTP记录已关闭清空，ADB VM端口已移除。
- 原图读取慢、APK30城市列表超时、APK31部署期间续期空白、测试脚本守卫错误及Web33首次城市入口失败均保留，不抹去失败历史。

## 剩余缺口与验收边界

1. 当前4条地图视频中3条目标城市没有有效地图视图；汕头入口实际失败（无地图容器），未伪造城市范围。另1条Aberdeen视频已完成手机/Web实际播放。这一城市入口缺口仍需补齐，不能把备用视频成功当成原失败已修复。
2. 200张阅读图首次冷加载性能NOT_RUN；详情大量图片可能造成有界缓存淘汰及同步生成成本，不能据单图结果声称大集合完成性能验收。新10k候选SQL压力、严格同camera/同内容手机A/B、真实跨设备撤权推送、iOS、本最终包完整发布/串文/上传重试链均NOT_RUN。
3. 一个旧外部头像没有保全对象，继续缺省。schema172通过的应用导入器严格匹配83/9/6，数据库JSON计数CHECK对直接手写缺key SQL的额外收紧尚未实施；本轮未通过这种旁路写入数据。
4. 权限到期仍重新鉴权，因此不承诺已经访问过的区域永久不发请求。缓存及真实手势结果只说明有效授权内复用覆盖范围和封面。
5. 本轮仅地图/阅读增量；此前所有数据适配、全部CM09和父任务不因此改为完成。`BT-FIX-NOW-UI-001`继续IN_PROGRESS。

适用UX-CHECK-04/05/06/10/12/13/14/16：同实体跳转、真实作者与未知占位、迟到/过期隔离、返回恢复、既有面板、正常字号真实手机布局以及脱敏计数取证。本批不新增发布副作用；辅助技术完整流程及跨设备恢复未实测，不声称全套UX-CHECK通过。按用户要求未录屏、不安排大字号检查。

源码、测试回执、脱敏计数及本报告进入本次Git提交；用户内容截图仅保存在本机outputs，不上传Git。具体提交号由提交完成后的外部交付回执绑定，避免用尚未发生的提交充当证据。
