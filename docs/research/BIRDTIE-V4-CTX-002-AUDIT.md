# BT-V4-CTX-002 多阶段情境审计

状态：仓库内实现与隔离开发验收完成；2026-10-02。正式环境尚未部署。

`033_context_graph.sql` 已有全局 Person→typed Context 的私密声明表，关系可为 `current/home/past/destination/affiliation/interest`，Context 类型可为 CITY/COUNTRY/INSTITUTION/COMMUNITY/ONLINE。旧 Person ID 与 Agent ID 不受城市约束；`opportunities.go` 只读取本人明确声明的 current/destination 城市。该表没有用户自助 API/UI，也未验证一个人同时拥有当前城市、过去的城市/大学、目的地和在线情境；不能把原始表结构当成已交付功能。

实现：`contextgraph.Declaration` 定义本人声明契约，仅开放已发布 CITY、本人填写的 INSTITUTION/ONLINE 与相容关系；Community/Country 保留底层节点，但不开放用户自助声称。`GET/POST /v1/me/contexts` 和 `DELETE /v1/me/contexts/{contextID}/{relation}` 共用真实会话校验，按 Person ID 限定读写。新当前城市在锁定本人账号的事务内替换旧当前城市；过去城市/学校、目的地、线上兴趣可并存，全部强制私密。中文设置页可查看、添加和移除这些声明，并明确说明学校/社群名称不构成身份认证。既有 Person、Personal Agent、CityContext ID 与旧 API 不变；此次无新迁移。

验收：`contextgraph.TestDeclarationFixtures` 用当前阿伯丁、过去爱丁堡和大学、目的地伦敦、线上兴趣及非法关系样本验证模型。`TestPersonContextLifecycleIntegration` 在一次性 PostgreSQL 用两个独立 Person 的 HTTP 会话走完整添加、当前城市替换、本人读取、异人隔离、删除/重建、匿名拒绝及账号数量不变；`pwsh -NoProfile -File automation/verify_place_context_migration.ps1 -Through 46` 全量 Go API/DB、旧 ID 与已有迁移保护 PASS。Flutter 中文页面测试覆盖保存当前城市、移除和线上兴趣；`flutter analyze`、`flutter test` 118 项、`go vet ./...`、`go build ./...`、Debug APK 构建及 `git diff --check` PASS。Debug/合成数据不构成正式上线或真实学校身份验证。非城市 Agent 查询消费仍由后续任务推进，Social Alpha 和 Closed Pilot 仍 NO。
