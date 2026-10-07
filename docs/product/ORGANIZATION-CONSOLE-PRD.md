# Organization Console PRD

Status: proposed MVP requirements; the minimal Activity publisher and public Organization profile are implemented, while the remaining sections are targets. Source: `Birdtie_Execution_and_CSSA_Partner_Pack.zip!/Birdtie_Execution_and_CSSA_Partner_Pack/docs/03_ORGANIZATION_CONSOLE_PRD.md` (imported 2026-09-30). Principal ownership, membership roles and Agent authority defer to [the canonical model](../architecture/AGENT-IDENTITY-AND-OWNERSHIP-MODEL.md). Console UI copy is Simplified Chinese first; English is secondary. See [the local device acceptance record](../research/2026-10-01-organization-console-device-acceptance.md) and [the public profile verification](../research/2026-10-01-public-organization-verification.md).

## 1. Goal

让 CSSA 等真实组织能够在不依赖开发者的情况下维护自己的组织信息和活动供给。

核心成功指标：**常规活动发布 < 3 分钟。**

## 2. MVP Navigation

- Overview
- Activities
- Organization Profile
- Agent Knowledge / FAQ
- Announcements
- Analytics

可以先做单一 Console 页面/Tab，不要求复杂后台框架。

## 3. Role & Permission

至少：
- Organization Owner/Admin
- Editor（可选）

Backend 必须校验 membership/role。

## 4. Organization Claim / Verification

Pilot 可由 Birdtie 手工 invite organization admin，但系统仍需记录：
- verification_status
- verified_at
- verification_method/internal note

未 verified 的公开索引组织不能拥有官方发布身份。

## 5. Activity Creation

### Required fields
- Title
- Date/time
- Venue/place
- Category
- Description
- Visibility

### Optional
- Capacity
- Price/free
- Language
- Eligibility
- external registration URL

### UX
- draft autosave or safe form state
- validation inline
- place search/selection
- preview before publish

### AI assist (P1)
支持粘贴活动文案 -> 自动提取字段 -> 管理员确认；不得未经确认自动 publish。

## 6. Activity Lifecycle

`draft -> published -> completed`

可从 published：
- edit
- cancel

重要变更触发 participant notification。

## 7. Overview

MVP metrics：
- Upcoming activities
- Views
- Detail opens
- Going/RSVP
- Saves（可选）

不要在 Pilot 早期做复杂 BI。

## 8. Organization Agent Knowledge

只能回答 verified/public organization data：
- profile
- published activities
- FAQ
- official links

不知道时必须表达 unknown，不 hallucinate。

Admin 可维护 FAQ：
- How do I join?
- Who can attend?
- Where are updates posted?
- Contact method

当前实现（BT-OAG-001）：管理员可新增、编辑、发布、撤下和删除 FAQ；仅已认证且公开的组织，其启用中的 Organization Agent 才会用已发布 FAQ、公开组织资料、组织提供的链接和未来公开活动作带来源的规则式回答。未命中时明确回答未知。组织身份核验工作流、开放式自然语言理解及政策内容的独立事实核查仍需后续能力；界面不得暗示已具备。

## 9. Announcement

MVP 目标不是群聊，而是重要更新：
- activity participants
- followers（后续）

需要：
- title/body
- audience
- sent_at
- audit trail

## 10. Analytics & Pilot Export

CSSA one-event pilot 至少可以看到：
- activity views
- detail opens
- RSVPs
- source entry (Now / Agent / Organization page, if available)

活动结束后可导出/展示简单 funnel。

当前实现（BT-ANA-001）：管理员可在“查看活动数据”读取组织活动的展示、详情打开、报名和收藏次数，并按附近、智能体结果、地图、组织主页、收件箱等入口查看汇总。统计只保存活动级事件，不保存用户或精确位置；展示和详情是原始事件次数，不是独立访客。现阶段提供页面与 API 汇总，尚无文件导出或去重用户转化率。

## 11. Acceptance

1. Admin 登录后只看到其组织的写权限。
2. 创建 Published Activity 后立即进入真实 search/index flow。
3. 普通用户可以在 Now/Agent 查到。
4. Admin 修改 venue/time 后详情同步。
5. 已报名用户收到 update notification。
6. Cancel 后不能继续 Join。
7. Draft 永远不出现在 public ResultSet。
8. Admin 可以不找开发者完成全流程。
