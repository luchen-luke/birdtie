# Birdtie V2 Agent-first 前端 IA 与 Shell 基线

日期：2026-09-30
状态：当前前端实现基线；取代 2026-09-29 的 Now / Explore / Network / Inbox 底栏基线。

## 产品原则

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
| Agent Composer | 输入意图、提交任务、后续提问。默认提示为 `What do you want to do?`。 |
| Agent Result Sheet | compact、half、full / conversation 连续状态；按任务整理结果，无类型 Tab。 |
| Sidebar | New、Home、My Activities、Groups、Saved、Recent Agent Tasks、Profile、Settings。Recent 恢复任务上下文、对话、结果和视角。 |
| Inbox | Action Center；按 Needs attention、Messages、Requests、Agent Updates、Updates 分组，无类型 Tab。 |

Agent 工作区状态为 idle、typing、searching、results、conversation。主要过渡为 Map → Result Sheet → Conversation；打开 Sidebar 和 Inbox 不重置任务。`New` 清除当前任务、结果、选中实体和地图结果上下文。

## 旧能力的归属

- Explore 的地图渲染、City/Place 公开读取和来源信息进入 Map Workspace / Entity Layer；Place 只在任务相关且满足公开位置精度时显示。
- Network 的 People、Group、同意与可见性边界进入 Agent Entity 及 Sidebar。配置 API 时，公开 Intent 人员和已审核公开 Group 来自 City Graph；未配置 API 时只允许隔离且明确标识的本地演示数据。
- Now 的即时 Activity 和城市内容进入 Agent Context 与地图结果；已审核公开 Activity 优先读取真实 City API。没有公开坐标的 Activity 只进入结果列表，不猜测位置。
- Inbox 保留并升级为 Action Center。真实 API 已接入 Place、Activity、Group 审核结果更新；Messages、Requests、Agent Updates 仍待后续 producer。My Birdtie 的 Profile、Settings、Saved 与私人草稿管理从 Sidebar 进入。

## 数据与权限边界

地图、结果及 Agent 共用 City Graph 的可见性、block、时间有效性和位置精度规则。Go API 已提供按 City 的规则文本查询：Activity、公开且经本人确认的 Intent 人员、已审核公开 Group、Place。人员只有粗略区域，不返回精确坐标；Group/Activity 仅在关联 Place 可公开点坐标时上图。查询不使用私有 Profile consent 授权扩大公开发现。登录用户的任务查询与 City 私有保存，恢复时重新按当前权限检索。当前没有消息接口或 AI 匹配；未配置 API 时使用明确标注的本地演示数据，不能把演示人物、参与人数或回复伪装成在线数据。Agent 不替用户联系、发布或报名。

## 本轮实现范围

Flutter Shell 继续使用既有 Go/PostgreSQL、PublicCityController、认证、Moment 草稿和 Mapbox adapter；未增加大型状态管理依赖。规则式 Agent 查询 API、登录用户任务历史、Group 独立审核与 Owner Inbox 更新已接入。Profile 编辑和本人确认的 Intent 提交也已接入；另一位 City reviewer 审核通过后，真实 People 可进入 Agent 结果。结果列表、地图实体由同一次查询驱动；本机暂存对话和视角。下一阶段完善联系请求、任务上下文持久化和安全限流。旧产品文档中 Now/Explore/Network 底栏描述属于此前版本，不再约束当前 Shell。
