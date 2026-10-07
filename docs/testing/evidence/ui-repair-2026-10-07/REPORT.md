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

腾讯云 TokenHub 模型 key 已通过 models 列表检查，尚未取得实际模型生成与检索证据。用户已授权本次开发验证累计 ¥10、单次 ¥0.20；联网工具截图显示剩余 49,979/50,000 次免费调用，但后付费仍启用。费用事实、免费资源消耗及实际搜索次数须分别记录；不能把 usage 当账单或默认费用为零。

用户新提供的 birdtie-dev WSA 专用 key 已保存到被 Git 忽略的 `apps/api/.env.wsa.local.json`，未写入源码、日志或客户端。本次累计/单次预算不变，须由原 062 账本共同约束 WSA 与模型。TokenHub 免费搜索次数不能推定属于 WSA 免费资源。

AIR-009 已在 UI 自然检查点后串行接续。`tencent_tokenhub.go`/测试修复默认 HTTP/2/请求体重放与连接复用的隐藏重发风险，固定 HY3 `thinking.type=disabled`，完善空搜索与缓存 token 合同；modelgateway **379 测试及子测试 PASS，0 FAIL/0 SKIP**，凭证 `tencent-wire-fix-01.result.json`。此后端增量发生在 API05 构建之后，未声称已包含在 APK05/API05 中，也未声称真实派发可用。

## 尚未验收

- 真实模型 + 站内/联网检索 + 来源链接 + 可点实体 + 同结果地图 + 多轮完整闭环：**PARTIAL / 首次真实尝试 FAILED**，模型、来源回答与同构建多轮验收仍 NOT_RUN。
- 开发地点 UI/地图连续流程和特定缺陷前后录屏：已取得历史证据；真实官网 Place 已通过原流程发布到批准的隔离库。真实检索回答与来源点击闭环仍 NOT_RUN；dev-seed 链接不能证明真实 HTTP(S) 来源打开。
- 本增量全部 Flutter 套件、Go 全套、真实 IdP/组织深链撤权、无障碍完整矩阵、性能 profile、iOS：**NOT_RUN**。
- Now 附件、就地登录待办恢复、历史搜索、完整 Business 导航以及 LIFE/导入候选的缺口按映射表保留。
- 未部署生产、未推送 GitHub；没有用测试活动、固定长文或 API key 可用代替基础 Agent 验收。

## 2026-10-08 真实链接续检查点

本节更新上述历史运行边界。基础 Agent 复用原 058 配置、062 预算、Task 与 Run；真实调用只有明确允许的本地验证账号生效。新 107–110 迁移只应用于新隔离库 `birdtie_ui_live_20261008_b160550993`，没有更改 APK05 使用的旧库或生产数据。Now 保留原始问题，同时从当前 CITY Task 编译城市和公开筛选条件；不把历史、个人画像、私有来源、地图坐标或结果 ID 发给供应商。

用户已独立批准 Art Gallery、Maritime Museum、Provost Skene’s House 三条官网事实候选，仅限该隔离库。通过原 contributor/reviewer 提交审核流程发布，三个真实 Place 的详情和公开地图 GET 均 HTTP 200，entityRef/detailRef/anchor 使用相同 ID。城市完整状态仍为 building，不能称整个 Aberdeen 供给已维护完毕。具体审核、来源与结果在 `work/ui-repair-2026-10-07/live06-city-candidates-02.freeze.json`；没有导入活动或 Civu 数据。

API06/APK06 已真实安装运行。2026-10-08 05:53:59 真机输入 `find place Aberdeen Art Gallery`，原 Now POST 返回 500。原 Task 保持 ACTIVE，Run 为 STOPPED；搜索 CALL 保留 UNKNOWN 的 80,000 µCNY（¥0.08）最坏上界占用，模型未预留或调用。没有重发旧 operation、退款 UNKNOWN 或重置账户。旧日志不能确定具体失败阶段，记为 **FAILED / CAUSE_NOT_DETERMINABLE**；新后端已补固定阶段诊断并保留错误身份，屏蔽查询、URL、密钥及供应商任意错误文本。提交前录屏与失败截图保留；核验末帧显示录屏结束在提交前，不把该视频当成响应或完成证据。

现已补失败后 Recent 的一次只读刷新，保持原错误、地图和会话；异步结果受账号、组织、城市、线上范围、生命周期与轮次守卫约束，不自动重试 POST、恢复或完成任务。APK07 定向 **189 PASS / 0 FAIL / 0 SKIP**，全量 analyze 无问题，debug APK 构建成功；257 个生产输入和 267 个测试输入的完整快照前后一致。APK SHA256 `f9843cb2b07a1c127c0cd1265e274816fd81dcf8436c6df9088d04052a2a6626`，已 `install -r` 到同一真机并核验实际 base.apk hash，没有 clear。源码和命令收据为 `work/ui-repair-2026-10-07/build07-client-01/freeze.json` 与 `device/build07-install.receipt.json`。

110 的真实 SQL 回归与原预算/授权/请求体守卫合并运行：**189 PASS / 0 FAIL / 0 SKIP**，源码前后未变，供应商调用 0，凭据 `live-source-native-db-08.result.json`。包含当前 Task 两轮、CITY 撤权及恢复、真实 HTTP/1 请求体发送前撤权和新旧范围混配拒绝。较早 db-07 为 19 PASS，但独立 fixture 同时编辑导致 sourceUnchanged=false，已保留并由 db-08 重跑，不把 db-07 称为冻结证明。

上述后端测试使用真实 SQL 和伪供应商 HTTP，仍不证明供应商真实回答。APK07 同构建的联网来源、真实模型、可点卡片、地图与多轮录屏：**NOT_RUN**，待后续实际验证更新；全清单保持 PARTIAL。费用限制已澄清为每次供应商 API 请求 ≤¥0.20、原账户累计 ≤¥10；UNKNOWN 上界占用不等于账单或确认免费。
