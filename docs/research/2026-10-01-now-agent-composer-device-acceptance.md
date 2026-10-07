# Now Agent 输入与结果联动验收（BT-AGT-003）

日期：2026-10-01。设备：已连接 Android 真机 `c641566b`，Birdtie debug 包 `app.civu.civu_mobile.birdtiepreview`，本地 API 通过 `adb reverse tcp:3694 tcp:3694`。

## 完成内容

- Now 输入框直接调用真实 Agent API；发送后立即记录用户输入，服务返回后先展示中文回答，再展示实体结果。请求失败只由结果 Sheet 展示可重试错误，并保留上次结果。
- 地点结果也投影为与 ResultSet 相同 ID 的地图实体；公开 WGS84 点才可有 Pin。地图相机保持原状态。
- `+` 快捷操作中的找组织、找地点、创建活动已对应当前真实能力；未开放的普通区域问答仍明确标注。输入框默认文案保持中文。
- 开始 Agent 任务后收起首屏附近活动概览；展开对话时实体预览卡不覆盖对话。
- 匿名追问合并原始请求时，“按市中心距离排序”能保留周末羽毛球类别和时间，并明确告知使用城市中心而非设备定位。

## 验证

- Flutter `flutter analyze` 无问题，`flutter test` 全部 41 项通过。包含地点结果/Pin/ResultSet ID 一致、快捷菜单、20 次输入焦点切换后 MapCanvas 不重建、概览与对话互斥的 widget 测试。
- Go `go test ./...`、`go vet ./...`、`go build ./...` 通过。
- Flutter debug APK 编译成功并通过 ADB 覆盖安装；真机顶层活动为 Birdtie，Flutter VM Service/DevTools 可连接。
- 真机输入 `Find badminton this weekend`：中文答复“找到 2 个符合条件的公开活动。”先于两条真实本地测试活动结果显示。点击“按市中心距离排序”后，答复改为“继续查找周末的羽毛球活动：按与城市中心的距离排序，找到 2 个（未使用设备定位）。”
- 真机展开对话可见首轮用户输入在 Agent 答复之前，且首屏概览已收起。截图：[首轮结果](evidence/2026-10-01-agent-device-result-unoccluded.png)、[追问结果](evidence/2026-10-01-agent-device-follow-up-fixed.png)、[无遮挡对话](evidence/2026-10-01-agent-device-conversation-unoccluded.png)。

## 限制

- 当前 Mapbox 瓦片请求返回 403，地图使用当前城市真实数据降级列表；真实地图 Pin 的视觉表现仍待地图服务权限恢复后验收。测试供给均标为本地测试。
- 首轮真机输入为英文以使用 ADB 键盘；中文意图及“近一点的呢？”已由 Go 语料测试和本地 API 请求验证，真机中文追问使用了界面提供的“按市中心距离排序”建议。
