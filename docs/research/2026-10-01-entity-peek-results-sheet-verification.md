# BT-NOW-003 — 单点预览与 Agent 结果面板验收

日期：2026-10-01。设备：Android `c641566b`，安装包 `app.civu.civu_mobile.birdtiepreview`。

## 结果

- 点击地图聚合成员后，仅显示轻量 `EntityPeekCard`；不创建 Agent 任务。长活动标题在两行内截断，`查看`操作保持可点击。[真机截图](evidence/2026-10-01-now003-entity.png)
- 提交 `badminton this weekend` 后显示独立的 `AgentResultsSheet`，先显示 Agent 回复，再显示结果。成功返回时收起旧 Pin 预览，避免两层卡片占据地图。[真机截图](evidence/2026-10-01-now003-results-clean.png)
- 面板 `peek → medium → expanded` 和反向切换均按一级进行。缓慢拖动根据位移判断方向；上滑真机进入对话，标题仍为中文结构化意图。[真机截图](evidence/2026-10-01-now003-expanded.png)
- 等待新请求时继续保留原有结果与选中 Pin；成功后统一切换到新结果并清除旧选择，失败则保留旧内容。

## 验证

- `flutter analyze`：无问题。
- `flutter test`：66 项通过，含档位、长标题、预览与新查询状态测试。
- Android debug APK 构建及 ADB 覆盖安装：通过；API 通过 `adb reverse tcp:3694 tcp:3694` 连接本机服务。

当前地图活动均标注“本地测试”，截图只证明预览、面板和数据投影的交互；不代表真实 CSSA 活动供给或封闭试点发布条件已经满足。
