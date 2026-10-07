# BT-V4-FOL-001 非对称 Follow 审计

状态：仓库内实现与隔离验证完成；2026-10-02。正式部署与真实用户验收尚未进行。

实施前没有 Follow 表、API 或 UI。已有 Person Tie/Friend 是双方确认的关系；未将其重命名、回填或自动转换为 Follow。Community/Organization/Business 是独立主体，Follow 不自动加入成员，也不获得管理权限或私密资料。Person Block 会撤销双方 Follow，阻止重新关注。

迁移 047 新增独立的非对称关系表、四类目标互斥约束、唯一索引、目标公开状态和 Block 写入保护。本人 Person 会话可通过 `GET/POST /v1/me/follows`、`DELETE /v1/me/follows/{targetType}/{targetID}` 管理自己的关注；可关注公开活跃 Person、活跃 Organization、公开已发布 Community、已核验活跃 Business。列表只返回当前仍可公开读取的目标。Person、Organization、Community 和已核验 Business 详情页接入中文关注按钮；换账号时旧账号状态立即隐藏并重新读取。

机会引擎仅在已有可读活动候选中，将**公开**且由已关注主体主办的活动列为 `FOLLOWED_PUBLIC`，返回 `FOLLOWED_ORGANIZER` 中文理由。非公开候选不会获得该排序或理由；Follow 本身不授予详情读取、成员、好友或活动权限。

验证：`pwsh -NoProfile -File automation/verify_place_context_migration.ps1 -Through 47` PASS，包含一次性 PostgreSQL 001–047、全量 Go API/DB 集成、匿名/异人/目标状态/Block/无 Tie 授权检查、旧 ID 保持、047 有数据回退保护和空表 down/reapply，以及 039–046 回归；`go vet ./...`、`go build ./...` PASS；`flutter analyze` 无问题、`flutter test` 121 PASS（关注切换和账号切换）、Debug APK 构建 PASS。所有主体均为合成开发数据，Debug 包没有构成真机正式验收。未部署 047，不能声称正式关注可用或 Closed Pilot Ready。
