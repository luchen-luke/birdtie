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
- Network 的 People、Group、同意与可见性边界进入 Agent Entity 及 Sidebar。人员和群组后端未就绪时，只允许隔离且明确标识的本地演示数据。
- Now 的即时 Activity 和城市内容进入 Agent Context 与地图结果；已审核公开 Activity 优先读取真实 City API。没有公开坐标的 Activity 只进入结果列表，不猜测位置。
- Inbox 保留并升级为 Action Center。My Birdtie 的 Profile、Settings、Saved 与私人草稿管理从 Sidebar 进入。

## 数据与权限边界

地图、结果及 Agent 以后必须共用 City Graph 的可见性、block、consent、时间有效性和位置精度规则。当前 Go API 没有 Agent 查询、People / Group 地图读模型或消息接口；第一阶段使用明确标注的本地任务演示与交互状态，不能把演示人物、参与人数或回复伪装成在线数据。真实 API 的 City、Place、Activity 结果应保留来源状态；Agent 不替用户联系、发布或报名。

## 本轮实现范围

本轮改造 Flutter Shell，继续使用既有 Go/PostgreSQL、PublicCityController、认证、Moment 草稿和 Mapbox adapter；不增加大型状态管理依赖。后续以 Agent 查询 API 替换本地任务数据源，并增加授权后的 People / Group 与真实 Inbox 读模型。旧产品文档中 Now/Explore/Network 底栏描述属于此前版本，不再约束当前 Shell。
