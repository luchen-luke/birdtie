# ADR 0007: Agent Workspace 查询与任务边界

身份与 workspace principal 定义现由 [Accepted Agent Identity and Ownership Model](../architecture/AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md) 统辖。本 ADR 的 City Graph 查询是 CityContext 能力，不是 City Agent 身份。

日期：2026-09-30
状态：已采用，首个规则式实现

## 决定

Birdtie V2 的意图输入先连接 Birdtie 自有的 City Graph 查询接口。服务端使用明确的词项过滤，并在同一城市内返回公开、有效的 Activity、Intent 人员、Community Group 与 Place。响应标记 `mode: rules`，不宣称为 AI 语义理解或撮合。

人员发现要求 Intent 由本人确认、处于 active/public 且未过期，同时 Profile 明确公开。人员只返回显示名、意图主题和粗略区域，不返回坐标。Group 由 Owner 确认并公开发布，发布路径见 ADR 0011。Activity 和 Group 只通过已公开、未过期且点精度的 Place 获得地图坐标。Place 的 area/city/none 精度不会在 API 中泄露底层经纬度。

登录用户的查询执行双向 Account block 过滤。私有 Profile consent 不改变公开发现范围；未来经同意的访问应走独立的授权操作。匿名查询只读；登录用户的任务记录归属当前 Personal 或 Organization principal，并记录启动该任务的人员、City Context、查询、意图、筛选条件、状态和对话。恢复时重新查询当前可见内容，避免保存过期或已撤销授权的结果快照。任务按 workspace principal 隔离，其他 principal 的任务以 404 响应。退出登录时客户端清空当前内存上下文。

## 当前限制

词项匹配只能覆盖字面文本，不提供同义词、语义匹配或推荐。确定性 Agent MVP 目前只解析“Find badminton this weekend”和同一任务中的“Anything closer?”，对其它意图明确说明当前能力边界。City 周末筛选按 City 时区计算；“更近”只按配置的 City 展示中心排序，不请求设备位置。公开查询的限流、滥用处置与 OIDC 端到端验证是对外开放前的门槛。Inbox 已接入 Place/Activity 审核结果更新；Agent 更新尚未接入。

## 实现更新（2026-09-30）

迁移 `019` 建立 Personal/Organization Agent、Organization principal、membership 和平台管理的 CityContext，不创建 City Agent；新个人注册和组织创建分别原子创建对应 Agent。迁移 `020` 为任务增加 principal、acting user、CityContext、intent、filters、conversation 和 `ACTIVE/COMPLETED/FAILED` 生命周期，并在数据库层校验组织任务的发起者是有效成员。

客户端使用同一任务 ID 续聊；Recent 从服务端恢复任务上下文并基于当前公开数据重查，New 清空当前任务且保留 Recent。API 当前为首个确定性 Activity 意图返回真实已发布记录。仅本地 Compose 数据库可手动导入 `apps/api/dev-seeds/001_badminton.sql` 合成羽毛球内容；正式迁移不包含该数据。

更新（2026-09-30）：ADR 0014 已加入显式真人联系请求与消息 producer，ADR 0015 已加入本人自愿公开的粗略地图区域标记；本 ADR 的规则查询和无真实 AI 对话边界仍有效。
