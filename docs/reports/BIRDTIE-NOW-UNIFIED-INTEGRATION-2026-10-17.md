# Birdtie Now 整合接线与合批编译 — 2026-10-17

本轮在官方 `D:\Project\birdtie` 实施，分支 `codex/birdtie-import`，基线 `f9645afe4060abe3538e49dbeb34bc4f07a891ad`。开始时只有用户解压的 `Birdtie-Now-Unified/` 未跟踪，其余工作树干净。保留其他worktree、原256任务、历史DONE/失败和门槛，未领取新任务。全部增量并回原 `BT-FIX-NOW-UI-001`；R编号只作交付索引。父任务仍 IN_PROGRESS，整体体验仍 PARTIAL。

材料来源为 `D:\Project\birdtie\Birdtie-Now-Unified`。已先读 START-HERE、规格、S01–S10 PDF、18行XLSX、两份研究、素材manifest及候选patch。PDF/XLSX只读，渲染与提取在 `work/now-unified-20261017/`。原patch逐hunk核对后合入，测试旧v1 fixture改为当前v2合同，不回退产品接口。

## 实际接线与18项对账

| 索引 | 实际实现/保留 | 本轮边界 |
| --- | --- | --- |
| R01 | 以官方HEAD对账、保留原任务与root lease，3文件候选补丁增量合入 | 无reset/clean/整文件旧版覆盖 |
| R02 | 新目的地长sheet不透明、沿现主题与中文字号；轻材质仅沿原入口 | 全站视觉验收未重跑 |
| R03 | 短预览固定屏幕内左右16dp，原生anchor临时缺失和相机移动不改变预览位置/State | 正常字号widget通过；本轮已装机启动；此预览专项真机NOT_RUN |
| R04 | 保留Now对话/草稿与地图实例，目的地单独焦点；原实体直接路径不变 | 实际draft/键盘/取消用例通过 |
| R05 | 地点真实总数与代表条目分开，展开继续原权限分页，不将preview称全量 | 相关保留/展开用例通过 |
| R06 | 沿原 PLACE_CONTENT/MOMENT_ANCHOR/CLUSTER 及真实对象ID，串文关系独立查询 | 不把坐标动态当商店，不猜串文 |
| R07 | 图片横竖和视频沿固定媒体外框，紧凑144dp；展开至多360dp，媒体页数单独标注 | 两种比例图片切页通过；真机视频首帧NOT_RUN |
| R08 | 新只读map-content thread链按权威成员顺序连续渲染，逐段正文/媒体/时间/位置；不可读段明确占位 | 代码/本地合成用例；API33已部署，真实授权串文读取NOT_RUN |
| R09 | 重叠对象短列表高度按当前合法成员数收紧，选中后原reader | 不扩大命中或伪造聚合总数 |
| R10 | 图片加载失败有真实清缓存重试；工作区/权限/迟到响应沿原退休边界 | 404区分旧路由与领域撤权，后者必须退休grant |
| R11 | 复用已有lazy descriptor；当前reader仅缓存明确读过的最多4图/1MiB，翻回不重传；身份/来源/期限变更或销毁清零 | 两图三次翻页仅2次body请求；真实设备冷启动/性能阶段计时NOT_RUN |
| R12 | 保留封面/按需显式播放；串文视频刷新改为同权威thread上下文，整串租期与媒体grant一同刷新 | 不以原生播放未运行称首帧性能通过 |
| R13 | 保留当前session恢复、地图provider/CRS/生命周期守卫，不猜原因改底图 | 装机当前Aberdeen地图正常；两类历史底图空白专项复现NOT_RUN |
| R14 | 只执行直接相关检查并最终统一APK/API构建；保留每轮失败 | profile APK与Linux amd64 API合批构建通过；后续按用户要求装机、部署API33/schema177 |
| R15 | 新 City.artwork metadata、独立descriptor/content、177精确系统绑定、324px透明派生；三城受控本地素材 | 原档案不变，生产177及两城精确绑定、公网图片核验PASS |
| R16 | 108px contain纪念品SliverGrid，普通手机两列、真实近期ID、全部/国内/境外；无图中性图，按需构造 | 1067城市仅构造可见区测试通过；卡高196为长名/地区留正常字号空间，未照搬旧238 |
| R17 | 复用既有300ms防抖、IME、abort、generation、预算、同意与署名；分类只过滤目录 | 国内分类下仍可搜索Paris，未再次跑付费provider矩阵 |
| R18 | 目录身份与Temporary镜头分开；无map先解析再显式消歧；成功后才切图，内存近期只存真实ID | 不持久化provider坐标/bounds，拒绝错国家/层级/供应商；读取布局按账号/目的地/源版本有界记忆 |

主要文件：`apps/client/lib/src/workspace/map_workspace.dart`、`now_map_content_panel.dart`、`owned_map_api.dart`、`owned_map_controller.dart`；`src/city/{public_city_controller,destination_search_controller,browse_destination,destination_artwork,destination_artwork_view,destination_picker_grid}.dart`；Go `internal/{cityartwork,foundation,httpapi,postgres,content}` 的对应 city_artwork/map_content_thread 文件及 `migrations/177_city_artwork_projection{,.down}.sql`。详细变更与SHA见最终receipt。

## 三城真实旧图

| 目的地 | sourceGeo → 真实内部City ID | 旧media / 原版本=原SHA256 | 原图与本轮结果 |
| --- | --- | --- | --- |
| 青岛 | `7a536573-67c6-42f0-9ede-c423fb5dba00` → `civu-geo-7a536573-67c6-42f0-9ede-c423fb5dba00` | `b768cc63-968c-4f07-9afc-914875b75aad` / `d2988c88b86453e1ead0c637ffeb85abdcfd89c9ce3894d12351a609b4429988` | 找回REQ-360新版，3351927B，1254×1254；324×324派生114324B已入APK，未降级REQ-357 |
| 济南 | `bbffd2af-68b4-49ee-bb01-5e5a7673aa9b` → `civu-geo-bbffd2af-68b4-49ee-bb01-5e5a7673aa9b` | `4929ecdc-e7a7-4e97-ba06-850899a01d67` / `69b9d3d653f3b7f64fd1423c54e18d73cb90f1186a824bb1cfe1319a835a4218` | 原2749346B，1254×1254；324×324派生217890B已入APK |
| 爱丁堡 | `66920ecb-9717-42bb-8320-a43a1d2851d2` → `civu-geo-66920ecb-9717-42bb-8320-a43a1d2851d2` | source media/version为空；包原PNG SHA `1cab9f1418801643a59ce4eaafd8b94be485c69d79b1465603ab653674a565e7` | 原242178B，512×512透明风笛，原字节复制为已选asset；不伪称第三份系统档案 |

两城原字节来自已保全的受限离线导出，只读提取；纪念品图片未重新绘制。青岛派生SHA `88030bc211d9882d144a24c2cecb355b1d26a7ab8ba1a236406433496e08404b`；济南派生SHA `3f92ee40c6947d33c2714da5691c40cb5514ed11806f7ccf6c710d1c317dcdc9`。三份systemArchives实际为青岛、济南、York；爱丁堡与这三份档案无强行关联。

177必须同run精确连接alias/system_archive、媒体版本/输入SHA/字节数，无匹配则不绑定；已存在新binding不覆盖。初次构建时未核验live；后续部署预检已在线核实这两城同run alias、系统原版本/SHA/字节数，177绑定与公网派生已启用。APK包含精确内部ID的已核实本地素材；远端有效新图优先，未知新版/非法metadata不退旧图。素材原始路径和去向同时记入CIVU-REUSE-MATRIX；源审计与导出receipt在 `docs/testing/evidence/now-unified-20261017/artwork-r15/`。

## 验证、失败与范围

最终命令、cwd、退出码、数量、日志、源码/配置摘要见 `docs/testing/evidence/now-unified-20261017/receipt.json`。直接相关入口：目的地search/map-target 12项、实际picker 2项、artwork最终7项、Now地图reader 3项；Go artwork、thread合同/HTTP/原生读取分别保留日志。重复执行不累加为新增测试量。

首次失败均保留：缺素材时bundle失败、URI基址query/fragment未清理、FutureBuilder更版旧snapshot与测试异步夹具、候选补丁v1 fixture/固定bounds与当前v2覆盖范围不符、旧碰撞fixture没有标签可用空间、接线初期展开重复读取。修复后仅重跑对应入口；没有删除失败测试或回退新合同。两处thread权限/刷新审查缺陷分别补直接负向检查。

适用UX-CHECK-01/02/04/05/06/07/08/09/10/11/12/13/14/16，14本轮按用户指定正常字号；没有恢复大字号专项或录屏。模拟HTTP/合成图片只在测试中，未进入产品内容库；素材缩略图实际view_image查看不算真实Birdtie截图。

后续安装与部署见下节。仍未运行：全部真实Now图卡与原生地图切换专项、真实provider新矩阵、真实认证/双账号与撤权、TalkBack/VoiceOver、真机视频首帧/两类底图空白/性能trace、177 down或整库恢复演练、独立原生SQL串文fixture、完整消费产品与发布验收。都不沿用旧APK40/42和API31/32结果填PASS。网页继续按既有用户指令延期，正常字号、不录屏，Closed Pilot与其他原门槛保留。

## 构建与实际交付

- 源码：`f9645afe4060abe3538e49dbeb34bc4f07a891ad` + 本次提交中的源码差异；构建时尚未commit，构建前后1575个记录输入的SHA完全一致。
- APK模式：profile preview / development；包名 `app.civu.civu_mobile.birdtiepreview`，versionName `0.1.0` / versionCode `1`，签名SHA1 `73840a17d0ca7b4bf41541b42cecb7a234d50523`，沿用原预览身份。
- APK：`D:\Project\birdtie\work\now-unified-20261017\Birdtie-Now-Unified-20261017.apk`（388746621B）；SHA256 `c16b53d96c74c9d0329dbf3999357359a05bf8f1d2c3f1ab2b1534c30baa7d0c`。原build输出 `D:\Project\birdtie\apps\client\build\app\outputs\flutter-apk\app-profile.apk`。Zip已核实3份目的地图asset及各自摘要。
- API候选：`D:\Project\birdtie\work\now-unified-20261017\birdtie-api-linux-amd64`，Linux amd64/CGO=0（36119320B）；SHA256 `9f90ab22c0b47abca7a74295f042951f14f30b5cb9f697d400a91a1798df19d1`。后续已部署为API33，线上进程摘要一致。
- APK配置路径：`D:/Project/birdtie/apps/client/.env.maps.mobile.provided.local.json`，SHA256 `f2c7755410093cf8592652ca200b12c17d0465f5aeaac1bbff064530ac9bda5a`；编译期目标 `https://47.110.154.133`。后续已在线核对API32/schema176并备份，事务执行177后切换API33。密钥没有写入回执。
- 命令：沿原APK42 `flutter build apk --profile --no-pub --no-tree-shake-icons` 和既有3个dart-define/地图配置；API `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -mod=readonly -trimpath`。准确参数、起止、cwd与退出码见build/receipt.json。
- 本轮构建调用一次APK和一次API，均PASS；最终直接Flutter共27项，不含重复复跑；Go artwork与thread各自记账，数据库SKIP单独为NOT_RUN。


初次合批编译阶段未安装/部署；用户随后明确要求“安装，部署，上传到github”，已按下述记录完成装机和服务启用。没有发送消息、发布用户动态或修改Civu/旧D:\BirdTie。装机与接口检查不等于全部真实界面/消费产品验收完成。


## 用户追加交付：安装、部署、GitHub

- 手机 `c641566b` 覆盖安装成功（`adb install -r`，保留应用数据），设备实际 `base.apk` SHA256与上述APK完全一致。冷启动 `Status: ok`，601ms，5秒后进程仍存活；仅检查该PID的crash buffer，无致命崩溃。真实截图 `install/launch.png` 可见Aberdeen地图、内容标记及保留的“巴塞罗那”草稿；未进行新认证、发送或发布。首次截图解析因Android在PNG前输出多显示器警告失败，保留失败说明，第二次保留提示并提取原PNG；这不是安装或启动失败。
- 后端从 `/opt/birdtie/releases/social176-20261016-build32` 切换至 `/opt/birdtie/releases/social177-20261017-build33`。上线API SHA256 `9f90ab22c0b47abca7a74295f042951f14f30b5cb9f697d400a91a1798df19d1`，进程与候选一致，loopback与公网HTTPS `/readyz` 均PASS；TLS校验未跳过。
- 独立Birdtie数据库身份/OID/owner/marker核验通过。迁移前新增独立custom备份502666857B，SHA256 `cc68faf2317c629836cabcfbd4d74617279aeebe8c2e1f6a9351a4a8d3b2a985`，`pg_restore --list`可读；备份保留服务器受限目录，未上传GitHub。原176条ledger文件摘要逐项一致；只新增177，ledger count/max均177。DDL与ledger同一事务，仅两份已审系统图绑定，原源元数据未变。
- 公网城市目录1067项；两城descriptor/content共4次200，尺寸324×324、字节数及SHA256与APK选定素材逐一相符；错误图版本404，新串文未认证401。此401只证明路由与鉴权边界，真实授权串文SQL/多账号验收仍NOT_RUN。
- API环境摘要未变，未修改Caddy/TLS公共路由。部署脚本具备就绪失败恢复API32状态/服务配置的路径，本次未触发；177是附加schema，未执行down或整库恢复，不把路径审查称回滚演练通过。
- GitHub目标为 `https://github.com/luchen-luke/birdtie` 的 `codex/birdtie-import`。仅精确提交本批42个源码、测试、3张选定图片、迁移和文档/队列文件；原始30文件整合包、私有导出、凭据、APK/API二进制、备份及设备截图均不入Git。实际commit与远端一致性保存在 `github/receipt.json`；构建源码仅对应基线加本批源码，交付后仅文档/队列追加结果，没有再次改源码或重复全量构建。

本节证据均相对 `docs/testing/evidence/now-unified-20261017/`：`install/install-command.json`、`install/device-apk-sha256.txt`、`install/launch-receipt.json`、`deploy/preflight01/receipt.json`、`deploy/deploy01/receipt.json`、`deploy/postflight01/receipt.json`、`deploy/public01/receipt.json`、`delivery-receipt.json`。原构建时的NOT_RUN作为历史快照保留在总receipt，后续实际结果独立追加；父任务仍IN_PROGRESS，原验收门槛不变。
