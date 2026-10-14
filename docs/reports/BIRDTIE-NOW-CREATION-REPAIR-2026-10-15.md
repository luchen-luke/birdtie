# Now 首页创建与媒体修复：2026-10-15

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
