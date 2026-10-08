# BirdTie UI 修复包增量交付记录

更新时间：2026-10-08。正式仓库：`D:/Project/birdtie`。状态：**PARTIAL，未完成整体验收**。

来源为用户指定的 `D:/Project/birdtie/BirdTie-UI-Repair-Package-2026-10-07.zip` 中 01–05 文件及 42 条候选。修复复用原 `BT-FIX-NOW-UI-001`、AIR、AGE 与领域任务，未创建平行队列。原任务历史、未提交工作及旧 worktree 均保留。本报告是实际改动与证据记录。

## 实际代码

- `sidebar.dart`：权限驱动组织导航，底部同一行左新建、右账户；历史在中间滚动，个人资产、组织和原账号菜单保留。
- `organization_workspaces.dart`：401/403、角色撤销、账号变化与迟到响应退役；仅身份/角色实际变化才清退旧工作区请求。
- `top_controls.dart`、`map_workspace.dart`：对称顶栏；城市入口统一到顶栏，线上等范围从同一选择器进入；不因组织加载通知重置 Now。
- `agent_workspace_controller.dart`：每轮回复及实体集合绑定当前用户消息前缀，保留历史结果与同一实体的选择；历史地图浏览不改写最新查询权限。
- `remote_agent_task_source.dart`、`agent_workspace_controller.dart` 与 Go `intent_parser.go`：匿名地点续问保留上一成功地点条件；新地点问题替换旧条件，带引号的地点名称按值处理，身份/来源变化后不复活旧条件。不把“还有别的吗”等尚未支持的问题伪造成成功。
- `map_workspace.dart`：城市、本人当前/过去/声明范围、线上及组织范围归入同一个有限高度选择器。提交已复核的异地范围时同步原 City 控制器；不因选择目的地立刻删除原任务、地图或草稿，未知城市不发送新请求。
- `agent_conversation.dart`、`agent_result_sheet.dart`：解释、卡片及来源处于同一消息列表；键盘打开后展开会话，消息和输入连续布局；历史结果详情仍由原领域端点重新读取。
- `agent_entity_result_card.dart`：可点 HTTP(S) 来源以及同实体地图查看；拒绝 dev-seed、javascript、带凭据及相对 URL。
- `map_canvas.dart`：地图使用控制器所呈现的同一结果集；保留地图实例及选择的迟到回调守卫。
- `agent_composer.dart`：移除未接入 Now 会话的加号；原私人 Moment 资产路径与安全草稿逻辑保留。**Now 附件协议仍未接通**。
- `native_city_map_io.dart`、`native_map_failure.dart`：原生资源失败显示可读原因与重试；去掉全屏绿遮罩；同实例重试保留相机和实体，完整地图加载后清错。
- 原 Civu 复用矩阵追加具体文件、依赖与许可审查。没有复制 Civu 业务源码、目录、媒体或数据。

完整 42 条候选映射见 `TASK-MAPPING.md`。候选状态不回写成原领域任务已完成。

## 构建与验证

第一阶段冻结源码：11 个受影响文件测试 **211 PASS**；全量 Flutter analyze 无问题；Android build03 成功并安装 Xiaomi 25098PN5AC / Android 16 / c641566b。build03 证据在 `work/ui-repair-2026-10-07/build03-evidence.json`。该版本的地图瓦片 403，因此地图验收失败，保留原始录屏和脱敏日志。

在真机发现缺口后补齐底栏同 Row、工具中的重复城市入口及原生地图错误提示。12 文件阶段 **223 PASS**，全量 analyze 与 Android build04 成功。APK04 已验证新令牌底图、卡片→地图→详情→返回，也真实复现了“找地点 3 个、续问地点变 0 个”；该失败录屏保留。

历史 APK05 冻结阶段：18 个直接相关 Flutter 测试文件 **317 PASS**；`flutter analyze --no-pub` 为 No issues found；`go test ./internal/agentworkspace -count=1 -json`、API05 与 Android build05 全部 exit 0。15 个受影响生产/Go 测试文件在验证前后 SHA256 相同。凭证：`work/ui-repair-2026-10-07/followup03-verification-results.json`、`followup03-source-before.json`、`followup03-source-after.json`。早期 311 PASS/3 FAIL 及 analyze 7 info 原始失败日志保留，未覆盖。

Mapbox 新公开移动令牌已放入被 Git 忽略的本地配置，实际瓦片预检返回 HTTP 200。旧网页令牌限制来源 `http://localhost:7357`，不适用于原生 SDK。新 APK05 已在 Xiaomi 25098PN5AC / Android 16 / c641566b 显示原生底图。

## 真机与同构建证据

- APK05 SHA256：`db4fcce0fab17c116fafa3451e1fd1d5a8a2ff3a26ff56d26c292098bf29577d`；API05 SHA256：`7ff7f4d5d2a2209d60c90ba6ef2fb4a0f3223ed22c9c90387c600c70ddcb8115`。`adb install -r` 保留应用数据，没有执行 clear；安装/设备/源码凭证在 `device/build05-install.receipt.json`。
- 同 APK05/API05：统一城市选择器→中文 IME 输入“找地点”→3 个开发地点卡片→地图选中体育馆→同体育馆详情→返回对话→续问“地点”仍 3 个→原地图选择保留。录屏 `device/build05-after-followup.mp4`；截图 32、36–42；`device/BUILD05-EVIDENCE-FINAL.json` 记录文件与构建 hash。
- 同 APK05/API05 的会话/地图切换执行 **20 对**，每对保存地图和会话截图，命令全部 exit 0。20 张地图截图的裁剪区像素完全相同；首、末地图与末会话人工检查同一体育馆、3 个地点及同一视野。`device/switch-cycles-05/receipt.json`、`visual-check.json` 与 `device/build05-switches.mp4`。这是该开发结果集的视图稳定性证据，不是所有地图点选/身份/性能矩阵通过。
- 修复前的特定缺陷证据：`device/build04-before-followup.mp4`（APK04，3→0）；修复后 APK05 为 3→3。前后修复包是不同 APK，明确区分；APK05 内连续操作和 20 对切换属于同一构建。没有原始整套 UI 修复前的完整录屏，记 **NOT_RUN**。

## 实际运行边界

只启动本次自有只读 API（127.0.0.1:18090），在录屏检查点核验旧自有进程的准确 exe/监听端口后替换为 API05。连接已有隔离开发数据库 `birdtie_oct07_phone_ec1d5cb204`，没有导入、重置或执行 migration；PostgreSQL 强制只读。站内匿名地点查询实际 HTTP 200、规则回答与 3 个开发地点可用；这些来源是 **dev-seed:// 开发示例**，不是真实城市供给或联网来源。详情中的公开摘要目前不可用，没有计为成功。

腾讯云 TokenHub 配置与官方计费文档已检查；已有收据中的 authenticated GET /v1/models 状态为 NOT_AVAILABLE，不能把 models.html 计费页面当成账户模型已开通证明。尚未取得成功的实际模型生成证据。用户已授权本次开发验证累计 ¥10、每次供应商 API 请求 ¥0.20；联网工具截图显示剩余 49,979/50,000 次免费调用，但后付费仍启用。费用事实、免费资源消耗及实际搜索次数须分别记录；不能把 usage 当账单或默认费用为零。

用户新提供的 birdtie-dev WSA 专用 key 已保存到被 Git 忽略的 `apps/api/.env.wsa.local.json`，未写入源码、日志或客户端。本次累计/单次预算不变，须由原 062 账本共同约束 WSA 与模型。TokenHub 免费搜索次数不能推定属于 WSA 免费资源。

AIR-009 已在 UI 自然检查点后串行接续。`tencent_tokenhub.go`/测试修复默认 HTTP/2/请求体重放与连接复用的隐藏重发风险，固定 HY3 `thinking.type=disabled`，完善空搜索与缓存 token 合同；modelgateway **379 测试及子测试 PASS，0 FAIL/0 SKIP**，凭证 `tencent-wire-fix-01.result.json`。此后端增量发生在 API05 构建之后，未声称已包含在 APK05/API05 中，也未声称真实派发可用。

## 尚未验收

- 真实模型 + 站内/联网检索 + 来源链接 + 可点实体 + 同结果地图 + 多轮完整闭环：**PARTIAL / 首次真实尝试 FAILED**，模型、来源回答与同构建多轮验收仍 NOT_RUN。
- 开发地点 UI/地图连续流程和特定缺陷前后录屏：已取得历史证据；真实官网 Place 已通过原流程发布到批准的隔离库。后续 APK07 已实际打开官网来源，见检查点；真实检索回答完整闭环仍未完成。dev-seed 链接不能证明真实 HTTP(S) 来源打开。
- 本增量全部 Flutter 套件、真实 IdP/组织深链撤权、无障碍完整矩阵、性能 profile、iOS：**NOT_RUN**。Go 全套实际运行有失败，详见后续检查点，不记为 NOT_RUN 或通过。
- Now 附件、就地登录待办恢复、历史搜索、完整 Business 导航以及 LIFE/导入候选的缺口按映射表保留。
- 未部署生产；源码已在后续检查点正常推送 GitHub，见交付记录。没有用测试活动、固定长文或 API key 可用代替基础 Agent 验收。

## 2026-10-08 真实链接续检查点

本节更新上述历史运行边界。基础 Agent 复用原 058 配置、062 预算、Task 与 Run；真实调用只有明确允许的本地验证账号生效。新 107–110 迁移只应用于新隔离库 `birdtie_ui_live_20261008_b160550993`，没有更改 APK05 使用的旧库或生产数据。Now 保留原始问题，同时从当前 CITY Task 编译城市和公开筛选条件；不把历史、个人画像、私有来源、地图坐标或结果 ID 发给供应商。

用户已独立批准 Art Gallery、Maritime Museum、Provost Skene’s House 三条官网事实候选，仅限该隔离库。通过原 contributor/reviewer 提交审核流程发布，三个真实 Place 的详情和公开地图 GET 均 HTTP 200，entityRef/detailRef/anchor 使用相同 ID。城市完整状态仍为 building，不能称整个 Aberdeen 供给已维护完毕。具体审核、来源与结果在 `work/ui-repair-2026-10-07/live06-city-candidates-02.freeze.json`；没有导入活动或 Civu 数据。

API06/APK06 已真实安装运行。2026-10-08 05:53:59 真机输入 `find place Aberdeen Art Gallery`，原 Now POST 返回 500。原 Task 保持 ACTIVE，Run 为 STOPPED；搜索 CALL 保留 UNKNOWN 的 80,000 µCNY（¥0.08）最坏上界占用，模型未预留或调用。没有重发旧 operation、退款 UNKNOWN 或重置账户。旧日志不能确定具体失败阶段，记为 **FAILED / CAUSE_NOT_DETERMINABLE**；新后端已补固定阶段诊断并保留错误身份，屏蔽查询、URL、密钥及供应商任意错误文本。提交前录屏与失败截图保留；核验末帧显示录屏结束在提交前，不把该视频当成响应或完成证据。

现已补失败后 Recent 的一次只读刷新，保持原错误、地图和会话；异步结果受账号、组织、城市、线上范围、生命周期与轮次守卫约束，不自动重试 POST、恢复或完成任务。APK07 定向 **189 PASS / 0 FAIL / 0 SKIP**，全量 analyze 无问题，debug APK 构建成功；257 个生产输入和 267 个测试输入的完整快照前后一致。APK SHA256 `f9843cb2b07a1c127c0cd1265e274816fd81dcf8436c6df9088d04052a2a6626`，已 `install -r` 到同一真机并核验实际 base.apk hash，没有 clear。源码和命令收据为 `work/ui-repair-2026-10-07/build07-client-01/freeze.json` 与 `device/build07-install.receipt.json`。

110 的真实 SQL 回归与原预算/授权/请求体守卫合并运行：**189 PASS / 0 FAIL / 0 SKIP**，源码前后未变，供应商调用 0，凭据 `live-source-native-db-08.result.json`。包含当前 Task 两轮、CITY 撤权及恢复、真实 HTTP/1 请求体发送前撤权和新旧范围混配拒绝。较早 db-07 为 19 PASS，但独立 fixture 同时编辑导致 sourceUnchanged=false，已保留并由 db-08 重跑，不把 db-07 称为冻结证明。

上述后端测试使用真实 SQL 和伪供应商 HTTP，仍不证明供应商真实回答。APK07 同构建的联网来源、真实模型、可点卡片、地图与多轮录屏：**NOT_RUN**，待后续实际验证更新；全清单保持 PARTIAL。费用限制已澄清为每次供应商 API 请求 ≤¥0.20、原账户累计 ≤¥10；UNKNOWN 上界占用不等于账单或确认免费。

### API07 / APK07 真实失败与恢复验证

API07 SHA256 为 `042b03eef90109a21e48733b070ad0a9f2207ad4e253ca5208b0f89fbbd314f4`，与上述 APK07 在同一真机运行。2026-10-08 06:43:37 实际 Now POST 仍返回 500，固定诊断阶段为 `native live Run source-preview-payload: DENIED`；没有把 API06 无法判定的原因回填成这个新原因。新 Task 保持 ACTIVE，Run STOPPED，模型仍 PLANNED/WAITING_SOURCE，没有 TOKEN 预留或原生发送许可。来源批次尚未持久化；账本不能单独证明实际联网发送次数。原 UNKNOWN 占用 ¥0.08 完全保留，新 CALL 增加 ¥0.08，累计最坏上界为 **¥0.16**，不是确认账单。凭据：`live07-native-fee-before01.json`、`live07-native-fee-after01.json`、`live07-native-fee-delta01.json`。

录屏 `device/build07-live01.mp4` 为 H.264 610×1328、180.346311 秒，实际包含输入、发送和失败；末帧已提取并人工核验，区别于 API06 提交前结束的录屏。截图 `build07-failed-response.png`、`build07-failed-recent.png` 证实失败后 Recent 已显示两个原任务。选择最新任务后，原 GET 恢复 1 个 Art Gallery 卡片、官方来源及同一地点地图选择；来源点击真实打开 Chrome 中 Aberdeen City Council 美术馆页面。该恢复回答为站内规则回答，**不是模型回答或成功联网闭环**。恢复和来源打开录屏为 `device/build07-failed-restore01.mp4`，地图联动另有 `device/build07-real-place-map.png` 截图，不声称该录屏已包含所有地图操作。

原正常 HTTP 鉴权会延长仍有效 Session 的空闲期限；地图证明以前绑定整行 xmin，会把这种正常刷新误认为撤权。111 迁移改为原 sessions 的授权代数，安全字段变化、过期恢复、缩短空闲期限仍使旧证明失效；地图许可的原截止时间不延长。仅在同一批准隔离库应用。真实 SQL 地图回归 **34 PASS / 0 FAIL / 0 SKIP**，凭据 `live-source-native-db-09.result.json`。APK07 重新登录原账号后显示三处真实地点底图，没有此前地图错误覆盖层；完整撤权真机矩阵仍 NOT_RUN。

Go 全套 `go test ./...` 实际结果为 **9074 PASS / 143 FAIL / 2805 SKIP**（按 JSON 测试及子测试事件计）。140 个失败事件在缺少显式隔离库参数时由测试安全前置条件拒绝，未执行对应 SQL 断言；其余 3 个事件暴露严格只读工具 schema 与实际 sources 字段不一致。该真实 schema 缺口已修复，agenttool 定向 **399 PASS / 0 FAIL / 0 SKIP**。后续冻结后端定向 **493 PASS / 0 FAIL / 173 SKIP**、API/维护 CLI 构建成功；173 个跳过项在该命令中为 NOT_RUN，独立 SQL 回归按各自收据记录，不累加成全套通过。凭据：`build07-backend-01.receipt.json`、`readonly-registry-sources-01/result.json`、`build07-backend-02.receipt.json`。

源码已形成本地检查点 `02b38fbe591333e1cadc2f780f5729f5d05452e4`，并与实际远端 main 三方合并为 `959282cda5c24ca2c6830e6afaed6b623de4ffe3`；14 个审查文件保留当前内容，合入 3 个原远端 map_link 兼容文件。原队列内容未变，密钥本地配置和运行证据没有提交。**尚未推送**；APK07 是合并前冻结输入，后续构建须重新绑定源码与证据。

### API08–10 / APK08–09 实际续接

新增源码修改原实现，没有新建任务系统：

- `model_egress_live_sources.go`、`model_egress_live_resolved_query.go`：按原生 4096 字节单消息限制，确定性选择完整来源前缀。供应商完整来源批次与摘要保留；模型仅收到实际选中的公开来源，原查询与编译的搜索条件分别处理。没有放宽网关、预算、来源授权或原生请求体限制。新边界单元与子测试 898 PASS，原生真实 SQL 合并回归 213 PASS，均 0 FAIL/0 SKIP。SQL 为伪供应商，不替代真实联网证据；API07 原批次字节没有记录，不能倒推它的确定根因。
- `map_camera_focus.dart`、`native_city_map_io.dart`、`public_city_map.dart`：仅结果坐标、范围或选中实体变化时调整相机；结果顺序、标题与文字变化、取消选择保留用户视角。异步相机操作检查当前输入、用户手势和生命周期，避免迟到结果覆盖当前选择。24 个决策测试通过，完整原生 SDK 手势/离屏实体点选真机矩阵仍 NOT_RUN。
- `map_workspace.dart`：IME 展开计算保留真实地图署名区域与输入框间距，保持会话连续布局。实际几何回归 6 PASS；原失败日志保留。
- `map_canvas.dart`：真实发现未选择城市时，中央按钮仍重复顶栏城市入口。四个新增断言先失败，现移除中央入口，仅显示通过顶栏选择的指导。目录重读仍走原 `loadCities`，不发送 Now。旧测试更新到当前账户 Tools、同一城市选择器、统一会话流和已移除的假附件；保留原地图身份、消息、草稿、授权、迟到请求及尺寸断言，未删测试或跳过原场景。最终 UI 合约回归 112 PASS；早期 81 PASS/36 FAIL 及中间失败日志保留。
- `tencent_failure.go`、`tencent_tokenhub.go`、`live_gateway.go`、`model_request_live_sources.go`：区分模型发送前守卫、实际 HTTP 状态与响应解析阶段。非 200 仅从最多 8192 字节响应提取已审核业务码和固定类型/参数枚举；不输出供应商 message、查询、URL、request ID 或密钥。原错误身份、取消优先、一次发送与 UNKNOWN 占用保留。最后诊断单元 514 PASS，原生诊断 10 PASS/19 SQL NOT_RUN。

APK08 定向 231 PASS；APK09 冻结后运行 15 个去重文件，**315 PASS/0 FAIL/0 SKIP**，全量 analyze 无问题，debug APK 构建成功。不能把 231 与重叠的 112 相加。APK09 258 个生产输入与 268 个测试输入前后一致，SHA256 为 `b9dd69be91e9c3dcaaa0929fe19e8e9c3ac1cab3c645d807f6eeee3b0f3d8e72`；`install -r` 后核验手机实际 base.apk 相同，未 clear。真机截图已观察唯一城市入口、底部新建/账户、真实中文 IME 输入与原生底图。凭据：`build09-client-01/freeze.json`、`device/build09-install.receipt.json`、`ui-contract-regression-01/freeze.json`。

三次新 Now 均通过原 Task/Run 和同一 062 账户预算，各自真实 WSA 批次持久化 3 个来源；没有用来源持久化冒充模型回答：

| 实际时间（北京时间） | 绑定构建 | 模型结果 | 原任务 / Run | 累计 UNKNOWN 最坏占用 |
|---|---|---|---|---|
| 07:20:59 | API08 / APK08 | HTTP 500；MODEL `NATIVE_ERROR`，原因未明 | `fe658aca-bb00-45ca-a8ea-f87ac7875687` ACTIVE / `f8629c22` 开头 Run STOPPED | ¥0.439680 |
| 07:49:28 | API09 / APK09 | 腾讯 HTTP 400；本地分类 INVALID_REQUEST，未记录腾讯业务码；API 500 | `d94b7579-3cef-410d-a4f1-b15e2d024bbf` ACTIVE / `8679039a-206b-4555-944a-06f791d8f3d8` STOPPED | ¥0.719360 |
| 08:08:28 | API10 / 同 APK09 | 腾讯 HTTP 400 / 业务码 **401006** / type invalid_request_error / source client；API 500 | `c4df269b-3442-4f8c-a70a-95e07ca34d39` ACTIVE / `43ec2aad-3de3-4c8d-9737-87fc7a5a248c` STOPPED | ¥0.999040 |

API10 SHA256 `810283a6471be26e8fc5494bf1fa62f2fc909c204ef5acc6958fc2b5257bf075`，定向后端 **710 PASS/0 FAIL/173 SKIP**，API 与维护 CLI 构建成功，1279 个输入前后一致。173 个跳过项是本命令中的 NOT_RUN，不改变前述 Go 全套 FAILED。原生路由版本与 CAS revision 601 保留；仅在准确 PID/exe/端口及空闲 Run 核验后停止本次自有 API09，再启动 API10，未动其他服务。凭据 `build10-backend-01.receipt.json`、`live10-api-01.receipt.json`、`live09-owned-api-stop-01.json`。

腾讯官方将 401006 定义为服务 ID 不存在或模型与服务不匹配，须确认可用服务配置。[腾讯云错误码说明](https://cloud.tencent.com/document/product/1823/131595)。当前固定模型 `hy3` 和广州接入地址匹配已核验的官方指南。随后独立读取同一配置的 `/v1/models` 实际 HTTP 200，118 项中 hy3 和截图中的三项 DeepSeek 均 online；该只读目录不证明 Hy3 服务开通、调用许可或生成成功。仅 1 次 metadata GET，生成/POST/账本写入为 0，冻结源码未变；凭据 `tencent-model-metadata-01/receipt.json`。须核对同账户广州启用管理、API Key 访问范围和服务绑定，不能凭错误码断言密钥无效或免费额度耗尽。没有改动有效的 token 参数试错或重复重发旧 operation。

费用 before/after/delta 见 `live08-native-fee-*`、`live09-native-fee-*`、`live10-native-fee-*`。每次新增 CALL 80,000 µCNY 与 TOKEN 199,680 µCNY 分别低于 200,000，累计保留 999,040 低于 10,000,000；SUBJECT/TENANT 镜像不双加，所有旧 UNKNOWN 原样保留。没有确认 usage 或现金账单，也不能据此确认免费。

视频范围已实际核验：`device/build08-live01.mp4` 185.796500 秒，frame175 显示真实失败；`device/build09-live01.mp4` 180.245022 秒，frame172/179 仍在提交前输入界面，**不能证明响应**，09 失败只有截图、日志和原账本；`device/live10-now01.mp4` 117.104311 秒，frame110 显示已提交后的失败与原生地图。08/09 归档为 `device/BUILD08-EVIDENCE-PARTIAL.json` 与 `BUILD09-EVIDENCE-PARTIAL.json`。同 APK09/API10 的真实模型回答、由其驱动的卡片/地图、多轮闭环仍 **FAILED / NOT_COMPLETED**。

API10 阶段跨重启恢复每轮结果集合仍有真实缺口：当时控制器内保存每轮集合；原远端会话仅持久化文字与来源，恢复时只有最后一轮拿到当前结果。旧轮实体不能用最新结果嫁接补齐。API11/APK12 对原个人 CITY 规则路径的实际修复见下节；模型路径与 Now 附件协议仍未写成完成，原任务队列状态保持不变。

API10/同 APK09 又经 Recent 原 GET 恢复最新 ACTIVE Task，实际显示 1 个 Art Gallery 卡片与官方链接；地图查看选中同一 Place/pin/预览，原详情 GET 显示 Schoolhill 地址和官网来源。点击详情资料来源在真机 Chrome 完成官网页加载，返回后卡片与地图仍同一地点。录屏 `device/live10-restored01.mp4` 180.266133 秒，frame172 已人工观察为加载完成的官网页；返回、重新展开及卡片滚动位于录屏结束之后，只以 `live10-conversation-card-return01.png` 等实际截图作证。恢复回答是原站内规则，不是成功模型或联网来源回答；新 GET 流程没有发送供应商生成请求，没有把失败 Task 改成 COMPLETED。完整 20 对切换/多轮/Save/撤权矩阵未在 API10 运行，仍 NOT_RUN。

### 后续独立回归与 GitHub 交付

当前代码与两份报告已正常、非强制推送至 [Birdtie main](https://github.com/luchen-luke/birdtie)，远端实际核验为 `91962eb88aa74ca34b2df295e51f668c80c41b1a`，tree `8f7fcba7c73a3b5b40565cc0c568bdb9cc606912`。这次增量为 20 个 Go/Dart 源码或测试文件及两份报告；全部正式源树 2271 个路径保留，四个原 queue/state 文件 working bytes 与原 checkpoint 完全相同。2240 个暂存文本与新增可达历史凭据扫描零命中，原 31 个应用二进制资产未变；密钥配置、APK/exe、原始录屏、work 与未选灰区材料留在本地。推送不等于完成 42 条或基础 Agent 验收。收据：`work/git-publication-continuation-10-commit-receipt.json`、`work/git-publication-continuation-10-push-result.json`。

首次真实全套 Go 隔离回归采用独占新库 `birdtie_ui_fullgo_20261008_8d0db3f335c9`，原 001–111 迁移与三条 synthetic fixture 实际执行成功；没有连接或写入真机验证库。`go test ./... -p=1 -count=1 -json` 实际 exit 1，Test terminal events **10892 PASS / 10 FAIL / 23 SKIP**，不去重。Postgres 包在默认 600 秒期限耗尽；当时新子例只运行 1.303 秒，不足以认定该例死锁。680 个顶层测试未启动为 **NOT_RUN**，两个已启动却无终止事件为 **NOT_FINISHED_TIMEOUT**。它们不隐藏在上述 10 个 Test 失败中，也不能称全套已执行完。1279 个输入和原 fixtures 前后未变；供应商调用、真机动作、旧库和原队列写入均为 0。收据：`fullgo-disposable-01/result.json`、`failure-summary.json`，完整失败和超时日志保留。

HTTP 旧 062 用例的共同原因已确定：fixture 的最后 user 是私有历史 canary，与当前 Task.query 不一致，被原 `currentEgressQuery` 拒绝；WaitedLock 因拒绝发生在 owner lock 前而未进入锁，Human options 因原拒绝返回空候选。只更新 `model_egress_budget_integration_test.go` 的历史/当前消息 fixture，并在 `model_egress_current_query_test.go` 增加精确 mismatch 拒绝用例。所有旧泄漏、身份、ABA、到期和零发送断言保留，生产权限与迁移没有修改。新独占父库与八个原 owned child DB 经完整迁移后，整组旧 062/Human **33 PASS / 0 FAIL / 0 SKIP**，CurrentQuery **17 PASS / 0 FAIL / 0 SKIP**；专用测试库已清理。冻结凭据 `legacy062-current-query-fixture-01/freeze.json`。这两份测试变化发生在 API10 编译之后，不改变其生产代码，也不把定向通过改称全套通过。

另一个原全套失败是个人 Memory 本人更新 HTTP 503。新独占诊断库中原 guard 31 次通过，保留全部原拒绝条件的诊断 guard 再运行 300 次通过，未观察到拒绝。诊断 guard 只存在该专用库，去除仅有的固定布尔日志/时钟观测后函数字节与原函数完全一致；仓库生产代码、迁移和权限未改。原失败根因 **NOT_REPRODUCED**，修复 **NOT_IMPLEMENTED**，不以这些定向通过消去原 503。凭据 `memory-update-diag-01/freeze.json`。该库与原全部证据保留以便后续定位。

同 API10/APK09 设备证据总索引为 `device/BUILD10-EVIDENCE-PARTIAL.json`，包含实际卡片、同 ID 详情/anchor/pin、官网加载、两段录屏哈希和模型失败。新录屏并未补成成功模型、多轮、20 对切换或性能验收；完整发现闭环仍待腾讯 Hy3 服务绑定核验以及实际成功后的同构建验证。

## 2026-10-08 API11 / APK12：每轮结果恢复与静默读取修复

本次接续原 `BT-FIX-NOW-UI-001` 的真实历史和地图缺口，仍为 **PARTIAL**。前一组 fixture 修复和两份报告已非强制推送为 `60176c3b878b1dfaee06c0dc60df0109b5612d25`；收据 `work/git-publication-continuation-11/publication-result.json`。此次在该检查点上修改 **24 个源码/测试文件与本报告、任务映射**，没有新增队列、数据表或迁移，没有覆盖 74 个待审材料或改写四个原 queue/state 文件。

### 实际改动文件与行为

| 文件（相对于正式仓库） | 实际变化 |
|---|---|
| `apps/api/internal/agentworkspace/model.go`、新增 `reply_membership.go` | 在原消息中记录 `agent-reply-membership-v1`、当前 Task/City、类型化 refs、消息前缀摘要和稳定结果集 ID。仅记录实际原查询取得的活动、地点或组织，最多 30 个同类型唯一 refs。 |
| `apps/api/internal/httpapi/agent_workspace.go`、`agent_result_projection.go`、`now_live_answers.go` | 原个人 CITY 规则结果在原任务事务内写入对应 assistant；原 GET 返回最新至多 30 条 `messageResults`。不创建新任务系统，不把失败的真实模型请求改用规则回答冒充成功。 |
| `apps/api/internal/postgres/agent_tool_search.go`、`agent_result_projection.go`、新增 `agent_reply_results.go` | 先限制当轮保存的 refs，再按原访问规则和来源版本重新读取；新出现的地点不会进入旧回答。原 Task、账号、授权、City、来源及读取期限在同一数据库快照复核，并在 SQL/提交后复核实际经过时间。读取句柄不序列化、不暴露原始权限证明。 |
| 新增 `apps/api/internal/agentworkspace/reply_membership_test.go`、`httpapi/agent_reply_results_integration_test.go`、`postgres/agent_reply_results_integration_test.go` | 真实 SQL/HTTP 的历史 A/B、不嫁接新 C、撤权、邀请、过期、迟到 mutation、空历史、截止时间及不透明读取边界。中文、HTML 转义、U+2028/U+2029 等摘要跨语言黄金值。 |
| 新增 `apps/client/lib/src/workspace/agent_reply_membership.dart`；`remote_agent_task_source.dart`、`agent_workspace_controller.dart`、`agent_result_sheet.dart` | 严格核验会员归属、消息摘要、顺序和同一 Task；分别恢复旧轮卡片。最新一轮继续使用含原 actions/followUps 的主结果，避免误当历史结果而隐藏原动作；没有新造动作授权。 |
| `agent_workspace_controller.dart`、`remote_agent_task_source.dart`、`map_workspace.dart`、`map_canvas.dart` | 对本人已完成个人 CITY Task，仅当前路由前台时通过原 `/v1/me/agent-tasks/{id}` GET 静默读取新授权结果；单次在途，失败停止自动重试。比对原任务字段、会话、筛选和 refs，只替换新实体投影，不重写会话/查询/草稿，不自动 POST、恢复或完成任务。 |
| `apps/client/lib/src/city/map_camera_focus.dart`、`public_city_map.dart`、`native_city_map_io.dart`、`native_city_map_stub.dart` | 静默刷新与旧投影过期时保留视野；显式用户选点、新查询和重开才恢复原相机聚焦决策。旧 A 被过滤或过期后地图保持关闭的空结果，不以最新 B 或城市目录补齐；当前合法 GET 才能恢复同一旧消息的 A。 |
| 新增 `apps/client/test/reply_history_restore_test.dart`、`reply_projection_refresh_test.dart`；`map_canvas_test.dart` | 43 个历史恢复例、40 个静默读取例及相关地图回归。补齐迟到请求、生命周期、旧 A 暂缺、当前 POST 等待期间旧结果过期和合法新结果重新聚焦。旧重复城市按钮断言改成现行唯一顶栏入口，保留原回调、身份和地图状态断言。 |

原读取期限没有延长；旧对象到期仍不可用，GET 返回单独的新授权投影。原无 membership 的消息继续展示其文字和来源，**不回填或嫁接当前结果**。本阶段覆盖原 personal/CITY activity/place/organization 规则路径；**成功模型 finalize 的每轮归属为 NOT_IMPLEMENTED**，不能称所有会话历史已完成。Now 附件仍 **UNCONNECTED**。

### 冻结构建与测试事实

| 实际命令范围 | 结果与证据 |
|---|---|
| 最终独占 PostgreSQL 原历史/投影回归 | **61 PASS / 0 FAIL / 0 SKIP**；原 HTTP 历史/编码后迟到 mutation 回归 **18 PASS / 0 FAIL / 0 SKIP**。新库 `birdtie_reply_history_20261008_8a743a680398` 使用原 001–111，无 seed，完成后核验 DROP。纯边界与摘要 **50 PASS**；三个命令期间 1277 个源码输入不变。`native-reply-history-06/freeze.json`。 |
| API11 相关 Go 范围及 API/维护 CLI 构建 | **757 PASS / 0 FAIL / 196 SKIP**；两个构建 exit 0，1284 个输入与文件清单前后不变。196 项在该命令为 **NOT_RUN**，不与独立 SQL 相加成全套通过。`build11-backend-01.receipt.json`。 |
| 最终 APK12，18 个去重相关 Flutter 文件 | **413 PASS / 0 FAIL / 0 SKIP**；全量 `flutter analyze --no-pub` 为 No issues found；debug APK exit 0。259 个生产输入、270 个测试输入和忽略的地图配置前后未变。`build12-client-01/receipt.json`。新历史 43 例、静默读取 40 例已包含其中，不再次累加。 |
| 静默读取定向检查点 | 200 个相关用例通过，包含新 40 例；analysis 无问题；489 个 Dart 文件冻结。`reply-projection-refresh-01/freeze.json`。这组与 APK12 重叠，不相加。 |

早期 SQL 超时、非法 profile fixture、Dart 语法/异步用例失败、旧城市叶子断言失败、历史恢复及 POST 等待期间过期的真实失败日志均保留。APK10 未安装；APK11 真机曾发现卡片在原读取期限后消失，截图 `history-after-first-result01.png` 保留。其 `history-after01-expiry-failure.mp4` 名称不能替代内容核验：frame170 仍在提交前输入，**该视频不能证明响应或过期失败**。APK12 是修复静默读取后的最终版本。前述全 Go FAILED、Memory 503 的 **NOT_REPRODUCED / FIX_NOT_IMPLEMENTED** 继续有效；此检查点没有重跑全 Go 或全 Flutter，记 **NOT_RUN**。

当前实际安装 APK12 SHA256 `0dc8230d92ba11cabf316011175b93d73b286993f978b4313b4099bf1610353c`；API11 SHA256 `9610b485acfea9b3fca2d4a31426db1408a4687837f3be80a49e148db88e06b0`。手机 Xiaomi 25098PN5AC / Android16 / c641566b，`install -r` 后核验实际 base.apk 相同，没有 clear。仅在原自有 API10 的准确进程、文件、端口及空闲检查点后换为 API11（127.0.0.1:18091）；原路由 revision 601、账户预算和旧 UNKNOWN 占用保留。凭据 `device/build12-install.receipt.json`、`live11-api-01.receipt.json`、`live10-owned-api-stop-01.json`。

### 同一 API11 / APK12 的真实手机结果

这些结果使用用户已批准的隔离库与官网 Place，并由原 contributor 测试账号查询。该账号不属于真实模型调用 allowlist；**回答是原站内规则数量句，不是模型回答或成功联网搜索回答**。

- 基线 APK09/API10 的 Task `6d150325-db37-459e-9848-c769d561548a` 先查 Gallery、后查 Museum，原返回没有每轮会员归属；重启 Recent 后只剩最后一轮 Museum 卡，旧 Gallery 用户消息和数量句没有 Gallery 卡。`device/history-before02.mp4`，99.970189 秒，frame40 已人工核验。前后修复使用不同 APK/API，明确绑定，不宣称同一二进制修改前后。
- 同 APK12/API11，先恢复已用真实 HTTP 建立的两轮 Task `339c4a6d-a21f-4c48-ac13-f4a1b8a23f98`；Museum 与旧 Gallery 各有自己的卡片和官网来源。点旧 Gallery 地图查看显示 Schoolhill 美术馆 pin/预览，原详情 GET 为同 Gallery 和 Schoolhill 地址；来源实际在 Chrome 打开市政府 Art Gallery 页面。`history-build12-after02.mp4` 174.996567 秒，frame40/80/170 分别为 Museum、旧 Gallery、Gallery 详情；加载完成的官网页面只由 `history-build12-old-source01.png` 作证，视频末段没有补成完整官网加载。
- 拖动 Gallery 地图后，`history-build12-pan-before01.png` 与 `pan-after01.png` 相隔超过 30 秒且约 75 秒；原图裁剪 `(0,1000,1220,1800)` 的像素完全一致，保留同 pin、街道和视野。API 日志有约 27 秒周期的 GET 200，与读取实现和操作窗口一致；日志没有 path/TaskID，不能仅据日志证明每个 GET 的具体路由。`history-build12-after03.mp4` 的 frame35/150 已检查；后来的返回地图只有截图，不虚构为都在视频内。
- 12:05:29、12:05:34 在同一手机依次提交 `find place Aberdeen Art Gallery`、`find place Aberdeen Maritime Museum`，两次原 POST 200。新 Task `3ef524d0-e4a7-42c1-9532-60b6cfedbeeb` 为原 person/CITY COMPLETED，四条消息的两个 assistant 分别持久化 refs `e7b7544c-0b39-4de6-a7fa-2cc00aacdc8a` 与 `bea8ee0c-9c09-4951-8f0c-8c5819e91c2d`，独立摘要和稳定结果集 ID。`history-build12-ui-first01.png`、`ui-second01.png` 及 `history-build12-after04.mp4`；frame94/110 显示 Museum，首次 Gallery 响应另有实际截图。容器时长 186.127456 秒，frame182 提取没有图，不能据时长认定视频包含后续重启。
- 12:07、12:10 实际 `am force-stop` / `am start`，不清除应用数据；Recent 原 GET 恢复两轮。较早 Gallery 卡、官网来源、Gallery 的 Schoolhill 地图仍可点击，最后查询保留 Museum。录像 `history-build12-after05.mp4` 177.696133 秒；frame17/24/168 分别核验 Museum 卡、Gallery 地图、仍为 Gallery 地图。连续应用内分享节选 `history-build12-after05-app-only.mp4` 从原视频第 14 秒开始、158 秒，去掉短暂 launcher 画面，原视频完整保留。12:15 的会话/地图切换在视频结束之后，单独截图作证：`ui-map-held03.png` 与 `ui-map-return03.png` 字节完全相同，`ui-conversation-return03.png` 中最新 Museum 仍在。

同实体原引用、卡片、详情、anchor/pin 在原 native HTTP 与 Task 只读快照共同核验。Task 读取只取上述批准测试账号和时间窗的元数据/会员 refs，REPEATABLE READ READ ONLY / ROLLBACK，没有读取私有画像、消息正文或别的账号。证据 `history-api11-http-01/receipt.json`、`history-build12-ui-task-01.json`、`history-build12-ui-evidence-01.json`；后者记录该窗口两次 POST200、51 GET200、1 GET409。早先详情窗口的 1 GET404 同样保留，不能写成所有请求通过，日志无路由所以不猜错误所属能力。

设备总索引 [BUILD12-EVIDENCE-PARTIAL.json](D:/Project/birdtie/work/ui-repair-2026-10-07/device/BUILD12-EVIDENCE-PARTIAL.json)，SHA256 `015570cdc10b81c8e2d3de0d33421a4e86d0a5e8326e25e7036e5f03380fcf32`；绑定上述两个构建、24 个源码字节、真实 Task、前后视频/截图、人工帧检查及局限。原模型账号四个表（Task、Run、reservation、budget account）在本次前后完全相同，UNKNOWN 最坏上界仍 **¥0.999040**；本轮供应商调用 0。`history-before-01.json`、`history-after-02.json`。它们不是确认账单或免费额度证明。

### 仍未完成的依赖

真实模型仍为前述 **401006 / NOT_COMPLETED**，须核验同一腾讯账号广州服务与模型绑定。目录 online 和免费资源截图不能代替生成许可。已只读复核 DeepSeek Flash 官方协议：[模型指南](https://cloud.tencent.com/document/product/1823/132248)、[参数文档](https://cloud.tencent.com/document/product/1823/135872)、[计费](https://cloud.tencent.com/document/product/1823/130055)。官方 wire ID `deepseek/deepseek-flash`、输出上限参数并不提供输入 token 上限；按峰时输入 ¥2/输出 ¥8 每百万 token 和保守上下文上界，其最坏费用超过单次 ¥0.20。当前字节上限不能直接冒充经过供应商认可的 token 计数；没有跳过原 062 价格版本/CAS/绑定、切换模型试费或释放旧 UNKNOWN。Flash 固定内部别名、可信输入 token 上界和对应原生价格注册 **NOT_IMPLEMENTED**。

完整真实 NL→站内+联网→模型有来源回答→可点卡/同结果地图→多轮与 Save，成功模型历史归属、Now 附件、完整组织/聊天/活动/提醒/个人资产真机回归、跨身份/撤权/cluster/性能/读屏/iOS/Web paint 矩阵仍未验收或 **NOT_RUN**。三处 Place 仅供批准隔离验证；整个城市仍 building/unverified；本次没有新增规划文档、复制 Civu 数据或批量改原任务为 DONE。
