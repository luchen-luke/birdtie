# 组织 Agent 公开知识问答验收（BT-OAG-001）

日期：2026-10-01。环境：本地 PostgreSQL、Go API、Android 真机 `c641566b`，开发预览包。

- 数据库迁移 `025` 增加组织 FAQ，区分草稿和已发布。组织 owner/admin 可在中文管理界面新增、编辑、发布、撤下、删除；非管理员写入返回 403。
- 公开组织页只在启用的 Organization Agent 且组织身份已认证时显示提问入口。回答仅引用已发布 FAQ、公开简介、组织提供的 HTTPS 链接或未来公开活动，每个已知回答带来源。未认证、未发布问答、未支持的政策问题以及没有依据的活动问题均明确返回 `unknown`，不补造规则。
- `automation/verify_organization_faq.ps1` 覆盖草稿、权限、认证状态、FAQ/简介/链接/活动来源、未知问题、编辑与删除，全部通过。测试时暂时设置的合成组织认证状态及 FAQ 已恢复和删除。
- 真机从活动详情进入组织公开页，输入 `website`，看到组织提供的链接和“依据：组织提供的链接”。截图：[真机带来源回答](evidence/2026-10-01-organization-agent-device-answer.png)。
- `go test ./...`、`go vet ./...`、`go build ./...`、`flutter analyze`、`flutter test`（51 项）和 Android debug APK 构建通过；新版 APK 已安装在真机。

“已认证”测试使用的是本地合成组织的临时存储状态。当前仓库仍没有组织身份核验工作流；组织自己维护的 FAQ、简介和链接也没有独立事实核查。规则式问答不等同于开放式对话或实时政策判断。
