# 收件箱验收（BT-INB-001）

日期：2026-10-01。环境：本地 PostgreSQL、Go API、Android 真机 `c641566b`，开发预览包。

- 收件箱直接读取账户的服务端项目；点击活动通知或人类消息通知后写入已读状态，并打开对应的活动详情或真实对话。联系申请区域仍展示服务端申请与对话。空收件箱可下拉刷新；读取失败显示重试按钮。未配置 API 时明确提示暂不可用，已移除 Anna/Kevin 等本地假消息。
- 迁移 `024` 为消息通知记录目标对话 ID，并回填已有消息通知。`automation/verify_inbox_conversation.ps1 -KeepTestData` 用本地合成双人会话验收：一条中文“收到新消息”通知、未读状态、正确的 `targetConversationId`、服务端可读取对话与消息、标记已读持久化、发件人不收到自己的消息通知，逐项 PASS。
- 真机安装新版调试 APK 后，测试账户在收件箱看到消息，点击后进入真实对话并看到“本地收件箱跳转验收消息”。截图：[收件箱](evidence/2026-10-01-inbox-device-list.png)、[对话](evidence/2026-10-01-inbox-device-conversation.png)。验收后合成会话、消息和通知已从本地库清理。
- `flutter analyze` 无问题，`flutter test` 48 项通过；新增组件测试覆盖空、错误、重试、消息已读和跳转回调。`go test ./...`、`go vet ./...`、`go build ./...` 与 Android debug APK 构建通过。

消息通知仅来自真实用户会话；本轮未增加陌生人自动私信或模拟聊天。地图瓦片 403 仍归地图任务处理。
