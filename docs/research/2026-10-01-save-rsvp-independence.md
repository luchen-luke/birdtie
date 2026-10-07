# 收藏与报名独立验收（BT-SAV-001）

日期：2026-10-01。环境：本地测试 API 与 Android 真机。

- 端到端脚本以学生账户报名新发布活动后，`GET /v1/me/saved` 中没有该活动。随后 `POST /v1/me/saved` 创建收藏，重新读取仍存在；`DELETE` 取消收藏后重新读取不存在。整个过程中 `GET /v1/activities/{id}/participations/me` 始终为 `going`。脚本输出 `[PASS] Save, unsave and RSVP independence`，并自动取消这次合成活动。
- 真机对保留的闭环测试活动先看到“你已报名”与“收藏”；点收藏后变为“已收藏”，再点变回“收藏”，报名仍显示“你已报名”和“取消报名”。“我的活动”将报名归于“即将参加”，收藏是独立分区。
- 后端保存和列表增加 `visibility='public'` 条件，与公开详情的可见性一致；非公开活动 ID 不会通过收藏泄露标题。
- `go test ./...`、`go vet ./...`、`go build ./...` 通过；`flutter analyze` 无问题，`flutter test` 46 项通过。

这项验收只使用本地虚构活动；Mapbox 瓦片 403 的问题仍由地图任务处理。
