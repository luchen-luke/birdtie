# Functional MVP PRD

Status: requirements, not an implementation claim. Source: `Birdtie_Execution_and_CSSA_Partner_Pack.zip!/Birdtie_Execution_and_CSSA_Partner_Pack/docs/01_FUNCTIONAL_MVP_PRD.md` (imported 2026-09-30). Agent identity and ownership are defined by [the canonical model](../architecture/AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md). Now behavior is defined by [the canonical Now specification](NOW-AGENT-MAP-WORKSPACE.md). UI copy defaults to Simplified Chinese; English is a complete secondary localization.

## 1. 文档目标

把 Birdtie 从静态壳子转成端到端真实可用的 MVP。本文档定义跨页面功能需求；具体 Now 与 Organization Console 见单独 PRD。

## 2. MVP Persona

### Student / User

需求：
- 快速知道附近近期有什么活动
- 用自然语言表达意图，而不是学习复杂筛选器
- 判断一个活动是否适合自己
- 报名、保存、查看未来安排
- 接收关键变更和提醒

### Organization Admin

需求：
- 3 分钟内发布活动
- 修改时间/地点/容量
- 看到报名/兴趣数据
- 向参与者发送必要更新
- 维护组织官方简介和常见问题

## 3. 核心 Domain Requirements

### AUTH-01 Authentication

**Must:**
- 可创建/登录真实用户
- Session/token 可持续
- API 使用真实身份
- 登出清理敏感状态

**Not required yet:**
- 复杂社交登录矩阵

### USER-01 User Profile

字段最低要求：
- display name
- optional university affiliation
- languages
- interests
- optional availability/preferences

Profile preference 用于排序，不用于屏蔽所有其他结果。

### ORG-01 Organization

组织字段：
- name
- slug
- description
- logo/avatar
- official links
- verification status
- visibility

### ORG-02 Membership / Role

至少：
- owner/admin
- member/editor（可后续合并）

所有写操作必须由 backend 权限验证，不能只靠 UI 隐藏按钮。

### ACT-01 Activity CRUD

字段：
- id
- organization_id / creator_id
- title
- description
- category
- starts_at / ends_at
- venue/place
- geo coordinates
- capacity (optional)
- price/free
- eligibility (optional)
- language (optional)
- visibility
- status: draft/published/cancelled/completed

### ACT-02 Activity Publishing

Draft 不得进入 public search。
Published 才能进入：
- Now
- Agent search
- Local Pulse counts
- Organization page

### ACT-03 Activity Updates

时间、地点、取消等关键变更必须：
- 更新数据库
- 更新所有 projection
- 对已报名用户产生 notification

### RSVP-01 Participation

状态至少：
- going
- pending（如需要审核）
- cancelled

必须数据库持久化并具有唯一性约束，防止重复 RSVP。

### SAVE-01 Save

用户可保存 Activity；与 RSVP 独立。

### PLAN-01 Plans

分区：
- Upcoming (going/pending)
- Saved
- Past

App restart 后仍应正确显示。

### NOTIF-01 Notifications

MVP 事件：
- RSVP approved（如有审核）
- activity starts soon
- venue/time changed
- activity cancelled

### INBOX-01 Messaging Scope

第一阶段优先 notifications。
若开放消息，优先：
- Organization -> participant
- Organizer -> activity participants

普通陌生用户私聊不是 P0。

## 4. Activity Detail Page Requirements

### Header
- Full title
- Organizer / verified state
- Hero/icon/category（没有图片也要稳定）

### Core info
- Date/time
- Venue
- Distance / area context
- Price/free
- Capacity / spots if available
- Eligibility
- Language if relevant

### Actions
Primary:
- Join / Request to join / Going

Secondary:
- Save
- Share
- Navigate

### Information
- Description
- Organizer profile entry
- Source / official link when indexed content is not partner-owned

### State
- loading
- success
- full/capacity reached
- cancelled
- unavailable
- error

## 5. Me Page Requirements

Sections:
- Profile
- Interests
- Languages
- Availability (optional)
- Agent preferences
- Privacy settings
- Sign out

Agent preference 仅改善 ranking，不应偷偷改变事实性搜索结果。

## 6. Functional Completion Definition

任何 requirement 只有同时满足以下条件才能标 `DONE`：

1. UI entry exists
2. backend behavior exists
3. real database persistence exists（若功能需要）
4. authorization / validation implemented
5. loading/empty/error handled
6. automated test or deterministic verification exists
7. real-device or integration verification completed for critical flow
8. app restart / state reload passes where applicable
9. no hardcoded production-facing result

否则必须标：`PARTIAL` / `MOCK` / `BLOCKED`。
