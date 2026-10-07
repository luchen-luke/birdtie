# 媒体本地解码与重编码

2026-10-06，原 BT-V5-AIR-030 的本地处理分支；不新增需求队列，不替代 AIR 的媒体授权规则。

## 已实现的代码范围

`media.LocalImageDerivativeBuilder` 实现已有 `DerivativeBuilder` 接口，接收本地字节流，只返回重编码后的静态 JPEG/PNG。原 `contract.go` 保留。接口中的 `BuildPublic` 是既有名字，返回字节不代表公开授权、实际发布或允许 AI 分析；此实现没有存储、网络或模型调用能力。

- 服务端统一上限：输入 10 MiB、单边 8192、像素 16 Mi、输出 16 MiB。读取及像素分配前检查，超限无结果；不得由上传者修改这些限制。
- 声明 MIME 必须与实际 JPEG/PNG 匹配；GIF、SVG、APNG 与不支持的格式拒绝。解码失败不返回“处理成功”。
- JPEG APP1 / PNG eXIf 仅解析有界 TIFF IFD0 方向，支持两个字节序和 1–8 方向，包括镜像。错误偏移、计数、类型、重复方向/EXIF 或非法方向拒绝，不猜方向。
- 仅把解码像素传给标准库编码器。EXIF、GPS、拍摄时间、XMP、PNG 文本、文件名及原 OCR 文本块不复制到输出。方向变换不把元数据当成本人到访证据。
- 读取/输出/逐行方向变换响应取消；输入读失败、取消、输出超限不返回衍生图。编解码 CPU 在单次有界标准库调用中不能提供实时抢占承诺。

## 尚未完成的消费链

实际仓库此前只有 `UploadIntent`、`Storage`、`Scanner`、`MetadataExtractor`、`DerivativeBuilder` 契约，没有上传存储/扫描/提取的当前调用者。本轮完成具体 codec，不能将它描述为原生媒体流水线已经接通。

上传归属与哈希/有效期核验、受认证保护的存储生命周期、用户分析同意和外部出口同意、当前授权的最终重验、可信本地隐私检测/遮挡/用户选择、消费者 UI 以及当前模型入口均仍待接入。剥离 EXIF **不会**遮挡图像像素中的人脸、地址或其他敏感信息；没有相应同意与前置隐私处理不得把衍生图发给视觉供应商。无法安全判断时应沿原需求提供本地处理/遮挡/不处理选择。

没有新 HTTP、模型工具、公开发布或媒体自动写入口；现有文本模型、身份、记忆来源与发布门槛不改变。BT-V5-AIR-030 仍为 PARTIAL，不让依赖完整媒体安全链的任务提前领取。

## 本轮验证

仅运行本 codec 的 5 个顶层单元测试（39 个含子测试的运行事件），`apps/api` 目录，退出码 0，0 失败/跳过。覆盖八方向与字节序、像素正确性、JPEG/PNG 元数据 canary、MIME 错配、截断及恶意 TIFF、APNG、输入/尺寸/像素/输出边界、取消和读失败。测试使用合成图片，无模型、存储或数据库连接。

精确命令、原始 JSON 日志、退出码与源 SHA 见 [本地处理证据](../testing/evidence/media-local-preprocessing-2026-10-06/README.md)。原生上传/授权矩阵/实际出网捕获/存储/真实图片用户验收、全面测试、分析、编译及真机均 NOT_RUN。Closed Pilot / Consumer Beta 仍为 NO。

## 2026-10-06 增量：已保存私人 Moment 的图片消费者

原 codec 批次的 39 个单元事件及其历史结论保持。本次只接通 BT-V5-AIR-030 中“已保存、本人、PRIVATE/DRAFT Moment”的直接图片入口，不替代完整媒体/视觉能力，也不覆盖 Now 加号中的图片导入。

### 当前实际消费路径

原资料页 → 本人的已保存私人记录 → 编辑动态 → **管理私人图片** → 明确选择一张系统图库 JPEG/PNG → 在本机解码并重编码 → 检查敏感内容、划定实色遮挡或清除 → 生成短期保存预览 → 检查具体图片与后果 → 明确确认私人保存 → 本人读取或确认移除。

- 新系统图库适配器只调用单图 gallery 选择，`requestFullMetadata: false`；无相机、语音、广泛文件/相册扫描。取消或选择器迟到不上传；进程遗失的选择只提示重新选择，不借给新身份。iOS 仅增加中文图库用途说明，Android 原权限不修改。原生插件调用、权限和注册仍未验。
- 本机读取上限 10 MiB，分配像素前检查静态 JPEG/PNG 图片头、8192 单边与 16 Mi 像素；拒绝重复 PNG IHDR / JPEG SOF、动画和截断。实际本机 PNG 重编码及实色遮挡使用 `dart:ui`，相关单元检查遮挡区域像素。头部结构单元不等于原生所有恶意图片解码安全或性能验收。
- 图片风险始终为 **UNKNOWN** 或 **USER_MASKED**。去除原文件编码/元数据不代表去除敏感像素；衍生图仍保存未遮挡像素。没有自动人脸、地址、OCR 或 PII 检测，不称 SAFE、匿名化或扫描通过。当前 UI 不提供 AI 分析批准，不发送模型、Memory 或公开出口。

### 原生权限、实际存储和生命周期实现

新增 `media.HumanPrivateImageStore` 的具体 PostgreSQL 实现及受原 `momentActor` 认证保护的七个 HTTP 路径。主服务复用同一原 content Store 的接口断言，无旧 owner-ID fallback、开发模拟保存或新启动开关。缺少迁移 100 时返回不可用，不能称已存储。

迁移 100 的 `private_moment_images` 保存实际衍生 BYTEA、真实字节哈希、原 input 哈希、操作 ID、当前 owner、Moment/source/session 绑定、版本和绝对期限；只写原 `media_assets` 的 `ready_private`，不写原私密元数据、公开 variant 或原始编码文件。期限为 5 分钟预览、最长 30 天逻辑保留；移除清 BYTEA 并保留不可复活的操作墓碑。未执行物理清理，不承诺磁盘页擦除。

原 Account / optional Agent → Moment UPDATE → image / canonical asset → Session 锁顺序与最后当前钟收口继续有效。Moment 锁串行限制最多 12 个当前预览或附件；列表在 LIMIT 前过滤过期行。保存前后验证同一具体 bytes/hash、预览 source/session 与期限；响应编码后，整份读取集合（包括空集合）通过同一 materialized clock 的 source/session/asset/state/hash/deadline SQL，随后直接 Commit，并在输出前拒绝取消请求。

普通本人私人文字编辑不会静默藏起 READY 图片：当前人类读取重新核验本人、当前资源可见性、资产和期限，旧上传预览仍失效。公开、撤回或作者变化由迁移中的 trigger 清字节并保留墓碑，之后恢复私人也不能复活旧附件。以上为具体源码、SQL-spy、纯函数与静态顺序/DDL 单元证据，**尚未实际运行 PostgreSQL 100、trigger、事务竞争或迁移**。

### 草稿、确认与未知结果

本机选择/遮挡、服务器 metadata 预览和私人像素保存分开。明确保存绑定当前图片哈希、具体预览 ID、当前 Moment/身份/工作范围与版本，账号、token、源或组织 ABA 永久退休旧页面；迟到选择、响应与确认不得带到新身份。401/403/409 要关闭页面并恢复身份或重新打开当前私人记录，旧确认不能重复使用。

预览 POST 丢失生成的预览 ID 时，只用本人的 client operation ID 读回执；PUT/DELETE 的网络、408、5xx、非预期/坏格式成功为结果未知，只 GET 核实，不盲重发。保存回执必须匹配已确认的具体预览 ID，移除确认必须匹配当前列表对象和版本。删除墓碑只可本人读 metadata，不能读图片或复活。

**当前操作 ID 只在该页面/controller 生命周期中保存。关闭页或重启后，精确原操作恢复尚未实现**；重新打开只能读取当前已保存图片状态，不能据此宣称恢复了丢失的原 operation。提示用户先检查当前记录，不直接重复添加。跨重启安全恢复仍是 AIR-030 的明确缺项。

### 本批验证与剩余门槛

只运行本需求相关四个 Flutter 单元文件（含原组件挂载）和 Go `TestPrivateMomentImage` 三个 package 的直接单元，原始命令/CWD/退出码、失败与最终通过日志、源码 SHA 和基线差异见 [消费者证据](../testing/evidence/private-moment-image-consumer-2026-10-06/README.md)。未重跑原 39 codec、全量测试、analysis、编译、真机或部署。

`flutter pub get` 在 Windows 插件 symlink 检查退出 1；依赖解析和 lock/package_config 可供 VM 单元使用，不能说原生插件生成、注册或构建成功。Windows 原有 generated 修改无本次 before 归因证据，保持现状，不恢复或重置。

BT-V5-AIR-030 继续 **PARTIAL**：实际迁移/事务授权与生命周期、真实图库原生调用、平台构建/真机/辅助技术/真实图片与内存性能、跨重启精确操作恢复、完整外部 Vision 同意与出网验收、Now 图片入口等未完成或 NOT_RUN。原发布门槛继续有效，Closed Pilot / Consumer Beta 仍 NO。

最后直接单位补注：busy 状态的同步监听器也可令当前身份/源失效；controller 在同步通知后、HTTP/gallery 之前再次观察并核验同一请求代际，实际 load/delete/select RED 后相关单位通过。此后只重跑该 controller 的相关单元，未将此前四文件整组通过冒充新修复后的重复整矩阵。


## 2026-10-06 接续：私人图片原操作恢复引用

上节“操作 ID 只在页面生命周期中保存”是上一批的历史状态。本接续不修改已封存消费者/codec证据，不改变原生七条接口、权限或迁移 100，只补原私人图片页面的客户端恢复消费链。

- 原 `PrivateMomentMediaController` 默认接现有 `FlutterSecureStorage` 的具体适配器。按 API 环境、本人和 Moment 分区，只持久保存闭合的 operation/phase、原输入 hash/MIME/大小/risk、具体 asset/版本、原 Moment 上传版本与有限 UTC 期限等引用。**不保存照片、像素、token、会话、标题、正文或批准**，也不向模型或公开出口传递。
- 原 POST 预览、PUT 保存、DELETE 移除必须先完成本机 journal 写入，再重新观察本人、token、工作范围、源和请求代际。读取、写入或清理恢复记录失败时，当前页面阻止新写；同步监听器或等待期间身份/source ABA 时不发旧请求。确认回执后清理失败仍显示待核实，不能重复提交。
- 关闭并从原图片入口重新打开时，先读取该本人/环境/记录的 journal；有待核实项时不借图片列表代替原 operation。用户明确选择待核实项并点击“核实当前操作结果”，只 GET 原 operation；不会自动 POST/PUT/DELETE。坏记录、404、网络/服务错误、身份/权限或版本变化不能当作 NO_EFFECT，也不能盲清引用或自动重发。
- 相同原 owner/Moment/operation/asset/输入绑定的 READY，可在当前合法身份和普通私人文字编辑后重新核实，期限依据实际 `retainUntil`；旧预览五分钟期限不是 READY 或 DELETE 墓碑的 GET 禁令。预览仍依原服务的 session/source 与实际预览期限；过期不代表未产生效果。DELETE 只核实同一 asset 当前墓碑及更高版本，**不是新建独立 DELETE 因果回执**。
- 重开后的恢复引用不恢复照片、`localReviewed` 或 `canSave`。当前同一个 controller 内仍实际持有且已检查的未变图片可以保留现有合法预览流程；关闭后必须重新选择、检查和明确确认，不能借 journal 复活旧批准。

### 本接续直接单元与真实未验范围

原关闭重开路径 RED：结果未知的保存关闭后丢失 operation，错误读成非待核实；原日志退出 1。实现后最终仅运行 pending-store/controller/sheet 三文件：**52 个行为单元、3 个加载事件，0 failure/error/skip，退出 0**。覆盖实际 SecureStorage adapter 的插件 mock 对象重建、三类真实写响应丢失、原页面关闭/打开/明确 GET、存储失败、原输入绑定、错误/过期、新合法 token、文字版本变化与 ABA。原 controller 18 / sheet 4 旧 case 正文和断言保留；fixture 注入独立 memory store，删除回执版本由 2 修为 3 以符合原 SQL `revision+1`，未放宽断言。新增 sheet 夹具误传参数的编译失败完整保留，不称产品或原生问题。

精确原始日志、CWD/参数/退出码、来源帧 SHA、基线差异及保留声明见 [操作恢复单元证据](../testing/evidence/private-moment-image-operation-recovery-2026-10-06/README.md)。没有重跑旧 codec/Go、全量测试、analysis、build、迁移或手机。插件 mock 和 controller 重建 **不等于真实平台安全存储、进程重启、原生图库、PG 100 或投递环境验收**。同设备独立并发窗口的 journal 竞争也未实测。

BT-V5-AIR-030 整体仍 **PARTIAL**：原生上传/授权/存储与生命周期、真实平台安全存储跨进程持久性、真实图库/照片/内存/辅助技术、Now 图片入口、自动 PII 或明确 Vision 出网同意等缺口保持。Closed Pilot / Consumer Beta 仍 NO。

## 2026-10-06 接续：Now 明确选择私人记录的图片入口

以上批次的证据与未验收结论保持。当前在原 BT-FIX-NOW-UI-001 的 OBS-21 / UIR-014 / QA-22 下补充真实消费入口，不另建媒体需求或将 SocialIntent 当作 Moment。

- Now 统一输入框的 `+` → **添加素材 / 为私人记录添加图片** → 选择原 `PrivateMomentController` 实际读取的本人已保存 `private / draft` Moment → 原 `PrivateMomentMediaController` 与图片管理页。没有自动选择图片、创建空记录、复制 Now 草稿、上传、公开、AI 或 Memory 动作；图片不加入当前 Now 对话。原文字/链接素材与六个快捷任务保留。
- 登录与个人工作范围必须有效；Moment 的 owner、授权头及 API 环境须与当前 Now 本人一致。读取和图片管理借用该 Moment 的原 client/base；子页不关闭借用 client。仅展示具有本人 author、合法 ID、正版本及当前 PRIVATE/DRAFT 的记录。空列表、读取失败、401/403 和版本/来源失效分别显示真实中文状态，不将失败当作没有记录，不提供无效权限重试。
- 原 Moment 刷新增加可选的当前消费者谓词；默认调用语义不改变。同步 loading 通知后、GET 前和 await 后核验捕获身份/请求与消费者；请求失效时只清本请求 loading，不清后来请求。来源替换即时递增独立 personal serial，再于构建后发布视觉通知，避免已挂载读者在另一页面构建中触发 AnimatedBuilder 报错。已有资料/设置边界与新入口捕获 serial；A→B→A 不能复活旧入口，也不清独立 Now 任务、地图或未发送素材草稿。
- 选择页自身捕获原 auth/moments/identityChanges/current/Org 回调，widget 同 key 换来源或事件 ABA 永久退休；图片管理仍绑定原具体 Moment 版本。取消/返回暂停编辑，不自动弹键盘。新入口不会借当前列表或新来源恢复旧批准。

本批仅直接相关 Flutter 单元：入口 31 项后，追加一个实际已粘贴素材的 Moment-only 来源替换控制，连同 3 项原素材单元单独运行。共 **35 个唯一行为单元**通过（两条命令的加载事件分别 1、2），没有为了封包重跑整组。完整命令/CWD/退出码、原 missing-entry / pre-wire / build 报错及测试异步夹具误序、当前源 SHA 和差异见 [Now 私人图片入口单元证据](../testing/evidence/now-private-moment-image-entry-2026-10-06/README.md)。原素材测试文件字节不变，原媒体三源及其 52 项单元未修改或重跑；只记录本批冻结时引用版本，不保证并行后续版本不变。

本入口测试的 HTTP/图库/安全存储均为本地合成或插件 mock，不等于真实 API 上传、系统图库、进程重启、PG 100 或授权生命周期通过。实际新 Now→图库→保存/重开、手机/辅助技术/性能、analysis、build、全面回归与外部 Vision 均 NOT_RUN；原媒体控制器的等待衔接候选由原 AIR-030 范围继续独立核验。本轮不解决原生首次触摸 UNKNOWN，也不扩展语音/文件/长文素材或外部图像分析。Now 与 AIR-030 整体仍 **PARTIAL**，Closed Pilot / Consumer Beta 仍 NO。

## 2026-10-07 增量：显式本机文字隐私辅助

原 AIR-030 消费者分支新增：私人图片选择/本机重编码后，用户主动点击「检查图片中的文字（本机）」→实际 Android 本机 OCR → 有界邮箱/电话候选行与图片提示框 → 用户勾选、检查并明确确认 → 原 `mask()` 实色像素处理。没有隐式扫描、默认人脸识别、敏感属性推断、自动遮挡/保存/上传/AI/Memory 或公开出口。其他平台/不可用/异常继续提供原手动遮挡、清除与私人保存；无结果也不能宣称安全。

具体消费者是 `PrivateMomentMediaSheet` / `PrivateMomentMediaController` → `MomentImageTextRecognizer` 的封闭 MethodChannel → Android `LocalImageTextRecognizer`，沿原 MainActivity 注册，不新建 HTTP 或权限。仅处理当前原重编码图片，同一次 request ID/hash/确定性缩放尺寸绑定，限制位置/文本；用户看到的是可能的邮箱/电话号码提示，不把 OCR 指令当执行指令，也不持久保存 OCR。所有等待后的原身份/来源与图片代际守卫继续有效；换图、清除、停止、关闭、退休丢旧结果与旧确认。识别不授予上传、模型出口、长期记忆或公开权限；风险仍只有 UNKNOWN/USER_MASKED。

Android 使用标准单线程 Tesseract4Android 4.9.0，JitPack 仓库仅开放对应 group；模型 `chi_sim+eng` 随包，固定官方 tessdata_fast commit `87416418657359cb625c412a48b6e1d6d41c29bd`，没有运行时下载。模型分别 SHA256 `a5fcb6f0db1e1d6d8522f39db4e848f05984669172e584e8d76b6b3141e1f730` / `7d4322bd2a7749724879683fc3912cb542f19906c83bcc1a52132556427170b2`。完整 wrapper/Tesseract/tessdata Apache-2.0、Leptonica、libjpeg/IJG、libpng 许可和来源保留于 `apps/client/android/app/src/main/assets/birdtie_ocr/`，原上游 URL、下载 SHA 及开发来源见本批证据。模型仅复制至应用 noBackup 私有目录并校验 hash；照片和 OCR 不写文件，不启用云服务 SDK、账号或模型出口。上游源码/manifest 静态审查不等于实际 AAR、离线或无遥测验收。

原图输入仍受 10 MiB、8192 单边/16 Mi 像素约束；识别尺寸按统一整数采样算法固定到最长 1600、最多 1,000,000 像素，再规范 Bitmap 到该精确尺寸。图小字、模糊文字或采样可能遗漏，用户界面明确告知；不是全图 PII 检测。回执最多64行、每行1024 UTF16单元、合计8192，最多24提示。原生进程单在途工作持有到真实 finally 清理结束；Dart timeout 不释放原生工作。`getHOCRText(0)` 先做可合作停止的识别，随后只读取 ResultIterator 有界行，HOCR 不保留/输出。取消标记由识别 worker 的 progress callback 调用 stop；Tess 方法不跨线程。30秒deadline是合作式检查，初始化/Bitmap 解码/内部 HOCR 分配无法承诺硬实时抢占或实测峰值内存。

验证只运行本批3个新直接测试文件。首轮旧页真实缺入口 RED；实现首轮15行为13通过、2新滚动/惰性布局夹具失败，保留原日志；纠正夹具后仅两受影响 widget 通过。唯一15行为具备通过证据，未再次整组重跑；具体命令、CWD、退出码、执行快照、原始失败、当前源/模型SHA和原测试字节保留见 [本机文字辅助证据](../testing/evidence/private-moment-local-image-text-2026-10-07/README.md)。MethodChannel 模拟平台不等于真实 OCR 执行。

本机文字辅助源码/相关单位已接，AIR-030 整体仍 PARTIAL：原生构建/注册/模型打包、真实识别质量/取消/离线出网捕获、实际OS/真机/辅助技术/内存性能、PG100/迁移与存储均 NOT_RUN；iOS/web/桌面没有 OCR 实现。完整视觉用途/用户AI与媒体出口同意/可信 Vision 消费者、全面PII检测仍未实现。Closed Pilot/Consumer Beta 继续 NO；不重跑原39 codec/52恢复等历史单元或全面测试/分析/编译。
