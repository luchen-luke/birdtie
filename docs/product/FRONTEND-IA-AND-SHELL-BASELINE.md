# Birdtie V2 Agent-first 前端 IA 与 Shell 基线

日期：2026-09-30
状态：当前前端实现基线；取代 2026-09-29 的 Now / Explore / Network / Inbox 底栏基线。

## 产品原则

身份、主体所有权与 City Context 规则以 [Agent Identity and Ownership Model](../architecture/AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md) 为准：一个真人对应一个 Personal Agent；组织通过 Membership 管理并拥有自己的 Organization Agent；CityContext 是平台管理的公共检索上下文，不是可关注或私聊的 Agent 身份。Sidebar workspace selector 切换 principal context。

组织 workspace 的 Agent 查询上下文由 Session 对应的真人身份与服务端 Membership 共同确定；组织任务历史归属组织 principal，未经 Membership 校验的 workspace 请求返回拒绝。当前组织查询仅调用公开 CityContext 检索，不代表已实现组织内容写入或管理员私有数据访问。

用户不是先寻找功能，而是告诉 Birdtie 自己想做什么。Birdtie 是以现实世界地图为环境、以 Agent 为主要交互方式的本地社交产品。

```text
User Intent → Birdtie Agent → People / Activities / Groups / Places
            → Contextual Map → Real-world Action
```

## 顶层 IA

**Map Workspace + Agent + Sidebar + Inbox** 是同一套移动端和宽屏 IA。首页是全屏地图工作区，叠加左上角 Sidebar、右上角 Inbox、底部 Agent Composer、随任务变化的 Entity Layer 和 Agent Result Sheet。移动端传统 Bottom Navigation 正式废弃。地图是持续存在的上下文。

| 入口 | 职责 |
|---|---|
| Map Workspace | 默认环境；只显示少量有依据且可见的 Birdtie 实体。任务改变可见实体、选中状态及地图视角。普通 Place 不默认铺满地图。 |
| Agent Composer | 输入意图并提交任务。当前每次提交都创建新任务；带上下文的后续提问属于后续能力。默认提示为 `What do you want to do?`。 |
| Agent Result Sheet | compact、half、full / conversation 连续状态；按任务整理结果，无类型 Tab。 |
| Sidebar | New、Home、My Activities、Groups、Saved、Recent Agent Tasks、Profile、Settings。当前 Recent 在恢复时重新获取结果；只保留本会话中单次输入的展示状态，不保存多轮对话或每个任务的地图视角。完整任务上下文恢复仍是后续能力。My Activities 首批是本人主动标记的私人“计划参加”，不代表报名；Saved 管理 Place/Activity/Group 私人书签；Settings 可查看并撤销 Profile 授权、解除屏蔽、退出登录和进入 Profile 编辑。 |
| Inbox | Action Center；按 Needs attention、Messages、Requests、Agent Updates、Updates 分组，无类型 Tab。 |

Agent 工作区状态为 idle、typing、searching、results、conversation。主要过渡为 Map → Result Sheet → Conversation；打开 Sidebar 和 Inbox 不重置任务。`New` 清除当前任务、结果、选中实体和地图结果上下文。

## 旧能力的归属

- Explore 的地图渲染、City/Place 公开读取和来源信息进入 Map Workspace / Entity Layer；Place 只在任务相关且满足公开位置精度时显示。
- Network 的 People、Group、同意与可见性边界进入 Agent Entity 及 Sidebar。配置 API 时，本人确认发布的公开 Intent 人员和 Group 来自 City Graph；未配置 API 时只允许隔离且明确标识的本地演示数据。
- Now 的即时 Activity 和城市内容进入 Agent Context 与地图结果；已审核公开 Activity 优先读取真实 City API。没有公开坐标的 Activity 只进入结果列表，不猜测位置。
- Inbox 保留并升级为 Action Center。真实 API 已接入 Place、Activity 审核结果更新及显式联系人请求、真人消息事件；Agent Updates 仍待后续 producer。My Birdtie 的 Profile、Settings、Saved 与私人草稿管理从 Sidebar 进入。

## 数据与权限边界

地图、结果及 Agent 共用 City Graph 的可见性、block、时间有效性和位置精度规则。Go API 已提供按 City 的规则文本查询：Activity、公开且经本人确认的 Intent 人员、由 Owner 发布的公开 Group、Place。人员不返回精确坐标；本人主动选择大范围区域时才生成近似示意地图锚点，未选择者只在结果列表中（ADR 0015）；Group/Activity 仅在关联 Place 可公开点坐标时上图。查询不使用私有 Profile consent 授权扩大公开发现。登录用户的任务查询与 City 私有保存，恢复时重新按当前权限检索；匿名当前会话任务的 Recent 也重新查询公开内容。真人联系请求与消息仅在用户明确操作后发送；目前没有 AI 匹配。未配置 API 时使用明确标注的本地演示数据，不能把演示人物、参与人数或回复伪装成在线数据。Agent 不替用户联系、发布或报名。

## 本轮实现范围

Flutter Shell 继续使用既有 Go/PostgreSQL、PublicCityController、认证、Moment 草稿和 Mapbox adapter；未增加大型状态管理依赖。规则式 Agent 查询 API、登录用户任务历史、Owner 发布和撤回 Group 已接入。Group 可不附外部链接；Profile 编辑和本人确认后直接发布的公开 Intent 也已接入。满足可见性和有效期条件时，真实 People 可进入 Agent 结果；仅主动公开地图区域的人员显示近似示意锚点（ADR 0015）。结果列表、地图实体由同一次查询驱动；当前只暂存单次输入的展示状态，地图视角不随 Recent 任务持久化。后续独立切片已实现 Place/Activity/Group 的 Owner-only Saved（ADR 0012）和 My Activities 私人计划（ADR 0013），均不扩大目标可见性或表示已报名。Settings 使用既有 Session/Consent/Block API，不另存客户端隐私状态。真人联系请求和消息经双方明确操作后接入 Inbox（ADR 0014），Agent Updates 尚无 producer。带上下文的多轮 Agent、个人精确地图点、真实活动报名/主办、高德 Web 实际加载与原生高德渲染尚未验证或完成。旧产品文档中 Now/Explore/Network 底栏描述属于此前版本，不再约束当前 Shell。
