# ADR 0007: Agent Workspace 查询与任务边界

日期：2026-09-30
状态：已采用，首个规则式实现

## 决定

Birdtie V2 的意图输入先连接 Birdtie 自有的 City Graph 查询接口。服务端使用明确的词项过滤，并在同一城市内返回公开、有效的 Activity、Intent 人员、Community Group 与 Place。响应标记 `mode: rules`，不宣称为 AI 语义理解或撮合。

人员发现要求 Intent 由本人确认、处于 active/public 且未过期，同时 Profile 明确公开。人员只返回显示名、意图主题和粗略区域，不返回坐标。Group 要求本人确认、独立城市审核并公开发布；初始为 private/draft，后续提交与审核路径见 ADR 0008。Activity 和 Group 只通过已公开、未过期且点精度的 Place 获得地图坐标。Place 的 area/city/none 精度不会在 API 中泄露底层经纬度。

登录用户的查询执行双向 Account block 过滤。私有 Profile consent 不改变公开发现范围；未来经同意的访问应走独立的授权操作。匿名查询只读；登录用户的任务记录包含所属 Account、City、查询文本和时间。恢复时重新查询当前可见内容，避免保存过期或已撤销授权的结果快照。任务由 Session 确定所有者，其他 Account 的任务以 404 响应。退出登录时客户端清空内存任务上下文。

## 当前限制

词项匹配只能覆盖字面文本，不提供同义词、排序解释、语义匹配或推荐。数据库无公开 Intent、Group、Place 或 Activity 种子，因此当前真实查询可以返回空结果。任务对话、地图视角、后续提问和任务完成状态尚未持久化。公开查询的限流、实际内容审核运营与 OIDC 端到端验证是对外开放前的门槛。Inbox 已接入审核结果更新；消息、请求与 Agent 更新尚未接入。
