# AgentResponse / ResultSet 契约验收（BT-AGT-002）

日期：2026-10-01。对照 `docs/architecture/AGENT-DATA-API-CONTRACTS.md`。

## 契约和客户端

- Agent 响应现有中文 `message` 和实体数组之外，增加 `requestId`、可恢复任务的 `conversationId` / `taskId`、`resultSet`、`actions`、`followUps` 和 `mapEffects`。
- `resultSet.entities` 的类型和 ID 直接来自本次返回的真实实体；只有公开精确 WGS84 点位进入 `mapEffects.pinEntityIds`。地图相机效果是 `preserve`，不会擅自重置视角。
- 登录任务的 ResultSet ID 由任务 ID 与最后更新时间组成，重新读取任务时保持相同；匿名响应使用请求 ID。ResultSet 是可重新查询的当前视图，不是不可变历史快照。
- 已授权组织所有者或管理员的创建活动请求可以获得 `OPEN_ORGANIZATION_CONSOLE` 操作；客户端点击后重新检查当前成员权限，再打开真实组织活动管理页。没有权限时不给操作。
- Flutter 解析新的契约字段并展示可执行操作，同时沿用现有实体数组完成当前页面的地图和结果卡投影。

## 验证

- Go 契约单测校验实体引用、地图标记、稳定 ID、请求 ID、内部筛选字段不外露，以及成功空结果和不支持请求的不同状态。`go test ./...`、`go vet ./...`、`go build ./...` 通过。
- Flutter `flutter analyze` 和 40 项测试通过；包含组织结果与创建操作按钮的 widget / 数据解析测试。
- 本地 API 实测：组织 1 个只生成组织引用且无地图 Pin；体育馆 2 个地点生成 2 个地点引用和 Pin；周末羽毛球 2 个活动生成 2 个活动引用和 Pin；图书馆无匹配返回 HTTP 200、`status: empty`、空引用；未知请求返回 `status: unsupported`。所有响应均有 `requestId` 和 ResultSet ID。

## 边界

- 当前 API 仍保留顶层实体数组供旧客户端读取，新的 ResultSet ID 不等于独立持久化快照。实体撤销公开权限后，恢复请求会重新检查可见性。
- Android 原生地图瓦片依然遇到 Mapbox 403，真实 Pin 的视觉验收仍取决于地图服务权限；此契约测试只证明 ID 与经纬度投影的一致性。
