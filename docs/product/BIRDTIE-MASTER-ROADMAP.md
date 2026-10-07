# Birdtie Master Roadmap - From Static Shell to Aberdeen Pilot

V4 direction is now defined by [Birdtie V4 Canonical Product Spec](BIRDTIE-CANONICAL-PRODUCT-SPEC-V4.md). This roadmap is a 2026-09-30 planning snapshot for the earlier Functional MVP; its stage labels and “current” assessment below are historical and must not override the live queue, [completion report](../reports/FUNCTIONAL-MVP-COMPLETION-REPORT.md), or V4 gap audit.

Status: proposed sequencing and internal targets; current implementation must be assessed in [Functional MVP Gap Analysis](FUNCTIONAL-MVP-GAP-ANALYSIS.md). Source: `Birdtie_Execution_and_CSSA_Partner_Pack.zip!/Birdtie_Execution_and_CSSA_Partner_Pack/docs/00_BIRDTIE_MASTER_ROADMAP.md` (imported 2026-09-30). Identity, Now behavior and map runtime defer to their existing canonical documents. User-facing copy is Simplified Chinese first.

## 1. 当前阶段判断

Birdtie 当前版本应定义为 **UI / Technical Prototype**，而不是 Functional MVP。现阶段已经证明 Flutter UI、Map、基础 Agent shell 可以运行，但核心价值链仍缺失真实数据、真实动作、持久化与组织供给。

阶段定义：

| 阶段 | 完成条件 | 当前判断 |
|---|---|---|
| UI Prototype | 页面可打开、基本视觉和控件存在 | 基本完成 |
| Functional Prototype | UI 背后是真实 API、真实状态 | 未完成 |
| Vertical Slice | 一条核心链路端到端可运行 | 未完成 |
| Closed Pilot | 真实组织 + 真实学生使用 | 未开始 |
| MVP | 可持续获得核心价值 | 未开始 |
| Public Beta | 多组织、多场景、可增长 | 未开始 |

## 2. Product North Star

Birdtie 当前不是“地图 + AI 搜索框”，而是：

> Agent-native local social discovery & participation platform.

对 Aberdeen 第一阶段用户，更具体地说：

> 帮助学生知道附近发生什么，找到值得参加的活动与组织，并真正参与进去。

开发优先级必须服务于：

```text
Real intent
  -> Personal Agent understands
  -> Real ResultSet
  -> Map / Conversation / Cards
  -> Activity detail
  -> Join / Save
  -> Plans / Reminder
  -> Real-world participation
```

## 3. 第一条必须打穿的 Vertical Slice

### Supply side

```text
Organization Admin
 -> Create Activity
 -> Publish
 -> Activity persisted
 -> Visible to public search / map
```

### User side

```text
User
 -> Open Now
 -> Local Pulse or natural-language query
 -> Personal Agent
 -> Real Activity ResultSet
 -> Activity Detail
 -> Join / RSVP
 -> Participation persisted
 -> Plans
```

这条链路没有打通前，不新增大型模块。

## 4. 一级导航 MVP

建议仅保留：

- **Now** - 当前区域发现 + Personal Agent 主入口
- **Plans** - 已参加、已收藏、历史活动
- **Inbox** - 通知优先；消息能力克制开放
- **Me** - Profile + Agent preferences

不要在 MVP 阶段新增 Discover / Groups / Community / Agents 等重叠一级入口。

## 5. 产品域与责任边界

```text
Real User
  |-- Personal Agent
  |     |-- Understand intent
  |     |-- Search/filter/compare
  |     '-- Conversation context
  |
  |-- Plans
  |-- Saves
  '-- Notifications

Organization
  |-- Membership / Admin permission
  |-- Organization Agent
  |-- Activities
  '-- Announcements / Analytics

CityContext
  |-- Public geographic context
  |-- Area Pulse input
  '-- No account / no social identity
```

### 关键原则

1. Personal Agent 是用户入口，不是数据库实体的替代品。
2. Organization Agent 只能基于该组织的 verified/public knowledge 回答。
3. CityContext 是公共上下文，不是“City Agent 用户”。
4. Activity / Organization / Place 是真实可操作实体。
5. Pin / Card / Sheet / Conversation Result 都是 Entity / ResultSet 的 projection。

## 6. 8 周推进计划

### Week 1 - 系统真正活起来

- 真机 API connectivity
- Auth 基础
- Activity / Organization / Place 真实数据库链路
- Development seed fixtures（通过真实 DB/API，不在 Widget hardcode）
- Local Pulse API
- Agent Query API
- Debug diagnostics / observability

**Gate:** 手机上可以通过真实 API 查到数据库里的真实 Activity。

### Week 2 - 用户 Vertical Slice

- Now -> Agent -> ResultSet
- Activity Detail
- RSVP
- Plans
- App restart 后状态持久化

**Gate:** 一个真实用户可以参加一个真实活动。

### Week 3 - Organization Vertical Slice

- Organization Admin 权限
- Activity create/edit/publish/cancel
- Organization Console minimal web/mobile management UI

**Gate:** CSSA 管理员不找开发者即可发布活动。

### Week 4 - Retention & Trust

- Notifications
- Activity update/cancel handling
- Organization verified knowledge / FAQ
- Basic analytics
- Report/moderation entry points

### Week 5 - CSSA Closed Pilot

- 10-20 testers
- 1-2 live events
- 记录 funnel / failures / qualitative feedback

### Week 6 - Fix Week

不开发大型新功能，只修：

- blockers
- reliability
- UX confusion
- content quality
- performance

### Week 7 - 扩大 3-5 个组织

验证 Birdtie 是否只对 CSSA 有效，还是跨活动类别成立。

### Week 8 - Aberdeen Beta Readiness

内部目标：

- 5 partner organizations
- 30+ current/upcoming activities
- 50-100 active testers
- 核心链路稳定

## 7. 暂缓范围

MVP 前暂缓：

- 陌生人完整私聊
- 好友关系和复杂 follow graph
- Feed / Moments
- 用户实时精确位置
- 多城市
- 高级推荐算法
- 广告、抽佣、复杂付费
- 大型 creator economy
- “万能 Agent”

## 8. 供给冷启动原则

AI 无法解决空数据库。

Pilot 前建议内容底线：

- 15-20 Organizations indexed
- 25-40 upcoming Activities
- 30+ Places
- 5-10 Chinese/CSSA-related activities or evergreen organization resources
- 15+ sports/social activities

Partner Organization 才拥有 Verified Page / Admin / Organization Agent；公开索引不能伪装成官方合作。

## 9. Pilot 核心指标

### Technical

- Crash-free sessions > 99.5%
- Search/API success > 98%
- Map keyboard flicker = 0
- stale result overwrite = 0
- Activity publish success > 99%
- initial core UI interactive < 3s（正常网络开发目标）

### Product（内部验证目标，不是行业 benchmark）

- 新用户 5 分钟内发现至少 1 条相关内容 > 70%
- Search -> Detail > 30%
- Detail -> Join/Save > 15%
- 7 日内二次打开 > 30%
- 合作组织发布活动 < 3 分钟
- 5 个 pilot organizations 中至少 3 个愿意继续使用

## 10. 决策原则

每新增一个需求先问：

> 它是否明显提高“发现值得参加的事情 -> 真正参与”的成功率、效率或信任？

如果不是，默认进入 Backlog 而不是当前 Sprint。
