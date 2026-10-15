# Birdtie GitHub、Now 核心功能与真实远端接续

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

日期：2026-10-15，Asia/Shanghai；根任务 `BT-FIX-NOW-UI-001`。本报告区分当前可用功能、实际数据承接与仍缺条件，不把局部能力当整个软件完成。

## 最新事实

- GitHub 已有整合基线 [af116c3](https://github.com/luchen-luke/birdtie/commit/af116c3)；本报告与本轮全部有效源码、测试、迁移和部署脚本一起纳入最终提交，沿同一 [codex/birdtie-import 分支](https://github.com/luchen-luke/birdtie/tree/codex/birdtie-import)。实际推送 SHA 以该分支及交付副本为准。私人源、真实媒体、密钥和生成缓存不提交。
- 独立杭州原生库 `birdtie_native_hz_20261015`／OID 54087 已应用 155；API 当前为 `social155-20261015-build05`，`/readyz` 200。通知原回执和城市动态分享修复后分别刷新 API；最后一次追加迁移 0，原 155 份 SQL 摘要未变。
- 当前 APK 与 Web 均成功合包且源码帧稳定，API target 为 `https://47.110.154.133`。APK 已 `adb install -r` 安装到 `app.civu.civu_mobile.birdtiepreview`，设备 `c641566b`、字号 1.0；原 Civu app 与应用数据保留。APK02 真实竖屏 Now→材料→动态→固定码登录→原面板恢复→添加图片可用→关闭草稿→退出登录 PASS，始终一个输入框；原旋转设置已恢复。图库选图、权限、物理视频播放器和手机发布触摸流程未运行。
- **继续使用固定验证码 123456。** 154 已提交 18 个旧手机号到永久原 owner 的不可变绑定；不会要求短信服务。使用一名真实旧用户验证两次登录同 owner，读取其 16 Note、11 Journey、2 ThemeMap 和真实详情；2 会话均退出并复验 401。账号首次接续 1、Personal Agent 激活 0、公开个人资料 0、旧 Session 复制 0。
- **公网入口 [https://47.110.154.133](https://47.110.154.133) 已切换 Birdtie Web02 与原生 API05。** HTTPS 使用默认证书验证；真实 Web 登录、原输入框编辑、图片预览／明确公开、同一动态查看／修改／撤回、退出通过。旧 Civu 网关、API writer 和四个 Pelias 已停止；旧 API 禁用，MainPID=0、8080拒绝。其90秒停止超时及 failed/timeout 状态保留，没有冒称正常退出；数据库、媒体、旧服务文件与容器保留。

## 本轮实现和实际流程

Now 首页保留原单一 composer，动态、活动、旅程、主题地图展开在原面板；结构化本机草稿按 owner／origin／city 保存。图片最多九张有序选取，视频本机选取与播放留在 Now；同账号正常续期保留未公开选择、退休批准，换账号或原来源消失清除字节。已发布正文编辑、查看与撤回在首页当前面板可达。

城市无 Place 的动态可按明确城市公开、修改和撤回；不伪造地图坐标。本人、作者完整历史以及城市／关注／好友 PUBLIC 筛选采用独立分页二级浏览，保留既有 Now 首页。动态点赞、评论、本人评论删除进入真实通知；通知定位到同一动态／评论，批量已读、清理及删除基于精确 cutoff 和 source 预览。好友、会话、分享沿既有领域与二级入口接线，原 PARTIAL／真实角色验收门槛未自动关闭。

新增公开材料 152＋155 为 **九图或一视频** 的有序整组合同，权利声明与具体派生图／视频绑定当前 Moment revision、owner、位置与受众；旧私人视频 125 不作为公开来源。视频自托管于现有杭州服务器，实际 FFmpeg／ffprobe／prlimit 路径和摘要核验、单任务准入、限内存／时间及私有临时目录；不依赖付费模型或外部媒体 provider。仅声明实际转码与结构验证，不冒称杀毒扫描或自动版权验证完成。

153 为 Birdtie 自有 thread/root/order：2–20 条同事务预览批准和发布，每条独立正文、媒体及合法位置，统一 PUBLIC 受众。单条修改／撤回与整串撤回按原 ID／版本；不是循环普通动态请求。串文 STAGE 保持共同服务器 scope，同时本机 journal 带每条 entry UUID，响应丢失后另一条不能拿到其材料或批准。

| 当前证据 | 实际结果／范围 |
|---|---|
| `host-client-final01` | 本轮受影响 Now／材料／地图及本机视频续期 23 PASS，源码帧稳定；相关 Dart 分析 0 error／0 warning，189 条 lint info 单列 |
| `host-detector-final01.json` | 一次 Impeccable 输出 `[]`，不是设备视觉验收 |
| 各独占模块冻结 receipts | 历史、互动、152、153、154、155 的必要直接和隔离原生验证分别记录，不合称一次全仓回归 |
| `remote-social-flow01` → `inbox-original-fact-repair01` → `remote-social-flow02` | 首次真实流程发现未持久化 unreadCount 导致回执变动；修复后首次 commit／原 GET／同 op 原 payload retry 相等；build04 真实发布／历史／赞评／通知／全部已读／公开编辑／撤回 PASS |
| `remote-media-thread-flow01` | build03 一轮真实 Linux／HTTP 流程：九JPEG顺序、实际首图 SHA、视频规范化并读取公开MP4 SHA、修改／撤回旧URL404、两条原子串文同事务附件／root／顺序／整串撤回／原事实保持；四个合成 Moment 均撤回、两个会话均退出。155媒体／串文产品源在build04未变，不把它冒称build04完整新回归 |
| `web-live01`＋`creation-chip-repair01/live-web02.json` | Web01 实际同一动态的图片发布、当前详情、正文修改和撤回 PASS；最后三行 InputChip 可操作性修复形成 Web02，真实关闭草稿 PASS，无新增动态。没有重复称 Web02 完整媒体回归 |
| `device-final02/qa-native-receipt.json` | 最终 APK02 真实竖屏有限创建／登录／关闭／退出流程 PASS，系统状态恢复；图库／物理视频／真机发布 NOT_RUN |
| `remote-friend-chat-flow01` → `city-moment-share-repair01` → `remote-friend-chat-flow02` | 首跑好友／关注／文字会话已通过，但无Place城市动态分享404；修复原 chatEntityQuery 的城市可选Place合同，一个隔离PG用例 PASS/0SKIP。API05真实二合成账号申请／接受／双方好友、关注取消不解除好友、文字原操作／历史／已读、同Moment分享和对方读取、撤回后卡片不留旧标题／ID全部 PASS；本轮好友移除、资料恢复、会话退出204＋401 |
| `remote-legacy-login01` | 真实旧输入单owner两次固定码登录原账号＋本人旧档案＋private profile／Agent暂停／退出后401 PASS；手机号、令牌和正文仅RAM |

## 旧数据适配的真实边界

| 类别 | 已完成 | 尚缺 |
|---|---|---|
| 20 个旧用户／21 认证身份 | 原 Account／Agent 永久映射；18 唯一手机号固定码接原 owner，实际验证 1 人 | 2 个仅邮箱用户无对应手机号；未冒称生产身份验证，未猜号码或激活 Agent |
| 103 旧动态 | 83 现存＋20墓碑，作为本人旧私人档案可读 | 尚未全转为当前可编辑／公开 Moment；旧多地点、串文及媒体没有凭历史公开标记授新权限 |
| 34 旅程／3主题地图／189地点收藏 | 私人旧档案提交并原 run 重读一致；旅程含12墓碑 | 当前 Journey／ThemeMap／SavedItems 与合法 Place 关联未全部转换 |
| 5511 Place／339旧城市 | 受限源、摘要与备份保留 | 5484 region未分配、源 CRS／live_only 权利需核实；不猜坐标或自动复制供应商目录 |
| 407媒体selector | 335仅完整性匹配、72差额已记录；加密备份保留 | NOT_SCANNED／RIGHTS_NOT_REVIEWED；21pending、45空MIME、3无owner，旧媒体实际导入0 |
| 旧关系、互动、会话、通知、组织 | 原备份保留 | 选定29表模型未覆盖其业务承接；备份不能称已适配到当前业务 |

因此，**GitHub源码、当前远端核心功能和固定码接续可分别交付；整个新要求与完整 Civu 数据业务适配尚不能称全部完成。** 好友／聊天／分享的双角色真实UI、聊天媒体／后台系统推送、旧数据差额、辅助技术／iOS与生产发布继续保留原门槛。普通站内社交无需 Agent/model provider；系统推送仍关闭，不向真实用户试发消息。

## 配对产物与失败保留

- 当前 API build05 SHA-256：`d4f81a65f0059328baabbbec587e9d7af40ca600d45948378b2b54ad0caa0e3a`；Web02 manifest SHA-256：`b986a49f9cc1e1590446f052fad536d8be8602dc74ce3dfa0d7230bfad347198`。配对源码字节最终核对一致；客户端在 API 分享修复后没有改动，未重复编译。
- APK SHA-256：`53b79dc5813c9ad1bd64274043e16d7b55f86056dd5c6606a08568f31b5f6b0c`；326761738 bytes。采用 development profile 以保留明确测试登录；不是 Consumer Beta production release。
- 受限凭据根：`docs/testing/evidence/github-remote-completion-20261015/` 和 `docs/testing/evidence/social-remote-migration-20261015/`。全部命令、cwd、退出码、源码前后帧、artifact摘要与首失败保留；不录屏。
- `remote-dev-phone-apply154-01` 在 prepare 阶段因部署脚本漏显式 repository root 拒绝，未apply；修正脚本后02提交＋原操作重读一致，不绕私密文件／DB身份守卫。
- `public-entry01` 预检发现Windows manifest分隔符；02网关检查失败后回滚。随后只读核验默认SNI与原有效IP证书，03保留原TLS及default_sni后切流 PASS。Web02只上传三个变化文件，复用39个已核摘要资产；原网关与配置保留。停止旧 writer 的首次收口错误地只接受inactive，后续只读审计确认实际为failed/timeout＋PID0＋disabled＋端口拒绝，原失败未覆盖。
- 已应用SQL在 `apps/api/migrations/.gitattributes` 标记 `*.sql -text`。34个历史SQL原Git blob为LF、既有工作树／真实已应用产物为CRLF；只将原工作树字节重新收入Git index，归一换行后与原blob完全相同。工作区SQL字节、当前155份部署摘要与服务器已应用DDL均未改，确保以后checkout仍匹配摘要。相应Git diff含换行差异，不将它隐藏为零改动。

## 早期149检查点（历史保留）

以下文字描述其记录时点；当前结论以上方及后续接续为准。

# Birdtie GitHub 远端和迁移交付记录

日期：2026-10-15，Asia/Shanghai。正式仓库 `D:\Project\birdtie`，接续原 `BT-FIX-NOW-UI-001`；没有替换任务队列或重开历史 DONE。

现有全部有效修改已推送到 [GitHub 分支 codex/birdtie-import](https://github.com/luchen-luke/birdtie/tree/codex/birdtie-import)，首个整合提交 [af116c3](https://github.com/luchen-luke/birdtie/commit/af116c3)。随后新增功能继续在同一分支实施，最终源码、构建与远端版本将在下方接续。整合提交包含 3082 个改动文件；Flutter 生成缓存未纳入，原密钥、私人提取数据及媒体备份留在仓外受限目录。提交前对候选源文件扫描，未发现真实凭据；GitHub 已接受提交和推送。

## 当前远端运行和真实数据

原已获准杭州主机已成功通过严格主机密钥校验连接。当前独立 Birdtie 数据库为 `birdtie_native_hz_20261015`，OID 54087。修复部署程序把 PostgreSQL OID 字符串与本机整数直接比较的问题后，`remote-upgrade149-02` 返回 `UPGRADED_READY`，原 148 份迁移字节保持。旧失败 `remote-upgrade149-01` 保留，未绕过数据库名、OID、owner 或 marker 守卫。

| 已导入类别 | 权威结果 | 用户可用性边界 |
|---|---|---|
| 20 个旧用户 | 永久 Account 和 Personal Agent 映射 | 仍暂停且 UNCLAIMED，未复制旧 Session，未激活真实账号 |
| 103 条旧动态 | 83 条现存私人档案，20 条删除墓碑 | 未转换为当前公开 Moment，保留原多地点、串文和未知时间 |
| 34 个旅程 | 私人旧档案，包括 12 条墓碑 | 未转成当前可编辑或公开 Journey |
| 3 个主题地图 | 私人旧档案 | 未转成当前公开 ThemeMap |
| 189 条地点收藏 | 私人旧收藏档案 | 未生成当前 SavedItems，等待本人及合法当前 Place 映射 |

旅程、地图和收藏的 `remote-personal-apply149-01` 已返回 `PERSONAL_ARCHIVES_COMMITTED`；原操作重读一致，源写入 0，媒体导入 false。完整凭据保存在 `docs/testing/evidence/social-remote-migration-20261015/`。

本机通过现有 SSH loopback 通道读取真实远端 `/healthz`、`/readyz`、`/v1/cities` 均返回 200。它证明实际远端接入；公网仍由旧 Civu 服务承接，尚未完成唯一 Birdtie 公网切流。

尚未完成：5511 个旧 Place 和 339 个旧城市的合法当前目录映射；21 个旧认证身份续接；407 个媒体对象的逐对象扫描、权利、派生及交付；旧关系、互动、消息、通知与组织的业务承接。媒体完整性匹配 335 个、差额 72 个，完整性通过不等于已获准公开。旧源库、媒体与加密备份继续保留。

## 本轮新增功能

首页创建材料已接入原 SecureStore，按来源、账号、城市与类型隔离，保留标题、正文、UTC 日期及旅程和主题地图的地点顺序。迟到读取不能覆盖新编辑，缺失地点不会静默丢弃；恢复内容仍是本机草稿。独占冻结帧的直接验证为 25 PASS，第一次 7 个界面失败及修复记录保留。

城市文字动态已沿原 MomentControl 的发布、公开修改及撤回动作实现；追加迁移 150，不改已应用的 145、148、149。新公开详情 v4 明确城市名称及位置精度，无地点时不伪造地点或地图点。单图保持原独立公开批准，城市照片和公开视频尚不可用。必要 Go 直接与隔离 PostgreSQL 链通过；客户端模型和操作验证 20 PASS，相关代码分析 0 error、0 warning。这些是各自源码帧的证据，没有合称一次最终全站测试。

完整本人及作者分页、城市和关系动态列表、点赞评论及通知管理正在原 SOC 清单中接续。多图、公开视频和包含媒体的原子串文尚未完成。当前不把本机材料预览、私人媒体或局部测试称为这些公开能力的交付。

本轮证据根为 `docs/testing/evidence/github-remote-completion-20261015/`。正常字号、不录屏；Closed Pilot 和 Consumer Beta 保留原门槛。
