# BT-V4-SAF-003 举报、屏蔽与支持路径审计

状态：仓库内实现与隔离开发验收完成；2026-10-02。正式试点值守仍未验收。

现状：`incident_reports` 与本人收据 API、24 小时 5 次限制和普通帮助入口已有；报告对象仅 `activity/organization/account/general`。活动详情、公开组织页和好友/账号页已有中文举报入口；聊天消息、Community 和独立 Business 无举报类型与详情入口。Person 双向 Block 会阻止好友联系、私信、公开资料、Intent 和部分活动/机会读取；举报仍允许在 Block 后提交。不能把举报提交视为运营处理完成，当前没有正式值守证据。

实施：增量迁移扩展报告对象至 `message/community/business`，保持旧报告 ID 与原类型；消息只允许会话参与者报，社群/商家须存在且按用户可见性判断，不从 404 泄露私密目标。添加对应中文入口与对象标签，并补匿名/异人/成员/屏蔽后报告、速率限制和收据的 API/数据库测试。梳理 Block 对上述详情、会话与 Agent 结果的一致性；若需要独立 Business/Community 屏蔽关系，先定义主体级别与对旧 Person Block 的兼容，不能从成员账号推断整个组织被屏蔽。用隔离库迁移 up/down/reapply、Flutter/Go 检查和真机开发预览验收。

完成证据：`046_social_report_targets.sql` 扩展对象类型，回退脚本在存在新报告时拒绝删去约束；`CreateReport` 在数据库写入前核验活跃 Person 报告者和对象可见性。Message 仅非发送者且为会话参与者可举报；Community 仅公开已发布或活跃成员可举报；Business 仅活跃且已核验经营主体可举报。24 小时 5 次限流、本人收据和原 Activity/Organization/Account 路径继续共用。聊天消息、社群详情、经核验商家所在地点页均有中文举报入口。

Person Block 的范围仍是两个自然人之间的连接、聊天、资料与受众访问；既有 `person_ties_integration_test.go`、`friend_chat_integration_test.go`、`social_privacy_integration_test.go` 和 `social_intent_audience_integration_test.go` 覆盖上述边界及 Block 后仍能举报。Block 不自动屏蔽其所属 Community 或 Business，避免把实体与成员混同。此次新增集成覆盖异人/本人消息拒绝、参与者在 Block 后举报、公开/隐藏社群、成员可举报隐藏社群、未核验/已核验商家、限流和本人 5 条收据。匿名请求继续由原报告 HTTP 鉴权中间件拒绝。

验证：`pwsh -NoProfile -File automation/verify_place_context_migration.ps1 -Through 46` PASS，隔离 PostgreSQL 应用 001–046、全量 Go API/DB 测试、旧 Place/Activity/Intent/Moment ID 保持、046 有数据下保护与空表 down/reapply、039–045 回归。`go vet ./...`、`go build ./...`、`flutter analyze`、`flutter test`（117 PASS）、隔离 Gradle home 的 `flutter build apk --debug --dart-define-from-file=.env.maps.mobile.local.json`、`git diff --check` 均 PASS。测试对象为合成开发资料；未部署正式 API、无真实举报或值守响应证据，Closed Pilot NO。
