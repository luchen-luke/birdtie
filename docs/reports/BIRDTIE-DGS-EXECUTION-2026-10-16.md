# Birdtie DGS 实际交付记录 · 2026-10-16

本批代码已合包并装机，API31/schema175已部署至当前Birdtie独立库；DGS整体仍为PARTIAL。用户指定网页延期、正常字号、不录屏、Civu只读。原任务/依赖/失败证据均保留。

## 当前交付身份

- 本地和GitHub阅读起点：`cef1a11ada611247021c4e81593c50437f336a66`，分支`codex/birdtie-import`。附件`4637cfb...`为更早祖先，无reset/clean。提交后的精确Git SHA由本机outputs交付回执给出，避免把构建起点冒充新提交。
- 安装APK40：SHA256 `63d410883abeafadb717844dc1aaa13a3b8d84225831f41cc513e3e1673fb9f7`，包`app.civu.civu_mobile.birdtiepreview`。40为交付批次标签；Android实际versionCode=1/versionName=0.1.0。562文件构建帧一致，替换安装保留数据。
- 活动API31：`/opt/birdtie/releases/social175-20261016-build31`，SHA256 `3359b4baf56aadad3dff66c25e9d9cceaab4a5ef16fc78273a78881942216b4c`；BIRDTIE_NATIVE_HZ/OID54087，迁移max=count175。进程可执行文件及就绪状态已核对。
- Web34未改。原Civu服务只读观察仍failed/failed，未干预。Civu-client本地/远端`1bbf0613b513001447f326225c944c1a1fbd8e8f`；server本地/跟踪`0489536a0eb0792deff7793c8b143ad3e741d5ce`，最新远端查询失败，不宣称已同步。

## 地图为什么会消失，以及本次修复

确定性测试复现两条缺陷：一个封面404/not_found或409/moment_changed把全部地图内容退休；组织身份回到个人时只清退、不恢复图层。现改为可靠的单资源错误仅撤销该项并合并重验；身份监听周期结束后读取最新身份和视口。账号、token、组织变化仍隔离旧响应，401及无法证明局部性的权限失败仍全局拒绝。

两种地图读取器逐项解码，单条坏记录不拖垮正常项；全批坏数据或全局权限/期限错误明确失败。503/网络失败只能保留原授权期限内的快照，有界重试，不能无限延长TTL。

真实手机APK39最初处于未登录状态；复用此前已经接续的真实Civu owner、按用户指定固定码登录后，Aberdeen出现7个内容标记。APK40安装及冷启动继续恢复该会话。这个现场事实解释本次未登录的空图，不替代上述代码根因，也不能证明用户此前所有消失都由登录造成。

## 高德、CIVU样式与全球目的地

Android使用用户新配、匹配预览包的Key；旧Civu Key曾真实返回INVALID_USER_SCODE。复用Civu的两份原始自定义样式，无网页Key依赖；原始样式SHA分别为`2e58328b9a676e23705f8a11ded22f9eb5d5482d1c2d1d00b9d67494ec25c8d1`、`a2055f0e095badfbbb402eb5e61b49ec5a7ff71795be6600a6a55a92bb9e034b`。Key不入Git，包内样式与源字节一致。APK40上海城市/街道尺度实际显示Civu底图，普通POI被抑制，真实业务内容点保留。未进行测绘参考点米级坐标校验。

Now入口已使用“选择城市或目的地”搜索；浏览目标与本人/草稿/公开位置分离。Mapbox全球搜索不加当前城市硬限制；Android国内搜索使用已包含的高德原生SDK、显式隐私同意、不取设备定位。Temporary结果不持久缓存，无内部city时通过现有bounds读取，不创建假city/POI。

APK39实际搜索Paris、在同名结果中选法国巴黎并定位，证明不依赖内部城市目录；这是39证据，不能记成40重复执行。APK40真实上海搜索与城市尺度通过。39曾把上海当省级zoom7；现仅4个直辖市adcode调整为城市zoom11，普通省保留zoom7、区县不变，Kotlin12+Flutter3项通过。

## 真实数据与清理

部署后只读对账：102条动态中83条为Civu稳定映射，18条来源未逐项确认且归测试账号，另1条已withdrawn/private。保留39条旧消息、11个会话；24张保护表摘要一致。真人登录令会话数69→70，这是合法新会话，未声称整张会话表完全不变。

按精确执行来源和依赖清理22表103行：8条确认合成动态、1个合成会话、3消息、2请求及依赖。全量备份已在隔离环境真实恢复，清理重演、正式清理、第二次ALREADY_CLEAN均通过。未删账号或媒体bucket；14类真实对象摘要不变，外键与12个命名触发器恢复，8条缓存失效事件已写入。

6个明确fixture账号改为suspended，保留其6个固定身份防止重注册；有效fixture会话为0。18条未知内容保留并随这些账号隔离；另1条非fixture未知记录保持原withdrawn/private，作者不受牵连。隔离动作在恢复库验证可恢复、幂等后上线，未按作者批量删除内容。

3份dev seed默认必须通过精确隔离库名、owner、DB注释及两项会话开关；7个历史实际环境注入入口在执行体前停止。真实用户固定验证码接续保留。旧seed UUID在线上原本为0，不把仓库有seed误称为它们已污染运行库。

## Civu源码对应与改动入口

| 复用对象 | 源/适配目的 | 本批实际改动路径 |
| --- | --- | --- |
| Civu原始高德样式 | `Civu-client/assets/map_styles/civu_android`原字节；地图桥保留现引擎 | `apps/client/assets/map_styles/civu_android/`；`lib/src/city/amap_offline_style.dart`、`amap_city_map_io.dart`、`amap_native_configuration.dart`；Android `MapConfiguration.kt`、`MainActivity.kt`、`build.gradle.kts` |
| 地图读取和常驻 | 保留已接入Civu媒体标记/预算/增量更新；修复身份和局部错误合同 | `apps/client/lib/src/workspace/owned_map_{api,controller,media_controller}.dart`、`map_layers_{api,controller}.dart`、`map_workspace.dart` |
| 全球目的地 | 复用现地图及高德SDK，独立browse destination | `apps/client/lib/src/city/browse_destination.dart`、`destination_search_{service,controller}.dart`、`amap_destination_search.dart`、`amap_session_consent.dart`、`public_city_controller.dart`；`workspace/map_canvas.dart`；Android `DestinationSearch.kt`、`DestinationAdministrativeType.kt` |
| Civu关注/通知 | 源`Civu-server/api/internal/store/social_postgres.go`等，逐规则矩阵核对；Birdtie原生领域适配 | `apps/api/migrations/175_person_follow_notifications*.sql`；`internal/postgres/follows.go`、`person_follow_notifications.go`、`notification_routing.go`、`inbox*.go`；`internal/httpapi/person_follow_notifications.go`、`inbox_page.go`、`moment_interaction.go`；`internal/agentnotification/registry.go`、`internal/inbox/page.go` |
| 无标题分享、通知兼容 | 保留Moment ID与当前权限；正文仅作显示预览 | `apps/client/lib/src/workspace/connections.dart`、`remote_inbox_source.dart`、`inbox.dart` |
| 精确清理与注入守卫 | 来源/ID/依赖/备份保护，无旧库写入 | `scripts/audit_birdtie_synthetic_data.py`、`purge_birdtie_synthetic_data.py`、`quarantine_birdtie_fixture_accounts.py`、`fixture_environment_guard.py`；`apps/api/dev-seeds/001..003`；`automation/verify_community_migrations.ps1`；`internal/postgres/owned_migration_database_test.go` |

表中`lib/`及Android短路径分别归`apps/client/`和`apps/client/android/app/src/main/kotlin/app/civu/civu_mobile/`；Go `internal/`归`apps/api/`。精确完整变更文件清单随本机outputs交付，测试文件与证据不在表中逐项重复。

新关注通知现在只由新原生关注事务生成不可变事件，重复请求不重复生成，旧关系不回放通知。互关不自动升级好友/会话。当前目标解析检查收件人、会话、关系、屏蔽、活跃状态和当前资料权限；响应前再次校验。旧客户端在计数/分页前过滤不支持的新事件；新客户端显式能力协商。未知通知单项只读降级、不跳转旧payload。完整逐域差异见`docs/audits/CIVU-SOCIAL-REUSE-MATRIX.md`。

## 直接验证和证据

证据根为`docs/testing/evidence/dgs-20261016/`；每份回执含命令/cwd/退出码与来源边界。重叠回归不累计为新增测试总数。

| 证据 | 实际结果 |
| --- | --- |
| `map-stability/final-targeted.json` | 49 PASS；修复前2 FAIL保留 |
| `identity-host/after-fix.json` | 3 PASS；组织切换修复前FAIL保留 |
| `destination/targeted-composite.json`、`root-integration/tests.json` | 26 PASS、7 PASS；后者含上述3个身份用例，不能重复计数 |
| `destination/municipality/freeze.json`及同目录报告 | Kotlin12、Flutter3 PASS |
| `notifications/targeted03.json` | 20 PASS；两轮fixture失败保留 |
| `social-reuse/flutter-targeted.json`、`go-domain.json` | 8 Flutter、4顶层Go PASS；分享修复前2 FAIL保留 |
| `social-reuse/follow-native02/receipt.json`、`follow-http03.json`、`follow-flutter02.json` | schema175隔离PG 1顶层、最终HTTP1顶层、Flutter6 PASS；registry另有直接PASS；重叠不累加 |
| `social-reuse/legacy-history175/receipt.json` | 隔离PG 1顶层PASS，原状态、owner、重复导入及禁止新Push |
| `data-cleanup/receipt.json`、`injection-guards/final-receipt02.json`、`post175-readonly01.json` | 备份恢复/限定清理/幂等/注入守卫/隔离及部署后数据核对PASS |
| `android40-device/verification.json`、`artifact-identity.json`、`install.json` | 新包身份/安装、真实owner与地图、原图片、前后台恢复、上海样式的限定真机证据 |

后台停留至少32秒超过30秒租期后返回，授权重新读取、内容恢复，Mapbox地图创建计数仍为1。图片阅读面板实际显示作者与原发布时间；没有为验收在实际环境新建内容。截图只保留用户本地outputs，Git仅有文件名/摘要，不上传私人截图。

## 性能实测与前后限制

同一设备4次东西平移（每次600ms、间隔2秒，总约11.25秒）：

| 构建/地图 | 业务map-content请求 | 封面请求 | Flutter GPU Draw p95/max | PSS前→后 KiB |
| --- | --- | --- | --- | --- |
| 历史APK29 / Mapbox | 2，1192.604/1792.035ms | 旧回执单独查阅 | 2.323ms / 见原回执 | 484135→509949 |
| APK40 / Mapbox | 1，108.37ms | 0 | 2.433/7.755ms；327帧0超16.67ms | 486851→514273 |
| APK40 / 高德 | 0 | 0 | 16.36/24.984ms；173帧8超16.67ms | 658551→650253 |

历史与当前数据集、API及缓存条件不同，不能据此计算严格优化百分比。APK40 Mapbox mapCreations=1，封面请求/解码计数在平移前后均7，marker进入/离开视口增量更新；不是每次相机移动重新加载所有封面。之后计数包括合法30秒租期重验，不能要求授权快照永久不联网。上述HTTP为Dart业务网络，不包含高德/Mapbox原生底图网络；Flutter事件不等同整个原生地图GPU。高德仍有长帧，性能整体不标PASS。重复压力/首交互完整指标NOT_RUN。

## 部署、回滚与待完成

API部署只追加174/175，原173迁移摘要不变，旧业务数据摘要保护，历史关注事件新增0。仅上海/汕头两个既有配置改为高德。部署前新备份SHA为`c333e6abd83851819e1a71476e6dcd2dbabd3f2bb3ae9b1c2456af272804d491`，已验证目录可读；这份新备份完整恢复NOT_RUN，不能混用清理批次已恢复的证据。部署失败回滚保护为二进制/服务配置，并不自动撤销DB DDL；175已有事件时down会拒绝。

未完成项：真实两人好友/私信/分享、双设备未读与Push依赖第二位真实参与者和设备（BLOCKED，具体测试NOT_RUN）；手机断网与30次冷启动、全Moment入口一致性、全部媒体抽样、严格坐标参考点及压力性能仍NOT_RUN。逐条消息删除/撤回writer与历史通知逐类型当前目标仍未补齐，保留为产品/适配缺口，不能当作仅缺测试。历史通知当前提供安全只读保留，符合附件允许的过渡方式。

当前进度如下。DONE仅限该检查点已定义证据，不代表整个产品或原父任务完成。

| 检查点 | 状态 | 证据及剩余边界 |
| --- | --- | --- |
| DGS-01 | DONE | 本地及GitHub起点cef1a11、当前APK40/API31/schema175逐项绑定；原附件旧基线不回退。 |
| DGS-02 | DONE | 单封面故障全清、组织切换不恢复已确定性复现；逐项解码与链路计数留证。现场未登录是另一个已见原因，不声称唯一根因。 |
| DGS-03 | DONE | 账号、token、组织监听后统一恢复3项通过；旧响应仍隔离；真实登录与40会话恢复已执行。 |
| DGS-04 | PARTIAL | 网络/资源/过期边界直接测试通过；真实后台超过30秒恢复、阅读后地图常驻通过；手机断网矩阵及30次循环NOT_RUN。 |
| DGS-05 | PARTIAL | v2无POI坐标点、空标题解码及无标题分享直接测试通过；本批所有发布/读取入口全链路NOT_RUN。 |
| DGS-06 | DONE | 来源清单、3 seed双重环境守卫、7旧注入脚本封停；14实际阻断尝试、隔离PG验证通过，真人固定码保留。 |
| DGS-07 | PARTIAL | 备份实际恢复、22表103行精确清理、幂等重跑及保护摘要通过；6测试账号隔离；手机长期旧缓存/搜索全入口矩阵NOT_RUN。 |
| DGS-08 | PARTIAL | 部署后83 Civu动态/39旧消息及稳定映射保护；真实owner登录和原图片读取通过；全部媒体与关系逐项真人验收NOT_RUN。 |
| DGS-09 | PARTIAL | 中英文Mapbox真实调用200、手机巴黎选择及原生高德上海成功；provider失败仅直接测试，真机外部故障NOT_RUN。 |
| DGS-10 | PARTIAL | 真实Now搜索/国家消歧/巴黎定位与上海城市尺度通过；全球语言和错误态真机矩阵NOT_RUN。 |
| DGS-11 | PARTIAL | 无city目的地状态和bounds-only查询直接测试、巴黎手机定位通过；跨日期变更线真实设备NOT_RUN。 |
| DGS-12 | PARTIAL | 好友/关注/屏蔽差异矩阵、新关注通知及当前权限适配；隔离双账号PG通过，真实双人设备依赖BLOCKED、E2E NOT_RUN。 |
| DGS-13 | PARTIAL | 原消息/历史通知留存、禁止历史Push、未知单项降级、原生新关注通知部署；双设备未读/后台Push NOT_RUN；逐条撤回writer仍缺。 |
| DGS-14 | PARTIAL | 无标题分享、当前目标resolver、迟到身份拒绝通过；真实地图→图片已执行；全入口同内容真人E2E NOT_RUN。 |
| DGS-15 | PARTIAL | APK40已装/API31已部署，地图/搜索/图片/前后台和性能抽样完成；其余明确列NOT_RUN/BLOCKED，整体不标完成。 |

清理/隔离工具仍精确绑定已审查的API30/schema173，在当前175会拒绝重跑；不为追求重复执行而放宽保护。清理报告与注入报告的早期状态保留为阶段快照，部署后以post175与APK40证据为准。fixture账号的真实公网拒绝登录本轮NOT_RUN；没有再登录这些账号来制造验证数据。
