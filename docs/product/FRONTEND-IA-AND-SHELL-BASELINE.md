# Birdtie 前端信息架构与应用 Shell 基线

日期：2026-09-29  
状态：前端实现基线；以 Birdtie 产品 IA 为权威来源，并完成 2026-09-25 之后参考前端结构的只读审阅。  
产品依据：[`01-Birdtie-产品文档-V3.0.md`](01-Birdtie-产品文档-V3.0.md) 第 4–5 节；结构参考见下文。  

## 1. 决策

- 正式前端按 Birdtie 的产品信息架构组织，不沿用当前 `apps/client/` 早期预览的导航、页面树、状态或本地样例内容。
- 早期预览已由新 shell 替换；不恢复其导航、页面树、演示数据或本地状态。新 shell 使用 Flutter，以 Birdtie 导航和领域状态实现。
- 手机主导航提供四个稳定入口：**Now、Explore、Network、Inbox**。**My Birdtie** 通过常驻个人头像/账户入口进入，不挤占四个核心任务入口。
- **Personal Agent** 是 My Birdtie 中的私人工作区入口，也可由全局自然语言输入/具体任务上下文进入。它不是独立公共社交页。
- **Organization / City workspace** 是有相应组织或维护权限时可进入的工作区，可由账户/工作区切换入口打开；不作为普通访客的默认底栏目标。
- 桌面端使用侧边导航呈现相同目的地；宽度变化不改变页面层级、对象语义或访问控制。

## 2. Birdtie 导航映射

| 产品 IA | 主界面职责 | 核心对象/操作 | 主要入口 |
|---|---|---|---|
| Now / Today | 当前选定城市正在发生和即将发生的事情 | upcoming/ongoing Activity、近期公开 Moment、活跃 Intent、城市内容状态 | 主导航 |
| Explore | Explore Anywhere 城市发现；地图与列表共享查询和筛选 | City、Place、Moment、Activity、Journey、Community；来源/更新时间；保存或发起 Intent | 主导航 |
| Network | 找到主动公开相关兴趣/Intent 的人、组织与社区 | People、Intent、Connection Request、Community；联系前明确对象与范围 | 主导航 |
| Inbox | 收件箱与关系中的下一步 | 联系请求状态、接受后会话、Agent 接待及转人工、组织人工接管 | 主导航 |
| My Birdtie | 个人身份和自己的资料/内容管理 | Profile、公开 Moments/Journeys、收藏、自己的 Intent、隐私设置 | 固定头像入口 |
| Personal Agent | 私人偏好、授权、Memory 导入和可审阅草稿 | Memory、Agent Run、草稿、提案、授权与运行记录 | My Birdtie + 上下文快捷入口 |
| Organization / City workspace | 维护被授权的城市内容和组织接待 | City Seed、Places、Activities、来源/有效期、角色与审核、City Agent 知识 | 有权限的 workspace switcher |

### 城市页面内部结构

Explore 选定 City 后，城市页面包含 Today/Now、Things to Do、Places、Moments & Experiences、Activities、Journeys、Communities & Organizations、People & Alumni 等模块。首期按照 MVP 范围逐步开放模块，但保留 IA 可扩展位置。空城显示真实供给状态和 seed 来源，不以虚构用户活跃度填充。

## 3. 跨页面交互规则

1. **城市上下文全局可见。** 用户可切换当前浏览 City，不要求设备定位；远程浏览不更改定位/位置隐私设置。
2. **地图与列表是一种 Explore 查询的两种呈现。** 共享 City、时间窗、类型筛选、查询状态、已选对象和返回位置。打开 Place/Moment/Activity/Journey 详情后返回，恢复查询条件。
3. **对象详情从任何上下文进入都指向同一个业务对象。** City、Place、User、地图和搜索不创建正文副本；详情中显示来源、发生时间、更新时间及授权的位置精度。
4. **创建动作先选对象类型，再填写和预览。** 全局 `Create` 可作为快捷入口，但不是第五个业务目的地；创建 Moment、Intent、Activity 等均走清晰的领域表单与确认步骤。
5. **需要身份的操作在边界处处理。** 未登录者可浏览公开城市；提交 Intent、发送连接请求、发布 Moment 等操作在明确的动作点进入登录/恢复流程。
6. **私密与公开内容分区。** Personal Agent 的 Memory/草稿不出现在 City Explore、公共搜索、地图或 City Agent；公开 Moment 只能通过作者确认后进入相应上下文。
7. **大屏用布局适配，不产生另一套 IA。** 侧边栏、页面内容区和上下文详情可并列；同一对象状态与动作语义保持一致。

## 4. 2026-09-25 之后前端结构审阅记录

只读审阅对象为 `D:\Program\Civu\Civu-client`，未改动其文件。相关提交包括：

- `efdf2a000`（2026-09-27）：保留个人页入口并将身份工具放入侧栏。
- `4b1bda628`（2026-09-27）：地图保持为独立主目的地，移除个人页重复动作。
- `885fb9ebf`（2026-09-28）：通知归入消息目的地。
- `a0c8045d8`（2026-09-28）：地图内容图层与持久地图宿主。
- `09b79850a`（2026-09-29）：地图详情承载 Moment 媒体和当前地点信息。

重点查看了 `lib/src/screens/page_home_shell.dart`、`lib/src/widgets/civu_home_tab_layout.dart` 和 `lib/src/state/home_tab.dart`。该实现呈现了这些可研究的 shell 技术模式：目的地集中定义、移动端底栏与宽屏 NavigationRail 复用目的地模型、固定创建快捷动作独立于选中 tab、详情通过页面栈承接、地图维持挂载并在后台停用，以及基于屏幕宽度切换 chrome/layout。

这些只是结构层模式。其 Civu 目的地集合（发现、消息、地图、旅程、个人）、默认地图首页、旅程主导航、登录拦截和具体屏幕语义都属于 Civu 产品决策，不直接搬入 Birdtie。Birdtie 按本文件第 1–3 节重新组织。

## 5. 当前实现与下一交付

此前 `apps/client/` 的早期预览有 Aberdeen 样例、筛选、详情、保存反馈和 Intent 草稿，但不满足本基线，现已由新 shell 替换；其样例内容与页面层级没有作为产品数据或新前端结构延续。

当前客户端已实现 shell scaffold、浏览器端 OIDC 回调、Session 与自身 Profile 读取，以及 Now/Explore 共用的公开 City 状态。Explore List 从 API 读取已发布 Place 并显示来源、维护者、时效和更新时间；Now 读取已审核公开 Activity 并区分 upcoming、ongoing、past、cancelled。登录后可在 Create 创建私人文字 Moment 草稿，并在 My Birdtie 查看、编辑或撤回；没有公开发布入口。尚未配置真实身份提供方，移动端和桌面端回调也未实现。下一阶段继续扩展 City Graph 前端连接点：

1. City selector 与城市上下文已从公开 API 读取；继续完善城市选择持久化、深链和加载状态。没有已发布城市时不展示虚构城市。
2. Explore Map/List 已共用选定 City、Place 名称/简介搜索结果和详情。Aberdeen Web Map 使用 Mapbox 并仅标注允许公开精确坐标的 Place；无令牌时显示可用性状态。继续增加分类/时间筛选、分页、地图相机恢复、国内高德适配和更完整的对象详情路由。
3. 依 Birdtie IA 完成 My Birdtie、Personal Agent 与 Organization workspace 路由边界；未接通的功能清楚标记，不伪装成可用能力。
4. API 接入时使用真实来源、维护者、更新时间和有效期；不恢复旧演示样例作为在线内容。

数据/API 接入遵循 [`MVP-IMPLEMENTATION-PLAN.md`](MVP-IMPLEMENTATION-PLAN.md) 和 [`03-Birdtie-技术架构-V1.0.md`](../architecture/03-Birdtie-技术架构-V1.0.md)；本文件不代表上述功能已经实现。
