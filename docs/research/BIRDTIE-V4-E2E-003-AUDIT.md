# BT-V4-E2E-003 多主体授权矩阵审计

状态：隔离开发 E2E 授权矩阵完成；2026-10-02。正式主体授权仍须试点核验。

现有独立集成：`activity_owner_integration_test.go` 验证 Organization owner/admin、普通成员、外人、主体停用对活动创建/编辑/发布/取消的影响；`social_activity_integration_test.go` 验证 Community owner/admin 与普通成员及 Person 主办；`business_organizer_integration_test.go` 通过 HTTP 验证未核验/已核验 Business、owner/admin/普通成员/外人、经营场地撤销和公开投影。Community 管理本身另有 `community_social_integration_test.go` 权限测试。这些均为合成隔离库测试，不是正式经营/组织授权证据。

实施与证据：新增 `actor_authorization_matrix_integration_test.go`，在同一隔离库创建三个独立主体及四个真人账号：每人在一个主体为 Owner、在另两个主体为普通 Member，另有外人。对 Organization/Community/已核验 Business 各自检查 Owner 创建和发布；其余三人创建、编辑、发布均被拒绝，未发布草稿也不出现在其管理列表。发布后以外人读取，公开详情的主办类型和 ID 必须只指向活动自己的主体；Owner 账号停用后列表与取消权限立即失效。既有三个主体独立的 HTTP/存储测试继续覆盖 Admin、普通成员、外人、未核验 Business、经营场地撤销与活动权限，通用会话层继续负责匿名/失效会话。矩阵未使用真实主体或正式会话。

首轮测试因测试代码把 `GetActivity(id,viewerID)` 参数顺序写反而失败；修正测试后未发现服务端跨主体授权缺陷。最终 `pwsh -NoProfile -File automation/verify_place_context_migration.ps1 -Through 46` PASS：一次性 PostgreSQL 001–046、全量 Go API/DB 测试、旧 Place/Activity/Intent/Moment ID 保持及 039–046 回退保护。`go vet ./...`、`go build ./...` PASS。本任务没有 Flutter 实现改动；上项 Flutter analyze、117 测试与 Debug APK 已通过。本结论只证明仓库内合成数据授权矩阵，不把正式 IdP、真实 Organization/Business/Community 授权或 Closed Pilot 记为完成。
