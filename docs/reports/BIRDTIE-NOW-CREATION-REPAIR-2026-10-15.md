# Now 首页创建与媒体修复：2026-10-15

## 2026-10-16 Now 地图与原动态阅读实际修复

本次发现“已迁入”并不等于“首页可见”：旧地点/动态的迁入草稿与PUBLIC筛选使Now空白，历史OSM来源链也漏接。独立库170已保留原公开事实5,492个地点和83条存续动态，3,724个地点有确切坐标；1,768个无充分坐标证明的地点保留不上图。阿伯丁及周边指定范围实际返回384个地点、8条动态地点关联、6个含动态地点与封面。原私人内容不扩大权限，原库/项目零写入。

API23/schema170、Web24与APK24已部署安装。真实手机已显示Civu式地点照片、名称和动态数；点选、在Now阅读正文/图片、返回与续期已验收。修复Android照片被旧图标名覆盖、透明画布误选邻点和弹层可见性交接。定向数据库/API/客户端测试及真实媒体摘要通过，完整记录见 `docs/reports/BIRDTIE-NOW-CIVU-MAP-2026-10-16.md` 和 `docs/testing/evidence/now-civu-map-20261016/checkpoint170.json`。

此结论仅针对地图与阅读范围，不把APK19发布证据改记为APK24重测。全仓回归、iOS、Web完整交互、全部城市/账号和最终包完整发布重跑仍NOT_RUN，父任务保持IN_PROGRESS。


## 2026-10-16 最新接续：Civu 数据适配与 Now 统一编辑器

当前独立 Birdtie 库为 schema 169，API `social169-20261015-build19`、Web19已部署，APK19已安装。固定验证码 123456 保持。普通动态与串文共用 Now 面板和原输入框，默认一条、显式追加；现有个人/地点编辑入口也返回同一编辑器。标题、城市、地点、旅程均不作为发布门槛，每条位置独立选择，不继承浏览城市。

新增 EDIT 保留原 Moment ID、链接及互动，逐条删除和整串撤回分开，排序保留历史身份。失败/未核实材料保留并阻止公开，原操作只核实或明确停止，最终发布需实际确认。真机发现的旧上传操作 ID 与草稿材料引用不同步已修复；旧异常草稿的内容和引用仍可恢复，不静默删材料。

169修复已发布视频stage过期后的继续编辑：仅沿用同Moment当前合法公开副本及原权利/保留期限。真实转码native复合和手机编辑/排序通过；随后单条删除、整串撤回均由手机明确确认，远端读取依次核对版本4/5/6及原ID，最后全部目标匿名详情/位置/媒体及五条旧媒体URL拒绝。

源码对应、改动文件、分批测试和真机范围见 `docs/reports/BIRDTIE-NOW-UNIFIED-MOMENT-EDITOR-2026-10-15.md` 及 `docs/testing/evidence/civu-native-adaptation-20261015/unified-editor168/device-final19/receipt.json`。测试用合成账号/自制材料，结束后目标公开内容已撤回。APK17/18历史验证及失败保持原时点，不换算为最终包逐项重测。

既有迁移结果保留：20 个旧 owner 归属、83 个私人动态、22 个旅程/110 明细、3 个主题地图、189 项收藏和原历史关系/消息；167其他图片适配新增77张、复用55张，共132份用途材料/153条关系（20墓碑）。407个资产中248个存续个人用途已接续，3系统资产及156删除/无引用资产继续受限保全。Civu源库/源项目零写入，原1–168 SQL保持。未代其他旧owner登录，未自动公开私人内容。

未扩大验收范围：完整全仓测试、本轮iOS和全站体验未运行；旧账号旅程/动态封面没有正向样本的项目仍按原记录 NOT_RUN。详细边界见新报告。父任务更广的剩余要求、历史状态和发布门槛继续保留。


## 2026-10-15 接续：schema167／APK16 已运行，统一编辑器新要求实施中

当前远端 API 为 `social167-20261015-build16`，独立 Birdtie 数据库 schema167；网页为 `final-web16`。APK16 已实际安装，SHA256 `b48438bebb8decfe08ab86e6423f7253c17a9d9de437067ff550482cfb5c14c4`。固定验证码123456保持。构建前后源码帧、部署文件摘要及安装回执对应；随后统一编辑器已开始修改，当前工作树不能再冒充已安装构建。

上一批新真机证据：APK16 公开详情视频实际播放至 2.250秒、整串撤回即时清退父详情。纯视频无标题／无正文／无地点的发布发生在 APK15；APK16 完成实际播放和明确撤回。两条合成测试串文和视频远端只读回查均已撤回；公开 MP4／poster／thumbnail 已退役。证据见 `docs/testing/evidence/civu-native-adaptation-20261015/device-final16/receipt.json` 和 `device15-both-readback01/receipt.json`。Now 内嵌视频播放仍标 NOT_RUN，不能把详情页播放扩大为全部入口通过。

原账号旧地点图片真机像素读取、收藏过期重新读取为 APK14 实测，未在 APK16 重跑；同源码修复保留。仅使用唯一已认领的真实旧账号，未替其他旧账号登录，未公开旧私人内容；该账号旅程封面／动态封面的正向样本没有，保持 NOT_RUN。地点图片目录 HTTP 实读19目标／23关系，实际原图HTTP读取2张，不代表逐张检验全部资产。

167实际新增77张私人图片、复用55张已有图片，形成132份用途材料、153条用途关系（133当前、20墓碑）。407个原资产闭合为248个存续个人用途资产、3个系统档案、156个删除／无引用受限保全资产。原20个owner、83私人动态、22旅程／110条明细、3主题地图、189收藏及历史关系／消息保持原生归属。3系统档案仅验证原字节摘要；不将保全资产自动激活。来源库零写入，1–166共307份SQL不变，旧失败记录保留。

用户新增“复用 Civu 串文编辑能力，Now 统一动态入口”的要求已并入原父任务：默认一条、显式再接一条、同编辑器处理原ID编辑／追加／删除／排序、失败恢复与明确公开确认。Civu-client 本地与 GitHub master 一致，来源只读审计已归档；旧 Civu 编辑模式并未实现已发布增删排序或编辑草稿持久化，不能按已有完成。当前正在补 Birdtie 新事务与统一面板，新增流程真机验收为 NOT_RUN，不能拿 APK16 作为新要求交付。规范见 `docs/product/BIRDTIE-NOW-UNIFIED-MOMENT-EDITOR-2026-10-15.md`，原父项继续 IN_PROGRESS。

下文为历史时点记录，保留原结论和失败证据，不覆盖本节。


## 2026-10-15 接续：160–166已部署，真机验收待设备连接

API已部署`social166-20261015-build13`，独立Birdtie库schema166；Web08已上线。新动态无标题输入，正文或真实媒体即可发布，城市与地图可选；0..50位置关联及真实外部引用沿Now同一输入框，62项真实HTTPS检查通过，包括移除全部位置后正文仍可读。当前外部检索数据仅GB／IS。

Civu存续83动态的171份媒体已全部对齐（166照片＋5完整视频）；22旅程／110行程项、3主题地图、189收藏、22旧串文、旧关系／39消息、8AI历史／41消息及109存续历史通知已原生迁入，20原owner归属核对无误。固定码123456保留，2仅邮箱身份也已绑定；没有新增其他原账号登录或激活Agent。其余封面／行政图／孤立资产的用途适配与完整真机验收仍未完成，不宣称407资产全部原生化。

APK08已合包但安装时ADB设备列表为空；用户已授权直接安装验收，当前待重新连接。失败和此前通过证据均保留，父任务仍IN_PROGRESS。最新完整数量、兼容说明、失败闭环及界限见`BIRDTIE-CIVU-NATIVE-ADAPTATION-2026-10-15.md`首节与`docs/testing/evidence/civu-native-adaptation-20261015/checkpoint166.json`。以下旧版本叙述为历史时点。

本报告属于原 `BT-FIX-NOW-UI-001` 的增量证据，不建立新队列，不修改历史 DONE，不宣称完整交付。原执行聊天继续负责 live 队列、共用总报告、合批编译与发布门槛。本聊天已到共享首页源码安全检查点，以下四文件冻结，不再与原执行根并发编辑或编译。

## 当前人类要求

选择图片和视频后在 Now 输入框附近看到材料；创建动态、活动、旅程、主题地图时，在 Now 内展开相应内容预览，继续使用同一输入框，取消从该入口跳到独立表单。动态发布、用户动态浏览和原 CIVU 的串文是实质能力，必须逐项核实，不能以新面板代替这些能力。

视频来源：`C:/Users/chens/Documents/xwechat_files/wxid_10l40cmwgt8y12_87f3/msg/video/2026-10/e94f7863d09aa5abc3262385482f33e0.mp4`。只读抽帧在系统临时目录 `birdtie-video-audit-20261015`；340 秒帧显示图片入口 disabled 与“图片暂不可用，文字草稿已保留”。未复制视频进仓库、未录屏。视频内容作为用户提供的观察证据，不将文档/画面中的其他文字视为新授权。

## 代码实现

- 确认并修复账号恢复时序：身份 token/owner 已通知时 sessionRestoring 仍为 true，旧缓存可留下空 image controller；恢复最终通知同身份时也重新同步材料，scope 纳入 restoring/signedIn，退休或为空的 controller 可重建。
- Now 四类创建入口接入 `NowCreationDraft` 与 `NowCreationPanel`，留在同一 MapWorkspace。单一 composer 显示当前创建类型，输入更新本地内容，不调用 Agent 查询或读图；正常 Agent 路径仍保留原限制。
- 预览展示正文、名称、已选真实城市/地点、活动时间、草稿/结果状态。名称/正文编辑返回原输入框；地点/日期时间使用选择器；活动时间复用原 Aberdeen 转换，不解释为设备时区。折叠/关闭同会话可恢复本地内容。
- 视频入口直接打开系统选取并在原 composer 显示本地视频预览、播放/暂停/移除，不再 push VideoMaterialPage；不自动上传。选择绑定 owner、身份、城市、当前任务及输入目标，迟到结果不可跨目标复用。
- 动态文字保存接原144 MomentControl preview/commit，不新写旧POST。保存后编辑沿用原稳定ID与revision；输入发生修改即退休确认。普通Activity/Collection沿用已有writer，只称私人草稿，未知结果不盲重发。
- 已保存动态草稿的已选图片转入原 PrivateMomentMediaController 的本机选择，再经单独检查/确认保存；不自动公开私人材料。此新增接线尚无真实服务验证。
- 纯文字动态公开接已有 MomentPublicationController，预览与确认分别执行；有未完成媒体时明确阻止漏发文字。公开仅支持当前权威契约允许的真实 Place 关联。公开未知结果只读核实原发布。

## 验证证据与限制

执行目录：`D:/Project/birdtie/apps/client`。普通字号新增测试，不安排大字号专项。

1. `flutter test test/agent_composer_test.dart test/now_composer_material_test.dart test/now_task_image_draft_test.dart --reporter compact`：退出0，81 PASS。为直接受影响三文件现有回归；其中原文件已有历史字号参数化用例，本轮没有新增字号范围。该次回归发生在后续最小冻结修正之前，不将其冒称最后source-frame全量结果。
2. `flutter test test/now_inline_creation_test.dart test/now_media_recovery_test.dart --reporter expanded`：首跑退出1，9 PASS/1 FAIL；失败为测试夹具重复赋值 late final moments。日志 `%TEMP%/birdtie-now-creation-direct-20261015.log`，保留。
3. 同命令 direct02：退出1，9 PASS/1 FAIL；唯一失败为具体文字preview用例的 pumpAndSettle timeout，后续仅修独占测试等待方式，主源码保持相同 SHA。日志 `%TEMP%/birdtie-now-creation-direct02-20261015.log`，保留。四类首页内创建/长中文/地图稳定、折叠关闭保留、账号隔离和两种恢复后真实本机选图均通过；该表述仅指合成widget环境。
4. 定点修复最后测试的等待方式后执行 `flutter test test/now_inline_creation_test.dart --plain-name "具体文字预览后修改输入移除确认，旧确认回调不能提交" --reporter expanded`：退出0，1 PASS，日志 `%TEMP%/birdtie-now-inline-preview-targeted-20261015.log`。用 `runAsync` 完成现有真实异步边界后再 bounded pump，保留 busy=false、具体预览存在、编辑退休确认与旧回调零PUT断言；不是删除/放松测试。结合 direct02 同源码帧另外9个PASS，本轮新增10项用例均有通过结果，未重新重复整套。
5. 修改文件格式化退出0。第一次格式化命令因在client工作目录重复传apps/client前缀未找到文件，随后用正确相对路径完成；未隐藏失败。

6. `git diff --check` 对本轮四个源码和两个新增测试退出0；Impeccable `detect --json` 一次输出空数组，未构成原生视觉验收。

APK/Web编译、本轮真机安装、真实图片与视频选择/权限/播放器、真实服务草稿保存/图片保存/公开发布、网络异常、辅助技术、真实正常/键盘/错误截图：均 NOT_RUN。当前没有新包交付。本机adb曾只读发现连接设备，不将此视为运行证据。遵守不录屏。

适用 UX-CHECK-01/02/03/04/05/06/07/08/09/10/11/12/13/14/15/16；目前仅有部分代码与widget证据，14按正常字号的窄屏/键盘/辅助技术范围；无生产/视觉完成推断。保留原全部门槛。

## 仍然缺失的实质能力

- 原生串文没有 thread/root/顺序/每条媒体和位置/统一受众与原子发布契约；未恢复。只读 Civu `D:/Program/Civu/Civu-server/api/internal/store/note_types.go:187` 确认原串文语义，未复制代码、依赖、数据或媒体。不能通过逐条创建假称串文。
- 完整城市/关注动态流未恢复。现有真实浏览仍为地图单动态详情、Place近30天分页、当前地图范围的作者动态；不是完整动态feed。
- 多图、视频随动态公开仍缺权威媒体发布契约。原视频purpose为私人视频材料；原公开动态图片仍是独立的一张public photo。新Now预览未扩大这些权限或能力。
- 旅程/主题地图的当前多地点选择保存在活动面板，跨退出/重进的完整地点顺序恢复还未覆盖；后续需在原typed草稿合同中补齐，不能仅以正文恢复称整份草稿稳定。
- 本地新创建面板目前按同一Now会话内保留；进程退出后的结构化创建内容恢复未完成，不能据composer已有typed persistence冒称新面板全恢复。
- 普通Activity writer缺已有immutable op/CAS语义，普通Collection UPDATE/WITHDRAW结果恢复有原审查差额，未将字段相等/当前GET猜为原操作成功；不关闭原未闭环项目。
- 公开动态仍要求canonical Place；城市级无Place动态不能发布。Activity/Collection的新入口目前只连私人草稿，不宣称公开创建流程完成。

这些是本次人类要求的未完成部分，纳入原任务/原报告继续处理；本次修复不是整个软件完成或发布验收。

## SOURCE_READY / 共享源码冻结

时间：2026-10-15T00:38:20.364260+08:00

| 路径 | SHA-256 |
|---|---|
| `apps/client/lib/src/workspace/map_workspace.dart` | `de188179f6019d8467d48018581b789f1893e25758713f41b894c9fc7aead137` |
| `apps/client/lib/src/workspace/agent_composer.dart` | `cff58ce89c4e8a268725f5cbff99e1b0cef08955e87245325255d67c88deb154` |
| `apps/client/lib/src/workspace/now_creation_draft.dart` | `acbb6785c337bb6a56ad883a7e83acca2b996e3c358373bc876dcf4979eb77af` |
| `apps/client/lib/src/workspace/now_creation_panel.dart` | `ca727c5f3efabc9cc707261ba42f1a0b775c872ee3bcadcb61a8e7ce6ba4aae3` |

交接范围：上表source-ready四文件。允许后续仅修独占测试/本报告；原执行根可对该确切源码帧做一次合批检查、分析、APK/Web。不得将当前临时日志或旧包验收视为这个源码帧已通过。当前聊天不新领取任务，不启动部署或迁移，不联系外部人员。

## 独占测试最终帧

- `apps/client/test/now_inline_creation_test.dart` SHA-256 `b643cb45210227c6cb7d06bfed085e0af9b55564c05f85ae05ce5fe6ed2dab05`
- `apps/client/test/now_media_recovery_test.dart` SHA-256 `7e4f22cd701f330e1645de1a7bc6e03e106e445793687c346a33e73e61096c95`

本聊天至此无运行中的 Flutter 测试/构建，主源码仍与 SOURCE_READY 表一致；未进一步变更 live 队列、共享总报告或原执行根的文件。首跑两次失败证据保留。

## 2026-10-15 后续实装与远端接续（早期缺口保留为历史）

公网唯一入口已成为 `https://47.110.154.133`，当前 API 为 `social155-20261015-build05`／原生155；Web02与Android APK02均指向该来源。APK SHA-256 `53b79dc5813c9ad1bd64274043e16d7b55f86056dd5c6606a08568f31b5f6b0c`，API SHA-256 `d4f81a65f0059328baabbbec587e9d7af40ca600d45948378b2b54ad0caa0e3a`。原 Civu app/data 保留，预览包已安装。采用用户指定固定验证码 `123456`，18个旧手机号绑定原owner；真实旧账号两次登录与16Note／11Journey／2ThemeMap读取通过，首次接续1人、Agent仍暂停、两个会话均退出。

新代码补齐城市动态、本人／作者完整历史、城市／关注／好友PUBLIC筛选、点赞评论与通知管理、有序九图或一视频、2–20条同事务串文；Now沿用原输入框和首页面板，包含已发布查看／正文修改／撤回，结构化本机草稿和媒体续期隔离。普通社交沿原Connection／Follow／聊天／143分享领域；没有复刻Civu ACL或启模型／付费provider。最后真实远端检查补发现并修复“城市动态分享仍强制Place”的查询差额，隔离PG一项与API05好友／聊天／分享有限流程均PASS。

实际Web完成单图动态发布／详情／编辑／撤回；Web02关闭草稿PASS。最终APK02真实竖屏Now创建／固定登录恢复／单一输入框／图片入口启用／关闭／退出PASS，原旋转恢复、字号1.0。服务器实际九图／视频Linux转码／两条原子串文流程在未变的相应模块源码帧通过；不冒称整站或手机全部媒体触摸验收。图库权限、手机播放器／发布、好友聊天分享双角色UI、辅助技术、iOS和后台系统推送仍未运行。正常字号、不录屏、中英混合语音延期继续有效。

旧Civu writer已停且禁用（failed/timeout、MainPID0、8080拒绝）；四Pelias和旧网关停止，旧库／媒体／容器／配置保留。停服务90秒SIGTERM超时记录及第一次状态断言失败保留。完整旧数据适配仍PARTIAL：20永久owner映射、103Note／34Journey／3ThemeMap／189SavedPlace私人档案已提交；5511Place／339城市合法当前映射、407旧媒体的72差额及扫描／权利、旧关系／消息／组织和2个仅邮箱账号未全接续。私人档案不等于当前可编辑／公开对象；停写后最终增量核对尚未完成。原Activity／Collection创建原事实等历史差额和发布门槛不因这批关闭。

详细事实及首失败保留见 [当前GitHub、远端与迁移记录](../reports/BIRDTIE-GITHUB-REMOTE-MIGRATION-2026-10-15.md)。本轮源码、测试、迁移、部署脚本与报告一起提交原GitHub分支；全量检查点位于 `docs/testing/evidence/social-remote-migration-20261015/checkpoint-final.json`。保留原256项、历史状态与父项IN_PROGRESS；Closed Pilot／Consumer Beta仍NO。
